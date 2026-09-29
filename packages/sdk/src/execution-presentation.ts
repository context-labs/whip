import type { AttemptPresentation, Message, MessagePresentation, Operations } from '@whip/protocol';
import type { DeepReadonly, SessionViewSnapshot } from './state.js';
import { cellExecutionRows } from './execution-state.js';
import type { CellExecutionRow, ExecutionViewSnapshot } from './execution-state.js';

type Preview = NonNullable<Operations['sessions.observe']['result']['preview']>;
type CallPreview = NonNullable<Preview['calls']>[number];
type PresentationPart = MessagePresentation['parts'][number];

/** A display row has no execution authority when cell is null. Source evidence
 * stays in the bounded native snapshots; this pure projection retains no cache. */
export interface ExecutionPresentationRow {
  id: string;
  kind: 'cell' | 'preview' | 'call' | 'attempt' | 'imported';
  state: 'writing' | 'pending' | 'running' | 'succeeded' | 'failed' | 'uncertain' | 'cancelled' | 'recorded';
  code: string;
  cell: CellExecutionRow | null;
  call: CellExecutionRow['call'];
  result: CellExecutionRow['result'];
  preview: DeepReadonly<CallPreview> | null;
  attempt: DeepReadonly<AttemptPresentation> | null;
  part: DeepReadonly<PresentationPart> | null;
  messageID: string | null;
  turnID: string | null;
  startedAt: string | null;
  truncated: boolean;
}
const key = (sessionID: string, attemptID: string, slotID: string) => JSON.stringify([sessionID, attemptID, slotID]);
const callKey = (sessionID: string, messageID: string, callID: string) => JSON.stringify([sessionID, messageID, callID]);
const codeOf = (call: CellExecutionRow['call']) => typeof call?.value.arguments.code === 'string' ? excerpt(call.value.arguments.code) : '';
const codeTruncated = (call: CellExecutionRow['call']) => {
  const code = call?.value.arguments.code;
  return typeof code === 'string' && (code.length > FIELD_BYTES || encoder.encode(code).length > FIELD_BYTES);
};

/** One stable row from first argument fragment to the canonical call/cell/result.
 * Missing presentation falls back to exact canonical call identity; it never
 * suppresses canonical execute calls or invents a persisted cell. */
