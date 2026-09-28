import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';

const initial = { major: 4, minor: 0, runtime_id: 'runtime', builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });
const created = '2026-09-28T12:00:00Z';
const snapshot = { id: 'snapshot', session_id: 'session', capture_id: 'capture', state: 'succeeded', scope: 'session_working_directory', semantics: 'Tracked files under the session working directory are overlaid.', created_at: created, released_at: null };
const action = { id: 'restore', session_id: 'session', snapshot_id: 'snapshot', kind: 'restore', state: 'uncertain', failure: 'Inspect the workspace.', created_at: created, finished_at: created };

test('workspace lost acknowledgements recover through read-only inspection without replay', async () => {
  const calls = [];
  let state = 'claimed';
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request);
    if (request.method === 'workspace.restore') throw new DeliveryError('acknowledgement lost');
    if (request.method === 'workspace.action') return success(request, { ...action, state, failure: state === 'claimed' ? null : action.failure, finished_at: state === 'claimed' ? null : created });
    if (request.method === 'workspace.snapshot') return success(request, snapshot);
    return success(request, { items: [snapshot] });
  }, { clientID: 'workspace' });
  await assert.rejects(client.restoreWorkspace('session', 'snapshot', 'restore'), DeliveryError);
  assert.equal((await client.getWorkspaceAction('session', 'restore')).state, 'claimed');
  state = 'uncertain';
  assert.equal((await client.getWorkspaceAction('session', 'restore')).state, 'uncertain');
  assert.equal((await client.getWorkspaceSnapshot('session', 'snapshot')).state, 'succeeded');
  assert.deepEqual((await client.listWorkspaceSnapshots({ session_id: 'session', after: 'previous', limit: 1 })).items, [snapshot]);
  assert.deepEqual(calls.map(value => value.method), ['workspace.restore', 'workspace.action', 'workspace.action', 'workspace.snapshot', 'workspace.snapshots']);
  assert.deepEqual(calls[0].params, { session_id: 'session', snapshot_id: 'snapshot', action_id: 'restore' });
});

test('workspace explicit retries keep exact caller IDs and terminal uncertainty is not retried', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request);
    return success(request, { action: { ...action, id: request.params.action_id, kind: request.method.split('.')[1] }, snapshot });
  }, { clientID: 'workspace' });
  for (const method of ['captureWorkspace', 'restoreWorkspace', 'releaseWorkspace']) {
    const id = method + '-stable';
    const first = await client[method]('session', 'snapshot', id);
    assert.equal(first.action.state, 'uncertain');
    const count = calls.length;
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(calls.length, count);
    const retried = await client[method]('session', 'snapshot', id);
    assert.deepEqual(first, retried);
    assert.deepEqual(calls.at(-2).params, calls.at(-1).params);
  }
  assert.equal(calls.length, 6);
});

test('workspace cancellation stops local observation and invalid IDs or pages never reach transport', async () => {
  const calls = [];
  const controller = new AbortController();
  const client = await Client.connect(async (request, expectedRuntimeID, options) => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request);
    assert.equal(expectedRuntimeID, initial.runtime_id);
    return new Promise((resolve, reject) => {
      options.signal.addEventListener('abort', () => reject(new DeliveryError('observer closed')), { once: true });
    });
  }, { clientID: 'workspace' });
  const pending = client.captureWorkspace('session', 'snapshot', 'capture', { signal: controller.signal });
  controller.abort();
  await assert.rejects(pending, DeliveryError);
  await assert.rejects(client.releaseWorkspace('session', 'snapshot', 'release', { signal: controller.signal }), error => error.name === 'AbortError');
  await assert.rejects(client.restoreWorkspace('session', 'snapshot', ''), TypeError);
  await assert.rejects(client.captureWorkspace('session', '../path', 'capture'), TypeError);
  await assert.rejects(client.listWorkspaceSnapshots({ session_id: 'session', limit: 101 }), TypeError);
  await assert.rejects(client.call('workspace.restore', { session_id: 'session', snapshot_id: 'snapshot', action_id: 'restore', path: '/private' }), TypeError);
  assert.deepEqual(calls.map(value => value.method), ['workspace.capture']);
});

test('workspace response validation rejects private identity, wider scope and unknown outcomes', async () => {
  for (const changed of [{ ...snapshot, binding: {} }, { ...snapshot, object_id: 'private' }, { ...snapshot, scope: 'repository' }, { ...snapshot, state: 'failed' }]) {
    const client = await Client.connect(async request => success(request, request.method === 'initialize' ? initial : changed), { clientID: 'workspace' });
    await assert.rejects(client.getWorkspaceSnapshot('session', 'snapshot'), TypeError);
  }
});
