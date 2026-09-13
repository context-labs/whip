"""Offline contract checks. No native trial or container is launched."""
import asyncio
import hashlib
import io
import json
from pathlib import Path
import shutil
import tarfile
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import AsyncMock, MagicMock, patch

from whip_evals.cli import parser
from whip_evals.common import EVALS, file_hash, read_json
from whip_evals.execution import job_config, schedule
from whip_evals.prepare import catalog, configuration, contract
from whip_evals.run import plan_run, run
from whip_evals.tasks import file_inventory, load_spec, prepare_tasks, task_archive_files


class ContentTransferTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        from whip_evals.adapter import WhipAdapter
        from whip_evals.observe import aggregate
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.body = b'synthetic content'
        self.digest = hashlib.sha256(self.body).hexdigest()
        self.manifest = {'bodies': [{'digest': self.digest, 'bytes': len(self.body)}]}
        self.metrics = aggregate([])
        self.outcome = dict(final_snapshot=True, frozen_daemon_pid=123, pending={}, evidence_errors=[])
        self.adapter = object.__new__(WhipAdapter)
        self.adapter.logs_dir = self.root / 'agent'
        self.adapter.engine, self.adapter.binary_digest = 'quickjs', 'fixture'
        self.adapter.timeout, self.adapter.max_cost, self.adapter.max_tokens = 10, 0, 0
        self.adapter.max_turns, self.adapter.max_output = 0, 0
        self.adapter.commit, self.adapter.fixture = False, True
        self.adapter.logger = MagicMock()

    def environment(self, manifest, transfer=None, exit_code=0):
        async def download(source, target):
            value = (self.metrics if target.name == 'metrics.json' else
                     self.outcome if target.name == 'outcome.json' else
                     manifest if target.name == 'content-export.json' else {})
            target.write_text(json.dumps(value))
        async def copy(source, target):
            self.assertEqual(source, '/logs/agent/whip/content/sha256')
            (target / self.digest).write_bytes(self.body)
        return SimpleNamespace(
            exec=AsyncMock(return_value=SimpleNamespace(return_code=exit_code, stdout='', stderr='')),
            upload_file=AsyncMock(), download_file=AsyncMock(side_effect=download),
            download_dir=AsyncMock(side_effect=transfer or copy))

    async def run_adapter(self, env):
        from harbor.models.agent.context import AgentContext
        context = AgentContext()
        await self.adapter.run('synthetic fixture', env, context)
        self.assertEqual(env.download_file.await_count, 11)
        return context

    async def test_native_bulk_2138_bodies_and_atomic_flat_destination(self):
        from harbor.environments.docker.docker_unix import UnixOps as HarborUnix
        from pier.environments.docker.docker_unix import UnixOps as PierUnix
        from whip_evals.adapter import CLEANUP_TIMEOUT_SECONDS
        self.assertEqual(CLEANUP_TIMEOUT_SECONDS, 240)
        source = self.root / 'source'
        source.mkdir()
        bodies = []
        for index in range(2138):
            body = (f'synthetic body {index}:'.encode() * 800)[:12000]
            digest = hashlib.sha256(body).hexdigest()
            (source / digest).write_bytes(body)
            bodies.append(dict(digest=digest, bytes=len(body)))
        for name, native_type in [('harbor', HarborUnix), ('pier', PierUnix)]:
            with self.subTest(runner=name):
                self.adapter.logs_dir = self.root / name
                destination = self.adapter.logs_dir / 'content/sha256'
                async def compose(args, check=False):
                    self.assertTrue(check)
                    self.assertEqual(args[:2], ['cp', 'main:/logs/agent/whip/content/sha256/.'])
                    self.assertFalse(destination.exists())
                    shutil.copytree(source, args[2], dirs_exist_ok=True, symlinks=True)
                    self.assertFalse(destination.exists())
                native = SimpleNamespace(_chown_to_host_user=AsyncMock(),
                                         _run_docker_compose_command=AsyncMock(side_effect=compose))
                env = self.environment({'bodies': bodies}, native_type(native).download_dir)
                context = await self.run_adapter(env)
                self.assertTrue(context.metadata['whip_accounting_complete'])
                self.assertEqual(env.download_dir.await_count, 1)
                self.assertEqual(native._run_docker_compose_command.await_count, 1)
                self.assertEqual(native._chown_to_host_user.await_count, int(name == 'pier'))
                self.assertEqual(len(list(destination.iterdir())), 2138)
                self.assertFalse((destination / 'sha256').exists())
                for body in bodies:
                    path = destination / body['digest']
                    self.assertEqual(path.stat().st_size, body['bytes'])
                    self.assertEqual(file_hash(path), body['digest'])
                self.assertEqual(list(destination.parent.glob('.transfer-*')), [])

    async def test_empty_manifest_skips_bulk(self):
        env = self.environment({'bodies': []})
        context = await self.run_adapter(env)
        self.assertTrue(context.metadata['whip_accounting_complete'])
        env.download_dir.assert_not_awaited()

    async def test_invalid_manifests_never_download_and_clear_accounting(self):
        invalid = [None, [], {}, {'bodies': None}, {'bodies': {}}, {'bodies': 'bad'},
                   {'bodies': [None]}, {'bodies': [{}]}, {'bodies': [self.manifest['bodies'][0]] * 2}]
        invalid += [{'bodies': [{'digest': digest, 'bytes': size}]} for digest, size in
                    [('../escape', 1), ('A' * 64, 1), (23, 1), (self.digest, True),
                     (self.digest, -1), (self.digest, 1.0), (self.digest, None)]]
        for index, manifest in enumerate(invalid):
            with self.subTest(manifest=index):
                self.adapter.logs_dir = self.root / str(index)
                env = self.environment(manifest)
                context = await self.run_adapter(env)
                self.assertFalse(context.metadata['whip_accounting_complete'])
                self.assertIsNone(context.cost_usd)
                self.assertIn('content transfer: ValueError', context.metadata['whip_evidence_errors'])
                env.download_dir.assert_not_awaited()

    async def test_failed_transfers_never_publish_or_certify_accounting(self):
        from harbor.models.agent.context import AgentContext
        modes = ('missing', 'extra', 'hash', 'size', 'symlink_inside', 'symlink_outside',
                 'subdirectory', 'preexisting', 'partial', 'timeout', 'cancel', 'validation_cancel')
        for mode in modes:
            with self.subTest(mode=mode):
                self.adapter.logs_dir = self.root / mode
                destination = self.adapter.logs_dir / 'content/sha256'
                if mode == 'preexisting':
                    destination.mkdir(parents=True)
                async def copy(source, staging):
                    target = staging / self.digest
                    if mode == 'missing':
                        return
                    target.write_bytes(self.body)
                    if mode == 'extra':
                        (staging / 'unexpected').write_bytes(b'extra')
                    elif mode in ('hash', 'size'):
                        target.write_bytes(b'x' * len(self.body) if mode == 'hash' else b'x')
                    elif mode.startswith('symlink'):
                        target.unlink()
                        other = (staging / 'other') if mode == 'symlink_inside' else self.root / 'outside'
                        other.write_bytes(self.body)
                        target.symlink_to(other)
                    elif mode == 'subdirectory':
                        target.unlink()
                        target.mkdir()
                    elif mode == 'partial':
                        raise OSError('synthetic partial transfer')
                    elif mode == 'timeout':
                        await asyncio.sleep(10)
                    elif mode == 'cancel':
                        raise asyncio.CancelledError()
                env = self.environment(self.manifest, copy)
                context = AgentContext()
                async def cancelled_yield(delay):
                    raise asyncio.CancelledError()
                with patch('whip_evals.adapter.CLEANUP_TIMEOUT_SECONDS', .02):
                    if mode == 'validation_cancel':
                        with patch('whip_evals.adapter.asyncio.sleep', side_effect=cancelled_yield):
                            with self.assertRaises(asyncio.CancelledError):
                                await self.adapter.run('fixture', env, context)
                    elif mode == 'cancel':
                        with self.assertRaises(asyncio.CancelledError):
                            await self.adapter.run('fixture', env, context)
                    else:
                        await self.adapter.run('fixture', env, context)
                self.assertFalse(context.metadata['whip_accounting_complete'])
                self.assertIsNone(context.cost_usd)
                self.assertTrue(context.metadata['whip_evidence_errors'])
                self.assertEqual(context.metadata['whip_cleanup']['truncated'], mode in ('timeout', 'cancel', 'validation_cancel'))
                self.assertEqual(destination.exists(), mode == 'preexisting')
                self.assertEqual(list(destination.parent.glob('.transfer-*')), [])
                self.assertEqual(env.download_dir.await_count, int(mode != 'preexisting'))

    async def test_content_failure_preserves_original_observer_error(self):
        from harbor.models.agent.context import AgentContext
        context = AgentContext()
        env = self.environment({}, exit_code=7)
        with self.assertRaisesRegex(RuntimeError, 'observer exited with code 7'):
            await self.adapter.run('fixture', env, context)
        self.assertFalse(context.metadata['whip_accounting_complete'])
        self.assertIn('content transfer: ValueError', context.metadata['whip_evidence_errors'])


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

    def test_catalog_request_identity_and_validation(self):
        protocol = load_spec()[2]
        model = {'id': 'kimi-k3', 'context_length': 1048576, 'max_completion_tokens': 131072,
                 'reasoning_efforts': ['high'], 'pricing': {'prompt': '0.000001'},
                 'input_modalities': ['text', 'image'], 'provider_extension': 'not-public'}
        for efforts in (['high'], ['low']):
            with self.subTest(efforts=efforts):
                selected = {**model, 'reasoning_efforts': efforts}
                response = io.BytesIO(json.dumps({'data': [selected]}).encode())
                with patch.dict('os.environ', {'INFERENCE_API_KEY': 'offline-fixture'}), \
                     patch('whip_evals.prepare.urllib.request.urlopen', return_value=response) as urlopen:
                    if 'high' in efforts:
                        frozen = catalog(protocol)
                        self.assertEqual(frozen, {k: v for k, v in selected.items() if k != 'provider_extension'})
                        self.assertNotIn('offline-fixture', json.dumps(frozen))
                    else:
                        with self.assertRaisesRegex(ValueError, 'pinned model/effort is not available'):
                            catalog(protocol)
                urlopen.assert_called_once()
                (request,), kwargs = urlopen.call_args
                self.assertEqual(request.full_url, protocol['endpoint'] + '/models')
                self.assertEqual(request.get_method(), 'GET')
                self.assertEqual(request.get_header('Authorization'), 'Bearer offline-fixture')
                self.assertEqual(request.get_header('User-agent'), 'whip-evals/0.1.0')
                self.assertEqual(kwargs, {'timeout': 30})

    def test_catalog_accepts_org_prefixed_listing_but_keeps_pinned_id(self):
        protocol = load_spec()[2]
        base = {'context_length': 1048576, 'max_completion_tokens': 131072,
                'reasoning_efforts': ['high'], 'pricing': {'prompt': '0.000001'}}
        accepted = {'prefixed only': ([{'id': 'moonshotai/kimi-k3', **base}], 'moonshotai/kimi-k3'),
                    'exact listing wins': ([{'id': 'moonshotai/kimi-k3', **base}, {'id': 'kimi-k3', **base}], None)}
        for name, (data, listed) in accepted.items():
            with self.subTest(name=name):
                response = io.BytesIO(json.dumps({'data': data}).encode())
                with patch.dict('os.environ', {'INFERENCE_API_KEY': 'offline-fixture'}), \
                     patch('whip_evals.prepare.urllib.request.urlopen', return_value=response):
                    frozen = catalog(protocol)
                self.assertEqual(frozen['id'], 'kimi-k3')
                self.assertEqual(frozen.get('listed_id'), listed)
                cached = contract({'engine': 'quickjs', 'configuration': configuration('quickjs')}, protocol, frozen)
                self.assertEqual(cached['catalog_cache']['inference-net']['models'][0]['id'], 'kimi-k3')
        for data in ([{'id': 'a/kimi-k3', **base}, {'id': 'b/kimi-k3', **base}], [{'id': 'kimi-k3-fast', **base}]):
            with self.subTest(ids=[m['id'] for m in data]):
                response = io.BytesIO(json.dumps({'data': data}).encode())
                with patch.dict('os.environ', {'INFERENCE_API_KEY': 'offline-fixture'}), \
                     patch('whip_evals.prepare.urllib.request.urlopen', return_value=response), \
                     self.assertRaisesRegex(ValueError, 'pinned model/effort is not available'):
                    catalog(protocol)

    def test_pier_native_proxy_has_only_the_descriptor_cap_changed(self):
        from pier.environments.docker.docker import DockerEnvironment
        from pier.environments.factory import EnvironmentFactory
        from pier.models.agent.network import NetworkAllowlist
        from pier.models.task.config import TaskConfig
        from pier.models.trial.config import EnvironmentConfig
        from pier.models.trial.paths import TrialPaths
        from whip_evals.adapter import PierDockerEnvironment
        from whip_evals.doctor import prepare_fixture
        policy = load_spec()[2]['network']
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            task = prepare_fixture(root / 'task', 'shared', no_network=True)
            for no_network in (False, True):
                with self.subTest(no_network=no_network):
                    native = TaskConfig.model_validate({'environment': {'network_mode': 'no-network' if no_network else 'public'}}).environment
                    environment = EnvironmentFactory.create_environment_from_config(
                        config=EnvironmentConfig(type='docker', import_path=policy['pier_environment_import_path']),
                        environment_dir=task / 'environment', environment_name='fixture', session_id='fixture',
                        trial_paths=TrialPaths(trial_dir=root / str(no_network)), task_env_config=native,
                        network_allowlist=NetworkAllowlist(domains=['api.inference.net']))
                    self.assertIsInstance(environment, PierDockerEnvironment)
                    # Only the random token is fixed; the actual pinned generator runs twice.
                    with patch('pier.environments.docker.docker.new_proxy_token', return_value='offline-proxy-token'):
                        DockerEnvironment._prepare_egress_proxy_compose(environment)
                        path = environment._egress_proxy_compose_path
                        original = read_json(path) if path else None
                        environment._prepare_egress_proxy_compose()
                    if not no_network:
                        self.assertIsNone(environment._egress_proxy_compose_path)
                        continue
                    actual = read_json(path)
                    self.assertEqual(actual['services']['pier-egress-proxy'].pop('ulimits'),
                                     {'nofile': policy['pier_proxy_nofile']})
                    self.assertEqual(actual, original)  # Main service, auth, domains and networks unchanged.
                    with patch.object(DockerEnvironment, '_prepare_egress_proxy_compose'):
                        environment._egress_proxy_compose_path = None
                        with self.assertRaisesRegex(RuntimeError, 'required inference proxy'):
                            environment._prepare_egress_proxy_compose()
                        environment._egress_proxy_compose_path = path
                        path.write_text(json.dumps({'services': {}}))
                        with self.assertRaises(KeyError):
                            environment._prepare_egress_proxy_compose()

    def test_doctor_ref_reaches_integration_build_and_preserves_default(self):
        from contextlib import redirect_stdout
        from whip_evals.cli import main
        from whip_evals.doctor import doctor, integration_check
        ref = 'e9c97beabf82f1d6923039b8f6a4ca6959ebcb00'
        host = {'runner_versions': load_spec()[2]['runner_versions'], 'os': 'linux'}
        for selected in (None, ref):
            with self.subTest(ref=selected):
                argv = ['doctor', '--integration'] + (['--ref', selected] if selected else [])
                with patch('whip_evals.doctor.doctor', return_value={'status': 'ready'}) as check, redirect_stdout(io.StringIO()):
                    self.assertEqual(main(argv), 0)
                check.assert_called_once_with(integration=True, ref=selected)
                with patch('whip_evals.doctor.environment', return_value=host), \
                     patch('whip_evals.doctor.shutil.which', return_value='/offline/tool'), \
                     patch('whip_evals.doctor.integration_check', return_value={'passed': True}) as integrate:
                    self.assertEqual(doctor(integration=True, ref=selected)['status'], 'ready')
                integrate.assert_called_once_with(EVALS, ref=selected)
                with tempfile.TemporaryDirectory() as temporary, \
                     patch('whip_evals.doctor.build_candidate', side_effect=RuntimeError('offline build boundary')) as build:
                    with self.assertRaisesRegex(RuntimeError, 'offline build boundary'):
                        integration_check(Path(temporary), ref=selected)
                build.assert_called_once_with('fixture', 'starlark', evals=Path(temporary), ref=selected)
        with patch('whip_evals.doctor.environment', return_value=host), \
             patch('whip_evals.doctor.shutil.which', return_value='/offline/tool'), \
             patch('whip_evals.doctor.integration_check') as integrate:
            doctor(ref=ref)
        integrate.assert_not_called()  # --ref alone is still read-only.

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
            if runner == 'pier':
                self.assertEqual(config['environment']['import_path'],
                                 load_spec()[2]['network']['pier_environment_import_path'])
            else:
                self.assertNotIn('import_path', config['environment'])
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
                for runner in ('harbor', 'pier'):
                    path = prepare_fixture(Path(temporary) / (runner + mode), mode, no_network=runner == 'pier')
                    cfg = import_module(runner + '.models.task.config').TaskConfig.model_validate(tomllib.loads((path / 'task.toml').read_text()))
                    self.assertEqual(cfg.agent.timeout_sec, 120)
                    self.assertTrue(cfg.verifier.collect)
                    if runner == 'pier':
                        self.assertEqual(cfg.environment.network_mode.value, 'no-network')
                        self.assertEqual(cfg.verifier.network_mode.value, 'no-network')
                    else:
                        self.assertEqual(cfg.environment.network_mode.value, 'public')
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
