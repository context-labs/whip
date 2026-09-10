import type { ContentHandle, RootSnapshot } from '@whip/protocol';
import type { DeepReadonly, HistoryView, SessionViewSnapshot } from './state.js';

export interface ExecutionHostCall {
  readonly id: string;
  readonly invocationId?: string;
  readonly status: 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted' | 'unknown';
  readonly name: string;
  readonly summary: string;
  readonly duration: string;
  readonly error?: string;
}

export interface ExecutionCell {
  readonly kind: 'cell';
  readonly id: string;
  readonly callId: string;
  readonly agentId: string;
  readonly turnId?: string;
  /** First observed event, retained when this cell joins recorded history. */
  readonly eventSeq?: string;
  /** Retained tool-event identities for joining bounded presentation prefixes. */
  readonly presentationSeqs?: readonly string[];
  /** Transcript sequence, when a recorded call is available. */
  readonly seq?: number;
  readonly code: string;
  readonly output: string;
  readonly value?: string;
  readonly hasValue?: boolean;
  readonly executionEngine?: string;
  readonly language?: string;
  readonly error?: string;
  /** Bounded checkpoint warning and omitted-binding details. */
  readonly scratch?: string;
  readonly status: 'writing' | 'running' | 'completed' | 'failed' | 'interrupted' | 'cancelled' | 'unknown';
  readonly hosts: readonly ExecutionHostCall[];
  readonly steps?: number;
  readonly quickjsJobs?: number;
  /** Wall time observed by this client, never inferred from replay. */
  readonly observedStartedAt?: number;
  readonly observedEndedAt?: number;
  readonly truncated?: boolean;
  /** This retained observation could not be safely associated with initial history. */
  readonly historyUnmatched?: boolean;
  readonly body?: ContentHandle;
}

export interface ExecutionRestart {
  readonly kind: 'restart';
  readonly historyUnmatched?: boolean;
  readonly id: string;
  readonly agentId: string;
  readonly text: string;
  readonly seq?: number;
}

export type ExecutionRow = ExecutionCell | ExecutionRestart;
type ObservedRow = ExecutionRow & {
  readonly eventSeq: string;
  readonly turnId?: string;
  readonly afterSeq: number;
  readonly closed?: boolean;
  readonly recordedId?: string;
  readonly recordedResultSeq?: number;
  readonly historyUnknown?: boolean;
  readonly historyUnmatched?: boolean;
};

/** Supplemental observation only. Recorded transcript bodies stay in HistoryView. */
export interface ExecutionEvidence {
  readonly rootId: string;
  readonly revision: string;
  readonly cursor: string;
  readonly rows: readonly ObservedRow[];
  readonly truncated: boolean;
}

const MAX_ENTRIES = 256;
const MAX_HOSTS = 128;
const MAX_BYTES = 1024 * 1024;
const FIELD_BYTES = 64 * 1024;
const PARSE_BYTES = 128 * 1024;
const encoder = new TextEncoder();
const decoder = new TextDecoder();
const size = (value: unknown): number => encoder.encode(JSON.stringify(value)).byteLength;
const record = (value: unknown): Record<string, unknown> | undefined => value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
const text = (value: unknown): string => typeof value === 'string' ? value : '';
const key = (...parts: (string | number)[]): string => JSON.stringify(parts);
const running = (row: ExecutionRow): boolean => row.kind === 'cell' && (row.status === 'writing' || row.status === 'running');

function hasHistoryBoundary(page: DeepReadonly<HistoryView> | undefined): boolean {
  return !!page && (page.throughSeq > 0 || page.messages.length > 0 || (!page.loading && !page.error && !page.hasMore));
}

function excerpt(value: string, limit = FIELD_BYTES, tail = false): string {
  // Bound the temporary encoder allocation as well as the retained UTF-8 result.
  const part = value.length > limit ? tail ? value.slice(-limit) : value.slice(0, limit) : value;
  const encoded = encoder.encode(part);
  if (encoded.byteLength <= limit) return part;
  let start = tail ? encoded.length - limit : 0;
  let end = tail ? encoded.length : limit;
  if (tail) while ((encoded[start]! & 0xc0) === 0x80) start++;
  else while ((encoded[end]! & 0xc0) === 0x80) end--;
  return decoder.decode(encoded.subarray(start, end));
}

