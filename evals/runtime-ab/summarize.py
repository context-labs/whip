#!/usr/bin/env python3
"""Derive per-trial evidence and descriptive paired summaries; never grade code."""
import argparse
import csv
import itertools
import json
import math
from pathlib import Path
import statistics

from result_evidence import ResultResolver, canonical_events, evidence_file, result_payload


def evidence(record):
    metrics = record.get("metrics") or {}
    row = {key: record.get(key) for key in ("name", "task", "runner", "engine", "repetition", "status", "success", "accounting_complete", "verifier_observed", "charged_or_reserved_usd", "exception")}
    row.update({key: metrics.get(key) for key in (
        "duration_seconds", "model_calls", "input_tokens", "output_tokens", "cache_tokens",
        "ledger_cost_usd", "reported_cost_usd", "normalized_cost_usd", "reported_cost_calls",
        "unknown_usage_calls", "unknown_cost_calls", "unknown_normalized_cost_calls", "pending_calls", "peak_input_tokens",
        "sampled_peak_container_rss_bytes", "sampled_container_cpu_seconds")})
    fallback = "runner_error" if record.get("exception") else "unknown" if record["status"] == "finished" else record["status"]
    outcome = record.get("agent_outcome") or {}
    row["outcome"] = outcome.get("status", fallback)
    final_duration = outcome.get("agent_duration_seconds", outcome.get("duration_seconds"))
    row["agent_duration_complete"] = (outcome.get("final_snapshot") is True
        and bool(outcome.get("frozen_daemon_pid") or outcome.get("daemon_stopped"))
        and isinstance(final_duration, (float, int)) and not isinstance(final_duration, bool)
        and math.isfinite(final_duration) and final_duration >= 0)
    row["metrics_duration_seconds"] = row.get("duration_seconds")
    if row["agent_duration_complete"]:
        # Final outcome timing remains authoritative even if a metrics download
        # failed and metadata still contains the last intermediate sample.
        row["duration_seconds"] = final_duration
    row["whole_trial_seconds"] = record.get("whole_trial_seconds")
    result_path = Path(record["result_path"]) if record.get("result_path") else None
    patch_path = result_path.parent / "artifacts/model.patch" if result_path else None
    row["submitted_patch_bytes"] = patch_path.stat().st_size if patch_path and patch_path.is_file() else None
    state_path = evidence_file(result_path.parent / "agent", "state.json") if result_path else None
    if state_path and state_path.exists():
        state = json.loads(state_path.read_text())
        row["agents"] = len(state["agents"])
        row["turns"] = len(state["turns"])
        row["failed_or_interrupted_turns"] = sum(turn["status"] in ("failed", "interrupted", "cancelled") for turn in state["turns"])
        row["checkpoint_bytes"] = sum(item["bytes"] for item in state["checkpoints"])
        for budget in state.get("budgets", []):
            if budget.get("agent_id") or budget["kind"] not in ("tokens", "cost"):
                continue
            for key in ("limit_value", "used_value", "reserved_value", "uncertain_value"):
                row["budget_" + budget["kind"] + "_" + key] = budget.get(key)
        root_id = state["root"]["id"]
        root_turns = [turn for turn in state["turns"] if turn["agent_id"] == root_id]
        row["latest_root_turn_status"] = root_turns[-1]["status"] if root_turns else None
        first = next((call for call in state["calls"] if call["agent_id"] == root_id and call["attempt"]["Purpose"] == "turn"), None)
        row["first_root_input_tokens"] = ((first or {}).get("result") or {}).get("Usage", {}).get("prompt_tokens")
        row["calls_at_output_cap"] = sum(
            (call.get("result") or {}).get("Usage", {}).get("completion_tokens", -1) == call.get("max_tokens")
            for call in state["calls"])
        cells, errors, warnings, invalid = 0, 0, 0, 0
        started, completed, unparsed = 0, 0, 0
        omission_notices, restore_failure_notices = 0, 0
        cell_metrics = {}
        # Events retain completed cells even if the model deletes a child and
        # its durable transcript later. Count each canonical event once.
        agent_dir = result_path.parent / "agent"
        events = list(canonical_events(evidence_file(agent_dir, "events.ndjson")))
        resolver = ResultResolver(agent_dir, root_id)
        resolutions = {}
        row["terminal_errors"] = [event["payload_inline"].get("error") for event in events
                                  if event["kind"] == "turn.failed" and event.get("payload_inline")]
        for event in events:
            body = event.get("payload_inline") or {}
            if event["kind"] == "stream.tool.started" and body.get("name") != "rlm_exec":
                invalid += 1
            if event["kind"] == "stream.tool.started" and body.get("name") == "rlm_exec":
                started += 1
            if event["kind"] != "stream.tool.completed" or body.get("name") != "rlm_exec":
                continue
            completed += 1
            text, resolution = resolver.resolve(event)
            resolutions[resolution] = resolutions.get(resolution, 0) + 1
            payload = result_payload(text)
            if payload:
                cells += 1
                if payload["execution_engine"] != record["engine"]:
                    raise ValueError("mixed-engine result: " + str(state_path))
                errors += bool(payload.get("termination") or text.startswith("Error:"))
                warnings += bool((payload.get("scratch") or {}).get("warning"))
                scratch, restored = payload.get("scratch") or {}, payload.get("restored") or {}
                omission_notices += len(scratch.get("skipped") or []) + (scratch.get("skipped_omitted") or 0)
                restore_failure_notices += len(restored.get("failed") or []) + (restored.get("failed_omitted") or 0)
                for name, value in payload.get("metrics", {}).items():
                    cell_metrics[name] = cell_metrics.get(name, 0) + value
            else:
                unparsed += 1
        resolver.close()
        row.update(cells=cells, cell_errors=errors, checkpoint_warnings=warnings, invalid_tool_calls=invalid)
        row.update(rlm_calls_started=started, rlm_calls_completed=completed, unparsed_rlm_results=unparsed)
        row.update(referenced_rlm_results=sum(count for status, count in resolutions.items() if status not in ("inline", "missing_result")),
                   resolved_referenced_rlm_results=sum(count for status, count in resolutions.items() if status.startswith("reference_verified_")),
                   unresolved_referenced_rlm_results=sum(count for status, count in resolutions.items() if status not in ("inline", "missing_result") and not status.startswith("reference_verified_")),
                   rlm_result_resolution_counts=resolutions)
        row.update(checkpoint_omission_notices=omission_notices, restore_failure_notices=restore_failure_notices)
        row.update({"cell_" + key: value for key, value in cell_metrics.items()})
    return row


