import { assertValid, type ContentHandle } from '@whip/protocol';
import type { CallOptions, WhipClient } from './client.js';
import { WhipError } from './errors.js';
import { decodeBase64, digestHex, encodeBase64, frozen, uuid } from './util.js';

export interface ContentScope { rootId: string; agentId?: string }
export interface ReadContentOptions extends CallOptions { maxBytes: number }
export interface UploadOptions extends ContentScope, CallOptions { mediaType?: string; source?: string }

/** Immutable scoped reference. Constructing a reference never fetches its body. */
export class ContentReference {
  readonly handle: Readonly<ContentHandle>;
  readonly scope: Readonly<ContentScope>;
  constructor(private readonly client: WhipClient, handle: ContentHandle, scope: ContentScope) {
    assertValid('ContentHandle', handle, 'response');
    if (!scope.rootId || BigInt(handle.size) < 0n || !/^[a-f0-9]{64}$/.test(handle.digest)) throw new TypeError('Invalid content scope, size or digest');
    this.handle = frozen({ ...handle });
    this.scope = frozen({ rootId: scope.rootId, ...(scope.agentId ? { agentId: scope.agentId } : {}) });
  }
  async readBytes(options: ReadContentOptions): Promise<Uint8Array<ArrayBuffer>> {
    options.signal?.throwIfAborted();
    if (!Number.isSafeInteger(options.maxBytes) || options.maxBytes < 0 || BigInt(this.handle.size) > BigInt(options.maxBytes)) throw new WhipError('resource_limit', 'Content exceeds the requested byte limit');
    const signal = transferSignal(this.client, options);
    const size = Number(this.handle.size); // Checked against the caller's safe integer bound.
    const bytes = new Uint8Array(size);
    if (this.client.transportKind === 'websocket') {
      const url = new URL(`/api/v3/content/${encodeURIComponent(this.handle.reference_id)}`, this.client.httpEndpoint);
      url.searchParams.set('root_id', this.scope.rootId);
      if (this.scope.agentId) url.searchParams.set('agent_id', this.scope.agentId);
      const response = await fetch(url, { signal, cache: 'no-store' });
      if (!response.ok) throw httpError(response.status);
      const length = response.headers.get('Content-Length');
      if (length !== null && length !== this.handle.size) { await response.body?.cancel(); throw new WhipError('invalid_response', 'Content length changed'); }
      const reader = response.body?.getReader();
      let offset = 0;
      if (reader) {
        try {
          for (;;) {
            const { value, done } = await reader.read();
            if (done) break;
            if (offset + value.byteLength > size) throw new WhipError('resource_limit', 'Downloaded content exceeds its declared size');
            bytes.set(value, offset); offset += value.byteLength;
          }
        } finally { await reader.cancel(); reader.releaseLock(); }
      }
      if (offset !== size) throw new WhipError('unavailable_capability', 'Content transfer was interrupted');
    } else {
      const limit = this.client.requireConnected().limits.content_chunk_bytes;
      // Even an empty body must validate its root/agent/reference grant.
      for (let offset = 0; offset < size || offset === 0;) {
        const result = await this.client.call('content.read', {
          root_id: this.scope.rootId, ...(this.scope.agentId ? { agent_id: this.scope.agentId } : {}),
          reference_id: this.handle.reference_id, offset: String(offset), limit: Math.max(1, Math.min(limit, size - offset)),
        }, { ...options, signal });
        if (result.content.reference_id !== this.handle.reference_id || result.content.digest !== this.handle.digest || result.content.size !== this.handle.size) throw new WhipError('invalid_response', 'Content reference changed during transfer');
        const chunk = decodeBase64(result.data ?? '');
        if (size === 0 && chunk.byteLength === 0) break;
        if (!chunk.byteLength || chunk.byteLength > Math.min(limit, size - offset)) throw new WhipError('invalid_response', 'Invalid content chunk length');
        bytes.set(chunk, offset); offset += chunk.byteLength;
      }
    }
    if (await digestHex(bytes) !== this.handle.digest) throw new WhipError('invalid_response', 'Content digest mismatch');
    return bytes;
  }
  async readText(options: ReadContentOptions): Promise<string> {
    return new TextDecoder('utf-8', { fatal: true }).decode(await this.readBytes(options));
  }
  async readJSON(options: ReadContentOptions): Promise<unknown> { return JSON.parse(await this.readText(options)); }
}

