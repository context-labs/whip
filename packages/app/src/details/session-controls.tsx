import { useState } from 'react';
import { SessionReload } from './session-reload';
import { useExecutionView, useSessionView } from '@whip/sdk/react';
import type { Operations } from '@whip/sdk';
type GoalRef = NonNullable<Operations['goals.create']['params']['expected_current']>;
type RunConfiguration = Operations['run.configure']['params']['configuration'];
type CompactionPolicy = NonNullable<
  Operations['sessions.configure']['params']['patch']['compaction']
>;
import { Badge, Button, CodeBlock, Field, Input, Select, Textarea } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { layout } from '../styles';
import { ModelSelection } from '../model-selection';
import { WholeTreeUsage } from './usage';
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

const counter = (value: string) =>
  /^(0|[1-9][0-9]{0,18})$/.test(value) && BigInt(value) <= 9223372036854775807n;
const identity = (props: InspectorProps) => ({
  client_id: props.client.clientID,
  request_id: crypto.randomUUID(),
});
function useIdle(props: InspectorProps) {
  const value = useSessionView(props.view).activity;
  return !!value && !value.active_turn && !value.active_workspace_action_id;
}

export function Goals(props: InspectorProps) {
  const runtime = useRuntime(),
    query = useDetailQuery(props, 'goals.current', { session_id: props.session.id }, true);
  const [draft, setDraft] = useState<{
    text: string;
    continuations: string;
    expected: GoalRef | null;
  }>();
  const goal = query.data?.goal,
    text = draft?.text ?? goal?.spec.text ?? '',
    continuations = draft?.continuations ?? goal?.spec.max_continuations ?? '100';
  const expected = goal ? { id: goal.id, revision: goal.revision } : null;
  const edit = (change: { text?: string; continuations?: string }) =>
    setDraft({ text, continuations, expected, ...draft, ...change });
  const changed =
    !!draft &&
    (draft.expected?.id !== expected?.id || draft.expected?.revision !== expected?.revision);
  const create = async (start: boolean) => {
    await runtime.run(
      runtime.command(props.client, 'goals.create', {
        session_id: props.session.id,
        goal_id: crypto.randomUUID(),
        expected_current: draft ? draft.expected : expected,
        spec: { text, max_continuations: continuations },
        start,
      }),
      start ? 'Start goal' : 'Save goal',
    );
    setDraft(undefined);
    await query.refetch();
  };
  return (
    <>
      <Section
        title="Session goal"
        description="The execution host owns goal continuation, including after you disconnect. Saving or starting a goal does not mean its work has completed."
      >
        <QueryFeedback query={query} connected={props.connected} />
        {goal && (
          <p>
            <Badge>{goal.state}</Badge> · {goal.continuations_used} additional continuations used
            {goal.stop_reason && ` · ${goal.stop_reason}`}
          </p>
        )}
        <Field label="Goal">
          <Textarea value={text} onChange={(event) => edit({ text: event.target.value })} />
        </Field>
        <Field
          label="Additional goal continuations"
          description="Exact whole number; zero allows only the initial input."
        >
          <Input
            value={continuations}
            inputMode="numeric"
            onChange={(event) => edit({ continuations: event.target.value })}
          />
        </Field>
        {changed && (
          <p role="status">
            The current goal changed while you were editing. Refresh before applying this draft.
          </p>
        )}
        <div {...stylex.props(layout.row, layout.wrap)}>
          <Action
            recoverable
            disabled={!props.connected || !query.data || !text.trim() || !counter(continuations)}
            run={() => create(false)}
          >
            Save goal
          </Action>
          <Action
            recoverable
            disabled={!props.connected || !query.data || !text.trim() || !counter(continuations)}
            run={() => create(true)}
          >
            Run goal
          </Action>
          <Action
            recoverable
            disabled={!props.connected || !query.data}
            run={async () => {
              await runtime.run(
                runtime.command(props.client, 'goals.formulate', {
                  session_id: props.session.id,
                  identity: identity(props),
                  request: {
                    goal_id: crypto.randomUUID(),
                    expected_current: expected,
                    start: false,
                  },
                }),
                'Draft goal from context',
              );
              setDraft(undefined);
              await query.refetch();
            }}
          >
            Draft from context
          </Action>
          {goal?.state === 'paused' && (
            <Action
              recoverable
              disabled={!props.connected}
              run={async () => {
                await runtime.run(
                  runtime.command(props.client, 'goals.resume', {
                    session_id: props.session.id,
                    identity: identity(props),
                    goal: { id: goal.id, revision: goal.revision },
                  }),
                  'Resume goal',
                );
                await query.refetch();
              }}
            >
              Resume goal
            </Action>
          )}
          <Action
            disabled={
              !props.connected || !goal || goal.state === 'cancelled' || goal.state === 'completed'
            }
            run={async () => {
              if (goal) await props.session.goals.cancel(goal.id);
              setDraft(undefined);
              await query.refetch();
            }}
          >
            Cancel goal
          </Action>
          <Button
            variant="ghost"
            disabled={!props.connected}
            onClick={() => {
              setDraft(undefined);
              void query.refetch();
            }}
          >
            Refresh goal
          </Button>
        </div>
      </Section>
      <Schedules {...props} />
    </>
  );
}
function Schedules(props: InspectorProps) {
  const runtime = useRuntime(),
    [after, setAfter] = useState<string>(),
    [expression, setExpression] = useState(''),
    [prompt, setPrompt] = useState(''),
    [selected, setSelected] = useState('');
  const query = useDetailQuery(
      props,
      'schedules.list',
      { session_id: props.session.id, limit: 16, ...(after === undefined ? {} : { after }) },
      true,
    ),
    items = query.data?.items ?? [];
  return (
    <Section
      title="Schedules"
      description="Schedules run on the execution host after you disconnect. Cancellation retains their recorded history."
    >
      <QueryFeedback query={query} connected={props.connected} />
      {!items.length && query.data && <Empty>No schedules in this page.</Empty>}
      {items.map((item) => (
        <article key={item.id} {...stylex.props(layout.column, layout.notice)}>
          <strong>{item.expression}</strong>
          <p>
            {item.preview}
            {item.preview_truncated && '…'}
          </p>
          <span>
            Next: {item.next_due ?? 'None'} · Last fire: {item.latest?.scheduled_for ?? 'Never'}
          </span>
          {item.failure && <p>{item.failure}</p>}
          <Button
            variant="ghost"
            disabled={!props.connected}
            onClick={() => setSelected(selected === item.id ? '' : item.id)}
          >
            Read scheduled prompt
          </Button>
          {selected === item.id && <ScheduleBody {...props} id={item.id} />}
          {item.cancelled_at ? (
            <Badge>Cancelled</Badge>
          ) : (
            <Action
              disabled={!props.connected}
              run={async () => {
                await props.session.schedules.cancel(item.id);
                await query.refetch();
              }}
            >
              Cancel schedule
            </Action>
          )}
        </article>
      ))}
      <PageControls
        after={after}
        next={query.data?.next_after ?? undefined}
        connected={props.connected}
        busy={query.isFetching}
        onChange={setAfter}
      />
      <Field label="When" description="A WHIP schedule expression, such as every 30m.">
        <Input value={expression} onChange={(event) => setExpression(event.target.value)} />
      </Field>
      <Field label="Scheduled prompt">
        <Textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} />
      </Field>
      <Action
        recoverable
        disabled={!props.connected || !expression.trim() || !prompt.trim()}
        run={async () => {
          await runtime.run(
            runtime.command(props.client, 'schedules.create', {
              session_id: props.session.id,
              schedule_id: crypto.randomUUID(),
              expression,
              parts: [{ type: 'text', text: prompt }],
            }),
            'Create schedule',
          );
          setExpression('');
          setPrompt('');
          await query.refetch();
        }}
      >
        Create schedule
      </Action>
    </Section>
  );
}
function ScheduleBody(props: InspectorProps & { id: string }) {
  const query = useDetailQuery(props, 'schedules.get', {
    session_id: props.session.id,
    schedule_id: props.id,
  });
  return (
    <>
      <QueryFeedback query={query} connected={props.connected} />
      {query.data?.parts.map((part, index) =>
        part.type === 'text' ? (
          <CodeBlock key={index} code={part.text} label="Scheduled prompt" maxBytes={128 << 10} />
        ) : part.type === 'content' ? (
          <ContentRead
            key={index}
            session={props.session}
            connected={props.connected}
            reference={part.reference_id}
            label="Scheduled attachment"
          />
        ) : (
          <CodeBlock
            key={index}
            code={JSON.stringify(part, null, 2)}
            label="Scheduled part"
            maxBytes={32 << 10}
          />
        ),
      )}
    </>
  );
}
export function formatBudgetAmount(kind: string, value: string) {
  const amount = BigInt(value);
  if (kind === 'model_cost_nano_usd')
    return `$${amount / 1_000_000_000n}.${(amount % 1_000_000_000n).toString().padStart(9, '0')}`;
  if (kind === 'model_elapsed_millis')
    return `${amount / 1000n}.${(amount % 1000n).toString().padStart(3, '0')} s`;
  return amount.toLocaleString();
}
export function Limits(props: InspectorProps) {
  const query = useDetailQuery(props, 'budgets.list', { session_id: props.session.id }, true),
    resources = useDetailQuery(props, 'resources.list', { session_id: props.session.id }, true);
  return (
    <>
      <WholeTreeUsage {...props} />
      <Section
        title="Selected agent budgets"
        description="These budget aggregates include this agent and its descendants. Ancestor and descendant usage can overlap; never add them together. Reserved and uncertain exposure is separate from reported whole-tree usage."
      >
        <QueryFeedback query={query} connected={props.connected} />
        {query.data?.items?.map((item) => (
          <div
            key={`${item.session_id}:${item.kind}`}
            {...stylex.props(layout.column, layout.notice)}
          >
            <strong>{item.kind.replaceAll('_', ' ')}</strong>
            <code>{item.session_id}</code>
            <span>
              {formatBudgetAmount(item.kind, item.used)} used ·{' '}
              {formatBudgetAmount(item.kind, item.reserved)} in flight
            </span>
            <span>
              {item.limit === null
                ? 'No local cap'
                : `Limit ${formatBudgetAmount(item.kind, item.limit)}`}
            </span>
            {item.incomplete && (
              <p>
                Usage is incomplete · {formatBudgetAmount(item.kind, item.uncertain)} unconfirmed.
              </p>
            )}
          </div>
        ))}
      </Section>
      <Section
        title="Resource limits"
        description="Host hard limits and ancestor limits still apply when this agent has no local cap."
      >
        <QueryFeedback query={resources} connected={props.connected} />
        {resources.data?.items?.map((item) => (
          <div
            key={`${item.session_id}:${item.kind}`}
            {...stylex.props(layout.column, layout.notice)}
          >
            <strong>{item.kind.replaceAll('_', ' ')}</strong>
            <span>
              {item.used} used · {item.limit === null ? 'No local cap' : `Limit ${item.limit}`}
            </span>
            <code>{item.session_id}</code>
          </div>
        ))}
      </Section>
      <Grants {...props} />
    </>
  );
}
export function ContextSettings(props: InspectorProps) {
  const [section, setSection] = useState('context');
  return (
    <>
      <Select
        label="Context settings section"
        value={section}
        onValueChange={setSection}
        options={[
          { value: 'context', label: 'Applied context' },
          { value: 'model', label: 'Model & runtime' },
          { value: 'compaction', label: 'Compaction' },
        ]}
      />
      {section === 'context' && <Context {...props} />}
      {section === 'model' && <ModelSettings {...props} />}
      {section === 'compaction' && <Compaction {...props} />}
    </>
  );
}
function Context(props: InspectorProps) {
  const workspace = useDetailQuery(props, 'workspace.inspect', { session_id: props.session.id }),
    evidence = useExecutionView(props.execution);
  const [turn, setTurn] = useState(''),
    [draft, setDraft] = useState<{ path: string; revision: string }>();
  const selectedTurn = turn || evidence.turns[0]?.id;
  const runtime = useRuntime(),
    idle = useIdle(props),
    path = draft?.path ?? workspace.data?.working_directory ?? props.selected.working_directory;
  return (
    <>
      <Section
        title="Applied context"
        description="Inspect the captured instruction manifest of an exact turn. Current configuration governs future turns; it does not rewrite an earlier capture."
      >
        <Select
          label="Captured turn"
          value={selectedTurn ?? ''}
          options={evidence.turns.map((value) => ({
            value: value.id,
            label: `${value.kind} · ${value.id}`,
          }))}
          onValueChange={setTurn}
        />
        {selectedTurn ? (
          <Instructions key={selectedTurn} {...props} turnID={selectedTurn} />
        ) : (
          <Empty>No captured turn in this execution window.</Empty>
        )}
        <p>Current standing instruction text:</p>
        <CodeBlock
          code={props.selected.configuration.instructions.text}
          label="Configured instructions"
          maxBytes={32 << 10}
        />
      </Section>
      <SessionReload key={`${props.client.runtimeID}:${props.session.id}`} {...props} />
      <Section
        title="Workspace"
        description="This path is on the execution host. Changes use the exact configuration revision and apply while this agent is idle."
      >
        <QueryFeedback query={workspace} connected={props.connected} />
        <p>{workspace.data?.working_directory ?? props.selected.working_directory}</p>
        <Field label="Host working directory">
          <Input
            value={path}
            onChange={(event) =>
              setDraft({
                path: event.target.value,
                revision:
                  draft?.revision ??
                  workspace.data?.configuration_revision ??
                  props.selected.config_revision,
              })
            }
          />
        </Field>
        <Action
          recoverable
          disabled={!props.connected || !idle || !path.trim()}
          run={async () => {
            await runtime.run(
              runtime.command(props.client, 'workspace.set', {
                id: crypto.randomUUID(),
                session_id: props.session.id,
                expected_revision:
                  draft?.revision ??
                  workspace.data?.configuration_revision ??
                  props.selected.config_revision,
                path,
              }),
              'Change workspace',
            );
            setDraft(undefined);
            await workspace.refetch();
            await props.view.refresh();
          }}
        >
          Change workspace
        </Action>
      </Section>
    </>
  );
}
function Instructions(props: InspectorProps & { turnID: string }) {
  const query = useDetailQuery(props, 'turns.instructions', { turn_id: props.turnID });
  return (
    <>
      <QueryFeedback query={query} connected={props.connected} />
      {query.data?.manifest ? (
        <>
          <span>{query.data.manifest.bytes} captured bytes</span>
          {query.data.manifest.sources.map((source, index) => (
            <div key={index} {...stylex.props(layout.column, layout.notice)}>
              <strong>{source.kind.replaceAll('_', ' ')}</strong>
              <code>{source.path}</code>
              <span>
                {source.scope} · {source.bytes} bytes
              </span>
              <details>
                <summary>Captured digest</summary>
                <code>{source.sha256}</code>
              </details>
            </div>
          ))}
        </>
      ) : (
        query.data && <Empty>No instruction manifest was captured for this turn.</Empty>
      )}
    </>
  );
}
function ModelSettings(props: InspectorProps) {
  const runtime = useRuntime(),
    idle = useIdle(props),
    [draft, setDraft] = useState<{ revision: string; value: RunConfiguration }>();
  const configuration = draft?.value ??
    props.selected.configuration.run ?? {
      system: '',
      max_turns: 0,
      headless: false,
      cache_key: '',
    };
  const edit = (patch: Partial<RunConfiguration>) =>
    setDraft({
      revision: draft?.revision ?? props.selected.config_revision,
      value: { ...configuration, ...patch },
    });
  return (
    <>
      <Section
        title="Model & reasoning"
        description="Changes apply to the selected root or child while it is idle. Provider credentials remain on the execution host."
      >
        <ModelSelection {...props} />
      </Section>
      <Section
        title="Run configuration"
        description="Explicit overrides preserve the remaining captured settings. Zero maximum turns is uncapped."
      >
        <Field label="System instruction override">
          <Textarea
            value={configuration.system}
            onChange={(event) => edit({ system: event.target.value })}
          />
        </Field>
        <Field label="Maximum turns">
          <Input
            type="number"
            min={0}
            max={1000000}
            value={configuration.max_turns}
            onChange={(event) =>
              edit({ max_turns: event.target.value === '' ? 0 : Number(event.target.value) })
            }
          />
        </Field>
        {draft && draft.revision !== props.selected.config_revision && (
          <p role="status">
            Configuration changed while you were editing. Refresh before applying this draft.
          </p>
        )}
        <Action
          recoverable
          disabled={
            !props.connected ||
            !idle ||
            !Number.isSafeInteger(configuration.max_turns) ||
            configuration.max_turns < 0 ||
            configuration.max_turns > 1000000
          }
          run={async () => {
            await runtime.run(
              runtime.command(props.client, 'run.configure', {
                id: crypto.randomUUID(),
                session_id: props.session.id,
                expected_revision: draft?.revision ?? props.selected.config_revision,
                configuration,
              }),
              'Apply run configuration',
            );
            setDraft(undefined);
            await props.view.refresh();
          }}
        >
          Apply overrides
        </Action>
        <Button
          variant="ghost"
          disabled={!props.connected}
          onClick={() => {
            setDraft(undefined);
            void props.view.refresh();
          }}
        >
          Refresh configuration
        </Button>
        <p>Reloading all captured host defaults is not yet available in this inspector.</p>
      </Section>
    </>
  );
}
export function Compaction(props: InspectorProps) {
  const runtime = useRuntime(),
    idle = useIdle(props),
    [after, setAfter] = useState<string>(),
    [selected, setSelected] = useState<string>();
  const [draft, setDraft] = useState<{ revision: string; policy: CompactionPolicy }>();
  const policy = draft?.policy ?? props.selected.configuration.compaction;
  const query = useDetailQuery(props, 'context.compactions', {
      session_id: props.session.id,
      limit: 16,
      ...(after === undefined ? {} : { after }),
    }),
    head = useDetailQuery(props, 'context.head', { session_id: props.session.id }, true);
  const edit = (patch: Partial<CompactionPolicy>) =>
    setDraft({
      revision: draft?.revision ?? props.selected.config_revision,
      policy: { ...structuredClone(policy), ...patch },
    });
  const editModel = (field: 'name' | 'provider', value: string) =>
    edit({
      model: {
        provider: policy.model?.provider ?? props.selected.configuration.model.provider,
        name: policy.model?.name ?? props.selected.configuration.model.name,
        effort: policy.model?.effort ?? '',
        temperature: policy.model?.temperature ?? null,
        top_p: policy.model?.top_p ?? null,
        [field]: value,
      },
    });
  return (
    <>
      <Section
        title="Compaction"
        description="Compaction changes model context while retaining raw transcript history. It does not undo files. These settings belong to the selected agent; host defaults remain in Settings."
      >
        <Action
          recoverable
          disabled={!props.connected || !idle}
          run={async () => {
            await runtime.run(
              runtime.command(props.client, 'sessions.compact', {
                session_id: props.session.id,
                identity: identity(props),
              }),
              'Compact context',
            );
            await query.refetch();
            await head.refetch();
          }}
        >
          Compact now
        </Action>
        <Field label="Compaction threshold percent" description="Zero uses the host default.">
          <Input
            type="number"
            min={0}
            max={100}
            value={policy.threshold_percent}
            onChange={(event) => edit({ threshold_percent: Number(event.target.value) })}
          />
        </Field>
        <Field label="Compaction model" description="An empty model uses the conversation model.">
          <Input
            value={policy.model?.name ?? ''}
            onChange={(event) => editModel('name', event.target.value)}
          />
        </Field>
        <Field label="Compaction provider">
          <Input
            value={policy.model?.provider ?? ''}
            onChange={(event) => editModel('provider', event.target.value)}
          />
        </Field>
        <Action
          disabled={
            !props.connected ||
            !idle ||
            !Number.isInteger(policy.threshold_percent) ||
            policy.threshold_percent < 0 ||
            policy.threshold_percent > 100
          }
          run={async () => {
            try {
              await props.session.configure(draft?.revision ?? props.selected.config_revision, {
                compaction: { ...policy, model: policy.model?.name ? { ...policy.model } : null },
              });
              setDraft(undefined);
            } finally {
              await props.view.refresh();
            }
          }}
        >
          Apply compaction settings
        </Action>
        {draft && draft.revision !== props.selected.config_revision && (
          <p role="status">Configuration changed. This draft retains its original revision.</p>
        )}
        <Button
          variant="ghost"
          disabled={!props.connected}
          onClick={() => {
            setDraft(undefined);
            void props.view.refresh();
          }}
        >
          Refresh compaction settings
        </Button>
      </Section>
      <Section title="Compaction history">
        <QueryFeedback query={query} connected={props.connected} />
        <QueryFeedback query={head} connected={props.connected} />
        {query.data?.items?.map((item) => (
          <article key={item.id} {...stylex.props(layout.column, layout.notice)}>
            <strong>Through message {item.through_sequence}</strong>
            <span>
              History revision {item.history_revision} · {item.text_bytes} bytes
            </span>
            {item.id === head.data?.compaction_id && <Badge>Selected</Badge>}
            <Button
              variant="ghost"
              disabled={!props.connected}
              onClick={() => setSelected(selected === item.id ? undefined : item.id)}
            >
              Read summary
            </Button>
            {selected === item.id && <CompactionText {...props} id={item.id} />}
          </article>
        ))}
        {query.data?.items?.length === 0 && <Empty>No compactions in this page.</Empty>}
        <PageControls
          after={after}
          next={query.data?.items?.at(-1)?.id}
          busy={query.isFetching}
          connected={props.connected}
          onChange={setAfter}
        />
        <Action
          disabled={!props.connected || !idle || !head.data?.compaction_id}
          run={async () => {
            const current = head.data;
            if (!current?.compaction_id) return;
            const record = await props.client.call('context.compaction', {
              session_id: props.session.id,
              compaction_id: current.compaction_id,
            });
            await props.client.call('context.select', {
              session_id: props.session.id,
              expected_revision: current.revision,
              compaction_id: record.metadata.base_id,
            });
            await head.refetch();
          }}
        >
          Undo selected compaction
        </Action>
      </Section>
    </>
  );
}
function CompactionText(props: InspectorProps & { id: string }) {
  const query = useDetailQuery(props, 'context.compaction', {
    session_id: props.session.id,
    compaction_id: props.id,
  });
  return (
    <>
      <QueryFeedback query={query} connected={props.connected} />
      {query.data && (
        <CodeBlock code={query.data.text} label="Compaction summary" maxBytes={128 << 10} />
      )}
    </>
  );
}
function Grants(props: InspectorProps) {
  const [after, setAfter] = useState<string>(),
    query = useDetailQuery(
      props,
      'grants.list',
      { session_id: props.session.id, limit: 32, ...(after === undefined ? {} : { after }) },
      true,
    );
  return (
    <Section
      title="Delegated capabilities"
      description="Grants name exact capabilities and resource scopes. Revoking one also removes dependent authority; Full Access does not widen a child’s delegation."
    >
      <QueryFeedback query={query} connected={props.connected} />
      {query.data?.items?.map((grant) => (
        <article key={grant.id} {...stylex.props(layout.column, layout.notice)}>
          <strong>{grant.capability}</strong>
          <Badge>
            {grant.revoked_at ? 'Revoked' : grant.operation_id ? 'One operation' : 'Standing'}
          </Badge>
          <code>{grant.resource}</code>
          <span>
            Owner {grant.session_id}
            {grant.issuer_id && ` · issued by ${grant.issuer_id}`}
          </span>
          <Action
            disabled={!props.connected || grant.revoked_at !== null}
            run={async () => {
              await props.client.call('grants.revoke', { grant_id: grant.id });
              await query.refetch();
            }}
          >
            Revoke grant
          </Action>
        </article>
      ))}
      {query.data?.items?.length === 0 && <Empty>No grants in this page.</Empty>}
      <PageControls
        after={after}
        next={query.data?.items?.at(-1)?.id}
        busy={query.isFetching}
        connected={props.connected}
        onChange={setAfter}
      />
    </Section>
  );
}
export function Permissions(props: InspectorProps) {
  const runtime = useRuntime(),
    query = useDetailQuery(props, 'permissions.policy', { session_id: props.session.id }, true);
  const [denial, setDenial] = useState<{ value: boolean; revision: string }>();
  const [draft, setDraft] = useState<{ mode: 'prompt' | 'automatic'; revision: string }>(),
    current = query.data;
  return (
    <>
      <Section
        title="Permission policy"
        description="Full Access approves eligible root actions automatically. Child grants, resource scopes, untrusted MCP approval, and intrinsic validation still apply."
      >
        <QueryFeedback query={query} connected={props.connected} />
        {current && (
          <>
            <p>
              Current: {current.mode === 'automatic' ? 'Full Access' : 'Ask for approval'} ·
              revision {current.revision}
            </p>
            <Select
              label="Permission policy"
              value={draft?.mode ?? current.mode}
              onValueChange={(mode) => {
                if (mode === 'prompt' || mode === 'automatic')
                  setDraft({ mode, revision: draft?.revision ?? current.revision });
              }}
              options={[
                { value: 'prompt', label: 'Ask for approval' },
                { value: 'automatic', label: 'Full Access' },
              ]}
            />
            <Action
              recoverable
              disabled={!props.connected || props.session.id !== props.rootId}
              run={async () => {
                try {
                  await runtime.run(
                    runtime.command(props.client, 'permissions.set_mode', {
                      session_id: props.session.id,
                      edit_id: crypto.randomUUID(),
                      expected_revision: draft?.revision ?? current.revision,
                      mode: draft?.mode ?? current.mode,
                    }),
                    'Change permission policy',
                  );
                  setDraft(undefined);
                } finally {
                  await query.refetch();
                }
              }}
            >
              Apply policy
            </Action>
            <Select
              label="Interactive permission requests"
              value={(denial?.value ?? current.deny_interactive) ? 'deny' : 'allow'}
              onValueChange={value => setDenial({ value: value === 'deny', revision: denial?.revision ?? current.revision })}
              options={[{ value: 'allow', label: 'Allow permission requests' }, { value: 'deny', label: 'Deny interactive permission requests' }]}
            />
            <p>This separate tree-wide setting closes pending approval requests and denies actions that require interactive permission. It preserves the approval mode and standing grants. Intrinsic questions still work.</p>
            <Action recoverable disabled={!props.connected || props.session.id !== props.rootId} run={async () => {
              try {
                await runtime.run(runtime.command(props.client, 'permissions.set_denial', {
                  session_id: props.session.id, edit_id: crypto.randomUUID(),
                  expected_revision: denial?.revision ?? current.revision,
                  deny_interactive: denial?.value ?? current.deny_interactive,
                }), 'Change interactive permission denial');
                setDenial(undefined);
              } finally { await query.refetch(); }
            }}>Apply request policy</Action>
            {denial && denial.revision !== current.revision && <p role="status">Permission requests changed while you were editing. This choice retains its original revision.</p>}
            {props.session.id !== props.rootId && (
              <p>
                Change the shared policy from the root agent. A displayed mode does not grant
                authority to this child.
              </p>
            )}
            {draft && draft.revision !== current.revision && (
              <p role="status">
                Policy changed while you were editing. This draft retains its original revision.
              </p>
            )}
            <Button
              variant="ghost"
              disabled={!props.connected}
              onClick={() => {
                setDraft(undefined);
                setDenial(undefined);
                void query.refetch();
              }}
            >
              Refresh policy
            </Button>
          </>
        )}
        <p>
          Saved host defaults are managed in Settings.
        </p>
      </Section>
      <Grants {...props} />
    </>
  );
}
