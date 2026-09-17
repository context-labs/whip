"""Cloud lifecycle contracts with no Docker, Modal API, credentials, or model calls."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import MagicMock, patch

# Replace only the SDK import, keeping real evaluation modules cached for other tests.
if "modal" in sys.modules:
    raise RuntimeError("cloud tests require an SDK-free process")
sys.modules["modal"] = MagicMock()
try:
    from whip_evals import modal_cloud as cloud, modal_cli, modal_worker
finally:
    del sys.modules["modal"]
from whip_evals.cli import parser
from whip_evals.common import file_hash, read_json, write_json


class NotFound(Exception):
    pass


class MemoryState:
    def __init__(self, values=None):
        self.values = {k: dict(v) for k, v in (values or {}).items()}

    def get(self, key):
        return self.values.get(key)

    def put(self, key, value, *, skip_if_exists=False):
        if skip_if_exists and key in self.values:
            return False
        self.values[key] = dict(value)
        return True

    def items(self):
        return list(self.values.items())

    def keys(self):
        return list(self.values)

    def pop(self, key):
        return self.values.pop(key)


class MemoryVolume:
    """Bytes keyed by absolute path with the SDK's read_file/iterdir/remove_file shapes."""
    def __init__(self, files=None):
        self.files = dict(files or {})
        self.failures = {}

    def read_file(self, path):
        if self.failures.get(path, 0) > 0:
            self.failures[path] -= 1
            raise ConnectionError("stream dropped")
        if path not in self.files:
            raise FileNotFoundError(path)
        data = self.files[path]
        for index in range(0, max(len(data), 1), 7):
            yield data[index:index + 7]

    def iterdir(self, prefix, recursive=True):
        matches = [p for p in sorted(self.files) if p.startswith(prefix + "/")]
        if not matches:
            raise FileNotFoundError(prefix)
        return [SimpleNamespace(path=p, type=1) for p in matches]

    def remove_file(self, path, recursive=False):
        keys = [k for k in self.files if k == path or k.startswith(path + "/")]
        if not keys:
            raise FileNotFoundError(path)
        for key in keys:
            del self.files[key]


class FakeSandbox:
    def __init__(self, polls):
        self.polls = list(polls)
        self.object_id = "sb-1"
        self.tags = None
        self.filesystem = MagicMock()
        self.terminated = False

    def poll(self):
        return self.polls.pop(0) if len(self.polls) > 1 else self.polls[0]

    def set_tags(self, tags):
        self.tags = tags

    def terminate(self):
        self.terminated = True
        self.polls = [137]

    def wait(self, raise_on_termination=False):
        pass


def fake_sdk(sandbox=None, create=None):
    return SimpleNamespace(
        App=SimpleNamespace(lookup=lambda name, environment_name: "app"),
        Image=SimpleNamespace(from_id=lambda image_id: "image"),
        Secret=SimpleNamespace(from_name=lambda *args, **kwargs: "secret"),
        Sandbox=SimpleNamespace(create=create or MagicMock(return_value=sandbox)),
        exception=SimpleNamespace(NotFoundError=NotFound),
        current_function_call_id=lambda: "fc-1")


def settings(**updates):
    shape = {"physical_cpus": 6, "memory_mb": 24576, "min_free_disk_mb": 100000}
    value = {"app": cloud.APP_NAME, "environment": cloud.ENVIRONMENT, "worker_image_id": "im-fixture",
             "max_jobs": 90, "jobs": 1, "shapes": {"harbor": shape, "pier": shape}}
    value.update(updates)
    return value


def trial(**updates):
    value = {"id": "t1", "task_id": "terminal-bench/fixture", "runner": "harbor", "engine": "quickjs",
             "binary_sha256": "f" * 64, "candidate_id": "quickjs", "repetition": 1, "outer_watchdog_seconds": 900,
             "resources": {"cpus": 1, "memory_mb": 2048, "storage_mb": 10240}}
    value.update(updates)
    return value


def marker(**updates):
    value = {"run_id": "r", "trial_id": "t1", "bundle_sha256": "a" * 64, "status": "completed", "files": {}}
    value.update(updates)
    return value


