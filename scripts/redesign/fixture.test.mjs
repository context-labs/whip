import assert from 'node:assert/strict';
import { cp, mkdir, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { basename, join } from 'node:path';
import { test } from 'node:test';
import { createWhipClient } from '../../packages/sdk/dist/index.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';
import { startFixture } from '../../packages/sdk/dist/testing-node.js';

// This process owns one disposable fixture. Keep its files until assertions and
// shutdown both succeed, so failures retain the actual SQLite/WAL and daemon log.
process.env.WHIP_SDK_KEEP_FIXTURE = '1';
const deadline = () => ({ signal: AbortSignal.timeout(15_000) });

test('real runtime preserves admission, transcript and identity across a crash', { timeout: 180_000 }, async t => {
  const fixture = await startFixture({
    retainOnFailure: true,
    env: { WHIP_SDK_AGENTS_FIXTURE: '1' },
  });
  const clients = [];
  const evidence = [];
  let passed = false;
  let closeError;
  const connect = async (transport, clientId) => {
    const client = createWhipClient({
      endpoint: transport === 'unix' ? unixSocket(fixture.info.socket) : fixture.info.endpoint,
      clientId, clientKind: 'automation',
    });
    clients.push(client);
    await client.connect();
    return client;
  };
  try {
    const originalIdentity = fixture.info.runtime_id;
    assert.equal(new URL(fixture.info.endpoint).hostname, '127.0.0.1');
    assert.ok((await stat(join(fixture.directory, 'home/runtime-v2/sessions.db'))).isFile());
    const records = [];
    for (const transport of ['unix', 'websocket']) {
      const clientId = `redesign-${transport}`;
      const client = await connect(transport, clientId);
      const creation = await client.sessions.create({
        cwd: fixture.directory, model: 'model', provider: 'provider', execution_engine: 'starlark',
      }).result(deadline());
      assert.equal(creation.status, 'succeeded', JSON.stringify(creation.failure));
      const rootId = creation.result.root_id;
      const session = client.session(rootId);
      const payload = { text: `phase-zero-${transport}` };
      const commandId = `accepted-${transport}`;
      const command = session.submit(payload, { commandId });
      const accepted = await command.accepted(deadline());
      assert.equal(accepted.command_id, commandId);
      assert.ok(['queued', 'running', 'succeeded'].includes(accepted.status));
      // Detaching this observer must leave the accepted execution owned by Go.
      client.close();
      const observer = await connect(transport, clientId);
      const outcome = await observer.recover(command.record).result(deadline());
      assert.equal(outcome.status, 'succeeded', JSON.stringify(outcome.failure));
      const duplicate = await observer.session(rootId).submit(payload, { commandId }).result(deadline());
      assert.equal(duplicate.command_id, outcome.command_id);
      assert.equal(duplicate.ingress_seq, outcome.ingress_seq);
      const history = await observer.session(rootId).history.page({}, deadline());
      assert.deepEqual(history.messages.map(entry => entry.message.role), ['user', 'assistant']);
      assert.equal(history.messages[0].message.content, payload.text);
      assert.equal(history.messages[1].message.content, `ack: ${payload.text}`);
      const accounting = (await observer.session(rootId).snapshot(deadline())).accounting;
      // A provider request proves this fixture exercised the actual model loop.
      assert.equal(BigInt(accounting.reported_calls) + BigInt(accounting.estimated_calls), 1n);
      records.push({ transport, clientId, rootId, payload, commandId, record: command.record, messages: history.messages });
      evidence.push({ transport, rootId, accepted, outcome, history, accounting });
      observer.close();
    }
    const firstPID = fixture.pid;
    const generation = fixture.info.generation;
    await fixture.crashAndRestart();
    assert.notEqual(fixture.pid, firstPID);
    assert.equal(fixture.info.generation, generation + 1);
    assert.equal(fixture.info.runtime_id, originalIdentity);
    for (const saved of records) {
      const client = await connect(saved.transport, saved.clientId);
      const recovered = await client.recover(saved.record).result(deadline());
      assert.equal(recovered.status, 'succeeded');
      const duplicate = await client.session(saved.rootId).submit(saved.payload, { commandId: saved.commandId }).result(deadline());
      assert.equal(duplicate.ingress_seq, recovered.ingress_seq);
      const history = await client.session(saved.rootId).history.page({}, deadline());
      assert.deepEqual(history.messages, saved.messages);
      const accounting = (await client.session(saved.rootId).snapshot(deadline())).accounting;
      assert.equal(BigInt(accounting.reported_calls) + BigInt(accounting.estimated_calls), 1n);
      evidence.push({ transport: saved.transport, recovered, history });
    }
    const bridge = JSON.parse(await readFile(join(fixture.directory, 'bridge.json'), 'utf8'));
    assert.equal(bridge.generation, generation + 1);
    passed = true;
  } finally {
    for (const client of clients) client.close();
    try { await fixture.close(); } catch (error) { closeError = error; }
    if (!passed || closeError) {
      const artifacts = join('test-results/redesign', basename(fixture.directory));
      await mkdir(artifacts, { recursive: true });
      await cp(fixture.directory, artifacts, { recursive: true, filter: path => !path.endsWith('/daemon.test') && !path.endsWith('.sock') });
      await writeFile(join(artifacts, 'daemon.log'), fixture.output);
      await writeFile(join(artifacts, 'observations.json'), JSON.stringify(evidence, null, 2) + '\n');
      t.diagnostic(`Failure artifacts: ${artifacts}; original fixture: ${fixture.directory}`);
    } else {
      await rm(fixture.directory, { recursive: true, force: true });
      await assert.rejects(readFile(join(fixture.directory, 'bridge.json')), { code: 'ENOENT' });
    }
    if (closeError) throw closeError;
  }
});
