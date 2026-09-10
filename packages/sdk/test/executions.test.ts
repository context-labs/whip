import assert from 'node:assert/strict';
import test from 'node:test';
import type { RootSnapshot } from '@whip/protocol';
import { executionRows, type ExecutionCell, type HistoryView, type SessionViewSnapshot } from '../src/state.js';
import { emptyExecutionEvidence, executionCode, observeExecution, reconcileExecutions, seedExecutions, settleExecutions, type ExecutionEvidence } from '../src/executions.js';

const history = (messages: HistoryView['messages'] = []): Record<string, HistoryView> => ({ root: {
  revision: '1', throughSeq: messages.at(-1)?.seq ?? 0, nextSeq: messages[0]?.seq ?? 0,
  hasMore: false, loading: false, messages, truncated: false,
} });
const root = (overrides: Partial<RootSnapshot> = {}): RootSnapshot => ({
  root_id: 'root', history_revision: '1', cursor: '0', active_turns: {}, presentation: [], agent_presentations: {}, ...overrides,
} as RootSnapshot);
const state = (executions: ExecutionEvidence | undefined, histories = history(), currentRoot = root()): SessionViewSnapshot => ({
  status: 'live', root: currentRoot, history: histories, executions, collections: {}, retainedBytes: 0, truncated: false, unavailable: false,
});
const cells = (snapshot: SessionViewSnapshot, agent = 'root'): readonly ExecutionCell[] => executionRows(snapshot, agent).filter((row): row is ExecutionCell => row.kind === 'cell');
const call = (seq: number, id: string, code: string): HistoryView['messages'][number] => ({ seq,
  message: { role: 'assistant', content: '', tool_calls: [{ id, type: 'function', function: { name: 'rlm_exec', arguments: JSON.stringify({ code }) } }] },
});
const completed = (seq: number, id: string, content: string): HistoryView['messages'][number] => ({ seq,
  message: { role: 'tool', tool_call_id: id, name: 'rlm_exec', content },
});
const success = JSON.stringify({ value: 42, output: 'printed\n', steps: 7 });

test('result v2 completes either engine without invented metrics and preserves explicit null', () => {
  for (const execution_engine of ['starlark', 'quickjs']) {
    const language = execution_engine === 'quickjs' ? 'javascript' : 'starlark';
    for (const has_value of [true, false]) {
      const payload = { format_version: 2, execution_engine, language, has_value, value: null, output: 'printed' };
      const row = cells(state(undefined, history([call(1, 'a', 'null'), completed(2, 'a', JSON.stringify(payload))])))[0]!;
      assert.equal(row.status, 'completed');
      assert.equal(row.hasValue, has_value);
      assert.equal(row.value, has_value ? 'null' : undefined);
      assert.equal(row.executionEngine, execution_engine);
      assert.equal(row.language, language);
      assert.equal(row.steps, undefined);
      assert.equal(row.quickjsJobs, undefined);
    }
  }
});

test('result v2 carries QuickJS jobs and lossless numeric tags in live and recorded evidence', () => {
  const value = { integer: { type: 'bigint', value: '900719925474099312345' }, decimal: { type: 'number', value: '1.0000000000000001' } };
  const payload = JSON.stringify({ format_version: 2, execution_engine: 'quickjs', language: 'javascript', has_value: true, value, metrics: { quickjs_jobs: 4 } });
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec' });
  book.emit('stream.tool.completed', { id: 'a', name: 'rlm_exec', result: payload });
  for (const row of [cells(book.snapshot())[0]!, cells(state(undefined, history([call(1, 'a', '42n'), completed(2, 'a', payload)])))[0]!]) {
    assert.equal(row.status, 'completed');
    assert.deepEqual(JSON.parse(row.value!), value);
    assert.equal(row.quickjsJobs, 4);
    assert.equal(row.steps, undefined);
  }
});

test('unknown and malformed result versions do not masquerade as completed legacy results', () => {
  const valid = { format_version: 2, execution_engine: 'quickjs', language: 'javascript', has_value: true, value: 42, steps: 0 };
  for (const patch of [{ format_version: 3 }, { language: 'starlark' }, { has_value: 'yes' }, { execution_engine: 'unknown' }, { metrics: { quickjs_jobs: -1 } }]) {
    const row = cells(state(undefined, history([call(1, 'a', '42'), completed(2, 'a', JSON.stringify({ ...valid, ...patch }))])))[0]!;
    assert.equal(row.status, 'unknown');
  }
});

