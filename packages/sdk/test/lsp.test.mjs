import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client } from '../dist/index.js';

test('language server status is a validated, uncached session observation', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] } };
    calls.push(request);
    return { jsonrpc: '2.0', id: request.id, result: { items: [{ name: 'gopls', state: 'not_started', workspace_root: null, failure: null }] } };
  }, { clientID: 'test' });
  assert.equal((await client.languageServerStatus('root')).items[0].state, 'not_started');
  await client.languageServerStatus('child');
  assert.deepEqual(calls.map(value => [value.method, value.params]), [['lsp.status', { session_id: 'root' }], ['lsp.status', { session_id: 'child' }]]);
  await assert.rejects(client.languageServerStatus('../escape'), TypeError);
  assert.equal(calls.length, 2);
});
