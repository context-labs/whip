import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { HostConnections, daemonEndpoint, type HostProfile } from '../src/hosts';
import { localProfile, urlProfile, type ConnectionProfile, type ConnectionOptions, type ResolvedConnection } from '../src/connections';
import type { AppPlatform } from '../src/platform';

const mocks = vi.hoisted(() => {
  const configurations = { revision: '1', remote_hosts: [] as HostProfile[] };
  const clients: Array<ReturnType<typeof create>> = [];
  const identities = new Map<string, string>();
  const connecting = new Map<string, Promise<void>>();
  const create = (options: { endpoint: string; clientId: string }) => {
    const listeners = new Set<() => void>();
    let snapshot: { state: string; info?: { runtime_id: string; connection_id: string }; error?: Error } = { state: 'closed' };
    const client = {
      clientId: options.clientId, endpoint: options.endpoint,
      getSnapshot: () => snapshot,
      requireConnected: () => { if (snapshot.state !== 'connected') throw new Error('Disconnected'); return snapshot.info!; },
      emit: (state: string, error?: Error) => { snapshot = { ...snapshot, state, error }; for (const listener of listeners) listener(); },
      connect: vi.fn(async () => {
        if (connecting.has(options.endpoint)) await connecting.get(options.endpoint);
        snapshot = { state: 'connected', info: { runtime_id: identities.get(options.endpoint) ?? options.endpoint, connection_id: crypto.randomUUID() } };
        for (const listener of listeners) listener();
      }),
      close: vi.fn(() => client.emit('closed')),
      subscribe: (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
      configuration: {
        get: vi.fn(async () => structuredClone(configurations)),
        update: vi.fn(async (patch: typeof configurations) => {
          if (patch.revision !== configurations.revision) throw new Error('Configuration revision conflict');
          configurations.revision = String(Number(configurations.revision) + 1);
          configurations.remote_hosts = structuredClone(patch.remote_hosts);
          return structuredClone(configurations);
        }),
      },
    };
    clients.push(client);
    return client;
  };
  return { clients, create, identities, connecting, configurations, lists: [] as Array<{ start: ReturnType<typeof vi.fn>; dispose: ReturnType<typeof vi.fn> }> };
});
vi.mock('@whip/sdk', () => ({ createWhipClient: mocks.create }));
vi.mock('@whip/sdk/state', () => ({ createSessionListView: () => {
  const list = { start: vi.fn(async () => {}), dispose: vi.fn(async () => {}) };
  mocks.lists.push(list);
  return list;
} }));

const stores: HostConnections[] = [];
const remote = (id: string): HostProfile => ({ id, name: id.toUpperCase(), url: `http://${id}.test`, runtime_id: id, connect_on_launch: true });
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((success, failure) => { resolve = success; reject = failure; });
  return { promise, resolve, reject };
}
function fixture(profiles = [remote('a'), remote('b')], platform: Partial<AppPlatform> = {}, values = new Map<string, string>()) {
  const effects = { connected: vi.fn(), detached: vi.fn() };
  mocks.configurations.remote_hosts = profiles;
  for (const profile of profiles) mocks.identities.set(profile.url, profile.runtime_id);
  mocks.identities.set('http://local.test', 'home');
  const hosts = new HostConnections({ defaultEndpoint: 'http://local.test', storage: {
    keys: () => [...values.keys()], getItem: key => values.get(key) ?? null,
    setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); },
  }, openExternal: async () => {}, copy: async () => {}, download: async () => {}, ...platform }, { list: async () => [], put: async () => {}, delete: async () => {} }, effects);
  stores.push(hosts);
  return { hosts, effects, values };
}
async function start(hosts: HostConnections) { await hosts.connect(); await hosts.refreshProfiles(); }
beforeEach(() => { mocks.clients.length = 0; mocks.lists.length = 0; mocks.identities.clear(); mocks.connecting.clear(); mocks.configurations.revision = '1'; });
afterEach(() => { for (const hosts of stores.splice(0)) hosts.dispose(); });

