import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client, DurableCommand, RecoveryJournal, RecoveryError, RecoveryPersistenceError, DeliveryError, recoveryNamespace } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const params = (id = 'request') => ({ ...fixture('SubmitParams'), identity: { client_id: 'client', request_id: id } });
const admission = (id = 'request') => { const result = fixture('Admission'); result.receipt.identity.request_id = id; return result; };
const missing = id => ({ jsonrpc: '2.0', id, error: { code: -32001, kind: 'NOT_FOUND', message: 'missing' } });
async function clientFixture(run, identity = {}) {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { ...initial, ...identity } };
    calls.push(structuredClone(request)); return run(request);
  }, { clientID: 'client' });
  return { client, calls };
}
function memoryStorage() {
  const records = new Map(); const events = [];
  return { records, events,
    async list(namespace, limit) { assert.equal(namespace, recoveryNamespace); return [...records.values()].slice(0, limit); },
    async put(namespace, key, record) { events.push(['put', namespace, key]); records.set(key, structuredClone(record)); },
    async delete(namespace, key) { events.push(['delete', namespace, key]); records.delete(key); },
  };
}

test('prepared commands freeze exact input and recover a lost acknowledgement without effect replay', async () => {
  let exists = false;
  const storage = memoryStorage(), journal = new RecoveryJournal(storage);
  const { client, calls } = await clientFixture(request => {
    if (request.method === 'receipts.get') return exists ? { jsonrpc: '2.0', id: request.id, result: admission() } : missing(request.id);
    assert.equal(storage.records.size, 1);
    if (exists) return { jsonrpc: '2.0', id: request.id, result: admission() };
    exists = true; throw new DeliveryError('lost acknowledgement');
  });
  const input = params(); const command = client.command('sessions.submit', input, { journal });
  input.parts[0].text = 'mutated'; command.params.parts[0].text = 'also mutated';
  assert.equal(calls.length, 0);
  await assert.rejects(command.send(), DeliveryError);
  assert.equal(calls[0].params.parts[0].text, 'Run this.');
  const loaded = DurableCommand.recover(client, (await journal.list())[0], { journal });
  assert.equal(calls.length, 1);
  assert.throws(() => loaded.send(), /explicit check\/retry/);
  assert.equal((await loaded.check()).state, 'identity_only');
  assert.equal(loaded.record.accepted, false);
  await assert.rejects(loaded.wait(), /Exact request acceptance/);
  assert.equal(calls.filter(value => value.method === 'sessions.submit').length, 1);
  await loaded.retry(); // Only this explicit request verifies the original payload.
  assert.equal((await loaded.check()).state, 'found');
  assert.equal((await loaded.wait()).receipt.deleted_at !== null, true);
  assert.equal(loaded.record.accepted, true);
  await loaded.forget(); assert.equal((await journal.list()).length, 0);
});

test('explicit retry deduplicates callers, keeps same bytes, and refuses a missing accepted receipt', async () => {
  let accepted = false, responses = 0;
  const { client, calls } = await clientFixture(request => {
    if (request.method === 'receipts.get') return accepted ? { jsonrpc: '2.0', id: request.id, result: admission() } : missing(request.id);
    responses++; if (responses === 1) throw new DeliveryError('not delivered');
    accepted = true; return { jsonrpc: '2.0', id: request.id, result: admission() };
  });
  const command = client.command('sessions.submit', params());
  await assert.rejects(command.send(), DeliveryError);
  await assert.rejects(command.send(), DeliveryError); assert.equal(responses, 1);
  const [left, right] = await Promise.all([command.retry(), command.retry()]);
  assert.deepEqual(left, right); assert.equal(responses, 2);
  assert.deepEqual(calls.filter(value => value.method === 'sessions.submit')[0].params, calls.filter(value => value.method === 'sessions.submit')[1].params);
  accepted = false;
  await assert.rejects(command.retry(), RecoveryError); assert.equal(responses, 2);
  const foreign = await clientFixture(() => { throw new Error('must not send'); }, { runtime_id: 'other' });
  assert.throws(() => DurableCommand.recover(foreign.client, command.record), /another runtime/);
});

test('one waiter can abort without cancelling a shared send or turning it into a remote failure', async () => {
  let complete;
  const { client, calls } = await clientFixture(request => new Promise(resolve => { complete = () => resolve({ jsonrpc: '2.0', id: request.id, result: admission() }); }));
  const command = client.command('sessions.submit', params());
  const abort = new AbortController();
  const waiter = command.send({ signal: abort.signal }); const other = command.send();
  abort.abort(new Error('stop observation'));
  await assert.rejects(waiter, /stop observation/);
  assert.equal(calls.length, 1); complete();
  assert.equal((await other).receipt.identity.request_id, 'request');
});

