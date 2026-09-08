import type { ReactNode } from 'react';
import { act, render } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { AppShell } from '../src/shell';
import { localProfile, resolveURLConnection } from '../src/platform';

const routing = vi.hoisted(() => ({ location: { pathname: '/h/mac/s/a' }, navigate: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useLocation: () => routing.location, useParams: () => ({}), useNavigate: () => routing.navigate,
}));
vi.mock('@tanstack/react-hotkeys', () => ({ useHotkey: () => {} }));
vi.mock('../src/session-sidebar', () => ({ SessionSidebar: () => null }));
vi.mock('../src/session-search-dialog', () => ({ SessionSearchDialog: () => null }));
vi.mock('../src/connection-dialog', () => ({ ConnectionDialog: () => null }));
vi.mock('../src/connection-notice', () => ({ ConnectionNotice: () => null }));
vi.mock('../src/conversation', () => ({ SessionContent: () => null }));
vi.mock('../src/workspace-views', () => ({ useWorkspaceViews: () => ({ views: new Map(), errors: new Map() }) }));
// Exercise the shell, imperative tab action and real tab store; layout geometry has separate browser tests.
vi.mock('@whip/ui/workspace-tabs', () => ({ WorkspaceTabs: ({ utilities }: { utilities: ReactNode }) => <div>{utilities}</div>, workspaceTabId: (id: string) => `tab-${id}` }));
vi.mock('@whip/ui/workspace-layout', () => ({ WorkspaceLayout: () => null, workspacePanelId: (id: string) => `panel-${id}` }));
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  routing.location = { pathname: '/h/mac/s/a' };
  routing.navigate.mockReset().mockImplementation(async ({ to, params }: { to: string; params?: { runtimeId: string; rootId: string } }) => {
    routing.location = { pathname: params ? `/h/${params.runtimeId}/s/${params.rootId}` : to };
  });
});
afterEach(() => vi.unstubAllGlobals());

function fixture(path = '/h/mac/s/a', roots = ['a', 'b']) {
  const listeners = new Set<() => void>();
  const hideWindow = vi.fn();
  const disk = new Map<string, string>();
  const runtime = new AppRuntime({ defaultConnection: localProfile, connectionKinds: ['local'], resolveConnection: resolveURLConnection,
    storage: { keys: () => [...disk.keys()], getItem: key => disk.get(key) ?? null, setItem: (key, value) => { disk.set(key, value); }, removeItem: key => { disk.delete(key); } },
    copy: async () => {}, openExternal: async () => {}, download: async () => 'saved', sessionLink: value => value,
    onCloseTab(listener) { listeners.add(listener); return () => { listeners.delete(listener); }; }, hideWindow,
  });
  const connected = { state: 'connected', info: { runtime_id: 'mac', negotiated_capabilities: [] } };
  const client = { subscribe: () => () => {}, getSnapshot: () => connected, close: vi.fn() } as unknown as WhipClient;
  vi.spyOn(runtime, 'getSnapshot').mockReturnValue({ ...runtime.getSnapshot(), client });
  for (const root of roots) runtime.tabs.open('mac', root);
  if (roots.includes('a')) runtime.tabs.visit('mac', 'a', {});
  runtime.setDraft('mac:a:a', 'Keep this draft');
  routing.location = { pathname: path };
  const view = render(<RuntimeContext.Provider value={runtime}><UIProvider><QueryClientProvider client={runtime.queries}>
    <AppShell><p>Current route</p></AppShell>
  </QueryClientProvider></UIProvider></RuntimeContext.Provider>);
  return { runtime, client, hideWindow, listeners,
    closeTab() { act(() => { for (const listener of listeners) listener(); }); },
    dispose() { view.unmount(); runtime.dispose(); },
  };
}

it('Cmd-W closes the focused session tab through the existing action and preserves work and drafts', async () => {
  const f = fixture();
  f.closeTab();
  expect(f.runtime.tabs.workspace('mac').tabs.map(tab => tab.rootId)).toEqual(['b']);
  expect(f.runtime.tabs.workspace('mac').closed[0]?.tab.rootId).toBe('a');
  expect(f.runtime.draft('mac:a:a')).toBe('Keep this draft');
  expect(f.hideWindow).not.toHaveBeenCalled(); expect(f.client.close).not.toHaveBeenCalled();
  expect(routing.navigate).toHaveBeenCalledWith(expect.objectContaining({ params: { runtimeId: 'mac', rootId: 'b' }, replace: true }));
  f.dispose(); expect(f.listeners.size).toBe(0);
});

it('Cmd-W returns to New session after the final tab, then hides the window on the next command', () => {
  const f = fixture('/h/mac/s/a', ['a']);
  f.closeTab(); expect(f.runtime.tabs.workspace('mac').tabs).toHaveLength(0);
  expect(routing.navigate).toHaveBeenCalledWith({ to: '/', replace: true }); expect(f.hideWindow).not.toHaveBeenCalled();
  f.closeTab(); expect(f.hideWindow).toHaveBeenCalledOnce(); expect(f.client.close).not.toHaveBeenCalled(); f.dispose();
});

it('Cmd-W hides a non-session page without closing background tabs or the SDK connection', () => {
  const f = fixture('/settings'); f.closeTab();
  expect(f.hideWindow).toHaveBeenCalledOnce(); expect(f.runtime.tabs.workspace('mac').tabs).toHaveLength(2);
  expect(f.client.close).not.toHaveBeenCalled(); f.dispose();
});
