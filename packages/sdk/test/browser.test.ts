import assert from 'node:assert/strict';
import test from 'node:test';
import { setImmediate as flush } from 'node:timers/promises';
import type { BrowserCommand, BrowserCommandResultParams, BrowserProviderBindParams, BrowserProviderEventParams } from '@whip/protocol';
import { WhipClient } from '../src/client.js';
import type { BrowserProviderBridge } from '../src/browser.js';
import { transportFixture, type FixtureRequest, type FixtureConnection } from './transport-fixture.js';
import type { ScriptedDaemonOptions } from '../src/testing.js';

const offer: BrowserProviderBindParams = { root_id: 'root', version: 1, desktop_id: 'desktop', window_id: 'window', create_profile_id: 'profile', offer_revision: 'offer', offered_tabs: [], offered_preview_hosts: [] };
const provider = { version: 1, provider_id: 'provider', provider_epoch: 'epoch' };
function command(id = 'command'): BrowserCommand {
  return { command_id: id, operation_id: 'operation', root_id: 'root', agent_id: 'root', provider_epoch: 'epoch', kind: 'cdp', deadline_millis: String(Date.now() + 30_000), arguments: { method: 'Runtime.evaluate', params: { expression: '1' } }, expected_document: 'document',
    scope: { provider_id: 'provider', provider_epoch: 'epoch', tab_id: 'tab', tab_generation: 'tab-generation', profile_id: 'profile', attachment_id: 'attachment', attachment_generation: 'attachment-generation', rights: ['control'] } };
}
function result(command: BrowserCommand): BrowserCommandResultParams {
  return { command_id: command.command_id, root_id: command.root_id, provider_epoch: command.provider_epoch, attachment_generation: command.scope.attachment_generation!, document_revision: 'document', result: { value: 1 } };
}
function setup(overrides: Partial<BrowserProviderBridge> = {}, options: ScriptedDaemonOptions = {}) {
  const fixture = transportFixture({ request(request, connection) {
    connection.reply(request, request.method === 'browser.provider.bind' ? provider : { accepted: true });
  }, ...options });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'desktop-client', clientKind: 'human', browserProvider: true, reconnect: false });
  const dispatched: BrowserCommand[] = [];
  const released: unknown[] = [];
  const cancelled: unknown[] = [];
  let listener: Parameters<BrowserProviderBridge['onEvent']>[0] | undefined;
  const bridge: BrowserProviderBridge = {
    select: async () => {}, dispatch: async command => { dispatched.push(command); return result(command); },
    cancel: input => { cancelled.push(input); }, release: async input => { released.push(input); },
    onEvent: callback => { listener = callback; return () => { listener = undefined; }; }, ...overrides,
  };
  return { client, fixture, bridge, dispatched, released, cancelled, emit: (event: BrowserProviderEventParams) => listener?.({ kind: 'provider', event }) };
}

test('metadata discovery uses the exact live v2 provider without dispatch or admission', async t => {
  let inventories = 0;
  const s = setup({ inventory: async request => { inventories++; return { request_id: request.request_id, root_id: request.root_id, provider_epoch: request.provider_epoch, tabs: [] }; } }, {
    request(request, connection) { connection.reply(request, request.method === 'browser.provider.bind' ? { ...provider, version: 2 } : { accepted: true }); },
  });
  t.after(() => s.client.close()); await s.client.connect();
  assert.ok((s.fixture.current.requests[0]!.params.capabilities as string[]).includes('desktop-browser-v2'));
  const selection = await s.client.browser.select({ ...offer, version: 2, availability: true }, s.bridge);
  const inventory = { request_id: 'inventory', root_id: 'root', agent_id: 'root', provider_id: 'provider', provider_epoch: 'epoch', tabs: [] };
  s.fixture.current.notify('browser.inventory', { ...inventory, provider_epoch: 'retired' }); await flush();
  assert.equal(inventories, 0);
  s.fixture.current.notify('browser.inventory', inventory); await flush(); await flush();
  assert.equal(inventories, 1); assert.equal(s.dispatched.length, 0);
  assert.deepEqual(s.fixture.current.requests.find(request => request.method === 'browser.inventory.result')?.params, { request_id: 'inventory', root_id: 'root', provider_epoch: 'epoch', tabs: [] });
  await selection.release(); s.fixture.current.notify('browser.inventory', inventory); await flush(); assert.equal(inventories, 1);
});

