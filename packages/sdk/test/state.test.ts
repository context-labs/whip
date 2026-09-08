import assert from 'node:assert/strict';
import test from 'node:test';
import type { RootSnapshot, StreamEvent } from '@whip/protocol';
import type { SdkEvent, WhipClient } from '../src/client.js';
import type { Session } from '../src/session.js';
import { createSessionListView, createSessionView, executionRows } from '../src/state.js';

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

test('permission mode updates survive snapshot refreshes without changing session metadata', async () => {
  const host = new Host();
  host.root.permission_mode = 'automatic';
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  await view.start();
  try {
    assert.equal(view.getSnapshot().root?.permission_mode, 'automatic');
    for (const mode of ['prompt', 'automatic']) {
      const before = view.getSnapshot().root!;
      host.root.permission_mode = mode;
      host.root.cursor = String(BigInt(host.root.cursor) + 1n);
      host.streams.at(-1)!.push(host.root.cursor, 'session.permission_mode.updated', { permission_mode: mode });
      await until(() => view.getSnapshot().root?.permission_mode === mode);
      assert.notEqual(before.permission_mode, mode, 'Previously published snapshots stay immutable');
      assert.equal('permission_mode' in view.getSnapshot().root!.meta, false);
      await view.refresh();
      assert.equal(view.getSnapshot().root?.permission_mode, mode);
    }
  } finally { await view.dispose(); }
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

for (const agentId of ['root', 'child']) {
  test(`${agentId}: lifecycle refresh preserves observed rows and merges only newer raw snapshot events`, async t => {
    const host = new Host();
    host.root.cursor = '9007199254740993';
    host.root.active_turns = { [agentId]: 'turn' };
    const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
    t.after(() => view.dispose());
    await view.start();
    const rows = () => (agentId === 'root' ? view.getSnapshot().root?.presentation : view.getSnapshot().root?.agent_presentations?.[agentId]) ?? [];
    let cursor = BigInt(host.root.cursor);
    const event = (kind: string, payload: StreamEvent) => ({ seq: String(++cursor), kind, payload: { ...payload, agent_id: agentId } });
    const observed = [
      event('stream.text', { text: 'Earlier answer' }),
      event('stream.tool.call', { id: 'done', args: 'complete arguments' }),
      event('stream.tool.completed', { id: 'done', result: 'completed result' }),
      event('stream.tool.call', { id: 'working', args: 'old arguments' }),
      event('stream.tool.output', { id: 'working', text: 'old output' }),
      event('stream.text', { text: 'A' }),
    ];
    for (const item of observed) host.streams[0]!.push(item.seq, item.kind, item.payload);
    await until(() => view.getSnapshot().root?.cursor === String(cursor));
    const before = rows();
    const lifecycle = event('model.call.settled', {});
    host.streams[0]!.push(lifecycle.seq, lifecycle.kind, lifecycle.payload);
    const newer = [
      event('stream.text', { text: 'B' }),
      event('stream.tool.call', { id: 'working', args: 'new arguments' }),
      event('stream.tool.output', { id: 'working', text: 'new output' }),
    ];
    host.root.cursor = String(cursor);
    host.root.meta.title = 'Refreshed metadata';
    host.root.questions = [{ question_id: 'pending' }];
    host.root.omitted = { presentation_prefix: true };
    const suffix = [...observed.slice(-1), ...newer];
    if (agentId === 'root') host.root.presentation = suffix;
    else host.root.agent_presentations = { [agentId]: suffix };
    const afterSnapshot = event('stream.reasoning', { text: 'After snapshot' });
    host.beforeAck = stream => {
      assert.equal(view.getSnapshot().status, 'live', 'ordinary metadata refresh must not disable focused controls');
      host.streams[0]!.push(afterSnapshot.seq, afterSnapshot.kind, { text: 'obsolete subscription' });
      stream.push(afterSnapshot.seq, afterSnapshot.kind, afterSnapshot.payload);
    };
    await until(() => view.getSnapshot().root?.cursor === afterSnapshot.seq);
    assert.equal(host.streams[0]!.closed, true);
    assert.deepEqual(rows().map(row => row.seq), [...before.map(row => row.seq), afterSnapshot.seq]);
    assert.deepEqual(rows().map(row => {
      const payload = row.payload as StreamEvent;
      return payload.args ?? payload.result ?? payload.text;
    }), ['Earlier answer', 'complete arguments', 'completed result', 'new arguments', 'new output', 'AB', 'After snapshot']);
    assert.equal((before[5]!.payload as StreamEvent).text, 'A', 'published rows stay immutable');
    assert.equal(view.getSnapshot().root?.meta.title, 'Refreshed metadata');
    assert.equal(view.getSnapshot().root?.questions?.[0]?.question_id, 'pending');
    assert.equal(view.getSnapshot().truncated, true);
    assert.equal(view.getSnapshot().root?.omitted?.presentation_prefix, true);
    // The repeated snapshot overlaps everything; its grouped text starts before
    // the observed cursor, and its older cumulative values must not regress rows.
    host.beforeAck = undefined;
    host.root.cursor = afterSnapshot.seq;
    const once = rows();
    host.root.omitted = {};
    await view.refresh();
    assert.deepEqual(rows(), once);
    assert.equal(view.getSnapshot().root?.omitted?.presentation_prefix, true, 'retained partial presentation keeps its omission flag');
    host.root.presentation = [];
    host.root.agent_presentations = {};
    await view.refresh();
    assert.deepEqual(rows(), once, 'even an entirely omitted agent presentation preserves observed rows');
  });

  for (const kind of ['stream.text', 'stream.reasoning', 'stream.terminal.output']) {
    test(`${agentId}: partial refresh separates ${kind} across missing snapshot and live deltas`, async t => {
      const host = new Host();
      host.root.active_turns = { [agentId]: 'turn' };
      const payload = (text: string) => ({ text, agent_id: agentId });
      const initial = [{ seq: '10', kind, payload: payload('A') }];
      if (agentId === 'root') host.root.presentation = initial;
      else host.root.agent_presentations = { [agentId]: initial };
      const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
      t.after(() => view.dispose());
      await view.start();
      // 11 and 14 were omitted. Only 12 and 13 are proven contiguous.
      host.root.cursor = '14';
      host.root.omitted = { presentation: true };
      const suffix = [
        { seq: '12', kind, payload: payload('C') }, { seq: '13', kind, payload: payload('D') },
      ];
      if (agentId === 'root') host.root.presentation = suffix;
      else host.root.agent_presentations = { [agentId]: suffix };
      await view.refresh();
      await view.refresh(); // A repeat must not lose the unresolved suffix boundary.
      host.streams.at(-1)!.push('15', kind, payload('F'));
      host.streams.at(-1)!.push('16', kind, payload('G'));
      await until(() => view.getSnapshot().root?.cursor === '16');
      const rows = agentId === 'root' ? view.getSnapshot().root?.presentation : view.getSnapshot().root?.agent_presentations?.[agentId];
      assert.deepEqual(rows?.map(row => (row.payload as StreamEvent).text), ['A', 'CD', 'FG']);
      assert.deepEqual(rows?.map(row => row.seq), ['10', '12', '15']);
    });
  }
}

for (const change of ['root ended', 'child ended', 'turn changed', 'revision changed']) {
  test(`snapshot replacement is scoped correctly when ${change}`, async t => {
    const host = new Host();
    host.root.active_turns = { root: 'root-turn', child: 'child-turn' };
    host.root.presentation = [{ seq: '9', kind: 'stream.text', payload: { text: 'root live' } }];
    host.root.agent_presentations = { child: [{ seq: '10', kind: 'stream.text', payload: { text: 'child live', agent_id: 'child' } }] };
    const view = createSessionView(host.session());
    t.after(() => view.dispose());
    await view.start();
    host.root.cursor = '11';
    host.root.presentation = [];
    host.root.agent_presentations = {};
    if (change === 'root ended') {
      delete host.root.active_turns.root;
      host.root.messages = [{ role: 'assistant', content: 'root live' }];
      host.streams[0]!.push('11', 'turn.succeeded', { turn_id: 'root-turn' });
    } else if (change === 'child ended') delete host.root.active_turns.child;
    else if (change === 'turn changed') host.root.active_turns.root = 'replacement-turn';
    else host.root.history_revision = '2';
    await view.refresh();
    assert.equal(view.getSnapshot().root?.presentation?.length, change === 'child ended' ? 1 : 0);
    assert.equal(view.getSnapshot().root?.agent_presentations?.child?.length ?? 0,
      change === 'root ended' || change === 'turn changed' ? 1 : 0);
    if (change === 'root ended') assert.equal(view.getSnapshot().history.root?.messages[0]?.message?.content, 'root live');
  });
}

for (const recovery of ['gap', 'subscription failure', 'reconnect', 'connection replaced']) {
  test(`${recovery} replaces presentation even when the active turn is unchanged`, async t => {
    const host = new Host();
    host.root.active_turns = { root: 'turn' };
    host.root.presentation = [{ seq: '10', kind: 'stream.text', payload: { text: 'before recovery' } }];
    const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
    t.after(() => view.dispose());
    await view.start();
    host.root.cursor = '20';
    host.root.presentation = [{ seq: '20', kind: 'stream.text', payload: { text: 'recovered suffix' } }];
    if (recovery === 'gap') {
      host.streams[0]!.push('12', 'stream.text', { text: 'gap' });
      await until(() => view.getSnapshot().status === 'stale');
      await view.refresh();
    } else if (recovery === 'subscription failure') {
      host.streams[0]!.fail();
      await until(() => view.getSnapshot().status === 'stale');
      await view.refresh();
    } else {
      if (recovery === 'reconnect') host.notify('reconnecting');
      host.notify('connected');
    }
    await until(() => view.getSnapshot().root?.cursor === '20');
    assert.deepEqual(view.getSnapshot().root?.presentation?.map(row => (row.payload as StreamEvent).text), ['recovered suffix']);
  });
}

test('retaining observed presentation during refresh still enforces the view byte budget', async t => {
  const host = new Host();
  host.root.active_turns = { root: 'turn' };
  host.root.presentation = [{ seq: '10', kind: 'stream.text', payload: { text: 'a'.repeat(600) } }];
  const view = createSessionView(host.session(), { maxBytes: 2048 });
  t.after(() => view.dispose());
  await view.start();
  assert.equal(view.getSnapshot().root?.presentation?.length, 1);
  host.root.cursor = '11';
  host.root.presentation = [{ seq: '11', kind: 'stream.text', payload: { text: 'b'.repeat(1600) } }];
  await view.refresh();
  assert.ok(view.getSnapshot().retainedBytes <= 2048);
  assert.equal(view.getSnapshot().truncated, true);
  assert.equal(view.getSnapshot().unavailable, true);
  assert.equal(view.getSnapshot().root?.presentation?.length, 0);
});

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

test('a rejected history request cannot resurrect a closed child view', async () => {
  const host = new Host();
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  await view.start();
  let reject!: (error: Error) => void;
  host.history = () => new Promise((_resolve, fail) => { reject = fail; });
  const pending = view.openAgent('child');
  view.closeAgent('child');
  reject(new Error('late child failure'));
  await pending;
  await pause();
  assert.equal(view.getSnapshot().history.child, undefined);
  assert.equal(view.getSnapshot().error, undefined);
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


test('REPL evidence shares the root subscription and survives commit and reconnect', async t => {
  const host = new Host();
  host.root.active_turns = { root: 'turn-1' };
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  const stream = host.streams[0]!;
  stream.push('11', 'stream.tool.call', { id: 'cell', name: 'rlm_exec', args: '{"code":"print(42)"}' });
  stream.push('12', 'stream.tool.started', { id: 'cell', name: 'rlm_exec' });
  stream.push('13', 'stream.cell.host', { id: 'cell', name: 'fs.read', args: '/file', text: '10ms' });
  stream.push('14', 'stream.tool.completed', { id: 'cell', name: 'rlm_exec', result: '{"value":42,"steps":7}' });
  await until(() => view.getSnapshot().root?.cursor === '14');
  const first = executionRows(view.getSnapshot(), 'root')[0]!;
  assert.equal(first.kind, 'cell');
  assert.equal(host.streams.length, 1, 'projection opens no additional subscription');
  assert.ok(host.calls.every(call => call.method === 'root.snapshot'));
  host.root.cursor = '14';
  host.root.active_turns = {};
  host.root.messages = [...host.root.messages!,
    { role: 'assistant', content: '', tool_calls: [{ id: 'cell', type: 'function', function: { name: 'rlm_exec', arguments: '{"code":"print(42)"}' } }] },
    { role: 'tool', tool_call_id: 'cell', content: '{"value":42,"steps":7}' }];
  host.root.message_seqs = [1, 2, 3];
  await view.refresh();
  const committed = executionRows(view.getSnapshot(), 'root');
  assert.equal(committed.length, 1);
  assert.equal(committed[0]!.id, first.id);
  assert.equal(committed[0]!.kind === 'cell' && committed[0]!.hosts[0]?.name, 'fs.read');
  assert.ok(Object.isFrozen(committed[0]!.kind === 'cell' && committed[0]!.hosts));
  host.notify('reconnecting');
  host.streams.at(-1)!.fail();
  host.root.cursor = '20';
  host.notify('connected');
  await until(() => view.getSnapshot().root?.cursor === '20');
  assert.equal(executionRows(view.getSnapshot(), 'root')[0]!.id, first.id);
  host.root = snapshot('30', '2');
  await view.refresh();
  assert.equal(executionRows(view.getSnapshot(), 'root').length, 0, 'rewind revision clears incompatible observed cells');
});

test('REPL unclosed cells stop spinning after disconnect, disposal clears evidence and runtime replacement cannot leak it', async t => {
  const host = new Host();
  host.root.active_turns = { root: 'turn-1' };
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  host.streams[0]!.push('11', 'stream.tool.started', { id: 'a', name: 'rlm_exec' });
  await until(() => view.getSnapshot().root?.cursor === '11');
  host.notify('reconnecting');
  const stale = executionRows(view.getSnapshot(), 'root')[0]!;
  assert.equal(stale.kind === 'cell' && stale.status, 'unknown');
  host.notify('connected', 'another-runtime');
  assert.equal(view.getSnapshot().executions, undefined);
  await view.dispose();
  assert.equal(view.getSnapshot().executions, undefined);
});

test('REPL supplements count against the SessionView byte limit even while publication timers are throttled', async t => {
  const host = new Host();
  host.root.active_turns = { root: 'turn-1' };
  const view = createSessionView(host.session(), { maxBytes: 4096, notificationIntervalMs: 10_000 });
  t.after(() => view.dispose());
  await view.start();
  host.streams[0]!.push('11', 'stream.tool.started', { id: 'a', name: 'rlm_exec' });
  host.streams[0]!.push('12', 'stream.tool.output', { id: 'a', text: 'x'.repeat(100_000) });
  await until(() => view.getSnapshot().root?.cursor === '12');
  const current = view.getSnapshot();
  assert.ok(current.retainedBytes <= 4096);
  assert.equal(current.truncated, true);
  const bytes = new TextEncoder().encode(JSON.stringify({ root: current.root, history: current.history, collections: current.collections, executions: current.executions })).byteLength;
  assert.equal(bytes, current.retainedBytes);
});


function accounting(revision: string, overrides: Partial<NonNullable<RootSnapshot['accounting']>> = {}): NonNullable<RootSnapshot['accounting']> {
  return { root_id: 'root', agent_id: 'root', scope: 'subtree', revision,
    reported_cost_micros: '9007199254740993', estimated_cost_micros: '17', reported_cost_calls: '1', estimated_cost_calls: '2',
    unknown_cost_calls: '3', reported_calls: '4', estimated_calls: '5', pending_calls: '6', ...overrides };
}

test('accounting replaces tree summaries exactly while stale, duplicate and differently scoped events only advance the cursor', async t => {
  const host = new Host();
  host.root.accounting = accounting('10');
  host.root.active_turns = { child: 'child-turn' };
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  await view.openAgent('child');
  const childHistory = view.getSnapshot().history.child;
  const initial = view.getSnapshot();
  const stream = host.streams[0]!;
  const updated = accounting('11', { estimated_cost_micros: '9007199254740994' });
  stream.push('11', 'stream.accounting', { accounting: updated });
  await until(() => view.getSnapshot().root?.cursor === '11');
  assert.deepEqual(view.getSnapshot().root?.accounting, updated);
  assert.equal(initial.root?.accounting?.estimated_cost_micros, '17', 'published summaries remain immutable');
  stream.push('11', 'stream.accounting', { accounting: accounting('999') });
  stream.push('12', 'stream.accounting', { accounting: updated });
  stream.push('13', 'stream.accounting', { accounting: accounting('10') });
  stream.push('14', 'stream.accounting', { accounting: accounting('14', { scope: 'agent' }) });
  stream.push('15', 'stream.accounting', { accounting: accounting('15', { root_id: 'other' }) });
  stream.push('16', 'stream.accounting', { agent_id: 'child', accounting: accounting('16', { agent_id: 'child' }) });
  stream.push('17', 'stream.accounting', {});
  await until(() => view.getSnapshot().root?.cursor === '17');
  assert.deepEqual(view.getSnapshot().root?.accounting, updated, 'neither duplicate nor child totals are added');
  assert.equal(view.getSnapshot().history.child, childHistory);
  assert.deepEqual(view.getSnapshot().root?.active_turns, { child: 'child-turn' });
  assert.deepEqual(view.getSnapshot().root?.presentation, []);
  assert.deepEqual(view.getSnapshot().root?.agent_presentations, {});
  assert.deepEqual(executionRows(view.getSnapshot(), 'root'), []);
  assert.deepEqual(executionRows(view.getSnapshot(), 'child'), []);
  const free = accounting('18', { reported_cost_micros: '0', reported_cost_calls: '1', estimated_calls: '1' });
  stream.push('18', 'stream.accounting', { accounting: free });
  await until(() => view.getSnapshot().root?.cursor === '18');
  assert.deepEqual(view.getSnapshot().root?.accounting, free, 'reported zero remains known even without token usage');
  await pause(150);
  assert.equal(host.calls.filter(call => call.method === 'root.snapshot').length, 1, 'accounting creates no extra snapshots or polls');
});

test('accounting gap and reconnect recovery use authoritative optional snapshots and exclude summaries from presentation', async t => {
  const host = new Host();
  host.root.accounting = accounting('10');
  host.root.presentation = [{ seq: '10', kind: 'stream.accounting', payload: { accounting: host.root.accounting } }];
  host.root.agent_presentations = { child: [{ seq: '9', kind: 'stream.accounting', payload: { accounting: accounting('9', { agent_id: 'child', scope: 'agent' }) } }] };
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  assert.deepEqual(view.getSnapshot().root?.presentation, []);
  assert.deepEqual(view.getSnapshot().root?.agent_presentations?.child, []);
  const old = host.streams[0]!;
  old.push('12', 'stream.accounting', { accounting: accounting('12') });
  await until(() => view.getSnapshot().status === 'stale');
  assert.equal(view.getSnapshot().root?.accounting?.revision, '10');
  host.root = { ...snapshot('20'), accounting: accounting('20', { reported_cost_micros: '25' }) };
  await view.refresh();
  assert.deepEqual(view.getSnapshot().root?.accounting, host.root.accounting);
  old.push('21', 'stream.accounting', { accounting: accounting('21') });
  await pause();
  assert.equal(view.getSnapshot().root?.accounting?.reported_cost_micros, '25');
  host.notify('reconnecting');
  host.streams.at(-1)!.fail();
  assert.equal(view.getSnapshot().root?.accounting?.reported_cost_micros, '25', 'stale totals remain visible during disconnect');
  host.root = snapshot('30');
  host.notify('connected');
  await until(() => view.getSnapshot().root?.cursor === '30');
  assert.equal(view.getSnapshot().root?.accounting, undefined, 'an absent summary is unavailable, not stale totals or zero');
});

test('model attempt lifecycle events use the existing coalesced snapshot refresh for budgets', async t => {
  const host = new Host();
  host.root.accounting = accounting('10');
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  const stream = host.streams[0]!;
  for (const [index, kind] of ['started', 'settled', 'corrected', 'interrupted'].entries()) {
    stream.push(String(index + 11), `model.call.${kind}`, { id: `call-${index}` });
  }
  host.root = { ...snapshot('14'), accounting: accounting('14'), budgets: [{ agent_id: 'root', state: {
    kind: 'cost', limit: null, remaining: null, used: '42', reserved: '0', uncertain: '12', incomplete: true,
  } }] };
  await until(() => view.getSnapshot().root?.budgets?.[0]?.state.used === '42');
  assert.equal(host.calls.filter(call => call.method === 'root.snapshot').length, 2, 'one refresh handles the burst');
  assert.equal(host.streams.length, 2);
  assert.deepEqual(view.getSnapshot().root?.presentation, []);
});
