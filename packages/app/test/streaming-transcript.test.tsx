import { expect, it, vi } from 'vitest';
import { act, fireEvent, render, renderHook } from '@testing-library/react';
import type { ExecutionCell, HistoryView } from '@whip/sdk/state';
import { timelineRows } from '../src/conversation-rows';
import { activityItems, activitySummary, conversationActivityRows, isActivityGroup, responseCopies } from '../src/chat-activity-rows';
import { MarkdownBlock, markdownRows, streamingSource, useCoalescedTranscript, type MarkdownRow } from '../src/streaming-markdown';
import { fadeDuration, MotionContext } from '../src/transcript-motion';
import { readingTarget } from '../src/reading-positions';

it('restores ordered reasoning, Unicode prose and tool identity without changing the mobile projection', () => {
  const history = { revision: '1', messages: [{ seq: 1, message: { role: 'assistant', content: 'Hi 🌍', tool_calls: [{ id: 'call', type: 'function', function: { name: 'rlm_exec', arguments: '{"code":"42"}' } }],
    presentation: { version: 1, turn_id: 'turn', parts: [{ id: 'r', kind: 'reasoning', text: 'Inspect first' }, { id: 't', kind: 'text', start: 0, end: 7 }, { id: 'c', kind: 'tool', call_id: 'call', tool_name: 'rlm_exec' }] },
  } }, { seq: 2, message: { role: 'tool', tool_call_id: 'call', content: 'result' } }] } as HistoryView;
  const rich = timelineRows(history, undefined, true);
  expect(rich.map(row => [row.id, row.role, row.text])).toEqual([['part:r', 'reasoning', 'Inspect first'], ['part:t', 'assistant', 'Hi 🌍'], ['part:c', 'tool', 'result']]);
  expect(timelineRows(history, undefined).map(row => row.role)).toEqual(['assistant', 'tool']);
  expect(timelineRows(undefined, [{ seq: '1', kind: 'stream.reasoning', payload: { text: 'Inspect first', part_id: 'r', turn_id: 'turn' } }], true)[0]!.id).toBe(rich[0]!.id);
});

it('groups reasoning and operations, counts exact edit targets once, and separates spawned agents', () => {
  const cell: ExecutionCell = { id: 'cell', partId: 'c', kind: 'cell', agentId: 'root', callId: 'call', turnId: 'turn', code: '42', output: '', status: 'running', hosts: [
    { id: 'read', name: 'files.read', summary: '', duration: '', status: 'completed' },
    { id: 'edit1', name: 'files.patch', summary: '', duration: '', status: 'completed', display: { target: 'a.ts' } },
    { id: 'edit2', name: 'files.write', summary: '', duration: '', status: 'failed', display: { target: 'a.ts' } },
    { id: 'spawn', name: 'agents.spawn', summary: '', duration: '', status: 'completed', display: { child_id: 'child' } },
    { id: 'run', name: 'shell.run', summary: '', duration: '', status: 'running' },
  ] };
  const rows = conversationActivityRows([{ id: 'reason', role: 'reasoning', text: 'Think', turnId: 'turn', live: true }, { id: 'tool', role: 'tool', text: '', partId: 'c', toolName: 'rlm_exec', callId: 'call', turnId: 'turn', live: true }], [cell]);
  expect(rows.map(row => row.role)).toEqual(['activity', 'agent-activity', 'activity']);
  const groups = rows.filter(isActivityGroup);
  expect(activityItems(groups[0]!).map(item => item.id)).toEqual(['reason', 'read', 'edit1', 'edit2']);
  expect(activitySummary(groups[0]!)).toBe('Thought · edited 1 file · read 1 file · 1 failed');
  expect(groups[0]!.autoOpen).toBeUndefined();
  expect(groups[1]!.autoOpen).toBe(true);
});

it('keeps group identity when a generic execution reveals its first real operation', () => {
  const cell: ExecutionCell = { id: 'c', kind: 'cell', agentId: 'root', callId: 'call', code: '', output: '', hosts: [], status: 'running' };
  const row = { id: 'tool', role: 'tool', toolName: 'rlm_exec', callId: 'call', text: '' };
  const first = conversationActivityRows([row], [cell]).filter(isActivityGroup);
  const next = conversationActivityRows([row], [{ ...cell, hosts: [{ id: 'host', name: 'files.read', summary: '', duration: '', status: 'running' }] }], first);
  expect(next[0]!.id).toBe(first[0]!.id);
});

it('exposes execution failure after successful operations without double-counting the execution', () => {
  const cell: ExecutionCell = { id: 'c', kind: 'cell', agentId: 'root', callId: 'call', code: 'fail()', output: '', status: 'failed', error: 'exception', hosts: [{ id: 'read', name: 'files.read', summary: '', duration: '', status: 'completed' }] };
  const group = conversationActivityRows([{ id: 'tool', role: 'tool', toolName: 'rlm_exec', callId: 'call', text: '' }], [cell])[0];
  expect(group && isActivityGroup(group) && activitySummary(group)).toBe('Read 1 file · 1 failed');
  expect(group && isActivityGroup(group) && activityItems(group).at(-1)?.cell?.error).toBe('exception');
});

it('reuses unchanged Markdown blocks and preserves parent reading aliases through settlement', () => {
  const cache = new Map();
  const row = { id: 'part:p', role: 'assistant', text: '# Heading\n\nHello', live: true };
  const first = markdownRows([row], cache);
  const second = markdownRows([{ ...row, text: row.text + ' world' }], cache);
  expect(second[0]!.id).toBe(first[0]!.id);
  expect('block' in second[0]! && second[0].block).toBe('block' in first[0]! && first[0].block);
  expect(second[1]!.memberIds).toContain('part:p');
  expect(markdownRows([{ ...row, text: row.text + ' world', live: false }], cache).map(row => row.id)).toEqual(second.map(row => row.id));
});

