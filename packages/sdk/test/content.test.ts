import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import test from 'node:test';
import type { ContentHandle } from '@whip/protocol';
import { WhipClient } from '../src/client.js';
import { ContentReference } from '../src/content.js';
import { decodeBase64, encodeBase64 } from '../src/util.js';
import { transportFixture } from './transport-fixture.js';

const text = new TextEncoder();
function handle(bytes: Uint8Array, reference = 'reference'): ContentHandle {
  return { reference_id: reference, size: String(bytes.length), digest: createHash('sha256').update(bytes).digest('hex'), media_type: 'text/plain' };
}

test('Unix content is lazy, scoped, chunked, and checked against the full digest', async t => {
  const bytes = text.encode('hello world');
  const content = handle(bytes);
  const fixture = transportFixture({ request(request, connection) {
    assert.equal(request.method, 'content.read');
    const offset = Number(request.params.offset);
    connection.reply(request, { content, data: encodeBase64(bytes.subarray(offset, offset + Number(request.params.limit))) });
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'reader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const reference = client.content(content, { rootId: 'root', agentId: 'child' });
  assert.equal(fixture.current.requests.length, 1);
  assert.throws(() => { (reference.handle as ContentHandle).digest = 'changed'; });
  assert.equal(await reference.readText({ maxBytes: 11 }), 'hello world');
  assert.deepEqual(fixture.current.requests.slice(1).map(request => request.params), [0, 4, 8].map(offset => ({
    root_id: 'root', agent_id: 'child', reference_id: 'reference', offset: String(offset), limit: Math.min(4, 11 - offset),
  })));
});

test('content byte bounds reject before I/O, including decimal sizes beyond Number precision', async t => {
  const fixture = transportFixture();
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'reader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const reference = client.content({ ...handle(text.encode('body')), size: '9007199254740993' }, { rootId: 'root' });
  await assert.rejects(reference.readBytes({ maxBytes: Number.MAX_SAFE_INTEGER }), { kind: 'resource_limit' });
  await assert.rejects(reference.readBytes({ maxBytes: Infinity }), { kind: 'resource_limit' });
  assert.equal(fixture.current.requests.length, 1);
  assert.throws(() => new ContentReference(client, { ...handle(new Uint8Array()), size: '-1' }, { rootId: 'root' }));
});

test('content integrity rejects changed handles, corrupted bytes, and oversized chunks', async t => {
  const bytes = text.encode('body');
  const content = handle(bytes);
  let mode = 'handle';
  const fixture = transportFixture({ request(request, connection) {
    connection.reply(request, {
      content: mode === 'handle' ? { ...content, reference_id: 'different' } : content,
      data: encodeBase64(mode === 'oversized' ? text.encode('too long') : text.encode('evil')),
    });
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'reader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const reference = client.content(content, { rootId: 'root' });
  await assert.rejects(reference.readBytes({ maxBytes: 4 }), { kind: 'invalid_response' });
  mode = 'digest';
  await assert.rejects(reference.readBytes({ maxBytes: 4 }), { kind: 'invalid_response' });
  mode = 'oversized';
  await assert.rejects(reference.readBytes({ maxBytes: 4 }), { kind: 'invalid_response' });
});

test('child grant denial is surfaced without retrying the reference as a root read', async t => {
  const fixture = transportFixture({ request(request, connection) { connection.error(request, 'permission_denied'); } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'reader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  await assert.rejects(client.content(handle(text.encode('secret')), { rootId: 'root', agentId: 'child' }).readBytes({ maxBytes: 6 }), { kind: 'permission_denied' });
  assert.equal(fixture.current.requests.length, 2);
  assert.equal(fixture.current.requests[1]!.params.agent_id, 'child');
});

test('even empty Unix content must pass the root/agent/reference grant check', async t => {
  const fixture = transportFixture({ request(request, connection) { connection.error(request, 'permission_denied'); } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'reader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  await assert.rejects(client.content(handle(new Uint8Array()), { rootId: 'root', agentId: 'child' }).readBytes({ maxBytes: 0 }), { kind: 'permission_denied' });
  assert.equal(fixture.current.requests[1]!.params.limit, 1);
});

test('Unix upload snapshots input, verifies result, and does not grant child authority', async t => {
  const original = text.encode('initial upload');
  const input = original.slice();
  const chunks: Uint8Array[] = [];
  const fixture = transportFixture({ request(request, connection) {
    if (request.method === 'upload.chunk') chunks.push(decodeBase64(request.params.data as string));
    connection.reply(request, request.method === 'upload.finish' ? handle(original) : { accepted: true });
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'uploader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const controller = new AbortController();
  const pending = client.upload(input, { rootId: 'root', mediaType: 'text/plain', source: 'test', signal: controller.signal });
  input.fill(0);
  const reference = await pending;
  assert.deepEqual(Buffer.concat(chunks), Buffer.from(original));
  assert.equal(reference.handle.digest, handle(original).digest);
  assert.deepEqual(reference.scope, { rootId: 'root' });
  assert.doesNotThrow(() => controller.abort());
  assert.equal(fixture.current.requests[1]!.params.expected_digest, handle(original).digest);
  const count = fixture.current.requests.length;
  await assert.rejects(client.upload(input, { rootId: 'root', agentId: 'child' }), { kind: 'invalid_arguments' });
  assert.equal(fixture.current.requests.length, count);
});

test('interrupted upload does not finish or automatically restart on reconnection', async t => {
  const fixture = transportFixture({ request(request, connection) {
    connection.reply(request, { accepted: true });
    if (request.method === 'upload.begin') connection.fail();
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'uploader' });
  t.after(() => client.close());
  await client.connect();
  await assert.rejects(client.upload(text.encode('input'), { rootId: 'root' }));
  await client.connect();
  assert.deepEqual(fixture.connections.flatMap(connection => connection.requests).filter(request => request.method.startsWith('upload.')).map(request => request.method), ['upload.begin']);
});

test('HTTP content preserves scoped URLs and detects interrupted or excessive bodies', async t => {
  const bytes = text.encode('body');
  const content = handle(bytes, 'ref/with?reserved');
  let mode = 'ok';
  const urls: URL[] = [];
  t.mock.method(globalThis, 'fetch', async (url: URL) => {
    urls.push(url);
    if (mode === 'denied') return new Response('', { status: 403 });
    return new Response(mode === 'short' ? 'bo' : mode === 'long' ? 'long body' : 'body');
  });
  const fixture = transportFixture({ kind: 'websocket' });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'reader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const reference = client.content(content, { rootId: 'root/?', agentId: 'child&' });
  assert.equal(await reference.readText({ maxBytes: 4 }), 'body');
  assert.equal(urls[0]!.pathname, '/api/v3/content/ref%2Fwith%3Freserved');
  assert.equal(urls[0]!.searchParams.get('root_id'), 'root/?');
  assert.equal(urls[0]!.searchParams.get('agent_id'), 'child&');
  mode = 'short';
  await assert.rejects(reference.readText({ maxBytes: 4 }), { kind: 'unavailable_capability' });
  mode = 'long';
  await assert.rejects(reference.readText({ maxBytes: 4 }), { kind: 'resource_limit' });
  mode = 'denied';
  await assert.rejects(reference.readText({ maxBytes: 4 }), { kind: 'permission_denied' });
  assert.equal(fixture.current.requests.length, 1);
});

test('HTTP upload verifies SHA header and returned handle, with bounded response bodies', async t => {
  const bytes = text.encode('upload');
  const content = handle(bytes);
  let mode = 'ok';
  t.mock.method(globalThis, 'fetch', async (url: URL, options: RequestInit) => {
    assert.equal(url.pathname, '/api/v3/content/upload');
    assert.equal(url.searchParams.get('root_id'), 'root');
    assert.equal(options.method, 'POST');
    assert.equal(new Headers(options.headers).get('X-Content-SHA256'), content.digest);
    assert.deepEqual(options.body, bytes);
    return new Response(mode === 'long' ? 'x'.repeat(70 << 10) : JSON.stringify(mode === 'digest' ? { ...content, digest: '0'.repeat(64) } : content));
  });
  const fixture = transportFixture({ kind: 'websocket' });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'uploader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  assert.equal((await client.upload(bytes, { rootId: 'root' })).handle.digest, content.digest);
  mode = 'digest';
  await assert.rejects(client.upload(bytes, { rootId: 'root' }), { kind: 'invalid_response' });
  mode = 'long';
  await assert.rejects(client.upload(bytes, { rootId: 'root' }), { kind: 'resource_limit' });
});

test('closing the client aborts HTTP transfers without sending an execution cancellation', async t => {
  let started!: () => void;
  const fetching = new Promise<void>(resolve => { started = resolve; });
  t.mock.method(globalThis, 'fetch', (_url: URL, options: RequestInit) => new Promise<Response>((_resolve, reject) => {
    options.signal?.addEventListener('abort', () => reject(options.signal!.reason), { once: true });
    started();
  }));
  const fixture = transportFixture({ kind: 'websocket' });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'reader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const pending = client.content(handle(text.encode('body')), { rootId: 'root' }).readText({ maxBytes: 4 });
  await fetching;
  client.close();
  await assert.rejects(pending, { kind: 'closed' });
  assert.ok(fixture.current.requests.every(request => request.method === 'initialize'));
});
