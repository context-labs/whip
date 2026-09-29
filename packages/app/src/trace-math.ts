// Layout math follows HALO's trace viewer (timelineMath, spanTree, rollups), ported to Whip spans.

import type { DeepReadonly, TraceRow } from '@whip/sdk/state';
type NativeSpan = NonNullable<TraceRow['span']>;
export type TraceSpanKind = NativeSpan['kind'];
/** Render-only geometry and labels over one canonical row. No execution state is
 * synthesized: record retains exact counters, timestamps and native outcome. */
export interface TraceSpan {
  readonly record: DeepReadonly<TraceRow>;
  readonly id: string;
  readonly traceId: string;
  readonly parentId: string;
  readonly sessionId: string;
  readonly turnId: string;
  readonly kind: TraceSpanKind;
  readonly name: string;
  readonly status: NativeSpan['state'];
  readonly startMs: number;
  readonly endMs: number | null;
  readonly attrs: Readonly<Record<string, string | bigint | boolean>>;
}

export function projectTraceRows(rows: readonly DeepReadonly<TraceRow>[]) {
  const present = rows.filter((row) => row.span !== null);
  const originNS =
    present.reduce<bigint | null>((origin, row) => {
      const start = BigInt(row.span!.start_ns);
      return origin === null || start < origin ? start : origin;
    }, null) ?? 0n;
  const spans: TraceSpan[] = present.map((record) => {
    const span = record.span!;
    return {
      record,
      id: record.span_id,
      traceId: span.trace_id,
      parentId: span.parent_span_id ?? '',
      sessionId: record.session_id,
      turnId: record.turn_id,
      kind: span.kind,
      name: span.name,
      status: span.state,
      startMs: Number(BigInt(span.start_ns) - originNS) / 1e6,
      endMs: span.end_ns === null ? null : Number(BigInt(span.end_ns) - originNS) / 1e6,
      attrs: Object.fromEntries(
        span.attributes.map((attribute) => [
          attribute.key,
          attribute.count !== null ? BigInt(attribute.count) : (attribute.text ?? attribute.flag!),
        ]),
      ),
    };
  });
  return { originNS, spans };
}

export interface TraceNode {
  readonly span: TraceSpan;
  readonly children: readonly TraceNode[];
  readonly depth: number;
}
/** Visible time window, in ms relative to the domain start. */
export interface TimelineView {
  t0: number;
  t1: number;
}
export interface TraceRollup {
  costNanoUSD: bigint | null;
  missingCost: number;
  missingInput: number;
  missingOutput: number;
  promptTokens: bigint | null;
  completionTokens: bigint | null;
  readonly descendants: number;
}

export const ROW_HEIGHT = 28;
export const MIN_BAR_PX = 3;
export const LABEL_MIN_WIDTH = 72;

type MutableNode = { span: TraceSpan; children: MutableNode[]; depth: number };

const byStart = (a: MutableNode, b: MutableNode) =>
  a.span.startMs === b.span.startMs
    ? a.span.id.localeCompare(b.span.id)
    : a.span.startMs - b.span.startMs;

function sortNodes(items: MutableNode[], depth: number) {
  items.sort(byStart);
  for (const item of items) {
    item.depth = depth;
    sortNodes(item.children, depth + 1);
  }
}

/** Roots are spans with no parent, or whose parent is missing from the list. */
export function buildSpanTree(spans: readonly TraceSpan[]): TraceNode[] {
  const nodes = new Map(
    spans.map((span) => [span.id, { span, children: [], depth: 0 } as MutableNode]),
  );
  const roots: MutableNode[] = [];
  for (const node of nodes.values()) {
    const parent = nodes.get(node.span.parentId);
    if (parent && parent !== node) parent.children.push(node);
    else roots.push(node);
  }
  sortNodes(roots, 0);
  return roots;
}

/** Depth-first rows; `expanded === null` expands everything. */
export function flattenRows(
  tree: readonly TraceNode[],
  expanded: ReadonlySet<string> | null,
): TraceNode[] {
  const rows: TraceNode[] = [];
  const walk = (node: TraceNode) => {
    rows.push(node);
    if (expanded === null || expanded.has(node.span.id)) node.children.forEach(walk);
  };
  tree.forEach(walk);
  return rows;
}

