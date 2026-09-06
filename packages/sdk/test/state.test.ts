import assert from 'node:assert/strict';
import test from 'node:test';
import type { RootSnapshot, StreamEvent } from '@whip/protocol';
import type { SdkEvent, WhipClient } from '../src/client.js';
import type { Session } from '../src/session.js';
import { createSessionListView, createSessionView } from '../src/state.js';

const pause = (ms = 5) => new Promise(resolve => setTimeout(resolve, ms));
async function until(predicate: () => boolean): Promise<void> {
  const deadline = Date.now() + 1500;
  while (!predicate()) {
    if (Date.now() > deadline) assert.fail('Condition did not become true');
    await pause();
  }
}

function snapshot(cursor = '10', revision = '1'): RootSnapshot {
  return {
    root_id: 'root', cursor, history_revision: revision, active_turns: {},
    meta: { id: 'root', kind: 'agent', title: 'Test', model: '', provider: '', cwd: '/',
      goal: '', forked_from: '', fork_seq: 0, tags: [], pinned: false, effort: '',
      usage_in: 0, usage_cached: 0, usage_out: 0, updated_at: '' },
    messages: [{ role: 'user', content: 'hello' }], message_seqs: [1],
    presentation: [], agent_presentations: {}, agents: [], inbox: [], blackboard: [],
    budgets: [], capabilities: [], schedules: [], permissions: [], questions: [],
  };
}

class Stream implements AsyncIterable<SdkEvent> {
  id = crypto.randomUUID();
  rootId = 'root';
  cursor = '10';
  queue: SdkEvent[] = [];
  pending?: { resolve: (value: IteratorResult<SdkEvent>) => void; reject: (error: Error) => void };
  closed = false;
  push(seq: string, kind: string, payload: unknown, unknown = false) {
    const event = { subscription_id: this.id, root_id: 'root', seq, kind, payload, unknown } as SdkEvent;
    if (this.pending) { this.pending.resolve({ done: false, value: event }); this.pending = undefined; }
    else this.queue.push(event);
  }
  next(): Promise<IteratorResult<SdkEvent>> {
    const item = this.queue.shift();
    if (item) return Promise.resolve({ done: false, value: item });
    if (this.closed) return Promise.resolve({ done: true, value: undefined });
    return new Promise((resolve, reject) => { this.pending = { resolve, reject }; });
  }
  fail() { this.pending?.reject(new Error('Connection lost')); this.pending = undefined; this.closed = true; }
  async dispose() {
    this.closed = true;
    this.pending?.resolve({ done: true, value: undefined });
    this.pending = undefined;
  }
  [Symbol.asyncIterator]() { return this; }
}

class Host {
  root = snapshot();
  streams: Stream[] = [];
  listeners = new Set<() => void>();
  commands = new Set<() => void>();
  calls: { method: string; params: Record<string, unknown> }[] = [];
  connection = { state: 'connected', info: { runtime_id: 'runtime', connection_id: 'connection-1' } };
  history?: (params: Record<string, unknown>) => Promise<unknown>;
  beforeAck?: (stream: Stream) => void;
  catalogRevision = '1';
  catalogItems = [{ id: 'root', kind: 'agent', title: 'Test', model: '', provider: '', cwd: '/', pinned: false, updated_at: '', truncated: false }];
  getSnapshot = () => this.connection;
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  onCommand = (fn: () => void) => { this.commands.add(fn); return () => { this.commands.delete(fn); }; };
  events = { subscribe: async (_rootId: string, cursor: string) => {
    const stream = new Stream();
    stream.cursor = cursor;
    this.streams.push(stream);
    this.beforeAck?.(stream);
    return stream;
  } };
  async call(method: string, params: Record<string, unknown>): Promise<unknown> {
    this.calls.push({ method, params });
    if (method === 'root.snapshot') return structuredClone(this.root);
    if (method === 'history.page') return this.history?.(params) ?? {
      history_revision: this.root.history_revision, through_seq: 1, next_seq: 1, has_more: false,
      messages: [{ seq: 1, message: { role: 'user', content: 'child' } }],
    };
    if (method === 'sessions.revision') return { revision: this.catalogRevision };
    if (method === 'sessions.list') return { revision: this.catalogRevision, items: this.catalogItems, has_more: false };
    if (method === 'root.collection') return { root_id: 'root', collection: params.collection, revision: '1', event_cursor: '10', items: [], has_more: false };
    throw new Error(`Unexpected method ${method}`);
  }
  session(): Session { return { rootId: 'root', client: this as unknown as WhipClient } as Session; }
  notify(state: string, runtime = 'runtime') {
    this.connection = { state, info: { runtime_id: runtime, connection_id: crypto.randomUUID() } };
    for (const listener of this.listeners) listener();
  }
}

