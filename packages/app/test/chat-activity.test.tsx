import { act, fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ActivityIndicator, ThemeProvider, UIProvider } from '@whip/ui';
import type { ExecutionCell, SessionViewSnapshot } from '@whip/sdk/state';
import { conversationActivityRows, isActivityGroup, responseCopies, type ActivityGroup } from '../src/chat-activity-rows';
import { activityStatus, ActivityGroupRow, ChatActivity } from '../src/chat-activity';
import { timelineRows, type TimelineRow } from '../src/conversation-rows';
import { readingTarget } from '../src/reading-positions';
import { ExecutionTime } from '../src/execution-time';

const cell = (id: string, seq?: number, status: ExecutionCell['status'] = 'completed'): ExecutionCell => ({
  kind: 'cell', id, callId: id, agentId: 'root', seq, status, code: `print('${id}')`, output: '', hosts: [],
});
const tool = (id: string, seq?: number): TimelineRow => ({ id: `tool:${id}`, seq, role: 'tool', callId: id, toolName: 'rlm_exec', text: '', label: 'Starlark execution' });
const group = (items: ExecutionCell[]): ActivityGroup => ({ id: 'group', role: 'activity', text: '', cells: items, memberIds: items.map(item => item.id), memberSeqs: [] });
const state = (overrides: Record<string, unknown> = {}): SessionViewSnapshot => ({
  status: 'live', root: { root_id: 'root', active_turns: { root: 'turn' }, agents: [], permissions: [], questions: [], presentation: [], agent_presentations: {}, ...overrides },
  history: {}, collections: {}, retainedBytes: 0, unavailable: false, truncated: false,
} as unknown as SessionViewSnapshot);

it('ignores anonymous reference-only tool updates and empty prose, and updates arriving tool names', () => {
  const rows = timelineRows(undefined, [
    { seq: '1', kind: 'stream.tool.call', payload: { truncated: true, content: { reference_id: 'body' } } },
    { seq: '2', kind: 'stream.text', payload: { text: '' } },
    { seq: '3', kind: 'stream.tool.call', payload: { id: 'call', args: '{' } },
    { seq: '4', kind: 'stream.tool.call', payload: { id: 'call', name: 'files.list', args: '{}' } },
    { seq: '5', kind: 'stream.tool.completed', payload: { id: 'call', result: 'Error: unknown tool "files.list"' } },
  ]);
  expect(rows).toHaveLength(1);
  expect(rows[0]).toMatchObject({ label: 'files.list', toolName: 'files.list', live: false, text: 'Error: unknown tool "files.list"' });
});

it('copies completed responses once, excludes tool/mailbox bodies, and leaves active responses without a footer', () => {
  const rows: TimelineRow[] = [
    { id: 'u1', role: 'user', text: 'First' },
    { id: 'a1', role: 'assistant', text: 'Starting.' }, tool('call'),
    { id: 'mail', role: 'mailbox', text: 'Internal delivery' },
    { id: 'a2', role: 'assistant', text: 'Finished.' },
    { id: 'u2', role: 'user', text: 'Next' },
    { id: 'a3', role: 'assistant', text: 'Working…', live: true },
    { id: 'queued', role: 'user', text: 'Later', queued: true },
  ];
  expect([...responseCopies(rows, true)]).toEqual([['a2', { label: 'Copy response', text: 'Starting.\n\nFinished.' }]]);
  expect([...responseCopies(rows, false).keys()]).toEqual(['a2', 'a3']);
  expect(responseCopies(rows.slice(1, 5), false, true).get('a2')?.label).toBe('Copy visible response');
});

it('groups typed contiguous cells while preserving authored prose and notices exactly', () => {
  const authored = { id: 'prose', role: 'assistant', text: 'Keep **every** authored byte.\n' };
  const notice = { id: 'notice', role: 'notice', text: 'Waiting for approval' };
  const cells = [cell('a', 1), cell('b', 2), cell('c', 4), cell('d', 6)];
  const output = conversationActivityRows([tool('a', 1), tool('b', 2), authored, tool('c', 4), notice, tool('d', 6)], cells);
  expect(output.map(row => isActivityGroup(row) ? row.cells.map(item => item.id) : row.id)).toEqual([['a', 'b'], 'prose', ['c'], 'notice', ['d']]);
  expect(output[1]).toBe(authored);
  expect(output[3]).toBe(notice);
  expect(conversationActivityRows([{ ...tool('a', 1), toolName: 'another-tool' }], [cells[0]!])[0]!.role).toBe('tool');
});

