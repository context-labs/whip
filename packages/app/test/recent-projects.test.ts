import type { Client } from '@whip/sdk';
import type { RecentTreesResult } from '@whip/protocol';
import { expect, it, vi } from 'vitest';
import { readRecentProjects } from '../src/recent-projects';
const item = (id: string): RecentTreesResult['items'][number] => ({
  root_id: 'root-' + id, working_directory: '/repo', model: { provider: 'test', name: 'test', effort: '' }, last_activity_at: '2026-09-29T00:00:00Z',
  tree: { id, metadata: { title: id, archived: false, pinned: false }, revision: '1', created_at: '2026-09-28T00:00:00Z', engine: 'starlark' },
});
const cursor = (id: string) => ({ tree_id: id, last_activity_at: '2026-09-29T00:00:00Z', pinned: false });
const page = (ids: string[], more = true): RecentTreesResult => ({ items: ids.map(item), catalog_revision: '1', has_more: more, ...(more ? { next_cursor: cursor(ids.at(-1)!) } : {}) });

it('bounds the recent window, deduplicates trees moving across advisory pages and keeps its range', async () => {
  const read = vi.fn().mockResolvedValueOnce(page(['a', 'b'])).mockResolvedValueOnce(page(['b', 'c']));
  const client = { trees: { recentPage: read } } as unknown as Client;
  const signal = new AbortController().signal;
  const result = await readRecentProjects(client, { pages: 2 }, signal);
  expect(result.items.map(item => item.tree.id)).toEqual(['a', 'b', 'c']);
  expect(result).toMatchObject({ nextCursor: cursor('c'), truncated: false, range: { pages: 2 } });
  expect(read.mock.calls).toEqual([
    [{ limit: 100, archived: false, pinned_first: true }, { signal }],
    [{ limit: 100, archived: false, pinned_first: true, after: cursor('b') }, { signal }],
  ]);
  read.mockImplementation(({ after }) => page([after?.tree_id + 'next']));
  const capped = await readRecentProjects(client, { pages: 999, after: cursor('c') }, signal);
  expect(capped.truncated).toBe(true);
  expect(read).toHaveBeenCalledTimes(7);
});

it('stops non-advancing cursors and missing continuations without retaining partial reads', async () => {
  const read = vi.fn().mockResolvedValue(page(['a']));
  const client = { trees: { recentPage: read } } as unknown as Client;
  await expect(readRecentProjects(client, { pages: 2 }, new AbortController().signal)).rejects.toThrow('did not advance');
  expect(read).toHaveBeenCalledTimes(2);
  read.mockResolvedValue({ ...page(['a']), next_cursor: undefined });
  await expect(readRecentProjects(client, { pages: 1 }, new AbortController().signal)).rejects.toThrow('no continuation');
});

it('bounds retained bytes and supplies a cursor for the last retained row', async () => {
  const rows = Array.from({ length: 100 }, (_, i) => ({ ...item(String(i)), working_directory: '😀'.repeat(4000) }));
  const read = vi.fn().mockResolvedValueOnce({ ...page(['99']), items: rows })
    .mockResolvedValueOnce({ ...page(['199']), items: rows.map((row, i) => ({ ...row, tree: { ...row.tree, id: String(i + 100) } })) });
  const result = await readRecentProjects({ trees: { recentPage: read } } as unknown as Client, { pages: 2 }, new AbortController().signal);
  expect(result.truncated).toBe(true);
  expect(new TextEncoder().encode(JSON.stringify(result)).length).toBeLessThan(2 << 20);
  expect(result.nextCursor?.tree_id).toBe(result.items.at(-1)?.tree.id);
  expect(result.items.length).toBeLessThan(200);
});
