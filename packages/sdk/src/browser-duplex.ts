import { callSignal } from './wire.js';
import { assertValid } from '@whip/protocol';
import type { ExecutorEvent, Request, Response } from '@whip/protocol';
import { maxFrameBytes, openBrowserConnection } from './browser-connection.js';
import type { BrowserConnection } from './browser-connection.js';
import { checkNetworkInitialize, networkInitialize } from './browser-identity.js';
import type { BrowserOptions } from './browser-identity.js';
import type { DuplexTransport } from './executors.js';
import { DeliveryError } from './wire.js';
import type { CallOptions } from './wire.js';

const maxEvents = 32;
const maxRequests = 32;

/** One persistent browser peer for ExecutorClient. Initialization verifies the
 * selected runtime and network acknowledgement before dependent calls. Aborting
 * a request closes its connection-owned leases, never accepted session work.
 * No reconnect, rebind or invocation replay occurs. */
export async function browserDuplex(endpoint: string, options: BrowserOptions & CallOptions): Promise<DuplexTransport> {
  const pinned = { expectedRuntimeID: options.expectedRuntimeID, expectedProcessEpoch: options.expectedProcessEpoch };
  networkInitialize({ major: 4 }, pinned);
  options.signal?.throwIfAborted();
  let connection: BrowserConnection | undefined;
  let ended = false;
  let failure: unknown;
  let consumed = false;
  let initialized = false;
  let initializing = false;
  let queuedBytes = 0;
  const queue: { value: ExecutorEvent; bytes: number }[] = [];
  const requests = new Map<string, { initialize: boolean; resolve(value: Response): void; reject(error: unknown): void; cleanup(): void }>();
  let waiting: { resolve(value: IteratorResult<ExecutorEvent>): void; reject(error: unknown): void } | undefined;
  const finish = (reason?: unknown) => {
    if (ended) return;
    ended = true;
    failure = reason;
    queue.length = 0;
    queuedBytes = 0;
    for (const request of requests.values()) {
      request.cleanup();
      request.reject(reason ?? new DeliveryError('Executor connection closed; delivery may be unknown'));
    }
    requests.clear();
    if (waiting) {
      if (reason !== undefined) waiting.reject(reason);
      else waiting.resolve({ done: true, value: undefined });
      waiting = undefined;
    }
    connection?.close();
  };
  // Only the connection establishment has this timer. It must not expire the
  // persistent peer after a successful connection.
  const connecting = new AbortController();
  const timer = setTimeout(() => connecting.abort(new DeliveryError('Browser connection timed out')), 15_000);
  const lifetime = options.signal ? AbortSignal.any([options.signal, connecting.signal]) : connecting.signal;
  try {
    connection = await openBrowserConnection(endpoint, {
      close: finish,
      message(value, bytes) {
        if (ended) return;
        if (value !== null && typeof value === 'object' && 'id' in value) {
          assertValid('Response', value);
          const request = requests.get(value.id);
          if (!request) throw new TypeError('Executor response has no pending request');
          // Keep the request in the map until verification succeeds so a bad
          // handshake rejects its waiter together with the whole connection.
          if (request.initialize) {
            checkNetworkInitialize(value.id, value, pinned);
            initialized = true;
          }
          requests.delete(value.id);
          request.cleanup();
          request.resolve(value);
        } else {
          if (!initialized) throw new TypeError('Executor event preceded initialization');
          assertValid('ExecutorEvent', value);
          if (waiting) {
            waiting.resolve({ done: false, value });
            waiting = undefined;
          } else {
            if (queue.length >= maxEvents || bytes > maxFrameBytes - queuedBytes) throw new TypeError('Executor event queue exceeds limit');
            queue.push({ value, bytes });
            queuedBytes += bytes;
          }
        }
      },
    }, lifetime);
  } finally { clearTimeout(timer); }
  if (ended) { connection.close(); throw failure ?? new DeliveryError('Browser connection closed'); }

  return {
    request(request: Request, callOptions: CallOptions = {}): Promise<Response> {
      callOptions.signal?.throwIfAborted();
      if (ended) return Promise.reject(failure ?? new DeliveryError('Executor connection is closed'));
      assertValid('Request', request);
      request = structuredClone(request);
      const initialize = request.method === 'initialize';
      if (initialize) {
        if (initializing || initialized) return Promise.reject(new TypeError('Executor connection already initialized'));
        assertValid('InitializeParams', request.params);
        request.params = networkInitialize(request.params, pinned);
      } else if (!initialized) {
        return Promise.reject(new TypeError('Executor connection requires initialization'));
      }
      if (requests.size >= maxRequests || requests.has(request.id)) return Promise.reject(new TypeError('Executor request capacity or identity conflict'));
      const signal = callSignal(callOptions);
      if (initialize) initializing = true;
      return new Promise<Response>((resolve, reject) => {
        const abort = () => finish(signal.reason);
        signal.addEventListener('abort', abort, { once: true });
        requests.set(request.id, { initialize, resolve, reject, cleanup: () => signal.removeEventListener('abort', abort) });
        if (signal.aborted) { abort(); return; }
        try { connection!.send(JSON.stringify(request)); } catch (error) { finish(error); }
      });
    },
    events: {
      [Symbol.asyncIterator]() {
        if (consumed) throw new TypeError('Executor event stream already consumed');
        consumed = true;
        return {
          next(): Promise<IteratorResult<ExecutorEvent>> {
            if (waiting) return Promise.reject(new TypeError('Concurrent executor event reads are unsupported'));
            if (ended) return failure === undefined ? Promise.resolve({ done: true, value: undefined }) : Promise.reject(failure);
            const item = queue.shift();
            if (item) { queuedBytes -= item.bytes; return Promise.resolve({ done: false, value: item.value }); }
            return new Promise((resolve, reject) => { waiting = { resolve, reject }; });
          },
          async return(): Promise<IteratorResult<ExecutorEvent>> { finish(); return { done: true, value: undefined }; },
        };
      },
    },
    async close() { finish(); },
  };
}