test('a rejected late inventory reply neither replays nor poisons live control', async t => {
  const errors: Error[] = []; let replies = 0;
  const s = setup({ inventory: async request => ({ request_id: request.request_id, root_id: request.root_id, provider_epoch: request.provider_epoch, tabs: [] }) }, {
    request(request, connection) {
      if (request.method === 'browser.inventory.result') { replies++; connection.error(request, 'request_expired', -32009); return; }
      connection.reply(request, request.method === 'browser.provider.bind' ? { ...provider, version: 2 } : { accepted: true });
    },
  });
  t.after(() => s.client.close()); await s.client.connect();
  const selected = await s.client.browser.select({ ...offer, version: 2, availability: true }, s.bridge, { onError: error => errors.push(error) });
  s.fixture.current.notify('browser.inventory', { request_id: 'cancelled', root_id: 'root', agent_id: 'root', provider_id: 'provider', provider_epoch: 'epoch', tabs: [] });
  await flush(); await flush();
  assert.equal(replies, 1); assert.equal(selected.active, true); assert.deepEqual(errors, []);
  s.fixture.current.notify('browser.command', command('after-inventory')); await flush();
  assert.equal(s.dispatched.length, 1);
});

test('Browser provider capability is opt-in and selection binds an exact root before native dispatch', async t => {
  const s = setup(); t.after(() => s.client.close()); await s.client.connect();
  assert.ok((s.fixture.current.requests[0]!.params.capabilities as string[]).includes('desktop-browser-v1'));
  s.fixture.current.notify('browser.command', command('before-selection')); await flush(); assert.equal(s.dispatched.length, 0);
  const selection = await s.client.browser.select(offer, s.bridge);
  assert.equal(selection.active, true);
  assert.deepEqual(s.fixture.current.requests.find(request => request.method === 'browser.provider.bind')?.params, offer);
  s.fixture.current.notify('browser.command', command()); await flush();
  assert.equal(s.dispatched.length, 1);
  assert.deepEqual(s.fixture.current.requests.at(-1)?.params, result(command()));
  await assert.rejects(s.client.browser.select(offer, s.bridge), /Release the existing/);
});

test('explicit release unbinds exactly once and only releases native authority, not the human tab', async t => {
  const s = setup(); t.after(() => s.client.close()); await s.client.connect();
  const selection = await s.client.browser.select(offer, s.bridge);
  await selection.release(); await selection.release();
  assert.equal(selection.active, false);
  const unbinds = s.fixture.current.requests.filter(request => request.method === 'browser.provider.unbind');
  assert.equal(unbinds.length, 1); assert.deepEqual(unbinds[0]!.params, { root_id: 'root', provider_epoch: 'epoch' });
  s.fixture.current.notify('browser.command', command()); await flush(); assert.equal(s.dispatched.length, 0);
});

test('idle provider revocation releases native by exact epoch and never rebinds', async t => {
  const s = setup(); t.after(() => s.client.close()); await s.client.connect();
  const errors: Error[] = [];
  const selection = await s.client.browser.select(offer, s.bridge, { onError: error => errors.push(error) });
  s.fixture.current.notify('browser.provider.revoked', { root_id: 'root', provider_id: 'provider', provider_epoch: 'old', reason: 'replaced' });
  await flush(); assert.equal(selection.active, true);
  s.fixture.current.notify('browser.provider.revoked', { root_id: 'root', provider_id: 'provider', provider_epoch: 'epoch', reason: 'replaced' });
  await flush(); assert.equal(selection.active, false); assert.equal(s.released.length, 1);
  assert.equal(errors.length, 1); assert.match(errors[0]!.message, /revoked.*replaced/);
  assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.provider.unbind').length, 0);
});

test('revocation racing the bind reply prevents native selection', async t => {
  const s = setup(); t.after(() => s.client.close()); await s.client.connect();
  const nativeSelect = s.bridge.select;
  let selected = false;
  s.bridge.select = async input => { selected = true; await nativeSelect(input); };
  // call() resolves on a microtask; revoke after its wire reply but before SDK bind resumes.
  const selecting = s.client.browser.select(offer, s.bridge);
  s.fixture.current.notify('browser.provider.revoked', { root_id: 'root', provider_id: 'provider', provider_epoch: 'epoch', reason: 'replaced' });
  await assert.rejects(selecting, /revoked before selection/);
  assert.equal(selected, false);
  assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.provider.unbind').length, 0);
});

