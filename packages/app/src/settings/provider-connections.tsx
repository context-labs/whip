import { ErrorNotice } from '../error-feedback';
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import type { ProviderList, ProviderLoginStatus } from '@whip/protocol';
import { Alert, Badge, Button, Dialog, Field, Input, Menu, type MenuItem } from '@whip/ui';
import { ChevronDown, ChevronUp, ExternalLink, Plus } from 'lucide-react';
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
import { loginStyles } from './provider-login.stylex';

export type ProviderEntry = NonNullable<ProviderList['providers']>[number];

const descriptions: Record<string, string> = {
  'inference-net': 'Sign in in your browser.',
  openrouter: 'Access models from multiple providers with one API key.',
  'openai-codex': 'Use Codex access included with your ChatGPT account.',
};

export function sourceLabel(entry: ProviderEntry) {
  const labels: Record<string, string> = { environment: 'Environment', env_file: 'Environment file', key_file: 'Key file', literal: 'API key', machine: 'Account', subscription: 'ChatGPT subscription', external: 'External account', command: 'Command', reference: 'Configuration' };
  return labels[entry.status.key_source] ?? (entry.custom ? 'Custom' : '');
}

export function stateLabel(entry: ProviderEntry) {
  if (entry.status.disabled) return 'Disabled on this host';
  if (entry.status.available) return '';
  const labels: Record<string, string> = { sign_in_required: 'Sign in again', setup_required: 'Finish setup', configuration_error: 'Configuration needs attention', signed_out: 'Not signed in', key_required: 'API key required', unchecked: 'Credential command has not been checked' };
  return labels[entry.status.auth_state ?? ''] ?? 'Not connected';
}

export function useProviderConnections(client: WhipClient, enabled: boolean) {
  const runtime = useRuntime();
  const host = client.getSnapshot().info?.runtime_id;
  const [discovering, setDiscovering] = useState(false);
  const [discoveryError, setDiscoveryError] = useState('');
  const [persistenceError, setPersistenceError] = useState('');
  const discoveryRequest = useRef<AbortController | null>(null);
  useEffect(() => {
    if (!discoveryRequest.current) setDiscovering(false);
    setDiscoveryError(''); setPersistenceError('');
    return () => { discoveryRequest.current?.abort(); discoveryRequest.current = null; };
  }, [client, host, enabled]);
  const inventory = useQuery({ queryKey: ['provider-list', host], queryFn: ({ signal }) => client.providers.list({ signal }), enabled });
  const flows = useQuery({
    queryKey: ['provider-login-flows', host], queryFn: ({ signal }) => client.providers.login.list({ signal }), enabled,
    refetchInterval: query => enabled && query.state.data?.flows?.some(flow => !terminalLoginStates.includes(flow.state)) ? 2000 : false,
  });
  const refresh = useCallback(async () => {
    if (host) await runtime.queries.invalidateQueries({ predicate: query => query.queryKey.includes(host) && query.queryKey[0] !== 'provider-login-flows' });
  }, [host, runtime.queries]);
  const discover = useCallback(async () => {
    if (!enabled || !host || discoveryRequest.current) return;
    const controller = new AbortController(); discoveryRequest.current = controller;
    setDiscovering(true); setDiscoveryError(''); setPersistenceError('');
    try {
      // Inventory queries stay read-only; discovery runs only when setup opens or the user refreshes.
      await runtime.queries.cancelQueries({ queryKey: ['provider-list', host], exact: true });
      controller.signal.throwIfAborted();
      const result = client.supports('rpc', 'provider.discover')
        ? await client.providers.discover({ signal: controller.signal })
        : await client.providers.list({ signal: controller.signal });
      if (controller.signal.aborted) return;
      runtime.queries.setQueryData(['provider-list', host], result);
      setPersistenceError(result.discovery_error ?? '');
      await refresh();
    } catch (error) {
      if (!controller.signal.aborted) { setDiscoveryError(errorMessage(error)); await refresh(); }
    } finally {
      if (discoveryRequest.current === controller) discoveryRequest.current = null;
      if (!controller.signal.aborted) setDiscovering(false);
    }
  }, [client, enabled, host, refresh, runtime.queries]);
  useEffect(() => {
    if (flows.data?.flows?.some(flow => flow.state === 'succeeded')) void refresh();
  }, [flows.data, refresh]);
  return { inventory, flows, refresh, discover, discovering, discoveryError, persistenceError };
}

