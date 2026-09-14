import { useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useWhipConnection } from '@whip/sdk/react';
import { useQuery } from '@tanstack/react-query';
import { Button, Dialog, Field, IconButton, Select, Textarea } from '@whip/ui';
import { ArrowUp, AtSign, Monitor, Paperclip, SlidersHorizontal } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { layout } from './styles';
import type { WhipClient } from '@whip/sdk';
import { WelcomeHostPicker } from './welcome-host-picker';
import { HostDialog, LocalRuntimeSetup } from './host-dialog';
import type { HostConnection } from './hosts';
import { DirectoryPicker } from './directory-picker';
import type { NewChatTab } from './session-tabs';
import { ProviderSetup } from './provider-setup';
import { useProviderConnections } from './settings/provider-connections';
import { CatalogModelPicker, DraftEffortPicker, catalogModels, modelEfforts, useProviderCatalog } from './model-selection';
import { PermissionModeControl } from './permission-mode';
import { welcomeDraftKey } from './welcome-submission';
import { errorMessage } from './platform';
import { ErrorNotice } from './error-feedback';
import { SessionInfoBar } from './session-info-bar';
import { WelcomeRecovery } from './welcome-recovery';
import { definitionOptions, useDefinitions } from './definitions';

export function Welcome({ tab, focused = true }: { tab: NewChatTab; focused?: boolean }) {
  const runtime = useRuntime();
  const navigate = useNavigate();
  const { hosts } = useAppState();
  useSyncExternalStore(runtime.welcome.subscribe, runtime.welcome.getSnapshot);
  const [adding, setAdding] = useState(false);
  const [hostSelectionError, setHostSelectionError] = useState<unknown>();
  const selected = tab.hostProfileId ?? tab.runtimeId;
  const host = hosts.find(host => host.id === selected || host.runtimeId === selected);
  let locked = false;
  try { locked = !!runtime.welcome.get(tab.id); } catch { locked = true; }
  const selectHost = (id: string) => {
    if (locked || id === host?.id) return;
    const next = hosts.find(host => host.id === id);
    try {
      runtime.tabs.updateNew(tab.id, { hostProfileId: id, runtimeId: next?.runtimeId, cwd: '', model: undefined, provider: undefined, effort: undefined });
      setHostSelectionError(undefined);
      if (next && next.state !== 'connected') void runtime.connections.connect(id).catch(() => {});
    } catch (error) { setHostSelectionError(error); }
  };
  const hostControl = <WelcomeHostPicker hosts={hosts} host={host} disabled={locked} onSelect={selectHost}
    onManage={() => void navigate({ to: '/settings', search: { section: 'connections' } })} />;
  return <><SessionInfoBar kind="new" host={host?.name ?? 'Choose a host'} cwd={tab.cwd} />
    <div {...stylex.props(styles.page)}><div {...stylex.props(styles.column)}>
      {host && runtime.platform.localRuntime && host.profile?.target.kind === 'local' && host.state !== 'connected'
        ? <><h1 {...stylex.props(styles.heading)}>What do you want to work on?</h1><LocalRuntimeSetup host={host} />{hostControl}</>
        : host?.client ? <NewSession key={`${tab.id}:${host.id}`} tab={tab} focused={focused} client={host.client} host={host} hostControl={hostControl} onConnectRemote={() => setAdding(true)} />
        : <><h1 {...stylex.props(styles.heading)}>What do you want to work on?</h1><p role="status">{host ? `${host.name} is ${host.state === 'closed' ? 'disconnected' : host.state}.` : 'Select or add an execution host to begin.'}</p>
          <div {...stylex.props(styles.toolbar)}>{hostControl}{host
            ? <Button onClick={() => void runtime.connections.connect(host.id).catch(() => {})}>Connect {host.name}</Button>
            : <Button onClick={() => setAdding(true)}>Add server</Button>}</div></>}
      <ErrorNotice type="action" owner={tab.id} title="Could not change host" error={hostSelectionError} />
      <WelcomeRecovery currentId={tab.id} />
    </div></div>
    <HostDialog open={adding} onOpenChange={setAdding} onSaved={selectHost} />
  </>;
}

function NewSession({ client, host, tab, focused, hostControl, onConnectRemote }: { client: WhipClient; host: HostConnection; tab: NewChatTab; focused: boolean; hostControl: ReactNode; onConnectRemote(): void }) {
  const connection = useWhipConnection(client);
  if (!connection.info?.runtime_id) return <><h1 {...stylex.props(styles.heading)}>What do you want to work on?</h1><p role="status">Connecting to {host.name}…</p>{hostControl}</>;
  return <WelcomeComposer client={client} host={host} tab={tab} focused={focused} hostControl={hostControl} onConnectRemote={onConnectRemote} />;
}