/** Decode the top-level code string while arguments are still arriving. */
export function executionCode(args: string): string {
  const input = excerpt(args, PARSE_BYTES);
  let index = 0;
  const space = () => { while (/\s/.test(input[index] ?? '') && index < input.length) index++; };
  const string = (): { value: string; complete: boolean } => {
    let value = '';
    index++;
    while (index < input.length) {
      const char = input[index++]!;
      if (char === '"') return { value, complete: true };
      if (char !== '\\') {
        if (char < ' ') return { value, complete: false };
        value += char;
        continue;
      }
      const escape = input[index++];
      if (!escape) break;
      if (escape === 'u') {
        const hex = input.slice(index, index + 4);
        if (!/^[\da-f]{4}$/i.test(hex)) break;
        value += String.fromCharCode(Number.parseInt(hex, 16));
        index += 4;
      } else {
        const escapes: Record<string, string> = { '"': '"', '\\': '\\', '/': '/', b: '\b', f: '\f', n: '\n', r: '\r', t: '\t' };
        if (!(escape in escapes)) break;
        value += escapes[escape];
      }
    }
    return { value, complete: false };
  };
  space();
  if (input[index++] !== '{') return '';
  while (index < input.length) {
    space();
    if (input[index] !== '"') return '';
    const name = string();
    space();
    if (!name.complete || input[index++] !== ':') return '';
    space();
    if (name.value === 'code') return input[index] === '"' ? excerpt(string().value) : '';
    // Skip other complete JSON values without mistaking nested or quoted code keys for the top level.
    let depth = 0;
    while (index < input.length) {
      const char = input[index];
      if (char === '"') { if (!string().complete) return ''; continue; }
      if (char === '{' || char === '[') depth++;
      if (char === '}' || char === ']') { if (depth === 0) return ''; depth--; }
      if (char === ',' && depth === 0) { index++; break; }
      index++;
    }
  }
  return '';
}

type Result = Pick<ExecutionCell, 'status' | 'output' | 'value' | 'hasValue' | 'executionEngine' | 'language' | 'error' | 'scratch' | 'steps' | 'quickjsJobs' | 'truncated'> & { restart?: string; hasResult?: boolean };
function result(raw: string): Result {
  const failed = raw.startsWith('Error:');
  // The dispatcher appends one JSON result after its possibly multiline error.
  const boundary = failed ? raw.lastIndexOf('\n{') : -1;
  const payload = failed && boundary >= 0 ? raw.slice(boundary + 1) : raw;
  const error = failed ? excerpt(raw.slice(6, boundary >= 0 ? boundary : undefined).trim()) : undefined;
  const fallback: Result = failed ? { status: 'failed', output: '', error, ...(raw.length > FIELD_BYTES ? { truncated: true } : {}) } : { status: 'unknown', output: '' };
  const bounded = excerpt(payload, PARSE_BYTES);
  if (bounded !== payload) return { ...fallback, truncated: true };
  let parsed: Record<string, unknown> | undefined;
  try { parsed = record(JSON.parse(bounded)); } catch { /* Incomplete or unavailable result. */ }
  if (!parsed || (parsed.output !== undefined && typeof parsed.output !== 'string')) return fallback;
  const versioned = parsed.format_version !== undefined;
  const metrics = record(parsed.metrics);
  if (versioned) {
    if (parsed.format_version !== 2 || typeof parsed.has_value !== 'boolean' || (parsed.has_value && !('value' in parsed))
      || !((parsed.execution_engine === 'starlark' && parsed.language === 'starlark') || (parsed.execution_engine === 'quickjs' && parsed.language === 'javascript'))
      || (parsed.metrics !== undefined && !metrics)) return fallback;
    if (metrics && Object.values(metrics).some(value => typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0)) return fallback;
  } else if (!('value' in parsed) || typeof parsed.steps !== 'number' || !Number.isInteger(parsed.steps) || parsed.steps < 0) return fallback;
  const hasValue = versioned ? parsed.has_value === true : parsed.value !== null || typeof parsed.value_preview === 'string';
  const output = text(parsed.output);
  const value = hasValue ? typeof parsed.value_preview === 'string' ? parsed.value_preview : JSON.stringify(parsed.value) : '';
  const scratch = record(parsed.scratch);
  return {
    hasResult: true, status: failed ? 'failed' : 'completed', output: excerpt(output, FIELD_BYTES, true), hasValue,
    ...(hasValue ? { value: excerpt(value) } : {}),
    executionEngine: versioned ? text(parsed.execution_engine) : 'starlark', language: versioned ? text(parsed.language) : 'starlark',
    ...(error ? { error } : {}),
    ...(scratch ? { scratch: scratchText(scratch) } : {}),
    ...(versioned ? {
      ...(parsed.execution_engine === 'starlark' && typeof metrics?.starlark_steps === 'number' ? { steps: metrics.starlark_steps } : {}),
      ...(parsed.execution_engine === 'quickjs' && typeof metrics?.quickjs_jobs === 'number' ? { quickjsJobs: metrics.quickjs_jobs } : {}),
    } : Number.isSafeInteger(parsed.steps) ? { steps: parsed.steps as number } : { truncated: true }),
    ...(parsed.truncated === true || output !== excerpt(output, FIELD_BYTES, true) || value !== excerpt(value) ? { truncated: true } : {}),
    ...(record(parsed.restored) ? { restart: restartText(record(parsed.restored)!) } : {}),
  };
}

