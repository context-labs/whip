import { afterEach, expect, it, vi } from 'vitest';
import { QueryObserver } from '@tanstack/react-query';
import type { Client, Transport } from '@whip/sdk';
import type { TreeCatalogView } from '@whip/sdk/state';
import { AppRuntime } from '../src/runtime';
import { createFallbackStorage } from '../src/platform';

const hosts = vi.hoisted(() => new Map<string, {
  revision: number; major: number; calls: string[];
  catalogs: { view: TreeCatalogView; listeners: Set<() => void> }[];
}>());
vi.mock('@whip/sdk/browser', () => ({
  discoverGateway: async (endpoint: string) => ({ runtime_id: new URL(endpoint).hostname, process_epoch: 'boot' }),
  browserSocket: (endpoint: string): Transport => async (request, _identity, options) => {
    options.signal?.throwIfAborted();
    const id = new URL(endpoint).hostname, host = hosts.get(id)!;
    host.calls.push(request.method);
    let result: unknown;
    switch (request.method) {
      case 'initialize': result = { major: host.major, minor: 0, runtime_id: id, process_epoch: 'boot', network_client: true, builtins: [] }; break;
      case 'host.profiles': result = { revision: '1'.repeat(64), profiles: [{ id: 'remote', name: 'Remote', url: 'http://remote', runtime_id: 'remote', connect_on_launch: false }] }; break;
      case 'trees.catalog': result = { revision: String(host.revision) }; break;
      case 'trees.list': result = { revision: String(host.revision), items: [], next_cursor: null }; break;
      case 'providers.list': result = { revision: '1'.repeat(64), routes: [], defaults: null, compaction_model: null }; break;
      default: throw new Error('Unexpected native request: ' + request.method);
    }
    return { jsonrpc: '2.0', id: request.id, result };
  },
}));
vi.mock('@whip/sdk/state', async importOriginal => {
  const actual = await importOriginal<typeof import('@whip/sdk/state')>();
  return { ...actual, createTreeCatalogView: (client: Client) => {
    const view = actual.createTreeCatalogView(client, { pollIntervalMs: 60_000 });
    const listeners = new Set<() => void>(), subscribe = view.subscribe;
    vi.spyOn(view, 'subscribe').mockImplementation(listener => {
      listeners.add(listener); const off = subscribe(listener);
      return () => { listeners.delete(listener); off(); };
    });
    hosts.get(client.runtimeID)!.catalogs.push({ view, listeners });
    return view;
  } };
});
const apps: AppRuntime[] = [];
afterEach(() => { apps.splice(0).forEach(app => app.dispose()); hosts.clear(); vi.useRealTimers(); });
function host(id: string) {
  const state = { revision: 1, major: 4, calls: [] as string[], catalogs: [] as { view: TreeCatalogView; listeners: Set<() => void> }[] };
  hosts.set(id, state); return state;
}
function application() {
  const app = new AppRuntime({ defaultEndpoint: 'http://local', storage: createFallbackStorage(() => { throw new Error('memory only'); }, () => {}), copy: async () => {}, download: async () => {}, openExternal: async () => {} });
  apps.push(app); return app;
}
async function refresh(app: AppRuntime, id: string) {
  await app.connections.host(id)!.list!.refresh();
  await vi.advanceTimersByTimeAsync(1);
}
async function setup() {
  vi.useFakeTimers();
  const local = host('local'), remote = host('remote'), app = application();
  await app.connect(); await app.connections.refreshProfiles(); await app.connections.connect('remote');
  await refresh(app, 'local'); await refresh(app, 'remote');
  return { app, local, remote };
}

it('coalesces catalog revisions across host-scoped title queries, refreshing all searches but not inactive caches or other hosts', async () => {
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
  for (let i = 0; i < 20; i++) local.revision++;
  await refresh(app, 'local');
  reads.forEach(read => expect(read).toHaveBeenCalledTimes(2));
  expect(app.queries.getQueryData(keys[0]!)).toBe('After');
  expect(app.queries.getQueryData(keys[2]!)).toEqual([]);
  expect(app.queries.getQueryData(keys[3]!)).toEqual(['After']);
  expect(app.queries.getQueryState(inactiveKey)?.isInvalidated).toBe(true);
  expect(app.queries.getQueryData(inactiveKey)).toBe('cached');
  expect(app.queries.getQueryState(remoteKey)).toEqual(remoteBefore);
  expect(app.queries.getQueryState(providerKey)).toEqual(providers);
  expect(local.catalogs[0]!.listeners.size).toBe(1); expect(remote.catalogs[0]!.listeners.size).toBe(1);
  expect([...local.calls, ...remote.calls].some(method => method.startsWith('sessions.'))).toBe(false);
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
  local.revision++; await refresh(app, 'local');
  expect(oldSignal.aborted).toBe(true);
  expect(read).toHaveBeenCalledTimes(2);
  const intermediateFinish = finish, intermediateSignal = signal;
  for (let i = 0; i < 20; i++) local.revision++;
  await refresh(app, 'local');
  expect(intermediateSignal.aborted).toBe(true);
  expect(read).toHaveBeenCalledTimes(3);
  finish('Newest'); await vi.advanceTimersByTimeAsync(1);
  intermediateFinish('Intermediate');
  oldFinish('Stale'); await vi.advanceTimersByTimeAsync(1);
  expect(app.queries.getQueryData(key)).toBe('Newest');
  stop();
});

it('owns one catalog listener per client and retires pending invalidation on detach/dispose', async () => {
  const { app, local, remote } = await setup();
  const first = app.connections.host('local')!.client!;
  const oldCatalog = local.catalogs[0]!;
  app.connections.disconnect('local');
  expect(oldCatalog.listeners.size).toBe(0);
  await app.connect(); await refresh(app, 'local');
  expect(app.connections.host('local')!.client).not.toBe(first);
  expect(local.catalogs).toHaveLength(2);
  expect(local.catalogs[1]!.listeners.size).toBe(1);
  const key = ['session-tab-summaries', 'local', ['inactive']];
  const invalidations = vi.spyOn(app.queries, 'invalidateQueries');
  let finish!: () => void;
  vi.spyOn(app.queries, 'cancelQueries').mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve; }));
  app.queries.setQueryData(key, 'Before detach');
  const retired = [...local.catalogs[1]!.listeners][0]!;
  local.revision++; await refresh(app, 'local');
  app.connections.disconnect('local');
  expect(local.catalogs[1]!.listeners.size).toBe(0);
  finish(); retired(); await vi.advanceTimersByTimeAsync(1);
  expect(invalidations).not.toHaveBeenCalled();
  expect(app.queries.getQueryData(key)).toBeUndefined();
  expect(remote.catalogs[0]!.listeners.size).toBe(1);
  vi.spyOn(app.queries, 'cancelQueries').mockImplementationOnce(() => new Promise<void>(resolve => { finish = resolve; }));
  remote.revision++; await refresh(app, 'remote');
  app.dispose(); finish(); await vi.advanceTimersByTimeAsync(1);
  expect(remote.catalogs[0]!.listeners.size).toBe(0);
  expect(invalidations).not.toHaveBeenCalled();
});

it('rejects older hosts before opening catalogs or metadata listeners', async () => {
  const local = host('local'); local.major = 3;
  const app = application();
  await expect(app.connect()).rejects.toThrow();
  expect(local.catalogs).toHaveLength(0);
  expect(local.calls).toEqual(['initialize']);
  expect(app.connections.home().client).toBeUndefined();
});
