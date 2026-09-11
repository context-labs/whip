import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { LocalRuntimePanel, LocalRuntimeSetup } from '../src/host-dialog';
import { HostNotice } from '../src/connection-notice';
import type { HostConnection } from '../src/hosts';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { localProfile, type AppLocalRuntime, type LocalRuntimeStatus } from '../src/platform';

const runtimes: AppRuntime[] = [];
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); vi.unstubAllGlobals(); });

const stopped: LocalRuntimeStatus = { state: 'stopped', executable: '/usr/local/bin/whipcode', home: '/Users/test/.whipcode',
  clientBuild: 'local-1234', message: 'whipcode is installed. The local daemon is stopped.', canInstall: false };
const running: LocalRuntimeStatus = { ...stopped, state: 'running', daemonBuild: 'local-1234', message: 'The local daemon is healthy.' };
function deferred<T>() {
  let resolve!: (result: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
function fixture(initial = stopped, desktop = true, onboarding = false, wrapper = false) {
  const api: AppLocalRuntime = {
    test: vi.fn(async () => initial), choose: vi.fn(async () => stopped),
    install: vi.fn(async () => stopped), installDefault: vi.fn(async () => stopped), restart: vi.fn(async () => running),
  };
  const values = new Map<string, string>();
  const runtime = new AppRuntime({
    storage: { keys: () => [...values.keys()], getItem: key => values.get(key) ?? null,
      setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } },
    defaultConnection: localProfile, connectionKinds: ['local', 'url', 'ssh'],
    resolveConnection: vi.fn(async () => { throw new Error('The local executable is unavailable.'); }),
    openExternal: async () => {}, copy: async () => {}, download: async () => 'saved',
    ...(desktop ? { localRuntime: api } : {}),
  });
  runtimes.push(runtime);
  const connect = vi.spyOn(runtime.connections, 'connect').mockResolvedValue();
  const tree = (open: boolean) => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider>
    <QueryClientProvider client={runtime.queries}>{open && desktop && (wrapper ? <LocalRuntimeSetup host={runtime.getSnapshot().home!} />
      : <LocalRuntimePanel api={api} hostState={runtime.getSnapshot().home!.state} disabled={false} onBusyChange={() => {}}
        onboarding={onboarding} onConnect={onboarding ? () => runtime.connections.connect('local') : undefined} />)}</QueryClientProvider>
  </UIProvider></ThemeProvider></RuntimeContext.Provider>;
  const view = render(tree(true));
  return { runtime, api, connect, ...view, reopen() { view.rerender(tree(false)); view.rerender(tree(true)); } };
}

it('makes local setup available before connection and keeps tests separate from starting the daemon', async () => {
  const f = fixture();
  const panel = screen.getByRole('region', { name: 'This Mac runtime' });
  await within(panel).findByText(stopped.message);
  expect(f.runtime.getSnapshot().home?.state).toBe('closed');
  expect(f.api.install).not.toHaveBeenCalled(); expect(f.api.restart).not.toHaveBeenCalled(); expect(f.connect).not.toHaveBeenCalled();
  const probe = deferred<LocalRuntimeStatus>(); vi.mocked(f.api.test).mockReturnValueOnce(probe.promise);
  fireEvent.click(within(panel).getByRole('button', { name: 'Test Connection' }));
  expect(within(panel).getByRole('status').textContent).toContain('Locating whipcode');
  expect((within(panel).getByRole('button', { name: 'Choose executable' }) as HTMLButtonElement).disabled).toBe(true);
  act(() => probe.resolve(stopped)); await within(panel).findByText(stopped.message);
  expect(f.connect).not.toHaveBeenCalled(); expect(f.api.install).not.toHaveBeenCalled();
  const diagnostics = within(panel).getByRole('button', { name: 'Runtime diagnostics' });
  expect(diagnostics.getAttribute('aria-expanded')).toBe('false');
  fireEvent.click(diagnostics);
  expect(diagnostics.getAttribute('aria-expanded')).toBe('true');
  expect(await within(panel).findByText(stopped.executable!)).toBeTruthy();
  expect(within(panel).getByText(stopped.home)).toBeTruthy();
});

it('offers explicit installation for a missing executable and reports a failed native action inline', async () => {
  const f = fixture({ state: 'missing', home: stopped.home, message: 'Choose or install whipcode.', canInstall: true });
  const install = await screen.findByRole('button', { name: 'Install whipcode' });
  expect(f.api.install).not.toHaveBeenCalled();
  vi.mocked(f.api.install).mockRejectedValueOnce(new Error('Cannot write /usr/local/bin. Choose a writable destination.'));
  fireEvent.click(install);
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Cannot write /usr/local/bin'));
  fireEvent.click(screen.getByRole('button', { name: 'Choose executable' }));
  await screen.findByText(stopped.message);
  expect(f.api.choose).toHaveBeenCalledOnce(); expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Install whipcode' })).toBeNull(); expect(f.connect).not.toHaveBeenCalled();
});

it('requires an explicit interruption confirmation before restarting a running daemon', async () => {
  const f = fixture(running);
  const restart = await screen.findByRole('button', { name: 'Restart daemon' });
  fireEvent.click(restart);
  expect(screen.getByText(/Restarting interrupts running work on This Mac/)).toBeTruthy();
  expect(f.api.restart).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Cancel restart' }));
  expect(screen.queryByRole('button', { name: 'Interrupt work and restart' })).toBeNull();
  fireEvent.click(restart);
  fireEvent.click(screen.getByRole('button', { name: 'Interrupt work and restart' }));
  await waitFor(() => expect(f.api.restart).toHaveBeenCalledOnce());
  await screen.findByText(running.message); expect(f.connect).not.toHaveBeenCalled();
});

it('retires a probe when the host dialog closes and preserves the newly opened result', async () => {
  const f = fixture(); await screen.findByText(stopped.message);
  const late = deferred<LocalRuntimeStatus>(); vi.mocked(f.api.test).mockReturnValueOnce(late.promise);
  fireEvent.click(screen.getByRole('button', { name: 'Test Connection' }));
  vi.mocked(f.api.test).mockResolvedValue(running);
  f.reopen(); await screen.findByText(running.message);
  await act(async () => { late.resolve({ ...stopped, message: 'Old probe result' }); await late.promise; });
  expect(screen.queryByText('Old probe result')).toBeNull(); expect(screen.getByText(running.message)).toBeTruthy();
});

it('keeps native repair controls out of shells without the capability', () => {
  const f = fixture(stopped, false);
  expect(screen.queryByRole('region', { name: 'This Mac runtime' })).toBeNull();
  expect(f.api.test).not.toHaveBeenCalled();

});

it('sets up a missing runtime with one action and connects only after installation succeeds', async () => {
  const f = fixture({ state: 'missing', home: stopped.home, message: 'No installation.', canInstall: true }, true, true);
  const button = await screen.findByRole('button', { name: 'Set up this Mac' });
  expect(f.api.installDefault).not.toHaveBeenCalled(); expect(f.connect).not.toHaveBeenCalled();
  expect(screen.queryByRole('button', { name: 'Choose executable' })).toBeNull();
  const install = deferred<LocalRuntimeStatus>(); vi.mocked(f.api.installDefault!).mockReturnValueOnce(install.promise);
  fireEvent.click(button); fireEvent.click(button);
  expect(f.api.installDefault).toHaveBeenCalledOnce(); expect(f.api.install).not.toHaveBeenCalled();
  expect(f.connect).not.toHaveBeenCalled();
  act(() => install.resolve(stopped));
  await waitFor(() => expect(f.connect).toHaveBeenCalledExactlyOnceWith('local'));
});

it('keeps failed installation local, offers retry, and preserves advanced manual installation', async () => {
  const f = fixture({ state: 'missing', home: stopped.home, message: 'No installation.', canInstall: true }, true, true);
  vi.mocked(f.api.installDefault!).mockRejectedValueOnce(new Error('Cannot install here. Choose a writable location.'));
  fireEvent.click(await screen.findByRole('button', { name: 'Set up this Mac' }));
  await screen.findByRole('button', { name: 'Retry setup' });
  expect(screen.getByRole('alert').textContent).toContain('writable location');
  expect(f.connect).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Advanced options' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Choose install location' }));
  await waitFor(() => expect(f.api.install).toHaveBeenCalledOnce());
  await waitFor(() => expect(f.connect).toHaveBeenCalledOnce());
});

