import { useEffect, useImperativeHandle, useMemo, useState, useSyncExternalStore, type ReactElement, type ReactNode, type Ref } from 'react';
import { Link, useLocation, useNavigate } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useWhipConnection } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import type { SessionSummariesResult } from '@whip/protocol';
import { Button, ContextMenu, IconButton, Input, Menu, Sheet, Tooltip, type MenuItem } from '@whip/ui';
import { WorkspaceTabs, workspaceTabId } from '@whip/ui/workspace-tabs';
import { ChevronDown, Circle, CircleHelp, MessageSquare, MessageSquareWarning, MoreHorizontal, Pencil, Plus, X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { sessionDestination } from './session-tab-routing';
import { layout } from './styles';

export interface SessionTabActions { next(offset: -1 | 1): void; close(): void; reopen(): void; showPicker(): void }
type SessionNavigationSummary = SessionSummariesResult['items'][number];
export function summaryDescription(item?: SessionNavigationSummary, stale = false) {
  if (!item || stale) return 'Activity unavailable';
  if (item.missing) return 'Session unavailable';
  const needs = BigInt(item.pending_permissions) + BigInt(item.pending_questions);
  if (needs > 0n) return `${needs} ${needs === 1n ? 'request needs' : 'requests need'} your input`;
  if (BigInt(item.running_agents) || BigInt(item.queued_agents)) return `${item.running_agents} running · ${item.queued_agents} queued agents`;
  return 'No currently observed activity';
}
function Status({ item, stale }: { item?: SessionNavigationSummary; stale: boolean }) {
  if (!item || stale || item.missing) return <CircleHelp size={13} aria-hidden="true" />;
  if (BigInt(item.pending_permissions) + BigInt(item.pending_questions) > 0n) return <MessageSquareWarning size={14} {...stylex.props(styles.attention)} aria-hidden="true" />;
  if (BigInt(item.running_agents) || BigInt(item.queued_agents)) return <Circle size={7} fill="currentColor" {...stylex.props(styles.running)} aria-hidden="true" />;
  return <MessageSquare size={13} aria-hidden="true" />;
}

export function SessionTabStrip({ client, compact, utilities, ref }: {
  client: WhipClient;
  compact: boolean;
  utilities: ReactNode;
  ref?: Ref<SessionTabActions>;
}) {
  const runtime = useRuntime();
  const app = useAppState();
  useSyncExternalStore(runtime.compositions.subscribe, runtime.compositions.getSnapshot, runtime.compositions.getSnapshot);
  const connection = useWhipConnection(client);
  const runtimeId = connection.info?.runtime_id ?? '';
  const snapshot = useSessionTabs();
  const workspace = snapshot.workspaces.find(item => item.runtimeId === runtimeId);
  const tabs = workspace?.tabs ?? [];
  const location = useLocation();
  const destination = sessionDestination(location.pathname);
  const active = destination?.runtimeId === runtimeId ? destination.rootId : undefined;
  const navigate = useNavigate();
  const [picker, setPicker] = useState(false);
  const [search, setSearch] = useState('');
  const [notice, setNotice] = useState('');
  const [visible, setVisible] = useState(() => document.visibilityState !== 'hidden');
  useEffect(() => { const change = () => setVisible(document.visibilityState !== 'hidden'); document.addEventListener('visibilitychange', change); return () => document.removeEventListener('visibilitychange', change); }, []);
  useEffect(() => { if (!notice) return; const timer = setTimeout(() => setNotice(''), 5000); return () => clearTimeout(timer); }, [notice]);
  const ids = useMemo(() => tabs.map(item => item.rootId).sort(), [tabs]);
  const supported = connection.info?.negotiated_capabilities?.includes('session_summaries') ?? false;
  const summaries = useQuery({
    queryKey: ['session-tab-summaries', runtimeId, ids],
    queryFn: ({ signal }) => client.sessions.summaries(ids, { signal }),
    enabled: visible && connection.state === 'connected' && supported && ids.length > 0,
    refetchInterval: visible && connection.state === 'connected' ? 2000 : false,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    staleTime: 0,
    gcTime: 0,
  });
  const items = new Map(summaries.data?.items.map(item => [item.root_id, item]));
  useEffect(() => {
    if (!visible || connection.state !== 'connected' || !supported || !ids.length) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = client.onEvent(event => {
      if (!ids.includes(event.root_id) || !/^(?:turn\.|agent\.(?:turn\.|admitted|prompt\.queued|subtree\.)|root\.|permission\.|question\.)/.test(event.kind) || timer) return;
      timer = setTimeout(() => {
        timer = undefined;
        void runtime.queries.invalidateQueries({ queryKey: ['session-tab-summaries', runtimeId] });
      }, 250);
    });
    return () => { off(); clearTimeout(timer); };
  }, [client, connection.state, ids, runtime, runtimeId, supported, visible]);
  useEffect(() => {
    if (!summaries.data) return;
    runtime.tabs.titles(runtimeId, new Map(summaries.data.items.filter(item => !item.missing).map(item => [item.root_id, item.title])));
  }, [runtime, runtimeId, summaries.data]);
  const stale = !supported || connection.state !== 'connected' || !!summaries.error;
  const title = (id: string) => items.get(id)?.title || tabs.find(item => item.rootId === id)?.titleHint || 'Untitled session';
  const project = (id: string) => items.get(id)?.cwd?.split(/[\\/]/).filter(Boolean).at(-1) ?? '';
  const hasDraft = (id: string) => runtime.hasSessionDraft(runtimeId, id);
  const go = (rootId: string, replace = false) => {
    const tab = runtime.tabs.workspace(runtimeId).tabs.find(item => item.rootId === rootId);
    setPicker(false);
    void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId, rootId }, search: tab?.location ?? {}, replace });
  };
  const close = (rootIds: readonly string[], focusAfterMenu = false) => {
    const focused = document.activeElement;
    const restoreFocus = rootIds.some(id => document.getElementById(workspaceTabId(id))?.closest('[data-workspace-tab]')?.contains(focused));
    const next = runtime.tabs.close(runtimeId, rootIds, active);
    setNotice(rootIds.length === 1 ? 'Tab closed. Work and drafts are kept.' : 'Tabs closed. Work and drafts are kept.');
    if (next === null) void navigate({ to: '/', replace: true });
    else if (next) go(next, true);
    if (restoreFocus || focusAfterMenu || rootIds.includes(active ?? '')) requestAnimationFrame(() => {
      const remaining = runtime.tabs.workspace(runtimeId).tabs;
      const replacement = typeof next === 'string' ? next : remaining.find(tab => tab.rootId === active)?.rootId ?? remaining[0]?.rootId;
      const target = replacement && document.getElementById(workspaceTabId(replacement));
      if (target) target.focus({ preventScroll: true });
      else document.querySelector<HTMLButtonElement>('[aria-label^="Open sessions:"], [aria-label="New session tab"]')?.focus();
    });
  };
  const reopen = () => {
    try { const id = runtime.tabs.reopen(runtimeId); if (id) go(id); setNotice(''); }
    catch (error) { runtime.report(error); }
  };
  const actions = (id: string): MenuItem[] => {
    const index = tabs.findIndex(item => item.rootId === id);
    return [
      { id: 'close', label: 'Close tab', onSelect: () => close([id], true) },
      { id: 'others', label: 'Close other tabs', disabled: tabs.length < 2, onSelect: () => close(tabs.filter(item => item.rootId !== id).map(item => item.rootId), true) },
      { id: 'right', label: 'Close tabs to the right', disabled: index === tabs.length - 1, onSelect: () => close(tabs.slice(index + 1).map(item => item.rootId), true) },
      { id: 'separator', separator: true, label: '' },
      { id: 'left', label: 'Move left', disabled: index <= 0, onSelect: () => runtime.tabs.move(runtimeId, id, -1) },
      { id: 'move-right', label: 'Move right', disabled: index >= tabs.length - 1, onSelect: () => runtime.tabs.move(runtimeId, id, 1) },
      { id: 'link', label: 'Copy session link', onSelect: () => void runtime.platform.copy(new URL(`/h/${encodeURIComponent(runtimeId)}/s/${encodeURIComponent(id)}`, window.location.href).href).catch(error => runtime.report(error)) },
    ];
  };
  useImperativeHandle(ref, () => ({
    next(offset) { if (!tabs.length) return; const index = tabs.findIndex(item => item.rootId === active); go(tabs[index < 0 ? offset === 1 ? 0 : tabs.length - 1 : (index + offset + tabs.length) % tabs.length]!.rootId); },
    close() { if (active) close([active]); },
    reopen,
    showPicker() { setPicker(true); setSearch(''); },
  }));
  const add = <IconButton variant="ghost" label="New session tab" onClick={() => void navigate({ to: '/' })}><Plus size={17} /></IconButton>;
  const pickerButton = <Tooltip label="All open tabs"><Button variant="ghost" aria-label={`All open tabs (${tabs.length})`} onClick={() => { setSearch(''); setPicker(true); }}><ChevronDown size={15} /><span>{tabs.length}</span></Button></Tooltip>;
  return <>
    {compact ? <header {...stylex.props(styles.mobileBar)}>
      <Button variant="ghost" xstyle={styles.mobileSelector} aria-label={`Open sessions: ${active ? title(active) : 'New session'}, ${tabs.length} tabs`} onClick={() => { setSearch(''); setPicker(true); }}>
        {active && <Status item={items.get(active)} stale={stale} />}<span {...stylex.props(layout.ellipsis, layout.grow)}>{active ? title(active) : 'New session'}</span><span {...stylex.props(styles.count)}>{tabs.length}</span><ChevronDown size={14} />
      </Button>{add}{utilities}
    </header> : <WorkspaceTabs
      value={active && tabs.some(tab => tab.rootId === active) ? active : null}
      panelId="whip-session-panel"
      onClose={id => close([id])}
      onReorder={order => runtime.tabs.reorder(runtimeId, order)}
      utilities={<>{add}{pickerButton}{utilities}</>}
      items={tabs.map(tab => {
        const name = title(tab.rootId), item = items.get(tab.rootId);
        const duplicate = tabs.some(other => other.rootId !== tab.rootId && title(other.rootId) === name);
        const sameProject = tabs.some(other => other.rootId !== tab.rootId && title(other.rootId) === name && project(other.rootId) === project(tab.rootId));
        const label = duplicate ? `${name} · ${project(tab.rootId) || tab.rootId.slice(0, 6)}${sameProject && project(tab.rootId) ? ` ${tab.rootId.slice(0, 6)}` : ''}` : name;
        return {
          value: tab.rootId, label,
          accessibleLabel: `${label}. ${summaryDescription(item, stale)}${hasDraft(tab.rootId) ? '. Unsent draft' : ''}`,
          status: <Status item={item} stale={stale} />,
          metadata: hasDraft(tab.rootId) ? <Pencil size={11} aria-label="Unsent draft" /> : undefined,
          tooltip: <div {...stylex.props(layout.column)}><strong>{name}</strong><span>{item?.cwd || 'Project unavailable'}</span><span>{summaryDescription(item, stale)}</span>{item?.truncated && <span>Some session metadata is shortened.</span>}<span>{app.endpoint}</span></div>,
          render: <Link to="/h/$runtimeId/s/$rootId" params={{ runtimeId, rootId: tab.rootId }} search={tab.location} preload={false} />,
          wrap: (element: ReactElement) => <ContextMenu items={actions(tab.rootId)}>{element}</ContextMenu>,
        };
      })}
    />}
    <Sheet open={picker} onOpenChange={setPicker} title="Open sessions" description="Closing a tab leaves its session, drafts, and agents on the host.">
      <Input aria-label="Find an open session" placeholder="Find a session or project…" value={search} onChange={event => setSearch(event.target.value)} />
      <div {...stylex.props(styles.pickerList)}>
        {tabs.filter(tab => `${title(tab.rootId)} ${items.get(tab.rootId)?.cwd ?? ''}`.toLocaleLowerCase().includes(search.toLocaleLowerCase())).map(tab => <div key={tab.rootId} {...stylex.props(styles.pickerRow, active === tab.rootId && styles.selected)}>
          <button {...stylex.props(styles.pickerSelect)} onClick={() => go(tab.rootId)}><Status item={items.get(tab.rootId)} stale={stale} /><span {...stylex.props(layout.column, styles.pickerText)}><strong>{title(tab.rootId)}</strong><span {...stylex.props(layout.muted)}>{project(tab.rootId)}{project(tab.rootId) ? ' · ' : ''}{summaryDescription(items.get(tab.rootId), stale)}{hasDraft(tab.rootId) ? ' · Unsent draft' : ''}</span></span></button>
          <Menu trigger={<IconButton variant="ghost" label={`Tab actions for ${title(tab.rootId)}`}><MoreHorizontal size={15} /></IconButton>} items={actions(tab.rootId)} />
          <IconButton variant="ghost" label={`Close ${title(tab.rootId)}`} onClick={() => close([tab.rootId])}><X size={16} /></IconButton>
        </div>)}
        {!tabs.length && <p {...stylex.props(layout.muted)}>No open session tabs. Choose a saved session or start a new one.</p>}
      </div>
      <Button variant="ghost" disabled={!workspace?.closed.length} onClick={reopen}>Reopen closed tab</Button>
    </Sheet>
    {notice && <div role="status" {...stylex.props(styles.toast)}><span>{notice}</span><Button variant="ghost" onClick={reopen}>Reopen</Button><IconButton label="Dismiss tab notice" onClick={() => setNotice('')}><X size={13} /></IconButton></div>}
  </>;
}

