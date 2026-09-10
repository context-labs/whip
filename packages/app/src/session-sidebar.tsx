import { useEffect, useLayoutEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction, type ReactNode, type RefObject } from 'react';
import { Link, useLocation, useNavigate } from '@tanstack/react-router';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useWhipConnection, useSessionListView } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import type { DeepReadonly, SessionListView } from '@whip/sdk/state';
import type { SessionCatalogPage } from '@whip/protocol';
import { useQuery } from '@tanstack/react-query';
import { Button, IconButton, Menu, ContextMenu, Spinner, WhipcodeWordmark } from '@whip/ui';
import { Archive, Plus, Search, Settings2, Plug, ArrowUpRight, MoreHorizontal, ChevronRight, ChevronDown, Circle, Pin, MessageSquareWarning } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { styles, sessionMarker, directoryMarker } from './session-sidebar.stylex';
import { layout } from './styles';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { sessionBusy, sessionNeedsInput } from './session-status';
import { sidebarRows, setDirectoryCollapsed, type SidebarState } from './sidebar-state';
import { openNewChat, sessionDestination } from './session-tab-routing';
import type { HostConnection } from './hosts';
import { useSessionActions } from './session-actions';
import { sessionSearch } from './session-tabs';

const noCollapsedDirectories: readonly string[] = [];

