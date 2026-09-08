import { useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useQueries } from '@tanstack/react-query';
import { WhipError } from '@whip/sdk';
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
  const [filter, setFilter] = useState('');
  const [after, setAfter] = useState<Record<string, string | undefined>>({});
  const close = (value: boolean) => {
    setOpen(value);
    if (!value) { setAfter({}); setFilter(''); }
  };
  // One advisory page per host supplies the badge without leasing any root views.
  const indexes = useQueries({ queries: hosts.map(host => ({
    queryKey: ['host-attention', host.runtimeId, after[host.runtimeId ?? '']],
    queryFn: ({ signal }: { signal: AbortSignal }) => host.client!.host.attention({ after_id: after[host.runtimeId!], limit: 64, max_bytes: 256 << 10 }, { signal }),
    enabled: !!host.client && host.state === 'connected',
    refetchInterval: (query: { state: { error: unknown } }) => host.client && host.state === 'connected'
      && !(query.state.error instanceof WhipError && query.state.error.kind === 'unsupported_operation') ? 3000 : false,
    gcTime: 0,
  })) });
  const groups = hosts.map((host, index) => ({ host, index: indexes[index]! }));
  const requests = groups.reduce((count, { host, index }) => count + (host.client && host.state === 'connected'
    ? index.data?.items?.filter(item => BigInt(item.pending_permissions) > 0n || !!item.questions?.length).length ?? 0 : 0), 0);
  const incomplete = groups.some(({ host, index }) => !host.client || host.state !== 'connected' || index.isPending || index.isError || index.data?.has_more || index.data?.truncated);
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
        {hosts.length > 1 && <Select label="Attention host" value={filter} options={[{ value: '', label: 'All hosts' }, ...hosts.map(host => ({ value: host.id, label: host.name }))]} onValueChange={setFilter} />}
        {groups.filter(({ host }) => !filter || host.id === filter).map(({ host, index }) => {
          const runtimeId = host.runtimeId ?? '';
          const connected = !!host.client && host.state === 'connected';
          const page = connected ? index.data : undefined;
          const error = connected ? index.error?.message : host.error ?? `${host.name} is ${host.state}. Connect it to see its attention requests.`;
          return <section key={host.id} aria-label={`${host.name} attention`} {...stylex.props(layout.column)}>
            <h3>{host.name}</h3>
            {error && <p role="alert">{host.name}: {error}</p>}
            {connected && index.isPending && <p role="status">Loading activity…</p>}
            {!error && !index.isPending && !page?.items?.length && <p>No active sessions on this page.</p>}
            {page?.items?.map(item => {
              const saved = runtime.tabs.preferred(runtimeId, item.root_id);
              return <Link key={item.root_id} to="/h/$runtimeId/s/$rootId" params={{ runtimeId, rootId: item.root_id }}
                search={sessionSearch(saved)} state={{ whipViewId: saved?.id }} preload={false}
                aria-label={`${item.title || 'Untitled session'} · ${host.name}`}
                onClick={event => {
                  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                  try { runtime.tabs.open(runtimeId, item.root_id, item.title); close(false); }
                  catch (error) { event.preventDefault(); runtime.report(error); }
                }} {...stylex.props(layout.sessionLink)}>
                <div {...stylex.props(layout.column)}>
                  <strong>{item.title || 'Untitled session'}</strong>
                  <span {...stylex.props(layout.muted)}>
                    {item.active_agents} active agents · {item.pending_permissions} permissions · {item.questions?.length ?? 0} questions
                    {item.truncated ? ' · details truncated' : ''}
                  </span>
                </div>
              </Link>;
            })}
            {connected && <div {...stylex.props(layout.row)}>
              {page?.has_more && <Button variant="secondary" disabled={index.isFetching} onClick={() => setAfter(current => ({ ...current, [runtimeId]: page.next_after_id }))}>Next sessions on {host.name}</Button>}
              <Button variant="ghost" onClick={() => {
                if (after[runtimeId]) setAfter(current => ({ ...current, [runtimeId]: undefined }));
                else void index.refetch();
              }}>Refresh first page on {host.name}</Button>
            </div>}
            {(after[runtimeId] || page?.has_more || page?.truncated) && <p {...stylex.props(layout.muted)}>
              This page is an advisory index and may omit other sessions needing attention. Refresh the first page to discover newly active sessions.
            </p>}
          </section>;
        })}
      </div>
    </Sheet>
  </>;
}
