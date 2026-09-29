"""Native ledger accounting and optional real Linux CLI/container acceptance."""
import contextlib
import hashlib
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from whip_evals.observe import (aggregate, configure_catalog, export_content_bodies,
                                final_accounting_complete, quiet_start, run,
                                signal_daemon, snapshot, write_config)


def attempt(*, state="succeeded", dispatched=1, cost=0, usage=None):
    return {"state": state, "dispatched_at": dispatched, "cost_nano_usd": cost,
            "cost_source": "unknown" if cost is None else "provider",
            "result": {"usage": usage or {}, "reported_cost_nano_usd": cost}}


class AccountingTests(unittest.TestCase):
    def test_independent_usage_presence_and_exact_money(self):
        known = attempt(cost=9007199254740993, usage={"input": 100, "output": 7, "cached_input": 0})
        unknown = attempt(state="uncertain", cost=None, usage={"input": 12})
        total = aggregate([known, unknown])
        self.assertEqual(total["ledger_cost_nano_usd"], 9007199254740993)
        self.assertEqual(total["input_tokens"], 112)
        self.assertEqual(total["output_tokens"], 7)
        self.assertEqual(total["unknown_usage_calls"], 1)
        self.assertEqual(total["unknown_cache_calls"], 1)
        self.assertEqual(total["unknown_cost_calls"], 1)

    def test_undispatched_and_reserved_are_distinct_from_unknown_spend(self):
        cancelled = attempt(state="cancelled", dispatched=None)
        reserved = attempt(state="reserved", dispatched=None, cost=None)
        total = aggregate([cancelled, reserved])
        self.assertEqual(total["dispatched_calls"], 0)
        self.assertEqual(total["unknown_cost_calls"], 0)
        self.assertEqual(total["unknown_usage_calls"], 0)
        self.assertEqual(total["pending_calls"], 1)

    def test_free_is_known_but_missing_usage_is_not_zero(self):
        metrics = aggregate([attempt()])
        outcome = {"final_snapshot": True, "daemon_stopped": True, "pending": {}}
        self.assertEqual(metrics["unknown_cost_calls"], 0)
        self.assertFalse(final_accounting_complete(metrics, outcome))

    def test_commit_or_failed_read_restarts_finality_window(self):
        self.assertEqual(quiet_start(2, exited=True, settled=True, changed=True, at=5), 5)
        self.assertIsNone(quiet_start(2, exited=True, settled=False, changed=False, at=5))

    def test_native_configuration_is_explicit_private_and_price_exact(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory) / "home"
            config = write_config(home, "quickjs", 0)
            model = {"id": "kimi-k3", "context_length": 1048576,
                     "max_completion_tokens": 262144, "reasoning_efforts": ["high"],
                     "pricing": {"prompt": "0.000000001234567", "completion": "0"}}
            configured = configure_catalog(config, model)
            route = configured["providers"]["inference-net"]["models"]["kimi-k3"]
            self.assertEqual(route["prices"]["input"], 1234567)
            self.assertEqual(route["prices"]["output"], 0)
            self.assertIsNone(route["prices"]["cached_input"])
            self.assertEqual(route["max_output_tokens"], 262144)
            self.assertEqual((home / "runtime-v4/host.json").stat().st_mode & 0o777, 0o600)
            self.assertFalse((home / "config.json").exists())
            self.assertFalse((home / "models.json").exists())
            self.assertEqual(config["providers"]["inference-net"]["models"]["kimi-k3"]["prices"], {})
            with self.assertRaisesRegex(ValueError, "not advertised"):
                configure_catalog(config, {**model, "id": "another-model"})
            with self.assertRaisesRegex(ValueError, "invalid advertised model price"):
                configure_catalog(config, {**model, "pricing": {"prompt": "NaN"}})


@unittest.skipUnless(sys.platform == "linux" and os.environ.get("WHIP_EVAL_TEST_BINARY"),
                     "requires a disposable Linux candidate")
