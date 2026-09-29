import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { Client } from '@whip/sdk';
import { RemoteDirectoryDialog, directoryCrumbs, recentDirectories } from '../src/remote-directory-dialog';
import { directoryOptions } from '../src/directory-queries';

beforeEach(() => vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })));
afterEach(() => vi.unstubAllGlobals());
const result = (path: string, names: string[] = []) => ({ path, parent: path.slice(0, path.lastIndexOf('/')) || '/', entries: names.map(name => ({ name, path: `${path}/${name}` })), has_more: false, next_after: '' });
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done; }); return { promise, resolve }; }
function fixture() {
  let online = true;
  const directories = vi.fn(async (params: { path?: string; limit?: number; prefix?: string; show_hidden?: boolean; after?: string }, _options?: { signal?: AbortSignal }) => result(params.path || '/home/sam', params.path === '/home/sam' ? ['alpha', 'beta', 'private'] : []));
  const createDirectory = vi.fn(async ({ parent, name }: { parent: string; name: string }) => ({ path: `${parent}/${name}` }));
  const client = { createHostDirectory: createDirectory, runtimeID: 'kuzco', processEpoch: 'boot', trees: { recent: async () => ({ items: [] }) }, call: (_method: string, params: never, options: never) => directories(params, options) } as unknown as Client;
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const onSelect = vi.fn(), onClose = vi.fn();
  const reconnect = vi.fn(async () => { setState('connected'); });
  function setState(state: string) { act(() => { online = state === 'connected'; view.rerender(tree()); }); }
  function setRuntime(runtimeID: string) { view.rerender(tree({ ...client, runtimeID } as Client)); }
  const tree = (activeClient = client, disabled = false) => <ThemeProvider initialTheme="dark"><UIProvider><QueryClientProvider client={query}><RemoteDirectoryDialog connected={online} client={activeClient} host={{ name: 'Kuzco', detail: 'sam@kuzco', reconnect }} value="/home/sam" disabled={disabled} onSelect={onSelect} onClose={onClose} /></QueryClientProvider></UIProvider></ThemeProvider>;
  const view = render(tree());
  return { client, directories, createDirectory, query, onSelect, onClose, reconnect, setState, setRuntime, view, rerender: (activeClient = client, disabled = false) => view.rerender(tree(activeClient, disabled)) };
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
  await waitFor(() => expect(f.directories).toHaveBeenCalledWith(expect.objectContaining({ prefix: 'a', after: '', show_hidden: true }), expect.anything()));
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
  const items = Array.from({ length: 8 }, (_, i) => ({ working_directory: `/project/${7 - i}` }));
  expect(recentDirectories([...items, items[7], { working_directory: 'relative' }] as never)).toEqual(['/project/7', '/project/6', '/project/5', '/project/4', '/project/3']);
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

const newFolder = () => screen.getByRole('button', { name: 'New folder', exact: true }) as HTMLButtonElement;
function nameFolder(name = 'project') {
  fireEvent.click(newFolder());
  const input = screen.getByRole('textbox', { name: 'Folder name' }) as HTMLInputElement;
  fireEvent.change(input, { target: { value: name } });
  return input;
}
const submitFolder = (input: HTMLElement) => fireEvent.submit(input.closest('form')!);

it('creates in the displayed parent, opens the result, and still requires explicit choice', async () => {
  const f = fixture(); await ready();
  fireEvent.click(folder('alpha')); await ready();
  const input = nameFolder();
  expect(document.activeElement).toBe(input);
  expect(screen.getByRole('form', { name: 'New folder' }).textContent).toContain('Create in Kuzco/home/sam');
  expect(choose().disabled).toBe(true);
  submitFolder(input);
  await screen.findByText('Folder created. Choose folder to use it.'); await ready();
  expect(f.createDirectory).toHaveBeenCalledExactlyOnceWith({ parent: '/home/sam', name: 'project' });
  expect(screen.getByRole('navigation', { name: 'Folder path' }).textContent).toContain('project');
  expect(f.onSelect).not.toHaveBeenCalled();
  fireEvent.click(choose());
  await waitFor(() => expect(f.onSelect).toHaveBeenCalledExactlyOnceWith('/home/sam/project'));
});

it('guards duplicate submissions and retains a conflict inline for correction without retries', async () => {
  const f = fixture(); await ready();
  let reject!: (error: Error) => void;
  f.createDirectory.mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail; }));
  const input = nameFolder('alpha');
  submitFolder(input); submitFolder(input);
  expect(f.createDirectory).toHaveBeenCalledTimes(1);
  expect(input.disabled).toBe(true);
  expect(newFolder().disabled).toBe(true);
  expect(within(screen.getByRole('form', { name: 'New folder' })).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
  await act(async () => reject(new Error('A folder already exists')));
  await screen.findByRole('alert');
  expect(screen.getByRole('alert').textContent).toBe('A folder already exists');
  expect(input.value).toBe('alpha'); expect(document.activeElement).toBe(input);
  expect(f.createDirectory).toHaveBeenCalledTimes(1);
  fireEvent.change(input, { target: { value: 'corrected' } }); submitFolder(input);
  await ready(); expect(f.createDirectory).toHaveBeenCalledTimes(2);
  expect(f.onSelect).not.toHaveBeenCalled();
});

