import { useState } from 'react';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import type { AppRuntime } from '../src/runtime';
import type { HostConnection } from '../src/hosts';
import type { AppPlatform } from '../src/platform';
import { RuntimeContext } from '../src/context';
import { HostDialog, ServerManager } from '../src/host-dialog';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());

function host(id: string, overrides: Partial<HostConnection> = {}): HostConnection {
  return { id, name: id === 'local' ? 'Local' : 'Build server', local: id === 'local', device: false,
    state: 'connected', client: {} as WhipClient, endpoint: `https://${id}.example/api/v3/ws`, runtimeId: `runtime-${id}`, connectOnLaunch: true,
    profile: { id, label: id, runtimeId: `runtime-${id}`, target: { kind: 'url', endpoint: `https://${id}.example` } }, ...overrides };
}
function fixture({ editing, hosts = [host('local'), host('remote')], platform = {}, form = false }: {
  editing?: HostConnection; hosts?: HostConnection[]; platform?: Partial<AppPlatform>; form?: boolean;
} = {}) {
  let snapshot = { hosts, home: hosts[0], profilesReady: true, legacyHosts: [] } as unknown as ReturnType<AppRuntime['getSnapshot']>;
  const listeners = new Set<() => void>();
  const tabs = { previous: [] };
  const connections = {
    refreshProfiles: vi.fn().mockResolvedValue(undefined), save: vi.fn().mockResolvedValue('saved'), saveNative: vi.fn().mockResolvedValue('saved'),
    connect: vi.fn().mockResolvedValue(undefined), select: vi.fn(), disconnect: vi.fn(), remove: vi.fn().mockResolvedValue(undefined),
    rename: vi.fn().mockResolvedValue(undefined),
    getSnapshot: () => ({ legacyProfiles: [] }),
  };
  const runtime = { platform, connections, getSnapshot: () => snapshot, subscribe: (fn: () => void) => { listeners.add(fn); return () => { listeners.delete(fn); }; },
    tabs: { getSnapshot: () => tabs, subscribe: () => () => {} },
  } as unknown as AppRuntime;
  const onSaved = vi.fn();
  function Form() { const [open, setOpen] = useState(true); return <HostDialog open={open} onOpenChange={setOpen} editing={editing} onSaved={onSaved} />; }
  render(<RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider>{form ? <Form /> : <ServerManager />}</UIProvider></ThemeProvider></RuntimeContext.Provider>);
  return { connections, onSaved, publish(patch: Partial<typeof snapshot>) { act(() => { snapshot = { ...snapshot, ...patch }; listeners.forEach(fn => fn()); }); } };
}
async function menu(name: string) { fireEvent.click(screen.getByRole('button', { name: `Actions for ${name}` })); return within(await screen.findByRole('menu')); }
async function add() { fireEvent.click(screen.getByRole('button', { name: 'Add server' })); return within(await screen.findByRole('dialog', { name: 'Add server' })); }

it('shows a quiet list with addresses, status, and no inline form or local tools', () => {
  fixture();
  expect(screen.getByRole('heading', { name: 'Saved servers' })).toBeTruthy();
  expect(screen.getByText('https://remote.example/api/v3/ws')).toBeTruthy();
  expect(screen.getAllByText('Connected')).toHaveLength(2);
  expect(screen.queryByRole('textbox')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Test Connection' })).toBeNull();
  expect(screen.queryByText(/Import desktop addresses/)).toBeNull();
});

it('adds a URL with a derived name, existing auto-connect default, and returns focus', async () => {
  const f = fixture(); const dialog = await add();
  await waitFor(() => expect(document.activeElement).toBe(dialog.getByLabelText('Server address')));
  expect(dialog.queryByRole('tab', { name: 'SSH', exact: true })).toBeNull();
  expect(dialog.queryByLabelText('Username')).toBeNull();
  expect(dialog.getByRole('button', { name: 'Advanced', exact: true }).getAttribute('aria-expanded')).toBe('false');
  fireEvent.change(dialog.getByLabelText('Server address'), { target: { value: 'https://build.example:8443' } });
  fireEvent.click(dialog.getByRole('button', { name: 'Add server' }));
  await waitFor(() => expect(f.connections.save).toHaveBeenCalledWith(expect.objectContaining({ name: 'build.example:8443', url: 'https://build.example:8443', connect_on_launch: true }), false, undefined));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.connections.select).toHaveBeenCalledWith('saved'); expect(f.connections.connect).toHaveBeenCalledWith('saved');
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Add server' })));
});

