import { abortError, WhipError } from './errors.js';

export const utf8 = new TextEncoder();
export const byteLength = (text: string): number => utf8.encode(text).byteLength;
export function object(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}
export function frozen<T>(value: T): T {
  if (value !== null && typeof value === 'object' && !Object.isFrozen(value)) {
    Object.freeze(value);
    for (const child of Object.values(value)) frozen(child);
  }
  return value;
}
export function withSignal<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise;
  if (signal.aborted) {
    void promise.catch(() => {});
    return Promise.reject(abortError(signal));
  }
  return new Promise((resolve, reject) => {
    const abort = () => { cleanup(); reject(abortError(signal)); };
    const cleanup = () => signal.removeEventListener('abort', abort);
    signal.addEventListener('abort', abort, { once: true });
    promise.then(value => { cleanup(); resolve(value); }, error => { cleanup(); reject(error); });
  });
}
export function decodeBase64(value: string): Uint8Array<ArrayBuffer> {
  try { return Uint8Array.from(atob(value), char => char.charCodeAt(0)); }
  catch { throw new WhipError('invalid_response', 'Invalid base64 data'); }
}
export function encodeBase64(bytes: Uint8Array): string {
  let text = '';
  for (let start = 0; start < bytes.length; start += 8192) {
    text += String.fromCharCode(...bytes.subarray(start, start + 8192));
  }
  return btoa(text);
}
export function subtle(): SubtleCrypto {
  if (!globalThis.crypto?.subtle) throw new WhipError('unavailable_capability', 'WebCrypto is unavailable. Use a secure browser context or supply a signing implementation.');
  return crypto.subtle;
}
export async function sha256(bytes: Uint8Array<ArrayBuffer>): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await subtle().digest('SHA-256', bytes));
}
export async function digestHex(bytes: Uint8Array<ArrayBuffer>): Promise<string> {
  return Array.from(await sha256(bytes), byte => byte.toString(16).padStart(2, '0')).join('');
}
export function uuid(): string {
  if (!globalThis.crypto?.randomUUID) throw new WhipError('unavailable_capability', 'A secure context with crypto.randomUUID is required to create command identities.');
  return crypto.randomUUID();
}
export function notify<T>(listeners: Set<(value: T) => void>, value: T): void {
  for (const listener of [...listeners]) {
    try { listener(value); }
    catch (error) { queueMicrotask(() => { throw error; }); }
  }
}
