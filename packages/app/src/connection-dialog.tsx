import { useEffect, useState, type ReactNode, type RefObject } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button, Collapsible, Field, Input, RadioGroup, Skeleton } from '@whip/ui';
import { ChevronRight, RefreshCw, Server, Terminal } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import type { ConnectionTarget } from './connections';
import { useAppState, useRuntime } from './context';
import { ErrorNotice } from './error-feedback';
import { sshStyles as styles } from './ssh-profile-picker.stylex';

type Target = Extract<ConnectionTarget, { kind: 'ssh' }>;

interface SSHFieldsProps {
  children?: ReactNode; hostRef?: RefObject<HTMLInputElement | null>; target: Target;
  busy: boolean; editing?: boolean; manual: boolean; onManualChange(manual: boolean): void; onChange(target: Target): void;
}

export function SSHFields(props: SSHFieldsProps) {
  const runtime = useRuntime();
  return props.editing || !runtime.platform.listSSHProfiles
    ? <SSHManualFields {...props} />
    : <SSHProfileFields {...props} />;
}

function SSHProfileFields({ target, busy, onChange, children, hostRef, manual, onManualChange }: SSHFieldsProps) {
  const runtime = useRuntime();
  const state = useAppState();
  const discover = runtime.platform.listSSHProfiles;
  const [filter, setFilter] = useState('');
  const query = useQuery({ queryKey: ['desktop', 'ssh-profiles'], queryFn: () => discover!(), staleTime: 0, gcTime: 0, retry: false, refetchOnWindowFocus: false });
  const profiles = query.data?.profiles ?? [];
  const added = new Set(state.hosts.filter(host => host.profile.target.kind === 'ssh' && !!host.profile.runtimeId)
    .map(host => (host.profile.target as Target).host.toLowerCase()));
  const visible = profiles.filter(profile => `${profile.alias} ${profile.hostname ?? ''} ${profile.user ?? ''}`.toLowerCase().includes(filter.trim().toLowerCase()));
  useEffect(() => {
    if (!busy && !manual && query.data && target.host &&
        (!query.data.profiles.some(profile => profile.alias === target.host) ||
         state.hosts.some(host => host.profile.target.kind === 'ssh' && host.profile.runtimeId && host.profile.target.host.toLowerCase() === target.host.toLowerCase()))) {
      onChange({ kind: 'ssh', host: '' });
    }
  }, [busy, manual, query.data, target.host, state.hosts, onChange]);
  const mode = (next: boolean) => { if (!busy) onManualChange(next); };
  return <div {...stylex.props(styles.column)}>
    {manual ? <div {...stylex.props(styles.summary)}>
      <span {...stylex.props(styles.muted)}>SSH profiles{query.data ? ` · ${profiles.length} found` : ''}</span>
      <Button type="button" variant="ghost" disabled={busy} onClick={() => mode(false)}><ChevronRight size={14} aria-hidden />Choose a profile</Button>
    </div> : <>
      <div {...stylex.props(styles.toolbar)}>
        <Input aria-label="Filter SSH profiles" xstyle={styles.search} placeholder="Filter profiles…" value={filter} disabled={busy} onChange={event => setFilter(event.target.value)} />
        <Button type="button" variant="ghost" disabled={busy || query.isFetching} loading={query.isFetching} onClick={() => { void query.refetch(); }}><RefreshCw size={14} aria-hidden />Refresh</Button>
      </div>
      <div {...stylex.props(styles.list)} aria-busy={query.isFetching}>
        {query.isPending ? <><span role="status" {...stylex.props(styles.muted)}>Loading SSH profiles…</span>{[0, 1, 2].map(key => <Skeleton key={key} xstyle={styles.skeleton} />)}</>
          : visible.length ? <RadioGroup label="SSH profiles" variant="cards" value={target.host} disabled={busy} onValueChange={host => onChange({ kind: 'ssh', host })}
            options={visible.map(profile => ({ value: profile.alias, label: profile.alias, icon: <Server size={18} />,
              description: profile.hostname || profile.user || profile.port ? `${profile.user ? `${profile.user}@` : ''}${profile.hostname ?? profile.alias}${profile.port ? ` · Port ${profile.port}` : ''}` : 'Uses SSH configuration',
              disabled: added.has(profile.alias.toLowerCase()), trailing: added.has(profile.alias.toLowerCase()) ? 'Added' : undefined }))} />
          : <div {...stylex.props(styles.empty)}><Terminal size={24} aria-hidden /><p {...stylex.props(styles.title)}>{query.isError ? 'SSH profiles unavailable' : profiles.length ? 'No profiles match' : 'No SSH profiles found'}</p>
            <p {...stylex.props(styles.muted)}>{query.isError ? 'Retry below, or enter a host manually.' : profiles.length ? 'Try another name or clear the filter.' : 'Add a profile to this Mac’s SSH configuration and refresh, or enter a host manually.'}</p>
            {profiles.length > 0 && <Button type="button" variant="ghost" onClick={() => setFilter('')}>Clear filter</Button>}</div>}
      </div>
      {query.isError && <ErrorNotice type="resource" owner="ssh-profiles" title="Couldn’t read SSH profiles" error={query.error} action={<Button type="button" disabled={busy || query.isFetching} onClick={() => { void query.refetch(); }}>Retry</Button>} />}
      {query.data?.truncated && <p role="status" {...stylex.props(styles.muted)}>Showing a limited list of SSH profiles. Enter a host manually if it’s missing.</p>}
      <p {...stylex.props(styles.muted)}>Whip must already be installed on the remote host.</p>
    </>}
    <Collapsible xstyle={styles.disclosure} disabled={busy} open={manual} onOpenChange={mode} title={<span {...stylex.props(styles.advancedTitle)}>Advanced SSH options{!manual && <span {...stylex.props(styles.hint)}>Enter manually</span>}</span>}><SSHManualFields target={target} busy={busy} onChange={onChange} hostRef={hostRef}>{children}</SSHManualFields></Collapsible>
  </div>;
}

