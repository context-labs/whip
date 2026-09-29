import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] };
const profile = { id: 'remote', name: 'Remote', url: 'https://EXAMPLE.test:443/', runtime_id: 'pinned', connect_on_launch: true };
async function setup(run) {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    calls.push(request); return run(request);
  }, { clientID: 'client' });
  return { client, calls };
}

test('saved host declarations retain exact URL/runtime pins and perform no connection or automatic retry', async () => {
  let lost = false;
  const { client, calls } = await setup(request => {
    if (lost) throw new DeliveryError('lost profile publication acknowledgement');
    return { jsonrpc: '2.0', id: request.id, result: { revision: 'a'.repeat(64), profiles: [profile] } };
  });
  assert.deepEqual((await client.hosts.profiles()).profiles, [profile]);
  await client.hosts.setProfiles('a'.repeat(64), [profile]);
  assert.equal(calls.at(-1).params.profiles[0].url, profile.url);
  assert.deepEqual(calls.map(call => call.method), ['host.profiles', 'host.set_profiles']);
  lost = true;
  await assert.rejects(client.hosts.setProfiles('a'.repeat(64), []), DeliveryError);
  assert.equal(calls.length, 3);
});

test('profile bounds and secret-shaped projections fail before entering client state', async () => {
  const { client, calls } = await setup(request => ({ jsonrpc: '2.0', id: request.id, result: { revision: 'a'.repeat(64), profiles: [profile], credential: 'private' } }));
  for (const profiles of [null, Array(17).fill(profile), [{ ...profile, name: '界'.repeat(257) }], [{ ...profile, url: 'https://user:secret@host' }], [{ ...profile, url: 'https://host?token=secret' }], [{ ...profile, url: `https://${'a'.repeat(2048)}` }]]) {
    await assert.rejects(client.hosts.setProfiles('a'.repeat(64), profiles), TypeError);
    assert.equal(calls.length, 0, JSON.stringify(profiles).slice(0, 80));
  }
  assert.equal(calls.length, 0);
  await assert.rejects(client.hosts.profiles(), TypeError);
  assert.equal(calls.length, 1);
});