test('snapshot plus pre-ack events preserves cursor precision and immutable snapshots', async () => {
  const host = new Host();
  host.root.cursor = '9007199254740993';
  host.root.presentation = [{ seq: '9007199254740993', kind: 'stream.text', payload: { text: 'before ' } }];
  host.beforeAck = stream => stream.push('9007199254740994', 'stream.text', { text: 'after' });
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  await view.start();
  const first = view.getSnapshot();
  await until(() => view.getSnapshot().root?.cursor === '9007199254740994');
  const next = view.getSnapshot();
  assert.equal((next.root?.presentation?.[0]?.payload as { text: string }).text, 'before after');
  assert.equal(first.root?.cursor, '9007199254740993');
  assert.equal(view.getSnapshot(), next);
  assert.throws(() => { (next.root!.meta as { title: string }).title = 'mutated'; });
  assert.equal(host.streams.length, 1);
  await view.dispose();
});

for (const agentId of ['root', 'child']) {
  test(`${agentId}: streaming rows append deltas, replace cumulative values and survive snapshot recovery`, async t => {
    const host = new Host();
    const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
    t.after(() => view.dispose());
    await view.start();
    const rows = () => agentId === 'root' ? view.getSnapshot().root?.presentation : view.getSnapshot().root?.agent_presentations?.[agentId];
    const events = [
      ['stream.text', { text: 'Hello ' }],
      ['stream.text', { text: 'world' }],
      ['stream.reasoning', { text: 'Think ' }],
      ['stream.reasoning', { text: 'more' }],
      ['stream.tool.call', { id: 'a', name: 'rlm_exec', args: '{"code":"print(' }],
      ['stream.tool.call', { id: 'b', name: 'rlm_exec', args: '{"code":"other"}' }],
      ['stream.tool.call', { id: 'a', name: 'rlm_exec', args: '{"code":"print(1)"}' }],
      ['stream.tool.output', { id: 'a', text: 'first' }],
      ['stream.tool.output', { id: 'b', text: 'other output' }],
      ['stream.tool.output', { id: 'a', text: 'first\nsecond' }],
      ['stream.terminal.output', { id: 'terminal', text: 'one ' }],
      ['stream.terminal.output', { id: 'terminal', text: 'two' }],
    ] satisfies [string, StreamEvent][];
    const raw = events.map(([kind, payload], index) => ({ seq: String(11 + index), kind, payload: { ...payload, agent_id: agentId } }));
    host.streams[0]!.push(raw[0]!.seq, raw[0]!.kind, raw[0]!.payload);
    await until(() => view.getSnapshot().root?.cursor === '11');
    const first = rows()![0]!;
    for (const event of raw.slice(1)) host.streams[0]!.push(event.seq, event.kind, event.payload);
    await until(() => view.getSnapshot().root?.cursor === '22');
    const live = rows()!;
    assert.deepEqual(live.map(row => row.seq), ['11', '13', '15', '16', '18', '19', '21']);
    assert.equal((first.payload as StreamEvent).text, 'Hello ', 'previous snapshots remain immutable');
    assert.deepEqual(live.map(row => { const payload = row.payload as StreamEvent; return payload.args ?? payload.text; }), [
      'Hello world', 'Think more', '{"code":"print(1)"}', '{"code":"other"}', 'first\nsecond', 'other output', 'one two',
    ]);
    host.root.cursor = '22';
    if (agentId === 'root') host.root.presentation = raw;
    else host.root.agent_presentations = { [agentId]: raw };
    await view.refresh();
    assert.deepEqual(rows(), live, 'raw snapshot events reconstruct the same rows as live delivery');
    host.streams.at(-1)!.push('23', 'stream.tool.call', { agent_id: agentId, id: 'a', name: 'rlm_exec', args: '{"code":"print(12)"}' });
    await until(() => view.getSnapshot().root?.cursor === '23');
    assert.equal(rows()!.length, live.length);
    assert.equal(rows()![2]!.seq, '15', 'the rendered row keeps its identity across updates and recovery');
    assert.equal((rows()![2]!.payload as StreamEvent).args, '{"code":"print(12)"}');
  });
}

