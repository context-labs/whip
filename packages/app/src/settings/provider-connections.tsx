import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import type { ProviderList, ProviderLoginStatus } from '@whip/protocol';
import { Alert, Badge, Button, Dialog, Field, Input } from '@whip/ui';
import { Plus } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useRuntime } from '../context';
import { errorMessage } from '../platform';
import { layout } from '../styles';
import { ProviderLogo } from '../provider-logo';
import { ConfigurationSettings } from './configuration';
import { SettingsGroup } from './section-layout';
import { LoginFlow, terminalLoginStates } from './provider-login';
import { useSettingsEdits } from './unsaved';

type ProviderEntry = NonNullable<ProviderList['providers']>[number];

const descriptions: Record<string, string> = {
  'inference-net': 'Connect your Inference.net account or use an API key.',
  openrouter: 'Access models from multiple providers with one API key.',
  'openai-codex': 'Use Codex access included with your ChatGPT account.',
};

function sourceLabel(entry: ProviderEntry) {
  if (entry.status.disabled) return 'Disabled';
  const labels: Record<string, string> = { environment: 'Environment', literal: 'API key', machine: 'Account', subscription: 'ChatGPT subscription', external: 'External account', command: 'Command', reference: 'Configuration' };
  return labels[entry.status.key_source] ?? (entry.custom ? 'Custom' : '');
}

function stateLabel(entry: ProviderEntry) {
  if (entry.status.disabled) return 'Disabled on this host';
  if (entry.status.available) return '';
  const labels: Record<string, string> = { sign_in_required: 'Sign in again', setup_required: 'Finish setup', configuration_error: 'Configuration needs attention', signed_out: 'Not signed in', key_required: 'API key required', unchecked: 'Credential command has not been checked' };
  return labels[entry.status.auth_state ?? ''] ?? 'Not connected';
}

