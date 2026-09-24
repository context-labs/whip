import { act, cleanup, render } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { HostAttentionResult } from '@whip/protocol';
import { ReadLane } from '../runtime/read-lane';
import { WorkspaceAttentionProvider, useWorkspaceAttention } from './workspace-index';
let mockWorkspace: any; let mockState: any;
jest.mock('../runtime/workspace-context', () => ({ useWorkspace: () => mockWorkspace, useWorkspaceState: () => mockState }));
let attention: ReturnType<typeof useWorkspaceAttention>;
function Probe() { attention = useWorkspaceAttention(); return null; }
const page = (id = 'same-root', more = false): HostAttentionResult => ({ items: [{ root_id: id, title: id, active_agents: '1', pending_permissions: '9007199254740993', questions: [], truncated: false }], has_more: more, next_after_id: more ? id : undefined, truncated: false });
const queries: QueryClient[] = [];
function fixture() {
  const query = new QueryClient({ defaultOptions: { queries: { retry: false, networkMode: 'always' } } }); queries.push(query);
  const readA = jest.fn(async (_params: unknown, _options: { signal: AbortSignal }) => page());
  const readB = jest.fn(async (_params: unknown, _options: { signal: AbortSignal }) => page());
  const connection = (id: string, read: typeof readA) => { const host = { id, name: id, runtimeId: `runtime-${id}` }; const client = { supports: () => true, host: { attention: read }, onCommand: jest.fn(() => () => {}) }; return { getSnapshot: () => ({ host, client, ready: true, active: mockState.active }) }; };
  mockState = { active: true, connections: [connection('a', readA), connection('b', readB)], hosts: [{ id: 'a' }, { id: 'b' }] };
  mockWorkspace = { reads: new ReadLane(), connectionKey: () => 1 };
  const tree = () => <QueryClientProvider client={query}><WorkspaceAttentionProvider><Probe /><Probe /></WorkspaceAttentionProvider></QueryClientProvider>;
  return { query, readA, readB, connection, tree };
}
async function flush() { await act(async () => { await jest.advanceTimersByTimeAsync(0); }); }
beforeEach(() => jest.useFakeTimers());
afterEach(async () => { await cleanup(); queries.splice(0).forEach(q => q.clear()); jest.useRealTimers(); });
test('two consumers share one poller and root IDs remain qualified by host', async () => {
  const f = fixture(); const screen = await render(f.tree()); await flush();
  expect(f.readA).toHaveBeenCalledTimes(1); expect(f.readB).toHaveBeenCalledTimes(1);
  expect(f.query.getQueryCache().getAll()[0].getObserversCount()).toBe(1);
  expect(attention.items.map(i => i.host.id)).toEqual(['a', 'b']); expect(attention.count).toBe(2);
  await act(async () => { await jest.advanceTimersByTimeAsync(10_000); }); await flush(); expect(f.readA).toHaveBeenCalledTimes(2);
  mockState = { ...mockState, active: false }; await screen.rerender(f.tree()); await f.query.cancelQueries();
  await act(async () => { await jest.advanceTimersByTimeAsync(30_000); });
  expect(f.readA).toHaveBeenCalledTimes(2); expect(attention.badge).toBe('2+');
});
test('one failed host preserves the healthy host and qualifies the count', async () => {
  const f = fixture(); f.readB.mockRejectedValue(new Error('Disconnected')); await render(f.tree()); await flush();
  expect(attention.items).toHaveLength(1); expect(attention.badge).toBe('1+'); expect(attention.pages[1].error).toBe('Disconnected');
});
test('next replaces a host page and old query windows are collected', async () => {
  const f = fixture(); f.readA.mockImplementation(async params => page((params as any).after_id ? 'second' : 'first', true));
  await render(f.tree()); await flush();
  await act(async () => { attention.next('a', 'first'); }); await flush();
  expect(attention.items.map(i => i.root_id)).toEqual(['second', 'same-root']); expect(attention.count).toBe(2); expect(attention.badge).toBe('2+');
  expect(f.readA.mock.calls.at(-1)?.[0]).toEqual({ after_id: 'first', limit: 64, max_bytes: 128 << 10 });
  expect(f.query.getQueryCache().getAll()).toHaveLength(1);
});
test('replacement aborts the old read and its late response cannot enter the new host index', async () => {
  const f = fixture(); let finish!: (value: HostAttentionResult) => void;
  f.readA.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  const screen = await render(f.tree()); await flush(); const signal = f.readA.mock.calls[0][1].signal;
  const read = jest.fn(async (_params: unknown, _options: { signal: AbortSignal }) => page('new-root'));
  mockState = { ...mockState, connections: [f.connection('new', read)], hosts: [{ id: 'new' }] };
  await screen.rerender(f.tree()); await flush(); expect(signal.aborted).toBe(true);
  await act(async () => { finish(page('old-root')); }); await flush();
  expect(attention.items.map(i => i.root_id)).toEqual(['new-root']); expect(attention.items[0].host.runtimeId).toBe('runtime-new');
});
