"""Offline immutable campaign and adversarial archive checks; never launch jobs."""
import copy
import io
import os
from pathlib import Path
import shutil
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

from whip_evals.common import EVALS, file_hash, read_json, value_hash, write_json
from whip_evals.modal_bundle import (CONTROLLER_MODULES, controller_revision, create_bundle,
                                    plan_campaign, prepare_campaign, verify_bundle)
from whip_evals.tasks import file_inventory, load_spec


class BundleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.payload = self.root / "payload"
        (self.payload / "evals").mkdir(parents=True)
        self.input = self.payload / "evals" / "input"
        self.input.write_bytes(b"frozen input")
        self.input.chmod(0o664)

    def bundle(self, name="bundle.tar"):
        archive = self.root / name
        inventory = create_bundle(self.payload, archive, paths=["evals/input"])
        return archive, inventory

    def test_deterministic_allowlist_hashes_modes_and_extraction(self):
        (self.payload / "evals" / "credentials.json").write_text("NOT INCLUDED")
        first, inventory = self.bundle()
        os.utime(self.input, (123, 456))
        second, other = self.bundle("second.tar")
        self.assertEqual(first.read_bytes(), second.read_bytes())
        self.assertEqual(inventory, other)
        self.assertEqual(set(inventory["files"]), {"evals/input"})
        self.assertEqual(verify_bundle(first, file_hash(first)), inventory)
        self.assertEqual(inventory["files"]["evals/input"], file_hash(self.input))
        with tarfile.open(first) as archive:
            self.assertEqual(archive.getmember("evals/input").mode, 0o664)  # tar keeps the mode itself
        with self.assertRaises(FileExistsError):
            self.bundle()

    def test_allowlist_rejects_symlink_and_nonregular_inputs(self):
        for kind in ("symlink", "fifo", "directory"):
            self.input.unlink(missing_ok=True)
            if kind == "symlink":
                self.input.symlink_to(self.root / "outside")
            elif kind == "fifo":
                os.mkfifo(self.input)
            else:
                self.input.mkdir()
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                self.bundle()
            if kind == "directory":
                self.input.rmdir()
        with self.assertRaises(ValueError):
            create_bundle(self.payload, self.root / "x.tar", paths=["evals/../outside"])

    def test_archive_tampering_missing_extra_duplicate_and_unsafe_entries(self):
        good, inventory = self.bundle()
        cases = [("evals/input", tarfile.REGTYPE, b"changed"),
                 ("evals/extra", tarfile.REGTYPE, b"extra"),
                 ("../outside", tarfile.REGTYPE, b"x"),
                 ("/evals/input", tarfile.REGTYPE, b"x"),
                 ("evals/../outside", tarfile.REGTYPE, b"x"),
                 ("evals//input", tarfile.REGTYPE, b"x"),
                 ("evals/input", tarfile.SYMTYPE, b""),
                 ("evals/input", tarfile.LNKTYPE, b""),
                 ("evals/input", tarfile.FIFOTYPE, b""),
                 ("evals/input", tarfile.CHRTYPE, b""),
                 ("evals/input", tarfile.DIRTYPE, b"")]
        for index, (name, kind, data) in enumerate(cases):
            bad = self.root / f"bad-{index}.tar"
            with tarfile.open(bad, "w") as archive:
                info = tarfile.TarInfo(name)
                info.mode, info.type, info.size = 0o664, kind, len(data)
                info.linkname = "../../outside" if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE) else ""
                archive.addfile(info, io.BytesIO(data))
            with self.subTest(name=name, kind=kind), self.assertRaises(ValueError):
                verify_bundle(bad, inventory)
            if kind != tarfile.REGTYPE or name != "evals/input" and name != "evals/extra":
                with self.assertRaises(ValueError):
                    verify_bundle(bad, file_hash(bad))
        with self.assertRaises(ValueError):
            verify_bundle(good, "0" * 64)
        missing = copy.deepcopy(inventory)
        missing["files"]["evals/missing"] = missing["files"]["evals/input"]
        with self.assertRaises(ValueError):
            verify_bundle(good, missing)
        duplicate = self.root / "duplicate.tar"
        with tarfile.open(duplicate, "w") as archive:
            for _ in range(2):
                info = tarfile.TarInfo("evals/input")
                info.mode, info.size = 0o664, 12
                archive.addfile(info, io.BytesIO(b"frozen input"))
        with self.assertRaises(ValueError):
            verify_bundle(duplicate, file_hash(duplicate))


