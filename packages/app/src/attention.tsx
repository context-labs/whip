import { useEffect, useRef, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useWhipConnection } from '@whip/sdk/react';
import type { WhipClient } from '@whip/sdk';
import { Badge, Button, Sheet } from '@whip/ui';
import { Bell } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime } from './context';
import { sessionSearch } from './session-tabs';
import { layout } from './styles';
import { attentionQuery, AttentionNotifications, scanAttention } from './attention-notifications';

/** Mounted once by the desktop shell, independently of sidebar/window visibility. */
export function DesktopAttention({ client }: { client: WhipClient }) {
  const runtime = useRuntime();
  const connection = useWhipConnection(client);
  const runtimeId = connection.info?.runtime_id ?? '';
  const identity = `${runtimeId}:${connection.info?.connection_id ?? ''}`;
  const enabled = connection.state === 'connected' && !!runtimeId;
  const notifications = useRef(new AttentionNotifications());
  const observed = useRef({ identity: '', updatedAt: 0 });
  useEffect(() => {
    runtime.platform.setNotificationsEnabled?.(true);
    return () => runtime.platform.setNotificationsEnabled?.(false);
  }, [runtime]);
  const index = useQuery({
    queryKey: ['desktop-attention', runtimeId, connection.info?.connection_id],
    queryFn: ({ signal }) => scanAttention(client, runtime.queries, runtimeId, signal),
    enabled,
    refetchInterval: enabled ? 3000 : false,
    refetchIntervalInBackground: true,
    staleTime: 0,
  });
  useEffect(() => {
    if (observed.current.identity !== identity) {
      notifications.current.reset(); observed.current = { identity, updatedAt: 0 };
    }
    const current = client.getSnapshot();
    if (!enabled || index.isError || runtime.getSnapshot().client !== client || current.state !== 'connected'
      || current.info?.connection_id !== connection.info?.connection_id) {
      notifications.current.reset(); observed.current.updatedAt = index.dataUpdatedAt; return;
    }
    if (!index.data || index.isFetching || observed.current.updatedAt === index.dataUpdatedAt) return;
    observed.current.updatedAt = index.dataUpdatedAt;
    const messages = notifications.current.observe(runtimeId, index.data);
    let cancelled = false;
    for (const message of messages) void runtime.platform.notify?.(message).catch(error => {
      if (!cancelled && runtime.getSnapshot().client === client) runtime.report(error);
    });
    return () => { cancelled = true; };
  }, [client, runtime, runtimeId, identity, connection.info?.connection_id, enabled, index.data, index.dataUpdatedAt, index.isFetching, index.isError]);
  return null;
}

export function Attention({ client }: { client: WhipClient }) {
  const { preferences } = useAppState();
  const runtime = useRuntime();
  const close = (value: boolean) => {
    setOpen(value);
    if (!value) setAfter(undefined);
  };
  const connection = useWhipConnection(client);
  const [open, setOpen] = useState(false);
  const [after, setAfter] = useState<string>();
  const enabled = connection.state === 'connected';
  const index = useQuery({
    ...attentionQuery(client, connection.info?.runtime_id, after),
    enabled,
    refetchInterval: enabled ? 3000 : false,
  });
  const requests =
    index.data?.items?.filter(
      (item) =>
        BigInt(item.pending_permissions) > 0n || !!item.questions?.length,
    ) ?? [];
  return (
    <>
      <Button
        variant="ghost"
        aria-label={`Attention · ${requests.length} sessions on this page need you`}
        onClick={() => setOpen(true)}
      >
        <Bell size={15} />
        {requests.length > 0 && <Badge tone="warning">{requests.length}</Badge>}
      </Button>
      <span
        {...stylex.props(layout.srOnly)}
        aria-live={preferences.attentionAnnouncements ? 'polite' : 'off'}
      >
        {requests.length
          ? `${requests.length} sessions on this page need your attention.`
          : ''}
      </span>
      <Sheet
        open={open}
        onOpenChange={close}
        title="Activity and attention"
        description="A lightweight index of active sessions on this host. Open a session to respond."
      >
        <div {...stylex.props(layout.column)}>
          {index.error && <p role="alert">{index.error.message}</p>}
          {!index.data?.items?.length && (
            <p>No active sessions on this page.</p>
          )}
          {index.data?.items?.map((item) => (
            <Link
              key={item.root_id}
              to="/h/$runtimeId/s/$rootId"
              params={{
                runtimeId: connection.info?.runtime_id || '',
                rootId: item.root_id,
              }}
              search={sessionSearch(runtime.tabs.preferred(connection.info?.runtime_id || '', item.root_id))}
              state={{ whipViewId: runtime.tabs.preferred(connection.info?.runtime_id || '', item.root_id)?.id }}
              onClick={() => close(false)}
              {...stylex.props(layout.sessionLink)}
            >
              <div {...stylex.props(layout.column)}>
                <strong>{item.title || 'Untitled session'}</strong>
                <span {...stylex.props(layout.muted)}>
                  {item.active_agents} active agents ·{' '}
                  {item.pending_permissions} permissions ·{' '}
                  {item.questions?.length ?? 0} questions
                  {item.truncated ? ' · details truncated' : ''}
                </span>
              </div>
            </Link>
          ))}
          <div {...stylex.props(layout.row)}>
            {index.data?.has_more && (
              <Button
                variant="secondary"
                onClick={() => setAfter(index.data?.next_after_id)}
              >
                Next sessions
              </Button>
            )}
            <Button
              variant="ghost"
              onClick={() => {
                if (after) setAfter(undefined);
                else void index.refetch();
              }}
            >
              Refresh first page
            </Button>
          </div>
          {after && (
            <p {...stylex.props(layout.muted)}>
              This page is an advisory index. Refresh the first page to discover
              newly active sessions.
            </p>
          )}
        </div>
      </Sheet>
    </>
  );
}