function scratchText(payload: Record<string, unknown>): string {
  const lines = text(payload.warning) ? [text(payload.warning)] : [];
  for (const item of Array.isArray(payload.skipped) ? payload.skipped.slice(0, 30) : []) {
    const skipped = record(item);
    if (skipped) lines.push(`${text(skipped.name)}: ${text(skipped.reason)}`);
  }
  const omitted = count(payload.skipped_omitted);
  if (omitted) lines.push(`${omitted} additional bindings omitted`);
  return excerpt(lines.join('\n'), 8192);
}

const count = (value: unknown): number => typeof value === 'number' && Number.isSafeInteger(value) && value > 0 ? value : 0;

function restartText(payload: Record<string, unknown>): string {
  const restored = (Array.isArray(payload.restored) ? payload.restored.length : 0) + count(payload.restored_omitted);
  const failed = payload.not_restored ?? payload.failed;
  const skipped = (Array.isArray(failed) ? failed.length : 0) + count(payload.failed_omitted);
  return `Restarted · restored ${restored}${skipped ? ` · ${skipped} skipped` : ''}`;
}

function reference(value: DeepReadonly<ContentHandle> | null | undefined): ContentHandle | undefined {
  return value ? { reference_id: value.reference_id, digest: value.digest, size: value.size,
    ...(value.media_type ? { media_type: value.media_type } : {}), ...(value.source ? { source: value.source } : {}) } : undefined;
}

type RecordedRow = ExecutionRow & { readonly resultSeq?: number };
type RecordedCell = ExecutionCell & { readonly resultSeq?: number };

function historyRows(rootId: string, agentId: string, history?: DeepReadonly<HistoryView>): RecordedRow[] {
  const rows: RecordedRow[] = [];
  const pending = new Map<string, number>();
  for (const item of history?.messages ?? []) {
    const message = item.message;
    if (!message) continue;
    for (const [index, call] of (message.tool_calls ?? []).entries()) {
      if (call.function.name !== 'rlm_exec') continue;
      const code = executionCode(call.function.arguments);
      pending.set(call.id, rows.length);
      rows.push({ kind: 'cell', id: key(rootId, agentId, history!.revision, 'history', item.seq, index), callId: call.id, agentId,
        seq: item.seq, code, output: '', status: 'unknown', hosts: [],
        ...(item.body ? { body: reference(item.body), truncated: true } : {}),
        ...(call.function.arguments.length > FIELD_BYTES ? { truncated: true } : {}),
      });
    }
    if (message.role !== 'tool' || !message.tool_call_id) continue;
    const index = pending.get(message.tool_call_id);
    const prior = index === undefined ? undefined : rows[index];
    if (prior?.kind !== 'cell' && message.name !== 'rlm_exec') continue;
    const decoded = result(typeof message.content === 'string' ? message.content : '');
    const { restart, hasResult: _hasResult, ...fields } = decoded;
    const cell: RecordedCell = {
      kind: 'cell', id: key(rootId, agentId, history!.revision, 'history-result', item.seq), callId: message.tool_call_id,
      agentId, seq: item.seq, code: '', hosts: [], ...(prior?.kind === 'cell' ? prior : {}), ...fields, resultSeq: item.seq,
      ...(item.body ? { body: reference(item.body), truncated: true } : {}),
    };
    if (index !== undefined) rows[index] = cell;
    else rows.push(cell);
    pending.delete(message.tool_call_id);
    if (restart) rows.push({ kind: 'restart', id: `${cell.id}:restart`, agentId, seq: cell.seq, text: restart });
  }
  return rows;
}

