import { useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent } from 'react';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { useWhipConnection } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import type { SessionListView } from '@whip/sdk/state';
import { Button, Checkbox, Dialog, IconButton, Input, Menu, Spinner } from '@whip/ui';
import { ArrowLeft, ArrowUp, Check, ChevronDown, ChevronRight, Folder, HardDrive, Home, Pencil, Search, Server } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { errorMessage } from './platform';
import { layout } from './styles';
import { styles } from './remote-directory-dialog.stylex';
import { directoryCache, directoryOptions } from './directory-queries';

export interface RemoteDirectoryHost {
  name: string;
  detail?: string;
  list?: SessionListView;
  reconnect?(): Promise<void>;
}
const subscribeNone = () => () => {};
const emptySnapshot = () => undefined;
const folderName = (path: string) => path.split(/[\\/]/).filter(Boolean).at(-1) || path;
const absolutePath = (path: string) => /^(\/|[a-z]:[\\/]|\\\\|~(?:\/|$))/i.test(path);

/** Paths are interpreted on the host, never with this browser's platform rules. */
export function directoryCrumbs(path: string) {
  const root = path.match(/^(?:[a-z]:[\\/]|\\\\[^\\]+\\[^\\]+\\?|\/)/i)?.[0];
  if (!root) return [];
  const separator = root.includes('\\') ? '\\' : '/';
  const result = [{ label: root, path: root }];
  let current = root.replace(/[\\/]$/, '');
  for (const label of path.slice(root.length).split(/[\\/]/).filter(Boolean)) {
    current += separator + label;
    result.push({ label, path: current });
  }
  return result;
}
export function recentDirectories(items: readonly { cwd: string; updated_at: string }[] = []) {
  return [...new Set([...items].sort((a, b) => b.updated_at.localeCompare(a.updated_at)).map(item => item.cwd).filter(path => path && path.length <= 4096 && absolutePath(path)))].slice(0, 5);
}

