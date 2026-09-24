#!/usr/bin/env python3
"""Packaged whipcode acceptance: real installer, renderer, session and daemon.

Run after building/packing the renderer (never substitutes an HTML placeholder).
No arguments builds two native candidates into temporary files and checks updates.
--binary PATH --version TAG checks an existing release candidate without rebuilding.
--browser additionally runs the existing real Chromium gateway/bootstrap smoke.
All installation, state and daemon processes belong to disposable homes.
"""
import argparse
import hashlib
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[1]
ASSETS = ['whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64',
          'whipcode-darwin-arm64', 'SHA256SUMS', 'install.sh']


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
    with urlopen(endpoint + '/api/v3/web', timeout=10) as response:
        assert response.status == 200
        json.load(response)


def create_session(socket_path, home):
    major = json.loads((ROOT / 'packages/protocol/schema/manifest.json').read_text())['major']
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
        connection.settimeout(10)
        connection.connect(socket_path)
        with connection.makefile('rwb') as stream:
            next_id = 0

            def rpc(method, params):
                nonlocal next_id
                next_id += 1
                stream.write(json.dumps({'jsonrpc': '2.0', 'id': next_id,
                                         'method': method, 'params': params}).encode() + b'\n')
                stream.flush()
                while True:
                    message = json.loads(stream.readline(1024 * 1024))
                    if message.get('id') == next_id:
                        assert not message.get('error'), message
                        return message['result']

            rpc('initialize', {'protocol_major': major, 'build_id': 'packaged-acceptance',
                               'client_kind': 'automation', 'client_id': 'packaged-acceptance'})
            # Persist an agent session without starting a turn or contacting a
            # provider. Explicit routing avoids requiring first-run credentials.
            result = rpc('command.submit', {'command_id': 'create-fresh-session', 'scope': 'daemon',
                         'operation': 'session.create', 'payload': {'kind': 'agent', 'cwd': str(home),
                             'model': 'fixture', 'provider': 'fixture', 'permission_mode': 'prompt',
                             'execution_engine': 'starlark'}})
            deadline = time.monotonic() + 10
            while result['status'] in ['queued', 'running', 'waiting'] and time.monotonic() < deadline:
                time.sleep(0.05)
                result = rpc('command.status', {'command_id': 'create-fresh-session'})
            assert result['status'] == 'succeeded', result
            return result['result']['root_id']


def assert_saved_session(home, session_id):
    # Empty sessions are deliberately absent from CLI resume history. Inspect
    # persistence read-only rather than submit a paid model turn to list one.
    database = home / 'runtime-v2/sessions.db'
    with sqlite3.connect(database.as_uri() + '?mode=ro', uri=True) as connection:
        saved = connection.execute('SELECT kind, model, provider FROM sessions WHERE id=?',
                                   (session_id,)).fetchone()
        assert saved == ('agent', 'fixture', 'fixture'), saved


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
                   WHIPCODE_BIN_DIR=str(destination), WHIPCODE_VERSION=args.version,
                   WHIPCODE_LISTEN='127.0.0.1:0', WHIPCODE_NETWORK='1',
                   FIXTURE=str(root / 'fixture.json'))
        fixture = {'releases': [], 'assets': {}, 'fail': False, 'installer': str(ROOT / 'install.sh')}

        def release(binary, tag):
            digest = hashlib.sha256(binary.read_bytes()).hexdigest()
            sums = root / (tag + '-SHA256SUMS')
            sums.write_text(''.join(f'{digest}  {name}\n' for name in ASSETS[:4]) +
                            hashlib.sha256((ROOT / 'install.sh').read_bytes()).hexdigest() + '  install.sh\n')
            result = {'tag_name': tag, 'draft': False, 'prerelease': '-' in tag.split('+')[0], 'assets': []}
            for name in ASSETS:
                ident = len(fixture['assets']) + 1
                path = sums if name == 'SHA256SUMS' else ROOT / 'install.sh' if name == 'install.sh' else binary
                fixture['assets'][str(ident)] = str(path)
                result['assets'].append({'id': ident, 'name': name, 'size': path.stat().st_size})
            fixture['releases'].append(result)

        def save_fixture():
            Path(env['FIXTURE']).write_text(json.dumps(fixture))

        release(candidate, args.version)
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
        run(Path('/bin/sh'), str(ROOT / 'install.sh'), env=env)
        binary = destination / 'whipcode'
        assert run(binary, '--version', env=env).stdout.strip() == 'whipcode ' + args.version
        run(binary, '--bench', env=env)
        assert (home / '.whipcode/config.json').is_file()
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
            initial = status()
            assert initial['state'] == 'running' and initial['daemon_build'] == args.version, initial
            assert initial['gateway']['state'] == 'ready', initial
            assert (app_home / 'config.json').stat().st_mode & 0o777 == 0o600
            assert (app_home / 'runtime-v2/sessions.db').is_file()
            renderer_smoke(initial['network_endpoint'], manifest)
            session_id = create_session(initial['socket'], home)
            assert_saved_session(app_home, session_id)
            run(binary, 'daemon', 'restart', env=env)
            restarted = status()
            assert restarted['pid'] != initial['pid'], restarted
            assert_saved_session(app_home, session_id)
            renderer_smoke(restarted['network_endpoint'], manifest)

            # A failed download must neither replace the binary nor restart runtime.
            fixture['fail'] = True
            save_fixture()
            assert run(binary, 'update', env=env, check=False).returncode != 0
            assert status()['pid'] == restarted['pid']
            assert run(binary, '--version', env=env).stdout.strip() == 'whipcode ' + args.version
            fixture['fail'] = False
            if newer:
                release(newer, args.update_version)
                save_fixture()
                # Updater must ignore a stale pin and install beside its actual
                # executable, not blindly trust an inherited destination override.
                env['WHIPCODE_BIN_DIR'] = str(root / 'wrong-destination')
                run(binary, 'update', env=env)
                deadline = time.monotonic() + 15
                while time.monotonic() < deadline:
                    updated = status()
                    if updated.get('daemon_build') == args.update_version:
                        break
                    time.sleep(0.1)
                else:
                    raise AssertionError(f'updated daemon did not restart: {updated}')
                assert run(binary, '--version', env=env).stdout.strip() == 'whipcode ' + args.update_version
                assert not (root / 'wrong-destination').exists()
                assert_saved_session(app_home, session_id)
                renderer_smoke(updated['network_endpoint'], manifest)
            print('PASS packaged installer, fixed identity, fresh config/session, renderer digests, daemon lifecycle and safe failure')
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
