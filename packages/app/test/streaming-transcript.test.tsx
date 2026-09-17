import { expect, it } from 'vitest';
import { fireEvent, render } from '@testing-library/react';
import type { ExecutionCell, HistoryView } from '@whip/sdk/state';
import { timelineRows } from '../src/conversation-rows';
import { activityItems, activitySummary, conversationActivityRows, isActivityGroup, responseCopies } from '../src/chat-activity-rows';
import { MarkdownBlock, markdownRows, streamingSource, type MarkdownRow } from '../src/streaming-markdown';
import { fadeDuration, MotionContext, springStep } from '../src/transcript-motion';
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

it('streaming display completion leaves the source and ambiguous fenced content untouched', () => {
  expect(streamingSource('**Hello')).toBe('**Hello**');
  expect(streamingSource('A `value')).toBe('A `value`');
  expect(streamingSource('```js\nconst x = "**')).toBe('```js\nconst x = "**');
  expect(streamingSource('Finished.')).toBe('Finished.');
});

it('uses adaptive fade bounds and a convergent spring without overshoot', () => {
  expect(fadeDuration(160, 10)).toEqual({ average: 115, duration: 345 });
  expect(fadeDuration(10, 1).duration).toBe(120);
  expect(fadeDuration(160, 5000).duration).toBe(400);
  let state = { position: 0, velocity: 0 };
  for (let frame = 0; frame < 400; frame++) {
    state = springStep(state.position, state.velocity, 1000, 1);
    expect(state.position).toBeLessThanOrEqual(1000);
  }
  expect(state.position).toBeCloseTo(1000, 3);
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
