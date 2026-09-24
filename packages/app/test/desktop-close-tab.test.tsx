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
vi.mock('@whip/ui/workspace-tabs', () => ({ WorkspaceDragScope: ({ children }: { children: ReactNode }) => <>{children}</>, WorkspaceTabs: ({ utilities }: { utilities: ReactNode }) => <div>{utilities}</div>, workspaceTabId: (id: string) => `tab-${id}` }));
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
  const listeners = new Set<() => void>(), newSessionListeners = new Set<() => void>(), reopenListeners = new Set<() => void>();
  const hideWindow = vi.fn();
  const disk = new Map<string, string>();
  const runtime = new AppRuntime({ defaultConnection: localProfile, connectionKinds: ['local'], resolveConnection: resolveURLConnection,
    storage: { keys: () => [...disk.keys()], getItem: key => disk.get(key) ?? null, setItem: (key, value) => { disk.set(key, value); }, removeItem: key => { disk.delete(key); } },
    copy: async () => {}, openExternal: async () => {}, download: async () => 'saved', sessionLink: value => value,
    onCloseTab(listener) { listeners.add(listener); return () => { listeners.delete(listener); }; }, hideWindow,
    onNewSession(listener) { newSessionListeners.add(listener); return () => { newSessionListeners.delete(listener); }; },
    onReopenClosedTab(listener) { reopenListeners.add(listener); return () => { reopenListeners.delete(listener); }; },
  });
  const connected = { state: 'connected', info: { runtime_id: 'mac', negotiated_capabilities: [] } };
  const client = { host: { attention: async () => ({ items: [] }) }, subscribe: () => () => {}, getSnapshot: () => connected, close: vi.fn() } as unknown as WhipClient;
  // The sidebar catalog knows each session's folder before any summaries poll; closing the last tab reuses it.
  const list = { subscribe: () => () => {}, getSnapshot: () => ({ status: 'live', page: { items: roots.map(id => ({ id, cwd: `/work/${id}` })) }, truncated: false }) };
  const home = { ...runtime.getSnapshot().home!, client, runtimeId: 'mac', state: 'connected' as const, list };
  vi.spyOn(runtime, 'getSnapshot').mockReturnValue({ ...runtime.getSnapshot(), home, hosts: [home] });
  for (const root of roots) runtime.tabs.open('mac', root);
  if (roots.includes('a')) runtime.tabs.visit('mac', 'a', {});
  runtime.setDraft('mac:a:a', 'Keep this draft');
  const draft = path === '/new/test' ? runtime.tabs.openNew({ runtimeId: 'mac' }) : undefined;
  routing.location = { pathname: draft ? `/new/${draft.id}` : path };
  const view = render(<RuntimeContext.Provider value={runtime}><UIProvider><QueryClientProvider client={runtime.queries}>
    <AppShell><p>Current route</p></AppShell>
  </QueryClientProvider></UIProvider></RuntimeContext.Provider>);
  return { runtime, client, hideWindow, listeners, newSessionListeners, reopenListeners,
    reopenTab() { act(() => { for (const listener of reopenListeners) listener(); }); },
    newSession() { act(() => { for (const listener of newSessionListeners) listener(); }); },
    closeTab() { act(() => { for (const listener of listeners) listener(); }); },
    dispose() { view.unmount(); runtime.dispose(); },
  };
}

it.each(['/', '/h/mac/s/a', '/h/mac/s/a?view=repl', '/new/test', '/settings', '/h/mac/t/terminal', '/browser/browser']
  .flatMap(path => ['palette', 'native'].map(source => ({ path, source }))))('$source New session allocates fresh tabs from $path', ({ path, source }) => {
  const f = fixture(path.split('?')[0], path === '/' ? [] : ['a', 'b']);
  if (path.endsWith('?view=repl')) act(() => { f.runtime.tabs.visit('mac', 'a', { view: 'repl' }); });
  const create = () => source === 'native' ? f.newSession() : fireEvent.click(screen.getByRole('button', { name: 'New session', exact: true }));
  const before = f.runtime.tabs.workspace().tabs;
  create();
  const first = f.runtime.tabs.workspace().tabs.find(tab => !before.some(previous => previous.id === tab.id))!;
  expect(first.kind).toBe('new');
  expect(routing.navigate).toHaveBeenLastCalledWith(expect.objectContaining({ to: '/new/$draftId', params: { draftId: first.id } }));
  create();
  expect(f.runtime.tabs.workspace().tabs).toHaveLength(before.length + 2);
  expect(f.runtime.tabs.workspace().tabs.filter(tab => !before.some(previous => previous.id === tab.id))).toHaveLength(2);
  expect(f.runtime.draft('mac:a:a')).toBe('Keep this draft');
  expect(f.client.close).not.toHaveBeenCalled();
  f.dispose();
});

it.each(['palette', 'native'])('%s creation at capacity reports the limit without changing navigation or selection', source => {
  const f = fixture();
  act(() => { while (f.runtime.tabs.workspace().tabs.length < 32) f.runtime.tabs.openNew(); });
  const before = f.runtime.tabs.workspace();
  const report = vi.spyOn(f.runtime, 'reportWorkspace');
  routing.navigate.mockClear();
  if (source === 'native') f.newSession();
  else fireEvent.click(screen.getByRole('button', { name: 'New session', exact: true }));
  expect(f.runtime.tabs.workspace()).toBe(before);
  expect(routing.navigate).not.toHaveBeenCalled();
  expect(report).toHaveBeenCalledOnce();
  f.dispose();
});

