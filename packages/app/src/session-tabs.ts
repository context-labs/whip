import { isInspectorSection, type InspectorSection } from './navigation';
import type { AppStorage } from './platform';

export interface SessionLocation { agent?: string; panel?: InspectorSection }
export interface SessionTab {
  readonly rootId: string;
  readonly titleHint: string;
  readonly location: Readonly<SessionLocation>;
}
export interface TabWorkspace {
  readonly runtimeId: string;
  readonly tabs: readonly SessionTab[];
  readonly closed: readonly { tab: SessionTab; index: number }[];
  readonly lastActiveRootId?: string;
}
export interface TabsSnapshot { readonly version: 1; readonly workspaces: readonly TabWorkspace[] }
export const MAX_SESSION_TABS = 32;
export const TAB_STORAGE_KEY = 'whip.web.tabs.v1';
const MAX_BYTES = 64 * 1024;
const bytes = (text: string) => new TextEncoder().encode(text).byteLength;
const identity = (value: unknown): value is string => typeof value === 'string' && value.length > 0 && value.length <= 256 && !/[\u0000-\u001f]/.test(value);
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const EMPTY_TABS: readonly SessionTab[] = Object.freeze([]);
const EMPTY_CLOSED: TabWorkspace['closed'] = Object.freeze([]);
const title = (value: unknown) => typeof value === 'string' ? [...value].slice(0, 128).join('') : '';
const location = (value: unknown): Readonly<SessionLocation> => Object.freeze(object(value) ? {
  ...(identity(value.agent) ? { agent: value.agent } : {}),
  ...(isInspectorSection(value.panel) ? { panel: value.panel } : {}),
} : {});
function tab(value: unknown): SessionTab | undefined {
  if (!object(value) || !identity(value.rootId)) return;
  return Object.freeze({ rootId: value.rootId, titleHint: title(value.titleHint), location: location(value.location) });
}
function freeze(workspace: TabWorkspace): TabWorkspace {
  return Object.freeze({ ...workspace, tabs: Object.freeze([...workspace.tabs]), closed: Object.freeze(workspace.closed.map(item => Object.freeze(item))) });
}
function restore(raw: string | null): readonly TabWorkspace[] {
  if (!raw || raw.length > MAX_BYTES || bytes(raw) > MAX_BYTES) return [];
  let parsed: unknown;
  try { parsed = JSON.parse(raw); } catch { return []; }
  if (!object(parsed) || parsed.version !== 1 || !Array.isArray(parsed.workspaces)) return [];
  const result: TabWorkspace[] = [];
  for (const entry of parsed.workspaces.slice(-4)) {
    if (!object(entry) || !identity(entry.runtimeId) || result.some(item => item.runtimeId === entry.runtimeId)) continue;
    const tabs: SessionTab[] = [];
    for (const value of Array.isArray(entry.tabs) ? entry.tabs.slice(0, MAX_SESSION_TABS) : []) {
      const item = tab(value);
      if (item && !tabs.some(existing => existing.rootId === item.rootId)) tabs.push(item);
    }
    const closed: { tab: SessionTab; index: number }[] = [];
    for (const value of Array.isArray(entry.closed) ? entry.closed.slice(-20) : []) {
      if (!object(value)) continue;
      const item = tab(value.tab);
      if (!item || tabs.some(existing => existing.rootId === item.rootId) || closed.some(existing => existing.tab.rootId === item.rootId)) continue;
      closed.push({ tab: item, index: typeof value.index === 'number' && Number.isInteger(value.index) ? Math.max(0, Math.min(MAX_SESSION_TABS, value.index)) : tabs.length });
    }
    result.push(freeze({ runtimeId: entry.runtimeId, tabs, closed, ...(tabs.some(item => item.rootId === entry.lastActiveRootId) ? { lastActiveRootId: entry.lastActiveRootId as string } : {}) }));
  }
  return result;
}

