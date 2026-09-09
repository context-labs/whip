import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from '@tanstack/react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import type { HostConnection } from '../src/hosts';
import type { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { Settings } from '../src/settings';
import { validateSettingsSearch } from '../src/settings/navigation';

vi.mock('../src/attention', () => ({ Attention: () => null }));
beforeEach(() => { vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })); vi.spyOn(window, 'scrollTo').mockImplementation(() => {}); });
afterEach(() => vi.unstubAllGlobals());

function host(id: string, runtimeId: string) {
  const configuration = { revision: 'v1', default_model: 'model-a', default_provider: 'openrouter', default_effort: 'high', compact_model: 'small', compact_provider: 'inference', compact_percent: 75, goal_max_rounds: 8, max_retries: 2, import_claude: true, import_codex: false };
  const get = vi.fn(async () => configuration);
  const update = vi.fn(async () => configuration);
  const client = { getSnapshot: () => ({ state: 'connected', info: { runtime_id: runtimeId } }), configuration: { get, update } } as unknown as WhipClient;
  return { record: { id, runtimeId, client, name: id === 'local' ? 'Local Mac' : 'Remote A', state: 'connected', profile: { target: { kind: 'url', endpoint: `https://${id}.example` } } } as HostConnection, get, update };
}
function fixture(loading = false) {
  const local = host('local', 'runtime-local'), remote = host('remote', 'runtime-remote');
  let state = { hosts: loading ? [local.record] : [local.record, remote.record], home: local.record, profilesReady: !loading,
    preferences: { commandShortcut: 'Mod+K', composerShortcut: 'Mod+Shift+L', attentionAnnouncements: true, desktopNotifications: false, toolDensity: 'compact' } } as unknown as ReturnType<AppRuntime['getSnapshot']>;
  const listeners = new Set<() => void>();
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const runtime = { queries, platform: {}, lastSession: () => ({ runtimeId: 'runtime-remote', rootId: 'a' }),
    getSnapshot: () => state, subscribe: (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; }, report: vi.fn() } as unknown as AppRuntime;
  const root = createRootRoute({ component: () => <Outlet /> });
  const settings = createRoute({ getParentRoute: () => root, path: '/settings', validateSearch: validateSettingsSearch, component: () => <Settings {...settings.useSearch()} /> });
  const router = createRouter({ routeTree: root.addChildren([settings]), history: createMemoryHistory({ initialEntries: ['/settings?section=execution'] }) });
  render(<RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={queries}><RouterProvider router={router} /></QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>);
  return { local, remote, runtime, queries, publish(hosts: HostConnection[]) { act(() => { state = { ...state, hosts, profilesReady: true }; listeners.forEach(listener => listener()); }); } };
}

it('pins an implicit execution host and keeps its dirty draft when the host disappears', async () => {
  const f = fixture();
  fireEvent.change(await screen.findByLabelText('Retry limit'), { target: { value: '5' } });
  f.publish([f.local.record]);
  expect((screen.getByLabelText('Retry limit') as HTMLInputElement).value).toBe('5');
  expect((screen.getByLabelText('Retry limit') as HTMLInputElement).disabled).toBe(true);
  expect(screen.getByText('Remote A · disconnected')).toBeTruthy();
  expect(f.local.get).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'General', exact: true }));
  fireEvent.click(await screen.findByRole('button', { name: 'Stay' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect((screen.getByLabelText('Retry limit') as HTMLInputElement).value).toBe('5');
  expect(f.remote.update).not.toHaveBeenCalled();
});

it('keeps edits on the old identity disabled until an explicit navigation after host replacement', async () => {
  const f = fixture();
  fireEvent.change(await screen.findByLabelText('Retry limit'), { target: { value: '5' } });
  const replacement = host('remote', 'replacement-runtime');
  f.publish([f.local.record, replacement.record]);
  expect((screen.getByLabelText('Retry limit') as HTMLInputElement).value).toBe('5');
  expect((screen.getByLabelText('Retry limit') as HTMLInputElement).disabled).toBe(true);
  expect(screen.getByText(/The execution host identity changed/)).toBeTruthy();
  expect(replacement.get).not.toHaveBeenCalled();
  expect(replacement.update).not.toHaveBeenCalled();
});

it('waits for remote profiles to resolve before picking an implicit default host', async () => {
  const f = fixture(true);
  await screen.findByRole('heading', { name: 'Agents & execution', exact: true });
  expect(f.local.get).not.toHaveBeenCalled();
  f.publish([f.local.record, f.remote.record]);
  await screen.findByLabelText('Retry limit');
  expect(f.remote.get).toHaveBeenCalledOnce();
  expect(f.local.get).not.toHaveBeenCalled();
});