it.each(['', '   ', '.', '..', 'nested/name', 'nested\\name', '/absolute', 'C:drive', 'nul\0name', 'é'.repeat(128)])('rejects invalid folder name %j without a mutation', async name => {
  const f = fixture(); await ready();
  const input = nameFolder(name);
  expect(within(screen.getByRole('form', { name: 'New folder' })).getByRole('button', { name: 'Create' }).hasAttribute('disabled')).toBe(true);
  submitFolder(input); expect(f.createDirectory).not.toHaveBeenCalled();
});

it('cancels the form with Escape or Cancel and restores focus without mutating', async () => {
  const f = fixture(); await ready();
  const input = nameFolder();
  fireEvent.keyDown(input, { key: 'Escape' });
  expect(screen.queryByRole('form', { name: 'New folder' })).toBeNull();
  expect(document.activeElement).toBe(newFolder()); expect(f.onClose).not.toHaveBeenCalled();
  nameFolder();
  fireEvent.click(within(screen.getByRole('form', { name: 'New folder' })).getByRole('button', { name: 'Cancel' }));
  expect(document.activeElement).toBe(newFolder());
  expect(f.createDirectory).not.toHaveBeenCalled(); expect(choose().disabled).toBe(false);
});

it('disables creation while offline, disabled, loading, or without a valid listing', async () => {
  const f = fixture(); await ready();
  f.setState('disconnected'); expect(newFolder().disabled).toBe(true);
  f.setState('connected'); await ready();
  f.rerender(f.client, true); expect(newFolder().disabled).toBe(true);
  f.rerender(); await ready();
  const listing = deferred<ReturnType<typeof result>>();
  f.directories.mockImplementationOnce(() => listing.promise);
  fireEvent.click(screen.getByRole('button', { name: 'Open alpha' }));
  expect(newFolder().disabled).toBe(true);
  await act(async () => listing.resolve(result('/home/sam/alpha'))); await ready();
  f.directories.mockRejectedValueOnce(new Error('Permission denied'));
  fireEvent.click(screen.getByRole('button', { name: 'File system', exact: true }));
  await screen.findByText('Can’t open this folder'); expect(newFolder().disabled).toBe(true);
});

it('invalidates every parent page/filter and alias on only the captured runtime and clears filters for hidden creation', async () => {
  const f = fixture(); await ready();
  f.directories.mockImplementation(async params => ({ ...result(params.path === '~' ? '/home/sam' : params.path!, ['alpha']), has_more: !params.after, next_after: 'alpha' }));
  const filter = screen.getByRole('textbox', { name: 'Filter folders by prefix' }) as HTMLInputElement;
  fireEvent.change(filter, { target: { value: 'a' } }); await ready();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Hidden folders' })); await ready();
  fireEvent.click(screen.getByRole('button', { name: 'Next folders' })); await ready();
  const cached = [
    ['directories', 'kuzco', '/home/sam', 'x', false, 'page-2'],
    ['directories', 'kuzco', '~', '', true, ''],
    ['directories', 'kuzco', '/alias', '', false, ''],
  ];
  const other = ['directories', 'other-host', '/home/sam', '', false, ''];
  // The daemon cleans paths but does not resolve symlinks: /alias stays /alias.
  act(() => { for (const key of [...cached, other]) f.query.setQueryData(key, result(key[2] === '~' ? '/home/sam' : key[2] as string)); });
  submitFolder(nameFolder('.hidden'));
  await screen.findByText('Folder created. Choose folder to use it.'); await ready();
  expect(f.createDirectory).toHaveBeenCalledExactlyOnceWith({ parent: '/home/sam', name: '.hidden' });
  expect(filter.value).toBe('');
  expect(screen.getByRole('checkbox', { name: 'Hidden folders' }).getAttribute('aria-checked')).toBe('false');
  expect(f.directories).toHaveBeenCalledWith(expect.objectContaining({ path: '/home/sam/.hidden', prefix: '', after: '', show_hidden: false }), expect.anything());
  for (const key of cached) expect(f.query.getQueryState(key)?.isInvalidated).toBe(true);
  expect(f.query.getQueryState(other)?.isInvalidated).toBe(false);
  expect(f.onSelect).not.toHaveBeenCalled();
});

