#!/usr/bin/env python3
"""Combine the frozen native-limits prefix and suffix without rewriting raw evidence."""
import argparse
import copy
import hashlib
import json
import math
from pathlib import Path

from summarize import main as summarize


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main(results):
    results = Path(results).resolve()
    prefix = results / "native-limits"
    suffix = results / "native-limits-v2"
    output = results / "native-limits-combined"
    recipe = json.loads((prefix / "recipe.json").read_text())
    continuation = json.loads((suffix / "recipe.json").read_text())
    assert continuation["schedule"] == recipe["schedule"][3:]
    launch = json.loads((suffix / "launch-audit.json").read_text())
    for key in launch["unchanged_execution_settings"]:
        assert recipe[key] == continuation[key], key

    receipt_path = prefix / "accounting-reconciliation.json"
    receipt = json.loads(receipt_path.read_text())
    for filename, expected in receipt["evidence_sha256"].items():
        assert sha256(Path(filename)) == expected, filename
    assert receipt["reconciled_call_accounting_complete"] is True
    assert receipt["frozen_final_database_and_state_agree"] is True

    rows = []
    sources = {}
    for directory, expected_count in ((prefix, 3), (suffix, 13)):
        path = directory / "trials.json"
        original = json.loads(path.read_text())
        assert len(original) == expected_count, (directory, len(original))
        sources[str(path)] = sha256(path)
        for record in original:
            assert record["status"] == "finished", record["name"]
            row = copy.deepcopy(record)
            row["provenance"] = {
                "raw_index_path": str(path), "raw_index_sha256": sources[str(path)],
                "raw_index": record["index"], "global_attempt": len(rows) + 1,
                "collector": directory.name,
                "timing_basis": "before observer cleanup" if directory == suffix
                    else "original observer duration; includes early cleanup",
            }
            if row["name"] == receipt["trial"]:
                assert math.isclose(row["metrics"]["ledger_cost_usd"],
                                    receipt["reconciled_ledger_cost_usd"], rel_tol=0, abs_tol=1e-12)
                assert row["agent_outcome"]["frozen_daemon_pid"]
                assert row["agent_outcome"]["latest_root_turn_status"] == "succeeded"
                row["provenance"]["reconciliation"] = {
                    "path": str(receipt_path), "sha256": sha256(receipt_path),
                    "raw_accounting_complete": record["accounting_complete"],
                    "raw_final_snapshot": record["agent_outcome"]["final_snapshot"],
                    "raw_charged_or_reserved_usd": record["charged_or_reserved_usd"],
                    "reason": "Verified frozen final database and complete call ledger; original flag was cleared by subsequent content-copy failure.",
                }
                row["accounting_complete"] = True
                row["charged_or_reserved_usd"] = receipt["reconciled_ledger_cost_usd"]
                row["agent_outcome"]["final_snapshot"] = True
                row["agent_outcome"]["content_export_complete"] = False
            scheduled = recipe["schedule"][len(rows)]
            for key, value in scheduled.items():
                assert row[key] == value, (row["name"], key)
            rows.append(row)

    assert len(rows) == len(recipe["schedule"]) == 16
    output.mkdir(exist_ok=True)
    (output / "derived-trials.json").write_text(json.dumps(rows, indent=2) + "\n")
    (output / "provenance.json").write_text(json.dumps({
        "source_trial_indexes": sources,
        "recipes": {str(p): sha256(p) for p in (prefix / "recipe.json", suffix / "recipe.json")},
        "reconciliation": {str(receipt_path): sha256(receipt_path)},
        "analysis_script_sha256": sha256(Path(__file__)),
        "summarizer_sha256": sha256(Path(__file__).with_name("summarize.py")),
        "raw_evidence_modified": False,
        "attempts_retained": 16,
        "replacements": 0,
        "provider_invoice_available": False,
        "note": "Derived accounting/finality for attempt 3 uses the hash-verified reconciliation; body-export coverage remains incomplete. All other raw flags and measured durations are preserved.",
    }, indent=2) + "\n")
    summarize(output, "derived-trials.json")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--results", default="evals/runtime-ab/results")
    main(parser.parse_args().results)
