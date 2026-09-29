import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { ExecutorClient, BrowserProviderClient, DeliveryError } from '../dist/index.js';
import { browserDuplex, browserProviderDuplex } from '../dist/browser.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: true, builtins: [] };
const response = (id, result) => JSON.stringify({ jsonrpc: '2.0', id, result });
class Socket {
  static OPEN = 1; static CLOSING = 2; static instances = []; static respond;
  readyState = 0; bufferedAmount = 0;
  constructor(url) {
    this.url = url; this.sent = []; Socket.instances.push(this);
    queueMicrotask(() => { if (this.readyState === 0) { this.readyState = 1; this.onopen?.({}); } });
  }
  send(raw) { const value = JSON.parse(raw); this.sent.push(value); Socket.respond?.(this, value); }
  close() { this.readyState = 3; }
  message(raw) { this.onmessage?.({ data: raw }); }
}
function setup(t, respond) {
  Socket.instances = []; Socket.respond = respond;
  const saved = globalThis.WebSocket; globalThis.WebSocket = Socket;
  t.after(() => { globalThis.WebSocket = saved; });
}
const options = { expectedRuntimeID: 'runtime', expectedProcessEpoch: 'boot_test' };
const open = extra => browserDuplex('https://example.test', { ...options, ...extra });
const answer = (socket, request) => queueMicrotask(() => socket.message(response(request.id, request.method === 'initialize' ? initial : fixture('ExecutorLease'))));
const clientFor = transport => ExecutorClient.connect(transport, { expectedRuntimeID: 'runtime' });

test('browser duplex verifies one network handshake and multiplexes exact events/replies', async t => {
  setup(t, (socket, request) => { answer(socket, request); if (request.method === 'executor.bind') queueMicrotask(() => socket.message(JSON.stringify(fixture('ExecutorEvent')))); });
  const mutable = { ...options };
  const transport = await browserDuplex('https://example.test', mutable);
  mutable.expectedRuntimeID = 'changed';
  const client = await clientFor(transport);
  const lease = await client.bind(fixture('ExecutorBindParams'));
  const events = client.events();
  const event = (await events.next()).value;
  assert.equal(event.generation, '9007199254740993');
  assert.equal(lease.generation, event.generation);
  assert.equal(atob(event.invocation.arguments_base64), '{"count":9007199254740993}');
  assert.equal(Socket.instances.length, 1);
  const socket = Socket.instances[0];
  assert.equal(socket.url, 'wss://example.test/api/v4/ws');
  assert.deepEqual(socket.sent[0].params, { major: 4, expected_runtime_id: 'runtime', network_client: true, expected_process_epoch: 'boot_test' });
  await events.return();
  assert.equal(socket.onmessage, null); assert.equal(socket.readyState, 3);
  await assert.rejects(client.bind(fixture('ExecutorBindParams')), /closed/);
});

test('network downgrade and identity mismatch close the peer before any bind', async t => {
  setup(t, answer);
  for (const value of [{ ...initial, network_client: false }, { ...initial, runtime_id: 'foreign' }, { ...initial, process_epoch: 'other' }, { ...initial, process_epoch: undefined }]) {
    Socket.respond = (socket, request) => queueMicrotask(() => socket.message(response(request.id, value)));
    await assert.rejects(clientFor(await open()), TypeError);
    assert.equal(Socket.instances.at(-1).sent.length, 1);
    assert.equal(Socket.instances.at(-1).onmessage, null);
  }
  const transport = await open();
  await assert.rejects(transport.request({ jsonrpc: '2.0', id: 'bind', method: 'executor.bind', params: fixture('ExecutorBindParams') }, {}), /requires initialization/);
  assert.equal(Socket.instances.at(-1).sent.length, 0);
  await transport.close();
});

test('unread browser events are bounded by count and actual frame bytes', async t => {
  setup(t, answer);
  for (const byteBound of [false, true]) {
    const transport = await open(); await clientFor(transport);
    const socket = Socket.instances.at(-1);
    const frame = (byteBound ? ' '.repeat(5 << 20) : '') + JSON.stringify(fixture('ExecutorEvent'));
    for (let index = 0; index < (byteBound ? 2 : 33); index++) socket.message(frame);
    await assert.rejects(transport.events[Symbol.asyncIterator]().next(), /queue exceeds limit/);
    assert.equal(socket.readyState, 3); await transport.close();
  }
});

