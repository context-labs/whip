import { describe, expect, it } from 'vitest';
import {
  buildSpanTree,
  clampView,
  fitView,
  flattenRows,
  formatDuration,
  formatTick,
  isSpanInFlight,
  niceTicks,
  panBy,
  rollup,
  spanCategory,
  spanDisplayName,
  spanEndMs,
  spanZoomView,
  timeForX,
  timelineDomain,
  traceTotals,
  projectTraceRows,
  formatNanoUSD,
  formatCount,
  xForTime,
  zoomAt,
  type TraceSpan,
} from '../src/trace-math';

function span(overrides: Partial<TraceSpan> & { id: string }): TraceSpan {
  return {
    record: {
      sequence: '1',
      root_id: 'root',
      session_id: 'a',
      turn_id: 'turn',
      source_kind: 'operation',
      source_id: overrides.id,
      span_id: overrides.id,
      span: null,
    },
    traceId: 't',
    parentId: '',
    sessionId: 'a',
    turnId: 'turn',
    kind: 'tool',
    name: overrides.id,
    status: 'succeeded',
    startMs: 0,
    endMs: 10,
    attrs: {},
    ...overrides,
  };
}
const ids = (nodes: readonly { span: TraceSpan }[]) => nodes.map((node) => node.span.id);

describe('buildSpanTree', () => {
  it('treats spans with a missing parent as roots and sorts children by start then id', () => {
    const tree = buildSpanTree([
      span({ id: 'root', startMs: 0 }),
      span({ id: 'b', parentId: 'root', startMs: 5 }),
      span({ id: 'a', parentId: 'root', startMs: 5 }),
      span({ id: 'early', parentId: 'root', startMs: 1 }),
      span({ id: 'leaf', parentId: 'a', startMs: 6 }),
      span({ id: 'orphan', parentId: 'gone', startMs: -1 }),
    ]);
    expect(ids(tree)).toEqual(['orphan', 'root']);
    expect(ids(tree[1]!.children)).toEqual(['early', 'a', 'b']);
    expect(tree[1]!.children[1]!.children[0]!.depth).toBe(2);
    expect(tree[0]!.depth).toBe(0);
  });
});

describe('flattenRows', () => {
  const tree = buildSpanTree([
    span({ id: 'root' }),
    span({ id: 'child', parentId: 'root', startMs: 1 }),
    span({ id: 'grandchild', parentId: 'child', startMs: 2 }),
  ]);
  it('expands everything for null', () => {
    expect(ids(flattenRows(tree, null))).toEqual(['root', 'child', 'grandchild']);
  });
  it('descends only into expanded nodes', () => {
    expect(ids(flattenRows(tree, new Set(['root'])))).toEqual(['root', 'child']);
    expect(ids(flattenRows(tree, new Set()))).toEqual(['root']);
  });
});

describe('span timing', () => {
  it('draws an open span to now, or to its start without a clock', () => {
    const open = span({ id: 'o', startMs: 100, endMs: null, status: 'running' });
    expect(spanEndMs(open, 250)).toBe(250);
    expect(spanEndMs(open)).toBe(100);
    expect(spanEndMs(span({ id: 'c', startMs: 100, endMs: 140 }), 999)).toBe(140);
    expect(isSpanInFlight(open)).toBe(true);
    expect(isSpanInFlight(span({ id: 'c', startMs: 1, endMs: 2 }))).toBe(false);
  });
  it('spans the domain over open and closed spans', () => {
    const domain = timelineDomain(
      [
        span({ id: 'a', startMs: 1000, endMs: 1500 }),
        span({ id: 'b', startMs: 1200, endMs: null }),
      ],
      3000,
    );
    expect(domain).toEqual({ startMs: 1000, dur: 2000 });
    expect(timelineDomain([])).toEqual({ startMs: 0, dur: 1 });
    expect(timelineDomain([span({ id: 'z', startMs: 5, endMs: 5 })]).dur).toBe(1);
  });
});

