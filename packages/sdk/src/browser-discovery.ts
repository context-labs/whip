import { assertValid } from '@whip/protocol';
import type { GatewayDiscovery } from '@whip/protocol';
import { endpointURL } from './browser-connection.js';
import type { CallOptions } from './wire.js';

/** Read public gateway identity before an explicit first connection. Discovery
 * never trusts a changed saved identity, mutates storage, or starts execution. */
export async function discoverGateway(endpoint: string, options: CallOptions & { expectedRuntimeID?: string } = {}): Promise<Readonly<GatewayDiscovery>> {
  const url = endpointURL(endpoint);
  url.protocol = url.protocol === 'wss:' ? 'https:' : 'http:';
  url.pathname = '/api/v4/web';
  const signal = options.signal ? AbortSignal.any([options.signal, AbortSignal.timeout(5000)]) : AbortSignal.timeout(5000);
  signal.throwIfAborted();
  const response = await fetch(url, { signal, redirect: 'error', credentials: 'omit', cache: 'no-store' });
  const reader = response.body?.getReader();
  if (!reader) throw new TypeError('Gateway discovery body is unavailable');
  try {
    if (!response.ok) throw new Error(`Gateway discovery failed (${response.status})`);
    if (response.headers.get('content-type')?.split(';')[0]?.trim() !== 'application/json') throw new TypeError('Expected gateway JSON metadata');
    const declared = response.headers.get('content-length');
    if (declared !== null && (!/^(0|[1-9][0-9]*)$/.test(declared) || Number(declared) > 4096)) throw new RangeError('Gateway metadata exceeds 4096 bytes');
    const buffer = new Uint8Array(4096);
    let size = 0;
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      if (value.length > buffer.length - size) throw new RangeError('Gateway metadata exceeds 4096 bytes');
      buffer.set(value, size); size += value.length;
    }
    if (declared !== null && Number(declared) !== size) throw new TypeError('Gateway metadata length mismatch');
    const result: unknown = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(buffer.subarray(0, size)));
    assertValid('GatewayDiscovery', result);
    if (options.expectedRuntimeID !== undefined && result.runtime_id !== options.expectedRuntimeID) throw new TypeError('This address serves a different runtime; explicitly accept its new identity to reconnect');
    return Object.freeze(result);
  } finally { await reader.cancel().catch(() => {}); }
}
