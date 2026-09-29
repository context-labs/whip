import { useRef, useState, type ReactNode } from 'react';
import { act, fireEvent, render, screen, within, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, expect, it, vi } from 'vitest';
import { SessionSidebar } from '../src/session-sidebar';
import { emptySidebarState } from '../src/sidebar-state';
import type { HostConnection } from '../src/hosts';
import type { SessionListView, SessionListSnapshot } from '@whip/sdk/state';

const fixture = vi.hoisted(() => {
  globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  return {
    app: { hosts: [] as HostConnection[] }, pathname: '/',
    runtime: { queries: { invalidateQueries: vi.fn() }, tabs: { preferred: vi.fn(), open: vi.fn() }, connections: { connect: vi.fn().mockResolvedValue(undefined) } },
    scrollToIndex: vi.fn(), archive: vi.fn(),
  };
});
Object.defineProperty(window, 'matchMedia', { configurable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) });
vi.mock('../src/context', () => ({
  useAppState: () => fixture.app, useRuntime: () => fixture.runtime, useSessionTabs: () => ({}),
}));
vi.mock('../src/session-actions', () => ({ useSessionActions: () => ({ items: () => [], archive: fixture.archive, prepare: vi.fn() }) }));
vi.mock('@whip/ui/workspace-tabs', () => ({ WorkspaceExternalSource: ({ children }: { children(props: object): ReactNode }) => children({}) }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children, to, params, search, preload, state, ...props }: { children: ReactNode; to: string; params?: { runtimeId: string; rootId: string }; search?: object; preload?: boolean; state?: object }) =>
    <a {...props} href={params ? '/h/' + params.runtimeId + '/s/' + params.rootId : to}>{children}</a>,
  useNavigate: () => vi.fn(), useLocation: () => ({ pathname: fixture.pathname }),
}));
vi.mock('@tanstack/react-virtual', () => ({ useVirtualizer: (options: { count: number; getItemKey(index: number): string; estimateSize(index: number): number }) => {
  const current = useRef(options);
  current.current = options;
  // TanStack retains one instance across renders, including route-only navigation.
  const virtualizer = useRef({
    getVirtualItems: () => Array.from({ length: current.current.count }, (_, index) => ({ index, key: current.current.getItemKey(index), size: current.current.estimateSize(index), start: index * 36, end: (index + 1) * 36 })),
    getTotalSize: () => current.current.count * 36, measure() {}, scrollToOffset() {}, scrollToIndex: fixture.scrollToIndex,
  });
  return virtualizer.current;
} }));

beforeEach(() => { fixture.pathname = '/'; });

const local = { id: 'local', name: 'Local', local: true, state: 'closed', endpoint: 'http://localhost' } as HostConnection;
const remote = { id: 'remote', name: 'mm', local: false, state: 'closed', endpoint: 'https://remote.example' } as HostConnection;
const session = (id: string, cwd: string, updated_at = '2026-09-27T12:00:00Z') => ({ id, cwd, updated_at, pinned: false, archived: false, title: id, kind: 'root', model: '', provider: '', truncated: false });
function loaded(host: HostConnection, items: ReturnType<typeof session>[], has_more = false) {
  let snapshot: SessionListSnapshot = { status: 'live', truncated: false, page: { items, revision: '1', has_more } };
  const listeners = new Set<() => void>();
  const list = { getSnapshot: () => snapshot, subscribe: (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener); }, loadMore: vi.fn().mockResolvedValue(undefined), refresh: vi.fn().mockResolvedValue(undefined) };
  const summaries = vi.fn().mockResolvedValue({ items: [] });
  return {
    host: { ...host, runtimeId: host.id, state: 'connected', list: list as unknown as SessionListView,
      client: { getSnapshot: () => ({ info: { negotiated_capabilities: ['session_summaries'] } }), sessions: { summaries }, onEvent: () => () => {} } } as unknown as HostConnection,
    list, summaries, listeners,
    update(items: ReturnType<typeof session>[]) { snapshot = { ...snapshot, page: { ...snapshot.page!, items } }; listeners.forEach(listener => listener()); },
  };
}
function Sidebar() {
  const [state, setState] = useState(emptySidebarState);
  return <SessionSidebar state={state} setState={setState} onSearch={vi.fn()} onConnect={vi.fn()} onNavigate={vi.fn()} />;
}
const sidebar = () => <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Sidebar /></QueryClientProvider>;

it('omits host accordions without hiding connections or server management', () => {
  fixture.app.hosts = [local, remote];
  render(sidebar());
  expect(screen.getByRole('region', { name: 'Projects' })).toBeTruthy();
  expect(screen.queryByText('Projects')).toBeNull();
  expect(screen.queryByRole('button', { name: /^Local\s*Offline$/ })).toBeNull();
  expect(screen.getByRole('button', { name: 'Connect Local' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Connect mm' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Manage servers' })).toBeTruthy();
});

