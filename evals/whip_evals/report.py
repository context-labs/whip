"""Offline normalization, paired statistics, and one JSON/Markdown/CSV projection."""
from collections import Counter
import csv
from decimal import Decimal
from datetime import datetime
import io
import json
import sqlite3
import math
from pathlib import Path
import random
import statistics

from .common import (SCHEMA_VERSION, atomic_write, file_hash, inside, number,
                     read_json, utc_now, value_hash, write_json)
from .observe import aggregate, rows


def quantile(values, fraction):
    if not values:
        return None
    values = sorted(values)
    position = (len(values) - 1) * fraction
    lower = int(position)
    upper = min(lower + 1, len(values) - 1)
    return values[lower] + (values[upper] - values[lower]) * (position - lower)


def money(value):
    return str(Decimal(str(value)).quantize(Decimal("0.000001")))


def empty_trial(trial):
    return {key: trial[key] for key in ("id", "task_id", "candidate_id", "repetition", "runner")} | {
        "started": False, "execution_status": "not_started", "grader_status": "missing",
        "success": None, "termination_source": None, "evidence_complete": False,
        "accounting_complete": False, "agent_seconds": None, "trial_seconds": None,
        "known_cost_usd": "0.000000", "cost_usd": None, "unknown_cost_calls": None,
        "unknown_usage_calls": None, "model_calls": None,
        "input_tokens": None, "output_tokens": None, "cache_tokens": None,
        "cleanup_complete": None, "error_codes": [], "artifacts": {}, "phase_seconds": {}}


PROVIDER_ERROR_TOKENS = ("api error:", "stream timed out", "no next token", "without a completion marker",
                         "invalid stream chunk", "429", "502", "503", "504", "520", "rate limit",
                         "connection reset", "unexpected eof", "context deadline exceeded",
                         "provider stream stalled", "per-attempt ceiling")
REQUIRED_EVIDENCE = ("state.json", "metrics.json", "outcome.json", "sessions.db", "identity.json",
                     "content-export.json", "configuration.json", "provider-catalog.json", "cli.ndjson", "cli.stderr")


def termination(outcome, raw, diagnostic, structured_errors=()):
    if raw.get("cancelled"):
        return "user_cancelled"  # only a human cancels a run
    if raw.get("outer_watchdog"):
        return "evaluator_watchdog"
    if outcome.get("status") == "timeout":
        return "benchmark_deadline"
    lower = diagnostic.lower()
    if "client.timeout" in lower or "timeout awaiting response headers" in lower:
        return "whip_request_timeout"
    if any(token in lower for token in ("host request limit", "step limit", "budget exhausted", "worker limit", "job limit")):
        return "whip_guard"
    if outcome.get("status") in ("agent_error", "startup_error"):
        # Generic status codes and transport words are evidence only in Whip's
        # structured errors, not arbitrary task output written to stderr.
        provider = any(token in error.lower() for error in structured_errors
                       for token in PROVIDER_ERROR_TOKENS)
        provider = provider or any(line.strip().startswith(("api error:", "whip: api error:", "whipcode: api error:"))
                                   for line in lower.splitlines())
        provider = provider or any(token in lower for token in (
            "inference stream timed out: no next token", "provider stream stalled: no data for"))
        provider = provider or ("model call exceeded the " in lower and " per-attempt ceiling" in lower)
        return "provider_error" if provider else "agent_error"
    if outcome.get("status") == "observer_error":
        return "export_error"
    return None


def phase_duration(timing):
    if not timing or not timing.get("started_at") or not timing.get("finished_at"):
        return None
    try:
        elapsed = (datetime.fromisoformat(timing["finished_at"]) - datetime.fromisoformat(timing["started_at"])).total_seconds()
        return elapsed if number(elapsed) else None
    except (TypeError, ValueError):
        return None


def span_counts(database):
    """Trace spans captured in the copied session database (schema 19+)."""
    if database is None:
        return {"span_count": None, "open_span_count": None}
    try:
        with sqlite3.connect(f"file:{database}?mode=ro&immutable=1", uri=True) as db:
            if not db.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='spans'").fetchone():
                return {"span_count": None, "open_span_count": None}
            total, open_spans = db.execute("SELECT count(*), coalesce(sum(end_ns=0), 0) FROM spans").fetchone()
        return {"span_count": total, "open_span_count": open_spans}
    except sqlite3.Error:
        return {"span_count": None, "open_span_count": None}


