import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const result = fixtures.find(value => value.type === 'RecentTreesResult' && value.valid && value.value.items.length).value;

test('recent trees is a bounded fresh read and does not replace ID-cursor catalog browsing', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] } };
    calls.push(request);
    return { jsonrpc: '2.0', id: request.id, result };
  }, { clientID: 'recent-client' });
  assert.deepEqual(await client.trees.recent(), result);
  assert.deepEqual(await client.trees.recent(1), result);
  assert.deepEqual(calls.map(call => [call.method, call.params]), [['trees.recent', { limit: 50 }], ['trees.recent', { limit: 1 }]]);
  for (const limit of [0, 101, 1.5]) await assert.rejects(client.trees.recent(limit), TypeError);
  await assert.rejects(client.trees.recent(1, { signal: AbortSignal.abort() }), error => error.name === 'AbortError');
  assert.equal(calls.length, 2);
});
