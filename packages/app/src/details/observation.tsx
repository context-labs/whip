import { useEffect, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { useSessionView } from '@whip/sdk/react';
import type { MailboxPageParams } from '@whip/protocol';
import { Badge, Button, CodeBlock, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { layout } from '../styles';
import { executionCode, timelineRows } from '../timeline';
import {
  Action,
  CollectionMore,
  ContentRead,
  Empty,
  Section,
  mergeBy,
  useCollection,
  type InspectorProps,
} from './shared';

export function Agents(props: InspectorProps) {
  const { view, root, connected } = props;
  const runtime = useRuntime();
  const collection = useCollection(view, 'agents');
  const agents = mergeBy(
    root.agents ?? [],
    collection.page?.items?.flatMap((item) => (item.agent ? [item.agent] : [])) ?? [],
    (item) => item.id,
  );
  const names = new Map(agents.map((item) => [item.id, item.name || 'Root agent']));
  const runtimeId = view.session.client.getSnapshot().info?.runtime_id ?? '';
  return (
    <Section
      title="Agent tree"
      description="Inspecting a transcript does not add it to another agent’s model context."
    >
      {agents.map((agent) => (
        <article key={agent.id} {...stylex.props(layout.column, layout.notice)}>
          <div {...stylex.props(layout.row, layout.wrap)}>
            <Link
              to="/h/$runtimeId/s/$rootId"
              params={{ runtimeId, rootId: view.session.rootId }}
              search={{ agent: agent.id === view.session.rootId ? undefined : agent.id }}
            >
              {agent.name || 'Root agent'}
            </Link>
            <Badge>{agent.lifecycle_phase || agent.status}</Badge>
          </div>
          {agent.parent_id && (
            <span {...stylex.props(layout.muted)}>
              From {names.get(agent.parent_id) || agent.parent_id}
            </span>
          )}
          <span>
            {agent.model} · {agent.provider}
            {agent.effort && ` · ${agent.effort}`}
          </span>
          {agent.blocking_reason && <p>Waiting for {agent.blocking_reason}</p>}
          {agent.terminal_cause && <p>Ended: {agent.terminal_cause}</p>}
          <span {...stylex.props(layout.muted)}>{agent.pending_mail} pending messages</span>
          {agent.report && (
            <CodeBlock label="Agent report" code={agent.report} maxBytes={32 << 10} />
          )}
          <div {...stylex.props(layout.row, layout.wrap)}>
            {root.active_turns?.[agent.id] && (
              <Action
                disabled={!connected}
                run={() =>
                  runtime.run(
                    agent.id === root.root_id
                      ? view.session.cancelTurn(root.active_turns![agent.id]!)
                      : view.session.agents.cancelTurn(agent.id, root.active_turns![agent.id]!),
                    'Cancel current turn',
                  )
                }
              >
                Cancel turn
              </Action>
            )}
            {agent.allowed_controls?.includes('agent.stop') && (
              <Action
                disabled={!connected}
                run={() => runtime.run(view.session.agents.control(agent.id), 'Stop agent subtree')}
              >
                Stop subtree
              </Action>
            )}
            {agent.allowed_controls?.includes('agent.delete') && (
              <Action
                disabled={!connected}
                danger
                run={() => runtime.run(view.session.agents.delete(agent.id), 'Delete agent')}
              >
                Delete agent
              </Action>
            )}
          </div>
        </article>
      ))}
      <CollectionMore
        collection={collection}
        omitted={root.omitted?.agents}
        connected={connected}
      />
    </Section>
  );
}

export function MailAndState(props: InspectorProps) {
  const [section, setSection] = useState('mail');
  return (
    <>
      <Select
        label="Shared state section"
        value={section}
        onValueChange={setSection}
        options={[
          { value: 'mail', label: 'Agent mailbox' },
          { value: 'blackboard', label: 'Blackboard' },
        ]}
      />
      {section === 'mail' ? <Mailbox {...props} /> : <Blackboard {...props} />}
    </>
  );
}
export function Mailbox({ view, agentId, connected }: InspectorProps) {
  const [status, setStatus] = useState('all');
  const [selected, setSelected] = useState('');
  const [notice, setNotice] = useState('');
  const cache = useQueryClient();
  const key = [
    'inspector-mail',
    view.session.client.getSnapshot().info?.runtime_id,
    view.session.rootId,
    agentId,
    status,
  ];
  const query = useInfiniteQuery({
    queryKey: key,
    initialPageParam: undefined as MailboxPageParams['cursor'],
    queryFn: ({ pageParam, signal }) =>
      view.session.mailbox.list(
        { agent_id: agentId, status, cursor: pageParam, limit: 32, max_bytes: 128 << 10 },
        { signal },
      ),
    getNextPageParam: (page) => (page.has_more ? page.next_cursor : undefined),
    maxPages: 4,
    gcTime: 0,
    retry: false,
    enabled: connected,
    refetchInterval: connected ? 3000 : false,
  });
  useEffect(() => {
    if (query.error && 'kind' in query.error && query.error.kind === 'resynchronization_required') {
      setNotice('Mailbox changed. Refreshed from the current first page.');
      setSelected('');
      void cache.resetQueries({ queryKey: key, exact: true });
    }
  }, [query.error, cache]);
  const items = query.data?.pages.flatMap((page) => page.items ?? []) ?? [];
  const selectedRevision = items.find((item) => item.id === selected)?.revision;
  const message = useQuery({
    queryKey: [...key, 'body', selected, selectedRevision],
    queryFn: ({ signal }) => view.session.mailbox.read(selected, agentId, { signal }),
    enabled: connected && !!selected && selectedRevision !== undefined,
    gcTime: 0,
    retry: false,
  });
  return (
    <Section
      title="Agent mailbox"
      description="Read-only mail inspection. These messages are separate from the command inbox; viewing them does not deliver, acknowledge, or complete them."
    >
      <Select
        label="Message state"
        value={status}
        onValueChange={(value) => {
          setStatus(value);
          setSelected('');
        }}
        options={['all', 'pending', 'delivered', 'done'].map((value) => ({ value, label: value }))}
      />
      {notice && <p role="status">{notice}</p>}
      {query.isLoading && <Empty>Loading mailbox…</Empty>}
      {query.error && <p role="alert">{query.error.message}</p>}
      {!query.isLoading && !items.length && <Empty>No messages in this state.</Empty>}
      {items.map((item) => (
        <article key={item.id} {...stylex.props(layout.column, layout.notice)}>
          <strong>{item.subject || item.kind}</strong>
          <span {...stylex.props(layout.muted)}>
            {item.sender} → {item.recipient}
          </span>
          <div {...stylex.props(layout.row)}>
            <Badge>{item.status}</Badge>
            <span>Revision {item.revision}</span>
          </div>
          <p>{item.excerpt}</p>
          <Button variant="ghost" disabled={!connected} onClick={() => setSelected(item.id)}>
            Inspect message
          </Button>
          {selected === item.id && (
            <>
              {message.error && <p role="alert">{message.error.message}</p>}
              {message.data && (
                <>
                  <ContentRead
                    key={`${message.data.id}:${message.data.revision}`}
                    view={view}
                    agentId={agentId}
                    value={message.data.body}
                    label="Message body"
                  />
                  {message.data.evidence_handle && (
                    <ContentRead
                      view={view}
                      agentId={agentId}
                      referenceId={message.data.evidence_handle}
                      label="Evidence"
                    />
                  )}
                </>
              )}
            </>
          )}
        </article>
      ))}
      {query.hasNextPage && (
        <Button
          disabled={!connected || query.isFetching}
          loading={query.isFetchingNextPage}
          variant="ghost"
          onClick={() => void query.fetchNextPage()}
        >
          Older messages
        </Button>
      )}
      {(query.data?.pages.length ?? 0) > 1 && (
        <Button
          variant="ghost"
          disabled={!connected}
          onClick={() => void cache.resetQueries({ queryKey: key, exact: true })}
        >
          Return to first page
        </Button>
      )}
    </Section>
  );
}
function Blackboard({ view, root, connected, agentId }: InspectorProps) {
  const collection = useCollection(view, 'blackboard');
  const entries = mergeBy(
    root.blackboard ?? [],
    collection.page?.items?.flatMap((item) => (item.blackboard ? [item.blackboard] : [])) ?? [],
    (item) => item.key,
  );
  return (
    <Section
      title="Blackboard"
      description="Shared state published by agents. Inspection never changes its version or admits it into model context."
    >
      {!entries.length && <Empty>No shared values yet.</Empty>}
      {entries.map((item) => (
        <article
          key={`${item.key}:${item.version}`}
          {...stylex.props(layout.column, layout.notice)}
        >
          <strong>{item.key}</strong>
          <span {...stylex.props(layout.muted)}>
            Version {item.version} · {item.author_agent_id}
          </span>
          <ContentRead view={view} agentId={agentId} value={item.payload} label="Shared value" />
        </article>
      ))}
      <CollectionMore
        collection={collection}
        omitted={root.omitted?.blackboard}
        connected={connected}
      />
    </Section>
  );
}
export function Executions({ view, root, agentId, connected }: InspectorProps) {
  const state = useSessionView(view);
  const runtime = useRuntime();
  const [limit, setLimit] = useState(16);
  const history = state.history[agentId];
  const rows = timelineRows(
    history,
    agentId === root.root_id ? root.presentation : root.agent_presentations?.[agentId],
  ).filter((row) => row.role === 'tool');
  return (
    <Section
      title="Executions"
      description="Code, tool calls, and their results from the retained transcript and active stream. Raw VM globals are not exposed by this protocol."
    >
      {history?.truncated && (
        <p role="status">
          Some history bodies are content references or were evicted. Open them explicitly to
          inspect the complete record.
        </p>
      )}
      {!rows.length && <Empty>No tool executions in the loaded history.</Empty>}
      {rows.length > limit && (
        <Button
          variant="ghost"
          onClick={() => setLimit((value) => Math.min(value + 16, rows.length))}
        >
          Show earlier loaded executions
        </Button>
      )}
      {rows.slice(-limit).map((row) => (
        <article key={row.id} {...stylex.props(layout.column, layout.notice)}>
          <div {...stylex.props(layout.row)}>
            <strong>{row.label || 'Tool execution'}</strong>
            <Badge>{row.live ? 'Streaming' : 'Recorded'}</Badge>
          </div>
          {row.args && (
            <CodeBlock
              code={executionCode(row.args)}
              language={row.label === 'Starlark execution' ? 'python' : 'json'}
              label={row.label === 'Starlark execution' ? 'Starlark program' : 'Call arguments'}
              maxBytes={32 << 10}
            />
          )}
          {row.text && <CodeBlock code={row.text} label="Output" maxBytes={32 << 10} />}
          {row.body && (
            <ContentRead view={view} value={row.body} agentId={agentId} label="Execution record" />
          )}
        </article>
      ))}
      {history?.hasMore && (
        <Button
          variant="ghost"
          disabled={!connected || history.loading}
          onClick={() => void view.loadOlder(agentId).catch((error) => runtime.report(error))}
        >
          Load older transcript page
        </Button>
      )}
      {!history && (
        <Button
          disabled={!connected}
          onClick={() => void view.openAgent(agentId).catch((error) => runtime.report(error))}
        >
          Load agent history
        </Button>
      )}
    </Section>
  );
}
