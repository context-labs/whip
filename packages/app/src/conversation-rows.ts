import type { DeepReadonly, HistoryView } from '@whip/sdk/state';
import type { RootSnapshot, StreamEvent } from '@whip/protocol';
import { admittedText, isChatInput, type InboxInput, type SubmittedInput } from './input-presentation';

export interface TimelineRow {
  id: string;
  role: string;
  text: string;
  label?: string;
  args?: string;
  live?: boolean;
  deliveries?: number;
  body?: NonNullable<HistoryView['messages'][number]['body']>;
  seq?: number;
  images?: ImagePart[];
  sentAt?: string;
  delivery?: string;
  queued?: boolean;
  /** Tool identity is separate from its human-readable label. */
  toolName?: string;
  callId?: string;
  turnId?: string;
  eventSeq?: string;
  activityBoundary?: string;
}
export interface ImagePart {
  url: string;
  width?: number;
  height?: number;
}
/** Also used when inspecting a bounded raw message loaded from content storage. */
export function messagePresentation(content: unknown): {
  text: string;
  images: ImagePart[];
} {
  if (typeof content === 'string') return { text: content, images: [] };
  const text: string[] = [];
  const images: ImagePart[] = [];
  if (Array.isArray(content))
    for (const part of content) {
      if (!part || typeof part !== 'object') continue;
      if (part.type === 'text' && typeof part.text === 'string')
        text.push(part.text);
      else if (
        part.type === 'image_url' &&
        typeof part.image_url?.url === 'string'
      )
        images.push({ url: part.image_url.url, width: part.w, height: part.h });
      else text.push(`[Unsupported content: ${String(part.type)}]`);
    }
  return { text: text.join('\n\n'), images };
}
type Presentation = DeepReadonly<RootSnapshot['presentation']>;

