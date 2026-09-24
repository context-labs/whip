import type { DeepReadonly, HistoryGap, HistoryView } from '@whip/sdk/state';
import type { DesignContext } from './browser-design-presentation';
import type { RootSnapshot, StreamEvent } from '@whip/protocol';
import { admittedText, inboxInputId, isChatInput, matchesInput, submittedInputId, type InboxInput, type SubmittedInput } from './input-presentation';

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
  designContext?: DeepReadonly<DesignContext> | null;
  designEvidence?: { context: DeepReadonly<DesignContext>; rawText: string; image?: ImagePart };
  inputAttachments?: NonNullable<InboxInput['preview']>['attachments'];
  sentAt?: string;
  delivery?: string;
  queued?: boolean;
  /** Tool identity is separate from its human-readable label. */
  toolName?: string;
  callId?: string;
  turnId?: string;
  eventSeq?: string;
  activityBoundary?: string;
  memberIds?: readonly string[];
  partId?: string;
  truncated?: boolean;
  /** Original retained prose, supplied once before splitting a message for display. */
  copyText?: string;
  historyGap?: DeepReadonly<HistoryGap>;
}
export interface ImagePart {
  url: string;
  width?: number;
  height?: number;
}
/** Also used when inspecting a bounded raw message loaded from content storage. */
export function messagePresentation(content: unknown, presentation?: unknown): {
  text: string;
  images: ImagePart[];
  designEvidence?: TimelineRow['designEvidence'];
} {
  if (typeof content === 'string') return { text: content, images: [] };
  const text: string[] = [];
  const images: ImagePart[] = [];
  // Only daemon-recorded part indices can separate evidence from authored prose.
  const metadata = presentation as { version?: number; design_context?: DesignContext & { context_part_index: number; screenshot_part_index?: number } } | undefined;
  const design = metadata?.version === 1 ? metadata.design_context : undefined;
  const contextPart = Array.isArray(content) && design && Number.isSafeInteger(design.context_part_index) ? content[design.context_part_index] : undefined;
  const imagePart = Array.isArray(content) && design && Number.isSafeInteger(design.screenshot_part_index) ? content[design.screenshot_part_index!] : undefined;
  const valid = design && Array.isArray(design.elements) && design.elements.length <= 8
    && design.elements.every(element => typeof element?.label === 'string')
    && Number.isSafeInteger(design.element_count) && design.element_count >= design.elements.length && design.element_count <= 1000
    && contextPart?.type === 'text' && typeof contextPart.text === 'string'
    && (design.screenshot_part_index === undefined || (design.screenshot_part_index !== design.context_part_index
      && imagePart?.type === 'image_url' && typeof imagePart.image_url?.url === 'string'));
  const designEvidence = valid ? { context: design, rawText: contextPart.text,
    ...(imagePart ? { image: { url: imagePart.image_url.url, width: imagePart.w, height: imagePart.h } } : {}) } : undefined;
  if (Array.isArray(content))
    for (const [index, part] of content.entries()) {
      if (designEvidence && (index === design!.context_part_index || index === design!.screenshot_part_index)) continue;
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
  return { text: text.join('\n\n'), images, ...(designEvidence ? { designEvidence } : {}) };
}
type Presentation = DeepReadonly<RootSnapshot['presentation']>;

/** Stable within this agent's reading view as pages fill the leading edge. */
export function historyGapRows(history: DeepReadonly<HistoryView> | undefined): TimelineRow[] {
  return (history?.gaps ?? []).map(gap => ({ id: `history-gap:${history!.revision}:${gap.toSeq}`, role: 'history-gap', text: '', seq: gap.fromSeq, historyGap: gap }));
}

/** Presentation grouping never changes authored messages or daemon history. */
export function timelineRows(
  history: DeepReadonly<HistoryView> | undefined,
  presentation: Presentation | undefined,
  rich = false,
): TimelineRow[] {
  const rows: TimelineRow[] = [];
  const calls = new Map<string, TimelineRow>();
  const gaps = historyGapRows(history);
  let gapIndex = 0;
  for (const entry of history?.messages ?? []) {
    while (gaps[gapIndex] && gaps[gapIndex]!.historyGap!.toSeq < entry.seq) {
      rows.push(gaps[gapIndex++]!);
      calls.clear();
    }
    const message = entry.message;
    const id = `h:${history?.revision}:${entry.seq}`;
    // Provider-facing user messages also carry runtime deliveries (including
    // screenshots). Only authored input belongs in the desktop/web user bubble.
    const sourceRole = message?.role ?? entry.role ?? 'notice';
    const role = rich && sourceRole === 'user' && !(message?.authored ?? entry.authored) ? 'internal' : sourceRole;
    const metadata = message?.presentation ?? entry.presentation;
    const orderedParts = rich && metadata?.version === 1 ? metadata.parts ?? [] : [];
    const resultPart = orderedParts.find(part => part.kind === 'result' && part.call_id);
    if (!message && resultPart && calls.has(resultPart.call_id!) && (!calls.get(resultPart.call_id!)!.turnId || calls.get(resultPart.call_id!)!.turnId === metadata!.turn_id)) {
      calls.get(resultPart.call_id!)!.body = entry.body ?? undefined;
      // Pending presentation (e.g. interrupted reasoning) still follows this result.
      for (const part of orderedParts) if (part.kind === 'reasoning' || (part.kind === 'text' && part.text))
        rows.push({ id: `part:${part.id}`, partId: part.id, seq: entry.seq, turnId: metadata!.turn_id, role: part.kind === 'text' ? 'assistant' : 'reasoning', text: part.text ?? '', body: entry.body ?? undefined, truncated: !!part.omitted });
      continue;
    }
    const detailed: TimelineRow[] = [];
    if (orderedParts.length) {
      const prose = messagePresentation(message?.content).text;
      const encoded = new TextEncoder().encode(prose);
      for (const part of orderedParts) {
        const base = { id: `part:${part.id}`, partId: part.id, seq: entry.seq, turnId: metadata!.turn_id, memberIds: [id], truncated: !!(metadata!.omitted || part.omitted) };
        if (part.kind === 'reasoning') detailed.push({ ...base, role: 'reasoning', text: part.text ?? '', body: entry.body ?? undefined });
        else if (part.kind === 'text' && part.text) detailed.push({ ...base, role: 'assistant', text: part.text, copyText: part.text, truncated: true });
        else if (part.kind === 'text' && message?.role === 'assistant' && !metadata!.omitted) {
          detailed.push({ ...base, role: 'assistant', text: new TextDecoder().decode(encoded.slice(part.start ?? 0, part.end ?? 0)) });
        } else if (part.kind === 'tool' && part.call_id) {
          const call = message?.tool_calls?.find(call => call.id === part.call_id);
          const row: TimelineRow = { ...base, role: 'tool', text: '', callId: part.call_id, toolName: call?.function.name ?? part.tool_name,
            args: call?.function.arguments, label: call?.function.name ?? part.tool_name ?? 'Execution', body: entry.body ?? undefined };
          detailed.push(row);
          calls.set(part.call_id, row);
        }
      }
      if (message?.role === 'assistant' || !message) {
        if (metadata!.omitted && prose) detailed.push({ id, seq: entry.seq, role: 'assistant', text: prose });
        if (entry.body && !detailed.some(row => row.body)) detailed.push({ id, seq: entry.seq, role, text: '', body: entry.body ?? undefined });
        const images = messagePresentation(message?.content).images;
        if (images.length) detailed.push({ id: `${id}:images`, seq: entry.seq, role: 'assistant', text: '', images });
        let copied = false;
        for (const row of detailed) if (row.role === 'assistant' && row.copyText === undefined) { row.copyText = copied ? '' : prose; copied = true; }
        rows.push(...detailed);
        continue;
      }
    }
    if (!message) {
      rows.push({
        id,
        seq: entry.seq,
        role,
        text: '',
        sentAt: entry.sent_at ?? undefined,
        ...(entry.body ? { body: entry.body ?? undefined } : {}),
      });
      continue;
    }
    const parts = messagePresentation(message.content, sourceRole === 'user' ? metadata : undefined);
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
      calls.has(message.tool_call_id) &&
      (!rich || !metadata?.turn_id || !calls.get(message.tool_call_id)!.turnId || calls.get(message.tool_call_id)!.turnId === metadata.turn_id)
    ) {
      calls.get(message.tool_call_id)!.text = parts.text;
      calls.get(message.tool_call_id)!.images = parts.images;
      if (entry.body) calls.get(message.tool_call_id)!.body = entry.body;
      rows.push(...detailed);
      continue;
    }
    if (parts.text || parts.images.length || entry.body || (message.role !== 'assistant' && !message.tool_calls?.length))
      rows.push({
        id,
        seq: entry.seq,
        role: digest ? 'mailbox' : role,
        text: parts.text,
        images: parts.images,
        ...(parts.designEvidence ? { designEvidence: parts.designEvidence } : {}),
        sentAt: message.sent_at ?? entry.sent_at ?? undefined,
        ...(message.role === 'tool'
          ? { label: message.name || 'Tool output' }
          : {}),
        ...(message.role === 'tool' ? { toolName: message.name, callId: message.tool_call_id } : {}),
      });
    rows.push(...detailed);
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
    if (event.kind === 'stream.discard') {
      // The stream failed after output and the message is being regenerated:
      // this turn's live text, reasoning and pending tool rows are gone.
      for (let i = rows.length - 1; i >= 0; i--) {
        const row = rows[i];
        if (row.live && row.turnId === payload.turn_id && (row.role === 'assistant' || row.role === 'reasoning' || row.role === 'tool')) rows.splice(i, 1);
      }
      liveCalls.clear();
      continue;
    }
    if (event.kind === 'stream.text' || event.kind === 'stream.reasoning') {
      if (!payload.text?.trim()) continue;
      rows.push({
        id: rich && payload.part_id ? `part:${payload.part_id}` : `live:${event.seq}`,
        ...(rich && payload.part_id ? { partId: payload.part_id } : {}),
        eventSeq: event.seq,
        activityBoundary,
        role: event.kind === 'stream.text' ? 'assistant' : 'reasoning',
        text: payload.text ?? '',
        live: true,
        turnId: payload.turn_id,
      });
    } else if (event.kind.startsWith('stream.tool.')) {
      if (!payload.id) continue;
      const key = JSON.stringify([payload.turn_id, payload.id, ...(rich ? [payload.part_id] : [])]);
      let row = liveCalls.get(key);
      if (!row && rich && !payload.part_id && !['stream.tool.call', 'stream.tool.started'].includes(event.kind)) {
        const candidates = [...liveCalls.values()].filter(item => item.live && item.callId === payload.id && item.turnId === payload.turn_id);
        if (candidates.length === 1) row = candidates[0];
      }
      if (row && !row.live && ['stream.tool.call', 'stream.tool.started'].includes(event.kind)) row = undefined;
      if (!row) {
        row = {
          id: rich && payload.part_id ? `part:${payload.part_id}` : `live-tool:${payload.id ?? event.seq}:${event.seq}`,
          ...(rich && payload.part_id ? { partId: payload.part_id } : {}),
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
  // The SDK deliberately retains separate fragments across gaps or notices.
  // They share a durable part identity, but must not share a React/virtualizer
  // key (or a Markdown cache entry). Keep the part as a reading alias.
  const seen = new Set<string>();
  for (const row of rows) {
    if (row.partId && row.eventSeq && seen.has(row.id)) {
      const part = row.id;
      row.id = `${part}:fragment:${row.eventSeq}`;
      row.memberIds = [...(row.memberIds ?? []), part];
    }
    seen.add(row.id);
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
  rich = false,
  queueStrip = false,
): TimelineRow[] {
  const inputs: TimelineRow[] = inbox.filter(item => isChatInput(item) && !(queueStrip && item.origin === 'client' && item.status === 'queued')).map((item) => {
    const local = submitted.find((input) => matchesInput(input, item));
    return {
      id: queueStrip ? inboxInputId(item, local) : local ? `input:${local.id}` : `inbox:${item.agent_id}:${item.seq}`,
      role: 'user',
      text: (queueStrip ? local?.preview?.text : undefined) ?? local?.text ?? item.preview?.text ?? admittedText(item.kind, item.payload),
      inputAttachments: queueStrip ? item.preview?.attachments ?? local?.preview?.attachments : undefined,
      designContext: queueStrip ? item.preview?.design_context ?? local?.preview?.design_context : undefined,
      eventSeq: queueStrip ? item.delivery_seq : undefined,
      memberIds: [`inbox:${item.agent_id}:${item.seq}`],
      sentAt: local?.sentAt,
      delivery: item.status === 'running' ? undefined : 'Queued',
      queued: item.status !== 'running' || queueStrip && !!item.delivery_seq || item.kind.startsWith('steer'),
    };
  });
  for (const input of submitted) {
    if (input.confirmed || queueStrip && input.queued || inbox.some((item) => matchesInput(input, item)))
      continue;
    inputs.push({
      id: queueStrip ? submittedInputId(input) : `input:${input.id}`,
      role: 'user',
      text: queueStrip ? input.preview?.text ?? input.text : input.text,
      inputAttachments: queueStrip ? input.preview?.attachments : undefined,
      designContext: queueStrip ? input.preview?.design_context : undefined,
      sentAt: input.sentAt,
      delivery:
        deliveries.get(input.id) ?? (input.accepted ? 'Queued' : 'Sending…'),
      queued: input.queued,
    });
  }
  const rows = timelineRows(history, presentation, rich);
  const firstLive = rows.findIndex((row) => row.seq === undefined);
  rows.splice(
    firstLive < 0 ? rows.length : firstLive,
    0,
    ...inputs.filter((row) => !row.queued),
  );
  for (const input of inputs.filter(row => row.queued)) {
    const index = input.eventSeq ? rows.findIndex(row => row.eventSeq && BigInt(row.eventSeq) > BigInt(input.eventSeq!)) : -1;
    rows.splice(index < 0 ? rows.length : index, 0, input);
  }
  return rows;
}
