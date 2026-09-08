import type { SessionCatalogPage } from '@whip/protocol';
import type { DeepReadonly } from '@whip/sdk/state';

export const sidebarStorageKey = 'whip.web.sidebar.v1';
export const defaultSidebarWidth = 320;
export interface SidebarState {
  width: number;
  hidden: boolean;
  hosts: { runtimeId: string; collapsed: string[] }[];
}
export const emptySidebarState = (): SidebarState => ({ width: defaultSidebarWidth, hidden: false, hosts: [] });
export function sidebarMaxWidth(viewport: number) { return Math.max(256, Math.min(420, viewport - 480)); }
export function sidebarWidth(width: number, viewport: number) { return Math.max(256, Math.min(sidebarMaxWidth(viewport), width)); }

/** Layout metadata only. Never persist the catalog or canonicalize host paths. */
export function readSidebarState(raw: string | null): SidebarState {
  if (!raw || new TextEncoder().encode(raw).length > 64 << 10) return emptySidebarState();
  try {
    const value = JSON.parse(raw);
    if (value.version !== 1 || !Number.isFinite(value.width) || typeof value.hidden !== 'boolean' || !Array.isArray(value.hosts)) return emptySidebarState();
    const hosts: SidebarState['hosts'] = [];
    for (const host of value.hosts.slice(-4)) {
      if (typeof host?.runtimeId !== 'string' || host.runtimeId.length > 256 || !Array.isArray(host.collapsed)) continue;
      if (hosts.some(item => item.runtimeId === host.runtimeId)) continue;
      hosts.push({ runtimeId: host.runtimeId, collapsed: [...new Set<string>(host.collapsed.filter((path: unknown): path is string => typeof path === 'string' && path.length <= 4096))].slice(-64) });
    }
    return { width: Math.max(256, Math.min(420, value.width)), hidden: value.hidden, hosts };
  } catch { return emptySidebarState(); }
}
export function setDirectoryCollapsed(state: SidebarState, runtimeId: string, cwd: string, collapsed: boolean): SidebarState {
  const previous = state.hosts.find(host => host.runtimeId === runtimeId)?.collapsed ?? [];
  if (previous.includes(cwd) === collapsed) return state;
  const paths = previous.filter(path => path !== cwd);
  if (collapsed) paths.push(cwd);
  const hosts = [...state.hosts.filter(host => host.runtimeId !== runtimeId), { runtimeId, collapsed: paths.slice(-64) }].slice(-4);
  // Long directory names also need a byte bound. Evict the oldest preference,
  // which simply makes that group expanded again; sessions are never evicted here.
  while (new TextEncoder().encode(JSON.stringify(hosts)).length > 60 << 10) {
    const first = { ...hosts[0]!, collapsed: hosts[0]!.collapsed.slice(1) };
    hosts[0] = first;
    if (!first.collapsed.length) hosts.shift();
  }
  return { ...state, hosts };
}

type Session = NonNullable<DeepReadonly<SessionCatalogPage>['items']>[number];
export type SidebarRow = { key: string; kind: 'directory'; cwd: string; label: string } | { key: string; kind: 'session'; session: Session };
const directoryName = (path: string) => path.split(/[\\/]/).filter(Boolean).at(-1) || path || 'Other sessions';
export function sidebarRows(items: readonly Session[], collapsed: readonly string[] = []): SidebarRow[] {
  const groups = new Map<string, Session[]>();
  for (const session of items) {
    const group = groups.get(session.cwd);
    if (group) group.push(session); else groups.set(session.cwd, [session]);
  }
  const names = new Map<string, string[]>();
  for (const cwd of groups.keys()) {
    const name = directoryName(cwd), paths = names.get(name);
    if (paths) paths.push(cwd); else names.set(name, [cwd]);
  }
  const labels = new Map<string, string>();
  for (const [name, paths] of names) {
    if (paths.length === 1) { labels.set(paths[0]!, name); continue; }
    const parents = paths.map(path => path.split(/[\\/]/).filter(Boolean).slice(0, -1));
    const depthLimit = Math.max(...parents.map(parts => parts.length));
    for (let depth = 1; depth <= depthLimit; depth++) {
      const suffixes = parents.map(parts => parts.slice(-depth).join('/'));
      const counts = new Map<string, number>();
      for (const suffix of suffixes) counts.set(suffix, (counts.get(suffix) ?? 0) + 1);
      paths.forEach((path, index) => { if (!labels.has(path) && counts.get(suffixes[index]!) === 1) labels.set(path, `${name} · ${suffixes[index] || path}`); });
      if (paths.every(path => labels.has(path))) break;
    }
    for (const path of paths) if (!labels.has(path)) labels.set(path, path ? `${name} · ${path}` : 'Other sessions');
  }
  const hidden = new Set(collapsed);
  const rows: SidebarRow[] = [];
  for (const [cwd, sessions] of groups) {
    rows.push({ key: `directory:${cwd}`, kind: 'directory', cwd, label: labels.get(cwd)! });
    if (!hidden.has(cwd)) for (const session of sessions) rows.push({ key: `session:${session.id}`, kind: 'session', session });
  }
  return rows;
}

export function newSessionSearch(search: Record<string, unknown>): { cwd?: string; runtimeId?: string } {
  return typeof search.cwd === 'string' && search.cwd.length > 0 && search.cwd.length <= 4096 &&
    typeof search.runtimeId === 'string' && search.runtimeId.length > 0 && search.runtimeId.length <= 256
    ? { cwd: search.cwd, runtimeId: search.runtimeId } : {};
}
