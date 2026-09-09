import type { ReactNode, RefObject } from 'react';
import { useEffect, useRef, useState } from 'react';
import { Alert, AlertDialog, Button, Collapsible, Dialog, Field, IconButton, Input, Menu, SettingsRow, StatusIndicator, Switch, Tabs, type MenuItem, type DialogProps } from '@whip/ui';
import { Globe, Monitor, MoreHorizontal, Plus, Terminal } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { colors, scale, surface, typography } from '@whip/ui/tokens.stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { errorMessage, type AppLocalRuntime, type LocalRuntimeStatus } from './platform';
import type { HostConnection } from './hosts';
import { layout } from './styles';
import { settingsSection } from './settings/section-layout';
import { SSHFields } from './connection-dialog';
import { daemonEndpoint, localProfile, type ConnectionProfile, type ConnectionTarget } from './connections';

type ServerForm = { editing?: HostConnection; legacy?: ConnectionProfile; endpoint?: string };
type HostDialogProps = ServerForm & { open: boolean; onOpenChange(open: boolean): void; onSaved?(id: string): void; finalFocus?: DialogProps['finalFocus'] };

/** Focused add/edit surface, also used directly by onboarding. */
export function HostDialog(props: HostDialogProps) {
  const [pending, setPending] = useState(false);
  const address = useRef<HTMLInputElement>(null);
  useEffect(() => { if (!props.open) setPending(false); }, [props.open]);
  return <Dialog open={props.open} onOpenChange={open => { if (!pending) props.onOpenChange(open); }}
    title={props.editing ? 'Edit server' : 'Add server'} initialFocus={address} finalFocus={props.finalFocus}>
    {props.open && <ServerFormFields {...props} addressRef={address} pending={pending} onPendingChange={setPending} />}
  </Dialog>;
}