export function RemoteDirectoryDialog({ client, value, disabled, host, onClose, onSelect }: {
  client: WhipClient; value: string; disabled: boolean; host?: RemoteDirectoryHost;
  onClose(): void; onSelect(path: string): void;
}) {
  const connection = useWhipConnection(client);
  const online = connection.state === 'connected';
  const queries = useQueryClient();
  const cache = directoryCache(queries);
  const hostName = host?.name ?? 'Execution host';
  const catalog = useSyncExternalStore(host?.list?.subscribe ?? subscribeNone, host?.list?.getSnapshot ?? emptySnapshot);
  const recent = recentDirectories(catalog?.page?.items ?? []);
  const [path, setPath] = useState(value);
  const [history, setHistory] = useState<string[]>([]);
  const [selected, setSelected] = useState('');
  const [editing, setEditing] = useState(false);
  const [typed, setTyped] = useState(value);
  const [filter, setFilter] = useState('');
  const [prefix, setPrefix] = useState('');
  const [hidden, setHidden] = useState(false);
  const [after, setAfter] = useState<string>();
  const [actionError, setActionError] = useState('');
  const [confirming, setConfirming] = useState(false);
  const [reconnecting, setReconnecting] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const editButton = useRef<HTMLButtonElement>(null);
  const rows = useRef<HTMLUListElement>(null);
  const listBody = useRef<HTMLDivElement>(null);
  const focusAfterNavigation = useRef(false);
  const filterInput = useRef<HTMLInputElement>(null);
  const wasEditing = useRef(false);
  const alive = useRef(true);
  const choiceVersion = useRef(0);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => { const timer = setTimeout(() => setPrefix(filter), 180); return () => clearTimeout(timer); }, [filter]);
  useLayoutEffect(() => {
    if (editing) { input.current?.focus(); input.current?.select(); }
    else if (wasEditing.current) editButton.current?.focus();
    wasEditing.current = editing;
  }, [editing]);
  const filterValid = !/[\\/\0]/.test(filter);
  const options = directoryOptions(client, { path, prefix, hidden, after });
  const query = useQuery({
    ...options,
    enabled: online && !disabled && !editing && filterValid && filter === prefix,
    placeholderData: keepPreviousData,
  });
  const ready = online && !disabled && !editing && filterValid && filter === prefix && query.isSuccess && !query.isFetching && !query.isPlaceholderData;
  // A child's own listing validates readability and can also serve its next open.
  const selectedFolder = useQuery({ ...directoryOptions(client, { path: selected }), enabled: ready && !!selected });
  const canChoose = !confirming && ready && (!selected || (selectedFolder.isSuccess && !selectedFolder.isFetching));
  const chosen = selected ? selectedFolder.data?.path ?? selected : query.data?.path ?? path;
  const current = query.error ? path : query.data?.path ?? path;
  const crumbs = directoryCrumbs(current);
  const entries = query.data?.entries ?? [];
  const loading = online && !editing && !query.error && (query.isFetching || query.isPending || filter !== prefix);
  const [showLoading, setShowLoading] = useState(false);
  useEffect(() => {
    setShowLoading(false);
    if (!loading) return;
    const timer = setTimeout(() => setShowLoading(true), 180);
    return () => clearTimeout(timer);
  }, [loading]);
  const recentKey = JSON.stringify(recent);
  useEffect(() => {
    if (!ready) return;
    cache.warm(client, { path: '~' });
    if (query.data?.parent) cache.warm(client, { path: query.data.parent });
    for (const folder of JSON.parse(recentKey) as string[]) cache.warm(client, { path: folder });
    if (query.data?.has_more) cache.warm(client, { path, prefix, hidden, after: query.data.next_after });
  }, [cache, client, ready, query.data, path, prefix, hidden, recentKey]);
  useEffect(() => {
    if (!online) {
      choiceVersion.current++;
      cache.cancel(client);
      void queries.invalidateQueries({ queryKey: ['directories', connection.info?.runtime_id], refetchType: 'none' });
    }
    return () => {
      cache.cancel(client);
      void queries.cancelQueries({ queryKey: ['directories', connection.info?.runtime_id], predicate: query => !query.getObserversCount() });
    };
  }, [cache, client, online, queries, connection.info?.runtime_id]);
  const scrollKey = JSON.stringify(options.queryKey);
  // Dialog portals mount after this component's first layout effect. Restore
  // cached positions when the viewport actually attaches, including on reopen.
  const attachList = useCallback((element: HTMLDivElement | null) => {
    listBody.current = element;
    if (element && !query.isPlaceholderData) element.scrollTop = cache.scroll(options);
  }, [cache, scrollKey, query.isPlaceholderData]);
  useLayoutEffect(() => {
    if (query.isPlaceholderData || !query.data) return;
    if (listBody.current) listBody.current.scrollTop = cache.scroll(options);
  }, [scrollKey, query.isPlaceholderData, !!query.data]);
  function warm(target: string) { if (online && !disabled && !editing) cache.warm(client, { path: target }); }
  useEffect(() => {
    if (!focusAfterNavigation.current || !ready) return;
    focusAfterNavigation.current = false;
    const first = rows.current?.querySelector<HTMLButtonElement>('[data-folder-select]');
    (first ?? filterInput.current)?.focus();
  }, [ready, current]);
  function resetSelection() { choiceVersion.current++; setSelected(''); setActionError(''); }
  function selectFolder(target: string) { choiceVersion.current++; cache.cancel(client, { path: target }); setSelected(target); setActionError(''); }
  function navigate(target: string, back = false) {
    if (!online || disabled) return;
    // Cached navigation can replace the focused row immediately. Move focus
    // before removal so the dialog's focus trap does not restore it afterward.
    if (rows.current?.contains(document.activeElement)) filterInput.current?.focus();
    choice.current = '';
    if (!back && target !== current) setHistory(previous => [...previous, current || '~'].slice(-32));
    cache.cancel(client, { path: target });
    setPath(target); setTyped(target); setEditing(false); setFilter(''); setPrefix(''); setAfter(undefined); resetSelection();
  }
  function editPath() { choiceVersion.current++; setTyped(current || '~'); setEditing(true); setActionError(''); }
  function cancelEdit() { setEditing(false); setTyped(current); setActionError(''); }
  const choice = useRef('');
  choice.current = online && !disabled && !editing && !query.isPlaceholderData && filter === prefix ? chosen : '';
  async function choose() {
    if (!canChoose || !chosen) return;
    const target = chosen, version = choiceVersion.current;
    const verification = selected ? directoryOptions(client, { path: selected }) : options;
    setConfirming(true); setActionError('');
    cache.cancel(client);
    try {
      const result = await queries.fetchQuery(verification);
      if (alive.current && version === choiceVersion.current && choice.current === target && client.getSnapshot().state === 'connected') onSelect(result.path);
    } catch (error) { if (alive.current && version === choiceVersion.current && choice.current === target) setActionError(errorMessage(error)); }
    finally { if (alive.current) setConfirming(false); }
  }
  async function reconnect() {
    setReconnecting(true); setActionError('');
    try { await host?.reconnect?.(); }
    catch (error) { if (alive.current) setActionError(errorMessage(error)); }
    finally { if (alive.current) setReconnecting(false); }
  }
  function shortcuts(event: KeyboardEvent) {
    if (event.nativeEvent.isComposing) return;
    if ((event.metaKey || event.ctrlKey) && event.shiftKey && event.key.toLowerCase() === 'g') {
      event.preventDefault(); event.stopPropagation(); editPath();
    } else if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
      event.preventDefault(); event.stopPropagation(); choose();
    } else if (event.key === 'Escape' && editing) {
      event.preventDefault(); event.stopPropagation(); cancelEdit();
    }
  }
  function rowKey(event: KeyboardEvent, index: number) {
    if (event.metaKey || event.ctrlKey || event.altKey || event.nativeEvent.isComposing) return;
    const next = event.key === 'ArrowDown' ? Math.min(index + 1, entries.length - 1) : event.key === 'ArrowUp' ? Math.max(index - 1, 0) : event.key === 'Home' ? 0 : event.key === 'End' ? entries.length - 1 : undefined;
    if (next !== undefined) {
      event.preventDefault(); selectFolder(entries[next].path);
      rows.current?.querySelectorAll<HTMLButtonElement>('[data-folder-select]')[next]?.focus();
    } else if (event.key === 'Enter' || event.key === 'ArrowRight') {
      event.preventDefault(); event.stopPropagation();
      // Keep focus on a stable control while the opened folder replaces these rows.
      filterInput.current?.focus(); focusAfterNavigation.current = true; navigate(entries[index].path);
    }
  }
  const locations = [
    { id: 'home', label: 'Home', icon: <Home size={14} />, onSelect: () => navigate('~'), disabled: !online || disabled },
    { id: 'root', label: 'File system', icon: <HardDrive size={14} />, onSelect: () => navigate(crumbs[0]?.path ?? '/'), disabled: !online || disabled },
    ...recent.map(folder => ({ id: folder, label: folderName(folder), icon: <Folder size={14} />, onSelect: () => navigate(folder), disabled: !online || disabled })),
  ];
  return <Dialog open onOpenChange={open => { if (!open) onClose(); }} title="Choose a folder" initialFocus={filterInput}
    xstyle={styles.dialog} headerXstyle={styles.header} bodyXstyle={styles.body}
    description={<span {...stylex.props(styles.host)}><Server size={14} /><span title={hostName} {...stylex.props(styles.hostName)}>{hostName}</span>{host?.detail && host.detail.replace(/^https?:\/\//, '').replace(/\/$/, '') !== hostName && <span title={host.detail} {...stylex.props(styles.hostDetail)}>{host.detail}</span>}<span {...stylex.props(styles.connectionStatus)}><span {...stylex.props(styles.dot, online && styles.connected)} />{online ? 'Connected' : 'Disconnected'}</span></span>}>
    <div onKeyDownCapture={shortcuts} {...stylex.props(styles.body)}>
      <div {...stylex.props(styles.workspace)}>
      <div {...stylex.props(styles.navigation)}>
        {editing ? <form {...stylex.props(styles.pathForm)} onSubmit={event => { event.preventDefault(); event.stopPropagation(); if (absolutePath(typed) && typed.length <= 4096 && !typed.includes('\0')) navigate(typed); else setActionError('Enter an absolute path or a path starting with ~/.'); }}>
          <Input ref={input} aria-label="Remote path" value={typed} maxLength={4096} xstyle={styles.pathInput} onChange={event => { setTyped(event.target.value); setActionError(''); }} />
          <Button type="submit" disabled={!online || disabled || !typed}>Go</Button><Button variant="ghost" onClick={cancelEdit}>Cancel path edit</Button>
        </form> : <>
          <IconButton label="Back" variant="ghost" disabled={!online || disabled || !history.length} onClick={() => { const previous = history.at(-1); if (previous !== undefined) { setHistory(history.slice(0, -1)); navigate(previous, true); } }}><ArrowLeft size={16} /></IconButton>
          <IconButton label="Parent folder" variant="ghost" disabled={!online || disabled || !query.data?.parent || query.data.parent === current} onClick={() => navigate(query.data!.parent)}><ArrowUp size={16} /></IconButton>
          <nav aria-label="Folder path" {...stylex.props(styles.breadcrumbs)}>{crumbs.length ? crumbs.map((crumb, index) => <span key={crumb.path} {...stylex.props(styles.crumb)}>{index > 0 && <ChevronRight size={12} />}<Button variant="ghost" title={crumb.path} aria-current={index === crumbs.length - 1 ? 'location' : undefined} disabled={!online || disabled} xstyle={styles.crumbButton} onPointerEnter={() => warm(crumb.path)} onFocus={() => warm(crumb.path)} onClick={() => navigate(crumb.path)}>{crumb.label}</Button></span>) : <span>{current || 'Home'}</span>}</nav>
          <Button ref={editButton} variant="ghost" onClick={editPath} disabled={!online || disabled}><Pencil size={14} /><span>Edit path</span></Button>
        </>}
      </div>
      <div {...stylex.props(styles.mobileLocations)}><Menu align="start" trigger={<Button variant="ghost"><Home size={14} />Locations<ChevronDown size={14} /></Button>} items={locations} /></div>
      <div {...stylex.props(styles.browser)}>
        <aside aria-label="Locations" {...stylex.props(styles.locations)}><p {...stylex.props(styles.locationLabel)}>Locations</p>
          {locations.slice(0, 2).map(location => <Button key={location.id} variant="ghost" disabled={location.disabled} onClick={location.onSelect} xstyle={styles.location}>{location.icon}{location.label}</Button>)}
          {!!recent.length && <><p {...stylex.props(styles.recentLabel)}>Recent</p>{recent.map(folder => <Button key={folder} variant="ghost" disabled={!online || disabled} title={folder} onPointerEnter={() => warm(folder)} onFocus={() => warm(folder)} onClick={() => navigate(folder)} xstyle={styles.location}><Folder size={14} /><span {...stylex.props(layout.ellipsis)}>{folderName(folder)}</span></Button>)}</>}
          <span {...stylex.props(styles.locationFoot)}><Server size={14} />Remote folders</span>
        </aside>
        <section aria-label="Folder browser" {...stylex.props(styles.content)}>
          <div {...stylex.props(styles.filters)}><div {...stylex.props(styles.search)}><Search size={14} /><Input ref={filterInput} aria-label="Filter folders by prefix" placeholder="Filter by folder name…" value={filter} maxLength={256} disabled={!online || disabled || editing} xstyle={styles.searchInput} onChange={event => { setFilter(event.target.value); setAfter(undefined); resetSelection(); }} /></div>
            <Checkbox label="Hidden folders" checked={hidden} disabled={!online || disabled || editing} onCheckedChange={checked => { setHidden(checked === true); setAfter(undefined); resetSelection(); }} />
          </div>
          <div {...stylex.props(styles.listHeader)}><span>Name</span><span {...stylex.props(styles.listStatus)}>{showLoading && loading && <span role="status" {...stylex.props(styles.loading)}><Spinner />Loading folders…</span>}{query.data && !query.error ? `${entries.length} ${query.data?.has_more || after || query.data?.truncated ? 'folders shown' : entries.length === 1 ? 'folder' : 'folders'}` : ''}</span></div>
          <div ref={attachList} aria-busy={loading} onScroll={() => { if (!query.isPlaceholderData && listBody.current) cache.saveScroll(options, listBody.current.scrollTop); }} {...stylex.props(styles.listBody)}>
            {!online ? <div role="status" {...stylex.props(styles.empty)}><Server size={24} /><strong>{hostName} is offline</strong><span>Reconnect to continue browsing. Your path is saved.</span>{host?.reconnect && <Button loading={reconnecting} onClick={() => void reconnect()}>Reconnect</Button>}</div>
              : query.error && !editing ? <div role="alert" {...stylex.props(styles.empty)}><Folder size={24} /><strong>Can’t open this folder</strong><span {...stylex.props(styles.errorDetail)}>{errorMessage(query.error)}</span><div {...stylex.props(styles.actions)}><Button onClick={editPath}>Edit path</Button><Button variant="ghost" onClick={() => void query.refetch()}>Retry</Button></div></div>
              : !filterValid ? <p role="status" {...stylex.props(styles.empty)}>Enter a folder name without slashes.</p>
              : loading && !query.data ? <div aria-hidden="true">{Array.from({ length: 6 }, (_, index) => <div key={index} {...stylex.props(styles.skeletonRow)}><Folder size={16} /><span {...stylex.props(styles.skeletonLine)} /></div>)}</div>
              : !entries.length ? <div role="status" {...stylex.props(styles.empty)}><Folder size={24} /><strong>{prefix ? 'No matching folders' : 'No subfolders here'}</strong><span>{prefix ? 'Try a different name or clear the filter.' : 'You can still use this folder for your session.'}</span>{prefix && <Button onClick={() => { setFilter(''); setPrefix(''); setAfter(undefined); resetSelection(); }}>Clear filter</Button>}</div>
              : <ul ref={rows} aria-label="Folders" {...stylex.props(styles.list, (editing || query.isPlaceholderData || filter !== prefix) && styles.dimmed)}>{entries.map((entry, index) => <li key={entry.path} {...stylex.props(styles.row, selected === entry.path && styles.selected)}>
                <Button data-folder-select variant="ghost" aria-label={entry.name} aria-pressed={selected === entry.path} title={entry.path} disabled={!ready} xstyle={styles.rowSelect}
                  onPointerEnter={() => warm(entry.path)} onFocus={() => warm(entry.path)}
                  onClick={() => selectFolder(entry.path)} onDoubleClick={() => navigate(entry.path)} onKeyDown={event => rowKey(event, index)}>
                  <span {...stylex.props(styles.iconSlot)}><Folder size={16} /></span><span {...stylex.props(styles.name)}>{entry.name}</span><span {...stylex.props(styles.checkSlot)}>{selected === entry.path && <Check size={14} />}</span>
                </Button><IconButton label={`Open ${entry.name}`} variant="ghost" disabled={!ready} onClick={() => navigate(entry.path)}><ChevronRight size={14} /></IconButton>
              </li>)}</ul>}
          </div>
          <div {...stylex.props(styles.listFooter)}>{query.data?.truncated ? <span>Listing limit reached. Narrow the folder name or enter a path.</span> : <span>{editing ? 'Press Go to open this path.' : 'Double-click a folder to open it'}</span>}
            <div {...stylex.props(styles.actions)}>{after && <Button size="sm" variant="ghost" disabled={!ready} onClick={() => { setAfter(undefined); resetSelection(); }}>First folders</Button>}{query.data?.has_more && <Button size="sm" variant="ghost" disabled={!ready} onClick={() => { setAfter(query.data!.next_after); resetSelection(); }}>Next folders</Button>}</div>
          </div>
        </section>
      </div>
      </div>
      <footer {...stylex.props(styles.footer)}><div aria-live="polite" {...stylex.props(styles.selection)}><span {...stylex.props(styles.caption)}>{selected ? 'Selected folder' : 'Current folder'}</span><span title={chosen} {...stylex.props(styles.selectedPath)}>{chosen || 'Home'}</span>
        <div {...stylex.props(styles.selectionStatus)}>{(actionError || (selected && selectedFolder.error))
          ? <span role="alert" {...stylex.props(styles.selectionError)}>{actionError || `Can’t use this folder: ${errorMessage(selectedFolder.error)}`}</span>
          : online && (confirming || (selected && selectedFolder.isFetching)) ? <span {...stylex.props(styles.caption)}>Checking folder…</span> : null}</div>
      </div><div {...stylex.props(styles.actions)}><Button variant="ghost" onClick={onClose}>Cancel</Button><Button variant="primary" disabled={!canChoose} onClick={choose}>Choose folder</Button></div></footer>
    </div>
  </Dialog>;
}
