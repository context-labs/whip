import { ErrorNotice } from './error-feedback';
import { useEffect, useRef, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useQueries, useQuery } from '@tanstack/react-query';
import type { HostAttentionParams } from '@whip/protocol';
import type { HostConnection } from './hosts';
import { clientQueryKey } from './client-query-key';
import { AttentionNotifications, scanAttention, attentionQuery } from './attention-notifications';
import { Badge, Button, Select, Sheet } from '@whip/ui';
import { Bell } from 'lucide-react';
import * as stylex from '@stylexjs/stylex';
import { useAppState, useRuntime, useSessionTabs } from './context';
import { sessionSearch } from './session-tabs';
import { layout } from './styles';

export function Attention() {
  const { preferences, hosts } = useAppState();
  const runtime = useRuntime();
  useSessionTabs();
  const [open, setOpen] = useState(false);
  const [actionError, setActionError] = useState<{ owner: string; error: unknown }>();
  const [filter, setFilter] = useState('');
  const [after, setAfter] = useState<Record<string, HostAttentionParams['after']>>({});
  const close = (value: boolean) => {
    setOpen(value);
    if (!value) { setAfter({}); setFilter(''); setActionError(undefined); }
  };
  // One advisory page per host supplies the badge without leasing any root views.
  const indexes = useQueries({ queries: hosts.map(host => ({
    ...attentionQuery(host.client, host.runtimeId, after[host.client ? clientQueryKey(host.client) : host.id] ?? null),
    enabled: !!host.client && host.state === 'connected',
    refetchInterval: host.client && host.state === 'connected' ? 3000 : false,
    gcTime: 0,
  })) });
  const groups = hosts.map((host, index) => ({ host, index: indexes[index]! }));
  const requests = groups.reduce((count, { host, index }) => count + (host.client && host.state === 'connected'
    ? index.data?.items?.filter(item => BigInt(item.activity.pending_permission_count) > 0n || BigInt(item.activity.pending_question_count) > 0n).length ?? 0 : 0), 0);
  const incomplete = groups.some(({ host, index }) => !host.client || host.state !== 'connected' || index.isPending || index.isError || index.data?.next_cursor);
  const description = `${requests} ${requests === 1 ? 'session' : 'sessions'} on loaded pages ${requests === 1 ? 'needs' : 'need'} you${incomplete ? ' · some sessions are not included' : ''}`;
  return <>
    <Button variant="ghost" aria-label={`Attention · ${description}`} onClick={() => setOpen(true)}>
      <Bell size={15} />
      {requests > 0 && <Badge tone="warning">{requests}</Badge>}
    </Button>
    <span {...stylex.props(layout.srOnly)} aria-live={preferences.attentionAnnouncements ? 'polite' : 'off'}>
      {requests ? description : ''}
    </span>
    <Sheet open={open} onOpenChange={close} title="Activity and attention"
      description="Active sessions across your hosts. Open a session to respond on its execution host.">
      <div {...stylex.props(layout.column)}>
        {actionError && <ErrorNotice type="action" owner={actionError.owner} error={actionError.error} title="Could not open session" onDismiss={() => setActionError(undefined)} />}
        {hosts.length > 1 && <Select label="Attention host" value={filter} options={[{ value: '', label: 'All hosts' }, ...hosts.map(host => ({ value: host.id, label: host.name }))]} onValueChange={setFilter} />}
        {groups.filter(({ host }) => !filter || host.id === filter).map(({ host, index }) => {
          const runtimeId = host.runtimeId ?? '';
          const key = host.client ? clientQueryKey(host.client) : host.id;
          const connected = !!host.client && host.state === 'connected';
          const page = connected ? index.data : undefined;
          const error = connected ? index.error : undefined;
          return <section key={host.id} aria-label={`${host.name} attention`} {...stylex.props(layout.column)}>
            <h3>{host.name}</h3>
            {error && <ErrorNotice type="resource" owner={`attention:${host.id}`} error={error} title={`Could not load activity on ${host.name}`} />}
            {!connected && <p role="status">Activity unavailable while offline. <Link to="/settings" search={{ section: "connections" }} onClick={() => close(false)}>Manage servers</Link></p>}
            {connected && index.isPending && <p role="status">Loading activity…</p>}
            {connected && !error && !index.isPending && !page?.items?.length && <p>No active sessions on this page.</p>}
            {page?.items?.map(item => {
              const saved = runtime.tabs.preferred(runtimeId, item.root_id);
              const search = { ...sessionSearch(saved), agent: item.session_id === item.root_id ? undefined : item.session_id };
              return <Link key={item.session_id} to="/h/$runtimeId/s/$rootId" params={{ runtimeId, rootId: item.root_id }}
                search={search} state={{ whipViewId: saved?.id }} preload={false}
                aria-label={`${item.title || 'Untitled session'} · ${host.name}`}
                onClick={event => {
                  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                  try { const id = runtime.tabs.open(runtimeId, item.root_id, item.title ?? ''); runtime.tabs.updateLocation(id, search); close(false); }
                  catch (error) { event.preventDefault(); setActionError({ owner: `${runtimeId}:${item.root_id}`, error }); }
                }} {...stylex.props(layout.sessionLink)}>
                <div {...stylex.props(layout.column)}>
                  <strong>{item.title || 'Untitled session'}</strong>
                  <span {...stylex.props(layout.muted)}>
                    {item.activity.active_turn ? 'Running · ' : ''}{item.activity.pending_permission_count} permissions · {item.activity.pending_question_count} questions{item.session_id !== item.root_id ? ' · child agent' : ''}
                  </span>
                </div>
              </Link>;
            })}
            {connected && <div {...stylex.props(layout.row)}>
              {page?.next_cursor && <Button variant="secondary" disabled={index.isFetching} onClick={() => setAfter(current => Object.fromEntries([...Object.entries(current).filter(([id]) => id !== key), [key, page.next_cursor]].slice(-4)))}>Next sessions on {host.name}</Button>}
              <Button variant="ghost" onClick={() => {
                if (after[key]) setAfter(current => ({ ...current, [key]: null }));
                else void index.refetch();
              }}>Refresh first page on {host.name}</Button>
            </div>}
            {(after[key] || page?.next_cursor) && <p {...stylex.props(layout.muted)}>
              This page is an advisory index and may omit other sessions needing attention. Refresh the first page to discover newly active sessions.
            </p>}
          </section>;
        })}
      </div>
    </Sheet>
  </>;
}