test('session language supplies unfinished root and descendant cells independently across sessions', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec', args: '{"code":"await files.read({path: \'a\'})"}' });
  book.emit('stream.tool.started', { id: 'b', agent_id: 'child', name: 'rlm_exec', args: '{"code":"42"}' });
  const javascript = book.snapshot();
  javascript.root = root({ meta: { execution_engine: 'quickjs' } as RootSnapshot['meta'] });
  assert.equal(cells(javascript)[0]!.language, 'javascript');
  assert.equal(cells(javascript, 'child')[0]!.language, 'javascript');
  assert.equal(cells(book.snapshot())[0]!.language, 'starlark');
  javascript.root = root({ meta: { execution_engine: 'unsupported' } as RootSnapshot['meta'] });
  assert.equal(cells(javascript)[0]!.language, undefined);
});

class Notebook {
  evidence = emptyExecutionEvidence('root', '1');
  seq = 0n;
  active: Record<string, string> = { root: 'turn-1', child: 'child-turn' };
  histories = history();
  emit(kind: string, payload: unknown, time: number | undefined = 100): void {
    this.evidence = observeExecution(this.evidence, { seq: String(++this.seq), kind, payload }, this.active, this.histories, time);
  }
  snapshot() { return state(this.evidence, this.histories); }
}

test('host starts update in place, repeated invocations and caught failures stay distinct', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'same', name: 'rlm_exec', turn_id: 'turn-1' });
  const scope = { id: 'same', turn_id: 'turn-1', name: 'files.read' };
  book.emit('stream.cell.host.started', { ...scope, invocation_id: '1:1', args: 'path=one' });
  const start = cells(book.snapshot())[0]!.hosts[0]!;
  assert.equal(start.status, 'running');
  assert.equal(start.duration, '');
  book.emit('stream.cell.host', { ...scope, invocation_id: '1:1', text: '3ms', result: 'missing' });
  book.emit('stream.cell.host.started', { ...scope, invocation_id: '1:2', args: 'path=two' });
  // A duplicate completion or delayed start must not clear the newer operation.
  book.emit('stream.cell.host', { ...scope, invocation_id: '1:1', text: '3ms', result: 'missing' });
  book.emit('stream.cell.host.started', { ...scope, invocation_id: '1:1' });
  let cell = cells(book.snapshot())[0]!;
  assert.equal(cell.hosts.length, 2);
  assert.equal(cell.hosts[0]!.id, start.id);
  assert.deepEqual(cell.hosts.map(host => host.status), ['failed', 'running']);
  assert.equal(cell.status, 'running', 'a caught host error is not a failed cell');
  book.emit('stream.cell.host', { ...scope, invocation_id: '1:2', text: '2ms' });
  book.emit('stream.tool.completed', { id: 'same', name: 'rlm_exec', result: success });
  cell = cells(book.snapshot())[0]!;
  assert.equal(cell.status, 'completed');
  assert.deepEqual(cell.hosts.map(host => host.status), ['failed', 'completed']);
});

test('late host completions are scoped to their turn and occurrence, even with reused tool IDs', () => {
  const book = new Notebook();
  const start = (invocation: string) => {
    book.emit('stream.tool.started', { id: 'same', name: 'rlm_exec' });
    book.emit('stream.cell.host.started', { id: 'same', name: 'shell.run', invocation_id: invocation, turn_id: book.active.root });
  };
  start('1:1');
  book.emit('stream.tool.completed', { id: 'same', name: 'rlm_exec', result: success });
  start('2:1');
  book.emit('stream.cell.host', { id: 'same', name: 'shell.run', invocation_id: '1:1', turn_id: 'turn-1', text: '1ms' });
  assert.deepEqual(cells(book.snapshot()).map(cell => cell.hosts[0]!.status), ['completed', 'running']);
  book.emit('turn.cancelled', { turn_id: 'turn-1' });
  book.active.root = 'turn-2';
  start('3:1');
  book.emit('stream.cell.host', { id: 'same', name: 'shell.run', invocation_id: '2:1', turn_id: 'turn-1', text: '2ms' });
  assert.equal(cells(book.snapshot()).at(-1)!.hosts[0]!.status, 'running');
  assert.equal(cells(book.snapshot()).at(-1)!.turnId, 'turn-2');
});