it('loads profiles from Local and observes three independent daemons', async () => {
  const { hosts } = fixture();
  await start(hosts);
  await vi.waitFor(() => expect(hosts.getSnapshot().hosts.every(host => host.state === 'connected')).toBe(true));
  expect(hosts.getSnapshot().hosts.map(host => host.runtimeId)).toEqual(['home', 'a', 'b']);
  expect(mocks.lists).toHaveLength(3);
  expect(mocks.clients[1]!.configuration.get).not.toHaveBeenCalled();
  hosts.disconnect('a');
  expect(mocks.clients[1]!.close).toHaveBeenCalledTimes(1);
  expect(mocks.clients[0]!.close).not.toHaveBeenCalled();
  expect(mocks.clients[2]!.close).not.toHaveBeenCalled();
  expect(hosts.host('b')?.state).toBe('connected');
  await hosts.connect('a');
  expect(hosts.host('a')?.state).toBe('connected');
  expect(mocks.lists).toHaveLength(4);
});

it('keeps remote connections alive through local failure and blocks local config edits', async () => {
  const { hosts } = fixture();
  await start(hosts);
  const remoteClient = hosts.host('b')!.client!;
  const signal = hosts.signal(remoteClient);
  hosts.disconnect('local');
  expect(hosts.isAttached(remoteClient)).toBe(true);
  expect(signal.aborted).toBe(false);
  await expect(hosts.remove('a')).rejects.toThrow('Reconnect Local');
  expect(hosts.host('a')?.client).toBeDefined();
});

it('does not probe or save before Local configuration has loaded', async () => {
  const { hosts } = fixture();
  await expect(hosts.save({ ...remote('c'), runtime_id: undefined })).rejects.toThrow('Reconnect Local');
  expect(mocks.clients).toHaveLength(0);
});

it('does not start a catalog when an endpoint serves a different identity', async () => {
  const { hosts } = fixture([remote('a')]);
  mocks.identities.set('http://a.test', 'replacement');
  await start(hosts);
  await vi.waitFor(() => expect(hosts.host('a')?.error).toContain('different daemon'));
  expect(hosts.host('a')?.client).toBeUndefined();
  expect(hosts.host('replacement')).toBeUndefined();
  expect(mocks.lists).toHaveLength(1);
  expect(hosts.home().client).toBeDefined();
});

it('checks aliases before saving and never duplicates the local runtime', async () => {
  const { hosts } = fixture([]);
  await start(hosts);
  mocks.identities.set('http://alias.test', 'home');
  await expect(hosts.save({ ...remote('alias'), runtime_id: undefined })).rejects.toThrow('already saved as Local');
  expect(hosts.getSnapshot().profiles).toEqual([]);
  expect(mocks.lists).toHaveLength(1);
  expect(mocks.clients[0]!.close).not.toHaveBeenCalled();
});

it('persists only through Local and reconciles a conflict without overwriting another browser', async () => {
  const { hosts } = fixture();
  await start(hosts);
  mocks.configurations.remote_hosts.push(remote('other-browser'));
  mocks.configurations.revision = '2';
  await expect(hosts.remove('a')).rejects.toThrow('revision conflict');
  expect(mocks.configurations.remote_hosts.map(host => host.id)).toEqual(['a', 'b', 'other-browser']);
  expect(hosts.getSnapshot().profiles).toHaveLength(3);
  await hosts.remove('a');
  expect(hosts.getSnapshot().profiles.map(host => host.id)).toEqual(['b', 'other-browser']);
  expect(hosts.host('a')).toBeUndefined();
  expect(mocks.clients[1]!.configuration.update).not.toHaveBeenCalled();
});

it('requires explicit acceptance of a changed identity and saves a disabled startup preference', async () => {
  const { hosts } = fixture([remote('a')]);
  await start(hosts);
  mocks.identities.set('http://replacement.test', 'replacement');
  const profile = { ...remote('a'), url: 'http://replacement.test', connect_on_launch: false };
  await expect(hosts.save(profile)).rejects.toThrow('Accept the new daemon identity');
  expect(hosts.host('a')?.client).toBeDefined();
  await hosts.save(profile, true);
  expect(hosts.host('a')).toBeUndefined();
  expect(hosts.host('replacement')?.client).toBeUndefined();
  expect(hosts.getSnapshot().profiles[0]).toMatchObject({ runtime_id: 'replacement', connect_on_launch: false });
});

