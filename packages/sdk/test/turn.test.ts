import assert from 'node:assert/strict';
import test from 'node:test';
import { WhipClient } from '../src/client.js';
import type { PermissionEvent, QuestionEvent, TurnEvent } from '../src/turn.js';
import { transportFixture, type FixtureConnection, type FixtureRequest } from './transport-fixture.js';

function snapshot(rootId: string, cursor: string, extra: Record<string, unknown> = {}) {
  return {
    root_id: rootId, cursor, history_revision: '1', active_turns: {},
    meta: { id: rootId, kind: 'agent', title: '', model: 'm', provider: 'p', cwd: '/srv', execution_engine: 'starlark', definition: 'coding', definition_revision: '',
      goal: '', forked_from: '', fork_seq: 0, tags: [], archived: false, pinned: false, effort: '', usage_in: 0, usage_cached: 0, usage_out: 0, updated_at: '' },
    messages: [], message_seqs: [], presentation: [], agent_presentations: {}, agents: [], inbox: [], blackboard: [],
    budgets: [], capabilities: [], schedules: [], permissions: [], questions: [], ...extra,
  };
}

/** A daemon that admits one submit, answers status polls from a mutable outcome, and lets the test push journal events. */
function turnDaemon(snapshotExtra: Record<string, unknown> = {}) {
  const commands = new Map<string, Record<string, unknown>>();
  let subscriptionId = '';
  let ingress = 6;
  const fixture = transportFixture({ request(request: FixtureRequest, connection: FixtureConnection) {
    switch (request.method) {
      case 'root.snapshot': connection.reply(request, snapshot(String(request.params.root_id), '3', snapshotExtra)); break;
      case 'permission.decide': connection.reply(request, { operation_id: 'op-1', lease_id: 'lease-1' }); break;
      case 'events.subscribe': subscriptionId = String(request.params.subscription_id); connection.reply(request, { subscription_id: subscriptionId, cursor: request.params.cursor }); break;
      case 'events.unsubscribe': connection.reply(request, {}); break;
      case 'command.submit': {
        const id = String(request.params.command_id);
        const immediate = request.params.operation !== 'submit';
        const record = { operation: request.params.operation, command_id: id, ingress_seq: String(++ingress), status: immediate ? 'succeeded' : 'running', ...(immediate ? { result: {} } : {}) };
        commands.set(id, record);
        connection.reply(request, record);
        break;
      }
      case 'command.status': connection.reply(request, commands.get(String(request.params.command_id))); break;
    }
  } });
  let seq = 3;
  const emit = (kind: string, payload: Record<string, unknown>) => {
    fixture.current.notify('event', { event: { root_id: 'root', subscription_id: subscriptionId, seq: String(++seq), kind, payload } });
  };
  const settle = (status: string, body: Record<string, unknown>) => {
    for (const record of commands.values()) if (record.operation === 'submit') Object.assign(record, { status, ...body });
  };
  const methods = () => fixture.current.requests.map(request => request.method);
  return { fixture, emit, settle, methods, commands };
}

