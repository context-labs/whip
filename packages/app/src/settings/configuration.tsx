import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useForm, useStore } from '@tanstack/react-form';
import type { WhipClient } from '@whip/sdk';
import type { ConfigurationUpdate, RuntimeConfiguration } from '@whip/protocol';
import { Button, Input, Select, Switch } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { layout } from '../styles';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';
import { useSettingsEdits } from './unsaved';

type Category = 'providers' | 'execution';
const providerFields = ['default_model', 'default_provider', 'default_effort'] as const;
const executionFields = ['compact_model', 'compact_provider', 'compact_percent', 'goal_max_rounds', 'max_retries', 'import_claude', 'import_codex'] as const;
type Values = Pick<RuntimeConfiguration, typeof providerFields[number] | typeof executionFields[number]>;
function values(config: RuntimeConfiguration): Values {
  return {
    default_model: config.default_model, default_provider: config.default_provider, default_effort: config.default_effort,
    compact_model: config.compact_model, compact_provider: config.compact_provider, compact_percent: config.compact_percent,
    goal_max_rounds: config.goal_max_rounds, max_retries: config.max_retries, import_claude: config.import_claude, import_codex: config.import_codex,
  };
}

/** Each editor may write only its owned fields, even when another category has changed. */
export function configurationPatch(category: Category, value: Values, revision: string): ConfigurationUpdate {
  if (category === 'providers') return {
    revision, default_model: value.default_model, default_provider: value.default_provider, default_effort: value.default_effort,
  };
  return {
    revision, compact_model: value.compact_model, compact_provider: value.compact_provider, compact_percent: value.compact_percent,
    goal_max_rounds: value.goal_max_rounds, max_retries: value.max_retries, import_claude: value.import_claude, import_codex: value.import_codex,
  };
}

export function ExecutionSettings({ client, enabled = client.getSnapshot().state === 'connected' }: { client: WhipClient; enabled?: boolean }) {
  return <ConfigurationSettings client={client} enabled={enabled} category="execution" />;
}

export function ConfigurationSettings({ client, enabled, category }: { client: WhipClient; enabled: boolean; category: Category }) {
  const runtimeId = client.getSnapshot().info?.runtime_id;
  const configuration = useQuery({
    queryKey: ['runtime-configuration', runtimeId],
    queryFn: ({ signal }) => client.configuration.get({ signal }),
    enabled,
  });
  const previous = useRef<{ runtimeId: string | undefined; config: RuntimeConfiguration } | undefined>(undefined);
  if (configuration.data) previous.current = { runtimeId, config: configuration.data };
  const current = configuration.data ?? (previous.current && previous.current.runtimeId === runtimeId ? previous.current.config : undefined);
  return <>
    {configuration.isPending && enabled && !current && <p role="status">Loading host defaults…</p>}
    {configuration.error && <p role="alert">{configuration.error.message}</p>}
    {current && <ConfigurationForm key={`${runtimeId}:${category}`} client={client} config={current} enabled={enabled} category={category} />}
  </>;
}