describe('view math', () => {
  const dur = 1000;
  it('clamps zoom and pan', () => {
    expect(clampView(0, 5000, dur)).toEqual({ t0: -250, t1: 1550 });
    const narrow = clampView(500, 500.1, dur);
    expect(narrow.t1 - narrow.t0).toBeCloseTo(2);
    expect(clampView(-900, -800, dur).t0).toBe(-250);
    expect(clampView(1400, 1500, dur).t1).toBe(1250);
  });
  it('fits with margins and round-trips zoom', () => {
    const fit = fitView(dur);
    expect(fit).toEqual({ t0: -25, t1: 1060 });
    const zoomed = zoomAt(fit, 0.5, 300, dur);
    expect(xForTime(300, zoomed, 900)).toBeCloseTo(xForTime(300, fit, 900));
    const back = zoomAt(zoomed, 2, 300, dur);
    expect(back.t0).toBeCloseTo(fit.t0);
    expect(back.t1).toBeCloseTo(fit.t1);
    expect(panBy(fit, 100, dur)).toEqual({ t0: 75, t1: 1160 });
  });
  it('frames a span and maps px both ways', () => {
    const view = spanZoomView(200, 400, dur);
    expect(view.t0).toBeLessThan(200);
    expect(view.t1).toBeGreaterThan(400);
    expect(view).toEqual({ t0: 160, t1: 440 });
    expect(timeForX(xForTime(333, view, 700), view, 700)).toBeCloseTo(333);
    expect(xForTime(view.t0, view, 700)).toBe(0);
    expect(xForTime(view.t1, view, 700)).toBe(700);
  });
});

describe('ticks and labels', () => {
  it('picks a nice evenly spaced step', () => {
    const ticks = niceTicks({ t0: 0, t1: 1000 }, 900);
    expect(ticks).toEqual([0, 200, 400, 600, 800, 1000]);
    const wide = niceTicks({ t0: -37, t1: 12_345 }, 1400);
    expect(wide.length).toBeGreaterThanOrEqual(4);
    expect(wide.length).toBeLessThanOrEqual(11);
    const steps = new Set(wide.slice(1).map((t, i) => t - wide[i]!));
    expect(steps.size).toBe(1);
  });
  it('formats ticks and durations', () => {
    expect(formatTick(850)).toBe('850ms');
    expect(formatTick(1500)).toBe('1.5s');
    expect(formatTick(120_000)).toBe('2m');
    expect(formatTick(-500)).toBe('-500ms');
    expect(formatDuration(41)).toBe('41ms');
    expect(formatDuration(26_300)).toBe('26.3s');
    expect(formatDuration(367_000)).toBe('6m 7s');
    expect(formatDuration(7_380_000)).toBe('2h 3m');
    expect(formatDuration(0.2)).toBe('<1ms');
  });
});

describe('roll-ups', () => {
  const spans = [
    span({ id: 'root', kind: 'agent', startMs: 0, endMs: 100 }),
    span({
      id: 'l1',
      kind: 'llm',
      parentId: 'root',
      startMs: 1,
      attrs: {
        'whip.cost.nano_usd': 1000n,
        'whip.cost.source': 'reported',
        'gen_ai.usage.input_tokens': 10n,
        'gen_ai.usage.output_tokens': 5n,
      },
    }),
    span({
      id: 'l2',
      kind: 'llm',
      parentId: 'root',
      startMs: 2,
      attrs: {
        'whip.cost.nano_usd': 500n,
        'whip.cost.source': 'estimated',
        'gen_ai.usage.input_tokens': 20n,
      },
    }),
    span({
      id: 'l3',
      kind: 'llm',
      parentId: 'root',
      startMs: 3,
      attrs: {
        'whip.cost.nano_usd': 999n,
        'whip.cost.source': 'unknown',
        'gen_ai.usage.output_tokens': 7n,
      },
    }),
    span({
      id: 'tool',
      kind: 'tool',
      parentId: 'l1',
      startMs: 4,
      attrs: { 'whip.cost.nano_usd': 123n },
    }),
  ];
  it('sums reported and estimated cost only, and counts descendants', () => {
    const [root] = buildSpanTree(spans);
    expect(rollup(root!)).toEqual({
      costNanoUSD: 1500n,
      promptTokens: 30n,
      completionTokens: 12n,
      missingCost: 1,
      missingInput: 1,
      missingOutput: 1,
      descendants: 4,
    });
    const tool = root!.children[0]!.children[0]!;
    expect(rollup(tool)).toEqual({
      costNanoUSD: null,
      promptTokens: null,
      completionTokens: null,
      missingCost: 0,
      missingInput: 0,
      missingOutput: 0,
      descendants: 0,
    });
  });
  it('totals the trace for the header', () => {
    expect(traceTotals(spans)).toEqual({
      costNanoUSD: 1500n,
      promptTokens: 30n,
      completionTokens: 12n,
      missingCost: 1,
      missingInput: 1,
      missingOutput: 1,
      descendants: 4,
      durationMs: 100,
      spans: 5,
    });
    expect(traceTotals([]).durationMs).toBe(0);
  });
});

