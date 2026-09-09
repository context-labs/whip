import { useEffect, useRef, useState } from 'react';
import type { WhipClient } from '@whip/sdk';
import type { ProviderLoginStatus } from '@whip/protocol';
import { Badge, Button, Field, Input, Select } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { useRuntime } from '../context';
import { layout } from '../styles';

export function LoginFlow({
  flow,
  client,
  enabled,
  refresh,
}: {
  flow: ProviderLoginStatus;
  client: WhipClient;
  enabled: boolean;
  refresh(): void;
}) {
  const runtime = useRuntime();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const request = useRef<AbortController | null>(null);
  useEffect(() => () => request.current?.abort(), []);
  const action = async (run: (signal: AbortSignal) => Promise<unknown>) => {
    if (!enabled || busy) return;
    const controller = new AbortController();
    request.current = controller; setBusy(true);
    try {
      await run(controller.signal);
      if (!controller.signal.aborted) refresh();
    } catch (error) {
      if (!controller.signal.aborted) runtime.report(error);
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
  return (
    <div {...stylex.props(layout.notice, layout.column)}>
      <div {...stylex.props(layout.row)}>
        <span>{flow.provider === 'openai-codex' ? 'OpenAI (ChatGPT subscription)' : 'Inference.net'}</span>
        <Badge>{flow.state}</Badge>
        <span>{flow.email}</span>
      </div>
      {flow.verification_url && !terminal && (
        <>
          <Button
            variant="secondary"
            onClick={() =>
              void runtime.platform.openExternal(flow.verification_url!).catch(error => runtime.report(error))
            }
          >
            Open verification page
          </Button>
          <span>
            Verification code: <strong>{flow.user_code}</strong>
          </span>
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
      {flow.team_id && !flow.project_id && !terminal && (
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
            Create project
          </Button>
        </>
      )}
      {flow.error && <p role="alert">{flow.error}</p>}
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
