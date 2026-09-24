import { randomBytes } from 'node:crypto';
import { chmod, mkdir, open, rename, rm } from 'node:fs/promises';
import path from 'node:path';
import { isIP } from 'node:net';
import { PreviewBudget } from './preview-budget';
import { createPreviewProxy, previewDestination, isPublicPreviewAddress, matchesPreviewLoopback, type PreviewLoopback } from './preview-proxy';
import type { SSHPreviewRequest, SSHPreviewRoute } from './preview-ssh-route';

export interface PreviewIdentity { savedHostId: string; runtimeId: string; projectId: string }
export interface PreviewDestination { remoteHost: PreviewLoopback; port: number }
export interface PreviewScope extends PreviewIdentity { connectionGeneration: string; destinations: PreviewDestination[] }
export interface PreviewConnection {
  generation: string; signal: AbortSignal;
  acquirePreviewRoute(request: SSHPreviewRequest, signal: AbortSignal): Promise<SSHPreviewRoute>;
}
export interface PreviewContents { id: number; session: object }
export interface PreviewAuthInfo { isProxy: boolean; host: string; port: number; scheme: string; realm: string }
interface Profile { id: string; identity: PreviewIdentity }
interface Environment {
  id: string; partition: string; scope: PreviewScope; revision: number;
  lifetime: AbortController; routes: Map<number, SSHPreviewRoute>;
  proxy: Awaited<ReturnType<typeof createPreviewProxy>>;
  tabs: Set<string>; contents: Map<number, object>;
  connectionSignal: AbortSignal; disconnect(): void;
}
export interface PreviewEnvironmentLease {
  environmentId: string; partition: string;
  proxyConfig: { mode: 'fixed_servers'; proxyRules: string; proxyBypassRules: string };
  bindContents(contents: PreviewContents): () => void;
  authenticate(contents: PreviewContents | null, info: PreviewAuthInfo): { username: string; password: string } | undefined;
  release(): Promise<void>;
}
export type PreviewLease = PreviewEnvironmentLease;
export interface PreviewEnvironmentOptions {
  directory: string;
  connection(identity: PreviewIdentity): PreviewConnection | undefined;
  invalidateAttachments(environmentId: string, reason: string): void;
  onDisconnected?(environmentId: string): void;
}
function identityKey(identity: PreviewIdentity) {
  for (const value of [identity.savedHostId, identity.runtimeId, identity.projectId])
    if (typeof value !== 'string' || !value || value.length > 512 || /[\u0000-\u001f\u007f]/.test(value)) throw new Error('Invalid preview identity');
  return JSON.stringify([identity.savedHostId, identity.runtimeId, identity.projectId]);
}
function destinations(values: PreviewDestination[]) {
  if (!Array.isArray(values) || !values.length || values.length > 4) throw new Error('Approve between one and four preview ports');
  const seen = new Set<number>();
  const result = values.map(value => {
    if (!value || !['127.0.0.1', '::1'].includes(value.remoteHost) || !Number.isInteger(value.port) || value.port < 1 || value.port > 65535 || seen.has(value.port)) throw new Error('Invalid preview destination');
    seen.add(value.port); return { remoteHost: value.remoteHost, port: value.port };
  });
  return result.sort((a, b) => a.port - b.port);
}
function scopeCopy(scope: PreviewScope): PreviewScope {
  identityKey(scope);
  if (typeof scope.connectionGeneration !== 'string' || !scope.connectionGeneration || scope.connectionGeneration.length > 256) throw new Error('Invalid preview connection');
  return { savedHostId: scope.savedHostId, runtimeId: scope.runtimeId, projectId: scope.projectId,
    connectionGeneration: scope.connectionGeneration, destinations: destinations(scope.destinations) };
}
const sameDestinations = (a: PreviewDestination[], b: PreviewDestination[]) => JSON.stringify(destinations(a)) === JSON.stringify(destinations(b));

