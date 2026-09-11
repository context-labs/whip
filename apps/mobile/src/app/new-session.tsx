import { ScrollView, View } from 'react-native';
import { useEffect, useMemo, useRef, useState } from 'react';
import { router, useIsFocused, useLocalSearchParams } from 'expo-router';
import * as Crypto from 'expo-crypto';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import { Actions, Field, Label, Loading, Notice, RowButton, Screen, Stack } from '../components/primitives';
import { Connection } from '../components/connection';
import { RuntimeScope, useRuntime, useRuntimeState } from '../runtime/context';
import { useWorkspace, useWorkspaceState } from '../runtime/workspace-context';
import { Button, ListRow, Sheet, Text } from '../ui';
import type { SavedHost } from '../runtime/runtime';
import {
  advanceCreation, creationCommands, creationDraftKey, creationModels, creationSettingsKey,
  nextCreationStep, reconcileCreation, stepCommand, validateWorkflow, type CreationWorkflow,
} from '../features/creation';

const errorText = (error: unknown) => error instanceof Error ? error.message : String(error);
export default function NewSessionScreen() {
  const workspace = useWorkspace(); const state = useWorkspaceState();
  const [hostId, setHostId] = useState<string>();
  const connection = workspace.runtime(hostId);
  if (!connection) return <Screen><Text variant="title">New session</Text><Text muted>Choose the computer that will run this session.</Text>{state.hosts.map(host => { const current = workspace.runtime(host.id)?.getSnapshot(); return <ListRow key={host.id} title={host.name} detail={current?.ready ? 'Connected' : current?.connecting ? 'Connecting…' : 'Connect this host to continue'} onPress={() => { if (current?.ready) setHostId(host.id); else router.push({ pathname: '/server', params: { hostId: host.id } }); }} />; })}<ListRow title="Add host" onPress={() => router.push('/server')} /></Screen>;
  return <RuntimeScope runtime={connection}><SelectedHostCreation onChangeHost={() => setHostId(undefined)} /></RuntimeScope>;
}
function SelectedHostCreation({ onChangeHost }: { onChangeHost(): void }) {
  const { host, client } = useRuntimeState();
  const params = useLocalSearchParams<{ cwd?: string; runtimeId?: string }>();
  if (!host?.runtimeId || !client) return <Screen><Connection /><Notice>Connect to a Whip host to create a session.</Notice></Screen>;
  return <><View style={{ paddingHorizontal: 20 }}><ListRow title={host.name} detail="Change host" onPress={onChangeHost} /></View><CreationForm key={`${host.id}:${host.runtimeId}:${host.clientId}`} host={host} client={client}
    initialPath={params.runtimeId === host.runtimeId && typeof params.cwd === 'string' ? params.cwd.slice(0, 2048) : ''} /></>;
}
function CreationForm({ host, client, initialPath }: { host: SavedHost; client: WhipClient; initialPath: string }) {
  const runtime = useRuntime(); const state = useRuntimeState(); const focused = useIsFocused();
  const runtimeId = host.runtimeId!;
  const [workflow, setWorkflow] = useState<CreationWorkflow>();
  const workflowRef = useRef<CreationWorkflow | undefined>(undefined);
  const [busy, setBusy] = useState(false); const busyRef = useRef(false);
  const [error, setError] = useState<string>();
  const [loadError, setLoadError] = useState<string>();
  const [stage, setStage] = useState<'folder' | 'review'>('folder');
  const [options, setOptions] = useState(false);
  const [browse, setBrowse] = useState(true); const [path, setPath] = useState('');
  const [typedPath, setTypedPath] = useState(''); const [after, setAfter] = useState<string>();
  const [showModels, setShowModels] = useState(false); const [modelSearch, setModelSearch] = useState('');
  const mounted = useRef(true); const visible = useRef(focused); visible.current = focused;
  const current = () => mounted.current && visible.current && runtime.getSnapshot().client === client && runtime.getSnapshot().host?.runtimeId === runtimeId && runtime.getSnapshot().ready;
  const save = async (next: CreationWorkflow) => {
    workflowRef.current = next;
    if (mounted.current) setWorkflow(next);
    await runtime.storage.set('settings', creationSettingsKey(host.id), next);
  };
  useEffect(() => {
    mounted.current = true;
    let cancelled = false;
    const controller = new AbortController();
    void (async () => {
      const saved = await runtime.storage.get<CreationWorkflow>('settings', creationSettingsKey(host.id));
      const defaults = saved ? undefined : await client.configuration.get({ signal: controller.signal });
      const next = saved ? validateWorkflow(saved, runtimeId, host.clientId) : {
        version: 1 as const, id: Crypto.randomUUID(), runtimeId, clientId: host.clientId, cwd: initialPath,
        executionEngine: defaults!.default_execution_engine,
        effortDone: false, promptSent: false,
      };
      if (cancelled) return;
      if (!saved) await runtime.storage.set('settings', creationSettingsKey(host.id), next);
      if (!cancelled) { workflowRef.current = next; setWorkflow(next); setPath(next.cwd); setTypedPath(next.cwd); if (next.rootId) setStage('review'); }
    })().catch(e => { if (!cancelled) setLoadError(errorText(e)); });
    return () => { cancelled = true; mounted.current = false; controller.abort(); };
  }, [runtime, host.id, runtimeId, host.clientId, initialPath]);
  useEffect(() => {
    if (!workflow || busy) return;
    try {
      const next = reconcileCreation(workflow, state.commands);
      if (next !== workflow) void save(next).catch(e => setError(errorText(e)));
    } catch (e) { setError(errorText(e)); }
  }, [workflow, state.commands, busy]);
  const enabled = state.ready && state.active && focused;
  const directories = useQuery({ queryKey: [runtimeId, 'host.directories', path, after], enabled: enabled && browse,
    queryFn: ({ signal }) => client.host.directories({ path: path || undefined, after, limit: 64 }, { signal }) });
  const definitions = useQuery({ queryKey: [runtimeId, 'definitions.list'], enabled: enabled && client.supports('rpc', 'definitions.list'), queryFn: async ({ signal }) => { const result = await client.call('definitions.list', {}, { signal }); if (new TextEncoder().encode(JSON.stringify(result)).byteLength > 256 << 10) throw new Error('The agent catalog exceeds the mobile limit. Use the host default.'); return result; } });
  const catalogs = useQuery({ queryKey: [runtimeId, 'provider.catalogs'], enabled,
    queryFn: async ({ signal }) => {
      const response = await client.providers.catalogs({ signal });
      if (new TextEncoder().encode(JSON.stringify(response)).byteLength > 1 << 20) throw new Error('Provider catalog exceeds the 1 MiB mobile limit.');
      return response;
    } });
  const models = useMemo(() => creationModels(catalogs.data?.result), [catalogs.data]);
  const selected = models.find(item => item.model === workflow?.model && item.provider === workflow?.provider);
  const engines = (client.getSnapshot().info?.execution_engines ?? []).filter(engine => engine.id === 'starlark' || engine.id === 'quickjs');
  const engineAvailable = engines.some(engine => engine.id === workflow?.executionEngine);
  const matchingModels = useMemo(() => models.filter(item => `${item.model} ${item.provider}`.toLowerCase().includes(modelSearch.trim().toLowerCase())), [models, modelSearch]);
  if (loadError) return <Screen><Connection /><Notice danger>{loadError}</Notice></Screen>;
  if (!workflow) return <Screen><Loading label="Restoring creation draft…" /></Screen>;
  const draftKey = creationDraftKey(workflow);
  const draft = runtime.draft(draftKey);
  const related = creationCommands(workflow, state.commands);
  const activeCommand = related.find(command => ['sending', 'checking', 'queued', 'running', 'waiting'].includes(command.status));
  const pending = workflow.pendingStep ? stepCommand(workflow, related, workflow.pendingStep) : undefined;
  const locked = busy || !!activeCommand || !!workflow.pendingStep;
  const editable = !locked && !workflow.rootId;
  const update = (patch: Partial<CreationWorkflow>) => { void save({ ...workflowRef.current!, ...patch }).catch(e => setError(errorText(e))); };
  const navigateFolder = (target: string) => { setPath(target); setTypedPath(target); setAfter(undefined); };
  const openCreated = () => {
    const rootId = workflowRef.current?.rootId;
    if (rootId && runtime.getSnapshot().client === client) router.replace({ pathname: '/session/[rootId]', params: { rootId, runtimeId, hostId: host.id } });
  };
  const start = async () => {
    if (busyRef.current || !current() || locked) return;
    busyRef.current = true; setBusy(true); setError(undefined);
    try {
      if (!workflowRef.current?.rootId && !engineAvailable) throw new Error('The host does not advertise the selected execution language. Reconnect to an updated host or choose an available language.');
      const next = await advanceCreation(workflowRef.current!, { run: runtime.run.bind(runtime), current, save, saveDraft: (key, value) => runtime.storage.setDraft(key, value), draft: key => runtime.draft(key) });
      if (current() && next.rootId && !nextCreationStep(next, runtime.draft(creationDraftKey(next)))) openCreated();
    } catch (e) { if (mounted.current) setError(errorText(e)); }
    finally { busyRef.current = false; if (mounted.current) setBusy(false); }
  };
  const resolve = async () => {
    if (busyRef.current || !current()) return;
    busyRef.current = true; setBusy(true); setError(undefined);
    try {
      if (pending) {
        if (!['failed', 'cancelled', 'interrupted', 'not_found'].includes(pending.status)) throw new Error('Check the original command before continuing.');
        await runtime.forgetCommand(pending);
      }
      await save({ ...workflowRef.current!, pendingStep: undefined });
    } catch (e) { if (mounted.current) setError(errorText(e)); }
    finally { busyRef.current = false; if (mounted.current) setBusy(false); }
  };
  const startAnother = async () => {
    if (busyRef.current || locked || !workflow.rootId || draft.text.trim() && !workflow.promptSent) return;
    setError(undefined);
    busyRef.current = true; setBusy(true);
    try {
      const defaults = await client.configuration.get();
      if (!current()) return;
      await save({ version: 1, id: Crypto.randomUUID(), runtimeId, clientId: host.clientId, cwd: workflow.cwd,
        executionEngine: defaults.default_execution_engine,
        model: workflow.model, provider: workflow.provider, effort: workflow.effort, effortDone: false, promptSent: false });
    } catch (e) { if (mounted.current) setError(errorText(e)); }
    finally { busyRef.current = false; if (mounted.current) setBusy(false); }
  };
  const step = nextCreationStep(workflow, draft);
  const proceedLabel = !workflow.rootId ? 'Create session' : step === 'effort' ? 'Apply reasoning and continue' : 'Send first message';
  return <Screen>
    {!state.ready && <Connection />}
    <Stack><Text variant="title">{workflow.rootId ? 'Your session is created' : stage === 'folder' ? 'Choose a folder' : 'Start something.'}</Text><Text muted>{workflow.rootId ? workflow.cwd : stage === 'folder' ? 'Pick the project you want to work in.' : 'Everything is ready. Add a first message, or start with an empty session.'}</Text></Stack>
    {error && <Notice danger>{error}</Notice>}
    {stage === 'folder' && !workflow.rootId ? <>
      <Field label="Folder path" value={typedPath} onChangeText={setTypedPath} editable={!locked} autoCapitalize="none" autoCorrect={false} maxLength={2048} placeholder="Enter a path on this host" onSubmitEditing={() => navigateFolder(typedPath.trim())} />
      <Button label="Open folder" variant="secondary" disabled={!enabled || locked || directories.isFetching} onPress={() => navigateFolder(typedPath.trim())} />
      {directories.isFetching && <Loading label="Reading host folders…" />}
      {directories.error && <Notice danger>{directories.error.message}</Notice>}
      {directories.data && <Stack>
        <Text variant="caption" muted selectable>{directories.data.path}</Text>
        {!!directories.data.parent && <ListRow title="Parent folder" detail="Go up one level" disabled={!enabled || locked} onPress={() => navigateFolder(directories.data!.parent)} />}
        {(directories.data.entries ?? []).slice(0, 64).map(entry => <ListRow key={entry.path} title={entry.name} disabled={!enabled || locked} onPress={() => navigateFolder(entry.path)} />)}
        {!directories.data.entries?.length && !directories.isFetching && <Text muted>No subfolders. You can work in this folder.</Text>}
        {(directories.data.truncated || directories.data.has_more) && <Notice>More folders are available. Open a subfolder, enter a path, or view the next page.</Notice>}
        {directories.data.has_more && directories.data.next_after && <Button label="Next folders" variant="quiet" disabled={!enabled || locked || directories.isFetching} onPress={() => setAfter(directories.data!.next_after)} />}
        {after && <Button label="First folders" variant="quiet" onPress={() => setAfter(undefined)} />}
        <Button label="Use this folder" disabled={!enabled || locked || directories.isFetching} onPress={() => { update({ cwd: directories.data!.path }); setBrowse(false); setStage('review'); }} />
      </Stack>}
    </> : <>
      <ListRow title={workflow.cwd.split('/').filter(Boolean).at(-1) || workflow.cwd} detail={workflow.cwd} onPress={workflow.rootId ? undefined : () => { setStage('folder'); setBrowse(true); }} />
      <ListRow title={workflow.model || 'Host default model'} detail={`${workflow.definition || 'Default agent'} · ${workflow.effort || 'Default reasoning'}`} onPress={() => setOptions(true)} />
      {workflow.rootId && <Button label="Open created session" onPress={openCreated} />}
    {!workflow.promptSent && <Field label="First message (optional)" value={draft.text} multiline textAlignVertical="top" style={{ minHeight: 144 }} editable={!busy && !activeCommand && workflow.pendingStep !== 'submit'}
      placeholder="Describe the work to start…" onChangeText={text => { try { runtime.setDraft(draftKey, text); } catch (e) { setError(errorText(e)); } }} />}
    {workflow.promptSent && <Notice>Your first message was submitted to the created session.</Notice>}
    {workflow.pendingStep && <Stack><Notice>{pending ? `Previous ${workflow.pendingStep} step: ${pending.status}. ${pending.message ?? 'Check its original command before continuing.'}` : 'No command record is available for this prepared step. Review the saved settings before clearing the preparation and continuing.'}</Notice>
      <Actions items={[
        ...(pending ? [{ label: 'Check original command', secondary: true, disabled: !enabled || busy, onPress: () => { void runtime.checkCommand(pending).catch(e => setError(errorText(e))); } }] : []),
        ...(!activeCommand && (!pending || ['failed', 'cancelled', 'interrupted', 'not_found'].includes(pending.status)) ? [{ label: pending?.status === 'not_found' ? 'Resolve missing attempt' : pending ? 'Resolve failed attempt' : 'Review and clear preparation', secondary: true, disabled: !enabled || busy, onPress: () => { void resolve(); } }] : []),
      ]} />
      <Label muted>Resolving preserves your draft. Continuing uses a new command ID for this step and keeps any session already created.</Label>
    </Stack>}
    {!workflow.pendingStep && activeCommand && <Notice>The previous step is still {activeCommand.status}. Whip will keep its result here.</Notice>}
    {step && <Actions items={[{ label: busy ? 'Starting your session…' : proceedLabel, disabled: !enabled || locked || !workflow.cwd.trim() || !workflow.rootId && !engineAvailable, onPress: () => { void start(); }, testID: 'create-session' }]} />}
    {workflow.rootId && !locked && (!draft.text.trim() || workflow.promptSent) && <Actions items={[{ label: 'Prepare another session', secondary: true, onPress: () => { void startAnother(); } }]} />}
    </>}
    <Sheet title="Session options" visible={options} onClose={() => setOptions(false)} full><ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={{ padding: 20, gap: 24 }}>
    <Stack><Label muted>Model</Label><Label>{workflow.model ? `${workflow.model} · ${workflow.provider}` : 'Use the host’s default model and provider'}</Label>
      {!workflow.rootId && <Actions items={[{ label: showModels ? 'Close model selection' : 'Choose model', secondary: true, disabled: locked, onPress: () => setShowModels(!showModels) }]} />}
      {catalogs.error && <Notice>{catalogs.error.message} Host defaults are still available.</Notice>}
      {catalogs.data?.result?.errors && <Notice>Some provider catalogs are unavailable. Configure providers on the host and refresh the catalog.</Notice>}
      {showModels && !workflow.rootId && <Stack>
        <Field label="Find a model or provider" value={modelSearch} onChangeText={setModelSearch} editable={!locked} maxLength={128} />
        <RowButton title="Use host defaults" selected={!workflow.model} disabled={locked} onPress={() => { update({ model: undefined, provider: undefined, effort: undefined }); setShowModels(false); }} />
        {catalogs.isFetching && <Loading label="Reading provider catalogs…" />}
        {matchingModels.slice(0, 64).map(item => <RowButton key={JSON.stringify([item.provider, item.model])} title={item.model} detail={item.provider}
          selected={selected === item} disabled={locked} onPress={() => { update({ model: item.model, provider: item.provider, effort: undefined }); setShowModels(false); }} />)}
        {matchingModels.length > 64 && <Notice>Showing 64 models. Narrow the search to find another model.</Notice>}
        {!matchingModels.length && !catalogs.isFetching && <Label muted>No matching catalog models. You can use the host defaults.</Label>}
        <Actions items={[{ label: 'Refresh models', secondary: true, disabled: !enabled || catalogs.isFetching, onPress: () => { void catalogs.refetch(); } }]} />
      </Stack>}
    </Stack>
    <Stack><Label muted>Execution language</Label>
      <Label muted>Fixed for this session and all its child agents.</Label>
      {workflow.rootId ? <Label>{workflow.executionEngine === 'quickjs' ? 'JavaScript (QuickJS)' : 'Starlark'}</Label> : engines.map(engine =>
        <RowButton key={engine.id} title={engine.label} selected={workflow.executionEngine === engine.id} disabled={!enabled || !editable} onPress={() => update({ executionEngine: engine.id })} />)}
      {!workflow.rootId && !engineAvailable && <Notice>The selected execution language is unavailable on this host.</Notice>}
    </Stack>
    <Stack><Label muted>Reasoning effort</Label>
      {workflow.rootId ? <Label>{workflow.effort ?? 'Host default'}{workflow.effortDone ? ' · Applied' : ''}</Label> : <>
        <RowButton title="Use host default effort" selected={!workflow.effort} disabled={locked} onPress={() => update({ effort: undefined })} />
        {(selected?.efforts ?? []).slice(0, 32).map(effort => <RowButton key={effort} title={effort} selected={workflow.effort === effort} disabled={locked} onPress={() => update({ effort })} />)}
        {!selected?.efforts.length && <Label muted>Select a catalog model with reasoning support to choose an effort level.</Label>}
      </>}
    </Stack>
      <Stack><Label muted>Agent definition</Label><RowButton title="Host default agent" selected={!workflow.definition} disabled={!editable} onPress={() => update({ definition: undefined })} />
      {definitions.data?.items?.slice(0, 64).map(item => <RowButton key={item.id} title={item.id} detail={item.built_in ? 'Built in' : 'Registered on host'} selected={workflow.definition === item.id} disabled={!editable} onPress={() => update({ definition: item.id })} />)}
      {definitions.error && <Notice>{definitions.error.message} The host default remains available.</Notice>}</Stack>
      <Button label="Done" onPress={() => setOptions(false)} />
    </ScrollView></Sheet>
    <Label muted>Your draft stays on this phone. Work continues on your host when you leave.</Label>
  </Screen>;
}
