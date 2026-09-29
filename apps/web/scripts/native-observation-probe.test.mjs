import assert from 'node:assert/strict';
import test from 'node:test';
import vm from 'node:vm';
import { randomUUID } from 'node:crypto';
import { openBrowserConnection } from '../../../packages/sdk/dist/browser-connection.js';
import { assertSharedObservations, installObservationProbe, retainObservationEvidence } from './native-observation-probe.mjs';

function fixture() {
  const pageEvents = new Map(), retired = [], sockets = [], physicalPending = new Map();
  class Socket {
    static OPEN = 1; static CLOSING = 2;
    handlers = new Map(); sent = []; closed = false; readyState = 1; bufferedAmount = 0;
    constructor() { sockets.push(this); queueMicrotask(() => this.onopen?.({})); }
    addEventListener(name, callback) { this.handlers.set(name, callback); }
    send(text) {
      if (this.sendError) throw this.sendError;
      this.sent.push(text);
      const request = JSON.parse(text);
      if (request.method === 'sessions.observe') physicalPending.set(this, request);
    }
    close() { if (this.closeError) throw this.closeError; this.closed = true; this.readyState = 2; } // Native close event may arrive later.
    reply(value) {
      if (physicalPending.get(this)?.id === value.id) physicalPending.delete(this);
      const event = { data: JSON.stringify(value) };
      this.handlers.get('message')?.(event); this.onmessage?.(event);
    }
    physicalClose() { physicalPending.delete(this); this.handlers.get('close')?.(); }
  }
  const realm = { WebSocket: Socket, crypto: { randomUUID }, performance, structuredClone,
    addEventListener: (name, callback) => pageEvents.set(name, callback),
    replObservationEvidence: value => { retired.push(value); return Promise.resolve(); } };
  vm.runInNewContext(`(${installObservationProbe.toString()})()`, realm);
  const connect = (epoch = 'boot', initialID = 'browser-initialize') => {
    const socket = new realm.WebSocket();
    socket.send(JSON.stringify({ id: initialID, method: 'initialize', params: {} }));
    socket.reply({ id: initialID, result: { runtime_id: 'runtime', process_epoch: epoch } });
    return socket;
  };
  const observe = (socket, id = 'rpc-1', owner = 'session') => socket.send(JSON.stringify({ id, method: 'sessions.observe', params: { session_id: owner } }));
  const rawPending = owner => [...physicalPending.values()].filter(request => request.params.session_id === owner).length;
  return { realm, connect, observe, read: () => realm.__readObservationProbe(), pageEvents, retired, rawPending, sockets };
}

test('a retired local observer stops counting before the delayed physical close event', () => {
  const f = fixture(), first = f.connect('boot', 'initialize'); f.observe(first);
  first.close();
  const second = f.connect('boot', 'initialize'); f.observe(second);
  // The viewer's former physical socket assertion fails even though the SDK
  // has synchronously retired the first local observation before replacing it.
  assert.equal(f.rawPending('session'), 2);
  assert.throws(() => assert(f.rawPending('session') <= 1, 'Duplicate views must share the owner observation'));
  assert.doesNotThrow(() => assertSharedObservations([f.read()]));
  assert.equal(Math.max(...Object.values(f.read().maximum)), 1);
  assert.equal(f.read().overlaps.length, 0);
  assert.equal(first.sent.length, 2); assert(first.closed); assert.equal(second.sent.length, 2);
  first.physicalClose();
  assert.equal(f.rawPending('session'), 1);
  assertSharedObservations([f.read()]);
});

test('the shipping browser transport retires observation ownership before physical close, without masking active overlap', async t => {
  const f = fixture(), controllers = [], closures = [], original = globalThis.WebSocket;
  globalThis.WebSocket = f.realm.WebSocket;
  t.after(() => { for (const controller of controllers) controller.abort(); globalThis.WebSocket = original; });
  const open = async () => {
    const controller = new AbortController(); controllers.push(controller);
    const connection = await openBrowserConnection('http://fixture.test', {
      message() {}, close(error) { closures.push(error); },
    }, controller.signal);
    connection.send(JSON.stringify({ id: 'initialize', method: 'initialize', params: {} }));
    f.sockets.at(-1).reply({ id: 'initialize', result: { runtime_id: 'runtime', process_epoch: 'boot' } });
    f.observe(connection);
    return controller;
  };
  const first = await open();
  const cancellation = new Error('Retire only this local observer'); first.abort(cancellation);
  assert.equal(closures[0], cancellation);
  assert.equal(f.sockets[0].readyState, 2, 'Physical close is still pending');
  await open();
  assert.equal(f.rawPending('session'), 2);
  assert.throws(() => assert(f.rawPending('session') <= 1, 'Duplicate views must share the owner observation'));
  assertSharedObservations([f.read()]);
  await open();
  assert.equal(Math.max(...Object.values(f.read().maximum)), 2);
  assert.throws(() => assertSharedObservations([f.read()]), /Duplicate views must share/);
});

