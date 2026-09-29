import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client, Session } from '../dist/index.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
async function clientFixture(run) {
  const calls = [];
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    calls.push(structuredClone(request)); return { jsonrpc: '2.0', id: request.id, result: run(request) };
  }, { clientID: 'client' });
  return { client, calls };
}

test('root and child handles are inert and use exactly the same scoped services', async () => {
  const { client, calls } = await clientFixture(request => {
    if (request.method === 'sessions.get') return { ...fixture('Session'), id: request.params.session_id, parent_id: request.params.session_id === 'child' ? 'root' : null };
    if (request.method === 'questions.list' || request.method === 'mail.list') return { items: [] };
    if (request.method === 'sessions.submit') { const result = fixture('Admission'); result.receipt.identity = request.params.identity; return result; }
    throw new Error(request.method);
  });
  const root = client.session('root'), child = client.sessions.handle('child');
  assert.ok(root instanceof Session); assert.ok(child instanceof Session); assert.equal(calls.length, 0);
  assert.equal((await root.get()).parent_id, null); assert.equal((await child.get()).parent_id, 'root');
  for (const session of [root, child]) {
    await session.questions.list({ session_id: 'wrong', pending_only: true });
    assert.equal(calls.at(-1).params.session_id, session.id);
    await session.mail.list(); assert.equal(calls.at(-1).params.limit, 50);
    const command = session.submission([{ type: 'text', text: session.id }], 'request-' + session.id);
    assert.equal(command.params.session_id, session.id);
    assert.equal(command.params.identity.client_id, 'client');
    await command.send();
    assert.equal(calls.at(-1).params.session_id, session.id);
  }
  assert.throws(() => client.session(''), TypeError);
});

test('scoped handles reject foreign session/turn evidence before a cancellation', async () => {
  const { client, calls } = await clientFixture(request => request.method === 'sessions.get' ? fixture('Session') : fixture('Turn'));
  const child = client.session('child');
  await assert.rejects(child.get(), /identity mismatch/);
  await assert.rejects(child.cancelTurn('turn_title'), /another session/);
  assert.deepEqual(calls.map(value => value.method), ['sessions.get', 'turns.get']);
});

test('scoped input metadata stays bounded and full payload/cancellation are explicit reads', async () => {
  let foreign = false;
  const { client, calls } = await clientFixture(request => {
    if (request.method === 'inputs.page') { const page = structuredClone(fixtures.find(value => value.type === 'InputPageResult' && value.valid && value.value.items?.[0]?.identity).value); for (const item of page.items) item.session_id = 'child'; return page; }
    if (request.method === 'inputs.get' || request.method === 'inputs.cancel') return { ...fixture('Input'), id: 'input', session_id: foreign ? 'foreign' : 'child' };
    throw new Error(request.method);
  });
  const session = client.session('child');
  const page = await session.inputs.page({ state: 'queued', after: '0', limit: 1 });
  assert.ok(page.items.every(item => !('parts' in item)));
  assert.deepEqual(page.items[0].identity, { client_id: 'client_fixture', request_id: 'request_fixture' });
  assert.equal(page.items[0].ordinal, '9007199254740993');
  assert.deepEqual(calls.map(call => call.method), ['inputs.page']);
  assert.ok((await session.inputs.get('input')).parts);
  await session.inputs.cancel('input');
  assert.deepEqual(calls.slice(-2).map(call => call.method), ['inputs.get', 'inputs.cancel']);
  foreign = true;
  await assert.rejects(session.inputs.cancel('input'), /another session/);
  assert.equal(calls.at(-1).method, 'inputs.get');
});


