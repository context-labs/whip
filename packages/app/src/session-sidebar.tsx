import { WorkspaceExternalSource } from '@whip/ui/workspace-tabs';
import { ErrorNotice } from './error-feedback';
import { useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore, type Dispatch, type SetStateAction, type ReactNode, type RefObject } from 'react';
import { Link, useLocation, useNavigate } from '@tanstack/react-router';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { DeepReadonly, SessionListSnapshot } from '@whip/sdk/state';
import type { SessionNavigationSummary } from './session-status';
import { useQueries } from '@tanstack/react-query';
import { Button, IconButton, Menu, ContextMenu, Spinner, WhipcodeWordmark, Tooltip, Popover } from '@whip/ui';
import { Plus, Search, Settings2, Plug, ArrowUpRight, MoreHorizontal, ChevronRight, ChevronDown, Circle, Pin, MessageSquare, MessageSquareWarning, Archive, Folder, Globe, RotateCw } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { styles, sessionMarker, directoryMarker } from './session-sidebar.stylex';
import { layout } from './styles';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { sessionBusy, sessionNeedsInput } from './session-status';
import { mixedSidebarRows, sidebarRowKey, setDirectoryCollapsed, defaultDirectorySessionLimit, type SidebarState, type HostSidebarRow } from './sidebar-state';
import { openNewChat, sessionDestination } from './session-tab-routing';
import type { HostConnection } from './hosts';
import { useSessionActions } from './session-actions';
import { sessionSearch } from './session-tabs';

const noCollapsedDirectories: readonly string[] = [];
const sidebarRowGap = 1;

interface SidebarProps {
  headerAction?: ReactNode;
  /** Inset window chrome: the brand row doubles as the window drag strip. */
  inset?: boolean;
  state: SidebarState;
  setState: Dispatch<SetStateAction<SidebarState>>;
  onSearch(): void;
  onConnect(): void;
  onNavigate(): void;
}
export function SessionSidebar(props: SidebarProps) {
  const app = useAppState();
  const scroll = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const [scrolled, setScrolled] = useState(false);
  return <>
    <div data-sidebar-header {...stylex.props(styles.header)}>
      <SidebarHeader onNavigate={props.onNavigate} headerAction={props.headerAction} inset={props.inset} />
      <span aria-hidden="true" data-sidebar-scroll-edge {...stylex.props(styles.scrollEdge, scrolled && styles.scrollEdgeVisible)} />
    </div>
    <div ref={scroll} aria-label="Saved sessions" onScroll={event => setScrolled(event.currentTarget.scrollTop > 0)} {...stylex.props(layout.sessionList, styles.list)}>
      <div ref={content} {...stylex.props(styles.scrollContent)}>
        <nav aria-label="Main navigation" {...stylex.props(styles.destinations)}>
          <button onClick={props.onSearch} {...stylex.props(styles.destination, styles.primaryDestination)}><Search size={16} />Search sessions</button>
          <Link id="whip-settings-link" to="/settings" search={{ section: "general" }} onClick={props.onNavigate} {...stylex.props(styles.destination, styles.primaryDestination)}><Settings2 size={16} />Settings</Link>
        </nav>
        <section aria-label="Projects" {...stylex.props(styles.projects)}>
          <SessionRows hosts={app.hosts} {...props} scroll={scroll} content={content} />
        </section>
        <SidebarFooter onConnect={props.onConnect} />
      </div>
    </div>
  </>;
}
type SidebarScroll = { scroll: RefObject<HTMLDivElement | null>; content: RefObject<HTMLDivElement | null> };

