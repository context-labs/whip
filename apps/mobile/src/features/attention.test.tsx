import { act, cleanup, render } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { HostAttentionResult } from '@whip/protocol';
import type { WhipClient } from '@whip/sdk';
import type { MobileRuntime } from '../runtime/runtime';
import { AttentionProvider, attentionSummary, useAttention, useAttentionFocus } from './attention';

let mockFocused = false;
jest.mock('expo-router', () => ({ useFocusEffect: (effect: () => void) => require('react').useEffect(() => mockFocused ? effect() : undefined, [effect, mockFocused]) }));

type Item = NonNullable<HostAttentionResult['items']>[number];
const item = (id: string, permissions = '0', questions: Item['questions'] = []): Item => ({ root_id: id, title: id, active_agents: '1', pending_permissions: permissions, questions, truncated: false });
const page = (items: Item[] = [], more = false, next?: string): HostAttentionResult => ({ items, has_more: more, next_after_id: next, truncated: false });
const clients: QueryClient[] = [];
let attention: ReturnType<typeof useAttention>;
function Probe() { attention = useAttention(); return null; }
function ScreenProbe() { useAttention(); useAttentionFocus(); return null; }
function fixture(read = jest.fn(async (_params: unknown, _options: { signal: AbortSignal }) => page([item('root', '1')]))) {
  const query = new QueryClient({ defaultOptions: { queries: { staleTime: 10_000, gcTime: 0, retry: false, networkMode: 'always', refetchOnWindowFocus: false, refetchOnReconnect: false } } });
  clients.push(query);
  const client = { clientId: 'phone', supports: () => true, host: { attention: read } } as unknown as WhipClient;
  let state = { client, host: { id: 'host', runtimeId: 'runtime' }, ready: true, active: true };
  const listeners = new Set<() => void>();
  const runtime = { query, getSnapshot: () => state, subscribe: (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; } } as unknown as MobileRuntime;
  const tree = () => <QueryClientProvider client={query}><AttentionProvider runtime={runtime}><Probe /><ScreenProbe /></AttentionProvider></QueryClientProvider>;
  return { query, runtime, client, read, tree,
    update(patch: Partial<typeof state>) { state = { ...state, ...patch }; listeners.forEach(listener => listener()); },
  };
}
async function flush() { await act(async () => { await jest.advanceTimersByTimeAsync(0); }); }
beforeEach(() => { mockFocused = false; jest.useFakeTimers(); });
afterEach(async () => { await cleanup(); clients.splice(0).forEach(query => query.clear()); jest.useRealTimers(); });

test('two consumers share one foreground poller even when Attention is unfocused, and backgrounding stops it', async () => {
  const f = fixture(); await render(f.tree()); await flush();
  expect(f.read).toHaveBeenCalledTimes(1);
  expect(f.query.getQueryCache().getAll()[0].getObserversCount()).toBe(1);
  await act(async () => { await jest.advanceTimersByTimeAsync(10_000); }); await flush();
  expect(f.read).toHaveBeenCalledTimes(2);
  await act(async () => { f.update({ active: false, ready: false }); await f.query.cancelQueries(); });
  await act(async () => { await jest.advanceTimersByTimeAsync(30_000); });
  expect(f.read).toHaveBeenCalledTimes(2); expect(attention.status).toBe('stale'); expect(attention.badge).toBe('?');
  await act(async () => { await f.query.invalidateQueries(); f.update({ active: true, ready: true }); }); await flush();
  expect(f.read).toHaveBeenCalledTimes(3);
  await act(async () => { await f.query.invalidateQueries(); }); await flush();
  expect(f.read).toHaveBeenCalledTimes(4); // Completed decisions refresh the same hidden-screen observer.
});

test('focus refreshes fresh data and repeated focus/manual refreshes join one in-flight request', async () => {
  const f = fixture(); const screen = await render(f.tree()); await flush();
  let finish!: (value: HostAttentionResult) => void;
  f.read.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  mockFocused = true; await screen.rerender(f.tree()); await flush();
  expect(f.read).toHaveBeenCalledTimes(2);
  const signal = f.read.mock.calls[1][1].signal;
  void attention.refresh(); void attention.refresh();
  mockFocused = false; await screen.rerender(f.tree());
  mockFocused = true; await screen.rerender(f.tree()); await flush();
  expect(f.read).toHaveBeenCalledTimes(2); expect(signal.aborted).toBe(false);
  await act(async () => { finish(page()); }); await flush();
  expect(attention.count).toBe(0); expect(attention.badge).toBeUndefined();
});