it('refreshes the new Local connection while an earlier connection read is pending', async () => {
  const { hosts } = fixture();
  await start(hosts);
  const local = mocks.clients[0]!;
  const old = deferred<typeof mocks.configurations>();
  const current = deferred<typeof mocks.configurations>();
  local.configuration.get.mockImplementationOnce(() => old.promise).mockImplementationOnce(() => current.promise);
  const previous = hosts.refreshProfiles();
  const failure = expect(previous).rejects.toThrow('Previous connection closed');
  local.emit('reconnecting');
  await local.connect();
  const next = hosts.refreshProfiles();
  const reads = local.configuration.get.mock.calls.length;
  old.reject(new Error('Previous connection closed'));
  await failure;
  expect(hosts.refreshProfiles()).toBe(next);
  expect(local.configuration.get).toHaveBeenCalledTimes(reads);
  expect(hosts.getSnapshot().profileError).toBeUndefined();
  current.resolve({ revision: '2', remote_hosts: [remote('b')] });
  await next;
  expect(hosts.getSnapshot().profiles.map(profile => profile.id)).toEqual(['b']);
});

it('does not let a delayed refresh undo a completed host removal', async () => {
  const { hosts } = fixture();
  await start(hosts);
  const old = deferred<typeof mocks.configurations>();
  const before = structuredClone(mocks.configurations);
  mocks.clients[0]!.configuration.get.mockImplementationOnce(() => old.promise);
  const refresh = hosts.refreshProfiles();
  await hosts.remove('a');
  old.resolve(before);
  await refresh;
  expect(hosts.getSnapshot().profiles.map(profile => profile.id)).toEqual(['b']);
  expect(hosts.host('a')).toBeUndefined();
  await hosts.setConnectOnLaunch('b', false);
  expect(mocks.configurations.remote_hosts[0]?.connect_on_launch).toBe(false);
});

it('reconciles a delayed write response after a newer configuration was observed and edited', async () => {
  const { hosts } = fixture();
  await start(hosts);
  const old = deferred<typeof mocks.configurations>();
  mocks.clients[0]!.configuration.update.mockImplementationOnce(() => old.promise);
  const removal = hosts.remove('a');
  mocks.configurations.revision = '2';
  mocks.configurations.remote_hosts = [remote('b')];
  const removed = structuredClone(mocks.configurations);
  await hosts.refreshProfiles();
  await hosts.setConnectOnLaunch('b', false);
  old.resolve(removed);
  await removal;
  expect(hosts.getSnapshot().profiles).toEqual([{ ...remote('b'), connect_on_launch: false }]);
});

it('reports a detached Local write without applying it to the old workspace snapshot', async () => {
  const { hosts } = fixture();
  await start(hosts);
  const pending = deferred<typeof mocks.configurations>();
  mocks.clients[0]!.configuration.update.mockImplementationOnce(() => pending.promise);
  const removal = hosts.remove('a');
  hosts.disconnect('local');
  mocks.configurations.revision = '2';
  mocks.configurations.remote_hosts = [remote('b')];
  pending.resolve(structuredClone(mocks.configurations));
  await expect(removal).rejects.toThrow('Local changed while saving hosts');
  expect(hosts.host('a')?.client).toBeDefined();
  await start(hosts);
  expect(hosts.host('a')).toBeUndefined();
});

it('edits an offline host without probing and only rebinds its identity explicitly', async () => {
  const { hosts } = fixture([remote('a')]);
  await start(hosts);
  hosts.disconnect('a');
  const clients = mocks.clients.length;
  mocks.identities.set('http://a.test', 'replacement');
  const renamed = { ...remote('a'), name: '  Kuzco  ', connect_on_launch: false };
  await hosts.save(renamed);
  expect(mocks.clients).toHaveLength(clients);
  expect(hosts.getSnapshot().profiles[0]).toMatchObject({ name: 'Kuzco', runtime_id: 'a' });
  expect(hosts.host('a')?.client).toBeUndefined();
  await hosts.save(renamed, true);
  expect(mocks.clients).toHaveLength(clients + 1);
  expect(hosts.getSnapshot().profiles[0]?.runtime_id).toBe('replacement');
});

it('uses the saved identity even when an edit omits its expected runtime', async () => {
  const { hosts } = fixture([remote('a')]);
  await start(hosts);
  mocks.identities.set('http://replacement.test', 'replacement');
  await expect(hosts.save({ ...remote('a'), runtime_id: undefined, url: 'http://replacement.test' }))
    .rejects.toThrow('Accept the new daemon identity');
  expect(hosts.getSnapshot().profiles[0]?.runtime_id).toBe('a');
});

it('does not recreate a removed host from an old edit form', async () => {
  const { hosts } = fixture([remote('a')]);
  await start(hosts);
  mocks.configurations.revision = '2';
  mocks.configurations.remote_hosts = [];
  await hosts.refreshProfiles();
  await expect(hosts.save(remote('a'))).rejects.toThrow('This saved host changed');
  expect(mocks.configurations.remote_hosts).toEqual([]);
});

