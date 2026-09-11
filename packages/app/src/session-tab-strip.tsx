import { typography } from '@whip/ui/tokens.stylex';
import { useEffect, useImperativeHandle, useMemo, useRef, useState, useSyncExternalStore, type ReactElement, type ReactNode, type Ref } from 'react';
import { Link, useLocation, useNavigate } from '@tanstack/react-router';
import { useQueries } from '@tanstack/react-query';
import { sessionBusy, sessionNeedsInput, summaryDescription, type SessionNavigationSummary } from './session-status';
import { Button, ContextMenu, IconButton, Input, Menu, Sheet, type MenuItem } from '@whip/ui';
import { WorkspaceTabs, workspaceTabId } from '@whip/ui/workspace-tabs';
import { WorkspaceLayout, workspacePanelId, type WorkspaceDrop } from '@whip/ui/workspace-layout';
import { SessionContent, SessionLoading } from './conversation';
import { selectedSessionTab, sessionPanes, sessionViewPane, sessionSearch, type SessionPane, type SessionTab, type SplitEdge } from './session-tabs';
import { useWorkspaceViews, workspaceRootKey } from './workspace-views';
import { ChevronDown, Circle, CircleHelp, MessageSquare, MessageSquareWarning, MoreHorizontal, Pencil, Plus, Columns2, X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { ErrorNotice } from './error-feedback';
import { Welcome } from './welcome';
import { draftDestination, openNewChat, tabDestination, sessionDestination } from './session-tab-routing';
import { layout } from './styles';

export interface SessionTabActions { next(offset: -1 | 1): void; close(): boolean; reopen(): void; showPicker(): void }
function Status({ item, stale }: { item?: SessionNavigationSummary; stale: boolean }) {
  if (!item || stale || item.missing) return <CircleHelp size={13} aria-hidden="true" />;
  if (sessionNeedsInput(item, stale)) return <MessageSquareWarning size={14} {...stylex.props(styles.attention)} aria-hidden="true" />;
  if (sessionBusy(item, stale)) return <Circle size={7} fill="currentColor" {...stylex.props(styles.running)} aria-hidden="true" />;
  return <MessageSquare size={13} aria-hidden="true" />;
}

export function SessionTabStrip({ compact, onManageHosts, utilities, children, notices, ref, sidebarHidden = false }: {
  compact: boolean;
  onManageHosts(): void;
  utilities: ReactNode;
  children?: ReactNode;
  notices?: ReactNode;
  ref?: Ref<SessionTabActions>;
  /** Sidebar is hidden: the strip's leading edge must clear the traffic lights. */
  sidebarHidden?: boolean;
}) {
  const runtime = useRuntime();
  const { hosts } = useAppState();
  useSyncExternalStore(runtime.compositions.subscribe, runtime.compositions.getSnapshot, runtime.compositions.getSnapshot);
  useSessionTabs();
  const workspace = runtime.tabs.workspace();
  const tabs = workspace.tabs;
  const panes = sessionPanes(workspace.layout);
  const route = useLocation();
  const destination = sessionDestination(route.pathname);
  // Keep other panes mounted during the route commit before onResolved admits a new tab.
  const matched = (!!destination && tabs.length > 0 && runtime.tabs.canOpen(destination.runtimeId, destination.rootId)) || !!tabs.find(tab => tab.id === draftDestination(route.pathname));
  const active = matched ? selectedSessionTab(workspace) : undefined;
  const navigate = useNavigate();
  const [picker, setPicker] = useState(false);
  const [search, setSearch] = useState('');
  const [notice, setNotice] = useState('');
  const [small, setSmall] = useState(compact);
  const inset = runtime.platform.chrome === 'inset';
  const [visible, setVisible] = useState(() => document.visibilityState !== 'hidden');
  useEffect(() => { const change = () => setVisible(document.visibilityState !== 'hidden'); document.addEventListener('visibilitychange', change); return () => document.removeEventListener('visibilitychange', change); }, []);
  useEffect(() => { if (!notice) return; const timer = setTimeout(() => setNotice(''), 5000); return () => clearTimeout(timer); }, [notice]);
  const groups = useMemo(() => hosts.flatMap(host => {
    if (!host.client || !host.runtimeId) return [];
    const ids = [...new Set(tabs.filter(tab => tab.kind !== 'new').filter(tab => tab.runtimeId === host.runtimeId).map(tab => tab.rootId))].sort();
    if (!ids.length) return [];
    const supported = host.client.getSnapshot().info?.negotiated_capabilities?.includes('session_summaries') ?? false;
    return [{ host, client: host.client, runtimeId: host.runtimeId, ids, enabled: visible && host.state === 'connected' && supported }];
  }), [hosts, tabs, visible]);
  const summaries = useQueries({ queries: groups.map(group => ({
    queryKey: ['session-tab-summaries', group.runtimeId, group.ids],
    queryFn: ({ signal }: { signal: AbortSignal }) => group.client.sessions.summaries(group.ids, { signal }),
    enabled: group.enabled, refetchInterval: group.enabled ? 2000 : false as const,
    refetchIntervalInBackground: false, refetchOnWindowFocus: true, staleTime: 0, gcTime: 0,
  })) });
  const items = new Map(summaries.flatMap((query, index) => query.data?.items.map(item => [workspaceRootKey({ runtimeId: groups[index]!.runtimeId, rootId: item.root_id }), item] as const) ?? []));
  const staleHosts = new Set(groups.filter((group, index) => !group.enabled || !!summaries[index]?.error).map(group => group.runtimeId));
  const stale = (tab: SessionTab) => tab.kind !== 'new' && (!groups.some(group => group.runtimeId === tab.runtimeId) || staleHosts.has(tab.runtimeId));
  const item = (tab: SessionTab) => tab.kind === 'new' ? undefined : items.get(workspaceRootKey(tab));
  const hostName = (tab: SessionTab) => hosts.find(host => tab.kind === 'new' && tab.hostProfileId ? host.id === tab.hostProfileId : !!tab.runtimeId && host.runtimeId === tab.runtimeId)?.name ?? (tab.runtimeId ? `Unavailable host ${tab.runtimeId.slice(0, 8)}` : 'Choose a host');
  useEffect(() => {
    const cleanups = groups.filter(group => group.enabled).map(group => {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const off = group.client.onEvent(event => {
        if (!group.ids.includes(event.root_id) || !/^(?:turn\.|agent\.(?:turn\.|admitted|prompt\.queued|subtree\.)|root\.|permission\.|question\.)/.test(event.kind) || timer) return;
        timer = setTimeout(() => { timer = undefined; void runtime.queries.invalidateQueries({ queryKey: ['session-tab-summaries', group.runtimeId] }); }, 250);
      });
      return () => { off(); clearTimeout(timer); };
    });
    return () => cleanups.forEach(cleanup => cleanup());
  }, [groups, runtime]);
  useEffect(() => {
    summaries.forEach((query, index) => {
      if (query.data) runtime.tabs.titles(groups[index]!.runtimeId, new Map(query.data.items.filter(item => !item.missing).map(item => [item.root_id, item.title])));
    });
  }, [runtime, groups, summaries]);

  const pendingNavigation = useRef<string | undefined>(undefined);
  const title = (tab: SessionTab) => tab.kind === 'new' ? 'New Chat' : item(tab)?.title || tab.titleHint || 'Untitled session';
  const project = (tab: SessionTab) => (tab.kind === 'new' ? tab.cwd : item(tab)?.cwd)?.split(/[\\/]/).filter(Boolean).at(-1) ?? '';
  const hasDraft = (tab: SessionTab) => tab.kind !== 'new' && runtime.hasSessionDraft(tab.runtimeId, tab.rootId);
  const go = (viewId: string, replace = false) => {
    runtime.clearWorkspaceError();
    const tab = runtime.tabs.workspace().tabs.find(t => t.id === viewId);
    if (!tab) return;
    setPicker(false);
    const location = route.search as { agent?: string; panel?: string; view?: 'repl' };
    if (tab.kind !== 'new' && destination?.runtimeId === tab.runtimeId && destination.rootId === tab.rootId && route.state.whipViewId === viewId && location.agent === tab.location.agent && location.panel === tab.location.panel && location.view === sessionSearch(tab).view) return;
    const target = JSON.stringify(tabDestination(tab));
    if (pendingNavigation.current === target) return;
    pendingNavigation.current = target;
    void navigate({ ...tabDestination(tab), replace }).catch(error => runtime.reportWorkspace(error)).finally(() => { if (pendingNavigation.current === target) pendingNavigation.current = undefined; });
  };
  const focusedPane = panes.find(p => p.id === workspace.focusedPaneId)!;
  const focusPane = (paneId: string) => {
    const current = runtime.tabs.workspace();
    if (!matched || current.focusedPaneId === paneId) return;
    const pane = sessionPanes(current.layout).find(p => p.id === paneId);
    if (pane?.selected) go(pane.selected);
  };
  const close = (viewIds: readonly string[], focusAfterMenu = false) => {
    const focused = document.activeElement;
    const restoreFocus = viewIds.some(id => document.getElementById(workspaceTabId(id))?.closest('[data-workspace-tab]')?.contains(focused));
    const next = runtime.tabs.closeViews(viewIds, active?.id);
    setNotice(viewIds.length === 1 ? 'Tab closed. Work and drafts are kept.' : 'Tabs closed. Work and drafts are kept.');
    if (next === null) void navigate({ to: '/', replace: true });
    else if (next) go(next, true);
    if (restoreFocus || focusAfterMenu || viewIds.includes(active?.id ?? '')) requestAnimationFrame(() => {
      const replacement = selectedSessionTab(runtime.tabs.workspace());
      const target = replacement && document.getElementById(workspaceTabId(replacement.id));
      if (target) target.focus({ preventScroll: true });
      else document.querySelector<HTMLButtonElement>('[aria-label^="Open sessions:"], [aria-label="New session tab"]')?.focus();
    });
  };
  const reopen = () => {
    try { const id = runtime.tabs.reopenView(); if (id) go(id); setNotice(''); }
    catch (error) { runtime.reportWorkspace(error); }
  };
  const split = (tab: SessionTab, edge: SplitEdge) => {
    try {
      const id = runtime.tabs.split(tab.id, edge);
      if (tab.kind === 'new') { go(id); return; }
      const oldKey = `${tab.runtimeId}:${tab.id}:${tab.location.agent ?? tab.rootId}`;
      const newKey = `${tab.runtimeId}:${id}:${tab.location.agent ?? tab.rootId}`;
      for (const suffix of ['', ':repl']) {
        const bookmark = runtime.readingPositions.get(oldKey + suffix);
        if (bookmark) runtime.readingPositions.set(newKey + suffix, bookmark);
      }
      const caret = runtime.compositions.selection(oldKey);
      if (caret) runtime.compositions.rememberSelection(newKey, caret);
      go(id);
    } catch (error) { runtime.reportWorkspace(error); }
  };
  const canSplit = (paneId: string, edge: SplitEdge) => {
    if (compact || small || panes.length >= 4) return false;
    const frame = [...document.querySelectorAll<HTMLElement>('[data-workspace-frame]')].find(el => el.dataset.workspaceFrame === paneId);
    if (!frame) return false;
    return edge === 'left' || edge === 'right' ? frame.clientWidth >= 641 : frame.clientHeight >= 481;
  };
  const canDrop = (drop: WorkspaceDrop) => {
    if (!drop.edge) return true;
    const source = sessionViewPane(runtime.tabs.workspace(), drop.viewId);
    return !!source && !(source.id === drop.paneId && source.tabs.length === 1) && panes.length + (source.tabs.length > 1 ? 1 : 0) <= 4;
  };
  const transfer = (drop: WorkspaceDrop) => {
    try { runtime.tabs.transfer(drop.viewId, drop.paneId, drop.edge, drop.index); go(drop.viewId); }
    catch (error) { runtime.reportWorkspace(error); }
  };
  const actions = (tab: SessionTab): MenuItem[] => {
    const pane = sessionViewPane(workspace, tab.id)!;
    const index = pane.tabs.findIndex(item => item.id === tab.id);
    if (tab.kind === 'new') return [
      { id: 'close', label: 'Close tab', onSelect: () => close([tab.id], true) },
      { id: 'others', label: 'Close other tabs', disabled: pane.tabs.length < 2, onSelect: () => close(pane.tabs.filter(item => item.id !== tab.id).map(item => item.id), true) },
      { id: 'right', label: 'Close tabs to the right', disabled: index === pane.tabs.length - 1, onSelect: () => close(pane.tabs.slice(index + 1).map(item => item.id), true) },
      ...panes.filter(p => p.id !== pane.id).map(p => ({ id: `move-${p.id}`, label: `Move to pane ${panes.indexOf(p) + 1}`, onSelect: () => transfer({ viewId: tab.id, paneId: p.id }) })),
      { id: 'split-right', label: 'Move to split right', disabled: pane.tabs.length < 2 || !canSplit(pane.id, 'right'), onSelect: () => transfer({ viewId: tab.id, paneId: pane.id, edge: 'right' }) },
      { id: 'split-down', label: 'Move to split below', disabled: pane.tabs.length < 2 || !canSplit(pane.id, 'bottom'), onSelect: () => transfer({ viewId: tab.id, paneId: pane.id, edge: 'bottom' }) },
      { id: 'left', label: 'Move left', disabled: index <= 0, onSelect: () => runtime.tabs.move(tab.id, -1) },
      { id: 'move-right', label: 'Move right', disabled: index >= pane.tabs.length - 1, onSelect: () => runtime.tabs.move(tab.id, 1) },
    ];
    const href = new URL(`/h/${encodeURIComponent(tab.runtimeId)}/s/${encodeURIComponent(tab.rootId)}`, 'https://whip.invalid');
    for (const [key, value] of Object.entries(sessionSearch(tab))) if (value) href.searchParams.set(key, value);
    return [
      { id: 'view', label: tab.kind === 'repl' ? 'Open chat' : 'Open REPL', onSelect: () => {
        setPicker(false);
        void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: tab.runtimeId, rootId: tab.rootId }, search: sessionSearch({ ...tab, kind: tab.kind === 'repl' ? 'chat' : 'repl' }), state: { whipViewId: tab.id } })
          .then(() => requestAnimationFrame(() => {
            if (selectedSessionTab(runtime.tabs.workspace())?.id === tab.id)
              document.getElementById(workspacePanelId(tab.id))?.focus();
          }))
          .catch(error => runtime.reportWorkspace(error));
      } },
      { id: 'details', label: 'Session details', onSelect: () => { setPicker(false); void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: tab.runtimeId, rootId: tab.rootId }, search: { ...sessionSearch(tab), panel: 'agents' }, state: { whipViewId: tab.id } }); } },
      { id: 'close', label: 'Close tab', onSelect: () => close([tab.id], true) },
      { id: 'others', label: 'Close other tabs', disabled: pane.tabs.length < 2, onSelect: () => close(pane.tabs.filter(item => item.id !== tab.id).map(item => item.id), true) },
      { id: 'right', label: 'Close tabs to the right', disabled: index === pane.tabs.length - 1, onSelect: () => close(pane.tabs.slice(index + 1).map(item => item.id), true) },
      { id: 'separator', separator: true, label: '' },
      { id: 'split-right', label: 'Split right', disabled: !canSplit(pane.id, 'right') || tabs.length >= 32, onSelect: () => split(tab, 'right') },
      { id: 'split-down', label: 'Split down', disabled: !canSplit(pane.id, 'bottom') || tabs.length >= 32, onSelect: () => split(tab, 'bottom') },
      ...panes.filter(p => p.id !== pane.id).map(p => ({ id: `move-${p.id}`, label: `Move to pane ${panes.indexOf(p) + 1}`, onSelect: () => transfer({ viewId: tab.id, paneId: p.id }) })),
      ...(['right', 'bottom'] as const).map(edge => ({ id: `move-new-${edge}`, label: `Move to new split ${edge === 'bottom' ? 'below' : 'right'}`, disabled: pane.tabs.length < 2 || !canSplit(pane.id, edge), onSelect: () => transfer({ viewId: tab.id, paneId: pane.id, edge }) })),
      { id: 'left', label: 'Move left', disabled: index <= 0, onSelect: () => runtime.tabs.move(tab.id, -1) },
      { id: 'move-right', label: 'Move right', disabled: index >= pane.tabs.length - 1, onSelect: () => runtime.tabs.move(tab.id, 1) },
      { id: 'link', label: 'Copy session link', onSelect: () => void runtime.platform.copy(runtime.platform.sessionLink ? runtime.platform.sessionLink(href.pathname + href.search, runtime.connections.host(tab.runtimeId)?.profile) : new URL(href.pathname + href.search, window.location.href).href).catch(error => runtime.reportWorkspace(error)) },
    ];
  };
  useImperativeHandle(ref, () => ({
    next(offset) { const list = focusedPane.tabs; if (!list.length) return; const index = list.findIndex(t => t.id === active?.id); go(list[index < 0 ? offset === 1 ? 0 : list.length - 1 : (index + offset + list.length) % list.length]!.id); },
    close() { if (!active) return false; close([active.id]); return true; }, reopen,
    showPicker() { setPicker(true); setSearch(''); },
  }));
  const add = <IconButton variant="ghost" label="New session tab" onClick={() => openNewChat(runtime, navigate)}><Plus size={17} /></IconButton>;
  const renderStrip = (pane: SessionPane, shared: boolean) => <WorkspaceTabs groupId={shared ? pane.id : undefined} label={panes.length > 1 ? `Open sessions in pane ${panes.indexOf(pane) + 1}` : 'Open sessions'}
    windowDrag={inset && pane.id === panes[0]?.id} trafficLightInset={inset && sidebarHidden}
    leading={(!matched || pane.id === visiblePanes[0]?.id) ? utilities : undefined}
    value={matched ? pane.selected ?? null : null} onClose={id => close([id])} panelId={pane.selected ? workspacePanelId(pane.selected) : undefined}
    onReorder={order => runtime.tabs.reorderPane(pane.id, order)} utilities={<>{add}{matched && <Menu trigger={<IconButton label={`Pane ${panes.indexOf(pane) + 1} actions`} variant="ghost"><Columns2 size={16} /></IconButton>} items={[
      { id: 'split-right', label: 'Split right', disabled: !pane.selected || pane.tabs.find(tab => tab.id === pane.selected)?.kind === 'new' || !canSplit(pane.id, 'right') || tabs.length >= 32, onSelect: () => { const tab = pane.tabs.find(t => t.id === pane.selected); if (tab) split(tab, 'right'); } },
      { id: 'split-down', label: 'Split down', disabled: !pane.selected || pane.tabs.find(tab => tab.id === pane.selected)?.kind === 'new' || !canSplit(pane.id, 'bottom') || tabs.length >= 32, onSelect: () => { const tab = pane.tabs.find(t => t.id === pane.selected); if (tab) split(tab, 'bottom'); } },
      ...panes.filter(p => p.id !== pane.id).map(p => ({ id: `focus-${p.id}`, label: `Focus pane ${panes.indexOf(p) + 1}`, onSelect: () => { if (p.selected) { go(p.selected); requestAnimationFrame(() => document.getElementById(workspacePanelId(p.selected!))?.focus()); } } })),
    ]}/>}</>}
    items={pane.tabs.map(tab => ({ value: tab.id, label: title(tab), accessibleLabel: `${title(tab)} · ${hostName(tab)}${tab.kind === 'repl' ? ' · REPL' : ''}${panes.length > 1 ? ` · Pane ${panes.indexOf(pane) + 1}` : ''}${tab.kind !== 'new' && tab.location.agent ? ` · Agent ${tab.location.agent}` : ''} · ${tab.kind === 'new' ? 'Not sent yet' : summaryDescription(item(tab), stale(tab))}${hasDraft(tab) ? ' · Unsent draft' : ''}`,
      render: <Link {...tabDestination(tab)} />,
      status: tab.kind === 'new' ? <MessageSquare size={13} aria-hidden="true"/> : <Status item={item(tab)} stale={stale(tab)}/>, metadata: <>{hostName(tab)}{tab.kind === 'repl' ? ' · REPL' : ''}{hasDraft(tab) && <Pencil aria-label="Unsent draft" size={10}/>}</>, tooltip: `${title(tab)} · ${hostName(tab)}${tab.kind === 'repl' ? ' · REPL' : ''}${project(tab) ? ` · ${project(tab)}` : ''}`,
      menu: <Menu trigger={<IconButton variant="ghost" label={`Tab actions for ${title(tab)}`}><MoreHorizontal size={13}/></IconButton>} items={actions(tab)} />,
      wrap: (element: ReactElement) => <ContextMenu items={actions(tab)}>{element}</ContextMenu>,
    }))}/>;
  const visiblePanes = matched ? panes.filter(p => !(compact || small) || p.id === workspace.focusedPaneId) : [];
  const visibleTabs = visiblePanes.flatMap(p => p.tabs.filter(t => t.id === p.selected));
  const views = useWorkspaceViews(runtime, visibleTabs.filter(tab => tab.kind !== 'new'), hosts);
  return <div {...stylex.props(styles.workspace)}>
    {compact && <header {...stylex.props(styles.mobileBar)}>
      {utilities}
      <Button variant="ghost" xstyle={styles.mobileSelector} aria-label={`Open sessions: ${active ? title(active) : 'New session'}, ${tabs.length} tabs`} onClick={() => { setSearch(''); setPicker(true); }}>
        {active && (active.kind === 'new' ? <MessageSquare size={13} aria-hidden="true"/> : <Status item={item(active)} stale={stale(active)}/>)}<span {...stylex.props(layout.ellipsis, layout.grow)}>{active ? `${title(active)}${active.kind === 'repl' ? ' · REPL' : ''}` : 'New session'}</span><span {...stylex.props(styles.count)}>{tabs.length}</span><ChevronDown size={14}/>
      </Button>{add}
    </header>}
    {notices}
    <ErrorNotice type="action" owner="workspace" title="Could not update the workspace" error={runtime.getSnapshot().workspaceError} onDismiss={() => runtime.clearWorkspaceError()} />
    {matched ? <WorkspaceLayout layout={workspace.layout} focusedPaneId={workspace.focusedPaneId} compact={compact} onCompactChange={setSmall}
      onResize={(id, ratio) => runtime.tabs.resize(id, ratio)} onDrop={transfer} canDrop={canDrop} onFocusPane={focusPane}
      renderHeader={id => compact ? null : renderStrip(panes.find(p => p.id === id)!, true)}
      panels={visibleTabs.map(tab => {
        const pane = sessionViewPane(workspace, tab.id)!;
        if (tab.kind === 'new') return { id: tab.id, paneId: pane.id, label: `Pane ${panes.indexOf(pane) + 1}: New Chat`, labelledBy: compact ? undefined : workspaceTabId(tab.id),
          content: <Welcome key={tab.id} tab={tab} focused={pane.id === workspace.focusedPaneId} /> };
        const view = views.views.get(workspaceRootKey(tab));
        return { id: tab.id, paneId: pane.id, label: `Pane ${panes.indexOf(pane) + 1}: ${title(tab)}${tab.kind === 'repl' ? ' · REPL' : ''}`, labelledBy: compact ? undefined : workspaceTabId(tab.id),
          content: view && view.session.client === hosts.find(host => host.runtimeId === tab.runtimeId)?.client ? <SessionContent kind={tab.kind} key={workspaceRootKey(tab)} view={view} expectedRuntimeId={tab.runtimeId} agentId={tab.location.agent ?? tab.rootId} panel={tab.location.panel} viewId={tab.id}/> : views.errors.has(workspaceRootKey(tab)) ? <div {...stylex.props(layout.empty)}>{hosts.find(host => host.runtimeId === tab.runtimeId)?.state === 'connected' ? <ErrorNotice type="session" owner={workspaceRootKey(tab)} error={views.errors.get(workspaceRootKey(tab))} /> : <p role="status">{hostName(tab)} is unavailable. Connect it to continue this session.</p>}<Button variant="secondary" onClick={onManageHosts}>Manage servers</Button></div> : <SessionLoading /> };
      })}/>
      : <>{!compact && renderStrip(focusedPane, false)}{children}</>}
    <Sheet open={picker} onOpenChange={setPicker} title="Open sessions" description="Closing a tab leaves its session, drafts, and agents on the host.">
      <Input aria-label="Find an open session" placeholder="Find a session or project…" value={search} onChange={event => setSearch(event.target.value)}/>
      <div {...stylex.props(styles.pickerList)}>
        {tabs.filter(tab => `${title(tab)} ${hostName(tab)} ${item(tab)?.cwd ?? ''}`.toLocaleLowerCase().includes(search.toLocaleLowerCase())).map(tab => <div key={tab.id} {...stylex.props(styles.pickerRow, active?.id === tab.id && styles.selected)}>
          <button {...stylex.props(styles.pickerSelect)} onClick={() => go(tab.id)}>{tab.kind === 'new' ? <MessageSquare size={13} aria-hidden="true"/> : <Status item={item(tab)} stale={stale(tab)}/>}<span {...stylex.props(layout.column, styles.pickerText)}><strong>{title(tab)}{tab.kind === 'repl' ? ' · REPL' : ''}</strong><span {...stylex.props(layout.muted)}>{panes.length > 1 ? `Pane ${panes.indexOf(sessionViewPane(workspace, tab.id)!) + 1} · ` : ''}{hostName(tab)} · {project(tab)}{project(tab) ? ' · ' : ''}{tab.kind === 'new' ? 'Not sent yet' : summaryDescription(item(tab), stale(tab))}{hasDraft(tab) ? ' · Unsent draft' : ''}</span></span></button>
          <Menu trigger={<IconButton variant="ghost" label={`Tab actions for ${title(tab)}`}><MoreHorizontal size={15}/></IconButton>} items={actions(tab)}/>
          <IconButton variant="ghost" label={`Close ${title(tab)}`} onClick={() => close([tab.id])}><X size={16}/></IconButton>
        </div>)}
        {!tabs.length && <p {...stylex.props(layout.muted)}>No open session tabs. Choose a saved session or start a new one.</p>}
      </div>
      <Button variant="ghost" disabled={!workspace.closed.length} onClick={reopen}>Reopen closed tab</Button>
    </Sheet>
    {notice && <div role="status" {...stylex.props(styles.toast)}><span>{notice}</span><Button variant="ghost" onClick={reopen}>Reopen</Button><IconButton label="Dismiss tab notice" onClick={() => setNotice('')}><X size={13}/></IconButton></div>}
  </div>;
}

