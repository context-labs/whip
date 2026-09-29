import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client, DeliveryError, RemoteError } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = (type, match = () => true) => structuredClone(fixtures.find(value => value.valid && value.type === type && match(value.value)).value);
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });

test('creation delivery preserves caller identity and explicit recovery, including deletion', async () => {
  const requests = [];
  let drop = true;
  let deleted = false;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    requests.push(structuredClone(request));
    if (request.method === 'trees.create' && drop) { drop = false; throw new DeliveryError('Acknowledgement lost'); }
    const result = fixture('CreateTreeResult', value => value.deleted === deleted);
    result.creation.id = 'MiXeD:Creation';
    return success(request, result);
  }, { clientID: 'creation-client' });
  const { creation_id, ...params } = fixture('CreateTreeParams', value => value.creation_id === 'MiXeD:Creation');
  await assert.rejects(client.createTree(params, creation_id), DeliveryError);
  assert.equal(requests.length, 1, 'no hidden retry or regenerated identity');
  const accepted = await client.getTreeCreation(creation_id);
  assert.deepEqual(await client.createTree(params, creation_id), accepted);
  assert.deepEqual(requests[0].params, requests[2].params);
  assert.equal(accepted.creation.id, creation_id);
  deleted = true;
  const tombstone = await client.createTree(params, creation_id);
  assert.equal(tombstone.deleted, true);
  assert.equal(tombstone.tree, null);
  assert.equal(tombstone.root, null);
  await assert.rejects(client.createTree(params, ''), TypeError);
  await assert.rejects(client.getTreeCreation('bad id'), TypeError);
  await assert.rejects(client.call('trees.create', params), TypeError);
  assert.equal(requests.length, 4);
});

test('catalog traversal freezes filters and exact revision, including empty final pages', async () => {
  const requests = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    requests.push(structuredClone(request));
    if (request.method === 'trees.catalog') return success(request, { revision: '9007199254740993' });
    const page = fixture('ListTreesResult', value => value.items.length > 0);
    if (request.params.after) { page.items = []; page.next_cursor = null; }
    return success(request, page);
  }, { clientID: 'catalog-client' });
  assert.deepEqual(await client.treeCatalog(), { revision: '9007199254740993' });
  const params = { limit: 1, pinned: false, archived: false };
  const pages = client.treePages(params);
  const first = (await pages.next()).value;
  const next = first.next_cursor;
  first.next_cursor = 'caller-mutated';
  params.pinned = true;
  assert.equal((await pages.next()).value.items.length, 0);
  assert.equal((await pages.next()).done, true);
  assert.equal(requests.length, 3, 'head read and exactly two bounded page calls');
  assert.equal(requests[2].params.after, next);
  assert.equal(requests[2].params.expected_revision, '9007199254740993');
  assert.equal(requests[2].params.pinned, false);
  for (const revision of ['0', '01', '-1', '9223372036854775808', 9007199254740993]) {
    await assert.rejects(client.listTrees({ limit: 1, expected_revision: revision }), TypeError);
  }
  assert.equal(requests.length, 3);
});

test('catalog conflict stops iteration without silently mixing or retrying pages', async () => {
  let calls = 0;
  let mismatch = false;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls++;
    if (request.params.after && !mismatch) return { jsonrpc: '2.0', id: request.id, error: { code: -32009, kind: 'CONFLICT', message: 'Catalog changed' } };
    const page = fixture('ListTreesResult', value => value.items.length > 0);
    if (mismatch) page.revision = '9007199254740994';
    return success(request, page);
  }, { clientID: 'catalog-client' });
  const pages = client.treePages({ limit: 1 });
  assert.equal((await pages.next()).done, false);
  await assert.rejects(pages.next(), error => error instanceof RemoteError && error.kind === 'CONFLICT');
  assert.equal((await pages.next()).done, true);
  assert.equal(calls, 2);
  mismatch = true;
  await assert.rejects(client.treePages({ limit: 1, expected_revision: '9007199254740993' }).next(), TypeError);
  await assert.rejects(client.treePages({ limit: 1 }, { signal: AbortSignal.abort() }).next(), error => error.name === 'AbortError');
  assert.equal(calls, 3);
});
