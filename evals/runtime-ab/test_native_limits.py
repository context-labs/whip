"""Native task windows and uncapped trials must not inherit experimental caps."""
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import MagicMock, patch

from observe import write_config
from study import native_agent_timeout, run


class NativeLimitsTests(unittest.TestCase):
    def test_runtime_defaults_and_provider_output_resolution(self):
        with tempfile.TemporaryDirectory() as directory:
            for engine in ("starlark", "quickjs"):
                config = write_config(Path(directory) / engine, engine, 0, native_defaults=True)
                self.assertEqual(config["rlm"], {"defaultEngine": engine})
                self.assertEqual(config["models"]["kimi-k3"]["maxOut"], 0)

    def test_native_timeout_requires_explicit_valid_task_limit(self):
        with tempfile.TemporaryDirectory() as directory:
            task = Path(directory) / "task.toml"
            task.write_text("[agent]\ntimeout_sec = 5400\n")
            self.assertEqual(native_agent_timeout(directory), 5400)
            for value in ("0", "-1", "nan", "1.5"):
                task.write_text("[agent]\ntimeout_sec = " + value + "\n")
                with self.assertRaises(ValueError):
                    native_agent_timeout(directory)

    def test_native_dispatch_ignores_old_caps_and_never_counts_unknown_cost_as_zero(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ("anko-typed-variable-bindings", "httpx-multipart-response-parsing"):
                task = root / "tasks" / name
                task.mkdir(parents=True)
                (task / "task.toml").write_text("[agent]\ntimeout_sec = 5400\n[verifier]\ntimeout_sec = 1800\n")
            binary = root / "whip"
            binary.write_text("fixture")
            args = SimpleNamespace(phase="formal", suite="repository", binary=str(binary),
                output=str(root / "results"), terminal="/unused", deepswe=str(root),
                repetitions=1, seed=20260910, trial_cap=2.5, total_cap=300, timeout=900,
                max_tokens=500000, max_turns=60, max_output=32768,
                native_limits=True, prior_exposure=23.936665, start_index=1)
            with patch("study.subprocess.Popen", return_value=MagicMock(returncode=0)) as launch:
                with self.assertRaisesRegex(RuntimeError, "unknown accounting"):
                    run(args)
                self.assertEqual(launch.call_count, 1)
            records = json.loads((root / "results/trials.json").read_text())
            self.assertEqual(records[0]["engine"], "quickjs")
            self.assertTrue(records[0]["name"].startswith("formal-02-"))
            recipe = json.loads((root / "results/recipe.json").read_text())
            self.assertEqual(recipe["start_index"], 1)
            self.assertEqual(len(recipe["schedule"]), 3)
            self.assertAlmostEqual(records[0]["charged_or_reserved_usd"], 276.063335)
            config = json.loads((root / "results" / (records[0]["name"] + ".json")).read_text())
            agent = config["agents"][0]
            self.assertEqual(agent["kwargs"]["timeout"], 5400)
            self.assertTrue(agent["kwargs"]["native_defaults"])
            for key in ("max_cost", "max_tokens", "max_turns", "max_output"):
                self.assertEqual(agent["kwargs"][key], 0)
            self.assertGreater(agent["override_timeout_sec"], 5400)
            self.assertNotIn("override_timeout_sec", config["tasks"][0])
            with patch("study.subprocess.Popen") as launch:
                with self.assertRaisesRegex(RuntimeError, "unknown accounting"):
                    run(args)
                launch.assert_not_called()


if __name__ == "__main__":
    unittest.main()
