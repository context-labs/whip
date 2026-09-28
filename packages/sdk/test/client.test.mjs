import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, DeliveryError, RemoteError } from '../dist/index.js';

const initial = { major: 4, minor: 0, runtime_id: 'runtime', builtins: [] };
const historySnapshot = { session_id: 'session', revision: '1', through_sequence: '9007199254740993', message_count: '1' };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });
test('core pins identity, validates before transport and rejects malformed responses', async () => {
  const calls = [];
  const client = await Client.connect(async (request, expected) => {
    calls.push({ request, expected });
    if (request.method === 'initialize') return success(request, initial);
    return success(request, { snapshot: historySnapshot, items: [] });
  }, { clientID: 'test' });
  const history = await client.call('sessions.history', { session_id: 'session', after: '9007199254740993', limit: 10 });
  assert.deepEqual(history, { snapshot: historySnapshot, items: [] });
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


test('observation advances exact cursors, reconciles preview IDs, and clears on a new process epoch', async () => {
  const preview = { attempt_id: 'attempt', turn_id: 'turn', message_id: 'answer', revision: '1', text: 'partial', reasoning: 'considering', calls: [], truncated: false };
  const reasoning = { ...preview, revision: '2', reasoning: 'considering the request' };
  const retry = { ...preview, attempt_id: 'retry', message_id: 'retry_answer', reasoning: '', text: '', revision: '0' };
  const message = { id: 'retry_answer', session_id: 'session', group_id: 'turn', opening_input: false, source: null, retired_by: null, retired_revision: null, turn_id: 'turn', input_id: null, mail: null, sequence: '9007199254740993', role: 'assistant', parts: [{ type: 'text', text: 'completed' }], created_at: '2026-09-27T12:00:00Z' };
  const pages = [
    { snapshot: historySnapshot, epoch: 'boot_one', messages: [], preview },
    { snapshot: historySnapshot, epoch: 'boot_one', messages: [], preview: reasoning },
    { snapshot: historySnapshot, epoch: 'boot_one', messages: [], preview: retry },
    { snapshot: historySnapshot, epoch: 'boot_one', messages: [message], preview: null },
    { snapshot: historySnapshot, epoch: 'boot_two', messages: [], preview: null },
  ];
  const requests = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    requests.push(request.params);
    return success(request, pages.shift());
  }, { clientID: 'observer' });
  const observation = client.observe('session');
  assert.deepEqual((await observation.next()).value.preview, preview);
  assert.deepEqual((await observation.next()).value.preview, reasoning);
  assert.deepEqual((await observation.next()).value.preview, retry);
  const committed = (await observation.next()).value;
  assert.equal(committed.messages[0].id, retry.message_id);
  assert.equal(committed.preview, null);
  assert.equal((await observation.next()).value.epoch, 'boot_two');
  await observation.return();
  assert.deepEqual(requests.map(request => request.after), ['0', '0', '0', '0', '9007199254740993']);
});

test('aborting a stalled observation stops polling and never sends execution cancellation', async () => {
  const controller = new AbortController();
  const calls = [];
  const client = await Client.connect(async request => {
    calls.push(request.method);
    return success(request, request.method === 'initialize' ? initial : { snapshot: historySnapshot, epoch: 'boot', messages: [], preview: null });
  }, { clientID: 'observer' });
  const observation = client.observe('session', { signal: controller.signal });
  await observation.next();
  const waiting = observation.next();
  controller.abort();
  await assert.rejects(waiting, error => error.name === 'AbortError');
  assert.deepEqual(calls, ['initialize', 'sessions.observe']);
});

