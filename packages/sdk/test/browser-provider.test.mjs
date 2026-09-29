import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { BrowserProviderClient, browserProviderFramed, DeliveryError, RemoteError } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const options = { expectedRuntimeID: 'runtime', expectedProcessEpoch: 'boot_test' };
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const response = (id, result) => JSON.stringify({ jsonrpc: '2.0', id, result });
const answer = (handlers, request) => handlers.message(response(request.id, request.method === 'initialize' ? initial : request.method === 'browser.provider.bind' ? fixture('BrowserProviderBindResult') : { accepted: true }));
async function setup(respond = answer, extra = {}) {
  let handlers, opens = 0;
  const connection = { kind: 'unix', bufferedAmount: 0, sent: [], closes: 0,
    send(raw) { const request = JSON.parse(raw); this.sent.push(request); respond(handlers, request, connection); },
    close() { this.closes++; assert.equal(this.closes, 1); handlers.close(new Error('disposed')); },
  };
  const transport = await browserProviderFramed(async h => { handlers = h; opens++; return connection; }, { ...options, ...extra });
  return { transport, connection, handlers, opens };
}
const clientFor = transport => BrowserProviderClient.connect(transport, options);

test('confined provider verifies exact identity once and multiplexes commands without rounding', async () => {
  const { transport, connection, handlers, opens } = await setup();
  const client = await clientFor(transport);
  const bound = await client.bind(fixture('BrowserProviderBindParams'));
  assert.equal(bound.provider_epoch, 'provider_epoch');
  handlers.message(JSON.stringify(fixture('BrowserEvent')));
  const events = client.events(), event = (await events.next()).value;
  assert.equal(event.command.deadline_millis, '9007199254740993');
  assert.equal(event.command.scope.control_lineage, 'lineage');
  await client.event(fixture('BrowserProviderEventParams'));
  assert.equal(connection.sent[2].params.sequence, '9007199254740993');
  assert.deepEqual(connection.sent[0].params, { major: 4, expected_runtime_id: 'runtime', expected_process_epoch: 'boot_test' });
  assert.equal(opens, 1);
  await events.return(); assert.equal(connection.closes, 1);
  await assert.rejects(client.bind(fixture('BrowserProviderBindParams')), /closed/);
});

test('native provider rejects wrong host, restarted process and network classification before binding', async () => {
  for (const initialBad of [{ ...initial, runtime_id: 'other' }, { ...initial, process_epoch: 'restarted' }, { ...initial, network_client: true }]) {
    const { transport, connection } = await setup((h, q) => h.message(response(q.id, initialBad)));
    await assert.rejects(clientFor(transport), /mismatch/);
    assert.equal(connection.sent.length, 1); assert.equal(connection.closes, 1);
  }
  let closes = 0;
  await assert.rejects(browserProviderFramed(async () => ({ kind: 'websocket', bufferedAmount: 0, send() { assert.fail(); }, close() { closes++; } }), options), /confined Unix/);
  assert.equal(closes, 1);
});

test('screenshot upload binds every chunk to the same pending command and acknowledges exactly once', async () => {
  const { transport, connection } = await setup(); const client = await clientFor(transport);
  const result = fixture('BrowserCommandResultParams'); delete result.screenshot;
  const bytes = new Uint8Array((64 << 10) + 3); bytes.fill(23);
  const promise = client.screenshotResult(result, bytes); bytes.fill(99);
  assert.deepEqual(await promise, { accepted: true });
  const chunks = connection.sent.filter(q => q.method === 'browser.screenshot.chunk');
  assert.deepEqual(chunks.map(q => q.params.offset), ['0', '65536']);
  assert.deepEqual(chunks.map(q => atob(q.params.data_base64).length), [65536, 3]);
  assert.equal(atob(chunks[0].params.data_base64).charCodeAt(0), 23);
  for (const chunk of chunks) for (const field of ['command_id', 'root_id', 'provider_epoch', 'attachment_generation']) assert.equal(chunk.params[field], result[field]);
  const settled = connection.sent.at(-1);
  assert.equal(settled.method, 'browser.command.result'); assert.equal(settled.params.screenshot.size, '65539');
  assert.equal(settled.params.screenshot.digest, Buffer.from(await crypto.subtle.digest('SHA-256', new Uint8Array(65539).fill(23))).toString('hex'));
  const count = connection.sent.length;
  await assert.rejects(client.screenshotResult(result, new Uint8Array((4 << 20) + 1)), RangeError);
  await assert.rejects(client.screenshotResult({ ...result, error: { kind: 'outcome_unknown', message: '' } }, new Uint8Array(1)), TypeError);
  assert.equal(connection.sent.length, count); await client.close();
});

