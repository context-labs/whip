import type { DeepReadonly } from '@whip/sdk/state';
import type { RootSnapshot } from '@whip/protocol';
import type { CommandOutcome } from '@whip/sdk';

export interface SubmittedInput {
  readonly id: string;
  readonly runtimeId: string;
  readonly rootId: string;
  readonly agentId: string;
  readonly clientId?: string;
  readonly text: string;
  readonly sentAt: string;
  readonly inboxSeq?: string;
  readonly accepted: boolean;
  readonly confirmed: boolean;
  readonly queued: boolean;
  readonly preview?: NonNullable<InboxInput["preview"]>;
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
    preview?: NonNullable<InboxInput["preview"]>,
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
  accept(id: string, inboxSeq?: string, runtimeId?: string) {
    this.publish(
      this.snapshot.map((item) =>
        item.id === id && (!runtimeId || item.runtimeId === runtimeId) && (!item.accepted || item.inboxSeq !== inboxSeq)
          ? Object.freeze({ ...item, accepted: true, inboxSeq })
          : item,
      ),
    );
  }
  acknowledge(receipt: CommandOutcome, runtimeId: string) {
    const input = this.snapshot.find((item) => item.id === receipt.command_id && item.runtimeId === runtimeId);
    if (!input) return;
    if (input.agentId === input.rootId)
      this.accept(input.id, receipt.ingress_seq, runtimeId);
    else if (receipt.result && 'inbox_seq' in receipt.result)
      this.accept(input.id, receipt.result.inbox_seq, runtimeId);
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

export type InboxInput = NonNullable<
  DeepReadonly<RootSnapshot>['inbox']
>[number];

export function matchesInput(input: SubmittedInput, item: InboxInput) {
  return input.agentId === item.agent_id && (input.inboxSeq === item.seq
    || !!input.clientId && input.clientId === item.command_client_id && input.id === item.command_id);
}

export function submittedInputId(input: SubmittedInput) {
  return `input:${input.clientId ? JSON.stringify([input.clientId, input.id]) : input.id}`;
}

export function inboxInputId(item: InboxInput, local?: SubmittedInput) {
  if (item.command_client_id && item.command_id) return `input:${JSON.stringify([item.command_client_id, item.command_id])}`;
  return local ? submittedInputId(local) : `inbox:${item.agent_id}:${item.seq}`;
}
export const isChatInput = (item: InboxInput) =>
  /^(submit|steer)(\.parts)?$/.test(item.kind);

export function admittedText(
  kind: string,
  payload: InboxInput['payload'],
): string {
  let text = payload.text ?? '';
  if (payload.binary) {
    try {
      text = new TextDecoder('utf-8', { fatal: true }).decode(
        Uint8Array.from(atob(payload.binary), (char) => char.charCodeAt(0)),
      );
    } catch {
      return 'Input preview is unavailable.';
    }
  }
  if (kind.endsWith('.parts')) {
    try {
      const input = JSON.parse(text);
      return `${typeof input.text === 'string' ? input.text : ''}${input.attachments?.length ? `\n${input.attachments.length} attached files` : ''}`;
    } catch {
      return 'Structured input preview is unavailable.';
    }
  }
  return (
    text ||
    (payload.reference_id
      ? 'Large input stored on the host.'
      : payload.inline
        ? JSON.stringify(payload.inline)
        : 'Input accepted.')
  );
}

export interface QueuedInputRow {
  id: string;
  text: string;
  status: string;
  item?: InboxInput;
  preview?: NonNullable<InboxInput['preview']>;
  stale?: boolean;
}

/** Display projection only: lifecycle/replay and paging belong to the SDK. */
export function queuedInputRows(
  inbox: readonly { item: InboxInput; stale: boolean }[],
  submitted: readonly SubmittedInput[],
  deliveries: ReadonlyMap<string, string> = new Map(),
): QueuedInputRow[] {
  const rows: QueuedInputRow[] = inbox.filter(({ item }) => item.origin === 'client' && item.status === 'queued').map(({ item, stale }) => {
    const local = submitted.find(input => matchesInput(input, item));
    return {
      id: inboxInputId(item, local),
      text: item.preview?.text ?? local?.preview?.text ?? local?.text ?? admittedText(item.kind, item.payload),
      status: stale ? 'Checking queue…' : item.steer_turn_id || item.kind.startsWith('steer') ? 'Steering…' : 'Queued',
      item, preview: item.preview ?? local?.preview, stale,
    };
  });
  for (const input of submitted) {
    if (!input.queued || input.confirmed || inbox.some(({ item }) => matchesInput(input, item))) continue;
    rows.push({ id: submittedInputId(input), text: input.preview?.text ?? input.text, preview: input.preview,
      status: deliveries.get(input.id) ?? (input.accepted ? 'Queued' : 'Sending…') });
  }
  return rows;
}
