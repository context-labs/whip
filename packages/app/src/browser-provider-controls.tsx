import { useEffect, useState, useSyncExternalStore } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { SessionCatalogPage } from '@whip/protocol';
import { Button, Checkbox, Dialog, Field, IconButton, Input, Select } from '@whip/ui';
import { MessagesSquare } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from './context';
import type { HostConnection } from './hosts';
import { browserProjectId, browserVersionHelp } from './browser-provider';
import { errorMessage } from './platform';
import { layout } from './styles';

type Conversation = { id: string; title: string; cwd: string };
const subscribeNone = () => () => {};
const emptyCatalog = () => undefined;

/** Explicit, window-local access selection. Opening this dialog never binds a provider. */
export function BrowserProviderControls({ tabId }: { tabId?: string }) {
  const runtime = useRuntime();
  const associations = useSyncExternalStore(runtime.browserAssociations.subscribe, runtime.browserAssociations.getSnapshot, runtime.browserAssociations.getSnapshot);
  const [open, setOpen] = useState(false);
  const active = associations.filter(item => item.status === 'selected').length;
  return <>
    {tabId ? <IconButton variant="ghost" label={`Conversation access${active ? ` (${active})` : ''}…`} disabled={!runtime.platform.browserAgent} onClick={() => setOpen(true)}>
      <MessagesSquare size={16} aria-hidden="true"/>
    </IconButton> : <Button variant="ghost" size="sm" disabled={!runtime.platform.browserAgent} onClick={() => setOpen(true)}>
      Manage conversation access{active ? ` (${active})` : ''}…
    </Button>}
    <Dialog open={open} onOpenChange={setOpen} title="Conversation Browser access" description="Choose exactly which conversation can request this Browser. Every operation remains subject to its permissions; offering a tab is not an approval.">
      {open && <BrowserProviderDialog tabId={tabId}/>}
    </Dialog>
  </>;
}
function BrowserProviderDialog({ tabId }: { tabId?: string }) {
  const runtime = useRuntime(), { hosts } = useAppState();
  const associations = useSyncExternalStore(runtime.browserAssociations.subscribe, runtime.browserAssociations.getSnapshot, runtime.browserAssociations.getSnapshot);
  const [hostId, setHostId] = useState(''), [root, setRoot] = useState<Conversation>();
  const [preview, setPreview] = useState(false), [pending, setPending] = useState(false), [error, setError] = useState('');
  const connected = hosts.filter(host => host.state === 'connected' && host.client && host.runtimeId);
  const host = connected.find(host => host.id === hostId);
  useEffect(() => { setRoot(undefined); setPreview(false); }, [host?.client, host?.runtimeId]);
  const existing = host && root && associations.find(item => item.runtimeId === host.runtimeId && item.rootId === root.id);
  const select = async () => {
    if (!host || !root || !tabId) return;
    setPending(true); setError('');
    try { await runtime.browserAssociations.select({ hostId: host.id, rootId: root.id, title: root.title, tabId, preview, projectId: browserProjectId(root.cwd) }); }
    catch (error) { setError(errorMessage(error)); }
    finally { setPending(false); }
  };
  return <div {...stylex.props(layout.column)}>
    {tabId && <>
      <Field label="Execution host"><Select label="Execution host" value={host?.id ?? ''} placeholder="Choose a connected host" disabled={pending}
        options={connected.map(host => ({ value: host.id, label: host.name }))} onValueChange={value => { setHostId(value); setRoot(undefined); setPreview(false); setError(''); }}/></Field>
      {!connected.length && <p role="status">Connect an execution host to choose a conversation. Human browsing does not require a host.</p>}
      {host && <ConversationPicker key={`${host.id}:${host.runtimeId}`} host={host} disabled={pending} root={root} onSelect={root => { setRoot(root); setPreview(false); setError(''); }}/>}
      {host?.profile.target.kind === 'ssh' && <Checkbox label="Offer this host’s SSH preview environment" checked={preview} disabled={pending || !(root && browserProjectId(root.cwd))}
        onCheckedChange={checked => setPreview(checked === true)} description={root && browserProjectId(root.cwd) ? `Project: ${root.cwd}. Offers verified SSH metadata only; no route starts before approval.` : 'Select a conversation with a project identity to offer SSH previews.'}/>}
      <p {...stylex.props(layout.muted)}>Offers this tab and a profile for new pages, not every open tab. Provider selection is not restored after disconnect or app restart.</p>
      <Button disabled={!host || !root || pending || existing?.status === 'selected' || existing?.status === 'selecting'} loading={pending} onClick={() => { void select(); }}>Offer Browser to conversation</Button>
    </>}
    <p {...stylex.props(layout.muted)}>{browserVersionHelp}</p>
    {error && <p role="alert">{error}</p>}
    <section aria-label="Selected conversations" {...stylex.props(layout.column)}>
      <h3>Selected conversations</h3>
      {!associations.length && <p>No conversation has Browser access from this window.</p>}
      {associations.map(item => <div key={item.key} {...stylex.props(layout.column)}>
        <div {...stylex.props(layout.row)}><span {...stylex.props(layout.grow)}>{item.title || 'Untitled conversation'} · {item.hostName} · {item.rootId.slice(-8)}</span>
          <Button variant="secondary" size="sm" aria-label={`Release Browser access for ${item.title || item.rootId}`} onClick={() => { void runtime.browserAssociations.release(item.key).catch(error => setError(errorMessage(error))); }}>Release</Button></div>
        <p role="status">{item.status === 'selected' ? 'Selected — permission requests may use this window' : item.status === 'available' ? 'Available for new-tab requests — no page access granted' : item.status === 'selecting' ? 'Selecting…' : item.status === 'unavailable' ? 'Unavailable — explicit selection required' : 'Browser access was not selected'}{item.error ? `: ${item.error}` : ''}</p>
      </div>)}
    </section>
    <p {...stylex.props(layout.muted)}>Release revokes this association; it does not close human Browser tabs or cancel unrelated conversation work.</p>
  </div>;
}
function ConversationPicker({ host, root, disabled, onSelect }: { host: HostConnection; root?: Conversation; disabled: boolean; onSelect(root: Conversation | undefined): void }) {
  const runtime = useRuntime();
  const catalog = useSyncExternalStore(host.list?.subscribe ?? subscribeNone, host.list?.getSnapshot ?? emptyCatalog, host.list?.getSnapshot ?? emptyCatalog);
  const [search, setSearch] = useState(''), [term, setTerm] = useState('');
  const [cursor, setCursor] = useState<SessionCatalogPage['next_cursor']>();
  useEffect(() => { const timer = setTimeout(() => setTerm(search.trim()), 250); return () => clearTimeout(timer); }, [search]);
  const query = useQuery({ queryKey: ['browser-conversations', host.runtimeId, term, cursor],
    queryFn: ({ signal }) => host.client!.sessions.list({ search: term, status: 'all', cursor, limit: 64, max_bytes: 256 << 10 }, { signal: AbortSignal.any([signal, runtime.connections.signal(host.client!)]) }),
    enabled: !!host.client && host.state === 'connected' && (!!term || !!cursor || !host.list), gcTime: 0 });
  const page = term || cursor || !host.list ? query.data : catalog?.page;
  const rows = (page?.items ?? []).slice(0, 64);
  const settling = search.trim() !== term, failure = query.error ?? catalog?.error;
  return <>
    <Field label="Find a conversation"><Input aria-label="Find a conversation" value={search} disabled={disabled} placeholder="Search conversations" onChange={event => { setSearch(event.target.value); setCursor(undefined); onSelect(undefined); }}/></Field>
    <Field label="Conversation"><Select label="Conversation" value={root?.id ?? ''} placeholder="Choose a conversation" disabled={disabled || settling || !rows.length}
      options={rows.map(row => ({ value: row.id, label: `${row.title || 'Untitled conversation'} · ${row.id.slice(-8)}` }))} onValueChange={id => onSelect(rows.find(row => row.id === id))}/></Field>
    {failure && <p role="alert">Could not load conversations: {errorMessage(failure)}</p>}
    {!rows.length && !failure && <p role="status">{query.isFetching || catalog?.status === 'loading' ? 'Loading conversations…' : 'No conversations found. Create a fresh conversation on this host.'}</p>}
    <div {...stylex.props(layout.row)}>{cursor && <Button variant="ghost" size="sm" disabled={disabled} onClick={() => { setCursor(undefined); onSelect(undefined); }}>First page</Button>}
      {page?.next_cursor && <Button variant="ghost" size="sm" disabled={disabled || settling} onClick={() => { setCursor(page.next_cursor); onSelect(undefined); }}>More conversations</Button>}</div>
  </>;
}