def partial_metrics(job_dir):
    """The observer's last metrics.json under a job that produced no native result."""
    samples = []
    for path in (sorted(job_dir.glob("*/agent/whip/metrics.json")) if job_dir and job_dir.is_dir() else []):
        try:
            sample = read_json(path)
        except (OSError, ValueError):
            continue
        if number(sample.get("ledger_cost_usd")):
            samples.append((sample, path))
    return max(samples, key=lambda item: item[0]["ledger_cost_usd"], default=None)


def normalize_trial(trial, raw, artifact_root):
    artifact_root = Path(artifact_root).resolve()
    row = empty_trial(trial)
    row.update(started=bool(raw.get("started")), trial_seconds=raw.get("trial_seconds"),
               cleanup_complete=raw.get("cleanup", {}).get("complete", False),
               phase_seconds=raw.get("phase_seconds", {}))
    if raw.get("error_code"):
        row["error_codes"].append(raw["error_code"])
    job_dir = inside(artifact_root, raw["job_path"]) if raw.get("job_path") else None
    results = list(job_dir.glob("*/result.json")) if job_dir else []
    if len(results) != 1:
        row.update(execution_status="cancelled" if raw.get("cancelled") else "runner_error",
                   termination_source=termination({}, raw, "") or "setup_error")
        row["error_codes"].append("missing_result" if not results else "multiple_results")
        # A run that died before its native result still paid for its model
        # calls: keep the observer's last ledger figure as a lower bound.
        partial = partial_metrics(job_dir)
        if partial:
            sample, path = partial
            row["known_cost_usd"] = money(sample["ledger_cost_usd"])
            for key in ("model_calls", "unknown_cost_calls", "unknown_usage_calls"):
                row[key] = sample.get(key)
            row["partial_observation"] = {"path": path.relative_to(artifact_root).as_posix(), "not_final": True,
                                          "observed_input_tokens": sample.get("input_tokens"),
                                          "observed_output_tokens": sample.get("output_tokens")}
        return row
    native_path = results[0]
    native = read_json(native_path)
    row["phase_seconds"].update({key: phase_duration(native.get(key)) for key in
        ("environment_setup", "agent_setup", "agent_execution", "verifier")})
    metadata = (native.get("agent_result") or {}).get("metadata") or {}
    # The observer writes to /logs/agent/whip, bind-mounted from the trial's
    # agent directory; the adapter only reports its own export failures.
    evidence = native_path.parent / "agent" / "whip"
    errors = ["adapter_export_error"] if metadata.get("whip_evidence_errors") else []
    paths = {name: evidence / name for name in REQUIRED_EVIDENCE}
    if any(not path.is_file() for path in paths.values()):
        errors.append("missing_required_evidence")
    paths = {name: (path if path.is_file() else None) for name, path in paths.items()}
    outcome = read_json(paths["outcome.json"]) if paths["outcome.json"] else {}
    metrics = read_json(paths["metrics.json"]) if paths["metrics.json"] else {}
    state = read_json(paths["state.json"]) if paths["state.json"] else {}
    content = read_json(paths["content-export.json"]) if paths["content-export.json"] else {}
    if not outcome.get("content_export_complete") or content.get("errors"):
        errors.append("incomplete_content_export")
    if paths["identity.json"]:
        identity = read_json(paths["identity.json"])
        if identity.get("engine") != trial["engine"] or identity.get("binary_sha256") != trial["binary_sha256"]:
            errors.append("candidate_identity_mismatch")
    rewards = (native.get("verifier_result") or {}).get("rewards") or {}
    reward = rewards.get("reward")
    if number(reward) and reward in (0, 1):
        row.update(success=reward == 1, grader_status="passed" if reward == 1 else "failed")
    else:
        row["grader_status"] = "error" if native.get("exception_info") else "missing"
    diagnostic = paths["cli.stderr"].read_text(errors="replace") if paths["cli.stderr"] else ""
    # In --format=json mode Whip emits structured errors on stdout. Ordinary
    # model text is not diagnostic evidence and must not determine attribution.
    structured_errors = []
    if paths["cli.ndjson"]:
        for line in paths["cli.ndjson"].read_text(errors="replace").splitlines():
            try:
                message = json.loads(line)
            except ValueError:
                continue
            if isinstance(message, dict) and message.get("type") == "error" and isinstance(message.get("error"), str):
                diagnostic += "\n" + message["error"]
                structured_errors.append(message["error"])
    row.update(execution_status=outcome.get("status", "cancelled" if raw.get("cancelled") else "runner_error"),
               termination_source=termination(outcome, raw, diagnostic, structured_errors))
    if row["termination_source"] is None and row["grader_status"] == "error":
        # A native exception before agent execution is setup, not grading. Keep
        # the native timing boundary authoritative; do not infer from log prose.
        executed = (native.get("agent_execution") or {}).get("started_at")
        verifying = (native.get("verifier") or {}).get("started_at")
        row["termination_source"] = "verifier_error" if verifying else "agent_error" if executed else "setup_error"
    # Finality: the observer took its last snapshot with the daemon frozen or
    # stopped, nothing was pending, and its export reported no errors.
    final = (outcome.get("final_snapshot") is True and not outcome.get("evidence_errors")
             and bool(outcome.get("frozen_daemon_pid") or outcome.get("daemon_stopped"))
             and bool(state.get("settled")) and not any((state.get("pending") or {}).values()))
    if not final:
        errors.append("incomplete_finality")
    duration = outcome.get("agent_duration_seconds")
    row["agent_seconds"] = duration if number(duration) and final else None
    calls = state.get("calls")
    # The copied database is the canonical ledger. A partial or desynchronized
    # upload must not make edited state.json totals eligible for promotion.
    if paths["sessions.db"]:
        try:
            with sqlite3.connect(paths["sessions.db"].as_uri() + "?mode=ro&immutable=1", uri=True) as db:
                db.row_factory = sqlite3.Row
                roots = [row["id"] for row in db.execute("SELECT id FROM sessions")]
                if roots != [(state.get("root") or {}).get("id")]:
                    errors.append("accounting_root_mismatch")
                copied_calls = rows(db, "SELECT * FROM model_calls ORDER BY rowid")
            if calls != copied_calls:
                errors.append("accounting_snapshot_mismatch")
        except sqlite3.Error:
            errors.append("unreadable_accounting_snapshot")
    computed = aggregate(calls) if calls is not None else {}
    # Complete accounting: every dispatched call reported usage and a cost, and
    # nothing about the evidence is in doubt. Unknown usage never becomes zero.
    row["accounting_complete"] = not errors and all(computed.get(key) == 0 for key in
                                                    ("unknown_usage_calls", "unknown_cost_calls", "pending_calls"))
    if calls is not None:
        row["known_cost_usd"] = money(sum(Decimal(call["cost_micros"]) for call in calls) / 1_000_000)
    elif number(metrics.get("ledger_cost_usd")):
        row["known_cost_usd"] = money(metrics["ledger_cost_usd"])
    if row["accounting_complete"]:
        row["cost_usd"] = row["known_cost_usd"]
    # Token totals are lower bounds when usage calls are unknown; they are shown,
    # and the unknown-usage count beside them says how firm they are.
    for key in ("unknown_cost_calls", "unknown_usage_calls", "model_calls", "input_tokens", "output_tokens", "cache_tokens"):
        row[key] = computed.get(key, metrics.get(key))
    row["diagnostics"] = {key: metrics.get(key) for key in (
        "peak_input_tokens", "sampled_peak_container_rss_bytes", "sampled_container_cpu_seconds",
        "reported_cost_usd", "reported_cost_calls")}
    row["diagnostics"].update(agent_count=len(state.get("agents", [])), turn_count=len(state.get("turns", [])),
                              **span_counts(paths["sessions.db"]))
    row["evidence_complete"] = not errors and row["cleanup_complete"] is True
    row["error_codes"].extend(sorted(set(errors)))
    row["artifacts"] = {"native_result": native_path.relative_to(artifact_root).as_posix(),
                        "native_result_sha256": file_hash(native_path),
                        "evidence_files": {p.relative_to(artifact_root).as_posix(): file_hash(p)
                                           for p in paths.values() if p is not None}}
    return row


