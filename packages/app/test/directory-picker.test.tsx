import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import type { Client } from '@whip/sdk';
import { DirectoryPicker } from '../src/directory-picker';
import { providerFixture } from './provider-fixture';

beforeEach(() => { vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })); });
afterEach(() => vi.unstubAllGlobals());
function fixture() {
  let finish: (value: { path: string }) => void = () => {};
  const pickDirectory = vi.fn(() => new Promise<{ path: string }>(resolve => { finish = resolve; }));
  const directories = vi.fn(async ({ path }: { path?: string }) => ({ path: path || '/start', entries: [], has_more: false }));
  const client = { runtimeID: 'local', processEpoch: 'boot', trees: { recent: async () => ({ items: [] }) }, call: (method: string, params: never, options: never) => method === 'host.directory.pick' ? pickDirectory() : directories(params, options) } as unknown as Client;
  const onSelect = vi.fn(), submit = vi.fn();
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const tree = (target = client, native = false, sessionTrigger = false) => <ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={query}>
    <form onSubmit={event => { event.preventDefault(); submit(); }}><DirectoryPicker connected client={target} native={native} sessionTrigger={sessionTrigger} value="/start" onSelect={onSelect} disabled={false} /></form>
  </QueryClientProvider></UIProvider></ThemeProvider>;
  return { client, tree, onSelect, submit, pickDirectory, directories, finish: (path: string) => finish({ path }) };
}
it.each([false, true])('browses remote directories without submitting the session form (session trigger: %s)', async sessionTrigger => {
  const f = fixture(); render(f.tree(f.client, false, sessionTrigger));
  expect(screen.queryByRole('button', { name: 'Choose folder…', exact: true })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: sessionTrigger ? 'Project folder' : 'Browse host', exact: true }));
  fireEvent.click(screen.getByRole('button', { name: 'Edit path', exact: true }));
  fireEvent.change(screen.getByLabelText('Remote path'), { target: { value: '/remote/project' } });
  fireEvent.click(screen.getByRole('button', { name: 'Go', exact: true }));
  await waitFor(() => expect(f.directories).toHaveBeenCalledWith(expect.objectContaining({ path: '/remote/project' }), expect.anything()));
  await screen.findByText('/remote/project', { exact: true });
  await waitFor(() => expect(screen.getByRole('button', { name: 'Choose folder', exact: true }).hasAttribute('disabled')).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder', exact: true }));
  await waitFor(() => expect(f.onSelect).toHaveBeenCalledExactlyOnceWith('/remote/project'));
  expect(f.submit).not.toHaveBeenCalled();
  expect(f.pickDirectory).not.toHaveBeenCalled();
});
it('ignores a native picker response after the target client changes', async () => {
  const f = fixture(), view = render(f.tree(f.client, true));
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder…', exact: true }));
  const remote = { ...f.client, runtimeID: 'remote' } as unknown as Client;
  view.rerender(f.tree(remote, false));
  await act(async () => f.finish('/local/stale'));
  expect(f.onSelect).not.toHaveBeenCalled();
});

it('uses native bounded host directory metadata and native recent roots', async () => {
  const f = await providerFixture(); const select = vi.fn();
  f.data.handlers['host.directories.list'] = request => {
    if (request.method !== 'host.directories.list') throw new Error('Wrong operation');
    return { path: request.params.path, parent: '/', entries: [], next_after: null, has_more: false, truncated: false };
  };
  f.data.handlers['trees.recent'] = () => ({ catalog_revision: '9007199254740993', items: [{ root_id: 'root', working_directory: '/recent/native', model: { provider: 'openrouter', name: 'fixture', effort: '' }, last_activity_at: '2026-09-28T00:00:00Z', tree: { id: 'tree', engine: 'starlark', metadata: { title: null, archived: false, pinned: false }, revision: '9007199254740994', created_at: '2026-09-28T00:00:00Z' } }], has_more: false });
  f.mount(<DirectoryPicker connected client={f.client} native={false} value="/start" onSelect={select} disabled={false} />);
  fireEvent.click(screen.getByRole('button', { name: 'Browse host', exact: true }));
  await screen.findByRole('button', { name: 'native', exact: true });
  expect(f.calls.find(call => call.method === 'trees.recent')?.params).toEqual({ limit: 50 });
  expect(f.calls.find(call => call.method === 'host.directories.list')?.params).toEqual({ path: '/start', after: '', prefix: '', show_hidden: false, limit: 64 });
  fireEvent.click(screen.getByRole('button', { name: 'native', exact: true }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Choose folder', exact: true })).toHaveProperty('disabled', false));
  fireEvent.click(screen.getByRole('button', { name: 'Choose folder', exact: true }));
  await waitFor(() => expect(select).toHaveBeenCalledExactlyOnceWith('/recent/native'));
  expect(f.calls.every(call => ['initialize', 'trees.recent', 'host.directories.list'].includes(call.method))).toBe(true);
});
it('discards a native picker answer when its host goes offline', async () => {
  const f = await providerFixture(); const select = vi.fn(); let finish!: (value: unknown) => void;
  f.data.handlers['host.directory.pick'] = () => new Promise(resolve => { finish = resolve; });
  const app = (connected: boolean) => <DirectoryPicker connected={connected} client={f.client} native value="/start" onSelect={select} disabled={false} />;
  const mounted = f.mount(app(true)); fireEvent.click(screen.getByRole('button', { name: 'Choose folder…', exact: true }));
  await waitFor(() => expect(f.count('host.directory.pick')).toBe(1));
  mounted.rerender(f.wrap(app(false)));
  await act(async () => finish({ path: '/stale', cancelled: false }));
  expect(select).not.toHaveBeenCalled();
});
