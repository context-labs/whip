import assert from 'node:assert/strict';
import test from 'node:test';
import { setTimeout as delay } from 'node:timers/promises';
import { manifest, type InitializeResult } from '@whip/protocol';
import { WhipClient } from '../src/client.js';
import type { Transport, TransportFactory, TransportHandlers } from '../src/transport.js';

type Request = { id: string; method: string; params: Record<string, unknown> };
interface Connection {
  requests: Request[];
  closed: boolean;
  reply(request: Request, result: unknown): void;
  error(request: Request, kind: string, code?: number): void;
  fail(error?: Error): void;
  notify(method: string, params: unknown): void;
}

function initialize(): InitializeResult {
  return {
    protocol_major: 2, protocol_minor: 1, runtime_id: 'runtime-fixture',
    connection_id: 'connection-fixture', host_platform: 'darwin', host_architecture: 'arm64',
    build_id: 'different-daemon-build', generation: '9007199254740993',
    capabilities: [], negotiated_capabilities: [], nonce: '',
    operations: manifest.operations.map(operation => ({ ...operation })),
    limits: {
      frame_bytes: 1 << 20, connections: 64, in_flight_requests: 2,
      outbound_messages: 1024, outbound_bytes: String(8 << 20), root_subscriptions: 16,
      content_chunk_bytes: 256 << 10, upload_bytes: String(64 << 20),
    },
  };
}

function harness(onRequest?: (request: Request, connection: Connection) => void) {
  const connections: Connection[] = [];
  const factory: TransportFactory = async (handlers: TransportHandlers): Promise<Transport> => {
    const connection: Connection = {
      requests: [], closed: false,
      reply: (request, result) => handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, result })),
      error: (request, kind, code = -32000) => handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: { code, message: kind, data: { kind } } })),
      fail: (error = new Error('socket lost')) => handlers.close(error),
      notify: (method, params) => handlers.message(JSON.stringify({ jsonrpc: '2.0', method, params })),
    };
    connections.push(connection);
    return {
      kind: 'unix', bufferedAmount: 0,
      send(text) {
        const request = JSON.parse(text) as Request;
        connection.requests.push(request);
        if (onRequest) onRequest(request, connection);
        else if (request.method === 'initialize') connection.reply(request, initialize());
      },
      close() { connection.closed = true; },
    };
  };
  return { factory, connections, get current() { return connections.at(-1)!; } };
}

test('an already-aborted connect does not create a transport', async t => {
  const server = harness();
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client' });
  t.after(() => client.close());
  await assert.rejects(client.connect({ signal: AbortSignal.abort() }), { name: 'AbortError' });
  assert.equal(server.connections.length, 0);
  assert.equal(client.getSnapshot().state, 'closed');
});

test('already-aborted recovered waits and retries do not query or submit work', async t => {
  const server = harness();
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const command = client.recover({ version: 1, runtimeId: 'runtime-fixture', clientId: 'client', commandId: 'absent', operation: 'submit', rootId: 'root' }, { text: 'original' });
  const signal = AbortSignal.abort();
  await assert.rejects(command.accepted({ signal }), { name: 'AbortError' });
  await assert.rejects(command.retry({ signal }), { name: 'AbortError' });
  // A rejected inner status promise must also be observed after the wait aborts.
  await delay(0);
  assert.equal(server.current.requests.length, 1);
});