test('reconnect cannot attach a reused tool ID with a missing prefix to an uncertain prior cell', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'same', name: 'rlm_exec', args: '{"code":"old_code()"}' });
  book.emit('stream.cell.host.started', { id: 'same', name: 'files.read', invocation_id: '1:1', turn_id: 'turn-1' });
  const before = cells(book.snapshot())[0]!;
  book.evidence = settleExecutions(book.evidence);
  book.evidence = seedExecutions(book.evidence, root({ cursor: '4', active_turns: book.active,
    presentation: [{ seq: '4', kind: 'stream.cell.host.started', payload: {
      id: 'same', name: 'agents.wait', invocation_id: '2:1', turn_id: 'turn-1',
    } }],
  }), book.histories);
  const after = cells(book.snapshot());
  assert.equal(after.length, 1);
  assert.equal(after[0]!.id, before.id);
  assert.equal(after[0]!.status, 'unknown');
  assert.equal(after[0]!.code, 'old_code()');
  assert.deepEqual(after[0]!.hosts.map(host => host.invocationId), ['1:1']);
  assert.equal(book.evidence.truncated, true);
});

test('host activity remains bounded without dropping the current invocation', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec' });
  for (let n = 0; n < 140; n++) {
    book.emit('stream.cell.host.started', { id: 'a', name: 'files.read', invocation_id: `1:${n}` });
    if (n < 139) book.emit('stream.cell.host', { id: 'a', name: 'files.read', invocation_id: `1:${n}`, text: '1ms' });
  }
  const cell = cells(book.snapshot())[0]!;
  assert.equal(cell.hosts.length, 128);
  assert.equal(cell.truncated, true);
  assert.equal(cell.hosts.at(-1)!.status, 'running');
  book.emit('turn.interrupted', { turn_id: 'turn-1' });
  assert.equal(cells(book.snapshot())[0]!.hosts.at(-1)!.status, 'interrupted');
});

test('snapshot reconstructs active host starts without inventing client timing', () => {
  const snapshot = root({ cursor: '2', active_turns: { root: 'turn-1' }, presentation: [
    { seq: '1', kind: 'stream.tool.started', payload: { id: 'a', name: 'rlm_exec', turn_id: 'turn-1' } },
    { seq: '2', kind: 'stream.cell.host.started', payload: { id: 'a', name: 'agents.wait', turn_id: 'turn-1', invocation_id: '1:1' } },
  ] });
  const evidence = seedExecutions(undefined, snapshot, history());
  const cell = cells(state(evidence))[0]!;
  assert.equal(cell.hosts[0]!.status, 'running');
  assert.equal(cell.observedStartedAt, undefined);
  const stale = cells(state(settleExecutions(evidence)))[0]!;
  assert.equal(stale.hosts[0]!.status, 'unknown');
  const recovered = cells(state(seedExecutions(settleExecutions(evidence), snapshot, history())))[0]!;
  assert.equal(recovered.id, cell.id);
  assert.equal(recovered.status, 'running');
  assert.equal(recovered.hosts[0]!.status, 'running');
  assert.equal(recovered.observedStartedAt, undefined);
});

test('an evicted host completion cannot contaminate a newer cell reusing the tool ID', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec' });
  for (let n = 0; n < 130; n++) {
    book.emit('stream.cell.host.started', { id: 'a', name: 'files.read', invocation_id: `1:${n}` });
    book.emit('stream.cell.host', { id: 'a', name: 'files.read', invocation_id: `1:${n}`, text: '1ms' });
  }
  book.emit('stream.tool.completed', { id: 'a', name: 'rlm_exec', result: success });
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec' });
  book.emit('stream.cell.host', { id: 'a', name: 'files.read', invocation_id: '1:0', result: 'old error' });
  assert.equal(cells(book.snapshot()).at(-1)!.hosts.length, 0);
});

test('a cancelled host stays cancelled after the turn settles', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec' });
  book.emit('stream.cell.host.started', { id: 'a', name: 'agents.wait', invocation_id: '1:1' });
  book.emit('stream.cell.host', { id: 'a', name: 'agents.wait', invocation_id: '1:1', host_status: 'cancelled', result: 'context canceled' });
  book.emit('turn.cancelled', { turn_id: 'turn-1' });
  assert.equal(cells(book.snapshot())[0]!.hosts[0]!.status, 'cancelled');
});

