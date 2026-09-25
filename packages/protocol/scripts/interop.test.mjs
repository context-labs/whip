import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { validate, assertValid, manifest } from '../generated/index.js';

test('host skill completion validates explicit global and legacy project scopes', () => {
  const base = { prefix: '', limit: 1024 };
  for (const fields of [{ cwd: '/project' }, { scope: '', cwd: '/project' }, { scope: 'global' }, { scope: 'global', cwd: '' }]) {
    assertValid('HostSkillCompletionParams', { ...base, ...fields });
    assert.equal(validate('HostSkillCompletionParams', { ...base, ...fields }), true);
  }
  for (const fields of [{}, { scope: '' }, { cwd: '' }, { scope: 'global', cwd: '/project' },
    { scope: 'global', cwd: ' ' }, { scope: 'project', cwd: '/project' }, { scope: 'other' },
    { scope: null, cwd: '/project' }, { scope: 'global', cwd: null }, { scope: 'global', cwd: 42 },
    { scope: 'global', extra: true }]) {
    assert.equal(validate('HostSkillCompletionParams', { ...base, ...fields }), false, JSON.stringify(fields));
  }
  for (const value of [{}, { scope: 'global', limit: 1 }, { scope: 'global', prefix: '' }]) {
    assert.equal(validate('HostSkillCompletionParams', value), false);
  }
});

test('bounded navigation summaries validate without coercing counters or requiring a known root', () => {
  assertValid('SessionSummariesParams', { root_ids: [] });
  assertValid('SessionSummariesParams', { root_ids: ['root', 'missing'] });
  for (const root_ids of [null, [''], ['root', 'root'], ['a'.repeat(257)], Array.from({ length: 33 }, (_, index) => String(index))]) {
    assert.equal(validate('SessionSummariesParams', { root_ids }), false);
  }
  const item = { root_id: 'root', missing: false, archived: false, title: '', cwd: '', running_agents: '9007199254740993', queued_agents: '0', pending_permissions: '1', pending_questions: '2', truncated: false };
  assertValid('SessionSummariesResult', { items: [item] });
  assertValid('SessionSummariesResult', { items: [{ ...item, root_id: 'missing', missing: true, running_agents: '0', pending_permissions: '0', pending_questions: '0' }] });
  assert.equal(validate('SessionSummariesResult', { items: null }), false);
  assert.equal(validate('SessionSummariesResult', { items: [{ ...item, running_agents: 9007199254740992 }] }), false);
});

