import type { TransportFactory } from '@whip/sdk';
import type { AppStorage } from './platform';

export type ConnectionTarget =
  | { kind: 'url'; endpoint: string }
  | { kind: 'local' }
  | { kind: 'ssh'; host: string; user?: string; port?: number;
      identityFile?: string; remoteExecutable?: string; remoteHome?: string };

export interface ConnectionProfile {
  id: string;
  label: string;
  target: ConnectionTarget;
  runtimeId?: string;
}
export interface ResolvedConnection {
  endpoint: string | TransportFactory;
  dispose(): void;
}
export interface ConnectionOptions {
  signal: AbortSignal;
  onProgress(message: string): void;
}

export const hostsStorageKey = 'whip.hosts.v2';
export const selectedHostStorageKey = 'whip.selectedHost.v2';
export const localProfile: ConnectionProfile = Object.freeze({
  id: 'local', label: 'This Mac', target: Object.freeze({ kind: 'local' }),
});

function text(value: unknown, name: string, limit: number): string {
  if (typeof value !== 'string' || !value || value.length > limit || /[\u0000-\u001f\u007f]/.test(value))
    throw new Error(`Invalid ${name}`);
  return value;
}

export function urlProfile(endpoint: string): ConnectionProfile {
  text(endpoint, 'daemon address', 2048);
  const url = new URL(endpoint);
  if (!['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol) || url.username || url.password || url.hash)
    throw new Error('Enter an HTTP or WebSocket daemon endpoint without credentials or a fragment');
  return { id: `url:${url.href}`, label: url.origin, target: { kind: 'url', endpoint: url.href } };
}

/** Validate persisted and bridge-supplied profiles; retain only known fields. */
export function validateProfile(value: unknown): ConnectionProfile {
  if (!value || typeof value !== 'object') throw new Error('Invalid execution host');
  const profile = value as ConnectionProfile;
  const id = text(profile.id, 'host identity', 4096);
  const label = text(profile.label, 'host label', 256);
  const runtimeId = profile.runtimeId === undefined ? undefined : text(profile.runtimeId, 'runtime identity', 256);
  const target = profile.target;
  if (!target || typeof target !== 'object') throw new Error('Invalid connection method');
  if (target.kind === 'url') {
    const canonical = urlProfile(target.endpoint);
    return { ...canonical, label, ...(runtimeId ? { runtimeId } : {}) };
  }
  if (target.kind === 'local') return { ...localProfile, ...(runtimeId ? { runtimeId } : {}) };
  if (target.kind !== 'ssh') throw new Error('Unsupported connection method');
  const host = text(target.host, 'SSH host', 255);
  if (!/^[a-zA-Z0-9\[][a-zA-Z0-9_.:\[\]-]*$/.test(host)) throw new Error('Enter an SSH alias or hostname; use the separate username field');
  const ssh: Extract<ConnectionTarget, { kind: 'ssh' }> = { kind: 'ssh', host };
  if (target.user !== undefined) {
    ssh.user = text(target.user, 'SSH username', 128);
    if (!/^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$/.test(ssh.user)) throw new Error('Invalid SSH username');
  }
  if (target.port !== undefined) {
    if (!Number.isSafeInteger(target.port) || target.port < 1 || target.port > 65535) throw new Error('SSH port must be between 1 and 65535');
    ssh.port = target.port;
  }
  for (const key of ['identityFile', 'remoteExecutable', 'remoteHome'] as const)
    if (target[key] !== undefined) ssh[key] = text(target[key], key, 2048);
  if (!id.startsWith('ssh:')) throw new Error('Invalid SSH profile identity');
  return { id, label, target: ssh, ...(runtimeId ? { runtimeId } : {}) };
}

export async function resolveURLConnection(profile: ConnectionProfile, options: ConnectionOptions): Promise<ResolvedConnection> {
  options.signal.throwIfAborted();
  const validated = validateProfile(profile);
  if (validated.target.kind !== 'url') throw new Error('This connection requires the Whip desktop app');
  return { endpoint: validated.target.endpoint, dispose() {} };
}

export function saveConnections(storage: AppStorage, hosts: readonly ConnectionProfile[], selected: ConnectionProfile): ConnectionProfile[] {
  const profiles = [selected, ...hosts.filter(host => host.id !== selected.id)].slice(0, 16).map(validateProfile);
  // Write the referenced record first. A failed selection write leaves the old selection valid.
  storage.setItem(hostsStorageKey, JSON.stringify(profiles));
  storage.setItem(selectedHostStorageKey, JSON.stringify(selected.id));
  return profiles;
}

export function readConnections(storage: AppStorage, fallback: ConnectionProfile, kinds: readonly ConnectionTarget['kind'][]): {
  hosts: ConnectionProfile[]; selected: ConnectionProfile; notice?: string; needsSelection?: boolean;
} {
  const result: ReturnType<typeof readConnections> = { hosts: [], selected: fallback };
  try {
    const raw = storage.getItem(hostsStorageKey);
    if (raw !== null) {
      if (raw.length > 128 << 10) throw new Error('Saved execution hosts exceed the storage limit');
      const values: unknown = JSON.parse(raw);
      if (!Array.isArray(values) || values.length > 16) throw new Error('Invalid saved execution hosts');
      for (const value of values) {
        try {
          const profile = validateProfile(value);
          if (!result.hosts.some(host => host.id === profile.id)) result.hosts.push(profile);
        } catch { result.notice = 'Some saved execution hosts could not be read. Add those hosts again.'; }
      }
      const selected: unknown = JSON.parse(storage.getItem(selectedHostStorageKey) ?? 'null');
      const profile = result.hosts.find(host => host.id === selected);
      if (profile && kinds.includes(profile.target.kind)) result.selected = profile;
      else {
        result.needsSelection = true;
        result.notice = 'The saved connection is unavailable. Choose an execution host to continue.';
      }
      return result;
    }
    const oldHosts = storage.getItem('whip.web.hosts.v1');
    const oldSelected = storage.getItem('whip.web.endpoint');
    if (oldHosts === null && oldSelected === null) {
      if (storage.getItem(selectedHostStorageKey) !== null) {
        result.needsSelection = true;
        result.notice = 'The saved connection is unavailable. Choose an execution host to continue.';
      }
      return result;
    }
    if ((oldHosts?.length ?? 0) > 64 << 10 || (oldSelected?.length ?? 0) > 4096) throw new Error('Saved host addresses exceed the storage limit');
    const values: unknown = JSON.parse(oldHosts ?? '[]');
    const selected: unknown = JSON.parse(oldSelected ?? 'null');
    if (selected === null || !kinds.includes('url')) {
      result.needsSelection = true;
      result.notice = 'The saved connection is unavailable. Choose an execution host to continue.';
    }
    for (const endpoint of [selected, ...(Array.isArray(values) ? values.slice(0, 16) : [])]) {
      if (endpoint === null) continue;
      try {
        const profile = urlProfile(endpoint as string);
        if (!result.hosts.some(host => host.id === profile.id)) result.hosts.push(profile);
        if (endpoint === selected && kinds.includes('url')) result.selected = profile;
      } catch {
        if (endpoint === selected) result.needsSelection = true;
        result.notice = 'Some saved host addresses were invalid. Choose an execution host to continue.';
      }
    }
    if (!result.needsSelection) result.hosts = saveConnections(storage, result.hosts, result.selected);
  } catch {
    result.needsSelection = true;
    result.notice = 'Saved execution hosts could not be loaded or migrated. Your existing records have been preserved.';
  }
  return result;
}
