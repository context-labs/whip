import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client } from '../dist/index.js';
import { createSessionView, createTreeCatalogView } from '../dist/state.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const owner = 'session_child';
const message = (sequence, text = 'body') => ({ ...fixture('Message'), id: 'message_' + sequence, session_id: owner, sequence: String(sequence), parts: [{ type: 'text', text }] });
async function backend() {
  const state = { activity: { ...fixture('SessionActivity'), session_id: owner, active_turn: null, active_input_id: null }, messages: [], revision: '1', epoch: 'boot_one', preview: null, calls: [], intercept: undefined, catalogRevision: '1', trees: [] };
  const snapshot = () => ({ session_id: owner, revision: state.revision, through_sequence: state.messages.at(-1)?.sequence ?? '0', message_count: String(state.messages.length) });
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: initial };
    state.calls.push(structuredClone(request)); await state.intercept?.(request);
    const p = request.params;
    const conflict = () => ({ jsonrpc: '2.0', id: request.id, error: { code: -32004, kind: 'CONFLICT', message: 'revision changed' } });
    let result;
    if (request.method === 'sessions.activity') result = state.activity;
    else if (request.method === 'sessions.history_page') {
      if (p.expected_revision !== undefined && p.expected_revision !== state.revision) return conflict();
      const all = state.messages.filter(value => p.cursor === undefined || (p.direction === 'backward' ? BigInt(value.sequence) < BigInt(p.cursor) : BigInt(value.sequence) > BigInt(p.cursor)));
      const messages = p.direction === 'backward' ? all.slice(-p.limit) : all.slice(0, p.limit);
      result = { snapshot: snapshot(), messages, next_cursor: all.length > messages.length ? (p.direction === 'backward' ? messages[0].sequence : messages.at(-1).sequence) : null };
    } else if (request.method === 'sessions.observe') {
      if (p.expected_revision !== undefined && p.expected_revision !== state.revision) return conflict();
      result = { snapshot: snapshot(), epoch: state.epoch, preview: state.preview, messages: state.messages.filter(value => BigInt(value.sequence) > BigInt(p.after)).slice(0, p.limit) };
    } else if (request.method === 'trees.catalog') result = { revision: state.catalogRevision };
    else if (request.method === 'trees.list') {
      if (p.expected_revision !== undefined && p.expected_revision !== state.catalogRevision) return conflict();
      const all = state.trees.filter(value => (!p.after || value.tree.id > p.after) && (p.archived === undefined || p.archived === value.tree.metadata.archived) && (p.pinned === undefined || p.pinned === value.tree.metadata.pinned));
      const items = all.slice(0, p.limit); result = { revision: state.catalogRevision, items, next_cursor: all.length > items.length ? items.at(-1).tree.id : null };
    } else throw new Error(request.method);
    return { jsonrpc: '2.0', id: request.id, result: structuredClone(result) };
  }, { clientID: 'view' });
  return { state, client };
}
const viewFor = (t, client, options = {}) => { const view = createSessionView(client.session(owner), { pollIntervalMs: 60_000, pageSize: 5, ...options }); t.after(() => view.dispose()); return view; };

test('large history starts at the actual tail and navigates bounded windows across sparse exact counters', async t => {
  const { state, client } = await backend();
  const base = 9007199254740993n;
  state.messages = Array.from({ length: 3000 }, (_, index) => message(base + BigInt(index) * 1000n));
  const view = viewFor(t, client, { maxMessages: 5 }); await view.start();
  assert.equal(state.calls.length, 3); assert.equal(state.calls[1].method, 'sessions.history_page'); assert.equal(state.calls[1].params.cursor, undefined);
  assert.deepEqual(view.getSnapshot().history.messages.map(value => value.sequence), state.messages.slice(-5).map(value => value.sequence));
  const first = view.getSnapshot().history.messages[0].sequence;
  await view.loadOlder();
  assert.equal(state.calls.at(-1).params.cursor, first);
  assert.deepEqual(view.getSnapshot().history.messages.map(value => value.sequence), state.messages.slice(-10, -5).map(value => value.sequence));
  assert.equal(view.getSnapshot().history.latestMissing, true);
  state.messages.push(message(base + 9_000_000n)); await view.refresh();
  assert.equal(view.getSnapshot().history.messages.length, 5); assert.equal(view.getSnapshot().history.messages[0].sequence, state.messages.at(-11).sequence);
  await view.latest(); assert.equal(view.getSnapshot().history.messages.at(-1).sequence, state.messages.at(-1).sequence);
  assert.equal(view.getSnapshot().history.latestMissing, false);
});

