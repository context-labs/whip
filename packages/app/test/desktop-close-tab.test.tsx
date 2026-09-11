import type { ReactNode } from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { AppShell } from '../src/shell';
import { localProfile, resolveURLConnection } from '../src/platform';

const routing = vi.hoisted(() => ({ location: { pathname: '/h/mac/s/a' }, navigate: vi.fn(), roots: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useLocation: () => routing.location, useParams: () => ({}), useNavigate: () => routing.navigate,
}));
vi.mock('@tanstack/react-hotkeys', () => ({ useHotkey: () => {} }));
vi.mock('@whip/ui', async importOriginal => ({
  ...await importOriginal<typeof import('@whip/ui')>(),
  CommandPicker: ({ items, onSelect }: { items: { value: string; label: string }[]; onSelect(value: string): void }) => <div>{items.filter(item => item.value === 'new').map(item => <button key={item.value} onClick={() => onSelect(item.value)}>{item.label}</button>)}</div>,
}));
vi.mock('../src/session-sidebar', () => ({ SessionSidebar: ({ onConnect }: { onConnect(): void }) => <button onClick={onConnect}>Manage servers</button> }));
vi.mock('../src/session-search-dialog', () => ({ SessionSearchDialog: () => null }));
vi.mock('../src/host-dialog', () => ({ HostDialog: () => null }));
vi.mock('../src/attention', () => ({ Attention: () => null, DesktopAttention: () => null }));
vi.mock('../src/connection-notice', () => ({ HostNotice: () => null }));
vi.mock('../src/conversation', () => ({ SessionContent: () => null, SessionLoading: () => null }));
vi.mock('../src/workspace-views', () => ({ useWorkspaceViews: (_runtime: unknown, roots: unknown) => { routing.roots(roots); return { views: new Map(), errors: new Map() }; }, workspaceRootKey: ({ runtimeId, rootId }: { runtimeId: string; rootId: string }) => JSON.stringify([runtimeId, rootId]) }));
// Exercise the shell, imperative tab action and real tab store; layout geometry has separate browser tests.
vi.mock('@whip/ui/workspace-tabs', () => ({ WorkspaceTabs: ({ utilities }: { utilities: ReactNode }) => <div>{utilities}</div>, workspaceTabId: (id: string) => `tab-${id}` }));
vi.mock('@whip/ui/workspace-layout', () => ({ WorkspaceLayout: () => null, workspacePanelId: (id: string) => `panel-${id}` }));
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  routing.location = { pathname: '/h/mac/s/a' };
  routing.navigate.mockReset().mockImplementation(async ({ to, params }: { to: string; params?: { runtimeId?: string; rootId?: string; draftId?: string } }) => {
    routing.location = { pathname: params?.draftId ? `/new/${params.draftId}` : params ? `/h/${params.runtimeId}/s/${params.rootId}` : to };
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
  const client = { host: { attention: async () => ({ items: [] }) }, subscribe: () => () => {}, getSnapshot: () => connected, close: vi.fn() } as unknown as WhipClient;
  const home = { ...runtime.getSnapshot().home!, client, runtimeId: 'mac', state: 'connected' as const };
  vi.spyOn(runtime, 'getSnapshot').mockReturnValue({ ...runtime.getSnapshot(), home, hosts: [home] });
  for (const root of roots) runtime.tabs.open('mac', root);
  if (roots.includes('a')) runtime.tabs.visit('mac', 'a', {});
  runtime.setDraft('mac:a:a', 'Keep this draft');
  const draft = path === '/new/test' ? runtime.tabs.openNew({ runtimeId: 'mac' }) : undefined;
  routing.location = { pathname: draft ? `/new/${draft.id}` : path };
  const view = render(<RuntimeContext.Provider value={runtime}><UIProvider><QueryClientProvider client={runtime.queries}>
    <AppShell><p>Current route</p></AppShell>
  </QueryClientProvider></UIProvider></RuntimeContext.Provider>);
  return { runtime, client, hideWindow, listeners,
    closeTab() { act(() => { for (const listener of listeners) listener(); }); },
    dispose() { view.unmount(); runtime.dispose(); },
  };
}

it.each(['/h/mac/s/a', '/h/mac/s/a?view=repl', '/new/test', '/settings'])('palette New session allocates fresh tabs from %s', path => {
  const f = fixture(path.split('?')[0]);
  if (path.endsWith('?view=repl')) act(() => { f.runtime.tabs.visit('mac', 'a', { view: 'repl' }); });
  const before = f.runtime.tabs.workspace().tabs;
  fireEvent.click(screen.getByRole('button', { name: 'New session', exact: true }));
  const first = f.runtime.tabs.workspace().tabs.find(tab => !before.some(previous => previous.id === tab.id))!;
  expect(first.kind).toBe('new');
  expect(routing.navigate).toHaveBeenLastCalledWith(expect.objectContaining({ to: '/new/$draftId', params: { draftId: first.id } }));
  fireEvent.click(screen.getByRole('button', { name: 'New session', exact: true }));
  expect(f.runtime.tabs.workspace().tabs).toHaveLength(before.length + 2);
  expect(f.runtime.tabs.workspace().tabs.filter(tab => !before.some(previous => previous.id === tab.id))).toHaveLength(2);
  expect(f.runtime.draft('mac:a:a')).toBe('Keep this draft');
  expect(f.client.close).not.toHaveBeenCalled();
  f.dispose();
});

it('palette creation at capacity reports the limit without changing navigation or selection', () => {
  const f = fixture();
  act(() => { while (f.runtime.tabs.workspace().tabs.length < 32) f.runtime.tabs.openNew(); });
  const before = f.runtime.tabs.workspace();
  const report = vi.spyOn(f.runtime, 'reportWorkspace');
  routing.navigate.mockClear();
  fireEvent.click(screen.getByRole('button', { name: 'New session', exact: true }));
  expect(f.runtime.tabs.workspace()).toBe(before);
  expect(routing.navigate).not.toHaveBeenCalled();
  expect(report).toHaveBeenCalledOnce();
  f.dispose();
});

it('server management uses Settings navigation without closing tabs, drafts or connections', () => {
  const f = fixture();
  const tabs = f.runtime.tabs.workspace().tabs;
  fireEvent.click(screen.getByRole('button', { name: 'Manage servers', exact: true }));
  expect(routing.navigate).toHaveBeenCalledWith({ to: '/settings', search: { section: 'connections', setting: 'hosts' } });
  expect(f.runtime.tabs.workspace().tabs).toEqual(tabs);
  expect(f.runtime.draft('mac:a:a')).toBe('Keep this draft');
  expect(f.client.close).not.toHaveBeenCalled();
  f.dispose();
});

it('Cmd-W closes the focused session tab through the existing action and preserves work and drafts', async () => {
  const f = fixture();
  f.closeTab();
  expect(f.runtime.tabs.workspace().tabs.map(tab => tab.rootId)).toEqual(['b']);
  expect(f.runtime.tabs.workspace().closed[0]?.tab.rootId).toBe('a');
  expect(f.runtime.draft('mac:a:a')).toBe('Keep this draft');
  expect(f.hideWindow).not.toHaveBeenCalled(); expect(f.client.close).not.toHaveBeenCalled();
  expect(routing.navigate).toHaveBeenCalledWith(expect.objectContaining({ params: { runtimeId: 'mac', rootId: 'b' }, replace: true }));
  f.dispose(); expect(f.listeners.size).toBe(0);
});

it('Cmd-W returns to New session after the final tab, then hides the window on the next command', () => {
  const f = fixture('/h/mac/s/a', ['a']);
  f.closeTab(); expect(f.runtime.tabs.workspace().tabs).toHaveLength(0);
  expect(routing.navigate).toHaveBeenCalledWith({ to: '/', replace: true }); expect(f.hideWindow).not.toHaveBeenCalled();
  f.closeTab(); expect(f.hideWindow).toHaveBeenCalledOnce(); expect(f.client.close).not.toHaveBeenCalled(); f.dispose();
});

it('Cmd-W returns from Settings without closing background tabs or the SDK connection', () => {
  const f = fixture('/settings'); f.closeTab();
  expect(f.hideWindow).not.toHaveBeenCalled(); expect(f.runtime.tabs.workspace().tabs).toHaveLength(2);
  expect(routing.navigate).toHaveBeenCalledWith(expect.objectContaining({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'mac', rootId: 'a' } }));
  expect(f.client.close).not.toHaveBeenCalled(); f.dispose();
});


it('Cmd-W closes a draft before hiding the window and drafts request no session views', () => {
  const f = fixture('/new/test', []);
  const draft = f.runtime.tabs.workspace().tabs[0]!;
  expect(draft.kind).toBe('new');
  expect(routing.roots).toHaveBeenLastCalledWith([]);
  f.closeTab();
  expect(f.runtime.tabs.workspace().tabs).toHaveLength(0);
  expect(f.runtime.tabs.workspace().closed.at(-1)?.tab.id).toBe(draft.id);
  expect(routing.navigate).toHaveBeenCalledWith({ to: '/', replace: true });
  expect(f.hideWindow).not.toHaveBeenCalled();
  f.dispose();
});