export function emptyExecutionEvidence(rootId: string, revision: string): ExecutionEvidence {
  return { rootId, revision, cursor: '0', rows: [], truncated: false };
}

/** Pure folding called only by the existing root stream; no transport or secondary cache. */
export function observeExecution(
  evidence: ExecutionEvidence, event: { seq: string; kind: string; payload: unknown },
  activeTurns: Readonly<Record<string, string>>, history: DeepReadonly<Record<string, HistoryView>>,
  observedAt?: number,
): ExecutionEvidence {
  if (BigInt(event.seq) <= BigInt(evidence.cursor)) return evidence;
  const payload = record(event.payload) ?? {};
  const agentId = text(payload.agent_id) || evidence.rootId;
  const rows = [...evidence.rows];
  const cursor = event.seq;
  const afterSeq = history[agentId]?.throughSeq ?? 0;
  const turnId = text(payload.turn_id) || activeTurns[agentId];
  if (event.kind === 'scratch.restored') {
    rows.push({ kind: 'restart', id: key(evidence.rootId, agentId, evidence.revision, 'restart', event.seq), agentId,
      text: restartText(payload), eventSeq: event.seq, afterSeq, turnId, historyUnknown: !hasHistoryBoundary(history[agentId]) });
  } else if (/^(agent\.)?turn\./.test(event.kind)) {
    for (let index = 0; index < rows.length; index++) {
      const row = rows[index]!;
      if (row.kind !== 'cell' || !running(row) || row.agentId !== agentId) continue;
      if (event.kind.endsWith('.started') || !row.turnId || row.turnId === payload.turn_id) {
        const status = event.kind.endsWith('.cancelled') ? 'cancelled' : event.kind.endsWith('.interrupted') ? 'interrupted'
          : event.kind.endsWith('.failed') ? 'failed' : 'unknown';
        rows[index] = { ...row, status, hosts: settleHosts(row.hosts, status), closed: true, ...(observedAt === undefined ? {} : { observedEndedAt: observedAt }),
          ...(payload.error ? { error: excerpt(text(payload.error)) } : {}) };
      }
    }
  } else if (event.kind.startsWith('stream.')) {
    const callId = text(payload.id);
    if (!callId) return { ...evidence, cursor };
    let index = -1;
    const invocationId = event.kind.startsWith('stream.cell.host') ? text(payload.invocation_id) : '';
    if (invocationId) index = rows.findIndex(row => row.kind === 'cell' && row.agentId === agentId && row.callId === callId
      && (!turnId || !row.turnId || row.turnId === turnId) && row.hosts.some(host => host.id === key(row.id, invocationId)));
    const knownInvocation = index >= 0;
    // Once its start has been evicted, an invocation's owner is unknowable.
    // A provider can reuse the tool ID, even within the same turn.
    if (invocationId && event.kind === 'stream.cell.host' && index < 0) return { ...evidence, cursor, truncated: true };
    for (let candidate = rows.length - 1; candidate >= 0; candidate--) {
      if (index >= 0) break;
      const row = rows[candidate]!;
      if (row.kind === 'cell' && row.agentId === agentId && row.callId === callId
        && (!turnId || !row.turnId || row.turnId === turnId)) { index = candidate; break; }
    }
    let prior = rows[index];
    // A reconnect can lose the next cell's tool prefix. Reused provider IDs do
    // not prove that its new invocation belongs to our uncertain prior cell.
    if (event.kind === 'stream.cell.host.started' && invocationId && !knownInvocation
      && prior?.kind === 'cell' && prior.status === 'unknown') return { ...evidence, cursor, truncated: true };
    if ((event.kind === 'stream.tool.call' || event.kind === 'stream.tool.started') && prior?.kind === 'cell'
      && prior.closed) { index = -1; prior = undefined; }
    const hostStart = event.kind === 'stream.cell.host.started' && invocationId && turnId && turnId === activeTurns[agentId];
    if (!prior && !hostStart && (payload.name !== 'rlm_exec' || !['stream.tool.call', 'stream.tool.started', 'stream.tool.completed'].includes(event.kind))) return { ...evidence, cursor };
    if (payload.name && payload.name !== 'rlm_exec' && !event.kind.startsWith('stream.cell.host')) return { ...evidence, cursor };
    let cell: ObservedRow & ExecutionCell = prior?.kind === 'cell' ? { ...prior } : {
      kind: 'cell', id: key(evidence.rootId, agentId, evidence.revision, 'event', event.seq), callId, agentId,
      eventSeq: event.seq, afterSeq, turnId, historyUnknown: !hasHistoryBoundary(history[agentId]), code: '', output: '', status: 'unknown', hosts: [],
    };
    switch (event.kind) {
      case 'stream.tool.call':
      case 'stream.tool.started': {
        const args = text(payload.args);
        cell = { ...cell, ...(args ? { code: executionCode(args) } : payload.truncated ? { code: '' } : {}), status: event.kind.endsWith('.started') ? 'running' : 'writing',
          ...(event.kind.endsWith('.started') && observedAt !== undefined && cell.observedStartedAt === undefined ? { observedStartedAt: observedAt } : {}),
          ...(args.length > FIELD_BYTES ? { truncated: true } : {}) };
        break;
      }
      case 'stream.tool.output': {
        const output = text(payload.text);
        const preview = excerpt(output, FIELD_BYTES, true);
        cell = { ...cell, output: preview, ...(cell.status === 'unknown' && !cell.closed && turnId ? { status: 'running' } : {}),
          ...(output !== preview ? { truncated: true } : {}) };
        break;
      }
      case 'stream.cell.host.started':
      case 'stream.cell.host': {
        const started = event.kind.endsWith('.started');
        const invocationId = text(payload.invocation_id);
        // Legacy completions have only an event occurrence; never join by name.
        const id = key(cell.id, invocationId || event.seq);
        const hosts = [...cell.hosts];
        const index = hosts.findIndex(host => host.id === id);
        if (started && (cell.closed || index >= 0)) break;
        const host: ExecutionHostCall = { id, ...(invocationId ? { invocationId } : {}), name: excerpt(text(payload.name), 1024),
          status: started ? 'running' : payload.host_status === 'cancelled' ? 'cancelled' : payload.result || payload.host_status === 'failed' ? 'failed' : payload.truncated ? 'unknown' : 'completed',
          summary: excerpt(text(payload.args), 2048), duration: started ? '' : excerpt(text(payload.text), 128),
          ...(payload.result ? { error: excerpt(text(payload.result), 2048) } : {}) };
        if (index >= 0) hosts[index] = host;
        else hosts.push(host);
        // Keep recent operations (especially the current invocation) at the cap.
        const overflow = hosts.length > MAX_HOSTS;
        cell = { ...cell, hosts: hosts.slice(-MAX_HOSTS), ...(!cell.closed && started ? { status: 'running' as const } : {}),
          ...(overflow || text(payload.name).length > 1024 || text(payload.args).length > 2048 || text(payload.result).length > 2048 ? { truncated: true } : {}) };
        break;
      }
      case 'stream.tool.completed': {
        const { restart, hasResult, ...fields } = result(text(payload.result));
        cell = { ...cell, ...fields, hosts: settleHosts(cell.hosts, fields.status), closed: true, output: hasResult ? fields.output : cell.output,
          ...(observedAt === undefined ? {} : { observedEndedAt: observedAt }) };
        if (restart && !rows.some(row => row.kind === 'restart' && row.agentId === agentId && row.turnId === turnId && row.text === restart && BigInt(row.eventSeq) >= BigInt(cell.eventSeq))) {
          rows.push({ kind: 'restart', id: `${cell.id}:restart`, agentId, text: restart, eventSeq: event.seq, afterSeq, turnId, historyUnknown: !hasHistoryBoundary(history[agentId]) });
        }
        break;
      }
      default: return { ...evidence, cursor };
    }
    if (payload.truncated) cell = { ...cell, truncated: true };
    if (event.kind.startsWith('stream.tool.')) cell = { ...cell, presentationSeqs: [...(cell.presentationSeqs ?? []), event.seq].slice(-128) };
    if (index < 0) rows.push(cell); else rows[index] = cell;
  }
  return boundExecutionEvidence({ ...evidence, cursor, rows });
}

