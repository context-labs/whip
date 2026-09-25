#!/usr/bin/env python3
"""Offline publication orchestration and workflow contracts; JS tests own byte integrity."""
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('publish-whipcode.sh').resolve()
SOURCE = 'a' * 40


class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='unified-publish-test-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        (self.root / 'candidate').mkdir()
        (self.root / 'install.sh').write_text('installer')
        (self.root / 'candidate/install.sh').write_text('pinned installer')
        (self.root / 'candidate/latest.sh').write_text('stable installer')
        self.state_file = self.root / 'state.json'
        self.state = {'calls': [], 'tag': False, 'tag_sha': SOURCE, 'workflow': 'active'}
        self.env = {**os.environ, 'PATH': str(self.root) + os.pathsep + os.environ['PATH'],
                    'TEST_STATE': str(self.state_file), 'SOURCE_SHA': SOURCE,
                    'SOURCE_REF': 'refs/heads/development', 'GITHUB_SHA': SOURCE,
                    'GITHUB_EVENT_NAME': 'push', 'GITHUB_REF': 'refs/heads/development',
                    'RELEASE_TAG': 'v1.0.1-alpha.1', 'RELEASE_MODE': 'alpha',
                    'GH_REPO': 'context-labs/whip', 'GITHUB_REPOSITORY': 'context-labs/whip',
                    'WHIP_RELEASE_ENABLED': 'true',
                    'WHIP_RELEASE_BASELINE': 'c' * 40}
        fake = r"""#!/usr/bin/env python3
import json, os, pathlib, sys
p=pathlib.Path(os.environ['TEST_STATE']); s=json.loads(p.read_text())
cmd=pathlib.Path(sys.argv[0]).name; a=sys.argv[1:]
s['calls'].append([cmd,*a])
def done(code=0, output=''):
    p.write_text(json.dumps(s)); sys.stdout.write(output); sys.exit(code)
if cmd=='git':
    if a[0]=='rev-parse': done(output=(s.get('checkout',os.environ['SOURCE_SHA']) if a[1]=='HEAD' else s['tag_sha']))
    if a[0]=='fetch': done(1 if s.get('fetch_fail') else 0)
    if a[0]=='merge-base':
        done(1 if (s.get('off_branch') or s.get('off_' + a[-1].split('/')[-1])
                   if a[-1].startswith('refs/remotes/origin/') else s.get('pre_baseline')) else 0)
    if a[0]=='ls-remote': done(128 if s.get('lookup_fail') else (0 if s['tag'] else 2))
if cmd=='gh':
    if a[:2]==['attestation','verify']:
        expected={'--repo':'context-labs/whip',
                  '--signer-workflow':'context-labs/whip/.github/workflows/publish-cli.yml',
                  '--signer-digest':os.environ['SOURCE_SHA'],
                  '--source-ref':s.get('attested_ref',os.environ['SOURCE_REF']),
                  '--source-digest':s.get('attested_sha',os.environ['SOURCE_SHA'])}
        valid=all(key in a and a[a.index(key)+1]==value for key,value in expected.items())
        done(0 if valid and '--deny-self-hosted-runners' in a and not s.get('missing_attestation') else 1)
    if '/actions/workflows/' in ' '.join(a): done(1 if s.get('workflow_fail') else 0,s['workflow'])
    if '--method' in a:
        if s.get('tag_fail'): done(1)
        s['tag']=True
        if s.get('orphan_after')=='tag': s['off_branch']=True
        if s.get('disable_after')=='tag': s['workflow']='disabled_manually'
        done()
if cmd=='node':
    helper=pathlib.Path(a[0]).name
    phase=helper+':'+(a[1] if helper=='release-candidate.mjs' else os.environ.get('WHIP_DESKTOP_PUBLISH_MODE',''))
    s.setdefault('phases',[]).append(phase)
    if s.get('fail_phase')==phase: done(1)
    if helper=='release-candidate.mjs' and s.get('bad_candidate'): done(1)
    if helper=='release-candidate.mjs':
        for name,expected in [('install.sh','pinned installer'),('latest.sh','stable installer')]:
            if pathlib.Path(a[2],name).read_text()!=expected: done(1)
    if helper=='publish-github.mjs' and os.environ.get('WHIP_DESKTOP_PUBLISH_MODE')=='promote': s['public']=True
    if s.get('disable_after')==phase: s['workflow']='disabled_manually'
    if s.get('orphan_after')==phase: s['off_branch']=True
    done()
done(99)
"""
        for name in ['git', 'gh', 'node']:
            path = self.root / name
            path.write_text(fake)
            path.chmod(0o755)

    def set_release(self, mode):
        ref = 'refs/heads/development' if mode == 'alpha' else 'refs/heads/main'
        self.env.update(RELEASE_MODE=mode, SOURCE_REF=ref, GITHUB_REF=ref,
                        GITHUB_EVENT_NAME='workflow_dispatch',
                        RELEASE_TAG='v1.0.1-alpha.1' if mode == 'alpha' else 'v1.0.1')

    def workflow_step(self, job, step_name, success=True, **overrides):
        # Execute the checked-in workflow shell, not a Python copy of its policy.
        workflow = (SCRIPT.parent.parent / '.github/workflows/publish-cli.yml').read_text()
        job_text = re.search(r'(?ms)^  ' + re.escape(job) + r':\n(.*?)(?=^  [a-zA-Z_-]+:|\Z)', workflow).group(1)
        step = job_text.split('      - name: ' + step_name + '\n', 1)[1].split('      - ', 1)[0]
        command = step.split('        run: |\n', 1)[1]
        command = '\n'.join(line[10:] for line in command.splitlines() if line.startswith('          '))
        output = self.root / 'output'
        output.write_text('')
        env = {**self.env, 'BASE_VERSION': '1.0.1', 'RUN_NUMBER': '42',
               'GITHUB_OUTPUT': str(output), **overrides}
        self.state_file.write_text(json.dumps(self.state))
        result = subprocess.run(['bash', '-euo', 'pipefail', '-c', command], cwd=self.root,
                                env=env, text=True, capture_output=True, timeout=15)
        self.state = json.loads(self.state_file.read_text())
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return dict(line.split('=', 1) for line in output.read_text().splitlines())

    def admit(self, success=True, **overrides):
        return self.workflow_step('metadata', 'Admit one clean branch source and shared release identity',
                                  success, **overrides)

    def verify_provenance(self, success=True):
        self.workflow_step('publish', 'Verify complete candidate provenance from the admitted source ref', success)

    def publish(self, success=True):
        self.state_file.write_text(json.dumps(self.state))
        result = subprocess.run(['bash', str(SCRIPT), 'candidate'], cwd=self.root,
                                env=self.env, text=True, capture_output=True, timeout=15)
        self.state = json.loads(self.state_file.read_text())
        self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)
        return result

    def assert_no_publication(self):
        self.assertFalse(self.state.get('public'))
        self.assertFalse(any('publish-' in p for p in self.state.get('phases', [])))

    def test_closed_event_ref_mode_mapping_at_admission_and_publication(self):
        allowed = {('push', 'refs/heads/development', 'alpha'),
                   ('workflow_dispatch', 'refs/heads/development', 'alpha'),
                   ('workflow_dispatch', 'refs/heads/main', 'stable')}
        initial = dict(self.state)
        for event in ['push', 'workflow_dispatch', 'pull_request', 'pull_request_target', 'schedule']:
            for ref in ['refs/heads/development', 'refs/heads/main', 'refs/heads/feature',
                        'refs/tags/v1.0.1', 'refs/pull/123/merge']:
                for mode in ['alpha', 'stable', '', 'beta']:
                    with self.subTest(event=event, ref=ref, mode=mode):
                        self.state = {**initial, 'calls': []}
                        self.set_release(mode)
                        self.env.update(GITHUB_EVENT_NAME=event, GITHUB_REF=ref)
                        accepted = (event, ref, mode) in allowed
                        outputs = self.admit(accepted)
                        if accepted:
                            version = '1.0.1-alpha.42' if mode == 'alpha' else '1.0.1'
                            self.assertEqual(outputs, dict(tag='v' + version, version=version,
                                mode=mode, channel='beta' if mode == 'alpha' else 'stable',
                                source=SOURCE, source_ref=ref))
                        else:
                            self.assertEqual(outputs, {})
                            self.assertEqual(self.state['calls'], [])
                        self.publish(accepted)
                        if not accepted:
                            self.assert_no_job_tag()
                            self.assertEqual(self.state['calls'], [])

    def test_admission_rejects_invalid_identity_and_configuration(self):
        cases = [('GITHUB_REPOSITORY', 'fork/whip'), ('WHIP_RELEASE_ENABLED', 'false'),
                 ('WHIP_RELEASE_ENABLED', ''), ('BASE_VERSION', ''), ('BASE_VERSION', '0.1.0'),
                 ('BASE_VERSION', '1.01.1'), ('BASE_VERSION', '1.0.1-alpha.1'),
                 ('BASE_VERSION', '1.0.1\ninjected=value'), ('RUN_NUMBER', '0'), ('RUN_NUMBER', '01'),
                 ('RUN_NUMBER', ''), ('SOURCE_SHA', 'a' * 39), ('SOURCE_SHA', 'A' * 40),
                 ('SOURCE_SHA', 'b' * 40), ('GITHUB_SHA', 'b' * 40),
                 ('WHIP_RELEASE_BASELINE', ''), ('WHIP_RELEASE_BASELINE', 'main')]
        for name, value in cases:
            with self.subTest(name=name, value=value):
                self.assertEqual(self.admit(False, **{name: value}), {})
                self.assertEqual(self.state['calls'], [])

    def test_admission_and_publisher_require_the_correct_branch_ancestry(self):
        for mode in ['alpha', 'stable']:
            with self.subTest(mode=mode):
                self.state = {'calls': [], 'tag': False, 'tag_sha': SOURCE, 'workflow': 'active'}
                self.set_release(mode)
                other_branch = 'main' if mode == 'alpha' else 'development'
                self.state['off_' + other_branch] = True
                # Alpha does not need to be merged to main (nor stable back to development).
                self.admit()
                self.publish()
                del self.state['off_' + other_branch]
                for field, value in [('off_branch', True), ('fetch_fail', True),
                                     ('pre_baseline', True), ('checkout', 'b' * 40)]:
                    with self.subTest(field=field):
                        self.state = {'calls': [], 'tag': False, 'workflow': 'active', field: value}
                        self.assertEqual(self.admit(False), {})
                        self.publish(False)
                        self.assert_no_job_tag()

    def test_publisher_requires_full_exact_source_sha_and_canonical_ref(self):
        for name, value in [('SOURCE_SHA', ''), ('SOURCE_SHA', 'a' * 39),
                            ('SOURCE_SHA', 'A' * 40), ('SOURCE_SHA', 'b' * 40),
                            ('GITHUB_SHA', ''), ('GITHUB_SHA', 'b' * 40),
                            ('SOURCE_REF', ''), ('SOURCE_REF', 'development'),
                            ('SOURCE_REF', 'refs/heads/main'), ('SOURCE_REF', 'refs/heads/*'),
                            ('SOURCE_REF', 'refs/tags/v1.0.1'), ('SOURCE_REF', 'refs/heads/development\n')]:
            with self.subTest(name=name, value=value):
                original = self.env[name]
                self.env[name] = value
                self.publish(False)
                self.assertEqual(self.state['calls'], [])
                self.env[name] = original

    def test_provenance_binds_every_asset_to_exact_branch_sha_and_signer(self):
        for mode in ['alpha', 'stable']:
            with self.subTest(mode=mode):
                self.set_release(mode)
                self.state['calls'] = []
                self.verify_provenance()
                verified = [call[3] for call in self.state['calls'] if call[:3] == ['gh', 'attestation', 'verify']]
                self.assertEqual(sorted(verified), ['candidate/install.sh', 'candidate/latest.sh'])
                for field, value in [('missing_attestation', True), ('attested_ref', ''),
                                     ('attested_ref', 'refs/heads/main' if mode == 'alpha' else 'refs/heads/development'),
                                     ('attested_sha', 'b' * 40), ('bad_candidate', True)]:
                    with self.subTest(field=field):
                        self.state[field] = value
                        self.verify_provenance(False)
                        self.assert_no_job_tag()
                        del self.state[field]
                original = self.env['SOURCE_REF']
                for ref in ['', 'refs/heads/*', 'refs/heads/main' if mode == 'alpha' else 'refs/heads/development']:
                    self.env['SOURCE_REF'] = ref
                    self.state['calls'] = []
                    self.verify_provenance(False)
                    self.assertEqual(self.state['calls'], [])
                self.env['SOURCE_REF'] = original
                self.env['SOURCE_SHA'] = 'b' * 40
                self.verify_provenance(False)
                self.env['SOURCE_SHA'] = SOURCE

    def test_one_ordered_lifecycle_feed_last(self):
        self.publish()
        self.assertEqual(self.state['phases'], ['release-candidate.mjs:verify',
            'publish-github.mjs:stage', 'publish.mjs:stage',
            'publish-github.mjs:promote', 'publish.mjs:promote'])
        self.assertTrue(self.state['tag'])
        self.assertTrue(self.state['public'])

    def test_stable_and_next_reviewed_base_use_same_lifecycle(self):
        for mode, tag in [('stable', 'v1.0.1'), ('stable', 'v2.1.0'), ('alpha', 'v2.1.0-alpha.9')]:
            with self.subTest(tag=tag):
                self.set_release(mode)
                self.env['RELEASE_TAG'] = tag
                self.publish()

    def test_global_switch_fails_closed(self):
        for value in ['', 'false', '1', 'TRUE']:
            with self.subTest(value=value):
                self.env['WHIP_RELEASE_ENABLED'] = value
                self.publish(False)
                self.assertEqual(self.state['calls'], [])

    def test_baseline_must_be_full_sha(self):
        for value in ['', 'main', 'a' * 39]:
            with self.subTest(value=value):
                self.env['WHIP_RELEASE_BASELINE'] = value
                self.publish(False)
                self.assertEqual(self.state['calls'], [])

    def test_wrong_repository_rejected(self):
        self.env['GITHUB_REPOSITORY'] = 'fork/whip'
        self.publish(False)
        self.assertEqual(self.state['calls'], [])

    def test_reject_wrong_channel_and_legacy_or_noncanonical_tags(self):
        for mode, tag in [('alpha', 'v1.0.1'), ('stable', 'v1.0.1-alpha.1'),
                          ('alpha', 'v0.0.1-alpha.1'), ('alpha', 'whipcode-v0.0.1'),
                          ('alpha', 'desktop-v1.0.1-alpha.1'), ('alpha', 'v1.0.1-alpha.0'),
                          ('alpha', 'v1.0.1-alpha.01'), ('stable', 'v1.01.0'),
                          ('alpha', 'v1.0.1-beta.1'), ('other', 'v1.0.1')]:
            with self.subTest(mode=mode, tag=tag):
                self.env.update(RELEASE_MODE=mode, RELEASE_TAG=tag)
                self.publish(False)
                self.assertEqual(self.state['calls'], [])

    def test_source_boundary_failures(self):
        for field, value in [('checkout', 'b' * 40), ('pre_baseline', True),
                             ('off_branch', True), ('fetch_fail', True)]:
            with self.subTest(field=field):
                self.state[field] = value
                self.publish(False)
                self.assert_no_publication()
                del self.state[field]

    def test_advancing_branch_does_not_supersede_pinned_source(self):
        # The git fake allows source ancestry but cannot answer current-tip API calls.
        self.publish()
        self.assertFalse(any('git/ref/heads/main' in ' '.join(c) for c in self.state['calls']))
        self.assertIn(['git', 'merge-base', '--is-ancestor', SOURCE, 'refs/remotes/origin/development'], self.state['calls'])

    def test_workflow_disabled_or_unavailable_fails_closed(self):
        for field, value in [('workflow', 'disabled_manually'), ('workflow_fail', True)]:
            with self.subTest(field=field):
                self.state[field] = value
                self.publish(False)
                self.assert_no_job_tag()
                self.state[field] = 'active' if field == 'workflow' else False

    def assert_no_job_tag(self):
        self.assertFalse(self.state['tag'])
        self.assert_no_publication()

    def test_invalid_candidate_before_tag_side_effect(self):
        self.state['bad_candidate'] = True
        self.publish(False)
        self.assert_no_job_tag()

    def test_both_installers_must_match_generated_bytes(self):
        for name, original in [('install.sh', 'pinned installer'), ('latest.sh', 'stable installer')]:
            with self.subTest(name=name):
                (self.root / 'candidate' / name).write_text('wrong')
                self.publish(False)
                self.assert_no_job_tag()
                (self.root / 'candidate' / name).write_text(original)

    def test_orphan_tag_wrong_source_rejected(self):
        self.state.update(tag=True, tag_sha='b' * 40)
        self.publish(False)
        self.assert_no_publication()

    def test_matching_tag_is_not_recreated(self):
        self.state['tag'] = True
        self.publish()
        self.assertFalse(any('--method' in c for c in self.state['calls']))

    def test_tag_lookup_and_creation_errors_fail_closed(self):
        for field in ['lookup_fail', 'tag_fail']:
            with self.subTest(field=field):
                self.state[field] = True
                self.publish(False)
                self.assert_no_publication()
                del self.state[field]

    def test_helper_failures_stop_following_phases(self):
        phases = ['publish-github.mjs:stage', 'publish.mjs:stage', 'publish-github.mjs:promote']
        for phase in phases:
            with self.subTest(phase=phase):
                self.state['phases'] = []
                self.state['fail_phase'] = phase
                self.publish(False)
                self.assertEqual(self.state['phases'][-1], phase)
                self.assertFalse(self.state.get('public'))

    def test_feed_failure_explicit_partial_success(self):
        self.state['fail_phase'] = 'publish.mjs:promote'
        result = self.publish(False)
        self.assertTrue(self.state['public'])
        self.assertIn('GitHub release is published, but Desktop feed promotion failed', result.stderr)

    def test_stop_between_staging_and_publication(self):
        self.state['disable_after'] = 'publish.mjs:stage'
        self.publish(False)
        self.assertFalse(self.state.get('public'))
        self.assertEqual(self.state['phases'][-1], 'publish.mjs:stage')

    def test_stop_after_github_reports_partial_success(self):
        self.state['disable_after'] = 'publish-github.mjs:promote'
        result = self.publish(False)
        self.assertTrue(self.state['public'])
        self.assertIn('feed promotion failed', result.stderr)
        self.assertNotIn('publish.mjs:promote', self.state['phases'])

    def test_source_removal_or_workflow_disable_blocks_every_next_effect(self):
        phases = ['release-candidate.mjs:verify', 'tag', 'publish-github.mjs:stage',
                  'publish.mjs:stage', 'publish-github.mjs:promote']
        for mode in ['alpha', 'stable']:
            self.set_release(mode)
            for condition in ['orphan_after', 'disable_after']:
                for index, phase in enumerate(phases):
                    with self.subTest(mode=mode, condition=condition, phase=phase):
                        self.state = {'calls': [], 'tag': False, 'tag_sha': SOURCE,
                                      'workflow': 'active', condition: phase}
                        result = self.publish(False)
                        self.assertEqual(self.state['phases'], [p for p in phases[:index + 1] if p != 'tag'])
                        self.assertEqual(self.state['tag'], index >= 1)
                        if phase == 'publish-github.mjs:promote':
                            self.assertTrue(self.state['public'])
                            self.assertIn('feed promotion failed', result.stderr)
                        else:
                            self.assertFalse(self.state.get('public'))

    def test_every_publication_phase_refreshes_the_canonical_ref(self):
        for mode in ['alpha', 'stable']:
            with self.subTest(mode=mode):
                self.set_release(mode)
                self.state = {'calls': [], 'tag': False, 'tag_sha': SOURCE, 'workflow': 'active'}
                self.publish()
                ref = self.env['SOURCE_REF']
                remote = ref.replace('refs/heads/', 'refs/remotes/origin/')
                authority = [
                    ['gh', 'api', 'repos/context-labs/whip/actions/workflows/publish-cli.yml', '--jq', '.state'],
                    ['git', 'rev-parse', 'HEAD'],
                    ['git', 'fetch', '--no-tags', 'origin', ref + ':' + remote],
                    ['git', 'merge-base', '--is-ancestor', self.env['WHIP_RELEASE_BASELINE'], SOURCE],
                    ['git', 'merge-base', '--is-ancestor', SOURCE, remote],
                ]
                effects = 0
                for index, call in enumerate(self.state['calls']):
                    if '--method' in call or (call[0] == 'node' and 'publish' in call[1]):
                        self.assertEqual(self.state['calls'][index - len(authority):index], authority)
                        effects += 1
                self.assertEqual(effects, 5)

    def test_exact_candidate_retry_after_partial_publication(self):
        for mode in ['alpha', 'stable']:
            with self.subTest(mode=mode):
                self.set_release(mode)
                self.state = {'calls': [], 'tag': False, 'tag_sha': SOURCE, 'workflow': 'active',
                              'fail_phase': 'publish.mjs:promote'}
                candidate = {p.name: p.read_bytes() for p in (self.root / 'candidate').iterdir()}
                self.publish(False)
                self.assertTrue(self.state['public'])
                del self.state['fail_phase']
                self.state['calls'] = []
                self.publish()
                self.assertFalse(any('--method' in call for call in self.state['calls']))
                self.assertEqual(candidate, {p.name: p.read_bytes() for p in (self.root / 'candidate').iterdir()})
                self.assertEqual(self.state['phases'][-1], 'publish.mjs:promote')


