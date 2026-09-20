import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { WhipClient } from '@whip/sdk';
import { RemoteDirectoryDialog, directoryCrumbs, recentDirectories } from '../src/remote-directory-dialog';
import { directoryOptions } from '../src/directory-queries';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const result = (path: string, names: string[] = []) => ({ path, parent: path.slice(0, path.lastIndexOf('/')) || '/', entries: names.map(name => ({ name, path: `${path}/${name}` })), has_more: false, next_after: '' });
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { promise, resolve }; }
function fixture() {
  let snapshot = { state: 'connected', info: { runtime_id: 'kuzco' } };
  const listeners = new Set<() => void>();
  const directories = vi.fn(async (params: { path?: string; limit?: number; prefix?: string; show_hidden?: boolean; after?: string }, _options?: { signal?: AbortSignal }) => result(params.path || '/home/sam', params.path === '/home/sam' ? ['alpha', 'beta', 'private'] : []));
  const client = { subscribe: (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener); }, getSnapshot: () => snapshot, host: { directories } } as unknown as WhipClient;
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const onSelect = vi.fn(), onClose = vi.fn();
  const reconnect = vi.fn(async () => { setState('connected'); });
  function setState(state: string) { act(() => { snapshot = { ...snapshot, state }; listeners.forEach(listener => listener()); }); }
  const view = render(<ThemeProvider initialTheme="dark"><UIProvider><QueryClientProvider client={query}><RemoteDirectoryDialog client={client} host={{ name: 'Kuzco', detail: 'sam@kuzco', reconnect }} value="/home/sam" disabled={false} onSelect={onSelect} onClose={onClose} /></QueryClientProvider></UIProvider></ThemeProvider>);
  return { client, directories, query, onSelect, onClose, reconnect, setState, view };
}
const choose = () => screen.getByRole('button', { name: 'Choose folder', exact: true }) as HTMLButtonElement;
const ready = () => waitFor(() => expect(choose().disabled).toBe(false));
const folder = (name: string) => within(screen.getByRole('list', { name: 'Folders' })).getByRole('button', { name, exact: true });

it('selects with one click, checks readability, and opens separately with the chevron', async () => {
  const f = fixture(); await ready();
  const probe = deferred<ReturnType<typeof result>>();
  f.directories.mockImplementationOnce(() => probe.promise);
  fireEvent.click(folder('alpha'));
  await screen.findByText('Checking folder…');
  expect(choose().disabled).toBe(true);
  fireEvent.click(choose()); expect(f.onSelect).not.toHaveBeenCalled();
  await act(async () => probe.resolve(result('/home/sam/alpha')));
  await ready();
  expect(screen.getByRole('navigation', { name: 'Folder path' }).textContent).not.toContain('alpha');
  fireEvent.click(choose()); await waitFor(() => expect(f.onSelect).toHaveBeenCalledExactlyOnceWith('/home/sam/alpha'));
  fireEvent.click(screen.getByRole('button', { name: 'Open beta' }));
  await screen.findByText('No subfolders here'); await ready();
  fireEvent.click(choose()); await waitFor(() => expect(f.onSelect).toHaveBeenLastCalledWith('/home/sam/beta'));
});

it('blocks inaccessible selections and recovers by selecting a readable folder', async () => {
  const f = fixture(); await ready();
  f.directories.mockRejectedValueOnce(new Error('Permission denied'));
  fireEvent.click(folder('private'));
  await screen.findByText('Can’t use this folder: Permission denied');
  expect(choose().disabled).toBe(true);
  fireEvent.keyDown(folder('private'), { key: 'Enter', metaKey: true }); expect(f.onSelect).not.toHaveBeenCalled();
  fireEvent.click(folder('beta')); await ready();
  fireEvent.click(choose()); await waitFor(() => expect(f.onSelect).toHaveBeenCalledExactlyOnceWith('/home/sam/beta'));
});

