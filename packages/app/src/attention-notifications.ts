import type { QueryClient } from '@tanstack/react-query';
import type { Client } from '@whip/sdk';
import { clientQueryKey } from './client-query-key';
import type { HostAttentionResult, HostAttentionParams } from '@whip/protocol';
import type { AppNotification } from './platform';

const rootsLimit = 256;
export function attentionQuery(client: Client | undefined, runtimeId: string | undefined, after: HostAttentionParams['after'] = null) {
  return {
    queryKey: ['host-attention', runtimeId, client ? clientQueryKey(client) : null, after],
    queryFn: ({ signal }: { signal: AbortSignal }) => client!.hostAttention({ after, limit: 64, max_bytes: 256 << 10 }, { signal }),
  };
}

type AttentionItem = { rootId: string; sessionId: string; title: string; permissions: string; questionCount: string };
export type AttentionScan = { items: AttentionItem[]; complete: boolean };

/** Reuse the UI's first page; bound additional reads without opening conversations. */
export async function scanAttention(client: Client, queries: QueryClient, runtimeId: string, signal: AbortSignal): Promise<AttentionScan> {
  signal.throwIfAborted();
  let page = await queries.fetchQuery({ ...attentionQuery(client, runtimeId), staleTime: 0, gcTime: 0 });
  const result: AttentionScan = { items: [], complete: true };
  const cursors = new Set<string>();
  for (let number = 0; number < 4; number++) {
    signal.throwIfAborted();
    result.complete &&= page.items.length <= 64;
    result.items.push(...page.items.slice(0, 64).map(summarize));
    if (!page.next_cursor) return result;
    const after = page.next_cursor;
    const key = JSON.stringify(after);
    if (number === 3 || cursors.has(key)) break;
    cursors.add(key);
    page = await client.hostAttention({ after, limit: 64, max_bytes: 256 << 10 }, { signal });
  }
  result.complete = false;
  return result;
}

function summarize(item: HostAttentionResult['items'][number]): AttentionItem {
  return { rootId: item.root_id, sessionId: item.session_id, title: (item.title ?? '').slice(0, 256),
    permissions: item.activity.pending_permission_count, questionCount: item.activity.pending_question_count };
}

type SeenRoot = { id: string; permissions: bigint; questions: bigint };

/** Advisory attention deltas only: disappearance and idle counts are never completion. */
export class AttentionNotifications {
  private roots = new Map<string, SeenRoot>();
  private runtimeId = '';
  private initialized = false;
  private completeBaseline = false;

  reset() {
    this.roots.clear(); this.initialized = this.completeBaseline = false;
  }

  observe(runtimeId: string, scan: AttentionScan): AppNotification[] {
    if (runtimeId !== this.runtimeId) { this.reset(); this.runtimeId = runtimeId; }
    const seen = new Set<string>();
    const notifications: AppNotification[] = [];
    for (const item of scan.items.slice(0, rootsLimit)) {
      if (!item.rootId || item.rootId.length > 256 || !item.sessionId || item.sessionId.length > 256 || seen.has(item.sessionId)) continue;
      seen.add(item.sessionId);
      const previous = this.roots.get(item.sessionId);
      const permissions = BigInt(item.permissions) > 0n ? BigInt(item.permissions) : 0n;
      const questions = BigInt(item.questionCount);
      const changed = previous
        ? permissions > previous.permissions || questions > previous.questions
        : this.initialized && this.completeBaseline && (permissions > 0n || questions > 0n);
      const entry: SeenRoot = { id: previous?.id ?? crypto.randomUUID(), permissions, questions };
      this.roots.delete(item.sessionId); this.roots.set(item.sessionId, entry);
      if (this.roots.size > rootsLimit) this.roots.delete(this.roots.keys().next().value!);
      let path: string;
      try { path = `/h/${encodeURIComponent(runtimeId)}/s/${encodeURIComponent(item.rootId)}${item.sessionId === item.rootId ? '' : `?agent=${encodeURIComponent(item.sessionId)}`}`; }
      catch { continue; }
      if (changed && this.initialized) notifications.push({
        id: entry.id,
        title: item.title.replace(/[\x00-\x1f\x7f]/g, ' ').slice(0, 256) || 'Whip needs your attention',
        body: `${permissions} permission ${permissions === 1n ? 'request' : 'requests'} · ${item.questionCount} ${questions === 1n ? 'question' : 'questions'} awaiting your response.`,
        path,
      });
    }
    // Only a complete successful scan can establish that pending requests cleared.
    if (scan.complete) for (const [id, entry] of this.roots) if (!seen.has(id)) {
      entry.permissions = 0n; entry.questions = 0n;
    }
    this.initialized = true; this.completeBaseline = scan.complete;
    return notifications;
  }
}