test('a cancellation for another attachment generation does not cancel this command', async t => {
  let finish!: (value: BrowserCommandResultParams) => void;
  const s = setup({ dispatch: () => new Promise(resolve => { finish = resolve; }) });
  t.after(() => s.client.close()); await s.client.connect(); await s.client.browser.select(offer, s.bridge);
  s.fixture.current.notify('browser.command', command()); await flush();
  s.fixture.current.notify('browser.command.cancel', { command_id: 'command', root_id: 'root', provider_epoch: 'epoch', attachment_generation: 'old-generation', reason: 'cancelled' });
  assert.equal(s.cancelled.length, 0);
  finish(result(command())); await flush();
  assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.command.result').length, 1);
});

test('native selection ACK gates commands and queued cancellation prevents dispatch', async t => {
  let acknowledge!: () => void;
  const s = setup({ select: () => new Promise<void>(resolve => { acknowledge = resolve; }) });
  t.after(() => s.client.close()); await s.client.connect();
  const selecting = s.client.browser.select(offer, s.bridge); await flush();
  s.fixture.current.notify('browser.command', command()); await flush(); assert.equal(s.dispatched.length, 0);
  const cancel = { command_id: 'command', root_id: 'root', provider_epoch: 'epoch', attachment_generation: 'attachment-generation', reason: 'cancelled' };
  s.fixture.current.notify('browser.command.cancel', cancel);
  acknowledge(); await selecting; await flush();
  assert.equal(s.dispatched.length, 0); assert.deepEqual(s.cancelled, [cancel]);
});

test('Browser commands are not replayed and wrong roots/epochs never reach native', async t => {
  const s = setup(); t.after(() => s.client.close()); await s.client.connect(); await s.client.browser.select(offer, s.bridge);
  s.fixture.current.notify('browser.command', { ...command('foreign-root'), root_id: 'foreign' });
  s.fixture.current.notify('browser.command', { ...command('foreign-epoch'), provider_epoch: 'old' });
  s.fixture.current.notify('browser.command', command()); s.fixture.current.notify('browser.command', command());
  await flush(); assert.equal(s.dispatched.length, 1);
});

test('native failures return outcome_unknown once, without retry', async t => {
  let calls = 0;
  const s = setup({ dispatch: async () => { calls++; throw new Error('native reply lost'); } });
  t.after(() => s.client.close()); await s.client.connect(); await s.client.browser.select(offer, s.bridge);
  s.fixture.current.notify('browser.command', command()); await flush();
  assert.equal(calls, 1); assert.deepEqual(s.fixture.current.requests.at(-1)?.params.error, { kind: 'outcome_unknown', message: 'native reply lost' });
});

test('expired commands are rejected before native delivery', async t => {
  const s = setup(); t.after(() => s.client.close()); await s.client.connect(); await s.client.browser.select(offer, s.bridge);
  s.fixture.current.notify('browser.command', { ...command(), deadline_millis: '1' }); await flush();
  assert.equal(s.dispatched.length, 0); assert.equal((s.fixture.current.requests.at(-1)?.params.error as { kind: string }).kind, 'deadline_exceeded');
});

test('disconnect releases native authority; reconnect never rebinds automatically', async t => {
  const s = setup(); t.after(() => s.client.close()); await s.client.connect();
  const selection = await s.client.browser.select(offer, s.bridge);
  s.fixture.current.fail(new Error('lost transport')); await flush();
  assert.equal(selection.active, false); assert.deepEqual(s.released, [{ rootId: 'root', providerEpoch: 'epoch' }]);
  await s.client.connect(); await flush();
  assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.provider.bind').length, 0);
  s.fixture.current.notify('browser.command', command()); await flush(); assert.equal(s.dispatched.length, 0);
});

