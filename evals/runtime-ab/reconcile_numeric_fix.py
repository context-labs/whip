#!/usr/bin/env python3
"""Verify final evidence and reserve three unknown provider requests without settling it."""
from datetime import datetime, timezone
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import subprocess

from result_evidence import canonical_events, evidence_file, ResultResolver


def main():
    root = Path(__file__).resolve().parent
    directory = root / "results/native-numeric-fix"
    index = directory / "trials.json"
    rows = json.loads(index.read_text())
    assert len(rows) == 1
    row = rows[0]
    assert row["status"] == "finished"
    assert row["accounting_complete"] is False
    assert row["verifier_observed"] is True
    agent = Path(row["result_path"]).parent / "agent"
    state_path = evidence_file(agent, "state.json")
    outcome_path = evidence_file(agent, "outcome.json")
    state = json.loads(state_path.read_text())
    outcome = json.loads(outcome_path.read_text())
    assert outcome["final_snapshot"] is True
    assert outcome["daemon_stopped"] is True or outcome.get("frozen_daemon_pid", 0) > 0
    assert outcome["content_export_complete"] is True
    assert not any(state["pending"].values())
    identity_path = agent.parent / "docker-identity.json"
    identity = json.loads(identity_path.read_text())
    container = subprocess.run(
        ["docker", "container", "inspect", "--format", "{{.Id}}", identity["container_id"]],
        capture_output=True, text=True, timeout=15)
    assert container.returncode == 1 and ("No such container" in container.stderr or "No such object" in container.stderr)
    calls = state["calls"]
    assert all(c["status"] != "running" for c in calls)
    unknown = [c for c in calls if (c.get("result") or {}).get("Dispatched") and
               c.get("cost_source") not in ("reported", "estimated")]
    assert len(unknown) == 3
    assert {c["id"] for c in unknown} == {
        "23d8e141df5f1b412a8ed7572a98d67d", "ba95467e857a1604ab5c2a696aaa8143",
        "ef4955b48ce60e30255e4a87110fd8d0"}
    assert all(c["status"] == "failed" for c in unknown)
    known = Decimal(sum(c["cost_micros"] for c in calls)) / 1_000_000
    assert abs(known - Decimal(str(row["metrics"]["ledger_cost_usd"]))) < Decimal("0.000000001")
    cost_budget = next(b for b in state["budgets"] if b["kind"] == "cost" and not b["agent_id"])
    assert cost_budget["reserved_value"] == 0
    assert cost_budget["used_value"] == int(known * 1_000_000)
    assert cost_budget["uncertain_value"] == 63014322
    evidence_path = directory / "provider-failure-evidence.json"
    evidence = json.loads(evidence_path.read_text())
    expected = evidence["request_identity"]["last_message"]["content"]
    events_path = evidence_file(agent, "events.ndjson")
    resolver = ResultResolver(agent, state["root"]["id"])
    event = next(e for e in canonical_events(events_path) if e["seq"] == 1054)
    actual, _ = resolver.resolve(event)
    resolver.close()
    assert actual == expected
    maximum = Decimal(1048576)
    assert all(c["attempt"]["MaxTokens"] == int(maximum) for c in unknown)
    catalog_path = evidence_file(agent, "provider-catalog.json")
    catalog = json.loads(catalog_path.read_text())
    assert catalog["context_length"] == catalog["max_completion_tokens"] == int(maximum)
    pricing = catalog["pricing"]
    assert all(c["attempt"]["Pricing"] == pricing for c in unknown)
    bound = maximum * (Decimal(pricing["prompt"]) + Decimal(pricing["completion"]))
    assert bound == Decimal("24.90368")
    prior = Decimal("64.621382")
    reserve = Decimal(75)
    paths = [index, state_path, outcome_path, events_path, catalog_path, identity_path, Path(row["result_path"]),
             evidence_path, directory / "provider-second-failure-evidence.json",
             directory / "provider-timeout-evidence.json",
             directory / "recipe.json", Path(__file__),
             root / "PREREGISTERED-NATIVE-NUMERIC-FIX-CONTINUATION.md"]
    receipt = {
        "recorded_at": datetime.now(timezone.utc).isoformat(), "trial": row["name"],
        "raw_accounting_complete": False, "unknown_cost_calls": 3, "pending_calls": 0,
        "model_calls": len(calls), "known_ledger_cost_usd": float(known),
        "raw_uncertain_cost_usd": 63.014322, "extra_reserve_usd": 75,
        "maximum_request_uncached_cost_usd": float(bound),
        "prior_exposure_usd": float(prior),
        "continuation_prior_exposure_usd": float(prior + known + reserve),
        "raw_placeholder_usd": row["charged_or_reserved_usd"],
        "matching_tool_result_sha256": hashlib.sha256(actual.encode()).hexdigest(),
        "provider_invoice_available": False, "raw_evidence_modified": False,
        "finality": "Verified final frozen-daemon snapshot has no pending work; owned container was subsequently deleted by the runner.",
        "note": "Three failed requests have unknown local usage: HTTP 502 (zero provider observability cost) HTTP 520, and a 600-second request timeout (no completed provider record for the latter two). Carry $75 in addition to known cost; this is conservative external authorization accounting, not a settled invoice or a Whip cap.",
        "evidence_sha256": {str(p.resolve()): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths},
    }
    (directory / "continuation-accounting.json").write_text(json.dumps(receipt, indent=2) + "\n")
    print(json.dumps({k: v for k, v in receipt.items() if k != "evidence_sha256"}))


if __name__ == "__main__":
    main()
