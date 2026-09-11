import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import { AppState, Pressable, ScrollView, View, type NativeScrollEvent, type NativeSyntheticEvent } from 'react-native';
import { Stack as RouteStack, router, useIsFocused, useLocalSearchParams } from 'expo-router';
import { useQuery } from '@tanstack/react-query';
import { ChevronDown, MoreHorizontal } from 'lucide-react-native';
import { Composer } from '../../components/composer';
import { SessionMenu } from '../../components/session-menu';
import { EmptyState, IconButton, ScreenHeader, Sheet, StatusBadge, Text } from '../../ui';
import { FlashList, type FlashListRef } from '@shopify/flash-list';
import { KeyboardAvoidingView } from 'react-native-keyboard-controller';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { conversationRows, type ReadingBookmark, type TimelineRow } from '@whip/app/presentation';
import { useSessionView } from '@whip/sdk/react';
import type { DeepReadonly, SessionView, SessionViewSnapshot } from '@whip/sdk/state';
import { useRootView, useRuntime, useRuntimeState } from '../../runtime/context';
import { draftKey } from '../../runtime/address';
import { Actions, Field, Label, Loading, Notice, RowButton, Screen, Stack } from '../../components/primitives';
import { BodyInspector, ConversationRow } from '../../components/conversation';
import { Requests } from '../../components/requests';
import { useDisplay, useTheme } from '../../theme/theme';
import { captureReadingBookmark, restoreReadingBookmark, validReadingBookmark } from '../../features/reading';
import { creationModels } from '../../features/creation';
import { idleRootSettings, pendingRootSetting, sessionEfforts } from '../../features/session-settings';