it('keeps typed paths unconfirmed until navigation succeeds, and supports retry', async () => {
  const f = fixture(); await ready();
  fireEvent.click(screen.getByRole('button', { name: 'Edit path' }));
  expect(choose().disabled).toBe(true);
  const input = screen.getByRole('textbox', { name: 'Remote path' });
  fireEvent.change(input, { target: { value: 'relative' } });
  fireEvent.submit(input.closest('form')!);
  await screen.findByText('Enter an absolute path or a path starting with ~/.');
  f.directories.mockRejectedValueOnce(new Error('No such directory'));
  fireEvent.change(input, { target: { value: '/missing' } });
  fireEvent.submit(input.closest('form')!);
  await screen.findByText('Can’t open this folder'); expect(choose().disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Retry' })); await ready();
  fireEvent.click(choose()); await waitFor(() => expect(f.onSelect).toHaveBeenCalledExactlyOnceWith('/missing'));
});

it('uses host-side prefix, hidden and page filters and retains bounded listings for revisits', async () => {
  const f = fixture(); await ready();
  f.directories.mockImplementation(async params => ({ ...result(params.path!, [params.after ? 'omega' : 'alpha']), has_more: !params.after, next_after: 'alpha' }));
  const filter = screen.getByRole('textbox', { name: 'Filter folders by prefix' });
  fireEvent.change(filter, { target: { value: 'a' } });
  expect(choose().disabled).toBe(true);
  await waitFor(() => expect(f.directories).toHaveBeenCalledWith(expect.objectContaining({ prefix: 'a', limit: 64, show_hidden: false }), expect.anything()));
  await ready(); fireEvent.click(screen.getByRole('button', { name: 'Next folders' }));
  await waitFor(() => expect(f.directories).toHaveBeenCalledWith(expect.objectContaining({ prefix: 'a', after: 'alpha' }), expect.anything()));
  await ready();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Hidden folders' }));
  await waitFor(() => expect(f.directories).toHaveBeenCalledWith(expect.objectContaining({ prefix: 'a', after: undefined, show_hidden: true }), expect.anything()));
  await ready();
  fireEvent.change(filter, { target: { value: '/' } });
  await screen.findByText('Enter a folder name without slashes.'); expect(choose().disabled).toBe(true);
  expect(f.directories.mock.calls.some(([p]) => p.prefix === '/')).toBe(false);
  f.view.unmount();
  expect(f.query.getQueryCache().getAll().length).toBeGreaterThan(0);
  expect(f.query.getQueryCache().getAll().length).toBeLessThanOrEqual(32);
  f.query.clear();
});

it('preserves the path while offline and revalidates after reconnecting', async () => {
  const f = fixture(); await ready(); fireEvent.click(folder('alpha')); await ready();
  f.setState('closed'); await screen.findByText('Kuzco is offline'); expect(choose().disabled).toBe(true);
  expect(screen.getByTitle('/home/sam/alpha')).toBeTruthy();
  const before = f.directories.mock.calls.length;
  fireEvent.click(screen.getByRole('button', { name: 'Reconnect' })); await ready();
  expect(f.reconnect).toHaveBeenCalledOnce(); expect(f.directories.mock.calls.length).toBeGreaterThan(before);
  fireEvent.click(choose()); await waitFor(() => expect(f.onSelect).toHaveBeenCalledExactlyOnceWith('/home/sam/alpha'));
});

it('supports arrow selection, Enter navigation and shortcuts without losing keyboard focus', async () => {
  const f = fixture(); await ready();
  folder('alpha').focus(); fireEvent.keyDown(folder('alpha'), { key: 'ArrowDown' });
  expect(folder('beta').getAttribute('aria-pressed')).toBe('true'); expect(document.activeElement).toBe(folder('beta'));
  fireEvent.keyDown(folder('beta'), { key: 'Enter' });
  await screen.findByText('No subfolders here'); await ready();
  const filter = screen.getByRole('textbox', { name: 'Filter folders by prefix' });
  await waitFor(() => expect(document.activeElement).toBe(filter));
  fireEvent.keyDown(filter, { key: 'G', metaKey: true, shiftKey: true });
  const path = screen.getByRole('textbox', { name: 'Remote path' }); expect(document.activeElement).toBe(path);
  fireEvent.keyDown(path, { key: 'Escape' }); expect(f.onClose).not.toHaveBeenCalled();
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Edit path' }));
  await ready();
  fireEvent.keyDown(screen.getByRole('button', { name: 'Edit path' }), { key: 'Enter', metaKey: true });
  await waitFor(() => expect(f.onSelect).toHaveBeenCalledExactlyOnceWith('/home/sam/beta'));
});

it('cancels pending reads when dismissed and never confirms a stale selection', async () => {
  const f = fixture(); await ready();
  const probe = deferred<ReturnType<typeof result>>();
  f.directories.mockImplementationOnce(() => probe.promise);
  fireEvent.click(folder('alpha'));
  await waitFor(() => expect(f.directories).toHaveBeenCalledWith(expect.objectContaining({ path: '/home/sam/alpha', limit: 64 }), expect.anything()));
  const signal = f.directories.mock.calls.at(-1)![1]!.signal!;
  fireEvent.click(screen.getByRole('button', { name: 'Open beta' })); await ready();
  expect(signal.aborted).toBe(true);
  await act(async () => probe.resolve(result('/home/sam/alpha')));
  fireEvent.click(choose()); await waitFor(() => expect(f.onSelect).toHaveBeenCalledExactlyOnceWith('/home/sam/beta'));
  const listing = deferred<ReturnType<typeof result>>(); f.directories.mockImplementationOnce(() => listing.promise);
  fireEvent.click(screen.getByRole('button', { name: 'Edit path', exact: true }));
  fireEvent.change(screen.getByLabelText('Remote path'), { target: { value: '/uncached' } });
  fireEvent.click(screen.getByRole('button', { name: 'Go', exact: true }));
  await screen.findByText('Loading folders…');
  const pendingSignal = f.directories.mock.calls.at(-1)![1]!.signal!;
  f.view.unmount(); expect(pendingSignal.aborted).toBe(true);
  await act(async () => listing.resolve(result('/home/sam')));
});

it('builds host paths independently of the browser OS and bounds recent folders', () => {
  expect(directoryCrumbs('/home/sam')).toEqual([{ label: '/', path: '/' }, { label: 'home', path: '/home' }, { label: 'sam', path: '/home/sam' }]);
  expect(directoryCrumbs('C:\\Users\\sam').map(crumb => crumb.path)).toEqual(['C:\\', 'C:\\Users', 'C:\\Users\\sam']);
  expect(directoryCrumbs('\\\\server\\share\\project').map(crumb => crumb.path)).toEqual(['\\\\server\\share\\', '\\\\server\\share\\project']);
  const items = Array.from({ length: 8 }, (_, i) => ({ cwd: `/project/${i}`, updated_at: `2026-09-${10 + i}` }));
  expect(recentDirectories([...items, items[7], { cwd: 'relative', updated_at: '2027' }])).toEqual(['/project/7', '/project/6', '/project/5', '/project/4', '/project/3']);
});

it('retains rows and breadcrumbs during a slow navigation and reuses Back without another read', async () => {
  const f = fixture(); await ready();
  const listing = deferred<ReturnType<typeof result>>();
  f.directories.mockImplementationOnce(() => listing.promise);
  fireEvent.click(screen.getByRole('button', { name: 'Open alpha' }));
  await screen.findByText('Loading folders…');
  expect(folder('beta')).toBeTruthy();
  expect((folder('beta') as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByRole('navigation', { name: 'Folder path' }).textContent).not.toContain('alpha');
  expect(choose().disabled).toBe(true);
  await act(async () => listing.resolve(result('/home/sam/alpha'))); await ready();
  const homeReads = () => f.directories.mock.calls.filter(([params]) => params.path === '/home/sam').length;
  const before = homeReads();
  fireEvent.click(screen.getByRole('button', { name: 'Back', exact: true }));
  await ready(); expect(folder('beta')).toBeTruthy(); expect(homeReads()).toBe(before);
});

it('revalidates an expired choice before confirming and rejects a late result after navigation', async () => {
  const f = fixture(); await ready();
  const options = directoryOptions(f.client, { path: '/home/sam' });
  act(() => { f.query.setQueryData(options.queryKey, result('/home/sam', ['alpha', 'beta']), { updatedAt: Date.now() - 20_000 }); });
  const check = deferred<ReturnType<typeof result>>();
  f.directories.mockImplementationOnce(() => check.promise);
  fireEvent.click(choose()); await screen.findByText('Checking folder…');
  expect(f.onSelect).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Home', exact: true }));
  await act(async () => check.resolve(result('/home/sam')));
  expect(f.onSelect).not.toHaveBeenCalled();
});
