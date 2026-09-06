import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { writeFile } from 'node:fs/promises';
import { createWhipClient, createWebCryptoSigner, webSocket } from '../dist/index.js';
import { unixSocket } from '../dist/node.js';
import { createSessionView } from '../dist/state.js';
import { eventually, startFixture } from '../scripts/fixture.mjs';

let fixture;
let reconnectRecoveryMs;
const clients = new Set();
before(async () => { fixture = await startFixture(); });
after(async () => {
  for (const client of clients) client.close();
  await fixture?.close();
});

async function connect(transport, clientId = crypto.randomUUID(), options = {}) {
  const client = createWhipClient({
    endpoint: transport === 'unix' ? unixSocket(fixture.info.socket) : fixture.info.endpoint,
    clientId, clientKind: 'automation', ...options,
  });
  clients.add(client);
  await client.connect();
  return client;
}

const deadline = () => ({ signal: AbortSignal.timeout(15_000) });
async function createRoot(client) {
  const outcome = await client.submit('session.create', {
    kind: 'agent', cwd: fixture.directory, model: 'model', provider: 'provider',
  }).result(deadline());
  assert.equal(outcome.status, 'succeeded');
  return outcome.result.root_id;
}

for (const transport of ['unix', 'websocket']) {
  test(`${transport}: commands, deduplication, history, and structured errors`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    const commandId = crypto.randomUUID();
    const command = client.submit('submit', { text: `${transport} round trip` }, { rootId, commandId });
    const accepted = await command.accepted(deadline());
    assert.match(accepted.ingress_seq, /^\d+$/);
    const result = await command.result(deadline());
    assert.equal(result.status, 'succeeded');
    assert.deepEqual(result.result, { text: `${transport} round trip` });
    const duplicate = await client.submit('submit', { text: `${transport} round trip` }, { rootId, commandId }).result(deadline());
    assert.equal(duplicate.ingress_seq, accepted.ingress_seq);
    const changed = client.submit('submit', { text: 'changed request' }, { rootId, commandId });
    await assert.rejects(changed.accepted(deadline()), error => error.kind === 'conflict');
    await assert.rejects(client.call('command.status', { command_id: 'not-present' }), error => error.kind === 'command_not_found');
    const snapshot = await client.call('root.snapshot', { root_id: rootId });
    assert.equal(snapshot.messages.length, 2);
    assert.equal(typeof snapshot.cursor, 'string');
    const history = await client.call('history.page', {
      root_id: rootId, agent_id: rootId, through_seq: -1, limit: 128, max_bytes: 524_288,
    });
    assert.equal(history.messages.length, 2);
    assert.equal(typeof history.history_revision, 'string');
    const replay = await client.call('events.replay', { root_id: rootId, cursor: '0', limit: 1000 });
    assert.equal(replay.latest, snapshot.cursor);
    await assert.rejects(client.call('events.replay', { root_id: rootId, cursor: (BigInt(replay.latest) + 1n).toString(), limit: 100 }), error => error.kind === 'resynchronization_required');
    assert.equal((await fixture.effects()).filter(value => value === `${transport} round trip`).length, 1);
    client.close();
  });

  test(`${transport}: detaching and aborting a waiter do not cancel accepted work`, async () => {
    const clientId = crypto.randomUUID();
    const client = await connect(transport, clientId);
    const rootId = await createRoot(client);
    const key = crypto.randomUUID();
    const commandId = crypto.randomUUID();
    const command = client.submit('submit', { text: `hold:${key}` }, { rootId, commandId });
    await command.accepted(deadline());
    await eventually(async () => (await fixture.effects()).includes(`hold:${key}`));
    const controller = new AbortController();
    const waiting = command.result({ signal: controller.signal });
    controller.abort();
    await assert.rejects(waiting);
    assert.equal((await command.status()).status, 'running');
    client.close();
    const opposite = transport === 'unix' ? 'websocket' : 'unix';
    const reattached = await connect(opposite, clientId);
    assert.equal((await reattached.call('command.status', { command_id: commandId })).status, 'running');
    await fixture.release(key);
    const result = await eventually(async () => {
      const status = await reattached.call('command.status', { command_id: commandId });
      return status.status === 'succeeded' && status;
    });
    assert.equal(result.result.text, `hold:${key}`);
    assert.equal((await fixture.effects()).filter(value => value === `hold:${key}`).length, 1);
    reattached.close();
  });

  test(`${transport}: stale cancellation cannot cancel the next turn`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    const firstId = crypto.randomUUID();
    await client.submit('submit', { text: 'completed turn' }, { rootId, commandId: firstId }).result(deadline());
    const key = crypto.randomUUID();
    const current = client.submit('submit', { text: `hold:${key}` }, { rootId });
    await current.accepted(deadline());
    await eventually(async () => (await fixture.effects()).includes(`hold:${key}`));
    const stale = await client.submit('cancel', { target_command_id: firstId }, { rootId }).result(deadline());
    assert.notEqual(stale.status, 'succeeded');
    assert.equal((await current.status()).status, 'running');
    await fixture.release(key);
    assert.equal((await current.result(deadline())).status, 'succeeded');
    client.close();
  });

  test(`${transport}: actual lost acceptance acknowledgement recovers by identity`, async () => {
    const factory = transport === 'unix' ? unixSocket(fixture.info.socket) : webSocket(fixture.info.endpoint);
    let discarded = false;
    let submissionCount = 0;
    const endpoint = async (handlers, signal) => {
      let acceptedRequestId;
      const connection = await factory({
        ...handlers,
        message(message) {
          const envelope = JSON.parse(message);
          if (!discarded && envelope.id === acceptedRequestId && envelope.result) {
            discarded = true;
            connection.close();
          } else handlers.message(message);
        },
      }, signal);
      return {
        ...connection,
        get bufferedAmount() { return connection.bufferedAmount; },
        send(message) {
          const envelope = JSON.parse(message);
          if (envelope.method === 'command.submit' && envelope.params.operation === 'submit') {
            acceptedRequestId = envelope.id;
            submissionCount++;
          }
          connection.send(message);
        },
      };
    };
    const client = await connect(transport, crypto.randomUUID(), { endpoint });
    const rootId = await createRoot(client);
    const text = 'lost acknowledgement ' + crypto.randomUUID();
    const command = client.submit('submit', { text }, { rootId });
    await command.accepted(deadline()).catch(error => assert.equal(error.kind, 'delivery_uncertain'));
    await eventually(() => client.getSnapshot().state === 'connected');
    assert.equal((await command.result(deadline())).status, 'succeeded');
    assert.ok(discarded);
    assert.equal(submissionCount, 1, 'An accepted command must be looked up, not submitted again');
    assert.equal((await fixture.effects()).filter(value => value === text).length, 1);
    client.close();
  });

  test(`${transport}: slow subscriptions fail explicitly without killing the connection`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    const snapshot = await client.call('root.snapshot', { root_id: rootId });
    const stream = await client.events.subscribe(rootId, snapshot.cursor, { maxMessages: 1 });
    let seen = 0;
    const off = client.onEvent(event => { if (event.root_id === rootId) seen++; });
    try {
      assert.equal((await client.session(rootId).submit({ text: 'overflow stream' }).result(deadline())).status, 'succeeded');
      await eventually(() => seen >= 2);
      await assert.rejects(stream.next(), error => error.kind === 'resynchronization_required');
      assert.equal(typeof (await client.call('daemon.ping', {})).generation, 'string');
      const fresh = await client.call('root.snapshot', { root_id: rootId });
      const replacement = await client.events.subscribe(rootId, fresh.cursor);
      await replacement.dispose();
    } finally { off(); await stream.dispose(); client.close(); }
  });

  test(`${transport}: bounded content crosses chunks and preserves root grants`, async () => {
    const client = await connect(transport);
    const rootId = await createRoot(client);
    const otherRoot = await createRoot(client);
    const data = new TextEncoder().encode('content '.repeat(45_000));
    const content = await client.upload(data, { rootId, mediaType: 'text/plain' });
    assert.equal(content.handle.size, String(data.byteLength));
    assert.deepEqual(await content.readBytes({ maxBytes: data.byteLength }), data);
    await assert.rejects(content.readBytes({ maxBytes: 1024 }));
    await assert.rejects(client.content(content.handle, { rootId: otherRoot }).readBytes({ maxBytes: data.byteLength }));
    client.close();
  });

  test(`${transport}: signed human enrollment and parallel nonce rotation use exact bytes`, async () => {
    const key = await crypto.subtle.generateKey('Ed25519', true, ['sign', 'verify']);
    const signer = createWebCryptoSigner(key.privateKey);
    const terminalKey = await crypto.subtle.importKey('pkcs8', Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), Buffer.alloc(32, 7)]), 'Ed25519', false, ['sign']);
    const client = await connect(transport, crypto.randomUUID(), { clientKind: 'human', signer });
    const publicKey = new Uint8Array(await crypto.subtle.exportKey('raw', key.publicKey));
    await client.permissions.enroll(publicKey, 'sdk-terminal-fixture', createWebCryptoSigner(terminalKey));
    assert.equal((await client.permissions.status()).paired, true);
    const rootId = await createRoot(client);
    const outcomes = await Promise.all([
      client.permissions.setMode(rootId, false, { commandId: 'literal <tag> & café ' + crypto.randomUUID() }),
      client.permissions.setMode(rootId, true, { commandId: 'next nonce ' + crypto.randomUUID() }),
    ]);
    for (const outcome of outcomes) {
      const handle = client.recover({ version: 1, runtimeId: fixture.info.runtime_id, clientId: client.clientId, commandId: outcome.command_id, operation: 'permission.mode', rootId });
      assert.equal((await handle.result(deadline())).status, 'succeeded');
    }
    const automation = await connect(transport, crypto.randomUUID(), { signer });
    await assert.rejects(automation.permissions.setMode(rootId, false), error => error.kind === 'permission_denied');
    automation.close(); client.close();
  });

  test(`${transport}: view converges, shares a subscription, and invalidates destructive history`, async () => {
    const writer = await connect(transport);
    const rootId = await createRoot(writer);
    const observer = await connect(transport === 'unix' ? 'websocket' : 'unix');
    const view = createSessionView(observer.session(rootId), { maxBytes: 16_384, maxMessages: 4 });
    const unsubscribeA = view.subscribe(() => {});
    const unsubscribeB = view.subscribe(() => {});
    try {
      await view.start();
      assert.equal(view.getSnapshot().status, 'live');
      assert.strictEqual(view.getSnapshot(), view.getSnapshot());
      assert.ok(Object.isFrozen(view.getSnapshot()));
      const extra = [];
      try {
        for (let index = 0; index < 15; index++) extra.push(await observer.events.subscribe(rootId, view.getSnapshot().root.cursor));
        await assert.rejects(observer.events.subscribe(rootId, view.getSnapshot().root.cursor), error => error.kind === 'resource_limit');
      } finally {
        await Promise.all(extra.map(stream => stream.dispose()));
      }
      const key = crypto.randomUUID();
      const command = writer.session(rootId).submit({ text: `hold:${key}` });
      await command.accepted(deadline());
      await eventually(() => view.getSnapshot().root.presentation?.some(event => event.kind === 'stream.text' && event.payload.text === `hold:${key}`));
      await fixture.release(key);
      assert.equal((await command.result(deadline())).status, 'succeeded');
      await eventually(() => view.getSnapshot().history[rootId]?.messages.length === 2);
      const revision = view.getSnapshot().history[rootId].revision;
      assert.equal((await writer.submit('history.clear', {}, { rootId }).result(deadline())).status, 'succeeded');
      await eventually(() => view.getSnapshot().history[rootId]?.revision !== revision && view.getSnapshot().history[rootId]?.messages.length === 0);
      await assert.rejects(observer.call('history.page', { root_id: rootId, agent_id: rootId, through_seq: -1, revision, limit: 128, max_bytes: 524_288 }), error => error.kind === 'resynchronization_required');
      for (let round = 0; round < 4; round++) await writer.session(rootId).submit({ text: 'bounded ' + round }).result(deadline());
      await eventually(() => view.getSnapshot().history[rootId]?.messages.length === 4);
      assert.ok(view.getSnapshot().retainedBytes <= 16_384);
      assert.ok(view.getSnapshot().history[rootId].messages.length <= 4);
    } finally {
      unsubscribeA(); unsubscribeB();
      await view.dispose();
      observer.close(); writer.close();
    }
  });
}

