import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });
const receipt = { id: 'edit', session_id: 'session', revision: '9007199254740993', deleted: true, session: null };

test('session control retries retain caller identity and do not rebase or replay automatically', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request);
    if (calls.length === 1) throw new DeliveryError('ack lost');
    return success(request, request.method === 'workspace.inspect' ? { session_id: 'session', working_directory: '/workspace ', configuration_revision: receipt.revision } : receipt);
  }, { clientID: 'controls' });
  const params = { id: 'edit', session_id: 'session', expected_revision: '9007199254740992', path: '../workspace ' };
  await assert.rejects(client.setWorkingDirectory(params), DeliveryError);
  assert.equal(calls.length, 1);
  assert.equal((await client.setWorkingDirectory(params)).deleted, true);
  assert.deepEqual(calls[0].params, calls[1].params);
  assert.equal((await client.inspectWorkspace('session')).working_directory, '/workspace ');
  await client.configureRun({ id: 'edit', session_id: 'session', expected_revision: '1', configuration: { system: '', max_turns: 0, headless: true, cache_key: '' } });
  assert.deepEqual(calls.map(call => call.method), ['workspace.set', 'workspace.set', 'workspace.inspect', 'run.configure']);
});

test('session controls validate bounded explicit configuration and forbid hidden authority fields', async () => {
  let calls = 0;
  const client = await Client.connect(async request => { calls++; return success(request, initial); }, { clientID: 'controls' });
  const params = { id: 'edit', session_id: 'session', expected_revision: '1', configuration: { system: '', max_turns: 0, headless: false, cache_key: '' } };
  for (const configuration of [{ ...params.configuration, max_turns: -1 }, { ...params.configuration, max_turns: 1000001 }, { ...params.configuration, cache_key: 'x'.repeat(4097) }, { ...params.configuration, permission_mode: 'automatic' }]) {
    await assert.rejects(client.configureRun({ ...params, configuration }), TypeError);
  }
  await assert.rejects(client.setWorkingDirectory({ id: 'edit', session_id: 'session', expected_revision: '1', path: '/workspace', reset_repl: true }), TypeError);
  assert.equal(calls, 1);
});
