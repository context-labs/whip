import type { ContentReference, Message, Operations } from '@whip/protocol';
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
  turnId?: string;
  groupId?: string;
  activityBoundary?: string;
  memberIds?: readonly string[];
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

/** The SDK owns history and full replacement previews. This pure projection only
 * groups display rows by recorded exchange/call identity; it reduces no events. */
export function timelineRows(history: DeepReadonly<HistoryView> | undefined, preview?: Preview): TimelineRow[] {
  const rows: TimelineRow[] = [];
  const calls = new Map<string, TimelineRow>();
  const gaps = historyGapRows(history);
  let gapIndex = 0;
  for (const message of history?.messages ?? []) {
    while (gaps[gapIndex] && BigInt(gaps[gapIndex]!.seq!) < BigInt(message.sequence)) {
      rows.push(gaps[gapIndex++]!); calls.clear();
    }
    const id = `message:${message.id}`;
    const base = { seq: message.sequence, turnId: message.turn_id ?? undefined, groupId: message.group_id, sentAt: message.created_at, memberIds: [id] };
    const presentation = messagePresentation(message.parts, message.role === 'user' ? message.design_context : undefined);
    if (message.role === 'assistant') {
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
    } else if (message.role === 'tool') {
      const result = message.parts[0].result;
      const call = calls.get(JSON.stringify([message.group_id, result.call_id]));
      if (call) {
        call.text = result.output; call.references = presentation.references;
        call.memberIds = [...(call.memberIds ?? []), id];
      } else rows.push({ ...base, id, role: 'tool', text: result.output, callId: result.call_id, label: 'Tool output', references: presentation.references });
    } else {
      const role = message.mail ? 'mailbox' : message.role === 'user' && (message.input_id || message.source || message.opening_input) ? 'user' : 'internal';
      const authoredID = message.input_id && message.input_identity ? authoredInputId(message.session_id, message.input_identity) : id;
      rows.push({ ...base, id: authoredID, role, ...presentation });
    }
  }
  rows.push(...gaps.slice(gapIndex));
  if (preview && !history?.messages.some(message => message.id === preview.message_id) && !history?.gaps.some(gap => gap.messageID === preview.message_id)) {
    const id = `message:${preview.message_id}`;
    const base = { live: true, turnId: preview.turn_id, memberIds: [id], truncated: preview.truncated };
    if (preview.reasoning) rows.push({ ...base, id: `${id}:reasoning`, role: 'reasoning', text: preview.reasoning });
    if (preview.text) rows.push({ ...base, id: `${id}:part:0`, role: 'assistant', text: preview.text });
    for (const call of preview.calls ?? []) rows.push({ ...base, id: `${id}:call:${call.id || call.index}`, role: 'tool', text: '',
      callId: call.id, toolName: call.name, label: call.name || 'Preparing tool call', args: call.arguments });
  }
  return rows;
}

/** Local delivery previews disappear only when their exact input is observed.
 * Queued input belongs in the separate queue strip and does not end a response. */
export function conversationRows(
  history: DeepReadonly<HistoryView> | undefined, preview: Preview | undefined,
  inbox: readonly InboxInput[], submitted: readonly SubmittedInput[], deliveries: ReadonlyMap<string, string> = new Map(),
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
  const rows = timelineRows(history, preview);
  const firstLive = rows.findIndex(row => row.live);
  rows.splice(firstLive < 0 ? rows.length : firstLive, 0, ...inputs);
  return rows;
}
