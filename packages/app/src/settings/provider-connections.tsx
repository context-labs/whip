import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Client, ProviderPresetsResult, ProviderInventory, ChangeProviderParams } from '@whip/sdk';
import { Alert, Badge, Button, Dialog, Field, Input, Select, Textarea } from '@whip/ui';
import { ChevronDown, ChevronUp, Plus } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useRuntime } from '../context';
import { errorMessage } from '../platform';
import { layout } from '../styles';
import { ErrorNotice } from '../error-feedback';
import { ProviderLogo } from '../provider-logo';
import { providerReady, recallProviderReady, rememberProviderReady } from '../provider-readiness';
import { ProviderDefaultsSettings } from './provider-defaults';
import { SettingsGroup } from './section-layout';
import { LoginFlow, readLoginFlows, pollingLoginStates, terminalLoginStates, type AccountFlow } from './provider-login';
import { useSettingsEdits } from './unsaved';
import { loginStyles } from './provider-login.stylex';

type ProviderPreset = ProviderPresetsResult['items'][number];
type ProviderRoute = ProviderInventory['routes'][number];
type ProviderCredentialInput = NonNullable<ChangeProviderParams['declaration']['credential']>;
export interface ProviderEntry { id: string; name: string; preset?: ProviderPreset; route?: ProviderRoute }
const keySetupEndpoints: Record<string, string> = { 'inference-net': 'https://api.inference.net/v1', openrouter: 'https://openrouter.ai/api/v1' };
export function sourceLabel(entry: ProviderEntry) {
  const source = entry.route?.credential.source;
  return source ? ({ env: 'Environment', file: 'Key file', command: 'Command', none: 'No authentication', 'inference-net': 'Inference.net account', 'openai-codex': 'ChatGPT subscription' })[source] : '';
}
export function locallyAvailable(entry: ProviderEntry) { return !!entry.route && providerReady({ configured: true, credential_state: entry.route.credential.state }) === true; }
export function stateLabel(entry: ProviderEntry) {
  if (!entry.route) return 'Not configured';
  return ({ available: 'Credentials available', not_required: 'No authentication required', missing: 'Credentials missing', unavailable: 'Credential source unavailable', unchecked: 'Credential command not checked', refresh_required: 'Account refresh required' })[entry.route.credential.state];
}
export function useProviderConnections(client: Client, enabled: boolean) {
  const runtime = useRuntime();
  const host = client.runtimeID;
  const inventory = useQuery({ queryKey: ['provider-list', host], queryFn: ({ signal }) => client.listProviders({ signal }), enabled });
  const presets = useQuery({ queryKey: ['provider-presets', host], queryFn: ({ signal }) => client.providerPresets({ signal }), enabled });
  const entries = useMemo(() => {
    const values = new Map<string, ProviderEntry>((presets.data?.items ?? []).map(preset => [preset.id, { id: preset.id, name: preset.name, preset }]));
    for (const route of inventory.data?.routes ?? []) values.set(route.id, { ...values.get(route.id), id: route.id, name: values.get(route.id)?.name ?? route.id, route });
    return [...values.values()];
  }, [inventory.data, presets.data]);
  const selection = inventory.data?.defaults;
  const readiness = useQuery({ queryKey: ['provider-readiness', host, selection], queryFn: ({ signal }) => {
    if (!selection) throw new Error('Choose a default model first');
    return client.providerReadiness(selection, { signal });
  }, enabled: enabled && !!selection });
  const ready = !inventory.data ? undefined : !selection ? false : providerReady(readiness.data);
  useEffect(() => { if (ready !== undefined && (!selection || readiness.data)) rememberProviderReady(runtime.platform.storage, host, ready); }, [ready, selection, readiness.data, runtime.platform.storage, host]);
  const flows = useQuery({ queryKey: ['provider-login-flows', host], queryFn: ({ signal }) => readLoginFlows(client, signal), enabled,
    refetchInterval: query => enabled && query.state.data?.some(flow => pollingLoginStates.includes(flow.value.state)) ? 2000 : false });
  const refresh = useCallback(async () => { await runtime.queries.invalidateQueries({ predicate: query => query.queryKey.includes(host) && query.queryKey[0] !== 'provider-login-flows' }); }, [host, runtime.queries]);
  const finished = flows.data?.filter(flow => flow.value.state === 'succeeded').map(flow => flow.value.id).join(',');
  useEffect(() => { if (finished) void refresh(); }, [finished, refresh]);
  return { inventory, presets, entries, readiness, ready, flows, refresh, lastKnownReady: recallProviderReady(runtime.platform.storage, host) };
}

