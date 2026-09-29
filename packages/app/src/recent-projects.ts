import type { Client } from '@whip/sdk';
import type { RecentTreesResult } from '@whip/protocol';

type RecentTreeCursor = NonNullable<RecentTreesResult['next_cursor']>;
export interface RecentProjectsRange { after?: RecentTreeCursor; pages: number }
export interface RecentProjects {
  items: RecentTreesResult['items'];
  nextCursor?: RecentTreeCursor;
  range: RecentProjectsRange;
  truncated: boolean;
}
const maxItems = 500;
const maxBytes = 2 << 20;
const maxPages = 5;

/** One bounded Query-owned window. Recency cursors are advisory: concurrent
 * activity/pin changes can move trees across pages. Dedupe within this window;
 * explicitly return to the latest window to discover work that moved ahead. */
export async function readRecentProjects(client: Client, range: RecentProjectsRange, signal: AbortSignal): Promise<RecentProjects> {
  const result: RecentProjects = { items: [], range, truncated: false };
  const seen = new Set<string>();
  const cursors = new Set<string>();
  let after = range.after, used = 1024;
  if (after) cursors.add(JSON.stringify(after));
  for (let index = 0; index < Math.min(maxPages, range.pages); index++) {
    const page = await client.trees.recentPage({ limit: 100, archived: false, pinned_first: true, ...(after ? { after } : {}) }, { signal });
    for (const item of page.items) {
      if (seen.has(item.tree.id)) continue;
      const bytes = new TextEncoder().encode(JSON.stringify(item)).length;
      if (result.items.length >= maxItems || used + bytes > maxBytes) {
        if (!result.items.length) throw new Error('The recent session exceeds the sidebar window limit.');
        result.truncated = true;
        return result;
      }
      used += bytes;
      seen.add(item.tree.id);
      result.items.push(item);
      result.nextCursor = { tree_id: item.tree.id, last_activity_at: item.last_activity_at, pinned: item.tree.metadata.pinned };
    }
    if (!page.has_more) { result.nextCursor = undefined; return result; }
    if (!page.next_cursor || !page.items.length) throw new Error('The recent sessions page has no continuation cursor.');
    const key = JSON.stringify(page.next_cursor);
    if (cursors.has(key)) throw new Error('The recent sessions cursor did not advance.');
    cursors.add(key);
    after = result.nextCursor = page.next_cursor;
  }
  result.truncated = range.pages >= maxPages && !!result.nextCursor;
  return result;
}