/** One per native window. Inputs are approved native scopes, never renderer authority. */
export class PreviewEnvironments {
  private readonly budget = new PreviewBudget();
  private readonly environments = new Map<string, Environment>();
  private profiles?: Profile[];
  private queue: Promise<unknown> = Promise.resolve();
  private closed = false;
  private readonly lifetime = new AbortController();
  private readonly reservations = new Map<string, { lease: PreviewLease; timer: NodeJS.Timeout }>();
  constructor(private readonly options: PreviewEnvironmentOptions) {}
  private serial<T>(action: () => Promise<T>): Promise<T> {
    const work = this.queue.then(action); this.queue = work.catch(() => {}); return work;
  }
  private async loadProfiles() {
    if (this.profiles) return this.profiles;
    await mkdir(this.options.directory, { recursive: true, mode: 0o700 }); await chmod(this.options.directory, 0o700);
    let text: string;
    try {
      const file = await open(path.join(this.options.directory, 'profiles.json'), 'r');
      try {
        const status = await file.stat();
        if (!status.isFile() || status.size > 64 * 1024) throw new Error('Invalid or oversized preview profile metadata');
        const bytes = Buffer.alloc(64 * 1024 + 1); const result = await file.read(bytes, 0, bytes.length, 0);
        if (result.bytesRead > 64 * 1024) throw new Error('Preview profile metadata exceeds its limit');
        text = bytes.subarray(0, result.bytesRead).toString('utf8');
      } finally { await file.close(); }
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code === 'ENOENT') { this.profiles = []; return this.profiles; }
      throw error;
    }
    try {
      const value: unknown = JSON.parse(text);
      if (!value || typeof value !== 'object' || !('version' in value) || value.version !== 1 || !('profiles' in value) || !Array.isArray(value.profiles) || value.profiles.length > 64) throw new Error();
      const ids = new Set<string>(); const keys = new Set<string>();
      const profiles = value.profiles.map((profile: Profile) => {
        if (!profile || typeof profile.id !== 'string' || !/^[a-f0-9]{48}$/.test(profile.id) || !profile.identity) throw new Error();
        const key = identityKey(profile.identity);
        if (ids.has(profile.id) || keys.has(key)) throw new Error(); ids.add(profile.id); keys.add(key);
        return { id: profile.id, identity: { savedHostId: profile.identity.savedHostId, runtimeId: profile.identity.runtimeId, projectId: profile.identity.projectId } };
      });
      this.profiles = profiles; return profiles;
    } catch { throw new Error('Preview profile metadata is corrupt; it was not overwritten'); }
  }
  private async persist(profiles: Profile[]) {
    const content = JSON.stringify({ version: 1, profiles });
    if (Buffer.byteLength(content) > 64 * 1024) throw new Error('Preview profile metadata exceeds its limit');
    const target = path.join(this.options.directory, 'profiles.json'); const temporary = `${target}.${randomBytes(8).toString('hex')}.tmp`;
    try {
      const file = await open(temporary, 'wx', 0o600);
      try { await file.writeFile(content); await file.sync(); } finally { await file.close(); }
      await rename(temporary, target); this.profiles = profiles;
    } finally { await rm(temporary, { force: true }); }
  }
  private async profile(identity: PreviewIdentity): Promise<Profile> {
    const key = identityKey(identity); const profiles = await this.loadProfiles();
    const existing = profiles.find(item => identityKey(item.identity) === key);
    if (existing) return existing;
    if (profiles.length >= 64) throw new Error('Preview profile limit reached; remove an unused profile first');
    const profile = { id: randomBytes(24).toString('hex'), identity: { savedHostId: identity.savedHostId, runtimeId: identity.runtimeId, projectId: identity.projectId } };
    await this.persist([...profiles, profile]); return profile;
  }
  /** Inert offer metadata only. This never resolves a connection or creates a route/proxy. */
  async describe(input: PreviewIdentity): Promise<string> {
    identityKey(input);
    const identity = { savedHostId: input.savedHostId, runtimeId: input.runtimeId, projectId: input.projectId };
    return await this.serial(async () => {
      if (this.closed) throw new Error('Preview window closed');
      return (await this.profile(identity)).id;
    });
  }
  private connection(scope: PreviewScope) {
    const connection = this.options.connection(scope);
    if (!connection || connection.signal.aborted || connection.generation !== scope.connectionGeneration) throw new Error('The approved SSH connection changed');
    return connection;
  }
  async ensure(scope: PreviewScope): Promise<string> {
    const lease = await this.acquire(scope, `setup-${randomBytes(12).toString('hex')}`);
    const prior = this.reservations.get(lease.environmentId);
    if (prior) { clearTimeout(prior.timer); await prior.lease.release(); }
    const timer = setTimeout(() => {
      this.reservations.delete(lease.environmentId); void lease.release().catch(() => {});
    }, 30_000); timer.unref();
    this.reservations.set(lease.environmentId, { lease, timer });
    return lease.environmentId;
  }
  async acquire(input: PreviewScope | string, tabId = randomBytes(16).toString('hex')): Promise<PreviewEnvironmentLease> {
    const offered = typeof input === 'string' ? this.environments.get(input)?.scope : input;
    if (!offered) throw new Error('Saved SSH preview requires renewed approval; it is unavailable');
    const scope = scopeCopy(offered);
    if (!tabId || tabId.length > 256) throw new Error('Invalid preview tab');
    const lease = await this.serial(async () => {
      if (this.closed) throw new Error('Preview window closed');
      const connection = this.connection(scope);
      const profile = await this.profile(scope);
      let environment = this.environments.get(profile.id);
      if (environment) {
        this.assertScope(profile.id, scope);
        if (environment.tabs.has(tabId)) throw new Error('Preview tab already owns a lease');
      } else {
        if (this.environments.size >= 4) throw new Error('Preview environment limit reached');
        const lifetime = new AbortController(); const signal = AbortSignal.any([lifetime.signal, connection.signal, this.lifetime.signal]);
        const routes = new Map<number, SSHPreviewRoute>();
        try {
          for (const destination of scope.destinations) routes.set(destination.port,
            await connection.acquirePreviewRoute({ ...destination, expectedGeneration: scope.connectionGeneration }, signal));
          signal.throwIfAborted(); this.connection(scope);
          const proxy = await createPreviewProxy({ budget: this.budget, signal, routes: scope.destinations.map(destination => ({ ...destination, open: operation => routes.get(destination.port)!.open(operation) })) });
          const id = profile.id;
          const disconnect = () => { void this.removeEnvironment(id, 'SSH preview disconnected').catch(() => {}); };
          environment = { id, partition: `persist:whip-preview-${id}`, scope, revision: 1, lifetime, routes, proxy, tabs: new Set(), contents: new Map(), connectionSignal: connection.signal, disconnect };
          this.environments.set(id, environment);
          connection.signal.addEventListener('abort', disconnect, { once: true });
          if (signal.aborted) { await this.removeEnvironment(id, 'SSH preview disconnected'); throw new Error('SSH preview disconnected'); }
        } catch (error) { lifetime.abort(); await Promise.allSettled([...routes.values()].map(route => route.close())); throw error; }
      }
      environment.tabs.add(tabId);
      const owned = environment;
      let released = false; const bound = new Set<number>();
      const current = () => !released && this.environments.get(owned.id) === owned && !owned.lifetime.signal.aborted;
      const lease: PreviewEnvironmentLease = {
        environmentId: owned.id, partition: owned.partition,
        proxyConfig: { mode: 'fixed_servers', proxyRules: owned.proxy.proxyRules, proxyBypassRules: owned.proxy.proxyBypassRules },
        bindContents: contents => {
          if (!current() || !Number.isInteger(contents.id) || !contents.session) throw new Error('Preview environment is unavailable');
          if (owned.contents.has(contents.id)) throw new Error('Preview contents already bound');
          owned.contents.set(contents.id, contents.session); bound.add(contents.id);
          return () => { bound.delete(contents.id); owned.contents.delete(contents.id); };
        },
        authenticate: (contents, info) => {
          const proxy = owned.proxy;
          if (!current() || !contents || !bound.has(contents.id) || owned.contents.get(contents.id) !== contents.session || !info.isProxy || info.host !== proxy.host || info.port !== proxy.port || info.scheme !== 'basic' || info.realm !== proxy.realm) return;
          return { username: proxy.username, password: proxy.password };
        },
        release: async () => {
          if (released) return; released = true;
          for (const id of bound) owned.contents.delete(id); bound.clear();
          owned.tabs.delete(tabId);
          if (!owned.tabs.size && this.environments.get(owned.id) === owned) await this.removeEnvironment(owned.id, 'Last preview tab closed');
        },
      };
      return lease;
    });
    if (typeof input === 'string') {
      const reservation = this.reservations.get(input);
      if (reservation) { this.reservations.delete(input); clearTimeout(reservation.timer); await reservation.lease.release(); }
    }
    return lease;
  }
  assertURL(environmentId: string, value: string) {
    const environment = this.environments.get(environmentId);
    if (!environment || environment.lifetime.signal.aborted) throw new Error('Preview environment unavailable');
    const url = new URL(value); url.hash = '';
    if (!['http:', 'https:'].includes(url.protocol)) throw new Error('Preview URL must be HTTP(S)');
    const target = previewDestination(url.href);
    const approved = environment.scope.destinations.find(item => item.port === target.port);
    if (target.loopback ? !approved || !environment.routes.has(target.port) || !matchesPreviewLoopback(target.hostname, approved.remoteHost) : isIP(target.hostname) && !isPublicPreviewAddress(target.hostname)) throw new Error('Preview destination is not approved');
    this.connection(environment.scope);
  }
  assertScope(environmentId: string, input: PreviewScope) {
    const scope = scopeCopy(input); const environment = this.environments.get(environmentId);
    if (!environment || environment.lifetime.signal.aborted || identityKey(environment.scope) !== identityKey(scope) || environment.scope.connectionGeneration !== scope.connectionGeneration || !sameDestinations(environment.scope.destinations, scope.destinations)) throw new Error('Preview scope changed; renewed approval is required');
    this.connection(scope); return environment.revision;
  }
  async expand(environmentId: string, input: PreviewScope) {
    const scope = scopeCopy(input);
    return await this.serial(async () => {
      const environment = this.environments.get(environmentId);
      if (!environment || identityKey(environment.scope) !== identityKey(scope) || environment.scope.connectionGeneration !== scope.connectionGeneration) throw new Error('Preview environment changed');
      const connection = this.connection(scope);
      for (const old of environment.scope.destinations) if (!scope.destinations.some(item => item.port === old.port && item.remoteHost === old.remoteHost)) throw new Error('Port expansion cannot replace an existing destination');
      if (sameDestinations(environment.scope.destinations, scope.destinations)) return environment.revision;
      // Invalidate first. No other controller can inherit a widened session-wide policy.
      this.options.invalidateAttachments(environmentId, 'Preview destination scope changed');
      const revision = environment.revision;
      const added = new Map<number, SSHPreviewRoute>();
      try {
        for (const destination of scope.destinations) if (!environment.routes.has(destination.port))
          added.set(destination.port, await connection.acquirePreviewRoute({ ...destination, expectedGeneration: scope.connectionGeneration }, environment.lifetime.signal));
        this.connection(scope); environment.lifetime.signal.throwIfAborted();
        if (environment.revision !== revision) throw new Error('Preview destinations changed during approval');
        for (const destination of scope.destinations) {
          const route = added.get(destination.port); if (!route) continue;
          environment.proxy.addRoute({ ...destination, open: signal => route.open(signal) }); environment.routes.set(destination.port, route);
        }
        environment.scope = scope; return ++environment.revision;
      } catch (error) {
        for (const [port, route] of added) { environment.proxy.revoke(port); environment.routes.delete(port); await route.close().catch(() => {}); }
        throw error;
      }
    });
  }
  async revoke(environmentId: string, port: number) {
    const environment = this.environments.get(environmentId); if (!environment) return;
    environment.proxy.revoke(port); const route = environment.routes.get(port); environment.routes.delete(port);
    environment.scope.destinations = environment.scope.destinations.filter(item => item.port !== port); environment.revision++;
    try { this.options.invalidateAttachments(environmentId, 'Preview destination revoked'); }
    finally {
      try { await route?.close(); }
      finally { if (!environment.routes.size) await this.removeEnvironment(environmentId, 'All preview destinations revoked'); }
    }
  }
  private async removeEnvironment(id: string, reason: string) {
    const environment = this.environments.get(id); if (!environment) return;
    this.environments.delete(id); environment.lifetime.abort(); environment.contents.clear();
    environment.connectionSignal.removeEventListener('abort', environment.disconnect);
    const reservation = this.reservations.get(id); if (reservation) { clearTimeout(reservation.timer); this.reservations.delete(id); }
    try { this.options.invalidateAttachments(id, reason); this.options.onDisconnected?.(id); }
    finally { await Promise.allSettled([environment.proxy.close(), ...[...environment.routes.values()].map(route => route.close())]); }
  }
  async forget(environmentId: string) {
    if (!/^[a-f0-9]{48}$/.test(environmentId)) throw new Error('Invalid preview environment');
    return await this.serial(async () => {
      await this.removeEnvironment(environmentId, 'Preview profile removed');
      const profiles = await this.loadProfiles(); await this.persist(profiles.filter(profile => profile.id !== environmentId));
      // Native must clear the corresponding persistent session site data after confirmation.
      return `persist:whip-preview-${environmentId}`;
    });
  }
  async close() {
    this.closed = true; this.lifetime.abort();
    for (const reservation of this.reservations.values()) clearTimeout(reservation.timer); this.reservations.clear();
    await Promise.all([...this.environments.keys()].map(id => this.removeEnvironment(id, 'Preview window closed')));
    await this.queue;
    await Promise.all([...this.environments.keys()].map(id => this.removeEnvironment(id, 'Preview window closed')));
  }
}
