import { createContext, useContext, useState, type PropsWithChildren } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { HostAttentionResult, ListTreesResult, RecentTreesResult } from '@whip/protocol';
import { useWorkspace, useWorkspaceState } from '../runtime/workspace-context';
import type { SavedHost } from '../runtime/runtime';

type Page = { host: SavedHost; sessions?: ListTreesResult; recent?: RecentTreesResult; attention?: HostAttentionResult; error?: string };
type Cursor = { kind: 'catalog'; after?: string; revision?: string } | { kind: 'attention'; after: NonNullable<HostAttentionResult['next_cursor']> };
export function useWorkspaceIndex(kind: 'sessions' | 'attention', search = '', status: 'active' | 'archived' = 'active', focused = true) {
  const workspace = useWorkspace(); const state = useWorkspaceState();
  const [cursors, setCursors] = useState<Record<string, Cursor>>({});
  const key = state.connections.map(runtime => { const s = runtime.getSnapshot(); return [s.host?.id, s.host?.runtimeId, s.client?.processEpoch, workspace.connectionKey(s.host?.id ?? ''), s.ready]; });
  const enabled = state.active && focused && state.connections.some(r => r.getSnapshot().ready);
  const result = useQuery({ queryKey: ['workspace-index', kind, key, search, status, cursors], enabled,
    gcTime: 0, staleTime: 10_000, refetchInterval: enabled ? 10_000 : false,
    queryFn: ({ signal }) => Promise.all(state.connections.filter(r => r.getSnapshot().host).map(async (runtime): Promise<Page> => {
      const connection = runtime.getSnapshot(); const host = connection.host!; const client = connection.client;
      if (!connection.ready || !client) return { host, error: connection.connecting ? 'Connecting…' : 'Host disconnected' };
      try {
        return await workspace.reads.run(signal, async () => {
          if (!runtime.getSnapshot().active || !runtime.getSnapshot().ready || runtime.getSnapshot().client !== client) throw new Error('Host connection changed');
          const cursor = cursors[host.id]; let page: Page;
          if (kind === 'attention') page = { host, attention: await client.call('host.attention', { after: cursor?.kind === 'attention' ? cursor.after : null, limit: 64, max_bytes: 128 << 10 }, { signal }) };
          else if (!search && status === 'active' && !cursor) page = { host, recent: await client.trees.recent(64, { signal }) };
          else page = { host, sessions: await client.trees.list({ search: search || undefined, archived: status === 'archived', limit: 64,
            ...(cursor?.kind === 'catalog' ? { after: cursor.after, expected_revision: cursor.revision } : {}) }, { signal }) };
          if (new TextEncoder().encode(JSON.stringify(page)).byteLength > 256 << 10) throw new Error('This host page exceeds the 256 KiB mobile limit. Narrow the search.');
          return page;
        });
      } catch (error) { return { host, error: error instanceof Error ? error.message : String(error) }; }
    })),
  });
  const pages = result.data ?? [];
  const unavailable = state.hosts.filter(host => !state.connections.some(r => r.getSnapshot().host?.id === host.id && r.getSnapshot().ready));
  const partial = unavailable.length > 0 || pages.some(p => p.error || p.sessions?.next_cursor || p.recent?.has_more || p.attention?.next_cursor) || Object.keys(cursors).length > 0;
  return { ...result, pages, partial, unavailable, enabled,
    next: (hostId: string, cursor: Cursor) => setCursors(current => ({ ...current, [hostId]: cursor })),
    first: () => setCursors({}), hasPrevious: Object.keys(cursors).length > 0,
  };
}
function useOwner() {
  const result = useWorkspaceIndex('attention');
  const items = result.pages.flatMap(page => (page.attention?.items ?? []).filter(item => BigInt(item.activity.pending_permission_count) > 0n || BigInt(item.activity.pending_question_count) > 0n).map(item => ({ ...item, host: page.host })));
  const count = items.length; const incomplete = result.partial || !result.enabled || result.isError;
  return { ...result, items, count, badge: result.isPending && result.enabled ? '…' : incomplete ? `${count || '?'}${count ? '+' : ''}` : count ? String(count) : undefined };
}
const Context = createContext<ReturnType<typeof useOwner> | null>(null);
export function WorkspaceAttentionProvider({ children }: PropsWithChildren) { const owner = useOwner(); return <Context.Provider value={owner}>{children}</Context.Provider>; }
export function useWorkspaceAttention() { const value = useContext(Context); if (!value) throw new Error('Workspace attention owner is missing'); return value; }
