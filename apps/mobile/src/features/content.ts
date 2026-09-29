import * as Crypto from 'expo-crypto';
import type { Client, ContentReference } from '@whip/sdk';
const maximum = 256 << 10;
/** Explicit scoped content read. Metadata and stream bounds precede allocation;
 * Expo's native digest avoids depending on browser-only SubtleCrypto. */
export async function readMobileContent(client: Client, endpoint: string, sessionId: string, reference: string | ContentReference, signal: AbortSignal): Promise<string> {
  signal = AbortSignal.any([signal, AbortSignal.timeout(30_000)]);
  signal.throwIfAborted();
  const expected = typeof reference === 'string' ? await client.session(sessionId).content.get(reference, { signal }) : reference;
  if (expected.session_id !== sessionId) throw new Error('Content belongs to another recipient.');
  if (BigInt(expected.size) > BigInt(maximum)) throw new Error('Content exceeds the 256 KiB mobile inspection limit.');
  if (!expected.media_type.startsWith('text/') && !['application/json', 'application/javascript'].includes(expected.media_type)) throw new Error('This attachment is not text. Open it in the host app.');
  const url = new URL('/api/v4/content/' + encodeURIComponent(expected.id), endpoint);
  url.protocol = url.protocol === 'wss:' ? 'https:' : url.protocol === 'ws:' ? 'http:' : url.protocol;
  url.searchParams.set('runtime_id', client.runtimeID); url.searchParams.set('expected_process_epoch', client.processEpoch); url.searchParams.set('session_id', sessionId);
  const response = await fetch(url, { signal, redirect: 'error', credentials: 'omit' });
  if (!response.ok || response.headers.get('X-Content-SHA256') !== expected.digest || response.headers.get('Content-Length') !== expected.size) { await response.body?.cancel(); throw new Error('Scoped content metadata changed or is unavailable.'); }
  const reader = response.body?.getReader(); if (!reader) throw new Error('Content streaming is unavailable.');
  const parts: Uint8Array[] = []; let size = 0;
  try {
    for (;;) { signal.throwIfAborted(); const next = await reader.read(); if (next.done) break; size += next.value.byteLength; if (size > maximum || BigInt(size) > BigInt(expected.size)) throw new Error('Content exceeds its declared size.'); parts.push(next.value); }
  } catch (error) { await reader.cancel().catch(() => {}); throw error; } finally { reader.releaseLock(); }
  if (String(size) !== expected.size) throw new Error('Content size mismatch.');
  const bytes = new Uint8Array(size); let offset = 0; for (const part of parts) { bytes.set(part, offset); offset += part.length; }
  const digest = [...new Uint8Array(await Crypto.digest(Crypto.CryptoDigestAlgorithm.SHA256, bytes))].map(value => value.toString(16).padStart(2, '0')).join('');
  signal.throwIfAborted(); if (digest !== expected.digest) throw new Error('Content digest mismatch.');
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
}
