import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client } from '../dist/index.js';
import { executionCode, executionPresentationRows } from '../dist/state.js';

const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = type => structuredClone(fixtures.find(v => v.type === type && v.valid).value);
const owner = 'session_child';
const presentation = { version: 1, attempt_id: 'attempt', truncated: false, parts: [{ id: 'p0', type: 'tool_call', call_index: 0, call_id: 'call_fixture' }] };
const callMessage = () => ({ ...fixture('HistoryResult').items.find(m => m.id === 'message_call'), turn_id: 'turn', group_id: 'turn', presentation: structuredClone(presentation) });
const execution = () => ({ runtimeID: 'runtime', sessionID: owner, latestMissing: false, windowBefore: null, olderCellCursor: null, turns: [{ ...fixture('Turn'), id: 'turn' }], cells: [], messages: [], operations: [], output: null });
const source = () => ({ runtimeID: 'runtime', sessionID: owner, status: 'live', activity: { active_turn: { id: 'turn' } }, history: { messages: [], latestMissing: false }, attemptPresentations: [], preview: null });

test('attempt slot identity survives partial IDs, canonical commit before cells, hydration and result', () => {
  const x = execution(), s = source();
  s.preview = { ...fixture('SessionObservation').preview, attempt_id: 'attempt', turn_id: 'turn', message_id: 'message_call', presentation: { ...presentation, parts: [{ id: 'p0', type: 'tool_call', call_index: 0 }] }, calls: [{ index: 0, id: '', name: 'exe', arguments: '{"code":"print(1)' }] };
  let rows = executionPresentationRows(x, s); const id = rows[0].id;
  assert.equal(rows[0].code, 'print(1)'); assert.equal(rows[0].state, 'writing'); assert.equal(rows[0].cell, null); assert.equal(rows[0].startedAt, null);
  s.preview.calls[0].id = 'call_fixture'; s.preview.calls[0].name = 'execute';
  assert.equal(executionPresentationRows(x, s)[0].id, id);
  s.history.messages = [callMessage()];
  rows = executionPresentationRows(x, s); assert.equal(rows.length, 1); assert.equal(rows[0].id, id); assert.equal(rows[0].kind, 'call');
  x.cells = [{ ...fixture('Cell'), turn_id: 'turn', state: 'running' }]; x.messages = s.history.messages; s.history.messages = []; s.preview = null;
  rows = executionPresentationRows(x, s); assert.equal(rows.length, 1); assert.equal(rows[0].id, id); assert.equal(rows[0].kind, 'cell'); assert.equal(rows[0].startedAt, x.cells[0].created_at);
  const result = { ...fixture('HistoryResult').items.find(m => m.id === 'message_result'), turn_id: 'turn', group_id: 'turn' };
  x.messages.push(result); x.cells[0].state = 'succeeded';
  rows = executionPresentationRows(x, s); assert.equal(rows[0].id, id); assert.equal(rows[0].cell.result.value.output, '1');
});

test('truncated or absent presentation never loses unreferenced committed execute calls', () => {
  const x = execution(), s = source(), m = callMessage();
  m.presentation = { ...presentation, truncated: true, parts: [] }; s.history.messages = [m];
  let rows = executionPresentationRows(x, s); assert.equal(rows.length, 1); assert.equal(rows[0].kind, 'call'); assert.equal(rows[0].code, 'print(1)');
  delete m.presentation; rows = executionPresentationRows(x, s); assert.equal(rows.length, 1);
  x.cells = [{ ...fixture('Cell'), turn_id: 'turn' }]; rows = executionPresentationRows(x, s); assert.equal(rows.length, 1); assert.equal(rows[0].kind, 'cell');
});

test('failed and imported presentation carries evidence but no execution authority', () => {
  const x = execution(), s = source();
  const failed = { group_id: 'turn', attempt_id: 'failed', turn_id: 'turn', state: 'failed', presentation: { version: 1, attempt_id: 'failed', truncated: false, parts: [{ id: 'p0', type: 'tool_call', call_index: 0, call: { index: 0, id: '', name: 'execute', arguments: '{"code":"partial' } }] } };
  s.attemptPresentations = [failed]; let row = executionPresentationRows(x, s)[0];
  assert.equal(row.code, 'partial'); assert.equal(row.state, 'failed'); assert.equal(row.cell, null); assert.equal(row.startedAt, null);
  s.attemptPresentations = [{ ...failed, source_session_id: 'original', group_id: 'imported' }];
  row = executionPresentationRows(x, s)[0]; assert.equal(row.turnID, null); assert.equal(row.cell, null);
  const prior = row.id; x.sessionID = s.sessionID = 'fork'; assert.notEqual(executionPresentationRows(x, s)[0].id, prior);
  s.history.messages = [{ ...callMessage(), session_id: 'fork', turn_id: null, group_id: 'imported' }];
  assert.equal(executionPresentationRows(x, s).find(row => row.kind === 'imported').state, 'recorded');
});

