"""Offline regressions for follow-up telemetry, cleanup and timing only."""
import asyncio
import json
from pathlib import Path
import subprocess
import signal
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import AsyncMock, MagicMock, patch

from harbor.models.agent.context import AgentContext
from observe import aggregate
from study import cleanup_owned_containers, paired_schedule, timing_envelope, run as run_study
from test_observe import call
from whip_adapter import WhipAdapter


class FollowupAdapterTests(unittest.IsolatedAsyncioTestCase):
    """Native evidence is bind-mounted; no intermediate cat/download probe exists."""
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.adapter = object.__new__(WhipAdapter)
        self.adapter.logs_dir = Path(self.directory.name)
        self.adapter.engine, self.adapter.binary_digest = "quickjs", "fixture"
        self.adapter.timeout, self.adapter.max_cost, self.adapter.max_tokens = 10, 0, 0
        self.adapter.max_turns, self.adapter.max_output = 0, 0
        self.adapter.commit, self.adapter.fixture = False, True
        self.adapter.contract, self.adapter.native_defaults = None, True
        self.adapter.logger = MagicMock()
        self.context = AgentContext()
        self.metrics = aggregate([call()])
        self.outcome = {"final_snapshot": True, "frozen_daemon_pid": 123,
                        "pending": {}, "evidence_errors": []}

    def publish(self):
        evidence = self.adapter.evidence_dir()
        evidence.mkdir(exist_ok=True)
        for name, value in (("metrics.json", self.metrics), ("outcome.json", self.outcome)):
            (evidence / name).write_text(json.dumps(value))
        return evidence

    def environment(self, execute):
        return SimpleNamespace(upload_file=AsyncMock(), exec=AsyncMock(side_effect=execute),
                               download_file=AsyncMock(), download_dir=AsyncMock())

    async def test_only_final_mounted_evidence_counts_without_probe_or_download(self):
        async def execute(command, **kwargs):
            self.assertTrue(command.startswith("python3"))
            self.publish()
            self.assertIsNone(self.context.cost_usd)
            return SimpleNamespace(stdout="observer finished", stderr="", return_code=0)
        environment = self.environment(execute)
        await self.adapter.run("fixture", environment, self.context)
        self.assertTrue(self.context.metadata["whip_accounting_complete"])
        self.assertEqual(self.context.cost_usd, .008)
        self.assertEqual(self.context.n_input_tokens, 1000)
        self.assertEqual(environment.exec.await_count, 1)
        environment.download_file.assert_not_awaited()
        environment.download_dir.assert_not_awaited()

    async def test_malformed_final_metrics_or_outcome_keep_observer_status(self):
        for malformed in ("metrics.json", "outcome.json"):
            with self.subTest(malformed=malformed):
                self.context = AgentContext()
                async def execute(command, **kwargs):
                    (self.publish() / malformed).write_text('{invalid-json')
                    return SimpleNamespace(stdout="observer finished", stderr="", return_code=0)
                await self.adapter.run("fixture", self.environment(execute), self.context)
                self.assertEqual(self.context.metadata["whip_observer_exit_code"], 0)
                self.assertFalse(self.context.metadata["whip_accounting_complete"])
                self.assertIsNone(self.context.cost_usd)
                self.assertIsNone(self.context.n_input_tokens)
                self.assertIn(malformed + " decode: JSONDecodeError", self.context.metadata["whip_evidence_errors"])
                self.assertEqual((self.adapter.logs_dir / "observer.stdout").read_text(), "observer finished")

    async def test_missing_evidence_is_unknown_without_download_retry(self):
        environment = self.environment(lambda *args, **kwargs: SimpleNamespace(stdout="", stderr="", return_code=0))
        await self.adapter.run("fixture", environment, self.context)
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertIsNone(self.context.cost_usd)
        self.assertIn("metrics.json: missing", self.context.metadata["whip_evidence_errors"])
        environment.download_file.assert_not_awaited()

    async def test_nonzero_observer_with_valid_evidence_remains_failed(self):
        async def execute(command, **kwargs):
            self.publish()
            return SimpleNamespace(stdout="raw", stderr="failed", return_code=7 if command.startswith("python3") else 0)
        with self.assertRaisesRegex(RuntimeError, "observer exited with code 7"):
            await self.adapter.run("fixture", self.environment(execute), self.context)
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertIsNone(self.context.cost_usd)
        self.assertTrue((self.adapter.evidence_dir() / "outcome.json").exists())

    async def test_lost_exec_stops_exact_native_trial_home(self):
        async def execute(command, **kwargs):
            if command.startswith("python3"):
                raise RuntimeError("observer transport failed")
            return SimpleNamespace(stdout="", stderr="", return_code=0)
        environment = self.environment(execute)
        with self.assertRaisesRegex(RuntimeError, "observer transport failed"):
            await self.adapter.run("fixture", environment, self.context)
        environment.exec.assert_any_await("/opt/whip/whip daemon stop",
            env={"WHIPCODE_HOME": "/tmp/whip-eval-home"}, timeout_sec=20)
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertIsNone(self.context.cost_usd)

    async def test_stalled_cleanup_preserves_primary_error_and_is_bounded(self):
        async def execute(command, **kwargs):
            if command.startswith("python3"):
                self.publish()
                raise RuntimeError("primary observer failure")
            await asyncio.Event().wait()
        started = asyncio.get_running_loop().time()
        with patch("whip_adapter.CLEANUP_TIMEOUT_SECONDS", .02):
            with self.assertRaisesRegex(RuntimeError, "primary observer failure"):
                await self.adapter.run("fixture", self.environment(execute), self.context)
        self.assertLess(asyncio.get_running_loop().time() - started, .5)
        self.assertTrue(self.context.metadata["whip_cleanup"]["truncated"])
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertIsNone(self.context.cost_usd)

    async def test_cancelled_observer_keeps_unknown_accounting(self):
        async def execute(command, **kwargs):
            if command.startswith("python3"):
                raise asyncio.CancelledError()
            return SimpleNamespace(stdout="", stderr="", return_code=0)
        with self.assertRaises(asyncio.CancelledError):
            await self.adapter.run("fixture", self.environment(execute), self.context)
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertIsNone(self.context.cost_usd)


