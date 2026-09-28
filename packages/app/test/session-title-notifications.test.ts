import { afterEach, expect, it, vi } from 'vitest';
import { QueryObserver } from '@tanstack/react-query';
import type { WhipClient } from '@whip/legacy-sdk';
import { AppRuntime } from '../src/runtime';
import { createFallbackStorage } from '../src/platform';

const clients = vi.hoisted(() => new Map<string, WhipClient>());
vi.mock('@whip/legacy-sdk', async importOriginal => ({ ...await importOriginal<typeof import('@whip/legacy-sdk')>(), createWhipClient: (options: { endpoint: string }) => clients.get(new URL(options.endpoint).hostname) }));
vi.mock('@whip/legacy-sdk/state', () => ({ createSessionListView: () => ({ start: async () => {}, dispose: async () => {} }) }));
const apps: AppRuntime[] = [];
afterEach(() => { apps.splice(0).forEach(app => app.dispose()); clients.clear(); vi.useRealTimers(); });

function host(runtimeId: string, supported = true) {
  const listeners = new Set<() => void>();
  const titles = new Set<(params: { root_id: string }) => void>();
  const snapshot = { state: 'connected', info: { runtime_id: runtimeId, connection_id: '1', negotiated_capabilities: supported ? ['session_title_notifications'] : [] } };
  const client = { getSnapshot: () => snapshot, subscribe: (listener: () => void) => { listeners.add(listener); return () => listeners.delete(listener); },
    onNotification: vi.fn((method: string, listener: (params: { root_id: string }) => void) => {
      expect(method).toBe('sessions.title.changed'); titles.add(listener); return () => titles.delete(listener);
    }),
    configuration: { get: vi.fn(async () => ({ revision: '1', remote_hosts: [] as { id: string; name: string; url: string; runtime_id: string; connect_on_launch: boolean }[] })) },
    providers: { list: async () => ({}), catalogs: async () => ({}) }, sessions: { list: async () => ({ items: [] }) },
    session: vi.fn(), events: { subscribe: vi.fn() }, connect: async () => {}, close: vi.fn(),
  };
  clients.set(runtimeId, client as unknown as WhipClient);
  return { client, titles, snapshot, notify: (root_id = 'unopened') => { for (const listener of titles) listener({ root_id }); },
    state: (state: string, connection_id = snapshot.info.connection_id) => { snapshot.state = state; snapshot.info.connection_id = connection_id; for (const listener of listeners) listener(); } };
}
async function setup(supported = true) {
  vi.useFakeTimers();
  const local = host('local', supported);
  const remote = host('remote', supported);
  local.client.configuration.get.mockResolvedValue({ revision: '1', remote_hosts: [{ id: 'remote', name: 'Remote', url: 'http://remote', runtime_id: 'remote', connect_on_launch: false }] });
  const app = new AppRuntime({ defaultEndpoint: 'http://local', storage: createFallbackStorage(() => { throw new Error('memory only'); }, () => {}), copy: async () => {}, download: async () => {}, openExternal: async () => {} });
  apps.push(app);
  await app.connect(); await app.connections.refreshProfiles(); await app.connections.connect('remote');
  return { app, local, remote };
}

it('coalesces title hints across host-scoped title queries, refreshing all searches but not inactive caches or other hosts', async () => {
  const { app, local, remote } = await setup();
  app.queries.setDefaultOptions({ queries: { ...app.queries.getDefaultOptions().queries, gcTime: Infinity } });
  let title = 'Before';
  const keys = [
    ['session-tab-summaries', 'local', ['inactive']], ['session-sidebar-summaries', 'local', ['visible']],
    ['session-search', 'local', 'before', 'all', 'page-2'], ['session-search', 'local', 'after', 'all', undefined],
    ['host-attention', 'local', 'cursor'],
  ];
  const reads = keys.map(key => vi.fn(async () => key[0] === 'session-search' ? (title.toLowerCase() === key[2] ? [title] : []) : title));
  const observers = keys.map((queryKey, index) => new QueryObserver(app.queries, { queryKey, queryFn: reads[index]!, staleTime: Infinity }));
  const stops = observers.map(observer => observer.subscribe(() => {}));
  await Promise.all(observers.map(observer => observer.refetch()));
  const inactiveKey = ['session-search', 'local', 'inactive'];
  app.queries.setQueryData(inactiveKey, 'cached');
  const remoteKey = ['session-tab-summaries', 'remote', ['inactive']];
  app.queries.setQueryData(remoteKey, 'Other host');
  const providerKey = ['provider-list', 'local'];
  app.queries.setQueryData(providerKey, 'Provider settings');
  const providers = app.queries.getQueryState(providerKey);
  const remoteBefore = app.queries.getQueryState(remoteKey);
  title = 'After';
  for (let i = 0; i < 20; i++) local.notify('off-page-root');
  await vi.advanceTimersByTimeAsync(251);
  reads.forEach(read => expect(read).toHaveBeenCalledTimes(2));
  expect(app.queries.getQueryData(keys[0]!)).toBe('After');
  expect(app.queries.getQueryData(keys[2]!)).toEqual([]);
  expect(app.queries.getQueryData(keys[3]!)).toEqual(['After']);
  expect(app.queries.getQueryState(inactiveKey)?.isInvalidated).toBe(true);
  expect(app.queries.getQueryData(inactiveKey)).toBe('cached');
  expect(app.queries.getQueryState(remoteKey)).toEqual(remoteBefore);
  expect(app.queries.getQueryState(providerKey)).toEqual(providers);
  expect(local.titles.size).toBe(1); expect(remote.titles.size).toBe(1);
  expect(local.client.session).not.toHaveBeenCalled(); expect(local.client.events.subscribe).not.toHaveBeenCalled();
  stops.forEach(stop => stop());
});