/** Provider identity and actions shared by Settings and first-session setup. */
export function ProviderConnectionRow({ entry, enabled, connect, onSelect, setup = false, hostName }: {
  entry: ProviderEntry; enabled: boolean; connect: boolean; onSelect(): void; setup?: boolean; hostName?: string;
}) {
  const action = entry.status.disabled ? 'Manage' : connect ? 'Connect' : setup ? 'Use' : 'Manage';
  return <div {...stylex.props(styles.row, setup && styles.setupRow)}>
    <div {...stylex.props(styles.identity, setup && styles.setupIdentity)}>
      <span {...stylex.props(styles.logoSlot)}><ProviderLogo id={entry.id} size={20} /></span>
      <div {...stylex.props(styles.details)}>
        <div {...stylex.props(styles.nameLine)}><strong {...stylex.props(styles.name, setup && styles.setupName)}>{entry.name}</strong>
          {!setup && entry.status.available && sourceLabel(entry) && <Badge>{sourceLabel(entry)}</Badge>}
          {entry.custom && entry.status.key_source !== 'none' && <Badge>Custom</Badge>}
          {connect && entry.recommended && <Badge tone="success">Recommended</Badge>}
        </div>
        {setup ? <span {...stylex.props(styles.description)}>{entry.status.available ? [sourceLabel(entry), hostName].filter(Boolean).join(' · ')
          : entry.status.disabled || ['configuration_error', 'sign_in_required', 'setup_required', 'unchecked'].includes(entry.status.auth_state ?? '') ? stateLabel(entry)
          : entry.id === 'openai-codex' ? 'Use your ChatGPT subscription.' : descriptions[entry.id] ?? 'Use an API key.'}</span>
          : entry.status.available ? <span {...stylex.props(styles.description)}>{entry.status.email || (entry.status.key_source === 'environment' ? 'Detected on this execution host' : entry.status.plan)}</span>
          : <span {...stylex.props(styles.description)}>{connect && !entry.status.disabled ? descriptions[entry.id] ?? 'Manage this configured endpoint.' : stateLabel(entry)}</span>}
      </div>
    </div>
    <Button data-provider-choice={setup || undefined} xstyle={setup && styles.setupAction} id={!setup && entry.id === 'openrouter' ? 'provider_key' : undefined} variant={connect ? 'secondary' : 'ghost'} disabled={!enabled}
      aria-label={`${action} ${entry.name}`} onClick={onSelect}>
      {connect && !entry.status.disabled && <Plus size={15} aria-hidden="true" />}{action}
    </Button>
  </div>;
}