test('snapshot/observation handoff retains intervening append, revision replacement and epoch preview clearing', async t => {
  const { state, client } = await backend(); state.messages = [message(1), message(100)];
  state.preview = fixture('SessionObservation').preview;
  let appended = false;
  state.intercept = request => { if (request.method === 'sessions.observe' && !appended) { appended = true; state.messages.push({ ...message(101), id: state.preview.message_id }); } };
  const view = viewFor(t, client); await view.start();
  assert.equal(view.getSnapshot().history.messages.length, 3); assert.equal(view.getSnapshot().preview, null);
  state.intercept = undefined; state.revision = '2'; state.messages = [message(1, 'retained'), message(9007199254740993n, 'after rewind')];
  await view.refresh(); assert.equal(view.getSnapshot().history.snapshot.revision, '2');
  assert.deepEqual(view.getSnapshot().history.messages.map(value => value.parts[0].text), ['retained', 'after rewind']);
  state.preview = fixture('SessionObservation').preview; await view.refresh(); assert.ok(view.getSnapshot().preview);
  state.epoch = 'boot_two'; state.preview = null; await view.refresh();
  assert.equal(view.getSnapshot().epoch, 'boot_two'); assert.equal(view.getSnapshot().preview, null);
});

test('oversized messages are explicit bounded gaps and error presentation cannot grow retained bytes', async t => {
  const { state, client } = await backend(); state.messages = [message(1, 'x'.repeat(30_000))];
  const view = viewFor(t, client, { maxBytes: 4096 }); await view.start();
  const snapshot = view.getSnapshot(); assert.equal(snapshot.history.messages.length, 0); assert.equal(snapshot.history.gaps[0].messageID, 'message_1');
  assert.equal(snapshot.truncated, true); assert.ok(snapshot.retainedBytes <= 4096); assert.ok(Object.isFrozen(snapshot.history.gaps));
  state.intercept = () => { throw new Error('\u0000'.repeat(1000)); }; await view.refresh();
  assert.equal(view.getSnapshot().status, 'stale'); assert.equal(view.getSnapshot().error.message.length, 256); assert.ok(view.getSnapshot().retainedBytes <= 4096);
  assert.equal(view.getSnapshot().retainedBytes, Buffer.byteLength(JSON.stringify(view.getSnapshot())));
});

test('suspension joins pending observation and discards a late page before explicit resume', async t => {
  const { state, client } = await backend(); state.messages = Array.from({ length: 20 }, (_, index) => message(index + 1));
  const view = viewFor(t, client, { maxMessages: 5 }); await view.start();
  let release, entered;
  const started = new Promise(resolve => { entered = resolve; }); const held = new Promise(resolve => { release = resolve; });
  state.intercept = async request => { if (request.method === 'sessions.history_page') { entered(); await held; } };
  const older = view.loadOlder(); await started;
  const stopped = view.suspend(); release(); await Promise.all([older, stopped]);
  assert.equal(view.getSnapshot().status, 'suspended'); assert.equal(view.getSnapshot().history.messages[0].sequence, '16');
  const count = state.calls.length; await view.refresh(); assert.equal(state.calls.length, count);
  state.intercept = undefined; state.messages.push(message(21)); await view.resume(); assert.equal(view.getSnapshot().history.messages.at(-1).sequence, '21');
});

test('foreign history ownership fails closed and concurrent refreshes have one bounded fetch', async t => {
  const { state, client } = await backend(); state.messages = [{ ...message(1), session_id: 'foreign' }];
  const view = viewFor(t, client); await Promise.all(Array.from({ length: 100 }, () => view.start()));
  assert.equal(state.calls.length, 2); assert.equal(view.getSnapshot().unavailable, true); assert.equal(view.getSnapshot().history.messages.length, 0);
});

