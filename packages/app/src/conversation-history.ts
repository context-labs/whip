import type { Session } from '@whip/sdk';
import type { DeepReadonly, HistoryView } from '@whip/sdk/state';

/** Find an actual preceding exchange boundary. Sequence gaps are not arithmetic.
 * The native transaction rechecks both the captured revision and observed tail. */
export async function historyBoundary(
  session: Session,
  history: DeepReadonly<HistoryView>,
  sequence: string,
  signal?: AbortSignal,
): Promise<string> {
  const snapshot = history.snapshot;
  const selected = history.messages.find(
    (message) => message.sequence === sequence,
  );
  if (
    !snapshot ||
    snapshot.session_id !== session.id ||
    !selected ||
    selected.session_id !== session.id
  )
    throw new Error('Reload this message before editing history');
  let cursor = history.messages.find(
    (message) => message.group_id === selected.group_id,
  )!.sequence;
  const earlier = history.messages.filter(
    (message) => BigInt(message.sequence) < BigInt(cursor),
  );
  const previous = earlier.at(-1);
  if (
    previous &&
    !history.gaps.some(
      (gap) =>
        BigInt(gap.sequence) > BigInt(previous.sequence) &&
        BigInt(gap.sequence) < BigInt(cursor),
    )
  )
    return previous.sequence;
  if (
    !previous &&
    history.olderCursor === null &&
    !history.gaps.some((gap) => BigInt(gap.sequence) < BigInt(cursor))
  )
    return '0';
  // An exchange may cross the visible window. Bound this explicit lookup rather
  // than crawling an arbitrary transcript or guessing the prior sequence.
  for (let pageIndex = 0; pageIndex < 4; pageIndex++) {
    const page = await session.history.page(
      {
        direction: 'backward',
        cursor,
        expected_revision: snapshot.revision,
        limit: 100,
      },
      { signal },
    );
    if (
      page.snapshot.session_id !== session.id ||
      page.snapshot.revision !== snapshot.revision ||
      page.snapshot.through_sequence !== snapshot.through_sequence
    )
      throw new Error(
        'History changed while selecting the exchange. Refresh and select it again.',
      );
    const values = page.messages ?? [];
    if (
      values.some(
        (message, index) =>
          message.session_id !== session.id ||
          message.retired_revision !== null ||
          BigInt(message.sequence) >= BigInt(cursor) ||
          (index > 0 &&
            BigInt(message.sequence) <= BigInt(values[index - 1]!.sequence)),
      )
    )
      throw new Error('History boundary page has a different owner or cursor');
    const previous = [...values]
      .reverse()
      .find((message) => message.group_id !== selected.group_id);
    if (previous) return previous.sequence;
    if (page.next_cursor === null) return '0';
    if (!values.length || page.next_cursor !== values[0]!.sequence)
      throw new Error('History boundary page made no progress');
    cursor = page.next_cursor;
  }
  throw new Error(
    'This exchange extends beyond the bounded lookup. Load earlier history and select its opening message.',
  );
}

/** Resolve a response's actual whole-group end. Never infer adjacent sequences;
 * retired gaps and imported groups are legitimate native history. */
export async function historyGroupEnd(
  session: Session, history: DeepReadonly<HistoryView>, sequence: string, signal?: AbortSignal,
): Promise<string> {
  const snapshot = history.snapshot;
  const selected = history.messages.find(message => message.sequence === sequence);
  if (!snapshot || snapshot.session_id !== session.id || !selected ||
    selected.session_id !== session.id || selected.retired_revision !== null)
    throw new Error('Reload this response before editing history');
  let tail = selected.sequence;
  const later = history.messages.filter(message => BigInt(message.sequence) > BigInt(tail));
  for (const message of later) {
    if (history.gaps.some(gap => BigInt(gap.sequence) > BigInt(tail) && BigInt(gap.sequence) < BigInt(message.sequence))) break;
    if (message.session_id !== session.id || message.retired_revision !== null)
      throw new Error('Response history has a different owner or revision');
    if (message.group_id !== selected.group_id) return tail;
    tail = message.sequence;
  }
  if (tail === snapshot.through_sequence) return tail;
  for (let pageIndex = 0; pageIndex < 4; pageIndex++) {
    const page = await session.history.page({ direction: 'forward', cursor: tail,
      expected_revision: snapshot.revision, limit: 100 }, { signal });
    if (page.snapshot.session_id !== session.id || page.snapshot.revision !== snapshot.revision ||
      page.snapshot.through_sequence !== snapshot.through_sequence)
      throw new Error('History changed while selecting the response. Refresh and select it again.');
    const values = page.messages ?? [];
    if (values.some((message, index) => message.session_id !== session.id || message.retired_revision !== null ||
      BigInt(message.sequence) <= BigInt(tail) ||
      (index > 0 && BigInt(message.sequence) <= BigInt(values[index - 1]!.sequence))))
      throw new Error('Response boundary page has a different owner or cursor');
    for (const message of values) {
      if (message.group_id !== selected.group_id) return tail;
      tail = message.sequence;
    }
    if (page.next_cursor === null) {
      if (tail !== snapshot.through_sequence) throw new Error('Response boundary page is incomplete');
      return tail;
    }
    if (!values.length || page.next_cursor !== tail)
      throw new Error('Response boundary page made no progress');
  }
  throw new Error('This response extends beyond the bounded lookup. Load later history before editing it.');
}

/** Explicit, bounded body inspection outside the SDK's retained transcript. */
export async function readLargeMessage(
  session: Session,
  id: string,
  sequence: string,
  maxBytes: number,
  signal?: AbortSignal,
): Promise<Uint8Array<ArrayBuffer>> {
  if (!Number.isSafeInteger(maxBytes) || maxBytes < 0 || maxBytes > 4 << 20)
    throw new Error('Message read limit exceeds 4 MiB');
  let result: Uint8Array<ArrayBuffer> | undefined,
    offset = 0;
  do {
    const page = await session.client.call(
      'context.read',
      {
        session_id: session.id,
        message_id: id,
        offset: String(offset),
        length: 65536,
      },
      { signal },
    );
    if (
      page.message.id !== id ||
      page.message.session_id !== session.id ||
      page.message.sequence !== sequence ||
      page.message.retired_revision !== null ||
      page.offset !== String(offset)
    )
      throw new Error('Message identity or history changed');
    const bytes = BigInt(page.message.parts_bytes);
    if (bytes > BigInt(maxBytes))
      throw new Error(`Message exceeds this ${maxBytes >> 20} MiB read limit`);
    if (!result) result = new Uint8Array(Number(bytes));
    if (bytes !== BigInt(result.length))
      throw new Error('Message size changed');
    const binary = atob(page.data_base64);
    if (
      btoa(binary) !== page.data_base64 ||
      binary.length > 65536 ||
      offset + binary.length > result.length
    )
      throw new Error('Invalid message page');
    result.set(
      Uint8Array.from(binary, (value) => value.charCodeAt(0)),
      offset,
    );
    offset += binary.length;
    if (page.next_offset === null) {
      if (offset !== result.length) throw new Error('Incomplete message body');
      return result;
    }
    if (!binary.length || page.next_offset !== String(offset))
      throw new Error('Message body cursor made no progress');
  } while (offset <= maxBytes);
  throw new Error('Message exceeds read limit');
}