test('design metadata is preserved in exact recoverable input and never invents part coordinates', async () => {
  const { client, calls } = await clientFixture(request => { const result = fixture('Admission'); result.receipt.identity = request.params.identity; return result; });
  const parts = [{ type: 'text', text: 'Literal input' }, { type: 'content', reference_id: 'evidence' }];
  const design = { context_attachment_id: 'evidence', elements: [{ label: 'Save', selector: 'button.save' }], element_count: 1, page_title: 'Selected page' };
  const command = client.session('child').submission(parts, 'design', { designContext: design });
  design.page_title = 'Edited later';
  assert.equal(command.params.design_context.page_title, 'Selected page');
  await command.send();
  assert.deepEqual(calls.at(-1).params.parts, parts);
  assert.equal(calls.at(-1).params.design_context.page_title, 'Selected page');
  assert.equal('context_part_index' in calls.at(-1).params.design_context, false);
  await client.session('child').submit(parts, 'direct-design', { designContext: design });
  assert.equal(calls.at(-1).params.design_context.page_title, 'Edited later');
  assert.throws(() => client.session('child').submission(parts, 'invalid', { designContext: { ...design, context_part_index: 1 } }), TypeError);
});

test('pending permission pages preserve exact owner and exclusive operation cursor', async () => {
  const { client, calls } = await clientFixture(request => {
    assert.equal(request.method, 'permissions.list');
    return { items: [] };
  });
  await client.session('child').permissions.list({ pending_only: true, after: 'operation_100', limit: 1 });
  assert.deepEqual(calls.at(-1).params, { limit: 1, pending_only: true, after: 'operation_100', session_id: 'child' });
  await client.session('child').permissions.list();
  assert.deepEqual(calls.at(-1).params, { limit: 50, session_id: 'child' });
});

test('grant revocation is scoped atomically and an uncertain write is never replayed', async () => {
  let foreign = false;
  let fail = false;
  const grant = { id: 'grant', session_id: 'child', capability: 'files.read', resource: '/workspace', operation_id: null, issuer_id: 'parent-grant', created_at: '2026-09-28T00:00:00Z', revoked_at: '2026-09-28T00:01:00Z' };
  const { client, calls } = await clientFixture(request => {
    if (fail) throw new Error('lost acknowledgement');
    const value = { ...grant, session_id: foreign ? 'other' : 'child' };
    return request.method === 'grants.list' ? { items: [value] } : value;
  });
  const session = client.session('child');
  await session.grants.list({ session_id: 'wrong' });
  await session.grants.revoke('grant');
  assert.deepEqual(calls.at(-1).params, { grant_id: 'grant', session_id: 'child' });
  foreign = true;
  await assert.rejects(session.grants.list(), /another session/);
  await assert.rejects(session.grants.revoke('grant'), /identity mismatch/);
  fail = true;
  const before = calls.length;
  await assert.rejects(session.grants.revoke('grant'), /lost acknowledgement/);
  assert.equal(calls.length, before + 1);
});

test('child names and template aliases pass through without becoming routing identities', async () => {
  const { client, calls } = await clientFixture(request => {
    if (request.method === 'sessions.spawn') {
      const session = { ...fixture('Session'), id: 'child', parent_id: 'root', name: request.params.name };
      const admission = fixture('Admission'); admission.receipt.identity = request.params.identity;
      admission.input = { ...fixture('Input'), session_id: 'child' };
      return { session, admission };
    }
    if (request.method === 'sessions.get') return { ...fixture('Session'), id: request.params.session_id, parent_id: 'root', name: 'Repository reviewer' };
    throw new Error(request.method);
  });
  const params = { name: 'Repository reviewer', template: 'review', parts: [{ type: 'text', text: 'Review' }], overrides: {}, grant_ids: null };
  const first = await client.session('root').spawn(params, 'named-spawn');
  const retried = await client.session('root').spawn(params, 'named-spawn');
  assert.equal(first.session.name, 'Repository reviewer');
  assert.equal(first.session.id, 'child');
  assert.deepEqual(calls[0].params, { ...params, parent_id: 'root', identity: { client_id: 'client', request_id: 'named-spawn' } });
  assert.deepEqual(calls[1].params, calls[0].params);
  assert.equal(retried.session.id, first.session.id);
  assert.equal((await client.session(first.session.id).get()).name, first.session.name);
  assert.equal(calls.at(-1).params.session_id, 'child');
});