async function until(check: () => boolean, what: string): Promise<void> {
  const deadline = Date.now() + 5000;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}`);
    await new Promise(resolve => setTimeout(resolve, 5));
  }
}

const root = (turn: string, extra: Record<string, unknown> = {}) => ({ agent_id: 'root', turn_id: turn, ...extra });

test('a turn subscribes before it submits, scopes events to its own turn, and settles from the command', async t => {
  const daemon = turnDaemon();
  const client = new WhipClient({ endpoint: daemon.fixture.factory, clientId: 'turns', reconnect: false, commandPollMs: 5 });
  t.after(() => client.close());
  await client.connect();
  const turn = client.session('root').run('Triage ticket 42');
  await until(() => daemon.methods().includes('command.submit'), 'the submit');
  assert.deepEqual(daemon.methods(), ['initialize', 'root.snapshot', 'events.subscribe', 'command.submit'], 'the subscription is admitted before the command is sent');
  daemon.emit('turn.started', root('turn-0', { inbox_seq: '2', status: 'running' }));       // an earlier turn still journaling
  daemon.emit('stream.text', root('turn-0', { text: 'stale ' }));
  daemon.emit('turn.started', root('turn-1', { inbox_seq: '7', status: 'running' }));
  assert.equal(await turn.turnId, 'turn-1');
  daemon.emit('stream.text', root('turn-1', { text: 'Hel' }));
  daemon.emit('stream.text', root('turn-1', { text: 'lo' }));
  daemon.emit('stream.text', root('turn-0', { text: 'never' }));
  daemon.emit('question.pending', root('turn-1', { question_id: 'q-1', question: 'Escalate?', options: [{ label: 'Yes' }, { label: 'No' }], multiple: false }));
  daemon.emit('stream.tool.call', root('turn-1', { id: 'call-1', name: 'rlm_exec', args: '{"code":"tools.lookup_ticket(id=\\"42\\")"}' }));
  daemon.emit('stream.cell.host.started', root('turn-1', { id: 'call-1', invocation_id: '1:1', name: 'tools.lookup_ticket', args: 'id=42' }));
  daemon.emit('stream.cell.host', root('turn-1', { id: 'call-1', invocation_id: '1:1', name: 'tools.lookup_ticket', args: 'id=42', text: '12ms', host_status: 'succeeded' }));
  daemon.emit('stream.hook.decision', root('turn-1', { id: 'call-1', name: 'before_tool', args: 'shell.run', text: 'deny', result: 'destructive' }));
  daemon.emit('stream.usage', root('turn-1', { usage: { used: 120, size: 8000, usage: { prompt_tokens: 100, completion_tokens: 20 } } }));
  daemon.emit('agent.admitted', { agent_id: 'child-1', status: 'queued' });
  daemon.emit('stream.text', { agent_id: 'child-1', turn_id: 'child-turn', text: 'child text' });
  daemon.emit('permission.pending', root('turn-1', { permission_id: 'p-1', operation: 'bash', command: 'npm test', rule: 'Bash(npm *)' }));
  daemon.emit('future.kind', root('turn-1', { anything: true }));
  daemon.emit('stream.tool.completed', root('turn-1', { id: 'call-1', name: 'rlm_exec', result: 'done' }));
  daemon.emit('turn.succeeded', root('turn-1', { status: 'succeeded' }));
  daemon.settle('succeeded', { result: { text: 'Hello. Ticket 42 is open.' } });
  const events: TurnEvent[] = [];
  for await (const event of turn) events.push(event);
  assert.deepEqual(events.map(event => event.type), ['raw', 'text', 'text', 'question', 'cell', 'host', 'host', 'hook', 'usage', 'child', 'permission', 'raw', 'cell', 'end']);
  assert.ok(events.every(event => event.turnId === 'turn-1' || event.type === 'child'));
  assert.deepEqual(events[1], { type: 'text', delta: 'Hel', seq: '7', agentId: 'root', turnId: 'turn-1' });
  const { answer, dismiss, ...question } = events[3] as QuestionEvent;
  assert.deepEqual(question, { type: 'question', id: 'q-1', question: 'Escalate?', options: [{ label: 'Yes' }, { label: 'No' }], multiple: false, seq: '10', agentId: 'root', turnId: 'turn-1' });
  assert.ok(typeof answer === 'function' && typeof dismiss === 'function');
  assert.deepEqual(events[4], { type: 'cell', id: 'call-1', status: 'called', code: 'tools.lookup_ticket(id="42")', seq: '11', agentId: 'root', turnId: 'turn-1' });
  assert.deepEqual(events[6], { type: 'host', id: 'call-1', invocationId: '1:1', operation: 'tools.lookup_ticket', summary: 'id=42', status: 'completed', duration: '12ms', seq: '13', agentId: 'root', turnId: 'turn-1' });
  assert.deepEqual(events[7], { type: 'hook', hook: 'before_tool', operation: 'shell.run', decision: 'deny', reason: 'destructive', seq: '14', agentId: 'root', turnId: 'turn-1' });
  assert.deepEqual(events[9], { type: 'child', childId: 'child-1', kind: 'agent.admitted', status: 'queued', seq: '16', agentId: 'child-1', turnId: 'turn-1' });
  const { allow, deny, ...permission } = events[10] as PermissionEvent;
  assert.deepEqual(permission, { type: 'permission', id: 'p-1', operation: 'bash', command: 'npm test', rule: 'Bash(npm *)', path: '', seq: '18', agentId: 'root', turnId: 'turn-1' });
  assert.ok(typeof allow === 'function' && typeof deny === 'function');
  assert.equal(events[11]?.type === 'raw' ? events[11].event.kind : '', 'future.kind');
  assert.deepEqual(events.at(-1), { type: 'end', status: 'succeeded', seq: '21', agentId: 'root', turnId: 'turn-1' });
  const result = await turn.result();
  assert.deepEqual(result, { status: 'succeeded', turnId: 'turn-1', text: 'Hello. Ticket 42 is open.', output: undefined, usage: { used: 120, size: 8000, usage: { prompt_tokens: 100, completion_tokens: 20 } } });
  assert.equal(await turn.text(), 'Hello. Ticket 42 is open.');
  assert.equal(daemon.methods().filter(method => method === 'events.unsubscribe').length, 1, 'the subscription is released once, when the turn ends');
});

test('includeChildren yields the events of agents spawned during the turn', async t => {
  const daemon = turnDaemon();
  const client = new WhipClient({ endpoint: daemon.fixture.factory, clientId: 'turns', reconnect: false, commandPollMs: 5 });
  t.after(() => client.close());
  await client.connect();
  const turn = client.session('root').run('go', { includeChildren: true });
  await until(() => daemon.methods().includes('command.submit'), 'the submit');
  daemon.emit('turn.started', root('turn-1', { inbox_seq: '7' }));
  daemon.emit('agent.admitted', { agent_id: 'child-1', status: 'queued' });
  daemon.emit('stream.text', { agent_id: 'child-1', turn_id: 'child-turn', text: 'child text' });
  daemon.emit('stream.text', { agent_id: 'stranger', turn_id: 'other', text: 'not ours' });
  daemon.emit('agent.turn.succeeded', { agent_id: 'child-1', turn_id: 'child-turn', status: 'succeeded' });
  daemon.emit('turn.succeeded', root('turn-1', { status: 'succeeded' }));
  daemon.settle('succeeded', { result: { text: 'done' } });
  const events: TurnEvent[] = [];
  for await (const event of turn) events.push(event);
  assert.deepEqual(events.map(event => [event.type, event.agentId]), [['raw', 'root'], ['child', 'child-1'], ['text', 'child-1'], ['child', 'child-1'], ['end', 'root']]);
  assert.deepEqual(events[2], { type: 'text', delta: 'child text', seq: '6', agentId: 'child-1', turnId: 'child-turn' });
});

test('cancel sends the command-targeted cancel and the result reports the outcome', async t => {
  const daemon = turnDaemon();
  const client = new WhipClient({ endpoint: daemon.fixture.factory, clientId: 'turns', reconnect: false, commandPollMs: 5 });
  t.after(() => client.close());
  await client.connect();
  const turn = client.session('root').run('go');
  await until(() => daemon.methods().includes('command.submit'), 'the submit');
  daemon.emit('turn.started', root('turn-1', { inbox_seq: '7' }));
  daemon.emit('stream.text', root('turn-1', { text: 'partial' }));
  await turn.cancel();
  const cancel = daemon.fixture.current.requests.filter(request => request.method === 'command.submit').at(-1)!;
  assert.equal(cancel.params.operation, 'cancel');
  assert.deepEqual(cancel.params.payload, { target_command_id: [...daemon.commands.keys()][0] });
  daemon.emit('turn.cancelled', root('turn-1', { status: 'cancelled' }));
  daemon.settle('cancelled', { failure: { code: -32000, message: 'cancelled by client', data: { kind: 'cancelled' } } });
  const result = await turn.result();
  assert.deepEqual(result, { status: 'cancelled', turnId: 'turn-1', text: 'partial', failure: { code: -32000, message: 'cancelled by client', data: { kind: 'cancelled' } } });
  await assert.rejects(turn.text(), { kind: 'cancelled', message: 'cancelled by client' });
  const events: TurnEvent[] = [];
  for await (const event of turn) events.push(event);
  assert.deepEqual(events.map(event => event.type), ['raw', 'text', 'end']);
});

test('an aborted signal cancels the turn and ends iteration; a command that fails before starting settles without a turn', async t => {
  const daemon = turnDaemon();
  const client = new WhipClient({ endpoint: daemon.fixture.factory, clientId: 'turns', reconnect: false, commandPollMs: 5 });
  t.after(() => client.close());
  await client.connect();
  const controller = new AbortController();
  const turn = client.session('root').run('go', { signal: controller.signal });
  await until(() => daemon.methods().includes('command.submit'), 'the submit');
  daemon.emit('turn.started', root('turn-1', { inbox_seq: '7' }));
  const iteration = (async () => { const seen: TurnEvent[] = []; for await (const event of turn) seen.push(event); return seen; })();
  controller.abort();
  await assert.rejects(iteration, { name: 'AbortError' });
  await until(() => daemon.methods().includes('events.unsubscribe') && daemon.fixture.current.requests.some(request => request.method === 'command.submit' && request.params.operation === 'cancel'), 'cancel and unsubscribe');

  const failing = turnDaemon();
  const other = new WhipClient({ endpoint: failing.fixture.factory, clientId: 'turns', reconnect: false, commandPollMs: 5 });
  t.after(() => other.close());
  await other.connect();
  const rejected = other.session('root').run('go');
  await until(() => failing.methods().includes('command.submit'), 'the submit');
  failing.settle('failed', { failure: { code: -32003, message: 'permission denied', data: { kind: 'permission_denied' } } });
  const result = await rejected.result();
  assert.deepEqual(result, { status: 'failed', failure: { code: -32003, message: 'permission denied', data: { kind: 'permission_denied' } } });
  await assert.rejects(rejected.turnId, { message: /ended before it started/ });
});

test('a consumer that falls behind fails the iterable but not the result', async t => {
  const daemon = turnDaemon();
  const client = new WhipClient({ endpoint: daemon.fixture.factory, clientId: 'turns', reconnect: false, commandPollMs: 5 });
  t.after(() => client.close());
  await client.connect();
  const turn = client.session('root').run('go', { maxMessages: 2 });
  await until(() => daemon.methods().includes('command.submit'), 'the submit');
  daemon.emit('turn.started', root('turn-1', { inbox_seq: '7' }));
  for (let index = 0; index < 5; index++) daemon.emit('stream.text', root('turn-1', { text: `${index}` }));
  daemon.emit('turn.succeeded', root('turn-1', { status: 'succeeded' }));
  daemon.settle('succeeded', { result: { text: '01234' } });
  const result = await turn.result();
  assert.equal(result.status, 'succeeded');
  assert.equal(result.status === 'succeeded' && result.text, '01234');
  await assert.rejects((async () => { for await (const event of turn) void event; })(), { kind: 'resynchronization_required' });
});

test('question and permission events reply through the session exactly once, and prompts() recovers open ones from the snapshot', async t => {
  const pendingQuestion = { question_id: 'q-open', question: 'Still there?', options: [{ label: 'Yes' }], multiple: false, agent_id: 'root', turn_id: 'turn-1' };
  const pendingPermission = { id: 'p-open', agent_id: 'root', operation_id: 'op', operation: 'bash', canonical_path: '', request_digest: 'd', capability_id: 'c', capability_generation: '1', status: 'pending', command: 'rm -rf build', rule: 'Bash(rm *)' };
  const settled = { ...pendingPermission, id: 'p-done', status: 'approved' };
  const daemon = turnDaemon({ questions: [pendingQuestion], permissions: [pendingPermission, settled] });
  const client = new WhipClient({ endpoint: daemon.fixture.factory, clientId: 'turns', reconnect: false, commandPollMs: 5 });
  t.after(() => client.close());
  await client.connect();
  const session = client.session('root');
  const turn = session.run('go');
  await until(() => daemon.methods().includes('command.submit'), 'the submit');
  daemon.emit('turn.started', root('turn-1', { inbox_seq: '7' }));
  daemon.emit('question.pending', root('turn-1', { question_id: 'q-1', question: 'Escalate?', options: [{ label: 'Yes' }, { label: 'No' }], multiple: false }));
  daemon.emit('question.pending', root('turn-1', { question_id: 'q-2', question: 'Which?', options: [{ label: 'a' }], multiple: false, questions: [{ question: 'Which?', options: [{ label: 'a' }], multiple: false }, { question: 'Why?', options: [{ label: 'b' }], multiple: true }] }));
  daemon.emit('permission.pending', root('turn-1', { permission_id: 'p-1', operation: 'bash', command: 'npm test', rule: 'Bash(npm *)' }));
  daemon.emit('permission.pending', root('turn-1', { permission_id: 'p-2', operation: 'write', command: 'notes.md', canonical_path: '/srv/notes.md' }));
  daemon.emit('turn.succeeded', root('turn-1', { status: 'succeeded' }));
  daemon.settle('succeeded', { result: { text: 'done' } });
  const events: TurnEvent[] = [];
  for await (const event of turn) events.push(event);
  const [single, batch] = events.filter(event => event.type === 'question') as [QuestionEvent, QuestionEvent];
  const [first, second] = events.filter(event => event.type === 'permission') as [PermissionEvent, PermissionEvent];
  const answered = single.answer(['Yes']);
  assert.equal(single.answer(['No']), answered, 'a second answer returns the first promise');
  await answered;
  await batch.answer([{ answer: ['a'] }, null]);
  await first.allow({ remember: 'tree' });
  await first.deny('late');
  await second.deny('not now');
  const answers = daemon.fixture.current.requests.filter(request => request.method === 'command.submit' && request.params.operation === 'question.answer').map(request => request.params.payload);
  assert.deepEqual(answers, [
    { id: 'q-1', answer: ['Yes'], dismissed: false },
    { id: 'q-2', answer: [], dismissed: false, answers: [{ answer: ['a'], dismissed: false }, { answer: [], dismissed: true }] },
  ]);
  const decisions = daemon.fixture.current.requests.filter(request => request.method === 'permission.decide').map(request => (request.params.decision as Record<string, unknown>));
  assert.equal(decisions.length, 2, 'one decision per prompt');
  assert.deepEqual(decisions.map(({ command_id: _, ...decision }) => decision), [
    { root_id: 'root', permission_id: 'p-1', allow: true, remember: 'tree' },
    { root_id: 'root', permission_id: 'p-2', allow: false, reason: 'not now' },
  ]);
  assert.ok(decisions.every(decision => typeof decision.command_id === 'string' && decision.command_id));

  const open = await session.prompts();
  assert.deepEqual(open.map(prompt => [prompt.type, prompt.id]), [['question', 'q-open'], ['permission', 'p-open']], 'settled permissions are not offered');
  await (open[0] as QuestionEvent).dismiss();
  await (open[1] as PermissionEvent).deny();
  assert.deepEqual(daemon.fixture.current.requests.filter(request => request.method === 'command.submit' && request.params.operation === 'question.answer').at(-1)?.params.payload, { id: 'q-open', answer: [], dismissed: true });
  const last = daemon.fixture.current.requests.filter(request => request.method === 'permission.decide').at(-1)?.params.decision as Record<string, unknown>;
  assert.deepEqual({ ...last, command_id: undefined }, { root_id: 'root', permission_id: 'p-open', allow: false, command_id: undefined });
});
