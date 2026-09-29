import { assertValid } from '@whip/protocol';
import type { ContentReference, Request, Response } from '@whip/protocol';
import { DeliveryError } from './wire.js';
import type { CallOptions, Transport } from './wire.js';
import { endpointURL, openBrowserConnection } from './browser-connection.js';
import { checkNetworkInitialize, networkInitialize } from './browser-identity.js';
import type { BrowserOptions } from './browser-identity.js';
export type { BrowserOptions } from './browser-identity.js';
export { checkNetworkInitialize, networkInitialize } from './browser-identity.js';
export { browserDuplex } from './browser-duplex.js';
export { discoverGateway } from './browser-discovery.js';

/** One connection per ordinary call. Every connection verifies the selected host.
 * Cancellation stops observation only; use explicit runtime operations to cancel work. */
export function browserSocket(endpoint: string, options: BrowserOptions): Transport {
  endpointURL(endpoint);
  const pinned = { ...options };
  networkInitialize({ major: 4 }, pinned);
  return async (request, expectedRuntimeID, callOptions) => {
    if (expectedRuntimeID !== undefined && expectedRuntimeID !== pinned.expectedRuntimeID) throw new TypeError('Runtime identity mismatch');
    assertValid('Request', request);
    request = structuredClone(request);
    const signal = callOptions.signal ? AbortSignal.any([callOptions.signal, AbortSignal.timeout(15_000)]) : AbortSignal.timeout(15_000);
    let pending: { id: string; resolve(value: Response): void; reject(error: unknown): void } | undefined;
    let failure: unknown;
    const connection = await openBrowserConnection(endpoint, {
      message(value) {
        assertValid('Response', value);
        if (!pending || pending.id !== value.id) throw new TypeError('Unexpected browser response identity');
        const current = pending; pending = undefined; current.resolve(value);
      },
      close(error) { failure = error; pending?.reject(error); pending = undefined; },
    }, signal);
    const invoke = (message: Request): Promise<Response> => new Promise((resolve, reject) => {
      if (failure !== undefined) { reject(failure); return; }
      pending = { id: message.id, resolve, reject };
      try { connection.send(JSON.stringify(message)); } catch (error) { pending = undefined; reject(error); }
    });
    try {
      if (request.method === 'initialize') {
        assertValid('InitializeParams', request.params);
        const response = await invoke({ ...request, params: networkInitialize(request.params, pinned) });
        checkNetworkInitialize(request.id, response, pinned);
        return response;
      }
      const initial = await invoke({ jsonrpc: '2.0', id: 'browser-initialize', method: 'initialize', params: networkInitialize({ major: 4 }, pinned) });
      checkNetworkInitialize('browser-initialize', initial, pinned);
      return await invoke(request);
    } finally { connection.close(); }
  };
}

const maxContentBytes = 4 << 20;

/** Scoped transfers use the same runtime content owner as RPC, capped at 4 MiB.
 * Keep the exact owner, reference and bytes after an uncertain upload. No retries. */
export function browserContent(endpoint: string, options: BrowserOptions) {
  const base = endpointURL(endpoint);
  base.protocol = base.protocol === 'wss:' ? 'https:' : 'http:';
  const pinned = { ...options };
  networkInitialize({ major: 4 }, pinned);
  const urlFor = (sessionID: string, referenceID: string) => {
    assertValid('ReadContentParams', { session_id: sessionID, reference_id: referenceID });
    const url = new URL('/api/v4/content/' + encodeURIComponent(referenceID), base);
    url.searchParams.set('runtime_id', pinned.expectedRuntimeID);
    url.searchParams.set('session_id', sessionID);
    if (pinned.expectedProcessEpoch) url.searchParams.set('expected_process_epoch', pinned.expectedProcessEpoch);
    return url;
  };
  const signalFor = (options: CallOptions) => options.signal ? AbortSignal.any([options.signal, AbortSignal.timeout(120_000)]) : AbortSignal.timeout(120_000);
  return {
    async upload(sessionID: string, referenceID: string, mediaType: string, bytes: Uint8Array, options: CallOptions = {}): Promise<ContentReference> {
      const signal = signalFor(options); signal.throwIfAborted();
      if (bytes.byteLength > maxContentBytes) throw new TypeError('Content exceeds 4 MiB');
      const data = new Uint8Array(bytes);
      const url = urlFor(sessionID, referenceID);
      const digest = await sha256(data);
      const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': mediaType, 'X-Content-SHA256': digest }, body: data, signal, redirect: 'error', credentials: 'same-origin' });
      if (!response.ok) { await response.body?.cancel(); throw new DeliveryError('Upload was not acknowledged; inspect the same owner and reference before retrying'); }
      const raw = await boundedBody(response, 64 << 10);
      const value: unknown = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(raw));
      assertValid('ContentReference', value);
      if (value.id !== referenceID || value.session_id !== sessionID || value.digest !== digest || value.size !== String(data.byteLength) || value.media_type !== mediaType) throw new TypeError('Content acknowledgement identity mismatch');
      return value;
    },
    async download(sessionID: string, referenceID: string, options: CallOptions = {}): Promise<Uint8Array> {
      const response = await fetch(urlFor(sessionID, referenceID), { signal: signalFor(options), redirect: 'error', credentials: 'same-origin' });
      if (!response.ok) { await response.body?.cancel(); throw new DeliveryError('Scoped content is unavailable'); }
      const length = response.headers.get('Content-Length');
      const expected = response.headers.get('X-Content-SHA256');
      if (!length || !/^(0|[1-9][0-9]*)$/.test(length) || Number(length) > maxContentBytes || !expected || !/^[a-f0-9]{64}$/.test(expected)) { await response.body?.cancel(); throw new TypeError('Invalid content metadata'); }
      const data = await boundedBody(response, Number(length));
      if (data.byteLength !== Number(length) || await sha256(data) !== expected) throw new TypeError('Content size or digest mismatch');
      return data;
    },
  };
}
async function boundedBody(response: globalThis.Response, maximum: number): Promise<Uint8Array<ArrayBuffer>> {
  const reader = response.body?.getReader();
  if (!reader) throw new TypeError('Missing response body');
  const parts: Uint8Array[] = []; let size = 0;
  try {
    for (;;) { const item = await reader.read(); if (item.done) break; size += item.value.byteLength; if (size > maximum) throw new TypeError('Content response exceeds limit'); parts.push(item.value); }
  } catch (error) { await reader.cancel().catch(() => {}); throw error; } finally { reader.releaseLock(); }
  const data = new Uint8Array(size); let offset = 0;
  for (const part of parts) { data.set(part, offset); offset += part.byteLength; }
  return data;
}
async function sha256(data: Uint8Array<ArrayBuffer>): Promise<string> {
  return [...new Uint8Array(await crypto.subtle.digest('SHA-256', data))].map(value => value.toString(16).padStart(2, '0')).join('');
}
