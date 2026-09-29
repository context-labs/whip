import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client } from '../dist/index.js';
import { createExecutionView, createSessionView, cellExecutionRows } from '../dist/state.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const owner = 'session_child';
const turn = id => ({ ...fixture('Turn'), id, session_id: owner, kind: 'prompt', state: 'succeeded', finished_at: '2026-09-27T12:00:01Z' });
const cell = (id, turnID = 'turn') => ({ ...fixture('Cell'), id, turn_id: turnID, session_id: owner });
const operation = (id, turnID = 'turn') => ({ ...fixture('HostOperation'), id, turn_id: turnID, session_id: owner });
const messages = () => fixture('HistoryResult').items.filter(value => value.id === 'message_call' || value.id === 'message_result').map(value => ({ ...value, turn_id: 'turn', group_id: 'turn', sequence: value.id === 'message_call' ? '9007199254740993' : '9007199254740994' }));

async function backend() {
  const state = { calls: [], turns: [turn('turn')], cells: [], operations: [], messages: [], revision: '1', epoch: 'boot', intercept: undefined, shortPages: false, output: null, activity: { ...fixture('SessionActivity'), active_turn: null, active_input_id: null } };
  const snapshot = () => ({ session_id: owner, revision: state.revision, through_sequence: state.messages.at(-1)?.sequence ?? '0', message_count: String(state.messages.length) });
  const connect = () => Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: state.epoch, network_client: false, builtins: [] } };
    state.calls.push(request); await state.intercept?.(request);
    const p = request.params;
    if ((request.method === 'sessions.history_page' || request.method === 'sessions.observe') && p.expected_revision && p.expected_revision !== state.revision) return { jsonrpc: '2.0', id: request.id, error: { code: -32004, kind: 'CONFLICT', message: 'rewound' } };
    let result;
    if (request.method === 'cells.output') result = { epoch: state.epoch, preview: state.output };
    else if (request.method === 'sessions.activity') result = state.activity;
    else if (request.method === 'sessions.history_page') result = { snapshot: snapshot(), messages: state.messages.slice(-p.limit), next_cursor: null };
    else if (request.method === 'sessions.observe') result = { snapshot: snapshot(), messages: state.messages.filter(value => BigInt(value.sequence) > BigInt(p.after)).slice(0, p.limit), epoch: state.epoch, preview: null };
    else if (request.method === 'context.read') {
      const c = state.cells.find(cell => cell.call_message_id === p.message_id || cell.result_message_id === p.message_id);
      const original = state.messages.find(message => message.id === p.message_id) ?? messages().find(message => message.id === p.message_id);
      if (!original || !c) return { jsonrpc: '2.0', id: request.id, error: { code: -32004, kind: 'NOT_FOUND', message: 'No retained body' } };
      const message = { ...original, turn_id: c.turn_id, group_id: c.turn_id };
      const data = Buffer.from(JSON.stringify(message.parts)); const offset = Number(p.offset), end = Math.min(data.length, offset + p.length);
      const { parts, ...metadata } = message;
      result = { message: { ...metadata, parts_bytes: String(data.length) }, offset: p.offset, next_offset: end < data.length ? String(end) : null, data_base64: data.subarray(offset, end).toString('base64') };
    } else if (request.method === 'turns.cells_page') {
      const all = state.cells.filter(cell => cell.turn_id === p.turn_id).slice().reverse();
      const offset = p.before ? all.findIndex(cell => cell.id === p.before) + 1 : 0;
      const items = all.slice(offset, offset + (state.shortPages ? 1 : p.limit));
      result = { items, next_cursor: offset + items.length < all.length ? items.at(-1).id : null };
    } else if (request.method === 'sessions.turns') {
      const offset = p.before ? state.turns.findIndex(turn => turn.id === p.before) + 1 : 0;
      const items = state.turns.slice(offset, offset + p.limit);
      result = { items, next_cursor: offset + items.length < state.turns.length ? items.at(-1)?.id ?? null : null };
    } else if (request.method === 'turns.get') result = state.turns.find(turn => turn.id === p.turn_id);
    else if (request.method === 'turns.cells' || request.method === 'turns.operations') {
      const all = request.method === 'turns.cells' ? state.cells : state.operations;
      result = { items: all.filter(value => value.turn_id === p.turn_id && value.id > (p.after ?? '')).slice(0, state.shortPages ? 1 : p.limit) };
    } else throw new Error(request.method);
    return { jsonrpc: '2.0', id: request.id, result: structuredClone(result) };
  }, { clientID: 'test' });
  return { state, connect, client: await connect() };
}
async function views(t, client, options) {
  const session = client.session(owner);
  const source = createSessionView(session, { pollIntervalMs: 60_000 });
  await source.start();
  const view = createExecutionView(session, source, { pollIntervalMs: 60_000, ...options });
  t.after(async () => { await view.dispose(); await source.dispose(); });
  return { source, view };
}

