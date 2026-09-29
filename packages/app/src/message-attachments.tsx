import { useQuery } from '@tanstack/react-query';
import type { Client } from '@whip/sdk';
import type { DeepReadonly } from '@whip/sdk/state';
import { Button, Spinner } from '@whip/ui';
import { DesignInputAttachments } from './design-input-attachments';
import type { DesignContext } from './browser-design-presentation';
import { ErrorNotice } from './error-feedback';

/** Canonical parts name references, not inline bytes. Inspect only metadata here;
 * the individual preview owns its scoped, cancellable byte read. */
export function MessageAttachments({ references, designContext, client, rootId, agentId, connected }: {
  references: readonly string[];
  designContext?: DeepReadonly<DesignContext> | null;
  client: Client; rootId: string; agentId: string; connected: boolean;
}) {
  const query = useQuery({
    queryKey: ['message-attachments', client.runtimeID, agentId, references],
    enabled: connected && references.length > 0, staleTime: Infinity, gcTime: 0, retry: false, refetchOnWindowFocus: false,
    queryFn: async ({ signal }) => {
      if (references.length > 128) throw new RangeError('Message has too many attachments');
      const unique = [...new Set(references)];
      const files = [];
      for (let offset = 0; offset < unique.length; offset += 4) {
        files.push(...await Promise.all(unique.slice(offset, offset + 4).map(id => client.session(agentId).content.get(id, { signal }))));
      }
      return files;
    },
  });
  if (query.error) return <ErrorNotice type="resource" owner={`${agentId}:attachments`} error={query.error}
    action={<Button disabled={!connected} onClick={() => void query.refetch()}>Retry attachments</Button>} />;
  if (!query.data) return connected ? <Spinner label="Loading attachments" /> : <p>Reconnect to load attachments.</p>;
  return <DesignInputAttachments files={query.data} designContext={designContext} client={client} rootId={rootId}
    agentId={agentId} runtimeId={client.runtimeID} connected={connected} />;
}