const styles = stylex.create({
  running: { color: colors.primary }, attention: { color: colors.warning },
  mobileBar: { display: 'flex', alignItems: 'center', gap: 2, minHeight: 52, paddingInline: 4, borderBottomWidth: 1, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder, backgroundColor: surface.navigation, flexShrink: 0 },
  mobileSelector: { flex: 1, minWidth: 0, minHeight: 44, textAlign: 'start' },
  count: { fontSize: 11, color: surface.secondaryText, backgroundColor: colors.element, padding: '2px 5px', borderRadius: 4 },
  pickerList: { display: 'flex', flexDirection: 'column', gap: 4, overflowY: 'auto', maxHeight: '65dvh' },
  pickerRow: { display: 'flex', alignItems: 'center', minWidth: 0, borderRadius: 6 },
  selected: { backgroundColor: colors.element },
  pickerSelect: { display: 'flex', alignItems: 'center', gap: 12, flex: 1, minWidth: 0, minHeight: 60, padding: 12, textAlign: 'start', borderWidth: 0, borderStyle: 'none', borderRadius: 6, color: colors.foreground, backgroundColor: { default: 'transparent', ':hover': colors.hover }, cursor: 'pointer' },
  pickerText: { gap: 5, overflowWrap: 'anywhere', fontSize: 13 },
  toast: { position: 'fixed', bottom: { default: 20, [scale.phone]: 12 }, insetInlineStart: '50%', transform: 'translateX(-50%)', zIndex: 30, display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 8, backgroundColor: colors.panel, color: colors.foreground, fontSize: 12, boxShadow: '0 4px 24px #0002', maxWidth: 'calc(100vw - 24px)' },
});
