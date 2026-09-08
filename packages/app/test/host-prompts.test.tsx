import { StrictMode } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { DesktopBridge, DesktopEvent, HostPrompt } from '../src/desktop-bridge';
import { createHostPrompts, HostPrompts } from '../src/host-prompts';

const controllers: ReturnType<typeof createHostPrompts>[] = [];
afterEach(() => { act(() => { for (const prompts of controllers.splice(0)) prompts.dispose(); }); });

function fixture() {
  const listeners = new Set<(event: DesktopEvent) => void>();
  const bridge = {
    onEvent: vi.fn((listener: (event: DesktopEvent) => void) => {
      listeners.add(listener); return () => { listeners.delete(listener); };
    }),
    answerPrompt: vi.fn<(id: string, values: string[] | null) => Promise<void>>().mockResolvedValue(),
  };
  const prompts = createHostPrompts(bridge as unknown as DesktopBridge);
  controllers.push(prompts);
  return {
    bridge, prompts, listeners,
    emit(event: DesktopEvent) { act(() => { for (const listener of listeners) listener(event); }); },
    mount() { return render(<StrictMode><UIProvider><HostPrompts prompts={prompts} /></UIProvider></StrictMode>); },
  };
}
function prompt(id = 'request', fields: HostPrompt['fields'] = []): HostPrompt {
  return {
    id, attemptId: 'connection', title: `SSH request ${id}`, confirmLabel: fields.length ? 'Send response' : 'Trust host',
    message: 'The host key is not known.\nSHA256:exact-fingerprint\n<img src=x onerror=alert(1)>', fields,
  };
}
function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}

it('retains early prompts under StrictMode and requires an explicit host-key confirmation', async () => {
  const f = fixture();
  f.emit({ kind: 'prompt', prompt: prompt() });
  f.mount();
  expect(f.bridge.onEvent).toHaveBeenCalledOnce();
  const dialog = screen.getByRole('dialog', { name: 'SSH request request' });
  expect(dialog.textContent).toContain(prompt().message);
  expect(dialog.querySelector('img')).toBeNull();
  expect(f.bridge.answerPrompt).not.toHaveBeenCalled();
  fireEvent.submit(dialog.querySelector('form')!);
  expect(f.bridge.answerPrompt).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Trust host' }));
  expect(f.bridge.answerPrompt).toHaveBeenCalledExactlyOnceWith('request', []);
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});

it('cancels host-key prompts through Cancel, the close control and Escape', async () => {
  const f = fixture(); f.mount();
  for (const action of ['cancel', 'close', 'escape']) {
    f.emit({ kind: 'prompt', prompt: prompt(action) });
    const dialog = screen.getByRole('dialog');
    if (action === 'escape') fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
    else fireEvent.click(screen.getByRole('button', { name: action === 'cancel' ? 'Cancel' : 'Cancel SSH request', exact: true }));
    await waitFor(() => expect(f.bridge.answerPrompt).toHaveBeenLastCalledWith(action, null));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  }
});

