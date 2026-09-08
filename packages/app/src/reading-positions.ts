export interface ReadingBookmark {
  readonly messageId: string;
  readonly revision: string;
  readonly offset: number;
  readonly follow: boolean;
  readonly seq?: number;
}
const encoder = new TextEncoder();

/** Bounded per-view/agent reading hints, never a second transcript cache. */
export class ReadingPositions {
  private readonly entries = new Map<string, ReadingBookmark>();
  get(key: string): ReadingBookmark | undefined {
    const value = this.entries.get(key);
    if (value) {
      this.entries.delete(key);
      this.entries.set(key, value);
    }
    return value;
  }
  set(key: string, bookmark: ReadingBookmark) {
    if (
      !key ||
      encoder.encode(key).length > 512 ||
      !bookmark.messageId ||
      encoder.encode(bookmark.messageId).length > 512 ||
      !/^\d{1,19}$/.test(bookmark.revision) ||
      !Number.isFinite(bookmark.offset) ||
      Math.abs(bookmark.offset) > 1_000_000 ||
      typeof bookmark.follow !== 'boolean' ||
      (bookmark.seq !== undefined &&
        (!Number.isSafeInteger(bookmark.seq) || bookmark.seq < 0))
    )
      return;
    this.entries.delete(key);
    this.entries.set(
      key,
      Object.freeze({
        messageId: bookmark.messageId,
        revision: bookmark.revision,
        offset: bookmark.offset,
        follow: bookmark.follow,
        ...(bookmark.seq === undefined ? {} : { seq: bookmark.seq }),
      }),
    );
    while (
      this.entries.size > 128 ||
      encoder.encode(JSON.stringify([...this.entries])).length > 64 * 1024
    )
      this.entries.delete(this.entries.keys().next().value!);
  }
  forgetView(runtimeId: string, viewId: string) {
    const prefix = `${runtimeId}:${viewId}:`;
    for (const key of this.entries.keys())
      if (key.startsWith(prefix)) this.entries.delete(key);
  }
  clear() {
    this.entries.clear();
  }
}

/** Choose only among already retained rows; restoration never loads more history. */
export function readingTarget(
  rows: readonly { id: string; seq?: number }[],
  revision: string,
  bookmark: ReadingBookmark,
): { index: number; offset: number; fallback: boolean } {
  const exact =
    revision === bookmark.revision
      ? rows.findIndex((row) => row.id === bookmark.messageId)
      : -1;
  if (exact >= 0)
    return { index: exact, offset: bookmark.offset, fallback: false };
  let index = rows.length ? 0 : -1;
  if (bookmark.seq !== undefined) {
    let nearest = Infinity;
    for (let current = 0; current < rows.length; current++) {
      const seq = rows[current]!.seq;
      if (seq === undefined) continue;
      const distance = Math.abs(seq - bookmark.seq);
      if (distance < nearest) {
        nearest = distance;
        index = current;
      }
    }
  }
  return { index, offset: 0, fallback: true };
}
