import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { AnchorHTMLAttributes, ReactNode } from 'react';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { ListTreesResult, HostAttentionResult, HostAttentionParams } from '@whip/protocol';
import { providerFixture } from './provider-fixture';
import type { AppRuntime } from '../src/runtime';
import type { HostConnection } from '../src/hosts';
import { RuntimeContext } from '../src/context';
import { SessionActionsProvider } from '../src/session-actions';
import { SessionSearchDialog } from '../src/session-search-dialog';
import { Attention } from '../src/attention';

const route = vi.hoisted(() => ({ location: { pathname: '/' }, navigate: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({
  useLocation: () => route.location, useNavigate: () => route.navigate,
  Link: ({ children, params, search: _search, state: _state, preload: _preload, to: _to, onClick, ...props }: AnchorHTMLAttributes<HTMLAnchorElement> & { children: ReactNode; params?: { runtimeId: string; rootId: string }; search: unknown; state: unknown; preload: unknown; to: string }) =>
    <a href={params ? `/h/${params.runtimeId}/s/${params.rootId}` : _to} {...props} onClick={event => { onClick?.(event); event.preventDefault(); }}>{children}</a>,
}));
beforeEach(() => {
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal('ResizeObserver', class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() });
  Object.defineProperty(HTMLElement.prototype, 'scrollTo', { configurable: true, value: vi.fn() });
});
afterEach(() => vi.unstubAllGlobals());

function page(title: string, cursor?: string): ListTreesResult {
  return { revision: '9007199254740993', items: [{ root_id: 'same-root', working_directory: '/repo', tree: { id: cursor ?? 'tree', metadata: { title, pinned: false, archived: false }, engine: 'starlark', revision: '1', created_at: '2026-09-08T00:00:00Z' } }], next_cursor: cursor ?? null };
}
function attention(title: string, next?: string, sessionId = 'same-root'): HostAttentionResult {
  return { items: [{ tree_id: 'tree', root_id: 'same-root', session_id: sessionId, title, activity: { session_id: sessionId, lifecycle: 'active', active_turn: null, active_input_id: null, queued_input_count: '0', pending_permission_count: '1', pending_question_count: '0', execution_permit: false, active_workspace_action_id: null } }], next_cursor: next ? { tree_id: next, session_id: sessionId } : null };
}
async function fixture() {
  const createHost = async (name: string, runtimeId: string) => {
    const f = await providerFixture({ runtimeID: runtimeId });
    const subscribers = new Set<() => void>();
    let catalog = { ...page(`${name} recent`), status: 'live' };
    const list = { getSnapshot: () => catalog, subscribe: (fn: () => void) => { subscribers.add(fn); return () => subscribers.delete(fn); }, refresh: vi.fn(async () => {}) };
    const search = vi.fn(async (params: { search?: string; after?: string | null; expected_revision?: string | null; limit: number }, _options: { signal?: AbortSignal }) => page(`${name} ${params.after ? 'next' : params.search ? 'match' : 'landing'}`, params.after ? undefined : `${runtimeId}-cursor`));
    const index = vi.fn(async (params: HostAttentionParams, _options: { signal?: AbortSignal }) => attention(`${name} ${params.after ? 'next request' : 'request'}`, params.after ? undefined : `${runtimeId}-after`));
    f.data.handlers['trees.list'] = (request, signal) => search(request.params as Parameters<typeof search>[0], { signal });
    f.data.handlers['host.attention'] = (request, signal) => index(request.params as HostAttentionParams, { signal });
    const client = f.client;
    vi.spyOn(client, 'session');
    const host = { profile: { target: { kind: 'url' } }, id: runtimeId, name, runtimeId, state: 'connected', client, list } as unknown as HostConnection;
    return { host, subscribers, list, search, index, client, revise: (revision: string) => { catalog = { ...catalog, revision }; subscribers.forEach(fn => fn()); } };
  };
  const local = await createHost('Local', 'local-runtime');
  const remote = await createHost('Kuzco', 'remote-runtime');
  const listeners = new Set<() => void>();
  let snapshot = { hosts: [local.host, remote.host], preferences: { attentionAnnouncements: true } };
  const tabSnapshot = {};
  const openTab = vi.fn(() => 'view');
  const updateLocation = vi.fn();
  const runtime = { subscribe: (fn: () => void) => { listeners.add(fn); return () => listeners.delete(fn); }, getSnapshot: () => snapshot,
    platform: { storage: { getItem: () => null } }, connections: { host: (id: string) => snapshot.hosts.find(host => host.runtimeId === id) },
    tabs: { subscribe: () => () => {}, getSnapshot: () => tabSnapshot, preferred: vi.fn(), open: openTab, updateLocation }, report: vi.fn() } as unknown as AppRuntime;
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: 10000 } } });
  Object.assign(runtime, { queries: query });
  const tree = (children: ReactNode) => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={query}><SessionActionsProvider>{children}</SessionActionsProvider></QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
  return { local, remote, runtime, query, openTab, updateLocation,
    render: (children: ReactNode) => { const result = render(tree(children)); return { ...result, rerender: (next: ReactNode) => result.rerender(tree(next)) }; },
    disconnect: () => { snapshot = { ...snapshot, hosts: [local.host, { ...remote.host, state: 'disconnected', client: undefined, list: undefined }] }; listeners.forEach(fn => fn()); },
  };
}

