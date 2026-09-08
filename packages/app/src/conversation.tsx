import { useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import type { WhipClient } from '@whip/sdk';
import { useSessionView, useWhipConnection } from '@whip/sdk/react';
import type { SessionView } from '@whip/sdk/state';
import {
  Badge,
  Button,
  CodeBlock,
  Dialog,
  Field,
  Input,
  Menu,
  Sheet,
} from '@whip/ui';
import { GitBranch, MoreHorizontal } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { layout } from './styles';
import {
  Timeline,
  conversationRows,
  messagePresentation,
  ImageAttachment,
  type TimelineRow,
} from './timeline';
import { Composer } from './composer';
import { ReplView } from './repl-view';
import { SessionModelPicker } from './model-selection';
import { PermissionModePicker } from './permission-mode';
import { admittedText, isChatInput } from './input-presentation';
import { PendingRequests } from './requests';
import type { InspectorSection } from './navigation';
import { SessionInspector } from './inspector';
import { selectedSessionTab, sessionViewPane, sessionSearch, type SessionTab } from './session-tabs';

/** Route admission/status only. Workspace reconciliation is the sole root lease owner. */
export function ConversationRoute({ rootId, runtimeId }: { rootId: string; runtimeId: string }) {
  const runtime = useRuntime();
  useSessionTabs();
  const { client } = useAppState();
  if (!runtime.tabs.canOpen(runtimeId, rootId)) return <div {...stylex.props(layout.empty)}><h1 {...stylex.props(layout.emptyTitle)}>Your session tabs are full</h1><p>Close an open tab to view this session. Its work stays on the host.</p></div>;
  if (!client) return <div {...stylex.props(layout.empty)}>Connect to the session’s execution host.</div>;
  return <ConversationHostStatus client={client} runtimeId={runtimeId} />;
}
function ConversationHostStatus({ client, runtimeId }: { client: WhipClient; runtimeId: string }) {
  const connection = useWhipConnection(client);
  if (connection.info && connection.info.runtime_id !== runtimeId) return (
    <div {...stylex.props(layout.empty)}>
      <h1 {...stylex.props(layout.emptyTitle)}>This session belongs to another host</h1>
      <p {...stylex.props(layout.emptyText)}>Reconnect to its original runtime to continue. No session requests were sent to this host.</p>
      <Link to="/">Choose a session</Link>
    </div>
  );
  return <div {...stylex.props(layout.empty)}>Opening session…</div>;
}

/** Shared session state, controls and inspectors for chat and REPL renderers. */
export function SessionContent({
  kind,
  view,
  expectedRuntimeId,
  agentId,
  panel,
  viewId,
}: {
  kind: SessionTab['kind'];
  view: SessionView;
  expectedRuntimeId: string;
  agentId: string;
  panel?: InspectorSection;
  viewId?: string;
}) {
  const runtime = useRuntime();
  useSessionTabs();
  const state = useSessionView(view);
  const { commands } = useAppState();
  const submitted = useSyncExternalStore(runtime.submittedInputs.subscribe, runtime.submittedInputs.getSnapshot);
  const session = view.session;
  const connection = useWhipConnection(session.client);
  const root = state.root;
  useEffect(() => {
    if (root?.meta.title) runtime.tabs.titles(expectedRuntimeId, new Map([[session.rootId, root.meta.title]]));
  }, [runtime, expectedRuntimeId, session.rootId, root?.meta.title]);
  const [rename, setRename] = useState(false);
  const [title, setTitle] = useState('');
  const [confirm, setConfirm] = useState<'delete' | 'clear'>();
  const clearRevision = useRef<string | undefined>(undefined);
  const [historyAction, setHistoryAction] = useState<{
    action: 'fork' | 'rewind';
    cut: number;
    revision: string;
  }>();
  const [stored, setStored] = useState<{
    title: string;
    text?: string;
    error?: string;
    body?: TimelineRow['body'];
    agentId: string;
  }>();
  const bodyRequest = useRef<AbortController | null>(null);
  useEffect(() => {
    setStored(undefined);
    return () => bodyRequest.current?.abort();
  }, [agentId, kind]);
  const navigate = useNavigate();
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const canCreateTab = () => {
    if (runtime.tabs.workspace(expectedRuntimeId).tabs.length < 32) return true;
    runtime.report('There are 32 open session tabs. Close a tab before creating another.');
    return false;
  };
  const navigationRevision = useRef(0);
  useLayoutEffect(() => { navigationRevision.current++; }, [agentId, panel, session, kind]);
  const stillHere = (revision: number) => mounted.current && navigationRevision.current === revision && runtime.getSnapshot().client === session.client;
  const focused = !viewId || selectedSessionTab(runtime.tabs.workspace(expectedRuntimeId))?.id === viewId;
  const setPanel = (next?: InspectorSection) => {
    const search = sessionSearch({ kind, location: { ...(agentId !== session.rootId ? { agent: agentId } : {}), panel: next } });
    void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: expectedRuntimeId, rootId: session.rootId }, search, state: { whipViewId: viewId }, replace: true });
  };
  const openCreated = async (rootId: string) => {
    const workspace = runtime.tabs.workspace(expectedRuntimeId);
    const pane = viewId ? sessionViewPane(workspace, viewId) : undefined;
    const active = !viewId || selectedSessionTab(workspace)?.id === viewId;
    const id = runtime.tabs.open(expectedRuntimeId, rootId, '', pane?.id);
    if (active) await navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: expectedRuntimeId, rootId }, search: {}, state: { whipViewId: id } });
    else runtime.tabs.activate(expectedRuntimeId, id, false);
  };
  const currentRuntime = connection.info?.runtime_id;
  const wrongRuntime = !!currentRuntime && currentRuntime !== expectedRuntimeId;
  const connected =
    connection.state === 'connected' &&
    !wrongRuntime &&
    state.status === 'live';
  const history = state.history[agentId];
  const presentation =
    agentId === session.rootId
      ? root?.presentation
      : root?.agent_presentations[agentId];
  const rows = useMemo(() => kind === 'chat' ? conversationRows(
    history, presentation,
    root?.inbox?.filter(item => item.agent_id === agentId) ?? [],
    submitted.filter(item => item.runtimeId === expectedRuntimeId && item.rootId === session.rootId && item.agentId === agentId),
    new Map(commands.filter(item => item.delivery).map(item => [item.id, item.delivery === 'absent' ? 'Not received · retry from the composer' : 'Checking delivery…'])),
  ) : [], [kind, history, presentation, root?.inbox, submitted, commands, expectedRuntimeId, session.rootId, agentId]);
  const admitted = root?.inbox?.filter(item => item.agent_id === agentId && !isChatInput(item)) ?? [];
  const pendingInputs = submitted.filter(item => item.runtimeId === expectedRuntimeId && item.rootId === session.rootId && item.accepted && !item.confirmed);
  const pendingInputIds = pendingInputs.map(item => item.id).join(',');
  useEffect(() => {
    // Seeing the exact inbox identity is sufficient, including after recovery.
    runtime.submittedInputs.confirm(submitted.filter(input => input.runtimeId === expectedRuntimeId && input.rootId === session.rootId && root?.inbox?.some(item => item.agent_id === input.agentId && item.seq === input.inboxSeq)).map(input => input.id));
  }, [runtime, submitted, expectedRuntimeId, session.rootId, root?.inbox]);
  useEffect(() => {
    if (!pendingInputIds || connection.state !== 'connected' || wrongRuntime) return;
    let current = true;
    // Let the SDK's coalesced lifecycle refresh populate the inbox first.
    // Only very fast turns (never observed in the inbox) need this fallback.
    const timer = setTimeout(() => {
      void (async () => {
        // A refresh can join an older snapshot. The second follows acceptance.
        await view.refresh();
        if (!current) return;
        await view.refresh();
        if (current && view.getSnapshot().status === 'live') runtime.submittedInputs.confirm(pendingInputIds.split(','));
      })().catch(error => runtime.report(error));
    }, 250);
    return () => { current = false; clearTimeout(timer); };
  }, [runtime, view, pendingInputIds, connection.state, wrongRuntime]);
  const agent = root?.agents?.find((item) => item.id === agentId);
  const activeTurn = root?.active_turns[agentId];
  useEffect(() => {
    if (agentId === session.rootId || wrongRuntime) return;
    // The recipient history exposes failures without leaking into another tab.
    return runtime.acquireAgent(view, agentId);
  }, [view, agentId, session.rootId, runtime, wrongRuntime]);
  async function fork() {
    if (!root || !canCreateTab()) return;
    const location = navigationRevision.current;
    try {
      const outcome = await runtime.run(
        session.fork({ expected_revision: root.history_revision }),
        'Fork session',
      );
      const id = outcome.result?.root_id;
      if (id && stillHere(location))
        await openCreated(id);
    } catch {
      /* The command notice retains errors and uncertain delivery. */
    }
  }
  async function readBody(row: TimelineRow) {
    if (!row.body) return;
    bodyRequest.current?.abort();
    bodyRequest.current = new AbortController();
    const signal = bodyRequest.current.signal;
    setStored({ title: 'Stored message', body: row.body, agentId });
    try {
      const text = await session.client
        .content(row.body, { rootId: session.rootId, agentId })
        .readText({ maxBytes: 1 << 20, signal });
      if (!signal.aborted)
        setStored({ title: 'Stored message', text, body: row.body, agentId });
    } catch (error) {
      if (!signal.aborted)
        setStored({
          title: 'Stored message',
          body: row.body,
          agentId,
          error: error instanceof Error ? error.message : String(error),
        });
    }
  }
  if (wrongRuntime)
    return (
      <div {...stylex.props(layout.empty)}>
        <h1 {...stylex.props(layout.emptyTitle)}>
          This session belongs to another host
        </h1>
        <p {...stylex.props(layout.emptyText)}>
          Reconnect to its original runtime to continue. No work has been sent
          to this host.
        </p>
        <Link to="/">Choose a session</Link>
      </div>
    );
  return (
    <>

      {kind === 'chat' && agentId !== session.rootId && (
        <div {...stylex.props(layout.notice, layout.row)}>
          <GitBranch size={14} />
          <strong>{agent?.name || agentId}</strong>
          <Badge>{agent?.status || 'Loading'}</Badge>
          <span {...stylex.props(layout.grow)} />
          <Link
            to="/h/$runtimeId/s/$rootId"
            params={{ runtimeId: expectedRuntimeId, rootId: session.rootId }}
            search={{}}
            state={{ whipViewId: viewId }}
          >
            Root conversation
          </Link>
        </div>
      )}
      {(state.error || history?.error) && (
        <p role="alert" {...stylex.props(layout.notice)}>
          {state.error?.message || history?.error?.message}
          <Button
            variant="ghost"
            onClick={() =>
              void view.refresh().catch((error) => runtime.report(error))
            }
          >
            Refresh
          </Button>
        </p>
      )}
      {kind === 'repl' ? <ReplView key={`repl:${expectedRuntimeId}:${session.rootId}:${agentId}`} view={view} state={state} agentId={agentId} runtimeId={expectedRuntimeId} viewId={viewId ?? session.rootId} connected={connected}
        onAgentChange={next => {
          const search = sessionSearch({ kind, location: { agent: next === session.rootId ? undefined : next, panel } });
          void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: expectedRuntimeId, rootId: session.rootId }, search, state: { whipViewId: viewId }, replace: true }).catch(error => runtime.report(error));
        }} /> : rows.length ? (
        <Timeline
          key={`timeline:${expectedRuntimeId}:${session.rootId}:${agentId}`}
          rows={rows}
          bookmarkKey={`${expectedRuntimeId}:${viewId ?? session.rootId}:${agentId}`}
          historyRevision={history?.revision}
          historyReady={!!history && !history.loading}
          hasMore={history?.hasMore ?? false}
          loadOlder={() => view.loadOlder(agentId)}
          readBody={(row) => void readBody(row)}
          historyAction={
            agentId === session.rootId && connected && root
              ? (row, action) => {
                  if (row.seq !== undefined)
                    setHistoryAction({
                      action,
                      cut: row.seq,
                      revision: root.history_revision,
                    });
                }
              : undefined
          }
        />
      ) : (
        <div {...stylex.props(layout.empty)}>
          <h2 {...stylex.props(layout.emptyTitle)}>
            {state.status === 'loading'
              ? 'Loading conversation…'
              : 'What would you like to work on?'}
          </h2>
          <p {...stylex.props(layout.emptyText)}>
            Give WHIP a goal, then follow the work and guide it as needed.
          </p>
        </div>
      )}
      {!!admitted.length && (
        <details {...stylex.props(layout.notice)}>
          <summary>
            {admitted.length} accepted{' '}
            {admitted.length === 1 ? 'input' : 'inputs'} · queued or running
          </summary>
          {admitted.map((item) => (
            <div key={`${item.agent_id}:${item.seq}`}>
              <Badge>{item.status}</Badge>
              <pre {...stylex.props(layout.pre)}>
                {admittedText(item.kind, item.payload)}
              </pre>
            </div>
          ))}
        </details>
      )}
      {root && (
        <PendingRequests
          root={root}
          session={session}
          disabled={!connected}
          refresh={() => view.refresh()}
        />
      )}
      {kind === 'chat' && <Composer
        key={`composer:${expectedRuntimeId}:${session.rootId}:${agentId}`}
        session={session}
        agentId={agentId}
        connected={connection.state === 'connected' && !wrongRuntime && !!root}
        activeTurn={activeTurn}
        runtimeId={expectedRuntimeId}
        viewId={viewId}
        modelControl={root && <>
          <PermissionModePicker view={view} root={root} connected={connected} agentId={agentId} />
          <SessionModelPicker view={view} root={root} connected={connected} agentId={agentId} />
        </>}
      />}
      <Sheet
        open={!!panel && focused}
        onOpenChange={open => { if (focused) setPanel(open ? panel || 'agents' : undefined); }}
        title="Session details"
        description="Inspect recursive work and control the session."
      >
        <Menu
          trigger={
            <Button variant="ghost" aria-label="Session actions">
              <MoreHorizontal size={16} />
            </Button>
          }
          items={[
            {
              id: 'Rename',
              label: 'Rename',
              onSelect: () => {
                setTitle(root?.meta.title || '');
                setRename(true);
              },
              disabled: !connected,
            },
            {
              id: 'Fork session',
              label: 'Fork session',
              onSelect: () => void fork(),
              disabled: !connected,
            },
            {
              id: 'Compact history',
              label: 'Compact history',
              onSelect: () => {
                void runtime
                  .run(session.history.compact(), 'Compact history')
                  .catch(() => {});
              },
              disabled: !connected,
            },
            {
              id: 'Clear history…',
              label: 'Clear history…',
              onSelect: () => {
                clearRevision.current = root?.history_revision;
                setConfirm('clear');
              },
              disabled: !connected,
            },
            {
              id: 'Delete session…',
              label: 'Delete session…',
              onSelect: () => setConfirm('delete'),
              disabled: !connected,
            },
          ]}
        />
        {root && (
          <SessionInspector
            kind={kind}
            viewId={viewId}
            section={panel || 'agents'}
            onSectionChange={setPanel}
            view={view}
            root={root}
            connected={connected}
            agentId={agentId}
          />
        )}
      </Sheet>
      <Dialog
        open={rename}
        onOpenChange={setRename}
        title="Rename session"
        footer={
          <Button
            disabled={!title.trim() || !connected}
            onClick={() =>
              void runtime
                .run(session.rename(title.trim()), 'Rename session')
                .then(() => setRename(false))
                .catch(() => {})
            }
          >
            Save
          </Button>
        }
      >
        <Field label="Session name">
          <Input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            autoFocus
          />
        </Field>
      </Dialog>
      <Dialog
        open={!!confirm}
        onOpenChange={(open) => {
          if (!open) setConfirm(undefined);
        }}
        title={
          confirm === 'delete'
            ? 'Delete this session?'
            : 'Clear conversation history?'
        }
        description={
          confirm === 'delete'
            ? 'The daemon will delete this session and its owned work. This cannot be undone.'
            : 'This removes the session’s conversation history. Fork it first if you want to keep a copy.'
        }
        footer={
          <Button
            variant="danger"
            disabled={!connected}
            onClick={async () => {
              const location = navigationRevision.current;
              try {
                const action = confirm;
                await runtime.run(
                  action === 'delete'
                    ? session.delete()
                    : session.history.clear(clearRevision.current),
                  action === 'delete' ? 'Delete session' : 'Clear history',
                );
                setConfirm(undefined);
                if (action === 'delete' && runtime.getSnapshot().client === session.client) {
                  const active = selectedSessionTab(runtime.tabs.workspace(expectedRuntimeId));
                  runtime.tabs.close(expectedRuntimeId, [session.rootId], active?.rootId);
                  if (active?.rootId === session.rootId) {
                    const next = selectedSessionTab(runtime.tabs.workspace(expectedRuntimeId));
                    if (next) await navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: expectedRuntimeId, rootId: next.rootId }, search: sessionSearch(next), state: { whipViewId: next.id }, replace: true });
                    else await navigate({ to: '/', replace: true });
                  }
                }
              } catch {}
            }}
          >
            Confirm {confirm}
          </Button>
        }
      />
      <Dialog
        open={!!historyAction}
        onOpenChange={(open) => {
          if (!open) setHistoryAction(undefined);
        }}
        title={
          historyAction?.action === 'rewind'
            ? 'Rewind before this message?'
            : 'Fork before this message?'
        }
        description={
          historyAction?.action === 'rewind'
            ? 'Later conversation will be removed and tracked file changes may be restored. The daemon rejects this action if history has changed since you selected it.'
            : 'Create a separate session with the earlier conversation. The current session continues independently.'
        }
        footer={
          <Button
            variant={historyAction?.action === 'rewind' ? 'danger' : 'primary'}
            disabled={!connected || !historyAction}
            onClick={async () => {
              if (!historyAction) return;
              if (historyAction.action === 'fork' && !canCreateTab()) return;
              const location = navigationRevision.current;
              try {
                if (historyAction.action === 'rewind')
                  await runtime.run(
                    session.history.rewind(
                      historyAction.cut,
                      historyAction.revision,
                    ),
                    'Rewind history',
                  );
                else {
                  const outcome = await runtime.run(
                    session.fork({
                      cut: historyAction.cut,
                      expected_revision: historyAction.revision,
                    }),
                    'Fork history',
                  );
                  if (outcome.result && stillHere(location))
                    await openCreated(outcome.result.root_id);
                }
                setHistoryAction(undefined);
              } catch {}
            }}
          >
            Confirm {historyAction?.action}
          </Button>
        }
      />
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
          <p role="alert">{stored.error}</p>
        ) : stored?.text !== undefined ? (
          <StoredMessage text={stored.text} />
        ) : (
          <p role="status">Loading…</p>
        )}
        {stored?.body && (
          <Button
            variant="ghost"
            onClick={async () => {
              const current = stored;
              try {
                runtime.platform.download(
                  await session.client
                    .content(current.body!, {
                      rootId: session.rootId,
                      agentId: current.agentId,
                    })
                    .readBytes({ maxBytes: 64 << 20 }),
                  'whip-message.json',
                  current.body!.media_type,
                );
              } catch (error) {
                runtime.report(error);
              }
            }}
          >
            Download stored message
          </Button>
        )}
      </Dialog>
    </>
  );
}

function StoredMessage({ text }: { text: string }) {
  let content: unknown = text;
  try {
    const value: unknown = JSON.parse(text);
    if (value && typeof value === 'object' && 'content' in value)
      content = value.content;
  } catch {
    /* Some references contain naturally textual output. */
  }
  const parts = messagePresentation(content);
  return (
    <div {...stylex.props(layout.column)}>
      <CodeBlock
        code={parts.text}
        label="Stored message"
        maxBytes={128 << 10}
      />
      {parts.images.map((image, index) => (
        <ImageAttachment key={index} image={image} />
      ))}
    </div>
  );
}
