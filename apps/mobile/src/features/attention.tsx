import { createContext, useCallback, useContext, useMemo, useSyncExternalStore, type PropsWithChildren } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { useFocusEffect } from 'expo-router';
import type { HostAttentionResult } from '@whip/protocol';
import type { MobileRuntime } from '../runtime/runtime';

const PAGE_LIMIT = 4;
const POLL_MS = 10_000;
type Status = 'current' | 'stale' | 'loading' | 'unavailable';

export function attentionSummary(pages: readonly HostAttentionResult[], status: Status) {
  const items = [...new Map(pages.flatMap(page => page.items ?? []).map(item => [item.root_id, item])).values()];
  const count = items.filter(item => BigInt(item.pending_permissions) > 0n || !!item.questions?.length).length;
  const partial = !!pages.at(-1)?.has_more || pages.some(page => page.truncated) || items.some(item => item.truncated);
  const countLabel = `${count}${partial ? '+' : ''} loaded session${count === 1 ? '' : 's'} with human requests`;
  const badge = status === 'loading' ? '…' : status !== 'current' ? '?' : count ? `${count}${partial ? '+' : ''}` : partial ? '?' : undefined;
  const accessibilityLabel = `Attention. ${status === 'loading' ? 'Checking for human requests.' : status === 'unavailable' ? 'Index unavailable.' : `${countLabel}. ${status === 'stale' ? 'Last observed count; index is stale. ' : ''}${partial ? 'Partial index; more sessions may need you.' : 'All index pages loaded.'}`}`;
  return { items, count, partial, countLabel, badge, accessibilityLabel };
}

function useAttentionOwner(runtime: MobileRuntime) {
  const { client, host, ready, active } = useSyncExternalStore(runtime.subscribe, runtime.getSnapshot);
  const supported = !!client?.supports('rpc', 'host.attention');
  const enabled = ready && active && supported;
  const current = useCallback(() => {
    const state = runtime.getSnapshot();
    return enabled && state.ready && state.active && state.client === client;
  }, [runtime, client, enabled]);
  const result = useInfiniteQuery({
    queryKey: [host?.runtimeId, 'attention'], enabled,
    initialPageParam: undefined as string | undefined, maxPages: PAGE_LIMIT, refetchInterval: enabled ? POLL_MS : false,
    queryFn: ({ pageParam, signal }) => {
      if (!current()) throw new Error('Attention is paused until this host is ready.');
      return client!.host.attention({ after_id: pageParam, limit: 64, max_bytes: 128 << 10 }, { signal });
    },
    getNextPageParam: (page, pages) => pages.length < PAGE_LIMIT && page.has_more ? page.next_after_id : undefined,
  });
  const status: Status = !result.data ? enabled && !result.error ? 'loading' : 'unavailable'
    : !enabled || result.error || result.isStale ? 'stale' : 'current';
  const summary = useMemo(() => attentionSummary(result.data?.pages ?? [], status), [result.data, status]);
  const refresh = useCallback(async () => {
    if (current()) await result.refetch({ cancelRefetch: false });
  }, [current, result.refetch]);
  const loadMore = useCallback(async () => {
    if (current() && result.hasNextPage && !result.isFetching) await result.fetchNextPage({ cancelRefetch: false });
  }, [current, result.hasNextPage, result.isFetching, result.fetchNextPage]);
  const statusMessage = !active ? 'Attention refresh is paused while Whip is in the background.'
    : !client || !ready ? 'Connect to this host to refresh its attention index.'
    : !supported ? 'This host does not support the attention index.'
    : result.error ? `Attention refresh failed. ${result.error.message}`
    : status === 'stale' ? 'Refreshing the last observed attention index.' : undefined;
  return { ...summary, status, statusMessage, refresh, loadMore, runtimeId: host?.runtimeId,
    enabled, isFetching: result.isFetching, isRefreshing: result.isRefetching,
    hasNextPage: result.hasNextPage, atPageLimit: (result.data?.pages.length ?? 0) === PAGE_LIMIT && !!result.data?.pages.at(-1)?.has_more,
  };
}

const AttentionContext = createContext<ReturnType<typeof useAttentionOwner> | null>(null);

/** One foreground observer, shared by every route and the tab badge. */
export function AttentionProvider({ runtime, children }: PropsWithChildren<{ runtime: MobileRuntime }>) {
  const attention = useAttentionOwner(runtime);
  return <AttentionContext.Provider value={attention}>{children}</AttentionContext.Provider>;
}
export function useAttention() {
  const attention = useContext(AttentionContext);
  if (!attention) throw new Error('Attention owner is not mounted.');
  return attention;
}
export function useAttentionFocus() {
  const { refresh } = useAttention();
  useFocusEffect(useCallback(() => { void refresh(); }, [refresh]));
}
