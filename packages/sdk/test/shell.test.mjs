import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';

test('interactive shell preserves exact operation and input counters without replay', async () => {
 const requests = [];
 const client = await Client.connect(async request => {
  if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] } };
  requests.push(request);
  if (request.method === 'shell.input') throw new DeliveryError('lost reply');
  return { jsonrpc: '2.0', id: request.id, result: { interaction: { operation_id: 'operation', started_at: '2026-09-28T00:00:00Z', data_base64: 'cHJvbXB0', from: '9007199254740993', through: '9007199254740999', next_input: '9007199254740993', seconds_left: 10 } } };
 }, { clientID: 'shell-test' });
 const view = await client.shellInteraction('owner');
 assert.equal(view.interaction.next_input, '9007199254740993');
 await assert.rejects(client.shellInput({session_id:'owner', operation_id:'operation', sequence:view.interaction.next_input, data_base64:'a2V5'}), DeliveryError);
 assert.equal(requests.length, 2);
 assert.deepEqual(requests[1].params, {session_id:'owner', operation_id:'operation', sequence:'9007199254740993', data_base64:'a2V5'});
 await assert.rejects(client.shellInteraction('owner', '-1'), TypeError);
 assert.equal(requests.length, 2);
});
