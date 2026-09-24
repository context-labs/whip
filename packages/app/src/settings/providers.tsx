import type { WhipClient } from '@whip/sdk';
import { ProviderConnections } from './provider-connections';

export function ProvidersSettings({
  client,
  enabled = client.getSnapshot().state === 'connected',
}: {
  client: WhipClient;
  enabled?: boolean;
}) {
  const info = client.getSnapshot().info;
  return <ProviderConnections
    key={`${info?.runtime_id}:${info?.connection_id}`} client={client} enabled={enabled} />;
}
