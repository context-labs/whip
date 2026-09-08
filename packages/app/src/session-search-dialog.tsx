import { useEffect, useId, useRef, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useSessionListView, useWhipConnection } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import type { SessionListView } from '@whip/sdk/state';
import type { SessionCatalogPage } from '@whip/protocol';
import { Button, ContextMenu, Dialog, IconButton, Input, Menu, type DialogProps } from '@whip/ui';
import { Code2, CornerDownLeft, MoreHorizontal, Search } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface } from '@whip/ui/tokens.stylex';
import { styles as sidebarStyles, sessionMarker } from './session-sidebar.stylex';
import { layout } from './styles';
import { useRuntime, useSessionTabs } from './context';
import { sessionSearch } from './session-tabs';

type Props = { client: WhipClient; list: SessionListView; open: boolean; onOpenChange(open: boolean): void; finalFocus: DialogProps['finalFocus'] };
export function SessionSearchDialog(props: Props) {
  const connection = useWhipConnection(props.client);
  return props.open ? <SearchDialog key={connection.info?.runtime_id ?? ''} {...props} /> : null;
}
function SearchDialog({ client, list, onOpenChange, finalFocus }: Props) {
  const runtime = useRuntime();
  useSessionTabs();
  const connection = useWhipConnection(client);
  const runtimeId = connection.info?.runtime_id ?? '';
  const catalog = useSessionListView(list);
  const [search, setSearch] = useState('');
  const [term, setTerm] = useState('');
  const [cursor, setCursor] = useState<SessionCatalogPage['next_cursor']>();
  const [selected, setSelected] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const results = useRef<HTMLDivElement>(null);
  const resultId = useId();
  useEffect(() => {
    const timer = setTimeout(() => { setTerm(search.trim()); setCursor(undefined); setSelected(0); }, 200);
    return () => clearTimeout(timer);
  }, [search]);
  const query = useQuery({
    queryKey: ['session-search', runtimeId, term, cursor],
    queryFn: ({ signal }) => client.sessions.list({ search: term, ...(cursor ? { cursor } : {}), limit: 64, max_bytes: 256 << 10 }, { signal }),
    enabled: !!term && connection.state === 'connected',
  });
  const page = term ? query.data : catalog.page;
  const items = (page?.items ?? []).slice(0, 64);
  const loading = term ? query.isFetching : catalog.status === 'loading';
  const error = term ? query.error?.message : catalog.error?.message;
  const current = Math.min(selected, Math.max(0, items.length - 1));
  const settling = search.trim() !== term || loading;
  const active = items[current];
  useEffect(() => {
    results.current?.querySelector<HTMLElement>(`[data-search-index="${current}"]`)?.scrollIntoView({ block: 'nearest' });
  }, [current]);
  const close = () => onOpenChange(false);
  return <Dialog open onOpenChange={onOpenChange} title="Search sessions" initialFocus={input} finalFocus={finalFocus} xstyle={styles.dialog}
    header={<div {...stylex.props(styles.searchHeader)}><Search size={20} aria-hidden="true" />
      <Input ref={input} value={search} onChange={event => setSearch(event.target.value)} aria-label="Search sessions on this host"
        placeholder="Search sessions and directories" xstyle={styles.input} aria-controls={resultId}
        onKeyDown={event => {
          if (event.nativeEvent.isComposing) return;
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault(); setSelected(index => Math.max(0, Math.min(items.length - 1, index + (event.key === 'ArrowDown' ? 1 : -1))));
          } else if (event.key === 'Enter' && active && !settling) {
            event.preventDefault(); results.current?.querySelector<HTMLAnchorElement>(`[data-search-index="${current}"] a`)?.click();
          }
        }} />
    </div>}>
    <div ref={results} id={resultId} {...stylex.props(styles.results)} aria-label="Session search results" aria-busy={loading}>
      {items.map((session, index) => {
        const saved = runtime.tabs.preferred(runtimeId, session.id);
        const actions = [{ id: 'background', label: 'Open in background tab', onSelect: () => {
          try { runtime.tabs.open(runtimeId, session.id, session.title); } catch (error) { runtime.report(error); }
        } }];
        return <ContextMenu key={session.id} items={actions}><div data-search-index={index} {...stylex.props(styles.result, sessionMarker, index === current && styles.highlighted)}
          onPointerMove={() => setSelected(index)} onFocus={() => setSelected(index)}>
          <Link to="/h/$runtimeId/s/$rootId" params={{ runtimeId, rootId: session.id }} search={sessionSearch(saved)} state={{ whipViewId: saved?.id }} preload={false}
            title={`${session.title || 'Untitled session'}\n${session.cwd}`} aria-label={`${session.title || 'Untitled session'}${session.cwd ? ` · ${session.cwd}` : ''}`}
            {...stylex.props(styles.link)} onClick={event => {
              if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
              try { runtime.tabs.open(runtimeId, session.id, session.title); close(); }
              catch (error) { event.preventDefault(); runtime.report(error); }
            }}>
            <Code2 size={18} aria-hidden="true" /><span {...stylex.props(layout.grow, layout.ellipsis)}>{session.title || 'Untitled session'}</span>
            {index === current ? <CornerDownLeft size={16} aria-hidden="true" /> : <span {...stylex.props(styles.date)}>{updatedLabel(session.updated_at)}</span>}
          </Link>
          <Menu trigger={<IconButton label={`Actions for ${session.title || 'Untitled session'}`} variant="ghost" xstyle={[sidebarStyles.icon, sidebarStyles.sessionMenu]}><MoreHorizontal size={14} /></IconButton>} items={actions} />
        </div></ContextMenu>;
      })}
      {!items.length && !error && <p role="status" {...stylex.props(styles.notice)}>{loading ? 'Loading sessions…' : term ? 'No matching sessions.' : 'No recent sessions yet.'}</p>}
      {error && <div role="alert" {...stylex.props(styles.notice)}>{error}<Button variant="ghost" onClick={() => { if (term) { if (cursor) setCursor(undefined); else void query.refetch(); } else void list.refresh().catch(error => runtime.report(error)); }}>Retry</Button></div>}
      {term && page?.has_more && <Button variant="ghost" disabled={settling} onClick={() => { setCursor(page.next_cursor); setSelected(0); results.current?.scrollTo(0, 0); }}>Load more sessions</Button>}
      {cursor && <Button variant="ghost" onClick={() => { setCursor(undefined); setSelected(0); }}>First results</Button>}
      {!term && ((page?.items?.length ?? 0) > 64 || page?.has_more) && <p {...stylex.props(styles.notice)}>Showing 64 recent sessions. Search to find older sessions.</p>}
    </div>
    <span role="status" {...stylex.props(layout.srOnly)}>{active ? `${active.title || 'Untitled session'}, result ${current + 1} of ${items.length}` : ''}</span>
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
  input: { borderWidth: 0, backgroundColor: 'transparent', boxShadow: 'none', fontSize: 16, flex: 1, minWidth: 0, paddingInline: 0 },
  results: { minHeight: 0, height: '100%', overflowY: 'auto', borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, paddingTop: 10 },
  result: { display: 'flex', alignItems: 'center', borderRadius: 8, minHeight: { default: 40, [scale.touch]: 48 }, color: surface.secondaryText },
  highlighted: { backgroundColor: colors.background, color: colors.foreground },
  link: { display: 'flex', alignItems: 'center', gap: 12, paddingInline: 12, minHeight: { default: 40, [scale.touch]: 48 }, flex: 1, minWidth: 0, color: 'inherit', textDecoration: 'none', fontSize: 14, borderRadius: 8 },
  date: { fontSize: 12, color: surface.secondaryText, flexShrink: 0, display: { default: 'block', [scale.phone]: 'none' } },
  notice: { padding: 12, color: surface.secondaryText, fontSize: 13 },
});