test('observation resets its bounded cursor on rewind and emits an empty new revision', async () => {
  const requests = [];
  const oldRevision = '9007199254740993';
  const newRevision = '9007199254740994';
  const retained = { id: 'retained', session_id: 'session', group_id: 'imported', opening_input: true, source: { session_id: 'source', message_id: 'original', sequence: '9007199254740993' }, retired_by: null, retired_revision: null, turn_id: null, input_id: null, mail: null, sequence: '1', role: 'user', parts: [{ type: 'text', text: 'retained' }], created_at: '2026-09-27T12:00:00Z' };
  let requestNumber = 0;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    requests.push(request.params);
    requestNumber++;
    if (requestNumber === 1 || requestNumber === 3) return { jsonrpc: '2.0', id: request.id, error: { code: -32009, message: 'history revision changed', kind: 'CONFLICT' } };
    const revision = requestNumber === 2 ? newRevision : '9007199254740995';
    const messages = requestNumber === 2 ? [retained] : [];
    return success(request, { epoch: 'same_process', snapshot: { session_id: 'session', revision, through_sequence: messages.length ? '1' : '0', message_count: messages.length ? '1' : '0' }, messages, preview: null });
  }, { clientID: 'observer' });
  const observation = client.observe('session', { after: '9007199254740993', expectedRevision: oldRevision });
  const rewound = (await observation.next()).value;
  assert.equal(rewound.snapshot.revision, newRevision);
  assert.deepEqual(rewound.messages, [retained]);
  const cleared = (await observation.next()).value;
  assert.equal(cleared.snapshot.revision, '9007199254740995');
  assert.deepEqual(cleared.messages, []);
  await observation.return();
  assert.deepEqual(requests.map(value => [value.after, value.expected_revision]), [
    ['9007199254740993', oldRevision], ['0', undefined], ['1', newRevision], ['0', undefined],
  ]);
});

test('resuming observation requires its history revision and transport failures do not restart it', async () => {
  const requests = [];
  const failure = new DeliveryError('lost connection');
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    requests.push(request);
    throw failure;
  }, { clientID: 'observer' });
  await assert.rejects(client.observe('session', { after: '2' }).next(), /history revision/);
  assert.equal(requests.length, 0);
  await assert.rejects(client.observe('session', { after: '2', expectedRevision: '1' }).next(), error => error === failure);
  assert.equal(requests.length, 1);
});

test('rewind retries preserve the edit ID and exact observed snapshot after a lost acknowledgement', async () => {
  const requests = [];
  const params = { session_id: 'session', expected_revision: '9007199254740993', observed_through: '9007199254740994', keep_through: '0' };
  const edit = { id: 'stable_edit', ...params, digest: 'a'.repeat(64), revision: '9007199254740994', created_at: '2026-09-28T12:00:00Z' };
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    requests.push(request);
    if (requests.length === 1) throw new DeliveryError('lost acknowledgement');
    return success(request, edit);
  }, { clientID: 'history' });
  await assert.rejects(client.rewind(params, 'stable_edit'), DeliveryError);
  assert.deepEqual(await client.rewind(params, 'stable_edit'), edit);
  assert.deepEqual(requests.map(value => value.method), ['sessions.rewind', 'sessions.rewind']);
  assert.deepEqual(requests[0].params, { ...params, edit_id: 'stable_edit' });
  assert.deepEqual(requests[1].params, requests[0].params);
  await assert.rejects(client.rewind({ ...params, expected_revision: 9007199254740993 }, 'stable_edit'), TypeError);
  assert.equal(requests.length, 2);
});

test('resource calls preserve exact limits and validate before transport', async () => {
  const calls = [];
  const value = { session_id: 'child', kind: 'queued_inputs', revision: '9007199254740993', limit: null, used: '9007199254740994' };
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request);
    return success(request, request.method === 'resources.list' ? { items: [value] } : value);
  }, { clientID: 'resources' });
  assert.deepEqual(await client.call('resources.list', { session_id: 'child' }), { items: [value] });
  const params = { session_id: 'child', expected_revision: '9007199254740992', resource: { kind: 'queued_inputs', limit: null } };
  assert.deepEqual(await client.call('resources.set', params), value);
  assert.deepEqual(calls[1].params, params);
  await assert.rejects(client.call('resources.set', { ...params, expected_revision: 9007199254740992 }), TypeError);
  await assert.rejects(client.call('resources.set', { ...params, resource: { kind: 'queued_inputs', limit: 1 } }), TypeError);
  assert.equal(calls.length, 2);
});

