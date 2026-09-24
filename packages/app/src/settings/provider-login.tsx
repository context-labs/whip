import { useEffect, useRef, useState } from 'react';
import type { WhipClient } from '@whip/sdk';
import type { ProviderLoginStatus } from '@whip/protocol';
import { Button, CopyButton, Field, Input, RadioGroup, ScrollArea, Spinner } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { errorMessage } from '../platform';
import { ErrorNotice } from '../error-feedback';
import { loginStyles as styles } from './provider-login.stylex';

export function LoginFlow({ flow, client, enabled, hostName, update, retry, leave, autoOpen = false }: {
  flow: ProviderLoginStatus; client: WhipClient; enabled: boolean; hostName: string;
  update(flow: ProviderLoginStatus): Promise<void>; retry(): void; leave(message?: string): void; autoOpen?: boolean;
}) {
  const runtime = useRuntime();
  const [name, setName] = useState('');
  const [team, setTeam] = useState(flow.team_id ?? '');
  const [project, setProject] = useState('');
  const [workspace, setWorkspace] = useState(false);
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const opened = useRef('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), [client]);
  const terminal = terminalLoginStates.includes(flow.state);
  const choosingTeam = flow.state === 'choose_team' || (flow.state === 'choose_project' && workspace);
  const choosingProject = flow.state === 'choose_project' && !workspace;
  const creatingProject = choosingProject && (creating || !flow.projects?.length);
  const waiting = ['authorizing', 'pending', 'polling'].includes(flow.state);
  const teamName = flow.teams?.find(item => item.id === flow.team_id)?.name;
  const heading = useRef<HTMLHeadingElement>(null);
  const step = !enabled ? 'offline' : `${flow.state}:${workspace}:${creatingProject}`;
  useEffect(() => { heading.current?.focus({ preventScroll: true }); setError(''); }, [step]);
  async function action(run: (signal: AbortSignal) => Promise<ProviderLoginStatus>, cancel = false) {
    if (!enabled || request.current) return;
    const controller = new AbortController(); request.current = controller;
    setBusy(true); setError('');
    try {
      const result = await run(controller.signal);
      if (controller.signal.aborted) return;
      await update(result);
      if (result.state !== 'choose_project') { setWorkspace(false); setProject(''); }
      if (cancel && result.state === 'cancelled') leave('Sign-in cancelled. Choose how you’d like to connect.');
    } catch (error) { if (!controller.signal.aborted) setError(errorMessage(error)); }
    finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setBusy(false); }
  }
  useEffect(() => {
    const url = flow.verification_url;
    const identity = `${flow.flow_id}:${url}`;
    if (!enabled || !autoOpen || !waiting || !url || opened.current === identity) return;
    opened.current = identity;
    void runtime.platform.openExternal(url).catch(() => {});
  }, [autoOpen, enabled, flow.flow_id, flow.verification_url, waiting, runtime.platform]);
  const cancel = <Button variant="ghost" disabled={!enabled || busy} onClick={() => void action(signal => client.providers.login.cancel(flow.flow_id, { signal }), true)}>Cancel sign-in</Button>;
  const title = !enabled ? `${hostName} is unavailable` : choosingTeam ? 'Choose a workspace' : creatingProject ? (flow.projects?.length ? 'Create a project' : 'Create your first project') : choosingProject ? 'Choose a project' : waiting ? 'Finish signing in in your browser' : flow.state === 'expired' ? 'This sign-in has expired' : loginStateLabel(flow.state);
  const submit = () => {
    if (choosingTeam && team) void action(signal => client.providers.login.selectTeam(flow.flow_id, team, { signal }));
    else if (choosingProject && (creatingProject ? name.trim() : project)) void action(signal => creatingProject
      ? client.providers.login.createProject(flow.flow_id, name.trim(), { signal })
      : client.providers.login.selectProject(flow.flow_id, project, { signal }));
  };
  return <form onSubmit={event => { event.preventDefault(); submit(); }} {...stylex.props(styles.flow)}>
    <div {...stylex.props(styles.content)}>
      <h3 ref={heading} tabIndex={-1} {...stylex.props(styles.title)}>{enabled && !terminal && !choosingTeam && !choosingProject && <span aria-hidden="true"><Spinner /></span>}{title}</h3>
      {!enabled ? <p {...stylex.props(styles.text)}>Reconnect to this host to continue. Your progress is kept here.</p> : <>
        {waiting && <>
          <p {...stylex.props(styles.text)}>{flow.user_code ? 'Enter this code if the provider asks for it.' : 'Complete sign-in in your browser to continue.'}</p>
          {flow.user_code && <div {...stylex.props(styles.codePanel)}><code {...stylex.props(styles.code)}>{flow.user_code}</code><CopyButton text={flow.user_code} label="Copy code" showLabel copy={runtime.platform.copy} onError={error => setError(errorMessage(error))} /></div>}
          <p {...stylex.props(styles.text)}>You can close this dialog and return while sign-in continues.</p>
        </>}
        {choosingTeam && <><p {...stylex.props(styles.text)}>Choose the workspace to connect on {hostName}.</p><ScrollArea xstyle={styles.choices}><RadioGroup label="Workspace" value={team} onValueChange={setTeam} disabled={busy} options={(flow.teams ?? []).map(item => ({ value: item.id, label: item.name }))} /></ScrollArea></>}
        {choosingProject && <p {...stylex.props(styles.text)}>{teamName ? `Projects in ${teamName}.` : 'Choose a project for this connection.'}</p>}
        {choosingProject && !creatingProject && <><ScrollArea xstyle={styles.choices}><RadioGroup label="Project" value={project} onValueChange={setProject} disabled={busy} options={(flow.projects ?? []).map(item => ({ value: item.id, label: item.name }))} /></ScrollArea><Button variant="ghost" disabled={busy} onClick={() => setCreating(true)}>Create a new project</Button></>}
        {creatingProject && <Field label="Project name"><Input value={name} disabled={busy} onChange={event => setName(event.target.value)} /></Field>}
        {flow.state === 'loading_projects' && <p {...stylex.props(styles.text)}>Loading projects{teamName ? ` in ${teamName}` : ''}…</p>}
        {['provisioning', 'creating_key', 'succeeded'].includes(flow.state) && <p {...stylex.props(styles.text)}>{flow.state === 'succeeded' ? `Connected on ${hostName}.` : `Saving your connection on ${hostName}.`}{teamName && ` Workspace: ${teamName}.`}</p>}
        {terminal && flow.state !== 'succeeded' && <p {...stylex.props(styles.text)}>{flow.state === 'expired' ? 'Start again to get a new verification code.' : flow.state === 'interrupted' ? 'Sign-in was interrupted. Refresh the provider connection before trying again; the last operation may have completed.' : 'Start sign-in again, or choose another connection method.'}</p>}
        {(error || flow.error) && <ErrorNotice type="action" owner={`sign-in:${flow.flow_id}`} title="Sign-in needs attention" error={error || flow.error} />}
      </>}
    </div>
    <div {...stylex.props(styles.footer)}>
      {!enabled ? <Button disabled xstyle={styles.submit}>Waiting for host…</Button> : terminal ? <><Button variant="ghost" onClick={() => leave()}>Use another method</Button>{flow.state !== 'succeeded' && <Button variant="primary" onClick={retry}>Sign in again</Button>}</> : <>
        {choosingProject && (creating || (flow.teams?.length ?? 0) > 1) ? <Button variant="ghost" disabled={busy} onClick={() => { if (creating) setCreating(false); else setWorkspace(true); }}>Back</Button> : cancel}
        {waiting && <Button variant="primary" disabled={!flow.verification_url} onClick={() => void runtime.platform.openExternal(flow.verification_url!).catch(error => setError(errorMessage(error)))}>Open verification page</Button>}
        {choosingTeam && <Button variant="primary" xstyle={styles.submit} disabled={busy || !team} type="submit">{busy ? 'Loading…' : 'Continue'}</Button>}
        {choosingProject && <Button variant="primary" xstyle={styles.submit} disabled={busy || (creatingProject ? !name.trim() : !project)} type="submit">{busy ? 'Connecting…' : creatingProject ? 'Create and connect' : 'Connect'}</Button>}
        {!waiting && !choosingTeam && !choosingProject && <Button disabled xstyle={styles.submit}>Connecting…</Button>}
      </>}
    </div>
  </form>;
}

export const terminalLoginStates: string[] = ['succeeded', 'failed', 'cancelled', 'interrupted', 'expired'];
export function loginStateLabel(state: string) {
  const labels: Record<string, string> = { pending: 'Waiting for sign-in', authorizing: 'Waiting for sign-in', polling: 'Waiting for sign-in', choose_team: 'Choose a workspace', loading_projects: 'Loading projects', choose_project: 'Choose a project', creating_key: 'Connecting your account', provisioning: 'Connecting your account', succeeded: 'Connected', failed: 'Sign-in failed', cancelled: 'Sign-in cancelled', interrupted: 'Sign-in interrupted', expired: 'Sign-in expired' };
  return labels[state] ?? 'Connecting your account';
}
