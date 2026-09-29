import { useInfiniteQuery } from '@tanstack/react-query';
import { assertValid } from '@whip/protocol';
import type { Client, DefinitionRef, ListDefinitionsResult } from '@whip/sdk';

export type DefinitionSummary = NonNullable<ListDefinitionsResult['items']>[number];
export const definitionsQueryKey = (client: Client) => ['definitions', client.runtimeID];
/** Metadata pages have one query owner. Selection always retains the immutable ref. */
export function useDefinitions(client: Client, enabled: boolean) {
  const query = useInfiniteQuery({
    queryKey: definitionsQueryKey(client), initialPageParam: undefined as DefinitionRef | undefined,
    queryFn: ({ signal, pageParam }) => client.agents.list({ limit: 100, ...(pageParam ? { after: pageParam } : {}) }, { signal }),
    getNextPageParam: (last, pages) => pages.length < 10 ? last.next_cursor ?? undefined : undefined, enabled,
  });
  const pages = query.data?.pages;
  const data = pages ? { items: pages.flatMap(page => page.items ?? []), next_cursor: pages.at(-1)?.next_cursor ?? null } : undefined;
  return { supported: true, truncated: (pages?.length ?? 0) >= 10 && !!data?.next_cursor, query: { ...query, data } };
}
export const definitionOptionValue = (ref: DefinitionRef) => JSON.stringify([ref.id, ref.revision]);
export function parseDefinitionOption(value: string): DefinitionRef {
  const parts: unknown = JSON.parse(value);
  if (!Array.isArray(parts) || parts.length !== 2) throw new TypeError('Choose an exact agent revision');
  const ref = { id: parts[0], revision: parts[1] };
  assertValid('DefinitionRef', ref);
  return ref;
}
export function definitionOptions(items: readonly DefinitionSummary[] | null | undefined) {
  return (items ?? []).map(item => ({ value: definitionOptionValue(item.ref), label: `${item.name || item.ref.id} · ${item.ref.id} @ ${item.ref.revision.slice(0, 12)}` }));
}
