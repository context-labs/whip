"""Synthetic policy and filesystem transactions only; no scored benchmark runs."""
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from whip_evals import baseline
from whip_evals.common import file_hash, read_json, value_hash, write_json
from whip_evals.execution import schedule
from whip_evals.integrity import inventory
from whip_evals.report import build_result, empty_trial, write_report
from whip_evals.tasks import load_spec


def campaign(run_id='offline-initial', previous=None, *, evals=None):
    lock, _, protocol = load_spec()
    tasks = lock['tasks']
    candidate = dict(id='candidate', engine='starlark', dirty=False, source_sha256='source', binary_sha256='binary',
                     binary_path='cache/binary', configuration={})
    candidates = [candidate]
    pointer = None
    if previous:
        old_manifest, _ = previous
        pointer = dict(run_id=old_manifest['run_id'], candidate_id='candidate', revision='revision')
        control = dict(old_manifest['candidates'][-1], id='control', baseline_revision='revision')
        candidates = [control, candidate]
    trials = schedule(tasks, candidates, 3, 3)
    manifest = dict(schema_version=1, run_id=run_id, schedule=trials, candidates=candidates,
                    task_ids=[t['id'] for t in tasks], profile='full', profile_version=1,
                    comparison_key='fixture-protocol', seed=3, repetitions=3, promote=True,
                    promotion_policy=protocol['promotion'], baseline_at_launch=pointer,
                    artifact_root='artifacts/' + run_id, track=protocol['track'])
    rows = []
    for trial in trials:
        row = empty_trial(trial)
        passing = 20 if previous and trial['candidate_id'] == 'candidate' else 10
        success = manifest['task_ids'].index(trial['task_id']) < passing
        row.update(started=True, success=success, grader_status='passed' if success else 'failed',
                   execution_status='completed', evidence_complete=True, accounting_complete=True,
                   cleanup_complete=True, known_cost_usd='1.000000', cost_usd='1.000000',
                   input_tokens=100, output_tokens=10, cache_tokens=0, unknown_cost_calls=0,
                   agent_seconds=10, model_calls=1)
        rows.append(row)
    if evals:
        binary = evals / 'cache/binary'
        binary.parent.mkdir(parents=True, exist_ok=True)
        binary.write_bytes(b'not an executable: offline fixture identity')
        for c in candidates:
            c['binary_sha256'] = file_hash(binary)
        for trial in trials:
            trial['binary_sha256'] = file_hash(binary)
    result = build_result(manifest, rows)
    result['integrity'] = {'complete': True}
    if evals:
        root = evals / manifest['artifact_root']
        root.mkdir(parents=True)
        (root / 'fixture.txt').write_text('synthetic evidence')
        audit_path = root / 'integrity.json'
        write_json(audit_path, inventory(root, audit_path))
        result['integrity'].update(path=audit_path.relative_to(evals).as_posix(), sha256=file_hash(audit_path))
        report = evals / 'reports' / run_id
        write_json(report / 'manifest.json', manifest)
        write_report(report, manifest, result)
    return manifest, result


