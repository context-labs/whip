import { useCallback, useEffect, useRef } from 'react';
import { Button, Field, Input } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { ErrorNotice } from './error-feedback';
import { maxAnswerLength, type HostPromptsController, type PendingPrompt } from './host-prompt-controller';
import { connectionStyles as styles } from './host-connection.stylex';

export function HostPromptForm({ active, prompts, onCancel }: { active: PendingPrompt; prompts: HostPromptsController; onCancel?(): void }) {
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
    if (cancel && onCancel) onCancel(); else prompts.answer(active.serial, values);
  }
  useEffect(() => { (prompt.fields.length ? firstInput.current : cancelButton.current)?.focus(); }, [active.serial, prompt.fields.length]);
  return <form ref={captureForm} autoComplete="off" {...stylex.props(styles.connection)} aria-busy={busy}
      onSubmit={event => { event.preventDefault(); if (prompt.fields.length) answer(); }}>
      <div {...stylex.props(styles.body)}>
        {onCancel && <p {...stylex.props(styles.promptTitle)}>{prompt.title}</p>}
        <p {...stylex.props(styles.message)}>{prompt.message}</p>
        {prompt.fields.map((field, index) => <Field key={index} label={field.label}>
          <Input ref={index === 0 ? firstInput : undefined} type={field.secret ? 'password' : 'text'}
            autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false}
            maxLength={maxAnswerLength} disabled={busy} />
        </Field>)}
        {error && <ErrorNotice type={errorType} owner={`ssh-response:${prompt.id}`} title={errorType === 'action' ? error : undefined} error={error} />}
      </div>
      <div {...stylex.props(styles.footer)}>
        <Button ref={cancelButton} type="button" variant="ghost" disabled={busy && !onCancel} onClick={() => answer(true)}>Cancel</Button>
        <Button type="button" variant="primary" loading={busy} onClick={() => answer()}>{prompt.confirmLabel}</Button>
      </div>
    </form>;
}
