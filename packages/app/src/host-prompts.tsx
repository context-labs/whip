import { ErrorNotice } from './error-feedback';
import { useCallback, useRef, useSyncExternalStore } from 'react';
import { Button, Dialog, Field, Input } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import type { DesktopBridge, HostPrompt } from './desktop-bridge';
import { layout } from './styles';

const maxAnswers = 8;
const maxAnswerLength = 4_096;
type PendingPrompt = { serial: number; prompt: HostPrompt; busy: boolean; error?: string; errorType?: 'action' | 'validation' };

/** Observe before connecting: an SSH challenge can arrive before React mounts. */
export function createHostPrompts(bridge: DesktopBridge) {
  let pending: PendingPrompt[] = [];
  let serial = 0;
  let disposed = false;
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
      if (pending.some(entry => entry.prompt.id === prompt.id)) return;
      if (pending.length === 4 || !validPrompt(prompt)) {
        notice('An SSH request exceeded the prompt limit and was declined.');
        void send(prompt.id, null).catch(() => notice('An extra SSH request could not be declined. Cancel the connection and try again.'));
        return;
      }
      pending = [...pending, { serial: ++serial, prompt, busy: false }]; emit();
    }
  });
  function dispose() {
    if (disposed) return;
    disposed = true; unsubscribe();
    const remaining = pending; pending = []; emit(); listeners.clear();
    for (const entry of remaining) if (!entry.busy) void send(entry.prompt.id, null).catch(() => {});
  }
  return {
    getSnapshot: () => pending[0] ?? null,
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
        // StrictMode re-subscribes during the same commit; a real unmount cancels.
        queueMicrotask(() => { if (!listeners.size) dispose(); });
      };
    },
    answer(serial: number, values: string[] | null) {
      const current = pending[0];
      if (disposed || !current || current.serial !== serial || current.busy) return;
      if (values && (values.length !== current.prompt.fields.length || values.some(value =>
        new TextEncoder().encode(value).byteLength > maxAnswerLength || /[\x00-\x1f\x7f]/.test(value)))) {
        notice('Each SSH response must fit within 4 KiB and contain no control characters.', 'validation'); return;
      }
      pending = [{ ...current, busy: true, error: undefined }, ...pending.slice(1)]; emit();
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

type HostPromptsController = ReturnType<typeof createHostPrompts>;

export function HostPrompts({ prompts }: { prompts: HostPromptsController }) {
  const active = useSyncExternalStore(prompts.subscribe, prompts.getSnapshot, prompts.getSnapshot);
  return active ? <PromptDialog key={active.serial} active={active} prompts={prompts} /> : null;
}

function PromptDialog({ active, prompts }: { active: PendingPrompt; prompts: HostPromptsController }) {
  const form = useRef<HTMLFormElement>(null);
  const firstInput = useRef<HTMLInputElement>(null);
  const cancelButton = useRef<HTMLButtonElement>(null);
  const { prompt, busy, error, errorType = 'action' } = active;
  const captureForm = useCallback((element: HTMLFormElement | null) => {
    form.current = element;
    return () => { for (const input of element?.querySelectorAll('input') ?? []) input.value = ''; };
  }, []);
  function answer(cancel = false) {
    const inputs = Array.from(form.current?.querySelectorAll('input') ?? []);
    const values = cancel ? null : inputs.map(input => input.value);
    for (const input of inputs) input.value = '';
    prompts.answer(active.serial, values);
  }
  return <Dialog open onOpenChange={open => { if (!open) answer(true); }} title={prompt.title}
    closeLabel="Cancel SSH request" initialFocus={prompt.fields.length ? firstInput : cancelButton}>
    <p {...stylex.props(styles.message)}>{prompt.message}</p>
    <form ref={captureForm} autoComplete="off" {...stylex.props(layout.column)} aria-busy={busy}
      onSubmit={event => { event.preventDefault(); if (prompt.fields.length) answer(); }}>
      {prompt.fields.map((field, index) => <Field key={index} label={field.label}>
        <Input ref={index === 0 ? firstInput : undefined} type={field.secret ? 'password' : 'text'}
          autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false}
          maxLength={maxAnswerLength} disabled={busy} />
      </Field>)}
      {error && <ErrorNotice type={errorType} owner={`ssh-response:${prompt.id}`} title={errorType === 'action' ? error : undefined} error={error} />}
      <div {...stylex.props(layout.row)}>
        <Button type="button" variant="primary" loading={busy} onClick={() => answer()}>{prompt.confirmLabel}</Button>
        <Button ref={cancelButton} type="button" variant="ghost" disabled={busy} onClick={() => answer(true)}>Cancel</Button>
      </div>
    </form>
  </Dialog>;
}

function validPrompt(prompt: HostPrompt) {
  return prompt.title.length <= 256 && prompt.message.length <= 65_536 && prompt.confirmLabel.length <= 128
    && prompt.fields.length <= maxAnswers && prompt.fields.every(field => field.label.length <= 256);
}

const styles = stylex.create({ message: { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: '40dvh', overflowY: 'auto' } });