/** Open spans draw out to `nowMs`. */
export function spanEndMs(span: TraceSpan, nowMs?: number): number {
  return Math.max(span.startMs, span.endMs ?? nowMs ?? span.startMs);
}

export const isSpanInFlight = (span: TraceSpan) => span.endMs === null;

export function timelineDomain(
  spans: readonly TraceSpan[],
  nowMs?: number,
): { startMs: number; dur: number } {
  if (spans.length === 0) return { startMs: 0, dur: 1 };
  let startMs = Infinity,
    endMs = -Infinity;
  for (const span of spans) {
    startMs = Math.min(startMs, span.startMs);
    endMs = Math.max(endMs, spanEndMs(span, nowMs));
  }
  return { startMs, dur: Math.max(1, endMs - startMs) };
}

/** Zoom stays between 0.2% and 180% of the domain; panning within a quarter-domain margin either side. */
export function clampView(t0: number, t1: number, dur: number): TimelineView {
  const width = Math.max(Math.max(1, dur * 0.002), Math.min(dur * 1.8, t1 - t0));
  const start = Math.max(-dur * 0.25, Math.min(dur * 1.25 - width, t0));
  return { t0: start, t1: start + width };
}

export const fitView = (dur: number) => clampView(-dur * 0.025, dur * 1.06, dur);

/** Zoom keeping `anchorT` fixed on screen; factor < 1 zooms in. */
export function zoomAt(
  view: TimelineView,
  factor: number,
  anchorT: number,
  dur: number,
): TimelineView {
  return clampView(
    anchorT - (anchorT - view.t0) * factor,
    anchorT + (view.t1 - anchorT) * factor,
    dur,
  );
}

export const panBy = (view: TimelineView, deltaT: number, dur: number) =>
  clampView(view.t0 + deltaT, view.t1 + deltaT, dur);

/** Frames one span with proportional padding. */
export function spanZoomView(startT: number, endT: number, dur: number): TimelineView {
  const pad = (endT - startT) * 0.15 + dur * 0.01;
  return clampView(startT - pad, endT + pad, dur);
}

export const xForTime = (t: number, view: TimelineView, width: number) =>
  ((t - view.t0) / (view.t1 - view.t0)) * width;
export const timeForX = (x: number, view: TimelineView, width: number) =>
  view.t0 + (x / width) * (view.t1 - view.t0);

/** Ticks at a nice step (1/2/2.5/5 × 10^k ms), about one label per 110px, 4–10 labels. */
export function niceTicks(view: TimelineView, width: number): number[] {
  const span = Math.max(1e-6, view.t1 - view.t0);
  const target = Math.max(4, Math.min(10, Math.floor(width / 110)));
  let step = 5e8;
  outer: for (let k = 0; k <= 8; k += 1)
    for (const base of [1, 2, 2.5, 5]) {
      if (span / (base * 10 ** k) <= target) {
        step = base * 10 ** k;
        break outer;
      }
    }
  const ticks: number[] = [];
  for (let t = Math.ceil(view.t0 / step) * step; t <= view.t1; t += step) ticks.push(t);
  return ticks;
}

/** "850ms" / "1.5s" / "2m" / "3.5h" / "2d" axis labels. */
export function formatTick(ms: number): string {
  const sign = ms < 0 ? '-' : '';
  const abs = Math.abs(ms);
  const unit = (value: number, suffix: string) => {
    const rounded = Math.round(value * 10) / 10;
    return `${sign}${Number.isInteger(rounded) ? rounded : rounded.toFixed(1)}${suffix}`;
  };
  if (abs >= 86_400_000) return unit(abs / 86_400_000, 'd');
  if (abs >= 3_600_000) return unit(abs / 3_600_000, 'h');
  if (abs >= 60_000) return unit(abs / 60_000, 'm');
  if (abs >= 1_000) return unit(abs / 1_000, 's');
  return `${sign}${Math.round(abs)}ms`;
}