it('retains the edit revision while probing so another browser cannot be overwritten', async () => {
  const { hosts } = fixture([remote('a')]);
  await start(hosts);
  const pending = deferred<void>();
  mocks.identities.set('http://alias.test', 'a');
  mocks.connecting.set('http://alias.test', pending.promise);
  const edit = hosts.save({ ...remote('a'), url: 'http://alias.test' });
  mocks.configurations.revision = '2';
  mocks.configurations.remote_hosts = [{ ...remote('a'), name: 'Other browser' }];
  await hosts.refreshProfiles();
  pending.resolve();
  await expect(edit).rejects.toThrow('revision conflict');
  expect(hosts.getSnapshot().profiles[0]?.name).toBe('Other browser');
});

it.each([
  { ...remote('c'), id: 'local' },
  { ...remote('c'), id: 'b' },
  { ...remote('c'), url: 'http://b.test:80/' },
  { ...remote('c'), runtime_id: 'b' },
  { ...remote('c'), name: ' \n ' },
  { ...remote('c'), url: 'http://c.test/proxy' },
])('rejects an invalid profile document before changing any connections: %j', async invalid => {
  const { hosts } = fixture();
  await start(hosts);
  const before = hosts.getSnapshot();
  mocks.configurations.revision = '2';
  mocks.configurations.remote_hosts = [remote('b'), invalid];
  await expect(hosts.refreshProfiles()).rejects.toThrow();
  expect(hosts.getSnapshot().profiles).toBe(before.profiles);
  expect(hosts.host('a')?.client).toBe(before.hosts[1]!.client);
  expect(hosts.host('b')?.client).toBe(before.hosts[2]!.client);
  expect(mocks.clients).toHaveLength(3);
  expect(hosts.getSnapshot().profilesReady).toBe(false);
});

it('keeps existing remotes usable when Local no longer advertises saved-host support', async () => {
  const { hosts } = fixture();
  await start(hosts);
  const remoteClient = hosts.host('a')!.client;
  const local = mocks.clients[0]!;
  local.configuration.get.mockResolvedValueOnce({ revision: '2' } as typeof mocks.configurations);
  await expect(hosts.refreshProfiles()).rejects.toThrow('Update the local daemon');
  expect(hosts.getSnapshot().profilesReady).toBe(false);
  expect(hosts.host('a')?.client).toBe(remoteClient);
  await expect(hosts.remove('a')).rejects.toThrow('load its configuration');
  expect(local.configuration.update).not.toHaveBeenCalled();
  await hosts.refreshProfiles();
  expect(hosts.getSnapshot().profilesReady).toBe(true);
});

it('normalizes loaded profiles without reconnecting an unchanged endpoint', async () => {
  const { hosts } = fixture([remote('a')]);
  await start(hosts);
  const client = hosts.host('a')!.client;
  mocks.configurations.remote_hosts = [{ ...remote('a'), name: '  Kuzco  ', url: 'ws://A.test:80/api/v3/ws' }];
  await hosts.refreshProfiles();
  expect(hosts.getSnapshot().profiles[0]).toMatchObject({ name: 'Kuzco', url: 'http://a.test' });
  expect(hosts.host('a')?.client).toBe(client);
});

it('normalizes supported endpoints and refuses credentials and path prefixes', () => {
  expect(daemonEndpoint(' wss://Kuzco.test/api/v3/ws ')).toBe('https://kuzco.test');
  expect(daemonEndpoint('http://[::1]:8080/')).toBe('http://[::1]:8080');
  for (const value of ['javascript:alert(1)', 'https://user:secret@host', 'https://host?token=x', 'https://host/proxy'])
    expect(() => daemonEndpoint(value)).toThrow();
});

