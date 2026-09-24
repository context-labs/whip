#!/usr/bin/env python3
"""Exercise main/install.sh in disposable homes against a deterministic GitHub API."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

INSTALLER = Path(__file__).resolve().parents[1] / 'install.sh'
NAMES = ['whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64',
         'whipcode-darwin-arm64', 'SHA256SUMS', 'install.sh']


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='whipcode-installer-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / 'tools'
        self.bin.mkdir()
        self.home = self.root / 'home'
        self.home.mkdir()
        self.dest = self.root / "bin with spaces and 'quotes'"
        self.dest.mkdir()
        self.data = {'pages': {}, 'assets': {}, 'fail': '', 'tags': {}}
        self.env = {'HOME': str(self.home),
                    'PATH': str(self.bin) + os.pathsep + '/usr/bin:/bin',
                    'TMPDIR': str(self.root),
                    'WHIPCODE_BIN_DIR': str(self.dest),
                    'WHIPCODE_CHANNEL': 'prerelease',
                    'FIXTURE': str(self.root / 'fixture.json'),
                    'OS': 'Linux', 'ARCH': 'x86_64'}
        self.tool('uname', '#!/bin/sh\ncase "$1" in -s) echo "$OS";; -m) echo "$ARCH";; esac\n')
        self.tool('gh', '#!/bin/sh\nexit 1\n')
        (self.bin / 'python3').symlink_to(sys.executable)
        self.tool('curl', '#!' + sys.executable + '\n' + r"""
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
""")
        self.data['pages']['1'] = [self.release('v1.0.0-alpha.9'), self.release('v1.0.0-alpha.10')]

    def tool(self, name, content):
        path = self.bin / name
        path.write_text(content)
        path.chmod(0o755)

    def release(self, tag, prerelease=None):
        body = f'#!/bin/sh\nprintf "whipcode {tag}\\n"\n'
        digest = hashlib.sha256(body.encode()).hexdigest()
        sums = ''.join(f'{digest}  {name}\n' for name in NAMES[:4])
        release = {'tag_name': tag, 'draft': False,
                   'prerelease': '-' in tag if prerelease is None else prerelease, 'assets': []}
        for name in NAMES:
            ident = len(self.data['assets']) + 1
            payload = sums if name == 'SHA256SUMS' else body
            self.data['assets'][str(ident)] = payload
            release['assets'].append({'id': ident, 'name': name, 'size': len(payload)})
        self.data['tags'][tag] = release
        return release

    def asset_id(self, name, tag='v1.0.0-alpha.10'):
        return str(next(a['id'] for a in self.data['tags'][tag]['assets'] if a['name'] == name))

    def install(self, success=True):
        Path(self.env['FIXTURE']).write_text(json.dumps(self.data))
        result = subprocess.run(['/bin/sh', str(INSTALLER)], env=self.env,
                                capture_output=True, text=True, timeout=30)
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(list(self.dest.glob('.whipcode.*')), [])
        self.assertEqual(list(self.root.glob('tmp.*')), [])
        return result

    def installed_version(self):
        return subprocess.check_output([str(self.dest / 'whipcode'), '--version'], text=True).strip()

    def test_all_platforms_and_numeric_alpha_order(self):
        for os_name, arch in [('Linux', 'x86_64'), ('Linux', 'aarch64'),
                              ('Darwin', 'arm64'), ('Darwin', 'amd64')]:
            with self.subTest(os=os_name, arch=arch):
                self.env.update(OS=os_name, ARCH=arch)
                self.install()
                self.assertEqual(self.installed_version(), 'whipcode v1.0.0-alpha.10')
                self.assertEqual((self.dest / 'whipcode').stat().st_mode & 0o777, 0o755)

    def test_default_stable_fails_before_v1(self):
        del self.env['WHIPCODE_CHANNEL']
        self.data['pages']['1'] += [self.release('v0.6.5'), self.release('whipcode-v0.0.999'),
                                    self.release('desktop-v9.0.0')]
        self.assertIn('WHIPCODE_CHANNEL=prerelease', self.install(False).stderr)
        self.assertFalse((self.dest / 'whipcode').exists())

    def test_stable_selects_v1_plus_not_latest_or_prerelease(self):
        del self.env['WHIPCODE_CHANNEL']
        self.data['pages']['1'] += [self.release(tag) for tag in
                                  ['v1.9.0', 'v1.10.0', 'v1.2.0', 'v2.0.0-alpha.1', 'v0.99.99']]
        self.install()
        self.assertEqual(self.installed_version(), 'whipcode v1.10.0')

    def test_semver_precedence_and_metadata(self):
        ordered = ['v1.0.0-alpha', 'v1.0.0-alpha.1', 'v1.0.0-alpha.9', 'v1.0.0-alpha.10',
                   'v1.0.0-alpha.beta', 'v1.0.0-beta', 'v1.0.0-beta.2',
                   'v1.0.0-beta.11', 'v1.0.0-rc.1', 'v1.0.0+build.17', 'v2.0.0-alpha.1']
        for tag in ordered:
            with self.subTest(tag=tag):
                self.data['pages']['1'] = [self.release(t) for t in reversed(ordered[:ordered.index(tag)+1])]
                self.install()
                self.assertEqual(self.installed_version(), 'whipcode ' + tag)

    def test_pagination_drafts_historical_and_missing_assets(self):
        draft = self.release('v9.0.0')
        draft['draft'] = True
        incomplete = self.release('v8.0.0')
        incomplete['assets'].pop()
        mismatched = self.release('v7.0.0-alpha.1', prerelease=False)
        foreign = [self.release(t) for t in ['v0.6.5', 'whipcode-v0.0.999', 'desktop-v999.0.0']]
        self.data['pages'] = {'1': [self.release('v1.0.0-alpha.9'), draft, incomplete, mismatched] + foreign + [{}] * 93,
                              '2': [self.release('v1.0.0-alpha.10')]}
        self.install()
        self.assertEqual(self.installed_version(), 'whipcode v1.0.0-alpha.10')
        self.assertIn('page=2', Path(self.env['FIXTURE']).with_suffix('.requests').read_text())

    def test_pin_overrides_channel_and_allows_explicit_rollback(self):
        self.install()
        self.env.update(WHIPCODE_CHANNEL='stable', WHIPCODE_VERSION='v1.0.0-alpha.9')
        self.install()
        self.assertEqual(self.installed_version(), 'whipcode v1.0.0-alpha.9')

    def test_no_automatic_downgrade(self):
        self.install()
        self.data['pages']['1'] = [self.release('v1.0.0-alpha.9')]
        self.assertIn('explicit rollback', self.install(False).stderr)
        self.assertEqual(self.installed_version(), 'whipcode v1.0.0-alpha.10')

    def test_invalid_pin_rejected_before_network(self):
        for tag in ['v0.6.5', 'whipcode-v0.0.1', 'desktop-v1.0.0', 'v01.0.0',
                    'v1.0.0-alpha.01', 'v1.0', 'v1.0.0/../../latest', 'latest']:
            with self.subTest(tag=tag):
                self.env['WHIPCODE_VERSION'] = tag
                self.install(False)
                self.assertFalse(Path(self.env['FIXTURE']).with_suffix('.requests').exists())

    def test_invalid_channel_rejected_before_network(self):
        self.env['WHIPCODE_CHANNEL'] = 'nightly'
        self.assertIn('WHIPCODE_CHANNEL', self.install(False).stderr)
        self.assertFalse(Path(self.env['FIXTURE']).with_suffix('.requests').exists())

    def test_checksum_mismatch_keeps_existing_binary(self):
        self.install()
        self.data['assets'][self.asset_id(NAMES[0])] = 'tampered binary'
        self.assertIn('CHECKSUM MISMATCH', self.install(False).stderr)
        self.assertEqual(self.installed_version(), 'whipcode v1.0.0-alpha.10')

    def test_missing_or_duplicate_checksum_keeps_existing_binary(self):
        self.install()
        ident = self.asset_id('SHA256SUMS')
        original = self.data['assets'][ident]
        for sums in ['', original + original]:
            self.data['assets'][ident] = sums
            self.install(False)
            self.assertEqual(self.installed_version(), 'whipcode v1.0.0-alpha.10')

    def test_checksum_valid_wrong_binary_version_is_not_installed(self):
        self.install()
        body = '#!/bin/sh\necho "whipcode v1.0.0-alpha.1"\n'
        self.data['assets'][self.asset_id(NAMES[0])] = body
        self.data['assets'][self.asset_id('SHA256SUMS')] = hashlib.sha256(body.encode()).hexdigest() + '  ' + NAMES[0] + '\n'
        self.assertIn('binary version', self.install(False).stderr)
        self.assertEqual(self.installed_version(), 'whipcode v1.0.0-alpha.10')

    def test_failed_asset_keeps_existing_binary(self):
        self.install()
        self.data['fail'] = '/assets/'
        self.install(False)
        self.assertEqual(self.installed_version(), 'whipcode v1.0.0-alpha.10')

    def test_failed_atomic_replace_keeps_existing_binary_and_cleans_staging(self):
        self.install()
        self.tool('mv', '#!/bin/sh\nexit 1\n')
        self.assertIn('could not replace', self.install(False).stderr)
        self.assertEqual(self.installed_version(), 'whipcode v1.0.0-alpha.10')

    def test_failed_later_page_never_installs_partial_candidate(self):
        self.data['pages']['1'] = [self.release('v1.0.0-alpha.9')] + [{}] * 99
        self.data['fail'] = 'page=2'
        self.install(False)
        self.assertFalse((self.dest / 'whipcode').exists())

    def test_requires_exact_complete_asset_set(self):
        for mutation in ['zero', 'missing', 'extra', 'duplicate']:
            with self.subTest(mutation=mutation):
                release = self.release('v1.0.0-alpha.11')
                if mutation == 'zero':
                    release['assets'][0]['size'] = 0
                elif mutation == 'missing':
                    release['assets'].pop()
                elif mutation == 'extra':
                    release['assets'].append({'name': 'old-product', 'id': 999, 'size': 1})
                else:
                    release['assets'].append(release['assets'][0])
                self.data['pages']['1'] = [release]
                self.install(False)
                self.assertFalse((self.dest / 'whipcode').exists())

    def test_directory_destination_is_never_treated_as_success(self):
        (self.dest / 'whipcode').mkdir()
        self.assertIn('destination is a directory', self.install(False).stderr)
        self.assertEqual(list((self.dest / 'whipcode').iterdir()), [])

    def test_default_destination_is_user_local(self):
        del self.env['WHIPCODE_BIN_DIR']
        self.install()
        self.assertTrue((self.home / '.local/bin/whipcode').is_file())
        self.assertFalse((self.dest / 'whipcode').exists())

    def test_shell_profile_quotes_destination_and_is_idempotent(self):
        self.install()
        original = (self.home / '.profile').read_text()
        self.install()
        self.assertEqual(original, (self.home / '.profile').read_text())
        result = subprocess.run(['/bin/sh', '-c', '. "$HOME/.profile"; command -v whipcode'],
                                env=self.env, text=True, capture_output=True, check=True)
        self.assertEqual(Path(result.stdout.strip()).resolve(), (self.dest / 'whipcode').resolve())

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