/** Editable state belongs to the stable workspace draft, not the selected host. */
export function WelcomeComposer({ client, host, tab, focused = true, hostControl, onConnectRemote }: {
  client: WhipClient; host: HostConnection; tab: NewChatTab; focused?: boolean; hostControl?: ReactNode; onConnectRemote?(): void;
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
  const [showOptions, setShowOptions] = useState(false);
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
  const catalog = useProviderCatalog(client, connected);
  const model = tab.model ?? selection?.model ?? '';
  const provider = tab.provider ?? selection?.provider ?? '';
  const route = providers.inventory.data?.providers?.find(item => item.id === provider);
  const ready = tab.model ? !!route?.status.available && !route.status.disabled : selection?.ready === true;
  const levels = modelEfforts(catalogModels(catalog.data?.result, provider), model);
  const requestedEffort = tab.effort ?? configuration.data?.default_effort ?? 'off';
  const effort = tab.effort ?? (levels.includes(requestedEffort) ? requestedEffort : 'off');
  const effortAvailable = tab.effort === undefined || levels.includes(effort);
  const requiresUpdate = !!providers.inventory.data && !selection;
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
    if (!connected || busy || (mode === 'start' && (!draft.trim() || recovery || recoveryError))) return;
    if (mode === 'start' && requiresUpdate) return;
    if (mode === 'start' && !engineAvailable) { setError('This host does not advertise the selected execution language. Reconnect to an updated host or select an available language.'); return; }
    if (mode === 'start' && definitions.query.data && !definitionAvailable) { setError(`This host has no agent definition named ${definition}. Choose an available agent.`); return; }
    if (mode === 'start' && !ready) { openProviders(); return; }
    if (mode === 'start' && !effortAvailable) { setError('Choose an available reasoning effort for this model before sending.'); return; }
    if (mode === 'start' && !cwd.trim()) { setError('Choose a project folder on this host before sending.'); return; }
    setBusy(true); setError('');
    try {
      mode === 'start'
        ? await runtime.welcome.start(tab.id, client, { cwd: cwd.trim(), model, provider, permission_mode: permission, execution_engine: executionEngine, ...(definitions.supported && tab.definition ? { definition: tab.definition } : {}) }, { effort: tab.effort === undefined ? undefined : effort })
        : await runtime.welcome.resume(tab.id, client, mode === 'retry');
    } catch (error) { if (mounted.current) setError(errorMessage(error)); }
    finally { if (mounted.current) setBusy(false); }
  }
  const disabled = !connected || busy || !!recovery || !!recoveryError;
  const setupVisible = (!ready || showProviders) && !recovery && !recoveryError;
  return <><h1 {...stylex.props(styles.heading)}>{setupVisible ? 'Connect a provider to get started' : 'What do you want to work on?'}</h1>
  <div ref={panel} {...stylex.props(styles.content)}>
    {!setupVisible && <><form onSubmit={event => { event.preventDefault(); void submit(); }} {...stylex.props(styles.composer)}>
      <Textarea ref={input} autoFocus={focused} data-whip-composer aria-label="Your first message" placeholder="Describe a task…" rows={3} xstyle={styles.input}
        value={draft} disabled={busy || !!recovery || !!recoveryError} maxLength={256 * 1024} onChange={event => { try { runtime.setDraft(key, event.target.value); } catch (error) { setError(errorMessage(error)); } }}
        onKeyDown={event => { if (event.key === 'Enter' && !event.shiftKey && !event.altKey && !event.ctrlKey && !event.metaKey && !event.nativeEvent.isComposing) { event.preventDefault(); if (!recovery) void submit(); } }} />
      <div {...stylex.props(styles.toolbar, styles.composerToolbar)}>
        <IconButton variant="ghost" label="Attach text or images" title="Attachments are available after the session starts" disabled><Paperclip size={15} /></IconButton>
        <IconButton variant="ghost" label="Add context" title="Context suggestions are available after the session starts" disabled><AtSign size={16} /></IconButton>
        <span {...stylex.props(layout.grow)} />
        <PermissionModeControl value={permission} disabled={disabled} onChange={permissionMode => updateSetup({ permissionMode: permissionMode as NewChatTab['permissionMode'] })} />
        {ready ? <CatalogModelPicker model={model} provider={provider} catalog={catalog.data?.result} loading={catalog.isFetching}
          error={connected ? catalog.error?.message : undefined} onRetry={() => void catalog.refetch()} disabled={disabled}
          onChange={(model, provider) => updateSetup({ model, provider, effort: modelEfforts(catalogModels(catalog.data?.result, provider), model).includes(effort) ? effort : 'off' })}
          footer={<Button variant="ghost" onClick={() => setShowOptions(true)}><SlidersHorizontal size={14} />Session options</Button>} />
          : <Button variant="ghost" disabled={disabled} onClick={openProviders}>Connect a provider</Button>}
        <DraftEffortPicker value={effort} levels={levels} disabled={disabled || !ready || catalog.isPending} onChange={effort => updateSetup({ effort })} />
        <Button type="submit" variant="primary" aria-label="Send first message" xstyle={styles.send} loading={busy}
          disabled={disabled || requiresUpdate || !engineAvailable || !effortAvailable || !ready || !draft.trim() || !cwd.trim()}><ArrowUp size={16} /></Button>
      </div>
    </form>
    <div {...stylex.props(styles.toolbar)}>
      {hostControl}
      <DirectoryPicker sessionTrigger client={client} native={host.local} pickDirectory={host.profile?.target.kind === 'local' ? runtime.platform.pickDirectory : undefined}
        host={{ name: host.name, list: host.list, detail: host.profile.target.kind === 'ssh' ? [host.profile.target.user, host.profile.target.host].filter(Boolean).join('@') : host.endpoint,
          reconnect: () => runtime.connections.connect(host.id) }}
        value={cwd} onSelect={path => { updateSetup({ cwd: path }); setError(''); }} disabled={disabled} />
    </div>
    </>}
    <Dialog open={showOptions} onOpenChange={setShowOptions} title="Session options">
      <Field label="Execution language" description="Fixed once the session starts."><Select label="Execution language" value={executionEngine}
        disabled={disabled || !engines?.length} options={engineAvailable ? engineOptions : [...engineOptions, { value: executionEngine, label: `${executionEngine} (unavailable)`, disabled: true }]}
        onValueChange={executionEngine => updateSetup({ executionEngine: executionEngine as NewChatTab['executionEngine'] })} /></Field>
      {definitions.supported && <Field label="Agent"><Select label="Agent" value={definition} disabled={disabled || !definitionChoices.length}
        options={definitionAvailable ? definitionChoices : [...definitionChoices, { value: definition, label: `${definition} (unavailable)`, disabled: true }]}
        onValueChange={definition => updateSetup({ definition })} /></Field>}
    </Dialog>
    {!setupVisible && (!engineAvailable || !definitionAvailable) && <Button variant="ghost" onClick={() => setShowOptions(true)}>Review unavailable session options</Button>}
    {!setupVisible && !effortAvailable && !catalog.isPending && <p role="status" {...stylex.props(styles.note)}>Choose an available reasoning effort for this model before sending.</p>}
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
    {setupVisible && <ProviderSetup client={client} enabled={connected && !busy} hostName={host.name} connections={providers}
      actions={host.local ? <Button variant="ghost" disabled={busy || !!recovery || !!recoveryError} onClick={() => onConnectRemote ? onConnectRemote() : void navigate({ to: '/settings', search: { section: 'connections' } })}><Monitor size={14} />Connect Remote</Button> : hostControl}
      onReady={() => { updateSetup({ model: undefined, provider: undefined, effort: undefined }); focusComposer(); }} />}
  </div></>;
}

