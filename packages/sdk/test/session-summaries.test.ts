import assert from 'node:assert/strict';
import test from 'node:test';
import type { SessionSummariesResult } from '@whip/protocol';
import { WhipClient } from '../src/client.js';
import { transportFixture } from './transport-fixture.js';

const summary = {
  root_id: 'root', missing: false, title: 'Working session', cwd: '/project', workspace_id: 'workspace',
  running_agents: '9007199254740993', queued_agents: '2', pending_permissions: '1', pending_questions: '3', truncated: false,
};
const result: SessionSummariesResult = { items: [summary, {
  root_id: 'absent', missing: true, title: '', cwd: '', running_agents: '0', queued_agents: '0',
  pending_permissions: '0', pending_questions: '0', truncated: false,
}] };

for (const kind of ['unix', 'websocket'] as const) {
  test(`${kind} session summaries are bounded typed queries without view or journal ownership`, async t => {
    const stored: unknown[] = [];
    const fixture = transportFixture({ kind, initialize: { protocol_minor: 1, capabilities: ['session_summaries'], negotiated_capabilities: ['session_summaries'] },
      request(request, connection) {
        if (request.method === 'sessions.summaries') connection.reply(request, result);
      },
    });
    const client = new WhipClient({ endpoint: fixture.factory, clientId: 'summary-client', reconnect: false,
      recoveryStorage: { async list() { return []; }, async put(value) { stored.push(value); }, async delete() {} },
    });
    t.after(() => client.close());
    await client.connect();
    const capabilities = fixture.current.requests[0]!.params.capabilities;
    assert.ok(capabilities instanceof Array);
    assert.ok(capabilities.includes('session_summaries'));
    const ids = ['root', 'absent'] as const;
    const read = await client.sessions.summaries(ids);
    assert.deepEqual(read, result);
    assert.equal(read.items[0]!.running_agents, '9007199254740993');
    assert.equal(read.items[1]!.missing, true);
    assert.deepEqual(fixture.current.requests.at(-1)!.params, { root_ids: ids });
    const sent = fixture.current.requests.length;
    for (const invalid of [['root', 'root'], [''], Array.from({ length: 33 }, (_, index) => `root-${index}`), ['a'.repeat(257)]]) {
      await assert.rejects(client.sessions.summaries(invalid));
    }
    assert.equal(fixture.current.requests.length, sent, 'invalid requests reached the daemon');
    assert.deepEqual(stored, []);
    assert.ok(fixture.current.requests.every(request => ['initialize', 'sessions.summaries'].includes(request.method)));
  });
}

test('summary errors, local aborts and disconnection never fabricate missing sessions or replay queries', async t => {
  let fail = true;
  const fixture = transportFixture({ request(request, connection) {
    if (request.method === 'sessions.summaries' && fail) connection.error(request, 'internal_error');
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'summary-errors' });
  t.after(() => client.close());
  await client.connect();
  await assert.rejects(client.sessions.summaries(['root']), { kind: 'internal_error' });
  fail = false;
  const abort = new AbortController();
  const aborted = client.sessions.summaries(['root'], { signal: abort.signal });
  abort.abort();
  await assert.rejects(aborted, { name: 'AbortError' });
  const pending = client.sessions.summaries(['root']);
  fixture.current.fail();
  await assert.rejects(pending);
  await client.connect();
  assert.equal(fixture.connections.flatMap(connection => connection.requests).filter(request => request.method === 'sessions.summaries').length, 3);
});

test('an older host without session summaries remains connectable and refuses the unavailable operation locally', async t => {
  const fixture = transportFixture();
  fixture.info.operations = fixture.info.operations!.filter(operation => operation.name !== 'sessions.summaries');
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'older-host', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  assert.equal(client.getSnapshot().state, 'connected');
  assert.equal(client.supports('rpc', 'sessions.summaries'), false);
  await assert.rejects(client.sessions.summaries(['root']), { kind: 'unsupported_operation' });
  assert.equal(fixture.current.requests.length, 1);
});
