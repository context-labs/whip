import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { Welcome } from '../src/welcome';

const route = vi.hoisted(() => ({ search: {} as { cwd?: string; runtimeId?: string }, location: { state: { __TSR_key: 'initial' } }, navigate: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useSearch: () => route.search,
  useLocation: ({ select }: { select(value: typeof route.location): unknown }) => select(route.location),
  useNavigate: () => route.navigate,
  useRouter: () => ({ state: { location: route.location } }),
}));
vi.mock('../src/directory-picker', () => ({ DirectoryPicker: () => null }));
beforeEach(() => { vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })); route.search = { cwd: '/repo', runtimeId: 'host' }; route.location = { state: { __TSR_key: 'initial' } }; route.navigate.mockClear(); });
afterEach(() => vi.unstubAllGlobals());
function fixture() {
  let connection = { state: 'connected', info: { runtime_id: 'host' } };
  const create = vi.fn(() => 'command');
  const client = { subscribe: () => () => {}, getSnapshot: () => connection, providers: { catalogs: vi.fn(async () => ({ result: { models: {} } })) }, sessions: { create } };
  const otherConnection = { state: 'connected', info: { runtime_id: 'another' } };
  const other = { ...client, getSnapshot: () => otherConnection };
  const state = { hosts: [
    { id: 'local', name: 'Local', local: true, runtimeId: 'host', state: 'connected', client },
    { id: 'remote', name: 'Kuzco', local: false, runtimeId: 'another', state: 'connected', client: other },
  ] };
  const run = vi.fn(async () => ({ result: { root_id: 'created' } }));
  const runtime = { connections: { isAttached: () => true }, subscribe: () => () => {}, getSnapshot: () => state, lastSession: () => undefined, tabs: { canOpen: () => true }, run, report: vi.fn() } as unknown as AppRuntime;
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const tree = () => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={query}><Welcome /></QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
  const view = render(tree());
  return { create, run, rerender: () => view.rerender(tree()), host: (id: string) => { connection = { ...connection, info: { runtime_id: id } }; view.rerender(tree()); } };
}
it('prefills without submitting, preserves edits on rerender, and clears on new route or host', async () => {
  const f = fixture(); const input = screen.getByLabelText('Working directory on Local') as HTMLInputElement;
  expect(input.value).toBe('/repo'); expect(f.create).not.toHaveBeenCalled();
  fireEvent.change(input, { target: { value: '/typed' } }); f.rerender(); expect(input.value).toBe('/typed');
  route.location = { state: { __TSR_key: 'explicit-new' } }; route.search = {}; f.rerender(); expect(input.value).toBe('');
  route.search = { cwd: '/second', runtimeId: 'host' }; f.rerender(); expect(input.value).toBe('/second');
  fireEvent.click(screen.getByRole('button', { name: 'Remote', exact: true }));
  expect((screen.getByLabelText('Working directory on Kuzco') as HTMLInputElement).value).toBe('');
  expect(f.create).not.toHaveBeenCalled();
});
it('submits the edited directory only when the existing form is submitted', async () => {
  const f = fixture();
  fireEvent.change(screen.getByLabelText('Working directory on Local'), { target: { value: '/edited' } });
  fireEvent.click(screen.getByRole('button', { name: 'Start a session' }));
  await waitFor(() => expect(f.create).toHaveBeenCalledExactlyOnceWith({ cwd: '/edited', model: '', provider: '' }));
  await waitFor(() => expect(route.navigate).toHaveBeenCalledWith({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'host', rootId: 'created' }, search: {} }));
});
