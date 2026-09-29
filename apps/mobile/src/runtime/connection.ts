import { Client, RemoteError } from '@whip/sdk';
import { browserSocket, discoverGateway } from '@whip/sdk/browser';
import type { SavedHost } from './runtime';

/** One verified, foreground observation lifetime. Each ordinary request opens
 * its own pinned socket; reconnecting never replays an application mutation. */
export async function connectMobile(host: SavedHost, signal: AbortSignal): Promise<Client> {
  const found = await discoverGateway(host.url, { signal });
  if (host.runtimeId && found.runtime_id !== host.runtimeId) throw new RemoteError({ code: -32000, kind: 'IDENTITY', message: 'This address now serves another runtime. Verify it before replacing its saved host entry. Drafts and delivery metadata have been retained.' });
  const transport = browserSocket(host.url, { expectedRuntimeID: found.runtime_id, expectedProcessEpoch: found.process_epoch });
  return Client.connect((request, expected, options) => transport(request, expected, {
    ...options, signal: options.signal ? AbortSignal.any([signal, options.signal]) : signal,
  }), { clientID: host.clientId, expectedRuntimeID: found.runtime_id, signal });
}
