"""Freeze an experiment, supervise native trials, and publish its evidence."""
import copy
from decimal import Decimal
import json
import os
from pathlib import Path
import re
import signal
import threading
import time

from . import baseline
from .common import (EVALS, REPO, atomic_write, file_hash, identifier, inside, new_id, read_json,
                     utc_now, value_hash, write_json)
from .execution import execute_job, job_config, run_pool, schedule
from .integrity import inventory
from .prepare import (available_capacity, build_candidate, catalog, configuration,
                      contract, environment, pull_images)
from .report import build_result, compare_results, empty_trial, money, normalize_trial, write_report
from .tasks import load_spec, prepare_tasks


def default_engine():
    home = Path(os.environ.get("WHIP_HOME", Path.home() / ".whip"))
    config = home / "config.json"
    value = "starlark"
    if config.exists():
        # Whip's config is JSONC: preserve quoted strings while removing comments
        # and trailing commas. This reads one preference; it never copies the home.
        string = r'"(?:\\.|[^"\\])*"'
        text = re.sub(string + r'|//[^\n]*|/\*[\s\S]*?\*/',
                      lambda m: m[0] if m[0].startswith('"') else ' ', config.read_text())
        text = re.sub(string + r'|,(?=\s*[}\]])', lambda m: m[0] if m[0].startswith('"') else '', text)
        try:
            value = json.loads(text).get("rlm", {}).get("defaultEngine", "starlark")
        except (ValueError, AttributeError) as error:
            raise ValueError("cannot read configured engine; repair Whip config or specify --engines") from error
    if value not in ("starlark", "quickjs"):
        raise ValueError("invalid configured execution engine")
    return value


def plan_run(args, *, evals=EVALS):
    lock, profiles, protocol = load_spec(evals)
    engines = args.engines.split(",") if args.engines else [default_engine()]
    if not 1 <= len(engines) <= 2 or len(set(engines)) != len(engines) or set(engines) - {"starlark", "quickjs"}:
        raise ValueError("choose one or two distinct engines: starlark,quickjs")
    if args.against and len(engines) != 1:
        raise ValueError("--against takes one candidate engine")
    if args.promote and (args.profile != "full" or len(engines) != 1 or args.against not in (None, "baseline")):
        raise ValueError("--promote requires Full, one candidate, and the accepted baseline control")
    repetitions = args.repetitions if args.repetitions is not None else (3 if args.promote else 1)
    if repetitions < 1 or (args.promote and repetitions != 3):
        raise ValueError("positive repetitions required; promotion uses exactly three")
    if not 1 <= args.jobs <= protocol["max_jobs"]:
        raise ValueError("jobs must be between 1 and 32")
    pointer = baseline.current(evals, protocol["track"])
    if args.against == "baseline" and pointer is None and not args.promote:
        raise ValueError("no accepted baseline exists; initialize with run full --promote")
    against = "baseline" if args.promote and pointer else args.against
    if args.promote and pointer is None:
        against = None
    count = 2 if against else len(engines)
    tasks = profiles[args.profile]["task_ids"]
    return {"profile": args.profile, "profile_version": profiles[args.profile]["version"],
            "task_ids": tasks, "engines": engines, "repetitions": repetitions,
            "candidate_count": count, "trial_count": len(tasks) * repetitions * count,
            "against": against, "baseline_at_launch": pointer, "jobs": args.jobs,
            "model": protocol["model"], "provider": protocol["provider"], "effort": protocol["effort"],
            "limits": protocol["limits"], "promote": args.promote}


def cost_estimate(previous, pointer, planned):
    estimate = {"cost_usd": None, "basis_run_id": pointer["run_id"] if pointer else None,
                "method": "accepted per-task mean cost, projected to all planned arms/repetitions", "is_limit": False}
    if previous:
        selected = [r for r in previous[1]["trials"] if r["candidate_id"] == pointer["candidate_id"]
                    and r["task_id"] in planned["task_ids"]]
        if selected and all(r["cost_usd"] is not None for r in selected):
            means = []
            for task in planned["task_ids"]:
                costs = [Decimal(r["cost_usd"]) for r in selected if r["task_id"] == task]
                if not costs:
                    return estimate
                means.append(sum(costs) / len(costs))
            estimate["cost_usd"] = money(sum(means) * planned["repetitions"] * planned["candidate_count"])
    return estimate


