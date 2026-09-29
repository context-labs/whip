import { connect } from 'node:net';
import { once } from 'node:events';
import { assertValid } from '@whip/protocol';
import type { ExecutorEvent, Request, Response } from '@whip/protocol';
import { DeliveryError } from './wire.js';
import type { CallOptions } from './wire.js';
import type { DuplexTransport } from './executors.js';

const maxFrameBytes = 8 << 20;
const maxEvents = 32;
const maxRequests = 32;

/** An explicit persistent Unix connection. Aborting a request closes this peer;
 * accepted ordinary session work still belongs to the runtime. No reconnects. */
export async function executorSocket(path: string, options: CallOptions = {}): Promise<DuplexTransport> {
  if (!path) throw new TypeError('Runtime socket required');
  options.signal?.throwIfAborted();
  const socket = connect(path);
  const connected = options.signal ? AbortSignal.any([options.signal, AbortSignal.timeout(15_000)]) : AbortSignal.timeout(15_000);
  const abortConnect = () => socket.destroy();
  connected.addEventListener('abort', abortConnect, { once: true });
  try {
    await once(socket, 'connect', { signal: connected });
  } catch (error) {
    socket.destroy();
    throw error;
  } finally {
    connected.removeEventListener('abort', abortConnect);
  }

  let failure: unknown;
  let ended = false;
  let consumed = false;
  let pending = Buffer.alloc(0);
  let queuedBytes = 0;
  const queue: { value: ExecutorEvent; bytes: number }[] = [];
  const requests = new Map<string, { resolve(value: Response): void; reject(error: unknown): void; cleanup(): void }>();
  let waiting: { resolve(value: IteratorResult<ExecutorEvent>): void; reject(error: unknown): void } | undefined;
  let closedResolve!: () => void;
  const closed = new Promise<void>(resolve => { closedResolve = resolve; });

  const abortLifetime = () => finish(options.signal?.reason);
  const finish = (reason?: unknown) => {
    if (ended) return;
    ended = true;
    options.signal?.removeEventListener('abort', abortLifetime);
    failure = reason;
    pending = Buffer.alloc(0);
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
    socket.destroy();
  };
  options.signal?.addEventListener('abort', abortLifetime, { once: true });
  if (options.signal?.aborted) abortLifetime();
  socket.on('error', error => finish(new DeliveryError('Executor connection failed; delivery may be unknown', { cause: error })));
  socket.on('close', () => {
    finish(new DeliveryError('Executor connection ended; invocations are not replayed'));
    closedResolve();
  });
  socket.on('data', (chunk: Buffer) => {
    if (ended) return;
    pending = Buffer.concat([pending, chunk]);
    try {
      for (;;) {
        const end = pending.indexOf(10);
        if (end < 0) {
          if (pending.length > maxFrameBytes) throw new TypeError('Executor frame exceeds limit');
          return;
        }
        if (end > maxFrameBytes) throw new TypeError('Executor frame exceeds limit');
        const frame = pending.subarray(0, end);
        pending = pending.subarray(end + 1);
        const value: unknown = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(frame));
        if (value !== null && typeof value === 'object' && 'id' in value) {
          assertValid('Response', value);
          const request = requests.get(value.id);
          if (!request) throw new TypeError('Executor response has no pending request');
          requests.delete(value.id);
          request.cleanup();
          request.resolve(value);
        } else {
          assertValid('ExecutorEvent', value);
          if (waiting) {
            waiting.resolve({ done: false, value });
            waiting = undefined;
          } else {
            if (queue.length >= maxEvents || end > maxFrameBytes - queuedBytes) throw new TypeError('Executor event queue exceeds limit');
            queue.push({ value, bytes: end });
            queuedBytes += end;
          }
        }
      }
    } catch (error) {
      finish(error);
    }
  });

  return {
    request(request: Request, callOptions: CallOptions = {}): Promise<Response> {
      callOptions.signal?.throwIfAborted();
      if (ended) return Promise.reject(failure ?? new DeliveryError('Executor connection is closed'));
      assertValid('Request', request);
      if (requests.size >= maxRequests || requests.has(request.id)) return Promise.reject(new TypeError('Executor request capacity or identity conflict'));
      const bytes = Buffer.from(JSON.stringify(request) + '\n');
      if (bytes.length > maxFrameBytes || bytes.length > maxFrameBytes - socket.writableLength) return Promise.reject(new TypeError('Executor write exceeds bounds'));
      return new Promise<Response>((resolve, reject) => {
        const signal = callOptions.signal ? AbortSignal.any([callOptions.signal, AbortSignal.timeout(15_000)]) : AbortSignal.timeout(15_000);
        const abort = () => finish(signal.reason);
        signal.addEventListener('abort', abort, { once: true });
        requests.set(request.id, { resolve, reject, cleanup: () => signal.removeEventListener('abort', abort) });
        if (signal.aborted) { abort(); return; }
        socket.write(bytes, error => { if (error) finish(new DeliveryError('Executor write failed; delivery may be unknown', { cause: error })); });
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
          async return(): Promise<IteratorResult<ExecutorEvent>> { finish(); await closed; return { done: true, value: undefined }; },
        };
      },
    },
    async close() { finish(); await closed; },
  };
}
