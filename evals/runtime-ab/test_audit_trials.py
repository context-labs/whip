"""Synthetic evidence checks; never launches a daemon or provider request."""

import hashlib
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

from audit_trials import accounting, audit_trial, digest, endpoint, tool_result


class TrialAuditTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.agent = self.directory / "jobs" / "trial" / "task__id" / "agent"
        self.agent.mkdir(parents=True)
        self.result = self.agent.parent / "result.json"
        self.result.write_text("{}")
        self.recipe = {"model": "kimi-k3", "provider": "https://api.inference.net/v1", "reasoning_effort": "high", "max_output": 32768, "host_concurrency": 1, "binary_sha256": "frozen"}
        self.descriptor = {"build": "build", "abi": "abi", "profile": "settled-v1", "fidelity": "whole-image", "bridge_sha256": "bridge"}
        self.row = {"name": "trial", "engine": "quickjs", "result_path": str(self.result), "accounting_complete": True}
        identity = {"binary_sha256": "frozen", "engine": "quickjs", "model": "kimi-k3", "provider": "inference-net", "provider_endpoint": "https://api.inference.net/v1", "reasoning_effort": "high", "live_provider": True}
        config = {"defaultModel": "kimi-k3", "defaultProvider": "inference-net", "defaultEffort": "high", "providers": {"inference-net": {"baseUrl": "https://api.inference.net/v1", "apiKey": "SECRET_SENTINEL"}}, "models": {"kimi-k3": {"maxOut": 32768}}, "rlm": {"defaultEngine": "quickjs", "maxConcurrentHostCalls": 1}}
        self.image = b"checkpoint image"
        self.envelope = {**self.descriptor, "engine": "quickjs", "root_id": "root", "agent_id": "child", "sequence": 1, "bytes": len(self.image), "sha256": hashlib.sha256(self.image).hexdigest(), "boundary": "settled-cell", "format_version": 1, "manifest": {"saved": ["SECRET_SENTINEL"]}}
        call = {"id": "call", "root_id": "root", "agent_id": "child", "attempt": {"Model": "kimi-k3", "Provider": "inference-net", "Purpose": "turn", "MaxTokens": 32768}, "max_tokens": 10000, "status": "succeeded", "result": {"Dispatched": True, "Usage": {"prompt_tokens": 10, "completion_tokens": 2, "prompt_tokens_details": {"cached_tokens": 3}}}, "usage_source": "reported", "cost_source": "estimated", "cost_micros": 5}
        self.state = {"root": {"id": "root", "execution_engine": "quickjs", "model": "kimi-k3", "provider": "inference-net", "effort": ""}, "agents": [{"id": "root", "model": "kimi-k3", "provider": "inference-net", "effort": ""}, {"id": "child", "parent_id": "root", "model": "kimi-k3", "provider": "inference-net", "effort": ""}], "calls": [call], "transcripts": [{"agent_id": "child", "content": {"role": "tool", "content": json.dumps({"format_version": 2, "execution_engine": "quickjs", "language": "javascript", "has_value": True, "value": "SECRET_SENTINEL"})}}], "checkpoints": [{"root_id": "root", "agent_id": "child", "envelope": self.envelope, "bytes": len(self.image)}], "settled": True, "pending": {"pending_model_calls": 0}}
        totals = accounting(self.state["calls"])
        metrics = {**totals, "ledger_cost_usd": 0.000005}
        for name, document in (("identity.json", identity), ("configuration.json", config), ("metrics.json", metrics), ("outcome.json", {"final_snapshot": True, "event_cursor": 2})):
            self.write(name, document)
        event = {"root_id": "root", "seq": 1, "kind": "stream.cell.host.started", "payload_inline": {"agent_id": "root", "name": "agents.spawn", "args": json.dumps({"effort": "high", "prompt": "SECRET_SENTINEL"})}}
        self.completed = {"root_id": "root", "seq": 2, "kind": "stream.tool.completed", "payload_inline": {"agent_id": "child", "name": "rlm_exec", "result": self.state["transcripts"][0]["content"]["content"]}}
        (self.agent / "events.ndjson").write_text("\n".join(json.dumps(value) for value in (event, self.completed, self.completed)) + "\n")
        with sqlite3.connect(self.agent / "sessions.db") as db:
            db.execute("CREATE TABLE agent_checkpoints (root_id,agent_id,envelope,image,bytes)")
            db.execute("INSERT INTO agent_checkpoints VALUES (?,?,?,?,?)", ("root", "child", json.dumps(self.envelope), self.image, len(self.image)))

    def write(self, name, document):
        (self.agent / name).write_text(json.dumps(document))

    def run_audit(self):
        self.write("state.json", self.state)
        return audit_trial(self.row, self.directory, self.recipe, {"quickjs": self.descriptor})

    def test_identity_inheritance_and_images_without_raw_output_or_mutation(self):
        self.write("state.json", self.state)
        before = {path.name: digest(path) for path in self.agent.iterdir()}
        report = audit_trial(self.row, self.directory, self.recipe, {"quickjs": self.descriptor})
        self.assertEqual(report["violations"], [])
        child = next(a for a in report["agents"]["retained"] if a["id"] == "child")
        self.assertEqual(child["effective_effort_inferred"], "high")
        self.assertEqual(child["result_engines"], {"quickjs": 1})
        self.assertEqual(report["cell_results"]["canonical_completions"], 1)
        self.assertEqual(report["checkpoints"]["blob_verified_count"], 1)
        self.assertEqual(report["accounting"]["requested_max_tokens_histogram"], {"32768": 1})
        self.assertEqual(report["accounting"]["max_tokens_histogram"], {"10000": 1})
        self.assertNotIn("SECRET_SENTINEL", json.dumps(report))
        self.assertEqual(before, {path.name: digest(path) for path in self.agent.iterdir()})

    def test_route_effort_and_checkpoint_corruption_are_detected(self):
        self.state["calls"][0]["attempt"]["Model"] = "other-model"
        self.state["agents"][1]["effort"] = "low"
        with sqlite3.connect(self.agent / "sessions.db") as db:
            db.execute("UPDATE agent_checkpoints SET image=?", (b"corrupt",))
        report = self.run_audit()
        codes = {issue["code"] for issue in report["violations"]}
        self.assertTrue({"agent_identity_mismatch", "model_route_mismatch", "checkpoint_blob_mismatch", "accounting_metric_mismatch"} <= codes)

    def test_zero_output_override_uses_catalog_and_native_budgets_are_audited(self):
        self.recipe.update(max_output=0, host_concurrency=None, native_limits=True,
                           agent_timeout_seconds={"fixture": 5400})
        self.row["task"] = "fixture"
        config = json.loads((self.agent / "configuration.json").read_text())
        config["models"]["kimi-k3"]["maxOut"] = 0
        config["rlm"] = {"defaultEngine": "quickjs"}
        self.write("configuration.json", config)
        self.write("provider-catalog.json", {"max_completion_tokens": 1048576})
        identity = json.loads((self.agent / "identity.json").read_text())
        identity["command"] = ["whip", "run", "--max-cost", "0", "--max-tokens", "0",
                               "--max-turns", "0", "--timeout", "5400s"]
        self.write("identity.json", identity)
        self.state["budgets"] = [{"agent_id": "", "kind": k, "limit_value": None} for k in ("cost", "tokens")]
        self.state["calls"][0]["attempt"]["MaxTokens"] = 1048576
        self.state["calls"][0]["max_tokens"] = 1048576
        self.assertEqual(self.run_audit()["violations"], [])
        self.state["budgets"][0]["limit_value"] = 2500000
        self.state["calls"][0]["max_tokens"] = 32768
        self.assertTrue({"native_trial_budget_capped", "native_turn_output_max_changed"} <=
                        {v["code"] for v in self.run_audit()["violations"]})

    def test_missing_export_and_deleted_child_remain_explicit_limits(self):
        self.state["agents"] = self.state["agents"][:1]
        (self.agent / "sessions.db").unlink()
        (self.agent / "outcome.json").unlink()
        report = self.run_audit()
        self.assertEqual(report["agents"]["observed_without_metadata"], ["child"])
        self.assertEqual(report["checkpoints"]["blob_verified_count"], 0)
        self.assertIn("final_snapshot_not_confirmed", {v["code"] for v in report["violations"]})
        self.assertTrue(any("deleted children" in item for item in report["limitations"]))
        self.assertTrue(any("Database export absent" in item for item in report["limitations"]))

    def test_auxiliary_effort_is_unknown_not_an_observed_mismatch(self):
        self.state["calls"][0]["attempt"]["Purpose"] = "helper"
        self.write("metrics.json", {**accounting(self.state["calls"]), "ledger_cost_usd": 0.000005})
        report = self.run_audit()
        self.assertEqual(report["violations"], [])
        self.assertEqual(report["accounting"]["effort_by_purpose_source_inference"]["helper"], "omitted; provider default is unknown")
        self.assertTrue(any("Auxiliary calls occurred" in item for item in report["limitations"]))

    def test_arguments_show_requested_overrides_without_prompt_content(self):
        event = {"root_id": "root", "seq": 1, "kind": "stream.cell.host.started", "payload_inline": {"agent_id": "root", "name": "agents.spawn", "args": json.dumps({"effort": "low", "temperature": 0.5, "max_tokens": 40000, "prompt": "SECRET_SENTINEL"})}}
        (self.agent / "events.ndjson").write_text(json.dumps(event) + "\n" + json.dumps(self.completed) + "\n")
        report = self.run_audit()
        self.assertTrue({"host_identity_override_requested", "host_sampling_override", "host_output_cap_exceeded"} <= {v["code"] for v in report["violations"]})
        self.assertNotIn("SECRET_SENTINEL", json.dumps(report))

    def test_error_prefixed_result_and_endpoint_redaction(self):
        result = {"format_version": 2, "execution_engine": "quickjs"}
        self.assertEqual(tool_result("Error: failed\n" + json.dumps(result)), result)
        self.assertIsNone(tool_result("Error: failed"))
        self.assertEqual(endpoint("https://user:secret@api.inference.net/v1?key=secret#secret"), "https://api.inference.net/v1")


if __name__ == "__main__":
    unittest.main()