function ConfigurationForm({ client, config, enabled, category }: {
  client: WhipClient; config: RuntimeConfiguration; enabled: boolean; category: Category;
}) {
  const runtime = useRuntime();
  const [base, setBase] = useState(config);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const request = useRef<AbortController | null>(null);
  const saved = useRef(false);
  useEffect(() => () => { request.current?.abort(); request.current = null; }, [client]);
  const form = useForm({
    defaultValues: values(base),
    onSubmit: async ({ value }) => {
      if (!enabled || request.current) return;
      setError(''); setNotice('');
      const controller = new AbortController();
      request.current = controller;
      const queryKey = ['runtime-configuration', client.getSnapshot().info?.runtime_id];
      try {
        if (category === 'execution' && (!Number.isInteger(value.compact_percent) || value.compact_percent < 0 || value.compact_percent > 100
          || !Number.isInteger(value.goal_max_rounds) || value.goal_max_rounds < 0 || !Number.isInteger(value.max_retries) || value.max_retries < 0)) {
          throw new Error('Use whole numbers: compaction from 0 to 100%, and non-negative goal rounds and retries.');
        }
        const result = await client.configuration.update(configurationPatch(category, value, base.revision), { signal: controller.signal });
        if (controller.signal.aborted) return;
        runtime.queries.setQueryData(queryKey, result);
        setBase(result); form.reset(values(result)); saved.current = true;
        setNotice('Host defaults saved.');
      } catch (value) {
        if (!controller.signal.aborted) {
          setError(value instanceof Error ? value.message : String(value));
          // Refresh evidence without retrying a potentially accepted configuration write.
          void runtime.queries.invalidateQueries({ queryKey });
        }
      } finally { if (!controller.signal.aborted) request.current = null; }
    },
  });
  const dirty = useStore(form.store, state => !state.isDefaultValue);
  const submitting = useStore(form.store, state => state.isSubmitting);
  useSettingsEdits({
    id: `configuration-${category}`, dirty,
    description: category === 'providers' ? 'Default model settings have unsaved changes on this execution host.' : 'Execution defaults have unsaved changes on this execution host.',
    discard: () => { setBase(config); form.reset(values(config)); setError(''); },
    save: async () => { saved.current = false; await form.handleSubmit(); return saved.current; },
  });
  const textField = (name: 'default_model' | 'default_provider' | 'compact_model' | 'compact_provider', label: string, description: string) => <form.Field key={name} name={name}>{field =>
    <SettingRow id={name} label={label} description={description}>
      <Input aria-label={label} xstyle={settingsSection.control} disabled={!enabled || submitting} value={field.state.value}
        onBlur={field.handleBlur} onChange={event => field.handleChange(event.target.value)} />
    </SettingRow>}
  </form.Field>;
  const numberField = (name: 'compact_percent' | 'goal_max_rounds' | 'max_retries', label: string, description: string, max?: number) => <form.Field key={name} name={name}>{field =>
    <SettingRow id={name} label={label} description={description}>
      <Input aria-label={label} xstyle={settingsSection.control} type="number" min={0} max={max} step={1} disabled={!enabled || submitting}
        value={Number.isNaN(field.state.value) ? '' : field.state.value} onBlur={field.handleBlur} onChange={event => field.handleChange(event.target.valueAsNumber)} />
    </SettingRow>}
  </form.Field>;
  return <form {...stylex.props(layout.column)} onSubmit={event => { event.preventDefault(); void form.handleSubmit(); }}>
    {category === 'providers' ? <SettingsGroup title="Defaults for new work">
      {textField('default_model', 'Default model', 'Model identifier used when starting work without an explicit model.')}
      {textField('default_provider', 'Default provider', 'Provider used for the default model.')}
      <form.Field name="default_effort">{field => <SettingRow id="default_effort" label="Reasoning effort" description="The default effort for models that support it.">
        <Select label="Reasoning effort" value={field.state.value} disabled={!enabled || submitting} xstyle={settingsSection.control} onValueChange={field.handleChange}
          options={[...new Set(['', 'off', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra', field.state.value])].map(value => ({ value, label: value || 'Model default' }))} />
      </SettingRow>}</form.Field>
    </SettingsGroup> : <>
      <SettingsGroup title="Context compaction">
        {textField('compact_model', 'Compaction model', 'Model used to summarize conversation context.')}
        {textField('compact_provider', 'Compaction provider', 'Provider used for context compaction.')}
        {numberField('compact_percent', 'Compaction threshold', 'Percentage of the context window at which compaction begins.', 100)}
      </SettingsGroup>
      <SettingsGroup title="Execution limits">
        {numberField('goal_max_rounds', 'Goal round limit', 'Maximum rounds for goal-driven work.')}
        {numberField('max_retries', 'Retry limit', 'Maximum retries for a failed model request.')}
      </SettingsGroup>
      <SettingsGroup title="Configuration imports">
        <form.Field name="import_claude">{field => <SettingRow id="import_claude" label="Import Claude configuration" description="Use MCP configuration from Claude on this execution host.">
          <Switch aria-label="Import Claude configuration" checked={field.state.value} disabled={!enabled || submitting} onCheckedChange={field.handleChange} />
        </SettingRow>}</form.Field>
        <form.Field name="import_codex">{field => <SettingRow id="import_codex" label="Import Codex configuration" description="Use MCP configuration from Codex on this execution host.">
          <Switch aria-label="Import Codex configuration" checked={field.state.value} disabled={!enabled || submitting} onCheckedChange={field.handleChange} />
        </SettingRow>}</form.Field>
      </SettingsGroup>
    </>}
    <p {...stylex.props(layout.muted)}>These defaults belong to the selected execution host and are shared with its other clients.</p>
    {base.revision !== config.revision && <div role="status" {...stylex.props(layout.notice)}>
      The host configuration changed. Your edits are preserved. Review the latest defaults before saving again.
      <Button type="button" variant="secondary" disabled={submitting} onClick={() => { setBase(config); form.reset(values(config)); setError(''); setNotice(''); }}>Discard edits and load current defaults</Button>
    </div>}
    {error && <p role="alert">{error}</p>}
    {notice && <p role="status">{notice}</p>}
    <div {...stylex.props(layout.row)}><Button type="submit" disabled={!enabled || submitting || !dirty} loading={submitting}>Save host defaults</Button></div>
  </form>;
}
