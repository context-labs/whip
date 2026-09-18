import { act, fireEvent, render, screen } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import { ActivityIndicator, ThemeProvider, UIProvider } from '@whip/ui';
import type { ExecutionCell, SessionViewSnapshot } from '@whip/sdk/state';
import { conversationActivityRows, isActivityGroup, responseCopies, type ActivityGroup } from '../src/chat-activity-rows';
import { activityStatus, TranscriptWorking } from '../src/chat-activity';
import { timelineRows, type TimelineRow } from '../src/conversation-rows';
import { readingTarget } from '../src/reading-positions';
import { ActivityHeader, ActivityDetail, InlineAgent } from '../src/transcript-activity';
import { ExecutionTime, formatHostDuration } from '../src/execution-time';
import { MotionContext } from '../src/transcript-motion';

const cell = (id: string, seq?: number, status: ExecutionCell['status'] = 'completed'): ExecutionCell => ({
  kind: 'cell', id, callId: id, agentId: 'root', seq, status, code: `print('${id}')`, output: '', hosts: [],
});
const tool = (id: string, seq?: number): TimelineRow => ({ id: `tool:${id}`, seq, role: 'tool', callId: id, toolName: 'rlm_exec', text: '', label: 'Starlark execution' });
const group = (items: ExecutionCell[]): ActivityGroup => ({ id: 'group', role: 'activity', text: '', cells: items, memberIds: items.map(item => item.id), memberSeqs: [] });
const state = (overrides: Record<string, unknown> = {}): SessionViewSnapshot => ({
  status: 'live', root: { root_id: 'root', active_turns: { root: 'turn' }, agents: [], permissions: [], questions: [], presentation: [], agent_presentations: {}, ...overrides },
  history: {}, collections: {}, retainedBytes: 0, unavailable: false, truncated: false,
} as unknown as SessionViewSnapshot);

it('rounds host durations to whole units except sub-millisecond measurements', () => {
  for (const [raw, display] of [
    ['2.93134ms', '3ms'], ['37.094122ms', '37ms'], ['48.464938ms', '48ms'], ['8.87777ms', '9ms'],
    ['1ms', '1ms'], ['0.125ms', '0.13ms'], ['125µs', '0.13ms'], ['400ns', '0.00ms'],
    ['0.999ms', '1ms'], ['999.9ms', '1s'], ['1.8s', '2s'], ['1m59.9s', '2m0s'],
    ['2h3m4.567s', '2h3m5s'], ['', ''], ['unavailable', 'unavailable'],
  ]) expect(formatHostDuration(raw!)).toBe(display);
});

it('does not call a failed session snapshot a host connection failure', () => {
  const unavailable = { ...state(), root: undefined, error: new Error('Snapshot unavailable') };
  expect(activityStatus(unavailable, 'root', [], true)).toMatchObject({ text: '', active: false });
  expect(activityStatus({ ...unavailable, error: undefined }, 'root', [], true).text).toBe('Loading session…');
});

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