class NativeObserverAcceptance(unittest.TestCase):
    def test_both_engines_whole_tree_freeze_and_scoped_export(self):
        binary = os.environ["WHIP_EVAL_TEST_BINARY"]
        for engine in ("starlark", "quickjs"):
            with self.subTest(engine=engine), tempfile.TemporaryDirectory(prefix="native-eval-") as temporary:
                base = Path(temporary)
                workspace, home, evidence = base / "workspace", base / "home", base / "jobs/t1/native/agent/whip"
                workspace.mkdir()
                for args in (("init", "-q"), ("config", "user.email", "fixture@example.invalid"),
                             ("config", "user.name", "Fixture"), ("commit", "--allow-empty", "-qm", "initial")):
                    subprocess.run(["git", *args], cwd=workspace, check=True)
                instruction = base / "instruction.txt"
                instruction.write_text("fixture-qualification: create and commit the fixture files")
                args = SimpleNamespace(binary=binary, engine=engine, home=str(home), evidence=str(evidence),
                    instruction=str(instruction), timeout=45, fixture=True, contract=None,
                    max_cost=0, max_tokens=0, max_turns=0, max_output=0,
                    commit=False, native_defaults=True, model="kimi-k3")
                outcome = None
                try:
                    with contextlib.chdir(workspace), patch.dict(os.environ, {"INFERENCE_API_KEY": "offline-fixture"}):
                        outcome = run(args)
                    self.assertEqual(outcome["status"], "completed", outcome)
                    self.assertTrue(outcome["final_snapshot"], outcome)
                    self.assertTrue(outcome["content_export_complete"], outcome)
                    self.assertIsNotNone(outcome["frozen_daemon_pid"])
                    state = snapshot(evidence / "sessions.db")
                    self.assertEqual(len(state["sessions"]), 2)
                    self.assertTrue(state["settled"])
                    metrics = aggregate(state["calls"])
                    self.assertGreaterEqual(metrics["model_calls"], 4)
                    self.assertTrue(final_accounting_complete(metrics, outcome))
                    self.assertEqual(metrics["unknown_cache_calls"], 0)
                    self.assertEqual((workspace / "child.txt").read_text(), "child")
                    self.assertEqual(len((workspace / "large.txt").read_text()), 70000)
                    self.assertIn("second", (workspace / "page.txt").read_text())
                    # The root CLI completed while its child still had real work.
                    root_turns = [turn for turn in state["turns"] if turn["session_id"] == state["root"]["id"]]
                    child_turns = [turn for turn in state["turns"] if turn["session_id"] != state["root"]["id"]]
                    self.assertLess(root_turns[0]["finished_at"], child_turns[0]["finished_at"])
                    manifest = json.loads((evidence / "content-export.json").read_text())
                    self.assertGreater(len(manifest["bodies"]), 0)
                    for body in manifest["bodies"]:
                        data = (evidence / "content/sha256" / body["digest"]).read_bytes()
                        self.assertEqual(len(data), body["bytes"])
                        self.assertEqual(hashlib.sha256(data).hexdigest(), body["digest"])
                    # Normalize a synthetic runner envelope around the real copied
                    # native ledger. This does not claim Harbor/Pier integration.
                    from whip_evals.report import normalize_trial
                    identity = json.loads((evidence / "identity.json").read_text())
                    (evidence.parents[1] / "result.json").write_text(json.dumps({
                        "agent_result": {"metadata": {}}, "verifier_result": {"rewards": {"reward": 1}}}))
                    trial = {"id": "t1", "task_id": "fixture", "candidate_id": "native", "repetition": 1,
                             "runner": "harbor", "engine": engine, "binary_sha256": identity["binary_sha256"]}
                    raw = {"started": True, "job_path": "jobs/t1", "cleanup": {"complete": True}}
                    normalized = normalize_trial(trial, raw, base)
                    self.assertTrue(normalized["accounting_complete"], normalized)
                    self.assertTrue(normalized["evidence_complete"], normalized)
                    self.assertEqual(normalized["diagnostics"]["agent_count"], 2)
                    saved = json.loads((evidence / "state.json").read_text())
                    saved["calls"][0]["cost_nano_usd"] = 99
                    (evidence / "state.json").write_text(json.dumps(saved))
                    altered = normalize_trial(trial, raw, base)
                    self.assertFalse(altered["accounting_complete"])
                    self.assertIn("accounting_snapshot_mismatch", altered["error_codes"])
                    # A queued child input in the copied real schema blocks finality.
                    with contextlib.closing(sqlite3.connect(evidence / "sessions.db")) as db, db:
                        db.execute("INSERT INTO inputs(id,session_id,source,parts,created_at) VALUES(?,?,'user','[]',1)",
                                   ("pending-test", child_turns[0]["session_id"]))
                    self.assertFalse(snapshot(evidence / "sessions.db")["settled"])
                    # Body evidence may not follow a substituted symlink.
                    digest = manifest["bodies"][0]["digest"]
                    source = home / "runtime-v4/artifacts/sha256" / digest
                    source.unlink()
                    source.symlink_to(instruction)
                    with self.assertRaisesRegex(ValueError, "content body export incomplete"):
                        export_content_bodies(home, evidence, state["root"]["id"])
                finally:
                    if outcome and outcome.get("frozen_daemon_pid"):
                        signal_daemon(home, binary, signal.SIGCONT,
                            (outcome["frozen_daemon_pid"], outcome["frozen_process_start_time"]))
                    subprocess.run([binary, "daemon", "stop"], env=dict(os.environ, WHIPCODE_HOME=str(home)),
                                   check=False, capture_output=True, timeout=20)


if __name__ == "__main__":
    unittest.main()