it.each([false, true])('keeps local setup out of project navigation (repairRequired=%s)', repairRequired => {
  fixture.app.hosts = [{ ...local, localRuntime: { state: 'missing', home: '/tmp/whip-test', message: 'No installation.', canInstall: true, repairRequired } }, remote];
  render(sidebar());
  expect(screen.queryByRole('button', { name: 'Set up this Mac' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Repair this Mac' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Connect Local' })).toBeNull();
  expect(screen.getByRole('button', { name: 'Connect mm' })).toBeTruthy();
});

it('interleaves directories, isolates identical paths/ids, and releases catalog observers', async () => {
  const a = loaded(local, [session('same', '/repo', '2026-09-27T13:00:00Z'), session('old', '/older', '2026-09-26T00:00:00Z')]);
  const b = loaded(remote, [session('same', '/repo')]);
  fixture.app.hosts = [a.host, b.host];
  const view = render(sidebar());
  const directories = () => [...view.container.querySelectorAll<HTMLElement>('[data-sidebar-directory]')];
  expect(directories().map(row => [row.dataset.sidebarRuntime, row.dataset.sidebarDirectory])).toEqual([['local', '/repo'], ['remote', '/repo'], ['local', '/older']]);
  expect(screen.getByRole('button', { name: 'mm · Connected · /repo' })).toBeTruthy();
  for (const directory of directories()) {
    expect(directory.querySelector('.lucide-folder, .lucide-folder-open, .lucide-globe')).toBeNull();
    expect(directory.querySelector('[data-directory-caret]')).toBeTruthy();
  }
  expect(screen.queryByRole('button', { name: 'Local · Connected · /repo' })).toBeNull();
  expect(view.container.querySelectorAll('a[href$="/s/same"]')).toHaveLength(2);
  const remoteSession = view.container.querySelector<HTMLElement>('[data-sidebar-session="same"][data-sidebar-runtime="remote"]')!;
  fireEvent.click(within(remoteSession).getByRole('button', { name: 'Archive same' }));
  expect(fixture.archive).toHaveBeenLastCalledWith({ runtimeId: 'remote', rootId: 'same', title: 'same', archived: false }, true);
  await waitFor(() => expect(a.summaries).toHaveBeenCalled());
  expect(a.summaries.mock.calls[0]?.[0]).toEqual(['old', 'same']);
  expect(b.summaries.mock.calls[0]?.[0]).toEqual(['same']);
  fireEvent.click(within(directories()[1]!).getByRole('button', { name: '/repo', exact: true }));
  expect(view.container.querySelector('a[href="/h/remote/s/same"]')).toBeNull();
  expect(view.container.querySelector('a[href="/h/local/s/same"]')).not.toBeNull();
  act(() => b.update([session('same', '/repo', '2026-09-28T00:00:00Z')]));
  expect(directories()[0]?.dataset.sidebarRuntime).toBe('remote');
  expect(within(directories()[0]!).getByRole('button', { name: '/repo', exact: true }).getAttribute('aria-expanded')).toBe('false');
  view.unmount();
  expect(a.listeners.size + b.listeners.size).toBe(0);
});

it('reveals on route-only navigation with stable catalogs and virtualizer', () => {
  const a = loaded(local, [session('same', '/repo')]);
  const b = loaded(remote, [session('same', '/repo')]);
  fixture.app.hosts = [a.host, b.host];
  fixture.pathname = '/';
  const view = render(sidebar());
  const directory = view.container.querySelector<HTMLElement>('[data-sidebar-directory][data-sidebar-runtime="remote"]')!;
  fireEvent.click(within(directory).getByRole('button', { name: '/repo', exact: true }));
  fixture.scrollToIndex.mockClear();
  fixture.pathname = '/h/remote/s/same';
  view.rerender(sidebar());
  expect(view.container.querySelector('a[href="/h/remote/s/same"]')).not.toBeNull();
  expect(fixture.scrollToIndex).toHaveBeenCalledOnce();
  expect(within(directory).getByRole('button', { name: '/repo', exact: true }).getAttribute('aria-expanded')).toBe('true');
  fixture.scrollToIndex.mockClear();
  fixture.pathname = '/h/local/s/same';
  view.rerender(sidebar());
  expect(fixture.scrollToIndex).toHaveBeenCalledOnce();
  fixture.pathname = '/';
});

it('exposes untruncated connection identity for duplicate host names and paths', async () => {
  const a = loaded(remote, [session('same', '/repo')]);
  const b = loaded({ ...remote, id: 'remote-2', endpoint: 'https://another.example' }, [session('same', '/repo')]);
  fixture.app.hosts = [a.host, b.host];
  render(sidebar());
  fireEvent.click(screen.getByRole('button', { name: 'mm · remote-2 · Connected · /repo', exact: true }));
  const details = await screen.findByRole('dialog', { name: /^mm · remote-2 Connected/ });
  expect(within(details).getByText('https://another.example', { exact: true })).toBeTruthy();
  expect(within(details).getByText('/repo', { exact: true })).toBeTruthy();
});