/** Compact connection choices shared by onboarding and Settings. */
export function ProviderConnectionList({ entries, enabled, hostName, onSelect, title, actions, refresh }: {
  entries: ProviderEntry[]; enabled: boolean; hostName: string; onSelect(entry: ProviderEntry): void;
  title?: string; actions?: ReactNode; refresh?: ReactNode;
}) {
  const [showAll, setShowAll] = useState(false);
  const defaults = ['inference-net', 'openrouter', 'openai', 'openai-codex'];
  const common = defaults.flatMap(id => entries.filter(entry => entry.id === id));
  const managed = entries.filter(entry => !defaults.includes(entry.id) && (entry.custom || entry.status.disabled ||
    ['sign_in_required', 'setup_required', 'configuration_error', 'unchecked'].includes(entry.status.auth_state ?? '')));
  const visible = showAll ? entries : common.length || managed.length ? [...common, ...managed] : entries.slice(0, 4);
  return <>
    {!title && showAll && refresh && <div {...stylex.props(styles.refresh)}>{refresh}</div>}
    {!!visible.length && <SettingsGroup title={title} action={refresh} panelXstyle={styles.choicePanel}>
      {visible.map(entry => <ProviderConnectionRow key={entry.id} setup entry={entry} hostName={hostName} enabled={enabled}
        connect={!((entry.status.available || entry.status.auth_state === 'unchecked') && !entry.status.disabled)} onSelect={() => onSelect(entry)} />)}
    </SettingsGroup>}
    <div {...stylex.props(styles.choiceActions)}>{actions}<Button variant="ghost" aria-expanded={showAll} onClick={() => setShowAll(!showAll)}>
      {showAll ? 'Show fewer providers' : 'Show all providers'}{showAll ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
    </Button></div>
  </>;
}

export function ProviderConnections({ client, enabled }: { client: WhipClient; enabled: boolean }) {
  const runtime = useRuntime();
  const host = client.getSnapshot().info?.runtime_id;
  const hostName = runtime.connections?.host(host ?? '')?.name ?? 'this execution host';
  const [selected, select] = useState<string>();
  const [notice, setNotice] = useState('');
  const [connectedProvider, setConnectedProvider] = useState<string>();
  const [defaultError, setDefaultError] = useState('');
  const [selectingDefault, setSelectingDefault] = useState(false);
  const defaultRequest = useRef<AbortController | null>(null);
  useEffect(() => () => defaultRequest.current?.abort(), []);
  const { inventory, flows, refresh, discover, discovering, discoveryError, persistenceError } = useProviderConnections(client, enabled);
  const entries = inventory.data?.providers ?? [];
  const completed = entries.find(entry => entry.id === connectedProvider);
  const defaultProvider = entries.find(entry => entry.id === inventory.data?.default_provider);
  const unavailableDefault = inventory.data?.default_provider && !defaultProvider?.status.available;
  const active = entries.find(entry => entry.id === selected);
  const connected = entries.filter(entry => entry.status.available);
  const disabled = entries.filter(entry => entry.status.disabled);
  const attention = entries.filter(entry => !entry.status.available && !entry.status.disabled &&
    ['sign_in_required', 'setup_required', 'configuration_error', 'unchecked'].includes(entry.status.auth_state ?? ''));
  const available = entries.filter(entry => !connected.includes(entry) && !attention.includes(entry) && !disabled.includes(entry));
  const refreshAction = <Button variant="ghost" disabled={!enabled || inventory.isFetching || discovering} onClick={() => void discover()}>Refresh</Button>;
  const renderRows = (values: ProviderEntry[], connect: boolean) => values.map(entry => <ProviderConnectionRow key={entry.id} entry={entry} enabled={enabled} connect={connect} onSelect={() => { select(entry.id); setNotice(''); }} />);
  return <>
    <div id="providers" tabIndex={-1} {...stylex.props(layout.column)}>
      {!available.length && <div id="provider" tabIndex={-1} {...stylex.props(styles.refresh)}>{refreshAction}</div>}
      {!inventory.data && inventory.isPending && <p role="status">Loading providers…</p>}
      {inventory.error && enabled && <ErrorNotice type="resource" owner={`${host}:providers`} title="Could not load providers" error={inventory.error} />}
      {enabled && (discoveryError || persistenceError || inventory.data?.discovery_error) && <ErrorNotice type="resource" owner={`${host}:provider-discovery`} title="Provider discovery needs attention" error={discoveryError || persistenceError || inventory.data?.discovery_error} />}
      {notice && <p role="status">{notice}</p>}
      {completed && <div {...stylex.props(styles.intro)}>
        <span {...stylex.props(styles.description)}>Your existing default is unchanged.</span>
        <Button variant="secondary" disabled={!enabled || selectingDefault} onClick={() => {
          if (!completed.suggested_model || !inventory.data) { document.getElementById('default_model')?.querySelector('button')?.focus(); return; }
          const controller = new AbortController(); defaultRequest.current = controller; setSelectingDefault(true); setDefaultError('');
          void client.configuration.update({ revision: inventory.data.revision, default_model: completed.suggested_model, default_provider: completed.id,
            ...(inventory.data.selection?.model !== completed.suggested_model || inventory.data.selection.provider !== completed.id ? { default_effort: '' } : {}) }, { signal: controller.signal })
            .then(async () => { await refresh(); if (!controller.signal.aborted) { setConnectedProvider(undefined); setNotice(`${completed.name} selected for new sessions.`); } })
            .catch(async error => { if (!controller.signal.aborted) { setDefaultError(errorMessage(error)); await refresh(); } })
            .finally(() => { if (!controller.signal.aborted) setSelectingDefault(false); });
        }}>{completed.suggested_model ? `Use ${completed.suggested_model} for new sessions` : 'Choose a model for new sessions'}</Button>
      </div>}
      {defaultError && <ErrorNotice type="action" owner={`${host}:default-provider`} title="Could not change the default provider" error={defaultError} />}
      {inventory.data && <>
        {!!connected.length && <SettingsGroup title="Connected providers">{renderRows(connected, false)}</SettingsGroup>}
        {!!disabled.length && <SettingsGroup title="Disabled providers">{renderRows(disabled, false)}</SettingsGroup>}
        {!!attention.length && <SettingsGroup title="Needs attention">{renderRows(attention, false)}</SettingsGroup>}
        {!!available.length && <div id="provider" tabIndex={-1} {...stylex.props(layout.column)}><ProviderConnectionList key={host} entries={available} enabled={enabled} hostName={hostName} title="Connect a provider" refresh={refreshAction} onSelect={entry => { select(entry.id); setNotice(''); }} /></div>}
      </>}
      {flows.data?.flows?.filter(flow => !terminalLoginStates.includes(flow.state)).map(flow => <div key={flow.flow_id} {...stylex.props(styles.intro)}>
        <span {...stylex.props(styles.description)}>Sign-in in progress: {entries.find(entry => entry.id === (flow.provider || 'inference-net'))?.name ?? flow.provider}</span>
        <Button variant="ghost" disabled={!enabled} onClick={() => select(flow.provider || 'inference-net')}>Continue sign-in</Button>
      </div>)}
      {flows.error && enabled && <ErrorNotice type="resource" owner={`${host}:sign-ins`} title="Could not load sign-in progress" error={flows.error} />}
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
      close={message => { select(undefined); if (message) setNotice(message); }}
      onConnected={message => { select(undefined); setConnectedProvider(active.id); setNotice(message ?? `${active.name} connected.`); }} />}
  </>;
}

