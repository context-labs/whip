import { ErrorNotice } from './error-feedback';
import { typography } from '@whip/ui/tokens.stylex';
import { useEffect, useId, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import { Link } from '@tanstack/react-router';
import { useQueries } from '@tanstack/react-query';
import type { SessionCatalogPage } from '@whip/protocol';
import { Button, ContextMenu, Dialog, IconButton, Input, Menu, Select, type DialogProps } from '@whip/ui';
import { Code2, CornerDownLeft, MoreHorizontal, Search } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface } from '@whip/ui/tokens.stylex';
import { styles as sidebarStyles, sessionMarker } from './session-sidebar.stylex';
import { layout } from './styles';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { useSessionActions } from './session-actions';
import { sessionSearch } from './session-tabs';

type Props = { initialStatus?: 'active' | 'archived' | 'all'; open: boolean; onOpenChange(open: boolean): void; finalFocus: DialogProps['finalFocus'] };
export function SessionSearchDialog(props: Props) {
  return props.open ? <SearchDialog {...props} /> : null;
}
function SearchDialog({ onOpenChange, finalFocus, initialStatus = 'active' }: Props) {
  const runtime = useRuntime();
  const actions = useSessionActions();
  const [actionError, setActionError] = useState<{ owner: string; error: unknown }>();
  const [status, setStatus] = useState(initialStatus);
  const filtered = status !== 'active';
  const { hosts } = useAppState();
  useSessionTabs();
  const [filter, setFilter] = useState('');
  const [search, setSearch] = useState('');
  const [term, setTerm] = useState('');
  const [cursors, setCursors] = useState<Record<string, SessionCatalogPage['next_cursor']>>({});
  const [selected, setSelected] = useState<string>();
  const input = useRef<HTMLInputElement>(null);
  const results = useRef<HTMLDivElement>(null);
  const resultId = useId();
  const visibleHosts = useMemo(() => hosts.filter(host => !filter || host.id === filter), [hosts, filter]);
  // Recent results observe the existing catalogs; opening search creates no second list poller.
  const catalogs = useMemo(() => {
    let snapshot = visibleHosts.map(host => host.list?.getSnapshot());
    return {
      getSnapshot: () => {
        const next = visibleHosts.map(host => host.list?.getSnapshot());
        if (next.some((value, index) => value !== snapshot[index])) snapshot = next;
        return snapshot;
      },
      subscribe: (listener: () => void) => {
        const releases = visibleHosts.map(host => host.list?.subscribe(listener));
        return () => releases.forEach(release => release?.());
      },
    };
  }, [visibleHosts]);
  const recent = useSyncExternalStore(catalogs.subscribe, catalogs.getSnapshot, catalogs.getSnapshot);
  const revisions = JSON.stringify(visibleHosts.map((host, index) => [host.runtimeId, recent[index]?.page?.revision]));
  const previousRevisions = useRef(new Map<string, string>());
  useEffect(() => {
    const current = new Map<string, string>(JSON.parse(revisions));
    const changed = new Set([...current].filter(([id, revision]) => revision && previousRevisions.current.has(id) && previousRevisions.current.get(id) !== revision).map(([id]) => id));
    previousRevisions.current = current;
    if (!changed.size) return;
    // Reuse catalog observation; other clients invalidate only their host's
    // on-demand search and cursors, without a second background list poller.
    setCursors(cursors => Object.fromEntries(Object.entries(cursors).filter(([id]) => !changed.has(id))));
    void runtime.queries.invalidateQueries({ predicate: query => query.queryKey[0] === 'session-search' && changed.has(query.queryKey[1] as string) });
  }, [runtime, revisions]);
  useEffect(() => {
    const timer = setTimeout(() => { setTerm(search.trim()); setCursors({}); setSelected(undefined); }, 200);
    return () => clearTimeout(timer);
  }, [search]);
  const queries = useQueries({ queries: visibleHosts.map(host => ({
    queryKey: ['session-search', host.runtimeId, term, status, cursors[host.runtimeId ?? '']],
    queryFn: ({ signal }: { signal: AbortSignal }) => host.client!.sessions.list({ search: term, status, cursor: cursors[host.runtimeId!], limit: 64, max_bytes: 256 << 10 }, { signal }),
    enabled: (!!term || filtered) && !!host.client && host.state === 'connected',
    gcTime: 0,
  })) });
  const groups = visibleHosts.map((host, index) => {
    const query = queries[index]!;
    const catalog = recent[index];
    const connected = !!host.client && host.state === 'connected';
    const page = connected ? ((term || filtered) ? query.data : catalog?.page) : undefined;
    return {
      host, query, page, connected,
      items: connected ? (page?.items ?? []).slice(0, 64) : [],
      loading: connected && ((term || filtered) ? query.isFetching : catalog?.status === 'loading'),
      error: connected ? ((term || filtered) ? query.error?.message : catalog?.error?.message) : undefined,
    };
  });
  const items = groups.flatMap(group => group.items.map(session => ({ session, host: group.host, key: JSON.stringify([group.host.runtimeId, session.id]) })));
  const loading = groups.some(group => group.loading);
  const current = Math.max(0, items.findIndex(item => item.key === selected));
  const settling = search.trim() !== term;
  const active = items[current];
  useEffect(() => {
    results.current?.querySelector<HTMLElement>(`[data-search-index="${current}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [current]);
  const close = () => onOpenChange(false);
  const cursor = (runtimeId: string, value: SessionCatalogPage['next_cursor']) => {
    setCursors(current => ({ ...current, [runtimeId]: value }));
    setSelected(undefined);
    results.current?.scrollTo(0, 0);
  };
  let offset = 0;
  return <Dialog open onOpenChange={onOpenChange} title={status === 'archived' ? 'Archived sessions' : 'Search sessions'} initialFocus={input} finalFocus={finalFocus} xstyle={styles.dialog}
    header={<div {...stylex.props(layout.column)}><div {...stylex.props(styles.searchHeader)}><Search size={20} aria-hidden="true" />
      <Input ref={input} value={search} onChange={event => setSearch(event.target.value)} aria-label={hosts.length > 1 ? 'Search sessions across hosts' : 'Search sessions on this host'}
        placeholder="Search sessions and directories" xstyle={styles.input} aria-controls={resultId}
        onKeyDown={event => {
          if (event.nativeEvent.isComposing) return;
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault(); setSelected(items[Math.max(0, Math.min(items.length - 1, current + (event.key === 'ArrowDown' ? 1 : -1)))]?.key);
          } else if (event.key === 'Enter' && active && !settling) {
            event.preventDefault(); results.current?.querySelector<HTMLAnchorElement>(`[data-search-index="${current}"] a`)?.click();
          }
        }} />
    </div><Select label="Session status" value={status} options={[{ value: 'active', label: 'Active sessions' }, { value: 'archived', label: 'Archived sessions' }, { value: 'all', label: 'All sessions' }]} onValueChange={value => { setStatus(value as typeof status); setCursors({}); setSelected(undefined); }} />{hosts.length > 1 && <Select label="Search host" value={filter} options={[{ value: '', label: 'All hosts' }, ...hosts.map(host => ({ value: host.id, label: host.name }))]}
      onValueChange={value => { setFilter(value); setSelected(undefined); }} />}</div>}>
    <div ref={results} id={resultId} {...stylex.props(styles.results)} aria-label="Session search results" aria-busy={loading}>
      {actionError && <ErrorNotice type="action" owner={actionError.owner} error={actionError.error} title="Could not open session" onDismiss={() => setActionError(undefined)} />}
      {groups.map(group => {
        const { host, page, query } = group;
        const runtimeId = host.runtimeId ?? '';
        const start = offset;
        offset += group.items.length;
        return <section key={host.id} aria-label={`${host.name} search results`}>
          {hosts.length > 1 && <h3 {...stylex.props(styles.host)}>{host.name}</h3>}
          {group.items.map((session, position) => {
            const index = start + position;
            const saved = runtime.tabs.preferred(runtimeId, session.id);
            const menuItems = actions.items({ runtimeId, rootId: session.id, title: session.title, archived: session.archived });
            return <ContextMenu key={session.id} items={menuItems} onOpenChange={actions.prepare}><div data-search-index={index} {...stylex.props(styles.result, sessionMarker, index === current && styles.highlighted)}
              onPointerMove={() => setSelected(items[index]!.key)} onFocus={() => setSelected(items[index]!.key)}>
              <Link to="/h/$runtimeId/s/$rootId" params={{ runtimeId, rootId: session.id }} search={sessionSearch(saved)} state={{ whipViewId: saved?.id }} preload={false}
                title={`${host.name}\n${session.title || 'Untitled session'}\n${session.cwd}`} aria-label={`${session.title || 'Untitled session'} · ${host.name}${session.cwd ? ` · ${session.cwd}` : ''}`}
                {...stylex.props(styles.link)} onClick={event => {
                  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                  try { runtime.tabs.open(runtimeId, session.id, session.title); close(); }
                  catch (error) { event.preventDefault(); setActionError({ owner: `${runtimeId}:${session.id}`, error }); }
                }}>
                <Code2 size={18} aria-hidden="true" /><span {...stylex.props(layout.grow, layout.ellipsis)}>{session.title || 'Untitled session'}{session.archived ? ' · Archived' : ''}</span>
                {index === current ? <CornerDownLeft size={16} aria-hidden="true" /> : <span {...stylex.props(styles.date)}>{updatedLabel(session.updated_at)}</span>}
              </Link>
              <Menu trigger={<IconButton label={`Actions for ${session.title || 'Untitled session'} on ${host.name}`} variant="ghost" xstyle={[sidebarStyles.icon, sidebarStyles.sessionMenu]}><MoreHorizontal size={14} /></IconButton>} items={menuItems} onOpenChange={actions.prepare} />
            </div></ContextMenu>;
          })}
          {group.connected && !group.items.length && !group.error && <p role="status" {...stylex.props(styles.notice)}>{group.loading ? 'Loading sessions…' : term ? 'No matching sessions.' : status === 'archived' ? 'No archived sessions.' : 'No recent sessions yet.'}</p>}
          {!group.connected && <p role="status" {...stylex.props(styles.notice)}>Sessions unavailable while {host.name} is offline. <Link to="/settings" search={{ section: "connections" }} onClick={close}>Manage servers</Link></p>}
          {group.error && <ErrorNotice type="resource" owner={`session-search:${host.id}`} error={group.error} title={`Could not load sessions on ${host.name}`} action={<Button variant="ghost" onClick={() => {
            if (term || filtered) { if (cursors[runtimeId]) cursor(runtimeId, undefined); else void query.refetch(); }
            else void host.list?.refresh().catch(() => {});
          }}>Retry {hosts.length > 1 ? host.name : ''}</Button>} />}
          {(term || filtered) && page?.has_more && <Button variant="ghost" disabled={settling || group.loading} onClick={() => cursor(runtimeId, page.next_cursor)}>Load more sessions{hosts.length > 1 ? ` on ${host.name}` : ''}</Button>}
          {cursors[runtimeId] && <Button variant="ghost" onClick={() => cursor(runtimeId, undefined)}>First results{hosts.length > 1 ? ` on ${host.name}` : ''}</Button>}
          {!term && !filtered && ((page?.items?.length ?? 0) > 64 || page?.has_more) && <p {...stylex.props(styles.notice)}>Showing 64 recent sessions{hosts.length > 1 ? ` on ${host.name}` : ''}. Search to find older sessions.</p>}
        </section>;
      })}
      {!groups.length && <p role="status" {...stylex.props(styles.notice)}>No hosts selected.</p>}
    </div>
    <span role="status" {...stylex.props(layout.srOnly)}>{active ? `${active.session.title || 'Untitled session'} on ${active.host.name}, result ${current + 1} of ${items.length}` : ''}</span>
  </Dialog>;
}
function updatedLabel(value: string) {
  const time = Date.parse(value);
  if (!Number.isFinite(time)) return '';
  const days = Math.max(0, Math.floor((Date.now() - time) / 86_400_000));
  return days < 1 ? 'Today' : days < 2 ? 'Yesterday' : days < 7 ? 'Past week' : days < 31 ? 'Past month' : new Date(time).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}
const styles = stylex.create({
  dialog: { width: 'min(680px, calc(100vw - 32px))', height: 'min(560px, calc(100dvh - 48px))', maxHeight: 'calc(100dvh - 48px)', padding: 16, gap: 12, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 12, backgroundColor: colors.panel, display: 'grid', gridTemplateRows: 'auto minmax(0, 1fr)', overflow: 'hidden' },
  searchHeader: { display: 'flex', alignItems: 'center', gap: 12, minHeight: 36, color: surface.secondaryText },
  input: { borderWidth: 0, backgroundColor: 'transparent', boxShadow: 'none', fontSize: typography.size16, flex: 1, minWidth: 0, paddingInline: 0 },
  results: { minHeight: 0, height: '100%', overflowY: 'auto', borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, paddingTop: 10 },
  host: { fontSize: typography.size12, color: surface.secondaryText, fontWeight: 600, paddingInline: 12, marginBlock: 8 },
  result: { display: 'flex', alignItems: 'center', borderRadius: 8, minHeight: { default: 40, [scale.touch]: 48 }, color: surface.secondaryText },
  highlighted: { backgroundColor: colors.background, color: colors.foreground },
  link: { display: 'flex', alignItems: 'center', gap: 12, paddingInline: 12, minHeight: { default: 40, [scale.touch]: 48 }, flex: 1, minWidth: 0, color: 'inherit', textDecoration: 'none', fontSize: typography.size14, borderRadius: 8 },
  date: { fontSize: typography.size12, color: surface.secondaryText, flexShrink: 0, display: { default: 'block', [scale.phone]: 'none' } },
  notice: { padding: 12, color: surface.secondaryText, fontSize: typography.size13 },
});
