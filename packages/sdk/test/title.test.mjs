import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client, RemoteError } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = (type, match = () => true) => structuredClone(fixtures.find(value => value.valid && value.type === type && match(value.value)).value);
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });

test('title reads retain immutable scope, receipt identity and exact revisions without a client cache', async () => {
  const requests = [];
  const decision = fixture('AutomaticTitleDecision', value => value.reason === 'eligible');
  const result = fixture('AutomaticTitleResult', value => !value.applied);
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    requests.push(request);
    return success(request, structuredClone(request.method === 'trees.title_decision' ? decision : result));
  }, { clientID: 'titles' });
  const first = await client.getAutomaticTitleDecision(decision.tree_id);
  assert.equal(first.config_revision, '9007199254740993');
  assert.equal(first.expected_revision, '9007199254740994');
  assert.deepEqual(first.receipt_identity, { client_id: 'automatic-title', request_id: decision.tree_id });
  first.source = 'consumer edit';
  assert.deepEqual(await client.getAutomaticTitleDecision(decision.tree_id), decision);
  assert.deepEqual(await client.getAutomaticTitleResult(result.tree_id, result.attempt_id), result);
  assert.deepEqual(requests.map(({ method, params }) => ({ method, params })), [
    { method: 'trees.title_decision', params: { tree_id: decision.tree_id } },
    { method: 'trees.title_decision', params: { tree_id: decision.tree_id } },
    { method: 'trees.title_result', params: { tree_id: result.tree_id, attempt_id: result.attempt_id } },
  ]);
  await assert.rejects(client.getAutomaticTitleResult(result.tree_id, ''), TypeError);
  await assert.rejects(client.getAutomaticTitleDecision(decision.tree_id, { signal: AbortSignal.abort() }), error => error.name === 'AbortError');
  assert.equal(requests.length, 3);
});

test('explicit title policy false crosses the configuration boundary and malformed evidence fails closed', async () => {
  const session = fixture('Session');
  session.configuration.automatic_title = false;
  let request;
  let candidate = fixture('AutomaticTitleResult');
  const client = await Client.connect(async value => {
    if (value.method === 'initialize') return success(value, initial);
    request = value;
    return success(value, value.method === 'sessions.configure' ? session : candidate);
  }, { clientID: 'titles' });
  assert.equal((await client.call('sessions.configure', { session_id: session.id, expected_revision: session.config_revision, patch: { automatic_title: false } })).configuration.automatic_title, false);
  assert.deepEqual(request.params.patch, { automatic_title: false });
  for (const text of ['🌍'.repeat(81), 'two\nlines', 'control\u0085', 'two\u2028lines', 'title\n', 'title\r', 'title\u2029', ' leading', 'trailing ', 'title\u00a0']) {
    candidate = { ...candidate, text };
    await assert.rejects(client.getAutomaticTitleResult(candidate.tree_id, candidate.attempt_id), TypeError);
  }
  candidate = { ...candidate, text: '🌍'.repeat(80) };
  assert.equal((await client.getAutomaticTitleResult(candidate.tree_id, candidate.attempt_id)).text, candidate.text);
});

test('uninitialized title evidence remains not-found and performs no automatic admission or retry', async () => {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request.method);
    return { jsonrpc: '2.0', id: request.id, error: { code: -32004, kind: 'NOT_FOUND', message: 'not found' } };
  }, { clientID: 'titles' });
  await assert.rejects(client.getAutomaticTitleDecision('new-tree'), error => error instanceof RemoteError && error.kind === 'NOT_FOUND');
  assert.deepEqual(calls, ['trees.title_decision']);
});
