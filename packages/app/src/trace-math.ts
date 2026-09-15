// Layout math follows HALO's trace viewer (timelineMath, spanTree, rollups), ported to Whip spans.

export type TraceSpanKind = 'agent' | 'llm' | 'tool' | 'host' | 'wait';
export type TraceSpanStatus = 'running' | 'ok' | 'error' | 'cancelled' | 'interrupted';
export interface TraceSpan {
  readonly id: string;
  readonly traceId: string;
  readonly parentId: string;   // '' for a trace root
  readonly rootId: string;
  readonly agentId: string;
  readonly turnId: string;
  readonly kind: TraceSpanKind;
  readonly name: string;
  readonly status: TraceSpanStatus;
  readonly startMs: number;    // wall clock, ms since epoch, fractional allowed
  readonly endMs: number;      // 0 while the span is still open
  readonly attrs: Readonly<Record<string, unknown>>;  // whip-native keys, see below
  readonly links: readonly { readonly traceId: string; readonly spanId: string }[];
  readonly updatedSeq: string;
}

export interface TraceNode { readonly span: TraceSpan; readonly children: readonly TraceNode[]; readonly depth: number }
/** Visible time window, in ms relative to the domain start. */
export interface TimelineView { t0: number; t1: number }
export interface TraceRollup {
  readonly costMicros: number | null;
  readonly promptTokens: number | null;
  readonly completionTokens: number | null;
  readonly descendants: number;
}

export const ROW_HEIGHT = 28;
export const MIN_BAR_PX = 3;
export const LABEL_MIN_WIDTH = 72;

type MutableNode = { span: TraceSpan; children: MutableNode[]; depth: number };

const byStart = (a: MutableNode, b: MutableNode) =>
  a.span.startMs === b.span.startMs ? a.span.id.localeCompare(b.span.id) : a.span.startMs - b.span.startMs;

function sortNodes(items: MutableNode[], depth: number) {
  items.sort(byStart);
  for (const item of items) { item.depth = depth; sortNodes(item.children, depth + 1); }
}

/** Roots are spans with no parent, or whose parent is missing from the list. */
export function buildSpanTree(spans: readonly TraceSpan[]): TraceNode[] {
  const nodes = new Map(spans.map(span => [span.id, { span, children: [], depth: 0 } as MutableNode]));
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
export function flattenRows(tree: readonly TraceNode[], expanded: ReadonlySet<string> | null): TraceNode[] {
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
  return span.endMs > span.startMs ? span.endMs : nowMs ?? span.startMs;
}

export const isSpanInFlight = (span: TraceSpan) => span.endMs <= span.startMs;

export function timelineDomain(spans: readonly TraceSpan[], nowMs?: number): { startMs: number; dur: number } {
  if (spans.length === 0) return { startMs: 0, dur: 1 };
  let startMs = Infinity, endMs = -Infinity;
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
export function zoomAt(view: TimelineView, factor: number, anchorT: number, dur: number): TimelineView {
  return clampView(anchorT - (anchorT - view.t0) * factor, anchorT + (view.t1 - anchorT) * factor, dur);
}

export const panBy = (view: TimelineView, deltaT: number, dur: number) => clampView(view.t0 + deltaT, view.t1 + deltaT, dur);

/** Frames one span with proportional padding. */
export function spanZoomView(startT: number, endT: number, dur: number): TimelineView {
  const pad = (endT - startT) * 0.15 + dur * 0.01;
  return clampView(startT - pad, endT + pad, dur);
}

export const xForTime = (t: number, view: TimelineView, width: number) => ((t - view.t0) / (view.t1 - view.t0)) * width;
export const timeForX = (x: number, view: TimelineView, width: number) => view.t0 + (x / width) * (view.t1 - view.t0);

/** Ticks at a nice step (1/2/2.5/5 × 10^k ms), about one label per 110px, 4–10 labels. */
export function niceTicks(view: TimelineView, width: number): number[] {
  const span = Math.max(1e-6, view.t1 - view.t0);
  const target = Math.max(4, Math.min(10, Math.floor(width / 110)));
  let step = 5e8;
  outer: for (let k = 0; k <= 8; k += 1)
    for (const base of [1, 2, 2.5, 5]) {
      if (span / (base * 10 ** k) <= target) { step = base * 10 ** k; break outer; }
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
  if (ms < 3_600_000) return pair(Math.floor(ms / 60_000), Math.round((ms % 60_000) / 1000), 'm', 's');
  if (ms < 86_400_000) return pair(Math.floor(ms / 3_600_000), Math.round((ms % 3_600_000) / 60_000), 'h', 'm');
  return pair(Math.floor(ms / 86_400_000), Math.round((ms % 86_400_000) / 3_600_000), 'd', 'h');
}

function num(value: unknown): number | null {
  const n = typeof value === 'string' && value !== '' ? Number(value) : value;
  return typeof n === 'number' && Number.isFinite(n) ? n : null;
}
const add = (total: number | null, value: number | null) => value === null ? total : (total ?? 0) + value;

type Sums = { costMicros: number | null; promptTokens: number | null; completionTokens: number | null };
function addSpan(span: TraceSpan, sums: Sums) {
  const { attrs } = span;
  const priced = span.kind === 'llm' && (attrs.cost_source === 'reported' || attrs.cost_source === 'estimated');
  sums.costMicros = add(sums.costMicros, priced ? num(attrs.cost_micros) : null);
  sums.promptTokens = add(sums.promptTokens, num(attrs.prompt_tokens));
  sums.completionTokens = add(sums.completionTokens, num(attrs.completion_tokens));
}

/** Sums over the node and all descendants; null where nothing contributed. */
export function rollup(node: TraceNode): TraceRollup {
  const sums: Sums = { costMicros: null, promptTokens: null, completionTokens: null };
  let count = 0;
  const visit = (current: TraceNode) => { addSpan(current.span, sums); count += 1; current.children.forEach(visit); };
  visit(node);
  return { ...sums, descendants: count - 1 };
}

export function traceTotals(spans: readonly TraceSpan[], nowMs?: number): TraceRollup & { durationMs: number; spans: number } {
  const sums: Sums = { costMicros: null, promptTokens: null, completionTokens: null };
  for (const span of spans) addSpan(span, sums);
  return { ...sums, descendants: Math.max(0, spans.length - 1), durationMs: spans.length ? timelineDomain(spans, nowMs).dur : 0, spans: spans.length };
}

const str = (value: unknown) => typeof value === 'string' ? value : '';

function truncate(text: string, max = 80): string {
  if (text.length <= max) return text;
  const cut = text.lastIndexOf(' ', max - 1);
  return `${text.slice(0, cut > max / 2 ? cut : max - 1).trimEnd()}…`;
}

export function spanDisplayName(span: TraceSpan): string {
  const { attrs, name, kind } = span;
  const label =
    kind === 'llm' ? str(attrs.model) || name
    : kind === 'agent' ? str(attrs.agent_name) || name
    : kind === 'wait' ? name
    : str(attrs.summary) ? `${name}: ${str(attrs.summary)}` : name;
  return truncate(label);
}

export const spanCategory = (span: TraceSpan): 'agent' | 'llm' | 'tool' | 'wait' => span.kind === 'host' ? 'tool' : span.kind;
