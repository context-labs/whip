import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import { AppState, Pressable, ScrollView, View, type NativeScrollEvent, type NativeSyntheticEvent } from 'react-native';
import { router, useIsFocused, useLocalSearchParams } from 'expo-router';
import * as Crypto from 'expo-crypto';
import { useQuery } from '@tanstack/react-query';
import { ChevronDown, MoreHorizontal } from 'lucide-react-native';
import { Composer } from '../../components/composer';
import { SessionControls } from '../../components/session-controls';
import { ExecutionDetails } from '../../components/execution-details';
import { SessionMenu } from '../../components/session-menu';
import { EmptyState, IconButton, ScreenHeader, Sheet, StatusBadge, Text } from '../../ui';
import { FlashList, type FlashListRef } from '@shopify/flash-list';
import { KeyboardAvoidingView } from 'react-native-keyboard-controller';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { conversationRows, type ReadingBookmark, type TimelineRow } from '@whip/app/presentation';
import { useSessionView } from '@whip/sdk/react';
import type { Session as SessionRecord, Tree } from '@whip/protocol';
import type { DeepReadonly, SessionView, SessionViewSnapshot } from '@whip/sdk/state';
import { RuntimeScope, useSessionOwner, useRuntime, useRuntimeState } from '../../runtime/context';
import type { SessionLease } from '../../runtime/runtime';
import { useWorkspace, useWorkspaceState } from '../../runtime/workspace-context';
import { draftKey } from '../../runtime/address';
import { Actions, Field, Label, Loading, Notice, RowButton, Screen, Stack } from '../../components/primitives';
import { BodyInspector, ConversationRow } from '../../components/conversation';
import { Requests } from '../../components/requests';
import { useDisplay, useTheme } from '../../theme/theme';
import { captureReadingBookmark, restoreReadingBookmark, validReadingBookmark } from '../../features/reading';
import { creationModels, readModels, type ModelSelection } from '../../features/creation';
import { idleRootSettings, sessionEfforts } from '../../features/session-settings';

