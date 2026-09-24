/** @jest-environment node */
import { captureReadingBookmark, restoreReadingBookmark, validReadingBookmark } from './reading';

const rows = [{ id: 'first', seq: 10 }, { id: 'middle', seq: 20 }, { id: 'last', seq: 30 }];

test('a partially visible row round-trips through FlashList offset coordinates including the header', () => {
  const measurement = { index: 1, rowY: 430, firstItemOffset: 80, scrollOffset: 545 };
  const bookmark = captureReadingBookmark(rows, '12', false, measurement)!;
  expect(bookmark).toMatchObject({ messageId: 'middle', seq: 20, revision: '12', offset: 35, follow: false });
  const restored = restoreReadingBookmark(rows, '12', bookmark);
  expect(restored).toEqual({ latest: false, index: 1, offset: 35, fallback: false });
  if (!restored.latest) expect(measurement.rowY + measurement.firstItemOffset + restored.offset).toBe(measurement.scrollOffset);
});

test('a missing native ref or row measurement does not manufacture a first-row bookmark', () => {
  expect(captureReadingBookmark(rows, '1', false)).toBeUndefined();
  expect(captureReadingBookmark(rows, '1', false, { index: -1, rowY: 0, firstItemOffset: 0, scrollOffset: 0 })).toBeUndefined();
  expect(captureReadingBookmark(rows, '1', false, { index: 0, rowY: NaN, firstItemOffset: 0, scrollOffset: 0 })).toBeUndefined();
});

test('a changed transcript revision uses the nearest retained sequence and discards the old offset', () => {
  const bookmark = { messageId: 'middle', seq: 24, revision: '1', offset: 180, follow: false };
  expect(restoreReadingBookmark([{ id: 'middle', seq: 1 }, { id: 'nearby', seq: 25 }], '2', bookmark))
    .toEqual({ latest: false, index: 1, offset: 0, fallback: true });
  expect(restoreReadingBookmark(rows.filter(row => row.id !== 'middle'), '1', bookmark))
    .toEqual({ latest: false, index: 1, offset: 0, fallback: true });
});

test('following latest survives revision changes and malformed saved hints are rejected', () => {
  expect(restoreReadingBookmark(rows, '2', { messageId: 'gone', revision: '1', offset: 12, follow: true })).toEqual({ latest: true, fallback: false });
  for (const value of [null, {}, { messageId: 'x', revision: '1', offset: Infinity, follow: false },
    { messageId: 'x', revision: '1', offset: 0, follow: 'false' }, { messageId: 'x', revision: 'NaN', offset: 0, follow: false },
    { messageId: 'x', revision: '1', offset: 0, follow: false, seq: -1 }]) expect(validReadingBookmark(value)).toBe(false);
});
