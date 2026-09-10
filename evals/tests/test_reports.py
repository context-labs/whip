"""Author small native-shaped records in temporary directories; execute nothing."""
import hashlib
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

from whip_evals.common import read_json, write_json
from whip_evals.observe import aggregate, rows
from whip_evals.report import (build_result, compare_trials, empty_trial, normalize_trial,
                               summarize, termination, write_report)


def evidence_fixture(root, *, reward=1, cost_source='reported', status='succeeded', stderr=''):
    trial = dict(id='t1', task_id='terminal-bench/fixture', candidate_id='candidate', repetition=1,
                 runner='harbor', engine='quickjs', binary_sha256='f' * 64)
    agent = root / 'jobs/t1/native/agent'
    agent.mkdir(parents=True)
    call = dict(root_id='root', status=status, cost_source=cost_source, usage_source='reported', cost_micros=1234567,
                attempt=dict(Provider='inference-net', Model='kimi-k3', Purpose='turn', Pricing={'prompt': '0.001', 'completion': '0.002'}),
                result=dict(Dispatched=True, Usage={'prompt_tokens': 100, 'completion_tokens': 10, 'prompt_tokens_details': {'cached_tokens': 20}}))
    body = b'content fixture'
    digest = hashlib.sha256(body).hexdigest()
    content = dict(root_id='root', bodies=[dict(digest=digest, bytes=len(body))], errors=[])
    with sqlite3.connect(agent / 'sessions.db') as db:
        db.row_factory = sqlite3.Row
        db.executescript('''
          CREATE TABLE sessions(id); INSERT INTO sessions VALUES('root');
          CREATE TABLE model_calls(root_id,status,cost_source,usage_source,cost_micros,attempt,result);
          CREATE TABLE content_references(id,digest,size);
          CREATE TABLE content_objects(digest,size);
          CREATE TABLE content_grants(reference_id,root_id);
        ''')
        db.execute('INSERT INTO model_calls VALUES(?,?,?,?,?,?,?)',
                   [json.dumps(v) if isinstance(v, dict) else v for v in call.values()])
        db.execute('INSERT INTO content_references VALUES(?,?,?)', ('ref', digest, len(body)))
        db.execute('INSERT INTO content_objects VALUES(?,?)', (digest, len(body)))
        db.execute("INSERT INTO content_grants VALUES('ref','root')")
        calls = rows(db, 'SELECT * FROM model_calls')
    content_path = agent / 'content/sha256' / digest
    content_path.parent.mkdir(parents=True)
    content_path.write_bytes(body)
    state = dict(root=dict(id='root'), calls=calls, settled=True, pending={}, agents=[], turns=[], budgets=[])
    outcome = dict(status='completed', final_snapshot=True, frozen_daemon_pid=123, pending={},
                   evidence_errors=[], content_export_complete=True, agent_duration_seconds=10)
    for name, value in [('state.json', state), ('outcome.json', outcome), ('metrics.json', aggregate(calls)),
                        ('identity.json', dict(engine='quickjs', binary_sha256='f' * 64)),
                        ('content-export.json', content), ('configuration.json', {}), ('provider-catalog.json', {})]:
        write_json(agent / name, value)
    (agent / 'cli.stderr').write_text(stderr)
    (agent / 'events.ndjson').write_text('')
    (agent / 'cli.ndjson').write_text('')
    native = dict(agent_result=dict(metadata=dict(whip_accounting_complete=True)),
                  verifier_result=dict(rewards=dict(reward=reward)),
                  environment_setup=dict(started_at='2026-09-10T00:00:00Z', finished_at='2026-09-10T00:00:03Z'),
                  verifier=dict(started_at='2026-09-10T00:00:20Z', finished_at='2026-09-10T00:00:22Z'))
    write_json(agent.parent / 'result.json', native)
    raw = dict(started=True, job_path='jobs/t1', cleanup=dict(complete=True), trial_seconds=25, phase_seconds=dict(queue=4, cleanup=1))
    return trial, raw, agent


class ReportTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_complete_native_grade_cost_and_phases(self):
        trial, raw, _ = evidence_fixture(self.root)
        row = normalize_trial(trial, raw, self.root)
        self.assertTrue(row['success'])
        self.assertTrue(row['evidence_complete'], row['error_codes'])
        self.assertTrue(row['accounting_complete'])
        self.assertEqual(row['cost_usd'], '1.234567')
        self.assertEqual(row['input_tokens'], 100)
        self.assertEqual(row['phase_seconds']['verifier'], 2)
        self.assertEqual(row['phase_seconds']['environment_setup'], 3)
        self.assertIsNone(row['phase_seconds']['agent_setup'])
        self.assertIsNone(row['provider_statuses'])

    def test_native_failures_are_grades_but_missing_boolean_and_fractional_rewards_are_not(self):
        for i, reward in enumerate((0, None, True, .5)):
            with self.subTest(reward=reward):
                directory = self.root / str(i)
                trial, raw, _ = evidence_fixture(directory, reward=reward)
                row = normalize_trial(trial, raw, directory)
                self.assertEqual(row['success'], False if reward == 0 else None)
                self.assertEqual(row['grader_status'], 'failed' if reward == 0 else 'missing')

    def test_stale_metrics_cannot_mask_running_unknown_cost_call(self):
        trial, raw, agent = evidence_fixture(self.root, cost_source='unknown', status='running')
        write_json(agent / 'metrics.json', aggregate([]))
        row = normalize_trial(trial, raw, self.root)
        self.assertIsNone(row['cost_usd'])
        self.assertEqual(row['unknown_cost_calls'], 1)
        self.assertFalse(row['accounting_complete'])
        self.assertFalse(row['evidence_complete'])
        self.assertIn('accounting_snapshot_mismatch', row['error_codes'])

    def test_missing_usage_does_not_become_zero(self):
        trial, raw, agent = evidence_fixture(self.root)
        state = read_json(agent / 'state.json')
        state['calls'][0]['usage_source'] = 'unknown'
        with sqlite3.connect(agent / 'sessions.db') as db:
            db.execute("UPDATE model_calls SET usage_source='unknown'")
        write_json(agent / 'state.json', state)
        write_json(agent / 'metrics.json', aggregate(state['calls']))
        row = normalize_trial(trial, raw, self.root)
        self.assertIsNone(row['input_tokens'])
        self.assertFalse(row['accounting_complete'])

    def test_corrupt_content_or_snapshot_or_identity_blocks_evidence(self):
        for i, mutation in enumerate(('body', 'database', 'identity', 'finality')):
            directory = self.root / str(i)
            trial, raw, agent = evidence_fixture(directory)
            if mutation == 'body':
                next((agent / 'content/sha256').iterdir()).write_bytes(b'corrupt')
            elif mutation == 'database':
                with sqlite3.connect(agent / 'sessions.db') as db:
                    db.execute('UPDATE model_calls SET cost_micros=1')
            elif mutation == 'identity':
                write_json(agent / 'identity.json', {'engine': 'starlark'})
            else:
                outcome = read_json(agent / 'outcome.json')
                outcome['frozen_daemon_pid'] = None
                write_json(agent / 'outcome.json', outcome)
            row = normalize_trial(trial, raw, directory)
            self.assertFalse(row['evidence_complete'], mutation)
            self.assertFalse(row['accounting_complete'], mutation)

    def test_failure_sources_do_not_conflate_timeouts(self):
        cases = [({'status': 'timeout'}, {}, '', 'benchmark_deadline'),
                 ({'status': 'agent_error'}, {}, 'Client.Timeout exceeded', 'whip_request_timeout'),
                 ({}, {'outer_watchdog': True}, '', 'evaluator_watchdog'),
                 ({}, {'cancelled': True, 'cancellation_source': 'controller_cancelled'}, '', 'controller_cancelled'),
                 ({'status': 'agent_error'}, {}, 'host request limit', 'whip_guard')]
        for outcome, raw, diagnostic, expected in cases:
            self.assertEqual(termination(outcome, raw, diagnostic), expected)

    def test_planned_denominator_and_unknown_cost_preserved(self):
        trial, raw, _ = evidence_fixture(self.root)
        passed = normalize_trial(trial, raw, self.root)
        missing = empty_trial(dict(trial, id='t2', task_id='terminal-bench/missing'))
        summary = summarize([passed, missing])
        self.assertEqual(summary['verified_success_rate'], .5)
        self.assertEqual(summary['graded_pass_rate'], 1)
        self.assertEqual(summary['known_cost_usd'], '1.234567')
        self.assertIsNone(summary['cost_usd'])
        self.assertIsNone(summary['effective_cost_per_pass_usd'])

    def test_early_failure_is_not_paired_speed_improvement(self):
        trial, raw, _ = evidence_fixture(self.root)
        control = normalize_trial(trial, raw, self.root)
        fast_failure = dict(control, success=False, agent_seconds=.01)
        comparison = compare_trials([control], [fast_failure], samples=100)
        self.assertIsNone(comparison['common_success_latency_ratio'])
        self.assertEqual(comparison['paired_losses'], 1)
        self.assertEqual(comparison, compare_trials([control], [fast_failure], samples=100))
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            compare_trials([control, control], [fast_failure])

    def test_immutable_report_and_duplicate_matrix(self):
        trial, raw, _ = evidence_fixture(self.root)
        row = normalize_trial(trial, raw, self.root)
        manifest = dict(run_id='offline', schedule=[trial], candidates=[{'id': 'candidate'}],
                        profile='smoke', profile_version=1, comparison_key='fixture', seed=1)
        result = build_result(manifest, [row])
        output = self.root / 'report'
        write_report(output, manifest, result)
        self.assertEqual(read_json(output / 'result.json'), result)
        self.assertTrue((output / 'trials.csv').exists())
        with self.assertRaises(FileExistsError):
            write_report(output, manifest, result)
        manifest['schedule'].append(dict(trial, id='different-id'))
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            build_result(manifest, [row])

    def test_json_cli_error_is_used_but_model_text_is_not(self):
        trial, raw, agent = evidence_fixture(self.root)
        outcome = read_json(agent / 'outcome.json')
        outcome['status'] = 'agent_error'
        write_json(agent / 'outcome.json', outcome)
        (agent / 'cli.ndjson').write_text(json.dumps({'type': 'text', 'text': 'Client.Timeout exceeded'}) + '\n')
        self.assertEqual(normalize_trial(trial, raw, self.root)['termination_source'], 'agent_error')
        (agent / 'cli.ndjson').write_text(json.dumps({'type': 'error', 'error': 'Client.Timeout exceeded'}) + '\n')
        self.assertEqual(normalize_trial(trial, raw, self.root)['termination_source'], 'whip_request_timeout')