export default function SessionRoute() {
  const params = useLocalSearchParams<{ runtimeId?: string; hostId?: string }>();
  const workspace = useWorkspace(); useWorkspaceState(); const insets = useSafeAreaInsets();
  const valid = typeof params.runtimeId === 'string' && params.runtimeId.length > 0 && params.runtimeId.length <= 512 && (params.hostId === undefined || typeof params.hostId === 'string' && params.hostId.length <= 512);
  const runtime = valid ? workspace.sessionRuntime(params.runtimeId!, params.hostId) : undefined;
  if (!runtime) return <Screen scroll={false}><View style={{ paddingTop: insets.top }}><ScreenHeader title="Session" onBack={() => router.back()} /></View><EmptyState title={valid ? 'Reconnect this host' : 'Session link unavailable'} description="Connect the host that owns this conversation. Your drafts and delivery records stay on this phone." action={{ label: 'Manage hosts', onPress: () => router.push('/settings/hosts') }} /></Screen>;
  return <RuntimeScope runtime={runtime}><SessionScreen /></RuntimeScope>;
}
export function SessionScreen() {
  const params = useLocalSearchParams<{ rootId: string; runtimeId: string; agentId?: string; requests?: string }>();
  const state = useRuntimeState();
  const rootId = typeof params.rootId === 'string' ? params.rootId : '', runtimeId = typeof params.runtimeId === 'string' ? params.runtimeId : '';
  const agentId = typeof params.agentId === 'string' ? params.agentId : rootId;
  const valid = /^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$/.test(rootId) && /^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$/.test(agentId) && state.host?.runtimeId === runtimeId;
  const metadata = useQuery({ queryKey: [runtimeId, 'session-metadata', rootId, agentId], enabled: valid && state.ready && state.active,
    queryFn: async ({ signal }) => {
      const client = state.client!; const root = await client.session(rootId).get({ signal });
      const recipient = agentId === rootId ? root : await client.session(agentId).get({ signal });
      if (root.parent_id !== null || root.tree_id !== recipient.tree_id) throw new Error('This recipient does not belong to the selected root session.');
      const tree = await client.trees.get(root.tree_id, { signal });
      return { root, recipient, tree };
    }, refetchInterval: state.active ? 10_000 : false });
  const lease = useSessionOwner(valid && metadata.data ? agentId : '', runtimeId);
  if (!valid) return <Screen><Notice>This session link belongs to another host or has an invalid recipient.</Notice></Screen>;
  if (metadata.error) return <Screen><Notice danger>{metadata.error.message}</Notice></Screen>;
  return lease && metadata.data && lease.view.getSnapshot().sessionID === agentId ? <SessionContent key={JSON.stringify([runtimeId, rootId, agentId])} lease={lease} {...metadata.data} runtimeId={runtimeId} showRequests={params.requests === 'true'} /> : <Loading label="Opening session…" />;
}
function SessionContent({ lease, root, recipient, tree, runtimeId, showRequests }: { lease: SessionLease; root: SessionRecord; recipient: SessionRecord; tree: Tree; runtimeId: string; showRequests: boolean }) {
  const runtime = useRuntime(), state = useRuntimeState(), theme = useTheme(), insets = useSafeAreaInsets();
  const view = lease.view, snapshot = useSessionView(view);
  const [sheet, setSheet] = useState<'agents' | 'requests' | 'details' | 'body' | 'menu' | undefined>(showRequests ? 'requests' : undefined);
  const [inspection, setInspection] = useState<TimelineRow>(); const [after, setAfter] = useState<string>();
  const agents = useQuery({ queryKey: [runtimeId, 'tree-agents', tree.id, after], enabled: sheet === 'agents' && state.active && state.ready,
    queryFn: async ({ signal }) => { const result = await state.client!.sessions.list(tree.id, { after, limit: 32 }, { signal }); if (new TextEncoder().encode(JSON.stringify(result)).byteLength > 2 << 20) throw new Error('Recipient page exceeds the mobile limit.'); return result; } });
  const name = recipient.id === root.id ? 'Root agent' : recipient.definition.id;
  const pending = BigInt(snapshot.activity?.pending_question_count ?? '0') + BigInt(snapshot.activity?.pending_permission_count ?? '0');
  const enabled = state.ready && snapshot.status === 'live' && !tree.metadata.archived && recipient.lifecycle === 'active';
  return <Screen scroll={false}>
    <View style={{ paddingTop: insets.top }}><ScreenHeader title={tree.metadata.title || 'New session'} onBack={() => router.back()} right={<IconButton label="Session menu" onPress={() => setSheet('menu')}><MoreHorizontal size={22} color={theme.colors.foreground} /></IconButton>} /></View>
    <Stack style={{ paddingHorizontal: 20, paddingBottom: 12, gap: 8 }}><View style={{ flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', gap: 12 }}><Pressable accessibilityRole="button" accessibilityLabel={`Recipient: ${name}. Choose agent`} onPress={() => setSheet('agents')} style={{ minHeight: 44, flex: 1, flexDirection: 'row', alignItems: 'center', gap: 6 }}><Text variant="caption" muted numberOfLines={1}>{name} · {state.host?.name}</Text><ChevronDown size={14} color={theme.colors.muted} /></Pressable><StatusBadge label={!state.ready ? 'Offline' : snapshot.activity?.active_turn ? 'Working' : 'Ready'} tone={!state.ready ? 'warning' : snapshot.activity?.active_turn ? 'primary' : 'muted'} /></View>
      {pending > 0n && <Actions items={[{ label: `${pending} requests need attention`, onPress: () => setSheet('requests'), testID: 'open-requests' }]} />}
    </Stack>
    {state.error && <Stack style={{ paddingHorizontal: 20 }}><Notice danger>{state.error}</Notice><Actions items={[{ label: 'Dismiss message', secondary: true, onPress: runtime.clearError }]} /></Stack>}
    {tree.metadata.archived && <Notice>This session is archived. Open the session menu to restore it.</Notice>}
    {snapshot.status !== 'live' && <Notice>{snapshot.unavailable ? 'This session is unavailable on the host.' : snapshot.error?.message ?? 'Reconnecting… Last observed messages remain visible.'}</Notice>}
    <Conversation view={view} rootId={root.id} runtimeId={runtimeId} snapshot={snapshot} agentId={recipient.id} model={recipient.configuration.model.name} enabled={enabled} onOptions={() => setSheet('details')} onInspect={row => { setInspection(row); setSheet('body'); }} />
    <Sheet title={sheet === 'menu' ? 'Session' : sheet === 'agents' ? 'Choose recipient' : sheet === 'requests' ? 'Needs you' : sheet === 'body' ? 'Retained content' : 'Session details'} visible={!!sheet} onClose={() => setSheet(undefined)} full={sheet !== 'menu'}>
      <View style={{ flex: 1, backgroundColor: theme.colors.panel }}>{sheet === 'menu' ? <SessionMenu rootId={root.id} tree={tree} onDetails={() => setSheet('details')} onClose={() => setSheet(undefined)} /> : sheet === 'body' && inspection ? <BodyInspector row={inspection} rootId={root.id} agentId={recipient.id} /> : sheet === 'requests' ? <Requests rootId={root.id} sessionId={recipient.id} view={view} disabled={!enabled} /> : <ScrollView contentContainerStyle={{ padding: 20, gap: 16 }}>
        {sheet === 'agents' ? <>{agents.isFetching && <Loading />}{agents.error && <Notice danger>{agents.error.message}</Notice>}
          {(agents.data?.items ?? []).map(item => <RowButton key={item.id} title={item.id === root.id ? 'Root agent' : item.definition.id} detail={`${item.lifecycle} · ${item.id}`} selected={item.id === recipient.id} onPress={() => { setSheet(undefined); router.setParams({ agentId: item.id }); }} />)}
          {agents.data?.items?.length === 32 && <Actions items={[{ label: 'Next recipients', onPress: () => setAfter(agents.data!.items!.at(-1)!.id) }]} />}{after && <Actions items={[{ label: 'First recipients', onPress: () => setAfter(undefined) }]} />}
        </> : <><Label muted>Directory</Label><Label selectable>{recipient.working_directory}</Label><Label muted>Model</Label><Label>{recipient.configuration.model.name} · {recipient.configuration.model.provider}</Label><Label muted>Reasoning effort</Label><Label>{recipient.configuration.model.effort || 'Default'}</Label>
          {recipient.id === root.id && <RootSettings view={view} session={recipient} />}
          <SessionControls rootId={root.id} session={recipient} view={view} /><ExecutionDetails session={recipient} view={view} execution={lease.execution} />
          <Label muted>Root ID</Label><Label selectable>{root.id}</Label><Label muted>Recipient ID</Label><Label selectable>{recipient.id}</Label>
        </>}
      </ScrollView>}</View>
    </Sheet>
  </Screen>;
}
function RootSettings({ view, session }: { view: SessionView; session: SessionRecord }) {
  const runtime = useRuntime(), state = useRuntimeState(), snapshot = useSessionView(view);
  const [open, setOpen] = useState(false), [search, setSearch] = useState(''), [busy, setBusy] = useState(false), [uncertain, setUncertain] = useState(false); const lock = useRef(false);
  const current = session.configuration.model;
  const catalogs = useQuery({ queryKey: [state.host?.runtimeId, 'provider.catalogs'], enabled: open && state.ready && state.active, queryFn: ({ signal }) => readModels(state.client!, signal) });
  const models = useMemo(() => creationModels(catalogs.data), [catalogs.data]);
  const matching = models.filter(item => `${item.model} ${item.provider}`.toLowerCase().includes(search.trim().toLowerCase()));
  const editable = state.ready && idleRootSettings(snapshot, session.id, session.id) && !busy && !uncertain && !runtime.isBlocked(session.id);
  async function apply(model: ModelSelection) {
    if (lock.current || !editable) return; if (!idleRootSettings(view.getSnapshot(), session.id, session.id)) { runtime.report(new Error('Refresh the current session before changing settings.')); return; } lock.current = true; setBusy(true);
    try { await runtime.requireReady().session(session.id).configure(session.config_revision, { model }); setOpen(false); }
    catch (error) { setUncertain(true); runtime.report(error); }
    finally { await runtime.query.invalidateQueries({ queryKey: [state.host?.runtimeId, 'session-metadata'] }); lock.current = false; setBusy(false); }
  }
  return <Stack><Label muted>Model changes preserve this session’s execution environment. Host policy rejects changes while incompatible work is active.</Label>
    {uncertain && <><Notice>The setting response was not confirmed. Read the current configuration before making another change.</Notice><Actions items={[{ label: 'Read current settings', disabled: busy || !state.ready, onPress: () => { void runtime.query.refetchQueries({ queryKey: [state.host?.runtimeId, 'session-metadata'] }, { throwOnError: true }).then(() => setUncertain(false)).catch(runtime.report); } }]} /></>}
    <Actions items={[{ label: open ? 'Close model selection' : 'Change model', onPress: () => setOpen(!open), disabled: busy }]} />
    {open && <><Field label="Find a model or provider" value={search} onChangeText={setSearch} maxLength={128} />{catalogs.isFetching && <Loading />}{catalogs.error && <Notice>{catalogs.error.message}</Notice>}
      {matching.slice(0, 64).map(item => <RowButton key={item.provider + '/' + item.model} title={item.model} detail={item.provider} selected={item.model === current.name && item.provider === current.provider} disabled={!editable} onPress={() => { void apply({ name: item.model, provider: item.provider, effort: '' }); }} />)}{matching.length > 64 && <Notice>Showing 64 choices. Narrow your search.</Notice>}
      <Label muted>Reasoning effort</Label>{sessionEfforts(models, current.name, current.provider).map(effort => <RowButton key={effort} title={effort || 'Default reasoning'} selected={effort === current.effort} disabled={!editable} onPress={() => { void apply({ ...current, effort }); }} />)}
    </>}
  </Stack>;
}
function Conversation({ view, rootId, runtimeId, snapshot, agentId, model, enabled, onInspect, onOptions }: { view: SessionView; rootId: string; runtimeId: string; snapshot: DeepReadonly<SessionViewSnapshot>; agentId: string; model: string; enabled: boolean; onInspect(row: TimelineRow): void; onOptions(): void }) {
  const runtime = useRuntime(), state = useRuntimeState(), theme = useTheme(), display = useDisplay(), insets = useSafeAreaInsets();
  const key = draftKey(runtimeId, rootId, agentId), draft = runtime.draft(key), draftStatus = runtime.draftStatus(key), history = snapshot.history;
  const [inputAfter, setInputAfter] = useState<string>();
  const inbox = useQuery({ queryKey: [runtimeId, 'inbox', agentId, inputAfter], enabled: enabled && state.active,
    queryFn: async ({ signal }) => { const page = await state.client!.session(agentId).inputs.page({ state: 'queued', after: inputAfter, limit: 64 }, { signal }); if (new TextEncoder().encode(JSON.stringify(page)).byteLength > 256 << 10) throw new Error('Input page exceeds the mobile limit.'); return page; }, refetchInterval: state.active ? 3000 : false });
  const submitted = useSyncExternalStore(runtime.submitted.subscribe, runtime.submitted.getSnapshot);
  const ownInputs = submitted.filter(item => item.runtimeId === runtimeId && item.rootId === rootId && item.agentId === agentId);
  const rows = useMemo(() => conversationRows(history, snapshot.preview, inbox.data?.items ?? [], ownInputs, new Map(state.commands.filter(item => !item.knownAccepted).map(item => [item.record.commandId, item.status]))), [history, snapshot.preview, inbox.data, submitted, state.commands]);
  useEffect(() => { const observed = new Set([...(inbox.data?.items ?? []).map(item => item.id), ...history.messages.map(item => item.input_id)]); runtime.submitted.confirm(runtime.submitted.getSnapshot().filter(item => item.runtimeId === runtimeId && item.rootId === rootId && item.agentId === agentId && !!item.inputId && observed.has(item.inputId)).map(item => item.id), runtimeId); }, [inbox.data, history.messages, runtimeId, rootId, agentId, submitted, runtime]);
  const list = useRef<FlashListRef<TimelineRow>>(null);
  const latestIntent = useRef(0);
  const [loadingLatest, setLoadingLatest] = useState(false);
  const [follow, setFollow] = useState(true); const followRef = useRef(true);
  const [bookmark, setBookmark] = useState<ReadingBookmark | null>();
  const anchor = useRef<ReadingBookmark | null | undefined>(undefined); const appliedRevision = useRef<string | undefined>(undefined);
  const restoring = useRef(false); const restoreEpoch = useRef(0); const listLoaded = useRef(false);
  const [placeNotice, setPlaceNotice] = useState(''); const [delivery, setDelivery] = useState<'queued' | 'steer'>('queued');
  const activeTurn = snapshot.activity?.active_turn;
  const blocked = runtime.isBlocked(rootId, agentId);
  const historyReady = snapshot.status === 'live' && history.snapshot !== null;
  const historyRevision = history.snapshot?.revision;
  const live = useRef({ rows, revision: historyRevision, ready: historyReady }); live.current = { rows, revision: historyRevision, ready: historyReady };
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
  useLayoutEffect(() => { latestIntent.current++; restoreEpoch.current++; restoring.current = false; }, [historyRevision, state.active]);
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
    if (!instance || !listLoaded.current || !state.active || restoring.current || bookmark === undefined || !historyReady || !history || !rows.length || appliedRevision.current === historyRevision!) return;
    const revision = historyRevision!; const epoch = ++restoreEpoch.current;
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
  useEffect(() => { void restore(); }, [bookmark, historyRevision, historyReady, rows.length, state.active]);
  function scroll(event: NativeSyntheticEvent<NativeScrollEvent>) {
    if (restoring.current || appliedRevision.current !== historyRevision) return;
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
    const current = runtime.draft(key); if (!current.text.trim() || !enabled || blocked) return;
    const requestId = Crypto.randomUUID();
    try {
      const result = await runtime.run('sessions.submit', { session_id: agentId, source: 'user', parts: [{ type: 'text', text: current.text }], identity: { client_id: runtime.requireReady().clientID, request_id: requestId }, delivery,
        ...(delivery === 'steer' && activeTurn ? { target_turn_id: activeTurn.id } : {}) }, { rootId, intent: { draftKey: key, draftRevision: current.revision } });
      runtime.submitted.acknowledge(result, runtimeId);
    } catch (error) { runtime.report(error); }
  }
  function stop() { if (activeTurn) void runtime.requireReady().session(agentId).cancelTurn(activeTurn.id).then(() => view.refresh()).catch(runtime.report); }
  return <KeyboardAvoidingView behavior="padding" automaticOffset style={{ flex: 1 }}>
    {!!placeNotice && <Notice>{placeNotice}</Notice>}
    {bookmark === undefined ? <Loading label="Restoring your reading place…" /> : <FlashList ref={listRef} data={rows} keyExtractor={row => row.id} getItemType={row => row.role} keyboardShouldPersistTaps="handled"
      renderItem={({ item }) => item.historyGap ? <Notice>This message exceeds this phone’s history window ({item.historyGap.bytes} bytes). Open the host app to inspect it.</Notice> : <ConversationRow row={item} onInspect={onInspect} onPageChange={() => { void readTextPage(item.id).catch(runtime.report); }} />}
      maintainVisibleContentPosition={{ autoscrollToBottomThreshold: follow ? 0.2 : undefined, animateAutoScrollToBottom: false, startRenderingFromBottom: rows.length > 12 && (!bookmark || bookmark.follow) }}
      onScrollBeginDrag={() => { latestIntent.current++; }} onLoad={() => { listLoaded.current = true; void restore(); }} onScroll={scroll} scrollEventThrottle={100} onMomentumScrollEnd={() => savePlace.current()} onScrollEndDrag={() => savePlace.current()}
      ListHeaderComponent={<Stack style={{ paddingHorizontal: 16 }}>{history.olderCursor && <Actions items={[{ label: 'Load earlier messages', secondary: true, disabled: !enabled, onPress: () => { void view.loadOlder().catch(runtime.report); } }]} />}{snapshot.error && <Notice danger>{snapshot.error.message}</Notice>}</Stack>}
      ListEmptyComponent={<View style={{ minHeight: 300 }}>{snapshot.status === 'loading' ? <Loading /> : <EmptyState title="What are we working on?" description="Ask a question, plan a change, or give your agent something to build." />}</View>} />}
    {(!follow || history.latestMissing) && <View style={{ paddingHorizontal: 16 }}><Actions items={[{ label: loadingLatest ? 'Loading latest…' : 'Jump to latest', secondary: true, disabled: loadingLatest || (history.latestMissing && !enabled), onPress: () => { void (async () => {
      const request = ++latestIntent.current;
      if (history.latestMissing) {
        followRef.current = false; setFollow(false); setLoadingLatest(true);
        try { await view.latest(); await new Promise<void>(resolve => requestAnimationFrame(() => resolve())); }
        catch (error) { runtime.report(error); return; }
        finally { setLoadingLatest(false); }
        if (request !== latestIntent.current || !list.current || !runtime.getSnapshot().active) return;
      }
      restoreEpoch.current++; restoring.current = false; followRef.current = true; setFollow(true); setPlaceNotice('');
      if (historyReady) appliedRevision.current = historyRevision;
      if (anchor.current) anchor.current = { ...anchor.current, follow: true };
      list.current?.scrollToEnd({ animated: !display.reducedMotion }); savePlace.current();
    })(); } }]} /></View>}
    <Stack style={{ paddingHorizontal: 12, paddingTop: 8, paddingBottom: Math.max(12, insets.bottom), gap: 8 }}>
      {inbox.error && <Notice>{inbox.error.message}</Notice>}
      {(inbox.data?.items ?? []).map(input => <View key={input.id}><Label numberOfLines={2}>{input.text_preview || `${input.attachment_count} attachments`}</Label><Actions items={[
        { label: 'Cancel queued message', disabled: !enabled, secondary: true, onPress: () => { void runtime.requireReady().session(agentId).inputs.cancel(input.id).then(() => inbox.refetch()).catch(runtime.report); } },
        ...(activeTurn ? [{ label: 'Steer current turn with this message', disabled: !enabled || blocked, onPress: () => { void runtime.run('inputs.steer', { session_id: agentId, input_id: input.id, turn_id: activeTurn.id, edit_id: Crypto.randomUUID() }, { rootId }).catch(runtime.report); } }] : []),
      ]} /></View>)}
      {inbox.data?.next_cursor && <Actions items={[{ label: 'Next queued messages', onPress: () => setInputAfter(inbox.data!.next_cursor!) }]} />}{inputAfter && <Actions items={[{ label: 'First queued messages', onPress: () => setInputAfter(undefined) }]} />}
      {!!draft.text && <Label muted={draftStatus !== 'failed'} accessibilityLiveRegion="polite" style={{ fontSize: 12, ...(draftStatus === 'failed' ? { color: theme.colors.error } : {}) }}>{draftStatus === 'saving' ? 'Saving draft…' : draftStatus === 'failed' ? 'Draft not saved. Copy your text before leaving.' : 'Draft saved on this phone.'}</Label>}
      {activeTurn && <View style={{ flexDirection: 'row', gap: 16 }}>{(['queued', 'steer'] as const).map(mode => <Pressable key={mode} accessibilityRole="radio" accessibilityState={{ checked: delivery === mode }} onPress={() => setDelivery(mode)} style={{ minHeight: 44, justifyContent: 'center' }}><Label style={{ color: delivery === mode ? theme.colors.primary : theme.colors.muted, fontSize: 13 }}>{mode === 'queued' ? 'Queue message' : 'Steer current work'}</Label></Pressable>)}</View>}
      {blocked && <Notice>Inspect the previous delivery in Drafts &amp; recovery. Your new draft is retained.</Notice>}
      <Composer value={draft.text} onChangeText={text => { try { runtime.setDraft(key, text); } catch (error) { runtime.report(error); } }} onSend={() => { void send(); }} disabled={!enabled || blocked || !draft.text.trim()} offline={!state.ready} model={model || 'Host default'} onOptions={onOptions} onStop={activeTurn && enabled ? stop : undefined} />
    </Stack>
  </KeyboardAvoidingView>;
}