interface SidebarProps {
  headerAction?: ReactNode;
  /** Inset window chrome: the brand row doubles as the window drag strip. */
  inset?: boolean;
  state: SidebarState;
  setState: Dispatch<SetStateAction<SidebarState>>;
  onSearch(status?: 'active' | 'archived' | 'all'): void;
  onConnect(): void;
  onNavigate(): void;
}
export function SessionSidebar(props: SidebarProps) {
  const app = useAppState();
  const scroll = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  return <>
    <SidebarDestinations onNavigate={props.onNavigate} onSearch={props.onSearch} headerAction={props.headerAction} inset={props.inset} />
    <div ref={scroll} aria-label="Saved sessions" {...stylex.props(layout.sessionList, styles.list)}>
      <div ref={content} {...stylex.props(styles.hosts)}>
        {app.hosts.map(host => <HostSection key={host.id} host={host} {...props} scroll={scroll} content={content} />)}
      </div>
    </div>
    <SidebarFooter onConnect={props.onConnect} />
  </>;
}
type SidebarScroll = { scroll: RefObject<HTMLDivElement | null>; content: RefObject<HTMLDivElement | null> };
function HostSection({ host, ...props }: SidebarProps & SidebarScroll & { host: HostConnection }) {
  const runtime = useRuntime();
  const [collapsed, setCollapsed] = useState(false);
  return <section aria-label={`${host.name} sessions`} {...stylex.props(styles.host, collapsed && styles.collapsedHost)}>
    <button {...stylex.props(styles.destination, styles.hostHeading)} aria-expanded={!collapsed} title={host.endpoint} onClick={() => setCollapsed(value => !value)}>
      {collapsed ? <ChevronRight size={13} /> : <ChevronDown size={13} />}<strong {...stylex.props(layout.grow, layout.ellipsis)}>{host.name}</strong>
      <span {...stylex.props(layout.muted)}>{host.state === 'closed' ? 'Offline' : host.state === 'connected' ? '' : host.state}</span>
    </button>
    {!collapsed && <>
      {host.error && <p role="status" {...stylex.props(layout.muted)}>{host.error}</p>}
      {host.client && host.list ? <HostSidebar {...props} client={host.client} list={host.list} />
        : <Button variant="ghost" onClick={() => void runtime.connections.connect(host.id).catch(() => {})}>Connect {host.name}</Button>}
    </>}
  </section>;
}
function SidebarDestinations({ onNavigate, onSearch, headerAction, inset }: { onNavigate(): void; onSearch(status?: 'active' | 'archived' | 'all'): void; headerAction?: ReactNode; inset?: boolean; }) {
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
    <nav aria-label="Main navigation" {...stylex.props(styles.destinations)}>
      <Link to="/" search={{ new: 1 }} preload={false} onClick={event => {
        if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        if (openNewChat(runtime, navigate)) onNavigate();
      }} {...stylex.props(styles.destination, styles.primaryDestination)}><Plus size={16} />New session</Link>
      <button onClick={() => onSearch()} {...stylex.props(styles.destination, styles.primaryDestination)}><Search size={16} />Search sessions</button>
      <button onClick={() => onSearch('archived')} {...stylex.props(styles.destination, styles.primaryDestination)}><Archive size={16} />Archived sessions</button>
      <Link id="whip-settings-link" to="/settings" onClick={onNavigate} {...stylex.props(styles.destination, styles.primaryDestination)}><Settings2 size={16} />Settings</Link>
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
function HostSidebar({ client, list, state, setState, onNavigate, scroll, content }: SidebarProps & SidebarScroll & { client: WhipClient; list: SessionListView }) {
  const runtime = useRuntime();
  const catalog = useSessionListView(list);
  const runtimeId = client.getSnapshot().info?.runtime_id ?? '';
  const collapsed = state.hosts.find(host => host.runtimeId === runtimeId)?.collapsed ?? noCollapsedDirectories;
  const collapse = (cwd: string, closed: boolean) => {
    const next = setDirectoryCollapsed(state, runtimeId, cwd, closed);
    setState(next);
    if (closed && state.hosts.some(host => host.collapsed.some(path => !next.hosts.find(item => item.runtimeId === host.runtimeId)?.collapsed.includes(path)))) {
      runtime.report('Older directory collapse preferences were reset to keep sidebar storage within its limit.');
    }
  };
  return <>
      <SessionRows client={client} scroll={scroll} content={content} page={catalog.page} loading={catalog.status === 'loading'} error={catalog.error?.message}
        collapsed={collapsed} onCollapse={collapse}
        onNavigate={onNavigate} loadMore={() => void list.loadMore().catch(error => runtime.report(error))} />
  </>;
}
function SessionRows({ client, page, loading, error, onNavigate, loadMore, scroll, content, collapsed = noCollapsedDirectories, onCollapse }: SidebarScroll & {
  client: WhipClient;
  page?: DeepReadonly<SessionCatalogPage>;
  loading: boolean;
  error?: string;
  onNavigate(): void;
  loadMore(): void;
  collapsed?: readonly string[];
  onCollapse?(cwd: string, closed: boolean): void;
}) {
  const runtime = useRuntime();
  const actions = useSessionActions();
  const navigate = useNavigate();
  useSessionTabs();
  const connection = useWhipConnection(client);
  const runtimeId = connection.info?.runtime_id ?? '';
  const container = useRef<HTMLDivElement>(null);
  const [scrollMargin, setScrollMargin] = useState(0);
  useLayoutEffect(() => {
    const measure = () => setScrollMargin(container.current?.offsetTop ?? 0);
    measure();
    const observer = new ResizeObserver(measure);
    if (content.current) observer.observe(content.current);
    return () => observer.disconnect();
  }, [content]);
  const [hoveredDirectory, setHoveredDirectory] = useState<string>();
  const location = useLocation();
  const selected = sessionDestination(location.pathname);
  const items = page?.items;
  const rows = useMemo(() => sidebarRows(items ?? [], collapsed), [items, collapsed]);
  const [touch, setTouch] = useState(() => window.matchMedia('(pointer: coarse), (max-width: 767px)').matches);
  useEffect(() => {
    const media = window.matchMedia('(pointer: coarse), (max-width: 767px)');
    const update = () => setTouch(media.matches);
    media.addEventListener('change', update); return () => media.removeEventListener('change', update);
  }, []);
  const rowHeight = (index: number) => rows[index]?.kind === 'directory' ? (touch ? 48 : 36) : touch ? 44 : 28;
  const offsets = useMemo(() => {
    let offset = 0;
    return new Map(rows.map((row, index) => { const start = offset; offset += rowHeight(index); return [row.key, start]; }));
  }, [rows, touch]);
  const virtual = useVirtualizer({ count: rows.length, getScrollElement: () => scroll.current, scrollMargin, estimateSize: rowHeight, overscan: 5, getItemKey: index => rows[index]!.key });
  const visibleRows = virtual.getVirtualItems();
  const ids = useMemo(() => visibleRows.flatMap(item => {
    const row = rows[item.index];
    return row?.kind === 'session' ? [row.session.id] : [];
  }).sort(), [visibleRows, rows]);
  const [visible, setVisible] = useState(() => document.visibilityState !== 'hidden');
  useEffect(() => { const change = () => setVisible(document.visibilityState !== 'hidden'); document.addEventListener('visibilitychange', change); return () => document.removeEventListener('visibilitychange', change); }, []);
  const supported = connection.info?.negotiated_capabilities?.includes('session_summaries') ?? false;
  const summaries = useQuery({
    queryKey: ['session-sidebar-summaries', runtimeId, ids],
    queryFn: async ({ signal }) => {
      const items = [];
      for (let offset = 0; offset < ids.length; offset += 32) {
        const page = await client.sessions.summaries(ids.slice(offset, offset + 32), { signal });
        items.push(...page.items);
      }
      return { items };
    },
    enabled: visible && connection.state === 'connected' && supported && ids.length > 0,
    refetchInterval: visible && connection.state === 'connected' ? 2000 : false,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    staleTime: 0,
    gcTime: 0,
  });
  const activity = new Map(summaries.data?.items.map(item => [item.root_id, item]));
  const activityStale = !supported || connection.state !== 'connected' || !!summaries.error;
  useEffect(() => {
    if (!visible || connection.state !== 'connected' || !supported || !ids.length) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = client.onEvent(event => {
      if (!ids.includes(event.root_id) || !/^(?:turn\.|agent\.(?:turn\.|admitted|prompt\.queued|subtree\.)|root\.|permission\.|question\.)/.test(event.kind) || timer) return;
      timer = setTimeout(() => {
        timer = undefined;
        void runtime.queries.invalidateQueries({ queryKey: ['session-sidebar-summaries', runtimeId] });
      }, 250);
    });
    return () => { off(); clearTimeout(timer); };
  }, [client, connection.state, ids, runtime, runtimeId, supported, visible]);
  // Virtual rows can move beneath a stationary pointer during scroll or refresh.
  useLayoutEffect(() => {
    setHoveredDirectory(container.current?.querySelector<HTMLElement>('[data-sidebar-cwd]:hover')?.dataset.sidebarCwd);
  }, [visibleRows, rows]);
  const anchor = useRef<{ key: string; group?: string; offset: number } | null>(null);
  const rememberAnchor = () => {
    const top = scroll.current?.scrollTop ?? 0;
    const visible = virtual.getVirtualItems().find(row => row.end > top);
    const row = visible && rows[visible.index];
    anchor.current = row && visible && visible.start <= top ? { key: row.key, group: row.kind === 'session' ? `directory:${row.session.cwd}` : undefined, offset: top - visible.start } : null;
  };
  useEffect(() => {
    const element = scroll.current;
    const update = () => { rememberAnchor(); setHoveredDirectory(container.current?.querySelector<HTMLElement>('[data-sidebar-cwd]:hover')?.dataset.sidebarCwd); };
    element?.addEventListener('scroll', update);
    return () => element?.removeEventListener('scroll', update);
  });
  useLayoutEffect(() => {
    virtual.measure();
    const saved = anchor.current;
    const start = saved && (offsets.get(saved.key) ?? offsets.get(saved.group ?? ''));
    if (start !== undefined && start !== null) virtual.scrollToOffset(scrollMargin + start + saved!.offset);
  }, [offsets, scrollMargin, virtual]);
  // Only route changes request a reveal. Polling must not undo a user's collapse.
  const pendingReveal = useRef<string | undefined>(undefined);
  useLayoutEffect(() => { pendingReveal.current = selected?.runtimeId === runtimeId ? selected.rootId : undefined; }, [location.pathname, runtimeId]);
  useLayoutEffect(() => {
    const id = pendingReveal.current;
    if (!id) return;
    const session = items?.find(item => item.id === id);
    if (!session) return;
    if (collapsed.includes(session.cwd)) { onCollapse?.(session.cwd, false); return; }
    const index = rows.findIndex(row => row.kind === 'session' && row.session.id === id);
    if (index < 0) return;
    virtual.scrollToIndex(index, { align: 'auto' });
    pendingReveal.current = undefined;
    rememberAnchor();
  });
  return <div ref={container}
    onPointerOver={event => { if (event.pointerType !== 'touch') setHoveredDirectory((event.target as Element).closest<HTMLElement>('[data-sidebar-cwd]')?.dataset.sidebarCwd); }}
    onPointerLeave={() => setHoveredDirectory(undefined)}>
    <div style={{ height: virtual.getTotalSize(), position: 'relative' }}>
      {visibleRows.map(row => {
        const item = rows[row.index]!;
        if (item.kind === 'directory') return <div key={item.key} data-sidebar-directory={item.cwd} data-sidebar-cwd={item.cwd}
          style={{ position: 'absolute', width: '100%', top: 0, height: row.size, transform: `translateY(${row.start - scrollMargin}px)` }} {...stylex.props(styles.group, directoryMarker)}>
          <button title={item.cwd || 'Other sessions'} aria-label={item.cwd || 'Other sessions'} aria-expanded={!collapsed.includes(item.cwd)}
            onClick={() => onCollapse?.(item.cwd, !collapsed.includes(item.cwd))} {...stylex.props(styles.destination, styles.groupButton, layout.grow)}>
            <span {...stylex.props(layout.ellipsis)}>{item.label}</span><span data-directory-caret {...stylex.props(styles.caret, hoveredDirectory === item.cwd && styles.revealed)}>{collapsed.includes(item.cwd) ? <ChevronRight size={12} /> : <ChevronDown size={12} />}</span>
          </button>
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
        return <div key={item.key} data-sidebar-session={session.id} data-sidebar-cwd={session.cwd}
          style={{ position: 'absolute', width: '100%', top: 0, height: row.size, transform: `translateY(${row.start - scrollMargin}px)` }}>
          <ContextMenu items={menuItems} onOpenChange={actions.prepare}><div {...stylex.props(styles.sessionRow, sessionMarker, active && styles.selected)}>
            <Link to="/h/$runtimeId/s/$rootId" params={{ runtimeId, rootId: session.id }} search={sessionSearch(saved)} state={{ whipViewId: saved?.id }} preload={false}
              aria-current={active ? 'page' : undefined} title={`${session.title || 'Untitled session'}\n${session.cwd}`}
              onClick={event => {
                if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                try { runtime.tabs.open(runtimeId, session.id, session.title); onNavigate(); }
                catch (error) { event.preventDefault(); runtime.report(error); }
              }} {...stylex.props(styles.sessionLink)}>
              <span {...stylex.props(styles.indicator)}>{session.pinned ? <Pin size={12} aria-label="Pinned" />
                : sessionNeedsInput(activity.get(session.id), activityStale) ? <MessageSquareWarning size={12} {...stylex.props(styles.attention)} aria-label="Needs your input" />
                : sessionBusy(activity.get(session.id), activityStale) ? <Spinner size={10} label="Session is busy" />
                : <Circle size={5} aria-hidden="true" />}</span>
              <span {...stylex.props(layout.grow)}><span {...stylex.props(styles.title, layout.ellipsis)}>{session.title || 'Untitled session'}</span>
              </span>
            </Link>
            <Menu trigger={<IconButton variant="ghost" label={`Actions for ${session.title || 'Untitled session'}`} xstyle={[styles.icon, styles.sessionMenu]}><MoreHorizontal size={14} /></IconButton>} items={menuItems} onOpenChange={actions.prepare} />
          </div></ContextMenu>
        </div>;
      })}
    </div>
    {!items?.length && <p {...stylex.props(layout.muted)}>{loading ? 'Loading sessions…' : 'No saved sessions yet.'}</p>}
    {page?.has_more && <Button variant="ghost" disabled={loading} onClick={loadMore}>Load more sessions</Button>}
    {error && <p role="alert">{error}</p>}
  </div>;
}