it('turns, failures, gaps and restarts split groups rather than claiming one continuous operation', () => {
  const cells = [cell('a', 1), { ...cell('b', 2, 'failed'), turnId: 'one' }, { ...cell('c', 3), turnId: 'one' }, { ...cell('d', 4), turnId: 'two' }, cell('e', 5)];
  const output = conversationActivityRows(cells.map(item => tool(item.id, item.seq)), [...cells, { kind: 'restart', id: 'restart', agentId: 'root', seq: 4, text: 'Restarted' }]);
  expect(output.filter(isActivityGroup).map(row => row.cells.length)).toEqual([1, 1, 1, 1, 1]);
});

it('folds internal deliveries into following work without absorbing prose or earlier executions', () => {
  const update: TimelineRow = { id: 'digest', seq: 2, role: 'mailbox', text: 'Mailbox digest: a report and a completion notice', deliveries: 2 };
  const prose: TimelineRow = { id: 'reply', seq: 4, role: 'assistant', text: 'The report is ready.' };
  const rows = conversationActivityRows([tool('a', 1), update, tool('b', 3), prose, { ...update, id: 'late', seq: 5 }], [cell('a', 1), cell('b', 3)]);
  expect(rows).toHaveLength(4);
  expect(rows[0]).toMatchObject({ cells: [{ id: 'a' }], updates: [] });
  expect(rows[1]).toMatchObject({ cells: [{ id: 'b' }], updates: [update], memberIds: expect.arrayContaining(['digest']) });
  expect(rows[2]).toBe(prose);
  expect(rows[3]).toMatchObject({ cells: [], updates: [{ id: 'late' }] });
  expect(readingTarget(rows, '1', { messageId: 'digest', seq: 2, revision: '1', follow: false, offset: 4 })).toEqual({ index: 1, offset: 4, fallback: false });
  const pending = conversationActivityRows([update], []).filter(isActivityGroup);
  expect(conversationActivityRows([update, tool('b', 3)], [cell('b', 3)], pending)[0]!.id).toBe(pending[0]!.id);
});

it('agent updates retain their raw contents and never imply an execution completed', () => {
  const readBody = vi.fn();
  const update: TimelineRow = { id: 'digest', role: 'mailbox', text: 'Mailbox digest: keep the full report.' };
  const activity = conversationActivityRows([update], []).filter(isActivityGroup)[0]!;
  const view = render(<ThemeProvider><UIProvider><ActivityGroupRow group={activity} open connected density="detailed" onToggle={vi.fn()} readBody={readBody} /></UIProvider></ThemeProvider>);
  expect(screen.getByText('Agent updates')).toBeDefined();
  expect(screen.queryByText('Completed work')).toBeNull();
  expect(view.container.querySelector('[data-agent-updates]')!.hasAttribute('open')).toBe(false);
  expect(view.container.querySelector('[data-agent-update] pre')!.textContent).toBe(update.text);
  expect(readBody).not.toHaveBeenCalled();
  const many = conversationActivityRows(Array.from({ length: 14 }, (_, index) => ({ ...update, id: String(index) })), []).filter(isActivityGroup);
  expect(many.map(group => group.updates!.length)).toEqual([6, 6, 2]);
});