test('partial arguments decode escaped prefixes, top-level keys and bounded Starlark', () => {
  assert.equal(executionCode('{"code":"print('), 'print(');
  assert.equal(executionCode('{"code":"x = \\"test\\"\\nprint(x)\\t\\u263a\\uD83D\\uDE80"}'), 'x = "test"\nprint(x)\t☺🚀');
  assert.equal(executionCode('{"code":"a\\u26'), 'a');
  assert.equal(executionCode('{"code":"a\\'), 'a');
  assert.equal(executionCode('{"nested":{"code":"wrong"},"x":["code",1],"code":"right"}'), 'right');
  assert.equal(executionCode('{"note":"a \\"code\\" string","code":"actual"}'), 'actual');
  assert.equal(executionCode('{"nested":{"code":"wrong"}}'), '');
  assert.equal(executionCode('{"code":123}'), '');
  assert.equal(executionCode('not JSON'), '');
  assert.ok(new TextEncoder().encode(executionCode(JSON.stringify({ code: '👋'.repeat(100_000) }))).length <= 64 * 1024 + 3);
});

test('arguments and print output are cumulative, agents are separate and host events retain occurrence identity', () => {
  const book = new Notebook();
  book.emit('stream.tool.call', { id: 'a', name: 'rlm_exec', args: '{"code":"print(' });
  book.emit('stream.tool.call', { id: 'b', name: 'rlm_exec', args: '{"code":"other"}' });
  book.emit('stream.tool.call', { id: 'a', name: 'rlm_exec', args: '{"code":"print(42)"}' });
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec' }, 200);
  book.emit('stream.tool.output', { id: 'a', text: 'one' });
  const first = cells(book.snapshot())[0]!;
  book.emit('stream.tool.output', { id: 'a', text: 'one\ntwo' });
  book.emit('stream.tool.started', { id: 'a', agent_id: 'child', name: 'rlm_exec', args: '{"code":"child"}' });
  for (let n = 0; n < 2; n++) book.emit('stream.cell.host', { id: 'a', name: 'fs.read', args: '/file', text: '2ms' });
  const row = cells(book.snapshot())[0]!;
  assert.equal(row.id, first.id);
  assert.equal(row.code, 'print(42)');
  assert.equal(row.output, 'one\ntwo');
  assert.equal(first.output, 'one');
  assert.equal(row.status, 'running');
  assert.equal(row.observedStartedAt, 200);
  assert.equal(row.hosts.length, 2);
  assert.notEqual(row.hosts[0]!.id, row.hosts[1]!.id);
  assert.equal(cells(book.snapshot(), 'child')[0]!.code, 'child');
  assert.equal(cells(book.snapshot()).length, 2);
});

test('recorded executions pair repeated IDs by occurrence, decode results honestly and retain scoped references', () => {
  const histories = history([
    call(1, 'same', 'first'), completed(2, 'same', success),
    call(3, 'same', 'second'), completed(4, 'same', 'Error: missing file'),
    call(5, 'invalid', 'third'), completed(6, 'invalid', '{"output":"partial"}'),
    completed(7, 'orphan', JSON.stringify({ value: null, steps: 0, restored: { restored: ['a', 'b'], failed: [{ name: 'c', reason: 'missing' }] } })),
  ]);
  histories.root!.messages[3]!.body = { reference_id: 'ref', digest: 'digest', size: '9007199254740993', media_type: 'application/json', source: 'history' };
  const rows = executionRows(state(undefined, histories), 'root');
  const values = cells(state(undefined, histories));
  assert.deepEqual(values.map(value => value.status), ['completed', 'failed', 'unknown', 'completed']);
  assert.equal(values[0]!.output, 'printed\n');
  assert.equal(values[0]!.value, '42');
  assert.equal(values[0]!.steps, 7);
  assert.equal(values[1]!.error, 'missing file');
  assert.equal(values[1]!.body?.size, '9007199254740993');
  assert.equal(values[1]!.truncated, true);
  assert.equal(values[3]!.code, '');
  assert.ok(rows.some(row => row.kind === 'restart' && row.text === 'Restarted · restored 2 · 1 skipped'));
  assert.notEqual(values[0]!.id, values[1]!.id);
  assert.equal(values[0]!.observedStartedAt, undefined);
  assert.equal(values[0]!.observedEndedAt, undefined);
  assert.ok(Object.isFrozen(rows));
  assert.ok(Object.isFrozen(rows[0]));
});

