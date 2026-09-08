import { useEffect, useRef, useState } from 'react';
import { Button, Dialog, Input, Switch } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { errorMessage, type AppLocalRuntime, type LocalRuntimeStatus } from './platform';
import type { HostConnection } from './hosts';
import { layout } from './styles';
import { SSHFields } from './connection-dialog';
import { localProfile, type ConnectionProfile, type ConnectionTarget } from './connections';

export function HostDialog({ open, onOpenChange, onSaved }: { open: boolean; onOpenChange(open: boolean): void; onSaved?(id: string): void }) {
  return <Dialog open={open} onOpenChange={onOpenChange} title="Execution hosts" description="Connect to execution hosts. URL profiles are shared through Local’s configuration; SSH settings stay on this device.">
    {open && <HostManager onSaved={id => { onSaved?.(id); onOpenChange(false); }} />}
  </Dialog>;
}

function HostManager({ onSaved }: { onSaved(id: string): void }) {
  const runtime = useRuntime();
  const state = useAppState();
  const tabs = useSessionTabs();
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const [editing, setEditing] = useState<HostConnection>();
  const [name, setName] = useState('');
  const [kind, setKind] = useState<'url' | 'ssh' | 'local'>('url');
  const [ssh, setSSH] = useState<Extract<ConnectionTarget, { kind: 'ssh' }>>({ kind: 'ssh', host: '' });
  const [legacy, setLegacy] = useState<ConnectionProfile>();
  const native = runtime.platform.connectionKinds?.includes('ssh');
  const [endpoint, setEndpoint] = useState('');
  const [connectOnLaunch, setConnectOnLaunch] = useState(true);
  const [acceptIdentity, setAcceptIdentity] = useState(false);
  const [pending, setPending] = useState(false);
  const [localPending, setLocalPending] = useState(false);
  const [error, setError] = useState('');
  const canSave = state.home?.state === 'connected' && state.profilesReady;
  useEffect(() => { void runtime.connections.refreshProfiles().catch(() => {}); }, [runtime]);
  const action = async (run: () => Promise<unknown>) => {
    setPending(true); setError('');
    try { await run(); } catch (error) { if (mounted.current) setError(errorMessage(error)); }
    finally { if (mounted.current) setPending(false); }
  };
  const edit = (host?: HostConnection) => {
    setEditing(host); setName(host?.name ?? ''); setEndpoint(host?.endpoint ?? '');
    setKind(host?.profile.target.kind ?? 'url'); setSSH(host?.profile.target.kind === 'ssh' ? host.profile.target : { kind: 'ssh', host: '' }); setLegacy(undefined);
    setConnectOnLaunch(host?.connectOnLaunch ?? true); setAcceptIdentity(false); setError('');
  };
  return <div {...stylex.props(layout.column)}>
    <div aria-label="Saved execution hosts" {...stylex.props(layout.column)}>
      {state.hosts.map(host => <div key={host.id} {...stylex.props(layout.column)}>
        <div {...stylex.props(layout.row, layout.wrap)}>
          <div {...stylex.props(layout.row, styles.identity)}>
            <strong title={host.name} {...stylex.props(layout.grow, layout.ellipsis)}>{host.name}</strong>
            <span>{host.state === 'closed' ? 'Disconnected' : host.state}</span>
          </div>
          <div {...stylex.props(layout.row)}>
            <Button variant="ghost" disabled={pending || localPending} onClick={() => void action(async () => {
              if (host.client || host.state === 'connecting') runtime.connections.disconnect(host.id); else { await runtime.connections.connect(host.id); runtime.connections.select(host.id); }
            })}>{host.client ? 'Disconnect' : host.state === 'connecting' ? 'Cancel connection' : 'Connect'}</Button>
            {(!host.local || host.device) && <Button variant="ghost" disabled={pending || localPending} onClick={() => edit(host)}>Edit</Button>}
            {!host.local && <Button variant="ghost" disabled={pending || (!host.device && !canSave)} onClick={() => void action(() => runtime.connections.remove(host.id))}>Remove</Button>}
          </div>
        </div>
        <span {...stylex.props(layout.muted)}>{host.profile.target.kind === 'ssh' ? `SSH · ${host.profile.target.host}` : host.endpoint}</span>
        {host.progress && <p role="status">{host.progress}</p>}
        {host.error && <p role="status" {...stylex.props(layout.muted)}>{host.error}</p>}
        {host.profile.target.kind === 'local' && runtime.platform.localRuntime && <LocalRuntimePanel
          api={runtime.platform.localRuntime} hostState={host.state} disabled={pending}
          onBusyChange={setLocalPending} />}
      </div>)}
    </div>
    {state.profileError && <p role="status">{state.profileError}</p>}
    {error && <p role="alert">{error}</p>}
    <form {...stylex.props(layout.column)} onSubmit={event => {
      event.preventDefault();
      event.stopPropagation();
      void action(async () => {
        const id = kind === 'url'
          ? await runtime.connections.save({ id: editing?.id ?? crypto.randomUUID(), name, url: endpoint, runtime_id: editing?.runtimeId ?? legacy?.runtimeId, connect_on_launch: connectOnLaunch }, acceptIdentity, legacy?.id)
          : await runtime.connections.saveNative({ id: editing?.id ?? `ssh:${crypto.randomUUID()}`, label: name || ssh.host, target: ssh, runtimeId: editing?.runtimeId, ...(kind === 'local' ? { ...localProfile, runtimeId: editing?.runtimeId } : {}) }, acceptIdentity);
        if (legacy) await runtime.connections.forgetLegacyProfile(legacy.id);
        if (mounted.current) runtime.connections.select(id);
        void runtime.connections.connect(id).catch(() => {});
        if (state.legacyHosts.includes(endpoint)) runtime.forgetLegacyHost(endpoint);
        if (mounted.current) onSaved(id);
      });
    }}>
      <strong>{editing ? `Edit ${editing.name}` : 'Add remote host'}</strong>
      {native && !editing && <div role="group" aria-label="Connection method" {...stylex.props(layout.row)}>{(['url', 'ssh'] as const).map(value => <Button key={value} type="button" variant={kind === value ? 'secondary' : 'ghost'} aria-pressed={kind === value} disabled={pending} onClick={() => { setKind(value); setLegacy(undefined); }}>{value === 'url' ? 'URL' : 'SSH'}</Button>)}</div>}
      <label htmlFor="host-name">Name</label>
      <Input id="host-name" value={name} onChange={event => setName(event.target.value)} placeholder="Kuzco" required />
      {kind === 'url' && <>
      <label htmlFor="host-endpoint">Daemon address</label>
      <Input id="host-endpoint" value={endpoint} onChange={event => setEndpoint(event.target.value)} placeholder="http://kuzco-4090:8080" required />
      <Switch label="Connect when the app opens" checked={connectOnLaunch} onCheckedChange={setConnectOnLaunch} />
      </>}
      {kind === 'ssh' && <SSHFields target={ssh} busy={pending} onChange={setSSH} />}
      {(editing || legacy?.runtimeId) && <Switch label="Accept a new daemon identity" description="Use only when this address intentionally points to a replacement daemon. Existing tabs keep their original identity." checked={acceptIdentity} onCheckedChange={setAcceptIdentity} />}
      <div {...stylex.props(layout.row)}>
        <Button type="submit" variant="primary" loading={pending} disabled={kind === 'url' && !canSave}>Save and connect</Button>
        {editing && <Button type="button" variant="ghost" onClick={() => edit()}>Cancel edit</Button>}
      </div>
      {kind === 'url' && !canSave && <p {...stylex.props(layout.muted)}>Connect Local to save remote hosts.</p>}
    </form>
    {!!runtime.connections.getSnapshot().legacyProfiles?.length && <details>
      <summary>Import desktop addresses</summary>
      <div {...stylex.props(layout.column)}>{runtime.connections.getSnapshot().legacyProfiles!.map(profile => <Button key={profile.id} variant="ghost" onClick={() => { edit(); setLegacy(profile); setName(profile.label); setEndpoint(profile.target.kind === 'url' ? profile.target.endpoint : ''); }}>{profile.label}</Button>)}</div>
      <p {...stylex.props(layout.muted)}>Save to verify the existing daemon identity and move this address into the shared configuration. Your tabs and drafts remain on their original host.</p>
    </details>}
    {!!tabs.previous.length && <details>
      <summary>Restore previous host tabs</summary>
      <div {...stylex.props(layout.column)}>{tabs.previous.map(entry => <div key={entry.runtimeId} {...stylex.props(layout.column)}><div {...stylex.props(layout.row)}>
        <span {...stylex.props(layout.grow)}>{runtime.connections.host(entry.runtimeId)?.name ?? entry.runtimeId} · {entry.workspace.tabs.length} tabs</span>
        <Button variant="ghost" onClick={() => { try { runtime.tabs.restorePrevious(entry.runtimeId); } catch (error) { setError(errorMessage(error)); } }}>Restore</Button>
        <Button variant="ghost" onClick={() => runtime.tabs.dismissPrevious(entry.runtimeId)}>Dismiss</Button>
      </div><details><summary>Open individual previous tabs</summary><div {...stylex.props(layout.column)}>{[...entry.workspace.tabs, ...entry.workspace.closed.map(item => item.tab)].map(tab => <Button key={tab.id} variant="ghost" onClick={() => { try { runtime.tabs.openPrevious(entry.runtimeId, tab.id); } catch (error) { setError(errorMessage(error)); } }}>{tab.titleHint || tab.rootId}{tab.kind === 'repl' ? ' · REPL' : ''}</Button>)}</div></details></div>)}</div>
      <p {...stylex.props(layout.muted)}>Restore when there is room in this window. Your saved sessions and drafts stay on their original host.</p>
    </details>}
    {!!state.legacyHosts.length && <details>
      <summary>Import previously saved addresses</summary>
      <div {...stylex.props(layout.column)}>{state.legacyHosts.map(url => <div key={url} {...stylex.props(layout.row)}>
        <Button variant="ghost" onClick={() => { edit(); setEndpoint(url); setName(url); }}>{url}</Button>
        <Button variant="ghost" onClick={() => runtime.forgetLegacyHost(url)}>Dismiss</Button>
      </div>)}</div>
      <p {...stylex.props(layout.muted)}>Select an address, name it, and save to verify and import it.</p>
    </details>}
  </div>;
}

