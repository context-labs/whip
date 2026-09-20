import { queryOptions, type QueryClient } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';

type Listing = { path?: string; prefix?: string; hidden?: boolean; after?: string };
export function directoryOptions(client: WhipClient, { path, prefix, hidden = false, after }: Listing = {}) {
  return queryOptions({
    queryKey: ['directories', client.getSnapshot().info?.runtime_id, path || '~', prefix || '', hidden, after ?? ''],
    queryFn: ({ signal }) => client.host.directories({ path: path || '~', prefix: prefix || undefined, show_hidden: hidden, after, limit: 64 }, { signal }),
    staleTime: 10_000, gcTime: 60_000, retry: false, networkMode: 'always',
    refetchOnWindowFocus: false, refetchOnReconnect: false,
  });
}

// One bounded queue/cache per application QueryClient. No directory data is persisted.
const caches = new WeakMap<QueryClient, ReturnType<typeof createDirectoryCache>>();
export function directoryCache(queries: QueryClient) {
  let cache = caches.get(queries);
  if (!cache) { cache = createDirectoryCache(queries); caches.set(queries, cache); }
  return cache;
}
function createDirectoryCache(queries: QueryClient) {
  type Job = { client: WhipClient; options: ReturnType<typeof directoryOptions>; key: string };
  const queued: Job[] = [];
  const running = new Map<string, Job>();
  const positions = new Map<string, number>();
  let pruning = false;
  let scheduled: ReturnType<typeof setTimeout> | undefined;
  const keyFor = (options: ReturnType<typeof directoryOptions>) => JSON.stringify(options.queryKey);
  function prune() {
    if (pruning) return;
    pruning = true;
    // Active pages are separately bounded by the API's 64-entry limit. Inactive
    // pages share a 32-page / 2 MiB budget across all hosts, including prefetches.
    const inactive = queries.getQueryCache().findAll({ queryKey: ['directories'] })
      .filter(query => !query.getObserversCount() && query.state.fetchStatus !== 'fetching')
      .sort((a, b) => b.state.dataUpdatedAt - a.state.dataUpdatedAt);
    let bytes = 0, count = 0;
    inactive.forEach(query => {
      const size = JSON.stringify(query.state.data ?? '').length * 2;
      if (count >= 32 || bytes + size > 2 * 1024 * 1024) queries.removeQueries({ queryKey: query.queryKey, exact: true });
      else { count++; bytes += size; }
    });
    pruning = false;
  }
  function schedule() {
    if (scheduled !== undefined || !queued.length) return;
    scheduled = setTimeout(() => { scheduled = undefined; pump(); }, 100);
  }
  function pump() {
    // Interactive reads always get priority over warming destinations.
    if (queries.getQueryCache().findAll({ queryKey: ['directories'] }).some(query => query.getObserversCount() && query.state.fetchStatus === 'fetching')) return;
    while (running.size < 2 && queued.length) {
      const job = queued.shift()!;
      const connection = job.client.getSnapshot();
      if (connection.state !== 'connected' || connection.info?.runtime_id !== job.options.queryKey[1]) continue;
      running.set(job.key, job);
      void queries.prefetchQuery(job.options).finally(() => { running.delete(job.key); prune(); schedule(); });
    }
  }
  queries.getQueryCache().subscribe(event => {
    if (event.query.queryKey[0] !== 'directories') return;
    if (event.type === 'removed') positions.delete(JSON.stringify(event.query.queryKey));
    if (event.type === 'updated' || event.type === 'observerRemoved') { prune(); schedule(); }
  });
  return {
    warm(client: WhipClient, listing: Listing) {
      if (client.getSnapshot().state !== 'connected' || !client.getSnapshot().info?.runtime_id) return;
      const options = directoryOptions(client, listing), key = keyFor(options);
      const query = queries.getQueryCache().find({ queryKey: options.queryKey, exact: true });
      if (query && !query.isStaleByTime(10_000)) return;
      if (running.has(key) || queued.some(job => job.key === key)) return;
      if (queued.length >= 8) queued.pop();
      queued.unshift({ client, options, key }); schedule();
    },
    cancel(client: WhipClient, keep?: Listing) {
      const keepKey = keep ? keyFor(directoryOptions(client, keep)) : undefined;
      for (let i = queued.length - 1; i >= 0; i--) if (queued[i].client === client) queued.splice(i, 1);
      for (const job of running.values()) {
        if (job.client !== client || job.key === keepKey) continue;
        const query = queries.getQueryCache().find({ queryKey: job.options.queryKey, exact: true });
        if (!query?.getObserversCount()) void queries.cancelQueries({ queryKey: job.options.queryKey, exact: true });
      }
    },
    saveScroll(options: ReturnType<typeof directoryOptions>, top: number) { positions.set(keyFor(options), top); },
    scroll(options: ReturnType<typeof directoryOptions>) { return positions.get(keyFor(options)) ?? 0; },
  };
}
