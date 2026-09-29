import assert from 'node:assert/strict';
import { test } from 'node:test';
import { setTimeout as delay } from 'node:timers/promises';
import { openBrowserConnection } from '../dist/browser-connection.js';
import { browserSocket } from '../dist/browser.js';

class Socket {
  static OPEN = 1;
  static CLOSING = 2;
  static instances = [];
  readyState = 0;
  bufferedAmount = 0;
  sent = [];
  constructor(url) { this.url = url; Socket.instances.push(this); }
  open() { this.readyState = 1; this.onopen?.({}); }
  close() { this.readyState = 3; }
  send(data) { this.sent.push(data); }
}
function setup(t) {
  Socket.instances = [];
  const original = globalThis.WebSocket;
  globalThis.WebSocket = Socket;
  const owner = new AbortController();
  t.after(() => { owner.abort(); globalThis.WebSocket = original; });
  return owner;
}
const handlers = { message() {}, close() {} };
const connect = signal => openBrowserConnection('http://example.test', handlers, signal);
const drain = () => new Promise(resolve => setImmediate(resolve));

test('four establishments share FIFO admission across hosts; established peers release the slot', async t => {
  const owner = setup(t);
  const calls = Array.from({ length: 36 }, (_, index) => openBrowserConnection(`http://host-${index}.test`, handlers, owner.signal));
  await drain();
  assert.equal(Socket.instances.length, 4);
  for (let index = 0; index < calls.length; index++) {
    const socket = Socket.instances[index];
    assert.equal(socket.url, `ws://host-${index}.test/api/v4/ws`);
    socket.open();
    await calls[index]; await drain();
    assert.equal(Socket.instances.length, Math.min(index + 5, calls.length));
    assert.equal(Socket.instances.filter(item => item.readyState === 0).length, Math.min(4, calls.length - index - 1));
  }
  assert.equal(Socket.instances.filter(item => item.readyState === 1).length, 36);
  for (const connection of await Promise.all(calls)) connection.close();
});

test('queue capacity is exact and aborted waiters never allocate or send', async t => {
  const owner = setup(t), queued = new AbortController();
  const active = Array.from({ length: 4 }, () => connect(owner.signal).catch(error => error));
  const calls = Array.from({ length: 128 }, () => connect(queued.signal).catch(error => error));
  await drain();
  assert.equal(Socket.instances.length, 4);
  await assert.rejects(connect(owner.signal), /queue is full/);
  const reason = new Error('owner retired before admission'); queued.abort(reason);
  assert((await Promise.all(calls)).every(error => error === reason));
  const replacement = connect(owner.signal);
  Socket.instances[0].open(); await drain();
  assert.equal(Socket.instances.length, 5);
  Socket.instances[4].open(); (await replacement).close();
  owner.abort(); await Promise.all(active);
  assert.equal(Socket.instances.length, 5);
  assert(Socket.instances.every(socket => socket.sent.length === 0));
});

test('ordinary call deadline removes a queued attempt before it receives a socket', async t => {
  const owner = setup(t);
  const active = Array.from({ length: 4 }, () => connect(owner.signal).catch(error => error));
  await drain();
  const wire = browserSocket('http://example.test', { expectedRuntimeID: 'runtime' });
  const result = wire({ jsonrpc: '2.0', id: 'waiting', method: 'initialize', params: { major: 4 } }, 'runtime', { timeoutMs: 20 });
  // AbortSignal.timeout is unref'd in Node; retain only this bounded test wait.
  await Promise.all([assert.rejects(result, error => error.name === 'TimeoutError'), delay(30)]);
  assert.equal(Socket.instances.length, 4);
  owner.abort(); await Promise.all(active); await drain();
  assert.equal(Socket.instances.length, 4);
});

test('constructor failure and connecting cancellation release exactly one slot', async t => {
  const owner = setup(t);
  globalThis.WebSocket = class extends Socket { constructor() { throw new Error('constructor failed'); } };
  await assert.rejects(connect(owner.signal), /constructor failed/);
  globalThis.WebSocket = Socket;
  const cancelled = new AbortController();
  const first = connect(cancelled.signal).catch(error => error);
  const active = Array.from({ length: 3 }, () => connect(owner.signal).catch(error => error));
  const waiting = connect(owner.signal);
  await drain(); assert.equal(Socket.instances.length, 4);
  const old = Socket.instances[0], lateOpen = old.onopen;
  const reason = new Error('cancelled connecting'); cancelled.abort(reason);
  assert.equal(await first, reason); await drain();
  assert.equal(Socket.instances.length, 5); assert.equal(old.readyState, 3);
  lateOpen({}); await drain();
  // A captured obsolete browser event cannot admit another connection or
  // turn cancellation into a successful connection.
  const next = connect(owner.signal).catch(error => error);
  await drain(); assert.equal(Socket.instances.length, 5);
  Socket.instances[4].open(); (await waiting).close();
  owner.abort(); await Promise.all([...active, next]);
});

test('cancellation between admission and construction does not allocate a late socket', async t => {
  const owner = setup(t), queued = new AbortController();
  const active = Array.from({ length: 4 }, () => connect(owner.signal).catch(error => error));
  const pending = connect(queued.signal).catch(error => error);
  await drain(); assert.equal(Socket.instances.length, 4);
  Socket.instances[0].open();
  const reason = new Error('cancelled after slot grant'); queued.abort(reason);
  assert.equal(await pending, reason); await drain();
  assert.equal(Socket.instances.length, 4);
  const next = connect(owner.signal);
  await drain(); assert.equal(Socket.instances.length, 5);
  Socket.instances[4].open(); (await next).close();
  owner.abort(); await Promise.all(active);
});