def normalize_or_error(trial, raw, artifact_root):
    """A row whose evidence cannot be read is an export error, never a missing trial."""
    try:
        return normalize_trial(trial, raw, artifact_root)
    except (OSError, ValueError, KeyError, TypeError) as error:
        row = empty_trial(trial)
        row.update(started=bool(raw.get("started")), execution_status="export_error",
                   termination_source="export_error", error_codes=[type(error).__name__],
                   cleanup_complete=raw.get("cleanup", {}).get("complete"))
        return row


def summarize(rows):
    n = len(rows)
    passed = sum(row["success"] is True for row in rows)
    graded = sum(row["success"] is not None for row in rows)
    complete_cost = bool(rows) and all(row.get("cost_usd") is not None for row in rows)
    known = sum((Decimal(row["known_cost_usd"]) for row in rows), Decimal(0))
    times = [row["agent_seconds"] for row in rows if number(row.get("agent_seconds"))]
    pass_times = [row["agent_seconds"] for row in rows if row["success"] is True and number(row.get("agent_seconds"))]
    totals = {key: sum(row[key] for row in rows) if rows and all(row.get(key) is not None for row in rows) else None
              for key in ("input_tokens", "output_tokens", "cache_tokens", "unknown_cost_calls", "unknown_usage_calls", "model_calls")}
    return {
        "planned": n, "started": sum(row["started"] for row in rows), "graded": graded,
        "passed": passed, "failed": graded - passed, "ungraded": n - graded,
        "cancelled": sum(row["termination_source"] == "user_cancelled" for row in rows),
        "provider_errors": sum(row["termination_source"] == "provider_error" for row in rows),
        "evidence_incomplete": sum(not row["evidence_complete"] for row in rows),
        "verified_success_rate": passed / n if n else None,
        "graded_pass_rate": passed / graded if graded else None,
        "evidence_complete": sum(row["evidence_complete"] for row in rows),
        "accounting_complete": sum(row["accounting_complete"] for row in rows),
        "known_cost_usd": money(known), "cost_usd": money(known) if complete_cost else None,
        "effective_cost_per_pass_usd": money(known / passed) if complete_cost and passed else None,
        "agent_median_seconds": statistics.median(times) if times else None,
        "agent_p95_seconds": quantile(times, .95), "agent_time_count": len(times),
        "success_median_seconds": statistics.median(pass_times) if pass_times else None,
        "success_time_count": len(pass_times),
        "termination_sources": dict(Counter(row["termination_source"] or "none" for row in rows)),
        "tokens": totals,
        "cache_read_ratio": totals["cache_tokens"] / totals["input_tokens"]
            if totals["input_tokens"] and totals["cache_tokens"] is not None else None,
    }