def arm_summary(arm):
    finished = [row for row in arm if row["status"] != "running"]
    passed = [row for row in finished if row["success"] is True]
    result = {
        "trials": len(arm), "finished_trials": len(finished), "passes": len(passed),
        "verifier_observed": sum(row.get("verifier_observed") is True for row in finished),
        "accounting_complete": bool(arm) and all(row.get("accounting_complete") for row in arm),
        "accounting_complete_trials": sum(row.get("accounting_complete") is True for row in finished),
        "observed_ledger_cost_usd": sum(row.get("ledger_cost_usd") or 0 for row in finished),
        "observed_normalized_cost_usd": sum(row.get("normalized_cost_usd") or 0 for row in finished),
        "charged_or_reserved_usd": sum(row.get("charged_or_reserved_usd") or 0 for row in arm),
    }
    for key in ("duration_seconds", "whole_trial_seconds", "ledger_cost_usd", "input_tokens", "output_tokens",
                "model_calls", "cells", "checkpoint_bytes", "sampled_peak_container_rss_bytes"):
        values = [row[key] for row in finished if row.get(key) is not None]
        result["median_" + key] = statistics.median(values) if values else None
    for key in ("cell_errors", "checkpoint_warnings", "checkpoint_omission_notices", "restore_failure_notices", "unknown_usage_calls", "unknown_cost_calls",
                "unknown_normalized_cost_calls", "reported_cost_calls", "calls_at_output_cap", "invalid_tool_calls",
                "rlm_calls_started", "rlm_calls_completed", "unparsed_rlm_results", "failed_or_interrupted_turns",
                "referenced_rlm_results", "resolved_referenced_rlm_results", "unresolved_referenced_rlm_results",
                "input_tokens", "output_tokens", "cache_tokens", "model_calls", "cells"):
        # These are observed totals. Accounting completeness and missing evidence
        # are reported independently; absence never establishes a zero-cost call.
        result["observed_" + key] = sum(row.get(key) or 0 for row in finished)
    result["successful_trials"] = {"count": len(passed)}
    for key in ("duration_seconds", "ledger_cost_usd", "normalized_cost_usd"):
        values = [row[key] for row in passed if row.get(key) is not None]
        result["successful_trials"]["median_" + key] = statistics.median(values) if values else None
        result["successful_trials"]["observed_total_" + key] = sum(values) if values else None
    result["successful_trials"]["accounting_complete"] = bool(passed) and all(row.get("accounting_complete") for row in passed)
    for group_name, group in (("all", finished), ("successful", passed)):
        costs = [row["ledger_cost_usd"] for row in group if row.get("accounting_complete") and row.get("ledger_cost_usd") is not None]
        result[group_name + "_complete_accounting_cost"] = {
            "count": len(costs), "median_usd": statistics.median(costs) if costs else None,
            "total_usd": sum(costs) if costs else None}
        durations = [row["duration_seconds"] for row in group if row.get("agent_duration_complete") and row.get("duration_seconds") is not None]
        result[group_name + "_complete_agent_duration"] = {
            "count": len(durations), "median_seconds": statistics.median(durations) if durations else None,
            "total_seconds": sum(durations) if durations else None}
    return result


