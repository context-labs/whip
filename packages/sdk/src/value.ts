export type DeepReadonly<T> = T extends (...args: never[]) => unknown ? T : T extends object ? { readonly [K in keyof T]: DeepReadonly<T[K]> } : T;
export function freeze<T>(value: T): DeepReadonly<T> {
  if (value && typeof value === 'object' && !Object.isFrozen(value)) {
    for (const item of Object.values(value)) freeze(item);
    Object.freeze(value);
  }
  return value as DeepReadonly<T>;
}
export const bytes = (value: unknown) => new TextEncoder().encode(JSON.stringify(value)).byteLength;
export function boundedInteger(value: number, name: string, maximum: number): number {
  if (!Number.isSafeInteger(value) || value < 1 || value > maximum) throw new RangeError(`${name} must be within 1..${maximum}`);
  return value;
}
export function delay(milliseconds: number, signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted();
  return new Promise((resolve, reject) => {
    const cleanup = () => { clearTimeout(timer); signal?.removeEventListener('abort', abort); };
    const abort = () => { cleanup(); reject(signal?.reason); };
    const timer = setTimeout(() => { cleanup(); resolve(); }, milliseconds);
    signal?.addEventListener('abort', abort, { once: true });
  });
}
/** Cancels this observer without cancelling a shared send or another waiter. */
export function withSignal<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise;
  signal.throwIfAborted();
  return new Promise((resolve, reject) => {
    const abort = () => { cleanup(); reject(signal.reason); };
    const cleanup = () => signal.removeEventListener('abort', abort);
    signal.addEventListener('abort', abort, { once: true });
    promise.then(value => { cleanup(); resolve(value); }, error => { cleanup(); reject(error); });
  });
}

/** Encode UTF-8 without Node globals or an argument stack proportional to input. */
export function utf8Base64(value: string): string {
  return bytesBase64(new TextEncoder().encode(value));
}
export function bytesBase64(encoded: Uint8Array): string {
  // Modern browsers encode the view directly, without a temporary binary string.
  // Keep the portable path for supported Node and native mobile runtimes.
  if ('toBase64' in encoded && typeof encoded.toBase64 === 'function') return encoded.toBase64();
  let binary = '';
  for (let offset = 0; offset < encoded.length; offset += 8192) binary += String.fromCharCode(...encoded.subarray(offset, offset + 8192));
  return btoa(binary);
}
