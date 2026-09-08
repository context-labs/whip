#!/usr/bin/env python3
"""Compile both distributions and prove independent configuration/daemon ownership."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]


def run(binary, *args, env, check=True):
    result = subprocess.run([str(binary), *args], env=env, cwd=env['HOME'],
                            capture_output=True, text=True, timeout=30)
    if check and result.returncode:
        raise AssertionError(f'{binary.name} {args}: {result.stdout}{result.stderr}')
    return result


with tempfile.TemporaryDirectory(prefix='whipcode-distributions-') as directory:
    root = Path(directory)
    binaries = {}
    for name, version in [('whip', 'v0.5.14'), ('whipcode', 'whipcode-v0.0.1')]:
        binary = root / name
        subprocess.run(['go', 'build', '-trimpath', '-ldflags',
                        f'-X main.version={version} -X github.com/context-labs/whip/internal/buildinfo.Name={name}',
                        '-o', str(binary), './cmd/whip'], cwd=ROOT, check=True)
        binaries[name] = binary
    home = root / 'home'
    home.mkdir()
    env = {key: value for key, value in os.environ.items()
           if key in ['PATH', 'TMPDIR', 'TEMP', 'TMP', 'USER', 'LOGNAME', 'SHELL', 'LANG']}
    env['HOME'] = str(home)
    for name, binary in binaries.items():
        output = run(binary, '--version', env=env).stdout.strip()
        expected = 'whip v0.5.14' if name == 'whip' else 'whipcode v0.0.1'
        assert output == expected, output
        run(binary, '--bench', env=env)
        assert (home / f'.{name}' / 'config.json').exists()
    renamed = root / 'renamed-executable'
    binaries['whipcode'].rename(renamed)
    assert run(renamed, '--version', env=env).stdout.strip() == 'whipcode v0.0.1'
    run(renamed, '--bench', env=env)
    assert not (home / '.renamed-executable').exists()
    renamed.rename(binaries['whipcode'])

    # Conflicting application overrides and stable networking must not redirect
    # whipcode. Deliberately use long homes to exercise the hashed socket fallback.
    stable_home = home / ('stable-' + 'x' * 90)
    code_home = home / ('code-' + 'x' * 90)
    env.update(WHIP_HOME=str(stable_home), WHIPCODE_HOME=str(code_home), WHIP_NETWORK='1')
    stable = binaries['whip']
    code = binaries['whipcode']
    def status(binary):
        return json.loads(run(binary, 'daemon', 'status', '--json', env=env).stdout)
    try:
        run(stable, 'daemon', 'start', env=env)
        run(code, 'daemon', 'start', env=env)
        first_stable, first_code = status(stable), status(code)
        assert first_stable['state'] == first_code['state'] == 'running'
        assert first_stable['pid'] != first_code['pid']
        assert first_stable['socket'] != first_code['socket']
        assert first_stable['daemon_build'] == 'v0.5.14'
        assert first_code['daemon_build'] == 'whipcode-v0.0.1'
        assert not first_code.get('network_endpoint'), first_code
        for owned_home in [stable_home, code_home]:
            assert (owned_home / 'config.json').is_file()
            assert (owned_home / 'runtime-v2' / 'sessions.db').is_file()
        assert (stable_home / 'config.json').stat().st_mode & 0o777 == 0o600
        run(code, 'daemon', 'restart', env=env)
        assert status(stable)['pid'] == first_stable['pid']
        assert status(code)['daemon_build'] == 'whipcode-v0.0.1'

        # Exercise updateCLI through a fake curl that supplies an installer which
        # verifies the destination and atomically installs a newer real binary.
        newer = root / 'new-whipcode'
        subprocess.run(['go', 'build', '-trimpath', '-ldflags',
                        '-X main.version=whipcode-v0.0.2 -X github.com/context-labs/whip/internal/buildinfo.Name=whipcode',
                        '-o', str(newer), './cmd/whip'], cwd=ROOT, check=True)
        tools = root / 'tools'
        tools.mkdir()
        curl = tools / 'curl'
        curl.write_text('''#!/bin/sh
case "$*" in *whip-rlm/install-whipcode.sh*) ;; *) exit 4;; esac
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then shift; cp "$FIXTURE_INSTALLER" "$1"; exit; fi
  shift
done
exit 5
''')
        curl.chmod(0o755)
        installer = root / 'fixture-installer.sh'
        installer.write_text('''#!/bin/sh
set -eu
test "$WHIPCODE_BIN_DIR" = "$EXPECTED_BIN_DIR"
cp "$NEW_BINARY" "$WHIPCODE_BIN_DIR/.update-fixture"
chmod +x "$WHIPCODE_BIN_DIR/.update-fixture"
mv "$WHIPCODE_BIN_DIR/.update-fixture" "$WHIPCODE_BIN_DIR/whipcode"
''')
        env.update(PATH=str(tools) + os.pathsep + env['PATH'], FIXTURE_INSTALLER=str(installer),
                   EXPECTED_BIN_DIR=str(root.resolve()), NEW_BINARY=str(newer),
                   WHIPCODE_BIN_DIR=str(root / 'wrong-destination'))
        run(code, 'update', env=env)
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            updated = status(code)
            if updated.get('daemon_build') == 'whipcode-v0.0.2':
                break
            time.sleep(0.1)
        else:
            raise AssertionError(f'updated daemon did not restart: {updated}')
        assert status(stable)['pid'] == first_stable['pid']
        assert run(code, '--version', env=env).stdout.strip() == 'whipcode v0.0.2'
        run(code, 'daemon', 'stop', env=env)
        assert status(stable)['state'] == 'running'
        assert status(stable)['pid'] == first_stable['pid']
        print('PASS: both compiled identities, home isolation, long sockets, independent restart and self-update')
    finally:
        for binary in binaries.values():
            run(binary, 'daemon', 'stop', env=env, check=False)
