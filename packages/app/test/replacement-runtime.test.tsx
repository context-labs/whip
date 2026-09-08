import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { WhipError, type ConnectionSnapshot, type WhipClient } from '@whip/sdk';
import { UIProvider } from '@whip/ui';
import { ConnectionNotice } from '../src/connection-notice';
import { ConnectionDialog } from '../src/connection-dialog';
import { RuntimeContext, useAppState } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { localProfile, urlProfile, type ConnectionProfile } from '../src/connections';

function clientFixture(runtimeId = 'old-runtime', error: Error = new WhipError('runtime_changed', 'The endpoint now serves a different runtime')) {
  const listeners = new Set<() => void>();
  let snapshot = { state: 'incompatible', error, info: { runtime_id: runtimeId, connection_id: 'original' } } as ConnectionSnapshot;
  const client = { getSnapshot: () => snapshot,
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
  } as WhipClient;
  return { client, listeners, update(next: ConnectionSnapshot) {
    act(() => { snapshot = next; for (const listener of listeners) listener(); });
  } };
}

function CurrentNotice() {
  const { client } = useAppState();
  return client && <ConnectionNotice client={client} />;
}

function fixture(dialog = false) {
  const original = clientFixture();
  const profile = { ...urlProfile('https://saved.example'), label: 'Saved host', runtimeId: 'old-runtime' };
  const local = { ...localProfile, runtimeId: 'mac-runtime' };
  const ssh: ConnectionProfile = { id: 'ssh:saved', label: 'Saved SSH', runtimeId: 'ssh-runtime', target: { kind: 'ssh', host: 'saved-alias' } };
  let state = { client: original.client, connection: profile as ConnectionProfile, hosts: [profile, local, ssh] };
  const listeners = new Set<() => void>();
  const connect = vi.fn(async (_profile: ConnectionProfile) => {});
  const report = vi.fn();
  const runtime = { getSnapshot: () => state, connect, report, platform: { connectionKinds: ['local', 'url', 'ssh'] },
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
  } as unknown as AppRuntime;
  const onOpenChange = vi.fn();
  const application = (open = true) => <RuntimeContext.Provider value={runtime}><UIProvider>
    {dialog ? <ConnectionDialog open={open} onOpenChange={onOpenChange} /> : <CurrentNotice />}
  </UIProvider></RuntimeContext.Provider>;
  const view = render(application());
  return { ...view, original, profile, local, ssh, connect, report, listeners, onOpenChange,
    setOpen(open: boolean) { view.rerender(application(open)); },
    get state() { return state; },
    change(next: Partial<typeof state>, emit = true) {
      act(() => { state = { ...state, ...next }; if (emit) for (const listener of listeners) listener(); });
    },
    confirm() {
      fireEvent.click(screen.getByRole('button', { name: 'Connect to replacement runtime' }));
      return screen.getByRole('dialog', { name: 'Connect to replacement runtime?' });
    },
  };
}

it('requires explicit confirmation and removes only runtime identity from the exact selected profile', async () => {
  const f = fixture(); expect(f.connect).not.toHaveBeenCalled();
  const dialog = f.confirm();
  expect(dialog.textContent).toContain('old tabs, drafts and command recovery records remain tied to the old runtime');
  expect(dialog.textContent).toContain('Saved host'); expect(f.connect).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect to replacement runtime' }));
  expect(f.connect).toHaveBeenCalledExactlyOnceWith({ id: f.profile.id, label: f.profile.label, target: f.profile.target });
  expect(f.profile.runtimeId).toBe('old-runtime'); expect(f.state.hosts[0]).toBe(f.profile);
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});

it('cancel and Escape preserve the saved identity without starting a connection', async () => {
  const f = fixture();
  fireEvent.click(within(f.confirm()).getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.keyDown(f.confirm(), { key: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.connect).not.toHaveBeenCalled(); expect(f.state.connection.runtimeId).toBe('old-runtime');
});

it('does not offer replacement for an unrelated incompatibility or an ordinary error message', () => {
  const f = fixture();
  for (const error of [new WhipError('unsupported_protocol', 'Unsupported protocol'), new Error('runtime_changed')]) {
    f.original.update({ state: 'incompatible', error });
    expect(screen.queryByRole('button', { name: 'Connect to replacement runtime' })).toBeNull();
  }
});

it.each(['profile', 'client', 'identity'] as const)('invalidates a confirmation when the captured %s changes', async kind => {
  const f = fixture(); f.confirm();
  if (kind === 'profile') f.change({ connection: { ...f.profile, id: 'another-profile' } });
  if (kind === 'client') f.change({ client: clientFixture().client });
  if (kind === 'identity') { f.profile.runtimeId = 'another-runtime'; f.change({ connection: f.profile }); }
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.connect).not.toHaveBeenCalled();
});

it('rechecks current state at confirmation even before a host-switch render is delivered', () => {
  const f = fixture(); const button = within(f.confirm()).getByRole('button', { name: 'Connect to replacement runtime' });
  f.change({ connection: { ...f.profile, runtimeId: 'newly-selected' } }, false);
  fireEvent.click(button); expect(f.connect).not.toHaveBeenCalled();
});

it('invalidates a confirmation when the SDK observed identity or error changes', async () => {
  const f = fixture(); f.confirm();
  f.original.update({ ...f.original.client.getSnapshot(), info: { runtime_id: 'changed-again' } } as ConnectionSnapshot);
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  f.confirm(); f.original.update({ state: 'connected' });
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull()); expect(f.connect).not.toHaveBeenCalled();
});