test('in-flight cancelled results are discarded, not replayed or sent under a new epoch', async t => {
  let finish!: (value: BrowserCommandResultParams) => void;
  const s = setup({ dispatch: () => new Promise(resolve => { finish = resolve; }) });
  t.after(() => s.client.close()); await s.client.connect(); await s.client.browser.select(offer, s.bridge);
  s.fixture.current.notify('browser.command', command()); await flush();
  s.fixture.current.notify('browser.command.cancel', { command_id: 'command', root_id: 'root', provider_epoch: 'epoch', attachment_generation: 'attachment-generation', reason: 'cancelled' });
  finish(result(command())); await flush();
  assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.command.result').length, 0);
});


test('a matching cancellation while result RPC is pending discards late rejection without releasing other attachments', async t => {
  let stalled!: { request: FixtureRequest; connection: FixtureConnection };
  const errors: Error[] = [];
  const s = setup({}, { request(request, connection) {
    if (request.method === 'browser.command.result' && request.params.command_id === 'command') { stalled = { request, connection }; return; }
    connection.reply(request, request.method === 'browser.provider.bind' ? provider : { accepted: true });
  } });
  t.after(() => s.client.close()); await s.client.connect();
  const selection = await s.client.browser.select(offer, s.bridge, { onError: error => errors.push(error) });
  s.fixture.current.notify('browser.command', command()); await flush();
  assert.ok(stalled, 'native result must already be waiting for its RPC acknowledgement');
  const cancel = { command_id: 'command', root_id: 'root', provider_epoch: 'epoch', attachment_generation: 'attachment-generation', reason: 'cancelled' };
  s.fixture.current.notify('browser.command.cancel', cancel); await flush();
  assert.deepEqual(s.cancelled, [cancel]);
  stalled.connection.error(stalled.request, 'conflict', -32009); await flush();
  assert.equal(selection.active, true); assert.deepEqual(s.released, []); assert.deepEqual(errors, []);
  s.fixture.current.notify('browser.command', command());
  const other = command('other-command');
  other.scope = { ...other.scope, tab_id: 'other-tab', tab_generation: 'other-tab-generation', attachment_id: 'other-attachment', attachment_generation: 'other-generation' };
  s.fixture.current.notify('browser.command', other); await flush();
  assert.deepEqual(s.dispatched.map(value => value.command_id), ['command', 'other-command']);
  assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.command.result' && request.params.command_id === 'command').length, 1, 'cancelled work is never replayed');
  assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.command.result' && request.params.command_id === 'other-command').length, 1);
  assert.equal(selection.active, true);
});

for (const mismatch of ['uncancelled', 'root_id', 'provider_epoch', 'command_id', 'attachment_generation'] as const) {
  test(`a result RPC rejection still fails closed for ${mismatch} cancellation`, async t => {
    let stalled!: { request: FixtureRequest; connection: FixtureConnection };
    const errors: Error[] = [];
    const s = setup({}, { request(request, connection) {
      if (request.method === 'browser.command.result') { stalled = { request, connection }; return; }
      connection.reply(request, request.method === 'browser.provider.bind' ? provider : { accepted: true });
    } });
    t.after(() => s.client.close()); await s.client.connect();
    const selection = await s.client.browser.select(offer, s.bridge, { onError: error => errors.push(error) });
    s.fixture.current.notify('browser.command', command()); await flush();
    assert.ok(stalled);
    if (mismatch !== 'uncancelled') {
      s.fixture.current.notify('browser.command.cancel', { command_id: 'command', root_id: 'root', provider_epoch: 'epoch', attachment_generation: 'attachment-generation', reason: 'cancelled', [mismatch]: 'wrong' });
    }
    assert.deepEqual(s.cancelled, []);
    stalled.connection.error(stalled.request, 'conflict', -32009); await flush();
    assert.equal(selection.active, false); assert.equal(s.released.length, 1); assert.equal(errors.length, 1);
    assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.provider.unbind').length, 1);
  });
}

