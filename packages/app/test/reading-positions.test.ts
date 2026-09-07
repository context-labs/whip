import { expect, it } from 'vitest';
import { ReadingPositions, readingTarget } from '../src/reading-positions';
const bookmark = {
  messageId: 'message',
  revision: '9007199254740993',
  offset: 15,
  follow: false,
  seq: 10,
};
it('retains stable identity/revision/offset without transcript payloads or numeric revision coercion', () => {
  const positions = new ReadingPositions();
  positions.set('runtime:root:child', {
    ...bookmark,
    text: 'not stored',
  } as typeof bookmark);
  expect(positions.get('runtime:root:child')).toEqual(bookmark);
  expect(Object.isFrozen(positions.get('runtime:root:child'))).toBe(true);
  positions.set('invalid', { ...bookmark, offset: NaN });
  positions.set('invalid', { ...bookmark, revision: '1e10' });
  expect(positions.get('invalid')).toBeUndefined();
});
it('evicts least recently read positions at both the128entry and64KiB bounds', () => {
  const positions = new ReadingPositions();
  for (let i = 0; i < 128; i++) positions.set(`r:${i}:root`, bookmark);
  positions.get('r:0:root');
  positions.set('r:128:root', bookmark);
  expect(positions.get('r:0:root')).toBeDefined();
  expect(positions.get('r:1:root')).toBeUndefined();
  positions.clear();
  for (let i = 0; i < 128; i++)
    positions.set(`r:${i}:root`, { ...bookmark, messageId: 'a'.repeat(512) });
  expect(positions.get('r:0:root')).toBeUndefined();
  expect(positions.get('r:127:root')).toBeDefined();
});
it('forgets only the requested runtime and root namespace', () => {
  const positions = new ReadingPositions();
  for (const key of ['a:root:one', 'a:root:two', 'a:root2:one', 'b:root:one'])
    positions.set(key, bookmark);
  positions.forgetRoot('a', 'root');
  expect(positions.get('a:root:one')).toBeUndefined();
  expect(positions.get('a:root:two')).toBeUndefined();
  expect(positions.get('a:root2:one')).toBeDefined();
  expect(positions.get('b:root:one')).toBeDefined();
});
it('restores exact anchors only within the same revision and falls back to nearest loaded sequence', () => {
  const rows = [
    { id: 'first', seq: 4 },
    { id: 'message', seq: 10 },
    { id: 'last', seq: 15 },
  ];
  expect(readingTarget(rows, bookmark.revision, bookmark)).toEqual({
    index: 1,
    offset: 15,
    fallback: false,
  });
  expect(readingTarget(rows, '9007199254740994', bookmark)).toEqual({
    index: 1,
    offset: 0,
    fallback: true,
  });
  expect(
    readingTarget([rows[0]!, rows[2]!], bookmark.revision, bookmark),
  ).toEqual({ index: 1, offset: 0, fallback: true });
  expect(readingTarget([], bookmark.revision, bookmark)).toEqual({
    index: -1,
    offset: 0,
    fallback: true,
  });
});