it('keeps group identity through append, prepend and live-to-history reconciliation', () => {
  const first = conversationActivityRows([tool('b', 2)], [cell('b', 2)]).filter(isActivityGroup);
  const appended = conversationActivityRows([tool('b', 2), tool('c')], [cell('b', 2), { ...cell('c', undefined, 'running'), eventSeq: '10' }], first).filter(isActivityGroup);
  expect(appended[0]!.id).toBe(first[0]!.id);
  const recorded = conversationActivityRows([tool('a', 1), tool('b', 2), tool('c', 3)], [cell('a', 1), cell('b', 2), cell('c', 3)], appended).filter(isActivityGroup);
  expect(recorded).toHaveLength(1);
  expect(recorded[0]!.id).toBe(first[0]!.id);
  expect(readingTarget(recorded, '1', { messageId: 'tool:b', revision: '1', follow: false, offset: 8 })).toEqual({ index: 0, offset: 8, fallback: false });
  expect(readingTarget(recorded, '1', { messageId: 'old-tool', seq: 3, revision: '1', follow: false, offset: 8 }).fallback).toBe(false);
});

it('status uses real operation evidence, human requests override it, and stale state stops activity', () => {
  const running = { ...cell('a', undefined, 'running'), turnId: 'turn', hosts: [{ id: 'host', name: 'files.read', summary: 'path=README.md', duration: '', status: 'running' as const }] };
  expect(activityStatus(state(), 'root', [running], true).text).toBe('Reading files');
  expect(activityStatus(state({ permissions: [{ status: 'pending' }] }), 'root', [running], true)).toMatchObject({ text: 'Waiting for your approval', active: false });
  expect(activityStatus(state(), 'root', [running], false)).toMatchObject({ text: 'Reconnecting · activity updates paused', active: false });
  expect(activityStatus(state({ active_turns: {}, omitted: { agents: true }, agents: [{ id: 'child', status: 'running' }] }), 'root', [], true).text).toBe('At least 1 agent still working');
  expect(activityStatus(state(), 'root', [{ ...running, turnId: 'old' }], true).text).toBe('Working');
});

it('uses retained event identity after a prefix is dropped instead of joining by a reused call ID', () => {
  const old = { ...cell('old'), callId: 'reused', turnId: 'turn', eventSeq: '1', presentationSeqs: ['1', '2'] };
  const current = { ...cell('current', undefined, 'running'), callId: 'reused', turnId: 'turn', eventSeq: '10', presentationSeqs: ['10', '12'] };
  const row = { ...tool('reused'), turnId: 'turn', eventSeq: '12' };
  const result = conversationActivityRows([row], [old, current]);
  expect(isActivityGroup(result[0]!) && result[0].cells[0]!.id).toBe('current');
  expect(result.filter(isActivityGroup).flatMap(group => group.cells).filter(cell => cell.id === 'current')).toHaveLength(1);
});

it('merged groups preserve bookmarks for both prior identities', () => {
  const cells = [cell('a', 1), cell('b', 2)];
  const previous = conversationActivityRows([tool('a', 1), { id: 'boundary', role: 'notice', text: 'Old boundary' }, tool('b', 2)], cells).filter(isActivityGroup);
  const merged = conversationActivityRows(cells.map(cell => tool(cell.id, cell.seq)), cells, previous);
  const bookmark = { messageId: previous[1]!.id, revision: '1', offset: 12, follow: false };
  expect(readingTarget(merged, '1', bookmark)).toEqual({ index: 0, offset: 12, fallback: false });
});

it('appending executions preserves a focused cell in the bounded expanded window', () => {
  const items = Array.from({ length: 6 }, (_, index) => cell(String(index)));
  const props = { open: true, connected: true, density: 'detailed' as const, onToggle: vi.fn(), readBody: vi.fn() };
  const app = (items: ExecutionCell[]) => <ThemeProvider><UIProvider><ActivityGroupRow group={group(items)} {...props} /></UIProvider></ThemeProvider>;
  const view = render(app(items));
  const first = view.container.querySelector<HTMLElement>('[data-activity-cell="0"]')!;
  first.tabIndex = 0;
  first.focus();
  view.rerender(app([...items, cell('6')]));
  expect(document.activeElement).toBe(first);
  expect(view.container.querySelectorAll('[data-activity-cell]')).toHaveLength(6);
});

