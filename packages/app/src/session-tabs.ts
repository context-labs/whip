import { isInspectorSection, type InspectorSection } from './navigation';
import type { AppStorage } from './platform';

export interface SessionLocation { agent?: string; panel?: InspectorSection }
export interface SessionSearch extends SessionLocation { view?: 'repl' }
export interface SessionTab {
  readonly id: string;
  readonly kind: 'chat' | 'repl';
  readonly runtimeId: string;
  readonly rootId: string;
  readonly titleHint: string;
  readonly location: Readonly<SessionLocation>;
}
export interface SessionPane {
  readonly type: 'pane';
  readonly id: string;
  readonly tabs: readonly SessionTab[];
  readonly selected?: string;
}
export interface SessionSplit {
  readonly type: 'split';
  readonly id: string;
  readonly direction: 'horizontal' | 'vertical';
  readonly ratio: number;
  readonly first: SessionLayout;
  readonly second: SessionLayout;
}
export type SessionLayout = SessionPane | SessionSplit;
export type SplitEdge = 'left' | 'right' | 'top' | 'bottom';
export interface TabWorkspace {
  readonly layout: SessionLayout;
  readonly focusedPaneId: string;
  /** Derived catalog for labels and summaries; the pane tree owns ordering. */
  readonly tabs: readonly SessionTab[];
  readonly closed: readonly { tab: SessionTab; index: number; paneId: string }[];
  readonly restoreSelection: boolean;
}
export interface PreviousWorkspace { readonly runtimeId: string; readonly workspace: TabWorkspace }
export interface TabsSnapshot { readonly version: 3; readonly workspace: TabWorkspace; readonly previous: readonly PreviousWorkspace[] }
export const MAX_SESSION_TABS = 32;
export const MAX_SESSION_PANES = 4;
export const TAB_STORAGE_KEY = 'whip.web.workspace.v3';
export const PREVIOUS_TAB_STORAGE_KEY = 'whip.web.workspace.v2';
export const LEGACY_TAB_STORAGE_KEY = 'whip.web.tabs.v1';
const MAX_BYTES = 64 * 1024;
const bytes = (text: string) => new TextEncoder().encode(text).byteLength;
const identity = (value: unknown): value is string => typeof value === 'string' && value.length > 0 && value.length <= 256 && !/[\u0000-\u001f]/.test(value);
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const title = (value: unknown) => typeof value === 'string' ? [...value].slice(0, 128).join('') : '';
const location = (value: unknown): Readonly<SessionLocation> => Object.freeze(object(value) ? {
  ...(identity(value.agent) ? { agent: value.agent } : {}),
  ...(isInspectorSection(value.panel) ? { panel: value.panel } : {}),
} : {});
export function validateSessionSearch(search: Record<string, unknown>): SessionSearch {
  return { ...location(search), ...(search.view === 'repl' ? { view: 'repl' } : {}) };
}
/** Mode has one persisted owner; route search is derived from the descriptor. */
export function sessionSearch(tab?: Pick<SessionTab, 'kind' | 'location'>): SessionSearch {
  return { ...tab?.location, ...(tab?.kind === 'repl' ? { view: 'repl' } : {}) };
}
const newId = () => crypto.randomUUID();
export function sessionPanes(node: SessionLayout): readonly SessionPane[] {
  return node.type === 'pane' ? [node] : [...sessionPanes(node.first), ...sessionPanes(node.second)];
}
export function selectedSessionTab(workspace: TabWorkspace): SessionTab | undefined {
  const pane = sessionPanes(workspace.layout).find(item => item.id === workspace.focusedPaneId);
  return pane?.tabs.find(item => item.id === pane.selected);
}
export function sessionViewPane(workspace: TabWorkspace, viewId: string) {
  return sessionPanes(workspace.layout).find(pane => pane.tabs.some(tab => tab.id === viewId));
}
function mapPanes(node: SessionLayout, update: (pane: SessionPane) => SessionPane): SessionLayout {
  if (node.type === 'pane') return update(node);
  return { ...node, first: mapPanes(node.first, update), second: mapPanes(node.second, update) };
}
function prune(node: SessionLayout): SessionLayout | undefined {
  if (node.type === 'pane') return node.tabs.length ? node : undefined;
  const first = prune(node.first), second = prune(node.second);
  return first && second ? { ...node, first, second } : first ?? second;
}
function replaceNode(node: SessionLayout, id: string, update: (node: SessionLayout) => SessionLayout): SessionLayout {
  if (node.id === id) return update(node);
  return node.type === 'pane' ? node : { ...node, first: replaceNode(node.first, id, update), second: replaceNode(node.second, id, update) };
}
function freezeNode(node: SessionLayout): SessionLayout {
  if (node.type === 'split') return Object.freeze({ ...node, first: freezeNode(node.first), second: freezeNode(node.second) });
  const tabs = Object.freeze(node.tabs.map(tab => Object.freeze({ ...tab, location: location(tab.location) })));
  return Object.freeze({ ...node, tabs, selected: tabs.some(tab => tab.id === node.selected) ? node.selected : tabs[0]?.id });
}
function emptyWorkspace(): TabWorkspace {
  return freeze({ restoreSelection: false, layout: { type: 'pane', id: 'main', tabs: [] }, focusedPaneId: 'main', closed: [] });
}
function freeze(workspace: Omit<TabWorkspace, 'tabs'>): TabWorkspace {
  const layout = freezeNode(workspace.layout);
  const panes = sessionPanes(layout);
  return Object.freeze({ ...workspace, layout, focusedPaneId: panes.some(p => p.id === workspace.focusedPaneId) ? workspace.focusedPaneId : panes[0]!.id,
    tabs: Object.freeze(panes.flatMap(p => p.tabs)), closed: Object.freeze(workspace.closed.map(item => Object.freeze({ ...item, tab: Object.freeze({ ...item.tab, location: location(item.tab.location) }) }))) });
}
function parseTab(value: unknown, runtimeId?: string, legacy = false): SessionTab | undefined {
  if (!object(value) || !identity(value.rootId) || (!legacy && !identity(value.id))) return;
  const kind = legacy && value.kind === undefined ? 'chat' : value.kind;
  if (kind !== 'chat' && kind !== 'repl') return;
  runtimeId ??= identity(value.runtimeId) ? value.runtimeId : undefined;
  if (!runtimeId) return;
  return { id: legacy ? value.rootId : value.id as string, kind, runtimeId, rootId: value.rootId, titleHint: title(value.titleHint), location: location(value.location) };
}
function restore(raw: string | null): readonly PreviousWorkspace[] {
  if (!raw || raw.length > MAX_BYTES || bytes(raw) > MAX_BYTES) return [];
  let parsed: unknown;
  try { parsed = JSON.parse(raw); } catch { return []; }
  if (!object(parsed) || ![1, 2, 3].includes(parsed.version as number)) return [];
  const entries = parsed.version === 3 ? [parsed.workspace] : Array.isArray(parsed.workspaces) ? parsed.workspaces.slice(-4) : [];
  const result: PreviousWorkspace[] = [];
  for (const entry of entries) {
    if (!object(entry)) continue;
    const runtimeId = parsed.version === 3 ? undefined : identity(entry.runtimeId) ? entry.runtimeId : undefined;
    if (parsed.version !== 3 && (!runtimeId || result.some(item => item.runtimeId === runtimeId))) continue;
    const ids = new Set<string>(), nodes = new Set<string>();
    let paneCount = 0;
    const readTabs = (values: unknown, legacy = false) => {
      const tabs: SessionTab[] = [];
      for (const value of Array.isArray(values) ? values.slice(0, MAX_SESSION_TABS) : []) {
        const tab = parseTab(value, runtimeId, legacy);
        if (tab && !ids.has(tab.id) && ids.size < MAX_SESSION_TABS) { ids.add(tab.id); tabs.push(tab); }
      }
      return tabs;
    };
    const readNode = (value: unknown, depth: number): SessionLayout | undefined => {
      if (!object(value) || !identity(value.id) || nodes.has(value.id) || depth > 3) return;
      nodes.add(value.id);
      if (value.type === 'pane') {
        if (++paneCount > MAX_SESSION_PANES) return;
        return { type: 'pane', id: value.id, tabs: readTabs(value.tabs), selected: identity(value.selected) ? value.selected : undefined };
      }
      if (value.type !== 'split' || !['horizontal', 'vertical'].includes(value.direction as string) || typeof value.ratio !== 'number' || !Number.isFinite(value.ratio) || value.ratio <= 0 || value.ratio >= 1) return;
      const first = readNode(value.first, depth + 1), second = readNode(value.second, depth + 1);
      if (!first || !second) return;
      return { type: 'split', id: value.id, direction: value.direction as SessionSplit['direction'], ratio: value.ratio, first, second };
    };
    const legacy = parsed.version === 1;
    let layout: SessionLayout | undefined = legacy ? { type: 'pane', id: 'main', tabs: readTabs(entry.tabs, true), selected: identity(entry.lastActiveRootId) ? entry.lastActiveRootId : undefined } : readNode(entry.layout, 0);
    if (!layout) continue;
    layout = prune(layout) ?? { type: 'pane', id: 'main', tabs: [] };
    const closed: TabWorkspace['closed'][number][] = [];
    const closedIds = new Set<string>();
    for (const value of Array.isArray(entry.closed) ? entry.closed.slice(-20) : []) {
      if (!object(value)) continue;
      const tab = parseTab(value.tab, runtimeId, legacy);
      if (!tab || ids.has(tab.id) || closedIds.has(tab.id)) continue;
      closedIds.add(tab.id);
      closed.push({ tab, index: typeof value.index === 'number' && Number.isInteger(value.index) ? Math.max(0, Math.min(MAX_SESSION_TABS, value.index)) : 0, paneId: !legacy && identity(value.paneId) ? value.paneId : 'main' });
    }
    const workspace = freeze({ layout, focusedPaneId: identity(entry.focusedPaneId) ? entry.focusedPaneId : 'main', closed, restoreSelection: parsed.version === 3 ? entry.restoreSelection === true : identity(entry.lastActiveRootId) });
    result.push({ runtimeId: runtimeId ?? '', workspace });
  }
  return result;
}
function serialize(workspace: TabWorkspace, migrated: readonly string[]) {
  const { tabs: _derived, ...saved } = workspace;
  return JSON.stringify({ version: 3, workspace: saved, migrated });
}

