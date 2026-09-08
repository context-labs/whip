import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import {
  assertValid, manifest, rpcOperations, runtimeOperations,
  type CommandOperation, type ContractTypes, type EphemeralOperation,
  type InitializeResult, type QueryOperation, type RpcMethod, type RpcMethods,
  type RuntimeOperation, type RuntimeOperations,
} from '@whip/protocol';
import { WhipClient } from '../src/client.js';
import type { RecoveryRecord } from '../src/command.js';
import type { TransportFactory } from '../src/transport.js';
import { transportFixture } from './transport-fixture.js';

interface WireRequest {
  id: string;
  method: RpcMethod;
  params: Record<string, unknown>;
}

// This test reads the actual Go-produced fixture artifact. It runs after tsc, so
// the relative path is from packages/sdk/build/test rather than this source file.
const fixtures = JSON.parse(await readFile(new URL('../../../protocol/schema/fixtures.json', import.meta.url), 'utf8')) as { type: keyof ContractTypes; value: unknown }[];
function fixture<T extends keyof ContractTypes>(type: T): ContractTypes[T] {
  const value: unknown = structuredClone(fixtures.find(item => item.type === type)?.value);
  assertValid(type, value);
  return value;
}

function initialization(): InitializeResult {
  return {
    ...fixture('InitializeResult'),
    protocol_major: manifest.major, protocol_minor: manifest.minor,
    runtime_id: 'registry-runtime', connection_id: 'registry-connection', generation: '1',
    operations: manifest.operations.map(operation => ({ ...operation })),
    limits: {
      frame_bytes: 1 << 20, connections: 64, in_flight_requests: 32,
      outbound_messages: 1024, outbound_bytes: String(8 << 20), root_subscriptions: 16,
      content_chunk_bytes: 256 << 10, upload_bytes: String(64 << 20),
    },
  };
}