export function ProviderConnectionRow({ entry, enabled, connect, onSelect, setup = false, hostName }: {
  entry: ProviderEntry; enabled: boolean; connect: boolean; onSelect(): void; setup?: boolean; hostName?: string;
}) {
  const action = connect ? 'Connect' : setup ? 'Use' : 'Manage';
  return <div {...stylex.props(styles.row, setup && styles.setupRow)}>
    <div {...stylex.props(styles.identity, setup && styles.setupIdentity)}><span {...stylex.props(styles.logoSlot)}><ProviderLogo id={entry.id} size={20} /></span>
      <div {...stylex.props(styles.details)}><div {...stylex.props(styles.nameLine)}><strong {...stylex.props(styles.name)}>{entry.name}</strong>{setup && entry.id === 'inference-net' && entry.preset?.base_url === keySetupEndpoints[entry.id] && (!entry.route || entry.route.base_url === entry.preset.base_url && entry.route.kind === entry.preset.kind) && <Badge>Recommended</Badge>}{sourceLabel(entry) && <Badge>{sourceLabel(entry)}</Badge>}{!entry.preset && <Badge>Custom</Badge>}</div>
        <span {...stylex.props(styles.description)}>{stateLabel(entry)}{setup && hostName ? ` · ${hostName}` : ''}</span></div></div>
    <Button data-provider-choice={setup || undefined} variant={connect ? 'secondary' : 'ghost'} disabled={!enabled} aria-label={`${action} ${entry.name}`} onClick={onSelect}>{connect && <Plus size={15} />}{action}</Button>
  </div>;
}
export function ProviderConnectionList({ entries, enabled, hostName, onSelect, title, actions, refresh, onExpandedChange, setup = true }: {
  entries: ProviderEntry[]; enabled: boolean; hostName: string; onSelect(entry: ProviderEntry): void; title?: string; actions?: ReactNode; refresh?: ReactNode; setup?: boolean; onExpandedChange?(expanded: boolean): void;
}) {
  const [showAll, setShowAll] = useState(false);
  useEffect(() => () => onExpandedChange?.(false), [onExpandedChange]);
  const common = entries.filter(entry => ['inference-net', 'openrouter', 'openai', 'openai-codex'].includes(entry.id) || entry.route);
  const visible = showAll ? entries : common.length ? common : entries.slice(0, 4);
  return <>{!!visible.length && <SettingsGroup title={title} action={refresh} panelXstyle={styles.choicePanel}>{visible.map(entry => <ProviderConnectionRow key={entry.id} setup={setup} entry={entry} hostName={hostName} enabled={enabled} connect={setup ? !locallyAvailable(entry) : !entry.route} onSelect={() => onSelect(entry)} />)}</SettingsGroup>}
    <div {...stylex.props(styles.choiceActions)}>{actions}<Button variant="ghost" aria-expanded={showAll} onClick={() => { onExpandedChange?.(!showAll); setShowAll(!showAll); }}>{showAll ? 'Show fewer providers' : 'Show all providers'}{showAll ? <ChevronUp size={14} /> : <ChevronDown size={14} />}</Button>{!title && showAll && refresh}</div></>;
}
export function ProviderConnections({ client, enabled }: { client: Client; enabled: boolean }) {
  const runtime = useRuntime();
  const hostName = runtime.connections.host(client.runtimeID)?.name ?? 'this execution host';
  const connections = useProviderConnections(client, enabled);
  const { inventory, presets, entries, flows, refresh } = connections;
  const [selected, select] = useState<string>();
  const [notice, setNotice] = useState('');
  const active = entries.find(entry => entry.id === selected) ?? (selected === '' ? { id: '', name: 'Custom provider' } : undefined);
  return <div {...stylex.props(layout.column)}>
    <div id="providers" tabIndex={-1} {...stylex.props(layout.column)}>
      {(inventory.isPending || presets.isPending) && <p role="status">Loading providers…</p>}
      <ErrorNotice type="resource" owner={`${client.runtimeID}:providers`} title="Could not load providers" error={inventory.error || presets.error} />
      <ProviderConnectionList setup={false} entries={entries} enabled={enabled} hostName={hostName} onSelect={entry => select(entry.id)} title="Providers" refresh={<Button variant="ghost" disabled={!enabled || inventory.isFetching} onClick={() => void refresh()}>Refresh local status</Button>} actions={<Button disabled={!enabled} onClick={() => select('')}>Add custom endpoint</Button>} />
      <p {...stylex.props(styles.description)}>Credential status is local evidence. Model access and inference have not been tested.</p>
      {flows.data?.filter(flow => !terminalLoginStates.includes(flow.value.state) || ['uncertain', 'interrupted'].includes(flow.value.state)).map(flow => <Button key={flow.value.id} disabled={!enabled} onClick={() => select(flow.provider)}>Review {flow.provider} sign-in</Button>)}
      <ErrorNotice type="resource" owner={`${client.runtimeID}:sign-ins`} title="Could not load sign-in progress" error={flows.error} />
      {notice && <p role="status">{notice}</p>}
    </div>
    <ProviderDefaultsSettings client={client} enabled={enabled} />
    {active && inventory.data && <ProviderConnectionDialog key={active.id} client={client} entry={active} enabled={enabled} revision={inventory.data.revision} hostName={hostName} flows={flows.data ?? []} refresh={refresh} refreshFlows={async () => { await flows.refetch(); }} close={message => { select(undefined); if (message) setNotice(message); }} />}
  </div>;
}