test('schedule retries retain caller identity and cursors retain exact nanoseconds', async () => {
  const requests = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    requests.push(request);
    if (request.method === 'schedules.create') return success(request, { id: request.params.schedule_id, schedule: null, deleted_at: '2026-09-28T12:00:00Z' });
    return success(request, { items: [], next_after: null, next_cursor: null });
  }, { clientID: 'scheduler' });
  const template = { session_id: 'session', expression: '@every 0.000000001s', parts: [{ type: 'text', text: 'exact' }] };
  await client.createSchedule(template, 'stable'); await client.createSchedule(template, 'stable');
  assert.deepEqual(requests[0].params, requests[1].params);
  const cursor = { id: 'stable', due: '2500-01-02T03:04:05.123456789Z' };
  await client.call('schedules.list', { session_id: 'session', upcoming: true, cursor, limit: 1 });
  assert.deepEqual(requests[2].params.cursor, cursor);
  await assert.rejects(client.call('schedules.list', { session_id: 'session', limit: 101 }), TypeError);
  await assert.rejects(client.call('schedules.list', { session_id: 'session', upcoming: true, cursor: { ...cursor, due: cursor.due.replace('789Z', '7891Z') }, limit: 1 }), TypeError);
  assert.equal(requests.length, 3);
});

test('goals preserve stable creation IDs, exact allowances, and ordinary resume identities', async () => {
  const calls = [];
  const goal = { id: 'goal', revision: '9007199254740993', session_id: 'session', spec: { text: 'objective', max_continuations: '9007199254740994' }, state: 'armed', continuations_used: '0', stop_reason: null, completion_turn_id: null, completion_operation_id: null, origin_formulation_attempt_id: null, created_at: '2026-09-27T12:00:00Z' };
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request);
    if (request.method === 'goals.create') return success(request, { id: goal.id, goal, current: true, initial: null, deleted_at: null });
    if (request.method === 'goals.current') return success(request, { goal });
    if (request.method === 'goals.get') return success(request, goal);
    if (request.method === 'goals.cancel') return success(request, { goal: { ...goal, state: 'cancelled' }, cancel_turn_id: null });
    return { jsonrpc: '2.0', id: request.id, error: { code: -32009, message: 'conflict', kind: 'CONFLICT' } };
  }, { clientID: 'test' });
  const params = { session_id: 'session', expected_current: null, spec: { text: 'objective', max_continuations: '9007199254740994' }, start: false };
  assert.equal((await client.createGoal(params, 'goal')).goal.spec.max_continuations, '9007199254740994');
  assert.deepEqual(calls[0].params, { ...params, goal_id: 'goal' });
  assert.equal((await client.currentGoal('session')).goal.revision, '9007199254740993');
  assert.equal((await client.getGoal('session', 'goal')).state, 'armed');
  await assert.rejects(client.resumeGoal('session', { id: 'goal', revision: '9007199254740993' }, 'resume'), error => error.kind === 'CONFLICT');
  assert.deepEqual(calls.at(-1).params.identity, { client_id: 'test', request_id: 'resume' });
  assert.equal((await client.cancelGoal('session', 'goal')).goal.state, 'cancelled');
  const before = calls.length;
  await assert.rejects(client.createGoal({ ...params, spec: { text: 'objective', max_continuations: 0 } }, 'bad'), TypeError);
  assert.equal(calls.length, before);
});