export default function SessionScreen() {
  const params = useLocalSearchParams<{ rootId: string; runtimeId: string; agentId?: string; requests?: string }>();
  const rootId = typeof params.rootId === 'string' ? params.rootId : '';
  const runtimeId = typeof params.runtimeId === 'string' ? params.runtimeId : '';
  const state = useRuntimeState();
  const valid = !!rootId && rootId.length <= 512 && !!runtimeId && runtimeId.length <= 512 && state.host?.runtimeId === runtimeId
    && (params.agentId === undefined || typeof params.agentId === 'string' && params.agentId.length > 0 && params.agentId.length <= 512);
  const view = useRootView(valid ? rootId : '', valid ? runtimeId : '');
  if (!valid)
    return <Screen><Notice>This session belongs to another host. Connect its saved server in Settings.</Notice></Screen>;
  return view?.session.rootId === rootId && view.session.client === state.client
    ? <SessionContent key={JSON.stringify([runtimeId, rootId])} view={view} runtimeId={runtimeId} initialAgent={params.agentId} showRequests={params.requests === 'true'} />
    : <Loading label="Opening session…" />;
}
function SessionContent({ view, runtimeId, initialAgent, showRequests }: { view: SessionView; runtimeId: string; initialAgent?: string; showRequests: boolean }) {
  const runtime = useRuntime(); const state = useRuntimeState(); const theme = useTheme(); const insets = useSafeAreaInsets();
  const snapshot = useSessionView(view); const root = snapshot.root;
  const [agentId, setAgent] = useState(initialAgent || view.session.rootId);
  const [sheet, setSheet] = useState<'agents' | 'requests' | 'details' | 'body' | 'menu' | undefined>(showRequests ? 'requests' : undefined);
  const [inspection, setInspection] = useState<{ row: TimelineRow; agentId: string }>();
  const agent = root?.agents?.find(a => a.id === agentId);
  const isRoot = agentId === view.session.rootId;
  const recipientExists = isRoot || !!agent;
  useEffect(() => {
    if (!recipientExists) return;
    try { const lease = runtime.acquireAgent(view, agentId); return lease.release; }
    catch (error) { runtime.report(error); }
  }, [agentId, view, runtime, recipientExists]);
  const name = isRoot ? 'Root agent' : agent?.name || agentId;
  const model = isRoot ? root?.meta.model : agent?.model;
  const provider = isRoot ? root?.meta.provider : agent?.provider;
  const effort = isRoot ? root?.meta.effort : agent?.effort;
  const questions = root?.questions?.length ?? 0;
  const permissions = root?.permissions?.filter(p => p.status === 'pending').length ?? 0;
  const enabled = state.ready && snapshot.status === 'live' && !!root && !root.meta.archived && (agentId === root.root_id || !!agent);
  return <Screen scroll={false}>
    <View style={{ paddingTop: insets.top }}><ScreenHeader title={root?.meta.title || 'New session'} onBack={() => router.back()} right={<IconButton label="Session menu" onPress={() => setSheet('menu')}><MoreHorizontal size={22} color={theme.colors.foreground} /></IconButton>} /></View>
    <Stack style={{ paddingHorizontal: 20, paddingBottom: 12, gap: 8 }}>
      <View style={{ flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', gap: 12 }}><Pressable accessibilityRole="button" accessibilityLabel={`Recipient: ${name}. Choose agent`} onPress={() => setSheet('agents')} style={{ minHeight: 44, flex: 1, flexDirection: 'row', alignItems: 'center', gap: 6 }}><Text variant="caption" muted numberOfLines={1}>{isRoot ? `${state.host?.name} · ${root?.meta.cwd?.split('/').filter(Boolean).at(-1) || 'Project'}` : name}</Text><ChevronDown size={14} color={theme.colors.muted} /></Pressable><StatusBadge label={!state.ready ? 'Offline' : root?.active_turns?.[agentId] ? 'Working' : 'Ready'} tone={!state.ready ? 'warning' : root?.active_turns?.[agentId] ? 'primary' : 'muted'} pulse={!!root?.active_turns?.[agentId]} /></View>
      {!!root && !recipientExists && <Notice>This recipient is outside the current session snapshot. Choose an available agent before sending.</Notice>}
      {(questions > 0 || permissions > 0 || root?.omitted?.questions || root?.omitted?.permissions) && <Actions items={[{ label: `${questions + permissions}${root?.omitted?.questions || root?.omitted?.permissions ? '+' : ''} requests need attention`, onPress: () => setSheet('requests'), testID: 'open-requests' }]} />}
    </Stack>
    {state.error && <Stack style={{ paddingHorizontal: 20 }}><Notice danger>{state.error}</Notice><Actions items={[{ label: 'Dismiss message', secondary: true, onPress: runtime.clearError }]} /></Stack>}
    {root?.meta.archived && <Notice>This session is archived. Open the session menu to restore it.</Notice>}
    {snapshot.status !== 'live' && <Notice>{snapshot.unavailable ? 'This session is unavailable on the host.' : snapshot.error?.message ?? 'Reconnecting… Last observed messages remain visible.'}</Notice>}
    {snapshot.truncated && <Pressable accessibilityRole="button" accessibilityLabel="View summarized session details" onPress={() => setSheet('details')} style={{ paddingHorizontal: 20, minHeight: 44, justifyContent: 'center' }}><Text muted variant="caption">More session details available →</Text></Pressable>}
    {root ? <Conversation key={agentId} view={view} runtimeId={runtimeId} snapshot={snapshot} agentId={agentId} name={name} enabled={enabled} onOptions={() => setSheet('details')} onInspect={row => { setInspection({ row, agentId }); setSheet('body'); }} /> : <Loading />}
    <Sheet title={sheet === 'menu' ? 'Session' : sheet === 'agents' ? 'Choose recipient' : sheet === 'requests' ? 'Needs you' : sheet === 'body' ? 'Full message' : 'Session details'} visible={!!sheet} onClose={() => setSheet(undefined)} full={sheet !== 'menu'}>
      <View style={{ flex: 1, backgroundColor: theme.colors.panel }}>
        {sheet === 'menu' ? <SessionMenu view={view} onDetails={() => setSheet('details')} onClose={() => setSheet(undefined)} /> : sheet === 'body' && inspection ? <BodyInspector row={inspection.row} rootId={view.session.rootId} agentId={inspection.agentId} /> : sheet === 'requests' && root ? <Requests root={root} view={view} disabled={!state.ready || snapshot.status !== 'live'} /> : <ScrollView contentContainerStyle={{ padding: 20, gap: 16 }}>
          {sheet === 'agents' ? <><Label style={{ fontSize: 24, fontWeight: '600' }}>Choose recipient</Label>
            <RowButton title="Root agent" selected={agentId === view.session.rootId} onPress={() => { setAgent(view.session.rootId); setSheet(undefined); router.setParams({ agentId: view.session.rootId }); }} />
            {root?.agents?.filter(a => a.id !== root.root_id).map(a => <RowButton key={a.id} title={a.name || a.id} detail={`${a.status} · ${a.model}`} selected={a.id === agentId} onPress={() => { setAgent(a.id); setSheet(undefined); router.setParams({ agentId: a.id }); }} />)}
            {root?.omitted?.agents && <Notice>More agents exist than this snapshot contains. Use the web app to inspect the full tree.</Notice>}
          </> : <><Label style={{ fontSize: 24, fontWeight: '600' }}>Session details</Label>
            {snapshot.truncated && <Notice>Some details are summarized in this session view. Earlier messages and full message content can be opened from the conversation. The web app can show larger outputs.</Notice>}<Label selectable>{root?.meta.title}</Label><Label muted>Directory</Label><Label selectable>{root?.meta.cwd}</Label>
            <Label muted>Model</Label><Label>{model ? `${model}${provider ? ` · ${provider}` : ''}` : 'Model unavailable'}</Label>
            <Label muted>Reasoning effort</Label><Label>{effort || 'Host default'}</Label>
            {agentId === view.session.rootId && root && <RootSettings view={view} />}
            <Label muted>Permission mode</Label><Label>{root?.permission_mode || 'Host controlled'}</Label>
            {root?.meta.goal && <><Label muted>Goal</Label><Label>{root.meta.goal}</Label></>}
            <Label muted>Root ID</Label><Label selectable>{view.session.rootId}</Label><Label muted>Recipient ID</Label><Label selectable>{agentId}</Label>
          </>}
        </ScrollView>}
        <View style={{ padding: 16 }}><Actions items={[{ label: 'Done', secondary: true, onPress: () => setSheet(undefined) }]} /></View>
      </View>
    </Sheet>
  </Screen>;
}
function RootSettings({ view }: { view: SessionView }) {
  const runtime = useRuntime(); const state = useRuntimeState(); const focused = useIsFocused();
  const snapshot = useSessionView(view); const root = snapshot.root;
  const rootId = view.session.rootId; const client = view.session.client;
  const runtimeId = client.getSnapshot().info?.runtime_id ?? '';
  const [modelsOpen, setModelsOpen] = useState(false); const [search, setSearch] = useState('');
  const [busy, setBusy] = useState(false); const busyRef = useRef(false);
  const mounted = useRef(true); const visible = useRef(focused); visible.current = focused;
  useLayoutEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, [view]);
  const connected = state.ready && state.active && focused && state.client === client;
  const pending = pendingRootSetting(state.commands, runtimeId, rootId);
  const editable = connected && idleRootSettings(snapshot, rootId, rootId) && !pending && !busy && !runtime.isBlocked(rootId);
  const catalogs = useQuery({ queryKey: [runtimeId, 'provider.catalogs'], enabled: connected,
    queryFn: async ({ signal }) => {
      const result = await client.providers.catalogs({ signal });
      if (new TextEncoder().encode(JSON.stringify(result)).byteLength > 1 << 20) throw new Error('Provider catalog exceeds the 1 MiB mobile limit.');
      return result;
    } });
  const models = useMemo(() => creationModels(catalogs.data?.result), [catalogs.data]);
  const matching = useMemo(() => models.filter(item => `${item.model} ${item.provider}`.toLowerCase().includes(search.trim().toLowerCase())), [models, search]);
  const efforts = sessionEfforts(models, root?.meta.model ?? '', root?.meta.provider ?? '');
  async function apply(change: { model: string; provider: string } | { effort: string }) {
    if (busyRef.current || !mounted.current || !visible.current) return;
    try {
      if (runtime.requireReady() !== client || !idleRootSettings(view.getSnapshot(), rootId, rootId)
        || pendingRootSetting(runtime.getSnapshot().commands, runtimeId, rootId) || runtime.isBlocked(rootId))
        throw new Error('Wait for this session’s work and pending settings to finish before changing its model or reasoning.');
      busyRef.current = true; setBusy(true);
      const outcome = 'model' in change
        ? await runtime.run('session.model', { ...change, persist_default: false }, { rootId, intent: { agentId: rootId } })
        : await runtime.run('session.effort', { ...change, persist_default: false }, { rootId, intent: { agentId: rootId } });
      if (outcome.status !== 'succeeded') throw new Error(outcome.failure?.message ?? `Setting change ${outcome.status}.`);
      if (mounted.current) setModelsOpen(false);
    } catch (error) { if (mounted.current && runtime.getSnapshot().client === client) runtime.report(error); }
    finally { busyRef.current = false; if (mounted.current) setBusy(false); }
  }
  return <Stack>
    <Label muted>Model and reasoning changes apply to this session.</Label>
    {!idleRootSettings(snapshot, rootId, rootId) && <Notice>Wait for all active turns, including child agents, to finish before changing these settings.</Notice>}
    {pending && <Notice>A setting change is still being checked. Inspect its delivery in Settings before changing it again.</Notice>}
    <Actions items={[{ label: modelsOpen ? 'Close model selection' : 'Change model', secondary: true, disabled: !connected || busy,
      onPress: () => setModelsOpen(!modelsOpen) }]} />
    {modelsOpen && <Stack>
      <Field label="Find a model or provider" value={search} onChangeText={setSearch} maxLength={128} />
      {catalogs.isFetching && <Loading label="Reading provider catalogs…" />}
      {matching.slice(0, 64).map(item => <RowButton key={JSON.stringify([item.provider, item.model])} title={item.model} detail={item.provider}
        selected={item.model === root?.meta.model && item.provider === root?.meta.provider}
        disabled={!editable || !client.supports('runtime', 'session.model') || item.model === root?.meta.model && item.provider === root?.meta.provider}
        onPress={() => { void apply({ model: item.model, provider: item.provider }); }} />)}
      {matching.length > 64 && <Notice>Showing 64 models. Narrow the search to find another model.</Notice>}
      {!matching.length && !catalogs.isFetching && <Label muted>No matching catalog models. Configure providers on the host.</Label>}
    </Stack>}
    {catalogs.error && <Notice>{catalogs.error.message}</Notice>}
    {catalogs.data?.result?.errors && <Notice>Some provider catalogs are unavailable.</Notice>}
    <Label muted>Reasoning effort for {root?.meta.model}</Label>
    {efforts.slice(0, 32).map(effort => <RowButton key={effort} title={effort === 'off' ? 'Default reasoning' : effort}
      selected={effort === (root?.meta.effort || 'off')}
      disabled={!editable || !client.supports('runtime', 'session.effort') || effort === (root?.meta.effort || 'off')}
      onPress={() => { void apply({ effort }); }} />)}
    <Actions items={[{ label: 'Refresh models', secondary: true, disabled: !connected || catalogs.isFetching,
      onPress: () => { void catalogs.refetch(); } }]} />
  </Stack>;
}
function Conversation({ view, runtimeId, snapshot, agentId, name, enabled, onInspect, onOptions }: { view: SessionView; runtimeId: string; snapshot: DeepReadonly<SessionViewSnapshot>; agentId: string; name: string; enabled: boolean; onInspect(row: TimelineRow): void; onOptions(): void }) {
  const runtime = useRuntime(); const state = useRuntimeState(); const theme = useTheme(); const display = useDisplay(); const insets = useSafeAreaInsets();
  const rootId = view.session.rootId; const root = snapshot.root!;
  const key = draftKey(runtimeId, rootId, agentId);
  const draft = runtime.draft(key);
  const draftStatus = runtime.draftStatus(key);
  const history = snapshot.history[agentId];
  const submitted = useSyncExternalStore(runtime.submitted.subscribe, runtime.submitted.getSnapshot);
  const ownInputs = submitted.filter(i => i.runtimeId === runtimeId && i.rootId === rootId && i.agentId === agentId);
  const rows = useMemo(() => conversationRows(history, agentId === rootId ? root.presentation : root.agent_presentations?.[agentId],
    (root.inbox ?? []).filter(i => i.agent_id === agentId), ownInputs, new Map(state.commands.filter(c => !c.accepted).map(c => [c.record.commandId, c.status === 'checking' ? 'Checking delivery' : c.status]))),
  [history, root, agentId, rootId, submitted, state.commands]);
  const list = useRef<FlashListRef<TimelineRow>>(null);
  const [follow, setFollow] = useState(true); const followRef = useRef(true);
  const [bookmark, setBookmark] = useState<ReadingBookmark | null>();
  const anchor = useRef<ReadingBookmark | null | undefined>(undefined); const appliedRevision = useRef<string | undefined>(undefined);
  const restoring = useRef(false); const restoreEpoch = useRef(0); const listLoaded = useRef(false);
  const [placeNotice, setPlaceNotice] = useState(''); const [delivery, setDelivery] = useState<'queued' | 'steer'>('queued');
  const activeTurn = root.active_turns?.[agentId];
  const blocked = runtime.isBlocked(rootId, agentId);
  const historyReady = !!history && !history.loading && !history.error && history.revision === root.history_revision;
  const live = useRef({ rows, revision: history?.revision, ready: historyReady }); live.current = { rows, revision: history?.revision, ready: historyReady };
  const capturePlace = useRef(() => {}); const savePlace = useRef(() => {});
  capturePlace.current = () => {
    const current = live.current; const instance = list.current;
    if (!instance || !current.ready || restoring.current || appliedRevision.current !== current.revision) return;
    const index = instance.getFirstVisibleIndex(); const layout = instance.getLayout(index);
    const captured = captureReadingBookmark(current.rows, current.revision, followRef.current, layout ? {
      index, rowY: layout.y, firstItemOffset: instance.getFirstItemOffset(), scrollOffset: instance.getAbsoluteLastScrollOffset(),
    } : undefined);
    if (captured) anchor.current = captured;
  };
  savePlace.current = () => {
    capturePlace.current();
    if (anchor.current) void runtime.storage.set('bookmarks', key, anchor.current).catch(runtime.report);
  };
  const listRef = useCallback((instance: FlashListRef<TimelineRow> | null) => {
    if (!instance) {
      // React clears refs before passive effect cleanup. Capture the old ref now.
      savePlace.current(); restoreEpoch.current++; restoring.current = false; listLoaded.current = false;
    }
    list.current = instance;
  }, []);
  useLayoutEffect(() => () => { savePlace.current(); restoreEpoch.current++; restoring.current = false; }, [key, runtime]);
  useLayoutEffect(() => { restoreEpoch.current++; restoring.current = false; }, [history?.revision, state.active]);
  useEffect(() => {
    let mounted = true;
    void runtime.storage.get<unknown>('bookmarks', key).then(value => {
      if (!mounted) return;
      if (value !== undefined && !validReadingBookmark(value)) throw new Error('The saved reading position is unavailable. Showing retained history.');
      anchor.current = value ?? null; setBookmark(value ?? null); setFollow(value?.follow ?? true); followRef.current = value?.follow ?? true;
    }).catch(error => { if (mounted) { runtime.report(error); anchor.current = null; setBookmark(null); } });
    const app = AppState.addEventListener('change', value => {
      if (value === 'background') { savePlace.current(); restoreEpoch.current++; restoring.current = false; }
    });
    return () => { mounted = false; app.remove(); };
  }, [key, runtime]);
  async function restore() {
    const instance = list.current;
    if (!instance || !listLoaded.current || !state.active || restoring.current || bookmark === undefined || !historyReady || !history || !rows.length || appliedRevision.current === history.revision) return;
    const revision = history.revision; const epoch = ++restoreEpoch.current;
    const target = restoreReadingBookmark(rows, revision, anchor.current ?? null);
    restoring.current = true;
    followRef.current = target.latest; setFollow(target.latest);
    try {
      if (target.latest) await instance.scrollToIndex({ index: rows.length - 1, viewPosition: 1, animated: false });
      else if (target.index >= 0) await instance.scrollToIndex({ index: target.index, viewOffset: target.offset, animated: false });
      if (epoch !== restoreEpoch.current || live.current.revision !== revision || list.current !== instance) return;
      appliedRevision.current = revision; restoring.current = false;
      setPlaceNotice(target.fallback ? 'History changed. Showing the nearest retained message to your saved place.' : '');
      capturePlace.current();
    } catch (error) {
      if (epoch !== restoreEpoch.current || list.current !== instance) return;
      appliedRevision.current = revision; restoring.current = false;
      setPlaceNotice('Your saved place could not be restored. Showing retained history.'); runtime.report(error);
    }
  }
  useEffect(() => { void restore(); }, [bookmark, history?.revision, historyReady, rows.length, state.active]);
  function scroll(event: NativeSyntheticEvent<NativeScrollEvent>) {
    if (restoring.current || appliedRevision.current !== history?.revision) return;
    const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
    const nearEnd = contentSize.height - contentOffset.y - layoutMeasurement.height < 100;
    followRef.current = nearEnd; setFollow(nearEnd);
    capturePlace.current();
  }
  async function readTextPage(rowId: string) {
    const epoch = ++restoreEpoch.current;
    restoring.current = true;
    followRef.current = false; setFollow(false);
    // Let follow mode turn off before scrolling. Ignore old native scroll events
    // until FlashList finishes, or they can re-enable following at the old end.
    try {
      await new Promise<void>(resolve => requestAnimationFrame(() => resolve()));
      const instance = list.current;
      const index = live.current.rows.findIndex(row => row.id === rowId);
      if (epoch === restoreEpoch.current && runtime.getSnapshot().active && instance && index >= 0)
        await instance.scrollToIndex({ index, viewPosition: 0, animated: false });
    } finally {
      if (epoch === restoreEpoch.current) { restoring.current = false; capturePlace.current(); }
    }
  }
  async function send() {
    const current = runtime.draft(key);
    if (!current.text.trim() || !enabled || blocked) return;
    const intent = { agentId, draftKey: key, draftRevision: current.revision };
    const preview = { agentId, text: current.text, queued: !!activeTurn };
    try {
      const outcome = agentId === rootId
        ? await runtime.run(delivery === 'steer' && activeTurn ? 'steer' : 'submit', { text: current.text }, { rootId, intent, preview })
        : await runtime.run('agent.submit', { id: agentId, text: current.text, delivery }, { rootId, intent, preview });
      if (['failed', 'cancelled', 'interrupted'].includes(outcome.status)) runtime.report(new Error(outcome.failure?.message ?? `Message ${outcome.status}`));
    } catch (error) { runtime.report(error); }
  }
  function stop() {
    if (!activeTurn) return;
    const promise = agentId === rootId ? runtime.run('cancel', { turn_id: activeTurn }, { rootId, intent: { agentId } })
      : runtime.run('agent.turn.cancel', { id: agentId, turn_id: activeTurn }, { rootId, intent: { agentId } });
    void promise.then(result => { if (result.status !== 'succeeded') runtime.report(new Error(result.failure?.message ?? `Stop ${result.status}`)); }).catch(runtime.report);
  }
  const supported = !!state.client?.supports('runtime', agentId !== rootId ? 'agent.submit' : delivery === 'steer' && activeTurn ? 'steer' : 'submit');
  return <KeyboardAvoidingView behavior="padding" automaticOffset style={{ flex: 1 }}>
    {!!placeNotice && <Notice>{placeNotice}</Notice>}
    {bookmark === undefined ? <Loading label="Restoring your reading place…" /> : <FlashList ref={listRef} data={rows} keyExtractor={row => row.id} getItemType={row => row.role} keyboardShouldPersistTaps="handled"
      renderItem={({ item }) => <ConversationRow row={item} onInspect={onInspect}
        onPageChange={() => { void readTextPage(item.id).catch(runtime.report); }} />}
      maintainVisibleContentPosition={{ autoscrollToBottomThreshold: follow ? 0.2 : undefined, animateAutoScrollToBottom: false, startRenderingFromBottom: rows.length > 12 && (!bookmark || bookmark.follow) }}
      onLoad={() => { listLoaded.current = true; void restore(); }} onScroll={scroll} scrollEventThrottle={100} onMomentumScrollEnd={() => savePlace.current()} onScrollEndDrag={() => savePlace.current()}
      ListHeaderComponent={<Stack style={{ paddingHorizontal: 16 }}>{history?.hasMore && <Actions items={[{ label: history.loading ? 'Loading earlier messages…' : 'Load earlier messages', secondary: true, disabled: !enabled || history.loading, onPress: () => { void view.loadOlder(agentId).catch(runtime.report); } }]} />}{history?.error && <Notice danger>{history.error.message}</Notice>}</Stack>}
      ListEmptyComponent={<View style={{ minHeight: 300 }}>{history?.loading ? <Loading /> : <EmptyState title="What are we working on?" description="Ask a question, plan a change, or give your agent something to build." />}</View>} />}
    {!follow && <View style={{ paddingHorizontal: 16 }}><Actions items={[{ label: 'Jump to latest', secondary: true, onPress: () => {
      restoreEpoch.current++; restoring.current = false; followRef.current = true; setFollow(true); setPlaceNotice('');
      if (historyReady) appliedRevision.current = history?.revision;
      if (anchor.current) anchor.current = { ...anchor.current, follow: true };
      list.current?.scrollToEnd({ animated: !display.reducedMotion }); savePlace.current();
    } }]} /></View>}
    <Stack style={{ paddingHorizontal: 12, paddingTop: 8, paddingBottom: Math.max(12, insets.bottom), gap: 8 }}>
      {!!draft.text && <Label muted={draftStatus !== 'failed'} accessibilityLiveRegion="polite" style={{ fontSize: 12, ...(draftStatus === 'failed' ? { color: theme.colors.error } : {}) }}>
        {draftStatus === 'saving' ? 'Saving draft…' : draftStatus === 'failed' ? 'Draft not saved. Copy your text before leaving.' : 'Draft saved on this phone.'}
      </Label>}
      {activeTurn && <View style={{ flexDirection: 'row', gap: 16 }}>
        {(['queued', 'steer'] as const).map(mode => <Pressable key={mode} accessibilityRole="radio" accessibilityState={{ checked: delivery === mode }} onPress={() => setDelivery(mode)} style={{ minHeight: 44, justifyContent: 'center' }}><Label style={{ color: delivery === mode ? theme.colors.primary : theme.colors.muted, fontSize: 13 }}>{mode === 'queued' ? 'Queue message' : 'Steer current work'}</Label></Pressable>)}
      </View>}
      {blocked && <Notice>Checking the previous delivery. Your new draft is retained.</Notice>}
      <Composer value={draft.text} onChangeText={text => { try { runtime.setDraft(key, text); } catch (error) { runtime.report(error); } }} onSend={() => { void send(); }} disabled={!enabled || !supported || blocked || !draft.text.trim()} offline={!state.ready} model={root.meta.model || 'Host default'} onOptions={onOptions} onStop={activeTurn && enabled && state.client?.supports('runtime', agentId === rootId ? 'cancel' : 'agent.turn.cancel') ? stop : undefined} />
    </Stack>
  </KeyboardAvoidingView>;
}