test('execution view reads bounded recent canonical metadata including direct work with no transcript', async t => {
  const { state, client } = await backend();
  state.turns = Array.from({ length: 5000 }, (_, index) => ({ ...turn('turn_' + index), kind: 'host_operation' }));
  state.operations = [{ ...operation('direct', 'turn_0'), origin: 'host_operation', cell_id: null, state: 'waiting', result: null, finished_at: null, dispatched_at: null }];
  const { view } = await views(t, client, { maxTurns: 3 });
  await view.start();
  const value = view.getSnapshot();
  assert.equal(value.turns.length, 3); assert.equal(value.cells.length, 0); assert.equal(value.operations[0].origin, 'host_operation');
  assert.equal(value.operations[0].state, 'waiting'); assert.equal(value.olderCursor, 'turn_2'); assert.equal(value.truncated, true);
  assert.ok(value.turns.every(turn => turn.kind === 'host_operation'));
  assert.ok(state.calls.every(call => !call.method.startsWith('inputs.') && call.method !== 'trace.page'));
  await view.loadOlder(); assert.deepEqual(view.getSnapshot().turns.map(turn => turn.id), ['turn_3', 'turn_4', 'turn_5']);
  assert.equal(view.getSnapshot().latestMissing, true);
  await view.focus('turn_40'); assert.equal(view.getSnapshot().turns[0].id, 'turn_40');
  await view.latest(); assert.equal(view.getSnapshot().turns[0].id, 'turn_0');
});

test('cell rows join only exact local owner, turn, message and call identities', async t => {
  const { state, client } = await backend();
  state.messages = messages(); state.cells = [cell('cell_fixture')];
  state.operations = [operation('a'), { ...operation('b'), cell_id: 'another_cell' }, { ...operation('c'), cell_id: null, origin: 'host_operation' }];
  const { view, source } = await views(t, client); await view.start();
  const row = cellExecutionRows(view.getSnapshot(), source.getSnapshot().history.messages)[0];
  assert.equal(row.call.value.id, 'call_fixture'); assert.equal(row.call.message.sequence, '9007199254740993');
  assert.equal(row.result.value.output, '1'); assert.deepEqual(row.operations.map(value => value.id), ['a']);
  const evidence = source.getSnapshot().history.messages;
  const metadataOnly = { ...view.getSnapshot(), messages: [] };
  assert.equal(cellExecutionRows(metadataOnly, evidence.map(value => ({ ...value, turn_id: null })))[0].call, null);
  assert.equal(cellExecutionRows(metadataOnly, evidence.map(value => ({ ...value, session_id: 'foreign' })))[0].result, null);
  assert.equal(cellExecutionRows(metadataOnly, evidence.map(value => ({ ...value, turn_id: 'another_turn' })))[0].call, null);
  assert.equal(cellExecutionRows(metadataOnly, evidence.map(value => ({ ...value, retired_by: 'rewind', retired_revision: '2' })))[0].result, null);
  assert.equal(cellExecutionRows(metadataOnly, evidence.map(value => ({ ...value, parts: [{ type: 'tool_call', call: { id: 'same_name_not_same_call', name: 'execute', arguments: {} } }] })))[0].call, null);
  assert.equal(row.call.message, evidence[0]); // The pure projection references its bounded source; no copied transcript owner.
});