def run(args, *, evals=EVALS, repo=REPO):
    evals, repo = Path(evals).resolve(), Path(repo).resolve()
    planned = plan_run(args, evals=evals)
    if args.dry_run:
        return planned
    lock, profiles, protocol = load_spec(evals)
    task_index = {task["id"]: task for task in lock["tasks"]}
    tasks = [task_index[task_id] for task_id in planned["task_ids"]]
    host = environment(evals)
    if host["runner_versions"] != protocol["runner_versions"] or host["os"] != "linux":
        raise ValueError("use the pinned Python environment and a Linux Docker engine")
    if args.promote and host["emulated"]:
        raise ValueError("promotion requires native linux/amd64 execution")
    capacity = available_capacity(host, protocol)
    for task in tasks:
        if any(task["resources"][key] > capacity[key] for key in capacity):
            raise ValueError("task cannot fit available capacity: " + task["id"])
    run_id = identifier(args.run_id) if args.run_id else new_id()
    report_dir = evals / "reports" / run_id
    report_dir.mkdir(parents=True, exist_ok=False)
    artifact_root = evals / "artifacts" / run_id
    artifact_root.mkdir(parents=True, exist_ok=False)
    started = time.monotonic()
    write_json(report_dir / "request.json", {"run_id": run_id, "created_at": utc_now(), **planned}, exclusive=True)
    print(f"Preparing {run_id}: {planned['trial_count']} trials; at most {args.jobs} active. No evaluator spending/token cap.", flush=True)
    try:
        candidates = []
        pointer = planned["baseline_at_launch"]
        previous = baseline.load_accepted(evals, pointer)
        estimate = cost_estimate(previous, pointer, planned)
        print("Historical cost estimate (not a limit): " + ("$" + estimate["cost_usd"] if estimate["cost_usd"] is not None else "unavailable"), flush=True)
        if planned["against"] == "baseline":
            old_manifest, _ = previous
            control = copy.deepcopy(next(c for c in old_manifest["candidates"] if c["id"] == pointer["candidate_id"]))
            control.update(id="control", baseline_revision=pointer["revision"])
            if file_hash(inside(evals, control["binary_path"])) != control["binary_sha256"]:
                raise ValueError("accepted build is unavailable or changed; restore its archived cache")
            candidates.append(control)
        elif planned["against"]:
            candidates.append(build_candidate("control", planned["engines"][0], repo=repo, evals=evals, ref=planned["against"]))
        # Capture the working tree once: editing source during preparation must
        # never turn a runtime comparison into a comparison of two agent builds.
        shared = build_candidate("candidate", planned["engines"][0], repo=repo, evals=evals, ref=args.ref)
        for engine in planned["engines"]:
            name = engine if len(planned["engines"]) == 2 else "candidate"
            candidate = copy.deepcopy(shared)
            candidate.update(id=name, engine=engine, configuration=configuration(engine))
            candidates.append(candidate)
        if args.promote and any(c["dirty"] for c in candidates):
            raise ValueError("promotion requires clean source; commit changes or use --ref")
        prepared = prepare_tasks(lock, tasks, evals / "cache" / "tasks")
        print("Pulling pinned images before task clocks start.", flush=True)
        pull_images(tasks)
        model = catalog(protocol)
        contracts = {}
        for candidate in candidates:
            path = artifact_root / "contracts" / (candidate["id"] + ".json")
            write_json(path, contract(candidate, protocol, model), exclusive=True)
            contracts[candidate["id"]] = path
        trials = schedule(tasks, candidates, planned["repetitions"], args.seed)
        comparison = {"protocol_sha256": value_hash(protocol), "task_lock_sha256": value_hash(lock),
            "provider_catalog_sha256": value_hash(model), "environment": {key: host[key] for key in
                ("os", "architecture", "docker_version", "cpu_model", "emulated", "runner_versions")},
            "requested_jobs": args.jobs, "resource_policy": protocol["headroom"],
            "measurement_code": {name: file_hash(Path(__file__).with_name(name)) for name in
                ("adapter.py", "observe.py", "execution.py", "report.py", "tasks.py")}}
        manifest = {"schema_version": 1, "run_id": run_id, "created_at": utc_now(),
            "profile": args.profile, "profile_version": planned["profile_version"],
            "task_ids": planned["task_ids"], "repetitions": planned["repetitions"], "seed": args.seed,
            "track": protocol["track"], "protocol": protocol, "promotion_policy": protocol["promotion"],
            "promote": args.promote, "label": args.label, "candidates": candidates, "schedule": trials,
            "baseline_at_launch": pointer, "host": host, "capacity": capacity, "jobs": args.jobs,
            "cost_estimate": estimate,
            "external_references": read_json(evals / "frontier" / "references.json"),
            "comparison": comparison, "comparison_key": value_hash(comparison), "catalog": model,
            "task_lock_sha256": value_hash(lock), "prepared_tasks": {key: value["sha256"] for key, value in prepared.items()},
            "resolved_tasks": {key: {section: value["resolved_native"][section] for section in ("agent", "environment", "verifier")}
                               for key, value in prepared.items()},
            "artifact_root": artifact_root.relative_to(evals).as_posix(), "uv_lock_sha256": file_hash(evals / "uv.lock")}
        if args.promote and previous and manifest["comparison_key"] != previous[1]["comparison_key"]:
            raise ValueError("baseline protocol/environment differs; establish a new track before promotion")
        write_json(report_dir / "manifest.json", manifest, exclusive=True)
        # Freeze every resolved native job and deadline before the first launch.
        jobs = {}
        for trial in trials:
            candidate = next(c for c in candidates if c["id"] == trial["candidate_id"])
            jobs[trial["id"]] = job_config(trial, task_index[trial["task_id"]],
                inside(evals, candidate["binary_path"]), contracts[candidate["id"]],
                prepared[trial["task_id"]]["path"], artifact_root / "jobs")
        write_json(artifact_root / "schedule.json", {key: {"config": val[0], "envelope": val[1]} for key, val in jobs.items()}, exclusive=True)
    except BaseException as error:
        write_json(report_dir / "preparation-error.json", {"status": "preparation_failed", "error_code": type(error).__name__, "created_at": utc_now()}, exclusive=True)
        atomic_write(report_dir / "report.md", (f"# Whip evaluation: {run_id}\n\nPreparation failed ({type(error).__name__}). No trials started; no score is asserted.\n").encode(), exclusive=True)
        raise
    cancelled = threading.Event()
    cancellation_source = "controller_cancelled"
    previous_handlers = {}
    def cancel_from_signal(*_):
        nonlocal cancellation_source
        cancellation_source = "user_cancelled"
        cancelled.set()

    for sig in (signal.SIGINT, signal.SIGTERM):
        previous_handlers[sig] = signal.signal(sig, cancel_from_signal)
    raw_records, normalized = {}, {}

    def worker(trial, event):
        config, envelope = jobs[trial["id"]]
        return execute_job(trial, config, envelope, artifact_root / "trials" / trial["id"], event, evals=evals)

    def collect(trial, raw):
        value = dict(raw)
        if value.get("cancelled"):
            value["cancellation_source"] = cancellation_source
        if value.get("job_path"):
            value["job_path"] = Path(value["job_path"]).relative_to(artifact_root).as_posix()
        raw_records[trial["id"]] = value
        try:
            row = normalize_trial(trial, value, artifact_root)
        except Exception as error:
            row = empty_trial(trial)
            row.update(started=value.get("started", False), execution_status="export_error",
                       termination_source="export_error", error_codes=[type(error).__name__],
                       cleanup_complete=value.get("cleanup", {}).get("complete"))
        normalized[trial["id"]] = row
        write_json(artifact_root / "trial-records.json", raw_records)
        write_json(report_dir / "progress.json", {"completed": len(normalized), "planned": len(trials), "updated_at": utc_now()})
        print(f"{len(normalized)}/{len(trials)} {trial['candidate_id']} {trial['task_id']}: {row['grader_status']}; cause={row['termination_source']}", flush=True)

    pool_stats = {}
    preparation_seconds = time.monotonic() - started
    try:
        outcomes = run_pool(trials, capacity, args.jobs, worker, cancelled=cancelled, on_result=collect, stats=pool_stats)
        for trial in trials:
            if trial["id"] not in normalized:
                collect(trial, outcomes[trial["id"]])
    finally:
        cancelled.set()
        for sig, handler in previous_handlers.items():
            signal.signal(sig, handler)
        result = build_result(manifest, list(normalized.values()), wall_seconds=time.monotonic() - started)
        result.update(concurrency={"requested": args.jobs, **pool_stats}, preparation_seconds=preparation_seconds)
        audit_path = artifact_root / "integrity.json"
        audit = inventory(artifact_root, audit_path)
        write_json(audit_path, audit, exclusive=True)
        result["integrity"] = {"path": audit_path.relative_to(evals).as_posix(), "sha256": file_hash(audit_path),
            "complete": not audit["errors"] and not audit["skipped_nonregular"],
            "exact_key_match_count": len(audit["exact_key_matches"])}
        if previous:
            result["historical_comparison"] = compare_results(previous[1], result,
                control_id=pointer["candidate_id"], seed=args.seed)
        write_report(report_dir, manifest, result)
        (report_dir / "progress.json").unlink(missing_ok=True)
    if args.promote:
        decision = baseline.publish(evals, manifest, result)
        print("Baseline accepted." if decision["accepted"] else "Baseline held: " + ", ".join(decision["failed_gates"]), flush=True)
    return {"run_id": run_id, "status": result["status"], "report": str(report_dir / "report.md")}


def regenerate(run_id, *, evals=EVALS):
    directory = Path(evals) / "reports" / identifier(run_id)
    manifest = read_json(directory / "manifest.json")
    root = inside(evals, manifest["artifact_root"])
    records_path = root / "trial-records.json"
    records = read_json(records_path) if records_path.exists() else {}
    rows = []
    for trial in manifest["schedule"]:
        raw = records.get(trial["id"])
        if raw is None:
            path = root / "trials" / trial["id"] / "execution.json"
            if path.exists():
                raw = read_json(path)
                raw["job_path"] = Path(raw["job_path"]).relative_to(root).as_posix()
        try:
            rows.append(normalize_trial(trial, raw, root) if raw else empty_trial(trial))
        except (OSError, ValueError, KeyError, TypeError) as error:
            row = empty_trial(trial)
            row.update(execution_status="export_error", termination_source="export_error", error_codes=[type(error).__name__])
            rows.append(row)
    result = build_result(manifest, rows)
    result["analysis_code_sha256"] = file_hash(Path(__file__).with_name("report.py"))
    output = directory / "analyses" / new_id()
    write_report(output, manifest, result)
    return {"report": str(output / "report.md"), "status": result["status"]}