test('runtime cancellation aborts the read and old host callbacks/results cannot populate the new index', async () => {
  let finish!: (value: HostAttentionResult) => void;
  const read = jest.fn((_params: unknown, _options: { signal: AbortSignal }) => new Promise<HostAttentionResult>(resolve => { finish = resolve; }));
  const f = fixture(read); await render(f.tree()); await flush();
  const oldRefresh = attention.refresh;
  const signal = read.mock.calls[0][1].signal;
  await act(async () => { f.update({ active: false, ready: false }); await f.query.cancelQueries(); });
  expect(signal.aborted).toBe(true);
  await attention.refresh(); expect(read).toHaveBeenCalledTimes(1);
  const nextRead = jest.fn(async () => page([item('new-root', '2')]));
  const next = { clientId: 'new-phone', supports: () => true, host: { attention: nextRead } } as unknown as WhipClient;
  await act(async () => {
    f.query.clear(); f.update({ client: next, host: { id: 'new-host', runtimeId: 'new-runtime' }, active: true, ready: true });
    finish(page([item('old-root', '99')]));
  }); await flush();
  await oldRefresh();
  expect(read).toHaveBeenCalledTimes(1); expect(nextRead).toHaveBeenCalledTimes(1);
  expect(attention.items.map(value => value.root_id)).toEqual(['new-root']);
  expect(attention.runtimeId).toBe('new-runtime'); expect(attention.count).toBe(1);
});

test('pagination stops at four bounded pages and counts only loaded sessions with human requests', async () => {
  const read = jest.fn(async (params: unknown, _options: { signal: AbortSignal }) => {
    const { after_id } = params as { after_id?: string };
    const index = Number(after_id ?? 0);
    return page(Array.from({ length: 64 }, (_, i) => item(`${index}-${i}`, i < 2 ? '9007199254740993' : '0')), true, String(index + 1));
  });
  const f = fixture(read); await render(f.tree()); await flush();
  for (let i = 0; i < 3; i++) { await act(async () => { await attention.loadMore(); }); await flush(); }
  await attention.loadMore();
  expect(read).toHaveBeenCalledTimes(4); expect(attention.items).toHaveLength(256);
  expect(attention.count).toBe(8); expect(attention.badge).toBe('8+');
  expect(attention.atPageLimit).toBe(true); expect(attention.hasNextPage).toBe(false);
  expect(read.mock.calls.every(([params]) => (params as { limit: number; max_bytes: number }).limit === 64 && (params as { max_bytes: number }).max_bytes === 128 << 10)).toBe(true);
  expect(attention.accessibilityLabel).toContain('Partial index; more sessions may need you.');
});

test('failed refresh preserves qualified last-observed rows and an unsupported host is unavailable', async () => {
  const f = fixture(); await render(f.tree()); await flush();
  f.read.mockRejectedValueOnce(new Error('host read failed'));
  await act(async () => { await attention.refresh(); }); await flush();
  expect(attention.items).toHaveLength(1); expect(attention.status).toBe('stale'); expect(attention.badge).toBe('?');
  expect(attention.statusMessage).toContain('host read failed'); expect(attention.accessibilityLabel).toContain('Last observed count');
  const unsupported = { ...f.client, supports: () => false } as unknown as WhipClient;
  await act(async () => { f.query.clear(); f.update({ client: unsupported, host: { id: 'unsupported', runtimeId: 'unsupported' } }); }); await flush();
  expect(attention.status).toBe('unavailable'); expect(attention.statusMessage).toContain('does not support');
  await attention.refresh(); expect(f.read).toHaveBeenCalledTimes(2);
});

test('duplicate roots, limited question details, and unknown counts never produce a false empty badge', () => {
  const limited = { ...item('question-root', '0', [{ question: 'Continue?' }]), truncated: true };
  const summary = attentionSummary([page([item('active'), limited], true, 'next'), page([limited, item('permission-root', '2')])], 'current');
  expect(summary.items).toHaveLength(3); expect(summary.count).toBe(2); expect(summary.badge).toBe('2+');
  expect(attentionSummary([page([item('active')])], 'current').badge).toBeUndefined();
  expect(attentionSummary([page([], true)], 'current').badge).toBe('?');
  const unavailable = attentionSummary([], 'unavailable');
  expect(unavailable.badge).toBe('?'); expect(unavailable.accessibilityLabel).toBe('Attention. Index unavailable.');
});