test('cell operation presentation follows exact recorded time, independent of opaque page IDs', async t => {
  const { state, client } = await backend();
  state.messages = messages(); state.cells = [cell('cell_fixture')];
  const at = (id, suffix, capability = 'files.read') => ({ ...operation(id), capability, created_at: `2500-01-01T00:00:${suffix}` });
  // These epoch nanoseconds exceed 2^53. All three fractional times occupy the
  // same millisecond, and a whole-second timestamp must still precede them.
  state.operations = [at('a-spawn', '00.000000003Z', 'agents.spawn'), at('b-later', '01Z'),
    at('c-read-2', '00.000000002Z'), at('d-whole', '00Z'), at('e-read-1', '00.000000001Z'),
    at('f-tied', '00.000000003Z'), { ...at('g-foreign', '00Z'), cell_id: 'another' }];
  state.shortPages = true;
  const { view, source } = await views(t, client); await view.start();
  const snapshot = view.getSnapshot(), rows = cellExecutionRows(snapshot, source.getSnapshot().history.messages);
  assert.deepEqual(rows[0].operations.map(value => value.id), ['d-whole', 'e-read-1', 'c-read-2', 'a-spawn', 'f-tied', 'b-later']);
  assert.deepEqual(snapshot.operations.map(value => value.id), state.operations.map(value => value.id));
  assert.deepEqual(state.calls.filter(call => call.method === 'turns.operations').map(call => call.params.after),
    [undefined, 'a-spawn', 'b-later', 'c-read-2', 'd-whole', 'e-read-1', 'f-tied', 'g-foreign']);
  assert.equal(rows[0].call.message.sequence, '9007199254740993');
  assert.equal(rows[0].operations[3], snapshot.operations[0]);
});

test('loaded older local turns are fetched without a transcript crawl and copied turns do not trigger reads', async t => {
  const { state, client } = await backend();
  state.turns = [turn('new'), turn('older')];
  state.messages = messages().map(message => ({ ...message, turn_id: 'older' }));
  state.cells = [cell('cell', 'older')];
  const { view } = await views(t, client, { maxTurns: 1 }); await view.start();
  assert.equal(view.getSnapshot().turns[0].id, 'older'); assert.equal(view.getSnapshot().cells[0].turn_id, 'older');
  assert.equal(state.calls.filter(call => call.method === 'turns.get').length, 1);
  assert.equal(state.calls.filter(call => call.method === 'sessions.history_page').length, 1);
});

test('execution count, byte and short-page bounds are explicit without skipping opaque IDs', async t => {
  const { state, client } = await backend();
  state.cells = Array.from({ length: 9 }, (_, index) => cell('cell_' + index));
  state.operations = Array.from({ length: 9 }, (_, index) => operation('op_' + index));
  state.shortPages = true;
  const { view } = await views(t, client, { maxCells: 3, maxOperations: 4 }); await view.start();
  assert.equal(view.getSnapshot().cells.length, 3); assert.equal(view.getSnapshot().operations.length, 4);
  assert.equal(view.getSnapshot().truncated, true);
  assert.deepEqual(state.calls.filter(call => call.method === 'turns.cells_page').map(call => call.params.before), [undefined, 'cell_8', 'cell_7']);
  const small = createExecutionView(client.session(owner), (await views(t, client)).source, { maxBytes: 4096, pollIntervalMs: 60_000 }); t.after(() => small.dispose());
  state.operations = [{ ...operation('huge'), arguments: { input: '界'.repeat(20_000) } }];
  await small.start(); const value = small.getSnapshot();
  assert.equal(value.operations.length, 0); assert.equal(value.truncated, true); assert.ok(value.retainedBytes <= 4096);
  assert.equal(value.retainedBytes, Buffer.byteLength(JSON.stringify(value))); assert.ok(Object.isFrozen(value.cells));
});