/** Presentation grouping never changes authored messages or daemon history. */
export function timelineRows(
  history: DeepReadonly<HistoryView> | undefined,
  presentation: Presentation | undefined,
): TimelineRow[] {
  const rows: TimelineRow[] = [];
  const calls = new Map<string, TimelineRow>();
  for (const entry of history?.messages ?? []) {
    const message = entry.message;
    const id = `h:${history?.revision}:${entry.seq}`;
    if (!message) {
      rows.push({
        id,
        seq: entry.seq,
        role: entry.role || 'notice',
        text: '',
        sentAt: entry.sent_at ?? undefined,
        ...(entry.body ? { body: entry.body } : {}),
      });
      continue;
    }
    const parts = messagePresentation(message.content);
    const digest =
      message.role === 'user' &&
      !message.authored &&
      parts.text.startsWith('Mailbox digest:');
    const previous = rows.at(-1);
    if (
      digest &&
      previous?.role === 'mailbox' &&
      previous.text === parts.text
    ) {
      previous.deliveries = (previous.deliveries ?? 1) + 1;
      continue;
    }
    if (
      message.role === 'tool' &&
      message.tool_call_id &&
      calls.has(message.tool_call_id)
    ) {
      calls.get(message.tool_call_id)!.text = parts.text;
      calls.get(message.tool_call_id)!.images = parts.images;
      if (entry.body) calls.get(message.tool_call_id)!.body = entry.body;
      continue;
    }
    if (parts.text || parts.images.length || entry.body || (message.role !== 'assistant' && !message.tool_calls?.length))
      rows.push({
        id,
        seq: entry.seq,
        role: digest ? 'mailbox' : message.role,
        text: parts.text,
        images: parts.images,
        sentAt: message.sent_at ?? entry.sent_at ?? undefined,
        ...(message.role === 'tool'
          ? { label: message.name || 'Tool output' }
          : {}),
        ...(message.role === 'tool' ? { toolName: message.name, callId: message.tool_call_id } : {}),
      });
    for (const call of message.tool_calls ?? []) {
      const row: TimelineRow = {
        id: `${id}:${call.id}`,
        seq: entry.seq,
        role: 'tool',
        toolName: call.function.name,
        callId: call.id,
        label:
          call.function.name === 'rlm_exec'
            ? 'Starlark execution'
            : call.function.name,
        args: call.function.arguments,
        text: '',
      };
      calls.set(call.id, row);
      rows.push(row);
    }
  }
  const liveCalls = new Map<string, TimelineRow>();
  let activityBoundary: string | undefined;
  for (const event of presentation ?? []) {
    const payload = event.payload as (StreamEvent & { truncated?: boolean }) | undefined;
    if (!payload || typeof payload !== 'object') continue;
    if (/^(agent\.)?turn\./.test(event.kind) || event.kind === 'scratch.restored') {
      activityBoundary = event.seq;
      liveCalls.clear();
    }
    if (event.kind === 'stream.text' || event.kind === 'stream.reasoning') {
      if (!payload.text?.trim()) continue;
      rows.push({
        id: `live:${event.seq}`,
        role: event.kind === 'stream.text' ? 'assistant' : 'reasoning',
        text: payload.text ?? '',
        live: true,
        turnId: payload.turn_id,
      });
    } else if (event.kind.startsWith('stream.tool.')) {
      if (!payload.id) continue;
      const key = JSON.stringify([payload.turn_id, payload.id]);
      let row = liveCalls.get(key);
      if (row && !row.live && ['stream.tool.call', 'stream.tool.started'].includes(event.kind)) row = undefined;
      if (!row) {
        row = {
          id: `live-tool:${payload.id ?? event.seq}:${event.seq}`,
          role: 'tool',
          toolName: payload.name,
          callId: payload.id,
          turnId: payload.turn_id,
          eventSeq: event.seq,
          activityBoundary,
          label:
            payload.name === 'rlm_exec'
              ? 'Starlark execution'
              : payload.name || 'Tool activity',
          text: '',
          live: true,
        };
        liveCalls.set(key, row);
        rows.push(row);
      }
      if (payload.name) {
        row.toolName = payload.name;
        row.label = payload.name === 'rlm_exec' ? 'Starlark execution' : payload.name;
      }
      if (payload.args !== undefined) row.args = payload.args;
      else if (payload.truncated && ['stream.tool.call', 'stream.tool.started'].includes(event.kind)) row.args = undefined;
      if (payload.result !== undefined) row.text = payload.result;
      else if (
        event.kind === 'stream.tool.output' &&
        payload.text !== undefined
      )
        row.text = payload.text;
      if (event.kind === 'stream.tool.completed') row.live = false;
    } else if (
      event.kind === 'stream.notice' ||
      event.kind === 'stream.terminal.awaiting'
    ) {
      rows.push({
        id: `live:${event.seq}`,
        role: 'notice',
        text:
          event.kind === 'stream.terminal.awaiting'
            ? 'This tool needs interactive terminal input. Open the session in the TUI to respond, or stop the turn.'
            : (payload.text ?? ''),
      });
    }
  }
  return rows;
}

/** Inbox removal and committed history arrive in the same root snapshot. */
export function conversationRows(
  history: DeepReadonly<HistoryView> | undefined,
  presentation: Presentation | undefined,
  inbox: readonly InboxInput[],
  submitted: readonly SubmittedInput[],
  deliveries: ReadonlyMap<string, string> = new Map(),
): TimelineRow[] {
  const inputs: TimelineRow[] = inbox.filter(isChatInput).map((item) => {
    const local = submitted.find((input) => input.inboxSeq === item.seq);
    return {
      id: local ? `input:${local.id}` : `inbox:${item.agent_id}:${item.seq}`,
      role: 'user',
      text: local?.text ?? admittedText(item.kind, item.payload),
      sentAt: local?.sentAt,
      delivery: item.status === 'running' ? undefined : 'Queued',
      queued: item.status !== 'running' || item.kind.startsWith('steer'),
    };
  });
  for (const input of submitted) {
    if (input.confirmed || inbox.some((item) => item.seq === input.inboxSeq))
      continue;
    inputs.push({
      id: `input:${input.id}`,
      role: 'user',
      text: input.text,
      sentAt: input.sentAt,
      delivery:
        deliveries.get(input.id) ?? (input.accepted ? 'Queued' : 'Sending…'),
      queued: input.queued,
    });
  }
  const rows = timelineRows(history, presentation);
  const firstLive = rows.findIndex((row) => row.seq === undefined);
  rows.splice(
    firstLive < 0 ? rows.length : firstLive,
    0,
    ...inputs.filter((row) => !row.queued),
  );
  return [...rows, ...inputs.filter((row) => row.queued)];
}