const styles = stylex.create({
  workspace: { display: 'flex', flexDirection: 'column', flex: 1, minWidth: 0, minHeight: 0, overflow: 'hidden' },
  running: { color: colors.primary }, attention: { color: colors.warning },
  mobileBar: { display: 'flex', alignItems: 'center', gap: 2, minHeight: 52, paddingInline: 4, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder, backgroundColor: surface.navigation, flexShrink: 0 },
  mobileSelector: { flex: 1, minWidth: 0, minHeight: 44, textAlign: 'start' },
  count: { fontSize: typography.size11, color: surface.secondaryText, backgroundColor: colors.element, padding: '2px 5px', borderRadius: 4 },
  pickerList: { display: 'flex', flexDirection: 'column', gap: 4, overflowY: 'auto', maxHeight: '65dvh' },
  pickerRow: { display: 'flex', alignItems: 'center', minWidth: 0, borderRadius: 6 },
  selected: { backgroundColor: colors.element },
  pickerSelect: { display: 'flex', alignItems: 'center', gap: 12, flex: 1, minWidth: 0, minHeight: 60, padding: 12, textAlign: 'start', borderWidth: 0, borderStyle: 'none', borderRadius: 6, color: colors.foreground, backgroundColor: { default: 'transparent', ':hover': colors.hover }, cursor: 'pointer' },
  pickerText: { gap: 5, overflowWrap: 'anywhere', fontSize: typography.size13 },
  toast: { position: 'fixed', bottom: { default: 20, [scale.phone]: 12 }, insetInlineStart: '50%', transform: 'translateX(-50%)', zIndex: 30, display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 8, backgroundColor: colors.panel, color: colors.foreground, fontSize: typography.size12, boxShadow: '0 4px 24px #0002', maxWidth: 'calc(100vw - 24px)' },
});
