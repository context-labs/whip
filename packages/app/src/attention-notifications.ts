import type { QueryClient } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import type { HostAttentionResult } from '@whip/protocol';
import type { AppNotification } from './platform';

const rootsLimit = 256;
const questionLimit = 64;
export function attentionQuery(client: WhipClient, runtimeId: string | undefined, after?: string) {
  return {
    queryKey: ['host-attention', runtimeId, after],
    queryFn: ({ signal }: { signal: AbortSignal }) => client.host.attention({ after_id: after, limit: 64, max_bytes: 256 << 10 }, { signal }),
  };
}

type AttentionItem = {
  rootId: string; title: string; permissions: string; questionCount: number;
  questionIds: string[]; completeQuestions: boolean;
};
export type AttentionScan = { items: AttentionItem[]; complete: boolean };

/** Reuse the UI's first page; bound additional reads without opening root views. */
export async function scanAttention(client: WhipClient, queries: QueryClient, runtimeId: string, signal: AbortSignal): Promise<AttentionScan> {
  signal.throwIfAborted();
  let page = await queries.fetchQuery({ ...attentionQuery(client, runtimeId), staleTime: 0 });
  const result: AttentionScan = { items: [], complete: true };
  const cursors = new Set<string>();
  for (let number = 0; number < 4; number++) {
    signal.throwIfAborted();
    result.complete &&= !page.truncated && (page.items?.length ?? 0) <= 64;
    result.items.push(...(page.items ?? []).slice(0, 64).map(summarize));
    if (!page.has_more) return result;
    const after = page.next_after_id;
    if (number === 3 || !after || after.length > 256 || cursors.has(after)) break;
    cursors.add(after);
    page = await client.host.attention({ after_id: after, limit: 64, max_bytes: 256 << 10 }, { signal });
  }
  result.complete = false;
  return result;
}

function summarize(item: NonNullable<HostAttentionResult['items']>[number]): AttentionItem {
  const questions = item.questions ?? [];
  const ids = questions.slice(0, questionLimit).map(question => question.question_id)
    .filter((id): id is string => !!id && id.length <= 256);
  return {
    rootId: item.root_id, title: item.title.slice(0, 256), permissions: item.pending_permissions,
    questionCount: questions.length, questionIds: ids,
    completeQuestions: questions.length <= questionLimit && ids.length === questions.length,
  };
}

type SeenRoot = { id: string; permissions: bigint; questions: number; ids: Set<string>; completeQuestions: boolean };

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
      if (!item.rootId || item.rootId.length > 256 || seen.has(item.rootId)) continue;
      seen.add(item.rootId);
      const previous = this.roots.get(item.rootId);
      const permissions = BigInt(item.permissions) > 0n ? BigInt(item.permissions) : 0n;
      const ids = new Set(item.questionIds.slice(0, questionLimit));
      const changed = previous
        ? permissions > previous.permissions || item.questionCount > previous.questions
          || (item.completeQuestions && previous.completeQuestions && [...ids].some(id => !previous.ids.has(id)))
        : this.initialized && this.completeBaseline && (permissions > 0n || item.questionCount > 0);
      const entry: SeenRoot = { id: previous?.id ?? crypto.randomUUID(), permissions, questions: item.questionCount, ids, completeQuestions: item.completeQuestions };
      this.roots.delete(item.rootId); this.roots.set(item.rootId, entry);
      if (this.roots.size > rootsLimit) this.roots.delete(this.roots.keys().next().value!);
      let path: string;
      try { path = `/h/${encodeURIComponent(runtimeId)}/s/${encodeURIComponent(item.rootId)}`; }
      catch { continue; }
      if (changed && this.initialized) notifications.push({
        id: entry.id,
        title: item.title.replace(/[\x00-\x1f\x7f]/g, ' ').slice(0, 256) || 'Whip needs your attention',
        body: `${permissions} permission ${permissions === 1n ? 'request' : 'requests'} · ${item.questionCount} ${item.questionCount === 1 ? 'question' : 'questions'} awaiting your response.`,
        path,
      });
    }
    // Only a complete successful scan can establish that pending requests cleared.
    if (scan.complete) for (const [id, entry] of this.roots) if (!seen.has(id)) {
      entry.permissions = 0n; entry.questions = 0; entry.ids.clear(); entry.completeQuestions = true;
    }
    this.initialized = true; this.completeBaseline = scan.complete;
    return notifications;
  }
}