test('late result rejection after explicit release cannot trigger teardown again or affect replacement', async t => {
  let stalled!: { request: FixtureRequest; connection: FixtureConnection };
  let bindings = 0;
  const errors: Error[] = [];
  const s = setup({}, { request(request, connection) {
    if (request.method === 'browser.command.result' && request.params.command_id === 'command') { stalled = { request, connection }; return; }
    connection.reply(request, request.method === 'browser.provider.bind' ? { ...provider, provider_epoch: ++bindings === 1 ? 'epoch' : 'replacement' } : { accepted: true });
  } });
  t.after(() => s.client.close()); await s.client.connect();
  const old = await s.client.browser.select(offer, s.bridge, { onError: error => errors.push(error) });
  s.fixture.current.notify('browser.command', command()); await flush(); assert.ok(stalled);
  await old.release();
  const replacement = await s.client.browser.select(offer, s.bridge);
  stalled.connection.error(stalled.request, 'conflict', -32009); await flush();
  assert.equal(old.active, false); assert.equal(replacement.active, true); assert.deepEqual(errors, []);
  assert.deepEqual(s.released, [{ rootId: 'root', providerEpoch: 'epoch' }]);
  const next = command('replacement-command'); next.provider_epoch = 'replacement'; next.scope.provider_epoch = 'replacement';
  s.fixture.current.notify('browser.command', next); await flush();
  assert.deepEqual(s.dispatched.map(value => value.command_id), ['command', 'replacement-command']);
  assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.provider.unbind').length, 1);
});

for (const interruption of ['timeout', 'abort', 'disconnect', 'revoke'] as const) {
  test(`missing native ACK settles on ${interruption}; late ACK cannot revoke a replacement`, { timeout: 2_000 }, async t => {
    let acknowledge!: () => void;
    const s = setup({ select: () => new Promise<void>(resolve => { acknowledge = resolve; }) });
    t.after(() => s.client.close()); await s.client.connect();
    const abort = new AbortController();
    const selecting = s.client.browser.select(offer, s.bridge, { signal: abort.signal, timeoutMs: interruption === 'timeout' ? 30 : 1_000 });
    const rejected = assert.rejects(selecting);
    await flush(); assert.equal(typeof acknowledge, 'function');
    s.fixture.current.notify('browser.command', command());
    if (interruption === 'abort') abort.abort();
    if (interruption === 'disconnect') s.fixture.current.fail(new Error('lost transport'));
    if (interruption === 'revoke') s.fixture.current.notify('browser.provider.revoked', { root_id: 'root', provider_id: 'provider', provider_epoch: 'epoch', reason: 'replaced' });
    await rejected; await flush();
    assert.equal(s.dispatched.length, 0);
    if (interruption === 'disconnect') await s.client.connect();
    s.fixture.reply('browser.provider.bind', () => ({ ...provider, provider_epoch: 'replacement' }));
    s.bridge.select = async () => {};
    const replacement = await s.client.browser.select(offer, s.bridge);
    acknowledge(); await flush();
    assert.equal(replacement.active, true);
    assert.ok(s.released.length > 0);
    assert.ok(s.released.every(value => (value as { providerEpoch: string }).providerEpoch === 'epoch'));
    assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.provider.unbind' && request.params.provider_epoch === 'replacement').length, 0);
    assert.equal(s.dispatched.length, 0);
  });
}

function observation(attachment = 'attachment', sequence = '1'): BrowserProviderEventParams {
  return { root_id: 'root', provider_epoch: 'epoch', tab_id: `tab-${attachment}`, tab_generation: 'tab-generation',
    attachment_id: attachment, attachment_generation: `generation-${attachment}`, sequence, document_revision: 'document',
    kind: 'cdp', method: 'Runtime.consoleAPICalled', params: { type: 'log' } };
}

for (const bound of ['count', 'bytes'] as const) {
  test(`native event ${bound} backlog is bounded and overflow visibly fails closed`, async t => {
    const events: FixtureRequest[] = [];
    const s = setup({}, { request(request, connection) {
      if (request.method === 'browser.provider.event') { events.push(request); return; }
      connection.reply(request, request.method === 'browser.provider.bind' ? provider : { accepted: true });
    } });
    const errors: Error[] = [];
    t.after(() => s.client.close()); await s.client.connect();
    const selection = await s.client.browser.select(offer, s.bridge, { onError: error => errors.push(error) });
    const body = bound === 'bytes' ? 'x'.repeat(250_000) : '';
    s.emit({ ...observation(), params: { body } }); await flush();
    assert.equal(events.length, 1); // First acknowledgement intentionally stalls.
    for (let i = 2; i <= (bound === 'count' ? 65 : 9); i++) s.emit({ ...observation('attachment', String(i)), params: { body } });
    await flush();
    assert.equal(selection.active, false); assert.equal(events.length, 1);
    assert.equal(errors.length, 1); assert.match(errors[0]!.message, /observation queue exceeded/);
    assert.equal(s.fixture.current.requests.filter(request => request.method === 'browser.provider.unbind').length, 1);
  });
}