test('a live cell retains its key and host evidence when recorded, with repeated IDs in later turns', () => {
  const book = new Notebook();
  book.histories = history([call(1, 'a', 'old'), completed(2, 'a', success)]);
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec', args: '{"code":"new"}' });
  book.emit('stream.cell.host', { id: 'a', name: 'fs.read', args: '/new', text: '8ms', result: 'missing' });
  const live = cells(book.snapshot()).at(-1)!;
  book.emit('stream.tool.completed', { id: 'a', name: 'rlm_exec', result: success }, 120);
  book.histories = history([...book.histories.root!.messages, call(3, 'a', 'new'), completed(4, 'a', success)]);
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const recorded = cells(book.snapshot());
  assert.equal(recorded.length, 2);
  assert.equal(recorded[1]!.id, live.id);
  assert.equal(recorded[1]!.hosts[0]!.error, 'missing');
  assert.equal(recorded[1]!.seq, 3);
  book.active.root = 'turn-2';
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec', args: '{"code":"third"}' });
  assert.equal(cells(book.snapshot()).length, 3);
  assert.notEqual(cells(book.snapshot())[2]!.id, live.id);
});

test('same tool-call ID can occur twice within one turn, including a malformed completion', () => {
  const book = new Notebook();
  book.emit('stream.tool.call', { id: 'same', name: 'rlm_exec', args: '{"code":"first"}' });
  book.emit('stream.tool.completed', { id: 'same', name: 'rlm_exec', result: 'malformed' });
  book.emit('stream.tool.call', { id: 'same', name: 'rlm_exec', args: '{"code":"second"}' });
  const rows = cells(book.snapshot());
  assert.equal(rows.length, 2);
  assert.equal(rows[0]!.status, 'unknown');
  assert.equal(rows[1]!.status, 'writing');
});

test('snapshot replay is BigInt-safe and does not duplicate calls, hosts or client timings', () => {
  const current = root({ cursor: '9007199254740998', active_turns: { root: 'turn-1', child: 'child-turn' }, presentation: [
    { seq: '9007199254740993', kind: 'stream.tool.started', payload: { id: 'a', name: 'rlm_exec', args: '{"code":"print(42)"}' } },
    { seq: '9007199254740994', kind: 'stream.cell.host', payload: { id: 'a', name: 'fs.read', text: '7ms' } },
    { seq: '9007199254740995', kind: 'stream.cell.host', payload: { id: 'a', name: 'fs.read', text: '7ms' } },
    { seq: '9007199254740996', kind: 'stream.tool.output', payload: { id: 'a', text: 'one' } },
    { seq: '9007199254740997', kind: 'stream.tool.output', payload: { id: 'a', text: 'one\ntwo' } },
  ], agent_presentations: { child: [
    { seq: '9007199254740998', kind: 'stream.tool.started', payload: { id: 'a', name: 'rlm_exec', args: '{"code":"child"}' } },
  ] } });
  const evidence = seedExecutions(undefined, current, history());
  const first = cells(state(evidence, history(), current))[0]!;
  assert.equal(first.hosts.length, 2);
  assert.equal(first.output, 'one\ntwo');
  assert.equal(first.observedStartedAt, undefined);
  assert.equal(cells(state(evidence, history(), current), 'child')[0]!.code, 'child');
  assert.deepEqual(seedExecutions(evidence, current, history()), evidence);
  const repeated = observeExecution(evidence, current.presentation![1]!, current.active_turns, history(), 10);
  assert.equal(repeated, evidence);
});

for (const terminal of ['succeeded', 'failed', 'cancelled', 'interrupted']) {
  test(`unfinished cells settle truthfully on turn.${terminal}`, () => {
    const book = new Notebook();
    book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec', args: '{"code":"sleep()"}' });
    book.emit(`turn.${terminal}`, { turn_id: 'turn-1', error: terminal === 'failed' ? 'turn failed' : undefined }, 200);
    const row = cells(book.snapshot())[0]!;
    assert.equal(row.status, terminal === 'succeeded' ? 'unknown' : terminal);
    assert.equal(row.observedEndedAt, 200);
  });
}

test('disconnect ends indefinite running indicators; history revision and root identity discard incompatible evidence', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec' });
  const stale = settleExecutions(book.evidence);
  assert.equal(cells(state(stale))[0]!.status, 'unknown');
  assert.equal(cells(state(stale))[0]!.observedEndedAt, undefined);
  const revised = seedExecutions(book.evidence, root({ history_revision: '2', cursor: '100' }), history());
  assert.equal(revised.rows.length, 0);
  const other = seedExecutions(book.evidence, root({ root_id: 'other', cursor: '100' }), history());
  assert.equal(other.rows.length, 0);
});

