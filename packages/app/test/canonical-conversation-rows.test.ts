import { expect, it } from 'vitest';
import type { Message } from '@whip/protocol';
import type { HistoryView } from '@whip/sdk/state';
import { conversationRows, timelineRows } from '../src/conversation-rows';

const base = { session_id: 'child', group_id: 'turn', opening_input: false, turn_id: 'turn', input_id: null, input_identity: null, mail: null, source: null, retired_by: null, retired_revision: null, created_at: '2026-01-01T00:00:00Z' };
const history = (messages: Message[], gaps: HistoryView['gaps'] = []): HistoryView => ({ snapshot: null, messages, gaps, olderCursor: null, latestMissing: false });
const call = (id: string, group: string, sequence: string): Message => ({ ...base, id, group_id: group, sequence, role: 'assistant', parts: [{ type: 'text', text: 'Before' }, { type: 'tool_call', call: { id: 'shared', name: 'rlm_exec', arguments: { code: 'print(1)' } } }, { type: 'text', text: 'After' }] });
const result = (id: string, group: string, sequence: string): Message => ({ ...base, id, group_id: group, sequence, role: 'tool', parts: [{ type: 'tool_result', result: { call_id: 'shared', output: group, is_error: false } }, { type: 'content', reference_id: id }] });
it('keeps exact sequence and ordered parts, matching tool results only inside their recorded exchange', () => {
  const rows = timelineRows(history([call('one', 'g1', '9007199254740992'), call('two', 'g2', '9007199254740993'), result('result1', 'g1', '9007199254740994'), result('result2', 'g2', '9007199254740995')]));
  expect(rows.map(row => row.role)).toEqual(['assistant', 'tool', 'assistant', 'assistant', 'tool', 'assistant']);
  expect(rows.filter(row => row.role === 'tool').map(row => [row.text, row.references, row.seq])).toEqual([['g1', ['result1'], '9007199254740992'], ['g2', ['result2'], '9007199254740993']]);
  expect(rows.filter(row => row.role === 'assistant').map(row => row.copyText)).toEqual(['Before\n\nAfter', '', 'Before\n\nAfter', '']);
});
it('replaces provisional text and reasoning wholesale, and suppresses a preview once the exact message is committed or omitted', () => {
  const preview = { attempt_id: 'attempt', turn_id: 'turn', message_id: 'one', revision: '2', text: 'Current', reasoning: 'Thinking', calls: [], truncated: false };
  expect(timelineRows(undefined, preview).map(row => row.text)).toEqual(['Thinking', 'Current']);
  expect(timelineRows(undefined, { ...preview, text: 'Replacement', reasoning: '' }).map(row => row.text)).toEqual(['Replacement']);
  expect(timelineRows(history([call('one', 'g1', '1')]), preview).every(row => !row.live)).toBe(true);
  expect(timelineRows(history([], [{ messageID: 'one', sequence: '1', reason: 'message_too_large', bytes: 99999 }]), preview)).toHaveLength(1);
});
it('uses mail provenance rather than text heuristics and removes only the exact committed local input', () => {
  const authored: Message = { ...base, id: 'authored', role: 'user', sequence: '1', input_id: 'input', opening_input: true, parts: [{ type: 'text', text: 'Mailbox digest: my literal request' }] };
  const mail: Message = { ...authored, id: 'mail', sequence: '2', input_id: null, input_identity: null, opening_input: false, mail: { id: 'mail', revision: '1', presentation: 'body' } };
  const imported: Message = { ...authored, id: 'imported', sequence: '3', input_id: null, input_identity: null, turn_id: null, source: { session_id: 'source', message_id: 'old', sequence: '9' } };
  const rows = conversationRows(history([authored, mail, imported]), null, [], [
    { id: 'local', runtimeId: 'runtime', rootId: 'root', agentId: 'child', text: 'Duplicate', inputId: 'input', accepted: true, confirmed: false, queued: false, sentAt: '' },
    { id: 'other', runtimeId: 'runtime', rootId: 'root', agentId: 'child', text: 'Uncertain', accepted: false, confirmed: false, queued: false, sentAt: '' },
  ]);
  expect(rows.map(row => row.role)).toEqual(['user', 'mailbox', 'user', 'user']);
  expect(rows.at(-1)).toMatchObject({ text: 'Uncertain', delivery: 'Sending…' });
});