function transferSignal(client: WhipClient, options: CallOptions): AbortSignal {
  return AbortSignal.any([client.lifetimeSignal, AbortSignal.timeout(options.timeoutMs ?? 30_000), ...(options.signal ? [options.signal] : [])]);
}
function httpError(status: number): WhipError {
  return new WhipError(status === 403 ? 'permission_denied' : status === 413 ? 'resource_limit' : 'unavailable_capability', `Content transfer failed (HTTP ${status})`);
}
export async function upload(client: WhipClient, input: Uint8Array<ArrayBuffer>, options: UploadOptions): Promise<ContentReference> {
  options.signal?.throwIfAborted();
  const info = client.requireConnected();
  if (!options.rootId || options.agentId && options.agentId !== options.rootId) throw new WhipError('invalid_arguments', 'Uploads are granted to the root; child grants must be issued by the daemon');
  if (BigInt(input.byteLength) > BigInt(info.limits.upload_bytes)) throw new WhipError('resource_limit', 'Upload exceeds the daemon limit');
  const signal = transferSignal(client, options);
  // Snapshot once: the digest and transmitted bytes must describe the same input.
  const bytes = input.slice();
  const digest = await digestHex(bytes);
  signal.throwIfAborted();
  let handle: ContentHandle;
  if (client.transportKind === 'websocket') {
    const url = new URL('/api/v3/content/upload', client.httpEndpoint);
    url.searchParams.set('root_id', options.rootId);
    const response = await fetch(url, { method: 'POST', signal, headers: { 'Content-Type': options.mediaType ?? 'application/octet-stream', 'X-Content-SHA256': digest }, body: bytes });
    if (!response.ok) throw httpError(response.status);
    // A handle is small, but still bound the response independently of the upload.
    const reader = response.body?.getReader();
    const chunks: Uint8Array[] = []; let size = 0;
    if (!reader) throw new WhipError('invalid_response', 'Upload response is missing');
    try {
      for (;;) {
        const { done, value } = await reader.read(); if (done) break;
        size += value.byteLength; if (size > 64 << 10) throw new WhipError('resource_limit', 'Upload response exceeds limit');
        chunks.push(value);
      }
    } finally { await reader.cancel(); reader.releaseLock(); }
    const data = new Uint8Array(size); let offset = 0;
    for (const chunk of chunks) { data.set(chunk, offset); offset += chunk.length; }
    const value: unknown = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(data));
    assertValid('ContentHandle', value, 'response'); handle = value;
  } else {
    const id = uuid();
    const epoch = info.connection_id;
    const check = () => { signal.throwIfAborted(); if (client.requireConnected().connection_id !== epoch) throw new WhipError('disconnected', 'Upload connection changed; begin a new upload explicitly'); };
    check();
    await client.call('upload.begin', { upload_id: id, root_id: options.rootId, expected_digest: digest, size: String(bytes.byteLength), media_type: options.mediaType, source: options.source }, { signal });
    for (let offset = 0; offset < bytes.length; offset += info.limits.content_chunk_bytes) {
      check();
      await client.call('upload.chunk', { upload_id: id, offset: String(offset), data: encodeBase64(bytes.subarray(offset, offset + info.limits.content_chunk_bytes)) }, { signal });
    }
    check(); handle = await client.call('upload.finish', { upload_id: id }, { signal });
  }
  if (handle.digest !== digest || handle.size !== String(bytes.byteLength)) throw new WhipError('invalid_response', 'Uploaded content digest or size mismatch');
  return new ContentReference(client, handle, options);
}
