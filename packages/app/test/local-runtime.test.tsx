import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { HostDialog } from '../src/host-dialog';
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
function fixture(initial = stopped, desktop = true) {
  const api: AppLocalRuntime = {
    test: vi.fn(async () => initial), choose: vi.fn(async () => stopped),
    install: vi.fn(async () => stopped), restart: vi.fn(async () => running),
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
    <QueryClientProvider client={runtime.queries}><HostDialog open={open} onOpenChange={() => {}} /></QueryClientProvider>
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
  expect((screen.getByRole('button', { name: 'Connect', exact: true }) as HTMLButtonElement).disabled).toBe(true);
  act(() => probe.resolve(stopped)); await within(panel).findByText(stopped.message);
  expect(f.connect).not.toHaveBeenCalled(); expect(f.api.install).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Connect', exact: true }));
  await waitFor(() => expect(f.connect).toHaveBeenCalledExactlyOnceWith('local'));
  expect(within(panel).getByText(stopped.executable!)).toBeTruthy();
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
  expect(screen.getByRole('button', { name: 'URL', exact: true })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'SSH', exact: true })).toBeTruthy();
});
