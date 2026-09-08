import { expect, it, vi } from 'vitest';
import type { AppRuntime } from '../src/runtime';
import { localProfile, urlProfile, type ConnectionProfile } from '../src/platform';
import { createSessionNavigator } from '../src/session-tab-routing';

function client(runtimeId: string, state = 'connected') {
  return { getSnapshot: () => ({ state, info: { runtime_id: runtimeId } }) };
}
function fixture() {
  const mac = { ...localProfile, runtimeId: 'mac' };
  const remote = { ...urlProfile('https://remote.example'), runtimeId: 'remote' };
  const state: { connection: ConnectionProfile; hosts: ConnectionProfile[]; client?: ReturnType<typeof client> } = {
    connection: mac, hosts: [mac, remote], client: client('mac'),
  };
  const connect = vi.fn(async (profile: ConnectionProfile) => { state.connection = profile; state.client = client(profile.runtimeId!); });
  const runtime = { getSnapshot: () => state, connect, report: vi.fn() };
  const navigate = vi.fn();
  return { mac, remote, state, runtime, navigate, navigator: createSessionNavigator(runtime as unknown as AppRuntime, navigate) };
}
function deferred() {
  let resolve!: () => void, reject!: (error: Error) => void;
  const promise = new Promise<void>((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}

it('opens the current verified host without reconnecting and preserves shared session search', async () => {
  const f = fixture(); await f.navigator.open('/h/mac/s/root?agent=child&view=repl');
  expect(f.runtime.connect).not.toHaveBeenCalled();
  expect(f.navigate).toHaveBeenCalledExactlyOnceWith('/h/mac/s/root?agent=child&view=repl');
});

it('connects only to the saved profile carrying the requested runtime identity', async () => {
  const f = fixture(); await f.navigator.open('/h/remote/s/root');
  expect(f.runtime.connect).toHaveBeenCalledExactlyOnceWith(f.remote);
  expect(f.runtime.connect.mock.calls[0]![0].runtimeId).toBe('remote');
  expect(f.navigate).toHaveBeenCalledExactlyOnceWith('/h/remote/s/root');
});

it('refuses unknown hosts without using connection hints from a link', async () => {
  const f = fixture(); await f.navigator.open('/h/unknown/s/root?host=https://untrusted.example');
  expect(f.runtime.connect).not.toHaveBeenCalled(); expect(f.navigate).not.toHaveBeenCalled();
  expect(f.runtime.report).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining('unknown execution host') }));
  expect(f.state.connection).toBe(f.mac);
});

it('leaves connection error reporting to the runtime without adopting the replacement host', async () => {
  const f = fixture(); f.runtime.connect.mockRejectedValueOnce(new Error('Runtime identity changed'));
  await f.navigator.open('/h/remote/s/root');
  expect(f.navigate).not.toHaveBeenCalled(); expect(f.state.connection).toBe(f.mac);
  expect(f.runtime.report).not.toHaveBeenCalled();
});

it('does not report a late connection failure into a manually selected host', async () => {
  const f = fixture(); const completion = deferred();
  f.runtime.connect.mockImplementationOnce(profile => { f.state.connection = profile; return completion.promise; });
  const opened = f.navigator.open('/h/remote/s/root');
  f.state.connection = f.mac; f.state.client = client('mac');
  completion.reject(new Error('Cancelled while switching hosts')); await opened;
  expect(f.navigate).not.toHaveBeenCalled(); expect(f.runtime.report).not.toHaveBeenCalled();
});

it('does not navigate a late connection result after the user selected a different host', async () => {
  const f = fixture(); const completion = deferred();
  f.runtime.connect.mockImplementationOnce(() => completion.promise);
  const opened = f.navigator.open('/h/remote/s/root');
  f.state.connection = { ...urlProfile('https://another.example'), runtimeId: 'another' };
  f.state.client = client('another'); completion.resolve(); await opened;
  expect(f.navigate).not.toHaveBeenCalled(); expect(f.runtime.report).not.toHaveBeenCalled();
});

it('lets a newer native link supersede pending navigation and ignores its late failure', async () => {
  const f = fixture(); const completion = deferred();
  f.runtime.connect.mockImplementationOnce(profile => { f.state.connection = profile; f.state.client = undefined; return completion.promise; });
  const old = f.navigator.open('/h/remote/s/old');
  await f.navigator.open('/h/mac/s/current');
  completion.reject(new Error('The old connection failed')); await old;
  expect(f.navigate).toHaveBeenCalledExactlyOnceWith('/h/mac/s/current'); expect(f.runtime.report).not.toHaveBeenCalled();
});

it('does not override a newly selected profile even when it reaches the same runtime', async () => {
  const f = fixture(); const completion = deferred();
  f.runtime.connect.mockImplementationOnce(() => completion.promise);
  const opened = f.navigator.open('/h/remote/s/root');
  f.state.connection = { ...urlProfile('https://another-route.example'), runtimeId: 'remote' };
  f.state.client = client('remote'); completion.resolve(); await opened;
  expect(f.navigate).not.toHaveBeenCalled(); expect(f.runtime.report).not.toHaveBeenCalled();
});

it('does not navigate using retained identity while the final connection is reconnecting', async () => {
  const f = fixture();
  f.runtime.connect.mockImplementationOnce(async () => { f.state.client = client('remote', 'reconnecting'); });
  await f.navigator.open('/h/remote/s/root');
  expect(f.navigate).not.toHaveBeenCalled();
});

it('cancels pending navigation on disposal and ignores later calls on the disposed navigator', async () => {
  const f = fixture(); const completion = deferred();
  f.runtime.connect.mockImplementationOnce(() => completion.promise);
  const opened = f.navigator.open('/h/remote/s/root');
  f.navigator.dispose(); completion.reject(new Error('Connection cancelled')); await opened;
  await f.navigator.open('/h/mac/s/later');
  expect(f.runtime.connect).toHaveBeenCalledOnce(); expect(f.navigate).not.toHaveBeenCalled(); expect(f.runtime.report).not.toHaveBeenCalled();
});

for (const path of ['https://elsewhere.example/h/mac/s/root', '//elsewhere.example/h/mac/s/root', '/settings', '/h/mac/s/root#fragment', '/h/mac/s/']) {
  it(`rejects a non-session native route: ${path}`, async () => {
    const f = fixture(); await f.navigator.open(path);
    expect(f.runtime.connect).not.toHaveBeenCalled(); expect(f.navigate).not.toHaveBeenCalled();
    expect(f.runtime.report).toHaveBeenCalledWith(expect.objectContaining({ message: 'Invalid session link' }));
  });
}