test('foreign execution pages and mismatched exact turn IDs fail closed before publishing', async t => {
  const { state, client } = await backend();
  state.cells = [{ ...cell('cell'), session_id: 'foreign' }];
  const { view } = await views(t, client); await view.start();
  assert.equal(view.getSnapshot().status, 'stale'); assert.equal(view.getSnapshot().unavailable, true); assert.equal(view.getSnapshot().cells.length, 0);
  state.cells = []; state.intercept = request => { if (request.method === 'turns.get') request.params.turn_id = 'turn'; };
  await view.focus('wrong'); assert.match(view.getSnapshot().error.message, /another session or identity/);
});

test('revision changes abort and discard pending execution evidence before reading new history', async t => {
  const { state, client } = await backend(); state.messages = messages(); state.cells = [cell('cell')];
  const { view, source } = await views(t, client); await view.start();
  let entered, release;
  const started = new Promise(resolve => { entered = resolve; }), held = new Promise(resolve => { release = resolve; });
  state.intercept = async request => { if (request.method === 'turns.cells_page') { entered(); await held; } };
  const pending = view.refresh(); await started;
  state.revision = '9007199254740993'; state.messages = []; await source.refresh();
  assert.equal(view.getSnapshot().cells.length, 0); assert.equal(view.getSnapshot().historyRevision, state.revision);
  state.cells = []; release(); await pending;
  assert.equal(view.getSnapshot().cells.length, 0);
  state.intercept = undefined; await view.refresh(); assert.equal(view.getSnapshot().status, 'live');
});

test('process restart requires explicit same-runtime reconnect and never resumes an old effect', async t => {
  const { state, client, connect } = await backend(); state.cells = [cell('cell')];
  const { view, source } = await views(t, client); await view.start();
  state.epoch = 'new_boot'; await source.refresh();
  assert.equal(view.getSnapshot().cells.length, 0);
  const calls = state.calls.length; await view.refresh();
  assert.equal(state.calls.length, calls); assert.match(view.getSnapshot().error.message, /process epoch/);
  const replacement = await connect(); await source.reconnect(replacement); await view.reconnect(replacement);
  assert.equal(view.getSnapshot().epoch, 'new_boot'); assert.equal(view.getSnapshot().status, 'live');
  assert.ok(state.calls.every(call => ['sessions.activity', 'sessions.history_page', 'sessions.observe', 'sessions.turns', 'turns.get', 'turns.cells', 'turns.cells_page', 'turns.operations', 'context.read'].includes(call.method)));
});

test('slow readers share one fetch; suspend joins it without cancelling runtime work', async t => {
  const { state, client } = await backend(); const { view } = await views(t, client);
  let entered, release;
  const started = new Promise(resolve => { entered = resolve; }), held = new Promise(resolve => { release = resolve; });
  state.intercept = async request => { if (request.method === 'sessions.turns') { entered(); await held; } };
  const unsubscribe = view.subscribe(() => { void view.refresh(); });
  const pending = view.start(); await started;
  const reads = Array.from({ length: 100 }, () => view.refresh());
  assert.equal(state.calls.filter(call => call.method === 'sessions.turns').length, 1);
  let suspended = false; const stopped = view.suspend().then(() => { suspended = true; });
  await Promise.resolve(); assert.equal(suspended, false); release(); await Promise.all([pending, stopped, ...reads]);
  assert.equal(view.getSnapshot().status, 'suspended'); assert.equal(view.getSnapshot().turns.length, 0);
  unsubscribe(); state.intercept = undefined; await view.resume(); assert.equal(view.getSnapshot().status, 'live');
  const listeners = Array.from({ length: 64 }, () => view.subscribe(() => {}));
  assert.throws(() => view.subscribe(() => {}), /listener limit/); for (const remove of listeners) remove();
});


