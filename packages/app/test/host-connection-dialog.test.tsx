import { StrictMode, useState } from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import type { DesktopBridge, DesktopEvent } from '../src/desktop-bridge';
import { createHostPrompts, HostPrompts } from '../src/host-prompts';
import { HostConnectionDialog } from '../src/host-connection-dialog';
import { HostDialog } from '../src/host-dialog';

const controllers: ReturnType<typeof createHostPrompts>[] = [];
afterEach(() => { for (const controller of controllers.splice(0)) controller.dispose(); });
function deferred<T = void>() {
  let resolve!: (value: T) => void, reject!: (error: Error) => void;
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}
function fixture(initial = false, background = false) {
  const listeners = new Set<(event: DesktopEvent) => void>();
  const answerPrompt = vi.fn().mockResolvedValue(undefined);
  const prompts = createHostPrompts({ answerPrompt, onEvent: listener => { listeners.add(listener); return () => { listeners.delete(listener); }; } } as DesktopBridge);
  controllers.push(prompts);
  const profile = { id: 'ssh:gpu', label: 'GPU', target: { kind: 'ssh' as const, host: 'gpu' } };
  const host = { id: profile.id, name: profile.label, profile, progress: 'Preparing SSH…', state: 'connecting', device: true };
  const state = { hosts: initial ? [] : [host], home: { state: 'connected' }, profilesReady: true, legacyHosts: [] };
  const done = deferred();
  let release = () => {};
  let attempt = 0;
  const start = (id: string, signal?: AbortSignal) => {
    release = prompts.registerAttempt(`attempt-${++attempt}`, id);
    signal?.addEventListener('abort', () => { release(); done.reject(new Error('cancelled')); }, { once: true });
    return done.promise;
  };
  const connections = {
    connect: vi.fn((id: string) => start(id)),
    saveNative: vi.fn(async (value: typeof profile, _accept: boolean, signal: AbortSignal) => { await start(value.id, signal); return value.id; }),
    disconnect: vi.fn(() => { release(); done.reject(new Error('cancelled')); }),
    select: vi.fn(), refreshProfiles: vi.fn().mockResolvedValue(undefined), host: () => host,
  };
  const runtime = { platform: { connectionKinds: ['ssh', 'url'], hostPrompts: prompts },
    connections, getSnapshot: () => state, subscribe: () => () => {},
  } as unknown as AppRuntime;
  function App() {
    const [open, setOpen] = useState(true);
    const [request] = useState(() => ({ profile, onCancel: () => setOpen(false), onConnected: () => setOpen(false) }));
    return <RuntimeContext.Provider value={runtime}><UIProvider>
      {initial ? <HostDialog open={open} onOpenChange={setOpen} /> : !background && open && <HostConnectionDialog open title="Connect" request={request} onOpenChange={setOpen} />}
      <HostPrompts prompts={prompts} />
    </UIProvider></RuntimeContext.Provider>;
  }
  render(<StrictMode><App /></StrictMode>);
  return { connections, done, prompts, answerPrompt,
    emit(event: DesktopEvent) { act(() => { for (const listener of listeners) listener(event); }); },
    challenge(id = 'password', fields = [{ label: 'Password', secret: true }]) {
      this.emit({ kind: 'prompt', prompt: { id, attemptId: `attempt-${attempt}`, title: 'Authenticate GPU', message: 'Enter your SSH credentials.', fields, confirmLabel: fields.length ? 'Continue' : 'Trust host' } });
    },
  };
}