test('permission defaults remain compatible with legacy hosts and typed updates', async () => {
  const fixtures = JSON.parse(await readFile(new URL('../schema/fixtures.json', import.meta.url), 'utf8'));
  const configuration = { ...fixtures.find(fixture => fixture.type === 'RuntimeConfiguration').value };
  delete configuration.default_permission_mode;
  assertValid('RuntimeConfiguration', configuration);
  for (const mode of ['prompt', 'automatic']) {
    assertValid('RuntimeConfiguration', { ...configuration, default_permission_mode: mode });
    assertValid('ConfigurationUpdate', { revision: 'current', default_permission_mode: mode });
  }
  assert.equal(validate('RuntimeConfiguration', { ...configuration, default_permission_mode: true }), false);
  assert.equal(validate('ConfigurationUpdate', { revision: 'current', default_permission_mode: true }), false);
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

test('custom provider mutations are sensitive host RPCs with strict nested request contracts', () => {
  for (const name of ['provider.create', 'provider.update']) {
    const operation = manifest.operations.find(operation => operation.name === name);
    assert.equal(operation.surface, 'rpc');
    assert.equal(operation.execution, 'ephemeral');
    assert.equal(operation.permission, 'configuration-revision');
    assert.equal(operation.sensitive, true);
  }
  assert.equal(manifest.operations.find(operation => operation.name === 'provider.get').execution, 'query');
  assert.equal(manifest.operations.find(operation => operation.name === 'provider.remove').execution, 'ephemeral');
  const create = {
    revision: 'revision', provider: 'local',
    definition: { name: 'Local', base_url: 'http://localhost:8080/v1', api: 'openai-completions' },
    credential: { mode: 'environment', environment_variable: 'LOCAL_API_KEY' },
    manual_model: { alias: 'local/model', id: 'model', context: 8192, max_output: 1024 },
    allow_unverified: true,
  };
  assertValid('ProviderCreateParams', create);
  assertValid('ProviderCreateParams', { ...create, credential: { mode: 'none' } });
  assertValid('ProviderUpdateParams', { revision: 'revision', provider: 'local', credential: { mode: 'keep' } });
  assert.equal(validate('ProviderCreateParams', { ...create, definition: { ...create.definition, api_key: 'secret' } }), false);
  assert.equal(validate('ProviderCreateParams', { ...create, credential: { mode: 'api_key', key: 123 } }), false);
  assert.equal(validate('ProviderCreateParams', { ...create, manual_model: { ...create.manual_model, max_output: '1024' } }), false);
  assert.equal(validate('ProviderCreateParams', { ...create, revision: 7 }), false);
  assert.equal(validate('ProviderRemoveParams', { provider: 'local' }), false);
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

test('provider picker metadata is optional and retains typed preset presentation', () => {
  const provider = { id: 'openai', name: 'OpenAI', custom: false, methods: ['api_key'], status: { provider: 'openai', configured: true, key_source: 'opencode', available: true, warnings: [] } };
  const inventory = { revision: '1', default_provider: '', providers: [provider] };
  assertValid('ProviderList', inventory);
  const described = { ...provider, category: 'popular', family: 'openai', key_url: 'https://platform.openai.com/api-keys' };
  assertValid('ProviderList', { ...inventory, providers: [described] });
  for (const field of ['category', 'family', 'key_url']) {
    assert.equal(validate('ProviderList', { ...inventory, providers: [{ ...described, [field]: 42 }] }, 'response'), false);
  }
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

test('upcoming schedules are optional, typed, and preserve explicit preview evidence', async () => {
  const fixtures = JSON.parse(await readFile(new URL('../schema/fixtures.json', import.meta.url), 'utf8'));
  const snapshot = structuredClone(fixtures.find(fixture => fixture.type === 'RootSnapshot').value);
  delete snapshot.upcoming_schedules;
  delete snapshot.upcoming_schedule_count;
  assertValid('RootSnapshot', snapshot, 'response');
  snapshot.upcoming_schedules = [];
  snapshot.upcoming_schedule_count = 0;
  assertValid('RootSnapshot', snapshot, 'response');
  const occurrence = { id: 14, next_fire: '2026-09-20T23:00:00Z', prompt: 'preview', prompt_truncated: true };
  snapshot.upcoming_schedules = [occurrence];
  snapshot.upcoming_schedule_count = 3;
  snapshot.omitted = { upcoming_schedules: true };
  assertValid('RootSnapshot', snapshot, 'response');
  for (const invalid of [{ ...occurrence, id: '14' }, { ...occurrence, next_fire: 123 }, { ...occurrence, prompt_truncated: 'yes' }]) {
    assert.equal(validate('RootSnapshot', { ...snapshot, upcoming_schedules: [invalid] }, 'response'), false);
  }
  const missingSlot = { ...occurrence };
  delete missingSlot.next_fire;
  assert.equal(validate('RootSnapshot', { ...snapshot, upcoming_schedules: [missingSlot] }, 'response'), false);
});

test('transcript messages validate optional exposed reasoning', async () => {
  const fixtures = JSON.parse(await readFile(new URL('../schema/fixtures.json', import.meta.url), 'utf8'));
  for (const type of ['RootSnapshot', 'AgentTranscriptResult', 'BoundedTranscriptPage']) {
    const value = structuredClone(fixtures.find(fixture => fixture.type === type).value);
    const message = { role: 'assistant', content: 'Retained response' };
    const entry = { seq: 1, role: message.role, message };
    if (type === 'AgentTranscriptResult') value.page.messages = [entry];
    else if (type === 'BoundedTranscriptPage') value.messages = [entry];
    else value.messages = [message];

    for (const mode of ['request', 'response']) {
      delete message.reasoning_content;
      assertValid(type, value, mode);
      assert.equal(Object.hasOwn(message, 'reasoning_content'), false);
      for (const reasoning of ['', 'Inspect the failing test before editing.']) {
        message.reasoning_content = reasoning;
        assertValid(type, value, mode);
        assert.equal(message.reasoning_content, reasoning);
      }
      for (const reasoning of [null, 42, false, {}, []]) {
        message.reasoning_content = reasoning;
        assert.equal(validate(type, value, mode), false, `${type} must reject malformed reasoning in ${mode} mode`);
      }
    }
  }
});

test('session usage remains valid without optional provenance', async () => {
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