function liveOutput(state) {
  state.turns = [{ ...turn('turn'), state: 'running', finished_at: null }];
  state.activity = { ...state.activity, active_turn: state.turns[0], active_input_id: 'input_active' };
  state.cells = [{ ...cell('cell'), state: 'running', result_message_id: null, checkpoint: null, finished_at: null }];
  state.output = { session_id: owner, turn_id: 'turn', cell_id: 'cell', call_message_id: state.cells[0].call_message_id, call_id: state.cells[0].call_id, history_revision: '1', revision: '9007199254740993', text: 'still working\n', truncated: false };
}

test('live stdout replaces one bounded exact-cell preview and disappears at settlement or detach', async t => {
  const { state, client } = await backend(); liveOutput(state);
  const { view, source } = await views(t, client); await view.start();
  assert.equal(view.getSnapshot().status, 'live', source.getSnapshot().error?.message);
  assert.equal(view.getSnapshot().output.text, 'still working\n');
  assert.equal(cellExecutionRows(view.getSnapshot(), [])[0].output.revision, '9007199254740993');
  state.output.text = 'still working\nnew line\n'; await view.refresh();
  assert.equal(view.getSnapshot().output.text, state.output.text);
  assert.equal(state.calls.filter(call => call.method === 'cells.output').length, 2);
  await view.suspend(); assert.equal(view.getSnapshot().output, null);
  await view.start(); assert.equal(view.getSnapshot().output.text, state.output.text);
  state.cells[0] = { ...state.cells[0], state: 'succeeded', finished_at: '2026-09-29T00:00:00Z' }; state.output = null;
  await view.refresh(); assert.equal(view.getSnapshot().output, null);
  assert.equal(cellExecutionRows(view.getSnapshot(), source.getSnapshot().history.messages)[0].output, null);
});

test('stdout ignores retired history and other cells and respects the execution byte budget', async t => {
  const { state, client } = await backend(); liveOutput(state);
  const { view, source } = await views(t, client, { maxBytes: 4096 });
  state.output.text = 'x'.repeat(5000); await view.start();
  assert.equal(view.getSnapshot().output, null); assert.equal(view.getSnapshot().truncated, true);
  assert.ok(view.getSnapshot().retainedBytes <= 4096);
  state.output.text = 'small'; state.output.history_revision = '2'; await view.refresh(); assert.equal(view.getSnapshot().output, null);
  state.output.history_revision = '1'; state.output.cell_id = 'other'; await view.refresh(); assert.equal(view.getSnapshot().output, null);
  state.output.cell_id = 'cell'; await view.refresh(); assert.equal(view.getSnapshot().output.text, 'small');
  state.activity.active_turn = null; state.activity.active_input_id = null; await source.refresh(); assert.equal(view.getSnapshot().output, null);
});

test('stdout rejects foreign owners, process epochs and oversized multibyte payloads', async () => {
  const { state, client } = await backend(); liveOutput(state);
  state.output.session_id = 'foreign'; await assert.rejects(client.session(owner).cells.output(), /another session/);
  state.output.session_id = owner; state.output.text = '界'.repeat(30000); await assert.rejects(client.session(owner).cells.output(), /byte limit/);
  state.output.text = 'valid'; state.epoch = 'new-boot'; await assert.rejects(client.session(owner).cells.output(), /process/);
});

test('late stdout reads cannot republish after observation suspend', { timeout: 5000 }, async t => {
  const { state, client } = await backend(); liveOutput(state);
  const { view } = await views(t, client); await view.start();
  let release, entered; const held = new Promise(resolve => { release = resolve; }); const started = new Promise(resolve => { entered = resolve; });
  state.intercept = async request => { if (request.method === 'cells.output') { entered(); await held; } };
  const refresh = view.refresh(); await started;
  const stopping = view.suspend(); assert.equal(view.getSnapshot().output, null);
  release(); await Promise.all([refresh, stopping]); assert.equal(view.getSnapshot().output, null);
  assert.equal(view.getSnapshot().status, 'suspended');
});