test('application recovery storage holds only identity metadata and fences admission failure', async () => {
  const records = new Map();
  const storage = {
    async list() { return [...records.values()]; },
    async put(record) { records.set(record.commandId, structuredClone(record)); },
    async delete(record) { records.delete(record.commandId); },
  };
  const clientId = crypto.randomUUID();
  const client = await connect('unix', clientId, { recoveryStorage: storage });
  const rootId = await createRoot(client);
  const text = 'private request text ' + crypto.randomUUID();
  const command = client.session(rootId).submit({ text });
  assert.equal((await command.result(deadline())).status, 'succeeded');
  const record = records.get(command.commandId);
  assert.deepEqual(Object.keys(record).sort(), ['clientId', 'commandId', 'operation', 'rootId', 'runtimeId', 'version']);
  assert.ok(!JSON.stringify(await storage.list()).includes(text));
  client.close();
  const recovery = await connect('websocket', clientId, { recoveryStorage: storage });
  assert.equal((await recovery.recover(record).result(deadline())).result.text, text);
  const missing = { ...record, commandId: crypto.randomUUID() };
  await assert.rejects(recovery.recover(missing).retry(), error => error.kind === 'recovery_required');
  const explicit = recovery.recover(missing, { text: 'explicit original retry' });
  await explicit.retry();
  assert.equal((await explicit.result(deadline())).status, 'succeeded');
  recovery.close();

  const failing = await connect('unix', crypto.randomUUID(), { recoveryStorage: { ...storage, async put() { throw new Error('storage unavailable'); } } });
  const neverSent = failing.submit('submit', { text: 'must never execute' }, { rootId });
  await assert.rejects(neverSent.accepted(), /storage unavailable/);
  await assert.rejects(failing.call('command.status', { command_id: neverSent.commandId }), error => error.kind === 'command_not_found');
  assert.ok(!(await fixture.effects()).includes('must never execute'));
  failing.close();
});

