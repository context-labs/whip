import type { Session } from '@whip/protocol';
import type { SessionViewSnapshot } from '@whip/sdk/state';
import { historyBoundaries, historyCut } from './history-actions';
const session = { id: 'child', config_revision: '9007199254740997' } as Session;
const observed = () => ({ status: 'live', sessionID: 'child', unavailable: false, history: { messages: [{ group_id: 'earlier', sequence: '9007199254740992' }, { group_id: 'later', sequence: '9007199254740993' }], snapshot: { session_id: 'child', revision: '9007199254740995', through_sequence: '9007199254740993' } } }) as SessionViewSnapshot;
test('fork and rewind boundaries preserve exact recipient and counters beyond Number precision', () => {
  expect(historyCut(session, observed(), '9007199254740992')).toEqual({ session_id: 'child', expected_history_revision: '9007199254740995', expected_config_revision: '9007199254740997', observed_through: '9007199254740993', keep_through: '9007199254740992' });
});
test.each(['', '01', '-1', '1.0', '1e1', '9007199254740994', '9223372036854775808'])('rejects invalid or unobserved boundary %s', value => expect(() => historyCut(session, observed(), value)).toThrow('exact retained'));
test('stale views and another recipient never authorize a history edit', () => {
  for (const patch of [{ status: 'suspended' }, { unavailable: true }, { sessionID: 'root' }, { history: { messages: [{ group_id: 'earlier', sequence: '9007199254740992' }, { group_id: 'later', sequence: '9007199254740993' }], snapshot: { ...observed().history.snapshot, session_id: 'root' } } }]) expect(() => historyCut(session, { ...observed(), ...patch } as SessionViewSnapshot, '0')).toThrow('current recipient');
});

test('group boundaries reject split and interleaved groups before preparing a durable request', () => {
  const snapshot = observed(); snapshot.history.messages = [
    { group_id: 'a', sequence: '1' }, { group_id: 'b', sequence: '2' }, { group_id: 'a', sequence: '3' }, { group_id: 'b', sequence: '4' },
  ] as unknown as SessionViewSnapshot['history']['messages'];
  expect(historyBoundaries(snapshot)).toEqual(['0', '4']);
  expect(() => historyCut(session, snapshot, '2')).toThrow('whole message group');
  expect(historyCut(session, snapshot, '0').keep_through).toBe('0');
});
