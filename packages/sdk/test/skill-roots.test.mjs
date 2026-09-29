import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';

test('skill publication and selection are explicit shared CAS without grants or mutation retries', async () => {
  const calls = [];
  let lost = false;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] } };
    calls.push(request);
    if (lost) throw new DeliveryError('lost publication acknowledgement');
    return { jsonrpc: '2.0', id: request.id, result: { revision: 'a'.repeat(64), roots: [{ id: 'personal', path: '/skills' }], defaults: [] } };
  }, { clientID: 'client' });
  const current = await client.hosts.skillRoots();
  await client.hosts.publishSkillRoot(current.revision, 'personal', '/skills');
  await client.hosts.setDefaultSkillRoots(current.revision, ['personal']);
  assert.deepEqual(calls.map(call => call.method), ['host.skills.roots', 'host.skills.publish', 'host.skills.set_defaults']);
  assert.deepEqual(calls[1].params, { expected_revision: current.revision, id: 'personal', path: '/skills' });
  assert.deepEqual(calls[2].params, { expected_revision: current.revision, roots: ['personal'] });
  await assert.rejects(client.hosts.setDefaultSkillRoots(current.revision, Array.from({ length: 17 }, (_, i) => `root-${i}`)), TypeError);
  assert.equal(calls.length, 3);
  lost = true;
  await assert.rejects(client.hosts.publishSkillRoot(current.revision, 'other', '/other'), DeliveryError);
  assert.equal(calls.length, 4);
});