/** "41ms" / "26.3s" / "6m 7s" / "2h 3m" / "3d 4h". */
export function formatDuration(ms: number): string {
  if (ms < 1) return '<1ms';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const pair = (big: number, small: number, bigUnit: string, smallUnit: string) =>
    small > 0 ? `${big}${bigUnit} ${small}${smallUnit}` : `${big}${bigUnit}`;
  if (ms < 3_600_000)
    return pair(Math.floor(ms / 60_000), Math.round((ms % 60_000) / 1000), 'm', 's');
  if (ms < 86_400_000)
    return pair(Math.floor(ms / 3_600_000), Math.round((ms % 3_600_000) / 60_000), 'h', 'm');
  return pair(Math.floor(ms / 86_400_000), Math.round((ms % 86_400_000) / 3_600_000), 'd', 'h');
}

const count = (value: unknown): bigint | null => (typeof value === 'bigint' ? value : null);
const add = (total: bigint | null, value: bigint | null) =>
  value === null ? total : (total ?? 0n) + value;
type Sums = Omit<TraceRollup, 'descendants'>;
const emptySums = (): Sums => ({
  costNanoUSD: null,
  promptTokens: null,
  completionTokens: null,
  missingCost: 0,
  missingInput: 0,
  missingOutput: 0,
});
function addSpan(span: TraceSpan, sums: Sums) {
  if (span.kind !== 'llm') return;
  const { attrs } = span;
  const source = attrs['whip.cost.source'];
  const cost =
    source === 'reported' || source === 'estimated' ? count(attrs['whip.cost.nano_usd']) : null;
  const input = count(attrs['gen_ai.usage.input_tokens']),
    output = count(attrs['gen_ai.usage.output_tokens']);
  sums.costNanoUSD = add(sums.costNanoUSD, cost);
  sums.promptTokens = add(sums.promptTokens, input);
  sums.completionTokens = add(sums.completionTokens, output);
  sums.missingCost += Number(cost === null);
  sums.missingInput += Number(input === null);
  sums.missingOutput += Number(output === null);
}

/** Totals only these loaded attempt spans. Null and missing counts remain distinct from zero. */
export function rollup(node: TraceNode): TraceRollup {
  const sums = emptySums();
  let count = 0;
  const visit = (current: TraceNode) => {
    addSpan(current.span, sums);
    count += 1;
    current.children.forEach(visit);
  };
  visit(node);
  return { ...sums, descendants: count - 1 };
}

export function traceTotals(
  spans: readonly TraceSpan[],
  nowMs?: number,
): TraceRollup & { durationMs: number; spans: number } {
  const sums = emptySums();
  for (const span of spans) addSpan(span, sums);
  return {
    ...sums,
    descendants: Math.max(0, spans.length - 1),
    durationMs: spans.length ? timelineDomain(spans, nowMs).dur : 0,
    spans: spans.length,
  };
}

export function formatNanoUSD(value: bigint): string {
  const fraction = (value % 1_000_000_000n)
    .toString()
    .padStart(9, '0')
    .replace(/0+$/, '')
    .padEnd(4, '0');
  return `$${value / 1_000_000_000n}.${fraction}`;
}
export function formatCount(value: bigint): string {
  return value.toLocaleString('en-US');
}

const str = (value: unknown) => (typeof value === 'string' ? value : '');

function truncate(text: string, max = 80): string {
  if (text.length <= max) return text;
  const cut = text.lastIndexOf(' ', max - 1);
  return `${text.slice(0, cut > max / 2 ? cut : max - 1).trimEnd()}…`;
}

export function spanDisplayName(span: TraceSpan): string {
  const { attrs, name, kind } = span;
  const label =
    kind === 'llm'
      ? str(attrs['gen_ai.operation.name']) === 'compaction'
        ? name
        : str(attrs['gen_ai.request.model']) || name
      : kind === 'agent'
        ? str(attrs['whip.agent.name']) || name
        : kind === 'wait'
          ? name
          : str(attrs['whip.summary'])
            ? `${name}: ${str(attrs['whip.summary'])}`
            : name;
  return truncate(label);
}

export const spanCategory = (span: TraceSpan): 'agent' | 'llm' | 'tool' | 'wait' =>
  span.kind === 'host' ? 'tool' : span.kind;
