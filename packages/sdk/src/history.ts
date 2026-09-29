import { assertValid } from '@whip/protocol';
import type { Message, ReadHistoryResult } from '@whip/protocol';
import type { Session } from './session.js';
import type { CallOptions } from './wire.js';

export interface MessageReadOptions extends CallOptions {
  maxBytes?: number;
  sequence?: string;
  turnID?: string;
  groupID?: string;
  role?: Message['role'];
}

/** Exact bounded canonical body read. Every chunk must retain the same complete
 * metadata; a rewind, owner switch or changed identity cannot splice bodies. */
export async function readHistoryMessage(session: Session, messageID: string, options: MessageReadOptions = {}): Promise<Message> {
  const maxBytes = options.maxBytes ?? 1 << 20;
  if (!Number.isSafeInteger(maxBytes) || maxBytes < 1 || maxBytes > 4 << 20) throw new RangeError('Message read limit must be within 1..4 MiB');
  let data: Uint8Array<ArrayBuffer> | undefined, metadata: ReadHistoryResult['message'] | undefined, identity: string | undefined, offset = 0;
  for (let chunks = 0; chunks < 64; chunks++) {
    const page = await session.client.call('context.read', { session_id: session.id, message_id: messageID, offset: String(offset), length: 65536 }, options);
    const m = page.message;
    if (m.id !== messageID || m.session_id !== session.id || m.retired_by !== null || m.retired_revision !== null || page.offset !== String(offset)
      || options.sequence !== undefined && m.sequence !== options.sequence || options.turnID !== undefined && m.turn_id !== options.turnID
      || options.groupID !== undefined && m.group_id !== options.groupID || options.role !== undefined && m.role !== options.role
      || !m.created_at) throw new TypeError('Message identity or history changed');
    const currentIdentity = JSON.stringify(m);
    if (identity !== undefined && identity !== currentIdentity) throw new TypeError('Message metadata changed between chunks');
    identity = currentIdentity; metadata = m;
    const size = BigInt(m.parts_bytes);
    if (size > BigInt(maxBytes)) throw new RangeError('Message exceeds retained body byte limit');
    if (!data) data = new Uint8Array(Number(size));
    const binary = atob(page.data_base64);
    if (btoa(binary) !== page.data_base64 || binary.length > 65536 || offset + binary.length > data.length) throw new TypeError('Invalid canonical message page');
    data.set(Uint8Array.from(binary, value => value.charCodeAt(0)), offset); offset += binary.length;
    if (page.next_offset === null) {
      if (offset !== data.length) throw new TypeError('Incomplete canonical message body');
      const { parts_bytes: _length, ...fields } = metadata;
      const value = { ...fields, created_at: m.created_at, parts: JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(data)) };
      assertValid('Message', value);
      return value;
    }
    if (!binary.length || page.next_offset !== String(offset)) throw new TypeError('Message body cursor made no progress');
  }
  throw new RangeError('Message read exceeds chunk limit');
}
