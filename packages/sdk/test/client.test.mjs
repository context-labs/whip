import assert from 'node:assert/strict';
import { test } from 'node:test';
import { Client, RemoteError } from '../dist/index.js';

const initial = { major: 4, minor: 0, runtime_id: 'runtime', builtins: [] };
const success = (request, result) => ({ jsonrpc: '2.0', id: request.id, result });
test('core pins identity, validates before transport and rejects malformed responses', async () => {
  const calls = [];
  const client = await Client.connect(async (request, expected) => {
    calls.push({ request, expected });
    if (request.method === 'initialize') return success(request, initial);
    return success(request, { items: [] });
  }, { clientID: 'test' });
  const history = await client.call('sessions.history', { session_id: 'session', after: '9007199254740993', limit: 10 });
  assert.deepEqual(history, { items: [] });
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
  const preview = { attempt_id: 'attempt', turn_id: 'turn', message_id: 'answer', revision: '1', text: 'partial', calls: [], truncated: false };
  const message = { id: 'answer', session_id: 'session', turn_id: 'turn', input_id: null, mail: null, sequence: '9007199254740993', role: 'assistant', parts: [{ type: 'text', text: 'completed' }], created_at: '2026-09-27T12:00:00Z' };
  const pages = [
    { epoch: 'boot_one', messages: [], preview },
    { epoch: 'boot_one', messages: [message], preview: null },
    { epoch: 'boot_two', messages: [], preview: null },
  ];
  const requests = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return success(request, initial);
    requests.push(request.params);
    return success(request, pages.shift());
  }, { clientID: 'observer' });
  const observation = client.observe('session');
  assert.deepEqual((await observation.next()).value.preview, preview);
  const committed = (await observation.next()).value;
  assert.equal(committed.messages[0].id, preview.message_id);
  assert.equal(committed.preview, null);
  assert.equal((await observation.next()).value.epoch, 'boot_two');
  await observation.return();
  assert.deepEqual(requests.map(request => request.after), ['0', '0', '9007199254740993']);
});

test('aborting a stalled observation stops polling and never sends execution cancellation', async () => {
  const controller = new AbortController();
  const calls = [];
  const client = await Client.connect(async request => {
    calls.push(request.method);
    return success(request, request.method === 'initialize' ? initial : { epoch: 'boot', messages: [], preview: null });
  }, { clientID: 'observer' });
  const observation = client.observe('session', { signal: controller.signal });
  await observation.next();
  const waiting = observation.next();
  controller.abort();
  await assert.rejects(waiting, error => error.name === 'AbortError');
  assert.deepEqual(calls, ['initialize', 'sessions.observe']);
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
