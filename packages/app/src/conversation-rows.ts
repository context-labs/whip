import type { AttemptPresentation, ContentReference, Message, MessagePresentation, Operations } from '@whip/protocol';
import type { DeepReadonly, HistoryGap, HistoryView } from '@whip/sdk/state';
import type { DesignContext } from './browser-design-presentation';
import { admittedText, authoredInputId, inboxInputId, isChatInput, matchesInput, submittedInputId, type InboxInput, type InputPreview, type SubmittedInput } from './input-presentation';

type Preview = DeepReadonly<Operations['sessions.observe']['result']['preview']>;
export interface TimelineRow {
  id: string;
  role: string;
  text: string;
  label?: string;
  args?: string;
  live?: boolean;
  deliveries?: number;
  body?: ContentReference;
  seq?: string;
  images?: ImagePart[];
  references?: readonly string[];
  designContext?: DeepReadonly<DesignContext> | null;
  inputAttachments?: InputPreview['attachments'];
  sentAt?: string;
  delivery?: string;
  queued?: boolean;
  toolName?: string;
  callId?: string;
  callIndex?: number;
  attemptState?: AttemptPresentation['state'];
  turnId?: string;
  groupId?: string;
  activityBoundary?: string;
  memberIds?: readonly string[];
  memberSeqs?: readonly string[];
  assistantSeq?: string;
  partId?: string;
  truncated?: boolean;
  copyText?: string;
  historyGap?: DeepReadonly<HistoryGap>;
}
export interface ImagePart { url: string; width?: number; height?: number }

/** Display only canonical parts. Design provenance must identify unique recorded
 * content parts; arbitrary text and filenames never establish that provenance. */
export function messagePresentation(parts: DeepReadonly<Message['parts']> | undefined, design?: DeepReadonly<Message['design_context']>) {
  const references = (parts ?? []).flatMap(part => part.type === 'content' ? [part.reference_id] : []);
  const context = design && parts?.[design.context_part_index];
  const screenshot = design?.screenshot_part_index == null ? undefined : parts?.[design.screenshot_part_index];
  const valid = design && context?.type === 'content' && context.reference_id === design.context_attachment_id
    && references.filter(id => id === context.reference_id).length === 1
    && (!design.screenshot_attachment_id ? design.screenshot_part_index == null : screenshot?.type === 'content'
      && screenshot.reference_id === design.screenshot_attachment_id && screenshot.reference_id !== context.reference_id
      && references.filter(id => id === screenshot.reference_id).length === 1);
  return {
    text: (parts ?? []).flatMap(part => part.type === 'text' ? [part.text] : part.type === 'tool_result' ? [part.result.output] : []).join('\n\n'),
    references,
    ...(valid ? { designContext: design } : {}),
  };
}

export function historyGapRows(history: DeepReadonly<HistoryView> | undefined): TimelineRow[] {
  return (history?.gaps ?? []).map(gap => ({ id: `message:${gap.messageID}`, role: 'history-gap', text: '', seq: gap.sequence, historyGap: gap }));
}

/** Optional display metadata orders existing canonical content. It cannot hide,
 * duplicate or replace model-facing text/calls, even when its bounds truncate. */
function orderedRows(owner: string, presentation: DeepReadonly<MessagePresentation> | null | undefined,
  source: TimelineRow[], text: string, base: Omit<TimelineRow, 'role' | 'text'>): TimelineRow[] {
  if (!presentation || presentation.version !== 1) return source;
  const output: TimelineRow[] = [], used = new Set<string>();
  const bytes = new TextEncoder().encode(text), decoder = new TextDecoder('utf-8', { fatal: true });
  const calls = source.filter(row => row.role === 'tool');
  let through = 0;
  const appendText = (id: string, start: number, end: number) => {
    if (end <= start) return;
    try { output.push({ ...base, id, role: 'assistant', text: decoder.decode(bytes.subarray(start, end)) }); }
    catch { return; }
    through = end;
  };
  for (const part of presentation.parts) {
    const id = JSON.stringify([owner, presentation.attempt_id, part.id]);
    if (part.type === 'reasoning' && part.text) output.push({ ...base, id, role: 'reasoning', text: part.text });
    else if (part.type === 'text' && 'start' in part && part.start !== null && part.end !== null &&
      Number.isSafeInteger(part.start) && Number.isSafeInteger(part.end) && part.start >= through && part.end >= part.start && part.end <= bytes.length) {
      appendText(`${base.id}:text:${through}`, through, part.start);
      appendText(id, part.start, part.end);
    } else if (part.type === 'tool_call') {
      const call = calls.find(row => part.call_id ? row.callId === part.call_id : row.callIndex === part.call_index);
      if (call && !used.has(call.id)) {
        used.add(call.id);
        output.push({ ...call, id, memberIds: [...(call.memberIds ?? []), call.id] });
      }
    }
  }
  appendText(`${base.id}:text:${through}`, through, bytes.length);
  output.push(...source.filter(row => row.role === 'tool' ? !used.has(row.id) : row.role === 'assistant' && !!row.references?.length));
  // Original canonical multipart copy is independent of its visual segmentation.
  const copy = source.filter(row => row.role === 'assistant').map(row => row.copyText ?? row.text).join('');
  let copied = false;
  return output.map(row => {
    if (row.role !== 'assistant') return row;
    const copyText = copied ? '' : copy;
    copied = true;
    return { ...row, copyText };
  });
}

