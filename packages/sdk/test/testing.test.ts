import assert from 'node:assert/strict';
import test from 'node:test';
import { WhipClient } from '../src/client.js';
import { scriptedDaemon } from '../src/testing.js';
import { defineAgent, tool } from '../src/agents.js';
import type { StandardSchemaWithJSON } from '../src/schema.js';
import type { QuestionEvent, TurnEvent } from '../src/turn.js';

test('a scripted turn drives session.run end to end without a daemon', async t => {
  const daemon = scriptedDaemon();
  const client = new WhipClient({ endpoint: daemon.factory, clientId: 'consumer', reconnect: false, commandPollMs: 5 });
  t.after(() => client.close());
  await client.connect();
  const played = daemon.turn('root', {
    steps: [
      { text: 'Looking up ticket 42. ' },
      { host: { id: '1:1', operation: 'tools.lookup_ticket', summary: 'id=42' } },
      { hook: { hook: 'before_tool', operation: 'shell.run', decision: 'deny', reason: 'destructive' } },
      { question: { id: 'q-1', question: 'Escalate?', options: [{ label: 'Yes' }, { label: 'No' }] } },
      { text: 'Escalated.' },
    ],
    text: 'Looking up ticket 42. Escalated.',
    output: { ticket: '42', escalatedTo: 'auth' },
  });
  const turn = client.session('root').run('Triage ticket 42');
  const seen: string[] = [];
  for await (const event of turn) {
    seen.push(event.type);
    if (event.type === 'question') await (event as QuestionEvent).answer(['Yes']);
  }
  await played;
  assert.deepEqual(seen, ['raw', 'text', 'host', 'host', 'hook', 'question', 'text', 'end']);
  const result = await turn.result();
  assert.deepEqual(result, { status: 'succeeded', turnId: 'root-turn-1', text: 'Looking up ticket 42. Escalated.', output: { ticket: '42', escalatedTo: 'auth' } });
  const answer = daemon.current;
  assert.deepEqual(answer.requests.filter(request => request.method === 'command.submit' && request.params.operation === 'question.answer').map(request => request.params.payload), [{ id: 'q-1', answer: ['Yes'], dismissed: false }]);
  // A failing turn settles from its failure.
  void daemon.turn('root', { status: 'failed', failure: { code: -32000, message: 'model unavailable', data: { kind: 'execution_failed' } } });
  const failed = await client.session('root').run('again').result();
  assert.deepEqual(failed, { status: 'failed', turnId: 'root-turn-2', failure: { code: -32000, message: 'model unavailable', data: { kind: 'execution_failed' } } });
});

test('reply tables and emitted events compose with the legacy request callback', async t => {
  const daemon = scriptedDaemon({ request(request, connection) { if (request.method === 'sessions.revision') connection.reply(request, { revision: '7' }); } })
    .reply('daemon.ping', () => ({ generation: '1', build_id: 'scripted' }));
  const client = new WhipClient({ endpoint: daemon.factory, clientId: 'consumer', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  assert.deepEqual(await client.call('daemon.ping', {}), { generation: '1', build_id: 'scripted' });
  assert.deepEqual(await client.call('sessions.revision', {}), { revision: '7' });
  daemon.serveSessions();
  const events: TurnEvent[] = [];
  const subscription = await client.events.subscribe('root', '0');
  daemon.emit('root', 'stream.notice', { text: 'hello' });
  const first = await subscription.next();
  assert.equal(first.value?.kind, 'stream.notice');
  assert.equal(first.value?.seq, '1');
  await subscription.dispose();
  assert.equal(events.length, 0);
});

test('a served agent types its turn result by the output contract through the scripted daemon', async t => {
  const contract: StandardSchemaWithJSON<{ ticket: string; escalatedTo: string | null }> = { '~standard': { version: 1, vendor: 'hand-rolled', validate: value => ({ value: value as { ticket: string; escalatedTo: string | null } }),
    jsonSchema: { input: () => ({ type: 'object' }), output: () => ({ type: 'object', properties: { ticket: { type: 'string' }, escalatedTo: { type: ['string', 'null'] } }, required: ['ticket', 'escalatedTo'] }) } } };
  const lookup = tool({ name: 'lookup_ticket', description: 'd', input: { type: 'object' }, execute: () => ({}) });
  const agent = defineAgent({ id: 'support-triage', modules: ['context'], tools: [lookup], output: contract });
  const daemon = scriptedDaemon().serveSessions();
  const client = new WhipClient({ endpoint: daemon.factory, clientId: 'consumer', reconnect: false, commandPollMs: 5 });
  t.after(() => client.close());
  await client.connect();
  const runtime = await client.agents.serve(agent);
  assert.equal(runtime.revision, 'scripted-revision-support-triage');
  const session = await runtime.sessions.create({ cwd: '/srv' });
  assert.equal(session.rootId, 'root-1');
  void daemon.turn(session.rootId, { steps: [{ text: 'Escalated.' }], text: 'Escalated.', output: { ticket: '42', escalatedTo: 'auth' } });
  const result = await session.run('Triage ticket 42').result();
  assert.equal(result.status, 'succeeded');
  if (result.status === 'succeeded') {
    const escalated: string | null = result.output.escalatedTo; // typed by the contract, no cast
    assert.deepEqual([result.output.ticket, escalated], ['42', 'auth']);
  }
  await assert.rejects(runtime.sessions.open('root-9'), { kind: 'conflict' });
});
