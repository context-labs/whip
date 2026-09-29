import assert from 'node:assert/strict';
import { join } from 'node:path';
import { Client, DeliveryError, DurableCommand } from '../../packages/sdk/dist/index.js';

export async function steeringAcceptance(runtime, client, createParams, evidence, { dropAcknowledgement, unixSocket, deadline, until }) {
  await runtime.stop(); await runtime.start('1h');
  const { root } = await client.call('trees.create', { ...createParams, creation_id: 'steering-root' }, deadline());
  const opening = client.session(root.id).submission([{ type: 'text', text: 'opening' }], 'steering-opening');
  await opening.send(deadline());
  const active = await until(() => client.session(root.id).activity(deadline()), value => value.active_turn !== null);
  const queued = client.session(root.id).submission([{ type: 'text', text: 'original queued steering' }], 'steering-queued');
  const input = (await queued.send(deadline())).input;
  let accepted;
  const close = await dropAcknowledgement(join(runtime.directory, 'steering-drop.sock'), runtime.info.socket, '', value => { accepted = value; }, response => response.result?.id === 'steering-promotion');
  let record;
  try {
    const unreliable = await Client.connect(unixSocket(join(runtime.directory, 'steering-drop.sock')), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    const command = unreliable.session(root.id).inputs.promotion(input.id, active.active_turn.id, 'steering-promotion');
    await assert.rejects(command.send(deadline()), DeliveryError); record = JSON.parse(JSON.stringify(command.record));
    assert.equal(accepted.input.steering.consumed, false);
  } finally { await close(); }
  await runtime.stop('SIGKILL'); await runtime.start('0');
  const recovered = DurableCommand.recover(client, record);
  const check = await recovered.check(deadline()); assert.equal(check.state, 'found');
  const done = await queued.wait(deadline());
  assert.equal(done.turn.state, 'succeeded'); assert.notEqual(done.turn.id, active.active_turn.id);
  assert.equal(done.input.steering.turn_id, active.active_turn.id); assert.equal(done.input.steering.consumed, false);
  const page = await client.session(root.id).history.page({ direction: 'backward' }, deadline());
  assert.equal(page.messages.filter(message => message.input_id === input.id).length, 1);
  assert.equal(page.messages.find(message => message.input_id === input.id).opening_input, true);
  await client.session(root.id).delete(deadline());
  const deleted = await recovered.check(deadline()); assert.equal(deleted.state, 'found'); assert.equal(deleted.evidence.deleted, true);
  evidence.push({ steering: { original_input: input.id, accepted_target: active.active_turn.id, recovered_turn: done.turn.id, consumed: false, tombstone: deleted.evidence.deleted } });
}