export function executionPresentationRows(execution: DeepReadonly<ExecutionViewSnapshot>, source: DeepReadonly<SessionViewSnapshot>): ExecutionPresentationRow[] {
  if (source.runtimeID !== execution.runtimeID || source.sessionID !== execution.sessionID) return [];
  const owner = source.sessionID, latest = !execution.latestMissing && !source.history.latestMissing;
  const messages = [...new Map([...(execution.messages ?? []), ...source.history.messages]
    .filter(message => message.session_id === owner && message.retired_by === null && message.retired_revision === null)
    .map(message => [message.id, message])).values()];
  const rows = new Map<string, ExecutionPresentationRow>();
  const cells = cellExecutionRows(execution, source.history.messages);
  const seenCalls = new Set(cells.map(row => callKey(owner, row.cell.call_message_id, row.cell.call_id)));
  // Cell pages arrive newest first by native ordinal. Present reading order.
  for (const cell of [...cells].reverse()) {
    const p = cell.call?.message.presentation;
    const part = p?.parts.find(part => part.type === 'tool_call' && part.call_id === cell.cell.call_id) ?? null;
    rows.set(cell.displayID, { id: cell.displayID, kind: 'cell', state: cell.cell.state, code: codeOf(cell.call), cell, call: cell.call, result: cell.result, preview: null, attempt: null, part,
      messageID: cell.cell.call_message_id, turnID: cell.cell.turn_id, startedAt: cell.cell.created_at, truncated: (p?.truncated ?? false) || codeTruncated(cell.call) });
  }
  const active = source.activity?.active_turn?.id;
  const orderedMessages = [...messages].sort((a, b) => BigInt(a.sequence) < BigInt(b.sequence) ? -1 : 1);
  const recordedResult = (call: DeepReadonly<Message>, callID: string): CellExecutionRow['result'] => {
    // Imported history deliberately has no cell authority. Within the retained
    // group, match the result before any later reuse of the provider call ID.
    let result: CellExecutionRow['result'] = null;
    for (const message of orderedMessages) {
      if (message.group_id !== call.group_id || BigInt(message.sequence) <= BigInt(call.sequence)) continue;
      if (message.parts?.some(part => part.type === 'tool_call' && part.call.id === callID)) break;
      const part = message.role === 'tool' ? message.parts?.find(part => part.type === 'tool_result' && part.result.call_id === callID) : undefined;
      if (part?.type !== 'tool_result') continue;
      if (result || source.history.gaps?.some(gap => BigInt(gap.sequence) > BigInt(call.sequence) && BigInt(gap.sequence) < BigInt(message.sequence))) return null;
      result = { message, value: part.result };
    }
    return result;
  };
  for (const message of messages) {
    const imported = message.turn_id === null;
    if (!imported) {
      const partialTurn = execution.windowBefore === message.turn_id || execution.olderCellCursor?.turnID === message.turn_id;
      if (partialTurn) {
        const retainedSequences = cells.filter(row => row.cell.turn_id === message.turn_id && row.call).map(row => BigInt(row.call!.message.sequence));
        const newerThanRetained = retainedSequences.length > 0 && retainedSequences.every(sequence => BigInt(message.sequence) > sequence);
        if (!latest || !newerThanRetained) continue;
      } else if (!latest && !execution.turns.some(turn => turn.id === message.turn_id)) continue;
    }
    for (const part of message.parts ?? []) {
      if (part.type !== 'tool_call' || part.call.name !== 'execute' || seenCalls.has(callKey(owner, message.id, part.call.id))) continue;
      const presentation = message.presentation, slot = presentation?.parts.find(p => p.type === 'tool_call' && p.call_id === part.call.id) ?? null;
      const id = slot && presentation ? key(owner, presentation.attempt_id, slot.id) : callKey(owner, message.id, part.call.id);
      const call = { message, value: part.call };
      rows.set(id, { id, kind: imported ? 'imported' : 'call', state: !imported && latest && message.turn_id === active ? 'pending' : 'recorded', code: codeOf(call), cell: null, call, result: recordedResult(message, part.call.id), preview: null, attempt: null, part: slot,
        messageID: message.id, turnID: message.turn_id, startedAt: null, truncated: (presentation?.truncated ?? false) || codeTruncated(call) });
    }
  }
  for (const attempt of source.attemptPresentations ?? []) {
    for (const part of attempt.presentation.parts) {
      if (part.type !== 'tool_call' || !part.call || part.call.name !== 'execute') continue;
      const id = key(owner, attempt.attempt_id, part.id);
      rows.set(id, { id, kind: 'attempt', state: attempt.state, code: executionCode(part.call.arguments), cell: null, call: null, result: null, preview: part.call, attempt, part,
        messageID: attempt.message_id ?? null, turnID: attempt.source_session_id ? null : attempt.turn_id, startedAt: null, truncated: attempt.presentation.truncated });
    }
  }
  const preview = latest && source.status === 'live' ? source.preview : null;
  if (preview) {
    const presentation = preview.presentation;
    for (const call of preview.calls ?? []) {
      // A partial name is still a writing row; no executable call is inferred.
      if (call.name !== '' && !'execute'.startsWith(call.name)) continue;
      const slot = presentation?.parts.find(part => part.type === 'tool_call' && part.call_index === call.index) ?? null;
      const id = key(owner, preview.attempt_id, slot?.id ?? `call_${call.index}`);
      if (rows.has(id)) continue;
      rows.set(id, { id, kind: 'preview', state: 'writing', code: executionCode(call.arguments), cell: null, call: null, result: null, preview: call, attempt: null, part: slot,
        messageID: preview.message_id, turnID: preview.turn_id, startedAt: null, truncated: preview.truncated });
    }
  }
  const byID = new Map(messages.map(message => [message.id, message]));
  const groupTail = new Map<string, string>();
  for (const message of messages) if (BigInt(message.sequence) > BigInt(groupTail.get(message.group_id) ?? '0')) groupTail.set(message.group_id, message.sequence);
  const position = (row: ExecutionPresentationRow) => row.call?.message.sequence ?? (row.messageID && byID.get(row.messageID)?.sequence) ?? (row.attempt && groupTail.get(row.attempt.group_id));
  return [...rows.values()].sort((a, b) => {
    const left = position(a), right = position(b);
    if (left && right) { const order = BigInt(left) < BigInt(right) ? -1 : BigInt(left) > BigInt(right) ? 1 : 0; if (order) return order; }
    if (a.kind === 'preview' || b.kind === 'preview') return a.kind === b.kind ? 0 : a.kind === 'preview' ? 1 : -1;
    return left && right && left === right && (a.kind === 'attempt') !== (b.kind === 'attempt') ? a.kind === 'attempt' ? -1 : 1 : 0;
  });
}

const FIELD_BYTES = 64 * 1024;
const PARSE_BYTES = 128 * 1024;
const encoder = new TextEncoder();
const decoder = new TextDecoder();
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

