import { ErrorNotice } from '../error-feedback';
import { useEffect, useRef, useState } from 'react';
import type { WhipClient } from '@whip/sdk';
import type { ProviderLoginStatus } from '@whip/protocol';
import { Badge, Button, Field, Input, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { layout } from '../styles';
import { errorMessage } from '../platform';

export function LoginFlow({
  flow,
  client,
  enabled,
  refresh,
  autoOpen = false,
}: {
  flow: ProviderLoginStatus;
  client: WhipClient;
  enabled: boolean;
  refresh(): void;
  autoOpen?: boolean;
}) {
  const runtime = useRuntime();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);
  const [creating, setCreating] = useState(false);
  const opened = useRef('');
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), [client]);
  const action = async (run: (signal: AbortSignal) => Promise<unknown>) => {
    if (!enabled || busy) return;
    const controller = new AbortController();
    request.current = controller; setBusy(true); setError('');
    try {
      await run(controller.signal);
      if (!controller.signal.aborted) refresh();
    } catch (error) {
      if (!controller.signal.aborted) setError(errorMessage(error));
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  };
  const terminal = terminalLoginStates.includes(flow.state);
  useEffect(() => {
    if (flow.state !== 'succeeded') return;
    const host = client.getSnapshot().info?.runtime_id;
    if (!host) return;
    void runtime.queries.invalidateQueries({ predicate: query =>
      query.queryKey.includes(host) && query.queryKey[0] !== 'provider-login-flows',
    });
  }, [flow.state, flow.flow_id, client, runtime.queries]);
  useEffect(() => {
    const url = flow.verification_url;
    const identity = `${flow.flow_id}:${url}`;
    if (!autoOpen || terminal || !url || opened.current === identity) return;
    opened.current = identity;
    // The explicit Sign in action owns this deferred opening. Browsers that
    // block asynchronous popups retain the visible verification-page button.
    void runtime.platform.openExternal(url).catch(() => {});
  }, [autoOpen, flow.flow_id, flow.verification_url, terminal, runtime.platform]);
  return (
    <div {...stylex.props(layout.notice, layout.column)}>
      <div {...stylex.props(layout.row)}>
        <span>{flow.provider === 'openai-codex' ? 'OpenAI (ChatGPT subscription)' : 'Inference.net'}</span>
        <Badge>{loginStateLabel(flow.state)}</Badge>
        <span>{flow.email}</span>
      </div>
      {flow.verification_url && !terminal && (
        <>
          <Button
            variant="secondary"
            onClick={() =>
              void runtime.platform.openExternal(flow.verification_url!).catch(error => setError(errorMessage(error)))
            }
          >
            Open verification page
          </Button>
          {flow.user_code && <div {...stylex.props(layout.row)}><span>Verification code: <strong>{flow.user_code}</strong></span>
            <Button variant="ghost" onClick={() => void runtime.platform.copy(flow.user_code!).then(() => setCopied(true)).catch(error => setError(errorMessage(error)))}>{copied ? 'Code copied' : 'Copy code'}</Button>
          </div>}
        </>
      )}
      {!!flow.teams?.length && !flow.team_id && (
        <Select
          label="Workspace"
          disabled={!enabled || busy}
          options={flow.teams.map((team) => ({
            value: team.id,
            label: team.name,
          }))}
          onValueChange={(id) =>
            void action(signal =>
              client.providers.login.selectTeam(flow.flow_id, id, { signal }),
            )
          }
        />
      )}
      {!!flow.projects?.length && flow.team_id && !flow.project_id && (
        <Select
          label="Project"
          disabled={!enabled || busy}
          options={flow.projects.map((project) => ({
            value: project.id,
            label: project.name,
          }))}
          onValueChange={(id) =>
            void action(signal =>
              client.providers.login.selectProject(flow.flow_id, id, { signal }),
            )
          }
        />
      )}
      {flow.team_id && !flow.project_id && !terminal && (!!flow.projects?.length && !creating ? <Button variant="ghost" onClick={() => setCreating(true)}>Create a new project</Button> : (
        <>
          <Field label="New project name">
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </Field>
          <Button
            variant="ghost"
            disabled={!enabled || busy || !name.trim()}
            onClick={() =>
              void action(signal =>
                client.providers.login.createProject(flow.flow_id, name.trim(), { signal }),
              )
            }
          >
            Create and continue
          </Button>
        </>
      ))}
      {(error || flow.error) && <ErrorNotice type="action" owner={`sign-in:${flow.flow_id}`} title="Sign-in needs attention" error={error || flow.error} />}
      {!terminal && (
        <Button
          variant="ghost"
          disabled={!enabled || busy}
          onClick={() =>
            void action(signal => client.providers.login.cancel(flow.flow_id, { signal }))
          }
        >
          Cancel sign-in
        </Button>
      )}
    </div>
  );
}


export const terminalLoginStates: string[] = ['succeeded', 'failed', 'cancelled', 'interrupted', 'expired'];

export function loginStateLabel(state: string) {
  const labels: Record<string, string> = { pending: 'Waiting for sign-in', authorizing: 'Waiting for sign-in', polling: 'Waiting for sign-in', choose_team: 'Choose a workspace', choose_project: 'Choose a project', creating_key: 'Connecting account', succeeded: 'Connected', failed: 'Sign-in failed', cancelled: 'Sign-in cancelled', interrupted: 'Sign-in interrupted', expired: 'Sign-in expired' };
  return labels[state] ?? 'Connecting account';
}