test('recovery journal enforces count/bytes and namespace before transmission without evicting work', async () => {
  const storage = memoryStorage(); const journal = new RecoveryJournal(storage, { maxRecords: 1, maxBytes: 4096 });
  const { client, calls } = await clientFixture(request => ({ jsonrpc: '2.0', id: request.id, result: admission(request.params.identity.request_id) }));
  const first = client.command('sessions.submit', params('first'), { journal });
  const second = client.command('sessions.submit', params('second'), { journal });
  const outcomes = await Promise.allSettled([first.send(), second.send()]);
  assert.equal(outcomes.filter(value => value.status === 'fulfilled').length, 1);
  assert.equal(calls.length, 1); assert.equal(storage.records.size, 1);
  const duplicate = client.command('sessions.submit', { ...params('first'), parts: [{ type: 'text', text: 'changed' }] }, { journal });
  await assert.rejects(duplicate.send(), RecoveryPersistenceError); assert.equal(calls.length, 1);
  const tiny = new RecoveryJournal(memoryStorage(), { maxBytes: 32 });
  await assert.rejects(client.command('sessions.submit', params('bytes'), { journal: tiny }).send(), error => error instanceof RecoveryPersistenceError && !error.accepted);
  assert.equal(calls.length, 1);
  assert.throws(() => DurableCommand.recover(client, { ...first.record, namespace: 'whip.v3.commands' }), TypeError);
  assert.throws(() => DurableCommand.recover(client, { ...first.record, privateKey: 'must not persist' }), TypeError);
  assert.throws(() => client.command('providers.create', { private_key: 'must not persist' }), /cannot be journaled/);
  assert.throws(() => client.command('terminal.write', { data_base64: 'a2V5' }), /cannot be journaled/);
  assert.equal((await journal.list())[0].accepted, true);
});

test('an acknowledgement survives a later local persistence failure as explicit evidence', async () => {
  const storage = memoryStorage(); const put = storage.put;
  storage.put = async (...args) => { if (args[2].accepted) throw new Error('storage unavailable'); await put(...args); };
  const { client, calls } = await clientFixture(request => ({ jsonrpc: '2.0', id: request.id, result: admission() }));
  const command = client.command('sessions.submit', params(), { journal: new RecoveryJournal(storage) });
  await assert.rejects(command.send(), error => error instanceof RecoveryPersistenceError && error.accepted && error.acknowledgement.receipt.identity.request_id === 'request');
  assert.equal(command.record.accepted, true); assert.equal(calls.length, 1);
});

test('receipt-less exact mutations report unavailable inspection and never manufacture input identities', async () => {
  const { client, calls } = await clientFixture(() => { throw new Error('inspection must not send'); });
  const command = client.command('sessions.rewind', { edit_id: 'Edit:Mixed', session_id: 'owner', expected_revision: '9007199254740993', observed_through: '9007199254740995', keep_through: '0' });
  assert.deepEqual(await command.check(), { state: 'unavailable' });
  await assert.rejects(command.wait(), /no input\/turn receipt/);
  assert.equal(calls.length, 0); assert.equal(command.params.edit_id, 'Edit:Mixed');
});

test('slow recovery storage has bounded pending work and never becomes an unbounded request queue', async () => {
  const storage = memoryStorage(); let release;
  const wait = new Promise(resolve => { release = resolve; });
  const list = storage.list;
  storage.list = async (...args) => { await wait; return list(...args); };
  const journal = new RecoveryJournal(storage, { maxRecords: 2 });
  const one = journal.list(), two = journal.list();
  await assert.rejects(journal.list(), /pending work limit/);
  release(); assert.deepEqual(await one, []); assert.deepEqual(await two, []);
});

test('an identity collision cannot confirm or wait on another command payload', async () => {
  const { client, calls } = await clientFixture(request => {
    if (request.method === 'receipts.get') return { jsonrpc: '2.0', id: request.id, result: admission() };
    return { jsonrpc: '2.0', id: request.id, error: { code: -32004, kind: 'CONFLICT', message: 'identity already has another payload' } };
  });
  const command = client.command('sessions.submit', { ...params(), parts: [{ type: 'text', text: 'different request' }] });
  assert.equal((await command.check()).state, 'identity_only');
  assert.equal(command.record.accepted, false);
  await assert.rejects(command.wait(), /Exact request acceptance/);
  assert.ok(calls.every(value => value.method === 'receipts.get'));
  await assert.rejects(command.retry(), error => error.kind === 'CONFLICT');
  assert.equal(command.record.accepted, false);
});