it.each([false, true])('cancels stale in-flight title reads even when it is the initial load (%s)', async initial => {
  const { app, local } = await setup();
  const key = ['session-tab-summaries', 'local', ['inactive']];
  if (!initial) app.queries.setQueryData(key, 'Previously cached');
  let finish!: (title: string) => void;
  let signal!: AbortSignal;
  const read = vi.fn(({ signal: requestSignal }: { signal: AbortSignal }) => {
    signal = requestSignal;
    return new Promise<string>(resolve => { finish = resolve; });
  });
  const observer = new QueryObserver(app.queries, { queryKey: key, queryFn: read, staleTime: 0 });
  const stop = observer.subscribe(() => {});
  const oldFinish = finish, oldSignal = signal;
  local.notify(); await vi.advanceTimersByTimeAsync(251);
  expect(oldSignal.aborted).toBe(true);
  expect(read).toHaveBeenCalledTimes(2);
  const intermediateFinish = finish, intermediateSignal = signal;
  for (let i = 0; i < 20; i++) local.notify();
  await vi.advanceTimersByTimeAsync(251);
  expect(intermediateSignal.aborted).toBe(true);
  expect(read).toHaveBeenCalledTimes(3);
  finish('Newest'); await vi.advanceTimersByTimeAsync(1);
  intermediateFinish('Intermediate');
  oldFinish('Stale'); await vi.advanceTimersByTimeAsync(1);
  expect(app.queries.getQueryData(key)).toBe('Newest');
  stop();
});

it('owns one listener through reconnect and retires timers and pending cancellation on detach/dispose', async () => {
  const { app, local, remote } = await setup();
  const key = ['session-tab-summaries', 'local', ['inactive']];
  const invalidations = vi.spyOn(app.queries, 'invalidateQueries');
  local.notify(); local.state('reconnecting');
  await vi.advanceTimersByTimeAsync(251);
  expect(invalidations).not.toHaveBeenCalled();
  local.state('connected', '2'); local.state('connected', '2');
  expect(local.titles.size).toBe(1);
  expect(local.client.onNotification).toHaveBeenCalledTimes(2);
  invalidations.mockClear();
  let finish!: () => void;
  vi.spyOn(app.queries, 'cancelQueries').mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve; }));
  app.queries.setQueryData(key, 'Before detach');
  local.notify(); await vi.advanceTimersByTimeAsync(251);
  const retired = [...local.titles][0]!;
  app.connections.disconnect('local');
  expect(local.titles.size).toBe(0);
  finish(); retired({ root_id: 'late' }); await vi.advanceTimersByTimeAsync(251);
  expect(invalidations).not.toHaveBeenCalled();
  expect(app.queries.getQueryData(key)).toBeUndefined();
  expect(remote.titles.size).toBe(1);
  remote.notify(); app.dispose(); await vi.advanceTimersByTimeAsync(251);
  expect(remote.titles.size).toBe(0);
  expect(invalidations).not.toHaveBeenCalled();
});

it('does not register an app title listener for older hosts', async () => {
  const { app, local, remote } = await setup(false);
  expect(local.client.onNotification).not.toHaveBeenCalled();
  expect(remote.client.onNotification).not.toHaveBeenCalled();
  app.dispose();
});