test('observation validators require a string reasoning preview', async () => {
  for (const reasoning of [undefined, null, 42, { text: 'not a string' }]) {
    const preview = { attempt_id: 'attempt', turn_id: 'turn', message_id: 'message', revision: '1', text: '', reasoning, calls: [], truncated: false };
    const client = await Client.connect(async request => success(request, request.method === 'initialize' ? initial : { snapshot: historySnapshot, epoch: 'boot', messages: [], preview }), { clientID: 'observer' });
    await assert.rejects(client.call('sessions.observe', { session_id: 'session', after: '0', limit: 100 }), TypeError);
  }
});

test('formulation preserves receipt identity and historical acceptance independently of maintenance outcome', async () => {
  const calls = [];
  const created = '2026-09-28T12:00:00Z';
  const request = { goal_id: 'goal', expected_current: null, max_continuations: '9007199254740993', start: false };
  const input = { id: 'input', session_id: 'session', source: 'user', kind: 'goal_formulation', parts: [], state: 'claimed', turn_id: 'turn', goal: null, schedule: null, created_at: created };
  const turn = { id: 'turn', session_id: 'session', kind: 'goal_formulation', goal: null, history_revision: '1', config_revision: '1', state: 'interrupted', failure: 'runtime stopped', started_at: created, finished_at: created };
  const admission = { receipt: { identity: { client_id: 'test', request_id: 'stable' }, digest: 'a'.repeat(64), input_id: 'input', deleted_at: null, created_at: created }, input, turn };
  const candidate = { history_revision: '1', input_id: 'input', session_id: 'session', request: { ...request, tail_messages: 8 }, after_sequence: '9007199254740993', through_sequence: '9007199254740994', turn_id: 'turn', attempt_id: 'attempt', text: 'Accepted objective', accepted: true, rejection: null, created_at: created };
  const client = await Client.connect(async message => {
    if (message.method === 'initialize') return success(message, initial);
    calls.push(message);
    if (message.method === 'goals.formulation') return success(message, candidate);
    return success(message, admission);
  }, { clientID: 'test' });
  const params = { session_id: 'session', request };
  await client.formulateGoal(params, 'stable');
  await client.formulateGoal(params, 'stable');
  assert.deepEqual(calls[0].params, calls[1].params);
  assert.deepEqual(calls[0].params, { ...params, identity: { client_id: 'test', request_id: 'stable' } });
  assert.equal((await client.wait('stable')).turn.state, 'interrupted');
  const evidence = await client.getGoalFormulation('session', 'attempt');
  assert.equal(evidence.accepted, true);
  assert.equal(evidence.request.start, false);
  assert.equal(evidence.request.max_continuations, '9007199254740993');
  assert.equal(evidence.through_sequence, '9007199254740994');
  assert.deepEqual(calls.at(-1).params, { session_id: 'session', attempt_id: 'attempt' });
  const before = calls.length;
  for (const tail_messages of [1, -1, 101, '8']) {
    await assert.rejects(client.formulateGoal({ ...params, request: { ...request, tail_messages } }, 'invalid'), TypeError);
  }
  await assert.rejects(client.formulateGoal({ ...params, request: { ...request, max_continuations: 0 } }, 'invalid'), TypeError);
  await assert.rejects(client.formulateGoal({ ...params, request: { ...request, extra: true } }, 'invalid'), TypeError);
  assert.equal(calls.length, before);
  await client.formulateGoal({ ...params, request: { ...request, max_continuations: '0', tail_messages: 2 } }, 'zero');
  assert.equal(calls.at(-1).params.request.max_continuations, '0');
});

