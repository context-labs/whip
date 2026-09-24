import assert from 'node:assert/strict';
import test from 'node:test';
import type { SpanPage, SpanRecord } from '@whip/protocol';
import type { SessionViewSnapshot } from '../src/state.js';
import { boundTraceEvidence, emptyTraceEvidence, mergeSpanPage, observeSpan, serverNowMs, toTraceSpan, traceRoots, traceSpans } from '../src/trace.js';

const ns = (ms: number): string => (BigInt(Math.round(ms)) * 1_000_000n).toString();
const record = (overrides: Partial<SpanRecord> & { id: string }): SpanRecord => ({
  trace_id: 't1', parent_id: '', root_id: 'root', agent_id: 'root', turn_id: 'turn-1', kind: 'agent', name: 'root', status: 'running',
  start_ns: ns(1_000), end_ns: '0', attrs: { agent_name: 'root' }, updated_seq: '1', ...overrides,
});
const snapshot = (trace: SessionViewSnapshot['trace']): SessionViewSnapshot => ({
  status: 'live', history: {}, collections: {}, retainedBytes: 0, truncated: false, unavailable: false, trace,
});

test('span events upsert by id and never regress to an older write', () => {
  let evidence = emptyTraceEvidence('root');
  evidence = observeSpan(evidence, { kind: 'span.started', payload: record({ id: 'turn' }) });
  evidence = observeSpan(evidence, { kind: 'span.started', payload: record({ id: 'call', kind: 'llm', parent_id: 'turn', name: 'p/m', start_ns: ns(1_500), updated_seq: '2' }) });
  evidence = observeSpan(evidence, { kind: 'span.ended', payload: record({ id: 'call', kind: 'llm', parent_id: 'turn', name: 'p/m', start_ns: ns(1_500), end_ns: ns(2_500), status: 'ok', attrs: { prompt_tokens: 12 }, updated_seq: '3' }) });
  // A stale start replayed after the end must not reopen the span.
  const stale = observeSpan(evidence, { kind: 'span.started', payload: record({ id: 'call', kind: 'llm', parent_id: 'turn', name: 'p/m', start_ns: ns(1_500), updated_seq: '2' }) });
  assert.equal(stale, evidence);
  const call = evidence.spans.call!;
  assert.equal(call.endMs, 2_500);
  assert.equal(call.status, 'ok');
  assert.equal(call.attrs.prompt_tokens, 12);
  assert.equal(call.parentId, 'turn');
  // Junk and other roots are ignored.
  assert.equal(observeSpan(evidence, { kind: 'span.started', payload: { id: 'x' } }), evidence);
  assert.equal(observeSpan(evidence, { kind: 'span.started', payload: record({ id: 'other', root_id: 'elsewhere' }) }), evidence);
  assert.equal(observeSpan(evidence, { kind: 'stream.text', payload: record({ id: 'nope' }) }), evidence);
  assert.equal(toTraceSpan(record({ id: 'bad', kind: 'weird' })), undefined);
});

test('pages merge, advance the cursor, and fix the server clock offset', () => {
  const page: SpanPage = {
    root_id: 'root', has_more: false, next_seq: '7', server_time_ns: ns(100_000),
    spans: [record({ id: 'turn', updated_seq: '6' }), record({ id: 'call', kind: 'llm', parent_id: 'turn', start_ns: ns(1_200), updated_seq: '7' })],
  };
  const evidence = mergeSpanPage(emptyTraceEvidence('root'), page, 90_000);
  assert.equal(evidence.loaded, true);
  assert.equal(evidence.pageCursor, '7');
  assert.equal(evidence.clockOffsetMs, 10_000);
  assert.equal(serverNowMs(evidence, 90_500), 100_500);
  assert.deepEqual(traceSpans(snapshot(evidence)).map(span => span.id), ['turn', 'call']);
  assert.deepEqual(traceSpans(snapshot(evidence), 'nope'), []);
  // A live event that already landed is not regressed by an older page.
  const live = observeSpan(evidence, { kind: 'span.ended', payload: record({ id: 'call', kind: 'llm', parent_id: 'turn', start_ns: ns(1_200), end_ns: ns(1_300), status: 'ok', updated_seq: '9' }) });
  const replayed = mergeSpanPage(live, { ...page, next_seq: '8', server_time_ns: '0' });
  assert.equal(replayed.spans.call!.endMs, 1_300);
  assert.equal(replayed.pageCursor, '8');
  assert.equal(replayed.clockOffsetMs, 10_000);
});

test('the budget evicts whole old traces first and keeps the newest trace complete', () => {
  let evidence = emptyTraceEvidence('root');
  for (const [trace, start] of [['t1', 1_000], ['t2', 2_000], ['t3', 3_000]] as const) {
    for (let index = 0; index < 4; index++) {
      evidence = observeSpan(evidence, { kind: 'span.started', payload: record({
        id: `${trace}-${index}`, trace_id: trace, parent_id: index ? `${trace}-0` : '', kind: index ? 'llm' : 'agent',
        start_ns: ns(start + index), updated_seq: String(evidence.pageCursor === '0' ? index + 1 : index + 1),
      }) });
    }
  }
  const bounded = boundTraceEvidence(evidence, 6);
  assert.equal(bounded.truncated, true);
  assert.deepEqual([...new Set(Object.values(bounded.spans).map(span => span.traceId))].sort(), ['t3']);
  assert.equal(Object.keys(bounded.spans).length, 4);
  assert.deepEqual(traceRoots(snapshot(evidence)).map(span => span.traceId), ['t3', 't2', 't1']);
  // A single oversized trace keeps its open spans over closed ones.
  const one = boundTraceEvidence({ ...bounded, spans: Object.fromEntries(Object.entries(bounded.spans).map(([id, span], index) => [id, index === 3 ? span : { ...span, endMs: span.startMs + 1, status: 'ok' as const }])) }, 2);
  assert.equal(Object.keys(one.spans).length, 2);
  assert.ok(Object.values(one.spans).some(span => span.endMs === 0));
});