def compare_trials(control, candidate, *, seed=0, samples=10000):
    def index(rows):
        result = {}
        for row in rows:
            key = (row["task_id"], row["repetition"])
            if key in result:
                raise ValueError("duplicate task/repetition in comparison")
            result[key] = row
        return result
    left, right = index(control), index(candidate)
    tasks = sorted({key[0] for key in left} & {key[0] for key in right})
    deltas, wins, losses, ties, latencies, common_tasks = [], 0, 0, 0, [], set()
    complete = bool(tasks)
    for task in tasks:
        a = [row for key, row in left.items() if key[0] == task]
        b = [row for key, row in right.items() if key[0] == task]
        if any(row["success"] is None for row in a + b):
            complete = False
            continue
        deltas.append(statistics.mean(row["success"] for row in b) - statistics.mean(row["success"] for row in a))
    for key in left.keys() & right.keys():
        a, b = left[key], right[key]
        if a["success"] is None or b["success"] is None:
            continue
        wins += b["success"] and not a["success"]
        losses += a["success"] and not b["success"]
        ties += a["success"] == b["success"]
        if a["success"] and b["success"] and number(a.get("agent_seconds"), positive=True) and number(b.get("agent_seconds"), positive=True):
            latencies.append(math.log(b["agent_seconds"] / a["agent_seconds"]))
            common_tasks.add(key[0])
    interval = None
    if complete and deltas:
        rng = random.Random(seed)
        boot = [statistics.mean(rng.choices(deltas, k=len(deltas))) for _ in range(samples)]
        interval = [quantile(boot, .025), quantile(boot, .975)]
    selected_left = [r for r in control if r["task_id"] in tasks]
    selected_right = [r for r in candidate if r["task_id"] in tasks]
    a, b = summarize(selected_left), summarize(selected_right)
    ca, cb = a["effective_cost_per_pass_usd"], b["effective_cost_per_pass_usd"]
    return {"task_count": len(tasks), "complete": complete,
            "paired_wins": wins, "paired_losses": losses, "paired_ties": ties,
            "delta": statistics.mean(deltas) if complete and deltas else None,
            "delta_percentage_points": 100 * statistics.mean(deltas) if complete and deltas else None,
            "delta_interval_95": interval, "bootstrap_samples": samples, "bootstrap_seed": seed,
            "cost_per_pass_ratio": float(Decimal(cb) / Decimal(ca)) if ca and cb and Decimal(ca) > 0 else None,
            "common_success_latency_ratio": math.exp(statistics.mean(latencies)) if latencies else None,
            "common_success_tasks": len(common_tasks), "common_success_pairs": len(latencies),
            "control": a, "candidate": b}