it('turn boundaries, gaps and restarts split groups while failures stay in their activity', () => {
  const cells = [cell('a', 1), { ...cell('b', 2, 'failed'), turnId: 'one' }, { ...cell('c', 3), turnId: 'one' }, { ...cell('d', 4), turnId: 'two' }, cell('e', 5)];
  const output = conversationActivityRows(cells.map(item => tool(item.id, item.seq)), [...cells, { kind: 'restart', id: 'restart', agentId: 'root', seq: 4, text: 'Restarted' }]);
  expect(output.filter(isActivityGroup).map(row => row.cells.length)).toEqual([1, 2, 1, 1]);
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
  const view = render(<ThemeProvider><UIProvider><ActivityHeader group={activity} open connected density="detailed" toggle={vi.fn()} /></UIProvider></ThemeProvider>);
  expect(screen.getByText('Agent updates')).toBeDefined();
  expect(view.container.querySelector('[data-agent-update] pre')).toBeNull();
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

it('does not append old unplaced executions after newer responses or move their copy footer', () => {
  const rows = [
    tool('recorded', 1),
    { id: 'old-reply', role: 'assistant', text: 'Earlier response.' },
    { id: 'input', role: 'user', text: 'Follow-up' },
    { id: 'reply', role: 'assistant', text: 'Current response.' },
  ];
  const executions = [
    { ...cell('recorded', 1), turnId: 'old' },
    { ...cell('unmatched'), turnId: 'old', historyUnmatched: true },
    { ...cell('unrecorded'), turnId: 'old' },
    { ...cell('stale-running', undefined, 'running'), turnId: 'old' },
    cell('unknown-turn'),
  ];
  for (const activeTurn of [undefined, 'new']) {
    const output = conversationActivityRows(rows, executions, [], activeTurn);
    expect(output.map(row => isActivityGroup(row) ? row.cells.map(cell => cell.id) : row.id)).toEqual([
      ['recorded'], 'old-reply', 'input', 'reply',
    ]);
    expect([...responseCopies(output, false).keys()]).toEqual(['old-reply', 'reply']);
  }
  expect(executions).toHaveLength(5); // Evidence stays available to the REPL.
});

it('shows unplaced work only for the active turn and keeps its identity when history arrives', () => {
  const execution = { ...cell('missing-prefix', undefined, 'running'), turnId: 'turn' };
  const pending = conversationActivityRows([], [execution], [], 'turn').filter(isActivityGroup);
  expect(pending).toHaveLength(1);
  expect(pending[0]!.cells).toEqual([execution]);
  // A settled operation can still belong to a turn that is continuing to work.
  const completed = { ...execution, status: 'completed' as const };
  expect(conversationActivityRows([], [completed], pending, 'turn')[0]!.id).toBe(pending[0]!.id);
  expect(conversationActivityRows([], [completed], pending)).toEqual([]);
  expect(conversationActivityRows([], [execution], pending, 'next-turn')).toEqual([]);
  const restored = conversationActivityRows([tool('missing-prefix', 4)], [{ ...completed, seq: 4 }], pending);
  expect(restored[0]!.id).toBe(pending[0]!.id);
});

it('merged groups preserve bookmarks for both prior identities', () => {
  const cells = [cell('a', 1), cell('b', 2)];
  const previous = conversationActivityRows([tool('a', 1), { id: 'boundary', role: 'notice', text: 'Old boundary' }, tool('b', 2)], cells).filter(isActivityGroup);
  const merged = conversationActivityRows(cells.map(cell => tool(cell.id, cell.seq)), cells, previous);
  const bookmark = { messageId: previous[1]!.id, revision: '1', offset: 12, follow: false };
  expect(readingTarget(merged, '1', bookmark)).toEqual({ index: 0, offset: 12, fallback: false });
});

it('inline agent cards use the typed child identity without hydrating a transcript', () => {
  const onAgent = vi.fn();
  const row = { ...tool('spawn'), role: 'agent-activity' as const, cell: cell('spawn'), agentHost: { id: 'host', name: 'agents.spawn', status: 'completed' as const, summary: '', duration: '', display: { child_id: 'child', label: 'Reviewer' } } };
  const view = render(<ThemeProvider><UIProvider><InlineAgent row={row} connected active={false} onAgent={onAgent} readBody={vi.fn()} /></UIProvider></ThemeProvider>);
  fireEvent.click(screen.getByRole('button', { name: /Reviewer/ }));
  expect(onAgent).toHaveBeenCalledWith('child');
  expect(view.container.querySelector('[data-inline-agent]')).toBeTruthy();
  expect(screen.getByText('Status unavailable')).toBeTruthy();
});

it('explicit execution details keep language and output and do not fetch stored bodies', () => {
  const readBody = vi.fn(), onOpenRepl = vi.fn();
  const execution = { ...cell('js'), language: 'javascript', code: 'await files.read({path: "README.md"})', output: 'hello' };
  render(<ThemeProvider><UIProvider><ActivityDetail item={{ id: 'js', kind: 'execution', cell: execution }} groupId="group" readBody={readBody} onOpenRepl={onOpenRepl} /></UIProvider></ThemeProvider>);
  expect(screen.getByText('JavaScript')).toBeDefined();
  expect(screen.getByText('hello')).toBeDefined();
  expect(readBody).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: 'Open in REPL' }));
  expect(onOpenRepl).toHaveBeenCalledOnce();
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

it('bridges sending into authoritative work without masking delivery uncertainty or queued input', () => {
  const idle = state({ active_turns: {} });
  const failed = { last_turn: { status: 'failed' } } as Parameters<typeof activityStatus>[4];
  expect(activityStatus(idle, 'root', [], true, failed, 'Sending…')).toMatchObject({ text: 'Sending…', active: true, pending: true });
  for (const delivery of ['Queued', 'Checking delivery…', 'Not received · retry from the composer']) {
    expect(activityStatus(idle, 'root', [], true, undefined, delivery)).toMatchObject({ active: false, pending: true });
  }
  expect(activityStatus(state(), 'root', [], true, undefined, 'Queued')).toMatchObject({ text: 'Working', active: true });
  expect(activityStatus(idle, 'root', [], false, undefined, 'Sending…')).toMatchObject({ text: 'Reconnecting · activity updates paused', active: false, pending: true });
  expect(activityStatus(state({ active_turns: {}, questions: [{ question_id: 'q' }] }), 'root', [], true, undefined, 'Sending…')).toMatchObject({ text: 'Waiting for your answer', active: false, pending: true });
});

it('renders immediately, rotates only during work, and stops timers on hidden, waiting and settled states', () => {
  vi.useFakeTimers();
  vi.setSystemTime(10_000);
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  const app = (status: Parameters<typeof TranscriptWorking>[0]['status'], turnId?: string, motion = true) =>
    <MotionContext.Provider value={motion}><TranscriptWorking key={turnId ?? 'pending'} status={status} turnId={turnId} /></MotionContext.Provider>;
  const view = render(app({ text: 'Sending…', active: true, pending: true }));
  const indicator = () => view.container.querySelector('[data-activity-animation]')!;
  expect(screen.getByText('Sending…')).toBeTruthy();
  expect(indicator().children).toHaveLength(9);
  expect(indicator().getAttribute('data-activity-animation')).toBe('running');
  expect(view.container.querySelector('[data-turn-elapsed]')).toBeNull();
  expect(vi.getTimerCount()).toBe(0);
  view.rerender(app({ text: 'Working', active: true }, 'first'));
  expect(screen.getByText('Thinking…')).toBeTruthy();
  expect(screen.getByText('0s')).toBeTruthy();
  act(() => vi.advanceTimersByTime(7000));
  expect(screen.getByText('Pondering…')).toBeTruthy();
  expect(screen.getByText('7s')).toBeTruthy();
  expect(view.container.querySelector('[role="status"], [aria-live]')).toBeNull();
  view.rerender(app({ text: 'Working', active: true }, 'first', false));
  expect(screen.getByText('Thinking…')).toBeTruthy();
  expect(indicator().getAttribute('data-activity-animation')).toBe('static');
  hidden.mockReturnValue(true);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  expect(vi.getTimerCount()).toBe(0);
  hidden.mockReturnValue(false);
  act(() => document.dispatchEvent(new Event('visibilitychange')));
  view.rerender(app({ text: 'Waiting for your approval', active: false, attention: true }, 'first'));
  expect(screen.getByText('Waiting for your approval')).toBeTruthy();
  expect(vi.getTimerCount()).toBe(0);
  view.rerender(app({ text: 'Working', active: true }, 'second'));
  expect(screen.getByText('0s')).toBeTruthy();
  view.rerender(app({ text: 'Last turn failed', active: false, attention: true }));
  expect(view.container.textContent).toBe('');
  expect(vi.getTimerCount()).toBe(0);
  view.unmount(); hidden.mockRestore(); vi.useRealTimers();
});

it('missing raw records split activity, prevent cross-gap call binding, and mark available copies incomplete', () => {
  const history = { revision: '1', throughSeq: 6, nextSeq: 1, hasMore: false, loading: false, truncated: false,
    gaps: [{ fromSeq: 3, toSeq: 4, status: 'paused' as const }], messages: [
      { seq: 1, message: { role: 'assistant', content: 'Before the missing input.' } },
      { seq: 2, message: { role: 'assistant', content: '', tool_calls: [{ id: 'reused', function: { name: 'rlm_exec', arguments: '{}' } }] } },
      { seq: 5, message: { role: 'tool', tool_call_id: 'reused', content: 'Unproven result' } },
      { seq: 6, message: { role: 'assistant', content: 'After the missing input.' } },
    ] };
  const rows = timelineRows(history, [], true);
  expect(rows.find(row => row.seq === 2)?.text).toBe('');
  expect(rows.map(row => row.seq)).toEqual([1, 2, 3, 5, 6]);
  expect(rows.find(row => row.historyGap)?.id).toBe('history-gap:1:4');
  const projected = conversationActivityRows(rows, [{ ...cell('first', 2), callId: 'reused' }]);
  const copies = [...responseCopies(projected, false).values()];
  expect(copies).toEqual([
    { label: 'Copy visible response', text: 'Before the missing input.' },
    { label: 'Copy visible response', text: 'After the missing input.' },
  ]);
  history.gaps[0]!.fromSeq = 4;
  expect(timelineRows(history, [], true).find(row => row.historyGap)?.id).toBe('history-gap:1:4');
});
