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
from whip_evals.modal_bundle import (create_bundle, extract_bundle, plan_campaign,
                                    prepare_campaign, prepare_fixture_campaign, verify_bundle)
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
        target = extract_bundle(first, self.root / "work", file_hash(first))
        self.assertEqual((target / "evals/input").read_bytes(), b"frozen input")
        self.assertEqual((target / "evals/input").stat().st_mode & 0o777, 0o664)
        with self.assertRaises(FileExistsError):
            extract_bundle(first, target, inventory)
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
            target = self.root / f"out-{index}"
            with self.subTest(name=name, kind=kind), self.assertRaises(ValueError):
                extract_bundle(bad, target, inventory)
            self.assertFalse(target.exists())
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

    def test_fixture_repetitions_are_explicit_and_capped_at_90_trials(self):
        default = plan_campaign(self.args(fixture=True, engines="quickjs"))
        self.assertEqual((default["repetitions"], default["trial_count"]), (1, 2))
        planned = plan_campaign(self.args(fixture=True, engines="quickjs", fixture_repetitions=45))
        self.assertEqual((planned["repetitions"], planned["trial_count"]), (45, 90))
        self.assertEqual(planned["task_ids"], default["task_ids"])
        self.assertTrue(planned["excluded_from_scores"])
        paired = plan_campaign(self.args(fixture=True, engines="starlark,quickjs", fixture_repetitions=22))
        self.assertEqual(paired["trial_count"], 88)
        for repetitions, engines in ((0, "quickjs"), (46, "quickjs"), (True, "quickjs"),
                                     (1.5, "quickjs"), ("2", "quickjs"), (None, "quickjs"),
                                     (23, "starlark,quickjs")):
            with self.subTest(repetitions=repetitions, engines=engines), self.assertRaises(ValueError):
                plan_campaign(self.args(fixture=True, engines=engines, fixture_repetitions=repetitions))
        scored = plan_campaign(self.args(fixture_repetitions=45))
        self.assertEqual((scored["repetitions"], scored["trial_count"]), (3, 90))

    def test_authored_fixture_uses_both_runners_without_catalog_or_real_key(self):
        self.check_fixture_preparation()

    def test_prepares_90_fake_only_native_fixture_trials(self):
        self.check_fixture_preparation(engines="quickjs", repetitions=45)

    def test_fixture_task_subset_is_validated_before_preparation(self):
        for selected in ([], ["fixture/unknown"], ["fixture/harbor-shared"] * 2,
                         "fixture/harbor-shared", [None], [["fixture/harbor-shared"]]):
            with self.subTest(selected=selected), self.assertRaises(ValueError):
                plan_campaign(self.args(fixture=True, fixture_task_ids=selected))
        for selected in (["fixture/harbor-shared"], ["fixture/pier-separate"]):
            planned = plan_campaign(self.args(fixture=True, engines="quickjs", fixture_task_ids=selected))
            self.assertEqual(planned["task_ids"], selected)
            self.assertEqual(planned["trial_count"], 1)
            self.assertTrue(planned["excluded_from_scores"])

    def test_single_authored_harbor_fixture_is_frozen_before_hashing(self):
        self.check_fixture_preparation(engines="quickjs", task_ids=["fixture/harbor-shared"])

    def check_fixture_preparation(self, *, engines=None, repetitions=1, task_ids=None):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            evals = root / "evals"
            shutil.copytree(EVALS / "frontier", evals / "frontier")
            for name in ("pyproject.toml", "uv.lock"):
                shutil.copyfile(EVALS / name, evals / name)
            binary = evals / "cache/builds" / ("f" * 64) / "whip-linux-amd64"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"offline binary")
            rg = binary.with_name("rg-linux-amd64")
            rg.write_bytes(b"offline ripgrep")
            write_json(binary.with_name("build.json"), {})
            metadata = dict(id="candidate", engine="starlark", commit="a" * 40, dirty=False,
                source_sha256="b" * 64, binary_sha256=file_hash(binary),
                binary_path=binary.relative_to(evals).as_posix(), build={"ripgrep_sha256": file_hash(rg)})
            with patch("whip_evals.prepare.build_candidate", return_value=metadata), \
                 patch("whip_evals.prepare.catalog", side_effect=AssertionError("catalog")), \
                 patch("whip_evals.prepare.contract", side_effect=AssertionError("contract")), \
                 patch("whip_evals.tasks.prepare_tasks", side_effect=AssertionError("real tasks")), \
                 patch("subprocess.run", side_effect=AssertionError("execution")), \
                 patch("urllib.request.urlopen", side_effect=AssertionError("network")), \
                 patch.dict(os.environ, {}, clear=True):
                args = SimpleNamespace(run_id="fixture-offline", engines=engines,
                                       fixture_repetitions=repetitions, fixture_task_ids=task_ids)
                campaign = prepare_fixture_campaign(args, evals=evals, repo=root)
            manifest = read_json(campaign / "manifest.json")
            self.assertEqual(manifest["profile"], "fixture")
            self.assertTrue(manifest["fixture"])
            self.assertTrue(manifest["excluded_from_scores"])
            self.assertFalse(manifest["promote"])
            self.assertEqual(manifest["external_provider_calls"], 0)
            expected_engines = {"starlark", "quickjs"} if engines is None else {engines}
            selected = task_ids or ["fixture/harbor-shared", "fixture/pier-separate"]
            self.assertEqual(manifest["task_ids"], selected)
            self.assertEqual(set(manifest["prepared_tasks"]), set(selected))
            self.assertEqual(set(manifest["resolved_tasks"]), set(selected))
            self.assertEqual(len(manifest["schedule"]), len(selected) * len(expected_engines) * repetitions)
            self.assertEqual(manifest["repetitions"], repetitions)
            self.assertEqual({t["repetition"] for t in manifest["schedule"]}, set(range(1, repetitions + 1)))
            self.assertEqual({t["engine"] for t in manifest["schedule"]}, expected_engines)
            self.assertEqual({t["runner"] for t in manifest["schedule"]},
                             {task_id.split("/")[1].split("-")[0] for task_id in selected})
            inputs = read_json(campaign / "inputs.json")
            frozen = campaign / "payload/evals/artifacts/fixture-offline/schedule.json"
            self.assertEqual(read_json(frozen), inputs)
            self.assertEqual(set(inputs), {trial["id"] for trial in manifest["schedule"]})
            for trial in manifest["schedule"]:
                config = inputs[trial["id"]]["config"]
                self.assertTrue(config["agents"][0]["kwargs"]["fixture"])
                self.assertNotIn("contract", config["agents"][0]["kwargs"])
                native = manifest["resolved_tasks"][trial["task_id"]]
                self.assertEqual(native["agent"]["timeout_sec"], 120)
                self.assertEqual(native["verifier"]["timeout_sec"], 60)
                separate = trial["runner"] == "pier"
                self.assertEqual(native["verifier"]["environment_mode"], "separate" if separate else None)
                self.assertEqual(trial["resources"]["cpus"], 4 if separate else 2)
                if separate:
                    self.assertEqual(native["environment"]["network_mode"], "no-network")
            inventory = read_json(campaign / "bundle.json")
            self.assertIn("evals/whip_evals/fixture_provider.py", inventory["files"])
            self.assertFalse(any("/contracts/" in name for name in inventory["files"]))
            if task_ids == ["fixture/harbor-shared"]:
                self.assertEqual(len(inputs), 1)
                self.assertFalse(any("/pier-separate/" in name for name in inventory["files"]))
            verify_bundle(campaign / "bundle.tar", inventory["sha256"])

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
                    "jobs": 90, "qualification_status": "passed", "qualification_receipt": "a" * 64,
                    "fixture": False, "shapes": {runner: {"physical_cpus": 8, "memory_mb": 32768,
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
            inputs = read_json(campaign / "inputs.json")
            self.assertEqual(set(inputs), {trial["id"] for trial in manifest["schedule"]})
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
            with self.assertRaises(FileExistsError):
                prepare_campaign(self.args(), evals=evals, repo=root)


if __name__ == "__main__":
    unittest.main()