/** Window-local navigation metadata. It has no daemon or execution dependencies. */
export class SessionTabs {
  private snapshot: TabsSnapshot;
  private listeners = new Set<() => void>();
  constructor(private readonly storage?: AppStorage, private readonly onNotice: (message: string) => void = () => {}) {
    let raw: string | null = null;
    try { raw = storage?.getItem(TAB_STORAGE_KEY) ?? null; } catch { onNotice('Session tabs could not be restored. This window will keep its layout in memory.'); }
    this.snapshot = Object.freeze({ version: 1, workspaces: Object.freeze(restore(raw)) });
  }
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  workspace(runtimeId: string): TabWorkspace {
    return this.snapshot.workspaces.find(item => item.runtimeId === runtimeId) ?? { runtimeId, tabs: EMPTY_TABS, closed: EMPTY_CLOSED };
  }
  private write(workspace: TabWorkspace) {
    if (!identity(workspace.runtimeId)) throw new Error('Invalid execution host identity');
    const previous = this.workspace(workspace.runtimeId);
    if (JSON.stringify(previous) === JSON.stringify(workspace)) return;
    let workspaces = [...this.snapshot.workspaces.filter(item => item.runtimeId !== workspace.runtimeId), freeze(workspace)].slice(-4);
    let next: TabsSnapshot = { version: 1, workspaces };
    if (bytes(JSON.stringify(next)) > MAX_BYTES) {
      workspaces = workspaces.map(item => freeze({ ...item, closed: [] }));
      while (workspaces.length > 1 && bytes(JSON.stringify({ version: 1, workspaces })) > MAX_BYTES) workspaces.shift();
      next = { version: 1, workspaces };
    }
    if (bytes(JSON.stringify(next)) > MAX_BYTES) throw new Error('This window’s tab layout is full. Close an open tab before adding another.');
    this.snapshot = Object.freeze({ ...next, workspaces: Object.freeze(workspaces) });
    try { this.storage?.setItem(TAB_STORAGE_KEY, JSON.stringify(this.snapshot)); }
    catch { this.onNotice('Session tabs are kept in memory because window storage is unavailable.'); }
    for (const listener of this.listeners) listener();
  }
  canOpen(runtimeId: string, rootId?: string) {
    const workspace = this.workspace(runtimeId);
    return workspace.tabs.length < MAX_SESSION_TABS || workspace.tabs.some(item => item.rootId === rootId);
  }
  open(runtimeId: string, rootId: string, titleHint = '') {
    if (!identity(rootId)) throw new Error('Invalid session identity');
    const workspace = this.workspace(runtimeId);
    if (workspace.tabs.some(item => item.rootId === rootId)) return;
    if (!this.canOpen(runtimeId)) throw new Error('There are 32 open session tabs. Close a tab before opening another.');
    this.write({ ...workspace, tabs: [...workspace.tabs, Object.freeze({ rootId, titleHint: title(titleHint), location: location({}) })], closed: workspace.closed.filter(item => item.tab.rootId !== rootId) });
  }
  visit(runtimeId: string, rootId: string, search: SessionLocation) {
    this.open(runtimeId, rootId);
    const workspace = this.workspace(runtimeId);
    this.write({ ...workspace, lastActiveRootId: rootId, tabs: workspace.tabs.map(item => item.rootId === rootId ? Object.freeze({ ...item, location: location(search) }) : item) });
  }
  home(runtimeId: string) {
    const workspace = this.workspace(runtimeId);
    if (workspace.lastActiveRootId) this.write({ ...workspace, lastActiveRootId: undefined });
  }
  titles(runtimeId: string, titles: ReadonlyMap<string, string>) {
    const workspace = this.workspace(runtimeId);
    this.write({ ...workspace, tabs: workspace.tabs.map(item => titles.has(item.rootId) ? Object.freeze({ ...item, titleHint: title(titles.get(item.rootId)) }) : item) });
  }
  close(runtimeId: string, rootIds: readonly string[], activeRootId?: string): string | null | undefined {
    const workspace = this.workspace(runtimeId);
    const removed = workspace.tabs.flatMap((tab, index) => rootIds.includes(tab.rootId) ? [{ tab, index }] : []);
    if (!removed.length) return;
    const tabs = workspace.tabs.filter(item => !rootIds.includes(item.rootId));
    const activeIndex = workspace.tabs.findIndex(item => item.rootId === activeRootId);
    const changed = !!activeRootId && rootIds.includes(activeRootId);
    const next = changed ? (workspace.tabs.slice(activeIndex + 1).find(item => !rootIds.includes(item.rootId)) ?? tabs.at(-1))?.rootId ?? null : undefined;
    this.write({ ...workspace, tabs, closed: [...workspace.closed.filter(item => !rootIds.includes(item.tab.rootId)), ...removed].slice(-20), lastActiveRootId: changed ? next ?? undefined : workspace.lastActiveRootId && rootIds.includes(workspace.lastActiveRootId) ? undefined : workspace.lastActiveRootId });
    return next;
  }
  reopen(runtimeId: string): string | undefined {
    const workspace = this.workspace(runtimeId), closed = workspace.closed.at(-1);
    if (!closed) return;
    if (!this.canOpen(runtimeId, closed.tab.rootId)) throw new Error('There are 32 open session tabs. Close a tab before reopening another.');
    const tabs = [...workspace.tabs];
    if (!tabs.some(item => item.rootId === closed.tab.rootId)) tabs.splice(Math.min(closed.index, tabs.length), 0, closed.tab);
    this.write({ ...workspace, tabs, closed: workspace.closed.slice(0, -1) });
    return closed.tab.rootId;
  }
  reorder(runtimeId: string, order: readonly string[]) {
    const workspace = this.workspace(runtimeId);
    if (order.length !== workspace.tabs.length || new Set(order).size !== order.length || order.some(id => !workspace.tabs.some(item => item.rootId === id))) throw new Error('Invalid session tab order');
    this.write({ ...workspace, tabs: order.map(id => workspace.tabs.find(item => item.rootId === id)!) });
  }
  move(runtimeId: string, rootId: string, offset: -1 | 1) {
    const order = this.workspace(runtimeId).tabs.map(item => item.rootId), from = order.indexOf(rootId), to = from + offset;
    if (from < 0 || to < 0 || to >= order.length) return;
    [order[from], order[to]] = [order[to]!, order[from]!];
    this.reorder(runtimeId, order);
  }
  dispose() { this.listeners.clear(); }
}
