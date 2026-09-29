import assert from 'node:assert/strict';
import { test } from 'node:test';
import { discoverGateway, browserSocket } from '../dist/browser.js';
import { Client } from '../dist/index.js';

const metadata = { available: false, major: 4, runtime_id: 'runtime', process_epoch: 'epoch', websocket_path: '/api/v4/ws', content_path: '/api/v4/content/', max_content_bytes: 4 << 20 };
function respond(body, headers = {}) { return new Response(body, { headers: { 'content-type': 'application/json', ...headers } }); }

test('discovery is bounded metadata and pins the following real handshake', async t => {
  const savedFetch = globalThis.fetch, savedSocket = globalThis.WebSocket;
  t.after(() => { globalThis.fetch = savedFetch; globalThis.WebSocket = savedSocket; });
  let reads = 0, handshakes = 0;
  globalThis.fetch = async (url, options) => {
    reads++;
    assert.equal(url.href, 'https://example.test/api/v4/web');
    assert.equal(options.redirect, 'error'); assert.equal(options.credentials, 'omit'); assert.equal(options.cache, 'no-store');
    return respond(JSON.stringify(metadata));
  };
  const found = await discoverGateway('wss://example.test/api/v4/ws');
  assert.equal(reads, 1); assert.equal(handshakes, 0); assert.equal(Object.isFrozen(found), true);
  assert.equal(found.available, false); // API availability is independent of packaged UI assets.
  let actual = found.runtime_id;
  class Socket {
    static OPEN = 1; static CLOSING = 2;
    readyState = 0; bufferedAmount = 0;
    constructor() { queueMicrotask(() => { this.readyState = 1; this.onopen?.({}); }); }
    send(raw) {
      const request = JSON.parse(raw); handshakes++;
      assert.equal(request.method, 'initialize'); assert.equal(request.params.expected_runtime_id, found.runtime_id);
      assert.equal(request.params.expected_process_epoch, found.process_epoch); assert.equal(request.params.network_client, true);
      queueMicrotask(() => this.onmessage?.({ data: JSON.stringify({ jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: actual, process_epoch: 'epoch', network_client: true, builtins: [] } }) }));
    }
    close() { this.readyState = 3; }
  }
  globalThis.WebSocket = Socket;
  const transport = browserSocket('https://example.test', { expectedRuntimeID: found.runtime_id, expectedProcessEpoch: found.process_epoch });
  assert.equal((await Client.connect(transport, { clientID: 'browser' })).runtimeID, 'runtime');
  actual = 'changed';
  await assert.rejects(Client.connect(transport, { clientID: 'browser' }), /mismatch/);
  assert.equal(handshakes, 2); // Never retries a changed host or silently rediscovers it.
  await assert.rejects(discoverGateway('https://example.test', { expectedRuntimeID: 'saved-other' }), /different runtime/);
});

test('discovery rejects legacy, redirected and oversized metadata; cancels the reader', async t => {
  const saved = globalThis.fetch; t.after(() => { globalThis.fetch = saved; });
  const bodies = [
    { ...metadata, major: 3 }, { ...metadata, runtime_id: '' }, { ...metadata, process_epoch: '' },
    { ...metadata, websocket_path: 'https://elsewhere.test' }, { ...metadata, content_path: '/api/v3/content/' },
    { ...metadata, max_content_bytes: 8 << 20 }, { ...metadata, credential: 'unexpected' },
  ];
  for (const body of bodies) {
    globalThis.fetch = async () => respond(JSON.stringify(body));
    await assert.rejects(discoverGateway('https://example.test'), TypeError);
  }
  globalThis.fetch = async () => respond(JSON.stringify(metadata), { 'content-type': 'text/html' });
  await assert.rejects(discoverGateway('https://example.test'), /JSON/);
  globalThis.fetch = async () => respond('{}', { 'content-length': '4097' });
  await assert.rejects(discoverGateway('https://example.test'), /4096/);
  globalThis.fetch = async () => respond('{}', { 'content-length': '3' });
  await assert.rejects(discoverGateway('https://example.test'), /length mismatch/);
  let cancelled = false;
  globalThis.fetch = async () => respond(new ReadableStream({ start(controller) { controller.enqueue(new Uint8Array(4097)); }, cancel() { cancelled = true; } }));
  await assert.rejects(discoverGateway('https://example.test'), /4096/); assert.equal(cancelled, true);
  globalThis.fetch = async () => respond(new Uint8Array([0xff]));
  await assert.rejects(discoverGateway('https://example.test'), TypeError);
  globalThis.fetch = async () => new Response('', { status: 403 });
  await assert.rejects(discoverGateway('https://example.test'), /403/);
  let calls = 0;
  globalThis.fetch = async () => { calls++; throw new Error('redirect rejected'); };
  await assert.rejects(discoverGateway('https://example.test'), /redirect rejected/); assert.equal(calls, 1);
  for (const endpoint of ['file:///tmp/runtime.sock', 'https://user:password@example.test', 'https://example.test/api/v3/ws', 'https://example.test/?secret=x']) await assert.rejects(discoverGateway(endpoint), TypeError);
  assert.equal(calls, 1);
  const controller = new AbortController(); const reason = new Error('local observation stopped'); controller.abort(reason);
  await assert.rejects(discoverGateway('https://example.test', { signal: controller.signal }), error => error === reason);
  assert.equal(calls, 1);
});
