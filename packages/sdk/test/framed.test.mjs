import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, framedTransport, DeliveryError } from '../dist/index.js';

const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const response = (id, result) => JSON.stringify({ jsonrpc: '2.0', id, result });
function connector(respond, kind = 'unix', options) {
  const connections = [];
  const open = async handlers => {
    const connection = {
      kind, bufferedAmount: 0, sent: [], closes: 0,
      send(raw) { const request = JSON.parse(raw); this.sent.push(request); respond(handlers, request, this); },
      close() { this.closes++; assert.equal(this.closes, 1); handlers.close(new Error('disposed')); },
    };
    connections.push(connection);
    return connection;
  };
  return { transport: framedTransport(open, options), connections };
}
const reply = (handlers, request) => handlers.message(response(request.id, request.method === 'initialize' ? initial : { revision: '2' }));
test('framed native calls verify runtime identity per connection and safely join reentrant closure', async () => {
  const { transport, connections } = connector(reply);
  const client = await Client.connect(transport, { clientID: 'native' });
  assert.equal((await client.treeCatalog()).revision, '2');
  assert.deepEqual(connections.map(connection => connection.sent.map(request => request.method)), [['initialize'], ['initialize', 'trees.catalog']]);
  assert.ok(connections.every(connection => connection.closes === 1));
  assert.equal(connections[1].sent[0].params.expected_runtime_id, 'runtime');
});
test('native transport rejects network bridges, changed identities and malformed responses before dependent requests', async () => {
  for (const bad of [{ ...initial, network_client: true }, { ...initial, runtime_id: 'other' }]) {
    const { transport, connections } = connector((handlers, request) => handlers.message(response(request.id, bad)));
    await assert.rejects(Client.connect(transport, { clientID: 'native', expectedRuntimeID: 'runtime' }), /mismatch/);
    assert.equal(connections[0].sent.length, 1);
  }
  for (const raw of ['{', response('wrong', initial), ' '.repeat((8 << 20) + 1)]) {
    const { transport, connections } = connector(handlers => handlers.message(raw));
    await assert.rejects(Client.connect(transport, { clientID: 'native' }));
    assert.equal(connections[0].closes, 1);
  }
  const { transport, connections } = connector(reply, 'websocket');
  await assert.rejects(Client.connect(transport, { clientID: 'native' }), /confined Unix/);
  assert.equal(connections[0].sent.length, 0);
});
test('lost acknowledgement never replays and abort while opening retires a late connection', async () => {
  const { transport, connections } = connector((handlers, request) => request.method === 'initialize' ? reply(handlers, request) : handlers.close(new Error('lost ack')));
  const client = await Client.connect(transport, { clientID: 'native' });
  await assert.rejects(client.treeCatalog(), DeliveryError);
  assert.equal(connections.length, 2);
  assert.equal(connections[1].sent.length, 2);
  const abort = new AbortController(), reason = new Error('stop local wait');
  let finish, closes = 0;
  const pending = Client.connect(framedTransport(() => new Promise(resolve => { finish = resolve; })), { clientID: 'native', signal: abort.signal });
  abort.abort(reason);
  await assert.rejects(pending, error => error === reason);
  finish({ kind: 'unix', bufferedAmount: 0, close() { closes++; }, send() { assert.fail('must not send after cancellation'); } });
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(closes, 1);
});
test('queued native bytes reject the next call without sending it', async () => {
  const { transport, connections } = connector((handlers, request, connection) => { connection.bufferedAmount = 8 << 20; reply(handlers, request); });
  const client = await Client.connect(transport, { clientID: 'native' });
  await assert.rejects(client.treeCatalog(), /queue limit/);
  assert.equal(connections[1].sent.length, 1);
});

test('native epoch pins reject restart before relaying a dependent effect', async () => {
  let epoch = initial.process_epoch;
  const { transport, connections } = connector((handlers, request) => handlers.message(response(request.id, { ...initial, process_epoch: epoch })), 'unix', { expectedProcessEpoch: epoch });
  const client = await Client.connect(transport, { clientID: 'native', expectedRuntimeID: initial.runtime_id });
  assert.equal(connections[0].sent[0].params.expected_process_epoch, epoch);
  epoch = 'boot_restarted';
  await assert.rejects(client.hosts.setProfiles('a'.repeat(64), []), /process generation/);
  assert.deepEqual(connections[1].sent.map(request => request.method), ['initialize']);
  assert.equal(connections[1].sent[0].params.expected_process_epoch, initial.process_epoch);
  assert.ok(connections.every(connection => connection.closes === 1));
  await assert.rejects(Client.connect(transport, { clientID: 'native' }), /process generation/);
  assert.equal(connections[2].sent.length, 1);
});