test('partial execution parser handles escapes, only top-level code and bounded UTF-8', () => {
  assert.equal(executionCode('{"meta":{"code":"wrong"},"code":"line\\n\\u754c\\"ok'), 'line\n界"ok');
  assert.equal(executionCode('{"meta":{"code":"wrong"}'), '');
  assert.equal(executionCode('{"code":false}'), '');
  const code = executionCode('{"code":"' + '界'.repeat(100000));
  assert.ok(Buffer.byteLength(code) <= 65536); assert.ok(!code.includes('\ufffd'));
});

async function exactReader(message, mutate = () => {}) {
  const calls = [], data = Buffer.from(JSON.stringify(message.parts));
  const { parts, ...metadata } = message;
  const client = await Client.connect(async request => {
    if (request.method === 'initialize') return { jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: false, builtins: [] } };
    calls.push(request); const offset = Number(request.params.offset), end = Math.min(data.length, offset + request.params.length);
    const result = { message: { ...metadata, parts_bytes: String(data.length) }, offset: String(offset), next_offset: end < data.length ? String(end) : null, data_base64: data.subarray(offset, end).toString('base64') };
    mutate(result, calls.length);
    return { jsonrpc: '2.0', id: request.id, result };
  }, { clientID: 'test' });
  return { calls, read: options => client.session(owner).history.message(message.id, options) };
}
test('exact body reads preserve canonical metadata and enforce chunk identity and byte budgets', async () => {
  const message = callMessage(); message.parts[0].call.arguments.code = '界'.repeat(40000);
  const good = await exactReader(message); const read = await good.read({ sequence: message.sequence, turnID: 'turn', groupID: 'turn', role: 'assistant' });
  assert.deepEqual(read, message); assert.equal(good.calls.length, 2); assert.equal(good.calls[1].params.offset, '65536');
  for (const mutate of [
    r => { r.message.session_id = 'foreign'; }, r => { r.message.retired_by = 'rewind'; r.message.retired_revision = '2'; },
    (r, n) => { if (n === 2) r.message.created_at = '2026-09-28T00:00:00Z'; },
    r => { r.message.turn_id = 'other'; }, r => { r.message.group_id = 'other'; }, r => { r.message.sequence = '400'; },
    r => { r.offset = '1'; }, r => { r.next_offset = '1'; }, r => { r.message.created_at = undefined; },
  ]) {
    const changed = await exactReader(message, mutate);
    await assert.rejects(changed.read({ sequence: message.sequence, turnID: 'turn', groupID: 'turn' }));
  }
  const large = await exactReader(message); await assert.rejects(large.read({ maxBytes: 4096 }), /byte limit/); assert.equal(large.calls.length, 1);
});

test('latest canonical call survives turn metadata lag and imported result joins stay within one unbroken group', () => {
  const x = execution(), s = source(), message = callMessage(); x.turns = []; s.activity.active_turn = null; s.history.messages = [message];
  assert.equal(executionPresentationRows(x, s).length, 1);
  const result = { ...fixture('HistoryResult').items.find(m => m.id === 'message_result'), turn_id: null, group_id: 'imported' };
  message.turn_id = null; message.group_id = 'imported'; s.history.messages = [message, result];
  assert.equal(executionPresentationRows(x, s)[0].result.value.output, '1');
  result.group_id = 'other'; assert.equal(executionPresentationRows(x, s)[0].result, null);
  result.group_id = 'imported'; s.history.gaps = [{ messageID: 'gap', sequence: message.sequence }];
  result.sequence = String(BigInt(message.sequence) + 2n); s.history.gaps[0].sequence = String(BigInt(message.sequence) + 1n);
  assert.equal(executionPresentationRows(x, s)[0].result, null);
  s.history.gaps = []; s.history.messages.splice(1, 0, { ...message, id: 'reused', presentation: undefined, sequence: String(BigInt(message.sequence) + 1n) });
  const rows = executionPresentationRows(x, s); assert.equal(rows.find(row => row.messageID === message.id).result, null); assert.equal(rows.find(row => row.messageID === 'reused').result.value.output, '1');
});
