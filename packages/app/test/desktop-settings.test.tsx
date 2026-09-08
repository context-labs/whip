import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { Settings } from '../src/settings';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { resolveURLConnection, urlProfile, type AppUpdates, type AppUpdateSnapshot } from '../src/platform';

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => vi.fn(),
}));
const runtimes: AppRuntime[] = [];
beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => { for (const runtime of runtimes.splice(0)) runtime.dispose(); vi.unstubAllGlobals(); });

function fixture(desktop = true) {
  let snapshot: AppUpdateSnapshot = { state: 'idle' };
  const listeners = new Set<() => void>();
  const updates: AppUpdates = {
    currentVersion: '1.2.3', getSnapshot: () => snapshot,
    subscribe(listener) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    check: vi.fn(async () => {}), install: vi.fn(async () => {}),
  };
  const values = new Map<string, string>();
  const runtime = new AppRuntime({
    storage: { keys: () => [...values.keys()], getItem: key => values.get(key) ?? null,
      setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } },
    defaultConnection: urlProfile('https://host.example'), connectionKinds: ['url'], resolveConnection: resolveURLConnection,
    openExternal: async () => {}, copy: async () => {}, download: async () => 'saved', sessionLink: path => path,
    ...(desktop ? { updates, notify: vi.fn(async () => {}) } : {}),
  });
  runtimes.push(runtime);
  const view = render(<RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider>
    <QueryClientProvider client={runtime.queries}><Settings section="device" /></QueryClientProvider>
  </UIProvider></ThemeProvider></RuntimeContext.Provider>);
  return { runtime, updates, listeners, ...view,
    update(next: AppUpdateSnapshot) { act(() => { snapshot = next; for (const listener of listeners) listener(); }); },
  };
}

it('shows the desktop version without checking on mount and exposes restart only after download', async () => {
  const f = fixture();
  const section = screen.getByRole('region', { name: 'Application updates' });
  expect(within(section).getByText('Whip 1.2.3')).toBeTruthy();
  expect(f.updates.check).not.toHaveBeenCalled(); expect(f.updates.install).not.toHaveBeenCalled();
  expect(screen.queryByRole('button', { name: 'Restart to update' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Check for updates' }));
  await waitFor(() => expect(f.updates.check).toHaveBeenCalledOnce());
  f.update({ state: 'checking' }); expect(within(section).getByRole('status').textContent).toContain('Checking');
  f.update({ state: 'available', version: '1.2.4' });
  expect(within(section).getByRole('status').textContent).toBe('Downloading Whip 1.2.4…');
  expect((screen.getByRole('button', { name: 'Check for updates' }) as HTMLButtonElement).disabled).toBe(true);
  expect(screen.queryByRole('button', { name: 'Restart to update' })).toBeNull();
  f.update({ state: 'downloaded', version: '1.2.4' });
  expect(f.updates.install).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Restart to update' }));
  await waitFor(() => expect(f.updates.install).toHaveBeenCalledOnce());
  f.unmount(); expect(f.listeners.size).toBe(0);
});

it('reports action and update errors and clears a stale error when a later native check succeeds', async () => {
  const f = fixture();
  vi.mocked(f.updates.check).mockRejectedValueOnce(new Error('Update service unavailable'));
  fireEvent.click(screen.getByRole('button', { name: 'Check for updates' }));
  await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Update service unavailable'));
  f.update({ state: 'checking' }); expect(screen.queryByRole('alert')).toBeNull();
  f.update({ state: 'error', error: 'The update signature could not be verified' });
  expect(screen.getByRole('alert').textContent).toBe('The update signature could not be verified');
  f.update({ state: 'current' });
  expect(screen.queryByRole('alert')).toBeNull(); expect(screen.getByText('Whip is up to date.')).toBeTruthy();
});

it('keeps a downloaded update available when restart is declined or fails', async () => {
  const f = fixture(); f.update({ state: 'downloaded', version: '1.2.4' });
  vi.mocked(f.updates.install).mockRejectedValueOnce(new Error('Save your draft before restarting'));
  fireEvent.click(screen.getByRole('button', { name: 'Restart to update' }));
  await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Save your draft before restarting'));
  expect((screen.getByRole('button', { name: 'Restart to update' }) as HTMLButtonElement).disabled).toBe(false);
});

it('omits desktop updates from the browser device settings', () => {
  const f = fixture(false);
  expect(screen.getByRole('heading', { name: 'Keyboard and attention' })).toBeTruthy();
  expect(screen.queryByRole('region', { name: 'Application updates' })).toBeNull();
  expect(f.updates.check).not.toHaveBeenCalled(); expect(f.listeners.size).toBe(0);
  expect(screen.queryByRole('switch', { name: /^Desktop notifications/ })).toBeNull();
});

it('defaults native notifications off and saves the explicit device preference', () => {
  const f = fixture(); const control = screen.getByRole('switch', { name: /^Desktop notifications/ });
  expect(control.getAttribute('aria-checked')).toBe('false');
  expect(screen.getByText(/Notifications stop when Whip quits/)).toBeTruthy();
  fireEvent.click(control);
  expect(f.runtime.getSnapshot().preferences.desktopNotifications).toBe(true);
  expect(JSON.parse(f.runtime.platform.storage.getItem('whip.web.preferences.v1')!).desktopNotifications).toBe(true);
});
