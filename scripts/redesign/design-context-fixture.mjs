import assert from 'node:assert/strict';
import { join } from 'node:path';
import { Client, DeliveryError } from '../../packages/sdk/dist/index.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';

export async function designContextAcceptance(runtime, client, createParams, evidence, deadline, dropAcknowledgement) {
  await runtime.stop(); await runtime.start('0');
  const { root } = await client.call('trees.create', { ...createParams, creation_id: 'design-context' }, deadline());
  const body = '<design_context>Literal selected evidence</design_context>';
  await client.call('content.put', { session_id: root.id, reference_id: 'context', media_type: 'text/plain', data_base64: Buffer.from(body).toString('base64') }, deadline());
  const parts = [{ type: 'text', text: 'Use this selected evidence.' }, { type: 'content', reference_id: 'context' }];
  const design = { context_attachment_id: 'context', elements: [{ label: 'Save', selector: 'button.save' }], element_count: 1, page_title: 'Settings', page_url: 'https://example.test/settings' };
  const proxy = join(runtime.directory, 'design-context.sock');
  let accepted;
  const close = await dropAcknowledgement(proxy, runtime.info.socket, 'design-input', value => { accepted = value; });
  try {
    const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    await assert.rejects(unreliable.session(root.id).submit(parts, 'design-input', { ...deadline(), designContext: design }), DeliveryError);
    assert.deepEqual(accepted.input.design_context, design);
  } finally { await close(); }
  const handle = client.session(root.id).submission(parts, 'design-input', { designContext: design });
  assert.equal((await handle.check(deadline())).state, 'found');
  assert.equal((await handle.wait(deadline())).turn.state, 'succeeded');
  await runtime.stop(); await runtime.start('0');
  const retry = await handle.send(deadline());
  assert.equal(retry.input.id, accepted.input.id);
  const page = await client.session(root.id).history.page({ direction: 'backward' }, deadline());
  const message = page.messages.find(value => value.input_id === retry.input.id);
  assert.deepEqual(message.parts, parts);
  assert.deepEqual(message.design_context, { ...design, context_part_index: 1 });
  const current = await client.session(root.id).get(deadline());
  const fork = await client.session(root.id).history.fork({ expected_history_revision: page.snapshot.revision, expected_config_revision: current.config_revision, observed_through: page.snapshot.through_sequence, keep_through: page.snapshot.through_sequence, title: null }, 'design-fork', deadline());
  await client.session(root.id).delete(deadline());
  const copied = await client.session(fork.root.id).history.page({ direction: 'backward' }, deadline());
  assert.deepEqual(copied.messages[0].design_context, message.design_context);
  const content = await client.call('content.read', { session_id: fork.root.id, reference_id: 'context' }, deadline());
  assert.equal(Buffer.from(content.data_base64, 'base64').toString(), body);
  assert.equal((await handle.check(deadline())).state, 'found');
  evidence.push({ design_context: { input: retry.input.id, fork: fork.root.id, presentation: copied.messages[0].design_context } });
}