function failedAttemptRows(owner: string, attempt: DeepReadonly<AttemptPresentation>): TimelineRow[] {
  const base = { turnId: attempt.source_session_id ? undefined : attempt.turn_id, groupId: attempt.group_id,
    activityBoundary: attempt.attempt_id, attemptState: attempt.state, copyText: '', truncated: attempt.presentation.truncated };
  const rows: TimelineRow[] = [{ ...base, id: JSON.stringify([owner, attempt.attempt_id, 'status']), role: 'internal',
    label: attempt.state === 'cancelled' ? 'Cancelled attempt' : attempt.state === 'uncertain' ? 'Interrupted attempt' : 'Failed attempt',
    text: 'This attempt ended before a completed response was recorded.' }];
  for (const part of attempt.presentation.parts) {
    const id = JSON.stringify([owner, attempt.attempt_id, part.id]);
    if (part.type === 'reasoning' || part.type === 'text' && 'text' in part) {
      if (part.text) rows.push({ ...base, id, role: part.type === 'reasoning' ? 'reasoning' : 'assistant', text: part.text });
    } else if (part.type === 'tool_call' && part.call) rows.push({ ...base, id, role: 'tool', text: '',
      label: 'Unfinished execution', toolName: part.call.name, callId: part.call.id, callIndex: part.call.index, args: part.call.arguments });
  }
  return rows;
}

/** The SDK owns history and full replacement previews. This pure projection only
 * groups display rows by recorded exchange/call identity; it reduces no events. */
