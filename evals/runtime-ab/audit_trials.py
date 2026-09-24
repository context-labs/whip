#!/usr/bin/env python3
"""Read completed study evidence; write only a separate, derived audit report.

Usage: python3 evals/runtime-ab/audit_trials.py RESULTS_DIR --output AUDIT.json
Repeat RESULTS_DIR to include a follow-up study. No provider calls, environment
reads, raw transcript output, or writes to trial evidence are performed.
"""

import argparse
from collections import Counter, defaultdict
from datetime import datetime, timezone
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import sqlite3
from urllib.parse import urlsplit, urlunsplit

from result_evidence import ResultResolver, canonical_events, result_payload

ROOT = Path(__file__).resolve().parents[2]
DEFAULT_BUILD = Path(__file__).resolve().parent / "results" / "build"
GENERATION_KEYS = (
    "model", "provider", "effort", "reasoning_effort", "max_tokens",
    "temperature", "top_p", "engine", "execution_engine", "rlm_engine",
)


def read_json(path):
    return json.loads(path.read_text())


def decode(value):
    if isinstance(value, (str, bytes)):
        try:
            return json.loads(value)
        except (ValueError, UnicodeError):
            return None
    return value


def digest(path):
    with path.open("rb") as stream:
        sha = hashlib.sha256()
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            sha.update(chunk)
    return sha.hexdigest()


def endpoint(value):
    parsed = urlsplit(value or "")
    # Omit credentials, query and fragment even if an unexpected config has them.
    host = parsed.hostname or ""
    if parsed.port:
        host += ":" + str(parsed.port)
    return urlunsplit((parsed.scheme, host, parsed.path.rstrip("/"), "", ""))


def tool_result(value):
    return result_payload(value) or None


def accounting(calls):
    totals = Counter(model_calls=len(calls))
    routes = Counter()
    max_tokens = Counter()
    requested_max_tokens = Counter()
    purposes = Counter()
    for call in calls:
        attempt = call.get("attempt") or {}
        routes["/".join(str(attempt.get(key, "")) for key in ("Provider", "Model", "Purpose"))] += 1
        max_tokens[str(call.get("max_tokens"))] += 1
        requested_max_tokens[str(attempt.get("MaxTokens"))] += 1
        purposes[str(attempt.get("Purpose"))] += 1
        result = call.get("result") or {}
        usage = result.get("Usage") or {}
        dispatched = result.get("Dispatched", call.get("status") != "rejected")
        totals["dispatched_calls"] += bool(dispatched)
        totals["pending_calls"] += call.get("status") == "running"
        totals["unknown_usage_calls"] += bool(dispatched and call.get("usage_source") != "reported")
        totals["unknown_cost_calls"] += bool(dispatched and call.get("cost_source") not in ("reported", "estimated"))
        totals["input_tokens"] += usage.get("prompt_tokens", 0)
        totals["output_tokens"] += usage.get("completion_tokens", 0)
        totals["cache_tokens"] += (usage.get("prompt_tokens_details") or {}).get("cached_tokens", 0)
        totals["ledger_cost_micros"] += call.get("cost_micros", 0)
    return {**totals, "routes": dict(sorted(routes.items())), "purposes": dict(sorted(purposes.items())), "max_tokens_histogram": dict(sorted(max_tokens.items())), "requested_max_tokens_histogram": dict(sorted(requested_max_tokens.items()))}


