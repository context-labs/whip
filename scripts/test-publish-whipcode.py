#!/usr/bin/env python3
"""Test release publication state transitions without writing to GitHub."""
import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().with_name('publish-whipcode.sh')
SOURCE = 'a' * 40
NAMES = ['whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64', 'whipcode-darwin-arm64']


class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='whipcode-publisher-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'artifacts').mkdir()
        self.tools = self.root / 'tools'
        self.tools.mkdir()
        for name in NAMES:
            (self.root / 'artifacts' / name).write_text(name)
        (self.root / 'install.sh').write_text('installer')
        self.state = {'exists': False, 'draft': True, 'tag': False, 'tag_sha': SOURCE,
                      'target': SOURCE, 'head': SOURCE, 'calls': [], 'fail_upload': False,
                      'supersede_upload': False, 'prerelease': True}
        self.state_path = self.root / 'state.json'
        self.env = {**os.environ, 'PATH': str(self.tools) + os.pathsep + os.environ['PATH'],
                    'STATE': str(self.state_path), 'SOURCE_SHA': SOURCE,
                    'RELEASE_TAG': 'v1.0.0-alpha.1', 'GH_REPO': 'context-labs/whip',
                    'RELEASE_MODE': 'alpha', 'WHIP_RELEASE_ENABLED': 'true',
                    'WHIP_RELEASE_BASELINE': 'c' * 40}
        for tool in ['gh', 'git']:
            p = self.tools / tool
            p.write_text('#!' + sys.executable + '\n' + r'''
import hashlib, json, os, sys
from pathlib import Path
p=Path(os.environ['STATE']); s=json.loads(p.read_text())
a=sys.argv[1:]; tool=Path(sys.argv[0]).name
s['calls'].append([tool]+a)
def done(code=0, output=''):
    p.write_text(json.dumps(s)); print(output,end=''); sys.exit(code)
if tool=='git':
    if a[0]=='ls-remote': done(128 if s.get('fail_lookup') else (0 if s['tag'] else 2))
    if a[0]=='fetch': done(1 if s.get('fail_fetch') else 0)
    if a[0]=='rev-parse': done(output=(s.get('checkout', os.environ['SOURCE_SHA']) if a[1]=='HEAD' else s['tag_sha'])+'\n')
    if a[0]=='merge-base': done(1 if s.get('pre_baseline') else 0)
if a[0]=='api':
    if any('/actions/workflows/' in value for value in a):
        done(1 if s.get('fail_workflow') else 0, s.get('workflow_state', 'active'))
    if '--method' in a:
        if s.get('fail_tag'): done(1)
        s['tag']=True; done()
    done(1 if s.get('fail_head') else 0, s['head']+'\n')
if a[:2]==['release','view']:
    if not s['exists']: done(1)
    names=['whipcode-linux-x64','whipcode-linux-arm64','whipcode-darwin-x64',
           'whipcode-darwin-arm64','SHA256SUMS','install.sh']
    if s.get('missing_asset'): names.pop()
    if s.get('extra_asset'): names.append('unexpected.txt')
    done(output=json.dumps({'isDraft':s['draft'],'isPrerelease':s['prerelease'],'targetCommitish':s['target'],
                            'assets':[{'name':n,'size':1} for n in names]}))
if a[:2]==['release','create']:
    s['exists']=True; s['draft']=True
    if s.get('disable_create'): s['workflow_state']='disabled_manually'
    done()
if a[:2]==['release','upload']:
    if s['fail_upload']: done(1)
    if s['supersede_upload']: s['head']='b'*40
    if s.get('disable_upload'): s['workflow_state']='disabled_manually'
    done()
if a[:2]==['release','edit']:
    s['draft']=False; s['prerelease']='--prerelease=false' not in a; done()
if a[:2]==['release','download']:
    dest=Path(a[a.index('--dir')+1]); dest.mkdir(parents=True)
    names=['whipcode-linux-x64','whipcode-linux-arm64','whipcode-darwin-x64','whipcode-darwin-arm64']
    sums=''
    for name in names:
        # Previously published bytes differ from a new rebuild but remain valid.
        body='published '+name
        (dest/name).write_text(body)
        sums+=hashlib.sha256(body.encode()).hexdigest()+'  '+name+'\n'
    if s.get('corrupt_published'): (dest/names[0]).write_text('corrupted')
    installer='different' if s.get('wrong_installer') else 'installer'
    (dest/'install.sh').write_text(installer)
    sums+=hashlib.sha256(installer.encode()).hexdigest()+'  install.sh\n'
    if s.get('incomplete_manifest'): sums=''
    if s.get('unsafe_manifest'): sums=sums.replace('install.sh', '../install.sh')
    (dest/'SHA256SUMS').write_text(sums)
    done()
done(9)
''')
            p.chmod(0o755)
        if not shutil.which('sha256sum'):
            shim = self.tools / 'sha256sum'
            shim.write_text('#!/bin/sh\nexec shasum -a 256 "$@"\n')
            shim.chmod(0o755)

    def publish(self, success=True):
        self.state_path.write_text(json.dumps(self.state))
        p = subprocess.run(['bash', str(SCRIPT)], cwd=self.root, env=self.env,
                           capture_output=True, text=True, timeout=30)
        self.state = json.loads(self.state_path.read_text())
        self.assertEqual(p.returncode == 0, success, p.stdout + p.stderr)
        return p

    def called(self, verb):
        return any(call[:3] == ['gh', 'release', verb] for call in self.state['calls'])

    def test_new_release(self):
        self.publish()
        self.assertTrue(self.called('create'))
        self.assertTrue(self.called('upload'))
        self.assertFalse(self.state['draft'])

    def test_tagless_draft_recovery(self):
        self.state['exists'] = True
        self.publish()
        self.assertFalse(self.called('create'))
        self.assertTrue(self.called('upload'))
        self.assertFalse(self.state['draft'])

    def test_wrong_draft_target(self):
        self.state.update(exists=True, target='b' * 40)
        self.publish(False)
        self.assertFalse(self.called('upload'))

    def test_conflicting_tag_without_release(self):
        self.state.update(tag=True, tag_sha='b' * 40)
        self.publish(False)
        self.assertFalse(self.called('create'))

    def test_published_rerun_verifies_without_clobber(self):
        self.state.update(exists=True, tag=True, draft=False)
        self.publish()
        self.assertTrue(self.called('download'))
        self.assertFalse(self.called('upload'))
        self.assertFalse(self.called('edit'))

    def test_published_checksum_failure_is_not_success(self):
        self.state.update(exists=True, tag=True, draft=False, corrupt_published=True)
        self.publish(False)
        self.assertFalse(self.called('upload'))
        self.assertFalse(self.called('edit'))

    def test_published_installer_must_match_source(self):
        self.state.update(exists=True, tag=True, draft=False, wrong_installer=True)
        self.publish(False)
        self.assertFalse(self.called('upload'))
        self.assertFalse(self.called('edit'))

    def test_tag_fetch_failure_prevents_creation(self):
        self.state.update(tag=True, fail_fetch=True)
        self.publish(False)
        self.assertFalse(self.called('create'))

    def test_upload_failure_keeps_draft(self):
        self.state['fail_upload'] = True
        self.publish(False)
        self.assertTrue(self.state['draft'])
        self.assertFalse(self.called('edit'))

    def test_superseded_source_skips_publish(self):
        self.state['head'] = 'b' * 40
        self.publish()
        self.assertFalse(self.called('create'))

    def test_superseded_upload_stays_draft(self):
        self.state['supersede_upload'] = True
        self.publish()
        self.assertTrue(self.called('upload'))
        self.assertTrue(self.state['draft'])
        self.assertFalse(self.called('edit'))

    def test_stable_is_explicit_and_latest(self):
        self.env.update(RELEASE_MODE='stable', RELEASE_TAG='v1.0.0')
        self.publish()
        self.assertFalse(self.state['prerelease'])
        edit = next(c for c in self.state['calls'] if c[:3] == ['gh', 'release', 'edit'])
        self.assertIn('--prerelease=false', edit)
        self.assertIn('--latest=true', edit)

    def test_alpha_release_notes_pin_prerelease_install(self):
        self.publish()
        notes = (self.root / 'artifacts/notes.md').read_text()
        self.assertIn('| WHIPCODE_VERSION=v1.0.0-alpha.1 sh', notes)

    def test_stable_release_notes_pin_stable_install(self):
        self.env.update(RELEASE_MODE='stable', RELEASE_TAG='v1.0.0')
        self.publish()
        notes = (self.root / 'artifacts/notes.md').read_text()
        self.assertIn('| WHIPCODE_VERSION=v1.0.0 sh', notes)

    def test_alpha_never_becomes_latest(self):
        self.publish()
        edit = next(c for c in self.state['calls'] if c[:3] == ['gh', 'release', 'edit'])
        self.assertIn('--prerelease', edit)
        self.assertIn('--latest=false', edit)
        self.assertTrue(any(c[:4] == ['gh', 'api', '--method', 'POST'] for c in self.state['calls']))

    def test_disabled_or_unset_switch_never_calls_github(self):
        for value in ['', 'false', '1', 'TRUE']:
            with self.subTest(value=value):
                self.env['WHIP_RELEASE_ENABLED'] = value
                self.publish(False)
                self.assertEqual(self.state['calls'], [])

    def test_baseline_required_and_must_be_sha(self):
        for value in ['', 'main', 'c' * 39]:
            with self.subTest(value=value):
                self.env['WHIP_RELEASE_BASELINE'] = value
                self.publish(False)
                self.assertEqual(self.state['calls'], [])

    def test_predating_baseline_fails(self):
        self.state['pre_baseline'] = True
        self.publish(False)
        self.assertFalse(self.called('create'))

    def test_checkout_must_equal_validated_source(self):
        self.state['checkout'] = 'b' * 40
        self.publish(False)
        self.assertFalse(self.called('create'))

    def test_reject_legacy_or_mismatched_channels(self):
        for mode, tag in [('alpha', 'whipcode-v0.0.1'), ('alpha', 'v0.0.1'),
                          ('alpha', 'v1.0.0'), ('alpha', 'v1.0.0-alpha.0'),
                          ('alpha', 'v1.0.0-alpha.01'), ('alpha', 'v1.0.0-beta.1'),
                          ('stable', 'v1.0.0-alpha.1'), ('stable', 'v2.0.0'),
                          ('other', 'v1.0.0')]:
            with self.subTest(mode=mode, tag=tag):
                self.env.update(RELEASE_MODE=mode, RELEASE_TAG=tag)
                self.publish(False)
                self.assertFalse(self.called('create'))

    def test_missing_or_extra_assets_prevent_publication(self):
        for field in ['missing_asset', 'extra_asset']:
            with self.subTest(field=field):
                self.state[field] = True
                self.publish(False)
                self.assertFalse(self.called('edit'))
                self.state[field] = False

    def test_disabled_workflow_prevents_publication(self):
        self.state['workflow_state'] = 'disabled_manually'
        self.publish(False)
        self.assertFalse(self.called('create'))

    def test_unavailable_workflow_state_fails_closed(self):
        self.state['fail_workflow'] = True
        self.publish(False)
        self.assertFalse(self.called('create'))

    def test_workflow_disabled_before_upload_does_not_clobber(self):
        self.state['disable_create'] = True
        self.publish(False)
        self.assertTrue(self.state['draft'])
        self.assertFalse(self.called('upload'))
        self.assertFalse(self.called('edit'))

    def test_workflow_disabled_during_upload_leaves_draft(self):
        self.state['disable_upload'] = True
        self.publish(False)
        self.assertTrue(self.state['draft'])
        self.assertFalse(self.called('edit'))

    def test_remote_head_failure_does_not_publish(self):
        self.state['fail_head'] = True
        self.publish(False)
        self.assertFalse(self.called('create'))

    def test_tag_lookup_failure_is_not_absence(self):
        self.state['fail_lookup'] = True
        self.publish(False)
        self.assertFalse(self.called('create'))

    def test_tag_creation_failure_keeps_draft(self):
        self.state['fail_tag'] = True
        self.publish(False)
        self.assertTrue(self.state['draft'])
        self.assertFalse(self.called('edit'))

    def test_published_assets_must_be_exact(self):
        self.state.update(exists=True, tag=True, draft=False, extra_asset=True)
        self.publish(False)
        self.assertFalse(self.called('upload'))
        self.assertFalse(self.called('edit'))

    def test_published_channel_must_match(self):
        self.state.update(exists=True, tag=True, draft=False, prerelease=False)
        self.publish(False)
        self.assertFalse(self.called('upload'))

    def test_published_incomplete_or_unsafe_checksums_fail(self):
        for field in ['incomplete_manifest', 'unsafe_manifest']:
            with self.subTest(field=field):
                self.state.update(exists=True, tag=True, draft=False)
                self.state[field] = True
                self.publish(False)
                self.assertFalse(self.called('edit'))
                self.state[field] = False
                shutil.rmtree(self.root / 'artifacts' / 'published', ignore_errors=True)


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.workflows = SCRIPT.parent.parent / '.github' / 'workflows'
        self.release = (self.workflows / 'publish-cli.yml').read_text()
        self.security = (self.workflows / 'security.yml').read_text()
        self.ci = (self.workflows / 'ci.yml').read_text()

    def job(self, workflow, name):
        # Source contracts supplement actionlint's YAML/expression validation.
        match = re.search(r'(?ms)^  ' + re.escape(name) + r':\n(.*?)(?=^  [a-zA-Z_-]+:|\Z)', workflow)
        self.assertIsNotNone(match, name)
        return match.group(1)

    def test_one_shared_validation_definition(self):
        for workflow in [self.ci, self.security]:
            self.assertIn('  workflow_call:\n', workflow)
            self.assertIn('  pull_request:\n    branches: [main]\n', workflow)
            self.assertIn('  push:\n    branches: [main]\n', workflow)
            self.assertNotIn('continue-on-error:', workflow)
        for old in ['ci-whipcode.yml', 'security-whipcode.yml', 'release.yml', 'release-whipcode.yml',
                    'whip--loupe-chat.yml', 'whip--loupe-review.yml']:
            self.assertFalse((self.workflows / old).exists())
        for job in ['lint', 'test', 'build', 'runtime', 'driver', 'sdk', 'distribution', 'desktop', 'mobile']:
            self.assertIn('${{ needs.' + job + '.result }}" = success', self.job(self.ci, 'go'))
        self.assertIn('if: always()', self.job(self.ci, 'go'))
        self.assertIn('npm run test:package', self.job(self.ci, 'sdk'))
        self.assertIn('needs: sdk', self.job(self.ci, 'distribution'))
        self.assertIn('node scripts/pack-web.mjs --release', self.job(self.ci, 'distribution'))

    def test_main_and_baseline_gate_all_publication(self):
        self.assertNotIn('pull_request:', self.release)
        self.assertNotIn('tags:', self.release)
        metadata = self.job(self.release, 'metadata')
        for guard in ["github.repository == 'context-labs/whip'", "github.ref == 'refs/heads/main'",
                      "vars.WHIP_RELEASE_ENABLED == 'true'", 'vars.WHIP_RELEASE_BASELINE',
                      'git merge-base --is-ancestor', 'git rev-parse origin/main']:
            self.assertIn(guard, metadata)
        self.assertIn('uses: ./.github/workflows/ci.yml', self.job(self.release, 'ci'))
        self.assertIn('uses: ./.github/workflows/security.yml', self.job(self.release, 'security'))
        self.assertIn('needs: [metadata, ci, security]', self.job(self.release, 'build'))
        publish = self.job(self.release, 'publish')
        self.assertIn('needs: [metadata, build]', publish)
        self.assertIn("if: vars.WHIP_RELEASE_ENABLED == 'true'", publish)
        self.assertIn('vars.WHIP_RELEASE_BASELINE', publish)
        self.assertIn('ref: ${{ needs.metadata.outputs.source }}', publish)

    def test_stable_requires_dispatch_and_protected_environment(self):
        self.assertIn('  workflow_dispatch:\n', self.release)
        self.assertIn('options: [alpha, stable]', self.release)
        self.assertIn("github.event_name == 'workflow_dispatch' && inputs.channel || 'alpha'", self.release)
        self.assertIn('alpha) tag="v1.0.0-alpha.$RUN_NUMBER"', self.release)
        self.assertIn('stable) tag=v1.0.0', self.release)
        self.assertIn("needs.metadata.outputs.mode == 'stable' && 'whipcode-stable'", self.job(self.release, 'publish'))
        self.assertIn('GH_TOKEN: ${{ github.token }}', self.job(self.release, 'publish'))
        self.assertNotIn('secrets.', self.release)
        self.assertIn('group: whipcode-publishing', self.release)
        self.assertIn('cancel-in-progress: false', self.release)

    def test_security_has_no_release_authority(self):
        self.assertIn('\npermissions:\n  contents: read\n', self.security)
        self.assertNotIn('secrets.', self.security)
        self.assertNotIn('contents: write', self.security)
        self.assertNotIn('pull_request_target:', self.release + self.security)
        self.assertEqual(self.security.count('security-events: write'), 1)
        self.assertIn('category: "/language:go"', self.job(self.security, 'codeql'))
        self.assertIn('go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...', self.security)

    def test_desktop_shares_boundary_and_validation(self):
        desktop = (self.workflows / 'release-desktop.yml').read_text()
        for expected in ["vars.WHIP_RELEASE_ENABLED == 'true'", "vars.WHIP_DESKTOP_RELEASE_ENABLED == 'true'",
                         'vars.WHIP_RELEASE_BASELINE', 'git merge-base --is-ancestor',
                         'git rev-parse origin/main', './.github/workflows/ci.yml',
                         './.github/workflows/security.yml', 'actions/attest@']:
            self.assertIn(expected, desktop)
        publish = (self.workflows / 'desktop-publish.yml').read_text()
        self.assertIn('environment: desktop-${{ inputs.channel }}-${{ inputs.mode }}', publish)
        self.assertIn('git/ref/heads/main', publish)
        self.assertIn('gh attestation verify', publish)
        self.assertIn('main.version=v$RELEASE_VERSION', self.job(desktop, 'linux'))

    def test_each_desktop_public_step_rechecks_current_main_and_workflow(self):
        publish = (self.workflows / 'desktop-publish.yml').read_text()
        blocks = re.findall(r'        run: \|\n((?:          .*\n)+)', publish)
        blocks = [b for b in blocks if 'node apps/desktop/scripts/publish' in b]
        self.assertEqual(len(blocks), 3)
        with tempfile.TemporaryDirectory(prefix='desktop-publish-guards-') as directory:
            root = Path(directory)
            gh = root / 'gh'
            gh.write_text("#!/bin/sh\ncase \"$*\" in *actions/workflows*) printf '%s' \"$WORKFLOW_STATE\";; *git/ref/heads/main*) printf '%s' \"$MAIN_SHA\";; *) exit 1;; esac\n")
            gh.chmod(0o755)
            node = root / 'node'
            node.write_text('#!/bin/sh\ntouch "$WROTE"\n')
            node.chmod(0o755)
            wrote = root / 'wrote'
            for index, block in enumerate(blocks):
                for state, head, allowed in [('active', SOURCE, True),
                                             ('active', 'b' * 40, False),
                                             ('disabled_manually', SOURCE, False),
                                             ('', SOURCE, False)]:
                    with self.subTest(step=index, state=state, head=head):
                        wrote.unlink(missing_ok=True)
                        env = {**os.environ, 'PATH': str(root) + os.pathsep + os.environ['PATH'],
                               'GITHUB_REPOSITORY': 'context-labs/whip', 'SOURCE_SHA': SOURCE,
                               'WORKFLOW_STATE': state, 'MAIN_SHA': head, 'WROTE': str(wrote)}
                        command = '\n'.join(line[10:] for line in block.splitlines())
                        result = subprocess.run(['bash', '-e', '-c', command], env=env,
                                                capture_output=True, text=True, timeout=10)
                        self.assertEqual(result.returncode == 0, allowed, result.stderr)
                        self.assertEqual(wrote.exists(), allowed)


if __name__ == '__main__':
    unittest.main()