it('refreshes an actively viewed parent when stale creation succeeds without navigating or selecting', async () => {
  const f = fixture(); await ready();
  const pending = deferred<{ path: string }>();
  f.createDirectory.mockImplementationOnce(() => pending.promise);
  submitFolder(nameFolder());
  fireEvent.click(screen.getByRole('button', { name: 'sam', exact: true })); await ready();
  f.directories.mockImplementation(async params => result(params.path!, params.path === '/home/sam' ? ['alpha', 'project'] : []));
  await act(async () => pending.resolve({ path: '/home/sam/project' }));
  await waitFor(() => expect(folder('project')).toBeTruthy());
  expect(screen.getByRole('navigation', { name: 'Folder path' }).textContent).not.toContain('project');
  expect(f.onSelect).not.toHaveBeenCalled();
  expect(screen.queryByText('Folder created. Choose folder to use it.')).toBeNull();
});

it('keeps a stale failure out of a new creation form after navigation', async () => {
  const f = fixture(); await ready();
  let reject!: (error: Error) => void;
  f.createDirectory.mockImplementationOnce(() => new Promise((_resolve, fail) => { reject = fail; }));
  submitFolder(nameFolder());
  fireEvent.click(screen.getByRole('button', { name: 'File system', exact: true })); await ready();
  const input = nameFolder('another');
  await act(async () => reject(new Error('Old request failed')));
  expect(screen.queryByText('Old request failed')).toBeNull(); expect(input.value).toBe('another');
  fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true });
  expect(f.onSelect).not.toHaveBeenCalled(); expect(f.createDirectory).toHaveBeenCalledTimes(1);
  submitFolder(input); await ready(); expect(f.createDirectory).toHaveBeenCalledTimes(2);
});

it('distinguishes successful creation from a failed destination listing and retries only the read', async () => {
  const f = fixture(); await ready();
  f.directories.mockImplementation(async params => { if (params.path === '/home/sam/project') throw new Error('Read denied'); return result(params.path!); });
  submitFolder(nameFolder());
  await screen.findByText('Folder created, but its contents could not be loaded. Retry the listing; do not create it again.');
  expect(choose().disabled).toBe(true); expect(newFolder().disabled).toBe(true);
  f.directories.mockImplementation(async params => result(params.path!));
  fireEvent.click(screen.getByRole('button', { name: 'Retry' })); await ready();
  expect(f.createDirectory).toHaveBeenCalledTimes(1); expect(f.onSelect).not.toHaveBeenCalled();
});

it.each(['navigation', 'close', 'unmount', 'runtime', 'client', 'disconnect'] as const)('invalidates the original parent but ignores late creation after %s', async boundary => {
  const f = fixture(); await ready();
  const pending = deferred<{ path: string }>();
  f.createDirectory.mockImplementationOnce(() => pending.promise);
  const original = ['directories', 'kuzco', '/home/sam', 'cached', true, 'page'];
  act(() => f.query.setQueryData(original, result('/home/sam')));
  submitFolder(nameFolder());
  if (boundary === 'navigation') fireEvent.click(screen.getByRole('button', { name: 'File system', exact: true }));
  if (boundary === 'close') fireEvent.click(screen.getAllByRole('button', { name: 'Cancel', exact: true }).at(-1)!);
  if (boundary === 'unmount') f.view.unmount();
  if (boundary === 'runtime') f.setRuntime('pacha');
  if (boundary === 'client') f.rerender({ ...f.client } as Client);
  if (boundary === 'disconnect') { f.setState('disconnected'); f.setState('connected'); }
  await act(async () => pending.resolve({ path: '/home/sam/project' }));
  await waitFor(() => expect(f.query.getQueryState(original)?.isInvalidated).toBe(true));
  expect(f.directories.mock.calls.some(([params]) => params.path === '/home/sam/project')).toBe(false);
  expect(f.onSelect).not.toHaveBeenCalled();
  if (boundary !== 'unmount') expect(screen.queryByText('Folder created. Choose folder to use it.')).toBeNull();
});
