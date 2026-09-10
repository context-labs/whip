#!/usr/bin/env python3
"""Combine the corrected Anko pair, preserving the failed-call accounting caveat."""
import argparse
import copy
import hashlib
import json
import math
from pathlib import Path

from summarize import main as summarize


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main(results):
    results = Path(results).resolve()
    prefix = results / "native-numeric-fix"
    suffix = results / "native-numeric-fix-v2"
    output = results / "native-numeric-fix-combined"
    recipe = json.loads((prefix / "recipe.json").read_text())
    continuation = json.loads((suffix / "recipe.json").read_text())
    assert continuation["schedule"] == recipe["schedule"][1:]
    for key in ("binary_sha256", "model", "provider", "reasoning_effort",
                "max_tree_tokens", "max_root_turn_rounds", "max_output",
                "per_trial_cost_cap_usd", "host_concurrency", "task_filter",
                "native_limits", "agent_timeout_seconds", "timing_envelopes",
                "temperature", "top_p", "runtime_configuration", "code_hashes"):
        assert recipe[key] == continuation[key], key

    receipt_path = prefix / "continuation-accounting.json"
    receipt = json.loads(receipt_path.read_text())
    assert receipt["unknown_cost_calls"] == 3
    assert receipt["pending_calls"] == 0
    assert receipt["extra_reserve_usd"] == 75
    assert receipt["raw_accounting_complete"] is False
    for path, expected in receipt["evidence_sha256"].items():
        assert digest(Path(path)) == expected, path
    assert math.isclose(continuation["prior_exposure_usd"],
                        receipt["continuation_prior_exposure_usd"], abs_tol=1e-9)

    rows, sources = [], {}
    for directory in (prefix, suffix):
        path = directory / "trials.json"
        original = json.loads(path.read_text())
        assert len(original) == 1, (directory, len(original))
        record = original[0]
        assert record["status"] == "finished", record["name"]
        sources[str(path)] = digest(path)
        row = copy.deepcopy(record)
        row["provenance"] = {
            "raw_index_path": str(path), "raw_index_sha256": sources[str(path)],
            "raw_index": record["index"], "pair_attempt": len(rows) + 1,
            "raw_charged_or_reserved_usd": record["charged_or_reserved_usd"],
        }
        if directory == prefix:
            assert record["name"] == receipt["trial"]
            assert record["accounting_complete"] is False
            assert math.isclose(record["metrics"]["ledger_cost_usd"],
                                receipt["known_ledger_cost_usd"], abs_tol=1e-9)
            row["charged_or_reserved_usd"] = receipt["known_ledger_cost_usd"] + 75
            row["provenance"]["external_reserve_amendment"] = str(receipt_path)
            row["provenance"]["accounting_note"] = (
                "The all-remaining placeholder is replaced only in this derived "
                "external exposure field by known ledger cost plus a $75 reserve. "
                "Raw cost/usage and accounting_complete=false remain unchanged. "
                "One HTTP 502 has zero provider observability cost; an HTTP 520 and a request timeout "
                "have no completed provider record. No invoice is available.")
        for key, value in recipe["schedule"][len(rows)].items():
            assert row[key] == value, (row["name"], key)
        rows.append(row)

    assert len(rows) == len(recipe["schedule"]) == 2
    output.mkdir(exist_ok=True)
    (output / "derived-trials.json").write_text(json.dumps(rows, indent=2) + "\n")
    (output / "provenance.json").write_text(json.dumps({
        "source_trial_indexes": sources,
        "recipes": {str(p): digest(p) for p in (prefix / "recipe.json", suffix / "recipe.json")},
        "accounting_amendment": {str(receipt_path): digest(receipt_path)},
        "analysis_script_sha256": digest(Path(__file__)),
        "summarizer_sha256": digest(Path(__file__).with_name("summarize.py")),
        "raw_evidence_modified": False, "attempts_retained": 2, "replacements": 0,
        "provider_invoice_available": False,
        "note": "Separate exploratory pair after a product fix; do not pool with the original binary's 16 attempts. Three QuickJS provider calls have incomplete local usage; preserve them and the explicit external reserve.",
    }, indent=2) + "\n")
    summarize(output, "derived-trials.json")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--results", default="evals/runtime-ab/results")
    main(parser.parse_args().results)
