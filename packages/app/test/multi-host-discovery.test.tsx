import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { AnchorHTMLAttributes, ReactNode } from 'react';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { SessionCatalogPage, HostAttentionResult } from '@whip/protocol';
import type { AppRuntime } from '../src/runtime';
import type { HostConnection } from '../src/hosts';
import { RuntimeContext } from '../src/context';
import { SessionActionsProvider } from '../src/session-actions';
import { SessionSearchDialog } from '../src/session-search-dialog';
import { Attention } from '../src/attention';

const route = vi.hoisted(() => ({ location: { pathname: '/' }, navigate: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({
  useLocation: () => route.location, useNavigate: () => route.navigate,
  Link: ({ children, params, search: _search, state: _state, preload: _preload, to: _to, onClick, ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { children: ReactNode; params: { runtimeId: string; rootId: string }; search: unknown; state: unknown; preload: unknown; to: string }) =>
    <a href={`/h/${params.runtimeId}/s/${params.rootId}`} {...props} onClick={event => { onClick?.(event); event.preventDefault(); }}>{children}</a>,
}));
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() });
  Object.defineProperty(HTMLElement.prototype, 'scrollTo', { configurable: true, value: vi.fn() });
});
afterEach(() => vi.unstubAllGlobals());

function page(title: string, cursor?: string): SessionCatalogPage {
  return { revision: '1', items: [{ id: 'same-root', kind: 'interactive', title, model: 'm', provider: 'p', cwd: '/repo', pinned: false, archived: false, updated_at: '2026-09-08T00:00:00Z', truncated: false }], has_more: !!cursor,
    ...(cursor ? { next_cursor: { revision: '1', offset: cursor } } : {}) };
}
function attention(title: string, next?: string): HostAttentionResult {
  return { items: [{ root_id: 'same-root', title, active_agents: '1', pending_permissions: '1', questions: [], truncated: false }], has_more: !!next, truncated: false, ...(next ? { next_after_id: next } : {}) };
}
function fixture() {
  const createHost = (name: string, runtimeId: string) => {
    const subscribers = new Set<() => void>();
    const catalog = { page: page(`${name} recent`), status: 'ready' };
    const list = { getSnapshot: () => catalog, subscribe: (fn: () => void) => { subscribers.add(fn); return () => subscribers.delete(fn); }, refresh: vi.fn(async () => {}) };
    const search = vi.fn(async (params: { cursor?: { offset: string } | null }, _options: { signal: AbortSignal }) => page(`${name} ${params.cursor ? 'next' : 'match'}`, params.cursor ? undefined : `${runtimeId}-cursor`));
    const index = vi.fn(async (params: { after_id?: string }, _options: { signal: AbortSignal }) => attention(`${name} ${params.after_id ? 'next request' : 'request'}`, params.after_id ? undefined : `${runtimeId}-after`));
    const client = { sessions: { list: search }, host: { attention: index }, session: vi.fn() };
    const host = { profile: { target: { kind: 'url' } }, id: runtimeId, name, runtimeId, state: 'connected', client, list } as unknown as HostConnection;
    return { host, subscribers, list, search, index, client };
  };
  const local = createHost('Local', 'local-runtime');
  const remote = createHost('Kuzco', 'remote-runtime');
  const listeners = new Set<() => void>();
  let snapshot = { hosts: [local.host, remote.host], preferences: { attentionAnnouncements: true } };
  const tabSnapshot = {};
  const openTab = vi.fn();
  const runtime = { subscribe: (fn: () => void) => { listeners.add(fn); return () => listeners.delete(fn); }, getSnapshot: () => snapshot,
    platform: { storage: { getItem: () => null } }, connections: { host: (id: string) => snapshot.hosts.find(host => host.runtimeId === id) },
    tabs: { subscribe: () => () => {}, getSnapshot: () => tabSnapshot, preferred: vi.fn(), open: openTab }, report: vi.fn() } as unknown as AppRuntime;
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 10000 } } });
  Object.assign(runtime, { queries: query });
  const tree = (children: ReactNode) => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={query}><SessionActionsProvider>{children}</SessionActionsProvider></QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
  return { local, remote, runtime, query, openTab,
    render: (children: ReactNode) => { const result = render(tree(children)); return { ...result, rerender: (next: ReactNode) => result.rerender(tree(next)) }; },
    disconnect: () => { snapshot = { ...snapshot, hosts: [local.host, { ...remote.host, state: 'disconnected', client: undefined, list: undefined }] }; listeners.forEach(fn => fn()); },
  };
}

