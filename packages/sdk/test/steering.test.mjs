import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError, DurableCommand } from '../dist/index.js';
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });
const receipt = { id: 'promotion', session_id: 'session', input_id: 'input', turn_id: 'turn', created_at: '2026-09-28T00:00:00Z', deleted: true, input: null };
async function fixture(response) {
 const calls = [];
 const client = await Client.connect(async request => {
  if (request.method === 'initialize') return success(request, initial);
  calls.push(structuredClone(request)); return response(request);
 }, { clientID: 'steering' });
 return { client, calls };
}

test('steering promotion recovers exact target receipt after lost acknowledgement without replay', async () => {
 const { client, calls } = await fixture(request => {
  if (request.method === 'inputs.steer') throw new DeliveryError('lost ack');
  return success(request, receipt);
 });
 const command = client.session('session').inputs.promotion('input', 'turn', 'promotion');
 await assert.rejects(command.send(), DeliveryError);
 const recovered = DurableCommand.recover(client, JSON.parse(JSON.stringify(command.record)));
 assert.throws(() => recovered.send(), /explicit check/);
 const found = await recovered.check();
 assert.equal(found.state, 'found'); assert.equal(found.evidence.deleted, true);
 assert.deepEqual(calls.map(call => call.method), ['inputs.steer', 'inputs.steering']);
 assert.deepEqual(command.params, { session_id: 'session', input_id: 'input', turn_id: 'turn', edit_id: 'promotion' });
});

test('steering evidence cannot satisfy another input, owner, or selected target', async () => {
 for (const changed of [{ input_id: 'other' }, { turn_id: 'later_turn' }, { session_id: 'other_owner' }, { id: 'other_edit' }]) {
  const { client, calls } = await fixture(request => success(request, { ...receipt, ...changed }));
  const command = client.session('session').inputs.promotion('input', 'turn', 'promotion');
  await assert.rejects(command.check(), TypeError);
  assert.equal(command.record.accepted, false); assert.equal(calls.length, 1);
 }
});

test('submission preserves complete original steering payload and rejects queue target mixing locally', async () => {
 const { client, calls } = await fixture(() => { throw new DeliveryError('observer disconnected'); });
 const parts = [{ type: 'text', text: 'exact' }, { type: 'content', reference_id: 'context' }];
 const designContext = { context_attachment_id: 'context', elements: [], element_count: 0 };
 const command = client.session('session').submission(parts, 'submit', { delivery: 'steer', targetTurnID: 'turn', designContext });
 parts[0].text = 'changed later';
 assert.equal(command.params.parts[0].text, 'exact');
 assert.equal(command.params.delivery, 'steer'); assert.equal(command.params.target_turn_id, 'turn');
 assert.deepEqual(command.params.design_context, designContext);
 await assert.rejects(command.send(), DeliveryError); assert.equal(calls.length, 1);
 assert.throws(() => client.session('session').submission(parts, 'invalid', { delivery: 'queued', targetTurnID: 'turn' }), TypeError);
 await assert.rejects(client.session('session').submit(parts, 'invalid', { targetTurnID: 'turn' }), TypeError);
 assert.equal(calls.length, 1);
});