export function boundExecutionEvidence(evidence: ExecutionEvidence, maxBytes = MAX_BYTES): ExecutionEvidence {
  const rows = [...evidence.rows];
  let truncated = evidence.truncated;
  const over = () => rows.length > MAX_ENTRIES || size({ ...evidence, rows, truncated }) > maxBytes;
  while (rows.length && over()) {
    const completed = rows.findIndex(row => !running(row));
    rows.splice(completed < 0 ? 0 : completed, 1);
    truncated = true;
  }
  return { ...evidence, rows, truncated };
}

export function settleExecutions(evidence: ExecutionEvidence, activeTurns?: Readonly<Record<string, string>>): ExecutionEvidence {
  return { ...evidence, rows: evidence.rows.map(row => row.kind === 'cell' && !row.closed && (!activeTurns || !activeTurns[row.agentId]
    || (row.turnId && row.turnId !== activeTurns[row.agentId])) ? { ...row, status: 'unknown', hosts: settleHosts(row.hosts, 'unknown'), ...(activeTurns ? { closed: true } : {}) } : row) };
}

function settleHosts(hosts: readonly ExecutionHostCall[], status: ExecutionCell['status']): readonly ExecutionHostCall[] {
  // A cell/turn outcome does not prove the host operation succeeded or failed.
  return hosts.map(host => host.status === 'running' ? { ...host,
    status: status === 'cancelled' || status === 'interrupted' ? status : 'unknown' } : host);
}

