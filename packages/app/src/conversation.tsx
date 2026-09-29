import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import type { Client, DurableCommand, Session } from '@whip/sdk';
import { useSessionView, useExecutionView } from '@whip/sdk/react';
import {
  cellExecutionRows,
  type ExecutionView,
  type SessionView,
  type TraceView as TraceObservation,
} from '@whip/sdk/state';
import { useQuery } from '@tanstack/react-query';
import { historyBoundary, historyGroupEnd, readLargeMessage } from './conversation-history';
import {
  Badge,
  Button,
  CodeBlock,
  Dialog,
  Menu,
  Sheet,
  Spinner,
} from '@whip/ui';
import { MoreHorizontal } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { useSessionActions } from './session-actions';
import { layout } from './styles';
import type { ReadingListActions } from './reading-list';
import { Timeline, conversationRows, type TimelineRow } from './timeline';
import { ErrorNotice } from './error-feedback';
import { Composer } from './composer';
import { ComposerQueue } from './composer-queue';
import { ChatDropSurface } from './chat-file-drop';
import { ReplView } from './repl-view';
import { TraceView } from './trace-view';
import { AgentTurnNotice, useSelectedAgent } from './agent-turn-notice';
import {
  activityStatus,
  CurrentActivity,
  TranscriptWorking,
} from './chat-activity';
import { AgentDock } from './agent-dock';
import { ScheduledWakeNotice } from './scheduled-wake-notice';
import {
  conversationActivityRows,
  isActivityGroup,
  type ActivityGroup,
} from './chat-activity-rows';
import { SessionTopBar } from './session-top-bar';
import { openChildChat, openSessionView } from './session-tab-routing';
import { PickerSkeletons, SessionModelPicker } from './model-selection';
import { PermissionModePicker } from './permission-mode';
import {
  admittedText,
  isAcceptedInputNotice,
  queuedInputRows,
} from './input-presentation';
import { PendingRequests } from './requests';
import type { InspectorSection } from './navigation';
import { SessionInspector } from './inspector';
import {
  isSessionTab,
  selectedSessionTab,
  sessionViewPane,
  sessionSearch,
  type ChildChatCompanion,
  type SessionViewKind,
} from './session-tabs';

const loadingStyles = stylex.create({
  overlay: {
    position: 'relative',
    flex: '1 1 0',
    zIndex: 2,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    transform: 'translateY(-20px)',
    minWidth: 0,
    minHeight: 0,
    backgroundColor: 'inherit',
  },
  indicator: {
    width: 40,
    height: 40,
    display: 'grid',
    placeItems: 'center',
    color: surface.secondaryText,
    backgroundColor: `color-mix(in srgb, ${colors.foreground} 3%, ${colors.background})`,
    borderWidth: 1,
    borderStyle: 'solid',
    borderColor: surface.quietBorder,
    borderRadius: '50%',
    boxShadow: 'none',
    opacity: { [scale.reducedMotion]: 0.88 },
  },
});

export function SessionLoading() {
  return (
    <div
      role="status"
      aria-label="Opening session"
      {...stylex.props(loadingStyles.overlay)}
    >
      <span {...stylex.props(loadingStyles.indicator)}>
        <Spinner size={18} label="Opening session" />
      </span>
    </div>
  );
}

/** Route admission/status only. Workspace reconciliation is the sole root lease owner. */
export function ConversationRoute({
  rootId,
  runtimeId,
}: {
  rootId: string;
  runtimeId: string;
}) {
  const runtime = useRuntime();
  useSessionTabs();
  const { hosts } = useAppState();
  const client = hosts.find((host) => host.runtimeId === runtimeId)?.client;
  // The tab strip renders open tabs; this body paints only when no tab holds the session (it is also mounted for a frame while a route commit leaves the tab).
  if (
    runtime.tabs
      .workspace()
      .tabs.some(
        (tab) =>
          isSessionTab(tab) &&
          tab.runtimeId === runtimeId &&
          tab.rootId === rootId,
      )
  )
    return null;
  if (!runtime.tabs.canOpen(runtimeId, rootId))
    return (
      <div {...stylex.props(layout.empty)}>
        <h1 {...stylex.props(layout.emptyTitle)}>Your session tabs are full</h1>
        <p>
          Close an open tab to view this session. Its work stays on the host.
        </p>
      </div>
    );
  if (!client)
    return (
      <div {...stylex.props(layout.empty)}>
        Connect to the session’s execution host.
      </div>
    );
  return <ConversationHostStatus client={client} runtimeId={runtimeId} />;
}
function ConversationHostStatus({
  client,
  runtimeId,
}: {
  client: Client;
  runtimeId: string;
}) {
  if (client.runtimeID !== runtimeId)
    return (
      <div {...stylex.props(layout.empty)}>
        <h1 {...stylex.props(layout.emptyTitle)}>
          This session belongs to another host
        </h1>
        <p>
          Reconnect to its original runtime to continue. No session requests
          were sent to this host.
        </p>
        <Link to="/">Choose a session</Link>
      </div>
    );
  return <div {...stylex.props(layout.empty)}>Opening session…</div>;
}

