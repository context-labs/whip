import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import type { SessionCatalogPage } from '@whip/protocol';
import { Button, Dialog, Field, IconButton, Input, Select } from '@whip/ui';
import { PanelsTopLeft } from 'lucide-react';
import { scale, typography } from '@whip/ui/tokens.stylex';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from './context';
import { browserPreviewAddress } from './browser-address';
import { browserProjectId } from './browser-provider';
import { sessionViewPane } from './session-tabs';
import { tabDestination } from './session-tab-routing';
import type { HostConnection } from './hosts';
import { errorMessage } from './platform';
import { layout } from './styles';

type PreviewRequest = { connectionId: string; runtimeId: string; projectId: string; url: string; paneId: string };
const subscribeNone = () => () => {};
const emptyCatalog = () => undefined;

/** Human browsing is independent of provider selection and agent permissions. */
export function BrowserPreviewControls({ tabId }: { tabId?: string }) {
  const runtime = useRuntime(), navigate = useNavigate();
  const [open, setOpen] = useState(false), [request, setRequest] = useState<PreviewRequest>();
  const [error, setError] = useState(''), [notice, setNotice] = useState('');
  const started = useRef<PreviewRequest | undefined>(undefined);
  useEffect(() => {
    if (!request || started.current === request) return;
    started.current = request;
    // The input dialog has unmounted before native may show its trusted host prompt.
    const { paneId, ...input } = request;
    void runtime.browser.createPreview(input, paneId).then(tab => {
      if (tab) return navigate(tabDestination(tab));
      setNotice('SSH preview was not opened.');
    }).catch(error => setError(errorMessage(error))).finally(() => setRequest(undefined));
  }, [request, runtime, navigate]);
  return <>
    {tabId ? <IconButton variant="ghost" label="Open SSH preview…" disabled={!runtime.platform.browser?.createPreview || !!request} onClick={() => { setError(''); setNotice(''); setOpen(true); }}>
      <PanelsTopLeft size={16} aria-hidden="true"/>
    </IconButton> : <Button variant="ghost" size="sm" disabled={!runtime.platform.browser?.createPreview || !!request} onClick={() => { setError(''); setNotice(''); setOpen(true); }}>Open SSH preview…</Button>}
    {request && <span role="status" {...stylex.props(!!tabId && styles.notice)}>Waiting for SSH preview confirmation…</span>}
    {error && <span role="alert" {...stylex.props(!!tabId && styles.notice)}>Could not open SSH preview: {error}</span>}
    {notice && <span role="status" {...stylex.props(!!tabId && styles.notice)}>{notice}</span>}
    {open && <Dialog open onOpenChange={setOpen} title="Open SSH preview" description="Browse a project on a connected SSH host. This does not select a conversation or grant any agent access. Native confirmation is required before a network route is opened.">
      <PreviewForm onSubmit={input => {
        const workspace = runtime.tabs.workspace();
        setRequest({ ...input, paneId: (tabId && sessionViewPane(workspace, tabId)?.id) || workspace.focusedPaneId });
        setOpen(false);
      }}/>
    </Dialog>}
  </>;
}
const styles = stylex.create({
  notice: { flexBasis: '100%', order: 1, minWidth: 0, paddingInline: scale.space1, fontSize: typography.size12, overflowWrap: 'anywhere' },
});

function PreviewForm({ onSubmit }: { onSubmit(input: Omit<PreviewRequest, 'paneId'>): void }) {
  const { hosts } = useAppState();
  const [hostId, setHostId] = useState('');
  const connected = hosts.filter(host => host.state === 'connected' && host.client && host.runtimeId && host.profile.target.kind === 'ssh');
  const host = connected.find(host => host.id === hostId);
  return <div {...stylex.props(layout.column)}>
    <Field label="SSH host"><Select label="SSH host" value={host?.id ?? ''} placeholder="Choose a connected SSH host"
      options={connected.map(host => ({ value: host.id, label: host.name }))} onValueChange={setHostId}/></Field>
    {!connected.length && <p role="status">Connect a saved SSH host to open its preview. URL and local hosts cannot supply an SSH route.</p>}
    {host && <PreviewProject key={`${host.id}:${host.runtimeId}`} host={host} onSubmit={onSubmit}/>}
  </div>;
}
function PreviewProject({ host, onSubmit }: { host: HostConnection; onSubmit(input: Omit<PreviewRequest, 'paneId'>): void }) {
  const runtime = useRuntime();
  const catalog = useSyncExternalStore(host.list?.subscribe ?? subscribeNone, host.list?.getSnapshot ?? emptyCatalog, host.list?.getSnapshot ?? emptyCatalog);
  const [cursor, setCursor] = useState<SessionCatalogPage['next_cursor']>();
  const [cwd, setCwd] = useState(''), [url, setURL] = useState('http://127.0.0.1:3000'), [error, setError] = useState('');
  const query = useQuery({ queryKey: ['browser-conversations', host.runtimeId, '', cursor],
    queryFn: ({ signal }) => host.client!.sessions.list({ status: 'all', cursor, limit: 64, max_bytes: 256 << 10 }, { signal: AbortSignal.any([signal, runtime.connections.signal(host.client!)]) }),
    enabled: !!host.client && host.state === 'connected' && (!!cursor || !host.list), gcTime: 0 });
  const page = cursor || !host.list ? query.data : catalog?.page;
  const projects = [...new Set((page?.items ?? []).slice(0, 64).map(row => row.cwd).filter(path => !!browserProjectId(path)))];
  const selected = projects.includes(cwd) ? cwd : '', failure = query.error ?? catalog?.error;
  return <form {...stylex.props(layout.column)} onSubmit={event => {
    event.preventDefault(); setError('');
    const projectId = browserProjectId(selected);
    if (!projectId || !host.runtimeId) return;
    try { onSubmit({ connectionId: host.profile.id, runtimeId: host.runtimeId, projectId, url: browserPreviewAddress(url) }); }
    catch (error) { setError(errorMessage(error)); }
  }}>
    <Field label="Project"><Select label="Project" value={selected} placeholder="Choose a project" options={projects.map(path => ({ value: path, label: path }))} onValueChange={setCwd}/></Field>
    <p {...stylex.props(layout.muted)}>Projects come from this host’s conversation catalog, at most 64 conversations per page. Choosing a project does not give any conversation Browser access.</p>
    {!projects.length && !failure && <p role="status">{query.isFetching || catalog?.status === 'loading' ? 'Loading projects…' : 'No project paths found. Start a conversation in the project on this host, then try again.'}</p>}
    {failure && <p role="alert">Could not load projects: {errorMessage(failure)}</p>}
    <div {...stylex.props(layout.row)}>
      {cursor && <Button type="button" variant="ghost" size="sm" onClick={() => { setCursor(undefined); setCwd(''); }}>First project page</Button>}
      {page?.next_cursor && <Button type="button" variant="ghost" size="sm" disabled={query.isFetching} onClick={() => { setCursor(page.next_cursor); setCwd(''); }}>More projects</Button>}
    </div>
    <Field label="Preview URL"><Input aria-label="Preview URL" value={url} onChange={event => { setURL(event.target.value); setError(''); }} placeholder="http://127.0.0.1:3000"/></Field>
    <p {...stylex.props(layout.muted)}>Use literal 127.0.0.1 or [::1]. Confirming a new port can expand network access for all Browser tabs in this project; native confirmation names the exact host and effect.</p>
    {error && <p role="alert">{error}</p>}
    <Button type="submit" disabled={!selected}>Continue to native confirmation</Button>
  </form>;
}
