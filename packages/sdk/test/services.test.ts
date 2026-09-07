import assert from 'node:assert/strict';
import test from 'node:test';
import { WhipClient } from '../src/client.js';
import { decodeBase64 } from '../src/util.js';
import { transportFixture } from './transport-fixture.js';

const decision = { root_id: 'root', permission_id: 'permission', allow: true, command_id: 'decision-1', reason: '<tag> & café "quoted"' };

for (const clientKind of ['human', 'automation'] as const) {
  test(`${clientKind} clients submit unsigned decisions with stable command identities`, async t => {
    const fixture = transportFixture({ request(request, connection) {
      if (request.method === 'permission.decide') connection.reply(request, { operation_id: 'operation', lease_id: 'lease' });
    } });
    const client = new WhipClient({ endpoint: fixture.factory, clientId: 'client', clientKind, reconnect: false });
    t.after(() => client.close());
    await client.connect();
    assert.deepEqual(await client.permissions.decide(decision), { operation_id: 'operation', lease_id: 'lease' });
    const first = fixture.current.requests.at(-1)!;
    assert.deepEqual(first.params, { decision });
    await client.permissions.decide(decision);
    assert.deepEqual(fixture.current.requests.at(-1)!.params, first.params);
    await client.permissions.decide({ root_id: 'root', permission_id: 'denied', allow: false });
    const generated = fixture.current.requests.at(-1)!.params.decision as typeof decision;
    assert.ok(generated.command_id);
    assert.equal(generated.allow, false);
  });
}

test('permission modes use ordinary durable commands and recovery metadata', async t => {
  const records: unknown[] = [];
  const fixture = transportFixture({ request(request, connection) {
    if (request.method === 'command.submit') connection.reply(request, {
      operation: 'permission.mode', command_id: request.params.command_id, ingress_seq: '1', status: 'succeeded', result: { text: 'configured' },
    });
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'script', reconnect: false,
    recoveryStorage: { async list() { return []; }, async put(record) { records.push(record); }, async delete() {} } });
  t.after(() => client.close());
  await client.connect();
  const command = client.permissions.setMode('root', false, { commandId: 'mode-1' });
  assert.equal(command.operation, 'permission.mode');
  assert.equal((await command.result()).status, 'succeeded');
  assert.deepEqual(fixture.current.requests.at(-1)!.params, {
    command_id: 'mode-1', scope: 'root', root_id: 'root', operation: 'permission.mode', payload: { external_permissions: false },
  });
  assert.equal(records.length, 1);
});

test('lost decision acknowledgement is not replayed after reconnect', async t => {
  const fixture = transportFixture();
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'client' });
  t.after(() => client.close());
  await client.connect();
  await assert.rejects(client.permissions.decide(decision, { timeoutMs: 10 }), { kind: 'timeout' });
  fixture.current.fail();
  await client.connect();
  assert.equal(fixture.connections.flatMap(connection => connection.requests).filter(request => request.method === 'permission.decide').length, 1);
});

test('a locally rejected approval does not reset unrelated in-flight queries', async t => {
  const fixture = transportFixture();
  fixture.info.limits.in_flight_requests = 1;
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const query = client.call('daemon.ping', {});
  void query.catch(() => {});
  await assert.rejects(client.permissions.decide(decision), { kind: 'resource_limit' });
  assert.equal(client.getSnapshot().state, 'connected');
  const request = fixture.current.requests.at(-1)!;
  assert.equal(request.method, 'daemon.ping');
  fixture.current.reply(request, { generation: fixture.info.generation, build_id: 'fixture' });
  await query;
});

test('provider secrets, configuration updates, and terminal input never enter recovery storage', async t => {
  const records: unknown[] = [];
  const fixture = transportFixture({ request(request, connection) {
    if (request.method === 'provider.key.set') connection.reply(request, {
      import_claude: false, import_codex: false, revision: '4', default_model: '', default_provider: 'openrouter',
      default_effort: '', compact_model: '', compact_provider: '', compact_percent: 70, goal_max_rounds: 1, max_retries: 1,
    });
    else if (request.method === 'config.update') connection.error(request, 'conflict');
    else if (request.method === 'provider.validate') connection.reply(request, {models: []});
    else if (request.method === 'provider.key.rotate') connection.reply(request, {provider: 'inference', configured: true, key_source: 'machine', warnings: []});
    else if (request.method === 'operation.invoke') connection.reply(request, { result: { text: 'ok' } });
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'script', reconnect: false,
    recoveryStorage: { async list() { return []; }, async put(record) { records.push(record); }, async delete() {} } });
  t.after(() => client.close());
  await client.connect();
  await client.providers.setKey({ provider: 'openrouter', key: 'secret-provider-key', environment: false, revision: '3' });
  await client.providers.validate({name: 'openrouter', base_url: 'https://provider.example/v1', key: 'secret-validation-key'});
  assert.deepEqual(fixture.current.requests.at(-1)!.params, {name: 'openrouter', base_url: 'https://provider.example/v1', key: 'secret-validation-key'});
  await client.providers.rotateKey('inference');
  assert.deepEqual(fixture.current.requests.at(-1)!.params, {provider: 'inference'});
  await assert.rejects(client.configuration.update({ revision: 'stale', default_model: 'model' }), { kind: 'conflict' });
  await client.session('root').terminalInput('terminal', new TextEncoder().encode('terminal-secret'));
  assert.deepEqual(records, []);
  assert.ok(fixture.current.requests.every(request => request.method !== 'command.submit'));
  assert.equal(JSON.stringify(client.getSnapshot()).includes('secret'), false);
  const input = fixture.current.requests.at(-1)!.params.payload as { bytes: string };
  assert.equal(new TextDecoder().decode(decodeBase64(input.bytes)), 'terminal-secret');
});

test('provider login reconnect reads existing flow state instead of beginning another login', async t => {
  const fixture = transportFixture({ request(request, connection) {
    if (request.method === 'provider.login.status') connection.reply(request, {
      flow_id: 'flow', state: 'interrupted', teams: [], projects: [], expires_at: '2026-01-01T00:00:00Z',
    });
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'script' });
  t.after(() => client.close());
  await client.connect();
  fixture.current.fail();
  await client.connect();
  assert.equal((await client.providers.login.status('flow')).state, 'interrupted');
  assert.equal(fixture.connections.flatMap(connection => connection.requests).some(request => request.method === 'provider.login.begin'), false);
});
