import assert from 'node:assert/strict';
import test from 'node:test';
import { WhipClient } from '../src/client.js';
import { scriptedDaemon } from '../src/testing.js';
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
