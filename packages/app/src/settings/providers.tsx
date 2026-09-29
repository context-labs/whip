import type { Client } from '@whip/sdk';
import { ProviderConnections } from './provider-connections';
export function ProvidersSettings({ client, enabled = true }: { client: Client; enabled?: boolean }) {
  return <ProviderConnections key={client.runtimeID} client={client} enabled={enabled} />;
}
