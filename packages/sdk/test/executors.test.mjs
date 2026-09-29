import assert from 'node:assert/strict';
import { once } from 'node:events';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import net from 'node:net';
import { join } from 'node:path';
import { test } from 'node:test';
import { ExecutorClient } from '../dist/index.js';
import { executorSocket } from '../dist/node.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };

test('executor connection pins runtime and carries exact generation/payload bytes without replay', async () => {
  const calls = [];
  let closes = 0;
  const transport = {
    async request(request) {
      calls.push(structuredClone(request));
      return { jsonrpc: '2.0', id: request.id, result: request.method === 'initialize' ? initial : request.method === 'executor.bind' ? fixture('ExecutorLease') : { accepted: true } };
    },
    events: (async function* () { yield fixture('ExecutorEvent'); })(),
    async close() { closes++; },
  };
  const client = await ExecutorClient.connect(transport, { expectedRuntimeID: 'runtime' });
  const lease = await client.bind(fixture('ExecutorBindParams'));
  assert.equal(lease.generation, '9007199254740993');
  const iterator = client.events();
  const event = (await iterator.next()).value;
  assert.equal(Buffer.from(event.invocation.arguments_base64, 'base64').toString(), '{"count":9007199254740993}');
  const result = fixture('ExecutorToolResultParams');
  result.output_base64 = Buffer.from('{"count":9007199254740993}').toString('base64');
  await client.result(result);
  assert.deepEqual(calls.at(-1).params, result);
  await assert.rejects(client.bind({ ...fixture('ExecutorBindParams'), tools: ['lookup', 'lookup'] }), TypeError);
  assert.equal(calls.length, 3);
  await iterator.return();
  assert.equal(closes, 1);
});

test('identity mismatch and malformed event close the peer instead of rebinding', async () => {
  let closes = 0;
  const transport = {
    async request(request) { return { jsonrpc: '2.0', id: request.id, result: initial }; },
    events: (async function* () { const event = fixture('ExecutorEvent'); event.invocation_id = 'foreign'; yield event; })(),
    async close() { closes++; },
  };
  await assert.rejects(ExecutorClient.connect(transport, { expectedRuntimeID: 'other' }), /identity mismatch/);
  const client = await ExecutorClient.connect(transport, { expectedRuntimeID: 'runtime' });
  await assert.rejects(client.events().next(), /identity mismatch/);
  assert.equal(closes, 2);
});

async function socketFixture(t, received) {
  const directory = await mkdtemp('/tmp/whip-executor-sdk-');
  const path = join(directory, 'socket');
  const sockets = new Set();
  const server = net.createServer(socket => {
    sockets.add(socket);
    socket.on('close', () => sockets.delete(socket));
    socket.on('error', () => {});
    let buffer = '';
    socket.on('data', chunk => {
      buffer += chunk;
      for (;;) {
        const end = buffer.indexOf('\n');
        if (end < 0) break;
        const request = JSON.parse(buffer.slice(0, end)); buffer = buffer.slice(end + 1);
        received(socket, request);
      }
    });
  });
  server.listen(path); await once(server, 'listening');
  t.after(async () => {
    for (const socket of sockets) socket.destroy();
    await new Promise(resolve => server.close(resolve));
    await rm(directory, { recursive: true, force: true });
  });
  return path;
}

test('persistent Unix adapter multiplexes replies/events and joins explicit closure', async t => {
  let connections = 0;
  let previous;
  const path = await socketFixture(t, (socket, request) => {
    if (socket !== previous) { connections++; previous = socket; }
    socket.write(JSON.stringify({ jsonrpc: '2.0', id: request.id, result: request.method === 'initialize' ? initial : fixture('ExecutorLease') }) + '\n');
    if (request.method === 'executor.bind') socket.write(JSON.stringify(fixture('ExecutorEvent')) + '\n');
  });
  const transport = await executorSocket(path);
  const client = await ExecutorClient.connect(transport, { expectedRuntimeID: 'runtime' });
  await client.bind(fixture('ExecutorBindParams'));
  const events = client.events();
  assert.equal((await events.next()).value.invocation.lease.generation, '9007199254740993');
  assert.equal(connections, 1);
  await client.close();
  assert.equal((await events.next()).done, true);
  await assert.rejects(client.pending(fixture('ExecutorPendingParams')), /closed/);
});

test('unread events and request cancellation fail the peer without reconnect', async t => {
  let overflowClosed;
  const closed = new Promise(resolve => { overflowClosed = resolve; });
  const path = await socketFixture(t, (socket, request) => {
    socket.write(JSON.stringify({ jsonrpc: '2.0', id: request.id, result: initial }) + '\n');
    socket.once('close', overflowClosed);
    for (let index = 0; index < 33; index++) socket.write(JSON.stringify(fixture('ExecutorEvent')) + '\n');
  });
  const transport = await executorSocket(path);
  await ExecutorClient.connect(transport, { expectedRuntimeID: 'runtime' });
  await closed;
  await assert.rejects(transport.events[Symbol.asyncIterator]().next(), /queue exceeds limit/);
  await transport.close();

  const silent = await socketFixture(t, () => {});
  const peer = await executorSocket(silent);
  const controller = new AbortController();
  const pending = peer.request({ jsonrpc: '2.0', id: 'one', method: 'initialize', params: { major: 4 } }, { signal: controller.signal });
  controller.abort(new Error('cancelled peer'));
  await assert.rejects(pending, /cancelled peer/);
  await peer.close();
});