it('leaves asynchronous error and cancellation ownership with runtime after a host switch', async () => {
  const f = fixture(); let reject!: (error: Error) => void;
  f.connect.mockImplementationOnce(() => new Promise<void>((_resolve, fail) => { reject = fail; }));
  fireEvent.click(within(f.confirm()).getByRole('button', { name: 'Connect to replacement runtime' }));
  const other = clientFixture('other-runtime');
  f.change({ client: other.client, connection: { ...localProfile, runtimeId: 'other-runtime' } });
  const latestDialog = f.confirm();
  await act(async () => { reject(new Error('Host changed while connecting')); });
  expect(screen.getByRole('dialog')).toBe(latestDialog); expect(f.report).not.toHaveBeenCalled();
  expect(f.connect).toHaveBeenCalledOnce(); expect(f.state.connection.runtimeId).toBe('other-runtime');
  f.unmount(); expect(f.listeners.size).toBe(0); expect(other.listeners.size).toBe(0); expect(f.original.listeners.size).toBe(0);
});

it('releases its observers while a confirmed replacement is still pending', async () => {
  const f = fixture(); let reject!: (error: Error) => void;
  f.connect.mockImplementationOnce(() => new Promise<void>((_resolve, fail) => { reject = fail; }));
  fireEvent.click(within(f.confirm()).getByRole('button', { name: 'Connect to replacement runtime' }));
  f.unmount();
  await act(async () => { reject(new Error('Application has been disposed')); });
  expect(f.listeners.size).toBe(0); expect(f.original.listeners.size).toBe(0);
  expect(f.connect).toHaveBeenCalledOnce(); expect(f.report).not.toHaveBeenCalled();
});

it('keeps remembered local runtime identity when selecting the This Mac connection method', async () => {
  const f = fixture(true);
  fireEvent.click(within(screen.getByRole('group', { name: 'Connection method' })).getByRole('button', { name: 'This Mac' }));
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await waitFor(() => expect(f.connect).toHaveBeenCalledExactlyOnceWith(f.local));
});

it('keeps saved identity when retyping the same URL in a different canonical spelling', async () => {
  const f = fixture(true);
  fireEvent.change(screen.getByRole('textbox', { name: 'Daemon address' }), { target: { value: 'https://SAVED.example:443' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await waitFor(() => expect(f.connect).toHaveBeenCalledExactlyOnceWith(f.profile));
});

it('keeps saved identity and profile ID when retyping the same SSH target', async () => {
  const f = fixture(true);
  fireEvent.click(within(screen.getByRole('group', { name: 'Connection method' })).getByRole('button', { name: 'SSH' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'SSH host or alias' }), { target: { value: 'saved-alias' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await waitFor(() => expect(f.connect).toHaveBeenCalledExactlyOnceWith(f.ssh));
});

it('does not display a retired connection error after a host switch', async () => {
  const f = fixture(true); let reject!: (error: Error) => void;
  f.connect.mockImplementationOnce(() => new Promise<void>((_resolve, fail) => { reject = fail; }));
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  f.change({ connection: f.local });
  await act(async () => { reject(new Error('The old SSH attempt was cancelled')); });
  expect(screen.queryByRole('alert')).toBeNull(); expect(f.onOpenChange).not.toHaveBeenCalled();
  expect((screen.getByRole('button', { name: 'Connect', exact: true }) as HTMLButtonElement).disabled).toBe(false);
});

it.each(['resolve', 'reject'] as const)('keeps a reopened dialog and its new attempt intact when an old attempt settles with %s', async outcome => {
  const f = fixture(true); let resolveOld!: () => void, rejectOld!: (error: Error) => void, resolveNew!: () => void;
  f.connect.mockImplementationOnce(() => new Promise<void>((done, fail) => { resolveOld = done; rejectOld = fail; }))
    .mockImplementationOnce(() => new Promise<void>(done => { resolveNew = done; }));
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  f.setOpen(false); f.setOpen(true);
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  expect(f.connect).toHaveBeenCalledTimes(2);
  await act(async () => { if (outcome === 'resolve') resolveOld(); else rejectOld(new Error('Old failure')); });
  expect(f.onOpenChange).not.toHaveBeenCalled(); expect(screen.queryByRole('alert')).toBeNull();
  expect((screen.getByRole('dialog').querySelector('button[type="submit"]') as HTMLButtonElement).disabled).toBe(true);
  await act(async () => { resolveNew(); }); expect(f.onOpenChange).toHaveBeenCalledExactlyOnceWith(false);
});

it('keeps connection-form validation errors visible without starting runtime work', () => {
  const f = fixture(true);
  fireEvent.change(screen.getByRole('textbox', { name: 'Daemon address' }), { target: { value: 'javascript:alert(1)' } });
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  expect(screen.getByRole('alert').textContent).toContain('HTTP or WebSocket'); expect(f.connect).not.toHaveBeenCalled();
});
