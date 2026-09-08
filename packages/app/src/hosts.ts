import { createWhipClient, type ConnectionState, type RecoveryStorage, type WhipClient } from '@whip/sdk';
import { createSessionListView, type SessionListView } from '@whip/sdk/state';
import type { RuntimeConfiguration } from '@whip/protocol';
import { errorMessage, type AppPlatform } from './platform';

export type HostProfile = NonNullable<RuntimeConfiguration['remote_hosts']>[number];
export interface HostConnection {
  readonly id: string;
  readonly name: string;
  readonly endpoint: string;
  readonly local: boolean;
  readonly runtimeId?: string;
  readonly connectOnLaunch: boolean;
  readonly state: ConnectionState;
  readonly client?: WhipClient;
  readonly list?: SessionListView;
  readonly error?: string;
}
interface ConnectionRecord {
  profile: HostProfile;
  client?: WhipClient;
  list?: SessionListView;
  verified: boolean;
  error?: string;
  waits: AbortController;
  unsubscribe?: () => void;
  connectionId?: string;
}
interface HostsSnapshot {
  readonly hosts: readonly HostConnection[];
  readonly profiles: readonly HostProfile[];
  readonly profilesReady: boolean;
  readonly profileError?: string;
}

export function daemonEndpoint(value: string): string {
  const url = new URL(value.trim());
  if (!['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol) || !url.hostname)
    throw new Error('Enter an HTTP or WebSocket daemon endpoint');
  if (url.username || url.password || url.search || url.hash || url.href.includes('?'))
    throw new Error('Daemon addresses cannot contain credentials, a query, or a fragment');
  if (!['', '/', '/api/v3/ws'].includes(url.pathname))
    throw new Error('Use a root-mounted daemon endpoint, without a proxy path prefix');
  url.protocol = url.protocol === 'ws:' ? 'http:' : url.protocol === 'wss:' ? 'https:' : url.protocol;
  url.pathname = '/';
  return url.origin;
}

function normalizeProfiles(profiles: readonly HostProfile[]): HostProfile[] {
  const ids = new Set<string>();
  const endpoints = new Set<string>();
  const runtimes = new Set<string>();
  return profiles.map(profile => {
    if (profile.id === 'local') throw new Error('A remote profile cannot replace Local');
    const normalized = { ...profile, name: profile.name.trim(), url: daemonEndpoint(profile.url) };
    if ([normalized.id, normalized.name, normalized.runtime_id].some(value => !value.trim() || /[\r\n\t\0]/.test(value)))
      throw new Error('A remote host requires an ID, name, and verified runtime ID');
    if (ids.has(normalized.id) || endpoints.has(normalized.url) || runtimes.has(normalized.runtime_id))
      throw new Error(`Remote host ${normalized.name} duplicates a saved profile, endpoint, or runtime`);
    ids.add(normalized.id); endpoints.add(normalized.url); runtimes.add(normalized.runtime_id);
    return normalized;
  });
}

/** Window-owned connections; saved profiles have exactly one owner: the local daemon. */
export class HostConnections {
  private readonly records = new Map<string, ConnectionRecord>();
  private readonly listeners = new Set<() => void>();
  private readonly probes = new Set<WhipClient>();
  private snapshot: HostsSnapshot = Object.freeze({ hosts: [], profiles: [], profilesReady: false });
  private revision?: string;
  private configurationVersion = 0;
  private refreshing?: { client: WhipClient; connectionId: string; version: number; promise: Promise<void> };
  private closed = false;

  constructor(
    private readonly platform: AppPlatform,
    private readonly recovery: RecoveryStorage,
    private readonly effects: { connected(runtimeId: string): void; detached(client: WhipClient, runtimeId: string): void },
  ) {
    this.records.set('local', this.record({ id: 'local', name: 'Local', url: daemonEndpoint(platform.defaultEndpoint), runtime_id: '', connect_on_launch: true }));
    this.publish();
  }
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private record(profile: HostProfile): ConnectionRecord {
    return { profile: Object.freeze({ ...profile }), verified: false, waits: new AbortController() };
  }
  private publish(patch: Partial<HostsSnapshot> = {}) {
    this.snapshot = Object.freeze({ ...this.snapshot, ...patch, hosts: Object.freeze([...this.records.values()].map(record => Object.freeze({
      id: record.profile.id, name: record.profile.name, endpoint: record.profile.url, local: record.profile.id === 'local',
      runtimeId: record.profile.runtime_id || undefined, connectOnLaunch: record.profile.connect_on_launch,
      state: record.client?.getSnapshot().state ?? 'closed',
      client: record.verified ? record.client : undefined, list: record.verified ? record.list : undefined,
      error: record.error ?? record.client?.getSnapshot().error?.message,
    }))) });
    for (const listener of this.listeners) listener();
  }
  host(runtimeId: string) { return this.snapshot.hosts.find(host => host.runtimeId === runtimeId); }
  home() { return this.snapshot.hosts.find(host => host.local)!; }
  isAttached(client: WhipClient) {
    return !this.closed && [...this.records.values()].some(record => record.verified && record.client === client);
  }
  signal(client: WhipClient): AbortSignal {
    const record = [...this.records.values()].find(record => record.client === client && record.verified);
    if (!record || this.closed) throw new Error('This host is no longer attached');
    return record.waits.signal;
  }
  private createClient(endpoint: string, reconnect = true) {
    if (this.closed) throw new Error('Application has been disposed');
    let clientId = this.platform.storage.getItem('whip.web.client.v1');
    if (!clientId) {
      if (!globalThis.crypto?.randomUUID) throw new Error('Open the web app on HTTPS or localhost to connect to a daemon');
      clientId = crypto.randomUUID();
      this.platform.storage.setItem('whip.web.client.v1', clientId);
    }
    return createWhipClient({ endpoint, clientId, clientKind: 'human', recoveryStorage: this.recovery, reconnect });
  }
  async connect(id = 'local'): Promise<void> {
    if (this.closed) throw new Error('Application has been disposed');
    const record = this.records.get(id);
    if (!record) throw new Error('This host is no longer saved');
    if (!record.client) {
      record.error = undefined;
      const client = this.createClient(record.profile.url);
      record.client = client;
      record.waits = new AbortController();
      record.unsubscribe = client.subscribe(() => this.connectionChanged(record, client));
      this.publish();
    }
    const client = record.client;
    await client.connect();
    // Also covers transports which were already connected when attached.
    this.connectionChanged(record, client);
    if (!record.verified || record.client !== client) throw new Error(record.error ?? 'Host was disconnected while connecting');
  }
  private connectionChanged(record: ConnectionRecord, client: WhipClient) {
    if (this.closed || record.client !== client) return;
    const connection = client.getSnapshot();
    if (connection.state === 'connected' && connection.info) {
      const info = connection.info;
      const expected = record.profile.runtime_id;
      const alias = [...this.records.values()].find(other => other !== record && other.profile.runtime_id === info.runtime_id);
      if (expected && expected !== info.runtime_id || alias && record.profile.id !== 'local') {
        record.error = alias ? `This daemon is already connected as ${alias.profile.name}` : 'This address now serves a different daemon. Edit the host and explicitly accept its new identity to reconnect.';
        record.verified = false;
        // Finish the SDK's connection notification before closing its transport.
        queueMicrotask(() => { if (record.client === client) { this.detach(record); this.publish(); } });
        this.publish();
        return;
      }
      record.verified = true;
      record.profile = Object.freeze({ ...record.profile, runtime_id: info.runtime_id });
      if (!record.list) {
        record.list = createSessionListView(client);
        void record.list.start().catch(error => { if (record.client === client) { record.error = errorMessage(error); this.publish(); } });
      }
      if (record.connectionId !== info.connection_id) {
        record.connectionId = info.connection_id;
        this.effects.connected(info.runtime_id);
        if (record.profile.id === 'local') void this.refreshProfiles().catch(() => {});
      }
    }
    this.publish();
  }
  disconnect(id: string) {
    const record = this.records.get(id);
    if (!record) return;
    this.detach(record);
    this.publish();
  }
  private detach(record: ConnectionRecord) {
    record.waits.abort();
    record.unsubscribe?.();
    record.unsubscribe = undefined;
    const client = record.client;
    record.client = undefined;
    record.verified = false;
    record.connectionId = undefined;
    void record.list?.dispose();
    record.list = undefined;
    if (client) { this.effects.detached(client, record.profile.runtime_id); client.close(); }
  }
  refreshProfiles(): Promise<void> {
    const record = this.records.get('local')!;
    const client = record.verified ? record.client : undefined;
    const connection = client?.getSnapshot();
    if (!client || connection?.state !== 'connected' || !connection.info) return Promise.reject(new Error('Reconnect Local to manage saved hosts'));
    const connectionId = connection.info.connection_id;
    const version = this.configurationVersion;
    const pending = this.refreshing;
    if (pending?.client === client && pending.connectionId === connectionId && pending.version === version) return pending.promise;
    const signal = record.waits.signal;
    const refresh = async () => {
      try {
        const configuration = await client.configuration.get({ signal });
        if (!this.currentConnection(client, connectionId) || version !== this.configurationVersion) return;
        this.applyProfiles(configuration);
      } catch (error) {
        if (this.currentConnection(client, connectionId) && version === this.configurationVersion)
          this.publish({ profilesReady: false, profileError: errorMessage(error) });
        throw error;
      }
    };
    const promise = refresh().finally(() => { if (this.refreshing?.promise === promise) this.refreshing = undefined; });
    this.refreshing = { client, connectionId, version, promise };
    return promise;
  }
  private currentConnection(client: WhipClient, connectionId: string) {
    const connection = client.getSnapshot();
    return this.isAttached(client) && connection.state === 'connected' && connection.info?.connection_id === connectionId;
  }
  private applyProfiles(configuration: RuntimeConfiguration) {
    if (!Array.isArray(configuration.remote_hosts)) throw new Error('Update the local daemon to save remote hosts in its configuration');
    // Validate the whole document before detaching any healthy connection.
    const profiles = normalizeProfiles(configuration.remote_hosts);
    this.revision = configuration.revision;
    ++this.configurationVersion;
    for (const [id, record] of this.records) {
      if (id !== 'local' && !profiles.some(profile => profile.id === id)) { this.detach(record); this.records.delete(id); }
    }
    const connect: string[] = [];
    for (const profile of profiles) {
      let record = this.records.get(profile.id);
      const changed = record && (record.profile.url !== profile.url || record.profile.runtime_id !== profile.runtime_id);
      if (changed) this.detach(record!);
      if (!record) { record = this.record(profile); this.records.set(profile.id, record); if (profile.connect_on_launch) connect.push(profile.id); }
      else { record.profile = Object.freeze({ ...profile }); if (changed && profile.connect_on_launch) connect.push(profile.id); }
    }
    this.publish({ profiles: Object.freeze(profiles.map(profile => Object.freeze({ ...profile }))), profilesReady: true, profileError: undefined });
    for (const id of connect) void this.connect(id).catch(() => {});
  }
  private async writeProfiles(profiles: readonly HostProfile[], revision = this.revision) {
    const client = this.home().client;
    if (!client || client.getSnapshot().state !== 'connected' || !revision || !this.snapshot.profilesReady)
      throw new Error('Reconnect Local and load its configuration before saving hosts');
    const connectionId = client.requireConnected().connection_id;
    const version = this.configurationVersion;
    const normalized = normalizeProfiles(profiles);
    try {
      const configuration = await client.configuration.update({ revision, remote_hosts: normalized }, { signal: this.signal(client) });
      if (!this.currentConnection(client, connectionId)) {
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
  async save(profile: Omit<HostProfile, 'runtime_id'> & { runtime_id?: string }, acceptChangedIdentity = false): Promise<string> {
    const endpoint = daemonEndpoint(profile.url);
    const name = profile.name.trim();
    if (!name || !profile.id.trim() || /[\r\n\t\0]/.test(name + profile.id) || profile.id === 'local')
      throw new Error('Enter a valid ID and name for the remote host');
    const profiles = this.snapshot.profiles;
    const revision = this.revision;
    if (!revision || !this.snapshot.profilesReady || this.home().state !== 'connected')
      throw new Error('Reconnect Local and load its configuration before saving hosts');
    const existing = profiles.find(host => host.id === profile.id);
    if (profile.runtime_id && existing?.runtime_id !== profile.runtime_id)
      throw new Error('This saved host changed. Reopen its settings before editing it.');
    let runtimeId = existing?.runtime_id;
    if (!existing || existing.url !== endpoint || acceptChangedIdentity) {
      const probe = this.createClient(endpoint, false);
      this.probes.add(probe);
      try { await probe.connect(); runtimeId = probe.requireConnected().runtime_id; }
      finally { probe.close(); this.probes.delete(probe); }
    }
    const expected = existing?.runtime_id ?? profile.runtime_id;
    if (expected && expected !== runtimeId && !acceptChangedIdentity)
      throw new Error('This address serves a different daemon. Accept the new daemon identity to save it; existing tabs will keep their original identity.');
    const alias = this.snapshot.hosts.find(host => host.runtimeId === runtimeId && host.id !== profile.id);
    if (alias) throw new Error(`This daemon is already saved as ${alias.name}`);
    const saved: HostProfile = { ...profile, name, url: endpoint, runtime_id: runtimeId! };
    await this.writeProfiles([...profiles.filter(host => host.id !== profile.id), saved], revision);
    return profile.id;
  }
  async setConnectOnLaunch(id: string, value: boolean) {
    await this.writeProfiles(this.snapshot.profiles.map(host => host.id === id ? { ...host, connect_on_launch: value } : host));
  }
  async remove(id: string) {
    if (id === 'local') throw new Error('Local owns this workspace’s saved hosts');
    await this.writeProfiles(this.snapshot.profiles.filter(host => host.id !== id));
  }
  dispose() {
    if (this.closed) return;
    this.closed = true;
    for (const probe of this.probes) probe.close();
    this.probes.clear();
    for (const record of this.records.values()) this.detach(record);
    this.listeners.clear();
  }
}