test('Unix outgoing bounds include the newline framing byte', async t => {
  const info = initialize();
  info.limits.frame_bytes = 256;
  const server = harness((request, connection) => {
    if (request.method === 'initialize') connection.reply(request, info);
    if (request.method === 'daemon.ping') connection.reply(request, { generation: '1', build_id: 'fixture' });
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const envelope = JSON.stringify({ jsonrpc: '2.0', id: '2', method: 'daemon.ping', params: {} });
  await assert.rejects(client.callEncoded('daemon.ping', ' '.repeat(256 - envelope.length) + '{}'), { kind: 'resource_limit' });
  assert.equal(server.current.requests.length, 1);
  await client.call('daemon.ping', {});
  assert.equal(client.getSnapshot().state, 'connected');
});

test('initialization cannot resurrect a connection lost after its response', async t => {
  const server = harness((request, connection) => {
    if (request.method === 'initialize') {
      connection.reply(request, initialize());
      connection.fail();
    }
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await assert.rejects(client.connect());
  assert.equal(client.getSnapshot().state, 'closed');
  assert.equal(server.current.closed, true);
});

test('explicit close wins over an already resolved initialization response', async () => {
  let client: WhipClient;
  const server = harness((request, connection) => {
    if (request.method === 'initialize') {
      connection.reply(request, initialize());
      client.close();
    }
  });
  client = new WhipClient({ endpoint: server.factory, clientId: 'client', heartbeatIntervalMs: 1 });
  await assert.rejects(client.connect());
  await delay(10);
  assert.equal(client.getSnapshot().state, 'closed');
  assert.equal(server.current.closed, true);
  assert.equal(server.current.requests.length, 1);
});

test('aborting a query removes its waiter and a late response cannot resolve another request', async t => {
  const server = harness();
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const controller = new AbortController();
  const aborted = client.call('daemon.ping', {}, { signal: controller.signal });
  const first = server.current.requests.at(-1)!;
  controller.abort();
  await assert.rejects(aborted, { name: 'AbortError' });
  const next = client.call('daemon.ping', {});
  const second = server.current.requests.at(-1)!;
  assert.notEqual(second.id, first.id);
  server.current.reply(first, { generation: '1', build_id: 'late' });
  server.current.reply(second, { generation: '2', build_id: 'current' });
  assert.deepEqual(await next, { generation: '2', build_id: 'current' });
  assert.equal(client.getSnapshot().state, 'connected');
});

test('close rejects every pending request and applies in-flight bounds before sending', async t => {
  const server = harness();
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const first = client.call('daemon.ping', {});
  const second = client.call('daemon.ping', {});
  await assert.rejects(client.call('daemon.ping', {}), { kind: 'resource_limit' });
  assert.equal(server.current.requests.length, 3);
  client.close();
  await assert.rejects(first, { kind: 'closed' });
  await assert.rejects(second, { kind: 'closed' });
  assert.equal(client.getSnapshot().state, 'closed');
});

test('malformed known responses fail the connection instead of reaching callers', async t => {
  const server = harness();
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const pending = client.call('daemon.ping', {});
  server.current.reply(server.current.requests.at(-1)!, { generation: 9007199254740992, build_id: 'bad-counter' });
  await assert.rejects(pending, { kind: 'invalid_response' });
  assert.equal(client.getSnapshot().state, 'closed');
});

test('command status rejects foreign recovery namespaces and pre-aborted lookups before sending', async t => {
  const server = harness();
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const record = { version: 1 as const, runtimeId: 'runtime-fixture', clientId: 'another-client', commandId: 'id', operation: 'submit' as const, rootId: 'root' };
  await assert.rejects(client.commandStatus(record), { kind: 'invalid_arguments' });
  await assert.rejects(client.commandStatus({ ...record, clientId: 'client' }, { signal: AbortSignal.abort() }), { name: 'AbortError' });
  assert.equal(server.current.requests.length, 1);
});

test('subscription routes early events, drops duplicates, and exposes sequence gaps', async t => {
  const server = harness((request, connection) => {
    if (request.method === 'initialize') connection.reply(request, initialize());
    if (request.method === 'events.subscribe') {
      connection.notify('event', { event: { root_id: 'root', subscription_id: request.params.subscription_id, seq: '9007199254740994', kind: 'stream.text', payload: { text: 'early' } } });
      connection.reply(request, { subscription_id: request.params.subscription_id, cursor: request.params.cursor });
    }
    if (request.method === 'events.unsubscribe') connection.reply(request, {});
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const subscription = await client.events.subscribe('root', '9007199254740993');
  const first = await subscription.next();
  assert.equal(first.value?.seq, '9007199254740994');
  assert.equal(subscription.cursor, '9007199254740994');
  const envelope = { root_id: 'root', subscription_id: subscription.id, seq: '9007199254740994', kind: 'stream.text', payload: { text: 'duplicate' } };
  server.current.notify('event', { event: envelope });
  const waiting = subscription.next();
  server.current.notify('event', { event: { ...envelope, seq: '9007199254740996' } });
  await assert.rejects(waiting, { kind: 'resynchronization_required' });
  assert.equal(subscription.cursor, '9007199254740994');
  assert.equal(client.getSnapshot().state, 'connected');
  assert.equal(server.current.requests.at(-1)?.method, 'events.unsubscribe');
});

test('slow subscriptions fail explicitly and release their connection slot', async t => {
  const info = initialize();
  info.limits.root_subscriptions = 1;
  const server = harness((request, connection) => {
    if (request.method === 'initialize') connection.reply(request, info);
    if (request.method === 'events.subscribe') connection.reply(request, { subscription_id: request.params.subscription_id, cursor: request.params.cursor });
    if (request.method === 'events.unsubscribe') connection.reply(request, {});
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const subscription = await client.events.subscribe('root', '0', { maxMessages: 1 });
  await assert.rejects(client.events.subscribe('root', '0'), { kind: 'resource_limit' });
  for (const seq of ['1', '2']) server.current.notify('event', { event: { root_id: 'root', subscription_id: subscription.id, seq, kind: 'future.kind', payload: {} } });
  await assert.rejects(subscription.next(), { kind: 'resynchronization_required' });
  const replacement = await client.events.subscribe('root', '2');
  await replacement.dispose();
  assert.equal((await replacement.next()).done, true);
});

test('aborting subscription admission cleans up after the late acknowledgement', async t => {
  const server = harness((request, connection) => {
    if (request.method === 'initialize') connection.reply(request, initialize());
    if (request.method === 'events.unsubscribe') connection.reply(request, {});
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const controller = new AbortController();
  const creation = client.events.subscribe('root', '0', { signal: controller.signal });
  const request = server.current.requests.at(-1)!;
  controller.abort();
  await assert.rejects(creation, { name: 'AbortError' });
  const beforeAcknowledgement = server.current.requests.length;
  server.current.reply(request, { subscription_id: request.params.subscription_id, cursor: request.params.cursor });
  await delay(0);
  const unsubscriptions = server.current.requests.slice(beforeAcknowledgement).filter(value => value.method === 'events.unsubscribe');
  assert.equal(unsubscriptions.length, 1);
  assert.equal(unsubscriptions[0]?.params.subscription_id, request.params.subscription_id);
  assert.equal(client.getSnapshot().state, 'connected');
});

test('command requests are frozen before application recovery storage resolves', async t => {
  let stored: unknown;
  let release!: () => void;
  const storedPromise = new Promise<void>(resolve => { release = resolve; });
  const server = harness((request, connection) => {
    if (request.method === 'initialize') connection.reply(request, initialize());
    if (request.method === 'command.submit') connection.reply(request, { command_id: request.params.command_id, operation: request.params.operation, ingress_seq: '1', status: 'queued' });
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false,
    recoveryStorage: { list: async () => [], delete: async () => {}, put: async record => { stored = record; await storedPromise; } },
  });
  t.after(() => client.close());
  await client.connect();
  const payload = { text: 'original private prompt' };
  const command = client.submit('submit', payload, { rootId: 'root', commandId: 'stable-id' });
  payload.text = 'mutated after submission';
  await delay(0);
  assert.equal(server.current.requests.length, 1);
  assert.deepEqual(stored, { version: 1, runtimeId: 'runtime-fixture', clientId: 'client', commandId: 'stable-id', operation: 'submit', rootId: 'root' });
  release();
  await command.accepted();
  assert.deepEqual(server.current.requests.at(-1)?.params.payload, { text: 'original private prompt' });
});

test('cancel admission cannot overtake the original command during asynchronous storage', async t => {
  let release!: () => void;
  const storage = new Promise<void>(resolve => { release = resolve; });
  const server = harness((request, connection) => {
    if (request.method === 'initialize') connection.reply(request, initialize());
    if (request.method === 'command.submit') connection.reply(request, { command_id: request.params.command_id, operation: request.params.operation, ingress_seq: '1', status: 'queued' });
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false,
    recoveryStorage: { list: async () => [], delete: async () => {}, put: async record => { if (record.operation === 'submit') await storage; } },
  });
  t.after(() => client.close());
  await client.connect();
  const command = client.submit('submit', { text: 'hello' }, { rootId: 'root', commandId: 'original' });
  const cancellation = command.cancel();
  await delay(0);
  assert.equal(server.current.requests.length, 1);
  release();
  await cancellation.accepted();
  const submissions = server.current.requests.filter(request => request.method === 'command.submit');
  assert.deepEqual(submissions.map(request => request.params.operation), ['submit', 'cancel']);
  assert.deepEqual(submissions[1]?.params.payload, { target_command_id: 'original' });
});

test('retry cannot erase an authoritative changed-payload conflict', async t => {
  const server = harness((request, connection) => {
    if (request.method === 'initialize') connection.reply(request, initialize());
    if (request.method === 'command.submit') connection.error(request, 'conflict', -32009);
    if (request.method === 'command.status') connection.reply(request, { command_id: request.params.command_id, operation: 'submit', ingress_seq: '1', status: 'queued' });
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const command = client.submit('submit', { text: 'changed payload' }, { rootId: 'root', commandId: 'existing' });
  await assert.rejects(command.accepted(), { kind: 'conflict' });
  await assert.rejects(command.retry(), { kind: 'conflict' });
  assert.equal(server.current.requests.filter(request => request.method === 'command.submit').length, 1);
});

test('generic status errors never authorize resubmission', async t => {
  const server = harness((request, connection) => {
    if (request.method === 'initialize') connection.reply(request, initialize());
    if (request.method === 'command.status') connection.error(request, 'execution_failed');
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const command = client.recover({ version: 1, runtimeId: 'runtime-fixture', clientId: 'client', commandId: 'unknown', operation: 'submit', rootId: 'root' }, { text: 'original' });
  await assert.rejects(command.retry(), { kind: 'execution_failed' });
  assert.equal(server.current.requests.filter(request => request.method === 'command.submit').length, 0);
});

test('unknown event names cannot resolve inherited schema properties', async t => {
  const server = harness((request, connection) => {
    if (request.method === 'initialize') connection.reply(request, initialize());
    if (request.method === 'events.subscribe') connection.reply(request, { subscription_id: request.params.subscription_id, cursor: request.params.cursor });
    if (request.method === 'events.unsubscribe') connection.reply(request, {});
  });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const subscription = await client.events.subscribe('root', '0');
  for (const [index, kind] of ['future.kind', '__proto__', 'constructor'].entries()) {
    server.current.notify('event', { event: { root_id: 'root', subscription_id: subscription.id, seq: String(index + 1), kind, payload: { future: true } } });
    const event = await subscription.next();
    assert.equal(event.value?.unknown, true);
    assert.equal(event.value?.kind, kind);
    assert.equal(client.getSnapshot().state, 'connected');
  }
  await subscription.dispose();
});
