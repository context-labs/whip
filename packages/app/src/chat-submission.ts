import type { Operations, Session } from '@whip/sdk';
import type { CompositionAttachment } from './compositions';
import type { AppRuntime, CommandNotice } from './runtime';

export interface ChatSubmission {
  runtime: Pick<AppRuntime, 'compositions' | 'submittedInputs' | 'getSnapshot' | 'run' | 'report' | 'recovery'>;
  session: Session;
  runtimeId: string;
  rootId: string;
  agentId: string;
  /** The author's surface key, not necessarily the destination's normal draft key. */
  compositionKey: string;
  connected: boolean;
  text: string;
  attachments: readonly CompositionAttachment[];
  designContext?: Operations['sessions.submit']['params']['design_context'];
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
  runtime, session, runtimeId, rootId, agentId, compositionKey: key, connected,
  text, attachments, designContext, delivery, activeTurn, onAccepted,
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
    if (session.id !== agentId || session.client.runtimeID !== runtimeId) throw new Error('The message recipient changed. Select the original conversation again.');
    if (attachments.some(item => item.value!.session_id !== agentId)) throw new Error('An attachment belongs to another conversation recipient. Upload it to this recipient before sending.');
    const parts: Operations['sessions.submit']['params']['parts'][number][] = [
      ...(text ? [{ type: 'text' as const, text }] : []),
      ...attachments.map(item => ({ type: 'content' as const, reference_id: item.value!.id })),
    ];
    const first = parts[0];
    if (!first) return { status: 'skipped' };
    const sentIds = attachments.map(item => item.id);
    inputId = runtime.submittedInputs.add(
      { runtimeId, rootId, agentId, clientId: session.client.clientID },
      text + (attachments.length ? `\n${attachments.length} attached files` : ''),
      !!activeTurn,
      undefined,
      { text, ...(designContext ? { design_context: designContext } : {}), attachment_count: attachments.length, attachments: attachments.map(item => ({ ...item.value! })) },
    );
    const steering = delivery === 'steer' && !!activeTurn;
    const command = session.submission([first, ...parts.slice(1)], inputId, {
      journal: runtime.recovery,
      ...(designContext ? { designContext } : {}),
      delivery: steering ? 'steer' : 'queued',
      ...(steering ? { targetTurnID: activeTurn } : {}),
    });
    await runtime.run(command, agentId === rootId ? 'Send message' : 'Message child', () => {
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