test('protocol 4.0 session usage does not disconnect the current client', async t => {
  const snapshot = fixture('RootSnapshot');
  // Do not type this older wire value as current usage: that would hide a
  // regression if a future additive field accidentally becomes required.
  const usage = { prompt_tokens: 17, completion_tokens: 3 };
  const response = { ...snapshot, root_id: 'root', messages: [{ role: 'assistant', content: 'Retained response', usage }] };
  delete response.accounting;
  const server = transportFixture({ request(request, connection) {
    if (request.method === 'root.snapshot') connection.reply(request, response);
    else if (request.method === 'daemon.ping') connection.reply(request, { generation: '1', build_id: 'legacy' });
  } });
  const client = new WhipClient({ endpoint: server.factory, clientId: 'legacy-reader', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  assert.equal(client.getSnapshot().info?.protocol_minor, 0);
  const root = await client.session('root').snapshot();
  assert.deepEqual(root.messages?.[0]?.usage, usage);
  assert.equal(root.accounting, undefined);
  await client.call('daemon.ping', {});
  assert.equal(client.getSnapshot().state, 'connected');
});

function rpcParams<M extends RpcMethod>(method: M): RpcMethods[M]['params'] {
  const params = fixture(rpcOperations[method].params_type);
  // The three dispatcher RPCs require valid nested operations in addition to
  // their outer Go wire shape. Their nested payloads also come from Go fixtures.
  if (method === 'command.submit') {
    return { command_id: 'raw-command', scope: 'root', root_id: 'root', operation: 'submit', payload: fixture('SubmitPayload') } as RpcMethods[M]['params'];
  }
  if (method === 'query') {
    return { root_id: 'root', operation: 'session.model.get', payload: fixture(runtimeOperations['session.model.get'].params_type) } as RpcMethods[M]['params'];
  }
  if (method === 'operation.invoke') {
    return { root_id: 'root', operation: 'terminal.input', payload: fixture(runtimeOperations['terminal.input'].params_type) } as RpcMethods[M]['params'];
  }
  return params as RpcMethods[M]['params'];
}

test('every generated operation dispatches through the SDK with its execution classification', async t => {
  const rpcHits = new Set<RpcMethod>();
  const runtimeHits = new Map<RuntimeOperation, string>();
  const writes: RecoveryRecord[] = [];
  const factory: TransportFactory = async handlers => ({
    kind: 'unix', bufferedAmount: 0, close() {},
    send(text) {
      const request = JSON.parse(text) as WireRequest;
      rpcHits.add(request.method);
      assertValid(rpcOperations[request.method].params_type, request.params);
      let result: unknown;
      if (request.method === 'initialize') result = initialization();
      else if (['command.submit', 'query', 'operation.invoke'].includes(request.method)) {
        const operation = request.params.operation as RuntimeOperation;
        const metadata = runtimeOperations[operation];
        assert.ok(metadata, `registered nested operation ${operation}`);
        assertValid(metadata.params_type, request.params.payload);
        const expectedMethod = metadata.execution === 'command' ? 'command.submit' : metadata.execution === 'query' ? 'query' : 'operation.invoke';
        assert.equal(request.method, expectedMethod, operation);
        runtimeHits.set(operation, request.method);
        const value = fixture(metadata.result_type);
        result = request.method === 'command.submit'
          ? { operation, command_id: request.params.command_id, ingress_seq: '9007199254740993', status: 'succeeded', result: value }
          : { result: value, root_id: 'root' };
      } else result = fixture(rpcOperations[request.method].result_type);
      handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, result }));
    },
  });
  const client = new WhipClient({
    endpoint: factory, clientId: 'registry-client', reconnect: false,
    recoveryStorage: {
      list: async () => writes,
      put: async record => { writes.push(record); },
      delete: async () => {},
    },
  });
  t.after(() => client.close());
  await client.connect();

  for (const method of Object.keys(rpcOperations) as RpcMethod[]) {
    await t.test(`rpc ${method}`, async () => {
      const before = writes.length;
      const result = await client.call(method, rpcParams(method));
      assertValid(rpcOperations[method].result_type, result, 'response');
      if (rpcOperations[method].execution !== 'command') assert.equal(writes.length, before, `${method} must not persist recovery input`);
    });
  }

  for (const operation of Object.keys(runtimeOperations) as RuntimeOperation[]) {
    await t.test(`runtime ${operation}`, async () => {
      const metadata = runtimeOperations[operation];
      const params = fixture(metadata.params_type);
      const expected = fixture(metadata.result_type);
      const before = writes.length;
      if (metadata.execution === 'query') {
        const result = await client.query(operation as QueryOperation, params as RuntimeOperations[QueryOperation]['params'], { rootId: 'root' });
        assert.deepEqual(result.result, expected);
        assert.equal(writes.length, before);
      } else if (metadata.execution === 'ephemeral') {
        const result = await client.invoke(operation as EphemeralOperation, params as RuntimeOperations[EphemeralOperation]['params'], { rootId: 'root' });
        assert.deepEqual(result.result, expected);
        assert.equal(writes.length, before);
      } else {
        assert.equal(metadata.execution, 'command', `new execution classification requires explicit SDK coverage: ${operation}`);
        const handle = client.submit(operation as CommandOperation, params as RuntimeOperations[CommandOperation]['params'], { rootId: 'root', commandId: `registry:${operation}` });
        const outcome = await handle.result();
        assert.equal(outcome.status, 'succeeded');
        assert.deepEqual(outcome.result, expected);
        assert.equal(writes.length, before + 1);
        assert.deepEqual(writes.at(-1), {
          version: 1, runtimeId: 'registry-runtime', clientId: 'registry-client',
          commandId: `registry:${operation}`, operation, rootId: 'root',
        });
      }
    });
  }

  assert.deepEqual([...rpcHits].sort(), Object.keys(rpcOperations).sort());
  assert.deepEqual([...runtimeHits.keys()].sort(), Object.keys(runtimeOperations).sort());
  assert.equal(runtimeHits.get('permission.mode'), 'command.submit');
  assert.equal(runtimeHits.get('terminal.input'), 'operation.invoke');
});

// Compile this against the real public SDK methods, not stand-in declarations.
// It is deliberately never executed: invalid calls must fail at compile time.
function classificationTypes(client: WhipClient): void {
  const result: Promise<RpcMethods['daemon.ping']['result']> = client.call('daemon.ping', {});
  void result;
  client.query('session.model.get', {});
  client.submit('submit', { text: 'hello' });
  client.invoke('terminal.input', { id: 'terminal', bytes: 'aGk=' });
  // @ts-expect-error A read-only query cannot enter the durable journal.
  client.submit('session.model.get', {});
  // @ts-expect-error Ephemeral terminal bytes cannot enter the durable journal.
  client.submit('terminal.input', { id: 'terminal', bytes: 'aGk=' });
  // @ts-expect-error Durable work cannot be sent through the query interface.
  client.query('submit', { text: 'hello' });
  // @ts-expect-error Durable work cannot use the ephemeral interface.
  client.invoke('submit', { text: 'hello' });
  // @ts-expect-error Permission modes use durable runtime operations, not a separate RPC.
  client.call('permission.mode', { enabled: true });
}
void classificationTypes;