test('ordinal cell windows pass 128 cells and sixteen turns without displacing the selected older page', async t => {
  const { state, client } = await backend();
  state.turns = Array.from({ length: 20 }, (_, i) => turn('turn_' + i));
  state.cells = Array.from({ length: 150 }, (_, i) => cell('opaque_' + (500 - i), 'turn_0'));
  const { view, source } = await views(t, client); await view.start();
  assert.equal(view.getSnapshot().cells.length, 128);
  assert.equal(view.getSnapshot().olderCellCursor.before, 'opaque_478');
  const initialIDs = new Set(view.getSnapshot().cells.map(c => c.id));
  await view.loadOlder();
  assert.equal(view.getSnapshot().cells.length, 22);
  assert.ok(view.getSnapshot().cells.every(c => !initialIDs.has(c.id)));
  const olderIDs = view.getSnapshot().cells.map(c => c.id);
  state.cells.push(cell('new_late_cell', 'turn_0')); await source.refresh(); await view.refresh();
  assert.deepEqual(view.getSnapshot().cells.map(c => c.id), olderIDs);
  await view.loadOlder(); assert.equal(view.getSnapshot().turns[0].id, 'turn_16');
  assert.equal(view.getSnapshot().turns.at(-1).id, 'turn_19');
  await view.latest(); assert.equal(view.getSnapshot().cells[0].id, 'new_late_cell');
});

test('exact execution bodies deduplicate within the retained window and release on revision and disposal', async t => {
  const { state, client } = await backend(); state.cells = [cell('cell')];
  const { view, source } = await views(t, client); await view.start();
  const value = view.getSnapshot(); assert.equal(value.messages.length, 2);
  assert.equal(cellExecutionRows(value, [])[0].call.value.arguments.code, 'print(1)');
  const count = () => state.calls.filter(c => c.method === 'context.read').length;
  assert.equal(count(), 2); await view.refresh(); assert.equal(count(), 2);
  assert.ok(view.getSnapshot().retainedBytes <= 4 << 20);
  state.revision = '2'; state.cells = []; await source.refresh(); await view.refresh();
  assert.deepEqual(view.getSnapshot().messages, []);
  await view.dispose(); assert.deepEqual(view.getSnapshot().messages, []);
});

test('late exact body reads cannot publish after the observation lease is released', async t => {
  const { state, client } = await backend(); state.cells = [cell('cell')];
  const { view } = await views(t, client);
  let entered, release; const started = new Promise(r => entered = r), held = new Promise(r => release = r);
  state.intercept = async request => { if (request.method === 'context.read') { entered(); await held; } };
  const pending = view.start(); await started; const stopped = view.suspend(); release(); await Promise.all([pending, stopped]);
  assert.equal(view.getSnapshot().status, 'suspended'); assert.deepEqual(view.getSnapshot().messages, []);
});

test('independent transcript eviction cannot remove retained execution bodies before the next refresh', async t => {
  const { state, client } = await backend(); state.messages = messages(); state.cells = [cell('cell')];
  const { view, source } = await views(t, client); await view.start();
  const snapshot = view.getSnapshot(), original = source.getSnapshot().history.messages;
  assert.equal(snapshot.messages.length, 2); assert.equal(snapshot.messages[0], original[0]);
  assert.equal(state.calls.filter(c => c.method === 'context.read').length, 0);
  state.messages = [{ ...original[0], id: 'later_text', sequence: '9007199254740995', parts: [{ type: 'text', text: 'Later body-only history window' }] }];
  await source.latest();
  const retained = cellExecutionRows(view.getSnapshot(), source.getSnapshot().history.messages)[0];
  assert.equal(retained.call.value.arguments.code, 'print(1)'); assert.equal(retained.result.value.output, '1');
  assert.equal(retained.call.message, original[0]); assert.equal(view.getSnapshot(), snapshot);
  assert.ok(snapshot.retainedBytes <= 4 << 20);
  await view.refresh(); assert.equal(state.calls.filter(c => c.method === 'context.read').length, 0);
});
