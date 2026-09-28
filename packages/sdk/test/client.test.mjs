import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, RemoteError } from '../dist/index.js';

const initial = { major: 4, minor: 0, runtime_id: 'runtime', builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });
test('core pins identity, validates before transport and rejects malformed responses', async () => {
  const calls = [];
  const client = await Client.connect(async (request, expected) => {
    calls.push({ request, expected });
    if (request.method === 'initialize') return success(request, initial);
    return success(request, { items: [] });
  }, { clientID: 'test' });
  const history = await client.call('sessions.history', { session_id: 'session', after: '9007199254740993', limit: 10 });
  assert.deepEqual(history, { items: [] });
  assert.equal(calls[1].expected, 'runtime');
  await assert.rejects(client.call('sessions.history', { session_id: 'session', after: 0, limit: 10 }), TypeError);
  assert.equal(calls.length, 2);
  const invalid = await Client.connect(async request => request.method === 'initialize' ? success(request, initial) : success(request, {}), { clientID: 'test' });
  await assert.rejects(invalid.recover('request'), TypeError);
});
test('remote conflicts stay distinguishable from delivery uncertainty', async () => {
  const client = await Client.connect(async request => request.method === 'initialize' ? success(request, initial) : {
    jsonrpc: '2.0', id: request.id, error: { code: -32009, message: 'conflict', kind: 'CONFLICT' },
  }, { clientID: 'test' });
  await assert.rejects(client.submit('session', [{ type: 'text', text: 'hello' }], 'stable'), error => error instanceof RemoteError && error.kind === 'CONFLICT');
});