it('keeps invalid and failed URL submissions open with entered fields and prevents duplicate saves', async () => {
  const f = fixture(); const dialog = await add(); const address = dialog.getByLabelText('Server address');
  fireEvent.change(address, { target: { value: 'not a server address' } });
  fireEvent.click(dialog.getByRole('button', { name: 'Add server' }));
  expect(await screen.findByText('Enter a valid HTTP or HTTPS server address.')).toBeTruthy();
  expect(address.getAttribute('aria-invalid')).toBe('true');
  fireEvent.change(address, { target: { value: 'https://user:secret@build.example' } });
  expect(screen.queryByRole('alert')).toBeNull();
  fireEvent.click(dialog.getByRole('button', { name: 'Add server' }));
  await screen.findByRole('alert'); expect(f.connections.save).not.toHaveBeenCalled();
  expect(screen.getByRole('alert').closest('[data-error-type]')?.getAttribute('data-error-type')).toBe('validation');
  fireEvent.change(address, { target: { value: 'https://build.example' } });
  let reject!: (error: Error) => void;
  f.connections.save.mockImplementationOnce(() => new Promise((_, fail) => { reject = fail; }));
  fireEvent.click(dialog.getByRole('button', { name: 'Add server' }));
  expect((address as HTMLInputElement).disabled).toBe(true);
  fireEvent.submit(address.closest('form')!); expect(f.connections.save).toHaveBeenCalledOnce();
  fireEvent.click(dialog.getByRole('button', { name: 'Close', exact: true })); expect(screen.getByRole('dialog')).toBeTruthy();
  await act(async () => reject(new Error('Configuration changed. Reload and retry.')));
  expect((address as HTMLInputElement).value).toBe('https://build.example');
  expect(screen.getByRole('alert').closest('[data-error-type]')?.getAttribute('data-error-type')).toBe('action');
  expect(screen.getByRole('alert').textContent).toContain('Configuration changed'); expect(f.connections.connect).not.toHaveBeenCalled();
});

it('retains saved URL values and requires explicit identity acceptance', async () => {
  const editing = host('remote', { connectOnLaunch: false }); const f = fixture({ editing, form: true });
  const dialog = within(await screen.findByRole('dialog', { name: 'Edit server' }));
  expect((dialog.getByLabelText('Server name (optional)') as HTMLInputElement).value).toBe('Build server');
  fireEvent.click(dialog.getByText('Advanced'));
  expect(dialog.getByRole('switch', { name: 'Connect when the app opens' }).getAttribute('aria-checked')).toBe('false');
  f.connections.save.mockRejectedValueOnce(new Error('This address serves a different daemon. Accept the new daemon identity to save it.'));
  fireEvent.click(dialog.getByRole('button', { name: 'Save changes' }));
  await screen.findByRole('alert');
  expect(f.connections.save.mock.calls[0][1]).toBe(false);
  fireEvent.click(dialog.getByRole('switch', { name: /Accept a new daemon identity/ }));
  fireEvent.click(dialog.getByRole('button', { name: 'Save changes' }));
  await waitFor(() => expect(f.connections.save).toHaveBeenLastCalledWith(expect.objectContaining({ id: 'remote', runtime_id: 'runtime-remote', connect_on_launch: false }), true, undefined));
  await waitFor(() => expect(f.onSaved).toHaveBeenCalledWith('saved'));
});

