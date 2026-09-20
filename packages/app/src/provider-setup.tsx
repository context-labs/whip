import { ErrorNotice } from './error-feedback';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import { Alert, Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { CatalogModelPicker } from './model-selection';
import { SettingsGroup } from './settings/section-layout';
import { errorMessage } from './platform';
import { ProviderConnectionDialog, ProviderConnectionList, ProviderConnectionRow, type ProviderEntry, type useProviderConnections } from './settings/provider-connections';

type Connections = ReturnType<typeof useProviderConnections>;

/** A view of host-owned readiness. Choosing a default is always an explicit write. */
export function ProviderSetup({ client, enabled, hostName, connections, onReady, actions }: {
  client: WhipClient; enabled: boolean; hostName: string; connections: Connections; onReady(): void; actions?: ReactNode;
}) {
  const { inventory, flows, refresh, discover, discovering, discoveryError, persistenceError } = connections;
  useEffect(() => { void discover(); }, [discover]);
  const entries = inventory.data?.providers ?? [];
  const available = entries.filter(entry => entry.status.available && !entry.status.disabled);
  const [selected, setSelected] = useState<string>();
  const [connecting, setConnecting] = useState<string>();
  const [pair, setPair] = useState<{ model: string; provider: string }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), [client]);
  const candidate = entries.find(entry => entry.id === selected) ?? (!inventory.data?.selection?.ready && available.length === 1 ? available[0] : undefined);
  const model = pair && pair.provider === candidate?.id ? pair.model : candidate?.suggested_model ?? '';
  const active = entries.find(entry => entry.id === connecting);
  const host = client.getSnapshot().info?.runtime_id;
  const catalog = useQuery({
    queryKey: ['provider-catalogs', host, candidate?.id],
    queryFn: ({ signal }) => client.providers.catalogs({ provider: candidate?.id, signal }),
    enabled: enabled && !!inventory.data?.selection && !!candidate,
  });
  // Catalog discovery may supply the host's recommendation; it never selects it.
  useEffect(() => { if (catalog.data) void inventory.refetch(); }, [catalog.data]);
  function choose(entry: ProviderEntry) {
    setError(''); setPair(undefined);
    if ((entry.status.available || entry.status.auth_state === 'unchecked') && !entry.status.disabled) setSelected(entry.id);
    else setConnecting(entry.id);
  }
  async function useModel() {
    if (!enabled || busy || !candidate || !model || !inventory.data) return;
    const controller = new AbortController(); request.current = controller;
    setBusy(true); setError('');
    try {
      await client.configuration.update({ revision: inventory.data.revision, default_model: model, default_provider: candidate.id,
        ...(inventory.data.selection?.model !== model || inventory.data.selection.provider !== candidate.id ? { default_effort: '' } : {}) }, { signal: controller.signal });
      await refresh();
      if (!controller.signal.aborted) onReady();
    } catch (error) {
      if (!controller.signal.aborted) { setError(errorMessage(error)); await inventory.refetch(); }
    } finally { if (!controller.signal.aborted) setBusy(false); }
  }
  const pending = entries.filter(entry => !available.includes(entry));
  const rows = (values: ProviderEntry[]) => values.map(entry => <ProviderConnectionRow key={entry.id} setup entry={entry} hostName={hostName}
    enabled={enabled && !busy} connect={!((entry.status.available || entry.status.auth_state === 'unchecked') && !entry.status.disabled)} onSelect={() => choose(entry)} />);
  const refreshButton = <Button variant="ghost" disabled={!enabled || inventory.isFetching || discovering} onClick={() => void discover()}>Refresh</Button>;
  if (inventory.data && !inventory.data.selection) return <section aria-label="Provider setup" {...stylex.props(styles.panel)}>
    <Alert tone="warning">Update Whip on {hostName} to use provider setup. Your draft is preserved.</Alert>
    <Button variant="ghost" disabled={!enabled || inventory.isFetching || discovering} onClick={() => void discover()}>Check again</Button>
    {actions}
  </section>;
  return <section aria-label="Provider setup" {...stylex.props(styles.panel)}>
    {inventory.isPending && <p role="status">Checking providers on {hostName}…</p>}
    {inventory.error && enabled && <ErrorNotice type="resource" owner={`${host}:providers`} title="Could not load providers" error={inventory.error} action={refreshButton} />}
    {enabled && (discoveryError || persistenceError || inventory.data?.discovery_error) && <ErrorNotice type="resource" owner={`${host}:provider-discovery`} title="Could not discover providers" error={discoveryError || persistenceError || inventory.data?.discovery_error} action={refreshButton} />}
    {!!available.length && <SettingsGroup title="Already available" panelXstyle={styles.providerPanel}>{rows(available)}</SettingsGroup>}
    {candidate && <div {...stylex.props(styles.confirmation)}>
      <p {...stylex.props(styles.note)}>Default for new sessions on {hostName}. Your first message makes the request.</p>
      <CatalogModelPicker label="Change model" settings model={model} provider={candidate.id} catalog={catalog.data?.result}
        loading={catalog.isFetching} error={enabled ? catalog.error?.message : undefined} disabled={!enabled || busy}
        onChange={(model, provider) => { setSelected(provider); setPair({ model, provider }); }} />
      {!model && <p role="status" {...stylex.props(styles.note)}>Choose a model for {candidate.name}.</p>}
      {catalog.error && <Button variant="ghost" disabled={!enabled || busy} onClick={() => void catalog.refetch()}>Retry model discovery</Button>}
      <Button data-provider-confirm variant="primary" disabled={!enabled || busy || !model} loading={busy} onClick={() => void useModel()}>Use {model || 'selected model'}</Button>
    </div>}
    <ProviderConnectionList key={host} entries={pending} enabled={enabled && !busy} hostName={hostName} onSelect={choose}
      title={available.length ? 'Connect another provider' : undefined} actions={actions} refresh={refreshButton} />
    {!inventory.isPending && !inventory.error && !entries.length && <p role="status" {...stylex.props(styles.note)}>No providers are available on this host. Try refreshing or connect a remote host.</p>}
    {flows.error && enabled && <ErrorNotice type="resource" owner={`${host}:sign-ins`} title="Could not load sign-in progress" error={flows.error} />}
    {error && <ErrorNotice type="action" owner={`${host}:default-model`} title="Could not save the default model" error={error} />}
    {active && inventory.data && <ProviderConnectionDialog key={active.id} client={client} entry={active} enabled={enabled}
      revision={inventory.data.revision} hostName={hostName} flows={flows.data?.flows ?? []}
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