/** Reconcile loaded history without copying its bodies into the supplemental evidence. */
export function reconcileExecutions(evidence: ExecutionEvidence, history: DeepReadonly<Record<string, HistoryView>>): ExecutionEvidence {
  const recorded = new Map<string, RecordedCell[]>();
  for (const agentId of new Set(evidence.rows.map(row => row.agentId))) {
    recorded.set(agentId, historyRows(evidence.rootId, agentId, history[agentId]).filter((row): row is RecordedCell => row.kind === 'cell'));
  }
  const used = new Set<string>();
  // Match occurrences in order. There is no turn ID in transcript messages,
  // so an agent's first history read cannot prove which already-observed call
  // it contains. Establish a boundary and join subsequent commits only.
  const rows = evidence.rows.map(row => {
    if (row.historyUnknown) {
      const page = history[row.agentId];
      return hasHistoryBoundary(page) ? { ...row, afterSeq: page!.throughSeq, historyUnknown: false,
        ...(row.kind === 'restart' || row.closed ? { historyUnmatched: true } : {}) } : row;
    }
    if (row.kind !== 'cell' || row.historyUnmatched) return row;
    const cells = recorded.get(row.agentId) ?? [];
    const match = cells.find(cell => !used.has(cell.id) && cell.callId === row.callId && (row.recordedId
      ? cell.id === row.recordedId || (row.recordedResultSeq !== undefined && cell.resultSeq === row.recordedResultSeq)
      : (cell.seq ?? 0) > row.afterSeq));
    if (!match) return row;
    used.add(match.id);
    return { ...row, seq: match.seq, recordedId: match.id, recordedResultSeq: match.resultSeq, historyUnmatched: undefined };
  });
  return boundExecutionEvidence({ ...evidence, rows });
}

