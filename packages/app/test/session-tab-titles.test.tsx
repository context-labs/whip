import type { ReactNode } from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import { UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import type { SessionSummariesResult } from '@whip/protocol';
import { AppRuntime } from '../src/runtime';
import { RuntimeContext } from '../src/context';
import { SessionTabStrip } from '../src/session-tab-strip';
import { SessionActionsProvider, useSessionActions } from '../src/session-actions';

const roots = vi.hoisted(() => vi.fn());
const connection = vi.hoisted(() => ({ client: undefined as WhipClient | undefined }));
vi.mock('@whip/sdk', async importOriginal => ({ ...await importOriginal<typeof import('@whip/sdk')>(), createWhipClient: () => connection.client }));
vi.mock('@whip/sdk/state', async importOriginal => ({ ...await importOriginal<typeof import('@whip/sdk/state')>(), createSessionListView: () => ({ start: async () => {}, dispose: async () => {}, getSnapshot: () => ({}) }) }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useLocation: () => ({ pathname: '/h/mac/s/active' }), useNavigate: () => vi.fn(),
}));
vi.mock('../src/conversation', () => ({ SessionContent: () => null, SessionLoading: () => null }));
vi.mock('../src/workspace-views', () => ({
  useWorkspaceViews: (_runtime: unknown, requested: unknown) => { roots(requested); return { views: new Map(), errors: new Map() }; },
  workspaceRootKey: ({ runtimeId, rootId }: { runtimeId: string; rootId: string }) => JSON.stringify([runtimeId, rootId]),
}));
// Layout geometry and transcript ownership have separate tests; keep the real summary query and tab store.
vi.mock('@whip/ui/workspace-tabs', () => ({
  WorkspaceTabs: ({ items, value }: { items: { value: string; label: ReactNode }[]; value: string }) => <div role="tablist">{items.map(item => <button key={item.value} role="tab" aria-selected={item.value === value}>{item.label}</button>)}</div>,
  workspaceTabId: (id: string) => id,
}));
vi.mock('@whip/ui/workspace-layout', () => ({
  WorkspaceLayout: ({ renderHeader, layout }: { renderHeader(id: string): ReactNode; layout: { id: string } }) => <>{renderHeader(layout.id)}</>,
  workspacePanelId: (id: string) => id,
}));
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });
function RenameInactive() {
  const actions = useSessionActions();
  return <button onClick={() => actions.items({ runtimeId: 'mac', rootId: 'inactive', title: 'Help me repair the build' }).find(item => item.id === 'rename')?.onSelect?.()}>Rename inactive</button>;
}

it.each([false, true])('updates inactive tab titles without root streams (notification: %s)', async notification => {
  vi.useFakeTimers();
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  const disk = new Map<string, string>();
  const runtime = new AppRuntime({ defaultEndpoint: 'http://localhost:8080',
    storage: { keys: () => [...disk.keys()], getItem: key => disk.get(key) ?? null, setItem: (key, value) => { disk.set(key, value); }, removeItem: key => { disk.delete(key); } },
    copy: async () => {}, openExternal: async () => {}, download: async () => 'saved', sessionLink: value => value,
  });
  const summary = (root_id: string, title: string): SessionSummariesResult['items'][number] => ({
    root_id, title, missing: false, archived: false, cwd: '/work', running_agents: '0', queued_agents: '0',
    pending_permissions: '0', pending_questions: '0', truncated: false,
  });
  let items = [summary('active', 'Active conversation'), summary('inactive', 'Help me repair the build')];
  const summaries = vi.fn(async () => ({ items: structuredClone(items) }));
  const subscribe = vi.fn();
  const session = vi.fn();
  const connected = { state: 'connected', info: { runtime_id: 'mac', connection_id: 'connection', negotiated_capabilities: ['session_summaries', ...(notification ? ['session_title_notifications'] : [])] } };
  const titleListeners = new Set<(params: { root_id: string }) => void>();
  connection.client = { getSnapshot: () => connected, sessions: { summaries, list: async () => ({ items: [] }), get: async () => ({ root_id: 'inactive', title: items[1]!.title, cwd: '/work', archived: false, history_revision: '1' }) }, events: { subscribe }, session,
    configuration: { get: async () => ({ revision: '1', remote_hosts: [] }) },
    providers: { list: async () => ({}), catalogs: async () => ({}) },
    onNotification: (method: string, listener: (params: { root_id: string }) => void) => {
      expect(method).toBe('sessions.title.changed'); titleListeners.add(listener); return () => titleListeners.delete(listener);
    },
    subscribe: () => () => {}, connect: async () => {}, onEvent: () => () => {}, close: vi.fn(),
  } as unknown as WhipClient;
  await runtime.connect();
  runtime.tabs.open('mac', 'inactive');
  runtime.tabs.open('mac', 'active');
  runtime.tabs.visit('mac', 'active', {});
  const view = render(<RuntimeContext.Provider value={runtime}><UIProvider><QueryClientProvider client={runtime.queries}>
    <SessionActionsProvider><RenameInactive /><SessionTabStrip compact={false} onManageHosts={() => {}} utilities={null} /></SessionActionsProvider>
  </QueryClientProvider></UIProvider></RuntimeContext.Provider>);
  try {
    await act(async () => { await vi.advanceTimersByTimeAsync(1); });
    expect(screen.getByRole('tab', { name: 'Help me repair the build' }).getAttribute('aria-selected')).toBe('false');
    expect(summaries).toHaveBeenCalledWith(['active', 'inactive'], { signal: expect.any(AbortSignal) });
    if (notification) {
      await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Rename inactive' })); });
      fireEvent.change(screen.getByRole('textbox'), { target: { value: 'My unsent rename' } });
    }
    const before = summaries.mock.calls.length;
    // The daemon persisted a generated title; no root event or local command is delivered here.
    items = [items[0]!, summary('inactive', 'Repair build')];
    for (const listener of titleListeners) listener({ root_id: 'inactive' });
    await act(async () => { await vi.advanceTimersByTimeAsync(notification ? 251 : 2001); });
    expect(titleListeners.size).toBe(notification ? 1 : 0);
    if (notification) {
      expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('My unsent rename');
      await act(async () => { fireEvent.click(screen.getByLabelText('Close', { exact: true })); });
    }
    expect(summaries.mock.calls.length).toBeGreaterThan(before);
    expect(screen.getByRole('tab', { name: 'Repair build' }).getAttribute('aria-selected')).toBe('false');
    expect(screen.getByRole('tab', { name: 'Active conversation' }).getAttribute('aria-selected')).toBe('true');
    expect(runtime.tabs.workspace().tabs.find(tab => tab.kind === 'chat' && tab.rootId === 'inactive')?.titleHint).toBe('Repair build');
    if (notification) {
      // A missed hint is still recovered by the original two-second summary poll.
      items = [items[0]!, summary('inactive', 'Recovered by polling')];
      await act(async () => { await vi.advanceTimersByTimeAsync(2001); });
      expect(screen.getByRole('tab', { name: 'Recovered by polling' }).getAttribute('aria-selected')).toBe('false');
    }
    expect(roots).toHaveBeenCalled();
    expect(roots.mock.calls.every(([requested]) => requested.length === 1 && requested[0].rootId === 'active')).toBe(true);
    expect(session).not.toHaveBeenCalled();
    expect(subscribe).not.toHaveBeenCalled();
  } finally {
    view.unmount();
    runtime.dispose();
    expect(titleListeners.size).toBe(0);
  }
});
