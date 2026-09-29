import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError } from '../dist/index.js';
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
test('session binary content upload preserves exact owner, bytes and reference without replay', async () => {
  let calls = 0, corrupt = false, lose = false;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    calls++; assert.equal(request.method, 'content.put');
    if (lose) throw new DeliveryError('lost acknowledgement');
    const params = request.params;
    const bytes = Buffer.from(params.data_base64, 'base64');
    const digest = [...new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))].map(value => value.toString(16).padStart(2, '0')).join('');
    return { jsonrpc: '2.0', id: request.id, result: { session_id: corrupt ? 'other' : params.session_id, id: params.reference_id, media_type: params.media_type, size: String(bytes.length), digest, created_at: new Date().toISOString() } };
  }, { clientID: 'test' });
  const content = client.session('child').content;
  const bytes = new Uint8Array([0, 255, 192, 0, 32]);
  const ref = await content.upload('original', 'application/octet-stream', bytes);
  assert.equal(ref.session_id, 'child'); assert.equal(ref.size, '5');
  corrupt = true; await assert.rejects(content.upload('corrupt', 'application/octet-stream', bytes), /identity or digest/);
  corrupt = false; lose = true; await assert.rejects(content.upload('uncertain', 'application/octet-stream', bytes), DeliveryError);
  assert.equal(calls, 3);
  await assert.rejects(content.upload('oversized', 'application/octet-stream', new Uint8Array((4 << 20) + 1)), /4 MiB/);
  assert.equal(calls, 3);
});

test('scoped reads verify owner, reference, metadata, canonical bytes and digest within the caller bound', async () => {
  const bytes = Uint8Array.from([0, 255, 192, 32]);
  const digest = Buffer.from(await crypto.subtle.digest('SHA-256', bytes)).toString('hex');
  const reference = { session_id: 'child', id: 'file', digest, size: '4', media_type: 'application/octet-stream', created_at: '2026-01-01T00:00:00Z' };
  let result = { reference, data_base64: Buffer.from(bytes).toString('base64') }, reads = 0;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    reads++;
    assert.deepEqual(request.params, { session_id: 'child', reference_id: 'file' });
    return { jsonrpc: '2.0', id: request.id, result };
  }, { clientID: 'reader' });
  const content = client.session('child').content;
  assert.deepEqual(await content.readBytes(reference), bytes);
  assert.deepEqual(await content.read('file'), result);
  await assert.rejects(content.readBytes({ ...reference, session_id: 'parent' }), /another session/);
  await assert.rejects(content.readBytes(reference, { maxBytes: 3 }), /read limit/);
  await assert.rejects(content.readBytes(reference, { maxBytes: Infinity }), /read limit/);
  assert.equal(reads, 2, 'invalid owner or known oversize never sends a read');
  for (const patch of [{ session_id: 'parent' }, { id: 'other' }, { media_type: 'text/plain' }, { digest: '0'.repeat(64) }, { size: '3' }]) {
    result = { reference: { ...reference, ...patch }, data_base64: Buffer.from(bytes).toString('base64') };
    await assert.rejects(content.readBytes(reference), /metadata mismatch/);
  }
  result = { reference: { ...reference, digest: '0'.repeat(64) }, data_base64: Buffer.from(bytes).toString('base64') };
  await assert.rejects(content.read('file'), /digest mismatch/);
  result = { reference, data_base64: Buffer.from(bytes).toString('base64') + '\n' };
  await assert.rejects(content.read('file'), /read limit|encoding/);
  result = { reference: { ...reference, size: String((4 << 20) + 1) }, data_base64: '' };
  await assert.rejects(content.read('file'), /read limit/);
  const beforeAbort = reads;
  await assert.rejects(content.read('file', { signal: AbortSignal.abort() }), /abort/i);
  assert.equal(reads, beforeAbort);
});

test('content metadata inspection reads no body and rejects another owner response', async () => {
  let corrupt = false;
  const reference = { session_id: 'child', id: 'file', digest: '0'.repeat(64), size: '4194304', media_type: 'image/png', created_at: '2026-01-01T00:00:00Z' };
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    assert.equal(request.method, 'content.get');
    assert.deepEqual(request.params, { session_id: 'child', reference_id: 'file' });
    return { jsonrpc: '2.0', id: request.id, result: { ...reference, session_id: corrupt ? 'other' : 'child' } };
  }, { clientID: 'metadata' });
  assert.deepEqual(await client.session('child').content.get('file'), reference);
  corrupt = true;
  await assert.rejects(client.session('child').content.get('file'), /identity mismatch/);
});

test('workspace and run control recovery preserves exact edit IDs and never guesses acceptance', async () => {
  const requests = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    requests.push(structuredClone(request));
    throw new DeliveryError('lost edit acknowledgement');
  }, { clientID: 'controls' });
  for (const [method, params] of [
    ['workspace.set', { id: 'directory', session_id: 'child', expected_revision: '9007199254740993', path: '/workspace' }],
    ['run.configure', { id: 'run', session_id: 'child', expected_revision: '9007199254740993', configuration: { system: '', cache_key: '', max_turns: 0, headless: true } }],
  ]) {
    const handle = client.command(method, params);
    assert.equal(handle.id, params.id);
    const saved = structuredClone(params);
    params.expected_revision = '1';
    await assert.rejects(handle.send(), DeliveryError);
    assert.deepEqual((await handle.check()), { state: 'unavailable' });
    assert.deepEqual(requests.at(-1).params, saved);
    const count = requests.length;
    await assert.rejects(handle.retry(), DeliveryError);
    assert.equal(requests.length, count + 1);
    assert.deepEqual(requests.at(-1).params, saved);
  }
});
