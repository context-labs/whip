import { assertValid } from '@whip/protocol';
import type { BrowserEvent, Request, Response } from '@whip/protocol';
import type { DuplexTransport } from './executors.js';
import type { FramedConnection, FramedConnector } from './framed.js';
import { callSignal, decodeResponse, DeliveryError } from './wire.js';
import type { CallOptions } from './wire.js';

const maxBytes = 8 << 20;
const encoder = new TextEncoder();

/** An explicitly opened confined native browser peer. The selected host's
 * existing connector is borrowed; this never prepares or starts a runtime. */
export async function browserProviderFramed(open: FramedConnector, options: { expectedRuntimeID: string; expectedProcessEpoch: string } & CallOptions): Promise<DuplexTransport<BrowserEvent>> {
  const pinned = { expected_runtime_id: options.expectedRuntimeID, expected_process_epoch: options.expectedProcessEpoch };
  assertValid('InitializeParams', { major: 4, ...pinned });
  const timeout = options.timeoutMs ?? 15_000;
  if (!Number.isSafeInteger(timeout) || timeout < 1 || timeout > 180_000) throw new RangeError('timeoutMs must be within 1..180000');
  options.signal?.throwIfAborted();
  const connectDeadline = new AbortController();
  const timer = setTimeout(() => connectDeadline.abort(new DeliveryError('Native browser connection timed out')), timeout);
  const connecting = options.signal ? AbortSignal.any([options.signal, connectDeadline.signal]) : connectDeadline.signal;
  let connection: FramedConnection | undefined;
  let ended = false, consumed = false, initialized = false, initializing = false;
  let failure: unknown;
  let openingReject: ((error: unknown) => void) | undefined;
  let queuedBytes = 0;
  const queue: { value: BrowserEvent; bytes: number }[] = [];
  const requests = new Map<string, { initialize: boolean; resolve(value: Response): void; reject(error: unknown): void; cleanup(): void }>();
  let waiting: { resolve(value: IteratorResult<BrowserEvent>): void; reject(error: unknown): void } | undefined;
  const finish = (reason?: unknown) => {
    if (ended) return;
    ended = true; failure = reason;
    options.signal?.removeEventListener('abort', abortLifetime);
    queue.length = 0; queuedBytes = 0;
    const error = reason ?? new DeliveryError('Browser provider closed; delivery may be unknown');
    openingReject?.(error);
    for (const pending of requests.values()) { pending.cleanup(); pending.reject(error); }
    requests.clear();
    if (waiting) {
      if (reason !== undefined) waiting.reject(reason);
      else waiting.resolve({ done: true, value: undefined });
      waiting = undefined;
    }
    const current = connection; connection = undefined;
    try { current?.close(); } catch { /* Closing must not erase an acknowledgement. */ }
  };
  const abortLifetime = () => finish(options.signal?.reason);
  const abortConnecting = () => finish(connecting.reason);
  options.signal?.addEventListener('abort', abortLifetime, { once: true });
  connecting.addEventListener('abort', abortConnecting, { once: true });
  try {
    const opened = open({
      message(raw) {
        if (ended) return;
        try {
          if (typeof raw !== 'string') throw new TypeError('Native browser frame must be text');
          const bytes = encoder.encode(raw).length + 1;
          if (bytes > maxBytes) throw new RangeError('Native browser frame exceeds limit');
          const value: unknown = JSON.parse(raw);
          if (value !== null && typeof value === 'object' && 'id' in value) {
            assertValid('Response', value);
            const pending = requests.get(value.id);
            if (!pending) throw new TypeError('Native browser response has no pending request');
            if (pending.initialize) {
              const initial = decodeResponse('initialize', value.id, value);
              if (initial.runtime_id !== pinned.expected_runtime_id || initial.process_epoch !== pinned.expected_process_epoch || initial.network_client) throw new TypeError('Native browser runtime identity, process generation or transport mismatch');
              initialized = true;
            }
            requests.delete(value.id); pending.cleanup(); pending.resolve(value);
          } else {
            if (!initialized) throw new TypeError('Native browser event preceded initialization');
            assertValid('BrowserEvent', value);
            if (waiting) { waiting.resolve({ done: false, value }); waiting = undefined; }
            else {
              if (queue.length >= 32 || bytes > maxBytes - queuedBytes) throw new RangeError('Native browser event queue exceeds limit');
              queue.push({ value, bytes }); queuedBytes += bytes;
            }
          }
        } catch (error) { finish(error); }
      },
      close(error) { finish(new DeliveryError('Native browser connection closed; delivery may be unknown', { cause: error })); },
    }, connecting, 'browser-provider');
    connection = await new Promise<FramedConnection>((resolve, reject) => {
      openingReject = reject;
      opened.then(value => {
        if (ended || connecting.aborted) {
          try { value.close(); } catch { /* Retire an abandoned late connector. */ }
          reject(failure ?? connecting.reason);
        } else resolve(value);
      }, reject);
      if (ended || connecting.aborted) reject(failure ?? connecting.reason);
    });
    openingReject = undefined;
    if (ended || connecting.aborted) {
      const late = connection; connection = undefined;
      try { late.close(); } catch { /* Opening and cancellation can settle together. */ }
      throw failure ?? connecting.reason;
    }
    if (connection.kind !== 'unix') throw new TypeError('Native browser peer requires a confined Unix connection');
  } catch (error) { finish(error); throw error; }
  finally { clearTimeout(timer); connecting.removeEventListener('abort', abortConnecting); }

  return {
    request(request: Request, callOptions: CallOptions = {}): Promise<Response> {
      if (ended) return Promise.reject(failure ?? new DeliveryError('Native browser peer is closed'));
      assertValid('Request', request);
      request = structuredClone(request);
      const initialize = request.method === 'initialize';
      if (initialize) {
        if (initialized || initializing) return Promise.reject(new TypeError('Native browser peer already initialized'));
        assertValid('InitializeParams', request.params);
        if (request.params.network_client || request.params.expected_runtime_id !== pinned.expected_runtime_id || request.params.expected_process_epoch !== pinned.expected_process_epoch) return Promise.reject(new TypeError('Native browser initialization identity mismatch'));
      } else if (!initialized) return Promise.reject(new TypeError('Native browser peer requires initialization'));
      if (requests.size >= 32 || requests.has(request.id)) return Promise.reject(new TypeError('Native browser request capacity or identity conflict'));
      const raw = JSON.stringify(request), bytes = encoder.encode(raw).length + 1;
      if (bytes > maxBytes || !Number.isSafeInteger(connection!.bufferedAmount) || connection!.bufferedAmount < 0 || bytes > maxBytes - connection!.bufferedAmount) return Promise.reject(new RangeError('Native browser write exceeds frame or queue limit'));
      const signal = callSignal(callOptions);
      if (initialize) initializing = true;
      return new Promise((resolve, reject) => {
        const abort = () => finish(signal.reason);
        signal.addEventListener('abort', abort, { once: true });
        requests.set(request.id, { initialize, resolve, reject, cleanup: () => signal.removeEventListener('abort', abort) });
        if (signal.aborted) { abort(); return; }
        try { connection!.send(raw); } catch (error) { finish(error); }
      });
    },
    events: {
      [Symbol.asyncIterator]() {
        if (consumed) throw new TypeError('Native browser events already consumed');
        consumed = true;
        return {
          next(): Promise<IteratorResult<BrowserEvent>> {
            if (waiting) return Promise.reject(new TypeError('Concurrent native browser event reads are unsupported'));
            if (ended) return failure === undefined ? Promise.resolve({ done: true, value: undefined }) : Promise.reject(failure);
            const item = queue.shift();
            if (item) { queuedBytes -= item.bytes; return Promise.resolve({ done: false, value: item.value }); }
            return new Promise((resolve, reject) => { waiting = { resolve, reject }; });
          },
          async return(): Promise<IteratorResult<BrowserEvent>> { finish(); return { done: true, value: undefined }; },
        };
      },
    },
    async close() { finish(); },
  };
}
