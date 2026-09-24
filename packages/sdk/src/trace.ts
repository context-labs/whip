import type { SpanPage, SpanRecord } from '@whip/protocol';
import type { DeepReadonly, SessionViewSnapshot } from './state.js';

/**
 * Trace evidence is the client's copy of the daemon's span table for one root:
 * durable pages (`trace.page`) merged with live `span.started` / `span.ended`
 * events by span id. Spans are bounded per root; when the cap is reached the
 * oldest traces are evicted whole so the newest trace stays complete and the
 * view says it is truncated. Timing is server-measured; open spans are drawn
 * to the server's clock through `clockOffsetMs`.
 */
export type TraceSpanKind = 'agent' | 'llm' | 'tool' | 'host' | 'wait';
export type TraceSpanStatus = 'running' | 'ok' | 'error' | 'cancelled' | 'interrupted';

export interface TraceSpan {
  readonly id: string;
  readonly traceId: string;
  readonly parentId: string;
  readonly rootId: string;
  readonly agentId: string;
  readonly turnId: string;
  readonly kind: TraceSpanKind;
  readonly name: string;
  readonly status: TraceSpanStatus;
  /** Wall clock in milliseconds since the epoch, fractional. */
  readonly startMs: number;
  /** 0 while the span is open. */
  readonly endMs: number;
  readonly attrs: Readonly<Record<string, unknown>>;
  readonly links: readonly { readonly traceId: string; readonly spanId: string }[];
  readonly updatedSeq: string;
}

export interface TraceEvidence {
  readonly rootId: string;
  /** Page cursor: the last `updated_seq` a durable page returned. */
  readonly pageCursor: string;
  readonly spans: Readonly<Record<string, TraceSpan>>;
  readonly loaded: boolean;
  readonly loading: boolean;
  readonly hasMore: boolean;
  readonly truncated: boolean;
  /** Server clock minus client clock at the last page, so open spans grow against the daemon's time. */
  readonly clockOffsetMs: number;
  readonly error?: Error;
}

export const MAX_TRACE_SPANS = 4096;
export const MAX_TRACE_BYTES = 2 * 1024 * 1024;
const encoder = new TextEncoder();

const kinds = new Set<TraceSpanKind>(['agent', 'llm', 'tool', 'host', 'wait']);
const statuses = new Set<TraceSpanStatus>(['running', 'ok', 'error', 'cancelled', 'interrupted']);
const nanosToMs = (value: string): number => {
  if (!/^\d+$/.test(value)) return 0;
  const nanos = BigInt(value);
  return Number(nanos / 1_000_000n) + Number(nanos % 1_000_000n) / 1_000_000;
};
const object = (value: unknown): Record<string, unknown> => value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};

/** Decode one wire record; undefined when it is not a span. */
export function toTraceSpan(record: unknown): TraceSpan | undefined {
  const raw = object(record) as Partial<SpanRecord>;
  if (typeof raw.id !== 'string' || !raw.id || typeof raw.trace_id !== 'string' || typeof raw.root_id !== 'string'
    || typeof raw.agent_id !== 'string' || typeof raw.start_ns !== 'string' || typeof raw.updated_seq !== 'string') return undefined;
  if (!kinds.has(raw.kind as TraceSpanKind) || !statuses.has(raw.status as TraceSpanStatus)) return undefined;
  const links = Array.isArray(raw.links) ? raw.links.flatMap(link => {
    const value = object(link);
    return typeof value.trace_id === 'string' && typeof value.span_id === 'string' ? [{ traceId: value.trace_id, spanId: value.span_id }] : [];
  }) : [];
  return {
    id: raw.id, traceId: raw.trace_id, parentId: raw.parent_id ?? '', rootId: raw.root_id, agentId: raw.agent_id, turnId: raw.turn_id ?? '',
    kind: raw.kind as TraceSpanKind, name: typeof raw.name === 'string' ? raw.name : '', status: raw.status as TraceSpanStatus,
    startMs: nanosToMs(raw.start_ns), endMs: typeof raw.end_ns === 'string' ? nanosToMs(raw.end_ns) : 0,
    attrs: Object.freeze({ ...object(raw.attrs) }), links: Object.freeze(links), updatedSeq: raw.updated_seq,
  };
}

export function emptyTraceEvidence(rootId: string): TraceEvidence {
  return { rootId, pageCursor: '0', spans: {}, loaded: false, loading: false, hasMore: false, truncated: false, clockOffsetMs: 0 };
}

const newer = (a: string, b: string): boolean => BigInt(a) > BigInt(b);

