import { callSignal } from './wire.js';
import { connect } from 'node:net';
import { once } from 'node:events';
import { assertValid } from '@whip/protocol';
import type { Request, Response } from '@whip/protocol';
import { decodeResponse, DeliveryError, RemoteError } from './wire.js';
import type { Transport } from './wire.js';

const maxFrameBytes = 8 << 20;

/** One bounded connection per call; each reconnect verifies the pinned runtime identity. */
export function unixSocket(path: string): Transport {
  if (!path) throw new TypeError('Runtime socket required');
  return async (request, expectedRuntimeID, options) => {
    const signal = callSignal(options);
    signal.throwIfAborted();
    const socket = connect(path);
    const aborted = () => socket.destroy();
    signal.addEventListener('abort', aborted, { once: true });
    let pending = Buffer.alloc(0);
    try {
      await once(socket, 'connect', { signal });
      const chunks = socket[Symbol.asyncIterator]();
      const invoke = async (message: Request): Promise<Response> => {
        const raw = JSON.stringify(message);
        if (Buffer.byteLength(raw) > maxFrameBytes) throw new TypeError('Request exceeds frame limit');
        socket.write(raw + '\n');
        for (;;) {
          const end = pending.indexOf(10);
          if (end >= 0) {
            if (end > maxFrameBytes) throw new TypeError('Response exceeds frame limit');
            const value: unknown = JSON.parse(pending.subarray(0, end).toString('utf8'));
            pending = pending.subarray(end + 1);
            assertValid('Response', value);
            return value;
          }
          if (pending.length > maxFrameBytes) throw new TypeError('Response exceeds frame limit');
          const chunk = await chunks.next();
          if (chunk.done) throw new DeliveryError('Connection closed before acknowledgement; delivery is unknown');
          pending = Buffer.concat([pending, chunk.value as Buffer]);
        }
      };
      if (request.method !== 'initialize') {
        const params = { major: 4, ...(expectedRuntimeID ? { expected_runtime_id: expectedRuntimeID } : {}) };
        const response = await invoke({ jsonrpc: '2.0', id: 'handshake', method: 'initialize', params });
        const initial = decodeResponse('initialize', 'handshake', response);
        if (initial.runtime_id !== expectedRuntimeID) throw new TypeError('Runtime identity mismatch');
      }
      return await invoke(request);
    } catch (error) {
      if (signal.aborted) throw signal.reason;
      if (error instanceof RemoteError || error instanceof DeliveryError || error instanceof TypeError) throw error;
      throw new DeliveryError('Transport failed; mutation delivery may be unknown', { cause: error });
    } finally {
      signal.removeEventListener('abort', aborted);
      socket.destroy();
    }
  };
}

export { executorSocket } from './executor-node.js';