test('restart events survive turns and byte/count bounds include hosts, identifiers and oversized active output', () => {
  const book = new Notebook();
  book.emit('scratch.restored', { restored: ['x'], not_restored: [{ name: 'y', reason: 'missing' }] });
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec' });
  for (let n = 0; n < 130; n++) book.emit('stream.cell.host', { id: 'a', name: 'fs.read', args: 'x'.repeat(8192), text: '1ms' });
  book.emit('stream.tool.output', { id: 'a', text: `${'👋'.repeat(200_000)}tail` });
  const active = cells(book.snapshot())[0]!;
  assert.equal(active.hosts.length, 128);
  assert.ok(active.output.endsWith('tail'));
  assert.equal(active.truncated, true);
  assert.ok(new TextEncoder().encode(JSON.stringify(book.evidence)).length <= 1024 * 1024);
  assert.equal(executionRows(book.snapshot(), 'root')[0]!.kind, 'restart');
  for (let n = 0; n < 300; n++) {
    book.emit('stream.tool.started', { id: `call-${n}`, name: 'rlm_exec', args: JSON.stringify({ code: 'x'.repeat(5000) }) });
    book.emit('stream.tool.completed', { id: `call-${n}`, name: 'rlm_exec', result: success });
  }
  assert.ok(book.evidence.rows.length <= 256);
  assert.equal(book.evidence.truncated, true);
  assert.ok(new TextEncoder().encode(JSON.stringify(book.evidence)).length <= 1024 * 1024);
  assert.ok(book.evidence.rows.some(row => row.id === active.id), 'older active evidence outlives completed evidence');
  book.emit('stream.tool.started', { id: 'huge'.repeat(400_000), name: 'rlm_exec' });
  assert.ok(new TextEncoder().encode(JSON.stringify(book.evidence)).length <= 1024 * 1024);
});


test('a newly opened child joins the newest recorded occurrence, not an older reused ID', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'same', agent_id: 'child', name: 'rlm_exec', args: '{"code":"same code"}' });
  const live = cells(book.snapshot(), 'child')[0]!;
  const childHistory = history([call(1, 'same', 'same code'), completed(2, 'same', success)]).root!;
  book.histories.child = childHistory;
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  assert.equal(cells(book.snapshot(), 'child').length, 2, 'an active call cannot steal an older completed result');
  book.emit('stream.tool.completed', { id: 'same', agent_id: 'child', name: 'rlm_exec', result: success });
  book.histories.child = history([...childHistory.messages, call(3, 'same', 'same code'), completed(4, 'same', success)]).root!;
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const joined = cells(book.snapshot(), 'child');
  assert.equal(joined.length, 2);
  assert.equal(joined[1]!.id, live.id);
  assert.equal(joined[1]!.seq, 3);
});

test('completed result output is authoritative and unsafe step counters are not rounded for display', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec' });
  book.emit('stream.tool.output', { id: 'a', text: 'provisional output' });
  book.emit('stream.tool.completed', { id: 'a', name: 'rlm_exec', result: '{"value":null,"steps":18446744073709551615}' });
  const row = cells(book.snapshot())[0]!;
  assert.equal(row.status, 'completed');
  assert.equal(row.output, '');
  assert.equal(row.steps, undefined);
  assert.equal(row.truncated, true);
});

test('embedded restart identity survives commit and scoped body refs remain immutable', () => {
  const book = new Notebook();
  const restored = JSON.stringify({ value: null, steps: 1, restored: { restored: ['x'] } });
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec', args: '{"code":"first"}' });
  book.emit('stream.tool.completed', { id: 'a', name: 'rlm_exec', result: restored });
  const restart = executionRows(book.snapshot(), 'root').find(row => row.kind === 'restart')!;
  const resultMessage = completed(2, 'a', restored);
  resultMessage.body = { reference_id: 'ref', digest: 'digest', size: '12', media_type: 'application/json', source: 'history' };
  book.histories = history([call(1, 'a', 'first'), resultMessage]);
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const rows = executionRows(book.snapshot(), 'root');
  assert.equal(rows.length, 2);
  assert.equal(rows.find(row => row.kind === 'restart')!.id, restart.id);
  assert.ok(Object.isFrozen(cells(book.snapshot())[0]!.body));
});


test('result-only history joins the same observed cell when backward paging supplies its call', () => {
  const book = new Notebook();
  book.histories.root!.throughSeq = 100;
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec', args: '{"code":"current"}' });
  book.emit('stream.cell.host', { id: 'a', name: 'fs.read', text: '5ms' });
  book.emit('stream.tool.completed', { id: 'a', name: 'rlm_exec', result: success });
  const live = cells(book.snapshot())[0]!;
  book.histories = history([completed(102, 'a', success)]);
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  assert.equal(cells(book.snapshot()).length, 1);
  assert.equal(cells(book.snapshot())[0]!.code, 'current');
  book.histories = history([call(101, 'a', 'current'), completed(102, 'a', success)]);
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const joined = cells(book.snapshot());
  assert.equal(joined.length, 1);
  assert.equal(joined[0]!.id, live.id);
  assert.equal(joined[0]!.seq, 101);
  assert.equal(joined[0]!.hosts.length, 1);
});

