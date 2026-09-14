"""Cloud fault contracts with no Docker, Modal API, credentials, or model calls."""
import hashlib
import json
from pathlib import Path
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
from whip_evals.common import write_json
from whip_evals.report import empty_trial


class MemoryState:
    def __init__(self, values=None):
        self.values = values or {}

    def get(self, key):
        return self.values.get(key)

    def put(self, key, value, *, skip_if_exists=False):
        if skip_if_exists and key in self.values:
            return False
        self.values[key] = dict(value)
        return True

    def items(self):
        return list(self.values.items())


def settings():
    shape = {"physical_cpus": 6, "memory_mb": 24576, "min_free_disk_mb": 100000}
    return {"app": cloud.APP_NAME, "environment": cloud.ENVIRONMENT,
            "worker_image_id": "im-fixture", "jobs": 1, "fixture": True,
            "qualification_status": "passed", "shapes": {"harbor": shape, "pier": shape}}


def attempt():
    return {"run_id": "r", "trial_id": "t1", "bundle_sha256": "a" * 64,
            "sandbox_name": cloud.sandbox_name("r", "t1"), "sandbox_id": "sb-test",
            "owner_id": "b" * 32, "cleanup_complete": False}


def trial():
    return {"id": "t1", "task_id": "fixture/native", "runner": "harbor",
            "candidate_id": "quickjs", "repetition": 1, "outer_watchdog_seconds": 900,
            "resources": {"cpus": 1, "memory_mb": 2048, "storage_mb": 10240}}


def stream_reader(read):
    def download(path, stream, *, concurrency):
        assert concurrency == 1
        return sum(stream.write(chunk) for chunk in read(path))
    return download


