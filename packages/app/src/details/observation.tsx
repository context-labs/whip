import { useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useExecutionView, useSessionView } from '@whip/sdk/react';
import { cellExecutionRows } from '@whip/sdk/state';
import { Badge, Button, CodeBlock, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { sessionSearch } from '../session-tabs';
import { layout } from '../styles';
import { ExecutionCellCard } from '../repl-view';
import { ErrorNotice } from '../error-feedback';
import { StateRead } from './state-read';
import { TurnUsage } from './usage';
import {
  Action,
  ContentRead,
  Empty,
  PageControls,
  QueryFeedback,
  Section,
  useDetailQuery,
  type InspectorProps,
} from './shared';

export function Agents(props: InspectorProps) {
  const [after, setAfter] = useState<string>();
  const query = useDetailQuery(
    props,
    'sessions.list',
    { tree_id: props.tree.id, limit: 16, ...(after === undefined ? {} : { after }) },
    true,
  );
  const agents = query.data?.items ?? [];
  return (
    <Section
      title="Agent tree"
      description="Inspecting a transcript does not add it to another agent’s model context. Each page retains up to 16 agents."
    >
      <QueryFeedback query={query} connected={props.connected} />
      {!agents.length && query.data && <Empty>No agents in this page.</Empty>}
      {agents.map((agent) => (
        <article key={agent.id} {...stylex.props(layout.column, layout.notice)}>
          <div {...stylex.props(layout.row, layout.wrap)}>
            <Link
              state={{ whipViewId: props.viewId }}
              to="/h/$runtimeId/s/$rootId"
              params={{ runtimeId: props.client.runtimeID, rootId: props.rootId }}
              search={sessionSearch({
                kind: props.kind ?? 'chat',
                location: { agent: agent.id === props.rootId ? undefined : agent.id },
              })}
            >
              {agent.id === props.rootId ? 'Root agent' : agent.definition.id}
            </Link>
            <Badge>{agent.lifecycle}</Badge>
          </div>
          <code>{agent.id}</code>
          {agent.parent_id && <span {...stylex.props(layout.muted)}>From {agent.parent_id}</span>}
          <span>
            {agent.configuration.model.provider} · {agent.configuration.model.name}
            {agent.configuration.model.effort && ` · ${agent.configuration.model.effort}`}
          </span>
          <AgentActivity {...props} agentID={agent.id} />
          <div {...stylex.props(layout.row, layout.wrap)}>
            <Action
              disabled={!props.connected || agent.lifecycle !== 'active'}
              run={async () => {
                await props.client.sessions.handle(agent.id).lifecycle('stopped');
                await query.refetch();
              }}
            >
              Stop subtree
            </Action>
            <Action
              disabled={!props.connected || agent.lifecycle !== 'stopped'}
              run={async () => {
                await props.client.sessions.handle(agent.id).lifecycle('active');
                await query.refetch();
              }}
            >
              Resume agent
            </Action>
            <Action
              disabled={!props.connected}
              danger
              run={async () => {
                await props.client.sessions.handle(agent.id).delete();
                await query.refetch();
              }}
            >
              Delete agent
            </Action>
          </div>
        </article>
      ))}
      <PageControls
        after={after}
        next={agents.at(-1)?.id}
        busy={query.isFetching}
        connected={props.connected}
        onChange={setAfter}
      />
    </Section>
  );
}
function AgentActivity(props: InspectorProps & { agentID: string }) {
  const state = useSessionView(props.view);
  if (props.agentID !== props.session.id || !state.activity) return null;
  const activity = state.activity,
    turn = activity.active_turn;
  return (
    <>
      <span>
        {activity.queued_input_count} queued inputs · {activity.pending_permission_count} pending
        permissions
      </span>
      {turn && (
        <>
          <Badge>{turn.state}</Badge>
          <Action disabled={!props.connected} run={() => props.session.cancelTurn(turn.id)}>
            Cancel current turn
          </Action>
        </>
      )}
    </>
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
export function Mailbox(props: InspectorProps) {
  const [state, setState] = useState<'all' | 'pending' | 'delivered' | 'done'>('all'),
    [after, setAfter] = useState<string>(),
    [selected, setSelected] = useState('');
  const query = useDetailQuery(
    props,
    'mail.list',
    {
      session_id: props.session.id,
      limit: 32,
      ...(state === 'all' ? {} : { state }),
      ...(after === undefined ? {} : { after }),
    },
    true,
  );
  const items = query.data?.items ?? [];
  return (
    <Section
      title="Agent mailbox"
      description="Viewing a message does not deliver, acknowledge, or complete it. This bounded metadata page is separate from the command inbox."
    >
      <Select
        label="Message state"
        value={state}
        onValueChange={(value) => {
          if (value === 'all' || value === 'pending' || value === 'delivered' || value === 'done') {
            setState(value);
            setAfter(undefined);
            setSelected('');
          }
        }}
        options={['all', 'pending', 'delivered', 'done'].map((value) => ({ value, label: value }))}
      />
      <QueryFeedback query={query} connected={props.connected} />
      {query.data && !items.length && <Empty>No messages in this page.</Empty>}
      {items.map((item) => (
        <article key={item.id} {...stylex.props(layout.column, layout.notice)}>
          <strong>{item.subject || item.source.kind}</strong>
          <span>
            {item.source.id} → {item.recipient_id}
          </span>
          <span>
            Revision {item.revision} · {item.body_bytes} bytes
          </span>
          <Badge>{item.state}</Badge>
          <Button
            variant="ghost"
            disabled={!props.connected}
            onClick={() => setSelected(selected === item.id ? '' : item.id)}
          >
            Inspect message
          </Button>
          {selected === item.id && (
            <MailBody key={`${item.id}:${item.revision}`} {...props} mailID={item.id} />
          )}
        </article>
      ))}
      <PageControls
        after={after}
        next={items.at(-1)?.id}
        busy={query.isFetching}
        connected={props.connected}
        onChange={(value) => {
          setAfter(value);
          setSelected('');
        }}
      />
    </Section>
  );
}
function MailBody(props: InspectorProps & { mailID: string }) {
  const query = useDetailQuery(props, 'mail.read', {
    session_id: props.session.id,
    mail_id: props.mailID,
  });
  const message = query.data;
  return (
    <>
      <QueryFeedback query={query} connected={props.connected} />
      {message && (
        <>
          <span>Read revision {message.mail.revision}</span>
          <CodeBlock code={message.body} label="Message body" maxBytes={128 << 10} />
          {message.mail.evidence_ref && (
            <ContentRead
              session={props.session}
              connected={props.connected}
              reference={message.mail.evidence_ref}
              label="Evidence"
            />
          )}
        </>
      )}
    </>
  );
}
function Blackboard(props: InspectorProps) {
  const [scope, setScope] = useState<'tree' | 'session'>('tree'),
    [after, setAfter] = useState<string>();
  const query = useDetailQuery(
    props,
    'state.list',
    { session_id: props.session.id, scope, limit: 32, ...(after === undefined ? {} : { after }) },
    true,
  );
  const items = query.data?.items ?? [];
  return (
    <Section
      title="Blackboard"
      description="Shared immutable values. Inspection does not publish a version or admit its bytes into model context. Text reads are limited to 1 MiB; explicit downloads support values up to 64 MiB."
    >
      <Select
        label="Shared value scope"
        value={scope}
        options={[
          { value: 'tree', label: 'Entire tree' },
          { value: 'session', label: 'Selected agent' },
        ]}
        onValueChange={(value) => {
          if (value === 'tree' || value === 'session') {
            setScope(value);
            setAfter(undefined);
          }
        }}
      />
      <QueryFeedback query={query} connected={props.connected} />
      {query.data && !items.length && <Empty>No shared values in this page.</Empty>}
      {items.map((item) => (
        <article key={item.id} {...stylex.props(layout.column, layout.notice)}>
          <strong>{item.key}</strong>
          <span>
            Version {item.revision} · {item.author_id} · {item.size} bytes
          </span>
          <StateRead session={props.session} version={item} connected={props.connected} />
        </article>
      ))}
      <PageControls
        after={after}
        next={items.at(-1)?.key}
        busy={query.isFetching}
        connected={props.connected}
        onChange={setAfter}
      />
    </Section>
  );
}
export function Executions(props: InspectorProps) {
  const evidence = useExecutionView(props.execution),
    history = useSessionView(props.view);
  const rows = cellExecutionRows(evidence, history.history.messages),
    direct = evidence.operations.filter((operation) => operation.cell_id === null);
  const [expanded, setExpanded] = useState<string>(),
    [focused, setFocused] = useState('');
  return (
    <Section
      title="Executions"
      description="Recorded cells and host operations from the bounded native execution window. Raw VM globals are not exposed. Direct human operations have no code cell."
    >
      {evidence.status !== 'live' && <p role="status">Execution observation: {evidence.status}</p>}
      {evidence.truncated && (
        <p role="status">
          Some execution details exceed this window. Select an exact turn or load an older page.
        </p>
      )}
      {evidence.error && (
        <ErrorNotice
          type="resource"
          owner={`${props.session.id}:execution`}
          error={evidence.error.message}
        />
      )}
      <Select
        label="Inspect turn"
        value={focused}
        options={[
          { value: '', label: 'Latest turns' },
          ...evidence.turns.map((turn) => ({
            value: turn.id,
            label: `${turn.kind} · ${turn.state} · ${turn.id}`,
          })),
        ]}
        onValueChange={(id) => {
          setFocused(id);
          void (id ? props.execution.focus(id) : props.execution.latest());
        }}
      />
      {(focused || evidence.turns[0]?.id) && <TurnUsage {...props} turnID={focused || evidence.turns[0]!.id} />}
      {rows.map((row, index) => (
        <ExecutionCellCard
          key={row.cell.id}
          row={row}
          number={index + 1}
          session={props.session}
          engine={props.tree.engine}
          connected={props.connected}
          expanded={expanded === row.cell.id}
          onToggle={() => setExpanded(expanded === row.cell.id ? undefined : row.cell.id)}
        />
      ))}
      {direct.map((operation) => (
        <article key={operation.id} {...stylex.props(layout.column, layout.notice)}>
          <strong>{operation.capability}</strong>
          <Badge>{operation.state}</Badge>
          <code>{operation.resource}</code>
          <span>Turn {operation.turn_id} · direct human operation</span>
          {operation.result?.failure && (
            <ErrorNotice type="execution" owner={operation.id} error={operation.result.failure} />
          )}
          {operation.result?.value !== undefined && (
            <CodeBlock
              code={JSON.stringify(operation.result.value, null, 2)}
              label="Operation output"
              maxBytes={128 << 10}
            />
          )}
          {operation.result?.content_references?.map((reference) => (
            <ContentRead
              key={reference}
              session={props.session}
              connected={props.connected}
              reference={reference}
              label="Operation result"
            />
          ))}
        </article>
      ))}
      {!rows.length && !direct.length && <Empty>No executions in this window.</Empty>}
      {evidence.olderCursor && (
        <Button
          disabled={!props.connected}
          variant="ghost"
          onClick={() => void props.execution.loadOlder()}
        >
          Older execution page
        </Button>
      )}
      {evidence.latestMissing && (
        <Button
          disabled={!props.connected}
          variant="ghost"
          onClick={() => void props.execution.latest()}
        >
          Latest executions
        </Button>
      )}
    </Section>
  );
}