class CampaignTests(unittest.TestCase):
    def args(self, **updates):
        result = dict(profile="full", engines="starlark", ref=None, repetitions=3,
                      jobs=90, seed=42, label=None, run_id="offline-modal", dry_run=False)
        result.update(updates)
        return SimpleNamespace(**result)

    def test_plan_is_distinct_and_preserves_native_limits(self):
        planned = plan_campaign(self.args())
        self.assertEqual(planned["jobs"], 90)
        self.assertEqual(planned["trial_count"], 90)
        self.assertFalse(planned["promote"])
        self.assertEqual(planned["limits"], load_spec()[2]["limits"])
        for overrides in ({"jobs": 91}, {"jobs": 0}, {"jobs": True}, {"promote": True}):
            with self.subTest(overrides=overrides), self.assertRaises(ValueError):
                plan_campaign(self.args(**overrides))

    def test_prepares_portable_native_jobs_without_docker_or_model_execution(self):
        from whip_evals.prepare import configuration
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            evals = root / "evals"
            shutil.copytree(EVALS / "frontier", evals / "frontier")
            for name in ("pyproject.toml", "uv.lock"):
                shutil.copyfile(EVALS / name, evals / name)
            binary = evals / "cache/builds" / ("a" * 64) / "whip-linux-amd64"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"fake binary")
            binary.chmod(0o755)
            rg = binary.with_name("rg-linux-amd64")
            rg.write_bytes(b"fake rg")
            rg.chmod(0o755)
            write_json(binary.with_name("build.json"), {"recipe": {}})
            binary.with_name("source.tar").write_bytes(b"PRIVATE SOURCE NEVER STAGED")
            (evals / "credentials.json").write_text("NEVER STAGED")
            metadata = dict(id="candidate", engine="starlark", commit="b" * 40, dirty=True,
                source_sha256="c" * 64, binary_sha256=file_hash(binary),
                binary_path=binary.relative_to(evals).as_posix(),
                build={"ripgrep_sha256": file_hash(rg)}, configuration=configuration("starlark"))

            def prepare(lock, tasks, cache):
                result = {}
                for task in tasks:
                    data = (f'version="1.0"\n[verifier]\ntimeout_sec={task["verifier_seconds"]}\n').encode()
                    files = {"task.toml": (data, 0o664)}
                    digest = value_hash(file_inventory(files))
                    path = cache / digest
                    path.mkdir(parents=True, exist_ok=True)
                    (path / "task.toml").write_bytes(data)
                    (path / "task.toml").chmod(0o664)
                    result[task["id"]] = {"path": str(path), "sha256": digest,
                        "resolved_native": {"agent": {}, "environment": {}, "verifier": {}}}
                return result

            model = dict(id="kimi-k3", context_length=10000, max_completion_tokens=1000,
                         reasoning_efforts=["high"], pricing={})
            with patch("whip_evals.prepare.build_candidate", return_value=metadata), \
                 patch("whip_evals.prepare.catalog", return_value=model), \
                 patch("whip_evals.tasks.prepare_tasks", side_effect=prepare), \
                 patch("whip_evals.prepare.environment", side_effect=AssertionError("Docker info")), \
                 patch("whip_evals.prepare.pull_images", side_effect=AssertionError("Docker pull")), \
                 patch("subprocess.run", side_effect=AssertionError("execution")), \
                 patch("urllib.request.urlopen", side_effect=AssertionError("network")):
                settings = {"environment": "whipcode", "app": "whip-eval", "worker_image_id": "im-offline",
                    "max_jobs": 90, "jobs": 90, "shapes": {runner: {"physical_cpus": 8, "memory_mb": 32768,
                    "min_free_disk_mb": 102400} for runner in ("harbor", "pier")}}
                campaign = prepare_campaign(self.args(execution_settings=settings), evals=evals, repo=root)
            manifest = read_json(campaign / "manifest.json")
            native = load_spec(evals)[2]
            for key, value in settings.items():
                self.assertEqual(manifest["protocol"]["execution"][key], value)
                self.assertEqual(manifest["comparison"]["environment"][key], value)
            self.assertNotEqual(manifest["track"], native["track"])
            for key in ("model", "effort", "limits", "network", "automatic_task_retries"):
                self.assertEqual(manifest["protocol"][key], native[key])
            self.assertEqual(manifest["jobs"], 90)
            self.assertEqual(len(manifest["schedule"]), 90)
            self.assertFalse(manifest["promote"])
            inputs = read_json(campaign / "schedule.json")
            self.assertEqual(set(inputs), {trial["id"] for trial in manifest["schedule"]})
            self.assertEqual(manifest["controller_source_sha256"], controller_revision())
            self.assertEqual(CONTROLLER_MODULES, ("modal_cloud.py", "common.py", "execution.py"))
            for trial in manifest["schedule"]:
                config = inputs[trial["id"]]["config"]
                self.assertEqual(config["environment"]["type"], "docker")
                self.assertEqual(config["retry"], {"max_retries": 0})
                self.assertEqual(config["jobs_dir"], "/work/evals/artifacts/offline-modal/jobs")
                self.assertTrue(config["tasks"][0]["path"].startswith("/work/evals/cache/tasks/"))
                kwargs = config["agents"][0]["kwargs"]
                self.assertEqual(kwargs["max_cost"], 0)
                self.assertTrue(kwargs["native_defaults"])
                self.assertTrue(kwargs["binary"].startswith("/work/evals/cache/builds/"))
            inventory = read_json(campaign / "bundle.json")
            verify_bundle(campaign / "bundle.tar", inventory["sha256"])
            self.assertFalse(any("source.tar" in name or "credentials" in name for name in inventory["files"]))
            self.assertIn("evals/artifacts/offline-modal/schedule.json", inventory["files"])
            self.assertNotIn("evals/artifacts/offline-modal/inputs.json", inventory["files"])
            self.assertNotIn("evals/artifacts/offline-modal/manifest.json", inventory["files"])
            self.assertFalse(any("fixture_provider" in name or "integrity" in name or name.endswith("uv.lock") for name in inventory["files"]))
            with self.assertRaises(FileExistsError):
                prepare_campaign(self.args(), evals=evals, repo=root)


if __name__ == "__main__":
    unittest.main()
