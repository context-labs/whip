import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Client, DefaultPermissionMode, ProviderInventory } from '@whip/sdk';
import { Button, Input, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { CatalogModelPicker, catalogModels, effortLabel, modelEfforts, useProviderCatalog } from '../model-selection';
import { modelSettings, type ModelSelection } from '../model-options';
import { useRuntime } from '../context';
import { ErrorNotice } from '../error-feedback';
import { layout } from '../styles';
import { SettingsGroup, SettingRow } from './section-layout';
import { useSettingsEdits } from './unsaved';

export function ProviderDefaultsSettings({ client, enabled, compaction = false }: { client: Client; enabled: boolean; compaction?: boolean }) {
  const inventory = useQuery({ queryKey: ['provider-list', client.runtimeID], queryFn: ({ signal }) => client.listProviders({ signal }), enabled });
  return <><ErrorNotice type="resource" owner={`${client.runtimeID}:defaults`} title="Could not load model defaults" error={inventory.error} />
    {inventory.data && <ModelDefaults key={`${client.runtimeID}:${compaction}`} client={client} enabled={enabled} current={inventory.data} compaction={compaction} />}
    {!compaction && <PermissionDefault client={client} enabled={enabled} />}</>;
}
function ModelDefaults({ client, enabled, current, compaction }: { client: Client; enabled: boolean; current: ProviderInventory; compaction: boolean }) {
  const runtime = useRuntime();
  const [base, setBase] = useState(current);
  const [selection, setSelection] = useState<ModelSelection | null>(compaction ? current.compaction_model : current.defaults);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useState('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => { setBusy(false); request.current = null; return () => request.current?.abort(); }, [client]);
  const catalog = useProviderCatalog(client, enabled);
  const dirty = JSON.stringify(selection) !== JSON.stringify(compaction ? base.compaction_model : base.defaults);
  const reload = () => { setBase(current); setSelection(compaction ? current.compaction_model : current.defaults); setError(undefined); setNotice(''); };
  async function save() {
    if (!enabled || request.current || base.revision !== current.revision) return false;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError(undefined);
    try {
      const params = { revision: base.revision, defaults: { selection, settings: selection ? modelSettings(catalog.data, selection.provider, selection.name) : null } };
      const result = compaction ? await client.setProviderCompactionModel(params, { signal: controller.signal }) : await client.setProviderDefaults(params, { signal: controller.signal });
      if (controller.signal.aborted) return false;
      setBase(result); setSelection(compaction ? result.compaction_model : result.defaults); runtime.queries.setQueryData(['provider-list', client.runtimeID], result);
      await runtime.queries.invalidateQueries({ predicate: query => query.queryKey.includes(client.runtimeID) && ['provider-readiness', 'provider-catalogs', 'permission-default', 'host-defaults'].includes(String(query.queryKey[0])) });
      setNotice('Host defaults saved. Existing sessions are unchanged.'); return true;
    } catch (error) { if (!controller.signal.aborted) { setError(error); await runtime.queries.invalidateQueries({ queryKey: ['provider-list', client.runtimeID] }); } return false; }
    finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setBusy(false); }
  }
  useSettingsEdits({ id: compaction ? 'compaction-model' : 'default-model', dirty, description: 'Unsaved model defaults on this execution host.', discard: reload, save });
  const efforts = modelEfforts(catalogModels(catalog.data, selection?.provider ?? ''), selection?.name ?? '');
  return <form {...stylex.props(layout.column)} onSubmit={event => { event.preventDefault(); void save(); }}>
    <SettingsGroup title={compaction ? 'Compaction model' : 'Defaults for new work'}>
      <SettingRow id={compaction ? 'compact_model' : 'default_model'} label={compaction ? 'Compaction model' : 'Default model'} description="Only an explicit save changes the host default. Model information does not establish inference access.">
        <CatalogModelPicker label={compaction ? 'Compaction model' : 'Default model'} settings model={selection?.name ?? ''} provider={selection?.provider ?? ''} catalog={catalog.data} loading={catalog.isPending} error={catalog.error?.message} disabled={!enabled || busy} onChange={(name, provider) => setSelection({ name, provider, effort: '' })} />
      </SettingRow>
      <SettingRow id="exact_model" label="Exact model ID"><Input aria-label="Exact model ID" value={selection?.name ?? ''} disabled={!enabled || busy} onChange={event => setSelection({ name: event.target.value, provider: selection?.provider ?? '', effort: '' })} /></SettingRow>
      <SettingRow id="exact_provider" label="Provider"><Input aria-label="Provider" value={selection?.provider ?? ''} disabled={!enabled || busy} onChange={event => setSelection({ name: selection?.name ?? '', provider: event.target.value, effort: '' })} /></SettingRow>
      <SettingRow id="default_effort" label="Reasoning effort" description="Unknown models can use the provider default."><Select label="Reasoning effort" value={selection?.effort || 'off'} disabled={!enabled || busy || !selection} onValueChange={effort => { if (selection) setSelection({ ...selection, effort: effort === 'off' ? '' : effort }); }} options={[...efforts.map(value => ({ value, label: effortLabel(value) })), ...(selection?.effort && !efforts.includes(selection.effort) ? [{ value: selection.effort, label: `${effortLabel(selection.effort)} (saved)` }] : [])]} /></SettingRow>
    </SettingsGroup>
    {base.revision !== current.revision && <p role="status">Host settings changed. Your edits are preserved. <Button disabled={busy} onClick={reload}>Discard edits and load current defaults</Button></p>}
    <ErrorNotice type="action" owner={`${client.runtimeID}:default-model`} title="Could not save host defaults" error={error} />
    {notice && <p role="status">{notice}</p>}
    <div {...stylex.props(layout.row)}><Button type="submit" disabled={!enabled || busy || !dirty || base.revision !== current.revision || !!selection && (!selection.name || !selection.provider)}>Save model default</Button><Button disabled={!enabled || busy} onClick={() => setSelection(null)}>Clear model default</Button></div>
  </form>;
}
function PermissionDefault({ client, enabled }: { client: Client; enabled: boolean }) {
  const result = useQuery({ queryKey: ['permission-default', client.runtimeID], queryFn: ({ signal }) => client.getDefaultPermissionMode({ signal }), enabled });
  return <><ErrorNotice type="resource" owner={`${client.runtimeID}:permission-default`} title="Could not load permission default" error={result.error} />{result.data && <PermissionDefaultForm key={client.runtimeID} client={client} enabled={enabled} current={result.data} refresh={async () => { await result.refetch(); }} />}</>;
}
function PermissionDefaultForm({ client, enabled, current, refresh }: { client: Client; enabled: boolean; current: DefaultPermissionMode; refresh(): Promise<void> }) {
  const [base, setBase] = useState(current);
  const [mode, setMode] = useState(current.mode);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const request = useRef<AbortController | null>(null);
  useEffect(() => { setBusy(false); request.current = null; return () => request.current?.abort(); }, [client]);
  const reload = () => { setBase(current); setMode(current.mode); setError(undefined); };
  async function save() {
    if (!enabled || request.current || base.revision !== current.revision) return false;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError(undefined);
    try { const result = await client.setDefaultPermissionMode({ expected_revision: base.revision, mode }, { signal: controller.signal }); if (controller.signal.aborted) return false; setBase(result); setMode(result.mode); await refresh(); return true; }
    catch (error) { if (!controller.signal.aborted) { setError(error); await refresh(); } return false; }
    finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setBusy(false); }
  }
  useSettingsEdits({ id: 'permission-default', dirty: mode !== base.mode, description: 'Unsaved default permission level.', discard: reload, save });
  return <SettingsGroup title="Default permission level"><SettingRow id="default_permission_mode" label="Permission level" description="Applies to future roots. Child tools still require scoped delegation."><Select label="Default permission level" value={mode} disabled={!enabled || busy} options={[{ value: 'prompt', label: 'Ask' }, { value: 'automatic', label: 'Full Access' }]} onValueChange={value => { if (value === 'prompt' || value === 'automatic') setMode(value); }} /></SettingRow>
    {base.revision !== current.revision && <p role="status">Host settings changed. <Button onClick={reload}>Discard permission edit and load current default</Button></p>}
    <ErrorNotice type="action" owner={`${client.runtimeID}:permission-default`} title="Could not save permission default" error={error} /><Button disabled={!enabled || busy || mode === base.mode || base.revision !== current.revision} onClick={() => void save()}>Save permission default</Button></SettingsGroup>;
}
