import { expect, it, vi } from 'vitest';
import type { AppRuntime } from '../src/runtime';
import { createSessionNavigator } from '../src/session-tab-routing';

function client(runtimeId: string, state = 'connected') {
  return { getSnapshot: () => ({ state, info: { runtime_id: runtimeId } }) };
}
function fixture() {
  const mac = { id: 'local', runtimeId: 'mac', client: client('mac') };
  const remote = { id: 'ssh:remote', runtimeId: 'remote', client: undefined as ReturnType<typeof client> | undefined };
  const state = { hosts: [mac, remote] };
  const connect = vi.fn(async (id: string) => { const host = state.hosts.find(host => host.id === id)!; host.client = client(host.runtimeId); });
  const runtime = { getSnapshot: () => state, connections: { connect, host: (id: string) => state.hosts.find(host => host.runtimeId === id) }, report: vi.fn() };
  const navigate = vi.fn();
  let location = '/';
  return { mac, remote, state, connect, runtime, navigate, setLocation(value: string) { location = value; },
    navigator: createSessionNavigator(runtime as unknown as AppRuntime, navigate, () => location) };
}
function deferred() {
  let resolve!: () => void, reject!: (error: Error) => void;
  const promise = new Promise<void>((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}

it('opens a verified host without reconnecting and preserves shared session search', async () => {
  const f = fixture(); await f.navigator.open('/h/mac/s/root?agent=child&view=repl');
  expect(f.connect).not.toHaveBeenCalled();
  expect(f.navigate).toHaveBeenCalledExactlyOnceWith('/h/mac/s/root?agent=child&view=repl');
});
it('connects the saved destination without disturbing another attached host', async () => {
  const f = fixture(); const local = f.mac.client;
  await f.navigator.open('/h/remote/s/root');
  expect(f.connect).toHaveBeenCalledExactlyOnceWith(f.remote.id);
  expect(f.mac.client).toBe(local);
  expect(f.navigate).toHaveBeenCalledExactlyOnceWith('/h/remote/s/root');
});
it('refuses unknown hosts without trusting endpoint hints in a link', async () => {
  const f = fixture(); await f.navigator.open('/h/unknown/s/root?host=https://untrusted.example');
  expect(f.connect).not.toHaveBeenCalled(); expect(f.navigate).not.toHaveBeenCalled();
  expect(f.runtime.report).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining('unknown execution host') }));
});
it('keeps failed connection reporting scoped to its host record', async () => {
  const f = fixture(); f.connect.mockRejectedValueOnce(new Error('Runtime identity changed'));
  await f.navigator.open('/h/remote/s/root');
  expect(f.navigate).not.toHaveBeenCalled(); expect(f.runtime.report).not.toHaveBeenCalled();
});
it.each(['resolve', 'reject'] as const)('does not override manual navigation when pending setup settles with %s', async outcome => {
  const f = fixture(); const completion = deferred(); f.connect.mockImplementationOnce(() => completion.promise);
  const opened = f.navigator.open('/h/remote/s/root'); f.setLocation('/settings');
  f.remote.client = client('remote');
  if (outcome === 'resolve') completion.resolve(); else completion.reject(new Error('Connection cancelled'));
  await opened; expect(f.navigate).not.toHaveBeenCalled(); expect(f.runtime.report).not.toHaveBeenCalled();
});
it('lets a newer native link supersede a pending navigation', async () => {
  const f = fixture(); const completion = deferred(); f.connect.mockImplementationOnce(() => completion.promise);
  const old = f.navigator.open('/h/remote/s/old'); await f.navigator.open('/h/mac/s/current');
  completion.reject(new Error('The old connection failed')); await old;
  expect(f.navigate).toHaveBeenCalledExactlyOnceWith('/h/mac/s/current'); expect(f.runtime.report).not.toHaveBeenCalled();
});
it.each(['replacement', 'reconnecting', 'removed'] as const)('does not navigate a late %s destination', async changed => {
  const f = fixture(); const completion = deferred(); f.connect.mockImplementationOnce(() => completion.promise);
  const opened = f.navigator.open('/h/remote/s/root');
  if (changed === 'replacement') f.remote.runtimeId = 'other';
  else if (changed === 'removed') f.state.hosts = [f.mac];
  else f.remote.client = client('remote', 'reconnecting');
  completion.resolve(); await opened; expect(f.navigate).not.toHaveBeenCalled();
});
it('cancels pending navigation on disposal and ignores later native links', async () => {
  const f = fixture(); const completion = deferred(); f.connect.mockImplementationOnce(() => completion.promise);
  const opened = f.navigator.open('/h/remote/s/root'); f.navigator.dispose();
  completion.reject(new Error('Connection cancelled')); await opened; await f.navigator.open('/h/mac/s/later');
  expect(f.connect).toHaveBeenCalledOnce(); expect(f.navigate).not.toHaveBeenCalled(); expect(f.runtime.report).not.toHaveBeenCalled();
});
for (const path of ['https://elsewhere.example/h/mac/s/root', '//elsewhere.example/h/mac/s/root', '/settings', '/h/mac/s/root#fragment', '/h/mac/s/']) {
  it(`rejects a non-session native route: ${path}`, async () => {
    const f = fixture(); await f.navigator.open(path);
    expect(f.connect).not.toHaveBeenCalled(); expect(f.navigate).not.toHaveBeenCalled();
    expect(f.runtime.report).toHaveBeenCalledWith(expect.objectContaining({ message: 'Invalid session link' }));
  });
}
