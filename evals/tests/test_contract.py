"""Offline contract checks. No native trial or container is launched."""
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

from whip_evals.cli import parser
from whip_evals.common import EVALS, file_hash, read_json
from whip_evals.execution import job_config, schedule
from whip_evals.prepare import catalog, configuration, contract
from whip_evals.run import plan_run, run
from whip_evals.tasks import file_inventory, load_spec, prepare_tasks, task_archive_files


class ContractTests(unittest.TestCase):
    def test_profiles_and_native_limits(self):
        lock, profiles, protocol = load_spec()
        self.assertEqual([len(profiles[p]['task_ids']) for p in ('smoke', 'medium', 'full')], [8, 15, 30])
        self.assertEqual(sum(t['runner'] == 'pier' for t in lock['tasks']), 9)
        self.assertEqual(protocol['limits'], dict(cost_usd=None, tokens=None, rounds=None, output_override=None))
        for task in lock['tasks']:
            self.assertFalse(any('solution' in name for name in task['files']))
            if task['id'].split('/')[-1] in ('regex-log', 'sqlite-db-truncate'):
                self.assertEqual(task['agent_seconds'], 1200)
                self.assertEqual(task['native']['agent']['timeout_sec'], 900)
            if task['runner'] == 'pier':
                self.assertEqual(task['resources']['memory_mb'], 16384)
                self.assertEqual(task['agent_seconds'], 5400)

    def test_all_workload_expansions_are_offline(self):
        with patch('whip_evals.run.environment', side_effect=AssertionError('Docker forbidden')), \
             patch('whip_evals.run.build_candidate', side_effect=AssertionError('build forbidden')), \
             patch('whip_evals.run.catalog', side_effect=AssertionError('network forbidden')), \
             patch('whip_evals.run.baseline.current', return_value=None):
            cases = [('smoke', [], 8), ('medium', ['--engines', 'starlark,quickjs'], 30),
                     ('full', [], 30), ('full', ['--promote'], 90)]
            for profile, extra, count in cases:
                args = parser().parse_args(['run', profile, '--engines', 'starlark', '--dry-run', *extra])
                self.assertEqual(run(args)['trial_count'], count)
            with patch('whip_evals.run.baseline.current', return_value={'revision': 'frozen'}):
                self.assertEqual(plan_run(parser().parse_args(['run', 'full', '--promote']))['trial_count'], 180)

    def test_invalid_campaigns_rejected_before_external_work(self):
        for flags in (['smoke', '--promote'], ['full', '--promote', '--repetitions', '2'],
                      ['full', '--jobs', '33'], ['smoke', '--repetitions', '0'],
                      ['smoke', '--engines', 'starlark,starlark'],
                      ['full', '--engines', 'starlark,quickjs', '--against', 'HEAD']):
            with self.subTest(flags=flags), self.assertRaises(ValueError):
                plan_run(parser().parse_args(['run', *flags]))

    def test_provider_modalities_and_production_defaults_survive_freezing(self):
        protocol = load_spec()[2]
        base = {'id': 'kimi-k3', 'context_length': 1048576, 'max_completion_tokens': 131072,
                'reasoning_efforts': ['high'], 'pricing': {'prompt': '0.000001', 'completion': '0.000003'}}
        for modalities in (None, [], ['text', 'image']):
            model = dict(base)
            if modalities is not None:
                model['input_modalities'] = modalities
            response = io.BytesIO(json.dumps({'data': [model]}).encode())
            with patch.dict('os.environ', {'INFERENCE_API_KEY': 'offline-fixture'}), \
                 patch('urllib.request.urlopen', return_value=response):
                frozen = catalog(protocol)
            cfg = configuration('quickjs')
            self.assertTrue(cfg['models']['kimi-k3']['vision'])
            self.assertEqual(cfg['models']['kimi-k3']['maxOut'], 0)
            self.assertEqual(cfg['rlm'], {'defaultEngine': 'quickjs'})
            cached = contract({'engine': 'quickjs', 'configuration': cfg}, protocol, frozen)['catalog_cache']['inference-net']['models'][0]
            self.assertEqual('inputModalities' in cached, modalities is not None)
            if modalities is not None:
                self.assertEqual(cached['inputModalities'], modalities)

    def test_balanced_paired_schedule(self):
        tasks = load_spec()[0]['tasks']
        candidates = [dict(id=e, engine=e, binary_sha256='shared') for e in ('starlark', 'quickjs')]
        rows = schedule(tasks, candidates, 3, 17)
        self.assertEqual(rows, schedule(tasks, candidates, 3, 17))
        self.assertEqual(len({r['id'] for r in rows}), 180)
        self.assertEqual(sum(r['candidate_id'] == 'starlark' for r in rows[:60:2]), 15)
        first = {(r['task_id'], r['repetition']): r['candidate_id'] for r in rows[::2]}
        for task in tasks:
            self.assertNotEqual(first[task['id'], 1], first[task['id'], 2])

    def test_native_job_parser_receives_unlimited_agent_and_one_attempt(self):
        for runner in ('harbor', 'pier'):
            trial = {'id': 'fixture', 'engine': 'quickjs', 'runner': runner}
            config, envelope = job_config(trial, {'agent_seconds': 120}, Path('/tmp/binary'),
                Path('/tmp/contract'), EVALS / 'runtime-ab/smoke', Path('/tmp/jobs'))
            self.assertEqual(config['retry']['max_retries'], 0)
            self.assertEqual(config['n_attempts'], 1)
            self.assertEqual(config['n_concurrent_trials'], 1)
            kwargs = config['agents'][0]['kwargs']
            self.assertEqual([kwargs[k] for k in ('max_cost', 'max_tokens', 'max_turns', 'max_output')], [0] * 4)
            self.assertTrue(kwargs['native_defaults'])
            self.assertEqual(kwargs['commit'], runner == 'pier')
            self.assertGreater(envelope['outer_watchdog_seconds'], envelope['agent_runner_seconds'])

    def test_archive_excludes_solutions_and_preparation_is_hash_checked(self):
        buffer = io.BytesIO()
        task_toml = b'version="1.0"\n[agent]\ntimeout_sec=900\n[verifier]\ntimeout_sec=30\n[environment]\ndocker_image="example:tag"\ncpus=1\nmemory_mb=2048\nstorage_mb=10240\n'
        with tarfile.open(fileobj=buffer, mode='w:gz') as archive:
            for name, data in [('task.toml', task_toml), ('instruction.md', b'offline'), ('solution/solve.sh', b'excluded')]:
                entry = tarfile.TarInfo('repo/tasks/fixture/' + name)
                entry.size, entry.mode = len(data), 0o644
                archive.addfile(entry, io.BytesIO(data))
        source = buffer.getvalue()
        files = task_archive_files(source, 'tasks/fixture')
        self.assertEqual(set(files), {'task.toml', 'instruction.md'})
        with tempfile.TemporaryDirectory() as temporary:
            cache = Path(temporary)
            archive = cache / 'source'
            archive.write_bytes(source)
            digest = file_hash(archive)
            archive.rename(cache / (digest + '.tar.gz'))
            lock = {'sources': {'fixture': {'sha256': digest, 'url': 'not-used'}}}
            task = {'id': 'terminal-bench/fixture', 'runner': 'harbor', 'path': 'tasks/fixture', 'source': 'fixture',
                    'files': file_inventory(files), 'agent_seconds': 1200, 'verifier_seconds': 30,
                    'image': 'example@sha256:' + 'a' * 64, 'resources': {'cpus': 1, 'memory_mb': 2048, 'storage_mb': 10240}}
            with patch('whip_evals.tasks.download', side_effect=AssertionError('network forbidden')):
                prepared = prepare_tasks(lock, [task], cache)[task['id']]
                self.assertEqual(prepared['resolved_native']['agent']['timeout_sec'], 1200)
                self.assertEqual(prepare_tasks(lock, [task], cache)[task['id']], prepared)
                (Path(prepared['path']) / 'instruction.md').write_text('tampered')
                with self.assertRaisesRegex(ValueError, 'modified'):
                    prepare_tasks(lock, [task], cache)

