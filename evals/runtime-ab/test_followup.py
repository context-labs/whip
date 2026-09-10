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
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.adapter = object.__new__(WhipAdapter)
        self.adapter.logs_dir = Path(self.directory.name)
        self.adapter.engine, self.adapter.binary_digest = "quickjs", "fixture"
        self.adapter.timeout, self.adapter.max_cost, self.adapter.max_tokens = 10, 2.5, 0
        self.adapter.max_turns, self.adapter.max_output = 60, 32768
        self.adapter.commit, self.adapter.fixture = False, True
        self.adapter.logger = MagicMock()
        self.context = AgentContext()
        self.metrics = aggregate([call()])
        self.outcome = {"final_snapshot": True, "frozen_daemon_pid": 123,
                        "pending": {}, "evidence_errors": []}
        self.probe_interval = patch("whip_adapter.PROBE_INTERVAL_SECONDS", .001)
        self.probe_interval.start()
        self.addCleanup(self.probe_interval.stop)

    async def download(self, source, target):
        value = self.metrics if target.name == "metrics.json" else self.outcome if target.name == "outcome.json" else {}
        target.write_text(json.dumps(value))

    def environment(self, execute, download=None):
        return SimpleNamespace(upload_file=AsyncMock(), exec=AsyncMock(side_effect=execute),
                               download_file=AsyncMock(side_effect=download or self.download))

    async def test_probe_timeout_retains_last_sample_then_complete_final_evidence(self):
        done = asyncio.Event()
        probes = 0

        async def execute(command, **kwargs):
            nonlocal probes
            if command.startswith("python3"):
                await done.wait()
            elif command.startswith("cat"):
                probes += 1
                if probes == 1:
                    return SimpleNamespace(stdout=json.dumps(self.metrics), return_code=0)
                # Last sample is still present at the timeout, independent of
                # canonical accounting becoming final during later download.
                self.assertEqual(self.context.metadata["whip"], self.metrics)
                done.set()
                raise RuntimeError("Command timed out after 15 seconds")
            return SimpleNamespace(stdout="", stderr="", return_code=0)

        environment = self.environment(execute)
        await self.adapter.run("fixture", environment, self.context)
        self.assertTrue(self.context.metadata["whip_accounting_complete"])
        self.assertEqual(self.context.cost_usd, .008)
        self.assertEqual(self.context.n_input_tokens, 1000)
        telemetry = self.context.metadata["whip_metrics_probes"]
        self.assertEqual(telemetry["timeouts"], 1)
        self.assertEqual(telemetry["samples"], 1)
        self.assertGreaterEqual(telemetry["max_staleness_seconds"], 0)
        self.assertEqual(self.context.metadata["whip_evidence_errors"], [])
        self.assertFalse(self.context.metadata["whip_cleanup"]["truncated"])

    async def test_actual_observer_failure_after_probe_timeout_still_propagates(self):
        done = asyncio.Event()

        async def execute(command, **kwargs):
            if command.startswith("python3"):
                await done.wait()
                raise RuntimeError("observer transport failed")
            if command.startswith("cat"):
                done.set()
                raise TimeoutError("probe timeout")
            return SimpleNamespace(stdout="", stderr="", return_code=0)

        environment = self.environment(execute)
        with self.assertRaisesRegex(RuntimeError, "observer transport failed"):
            await self.adapter.run("fixture", environment, self.context)
        self.assertEqual(self.context.metadata["whip_metrics_probes"]["timeouts"], 1)
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertIsNone(self.context.cost_usd)
        environment.exec.assert_any_await("/opt/whip/whip daemon stop",
            env={"WHIP_HOME": "/tmp/whip-eval-home"}, timeout_sec=20)

    async def test_no_probe_after_operation_completes(self):
        async def execute(command, **kwargs):
            self.assertTrue(command.startswith("python3"))
            return SimpleNamespace(stdout="", stderr="", return_code=0)

        await self.adapter.run("fixture", self.environment(execute), self.context)
        self.assertEqual(self.context.metadata["whip_metrics_probes"]["attempts"], 0)
        self.assertTrue(self.context.metadata["whip_accounting_complete"])

    async def test_other_probe_errors_are_not_tolerated(self):
        async def execute(command, **kwargs):
            if command.startswith("python3"):
                await asyncio.Event().wait()
            if command.startswith("cat"):
                raise RuntimeError("Docker connection lost")
            return SimpleNamespace(stdout="", stderr="", return_code=0)

        with self.assertRaisesRegex(RuntimeError, "Docker connection lost"):
            await self.adapter.run("fixture", self.environment(execute), self.context)
        self.assertEqual(self.context.metadata["whip_metrics_probes"]["timeouts"], 0)
        self.assertFalse(self.context.metadata["whip_accounting_complete"])

    async def test_malformed_intermediate_metrics_remain_fatal(self):
        async def execute(command, **kwargs):
            if command.startswith("python3"):
                await asyncio.Event().wait()
            if command.startswith("cat"):
                return SimpleNamespace(stdout='{invalid-json', stderr="", return_code=0)
            return SimpleNamespace(stdout="", stderr="", return_code=0)

        with self.assertRaises(json.JSONDecodeError):
            await self.adapter.run("fixture", self.environment(execute), self.context)
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertIsNone(self.context.cost_usd)
        self.assertEqual(self.context.metadata["whip_metrics_probes"]["timeouts"], 0)

    async def test_malformed_final_metrics_or_outcome_keep_observer_status_and_decode_error(self):
        async def execute(command, **kwargs):
            return SimpleNamespace(stdout="observer finished", stderr="", return_code=0)

        for malformed in ("metrics.json", "outcome.json"):
            with self.subTest(malformed=malformed):
                self.context = AgentContext()

                async def download(source, target):
                    if target.name == malformed:
                        target.write_text('{invalid-json')
                    else:
                        await self.download(source, target)

                await self.adapter.run("fixture", self.environment(execute, download), self.context)
                self.assertEqual(self.context.metadata["whip_observer_exit_code"], 0)
                self.assertFalse(self.context.metadata["whip_accounting_complete"])
                self.assertIsNone(self.context.cost_usd)
                self.assertIsNone(self.context.n_input_tokens)
                self.assertIn(malformed + " decode: JSONDecodeError", self.context.metadata["whip_evidence_errors"])
                self.assertEqual((self.adapter.logs_dir / "observer.stdout").read_text(), "observer finished")

    async def test_nonzero_observer_exit_is_an_error_with_evidence(self):
        async def execute(command, **kwargs):
            return SimpleNamespace(stdout="raw", stderr="failed", return_code=7 if command.startswith("python3") else 0)

        with self.assertRaisesRegex(RuntimeError, "observer exited with code 7"):
            await self.adapter.run("fixture", self.environment(execute), self.context)
        self.assertEqual(self.context.metadata["whip_observer_exit_code"], 7)
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertTrue((self.adapter.logs_dir / "outcome.json").exists())

    async def test_stalled_evidence_cleanup_is_bounded_and_unknown(self):
        async def execute(command, **kwargs):
            return SimpleNamespace(stdout="", stderr="", return_code=0)

        async def download(source, target):
            target.write_text('{"partial":')
            await asyncio.Event().wait()

        started = asyncio.get_running_loop().time()
        with patch("whip_adapter.CLEANUP_TIMEOUT_SECONDS", .02):
            await self.adapter.run("fixture", self.environment(execute, download), self.context)
        self.assertLess(asyncio.get_running_loop().time() - started, .5)
        self.assertTrue(self.context.metadata["whip_cleanup"]["truncated"])
        self.assertEqual(self.context.metadata["whip_cleanup"]["downloaded"], [])
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertIsNone(self.context.cost_usd)
        self.assertIsNone(self.context.n_input_tokens)
        self.assertIn("evidence/daemon cleanup exceeded deadline", self.context.metadata["whip_evidence_errors"])

    async def test_later_stalled_download_preserves_raw_metrics_but_not_final_accounting(self):
        async def execute(command, **kwargs):
            return SimpleNamespace(stdout="", stderr="", return_code=0)

        async def download(source, target):
            if target.name in ("metrics.json", "outcome.json"):
                await self.download(source, target)
            else:
                await asyncio.Event().wait()

        with patch("whip_adapter.CLEANUP_TIMEOUT_SECONDS", .02):
            await self.adapter.run("fixture", self.environment(execute, download), self.context)
        self.assertEqual(self.context.metadata["whip"], self.metrics)
        self.assertEqual(self.context.metadata["whip_outcome"], self.outcome)
        self.assertTrue(self.context.metadata["whip_cleanup"]["truncated"])
        self.assertFalse(self.context.metadata["whip_accounting_complete"])
        self.assertIsNone(self.context.cost_usd)
        self.assertIsNone(self.context.n_input_tokens)

    async def test_stalled_daemon_stop_does_not_replace_observer_error(self):
        async def execute(command, **kwargs):
            if command.startswith("python3"):
                raise RuntimeError("original observer failure")
            await asyncio.Event().wait()

        with patch("whip_adapter.CLEANUP_TIMEOUT_SECONDS", .02):
            with self.assertRaisesRegex(RuntimeError, "original observer failure"):
                await self.adapter.run("fixture", self.environment(execute), self.context)
        self.assertTrue(self.context.metadata["whip_cleanup"]["truncated"])
        self.assertFalse(self.context.metadata["whip_accounting_complete"])

    async def test_external_cancellation_during_cleanup_still_propagates(self):
        download_started = asyncio.Event()

        async def execute(command, **kwargs):
            return SimpleNamespace(stdout="", stderr="", return_code=0)

        async def download(source, target):
            download_started.set()
            await asyncio.Event().wait()

        task = asyncio.create_task(self.adapter.run("fixture", self.environment(execute, download), self.context))
        await download_started.wait()
        task.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await task
        self.assertTrue(self.context.metadata["whip_cleanup"]["truncated"])
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
        self.assertEqual(envelope["outer_watchdog_seconds"], 1800 + 600 + 1245 + 300 + 3600 + 1 + 960 + 60)

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
