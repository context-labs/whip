import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useForm, useStore } from '@tanstack/react-form';
import type { Client, HostExecutionDefaults } from '@whip/sdk';
import { Button, Input, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { ErrorNotice } from '../error-feedback';
import { layout } from '../styles';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';
import { useSettingsEdits } from './unsaved';
import { ProviderDefaultsSettings } from './provider-defaults';
import { ExternalBrowserSettings } from './external-browser';

type Values = Omit<HostExecutionDefaults, 'revision'>;
function values({ revision: _revision, ...value }: HostExecutionDefaults): Values { return value; }
export function ExecutionSettings({ client, enabled = true }: { client: Client; enabled?: boolean }) {
  const query = useQuery({ queryKey: ['host-defaults', client.runtimeID], queryFn: ({ signal }) => client.hosts.executionDefaults({ signal }), enabled });
  return <><ErrorNotice type="resource" owner={`${client.runtimeID}:execution-defaults`} title="Could not load execution defaults" error={query.error} />
    {query.isPending && <p role="status">Loading host defaults…</p>}
    {query.data && <ExecutionForm key={client.runtimeID} client={client} enabled={enabled} current={query.data} />}
    <ProviderDefaultsSettings client={client} enabled={enabled} compaction /><ExternalBrowserSettings client={client} enabled={enabled}/></>;
}
function ExecutionForm({ client, enabled, current }: { client: Client; enabled: boolean; current: HostExecutionDefaults }) {
  const runtime = useRuntime();
  const [base, setBase] = useState(current);
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useState('');
  const request = useRef<AbortController | null>(null);
  const saved = useRef(false);
  useEffect(() => () => { request.current?.abort(); request.current = null; }, [client]);
  const form = useForm({ defaultValues: values(base), onSubmit: async ({ value }) => {
    if (!enabled || request.current || base.revision !== current.revision) return;
    const controller = new AbortController(); request.current = controller; setError(undefined); setNotice('');
    try {
      if (!/^(0|[1-9][0-9]{0,18})$/.test(value.goal_max_continuations) || BigInt(value.goal_max_continuations) > 9223372036854775807n) throw new Error('Use a non-negative decimal continuation count within the host limit');
      if (!Number.isInteger(value.compaction_percent) || value.compaction_percent < 0 || value.compaction_percent > 100 || !Number.isInteger(value.max_attempts) || value.max_attempts < 1 || value.max_attempts > 5) throw new Error('Use whole numbers: compaction 0–100 and attempts 1–5');
      const result = await client.hosts.setExecutionDefaults(base.revision, value, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setBase(result); form.reset(values(result)); saved.current = true; setNotice('Host defaults saved. Existing sessions keep their captured configuration.');
      runtime.queries.setQueryData(['host-defaults', client.runtimeID], result);
      await runtime.queries.invalidateQueries({ predicate: query => query.queryKey.includes(client.runtimeID) && query.queryKey[0] !== 'host-defaults' });
    } catch (error) { if (!controller.signal.aborted) { setError(error); await runtime.queries.invalidateQueries({ queryKey: ['host-defaults', client.runtimeID] }); } }
    finally { if (request.current === controller) request.current = null; }
  } });
  const dirty = useStore(form.store, state => !state.isDefaultValue), busy = useStore(form.store, state => state.isSubmitting);
  const reload = () => { setBase(current); form.reset(values(current)); setError(undefined); setNotice(''); };
  useSettingsEdits({ id: 'execution-defaults', dirty, description: 'Unsaved execution defaults on this host.', discard: reload, save: async () => { saved.current = false; await form.handleSubmit(); return saved.current; } });
  return <form {...stylex.props(layout.column)} onSubmit={event => { event.preventDefault(); void form.handleSubmit(); }}>
    <SettingsGroup title="Defaults for future work">
      <form.Field name="engine">{field => <SettingRow id="default_execution_engine" label="Execution language" description="Future roots use this engine; their children inherit it. Existing trees keep their engine."><Select label="Execution language" value={field.state.value} disabled={!enabled || busy} onValueChange={value => { if (value === 'starlark' || value === 'quickjs') field.handleChange(value); }} options={[{ value: 'starlark', label: 'Starlark' }, { value: 'quickjs', label: 'JavaScript' }]} /></SettingRow>}</form.Field>
      <form.Field name="effort">{field => <SettingRow id="execution_effort" label="Default reasoning effort" description="Saved with the default model. Empty uses the provider default; a nonempty value requires a selected default model."><Input aria-label="Default reasoning effort" value={field.state.value} disabled={!enabled || busy} onChange={event => field.handleChange(event.target.value)} /></SettingRow>}</form.Field>
      <form.Field name="compaction_percent">{field => <SettingRow id="compact_percent" label="Compaction threshold" description="Percentage of the context window. Zero uses the host default of 50%."><Input aria-label="Compaction threshold" type="number" min={0} max={100} step={1} value={Number.isNaN(field.state.value) ? '' : field.state.value} disabled={!enabled || busy} onChange={event => field.handleChange(event.target.valueAsNumber)} /></SettingRow>}</form.Field>
      <form.Field name="goal_max_continuations">{field => <SettingRow id="goal_max_rounds" label="Additional goal continuations" description="Automatic continuations after the initial attempt. Zero disables additional continuations."><Input aria-label="Additional goal continuations" inputMode="numeric" value={field.state.value} disabled={!enabled || busy} onChange={event => field.handleChange(event.target.value)} /></SettingRow>}</form.Field>
      <form.Field name="max_attempts">{field => <SettingRow id="max_retries" label="Maximum attempts" description="Includes the initial model request, from 1 to 5. Explicit per-model settings take precedence. Uncertain output is not automatically regenerated."><Input aria-label="Maximum attempts" type="number" min={1} max={5} step={1} value={Number.isNaN(field.state.value) ? '' : field.state.value} disabled={!enabled || busy} onChange={event => field.handleChange(event.target.valueAsNumber)} xstyle={settingsSection.control} /></SettingRow>}</form.Field>
    </SettingsGroup>
    {base.revision !== current.revision && <p role="status">Host settings changed. Your edits are preserved. <Button disabled={busy} onClick={reload}>Discard edits and load current defaults</Button></p>}
    <ErrorNotice type="action" owner={`${client.runtimeID}:execution-defaults`} title="Could not save execution defaults" error={error} />{notice && <p role="status">{notice}</p>}
    <Button type="submit" disabled={!enabled || busy || !dirty || base.revision !== current.revision}>Save execution defaults</Button>
  </form>;
}
