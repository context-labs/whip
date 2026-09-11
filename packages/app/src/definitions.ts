import { useQuery } from '@tanstack/react-query';
import type { WhipClient } from '@whip/sdk';
import type { DefinitionList } from '@whip/protocol';

export type DefinitionSummary = NonNullable<DefinitionList['items']>[number];

/** The definitions a host can run: built-ins first, then registered ids. Gated on the RPC being advertised. */
export function useDefinitions(client: WhipClient, enabled: boolean) {
  const runtimeId = client.getSnapshot().info?.runtime_id;
  const supported = enabled && client.supports('rpc', 'definitions.list');
  return { supported, query: useQuery({
    queryKey: ['definitions', runtimeId],
    queryFn: ({ signal }) => client.agents.list({ signal }),
    enabled: supported,
  }) };
}
export const definitionsQueryKey = (client: WhipClient) => ['definitions', client.getSnapshot().info?.runtime_id];

export function definitionOptions(items: readonly DefinitionSummary[] | null | undefined) {
  return (items ?? []).map(item => ({ value: item.id, label: item.built_in ? `${item.id} (built-in)` : item.id }));
}
