#!/usr/bin/env python3
"""Packaged whipcode acceptance: real installer, renderer, session and daemon.

Run after building/packing the renderer (never substitutes an HTML placeholder).
No arguments builds two native candidates into temporary files and checks updates.
--binary PATH --version TAG checks an existing release candidate without rebuilding.
--browser additionally runs the existing real Chromium gateway/bootstrap smoke.
All installation, state and daemon processes belong to disposable homes.
"""
import argparse
from contextlib import contextmanager
import hashlib
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[1]
ASSETS = ['whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64',
          'whipcode-darwin-arm64', 'SHA256SUMS', 'install.sh', 'latest.sh']


def run(binary, *args, env, check=True):
    result = subprocess.run([str(binary), *args], env=env, cwd=env['HOME'],
                            capture_output=True, text=True, timeout=60)
    if check and result.returncode:
        raise AssertionError(f'{binary.name} {args}: {result.stdout}{result.stderr}')
    return result


def build(binary, version):
    subprocess.run(['go', 'build', '-trimpath', '-ldflags', f'-X main.version={version}',
                    '-o', str(binary), './cmd/whip'], cwd=ROOT, check=True, timeout=300)


class ModuleScripts(HTMLParser):
    def __init__(self):
        super().__init__()
        self.sources = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'script' and attrs.get('type') == 'module' and attrs.get('src'):
            self.sources.append(attrs['src'].lstrip('/'))


def renderer_smoke(endpoint, manifest):
    # Compare every served file with the same build manifest used by packaging.
    # A placeholder index or an unembedded JS chunk cannot pass this test.
    for name, expected in manifest['files'].items():
        assert not name.startswith('/') and '..' not in Path(name).parts, name
        with urlopen(endpoint + '/' + name, timeout=10) as response:
            data = response.read()
            assert response.status == 200, name
            assert response.headers['Content-Security-Policy'] == manifest['csp'], name
            assert len(data) == expected['bytes'], name
            assert hashlib.sha256(data).hexdigest() == expected['sha256'], name
    with urlopen(endpoint + '/', timeout=10) as response:
        index = response.read()
    assert hashlib.sha256(index).hexdigest() == manifest['files']['index.html']['sha256']
    parser = ModuleScripts()
    parser.feed(index.decode())
    assert parser.sources, 'actual renderer must load a module entrypoint'
    assert all(name in manifest['files'] and manifest['files'][name]['bytes'] > 0
               for name in parser.sources), parser.sources
    with urlopen(endpoint + '/api/v4/web', timeout=10) as response:
        assert response.status == 200
        json.load(response)


@contextmanager
def native_rpc(socket_path):
    major = json.loads((ROOT / 'packages/protocol/schema/manifest.json').read_text())['major']
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(10)
        connection.connect(socket_path)
        with connection.makefile('rwb') as stream:
            next_id = 0

            def rpc(method, params):
                nonlocal next_id
                next_id += 1
                ident = str(next_id)
                stream.write(json.dumps({'jsonrpc': '2.0', 'id': ident,
                                         'method': method, 'params': params}).encode() + b'\n')
                stream.flush()
                raw = stream.readline((8 << 20) + 1)
                assert raw.endswith(b'\n') and len(raw) <= 8 << 20, 'missing or oversized native response'
                message = json.loads(raw)
                assert message.get('id') == ident and not message.get('error'), message
                return message['result']

            initialized = rpc('initialize', {'major': major})
            assert initialized['major'] == major and not initialized['network_client'], initialized
            yield rpc, initialized


def create_session(socket_path, home):
    with native_rpc(socket_path) as (rpc, initialized):
        definition = next(ref for ref in initialized['builtins'] if ref['id'] == 'coding')
        # No turn or provider call: the ordinary creation transaction captures
        # and persists this explicit model route for later restart verification.
        result = rpc('trees.create', {'creation_id': 'create-fresh-session',
                     'definition': definition, 'working_directory': str(home),
                     'metadata': {'title': None, 'archived': False, 'pinned': False},
                     'engine': 'starlark', 'permission_mode': 'prompt',
                     'overrides': {'automatic_title': False,
                                   'model': {'provider': 'fixture', 'name': 'fixture', 'effort': ''}}})
        assert not result['deleted'] and result['root'], result
        return result['root']['id']


def assert_saved_session(socket_path, session_id):
    # Fresh public reads after every process change establish persisted state
    # without coupling package acceptance to SQL columns or submitting a paid turn.
    with native_rpc(socket_path) as (rpc, _):
        saved = rpc('sessions.get', {'session_id': session_id})
        assert saved['id'] == session_id and saved['parent_id'] is None, saved
        assert saved['definition']['id'] == 'coding', saved
        assert saved['configuration']['model'] == {'provider': 'fixture', 'name': 'fixture', 'effort': ''}, saved
        assert saved['config_revision'] == '1', saved


