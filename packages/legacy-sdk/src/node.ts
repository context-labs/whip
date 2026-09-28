import { connect } from 'node:net';
import { WhipError, abortError } from './errors.js';
import type { TransportFactory } from './transport.js';
export { createWhipClient, WhipClient } from './client.js';

/** Attach to an existing daemon without enabling its network listener. */
export function unixSocket(path: string, maxFrameBytes = 1 << 20): TransportFactory {
  if (!path || !Number.isSafeInteger(maxFrameBytes) || maxFrameBytes < 1) throw new TypeError('Invalid Unix transport options');
  return (handlers, signal) => new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const socket = connect(path);
    let opened = false;
    let closed = false;
    let parts: Buffer[] = [];
    let size = 0;
    const finish = (error: Error) => {
      if (closed) return;
      closed = true;
      signal.removeEventListener('abort', abort);
      parts = []; size = 0;
      socket.destroy();
      if (!opened) reject(error);
      else handlers.close(error);
    };
    const abort = () => finish(abortError(signal));
    signal.addEventListener('abort', abort, { once: true });
    socket.on('error', error => finish(error));
    socket.on('close', () => finish(new WhipError('disconnected', 'Unix socket closed')));
    socket.on('data', (bytes: Buffer) => {
      let start = 0;
      while (start < bytes.length && !closed) {
        const end = bytes.indexOf(10, start);
        const part = bytes.subarray(start, end < 0 ? bytes.length : end);
        size += part.length;
        if (size > maxFrameBytes) { finish(new WhipError('resource_limit', 'Unix message exceeds the frame limit')); return; }
        parts.push(part);
        if (end < 0) break;
        try {
          const text = new TextDecoder('utf-8', { fatal: true }).decode(Buffer.concat(parts, size));
          parts = []; size = 0;
          handlers.message(text);
        } catch { finish(new WhipError('invalid_response', 'Invalid UTF-8 socket message')); return; }
        start = end + 1;
      }
    });
    socket.once('connect', () => {
      opened = true;
      resolve({
        kind: 'unix',
        get bufferedAmount() { return socket.writableLength; },
        send(message) {
          if (closed) throw new WhipError('disconnected', 'Unix socket is disconnected');
          socket.write(message + '\n');
        },
        close() { finish(new WhipError('disconnected', 'Unix socket closed')); },
      });
    });
  });
}