function LocalRuntimePanel({ api, hostState, disabled, onBusyChange }: {
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
    {status?.state === 'stopped' && <p {...stylex.props(layout.muted, styles.runtimeMessage)}>Use Connect above to start this daemon. Test Connection does not start it.</p>}
    {confirmRestart && <div {...stylex.props(layout.column, layout.notice)}>
      <p {...stylex.props(styles.runtimeMessage)}>Restarting interrupts running work on This Mac, including work started from the CLI or web app. Existing sessions are retained.</p>
      <div {...stylex.props(layout.row, layout.wrap)}>
        <Button variant="danger" disabled={busy} onClick={() => void run('restart')}>Interrupt work and restart</Button>
        <Button variant="ghost" disabled={busy} onClick={() => setConfirmRestart(false)}>Cancel restart</Button>
      </div>
    </div>}
    {status && <details>
      <summary>Runtime diagnostics</summary>
      <dl {...stylex.props(styles.diagnostics)}>
        <dt>Last checked state</dt><dd>{status.state}</dd>
        <dt>Executable</dt><dd><code>{status.executable ?? 'No executable selected'}</code></dd>
        <dt>State directory</dt><dd><code>{status.home}</code></dd>
        <dt>Client build</dt><dd><code>{status.clientBuild ?? 'Unavailable'}</code></dd>
        <dt>Daemon build</dt><dd><code>{status.daemonBuild ?? 'Unavailable'}</code></dd>
      </dl>
    </details>}
  </section>;
}

const styles = stylex.create({
  identity: { flex: '1 1 160px' },
  runtimeMessage: { margin: 0 },
  diagnostics: { display: 'grid', gridTemplateColumns: 'max-content minmax(0, 1fr)', gap: 8, overflowWrap: 'anywhere' },
});
