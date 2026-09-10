"""Offline normalization, paired statistics, and one JSON/Markdown/CSV projection."""
from collections import Counter, defaultdict
import csv
from decimal import Decimal
from datetime import datetime
import io
import json
import math
from pathlib import Path
import random
import sqlite3
import statistics

from .common import (SCHEMA_VERSION, atomic_write, file_hash, inside, number,
                     read_json, utc_now, value_hash, write_json)
from .observe import aggregate, final_accounting_complete, rows as database_rows
from .result_evidence import canonical_events, evidence_file, ResultResolver, result_payload


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
        "input_tokens": None, "output_tokens": None, "cache_tokens": None,
        "cleanup_complete": None, "error_codes": [], "artifacts": {}, "phase_seconds": {}}


def termination(outcome, raw, diagnostic):
    if raw.get("cancelled"):
        return raw.get("cancellation_source", "user_cancelled")
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
        return "provider_error" if any(s in lower for s in ("429", "502", "503", "520")) else "agent_error"
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


def verify_snapshot(path, state, content):
    root_id = state.get("root", {}).get("id")
    if not path or not root_id or content.get("root_id") != root_id:
        return False
    try:
        with sqlite3.connect(path.resolve().as_uri() + "?mode=ro&immutable=1", uri=True) as database:
            database.row_factory = sqlite3.Row
            if [r[0] for r in database.execute("SELECT id FROM sessions")] != [root_id]:
                return False
            calls = database_rows(database, "SELECT * FROM model_calls WHERE root_id=? ORDER BY rowid", (root_id,))
            if calls != state.get("calls"):
                return False
            bodies = database.execute("SELECT DISTINCT r.digest,r.size FROM content_references r JOIN content_objects o ON o.digest=r.digest JOIN content_grants g ON g.reference_id=r.id WHERE g.root_id=?", (root_id,)).fetchall()
            expected = {(r[0], r[1]) for r in bodies}
            actual = {(r["digest"], r["bytes"]) for r in content.get("bodies", [])}
            return expected == actual and len(actual) == len(content.get("bodies", []))
    except (sqlite3.Error, KeyError, TypeError):
        return False


