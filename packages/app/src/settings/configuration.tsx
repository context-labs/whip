import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useForm, useStore } from '@tanstack/react-form';
import type { Client, HostExecutionDefaults } from '@whip/sdk';
import { Button, Input, Select, Switch } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { ErrorNotice } from '../error-feedback';
import { CompactionSettings } from '../compaction-settings';
import { layout } from '../styles';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';
import { useSettingsEdits } from './unsaved';
import { ExternalBrowserSettings } from './external-browser';

type Values = HostExecutionDefaults['preferences'];
function values(current: HostExecutionDefaults): Values { return current.preferences; }
export function ExecutionSettings({ client, enabled = true }: { client: Client; enabled?: boolean }) {
  const query = useQuery({ queryKey: ['host-defaults', client.runtimeID], queryFn: ({ signal }) => client.hosts.executionDefaults({ signal }), enabled });
  const owner = `${client.runtimeID}:${client.processEpoch}`;
  const previous = useRef<{ owner: string; current: HostExecutionDefaults } | undefined>(undefined);
  if (query.data) previous.current = { owner, current: query.data };
  const current = query.data ?? (previous.current?.owner === owner ? previous.current.current : undefined);
  return <><ErrorNotice type="resource" owner={`${client.runtimeID}:execution-defaults`} title="Could not load execution defaults" error={query.error} />
    {query.isPending && enabled && !current && <p role="status">Loading host defaults…</p>}
    {current && <ExecutionForm key={owner} client={client} enabled={enabled && !!query.data} current={current} />}
    <ExternalBrowserSettings client={client} enabled={enabled}/></>;
}
function ExecutionForm({ client, enabled, current }: { client: Client; enabled: boolean; current: HostExecutionDefaults }) {
  const runtime = useRuntime();
  const [base, setBase] = useState(current);
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useState('');
  const request = useRef<AbortController | null>(null);
  const saved = useRef(false);
  useEffect(() => () => request.current?.abort(), [client]);
  const form = useForm({ defaultValues: values(base), onSubmit: async ({ value }) => {
    if (!enabled || request.current || base.revision !== current.revision) return;
    const controller = new AbortController(); request.current = controller; setError(undefined); setNotice('');
    try {
      if (value.goal_max_continuations !== null && (!/^(0|[1-9][0-9]{0,18})$/.test(value.goal_max_continuations) || BigInt(value.goal_max_continuations) > 9223372036854775807n)) throw new Error('Use a non-negative whole number for the goal round limit.');
      if (!Number.isSafeInteger(value.max_attempts) || value.max_attempts < 0) throw new Error('Use a non-negative whole number for the retry limit.');
      const result = await client.hosts.setExecutionPreferences(base.revision, value, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setBase(result); form.reset(values(result)); saved.current = true; setNotice('Host defaults saved.');
      runtime.queries.setQueryData(['host-defaults', client.runtimeID], result);
      await runtime.queries.invalidateQueries({ predicate: query => query.queryKey.includes(client.runtimeID) && query.queryKey[0] !== 'host-defaults' });
    } catch (error) { if (!controller.signal.aborted) { setError(error); await runtime.queries.invalidateQueries({ queryKey: ['host-defaults', client.runtimeID] }); } }
    finally { if (request.current === controller) request.current = null; }
  } });
  const draft = useStore(form.store, state => state.values);
  const dirty = useStore(form.store, state => !state.isDefaultValue), busy = useStore(form.store, state => state.isSubmitting);
  useEffect(() => {
    if (!dirty && !busy && error === undefined && base.revision !== current.revision) { setBase(current); form.reset(values(current)); }
  }, [base.revision, current, dirty, busy, error, form]);
  const reload = () => { setBase(current); form.reset(values(current)); setError(undefined); setNotice(''); };
  useSettingsEdits({ id: 'configuration-execution', dirty, description: 'Execution defaults have unsaved changes on this execution host.', discard: reload, save: async () => { saved.current = false; await form.handleSubmit(); return saved.current; } });
  return <form noValidate {...stylex.props(layout.column)} onSubmit={event => { event.preventDefault(); void form.handleSubmit(); }}>
    <SettingsGroup title="Defaults for new sessions">
      <form.Field name="engine">{field => <SettingRow id="default_execution_engine" label="Execution language" description="Used for future sessions and all their child agents. Existing sessions keep their language.">
        <Select label="Execution language" value={field.state.value} disabled={!enabled || busy} xstyle={settingsSection.control} onValueChange={value => { if (value === 'starlark' || value === 'quickjs') field.handleChange(value); }} options={[{ value: 'starlark', label: 'Starlark' }, { value: 'quickjs', label: 'JavaScript' }]} /></SettingRow>}</form.Field>
    </SettingsGroup>
    <SettingsGroup title="Context compaction"><CompactionSettings client={client} value={draft} base={base.preferences} disabled={!enabled || busy} onChange={next => { form.setFieldValue('compaction_model', next.compaction_model); form.setFieldValue('compaction_percent', next.compaction_percent); }} /></SettingsGroup>
    <SettingsGroup title="Execution limits">
      <form.Field name="goal_max_continuations">{field => <SettingRow id="goal_max_rounds" label="Goal round limit" description={field.state.value === '0' ? 'Additional goal continuations are disabled on this host.' : 'Maximum additional rounds for goal-driven work. 0 uses the default of 100.'}>
        {field.state.value === '0' ? <Button disabled={!enabled || busy} onClick={() => field.handleChange(null)}>Use default goal limit</Button> : <Input aria-label="Goal round limit" inputMode="numeric" value={field.state.value ?? '0'} disabled={!enabled || busy} onBlur={field.handleBlur} onChange={event => field.handleChange(event.target.value === '0' ? null : event.target.value)} xstyle={settingsSection.control} />}
      </SettingRow>}</form.Field>
      <form.Field name="max_attempts">{field => <SettingRow id="max_retries" label="Retry limit" description="Total attempts for a failed model request. 0 uses the default of 3; 1 disables retries."><Input aria-label="Retry limit" type="number" min={0} max={Number.MAX_SAFE_INTEGER} step={1} value={Number.isNaN(field.state.value) ? '' : field.state.value} disabled={!enabled || busy} onBlur={field.handleBlur} onChange={event => field.handleChange(event.target.valueAsNumber)} xstyle={settingsSection.control} /></SettingRow>}</form.Field>
    </SettingsGroup>
    <SettingsGroup title="Configuration imports">
      {(['claude', 'codex'] as const).map(source => <form.Field key={source} name={source === 'claude' ? 'import_claude' : 'import_codex'}>{field => <SettingRow id={`import_${source}`} label={`Import ${source === 'claude' ? 'Claude' : 'Codex'} configuration`} description={`Include ${source === 'claude' ? 'Claude' : 'Codex'} MCP configuration when reviewing imports on this execution host.`}>
        <Switch aria-label={`Import ${source === 'claude' ? 'Claude' : 'Codex'} configuration`} checked={field.state.value} disabled={!enabled || busy} onCheckedChange={field.handleChange} /></SettingRow>}</form.Field>)}
    </SettingsGroup>
    <p {...stylex.props(layout.muted)}>These defaults belong to the selected execution host and are shared with its other clients.</p>
    {base.revision !== current.revision && <div role="status" {...stylex.props(layout.notice)}>The host configuration changed. Your edits are preserved. Review the latest defaults before saving again. <Button type="button" variant="secondary" disabled={busy} onClick={reload}>Discard edits and load current defaults</Button></div>}
    <ErrorNotice type="action" owner={`${client.runtimeID}:configuration-execution`} title="Could not save host defaults" error={error} />{notice && <p role="status">{notice}</p>}
    <div {...stylex.props(layout.row)}><Button type="submit" loading={busy} disabled={!enabled || busy || !dirty || base.revision !== current.revision}>Save host defaults</Button></div>
  </form>;
}