/** One immutable owner for window-local view identities, panes and tab order. */
export class SessionTabs {
  private snapshot: TabsSnapshot;
  private migrated: string[] = [];
  private listeners = new Set<() => void>();
  constructor(private readonly storage?: AppStorage, private readonly onNotice: (message: string) => void = () => {}, preferredRuntimeId?: string) {
    let raw: string | null = null, old: string | null = null;
    try {
      raw = storage?.getItem(TAB_STORAGE_KEY) ?? null;
      old = storage?.getItem(PREVIOUS_TAB_STORAGE_KEY) ?? storage?.getItem(LEGACY_TAB_STORAGE_KEY) ?? null;
    } catch { onNotice('Session tabs could not be restored. This window will keep its layout in memory.'); }
    const previous = restore(old);
    const restored = restore(raw)[0]?.workspace;
    if (restored && raw) {
      const saved: unknown = JSON.parse(raw);
      if (object(saved) && Array.isArray(saved.migrated)) this.migrated = saved.migrated.filter(identity).slice(0, 4);
    }
    let initial = !restored ? previous.find(item => item.runtimeId === preferredRuntimeId) ?? previous.at(-1) : undefined;
    if (initial && bytes(serialize(initial.workspace, [initial.runtimeId])) > MAX_BYTES) {
      onNotice('The previous layout exceeds this window’s metadata limit. Its tabs remain available individually in Execution hosts.');
      initial = undefined;
    }
    if (initial) this.migrated.push(initial.runtimeId);
    this.snapshot = Object.freeze({ version: 3, workspace: restored ?? initial?.workspace ?? emptyWorkspace(), previous: Object.freeze(previous.filter(item => !this.migrated.includes(item.runtimeId))) });
    // Keep original v1/v2 storage intact until the user restores or dismisses its
    // other layouts. They must never be evicted to fit the new window limits.
    if (initial) this.persist();
  }
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  workspace(): TabWorkspace { return this.snapshot.workspace; }
  private persist() {
    try { this.storage?.setItem(TAB_STORAGE_KEY, serialize(this.snapshot.workspace, this.migrated)); }
    catch { this.onNotice('Session tabs are kept in memory because window storage is unavailable.'); }
  }
  private write(workspace: Omit<TabWorkspace, 'tabs'>, previous = this.snapshot.previous, migrated = this.migrated) {
    let next = freeze(workspace);
    if (bytes(serialize(next, migrated)) > MAX_BYTES) next = freeze({ ...next, closed: [] });
    if (bytes(serialize(next, migrated)) > MAX_BYTES) throw new Error('This window’s tab layout is full. Close an open tab before adding another.');
    if (JSON.stringify(this.workspace()) === JSON.stringify(next) && previous === this.snapshot.previous) return;
    this.migrated = migrated;
    this.snapshot = Object.freeze({ version: 3, workspace: next, previous: Object.freeze(previous) });
    this.persist();
    for (const listener of this.listeners) listener();
  }
  restorePrevious(runtimeId: string) {
    const entry = this.snapshot.previous.find(item => item.runtimeId === runtimeId);
    if (!entry) return;
    const workspace = this.workspace(), previous = entry.workspace;
    if (workspace.tabs.length + previous.tabs.length > MAX_SESSION_TABS) throw new Error('Close some tabs before restoring this layout; the window supports 32 open tabs.');
    if (workspace.tabs.length && sessionPanes(workspace.layout).length + sessionPanes(previous.layout).length > MAX_SESSION_PANES) throw new Error('Close some panes before restoring this layout; the window supports four panes.');
    // Reassign view and pane IDs when merging independent legacy identity spaces.
    const viewIds = new Map(previous.tabs.map(tab => [tab.id, newId()]));
    const nodeIds = new Map<string, string>();
    const remap = (node: SessionLayout): SessionLayout => {
      const id = newId(); nodeIds.set(node.id, id);
      return node.type === 'split' ? { ...node, id, first: remap(node.first), second: remap(node.second) } : { ...node, id, tabs: node.tabs.map(tab => ({ ...tab, id: viewIds.get(tab.id)! })), selected: node.selected ? viewIds.get(node.selected) : undefined };
    };
    const restored = remap(previous.layout);
    const closed = previous.closed.map(item => ({ ...item, tab: { ...item.tab, id: newId() }, paneId: nodeIds.get(item.paneId) ?? workspace.focusedPaneId }));
    if (workspace.closed.length + closed.length > 20) throw new Error('Reopen some closed tabs or open previous tabs individually before restoring this layout; the window keeps 20 closed tabs.');
    const layout: SessionLayout = workspace.tabs.length ? { type: 'split', id: newId(), direction: 'horizontal', ratio: .5, first: workspace.layout, second: restored } : restored;
    const combined = freeze({ ...workspace, layout, focusedPaneId: workspace.tabs.length ? workspace.focusedPaneId : nodeIds.get(previous.focusedPaneId)!, closed: [...workspace.closed, ...closed] });
    if (bytes(serialize(combined, [...this.migrated, runtimeId])) > MAX_BYTES) throw new Error('This layout exceeds the window’s metadata limit. Open its previous tabs individually.');
    this.write(combined, this.snapshot.previous.filter(item => item !== entry), [...this.migrated, runtimeId]);
  }
  openPrevious(runtimeId: string, viewId: string) {
    const previous = this.snapshot.previous.find(item => item.runtimeId === runtimeId)?.workspace;
    const tab = [...(previous?.tabs ?? []), ...(previous?.closed.map(item => item.tab) ?? [])].find(tab => tab.id === viewId);
    if (!tab) return;
    return this.add({ ...tab, id: newId() });
  }
  dismissPrevious(runtimeId: string) {
    if (!this.snapshot.previous.some(item => item.runtimeId === runtimeId)) return;
    this.write(this.workspace(), this.snapshot.previous.filter(item => item.runtimeId !== runtimeId), [...this.migrated, runtimeId]);
  }
  canOpen(runtimeId?: string, rootId?: string) {
    const workspace = this.workspace();
    return workspace.tabs.length < MAX_SESSION_TABS || workspace.tabs.some(item => item.runtimeId === runtimeId && item.rootId === rootId);
  }
  preferred(runtimeId: string, rootId: string): SessionTab | undefined {
    const workspace = this.workspace();
    const selected = selectedSessionTab(workspace);
    const matches = (tab: SessionTab) => tab.runtimeId === runtimeId && tab.rootId === rootId;
    return (selected && matches(selected) ? selected : undefined) ?? sessionPanes(workspace.layout).find(p => p.id === workspace.focusedPaneId)?.tabs.find(matches) ?? workspace.tabs.find(matches);
  }
  open(runtimeId: string, rootId: string, titleHint = '', paneId?: string): string {
    if (!identity(runtimeId)) throw new Error('Invalid execution host identity');
    if (!identity(rootId)) throw new Error('Invalid session identity');
    const existing = this.preferred(runtimeId, rootId);
    if (existing) return existing.id;
    return this.add({ id: rootId, kind: 'chat', runtimeId, rootId, titleHint: title(titleHint), location: location({}) }, paneId);
  }
  private add(tab: SessionTab, paneId?: string): string {
    if (!this.canOpen()) throw new Error('There are 32 open session tabs. Close a tab before opening another.');
    const workspace = this.workspace();
    const target = sessionPanes(workspace.layout).find(p => p.id === (paneId ?? workspace.focusedPaneId)) ?? sessionPanes(workspace.layout)[0]!;
    // Reopened roots may share an old primary ID with a different view in history.
    if (workspace.tabs.some(t => t.id === tab.id) || workspace.closed.some(({ tab: old }) => old.id === tab.id && (old.runtimeId !== tab.runtimeId || old.rootId !== tab.rootId))) tab = { ...tab, id: newId() };
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => p.id === target.id ? { ...p, tabs: [...p.tabs, tab] } : p), closed: workspace.closed.filter(item => item.tab.id !== tab.id) });
    return tab.id;
  }
  visit(runtimeId: string, rootId: string, search: SessionSearch, viewId?: string) {
    const id = this.workspace().tabs.find(t => t.id === viewId && t.runtimeId === runtimeId && t.rootId === rootId)?.id ?? this.open(runtimeId, rootId);
    const workspace = this.workspace();
    const pane = sessionViewPane(workspace, id)!;
    this.write({ ...workspace, focusedPaneId: pane.id, restoreSelection: true, layout: mapPanes(workspace.layout, p => p.id === pane.id ? { ...p, selected: id, tabs: p.tabs.map(t => t.id === id ? { ...t, kind: search.view === 'repl' ? 'repl' : 'chat', location: location(search) } : t) } : p) });
    return id;
  }
  updateLocation(viewId: string, search: SessionLocation) {
    const workspace = this.workspace();
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => ({ ...p, tabs: p.tabs.map(t => t.id === viewId ? { ...t, location: location(search) } : t) })) });
  }
  activate(viewId: string, focus = true) {
    const workspace = this.workspace(), pane = sessionViewPane(workspace, viewId);
    if (!pane) return;
    this.write({ ...workspace, focusedPaneId: focus ? pane.id : workspace.focusedPaneId,
      restoreSelection: focus || workspace.restoreSelection,
      layout: mapPanes(workspace.layout, p => p.id === pane.id ? { ...p, selected: viewId } : p) });
  }
  home() {
    const workspace = this.workspace();
    if (workspace.restoreSelection) this.write({ ...workspace, restoreSelection: false });
  }
  titles(runtimeId: string, titles: ReadonlyMap<string, string>) {
    const workspace = this.workspace();
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => ({ ...p, tabs: p.tabs.map(t => t.runtimeId === runtimeId && titles.has(t.rootId) ? { ...t, titleHint: title(titles.get(t.rootId)) } : t) })) });
  }
  closeViews(viewIds: readonly string[], activeViewId?: string): string | null | undefined {
    const workspace = this.workspace();
    const removed = sessionPanes(workspace.layout).flatMap(p => p.tabs.flatMap((tab, index) => viewIds.includes(tab.id) ? [{ tab, index, paneId: p.id }] : []));
    if (!removed.length) return;
    let layout = mapPanes(workspace.layout, p => {
      const tabs = p.tabs.filter(t => !viewIds.includes(t.id));
      const index = p.tabs.findIndex(t => t.id === p.selected);
      const selected = viewIds.includes(p.selected ?? '') ? (p.tabs.slice(index + 1).find(t => !viewIds.includes(t.id)) ?? tabs.at(-1))?.id : p.selected;
      return { ...p, tabs, selected };
    });
    layout = prune(layout) ?? { type: 'pane', id: workspace.focusedPaneId, tabs: [] };
    const next = freeze({ ...workspace, layout, closed: [...workspace.closed.filter(item => !viewIds.includes(item.tab.id)), ...removed].slice(-20) });
    const selected = selectedSessionTab(next);
    this.write({ ...next, restoreSelection: workspace.restoreSelection && !!selected });
    return activeViewId && viewIds.includes(activeViewId) ? selected?.id ?? null : undefined;
  }
  close(runtimeId: string, rootIds: readonly string[], activeRootId?: string): string | null | undefined {
    const workspace = this.workspace();
    const active = workspace.tabs.find(t => t.runtimeId === runtimeId && t.rootId === activeRootId);
    const next = this.closeViews(workspace.tabs.filter(t => t.runtimeId === runtimeId && rootIds.includes(t.rootId)).map(t => t.id), active?.id);
    return typeof next === 'string' ? this.workspace().tabs.find(t => t.id === next)?.rootId : next;
  }
  reopenView(): string | undefined {
    const workspace = this.workspace(), closed = workspace.closed.at(-1);
    if (!closed) return;
    if (!this.canOpen()) throw new Error('There are 32 open session tabs. Close a tab before reopening another.');
    const pane = sessionPanes(workspace.layout).find(p => p.id === closed.paneId) ?? sessionPanes(workspace.layout).find(p => p.id === workspace.focusedPaneId)!;
    const tabs = [...pane.tabs]; tabs.splice(Math.min(closed.index, tabs.length), 0, closed.tab);
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => p.id === pane.id ? { ...p, tabs } : p), closed: workspace.closed.slice(0, -1) });
    return closed.tab.id;
  }
  reorderPane(paneId: string, order: readonly string[]) {
    const workspace = this.workspace(), pane = sessionPanes(workspace.layout).find(p => p.id === paneId);
    if (!pane || order.length !== pane.tabs.length || new Set(order).size !== order.length || order.some(id => !pane.tabs.some(t => t.id === id))) throw new Error('Invalid session tab order');
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => p.id === paneId ? { ...p, tabs: order.map(id => p.tabs.find(t => t.id === id)!) } : p) });
  }
  move(viewId: string, offset: -1 | 1) {
    const workspace = this.workspace(), pane = sessionViewPane(workspace, viewId);
    if (!pane) return;
    const order = pane.tabs.map(t => t.id), from = order.indexOf(viewId), to = from + offset;
    if (to < 0 || to >= order.length) return;
    [order[from], order[to]] = [order[to]!, order[from]!];
    this.reorderPane(pane.id, order);
  }
  split(viewId: string, edge: SplitEdge): string {
    const workspace = this.workspace(), pane = sessionViewPane(workspace, viewId), source = workspace.tabs.find(t => t.id === viewId);
    if (!pane || !source) throw new Error('This view is no longer open');
    if (sessionPanes(workspace.layout).length >= MAX_SESSION_PANES) throw new Error('There are four panes. Move a tab to an existing pane or close a pane first.');
    if (!this.canOpen()) throw new Error('There are 32 open session tabs. Close a tab before splitting this view.');
    const duplicate = { ...source, id: newId() };
    const newPane: SessionPane = { type: 'pane', id: newId(), tabs: [duplicate], selected: duplicate.id };
    this.write({ ...workspace, layout: replaceNode(workspace.layout, pane.id, node => splitNode(node, newPane, edge)), focusedPaneId: newPane.id, restoreSelection: true });
    return duplicate.id;
  }
  transfer(viewId: string, paneId: string, edge?: SplitEdge, index?: number) {
    const workspace = this.workspace(), sourcePane = sessionViewPane(workspace, viewId), targetPane = sessionPanes(workspace.layout).find(p => p.id === paneId);
    const tab = sourcePane?.tabs.find(t => t.id === viewId);
    if (!sourcePane || !targetPane || !tab) return;
    if (edge && sourcePane.id === paneId && sourcePane.tabs.length === 1) return;
    if (edge && sessionPanes(workspace.layout).length + (sourcePane.tabs.length > 1 ? 1 : 0) > MAX_SESSION_PANES) throw new Error('There are four panes. Move this tab into an existing pane.');
    let layout = mapPanes(workspace.layout, p => {
      if (p.id !== sourcePane.id) return p;
      const tabs = p.tabs.filter(t => t.id !== viewId);
      return { ...p, tabs, selected: p.selected === viewId ? tabs[Math.min(p.tabs.findIndex(t => t.id === viewId), tabs.length - 1)]?.id : p.selected };
    });
    let focusedPaneId = paneId;
    if (edge) {
      const newPane: SessionPane = { type: 'pane', id: newId(), tabs: [tab], selected: tab.id };
      focusedPaneId = newPane.id;
      layout = replaceNode(layout, paneId, node => splitNode(node, newPane, edge));
    } else layout = mapPanes(layout, p => {
      if (p.id !== paneId) return p;
      const insertion = index === undefined ? p.tabs.length : index - (sourcePane.id === paneId && sourcePane.tabs.findIndex(t => t.id === viewId) < index ? 1 : 0);
      const tabs = [...p.tabs]; tabs.splice(Math.max(0, Math.min(tabs.length, insertion)), 0, tab);
      return { ...p, tabs, selected: tab.id };
    });
    this.write({ ...workspace, layout: prune(layout)!, focusedPaneId, restoreSelection: true });
  }
  resize(splitId: string, ratio: number) {
    if (!Number.isFinite(ratio)) return;
    const workspace = this.workspace();
    this.write({ ...workspace, layout: replaceNode(workspace.layout, splitId, n => n.type === 'split' ? { ...n, ratio: Math.max(.01, Math.min(.99, ratio)) } : n) });
  }
  dispose() { this.listeners.clear(); }
}
function splitNode(existing: SessionLayout, pane: SessionPane, edge: SplitEdge): SessionSplit {
  const before = edge === 'left' || edge === 'top';
  return { type: 'split', id: newId(), direction: edge === 'left' || edge === 'right' ? 'horizontal' : 'vertical', ratio: .5, first: before ? pane : existing, second: before ? existing : pane };
}