const styles = stylex.create({
  page: { display: 'flex', flex: 1, minHeight: 0, flexDirection: 'column', alignItems: 'center', overflowY: 'auto', paddingInline: { default: 32, [scale.phone]: 16 }, paddingTop: 32, paddingBottom: { default: 116, [scale.phone]: 32 } },
  column: { width: 'min(100%, 620px)', minWidth: 0, marginBlock: 'auto', display: 'flex', flexDirection: 'column', gap: scale.space4 },
  heading: { fontSize: typography.size24, fontWeight: 550, lineHeight: '32px', letterSpacing: '-0.025em', margin: 0 },
  content: { display: 'flex', flexDirection: 'column', gap: scale.space2, minWidth: 0 },
  composer: { display: 'flex', flexDirection: 'column', gap: scale.space2, padding: scale.space3, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 20, backgroundColor: colors.element },
  input: { minHeight: 96, maxHeight: 220, resize: 'vertical', borderWidth: 0, boxShadow: 'none', outline: 'none', backgroundColor: { default: 'transparent', ':hover': 'transparent' }, fontSize: { default: typography.size14, [scale.phone]: typography.size16 }, padding: scale.space1 },
  toolbar: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space1, minWidth: 0 },
  composerToolbar: { justifyContent: 'flex-end' },
  send: { borderRadius: '50%', width: { default: 32, [scale.touch]: 44 }, paddingInline: 0 },
  note: { fontSize: typography.size12, color: surface.secondaryText, margin: 0, lineHeight: 1.5 },
});
