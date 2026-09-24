#!/usr/bin/env python3
"""Record model time and execution-error evidence for the completed native study."""
import argparse
from collections import Counter
import json
from pathlib import Path

from result_evidence import ResultResolver, canonical_events, evidence_file, result_payload


def main(directory):
    directory = Path(directory)
    records = json.loads((directory / "derived-trials.json").read_text())
    analysis = json.loads((directory / "analysis.json").read_text())
    measurements = {row["name"]: row for row in analysis["trials"]}
    rows = []
    for record in records:
        agent_dir = Path(record["result_path"]).parent / "agent"
        state = json.loads(evidence_file(agent_dir, "state.json").read_text())
        errors, terminations = Counter(), Counter()
        resolver = ResultResolver(agent_dir, state["root"]["id"])
        for event in canonical_events(evidence_file(agent_dir, "events.ndjson")):
            body = event.get("payload_inline") or {}
            if event["kind"] != "stream.tool.completed" or body.get("name") != "rlm_exec":
                continue
            text, _ = resolver.resolve(event)
            payload = result_payload(text)
            if text and text.startswith("Error:"):
                errors[text.splitlines()[0][:280]] += 1
            if payload and payload.get("termination"):
                terminations[json.dumps(payload["termination"], sort_keys=True)] += 1
        resolver.close()
        measured = measurements[record["name"]]
        elapsed = sum(call.get("elapsed_millis", 0) or 0 for call in state["calls"]) / 1000
        rows.append({
            "name": record["name"], "engine": record["engine"],
            "task": record["task"], "repetition": record["repetition"],
            "native_rewards": record.get("rewards"),
            "model_call_seconds": elapsed,
            "agent_seconds": measured["duration_seconds"],
            "model_time_fraction": elapsed / measured["duration_seconds"],
            "call_purposes": dict(Counter(call["attempt"]["Purpose"] for call in state["calls"])),
            "agents": len(state["agents"]),
            "execution_error_prefix_counts": dict(errors),
            "cell_termination_counts": dict(terminations),
            "content_export_complete": record["agent_outcome"].get("content_export_complete"),
            "raw_result_path": record["result_path"],
        })
    result = {
        "note": "Recorded call times are summed; their fraction of wall time is interpretable for these serial ordinary-turn runs. Error prefixes are truncated at 280 characters. Missing content_export_complete on the original observer is not proof of complete body export.",
        "trials": rows,
    }
    (directory / "diagnostics.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"trials": len(rows), "model_call_seconds": sum(r["model_call_seconds"] for r in rows),
                      "execution_errors": sum(sum(r["execution_error_prefix_counts"].values()) for r in rows),
                      "cell_terminations": sum(sum(r["cell_termination_counts"].values()) for r in rows)}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory")
    main(parser.parse_args().directory)