export function timelineRows(history: DeepReadonly<HistoryView> | undefined, preview?: Preview,
  attempts: readonly DeepReadonly<AttemptPresentation>[] = []): TimelineRow[] {
  const rows: TimelineRow[] = [];
  const calls = new Map<string, TimelineRow>();
  const gaps = historyGapRows(history);
  let gapIndex = 0;
  const shownAttempts = new Set<string>();
  const appendAttempts = (group: string, message?: string) => {
    const owner = history?.snapshot?.session_id;
    if (!owner) return;
    for (const attempt of attempts) {
      const key = JSON.stringify([group, attempt.attempt_id]);
      if (attempt.group_id !== group || shownAttempts.has(key) || message && attempt.message_id !== message) continue;
      rows.push(...failedAttemptRows(owner, attempt)); shownAttempts.add(key);
    }
  };
  let previousGroup: string | undefined;
  for (const message of history?.messages ?? []) {
    if (previousGroup && previousGroup !== message.group_id) appendAttempts(previousGroup);
    previousGroup = message.group_id;
    if (message.role === 'assistant') {
      appendAttempts(message.group_id, message.id);
      if (message.source) appendAttempts(message.group_id, message.source.message_id);
    }
    while (gaps[gapIndex] && BigInt(gaps[gapIndex]!.seq!) < BigInt(message.sequence)) {
      rows.push(gaps[gapIndex++]!); calls.clear();
    }
    const id = `message:${message.id}`;
    const base = { seq: message.sequence, turnId: message.turn_id ?? undefined, groupId: message.group_id, sentAt: message.created_at, memberIds: [id], assistantSeq: message.role === 'assistant' ? message.sequence : undefined };
    const presentation = messagePresentation(message.parts, message.role === 'user' ? message.design_context : undefined);
    if (message.role === 'assistant') {
      const start = rows.length;
      let copied = false;
      for (const [index, part] of message.parts.entries()) {
        const partID = `${id}:part:${index}`;
        if (part.type === 'text') {
          rows.push({ ...base, id: partID, role: 'assistant', text: part.text, copyText: copied ? '' : presentation.text });
          copied = true;
        } else if (part.type === 'tool_call') {
          const row: TimelineRow = { ...base, id: `${id}:call:${part.call.id}`, role: 'tool', text: '',
            callId: part.call.id, toolName: part.call.name, label: part.call.name === 'execute' ? 'Execution' : part.call.name,
            args: JSON.stringify(part.call.arguments) };
          rows.push(row); calls.set(JSON.stringify([message.group_id, part.call.id]), row);
        } else rows.push({ ...base, id: partID, role: 'assistant', text: '', references: [part.reference_id] });
      }
      const source = rows.splice(start);
      const ordered = orderedRows(message.session_id, message.presentation, source,
        message.parts.flatMap(part => part.type === 'text' ? [part.text] : []).join(''), { ...base, id });
      rows.push(...ordered);
      for (const row of ordered) if (row.role === 'tool' && row.callId) calls.set(JSON.stringify([message.group_id, row.callId]), row);
    } else if (message.role === 'tool') {
      const result = message.parts[0].result;
      const call = calls.get(JSON.stringify([message.group_id, result.call_id]));
      if (call) {
        call.text = result.output; call.references = presentation.references;
        call.memberIds = [...(call.memberIds ?? []), id];
        call.memberSeqs = [...(call.memberSeqs ?? [call.seq!]), message.sequence];
      } else rows.push({ ...base, id, role: 'tool', text: result.output, callId: result.call_id, label: 'Tool output', references: presentation.references });
    } else {
      const role = message.mail ? 'mailbox' : message.role === 'user' && (message.input_id || message.source || message.opening_input) ? 'user' : 'internal';
      const authoredID = message.input_id && message.input_identity ? authoredInputId(message.session_id, message.input_identity) : id;
      rows.push({ ...base, id: authoredID, role, ...presentation });
    }
  }
  if (previousGroup) appendAttempts(previousGroup);
  rows.push(...gaps.slice(gapIndex));
  if (preview && !history?.messages.some(message => message.id === preview.message_id) && !history?.gaps.some(gap => gap.messageID === preview.message_id)) {
    const start = rows.length;
    const id = `message:${preview.message_id}`;
    const base = { live: true, turnId: preview.turn_id, memberIds: [id], truncated: preview.truncated };
    if (preview.reasoning) rows.push({ ...base, id: `${id}:reasoning`, role: 'reasoning', text: preview.reasoning });
    if (preview.text) rows.push({ ...base, id: `${id}:part:0`, role: 'assistant', text: preview.text });
    for (const call of preview.calls ?? []) rows.push({ ...base, id: `${id}:call:${call.id || call.index}`, role: 'tool', text: '',
      callId: call.id, callIndex: call.index, toolName: call.name, label: call.name || 'Preparing tool call', args: call.arguments });
    if (preview.presentation && history?.snapshot) {
      const source = rows.splice(start);
      rows.push(...orderedRows(history.snapshot.session_id, preview.presentation, source, preview.text, { ...base, id }));
    }
  }
  return rows;
}

/** Local delivery previews disappear only when their exact input is observed.
 * Queued input belongs in the separate queue strip and does not end a response. */
export function conversationRows(
  history: DeepReadonly<HistoryView> | undefined, preview: Preview | undefined,
  inbox: readonly InboxInput[], submitted: readonly SubmittedInput[], deliveries: ReadonlyMap<string, string> = new Map(),
  attempts: readonly DeepReadonly<AttemptPresentation>[] = [],
): TimelineRow[] {
  const committed = new Set(history?.messages.flatMap(message => message.input_id ? [message.input_id] : []) ?? []);
  const inputs: TimelineRow[] = inbox.filter(item => isChatInput(item) && item.state === 'claimed' && !committed.has(item.id)).map(item => {
    const local = submitted.find(input => matchesInput(input, item));
    return { id: inboxInputId(item, local), role: item.source === 'user' ? 'user' : 'internal', text: local?.preview?.text ?? local?.text ?? admittedText(item),
      inputAttachments: local?.preview?.attachments, designContext: local?.preview?.design_context, sentAt: local?.sentAt, delivery: 'Accepted' };
  });
  for (const input of submitted) {
    const authored = history?.messages.some(message => message.input_id && matchesInput(input, {
      session_id: message.session_id, id: message.input_id, identity: message.input_identity,
    }));
    if (input.confirmed || input.queued || authored || input.inputId && committed.has(input.inputId) || inbox.some(item => matchesInput(input, item))) continue;
    inputs.push({ id: submittedInputId(input), role: 'user', text: input.preview?.text ?? input.text,
      inputAttachments: input.preview?.attachments, designContext: input.preview?.design_context, sentAt: input.sentAt,
      delivery: deliveries.get(input.id) ?? (input.accepted ? 'Accepted' : 'Sending…') });
  }
  const rows = timelineRows(history, preview, attempts);
  const firstLive = rows.findIndex(row => row.live);
  rows.splice(firstLive < 0 ? rows.length : firstLive, 0, ...inputs);
  return rows;
}
