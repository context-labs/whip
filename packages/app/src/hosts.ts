import { Client, DeliveryError, RemoteError, framedTransport, type HostProfiles, type RecoveryStorage, type Transport } from '@whip/sdk';
import { discoverGateway, browserSocket } from '@whip/sdk/browser';
import { createTreeCatalogView, type TreeCatalogView } from '@whip/sdk/state';
import { errorMessage, readPreference, type AppPlatform, type LocalRuntimeStatus } from './platform';
import { daemonEndpoint, readConnections, saveConnections, urlProfile, validateProfile, type ConnectionProfile, type ResolvedConnection } from './connections';

export type HostProfile = HostProfiles['profiles'][number];
export type ConnectionState = 'closed' | 'connecting' | 'connected' | 'stale';
export interface HostConnection {
  readonly id: string;
  readonly name: string;
  readonly endpoint: string;
  readonly local: boolean;
  readonly runtimeId?: string;
  readonly connectOnLaunch: boolean;
  readonly state: ConnectionState;
  readonly client?: Client;
  readonly list?: TreeCatalogView;
  readonly error?: string;
  readonly profile: ConnectionProfile;
  readonly device: boolean;
  readonly progress?: string;
  readonly localRuntime?: LocalRuntimeStatus;
}
interface ConnectionRecord {
  profile: HostProfile;
  target: ConnectionProfile;
  device: boolean;
  resolved?: ResolvedConnection;
  setup?: { controller: AbortController; promise: Promise<void> };
  progress?: string;
  localRuntime?: LocalRuntimeStatus;
  client?: Client;
  retiredClient?: Client;
  list?: TreeCatalogView;
  verified: boolean;
  error?: string;
  waits: AbortController;
  preparation?: AbortController;
  state: ConnectionState;
  endpoint?: ResolvedConnection['endpoint'];
  retry?: ReturnType<typeof setTimeout>;
  retryCount: number;
}
interface HostsSnapshot {
  readonly hosts: readonly HostConnection[];
  readonly profiles: readonly HostProfile[];
  readonly profilesReady: boolean;
  readonly profileError?: string;
  readonly legacyProfiles?: readonly ConnectionProfile[];
  readonly selectedId?: string;
  readonly notice?: string;
}

export { daemonEndpoint } from './connections';

/** Expected native-local setup requirement, not a failed connection. */
export class LocalRuntimeSetupRequiredError extends Error {
  constructor() {
    super('Set up this Mac before connecting.');
    this.name = 'LocalRuntimeSetupRequiredError';
  }
}

class ChangedIdentityError extends Error {
  constructor(message = 'This address now serves a different daemon. Edit the host and explicitly accept its new identity to reconnect.') { super(message); }
}

function normalizeProfiles(profiles: readonly HostProfile[]): HostProfile[] {
  if (profiles.length > 16) throw new Error('There are 16 saved URL hosts. Remove a host before adding another.');
  const ids = new Set<string>();
  const endpoints = new Set<string>();
  const runtimes = new Set<string>();
  return profiles.map(profile => {
    if (profile.id === 'local') throw new Error('A remote profile cannot replace Local');
    const normalized = { ...profile, name: profile.name.trim(), url: profile.url };
    if ([normalized.id, normalized.name, normalized.runtime_id].some(value => !value.trim() || /[\r\n\t\0]/.test(value)))
      throw new Error('A remote host requires an ID, name, and verified runtime ID');
    if (ids.has(normalized.id) || endpoints.has(daemonEndpoint(normalized.url)) || runtimes.has(normalized.runtime_id))
      throw new Error(`Remote host ${normalized.name} duplicates a saved profile, endpoint, or runtime`);
    ids.add(normalized.id); endpoints.add(daemonEndpoint(normalized.url)); runtimes.add(normalized.runtime_id);
    return normalized;
  });
}