test('two true same-document/runtime/epoch/owner observations are retained as a violation', () => {
  const f = fixture(), first = f.connect('boot', 'initialize'), second = f.connect('boot', 'initialize');
  f.observe(first); f.observe(second);
  assert.equal(f.read().clientInitializations, 2);
  assert.equal(Math.max(...Object.values(f.read().maximum)), 2);
  assert.throws(() => assertSharedObservations([f.read()]), /Duplicate views must share/);
  first.reply({ id: 'rpc-1', result: {} }); second.close();
  const overlap = f.read().overlaps[0];
  assert.equal(overlap.length, 2);
  assert.deepEqual(overlap.map(item => item.settled), ['reply', 'close_called']);
  assert.notEqual(overlap[0].connection, overlap[1].connection);
  assert.notEqual(overlap[0].clientInitialization, overlap[1].clientInitialization);
  assert(overlap.every(item => item.settledAt >= item.sentAt));
  assert.throws(() => assertSharedObservations([f.read()]), /Duplicate views must share/);
});

test('process epochs and owner identities remain separate without hiding same-epoch duplicates', () => {
  const f = fixture(), old = f.connect('old'), fresh = f.connect('new');
  f.observe(old); f.observe(fresh); f.observe(f.connect('new'), 'rpc-1', 'child');
  assert.equal(Object.keys(f.read().maximum).length, 3);
  assert.equal(Math.max(...Object.values(f.read().maximum)), 1);
  assertSharedObservations([f.read()]);
  f.observe(f.connect('new'));
  assert.equal(Math.max(...Object.values(f.read().maximum)), 2);
  assert.throws(() => assertSharedObservations([f.read()]), /Duplicate views must share/);
});

test('distinct documents do not share an observation owner even with identical runtime, epoch and session', () => {
  const first = fixture(), second = fixture();
  first.observe(first.connect()); second.observe(second.connect());
  assert.notEqual(first.read().documentID, second.read().documentID);
  assertSharedObservations([first.read(), second.read()]);
  second.observe(second.connect());
  assert.throws(() => assertSharedObservations([first.read(), second.read()]), /Duplicate views must share/);
});

test('every publication latches ownership failure before an older snapshot replaces the document', () => {
  const f = fixture(), documents = [], errors = [];
  f.observe(f.connect()); const older = f.read();
  f.observe(f.connect());
  retainObservationEvidence(documents, f.read(), error => errors.push(error));
  retainObservationEvidence(documents, older, error => errors.push(error));
  assert.equal(documents.length, 1);
  assertSharedObservations(documents);
  assert.equal(errors.length, 1);
  assert.match(errors[0].message, /Duplicate views must share/);
});

test('the viewer cannot pass on missing, faulty or overflowed observation evidence', () => {
  const f = fixture();
  assert.throws(() => assertSharedObservations([]), /Missing/);
  assert.throws(() => assertSharedObservations([f.read()]), /No verified session/);
  f.observe(f.connect());
  for (const evidence of [
    { ...f.read(), overflow: true },
    { ...f.read(), faults: ['unverified initialization'] },
    { ...f.read(), maximum: { '["runtime",null,"session"]': 1 } },
  ]) assert.throws(() => assertSharedObservations([evidence]));
  const documents = [], errors = [];
  for (let index = 0; index < 33; index++) retainObservationEvidence(documents, { ...f.read(), documentID: `document-${index}` }, error => errors.push(error));
  assert.equal(documents.length, 32);
  assert.equal(errors.length, 1); assert.match(errors[0].message, /overflow/);
  retainObservationEvidence(documents, { ...f.read(), padding: 'x'.repeat(128 << 10) }, error => errors.push(error));
  assert.equal(documents.length, 32); assert.equal(errors.length, 2);
});

test('document retirement preserves bounded evidence without sending or closing a socket', () => {
  const f = fixture(), first = f.connect(), second = f.connect();
  f.observe(first); f.observe(second); f.pageEvents.get('pagehide')();
  assert.equal(f.retired.length, 3);
  assert.equal(Math.max(...Object.values(f.retired[1].maximum)), 2);
  assert(f.retired.at(-1).overlaps[0].every(item => item.settled === 'document_retired'));
  assert.equal(first.sent.length, 2); assert.equal(second.sent.length, 2);
  assert.equal(first.closed, false); assert.equal(second.closed, false);
});

test('live socket evidence overflow is explicit', () => {
  const f = fixture();
  for (let index = 0; index < 129; index++) f.connect();
  assert.equal(f.read().overflow, true);
});

test('failed native sends preserve the exception without inventing a pending observation', () => {
  const f = fixture(), failed = f.connect(), active = f.connect();
  failed.sendError = new Error('native send rejected');
  assert.throws(() => f.observe(failed), error => error === failed.sendError);
  f.observe(active);
  assert.equal(failed.sent.length, 1);
  assert.equal(Math.max(...Object.values(f.read().maximum)), 1);
  assert.equal(f.read().overlaps.length, 0);
});

test('failed native closes preserve the exception and leave the true observation pending', () => {
  const f = fixture(), first = f.connect(), second = f.connect();
  f.observe(first); first.closeError = new Error('native close rejected');
  assert.throws(() => first.close(), error => error === first.closeError);
  f.observe(second);
  assert.equal(first.closed, false);
  assert.equal(Math.max(...Object.values(f.read().maximum)), 2);
  assert.equal(f.read().overlaps[0][0].settled, null);
  first.reply({ id: 'rpc-1', result: {} });
  assert.equal(f.read().overlaps[0][0].settled, 'reply');
});
