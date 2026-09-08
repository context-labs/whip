import { useEffect, useRef, useState } from 'react';
import { Button, Dialog, Field, IconButton, Input } from '@whip/ui';
import { X } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from './context';
import { localProfile, urlProfile, validateProfile, type ConnectionProfile, type ConnectionTarget } from './connections';
import { layout } from './styles';

export function ConnectionDialog({ open, onOpenChange }: { open: boolean; onOpenChange(open: boolean): void }) {
  const runtime = useRuntime();
  const state = useAppState();
  const [profile, setProfile] = useState(state.connection);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const attempt = useRef<{ profileId: string } | undefined>(undefined);
  useEffect(() => {
    attempt.current = undefined; setBusy(false);
    return () => { attempt.current = undefined; };
  }, [open]);
  useEffect(() => {
    if (attempt.current && attempt.current.profileId !== state.connection.id) { attempt.current = undefined; setBusy(false); }
    if (open) { setProfile(state.connection); setError(''); }
  }, [open, state.connection]);
  const kinds = runtime.platform.connectionKinds;
  const target = profile.target;
  const replaceTarget = (target: ConnectionTarget) => setProfile({ ...profile, id: '', runtimeId: undefined, target });
  const ssh = (key: 'host' | 'user' | 'port' | 'identityFile' | 'remoteExecutable' | 'remoteHome', value: string) => {
    if (target.kind !== 'ssh') return;
    replaceTarget({ ...target, [key]: value === '' ? undefined : key === 'port' ? Number(value) : value });
  };
  return <Dialog open={open} onOpenChange={onOpenChange} title="Connect to an execution host"
    description="Your sessions and work run on this host.">
    <form {...stylex.props(layout.column)} onSubmit={async event => {
      event.preventDefault(); if (busy) return;
      setError('');
      let selected: ConnectionProfile;
      try {
        if (target.kind === 'url') selected = { ...urlProfile(target.endpoint), ...(profile.runtimeId ? { runtimeId: profile.runtimeId } : {}) };
        else if (target.kind === 'local') selected = { ...localProfile, ...(profile.runtimeId ? { runtimeId: profile.runtimeId } : {}) };
        else selected = { ...profile, id: profile.id || `ssh:${crypto.randomUUID()}`, label: target.host };
        selected = validateProfile(selected);
        // Retyping an existing destination is a reconnect, not consent to replace
        // its remembered runtime. Replacement has its own explicit confirmation.
        selected = state.hosts.find(host => JSON.stringify(host.target) === JSON.stringify(selected.target)) ?? selected;
      } catch (value) { setError(value instanceof Error ? value.message : String(value)); return; }
      const request = { profileId: selected.id };
      attempt.current = request; setBusy(true);
      try {
        await runtime.connect(selected);
        if (attempt.current === request && runtime.getSnapshot().connection.id === selected.id) onOpenChange(false);
      } catch { /* Runtime/SDK own current connection errors and suppress retired attempts. */ }
      finally { if (attempt.current === request) { attempt.current = undefined; setBusy(false); } }
    }}>
      {!!state.hosts.length && <div {...stylex.props(layout.column)} aria-label="Saved execution hosts">
        {state.hosts.filter(host => kinds.includes(host.target.kind)).map(host => <div key={host.id} {...stylex.props(layout.row)}>
          <Button type="button" variant="ghost" disabled={busy} onClick={() => setProfile(host)}>{host.label}</Button>
          <IconButton label={`Forget ${host.label}`} disabled={busy} onClick={() => {
            try { runtime.forgetHost(host.id); } catch (value) { runtime.report(value); }
          }}><X size={13} /></IconButton>
        </div>)}
      </div>}
      {kinds.length > 1 && <div role="group" aria-label="Connection method" {...stylex.props(layout.row)}>
        {kinds.map(kind => <Button key={kind} type="button" variant={target.kind === kind ? 'secondary' : 'ghost'}
          aria-pressed={target.kind === kind} disabled={busy} onClick={() => {
            if (kind === 'local') setProfile(state.hosts.find(host => host.target.kind === 'local') ?? localProfile);
            else replaceTarget(kind === 'url' ? { kind, endpoint: '' } : { kind, host: '' });
          }}>{kind === 'local' ? 'This Mac' : kind === 'ssh' ? 'SSH' : 'URL'}</Button>)}
      </div>}
      {target.kind === 'url' && <>
        <label htmlFor="host-endpoint">Daemon address</label>
        <Input id="host-endpoint" value={target.endpoint} disabled={busy} required autoFocus
          placeholder="https://whip.example.com" onChange={event => replaceTarget({ kind: 'url', endpoint: event.target.value })} />
        <p {...stylex.props(layout.muted)}>Use the endpoint shown by <code>whip daemon status</code>. A remote connection needs the host’s HTTPS address.</p>
      </>}
      {target.kind === 'local' && <p {...stylex.props(layout.muted)}>Use Whip on this Mac. The desktop app starts the bundled runtime when needed; your work continues when you close the app.</p>}
      {target.kind === 'ssh' && <>
        <Field label="SSH host or alias"><Input value={target.host ?? ''} required autoFocus disabled={busy} placeholder="my-server"
          onChange={event => ssh('host', event.target.value)} /></Field>
        <p {...stylex.props(layout.muted)}>Uses your SSH configuration and keys. Whip must already be installed on the remote host.</p>
        <details><summary>Connection options</summary><div {...stylex.props(layout.column)}>
          <Field label="Username"><Input value={target.user ?? ''} disabled={busy} onChange={event => ssh('user', event.target.value)} /></Field>
          <Field label="Port"><Input type="number" min={1} max={65535} value={target.port ?? ''} disabled={busy} placeholder="22" onChange={event => ssh('port', event.target.value)} /></Field>
          <Field label="Identity file"><Input value={target.identityFile ?? ''} disabled={busy} placeholder="Use SSH configuration" onChange={event => ssh('identityFile', event.target.value)} /></Field>
          <Field label="Remote Whip executable"><Input value={target.remoteExecutable ?? ''} disabled={busy} placeholder="whip" onChange={event => ssh('remoteExecutable', event.target.value)} /></Field>
          <Field label="Remote Whip home"><Input value={target.remoteHome ?? ''} disabled={busy} placeholder="Use remote default" onChange={event => ssh('remoteHome', event.target.value)} /></Field>
        </div></details>
      </>}
      {state.connectionProgress && <p role="status">{state.connectionProgress}</p>}
      {error && <p role="alert">{error}</p>}
      <div {...stylex.props(layout.row)}><Button type="submit" variant="primary" loading={busy}>Connect</Button>
        {busy && <Button type="button" variant="ghost" onClick={() => runtime.cancelConnection()}>Cancel connection</Button>}</div>
    </form>
  </Dialog>;
}
