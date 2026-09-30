import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Client, ProviderInventory } from '@whip/sdk';
import { Button, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { CatalogModelPicker, catalogModels, effortLabel, modelEfforts, useProviderCatalog } from '../model-selection';
import { modelSettings, type ModelSelection } from '../model-options';
import { PermissionModeControl } from '../permission-mode';
import { useRuntime } from '../context';
import { ErrorNotice } from '../error-feedback';
import { layout } from '../styles';
import { SettingsGroup, SettingRow, settingsSection } from './section-layout';
import { useSettingsEdits } from './unsaved';

/** The reference Providers category has one draft and one atomic Save. */
export function ProviderDefaultsSettings({ client, enabled }: { client: Client; enabled: boolean }) {
  const inventory = useQuery({ queryKey: ['provider-list', client.runtimeID], queryFn: ({ signal }) => client.listProviders({ signal }), enabled });
  const owner = `${client.runtimeID}:${client.processEpoch}`;
  const previous = useRef<{ owner: string; current: ProviderInventory } | undefined>(undefined);
  if (inventory.data) previous.current = { owner, current: inventory.data };
  const current = inventory.data ?? (previous.current?.owner === owner ? previous.current.current : undefined);
  return <><ErrorNotice type="resource" owner={`${client.runtimeID}:defaults`} title="Could not load model defaults" error={inventory.error} />
    {current && <ProviderDefaultsForm key={owner} client={client} enabled={enabled && !!inventory.data} current={current} />}</>;
}
function ProviderDefaultsForm({ client, enabled, current }: { client: Client; enabled: boolean; current: ProviderInventory }) {
  const runtime = useRuntime();
  const [base, setBase] = useState(current);
  const [selection, setSelection] = useState<ModelSelection | null>(current.defaults);
  const [mode, setMode] = useState(current.permission_mode);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useState('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), [client]);
  const catalog = useProviderCatalog(client, enabled);
  const dirty = JSON.stringify(selection) !== JSON.stringify(base.defaults) || mode !== base.permission_mode;
  useEffect(() => {
    if (!dirty && !busy && error === undefined && base.revision !== current.revision) { setBase(current); setSelection(current.defaults); setMode(current.permission_mode); }
  }, [base.revision, current, dirty, busy, error]);
  const reload = () => { setBase(current); setSelection(current.defaults); setMode(current.permission_mode); setError(undefined); setNotice(''); };
  async function save() {
    if (!enabled || request.current || base.revision !== current.revision) return false;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError(undefined);
    try {
      const result = await client.setProviderPreferences({ revision: base.revision, defaults: { selection, settings: selection ? modelSettings(catalog.data, selection.provider, selection.name) : null }, permission_mode: mode }, { signal: controller.signal });
      if (controller.signal.aborted) return false;
      setBase(result); setSelection(result.defaults); setMode(result.permission_mode);
      runtime.queries.setQueryData(['provider-list', client.runtimeID], result);
      await runtime.queries.invalidateQueries({ predicate: query => query.queryKey.includes(client.runtimeID) && query.queryKey[0] !== 'provider-list' });
      setNotice('Host defaults saved.'); return true;
    } catch (error) { if (!controller.signal.aborted) { setError(error); await runtime.queries.invalidateQueries({ queryKey: ['provider-list', client.runtimeID] }); } return false; }
    finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setBusy(false); }
  }
  useSettingsEdits({ id: 'configuration-providers', dirty, description: 'Default model settings have unsaved changes on this execution host.', discard: reload, save });
  const efforts = modelEfforts(catalogModels(catalog.data, selection?.provider ?? ''), selection?.name ?? '');
  const effort = selection?.effort || 'default';
  return <form {...stylex.props(layout.column)} onSubmit={event => { event.preventDefault(); void save(); }}>
    <SettingsGroup title="Defaults for new work">
      <SettingRow id="default_model" label="Default model" description="Choose the model and provider for new work.">
        <CatalogModelPicker label="Default model" settings model={selection?.name ?? ''} provider={selection?.provider ?? ''} catalog={catalog.data}
          loading={catalog.isPending} error={enabled ? catalog.error?.message : undefined} disabled={!enabled || busy} xstyle={settingsSection.control}
          onChange={(name, provider) => setSelection({ name, provider, effort: modelEfforts(catalogModels(catalog.data, provider), name).includes(effort) ? selection?.effort ?? '' : '' })} />
      </SettingRow>
      <SettingRow id="default_effort" label="Reasoning effort" description="Available levels depend on the selected model and provider.">
        <Select label="Reasoning effort" value={effort} disabled={!enabled || busy || !selection} xstyle={settingsSection.control}
          onValueChange={value => { if (selection) setSelection({ ...selection, effort: value === 'default' ? '' : value }); }}
          options={[...efforts.map(value => ({ value, label: effortLabel(value) })), ...(!efforts.includes(effort) ? [{ value: effort, label: `${effortLabel(effort)} (saved)`, disabled: true }] : [])]} />
      </SettingRow>
      <SettingRow id="default_permission_mode" label="Default permission level" description="Choose how permissions are approved for new sessions.">
        <PermissionModeControl presentation="settings" label="Default permission level" value={mode} disabled={!enabled || busy} onChange={value => { if (value === 'prompt' || value === 'automatic') setMode(value); }} />
      </SettingRow>
    </SettingsGroup>
    <p {...stylex.props(layout.muted)}>These defaults belong to the selected execution host and are shared with its other clients.</p>
    {base.revision !== current.revision && <div role="status" {...stylex.props(layout.notice)}>The host configuration changed. Your edits are preserved. Review the latest defaults before saving again.
      <Button type="button" variant="secondary" disabled={busy} onClick={reload}>Discard edits and load current defaults</Button></div>}
    <ErrorNotice type="action" owner={`${client.runtimeID}:configuration-providers`} title="Could not save host defaults" error={error} />
    {notice && <p role="status">{notice}</p>}
    <div {...stylex.props(layout.row)}><Button type="submit" loading={busy} disabled={!enabled || busy || !dirty || base.revision !== current.revision}>Save host defaults</Button></div>
  </form>;
}