function nativeFixture(profiles = [remote('a')], saved: ConnectionProfile[] = []) {
  const values = new Map<string, string>();
  if (saved.length) { values.set('whip.hosts.v2', JSON.stringify(saved)); values.set('whip.selectedHost.v2', JSON.stringify(saved.at(-1)!.id)); }
  const disposals = new Map<string, ReturnType<typeof vi.fn>>();
  const resolveConnection = vi.fn(async (profile: ConnectionProfile, _options: ConnectionOptions): Promise<ResolvedConnection> => {
    const dispose = vi.fn(); disposals.set(profile.id, dispose);
    return { endpoint: profile.target.kind === 'local' ? 'http://local.test' : profile.target.kind === 'url' ? profile.target.endpoint : `http://${profile.target.host}.test`, dispose };
  });
  return { ...fixture(profiles, { defaultConnection: localProfile, connectionKinds: ['local', 'url', 'ssh'], resolveConnection }, values), resolveConnection, disposals };
}
const sshProfile = (id = 'server'): ConnectionProfile => ({ id: `ssh:${id}`, label: id, target: { kind: 'ssh', host: id } });

it('keeps local, URL and SSH transports independently attached through refresh and native disconnect', async () => {
  const f = nativeFixture(); await start(f.hosts);
  await f.hosts.saveNative(sshProfile());
  expect(f.hosts.getSnapshot().hosts).toHaveLength(3);
  await f.hosts.refreshProfiles(); expect(f.hosts.getSnapshot().hosts).toHaveLength(3);
  const local = f.hosts.home().client; const url = f.hosts.host('a')!.client;
  f.hosts.disconnect('ssh:server');
  expect(f.disposals.get('ssh:server')).toHaveBeenCalledOnce();
  expect(f.hosts.home().client).toBe(local); expect(f.hosts.host('a')!.client).toBe(url);
  expect(f.disposals.get('local')).not.toHaveBeenCalled(); expect(f.disposals.get('a')).not.toHaveBeenCalled();
  expect(mocks.configurations.remote_hosts.map(profile => profile.id)).toEqual(['a']);
  expect(JSON.parse(f.values.get('whip.hosts.v2')!).some((profile: ConnectionProfile) => profile.id === 'ssh:server')).toBe(true);
});
it('deduplicates setup per host and disposes late transports after cancellation', async () => {
  const saved = { ...sshProfile(), runtimeId: 'remote' }; const f = nativeFixture([], [localProfile, saved]); await start(f.hosts);
  const setup = deferred<ResolvedConnection>(); let signal!: AbortSignal;
  f.resolveConnection.mockImplementationOnce(async (_profile, options) => { signal = options.signal; options.onProgress('Checking host key'); return setup.promise; });
  const connection = f.hosts.connect(saved.id); expect(f.hosts.connect(saved.id)).toBe(connection);
  expect(f.hosts.getSnapshot().hosts.find(host => host.id === saved.id)?.progress).toBe('Checking host key');
  const local = f.hosts.home().client; f.hosts.disconnect(saved.id); expect(signal.aborted).toBe(true);
  const dispose = vi.fn(); setup.resolve({ endpoint: 'http://retired.test', dispose });
  await expect(connection).rejects.toThrow(); expect(dispose).toHaveBeenCalledOnce();
  expect(mocks.clients.some(client => client.endpoint === 'http://retired.test')).toBe(false);
  expect(f.hosts.home().client).toBe(local);
});
it('requires explicit native replacement consent and keeps remembered identity after rejection', async () => {
  const saved = { ...sshProfile(), runtimeId: 'old' }; const f = nativeFixture([], [localProfile, saved]); await start(f.hosts);
  mocks.identities.set('http://server.test', 'replacement');
  await expect(f.hosts.saveNative(sshProfile())).rejects.toThrow('different daemon');
  expect(f.hosts.host('old')?.profile.runtimeId).toBe('old'); expect(f.hosts.host('replacement')).toBeUndefined();
  expect(JSON.parse(f.values.get('whip.hosts.v2')!).find((profile: ConnectionProfile) => profile.id === saved.id).runtimeId).toBe('old');
  await f.hosts.saveNative(saved, true); expect(f.hosts.host('replacement')?.client).toBeDefined();
  expect(f.hosts.home().client).toBeDefined();
});
it('does not bypass a saved SSH identity by retyping its target with a new profile ID', async () => {
  const saved = { ...sshProfile(), runtimeId: 'old' }; const f = nativeFixture([], [localProfile, saved]); await start(f.hosts);
  mocks.identities.set('http://server.test', 'replacement');
  await expect(f.hosts.saveNative({ ...sshProfile(), id: 'ssh:new' })).rejects.toThrow('different daemon');
  expect(f.hosts.host('old')?.id).toBe(saved.id); expect(f.hosts.getSnapshot().hosts).toHaveLength(2);
});
it('preserves old desktop addresses until verified import and retains their identity', async () => {
  const legacy = { ...urlProfile('http://a.test'), runtimeId: 'old' }; const f = nativeFixture([], [localProfile, legacy]); await start(f.hosts);
  expect(f.hosts.getSnapshot().legacyProfiles).toEqual([legacy]);
  mocks.identities.set('http://a.test', 'replacement');
  const profile = { ...remote('a'), runtime_id: legacy.runtimeId };
  await expect(f.hosts.save(profile, false, legacy.id)).rejects.toThrow('Accept the new daemon identity');
  expect(f.hosts.getSnapshot().legacyProfiles).toEqual([legacy]);
  mocks.identities.set('http://a.test', 'old'); await f.hosts.save(profile, false, legacy.id);
  await f.hosts.forgetLegacyProfile(legacy.id);
  expect(f.hosts.getSnapshot().legacyProfiles).toEqual([]);
  expect(mocks.configurations.remote_hosts[0]?.runtime_id).toBe('old');
});
it('does not let native profiles be overwritten by a shared configuration collision', async () => {
  const f = nativeFixture([], [localProfile, sshProfile()]); await start(f.hosts);
  mocks.configurations.remote_hosts = [{ ...remote('a'), id: 'ssh:server' }];
  await expect(f.hosts.refreshProfiles()).rejects.toThrow('conflicts with a native host ID');
  expect(f.hosts.getSnapshot().hosts.find(host => host.id === 'ssh:server')?.profile.target.kind).toBe('ssh');
});
it('disposes native setup when the window closes without disconnecting accepted daemon work itself', async () => {
  const f = nativeFixture([], [localProfile, sshProfile()]); await start(f.hosts);
  const setup = deferred<ResolvedConnection>(); f.resolveConnection.mockReturnValueOnce(setup.promise);
  const pending = f.hosts.connect('ssh:server'); f.hosts.dispose(); const dispose = vi.fn(); setup.resolve({ endpoint: 'http://server.test', dispose });
  await expect(pending).rejects.toThrow(); expect(dispose).toHaveBeenCalledOnce(); expect(f.disposals.get('local')).toHaveBeenCalledOnce();
});
it('does not lose the freshly verified local identity when saving or selecting SSH', async () => {
  const f = nativeFixture([]); await start(f.hosts); await f.hosts.saveNative(sshProfile()); f.hosts.select('ssh:server');
  const saved = JSON.parse(f.values.get('whip.hosts.v2')!) as ConnectionProfile[];
  expect(saved.find(profile => profile.id === 'local')?.runtimeId).toBe('home');
  expect(saved.find(profile => profile.id === 'ssh:server')?.runtimeId).toBe('http://server.test');
});
it('keeps corrupt device records intact during automatic managed-local attachment', async () => {
  const values = new Map([['whip.hosts.v2', '{broken']]);
  const { hosts } = fixture([], { defaultConnection: localProfile, connectionKinds: ['local'], resolveConnection: async () => ({ endpoint: 'http://local.test', dispose() {} }) }, values);
  await start(hosts); expect(values.get('whip.hosts.v2')).toBe('{broken');
  expect(hosts.home().error).toContain('preserved'); expect(hosts.home().client).toBeDefined();
});
it('restores a focused shared URL ID without copying its profile back into device storage', async () => {
  const f = nativeFixture(); await start(f.hosts); f.hosts.select('a');
  expect(f.values.get('whip.selectedHost.v3')).toBe('"a"');
  expect(JSON.parse(f.values.get('whip.hosts.v2')!).every((profile: ConnectionProfile) => profile.target.kind !== 'url')).toBe(true);
  const restored = fixture([remote('a')], { defaultConnection: localProfile, connectionKinds: ['local', 'url', 'ssh'], resolveConnection: f.resolveConnection }, f.values);
  expect(restored.hosts.getSnapshot().selectedId).toBe('a');
  await start(restored.hosts); expect(restored.hosts.host('a')?.client).toBeDefined();
});
it('does not evict old desktop addresses to persist the managed-local profile', async () => {
  const saved = Array.from({ length: 16 }, (_, index) => urlProfile(`http://legacy-${index}.test`));
  const f = nativeFixture([], saved); await start(f.hosts);
  expect(JSON.parse(f.values.get('whip.hosts.v2')!)).toEqual(saved);
  expect(f.hosts.getSnapshot().legacyProfiles).toHaveLength(16);
  expect(f.hosts.home().error).toContain('storage is full');
});