export function ProviderConnectionDialog({ client, entry, enabled, revision, hostName, flows, refresh, refreshFlows, close, onConnected }: {
  client: WhipClient; entry: ProviderEntry; enabled: boolean; revision: string; hostName: string; flows: ProviderLoginStatus[];
  refresh(): Promise<void>; refreshFlows(): Promise<void>; close(message?: string): void; onConnected?(message?: string): void;
}) {
  const runtime = useRuntime();
  const [key, setKey] = useState('');
  const [showKey, setShowKey] = useState(!entry.status.available && !entry.status.disabled && !['literal', 'machine', 'subscription', 'external'].includes(entry.status.key_source) && !entry.methods?.includes('login') && !!entry.methods?.includes('api_key'));
  const [discard, setDiscard] = useState<'back' | 'close' | null>(null);
  const [loginId, setLoginId] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [hiddenFlow, setHiddenFlow] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const observingLogin = useRef<string | null>(null);
  const host = client.getSnapshot().info?.runtime_id;
  const [autoOpenLogin, setAutoOpenLogin] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), [client]);
  useSettingsEdits({ id: 'provider_key', dirty: Boolean(key), description: 'Unsaved API key', discard: () => setKey('') });
  async function action(run: (signal: AbortSignal) => Promise<void>) {
    if (!enabled || request.current) return;
    const controller = new AbortController(); request.current = controller;
    setBusy(true); setError(''); setNotice('');
    try { await run(controller.signal); }
    catch (error) {
      if (!controller.signal.aborted) { setError(errorMessage(error)); await Promise.allSettled([refresh(), refreshFlows()]); }
    } finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) { setBusy(false); setStarting(false); } }
  }
  const accountFlows = flows.filter(flow => (flow.provider || 'inference-net') === entry.id);
  const activeFlow = accountFlows.find(flow => !terminalLoginStates.includes(flow.state));
  const latestFlow = accountFlows.find(flow => flow.flow_id === loginId) ?? activeFlow;
  const visibleFlow = !starting && latestFlow?.flow_id !== hiddenFlow ? latestFlow : undefined;
  const inLogin = starting || !!visibleFlow;
  const canDisconnect = ['literal', 'machine'].includes(entry.status.key_source) ||
    (entry.status.key_source === 'subscription' && entry.status.auth_state !== 'signed_out');
  const initialConnection = !entry.status.available && !entry.status.disabled && !canDisconnect &&
    ['key_required', 'signed_out'].includes(entry.status.auth_state ?? 'key_required');
  const finish = async (message: string, signal: AbortSignal, connected = false) => { await refresh(); if (!signal.aborted) { if (connected && onConnected) onConnected(message); else close(message); } };
  async function updateFlow(flow: ProviderLoginStatus) {
    const queryKey = ['provider-login-flows', host];
    await runtime.queries.cancelQueries({ queryKey, exact: true });
    runtime.queries.setQueryData<{ flows: ProviderLoginStatus[] }>(queryKey, previous => ({ flows: [flow, ...(previous?.flows ?? []).filter(item => item.flow_id !== flow.flow_id)] }));
    setLoginId(flow.flow_id);
  }
  const completion = useRef({ refresh, onConnected, close });
  completion.current = { refresh, onConnected, close };
  useEffect(() => {
    if (activeFlow) { observingLogin.current = activeFlow.flow_id; setLoginId(activeFlow.flow_id); }
    if (latestFlow?.state !== 'succeeded' || observingLogin.current !== latestFlow.flow_id) return;
    observingLogin.current = null;
    let mounted = true;
    void completion.current.refresh().then(() => { if (mounted) { if (completion.current.onConnected) completion.current.onConnected(); else completion.current.close(); } }).catch(error => { if (mounted) setError(errorMessage(error)); });
    return () => { mounted = false; };
  }, [latestFlow?.state, latestFlow?.flow_id, activeFlow?.flow_id]);
  const leaveKey = (destination: 'back' | 'close') => {
    if (key) setDiscard(destination);
    else if (destination === 'close') close();
    else { setShowKey(false); setError(''); }
  };
  const startLogin = () => void action(async signal => {
    setStarting(true); setShowKey(false); setHiddenFlow(null); setAutoOpenLogin(true);
    const current = await client.providers.login.list({ signal });
    const existing = current.flows?.find(flow => (flow.provider || 'inference-net') === entry.id && !terminalLoginStates.includes(flow.state));
    const flow = existing ?? await client.providers.login.begin({ provider: entry.id, signal });
    if (!signal.aborted) { observingLogin.current = flow.flow_id; await updateFlow(flow); }
  });
  const leaveLogin = (message?: string) => { setHiddenFlow(latestFlow?.flow_id ?? null); setLoginId(null); setAutoOpenLogin(false); setNotice(message ?? ''); setError(''); };
  const editKey = () => { setShowKey(true); setError(''); setNotice(''); };
  const useDetectedKey = () => void action(async signal => {
    await client.providers.setKey({ provider: entry.id, revision, key: '', environment: true }, { signal });
    await finish(`${entry.name} now uses the detected host key.`, signal, true);
  });
  const connectionActions: MenuItem[] = [];
  if (entry.methods?.includes('login')) connectionActions.push({ id: 'sign-in', label: entry.status.available ? 'Sign in with another account' : entry.status.auth_state === 'setup_required' ? 'Finish connecting' : 'Sign in', onSelect: startLogin });
  if (entry.methods?.includes('api_key')) connectionActions.push({ id: 'key', label: entry.status.available ? 'Replace API key' : 'Use an API key', onSelect: editKey });
  if (entry.methods?.includes('environment')) connectionActions.push({ id: 'environment', label: 'Use detected key', onSelect: useDetectedKey });
  if (entry.status.available || entry.status.configured || entry.status.disabled) {
    connectionActions.push({ id: 'toggle', label: entry.status.disabled ? 'Enable on this host' : 'Disable on this host', onSelect: () => void action(async signal => {
      const disable = !entry.status.disabled;
      const configuration = await client.configuration.get({ signal });
      if (configuration.revision !== revision) throw new Error('Provider settings changed. Review the refreshed connection and try again.');
      const disabled = (configuration.disabled_providers ?? []).filter(id => id !== entry.id);
      if (disable) disabled.push(entry.id);
      await client.configuration.update({ revision, disabled_providers: disabled }, { signal });
      await finish(`${entry.name} ${disable ? 'disabled' : 'enabled'} on ${hostName}.`, signal);
    }) });
    if (entry.id === 'inference-net' && entry.status.key_source === 'machine' && entry.status.email) connectionActions.push({ id: 'rotate', label: 'Rotate machine key', onSelect: () => void action(async signal => {
      await client.providers.rotateKey(entry.id, { signal });
      await refresh();
      if (!signal.aborted) setNotice('Machine key rotated.');
    }) });
  }
  if (canDisconnect) {
    if (connectionActions.length) connectionActions.push({ id: 'disconnect-divider', label: '', separator: true });
    connectionActions.push({ id: 'disconnect', label: 'Disconnect provider', danger: true, onSelect: () => void action(async signal => {
      const result = await client.providers.disconnect({ provider: entry.id, revision }, { signal });
      const message = result.available ? `${entry.name} saved connection removed. Credentials from this host still make it available.` : `${entry.name} disconnected.`;
      await finish([message, ...(result.warnings ?? [])].join(' '), signal);
    }) });
  }
  const description = `${initialConnection || showKey || inLogin ? 'Connect' : 'Connection'} on ${hostName}`;
  return <Dialog open xstyle={(showKey || inLogin) && loginStyles.dialog} bodyXstyle={loginStyles.body} title={<span {...stylex.props(styles.nameLine)}><ProviderLogo id={entry.id} />{entry.name}</span>} description={description} onOpenChange={open => { if (!open) leaveKey('close'); }}>
    {!enabled && !inLogin && <Alert tone="warning">{hostName} is unavailable. Reconnect to continue; your draft is kept here.</Alert>}
    {showKey && !inLogin && <form onSubmit={event => {
      event.preventDefault(); const value = key.trim(); if (!value || busy || !enabled) return;
      void action(async signal => {
        await client.providers.setKey({ revision, provider: entry.id, key: value, environment: false }, { signal });
        if (!signal.aborted) setKey('');
        await finish(`${entry.name} connected.`, signal, true);
      });
    }} {...stylex.props(loginStyles.flow)}>
      <Field label="API key" error={error && <ErrorNotice type="action" owner={`provider:${entry.id}`} title="Could not connect with this key" error={error} />} description={<>Enter an API key above, or specify{' '}
        {entry.status.environment_variable ? <code>{entry.status.environment_variable}</code> : 'the provider’s configured API key variable'} in the host’s environment.
        {' '}Restart the host after changing environment variables to refresh.</>}><Input type="password" autoComplete="off" spellCheck={false}
        value={key} disabled={!enabled || busy} onChange={event => setKey(event.target.value)} /></Field>
      <div {...stylex.props(loginStyles.footer)}><Button variant="ghost" disabled={busy} onClick={() => leaveKey('back')}>Back</Button><Button variant="primary" xstyle={loginStyles.submit} type="submit" disabled={!enabled || busy || !key.trim()}>{busy ? 'Connecting…' : 'Connect'}</Button></div>
    </form>}
    {!showKey && !inLogin && <>
    {initialConnection && entry.methods?.includes('login') && <div {...stylex.props(loginStyles.content)}><h3 {...stylex.props(loginStyles.title)}>How would you like to connect?</h3><p {...stylex.props(loginStyles.text)}>Sign in with your {entry.id === 'inference-net' ? 'Inference.net' : entry.name} account{entry.methods.includes('api_key') ? ', or use an API key.' : '.'}</p></div>}
    {!showKey && !initialConnection && <div {...stylex.props(styles.account)}>
      <div {...stylex.props(styles.nameLine)}>
        <Badge tone={entry.status.disabled ? 'neutral' : entry.status.available ? 'success' : 'warning'}>{entry.status.available ? 'Connected' : stateLabel(entry)}</Badge>
      </div>
      <dl {...stylex.props(styles.metadata)}>
        {sourceLabel(entry) && <><dt {...stylex.props(styles.description)}>Connection method</dt><dd {...stylex.props(styles.value)}>{sourceLabel(entry)}</dd></>}
        {entry.status.email && <><dt {...stylex.props(styles.description)}>Email</dt><dd {...stylex.props(styles.value)}>{entry.status.email}</dd></>}
        {entry.status.team_name && <><dt {...stylex.props(styles.description)}>Team</dt><dd {...stylex.props(styles.value)}>{entry.status.team_name}</dd></>}
        {entry.status.project_name && <><dt {...stylex.props(styles.description)}>Project</dt><dd {...stylex.props(styles.value)}>{entry.status.project_name}</dd></>}
        {entry.status.plan && <><dt {...stylex.props(styles.description)}>Plan</dt><dd {...stylex.props(styles.value)}>{entry.status.plan}</dd></>}
      </dl>
    </div>}
    {entry.status.disabled && <p {...stylex.props(styles.description)}>Your credentials are unchanged. Enable this provider to use its existing connection again.</p>}
    {!showKey && !initialConnection && entry.status.key_source === 'environment' && <p {...stylex.props(styles.description)}>
      {entry.status.environment_variable ? <><code>{entry.status.environment_variable}</code> is read</> : 'Credentials are read'} from this host’s environment.
      To disconnect, remove the key from the host’s environment and restart the host.
    </p>}
    {entry.status.key_source === 'external' && <p {...stylex.props(styles.description)}>Credentials are managed by the Inference.net CLI on this host. Sign out there to disconnect.</p>}
    {['env_file', 'key_file'].includes(entry.status.key_source) && <p {...stylex.props(styles.description)}>
      {entry.status.environment_variable ? <><code>{entry.status.environment_variable}</code> is read</> : 'Credentials are read'} from {entry.status.credential_path ? <code>{entry.status.credential_path}</code> : 'a configured file'} on this host.
      {' '}Remove the key from that file to disconnect.
    </p>}
    {entry.status.key_source === 'command' && <p {...stylex.props(styles.description)}>The host runs your configured credential command when it needs a key. Remove that credential configuration to disconnect. Opening this page does not run the command.</p>}
    {entry.status.warnings?.map((warning, index) => <Alert key={index} tone="warning">{warning}</Alert>)}
    {entry.id === 'openai-codex' && <p {...stylex.props(styles.description)}>Uses your ChatGPT account’s Codex access. Enable device code authorization in ChatGPT Security settings before signing in. Subscription usage is separate from API billing.</p>}
    {initialConnection ? <div {...stylex.props(loginStyles.content)}>
      {entry.methods?.includes('login') && <Button variant="primary" xstyle={loginStyles.full} disabled={!enabled || busy} onClick={startLogin}>{entry.id === 'inference-net' ? 'Sign in with Inference.net' : 'Sign in'}<ExternalLink size={14} /></Button>}
      {entry.methods?.includes('api_key') && <Button xstyle={loginStyles.full} disabled={!enabled || busy} onClick={editKey}>Use an API key</Button>}
      {entry.methods?.includes('environment') && <Button xstyle={loginStyles.full} disabled={!enabled || busy} onClick={useDetectedKey}>Use detected key</Button>}
    </div> : <div {...stylex.props(styles.accountFooter)}>
      <Menu align="start" trigger={<Button variant="ghost" disabled={!enabled || busy || !connectionActions.length}>Connection options<ChevronDown size={14} /></Button>} items={connectionActions.map(item => ({ ...item, disabled: !enabled || busy }))} />
      <Button variant="primary" onClick={() => close()}>Done</Button>
    </div>}
    </>}
    {starting && <div {...stylex.props(loginStyles.flow)}><h3 {...stylex.props(loginStyles.title)}>Starting sign-in…</h3><p {...stylex.props(loginStyles.text)}>Preparing a secure sign-in on {hostName}.</p><div {...stylex.props(loginStyles.footer)}><Button disabled>Starting…</Button></div></div>}
    {visibleFlow && <LoginFlow key={visibleFlow.flow_id} flow={visibleFlow} autoOpen={autoOpenLogin} client={client} enabled={enabled} hostName={hostName} update={updateFlow} retry={startLogin} leave={leaveLogin} />}
    {error && (!showKey || inLogin) && <ErrorNotice type="action" owner={`provider:${entry.id}`} title="Could not update the provider connection" error={error} />}{notice && <Alert tone="success">{notice}</Alert>}
    {discard && <Dialog open title="Discard this API key?" description="The key you entered hasn’t been connected. Discard it to leave this form." onOpenChange={open => { if (!open) setDiscard(null); }} footer={<><Button onClick={() => setDiscard(null)}>Keep editing</Button><Button variant="primary" onClick={() => { setKey(''); setDiscard(null); if (discard === 'close') close(); else { setShowKey(false); setError(''); } }}>Discard key</Button></>} />}

  </Dialog>;
}