it('gives retained fragments of one part distinct Markdown and virtual row identities', () => {
  const payload = { part_id: 'p', turn_id: 'turn' };
  const rows = timelineRows(undefined, [
    { seq: '10', kind: 'stream.text', payload: { ...payload, text: '1. First item' } },
    // Missing deltas must stay separate; the UI cannot reconstruct the gap.
    { seq: '15', kind: 'stream.text', payload: { ...payload, text: '2. Second item' } },
    { seq: '16', kind: 'stream.notice', payload: { text: 'Notice' } },
    { seq: '17', kind: 'stream.text', payload: { ...payload, text: '3. Third item' } },
  ], true);
  expect(rows.map(row => row.id)).toEqual(['part:p', 'part:p:fragment:15', 'live:16', 'part:p:fragment:17']);
  expect(rows[1]!.memberIds).toContain('part:p');
  const cache = new Map();
  const blocks = markdownRows(rows, cache);
  expect(new Set(blocks.map(row => row.id)).size).toBe(blocks.length);
  expect(cache.size).toBe(3);
  expect(rows.filter(row => row.role === 'assistant').map(row => row.text)).toEqual(['1. First item', '2. Second item', '3. Third item']);
});

it('streaming display completion leaves the source and ambiguous fenced content untouched', () => {
  expect(streamingSource('**Hello')).toBe('**Hello**');
  expect(streamingSource('A `value')).toBe('A `value`');
  expect(streamingSource('```js\nconst x = "**')).toBe('```js\nconst x = "**');
  expect(streamingSource('Finished.')).toBe('Finished.');
  expect(streamingSource('* list')).toBe('* list');
  expect(streamingSource('a_b')).toBe('a_b');
});

it('coalesces live changes to 30Hz while publishing settlement immediately', () => {
  vi.useFakeTimers();
  try {
    const hook = renderHook(({ text, live }) => useCoalescedTranscript([{ id: 'a', role: 'assistant', text, live }]), { initialProps: { text: 'first', live: true } });
    for (let i = 0; i < 100; i++) hook.rerender({ text: `delta-${i}`, live: true });
    expect(hook.result.current[0]!.text).toBe('first');
    act(() => vi.advanceTimersByTime(35));
    expect(hook.result.current[0]!.text).toBe('delta-99');
    hook.rerender({ text: 'finished', live: false });
    expect(hook.result.current[0]!.text).toBe('finished');
    hook.unmount();
  } finally { vi.useRealTimers(); }
});

it('uses adaptive fade bounds', () => {
  expect(fadeDuration(160, 10)).toEqual({ average: 115, duration: 345 });
  expect(fadeDuration(10, 1).duration).toBe(120);
  expect(fadeDuration(160, 5000).duration).toBe(400);

});

it('copies original prose before presentation splits, excluding thought and output', () => {
  const rows = timelineRows({ revision: '1', messages: [{ seq: 1, message: { role: 'assistant', content: 'Hello world', presentation: { version: 1, parts: [
    { id: 'a', kind: 'text', start: 0, end: 6 }, { id: 'b', kind: 'reasoning', text: 'private thought' }, { id: 'c', kind: 'text', start: 6, end: 11 },
  ] } } }] } as HistoryView, undefined, true);
  expect([...responseCopies(rows, false).values()]).toEqual([{ text: 'Hello world', label: 'Copy response' }]);
});

it('restores a visible operation before its enclosing group alias', () => {
  expect(readingTarget([{ id: 'group', memberIds: ['operation'] }, { id: 'operation' }], '1', { messageId: 'operation', revision: '1', offset: 12, follow: false })).toEqual({ index: 1, offset: 12, fallback: false });
});

it('keeps a selected Markdown block intact through append and settlement, then catches up', () => {
  const cache = new Map();
  const row = (text: string, live = true) => markdownRows([{ id: 'text', role: 'assistant', text, live }], cache)[0] as MarkdownRow;
  const tree = (text: string, live = true) => <MotionContext.Provider value={true}><MarkdownBlock row={row(text, live)} components={{}} /></MotionContext.Provider>;
  const view = render(tree('Hello 🌍'));
  view.rerender(tree('Hello 🌍 **world**'));
  const source = view.container.querySelector('p')!;
  const range = document.createRange(); range.selectNodeContents(source);
  document.getSelection()!.removeAllRanges(); document.getSelection()!.addRange(range);
  fireEvent(document, new Event('selectionchange'));
  view.rerender(tree('Hello 🌍 **world** and everyone.', false));
  expect(document.getSelection()!.toString()).toBe('Hello 🌍 world');
  expect(view.container.textContent).toBe('Hello 🌍 world');
  document.getSelection()!.removeAllRanges(); fireEvent(document, new Event('selectionchange'));
  expect(view.container.textContent).toBe('Hello 🌍 world and everyone.');
  expect(view.container.querySelector('[data-stream-chunk]')).toBeNull();
});

it('keeps orphaned reasoning and prose after their last user or tool record', () => {
  const history = { revision: '1', messages: [{ seq: 1, message: { role: 'user', content: 'Request', presentation: { version: 1, parts: [{ id: 'r', kind: 'reasoning', text: 'Thought' }, { id: 'p', kind: 'text', text: 'Partial response' }] } } }] } as HistoryView;
  expect(timelineRows(history, undefined, true).map(row => row.text)).toEqual(['Request', 'Thought', 'Partial response']);
});