function SSHManualFields({ target, busy, onChange, children, hostRef }: Pick<SSHFieldsProps, 'target' | 'busy' | 'onChange' | 'children' | 'hostRef'>) {
  const ssh = (key: 'host' | 'user' | 'port' | 'identityFile' | 'remoteExecutable' | 'remoteHome', value: string) => {
    onChange({ ...target, [key]: value === '' && key !== 'host' ? undefined : key === 'port' ? Number(value) : value });
  };
  return <div {...stylex.props(styles.column)}>
    <p {...stylex.props(styles.muted)}>Enter a host manually. Other fields are optional.</p>
    <Field label="SSH host or alias"><Input ref={hostRef} value={target.host ?? ''} required autoFocus disabled={busy} placeholder="my-server" onChange={event => ssh('host', event.target.value)} /></Field>
    {children}
    <div {...stylex.props(styles.fields)}>
      <Field xstyle={styles.field} label="Username"><Input value={target.user ?? ''} disabled={busy} onChange={event => ssh('user', event.target.value)} /></Field>
      <Field xstyle={styles.field} label="Port"><Input type="number" min={1} max={65535} value={target.port ?? ''} disabled={busy} placeholder="22" onChange={event => ssh('port', event.target.value)} /></Field>
    </div>
    <Field label="Identity file"><Input value={target.identityFile ?? ''} disabled={busy} placeholder="Use SSH configuration" onChange={event => ssh('identityFile', event.target.value)} /></Field>
    <div {...stylex.props(styles.fields)}>
      <Field xstyle={styles.field} label="Remote Whip executable"><Input value={target.remoteExecutable ?? ''} disabled={busy} placeholder="whip" onChange={event => ssh('remoteExecutable', event.target.value)} /></Field>
      <Field xstyle={styles.field} label="Remote Whip home"><Input value={target.remoteHome ?? ''} disabled={busy} placeholder="Use remote default" onChange={event => ssh('remoteHome', event.target.value)} /></Field>
    </div>
    <p {...stylex.props(styles.muted)}>Blank fields use your SSH configuration and remote defaults. Whip must already be installed on the remote host.</p>
  </div>;
}