it('observes catalogs and loads all-status pages only while search is open, routing identical roots to their source hosts', async () => {
  const f = await fixture(); const close = vi.fn();
  const view = f.render(<SessionSearchDialog open={false} onOpenChange={close} finalFocus={false} />);
  expect(f.local.subscribers.size).toBe(0); expect(f.remote.subscribers.size).toBe(0);
  expect(f.local.search).not.toHaveBeenCalled(); expect(f.remote.search).not.toHaveBeenCalled();
  view.rerender(<SessionSearchDialog open onOpenChange={close} finalFocus={false} />);
  expect(f.local.subscribers.size).toBe(1); expect(f.remote.subscribers.size).toBe(1);
  for (const host of [f.local, f.remote]) {
    expect(host.search).toHaveBeenCalledExactlyOnceWith({ search: '', limit: 64 }, { signal: expect.any(AbortSignal) });
  }
  expect((await screen.findByRole('link', { name: 'Local landing · Local · /repo' })).getAttribute('href')).toBe('/h/local-runtime/s/same-root');
  const remote = await screen.findByRole('link', { name: 'Kuzco landing · Kuzco · /repo' });
  expect(remote.getAttribute('href')).toBe('/h/remote-runtime/s/same-root');
  fireEvent.click(remote);
  expect(f.openTab).toHaveBeenCalledExactlyOnceWith('remote-runtime', 'same-root', 'Kuzco landing');
  expect(close).toHaveBeenCalledWith(false);
  view.rerender(<SessionSearchDialog open={false} onOpenChange={close} finalFocus={false} />);
  expect(f.local.subscribers.size).toBe(0); expect(f.remote.subscribers.size).toBe(0);
  await waitFor(() => expect(f.query.getQueryCache().getAll()).toEqual([]));
  f.local.search.mockImplementation(() => new Promise(() => {}));
  f.remote.search.mockImplementation(() => new Promise(() => {}));
  view.rerender(<SessionSearchDialog open onOpenChange={close} finalFocus={false} />);
  expect(screen.queryByRole('link', { name: 'Local landing · Local · /repo' })).toBeNull();
  expect(screen.queryByRole('link', { name: 'Kuzco landing · Kuzco · /repo' })).toBeNull();
  expect(f.local.search).toHaveBeenCalledTimes(2); expect(f.remote.search).toHaveBeenCalledTimes(2);
  expect(f.local.client.session).not.toHaveBeenCalled(); expect(f.remote.client.session).not.toHaveBeenCalled();
});

it('keeps pagination and search failures independent, and filters hosts without hydrating roots', async () => {
  const f = await fixture();
  f.render(<SessionSearchDialog open onOpenChange={() => {}} finalFocus={false} />);
  await screen.findByRole('link', { name: 'Kuzco landing · Kuzco · /repo' });
  f.remote.search.mockRejectedValueOnce(new Error('Kuzco unavailable'));
  fireEvent.change(screen.getByRole('textbox', { name: 'Search sessions across hosts' }), { target: { value: 'match' } });
  await screen.findByRole('link', { name: 'Local match · Local · /repo' });
  expect(screen.getByRole('alert').textContent).toContain('Kuzco unavailable');
  fireEvent.click(screen.getByRole('button', { name: 'Retry Kuzco' }));
  await screen.findByRole('link', { name: 'Kuzco match · Kuzco · /repo' });
  fireEvent.click(screen.getByRole('button', { name: 'Load more sessions on Local' }));
  await screen.findByRole('link', { name: 'Local next · Local · /repo' });
  expect(screen.getByRole('link', { name: 'Kuzco match · Kuzco · /repo' })).toBeTruthy();
  expect(f.remote.search).toHaveBeenCalledTimes(3);
  expect(f.local.search.mock.lastCall?.[0]).toEqual({ search: 'match', after: 'local-runtime-cursor', expected_revision: '9007199254740993', limit: 64 });
  fireEvent.click(screen.getByRole('combobox', { name: 'Search host' }));
  const option = await screen.findByRole('option', { name: 'Kuzco' }); fireEvent.pointerDown(option); fireEvent.click(option);
  await waitFor(() => expect(screen.queryByRole('region', { name: 'Local search results' })).toBeNull());
  expect(screen.getByRole('link', { name: 'Kuzco match · Kuzco · /repo' })).toBeTruthy();
  expect(f.local.client.session).not.toHaveBeenCalled(); expect(f.remote.client.session).not.toHaveBeenCalled();
});