it.each(['dialog', 'alertdialog'])('native New session does not navigate behind an open %s and unsubscribes on unmount', role => {
  const f = fixture();
  const before = f.runtime.tabs.workspace();
  const dialog = document.createElement('div'); dialog.setAttribute('role', role); document.body.append(dialog);
  routing.navigate.mockClear();
  f.newSession();
  expect(f.runtime.tabs.workspace()).toBe(before);
  expect(routing.navigate).not.toHaveBeenCalled();
  dialog.remove();
  f.newSession();
  expect(f.runtime.tabs.workspace().tabs).toHaveLength(before.tabs.length + 1);
  expect(f.newSessionListeners.size).toBe(1);
  f.dispose();
  expect(f.newSessionListeners.size).toBe(0);
});

it.each(['/', '/h/mac/s/a', '/settings'])('native Reopen restores last-closed order and identity from %s', path => {
  const f = fixture(path);
  const [a, b] = f.runtime.tabs.workspace().tabs;
  act(() => { f.runtime.tabs.closeViews([a!.id]); f.runtime.tabs.closeViews([b!.id]); });
  routing.navigate.mockClear();
  f.reopenTab();
  expect(f.runtime.tabs.workspace().tabs.map(tab => tab.id)).toEqual([b!.id]);
  expect(routing.navigate).toHaveBeenLastCalledWith(expect.objectContaining({ state: { whipViewId: b!.id }, params: { runtimeId: 'mac', rootId: 'b' } }));
  f.reopenTab();
  expect(f.runtime.tabs.workspace().tabs.map(tab => tab.id)).toEqual([a!.id, b!.id]);
  expect(routing.navigate).toHaveBeenLastCalledWith(expect.objectContaining({ state: { whipViewId: a!.id }, params: { runtimeId: 'mac', rootId: 'a' } }));
  expect(f.runtime.draft('mac:a:a')).toBe('Keep this draft');
  const before = f.runtime.tabs.workspace();
  routing.navigate.mockClear(); f.reopenTab();
  expect(f.runtime.tabs.workspace()).toBe(before);
  expect(routing.navigate).not.toHaveBeenCalled();
  expect(f.hideWindow).not.toHaveBeenCalled(); expect(f.client.close).not.toHaveBeenCalled();
  expect(f.reopenListeners.size).toBe(1); f.dispose(); expect(f.reopenListeners.size).toBe(0);
});

it('native Reopen restores an unfinished draft without allocating a new one', () => {
  const f = fixture('/new/test', []);
  const draft = f.runtime.tabs.workspace().tabs[0]!;
  f.closeTab(); routing.navigate.mockClear(); f.reopenTab();
  expect(f.runtime.tabs.workspace().tabs).toEqual([draft]);
  expect(routing.navigate).toHaveBeenCalledWith(expect.objectContaining({ to: '/new/$draftId', params: { draftId: draft.id } }));
  f.dispose();
});

it.each(['dialog', 'alertdialog'])('native Reopen waits while a %s is open', role => {
  const f = fixture(); f.closeTab();
  const before = f.runtime.tabs.workspace();
  const dialog = document.createElement('div'); dialog.setAttribute('role', role); document.body.append(dialog);
  routing.navigate.mockClear(); f.reopenTab();
  expect(f.runtime.tabs.workspace()).toBe(before); expect(routing.navigate).not.toHaveBeenCalled();
  dialog.remove(); f.reopenTab();
  expect(f.runtime.tabs.workspace().closed).toHaveLength(0);
  f.dispose();
});

it.each(['/h/mac/s/a', '/settings'])('native Reopen reports capacity failure from %s without consuming history', path => {
  const f = fixture(path);
  act(() => { f.runtime.tabs.closeViews([f.runtime.tabs.workspace().tabs[0]!.id]); });
  act(() => { for (let i = 0; i < 31; i++) f.runtime.tabs.openNew({ runtimeId: 'mac' }); });
  const before = f.runtime.tabs.workspace();
  const report = vi.spyOn(f.runtime, 'reportWorkspace');
  routing.navigate.mockClear(); f.reopenTab();
  expect(f.runtime.tabs.workspace()).toBe(before); expect(routing.navigate).not.toHaveBeenCalled();
  expect(report).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining('32 open session tabs') }));
  f.dispose();
});

it('native Reopen reports a failed navigation rather than dropping the restored tab', async () => {
  const f = fixture(); f.closeTab();
  const closed = f.runtime.tabs.workspace().closed.at(-1)!.tab;
  const report = vi.spyOn(f.runtime, 'reportWorkspace');
  const error = new Error('Navigation failed'); routing.navigate.mockRejectedValueOnce(error);
  f.reopenTab(); await act(async () => {});
  expect(f.runtime.tabs.workspace().tabs).toContainEqual(closed);
  expect(report).toHaveBeenCalledWith(error);
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

it('Cmd-W past the final session tab opens a New Chat on its host, then empties, then hides the window', () => {
  const f = fixture('/h/mac/s/a', ['a']);
  f.closeTab();
  const [draft] = f.runtime.tabs.workspace().tabs;
  expect(f.runtime.tabs.workspace().tabs).toHaveLength(1); expect(draft).toMatchObject({ kind: 'new', runtimeId: 'mac', cwd: '/work/a' });
  expect(routing.navigate).toHaveBeenLastCalledWith(expect.objectContaining({ to: '/new/$draftId', params: { draftId: draft!.id }, replace: true }));
  expect(f.hideWindow).not.toHaveBeenCalled();
  f.closeTab(); expect(f.runtime.tabs.workspace().tabs).toHaveLength(0);
  expect(routing.navigate).toHaveBeenLastCalledWith({ to: '/', replace: true }); expect(f.hideWindow).not.toHaveBeenCalled();
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
