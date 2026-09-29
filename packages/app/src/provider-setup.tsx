import { ErrorNotice } from './error-feedback';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { Client } from '@whip/sdk';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { CatalogModelPicker, useProviderCatalog } from './model-selection';
import { modelSettings } from './model-options';
import { SettingsGroup } from './settings/section-layout';
import { errorMessage } from './platform';
import { ProviderConnectionDialog, ProviderConnectionList, ProviderConnectionRow, type ProviderEntry, type useProviderConnections, locallyAvailable } from './settings/provider-connections';

type Connections = ReturnType<typeof useProviderConnections>;

/** A view of host-owned readiness. Choosing a default is always an explicit write. */
export function ProviderSetup({ client, enabled, hostName, connections, onReady, actions, onExpandedChange }: {
  client: Client; enabled: boolean; hostName: string; connections: Connections; onReady(): void; actions?: ReactNode; onExpandedChange?(expanded: boolean): void;
}) {
  const { inventory, flows, refresh, entries } = connections;
  const available = entries.filter(locallyAvailable);
  const [selected, setSelected] = useState<string>();
  const [connecting, setConnecting] = useState<string>();
  const [pair, setPair] = useState<{ model: string; provider: string }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => { setBusy(false); request.current = null; return () => request.current?.abort(); }, [client]);
  const candidate = entries.find(entry => entry.id === selected) ?? (!connections.ready && available.length === 1 ? available[0] : undefined);
  const model = pair && pair.provider === candidate?.id ? pair.model : candidate?.preset?.suggested_models[0] ?? '';
  const active = entries.find(entry => entry.id === connecting);
  const host = client.runtimeID;
  const catalog = useProviderCatalog(client, enabled && !!candidate, candidate?.id);
  function choose(entry: ProviderEntry) {
    setError(''); setPair(undefined);
    if (locallyAvailable(entry)) setSelected(entry.id);
    else setConnecting(entry.id);
  }
  async function useModel() {
    if (!enabled || busy || !candidate || !model || !inventory.data) return;
    const controller = new AbortController(); request.current = controller;
    setBusy(true); setError('');
    try {
      await client.setProviderDefaults({ revision: inventory.data.revision, defaults: { selection: { name: model, provider: candidate.id, effort: '' }, settings: modelSettings(catalog.data, candidate.id, model) } }, { signal: controller.signal });
      await refresh();
      if (!controller.signal.aborted) onReady();
    } catch (error) {
      if (!controller.signal.aborted) { setError(errorMessage(error)); await inventory.refetch(); }
    } finally { if (!controller.signal.aborted) setBusy(false); }
  }
  const pending = entries.filter(entry => !available.includes(entry));
  const rows = (values: ProviderEntry[]) => values.map(entry => <ProviderConnectionRow key={entry.id} setup entry={entry} hostName={hostName}
    enabled={enabled && !busy} connect={!locallyAvailable(entry)} onSelect={() => choose(entry)} />);
  const refreshButton = <Button variant="ghost" disabled={!enabled || inventory.isFetching} onClick={() => void refresh()}>Refresh</Button>;
  return <section aria-label="Provider setup" {...stylex.props(styles.panel)}>
    {inventory.isPending && <p role="status">Checking providers on {hostName}…</p>}
    {inventory.error && enabled && <ErrorNotice type="resource" owner={`${host}:providers`} title="Could not load providers" error={inventory.error} action={refreshButton} />}

    {!!available.length && <SettingsGroup title="Configured credentials" panelXstyle={styles.providerPanel}>{rows(available)}</SettingsGroup>}
    {candidate && <div {...stylex.props(styles.confirmation)}>
      <p {...stylex.props(styles.note)}>Default for new sessions on {hostName}. Your first message makes the request; no inference has been tested.</p>
      <CatalogModelPicker label="Change model" settings model={model} provider={candidate.id} catalog={catalog.data}
        loading={catalog.isFetching} error={enabled ? catalog.error?.message : undefined} disabled={!enabled || busy}
        onChange={(model, provider) => { setSelected(provider); setPair({ model, provider }); }} />
      {!model && <p role="status" {...stylex.props(styles.note)}>Choose a model for {candidate.name}.</p>}
      {catalog.error && <Button variant="ghost" disabled={!enabled || busy} onClick={() => void catalog.refetch()}>Retry cached models</Button>}
      <Button data-provider-confirm variant="primary" disabled={!enabled || busy || !model} loading={busy} onClick={() => void useModel()}>Use {model || 'selected model'}</Button>
    </div>}
    <ProviderConnectionList key={host} entries={pending} enabled={enabled && !busy} hostName={hostName} onSelect={choose}
      title={available.length ? 'Connect another provider' : undefined} actions={actions} refresh={refreshButton} onExpandedChange={onExpandedChange} />
    {!inventory.isPending && !inventory.error && !entries.length && <p role="status" {...stylex.props(styles.note)}>No providers are available on this host. Try refreshing or connect a remote host.</p>}
    {flows.error && enabled && <ErrorNotice type="resource" owner={`${host}:sign-ins`} title="Could not load sign-in progress" error={flows.error} />}
    {error && <ErrorNotice type="action" owner={`${host}:default-model`} title="Could not save the default model" error={error} />}
    {active && inventory.data && <ProviderConnectionDialog key={active.id} client={client} entry={active} enabled={enabled}
      revision={inventory.data.revision} hostName={hostName} flows={flows.data ?? []}
      refresh={refresh} refreshFlows={async () => { await flows.refetch(); }} close={() => setConnecting(undefined)}
      onConnected={() => { setSelected(active.id); setConnecting(undefined); void refresh(); }} />}
  </section>;
}

const styles = stylex.create({
  panel: { display: 'flex', flexDirection: 'column', width: '100%', gap: scale.space2, textAlign: 'start' },
  providerPanel: { padding: scale.space3, gap: scale.space2, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder },
  note: { fontSize: typography.size12, color: surface.secondaryText, margin: 0, lineHeight: 1.5, overflowWrap: 'anywhere' },
  confirmation: { display: 'flex', flexDirection: 'column', gap: scale.space3, padding: scale.space4, borderRadius: scale.radiusControl, backgroundColor: colors.panel },
});
