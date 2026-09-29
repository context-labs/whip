import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { selectBrowserProvider, DeliveryError, RemoteError } from '../dist/index.js';
const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const defer = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
async function until(predicate) { for (let i = 0; i < 100; i++) { if (predicate()) return; await new Promise(resolve => setImmediate(resolve)); } assert.fail('condition did not settle'); }
function setup() {
  const queue = []; let waiter, ended = false, listener;
  const sent = [], commands = [], cancelled = [], retired = [], released = [], errors = [];
  const peer = {
    async bind() { return fixture('BrowserProviderBindResult'); },
    async result(value) { sent.push(['result', value]); return { accepted: true }; },
    async screenshotResult(value, bytes) { sent.push(['screenshot', value, bytes]); return { accepted: true }; },
    async inventory(value) { sent.push(['inventory', value]); return { accepted: true }; },
    async event(value) { sent.push(['event', value]); return { accepted: true }; },
    async *events() { while (!ended) { if (!queue.length) { waiter = defer(); await waiter.promise; } if (queue.length) yield queue.shift(); } },
    async close() { ended = true; waiter?.resolve(); },
  };
  const bridge = {
    async select() {},
    async dispatch(command) { commands.push(command); return result(command); },
    async inventory(request) { return { request_id: request.request_id, root_id: request.root_id, provider_epoch: request.provider_epoch, tabs: [] }; },
    cancel(value) { cancelled.push(value); },
    async retire(value) { retired.push(value); },
    async release(value) { released.push(value); },
    onEvent(fn) { listener = fn; return () => { listener = undefined; }; },
  };
  const send = value => { queue.push(value); waiter?.resolve(); };
  return { peer, bridge, sent, commands, cancelled, retired, released, errors, send, emit: value => listener?.({ kind: 'provider', event: value }), select: options => selectBrowserProvider(peer, fixture('BrowserProviderBindParams'), bridge, { onError: error => errors.push(error), ...options }) };
}
function command(id = 'command', attachment = 'attachment') {
  const event = fixture('BrowserEvent');
  event.command.deadline_millis = String(Date.now() + 60000);
  event.command.command_id = id; event.command.scope.attachment_id = attachment;
  return event;
}
function result(command) { return { command_id: command.command_id, root_id: command.root_id, provider_epoch: command.provider_epoch, attachment_generation: command.scope.attachment_generation, document_revision: 'doc', url: 'about:blank', title: 'Human page', result: {} }; }
function cancel(event) { return { jsonrpc: '2.0', method: 'browser.command.cancel', command: null, inventory: null, cancel: { command_id: event.command.command_id, root_id: event.command.root_id, provider_epoch: event.command.provider_epoch, attachment_generation: event.command.scope.attachment_generation }, revoked: null, retired: null }; }

test('selection waits for native acknowledgement before dispatch and keeps exact one-use commands', async () => {
  const f = setup(), native = defer(); f.bridge.select = () => native.promise;
  const starting = f.select(); f.send(command());
  await new Promise(resolve => setImmediate(resolve)); assert.equal(f.commands.length, 0);
  native.resolve(); const selection = await starting;
  await until(() => f.sent.length === 1); assert.equal(f.commands.length, 1);
  f.send(command()); await until(() => !selection.active);
  await selection.release(); assert.equal(f.commands.length, 1); assert.match(f.errors[0].message, /replay/); assert.equal(f.released.length, 1);
});

test('explicit cancellation joins only its native call and does not acknowledge or repeat it', async () => {
  const f = setup(), completion = defer(), event = command();
  f.bridge.dispatch = async value => { f.commands.push(value); if (value.command_id === 'command') await completion.promise; return result(value); };
  f.bridge.cancel = value => { f.cancelled.push(value); completion.resolve(); };
  const selection = await f.select(); f.send(event); await until(() => f.commands.length === 1);
  f.send(cancel(event)); await until(() => f.cancelled.length === 1);
  f.send(command('other')); await until(() => f.sent.length === 1);
  assert.equal(f.sent[0][1].command_id, 'other'); assert.equal(selection.active, true);
  await selection.release(); assert.equal(f.commands.length, 2);
});