const styles = stylex.create({
  intro: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: scale.space3, flexWrap: 'wrap' },
  refresh: { display: 'flex', justifyContent: 'flex-end' },
  row: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: scale.space3, minWidth: 0,
    paddingBlock: scale.space3, borderBottomWidth: { default: 1, ':last-child': 0 }, borderBottomStyle: 'solid', borderBottomColor: surface.quietBorder },
  choicePanel: { padding: scale.space3, gap: scale.space2, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder },
  choiceActions: { display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: scale.space1 },
  setupRow: { minHeight: 64, paddingBlock: 10, borderBottomWidth: 0, flexWrap: 'nowrap' },
  setupIdentity: { flex: '1 1 0' },
  setupName: { lineHeight: '22px' },
  setupAction: { flexShrink: 0, minWidth: 98 },
  identity: { display: 'flex', alignItems: 'flex-start', gap: scale.space3, minWidth: 0, flex: '1 1 220px' },
  details: { display: 'flex', flexDirection: 'column', minWidth: 0, gap: scale.space1 },
  nameLine: { display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: scale.space2, minWidth: 0 },
  name: { fontSize: typography.size14, fontWeight: 500, color: colors.foreground, overflowWrap: 'anywhere' },
  description: { color: surface.secondaryText, fontSize: typography.size12, lineHeight: 1.5, margin: 0, overflowWrap: 'anywhere' },
  logoSlot: { display: 'flex', alignItems: 'center', height: '1.5em', fontSize: typography.size14, flexShrink: 0 },
  account: { display: 'flex', flexDirection: 'column', gap: scale.space4 },
  metadata: { display: 'grid', gridTemplateColumns: 'auto minmax(0, 1fr)', columnGap: scale.space4, rowGap: scale.space3, margin: 0, alignItems: 'baseline' },
  value: { margin: 0, overflowWrap: 'anywhere' },
  actionButton: { maxWidth: '100%', whiteSpace: 'normal' },
  accountFooter: { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: scale.space2, borderTopWidth: 1, borderTopStyle: 'solid', borderTopColor: surface.quietBorder, paddingTop: scale.space4 },
});
