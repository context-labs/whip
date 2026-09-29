import { useCallback, useEffect, useRef, useState, useSyncExternalStore, type ReactNode, type RefObject } from 'react';
import { useNavigate } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { Button, Dialog, Field, IconButton, Select, Textarea } from '@whip/ui';
import { ArrowUp, AtSign, Monitor, Paperclip } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime } from './context';
import { layout } from './styles';
import type { Client } from '@whip/sdk';
import { WelcomeHostPicker } from './welcome-host-picker';
import { HostDialog, LocalRuntimeSetup } from './host-dialog';
import type { HostConnection } from './hosts';
import { DirectoryPicker } from './directory-picker';
import { useSkillCompletion } from './use-skill-completion';
import { welcomeDraftKey, type NewChatTab } from './session-tabs';
import { ProviderSetup } from './provider-setup';
import { useProviderConnections } from './settings/provider-connections';
import { CatalogModelPicker, DraftEffortPicker, PickerSkeletons, catalogModels, modelEfforts, useProviderCatalog } from './model-selection';
import { PermissionModeControl } from './permission-mode';
import { errorMessage } from './platform';
import { ErrorNotice } from './error-feedback';
import { SessionTopBar } from './session-top-bar';
import { definitionOptionValue, definitionOptions, parseDefinitionOption, useDefinitions } from './definitions';
import { MCPImportScreen, shouldOffer, useMCPImportCandidates } from './mcp-import';
import { ComposerAttachments } from './composer-attachments';
import { startNewChat } from './new-chat';
import { ChatDropSurface, ChatFileDrop } from './chat-file-drop';

export function Welcome({ tab, focused = true }: { tab: NewChatTab; focused?: boolean }) {
  const runtime = useRuntime();
  const navigate = useNavigate();
  const { hosts } = useAppState();
  const [adding, setAdding] = useState(false);
  const [hostSelectionError, setHostSelectionError] = useState<unknown>();
  const dropTarget = useRef<HTMLDivElement>(null);
  const column = useRef<HTMLDivElement>(null);
  const [providerTop, setProviderTop] = useState<number>();
  const onProviderExpandedChange = useCallback((expanded: boolean) => {
    // Keep the collapsed view's measured top spacing instead of re-centering a taller list.
    const top = expanded && column.current ? parseFloat(getComputedStyle(column.current).marginTop) : undefined;
    setProviderTop(top !== undefined && Number.isFinite(top) ? top : undefined);
  }, []);
  const { sending } = useSyncExternalStore(runtime.compositions.subscribe, () => runtime.compositions.get(welcomeDraftKey(tab.id)));
  const selected = tab.hostProfileId ?? tab.runtimeId;
  const host = hosts.find(host => host.id === selected || host.runtimeId === selected);
  const selectHost = (id: string) => {
    if (sending || id === host?.id) return;
    const next = hosts.find(host => host.id === id);
    try {
      runtime.tabs.updateNew(tab.id, { hostProfileId: id, runtimeId: next?.runtimeId, cwd: '', model: undefined, provider: undefined, effort: undefined, definition: undefined, unresolvedDefinition: undefined });
      setHostSelectionError(undefined);
      if (next && next.state !== 'connected') void runtime.connections.connect(id).catch(() => {});
    } catch (error) { setHostSelectionError(error); }
  };
  const configuring = !!(host && runtime.platform.localRuntime && host.profile?.target.kind === 'local' && host.state !== 'connected');
  const hostControl = <WelcomeHostPicker hosts={hosts} host={host} disabled={sending} onSelect={selectHost}
    onManage={() => void navigate({ to: '/settings', search: { section: 'connections' } })} />;
  return <ChatDropSurface ref={dropTarget}><SessionTopBar kind="new" host={host?.name ?? 'Choose a host'} cwd={tab.cwd} />
    <div {...stylex.props(styles.page, configuring && layout.setupPage)}><div ref={column} {...stylex.props(styles.column, configuring && layout.setupColumn)} style={providerTop === undefined ? undefined : { marginTop: providerTop, marginBottom: 0 }}>
      {configuring && host
        ? <LocalRuntimeSetup host={host} />
        : host?.client ? <WelcomeComposer key={`${tab.id}:${host.id}`} tab={tab} focused={focused} client={host.client} host={host} hostControl={hostControl} onProviderExpandedChange={onProviderExpandedChange} onConnectRemote={() => setAdding(true)} dropTarget={dropTarget} />
        : <><h1 {...stylex.props(styles.heading)}>What do you want to work on?</h1><p role="status">{host ? `${host.name} is ${host.state === 'closed' ? 'disconnected' : host.state}.` : 'Select or add an execution host to begin.'}</p>
          <div {...stylex.props(styles.toolbar)}>{hostControl}{host
            ? <Button onClick={() => void runtime.connections.connect(host.id).catch(() => {})}>Connect {host.name}</Button>
            : <Button onClick={() => setAdding(true)}>Add server</Button>}</div></>}
      <ErrorNotice type="action" owner={tab.id} title="Could not change host" error={hostSelectionError} />
    </div></div>
    <HostDialog open={adding} onOpenChange={setAdding} onSaved={selectHost} />
  </ChatDropSurface>;
}