it('keeps host details compact without repeating the address and retains reconnect actions', async () => {
  const a = loaded({ ...remote, endpoint: 'mm' }, [session('same', '/repo')]);
  fixture.app.hosts = [a.host];
  const view = render(sidebar());
  fireEvent.click(screen.getByRole('button', { name: 'mm · Connected · /repo', exact: true }));
  const details = await screen.findByRole('dialog', { name: 'mm Connected', exact: true });
  expect(within(details).getAllByText('mm', { exact: true })).toHaveLength(1);
  expect(within(details).getByText('/repo', { exact: true })).toBeTruthy();
  expect(within(details).queryByRole('button', { name: 'Reconnect mm' })).toBeNull();
  expect(within(details).getByRole('button', { name: 'Manage servers' })).toBeTruthy();
  fixture.app.hosts = [{ ...a.host, state: 'reconnecting', error: 'Connection lost' }];
  view.rerender(sidebar());
  expect(within(details).getByText('Reconnecting', { exact: true })).toBeTruthy();
  expect(within(details).getByRole('status').textContent).toBe('Connection lost');
  expect(within(details).getByRole('button', { name: 'Reconnect mm' }).hasAttribute('disabled')).toBe(true);
  fixture.app.hosts = [{ ...a.host, state: 'closed' }];
  view.rerender(sidebar());
  fireEvent.click(within(details).getByRole('button', { name: 'Reconnect mm' }));
  expect(fixture.runtime.connections.connect).toHaveBeenLastCalledWith('remote');
});

it('reveals an older routed session only in its source host, never on catalog polling', () => {
  const a = loaded(local, [session('same', '/repo')]);
  const b = loaded(remote, Array.from({ length: 16 }, (_, i) => session('remote-' + i, '/repo')));
  fixture.app.hosts = [a.host, b.host];
  fixture.pathname = '/h/remote/s/remote-15';
  const view = render(sidebar());
  expect(view.container.querySelectorAll('[data-sidebar-session][data-sidebar-runtime="remote"]')).toHaveLength(16);
  expect(view.container.querySelectorAll('[data-sidebar-session][data-sidebar-runtime="local"]')).toHaveLength(1);
  expect(view.container.querySelector('a[href="/h/remote/s/remote-15"]')?.getAttribute('aria-current')).toBe('page');
  const directory = view.container.querySelector('[data-sidebar-directory][data-sidebar-runtime="remote"]')!;
  fireEvent.click(within(directory as HTMLElement).getByRole('button', { name: '/repo', exact: true }));
  act(() => b.update([session('remote-15', '/repo')]));
  expect(view.container.querySelector('a[href="/h/remote/s/remote-15"]')).toBeNull();
  fixture.pathname = '/';
});

it('keeps seven/More/Less per host, stale rows on transport loss, and clears detached rows', async () => {
  const a = loaded(local, Array.from({ length: 15 }, (_, i) => session('local-' + i, '/repo')));
  const b = loaded(remote, Array.from({ length: 15 }, (_, i) => session('remote-' + i, '/repo')), true);
  fixture.app.hosts = [a.host, b.host];
  const view = render(sidebar());
  const sessions = (id: string) => view.container.querySelectorAll('[data-sidebar-session][data-sidebar-runtime="' + id + '"]');
  expect(sessions('local')).toHaveLength(7);
  expect(sessions('remote')).toHaveLength(7);
  const more = () => view.container.querySelector<HTMLButtonElement>('button[data-sidebar-runtime="remote"][aria-label="More sessions in /repo"]')!;
  fireEvent.click(more()); expect(sessions('remote')).toHaveLength(14);
  fireEvent.click(more()); expect(sessions('remote')).toHaveLength(15);
  fireEvent.click(screen.getByRole('button', { name: 'Less sessions in /repo' }));
  expect(sessions('remote')).toHaveLength(7);
  expect(sessions('local')).toHaveLength(7);
  fireEvent.click(screen.getByRole('button', { name: 'Load more sessions · mm' }));
  expect(b.list.loadMore).toHaveBeenCalledOnce(); expect(a.list.loadMore).not.toHaveBeenCalled();
  fixture.app.hosts = [a.host, { ...b.host, state: 'reconnecting' }];
  view.rerender(sidebar());
  expect(screen.getByRole('button', { name: 'mm · Reconnecting · /repo' })).toBeTruthy();
  expect(sessions('remote')).toHaveLength(7);
  fixture.app.hosts = [a.host, remote];
  view.rerender(sidebar());
  expect(sessions('remote')).toHaveLength(0);
  expect(screen.getByRole('button', { name: 'Connect mm' })).toBeTruthy();
});
