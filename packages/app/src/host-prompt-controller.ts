import type { DesktopBridge, HostPrompt } from './desktop-bridge';

const maxAnswers = 8;
export const maxAnswerLength = 4_096;
export type PendingPrompt = { hostId?: string; serial: number; prompt: HostPrompt; busy: boolean; error?: string; errorType?: 'action' | 'validation' };

/** Observe before connecting: an SSH challenge can arrive before React mounts. */
export function createHostPrompts(bridge: DesktopBridge) {
  let pending: PendingPrompt[] = [];
  let serial = 0;
  let disposed = false;
  let managedAttempts = false;
  const attempts = new Map<string, string>();
  const claimed = new Map<string, number>();
  const listeners = new Set<() => void>();
  const emit = () => { for (const listener of listeners) listener(); };
  const send = (id: string, values: string[] | null) => {
    try { return Promise.resolve(bridge.answerPrompt(id, values)); }
    catch (error) { return Promise.reject(error); }
  };
  const notice = (error: string, errorType: 'action' | 'validation' = 'action') => {
    if (disposed || !pending[0]) return;
    pending = [{ ...pending[0], error, errorType }, ...pending.slice(1)]; emit();
  };
  const unsubscribe = bridge.onEvent(event => {
    if (disposed) return;
    if (event.kind === 'prompt-dismissed') {
      pending = pending.filter(entry => entry.prompt.id !== event.id); emit();
    } else if (event.kind === 'prompt') {
      const prompt = event.prompt;
      if (managedAttempts && !attempts.has(prompt.attemptId)) { void send(prompt.id, null).catch(() => {}); return; }
      if (pending.some(entry => entry.prompt.id === prompt.id)) return;
      if (pending.length === 4 || !validPrompt(prompt)) {
        notice('An SSH request exceeded the prompt limit and was declined.');
        void send(prompt.id, null).catch(() => notice('An extra SSH request could not be declined. Cancel the connection and try again.'));
        return;
      }
      pending = [...pending, { serial: ++serial, prompt, hostId: attempts.get(prompt.attemptId), busy: false }]; emit();
    }
  });
  function dispose() {
    if (disposed) return;
    disposed = true; unsubscribe();
    const remaining = pending; pending = []; attempts.clear(); claimed.clear(); emit(); listeners.clear();
    for (const entry of remaining) if (!entry.busy) void send(entry.prompt.id, null).catch(() => {});
  }
  return {
    getSnapshot: () => claimed.size ? null : pending[0] ?? null,
    isClaimed: (hostId: string) => claimed.has(hostId),
    forHost: (hostId: string) => pending.find(entry => entry.hostId === hostId) ?? null,
    registerAttempt(attemptId: string, hostId: string) {
      managedAttempts = true;
      attempts.set(attemptId, hostId);
      return () => {
        attempts.delete(attemptId);
        pending = pending.filter(entry => entry.prompt.attemptId !== attemptId); emit();
      };
    },
    claim(hostId: string) {
      claimed.set(hostId, (claimed.get(hostId) ?? 0) + 1); emit();
      return () => {
        const count = (claimed.get(hostId) ?? 1) - 1;
        if (count) claimed.set(hostId, count); else claimed.delete(hostId);
        emit();
      };
    },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
        // StrictMode re-subscribes during the same commit; a real unmount cancels.
        queueMicrotask(() => { if (!listeners.size) dispose(); });
      };
    },
    answer(serial: number, values: string[] | null) {
      const current = pending.find(entry => entry.serial === serial);
      if (disposed || !current || current.busy) return;
      if (values && (values.length !== current.prompt.fields.length || values.some(value =>
        new TextEncoder().encode(value).byteLength > maxAnswerLength || /[\x00-\x1f\x7f]/.test(value)))) {
        pending = pending.map(entry => entry === current ? { ...entry, error: 'Each SSH response must fit within 4 KiB and contain no control characters.', errorType: 'validation' } : entry); emit(); return;
      }
      pending = pending.map(entry => entry === current ? { ...entry, busy: true, error: undefined } : entry); emit();
      void send(current.prompt.id, values).then(() => {
        if (disposed) return;
        pending = pending.filter(entry => entry.serial !== serial); emit();
      }, () => {
        if (disposed) return;
        pending = pending.map(entry => entry.serial === serial ? {
          ...entry, busy: false, errorType: 'action', error: 'The SSH response could not be sent. Enter it again or cancel the request.',
        } : entry); emit();
      });
    },
    dispose,
  };
}

export type HostPromptsController = ReturnType<typeof createHostPrompts>;

function validPrompt(prompt: HostPrompt) {
  return prompt.title.length <= 256 && prompt.message.length <= 65_536 && prompt.confirmLabel.length <= 128
    && prompt.fields.length <= maxAnswers && prompt.fields.every(field => field.label.length <= 256);
}