def normalize_trial(trial, raw, artifact_root):
    artifact_root = Path(artifact_root).resolve()
    row = empty_trial(trial)
    row.update(started=bool(raw.get("started")), trial_seconds=raw.get("trial_seconds"),
               cleanup_complete=raw.get("cleanup", {}).get("complete", False),
               phase_seconds=raw.get("phase_seconds", {}))
    row["execution_status"] = "cancelled" if raw.get("cancelled") else "runner_error"
    row["termination_source"] = termination({}, raw, "") or "setup_error"
    if raw.get("error_code"):
        row["error_codes"].append(raw["error_code"])
    results = list(inside(artifact_root, raw["job_path"]).glob("*/result.json")) if raw.get("job_path") else []
    if len(results) != 1:
        row["error_codes"].append("missing_result" if not results else "multiple_results")
        return row
    native_path = results[0]
    native = read_json(native_path)
    row["phase_seconds"].update({key: phase_duration(native.get(key)) for key in
        ("environment_setup", "agent_setup", "agent_execution", "verifier")})
    context = native.get("agent_result") or {}
    metadata = context.get("metadata") or {}
    metrics = metadata.get("whip") or {}
    outcome = metadata.get("whip_outcome") or {}
    agent = native_path.parent / "agent"
    errors = ["adapter_export_error"] if metadata.get("whip_evidence_errors") else []
    required = ("state.json", "metrics.json", "outcome.json", "sessions.db", "identity.json",
                "content-export.json", "configuration.json", "provider-catalog.json", "events.ndjson",
                "cli.ndjson", "cli.stderr")
    paths = {name: evidence_file(agent, name) for name in required}
    if any(path is None for path in paths.values()):
        errors.append("missing_required_evidence")
    if paths["outcome.json"]:
        outcome = read_json(paths["outcome.json"])
    if paths["metrics.json"]:
        metrics = read_json(paths["metrics.json"])
    state = read_json(paths["state.json"]) if paths["state.json"] else {}
    content = read_json(paths["content-export.json"]) if paths["content-export.json"] else {}
    if not outcome.get("content_export_complete") or content.get("errors"):
        errors.append("incomplete_content_export")
    if not verify_snapshot(paths["sessions.db"], state, content):
        errors.append("snapshot_inconsistent")
    for body in content.get("bodies", []):
        digest = body.get("digest", "")
        if len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
            errors.append("invalid_content_identity")
            continue
        content_path = paths["content-export.json"].parent / "content" / "sha256" / digest
        if not content_path.is_file() or content_path.stat().st_size != body["bytes"] or file_hash(content_path) != digest:
            errors.append("content_digest_mismatch")
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
    if paths["cli.ndjson"]:
        for line in paths["cli.ndjson"].read_text(errors="replace").splitlines():
            try:
                message = json.loads(line)
            except ValueError:
                continue
            if isinstance(message, dict) and message.get("type") == "error" and isinstance(message.get("error"), str):
                diagnostic += "\n" + message["error"]
    row.update(execution_status=outcome.get("status", row["execution_status"]),
               termination_source=termination(outcome, raw, diagnostic))
    if row["termination_source"] is None and row["grader_status"] == "error":
        row["termination_source"] = "verifier_error"
    if outcome.get("final_snapshot") is not True or outcome.get("evidence_errors"):
        errors.append("incomplete_final_snapshot")
    duration = outcome.get("agent_duration_seconds")
    duration_complete = (outcome.get("final_snapshot") is True and
                         bool(outcome.get("frozen_daemon_pid") or outcome.get("daemon_stopped")))
    if not duration_complete or not state.get("settled") or any((state.get("pending") or {}).values()):
        errors.append("incomplete_finality")
    row["agent_seconds"] = duration if number(duration) and duration_complete else None
    calls = state.get("calls")
    computed = aggregate(calls) if calls is not None else {}
    matching = calls is not None and all(computed.get(key) == metrics.get(key) for key in
        ("model_calls", "unknown_cost_calls", "unknown_usage_calls", "pending_calls", "input_tokens", "output_tokens", "cache_tokens"))
    if not matching:
        errors.append("accounting_snapshot_mismatch")
    row["accounting_complete"] = bool(metadata.get("whip_accounting_complete") is True
                                       and final_accounting_complete(computed, outcome)
                                       and computed.get("unknown_usage_calls") == 0
                                       and matching and not errors)
    if calls is not None:
        row["known_cost_usd"] = money(sum(Decimal(call["cost_micros"]) for call in calls) / 1_000_000)
    elif number(metrics.get("ledger_cost_usd")):
        row["known_cost_usd"] = money(metrics["ledger_cost_usd"])
    if row["accounting_complete"]:
        row["cost_usd"] = row["known_cost_usd"]
    row["unknown_cost_calls"] = computed.get("unknown_cost_calls", metrics.get("unknown_cost_calls"))
    row["unknown_usage_calls"] = computed.get("unknown_usage_calls", metrics.get("unknown_usage_calls"))
    row["model_calls"] = computed.get("model_calls", metrics.get("model_calls"))
    usage_complete = (row["accounting_complete"] and metrics.get("unknown_usage_calls") == 0)
    for key in ("input_tokens", "output_tokens", "cache_tokens"):
        row[key] = metrics.get(key) if usage_complete else None
    row["cost_sources"] = dict(Counter(call.get("cost_source", "unknown") for call in calls or []))
    row["calls_by_purpose"] = dict(Counter(call.get("attempt", {}).get("Purpose", "unknown") for call in calls or []))
    row["provider_statuses"] = None  # The durable ModelAttemptResult does not retain HTTP status codes.
    row["provider_status_coverage"] = "unavailable"
    row["diagnostics"] = {key: metrics.get(key) for key in (
        "peak_input_tokens", "sampled_peak_container_rss_bytes", "sampled_container_cpu_seconds",
        "reported_cost_usd", "reported_cost_calls", "normalized_cost_usd", "unknown_normalized_cost_calls")}
    row["diagnostics"].update(agent_count=len(state.get("agents", [])), turn_count=len(state.get("turns", [])))
    resolver = ResultResolver(agent, state.get("root", {}).get("id"))
    cells, cell_errors, unresolved = 0, 0, 0
    try:
        for event in canonical_events(paths["events.ndjson"]):
            payload = event.get("payload_inline") or {}
            if event.get("kind") == "stream.tool.completed" and payload.get("name") == "rlm_exec":
                text, resolution = resolver.resolve(event)
                result = result_payload(text)
                if result:
                    cells += 1
                    cell_errors += bool(result.get("termination"))
                else:
                    unresolved += 1
    finally:
        resolver.close()
    row["diagnostics"].update(cells=cells, cell_errors=cell_errors, unresolved_cells=unresolved)
    row["definition_sha256"] = value_hash([a.get("definition") for a in state.get("agents", [])]) if state else None
    row["controls"] = [{key: budget.get(key) for key in ("agent_id", "kind", "limit_value", "used_value", "uncertain_value")}
                       for budget in state.get("budgets", [])]
    row["evidence_complete"] = not errors and row["cleanup_complete"] is True
    row["error_codes"].extend(sorted(set(errors)))
    row["artifacts"] = {"native_result": native_path.relative_to(artifact_root).as_posix(),
                        "native_result_sha256": file_hash(native_path),
                        "evidence_files": {p.relative_to(artifact_root).as_posix(): file_hash(p)
                                           for p in paths.values() if p is not None}}
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
              for key in ("input_tokens", "output_tokens", "cache_tokens", "unknown_cost_calls", "model_calls")}
    return {
        "planned": n, "started": sum(row["started"] for row in rows), "graded": graded,
        "passed": passed, "failed": graded - passed, "ungraded": n - graded,
        "cancelled": sum(row["termination_source"] == "user_cancelled" for row in rows),
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


def build_result(manifest, rows, *, finished_at=None, wall_seconds=None):
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
    result = {"schema_version": SCHEMA_VERSION, "run_id": manifest["run_id"],
              "manifest_sha256": value_hash(manifest), "comparison_key": manifest["comparison_key"],
              "profile": manifest["profile"], "profile_version": manifest["profile_version"],
              "status": "complete" if all(r["success"] is not None and r["evidence_complete"] for r in ordered) else "partial",
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
    lines = [f"# Whip evaluation: {result['run_id']}", "", f"Status: **{result['status']}**. Profile: **{result['profile']}**.",
             "", "Local Frontier adaptation; no published leaderboard ranking.", "",
             "| Candidate | Verified successes / planned | Graded | Known cost (USD) | Complete cost (USD) | Agent median (s) |",
             "| --- | --- | --- | --- | --- | --- |"]
    for name, arm in result["arms"].items():
        lines.append(f"| {name} | {arm['passed']}/{arm['planned']} | {arm['graded']} | {arm['known_cost_usd']} | {arm['cost_usd'] or 'unknown'} | {arm['agent_median_seconds']} |")
    lines += ["", "Incomplete runs show confirmed successes against the entire planned denominator, not an exclusion-adjusted score.",
              "All-attempt timing includes failures; use common-success paired latency for speed comparisons.", "",
              "## Coverage and failure attribution", ""]
    for name, arm in result["arms"].items():
        lines += [f"- {name}: evidence {arm['evidence_complete']}/{arm['planned']}; accounting {arm['accounting_complete']}/{arm['planned']}; ungraded {arm['ungraded']}; termination causes {arm['termination_sources']}."]
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
    fields = ("id", "task_id", "candidate_id", "repetition", "runner", "started", "success", "grader_status", "execution_status", "termination_source", "evidence_complete", "accounting_complete", "known_cost_usd", "cost_usd", "agent_seconds", "trial_seconds", "input_tokens", "output_tokens", "cache_tokens")
    stream = io.StringIO(newline="")
    writer = csv.DictWriter(stream, fieldnames=fields, extrasaction="ignore")
    writer.writeheader()
    writer.writerows(result["trials"])
    atomic_write(directory / "trials.csv", stream.getvalue().encode(), exclusive=True)
    # result.json is the final publication marker; projections exist first.
    write_json(directory / "result.json", result, exclusive=True)
