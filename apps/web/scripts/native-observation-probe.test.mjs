import assert from 'node:assert/strict';
import test from 'node:test';
import vm from 'node:vm';
import { randomUUID } from 'node:crypto';
import { installObservationProbe } from './native-observation-probe.mjs';

function fixture() {
  const pageEvents = new Map(), retired = [];
  class Socket {
    handlers = new Map(); sent = []; closed = false;
    addEventListener(name, callback) { this.handlers.set(name, callback); }
    send(text) { if (this.sendError) throw this.sendError; this.sent.push(text); }
    close() { if (this.closeError) throw this.closeError; this.closed = true; } // Native close event may arrive later.
    reply(value) { this.handlers.get('message')?.({ data: JSON.stringify(value) }); }
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
  return { realm, connect, observe, read: () => realm.__readObservationProbe(), pageEvents, retired };
}

test('a retired local observer stops counting before the delayed physical close event', () => {
  const f = fixture(), first = f.connect('boot', 'initialize'); f.observe(first);
  first.close();
  const second = f.connect('boot', 'initialize'); f.observe(second);
  assert.equal(Math.max(...Object.values(f.read().maximum)), 1);
  assert.equal(f.read().overlaps.length, 0);
  assert.equal(first.sent.length, 2); assert(first.closed); assert.equal(second.sent.length, 2);
});

test('two true same-document/runtime/epoch/owner observations are retained as a violation', () => {
  const f = fixture(), first = f.connect(), second = f.connect();
  f.observe(first); f.observe(second);
  assert.equal(Math.max(...Object.values(f.read().maximum)), 2);
  first.reply({ id: 'rpc-1', result: {} }); second.close();
  const overlap = f.read().overlaps[0];
  assert.equal(overlap.length, 2);
  assert.deepEqual(overlap.map(item => item.settled), ['reply', 'close_called']);
  assert.notEqual(overlap[0].connection, overlap[1].connection);
  assert(overlap.every(item => item.settledAt >= item.sentAt));
});

test('process epochs and owner identities remain separate without hiding same-epoch duplicates', () => {
  const f = fixture(), old = f.connect('old'), fresh = f.connect('new');
  f.observe(old); f.observe(fresh); f.observe(f.connect('new'), 'rpc-1', 'child');
  assert.equal(Object.keys(f.read().maximum).length, 3);
  assert.equal(Math.max(...Object.values(f.read().maximum)), 1);
  f.observe(f.connect('new'));
  assert.equal(Math.max(...Object.values(f.read().maximum)), 2);
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
