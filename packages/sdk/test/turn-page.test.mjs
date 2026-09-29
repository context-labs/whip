import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const page = () => structuredClone(fixtures.find(value => value.type === 'TurnPageResult' && value.valid).value);

test('recent turns are an explicit bounded owner read with exact counters and opaque continuation', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    calls.push(request);
    return { jsonrpc: '2.0', id: request.id, result: request.method === 'initialize'
      ? { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'epoch', network_client: false, builtins: [] }
      : request.params.before ? { items: [], next_cursor: null } : page() };
  }, { clientID: 'test' });
  const session = client.session('session_child');
  const result = await session.turns.page({ session_id: 'foreign', limit: 1 });
  assert.equal(result.items[0].kind, 'host_operation');
  assert.equal(result.items[0].config_revision, '9007199254740993');
  assert.equal(result.items[0].history_revision, '9007199254740993');
  assert.equal(calls.at(-1).params.session_id, 'session_child');
  assert.equal(calls.at(-1).params.limit, 1);
  assert.deepEqual(await session.turns.page({ before: result.next_cursor }), { items: [], next_cursor: null });
  assert.equal(calls.at(-1).params.before, 'turn_direct');
  assert.deepEqual(calls.map(value => value.method), ['initialize', 'sessions.turns', 'sessions.turns']);
});

test('recent turn pages reject foreign, duplicate, overfull and inconsistent continuation evidence', async () => {
  let value;
  const client = await Client.connect(async request => ({ jsonrpc: '2.0', id: request.id, result: request.method === 'initialize'
    ? { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'epoch', network_client: false, builtins: [] } : value }), { clientID: 'test' });
  const session = client.session('session_child');
  value = page(); value.items[0].session_id = 'foreign';
  await assert.rejects(session.turns.page({ limit: 1 }), /scope or identity/);
  value = page(); value.items.push(value.items[0]);
  await assert.rejects(session.turns.page({ limit: 2 }), /scope or identity/);
  value = page(); value.next_cursor = 'unrelated';
  await assert.rejects(session.turns.page({ limit: 1 }), /continuation/);
  value = page();
  await assert.rejects(session.turns.page({ before: 'turn_direct', limit: 1 }), /scope or identity/);
  await assert.rejects(session.turns.page({ limit: 2 }), /continuation/);
  value = page(); value.items.push({ ...value.items[0], id: 'older' }); value.next_cursor = null;
  await assert.rejects(session.turns.page({ limit: 1 }), /continuation/);
});
