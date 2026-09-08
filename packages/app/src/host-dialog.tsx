import { useEffect, useState } from 'react';
import { Button, Dialog, Input, Switch } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { errorMessage } from './platform';
import type { HostConnection } from './hosts';
import { layout } from './styles';

export function HostDialog({ open, onOpenChange, onSaved }: { open: boolean; onOpenChange(open: boolean): void; onSaved?(id: string): void }) {
  return <Dialog open={open} onOpenChange={onOpenChange} title="Execution hosts" description="Connect to existing daemons. Saved hosts are shared through Local’s whipcode configuration.">
    {open && <HostManager onSaved={id => { onSaved?.(id); onOpenChange(false); }} />}
  </Dialog>;
}

function HostManager({ onSaved }: { onSaved(id: string): void }) {
  const runtime = useRuntime();
  const state = useAppState();
  const tabs = useSessionTabs();
  const [editing, setEditing] = useState<HostConnection>();
  const [name, setName] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [connectOnLaunch, setConnectOnLaunch] = useState(true);
  const [acceptIdentity, setAcceptIdentity] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const canSave = state.home?.state === 'connected' && state.profilesReady;
  useEffect(() => { void runtime.connections.refreshProfiles().catch(() => {}); }, [runtime]);
  const action = async (run: () => Promise<unknown>) => {
    setPending(true); setError('');
    try { await run(); } catch (error) { setError(errorMessage(error)); }
    finally { setPending(false); }
  };
  const edit = (host?: HostConnection) => {
    setEditing(host); setName(host?.name ?? ''); setEndpoint(host?.endpoint ?? '');
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
            <Button variant="ghost" disabled={pending} onClick={() => void action(async () => {
              if (host.client) runtime.connections.disconnect(host.id); else await runtime.connections.connect(host.id);
            })}>{host.client ? 'Disconnect' : 'Connect'}</Button>
            {!host.local && <Button variant="ghost" disabled={pending} onClick={() => edit(host)}>Edit</Button>}
            {!host.local && <Button variant="ghost" disabled={pending || !canSave} onClick={() => void action(() => runtime.connections.remove(host.id))}>Remove</Button>}
          </div>
        </div>
        <span {...stylex.props(layout.muted)}>{host.endpoint}</span>
        {host.error && <p role="status" {...stylex.props(layout.muted)}>{host.error}</p>}
      </div>)}
    </div>
    {state.profileError && <p role="status">{state.profileError}</p>}
    {error && <p role="alert">{error}</p>}
    <form {...stylex.props(layout.column)} onSubmit={event => {
      event.preventDefault();
      event.stopPropagation();
      void action(async () => {
        const id = await runtime.connections.save({ id: editing?.id ?? crypto.randomUUID(), name, url: endpoint, runtime_id: editing?.runtimeId, connect_on_launch: connectOnLaunch }, acceptIdentity);
        void runtime.connections.connect(id).catch(() => {});
        if (state.legacyHosts.includes(endpoint)) runtime.forgetLegacyHost(endpoint);
        onSaved(id);
      });
    }}>
      <strong>{editing ? `Edit ${editing.name}` : 'Add remote host'}</strong>
      <label htmlFor="host-name">Name</label>
      <Input id="host-name" value={name} onChange={event => setName(event.target.value)} placeholder="Kuzco" required />
      <label htmlFor="host-endpoint">Daemon address</label>
      <Input id="host-endpoint" value={endpoint} onChange={event => setEndpoint(event.target.value)} placeholder="http://kuzco-4090:8080" required />
      <Switch label="Connect when the app opens" checked={connectOnLaunch} onCheckedChange={setConnectOnLaunch} />
      {editing && <Switch label="Accept a new daemon identity" description="Use only when this address intentionally points to a replacement daemon. Existing tabs keep their original identity." checked={acceptIdentity} onCheckedChange={setAcceptIdentity} />}
      <div {...stylex.props(layout.row)}>
        <Button type="submit" variant="primary" loading={pending} disabled={!canSave}>Save and connect</Button>
        {editing && <Button type="button" variant="ghost" onClick={() => edit()}>Cancel edit</Button>}
      </div>
      {!canSave && <p {...stylex.props(layout.muted)}>Connect Local to save remote hosts.</p>}
    </form>
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

const styles = stylex.create({
  identity: { flex: '1 1 160px' },
});
