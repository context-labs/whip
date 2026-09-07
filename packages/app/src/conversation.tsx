import { useEffect, useMemo, useRef, useState } from 'react';
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
import { GitBranch, MoreHorizontal, PanelRight, Square } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from './context';
import { layout } from './styles';
import {
  Timeline,
  timelineRows,
  messagePresentation,
  ImageAttachment,
  type TimelineRow,
} from './timeline';
import { Composer } from './composer';
import { PendingRequests } from './requests';
import type { InspectorSection } from './navigation';
import { SessionInspector } from './inspector';

export function ConversationRoute({
  rootId,
  runtimeId,
  agentId,
  panel,
}: {
  rootId: string;
  runtimeId: string;
  agentId?: string;
  panel?: InspectorSection;
}) {
  const { client } = useAppState();
  if (!client)
    return (
      <div {...stylex.props(layout.empty)}>
        Connect to the session’s execution host.
      </div>
    );
  return (
    <AttachedConversation
      client={client}
      rootId={rootId}
      runtimeId={runtimeId}
      agentId={agentId}
      panel={panel}
    />
  );
}
function AttachedConversation({
  client,
  rootId,
  runtimeId,
  agentId,
  panel,
}: {
  client: WhipClient;
  rootId: string;
  runtimeId: string;
  agentId?: string;
  panel?: InspectorSection;
}) {
  const runtime = useRuntime();
  const connection = useWhipConnection(client);
  const matched = connection.info?.runtime_id === runtimeId;
  const [view, setView] = useState<SessionView>();
  useEffect(() => {
    if (!matched) return;
    const lease = runtime.acquireView(rootId);
    setView(lease.view);
    try {
      runtime.rememberSession(runtimeId, rootId);
    } catch (error) {
      runtime.report(error);
    }
    return lease.release;
  }, [runtime, client, rootId, matched, runtimeId]);
  if (connection.info && !matched)
    return (
      <div {...stylex.props(layout.empty)}>
        <h1 {...stylex.props(layout.emptyTitle)}>
          This session belongs to another host
        </h1>
        <p {...stylex.props(layout.emptyText)}>
          Reconnect to its original runtime to continue. No session requests
          were sent to this host.
        </p>
        <Link to="/">Choose a session</Link>
      </div>
    );
  if (!view || view.session.rootId !== rootId || view.session.client !== client)
    return <div {...stylex.props(layout.empty)}>Opening session…</div>;
  return (
    <Conversation
      key={`${runtimeId}:${rootId}`}
      view={view}
      expectedRuntimeId={runtimeId}
      agentId={agentId || rootId}
      panel={panel}
    />
  );
}