it('observes recent catalogs only while search is open, and routes identical roots to their source hosts', async () => {
  const f = fixture(); const close = vi.fn();
  const view = f.render(<SessionSearchDialog open={false} onOpenChange={close} finalFocus={false} />);
  expect(f.local.subscribers.size).toBe(0); expect(f.remote.subscribers.size).toBe(0);
  view.rerender(<SessionSearchDialog open onOpenChange={close} finalFocus={false} />);
  expect(f.local.subscribers.size).toBe(1); expect(f.remote.subscribers.size).toBe(1);
  expect(f.local.search).not.toHaveBeenCalled(); expect(f.remote.search).not.toHaveBeenCalled();
  expect(screen.getByRole('link', { name: 'Local recent · Local · /repo' }).getAttribute('href')).toBe('/h/local-runtime/s/same-root');
  const remote = screen.getByRole('link', { name: 'Kuzco recent · Kuzco · /repo' });
  expect(remote.getAttribute('href')).toBe('/h/remote-runtime/s/same-root');
  fireEvent.click(remote);
  expect(f.openTab).toHaveBeenCalledExactlyOnceWith('remote-runtime', 'same-root', 'Kuzco recent');
  expect(close).toHaveBeenCalledWith(false);
  view.rerender(<SessionSearchDialog open={false} onOpenChange={close} finalFocus={false} />);
  expect(f.local.subscribers.size).toBe(0); expect(f.remote.subscribers.size).toBe(0);
  expect(f.local.client.session).not.toHaveBeenCalled(); expect(f.remote.client.session).not.toHaveBeenCalled();
});

it('keeps pagination and search failures independent, and filters hosts without hydrating roots', async () => {
  const f = fixture();
  f.remote.search.mockRejectedValueOnce(new Error('Kuzco unavailable'));
  f.render(<SessionSearchDialog open onOpenChange={() => {}} finalFocus={false} />);
  fireEvent.change(screen.getByRole('textbox', { name: 'Search sessions across hosts' }), { target: { value: 'match' } });
  await screen.findByRole('link', { name: 'Local match · Local · /repo' });
  expect(screen.getByRole('alert').textContent).toContain('Kuzco unavailable');
  fireEvent.click(screen.getByRole('button', { name: 'Retry Kuzco' }));
  await screen.findByRole('link', { name: 'Kuzco match · Kuzco · /repo' });
  fireEvent.click(screen.getByRole('button', { name: 'Load more sessions on Local' }));
  await screen.findByRole('link', { name: 'Local next · Local · /repo' });
  expect(screen.getByRole('link', { name: 'Kuzco match · Kuzco · /repo' })).toBeTruthy();
  expect(f.remote.search).toHaveBeenCalledTimes(2);
  expect(f.local.search.mock.lastCall?.[0]).toEqual({ search: 'match', status: 'active', cursor: { revision: '1', offset: 'local-runtime-cursor' }, limit: 64, max_bytes: 256 << 10 });
  fireEvent.click(screen.getByRole('combobox', { name: 'Search host' }));
  const option = await screen.findByRole('option', { name: 'Kuzco' }); fireEvent.pointerDown(option); fireEvent.click(option);
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Local search results' })).toBeNull());
  expect(screen.getByRole('link', { name: 'Kuzco match · Kuzco · /repo' })).toBeTruthy();
  expect(f.local.client.session).not.toHaveBeenCalled(); expect(f.remote.client.session).not.toHaveBeenCalled();
});