type HistoryConfirmation = { afterSelection?: boolean } & (
  | { action: 'fork'; command: DurableCommand<'sessions.fork'> }
  | { action: 'rewind' | 'clear'; command: DurableCommand<'sessions.rewind'> });

/** Shared native observers are leased by the workspace. This component owns only
 * presentation, bounded control reads, drafts and explicit action confirmations. */
export function SessionContent({
  client,
  session,
  rootId,
  kind,
  view,
  execution,
  trace,
  expectedRuntimeId,
  agentId,
  panel,
  viewId,
  summaryCwd,
}: {
  client: Client;
  session: Session;
  rootId: string;
  kind: SessionViewKind;
  view: SessionView;
  execution: ExecutionView;
  trace?: TraceObservation;
  expectedRuntimeId: string;
  agentId: string;
  panel?: InspectorSection;
  viewId?: string;
  summaryCwd?: string;
}) {
  const runtime = useRuntime();
  useSessionTabs();
  const state = useSessionView(view),
    evidence = useExecutionView(execution);
  const { commands, preferences, hosts } = useAppState();
  const host = hosts.find((host) => host.runtimeId === expectedRuntimeId);
  const identityValid =
    client.runtimeID === expectedRuntimeId &&
    session.client === client &&
    session.id === agentId &&
    state.runtimeID === expectedRuntimeId &&
    state.sessionID === session.id &&
    evidence.runtimeID === expectedRuntimeId &&
    evidence.sessionID === session.id;
  const hostConnected =
    identityValid &&
    host?.client === client &&
    host.state === 'connected' &&
    runtime.connections.isAttached(client);
  const rootSession = useMemo(() => client.session(rootId), [client, rootId]);
  const selectedQuery = useSelectedAgent(session, hostConnected),
    rootQuery = useSelectedAgent(rootSession, hostConnected);
  const selected = selectedQuery.data,
    root = rootQuery.data;
  const scopeValid =
    !!selected &&
    !!root &&
    root.parent_id === null &&
    root.tree_id === selected.tree_id;
  const treeQuery = useQuery({
    queryKey: [
      'conversation-tree',
      client.runtimeID,
      client.processEpoch,
      rootId,
      root?.tree_id,
    ],
    queryFn: async ({ signal }) => {
      if (!root) throw new Error('Root metadata unavailable');
      return client.trees.get(root.tree_id, { signal });
    },
    enabled: hostConnected && scopeValid,
    retry: false,
    gcTime: 0,
    refetchInterval: hostConnected && scopeValid ? 3000 : false,
  });
  const tree = treeQuery.data;
  // Observation owns content readiness. Tree details only enrich controls;
  // loading or failing them must not interrupt an already observed session.
  const observing = hostConnected && state.status === 'live';
  const connected =
    observing && scopeValid && !selectedQuery.error && !rootQuery.error;
  const opening =
    hostConnected && !state.history.snapshot && !state.error &&
    !selectedQuery.error && !rootQuery.error &&
    (state.status === 'idle' || state.status === 'loading');
  const controlsPending =
    hostConnected && (!selected || !root) &&
    !selectedQuery.error && !rootQuery.error;
  useEffect(() => {
    if (tree?.metadata.title)
      runtime.tabs.titles(
        expectedRuntimeId,
        new Map([[rootId, tree.metadata.title]]),
      );
  }, [runtime, expectedRuntimeId, rootId, tree?.metadata.title]);
  const [queueCursor, setQueueCursor] = useState<string>();
  const queue = useQuery({
    queryKey: [
      'conversation-queue',
      client.runtimeID,
      client.processEpoch,
      session.id,
      queueCursor,
    ],
    queryFn: ({ signal }) =>
      session.inputs.page(
        { state: 'queued', after: queueCursor, limit: 100 },
        { signal },
      ),
    enabled: connected,
    gcTime: 0,
    retry: false,
    refetchInterval: connected ? 1500 : false,
  });
  useEffect(() => setQueueCursor(undefined), [client, session.id]);
  const submitted = useSyncExternalStore(
    runtime.submittedInputs.subscribe,
    runtime.submittedInputs.getSnapshot,
  );
  const localInputs = useMemo(
    () =>
      submitted.filter(
        (item) =>
          item.runtimeId === expectedRuntimeId &&
          item.rootId === rootId &&
          item.agentId === session.id,
      ),
    [submitted, expectedRuntimeId, rootId, session.id],
  );
  const deliveries = useMemo(
    () =>
      new Map(
        commands
          .filter(
            (item) => item.runtimeId === expectedRuntimeId && item.delivery,
          )
          .map((item) => [
            item.commandId,
            item.delivery === 'absent'
              ? 'Not received · explicit retry available'
              : 'Checking delivery…',
          ]),
      ),
    [commands, expectedRuntimeId],
  );
  const inbox = queue.data?.items ?? [];
  const queueRows = useMemo(
    () =>
      queuedInputRows(
        inbox.map((item) => ({ item, stale: !connected || !!queue.error })),
        localInputs,
        deliveries,
      ),
    [inbox, connected, queue.error, localInputs, deliveries],
  );
  const rows = useMemo(
    () =>
      kind === 'chat'
        ? conversationRows(
            state.history,
            state.preview ?? undefined,
            inbox,
            localInputs,
            deliveries,
          )
        : [],
    [kind, state.history, state.preview, inbox, localInputs, deliveries],
  );
  const cells = useMemo(
    () => cellExecutionRows(evidence, state.history.messages),
    [evidence, state.history.messages],
  );
  const active = state.activity?.active_turn ?? undefined,
    activeTurn = active?.id;
  const lastTurn = active ?? evidence.turns[0];
  const previousGroups = useRef<readonly ActivityGroup[]>([]);
  const groupRevision = `${client.processEpoch}:${session.id}:${state.history.snapshot?.revision ?? ''}`;
  const priorRevision = useRef(groupRevision);
  const activityRows = useMemo(
    () =>
      conversationActivityRows(
        rows,
        cells,
        priorRevision.current === groupRevision ? previousGroups.current : [],
        activeTurn,
        evidence.operations,
      ),
    [rows, cells, activeTurn, evidence.operations, groupRevision],
  );
  useLayoutEffect(() => {
    priorRevision.current = groupRevision;
    previousGroups.current = activityRows.filter(isActivityGroup).slice(-128);
  }, [activityRows, groupRevision]);
  useEffect(() => {
    const accepted = new Set([
      ...state.history.messages.flatMap((message) =>
        message.input_id ? [message.input_id] : [],
      ),
      ...inbox.map((item) => item.id),
    ]);
    runtime.submittedInputs.confirm(
      localInputs
        .filter((input) => input.inputId && accepted.has(input.inputId))
        .map((input) => input.id),
      expectedRuntimeId,
    );
  }, [runtime, localInputs, state.history.messages, inbox, expectedRuntimeId]);
  const admitted = inbox.filter(isAcceptedInputNotice);
  const delivery = rows
    .filter((row) => row.role === 'user' && row.delivery)
    .at(-1)?.delivery;
  const resourceError =
    selectedQuery.error ||
    rootQuery.error ||
    treeQuery.error ||
    state.error?.message;
  const status = opening
    ? { text: 'Loading session…', active: false }
    : resourceError && !state.history.snapshot
      ? { text: 'Session unavailable', active: false }
      : activityStatus(state, cells, observing, lastTurn, delivery);
  const actions = useSessionActions(),
    navigate = useNavigate();
  const [actionError, setActionError] = useState<unknown>();
  const [confirmation, setConfirmation] = useState<HistoryConfirmation>();
  const [historyPending, setHistoryPending] = useState(false),
    [historyStarted, setHistoryStarted] = useState(false);
  const historyLock = useRef(false);
  const confirmationID = useRef<string | undefined>(undefined);
  const [stored, setStored] = useState<{
    title: string;
    text?: string;
    error?: string;
    body?: TimelineRow['body'];
    gap?: { id: string; sequence: string };
  }>();
  const bodyRequest = useRef<AbortController | null>(null);
  const dropTarget = useRef<HTMLDivElement>(null),
    readingActions = useRef<ReadingListActions>(null);
  const childCompanion = useRef<ChildChatCompanion | undefined>(undefined);
  const [childViewId, setChildViewId] = useState<string>();
  const [childOpenError, setChildOpenError] = useState<{
    agentId: string;
    message: string;
  }>();
  const owner = useRef({});
  useLayoutEffect(() => {
    owner.current = {};
    historyLock.current = false;
    confirmationID.current = undefined;
    setHistoryPending(false);
    childCompanion.current = undefined;
    setChildViewId(undefined);
    setChildOpenError(undefined);
    setActionError(undefined);
    setConfirmation(undefined);
    setHistoryStarted(false);
    setStored(undefined);
    bodyRequest.current?.abort();
    return () => {
      owner.current = {};
      bodyRequest.current?.abort();
    };
  }, [client, session, rootId, viewId]);
  useEffect(() => {
    setStored(undefined);
    bodyRequest.current?.abort();
  }, [kind]);
  useEffect(() => {
    if (!connected) bodyRequest.current?.abort();
  }, [connected]);
  const stillHere = (token: object) =>
    token === owner.current && runtime.connections.isAttached(client);
  const workspace = runtime.tabs.workspace();
  const focused = !viewId || selectedSessionTab(workspace)?.id === viewId;
  const openedChild = workspace.tabs.find(
    (tab) =>
      tab.id === childViewId &&
      isSessionTab(tab) &&
      tab.kind === 'chat' &&
      tab.runtimeId === expectedRuntimeId &&
      tab.rootId === rootId &&
      sessionViewPane(workspace, tab.id)?.selected === tab.id,
  );
  const openAgent = (next: string, openInTab = false) => {
    setChildOpenError(undefined);
    const result = openChildChat(runtime, navigate, viewId ?? rootId, next, {
      companion: childCompanion.current,
      openInTab,
      onUnavailable: (message) => setChildOpenError({ agentId: next, message }),
    });
    if (result) {
      childCompanion.current = result.companion;
      setChildViewId(result.tab.id);
    }
  };
  const setPanel = (next?: InspectorSection) => {
    const search = sessionSearch({
      kind,
      location: {
        ...(session.id !== rootId ? { agent: session.id } : {}),
        panel: next,
      },
    });
    void navigate({
      to: '/h/$runtimeId/s/$rootId',
      params: { runtimeId: expectedRuntimeId, rootId },
      search,
      state: { whipViewId: viewId },
      replace: true,
    }).catch((error) => runtime.reportWorkspace(error));
  };
  const openCreated = async (id: string) => {
    const workspace = runtime.tabs.workspace(),
      pane = viewId ? sessionViewPane(workspace, viewId) : undefined;
    const active = !viewId || selectedSessionTab(workspace)?.id === viewId;
    const tab = runtime.tabs.open(expectedRuntimeId, id, '', pane?.id);
    if (active)
      await navigate({
        to: '/h/$runtimeId/s/$rootId',
        params: { runtimeId: expectedRuntimeId, rootId: id },
        search: {},
        state: { whipViewId: tab },
      });
    else runtime.tabs.activate(tab, false);
  };
  const refresh = async () => {
    await Promise.all([view.refresh(), execution.refresh()]);
  };
  async function prepareHistory(
    action: 'fork' | 'rewind' | 'clear',
    row?: TimelineRow,
    afterSelection = false,
  ) {
    if (
      !connected ||
      !selected ||
      !state.history.snapshot ||
      state.status !== 'live' ||
      historyLock.current
    )
      return;
    historyLock.current = true;
    setHistoryPending(true);
    setActionError(undefined);
    const token = owner.current,
      snapshot = state.history.snapshot;
    try {
      const keep =
        action === 'clear'
          ? '0'
          : await (afterSelection ? historyGroupEnd : historyBoundary)(
              session,
              state.history,
              row!.seq!,
              runtime.connections.signal(client),
            );
      if (!stillHere(token)) return;
      const id = crypto.randomUUID();
      confirmationID.current = id;
      setConfirmation(
        action === 'fork'
          ? {
              action,
              afterSelection,
              command: runtime.command(client, 'sessions.fork', {
                fork_id: id,
                session_id: session.id,
                expected_history_revision: snapshot.revision,
                expected_config_revision: selected.config_revision,
                observed_through: snapshot.through_sequence,
                keep_through: keep,
                title: null,
              }),
            }
          : {
              action,
              afterSelection,
              command: runtime.command(client, 'sessions.rewind', {
                edit_id: id,
                session_id: session.id,
                expected_revision: snapshot.revision,
                observed_through: snapshot.through_sequence,
                keep_through: keep,
              }),
            },
      );
      setHistoryStarted(false);
    } catch (error) {
      if (stillHere(token)) setActionError(error);
    } finally {
      historyLock.current = false;
      if (stillHere(token)) setHistoryPending(false);
    }
  }
  async function applyHistory() {
    if (!confirmation || !connected || historyStarted || historyLock.current ||
      (confirmation.afterSelection && confirmation.action === 'rewind' && activeTurn))
      return;
    if (confirmation.action === 'fork' && !runtime.tabs.canOpen()) {
      setActionError(
        new Error('Close an open tab before creating another session.'),
      );
      return;
    }
    historyLock.current = true;
    setHistoryPending(true);
    setHistoryStarted(true);
    setActionError(undefined);
    const token = owner.current,
      commandID = confirmation.command.id;
    const current = () =>
      stillHere(token) && confirmationID.current === commandID;
    try {
      if (confirmation.action === 'fork') {
        const result = await runtime.run(confirmation.command, 'Fork history');
        if (current() && result.root && result.tree)
          await openCreated(result.fork.root_id);
      } else {
        await runtime.run(
          confirmation.command,
          confirmation.action === 'clear' ? 'Clear history' : 'Rewind history',
        );
        if (current()) await refresh();
      }
      if (current()) setConfirmation(undefined);
    } catch (error) {
      if (current()) setActionError(error);
    } finally {
      if (current()) {
        historyLock.current = false;
        setHistoryPending(false);
      }
    }
  }
  async function readBody(row: TimelineRow) {
    if (!row.body) return;
    bodyRequest.current?.abort();
    const abort = new AbortController();
    bodyRequest.current = abort;
    const current = { title: 'Stored message', body: row.body };
    setStored(current);
    try {
      const bytes = await session.content.readBytes(row.body, {
        maxBytes: 1 << 20,
        signal: abort.signal,
      });
      if (!abort.signal.aborted)
        setStored({
          ...current,
          text: new TextDecoder('utf-8', { fatal: true }).decode(bytes),
        });
    } catch (error) {
      if (!abort.signal.aborted)
        setStored({
          ...current,
          error: error instanceof Error ? error.message : String(error),
        });
    }
  }
  async function loadGap(id: string) {
    const gap = state.history.gaps.find((item) => item.messageID === id);
    if (!gap) throw new Error('Message gap is no longer in this window');
    bodyRequest.current?.abort();
    const abort = new AbortController();
    bodyRequest.current = abort;
    const current = {
      title: 'Large message',
      gap: { id, sequence: gap.sequence },
    };
    setStored(current);
    try {
      const bytes = await readLargeMessage(
        session,
        id,
        gap.sequence,
        1 << 20,
        abort.signal,
      );
      if (!abort.signal.aborted)
        setStored({
          ...current,
          text: new TextDecoder('utf-8', { fatal: true }).decode(bytes),
        });
    } catch (error) {
      if (!abort.signal.aborted)
        setStored({
          ...current,
          error: error instanceof Error ? error.message : String(error),
        });
    }
  }
  if (!identityValid || (selected && root && !scopeValid))
    return (
      <div {...stylex.props(layout.empty)}>
        <h1 {...stylex.props(layout.emptyTitle)}>Session identity changed</h1>
        <p>Reconnect the original host and reopen this session.</p>
        <Link to="/">Choose a session</Link>
      </div>
    );
  const history = state.history;
  return (
    <ChatDropSurface ref={dropTarget}>
      <SessionTopBar
        kind={kind}
        host={host?.name ?? 'Unavailable host'}
        cwd={selected?.working_directory ?? summaryCwd}
        pending={opening || controlsPending}
        agentName={
          session.id === rootId ? 'Root' : selected?.definition.id || session.id
        }
        onAgents={() => setPanel('agents')}
        onRoot={
          session.id !== rootId
            ? () => {
                void navigate({
                  to: '/h/$runtimeId/s/$rootId',
                  params: { runtimeId: expectedRuntimeId, rootId },
                  search: sessionSearch({ kind, location: {} }),
                  state: { whipViewId: viewId },
                }).catch((error) => runtime.reportWorkspace(error));
              }
            : undefined
        }
        onChat={() => {
          void openSessionView(
            runtime,
            navigate,
            viewId ?? rootId,
            'chat',
            true,
          );
        }}
        onRepl={() => {
          void openSessionView(
            runtime,
            navigate,
            viewId ?? rootId,
            'repl',
            true,
          );
        }}
        onTrace={() => {
          void openSessionView(
            runtime,
            navigate,
            viewId ?? rootId,
            'trace',
            true,
          );
        }}
        detailsOpen={!!panel && focused}
        onDetails={() => setPanel(panel ? undefined : 'agents')}
        onPrepare={actions.prepare}
        actions={
          tree
            ? actions.items({
                runtimeId: expectedRuntimeId,
                rootId,
                title: tree.metadata.title ?? '',
                archived: tree.metadata.archived,
              })
            : []
        }
        activity={
          <CurrentActivity
            status={status}
            connected={observing}
            onDetails={() => setPanel('agents')}
          />
        }
      />
      {resourceError && (
        <ErrorNotice
          type="session"
          owner={`${expectedRuntimeId}:${rootId}:${session.id}`}
          error={resourceError}
          title={state.error ? undefined : 'Could not load session details'}
          action={
            <Button
              variant="ghost"
              disabled={!hostConnected}
              onClick={() =>
                void Promise.all([
                  refresh(),
                  selectedQuery.refetch(),
                  rootQuery.refetch(),
                  treeQuery.refetch(),
                ]).catch(setActionError)
              }
            >
              Refresh
            </Button>
          }
        />
      )}
      {!!actionError && !confirmation && (
        <ErrorNotice
          type="action"
          owner={`${session.id}:history`}
          error={actionError}
          onDismiss={() => setActionError(undefined)}
        />
      )}
      {kind === 'trace' ? (
        trace ? (
          <TraceView
            key={`trace:${expectedRuntimeId}:${rootId}:${viewId}`}
            view={trace}
            client={client}
            viewId={viewId ?? rootId}
            connected={connected}
          />
        ) : (
          <SessionLoading />
        )
      ) : kind === 'repl' ? (
        <>
          <ReplView
            session={session}
            view={view}
            execution={execution}
            engine={tree?.engine ?? 'starlark'}
            runtimeId={expectedRuntimeId}
            viewId={viewId ?? rootId}
            connected={connected}
            loadGap={loadGap}
          />
          <AgentTurnNotice
            session={session}
            selected={selected}
            turn={lastTurn}
            activeTurn={activeTurn}
            connected={connected}
          />
        </>
      ) : activityRows.length ||
        activeTurn ||
        history.olderCursor ||
        history.latestMissing ? (
        <Timeline
          readingActionsRef={readingActions}
          active={!!activeTurn}
          activeTurnId={activeTurn}
          key={`timeline:${expectedRuntimeId}:${rootId}:${session.id}`}
          rows={activityRows}
          onAgent={openAgent}
          footer={
            <>
              <TranscriptWorking
                key={activeTurn ?? 'pending'}
                status={status}
                turnId={activeTurn}
                startedAt={active?.started_at}
              />
              {evidence.truncated && (
                <p {...stylex.props(layout.notice)}>
                  Some execution details are outside this bounded window. Open
                  REPL to inspect older work.
                </p>
              )}
              <AgentTurnNotice
                session={session}
                selected={selected}
                turn={lastTurn}
                activeTurn={activeTurn}
                connected={connected}
              />
            </>
          }
          connected={connected}
          density={preferences.toolDensity}
          onOpenRepl={() => {
            void openSessionView(runtime, navigate, viewId ?? rootId, 'repl');
          }}
          bookmarkKey={`${expectedRuntimeId}:${viewId ?? rootId}:${session.id}`}
          historyRevision={history.snapshot?.revision}
          historyThroughSeq={history.snapshot?.through_sequence}
          rewindDisabled={!!activeTurn || historyPending}
          responseHistoryAction={session.id === rootId && connected && state.status === 'live' && !historyPending
            ? (sequence, action) => { void prepareHistory(action, { id: '', role: 'assistant', text: '', seq: sequence }, true); }
            : undefined}
          historyCursor={history.olderCursor ?? undefined}
          historyReady={!!history.snapshot}
          canLoadOlder={connected}
          loadingHistory={state.status === 'loading'}
          hasMore={history.olderCursor !== null}
          loadOlder={() => view.loadOlder()}
          loadGap={loadGap}
          loadLatest={() => view.latest()}
          latestMissing={history.latestMissing}
          readBody={(row) => void readBody(row)}
          messageScope={{ client, rootId, agentId: session.id }}
          historyAction={
            connected && state.status === 'live' && !historyPending
              ? (row, action) => {
                  if (row.seq) void prepareHistory(action, row);
                }
              : undefined
          }
        />
      ) : opening || state.status === 'loading' ? (
        <SessionLoading />
      ) : (
        <div {...stylex.props(layout.empty)}>
          {resourceError && !state.history.snapshot ? (
            <p {...stylex.props(layout.emptyText)}>
              Session content is unavailable. Use Refresh above.
            </p>
          ) : (
            <>
              <h2 {...stylex.props(layout.emptyTitle)}>
                What do you want to work on?
              </h2>
              <p {...stylex.props(layout.emptyText)}>
                Give WHIP a goal, then follow the work and guide it as needed.
              </p>
              <AgentTurnNotice
                session={session}
                selected={selected}
                turn={lastTurn}
                activeTurn={activeTurn}
                connected={connected}
              />
            </>
          )}
        </div>
      )}
      {!!admitted.length && (
        <details {...stylex.props(layout.notice)}>
          <summary>
            {admitted.length} accepted inputs in this queue page
          </summary>
          {admitted.map((item) => (
            <div key={item.id}>
              <Badge>{item.state}</Badge>
              <pre {...stylex.props(layout.pre)}>{admittedText(item)}</pre>
            </div>
          ))}
        </details>
      )}
      {kind === 'chat' && childOpenError && (
        <ErrorNotice
          type="action"
          owner={`${viewId ?? rootId}:open-child`}
          title="Could not open agent chat"
          error={childOpenError.message}
          tone="neutral"
          onDismiss={() => setChildOpenError(undefined)}
          action={
            runtime.tabs.canOpen() && (
              <Button
                variant="ghost"
                onClick={() => openAgent(childOpenError.agentId, true)}
              >
                Open in tab
              </Button>
            )
          }
        />
      )}
      {scopeValid && (
        <PendingRequests
          session={session}
          rootId={rootId}
          disabled={!connected}
          refresh={refresh}
          pendingCount={state.activity?.pending_permission_count}
        />
      )}
      {kind === 'chat' && (
        <Composer
          key={`composer:${expectedRuntimeId}:${rootId}:${session.id}`}
          session={session}
          rootId={rootId}
          onAccepted={() => readingActions.current?.jumpToLatest()}
          dropTarget={dropTarget}
          notice={
            <ScheduledWakeNotice
              session={session}
              connected={connected}
              onSchedules={() => setPanel('goals')}
            />
          }
          agents={
            tree && (
              <AgentDock
                session={session}
                treeId={tree.id}
                connected={connected}
                onAgent={openAgent}
                onAllAgents={() => setPanel('agents')}
                openAgentId={
                  openedChild && isSessionTab(openedChild)
                    ? openedChild.location.agent
                    : undefined
                }
              />
            )
          }
          queueEnabled
          queue={
            <>
              <ErrorNotice
                type="resource"
                owner={`${session.id}:queue`}
                error={queue.error}
                title="Queue updates unavailable"
              />
              {queueCursor && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setQueueCursor(undefined)}
                >
                  Return to first queued messages
                </Button>
              )}
              <ComposerQueue
                rows={queueRows}
                session={session}
                rootId={rootId}
                runtimeId={expectedRuntimeId}
                activeTurn={activeTurn}
                connected={connected && !queue.error}
                hasMore={!!queue.data?.next_cursor}
                refresh={async () => {
                  const result = await queue.refetch();
                  if (result.error) throw result.error;
                }}
                loadMore={async () => {
                  if (queue.data?.next_cursor)
                    setQueueCursor(queue.data.next_cursor);
                }}
              />
            </>
          }
          connected={connected}
          unavailableReason={
            !connected && !opening && !controlsPending
              ? 'Session content or host connection is unavailable.'
              : undefined
          }
          pending={opening || controlsPending}
          activeTurn={activeTurn}
          lastTurn={lastTurn}
          runtimeId={expectedRuntimeId}
          viewId={viewId}
          active={focused && !panel}
          modelControl={
            selected ? (
              <>
                <PermissionModePicker
                  session={session}
                  rootId={rootId}
                  selected={selected}
                  connected={connected}
                />
                <SessionModelPicker
                  client={client}
                  session={session}
                  selected={selected}
                  view={view}
                  connected={connected}
                />
              </>
            ) : (
              (opening || controlsPending) && <PickerSkeletons />
            )
          }
        />
      )}
      <Sheet
        open={!!panel && focused}
        onOpenChange={(open) => {
          if (focused) setPanel(open ? panel || 'agents' : undefined);
        }}
        title="Session details"
        description="Inspect recursive work and control the session."
      >
        <Menu
          trigger={
            <Button variant="ghost" aria-label="Session actions">
              <MoreHorizontal size={16} />
            </Button>
          }
          onOpenChange={actions.prepare}
          items={[
            ...actions.items({
              runtimeId: expectedRuntimeId,
              rootId,
              title: tree?.metadata.title ?? '',
              archived: tree?.metadata.archived,
            }),
            {
              id: 'Compact history',
              label: 'Compact history',
              disabled: !connected,
              onSelect: () => {
                const token = owner.current;
                setActionError(undefined);
                void runtime
                  .run(
                    runtime.command(client, 'sessions.compact', {
                      session_id: session.id,
                      identity: {
                        client_id: client.clientID,
                        request_id: crypto.randomUUID(),
                      },
                    }),
                    'Compact history',
                  )
                  .catch((error) => {
                    if (stillHere(token)) setActionError(error);
                  });
              },
            },
            {
              id: 'Clear history…',
              label: 'Clear history…',
              disabled: !connected || !history.snapshot || historyPending,
              onSelect: () => void prepareHistory('clear'),
            },
          ]}
        />
        {selected && tree && (
          <SessionInspector
            client={client}
            session={session}
            rootId={rootId}
            tree={tree}
            selected={selected}
            view={view}
            execution={execution}
            connected={connected}
            kind={kind}
            viewId={viewId}
            section={panel || 'agents'}
            onSectionChange={setPanel}
          />
        )}
      </Sheet>
      <Dialog
        open={!!confirmation}
        onOpenChange={(open) => {
          if (!open) {
            confirmationID.current = undefined;
            historyLock.current = false;
            setHistoryPending(false);
            setConfirmation(undefined);
            setActionError(undefined);
          }
        }}
        title={
          confirmation?.action === 'clear'
            ? 'Clear conversation history?'
            : confirmation?.afterSelection
              ? confirmation.action === 'fork' ? 'Fork from here?' : 'Rewind to here?'
              : `${confirmation?.action === 'fork' ? 'Fork' : 'Rewind'} before this exchange?`
        }
        description={
          confirmation?.action === 'fork'
            ? confirmation.afterSelection
              ? 'Create a separate session including this response. The current session continues independently.'
              : 'Create a separate session with the earlier complete exchanges. The current session continues independently.'
            : 'Later conversation and its REPL checkpoint will be retired. Files are not restored. The host rejects a changed history revision or tail.'
        }
        footer={
          <Button
            variant={confirmation?.action === 'fork' ? 'primary' : 'danger'}
            disabled={!connected || historyPending || historyStarted || !!(confirmation?.afterSelection && confirmation.action === 'rewind' && activeTurn)}
            loading={historyPending}
            onClick={() => void applyHistory()}
          >
            Confirm {confirmation?.action}
          </Button>
        }
      >
        <ErrorNotice
          type="action"
          owner={`${session.id}:history`}
          error={actionError}
        />
        {historyStarted && !!actionError && (
          <p>
            The original request is retained in Recovery settings. Check it
            there before retrying; a new confirmation creates a new request.
          </p>
        )}
      </Dialog>
      <Dialog
        open={!!stored}
        onOpenChange={(open) => {
          if (!open) {
            bodyRequest.current?.abort();
            setStored(undefined);
          }
        }}
        title={stored?.title || 'Stored message'}
      >
        {stored?.error ? (
          <ErrorNotice
            type="resource"
            owner={`${session.id}:message`}
            error={stored.error}
          />
        ) : stored?.text !== undefined ? (
          <CodeBlock
            code={stored.text}
            label={stored.title}
            maxBytes={128 << 10}
          />
        ) : (
          <p role="status">Loading…</p>
        )}
        {(stored?.body || stored?.gap) && (
          <Button
            variant="ghost"
            disabled={!connected}
            onClick={async () => {
              const current = stored;
              const token = owner.current;
              try {
                const signal = runtime.connections.signal(client);
                const bytes = current.body
                  ? await session.content.readBytes(current.body, {
                      maxBytes: 4 << 20,
                      signal,
                    })
                  : await readLargeMessage(
                      session,
                      current.gap!.id,
                      current.gap!.sequence,
                      4 << 20,
                      signal,
                    );
                if (stillHere(token))
                  await runtime.platform.download(
                    bytes,
                    'whip-message.json',
                    current.body?.media_type ?? 'application/json',
                  );
              } catch (error) {
                if (stillHere(token))
                  setStored((previous) =>
                    previous === current
                      ? {
                          ...previous,
                          error:
                            error instanceof Error
                              ? error.message
                              : String(error),
                        }
                      : previous,
                  );
              }
            }}
          >
            Download stored message
          </Button>
        )}
      </Dialog>
    </ChatDropSurface>
  );
}
