import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { listSSHProfiles } from '../src/ssh-profiles';

async function fixture(t: test.TestContext, config?: string) {
  const home = await mkdtemp(path.join(tmpdir(), 'whip-ssh-profiles-'));
  t.after(() => rm(home, { recursive: true, force: true }));
  await mkdir(path.join(home, '.ssh'));
  if (config !== undefined) await writeFile(path.join(home, '.ssh/config'), config);
  return home;
}

test('discovers literal aliases and explicit metadata without evaluating Match exec or wildcards', async t => {
  const home = await fixture(t, `
# fixture
Host "gpu-box" secondary !excluded prod-* foo? bar[ab]
  HostName = gpu.internal # comment
  User deploy
  Port 2222
Host GPU-BOX
  HostName ignored.internal
Host *
  User should-not-be-applied-as-an-explicit-hint
Match exec "exit 99"
  HostName ignored
  Include missing-command-output
Host last
  HostName %h.internal
  IdentityFile /private/never-expose-this
`);
  assert.deepEqual(await listSSHProfiles(home), { profiles: [
    { alias: 'gpu-box', hostname: 'gpu.internal', user: 'deploy', port: 2222 },
    { alias: 'secondary', hostname: 'gpu.internal', user: 'deploy', port: 2222 },
    { alias: 'last' },
  ], truncated: false });
});

test('reads quoted Includes and globs in lexical order, with SSH-relative paths', async t => {
  const home = await fixture(t, 'Include "profiles with spaces/*.conf"\nInclude ~/extra.conf\nHost final');
  await mkdir(path.join(home, '.ssh/profiles with spaces'));
  await writeFile(path.join(home, '.ssh/profiles with spaces/b.conf'), 'Host b\nUser sam');
  await writeFile(path.join(home, '.ssh/profiles with spaces/a.conf'), 'Host a\nHostName a.internal');
  await writeFile(path.join(home, 'extra.conf'), 'Host extra');
  assert.deepEqual((await listSSHProfiles(home)).profiles.map(p => p.alias), ['a', 'b', 'extra', 'final']);
});

test('missing config is empty; malformed and cyclic config remain errors', async t => {
  const home = await fixture(t);
  assert.deepEqual(await listSSHProfiles(home), { profiles: [], truncated: false });
  await writeFile(path.join(home, '.ssh/config'), 'Host "unfinished');
  await assert.rejects(listSSHProfiles(home), /unfinished/);
  await writeFile(path.join(home, '.ssh/config'), 'Include config');
  await assert.rejects(listSSHProfiles(home), /cycle/);
});

test('bounds configuration bytes and profiles, explicitly reporting truncation', async t => {
  const home = await fixture(t, Array.from({ length: 300 }, (_, i) => `Host host-${i}`).join('\n'));
  const result = await listSSHProfiles(home);
  assert.equal(result.profiles.length, 256);
  assert.equal(result.truncated, true);
  await writeFile(path.join(home, '.ssh/config'), '#'.repeat((1 << 20) + 1));
  await assert.rejects(listSSHProfiles(home), /1 MiB/);
});

test('retains include context but omits conditional and token-expanded hints', async t => {
  const home = await fixture(t, 'Host work\nInclude metadata\nMatch host work\nUser conditional\nHost other\nPort 99999\nUser ${USER}');
  await writeFile(path.join(home, '.ssh/metadata'), 'User sam\nPort 22');
  assert.deepEqual((await listSSHProfiles(home)).profiles, [{ alias: 'work', user: 'sam', port: 22 }, { alias: 'other' }]);
});

test('bounds Include fan-out and nesting without silently claiming a complete list', async t => {
  const home = await fixture(t, 'Include profiles/*');
  await mkdir(path.join(home, '.ssh/profiles'));
  await Promise.all(Array.from({ length: 70 }, (_, i) => writeFile(path.join(home, `.ssh/profiles/${i}.conf`), `Host host-${i}`)));
  const result = await listSSHProfiles(home);
  assert.equal(result.truncated, true);
  assert.equal(result.profiles.length, 63); // The root config counts toward the 64-file budget.
  await writeFile(path.join(home, '.ssh/config'), 'Include level-0');
  for (let i = 0; i < 10; i++) await writeFile(path.join(home, `.ssh/level-${i}`), `Include level-${i + 1}\nHost level-${i}`);
  assert.equal((await listSSHProfiles(home)).truncated, true);
});