it('aborts pending search reads on close and preserves healthy results when another host disconnects', async () => {
  const f = fixture(); let signal: AbortSignal | undefined;
  f.remote.search.mockImplementation((_params, options) => { signal = options.signal; return new Promise(() => {}); });
  const view = f.render(<SessionSearchDialog open onOpenChange={() => {}} finalFocus={false} />);
  fireEvent.change(screen.getByRole('textbox', { name: 'Search sessions across hosts' }), { target: { value: 'match' } });
  await screen.findByRole('link', { name: 'Local match · Local · /repo' });
  await waitFor(() => expect(signal).toBeDefined());
  act(() => f.disconnect());
  expect(screen.getByRole('link', { name: 'Local match · Local · /repo' })).toBeTruthy();
  expect(screen.getByRole('alert').textContent).toContain('Kuzco is disconnected');
  view.rerender(<SessionSearchDialog open={false} onOpenChange={() => {}} finalFocus={false} />);
  expect(signal!.aborted).toBe(true);
  await waitFor(() => expect(f.query.getQueryCache().getAll()).toHaveLength(0));
});

it('aggregates attention with host-scoped links, independent pages and visible partial failures', async () => {
  const f = fixture(); f.render(<Attention />);
  await screen.findByRole('button', { name: /Attention · 2 sessions/ });
  fireEvent.click(screen.getByRole('button', { name: /Attention ·/ }));
  const local = screen.getByRole('region', { name: 'Local attention' });
  const remote = screen.getByRole('region', { name: 'Kuzco attention' });
  expect(within(local).getByRole('link').getAttribute('href')).toBe('/h/local-runtime/s/same-root');
  expect(within(remote).getByRole('link').getAttribute('href')).toBe('/h/remote-runtime/s/same-root');
  fireEvent.click(within(remote).getByRole('button', { name: 'Next sessions on Kuzco' }));
  await within(remote).findByRole('link', { name: 'Kuzco next request · Kuzco' });
  expect(f.local.index).toHaveBeenCalledTimes(1);
  expect(f.remote.index.mock.lastCall?.[0]).toEqual({ after_id: 'remote-runtime-after', limit: 64, max_bytes: 256 << 10 });
  act(() => f.disconnect());
  expect(within(local).getByRole('link')).toBeTruthy();
  expect(within(remote).getByRole('alert').textContent).toContain('Kuzco is disconnected');
  expect(within(remote).queryByRole('link')).toBeNull();
  fireEvent.click(within(local).getByRole('link'));
  expect(f.openTab).toHaveBeenCalledExactlyOnceWith('local-runtime', 'same-root', 'Local request');
  expect(f.local.client.session).not.toHaveBeenCalled(); expect(f.remote.client.session).not.toHaveBeenCalled();
});

it('shows healthy attention requests when another daemon lacks the optional read', async () => {
  const f = fixture(); f.remote.index.mockRejectedValue(new Error('host.attention is unsupported'));
  f.render(<Attention />);
  await screen.findByRole('button', { name: /Attention · 1 session/ });
  fireEvent.click(screen.getByRole('button', { name: /Attention ·/ }));
  expect(screen.getByRole('link', { name: 'Local request · Local' })).toBeTruthy();
  expect(screen.getByRole('alert').textContent).toContain('host.attention is unsupported');
  fireEvent.click(screen.getByRole('combobox', { name: 'Attention host' }));
  const option = await screen.findByRole('option', { name: 'Local' }); fireEvent.pointerDown(option); fireEvent.click(option);
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Kuzco attention' })).toBeNull());
  expect(screen.getByRole('link', { name: 'Local request · Local' })).toBeTruthy();
});


it('keeps the highlighted session on its host when an earlier host finishes searching', async () => {
  const f = fixture(); let finish: (value: SessionCatalogPage) => void = () => {};
  f.local.search.mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  f.render(<SessionSearchDialog open onOpenChange={() => {}} finalFocus={false} />);
  const input = screen.getByRole('textbox', { name: 'Search sessions across hosts' });
  fireEvent.change(input, { target: { value: 'match' } });
  const remote = await screen.findByRole('link', { name: 'Kuzco match · Kuzco · /repo' });
  fireEvent.pointerMove(remote.closest('[data-search-index]')!);
  await act(async () => finish(page('Local match')));
  await screen.findByRole('link', { name: 'Local match · Local · /repo' });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(f.openTab).toHaveBeenCalledExactlyOnceWith('remote-runtime', 'same-root', 'Kuzco match');
});