it('aborts pending search reads on close and preserves healthy results when another host disconnects', async () => {
  const f = await fixture(); let signal: AbortSignal | undefined;
  f.remote.search.mockImplementation((_params, options) => { signal = options.signal; return new Promise(() => {}); });
  const view = f.render(<SessionSearchDialog open onOpenChange={() => {}} finalFocus={false} />);
  fireEvent.change(screen.getByRole('textbox', { name: 'Search sessions across hosts' }), { target: { value: 'match' } });
  await screen.findByRole('link', { name: 'Local match · Local · /repo' });
  await waitFor(() => expect(signal).toBeDefined());
  act(() => f.disconnect());
  expect(screen.getByRole('link', { name: 'Local match · Local · /repo' })).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.getByText('Sessions unavailable while Kuzco is offline.')).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Manage servers' }).getAttribute('href')).toBe('/settings');
  view.rerender(<SessionSearchDialog open={false} onOpenChange={() => {}} finalFocus={false} />);
  expect(signal!.aborted).toBe(true);
  await waitFor(() => expect(f.query.getQueryCache().getAll()).toEqual([]));
});

it('aggregates attention with host-scoped links, independent pages and visible partial failures', async () => {
  const f = await fixture(); f.render(<Attention />);
  await screen.findByRole('button', { name: /Attention · 2 sessions/ });
  fireEvent.click(screen.getByRole('button', { name: /Attention ·/ }));
  const local = screen.getByRole('region', { name: 'Local attention' });
  const remote = screen.getByRole('region', { name: 'Kuzco attention' });
  expect(within(local).getByRole('link').getAttribute('href')).toBe('/h/local-runtime/s/same-root');
  expect(within(remote).getByRole('link').getAttribute('href')).toBe('/h/remote-runtime/s/same-root');
  fireEvent.click(within(remote).getByRole('button', { name: 'Next sessions on Kuzco' }));
  await within(remote).findByRole('link', { name: 'Kuzco next request · Kuzco' });
  expect(f.local.index).toHaveBeenCalledTimes(1);
  expect(f.remote.index.mock.lastCall?.[0]).toEqual({ after: { tree_id: 'remote-runtime-after', session_id: 'same-root' }, limit: 64, max_bytes: 256 << 10 });
  act(() => f.disconnect());
  expect(within(local).getByRole('link')).toBeTruthy();
  expect(within(remote).queryByRole('alert')).toBeNull();
  expect(within(remote).getByText('Activity unavailable while offline.')).toBeTruthy();
  expect(within(remote).getByRole('link', { name: 'Manage servers' }).getAttribute('href')).toBe('/settings');
  fireEvent.click(within(local).getByRole('link'));
  expect(f.openTab).toHaveBeenCalledExactlyOnceWith('local-runtime', 'same-root', 'Local request');
  expect(f.local.client.session).not.toHaveBeenCalled(); expect(f.remote.client.session).not.toHaveBeenCalled();
});

it('shows healthy attention requests when another daemon lacks the optional read', async () => {
  const f = await fixture(); f.remote.index.mockRejectedValue(new Error('host.attention is unsupported'));
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
  const f = await fixture(); let finish: (value: ListTreesResult) => void = () => {};
  f.render(<SessionSearchDialog open onOpenChange={() => {}} finalFocus={false} />);
  await screen.findByRole('link', { name: 'Local landing · Local · /repo' });
  f.local.search.mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  const input = screen.getByRole('textbox', { name: 'Search sessions across hosts' });
  fireEvent.change(input, { target: { value: 'match' } });
  const remote = await screen.findByRole('link', { name: 'Kuzco match · Kuzco · /repo' });
  fireEvent.pointerMove(remote.closest('[data-search-index]')!);
  await act(async () => finish(page('Local match')));
  await screen.findByRole('link', { name: 'Local match · Local · /repo' });
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(f.openTab).toHaveBeenCalledExactlyOnceWith('remote-runtime', 'same-root', 'Kuzco match');
});


it('invalidates only the changed native catalog and drops its revision-bound cursor', async () => {
  const f = await fixture(); f.render(<SessionSearchDialog open onOpenChange={() => {}} finalFocus={false} />);
  await screen.findByRole('link', { name: 'Local landing · Local · /repo' });
  fireEvent.click(screen.getByRole('button', { name: 'Load more sessions on Local' }));
  await screen.findByRole('link', { name: 'Local next · Local · /repo' });
  act(() => f.local.revise('9007199254740994'));
  await screen.findByRole('link', { name: 'Local landing · Local · /repo' });
  expect(f.local.search.mock.lastCall?.[0]).toEqual({ search: '', limit: 64 });
  expect(f.remote.search).toHaveBeenCalledOnce();
});

it('opens native child attention on its exact host and recipient without a transcript read', async () => {
  const f = await fixture(); f.remote.index.mockResolvedValue(attention('Child needs approval', undefined, 'child'));
  f.render(<Attention />);
  await screen.findByRole('button', { name: /Attention · 2 sessions/ });
  fireEvent.click(screen.getByRole('button', { name: /Attention ·/ }));
  fireEvent.click(screen.getByRole('link', { name: 'Child needs approval · Kuzco' }));
  expect(f.openTab).toHaveBeenCalledExactlyOnceWith('remote-runtime', 'same-root', 'Child needs approval');
  expect(f.updateLocation).toHaveBeenCalledExactlyOnceWith('view', { agent: 'child' });
  expect(f.remote.client.session).not.toHaveBeenCalled();
});