function useSidebarActivity(hosts: readonly HostConnection[], rows: readonly HostSidebarRow[]) {
  const runtime = useRuntime();
  const [visible, setVisible] = useState(() => document.visibilityState !== 'hidden');
  useEffect(() => {
    const change = () => setVisible(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', change);
    return () => document.removeEventListener('visibilitychange', change);
  }, []);
  // Canonicalize the visible set so virtualizer renders don't restart subscriptions.
  const selection = JSON.stringify(hosts.flatMap(host => {
    const ids = rows.flatMap(row => row.kind === 'session' && row.runtimeId === host.runtimeId ? [row.session.id] : []).sort();
    return ids.length && host.client && host.runtimeId ? [[host.runtimeId, ids]] : [];
  }));
  const targets = useMemo(() => (JSON.parse(selection) as [string, string[]][]).map(([runtimeId, ids]) => {
    const host = hosts.find(host => host.runtimeId === runtimeId)!;
    return { runtimeId, ids, client: host.client!, enabled: visible && host.state === 'connected'
      && !!host.client!.getSnapshot().info?.negotiated_capabilities?.includes('session_summaries') };
  }), [hosts, selection, visible]);
  const summaries = useQueries({ queries: targets.map(({ runtimeId, ids, client, enabled }) => ({
    queryKey: ['session-sidebar-summaries', runtimeId, ids],
    queryFn: async ({ signal }: { signal: AbortSignal }) => {
      const items: SessionNavigationSummary[] = [];
      for (let offset = 0; offset < ids.length; offset += 32) {
        const page = await client.sessions.summaries(ids.slice(offset, offset + 32), { signal });
        items.push(...page.items);
      }
      return { items };
    },
    enabled,
    refetchInterval: enabled ? 2000 : false as const,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    staleTime: 0,
    gcTime: 0,
  })) });
  useEffect(() => {
    const releases = targets.filter(target => target.enabled).map(({ client, runtimeId, ids }) => {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const off = client.onEvent(event => {
        if (!ids.includes(event.root_id) || !/^(?:turn\.|agent\.(?:turn\.|admitted|prompt\.queued|subtree\.)|root\.|permission\.|question\.)/.test(event.kind) || timer) return;
        timer = setTimeout(() => {
          timer = undefined;
          void runtime.queries.invalidateQueries({ queryKey: ['session-sidebar-summaries', runtimeId] });
        }, 250);
      });
      return () => { off(); clearTimeout(timer); };
    });
    return () => releases.forEach(release => release());
  }, [runtime, targets]);
  const activity = new Map<string, SessionNavigationSummary>();
  summaries.forEach((summary, index) => {
    const target = targets[index]!;
    if (target.enabled && !summary.error) for (const item of summary.data?.items ?? []) {
      activity.set(sidebarRowKey('session', target.runtimeId, item.root_id), item);
    }
  });
  return activity;
}

function DirectoryHost({ host, cwd, hosts, onConnect }: { host: HostConnection; cwd: string; hosts: readonly HostConnection[]; onConnect(): void }) {
  const runtime = useRuntime();
  const connected = host.state === 'connected';
  const connecting = host.state === 'connecting' || host.state === 'reconnecting';
  const status = connected ? 'Connected' : connecting ? 'Reconnecting' : 'Offline';
  const name = hosts.some(other => other.id !== host.id && other.name === host.name) ? host.name + ' · ' + host.id : host.name;
  const identity = name + ' · ' + status;
  const target = host.profile?.target;
  const connection = target?.kind === 'ssh' ? [target.user, target.host].filter(Boolean).join('@') + (target.port ? ':' + target.port : '') : host.endpoint;
  return <Popover side="right" align="start" title={<span {...stylex.props(styles.hostHeading)}>
    <span {...stylex.props(styles.hostHeadingRow)}>
      <Globe size={14} aria-hidden="true" {...stylex.props(styles.detailIcon)} />
      <span {...stylex.props(layout.grow)}>{name}</span>{' '}
      <span {...stylex.props(styles.hostStatus)}>
        <Circle size={6} aria-hidden="true" fill={connected ? 'currentColor' : 'none'} {...stylex.props(styles.statusDot, connected && styles.connected)} />
        {status}
      </span>
    </span>
    {connection && connection !== host.name && <span {...stylex.props(styles.hostConnection)}>{' '}{connection}</span>}
  </span>} xstyle={styles.hostDetails} trigger={
    <button type="button" aria-label={identity + ' · ' + (cwd || 'Other sessions')} title={identity} {...stylex.props(styles.destination, styles.hostMetadata)}>
      <span {...stylex.props(layout.ellipsis)}>{name}</span>
      {!connected && <span {...stylex.props(styles.connectionLabel)}>{status}</span>}
      <Circle size={6} aria-hidden="true" fill={connected ? 'currentColor' : 'none'} {...stylex.props(styles.statusDot, connected && styles.connected)} />
    </button>
  }>
    <span {...stylex.props(styles.hostPath)}>
      <Folder size={14} aria-hidden="true" {...stylex.props(styles.detailIcon)} />
      <span>{cwd || 'Other sessions'}</span>
    </span>
    {host.error && <span role="status" {...stylex.props(styles.hostError)}>{host.error}</span>}
    {!connected && <Button variant="ghost" xstyle={styles.hostAction} aria-label={'Reconnect ' + host.name} disabled={connecting} onClick={() => void runtime.connections.connect(host.id).catch(() => {})}>
      <RotateCw size={14} aria-hidden="true" />Reconnect
    </Button>}
    <Button variant="ghost" xstyle={styles.hostAction} onClick={onConnect}><ArrowUpRight size={14} aria-hidden="true" />Manage servers</Button>
  </Popover>;
}

function HostFeedback({ host, catalog }: { host: HostConnection; catalog?: DeepReadonly<SessionListSnapshot> }) {
  const runtime = useRuntime();
  if (host.localRuntime?.state === 'missing') return null;
  if (!host.client || !host.list) return <Button variant="ghost" disabled={host.state === 'connecting'} onClick={() => {
    void runtime.connections.connect(host.id).catch(() => {});
  }}>{host.state === 'connecting' ? host.progress || 'Connecting ' + host.name + '…' : 'Connect ' + host.name}</Button>;
  const loading = catalog?.status === 'loading';
  const connected = host.state === 'connected';
  return <>
    {!catalog?.page && !catalog?.error && <p role="status" {...stylex.props(styles.feedback)}>{connected ? 'Loading sessions · ' : 'Sessions unavailable · '}{host.name}</p>}
    {catalog?.page?.has_more && <Button variant="ghost" disabled={loading || !connected} onClick={() => void host.list!.loadMore().catch(() => {})}>Load more sessions · {host.name}</Button>}
    {connected && catalog?.error && <ErrorNotice type="resource" owner={'sessions:' + host.runtimeId} error={catalog.error.message} title={'Could not load sessions · ' + host.name}
      action={<Button variant="ghost" disabled={loading} onClick={() => void host.list!.refresh().catch(() => {})}>Retry</Button>} />}
  </>;
}

function SidebarHeader({ onNavigate, headerAction, inset }: { onNavigate(): void; headerAction?: ReactNode; inset?: boolean; }) {
  const runtime = useRuntime();
  const navigate = useNavigate();
  // Inset window chrome (desktop): the brand row is an empty drag strip for
  // the traffic lights and the wordmark sits below it in the sidebar body,
  // aligned with the nav icons. Browsers keep it as the home link in the row.
  const wordmark = <WhipcodeWordmark aria-label="Whipcode" {...stylex.props(styles.wordmark)} />;
  return <>
    <div {...stylex.props(styles.brandRow, inset && styles.brandRowInset, inset && layout.windowDrag)}>
      {!inset && <Link to="/" search={{}} onClick={onNavigate} {...stylex.props(styles.wordmarkLink, layout.grow)}>{wordmark}</Link>}
      {inset ? <div {...stylex.props(styles.brandRowAction, layout.windowNoDrag)}>{headerAction}</div> : headerAction}
    </div>
    {inset && <div {...stylex.props(styles.wordmarkBelow)}>{wordmark}</div>}
    <nav aria-label="Create session">
      <Link to="/" search={{ new: 1 }} preload={false} onClick={event => {
        if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        if (openNewChat(runtime, navigate)) onNavigate();
      }} {...stylex.props(styles.destination, styles.primaryDestination)}><Plus size={16} />New session</Link>
    </nav>
  </>;
}
function SidebarFooter({ onConnect }: { onConnect(): void }) {
  return <div {...stylex.props(styles.footer)}>
    <button {...stylex.props(styles.destination, layout.grow)} onClick={onConnect} aria-label="Manage servers">
      <Plug size={16} /><span {...stylex.props(layout.ellipsis, layout.grow)}>Servers</span><ArrowUpRight size={13} />
    </button>
  </div>;
}
function SessionRows({ hosts, state, setState, onNavigate, onConnect, scroll, content }: SidebarProps & SidebarScroll & { hosts: readonly HostConnection[] }) {
  const runtime = useRuntime();
  const actions = useSessionActions();
  const [actionError, setActionError] = useState<{ owner: string; error: unknown }>();
  const navigate = useNavigate();
  useSessionTabs();
  // Same snapshot adapter as Search: observe SDK catalogs, never mirror them in state.
  const catalogs = useMemo(() => {
    let snapshot = hosts.map(host => host.list?.getSnapshot());
    return {
      getSnapshot: () => {
        const next = hosts.map(host => host.list?.getSnapshot());
        if (next.some((value, index) => value !== snapshot[index])) snapshot = next;
        return snapshot;
      },
      subscribe: (listener: () => void) => {
        const releases = hosts.map(host => host.list?.subscribe(listener));
        return () => releases.forEach(release => release?.());
      },
    };
  }, [hosts]);
  const snapshots = useSyncExternalStore(catalogs.subscribe, catalogs.getSnapshot, catalogs.getSnapshot);
  const [collapseNotice, setCollapseNotice] = useState('');
  const collapse = (runtimeId: string, cwd: string, closed: boolean) => {
    const next = setDirectoryCollapsed(state, runtimeId, cwd, closed);
    setState(next);
    if (closed && state.hosts.some(host => host.collapsed.some(path => !next.hosts.find(item => item.runtimeId === host.runtimeId)?.collapsed.includes(path)))) {
      setCollapseNotice('Older directory collapse preferences were reset to keep sidebar storage within its limit.');
    }
  };
  const container = useRef<HTMLDivElement>(null);
  const [scrollMargin, setScrollMargin] = useState(0);
  useEffect(() => {
    const measure = () => {
      const element = container.current, viewport = scroll.current;
      if (element && viewport) setScrollMargin(element.getBoundingClientRect().top - viewport.getBoundingClientRect().top + viewport.scrollTop);
    };
    measure();
    const observer = new ResizeObserver(measure);
    if (content.current) observer.observe(content.current);
    return () => observer.disconnect();
  }, [content, scroll]);
  const [hoveredDirectory, setHoveredDirectory] = useState<string>();
  const location = useLocation();
  const selected = sessionDestination(location.pathname);
  const [directoryLimits, setDirectoryLimits] = useState<ReadonlyMap<string, ReadonlyMap<string, number>>>(() => new Map());
  const setDirectoryLimit = (runtimeId: string, cwd: string, limit: number) => setDirectoryLimits(previous => {
    const limits = new Map(previous.get(runtimeId));
    limits.delete(cwd);
    if (limit > defaultDirectorySessionLimit) limits.set(cwd, limit);
    const next = new Map([...previous].filter(([id]) => hosts.some(host => host.runtimeId === id && host.list)));
    next.set(runtimeId, new Map([...limits].slice(-64)));
    return next;
  });
  useEffect(() => {
    setDirectoryLimits(previous => {
      const next = new Map([...previous].filter(([id]) => hosts.some(host => host.runtimeId === id && host.list)));
      return next.size === previous.size ? previous : next;
    });
  }, [hosts]);
  const sources = useMemo(() => hosts.flatMap((host, index) => host.runtimeId && host.list ? [{
    runtimeId: host.runtimeId, items: snapshots[index]?.page?.items ?? [],
    collapsed: state.hosts.find(item => item.runtimeId === host.runtimeId)?.collapsed ?? noCollapsedDirectories,
    limits: directoryLimits.get(host.runtimeId),
  }] : []), [hosts, snapshots, state.hosts, directoryLimits]);
  const rows = useMemo(() => mixedSidebarRows(sources), [sources]);
  const [touch, setTouch] = useState(() => window.matchMedia('(pointer: coarse), (max-width: 767px)').matches);
  useEffect(() => {
    const media = window.matchMedia('(pointer: coarse), (max-width: 767px)');
    const update = () => setTouch(media.matches);
    media.addEventListener('change', update); return () => media.removeEventListener('change', update);
  }, []);
  const rowHeight = (index: number) => rows[index]?.kind === 'directory' ? (touch ? 48 : 36) : touch ? 44 : 28;
  const offsets = useMemo(() => {
    let offset = 0;
    return new Map(rows.map((row, index) => { const start = offset; offset += rowHeight(index) + sidebarRowGap; return [row.key, start]; }));
  }, [rows, touch]);
  const virtual = useVirtualizer({ count: rows.length, getScrollElement: () => scroll.current, scrollMargin, estimateSize: rowHeight, gap: sidebarRowGap, overscan: 5, getItemKey: index => rows[index]!.key });
  const visibleRows = virtual.getVirtualItems();
  const activity = useSidebarActivity(hosts, visibleRows.map(item => rows[item.index]!));
  // Virtual rows can move beneath a stationary pointer during scroll or refresh.
  useLayoutEffect(() => {
    setHoveredDirectory(container.current?.querySelector<HTMLElement>('[data-sidebar-group]:hover')?.dataset.sidebarGroup);
  }, [visibleRows, rows]);
  const anchor = useRef<{ key: string; group?: string; offset: number } | null>(null);
  const rememberAnchor = () => {
    const top = scroll.current?.scrollTop ?? 0;
    const visible = virtual.getVirtualItems().find(row => row.end > top);
    const row = visible && rows[visible.index];
    anchor.current = row && visible && visible.start <= top ? { key: row.key, group: row.directoryKey, offset: top - visible.start } : null;
  };
  const onScroll = useRef(rememberAnchor);
  useLayoutEffect(() => { onScroll.current = rememberAnchor; });
  useEffect(() => {
    const element = scroll.current;
    // Keep this listener attached: the virtualizer can synchronously render during
    // the same scroll event, before later native listeners have been invoked.
    const update = () => { onScroll.current(); setHoveredDirectory(container.current?.querySelector<HTMLElement>('[data-sidebar-group]:hover')?.dataset.sidebarGroup); };
    element?.addEventListener('scroll', update);
    return () => element?.removeEventListener('scroll', update);
  }, [scroll]);
  useLayoutEffect(() => {
    virtual.measure();
    const saved = anchor.current;
    const start = saved && (offsets.get(saved.key) ?? offsets.get(saved.group ?? ''));
    if (start !== undefined && start !== null) virtual.scrollToOffset(scrollMargin + start + saved!.offset);
  }, [offsets, scrollMargin, virtual]);
  // Only route changes request a reveal. Polling must not undo a user's collapse.
  const pendingReveal = useRef<ReturnType<typeof sessionDestination>>(undefined);
  useLayoutEffect(() => { pendingReveal.current = sessionDestination(location.pathname); }, [location.pathname]);
  useLayoutEffect(() => {
    const target = pendingReveal.current;
    if (!target) return;
    const source = sources.find(source => source.runtimeId === target.runtimeId);
    const session = source?.items.find(item => item.id === target.rootId);
    if (!source || !session) return;
    if (source.collapsed.includes(session.cwd)) { collapse(target.runtimeId, session.cwd, false); return; }
    const index = rows.findIndex(row => row.kind === 'session' && row.runtimeId === target.runtimeId && row.session.id === target.rootId);
    if (index < 0) {
      const position = source.items.filter(item => item.cwd === session.cwd).findIndex(item => item.id === target.rootId);
      setDirectoryLimit(target.runtimeId, session.cwd, Math.ceil((position + 1) / defaultDirectorySessionLimit) * defaultDirectorySessionLimit);
      return;
    }
    virtual.scrollToIndex(index, { align: 'auto' });
    pendingReveal.current = undefined;
  }, [location.pathname, sources, rows, virtual]);
  return <div ref={container}
    onPointerOver={event => { if (event.pointerType !== 'touch') setHoveredDirectory((event.target as Element).closest<HTMLElement>('[data-sidebar-group]')?.dataset.sidebarGroup); }}
    onPointerLeave={() => setHoveredDirectory(undefined)}>
    <div style={{ height: virtual.getTotalSize(), position: 'relative' }}>
      {visibleRows.map(row => {
        const item = rows[row.index]!;
        const runtimeId = item.runtimeId;
        const host = hosts.find(host => host.runtimeId === runtimeId)!;
        const collapsed = state.hosts.find(host => host.runtimeId === runtimeId)?.collapsed ?? noCollapsedDirectories;
        if (item.kind === 'more') return <button key={item.key} type="button" data-sidebar-cwd={item.cwd} data-sidebar-runtime={runtimeId} data-sidebar-group={item.directoryKey}
          aria-label={`${item.expanded ? 'Less' : 'More'} sessions in ${item.cwd || 'Other sessions'}`} aria-expanded={item.visibleCount > defaultDirectorySessionLimit}
          onClick={() => setDirectoryLimit(runtimeId, item.cwd, item.expanded ? defaultDirectorySessionLimit : item.visibleCount + defaultDirectorySessionLimit)}
          style={{ position: 'absolute', width: '100%', top: 0, height: row.size, transform: `translateY(${row.start - scrollMargin}px)` }}
          {...stylex.props(styles.destination, styles.moreButton)}>
          {item.expanded ? <ChevronRight size={12} aria-hidden="true" /> : <ChevronDown size={12} aria-hidden="true" />}
          {item.expanded ? 'Less' : 'More'}
        </button>;
        if (item.kind === 'directory') return <div key={item.key} data-sidebar-directory={item.cwd} data-sidebar-cwd={item.cwd} data-sidebar-runtime={runtimeId} data-sidebar-group={item.directoryKey}
          style={{ position: 'absolute', width: '100%', top: 0, height: row.size, transform: `translateY(${row.start - scrollMargin}px)` }} {...stylex.props(styles.group, directoryMarker)}>
          <Tooltip label={host.name + ' · ' + (item.cwd || 'Other sessions')}><button title={item.cwd || 'Other sessions'} aria-label={item.cwd || 'Other sessions'} aria-description={host.name} aria-expanded={!collapsed.includes(item.cwd)}
            onClick={() => collapse(runtimeId, item.cwd, !collapsed.includes(item.cwd))} {...stylex.props(styles.destination, styles.groupButton, layout.grow)}>
            <span {...stylex.props(layout.ellipsis)}>{item.label}</span><span data-directory-caret {...stylex.props(styles.caret, hoveredDirectory === item.directoryKey && styles.revealed)}>{collapsed.includes(item.cwd) ? <ChevronRight size={12} /> : <ChevronDown size={12} />}</span>
          </button></Tooltip>
          {(!host.local || host.state !== 'connected') && <DirectoryHost host={host} cwd={item.cwd} hosts={hosts} onConnect={onConnect} />}
          {item.cwd && <Link to="/" search={{ new: 1, cwd: item.cwd, runtimeId }} preload={false} onClick={event => {
            if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
            event.preventDefault();
            if (openNewChat(runtime, navigate, { cwd: item.cwd, runtimeId })) onNavigate();
          }}
            aria-label={`New session in ${item.cwd}`} title={`New session in ${item.cwd}`} {...stylex.props(styles.destination, styles.icon)}><Plus size={14} /></Link>}
        </div>;
        const session = item.session;
        const saved = runtime.tabs.preferred(runtimeId, session.id);
        const active = selected?.runtimeId === runtimeId && selected.rootId === session.id;
        const menuItems = actions.items({ runtimeId, rootId: session.id, title: session.title, archived: session.archived });
        return <div key={item.key} data-sidebar-session={session.id} data-sidebar-cwd={session.cwd} data-sidebar-runtime={runtimeId} data-sidebar-group={item.directoryKey}
          style={{ position: 'absolute', width: '100%', top: 0, height: row.size, transform: `translateY(${row.start - scrollMargin}px)` }}>
          <ContextMenu items={menuItems} onOpenChange={actions.prepare}><div {...stylex.props(styles.sessionRow, sessionMarker, active && styles.selected)}>
            <WorkspaceExternalSource id={`sidebar:${JSON.stringify([runtimeId, session.id])}`}
              data={{ runtimeId, rootId: session.id, titleHint: session.title }}
              label={session.title || 'Untitled session'} status={<MessageSquare size={13}/>} disabled={touch || !runtimeId}>
              {sourceProps => <Link {...sourceProps} to="/h/$runtimeId/s/$rootId" params={{ runtimeId, rootId: session.id }} search={sessionSearch(saved)} state={{ whipViewId: saved?.id }} preload={false}
              aria-current={active ? 'page' : undefined} title={`${session.title || 'Untitled session'}\n${session.cwd}`}
              onClick={event => {
                if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                try { runtime.tabs.open(runtimeId, session.id, session.title); onNavigate(); }
                catch (error) { event.preventDefault(); setActionError({ owner: `${runtimeId}:${session.id}`, error }); }
              }} {...stylex.props(styles.sessionLink)}>
              <span {...stylex.props(styles.indicator)}>{session.pinned ? <Pin size={12} aria-label="Pinned" />
                : sessionNeedsInput(activity.get(item.key)) ? <MessageSquareWarning size={12} {...stylex.props(styles.attention)} aria-label="Needs your input" />
                : sessionBusy(activity.get(item.key)) ? <Spinner size={10} label="Session is busy" />
                : <Circle size={5} aria-hidden="true" />}</span>
              <span {...stylex.props(layout.grow)}><span {...stylex.props(styles.title, layout.ellipsis)}>{session.title || 'Untitled session'}</span>
              </span>
            </Link>}
            </WorkspaceExternalSource>
            {!session.archived && <IconButton variant="ghost" label={`Archive ${session.title || 'Untitled session'}`} title="Archive chat"
              onClick={event => { event.preventDefault(); event.stopPropagation(); void actions.archive({ runtimeId, rootId: session.id, title: session.title, archived: session.archived }, true); }}
              xstyle={[styles.icon, styles.sessionMenu]}><Archive size={14} /></IconButton>}
            <Menu trigger={<IconButton variant="ghost" label={`Actions for ${session.title || 'Untitled session'}`} xstyle={[styles.icon, styles.sessionMenu]}><MoreHorizontal size={14} /></IconButton>} items={menuItems} onOpenChange={actions.prepare} />
          </div></ContextMenu>
        </div>;
      })}
    </div>
    {collapseNotice && <p role="status" {...stylex.props(styles.feedback)}>{collapseNotice}</p>}
    {!rows.length && hosts.length > 0 && hosts.every((host, index) => host.state === 'connected' && snapshots[index]?.status === 'live') && <p {...stylex.props(styles.feedback)}>No saved sessions yet.</p>}
    {hosts.map((host, index) => <HostFeedback key={host.id} host={host} catalog={snapshots[index]} />)}
    {actionError && <ErrorNotice type="action" owner={actionError.owner} error={actionError.error} title="Could not open session" onDismiss={() => setActionError(undefined)} />}
  </div>;
}