export function ProviderConnectionDialog({ client, entry, enabled, revision, hostName, flows, refresh, refreshFlows, close, onConnected }: {
  client: Client; entry: ProviderEntry; enabled: boolean; revision: string; hostName: string; flows: AccountFlow[];
  refresh(): Promise<void>; refreshFlows(): Promise<void>; close(message?: string): void; onConnected?(message?: string): void;
}) {
  const runtime = useRuntime();
  const [base, setBase] = useState({ revision, route: entry.route });
  const [key, setKey] = useState('');
  const publication = useRef<{ key: string; id: string } | null>(null);
  const [id, setID] = useState(entry.id);
  const [url, setURL] = useState(entry.route?.base_url ?? entry.preset?.base_url ?? '');
  const [kind, setKind] = useState(entry.route?.kind ?? entry.preset?.kind ?? 'openai-chat');
  const [source, setSource] = useState(entry.route ? 'keep' : entry.preset?.methods.some(method => method === 'login') ? 'account' : 'key');
  const [environment, setEnvironment] = useState(entry.route?.credential.environment || entry.preset?.environments[0] || '');
  const [file, setFile] = useState(entry.route?.credential.file ?? '');
  const [command, setCommand] = useState('{"executable":"","arguments":[],"environment":[]}');
  const [discard, setDiscard] = useState(false);
  const [flowID, setFlowID] = useState<string>();
  const [hiddenFlow, setHiddenFlow] = useState<string>();
  const [autoOpen, setAutoOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [remove, setRemove] = useState(false);
  const [removeError, setRemoveError] = useState('');
  const [restartLogin, setRestartLogin] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => { setBusy(false); request.current = null; return () => request.current?.abort(); }, [client]);
  useSettingsEdits({ id: 'provider_key', dirty: !!key || source === 'command', description: 'Unsaved provider credential', discard: () => { setKey(''); setCommand(''); setSource(entry.route ? 'keep' : 'account'); publication.current = null; } });
  const managed = entry.id === 'inference-net' || entry.id === 'openai-codex';
  const account = useQuery({ queryKey: ['provider-account', client.runtimeID, entry.id], queryFn: async ({ signal }) => entry.id === 'inference-net'
    ? { provider: 'inference-net' as const, value: await client.inferenceAccountStatus({ signal }) } : { provider: 'openai-codex' as const, value: await client.openAIAccountStatus({ signal }) }, enabled: enabled && managed });
  const cleanup = useQuery({ queryKey: ['provider-cleanup', client.runtimeID], queryFn: ({ signal }) => client.listInferenceCleanup({ signal }), enabled: enabled && entry.id === 'inference-net' });
  const accountFlows = flows.filter(flow => flow.provider === entry.id);
  const flow = accountFlows.find(flow => flow.value.id === flowID) ?? accountFlows.find(flow => !terminalLoginStates.includes(flow.value.state) || ['uncertain', 'interrupted'].includes(flow.value.state));
  const visibleFlow = flow && flow.value.id !== hiddenFlow ? flow : undefined;
  async function updateFlow(value: AccountFlow) {
    await runtime.queries.cancelQueries({ queryKey: ['provider-login-flows', client.runtimeID] });
    runtime.queries.setQueryData<AccountFlow[]>(['provider-login-flows', client.runtimeID], previous => [value, ...(previous ?? []).filter(item => item.value.id !== value.value.id)]);
    setFlowID(value.value.id); setHiddenFlow(undefined);
  }
  async function action(run: (signal: AbortSignal) => Promise<void>, owner: 'provider' | 'remove' = 'provider') {
    if (!enabled || request.current) return;
    const failure = owner === 'remove' ? setRemoveError : setError;
    const controller = new AbortController(); request.current = controller; setBusy(true); failure(''); setNotice('');
    try { await run(controller.signal); }
    catch (error) { if (!controller.signal.aborted) { failure(errorMessage(error)); await Promise.allSettled([refresh(), refreshFlows()]); } }
    finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setBusy(false); }
  }
  const changed = async (message: string, signal: AbortSignal) => { await refresh(); if (!signal.aborted) { setNotice(message); onConnected?.(message); } };
  const start = () => void action(async signal => {
    const current = await readLoginFlows(client, signal);
    const existing = current.find(flow => flow.provider === entry.id && (!terminalLoginStates.includes(flow.value.state) || ['uncertain', 'interrupted'].includes(flow.value.state)));
    if (existing && ['uncertain', 'interrupted'].includes(existing.value.state)) { setRestartLogin(true); return; }
    const next: AccountFlow = existing ?? (entry.id === 'inference-net' ? { provider: 'inference-net', value: await client.beginInferenceLogin({ signal }) } : { provider: 'openai-codex', value: await client.beginOpenAILogin({ signal }) });
    if (!signal.aborted) { setAutoOpen(true); await updateFlow(next); }
  });
  const keyProvider = id === 'openrouter' || id === 'inference-net' ? id : null;
  const validatedSetup = !!keyProvider && keySetupEndpoints[id] === url && kind === 'openai-chat' && (source === 'key' || source === 'env' && environment === entry.preset?.environments[0]);
  const save = () => void action(async signal => {
    if (revision !== base.revision) throw new Error('Review the changed host revision before saving these edits');
    let credential: ProviderCredentialInput | null = null;
    let keyInput: ChangeProviderParams['key'] = null;
    if (source === 'key') {
      if (!key.trim()) throw new Error('Enter an API key');
      if (publication.current?.key !== key.trim()) publication.current = { key: key.trim(), id: crypto.randomUUID() };
      keyInput = publication.current; credential = { source: 'file', environment: '', file: '', command: null };
    } else if (source !== 'keep') {
      if (!['env', 'file', 'command', 'none'].includes(source)) throw new Error('Choose a credential source');
      credential = { source: source as ProviderCredentialInput['source'], environment: source === 'env' ? environment : '', file: source === 'file' ? file : '', command: source === 'command' ? JSON.parse(command) : null };
    }
    const params: ChangeProviderParams = { revision: base.revision, provider: id, declaration: { kind, base_url: url, credential, models: base.route?.models ?? {} }, keep_credential: source === 'keep', key: keyInput };
    const result = validatedSetup && keyProvider ? await client.setupProviderKey({ revision: base.revision, provider: keyProvider, key: keyInput, environment: source === 'env' }, { signal }) : entry.route ? await client.updateProvider(params, { signal }) : await client.createProvider(params, { signal });
    if (signal.aborted) return;
    setBase({ revision: result.revision, route: result.routes.find(route => route.id === id) }); runtime.queries.setQueryData(['provider-list', client.runtimeID], result); setKey(''); publication.current = null;
    await changed(validatedSetup ? 'Credentials checked by model discovery. Inference has not been tested. Your default model is unchanged.' : 'Provider saved. Your default model is unchanged.', signal);
  });
  const leave = () => { if (key || source === 'command') setDiscard(true); else close(); };
  return <Dialog open xstyle={loginStyles.dialog} bodyXstyle={loginStyles.body} title={<span {...stylex.props(styles.nameLine)}><ProviderLogo id={entry.id} />{entry.name}</span>} description={`Connection on ${hostName}`} onOpenChange={open => { if (!open) leave(); }}>
    {!enabled && <Alert tone="warning">{hostName} is unavailable. Reconnect to continue; your draft is kept here.</Alert>}
    {visibleFlow ? <LoginFlow flow={visibleFlow} client={client} enabled={enabled} hostName={hostName} autoOpen={autoOpen} update={updateFlow} refresh={async () => { await Promise.all([refresh(), refreshFlows()]); }} leave={message => { setHiddenFlow(visibleFlow.value.id); if (message) setNotice(message); }} /> : <div {...stylex.props(layout.column)}>
      <p>{stateLabel(entry)}{sourceLabel(entry) && ` · ${sourceLabel(entry)}`}. Inference has not been tested.</p>
      {account.data && <><p>{account.data.value.email ?? 'No saved account email'}{account.data.provider === 'inference-net' ? ` · ${account.data.value.management_state} management session · ${account.data.value.inference_state} inference key` : ` · ${account.data.value.auth_state}`}</p><p>Account route: {account.data.value.route_state}.</p><ErrorNotice type="resource" owner={`${entry.id}:account-state`} title="Account needs attention" error={account.data.value.failure} /></>}
      <ErrorNotice type="resource" owner={`${entry.id}:account`} title="Could not read account status" error={account.error} />
      {managed && <div {...stylex.props(layout.row)}><Button disabled={!enabled || busy} onClick={start}>Sign in{entry.id === 'inference-net' ? ' with Inference.net' : ' with ChatGPT'}</Button>
        <Button disabled={!enabled || busy} onClick={() => void action(async signal => { if (entry.id === 'inference-net') await client.setupInferenceAccount({ signal }); else await client.setupOpenAIAccount({ signal }); await changed('Saved account route configured. Defaults are unchanged.', signal); })}>Use saved account</Button>
        <Button disabled={!enabled || busy} onClick={() => void action(async signal => {
          if (entry.id === 'inference-net') { const result = await client.logoutInferenceAccount({ signal }); await refresh(); if (result.local_failure) throw new Error(result.local_failure); await changed('Local account disconnected.', signal); if (result.cleanup_failure && !signal.aborted) setError(`Remote cleanup needs attention: ${result.cleanup_failure}`); }
          else { await client.logoutOpenAIAccount({ signal }); await changed('Local account disconnected.', signal); }
        })}>Sign out</Button>
        {entry.id === 'inference-net' && <Button disabled={!enabled || busy} onClick={() => void action(async signal => { const value = await client.rotateInferenceKey({ signal }); if (!signal.aborted) await updateFlow({ provider: 'inference-net', value }); })}>Rotate machine key</Button>}
      </div>}
      {!!cleanup.data?.items.length && <><p>Retained account cleanup: {cleanup.data.items.map(item => `${item.key_state} key, ${item.session_state} session`).join('; ')}.</p><Button disabled={!enabled || busy} onClick={() => void action(async signal => { const result = await client.retryInferenceCleanup({ signal }); if (result.failure) throw new Error(result.failure); await changed('Cleanup checked. Review retained records for its outcome.', signal); })}>Retry account cleanup</Button></>}
      <ErrorNotice type="resource" owner={`${entry.id}:cleanup`} title="Cleanup needs attention" error={cleanup.error || cleanup.data?.failure} />
      {entry.id !== 'openai-codex' && <form onSubmit={event => { event.preventDefault(); save(); }} {...stylex.props(layout.column)}>
        {!entry.id && <Field label="Provider id"><Input value={id} disabled={!enabled || busy} onChange={event => setID(event.target.value)} /></Field>}
        <Field label="Endpoint" description="Changing the endpoint or adapter requires choosing its credential source again."><Input value={url} disabled={!enabled || busy} onChange={event => setURL(event.target.value)} /></Field>
        <Field label="API adapter"><Select label="API adapter" value={kind} disabled={!enabled || busy} onValueChange={value => { if (value === 'openai-chat' || value === 'openai-responses') setKind(value); }} options={[{ value: 'openai-chat', label: 'Chat Completions' }, { value: 'openai-responses', label: 'Responses' }]} /></Field>
        <Field label="Credential source"><Select label="Credential source" value={source} disabled={!enabled || busy} onValueChange={setSource} options={[...(entry.route ? [{ value: 'keep', label: 'Keep current source' }] : []), { value: 'key', label: 'Paste API key' }, { value: 'env', label: 'Environment variable' }, { value: 'file', label: 'Private file' }, { value: 'command', label: 'Explicit command' }, { value: 'none', label: 'No authentication' }, ...(managed ? [{ value: 'account', label: 'Account sign-in above' }] : [])]} /></Field>
        {source === 'key' && <Field label="API key" description={`Enter an API key, or choose an environment variable${environment ? ` such as ${environment}` : ''}. Restart the host after changing its environment.`}><Input type="password" autoComplete="off" spellCheck={false} maxLength={65536} value={key} disabled={!enabled || busy} onChange={event => setKey(event.target.value)} /></Field>}
        {source === 'env' && <Field label="Environment variable"><Input value={environment} disabled={!enabled || busy} onChange={event => setEnvironment(event.target.value)} /></Field>}
        {source === 'file' && <Field label="Private key file"><Input value={file} disabled={!enabled || busy} onChange={event => setFile(event.target.value)} /></Field>}
        {source === 'command' && <Field label="Credential command JSON" description="Explicit executable, argument list, and environment variable names. Opening Settings does not run it."><Textarea value={command} disabled={!enabled || busy} onChange={event => setCommand(event.target.value)} /></Field>}
        {source === 'keep' && <p>{entry.route?.credential.environment || entry.route?.credential.file || sourceLabel(entry)}</p>}
        {base.revision !== revision && <p role="status">Host settings changed. Your edits are preserved. <Button disabled={busy} onClick={() => setBase({ revision, route: entry.route })}>Review current revision and keep these edits</Button></p>}
        <Button type="submit" variant="primary" disabled={!enabled || busy || base.revision !== revision || source === 'account' || !id || !url}>{validatedSetup ? 'Connect' : 'Save provider'}</Button>
      </form>}
      {entry.route && <Button disabled={!enabled || busy} onClick={() => void action(async signal => { const result = await client.refreshProviderCatalog(entry.id, { signal }); await refresh(); if (result.failure) throw new Error(result.failure); if (!signal.aborted) setNotice(`Catalog: ${result.discovery}. Inference has not been tested.`); })}>Refresh model catalog</Button>}
      {entry.route && <Button disabled={!enabled || busy} onClick={() => { setRemoveError(''); setRemove(true); }}>Remove configured route</Button>}
      <Button onClick={leave}>Done</Button>
    </div>}
    <ErrorNotice type="action" owner={`provider:${entry.id}`} title="Could not update provider connection" error={error} />{notice && <Alert tone="success">{notice}</Alert>}
    {restartLogin && <Dialog open title="Start another sign-in?" description="The previous account change has an uncertain outcome. Inspect the account and its existing keys or projects before continuing; starting again may create another credential." onOpenChange={setRestartLogin} footer={<><Button onClick={() => setRestartLogin(false)}>Back</Button><Button disabled={!enabled || busy} onClick={() => void action(async signal => { const next: AccountFlow = entry.id === 'inference-net' ? { provider: 'inference-net', value: await client.beginInferenceLogin({ signal }) } : { provider: 'openai-codex', value: await client.beginOpenAILogin({ signal }) }; if (!signal.aborted) { setRestartLogin(false); setAutoOpen(true); await updateFlow(next); } })}>Start new sign-in</Button></>} />}
    {discard && <Dialog open title="Discard this credential?" description="This unsaved credential is held only in this form." onOpenChange={setDiscard} footer={<><Button onClick={() => setDiscard(false)}>Keep editing</Button><Button onClick={() => { setKey(''); publication.current = null; close(); }}>Discard credential</Button></>} />}
    {remove && <Dialog open title="Remove this provider route?" description="Saved key files and remote accounts are preserved. Choose a different default first if this route is in use." onOpenChange={setRemove} footer={<><Button onClick={() => setRemove(false)}>Cancel</Button><Button disabled={!enabled || busy} onClick={() => void action(async signal => { await client.removeProvider({ revision, provider: entry.id, replacement: null }, { signal }); if (signal.aborted) return; close('Provider route removed. Credential files are unchanged.'); await refresh(); }, 'remove')}>Remove route</Button></>}><ErrorNotice type="action" owner={`provider:${entry.id}:remove`} title="Could not remove provider route" error={removeError} /></Dialog>}
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
