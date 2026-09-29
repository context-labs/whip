import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] };

test('published standing text uses exact CAS with no publication or uncertain write replay', async () => {
  const calls = [];
  let value = { published: false, revision: null, text: null };
  let lost = false;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    calls.push(request);
    if (lost) throw new DeliveryError('lost standing acknowledgement');
    return { jsonrpc: '2.0', id: request.id, result: value };
  }, { clientID: 'editor' });
  assert.deepEqual(await client.hosts.standingInstructions(), value);
  value = { published: true, revision: 'a'.repeat(64), text: '# raw\r\n café\n' };
  assert.deepEqual(await client.hosts.standingInstructions(), value);
  await client.hosts.writeStandingInstructions(value.revision, '');
  assert.deepEqual(calls.at(-1).params, { expected_revision: value.revision, text: '' });
  for (const [revision, text] of [['stale', 'text'], [value.revision, 'nul\0'], [value.revision, 'x'.repeat(65537)]]) {
    await assert.rejects(client.hosts.writeStandingInstructions(revision, text), TypeError);
  }
  assert.equal(calls.length, 3);
  lost = true;
  await assert.rejects(client.hosts.writeStandingInstructions(value.revision, 'edit'), DeliveryError);
  assert.equal(calls.length, 4);
  assert.deepEqual(calls.map(call => call.method), ['host.standing.read', 'host.standing.read', 'host.standing.write', 'host.standing.write']);
});