/** Editable state belongs to the stable workspace draft, not the selected host. */
export function WelcomeComposer({ client, host, tab, focused = true, hostControl, onConnectRemote, dropTarget, onProviderExpandedChange }: {
  client: Client; host: HostConnection; tab: NewChatTab; focused?: boolean; hostControl?: ReactNode; onConnectRemote?(): void;
  dropTarget?: RefObject<HTMLElement | null>; onProviderExpandedChange?(expanded: boolean): void;
}) {
  const runtime = useRuntime();
  const app = useAppState();
  const runtimeId = client.runtimeID;
  const key = welcomeDraftKey(tab.id);
  const navigate = useNavigate();
  const draft = useSyncExternalStore(listener => runtime.subscribeDraft(key, listener), () => runtime.draft(key));
  const cwd = tab.cwd;
  function updateSetup(patch: Parameters<typeof runtime.tabs.updateNew>[1]) {
    try { runtime.tabs.updateNew(tab.id, patch); } catch (error) { setError(errorMessage(error)); }
  }
  const { sending: busy, attachments } = useSyncExternalStore(runtime.compositions.subscribe, () => runtime.compositions.get(key));
  const [error, setError] = useState('');
  const [showProviders, setShowProviders] = useState(false);
  const [showOptions, setShowOptions] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  const files = useRef<HTMLInputElement>(null);
  const form = useRef<HTMLFormElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const isFocused = useRef(focused);
  isFocused.current = focused;
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const connected = host.state === 'connected' && host.client === client && host.runtimeId === runtimeId;
  const engines = [{ id: 'starlark', label: 'Starlark' }, { id: 'quickjs', label: 'JavaScript' }] as const;
  const configuration = useQuery({ queryKey: ['host-permission-default', runtimeId, client.processEpoch], queryFn: ({ signal }) => client.getDefaultPermissionMode({ signal }), enabled: connected, retry: false });
  const execution = useQuery({ queryKey: ['host-execution-defaults', runtimeId, client.processEpoch], queryFn: ({ signal }) => client.hosts.executionDefaults({ signal }), enabled: connected, retry: false });
  const permission = tab.permissionMode ?? configuration.data?.mode ?? '';
  const permissionAvailable = permission === 'prompt' || permission === 'automatic';
  const providers = useProviderConnections(client, connected);
  const selection = providers.inventory.data?.defaults;
  const catalog = useProviderCatalog(client, connected);
  const model = tab.model ?? selection?.name ?? '';
  const provider = tab.provider ?? selection?.provider ?? '';
  const levels = modelEfforts(catalogModels(catalog.data, provider), model);
  const requestedEffort = tab.effort ?? execution.data?.effort ?? selection?.effort ?? 'off';
  const effort = tab.effort ?? (levels.includes(requestedEffort) ? requestedEffort : 'off');
  const effortAvailable = tab.effort === undefined || levels.includes(effort);
  const readiness = useQuery({ queryKey: ['provider-readiness', runtimeId, { provider, name: model, effort: effort === 'off' ? '' : effort }],
    queryFn: ({ signal }) => client.providerReadiness({ provider, name: model, effort: effort === 'off' ? '' : effort }, { signal }), enabled: connected && !!provider && !!model, retry: false });
  const ready = readiness.data ? readiness.data.configured && ['available', 'not_required'].includes(readiness.data.credential_state) : providers.ready === false ? false : undefined;
  const executionEngine = tab.executionEngine ?? execution.data?.engine ?? '';
  const engineOptions = engines.map(engine => ({ value: engine.id, label: engine.label }));
  const engineAvailable = engineOptions.some(engine => engine.value === executionEngine);
  const definitions = useDefinitions(client, connected);
  const definition = tab.unresolvedDefinition ? undefined : tab.definition ?? client.builtins.find(ref => ref.id === 'coding');
  const definitionValue = definition ? definitionOptionValue(definition) : '';
  const definitionChoices = definitionOptions(definitions.query.data?.items);
  if (definition && !definitionChoices.some(choice => choice.value === definitionValue))
    definitionChoices.unshift({ value: definitionValue, label: `${definition.id} @ ${definition.revision.slice(0, 12)}` });
  const definitionAvailable = !!definition;
  const unresolved = app.commands.find(command => command.draftKey === key && command.delivery);
  const changeDraft = (text: string) => { try { runtime.setDraft(key, text); } catch (error) { setError(errorMessage(error)); } };
  const skills = useSkillCompletion({ client, owner: key, scope: { cwd, definition: definition ?? null },
    input, draft, change: changeDraft, connected, blocked: busy || !permissionAvailable || showOptions || showProviders || !focused });
  function openProviders() { setShowProviders(true); requestAnimationFrame(() => { if (!isFocused.current) return; const setup = panel.current?.querySelector<HTMLElement>('[aria-label="Provider setup"]'); setup?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' }); (setup?.querySelector<HTMLButtonElement>('[data-provider-confirm]:not(:disabled)') ?? setup?.querySelector<HTMLButtonElement>('[data-provider-choice]'))?.focus(); }); }
  function focusComposer() { setShowProviders(false); requestAnimationFrame(() => { if (isFocused.current) input.current?.focus(); }); }
  function attach(files: File[]) {
    if (busy || unresolved) return;
    try { runtime.compositions.stage(key, files); setError(''); }
    catch (error) { setError(errorMessage(error)); }
  }
  async function submit() {
    skills.dismiss();
    if (!connected || busy || !permissionAvailable || (!draft.trim() && !attachments.length) || unresolved) return;
    if (!engineAvailable) { setError('Choose an execution language after the host defaults finish loading.'); return; }
    if (!definition) { setError('Choose an exact agent revision before sending.'); setShowOptions(true); return; }
    if (!ready) { openProviders(); return; }
    if (!effortAvailable) { setError('Choose an available reasoning effort for this model before sending.'); return; }
    if (!cwd.trim()) { setError('Choose a project folder on this host before sending.'); return; }
    setError('');
    try {
      await startNewChat(runtime, client, tab.id, { definition, engine: executionEngine as 'starlark' | 'quickjs',
        working_directory: cwd.trim(), metadata: { title: null, archived: false, pinned: false }, permission_mode: permission,
        overrides: { model: { provider, name: model, effort: effort === 'off' ? '' : effort } } });
    } catch (error) { if (mounted.current) setError(errorMessage(error)); else runtime.report(error); }
  }

  const disabled = !connected || busy;
  // While the inventory is pending, the device's last answer for this host picks the layout; unknown keeps the composer's footprint.
  const knownReady = providers.inventory.isPending ? providers.lastKnownReady : ready;
  const setupVisible = knownReady === false || showProviders;
  // Once a provider works, a host that has MCP servers configured for other
  // agents gets one offer to bring them in. Native MCP configuration records whether this host
  // has answered, so an answered host never reads the other agents' files again.
  const mcp = useQuery({ queryKey: ['mcp-configuration', runtimeId], queryFn: ({ signal }) => client.mcpConfiguration({ signal }), enabled: connected && ready === true && !setupVisible, retry: false });
  const offerable = connected && ready === true && !setupVisible && mcp.data?.imports.offered === false;
  const offer = useMCPImportCandidates(client, { enabled: offerable });
  const offerVisible = offerable && shouldOffer(offer.query.data, mcp.data);
  useEffect(() => {
    if (!focused || setupVisible || offerVisible || !window.matchMedia('(min-width: 768px)').matches) return;
    // A new workspace panel is hidden until layout measures it; mount-time autoFocus runs too early.
    const frame = requestAnimationFrame(() => {
      const active = document.activeElement;
      if (active instanceof HTMLElement && active.closest(
        '[aria-modal="true"], [role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]',
      )) return;
      input.current?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [focused, setupVisible, offerVisible, key]);
  return <><h1 {...stylex.props(styles.heading)}>{setupVisible ? 'Connect a provider to get started' : offerVisible ? 'Bring your MCP servers into Whip' : 'What do you want to work on?'}</h1>
  <div ref={panel} {...stylex.props(styles.content)}>
    {offerVisible && <><MCPImportScreen client={client} hostName={host.name} onDone={focusComposer} />
      <div {...stylex.props(styles.toolbar)}>{hostControl}</div></>}
    {!setupVisible && !offerVisible && <><form ref={form} onSubmit={event => { event.preventDefault(); void submit(); }} {...stylex.props(styles.composer)}>
      <ChatFileDrop target={dropTarget ?? form} scope={key}
        unavailable={busy ? 'Wait for this message to be accepted before attaching files.' : unresolved ? 'Check your previous submission before attaching files.' : undefined}
        onFiles={attach} onError={setError} />
      <ComposerAttachments attachments={attachments} owner={key} disabled={busy || !!unresolved} onRemove={id => runtime.compositions.remove(key, id)} />
      <Textarea ref={input} {...skills.inputProps} onSelect={skills.onSelect} data-whip-composer aria-label="Your first message" placeholder="Describe a task…" rows={3} xstyle={styles.input}
        onPaste={event => {
          const images = Array.from(event.clipboardData.files).filter(file => file.type.startsWith('image/'));
          if (images.length) { event.preventDefault(); attach(images); }
        }}
        value={draft} disabled={busy} maxLength={256 * 1024} onChange={event => { changeDraft(event.target.value); skills.onChange(); }}
        onKeyDown={event => { if (skills.onKeyDown(event)) return; if (event.key === 'Enter' && !event.shiftKey && !event.altKey && !event.ctrlKey && !event.metaKey && !event.nativeEvent.isComposing) { event.preventDefault(); void submit(); } }} />
      {skills.popup}
      <div {...stylex.props(styles.toolbar, styles.composerToolbar)}>
        <input ref={files} type="file" multiple accept="image/*,.txt,.md,.go,.py,.js,.ts,.tsx,.json,.yaml,.yml,.toml,.csv,.log"
          {...stylex.props(styles.hidden)} onChange={event => {
            const selected = Array.from(event.target.files ?? []); event.target.value = ''; attach(selected);
          }} />
        <IconButton variant="ghost" label="Attach text or images" disabled={busy || !!unresolved} onClick={() => files.current?.click()}><Paperclip size={15} /></IconButton>
        <IconButton variant="ghost" label="Add context" title="Context suggestions are available after the session starts" disabled><AtSign size={16} /></IconButton>
        <span {...stylex.props(layout.grow)} />
        <PermissionModeControl value={permission} inherited={tab.permissionMode === undefined} disabled={disabled} onChange={permissionMode => updateSetup({ permissionMode: permissionMode as NewChatTab['permissionMode'] })} />
        {ready ? <CatalogModelPicker model={model} provider={provider} catalog={catalog.data} loading={catalog.isFetching}
          error={connected ? catalog.error?.message : undefined} onRetry={() => void catalog.refetch()} disabled={disabled}
          onChange={(model, provider) => updateSetup({ model, provider, effort: modelEfforts(catalogModels(catalog.data, provider), model).includes(effort) ? effort : 'off' })}
          onSessionOptions={() => setShowOptions(true)} />
          : providers.inventory.isPending ? <PickerSkeletons count={2} />
          : <Button variant="ghost" disabled={disabled} onClick={openProviders}>Connect a provider</Button>}
        {(ready || !providers.inventory.isPending) && <DraftEffortPicker value={effort} levels={levels} disabled={disabled || !ready || catalog.isPending} onChange={effort => updateSetup({ effort })} />}
        <Button type="submit" variant="primary" aria-label="Send first message" xstyle={styles.send} loading={busy}
          disabled={disabled || !permissionAvailable || !!unresolved || !definitionAvailable || !engineAvailable || !effortAvailable || !ready || (!draft.trim() && !attachments.length) || !cwd.trim()}>{!busy && <ArrowUp size={16} />}</Button>
      </div>
    </form>
    <div {...stylex.props(styles.toolbar)}>
      {hostControl}
      <DirectoryPicker sessionTrigger connected={connected} client={client} native={host.local} pickDirectory={host.profile?.target.kind === 'local' ? runtime.platform.pickDirectory : undefined}
        host={{ name: host.name, detail: host.profile.target.kind === 'ssh' ? [host.profile.target.user, host.profile.target.host].filter(Boolean).join('@') : host.endpoint,
          reconnect: () => runtime.connections.connect(host.id) }}
        value={cwd} onSelect={path => { updateSetup({ cwd: path }); setError(''); }} disabled={disabled} />
    </div>
    </>}
    <Dialog open={showOptions} onOpenChange={setShowOptions} title="Session options">
      <Field label="Execution language" description="Fixed once the session starts."><Select label="Execution language" value={executionEngine}
        disabled={disabled || !engines?.length} options={engineAvailable ? engineOptions : [...engineOptions, { value: executionEngine, label: `${executionEngine} (unavailable)`, disabled: true }]}
        onValueChange={executionEngine => updateSetup({ executionEngine: executionEngine as NewChatTab['executionEngine'] })} /></Field>
      {definitions.supported && <Field label="Agent"><Select label="Agent" value={definitionValue} disabled={disabled || !definitionChoices.length}
        options={definitionAvailable ? definitionChoices : [...definitionChoices, { value: '', label: `${tab.unresolvedDefinition ?? 'Agent'} (choose a revision)`, disabled: true }]}
        onValueChange={value => updateSetup({ definition: parseDefinitionOption(value), unresolvedDefinition: undefined })} /></Field>}
    {definitions.query.hasNextPage && <Button variant="ghost" disabled={disabled || definitions.query.isFetchingNextPage} onClick={() => void definitions.query.fetchNextPage()}>Load more agents</Button>}
      {definitions.truncated && <p role="status">Only the first 1,000 agent revisions are shown.</p>}
      {definitions.query.error && <ErrorNotice type="resource" owner={`${runtimeId}:definitions`} title="Could not load agent revisions" error={definitions.query.error} />}
    </Dialog>
    {!setupVisible && connected && (!engineAvailable || !definitionAvailable) && <Button variant="ghost" onClick={() => setShowOptions(true)}>Review unavailable session options</Button>}
    {!setupVisible && !effortAvailable && !catalog.isPending && <p role="status" {...stylex.props(styles.note)}>Choose an available reasoning effort for this model before sending.</p>}
    {execution.error && !tab.executionEngine && <ErrorNotice type="resource" owner={`${runtimeId}:execution-defaults`} title="Could not load execution defaults" error={execution.error} action={<Button onClick={() => void execution.refetch()}>Retry execution defaults</Button>} />}
    {tab.permissionMode === undefined && !configuration.data && (configuration.error
      ? <ErrorNotice type="resource" owner={`${runtimeId}:configuration`} title="Could not load host defaults" error={configuration.error}
        action={<Button variant="ghost" disabled={!connected || configuration.isFetching} onClick={() => void configuration.refetch()}>Retry host defaults</Button>} />
      : <p role="status" {...stylex.props(styles.note)}>Loading host permission defaults… Choose a permission level to override.</p>)}
    {unresolved && <ErrorNotice type="submission" owner={key} tone="warning"
      title={unresolved.delivery === 'absent' ? 'Your first message was not received' : 'Checking whether your first message was received'}
      error={unresolved.error || 'Check this command before sending again. Your draft is preserved.'}
      action={<>
        <Button type="button" variant="ghost" disabled={!connected} onClick={() => void runtime.checkCommand(unresolved.id).catch(error => setError(errorMessage(error)))}>Check status</Button>
        {unresolved.delivery === 'absent' && <Button type="button" variant="ghost" disabled={!connected} onClick={() => void runtime.retryCommand(unresolved.id).catch(error => setError(errorMessage(error)))}>Send again</Button>}
      </>} />}
    {error && !unresolved && <ErrorNotice type="submission" owner={key} error={error} action={error.includes('saved session creation') ? <Button onClick={() => void navigate({ to: '/settings', search: { section: 'general', setting: 'commandRecovery' } })}>Review saved commands</Button> : undefined} />}
    {!connected && <p role="status" {...stylex.props(styles.note)}>Reconnecting to {host.name}. Your draft stays here and will not be sent automatically.</p>}
    {setupVisible && <ProviderSetup client={client} enabled={connected && !busy} hostName={host.name} connections={providers} onExpandedChange={onProviderExpandedChange}
      actions={host.local ? <Button variant="ghost" disabled={busy} onClick={() => onConnectRemote ? onConnectRemote() : void navigate({ to: '/settings', search: { section: 'connections' } })}><Monitor size={14} />Connect Remote</Button> : showProviders ? hostControl : undefined}
      onReady={() => { updateSetup({ model: undefined, provider: undefined, effort: undefined }); focusComposer(); }} />}
  </div></>;
}

const styles = stylex.create({
  hidden: { display: 'none' },
  page: { display: 'flex', flex: 1, minHeight: 0, flexDirection: 'column', alignItems: 'center', overflowY: 'auto', paddingInline: { default: 32, [scale.phone]: 16 }, paddingTop: 32, paddingBottom: { default: 116, [scale.phone]: 32 } },
  column: { width: 'min(100%, 620px)', minWidth: 0, marginBlock: 'auto', display: 'flex', flexDirection: 'column', gap: scale.space4 },
  heading: { fontSize: typography.size24, fontWeight: 550, lineHeight: '32px', letterSpacing: '-0.025em', margin: 0 },
  content: { display: 'flex', flexDirection: 'column', gap: scale.space2, minWidth: 0 },
  composer: { position: 'relative', display: 'flex', flexDirection: 'column', gap: scale.space2, padding: scale.space3, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: 20, backgroundColor: colors.element },
  input: { minHeight: 96, maxHeight: 220, resize: 'none', borderWidth: 0, boxShadow: 'none', outline: 'none', backgroundColor: { default: 'transparent', ':hover': 'transparent' }, fontSize: { default: typography.size14, [scale.phone]: typography.size16 }, padding: scale.space1 },
  toolbar: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space1, minWidth: 0 },
  composerToolbar: { justifyContent: 'flex-end' },
  send: { borderRadius: '50%', width: { default: 32, [scale.touch]: 44 }, paddingInline: 0 },
  note: { fontSize: typography.size12, color: surface.secondaryText, margin: 0, lineHeight: 1.5 },
});