class WorkflowTests(unittest.TestCase):
    def setUp(self):
        self.workflows = SCRIPT.parent.parent / '.github/workflows'
        self.release = (self.workflows / 'publish-cli.yml').read_text()
        self.desktop = (self.workflows / 'desktop-release.yml').read_text()
        self.ci = (self.workflows / 'ci.yml').read_text()
        self.security = (self.workflows / 'security.yml').read_text()

    def job(self, workflow, name):
        match = re.search(r'(?ms)^  ' + re.escape(name) + r':\n(.*?)(?=^  [a-zA-Z_-]+:|\Z)', workflow)
        self.assertIsNotNone(match, name)
        return match.group(1)

    def test_one_workflow_identity_and_version(self):
        self.assertIn('name: releaseWHIP\n', self.release)
        self.assertIn('  push:\n    branches: [development]\n', self.release)
        self.assertIn('  workflow_dispatch:\n', self.release)
        self.assertNotIn('tags:', self.release)
        self.assertEqual(self.release.count('BASE_VERSION: 1.0.1'), 1)
        self.assertIn('version="$BASE_VERSION-alpha.$RUN_NUMBER"', self.release)
        self.assertIn('version="$BASE_VERSION"; channel=stable', self.release)
        for name in ['release-desktop.yml', 'desktop-publish.yml', 'release-whipcode.yml', 'release.yml']:
            self.assertFalse((self.workflows / name).exists())

    def test_admission_pins_canonical_branch_source(self):
        metadata = self.job(self.release, 'metadata')
        for guard in ["github.repository == 'context-labs/whip'", 'push:refs/heads/development:alpha|workflow_dispatch:refs/heads/development:alpha)',
                      'workflow_dispatch:refs/heads/main:stable)',
                      "vars.WHIP_RELEASE_ENABLED == 'true'",
                      'git merge-base --is-ancestor "$WHIP_RELEASE_BASELINE" "$SOURCE_SHA"',
                      'git merge-base --is-ancestor "$SOURCE_SHA" "refs/remotes/origin/$source_branch"', 'ref: ${{ github.sha }}']:
            self.assertIn(guard, metadata)
        self.assertNotIn('git rev-parse origin/main', self.release)
        self.assertIn('cancel-in-progress: false', self.release)
        self.assertIn('# ponytail:', self.release)
        self.assertIn("group: whipcode-publishing-${{ github.event_name == 'workflow_dispatch' && inputs.channel || 'alpha' }}", self.release)
        self.assertIn("RELEASE_MODE: ${{ github.event_name == 'push' && 'alpha' || inputs.channel }}", metadata)
        self.assertIn('source_ref: ${{ steps.version.outputs.source_ref }}', metadata)
        self.assertIn('SOURCE_REF: ${{ needs.metadata.outputs.source_ref }}', self.job(self.release, 'publish'))

    def test_complete_graph_is_mandatory(self):
        self.assertIn('needs: [metadata, ci, security]', self.job(self.release, 'build'))
        self.assertIn('needs: [metadata, ci, security]', self.job(self.release, 'desktop'))
        self.assertIn('needs: [metadata, build]', self.job(self.release, 'linux'))
        self.assertIn('needs: [metadata, build, desktop, linux]', self.job(self.release, 'candidate'))
        self.assertIn('needs: [metadata, candidate]', self.job(self.release, 'publish'))
        self.assertNotIn('continue-on-error:', self.release + self.desktop)
        self.assertIn("if: vars.WHIP_RELEASE_ENABLED == 'true'\n", self.job(self.release, 'publish'))
        self.assertIn("if: vars.WHIP_RELEASE_ENABLED == 'true'\n", self.job(self.desktop, 'package'))
        self.assertEqual(self.release.count('uses: ./.github/workflows/ci.yml'), 1)
        self.assertEqual(self.release.count('uses: ./.github/workflows/security.yml'), 1)

    def test_single_publish_approval_and_scoped_secrets(self):
        publish = self.job(self.release, 'publish')
        self.assertIn("'desktop-stable-stage' || 'desktop-beta-stage'", publish)
        self.assertIn('bash scripts/publish-whipcode.sh candidate', publish)
        self.assertIn('GH_TOKEN: ${{ github.token }}', publish)
        self.assertEqual(self.release.count('contents: write'), 1)
        self.assertEqual(self.release.count('secrets: inherit'), 1)
        self.assertIn('secrets: inherit', self.job(self.release, 'desktop'))
        for name in ['ci', 'security', 'build', 'linux', 'candidate', 'publish']:
            self.assertNotIn('secrets: inherit', self.job(self.release, name))
        signing_env = self.desktop.split('    steps:')[0]
        self.assertNotIn('secrets.', signing_env)
        self.assertEqual(self.desktop.count('secrets.'), 3)
        signing_step = self.desktop.split('      - name: Import temporary signing credentials')[1].split('      - name:')[0]
        self.assertEqual(signing_step.count('secrets.'), 3)
        self.assertNotIn('pull_request:', self.release)
        self.assertNotIn('desktop-stable-promote', self.release)
        self.assertNotIn('GH_TOKEN:', publish.split('    steps:')[0])
        self.assertEqual(publish.count('GH_TOKEN: ${{ github.token }}'), 2)
        checkout = publish.split('uses: actions/checkout@')[1].split('      - ')[0]
        self.assertIn('persist-credentials: false', checkout)
        self.assertIn('environment: desktop-signing', self.desktop)
        self.assertIn('ref: ${{ inputs.source }}', self.desktop)

    def test_exact_standalone_linux_reused_without_compilation(self):
        linux = self.job(self.release, 'linux')
        self.assertIn('name: whipcode-bin-linux-amd64', linux)
        self.assertIn('linux-smoke.mjs linux/whipcode-linux-x64', linux)
        self.assertNotIn('go build', linux)
        self.assertIn('path: linux/linux-runtime.json', linux)
        self.assertNotIn('whipcode-linux-x64', self.job(self.release, 'candidate'))

    def test_candidate_attestation_matches_admitted_source_workflow(self):
        candidate = self.job(self.release, 'candidate')
        self.assertIn('node scripts/build-installers.mjs "$RELEASE_TAG" candidate', candidate)
        self.assertLess(candidate.index('node scripts/build-installers.mjs'), candidate.index('release-candidate.mjs assemble candidate'))
        self.assertIn('release-candidate.mjs assemble candidate', candidate)
        self.assertIn('run: npm ci', candidate)
        self.assertIn("ELECTRON_SKIP_BINARY_DOWNLOAD: '1'", candidate)
        self.assertLess(candidate.index('run: npm ci'), candidate.index('release-candidate.mjs assemble candidate'))
        self.assertIn('subject-path: candidate/*', candidate)
        publish = self.job(self.release, 'publish')
        self.assertIn('--signer-workflow context-labs/whip/.github/workflows/publish-cli.yml', publish)
        self.assertIn('--source-ref "$SOURCE_REF" --source-digest "$SOURCE_SHA"', publish)
        self.assertIn('release-candidate.mjs verify candidate', publish)
        self.assertIn('--deny-self-hosted-runners', publish)

    def test_candidate_run_steps_install_dependencies_before_node(self):
        candidate = self.job(self.release, 'candidate')
        # Exercise run-step ordering in an empty workspace; the fake node fails
        # unless npm ci actually ran with Electron downloads disabled first.
        with tempfile.TemporaryDirectory(prefix='candidate-workspace-') as directory:
            root = Path(directory)
            (root / 'candidate').mkdir()
            (root / 'install.sh').write_text('installer')
            (root / 'npm').write_text('#!/bin/sh\n[ "$1" = ci ] && [ "$ELECTRON_SKIP_BINARY_DOWNLOAD" = 1 ] || exit 1\ntouch dependencies-ready\n')
            (root / 'node').write_text('#!/bin/sh\ntest -f dependencies-ready || exit 1\ncase "$1" in scripts/build-installers.mjs) printf pinned > candidate/install.sh; printf stable > candidate/latest.sh;; *) test -s candidate/install.sh && test -s candidate/latest.sh || exit 1; touch assembled;; esac\n')
            for executable in ['npm', 'node']:
                (root / executable).chmod(0o755)
            env = {**os.environ, 'PATH': str(root) + os.pathsep + os.environ['PATH'], 'RELEASE_TAG': 'v1.0.1-alpha.1'}
            executed = 0
            for step in candidate.split('      - ')[1:]:
                match = re.search(r'^        run: (.*)$', step, re.M)
                if not match:
                    continue
                command = match.group(1)
                if command == '|':
                    command = '\n'.join(line[10:] for line in step[match.end():].splitlines()
                                        if line.startswith('          '))
                step_env = dict(env)
                if "ELECTRON_SKIP_BINARY_DOWNLOAD: '1'" in step:
                    step_env['ELECTRON_SKIP_BINARY_DOWNLOAD'] = '1'
                result = subprocess.run(['bash', '-e', '-c', command], cwd=root, env=step_env,
                                        capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                executed += 1
            self.assertEqual(executed, 2)
            self.assertTrue((root / 'assembled').exists())

    def test_distribution_installer_dependencies_precede_execution(self):
        distribution = self.job(self.ci, 'distribution')
        self.assertLess(distribution.index('uses: actions/setup-node@'), distribution.index('run: npm ci'))
        self.assertEqual(distribution.count('run: npm ci'), 1)
        for command in ['python3 scripts/test-install-whipcode.py', 'python3 scripts/test-publish-whipcode.py',
                        'node --test scripts/build-installers.test.mjs']:
            self.assertLess(distribution.index('run: npm ci'), distribution.index(command))

    def test_all_checkouts_explicit_immutable_source(self):
        for text in [self.release, self.desktop, self.ci, self.security,
                     (self.workflows / 'desktop-check.yml').read_text(),
                     (self.workflows / 'mobile.yml').read_text()]:
            for block in text.split('uses: actions/checkout@')[1:]:
                checkout = block.split('      - ')[0]
                self.assertRegex(checkout, r'ref: \$\{\{ (github\.sha|needs\.metadata\.outputs\.source|inputs\.source) \}\}')
        self.assertIn('SOURCE_SHA: ${{ github.sha }}', self.job(self.release, 'metadata'))
        self.assertIn('source: ${{ needs.metadata.outputs.source }}', self.job(self.release, 'desktop'))

    def test_shared_gates_still_fail_closed(self):
        for workflow in [self.ci, self.security]:
            self.assertIn('  workflow_call:\n', workflow)
            self.assertIn('  pull_request:\n    branches: [main, development]\n', workflow)
            self.assertIn('  push:\n    branches: [main, development]\n', workflow)
            self.assertNotIn('continue-on-error:', workflow)
        for name in ['lint', 'test', 'build', 'runtime', 'driver', 'sdk', 'distribution', 'desktop', 'mobile']:
            self.assertIn('${{ needs.' + name + '.result }}" = success', self.job(self.ci, 'go'))
        self.assertIn('if: always()', self.job(self.ci, 'go'))
        self.assertNotIn('contents: write', self.security)
        self.assertNotIn('secrets.', self.security)
        self.assertNotIn('pull_request_target:', self.release + self.security)


if __name__ == '__main__':
    unittest.main()