test('typed stale attachment event retires only its exact queue; unrelated attachment stays active', async t => {
  let stalled!: { request: FixtureRequest; connection: FixtureConnection };
  const events: FixtureRequest[] = [];
  const s = setup({}, { request(request, connection) {
    if (request.method === 'browser.provider.event') {
      events.push(request);
      if (request.params.attachment_id === 'a') { stalled = { request, connection }; return; }
    }
    connection.reply(request, request.method === 'browser.provider.bind' ? provider : { accepted: true });
  } });
  t.after(() => s.client.close()); await s.client.connect();
  const selection = await s.client.browser.select(offer, s.bridge);
  s.emit(observation('a')); await flush();
  s.emit(observation('a', '2')); s.emit(observation('b'));
  stalled.connection.error(stalled.request, 'browser_event_stale', -32009); await flush();
  s.emit(observation('a', '3')); s.emit(observation('b', '2')); await flush();
  assert.deepEqual(events.map(request => [request.params.attachment_id, request.params.sequence]), [['a', '1'], ['b', '1'], ['b', '2']]);
  assert.equal(selection.active, true); assert.deepEqual(s.released, []);
});

test('event rejection other than typed stale attachment still fails the provider closed', async t => {
  const errors: Error[] = [];
  const s = setup({}, { request(request, connection) {
    if (request.method === 'browser.provider.event') { connection.error(request, 'permission_denied', -32009); return; }
    connection.reply(request, request.method === 'browser.provider.bind' ? provider : { accepted: true });
  } });
  t.after(() => s.client.close()); await s.client.connect();
  const selection = await s.client.browser.select(offer, s.bridge, { onError: error => errors.push(error) });
  s.emit(observation()); await flush();
  assert.equal(selection.active, false); assert.equal(errors.length, 1); assert.equal(s.released.length, 1);
});

for (const kind of ['unix', 'websocket'] as const) {
  test(`${kind} screenshots use connection-bound chunk RPC under the exact root/agent, never HTTP`, { timeout: 2_000 }, async t => {
    const bytes = new Uint8Array([0xff, 0xd8, 0xff, 1, 2, 3, 4, 5, 6]);
    const s = setup({ dispatch: async command => ({ ...result(command), screenshotBytes: bytes }) }, { kind });
    t.after(() => s.client.close());
    const http = t.mock.method(globalThis, 'fetch', async () => { throw new Error('Browser screenshot must not use HTTP'); });
    await s.client.connect(); await s.client.browser.select(offer, s.bridge);
    const handle = { reference_id: 'content', digest: await s.client.digestHex(bytes), size: String(bytes.length), media_type: 'image/jpeg' };
    const received: number[] = [];
    s.fixture.reply('upload.begin', request => {
      assert.equal(request.params.root_id, 'root'); assert.equal(request.params.agent_id, 'child');
      assert.equal(request.params.media_type, 'image/jpeg'); assert.equal(request.params.source, 'desktop-browser');
      assert.equal(request.params.expected_digest, handle.digest); assert.equal(request.params.size, handle.size);
      return { accepted: true };
    }).reply('upload.chunk', request => {
      assert.equal(request.params.offset, String(received.length));
      received.push(...Buffer.from(request.params.data as string, 'base64'));
      return { accepted: true };
    }).reply('upload.finish', () => handle);
    let finish!: (wire: Record<string, unknown>) => void;
    const finished = new Promise<Record<string, unknown>>(resolve => { finish = resolve; });
    s.fixture.reply('browser.command.result', request => { finish(request.params); return { accepted: true }; });
    s.fixture.current.notify('browser.command', { ...command(), agent_id: 'child' });
    const wire = await finished;
    assert.deepEqual(received, [...bytes]); assert.deepEqual(wire.screenshot, handle);
    assert.equal('screenshotBytes' in wire, false); assert.equal(http.mock.callCount(), 0);
    assert.deepEqual(s.fixture.current.requests.filter(request => request.method.startsWith('upload.')).map(request => request.method),
      ['upload.begin', 'upload.chunk', 'upload.chunk', 'upload.chunk', 'upload.finish']);
  });
}
