import assert from 'node:assert/strict';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import test, { type TestContext } from 'node:test';
import type { OpenProjectRequest } from '@whip/app/platform';
import { transportFixture } from '../../../packages/sdk/test/transport-fixture';
import { ProjectEditors, projectArguments, projectEnvironment, projectSSHAlias, validateOpenProject, verifyProjectRuntime } from '../src/project-open';

const request: OpenProjectRequest = { app: 'cursor', directory: '/remote/project', connectionId: 'handle', runtimeId: 'runtime' };

test('native project verification rejects a replacement runtime before opening any views or submitting work', async () => {
  const f = transportFixture();
  await verifyProjectRuntime(f.factory, f.info.runtime_id, new AbortController().signal);
  assert.deepEqual(f.current.requests.map(item => item.method), ['initialize']);
  await assert.rejects(verifyProjectRuntime(f.factory, 'replaced-runtime', new AbortController().signal), /host could not be verified/);
  assert.deepEqual(f.current.requests.map(item => item.method), ['initialize']);
  const controller = new AbortController(); controller.abort();
  await assert.rejects(verifyProjectRuntime(f.factory, f.info.runtime_id, controller.signal), /host could not be verified/);
  assert.equal(f.connections.length, 2);
});

test('launcher environment preserves OS authentication without leaking provider keys or remote routing hooks', () => {
  assert.deepEqual(projectEnvironment({ HOME: '/home/sam', USER: 'sam', SSH_AUTH_SOCK: '/agent', PATH: '/untrusted',
    OPENAI_API_KEY: 'secret', NODE_OPTIONS: '--require /inject', ELECTRON_RUN_AS_NODE: '1', VSCODE_IPC_HOOK_CLI: '/remote' }),
  { HOME: '/home/sam', USER: 'sam', SSH_AUTH_SOCK: '/agent', PATH: '/usr/bin:/bin:/usr/sbin:/sbin' });
});

test('accepts fixed applications and bounded literal paths, rejecting command and alias injection', () => {
  const directory = '/space # % ü/$(touch injected) "quote" `literal`';
  assert.equal(validateOpenProject({ ...request, directory, extra: 'ignored' }).directory, directory);
  for (const app of ['shell', '__proto__', 'constructor', null]) assert.throws(() => validateOpenProject({ ...request, app }), /supported/);
  for (const directory of ['relative', '', '/newline\n', '/nul\0', '/x'.repeat(9000)])
    assert.throws(() => validateOpenProject({ ...request, directory }), /directory/);
  for (const sshAlias of ['-oProxyCommand=bad', 'user@host', 'host:22', 'host/path', 'host space', 'host\n', '$(bad)', ''])
    assert.throws(() => validateOpenProject({ ...request, sshAlias }), /alias/);
  assert.equal(validateOpenProject({ ...request, sshAlias: 'gpu-4090-sam' }).sshAlias, 'gpu-4090-sam');
});

test('remote profiles require explicit alias configuration when settings cannot be faithfully reused', () => {
  assert.equal(projectSSHAlias({ kind: 'local' }), undefined);
  assert.equal(projectSSHAlias({ kind: 'ssh', host: 'gpu-4090-sam' }), 'gpu-4090-sam');
  assert.throws(() => projectSSHAlias({ kind: 'local' }, 'remote'), /local/);
  for (const overrides of [{ user: 'sam' }, { port: 2222 }, { identityFile: '/key' }]) {
    const target = { kind: 'ssh' as const, host: 'gpu', ...overrides };
    assert.throws(() => projectSSHAlias(target), /username, port, and identity/);
    assert.equal(projectSSHAlias(target, 'configured-gpu'), 'configured-gpu');
  }
  for (const endpoint of ['http://localhost:8080', 'https://remote.ts.net']) {
    const target = { kind: 'url' as const, endpoint };
    assert.throws(() => projectSSHAlias(target), /server URL/);
    assert.equal(projectSSHAlias(target, 'configured-gpu'), 'configured-gpu');
  }
});

