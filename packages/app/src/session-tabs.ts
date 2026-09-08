import { isInspectorSection, type InspectorSection } from './navigation';
import type { AppStorage } from './platform';

export interface SessionLocation { agent?: string; panel?: InspectorSection }
export interface SessionSearch extends SessionLocation { view?: 'repl' }
export interface SessionTab {
  readonly id: string;
  readonly kind: 'chat' | 'repl';
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
  readonly runtimeId: string;
  readonly layout: SessionLayout;
  readonly focusedPaneId: string;
  /** Derived catalog for labels and summaries; the pane tree owns ordering. */
  readonly tabs: readonly SessionTab[];
  readonly closed: readonly { tab: SessionTab; index: number; paneId: string }[];
  readonly lastActiveRootId?: string;
}
export interface TabsSnapshot { readonly version: 2; readonly workspaces: readonly TabWorkspace[] }
export const MAX_SESSION_TABS = 32;
export const MAX_SESSION_PANES = 4;
export const TAB_STORAGE_KEY = 'whip.web.workspace.v2';
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
function emptyWorkspace(runtimeId: string): TabWorkspace {
  return freeze({ runtimeId, layout: { type: 'pane', id: 'main', tabs: [] }, focusedPaneId: 'main', closed: [] });
}
function freeze(workspace: Omit<TabWorkspace, 'tabs'>): TabWorkspace {
  const layout = freezeNode(workspace.layout);
  const panes = sessionPanes(layout);
  return Object.freeze({ ...workspace, layout, focusedPaneId: panes.some(p => p.id === workspace.focusedPaneId) ? workspace.focusedPaneId : panes[0]!.id,
    tabs: Object.freeze(panes.flatMap(p => p.tabs)), closed: Object.freeze(workspace.closed.map(item => Object.freeze({ ...item, tab: Object.freeze({ ...item.tab, location: location(item.tab.location) }) }))) });
}
function parseTab(value: unknown, legacy = false): SessionTab | undefined {
  if (!object(value) || !identity(value.rootId) || (!legacy && !identity(value.id))) return;
  const kind = legacy && value.kind === undefined ? 'chat' : value.kind;
  if (kind !== 'chat' && kind !== 'repl') return;
  return { id: legacy ? value.rootId : value.id as string, kind, rootId: value.rootId, titleHint: title(value.titleHint), location: location(value.location) };
}
function restore(raw: string | null): readonly TabWorkspace[] {
  if (!raw || raw.length > MAX_BYTES || bytes(raw) > MAX_BYTES) return [];
  let parsed: unknown;
  try { parsed = JSON.parse(raw); } catch { return []; }
  if (!object(parsed) || ![1, 2].includes(parsed.version as number) || !Array.isArray(parsed.workspaces)) return [];
  const result: TabWorkspace[] = [];
  for (const entry of parsed.workspaces.slice(-4)) {
    if (!object(entry) || !identity(entry.runtimeId) || result.some(item => item.runtimeId === entry.runtimeId)) continue;
    const ids = new Set<string>(), nodes = new Set<string>();
    let paneCount = 0;
    const readTabs = (values: unknown, legacy = false) => {
      const tabs: SessionTab[] = [];
      for (const value of Array.isArray(values) ? values.slice(0, MAX_SESSION_TABS) : []) {
        const tab = parseTab(value, legacy);
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
      const tab = parseTab(value.tab, legacy);
      if (!tab || ids.has(tab.id) || closedIds.has(tab.id)) continue;
      closedIds.add(tab.id);
      closed.push({ tab, index: typeof value.index === 'number' && Number.isInteger(value.index) ? Math.max(0, Math.min(MAX_SESSION_TABS, value.index)) : 0, paneId: !legacy && identity(value.paneId) ? value.paneId : 'main' });
    }
    const workspace = freeze({ runtimeId: entry.runtimeId, layout, focusedPaneId: identity(entry.focusedPaneId) ? entry.focusedPaneId : 'main', closed });
    result.push(freeze({ ...workspace, lastActiveRootId: workspace.tabs.some(tab => tab.rootId === entry.lastActiveRootId) ? entry.lastActiveRootId as string : undefined }));
  }
  return result;
}
function serialize(workspaces: readonly TabWorkspace[]) {
  return JSON.stringify({ version: 2, workspaces: workspaces.map(({ tabs: _derived, ...workspace }) => workspace) });
}

/** One immutable owner for window-local view identities, panes and tab order. */
export class SessionTabs {
  private snapshot: TabsSnapshot;
  private listeners = new Set<() => void>();
  constructor(private readonly storage?: AppStorage, private readonly onNotice: (message: string) => void = () => {}) {
    let raw: string | null = null;
    try { raw = storage?.getItem(TAB_STORAGE_KEY) ?? storage?.getItem(LEGACY_TAB_STORAGE_KEY) ?? null; }
    catch { onNotice('Session tabs could not be restored. This window will keep its layout in memory.'); }
    this.snapshot = Object.freeze({ version: 2, workspaces: Object.freeze(restore(raw)) });
  }
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  workspace(runtimeId: string): TabWorkspace {
    return this.snapshot.workspaces.find(item => item.runtimeId === runtimeId) ?? emptyWorkspace(runtimeId);
  }
  private write(workspace: Omit<TabWorkspace, 'tabs'>) {
    if (!identity(workspace.runtimeId)) throw new Error('Invalid execution host identity');
    const nextWorkspace = freeze(workspace);
    if (JSON.stringify(this.workspace(workspace.runtimeId)) === JSON.stringify(nextWorkspace)) return;
    let workspaces = [...this.snapshot.workspaces.filter(item => item.runtimeId !== workspace.runtimeId), nextWorkspace].slice(-4);
    if (bytes(serialize(workspaces)) > MAX_BYTES) {
      workspaces = workspaces.map(item => freeze({ ...item, closed: [] }));
      while (workspaces.length > 1 && bytes(serialize(workspaces)) > MAX_BYTES) workspaces.shift();
    }
    if (bytes(serialize(workspaces)) > MAX_BYTES) throw new Error('This window’s tab layout is full. Close an open tab before adding another.');
    this.snapshot = Object.freeze({ version: 2, workspaces: Object.freeze(workspaces) });
    try { this.storage?.setItem(TAB_STORAGE_KEY, serialize(workspaces)); }
    catch { this.onNotice('Session tabs are kept in memory because window storage is unavailable.'); }
    for (const listener of this.listeners) listener();
  }
  canOpen(runtimeId: string, rootId?: string) {
    const workspace = this.workspace(runtimeId);
    return workspace.tabs.length < MAX_SESSION_TABS || workspace.tabs.some(item => item.rootId === rootId);
  }
  preferred(runtimeId: string, rootId: string): SessionTab | undefined {
    const workspace = this.workspace(runtimeId);
    const selected = selectedSessionTab(workspace);
    return (selected?.rootId === rootId ? selected : undefined) ?? sessionPanes(workspace.layout).find(p => p.id === workspace.focusedPaneId)?.tabs.find(t => t.rootId === rootId) ?? workspace.tabs.find(t => t.rootId === rootId);
  }
  open(runtimeId: string, rootId: string, titleHint = '', paneId?: string): string {
    if (!identity(rootId)) throw new Error('Invalid session identity');
    const existing = this.preferred(runtimeId, rootId);
    if (existing) return existing.id;
    return this.add(runtimeId, { id: rootId, kind: 'chat', rootId, titleHint: title(titleHint), location: location({}) }, paneId);
  }
  private add(runtimeId: string, tab: SessionTab, paneId?: string): string {
    if (!this.canOpen(runtimeId)) throw new Error('There are 32 open session tabs. Close a tab before opening another.');
    const workspace = this.workspace(runtimeId);
    const target = sessionPanes(workspace.layout).find(p => p.id === (paneId ?? workspace.focusedPaneId)) ?? sessionPanes(workspace.layout)[0]!;
    // Reopened roots may share an old primary ID with a different view in history.
    if (workspace.tabs.some(t => t.id === tab.id)) tab = { ...tab, id: newId() };
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => p.id === target.id ? { ...p, tabs: [...p.tabs, tab] } : p), closed: workspace.closed.filter(item => item.tab.id !== tab.id) });
    return tab.id;
  }
  visit(runtimeId: string, rootId: string, search: SessionSearch, viewId?: string) {
    const id = this.workspace(runtimeId).tabs.find(t => t.id === viewId && t.rootId === rootId)?.id ?? this.open(runtimeId, rootId);
    const workspace = this.workspace(runtimeId);
    const pane = sessionViewPane(workspace, id)!;
    this.write({ ...workspace, focusedPaneId: pane.id, lastActiveRootId: rootId, layout: mapPanes(workspace.layout, p => p.id === pane.id ? { ...p, selected: id, tabs: p.tabs.map(t => t.id === id ? { ...t, kind: search.view === 'repl' ? 'repl' : 'chat', location: location(search) } : t) } : p) });
    return id;
  }
  updateLocation(runtimeId: string, viewId: string, search: SessionLocation) {
    const workspace = this.workspace(runtimeId);
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => ({ ...p, tabs: p.tabs.map(t => t.id === viewId ? { ...t, location: location(search) } : t) })) });
  }
  activate(runtimeId: string, viewId: string, focus = true) {
    const workspace = this.workspace(runtimeId), pane = sessionViewPane(workspace, viewId);
    if (!pane) return;
    this.write({ ...workspace, focusedPaneId: focus ? pane.id : workspace.focusedPaneId,
      lastActiveRootId: focus || pane.id === workspace.focusedPaneId ? pane.tabs.find(t => t.id === viewId)!.rootId : workspace.lastActiveRootId,
      layout: mapPanes(workspace.layout, p => p.id === pane.id ? { ...p, selected: viewId } : p) });
  }
  home(runtimeId: string) {
    const workspace = this.workspace(runtimeId);
    if (workspace.lastActiveRootId) this.write({ ...workspace, lastActiveRootId: undefined });
  }
  titles(runtimeId: string, titles: ReadonlyMap<string, string>) {
    const workspace = this.workspace(runtimeId);
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => ({ ...p, tabs: p.tabs.map(t => titles.has(t.rootId) ? { ...t, titleHint: title(titles.get(t.rootId)) } : t) })) });
  }
  closeViews(runtimeId: string, viewIds: readonly string[], activeViewId?: string): string | null | undefined {
    const workspace = this.workspace(runtimeId);
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
    this.write({ ...next, lastActiveRootId: workspace.lastActiveRootId ? selected?.rootId : undefined });
    return activeViewId && viewIds.includes(activeViewId) ? selected?.id ?? null : undefined;
  }
  close(runtimeId: string, rootIds: readonly string[], activeRootId?: string): string | null | undefined {
    const workspace = this.workspace(runtimeId);
    const active = workspace.tabs.find(t => t.rootId === activeRootId);
    const next = this.closeViews(runtimeId, workspace.tabs.filter(t => rootIds.includes(t.rootId)).map(t => t.id), active?.id);
    return typeof next === 'string' ? this.workspace(runtimeId).tabs.find(t => t.id === next)?.rootId : next;
  }
  reopenView(runtimeId: string): string | undefined {
    const workspace = this.workspace(runtimeId), closed = workspace.closed.at(-1);
    if (!closed) return;
    if (!this.canOpen(runtimeId)) throw new Error('There are 32 open session tabs. Close a tab before reopening another.');
    const pane = sessionPanes(workspace.layout).find(p => p.id === closed.paneId) ?? sessionPanes(workspace.layout).find(p => p.id === workspace.focusedPaneId)!;
    const tabs = [...pane.tabs]; tabs.splice(Math.min(closed.index, tabs.length), 0, closed.tab);
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => p.id === pane.id ? { ...p, tabs } : p), closed: workspace.closed.slice(0, -1) });
    return closed.tab.id;
  }
  reorderPane(runtimeId: string, paneId: string, order: readonly string[]) {
    const workspace = this.workspace(runtimeId), pane = sessionPanes(workspace.layout).find(p => p.id === paneId);
    if (!pane || order.length !== pane.tabs.length || new Set(order).size !== order.length || order.some(id => !pane.tabs.some(t => t.id === id))) throw new Error('Invalid session tab order');
    this.write({ ...workspace, layout: mapPanes(workspace.layout, p => p.id === paneId ? { ...p, tabs: order.map(id => p.tabs.find(t => t.id === id)!) } : p) });
  }
  move(runtimeId: string, viewId: string, offset: -1 | 1) {
    const workspace = this.workspace(runtimeId), pane = sessionViewPane(workspace, viewId);
    if (!pane) return;
    const order = pane.tabs.map(t => t.id), from = order.indexOf(viewId), to = from + offset;
    if (to < 0 || to >= order.length) return;
    [order[from], order[to]] = [order[to]!, order[from]!];
    this.reorderPane(runtimeId, pane.id, order);
  }
  split(runtimeId: string, viewId: string, edge: SplitEdge): string {
    const workspace = this.workspace(runtimeId), pane = sessionViewPane(workspace, viewId), source = workspace.tabs.find(t => t.id === viewId);
    if (!pane || !source) throw new Error('This view is no longer open');
    if (sessionPanes(workspace.layout).length >= MAX_SESSION_PANES) throw new Error('There are four panes. Move a tab to an existing pane or close a pane first.');
    if (!this.canOpen(runtimeId)) throw new Error('There are 32 open session tabs. Close a tab before splitting this view.');
    const duplicate = { ...source, id: newId() };
    const newPane: SessionPane = { type: 'pane', id: newId(), tabs: [duplicate], selected: duplicate.id };
    this.write({ ...workspace, layout: replaceNode(workspace.layout, pane.id, node => splitNode(node, newPane, edge)), focusedPaneId: newPane.id, lastActiveRootId: source.rootId });
    return duplicate.id;
  }
  transfer(runtimeId: string, viewId: string, paneId: string, edge?: SplitEdge, index?: number) {
    const workspace = this.workspace(runtimeId), sourcePane = sessionViewPane(workspace, viewId), targetPane = sessionPanes(workspace.layout).find(p => p.id === paneId);
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
    this.write({ ...workspace, layout: prune(layout)!, focusedPaneId, lastActiveRootId: tab.rootId });
  }
  resize(runtimeId: string, splitId: string, ratio: number) {
    if (!Number.isFinite(ratio)) return;
    const workspace = this.workspace(runtimeId);
    this.write({ ...workspace, layout: replaceNode(workspace.layout, splitId, n => n.type === 'split' ? { ...n, ratio: Math.max(.01, Math.min(.99, ratio)) } : n) });
  }
  dispose() { this.listeners.clear(); }
}
function splitNode(existing: SessionLayout, pane: SessionPane, edge: SplitEdge): SessionSplit {
  const before = edge === 'left' || edge === 'top';
  return { type: 'split', id: newId(), direction: edge === 'left' || edge === 'right' ? 'horizontal' : 'vertical', ratio: .5, first: before ? pane : existing, second: before ? existing : pane };
}