it('agent rows show names, stay bounded, and open an agent without hydrating its transcript', () => {
  const onAgent = vi.fn(), onAllAgents = vi.fn();
  const snapshot = state({ agents: Array.from({ length: 8 }, (_, index) => ({ id: `child-${index}`, parent_id: 'root', name: `Review ${index}`, model: 'model', status: 'running' })) });
  render(<ThemeProvider><UIProvider><ChatActivity state={snapshot} cells={[]} agentId="root" connected onAgent={onAgent} onAllAgents={onAllAgents} /></UIProvider></ThemeProvider>);
  expect(screen.getAllByRole('button', { name: /^Review / })).toHaveLength(3);
  fireEvent.click(screen.getByRole('button', { name: /^Review 0/ }));
  expect(onAgent).toHaveBeenCalledWith('child-0');
  fireEvent.click(screen.getByRole('button', { name: 'View all session agents' }));
  expect(onAllAgents).toHaveBeenCalledOnce();
});

it('expanded groups bound code and DOM work and never fetch stored bodies automatically', () => {
  const readBody = vi.fn(), onOpenRepl = vi.fn();
  render(<ThemeProvider><UIProvider><ActivityGroupRow group={group(Array.from({ length: 100 }, (_, index) => ({ ...cell(String(index)), code: 'x'.repeat(20_000) })))}
    open connected density="detailed" onToggle={vi.fn()} readBody={readBody} onOpenRepl={onOpenRepl} /></UIProvider></ThemeProvider>);
  expect(screen.getByText(/Showing 6 of 100/)).toBeDefined();
  expect(document.querySelectorAll('pre')).toHaveLength(6);
  expect(document.body.textContent!.length).toBeLessThan(30_000);
  expect(readBody).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Open in REPL' }));
  expect(onOpenRepl).toHaveBeenCalledOnce();
});

it('named agents keep focus and admission order as other agents need attention', () => {
  const children = Array.from({ length: 4 }, (_, index) => ({ id: `child-${index}`, parent_id: 'root', name: `Agent ${index}`, status: 'running' }));
  const app = (agents: unknown[]) => <ThemeProvider><UIProvider><ChatActivity state={state({ agents })} cells={[]} agentId="root" connected onAgent={vi.fn()} onAllAgents={vi.fn()} /></UIProvider></ThemeProvider>;
  const view = render(app(children));
  const focused = screen.getByRole('button', { name: /^Agent 2/ });
  focused.focus();
  view.rerender(app([{ ...children[3], blocking_reason: 'permission' }, ...children.slice(0, 3).reverse()]));
  expect(document.activeElement).toBe(focused);
  expect(screen.getAllByRole('button', { name: /^Agent / }).map(item => item.getAttribute('data-activity-agent'))).toEqual(['child-0', 'child-1', 'child-2']);
});

it('observed time pauses its timer while hidden and never invents a timer for replay', () => {
  vi.useFakeTimers();
  vi.setSystemTime(10_000);
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  const running = { ...cell('active', undefined, 'running'), observedStartedAt: 8000 };
  const view = render(<ExecutionTime cell={running} connected />);
  expect(screen.getByText('Observed 2.0s')).toBeDefined();
  hidden.mockReturnValue(true);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(vi.getTimerCount()).toBe(0);
  act(() => vi.advanceTimersByTime(5000));
  hidden.mockReturnValue(false);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(screen.getByText('Observed 7.0s')).toBeDefined();
  view.rerender(<ExecutionTime cell={cell('replayed', undefined, 'running')} connected />);
  expect(view.container.textContent).toBe('');
  expect(vi.getTimerCount()).toBe(0);
  view.unmount(); hidden.mockRestore(); vi.useRealTimers();
});

it('motion starts after a short delay and stops when hidden or reduced', () => {
  vi.useFakeTimers();
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  const { container, rerender, unmount } = render(<ActivityIndicator active />);
  const status = () => container.querySelector('[data-activity-animation]')!.getAttribute('data-activity-animation');
  expect(status()).toBe('static');
  act(() => vi.advanceTimersByTime(1100));
  expect(status()).toBe('running');
  hidden.mockReturnValue(true);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(status()).toBe('static');
  expect(vi.getTimerCount()).toBe(0);
  rerender(<ActivityIndicator active reduceMotion />);
  expect(status()).toBe('static');
  unmount();
  expect(vi.getTimerCount()).toBe(0);
  hidden.mockRestore();
  vi.useRealTimers();
});