it('does not connect when installation is cancelled or discovers an incompatible installation', async () => {
  const missing: LocalRuntimeStatus = { state: 'missing', home: stopped.home, message: 'No installation.', canInstall: true };
  const f = fixture(missing, true, true);
  vi.mocked(f.api.installDefault!).mockResolvedValueOnce(missing);
  fireEvent.click(await screen.findByRole('button', { name: 'Set up this Mac' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Set up this Mac' }).hasAttribute('disabled')).toBe(false));
  expect(f.connect).not.toHaveBeenCalled();
  vi.mocked(f.api.installDefault!).mockResolvedValueOnce({ ...stopped, state: 'incompatible', message: 'Choose a matching backend.' });
  fireEvent.click(screen.getByRole('button', { name: 'Set up this Mac' }));
  await screen.findByText('Choose a matching backend.');
  expect(f.connect).not.toHaveBeenCalled();
  expect(screen.queryByRole('button', { name: 'Set up this Mac' })).toBeNull();
  expect(f.api.restart).not.toHaveBeenCalled();
});

it('retires installation observation on welcome unmount without initiating a late connection', async () => {
  const f = fixture({ state: 'missing', home: stopped.home, message: 'No installation.', canInstall: true }, true, true);
  const install = deferred<LocalRuntimeStatus>(); vi.mocked(f.api.installDefault!).mockReturnValueOnce(install.promise);
  fireEvent.click(await screen.findByRole('button', { name: 'Set up this Mac' }));
  f.unmount();
  await act(async () => { install.resolve(stopped); await install.promise; });
  expect(f.connect).not.toHaveBeenCalled();
});

it('attaches an existing runtime through the normal connection owner without installing or restarting', async () => {
  const f = fixture(running, true, true, true);
  await waitFor(() => expect(f.connect).toHaveBeenCalledExactlyOnceWith('local'));
  expect(f.api.install).not.toHaveBeenCalled(); expect(f.api.installDefault).not.toHaveBeenCalled();
  expect(f.api.restart).not.toHaveBeenCalled();
});

it('leaves an automatic failed probe with the host owner and offers connection retry without diagnostics', async () => {
  const api: AppLocalRuntime = { test: vi.fn().mockRejectedValue(new Error('Diagnostic socket unavailable')), choose: vi.fn(), install: vi.fn(), restart: vi.fn() };
  const onConnect = vi.fn(async () => {});
  const host = { id: 'local', name: 'This Mac', state: 'reconnecting', error: 'Connection socket unavailable' } as HostConnection;
  render(<ThemeProvider initialTheme="light"><UIProvider>
    <HostNotice host={host} onManage={() => {}} />
    <LocalRuntimePanel api={api} hostState={host.state} connectionError={host.error} disabled={false} onBusyChange={() => {}} onboarding onConnect={onConnect} />
  </UIProvider></ThemeProvider>);
  const retry = await screen.findByRole('button', { name: 'Retry connection' });
  await waitFor(() => expect(retry).toHaveProperty('disabled', false));
  expect(document.querySelectorAll('[data-error-type]')).toHaveLength(1);
  expect(document.querySelector('[data-error-type]')?.getAttribute('data-error-type')).toBe('host');
  expect(screen.queryByText('Diagnostic socket unavailable')).toBeNull();
  expect(screen.queryByText('Checking this Mac…')).toBeNull();
  expect(screen.getByText('This Mac is unavailable. Reconnect to continue.')).toBeTruthy();
  fireEvent.click(retry);
  await waitFor(() => expect(onConnect).toHaveBeenCalledOnce());
  fireEvent.click(screen.getByRole('button', { name: 'Advanced options' }));
  const test = await screen.findByRole('button', { name: 'Test Connection' });
  await waitFor(() => expect(test).toHaveProperty('disabled', false));
  fireEvent.click(test);
  await waitFor(() => expect(document.querySelector('[data-error-type="action"]')).not.toBeNull());
  expect(document.querySelectorAll('[data-error-type="host"]')).toHaveLength(1);
  expect(document.querySelector('[data-error-type="action"]')?.textContent).toContain('Diagnostic socket unavailable');
});
it('classifies an automatic diagnostic failure as a resource when no host failure owns it', async () => {
  const api: AppLocalRuntime = { test: vi.fn().mockRejectedValue(new Error('Cannot inspect executable')), choose: vi.fn(), install: vi.fn(), restart: vi.fn() };
  render(<ThemeProvider initialTheme="light"><UIProvider>
    <LocalRuntimePanel api={api} hostState="connected" disabled={false} onBusyChange={() => {}} />
  </UIProvider></ThemeProvider>);
  const notice = await screen.findByRole('alert');
  expect(notice.closest('[data-error-type]')?.getAttribute('data-error-type')).toBe('resource');
  expect(notice.textContent).toContain('Could not load local runtime diagnostics');
  expect(screen.getByText('This Mac is connected. Runtime diagnostics are unavailable.')).toBeTruthy();
  vi.mocked(api.test).mockResolvedValueOnce(running);
  fireEvent.click(screen.getByRole('button', { name: 'Retry diagnostics' }));
  await screen.findByText(running.message);
  expect(screen.queryByRole('alert')).toBeNull();
});