test('account helpers observe ephemeral flows without cache, polling, or timestamp coercion', async () => {
  const id = 'AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB';
  const expiry = '2500-01-02T03:04:05.123456789Z';
  const flow = { id, state: 'authorizing', verification_url: 'https://auth.openai.com/codex/device', user_code: 'SAFE-CODE', expires_at: expiry, failure: null };
  const status = { auth_state: 'stored', route_state: 'configured', account_id: 'account', email: null, plan: null, expires_at: expiry, failure: null };
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request);
    if (request.method === 'accounts.openai.list') return success(request, { items: [structuredClone(flow)] });
    if (request.method === 'accounts.openai.get' || request.method === 'accounts.openai.begin') return success(request, structuredClone(flow));
    if (request.method === 'accounts.openai.cancel') return success(request, { ...flow, state: 'cancelled', verification_url: null, user_code: null });
    return success(request, status);
  }, { clientID: 'account-observer' });
  const accepted = await client.beginOpenAILogin();
  accepted.user_code = 'local change';
  assert.equal((await client.listOpenAILogins()).items[0].user_code, 'SAFE-CODE');
  assert.equal((await client.getOpenAILogin(id)).expires_at, expiry);
  assert.equal((await client.cancelOpenAILogin(id)).state, 'cancelled');
  assert.equal((await client.openAIAccountStatus()).expires_at, expiry);
  assert.deepEqual(await client.setupOpenAIAccount(), status);
  assert.deepEqual(await client.logoutOpenAIAccount(), status);
  assert.deepEqual(calls.map(call => call.method), ['accounts.openai.begin', 'accounts.openai.list', 'accounts.openai.get', 'accounts.openai.cancel', 'accounts.openai.status', 'accounts.openai.setup', 'accounts.openai.logout']);
  assert.deepEqual(calls[0].params, {});
  assert.deepEqual(calls[2].params, { flow_id: id });
  await assert.rejects(client.getOpenAILogin('invalid'), TypeError);
  const controller = new AbortController(); controller.abort();
  await assert.rejects(client.beginOpenAILogin({ signal: controller.signal }), error => error.name === 'AbortError');
  assert.equal(calls.length, 7);
});

test('account errors and interrupted epochs stay explicit; malformed expiry fails closed', async () => {
  const id = 'AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB';
  const interrupted = { id, state: 'interrupted', verification_url: null, user_code: null, expires_at: null, failure: null };
  let response = interrupted;
  const client = await Client.connect(async request => request.method === 'initialize' ? success(request, initial) : success(request, response), { clientID: 'account' });
  assert.deepEqual(await client.getOpenAILogin(id), interrupted);
  for (const expiry of ['2026-02-29T00:00:00Z', '2026-01-01T00:00:00.1234567891Z', '2026-01-01T00:00:00.10Z', '2026-01-01T00:00:00+00:00']) {
    response = { ...interrupted, expires_at: expiry };
    await assert.rejects(client.getOpenAILogin(id), TypeError);
  }
  const failed = await Client.connect(async request => request.method === 'initialize' ? success(request, initial) : { jsonrpc: '2.0', id: request.id, error: { code: -32021, kind: 'ACCOUNT_SETUP', message: 'Saved login needs route setup' } }, { clientID: 'account' });
  await assert.rejects(failed.setupOpenAIAccount(), error => error instanceof RemoteError && error.kind === 'ACCOUNT_SETUP');
});


test('lost account begin acknowledgement requires explicit flow recovery, never a SQL receipt or automatic retry', async () => {
  const flow = { id: 'AAAAAAAAAAAAAAAAAAAAAAAAAA:BBBBBBBBBBBBBBBBBBBBBBBBBB', state: 'authorizing', verification_url: null, user_code: null, expires_at: '2026-09-28T12:00:00.000000001Z', failure: null };
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    calls.push(request.method);
    if (request.method === 'accounts.openai.begin') throw new DeliveryError('acknowledgement lost');
    return success(request, request.method === 'accounts.openai.list' ? { items: [flow] } : flow);
  }, { clientID: 'account-recovery' });
  await assert.rejects(client.beginOpenAILogin(), DeliveryError);
  assert.deepEqual(calls, ['accounts.openai.begin']);
  const found = await client.listOpenAILogins();
  assert.equal((await client.getOpenAILogin(found.items[0].id)).id, flow.id);
  assert.deepEqual(calls, ['accounts.openai.begin', 'accounts.openai.list', 'accounts.openai.get']);
});