test('lost screenshot acknowledgement closes the peer without repeating upload or settlement', async () => {
  const { transport, connection } = await setup((h, q) => q.method === 'browser.screenshot.chunk' ? h.close(new Error('lost ack')) : answer(h, q));
  const client = await clientFor(transport), result = fixture('BrowserCommandResultParams'); delete result.screenshot;
  await assert.rejects(client.screenshotResult(result, new Uint8Array(65537)), DeliveryError);
  assert.deepEqual(connection.sent.map(q => q.method), ['initialize', 'browser.screenshot.chunk']);
  assert.equal(connection.closes, 1);
});

test('stale scope events remain a visible local error without revoking sibling controls', async () => {
  const { transport, connection } = await setup((h, q) => q.method === 'browser.provider.event' ? h.message(JSON.stringify({ jsonrpc: '2.0', id: q.id, error: { kind: 'BROWSER_EVENT_STALE', code: -32009, message: 'retired' } })) : answer(h, q));
  const client = await clientFor(transport);
  await assert.rejects(client.event(fixture('BrowserProviderEventParams')), error => error instanceof RemoteError && error.kind === 'BROWSER_EVENT_STALE');
  assert.equal(connection.closes, 0);
  await client.bind(fixture('BrowserProviderBindParams')); await client.close();
});

test('event count, aggregate bytes, frame size and shape are enforced on native peers', async () => {
  for (const mode of ['count', 'bytes', 'frame', 'shape', 'foreign']) {
    const { transport, handlers, connection } = await setup(); await clientFor(transport);
    const frame = (mode === 'bytes' ? ' '.repeat(5 << 20) : '') + JSON.stringify(fixture('BrowserEvent'));
    if (mode === 'frame') handlers.message(' '.repeat((8 << 20) + 1));
    else if (mode === 'shape') { const bad = fixture('BrowserEvent'); bad.command = null; handlers.message(JSON.stringify(bad)); }
    else if (mode === 'foreign') handlers.message(response('invented', { accepted: true }));
    else for (let index = 0; index < (mode === 'count' ? 33 : 2); index++) handlers.message(frame);
    await assert.rejects(transport.events[Symbol.asyncIterator]().next());
    assert.equal(connection.closes, 1);
  }
});

test('pending requests, queued writes and event consumers remain bounded; abort joins all waiters', async () => {
  let respond = answer;
  const { transport, connection } = await setup((...args) => respond(...args)); await clientFor(transport);
  respond = () => {};
  const pending = [];
  for (let index = 0; index < 32; index++) pending.push(transport.request({ jsonrpc: '2.0', id: String(index), method: 'browser.provider.event', params: {} }, {}).catch(error => error));
  await assert.rejects(transport.request({ jsonrpc: '2.0', id: 'extra', method: 'browser.provider.event', params: {} }, {}), /capacity/);
  const events = transport.events[Symbol.asyncIterator](), next = events.next();
  await assert.rejects(events.next(), /Concurrent/);
  await events.return(); assert.equal((await next).done, true);
  assert.ok((await Promise.all(pending)).every(value => value instanceof DeliveryError)); assert.equal(connection.closes, 1);
  const full = await setup(); await clientFor(full.transport); full.connection.bufferedAmount = 8 << 20;
  await assert.rejects(full.transport.request({ jsonrpc: '2.0', id: 'full', method: 'browser.provider.event', params: {} }, {}), /queue limit/);
  assert.equal(full.connection.sent.length, 1); await full.transport.close();
});

test('lifetime cancellation also retires a connector that opens late and never reconnects', async () => {
  const controller = new AbortController(); let finish, opens = 0, closes = 0;
  const pending = browserProviderFramed(() => { opens++; return new Promise(resolve => { finish = resolve; }); }, { ...options, signal: controller.signal });
  const reason = new Error('host retired'); controller.abort(reason); await assert.rejects(pending, error => error === reason);
  finish({ kind: 'unix', bufferedAmount: 0, send() { assert.fail(); }, close() { closes++; } });
  await new Promise(resolve => setImmediate(resolve)); assert.equal(closes, 1); assert.equal(opens, 1);
  const active = new AbortController(), peer = await setup(answer, { signal: active.signal }); await clientFor(peer.transport);
  active.abort(reason); await assert.rejects(peer.transport.events[Symbol.asyncIterator]().next(), error => error === reason);
  assert.equal(peer.connection.closes, 1);
});

test('successful native connection cancels its establishment deadline without losing lifetime cancellation', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const controller = new AbortController();
  const peer = await setup(answer, { signal: controller.signal, timeoutMs: 100 });
  await clientFor(peer.transport);
  t.mock.timers.tick(20_000);
  assert.equal(peer.connection.closes, 0);
  controller.abort(new Error('explicit retire'));
  assert.equal(peer.connection.closes, 1);
});
