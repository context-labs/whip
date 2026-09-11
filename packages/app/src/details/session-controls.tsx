import { ErrorNotice } from '../error-feedback';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge, Button, CodeBlock, Field, Input, Select, Textarea } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { layout } from '../styles';
import { ModelSelection } from '../model-selection';
import {
  Action,
  CollectionMore,
  Empty,
  QueryFeedback,
  Section,
  mergeBy,
  useCollection,
  useDetailQuery,
  type InspectorProps,
} from './shared';

export function Goals({ view, root, connected }: InspectorProps) {
  const runtime = useRuntime();
  const [goal, setGoal] = useState(root.meta.goal);
  const [schedule, setSchedule] = useState('');
  const [prompt, setPrompt] = useState('');
  const collection = useCollection(view, 'schedules');
  const schedules = mergeBy(
    root.schedules ?? [],
    collection.page?.items?.flatMap((item) => (item.schedule ? [item.schedule] : [])) ?? [],
    (item) => String(item.id),
  );
  return (
    <>
      <Section
        title="Session goal"
        description="The daemon owns goal continuation. Completing one command does not mean every descendant or schedule has finished."
      >
        <Field label="Goal">
          <Textarea value={goal} onChange={(event) => setGoal(event.target.value)} />
        </Field>
        <div {...stylex.props(layout.row, layout.wrap)}>
          <Action
            disabled={!connected}
            run={() => runtime.run(view.session.command('goal.set', { text: goal }), 'Save goal')}
          >
            Save goal
          </Action>
          <Action
            disabled={!connected || !goal.trim()}
            run={() => runtime.run(view.session.command('goal.run', { text: goal }), 'Run goal')}
          >
            Run goal
          </Action>
          <Action
            disabled={!connected}
            run={async () => {
              const outcome = await runtime.run(
                view.session.command('goal.from-context', {}),
                'Draft goal from context',
              );
              setGoal(outcome.result?.goal || '');
            }}
          >
            Draft from context
          </Action>
          <Action
            disabled={!connected || !root.meta.goal}
            run={async () => {
              await runtime.run(view.session.command('goal.set', { text: '' }), 'Clear goal');
              setGoal('');
            }}
          >
            Clear goal
          </Action>
        </div>
      </Section>
      <Section
        title="Schedules"
        description="Schedules run on the execution host, including after you disconnect."
      >
        {!schedules.length && <Empty>No schedules.</Empty>}
        {schedules.map((item) => (
          <article key={item.id} {...stylex.props(layout.column, layout.notice)}>
            <strong>{item.schedule}</strong>
            <p>{item.prompt}</p>
            <span {...stylex.props(layout.muted)}>Last fire: {item.last_fire || 'Never'}</span>
            <Action
              disabled={!connected}
              run={() =>
                runtime.run(
                  view.session.command('schedule.delete', { schedule_id: item.id }),
                  'Delete schedule',
                )
              }
            >
              Delete schedule
            </Action>
          </article>
        ))}
        <CollectionMore
          collection={collection}
          omitted={root.omitted?.schedules}
          connected={connected}
        />
        <Field label="When" description="A WHIP schedule expression, such as every 30m.">
          <Input value={schedule} onChange={(event) => setSchedule(event.target.value)} />
        </Field>
        <Field label="Scheduled prompt">
          <Textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} />
        </Field>
        <Action
          disabled={!connected || !schedule.trim() || !prompt.trim()}
          run={async () => {
            await runtime.run(
              view.session.command('schedule.create', { schedule, prompt }),
              'Create schedule',
            );
            setSchedule('');
            setPrompt('');
          }}
        >
          Create schedule
        </Action>
      </Section>
    </>
  );
}
export function formatBudgetAmount(kind: string, value: string) {
  const amount = BigInt(value);
  if (kind === 'cost') return `$${amount / 1_000_000n}.${(amount % 1_000_000n).toString().padStart(6, '0')}`;
  if (kind === 'elapsed') return `${amount / 1000n}.${(amount % 1000n).toString().padStart(3, '0')} s`;
  return `${amount.toLocaleString()}${kind === 'tokens' ? ' tokens' : ''}`;
}

