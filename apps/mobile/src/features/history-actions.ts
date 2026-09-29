import type { Session } from '@whip/protocol';
import type { DeepReadonly, SessionViewSnapshot } from '@whip/sdk/state';
/** Known complete group boundaries in the bounded transcript window. The host
 * still validates terminal turns and any interleaved groups outside this page. */
export function historyBoundaries(snapshot: DeepReadonly<SessionViewSnapshot>): string[] {
  const messages = snapshot.history.messages, ends = new Map<string, number>();
  messages.forEach((message, index) => ends.set(message.group_id, index));
  let through = -1; const boundaries = ['0'];
  messages.forEach((message, index) => {
    through = Math.max(through, ends.get(message.group_id)!);
    if (through === index) boundaries.push(message.sequence);
  });
  return boundaries;
}
/** Capture a reviewed history boundary without narrowing exact SQL counters. */
export function historyCut(session: Session, snapshot: DeepReadonly<SessionViewSnapshot>, keepThrough: string) {
  const history = snapshot.history.snapshot;
  if (snapshot.status !== 'live' || snapshot.unavailable || snapshot.sessionID !== session.id || !history || history.session_id !== session.id) throw new Error('Read the current recipient history before changing it.');
  if (!/^(0|[1-9][0-9]{0,18})$/.test(keepThrough) || BigInt(keepThrough) > 9223372036854775807n || BigInt(keepThrough) > BigInt(history.through_sequence)) throw new Error('Choose an exact retained message sequence within the observed history.');
  if (snapshot.activity?.active_turn) throw new Error('Wait for this recipient’s active turn before changing its history.');
  if (!historyBoundaries(snapshot).includes(keepThrough)) throw new Error('Load and choose a whole message group boundary from the transcript.');
  return { session_id: session.id, expected_history_revision: history.revision, expected_config_revision: session.config_revision, observed_through: history.through_sequence, keep_through: keepThrough };
}
