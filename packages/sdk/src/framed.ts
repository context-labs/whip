import { callSignal } from './wire.js';
import { assertValid } from '@whip/protocol';
import type { Request, Response } from '@whip/protocol';
import { decodeResponse, DeliveryError, RemoteError } from './wire.js';
import type { Transport } from './wire.js';

/** Platform-owned framing, such as a confined Electron IPC connection. Each
 * message is one complete JSON envelope; the connector must bound native queues. */
export interface FramedConnection {
  readonly kind: 'unix' | 'websocket';
  readonly bufferedAmount: number;
  send(message: string): void;
  close(): void;
}
export interface FrameHandlers { message(message: string): void; close(error: Error): void }
export type FramedConnector = (handlers: FrameHandlers, signal: AbortSignal) => Promise<FramedConnection>;
const maxFrameBytes = 8 << 20;
const encoder = new TextEncoder();

/** One verified native connection per call. No reconnect, notification replay,
 * credential transport, or native bridge implementation belongs here. */
export function framedTransport(open: FramedConnector, { expectedProcessEpoch }: { expectedProcessEpoch?: string } = {}): Transport {
  return async (request, expectedRuntimeID, options) => {
    assertValid('Request', request);
    request = structuredClone(request);
    if (request.method === 'initialize' && expectedProcessEpoch !== undefined) {
      assertValid('InitializeParams', request.params);
      if (request.params.expected_process_epoch !== undefined && request.params.expected_process_epoch !== expectedProcessEpoch) throw new TypeError('Native process generation mismatch');
      request.params = { ...request.params, expected_process_epoch: expectedProcessEpoch };
      assertValid('InitializeParams', request.params);
    }
    const signal = callSignal(options);
    signal.throwIfAborted();
    let connection: FramedConnection | undefined;
    let pending: { id: string; resolve(value: Response): void; reject(error: unknown): void } | undefined;
    let failure: unknown;
    let finished = false;
    let openingReject: ((error: unknown) => void) | undefined;
    const close = () => {
      const current = connection;
      connection = undefined; // Native close may synchronously notify its handlers.
      try { current?.close(); } catch { /* Disposal must not erase a received acknowledgement. */ }
    };
    const fail = (error: unknown) => {
      if (finished) return;
      failure ??= error;
      const current = pending; pending = undefined;
      current?.reject(failure);
      openingReject?.(failure);
      close();
    };
    const abort = () => fail(signal.reason);
    signal.addEventListener('abort', abort, { once: true });
    try {
      const opened = open({
        message(raw) {
          if (finished || failure !== undefined) return;
          try {
            if (typeof raw !== 'string' || encoder.encode(raw).length + 1 > maxFrameBytes) throw new TypeError('Native response exceeds frame limit');
            const value: unknown = JSON.parse(raw);
            assertValid('Response', value);
            if (!pending || value.id !== pending.id) throw new TypeError('Unexpected native response identity');
            const result = pending; pending = undefined; result.resolve(value);
          } catch (error) { fail(error); }
        },
        close(error) { fail(new DeliveryError('Native connection closed; delivery may be unknown', { cause: error })); },
      }, signal);
      connection = await new Promise<FramedConnection>((resolve, reject) => {
        openingReject = reject;
        opened.then(value => {
          if (finished || signal.aborted || failure !== undefined) {
            try { value.close(); } catch { /* Retire a connector which completed after cancellation. */ }
            reject(failure ?? signal.reason);
          } else resolve(value);
        }, reject);
        if (signal.aborted || failure !== undefined) reject(failure ?? signal.reason);
      });
      openingReject = undefined;
      signal.throwIfAborted();
      if (connection.kind !== 'unix') throw new TypeError('Native framed transport requires a confined Unix connection; use browserSocket for gateways');
      const invoke = (message: Request): Promise<Response> => new Promise((resolve, reject) => {
        if (failure !== undefined) { reject(failure); return; }
        const raw = JSON.stringify(message);
        const size = encoder.encode(raw).length + 1;
        if (size > maxFrameBytes || !Number.isSafeInteger(connection!.bufferedAmount) || connection!.bufferedAmount < 0 || size > maxFrameBytes - connection!.bufferedAmount) { reject(new RangeError('Native request exceeds frame or queue limit')); return; }
        pending = { id: message.id, resolve, reject };
        try { connection!.send(raw); } catch (error) { fail(error); }
      });
      if (request.method !== 'initialize') {
        const response = await invoke({ jsonrpc: '2.0', id: 'native-initialize', method: 'initialize', params: { major: 4, expected_runtime_id: expectedRuntimeID, ...(expectedProcessEpoch === undefined ? {} : { expected_process_epoch: expectedProcessEpoch }) } });
        const initial = decodeResponse('initialize', 'native-initialize', response);
        if (initial.runtime_id !== expectedRuntimeID || initial.network_client || expectedProcessEpoch !== undefined && initial.process_epoch !== expectedProcessEpoch) throw new TypeError('Native runtime identity, process generation or transport mismatch');
      }
      const response = await invoke(request);
      if (request.method === 'initialize') {
        const initial = decodeResponse('initialize', request.id, response);
        if (initial.network_client || expectedRuntimeID !== undefined && initial.runtime_id !== expectedRuntimeID || expectedProcessEpoch !== undefined && initial.process_epoch !== expectedProcessEpoch) throw new TypeError('Native runtime identity, process generation or transport mismatch');
      }
      return response;
    } catch (error) {
      if (signal.aborted) throw signal.reason;
      if (error instanceof TypeError || error instanceof RangeError || error instanceof RemoteError || error instanceof DeliveryError) throw error;
      throw new DeliveryError('Native transport failed; delivery may be unknown', { cause: error });
    } finally { finished = true; signal.removeEventListener('abort', abort); close(); }
  };
}
