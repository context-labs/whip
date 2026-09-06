import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { setTimeout as delay } from 'node:timers/promises';
import test from 'node:test';
import { WhipClient } from '../src/client.js';
import { approvalDigest, createWebCryptoSigner } from '../src/services.js';
import { decodeBase64, encodeBase64 } from '../src/util.js';
import { transportFixture, type FixtureRequest } from './transport-fixture.js';

const decision = { root_id: 'root', permission_id: 'permission', allow: true, command_id: 'decision-1', reason: '<tag> & café "quoted"' };
const nonce2 = encodeBase64(new Uint8Array(32).fill(4));
async function until(predicate: () => boolean) {
  for (let attempt = 0; attempt < 100; attempt++) { if (predicate()) return; await delay(2); }
  assert.fail('Expected request did not arrive');
}

test('approval digest and WebCrypto signer match Go exact-byte signing fixture', async () => {
  const fixture = JSON.parse(await readFile(new URL('../../../protocol/schema/signing-fixture.json', import.meta.url), 'utf8')) as {
    method: 'permission.decide'; generation: string; nonce: string; payload: string; seed: string;
    public_key: string; digest: string; signature: string;
  };
  const digest = await approvalDigest(fixture.method, fixture.generation, decodeBase64(fixture.nonce), fixture.payload);
  assert.equal(encodeBase64(digest), fixture.digest);
  const prefix = Uint8Array.from(Buffer.from('302e020100300506032b657004220420', 'hex'));
  const key = new Uint8Array(prefix.length + 32);
  key.set(prefix); key.set(decodeBase64(fixture.seed), prefix.length);
  const privateKey = await crypto.subtle.importKey('pkcs8', key, 'Ed25519', false, ['sign']);
  assert.equal(encodeBase64(await createWebCryptoSigner(privateKey).sign(digest)), fixture.signature);
  const reserialized = JSON.stringify(JSON.parse(fixture.payload));
  assert.notEqual(encodeBase64(await approvalDigest(fixture.method, fixture.generation, decodeBase64(fixture.nonce), reserialized)), fixture.digest);
});

test('approval queue signs exact transmitted bytes and rotates nonce only after acknowledgement', async t => {
  const requests: FixtureRequest[] = [];
  const signed: Uint8Array[] = [];
  const fixture = transportFixture({ request: request => { requests.push(request); } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'human', clientKind: 'human', reconnect: false,
    signer: { async sign(digest) { signed.push(digest); return new Uint8Array(64); } } });
  t.after(() => client.close());
  await client.connect();
  const first = client.permissions.decide(decision);
  const second = client.permissions.setMode('root', false, { commandId: 'mode-2' });
  await until(() => requests.length === 1);
  assert.equal(signed.length, 1);
  const payload = JSON.stringify(decision);
  assert.ok(requests[0]!.raw.includes(`"decision":${payload},"signature":`));
  assert.deepEqual(signed[0], await approvalDigest('permission.decide', fixture.info.generation, decodeBase64(fixture.info.nonce!), payload));
  fixture.current.reply(requests[0]!, { operation_id: 'operation', lease_id: 'lease', nonce: nonce2 });
  await first;
  await until(() => requests.length === 2);
  const command = JSON.stringify(requests[1]!.params.command);
  assert.ok(requests[1]!.raw.includes(`"command":${command},"signature":`));
  assert.deepEqual(signed[1], await approvalDigest('permission.mode', fixture.info.generation, decodeBase64(nonce2), command));
  fixture.current.reply(requests[1]!, { command: { operation: 'permission.mode', command_id: 'mode-2', ingress_seq: '1', status: 'succeeded', result: { text: 'configured' } }, nonce: nonce2 });
  await second;
  assert.ok(requests.every(request => request.method !== 'command.submit'));
});