function ServerFormFields({ editing, legacy, endpoint: initialEndpoint, onSaved, onOpenChange, addressRef, pending, onPendingChange }: HostDialogProps & {
  addressRef: RefObject<HTMLInputElement | null>; pending: boolean; onPendingChange(value: boolean): void;
}) {
  const runtime = useRuntime();
  const state = useAppState();
  const mounted = useRef(true);
  const submitting = useRef(false);
  const nativeSave = useRef<AbortController | undefined>(undefined);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; nativeSave.current?.abort(); }; }, []);
  useEffect(() => { void runtime.connections.refreshProfiles().catch(() => {}); }, [runtime]);
  const [name, setName] = useState(editing?.name ?? legacy?.label ?? '');
  const [kind, setKind] = useState<'url' | 'ssh' | 'local'>(editing?.profile.target.kind ?? 'url');
  const [ssh, setSSH] = useState<Extract<ConnectionTarget, { kind: 'ssh' }>>(editing?.profile.target.kind === 'ssh' ? editing.profile.target : { kind: 'ssh', host: '' });
  const [endpoint, setEndpoint] = useState(editing?.endpoint ?? initialEndpoint ?? (legacy?.target.kind === 'url' ? legacy.target.endpoint : ''));
  const [connectOnLaunch, setConnectOnLaunch] = useState(editing?.connectOnLaunch ?? true);
  const [acceptIdentity, setAcceptIdentity] = useState(false);
  const [error, setError] = useState('');
  const canSave = state.home?.state === 'connected' && state.profilesReady;
  const nativeSSH = runtime.platform.connectionKinds?.includes('ssh') ?? false;
  const unsupported = kind === 'ssh' && !nativeSSH;
  const nameField = <Field label="Server name (optional)"><Input value={name} disabled={pending} onChange={event => setName(event.target.value)} placeholder={kind === 'ssh' ? 'Use the SSH host' : 'Use the server address'} /></Field>;
  const connectionFields = <div {...stylex.props(layout.column)}>
    {kind === 'url' && <>
      <Field label="Server address" description="The HTTP or HTTPS address of a running Whip server."><Input ref={addressRef} value={endpoint} disabled={pending} onChange={event => setEndpoint(event.target.value)} placeholder="https://server.example.com" required /></Field>
      {nameField}
    </>}
    {kind === 'ssh' && (nativeSSH
      ? <SSHFields target={ssh} busy={pending} onChange={setSSH} hostRef={addressRef}>{nameField}</SSHFields>
      : <Alert title="SSH requires Whip desktop" action={<Button type="button" variant="secondary" onClick={() => setKind('url')}>Use server URL</Button>}>
          Connect with your SSH configuration and keys in the desktop app. Browsers cannot open SSH connections. To connect here, use the HTTP or HTTPS address of a running Whip server.
        </Alert>)}
  </div>;
  return <form {...stylex.props(layout.column)} onSubmit={event => {
    event.preventDefault(); event.stopPropagation();
    if (submitting.current || unsupported || (kind === 'url' && !canSave)) return;
    submitting.current = true; onPendingChange(true); setError('');
    void (async () => {
      try {
        const url = kind === 'url' ? daemonEndpoint(endpoint) : '';
        const label = name.trim() || (kind === 'url' ? new URL(url).host : ssh.host.trim());
        nativeSave.current = kind === 'url' ? undefined : new AbortController();
        const id = kind === 'url'
          ? await runtime.connections.save({ id: editing?.id ?? crypto.randomUUID(), name: label, url, runtime_id: editing?.runtimeId ?? legacy?.runtimeId, connect_on_launch: connectOnLaunch }, acceptIdentity, legacy?.id)
          : await runtime.connections.saveNative({ id: editing?.id ?? `ssh:${crypto.randomUUID()}`, label, target: ssh, runtimeId: editing?.runtimeId, ...(kind === 'local' ? { ...localProfile, runtimeId: editing?.runtimeId } : {}) }, acceptIdentity, nativeSave.current?.signal);
        if (legacy) await runtime.connections.forgetLegacyProfile(legacy.id);
        if (mounted.current) runtime.connections.select(id);
        void runtime.connections.connect(id).catch(() => {});
        if (state.legacyHosts.includes(endpoint)) runtime.forgetLegacyHost(endpoint);
        if (mounted.current) { onSaved?.(id); onOpenChange(false); }
      } catch (error) { if (mounted.current) setError(errorMessage(error)); }
      finally { submitting.current = false; if (mounted.current) onPendingChange(false); }
    })();
  }}>
    {nativeSSH && !editing && !legacy ? <Tabs label="Connection method" value={kind} onValueChange={value => { if (!pending) { setKind(value as 'url' | 'ssh'); setError(''); } }} items={[
      { value: 'url', label: <><Globe size={15} aria-hidden />Server URL</>, disabled: pending, content: kind === 'url' ? connectionFields : null },
      { value: 'ssh', label: <><Terminal size={15} aria-hidden />SSH</>, disabled: pending, content: kind === 'ssh' ? connectionFields : null },
    ]} /> : connectionFields}
    {!unsupported && (kind !== 'ssh' || editing || legacy?.runtimeId) && <Collapsible title="Advanced"><div {...stylex.props(layout.column)}>
      {kind === 'url' && <Switch label="Connect when the app opens" checked={connectOnLaunch} disabled={pending} onCheckedChange={setConnectOnLaunch} />}
      {(editing || legacy?.runtimeId) && <Switch label="Accept a new daemon identity" description="Use only when this address intentionally points to a replacement daemon. Existing tabs keep their original identity." checked={acceptIdentity} disabled={pending} onCheckedChange={setAcceptIdentity} />}
    </div></Collapsible>}
    {kind === 'url' && !canSave && <Alert tone="warning">Connect Local and load its server profiles to save URL servers.</Alert>}
    {error && <Alert tone="error">{error}</Alert>}
    <div {...stylex.props(layout.row, styles.footer)}>
      <Button type="button" variant="ghost" disabled={pending && kind === 'url'} onClick={() => { if (pending) nativeSave.current?.abort(); else onOpenChange(false); }}>{pending && kind !== 'url' ? 'Cancel connection' : 'Cancel'}</Button>
      {!unsupported && <Button type="submit" variant="primary" loading={pending} disabled={pending || (kind === 'url' && !canSave)}>{pending ? kind === 'url' ? 'Verifying and saving…' : 'Saving and connecting…' : editing ? 'Save changes' : 'Add server'}</Button>}
    </div>
  </form>;
}