def audit_trial(row, directory, recipe, descriptors):
    violations, limitations = [], []
    report = {"name": row["name"], "engine": row.get("engine"), "violations": violations, "limitations": limitations}

    def issue(code, **details):
        violations.append({"code": code, **details})

    result_path = Path(row["result_path"])
    if not result_path.is_absolute():
        result_path = directory / result_path
    # Also supports moving an archived study out of its original absolute path.
    if not result_path.exists():
        matches = list((directory / "jobs" / row["name"]).glob("*/result.json"))
        if len(matches) == 1:
            result_path = matches[0]
    agent_dir = result_path.parent / "agent"
    sources = {}

    def locate(name):
        for path in (agent_dir / name, agent_dir / "whip" / name):
            if path.is_file():
                sources[name] = {"path": str(path.resolve()), "sha256": digest(path)}
                return path
        return None

    def document(name, default=None):
        path = locate(name)
        return read_json(path) if path else default

    state = document("state.json")
    if not state:
        issue("state_snapshot_unavailable")
        report["evidence"] = sources
        return report
    config = document("configuration.json", {})
    identity = document("identity.json", {})
    metrics = document("metrics.json", row.get("metrics") or {})
    outcome = document("outcome.json", row.get("agent_outcome") or {})
    engine = row.get("engine")
    expected_model = recipe.get("model", "kimi-k3")
    expected_provider = "inference-net"
    expected_endpoint = endpoint(recipe.get("provider", "https://api.inference.net/v1"))
    expected_effort = recipe.get("reasoning_effort", "high")
    expected_max = recipe.get("max_output", 32768)
    resolved_max = expected_max
    if expected_max == 0:
        catalog = document("provider-catalog.json", {})
        resolved_max = catalog.get("max_completion_tokens")
        if not isinstance(resolved_max, int) or resolved_max <= 0:
            issue("provider_output_max_unavailable")
            resolved_max = 0
    root = state.get("root") or {}
    root_id = root.get("id")
    provider_config = (config.get("providers") or {}).get(expected_provider, {})
    model_config = (config.get("models") or {}).get(expected_model, {})
    report["identity"] = {key: identity.get(key) for key in (
        "binary_sha256", "engine", "model", "provider", "reasoning_effort", "live_provider")}
    report["identity"]["provider_endpoint"] = endpoint(identity.get("provider_endpoint"))
    report["configuration"] = {
        "default_model": config.get("defaultModel"), "default_provider": config.get("defaultProvider"),
        "default_effort": config.get("defaultEffort"), "provider_endpoint": endpoint(provider_config.get("baseUrl")),
        "model_max_output": model_config.get("maxOut"),
        "resolved_provider_output_max": resolved_max,
        "temperature": model_config.get("temperature"), "top_p": model_config.get("topP"),
        "engine": (config.get("rlm") or {}).get("defaultEngine"),
        "max_concurrent_host_calls": (config.get("rlm") or {}).get("maxConcurrentHostCalls"),
    }
    checks = {
        "identity.engine": (identity.get("engine"), engine),
        "identity.model": (identity.get("model"), expected_model),
        "identity.provider": (identity.get("provider"), expected_provider),
        "identity.provider_endpoint": (endpoint(identity.get("provider_endpoint")), expected_endpoint),
        "identity.reasoning_effort": (identity.get("reasoning_effort"), expected_effort),
        "identity.live_provider": (identity.get("live_provider"), True),
        "identity.binary_sha256": (identity.get("binary_sha256"), recipe.get("binary_sha256")),
        "config.model": (config.get("defaultModel"), expected_model),
        "config.provider": (config.get("defaultProvider"), expected_provider),
        "config.endpoint": (endpoint(provider_config.get("baseUrl")), expected_endpoint),
        "config.effort": (config.get("defaultEffort"), expected_effort),
        "config.max_output": (model_config.get("maxOut"), expected_max),
        "config.engine": ((config.get("rlm") or {}).get("defaultEngine"), engine),
        "config.host_concurrency": ((config.get("rlm") or {}).get("maxConcurrentHostCalls"), recipe.get("host_concurrency", 1)),
        "root.engine": (root.get("execution_engine"), engine),
        "root.model": (root.get("model"), expected_model),
        "root.provider": (root.get("provider"), expected_provider),
        "root.effort": (root.get("effort") or config.get("defaultEffort"), expected_effort),
    }
    for field, (actual, expected) in checks.items():
        if actual != expected:
            issue("identity_mismatch", field=field, actual=actual, expected=expected)
    for key in ("temperature", "topP", "top_p"):
        if model_config.get(key) is not None:
            issue("sampling_override_in_model_configuration", field=key, value=model_config[key])

    agents = {a["id"]: a for a in state.get("agents", [])}
    if root_id not in agents:
        issue("root_agent_metadata_missing")
    calls = state.get("calls", [])
    observed_agents = set(agents) | {c.get("agent_id") for c in calls}
    result_engines = defaultdict(Counter)
    checkpoint_warnings = Counter()
    def record_result(agent_id, text):
        result = tool_result(text)
        if result:
            observed_agents.add(agent_id)
            result_engines[agent_id][str(result.get("execution_engine"))] += 1
            if result.get("execution_engine") != engine:
                issue("result_engine_mismatch", agent_id=agent_id, actual=result.get("execution_engine"))
            if result.get("language") != {"quickjs": "javascript", "starlark": "starlark"}.get(engine):
                issue("result_language_mismatch", agent_id=agent_id)
            scratch = result.get("scratch") or {}
            if scratch.get("warning"):
                checkpoint_warnings["checkpoint_warning_cells"] += 1
            if scratch.get("skipped"):
                checkpoint_warnings["skipped_globals_cells"] += 1
            if result.get("restored"):
                checkpoint_warnings["restore_report_cells"] += 1

    host_counts, overrides = Counter(), []
    events_path = locate("events.ndjson")
    event_count, event_cursor, malformed_events = 0, 0, 0
    result_resolutions, unresolved_results = Counter(), []
    resolver = ResultResolver(agent_dir, root_id)
    if events_path:
        try:
            for event in canonical_events(events_path):
                event_count += 1
                event_cursor = max(event_cursor, event.get("seq", 0))
                payload = decode(event.get("payload_inline", event.get("payload"))) or {}
                if not isinstance(payload, dict):
                    continue
                if event.get("kind") == "stream.tool.completed" and payload.get("name") == "rlm_exec":
                    text, resolution = resolver.resolve(event)
                    result_resolutions[resolution] += 1
                    agent_id = payload.get("agent_id")
                    observed_agents.add(agent_id)
                    if tool_result(text):
                        record_result(agent_id, text)
                    else:
                        unresolved_results.append({"event_seq": event.get("seq"), "agent_id": agent_id, "resolution": resolution})
                    if resolution.endswith(("_mismatch", "_invalid")):
                        issue("cell_result_content_integrity_failure", event_seq=event.get("seq"), resolution=resolution)
                if event.get("kind") != "stream.cell.host.started":
                    continue
                name = payload.get("name", "")
                host_counts[name] += 1
                observed_agents.add(payload.get("agent_id"))
                if not name.startswith(("models.", "agents.")):
                    continue
                args = decode(payload.get("args"))
                if not isinstance(args, dict):
                    issue("host_arguments_unavailable", name=name, event_seq=event.get("seq"))
                    continue
                selected = {key: args[key] for key in GENERATION_KEYS if key in args}
                overrides.append({"event_seq": event.get("seq"), "agent_id": payload.get("agent_id"), "name": name, "generation_arguments": selected})
                for key, expected in (("model", expected_model), ("provider", expected_provider), ("effort", expected_effort), ("reasoning_effort", expected_effort), ("engine", engine), ("execution_engine", engine), ("rlm_engine", engine)):
                    if selected.get(key) not in (None, "", expected):
                        issue("host_identity_override_requested", event_seq=event.get("seq"), name=name, field=key, actual=selected[key], expected=expected)
                for key in ("temperature", "top_p"):
                    if selected.get(key) is not None:
                        issue("host_sampling_override", event_seq=event.get("seq"), name=name, field=key, value=selected[key])
                cap = selected.get("max_tokens")
                if isinstance(cap, (int, float)) and cap > resolved_max:
                    issue("host_output_cap_exceeded", event_seq=event.get("seq"), value=cap)
        except (ValueError, TypeError):
            malformed_events += 1
        finally:
            resolver.close()
    else:
        limitations.append("Host-call arguments unavailable: events.ndjson is absent.")
    if malformed_events:
        issue("malformed_events", count=malformed_events)
    if outcome.get("event_cursor") is not None and outcome["event_cursor"] != event_cursor:
        issue("event_cursor_mismatch", observed=event_cursor, final=outcome["event_cursor"])
    report["host_calls"] = {"started_by_name": dict(sorted(host_counts.items())), "model_or_agent_calls": overrides, "arguments_note": "Arguments record attempted host calls; an override request is not proof it was accepted. Actual dispatched model routes are checked in the call ledger.", "event_rows": event_count, "last_event_cursor": event_cursor}
    report["cell_results"] = {"canonical_completions": sum(result_resolutions.values()), "verified_result_v2_cells": sum(sum(counts.values()) for counts in result_engines.values()), "verified_after_event_pruning": sum(count for status, count in result_resolutions.items() if status.startswith("reference_verified_") and status.endswith("_after_pruning")), "resolution_counts": dict(result_resolutions), "unresolved": unresolved_results}
    if unresolved_results:
        limitations.append("Some canonical cell completions could not be decoded. Reference bodies are accepted only after database ownership and exact body hash/size checks; unavailable bodies remain unknown and transcripts are never counted separately.")

    def inherited_effort(agent_id, seen=None):
        seen = set() if seen is None else seen
        if agent_id in seen or agent_id not in agents:
            return None
        agent = agents[agent_id]
        if agent.get("effort"):
            return agent["effort"]
        if agent_id == root_id:
            return root.get("effort") or config.get("defaultEffort")
        return inherited_effort(agent.get("parent_id"), seen | {agent_id})

    metadata = []
    for agent_id, agent in sorted(agents.items()):
        stored_effort = agent.get("effort") or ""
        effective = inherited_effort(agent_id)
        model, provider = agent.get("model"), agent.get("provider")
        metadata.append({"id": agent_id, "parent_id": agent.get("parent_id"), "model": model, "provider": provider, "stored_effort": stored_effort, "effective_effort_inferred": effective, "root_engine_inherited": root.get("execution_engine"), "result_engines": dict(result_engines[agent_id])})
        for field, actual, expected in (("model", model, expected_model), ("provider", provider, expected_provider), ("effort", effective, expected_effort)):
            if actual != expected:
                issue("agent_identity_mismatch", agent_id=agent_id, field=field, actual=actual, expected=expected)
        if agent_id != root_id and agent.get("parent_id") not in agents:
            limitations.append("Retained agent " + agent_id + " has no retained parent metadata.")
    unknown_agents = sorted(a for a in observed_agents - set(agents) if a)
    report["agents"] = {"root_id": root_id, "retained": metadata, "observed_without_metadata": unknown_agents, "retained_descendants": len(agents) - int(root_id in agents)}
    if unknown_agents:
        limitations.append("Some observed agents have no final metadata (for example, deleted children); their effort cannot be resolved from the final snapshot.")
    if len(observed_agents - {root_id, None}) == 0:
        limitations.append("No descendant activity observed; this trial provides no empirical child-engine inheritance evidence.")
    limitations.append("Per-call wire effort/temperature/top_p are not stored. Effort is inferred from pinned configuration, retained agent metadata and recorded host arguments using the frozen-source audit; no wire-level claim is made.")

    totals = accounting(calls)
    totals["effort_by_purpose_source_inference"] = {
        purpose: ("agent effort (configured high unless a recorded override applies)" if purpose in ("turn", "final") else "omitted; provider default is unknown" if purpose in ("helper", "compaction") else "request construction not audited")
        for purpose in totals["purposes"]
    }
    if set(totals["purposes"]) - {"turn", "final"}:
        limitations.append("Auxiliary calls occurred. Frozen helper and compaction request constructors omit reasoning effort; their provider default is unknown. This is not an observed alternative effort value.")
    for call in calls:
        attempt = call.get("attempt") or {}
        if attempt.get("Model") != expected_model or attempt.get("Provider") != expected_provider:
            issue("model_route_mismatch", call_id=call.get("id"), agent_id=call.get("agent_id"), model=attempt.get("Model"), provider=attempt.get("Provider"))
        if call.get("root_id") != root_id:
            issue("model_call_root_mismatch", call_id=call.get("id"))
        if not isinstance(call.get("max_tokens"), int) or call["max_tokens"] > resolved_max:
            issue("model_call_output_cap_invalid", call_id=call.get("id"), max_tokens=call.get("max_tokens"))
        if recipe.get("native_limits") and attempt.get("Purpose") == "turn":
            if attempt.get("MaxTokens") != resolved_max or call.get("max_tokens") != resolved_max:
                issue("native_turn_output_max_changed", call_id=call.get("id"), requested=attempt.get("MaxTokens"), admitted=call.get("max_tokens"), expected=resolved_max)
    if recipe.get("native_limits"):
        if config.get("rlm") != {"defaultEngine": engine}:
            issue("native_runtime_defaults_overridden")
        root_budgets = {b["kind"]: b for b in state.get("budgets", []) if b.get("agent_id") == ""}
        for kind in ("cost", "tokens"):
            if kind not in root_budgets or root_budgets[kind].get("limit_value") is not None:
                issue("native_trial_budget_capped", kind=kind)
        command = identity.get("command") or []
        for flag in ("--max-cost", "--max-tokens", "--max-turns"):
            try:
                uncapped = float(command[command.index(flag) + 1]) == 0
            except (ValueError, IndexError, TypeError):
                uncapped = False
            if not uncapped:
                issue("native_command_cap", flag=flag)
        try:
            actual_timeout = command[command.index("--timeout") + 1]
            expected_timeout = str(recipe["agent_timeout_seconds"][row["task"]]) + "s"
            if actual_timeout != expected_timeout:
                issue("native_agent_timeout_mismatch", actual=actual_timeout, expected=expected_timeout)
        except (ValueError, IndexError, KeyError, TypeError):
            issue("native_agent_timeout_unverifiable")
    for key in ("model_calls", "routes", "input_tokens", "output_tokens", "cache_tokens", "unknown_usage_calls", "unknown_cost_calls", "pending_calls"):
        if metrics.get(key) != totals.get(key, 0):
            issue("accounting_metric_mismatch", field=key, recomputed=totals.get(key, 0), recorded=metrics.get(key))
    if "ledger_cost_usd" in metrics:
        discrepancy = abs(Decimal(str(metrics["ledger_cost_usd"])) * 1_000_000 - totals["ledger_cost_micros"])
        if discrepancy > Decimal("0.001"):
            issue("accounting_cost_mismatch", delta_micros=float(discrepancy))
    if totals.get("pending_calls") or totals.get("unknown_usage_calls") or totals.get("unknown_cost_calls"):
        issue("incomplete_model_accounting", **{key: totals.get(key, 0) for key in ("pending_calls", "unknown_usage_calls", "unknown_cost_calls")})
    if not outcome.get("final_snapshot") or outcome.get("evidence_errors"):
        issue("final_snapshot_not_confirmed", final_snapshot=outcome.get("final_snapshot"), evidence_error_count=len(outcome.get("evidence_errors") or []))
    if outcome.get("content_export_errors"):
        issue("content_body_export_incomplete", error_count=len(outcome["content_export_errors"]))
    totals["final_snapshot"] = outcome.get("final_snapshot")
    totals["state_settled"] = state.get("settled")
    totals["pending"] = state.get("pending")
    totals["observer_accounting_complete"] = row.get("accounting_complete")
    totals["output_cap_note"] = "Ledger values below the configured maximum record admitted caps. They are not evidence of a model-requested override; explicit host max_tokens arguments are listed separately."
    report["accounting"] = totals

    descriptor = descriptors.get(engine, {})
    checkpoints = []
    database = locate("sessions.db")
    images = {}
    if database:
        try:
            # Exported backups are frozen files: immutable prevents journal writes.
            with sqlite3.connect(database.resolve().as_uri() + "?mode=ro&immutable=1", uri=True) as db:
                for db_root, agent_id, envelope, image, size in db.execute("SELECT root_id,agent_id,envelope,image,bytes FROM agent_checkpoints"):
                    images[(db_root, agent_id)] = {"envelope": decode(envelope), "bytes": size, "image_bytes": len(image), "image_sha256": hashlib.sha256(image).hexdigest()}
        except sqlite3.Error as error:
            limitations.append("Checkpoint database unreadable (" + type(error).__name__ + "); BLOB verification unavailable.")
    else:
        limitations.append("Database export absent; checkpoint metadata is available but image bytes and hash cannot be independently verified.")
    for checkpoint in state.get("checkpoints", []):
        envelope = checkpoint.get("envelope") or {}
        owner = (checkpoint.get("root_id"), checkpoint.get("agent_id"))
        agent_id = owner[1]
        item = {key: envelope.get(key) for key in ("root_id", "agent_id", "engine", "build", "abi", "profile", "fidelity", "bridge_sha256", "sequence", "bytes", "sha256", "boundary")}
        item["skipped_globals_count"] = len((envelope.get("manifest") or {}).get("skipped") or [])
        item["blob_verified"] = False
        for field in ("engine", "build", "abi", "profile", "fidelity", "bridge_sha256"):
            expected = engine if field == "engine" else descriptor.get(field)
            if envelope.get(field) != expected:
                issue("checkpoint_identity_mismatch", agent_id=agent_id, field=field, actual=envelope.get(field), expected=expected)
        if owner[0] != root_id or envelope.get("root_id") != owner[0] or envelope.get("agent_id") != agent_id:
            issue("checkpoint_owner_mismatch", agent_id=agent_id)
        if checkpoint.get("bytes") != envelope.get("bytes") or not 0 < envelope.get("bytes", 0) <= 40 * 1024 * 1024:
            issue("checkpoint_size_invalid", agent_id=agent_id)
        if envelope.get("boundary") != "settled-cell" or envelope.get("format_version") != 1 or not isinstance(envelope.get("sequence"), int) or envelope["sequence"] < 1:
            issue("checkpoint_boundary_invalid", agent_id=agent_id)
        image = images.get(owner)
        if image:
            item["blob_verified"] = (image["envelope"] == envelope and image["bytes"] == envelope.get("bytes") == image["image_bytes"] and image["image_sha256"] == envelope.get("sha256"))
            if not item["blob_verified"]:
                issue("checkpoint_blob_mismatch", agent_id=agent_id)
        elif database:
            issue("checkpoint_blob_unavailable", agent_id=agent_id)
        checkpoints.append(item)
    total_bytes = sum(item.get("bytes", 0) for item in checkpoints)
    if total_bytes > 256 * 1024 * 1024:
        issue("root_checkpoint_quota_exceeded", bytes=total_bytes)
    if set(images) != {(c.get("root_id"), c.get("agent_id")) for c in state.get("checkpoints", [])} and database:
        issue("checkpoint_database_snapshot_set_mismatch")
    report["checkpoints"] = {"count": len(checkpoints), "total_bytes": total_bytes, "blob_verified_count": sum(item["blob_verified"] for item in checkpoints), "items": checkpoints, "result_warning_counts": dict(checkpoint_warnings)}
    limitations.append("Checkpoint accounting covers the retained final generation, not bytes written across all generations, deleted checkpoints, or proof that a restore occurred.")
    report["evidence"] = sources
    return report


