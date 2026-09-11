import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { useLocation, useNavigate } from '@tanstack/react-router';
import type { WhipClient } from '@whip/sdk';
import { Button, Dialog, Field, Input, useToast, type MenuItem } from '@whip/ui';
import { useAppState, useRuntime } from './context';
import { errorMessage, readPreference, type ProjectEditor } from './platform';
import { sessionDestination, tabDestination } from './session-tab-routing';
import { selectedSessionTab } from './session-tabs';
import { ErrorNotice } from './error-feedback';

export interface SessionActionTarget {
  runtimeId: string;
  rootId: string;
  title: string;
  archived?: boolean;
}
type Action = 'rename' | 'fork' | 'delete' | 'ssh';
type Metadata = Awaited<ReturnType<WhipClient['sessions']['get']>>;
interface Selection { target: SessionActionTarget; hostName: string; client: WhipClient; action: Action; metadata?: Metadata; error?: string; errorType?: 'action' | 'validation' | 'resource' }
interface Actions { items(target: SessionActionTarget): MenuItem[]; prepare(open: boolean): void }
const ActionsContext = createContext<Actions | null>(null);
const aliasKey = (runtimeId: string) => `whip.web.editor-ssh.v1:${runtimeId}`;

/** One dialog owner outside virtual rows; every operation retains its original host/root. */
export function SessionActionsProvider({ children }: { children: ReactNode }) {
  const runtime = useRuntime();
  useAppState();
  const navigate = useNavigate();
  const location = useLocation();
  const locationRef = useRef(location);
  locationRef.current = location;
  const toast = useToast();
  const [selection, setSelection] = useState<Selection>();
  const [failure, setFailure] = useState<{ target: SessionActionTarget; title: string; error: unknown }>();
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState(false);
  const lock = useRef(false);
  const [editors, setEditors] = useState<readonly ProjectEditor[]>();
  const [editorError, setEditorError] = useState<string>();
  const request = useRef<AbortController | undefined>(undefined);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; request.current?.abort(); }; }, []);
  const alias = (runtimeId: string) => {
    const saved = readPreference<unknown>(runtime.platform.storage, aliasKey(runtimeId), '');
    return typeof saved === 'string' && /^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,252}$/.test(saved) ? saved : '';
  };
  function attached(target: SessionActionTarget, client?: WhipClient) {
    const host = runtime.connections.host(target.runtimeId);
    if (!host?.client || host.state !== 'connected' || (client && client !== host.client))
      throw new Error('This session’s host is disconnected or changed. Reconnect it and try again.');
    return host;
  }
  async function metadata(target: SessionActionTarget, client: WhipClient, signal?: AbortSignal) {
    attached(target, client);
    const result = await client.sessions.get(target.rootId, { signal });
    attached(target, client);
    return result;
  }
  function close() { if (!lock.current) { request.current?.abort(); setSelection(undefined); } }
  async function begin(target: SessionActionTarget, action: Action) {
    if (lock.current) return;
    const origin = locationRef.current;
    request.current?.abort();
    const abort = new AbortController();
    request.current = abort;
    setFailure(undefined);
    let next: Selection | undefined;
    try {
      const host = attached(target);
      const client = host.client!;
      next = { target, hostName: host.name, client, action };
      setSelection(next);
      setValue(action === 'ssh' ? alias(target.runtimeId) : '');
      if (action === 'ssh') return;
      const data = await metadata(target, client, abort.signal);
      if (abort.signal.aborted) return;
      setSelection({ ...next, metadata: data });
      setValue(data.title);
      if (action === 'fork') await fork(next, data, origin);
    } catch (error) {
      if (abort.signal.aborted || !mounted.current) return;
      if (next) setSelection({ ...next, error: errorMessage(error), errorType: 'resource' });
      else setFailure({ target, title: 'Could not open session action', error });
    }
  }
  async function perform(work: () => Promise<void>, target?: SessionActionTarget, title = 'Session action failed') {
    if (lock.current) return;
    lock.current = true;
    setBusy(true); setFailure(undefined);
    setSelection(current => current ? { ...current, error: undefined, errorType: undefined } : current);
    try { await work(); }
    catch (error) {
      if (mounted.current) setSelection(current => current ? { ...current, error: errorMessage(error) } : current);
      if (mounted.current && target) setFailure({ target, title, error });
    } finally { lock.current = false; if (mounted.current) setBusy(false); }
  }
  async function fork(current: Selection, data: Metadata, origin: typeof location) {
    await perform(async () => {
      if (runtime.tabs.workspace().tabs.length >= 32) throw new Error('Close a tab before forking: there are already 32 open session tabs.');
      attached(current.target, current.client);
      const result = await runtime.run(current.client.session(current.target.rootId).fork({ expected_revision: data.history_revision }), 'Fork session');
      const rootId = result.result?.root_id;
      if (!rootId) throw new Error('The host did not return the new session. Check command recovery before trying again.');
      attached(current.target, current.client);
      // A late result stays available in the catalog without stealing navigation.
      if (mounted.current && locationRef.current === origin) {
        const id = runtime.tabs.open(current.target.runtimeId, rootId);
        setSelection(undefined);
        await navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: current.target.runtimeId, rootId }, search: {}, state: { whipViewId: id } });
      } else setSelection(undefined);
    });
  }
  async function submit() {
    if (!selection) return;
    const current = selection;
    if (current.action === 'ssh' && !/^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,252}$/.test(value.trim())) {
      setSelection({ ...current, errorType: 'validation', error: 'Enter an SSH host alias from your SSH config, such as gpu-4090-sam.' });
      return;
    }
    await perform(async () => {
      const { target, client, action } = current;
      attached(target, client);
      if (action === 'ssh') {
        const name = value.trim();
        runtime.platform.storage.setItem(aliasKey(target.runtimeId), JSON.stringify(name));
      } else if (action === 'rename') {
        await runtime.run(client.session(target.rootId).rename(value.trim()), 'Rename session');
        runtime.tabs.titles(target.runtimeId, new Map([[target.rootId, value.trim()]]));
      } else if (action === 'delete') {
        await runtime.run(client.session(target.rootId).delete(), 'Delete session');
        const currentRoute = sessionDestination(locationRef.current.pathname);
        runtime.forgetSession(target.runtimeId, target.rootId);
        if (currentRoute?.runtimeId === target.runtimeId && currentRoute.rootId === target.rootId) {
          const next = selectedSessionTab(runtime.tabs.workspace());
          if (next) await navigate({ ...tabDestination(next), replace: true });
          else await navigate({ to: '/', replace: true });
        }
      }
      setSelection(undefined);
    });
  }
  async function archive(target: SessionActionTarget, archived: boolean) {
    await perform(async () => {
      const client = attached(target).client!;
      await runtime.run(client.session(target.rootId).archive(archived), archived ? 'Archive session' : 'Restore session');
      toast.add({ title: archived ? 'Session archived' : 'Session restored', description: archived
        ? <Button variant="ghost" onClick={() => void archive(target, false)}>Undo archive</Button>
        : 'This session is back in the sidebar.' });
    }, target, archived ? 'Could not archive session' : 'Could not restore session');
  }
  async function openDirectory(target: SessionActionTarget, app?: ProjectEditor['id']) {
    await perform(async () => {
      const host = attached(target);
      const data = await metadata(target, host.client!);
      if (!data.cwd) throw new Error('This session has no working directory.');
      if (app) await runtime.platform.projectEditors!.open({ app, directory: data.cwd, connectionId: host.id, runtimeId: target.runtimeId, sshAlias: host.profile.target.kind === 'local' ? undefined : alias(target.runtimeId) || undefined });
      else { await runtime.platform.copy(data.cwd); toast.add({ title: 'Directory copied' }); }
    }, target, app ? 'Could not open directory' : 'Could not copy directory');
  }
  const prepare = (open: boolean) => {
    if (!open || !runtime.platform.projectEditors) return;
    setEditorError(undefined);
    void runtime.platform.projectEditors.list().then(value => { if (mounted.current) setEditors(value); }).catch(error => { if (mounted.current) setEditorError(errorMessage(error)); });
  };
  function items(target: SessionActionTarget): MenuItem[] {
    const host = runtime.connections.host(target.runtimeId);
    const disabled = busy || host?.state !== 'connected';
    const remote = host?.profile.target.kind !== 'local';
    const ssh = host?.profile.target.kind === 'ssh' ? host.profile.target : undefined;
    const usableAlias = ssh && !ssh.user && !ssh.port && !ssh.identityFile && /^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$/.test(ssh.host);
    const needsAlias = remote && !alias(target.runtimeId) && !usableAlias;
    const native: MenuItem[] = runtime.platform.projectEditors ? [
      ...(editors ?? []).map(editor => ({ id: editor.id, label: `${editor.label}${!editor.installed ? ' (not installed)' : editor.id === 'finder' && remote ? ' (local folders only)' : needsAlias ? ' (configure SSH first)' : ''}`,
        disabled: disabled || !editor.installed || (editor.id === 'finder' && remote) || needsAlias, onSelect: () => void openDirectory(target, editor.id) })),
      ...(editorError ? [{ id: 'editor-error', label: <span role="status" data-error-type="resource" data-error-owner="installed-editors">Could not load installed editors. Reopen this menu to retry.</span>, disabled: true }]
        : !editors ? [{ id: 'loading', label: 'Finding installed editors…', disabled: true }] : []),
      ...(remote ? [{ id: 'ssh', label: 'Configure SSH for editors…', disabled, onSelect: () => void begin(target, 'ssh') }] : []),
    ] : [];
    return [
      { id: 'background', label: 'Open in background tab', disabled, onSelect: () => { try { runtime.tabs.open(target.runtimeId, target.rootId, target.title); } catch (error) { setFailure({ target, title: 'Could not open session tab', error }); } } },
      { id: 'open-in', label: 'Open in', disabled, items: [...native, { id: 'copy-directory', label: 'Copy directory', disabled, onSelect: () => void openDirectory(target) }] },
      { id: 'rename', label: 'Rename', disabled, onSelect: () => void begin(target, 'rename') },
      { id: 'fork', label: 'Fork', disabled, onSelect: () => void begin(target, 'fork') },
      { id: 'archive', label: target.archived ? 'Restore' : 'Archive', disabled, onSelect: () => void archive(target, !target.archived) },
      { id: 'destructive-separator', label: '', separator: true },
      { id: 'delete', label: 'Delete…', danger: true, disabled, onSelect: () => void begin(target, 'delete') },
    ];
  }
  const title = selection?.action === 'rename' ? 'Rename session' : selection?.action === 'delete' ? 'Delete this session?' : selection?.action === 'ssh' ? 'SSH for editors' : 'Fork session';
  return <ActionsContext.Provider value={{ items, prepare }}>{children}
    <Dialog open={!!selection} onOpenChange={open => { if (!open) close(); }} title={title}
      description={selection?.action === 'delete' ? 'This permanently deletes the session, its owned work, and unsent drafts. Running work will stop. Files in the working directory stay on disk.' : selection?.action === 'ssh' ? 'Use an SSH config alias on this computer. The editor handles SSH authentication. This setting applies to this Whip host on this device.' : undefined}
      footer={<><Button onClick={close} disabled={busy}>Close</Button>{selection?.action !== 'fork' && <Button variant={selection?.action === 'delete' ? 'danger' : 'primary'} loading={busy}
        disabled={busy || (selection?.action !== 'ssh' && !selection?.metadata) || (selection?.action !== 'delete' && !value.trim())}
        onClick={() => void submit()}>{selection?.action === 'delete' ? 'Delete session' : 'Save'}</Button>}</>}>
      {selection?.error && <ErrorNotice type={selection.errorType ?? "action"} owner={`${selection.target.runtimeId}:${selection.target.rootId}`} error={selection.error}
        action={selection.errorType === "resource" && <Button variant="ghost" onClick={() => void begin(selection.target, selection.action)}>Retry loading session</Button>} />}
      {selection && <p>{selection.target.title || 'Untitled session'} · {selection.hostName}</p>}
      {selection?.action !== 'ssh' && !selection?.metadata && !selection?.error && <p role="status">Loading session details…</p>}
      {(selection?.action === 'rename' && selection.metadata || selection?.action === 'ssh') && <Field label={selection.action === 'ssh' ? 'SSH host alias' : 'Session name'}><Input value={value} onChange={event => setValue(event.target.value)} maxLength={selection.action === 'ssh' ? 253 : 65536} disabled={busy} autoFocus onKeyDown={event => { if (event.key === 'Enter' && !event.nativeEvent.isComposing && value.trim()) void submit(); }} /></Field>}
      {selection?.action === 'delete' && selection.metadata && <><p>{selection.metadata.title || 'Untitled session'}</p><p>Host: {selection.hostName}</p></>}
      {selection?.action === 'fork' && busy && <p role="status">Copying committed conversation history in the same working directory…</p>}
    </Dialog>
    <Dialog open={!!failure} title={failure?.title ?? "Session action failed"} onOpenChange={open => { if (!open) setFailure(undefined); }}
      footer={<Button onClick={() => setFailure(undefined)}>Close</Button>}>
      {failure && <><p>{failure.target.title || "Untitled session"} · {runtime.connections.host(failure.target.runtimeId)?.name ?? failure.target.runtimeId}</p>
        <ErrorNotice type="action" owner={`${failure.target.runtimeId}:${failure.target.rootId}`} error={failure.error} /></>}
    </Dialog>
  </ActionsContext.Provider>;
}

export function useSessionActions() {
  const actions = useContext(ActionsContext);
  if (!actions) throw new Error('Session action provider is missing');
  return actions;
}