function Conversation({
  view,
  expectedRuntimeId,
  agentId,
  panel,
}: {
  view: SessionView;
  expectedRuntimeId: string;
  agentId: string;
  panel?: InspectorSection;
}) {
  const runtime = useRuntime();
  const state = useSessionView(view);
  const session = view.session;
  const connection = useWhipConnection(session.client);
  const root = state.root;
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
  }, [agentId]);
  const navigate = useNavigate();
  const setPanel = (next?: InspectorSection) => void navigate({ to: '/h/$runtimeId/s/$rootId', params: { runtimeId: expectedRuntimeId, rootId: session.rootId }, search: previous => ({ ...previous, panel: next }), replace: true });
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
  const rows = useMemo(
    () => timelineRows(history, presentation),
    [history, presentation],
  );
  const admitted =
    root?.inbox?.filter((item) => item.agent_id === agentId) ?? [];
  const agent = root?.agents?.find((item) => item.id === agentId);
  const activeTurn = root?.active_turns[agentId];
  useEffect(() => {
    if (agentId === session.rootId || wrongRuntime) return;
    void view.openAgent(agentId).catch((error) => runtime.report(error));
    return () => view.closeAgent(agentId);
  }, [view, agentId, session.rootId, runtime, wrongRuntime]);
  async function fork() {
    if (!root) return;
    try {
      const outcome = await runtime.run(
        session.fork({ expected_revision: root.history_revision }),
        'Fork session',
      );
      const id = outcome.result?.root_id;
      if (id)
        await navigate({
          to: '/h/$runtimeId/s/$rootId',
          params: { runtimeId: expectedRuntimeId, rootId: id },
          search: {},
        });
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
      <header {...stylex.props(layout.header)}>
        <div {...stylex.props(layout.column, layout.grow)}>
          <h1 {...stylex.props(layout.title)}>
            {root?.meta.title || 'Untitled session'}
          </h1>
          <span {...stylex.props(layout.muted, layout.ellipsis)}>
            {root?.meta.cwd}
          </span>
        </div>
        <Badge tone={connected ? 'neutral' : 'warning'}>{state.status}</Badge>
        <Button variant="ghost" onClick={() => setPanel('agents')}>
          <PanelRight size={15} /> Details
        </Button>
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
      </header>
      {agentId !== session.rootId && (
        <div {...stylex.props(layout.notice, layout.row)}>
          <GitBranch size={14} />
          <strong>{agent?.name || agentId}</strong>
          <Badge>{agent?.status || 'Loading'}</Badge>
          <span {...stylex.props(layout.grow)} />
          <Link
            to="/h/$runtimeId/s/$rootId"
            params={{ runtimeId: expectedRuntimeId, rootId: session.rootId }}
            search={{}}
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
      {(state.truncated || state.unavailable || history?.truncated) && (
        <p {...stylex.props(layout.notice)}>
          Some output is truncated or unavailable. Older messages and stored
          bodies can be loaded explicitly.
        </p>
      )}
      {rows.length ? (
        <Timeline
          key={`timeline:${expectedRuntimeId}:${session.rootId}:${agentId}`}
          rows={rows}
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
      {activeTurn && (
        <div {...stylex.props(layout.row, layout.notice)}>
          <Badge tone="info">{agent?.lifecycle_phase || 'Working'}</Badge>
          <span {...stylex.props(layout.grow)} />
          <Button
            size="sm"
            variant="ghost"
            disabled={!connected}
            onClick={() =>
              void runtime
                .run(
                  agentId === session.rootId
                    ? session.cancelTurn(activeTurn)
                    : session.agents.cancelTurn(agentId, activeTurn),
                  'Stop turn',
                )
                .catch(() => {})
            }
          >
            <Square size={12} /> Stop this turn
          </Button>
        </div>
      )}
      <Composer
        key={`composer:${expectedRuntimeId}:${session.rootId}:${agentId}`}
        session={session}
        agentId={agentId}
        connected={connection.state === 'connected' && !wrongRuntime && !!root}
        activeTurn={activeTurn}
        runtimeId={expectedRuntimeId}
      />
      <Sheet
        open={!!panel}
        onOpenChange={open => setPanel(open ? panel || 'agents' : undefined)}
        title="Session details"
        description="Inspect recursive work and control the session."
      >
        {root && (
          <SessionInspector
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
              try {
                const action = confirm;
                await runtime.run(
                  action === 'delete'
                    ? session.delete()
                    : session.history.clear(clearRevision.current),
                  action === 'delete' ? 'Delete session' : 'Clear history',
                );
                setConfirm(undefined);
                if (action === 'delete') await navigate({ to: '/' });
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
                  if (outcome.result)
                    await navigate({
                      to: '/h/$runtimeId/s/$rootId',
                      params: {
                        runtimeId: expectedRuntimeId,
                        rootId: outcome.result.root_id,
                      },
                      search: {},
                    });
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

export function admittedText(
  kind: string,
  payload: {
    text?: string | null;
    binary?: string | null;
    inline?: unknown;
    reference_id: string;
  },
): string {
  let text = payload.text ?? '';
  if (payload.binary) {
    try {
      text = new TextDecoder('utf-8', { fatal: true }).decode(
        Uint8Array.from(atob(payload.binary), (char) => char.charCodeAt(0)),
      );
    } catch {
      return 'Input preview is unavailable.';
    }
  }
  if (kind.endsWith('.parts')) {
    try {
      const input = JSON.parse(text);
      return `${typeof input.text === 'string' ? input.text : ''}${input.attachments?.length ? `\n${input.attachments.length} attached files` : ''}`;
    } catch {
      return 'Structured input preview is unavailable.';
    }
  }
  return (
    text ||
    (payload.reference_id
      ? 'Large input stored on the host.'
      : payload.inline
        ? JSON.stringify(payload.inline)
        : 'Input accepted.')
  );
}
