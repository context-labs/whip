import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import { ThemeProvider, UIProvider } from '@whip/ui';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import { Welcome } from '../src/welcome';
import { localProfile, urlProfile, type ConnectionProfile } from '../src/platform';

const route = vi.hoisted(() => ({ search: {} as { cwd?: string; runtimeId?: string }, location: { state: { __TSR_key: 'initial' } }, navigate: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useSearch: () => route.search,
  useLocation: ({ select }: { select(value: typeof route.location): unknown }) => select(route.location),
  useNavigate: () => route.navigate,
  useRouter: () => ({ get state() { return { location: route.location }; } }),
}));
vi.mock('../src/directory-picker', () => ({ DirectoryPicker: () => <button>Browse host</button> }));
beforeEach(() => { vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} })); route.search = { cwd: '/repo', runtimeId: 'host' }; route.location = { state: { __TSR_key: 'initial' } }; route.navigate.mockClear(); });
afterEach(() => vi.unstubAllGlobals());
function fixture(profile: ConnectionProfile = urlProfile('https://host.example')) {
  let connection = { state: 'connected', info: { runtime_id: 'host' } };
  const create = vi.fn(() => 'command');
  const client = { subscribe: () => () => {}, getSnapshot: () => connection, providers: { catalogs: vi.fn(async () => ({ result: { models: {} } })) }, sessions: { create } };
  const state = { client, connection: profile };
  const run = vi.fn(async () => ({ result: { root_id: 'created' } }));
  const pickDirectory = vi.fn<() => Promise<string | undefined>>().mockResolvedValue('/native/project');
  const report = vi.fn();
  const runtime = { platform: { pickDirectory }, subscribe: () => () => {}, getSnapshot: () => state, lastSession: () => undefined, tabs: { canOpen: () => true }, run, report } as unknown as AppRuntime;
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const tree = () => <RuntimeContext.Provider value={runtime}><ThemeProvider initialTheme="light"><UIProvider><QueryClientProvider client={query}><Welcome /></QueryClientProvider></UIProvider></ThemeProvider></RuntimeContext.Provider>;
  const view = render(tree());
  return { create, run, report, pickDirectory, unmount: view.unmount, rerender: () => view.rerender(tree()),
    profile: (profile: ConnectionProfile) => { state.connection = profile; view.rerender(tree()); },
    host: (id: string) => { connection = { ...connection, info: { runtime_id: id } }; view.rerender(tree()); } };
}
it('prefills without submitting, preserves edits on rerender, and clears on new route or host', async () => {
  const f = fixture(); const input = screen.getByLabelText('Working directory on the host') as HTMLInputElement;
  expect(input.value).toBe('/repo'); expect(f.create).not.toHaveBeenCalled();
  fireEvent.change(input, { target: { value: '/typed' } }); f.rerender(); expect(input.value).toBe('/typed');
  route.location = { state: { __TSR_key: 'explicit-new' } }; route.search = {}; f.rerender(); expect(input.value).toBe('');
  route.search = { cwd: '/second', runtimeId: 'host' }; f.rerender(); expect(input.value).toBe('/second');
  f.host('another'); expect(input.value).toBe('');
  expect(f.create).not.toHaveBeenCalled();
});
it('submits the edited directory only when the existing form is submitted', async () => {
  const f = fixture();
  fireEvent.change(screen.getByLabelText('Working directory on the host'), { target: { value: '/edited' } });
  fireEvent.click(screen.getByRole('button', { name: 'Start a session' }));
  await waitFor(() => expect(f.create).toHaveBeenCalledExactlyOnceWith({ cwd: '/edited', model: '', provider: '' }));
  await waitFor(() => expect(route.navigate).toHaveBeenCalledWith({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: 'host', rootId: 'created' }, search: {} }));
});

it('uses the native chooser only for This Mac and treats cancellation as no change', async () => {
  const f = fixture(localProfile);
  expect(screen.queryByRole('button', { name: 'Browse host' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Browse this Mac' }));
  await waitFor(() => expect((screen.getByLabelText('Working directory on the host') as HTMLInputElement).value).toBe('/native/project'));
  expect(f.pickDirectory).toHaveBeenCalledOnce(); expect(f.create).not.toHaveBeenCalled();
  f.pickDirectory.mockResolvedValueOnce(undefined);
  fireEvent.click(screen.getByRole('button', { name: 'Browse this Mac' }));
  await waitFor(() => expect(f.pickDirectory).toHaveBeenCalledTimes(2));
  expect((screen.getByLabelText('Working directory on the host') as HTMLInputElement).value).toBe('/native/project');
  expect(f.report).not.toHaveBeenCalled();
});

for (const profile of [urlProfile('https://remote.example'), { id: 'ssh:remote', label: 'Remote', target: { kind: 'ssh', host: 'remote' } } satisfies ConnectionProfile]) {
  it(`keeps ${profile.target.kind} directory selection on the execution host`, () => {
    const f = fixture(profile);
    expect(screen.queryByRole('button', { name: 'Browse this Mac' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Browse host' }));
    expect(f.pickDirectory).not.toHaveBeenCalled();
  });
}

it('reports a native chooser failure and allows another attempt', async () => {
  const f = fixture(localProfile);
  f.pickDirectory.mockRejectedValueOnce(new Error('The folder picker is unavailable'));
  fireEvent.click(screen.getByRole('button', { name: 'Browse this Mac' }));
  await waitFor(() => expect(f.report).toHaveBeenCalledWith(expect.objectContaining({ message: 'The folder picker is unavailable' })));
  expect((screen.getByLabelText('Working directory on the host') as HTMLInputElement).value).toBe('/repo');
  expect((screen.getByRole('button', { name: 'Browse this Mac' }) as HTMLButtonElement).disabled).toBe(false);
});

for (const change of ['profile', 'runtime', 'route', 'route-same-key', 'unmount'] as const) {
  it(`ignores a late native folder selection after ${change} changes`, async () => {
    const f = fixture(localProfile); let finish!: (path: string) => void;
    f.pickDirectory.mockReturnValueOnce(new Promise(resolve => { finish = resolve; }));
    fireEvent.click(screen.getByRole('button', { name: 'Browse this Mac' }));
    if (change === 'profile') f.profile(urlProfile('https://remote.example'));
    else if (change === 'runtime') f.host('another-runtime');
    else if (change === 'route' || change === 'route-same-key') {
      route.location = { state: { __TSR_key: change === 'route' ? 'different-page' : 'initial' } }; f.rerender();
    }
    else f.unmount();
    await act(async () => finish('/wrong-local-folder'));
    const input = screen.queryByLabelText('Working directory on the host') as HTMLInputElement | null;
    expect(input?.value).not.toBe('/wrong-local-folder'); expect(f.report).not.toHaveBeenCalled();
    const chooser = screen.queryByRole('button', { name: 'Browse this Mac' }) as HTMLButtonElement | null;
    if (chooser) expect(chooser.disabled).toBe(false);
  });
}