class PreparationTests(unittest.TestCase):
    def test_commented_config_reads_only_engine_preference(self):
        from whip_evals.run import default_engine
        with tempfile.TemporaryDirectory() as temporary, patch.dict('os.environ', {'WHIP_HOME': temporary}):
            Path(temporary, 'config.json').write_text('''// Local comment
            {"note":"https://example.invalid/a/*b*/", "rlm": {"defaultEngine":"quickjs",},}
            ''')
            self.assertEqual(default_engine(), 'quickjs')

    def test_qualification_fixtures_parse_with_both_native_runners(self):
        from importlib import import_module
        from whip_evals.doctor import prepare_fixture
        import tomllib
        with tempfile.TemporaryDirectory() as temporary:
            for mode in ('shared', 'separate'):
                path = prepare_fixture(Path(temporary) / mode, mode)
                for runner in ('harbor', 'pier'):
                    cfg = import_module(runner + '.models.task.config').TaskConfig.model_validate(tomllib.loads((path / 'task.toml').read_text()))
                    self.assertEqual(cfg.agent.timeout_sec, 120)
                    self.assertTrue(cfg.verifier.collect)
                    if mode == 'separate':
                        self.assertEqual(cfg.verifier.environment_mode.value, mode)
                        self.assertIn('COPY test.sh /tests/test.sh', (path / 'tests/Dockerfile').read_text())

    def test_runtime_ab_freezes_exactly_one_build(self):
        import shutil
        with tempfile.TemporaryDirectory() as temporary:
            evals = Path(temporary).resolve()
            shutil.copytree(EVALS / 'frontier', evals / 'frontier')
            (evals / 'uv.lock').write_text('offline lock fixture')
            protocol = load_spec()[2]
            host = dict(cpus=128, memory_bytes=1024**4, disk_free_mb=1000000, architecture='amd64',
                        docker_version='fixture', os='linux', cpu_model='fixture', emulated=False,
                        runner_versions=protocol['runner_versions'])
            frozen = dict(id='candidate', engine='starlark', binary_sha256='a' * 64, source_sha256='b' * 64,
                          binary_path='cache/fake', commit='fixture', dirty=True, configuration={})
            def prepared(lock, tasks, cache):
                return {t['id']: dict(path=str(cache / 'fake'), sha256='offline', resolved_native={k: {} for k in ('agent', 'environment', 'verifier')}) for t in tasks}
            def pool(trials, *_, **kwargs):
                return {t['id']: {'started': False, 'cleanup': {'complete': True}} for t in trials}
            args = parser().parse_args(['run', 'smoke', '--engines', 'starlark,quickjs', '--run-id', 'offline-build-test'])
            with patch('whip_evals.run.environment', return_value=host), \
                 patch('whip_evals.run.build_candidate', return_value=frozen) as build, \
                 patch('whip_evals.run.prepare_tasks', side_effect=prepared), \
                 patch('whip_evals.run.pull_images'), patch('whip_evals.run.catalog', return_value={}), \
                 patch('whip_evals.run.contract', return_value={}), \
                 patch('whip_evals.run.job_config', return_value=({}, {})), \
                 patch('whip_evals.run.execute_job', side_effect=AssertionError('no evaluation allowed')), \
                 patch('whip_evals.run.run_pool', side_effect=pool), patch('builtins.print'):
                value = run(args, evals=evals)
            self.assertEqual(value['status'], 'partial')
            build.assert_called_once()
            manifest = read_json(evals / 'reports/offline-build-test/manifest.json')
            a, b = manifest['candidates']
            self.assertEqual(a['binary_sha256'], b['binary_sha256'])
            self.assertEqual(a['source_sha256'], b['source_sha256'])
            self.assertEqual([c['configuration']['rlm']['defaultEngine'] for c in (a, b)], ['starlark', 'quickjs'])

    def test_own_reports_do_not_dirty_or_change_candidate_build(self):
        from whip_evals.prepare import command, source_snapshot
        with tempfile.TemporaryDirectory() as temporary:
            repo = Path(temporary)
            command(['git', 'init', '-q'], cwd=repo)
            (repo / 'main.txt').write_text('tracked source')
            command(['git', 'add', 'main.txt'], cwd=repo)
            command(['git', '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'fixture'], cwd=repo)
            initial = source_snapshot(repo)
            self.assertFalse(initial[2])
            (repo / 'evals/reports/first').mkdir(parents=True)
            (repo / 'evals/reports/first/request.json').write_text('generated')
            self.assertEqual(source_snapshot(repo), initial)
            (repo / 'main.txt').write_text('working source')
            dirty = source_snapshot(repo)
            self.assertTrue(dirty[2])
            with tarfile.open(fileobj=io.BytesIO(dirty[0])) as archive:
                self.assertEqual(archive.getnames(), ['main.txt'])
                self.assertEqual(archive.extractfile('main.txt').read(), b'working source')
            self.assertEqual(source_snapshot(repo, 'HEAD'), initial)

    def test_historical_cost_forecast_never_becomes_a_limit(self):
        from whip_evals.run import cost_estimate
        planned = dict(task_ids=['a', 'b'], repetitions=3, candidate_count=2)
        pointer = dict(run_id='accepted', candidate_id='candidate')
        previous = ({}, {'trials': [dict(task_id=t, candidate_id='candidate', cost_usd='1.000000') for t in ('a', 'b')]})
        forecast = cost_estimate(previous, pointer, planned)
        self.assertEqual(forecast['cost_usd'], '12.000000')
        self.assertFalse(forecast['is_limit'])
        previous[1]['trials'][0]['cost_usd'] = None
        self.assertIsNone(cost_estimate(previous, pointer, planned)['cost_usd'])