export function ProviderConnections({ client, enabled }: { client: WhipClient; enabled: boolean }) {
  const runtime = useRuntime();
  const host = client.getSnapshot().info?.runtime_id;
  const hostName = runtime.connections?.host(host ?? '')?.name ?? 'this execution host';
  const [selected, select] = useState<string>();
  const [notice, setNotice] = useState('');
  const inventory = useQuery({ queryKey: ['provider-list', host], queryFn: ({ signal }) => client.providers.list({ signal }), enabled });
  const flows = useQuery({
    queryKey: ['provider-login-flows', host], queryFn: ({ signal }) => client.providers.login.list({ signal }), enabled,
    refetchInterval: query => enabled && query.state.data?.flows?.some(flow => !terminalLoginStates.includes(flow.state)) ? 2000 : false,
  });
  const refresh = async () => {
    if (host) await runtime.queries.invalidateQueries({ predicate: query => query.queryKey.includes(host) && query.queryKey[0] !== 'provider-login-flows' });
  };
  useEffect(() => {
    if (flows.data?.flows?.some(flow => flow.state === 'succeeded')) void refresh();
  }, [flows.data]);
  const entries = inventory.data?.providers ?? [];
  const defaultProvider = entries.find(entry => entry.id === inventory.data?.default_provider);
  const unavailableDefault = inventory.data?.default_provider && !defaultProvider?.status.available;
  const active = entries.find(entry => entry.id === selected);
  const connected = entries.filter(entry => entry.status.available);
  const attention = entries.filter(entry => !entry.status.available && !entry.status.disabled &&
    (entry.status.configured || ['sign_in_required', 'setup_required', 'configuration_error'].includes(entry.status.auth_state ?? '')));
  const available = entries.filter(entry => !connected.includes(entry) && !attention.includes(entry));
  const renderRows = (values: ProviderEntry[], connect: boolean) => values.map(entry => <div key={entry.id} {...stylex.props(styles.row)}>
    <div {...stylex.props(styles.identity)}>
      <span {...stylex.props(styles.logoSlot)}><ProviderLogo id={entry.id} /></span>
      <div {...stylex.props(styles.details)}>
        <div {...stylex.props(styles.nameLine)}><strong {...stylex.props(styles.name)}>{entry.name}</strong>
          {sourceLabel(entry) && (entry.status.available || entry.status.configured || entry.status.disabled) && <Badge>{sourceLabel(entry)}</Badge>}
          {entry.custom && entry.status.key_source !== 'none' && <Badge>Custom</Badge>}
        </div>
        {entry.status.available ? <span {...stylex.props(styles.description)}>{entry.status.email || (entry.status.key_source === 'environment' ? 'Detected on this execution host' : entry.status.plan)}</span>
          : <span {...stylex.props(styles.description)}>{connect && !entry.status.disabled ? descriptions[entry.id] ?? 'Manage this configured endpoint.' : stateLabel(entry)}</span>}
      </div>
    </div>
    <Button id={entry.id === 'openrouter' ? 'provider_key' : undefined} variant={connect ? 'secondary' : 'ghost'} disabled={!enabled}
      aria-label={`${connect ? 'Connect' : 'Manage'} ${entry.name}`} onClick={() => { select(entry.id); setNotice(''); }}>
      {connect && <Plus size={15} aria-hidden="true" />}{connect ? 'Connect' : 'Manage'}
    </Button>
  </div>);
  return <>
    <div id="providers" tabIndex={-1} {...stylex.props(layout.column)}>
      <div id="provider" tabIndex={-1} {...stylex.props(styles.intro)}>
        <span {...stylex.props(styles.description)}>Connections belong to {hostName}.</span>
        <Button variant="ghost" disabled={!enabled || inventory.isFetching} onClick={() => { void inventory.refetch(); }}>Refresh</Button>
      </div>
      {!inventory.data && inventory.isPending && <p role="status">Loading providers…</p>}
      {inventory.error && <p role="alert">{inventory.error.message}</p>}
      {notice && <p role="status">{notice}</p>}
      {inventory.data && <>
        <SettingsGroup title="Connected providers">{connected.length ? renderRows(connected, false) : <p {...stylex.props(styles.description)}>No providers connected on this host.</p>}</SettingsGroup>
        {!!attention.length && <SettingsGroup title="Needs attention">{renderRows(attention, false)}</SettingsGroup>}
        {!!available.length && <SettingsGroup title="Connect a provider">{renderRows(available, true)}</SettingsGroup>}
      </>}
      {flows.data?.flows?.filter(flow => !terminalLoginStates.includes(flow.state)).map(flow => <div key={flow.flow_id} {...stylex.props(styles.intro)}>
        <span {...stylex.props(styles.description)}>Sign-in in progress: {entries.find(entry => entry.id === (flow.provider || 'inference-net'))?.name ?? flow.provider}</span>
        <Button variant="ghost" disabled={!enabled} onClick={() => select(flow.provider || 'inference-net')}>Continue sign-in</Button>
      </div>)}
      {flows.error && <p role="alert">{flows.error.message}</p>}
    </div>
    {unavailableDefault && <div role="status" {...stylex.props(layout.notice, styles.intro)}>
      <span>The default provider, {defaultProvider?.name ?? inventory.data?.default_provider}, is unavailable. Your default model is unchanged.</span>
      {defaultProvider && <Button variant="secondary" disabled={!enabled} onClick={() => select(defaultProvider.id)}>Manage default provider</Button>}
      <Button variant="ghost" onClick={() => document.getElementById('default_model')?.querySelector('button')?.focus()}>Change default model</Button>
    </div>}
    <ConfigurationSettings client={client} enabled={enabled} category="providers" defaultProvider={inventory.data?.default_provider} />
    {active && inventory.data && <ProviderConnectionDialog key={active.id} client={client} entry={active} enabled={enabled}
      revision={inventory.data.revision} hostName={hostName} flows={flows.data?.flows ?? []}
      refresh={refresh} refreshFlows={async () => { await flows.refetch(); }}
      close={message => { select(undefined); if (message) setNotice(message); }} />}
  </>;
}