/** The canonical server list lives in Settings, never inside the add dialog. */
export function ServerManager({ header }: { header?: ReactNode } = {}) {
  const runtime = useRuntime();
  const state = useAppState();
  const tabs = useSessionTabs();
  const returnFocus = useRef<HTMLButtonElement | null>(null);
  const addButton = useRef<HTMLButtonElement | null>(null);
  const triggers = useRef<Record<string, HTMLButtonElement | null>>({});
  const [form, setForm] = useState<ServerForm>();
  const [local, setLocal] = useState<string>();
  const [removing, setRemoving] = useState<HostConnection>();
  const [pending, setPending] = useState(false);
  const [localPending, setLocalPending] = useState(false);
  const [error, setError] = useState('');
  const active = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => { void runtime.connections.refreshProfiles().catch(() => {}); }, [runtime]);
  const canSave = state.home?.state === 'connected' && state.profilesReady;
  const action = async (run: () => Promise<unknown>) => {
    if (active.current) return;
    active.current = true; setPending(true); setError('');
    try { await run(); } catch (error) { if (mounted.current) setError(errorMessage(error)); }
    finally { active.current = false; if (mounted.current) setPending(false); }
  };
  const localHost = state.hosts.find(host => host.id === local);
  return <div {...stylex.props(layout.column)}>
    <div {...stylex.props(layout.row, layout.wrap)}>
      <h1 id="settings-title" {...stylex.props(layout.grow, styles.heading)}>Servers</h1>
      <Button variant="primary" disabled={pending || localPending} ref={addButton} onClick={event => { returnFocus.current = event.currentTarget; setForm({}); }}><Plus size={16} aria-hidden />Add server</Button>{header}
    </div>
    <div aria-label="Saved servers" {...stylex.props(layout.column)}>
      {state.hosts.map(host => {
        const items: MenuItem[] = [
          { id: 'connect', label: host.state === 'connecting' ? 'Cancel connection' : host.client ? 'Disconnect' : 'Connect', onSelect: () => {
            // Do not hold a pending lock across connection setup: Cancel must stay reachable.
            if (host.client || host.state === 'connecting') runtime.connections.disconnect(host.id);
            else { setError(''); void runtime.connections.connect(host.id).then(() => runtime.connections.select(host.id)).catch(error => { if (mounted.current) setError(errorMessage(error)); }); }
          } },
          ...(!host.local || host.device ? [{ id: 'edit', label: 'Edit server', disabled: host.state === 'connecting', onSelect: () => setForm({ editing: host }) }] : []),
          ...(host.profile.target.kind === 'local' && runtime.platform.localRuntime ? [{ id: 'local', label: 'Local server settings', onSelect: () => setLocal(host.id) }] : []),
          ...(!host.local ? [{ id: 'remove', label: 'Remove server', danger: true, disabled: host.state === 'connecting' || (!host.device && !canSave), onSelect: () => { setError(''); setRemoving(host); } }] : []),
        ];
        const address = host.profile.target.kind === 'local' ? 'This device' : host.profile.target.kind === 'ssh' ? `SSH · ${host.profile.target.host}` : host.endpoint;
        const status = host.state === 'closed' ? 'Disconnected' : host.state[0].toUpperCase() + host.state.slice(1);
        const HostIcon = host.local ? Monitor : host.profile.target.kind === 'ssh' ? Terminal : Globe;
        return <div key={host.id} {...stylex.props(styles.server)}>
          <SettingsRow xstyle={settingsSection.rowContent} label={<span {...stylex.props(styles.identity)}>
            <HostIcon size={20} aria-hidden {...stylex.props(styles.icon)} />
            <span {...stylex.props(styles.name)}>{host.name}</span>
          </span>} description={<span {...stylex.props(styles.address)}>{address}</span>}>
            <div {...stylex.props(layout.row, layout.wrap)}>
              <StatusIndicator tone={host.state === 'connected' ? 'success' : host.error ? 'error' : host.state === 'incompatible' ? 'warning' : 'neutral'}>{status}</StatusIndicator>
              <Menu onOpenChange={open => { if (open) returnFocus.current = triggers.current[host.id]; }} trigger={<IconButton ref={element => { if (element) triggers.current[host.id] = element; else delete triggers.current[host.id]; }} label={`Actions for ${host.name}`} variant="ghost" disabled={pending || localPending}><MoreHorizontal size={18} /></IconButton>} items={items} />
            </div>
          </SettingsRow>
          {host.progress && <p role="status" {...stylex.props(styles.runtimeMessage)}>{host.progress}</p>}
          {host.error && <p role="status" {...stylex.props(styles.runtimeMessage, styles.address)}>{host.error}</p>}
        </div>;
      })}
    </div>
    {state.profileError && <p role="status">{state.profileError}</p>}
    {error && !removing && <p role="alert">{error}</p>}
    <HostDialog finalFocus={returnFocus} open={!!form} {...form} onOpenChange={open => { if (!open) setForm(undefined); }} />
    <AlertDialog open={!!removing} onOpenChange={open => { if (!open && !pending) setRemoving(undefined); }} title={`Remove ${removing?.name ?? 'server'}?`}
      description="This removes the saved connection, not daemon sessions. Tabs and drafts stay on their original server." finalFocus={() => returnFocus.current?.isConnected ? returnFocus.current : addButton.current} confirmLabel="Remove server" danger loading={pending}
      onConfirm={() => { if (removing) void action(async () => { await runtime.connections.remove(removing.id); if (mounted.current) setRemoving(undefined); }); }}>
      {error && <p role="alert">{error}</p>}
    </AlertDialog>
    <Dialog open={!!localHost} onOpenChange={open => { if (!open && !localPending) setLocal(undefined); }} title="Local server settings" finalFocus={returnFocus}>
      {localHost && runtime.platform.localRuntime && <LocalRuntimePanel api={runtime.platform.localRuntime} hostState={localHost.state} disabled={pending} onBusyChange={setLocalPending} />}
    </Dialog>
    {!!runtime.connections.getSnapshot().legacyProfiles?.length && <Collapsible title="Import desktop addresses">
      <div {...stylex.props(layout.column)}>{runtime.connections.getSnapshot().legacyProfiles!.map(profile => <Button key={profile.id} variant="ghost" disabled={pending || localPending} onClick={event => { returnFocus.current = event.currentTarget; setForm({ legacy: profile }); }}>{profile.label}</Button>)}</div>
      <p {...stylex.props(layout.muted)}>Save to verify the existing daemon identity and move this address into the shared configuration. Your tabs and drafts remain on their original host.</p>
    </Collapsible>}
    {!!tabs.previous.length && <Collapsible title="Restore previous host tabs">
      <div {...stylex.props(layout.column)}>{tabs.previous.map(entry => <div key={entry.runtimeId} {...stylex.props(layout.column)}><div {...stylex.props(layout.row)}>
        <span {...stylex.props(layout.grow)}>{runtime.connections.host(entry.runtimeId)?.name ?? entry.runtimeId} · {entry.workspace.tabs.length} tabs</span>
        <Button variant="ghost" onClick={() => { try { runtime.tabs.restorePrevious(entry.runtimeId); } catch (error) { setError(errorMessage(error)); } }}>Restore</Button>
        <Button variant="ghost" onClick={() => runtime.tabs.dismissPrevious(entry.runtimeId)}>Dismiss</Button>
      </div><Collapsible title="Open individual previous tabs"><div {...stylex.props(layout.column)}>{[...entry.workspace.tabs, ...entry.workspace.closed.map(item => item.tab)].map(tab => <Button key={tab.id} variant="ghost" onClick={() => { try { runtime.tabs.openPrevious(entry.runtimeId, tab.id); } catch (error) { setError(errorMessage(error)); } }}>{tab.titleHint || tab.rootId}{tab.kind === 'repl' ? ' · REPL' : ''}</Button>)}</div></Collapsible></div>)}</div>
      <p {...stylex.props(layout.muted)}>Restore when there is room in this window. Your saved sessions and drafts stay on their original host.</p>
    </Collapsible>}
    {!!state.legacyHosts.length && <Collapsible title="Import previously saved addresses">
      <div {...stylex.props(layout.column)}>{state.legacyHosts.map(url => <div key={url} {...stylex.props(layout.row)}>
        <Button variant="ghost" onClick={event => { returnFocus.current = event.currentTarget; setForm({ endpoint: url }); }}>{url}</Button>
        <Button variant="ghost" onClick={() => runtime.forgetLegacyHost(url)}>Dismiss</Button>
      </div>)}</div>
      <p {...stylex.props(layout.muted)}>Select an address and save to verify and import it.</p>
    </Collapsible>}
  </div>;
}