class FollowupStudyTests(unittest.TestCase):
    def test_repository_suite_filters_original_seeded_schedule(self):
        tasks = [("regex", "harbor", "/unused", False), ("openssl", "harbor", "/unused", False),
                 ("anko", "pier", "/unused", True), ("httpx", "pier", "/unused", True)]
        with patch("study.task_digest", return_value="fixture"):
            all_trials = paired_schedule(tasks, 2, 20260910)
            repository = paired_schedule(tasks, 2, 20260910, "repository")
        self.assertEqual(repository, [trial for trial in all_trials if trial["runner"] == "pier"])
        self.assertEqual([(t["task"], t["engine"]) for t in repository[:4]],
                         [("httpx", "starlark"), ("httpx", "quickjs"), ("anko", "quickjs"), ("anko", "starlark")])

    def test_pier_timing_retains_two_full_native_verifier_attempts(self):
        with tempfile.TemporaryDirectory() as directory:
            (Path(directory) / "task.toml").write_text('''[environment]
build_timeout_sec = 1800
[verifier]
timeout_sec = 1800
environment_mode = "separate"
[[verifier.collect]]
timeout_sec = 300
''')
            envelope = timing_envelope(directory, "pier", 900)
        self.assertEqual(envelope["agent_runner_seconds"], 1245)
        self.assertEqual(envelope["native_verifier_attempts"], 2)
        self.assertEqual(envelope["native_verifier_seconds_per_attempt"], 1800)
        self.assertEqual(envelope["separate_verifier_build_outside_timeout_seconds"], 0)
        self.assertEqual(envelope["outer_watchdog_seconds"], 1800 + 1920 + 1245 + 300 + 3600 + 1 + 960 + 60)

    def test_harbor_separate_build_has_its_own_budget(self):
        with tempfile.TemporaryDirectory() as directory:
            (Path(directory) / "task.toml").write_text('[verifier]\nenvironment_mode = "separate"\ntimeout_sec = 900\n')
            envelope = timing_envelope(directory, "harbor", 900)
        self.assertEqual(envelope["native_verifier_attempts"], 1)
        self.assertEqual(envelope["native_verifier_seconds_per_attempt"], 900)
        self.assertEqual(envelope["separate_verifier_build_outside_timeout_seconds"], 600)

    def cleanup_fixture(self, directory):
        job = Path(directory) / "job"
        trial = job / "owned-random-trial"
        trial.mkdir(parents=True)
        (trial / "config.json").write_text(json.dumps({"trial_name": trial.name, "trials_dir": str(job)}))
        return job, trial

    def test_watchdog_only_removes_exact_project_and_owned_mount(self):
        with tempfile.TemporaryDirectory() as directory:
            job, trial = self.cleanup_fixture(directory)
            removed = []

            def docker(command, **kwargs):
                self.assertLessEqual(kwargs["timeout"], 30)
                if command[1] == "ps":
                    owned = command[-1].endswith("=" + trial.name)
                    return SimpleNamespace(stdout="owned-id\n" if owned and not removed else "")
                if command[1] == "inspect":
                    return SimpleNamespace(stdout=json.dumps({"id": "owned-id", "project": trial.name,
                        "mounts": [{"Type": "bind", "Source": str(trial / "agent"), "Destination": "/logs/agent"}]}))
                self.assertEqual(command, ["docker", "rm", "-f", "owned-id"])
                removed.append("owned-id")
                return SimpleNamespace(stdout="owned-id")

            with patch("study.subprocess.run", side_effect=docker):
                report = cleanup_owned_containers(job, "pier")
        self.assertTrue(report["complete"])
        self.assertEqual(report["removed_container_ids"], ["owned-id"])

    def test_watchdog_refuses_foreign_mount_even_with_matching_project(self):
        with tempfile.TemporaryDirectory() as directory:
            job, trial = self.cleanup_fixture(directory)

            def docker(command, **kwargs):
                if command[1] == "ps":
                    return SimpleNamespace(stdout="foreign-id\n")
                self.assertEqual(command[1], "inspect")
                return SimpleNamespace(stdout=json.dumps({"id": "foreign-id", "project": trial.name,
                    "mounts": [{"Type": "bind", "Source": "/unrelated/agent", "Destination": "/logs/agent"}]}))

            with patch("study.subprocess.run", side_effect=docker):
                report = cleanup_owned_containers(job, "pier")
        self.assertFalse(report["complete"])
        self.assertEqual(report["removed_container_ids"], [])

    def test_outer_watchdog_has_bounded_signal_waits_and_always_attempts_owned_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for task in ("anko-typed-variable-bindings", "httpx-multipart-response-parsing"):
                path = root / "tasks" / task
                path.mkdir(parents=True)
                (path / "task.toml").write_text("[verifier]\ntimeout_sec = 1800\n")
            binary = root / "whip"
            binary.write_text("fixture")
            args = SimpleNamespace(phase="formal", suite="repository", binary=str(binary),
                output=str(root / "results"), terminal=str(root / "not-needed"), deepswe=str(root),
                repetitions=1, seed=20260910, trial_cap=2.5, total_cap=10, timeout=900,
                max_tokens=0, max_turns=60, max_output=32768)
            process = MagicMock(pid=99999, returncode=None)
            process.wait.side_effect = [subprocess.TimeoutExpired("runner", limit) for limit in (1, 60, 15)]
            with patch("study.subprocess.Popen", return_value=process) as launch, \
                    patch("study.os.killpg") as killpg, \
                    patch("study.cleanup_owned_containers", return_value={"complete": False, "errors": ["TimeoutExpired"]}) as cleanup:
                with self.assertRaisesRegex(RuntimeError, "outer watchdog interrupted"):
                    run_study(args)
            self.assertEqual(launch.call_count, 1)
            self.assertEqual([call.kwargs["timeout"] for call in process.wait.call_args_list][1:], [60, 15])
            self.assertEqual([call.args for call in killpg.call_args_list], [(99999, signal.SIGINT), (99999, signal.SIGKILL)])
            cleanup.assert_called_once()
            record = json.loads((root / "results/trials.json").read_text())[0]
            self.assertTrue(record["outer_watchdog"])
            self.assertEqual(record["watchdog_runner_stop_error"], "TimeoutExpired")
            self.assertEqual(record["charged_or_reserved_usd"], 2.5)
            self.assertFalse(record["watchdog_cleanup"]["complete"])
            recipe = json.loads((root / "results/recipe.json").read_text())
            self.assertEqual(recipe["suite"], "repository")
            self.assertEqual(recipe["automatic_task_retries"], 0)
            self.assertEqual(recipe["max_tree_tokens"], 0)
            # An outer-watchdog record cannot silently resume into another job.
            with patch("study.subprocess.Popen") as launch:
                with self.assertRaisesRegex(RuntimeError, "prior trial has an uncertain result"):
                    run_study(args)
                launch.assert_not_called()

    def test_watchdog_cleanup_timeout_is_reported_without_retry(self):
        with tempfile.TemporaryDirectory() as directory:
            job, trial = self.cleanup_fixture(directory)
            with patch("study.subprocess.run", side_effect=subprocess.TimeoutExpired("docker", 30)) as docker:
                report = cleanup_owned_containers(job, "pier")
        self.assertFalse(report["complete"])
        self.assertEqual(report["errors"], ["TimeoutExpired"])
        self.assertEqual(docker.call_count, 1)
        self.assertLessEqual(docker.call_args.kwargs["timeout"], 30)


if __name__ == "__main__":
    unittest.main()
