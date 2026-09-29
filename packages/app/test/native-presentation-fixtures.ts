import { assertValid } from '@whip/protocol';
import type { Message, Operations } from '@whip/protocol';
import type { HistoryView } from '@whip/sdk/state';
import type { InboxInput } from '../src/input-presentation';

export const created = '2026-09-07T20:48:00Z';
export function message(overrides: Partial<Message> = {}): Message {
  const value = { id: 'message', session_id: 'root', group_id: 'turn', turn_id: 'turn', input_id: null, input_identity: null,
    opening_input: false, source: null, retired_by: null, retired_revision: null, mail: null, sequence: '1',
    role: 'assistant', parts: [{ type: 'text', text: 'Done' }], created_at: created, ...overrides } as Message;
  assertValid('Message', value);
  return value;
}
export function history(...messages: Message[]): HistoryView {
  return { snapshot: { session_id: 'root', revision: '1', through_sequence: messages.at(-1)?.sequence ?? '0', message_count: String(messages.length) }, messages, gaps: [], olderCursor: null, latestMissing: false };
}
export function inbox(id: string, state: InboxInput['state'] = 'claimed', overrides: Partial<InboxInput> = {}): InboxInput {
  const value = { id, session_id: 'root', identity: null, ordinal: id, state, kind: 'prompt', source: 'user',
    turn_id: state === 'claimed' ? 'turn' : null, created_at: created, text_preview: 'Again', preview_truncated: false, attachment_count: '0', ...overrides } as InboxInput;
  assertValid('InputPageResult', { items: [value], next_cursor: null });
  return value;
}
export function preview(overrides: Partial<NonNullable<Operations['sessions.observe']['result']['preview']>> = {}) {
  return { attempt_id: 'attempt', turn_id: 'turn', message_id: 'live', revision: '1', text: 'Working', reasoning: '', calls: [], truncated: false, ...overrides };
}
