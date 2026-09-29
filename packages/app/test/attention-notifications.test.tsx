import { StrictMode, useSyncExternalStore } from 'react';
import { act, render } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { focusManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { UIProvider } from '@whip/ui';
import { Client } from '@whip/sdk';
import type { HostAttentionResult, HostAttentionParams } from '@whip/protocol';
import { Attention, DesktopAttention } from '../src/attention';
import { AttentionNotifications, scanAttention, type AttentionScan } from '../src/attention-notifications';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';
import type { HostConnection } from '../src/hosts';

vi.mock('@tanstack/react-router', () => ({ Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a> }));
afterEach(() => { focusManager.setFocused(true); vi.useRealTimers(); });
function item(rootId = 'root', permissions = '0', questions = '0', sessionId = rootId): AttentionScan['items'][number] {
  return { rootId, sessionId, title: 'Session title', permissions, questionCount: questions };
}
function scan(items: AttentionScan['items'], complete = true): AttentionScan { return { items, complete }; }
function page(permissions = '0', questions = '0', sessionId = 'root'): HostAttentionResult {
  return { items: [{ tree_id: 'tree', root_id: 'root', session_id: sessionId, title: 'Session title', activity: {
    session_id: sessionId, lifecycle: 'active', active_turn: null, active_input_id: null, queued_input_count: '0',
    pending_permission_count: permissions, pending_question_count: questions, execution_permit: false, active_workspace_action_id: null,
  } }], next_cursor: null };
}
async function nativeClient(read: (params: HostAttentionParams, signal?: AbortSignal) => unknown | Promise<unknown>, runtimeID = 'runtime', epoch = 'first') {
  return Client.connect(async (request, _identity, options) => ({ jsonrpc: '2.0', id: request.id, result: request.method === 'initialize'
    ? { major: 4, minor: 0, runtime_id: runtimeID, process_epoch: epoch, network_client: false, builtins: [] }
    : request.method === 'host.attention' ? await read(request.params as HostAttentionParams, options.signal)
    : (() => { throw Error(`Unexpected ${request.method}`); })() }), { clientID: 'attention-fixture' });
}

it('suppresses historical counts and duplicates and emits only advisory request counts', () => {
  const tracker = new AttentionNotifications();
  expect(tracker.observe('runtime:one', scan([item('root', '1', '1')]))).toEqual([]);
  expect(tracker.observe('runtime:one', scan([item('root', '1', '1')]))).toEqual([]);
  const first = tracker.observe('runtime:one', scan([item('root', '2', '2')]));
  expect(first).toHaveLength(1); expect(first[0]).toEqual({
    id: expect.stringMatching(/^[a-zA-Z0-9-]{1,128}$/), title: 'Session title',
    body: '2 permission requests · 2 questions awaiting your response.', path: '/h/runtime%3Aone/s/root',
  });
  expect(tracker.observe('runtime:one', scan([item('root', '2', '2')]))).toEqual([]);
  const second = tracker.observe('runtime:one', scan([item('root', '2', '3')]));
  expect(second).toHaveLength(1); expect(second[0]!.id).toBe(first[0]!.id);
});

it('keeps sibling attention independent and routes notifications to the exact child with large counters intact', () => {
  const tracker = new AttentionNotifications();
  tracker.observe('runtime', scan([item('root', '0', '0', 'child:a'), item('root', '0', '0', 'child:b')]));
  const messages = tracker.observe('runtime', scan([item('root', '0', '9007199254740993', 'child:a'), item('root', '1', '0', 'child:b')]));
  expect(messages).toHaveLength(2);
  expect(messages[0]!.body).toContain('9007199254740993 questions');
  expect(messages.map(message => message.path)).toEqual(['/h/runtime/s/root?agent=child%3Aa', '/h/runtime/s/root?agent=child%3Ab']);
});

it('does not announce completion from disappearance or idle counts and only clears requests after complete scans', () => {
  const tracker = new AttentionNotifications(); tracker.observe('runtime', scan([item('root', '1')]));
  expect(tracker.observe('runtime', scan([], false))).toEqual([]);
  expect(tracker.observe('runtime', scan([item('root', '1')]))).toEqual([]);
  expect(tracker.observe('runtime', scan([]))).toEqual([]);
  expect(tracker.observe('runtime', scan([item('root', '1')]))).toHaveLength(1);
  expect(tracker.observe('runtime', scan([item('root')]))).toEqual([]);
});

it('suppresses unseen historical sessions after incomplete scans and resets the baseline on host changes and reconnects', () => {
  const tracker = new AttentionNotifications(); tracker.observe('old', scan([item('root')], false));
  expect(tracker.observe('old', scan([item('unseen', '1')], false))).toEqual([]);
  expect(tracker.observe('new', scan([item('unseen', '5', '1')]))).toEqual([]);
  tracker.reset(); expect(tracker.observe('new', scan([item('unseen', '6', '2')]))).toEqual([]);
  expect(tracker.observe('new', scan([item('newly-active', '1')]))).toHaveLength(1);
});

it('bounds native scans to four pages and retains no execution payload', async () => {
  const attention = vi.fn(async (params: HostAttentionParams) => ({ ...page('1', '1'), next_cursor: { tree_id: `${params.after?.tree_id ?? ''}next`, session_id: 'root' } }));
  const client = await nativeClient(attention);
  const queries = new QueryClient({ defaultOptions: { queries: { gcTime: 0, retry: false } } });
  const result = await scanAttention(client, queries, 'runtime', new AbortController().signal);
  expect(attention).toHaveBeenCalledTimes(4); expect(result.complete).toBe(false); expect(result.items).toHaveLength(4);
  expect(JSON.stringify(result)).not.toContain('active_turn');
  expect(result.items[0]?.questionCount).toEqual('1'); queries.clear();
});

async function observer(desktop = true) {
  const listeners = new Set<() => void>();
  let response = page();
  const hostAttention = vi.fn(async () => response);
  let client = await nativeClient(hostAttention);
  const notify = vi.fn(async () => {}), setNotificationsEnabled = vi.fn();
  const queries = new QueryClient({ defaultOptions: { queries: { gcTime: 0, retry: false, staleTime: 0, refetchOnWindowFocus: false } } });
  let state = { preferences: { attentionAnnouncements: false, desktopNotifications: true }, hosts: [{ id: 'local', name: 'This Mac', client, runtimeId: 'runtime', state: 'connected' } as HostConnection] };
  const subscribe = (fn: () => void) => { listeners.add(fn); return () => { listeners.delete(fn); }; };
  const runtime = { platform: { notify, setNotificationsEnabled }, queries, subscribe, getSnapshot: () => state,
    connections: { isAttached: (candidate: Client) => candidate === client && state.hosts[0]!.state === 'connected' },
    tabs: { preferred: () => undefined, subscribe: () => () => {}, getSnapshot: () => null }, report: vi.fn() } as unknown as AppRuntime;
  function Observer() {
    const current = useSyncExternalStore(subscribe, () => state);
    return desktop ? <DesktopAttention host={current.hosts[0]!} /> : <Attention />;
  }
  const mounted = render(<StrictMode><RuntimeContext.Provider value={runtime}><UIProvider><QueryClientProvider client={queries}><Observer /></QueryClientProvider></UIProvider></RuntimeContext.Provider></StrictMode>);
  return { queries, notify, setNotificationsEnabled, hostAttention, listeners,
    response(next: HostAttentionResult) { response = next; },
    async connection(status: HostConnection['state'], epoch = 'first', runtimeID = 'runtime') {
      if (status === 'connected') client = await nativeClient(hostAttention, runtimeID, epoch);
      act(() => { state = { ...state, hosts: [{ ...state.hosts[0]!, state: status, client, runtimeId: runtimeID }] }; for (const listener of listeners) listener(); });
    },
    async tick(milliseconds = 3001) { await act(async () => { await vi.advanceTimersByTimeAsync(milliseconds); }); },
    close() { mounted.unmount(); queries.clear(); },
  };
}

it('continues opted-in desktop observation while hidden and drops reconnect bursts without root subscriptions', async () => {
  vi.useFakeTimers(); focusManager.setFocused(false);
  const f = await observer(); await f.tick(1);
  expect(f.notify).not.toHaveBeenCalled(); expect(f.hostAttention).toHaveBeenCalledOnce();
  f.response(page('1', '1')); await f.tick(); expect(f.notify).toHaveBeenCalledOnce();
  await f.tick(); expect(f.notify).toHaveBeenCalledOnce();
  await f.connection('closed'); f.response(page('5', '2')); await f.tick();
  const before = f.hostAttention.mock.calls.length;
  await f.connection('connected', 'second'); await f.tick(1);
  expect(f.hostAttention.mock.calls.length).toBeGreaterThan(before); expect(f.notify).toHaveBeenCalledOnce();
  f.response(page('6', '2')); await f.tick(); expect(f.notify).toHaveBeenCalledTimes(2);
  await f.connection('connected', 'third', 'other-runtime'); await f.tick(1); expect(f.notify).toHaveBeenCalledTimes(2);
  f.close(); expect(f.listeners.size).toBe(0);
});

it('retains ordinary browser polling policy while hidden and never sends native notifications', async () => {
  vi.useFakeTimers(); focusManager.setFocused(false);
  const f = await observer(false); await f.tick(1);
  const initial = f.hostAttention.mock.calls.length;
  f.response(page('1', '1')); await f.tick(6001);
  expect(f.hostAttention).toHaveBeenCalledTimes(initial); expect(f.notify).not.toHaveBeenCalled();
  expect(f.setNotificationsEnabled).not.toHaveBeenCalled(); f.close();
});

it('establishes a fresh silent baseline after a failed attention refresh', async () => {
  vi.useFakeTimers(); const f = await observer(); await f.tick(1);
  f.hostAttention.mockRejectedValueOnce(new Error('Connection interrupted'));
  f.response(page('5', '1')); await f.tick(); await f.tick(); expect(f.notify).not.toHaveBeenCalled();
  f.response(page('6', '1')); await f.tick(); expect(f.notify).toHaveBeenCalledOnce(); f.close();
});
