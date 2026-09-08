import { readingTarget, type ReadingBookmark } from '@whip/app/presentation';

type ReadingRow = { id: string; seq?: number };
export interface ReadingMeasurement { index: number; rowY: number; firstItemOffset: number; scrollOffset: number }

export function validReadingBookmark(value: unknown): value is ReadingBookmark {
  if (!value || typeof value !== 'object') return false;
  const bookmark = value as ReadingBookmark;
  return typeof bookmark.messageId === 'string' && bookmark.messageId.length > 0 && bookmark.messageId.length <= 512
    && typeof bookmark.revision === 'string' && /^\d{1,19}$/.test(bookmark.revision)
    && Number.isFinite(bookmark.offset) && Math.abs(bookmark.offset) <= 1_000_000 && typeof bookmark.follow === 'boolean'
    && (bookmark.seq === undefined || Number.isSafeInteger(bookmark.seq) && bookmark.seq >= 0);
}

/** FlashList adds viewOffset to row.y + firstItemOffset, including a partially read row. */
export function captureReadingBookmark(rows: readonly ReadingRow[], revision: string | undefined, follow: boolean, measurement?: ReadingMeasurement): ReadingBookmark | undefined {
  if (!measurement || !Number.isInteger(measurement.index)) return;
  const row = rows[measurement.index];
  if (!row || !revision) return;
  const bookmark = { messageId: row.id, seq: row.seq, revision,
    offset: measurement.scrollOffset - measurement.firstItemOffset - measurement.rowY, follow };
  return validReadingBookmark(bookmark) ? bookmark : undefined;
}

/** Uses retained rows only; a changed revision invalidates the old pixel offset. */
export function restoreReadingBookmark(rows: readonly ReadingRow[], revision: string, bookmark: ReadingBookmark | null) {
  if (!bookmark || bookmark.follow) return { latest: true as const, fallback: false };
  return { latest: false as const, ...readingTarget(rows, revision, bookmark) };
}
