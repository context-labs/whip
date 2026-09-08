import { Field, Input } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import type { ConnectionTarget } from './connections';
import { layout } from './styles';

export function SSHFields({ target, busy, onChange }: { target: Extract<ConnectionTarget, { kind: 'ssh' }>; busy: boolean; onChange(target: Extract<ConnectionTarget, { kind: 'ssh' }>): void }) {
  const ssh = (key: 'host' | 'user' | 'port' | 'identityFile' | 'remoteExecutable' | 'remoteHome', value: string) => {
    onChange({ ...target, [key]: value === '' ? undefined : key === 'port' ? Number(value) : value });
  };
  return <>
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
  </>;
}