export function Limits({ view, root, connected }: InspectorProps) {
  const runtime = useRuntime();
  const budgets = useCollection(view, 'budgets');
  const capabilities = useCollection(view, 'capabilities');
  const values = mergeBy(
    root.budgets ?? [],
    budgets.page?.items?.flatMap((item) => (item.budget ? [item.budget] : [])) ?? [],
    (item) => `${item.agent_id}:${item.state.kind}`,
  );
  const grants = mergeBy(
    root.capabilities ?? [],
    capabilities.page?.items?.flatMap((item) => (item.capability ? [item.capability] : [])) ?? [],
    (item) => item.id,
  );
  const agents = root.agents ?? [];
  const accounting = root.accounting?.root_id === root.root_id && root.accounting.agent_id === root.root_id
    && root.accounting.scope === 'subtree' ? root.accounting : undefined;
  const calls = (value: string) => `${BigInt(value).toLocaleString()} ${value === '1' ? 'call' : 'calls'}`;
  return (
    <>
      <Section
        title="Usage"
        description="Usage includes descendants. Ancestor and child totals overlap. Model usage is unlimited unless an agent explicitly caps a child."
      >
        {accounting ? <div {...stylex.props(layout.column)} aria-label="Entire session tree accounting">
          <strong>Entire session tree</strong>
          <span>Provider-reported cost: {formatBudgetAmount('cost', accounting.reported_cost_micros)} · {calls(accounting.reported_cost_calls)}</span>
          <span>Catalog-estimated cost: {formatBudgetAmount('cost', accounting.estimated_cost_micros)} · {calls(accounting.estimated_cost_calls)}</span>
          <span>Unknown cost: {calls(accounting.unknown_cost_calls)}</span>
          <span>Missing token usage: {calls(accounting.estimated_calls)}</span>
          <span>In-flight requests: {calls(accounting.pending_calls)}</span>
          <p {...stylex.props(layout.muted)}>Missing token usage and unknown cost are independent; a provider can report a charge without reporting tokens.</p>
        </div> : <p {...stylex.props(layout.muted)}>Model accounting details are unavailable on this snapshot.</p>}
        {!accounting && <p>
          {root.meta.usage_in.toLocaleString()} input · {root.meta.usage_out.toLocaleString()}{' '}
          output · {root.meta.usage_cached.toLocaleString()} cached tokens
        </p>}
        {values.map((item) => (
          <div
            key={`${item.agent_id}:${item.state.kind}`}
            {...stylex.props(layout.column, layout.notice)}
          >
            <strong>{item.state.kind}</strong>
            <span {...stylex.props(layout.muted)}>
              {agents.find((agent) => agent.id === item.agent_id)?.name || item.agent_id || 'Root tree'}
            </span>
            <span>
              {formatBudgetAmount(item.state.kind, item.state.used)} used · {formatBudgetAmount(item.state.kind, item.state.reserved)} in flight
            </span>
            <span>
              {item.state.limit === null
                ? 'Unlimited'
                : `${formatBudgetAmount(item.state.kind, item.state.remaining!)} remaining of ${formatBudgetAmount(item.state.kind, item.state.limit)}`}
            </span>
            {item.state.incomplete && <span {...stylex.props(layout.muted)}>
              Usage is incomplete{item.state.uncertain !== '0' ? ` · estimated ${formatBudgetAmount(item.state.kind, item.state.uncertain)} unconfirmed` : ''}.
            </span>}
          </div>
        ))}
        <CollectionMore
          collection={budgets}
          omitted={root.omitted?.budgets}
          connected={connected}
        />
      </Section>
      <Section
        title="Delegated capabilities"
        description="Revoking a grant changes the authority available to agents. The daemon revalidates it on execution."
      >
        {!grants.length && <Empty>No delegated grants in this page.</Empty>}
        {grants.map((grant) => (
          <article key={grant.id} {...stylex.props(layout.column, layout.notice)}>
            <div {...stylex.props(layout.row)}>
              <strong>{grant.agent_id}</strong>
              <Badge>{grant.status}</Badge>
            </div>
            <span>{grant.operations?.join(', ')}</span>
            <span {...stylex.props(layout.muted)}>
              {grant.file_scope === 'inherit' ? 'Inherits the issuer’s filesystem scope'
                : grant.file_scope === 'session' && root.permission_mode === 'automatic' ? 'All host paths (Full Access)'
                  : grant.scopes?.join(', ')}
            </span>
            {grant.mcp?.map((scope, index) => (
              <CodeBlock
                key={index}
                code={JSON.stringify(scope, null, 2)}
                label="MCP authority"
                maxBytes={8192}
              />
            ))}
            <Action
              disabled={
                !connected ||
                grant.status !== 'active' ||
                !agents
                  .find((agent) => agent.id === grant.agent_id)
                  ?.allowed_controls?.includes('capability.revoke')
              }
              run={() =>
                runtime.run(
                  view.session.command('capability.revoke', { id: grant.id }),
                  'Revoke capability',
                )
              }
            >
              Revoke grant
            </Action>
          </article>
        ))}
        <CollectionMore
          collection={capabilities}
          omitted={root.omitted?.capabilities}
          connected={connected}
        />
      </Section>
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
  const query = useDetailQuery(props, 'context.audit', {});
  const workspace = useDetailQuery(props, 'workspace.inspect', {});
  const runtime = useRuntime();
  const [path, setPath] = useState(props.root.meta.cwd);
  return (
    <>
      <Section
        title="Applied context"
        description="Environment context is assembled on the execution host. This is the daemon’s summary, not an invented list of files admitted to the model."
      >
        <QueryFeedback view={props.view} query={query} />
        {query.data?.result?.rows?.map((row, index) => (
          <div key={index} {...stylex.props(layout.column, layout.notice)}>
            <strong>{row.label}</strong>
            <span>{row.bytes ?? 0} bytes</span>
            {row.note && <p>{row.note}</p>}
          </div>
        ))}
      </Section>
      <Section
        title="Workspace"
        description="This path is on the execution host. Changing it changes the agent’s working directory."
      >
        <QueryFeedback view={props.view} query={workspace} />
        <p>
          {workspace.data?.result?.path ||
            query.data?.result?.working_directory ||
            props.root.meta.cwd}
        </p>
        <Field label="Host working directory">
          <Input value={path} onChange={(event) => setPath(event.target.value)} />
        </Field>
        <Action
          disabled={
            !props.connected || !path.trim() || !!Object.keys(props.root.active_turns ?? {}).length
          }
          run={() =>
            runtime.run(props.view.session.command('workspace.set', { path }), 'Change workspace')
          }
        >
          Change workspace
        </Action>
      </Section>
      <Section
        title="Automatic session titles"
        description="Enable the daemon to name this session from its first exchange. This operation enables titles; the current protocol does not expose disabling or reading back this policy."
      >
        <Action
          disabled={!props.connected}
          run={() =>
            runtime.run(
              props.view.session.command('session.autotitle', {}),
              'Enable automatic titles',
            )
          }
        >
          Enable automatic titles
        </Action>
      </Section>
    </>
  );
}
function ModelSettings({ view, root, connected }: InspectorProps) {
  const runtime = useRuntime();
  const [system, setSystem] = useState('');
  const [turns, setTurns] = useState('');
  const idle = !Object.keys(root.active_turns ?? {}).length;
  return (
    <>
      <Section
        title="Model & reasoning"
        description="Changes apply to this root session while it is idle. Provider credentials stay on the execution host."
      >
        <ModelSelection view={view} root={root} connected={connected} />
        <Action
          disabled={!connected || !idle}
          run={() =>
            runtime.run(view.session.command('session.reload', {}), 'Reload session runtime')
          }
        >
          Reload runtime from host settings
        </Action>
      </Section>
      <Section
        title="Run configuration"
        description="Advanced values are applied explicitly. Current overrides are not exposed by the protocol."
      >
        <Field label="System instruction override">
          <Textarea value={system} onChange={(event) => setSystem(event.target.value)} />
        </Field>
        <Field label="Maximum turns">
          <Input
            type="number"
            min={1}
            step={1}
            value={turns}
            onChange={(event) => setTurns(event.target.value)}
          />
        </Field>
        <Action
          disabled={
            !connected ||
            !idle ||
            (!system && !turns) ||
            (!!turns && (!Number.isSafeInteger(Number(turns)) || Number(turns) < 1))
          }
          run={() =>
            runtime.run(
              view.session.configure({
                ...(system ? { system } : {}),
                ...(turns ? { max_turns: Number(turns) } : {}),
              }),
              'Apply run configuration',
            )
          }
        >
          Apply overrides
        </Action>
      </Section>
    </>
  );
}
export function Compaction(props: InspectorProps) {
  const runtime = useRuntime();
  const query = useDetailQuery(props, 'history.compact.log', {});
  const configuration = useQuery({
    queryKey: [
      'inspector-compaction-config',
      props.view.session.client.getSnapshot().info?.runtime_id,
    ],
    queryFn: ({ signal }) => props.view.session.client.configuration.get({ signal }),
    enabled: props.connected,
    gcTime: 0,
  });
  const [draft, setDraft] = useState<{ revision: string; model: string; provider: string }>();
  const model = draft?.model ?? configuration.data?.compact_model ?? '';
  const provider = draft?.provider ?? configuration.data?.compact_provider ?? '';
  const edit = (field: 'model' | 'provider', value: string) => {
    if (!configuration.data) return;
    setDraft({
      ...(draft ?? { revision: configuration.data.revision, model, provider }),
      [field]: value,
    });
  };
  const [offset, setOffset] = useState(0);
  const idle = !Object.keys(props.root.active_turns ?? {}).length;
  const records = query.data?.result ?? [];
  return (
    <>
      <Section
        title="Compaction"
        description="Compaction reduces model context while retaining raw transcript history. It does not undo files."
      >
        <Action
          disabled={!props.connected || !idle}
          run={() => runtime.run(props.view.session.history.compact(), 'Compact context')}
        >
          Compact now
        </Action>
        <Field
          label="Compaction model"
          description="Blank restores WHIP’s built-in compaction model. This updates shared host defaults and reloads this idle session."
        >
          <Input
            value={model}
            disabled={!configuration.data}
            onChange={(event) => edit('model', event.target.value)}
          />
        </Field>
        <Field label="Compaction provider">
          <Input
            value={provider}
            disabled={!configuration.data}
            onChange={(event) => edit('provider', event.target.value)}
          />
        </Field>
        <Action
          disabled={!props.connected || !idle || !configuration.data}
          run={async () => {
            await props.view.session.client.configuration.update({
              revision: draft?.revision ?? configuration.data!.revision,
              compact_model: model,
              compact_provider: model ? provider : '',
            });
            await runtime.run(
              props.view.session.command('session.reload', {}),
              'Reload compaction settings',
            );
            setDraft(undefined);
            await configuration.refetch();
          }}
        >
          Apply compaction defaults
        </Action>
        {configuration.error && props.connected && <ErrorNotice type="resource" owner={`${props.view.session.rootId}:configuration`} title="Could not load compaction defaults" error={configuration.error} />}
        {draft && configuration.data && draft.revision !== configuration.data.revision && (
          <p role="status">
            Host configuration changed while you were editing. Refresh these fields before applying
            them.
          </p>
        )}
        <Button
          variant="ghost"
          disabled={!props.connected}
          onClick={() => {
            setDraft(undefined);
            void configuration.refetch();
          }}
        >
          Refresh compaction defaults
        </Button>
      </Section>
      <Section title="Compaction history">
        <QueryFeedback query={query} view={props.view} />
        {records.slice(offset, offset + 16).map((record) => (
          <article key={record.seq} {...stylex.props(layout.column, layout.notice)}>
            <strong>
              Compaction {record.seq} · cutoff {record.cutoff}
            </strong>
            <CodeBlock code={record.summary} label="Summary" maxBytes={32 << 10} />
          </article>
        ))}
        {records.length > offset + 16 && (
          <Button variant="ghost" onClick={() => setOffset((value) => value + 16)}>
            Next records
          </Button>
        )}
        {offset > 0 && (
          <Button variant="ghost" onClick={() => setOffset((value) => Math.max(0, value - 16))}>
            Previous records
          </Button>
        )}
        {!records.length && !query.isLoading && <Empty>No compactions in this session.</Empty>}
        <Action
          disabled={!props.connected || !idle || !records.length}
          run={() =>
            runtime.run(
              props.view.session.command('history.compact.retry', {}),
              'Undo most recent compaction',
            )
          }
        >
          Undo latest compaction
        </Action>
      </Section>
    </>
  );
}
export function Permissions(props: InspectorProps) {
  const runtime = useRuntime();
  const query = useDetailQuery(props, 'permission.rules', {});
  const idle = !Object.keys(props.root.active_turns ?? {}).length;
  const [policy, setPolicy] = useState('client');
  const current = props.root.permission_mode === 'automatic' ? 'host' : props.root.permission_mode === 'prompt' ? 'client' : '';
  return (
    <>
      <Section
        title="Permission policy"
        description="Full Access allows files outside the project and approves actions automatically. Explicit agent limits still apply."
      >
        {current && <p {...stylex.props(layout.muted)}>Current policy: {current === 'host' ? 'Full Access' : 'Ask for approval'}</p>}
        <Field label="Use this policy">
          <Select
            label="Permission policy"
            value={policy}
            onValueChange={setPolicy}
            options={[
              { value: 'client', label: 'Ask for approval' },
              { value: 'host', label: 'Full Access' },
            ]}
          />
        </Field>
        <Action
          disabled={!props.connected || !idle}
          run={() =>
            runtime.run(
              props.view.session.command('permission.mode', {
                external_permissions: policy === 'client',
              }),
              'Change permission policy',
            )
          }
        >
          Apply policy
        </Action>
        <Action
          disabled={!props.connected || !idle}
          run={() =>
            runtime.run(
              props.view.session.command('tool.configure', { deny_permissions: true }),
              'Deny interactive tool permissions',
            )
          }
        >
          Deny interactive tool permissions
        </Action>
      </Section>
      <Section title="Saved session rules">
        <QueryFeedback view={props.view} query={query} />
        {query.data?.result?.rules?.map((rule) => (
          <div key={rule.id} {...stylex.props(layout.column, layout.notice)}>
            <strong>{rule.operation}</strong>
            <code>{rule.rule}</code>
            <span {...stylex.props(layout.muted)}>
              {rule.principal_id} · {rule.created_at}
            </span>
            <Action
              disabled={!props.connected}
              run={() =>
                runtime.run(
                  props.view.session.command('permission.forget', { id: rule.id }),
                  'Forget permission rule',
                )
              }
            >
              Forget rule
            </Action>
          </div>
        ))}
        {query.data?.result?.rules?.length === 0 && <Empty>No saved session rules.</Empty>}
      </Section>
      <Section title="Global host rules">
        {query.data?.result?.global?.map((rule, index) => (
          <code key={index}>{rule}</code>
        ))}
        {query.data?.result?.global?.length === 0 && <Empty>No global rules.</Empty>}
      </Section>
    </>
  );
}