test('disconnect during signing cannot transmit a signature on its replacement connection', async t => {
  let release!: (signature: Uint8Array<ArrayBuffer>) => void;
  let started = false;
  const fixture = transportFixture();
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'human', clientKind: 'human',
    signer: { sign() { started = true; return new Promise(resolve => { release = resolve; }); } } });
  t.after(() => client.close());
  await client.connect();
  const pending = client.permissions.decide(decision);
  await until(() => started);
  fixture.current.fail();
  await client.connect();
  release(new Uint8Array(64));
  await assert.rejects(pending, { kind: 'recovery_required' });
  assert.ok(fixture.connections.every(connection => connection.requests.every(request => request.method === 'initialize')));
});

test('lost signed acknowledgement refreshes nonce without replaying the decision', async t => {
  const fixture = transportFixture();
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'human', clientKind: 'human',
    signer: { async sign() { return new Uint8Array(64); } } });
  t.after(() => client.close());
  await client.connect();
  await assert.rejects(client.permissions.decide(decision, { timeoutMs: 10 }), { kind: 'timeout' });
  assert.equal(client.getSnapshot().state, 'reconnecting');
  await client.connect();
  assert.equal(fixture.connections.flatMap(connection => connection.requests).filter(request => request.method === 'permission.decide').length, 1);
});

test('a locally rejected approval does not reset unrelated in-flight queries', async t => {
  const fixture = transportFixture();
  fixture.info.limits.in_flight_requests = 1;
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'human', clientKind: 'human', reconnect: false,
    signer: { async sign() { return new Uint8Array(64); } } });
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

test('automation cannot sign human decisions or enroll a human identity', async t => {
  let signs = 0;
  const signer = { async sign() { signs++; return new Uint8Array(64); } };
  const fixture = transportFixture();
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'script', clientKind: 'automation', reconnect: false, signer: signer });
  t.after(() => client.close());
  await client.connect();
  await assert.rejects(client.permissions.decide(decision), { kind: 'permission_denied' });
  await assert.rejects(client.permissions.setMode('root', false), { kind: 'permission_denied' });
  await assert.rejects(client.permissions.enroll(new Uint8Array(32), 'paired', signer), { kind: 'permission_denied' });
  assert.equal(signs, 0);
  assert.equal(fixture.current.requests.length, 1);
});

test('browser enrollment requires a paired authorizer and never claims terminal confirmation', async t => {
  let open = true;
  const fixture = transportFixture({ request(request, connection) {
    if (request.method === 'identity.status') connection.reply(request, { client_id: 'human', kind: 'human', paired: false, enrollment_open: open });
    else if (request.method === 'identity.enroll') connection.reply(request, { client_id: 'human', kind: 'human', nonce: nonce2 });
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'human', clientKind: 'human', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const signer = { async sign() { return new Uint8Array(64); } };
  await assert.rejects(client.permissions.enroll(new Uint8Array(32), 'paired', signer), { kind: 'permission_denied' });
  assert.equal(fixture.current.requests.filter(request => request.method === 'identity.enroll').length, 0);
  open = false;
  await client.permissions.enroll(new Uint8Array(32), 'paired', signer);
  const request = fixture.current.requests.at(-1)!;
  assert.equal(request.method, 'identity.enroll');
  assert.equal(request.params.authorized_by, 'paired');
  assert.equal(Object.hasOwn(request.params, 'tty_confirmed'), false);
});

test('provider secrets, configuration updates, and terminal input never enter recovery storage', async t => {
  const records: unknown[] = [];
  const fixture = transportFixture({ request(request, connection) {
    if (request.method === 'provider.key.set') connection.reply(request, {
      import_claude: false, import_codex: false, revision: '4', default_model: '', default_provider: 'openrouter',
      default_effort: '', compact_model: '', compact_provider: '', compact_percent: 70, goal_max_rounds: 1, max_retries: 1,
    });
    else if (request.method === 'config.update') connection.error(request, 'conflict');
    else if (request.method === 'operation.invoke') connection.reply(request, { result: { text: 'ok' } });
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'script', reconnect: false,
    recoveryStorage: { async list() { return []; }, async put(record) { records.push(record); }, async delete() {} } });
  t.after(() => client.close());
  await client.connect();
  await client.providers.setKey({ provider: 'openrouter', key: 'secret-provider-key', environment: false, revision: '3' });
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