test('duplicates are ignored, gaps stop the stream, and replaced subscriptions cannot change a new view', async () => {
  const host = new Host();
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  await view.start();
  const old = host.streams[0]!;
  old.push('11', 'stream.text', { text: 'once' });
  old.push('11', 'stream.text', { text: 'duplicated' });
  await until(() => view.getSnapshot().root?.cursor === '11');
  assert.equal((view.getSnapshot().root?.presentation?.[0]?.payload as { text: string }).text, 'once');
  old.push('13', 'stream.text', { text: 'gap' });
  await until(() => view.getSnapshot().status === 'stale');
  assert.equal(view.getSnapshot().root?.cursor, '11');
  host.root = snapshot('20');
  await view.refresh();
  old.push('21', 'stream.text', { text: 'obsolete' });
  host.streams.at(-1)!.push('21', 'stream.text', { text: 'new' });
  await until(() => view.getSnapshot().root?.cursor === '21');
  assert.equal((view.getSnapshot().root?.presentation?.[0]?.payload as { text: string }).text, 'new');
  await view.dispose();
});

test('reconnect retains stale data and refuses to attach the view to a different runtime', async () => {
  const host = new Host();
  const view = createSessionView(host.session());
  await view.start();
  host.notify('reconnecting');
  host.streams.at(-1)!.fail();
  assert.equal(view.getSnapshot().status, 'stale');
  assert.equal(view.getSnapshot().root?.meta.title, 'Test');
  host.root = snapshot('20');
  host.notify('connected');
  await until(() => view.getSnapshot().root?.cursor === '20');
  const count = host.calls.length;
  host.notify('connected', 'replacement-runtime');
  assert.equal(view.getSnapshot().status, 'error');
  await pause();
  assert.equal(host.calls.length, count);
  await view.dispose();
});

test('history pages cannot mix pre-rewind and post-rewind revisions', async () => {
  const host = new Host();
  const view = createSessionView(host.session());
  await view.start();
  let resolve!: (value: unknown) => void;
  host.history = () => new Promise(done => { resolve = done; });
  const pending = view.openAgent('child');
  host.root = snapshot('20', '2');
  host.history = async () => ({ history_revision: '2', through_seq: 1, next_seq: 1, has_more: false,
    messages: [{ seq: 1, message: { role: 'assistant', content: 'current' } }] });
  // Do not block the refresh on the deliberate, stale request.
  const refresh = view.refresh();
  await until(() => view.getSnapshot().root?.history_revision === '2');
  resolve({ history_revision: '1', through_seq: 2, next_seq: 2, has_more: false,
    messages: [{ seq: 2, message: { role: 'assistant', content: 'rewound' } }] });
  await pending;
  await refresh;
  assert.equal(view.getSnapshot().history.child?.revision, '2');
  assert.equal(view.getSnapshot().history.child?.messages[0]?.message?.content, 'current');
  assert.equal(view.getSnapshot().history.root?.revision, '2');
  await view.dispose();
});

test('backward pagination retains requested pages and revision-bound cursors', async () => {
  const host = new Host();
  host.root.messages = [4, 5].map(seq => ({ role: 'user', content: String(seq) }));
  host.root.message_seqs = [4, 5];
  host.root.first_message_seq = 4;
  host.history = async () => ({ history_revision: '1', through_seq: 5, next_seq: 2, has_more: true,
    messages: [2, 3].map(seq => ({ seq, message: { role: 'user', content: String(seq) } })) });
  const view = createSessionView(host.session(), { maxMessages: 3 });
  await view.start();
  await view.loadOlder();
  const page = view.getSnapshot().history.root!;
  assert.deepEqual(page.messages.map(message => message.seq), [2, 3, 4]);
  assert.equal(page.nextSeq, 2);
  assert.equal(page.truncated, true);
  assert.deepEqual(host.calls.at(-1)?.params, {
    root_id: 'root', agent_id: 'root', recent: true, through_seq: 5,
    before_seq: 4, revision: '1', limit: 128, max_bytes: 256 * 1024,
  });
  await view.dispose();
});