test('configuration revision conflicts preserve the winning host update', async () => {
  const unix = await connect('unix');
  const websocket = await connect('websocket');
  const initial = await unix.configuration.get();
  const results = await Promise.allSettled([
    unix.configuration.update({ revision: initial.revision, max_retries: initial.max_retries + 1 }),
    websocket.configuration.update({ revision: initial.revision, max_retries: initial.max_retries + 2 }),
  ]);
  const successful = results.filter(result => result.status === 'fulfilled');
  const failed = results.filter(result => result.status === 'rejected');
  assert.equal(successful.length, 1);
  assert.equal(failed.length, 1);
  assert.equal(failed[0].reason.kind, 'conflict');
  assert.deepEqual(await websocket.configuration.get(), successful[0].value);
  assert.notEqual(successful[0].value.revision, initial.revision);
  unix.close(); websocket.close();
});

test('actual process crash preserves outcomes and never repeats uncertain effects', async () => {
  const clientId = crypto.randomUUID();
  const client = await connect('unix', clientId, { reconnect: false });
  const rootId = await createRoot(client);
  const key = crypto.randomUUID();
  const commandId = crypto.randomUUID();
  const command = client.submit('submit', { text: `hold:${key}` }, { rootId, commandId });
  const accepted = await command.accepted(deadline());
  await eventually(async () => (await fixture.effects()).includes(`hold:${key}`));
  const observer = await connect('websocket');
  const view = createSessionView(observer.session(rootId));
  await view.start();
  assert.equal(view.getSnapshot().status, 'live');
  const previousRuntime = fixture.info.runtime_id;
  const previousGeneration = fixture.info.generation;
  const recoveryStarted = performance.now();
  await fixture.crashAndRestart();
  assert.equal(fixture.info.runtime_id, previousRuntime);
  assert.equal(fixture.info.generation, previousGeneration + 1);
  for (const transport of ['unix', 'websocket']) {
    const recovery = await connect(transport, clientId);
    const status = await recovery.call('command.status', { command_id: commandId });
    assert.equal(status.status, 'interrupted');
    assert.equal(status.ingress_seq, accepted.ingress_seq);
    assert.ok(status.failure);
    const duplicate = await recovery.submit('submit', { text: `hold:${key}` }, { rootId, commandId }).result(deadline());
    assert.equal(duplicate.status, 'interrupted');
    recovery.close();
  }
  try {
    await eventually(() => observer.getSnapshot().state === 'connected' && observer.getSnapshot().info.generation === String(previousGeneration + 1));
    const authoritative = await observer.call('root.snapshot', { root_id: rootId });
    await eventually(() => view.getSnapshot().status === 'live' && BigInt(view.getSnapshot().root.cursor) >= BigInt(authoritative.cursor));
    assert.deepEqual(view.getSnapshot().root.messages, authoritative.messages);
    assert.deepEqual(view.getSnapshot().root.active_turns, authoritative.active_turns);
    reconnectRecoveryMs = performance.now() - recoveryStarted;
  } finally { await view.dispose(); observer.close(); }
  assert.equal((await fixture.effects()).filter(value => value === `hold:${key}`).length, 1);
  client.close();
});

