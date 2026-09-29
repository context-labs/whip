import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] };
const defaults = { engine: 'quickjs', effort: 'high', compaction_percent: 0, goal_max_continuations: '9007199254740993', max_attempts: 3 };

test('execution defaults use exact shared CAS and never replay uncertain publication', async () => {
  const calls = [];
  let lost = false;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    calls.push(request);
    if (lost) throw new DeliveryError('lost defaults acknowledgement');
    return { jsonrpc: '2.0', id: request.id, result: { revision: 'a'.repeat(64), ...defaults, preferences: { engine: 'quickjs', compaction_percent: 0, compaction_model: { selection: null, settings: null }, goal_max_continuations: null, max_attempts: 0, import_claude: true, import_codex: true } } };
  }, { clientID: 'client' });
  const read = await client.hosts.executionDefaults();
  assert.equal(read.goal_max_continuations, '9007199254740993');
  await client.hosts.setExecutionDefaults(read.revision, defaults);
  assert.deepEqual(calls.at(-1).params, { expected_revision: read.revision, defaults });
  assert.deepEqual(calls.map(call => call.method), ['host.execution_defaults', 'host.set_execution_defaults']);
  for (const invalid of [{ ...defaults, max_attempts: 0 }, { ...defaults, max_attempts: Number.MAX_SAFE_INTEGER + 1 }, { ...defaults, goal_max_continuations: '-1' }, { ...defaults, compaction_percent: 101 }]) {
    await assert.rejects(client.hosts.setExecutionDefaults(read.revision, invalid), TypeError);
    assert.equal(calls.length, 2);
  }
  lost = true;
  await assert.rejects(client.hosts.setExecutionDefaults(read.revision, { ...defaults, goal_max_continuations: '0', max_attempts: 1 }), DeliveryError);
  assert.equal(calls.length, 3);
});

test('execution preferences preserve explicit zero versus default intent in a single publication', async () => {
  const calls = [];
  const preferences = { engine: 'quickjs', compaction_percent: 0, compaction_model: { selection: null, settings: null }, goal_max_continuations: null, max_attempts: 0, import_claude: true, import_codex: false };
  let lost = false;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    calls.push(structuredClone(request));
    if (lost) throw new DeliveryError('uncertain preferences');
    return { jsonrpc: '2.0', id: request.id, result: { revision: 'a'.repeat(64), ...defaults, preferences: request.params.preferences } };
  }, { clientID: 'preferences' });
  assert.deepEqual((await client.hosts.setExecutionPreferences('a'.repeat(64), preferences)).preferences, preferences);
  const explicit = { ...preferences, goal_max_continuations: '0', max_attempts: Number.MAX_SAFE_INTEGER };
  assert.deepEqual((await client.hosts.setExecutionPreferences('a'.repeat(64), explicit)).preferences, explicit);
  assert.deepEqual(calls.map(call => call.method), ['host.set_execution_preferences', 'host.set_execution_preferences']);
  assert.deepEqual(calls[0].params, { expected_revision: 'a'.repeat(64), preferences });
  await assert.rejects(client.hosts.setExecutionPreferences('a'.repeat(64), { ...preferences, max_attempts: Number.MAX_SAFE_INTEGER + 1 }), TypeError);
  assert.equal(calls.length, 2);
  lost = true;
  await assert.rejects(client.hosts.setExecutionPreferences('a'.repeat(64), preferences), DeliveryError);
  assert.equal(calls.length, 3);
});