export function LocalRuntimePanel({ api, hostState, disabled, onBusyChange }: {
  api: AppLocalRuntime; hostState: HostConnection['state']; disabled: boolean; onBusyChange(busy: boolean): void;
}) {
  const mounted = useRef(true);
  const request = useRef(0);
  const active = useRef<keyof AppLocalRuntime | undefined>(undefined);
  const [pending, setPending] = useState<keyof AppLocalRuntime>();
  const [status, setStatus] = useState<LocalRuntimeStatus>();
  const [error, setError] = useState('');
  const [confirmRestart, setConfirmRestart] = useState(false);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; request.current++; onBusyChange(false); };
  }, [onBusyChange]);
  const run = async (method: keyof AppLocalRuntime) => {
    if (active.current && (active.current !== 'test' || method !== 'test')) return;
    const id = ++request.current;
    active.current = method; setPending(method); onBusyChange(true); setError(''); setConfirmRestart(false);
    try {
      const result = await api[method]();
      if (mounted.current && id === request.current) setStatus(result);
    } catch (error) {
      if (mounted.current && id === request.current) setError(errorMessage(error));
    } finally {
      if (mounted.current && id === request.current) {
        active.current = undefined; setPending(undefined); onBusyChange(false);
      }
    }
  };
  useEffect(() => {
    if (hostState !== 'connecting') void run('test');
  }, [api, hostState]);
  const busy = disabled || pending !== undefined;
  return <section aria-label="This Mac runtime" {...stylex.props(layout.column)}>
    <p role="status" {...stylex.props(styles.runtimeMessage)}>{pending === 'test' ? 'Locating whipcode, checking the installation and contacting the daemon…'
      : pending === 'choose' ? 'Choose the whipcode executable in the native file dialog…'
      : pending === 'install' ? 'Installing the verified whipcode runtime…'
      : pending === 'restart' ? 'Restarting the local daemon…'
      : status?.message ?? 'Test the local whipcode installation to see its status.'}</p>
    {error && <p role="alert" {...stylex.props(layout.error, styles.runtimeMessage)}>{error}</p>}
    <div {...stylex.props(layout.row, layout.wrap)}>
      <Button variant="secondary" disabled={busy} loading={pending === 'test'} onClick={() => void run('test')}>Test Connection</Button>
      <Button variant="ghost" disabled={busy} loading={pending === 'choose'} onClick={() => void run('choose')}>Choose executable</Button>
      {status?.canInstall && <Button variant="primary" disabled={busy} loading={pending === 'install'} onClick={() => void run('install')}>Install whipcode</Button>}
      {status?.executable && status.state !== 'missing' && status.state !== 'stopped' && <Button variant="ghost" disabled={busy} loading={pending === 'restart'} onClick={() => setConfirmRestart(true)}>Restart daemon</Button>}
    </div>
    {status?.state === 'stopped' && <p {...stylex.props(layout.muted, styles.runtimeMessage)}>Use Connect in the server menu to start this daemon. Test Connection does not start it.</p>}
    {confirmRestart && <div {...stylex.props(layout.column, layout.notice)}>
      <p {...stylex.props(styles.runtimeMessage)}>Restarting interrupts running work on This Mac, including work started from the CLI or web app. Existing sessions are retained.</p>
      <div {...stylex.props(layout.row, layout.wrap)}>
        <Button variant="danger" disabled={busy} onClick={() => void run('restart')}>Interrupt work and restart</Button>
        <Button variant="ghost" disabled={busy} onClick={() => setConfirmRestart(false)}>Cancel restart</Button>
      </div>
    </div>}
    {status && <Collapsible title="Runtime diagnostics">
      <dl {...stylex.props(styles.diagnostics)}>
        <dt>Last checked state</dt><dd>{status.state}</dd>
        <dt>Executable</dt><dd><code>{status.executable ?? 'No executable selected'}</code></dd>
        <dt>State directory</dt><dd><code>{status.home}</code></dd>
        <dt>Client build</dt><dd><code>{status.clientBuild ?? 'Unavailable'}</code></dd>
        <dt>Daemon build</dt><dd><code>{status.daemonBuild ?? 'Unavailable'}</code></dd>
      </dl>
    </Collapsible>}
  </section>;
}

const styles = stylex.create({
  heading: { margin: 0, fontSize: typography.size20, fontWeight: 560, lineHeight: 1.4 },
  identity: { display: 'flex', alignItems: 'center', gap: scale.space3, minWidth: 0 },
  icon: { flexShrink: 0, color: surface.secondaryText },
  name: { fontSize: typography.size14, fontWeight: 500, overflowWrap: 'anywhere' },
  address: { overflowWrap: 'anywhere' },
  footer: { justifyContent: 'flex-end' },
  server: { minWidth: 0, padding: scale.space4, borderWidth: 1, borderStyle: 'solid', borderColor: surface.quietBorder, borderRadius: scale.radiusDialog, backgroundColor: colors.panel },
  runtimeMessage: { margin: 0 },
  diagnostics: { display: 'grid', gridTemplateColumns: 'max-content minmax(0, 1fr)', gap: 8, overflowWrap: 'anywhere' },
});