test('duplicate, foreign and malformed messages terminate or reject without replay', async t => {
  setup(t, answer);
  for (const raw of ['{', response('foreign', initial), new Uint8Array([1]), ' '.repeat((8 << 20) + 1)]) {
    const transport = await open(); await clientFor(transport);
    const socket = Socket.instances.at(-1); socket.message(raw);
    await assert.rejects(transport.events[Symbol.asyncIterator]().next());
    assert.equal(socket.sent.length, 1); assert.equal(socket.readyState, 3);
  }
  const transport = await open(); const client = await clientFor(transport);
  const events = client.events();
  const bad = fixture('ExecutorEvent'); bad.invocation_id = 'foreign';
  Socket.instances.at(-1).message(JSON.stringify(bad));
  await assert.rejects(events.next(), /identity mismatch/);
  assert.equal(Socket.instances.at(-1).readyState, 3);
});

test('outstanding requests and native writes are bounded; abort rejects every waiter and closes', async t => {
  setup(t, answer);
  const transport = await open(); await clientFor(transport);
  Socket.respond = () => {};
  const calls = [];
  for (let index = 0; index < 32; index++) calls.push(transport.request({ jsonrpc: '2.0', id: String(index), method: 'executor.pending', params: fixture('ExecutorPendingParams') }, {}).catch(error => error));
  await assert.rejects(transport.request({ jsonrpc: '2.0', id: 'extra', method: 'executor.pending', params: {} }, {}), /capacity/);
  await assert.rejects(transport.request({ jsonrpc: '2.0', id: '0', method: 'executor.pending', params: {} }, {}), /identity conflict/);
  const events = transport.events[Symbol.asyncIterator](); const next = events.next();
  await assert.rejects(events.next(), /Concurrent/);
  await events.return(); assert.equal((await next).done, true);
  assert.ok((await Promise.all(calls)).every(value => value instanceof DeliveryError));

  Socket.respond = answer;
  const peer = await open(); await clientFor(peer);
  Socket.respond = () => {};
  const controller = new AbortController(); const reason = new Error('explicit observation cancellation');
  const pending = peer.request({ jsonrpc: '2.0', id: 'one', method: 'executor.pending', params: {} }, { signal: controller.signal });
  controller.abort(reason); await assert.rejects(pending, error => error === reason);
  assert.equal(Socket.instances.at(-1).sent.length, 2); assert.equal(Socket.instances.at(-1).onmessage, null);

  Socket.respond = answer;
  const full = await open(); await clientFor(full);
  Socket.instances.at(-1).bufferedAmount = 8 << 20;
  await assert.rejects(full.request({ jsonrpc: '2.0', id: 'full', method: 'executor.pending', params: {} }, {}), /queue limit/);
  assert.equal(Socket.instances.at(-1).sent.length, 1); assert.equal(Socket.instances.at(-1).readyState, 3);
});

test('successful connection cancels only its connect timer; lifetime abort remains active', async t => {
  setup(t, answer);
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const lifetime = new AbortController();
  const peer = await open({ signal: lifetime.signal }); await clientFor(peer);
  t.mock.timers.tick(20_000);
  assert.equal(Socket.instances.at(-1).readyState, 1);
  lifetime.abort(new Error('lifetime ended'));
  await assert.rejects(peer.events[Symbol.asyncIterator]().next(), /lifetime ended/);
  assert.equal(Socket.instances.at(-1).readyState, 3);
});


test('network browser provider uses the same bounded transport with its own strict event contract', async t => {
  setup(t, (socket, request) => queueMicrotask(() => socket.message(response(request.id, request.method === 'initialize' ? initial : fixture('BrowserProviderBindResult')))));
  const transport = await browserProviderDuplex('https://example.test', options);
  const client = await BrowserProviderClient.connect(transport, options);
  await client.bind(fixture('BrowserProviderBindParams'));
  const socket = Socket.instances[0];
  socket.message(JSON.stringify(fixture('BrowserEvent')));
  const events = client.events(), event = (await events.next()).value;
  assert.equal(event.command.deadline_millis, '9007199254740993');
  assert.equal(socket.sent[0].params.network_client, true);
  assert.equal(socket.sent[0].params.expected_process_epoch, 'boot_test');
  socket.message(JSON.stringify(fixture('ExecutorEvent')));
  await assert.rejects(events.next(), TypeError);
  assert.equal(socket.readyState, 3); assert.equal(Socket.instances.length, 1);
});