function ProviderConnectionDialog({ client, entry, enabled, revision, hostName, flows, refresh, refreshFlows, close }: {
  client: WhipClient; entry: ProviderEntry; enabled: boolean; revision: string; hostName: string; flows: ProviderLoginStatus[];
  refresh(): Promise<void>; refreshFlows(): Promise<void>; close(message?: string): void;
}) {
  const [key, setKey] = useState('');
  const [showKey, setShowKey] = useState(!entry.status.available && !entry.methods?.includes('login') && !!entry.methods?.includes('api_key'));
  const [discard, setDiscard] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  useSettingsEdits({ id: 'provider_key', dirty: Boolean(key), description: 'Unsaved API key', discard: () => setKey('') });
  async function action(run: (signal: AbortSignal) => Promise<void>) {
    if (!enabled || busy) return;
    const controller = new AbortController(); request.current = controller;
    setBusy(true); setError(''); setNotice('');
    try { await run(controller.signal); }
    catch (error) {
      if (!controller.signal.aborted) { setError(errorMessage(error)); await refresh(); }
    } finally { if (!controller.signal.aborted) setBusy(false); }
  }
  const accountFlows = flows.filter(flow => (flow.provider || 'inference-net') === entry.id);
  const activeFlow = accountFlows.find(flow => !terminalLoginStates.includes(flow.state));
  const latestFlow = activeFlow ?? accountFlows.sort((a, b) => b.expires_at.localeCompare(a.expires_at))[0];
  const canDisconnect = ['literal', 'machine'].includes(entry.status.key_source) ||
    (entry.status.key_source === 'subscription' && entry.status.auth_state !== 'signed_out');
  const finish = async (message: string, signal: AbortSignal) => { await refresh(); if (!signal.aborted) close(message); };
  const requestClose = () => { if (key) setDiscard(true); else close(); };
  return <Dialog open title={<span {...stylex.props(styles.nameLine)}><ProviderLogo id={entry.id} />{entry.name}</span>} description={`Provider connection on ${hostName}.`} onOpenChange={open => { if (!open) requestClose(); }}>
    <div {...stylex.props(styles.account)}>
      <div {...stylex.props(styles.nameLine)}><Badge>{sourceLabel(entry) || 'Not connected'}</Badge>
        {stateLabel(entry) && <span role="status" {...stylex.props(styles.description)}>{stateLabel(entry)}</span>}
      </div>
      {(entry.status.email || entry.status.project_name || entry.status.plan) && <dl {...stylex.props(styles.metadata)}>
        {entry.status.email && <><dt {...stylex.props(styles.description)}>Email</dt><dd {...stylex.props(styles.value)}>{entry.status.email}</dd></>}
        {entry.status.project_name && <><dt {...stylex.props(styles.description)}>Project</dt><dd {...stylex.props(styles.value)}>{entry.status.project_name}</dd></>}
        {entry.status.plan && <><dt {...stylex.props(styles.description)}>Plan</dt><dd {...stylex.props(styles.value)}>{entry.status.plan}</dd></>}
      </dl>}
    </div>
    {entry.status.key_source === 'environment' && <p {...stylex.props(styles.description)}>
      {entry.status.environment_variable ? <><code>{entry.status.environment_variable}</code> is read</> : 'Credentials are read'} from this host’s environment.
      Environment changes take effect after restarting the execution host. Disabling here keeps the environment unchanged.
    </p>}
    {entry.status.key_source === 'external' && <p {...stylex.props(styles.description)}>Credentials are managed by the Inference.net CLI on this host. You can disable this connection here.</p>}
    {entry.status.key_source === 'command' && <p {...stylex.props(styles.description)}>The host runs your configured credential command when it needs a key. Opening this page does not run it.</p>}
    {entry.status.warnings?.map((warning, index) => <Alert key={index} tone="warning">{warning}</Alert>)}
    {entry.id === 'openai-codex' && <p {...stylex.props(styles.description)}>Uses your ChatGPT account’s Codex access. Enable device code authorization in ChatGPT Security settings before signing in. Subscription usage is separate from API billing.</p>}
    {latestFlow && <LoginFlow flow={latestFlow} client={client} enabled={enabled && !busy} refresh={() => { void refreshFlows(); }} />}
    <div {...stylex.props(styles.actions)}>
      {entry.methods?.includes('login') && !activeFlow && <Button xstyle={styles.actionButton} disabled={!enabled || busy} onClick={() => void action(async signal => {
        await client.providers.login.begin({ provider: entry.id, signal });
        if (!signal.aborted) await refreshFlows();
      })}>{entry.status.available ? 'Sign in with another account' : 'Sign in'}</Button>}
      {entry.methods?.includes('api_key') && !showKey && <Button xstyle={styles.actionButton} variant="secondary" disabled={!enabled || busy} onClick={() => setShowKey(true)}>{entry.status.available ? 'Replace API key' : 'Use an API key'}</Button>}
      {entry.methods?.includes('environment') && <Button xstyle={styles.actionButton} variant="secondary" disabled={!enabled || busy} onClick={() => void action(async signal => {
        await client.providers.setKey({ provider: entry.id, revision, key: '', environment: true }, { signal });
        await finish(`${entry.name} now uses the host environment.`, signal);
      })}>Use host environment</Button>}
    </div>
    {showKey && <form onSubmit={event => {
      event.preventDefault(); const value = key.trim(); if (!value || busy || !enabled) return;
      setKey(''); void action(async signal => {
        await client.providers.setKey({ revision, provider: entry.id, key: value, environment: false }, { signal });
        await finish(`${entry.name} connected.`, signal);
      });
    }} {...stylex.props(layout.column)}>
      <Field label="API key" description="Validated and saved on this execution host."><Input type="password" autoComplete="off" spellCheck={false}
        value={key} disabled={!enabled || busy} onChange={event => setKey(event.target.value)} /></Field>
      <Button xstyle={styles.actionButton} type="submit" disabled={!enabled || busy || !key.trim()}>{busy ? 'Connecting…' : 'Connect'}</Button>
    </form>}
    {(entry.status.available || entry.status.configured || entry.status.disabled) && <div {...stylex.props(styles.maintenance)}>
      <Button xstyle={styles.actionButton} variant="ghost" disabled={!enabled || busy} onClick={() => void action(async signal => {
        const config = await client.configuration.get({ signal });
        if (config.revision !== revision) throw new Error('Provider settings changed. Review the refreshed connection and try again.');
        const names = config.disabled_providers ?? [];
        const disabled = entry.status.disabled ? names.filter(name => name !== entry.id) : [...new Set([...names, entry.id])];
        await client.configuration.update({ revision, disabled_providers: disabled }, { signal });
        await finish(`${entry.name} ${entry.status.disabled ? 'enabled' : 'disabled'} on this host.`, signal);
      })}>{entry.status.disabled ? 'Enable on this host' : 'Disable on this host'}</Button>
      {entry.id === 'inference-net' && entry.status.key_source === 'machine' && entry.status.email && <Button xstyle={styles.actionButton} variant="ghost" disabled={!enabled || busy} onClick={() => void action(async signal => {
        await client.providers.rotateKey(entry.id, { signal }); await refresh(); if (!signal.aborted) setNotice('Machine key rotated.');
      })}>Rotate machine key</Button>}
    </div>}
    {canDisconnect && <div {...stylex.props(styles.disconnect)}><Button xstyle={styles.actionButton} variant="danger" disabled={!enabled || busy} onClick={() => void action(async signal => {
      const result = await client.providers.disconnect({ provider: entry.id, revision }, { signal });
      await finish([`${entry.name} disconnected.`, ...(result.warnings ?? [])].join(' '), signal);
    })}>Disconnect provider</Button></div>}
    {error && <Alert tone="error">{error}</Alert>}{notice && <Alert tone="success">{notice}</Alert>}
    {discard && <div {...stylex.props(layout.notice, layout.column)}><p {...stylex.props(styles.value)}>Discard the unsaved API key?</p><div {...stylex.props(layout.row)}>
      <Button xstyle={styles.actionButton} variant="secondary" onClick={() => setDiscard(false)}>Keep editing</Button><Button xstyle={styles.actionButton} onClick={() => { setKey(''); close(); }}>Discard key</Button>
    </div></div>}
  </Dialog>;
}

