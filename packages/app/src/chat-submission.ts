import type { Session } from '@whip/sdk';
import type { CompositionAttachment } from './compositions';
import type { AppRuntime, CommandNotice } from './runtime';

export interface ChatSubmission {
  runtime: Pick<AppRuntime, 'compositions' | 'submittedInputs' | 'getSnapshot' | 'run' | 'report'>;
  session: Session;
  runtimeId: string;
  agentId: string;
  /** The author's surface key, not necessarily the destination's normal draft key. */
  compositionKey: string;
  connected: boolean;
  text: string;
  attachments: readonly CompositionAttachment[];
  delivery: 'queued' | 'steer';
  activeTurn?: string;
  /** Clear only this surface's matching text. Called on admission, not completion. */
  onAccepted?: () => void;
}

export type ChatSubmissionResult =
  | { status: 'skipped'; delivery?: CommandNotice['delivery'] }
  | { status: 'completed'; accepted: boolean }
  | { status: 'failed'; error: unknown; accepted: boolean; outcome?: string; delivery?: CommandNotice['delivery'] };

/** Shared chat admission path. Never reads or overwrites a destination's draft. */
export async function submitChatInput({
  runtime, session, runtimeId, agentId, compositionKey: key, connected,
  text, attachments, delivery, activeTurn, onAccepted,
}: ChatSubmission): Promise<ChatSubmissionResult> {
  const unresolved = runtime.getSnapshot().commands.find(command => command.draftKey === key && command.delivery);
  if (unresolved) return { status: 'skipped', delivery: unresolved.delivery };
  if (
    !connected ||
    (!text.trim() && !attachments.length) ||
    attachments.some(item => !item.value)
  ) return { status: 'skipped' };

  let token: symbol | undefined;
  let inputId: string | undefined;
  let accepted = false;
  try {
    token = runtime.compositions.beginSubmission(key);
    if (!token) return { status: 'skipped' };
    const payload = {
      text,
      ...(attachments.length ? { attachments: attachments.map(item => item.value!) } : {}),
    };
    const sentIds = attachments.map(item => item.id);
    inputId = runtime.submittedInputs.add(
      { runtimeId, rootId: session.rootId, agentId, clientId: session.client.clientId },
      text + (attachments.length ? `\n${attachments.length} attached files` : ''),
      !!activeTurn,
      undefined,
      { text, attachment_count: attachments.length, attachments: attachments.map(item => ({ ...item.value!, content: { source: '', media_type: '', ...item.value!.content } })) },
    );
    const command = agentId !== session.rootId
      ? session.command('agent.submit', { id: agentId, ...payload, delivery }, { commandId: inputId })
      : delivery === 'steer' && activeTurn
        ? session.steer(payload, { commandId: inputId })
        : session.submit(payload, { commandId: inputId });
    await runtime.run(command, agentId === session.rootId ? 'Send message' : 'Message child', () => {
      accepted = true;
      try {
        onAccepted?.();
      } catch (error) {
        runtime.report(error);
      }
      runtime.compositions.clear(key, sentIds);
      runtime.compositions.finishSubmission(key, token!);
    }, key);
    return { status: 'completed', accepted };
  } catch (error) {
    const command = runtime.getSnapshot().commands.find(item => item.commandId === inputId && item.runtimeId === runtimeId);
    if (inputId && !command?.delivery) runtime.submittedInputs.remove(inputId, runtimeId);
    // Missing admission is not a rejection: retain the frozen command and authored input.
    // Recovery belongs to runtime.run; never retry here with a new command ID.
    return { status: 'failed', error, accepted, outcome: command?.status, delivery: command?.delivery };
  } finally {
    if (token) runtime.compositions.finishSubmission(key, token);
  }
}