test('measure SDK admission and completion with four concurrent roots', async t => {
  const admission = [], completion = [], eventToView = [], eventDelivery = [], retainedBytes = [];
  let statusRequests = 0;
  await Promise.all(Array.from({ length: 4 }, async (_, index) => {
    const transport = index % 2 ? 'unix' : 'websocket';
    const factory = transport === 'unix' ? unixSocket(fixture.info.socket) : webSocket(fixture.info.endpoint);
    const endpoint = async (handlers, signal) => {
      const connection = await factory(handlers, signal);
      return { ...connection, get bufferedAmount() { return connection.bufferedAmount; }, send(message) {
        if (JSON.parse(message).method === 'command.status') statusRequests++;
        connection.send(message);
      } };
    };
    const client = await connect(transport, crypto.randomUUID(), { endpoint });
    const rootId = await createRoot(client);
    const view = createSessionView(client.session(rootId));
    const arrivals = new Map();
    let start;
    let firstEvent = false;
    const offEvent = client.onEvent(event => {
      if (event.root_id !== rootId || !event.kind.startsWith('stream.')) return;
      arrivals.set(event.seq, performance.now());
      if (firstEvent) { eventDelivery.push(performance.now() - start); firstEvent = false; }
    });
    const offView = view.subscribe(() => {
      const state = view.getSnapshot();
      if (!state.root) return;
      for (const [seq, arrival] of arrivals) {
        if (BigInt(seq) <= BigInt(state.root.cursor)) { eventToView.push(performance.now() - arrival); arrivals.delete(seq); }
      }
      retainedBytes.push(state.retainedBytes);
    });
    try {
      await view.start();
      for (let round = 0; round < 8; round++) {
        start = performance.now();
        firstEvent = true;
        const text = `measure ${index}/${round}`;
        const command = client.session(rootId).submit({ text });
        await command.accepted(deadline());
        admission.push(performance.now() - start);
        assert.equal((await command.result(deadline())).status, 'succeeded');
        completion.push(performance.now() - start);
        await eventually(() => view.getSnapshot().history[rootId]?.messages.at(-1)?.message?.content === text);
      }
      await eventually(() => arrivals.size === 0);
    } finally {
      offEvent(); offView(); await view.dispose(); client.close();
    }
  }));
  const summarize = values => {
    values.sort((a, b) => a - b);
    return { samples: values.length, p50_ms: values[Math.floor(values.length / 2)], p95_ms: values[Math.floor(values.length * 0.95)] };
  };
  assert.ok(eventToView.length > 0 && eventDelivery.length > 0);
  const measurements = {
    concurrent_roots: 4, admission: summarize(admission), completion: summarize(completion),
    submit_to_stream: summarize(eventDelivery), event_to_view: summarize(eventToView),
    max_view_retained_bytes: Math.max(...retainedBytes), command_status_requests: statusRequests,
    crash_to_recovered_view_ms: reconnectRecoveryMs,
    durability: 'SQLite WAL / synchronous=NORMAL',
  };
  t.diagnostic(JSON.stringify(measurements));
  if (process.env.WHIP_SDK_MEASUREMENTS) await writeFile(process.env.WHIP_SDK_MEASUREMENTS, JSON.stringify(measurements, null, 2) + '\n');
});