def wait_for_gateway(status, version, previous_process=None, timeout=15):
    # Process epoch identifies a replacement owner; build identity alone is not
    # gateway readiness. Durable runtime identity must survive an explicit restart.
    deadline = time.monotonic() + timeout
    observed = None
    while time.monotonic() < deadline:
        observed = status()
        process = observed.get('process') or {}
        if (observed.get('state') == 'running' and process.get('build') == version
                and process.get('web_state') == 'running' and process.get('web_endpoint')
                and (previous_process is None or (
                    process.get('runtime_id') == previous_process['runtime_id']
                    and process.get('process_epoch') != previous_process['process_epoch']))):
            return observed
        time.sleep(0.1)
    raise AssertionError(f'daemon {version} did not reach managed gateway readiness: {observed}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path)
    parser.add_argument('--version', default='v1.0.0-alpha.9')
    parser.add_argument('--update-binary', type=Path)
    parser.add_argument('--update-version', default='v1.0.0-alpha.10')
    parser.add_argument('--browser', action='store_true')
    args = parser.parse_args()
    manifest = json.loads((ROOT / 'apps/web/renderer-manifest.json').read_text())
    assert manifest['schema'] == 1 and 'index.html' in manifest['files']
    assert platform.system() in ['Darwin', 'Linux'], 'native CLI packages support macOS/Linux'
    with tempfile.TemporaryDirectory(prefix='whipcode-package-') as directory:
        root = Path(directory).resolve()
        candidate = args.binary.resolve() if args.binary else root / 'candidate'
        newer = args.update_binary.resolve() if args.update_binary else None
        if not args.binary:
            build(candidate, args.version)
            newer = root / 'newer-candidate'
            build(newer, args.update_version)
        home = root / 'home'
        home.mkdir()
        tools = root / 'tools'
        tools.mkdir()
        destination = root / 'bin'
        env = {key: value for key, value in os.environ.items()
               if key in ['PATH', 'TMPDIR', 'TEMP', 'TMP', 'USER', 'LOGNAME', 'SHELL', 'LANG']}
        env.update(HOME=str(home), PATH=str(tools) + os.pathsep + env.get('PATH', '/usr/bin:/bin'),
                   WHIPCODE_BIN_DIR=str(destination),
                   WHIPCODE_LISTEN='127.0.0.1:0', WHIPCODE_NETWORK='1',
                   FIXTURE=str(root / 'fixture.json'))
        fixture = {'releases': [], 'assets': {}, 'fail': False, 'installer': str(ROOT / 'install.sh')}

        def release(binary, tag):
            directory = root / tag
            subprocess.run(['node', str(ROOT / 'scripts/build-installers.mjs'), tag, str(directory)],
                           env=env, check=True, capture_output=True, text=True, timeout=10)
            paths = {name: binary for name in ASSETS[:4]}
            paths.update({name: directory / name for name in ['install.sh', 'latest.sh']})
            sums = directory / 'SHA256SUMS'
            sums.write_text(''.join(hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + name + '\n'
                                    for name, path in paths.items()))
            paths['SHA256SUMS'] = sums
            result = {'tag_name': tag, 'draft': False, 'prerelease': '-' in tag.split('+')[0], 'assets': []}
            for name, path in paths.items():
                ident = len(fixture['assets']) + 1
                fixture['assets'][str(ident)] = str(path)
                result['assets'].append({'id': ident, 'name': name, 'size': path.stat().st_size})
            fixture['releases'].append(result)
            return directory / 'install.sh'

        def save_fixture():
            Path(env['FIXTURE']).write_text(json.dumps(fixture))

        pinned_installer = release(candidate, args.version)
        if newer:
            # A newer release is already discoverable: initial installation must
            # still use the embedded pin with no selection environment variables.
            release(newer, args.update_version)
        save_fixture()
        curl = tools / 'curl'
        curl.write_text('#!' + sys.executable + '\n' + r"""
import json, os, shutil, sys
from pathlib import Path
args = sys.argv[1:]
f = json.loads(Path(os.environ['FIXTURE']).read_text())
if f['fail']: sys.exit(22)
url = next(a for a in args if a.startswith('https://'))
path = None
if url == 'https://raw.githubusercontent.com/context-labs/whip/main/install.sh':
    path = f['installer']
elif url.startswith('https://api.github.com/repos/context-labs/whip/releases/assets/'):
    path = f['assets'][url.rsplit('/', 1)[1]]
elif url.startswith('https://api.github.com/repos/context-labs/whip/releases/tags/'):
    tag = url.rsplit('/', 1)[1]
    data = next((r for r in f['releases'] if r['tag_name'] == tag), None)
    if data is None: sys.exit(22)
elif url == 'https://api.github.com/repos/context-labs/whip/releases?per_page=100&page=1':
    data = f['releases']
else:
    sys.exit('unexpected fixture URL: ' + url)
if path:
    shutil.copyfile(path, args[args.index('-o') + 1])
else:
    sys.stdout.write(json.dumps(data))
""")
        curl.chmod(0o755)
        gh = tools / 'gh'
        gh.write_text('#!/bin/sh\nexit 1\n')
        gh.chmod(0o755)
        (tools / 'python3').symlink_to(sys.executable)
        run(Path('/bin/sh'), str(pinned_installer), env=env)
        binary = destination / 'whipcode'
        assert run(binary, '--version', env=env).stdout.strip() == 'whipcode ' + args.version
        run(binary, '--bench', env=env)
        assert not (home / '.whipcode').exists(), 'read-only benchmark initialized product state'
        run(binary, '--bench-init', env=env)
        host_file = home / '.whipcode/runtime-v4/host.json'
        assert host_file.is_file()
        host_before = host_file.read_bytes()
        run(binary, '--bench', env=env)
        assert host_file.read_bytes() == host_before
        assert not (home / '.whipcode/runtime-v4/state.db').exists()
        assert not (home / '.whipcode/config.json').exists()
        assert not (home / '.whip').exists()
        # The product identity is fixed even if someone renames the executable.
        renamed = destination / 'renamed-executable'
        binary.rename(renamed)
        assert run(renamed, '--version', env=env).stdout.strip() == 'whipcode ' + args.version
        renamed.rename(binary)
        # Exercise the explicit home override and long-socket fallback as well.
        app_home = home / ('custom-' + 'x' * 90)
        env['WHIPCODE_HOME'] = str(app_home)

        def status():
            return json.loads(run(binary, 'daemon', 'status', '--json', env=env).stdout)

        try:
            run(binary, 'daemon', 'start', env=env)
            initial = wait_for_gateway(status, args.version)
            assert (app_home / 'runtime-v4/host.json').stat().st_mode & 0o777 == 0o600
            assert (app_home / 'runtime-v4/state.db').is_file()
            renderer_smoke(initial['process']['web_endpoint'], manifest)
            session_id = create_session(initial['socket'], home)
            assert_saved_session(status()['socket'], session_id)
            run(binary, 'daemon', 'restart', env=env)
            restarted = wait_for_gateway(status, args.version, previous_process=initial['process'])
            assert_saved_session(status()['socket'], session_id)
            renderer_smoke(restarted['process']['web_endpoint'], manifest)

            # A failed download must neither replace the binary nor restart runtime.
            fixture['fail'] = True
            save_fixture()
            assert run(binary, 'update', env=env, check=False).returncode != 0
            assert status()['process']['pid'] == restarted['process']['pid']
            assert status()['process']['process_epoch'] == restarted['process']['process_epoch']
            assert run(binary, '--version', env=env).stdout.strip() == 'whipcode ' + args.version
            fixture['fail'] = False
            if newer:
                save_fixture()
                # Updater fetches the raw source installer, not the old release's
                # pinned asset. It must ignore stale inherited placement/version.
                env['WHIPCODE_VERSION'] = args.version
                env['WHIPCODE_BIN_DIR'] = str(root / 'wrong-destination')
                run(binary, 'update', env=env)
                updated = wait_for_gateway(status, args.update_version, previous_process=restarted['process'])
                assert run(binary, '--version', env=env).stdout.strip() == 'whipcode ' + args.update_version
                assert not (root / 'wrong-destination').exists()
                assert_saved_session(status()['socket'], session_id)
                renderer_smoke(updated['process']['web_endpoint'], manifest)
            print('PASS packaged pinned installer without selection env, fixed identity, fresh config/session, renderer digests, daemon lifecycle and safe failure')
            if newer:
                print('PASS new-to-new update, destination/channel selection, daemon reconnect and session persistence')
        finally:
            run(binary, 'daemon', 'stop', '--timeout', '5s', env=env)
            assert status()['state'] != 'running', 'owned daemon did not stop'
        if args.browser:
            subprocess.run(['node', 'scripts/web-gateway-smoke.mjs', str(binary), '--browser'],
                           cwd=ROOT, check=True, timeout=120)


if __name__ == '__main__':
    main()