export function seedExecutions(previous: ExecutionEvidence | undefined, root: RootSnapshot, history: Record<string, HistoryView>): ExecutionEvidence {
  let evidence = previous?.rootId === root.root_id && previous.revision === root.history_revision
    ? previous : emptyExecutionEvidence(root.root_id, root.history_revision);
  const events = [...(root.presentation ?? []), ...Object.entries(root.agent_presentations ?? {}).flatMap(([agentId, items]) =>
    (items ?? []).map(event => ({ ...event, payload: { ...record(event.payload), agent_id: agentId } })))]
    .sort((a, b) => BigInt(a.seq) < BigInt(b.seq) ? -1 : BigInt(a.seq) > BigInt(b.seq) ? 1 : 0);
  if (evidence.rows.some(row => row.kind === 'cell' && row.status === 'unknown' && !row.closed)) {
    let replay = emptyExecutionEvidence(root.root_id, root.history_revision);
    for (const event of events) replay = observeExecution(replay, event, root.active_turns, history);
    evidence = { ...evidence, rows: evidence.rows.map(row => {
      if (row.kind !== 'cell' || row.closed || row.status !== 'unknown' || !row.turnId || root.active_turns[row.agentId] !== row.turnId) return row;
      const confirmed = replay.rows.find((item): item is ObservedRow & ExecutionCell => item.kind === 'cell'
        && item.agentId === row.agentId && item.turnId === row.turnId && item.callId === row.callId
        && (item.eventSeq === row.eventSeq || item.hosts.some(host => host.invocationId && row.hosts.some(old => old.invocationId === host.invocationId))));
      if (!confirmed || confirmed.closed) return row;
      return { ...row, status: confirmed.status, observedStartedAt: undefined, hosts: row.hosts.map(host => {
        const current = confirmed.hosts.find(item => host.invocationId && item.invocationId === host.invocationId);
        return host.status === 'unknown' && current ? { ...current, id: host.id } : host;
      }) };
    }) };
  }
  for (const event of events) evidence = observeExecution(evidence, event, root.active_turns, history);
  return reconcileExecutions(settleExecutions({ ...evidence, cursor: root.cursor }, root.active_turns), history);
}

/** Immutable notebook rows derived from the loaded history and bounded observed evidence. */
export function executionRows(snapshot: DeepReadonly<SessionViewSnapshot>, agentId: string): readonly ExecutionRow[] {
  const root = snapshot.root;
  if (!root) return [];
  const engine = root.meta?.execution_engine ?? 'starlark';
  const language = engine === 'quickjs' ? 'javascript' : engine === 'starlark' ? 'starlark' : undefined;
  const history = historyRows(root.root_id, agentId, snapshot.history[agentId]);
  const evidence = snapshot.executions;
  const observed = evidence?.revision === root.history_revision && evidence.rootId === root.root_id ? evidence.rows.filter(row => row.agentId === agentId) : [];
  const recorded = new Map(observed.filter((row): row is ObservedRow & ExecutionCell => row.kind === 'cell' && row.recordedId !== undefined)
    .map(row => [row.recordedId, row]));
  const used = new Set<string>();
  const rows: ExecutionRow[] = history.map(row => {
    if (row.kind === 'restart') {
      const live = observed.find(item => item.kind === 'restart' && !item.historyUnknown && !item.historyUnmatched && !used.has(item.id) && item.text === row.text
        && row.seq !== undefined && row.seq > item.afterSeq);
      if (!live) return row;
      used.add(live.id);
      return { ...row, id: live.id };
    }
    const live = recorded.get(row.id);
    if (!live) return row;
    used.add(live.id);
    return { ...live, ...row, id: live.id, code: row.code || live.code, hosts: live.hosts,
      observedStartedAt: live.observedStartedAt, observedEndedAt: live.observedEndedAt,
      ...(row.status === 'unknown' ? { status: live.status, output: row.output || live.output, value: row.value ?? live.value, error: row.error ?? live.error, steps: row.steps ?? live.steps } : {}),
      ...(live.truncated || row.truncated ? { truncated: true } : {}) };
  });
  for (const row of observed) {
    if (used.has(row.id)) continue;
    const following = row.historyUnknown ? -1 : rows.findIndex(item => item.seq !== undefined && item.seq > (row.seq ?? row.afterSeq));
    rows.splice(following < 0 ? rows.length : following, 0, row);
  }
  return Object.freeze(rows.map(row => Object.freeze(row.kind === 'cell'
    ? { ...row, executionEngine: row.executionEngine ?? engine,
      language: row.language ?? language, hosts: Object.freeze(row.hosts.map(host => Object.freeze(host))),
      ...(row.presentationSeqs ? { presentationSeqs: Object.freeze([...row.presentationSeqs]) } : {}),
      ...(row.body ? { body: Object.freeze(row.body) } : {}) } : row)));
}