test('editor URLs encode full paths without shell parsing or query/fragment ambiguity', () => {
  const directory = '/space # % ü/$(literal) "quotes"/file?query';
  for (const app of ['cursor', 'vscode'] as const) {
    const local = projectArguments(app, directory);
    assert.equal(local[0], '--folder-uri');
    assert.equal(decodeURIComponent(new URL(local[1]!).pathname), directory);
    assert.equal(new URL(local[1]!).protocol, 'file:');
    const remote = new URL(projectArguments(app, directory, 'gpu-4090-sam')[1]!);
    assert.equal(remote.protocol, 'vscode-remote:');
    assert.equal(remote.host, 'ssh-remote+gpu-4090-sam');
    assert.equal(decodeURIComponent(remote.pathname), directory);
    assert.equal(remote.search + remote.hash, '');
  }
  assert.deepEqual(projectArguments('zed', directory), [directory]);
  const remote = new URL(projectArguments('zed', directory, 'gpu-4090-sam')[0]!);
  assert.equal(remote.protocol, 'ssh:'); assert.equal(remote.host, 'gpu-4090-sam');
  assert.equal(decodeURIComponent(remote.pathname), directory);
  assert.equal(remote.search + remote.hash, '');
});

async function fixture(t: TestContext) {
  const directory = await mkdtemp('/tmp/whip-editors-');
  t.after(() => rm(directory, { recursive: true, force: true }));
  const executable = path.join(directory, 'Cursor.app/Contents/Resources/app/bin/cursor');
  await mkdir(path.dirname(executable), { recursive: true }); await writeFile(executable, '#!/bin/true\n');
  const launches: { file: string; args: string[] }[] = [];
  const finder: string[] = [];
  let error: Error | undefined; let finderError = '';
  const editors = new ProjectEditors(async value => { finder.push(value); return finderError; }, async (file, args) => {
    if (file === '/usr/bin/mdfind') return '';
    if (error) throw error;
    launches.push({ file, args }); return '';
  }, [directory]);
  return { directory, executable, editors, launches, finder,
    failLaunch() { error = new Error('raw launcher output must not enter renderer'); },
    failFinder() { finderError = 'Permission denied'; } };
}

test('discovers fixed installed bundles and opens local paths with literal argument arrays', { skip: process.platform !== 'darwin' }, async t => {
  const f = await fixture(t);
  assert.deepEqual(await f.editors.list(), [
    { id: 'cursor', label: 'Cursor', installed: true }, { id: 'vscode', label: 'VS Code', installed: false },
    { id: 'zed', label: 'Zed', installed: false }, { id: 'finder', label: 'Finder', installed: true },
  ]);
  const directory = path.join(f.directory, 'space # % ü $(literal)'); await mkdir(directory);
  await f.editors.open({ ...request, directory }, { kind: 'local' }, new AbortController().signal);
  assert.deepEqual(f.launches, [{ file: f.executable, args: projectArguments('cursor', directory) }]);
  await f.editors.open({ ...request, app: 'finder', directory }, { kind: 'local' }, new AbortController().signal);
  assert.deepEqual(f.finder, [directory]);
});

test('remote folders are never checked or opened locally and Finder is unavailable for every remote transport', { skip: process.platform !== 'darwin' }, async t => {
  const f = await fixture(t); const signal = new AbortController().signal;
  await f.editors.open(request, { kind: 'ssh', host: 'gpu' }, signal);
  assert.deepEqual(f.launches, [{ file: f.executable, args: projectArguments('cursor', '/remote/project', 'gpu') }]);
  for (const target of [{ kind: 'ssh' as const, host: 'gpu' }, { kind: 'url' as const, endpoint: 'http://localhost:8080' }])
    await assert.rejects(f.editors.open({ ...request, app: 'finder', sshAlias: 'gpu' }, target, signal), /Finder.*This Mac/);
  assert.deepEqual(f.finder, []);
});

test('missing local folders, applications, failed launches, Finder errors, and cancelled operations are actionable', { skip: process.platform !== 'darwin' }, async t => {
  const f = await fixture(t); const signal = new AbortController().signal;
  await assert.rejects(f.editors.open(request, { kind: 'local' }, signal), /directory no longer exists/);
  await assert.rejects(f.editors.open({ ...request, app: 'vscode' }, { kind: 'ssh', host: 'gpu' }, signal), /VS Code is not installed/);
  f.failLaunch();
  await assert.rejects(f.editors.open(request, { kind: 'ssh', host: 'gpu' }, signal), /Cursor did not accept.*Remote SSH/);
  f.failFinder();
  await assert.rejects(f.editors.open({ ...request, app: 'finder', directory: f.directory }, { kind: 'local' }, signal), /Finder.*Permission denied/);
  const controller = new AbortController(); controller.abort(new Error('Cancelled'));
  await assert.rejects(f.editors.open(request, { kind: 'ssh', host: 'gpu' }, controller.signal), /Cancelled/);
  assert.deepEqual(f.launches, []);
});
