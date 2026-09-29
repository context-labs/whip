import json
import errno
from pathlib import Path
import signal
import sqlite3
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import AsyncMock, MagicMock, patch

from observe import aggregate, final_accounting_complete, quiet_start, run, settled_outcome, signal_daemon, snapshot
from whip_adapter import WhipAdapter
from study import paired_schedule, verifier_success
from harbor.models.agent.context import AgentContext as HarborContext
from pier.models.agent.context import AgentContext as PierContext


def call(status="succeeded", usage_source="reported", cost_source="reported"):
    return {"state": status, "dispatched_at": 1, "cost_nano_usd": 8000000 if cost_source == "reported" else None,
            "result": {"usage": {"input": 1000, "output": 200, "cached_input": 500},
                       "reported_cost_nano_usd": 8000000}}


class AccountingTests(unittest.TestCase):
    def test_native_reward_excludes_pier_diagnostic_counts(self):
        self.assertTrue(verifier_success({"reward": 1, "f2p_total": 122, "f2p_passed": 122, "p2p_total": 1272, "partial": 1}))
        self.assertFalse(verifier_success({"reward": 0, "p2p": 1, "partial": .91}))
        self.assertFalse(verifier_success({"partial": 1}))

    def test_pier_proxy_is_applied_to_agent_process_only(self):
        environment = SimpleNamespace(agent_process_env=lambda env: {"HTTPS_PROXY": "http://proxy.invalid:8080", **env})
        values = {"INFERENCE_API_KEY": "fixture"}
        self.assertEqual(WhipAdapter.process_env(environment, values), {"HTTPS_PROXY": "http://proxy.invalid:8080", **values})
        self.assertEqual(WhipAdapter.process_env(SimpleNamespace(), values), values)

    def test_each_pair_reverses_order_across_repetitions(self):
        tasks = [(name, "harbor", "/unused-fixture", False) for name in ("a", "b", "c", "d")]
        with patch("study.task_digest", return_value="fixture"):
            schedule = paired_schedule(tasks, 2, 20260910)
            self.assertEqual(schedule, paired_schedule(tasks, 2, 20260910))
        pairs = [schedule[index:index+2] for index in range(0, len(schedule), 2)]
        first = {pair[0]["task"]: pair[0]["engine"] for pair in pairs if pair[0]["repetition"] == 1}
        second = {pair[0]["task"]: pair[0]["engine"] for pair in pairs if pair[0]["repetition"] == 2}
        self.assertEqual(list(first.values()).count("starlark"), 2)
        for pair in pairs:
            self.assertEqual(pair[0]["task"], pair[1]["task"])
            self.assertNotEqual(pair[0]["engine"], pair[1]["engine"])
        for task in first:
            self.assertNotEqual(first[task], second[task])

    def test_missing_interrupted_result_remains_unknown(self):
        interrupted = call("interrupted", "none", "none")
        interrupted["result"] = None
        totals = aggregate([interrupted])
        self.assertEqual(totals["unknown_usage_calls"], 1)
        self.assertEqual(totals["unknown_cost_calls"], 1)

    def test_zero_pricing_is_known(self):
        free = call()
        free["cost_nano_usd"] = free["result"]["reported_cost_nano_usd"] = 0
        totals = aggregate([free])
        self.assertEqual(totals["ledger_cost_usd"], 0)
        self.assertEqual(totals["unknown_cost_calls"], 0)

    def test_partial_snapshot_cannot_release_trial_reservation(self):
        metrics = aggregate([call()])
        outcome = {"final_snapshot": True, "frozen_daemon_pid": 123, "pending": {}, "evidence_errors": []}
        self.assertTrue(final_accounting_complete(metrics, outcome))
        self.assertFalse(final_accounting_complete(metrics, None))
        self.assertFalse(final_accounting_complete(metrics, dict(outcome, final_snapshot=False)))
        self.assertFalse(final_accounting_complete(metrics, dict(outcome, frozen_daemon_pid=None)))
        self.assertFalse(final_accounting_complete(metrics, dict(outcome, pending={"active_turns": 1})))
        self.assertFalse(final_accounting_complete(metrics, dict(outcome, evidence_errors=["export failed"])))

    def test_independent_normalization_and_whole_tree(self):
        first, child = call(), call()
        # Root and child attempts use the same canonical ledger.
        totals = aggregate([first, child])
        self.assertEqual(totals["input_tokens"], 2000)
        self.assertEqual(totals["cache_tokens"], 1000)
        self.assertAlmostEqual(totals["reported_cost_usd"], 0.016)
        self.assertAlmostEqual(totals["ledger_cost_usd"], 0.016)
        self.assertEqual(totals["model_calls"], 2)

    def test_unknown_is_not_zero_in_actual_runner_contexts(self):
        pending = call("dispatched", "none", "none")
        pending["result"] = None
        totals = aggregate([pending])
        adapter = object.__new__(WhipAdapter)
        adapter.engine, adapter.binary_digest = "quickjs", "fixture-digest"
        for context_type in (HarborContext, PierContext):
            with self.subTest(context=context_type.__name__):
                context = context_type()
                adapter.update_context(context, totals)
                self.assertIsNone(context.n_input_tokens)
                self.assertIsNone(context.cost_usd)
                adapter.update_context(context, aggregate([call()]))
                self.assertEqual(context.n_input_tokens, 1000)
                self.assertEqual(context.cost_usd, 0.008)


