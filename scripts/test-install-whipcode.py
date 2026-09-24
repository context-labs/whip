#!/usr/bin/env python3
"""Exercise the real installer with isolated homes and a deterministic GitHub API."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

INSTALLER = Path(__file__).resolve().parents[1] / 'install-whipcode.sh'
NAMES = ['whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64',
         'whipcode-darwin-arm64', 'SHA256SUMS', 'install-whipcode.sh']


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='whipcode-installer-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / 'tools'
        self.bin.mkdir()
        self.home = self.root / 'home'
        self.home.mkdir()
        self.dest = self.root / 'bin with spaces'
        self.dest.mkdir()
        self.data = {'pages': {}, 'assets': {}, 'fail': '', 'tags': {}}
        self.env = {**os.environ, 'HOME': str(self.home),
                    'PATH': str(self.bin) + os.pathsep + '/usr/bin:/bin',
                    'WHIPCODE_BIN_DIR': str(self.dest), 'WHIPCODE_VERSION': '',
                    'WHIP_VERSION': 'v99.0.0', 'GH_TOKEN': '',
                    'FIXTURE': str(self.root / 'fixture.json'),
                    'OS': 'Linux', 'ARCH': 'x86_64'}
        self.tool('uname', '#!/bin/sh\ncase "$1" in -s) echo "$OS";; -m) echo "$ARCH";; esac\n')
        self.tool('gh', '#!/bin/sh\nexit 1\n')
        (self.bin / 'python3').symlink_to(sys.executable)
        self.tool('curl', '#!' + sys.executable + '\n' + r'''
import json, os, sys
from pathlib import Path
args = sys.argv[1:]
url = next(a for a in args if a.startswith('https://'))
p = Path(os.environ['FIXTURE'])
f = json.loads(p.read_text())
with p.with_suffix('.requests').open('a') as log:
    log.write(json.dumps(args)+'\n')
if f['fail'] and f['fail'] in url:
    sys.exit(22)
if '/releases/assets/' in url:
    output = f['assets'][url.rsplit('/', 1)[1]]
elif '/releases/tags/' in url:
    tag = url.rsplit('/', 1)[1]
    if tag not in f['tags']: sys.exit(22)
    output = json.dumps(f['tags'][tag])
else:
    page = url.rsplit('page=', 1)[1]
    output = json.dumps(f['pages'].get(page, []))
if '-o' in args:
    Path(args[args.index('-o')+1]).write_text(output)
else:
    sys.stdout.write(output)
''')
        self.data['pages']['1'] = [self.release(10), self.release(9)]
        (self.dest / 'whip').write_text('stable sibling must survive')

    def tool(self, name, content):
        p = self.bin / name
        p.write_text(content)
        p.chmod(0o755)

    def release(self, number, tag=None):
        body = f'#!/bin/sh\nprintf "whipcode v0.0.{number}\\n"\n'
        digest = hashlib.sha256(body.encode()).hexdigest()
        sums = ''.join(f'{digest}  {name}\n' for name in NAMES[:4])
        release = {'tag_name': tag or f'whipcode-v0.0.{number}', 'draft': False,
                   'prerelease': True, 'assets': []}
        for i, name in enumerate(NAMES):
            ident = number * 10 + i + 1
            payload = sums if name == 'SHA256SUMS' else body
            self.data['assets'][str(ident)] = payload
            release['assets'].append({'id': ident, 'name': name, 'size': len(payload)})
        self.data['tags'][release['tag_name']] = release
        return release

    def install(self, success=True):
        Path(self.env['FIXTURE']).write_text(json.dumps(self.data))
        result = subprocess.run(['/bin/sh', str(INSTALLER)], env=self.env,
                                capture_output=True, text=True, timeout=30)
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.dest / 'whip').read_text(), 'stable sibling must survive')
        self.assertEqual(list(self.dest.glob('.whipcode.*')), [])
        return result

    def installed_version(self):
        return subprocess.check_output([str(self.dest / 'whipcode'), '--version'], text=True).strip()

    def test_all_platforms_and_numeric_order(self):
        for os_name, arch in [('Linux', 'x86_64'), ('Linux', 'aarch64'),
                              ('Darwin', 'arm64'), ('Darwin', 'amd64')]:
            with self.subTest(os=os_name, arch=arch):
                self.env.update(OS=os_name, ARCH=arch)
                self.install()
                self.assertEqual(self.installed_version(), 'whipcode v0.0.10')
                self.assertTrue((self.dest / 'whipcode').stat().st_mode & 0o111)

    def test_pagination_drafts_missing_assets_and_stable(self):
        draft = self.release(999)
        draft['draft'] = True
        incomplete = self.release(998)
        incomplete['assets'].pop()
        stable = self.release(997, 'v997.0.0')
        self.data['pages'] = {'1': [self.release(9), draft, incomplete, stable] + [{}] * 96,
                              '2': [self.release(10)]}
        self.install()
        self.assertEqual(self.installed_version(), 'whipcode v0.0.10')
        self.assertIn('page=2', Path(self.env['FIXTURE']).with_suffix('.requests').read_text())

    def test_pinned_install_and_explicit_rollback(self):
        self.install()
        self.env['WHIPCODE_VERSION'] = 'whipcode-v0.0.9'
        self.install()
        self.assertEqual(self.installed_version(), 'whipcode v0.0.9')

    def test_no_automatic_downgrade(self):
        self.install()
        self.data['pages']['1'] = [self.release(9)]
        self.install(False)
        self.assertEqual(self.installed_version(), 'whipcode v0.0.10')

    def test_foreign_pin_rejected_before_network(self):
        self.env['WHIPCODE_VERSION'] = 'v0.5.14'
        self.install(False)
        self.assertFalse(Path(self.env['FIXTURE']).with_suffix('.requests').exists())

    def test_checksum_mismatch_keeps_existing_binary(self):
        self.install()
        self.data['assets']['101'] = 'tampered binary'
        self.assertIn('CHECKSUM MISMATCH', self.install(False).stderr)
        self.assertEqual(self.installed_version(), 'whipcode v0.0.10')

    def test_failed_asset_keeps_existing_binary(self):
        self.install()
        self.data['fail'] = '/assets/'
        self.install(False)
        self.assertEqual(self.installed_version(), 'whipcode v0.0.10')

    def test_failed_later_page_never_installs_partial_candidate(self):
        self.data['pages']['1'] = [self.release(9)] + [{}] * 99
        self.data['fail'] = 'page=2'
        self.install(False)
        self.assertFalse((self.dest / 'whipcode').exists())

    def test_no_complete_release(self):
        release = self.release(10)
        release['assets'][0]['size'] = 0
        self.data['pages']['1'] = [release]
        self.install(False)
        self.assertFalse((self.dest / 'whipcode').exists())

    def test_unsupported_platform(self):
        self.env['OS'] = 'Windows'
        self.assertIn('unsupported OS', self.install(False).stderr)

    def test_authentication(self):
        self.env['GH_TOKEN'] = 'fixture-token'
        self.install()
        requests = Path(self.env['FIXTURE']).with_suffix('.requests').read_text()
        self.assertIn('Authorization: Bearer fixture-token', requests)

    def test_missing_parser(self):
        (self.bin / 'python3').unlink()
        self.env['PATH'] = str(self.bin)
        for command in ['mktemp', 'rm']:
            (self.bin / command).symlink_to(shutil.which(command))
        self.assertIn('required command not found: python3', self.install(False).stderr)


if __name__ == '__main__':
    unittest.main()