test('large live output is bounded and explicitly unavailable', async () => {
  const host = new Host();
  const view = createSessionView(host.session(), { maxBytes: 2048, notificationIntervalMs: 1 });
  await view.start();
  host.streams[0]!.push('11', 'stream.text', { text: 'x'.repeat(10000) });
  await until(() => view.getSnapshot().root?.cursor === '11');
  assert.equal(view.getSnapshot().truncated, true);
  assert.equal(view.getSnapshot().unavailable, true);
  assert.ok(view.getSnapshot().retainedBytes <= 2048);
  await view.dispose();
});

test('a burst processes every fragment while publishing one immutable view update', async () => {
  const host = new Host();
  const view = createSessionView(host.session(), { notificationIntervalMs: 16 });
  await view.start();
  let notices = 0;
  const stop = view.subscribe(() => { notices++; });
  for (let seq = 11; seq <= 510; seq++) host.streams[0]!.push(String(seq), 'stream.text', { text: 'x' });
  await until(() => view.getSnapshot().root?.cursor === '510');
  assert.equal((view.getSnapshot().root?.presentation?.[0]?.payload as { text: string }).text.length, 500);
  assert.equal(notices, 1);
  stop();
  await view.dispose();
});

test('throttled notification timers cannot leave growing event state unbounded', async () => {
  const host = new Host();
  const view = createSessionView(host.session(), { maxBytes: 2048, notificationIntervalMs: 10_000 });
  await view.start();
  let notices = 0;
  const stop = view.subscribe(() => {
    notices++;
    assert.ok(view.getSnapshot().retainedBytes <= 2048);
  });
  for (let seq = 11; seq <= 60; seq++) host.streams[0]!.push(String(seq), 'stream.text', { text: 'x'.repeat(4096) });
  // This finishes before the deliberately delayed notification timer can fire.
  await until(() => view.getSnapshot().root?.cursor === '60');
  assert.ok(notices > 0);
  assert.equal(view.getSnapshot().truncated, true);
  assert.equal(view.getSnapshot().unavailable, true);
  stop();
  await view.dispose();
});

test('cache pressure evicts child history before the current root history', async () => {
  const host = new Host();
  host.root.messages = [{ role: 'user', content: 'root'.repeat(500) }];
  host.history = async () => ({ history_revision: '1', through_seq: 1, next_seq: 1, has_more: false,
    messages: [{ seq: 1, message: { role: 'assistant', content: 'child'.repeat(600) } }] });
  const view = createSessionView(host.session(), { maxBytes: 6000 });
  await view.start();
  await view.openAgent('child');
  assert.equal(view.getSnapshot().history.child, undefined);
  assert.equal(view.getSnapshot().history.root?.messages[0]?.message?.content, 'root'.repeat(500));
  assert.ok(view.getSnapshot().retainedBytes <= 6000);
  assert.equal(view.getSnapshot().truncated, true);
  await view.dispose();
});

test('questions resolve across clients and unknown event payloads stay explicit', async () => {
  const host = new Host();
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  await view.start();
  host.streams[0]!.push('11', 'question.pending', { question_id: 'q', question: 'Continue?' });
  await until(() => view.getSnapshot().root?.questions?.length === 1);
  host.streams[0]!.push('12', 'question.answered', { question_id: 'q', answer: ['yes'] });
  host.streams[0]!.push('13', 'future.event', { field: 'unknown' }, true);
  await until(() => view.getSnapshot().root?.cursor === '13');
  assert.equal(view.getSnapshot().root?.questions?.length, 0);
  assert.equal(view.getSnapshot().unavailable, true);
  await view.dispose();
});

test('catalog polling is observed, revision-based, and never opens roots', async () => {
  const host = new Host();
  const list = createSessionListView(host as unknown as WhipClient, { pollIntervalMs: 10 });
  await list.start();
  const initial = host.calls.length;
  await pause(25);
  assert.equal(host.calls.length, initial);
  const stop = list.subscribe(() => {});
  await until(() => host.calls.some(call => call.method === 'sessions.revision'));
  host.catalogRevision = '2';
  host.catalogItems = [{ ...host.catalogItems[0]!, title: 'Renamed' }];
  await until(() => list.getSnapshot().page?.revision === '2');
  assert.equal(list.getSnapshot().page?.items?.[0]?.title, 'Renamed');
  stop();
  await pause(15);
  const stopped = host.calls.length;
  await pause(25);
  assert.equal(host.calls.length, stopped);
  assert.equal(host.streams.length, 0);
  assert.ok(host.calls.every(call => call.method.startsWith('sessions.')));
  await list.dispose();
});
