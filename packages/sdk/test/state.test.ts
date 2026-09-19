import assert from 'node:assert/strict';
import test from 'node:test';
import type { RootSnapshot, SpanPage, StreamEvent } from '@whip/protocol';
import type { CallOptions, SdkEvent, WhipClient } from '../src/client.js';
import type { Session } from '../src/session.js';
import { createSessionListView, createSessionView, executionRows, inboxItems } from '../src/state.js';

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
    meta: { id: 'root', kind: 'agent', title: 'Test', model: '', provider: '', cwd: '/', execution_engine: 'starlark', definition: 'coding', definition_revision: '',
      goal: '', forked_from: '', fork_seq: 0, tags: [], archived: false, pinned: false, effort: '',
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
  commands = new Set<(outcome: { command_id: string; status: string }) => void>();
  calls: { method: string; params: Record<string, unknown> }[] = [];
  connection = { state: 'connected', info: { runtime_id: 'runtime', connection_id: 'connection-1' } };
  history?: (params: Record<string, unknown>) => Promise<unknown>;
  trace?: (params: Record<string, unknown>, options?: CallOptions) => Promise<unknown>;
  beforeAck?: (stream: Stream) => void;
  catalogRevision = '1';
  catalogItems = [{ id: 'root', kind: 'agent', title: 'Test', model: '', provider: '', cwd: '/', pinned: false, updated_at: '', truncated: false }];
  getSnapshot = () => this.connection;
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  onCommand = (fn: (outcome: { command_id: string; status: string }) => void) => { this.commands.add(fn); return () => { this.commands.delete(fn); }; };
  events = { subscribe: async (_rootId: string, cursor: string) => {
    const stream = new Stream();
    stream.cursor = cursor;
    this.streams.push(stream);
    this.beforeAck?.(stream);
    return stream;
  } };
  async call(method: string, params: Record<string, unknown>, options?: CallOptions): Promise<unknown> {
    this.calls.push({ method, params });
    if (method === 'root.snapshot') return structuredClone(this.root);
    if (method === 'trace.page') return this.trace?.(params, options);
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

test('a long committed turn recovers the interior history interval and its five observed executions', async t => {
  const host = new Host();
  const messages = Array.from({ length: 1658 }, (_, i) => ({ seq: i + 1, message: { role: 'assistant', content: `Record ${i + 1}` } }));
  messages[1588]!.message = { role: 'user', content: 'Synthetic long-turn request' };
  for (const seq of [1590, 1592, 1594, 1596, 1598]) {
    messages[seq - 1]!.message = { role: 'assistant', content: '', tool_calls: [{ id: `call-${seq}`, function: { name: 'rlm_exec', arguments: '{"code":"42"}' } }],
      presentation: { version: 1, turn_id: 'long', parts: [{ id: `part-${seq}`, kind: 'tool', call_id: `call-${seq}`, tool_name: 'rlm_exec' }] } } as typeof messages[number]['message'];
  }
  const recent = (first: number, last: number) => {
    const page = messages.slice(first - 1, last);
    host.root.messages = page.map(item => item.message);
    host.root.message_seqs = page.map(item => item.seq);
    host.root.omitted = { messages: true };
  };
  recent(1530, 1588);
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  let eventSeq = 10;
  for (const seq of [1590, 1592, 1594, 1596, 1598]) host.streams[0]!.push(String(++eventSeq), 'stream.tool.started', {
    id: `call-${seq}`, name: 'rlm_exec', part_id: `part-${seq}`, turn_id: 'long',
  });
  await until(() => view.getSnapshot().root?.cursor === String(eventSeq));
  const ids = executionRows(view.getSnapshot(), 'root').map(row => row.id);
  host.root.cursor = String(eventSeq);
  recent(1601, 1658);
  host.history = async params => {
    assert.equal(params.after_seq, 1588);
    assert.equal(params.through_seq, 1600);
    assert.equal(params.revision, '1');
    return { history_revision: '1', through_seq: 1600, next_seq: 1600, has_more: false, messages: messages.slice(1588, 1600) };
  };
  await view.refresh();
  await until(() => view.getSnapshot().history.root!.messages.some(item => item.seq === 1589));
  assert.deepEqual(view.getSnapshot().history.root!.messages.map(item => item.seq), messages.slice(1529).map(item => item.seq));
  const cells = executionRows(view.getSnapshot(), 'root');
  assert.deepEqual(cells.map(row => row.id), ids);
  assert.deepEqual(cells.map(row => row.seq), [1590, 1592, 1594, 1596, 1598]);
  assert.equal(host.calls.filter(call => call.method === 'history.page').length, 1);
});

test('text and reasoning retain one part across interleaved usage and host updates', async t => {
  const host = new Host();
  host.root.active_turns = { root: 'turn' };
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  let seq = 10;
  const events: NonNullable<RootSnapshot['presentation']> = [];
  const push = (kind: string, payload: StreamEvent) => {
    events.push({ seq: String(++seq), kind, payload });
    host.streams[0]!.push(String(seq), kind, payload);
  };
  for (const kind of ['stream.text', 'stream.reasoning']) {
    const payload = { agent_id: 'root', turn_id: 'turn', part_id: kind };
    push(kind, { ...payload, text: '1. **First**' });
    push('stream.usage', { agent_id: 'root' });
    push(kind, { ...payload, text: ' ' });
    push('stream.cell.host', { agent_id: 'root', id: 'call', invocation_id: 'host', name: 'files.read' });
    push(kind, { ...payload, text: 'item\n2. Second item' });
  }
  await until(() => view.getSnapshot().root?.cursor === String(seq));
  for (const kind of ['stream.text', 'stream.reasoning']) {
    const rows = view.getSnapshot().root?.presentation?.filter(row => row.kind === kind);
    assert.deepEqual(rows?.map(row => (row.payload as StreamEvent).text), ['1. **First** item\n2. Second item']);
  }
  const before = view.getSnapshot().root?.presentation;
  host.root.cursor = String(seq);
  host.root.presentation = events;
  await view.refresh();
  assert.deepEqual(view.getSnapshot().root?.presentation, before, 'refresh does not replay already observed fragments');
  host.notify('stale');
  host.notify('connected');
  await until(() => view.getSnapshot().status === 'live');
  assert.deepEqual(view.getSnapshot().root?.presentation, before, 'reconnect assembles raw snapshot deltas identically');
});

test('part assembly stops at notices, discarded output, and other turns', async t => {
  const host = new Host();
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  let seq = 10;
  const push = (kind: string, payload: StreamEvent) => host.streams[0]!.push(String(++seq), kind, { agent_id: 'root', ...payload });
  const text = (value: string, turn = 'turn') => push('stream.text', { part_id: 'part', turn_id: turn, text: value });
  text('A');
  push('stream.notice', { text: 'Notice' });
  text('B');
  push('stream.discard', { turn_id: 'turn' });
  text('C');
  push('stream.usage', {});
  text('D', 'next-turn');
  await until(() => view.getSnapshot().root?.cursor === String(seq));
  assert.deepEqual(view.getSnapshot().root?.presentation?.filter(row => row.kind === 'stream.text').map(row => (row.payload as StreamEvent).text), ['A', 'B', 'C', 'D']);
});

test('large cumulative calls retain child identity; legacy references never become root activity', async t => {
  const host = new Host();
  host.root.active_turns = { child: 'turn' };
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  const content = { reference_id: 'large', digest: 'a'.repeat(64), size: '12000', media_type: 'application/json' };
  let seq = 10;
  const push = (kind: string, payload: unknown) => host.streams[0]!.push(String(++seq), kind, payload);
  push('stream.tool.call', { agent_id: 'child', turn_id: 'turn', id: 'call', name: 'rlm_exec', args: '{"code":"print(1)"}' });
  for (let i = 0; i < 30; i++) {
    push('stream.tool.call', { truncated: true, content });
    push('stream.tool.call', { agent_id: 'child', turn_id: 'turn', id: 'call', name: 'rlm_exec', truncated: true, content });
  }
  push('stream.tool.started', { agent_id: 'child', turn_id: 'turn', id: 'call', name: 'rlm_exec', truncated: true, content });
  push('stream.tool.completed', { agent_id: 'child', turn_id: 'turn', id: 'call', name: 'rlm_exec', truncated: true, content });
  await until(() => view.getSnapshot().root?.cursor === String(seq));
  const state = view.getSnapshot();
  assert.equal(state.unavailable, true);
  assert.equal(state.root?.presentation?.length, 0);
  assert.equal(state.root?.agent_presentations?.child?.length, 3);
  assert.equal(state.executions?.rows.length, 1);
  assert.equal(state.executions?.rows[0]?.agentId, 'child');
  assert.equal(state.executions?.rows[0]?.closed, true);
  assert.equal(state.executions?.rows[0]?.kind === 'cell' && state.executions.rows[0].status, 'unknown', 'large completion ends activity without inventing success');
  assert.equal(host.calls.filter(call => call.method === 'content.read').length, 0);
});

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

test('archive and restore events update metadata without interrupting an open root or replacing its history', async t => {
  const host = new Host();
  host.root.active_turns = { root: 'running-turn' };
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  const before = view.getSnapshot();
  host.streams[0]!.push('11', 'session.archived.updated', { archived: true });
  await until(() => view.getSnapshot().root?.meta.archived === true);
  assert.equal(before.root?.meta.archived, false);
  assert.equal(view.getSnapshot().status, 'live');
  assert.deepEqual(view.getSnapshot().root?.active_turns, { root: 'running-turn' });
  assert.deepEqual(view.getSnapshot().history, before.history);
  host.streams[0]!.push('12', 'session.archived.updated', { archived: false });
  await until(() => view.getSnapshot().root?.meta.archived === false);
  assert.equal(host.streams.length, 1);
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
      const payload = (text: string) => ({ text, agent_id: agentId, part_id: 'part', turn_id: 'turn' });
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
      host.streams.at(-1)!.push('15', 'stream.usage', { agent_id: agentId });
      host.streams.at(-1)!.push('16', kind, payload('F'));
      host.streams.at(-1)!.push('17', kind, payload('G'));
      await until(() => view.getSnapshot().root?.cursor === '17');
      const rows = agentId === 'root' ? view.getSnapshot().root?.presentation : view.getSnapshot().root?.agent_presentations?.[agentId];
      assert.deepEqual(rows?.filter(row => row.kind === kind).map(row => (row.payload as StreamEvent).text), ['A', 'CD', 'FG']);
      assert.deepEqual(rows?.filter(row => row.kind === kind).map(row => row.seq), ['10', '12', '16']);
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

for (const agentId of ['root', 'child']) {
  test(`${agentId} history exhaustion survives refresh until its boundary is evicted or revised`, async () => {
    const host = new Host();
    const messages = (first: number, last: number) => Array.from({ length: last - first + 1 }, (_, index) => ({
      seq: first + index, message: { role: 'user' as const, content: String(first + index) },
    }));
    let recent = messages(3, 4);
    const setRecent = () => {
      host.root.messages = recent.map(item => item.message);
      host.root.message_seqs = recent.map(item => item.seq);
      host.root.first_message_seq = recent[0]!.seq;
      host.root.omitted = { messages: true };
    };
    setRecent();
    host.history = async params => {
      const page = params.before_seq ? messages(1, Number(params.before_seq) - 1) : recent;
      return { history_revision: host.root.history_revision, through_seq: recent.at(-1)!.seq,
        next_seq: page[0]?.seq ?? params.before_seq, has_more: page[0]!.seq > 1, messages: page };
    };
    const view = createSessionView(host.session(), { maxMessages: 4 });
    try {
      await view.start();
      if (agentId === 'child') await view.openAgent(agentId);
      assert.equal(view.getSnapshot().history[agentId]!.hasMore, true);
      await view.loadOlder(agentId);
      for (let attempt = 0; attempt < 2; attempt++) {
        await view.refresh();
        const history = view.getSnapshot().history[agentId]!;
        assert.deepEqual(history.messages.map(item => item.seq), [1, 2, 3, 4]);
        assert.equal(history.nextSeq, 1);
        assert.equal(history.hasMore, false);
        const count = host.calls.length;
        await view.loadOlder(agentId);
        assert.equal(host.calls.length, count, 'Exhausted history must not issue another RPC');
      }
      // A complete incoming page still needs older paging if the cache trims it.
      recent = messages(1, 6);
      setRecent();
      await view.refresh();
      assert.deepEqual(view.getSnapshot().history[agentId]!.messages.map(item => item.seq), [3, 4, 5, 6]);
      assert.equal(view.getSnapshot().history[agentId]!.nextSeq, 3);
      assert.equal(view.getSnapshot().history[agentId]!.hasMore, true);
      await view.loadOlder(agentId);
      assert.equal(view.getSnapshot().history[agentId]!.hasMore, false);
      recent = messages(3, 4);
      setRecent();
      host.root.history_revision = '2';
      await view.refresh();
      assert.deepEqual(view.getSnapshot().history[agentId]!.messages.map(item => item.seq), [3, 4]);
      assert.equal(view.getSnapshot().history[agentId]!.hasMore, true);
    } finally { await view.dispose(); }
  });
}

test('a terminal empty page exhausts a retained boundary and concurrent readers share a page', async () => {
  const host = new Host();
  host.root.messages = [{ role: 'user', content: 'retained' }];
  host.root.message_seqs = [5];
  const view = createSessionView(host.session());
  try {
    await view.start();
    let resolve!: (value: unknown) => void;
    host.history = () => new Promise(done => { resolve = done; });
    const before = host.calls.length;
    const pending = [view.loadOlder(), view.loadOlder()];
    assert.equal(host.calls.length, before + 1);
    resolve({ history_revision: '1', through_seq: 5, next_seq: 5, has_more: false, messages: [] });
    await Promise.all(pending);
    await view.refresh();
    assert.equal(view.getSnapshot().history.root!.messages[0]!.seq, 5);
    assert.equal(view.getSnapshot().history.root!.hasMore, false);
  } finally { await view.dispose(); }
});

test('empty root history and omitted bodies do not imply older messages', async () => {
  const host = new Host();
  host.root.omitted = { messages: true };
  const view = createSessionView(host.session());
  try {
    await view.start();
    assert.equal(view.getSnapshot().history.root!.hasMore, false);
    host.root = { ...snapshot('20', '2'), messages: [], message_seqs: [] };
    await view.refresh();
    assert.equal(view.getSnapshot().history.root!.hasMore, false);
    assert.equal(view.getSnapshot().history.root!.messages.length, 0);
    host.root.omitted = { messages: true };
    await view.refresh();
    await until(() => view.getSnapshot().history.root!.messages.length === 1);
    assert.equal(host.calls.filter(call => call.method === 'history.page').length, 1, 'An omitted empty snapshot is recovered automatically');
    assert.equal(view.getSnapshot().history.root!.hasMore, false);
    assert.equal(host.calls.at(-1)?.params.through_seq, -1);
    assert.equal(host.calls.at(-1)?.params.before_seq, undefined);
  } finally { await view.dispose(); }
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
  host.history = async () => ({ history_revision: '1', through_seq: 1, next_seq: 1, has_more: false,
    messages: [{ seq: 1, message: { role: 'assistant', content: 'recovered child' } }] });
  await view.loadOlder('child');
  assert.equal(view.getSnapshot().history.child?.messages[0]?.message?.content, 'recovered child');
  assert.equal(view.getSnapshot().history.child?.hasMore, false);
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

test('archive completion refreshes the active catalog once without waiting for the poll or opening a root', async t => {
  const host = new Host();
  const list = createSessionListView(host as unknown as WhipClient, { pollIntervalMs: 60_000 });
  const stop = list.subscribe(() => {});
  t.after(async () => { stop(); await list.dispose(); });
  await list.start();
  host.catalogRevision = '2'; host.catalogItems = [];
  for (const notify of host.commands) notify({ command_id: 'archive-root', status: 'running' });
  assert.equal(list.getSnapshot().page?.revision, '1');
  for (const notify of host.commands) notify({ command_id: 'archive-root', status: 'succeeded' });
  await until(() => list.getSnapshot().page?.revision === '2');
  assert.deepEqual(list.getSnapshot().page?.items, []);
  const calls = host.calls.length;
  for (const notify of host.commands) notify({ command_id: 'archive-root', status: 'succeeded' });
  await pause();
  assert.equal(host.calls.length, calls);
  assert.ok(host.calls.filter(call => call.method === 'sessions.list').every(call => call.params.status === 'active'));
  assert.equal(host.streams.length, 0);
});

test('an observed catalog stops its timer while paused and resumes polling on reconnect', async t => {
  const host = new Host();
  const list = createSessionListView(host as unknown as WhipClient, { pollIntervalMs: 10 });
  const stop = list.subscribe(() => {});
  t.after(async () => { stop(); await list.dispose(); });
  await list.start();
  const refresh = t.mock.method(list, 'refresh');
  host.notify('paused');
  await pause(35);
  assert.equal(refresh.mock.callCount(), 0, 'paused observers must not keep waking a timer');
  assert.equal(list.getSnapshot().status, 'stale');
  host.catalogRevision = '2';
  host.notify('connected');
  await until(() => list.getSnapshot().page?.revision === '2');
  const resumed = refresh.mock.callCount();
  await until(() => refresh.mock.callCount() > resumed);
  assert.equal(list.getSnapshot().status, 'live');
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
  const bytes = new TextEncoder().encode(JSON.stringify({ root: current.root, history: current.history, collections: current.collections, unverifiedInbox: current.unverifiedInbox, executions: current.executions })).byteLength;
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

test('saved child failure survives refresh without inventing an execution cell', async t => {
  const host = new Host();
  const child: NonNullable<RootSnapshot['agents']>[number] = {
    execution_engine: 'starlark',
    id: 'child', root_id: 'root', parent_id: 'root', name: 'Research', model: 'model', provider: 'provider', effort: '', cwd: '/', report: 'notice', status: 'idle',
    pending_mail: 0, lifecycle_phase: 'idle', blocking_reason: '', terminal_cause: '', allowed_controls: [],
  };
  host.root.agents = [child];
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  host.streams.at(-1)!.push('11', 'agent.turn.started', { agent_id: 'child', turn_id: 'child-turn', status: 'running' });
  await until(() => view.getSnapshot().root?.active_turns.child === 'child-turn');
  host.root = { ...host.root, cursor: '12', active_turns: {}, agents: [{ ...child, last_turn: { turn_id: 'child-turn', status: 'failed', event_seq: '12', error: 'Invalid prompt_cache_key' } }] };
  host.streams.at(-1)!.push('12', 'agent.turn.failed', { agent_id: 'child', turn_id: 'child-turn', status: 'failed', error: 'Invalid prompt_cache_key' });
  await until(() => view.getSnapshot().root?.agents?.[0]?.last_turn?.status === 'failed');
  assert.equal(view.getSnapshot().root?.active_turns.child, undefined);
  assert.deepEqual(executionRows(view.getSnapshot(), 'child'), []);
  await view.refresh();
  assert.equal(view.getSnapshot().root?.agents?.[0]?.last_turn?.error, 'Invalid prompt_cache_key');
  assert.equal(host.streams.filter(stream => !stream.closed).length, 1);
  host.root = { ...host.root, cursor: '14', agents: [{ ...child, last_turn: { turn_id: 'next-turn', status: 'succeeded', event_seq: '14' } }] };
  host.streams.at(-1)!.push('13', 'agent.turn.started', { agent_id: 'child', turn_id: 'next-turn', status: 'running' });
  host.streams.at(-1)!.push('14', 'agent.turn.succeeded', { agent_id: 'child', turn_id: 'next-turn', status: 'succeeded' });
  await until(() => view.getSnapshot().root?.agents?.[0]?.last_turn?.status === 'succeeded');
  assert.equal(view.getSnapshot().root?.agents?.[0]?.last_turn?.error, undefined);
});

function historyRecords(first: number, last: number) {
  return Array.from({ length: last - first + 1 }, (_, index) => ({ seq: first + index, message: { role: 'assistant', content: `record ${first + index}` } }));
}
function historySnapshot(host: Host, first: number, last: number) {
  const records = historyRecords(first, last);
  host.root.messages = records.map(item => item.message);
  host.root.message_seqs = records.map(item => item.seq);
}
function gapPage(params: Record<string, unknown>, count = 128) {
  const first = Number(params.after_seq) + 1, last = Math.min(Number(params.through_seq), first + count - 1);
  return { history_revision: '1', through_seq: params.through_seq, next_seq: last, has_more: last < Number(params.through_seq), messages: historyRecords(first, last) };
}

test('gap repair is capped, shares readers, consumes live events, and preserves its retry identity', async t => {
  const host = new Host();
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  let release!: (page: unknown) => void;
  host.history = () => new Promise(resolve => { release = resolve; });
  historySnapshot(host, 20, 22);
  await view.refresh();
  const history = view.getSnapshot().history.root!;
  assert.deepEqual(history.gaps, [{ fromSeq: 2, toSeq: 19, status: 'loading', error: undefined }]);
  assert.equal(history.loading, false, 'Gap loading leaves the current transcript usable');
  const shared = view.loadHistoryGap('root', 19);
  assert.equal(host.calls.filter(call => call.method === 'history.page').length, 1);
  host.streams.at(-1)!.push('11', 'stream.text', { text: 'still streaming', turn_id: 'next' });
  await until(() => view.getSnapshot().root?.cursor === '11');
  host.history = async params => gapPage(params, 2);
  release(gapPage({ after_seq: 1, through_seq: 19 }, 2));
  await shared;
  await until(() => view.getSnapshot().history.root!.gaps?.[0]?.status === 'paused');
  assert.equal(host.calls.filter(call => call.method === 'history.page').length, 4);
  assert.deepEqual(view.getSnapshot().history.root!.gaps?.map(gap => [gap.fromSeq, gap.toSeq]), [[10, 19]]);
  await view.refresh();
  assert.equal(host.calls.filter(call => call.method === 'history.page').length, 4, 'Ordinary refresh cannot restart exhausted work');
  await view.loadHistoryGap('root', 19);
  assert.equal(view.getSnapshot().history.root!.gaps?.[0]?.fromSeq, 12);
  assert.equal(view.getSnapshot().history.root!.gaps?.[0]?.status, 'paused');
  assert.ok(host.calls.filter(call => call.method === 'history.page').every(call => call.params.limit === 128 && call.params.max_bytes === 256 * 1024));
});

for (const invalid of ['empty', 'out-of-order', 'wrong-end', 'error']) test(`invalid gap read (${invalid}) stays local and supports explicit retry`, async t => {
  const host = new Host();
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  historySnapshot(host, 5, 7);
  host.history = async params => {
    if (invalid === 'error') throw new Error('x'.repeat(2048));
    const page = gapPage(params);
    return { ...page, ...(invalid === 'empty' ? { messages: [] } : invalid === 'wrong-end' ? { through_seq: 99 } : { messages: [...page.messages].reverse() }) };
  };
  await view.refresh();
  await until(() => view.getSnapshot().history.root!.gaps?.[0]?.status === 'error');
  assert.equal(view.getSnapshot().status, 'live');
  assert.equal(view.getSnapshot().error, undefined);
  assert.ok(view.getSnapshot().history.root!.gaps![0]!.error!.length <= 512);
  await view.refresh();
  assert.equal(host.calls.filter(call => call.method === 'history.page').length, 1);
  host.history = async params => gapPage(params);
  await view.loadHistoryGap('root', 4);
  assert.deepEqual(view.getSnapshot().history.root!.gaps, []);
  assert.equal(view.getSnapshot().history.root!.hasMore, false);
});

test('refresh and revision changes reject both stale gap successes and errors', async t => {
  const host = new Host();
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  let reject!: (error: Error) => void;
  host.history = () => new Promise((_, fail) => { reject = fail; });
  historySnapshot(host, 5, 7);
  await view.refresh();
  host.root = snapshot('20', '2');
  await view.refresh();
  reject(new Error('obsolete'));
  await pause();
  assert.equal(view.getSnapshot().history.root!.revision, '2');
  assert.deepEqual(view.getSnapshot().history.root!.gaps, []);
  assert.equal(view.getSnapshot().error, undefined);
  let resolve!: (value: unknown) => void;
  host.history = () => new Promise(done => { resolve = done; });
  historySnapshot(host, 5, 7);
  await view.refresh();
  host.root = snapshot('30', '3');
  await view.refresh();
  resolve({ ...gapPage({ after_seq: 1, through_seq: 4 }), history_revision: '2' });
  await pause();
  assert.equal(view.getSnapshot().history.root!.revision, '3');
  assert.equal(view.getSnapshot().history.root!.messages.length, 1);
});

test('opened child coverage repairs without opening other agents and close/reopen rejects late reads', async t => {
  const host = new Host();
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  host.history = async () => ({ history_revision: '1', through_seq: 1, next_seq: 1, has_more: false, messages: historyRecords(1, 1) });
  await view.openAgent('child');
  host.history = async params => params.after_seq !== undefined ? gapPage(params) : { history_revision: '1', through_seq: 7, next_seq: 5, has_more: true, messages: historyRecords(5, 7) };
  await view.refresh();
  await until(() => view.getSnapshot().history.child?.messages.length === 7);
  assert.deepEqual(Object.keys(view.getSnapshot().history).sort(), ['child', 'root']);
  assert.equal(host.streams.filter(stream => !stream.closed).length, 1);
  let resolve!: (value: unknown) => void;
  host.history = () => new Promise(done => { resolve = done; });
  const oldRead = view.loadLatest('child');
  view.closeAgent('child');
  host.history = async () => ({ history_revision: '1', through_seq: 8, next_seq: 8, has_more: false, messages: historyRecords(8, 8) });
  const reopened = view.openAgent('child');
  resolve({ history_revision: '1', through_seq: 99, next_seq: 99, has_more: false, messages: historyRecords(99, 99) });
  await Promise.all([oldRead, reopened]);
  assert.deepEqual(view.getSnapshot().history.child!.messages.map(item => item.seq), [8]);
});

test('omitting every snapshot message reads a bounded recent page even with older retained history', async t => {
  const host = new Host();
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  host.root.messages = []; host.root.message_seqs = []; host.root.omitted = { messages: true };
  host.history = async params => params.after_seq !== undefined ? gapPage(params) : { history_revision: '1', through_seq: 10, next_seq: 5, has_more: true, messages: historyRecords(5, 10) };
  await view.refresh();
  await until(() => view.getSnapshot().history.root!.messages.length === 10);
  assert.equal(view.getSnapshot().history.root!.latestMissing, false);
  assert.deepEqual(view.getSnapshot().history.root!.gaps, []);
  await view.refresh();
  assert.equal(host.calls.filter(call => call.method === 'history.page').length, 2);
});

test('manual recovery retains its page, exposes an evicted suffix, and Latest restores recent records within budget', async t => {
  const host = new Host();
  const view = createSessionView(host.session(), { maxMessages: 6 });
  t.after(() => view.dispose());
  await view.start();
  historySnapshot(host, 20, 21);
  host.history = async () => { throw new Error('offline history'); };
  await view.refresh();
  await until(() => view.getSnapshot().history.root!.gaps?.[0]?.status === 'error');
  host.history = async params => params.after_seq !== undefined ? gapPage(params, 6) : { history_revision: '1', through_seq: 21, next_seq: 16, has_more: true, messages: historyRecords(16, 21) };
  await view.loadHistoryGap('root', 19);
  assert.deepEqual(view.getSnapshot().history.root!.messages.map(item => item.seq), [2, 3, 4, 5, 6, 7]);
  assert.equal(view.getSnapshot().history.root!.latestMissing, true);
  await view.loadLatest();
  assert.deepEqual(view.getSnapshot().history.root!.messages.map(item => item.seq), [16, 17, 18, 19, 20, 21]);
  assert.equal(view.getSnapshot().history.root!.latestMissing, false);
  assert.ok(view.getSnapshot().retainedBytes <= 8 * 1024 * 1024);
  assert.deepEqual(view.getSnapshot().history.root!.gaps, []);
});

test('reconnection retries a failed range, and contiguous offloaded bodies are not gaps', async t => {
  const host = new Host();
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  historySnapshot(host, 5, 7);
  host.history = async () => { throw new Error('try after reconnect'); };
  await view.refresh();
  await until(() => view.getSnapshot().history.root!.gaps?.[0]?.status === 'error');
  host.notify('stale');
  host.history = async params => ({ ...gapPage(params), messages: historyRecords(2, 4).map(({ seq }) => ({ seq, role: 'assistant', body: { reference_id: `body-${seq}`, size: '999999', digest: 'digest', media_type: 'application/json' } })) });
  host.notify('connected');
  await until(() => view.getSnapshot().status === 'live' && view.getSnapshot().history.root!.gaps?.length === 0);
  assert.equal(view.getSnapshot().history.root!.messages.filter(item => item.body).length, 3);
  assert.ok(host.calls.every(call => call.method !== 'content.read'));
});

test('new snapshot gaps and a cached paused gap keep independent recovery state', async t => {
  const host = new Host();
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  host.history = async params => gapPage(params, 1);
  historySnapshot(host, 20, 22);
  await view.refresh();
  await until(() => view.getSnapshot().history.root!.gaps?.[0]?.status === 'paused');
  historySnapshot(host, 25, 27);
  await view.refresh();
  await until(() => view.getSnapshot().history.root!.messages.some(item => item.seq === 24));
  assert.deepEqual(view.getSnapshot().history.root!.gaps?.map(gap => [gap.fromSeq, gap.toSeq, gap.status]), [[6, 19, 'paused']]);
  assert.equal(host.calls.filter(call => call.method === 'history.page').length, 6);
});

test('manual gap navigation keeps a dropped zero-based root prefix reachable', async t => {
  const host = new Host();
  historySnapshot(host, 0, 0);
  const view = createSessionView(host.session(), { maxMessages: 3 });
  t.after(() => view.dispose());
  await view.start();
  host.history = async () => { throw new Error('defer recovery'); };
  historySnapshot(host, 10, 10);
  await view.refresh();
  await until(() => view.getSnapshot().history.root!.gaps?.[0]?.status === 'error');
  host.history = async params => gapPage(params, 3);
  await view.loadHistoryGap('root', 9);
  assert.deepEqual(view.getSnapshot().history.root!.messages.map(item => item.seq), [1, 2, 3]);
  assert.equal(view.getSnapshot().history.root!.hasMore, true, 'The evicted record zero stays pageable');
  assert.equal(view.getSnapshot().history.root!.latestMissing, true);
});


test('inbox projection respects recipient, newer pages, partial evidence and stable collection revisions', () => {
  const root = snapshot('20');
  root.collection_revision = '5'; root.omitted = { inbox: true };
  const input = (seq: string, agent = 'root', status = 'queued') => ({ root_id: 'root', agent_id: agent, seq, kind: 'submit', status, origin: 'client', payload: { text: seq, reference_id: '', digest: '', size: '1', media_type: '', source: '' } });
  root.inbox = [input('1')];
  const page = { root_id: 'root', collection: 'inbox', revision: '5', event_cursor: '15', items: [{ inbox: input('2') }, { inbox: input('1', 'child') }], has_more: false };
  const state = { root, status: 'live' as const, history: {}, collections: { inbox: page }, retainedBytes: 0, truncated: true, unavailable: false };
  assert.deepEqual(inboxItems(state, 'root').rows.map(row => [row.item.seq, row.stale]), [['1', false], ['2', false]]);
  assert.equal(inboxItems(state, 'root').hasMore, false);
  root.collection_revision = '6';
  assert.equal(inboxItems(state, 'root').rows[1]?.stale, true);
  page.event_cursor = '25'; page.items = [{ inbox: input('1', 'root', 'running') }];
  assert.equal(inboxItems(state, 'root').rows[0]?.item.status, 'running');
  root.omitted.inbox = false;
  assert.equal(inboxItems(state, 'root').rows.length, 1);
});

test('partial snapshots keep missing waiting inputs unverified until a page or lifecycle event resolves them', async t => {
  const host = new Host();
  host.root.inbox = ['1', '2'].map(seq => ({ root_id: 'root', agent_id: 'root', seq, origin: 'client', kind: 'submit', status: 'queued',
    payload: { text: seq, reference_id: '', digest: '', size: '1', media_type: '', source: '' } }));
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  host.root.inbox = []; host.root.omitted = { inbox: true };
  await view.refresh();
  assert.deepEqual(inboxItems(view.getSnapshot(), 'root').rows.map(row => [row.item.seq, row.stale]), [['1', true], ['2', true]]);
  host.streams.at(-1)!.push('11', 'inbox.running', { agent_id: 'root', inbox_seq: '2', turn_id: 'turn' });
  await until(() => inboxItems(view.getSnapshot(), 'root').rows.some(row => row.item.status === 'running'));
  assert.deepEqual(inboxItems(view.getSnapshot(), 'root').rows.map(row => [row.item.seq, row.stale, row.item.delivery_seq]), [['1', true, undefined], ['2', false, '11']]);
  host.root.cursor = '11';
  await view.loadCollection('inbox');
  assert.equal(view.getSnapshot().unverifiedInbox?.length, 0);
  assert.ok(view.getSnapshot().retainedBytes <= 8 << 20);
});

test('a steer delivery boundary prevents adjacent response fragments from coalescing across authored input', async t => {
  const host = new Host();
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  const stream = host.streams.at(-1)!;
  stream.push('11', 'stream.text', { text: 'Before.' });
  stream.push('12', 'inbox.running', { agent_id: 'root', inbox_seq: '1', turn_id: 'turn' });
  stream.push('13', 'stream.text', { text: 'After.' });
  await until(() => view.getSnapshot().root?.cursor === '13');
  assert.deepEqual(view.getSnapshot().root?.presentation?.map(row => row.payload), [{ text: 'Before.' }, { text: 'After.' }]);
  host.root = { ...snapshot('13'), presentation: [
    { seq: '11', kind: 'stream.text', payload: { text: 'Before.' } },
    { seq: '13', kind: 'stream.text', payload: { text: 'After.' } },
  ], inbox: [{ root_id: 'root', agent_id: 'root', seq: '1', kind: 'submit', status: 'running', delivery_seq: '12',
    payload: { text: 'B', reference_id: '', digest: '', size: '1', media_type: '', source: '' } }] };
  const reopened = createSessionView(host.session());
  t.after(() => reopened.dispose());
  await reopened.start();
  assert.deepEqual(reopened.getSnapshot().root?.presentation?.map(row => row.payload), [{ text: 'Before.' }, { text: 'After.' }]);
});

function recordedTracePage(seq = '1', hasMore = false): SpanPage {
  return { root_id: 'root', next_seq: seq, has_more: hasMore, server_time_ns: '1700000000000000000', spans: [{
    id: `span-${seq}`, trace_id: 'old-turn', root_id: 'root', agent_id: 'root', kind: 'agent', name: 'old turn', status: 'ok',
    start_ns: '1600000000000000000', end_ns: '1600000000001250000', updated_seq: seq,
  }] };
}
function pendingTracePage() {
  let resolve!: (page: SpanPage) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<SpanPage>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

test('historical trace reads survive snapshot refresh and concurrent callers wait for the same page', async t => {
  const host = new Host();
  const pending = pendingTracePage();
  host.trace = () => pending.promise;
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  const first = view.loadTrace();
  let joined = false;
  const second = view.loadTrace().then(() => { joined = true; });
  await until(() => !!view.getSnapshot().trace?.loading);
  assert.equal(joined, false);
  await view.refresh();
  pending.resolve(recordedTracePage());
  await Promise.all([first, second]);
  const trace = view.getSnapshot().trace!;
  assert.equal(trace.loading, false);
  assert.equal(trace.loaded, true);
  assert.equal(trace.spans['span-1']!.startMs, 1600000000000);
  assert.equal(trace.spans['span-1']!.endMs - trace.spans['span-1']!.startMs, 1.25);
  assert.equal(host.calls.filter(call => call.method === 'trace.page').length, 1);
});

test('trace errors after refresh remain retryable and empty historical sessions settle as loaded', async t => {
  const host = new Host();
  const pending = pendingTracePage();
  host.trace = () => pending.promise;
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  const load = view.loadTrace();
  const rejected = assert.rejects(load, /timed out/);
  await until(() => !!view.getSnapshot().trace?.loading);
  await view.refresh();
  pending.reject(new Error('Request timed out: trace.page'));
  await rejected;
  assert.equal(view.getSnapshot().trace!.loading, false);
  assert.match(view.getSnapshot().trace!.error!.message, /timed out/);
  host.trace = async () => ({ ...recordedTracePage('0'), spans: [] });
  await view.loadTrace();
  assert.equal(view.getSnapshot().trace!.loaded, true);
  assert.equal(view.getSnapshot().trace!.loading, false);
  assert.equal(view.getSnapshot().trace!.error, undefined);
  assert.deepEqual(view.getSnapshot().trace!.spans, {});
});

test('trace disconnect aborts only the old read and late replies cannot overwrite reconnect results', async t => {
  const host = new Host();
  const first = pendingTracePage();
  const next = pendingTracePage();
  let signal: AbortSignal | undefined;
  host.trace = (_params, options) => { signal = options?.signal; return first.promise; };
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  const oldLoad = view.loadTrace();
  await until(() => !!signal);
  host.notify('disconnected');
  assert.equal(signal!.aborted, true);
  assert.equal(view.getSnapshot().trace!.loading, false);
  host.trace = () => next.promise;
  host.notify('connected');
  await until(() => view.getSnapshot().status === 'live');
  const newLoad = view.loadTrace();
  await until(() => !!view.getSnapshot().trace?.loading);
  first.resolve(recordedTracePage('1'));
  await oldLoad;
  assert.equal(view.getSnapshot().trace!.loading, true, 'old finally must not clear the new loading flag');
  assert.equal(view.getSnapshot().trace!.spans['span-1'], undefined);
  next.resolve(recordedTracePage('2'));
  await newLoad;
  assert.equal(view.getSnapshot().trace!.loading, false);
  assert.equal(view.getSnapshot().trace!.pageCursor, '2');
});

test('trace pagination pauses after eight pages and continues from its durable cursor, including after an error', async t => {
  const host = new Host();
  host.trace = async params => recordedTracePage(String(Number(params.after_seq) + 1), true);
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  await view.loadTrace();
  assert.equal(view.getSnapshot().trace!.pageCursor, '8');
  assert.equal(view.getSnapshot().trace!.hasMore, true);
  assert.equal(view.getSnapshot().trace!.loading, false);
  assert.equal(host.calls.filter(call => call.method === 'trace.page').length, 8);
  host.trace = async params => {
    if (params.after_seq === '8') return recordedTracePage('9', true);
    throw new Error('page failed');
  };
  await assert.rejects(view.loadTrace(), /page failed/);
  assert.equal(view.getSnapshot().trace!.pageCursor, '9');
  assert.equal(view.getSnapshot().trace!.loading, false);
  host.trace = async params => { assert.equal(params.after_seq, '9'); return recordedTracePage('10'); };
  await view.loadTrace();
  assert.equal(view.getSnapshot().trace!.hasMore, false);
  assert.equal(view.getSnapshot().trace!.error, undefined);
  assert.equal(Object.keys(view.getSnapshot().trace!.spans).length, 10);
});

test('trace pages cannot regress live spans or advance the durable cursor from live events', async t => {
  const host = new Host();
  const pending = pendingTracePage();
  host.trace = () => pending.promise;
  const view = createSessionView(host.session(), { notificationIntervalMs: 1 });
  t.after(() => view.dispose());
  await view.start();
  const load = view.loadTrace();
  await until(() => !!view.getSnapshot().trace?.loading);
  const live = { ...recordedTracePage().spans![0]!, updated_seq: '11', end_ns: '1600000000002500000' };
  host.streams.at(-1)!.push('11', 'span.ended', live);
  await until(() => view.getSnapshot().trace?.spans['span-1']?.updatedSeq === '11');
  pending.resolve(recordedTracePage());
  await load;
  assert.equal(view.getSnapshot().trace!.spans['span-1']!.updatedSeq, '11');
  assert.equal(view.getSnapshot().trace!.pageCursor, '1');
  host.trace = async params => { assert.equal(params.after_seq, '1'); return recordedTracePage('11'); };
  host.notify('disconnected');
  host.notify('connected');
  await until(() => view.getSnapshot().status === 'live');
  await view.loadTrace();
  assert.equal(view.getSnapshot().trace!.pageCursor, '11');
});

test('trace disposal cancels pending transport and wrong-root/nonadvancing pages are retryable failures', async t => {
  const host = new Host();
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  host.trace = async () => ({ ...recordedTracePage(), root_id: 'other' });
  await assert.rejects(view.loadTrace(), /different root/);
  assert.equal(view.getSnapshot().trace!.loading, false);
  host.trace = async () => recordedTracePage('0', true);
  await assert.rejects(view.loadTrace(), /did not advance/);
  assert.equal(view.getSnapshot().trace!.pageCursor, '0');
  const pending = pendingTracePage();
  let signal: AbortSignal | undefined;
  host.trace = (_params, options) => { signal = options?.signal; return pending.promise; };
  const load = view.loadTrace();
  await until(() => !!signal);
  await view.dispose();
  assert.equal(signal!.aborted, true);
  pending.resolve(recordedTracePage());
  await load;
  assert.equal(view.getSnapshot().trace, undefined);
  await assert.rejects(view.loadTrace(), /closed/);
});

test('runtime replacement cancels old trace evidence and refuses new trace reads on that view', async t => {
  const host = new Host();
  const pending = pendingTracePage();
  let signal: AbortSignal | undefined;
  host.trace = (_params, options) => { signal = options?.signal; return pending.promise; };
  const view = createSessionView(host.session());
  t.after(() => view.dispose());
  await view.start();
  const load = view.loadTrace();
  await until(() => !!signal);
  host.notify('connected', 'different-runtime');
  assert.equal(signal!.aborted, true);
  assert.equal(view.getSnapshot().trace!.loading, false);
  pending.reject(new Error('late old-runtime failure'));
  await load;
  assert.equal(view.getSnapshot().trace!.error, undefined);
  assert.deepEqual(view.getSnapshot().trace!.spans, {});
  await assert.rejects(view.loadTrace(), /runtime changed/);
});
