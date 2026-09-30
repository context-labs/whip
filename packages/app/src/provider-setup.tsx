import { ErrorNotice } from './error-feedback';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { Client } from '@whip/sdk';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { scale, surface, typography } from '@whip/ui/tokens.stylex';
import { modelSettings, readModelCatalog } from './model-options';
import { SettingsGroup } from './settings/section-layout';
import { errorMessage } from './platform';
import { ProviderConnectionDialog, ProviderConnectionList, ProviderConnectionRow, type ProviderEntry, type useProviderConnections, locallyAvailable } from './settings/provider-connections';

type Connections = ReturnType<typeof useProviderConnections>;

/** A provider choice applies its known preset or leaves model selection to the composer. */
export function ProviderSetup({ client, enabled, hostName, connections, onReady, actions, onExpandedChange }: {
  client: Client; enabled: boolean; hostName: string; connections: Connections; onReady(provider: string, model: string, effort: string): void; actions?: ReactNode; onExpandedChange?(expanded: boolean): void;
}) {
  const { inventory, candidates, flows, refresh, entries } = connections;
  const available = entries.filter(locallyAvailable);
  const [connecting, setConnecting] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => { setBusy(false); request.current = null; return () => request.current?.abort(); }, [client]);
  const active = entries.find(entry => entry.id === connecting);
  const host = client.runtimeID;
  function choose(entry: ProviderEntry) {
    setError('');
    if (locallyAvailable(entry)) void useProvider(entry);
    else setConnecting(entry.id);
  }
  async function useProvider(entry: ProviderEntry, justConnected = false) {
    if (!enabled || request.current || !inventory.data) return;
    const controller = new AbortController(); request.current = controller;
    setBusy(true); setError(''); setConnecting(undefined);
    try {
      let current = justConnected ? await client.listProviders({ signal: controller.signal }) : inventory.data;
      if (!current.routes.some(route => route.id === entry.id)) {
        const detected = entry.candidates?.[0];
        if (!detected) throw new Error('Choose a connection method first');
        current = await client.useProviderCandidate({ revision: current.revision, provider: detected.provider, source: detected.source }, { signal: controller.signal });
      }
      const route = current.routes.find(route => route.id === entry.id);
      const preset = entry.preset && route?.kind === entry.preset.kind && route.base_url.replace(/\/$/, '') === entry.preset.base_url.replace(/\/$/, '') ? entry.preset : undefined;
      const model = preset?.suggested_models[0] ?? '';
      const effort = model ? preset?.suggested_effort ?? '' : '';
      if (model) {
        const models = await readModelCatalog(client, controller.signal, entry.id);
        await client.setProviderDefaults({ revision: current.revision, defaults: { selection: { name: model, provider: entry.id, effort }, settings: modelSettings(models, entry.id, model) } }, { signal: controller.signal });
        await refresh();
      }
      if (!controller.signal.aborted) onReady(entry.id, model, effort);
    } catch (error) {
      if (!controller.signal.aborted) { setError(errorMessage(error)); await inventory.refetch(); }
    } finally {
      if (request.current === controller) request.current = null;
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  const pending = entries.filter(entry => !available.includes(entry));
  const rows = (values: ProviderEntry[]) => values.map(entry => <ProviderConnectionRow key={entry.id} setup entry={entry} hostName={hostName}
    enabled={enabled && !busy} connect={!locallyAvailable(entry)} onSelect={() => choose(entry)} />);
  const refreshButton = <Button variant="ghost" disabled={!enabled || inventory.isFetching} onClick={() => void refresh()}>Refresh</Button>;
  return <section aria-label="Provider setup" {...stylex.props(styles.panel)}>
    {inventory.isPending && <p role="status">Checking providers on {hostName}…</p>}
    {inventory.error && enabled && <ErrorNotice type="resource" owner={`${host}:providers`} title="Could not load providers" error={inventory.error} action={refreshButton} />}

    {!!available.length && <SettingsGroup title="Already available" panelXstyle={styles.providerPanel}>{rows(available)}</SettingsGroup>}
    <ProviderConnectionList key={host} entries={pending} enabled={enabled && !busy} hostName={hostName} onSelect={choose}
      title={available.length ? 'Connect another provider' : undefined} actions={actions} refresh={refreshButton} onExpandedChange={onExpandedChange} />
    {!inventory.isPending && !inventory.error && !entries.length && <p role="status" {...stylex.props(styles.note)}>No providers are available on this host. Try refreshing or connect a remote host.</p>}
    {candidates.error && enabled && <ErrorNotice type="resource" owner={`${host}:provider-candidates`} title="Could not check available credentials" error={candidates.error} />}
    {flows.error && enabled && <ErrorNotice type="resource" owner={`${host}:sign-ins`} title="Could not load sign-in progress" error={flows.error} />}
    {error && <ErrorNotice type="action" owner={`${host}:default-model`} title="Could not select provider" error={error} />}
    {active && inventory.data && <ProviderConnectionDialog key={active.id} client={client} entry={active} enabled={enabled}
      revision={inventory.data.revision} hostName={hostName} flows={flows.data ?? []}
      refresh={refresh} refreshFlows={async () => { await flows.refetch(); }} close={() => setConnecting(undefined)}
      onConnected={() => void useProvider(active, true)} />}
  </section>;
}

const styles = stylex.create({
  note: { fontSize: typography.size12, color: surface.secondaryText, margin: 0, lineHeight: 1.5 },
  panel: { display: 'flex', flexDirection: 'column', width: '100%', gap: scale.space2, textAlign: 'start' },
  providerPanel: { padding: scale.space3, gap: scale.space2, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder },
});