def run_status(rows, *, cancelled=False):
    """`cancelled` only when a human cancelled; `incomplete` names what is missing."""
    if cancelled:
        return "cancelled"
    if all(r["success"] is not None and r["evidence_complete"] for r in rows):
        return "complete"
    return "incomplete"


def build_result(manifest, rows, *, finished_at=None, wall_seconds=None, cancelled=False):
    expected = {trial["id"]: trial for trial in manifest["schedule"]}
    cells = {(t["task_id"], t["candidate_id"], t["repetition"]) for t in manifest["schedule"]}
    if len(expected) != len(manifest["schedule"]) or len(cells) != len(expected):
        raise ValueError("duplicate trial or task/candidate/repetition in schedule")
    seen = {}
    for row in rows:
        if row["id"] in seen or row["id"] not in expected:
            raise ValueError("duplicate or unexpected trial record")
        trial = expected[row["id"]]
        if any(row[k] != trial[k] for k in ("task_id", "candidate_id", "repetition", "runner")):
            raise ValueError("trial identity differs from frozen schedule")
        seen[row["id"]] = row
    ordered = [seen.get(trial["id"], empty_trial(trial)) for trial in manifest["schedule"]]
    arms = {}
    for candidate in manifest["candidates"]:
        name = candidate["id"]
        selected = [row for row in ordered if row["candidate_id"] == name]
        arms[name] = summarize(selected)
        arms[name]["suites"] = {suite: summarize([r for r in selected if r["task_id"].split("/")[0] == suite])
                               for suite in sorted({r["task_id"].split("/")[0] for r in selected})}
        arms[name]["repetitions"] = {str(rep): summarize([r for r in selected if r["repetition"] == rep])
                                     for rep in sorted({r["repetition"] for r in selected})}
    whole = summarize(ordered)
    result = {"schema_version": SCHEMA_VERSION, "run_id": manifest["run_id"],
              "manifest_sha256": value_hash(manifest), "comparison_key": manifest["comparison_key"],
              "profile": manifest["profile"], "profile_version": manifest["profile_version"],
              "status": run_status(ordered, cancelled=cancelled),
              "status_detail": {"ungraded": whole["ungraded"], "evidence_incomplete": whole["evidence_incomplete"],
                                "provider_errors": whole["provider_errors"], "cancelled_trials": whole["cancelled"]},
              "finished_at": finished_at or utc_now(), "wall_seconds": wall_seconds,
              "methodology_comparable": False, "arms": arms, "trials": ordered, "comparisons": []}
    if len(manifest["candidates"]) == 2:
        control, candidate = [c["id"] for c in manifest["candidates"]]
        result["comparisons"].append({"control_id": control, "candidate_id": candidate,
            **compare_trials([r for r in ordered if r["candidate_id"] == control],
                             [r for r in ordered if r["candidate_id"] == candidate],
                             seed=manifest["seed"], samples=manifest["promotion_policy"]["bootstrap_samples"])})
    return result


def compare_results(control, candidate, *, control_id=None, candidate_id=None, seed=0):
    control_id = control_id or next(reversed(control["arms"]))
    candidate_id = candidate_id or next(reversed(candidate["arms"]))
    return {"compatible": control["comparison_key"] == candidate["comparison_key"],
            "kind": "historical", "control_run": control["run_id"], "candidate_run": candidate["run_id"],
            **compare_trials([r for r in control["trials"] if r["candidate_id"] == control_id],
                             [r for r in candidate["trials"] if r["candidate_id"] == candidate_id], seed=seed)}