it('keeps reconnect progress, repeated authentication and success in one dialog', async () => {
  const f = fixture();
  await waitFor(() => expect(f.connections.connect).toHaveBeenCalledExactlyOnceWith('ssh:gpu'));
  const dialog = screen.getByRole('dialog');
  expect(screen.getByRole('status').textContent).toContain('Preparing SSH');
  f.challenge();
  expect(screen.getAllByRole('dialog')).toHaveLength(1);
  expect(screen.getByRole('dialog')).toBe(dialog);
  expect(screen.queryByRole('status')).toBeNull();
  const input = screen.getByLabelText('Password') as HTMLInputElement;
  expect(document.activeElement).toBe(input);
  fireEvent.change(input, { target: { value: 'secret' } });
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
  expect(input.value).toBe('');
  await waitFor(() => expect(screen.getByRole('status')).toBeTruthy());
  f.challenge('code', [{ label: 'Verification code', secret: false }]);
  expect(screen.getAllByRole('dialog')).toHaveLength(1);
  fireEvent.change(screen.getByLabelText('Verification code'), { target: { value: '123456' } });
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
  await act(async () => { f.done.resolve(); });
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(f.connections.disconnect).not.toHaveBeenCalled();
});

it('shows failure and retries a saved connection without re-saving its profile', async () => {
  const f = fixture();
  await waitFor(() => expect(f.connections.connect).toHaveBeenCalledOnce());
  await act(async () => f.done.reject(new Error('Host did not respond')));
  expect(screen.getByRole('alert').textContent).toContain('Couldn’t connect');
  const next = deferred(); f.connections.connect.mockImplementationOnce(() => next.promise);
  fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
  await waitFor(() => expect(f.connections.connect).toHaveBeenCalledTimes(2));
  expect(screen.queryByRole('alert')).toBeNull();
  await act(async () => next.resolve());
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(f.connections.saveNative).not.toHaveBeenCalled();
});

it('cancels authentication through Escape and retires its prompt', async () => {
  const f = fixture();
  await waitFor(() => expect(f.connections.connect).toHaveBeenCalledOnce());
  f.challenge();
  const input = screen.getByLabelText('Password') as HTMLInputElement;
  fireEvent.change(input, { target: { value: 'secret' } });
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape', code: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(input.value).toBe(''); expect(f.prompts.forHost('ssh:gpu')).toBeNull();
  expect(f.connections.disconnect).toHaveBeenCalledExactlyOnceWith('ssh:gpu');
  expect(f.answerPrompt).not.toHaveBeenCalled();
});

it('initial connection uses the same prompt view and restores its form on cancel', async () => {
  const f = fixture(true);
  fireEvent.click(screen.getByRole('tab', { name: 'SSH' }));
  fireEvent.change(screen.getByLabelText('SSH host or alias'), { target: { value: 'new-gpu' } });
  fireEvent.change(screen.getByLabelText('Server name (optional)'), { target: { value: 'My GPU' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await waitFor(() => expect(f.connections.saveNative).toHaveBeenCalledOnce());
  f.challenge();
  expect(screen.getAllByRole('dialog')).toHaveLength(1);
  const dialog = screen.getByRole('dialog', { name: 'Connect to My GPU' });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel', exact: true }));
  await waitFor(() => expect(screen.getByRole('dialog', { name: 'Add server' })).toBeTruthy());
  expect((screen.getByLabelText('SSH host or alias') as HTMLInputElement).value).toBe('new-gpu');
  expect((screen.getByLabelText('Server name (optional)') as HTMLInputElement).value).toBe('My GPU');
  expect(screen.queryByRole('alert')).toBeNull();
  expect(f.connections.saveNative.mock.calls[0]![2].aborted).toBe(true);
});


it('opens background authentication in the shared dialog and keeps subsequent errors there', async () => {
  const f = fixture(false, true);
  expect(screen.queryByRole('dialog')).toBeNull();
  f.prompts.registerAttempt('attempt-0', 'ssh:gpu');
  f.connections.connect.mockReturnValue(f.done.promise);
  f.challenge();
  await waitFor(() => expect(screen.getByRole('dialog', { name: 'Connect to GPU' })).toBeTruthy());
  await waitFor(() => expect(f.connections.connect).toHaveBeenCalledOnce());
  fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'secret' } });
  fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
  await waitFor(() => expect(screen.getByRole('status')).toBeTruthy());
  await act(async () => f.done.reject(new Error('Whip is not installed')));
  expect(screen.getByRole('alert').textContent).toContain('Couldn’t connect');
  fireEvent.click(screen.getByText('Close', { selector: 'button' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});
