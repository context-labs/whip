import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { validate, assertValid, manifest } from '../generated/index.js';

test('bounded navigation summaries validate without coercing counters or requiring a known root', () => {
  assertValid('SessionSummariesParams', { root_ids: [] });
  assertValid('SessionSummariesParams', { root_ids: ['root', 'missing'] });
  for (const root_ids of [null, [''], ['root', 'root'], ['a'.repeat(257)], Array.from({ length: 33 }, (_, index) => String(index))]) {
    assert.equal(validate('SessionSummariesParams', { root_ids }), false);
  }
  const item = { root_id: 'root', missing: false, title: '', cwd: '', running_agents: '9007199254740993', queued_agents: '0', pending_permissions: '1', pending_questions: '2', truncated: false };
  assertValid('SessionSummariesResult', { items: [item] });
  assertValid('SessionSummariesResult', { items: [{ ...item, root_id: 'missing', missing: true, running_agents: '0', pending_permissions: '0', pending_questions: '0' }] });
  assert.equal(validate('SessionSummariesResult', { items: null }), false);
  assert.equal(validate('SessionSummariesResult', { items: [{ ...item, running_agents: 9007199254740992 }] }), false);
});

test('Go-produced fixtures validate without numeric coercion', async () => {
  const fixtures = JSON.parse(await readFile(new URL('../schema/fixtures.json', import.meta.url), 'utf8'));
  for (const fixture of fixtures) assertValid(fixture.type, fixture.value);
  const cursor = fixtures.find(fixture => fixture.type === 'SubscribeParams').value.cursor;
  assert.equal(cursor, '9007199254740993');
  assert.equal(validate('SubscribeParams', { root_id: 'root', subscription_id: 'view', cursor: 1 }), false);
  assert.equal(validate('SubscribeParams', { root_id: 'root', subscription_id: 'view', cursor: '9223372036854775808' }), false);
});

test('TypeScript requests reject undeclared fields and malformed input', () => {
  assertValid('InitializeParams', { protocol_major: manifest.major, build_id: 'different-build', client_kind: 'human', client_id: 'browser' });
  assert.equal(validate('InitializeParams', { protocol_major: '3', build_id: '', client_kind: 'human', client_id: 'browser' }), false);
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
  assert.equal(rpcOperations['permission.mode'], undefined);
  assert.equal(rpcOperations['identity.enroll'], undefined);
  assert.equal(rpcOperations['identity.status'], undefined);
  assert.equal(rpcOperations['permission.decide'].permission, 'trusted-client-decision');
  assert.equal(runtimeOperations['permission.mode'].execution, 'command');
});


test('permission decisions are typed requests without signing credentials', () => {
  const request = { decision: { command_id: 'decision', root_id: 'root', permission_id: 'permission', allow: true } };
  assertValid('PermissionDecisionParams', request);
  assert.equal(validate('PermissionDecisionParams', { ...request, signature: 'old-signature' }), false);
  assert.equal(validate('PermissionDecisionParams', { decision: { ...request.decision, allow: 'true' } }), false);
  assertValid('PermissionDecisionResult', { operation_id: 'operation', lease_id: 'lease' });
});

test('collection variants preserve typed properties and exactly one item', () => {
  const page = { root_id: 'root', collection: 'budgets', revision: '1', event_cursor: '1', has_more: false,
    items: [{ budget: { agent_id: 'root', state: { kind: 'tokens', limit: '9007199254740993', used: '0', reserved: '0', uncertain: '0', incomplete: false, remaining: '9007199254740993' } } }] };
  assertValid('RootCollectionPage', page);
  assert.equal(validate('RootCollectionPage', { ...page, items: [{}] }), false);
  assert.equal(validate('RootCollectionPage', { ...page, items: [{ ...page.items[0], agent: null }] }), false);
  assert.equal(validate('RootCollectionPage', { ...page, items: [{ budget: { ...page.items[0].budget, agent_id: 1 } }] }), false);
});


test('model budgets preserve explicit unlimited and exact uncertainty', () => {
  const state = { kind: 'cost', limit: null, remaining: null, used: '9007199254740993', reserved: '0', uncertain: '23883863', incomplete: true };
  assertValid('BudgetState', state);
  assertValid('BudgetState', { ...state, limit: '9223372036854775807', remaining: '0' });
  assert.equal(validate('BudgetState', { ...state, limit: 0 }), false);
  assert.equal(validate('BudgetState', { ...state, uncertain: 23883863 }), false);
});

test('protocol 4.0 usage remains valid without the additive reported field', async () => {
  const fixtures = JSON.parse(await readFile(new URL('../schema/fixtures.json', import.meta.url), 'utf8'));
  for (const type of ['RootSnapshot', 'AgentTranscriptResult', 'BoundedTranscriptPage', 'CompactionResult', 'StreamEvent']) {
    const value = structuredClone(fixtures.find(fixture => fixture.type === type)?.value ?? {});
    const usage = { prompt_tokens: 17, completion_tokens: 3 };
    const message = { role: 'assistant', content: 'Retained response', usage };
    if (type === 'AgentTranscriptResult') value.page.messages = [{ seq: 1, role: message.role, message }];
    else if (type === 'BoundedTranscriptPage') value.messages = [{ seq: 1, role: message.role, message }];
    else if (type === 'StreamEvent') value.usage = { used: 20, size: 4096, usage };
    else if (type === 'CompactionResult') value.usage = usage;
    else value.messages = [message];
    assertValid(type, value, 'response');
    assert.equal(Object.hasOwn(usage, 'reported'), false, 'validation must not invent usage provenance');
    for (const reported of [false, true]) {
      usage.reported = reported;
      assertValid(type, value, 'response');
    }
    for (const reported of [null, 'true', 0]) {
      usage.reported = reported;
      assert.equal(validate(type, value, 'response'), false, `${type} must reject malformed provenance`);
    }
  }
});
