import { expect, it, vi } from 'vitest';
import { act, fireEvent, render, renderHook } from '@testing-library/react';
import type { CellExecutionRow } from '@whip/sdk/state';
import type { HostOperation } from '@whip/protocol';
import { assertValid } from '@whip/protocol';
import { created, history, message, preview } from './native-presentation-fixtures';
import { timelineRows } from '../src/conversation-rows';
import { activityItems, activitySummary, conversationActivityRows, isActivityGroup, responseCopies } from '../src/chat-activity-rows';
import { MarkdownBlock, markdownRows, streamingSource, useCoalescedTranscript, type MarkdownRow } from '../src/streaming-markdown';
import { fadeDuration, MotionContext } from '../src/transcript-motion';
import { readingTarget } from '../src/reading-positions';

function operation(id: string, capability: string, state: HostOperation['state'] = 'succeeded', resource = 'workspace', value: unknown = {}): HostOperation {
  const settled = state === 'succeeded' || state === 'failed';
  const result: HostOperation = { id, session_id: 'root', turn_id: 'turn', cell_id: 'cell', request_id: id, origin: 'cell', capability, resource,
    arguments: {}, state, permission_revision: null, grant_id: null, result: settled ? { state, value, content_references: [] } : null,
    created_at: created, dispatched_at: created, finished_at: settled ? created : null };
  assertValid('HostOperation', result); return result;
}
function execution(operations: HostOperation[] = [], failed = false): CellExecutionRow {
  const call = { id: 'call', name: 'execute', arguments: { code: '42' } };
  const result = { call_id: 'call', output: 'exception', is_error: true };
  const cell: CellExecutionRow = { output: null, cell: { id: 'cell', session_id: 'root', turn_id: 'turn', call_message_id: 'call-message', call_id: 'call',
    state: failed ? 'failed' : 'running', result_message_id: failed ? 'result-message' : null, checkpoint: null, created_at: created, finished_at: failed ? created : null },
    turn: null, call: { message: message({ id: 'call-message', parts: [{ type: 'tool_call', call }] }), value: call },
    result: failed ? { message: message({ id: 'result-message', role: 'tool', sequence: '2', parts: [{ type: 'tool_result', result }] }), value: result } : null, operations };
  assertValid('Cell', cell.cell); return cell;
}
const toolRow = { id: 'tool', role: 'tool', text: '', seq: '1', toolName: 'execute', callId: 'call', turnId: 'turn' };

it('projects replacement reasoning, Unicode canonical prose and exact tool identity', () => {
  const call = { id: 'call', name: 'execute', arguments: { code: '42' } };
  const retained = history(message({ id: 'answer', parts: [{ type: 'text', text: 'Hi 🌍' }, { type: 'tool_call', call }] }),
    message({ id: 'result', sequence: '2', role: 'tool', parts: [{ type: 'tool_result', result: { call_id: 'call', output: 'result', is_error: false } }] }));
  expect(timelineRows(retained).map(row => [row.id, row.role, row.text])).toEqual([
    ['message:answer:part:0', 'assistant', 'Hi 🌍'], ['message:answer:call:call', 'tool', 'result'],
  ]);
  // Reasoning belongs to the full replacement preview; it is not fabricated
  // as durable text when the canonical message is committed.
  const live = timelineRows(undefined, preview({ message_id: 'answer', reasoning: 'Inspect first', text: 'Hi 🌍' }));
  expect(live.map(row => row.role)).toEqual(['reasoning', 'assistant']);
  expect(live[1]!.id).toBe(timelineRows(retained)[0]!.id);
  expect(timelineRows(retained, preview({ message_id: 'answer', reasoning: 'stale', text: 'partial' }))).toHaveLength(2);
});

it('groups reasoning and operations, counts exact edit targets once, and separates spawned agents', () => {
  const cell = execution([operation('read', 'files.read'), { ...operation('edit1', 'files.patch'), arguments: { path: 'a.ts' } },
    { ...operation('edit2', 'files.write', 'failed'), arguments: { path: 'a.ts' } }, operation('spawn', 'agents.spawn', 'succeeded', 'tree', { session_id: 'child' }),
    operation('run', 'shell.run', 'dispatched')]);
  const rows = conversationActivityRows([{ id: 'reason', role: 'reasoning', text: 'Think', turnId: 'turn', live: true }, { ...toolRow, live: true }], [cell]);
  expect(rows.map(row => row.role)).toEqual(['activity', 'agent-activity', 'activity']);
  const groups = rows.filter(isActivityGroup);
  expect(activityItems(groups[0]!).map(item => item.id)).toEqual(['reason', 'read', 'edit1', 'edit2']);
  expect(activitySummary(groups[0]!)).toBe('Thought · edited 1 file · read 1 file · 1 failed');
  expect(groups[0]!.autoOpen).toBeUndefined();
  expect(groups[1]!.autoOpen).toBe(true);
});

it('keeps group identity when a generic execution reveals its first real operation', () => {
  const first = conversationActivityRows([toolRow], [execution()]).filter(isActivityGroup);
  const next = conversationActivityRows([toolRow], [execution([operation('host', 'files.read', 'dispatched')])], first);
  expect(next[0]!.id).toBe(first[0]!.id);
});

it('exposes execution failure after successful operations without double-counting the execution', () => {
  const group = conversationActivityRows([toolRow], [execution([operation('read', 'files.read')], true)])[0];
  expect(group && isActivityGroup(group) && activitySummary(group)).toBe('Read 1 file · 1 failed');
  expect(group && isActivityGroup(group) && activityItems(group).at(-1)?.cell?.result?.value.output).toBe('exception');
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

it('gives canonical parts and explicit omitted messages distinct Markdown and virtual identities', () => {
  const retained = history(message({ id: 'before', parts: [{ type: 'text', text: '1. First item' }, { type: 'text', text: '2. Second item' }] }));
  retained.gaps = [{ messageID: 'omitted', sequence: '2', reason: 'message_too_large', bytes: 9000 }];
  const rows = timelineRows(retained, preview({ text: '3. Third item' }));
  expect(rows.map(row => row.id)).toEqual(['message:before:part:0', 'message:before:part:1', 'message:omitted', 'message:live:part:0']);
  expect(rows[1]!.memberIds).toContain('message:before');
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

it('copies canonical prose once, excluding reasoning and tool output', () => {
  const retained = history(message({ parts: [{ type: 'text', text: 'Hello' }, { type: 'text', text: 'world' },
    { type: 'tool_call', call: { id: 'call', name: 'execute', arguments: {} } }] }),
    message({ id: 'result', sequence: '2', role: 'tool', parts: [{ type: 'tool_result', result: { call_id: 'call', output: 'private output', is_error: false } }] }));
  const rows = timelineRows(retained, preview({ reasoning: 'private thought', text: '' }));
  expect([...responseCopies(rows, false).values()]).toEqual([{ text: 'Hello\n\nworld', label: 'Copy response' }]);
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

it('keeps a partial replacement response after its last canonical user or tool record', () => {
  const retained = history(message({ role: 'user', input_id: 'input', parts: [{ type: 'text', text: 'Request' }] }));
  expect(timelineRows(retained, preview({ reasoning: 'Thought', text: 'Partial response' })).map(row => row.text)).toEqual(['Request', 'Thought', 'Partial response']);
});
