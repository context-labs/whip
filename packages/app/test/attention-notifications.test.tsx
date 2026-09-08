import { StrictMode } from 'react';
import { act, render } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { focusManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { UIProvider } from '@whip/ui';
import type { WhipClient, ConnectionSnapshot } from '@whip/sdk';
import type { HostAttentionResult } from '@whip/protocol';
import { Attention, DesktopAttention } from '../src/attention';
import { AttentionNotifications, scanAttention, type AttentionScan } from '../src/attention-notifications';
import { RuntimeContext } from '../src/context';
import type { AppRuntime } from '../src/runtime';

vi.mock('@tanstack/react-router', () => ({ Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a> }));
afterEach(() => { focusManager.setFocused(true); vi.useRealTimers(); });
function item(rootId = 'root', permissions = '0', ids: string[] = []): AttentionScan['items'][number] {
  return { rootId, title: 'Session title', permissions, questionCount: ids.length, questionIds: ids, completeQuestions: true };
}
function scan(items: AttentionScan['items'], complete = true): AttentionScan { return { items, complete }; }
function page(permissions = '0', ids: string[] = []): HostAttentionResult {
  return { items: [{ root_id: 'root', title: 'Session title', active_agents: '1', pending_permissions: permissions,
    questions: ids.map(question_id => ({ question_id, question: 'secret question body', options: [{ label: 'private answer' }] })), truncated: false }],
    has_more: false, truncated: false };
}

it('suppresses historical data and duplicates, coalesces requests per root, and emits no question content', () => {
  const tracker = new AttentionNotifications();
  expect(tracker.observe('runtime:one', scan([item('root', '1', ['old'])]))).toEqual([]);
  expect(tracker.observe('runtime:one', scan([item('root', '1', ['old'])]))).toEqual([]);
  const first = tracker.observe('runtime:one', scan([item('root', '2', ['old', 'new'])]));
  expect(first).toHaveLength(1); expect(first[0]).toEqual({
    id: expect.stringMatching(/^[a-zA-Z0-9-]{1,128}$/), title: 'Session title',
    body: '2 permission requests · 2 questions awaiting your response.', path: '/h/runtime%3Aone/s/root',
  });
  const second = tracker.observe('runtime:one', scan([item('root', '2', ['replacement', 'new'])]));
  expect(second).toHaveLength(1); expect(second[0]!.id).toBe(first[0]!.id);
  expect(second[0]!.body).not.toContain('replacement');
  expect(tracker.observe('runtime:one', scan([item('root', '2', ['new', 'replacement'])]))).toEqual([]);
});

it('does not announce completion from disappearance or idle counts and only clears requests after complete scans', () => {
  const tracker = new AttentionNotifications(); tracker.observe('runtime', scan([item('root', '1')]));
  expect(tracker.observe('runtime', scan([], false))).toEqual([]);
  expect(tracker.observe('runtime', scan([item('root', '1')]))).toEqual([]);
  expect(tracker.observe('runtime', scan([]))).toEqual([]);
  expect(tracker.observe('runtime', scan([item('root', '1')]))).toHaveLength(1);
  expect(tracker.observe('runtime', scan([item('root')]))).toEqual([]);
});

it('suppresses unseen historical roots after truncated scans and resets the baseline on host changes and reconnects', () => {
  const tracker = new AttentionNotifications(); tracker.observe('old', scan([item('root')], false));
  expect(tracker.observe('old', scan([item('unseen', '1')], false))).toEqual([]);
  expect(tracker.observe('new', scan([item('unseen', '5', ['historical'])]))).toEqual([]);
  tracker.reset(); expect(tracker.observe('new', scan([item('unseen', '6', ['historical', 'while-disconnected'])]))).toEqual([]);
  expect(tracker.observe('new', scan([item('newly-active', '1')]))).toHaveLength(1);
});

it('bounds scans to four pages and projects out question bodies, choices and other lifecycle payloads', async () => {
  const attention = vi.fn(async (params: { after_id?: string }) => ({ ...page('1', ['question']), has_more: true, next_after_id: `${params.after_id ?? ''}next` }));
  const client = { host: { attention } } as unknown as WhipClient;
  const queries = new QueryClient({ defaultOptions: { queries: { gcTime: 0, retry: false } } });
  const result = await scanAttention(client, queries, 'runtime', new AbortController().signal);
  expect(attention).toHaveBeenCalledTimes(4); expect(result.complete).toBe(false); expect(result.items).toHaveLength(4);
  expect(JSON.stringify(result)).not.toContain('secret question'); expect(JSON.stringify(result)).not.toContain('private answer');
  expect(result.items[0]?.questionIds).toEqual(['question']); queries.clear();
});

function observer(desktop = true) {
  const listeners = new Set<() => void>();
  let connection = { state: 'connected', info: { runtime_id: 'runtime', connection_id: 'first' } } as ConnectionSnapshot;
  let response = page();
  const hostAttention = vi.fn(async () => response);
  const client = { subscribe: (fn: () => void) => { listeners.add(fn); return () => { listeners.delete(fn); }; }, getSnapshot: () => connection,
    host: { attention: hostAttention } } as unknown as WhipClient;
  const notify = vi.fn(async () => {});
  const setNotificationsEnabled = vi.fn();
  const queries = new QueryClient({ defaultOptions: { queries: { gcTime: 0, retry: false, staleTime: 0, refetchOnWindowFocus: false } } });
  const state = { preferences: { attentionAnnouncements: false, desktopNotifications: true }, client };
  const runtime = { platform: { notify, setNotificationsEnabled }, queries, subscribe: () => () => {}, getSnapshot: () => state,
    tabs: { preferred: () => undefined }, report: vi.fn() } as unknown as AppRuntime;
  const mounted = render(<StrictMode><RuntimeContext.Provider value={runtime}><UIProvider><QueryClientProvider client={queries}>
    {desktop ? <DesktopAttention client={client} /> : <Attention client={client} />}
  </QueryClientProvider></UIProvider></RuntimeContext.Provider></StrictMode>);
  return { queries, notify, setNotificationsEnabled, hostAttention, listeners,
    response(next: HostAttentionResult) { response = next; },
    connection(state: ConnectionSnapshot['state'], id = 'first', host = 'runtime') {
      act(() => { connection = { state, info: { runtime_id: host, connection_id: id } } as ConnectionSnapshot; for (const listener of listeners) listener(); });
    },
    async tick(milliseconds = 3001) { await act(async () => { await vi.advanceTimersByTimeAsync(milliseconds); }); },
    close() { mounted.unmount(); queries.clear(); },
  };
}

it('continues opted-in desktop observation while hidden and drops reconnect bursts without additional root subscriptions', async () => {
  vi.useFakeTimers(); focusManager.setFocused(false);
  const f = observer(); await f.tick(1);
  expect(f.notify).not.toHaveBeenCalled(); expect(f.hostAttention).toHaveBeenCalledOnce();
  expect(f.setNotificationsEnabled).toHaveBeenLastCalledWith(true);
  f.response(page('1', ['new-question'])); await f.tick();
  expect(f.notify).toHaveBeenCalledOnce();
  await f.tick(); expect(f.notify).toHaveBeenCalledOnce();
  f.connection('reconnecting'); f.response(page('5', ['new-question', 'during-disconnection'])); await f.tick();
  const before = f.hostAttention.mock.calls.length;
  f.connection('connected', 'second'); await f.tick(1);
  expect(f.hostAttention.mock.calls.length).toBeGreaterThan(before); expect(f.notify).toHaveBeenCalledOnce();
  f.response(page('6', ['new-question', 'during-disconnection'])); await f.tick();
  expect(f.notify).toHaveBeenCalledTimes(2);
  f.connection('connected', 'third', 'other-runtime'); await f.tick(1); expect(f.notify).toHaveBeenCalledTimes(2);
  f.close(); expect(f.listeners.size).toBe(0); expect(f.setNotificationsEnabled).toHaveBeenLastCalledWith(false);
});

it('retains ordinary browser polling policy while hidden and never sends native notifications', async () => {
  vi.useFakeTimers(); focusManager.setFocused(false);
  const f = observer(false); await f.tick(1);
  const initial = f.hostAttention.mock.calls.length;
  f.response(page('1', ['new'])); await f.tick(6001);
  expect(f.hostAttention).toHaveBeenCalledTimes(initial); expect(f.notify).not.toHaveBeenCalled();
  expect(f.setNotificationsEnabled).not.toHaveBeenCalled(); f.close();
});

it('establishes a fresh silent baseline after a failed attention refresh', async () => {
  vi.useFakeTimers(); const f = observer(); await f.tick(1);
  f.hostAttention.mockRejectedValueOnce(new Error('Connection interrupted'));
  f.response(page('5', ['during-outage'])); await f.tick(); await f.tick();
  expect(f.notify).not.toHaveBeenCalled();
  f.response(page('6', ['during-outage'])); await f.tick(); expect(f.notify).toHaveBeenCalledOnce(); f.close();
});