class AdapterFailureTests(unittest.IsolatedAsyncioTestCase):
    async def test_lost_exec_stops_owned_daemon_and_preserves_unknown_usage(self):
        with tempfile.TemporaryDirectory() as directory:
            adapter = object.__new__(WhipAdapter)
            adapter.logs_dir = Path(directory)
            adapter.engine, adapter.binary_digest = "quickjs", "fixture-digest"
            adapter.timeout, adapter.max_cost, adapter.max_tokens = 10, 1, 1000
            adapter.max_turns = 2
            adapter.max_output = 8192
            adapter.commit, adapter.fixture = False, True
            adapter.logger = MagicMock()
            adapter.native_defaults, adapter.contract = True, None
            async def execute(command, **kwargs):
                if command.startswith("python3"):
                    raise RuntimeError("transport lost")
                return SimpleNamespace(stdout="", stderr="", return_code=0)
            environment = SimpleNamespace(upload_file=AsyncMock(), exec=AsyncMock(side_effect=execute),
                                          download_file=AsyncMock(side_effect=OSError("no evidence")))
            context = HarborContext()
            with self.assertRaisesRegex(RuntimeError, "transport lost"):
                await adapter.run("offline instruction", environment, context)
            environment.exec.assert_any_await("/opt/whip/whip daemon stop",
                                               env={"WHIPCODE_HOME": "/tmp/whip-eval-home"}, timeout_sec=20)
            self.assertFalse(context.metadata["whip_accounting_complete"])
            self.assertIsNone(context.cost_usd)
            self.assertIsNone(context.n_input_tokens)


class FinalityTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / "sessions.db"
        self.db = sqlite3.connect(self.path)
        self.addCleanup(self.db.close)
        self.db.executescript("""
        CREATE TABLE session_trees(id); INSERT INTO session_trees VALUES('tree');
        CREATE TABLE sessions(id,tree_id,parent_id,lifecycle,created_at);
        INSERT INTO sessions VALUES('root','tree',NULL,'active',0),('child','tree','root','active',1);
        CREATE TABLE turns(id,session_id,state,finished_at,started_at);
        INSERT INTO turns VALUES('root-turn','root','succeeded',1,0);
        CREATE TABLE model_attempts(id,finished_at);
        CREATE TABLE attempt_budget_ancestors(attempt_id,session_id);
        CREATE TABLE inputs(id,session_id,kind,turn_id,steered_turn_id,cancelled_at);
        CREATE TABLE mail(id,recipient_id,state,deleted_at,revision);
        CREATE TABLE mail_revisions(mail_id,revision,delivery,available_at);
        CREATE TABLE budget_limits(session_id,kind);
        CREATE TABLE schedules(session_id,id,schedule);
        CREATE TABLE operations(finished_at);
        CREATE TABLE permissions(state);
        CREATE TABLE goals(state,deleted_at);
        """)
        self.db.commit()

    def test_snapshot_keeps_owner_identity_without_copying_transcript_bodies(self):
        state = snapshot(self.path)
        self.assertEqual(state["evidence_schema"], "native-v4")
        self.assertEqual([row["id"] for row in state["sessions"]], ["root", "child"])
        self.assertEqual(state["turns"][0]["session_id"], "root")
        # Native messages/trace are retained in the database backup, not duplicated
        # as retired event/transcript projections in every polling snapshot.
        self.assertNotIn("events", state)
        self.assertNotIn("transcripts", state)

    def test_first_root_completion_does_not_end_running_child(self):
        self.db.execute("INSERT INTO turns VALUES('child-turn','child','running',NULL,1)")
        self.db.commit()
        self.assertFalse(snapshot(self.path)["settled"])
        self.db.execute("UPDATE turns SET state='succeeded',finished_at=2")
        self.db.execute("INSERT INTO mail VALUES('mail','root','pending',NULL,1)")
        self.db.execute("INSERT INTO mail_revisions VALUES('mail',1,'queued',0)")
        self.db.commit()
        self.assertFalse(snapshot(self.path)["settled"])
        self.db.execute("UPDATE mail SET state='delivered'")
        self.db.commit()
        self.assertTrue(snapshot(self.path)["settled"])

    def test_failed_prompt_retry_barrier_at_every_depth_and_next_turn_mail(self):
        for owner in ("root", "child"):
            with self.subTest(owner=owner):
                self.db.execute("INSERT INTO turns VALUES(?,?,'failed',2,1)", (owner+"-failed", owner))
                self.db.execute("INSERT INTO mail VALUES(?,?,'pending',NULL,1)", (owner+"-mail", owner))
                self.db.execute("INSERT INTO mail_revisions VALUES(?,1,'queued',0)", (owner+"-mail",))
                self.db.commit()
                self.assertTrue(snapshot(self.path)["settled"])
        self.db.execute("INSERT INTO mail VALUES('later','child','pending',NULL,1)")
        self.db.execute("INSERT INTO mail_revisions VALUES('later',1,'next_turn',0)")
        self.db.execute("INSERT INTO inputs VALUES('queued','child','prompt',NULL,NULL,NULL)")
        self.db.commit()
        self.assertFalse(snapshot(self.path)["settled"])

    def test_inputs_operations_permissions_attempts_and_goals_block_finality(self):
        values = {"inputs": "'queued','child','prompt',NULL,NULL,NULL", "operations": "NULL",
                  "permissions": "'pending'", "model_attempts": "'attempt',NULL", "goals": "'armed',NULL"}
        for table, value in values.items():
            with self.subTest(table=table):
                self.db.execute(f"INSERT INTO {table} VALUES({value})")
                self.db.commit()
                self.assertFalse(snapshot(self.path)["settled"])
                self.db.execute(f"DELETE FROM {table}")
                self.db.commit()

    def test_schedules_remain_evidence_at_finality(self):
        self.db.execute("INSERT INTO schedules VALUES('root',1,'@every 1h')")
        self.db.commit()
        state = snapshot(self.path)
        self.assertTrue(state["settled"])
        self.assertEqual(self.db.execute("SELECT schedule FROM schedules").fetchone()[0], "@every 1h")

    def test_events_restart_quiet_window(self):
        self.assertEqual(quiet_start(1, exited=True, settled=True, changed=True, at=2), 2)
        self.assertEqual(quiet_start(1, exited=True, settled=True, changed=False, at=2), 1)
        self.assertIsNone(quiet_start(1, exited=True, settled=False, changed=False, at=2))

    def test_latest_root_turn_classifies_outcome_after_initial_cli_success(self):
        for status in ("failed", "cancelled", "interrupted"):
            with self.subTest(status=status):
                self.db.execute("INSERT INTO turns VALUES(?,'root',?,1,1)", (status,status))
                self.db.execute("INSERT INTO turns VALUES('child-success','child','succeeded',1,1)")
                self.db.commit()
                outcome = settled_outcome(snapshot(self.path), 0)
                self.assertEqual(outcome["status"], "agent_error")
                self.assertEqual(outcome["latest_root_turn_status"], status)
        self.assertEqual(outcome["failed_or_interrupted_turns"], 3)
        self.db.execute("INSERT INTO turns VALUES('recovered','root','succeeded',2,2)")
        self.db.commit()
        recovered = snapshot(self.path)
        self.assertEqual(settled_outcome(recovered, 0)["status"], "completed")
        self.assertEqual(settled_outcome(recovered, 0)["failed_or_interrupted_turns"], 3)
        self.assertEqual(settled_outcome(recovered, 1)["status"], "agent_error")

    def test_pidfd_fallback_verifies_process_identity(self):
        home = Path(self.directory.name) / "home"
        runtime = home / "runtime-v4"
        runtime.mkdir(parents=True)
        (runtime / "runtime.lock").write_text("")
        (runtime / "runtime.lock").chmod(0o600)
        binary = home / "whip"
        binary.write_text("fixture")
        proc = home / "proc"
        process = proc / "123"
        process.mkdir(parents=True)
        (process / "exe").symlink_to(binary)
        (process / "environ").write_bytes(b"WHIPCODE_HOME=" + str(home).encode() + b"\0")
        (process / "stat").write_text("123 (whip) S " + "0 " * 18 + "123456 0 0")
        (process / "cmdline").write_bytes(str(binary.resolve()).encode() + b"\0_native-runtime\0-directory\0" + str(runtime).encode() + b"\0")
        (process / "fd").mkdir()
        (process / "fd/3").symlink_to(runtime / "runtime.lock")
        status = json.dumps({"state":"running", "directory":str(runtime), "process":{"pid":123}}).encode()
        real_path = Path
        with patch("observe.Path", side_effect=lambda value: proc if value == "/proc" else real_path(value)), \
             patch("observe.subprocess.run", return_value=SimpleNamespace(stdout=status)), \
             patch("observe.fcntl.flock", side_effect=BlockingIOError), \
             patch("observe.os.pidfd_open", create=True, side_effect=OSError(errno.ENOSYS, "unsupported")), \
             patch("observe.os.kill") as kill:
            self.assertEqual(signal_daemon(home, binary, signal.SIGSTOP), (123, "verified-pid", "123456"))
            kill.assert_called_once_with(123, signal.SIGSTOP)
            kill.reset_mock()
            (process / "environ").write_bytes(b"WHIPCODE_HOME=/different\0")
            with self.assertRaisesRegex(RuntimeError, "home differs"):
                signal_daemon(home, binary, signal.SIGSTOP)
            kill.assert_not_called()
            (process / "environ").write_bytes(b"WHIPCODE_HOME=" + str(home).encode() + b"\0")
            (process / "exe").unlink()
            (process / "exe").symlink_to("/mnt/rv/[rosetta]")
            (process / "maps").write_text(f"100-200 r-xp 00000 00:23 {binary.stat().st_ino} {binary.resolve()}\n")
            self.assertEqual(signal_daemon(home, binary, signal.SIGSTOP), (123, "verified-pid-rosetta", "123456"))
            kill.reset_mock()
            (process / "maps").write_text(f"100-200 r-xp 00000 00:23 0 {binary.resolve()}\n")
            with self.assertRaisesRegex(RuntimeError, "executable differs"):
                signal_daemon(home, binary, signal.SIGSTOP)
            kill.assert_not_called()

    def test_transient_read_failure_does_not_reuse_popped_or_stale_state(self):
        home = Path(self.directory.name) / "home"
        runtime = home / "runtime-v4"
        runtime.mkdir(parents=True)
        database = runtime / "state.db"
        with sqlite3.connect(database) as db:
            db.executescript("""
            CREATE TABLE sessions(id,parent_id,tree_id); INSERT INTO sessions VALUES('root',NULL,'tree');
            CREATE TABLE content_bodies(digest,size);
            CREATE TABLE content_references(id,digest,owner_session_id);
            """)
        instruction = Path(self.directory.name) / "instruction.txt"
        instruction.write_text("offline instruction")
        binary = Path(self.directory.name) / "whip"
        binary.write_text("offline binary fixture")
        clock = [0.0]
        samples = [0]
        def sample(*_):
            samples[0] += 1
            if samples[0] == 1:
                raise sqlite3.OperationalError("temporary lock")
            return {"root": {"id": "root"}, "turns": [{"session_id": "root", "state": "succeeded"}],
                    "calls": [], "pending": {}, "settled": True}
        process = MagicMock()
        process.poll.return_value = 0
        process.returncode = 0
        args = SimpleNamespace(home=str(home), evidence=str(home / "evidence"), engine="starlark",
                               fixture=False, max_output=8192, binary=str(binary), instruction=str(instruction),
                               commit=False, max_cost=1, max_tokens=1000, max_turns=2, timeout=15)
        with patch.dict("os.environ", {"INFERENCE_API_KEY": "offline"}), \
             patch("observe.write_config", return_value={}), \
             patch("observe.discover_catalog", return_value={"defaults":{"model":{"name":"kimi-k3"}}}), \
             patch("observe.prepare_native_session", return_value="root"), \
             patch("observe.subprocess.Popen", return_value=process), \
             patch("observe.process_sample", return_value=(0, {})), \
             patch("observe.snapshot", side_effect=sample), \
             patch("observe.freeze_daemon", return_value=(123, "pidfd", "123456")), \
             patch("observe.time.monotonic", side_effect=lambda: clock[0]), \
             patch("observe.time.sleep", side_effect=lambda delay: clock.__setitem__(0, clock[0] + delay)):
            outcome = run(args)
        self.assertEqual(outcome["status"], "completed")
        self.assertEqual(outcome["latest_root_turn_status"], "succeeded")
        self.assertEqual(outcome["failed_or_interrupted_turns"], 0)
        self.assertTrue(outcome["final_snapshot"])
        self.assertEqual(outcome["evidence_errors"], [])
        self.assertGreaterEqual(outcome["duration_seconds"], 2.25)
        self.assertGreaterEqual(outcome["agent_duration_seconds"], 2.25)
        self.assertLessEqual(outcome["agent_duration_seconds"], outcome["duration_seconds"])

    def test_content_copy_failure_does_not_erase_verified_accounting_snapshot(self):
        with patch("observe.export_content_bodies", side_effect=ValueError("missing fixture body")):
            self.test_transient_read_failure_does_not_reuse_popped_or_stale_state()
        outcome = json.loads((Path(self.directory.name) / "home/evidence/outcome.json").read_text())
        self.assertTrue(outcome["final_snapshot"])
        self.assertFalse(outcome["content_export_complete"])
        self.assertEqual(outcome["content_export_errors"], ["missing fixture body"])
        self.assertTrue(final_accounting_complete(aggregate([]), outcome))


if __name__ == "__main__":
    unittest.main()
