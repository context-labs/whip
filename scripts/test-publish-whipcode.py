#!/usr/bin/env python3
"""Test release publication state transitions without writing to GitHub."""
import hashlib
import json
import os
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
        (self.root / 'install-whipcode.sh').write_text('installer')
        self.state = {'exists': False, 'draft': True, 'tag': False, 'tag_sha': SOURCE,
                      'target': SOURCE, 'head': SOURCE, 'calls': [], 'fail_upload': False,
                      'supersede_upload': False}
        self.state_path = self.root / 'state.json'
        self.env = {**os.environ, 'PATH': str(self.tools) + os.pathsep + os.environ['PATH'],
                    'STATE': str(self.state_path), 'SOURCE_SHA': SOURCE,
                    'RELEASE_TAG': 'whipcode-v0.0.1', 'GH_REPO': 'context-labs/whip'}
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
    if a[0]=='ls-remote': done(0 if s['tag'] else 2)
    if a[0]=='fetch': done(1 if s.get('fail_fetch') else 0)
    if a[0]=='rev-parse': done(output=s['tag_sha']+'\n')
if a[0]=='api': done(output=s['head']+'\n')
if a[:2]==['release','view']:
    if not s['exists']: done(1)
    names=['whipcode-linux-x64','whipcode-linux-arm64','whipcode-darwin-x64',
           'whipcode-darwin-arm64','SHA256SUMS','install-whipcode.sh']
    done(output=json.dumps({'isDraft':s['draft'],'targetCommitish':s['target'],
                            'assets':[{'name':n,'size':1} for n in names]}))
if a[:2]==['release','create']:
    s['exists']=True; s['draft']=True; done()
if a[:2]==['release','upload']:
    if s['fail_upload']: done(1)
    if s['supersede_upload']: s['head']='b'*40
    done()
if a[:2]==['release','edit']:
    s['draft']=False; s['tag']=True; done()
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
    (dest/'SHA256SUMS').write_text(sums)
    (dest/'install-whipcode.sh').write_text('different' if s.get('wrong_installer') else 'installer')
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


if __name__ == '__main__':
    unittest.main()