const styles = stylex.create({
  intro: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: scale.space3, flexWrap: 'wrap' },
  row: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: scale.space3, minWidth: 0,
    paddingBlock: scale.space3, borderBottomWidth: { default: 1, ':last-child': 0 }, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder },
  identity: { display: 'flex', alignItems: 'flex-start', gap: scale.space3, minWidth: 0, flex: '1 1 220px' },
  details: { display: 'flex', flexDirection: 'column', minWidth: 0, gap: scale.space1 },
  nameLine: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: scale.space2, minWidth: 0 },
  name: { fontSize: typography.size14, fontWeight: 500, color: colors.foreground, overflowWrap: 'anywhere' },
  description: { color: surface.secondaryText, fontSize: typography.size12, lineHeight: 1.5, margin: 0, overflowWrap: 'anywhere' },
  logoSlot: { display: 'flex', alignItems: 'center', height: '1.5em', fontSize: typography.size14, flexShrink: 0 },
  account: { display: 'flex', flexDirection: 'column', gap: scale.space3, padding: scale.space4, borderRadius: scale.radiusControl, backgroundColor: colors.panel },
  metadata: { display: 'grid', gridTemplateColumns: 'auto minmax(0, 1fr)', columnGap: scale.space4, rowGap: scale.space2, margin: 0, alignItems: 'baseline' },
  value: { margin: 0, overflowWrap: 'anywhere' },
  actionButton: { maxWidth: '100%', whiteSpace: 'normal' },
  actions: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space2 },
  maintenance: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space2, borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, paddingTop: scale.space3 },
  disconnect: { display: 'flex', justifyContent: 'flex-end' },
});
