import assert from 'node:assert/strict';
import test, { type TestContext } from 'node:test';
import { webSocket } from '../src/transport.js';
import { WhipError } from '../src/errors.js';

function nativeSocket(t: TestContext) {
  let socket: NativeSocket;
  class NativeSocket {
    static OPEN = 1;
    static CLOSING = 2;
    readyState = 0;
    closeCount = 0;
    onopen: (() => void) | null = null;
    onmessage: ((event: { data: unknown }) => void) | null = null;
    onerror: (() => void) | null = null;
    onclose: ((event: { code: number; reason: string }) => void) | null = null;
    constructor(endpoint: unknown) {
      assert.equal(typeof endpoint, 'string');
      assert.equal(endpoint, 'wss://whip.example.ts.net/api/v3/ws');
      socket = this;
    }
    open() { this.readyState = 1; this.onopen?.(); }
    fail(reason: string) {
      this.readyState = 3;
      this.onerror?.();
      this.onclose?.({ code: 1006, reason });
    }
    close() { this.closeCount++; this.readyState = 3; }
    send() {}
  }
  const original = globalThis.WebSocket;
  t.after(() => { globalThis.WebSocket = original; });
  globalThis.WebSocket = NativeSocket as unknown as typeof WebSocket;
  return () => socket;
}

const endpoint = 'https://whip.example.ts.net';

test('WebSocket construction supports native implementations requiring a string URL', async t => {
  const getSocket = nativeSocket(t);
  const opening = webSocket(endpoint)({ message() {}, close() {} }, new AbortController().signal);
  getSocket().open();
  const transport = await opening;
  assert.equal(transport.httpEndpoint, 'https://whip.example.ts.net');
  assert.equal(transport.bufferedAmount, 0);
  transport.close();
});

test('React Native error followed by close preserves the native failure during opening', async t => {
  const getSocket = nativeSocket(t);
  const opening = webSocket(endpoint)({ message() {}, close() { assert.fail('Unopened transport must reject'); } }, new AbortController().signal);
  getSocket().fail('Received bad response code from server: 403.');
  await assert.rejects(opening, { name: 'WhipError', kind: 'disconnected', message: 'WebSocket closed (1006): Received bad response code from server: 403.' });
  assert.equal(getSocket().onclose, null);
  assert.equal(getSocket().onerror, null);
});

test('React Native error followed by close reports one bounded, sanitized native failure after opening', async t => {
  const getSocket = nativeSocket(t);
  const errors: Error[] = [];
  const opening = webSocket(endpoint)({ message() {}, close(error) { errors.push(error); } }, new AbortController().signal);
  getSocket().open();
  await opening;
  getSocket().fail(`TLS\n\tfailed\u0000\u202e\u2066 ${'x'.repeat(300)}UNBOUNDED_SUFFIX`);
  await Promise.resolve();
  assert.equal(errors.length, 1);
  assert.ok(errors[0] instanceof WhipError);
  assert.match(errors[0].message, /^WebSocket closed \(1006\): TLS failed x+$/);
  assert.ok(errors[0].message.length <= 'WebSocket closed (1006): '.length + 256);
  assert.doesNotMatch(errors[0].message, /UNBOUNDED_SUFFIX/);
});

test('an error without a close rejects opening on the next microtask', async t => {
  const getSocket = nativeSocket(t);
  const opening = webSocket(endpoint)({ message() {}, close() { assert.fail('Unopened transport must reject'); } }, new AbortController().signal);
  getSocket().onerror?.();
  await assert.rejects(opening, { kind: 'disconnected', message: 'WebSocket connection failed' });
  assert.equal(getSocket().closeCount, 1);
  assert.equal(getSocket().onopen, null);
});

test('normal remote close retains its code and reason and closes observation once', async t => {
  const getSocket = nativeSocket(t);
  const controller = new AbortController();
  const errors: Error[] = [];
  const opening = webSocket(endpoint)({ message() {}, close(error) { errors.push(error); } }, controller.signal);
  getSocket().open();
  const transport = await opening;
  getSocket().readyState = 3;
  getSocket().onclose?.({ code: 1000, reason: 'Server shutting down' });
  transport.close();
  controller.abort();
  assert.equal(errors.length, 1);
  assert.equal(errors[0]?.message, 'WebSocket closed (1000): Server shutting down');
  assert.equal(getSocket().closeCount, 0);
});

test('local close wins over a queued error fallback without a later callback', async t => {
  const getSocket = nativeSocket(t);
  const controller = new AbortController();
  const errors: Error[] = [];
  const opening = webSocket(endpoint)({ message() {}, close(error) { errors.push(error); } }, controller.signal);
  getSocket().open();
  const transport = await opening;
  getSocket().onerror?.();
  transport.close();
  controller.abort();
  await Promise.resolve();
  transport.close();
  assert.equal(errors.length, 1);
  assert.equal(errors[0]?.message, 'WebSocket closed');
  assert.equal(getSocket().closeCount, 1);
  assert.equal(getSocket().onclose, null);
  assert.throws(() => transport.send('after close'), { kind: 'disconnected' });
});

test('abort during opening wins over a queued error fallback and releases native handlers', async t => {
  const getSocket = nativeSocket(t);
  const controller = new AbortController();
  const opening = webSocket(endpoint)({ message() {}, close() { assert.fail('Unopened transport must reject'); } }, controller.signal);
  const reason = new WhipError('timeout', 'Probe deadline elapsed');
  getSocket().onerror?.();
  controller.abort(reason);
  await assert.rejects(opening, error => error === reason);
  assert.equal(getSocket().closeCount, 1);
  assert.equal(getSocket().onopen, null);
  assert.equal(getSocket().onmessage, null);
  assert.equal(getSocket().onclose, null);
});

test('abort after opening reports its original reason once despite a queued error fallback', async t => {
  const getSocket = nativeSocket(t);
  const controller = new AbortController();
  const errors: Error[] = [];
  const opening = webSocket(endpoint)({ message() {}, close(error) { errors.push(error); } }, controller.signal);
  getSocket().open();
  const transport = await opening;
  const reason = new WhipError('paused', 'App backgrounded');
  getSocket().onerror?.();
  controller.abort(reason);
  await Promise.resolve();
  transport.close();
  assert.deepEqual(errors, [reason]);
  assert.equal(getSocket().closeCount, 1);
});