export function DesktopAttention({ host }: { host: HostConnection }) {
  const runtime = useRuntime();
  const client = host.client;
  const runtimeId = host.runtimeId ?? '';
  const identity = client ? clientQueryKey(client) : host.id;
  const enabled = !!client && host.state === 'connected' && runtimeId === client.runtimeID;
  const notifications = useRef(new AttentionNotifications());
  const observed = useRef({ identity: '', updatedAt: 0 });
  const index = useQuery({
    queryKey: ['desktop-attention', runtimeId, identity],
    queryFn: ({ signal }) => scanAttention(client!, runtime.queries, runtimeId, signal),
    enabled,
    refetchInterval: enabled ? 3000 : false,
    refetchIntervalInBackground: true,
    staleTime: 0, gcTime: 0,
  });
  useEffect(() => {
    if (observed.current.identity !== identity) {
      notifications.current.reset(); observed.current = { identity, updatedAt: 0 };
    }
    if (!enabled || index.isError || !client || !runtime.connections.isAttached(client)) {
      notifications.current.reset(); observed.current.updatedAt = index.dataUpdatedAt; return;
    }
    if (!index.data || index.isFetching || observed.current.updatedAt === index.dataUpdatedAt) return;
    observed.current.updatedAt = index.dataUpdatedAt;
    const messages = notifications.current.observe(runtimeId, index.data);
    let cancelled = false;
    for (const message of messages) void runtime.platform.notify?.(message).catch(error => {
      if (!cancelled && runtime.connections.isAttached(client)) runtime.report(error);
    });
    return () => { cancelled = true; };
  }, [client, runtime, runtimeId, identity, enabled, index.data, index.dataUpdatedAt, index.isFetching, index.isError]);
  return null;
}
