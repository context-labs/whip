import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import { useWhipConnection } from '@whip/sdk/react';
import { useQuery } from '@tanstack/react-query';
import { Button, Select, Textarea } from '@whip/ui';
import { ArrowUp, FolderOpen } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { layout } from './styles';
import type { WhipClient } from '@whip/sdk';
import { HostSelector } from './host-selector';
import { HostDialog, LocalRuntimeSetup } from './host-dialog';
import type { HostConnection } from './hosts';
import { DirectoryPicker } from './directory-picker';
import { sessionSearch, type NewChatTab } from './session-tabs';
import { ProviderSetup } from './provider-setup';
import { useProviderConnections } from './settings/provider-connections';
import { ProviderLogo } from './provider-logo';
import { PermissionModeControl } from './permission-mode';
import { welcomeDraftKey } from './welcome-submission';
import { errorMessage } from './platform';
import { ErrorNotice } from './error-feedback';
import { WelcomeRecovery } from './welcome-recovery';
import { definitionOptions, useDefinitions } from './definitions';

export function Welcome({ tab, focused = true }: { tab: NewChatTab; focused?: boolean }) {
  const runtime = useRuntime();
  const { hosts } = useAppState();
  useSyncExternalStore(runtime.welcome.subscribe, runtime.welcome.getSnapshot);
  const [changingHost, setChangingHost] = useState(false);
  const [adding, setAdding] = useState(false);
  const [hostSelectionError, setHostSelectionError] = useState<unknown>();
  const selected = tab.hostProfileId ?? tab.runtimeId;
  const host = hosts.find(host => host.id === selected || host.runtimeId === selected);
  let locked = false;
  try { locked = !!runtime.welcome.get(tab.id); } catch { locked = true; }
  const selectHost = (id: string) => {
    if (locked) return;
    const next = hosts.find(host => host.id === id);
    try { runtime.tabs.updateNew(tab.id, { hostProfileId: id, runtimeId: next?.runtimeId }); setChangingHost(false); setHostSelectionError(undefined); }
    catch (error) { setHostSelectionError(error); }
  };
  return <div {...stylex.props(layout.empty, styles.page)}>
    <h1 {...stylex.props(layout.emptyTitle)}>What would you like to work on?</h1>
    <div {...stylex.props(styles.column)}>
      {host && runtime.platform.localRuntime && host.profile?.target.kind === 'local' && host.state !== 'connected'
        ? <LocalRuntimeSetup host={host} />
        : host?.client ? <NewSession key={`${tab.id}:${host.id}`} tab={tab} focused={focused} client={host.client} host={host} />
        : host ? <><p role="status">{host.name} is {host.state === 'closed' ? 'disconnected' : host.state}.</p><Button onClick={() => void runtime.connections.connect(host.id).catch(() => {})}>Connect {host.name}</Button></>
        : <p>Select or add an execution host to begin.</p>}
      <div {...stylex.props(styles.host)}><span>{host?.name ?? 'Execution host'}</span><Button variant="ghost" disabled={locked} onClick={() => setChangingHost(value => !value)}>Change host</Button></div>
      {changingHost && <div {...stylex.props(layout.column)}>
        {!!hosts.length && <HostSelector hosts={hosts} host={host} onValueChange={selectHost} />}
        <Button variant="ghost" onClick={() => setAdding(true)}>Connect to another machine</Button>
      </div>}
<ErrorNotice type="action" owner={tab.id} title="Could not change host" error={hostSelectionError} />
    </div>
    <WelcomeRecovery currentId={tab.id} />
    <HostDialog open={adding} onOpenChange={setAdding} onSaved={selectHost} />
  </div>;
}

function NewSession({ client, host, tab, focused }: { client: WhipClient; host: HostConnection; tab: NewChatTab; focused: boolean }) {
  const connection = useWhipConnection(client);
  if (!connection.info?.runtime_id) return <p role="status">Connecting to {host.name}…</p>;
  return <WelcomeComposer client={client} host={host} tab={tab} focused={focused} />;
}

