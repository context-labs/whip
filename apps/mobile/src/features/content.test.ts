/// <reference types="node" />
/** @jest-environment node */
import { createHash } from 'node:crypto';
import type { Client, ContentReference } from '@whip/sdk';
import { readMobileContent } from './content';
jest.mock('expo-crypto', () => ({ CryptoDigestAlgorithm: { SHA256: 'sha256' }, digest: async (_: string, bytes: Uint8Array) => { const result = require('node:crypto').createHash('sha256').update(bytes).digest(); return result.buffer.slice(result.byteOffset, result.byteOffset + result.byteLength); } }));
const originalFetch = globalThis.fetch;
afterEach(() => { globalThis.fetch = originalFetch; jest.restoreAllMocks(); });
function fixture(text = '😀 retained text') {
  const bytes = new TextEncoder().encode(text), digest = createHash('sha256').update(bytes).digest('hex');
  const ref: ContentReference = { id: 'ref', session_id: 'child', size: String(bytes.length), digest, media_type: 'text/plain', created_at: '' };
  const get = jest.fn(async () => ref);
  const client = { runtimeID: 'runtime', processEpoch: 'boot', session: jest.fn(() => ({ content: { get } })) } as unknown as Client;
  globalThis.fetch = jest.fn(async () => new Response(bytes, { headers: { 'Content-Length': String(bytes.length), 'X-Content-SHA256': digest } }));
  return { bytes, digest, ref, get, client };
}
test('explicit attachment read checks child owner, runtime, epoch, exact digest and UTF-8', async () => {
  const f = fixture(); expect(await readMobileContent(f.client, 'https://host.example', 'child', 'ref', new AbortController().signal)).toBe('😀 retained text');
  expect(f.client.session).toHaveBeenCalledWith('child'); expect(f.get).toHaveBeenCalledWith('ref', expect.anything());
  const call = (globalThis.fetch as jest.Mock).mock.calls[0]; const url = call[0] as URL;
  expect(url.pathname).toBe('/api/v4/content/ref'); expect(Object.fromEntries(url.searchParams)).toEqual({ runtime_id: 'runtime', expected_process_epoch: 'boot', session_id: 'child' }); expect(call[1]).toMatchObject({ redirect: 'error', credentials: 'omit' });
});
test('oversized, other-owner and nontext metadata prevents every body request', async () => {
  for (const patch of [{ size: '9007199254740993' }, { session_id: 'sibling' }, { media_type: 'image/png' }]) { const f = fixture(); Object.assign(f.ref, patch); await expect(readMobileContent(f.client, 'https://host.example', 'child', 'ref', new AbortController().signal)).rejects.toThrow(); expect(globalThis.fetch).not.toHaveBeenCalled(); }
});
test('incorrect headers stop before reader allocation; bounded stream rejects a lying server and cancels', async () => {
  const f = fixture(); const cancel = jest.fn();
  globalThis.fetch = jest.fn(async () => ({ ok: true, headers: new Headers({ 'Content-Length': f.ref.size, 'X-Content-SHA256': 'b'.repeat(64) }), body: { cancel } }) as unknown as Response);
  await expect(readMobileContent(f.client, 'https://host.example', 'child', f.ref, new AbortController().signal)).rejects.toThrow('metadata'); expect(cancel).toHaveBeenCalled();
  const streamCancel = jest.fn(); globalThis.fetch = jest.fn(async () => new Response(new ReadableStream({ start(controller) { controller.enqueue(new Uint8Array(256 << 10)); }, cancel: streamCancel }), { headers: { 'Content-Length': f.ref.size, 'X-Content-SHA256': f.digest } }));
  await expect(readMobileContent(f.client, 'https://host.example', 'child', f.ref, new AbortController().signal)).rejects.toThrow('declared size'); expect(streamCancel).toHaveBeenCalled();
});
test('wrong bytes with matching size never render; pre-cancelled observation does not read', async () => {
  const f = fixture('abc'); globalThis.fetch = jest.fn(async () => new Response('def', { headers: { 'Content-Length': '3', 'X-Content-SHA256': f.digest } }));
  await expect(readMobileContent(f.client, 'https://host.example', 'child', f.ref, new AbortController().signal)).rejects.toThrow('digest');
  const abort = new AbortController(); abort.abort(); await expect(readMobileContent(f.client, 'https://host.example', 'child', f.ref, abort.signal)).rejects.toThrow();
});