def audit(directories, build):
    effort_path = build / "reasoning-effort-audit.json"
    effort = read_json(effort_path)
    source_hashes = read_json(build / "source-files.json")
    source_names = set(effort["source_hashes_match_frozen_binary"]) | {"internal/daemon/agent_session.go", "internal/daemon/recursive_runtime.go", "internal/llm/accounting.go", "internal/session/event.go"}
    source_matches = {name: (ROOT / name).is_file() and digest(ROOT / name) == source_hashes.get(name) for name in sorted(source_names)}
    descriptors = {engine: read_json(build / (engine + "-descriptor.json")) for engine in ("starlark", "quickjs")}
    studies = []
    for directory in directories:
        recipe = read_json(directory / "recipe.json")
        # Hash exactly the index bytes used even when an active study advances.
        index_bytes = (directory / "trials.json").read_bytes()
        rows = json.loads(index_bytes)
        completed = [row for row in rows if row.get("result_path") and row.get("status") != "running"]
        trials = [audit_trial(row, directory, recipe, descriptors) for row in completed]
        studies.append({"directory": str(directory), "recipe_sha256": digest(directory / "recipe.json"), "trials_index_sha256": hashlib.sha256(index_bytes).hexdigest(), "completed_trials_audited": len(trials), "rows_excluded_without_completed_result": len(rows) - len(completed), "trials": trials})
    all_trials = [trial for study in studies for trial in study["trials"]]
    counts = Counter(v["code"] for trial in all_trials for v in trial["violations"])
    return {
        "format_version": 1, "generated_at": datetime.now(timezone.utc).isoformat(),
        "audit_script_sha256": digest(Path(__file__)),
        "result_evidence_helper_sha256": digest(Path(__file__).with_name("result_evidence.py")),
        "event_retention_reconstruction": {"source": "internal/session/event.go", "frozen_event_retention": 10000, "criteria": "A missing positive event sequence must precede an exact 10,000-row contiguous final root event window. Reference/object metadata, unrevoked root grant, full body size and SHA-256 must still verify. These recoveries have explicit after_pruning statuses; unproven absence remains unavailable, and differing retained events remain mismatches."},
        "reasoning_effort_source_audit": {"path": str(effort_path), "sha256": digest(effort_path), "current_sources_match_frozen_manifest": source_matches, "stored_empty_effort": effort["stored_empty_effort"], "scope": "Configured high applies to ordinary root/child turns and final-answer requests. Frozen AgentSession.complete and Agent compaction requests omit effort; the provider default is unknown. Recorded auxiliary purposes are reported separately.", "engine_inheritance": "Frozen RecursiveRuntime passes the root engine to every child kernel and rejects spawn engine arguments. Per-agent result-v2 and checkpoint engine fields provide empirical corroboration when retained.", "max_tokens": "Frozen llm.runAttempt records requested MaxTokens in attempt and admitted MaxTokens in the ledger row; budget admission can shrink the output cap."},
        "summary": {"source_inference_validated_against_frozen_manifest": all(source_matches.values()), "completed_trials_audited": len(all_trials), "model_calls": sum(t.get("accounting", {}).get("model_calls", 0) for t in all_trials), "trials_with_violations": sum(bool(t["violations"]) for t in all_trials), "violation_counts": dict(sorted(counts.items())), "retained_descendants": sum(t.get("agents", {}).get("retained_descendants", 0) for t in all_trials), "checkpoint_blobs_verified": sum(t.get("checkpoints", {}).get("blob_verified_count", 0) for t in all_trials)},
        "studies": studies,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("results", type=Path, nargs="+")
    parser.add_argument("--build", type=Path, default=DEFAULT_BUILD)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    directories = [path.resolve() for path in args.results]
    output = args.output.resolve()
    for directory in directories:
        if output == directory / "recipe.json" or output == directory / "trials.json" or directory / "jobs" in output.parents:
            parser.error("--output must not overwrite raw study evidence")
    result = audit(directories, args.build.resolve())
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"output": str(output), **result["summary"]}, indent=2))


if __name__ == "__main__":
    main()