/** Window-owned clients. URL profiles belong to Local; native profiles belong to this device. */
export class HostConnections {
  private readonly records = new Map<string, ConnectionRecord>();
  private readonly listeners = new Set<() => void>();
  private readonly probes = new Set<AbortController>();
  private snapshot: HostsSnapshot = Object.freeze({ hosts: [], profiles: [], profilesReady: false });
  private revision?: string;
  private configurationVersion = 0;
  private refreshing?: { client: Client; version: number; promise: Promise<void> };
  private closed = false;
  private deviceProfiles: ConnectionProfile[] = [];
  private selected?: ConnectionProfile;
  private readonly preserveDeviceRecords: boolean;

  constructor(
    private readonly platform: AppPlatform,
    _recovery: RecoveryStorage,
    private readonly effects: { connected(runtimeId: string, client: Client): void; detached(client: Client, runtimeId: string, options: { recovering: boolean }): void },
  ) {
    const fallback = platform.defaultConnection ?? urlProfile(platform.defaultEndpoint!);
    const native = fallback.target.kind === 'local';
    const saved = native ? readConnections(platform.storage, fallback, platform.connectionKinds ?? ['url']) : undefined;
    this.preserveDeviceRecords = saved?.needsSelection === true || saved?.notice !== undefined;
    this.deviceProfiles = saved?.hosts ?? [];
    this.selected = saved?.selected;
    const focused = readPreference<unknown>(platform.storage, 'whip.selectedHost.v3', undefined);
    const selectedId = typeof focused === 'string' && focused.length <= 4096 ? focused : saved?.selected.id;
    const local = this.deviceProfiles.find(profile => profile.target.kind === 'local') ?? fallback;
    const alias = readPreference<unknown>(platform.storage, 'whip.localHostName', undefined);
    const localName = native ? local.label : typeof alias === 'string' && alias.trim() && alias.length <= 256 && !/[\u0000-\u001f\u007f]/.test(alias) ? alias : 'Local';
    this.records.set('local', this.record({ id: 'local', name: localName, url: native ? '' : daemonEndpoint((fallback.target as { endpoint: string }).endpoint), runtime_id: local.runtimeId ?? '', connect_on_launch: true }, { ...local, label: localName }, native));
    for (const profile of this.deviceProfiles) if (profile.target.kind === 'ssh') {
      this.records.set(profile.id, this.record({ id: profile.id, name: profile.label, url: '', runtime_id: profile.runtimeId ?? '', connect_on_launch: profile.id === selectedId }, profile, true));
    }
    this.snapshot = { ...this.snapshot, selectedId: selectedId && !selectedId.startsWith('url:') ? selectedId : 'local',
      legacyProfiles: this.deviceProfiles.filter(profile => profile.target.kind === 'url'),
      notice: saved?.notice ?? (this.selected?.target.kind === 'url' ? 'Import your previously selected address in Settings → Servers to reconnect. Its saved identity and tabs are preserved.' : undefined) };
    this.publish();
  }
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { if (this.listeners.size >= 64 && !this.listeners.has(listener)) throw new Error('Host observer limit reached'); this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private record(profile: HostProfile, target: ConnectionProfile = { id: profile.id, label: profile.name, runtimeId: profile.runtime_id || undefined, target: { kind: 'url', endpoint: profile.url } }, device = false): ConnectionRecord {
    return { profile: Object.freeze({ ...profile }), target, device, verified: false, state: 'closed', retryCount: 0, waits: new AbortController() };
  }
  private publish(patch: Partial<HostsSnapshot> = {}) {
    this.snapshot = Object.freeze({ ...this.snapshot, ...patch, hosts: Object.freeze([...this.records.values()].map(record => Object.freeze({
      id: record.profile.id, name: record.profile.name, endpoint: record.profile.url, local: record.profile.id === 'local',
      runtimeId: record.profile.runtime_id || undefined, connectOnLaunch: record.profile.connect_on_launch,
      state: record.state,
      profile: record.target, device: record.device, progress: record.progress, localRuntime: record.localRuntime,
      client: record.verified ? record.client : undefined, list: record.list,
      error: record.error,
    }))) });
    for (const listener of this.listeners) listener();
  }
  host(runtimeId: string) { return this.snapshot.hosts.find(host => host.runtimeId === runtimeId); }
  home() { return this.snapshot.hosts.find(host => host.local)!; }
  isAttached(client: Client) {
    return !this.closed && [...this.records.values()].some(record => record.verified && record.client === client);
  }
  signal(client: Client): AbortSignal {
    const record = [...this.records.values()].find(record => record.client === client && record.verified);
    if (!record || this.closed) throw new Error('This host is no longer attached');
    return record.waits.signal;
  }
  private async createClient(endpoint: ResolvedConnection['endpoint'], expected: string | undefined, lifetime: AbortSignal, failed?: (error: unknown) => void) {
    if (this.closed) throw new Error('Application has been disposed');
    lifetime.throwIfAborted();
    let clientID = this.platform.storage.getItem('whip.web.client.v1');
    if (!clientID) {
      if (!globalThis.crypto?.randomUUID) throw new Error('Open the web app on HTTPS or localhost to connect to a daemon');
      clientID = crypto.randomUUID();
      this.platform.storage.setItem('whip.web.client.v1', clientID);
    }
    let transport: Transport;
    if (typeof endpoint === 'string') {
      const discovery = await discoverGateway(endpoint, { signal: lifetime });
      if (expected && discovery.runtime_id !== expected) throw new ChangedIdentityError();
      transport = browserSocket(endpoint, { expectedRuntimeID: discovery.runtime_id, expectedProcessEpoch: discovery.process_epoch });
      expected = discovery.runtime_id;
    } else transport = framedTransport(endpoint);
    const scoped: Transport = async (request, runtimeID, options) => {
      const signal = options.signal ? AbortSignal.any([lifetime, options.signal]) : lifetime;
      signal.throwIfAborted();
      try { return await transport(request, runtimeID, { ...options, signal }); }
      catch (error) {
        if (!signal.aborted && (error instanceof DeliveryError || error instanceof TypeError || error instanceof RemoteError && error.kind === 'IDENTITY')) failed?.(error);
        throw error;
      }
    };
    try {
      const client = await Client.connect(scoped, { clientID, expectedRuntimeID: expected, signal: lifetime });
      lifetime.throwIfAborted();
      if (typeof endpoint !== 'string') transport = framedTransport(endpoint, { expectedProcessEpoch: client.processEpoch });
      return client;
    } catch (error) {
      if (typeof endpoint !== 'string' && /runtime identity mismatch/i.test(errorMessage(error))) throw new ChangedIdentityError();
      throw error;
    }
  }
  connect(id = 'local'): Promise<void> {
    if (this.closed) return Promise.reject(new Error('Application has been disposed'));
    const record = this.records.get(id);
    if (!record) return Promise.reject(new Error('This host is no longer saved'));
    if (record.setup) return record.setup.promise;
    if (record.verified && record.client) return Promise.resolve();
    return this.establish(record, false);
  }
  private establish(record: ConnectionRecord, recovering: boolean): Promise<void> {
    clearTimeout(record.retry); record.retry = undefined;
    const controller = new AbortController();
    const setup = { controller, promise: Promise.resolve() };
    record.setup = setup;
    record.waits = new AbortController();
    if (!recovering) { record.preparation?.abort(); record.preparation = controller; }
    record.state = recovering ? 'stale' : 'connecting';
    if (!recovering) record.error = undefined;
    const connect = async () => {
      try {
        controller.signal.throwIfAborted();
        if (!recovering) {
          record.resolved?.dispose(); record.resolved = undefined;
          if (record.target.target.kind === 'local' && this.platform.localRuntime) {
            const status = await this.platform.localRuntime.test();
            controller.signal.throwIfAborted();
            record.localRuntime = Object.freeze({ ...status });
            this.publish();
            if (status.state === 'missing') throw new LocalRuntimeSetupRequiredError();
          }
          record.endpoint = record.profile.url;
          if (this.platform.resolveConnection) {
            const resolved = await this.platform.resolveConnection(record.target, { signal: controller.signal, onProgress: message => {
              if (record.setup === setup) { record.progress = message.slice(0, 1024); this.publish(); }
            } });
            if (controller.signal.aborted || record.setup !== setup || this.closed) { resolved.dispose(); controller.signal.throwIfAborted(); throw new Error('Connection setup was retired'); }
            record.resolved = resolved; record.endpoint = resolved.endpoint;
          }
        }
        controller.signal.throwIfAborted();
        if (record.endpoint === undefined) throw new Error('Reconnect this host to prepare its connection');
        let client: Client | undefined;
        const lifetime = AbortSignal.any([controller.signal, record.waits.signal, ...(record.preparation ? [record.preparation.signal] : [])]);
        client = await this.createClient(record.endpoint, record.profile.runtime_id || undefined, lifetime, error => {
          if (client) this.stale(record, client, error);
        });
        controller.signal.throwIfAborted();
        if (record.setup !== setup || this.closed) throw new Error('Connection setup was retired');
        this.attached(record, client);
        if (record.client !== client || !record.verified) throw new Error('Host was disconnected while connecting');
      } catch (error) {
        if (record.setup === setup) {
          record.setup = undefined;
          if (!(error instanceof LocalRuntimeSetupRequiredError)) record.error = errorMessage(error).slice(0, 1024);
          if (recovering && !(error instanceof ChangedIdentityError)) {
            record.state = 'stale'; controller.abort(); this.retry(record);
          } else this.detach(record);
          this.publish();
        }
        throw error;
      } finally {
        if (record.setup === setup) { record.setup = undefined; record.progress = undefined; this.publish(); if (record.state === 'stale') this.retry(record); }
      }
    };
    // Defer I/O until the complete setup promise is installed, including for a
    // synchronous subscriber which calls connect again while publishing.
    setup.promise = Promise.resolve().then(connect);
    this.publish();
    return setup.promise;
  }
  private retry(record: ConnectionRecord) {
    if (this.closed || record.retry || record.setup || record.endpoint === undefined) return;
    const delay = Math.min(10_000, 500 * 2 ** Math.min(record.retryCount++, 5));
    record.retry = setTimeout(() => {
      record.retry = undefined;
      if (!this.closed && this.records.get(record.profile.id) === record && record.state === 'stale') void this.establish(record, true).catch(() => {});
    }, delay);
  }
  private stale(record: ConnectionRecord, client: Client, error: unknown) {
    if (this.closed || record.client !== client) return;
    record.client = undefined; record.retiredClient = client; record.verified = false; record.state = 'stale';
    record.error = errorMessage(error).slice(0, 1024);
    record.waits.abort();
    void record.list?.suspend();
    this.effects.detached(client, record.profile.runtime_id, { recovering: true });
    this.publish(); this.retry(record);
  }
  async connectOnLaunch() {
    const local = this.connect('local');
    // Saved remote hosts restore in the background; they must not gate the shell.
    for (const record of this.records.values())
      if (record.profile.id !== 'local' && record.profile.connect_on_launch)
        void this.connect(record.profile.id).catch(() => {});
    await local.catch(() => {});
  }
  private persistDevice(profile: ConnectionProfile) {
    if (this.preserveDeviceRecords) throw new Error('Saved execution hosts need recovery. Original device records have been preserved.');
    if (!this.deviceProfiles.some(item => item.id === profile.id) && this.deviceProfiles.length >= 16) throw new Error('Saved host storage is full. Import an old URL or remove a host before saving another.');
    const previous = this.selected ?? profile;
    const selected = previous.id === profile.id ? profile : previous;
    this.deviceProfiles = saveConnections(this.platform.storage, [profile, ...this.deviceProfiles.filter(item => item.id !== profile.id)], selected);
    this.selected = selected;
  }
  async saveNative(profile: ConnectionProfile, acceptChangedIdentity = false, signal?: AbortSignal): Promise<string> {
    signal?.throwIfAborted();
    let target = validateProfile(profile);
    const duplicate = [...this.records.values()].find(record => record.device && JSON.stringify(record.target.target) === JSON.stringify(target.target));
    if (duplicate && !this.records.has(target.id)) target = { ...target, id: duplicate.target.id, runtimeId: duplicate.target.runtimeId };
    if (target.target.kind === 'url' || !this.platform.connectionKinds?.includes(target.target.kind)) throw new Error('Unsupported native connection');
    const existing = this.records.get(target.id);
    if (existing && !existing.device) throw new Error('This host ID belongs to a saved URL');
    if (existing && profile.runtimeId && existing.profile.runtime_id !== profile.runtimeId) throw new Error('This saved host changed. Reopen its settings before editing it.');
    if (!existing && this.deviceProfiles.length >= 16) throw new Error('There are 16 saved native hosts. Remove a host before adding another.');
    const expected = existing?.profile.runtime_id ?? target.runtimeId;
    const remembered = { ...target, runtimeId: acceptChangedIdentity ? undefined : expected };
    if (existing) this.detach(existing);
    const record = this.record({ id: target.id, name: target.label, url: '', runtime_id: remembered.runtimeId ?? '', connect_on_launch: true }, remembered, true);
    this.records.set(target.id, record);
    this.publish();
    const cancel = () => { if (this.records.get(target.id) === record) this.disconnect(target.id); };
    signal?.addEventListener('abort', cancel, { once: true });
    try { await this.connect(target.id); signal?.throwIfAborted(); this.persistDevice(record.target); }
    catch (error) {
      if (this.records.get(target.id) === record) {
        this.detach(record);
        const message = signal?.aborted ? undefined : errorMessage(error);
        if (existing) { this.records.set(target.id, existing); existing.error = message; }
        else record.error = message;
        this.publish();
      }
      throw error;
    }
    finally { signal?.removeEventListener('abort', cancel); }
    return target.id;
  }
  select(id: string) {
    const record = this.records.get(id);
    if (!record) return;
    this.platform.storage.setItem('whip.selectedHost.v3', JSON.stringify(id));
    if (record.device) { this.selected = record.target; this.persistDevice(record.target); }
    this.publish({ selectedId: id });
  }
  async forgetLegacyProfile(id: string) {
    if (this.preserveDeviceRecords) throw new Error('Saved execution hosts need recovery. Original device records have been preserved.');
    const profiles = this.deviceProfiles.filter(profile => profile.id !== id);
    const selected = this.selected?.id === id ? this.records.get('local')!.target : this.selected ?? this.records.get('local')!.target;
    this.deviceProfiles = saveConnections(this.platform.storage, profiles, selected);
    this.selected = selected;
    const local = this.records.get('local')!;
    if (local.device && local.target.runtimeId) this.persistDevice(local.target);
    this.publish({ legacyProfiles: this.deviceProfiles.filter(profile => profile.target.kind === 'url'), notice: undefined });
  }
  private attached(record: ConnectionRecord, client: Client) {
    const alias = [...this.records.values()].find(other => other !== record && other.profile.runtime_id === client.runtimeID);
    if (alias) throw new ChangedIdentityError(`This daemon is already connected as ${alias.profile.name}`);
    record.verified = true; record.client = client; record.retiredClient = undefined; record.state = 'connected'; record.error = undefined;
    record.profile = Object.freeze({ ...record.profile, runtime_id: client.runtimeID });
    if (record.target.runtimeId !== client.runtimeID) {
      record.target = { ...record.target, runtimeId: client.runtimeID };
      if (record.device) {
        try { this.persistDevice(record.target); } catch (error) { record.error = errorMessage(error).slice(0, 1024); }
      }
    }
    const retained = record.list;
    record.list ??= createTreeCatalogView(client);
    const list = record.list;
    this.publish();
    if (!this.isAttached(client)) return;
    this.effects.connected(client.runtimeID, client);
    if (!this.isAttached(client)) return;
    void (retained ? retained.reconnect(client) : list.start()).then(
      () => { if (record.client === client) record.retryCount = 0; },
      error => this.stale(record, client, error),
    );
    if (record.profile.id === 'local') void this.refreshProfiles().catch(() => {});
  }
  disconnect(id: string) {
    const record = this.records.get(id);
    if (!record) return;
    this.detach(record);
    this.publish();
  }
  private detach(record: ConnectionRecord) {
    clearTimeout(record.retry); record.retry = undefined; record.retryCount = 0;
    record.setup?.controller.abort();
    record.setup = undefined;
    record.progress = undefined;
    record.waits.abort();
    record.preparation?.abort(); record.preparation = undefined;
    const client = record.client ?? record.retiredClient;
    record.client = undefined; record.retiredClient = undefined;
    record.verified = false;
    record.state = 'closed';
    record.endpoint = undefined;
    void record.list?.dispose();
    record.list = undefined;
    if (client) this.effects.detached(client, record.profile.runtime_id, { recovering: false });
    record.resolved?.dispose();
    record.resolved = undefined;
  }
  refreshProfiles(): Promise<void> {
    const record = this.records.get('local')!;
    const client = record.verified ? record.client : undefined;
    if (!client || record.state !== 'connected') return Promise.reject(new Error('Reconnect Local to manage saved hosts'));
    const version = this.configurationVersion;
    const pending = this.refreshing;
    if (pending?.client === client && pending.version === version) return pending.promise;
    const signal = record.waits.signal;
    const refresh = async () => {
      try {
        const configuration = await client.hosts.profiles({ signal });
        if (!this.isAttached(client) || version !== this.configurationVersion) return;
        this.applyProfiles(configuration);
      } catch (error) {
        if (this.isAttached(client) && version === this.configurationVersion)
          this.publish({ profilesReady: false, profileError: errorMessage(error) });
        throw error;
      }
    };
    const promise = refresh().finally(() => { if (this.refreshing?.promise === promise) this.refreshing = undefined; });
    this.refreshing = { client, version, promise };
    return promise;
  }
  private applyProfiles(configuration: HostProfiles) {
    if (!Array.isArray(configuration.profiles)) throw new Error('Update the local daemon to save remote hosts in its configuration');
    // Validate the whole document before detaching any healthy connection.
    const profiles = normalizeProfiles(configuration.profiles);
    if (profiles.some(profile => this.records.get(profile.id)?.device)) throw new Error('A remote profile conflicts with a native host ID');
    this.revision = configuration.revision;
    ++this.configurationVersion;
    for (const [id, record] of this.records) {
      if (id !== 'local' && !record.device && !profiles.some(profile => profile.id === id)) { this.detach(record); this.records.delete(id); }
    }
    const connect: string[] = [];
    for (const profile of profiles) {
      let record = this.records.get(profile.id);
      const changed = record && (record.profile.url !== profile.url || record.profile.runtime_id !== profile.runtime_id);
      if (changed) this.detach(record!);
      if (!record) { record = this.record(profile); this.records.set(profile.id, record); if (profile.connect_on_launch) connect.push(profile.id); }
      else { record.profile = Object.freeze({ ...profile }); record.target = { id: profile.id, label: profile.name, runtimeId: profile.runtime_id, target: { kind: 'url', endpoint: profile.url } }; if (changed && profile.connect_on_launch) connect.push(profile.id); }
    }
    this.publish({ profiles: Object.freeze(profiles.map(profile => Object.freeze({ ...profile }))), profilesReady: true, profileError: undefined });
    for (const id of connect) void this.connect(id).catch(() => {});
  }
  private async writeProfiles(profiles: readonly HostProfile[], revision = this.revision, client = this.home().client) {
    if (!client || !this.isAttached(client) || !revision || !this.snapshot.profilesReady)
      throw new Error('Reconnect Local and load its configuration before saving hosts');
    const version = this.configurationVersion;
    const normalized = normalizeProfiles(profiles);
    try {
      const configuration = await client.hosts.setProfiles(revision, normalized, { signal: this.signal(client) });
      if (!this.isAttached(client)) {
        ++this.configurationVersion;
        throw new Error('Local changed while saving hosts. Reload its configuration to confirm the saved hosts.');
      }
      if (version === this.configurationVersion) this.applyProfiles(configuration);
      else {
        // A concurrent read or write already changed our snapshot. Re-read instead
        // of applying an older response, and invalidate reads begun before it.
        ++this.configurationVersion;
        await this.refreshProfiles();
      }
    } catch (error) {
      await this.refreshProfiles().catch(() => {});
      throw error;
    }
  }
  async save(profile: Omit<HostProfile, 'runtime_id'> & { runtime_id?: string }, acceptChangedIdentity = false, importId?: string): Promise<string> {
    const endpoint = daemonEndpoint(profile.url);
    const name = profile.name.trim();
    if (!name || !profile.id.trim() || /[\r\n\t\0]/.test(name + profile.id) || profile.id === 'local')
      throw new Error('Enter a valid ID and name for the remote host');
    const profiles = this.snapshot.profiles;
    const revision = this.revision;
    const source = this.home().client;
    if (!source || !revision || !this.snapshot.profilesReady || this.home().state !== 'connected')
      throw new Error('Reconnect Local and load its configuration before saving hosts');
    const existing = profiles.find(host => host.id === profile.id);
    const legacy = importId ? this.deviceProfiles.find(item => item.id === importId && item.target.kind === 'url') : undefined;
    if (importId && (!legacy || legacy.runtimeId !== profile.runtime_id)) throw new Error('This saved desktop address changed. Reopen it before importing.');
    if (!legacy && profile.runtime_id && existing?.runtime_id !== profile.runtime_id)
      throw new Error('This saved host changed. Reopen its settings before editing it.');
    let runtimeId = existing?.runtime_id;
    if (!existing || daemonEndpoint(existing.url) !== endpoint || acceptChangedIdentity) {
      if (this.probes.size >= 8) throw new Error('Host verification is busy; wait for another check to finish');
      const probe = new AbortController();
      this.probes.add(probe);
      try { runtimeId = (await this.createClient(endpoint, undefined, probe.signal)).runtimeID; }
      finally { probe.abort(); this.probes.delete(probe); }
    }
    const expected = existing?.runtime_id ?? profile.runtime_id;
    if (expected && expected !== runtimeId && !acceptChangedIdentity)
      throw new Error('This address serves a different daemon. Accept the new daemon identity to save it; existing tabs will keep their original identity.');
    const alias = this.snapshot.hosts.find(host => host.runtimeId === runtimeId && host.id !== profile.id);
    if (alias) throw new Error(`This daemon is already saved as ${alias.name}`);
    const saved: HostProfile = { ...profile, name, url: existing && daemonEndpoint(existing.url) === endpoint ? existing.url : endpoint, runtime_id: runtimeId! };
    await this.writeProfiles([...profiles.filter(host => host.id !== profile.id), saved], revision, source);
    return profile.id;
  }
  async setConnectOnLaunch(id: string, value: boolean) {
    await this.writeProfiles(this.snapshot.profiles.map(host => host.id === id ? { ...host, connect_on_launch: value } : host));
  }
  /** A display-name change preserves the attached client and daemon identity. */
  async rename(id: string, value: string) {
    const name = value.trim();
    if (!name || [...name].length > 256 || /[\u0000-\u001f\u007f]/.test(name)) throw new Error('Enter a server name between 1 and 256 characters.');
    const record = this.records.get(id);
    if (!record || this.closed) throw new Error('This server is no longer available.');
    if (name === record.profile.name) return;
    if (!record.device && id !== 'local') {
      await this.writeProfiles(this.snapshot.profiles.map(host => host.id === id ? { ...host, name } : host));
      return;
    }
    const target = { ...record.target, label: name };
    if (record.device) this.persistDevice(target);
    else this.platform.storage.setItem('whip.localHostName', JSON.stringify(name));
    record.target = target;
    record.profile = Object.freeze({ ...record.profile, name });
    this.publish();
  }
  async remove(id: string) {
    if (id === 'local') throw new Error('Local owns this workspace’s saved hosts');
    const record = this.records.get(id);
    if (record?.device) {
      await this.forgetLegacyProfile(id);
      this.detach(record); this.records.delete(id); this.publish(); return;
    }
    await this.writeProfiles(this.snapshot.profiles.filter(host => host.id !== id));
  }
  dispose() {
    if (this.closed) return;
    this.closed = true;
    for (const probe of this.probes) probe.abort();
    this.probes.clear();
    for (const record of this.records.values()) this.detach(record);
    this.listeners.clear();
  }
}
