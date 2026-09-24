import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest

from summarize import arm_summary, cluster_intervals, main


class IncompleteComparisonTests(unittest.TestCase):
    def analyze(self, *, unknown_usage, complete, duration_complete=True, final_duration=10):
        records = []
        for engine in ("starlark", "quickjs"):
            records.append({"name": engine, "task": "fixture", "runner": "harbor",
                "engine": engine, "repetition": 1, "status": "finished", "success": True,
                "accounting_complete": complete if engine == "quickjs" else True,
                "agent_outcome": {"final_snapshot": duration_complete if engine == "quickjs" else True,
                    "frozen_daemon_pid": 123, "duration_seconds": final_duration if engine == "quickjs" else 10},
                "metrics": {"duration_seconds": 10, "ledger_cost_usd": .1,
                    "input_tokens": 0 if engine == "quickjs" else 100,
                    "output_tokens": 0 if engine == "quickjs" else 10,
                    "unknown_usage_calls": unknown_usage if engine == "quickjs" else 0,
                    "pending_calls": 0}})
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "trials.json").write_text(json.dumps(records))
            with contextlib.redirect_stdout(io.StringIO()):
                main(directory)
            return json.loads((root / "analysis.json").read_text())

    def test_unknown_cost_has_no_paired_cost_or_cost_interval(self):
        result = self.analyze(unknown_usage=1, complete=False)
        self.assertNotIn("quickjs_minus_starlark_ledger_cost_usd", result["pairs"][0])
        self.assertNotIn("quickjs_minus_starlark_ledger_cost_usd", result["cluster_intervals"])
        self.assertEqual(result["summary"]["quickjs"]["observed_ledger_cost_usd"], .1)

    def test_known_cost_without_tokens_does_not_invent_token_savings(self):
        result = self.analyze(unknown_usage=1, complete=True)
        pair = result["pairs"][0]
        self.assertIn("quickjs_minus_starlark_ledger_cost_usd", pair)
        self.assertNotIn("quickjs_minus_starlark_input_tokens", pair)
        self.assertNotIn("quickjs_minus_starlark_output_tokens", pair)

    def test_partial_telemetry_is_not_final_agent_duration(self):
        result = self.analyze(unknown_usage=1, complete=False, duration_complete=False)
        self.assertNotIn("quickjs_minus_starlark_duration_seconds", result["pairs"][0])
        self.assertNotIn("quickjs_minus_starlark_duration_seconds", result["cluster_intervals"])
        self.assertEqual(result["summary"]["quickjs"]["median_duration_seconds"], 10)
        self.assertEqual(result["summary"]["quickjs"]["all_complete_agent_duration"]["count"], 0)

    def test_final_outcome_duration_wins_over_stale_intermediate_metrics(self):
        result = self.analyze(unknown_usage=1, complete=False, final_duration=25)
        row = next(row for row in result["trials"] if row["engine"] == "quickjs")
        self.assertEqual(row["metrics_duration_seconds"], 10)
        self.assertEqual(row["duration_seconds"], 25)
        self.assertEqual(result["pairs"][0]["quickjs_minus_starlark_duration_seconds"], 15)

    def test_missing_final_duration_is_not_replaced_by_an_interim_sample(self):
        result = self.analyze(unknown_usage=1, complete=False, final_duration=None)
        self.assertNotIn("quickjs_minus_starlark_duration_seconds", result["pairs"][0])

    def test_missing_verifier_stays_in_attempted_denominator(self):
        result = arm_summary([{"status": "finished", "success": False, "verifier_observed": False}])
        self.assertEqual(result["trials"], 1)
        self.assertEqual(result["finished_trials"], 1)
        self.assertEqual(result["passes"], 0)
        self.assertEqual(result["verifier_observed"], 0)

    def test_bootstrap_resamples_tasks_with_repetitions_kept_together(self):
        pairs = [{"task": task, "repetition": repetition,
                  "starlark_pass": True, "quickjs_pass": task == "b"}
                 for task in ("a", "b") for repetition in (1, 2)]
        result = cluster_intervals(pairs)
        interval = result["quickjs_minus_starlark_pass"]
        self.assertEqual(result["task_clusters"], 2)
        self.assertEqual(interval["resamples"], 4)
        self.assertEqual(interval["mean"], -.5)
        self.assertAlmostEqual(interval["low"], -.9625)
        self.assertAlmostEqual(interval["high"], -.0375)


if __name__ == "__main__":
    unittest.main()
