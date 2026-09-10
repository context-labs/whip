import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import { Alert, Button } from '@whip/ui';
import { ArrowRight } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { CatalogModelPicker } from './model-selection';
import { ProviderLogo } from './provider-logo';
import { layout } from './styles';
import { errorMessage } from './platform';
import { ProviderConnectionDialog, sourceLabel, stateLabel, type ProviderEntry, type useProviderConnections } from './settings/provider-connections';

type Connections = ReturnType<typeof useProviderConnections>;

/** A view of host-owned readiness. Choosing a default is always an explicit write. */
export function ProviderSetup({ client, enabled, hostName, connections, onReady }: {
  client: WhipClient; enabled: boolean; hostName: string; connections: Connections; onReady(): void;
}) {
  const { inventory, flows, refresh, discover, discovering, discoveryError } = connections;
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
  const rows = (values: ProviderEntry[]) => values.map(entry => <Button key={entry.id} data-provider-choice variant="ghost" xstyle={styles.row}
    disabled={!enabled || busy} onClick={() => choose(entry)} aria-label={`${(entry.status.available || entry.status.auth_state === 'unchecked') && !entry.status.disabled ? 'Use' : 'Connect'} ${entry.name}`}>
    <ProviderLogo id={entry.id} />
    <span {...stylex.props(styles.identity)}>
      <span {...stylex.props(layout.row)}><span>{entry.name}</span>{entry.recommended && <span {...stylex.props(styles.note)}>Recommended</span>}</span>
      <span {...stylex.props(styles.note)}>{entry.status.available ? `${sourceLabel(entry)} · ${hostName}`
        : entry.status.disabled || ['configuration_error', 'sign_in_required', 'setup_required', 'unchecked'].includes(entry.status.auth_state ?? '') ? stateLabel(entry)
        : entry.id === 'inference-net' ? 'Sign in in your browser' : entry.id === 'openai-codex' ? 'Use your ChatGPT subscription' : 'Use an API key'}</span>
    </span><ArrowRight size={14} aria-hidden />
  </Button>);
  if (inventory.data && !inventory.data.selection) return <section aria-label="Provider setup" {...stylex.props(styles.panel)}>
    <Alert tone="warning">Update Whip on {hostName} to use provider setup. Your draft is preserved.</Alert>
    <Button variant="ghost" disabled={!enabled || inventory.isFetching || discovering} onClick={() => void discover()}>Check again</Button>
  </section>;
  return <section aria-label="Provider setup" {...stylex.props(styles.panel)}>
    <div {...stylex.props(styles.heading)}><strong>{available.length ? 'Already available' : 'Connect a provider to send your first message'}</strong>
      <Button variant="ghost" disabled={!enabled || inventory.isFetching || discovering} onClick={() => void discover()}>Refresh</Button></div>
    {inventory.isPending && <p role="status">Checking providers on {hostName}…</p>}
    {inventory.error && <Alert tone="error">{inventory.error.message}</Alert>}
    {discoveryError && <Alert tone="error">{discoveryError}</Alert>}
    {!!available.length && rows(available)}
    {candidate && <div {...stylex.props(styles.confirmation)}>
      <p {...stylex.props(styles.note)}>Default for new sessions on {hostName}. Your first message makes the request.</p>
      <CatalogModelPicker label="Change model" settings model={model} provider={candidate.id} catalog={catalog.data?.result}
        loading={catalog.isFetching} error={catalog.error?.message} disabled={!enabled || busy}
        onChange={(model, provider) => { setSelected(provider); setPair({ model, provider }); }} />
      {!model && <p role="status" {...stylex.props(styles.note)}>Choose a model for {candidate.name}.</p>}
      {catalog.error && <Button variant="ghost" disabled={!enabled || busy} onClick={() => void catalog.refetch()}>Retry model discovery</Button>}
      <Button data-provider-confirm variant="primary" disabled={!enabled || busy || !model} loading={busy} onClick={() => void useModel()}>Use {model || 'selected model'}</Button>
    </div>}
    {!!available.length && entries.some(entry => !available.includes(entry)) && <p {...stylex.props(styles.group)}>Connect another provider</p>}
    {rows(entries.filter(entry => !available.includes(entry)))}
    {flows.error && <Alert tone="error">{flows.error.message}</Alert>}
    {error && <Alert tone="error">{error}</Alert>}
    {active && inventory.data && <ProviderConnectionDialog key={active.id} client={client} entry={active} enabled={enabled}
      revision={inventory.data.revision} hostName={hostName} flows={flows.data?.flows ?? []}
      refresh={refresh} refreshFlows={async () => { await flows.refetch(); }} close={() => setConnecting(undefined)}
      onConnected={() => { setSelected(active.id); setConnecting(undefined); void refresh(); }} />}
  </section>;
}

const styles = stylex.create({
  panel: { display: 'flex', flexDirection: 'column', width: '100%', gap: scale.space2, textAlign: 'start' },
  heading: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: scale.space2, fontSize: typography.size13, color: surface.secondaryText },
  row: { width: '100%', justifyContent: 'flex-start', gap: scale.space3, textAlign: 'start', height: 'auto', padding: scale.space3, whiteSpace: 'normal' },
  identity: { display: 'flex', flexDirection: 'column', gap: scale.space1, flex: 1, minWidth: 0 },
  note: { fontSize: typography.size12, color: surface.secondaryText, margin: 0, lineHeight: 1.5, overflowWrap: 'anywhere' },
  group: { fontSize: typography.size12, color: surface.secondaryText, marginBottom: 0, marginTop: scale.space4 },
  confirmation: { display: 'flex', flexDirection: 'column', gap: scale.space3, padding: scale.space4, borderRadius: scale.radiusControl, backgroundColor: colors.panel },
});