def cluster_intervals(pairs):
    groups = {}
    for pair in pairs:
        if pair["starlark_pass"] is None or pair["quickjs_pass"] is None:
            continue
        groups.setdefault(pair["task"], []).append(pair)
    if not groups:
        return {}
    result = {"method": "Exact percentile bootstrap of whole task clusters, preserving both engines and repetitions; 95% intervals. Very small selected task sample.",
              "task_clusters": len(groups)}
    if len(groups) > 6:
        result["omitted"] = "Exact enumeration is limited to six task clusters."
        return result
    for key in ("pass", "duration_seconds", "ledger_cost_usd"):
        clusters = []
        for group in groups.values():
            values = [(float(p["quickjs_pass"]) - float(p["starlark_pass"])) if key == "pass"
                      else p.get("quickjs_minus_starlark_" + key) for p in group]
            if any(value is None for value in values):
                break
            clusters.append(values)
        if len(clusters) != len(groups):
            continue
        means = sorted(statistics.mean(value for cluster in sample for value in cluster)
                       for sample in itertools.product(clusters, repeat=len(clusters)))
        def percentile(p):
            position = (len(means) - 1) * p
            low = int(position)
            high = min(low + 1, len(means) - 1)
            return means[low] + (means[high] - means[low]) * (position - low)
        result["quickjs_minus_starlark_" + key] = {
            "mean": statistics.mean(value for cluster in clusters for value in cluster),
            "low": percentile(.025), "high": percentile(.975), "resamples": len(means)}
    return result


def main(directory, records_name="trials.json"):
    root = Path(directory)
    records = json.loads((root / records_name).read_text())
    rows = [evidence(record) for record in records]
    summary = {}
    for engine in ("starlark", "quickjs"):
        arm = [row for row in rows if row["engine"] == engine]
        summary[engine] = arm_summary(arm)
    grouped = {}
    for row in rows:
        grouped.setdefault((row["task"], row["repetition"]), {})[row["engine"]] = row
    pairs = []
    for (task, repetition), pair in grouped.items():
        if len(pair) != 2 or any(row["status"] == "running" for row in pair.values()):
            continue
        star, js = pair["starlark"], pair["quickjs"]
        value = {"task": task, "repetition": repetition,
                 "starlark_pass": star["success"], "quickjs_pass": js["success"]}
        for key in ("duration_seconds", "ledger_cost_usd", "input_tokens", "output_tokens"):
            if key == "duration_seconds" and not (star.get("agent_duration_complete") and js.get("agent_duration_complete")):
                continue
            if key != "duration_seconds" and not (star.get("accounting_complete") and js.get("accounting_complete")):
                continue
            if key in ("input_tokens", "output_tokens") and any(
                row.get("unknown_usage_calls") != 0 or row.get("pending_calls") != 0 for row in (star, js)
            ):
                continue
            if star.get(key) is not None and js.get(key) is not None:
                value["quickjs_minus_starlark_" + key] = js[key] - star[key]
        pairs.append(value)
    suites = {runner: {engine: arm_summary([row for row in rows if row["runner"] == runner and row["engine"] == engine])
                       for engine in ("starlark", "quickjs")} for runner in sorted({row["runner"] for row in rows})}
    result = {"summary": summary, "suites": suites, "pairs": pairs, "trials": rows,
              "cluster_intervals": cluster_intervals(pairs),
              "inference_limit": "Descriptive paired subset; repetitions share tasks and are not independent new tasks."}
    (root / "analysis.json").write_text(json.dumps(result, indent=2) + "\n")
    if rows:
        keys = list(dict.fromkeys(key for row in rows for key in row))
        with (root / "trials.csv").open("w", newline="") as output:
            writer = csv.DictWriter(output, fieldnames=keys)
            writer.writeheader()
            writer.writerows(rows)
    print(json.dumps(summary, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("directory")
    parser.add_argument("--records", default="trials.json", help="explicit recovered record file, if needed; raw records stay unchanged")
    args = parser.parse_args()
    main(args.directory, args.records)