/** Editable state belongs to the stable workspace draft, not the selected host. */
export function WelcomeComposer({ client, host, tab, focused = true }: {
  client: WhipClient; host: HostConnection; tab: NewChatTab; focused?: boolean;
}) {
  const runtime = useRuntime();
  const app = useAppState();
  useSyncExternalStore(runtime.welcome.subscribe, runtime.welcome.getSnapshot);
  const connection = useWhipConnection(client);
  const runtimeId = connection.info?.runtime_id ?? host.runtimeId!;
  const key = welcomeDraftKey(tab.id);
  const navigate = useNavigate();
  const draft = useSyncExternalStore(listener => runtime.subscribeDraft(key, listener), () => runtime.draft(key));
  const cwd = tab.cwd;
  const permission = tab.permissionMode;
  function updateSetup(patch: Parameters<typeof runtime.tabs.updateNew>[1]) {
    try { runtime.tabs.updateNew(tab.id, patch); } catch (error) { setError(errorMessage(error)); }
  }
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [showProviders, setShowProviders] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const isFocused = useRef(focused);
  isFocused.current = focused;
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const connected = connection.state === 'connected';
  const engines = connection.info?.execution_engines;
  const configuration = useQuery({ queryKey: ['runtime-configuration', runtimeId], queryFn: ({ signal }) => client.configuration.get({ signal }), enabled: connected });
  const providers = useProviderConnections(client, connected);
  const selection = providers.inventory.data?.selection;
  const ready = selection?.ready === true;
  const requiresUpdate = !!providers.inventory.data && !selection;
  const previous = runtime.lastSession();
  const previousView = previous ? runtime.tabs.preferred(previous.runtimeId, previous.rootId) : undefined;
  let recovery: ReturnType<typeof runtime.welcome.get>;
  let recoveryError = '';
  try { recovery = runtime.welcome.get(tab.id); } catch (error) { recoveryError = errorMessage(error); }
  const executionEngine = recovery?.params?.execution_engine ?? tab.executionEngine ?? configuration.data?.default_execution_engine ?? connection.info?.default_execution_engine ?? 'starlark';
  const engineOptions = (engines ?? []).filter(engine => engine.id === 'starlark' || engine.id === 'quickjs')
    .map(engine => ({ value: engine.id, label: engine.label }));
  const engineAvailable = engineOptions.some(engine => engine.value === executionEngine);
  // The agent definition the session runs. Older hosts do not advertise the
  // registry; they run the coding agent and the picker stays hidden.
  const definitions = useDefinitions(client, connected);
  const definition = recovery?.params?.definition ?? tab.definition ?? 'coding';
  const definitionChoices = definitionOptions(definitions.query.data?.items);
  const definitionAvailable = !definitions.supported || definitionChoices.some(choice => choice.value === definition);
  const unresolved = app.commands.find(command => command.draftKey === key && command.delivery);
  function openProviders() { setShowProviders(true); requestAnimationFrame(() => { if (!isFocused.current) return; const setup = panel.current?.querySelector<HTMLElement>('[aria-label="Provider setup"]'); setup?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' }); (setup?.querySelector<HTMLButtonElement>('[data-provider-confirm]:not(:disabled)') ?? setup?.querySelector<HTMLButtonElement>('[data-provider-choice]'))?.focus(); }); }
  function focusComposer() { setShowProviders(false); requestAnimationFrame(() => { if (isFocused.current) input.current?.focus(); }); }
  async function submit(mode: 'start' | 'check' | 'retry' = 'start') {
    if (!connected || busy) return;
    if (mode === 'start' && requiresUpdate) return;
    if (mode === 'start' && !engineAvailable) { setError('This host does not advertise the selected execution language. Reconnect to an updated host or select an available language.'); return; }
    if (mode === 'start' && definitions.query.data && !definitionAvailable) { setError(`This host has no agent definition named ${definition}. Choose an available agent.`); return; }
    if (mode === 'start' && !ready) { openProviders(); return; }
    if (mode === 'start' && !cwd.trim()) { setError('Choose a project folder on this host before sending.'); return; }
    setBusy(true); setError('');
    try {
      mode === 'start'
        ? await runtime.welcome.start(tab.id, client, { cwd: cwd.trim(), model: selection!.model, provider: selection!.provider, permission_mode: permission, execution_engine: executionEngine, ...(definitions.supported && tab.definition ? { definition: tab.definition } : {}) })
        : await runtime.welcome.resume(tab.id, client, mode === 'retry');
    } catch (error) { if (mounted.current) setError(errorMessage(error)); }
    finally { if (mounted.current) setBusy(false); }
  }
  return <div ref={panel} {...stylex.props(layout.column)}>
    {previous && previous.runtimeId === runtimeId && <Link to="/h/$runtimeId/s/$rootId" params={previous} search={sessionSearch(previousView)} state={{ whipViewId: previousView?.id }}>Continue your previous session</Link>}
    <form onSubmit={event => { event.preventDefault(); void submit(); }} {...stylex.props(styles.composer)}>
      <Textarea ref={input} autoFocus={focused} data-whip-composer aria-label="Your first message" placeholder="Describe a task…" rows={3} xstyle={styles.input}
        value={draft} disabled={busy || !!recovery || !!recoveryError} maxLength={256 * 1024} onChange={event => { try { runtime.setDraft(key, event.target.value); } catch (error) { setError(errorMessage(error)); } }}
        onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey && !event.altKey && !event.ctrlKey && !event.metaKey && !event.nativeEvent.isComposing) { event.preventDefault(); if (!recovery) void submit(); } }} />
      <div {...stylex.props(styles.toolbar)}>
        {cwd && <span title={cwd} {...stylex.props(styles.folder)}><FolderOpen size={14} />{cwd.split('/').filter(Boolean).at(-1) ?? cwd}</span>}
        <DirectoryPicker compact client={client} native={host.local} pickDirectory={host.profile?.target.kind === 'local' ? runtime.platform.pickDirectory : undefined}
          value={cwd} onSelect={path => { updateSetup({ cwd: path }); setError(''); }} disabled={!connected || busy || !!recovery} />
      </div>
      <div {...stylex.props(styles.toolbar)}>
        <Select label="Execution language" value={executionEngine} disabled={!connected || busy || !!recovery || !!recoveryError || !engines?.length}
          options={engineAvailable ? engineOptions : [...engineOptions, { value: executionEngine, label: `${executionEngine === 'quickjs' ? 'JavaScript (QuickJS)' : 'Starlark'} (unavailable)`, disabled: true }]}
          onValueChange={executionEngine => updateSetup({ executionEngine: executionEngine as NewChatTab['executionEngine'] })} />
        <span {...stylex.props(styles.note)}>Fixed for this session and its child agents.</span>
      </div>
      {definitions.supported && <div {...stylex.props(styles.toolbar)}>
        <Select label="Agent" value={definition} disabled={!connected || busy || !!recovery || !!recoveryError || !definitionChoices.length}
          options={definitionAvailable ? definitionChoices : [...definitionChoices, { value: definition, label: `${definition} (unavailable)`, disabled: true }]}
          onValueChange={definition => updateSetup({ definition })} />
        <span {...stylex.props(styles.note)}>Its persona, tools, and children define this session. Manage agents in Settings.</span>
      </div>}
      <div {...stylex.props(styles.toolbar)}>
        <Button variant="ghost" disabled={!connected || busy} onClick={() => showProviders ? setShowProviders(false) : openProviders()} aria-label={ready ? 'Change provider or model' : 'Connect a provider'}>
          {ready && <ProviderLogo id={selection.provider} size={14} />}<span {...stylex.props(layout.ellipsis)}>{ready ? `${selection.model} · ${selection.provider}` : 'Connect a provider'}</span>
        </Button>
        <PermissionModeControl value={permission} disabled={busy || !!recovery} onChange={permissionMode => updateSetup({ permissionMode: permissionMode as NewChatTab['permissionMode'] })} />
        <span {...stylex.props(layout.grow)} />
        <Button type="submit" variant="primary" aria-label={ready ? 'Send first message' : 'Set up provider'} xstyle={ready ? styles.send : undefined}
          loading={busy} disabled={!connected || requiresUpdate || !engineAvailable || !!recovery || !!recoveryError || (ready && (!draft.trim() || !cwd.trim()))}>{ready ? <ArrowUp size={16} /> : 'Connect'}</Button>
      </div>
    </form>
    {(recovery || recoveryError) && <div data-error-type="submission" data-error-owner={key} role="status" {...stylex.props(layout.notice, layout.column)}>
      <span>{recoveryError || recovery?.error || (recovery?.state === 'accepted' ? 'Your first message was accepted. Continue the created session.' : 'Your first message has a saved recovery record. Check its status before sending again.')}</span>
      {recovery && <div {...stylex.props(layout.row)}>
        {recovery.state !== 'failed' && <Button disabled={!connected || busy} onClick={() => void submit('check')}>{recovery.state === 'accepted' ? 'Continue session' : 'Check first message'}</Button>}
        {(recovery.state === 'absent' || unresolved?.delivery === 'absent') && <Button variant="secondary" disabled={!connected || busy} onClick={() => void submit('retry')}>Retry original request</Button>}
        {recovery.state === 'failed' && <Button variant="secondary" disabled={busy} onClick={() => { void runtime.welcome.finish(tab.id, recovery!.create.commandId).then(() => setError('')).catch(error => setError(errorMessage(error))); }}>Discard failed request</Button>}
        {recovery.rootId && <Button variant="ghost" onClick={() => void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: recovery!.create.runtimeId, rootId: recovery!.rootId! }, search: {} })}>Open created session</Button>}
      </div>}
    </div>}
    {error && !recovery && !recoveryError && <ErrorNotice type="submission" owner={key} error={error} />}
    {!connected && <p role="status" {...stylex.props(styles.note)}>Reconnecting to {host.name}. Your draft stays here and will not be sent automatically.</p>}
    {(!ready || showProviders) && <ProviderSetup client={client} enabled={connected && !busy} hostName={host.name} connections={providers} onReady={focusComposer} />}
  </div>;
}

const styles = stylex.create({
  page: { justifyContent: 'flex-start', paddingTop: { default: 'min(12vh, 100px)', [scale.phone]: 32 }, overflowY: 'auto' },
  column: { width: 'min(100%, 620px)', textAlign: 'left', display: 'flex', flexDirection: 'column', gap: scale.space4 },
  composer: { display: 'flex', flexDirection: 'column', gap: scale.space2, padding: scale.space3, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 20, backgroundColor: colors.element },
  input: { minHeight: 96, maxHeight: 220, resize: 'vertical', borderWidth: 0, boxShadow: 'none', backgroundColor: { default: 'transparent', ':hover': 'transparent' }, fontSize: { default: typography.size14, [scale.phone]: typography.size16 }, padding: scale.space1 },
  toolbar: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space1, minWidth: 0 },
  folder: { display: 'flex', alignItems: 'center', gap: scale.space2, color: surface.secondaryText, fontSize: typography.size12, maxWidth: '100%', overflowWrap: 'anywhere' },
  send: { borderRadius: '50%', width: 36, paddingInline: 0 },
  host: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', color: surface.secondaryText, fontSize: typography.size12 },
  note: { fontSize: typography.size12, color: surface.secondaryText, margin: 0, lineHeight: 1.5 },
});