class SettingsTests(unittest.TestCase):
    def test_frontier_config_is_valid_and_secret_free(self):
        config = cloud.load_config()
        self.assertEqual(config["environment"], "whipcode")
        self.assertEqual(config["jobs"], config["max_jobs"])
        self.assertNotIn("INFERENCE", json.dumps(config).upper().replace("INFERENCE-NET", ""))

    def test_settings_restrict_environment_image_and_concurrency(self):
        self.assertEqual(cloud.validate_settings(settings())["jobs"], 1)
        self.assertEqual(cloud.validate_settings(settings(max_jobs=10, jobs=10))["jobs"], 10)
        for bad in ({"environment": "main"}, {"app": "other"}, {"worker_image_id": "latest"},
                    {"jobs": 91}, {"jobs": 0}, {"jobs": True}, {"max_jobs": 5, "jobs": 6},
                    {"secret": "x"}, {"shapes": {"harbor": {"physical_cpus": 6}}}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                cloud.validate_settings(settings(**bad))

    def test_launch_is_unix_only_flagged_cloud_and_secret_free(self):
        command = cloud.launch_command("r", "t1", "a" * 64)
        self.assertIn("export WHIP_EVAL_CLOUD=1", command)
        self.assertIn("--host=unix:///var/run/docker.sock", command)
        self.assertNotIn("tcp://", command)
        self.assertNotIn("INFERENCE_API_KEY", command)
        self.assertIn("whip_evals.modal_worker r t1 " + "a" * 64, command)
        with self.assertRaises(ValueError):
            cloud.launch_command("r", "t1", "short")


class ExecuteTests(unittest.TestCase):
    def campaign(self, state=None, evidence=None):
        return cloud.ModalCampaign("r", "a" * 64, settings(), state or MemoryState(), MagicMock(), evidence or MemoryVolume())

    def run_execute(self, campaign, sandbox, *, cancelled=None, create=None, **overrides):
        cancelled = cancelled or threading.Event()
        with patch.object(cloud, "sdk", return_value=fake_sdk(sandbox, create)), \
             patch.object(cloud, "POLL_SECONDS", 0), patch.object(cloud, "MARKER_GRACE_SECONDS", 0):
            return campaign.execute(trial(**overrides), cancelled), cancelled

    def test_waits_for_vm_exit_then_reads_marker_and_never_cancels_campaign(self):
        evidence = MemoryVolume({"/r/attempts/t1/complete.json": json.dumps(marker()).encode()})
        sandbox = FakeSandbox([None, None, 0])
        campaign = self.campaign(evidence=evidence)
        result, cancelled = self.run_execute(campaign, sandbox)
        self.assertTrue(result["started"])
        self.assertTrue(result["cleanup"]["complete"])
        self.assertEqual(result["cloud"]["state"], "completed")
        self.assertEqual(result["cloud"]["worker_status"], "completed")
        self.assertEqual(result["cloud"]["worker_exit_code"], 0)
        self.assertFalse(cancelled.is_set())
        self.assertFalse(sandbox.terminated)
        self.assertEqual(sandbox.tags, cloud.worker_tags("r", "t1", "a" * 64))
        self.assertEqual(campaign.state.get("r/attempt/t1")["state"], "completed")
        sandbox.filesystem.write_text.assert_not_called()

    def test_exit_without_marker_is_recorded_and_the_run_continues(self):
        sandbox = FakeSandbox([None, 2])
        result, cancelled = self.run_execute(self.campaign(), sandbox)
        self.assertEqual(result["cloud"]["state"], "exited_without_marker")
        self.assertEqual(result["cloud"]["worker_exit_code"], 2)
        self.assertTrue(result["cleanup"]["complete"])
        self.assertFalse(cancelled.is_set())

    def test_marker_with_foreign_identity_is_not_completion(self):
        evidence = MemoryVolume({"/r/attempts/t1/complete.json": json.dumps(marker(bundle_sha256="b" * 64)).encode()})
        sandbox = FakeSandbox([None, 0])
        result, _ = self.run_execute(self.campaign(evidence=evidence), sandbox)
        self.assertEqual(result["cloud"]["state"], "exited_without_marker")

    def test_human_cancel_is_delivered_once_and_vm_exit_is_awaited(self):
        cancelled = threading.Event()
        cancelled.set()
        sandbox = FakeSandbox([None, None, None, 0])
        campaign = self.campaign()
        result, _ = self.run_execute(campaign, sandbox, cancelled=cancelled)
        sandbox.filesystem.write_text.assert_called_once_with("cancel\n", "/tmp/whip-eval-cancel")
        self.assertIn("cancel_sent_at", result["cloud"])
        self.assertTrue(result["cleanup"]["complete"])
        self.assertFalse(sandbox.terminated)

    def test_controller_failure_mid_flight_terminates_the_vm_and_records_its_exit(self):
        sandbox = FakeSandbox([None])
        sandbox.set_tags = MagicMock(side_effect=RuntimeError("tag service down"))
        result, _ = self.run_execute(self.campaign(), sandbox)
        self.assertEqual(result["cloud"]["state"], "controller_error")
        self.assertTrue(sandbox.terminated)
        self.assertEqual(result["cloud"]["worker_exit_code"], 137)
        self.assertTrue(result["cleanup"]["complete"])

    def test_interrupted_controller_leaves_the_vm_running(self):
        # Modal preemption interrupts the coordinator; the VM finishes on its own.
        sandbox = FakeSandbox([None])
        sandbox.set_tags = MagicMock(side_effect=KeyboardInterrupt)
        state = MemoryState()
        with self.assertRaises(KeyboardInterrupt):
            self.run_execute(self.campaign(state=state), sandbox)
        record = state.get("r/attempt/t1")
        self.assertEqual(record["state"], "detached")
        self.assertFalse(sandbox.terminated)
        self.assertFalse(record["cleanup_complete"])

    def test_duplicate_attempt_never_creates_a_second_vm(self):
        state = MemoryState({"r/attempt/t1": {"state": "running"}})
        create = MagicMock(side_effect=AssertionError("must not create"))
        result, cancelled = self.run_execute(self.campaign(state=state), None, create=create)
        self.assertEqual(result["error_code"], "attempt_already_claimed")
        self.assertFalse(result["started"])
        self.assertFalse(cancelled.is_set())
        create.assert_not_called()

    def test_create_failure_is_a_recorded_controller_error(self):
        create = MagicMock(side_effect=RuntimeError("platform unavailable"))
        result, cancelled = self.run_execute(self.campaign(), None, create=create)
        self.assertEqual(result["cloud"]["state"], "controller_error")
        self.assertEqual(result["cloud"]["error_code"], "RuntimeError")
        self.assertFalse(result["started"])
        self.assertFalse(result["cleanup"]["complete"])
        self.assertFalse(cancelled.is_set())


class CoordinateTests(unittest.TestCase):
    def inputs(self):
        manifest = {"run_id": "r", "schedule": [trial()], "candidates": [{"id": "quickjs"}]}
        schedule = {"t1": {"envelope": {"outer_watchdog_seconds": 900}}}
        return MemoryVolume({"/r/manifest.json": json.dumps(manifest).encode(),
                             "/r/schedule.json": json.dumps(schedule).encode()})

    def request(self, **updates):
        value = {"bundle_sha256": "a" * 64, "settings": settings(),
                 "controller_source_sha256": cloud.controller_revision()}
        value.update(updates)
        return value

    def coordinate(self, state, pool):
        with patch.object(cloud, "sdk", return_value=fake_sdk()), \
             patch.object(cloud, "resources", return_value=(state, self.inputs(), MemoryVolume())), \
             patch("whip_evals.execution.run_pool", side_effect=pool) as run_pool:
            result = cloud.coordinate("r", "a" * 64, settings(), cloud.controller_revision())
        return result, run_pool

    def test_stale_or_mismatched_deployment_fails_before_dispatch(self):
        for request in (self.request(bundle_sha256="b" * 64), self.request(controller_source_sha256="0" * 64),
                        self.request(settings=settings(jobs=2))):
            with self.subTest(request=request):
                state = MemoryState({"r/request": request})
                result, run_pool = self.coordinate(state, AssertionError("no dispatch"))
                self.assertEqual((result["status"], result["error_code"]), ("failed", "deployment_identity_mismatch"))
                run_pool.assert_not_called()
                self.assertEqual(state.get("r/status")["status"], "failed")

    def test_completed_and_human_cancelled_status_words(self):
        def completed(trials, capacity, jobs, worker, *, cancelled, stats, cancel_on_interrupt):
            self.assertEqual(jobs, 1)
            self.assertFalse(cancel_on_interrupt)  # a preempted coordinator is not a cancel
            return {"t1": {"started": True, "cleanup": {"complete": True}}}
        state = MemoryState({"r/request": self.request()})
        result, _ = self.coordinate(state, completed)
        self.assertEqual(result["status"], "completed")
        self.assertEqual((result["planned"], result["recorded"], result["unproven_vm_exits"]), (1, 1, 0))
        self.assertEqual(state.get("r/status")["status"], "completed")

        def cancelled_run(trials, capacity, jobs, worker, *, cancelled, stats, cancel_on_interrupt):
            cancelled.set()  # a human cancel delivered through the state watcher
            return {"t1": {"started": False, "cancelled": True, "cleanup": {"complete": True}}}
        state = MemoryState({"r/request": self.request()})
        result, _ = self.coordinate(state, cancelled_run)
        self.assertEqual(result["status"], "cancelled")

    def test_second_coordinator_for_the_same_run_does_not_dispatch(self):
        state = MemoryState({"r/request": self.request(), "r/controller": {"call_id": "fc-0"}})
        result, run_pool = self.coordinate(state, AssertionError("no dispatch"))
        self.assertEqual(result["status"], "already_claimed")
        run_pool.assert_not_called()


class FetchTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.evals = Path(self.temporary.name)
        self.manifest = {"run_id": "r", "schedule": [trial(), trial(id="t2", task_id="terminal-bench/other")],
                         "candidates": [{"id": "quickjs"}], "profile": "smoke", "profile_version": 1,
                         "comparison_key": "offline", "seed": 1, "artifact_root": "artifacts/r"}
        self.request = {"run_id": "r", "bundle_sha256": "a" * 64, "planned": 2, "settings": settings()}

    def volumes(self):
        # The observer's last metrics.json survives a worker that never produced a native result.
        interim = json.dumps({"ledger_cost_usd": 1.5, "model_calls": 3, "unknown_cost_calls": 1,
                              "unknown_usage_calls": 0, "event_cursor": 40, "input_tokens": 500}).encode()
        record = json.dumps({"started": True, "job_path": "jobs/t1", "cleanup": {"complete": True}}).encode()
        files = {"artifacts/record.json": record, "artifacts/jobs/t1/native__x/agent/whip/metrics.json": interim,
                 "started.json": b"{}"}
        import hashlib
        inventory = {name: hashlib.sha256(data).hexdigest() for name, data in files.items()}
        evidence = {"/r/attempts/t1/" + name: data for name, data in files.items()}
        evidence["/r/attempts/t1/complete.json"] = json.dumps(marker(files=inventory)).encode()
        evidence["/r/attempts/t2/started.json"] = b"{}"
        inputs = MemoryVolume({"/r/manifest.json": json.dumps(self.manifest).encode(), "/r/bundle.tar": b"tar"})
        return inputs, MemoryVolume(evidence)

    def state(self, status="completed"):
        return MemoryState({"r/request": self.request, "r/status": {"status": status},
                            "r/attempt/t1": {"trial_id": "t1", "sandbox_id": "sb-1", "cleanup_complete": True},
                            "r/attempt/t2": {"trial_id": "t2", "sandbox_id": "sb-2", "cleanup_complete": False}})

    @staticmethod
    def collect(evidence, run_id, root):
        for path, data in evidence.files.items():
            target = Path(root) / path.split("/", 2)[2]
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        return len(evidence.files)

    def fetch(self, state, **kwargs):
        inputs, evidence = self.volumes()
        with patch.object(cloud, "resources", return_value=(state, inputs, evidence)), \
             patch.object(cloud, "sdk", return_value=fake_sdk()), \
             patch.object(modal_cli, "collect_files", side_effect=self.collect):
            return modal_cli.fetch("r", evals=self.evals, **kwargs), inputs, evidence

    def test_fetch_verifies_marker_hashes_retains_partial_cost_and_prunes(self):
        state = self.state()
        outcome, inputs, evidence = self.fetch(state)
        self.assertEqual(outcome["status"], "incomplete")
        self.assertEqual(outcome["retained_files"], 5)
        result = read_json(Path(outcome["report"]).with_name("result.json"))
        first, second = result["trials"]
        self.assertNotIn("cloud_snapshot_incomplete", first["error_codes"])
        self.assertIn("missing_result", first["error_codes"])
        self.assertEqual(first["known_cost_usd"], "1.500000")
        self.assertIsNone(first["cost_usd"])
        self.assertEqual(first["partial_observation"]["observed_input_tokens"], 500)
        self.assertTrue(first["partial_observation"]["path"].startswith("attempts/t1/artifacts/jobs/t1/"))
        self.assertTrue(first["cleanup_complete"])
        self.assertIn("cloud_snapshot_incomplete", second["error_codes"])
        self.assertEqual(second["termination_source"], "cloud_worker_incomplete")
        self.assertFalse(second["cleanup_complete"])
        self.assertTrue(Path(outcome["artifact_root"], "attempts/t1/artifacts/record.json").is_file())
        # Modal keeps nothing but the fetched receipt once the report is written.
        self.assertTrue(outcome["pruned"])
        self.assertEqual(evidence.files, {})
        self.assertEqual(inputs.files, {})
        self.assertEqual(set(state.values), {"r/fetched"})
        self.assertEqual(state.get("r/fetched")["report"], outcome["report"])
        with patch.object(cloud, "resources", return_value=(state, MagicMock(), MagicMock())):
            self.assertEqual(modal_cli.status("r")["status"], "fetched")

    def test_keep_and_unfinished_runs_leave_modal_data_in_place(self):
        outcome, inputs, evidence = self.fetch(self.state(), keep=True)
        self.assertFalse(outcome["pruned"])
        self.assertTrue(evidence.files and inputs.files)
        state = self.state("running")
        outcome, inputs, evidence = self.fetch(state)
        self.assertFalse(outcome["pruned"])
        self.assertIn("not finished", outcome["note"])
        self.assertIsNone(state.get("r/fetched"))
        self.assertTrue(evidence.files)

    def test_tampered_evidence_is_never_verified(self):
        state = self.state()
        inputs, evidence = self.volumes()
        evidence.files["/r/attempts/t1/artifacts/record.json"] = b'{"started": true, "job_path": "jobs/t1", "cleanup": {"complete": true}} '
        with patch.object(cloud, "resources", return_value=(state, inputs, evidence)), \
             patch.object(modal_cli, "collect_files", side_effect=self.collect):
            outcome = modal_cli.fetch("r", evals=self.evals, keep=True)
        result = read_json(Path(outcome["report"]).with_name("result.json"))
        self.assertIn("cloud_snapshot_incomplete", result["trials"][0]["error_codes"])

    def test_prune_refuses_unfinished_runs_without_force(self):
        state = self.state("running")
        inputs, evidence = self.volumes()
        with patch.object(cloud, "resources", return_value=(state, inputs, evidence)), patch.object(cloud, "sdk", return_value=fake_sdk()):
            with self.assertRaisesRegex(ValueError, "not finished"):
                modal_cli.prune("r")
            self.assertTrue(evidence.files)
            self.assertEqual(modal_cli.prune("r", force=True)["state_keys"], 4)
            # Pruning again (data already gone) is a quiet no-op for the volumes.
            state.put("r/request", self.request)
            state.put("r/status", {"status": "completed"})
            self.assertEqual(modal_cli.prune("r")["inputs"], False)
        self.assertEqual(state.values, {})

    def test_collect_uses_the_cli_transfer_and_flattens(self):
        _, evidence = self.volumes()
        commands = []
        def run(command, **kwargs):
            commands.append(command)
            destination = Path(command[-1]) / "r"
            for path, data in evidence.files.items():
                target = destination / path.split("/", 2)[2]
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(data)
            return SimpleNamespace(returncode=0)
        root = self.evals / "collected"
        with patch.object(modal_cli.subprocess, "run", side_effect=run), patch.object(cloud, "sdk", return_value=fake_sdk()):
            count = modal_cli.collect_files(evidence, "r", root)
        self.assertEqual(count, len(evidence.files))
        self.assertEqual(commands[0][1:6], ["-m", "modal", "volume", "get", "--env"])
        self.assertIn("--force", commands[0])
        self.assertEqual(commands[0][-3:], [cloud.EVIDENCE_VOLUME, "/r", str(root)])
        self.assertTrue((root / "attempts/t1/complete.json").is_file())
        self.assertFalse((root / "r").exists())
        failing = subprocess.CalledProcessError(1, "modal", stderr=b"boom")
        with patch.object(modal_cli.subprocess, "run", side_effect=failing), patch.object(modal_cli.time, "sleep"), \
             patch.object(cloud, "sdk", return_value=fake_sdk()), self.assertRaisesRegex(RuntimeError, "boom"):
            modal_cli.collect_files(evidence, "r", self.evals / "collected-3")
        class SdkNotFoundVolume:
            def iterdir(self, prefix, recursive=True):
                raise NotFound("No such file or directory")
        with patch.object(cloud, "sdk", return_value=fake_sdk()):
            self.assertEqual(modal_cli.collect_files(MemoryVolume(), "r", self.evals / "collected-4"), 0)
            self.assertEqual(modal_cli.collect_files(SdkNotFoundVolume(), "r", self.evals / "collected-5"), 0)

    def test_status_cancel_and_cli_surface(self):
        state = self.state("running")
        with patch.object(cloud, "resources", return_value=(state, MagicMock(), MagicMock())):
            value = modal_cli.status("r")
            self.assertEqual((value["status"], value["planned"], len(value["attempts"])), ("running", 2, 2))
            self.assertFalse(value["cancel_requested"])
            cancelled = modal_cli.cancel("r")
            self.assertEqual(cancelled["status"], "cancel_requested")
            self.assertTrue(state.get("r/cancel"))
            with self.assertRaises(ValueError):
                modal_cli.status("unknown")
        args = parser().parse_args(["modal", "fetch", "r", "--keep"])
        self.assertTrue(args.keep)
        self.assertTrue(parser().parse_args(["modal", "prune", "r", "--force"]).force)
        self.assertIsNone(parser().parse_args(["modal", "submit", "full", "--allow-model-calls"]).jobs)
        with self.assertRaises(SystemExit):
            parser().parse_args(["modal", "submit", "full", "--settings", "x.json"])
        for removed in (["modal", "reconcile", "r"], ["modal", "logs", "r"]):
            with self.assertRaises(SystemExit):
                parser().parse_args(removed)


class WorkerTests(unittest.TestCase):
    def exercise(self, root, fail_copy=False):
        from contextlib import ExitStack
        work, evidence, tmp = root / "work", root / "evidence", root / "tmp"
        tmp.mkdir()
        evidence.mkdir()
        native_trial = trial()
        manifest = {"schedule": [native_trial], "protocol": {"headroom": {}, "runner_versions": {}}}
        write_json(work / "evals/reports/r/manifest.json", manifest)
        write_json(work / "evals/artifacts/r/schedule.json", {"t1": {"config": {}, "envelope": {}}})
        artifact_root = evidence.resolve() / "r/attempts/t1/artifacts"
        def paths(value):
            text = str(value)
            return tmp / text.removeprefix("/tmp/") if text.startswith("/tmp/whip-eval") else Path(value)
        def execute(t, config, envelope, directory, cancelled, **kwargs):
            self.assertEqual(directory, tmp / "whip-eval-native/t1")
            self.assertEqual(config["jobs_dir"], str(artifact_root / "jobs"))
            directory.mkdir(parents=True, exist_ok=False)
            write_json(directory / "execution.json", {"closed": True}, exclusive=True)
            (directory / "runner.log").write_text("closed log\n")
            return {"job_path": str(artifact_root / "jobs/native"), "cleanup": {"complete": True}}
        with ExitStack() as stack:
            for name, options in {
                "Path": {"side_effect": paths},
                "environment": {"return_value": {"os": "linux", "emulated": False, "runner_versions": {}}},
                "preflight": {"return_value": None}, "execute_job": {"side_effect": execute},
            }.items():
                stack.enter_context(patch.object(modal_worker, name, **options))
            stack.enter_context(patch("whip_evals.tasks.load_spec", return_value=({"tasks": []}, None, None)))
            if fail_copy:
                stack.enter_context(patch.object(modal_worker.shutil, "copyfileobj", side_effect=OSError("copy failed")))
            return modal_worker.run_worker("r", "t1", "a" * 64, work=work, evidence=evidence)

    def test_worker_publishes_receipts_then_marker_with_file_hashes(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            result = self.exercise(root)
            self.assertEqual(result["status"], "completed")
            attempt = root / "evidence/r/attempts/t1"
            self.assertEqual((attempt / "artifacts/trials/t1/runner.log").read_text(), "closed log\n")
            self.assertEqual(read_json(attempt / "complete.json")["files"], result["files"])
            for name, digest in result["files"].items():
                self.assertEqual(file_hash(attempt / name), digest, name)
            self.assertIn("artifacts/record.json", result["files"])
            self.assertNotIn("complete.json", result["files"])
            self.assertNotIn("row", result)
            self.assertFalse((attempt / "normalized.json").exists())

    def test_receipt_copy_failure_is_recorded_and_still_publishes_the_marker(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            result = self.exercise(root, fail_copy=True)
            self.assertEqual(result["status"], "completed")
            self.assertEqual(result["receipt_error"], "OSError")
            self.assertTrue((root / "evidence/r/attempts/t1/complete.json").exists())

    def test_preflight_checks_resource_fit_only(self):
        host = {"cpus": 6, "memory_bytes": 24 * 1024 ** 3, "disk_free_mb": 100000}
        headroom = {"cpus": 1, "memory_mb": 1024, "storage_mb": 1024}
        modal_worker.preflight(host, trial(), headroom)
        with self.assertRaises(ValueError):
            modal_worker.preflight(host, trial(resources={"cpus": 6, "memory_mb": 2048, "storage_mb": 10240}), headroom)


class PrivateProxyTests(unittest.TestCase):
    def environment(self, directory):
        from whip_evals.adapter import PierDockerEnvironment
        from pier.models.trial.paths import TrialPaths
        value = object.__new__(PierDockerEnvironment)
        value.trial_paths = TrialPaths(directory)
        value.task_env_config = SimpleNamespace(allow_internet=False)
        value.network_allowlist = SimpleNamespace(domains=["api.inference.net"])
        value.environment_dir = directory / "nonexistent-environment"
        value._egress_proxy_compose_path = None
        value._egress_proxy_env = None
        return value

    def test_cloud_proxy_uses_native_generator_only_in_private_directory_until_stop(self):
        import asyncio
        import os
        from whip_evals import adapter
        with tempfile.TemporaryDirectory() as temporary:
            durable = Path(temporary)
            environment = self.environment(durable)
            original = environment.trial_paths
            with patch.dict(os.environ, {"WHIP_EVAL_CLOUD": "1"}), patch("pier.environments.docker.docker.new_proxy_token", return_value="private-proxy-sentinel"):
                environment._prepare_egress_proxy_compose()
            self.assertIs(environment.trial_paths, original)
            private = Path(environment._whip_proxy_directory.name)
            self.assertEqual(private.stat().st_mode & 0o777, 0o700)
            self.assertEqual(list(durable.iterdir()), [])
            compose = json.loads(environment._egress_proxy_compose_path.read_text())
            proxy = compose["services"]["pier-egress-proxy"]
            self.assertEqual(proxy["environment"]["PROXY_TOKEN"], "private-proxy-sentinel")
            self.assertEqual(proxy["ulimits"]["nofile"], {"soft": 65536, "hard": 65536})
            async def native_stop(instance, delete):
                self.assertTrue(private.exists())
                self.assertIs(instance.trial_paths, original)
            with patch.object(adapter.DockerEnvironment, "stop", native_stop):
                asyncio.run(environment.stop(delete=True))
            self.assertFalse(private.exists())
            self.assertEqual(list(durable.iterdir()), [])

    def test_local_native_proxy_path_is_unchanged(self):
        import os
        with tempfile.TemporaryDirectory() as temporary:
            environment = self.environment(Path(temporary))
            with patch.dict(os.environ, {"WHIP_EVAL_CLOUD": ""}), patch("pier.environments.docker.docker.new_proxy_token", return_value="local-test-sentinel"):
                environment._prepare_egress_proxy_compose()
            self.assertEqual(environment._egress_proxy_compose_path.parent, Path(temporary))
            self.assertIsNone(getattr(environment, "_whip_proxy_directory", None))


if __name__ == "__main__":
    unittest.main()
