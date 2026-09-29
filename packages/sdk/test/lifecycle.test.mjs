import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';

const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] };
const status = { runtime_id: 'runtime', process_epoch: 'boot', pid: 123, build: 'fixture', started_at: '2026-09-28T12:00:00Z', web_endpoint: '' };

test('host lifecycle pins explicit stop epoch and does not replay lost acknowledgement', async () => {
  const calls = [];
  let lost = false;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    calls.push(request);
    if (lost) throw new DeliveryError('stop acknowledgement lost');
    return { jsonrpc: '2.0', id: request.id, result: request.method === 'host.status' ? status : { runtime_id: 'runtime', process_epoch: request.params.process_epoch } };
  }, { clientID: 'client' });
  const current = await client.hosts.status();
  await client.hosts.stop(current.process_epoch);
  assert.deepEqual(calls.at(-1).params, { runtime_id: 'runtime', process_epoch: 'boot' });
  lost = true;
  await assert.rejects(client.hosts.stop('boot'), DeliveryError);
  assert.deepEqual(calls.map(call => call.method), ['host.status', 'host.stop', 'host.stop']);
  await assert.rejects(client.hosts.stop(''), TypeError);
  assert.equal(calls.length, 3);
});

test('host status rejects a foreign runtime', async () => {
  const client = await Client.connect(async request => ({ jsonrpc: '2.0', id: request.id, result: request.method === 'initialize' ? initial : { ...status, runtime_id: 'foreign' } }), { clientID: 'client' });
  await assert.rejects(client.hosts.status(), /identity/);
});
