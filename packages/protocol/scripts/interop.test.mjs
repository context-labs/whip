import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { validate, assertValid, manifest } from '../generated/index.js';

test('Go-produced fixtures validate without numeric coercion', async () => {
  const fixtures = JSON.parse(await readFile(new URL('../schema/fixtures.json', import.meta.url), 'utf8'));
  for (const fixture of fixtures) assertValid(fixture.type, fixture.value);
  const cursor = fixtures.find(fixture => fixture.type === 'SubscribeParams').value.cursor;
  assert.equal(cursor, '9007199254740993');
  assert.equal(validate('SubscribeParams', { root_id: 'root', subscription_id: 'view', cursor: 1 }), false);
  assert.equal(validate('SubscribeParams', { root_id: 'root', subscription_id: 'view', cursor: '9223372036854775808' }), false);
});

test('TypeScript requests reject undeclared fields and malformed input', () => {
  assertValid('InitializeParams', { protocol_major: 2, build_id: 'different-build', client_kind: 'human', client_id: 'browser' });
  assert.equal(validate('InitializeParams', { protocol_major: '2', build_id: '', client_kind: 'human', client_id: 'browser' }), false);
  assert.equal(validate('SubscribeParams', { root_id: 'root', subscription_id: 'view', cursor: '1', extra: true }), false);
  assert.throws(() => validate('unknown', {}), /Unknown/);
});

test('registered operations and events all reference generated contracts', () => {
  for (const operation of manifest.operations) {
    assert.doesNotThrow(() => validate(operation.params_type, {}));
    assert.doesNotThrow(() => validate(operation.result_type, {}));
  }
  assert.equal(new Set(manifest.operations.map(operation => operation.surface + ':' + operation.name)).size, manifest.operations.length);
});

test('Go and WebCrypto signatures bind exact transmitted payload bytes', async () => {
 const fixture=JSON.parse(await readFile(new URL('../schema/signing-fixture.json',import.meta.url),'utf8'));
 const bytes = value => new Uint8Array(Buffer.from(value,'base64'));
 const encoder=new TextEncoder();
 const prefix=encoder.encode('whip privileged request v2\0'+fixture.method+fixture.generation+'\0');
 const nonce=bytes(fixture.nonce);
 const payload=encoder.encode(fixture.payload);
 const input=new Uint8Array(prefix.length+nonce.length+payload.length);
 input.set(prefix);input.set(nonce,prefix.length);input.set(payload,prefix.length+nonce.length);
 const digest=new Uint8Array(await crypto.subtle.digest('SHA-256',input));
 assert.deepEqual(digest,bytes(fixture.digest));
 const publicKey=await crypto.subtle.importKey('raw',bytes(fixture.public_key),{name:'Ed25519'},false,['verify']);
 assert.equal(await crypto.subtle.verify('Ed25519',publicKey,bytes(fixture.signature),digest),true);
 const pkcs8Prefix=Buffer.from('302e020100300506032b657004220420','hex');
 const privateKey=await crypto.subtle.importKey('pkcs8',Buffer.concat([pkcs8Prefix,bytes(fixture.seed)]),{name:'Ed25519'},false,['sign']);
 assert.deepEqual(new Uint8Array(await crypto.subtle.sign('Ed25519',privateKey,digest)),bytes(fixture.signature));
 const changed=new Uint8Array(input);changed[changed.length-1]^=1;
 const changedDigest=await crypto.subtle.digest('SHA-256',changed);
 assert.equal(await crypto.subtle.verify('Ed25519',publicKey,bytes(fixture.signature),changedDigest),false);
});

test('responses accept additive fields without weakening known fields or changing input', () => {
  const value = { event: { root_id: 'root', seq: '9007199254740993', kind: 'new.event', payload: { future: true }, future: 1 }, future: true };
  const original = structuredClone(value);
  assert.equal(validate('EventNotification', value), false);
  assert.equal(validate('EventNotification', value, 'response'), true);
  assert.deepEqual(value, original);
  assert.equal(validate('EventNotification', { ...value, event: { ...value.event, seq: 42 } }, 'response'), false);
  assert.equal(validate('EventNotification', { ...value, event: { ...value.event, root_id: null } }, 'response'), false);
  assert.throws(() => validate('EventNotification', value, 'unexpected'), /Unknown WHIP validation mode/);
});

test('decimal string counters preserve signed int64 boundaries in both modes', () => {
  for (const mode of ['request', 'response']) {
    for (const cursor of ['0', '-1', '9007199254740993', '9223372036854775807', '-9223372036854775808']) {
      assert.equal(validate('SubscribeParams', { root_id: 'root', subscription_id: 'view', cursor }, mode), true, cursor);
    }
    for (const cursor of ['01', '+1', '1.0', '1e3', ' 1', '9223372036854775808', '-9223372036854775809', '9'.repeat(100)]) {
      assert.equal(validate('SubscribeParams', { root_id: 'root', subscription_id: 'view', cursor }, mode), false, cursor);
    }
  }
});

test('generated operation lookups and Go fixtures cover every registry entry', async () => {
  const { rpcOperations, runtimeOperations } = await import('../generated/index.js');
  const fixtures = JSON.parse(await readFile(new URL('../schema/fixtures.json', import.meta.url), 'utf8'));
  const fixtureTypes = new Set(fixtures.map(fixture => fixture.type));
  assert.equal(Object.keys(rpcOperations).length + Object.keys(runtimeOperations).length, manifest.operations.length);
  for (const operation of manifest.operations) {
    const lookup = operation.surface === 'rpc' ? rpcOperations : runtimeOperations;
    assert.deepEqual(lookup[operation.name], { ...operation, sensitive: operation.sensitive ?? false });
    assert.ok(fixtureTypes.has(operation.params_type), `${operation.name} request fixture`);
    assert.ok(fixtureTypes.has(operation.result_type), `${operation.name} result fixture`);
  }
  assert.equal(rpcOperations['permission.mode'].execution, 'ephemeral');
  assert.equal(runtimeOperations['permission.mode'].execution, 'command');
});
