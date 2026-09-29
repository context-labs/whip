import type { Admission, ContentReference, Input, InputPageResult, RequestIdentity } from '@whip/protocol';
import type { DesignContext } from './browser-design-presentation';
export interface InputPreview {
  readonly text: string;
  readonly attachment_count: number;
  readonly attachments?: readonly ContentReference[];
  readonly design_context?: DesignContext;
}

export interface SubmittedInput {
  readonly id: string;
  readonly runtimeId: string;
  readonly rootId: string;
  readonly agentId: string;
  readonly clientId?: string;
  readonly text: string;
  readonly sentAt: string;
  readonly inputId?: string;
  readonly accepted: boolean;
  readonly confirmed: boolean;
  readonly queued: boolean;
  readonly preview?: InputPreview;
}

/** Small, window-local submission previews; authoritative input stays in the SDK. */
export class SubmittedInputs {
  private snapshot: readonly SubmittedInput[] = Object.freeze([]);
  private listeners = new Set<() => void>();
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  private publish(inputs: readonly SubmittedInput[]) {
    if (
      inputs.length === this.snapshot.length &&
      inputs.every((item, index) => item === this.snapshot[index])
    )
      return;
    this.snapshot = Object.freeze(inputs);
    for (const listener of this.listeners) listener();
  }
  add(
    scope: Pick<SubmittedInput, 'runtimeId' | 'rootId' | 'agentId' | 'clientId'>,
    text: string,
    queued = false,
    id: string = crypto.randomUUID(),
    preview?: InputPreview,
  ) {
    const input = Object.freeze({
      ...scope,
      text,
      preview,
      queued,
      id,
      sentAt: new Date().toISOString(),
      accepted: false,
      confirmed: false,
    });
    const next = [...this.snapshot, input];
    const bytes = () =>
      new TextEncoder().encode(JSON.stringify(next)).byteLength;
    while (next.length > 32 || bytes() > 1024 * 1024) {
      const oldest = next.findIndex((item) => item.confirmed);
      if (oldest < 0)
        throw new Error(
          'Submission previews are full. Wait for pending messages to be acknowledged before sending more.',
        );
      next.splice(oldest, 1);
    }
    this.publish(next);
    return input.id;
  }
  accept(id: string, inputId?: string, runtimeId?: string) {
    this.publish(
      this.snapshot.map((item) =>
        item.id === id && (!runtimeId || item.runtimeId === runtimeId) && (!item.accepted || item.inputId !== inputId)
          ? Object.freeze({ ...item, accepted: true, inputId })
          : item,
      ),
    );
  }
  acknowledge(receipt: Admission, runtimeId: string) {
    const input = this.snapshot.find(item => item.id === receipt.receipt.identity.request_id && item.runtimeId === runtimeId
      && (!item.clientId || item.clientId === receipt.receipt.identity.client_id));
    if (input && (!receipt.input || receipt.input.session_id === input.agentId)) this.accept(input.id, receipt.receipt.input_id ?? undefined, runtimeId);
  }
  confirm(ids: readonly string[], runtimeId?: string) {
    const confirmed = new Set(ids);
    this.publish(
      this.snapshot.map((item) =>
        confirmed.has(item.id) && (!runtimeId || item.runtimeId === runtimeId) && !item.confirmed
          ? Object.freeze({ ...item, confirmed: true })
          : item,
      ),
    );
  }
  remove(id: string, runtimeId?: string) {
    this.publish(this.snapshot.filter((item) => item.id !== id || !!runtimeId && item.runtimeId !== runtimeId));
  }
  clear() {
    this.publish([]);
  }
}

export type InboxInput = NonNullable<InputPageResult['items']>[number];

export function matchesInput(input: SubmittedInput, item: Pick<InboxInput, 'id' | 'session_id' | 'identity'>) {
  return input.agentId === item.session_id && (input.inputId === item.id || !!item.identity
    && input.clientId === item.identity.client_id && input.id === item.identity.request_id);
}
export function submittedInputId(input: SubmittedInput) {
  return authoredInputId(input.agentId, { client_id: input.clientId ?? '', request_id: input.id });
}
// The caller supplies one runtime-scoped owner window; never match input text.
export function authoredInputId(owner: string, identity: RequestIdentity) {
  return `input:${JSON.stringify([owner, identity.client_id, identity.request_id])}`;
}
export function inboxInputId(item: InboxInput, local?: SubmittedInput) {
  return item.identity ? authoredInputId(item.session_id, item.identity) : local ? submittedInputId(local) : `input:${item.session_id}:${item.id}`;
}
export const isChatInput = (item: InboxInput) => item.kind === 'prompt' && (item.source === 'user' || item.source === 'agent');
export const isAcceptedInputNotice = (item: InboxInput) => !isChatInput(item) && item.source !== 'schedule';
export function admittedText(input: InboxInput | Input): string {
  if ('text_preview' in input) return input.text_preview || (input.attachment_count !== '0' ? `${input.attachment_count} attached files` : 'Input accepted.');
  const text = input.parts.flatMap(part => part.type === 'text' ? [part.text] : []).join('\n');
  const attachments = input.parts.filter(part => part.type === 'content').length;
  return text + (attachments ? `\n${attachments} attached files` : '') || 'Input accepted.';
}
export interface QueuedInputRow {
  id: string;
  text: string;
  status: string;
  item?: InboxInput;
  preview?: InputPreview;
  stale?: boolean;
}
/** Display projection only: lifecycle/replay and paging belong to the SDK. */
export function queuedInputRows(
  inbox: readonly { item: InboxInput; stale: boolean }[], submitted: readonly SubmittedInput[],
  deliveries: ReadonlyMap<string, string> = new Map(),
): QueuedInputRow[] {
  const rows: QueuedInputRow[] = inbox.filter(({ item }) => item.source === 'user' && item.state === 'queued').map(({ item, stale }) => {
    const local = submitted.find(input => matchesInput(input, item));
    return { id: inboxInputId(item, local), text: local?.preview?.text ?? local?.text ?? admittedText(item),
      status: stale ? 'Checking queue…' : item.steering ? 'Steering' : 'Queued', item, preview: local?.preview, stale };
  });
  for (const input of submitted) {
    if (!input.queued || input.confirmed || inbox.some(({ item }) => matchesInput(input, item))) continue;
    rows.push({ id: submittedInputId(input), text: input.preview?.text ?? input.text, preview: input.preview,
      status: deliveries.get(input.id) ?? (input.accepted ? 'Queued' : 'Sending…') });
  }
  return rows;
}