describe('labels', () => {
  it('follows the per-kind rules', () => {
    expect(
      spanDisplayName(
        span({ id: 'x', kind: 'llm', name: 'chat', attrs: { 'gen_ai.request.model': 'gpt-x' } }),
      ),
    ).toBe('gpt-x');
    expect(spanDisplayName(span({ id: 'x', kind: 'llm', name: 'chat' }))).toBe('chat');
    expect(
      spanDisplayName(
        span({ id: 'x', kind: 'tool', name: 'read', attrs: { 'whip.summary': 'a.txt' } }),
      ),
    ).toBe('read: a.txt');
    expect(spanDisplayName(span({ id: 'x', kind: 'host', name: 'fs' }))).toBe('fs');
    expect(
      spanDisplayName(
        span({ id: 'x', kind: 'agent', name: 'run', attrs: { 'whip.agent.name': 'planner' } }),
      ),
    ).toBe('planner');
    expect(
      spanDisplayName(
        span({ id: 'x', kind: 'wait', name: 'approval', attrs: { 'whip.agent.name': 'ignored' } }),
      ),
    ).toBe('approval');
  });
  it('truncates on a word boundary with an ellipsis', () => {
    const long = 'alpha '.repeat(20).trim();
    const label = spanDisplayName(span({ id: 'x', kind: 'wait', name: long }));
    expect(label).toBe(`${'alpha '.repeat(13).trim()}…`);
    expect(label.length).toBeLessThanOrEqual(80);
    expect(spanDisplayName(span({ id: 'x', kind: 'wait', name: 'a'.repeat(100) }))).toBe(
      `${'a'.repeat(79)}…`,
    );
  });
  it('maps host to the tool category', () => {
    expect(spanCategory(span({ id: 'x', kind: 'host' }))).toBe('tool');
    expect(spanCategory(span({ id: 'x', kind: 'llm' }))).toBe('llm');
  });
});

it('projects native timestamps only after exact subtraction and preserves zero or clock-reversed closed evidence', () => {
  const base = {
    sequence: '9007199254740993',
    root_id: 'root',
    session_id: 'child',
    turn_id: 'turn',
    source_kind: 'attempt' as const,
    source_id: 'attempt',
    span_id: '0123456789abcdef',
    span: {
      trace_id: '0123456789abcdef0123456789abcdef',
      parent_span_id: null,
      kind: 'llm' as const,
      name: 'model',
      state: 'succeeded' as const,
      start_ns: '1790600000000000001',
      end_ns: '1790600000000000001',
      attributes: [
        { key: 'gen_ai.usage.input_tokens', count: '9007199254740993', text: null, flag: null },
      ],
    },
  };
  const projected = projectTraceRows([base]);
  expect(projected.originNS).toBe(1790600000000000001n);
  expect(projected.spans[0]!.startMs).toBe(0);
  expect(projected.spans[0]!.endMs).toBe(0);
  expect(isSpanInFlight(projected.spans[0]!)).toBe(false);
  expect(traceTotals(projected.spans).promptTokens).toBe(9007199254740993n);
  expect(formatCount(9007199254740993n)).toBe('9,007,199,254,740,993');
  expect(formatNanoUSD(9007199254740993n)).toBe('$9007199.254740993');
  const skew = projectTraceRows([
    { ...base, span: { ...base.span, end_ns: '1790600000000000000' } },
  ]);
  expect(skew.spans[0]!.endMs).toBe(-0.000001);
  expect(isSpanInFlight(skew.spans[0]!)).toBe(false);
  expect(spanEndMs(skew.spans[0]!)).toBe(0);
  const open = projectTraceRows([
    { ...base, span: { ...base.span, end_ns: null, state: 'running' as const } },
  ]);
  expect(spanEndMs(open.spans[0]!, -3000)).toBe(0);
  expect(projectTraceRows([{ ...base, span: null }]).spans).toEqual([]);
});
