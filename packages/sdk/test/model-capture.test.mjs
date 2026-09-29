import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { test } from 'node:test';
import { Client } from '../dist/index.js';
const hash = value => createHash('sha256').update(value).digest('hex');
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] };
test('model inspection reads only exact owner/attempt and verifies chunked text without private request reconstruction', async () => {
  const data = Buffer.from('base 😃 notice'), split = 7; // deliberately split inside a UTF-8 character
  const pieces = [data.subarray(0, split), data.subarray(split)];
  const refs = pieces.map(piece => ({ id: `model_${hash(piece)}`, session_id: 'child', digest: hash(piece), size: String(piece.length), media_type: 'text/plain', created_at: '2026-09-28T00:00:00Z' }));
  const text = { digest: hash(data), bytes: String(data.length), status: 'available', chunks: refs };
  const empty = { digest: hash(''), bytes: '0', status: 'available', chunks: [] };
  const evidence = { session_id: 'child', attempt_id: 'attempt', turn_id: 'turn', request_digest: 'a'.repeat(64), capture: { request_digest: 'a'.repeat(64), source_digest: 'b'.repeat(64), instructions: text, notices: empty, messages: [], tools_digest: hash(''), tools_count: 0, context_complete: true }, compaction: null };
  let response = evidence, reads = 0;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    reads++;
    if (request.method === 'models.inspection') { assert.deepEqual(request.params, { session_id: 'child', attempt_id: 'attempt' }); return { jsonrpc: '2.0', id: request.id, result: response }; }
    assert.equal(request.method, 'content.read');
    const index = refs.findIndex(ref => ref.id === request.params.reference_id);
    assert.equal(request.params.session_id, 'child');
    return { jsonrpc: '2.0', id: request.id, result: { reference: refs[index], data_base64: pieces[index].toString('base64') } };
  }, { clientID: 'test' });
  const models = client.session('child').models;
  assert.deepEqual(await models.inspection('attempt'), evidence);
  assert.equal(await models.readText(text), data.toString());
  assert.equal(reads, 3);
  await assert.rejects(models.readText(text, { maxBytes: 2 }), /limit/);
  await assert.rejects(models.readText({ ...text, chunks: [{ ...refs[0], session_id: 'other' }, refs[1]] }), /owner/);
  await assert.rejects(models.readText({ ...text, status: 'quota', chunks: [] }), /unavailable/);
  assert.equal(reads, 3, 'bounds, owner and absence rejected before reads');
  await assert.rejects(models.readText({ ...text, digest: 'f'.repeat(64) }), /digest mismatch/);
  for (const patch of [{ session_id: 'other' }, { attempt_id: 'other' }, { request_digest: 'c'.repeat(64) }]) { response = { ...evidence, ...patch }; await assert.rejects(models.inspection('attempt'), /identity mismatch/); }
  response = { ...evidence, capture: null };
  assert.equal((await models.inspection('attempt')).capture, null);
});

test('compaction inspection reads the exact committed summary rather than current selection', async () => {
  const metadata = { history_revision: '0', source: null, id: 'summary', session_id: 'child', turn_id: 'turn', attempt_id: 'attempt', base_id: null, expected_revision: '0', through_sequence: '9007199254740993', pinned_message_ids: [], text_bytes: '5', created_at: '2026-09-28T00:00:00Z' };
  const inspection = { session_id: 'child', attempt_id: 'attempt', turn_id: 'turn', request_digest: 'a'.repeat(64), capture: null, compaction: metadata };
  let body = { metadata, text: 'exact' }, reads = 0;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    reads++; assert.equal(request.method, 'context.compaction');
    assert.deepEqual(request.params, { session_id: 'child', compaction_id: 'summary' });
    return { jsonrpc: '2.0', id: request.id, result: body };
  }, { clientID: 'test' });
  const models = client.session('child').models;
  assert.equal((await models.readCompaction(inspection)).text, 'exact');
  await assert.rejects(models.readCompaction({ ...inspection, attempt_id: 'other' }), /identity mismatch/);
  assert.equal(reads, 1);
  body = { ...body, metadata: { ...metadata, through_sequence: '9007199254740992' } };
  await assert.rejects(models.readCompaction(inspection), /identity or size mismatch/);
});