it('keeps SSH available without Local and offers scoped setup cancellation', async () => {
  const f = fixture({ hosts: [host('local', { state: 'closed', client: undefined })], platform: { connectionKinds: ['local', 'url', 'ssh'] } });
  const dialog = await add(); expect((dialog.getByRole('button', { name: 'Add server' }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(dialog.getByRole('tab', { name: 'SSH', exact: true }));
  expect(dialog.queryByLabelText('Server address')).toBeNull(); expect(dialog.queryByLabelText(/Password/)).toBeNull();
  fireEvent.change(dialog.getByLabelText('SSH host or alias'), { target: { value: 'build-alias' } });
  let signal!: AbortSignal;
  f.connections.saveNative.mockImplementationOnce((_, __, value: AbortSignal) => new Promise((_, reject) => {
    signal = value; signal.addEventListener('abort', () => reject(new Error('Connection cancelled')), { once: true });
  }));
  fireEvent.click(dialog.getByRole('button', { name: 'Add server' }));
  expect(f.connections.saveNative).toHaveBeenCalledWith(expect.objectContaining({ label: 'build-alias', target: { kind: 'ssh', host: 'build-alias' } }), false, expect.any(AbortSignal));
  fireEvent.click(dialog.getByRole('button', { name: 'Cancel connection' }));
  await screen.findByText('Connection cancelled'); expect(signal.aborted).toBe(true);
  expect(f.connections.save).not.toHaveBeenCalled(); expect((dialog.getByLabelText('SSH host or alias') as HTMLInputElement).value).toBe('build-alias');
  fireEvent.click(dialog.getByRole('button', { name: 'Add server' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});

it('shows connection availability without duplicating host errors and keeps cancellation reachable', async () => {
  const remote = host('remote', { state: 'connecting', client: undefined, progress: 'Opening SSH tunnel…' });
  const f = fixture({ hosts: [host('local'), remote] });
  expect(screen.getByText('Opening SSH tunnel…')).toBeTruthy();
  const actions = await menu('Build server');
  expect(actions.getByRole('menuitem', { name: 'Edit server' }).getAttribute('aria-disabled')).toBe('true');
  fireEvent.click(actions.getByRole('menuitem', { name: 'Cancel connection' })); expect(f.connections.disconnect).toHaveBeenCalledWith('remote');
  f.publish({ hosts: [host('local'), { ...remote, state: 'closed', error: 'Host key was rejected' }] });
  expect(screen.queryByText('Host key was rejected')).toBeNull(); expect(screen.getByText('Disconnected')).toBeTruthy();
  fireEvent.click((await menu('Build server')).getByRole('menuitem', { name: 'Connect' }));
  await waitFor(() => expect(f.connections.select).toHaveBeenCalledWith('remote'));
});

it('does not offer removal for Local and confirms remote removal with a surviving focus target', async () => {
  const f = fixture(); const local = await menu('Local'); expect(local.queryByRole('menuitem', { name: 'Remove server' })).toBeNull();
  fireEvent.click(local.getByRole('menuitem', { name: 'Disconnect' }));
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Cancel', exact: true }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  fireEvent.click((await menu('Build server')).getByRole('menuitem', { name: 'Remove server' }));
  const dialog = within(await screen.findByRole('alertdialog')); expect(f.connections.remove).not.toHaveBeenCalled();
  expect(dialog.getByText(/not daemon sessions/)).toBeTruthy();
  f.connections.remove.mockImplementationOnce(async () => f.publish({ hosts: [host('local')] }));
  fireEvent.click(dialog.getByRole('button', { name: 'Remove server' }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  expect(f.connections.remove).toHaveBeenCalledWith('remote');
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Add server' })));
});

it('closes after a successful save even if connection subsequently fails', async () => {
  const f = fixture({ form: true });
  f.connections.connect.mockRejectedValueOnce(new Error('Server went offline'));
  const dialog = within(await screen.findByRole('dialog', { name: 'Add server' }));
  fireEvent.change(dialog.getByLabelText('Server address'), { target: { value: 'https://build.example' } });
  fireEvent.click(dialog.getByRole('button', { name: 'Add server' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.onSaved).toHaveBeenCalledWith('saved');
  expect(f.connections.select).toHaveBeenCalledWith('saved');
});

it('leaves removal failures visible without closing the confirmation', async () => {
  const f = fixture();
  f.connections.remove.mockRejectedValueOnce(new Error('Local is offline'));
  fireEvent.click((await menu('Build server')).getByRole('menuitem', { name: 'Remove server' }));
  const dialog = within(await screen.findByRole('alertdialog'));
  expect(f.connections.remove).not.toHaveBeenCalled();
  fireEvent.click(dialog.getByRole('button', { name: 'Remove server' }));
  await screen.findByRole('alert');
  expect(screen.getByRole('alertdialog')).toBeTruthy();
  expect(screen.getByText('Build server')).toBeTruthy();
});

it('returns focus to the row trigger when cancelling an edit', async () => {
  fixture();
  fireEvent.click((await menu('Build server')).getByRole('menuitem', { name: 'Edit server' }));
  const dialog = within(await screen.findByRole('dialog', { name: 'Edit server' }));
  fireEvent.click(dialog.getByRole('button', { name: 'Cancel', exact: true }));
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Actions for Build server' })));
});

it('opens local runtime tools only on request and prevents closing during an operation', async () => {
  let resolve!: (value: unknown) => void;
  const test = vi.fn().mockImplementation(() => new Promise(done => { resolve = done; }));
  const f = fixture({ hosts: [host('local', { name: 'This Mac', device: true, profile: { id: 'local', label: 'This Mac', target: { kind: 'local' } } })],
    platform: { localRuntime: { test, choose: vi.fn(), install: vi.fn(), restart: vi.fn() } } });
  expect(test).not.toHaveBeenCalled();
  fireEvent.click((await menu('This Mac')).getByRole('menuitem', { name: 'Local server settings' }));
  const dialog = within(await screen.findByRole('dialog', { name: 'Local server settings' }));
  await waitFor(() => expect(test).toHaveBeenCalledOnce());
  fireEvent.click(dialog.getByRole('button', { name: 'Close', exact: true })); expect(screen.getByRole('dialog')).toBeTruthy();
  await act(async () => resolve({ state: 'stopped', home: '/tmp/whip', message: 'Daemon stopped.' }));
  expect(f.connections.connect).not.toHaveBeenCalled();
  fireEvent.click(dialog.getByRole('button', { name: 'Close', exact: true }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});

it('renames Local from its menu without editing the connection and restores focus', async () => {
  const f = fixture();
  fireEvent.click((await menu('Local')).getByRole('menuitem', { name: 'Rename server' }));
  const dialog = within(await screen.findByRole('dialog', { name: 'Rename server' }));
  fireEvent.change(dialog.getByLabelText('Server name'), { target: { value: 'Studio' } });
  fireEvent.click(dialog.getByRole('button', { name: 'Save name' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(f.connections.rename).toHaveBeenCalledExactlyOnceWith('local', 'Studio');
  expect(f.connections.save).not.toHaveBeenCalled();
  expect(f.connections.connect).not.toHaveBeenCalled();
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Actions for Local' })));
});

it.each(['Local', 'Build server'])('puts Disconnect last and requires confirmation for %s', async name => {
  const f = fixture();
  const actions = await menu(name);
  expect(actions.getAllByRole('menuitem').at(-1)!.textContent).toBe('Disconnect');
  fireEvent.click(actions.getByRole('menuitem', { name: 'Disconnect' }));
  const dialog = within(await screen.findByRole('alertdialog', { name: `Disconnect ${name}?` }));
  expect(f.connections.disconnect).not.toHaveBeenCalled();
  fireEvent.click(dialog.getByRole('button', { name: 'Cancel', exact: true }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  expect(f.connections.disconnect).not.toHaveBeenCalled();
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: `Actions for ${name}` })));
  fireEvent.click((await menu(name)).getByRole('menuitem', { name: 'Disconnect' }));
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Disconnect', exact: true }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  expect(f.connections.disconnect).toHaveBeenCalledExactlyOnceWith(name === 'Local' ? 'local' : 'remote');
  expect(f.connections.remove).not.toHaveBeenCalled();
});