test('initial child history cannot borrow observed traces from a completed identical call', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'same', agent_id: 'child', name: 'rlm_exec', args: '{"code":"same code"}' });
  book.emit('stream.cell.host', { id: 'same', agent_id: 'child', name: 'new-host', text: '8ms' });
  book.emit('stream.tool.completed', { id: 'same', agent_id: 'child', name: 'rlm_exec', result: success });
  const live = cells(book.snapshot(), 'child')[0]!;
  const old = history([call(1, 'same', 'same code'), completed(2, 'same', success)]).root!;
  book.histories.child = old;
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const ambiguous = cells(book.snapshot(), 'child');
  assert.equal(ambiguous.length, 2);
  assert.equal(ambiguous[0]!.hosts.length, 0);
  assert.equal(ambiguous[1]!.id, live.id);
  assert.equal(ambiguous[1]!.historyUnmatched, true, 'unresolved history association is disclosed');
  book.histories.child = history([...old.messages, call(3, 'same', 'same code'), completed(4, 'same', success)]).root!;
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const joined = cells(book.snapshot(), 'child');
  assert.equal(joined.length, 3, 'ambiguous observations remain explicitly separate');
  assert.equal(joined[0]!.hosts.length, 0);
  assert.equal(joined.find(row => row.id === live.id)!.hosts[0]!.name, 'new-host');
  assert.equal(joined.find(row => row.id === live.id)!.historyUnmatched, true);
  assert.ok(joined.filter(row => row.id !== live.id).every(row => row.hosts.length === 0));
});

test('current observed child work stays below history when its first page arrives', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'new', agent_id: 'child', name: 'rlm_exec', args: '{"code":"new()"}' });
  book.histories.child = history([call(1, 'old', 'old()'), completed(2, 'old', success)]).root!;
  assert.deepEqual(cells(book.snapshot(), 'child').map(row => row.code), ['old()', 'new()']);
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  assert.deepEqual(cells(book.snapshot(), 'child').map(row => row.code), ['old()', 'new()']);
});


for (const placeholder of ['loading', 'error']) {
  test(`initial ${placeholder} history is not a known occurrence boundary`, () => {
    const book = new Notebook();
    book.histories.child = { ...history().root!, loading: placeholder === 'loading', ...(placeholder === 'error' ? { error: new Error('missing child') } : {}) };
    book.emit('stream.tool.started', { id: 'same', agent_id: 'child', name: 'rlm_exec', args: '{"code":"new()"}' });
    book.emit('stream.tool.completed', { id: 'same', agent_id: 'child', name: 'rlm_exec', result: success });
    book.evidence = reconcileExecutions(book.evidence, book.histories);
    book.histories.child = history([call(1, 'same', 'old()'), completed(2, 'same', success)]).root!;
    book.evidence = reconcileExecutions(book.evidence, book.histories);
    const rows = cells(book.snapshot(), 'child');
    assert.deepEqual(rows.map(row => row.code), ['old()', 'new()']);
    assert.equal(rows[1]!.historyUnmatched, true);
  });
}

test('an observed child restart stays after old history and does not steal an identical embedded restart', () => {
  const book = new Notebook();
  book.emit('scratch.restored', { agent_id: 'child', restored: ['x'] });
  const marker = executionRows(book.snapshot(), 'child')[0]!;
  book.histories.child = history([call(1, 'old', 'old()'), completed(2, 'old', JSON.stringify({ value: null, steps: 1, restored: { restored: ['x'] } }))]).root!;
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const rows = executionRows(book.snapshot(), 'child');
  assert.equal(rows.length, 3);
  assert.equal(rows[2]!.id, marker.id);
  assert.notEqual(rows[1]!.id, marker.id);
});