function trees(count) { return Array.from({ length: count }, (_, index) => { const item = fixture('ListTreesResult').items[0]; item.tree.id = 'tree_' + String(index).padStart(4, '0'); item.root_id = 'root_' + index; return item; }); }
test('catalog head invalidates off-page changes without hydrating sessions; capacity remains navigable', async t => {
  const { state, client } = await backend(); state.trees = trees(20);
  const view = createTreeCatalogView(client, { maxItems: 4, pageSize: 2, pollIntervalMs: 60_000 }); t.after(() => view.dispose());
  await view.start(); await view.loadMore(); assert.equal(view.getSnapshot().items.length, 4); assert.equal(view.getSnapshot().truncated, true);
  await view.next(); assert.equal(view.getSnapshot().items[0].tree.id, 'tree_0004'); assert.equal(view.getSnapshot().items.length, 2);
  state.catalogRevision = '9007199254740993'; state.trees[19].tree.metadata.title = 'off-page edit';
  await view.refresh(); assert.equal(view.getSnapshot().revision, state.catalogRevision); assert.equal(view.getSnapshot().windowAfter, 'tree_0003');
  const count = state.calls.length; await view.refresh(); assert.equal(state.calls.length, count + 1); assert.equal(state.calls.at(-1).method, 'trees.catalog');
  assert.ok(state.calls.every(value => value.method.startsWith('trees.')));
});

test('catalog revision conflicts replace a page window instead of mixing generations', async t => {
  const { state, client } = await backend(); state.trees = trees(6);
  const view = createTreeCatalogView(client, { pageSize: 2, pollIntervalMs: 60_000 }); t.after(() => view.dispose()); await view.start();
  state.intercept = request => { if (request.method === 'trees.list' && request.params.expected_revision) { state.catalogRevision = '2'; state.trees[0].tree.metadata.title = 'fresh'; } };
  await view.loadMore(); assert.equal(view.getSnapshot().revision, '2'); assert.equal(view.getSnapshot().items.length, 2); assert.equal(view.getSnapshot().items[0].tree.metadata.title, 'fresh');
  assert.ok(view.getSnapshot().retainedBytes < (2 << 20));
});

// A slow UI does not need a second event queue. It reads one latest immutable
// snapshot; reentrant refreshes join the same in-flight observation.
test('slow and reentrant consumers retain one snapshot without starting duplicate polls', async t => {
  const { state, client } = await backend(); state.messages = [message(1)];
  const view = viewFor(t, client);
  let notifications = 0;
  const unsubscribe = view.subscribe(() => { notifications++; void view.refresh(); });
  await view.start();
  assert.equal(state.calls.length, 3);
  const old = view.getSnapshot();
  unsubscribe();
  for (let index = 2; index <= 40; index++) { state.messages.push(message(index)); await view.refresh(); }
  assert.equal(old.history.messages.length, 1);
  assert.equal(view.getSnapshot().history.messages.at(-1).sequence, '40');
  assert.ok(notifications <= 3);
  const listeners = Array.from({ length: 64 }, () => view.subscribe(() => {}));
  assert.throws(() => view.subscribe(() => {}), /listener limit/);
  for (const remove of listeners) remove();
});

test('activity observes other clients durable work without hydrating queue payloads or guessing from previews', async t => {
  const { state, client } = await backend(); const view = viewFor(t, client); await view.start();
  state.activity = { ...state.activity, queued_input_count: '9007199254740993', pending_question_count: '1', active_turn: { ...fixture('Turn'), session_id: owner, state: 'running', finished_at: null }, execution_permit: false };
  await view.refresh();
  assert.equal(view.getSnapshot().preview, null);
  assert.equal(view.getSnapshot().activity.queued_input_count, '9007199254740993');
  assert.equal(view.getSnapshot().activity.active_turn.state, 'running');
  assert.equal(view.getSnapshot().activity.execution_permit, false);
  assert.ok(state.calls.every(call => !call.method.startsWith('inputs.')));
  await view.suspend(); assert.equal(view.getSnapshot().activity, null);
  state.activity = { ...state.activity, active_turn: null, active_workspace_action_id: 'restore', queued_input_count: '0' };
  await view.resume(); assert.equal(view.getSnapshot().activity.active_workspace_action_id, 'restore');
  state.intercept = () => { throw new Error('disconnected'); }; await view.refresh();
  assert.equal(view.getSnapshot().status, 'stale'); assert.equal(view.getSnapshot().activity, null);
});

test('catalog search stays in every bounded host request without session hydration', async t => {
  const { state, client } = await backend(); state.trees = trees(6);
  const view = createTreeCatalogView(client, { search: '100%_Ready', pageSize: 2, pollIntervalMs: 60_000 });
  t.after(() => view.dispose());
  await view.start(); await view.loadMore();
  const pages = state.calls.filter(call => call.method === 'trees.list');
  assert.equal(pages.length, 2);
  assert.ok(pages.every(call => call.params.search === '100%_Ready'));
  assert.equal(pages[1].params.expected_revision, view.getSnapshot().revision);
  assert.ok(state.calls.every(call => call.method.startsWith('trees.')));
});
