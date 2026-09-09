import assert from 'node:assert/strict';
import { copyFile, cp, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { sha256 } from './renderer-artifact.mjs';
import { installLocalBuild, main } from './update-local.mjs';

async function fixture(t) {
  const root = await mkdtemp(path.join(tmpdir(), 'whip-local-update-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const app = path.join(root, 'Applications/Whip.app');
  const bundle = path.join(root, 'build/Whip.app');
  const executable = path.join(root, 'bin/whipcode');
  for (const directory of [app, path.join(bundle, 'Contents/Helpers'), path.dirname(executable)]) await mkdir(directory, { recursive: true });
  await writeFile(path.join(app, 'old-app'), 'previous app');
  await writeFile(path.join(bundle, 'Contents/Helpers/whipcode'), 'new signed backend');
  await writeFile(executable, 'previous backend');
  const state = path.join(root, 'sessions.db'); await writeFile(state, 'saved sessions');
  const evidence = { buildId: 'local-test', nativeFiles: { whipcode: { sha256: sha256('new signed backend') } } };
  const events = [];
  const dependencies = {
    verify: async staged => {
      events.push('verify');
      assert.equal(await readFile(path.join(staged, 'Contents/Helpers/whipcode'), 'utf8'), 'new signed backend');
      return evidence;
    },
    quit: async () => { events.push('quit'); assert.equal(await readFile(executable, 'utf8'), 'previous backend'); },
    execute: async (file, args) => {
      if (file === '/usr/bin/ditto') { events.push('copy'); await cp(args[0], args[1], { recursive: true }); return ''; }
      if (args[0] === '_desktop-runtime-sync') {
        events.push('sync');
        assert.equal(file, path.join(app, 'Contents/Helpers/whipcode'));
        assert.deepEqual(args.slice(1), ['--executable', executable, '--expected-sha256', sha256('previous backend'),
          '--sha256', sha256('new signed backend'), '--interrupt']);
        await copyFile(file, executable);
        return JSON.stringify({ state: 'ready', buildId: evidence.buildId });
      }
      if (file === executable) { events.push('status'); return JSON.stringify({ state: 'running', daemon_build: evidence.buildId, build_match: true }); }
      assert.equal(file, '/usr/bin/open'); assert.deepEqual(args, ['-a', app]); events.push('open'); return '';
    },
  };
  return { app, executable, evidence, events, dependencies, state,
    options: { app, bundle, executable, evidence, expected: sha256('previous backend'), env: {} },
    backups: async () => (await readdir(path.dirname(app))).filter(name => name.startsWith('.whip-local-update-')) };
}

test('installs verified app and identical backend, retains previous files, checks readiness before opening', async t => {
  const f = await fixture(t);
  const result = await installLocalBuild(f.options, f.dependencies);
  assert.deepEqual(f.events, ['copy', 'verify', 'quit', 'sync', 'status', 'open']);
  assert.equal(await readFile(f.executable, 'utf8'), 'new signed backend');
  assert.equal(await readFile(path.join(result.backup, 'whipcode.previous'), 'utf8'), 'previous backend');
  assert.equal(await readFile(path.join(result.backup, 'previous.app/old-app'), 'utf8'), 'previous app');
  assert.equal(await readFile(f.state, 'utf8'), 'saved sessions');
});

for (const failure of ['copy', 'signature', 'wrong-build', 'changed-backend', 'quit']) {
  test(`${failure} failure leaves installed app and backend untouched`, async t => {
    const f = await fixture(t);
    if (failure === 'copy') f.dependencies.execute = async () => { throw new Error('copy failed'); };
    if (failure === 'signature') f.dependencies.verify = async () => { throw new Error('signature failed'); };
    if (failure === 'wrong-build') f.dependencies.verify = async () => ({ ...f.evidence, buildId: 'different' });
    if (failure === 'changed-backend') await writeFile(f.executable, 'external update');
    if (failure === 'quit') f.dependencies.quit = async () => { throw new Error('quit cancelled'); };
    await assert.rejects(installLocalBuild(f.options, f.dependencies));
    assert.equal(await readFile(path.join(f.app, 'old-app'), 'utf8'), 'previous app');
    assert.equal(await readFile(f.executable, 'utf8'), failure === 'changed-backend' ? 'external update' : 'previous backend');
    assert.equal(await readFile(f.state, 'utf8'), 'saved sessions');
    assert.deepEqual(await f.backups(), []);
    assert(!f.events.includes('sync') && !f.events.includes('open'));
  });
}

for (const failure of ['handoff', 'post-migration', 'wrong-daemon']) {
  test(`${failure} failure retains recovery files and never reopens or downgrades`, async t => {
    const f = await fixture(t);
    const execute = f.dependencies.execute;
    f.dependencies.execute = async (file, args, options) => {
      if (args[0] === '_desktop-runtime-sync') {
        if (failure === 'handoff') throw new Error('owner still stopping');
        const result = await execute(file, args, options);
        await writeFile(f.state, 'migrated sessions');
        if (failure === 'post-migration') throw new Error('readiness timeout');
        return result;
      }
      if (failure === 'wrong-daemon' && file === f.executable)
        return JSON.stringify({ state: 'running', daemon_build: 'old', build_match: false });
      return execute(file, args, options);
    };
    await assert.rejects(installLocalBuild(f.options, f.dependencies), /recovery files:/);
    const [backup] = await f.backups(); assert(backup);
    assert.equal(await readFile(path.join(path.dirname(f.app), backup, 'previous.app/old-app'), 'utf8'), 'previous app');
    assert.equal(await readFile(f.executable, 'utf8'), failure === 'handoff' ? 'previous backend' : 'new signed backend');
    assert.equal(await readFile(f.state, 'utf8'), failure === 'handoff' ? 'saved sessions' : 'migrated sessions');
    assert(!f.events.includes('open'));
  });
}

test('a failed app replacement restores the prior app and retains recovery files', async t => {
  const f = await fixture(t);
  let staged;
  const verify = f.dependencies.verify;
  f.dependencies.verify = async directory => { staged = directory; return verify(directory); };
  f.dependencies.quit = async () => { await rm(staged, { recursive: true }); };
  await assert.rejects(installLocalBuild(f.options, f.dependencies), /recovery files:/);
  assert.equal(await readFile(path.join(f.app, 'old-app'), 'utf8'), 'previous app');
  assert.equal(await readFile(f.executable, 'utf8'), 'previous backend');
  assert.equal((await f.backups()).length, 1);
  assert(!f.events.includes('sync') && !f.events.includes('open'));
});

test('help works without inspecting or changing the local installation', async () => {
  await main(['--help']);
  await assert.rejects(main(['--unknown-option']), /Unknown option/);
});
