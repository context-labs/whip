import { useEffect, useRef, useState } from 'react';
import type { Client, InferenceFlow, OpenAILoginFlow } from '@whip/sdk';
import { Button, CopyButton, Field, Input, RadioGroup, ScrollArea, Spinner } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { errorMessage } from '../platform';
import { ErrorNotice } from '../error-feedback';
import { loginStyles as styles } from './provider-login.stylex';

export type AccountFlow = { provider: 'inference-net'; value: InferenceFlow } | { provider: 'openai-codex'; value: OpenAILoginFlow };
export const terminalLoginStates = ['succeeded', 'failed', 'cancelled', 'interrupted', 'expired', 'uncertain'];
export const pollingLoginStates = ['authorizing', 'loading_projects', 'creating_project', 'provisioning'];
export async function readLoginFlows(client: Client, signal: AbortSignal): Promise<AccountFlow[]> {
  const [inference, openai] = await Promise.all([client.listInferenceLogins({ signal }), client.listOpenAILogins({ signal })]);
  return [...inference.items.map(value => ({ provider: 'inference-net' as const, value })), ...openai.items.map(value => ({ provider: 'openai-codex' as const, value }))];
}
export function LoginFlow({ flow, client, enabled, hostName, update, refresh, prepareSetup, leave, autoOpen = false }: {
  flow: AccountFlow; client: Client; enabled: boolean; hostName: string;
  update(flow: AccountFlow): Promise<void>; refresh(): Promise<void>; prepareSetup?(signal: AbortSignal): Promise<void>; leave(message?: string): void; autoOpen?: boolean;
}) {
  const runtime = useRuntime();
  const value = flow.value;
  const inference = flow.provider === 'inference-net' ? flow.value : undefined;
  const [name, setName] = useState('');
  const [team, setTeam] = useState(inference?.team_id ?? '');
  const [project, setProject] = useState('');
  const [workspace, setWorkspace] = useState(false);
  const [creating, setCreating] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const opened = useRef('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => { setBusy(false); request.current = null; return () => request.current?.abort(); }, [client]);
  const terminal = terminalLoginStates.includes(value.state);
  const choosingTeam = value.state === 'choose_team' || (value.state === 'choose_project' && workspace);
  const choosingProject = value.state === 'choose_project' && !workspace;
  const creatingProject = choosingProject && (creating || !inference?.projects.length);
  const waiting = value.state === 'authorizing';
  const uncertain = value.state === 'uncertain' || value.state === 'interrupted';
  const recoverable = ['persistence_required', 'setup_required', 'cleanup_required'].includes(value.state);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => { heading.current?.focus({ preventScroll: true }); }, [value.state, workspace, creatingProject, enabled]);
  async function action(run: (signal: AbortSignal) => Promise<AccountFlow | void>, cancel = false) {
    if (!enabled || request.current) return;
    const controller = new AbortController(); request.current = controller; setBusy(true); setError('');
    try {
      const next = await run(controller.signal);
      if (controller.signal.aborted) return;
      if (next) { await update(next); if (next.value.state !== 'choose_project') { setWorkspace(false); setProject(''); } }
      await refresh();
      if (cancel && next?.value.state === 'cancelled') leave('Sign-in cancelled.');
    } catch (error) {
      if (!controller.signal.aborted) { setError(errorMessage(error)); await refresh().catch(() => {}); }
    } finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setBusy(false); }
  }
  useEffect(() => {
    const url = value.verification_url;
    const identity = `${value.id}:${url}`;
    if (!enabled || !autoOpen || !waiting || !url || opened.current === identity) return;
    opened.current = identity;
    void runtime.platform.openExternal(url).catch(() => {});
  }, [autoOpen, enabled, value.id, value.verification_url, waiting, runtime.platform]);
  const wrapInference = async (promise: Promise<InferenceFlow>): Promise<AccountFlow> => ({ provider: 'inference-net', value: await promise });
  const cancel = <Button variant="ghost" disabled={!enabled || busy} onClick={() => void action(async signal => flow.provider === 'inference-net'
    ? wrapInference(client.cancelInferenceLogin(value.id, { signal })) : { provider: 'openai-codex', value: await client.cancelOpenAILogin(value.id, { signal }) }, true)}>Cancel sign-in</Button>;
  const submit = () => {
    if (!inference) return;
    if (choosingTeam && team) void action(signal => wrapInference(client.selectInferenceTeam(value.id, team, { signal })));
    else if (choosingProject && (creatingProject ? name.trim() : project)) void action(signal => wrapInference(creatingProject
      ? client.createInferenceProject(value.id, name.trim(), { signal }) : client.selectInferenceProject(value.id, project, { signal })));
  };
  return <form onSubmit={event => { event.preventDefault(); submit(); }} {...stylex.props(styles.flow)}>
    <div {...stylex.props(styles.content)}>
      <h3 ref={heading} tabIndex={-1} {...stylex.props(styles.title)}>{enabled && pollingLoginStates.includes(value.state) && <span aria-hidden="true"><Spinner /></span>}
        {!enabled ? `${hostName} is unavailable` : choosingTeam ? 'Choose a workspace' : creatingProject ? 'Create a project' : choosingProject ? 'Choose a project' : loginStateLabel(value.state)}</h3>
      {!enabled ? <p {...stylex.props(styles.text)}>Reconnect to this host to continue. Your progress is kept here.</p> : <>
        {waiting && <><p {...stylex.props(styles.text)}>Complete sign-in in your browser. You can close this dialog while the host waits.</p>
          {value.user_code && <div {...stylex.props(styles.codePanel)}><code {...stylex.props(styles.code)}>{value.user_code}</code><CopyButton text={value.user_code} label="Copy code" showLabel copy={runtime.platform.copy} onError={error => setError(errorMessage(error))} /></div>}</>}
        {choosingTeam && <ScrollArea xstyle={styles.choices}><RadioGroup label="Workspace" value={team} onValueChange={setTeam} disabled={busy} options={(inference?.teams ?? []).map(item => ({ value: item.id, label: item.name }))} /></ScrollArea>}
        {choosingProject && !creatingProject && <><ScrollArea xstyle={styles.choices}><RadioGroup label="Project" value={project} onValueChange={setProject} disabled={busy} options={(inference?.projects ?? []).map(item => ({ value: item.id, label: item.name }))} /></ScrollArea><Button variant="ghost" disabled={busy} onClick={() => setCreating(true)}>Create a new project</Button></>}
        {creatingProject && <Field label="Project name"><Input value={name} maxLength={256} disabled={busy} onChange={event => setName(event.target.value)} /></Field>}
        {uncertain && <p {...stylex.props(styles.text)}>The last account change may have completed. Check this flow and the account before starting another sign-in, key rotation, or project creation.</p>}
        {recoverable && <p {...stylex.props(styles.text)}>The host kept the completed steps. Continue setup to retry the remaining step without creating another key.</p>}
        {value.state === 'succeeded' && <p {...stylex.props(styles.text)}>Account setup completed on {hostName}. Your default model is unchanged; inference has not been tested.</p>}
        {(error || value.failure) && <ErrorNotice type="action" owner={`sign-in:${value.id}`} title="Sign-in needs attention" error={error || value.failure} />}
      </>}
    </div>
    <div {...stylex.props(styles.footer)}>
      <Button variant="ghost" onClick={() => leave()}>Back</Button>
      {enabled && <>
        {!terminal && cancel}
        {waiting && <Button variant="primary" disabled={!value.verification_url} onClick={() => { if (value.verification_url) void runtime.platform.openExternal(value.verification_url).catch(error => setError(errorMessage(error))); }}>Open verification page</Button>}
        {choosingTeam && <Button variant="primary" disabled={busy || !team} type="submit">Continue</Button>}
        {choosingProject && <><Button variant="ghost" disabled={busy} onClick={() => { if (creating) setCreating(false); else setWorkspace(true); }}>Choose workspace</Button><Button variant="primary" disabled={busy || (creatingProject ? !name.trim() : !project)} type="submit">{creatingProject ? 'Create and connect' : 'Connect'}</Button></>}
        {recoverable && <Button variant="primary" disabled={busy} onClick={() => void action(async signal => {
          if (value.state === 'setup_required') await prepareSetup?.(signal);
          if (flow.provider === 'inference-net') return wrapInference(client.retryInferenceLogin(value.id, { signal }));
          await client.setupOpenAIAccount({ signal }); leave('Saved account route configured.');
        })}>Continue setup</Button>}
        {(terminal || recoverable) && <Button disabled={busy} onClick={() => void action(async signal => flow.provider === 'inference-net'
          ? wrapInference(client.getInferenceLogin(value.id, { signal })) : { provider: 'openai-codex', value: await client.getOpenAILogin(value.id, { signal }) })}>Check progress</Button>}
      </>}
    </div>
  </form>;
}
export function loginStateLabel(state: string) {
  const labels: Record<string, string> = { authorizing: 'Finish signing in in your browser', loading_projects: 'Loading projects', creating_project: 'Creating project', provisioning: 'Connecting account', succeeded: 'Setup completed', failed: 'Sign-in failed', cancelled: 'Sign-in cancelled', interrupted: 'Sign-in interrupted', expired: 'Sign-in expired', uncertain: 'Account change needs review', persistence_required: 'Save connection', setup_required: 'Finish route setup', cleanup_required: 'Account cleanup needs attention' };
  return labels[state] ?? state.replaceAll('_', ' ');
}