class LateExportAckTests(unittest.TestCase):
    class NotFoundError(Exception):
        pass

    def run_attempt(self, *, polls=None, failure=None, ownership="valid", missing=False,
                    bad_hash=False, flags=None):
        now = [0]
        state, modal, sandbox, volume = MemoryState(), MagicMock(), MagicMock(), MagicMock()
        cancelled = threading.Event()
        modal.exception = SimpleNamespace(NotFoundError=self.NotFoundError)
        modal.Sandbox.create.return_value = modal.Sandbox.from_id.return_value = sandbox
        sandbox.object_id, sandbox.poll.return_value = "sb-owned", 0
        sandbox.poll.side_effect = polls
        sandbox.filesystem.read_text.return_value = "{}"
        def tags():
            if ownership == "unavailable":
                raise self.NotFoundError("tags unavailable")
            if ownership == "wrong_owner" or (ownership == "lost_after_ack" and sandbox.get_tags.call_count > 1):
                return {}
            return sandbox.set_tags.call_args.args[0]
        sandbox.get_tags.side_effect = tags
        foreign = MagicMock(object_id="sb-foreign")
        if ownership == "foreign_id":
            modal.Sandbox.from_id.return_value = foreign
        elif ownership == "foreign_after_ack":
            modal.Sandbox.from_id.side_effect = [sandbox, foreign, foreign]
        if failure:
            operation = sandbox.exec if failure[0] == "exec" else sandbox.exec.return_value.wait
            operation.side_effect = failure[1]
        body = b"closed native evidence with unchanged grade and accounting"
        marker = {"run_id": "r", "trial_id": "t1", "bundle_sha256": "a" * 64,
                  "status": "completed", "integrity_complete": True, "security_failure": False,
                  "accounting_complete": True, "success": False,
                  "files": {"record.json": "0" * 64 if bad_hash else hashlib.sha256(body).hexdigest()},
                  **(flags or {})}
        def advance(seconds):
            now[0] += seconds
        def read(path):
            if path.endswith("/ready.json"):
                return [json.dumps({k: marker[k] for k in ("run_id", "trial_id", "bundle_sha256")}).encode()]
            if path.endswith("/complete.json"):
                if missing:
                    advance(601)
                    raise FileNotFoundError("synthetic absent marker")
                return [json.dumps(marker).encode()]
            advance(601)  # Real durable_completion hashing crosses the unchanged worker wait.
            return [body]
        volume.read_file.side_effect = read
        controller = cloud.ModalCampaign("r", "a" * 64, settings(), state, MagicMock(), volume)
        with patch.object(cloud, "sdk", return_value=modal), \
             patch.object(cloud.time, "monotonic", side_effect=lambda: now[0]), \
             patch.object(cloud.time, "sleep", side_effect=advance):
            result = controller.execute(trial(), cancelled)
        modal.Sandbox.create.assert_called_once()
        self.assertEqual(modal.Sandbox.create.call_args.kwargs["timeout"], trial()["outer_watchdog_seconds"] + 1800)
        self.assertEqual((cloud.EXPORT_SECONDS, cloud.POLL_SECONDS), (600, 5))
        self.assertGreater(now[0], 600)
        row = result["cloud"]
        if not missing and not bad_hash:
            self.assertEqual(row["completion"], marker)
            self.assertEqual(row["evidence_complete"], marker["integrity_complete"])
            self.assertFalse(row["completion"]["success"])  # VM exit is never native grade PASS.
        return SimpleNamespace(row=row, sandbox=sandbox, modal=modal, foreign=foreign,
                               cancelled=cancelled.is_set(), state=state)

    def test_verified_slow_export_actual_exit_skips_ack_and_terminate(self):
        result = self.run_attempt()
        self.assertEqual((result.row["state"], result.row["export_ack_status"], result.row["actual_exit_code"]),
                         ("completed", "obsolete_actual_exit", 0))
        self.assertTrue(result.row["cleanup_complete"])
        self.assertFalse(result.cancelled)
        result.sandbox.exec.assert_not_called()
        result.sandbox.terminate.assert_not_called()
        result.sandbox.wait.assert_not_called()

    def test_dispatch_and_wait_notfound_require_reowned_actual_exit(self):
        for phase in ("exec", "wait"):
            with self.subTest(phase=phase):
                result = self.run_attempt(polls=[None, 0, 0], failure=(phase, self.NotFoundError("exit race")))
                self.assertEqual(result.row["export_ack_status"], "obsolete_after_not_found")
                self.assertEqual(result.row["actual_exit_code"], 0)
                self.assertEqual(result.modal.Sandbox.from_id.call_count, 3)
                self.assertFalse(result.cancelled)
                result.sandbox.exec.assert_called_once_with("touch", "/tmp/whip-eval-exported")
                result.sandbox.terminate.assert_not_called()

    def test_none_bool_or_error_after_notfound_cannot_establish_exit(self):
        for value in (None, True, False, self.NotFoundError("poll missing"), RuntimeError("unknown")):
            with self.subTest(value=type(value).__name__):
                result = self.run_attempt(polls=[None, value, 0], failure=("exec", self.NotFoundError("exit race")))
                self.assertEqual(result.row["state"], "indeterminate")
                self.assertNotIn("export_ack_status", result.row)
                self.assertTrue(result.cancelled)
                result.sandbox.exec.assert_called_once()

    def test_initial_poll_bool_or_exception_is_not_ack_permission(self):
        for value in (True, False, self.NotFoundError("missing"), RuntimeError("unknown")):
            with self.subTest(value=type(value).__name__):
                result = self.run_attempt(polls=[value, 0])
                self.assertEqual(result.row["state"], "indeterminate")
                self.assertTrue(result.cancelled)
                result.sandbox.exec.assert_not_called()

    def test_only_typed_sdk_notfound_can_be_suppressed(self):
        for error in (FileNotFoundError("not SDK NotFound"), RuntimeError("transport error")):
            with self.subTest(error=type(error).__name__):
                result = self.run_attempt(polls=[None, 0], failure=("exec", error))
                self.assertEqual(result.row["state"], "indeterminate")
                self.assertNotIn("export_ack_status", result.row)
                self.assertTrue(result.cancelled)

    def test_foreign_id_owner_or_unavailable_ownership_blocks_all_mutation(self):
        for ownership in ("foreign_id", "wrong_owner", "unavailable"):
            with self.subTest(ownership=ownership):
                result = self.run_attempt(ownership=ownership)
                self.assertEqual(result.row["state"], "indeterminate")
                self.assertFalse(result.row["cleanup_complete"])
                self.assertTrue(result.cancelled)
                result.sandbox.exec.assert_not_called()
                result.sandbox.terminate.assert_not_called()
                result.foreign.poll.assert_not_called()
                result.foreign.terminate.assert_not_called()

    def test_notfound_recheck_must_revalidate_owner_and_exact_id(self):
        for ownership in ("lost_after_ack", "foreign_after_ack"):
            with self.subTest(ownership=ownership):
                result = self.run_attempt(polls=[None], ownership=ownership,
                                          failure=("exec", self.NotFoundError("exit race")))
                self.assertEqual(result.row["state"], "indeterminate")
                self.assertFalse(result.row["cleanup_complete"])
                self.assertTrue(result.cancelled)
                result.sandbox.exec.assert_called_once()
                result.sandbox.terminate.assert_not_called()
                result.foreign.terminate.assert_not_called()

    def test_missing_marker_or_mismatched_hash_never_acknowledges(self):
        for missing in (True, False):
            with self.subTest(missing=missing):
                result = self.run_attempt(missing=missing, bad_hash=not missing)
                result.sandbox.exec.assert_not_called()
                self.assertNotIn("completion", result.row)
                self.assertFalse(result.row["evidence_complete"])

    def test_negative_flags_and_security_stop_survive_obsolete_ack(self):
        for security, integrity, accounting in ((True, False, False), (True, True, True),
                                               (False, False, False), (False, True, False)):
            with self.subTest(security=security, integrity=integrity, accounting=accounting):
                result = self.run_attempt(flags={"security_failure": security, "integrity_complete": integrity,
                                                 "accounting_complete": accounting})
                self.assertEqual(result.row["state"], "security_failure" if security else "completed")
                self.assertEqual(result.cancelled, security or not integrity)
                result.sandbox.exec.assert_not_called()
                if security or not integrity:
                    self.assertEqual(result.state.get("r/cancel")["reason"], "evidence_integrity_failure")

    def test_nonzero_vm_exit_is_metadata_not_grade(self):
        result = self.run_attempt(polls=[17, 17])
        self.assertEqual(result.row["actual_exit_code"], 17)
        self.assertTrue(result.row["cleanup_complete"])
        self.assertFalse(result.cancelled)

    def test_normal_alive_ack_then_cleanup_is_unchanged(self):
        result = self.run_attempt(polls=[None, None, 0])
        self.assertEqual(result.row["export_ack_status"], "acknowledged")
        self.assertTrue(result.row["cleanup_complete"])
        result.sandbox.exec.assert_called_once_with("touch", "/tmp/whip-eval-exported")
        result.sandbox.exec.return_value.wait.assert_called_once_with()
        result.sandbox.terminate.assert_called_once_with()
        result.sandbox.wait.assert_called_once_with(raise_on_termination=False)

    def test_cleanup_poll_error_needs_terminate_wait_and_fresh_actual_exit(self):
        result = self.run_attempt(polls=[None, RuntimeError("initial cleanup poll unknown"), 0])
        self.assertEqual(result.row["cleanup_poll_error"], "RuntimeError")
        self.assertEqual(result.row["actual_exit_code"], 0)
        self.assertTrue(result.row["cleanup_complete"])
        result.sandbox.terminate.assert_called_once_with()
        result.sandbox.wait.assert_called_once_with(raise_on_termination=False)

    def test_cleanup_unknown_retains_termination_but_needs_actual_final_poll(self):
        for value in (None, True, False, self.NotFoundError("missing"), RuntimeError("unknown")):
            with self.subTest(value=type(value).__name__):
                result = self.run_attempt(polls=[None, value, value])
                self.assertEqual(result.row["export_ack_status"], "acknowledged")
                self.assertFalse(result.row["cleanup_complete"])
                self.assertTrue(result.cancelled)
                result.sandbox.terminate.assert_called_once_with()
                result.sandbox.wait.assert_called_once_with(raise_on_termination=False)
                self.assertNotIn("actual_exit_code", result.row)


