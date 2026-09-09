import assert from 'node:assert/strict';
import test from 'node:test';
import type { SessionMetadata } from '@whip/protocol';
import type { RecoveryRecord } from '../src/command.js';
import { WhipClient } from '../src/client.js';
import { transportFixture } from './transport-fixture.js';

for (const kind of ['unix', 'websocket'] as const) {
  test(`${kind}: row metadata and archive actions use bounded reads and root-bound durable commands`, async t => {
    const metadata: SessionMetadata = {
      root_id: 'clicked-root', title: 'Complete title '.repeat(20), cwd: '/project/' + 'deep-directory/'.repeat(20),
      history_revision: '9007199254740993', archived: false,
    };
    const records: RecoveryRecord[] = [];
    const fixture = transportFixture({ kind, request(request, connection) {
      if (request.method === 'sessions.get') connection.reply(request, metadata);
      if (request.method === 'sessions.list') connection.reply(request, { revision: '1', items: [], has_more: false });
      if (request.method === 'command.submit') connection.reply(request, {
        operation: 'session.archive', command_id: request.params.command_id, ingress_seq: '1', status: 'succeeded', result: request.params.payload,
      });
    } });
    const client = new WhipClient({ endpoint: fixture.factory, clientId: 'row-actions', reconnect: false,
      recoveryStorage: { async list() { return []; }, async put(record) { records.push(record); }, async delete() {} },
    });
    t.after(() => client.close()); await client.connect();
    assert.deepEqual(await client.sessions.get('clicked-root'), metadata);
    assert.deepEqual(fixture.current.requests.at(-1)!.params, { root_id: 'clicked-root' });
    for (const status of ['active', 'archived', 'all'] as const) {
      await client.sessions.list({ status, limit: 32, max_bytes: 16 << 10 });
      assert.deepEqual(fixture.current.requests.at(-1)!.params, { status, limit: 32, max_bytes: 16 << 10 });
    }
    assert.equal(records.length, 0, 'metadata reads cannot write command recovery or open a session');
    const session = client.session('clicked-root');
    for (const archived of [true, false]) {
      const command = session.archive(archived);
      assert.deepEqual((await command.result()).result, { archived });
      assert.deepEqual(fixture.current.requests.at(-1)!.params, {
        command_id: command.commandId, scope: 'root', root_id: 'clicked-root', operation: 'session.archive', payload: { archived },
      });
      assert.equal(records.at(-1)!.rootId, 'clicked-root');
      assert.equal(records.at(-1)!.operation, 'session.archive');
    }
    assert.ok(fixture.current.requests.every(request => ['initialize', 'sessions.get', 'sessions.list', 'command.submit'].includes(request.method)));
  });
}

test('metadata failures and cancellation remain errors, without fabricating a session or replaying reads', async t => {
  let failure = true;
  const fixture = transportFixture({ request(request, connection) {
    if (request.method === 'sessions.get' && failure) connection.error(request, 'not_found');
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'metadata-errors' });
  t.after(() => client.close()); await client.connect();
  await assert.rejects(client.sessions.get('root'), { kind: 'not_found' });
  failure = false;
  const controller = new AbortController();
  const pending = client.sessions.get('root', { signal: controller.signal });
  controller.abort(); await assert.rejects(pending, { name: 'AbortError' });
  fixture.current.fail(); await client.connect();
  assert.equal(fixture.connections.flatMap(connection => connection.requests).filter(request => request.method === 'sessions.get').length, 2);
});