def write_report(directory, manifest, result):
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    if (directory / "result.json").exists():
        raise FileExistsError(directory / "result.json")
    methodology = "Local Frontier adaptation; no published leaderboard ranking."
    detail = result.get("status_detail") or {}
    status_line = (f"Status: **{result['status']}** ({detail.get('ungraded', 0)} ungraded, "
                   f"{detail.get('evidence_incomplete', 0)} evidence-incomplete, {detail.get('provider_errors', 0)} provider errors, "
                   f"{detail.get('cancelled_trials', 0)} cancelled). Profile: **{result['profile']}**.")
    lines = [f"# Whip evaluation: {result['run_id']}", "", status_line,
             "", methodology, "",
             "| Candidate | Verified successes / planned | Graded | Known cost (USD) | Unknown-usage calls | Agent median (s) |",
             "| --- | --- | --- | --- | --- | --- |"]
    for name, arm in result["arms"].items():
        unknown = (arm.get("tokens") or {}).get("unknown_usage_calls")
        lines.append(f"| {name} | {arm['passed']}/{arm['planned']} | {arm['graded']} | {arm['known_cost_usd']} | {'n/a' if unknown is None else unknown} | {arm['agent_median_seconds']} |")
    lines += ["", "Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.",
              "All-attempt timing includes failures; use common-success paired latency for speed comparisons.", "",
              "## Coverage and failure attribution", ""]
    for name, arm in result["arms"].items():
        lines += [f"- {name}: evidence {arm['evidence_complete']}/{arm['planned']}; accounting {arm['accounting_complete']}/{arm['planned']}; ungraded {arm['ungraded']}; provider errors {arm['provider_errors']}; termination causes {arm['termination_sources']}."]
        for suite, value in arm["suites"].items():
            lines.append(f"- {name} / {suite}: {value['passed']}/{value['planned']} verified successes.")
    for comparison in result["comparisons"]:
        lines += ["", "## Paired comparison", "",
                  f"Wins/losses/ties: {comparison['paired_wins']}/{comparison['paired_losses']}/{comparison['paired_ties']}.",
                  f"Pass-rate delta (percentage points): {comparison['delta_percentage_points']}; 95% task-cluster interval (fraction): {comparison['delta_interval_95']}.",
                  f"Cost per pass ratio: {comparison['cost_per_pass_ratio']}; common-success latency ratio: {comparison['common_success_latency_ratio']} ({comparison['common_success_tasks']} tasks)."]
    if result.get("historical_comparison"):
        c = result["historical_comparison"]
        lines += ["", f"Historical baseline comparison: {c['control_run']}; compatible: {c['compatible']}; delta: {c['delta_percentage_points']} percentage points."]
    if result["profile"] == "full" and manifest.get("external_references"):
        refs = manifest["external_references"]
        lines += ["", "## Published context", "", "Different provider/environment; no matched comparison or ranking.", ""]
        for ref in refs["scores"]:
            lines.append(f"- {ref['harness']} {ref['version']}: {ref['published_pass_rate_percent']}% on 30 tasks, one attempt each.")
        lines += ["", f"Source: [pinned Frontier results]({refs['source_url']})."]
    lines += ["", "## Trial outcomes", "", "| Task | Candidate | Repetition | Grade | Execution | Cause |", "| --- | --- | --- | --- | --- | --- |"]
    for row in result["trials"]:
        lines.append(f"| {row['task_id']} | {row['candidate_id']} | {row['repetition']} | {row['grader_status']} | {row['execution_status']} | {row['termination_source']} |")
    atomic_write(directory / "report.md", ("\n".join(lines) + "\n").encode(), exclusive=True)
    fields = ("id", "task_id", "candidate_id", "repetition", "runner", "started", "success", "grader_status", "execution_status", "termination_source", "evidence_complete", "accounting_complete", "known_cost_usd", "cost_usd", "agent_seconds", "trial_seconds", "model_calls", "unknown_usage_calls", "input_tokens", "output_tokens", "cache_tokens")
    stream = io.StringIO(newline="")
    writer = csv.DictWriter(stream, fieldnames=fields, extrasaction="ignore")
    writer.writeheader()
    writer.writerows(result["trials"])
    atomic_write(directory / "trials.csv", stream.getvalue().encode(), exclusive=True)
    # result.json is the final publication marker; projections exist first.
    write_json(directory / "result.json", result, exclusive=True)
