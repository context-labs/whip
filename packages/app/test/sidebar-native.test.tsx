import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { createTreeCatalogView } from '@whip/sdk/state';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { AnchorHTMLAttributes, ReactNode } from 'react';
import type { ListTreesResult } from '@whip/protocol';
import { providerFixture } from './provider-fixture';
import { RuntimeContext } from '../src/context';
import { SessionSidebar } from '../src/session-sidebar';
import { SessionActionsProvider } from '../src/session-actions';
import { SessionTabs } from '../src/session-tabs';
import { emptySidebarState } from '../src/sidebar-state';
import type { HostConnection } from '../src/hosts';

vi.hoisted(() => { globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }; });

vi.mock('@tanstack/react-router', () => ({
  useLocation: () => ({ pathname: '/' }), useNavigate: () => vi.fn(),
  Link: ({ children, params, search: _search, state: _state, preload: _preload, to: _to, ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { children: ReactNode; params?: { runtimeId: string; rootId: string }; search: unknown; state: unknown; preload: unknown; to: string }) =>
    <a {...props} href={params ? `/h/${params.runtimeId}/s/${params.rootId}` : _to}>{children}</a>,
}));
vi.mock('@tanstack/react-virtual', () => ({ useVirtualizer: ({ count, getItemKey }: { count: number; getItemKey(index: number): string }) => ({
  getVirtualItems: () => Array.from({ length: count }, (_, index) => ({ key: getItemKey(index), index, size: 28, start: index * 28, end: (index + 1) * 28 })),
  getTotalSize: () => count * 28, measure() {}, scrollToOffset() {}, scrollToIndex() {},
}) }));
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} });
});
afterEach(() => vi.unstubAllGlobals());
const row = (id: string): ListTreesResult['items'][number] => ({ root_id: `root-${id}`, working_directory: '/repo', tree: {
  id, metadata: { title: `Session ${id}`, pinned: false, archived: false }, engine: 'starlark', revision: '1', created_at: '2026-09-28T00:00:00Z',
} });

it('renders the bounded native catalog, observes only visible root summaries and continues at capacity', async () => {
  const f = await providerFixture(); const items = ['a', 'b', 'c'].map(row);
  f.data.handlers['trees.list'] = request => {
    const after = (request.params as { after?: string }).after;
    const index = after ? items.findIndex(item => item.tree.id === after) + 1 : 0;
    return { revision: '9007199254740993', items: [items[index]], next_cursor: index < 2 ? items[index]!.tree.id : null };
  };
  f.data.handlers['trees.summaries'] = request => ({ items: (request.params as { root_ids: string[] }).root_ids.map(id => ({ ...items.find(row => row.root_id === id)!, activity: {
    active_turn_count: '0', queued_input_count: '0', pending_permission_count: '9007199254740993', pending_question_count: '0', active_workspace_action_count: '0',
  } })), missing_root_ids: [] });
  const list = createTreeCatalogView(f.client, { maxItems: 2, pageSize: 1, maxBytes: 4096, pollIntervalMs: 60000 });
  await list.start();
  const host = { id: 'local', name: 'Local', runtimeId: 'host', state: 'connected', client: f.client, list, profile: { target: { kind: 'url' } } } as HostConnection;
  const state = { hosts: [host] };
  f.runtime.connections.host = () => host;
  Object.assign(f.runtime, { subscribe: () => () => {}, getSnapshot: () => state, tabs: new SessionTabs() });
  const mounted = render(<RuntimeContext.Provider value={f.runtime}><QueryClientProvider client={f.queries}><ThemeProvider initialTheme="light"><UIProvider><SessionActionsProvider>
    <SessionSidebar state={emptySidebarState()} setState={vi.fn()} onSearch={vi.fn()} onConnect={vi.fn()} onNavigate={vi.fn()} />
  </SessionActionsProvider></UIProvider></ThemeProvider></QueryClientProvider></RuntimeContext.Provider>);
  try {
    expect(screen.getByRole('link', { name: 'Session a' }).getAttribute('href')).toBe('/h/host/s/root-a');
    await screen.findByLabelText('Needs your input');
    expect(f.calls.find(call => call.method === 'trees.summaries')?.params).toEqual({ root_ids: ['root-a'] });
    fireEvent.click(screen.getByRole('button', { name: 'Load more sessions' }));
    await screen.findByRole('link', { name: 'Session b' });
    expect(screen.getByText(/This sidebar window reached its limit/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Next sessions' }));
    await screen.findByRole('link', { name: 'Session c' });
    expect(screen.queryByRole('link', { name: 'Session a' })).toBeNull();
    await waitFor(() => expect(f.calls.some(call => call.method === 'trees.summaries' && JSON.stringify(call.params) === JSON.stringify({ root_ids: ['root-c'] }))).toBe(true));
    expect(f.calls.some(call => call.method.startsWith('history.') || call.method === 'sessions.get' || call.method === 'sessions.submit')).toBe(false);
    expect(list.getSnapshot().items).toHaveLength(1);
  } finally { mounted.unmount(); await act(async () => list.dispose()); f.queries.clear(); }
});