test('release closes peer, cancels native work and waits until native handler has joined', async () => {
  const f = setup(), completion = defer(); let joined = false;
  f.bridge.dispatch = async value => { f.commands.push(value); await completion.promise; joined = true; return result(value); };
  const selection = await f.select(); f.send(command()); await until(() => f.commands.length === 1);
  let finished = false; const releasing = selection.release().then(() => { finished = true; });
  await until(() => f.cancelled.length === 1); assert.equal(finished, false); assert.equal(selection.active, false);
  completion.resolve(); await releasing; assert.equal(joined, true); assert.equal(f.sent.length, 0); assert.equal(f.released.length, 1);
});

test('native errors and invalid result ownership become one safe uncertain result without exposing diagnostics', async () => {
  for (const mode of ['throw', 'foreign', 'bad-screenshot']) {
    const f = setup();
    f.bridge.dispatch = async value => { f.commands.push(value); if (mode === 'throw') throw new Error('secret-native-diagnostic'); if (mode === 'foreign') return { ...result(value), root_id: 'foreign' }; return { ...result(value), screenshotBytes: new Uint8Array((4 << 20) + 1) }; };
    const selection = await f.select(); f.send(command()); await until(() => f.sent.length === 1);
    assert.equal(f.sent[0][1].error.kind, 'outcome_unknown'); assert.doesNotMatch(JSON.stringify(f.sent), /secret-native-diagnostic/);
    assert.equal(f.commands.length, 1); await selection.release();
  }
});

test('lost result acknowledgement closes and joins without a retry or a second native call', async () => {
  const f = setup(); f.peer.result = async value => { f.sent.push(['result', value]); throw new DeliveryError('lost acknowledgement'); };
  const selection = await f.select(); f.send(command()); await until(() => !selection.active); await selection.release();
  assert.equal(f.commands.length, 1); assert.equal(f.sent.length, 1); assert.equal(f.released.length, 1);
});

test('retirement tears down exact scopes and stale observation does not affect sibling authority', async () => {
  const f = setup(), selection = await f.select(), old = fixture('BrowserProviderEventParams');
  f.peer.event = async value => { f.sent.push(['event', value]); if (value.attachment_id === old.attachment_id) throw new RemoteError({ kind: 'BROWSER_EVENT_STALE', code: -32009, message: 'retired' }); return { accepted: true }; };
  f.emit(old); await until(() => f.sent.length === 1); await new Promise(resolve => setImmediate(resolve));
  f.emit(old); f.emit({ ...old, attachment_id: 'sibling' }); await until(() => f.sent.length === 2); assert.equal(selection.active, true);
  const retired = { jsonrpc: '2.0', method: 'browser.scopes.retired', command: null, inventory: null, cancel: null, revoked: null, retired: { root_id: old.root_id, provider_id: 'provider', provider_epoch: old.provider_epoch, scopes: [command().command.scope] } };
  f.send(retired); await until(() => f.retired.length === 1); assert.deepEqual(f.retired[0], retired.retired);
  await selection.release();
});

test('command and observation queues fail closed at their explicit bounds without replay', async () => {
  const f = setup(), callbacks = new Map();
  f.bridge.dispatch = async value => { f.commands.push(value); const wait = defer(); callbacks.set(value.command_id, wait); await wait.promise; return result(value); };
  f.bridge.cancel = value => callbacks.get(value.command_id)?.resolve();
  const selection = await f.select();
  for (let i = 0; i < 33; i++) f.send(command('cmd_' + i));
  await until(() => !selection.active); await selection.release(); assert.equal(f.commands.length, 32); assert.equal(f.sent.length, 0);
  const observed = setup(), stalled = defer();
  observed.peer.event = () => stalled.promise;
  const close = observed.peer.close; observed.peer.close = async () => { stalled.resolve({ accepted: true }); await close(); };
  const observing = await observed.select();
  for (let i = 0; i < 65; i++) observed.emit({ ...fixture('BrowserProviderEventParams'), sequence: String(i + 1) });
  await until(() => !observing.active); await observing.release(); assert.match(observed.errors[0].message, /observation queue/);
});

test('late native selection acknowledgement is joined and releases only its exact generation', async () => {
  const f = setup(), native = defer(), abort = new AbortController(); let selected = false;
  f.bridge.select = async () => { selected = true; await native.promise; };
  const starting = f.select({ signal: abort.signal }); await until(() => selected);
  abort.abort(new Error('host retired')); await assert.rejects(starting, /host retired/);
  assert.equal(f.released.length, 0); native.resolve(); await until(() => f.released.length === 1);
  assert.deepEqual(f.released[0], { rootId: 'session_root', providerEpoch: 'provider_epoch' });
  assert.equal(f.commands.length, 0);
});
