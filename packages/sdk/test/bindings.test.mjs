import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const initial = { major: 4, minor: 0, runtime_id: 'runtime', builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });

test('binding reads retain separate origins and explicit empty updates do not invent executor authority', async () => {
  const session = structuredClone(fixtures.find(value => value.type === 'Session' && value.valid).value);
  session.configuration.modules = [];
  session.configuration.tools_definition = { id: 'tool-owner', revision: 'a'.repeat(64) };
  session.configuration.hooks_definition = { id: 'hook-owner', revision: 'b'.repeat(64) };
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request);
    return success(request, session);
  }, { clientID: 'bindings' });
  const result = await client.call('sessions.configure', { session_id: session.id, expected_revision: session.config_revision, patch: { modules: [], tools: {} } });
  assert.deepEqual(calls[0].params.patch, { modules: [], tools: {} });
  assert.deepEqual(result.configuration.modules, []);
  assert.equal(result.configuration.tools_definition.id, 'tool-owner');
  assert.equal(result.configuration.hooks_definition.id, 'hook-owner');
  for (const patch of [{ modules: ['unknown'] }, { modules: ['files', 'files'] }, { tools_definition: session.configuration.tools_definition }, { hooks_definition: session.configuration.hooks_definition }]) {
    await assert.rejects(client.call('sessions.configure', { session_id: session.id, expected_revision: session.config_revision, patch }), TypeError);
  }
  assert.equal(calls.length, 1);
});
