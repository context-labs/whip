import { expect, it } from 'vitest';
import type { Message } from '@whip/sdk';
import { historyBoundary, readLargeMessage } from '../src/conversation-history';
import {
  history,
  messageBase,
  parameters,
} from './native-conversation-fixture';
import { providerFixture } from './provider-fixture';
const { created_at: _createdAt, ...metadataBase } = messageBase;
const message = (id: string, group: string, sequence: string): Message => ({
  ...messageBase,
  id,
  group_id: group,
  sequence,
  role: 'user',
  parts: [{ type: 'text', text: id }],
});
it('uses an actual preceding exchange tail, preserving retired sequence gaps and >2^53 values', async () => {
  const f = await providerFixture(),
    handle = f.client.session('root');
  const messages = [
    message('previous', 'one', '9007199254740993'),
    message('opening', 'two', '9007199254741993'),
    message('later', 'two', '9007199254741995'),
  ];
  expect(
    await historyBoundary(handle, history(messages), messages[2]!.sequence),
  ).toBe('9007199254740993');
  expect(f.count('sessions.history_page')).toBe(0);
  expect(
    await historyBoundary(handle, history(messages), messages[0]!.sequence),
  ).toBe('0');
});
it('bounded backward lookup finds an exchange opening outside the visible window without guessing a sequence', async () => {
  const f = await providerFixture(),
    handle = f.client.session('root'),
    current = history([message('later', 'two', '9007199254741995')]);
  current.olderCursor = current.messages[0]!.sequence;
  f.data.handlers['sessions.history_page'] = () => ({
    snapshot: current.snapshot,
    messages: [
      message('previous', 'one', '9007199254740993'),
      message('opening', 'two', '9007199254741993'),
    ],
    next_cursor: null,
  });
  expect(
    await historyBoundary(handle, current, current.messages[0]!.sequence),
  ).toBe('9007199254740993');
  expect(f.calls.at(-1)?.params).toMatchObject({
    cursor: '9007199254741995',
    expected_revision: '9007199254740993',
    direction: 'backward',
    limit: 100,
    session_id: 'root',
  });
});
it('refuses to silently refresh a changed tail or history revision during confirmation preparation', async () => {
  const f = await providerFixture(),
    current = history([message('later', 'two', '5')]);
  current.olderCursor = '5';
  f.data.handlers['sessions.history_page'] = () => ({
    snapshot: { ...current.snapshot, through_sequence: '6' },
    messages: [],
    next_cursor: null,
  });
  await expect(
    historyBoundary(f.client.session('root'), current, '5'),
  ).rejects.toThrow('History changed');
});
it('reads a large Unicode message in bounded byte chunks with identity checks and no transcript insertion', async () => {
  const f = await providerFixture(),
    bytes = new TextEncoder().encode(
      JSON.stringify([{ type: 'text', text: '🌍'.repeat(30000) }]),
    );
  f.data.handlers['context.read'] = (request) => {
    const offset = Number(
        parameters('ReadHistoryParams', request.params).offset,
      ),
      data = bytes.slice(
        offset,
        offset + parameters('ReadHistoryParams', request.params).length,
      );
    return {
      message: {
        ...metadataBase,
        id: 'large',
        sequence: '9007199254740993',
        role: 'user',
        parts_bytes: String(bytes.length),
      },
      offset: String(offset),
      next_offset:
        offset + data.length === bytes.length
          ? null
          : String(offset + data.length),
      data_base64: btoa(
        Array.from(data, (byte) => String.fromCharCode(byte)).join(''),
      ),
    };
  };
  expect(
    Array.from(
      await readLargeMessage(
        f.client.session('root'),
        'large',
        '9007199254740993',
        1 << 20,
      ),
    ),
  ).toEqual(Array.from(bytes));
  expect(f.count('context.read')).toBe(2);
  expect(f.count('sessions.observe')).toBe(0);
  await expect(
    readLargeMessage(f.client.session('root'), 'large', 'different', 1 << 20),
  ).rejects.toThrow('identity');
});
it('refuses oversize bodies before further reads and rejects offset/retirement mismatches', async () => {
  const f = await providerFixture();
  const result = {
    message: {
      ...metadataBase,
      id: 'large',
      sequence: '1',
      role: 'user',
      parts_bytes: '2097152',
    },
    offset: '0',
    next_offset: '1',
    data_base64: 'YQ==',
  };
  f.data.handlers['context.read'] = () => result;
  await expect(
    readLargeMessage(f.client.session('root'), 'large', '1', 1 << 20),
  ).rejects.toThrow('exceeds');
  expect(f.count('context.read')).toBe(1);
  f.data.handlers['context.read'] = () => ({ ...result, offset: '1' });
  await expect(
    readLargeMessage(f.client.session('root'), 'large', '1', 4 << 20),
  ).rejects.toThrow('identity');
  await expect(
    readLargeMessage(f.client.session('root'), 'large', '1', 8 << 20),
  ).rejects.toThrow('limit');
});

it('caps explicit exchange lookup and rejects unordered or retired boundary evidence', async () => {
  const f = await providerFixture();
  const current = history([message('later', 'same', '1000')]);
  current.olderCursor = '1000';
  f.data.handlers['sessions.history_page'] = (request) => {
    const cursor = BigInt(
      parameters('HistoryPageParams', request.params).cursor!,
    );
    const next = String(cursor - 1n);
    return {
      snapshot: current.snapshot,
      messages: [message(next, 'same', next)],
      next_cursor: next,
    };
  };
  await expect(
    historyBoundary(f.client.session('root'), current, '1000'),
  ).rejects.toThrow('bounded lookup');
  expect(f.count('sessions.history_page')).toBe(4);
  f.data.handlers['sessions.history_page'] = () => ({
    snapshot: current.snapshot,
    messages: [message('a', 'prior', '999'), message('b', 'same', '998')],
    next_cursor: null,
  });
  await expect(
    historyBoundary(f.client.session('root'), current, '1000'),
  ).rejects.toThrow('owner or cursor');
  f.data.handlers['sessions.history_page'] = () => ({
    snapshot: current.snapshot,
    messages: [
      {
        ...message('retired', 'prior', '999'),
        retired_revision: '2',
        retired_by: 'edit',
      },
    ],
    next_cursor: null,
  });
  await expect(
    historyBoundary(f.client.session('root'), current, '1000'),
  ).rejects.toThrow('owner or cursor');
});
