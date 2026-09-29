import { ErrorNotice } from '../error-feedback';
import { useId, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { DeliveryError, RecoveryError, RecoveryPersistenceError } from '@whip/sdk';
import type { Client, Operations, Session, SessionRecord, Tree } from '@whip/sdk';
import type { DeepReadonly, ExecutionView, SessionView } from '@whip/sdk/state';
import { Button } from '@whip/ui';
import * as stylex from '@stylexjs/stylex';
import { layout } from '../styles';

export interface InspectorProps {
  client: Client;
  session: Session;
  rootId: string;
  tree: DeepReadonly<Tree>;
  selected: DeepReadonly<SessionRecord>;
  view: SessionView;
  execution: ExecutionView;
  connected: boolean;
  viewId?: string;
  kind?: 'chat' | 'repl' | 'trace';
}
type DetailRead =
  | 'sessions.list'
  | 'mail.list'
  | 'mail.read'
  | 'state.history'
  | 'schedules.list'
  | 'schedules.get'
  | 'context.compaction'
  | 'context.head'
  | 'context.compactions'
  | 'context.snapshot'
  | 'turns.instructions'
  | 'workspace.inspect'
  | 'permissions.policy'
  | 'grants.list'
  | 'budgets.list'
  | 'resources.list'
  | 'tool.schemas'
  | 'mcp.status'
  | 'mcp.configuration'
  | 'lsp.status'
  | 'computer.status'
  | 'host.browser_driver'
  | 'browser.attachments'
  | 'definitions.get'
  | 'goals.current'
  | 'state.list'
  | 'state.read';
export function useDetailQuery<M extends DetailRead>(
  props: Pick<InspectorProps, 'client' | 'session' | 'connected'>,
  method: M,
  params: Operations[M]['params'],
  poll = false,
) {
  const query = useQuery({
    queryKey: [
      'inspector',
      props.client.runtimeID,
      props.client.processEpoch,
      props.session.id,
      method,
      params,
    ],
    queryFn: ({ signal }) => props.client.call(method, params, { signal }),
    enabled: props.connected,
    gcTime: 0,
    retry: false,
    refetchInterval: props.connected && poll ? 3000 : false,
  });
  return { ...query, errorOwner: `${props.client.runtimeID}:${props.session.id}:${method}` };
}
export function QueryFeedback({
  query,
  connected,
}: {
  query: {
    error: Error | null;
    isLoading: boolean;
    errorOwner?: string;
    refetch?(): Promise<unknown>;
  };
  connected: boolean;
}) {
  if (!connected) return <p role="status">Unavailable while this host is offline.</p>;
  if (query.error)
    return (
      <ErrorNotice
        type="resource"
        owner={query.errorOwner ?? 'inspector'}
        title="Could not load this resource"
        error={query.error}
        action={
          query.refetch && (
            <Button variant="ghost" onClick={() => void query.refetch?.()}>
              Retry
            </Button>
          )
        }
      />
    );
  if (query.isLoading)
    return (
      <p role="status" {...stylex.props(layout.muted)}>
        Loading from the execution host…
      </p>
    );
  return null;
}
export function Action({
  children,
  run,
  disabled,
  danger = false,
  recoverable = false,
  successLabel = 'Applied',
}: {
  children: ReactNode;
  run: () => Promise<unknown>;
  disabled?: boolean;
  danger?: boolean;
  recoverable?: boolean;
  successLabel?: string;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [done, setDone] = useState(false);
  const [needsRecovery, setNeedsRecovery] = useState(false);
  const actionId = useId();
  return (
    <div {...stylex.props(layout.column)}>
      <Button
        size="sm"
        variant={danger ? 'danger' : 'secondary'}
        disabled={disabled || busy || needsRecovery}
        loading={busy}
        onClick={() => {
          if (disabled || busy || needsRecovery) return;
          setBusy(true);
          setError('');
          setDone(false);
          void run()
            .then(
              () => setDone(true),
              (error) => {
                setError(error instanceof Error ? error.message : String(error));
                if (
                  recoverable &&
                  (error instanceof DeliveryError ||
                    error instanceof RecoveryError ||
                    error instanceof RecoveryPersistenceError ||
                    (error instanceof Error && error.name === 'AbortError'))
                )
                  setNeedsRecovery(true);
              },
            )
            .finally(() => setBusy(false));
        }}
      >
        {children}
      </Button>
      {error && (
        <ErrorNotice
          type="action"
          owner={`inspector-action:${actionId}`}
          title="Could not complete this action"
          error={error}
        />
      )}
      {needsRecovery && (
        <p role="status">
          Inspect this command in Settings → Command recovery before starting another. Its exact
          request has been retained when recovery storage is available.
        </p>
      )}
      {done && (
        <span role="status" {...stylex.props(layout.muted)}>
          {successLabel}
        </span>
      )}
    </div>
  );
}
export function Section({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <section {...stylex.props(layout.column)}>
      <h3 {...stylex.props(layout.title)}>{title}</h3>
      {description && <p {...stylex.props(layout.muted)}>{description}</p>}
      {children}
    </section>
  );
}
export function Empty({ children }: { children: ReactNode }) {
  return <p {...stylex.props(layout.muted)}>{children}</p>;
}
export { ContentRead } from './content-read';

/** Replace one bounded page; never accumulate metadata behind the inspector. */
export function PageControls({
  after,
  next,
  busy,
  connected,
  onChange,
}: {
  after?: string;
  next?: string;
  busy: boolean;
  connected: boolean;
  onChange(value: string | undefined): void;
}) {
  return (
    <div {...stylex.props(layout.row)}>
      {after !== undefined && (
        <Button variant="ghost" disabled={!connected || busy} onClick={() => onChange(undefined)}>
          First page
        </Button>
      )}
      {next !== undefined && (
        <Button variant="ghost" disabled={!connected || busy} onClick={() => onChange(next)}>
          Next page
        </Button>
      )}
    </div>
  );
}