test('an ambiguous first-read observation cannot attach to a later reuse of its call ID', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'same', agent_id: 'child', name: 'rlm_exec', args: '{"code":"first()"}' });
  book.emit('stream.cell.host', { id: 'same', agent_id: 'child', name: 'first-host', text: '1ms' });
  book.emit('stream.tool.completed', { id: 'same', agent_id: 'child', name: 'rlm_exec', result: success });
  const live = cells(book.snapshot(), 'child')[0]!;
  book.histories.child = history([call(1, 'same', 'first()'), completed(2, 'same', success)]).root!;
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  book.histories.child = history([...book.histories.child.messages, call(3, 'same', 'future()'), completed(4, 'same', success)]).root!;
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const rows = cells(book.snapshot(), 'child');
  assert.equal(rows.find(row => row.code === 'future()')!.hosts.length, 0);
  assert.equal(rows.find(row => row.id === live.id)!.historyUnmatched, true);
});

test('known history boundaries associate repeated IDs in order across multiple committed occurrences', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'same', name: 'rlm_exec', args: '{"code":"first()"}' });
  book.emit('stream.tool.completed', { id: 'same', name: 'rlm_exec', result: success });
  const first = cells(book.snapshot())[0]!;
  book.histories = history([call(1, 'same', 'first()'), completed(2, 'same', success), call(3, 'same', 'future()'), completed(4, 'same', success)]);
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const joined = cells(book.snapshot());
  assert.equal(joined.length, 2);
  assert.equal(joined[0]!.id, first.id);
  assert.equal(joined[0]!.code, 'first()');
});


test('bounded first history cannot prove that an absent closed observed occurrence is uncommitted', () => {
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'absent', agent_id: 'child', name: 'rlm_exec', args: '{"code":"old()"}' });
  book.emit('stream.cell.host', { id: 'absent', agent_id: 'child', name: 'old-host' });
  book.emit('stream.tool.completed', { id: 'absent', agent_id: 'child', name: 'rlm_exec', result: success });
  const live = cells(book.snapshot(), 'child')[0]!;
  book.histories.child = { ...history([call(100, 'other', 'other()'), completed(101, 'other', success)]).root!, hasMore: true };
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  book.histories.child = history([...book.histories.child.messages, call(102, 'absent', 'future()'), completed(103, 'absent', success)]).root!;
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  const rows = cells(book.snapshot(), 'child');
  assert.equal(rows.find(row => row.id === live.id)!.historyUnmatched, true);
  assert.equal(rows.find(row => row.code === 'future()')!.hosts.length, 0);
});


test('failed structured results preserve output, scratch notices and restore counts through live and recorded replay', () => {
  const payload = 'Error: Traceback\ncell failed\n' + JSON.stringify({ value: null, output: 'before failure\n', steps: 11,
    scratch: { warning: 'Scratch checkpoint failed. Do not replay effects.', skipped: [{ name: 'helper', reason: 'closure' }], skipped_omitted: 3 },
    restored: { restored: ['saved'], restored_omitted: 2, failed: [], failed_omitted: 1 }, truncated: true });
  const book = new Notebook();
  book.emit('stream.tool.started', { id: 'a', name: 'rlm_exec', args: '{"code":"fail()"}' });
  book.emit('stream.tool.output', { id: 'a', text: 'old streamed output' });
  book.emit('stream.tool.completed', { id: 'a', name: 'rlm_exec', result: payload });
  const assertResult = (snapshot: SessionViewSnapshot) => {
    const cell = cells(snapshot)[0]!;
    assert.equal(cell.status, 'failed');
    assert.equal(cell.output, 'before failure\n');
    assert.equal(cell.steps, 11);
    assert.equal(cell.error, 'Traceback\ncell failed');
    assert.match(cell.scratch!, /Do not replay effects/);
    assert.match(cell.scratch!, /helper: closure/);
    assert.match(cell.scratch!, /3 additional bindings omitted/);
    assert.equal(cell.truncated, true);
    assert.ok(executionRows(snapshot, 'root').some(row => row.kind === 'restart' && row.text === 'Restarted · restored 3 · 1 skipped'));
  };
  assertResult(book.snapshot());
  book.histories = history([call(1, 'a', 'fail()'), completed(2, 'a', payload)]);
  book.evidence = reconcileExecutions(book.evidence, book.histories);
  assertResult(book.snapshot());
  assertResult(state(undefined, book.histories));
});

test('bounded return-value previews remain previews instead of falsely showing null', () => {
  const snapshot = state(undefined, history([call(1, 'a', 'huge_value'), completed(2, 'a', JSON.stringify({
    value: null, value_preview: '[1,2,...', steps: 2, truncated: true,
  }))]));
  assert.equal(cells(snapshot)[0]!.value, '[1,2,...');
  assert.equal(cells(snapshot)[0]!.truncated, true);
});