it('answers a multi-field challenge once and immediately clears bounded secret inputs', async () => {
  const f = fixture(); const completion = deferred();
  f.bridge.answerPrompt.mockReturnValue(completion.promise); f.mount();
  f.emit({ kind: 'prompt', prompt: prompt('challenge', [{ label: 'Password', secret: true }, { label: 'One-time code', secret: false }]) });
  const password = screen.getByLabelText('Password') as HTMLInputElement;
  const code = screen.getByLabelText('One-time code') as HTMLInputElement;
  expect(password.type).toBe('password'); expect(password.autocomplete).toBe('off');
  expect(password.maxLength).toBe(4_096); expect(code.type).toBe('text');
  fireEvent.change(password, { target: { value: 'private password' } });
  fireEvent.change(code, { target: { value: '123456' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send response' }));
  expect(password.value).toBe(''); expect(code.value).toBe('');
  fireEvent.submit(screen.getByRole('dialog').querySelector('form')!);
  expect(f.bridge.answerPrompt).toHaveBeenCalledExactlyOnceWith('challenge', ['private password', '123456']);
  await act(async () => completion.resolve());
  expect(screen.queryByRole('dialog')).toBeNull();
});

it('keeps failed answers visible without retaining the secret or displaying a host error payload', async () => {
  const f = fixture(); f.mount();
  f.bridge.answerPrompt.mockRejectedValueOnce(new Error('sensitive host error content'));
  f.emit({ kind: 'prompt', prompt: prompt('password', [{ label: 'Passphrase', secret: true }]) });
  const input = screen.getByLabelText('Passphrase') as HTMLInputElement;
  fireEvent.change(input, { target: { value: 'private value' } });
  fireEvent.submit(screen.getByRole('dialog').querySelector('form')!);
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('could not be sent'));
  expect(input.value).toBe('');
  expect(document.body.textContent).not.toContain('sensitive host error content');
  fireEvent.click(screen.getByRole('button', { name: 'Cancel', exact: true }));
  expect(f.bridge.answerPrompt).toHaveBeenLastCalledWith('password', null);
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});

it('checks UTF-8 bytes and rejects control characters before sending an answer', () => {
  const f = fixture(); f.mount();
  f.emit({ kind: 'prompt', prompt: prompt('bounded', [{ label: 'Password', secret: true }]) });
  const input = screen.getByLabelText('Password') as HTMLInputElement;
  for (const value of ['é'.repeat(2_049), 'secret\u0001']) {
    fireEvent.change(input, { target: { value } });
    fireEvent.click(screen.getByRole('button', { name: 'Send response' }));
    expect(screen.getByRole('alert').textContent).toContain('4 KiB');
    expect(input.value).toBe(''); expect(f.bridge.answerPrompt).not.toHaveBeenCalled();
  }
});

it('ignores stale answers and dismissals after a newer prompt replaces an in-flight answer', async () => {
  const f = fixture(); f.mount(); const completion = deferred();
  f.bridge.answerPrompt.mockReturnValueOnce(completion.promise);
  f.emit({ kind: 'prompt', prompt: prompt('old', [{ label: 'Old password', secret: true }]) });
  const old = f.prompts.getSnapshot()!;
  const input = screen.getByLabelText('Old password') as HTMLInputElement;
  fireEvent.change(input, { target: { value: 'old secret' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send response' }));
  f.emit({ kind: 'prompt', prompt: prompt('new', [{ label: 'New password', secret: true }]) });
  f.emit({ kind: 'prompt-dismissed', id: 'old' });
  const replacement = screen.getByLabelText('New password') as HTMLInputElement;
  fireEvent.change(replacement, { target: { value: 'new secret' } });
  expect(input.value).toBe('');
  await act(async () => completion.reject(new Error('late failure')));
  f.emit({ kind: 'prompt-dismissed', id: 'old' });
  act(() => f.prompts.answer(old.serial, ['stale answer']));
  expect(screen.getByRole('dialog', { name: 'SSH request new' })).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull(); expect(replacement.value).toBe('new secret');
  expect(f.bridge.answerPrompt).toHaveBeenCalledOnce();
  f.emit({ kind: 'prompt-dismissed', id: 'new' });
  expect(replacement.value).toBe(''); expect(screen.queryByRole('dialog')).toBeNull();
});

it('bounds and deduplicates the queue and cancels unanswered requests on real unmount', async () => {
  const f = fixture(); const mounted = f.mount();
  for (const id of ['one', 'two', 'three', 'four', 'four', 'overflow']) f.emit({ kind: 'prompt', prompt: prompt(id) });
  expect(screen.getAllByRole('dialog')).toHaveLength(1);
  expect(screen.getByRole('alert').textContent).toContain('prompt limit');
  expect(f.bridge.answerPrompt).toHaveBeenCalledExactlyOnceWith('overflow', null);
  await act(async () => mounted.unmount());
  expect(f.listeners.size).toBe(0);
  expect(f.bridge.answerPrompt.mock.calls).toEqual([
    ['overflow', null], ['one', null], ['two', null], ['three', null], ['four', null],
  ]);
  f.emit({ kind: 'prompt', prompt: prompt('after-unmount') });
  expect(f.prompts.getSnapshot()).toBeNull();
});

it('declines oversized messages and challenges without truncating a host fingerprint', async () => {
  const f = fixture(); f.mount();
  f.emit({ kind: 'prompt', prompt: { ...prompt('too-long'), message: 'a'.repeat(65_537) } });
  f.emit({ kind: 'prompt', prompt: prompt('too-many-fields', Array.from({ length: 9 }, () => ({ label: 'Secret', secret: true }))) });
  expect(f.bridge.answerPrompt.mock.calls).toEqual([['too-long', null], ['too-many-fields', null]]);
  expect(screen.queryByRole('dialog')).toBeNull();
});