function upsert(spans: Record<string, TraceSpan>, span: TraceSpan): boolean {
  const existing = spans[span.id];
  if (existing && !newer(span.updatedSeq, existing.updatedSeq)) return false;
  spans[span.id] = span;
  return true;
}

/** Fold one journal event; anything but a span event returns the evidence unchanged. */
export function observeSpan(evidence: TraceEvidence, event: { kind: string; payload: unknown }): TraceEvidence {
  if (event.kind !== 'span.started' && event.kind !== 'span.ended') return evidence;
  const span = toTraceSpan(event.payload);
  if (!span || span.rootId !== evidence.rootId) return evidence;
  const spans = { ...evidence.spans };
  if (!upsert(spans, span)) return evidence;
  return boundTraceEvidence({ ...evidence, spans });
}

/** Merge one durable page and record the server clock it was read at. */
export function mergeSpanPage(evidence: TraceEvidence, page: SpanPage, receivedAtMs = Date.now()): TraceEvidence {
  const spans = { ...evidence.spans };
  for (const record of page.spans ?? []) {
    const span = toTraceSpan(record);
    if (span && span.rootId === evidence.rootId) upsert(spans, span);
  }
  const serverNow = nanosToMs(page.server_time_ns);
  return boundTraceEvidence({
    ...evidence, spans, loaded: true, loading: false, hasMore: page.has_more, error: undefined,
    pageCursor: newer(page.next_seq, evidence.pageCursor) ? page.next_seq : evidence.pageCursor,
    clockOffsetMs: serverNow > 0 ? serverNow - receivedAtMs : evidence.clockOffsetMs,
  });
}

/** Evict whole traces, oldest first, until the span count and byte budget hold. */
export function boundTraceEvidence(evidence: TraceEvidence, maxSpans = MAX_TRACE_SPANS, maxBytes = MAX_TRACE_BYTES): TraceEvidence {
  let spans = evidence.spans;
  let count = Object.keys(spans).length;
  let bytes = count > maxSpans ? Infinity : encoder.encode(JSON.stringify(spans)).byteLength;
  if (count <= maxSpans && bytes <= maxBytes) return evidence;
  const starts = new Map<string, number>();
  for (const span of Object.values(spans)) starts.set(span.traceId, Math.min(starts.get(span.traceId) ?? Infinity, span.startMs));
  const traces = [...starts.entries()].sort((a, b) => a[1] - b[1]).map(([traceId]) => traceId);
  let truncated = evidence.truncated;
  while (traces.length > 1 && (count > maxSpans || bytes > maxBytes)) {
    const oldest = traces.shift()!;
    spans = Object.fromEntries(Object.entries(spans).filter(([, span]) => span.traceId !== oldest));
    count = Object.keys(spans).length;
    bytes = encoder.encode(JSON.stringify(spans)).byteLength;
    truncated = true;
  }
  if (count > maxSpans || bytes > maxBytes) {
    // One trace alone is over budget: keep its newest spans, drop closed old ones first.
    const ordered = Object.values(spans).sort((a, b) => Number(a.endMs === 0) - Number(b.endMs === 0) || a.startMs - b.startMs);
    while (ordered.length && (ordered.length > maxSpans || bytes > maxBytes)) {
      ordered.shift();
      bytes = encoder.encode(JSON.stringify(ordered)).byteLength;
    }
    spans = Object.fromEntries(ordered.map(span => [span.id, span]));
    truncated = true;
  }
  return { ...evidence, spans, truncated };
}

/** Spans of one trace (or every trace) in start order. */
export function traceSpans(snapshot: DeepReadonly<SessionViewSnapshot>, traceId?: string): readonly TraceSpan[] {
  const evidence = snapshot.trace;
  if (!evidence) return [];
  return Object.values(evidence.spans)
    .filter(span => !traceId || span.traceId === traceId)
    .sort((a, b) => a.startMs - b.startMs || a.id.localeCompare(b.id)) as TraceSpan[];
}

/** Turn spans that start a trace, newest first: the trace picker's list. */
export function traceRoots(snapshot: DeepReadonly<SessionViewSnapshot>): readonly TraceSpan[] {
  const evidence = snapshot.trace;
  if (!evidence) return [];
  return Object.values(evidence.spans)
    .filter(span => span.kind === 'agent' && span.parentId === '')
    .sort((a, b) => b.startMs - a.startMs || a.id.localeCompare(b.id)) as TraceSpan[];
}

/** The daemon's current time as this client can best estimate it. */
export function serverNowMs(evidence: DeepReadonly<TraceEvidence> | undefined, clientNowMs = Date.now()): number {
  return clientNowMs + (evidence?.clockOffsetMs ?? 0);
}