class CloudTests(unittest.TestCase):
    def test_default_full_is_ninety_and_never_promotes(self):
        args = parser().parse_args(["modal", "submit", "full", "--dry-run"])
        value = modal_cli.submit(args)
        self.assertEqual((len(value["task_ids"]), value["repetitions"], value["trial_count"]), (30, 3, 90))
        self.assertFalse(value["promote"])
        self.assertEqual((value["model"], value["effort"]), ("kimi-k3", "high"))

    def test_settings_restrict_environment_and_secrets(self):
        self.assertEqual(cloud.validate_settings(settings()), settings())
        for extra in ({"environment": "main"}, {"INFERENCE_API_KEY": "sentinel"}, {"qualification_status": "pending"}):
            with self.assertRaises(ValueError):
                cloud.validate_settings(settings() | extra)

    def test_launch_unix_only_and_bundled_source(self):
        command = cloud.launch_command("r", "t1", "a" * 64, "b" * 32)
        self.assertIn("dockerd --host=unix:///var/run/docker.sock", command)
        self.assertNotIn("dockerd-entrypoint", command)
        self.assertNotIn("tcp://", command)
        self.assertIn("PYTHONPATH=/work/evals exec /.uv/.venv/bin/python", command)
        with self.assertRaises(ValueError):
            cloud.launch_command("r;bad", "t1", "a" * 64, "b" * 32)

    def test_admission_observed_guest_not_theoretical_cpu(self):
        with tempfile.TemporaryDirectory() as directory:
            host = {"cpus": 2, "memory_bytes": 24576 * 1024**2, "disk_free_mb": 100000}
            headroom = {"cpus": 2, "memory_mb": 4096, "storage_mb": 10240}
            with self.assertRaises(ValueError):
                modal_worker.preflight(host, trial(), headroom, proc=Path(directory))
            host["cpus"] = 6
            modal_worker.preflight(host, trial(), headroom, proc=Path(directory))
            net = Path(directory) / "net"
            net.mkdir()
            (net / "tcp").write_text("header\n0: 00000000:0947 00000000:0000 0A rest\n")
            with self.assertRaises(ValueError):
                modal_worker.preflight(host, trial(), headroom, proc=Path(directory))

    def test_hash_valid_marker_is_not_integrity_approval(self):
        body = b"receipt"
        marker = {"run_id": "r", "trial_id": "t1", "bundle_sha256": "a" * 64,
                  "integrity_complete": False, "files": {"receipt": hashlib.sha256(body).hexdigest()}}
        volume = MagicMock()
        volume.read_file.side_effect = lambda path: [json.dumps(marker).encode()] if path.endswith("complete.json") else [body]
        result = cloud.durable_completion(volume, "r", "t1", "a" * 64)
        self.assertFalse(result["integrity_complete"])
        marker["files"]["receipt"] = "0" * 64
        with self.assertRaises(ValueError):
            cloud.durable_completion(volume, "r", "t1", "a" * 64)

    def test_late_create_requires_unpredictable_worker_claim(self):
        record = attempt() | {"sandbox_id": None}
        sandbox, modal = MagicMock(), MagicMock()
        sandbox.get_tags.return_value = {}
        sandbox.filesystem.read_text.return_value = json.dumps(record)
        modal.Sandbox.from_name.return_value = sandbox
        with patch.object(cloud, "sdk", return_value=modal):
            self.assertIs(cloud.owned_worker(record, "a" * 64), sandbox)
            sandbox.filesystem.read_text.return_value = json.dumps(record | {"owner_id": "c" * 32})
            with self.assertRaises(ValueError):
                cloud.owned_worker(record, "a" * 64)
        sandbox.terminate.assert_not_called()

    def test_shared_volume_claim_cannot_prove_foreign_vm(self):
        record = attempt() | {"sandbox_id": None}
        sandbox, modal = MagicMock(), MagicMock()
        sandbox.get_tags.return_value = {}
        sandbox.filesystem.read_text.side_effect = lambda path: "{}" if path.startswith("/tmp/") else json.dumps(record)
        modal.Sandbox.from_name.return_value = sandbox
        with patch.object(cloud, "sdk", return_value=modal):
            with self.assertRaises(ValueError):
                cloud.owned_worker(record, "a" * 64)
        sandbox.filesystem.read_text.assert_called_once_with("/tmp/whip-eval-owner.json")
        sandbox.terminate.assert_not_called()

    def test_newer_state_without_cursor_preserves_greater_paid_lower_bound(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_json(root / "artifacts/metrics.json", {"ledger_cost_usd": 1, "model_calls": 1, "event_cursor": 100})
            write_json(root / "artifacts/state.json", {"calls": [{"fixture": True}]})
            with patch("whip_evals.observe.aggregate", return_value={"ledger_cost_usd": 2, "model_calls": 2}):
                row = modal_cli.retain_partial(empty_trial(trial()), root, attempt())
            self.assertEqual(row["known_cost_usd"], "2.000000")
            self.assertIsNone(row["cost_usd"])
            self.assertFalse(row["accounting_complete"])

    def test_fetch_hash_valid_integrity_false_cannot_become_complete(self):
        self.check_rejected_fetch(False, b"{}")
        self.check_rejected_fetch(True, b'{"changed":true}')

    def check_rejected_fetch(self, integrity, record_bytes):
        with tempfile.TemporaryDirectory() as directory:
            marker = {"run_id": "r", "trial_id": "t1", "bundle_sha256": "a" * 64,
                      "integrity_complete": integrity, "files": {"artifacts/record.json": hashlib.sha256(b"{}").hexdigest()}}
            remote = {"r/attempts/t1/artifacts/record.json": record_bytes,
                      "r/attempts/t1/complete.json": json.dumps(marker).encode()}
            volume = MagicMock()
            volume.iterdir.return_value = [SimpleNamespace(path=name, type=1) for name in remote]
            volume.read_file.side_effect = lambda path: [remote[path]]
            volume._read_file_into_fileobj.side_effect = stream_reader(volume.read_file)
            state = MemoryState({"r/request": {"bundle_sha256": "a" * 64}, "r/attempt/t1": attempt()})
            complete = empty_trial(trial()) | {"started": True, "evidence_complete": True,
                        "accounting_complete": True, "cost_usd": "1.250000", "known_cost_usd": "1.250000"}
            manifest = {"schedule": [trial()]}
            with patch.object(cloud, "resources", return_value=(state, None, volume)), patch.object(cloud, "read_remote", return_value=manifest), patch("whip_evals.report.normalize_trial", return_value=complete), patch("whip_evals.report.build_result", side_effect=lambda m, rows: {"status": "partial", "trials": rows}) as build, patch("whip_evals.report.write_report"):
                modal_cli.fetch("r", evals=Path(directory))
            row = build.call_args.args[1][0]
            self.assertFalse(row["evidence_complete"])
            self.assertFalse(row["accounting_complete"])
            self.assertIsNone(row["cost_usd"])
            self.assertEqual(row["known_cost_usd"], "1.250000")
            self.assertIn("cloud_snapshot_incomplete" if integrity else "cloud_integrity_failure", row["error_codes"])

    def test_collection_bounds_logical_streams_and_submission_to_eight(self):
        lock, barrier = threading.Lock(), threading.Barrier(8, timeout=3)
        counts = {"listed": 0, "active": 0, "finished": 0, "peak": 0, "outstanding": 0}
        def entries(*args, **kwargs):
            for index in range(17):
                with lock:
                    counts["listed"] += 1
                    counts["outstanding"] = max(counts["outstanding"], counts["listed"] - counts["finished"])
                yield SimpleNamespace(path=f"r/files/{index}", type=1)
        def read(path):
            with lock:
                counts["active"] += 1
                counts["peak"] = max(counts["peak"], counts["active"])
            try:
                if int(path.rsplit("/", 1)[1]) < 8:
                    barrier.wait()
                yield b"one"
                yield b"two"
            finally:
                with lock:
                    counts["active"] -= 1
                    counts["finished"] += 1
        volume = SimpleNamespace(iterdir=entries, _read_file_into_fileobj=stream_reader(read))
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.assertEqual(modal_cli.collect_files(volume, "r", root), 17)
            self.assertEqual(counts["peak"], 8)
            self.assertLessEqual(counts["outstanding"], 8)
            self.assertEqual(counts["active"], 0)
            self.assertEqual(len(list((root / "files").iterdir())), 17)
            self.assertTrue(all(path.read_bytes() == b"onetwo" for path in (root / "files").iterdir()))

    def test_collection_failed_stream_never_publishes_partial_or_temp_file(self):
        for error in (RuntimeError, FileNotFoundError):
            with self.subTest(error=error), tempfile.TemporaryDirectory() as directory:
                def read(path):
                    yield b"partial"
                    raise error("download failed")
                volume = SimpleNamespace(iterdir=lambda *a, **k: [SimpleNamespace(path="r/file", type=1)],
                                         _read_file_into_fileobj=stream_reader(read))
                with self.assertRaises(error):
                    modal_cli.collect_files(volume, "r", Path(directory))
                self.assertEqual(list(Path(directory).iterdir()), [])
        volume = MagicMock()
        volume.iterdir.side_effect = FileNotFoundError
        with tempfile.TemporaryDirectory() as directory:
            self.assertEqual(modal_cli.collect_files(volume, "r", Path(directory)), 0)
        volume.read_file.assert_not_called()

    def test_collection_rejects_path_escape_and_never_overwrites(self):
        for path in ("other/file", "r/../../outside", "/r//absolute"):
            with self.subTest(path=path), tempfile.TemporaryDirectory() as directory:
                volume = MagicMock()
                volume.iterdir.return_value = [SimpleNamespace(path=path, type=1)]
                with self.assertRaises(ValueError):
                    modal_cli.collect_files(volume, "r", Path(directory))
                volume.read_file.assert_not_called()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "file").write_bytes(b"original")
            volume = SimpleNamespace(iterdir=lambda *a, **k: [SimpleNamespace(path="r/file", type=1)],
                                     _read_file_into_fileobj=stream_reader(lambda path: iter([b"replacement"])))
            with self.assertRaises(FileExistsError):
                modal_cli.collect_files(volume, "r", root)
            self.assertEqual((root / "file").read_bytes(), b"original")
            self.assertEqual(list(root.iterdir()), [root / "file"])

    def test_collection_incompatible_sdk_fails_closed_before_reads(self):
        volume = MagicMock()
        with tempfile.TemporaryDirectory() as directory, patch.object(modal_cli, "version", return_value="1.5.4"):
            with self.assertRaises(RuntimeError):
                modal_cli.collect_files(volume, "r", Path(directory))
        volume.iterdir.assert_not_called()
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(RuntimeError):
                modal_cli.collect_files(SimpleNamespace(), "r", Path(directory))
            incompatible = SimpleNamespace(iterdir=lambda *a, **k: [SimpleNamespace(path="r/file", type=1)],
                                            _read_file_into_fileobj=lambda path, stream: None)
            with self.assertRaises(TypeError):
                modal_cli.collect_files(incompatible, "r", Path(directory))
            self.assertEqual(list(Path(directory).iterdir()), [])

    def test_cloud_cleanup_does_not_inherit_native_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            for native in (True, False, None):
                for worker in (True, False, None):
                    with self.subTest(native=native, worker=worker):
                        ledger = attempt() | {"cleanup_complete": worker}
                        row = empty_trial(trial()) | {"cleanup_complete": native}
                        result = modal_cli.retain_partial(row, directory, ledger)
                        self.assertEqual(result["native_cleanup_complete"], native)
                        self.assertEqual(result["cleanup_complete"], worker is True)
                        self.assertFalse(result["evidence_complete"])
                        self.assertFalse(result["accounting_complete"])
            row = empty_trial(trial()) | {"cleanup_complete": True}
            self.assertFalse(modal_cli.retain_partial(row, directory, {})["cleanup_complete"])

    def test_cloud_cleanup_accepts_only_matching_owned_exit_reconciliation(self):
        proof = attempt() | {"cleanup_complete": True, "ownership_proven": True, "worker_exit_code": 137}
        variants = [(proof, True)]
        for key in ("run_id", "trial_id", "bundle_sha256", "owner_id", "sandbox_id"):
            variants.append((proof | {key: "other"}, False))
        for key, value in (("ownership_proven", False), ("cleanup_complete", False),
                           ("worker_exit_code", None), ("worker_exit_code", False)):
            variants.append((proof | {key: value}, False))
        variants.append((proof | {"worker_exit_code": 0}, True))
        with tempfile.TemporaryDirectory() as directory:
            for reconciliation, expected in variants:
                with self.subTest(reconciliation=reconciliation):
                    row = empty_trial(trial()) | {"cleanup_complete": True}
                    result = modal_cli.retain_partial(row, directory, attempt(), reconciliation)
                    self.assertEqual(result["cleanup_complete"], expected)
                    self.assertTrue(result["native_cleanup_complete"])
            late = attempt() | {"sandbox_id": None}
            row = modal_cli.retain_partial(empty_trial(trial()), directory, late, proof)
            self.assertTrue(row["cleanup_complete"])
            missing_owner = attempt() | {"owner_id": None}
            row = modal_cli.retain_partial(empty_trial(trial()), directory, missing_owner, proof)
            self.assertFalse(row["cleanup_complete"])

    def test_fetch_uses_owned_exit_reconciliation_after_dead_coordinator(self):
        state = MemoryState({"r/request": {"bundle_sha256": "a" * 64}, "r/attempt/t1": attempt(),
            "r/reconciliation/t1": attempt() | {"cleanup_complete": True,
                "ownership_proven": True, "worker_exit_code": 137}})
        volume = MagicMock()
        volume.iterdir.return_value = []
        with tempfile.TemporaryDirectory() as directory, patch.object(
                cloud, "resources", return_value=(state, None, volume)), patch.object(
                cloud, "read_remote", return_value={"schedule": [trial()]}), patch(
                "whip_evals.report.build_result", side_effect=lambda m, rows: {"status": "partial", "trials": rows}) as build, patch(
                "whip_evals.report.write_report"):
            modal_cli.fetch("r", evals=Path(directory))
        row = build.call_args.args[1][0]
        self.assertTrue(row["cleanup_complete"])
        self.assertIsNone(row["native_cleanup_complete"])
        self.assertFalse(row["evidence_complete"])
        self.assertFalse(row["accounting_complete"])

    def test_required_identity_rejects_old_three_argument_deployment(self):
        invoked = []
        def old_coordinate(run_id, digest, settings):
            invoked.append(run_id)
        with self.assertRaises(TypeError):
            old_coordinate("r", "a" * 64, settings(), "revision")
        self.assertEqual(invoked, [])
        with self.assertRaises(TypeError):
            cloud.coordinate("r", "a" * 64, settings())

    def test_stale_deployed_controller_fails_before_dispatch(self):
        state = MemoryState({"r/request": {"bundle_sha256": "a" * 64, "settings": settings(),
                                         "controller_source_sha256": "stale"}})
        with patch.object(cloud, "sdk", return_value=MagicMock()), patch.object(cloud, "resources", return_value=(state, None, None)), patch.object(cloud.ModalCampaign, "execute") as execute:
            result = cloud.coordinate("r", "a" * 64, settings(), "stale")
        self.assertEqual(result["error_code"], "deployment_identity_mismatch")
        self.assertEqual(result["started_trials"], 0)
        execute.assert_not_called()

    def test_duplicate_attempt_never_creates(self):
        state = MemoryState({"r/attempt/t1": attempt()})
        controller = cloud.ModalCampaign("r", "a" * 64, settings(), state, None, None)
        modal = MagicMock()
        cancelled = threading.Event()
        with patch.object(cloud, "sdk", return_value=modal):
            result = controller.execute(trial(), cancelled)
        modal.Sandbox.create.assert_not_called()
        self.assertTrue(cancelled.is_set())
        self.assertEqual(result["error_code"], "attempt_already_claimed")

    def test_audit_failure_stops_admission_and_keeps_status(self):
        state = MemoryState()
        controller = cloud.ModalCampaign("r", "a" * 64, settings(), state, MagicMock(), MagicMock())
        modal, sandbox = MagicMock(), MagicMock()
        modal.Sandbox.create.return_value = sandbox
        sandbox.object_id = "sb-owned"
        modal.Sandbox.from_id.return_value = sandbox
        sandbox.get_tags.side_effect = lambda: sandbox.set_tags.call_args.args[0]
        sandbox.poll.return_value = 0
        cancelled = threading.Event()
        marker = {"integrity_complete": False, "security_failure": True}
        modal.exception = SimpleNamespace(NotFoundError=FileNotFoundError)
        with patch.object(cloud, "sdk", return_value=modal), patch.object(cloud, "read_remote", side_effect=FileNotFoundError), patch.object(cloud, "durable_completion", return_value=marker):
            controller.execute(trial(), cancelled)
        self.assertTrue(cancelled.is_set())
        self.assertEqual(state.get("r/cancel")["reason"], "evidence_integrity_failure")
        self.assertEqual(state.get("r/attempt/t1")["state"], "security_failure")
        self.assertFalse(state.get("r/attempt/t1")["evidence_complete"])

    def test_missing_or_wrong_ready_never_admits_native_work(self):
        for ready in (None, {"run_id": "foreign", "trial_id": "t1", "bundle_sha256": "a" * 64}):
            with self.subTest(ready=ready):
                state, modal, sandbox = MemoryState(), MagicMock(), MagicMock()
                modal.Sandbox.create.return_value = sandbox
                modal.exception = SimpleNamespace(NotFoundError=FileNotFoundError)
                sandbox.object_id, sandbox.poll.return_value = "sb-owned", 0
                controller = cloud.ModalCampaign("r", "a" * 64, settings(), state, MagicMock(), MagicMock())
                with patch.object(cloud, "sdk", return_value=modal), patch.object(cloud, "read_remote", return_value=ready, side_effect=FileNotFoundError if ready is None else None), patch.object(cloud, "durable_completion", return_value={"integrity_complete": False}):
                    controller.execute(trial(), threading.Event())
                sandbox.filesystem.write_text.assert_not_called()
                self.assertNotIn("admitted_at", state.get("r/attempt/t1"))

    def test_reconcile_ack_requires_owned_worker_and_verified_final_bytes(self):
        for owned, valid in ((True, True), (True, False), (False, True)):
            with self.subTest(owned=owned, valid=valid):
                record = attempt()
                state = MemoryState({"r/request": {"bundle_sha256": "a" * 64, "planned": 1}, "r/attempt/t1": record})
                sandbox = MagicMock()
                sandbox.poll.return_value = None
                with patch.object(cloud, "resources", return_value=(state, None, None)), patch.object(cloud, "owned_worker", return_value=sandbox, side_effect=None if owned else ValueError), patch.object(cloud, "durable_completion", return_value={"status": "completed"}, side_effect=None if valid else ValueError):
                    modal_cli.status("r", reconcile=True)
                if owned and valid:
                    sandbox.filesystem.write_text.assert_called_once_with("verified\n", "/tmp/whip-eval-exported")
                else:
                    sandbox.filesystem.write_text.assert_not_called()
                sandbox.terminate.assert_not_called()

    def test_reconcile_missing_final_marker_never_acknowledges(self):
        record = attempt()
        state = MemoryState({"r/request": {"bundle_sha256": "a" * 64, "planned": 1},
                             "r/attempt/t1": record})
        sandbox, volume = MagicMock(), MagicMock()
        sandbox.object_id = "sb-test"
        sandbox.poll.return_value = None
        volume.read_file.side_effect = FileNotFoundError("final marker not committed")
        with patch.object(cloud, "resources", return_value=(state, None, volume)), \
             patch.object(cloud, "owned_worker", return_value=sandbox):
            result = modal_cli.status("r", reconcile=True)
        row = result["attempts"][0]
        self.assertTrue(row["ownership_proven"])
        self.assertFalse(row["durable_result"])
        self.assertFalse(row["cleanup_complete"])
        self.assertEqual(row["reconcile_error"], "FileNotFoundError")
        self.assertNotIn("final_ack_at", row)
        self.assertNotIn("final_ack_at", state.get("r/reconciliation/t1"))
        volume.read_file.assert_called_once_with("/r/attempts/t1/complete.json")
        sandbox.filesystem.write_text.assert_not_called()
        sandbox.terminate.assert_not_called()

    def test_reconcile_hash_verified_invalid_evidence_acknowledges_collection_only(self):
        body = b"invalid evidence classification is retained"
        marker = {"run_id": "r", "trial_id": "t1", "bundle_sha256": "a" * 64,
                  "status": "security_failure", "security_failure": True,
                  "integrity_complete": False, "accounting_complete": False,
                  "files": {"receipt.json": hashlib.sha256(body).hexdigest()}}
        record = attempt() | {"state": "committed", "completion": marker}
        state = MemoryState({"r/request": {"bundle_sha256": "a" * 64, "planned": 1},
                             "r/status": {"status": "partial"}, "r/attempt/t1": record})
        sandbox, volume = MagicMock(), MagicMock()
        sandbox.object_id = "sb-test"
        sandbox.poll.return_value = None
        remote = {"/r/attempts/t1/complete.json": json.dumps(marker).encode(),
                  "/r/attempts/t1/receipt.json": body}
        volume.read_file.side_effect = lambda path: [remote[path]]
        with patch.object(cloud, "resources", return_value=(state, None, volume)), \
             patch.object(cloud, "owned_worker", return_value=sandbox):
            result = modal_cli.status("r", reconcile=True)
        row = result["attempts"][0]
        self.assertTrue(row["ownership_proven"])
        self.assertTrue(row["durable_result"])
        self.assertIn("final_ack_at", row)
        self.assertFalse(row["cleanup_complete"])
        self.assertEqual(row["worker_status"], "security_failure")
        self.assertEqual(result["status"], "partial")
        retained = state.get("r/reconciliation/t1")["completion"]
        self.assertFalse(retained["integrity_complete"])
        self.assertFalse(retained["accounting_complete"])
        self.assertTrue(retained["security_failure"])
        self.assertEqual(state.get("r/attempt/t1")["completion"], marker)
        self.assertEqual(retained, marker)
        volume.read_file.assert_any_call("/r/attempts/t1/receipt.json")
        sandbox.filesystem.write_text.assert_called_once_with("verified\n", "/tmp/whip-eval-exported")
        sandbox.terminate.assert_not_called()

    def test_foreign_name_after_lost_create_is_never_terminated(self):
        state = MemoryState()
        controller = cloud.ModalCampaign("r", "a" * 64, settings(), state, MagicMock(), MagicMock())
        modal, sandbox = MagicMock(), MagicMock()
        modal.Sandbox.create.side_effect = RuntimeError("sentinel secret must not be retained")
        modal.Sandbox.from_name.return_value = sandbox
        sandbox.get_tags.return_value = {}
        sandbox.filesystem.read_text.return_value = "{}"
        with patch.object(cloud, "sdk", return_value=modal):
            controller.execute(trial(), threading.Event())
        sandbox.terminate.assert_not_called()
        self.assertNotIn("sentinel", json.dumps(state.values))
        self.assertFalse(state.get("r/attempt/t1")["cleanup_complete"])

    def test_cancel_independent_of_dead_coordinator_and_reconciles_late_create(self):
        record = attempt() | {"sandbox_id": None}
        state = MemoryState({"r/request": {"planned": 90, "bundle_sha256": "a" * 64}, "r/attempt/t1": record})
        sandbox = MagicMock()
        sandbox.object_id = "sb-late"
        sandbox.poll.return_value = None
        with patch.object(cloud, "resources", return_value=(state, None, None)), patch.object(cloud, "owned_worker", return_value=sandbox), patch.object(cloud, "durable_completion", side_effect=FileNotFoundError):
            result = modal_cli.cancel("r")
        self.assertEqual(result["attempts"][0]["sandbox_id"], "sb-late")
        sandbox.filesystem.write_text.assert_called_once_with("cancel\n", "/tmp/whip-eval-cancel")
        self.assertTrue(state.get("r/cancel"))
        self.assertNotIn("r/controller", state.values)

    def test_partial_paid_trial_without_native_result_keeps_observed_bill(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            write_json(root / "artifacts/jobs/t1/native/agent/metrics.interim.json", {
                "ledger_cost_usd": 1.25, "model_calls": 2, "unknown_cost_calls": 1,
                "input_tokens": 100, "output_tokens": 20, "event_cursor": 42})
            row = modal_cli.retain_partial(empty_trial(trial()), root, attempt())
            self.assertTrue(row["started"])
            self.assertEqual(row["known_cost_usd"], "1.250000")
            self.assertIsNone(row["cost_usd"])
            self.assertFalse(row["accounting_complete"])
            self.assertFalse(row["evidence_complete"])
            self.assertIsNone(row["input_tokens"])
            self.assertEqual(row["partial_observation"]["observed_input_tokens"], 100)


class ReceiptPublicationTests(unittest.TestCase):
    def exercise(self, root, fail_copy, root_alias=False):
        from contextlib import ExitStack
        work, evidence, tmp = root / "work", root / "evidence", root / "tmp"
        tmp.mkdir()
        evidence.mkdir()
        if root_alias:
            canonical = root / "canonical-volume"
            evidence.rename(canonical)
            evidence.symlink_to(canonical, target_is_directory=True)
        (tmp / "whip-eval-admitted").touch()
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
                "Path": {"side_effect": paths}, "verify_bundle": {"return_value": None},
                "environment": {"return_value": {"os": "linux", "emulated": False, "runner_versions": {}}},
                "preflight": {"return_value": None}, "execute_job": {"side_effect": execute},
                "normalize_trial": {"return_value": {"accounting_complete": False}},
            }.items():
                stack.enter_context(patch.object(modal_worker, name, **options))
            stack.enter_context(patch("whip_evals.tasks.load_spec", return_value=({"tasks": []}, None, None)))
            if fail_copy:
                stack.enter_context(patch.object(modal_worker.shutil, "copyfileobj", side_effect=OSError("copy failed")))
            return modal_worker.run_worker("r", "t1", "a" * 64, work=work, evidence=evidence)

    def test_closed_ext4_receipts_are_hashed_after_copy(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            result = self.exercise(root, False)
            self.assertEqual(result["status"], "completed")
            self.assertIn("artifacts/trials/t1/runner.log", result["files"])
            self.assertIn("artifacts/trials/t1/execution.json", result["files"])
            self.assertEqual((root / "evidence/r/attempts/t1/artifacts/trials/t1/runner.log").read_text(), "closed log\n")

    def test_trusted_volume_alias_is_canonical_before_native_paths(self):
        with tempfile.TemporaryDirectory() as temporary:
            result = self.exercise(Path(temporary), False, root_alias=True)
            self.assertEqual(result["status"], "completed")
            self.assertNotIn("error_code", result)

    def test_adapter_still_rejects_untrusted_content_descendant_symlink(self):
        import asyncio
        from whip_evals.adapter import download_content
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            logs, outside = root / "logs", root / "outside"
            logs.mkdir()
            outside.mkdir()
            (logs / "content").symlink_to(outside, target_is_directory=True)
            environment = MagicMock()
            with self.assertRaisesRegex(ValueError, "must not follow symlinks"):
                asyncio.run(download_content(environment, logs, {"bodies": [{"digest": "a" * 64, "bytes": 1}]}))
            environment.download_dir.assert_not_called()

    def test_receipt_copy_failure_cannot_publish_final_marker(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with self.assertRaises(OSError):
                self.exercise(root, True)
            self.assertFalse((root / "evidence/r/attempts/t1/complete.json").exists())
            self.assertTrue((root / "evidence/r/attempts/t1/ready.json").exists())


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
        import inspect
        import os
        from whip_evals import adapter
        with tempfile.TemporaryDirectory() as temporary:
            durable = Path(temporary)
            environment = self.environment(durable)
            original = environment.trial_paths
            self.assertFalse(inspect.iscoroutinefunction(adapter.DockerEnvironment._prepare_egress_proxy_compose))
            with patch.dict(os.environ, {"WHIP_EVAL_OWNER_ID": "owned-fixture"}), patch("pier.environments.docker.docker.new_proxy_token", return_value="private-proxy-sentinel"):
                environment._prepare_egress_proxy_compose()
            self.assertIs(environment.trial_paths, original)
            private = Path(environment._whip_proxy_directory.name)
            self.assertEqual(private.stat().st_mode & 0o777, 0o700)
            self.assertEqual(list(durable.iterdir()), [])
            compose = json.loads(environment._egress_proxy_compose_path.read_text())
            proxy = compose["services"]["pier-egress-proxy"]
            self.assertEqual(proxy["environment"]["PROXY_TOKEN"], "private-proxy-sentinel")
            self.assertEqual(proxy["ulimits"]["nofile"], {"soft": 65536, "hard": 65536})
            self.assertEqual(compose["networks"]["pier-egress-internal"], {"internal": True})
            async def native_stop(instance, delete):
                self.assertTrue(private.exists())
                self.assertIs(instance.trial_paths, original)
            with patch.object(adapter.DockerEnvironment, "stop", native_stop):
                asyncio.run(environment.stop(delete=True))
            self.assertFalse(private.exists())
            self.assertEqual(list(durable.iterdir()), [])

    def test_paths_restore_and_private_cleanup_on_native_exception(self):
        import os
        from whip_evals import adapter
        with tempfile.TemporaryDirectory() as temporary:
            environment = self.environment(Path(temporary))
            original, touched = environment.trial_paths, []
            def fail(instance):
                touched.append(instance.trial_paths.trial_dir)
                (touched[-1] / "private").write_text("private-proxy-sentinel")
                raise ValueError("native fixture failure")
            with patch.dict(os.environ, {"WHIP_EVAL_OWNER_ID": "owned-fixture"}), patch.object(adapter.DockerEnvironment, "_prepare_egress_proxy_compose", fail):
                with self.assertRaises(ValueError):
                    environment._prepare_egress_proxy_compose()
            self.assertIs(environment.trial_paths, original)
            self.assertFalse(touched[0].exists())
            self.assertEqual(list(original.trial_dir.iterdir()), [])

    def test_local_native_proxy_path_is_unchanged(self):
        import os
        with tempfile.TemporaryDirectory() as temporary:
            environment = self.environment(Path(temporary))
            with patch.dict(os.environ, {"WHIP_EVAL_OWNER_ID": ""}), patch("pier.environments.docker.docker.new_proxy_token", return_value="local-test-sentinel"):
                environment._prepare_egress_proxy_compose()
            self.assertEqual(environment._egress_proxy_compose_path.parent, Path(temporary))
            self.assertIsNone(getattr(environment, "_whip_proxy_directory", None))


if __name__ == "__main__":
    unittest.main()
