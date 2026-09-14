import { QueryClient } from '@tanstack/react-query';
import { afterEach, expect, it, vi } from 'vitest';
import type { WhipClient } from '@whip/sdk';
import { directoryCache, directoryOptions } from '../src/directory-queries';

afterEach(() => vi.useRealTimers());
function fixture(id = 'remote') {
  const directories = vi.fn(async ({ path }: { path?: string }) => ({ path: path!, parent: '/', entries: [], has_more: false, next_after: '' }));
  const client = { getSnapshot: () => ({ state: 'connected', info: { runtime_id: id } }), host: { directories } } as unknown as WhipClient;
  return { client, directories };
}
it('reuses a warmed listing for selection and navigation while isolating hosts and filters', async () => {
  vi.useFakeTimers();
  const queries = new QueryClient(), cache = directoryCache(queries), a = fixture('a'), b = fixture('b');
  cache.warm(a.client, { path: '/project' }); cache.warm(a.client, { path: '/project' });
  await vi.advanceTimersByTimeAsync(110);
  await queries.fetchQuery(directoryOptions(a.client, { path: '/project' }));
  expect(a.directories).toHaveBeenCalledTimes(1);
  await queries.fetchQuery(directoryOptions(b.client, { path: '/project' }));
  await queries.fetchQuery(directoryOptions(a.client, { path: '/project', hidden: true }));
  expect(a.directories).toHaveBeenCalledTimes(2); expect(b.directories).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(10_001);
  await queries.fetchQuery(directoryOptions(a.client, { path: '/project' }));
  expect(a.directories).toHaveBeenCalledTimes(3);
  queries.clear();
});

it('bounds speculative concurrency, cancels obsolete work and lets navigation reuse its request', async () => {
  vi.useFakeTimers();
  const queries = new QueryClient(), cache = directoryCache(queries), f = fixture();
  const signals: AbortSignal[] = [];
  f.directories.mockImplementation((_params, options?: { signal?: AbortSignal }) => new Promise((_resolve, reject) => {
    signals.push(options!.signal!);
    options!.signal!.addEventListener('abort', () => reject(new Error('cancelled')));
  }));
  for (let i = 0; i < 20; i++) cache.warm(f.client, { path: `/folder-${i}` });
  await vi.advanceTimersByTimeAsync(110);
  expect(f.directories).toHaveBeenCalledTimes(2);
  cache.cancel(f.client, { path: '/folder-19' });
  expect(signals.filter(signal => signal.aborted)).toHaveLength(1);
  cache.cancel(f.client); await vi.advanceTimersByTimeAsync(200);
  expect(signals.every(signal => signal.aborted)).toBe(true);
  expect(f.directories).toHaveBeenCalledTimes(2);
  queries.clear();
});

it('caps inactive pages and bytes and retires scroll positions with their queries', () => {
  const queries = new QueryClient(), cache = directoryCache(queries), f = fixture();
  for (let i = 0; i < 50; i++) {
    const options = directoryOptions(f.client, { path: `/folder-${i}` });
    queries.setQueryData(options.queryKey, { path: `/folder-${i}`, parent: '/', entries: [], has_more: false, next_after: '' }, { updatedAt: i + 1 });
  }
  expect(queries.getQueryCache().getAll()).toHaveLength(32);
  const options = directoryOptions(f.client, { path: '/large' });
  queries.setQueryData(options.queryKey, { path: '/large', parent: '/', entries: [], has_more: false, next_after: '' });
  cache.saveScroll(options, 160); expect(cache.scroll(options)).toBe(160);
  queries.setQueryData(options.queryKey, { path: '/large', parent: '/', entries: [{ name: 'x', path: 'x'.repeat(1024 * 1024) }], has_more: false, next_after: '' });
  expect(queries.getQueryData(options.queryKey)).toBeUndefined();
  expect(cache.scroll(options)).toBe(0);
  queries.clear();
});

it('expires inactive directory results after a minute', async () => {
  vi.useFakeTimers();
  const queries = new QueryClient(), f = fixture(); directoryCache(queries);
  const options = directoryOptions(f.client, { path: '/project' });
  await queries.fetchQuery(options);
  await vi.advanceTimersByTimeAsync(60_001);
  expect(queries.getQueryData(options.queryKey)).toBeUndefined();
  queries.clear();
});

it('does not run queued reads after a client attaches to a different runtime', async () => {
  vi.useFakeTimers();
  const queries = new QueryClient(), cache = directoryCache(queries), f = fixture();
  cache.warm(f.client, { path: '/project' });
  vi.spyOn(f.client, 'getSnapshot').mockReturnValue({ state: 'connected', info: { runtime_id: 'different' } } as never);
  await vi.advanceTimersByTimeAsync(110);
  expect(f.directories).not.toHaveBeenCalled();
  queries.clear();
});
