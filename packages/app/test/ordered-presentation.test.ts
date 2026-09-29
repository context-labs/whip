import { expect, it } from 'vitest';
import type { AttemptPresentation, MessagePresentation } from '@whip/protocol';
import { history, message, preview } from './native-presentation-fixtures';
import { timelineRows } from '../src/conversation-rows';
import { conversationActivityRows, responseCopies } from '../src/chat-activity-rows';

const key = (slot: string, owner = 'root', attempt = 'attempt') => JSON.stringify([owner, attempt, slot]);
const call = { id: 'call', name: 'execute', arguments: { code: 'print("🌍")' } };
const presentation: MessagePresentation = { version: 1, attempt_id: 'attempt', truncated: false, parts: [
  { id: 'p0', type: 'reasoning', text: 'First thought' },
  { id: 'p1', type: 'text', start: 0, end: 7 },
  { id: 'p2', type: 'tool_call', call_index: 0, call_id: 'call' },
  { id: 'p3', type: 'reasoning', text: 'Second thought' },
  { id: 'p4', type: 'text', start: 7, end: 12 },
] };
const answer = () => message({ id: 'answer', presentation, parts: [
  { type: 'text', text: 'Hi 🌍 done' }, { type: 'tool_call', call },
] });

it('keeps ordered Unicode prose, reasoning and call identities through streamed ID and settlement', () => {
  const source = history();
  const writing = (id: string) => timelineRows(source, preview({ message_id: 'answer', text: 'Hi 🌍 done',
    reasoning: 'First thoughtSecond thought', presentation: { ...presentation, parts: presentation.parts.map(part =>
      part.type === 'tool_call' ? { ...part, call_id: id || undefined } : part) },
    calls: [{ index: 0, id, name: 'execute', arguments: '{"code":"print(\\"🌍' }] }));
  const first = writing(''), second = writing('call');
  const completed = timelineRows(history(answer()));
  expect(first.map(row => row.id)).toEqual(['p0', 'p1', 'p2', 'p3', 'p4'].map(slot => key(slot)));
  expect(second.map(row => row.id)).toEqual(first.map(row => row.id));
  expect(completed.map(row => row.id)).toEqual(first.map(row => row.id));
  expect(completed.map(row => [row.role, row.text])).toEqual([
    ['reasoning', 'First thought'], ['assistant', 'Hi 🌍'], ['tool', ''], ['reasoning', 'Second thought'], ['assistant', ' done'],
  ]);
  expect(completed.some(row => row.live)).toBe(false);
  expect([...responseCopies(completed, false).values()][0]?.text).toBe('Hi 🌍 done');
});

it('attaches canonical results to ordered calls and scopes imported display identities to their owner', () => {
  const result = message({ id: 'result', sequence: '8', role: 'tool', parts: [{ type: 'tool_result', result: { call_id: 'call', output: 'done', is_error: false } }] });
  const rows = timelineRows(history(answer(), result));
  expect(rows.find(row => row.id === key('p2'))).toMatchObject({ text: 'done', memberSeqs: ['1', '8'] });
  const copied = timelineRows(history({ ...answer(), session_id: 'fork', turn_id: null, group_id: 'imported' }));
  expect(copied[0]?.id).toBe(key('p0', 'fork'));
  expect(copied.every(row => row.turnId === undefined)).toBe(true);
});

it('never drops or repeats canonical content omitted by bounded presentation', () => {
  const source = answer();
  const rows = timelineRows(history({ ...source, presentation: { ...presentation, truncated: true,
    parts: [{ id: 'p0', type: 'reasoning', text: 'Retained' }, { id: 'p1', type: 'text', start: 0, end: 7 }] } }));
  expect(rows.filter(row => row.role === 'assistant').map(row => row.text).join('')).toBe('Hi 🌍 done');
  expect(rows.filter(row => row.role === 'tool')).toHaveLength(1);
  expect([...responseCopies(rows, false).values()][0]?.text).toBe('Hi 🌍 done');
  const overlapping = timelineRows(history({ ...source, presentation: { ...presentation, parts: [
    { id: 'one', type: 'text', start: 0, end: 7 }, { id: 'two', type: 'text', start: 0, end: 12 },
  ] } }));
  expect(overlapping.filter(row => row.role === 'assistant').map(row => row.text).join('')).toBe('Hi 🌍 done');
});

it('retains failed-attempt evidence before its eventual response without copying it as canonical prose', () => {
  const attempts: AttemptPresentation[] = [{ group_id: 'group', attempt_id: 'failed-attempt', turn_id: 'turn', message_id: 'answer', state: 'failed',
    presentation: { version: 1, attempt_id: 'failed-attempt', truncated: false, parts: [
      { id: 'p0', type: 'reasoning', text: 'Partial reasoning' }, { id: 'p1', type: 'text', text: 'Partial answer' },
      { id: 'p2', type: 'tool_call', call_index: 0, call: { index: 0, id: '', name: 'execute', arguments: '{"code":"partial' } },
    ] } }];
  const settled = { ...answer(), group_id: 'group' };
  const rows = timelineRows(history(settled), undefined, attempts);
  expect(rows[0]?.label).toBe('Failed attempt');
  const unfinished = rows.find(row => row.id === key('p2', 'root', 'failed-attempt'));
  expect(unfinished?.attemptState).toBe('failed');
  expect(unfinished?.seq).toBeUndefined();
  const grouped = conversationActivityRows(rows, []);
  expect([...responseCopies(grouped, false).values()][0]).toMatchObject({ text: 'Hi 🌍 done', sequence: '1' });
  expect(timelineRows(history(settled), undefined, attempts).map(row => row.id)).toEqual(rows.map(row => row.id));
});
