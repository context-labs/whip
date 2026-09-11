import { createContext, useContext, useEffect, useState, type PropsWithChildren } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { HostAttentionResult, SessionCatalogPage } from '@whip/protocol';
import { useWorkspace, useWorkspaceState } from '../runtime/workspace-context';
import type { SavedHost } from '../runtime/runtime';

type Page = { host: SavedHost; sessions?: SessionCatalogPage; attention?: HostAttentionResult; error?: string };
type Cursor = NonNullable<SessionCatalogPage['next_cursor']> | string;
export function useWorkspaceIndex(kind: 'sessions' | 'attention', search = '', status: 'active' | 'archived' = 'active', focused = true) {
  const workspace = useWorkspace(); const state = useWorkspaceState();
  const [cursors, setCursors] = useState<Record<string, Cursor>>({});
  const key = state.connections.map(runtime => { const s = runtime.getSnapshot(); return [s.host?.id, s.host?.runtimeId, workspace.connectionKey(s.host?.id ?? ''), s.ready]; });
  const enabled = state.active && focused && state.connections.some(r => r.getSnapshot().ready);
  const result = useQuery({ queryKey: ['workspace-index', kind, key, search, status, cursors], enabled,
    gcTime: 0, staleTime: 10_000, refetchInterval: enabled ? 10_000 : false,
    queryFn: ({ signal }) => Promise.all(state.connections.filter(r => r.getSnapshot().host).map(async (runtime): Promise<Page> => {
      const connection = runtime.getSnapshot(); const host = connection.host!; const client = connection.client;
      if (!connection.ready || !client) return { host, error: connection.connecting ? 'Connecting…' : 'Host disconnected' };
      try {
        return await workspace.reads.run(signal, async () => {
          if (!runtime.getSnapshot().active || !runtime.getSnapshot().ready || runtime.getSnapshot().client !== client) throw new Error('Host connection changed');
          if (kind === 'sessions') return { host, sessions: await client.sessions.list({ search: search || undefined, status, cursor: typeof cursors[host.id] === 'object' ? cursors[host.id] as NonNullable<SessionCatalogPage['next_cursor']> : undefined, limit: 128, max_bytes: 256 << 10 }, { signal }) };
          if (!client.supports('rpc', 'host.attention')) throw new Error('Attention is not supported by this host');
          return { host, attention: await client.host.attention({ after_id: typeof cursors[host.id] === 'string' ? cursors[host.id] as string : undefined, limit: 64, max_bytes: 128 << 10 }, { signal }) };
        });
      } catch (error) { return { host, error: error instanceof Error ? error.message : String(error) }; }
    })),
  });
  const pages = result.data ?? [];
  const unavailable = state.hosts.filter(host => !state.connections.some(r => r.getSnapshot().host?.id === host.id && r.getSnapshot().ready));
  const partial = unavailable.length > 0 || pages.some(p => p.error || p.sessions?.has_more || p.attention?.has_more || p.attention?.truncated || p.attention?.items?.some(i => i.truncated)) || Object.keys(cursors).length > 0;
  return { ...result, pages, partial, unavailable, enabled,
    next: (hostId: string, cursor: Cursor) => setCursors(current => ({ ...current, [hostId]: cursor })),
    first: () => setCursors({}), hasPrevious: Object.keys(cursors).length > 0,
  };
}
function useOwner() {
  const result = useWorkspaceIndex('attention');
  const workspace = useWorkspace(); const state = useWorkspaceState(); const query = useQueryClient();
  const identity = state.connections.map(r => `${workspace.connectionKey(r.getSnapshot().host?.id ?? '')}:${r.getSnapshot().ready}`).join(',');
  useEffect(() => { const stops = state.connections.map(r => r.getSnapshot().client?.onCommand(outcome => { if (['succeeded', 'failed', 'cancelled', 'interrupted'].includes(outcome.status)) void query.invalidateQueries({ queryKey: ['workspace-index'] }); })); return () => stops.forEach(stop => stop?.()); }, [identity, query]);
  const items = result.pages.flatMap(page => (page.attention?.items ?? []).filter(item => BigInt(item.pending_permissions) > 0n || item.questions?.length).map(item => ({ ...item, host: page.host })));
  const count = items.length; const incomplete = result.partial || !result.enabled || result.isError;
  return { ...result, items, count, badge: result.isPending && result.enabled ? '…' : incomplete ? `${count || '?'}${count ? '+' : ''}` : count ? String(count) : undefined };
}
const Context = createContext<ReturnType<typeof useOwner> | null>(null);
export function WorkspaceAttentionProvider({ children }: PropsWithChildren) { const owner = useOwner(); return <Context.Provider value={owner}>{children}</Context.Provider>; }
export function useWorkspaceAttention() { const value = useContext(Context); if (!value) throw new Error('Workspace attention owner is missing'); return value; }