class BaselineTests(unittest.TestCase):
    def test_initial_campaign_and_replacement(self):
        previous = campaign()
        self.assertTrue(baseline.evaluate(*previous)['eligible'])
        candidate = campaign('replacement', previous)
        self.assertTrue(baseline.evaluate(*candidate, previous=previous)['eligible'])

    def test_incomplete_dirty_subset_and_unknown_cost_are_held(self):
        for mutation, gate in [('dirty', 'clean_reproducible_builds'), ('subset', 'full_profile'),
                               ('evidence', 'complete_evidence'), ('accounting', 'complete_evidence'),
                               ('integrity', 'integrity_verified'), ('opt_in', 'explicit_promotion_campaign')]:
            manifest, result = campaign()
            if mutation == 'dirty':
                manifest['candidates'][0]['dirty'] = True
            elif mutation == 'subset':
                manifest['profile'] = 'smoke'
            elif mutation == 'evidence':
                result['trials'][0]['evidence_complete'] = False
            elif mutation == 'accounting':
                result['trials'][0]['accounting_complete'] = False
                result['trials'][0]['cost_usd'] = None
            elif mutation == 'integrity':
                result['integrity']['complete'] = False
            else:
                manifest['promote'] = False
            self.assertIn(gate, baseline.evaluate(manifest, result)['failed_gates'])

    def test_replacement_quality_cost_latency_and_identity_guards(self):
        previous = campaign()
        for mutation, gate in [('tie', 'quality_improvement'), ('cost', 'cost_guard'),
                               ('slow', 'latency_guard'), ('protocol', 'compatible_protocol'),
                               ('control', 'contemporary_control')]:
            manifest, result = campaign('replacement', previous)
            if mutation == 'protocol':
                manifest['comparison_key'] = 'different'
            elif mutation == 'control':
                manifest['candidates'][0]['binary_sha256'] = 'wrong'
            else:
                control = {(r['task_id'], r['repetition']): r for r in result['trials'] if r['candidate_id'] == 'control'}
                for row in result['trials']:
                    if row['candidate_id'] != 'candidate':
                        continue
                    if mutation == 'tie':
                        row['success'] = control[row['task_id'], row['repetition']]['success']
                    elif mutation == 'cost':
                        row.update(cost_usd='100.000000', known_cost_usd='100.000000')
                    else:
                        row['agent_seconds'] = 12
            result = build_result(manifest, result['trials'])
            result['integrity'] = {'complete': True}
            self.assertIn(gate, baseline.evaluate(manifest, result, previous=previous)['failed_gates'])

    def test_publication_history_and_stale_pointer(self):
        with tempfile.TemporaryDirectory() as temporary:
            evals = Path(temporary)
            first = campaign('first', evals=evals)
            second = campaign('second', evals=evals)  # Both launched before any accepted baseline.
            accepted = baseline.publish(evals, *first)
            self.assertTrue(accepted['accepted'])
            pointer = baseline.current(evals, first[0]['track'])
            history = read_json(baseline.track_path(evals, first[0]['track']) / 'history' / (pointer['revision'] + '.json'))
            self.assertTrue(history['decision']['accepted'])
            self.assertEqual(history['decision']['baseline_revision'], pointer['revision'])
            held = baseline.publish(evals, *second)
            self.assertFalse(held['accepted'])
            self.assertIn('stale_baseline', held['failed_gates'])
            self.assertEqual(baseline.current(evals, first[0]['track']), pointer)
            self.assertEqual(baseline.load_accepted(evals, pointer)[0], first[0])

    def test_evidence_tamper_cannot_publish(self):
        with tempfile.TemporaryDirectory() as temporary:
            evals = Path(temporary)
            manifest, result = campaign(evals=evals)
            (evals / manifest['artifact_root'] / 'fixture.txt').write_text('changed')
            with self.assertRaisesRegex(ValueError, 'evidence changed'):
                baseline.publish(evals, manifest, result)
            self.assertIsNone(baseline.current(evals, manifest['track']))

    def test_interrupted_pointer_write_keeps_current_uninitialized(self):
        with tempfile.TemporaryDirectory() as temporary:
            evals = Path(temporary)
            manifest, result = campaign(evals=evals)
            original = baseline.write_json
            def fail_pointer(path, *args, **kwargs):
                if Path(path).name == 'current.json':
                    raise OSError('offline interrupted publication')
                return original(path, *args, **kwargs)
            with patch('whip_evals.baseline.write_json', side_effect=fail_pointer), self.assertRaises(OSError):
                baseline.publish(evals, manifest, result)
            self.assertIsNone(baseline.current(evals, manifest['track']))

    def test_simultaneous_publishers_cannot_clobber_each_other(self):
        from concurrent.futures import ThreadPoolExecutor
        with tempfile.TemporaryDirectory() as temporary:
            evals = Path(temporary)
            a = campaign('concurrent-a', evals=evals)
            b = campaign('concurrent-b', evals=evals)
            with ThreadPoolExecutor(max_workers=2) as pool:
                futures = [pool.submit(baseline.publish, evals, *c) for c in (a, b)]
                decisions = [future.result() for future in futures]
            self.assertEqual(sum(d['accepted'] for d in decisions), 1)
            winner = next(d for d in decisions if d['accepted'])
            self.assertEqual(baseline.current(evals, a[0]['track'])['run_id'], winner['run_id'])
