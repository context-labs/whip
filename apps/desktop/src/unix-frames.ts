import { connect } from 'node:net';
import type { FramedConnector } from '@whip/sdk';

export const nativeFrameBytes = 8 << 20;

/** Native byte framing only. Protocol identity and operation state belong to the SDK/runtime. */
export function unixFrames(path: string): FramedConnector {
  if (!path) throw new TypeError('Runtime socket required');
  return (handlers, signal) => new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const socket = connect(path);
    let opened = false;
    let closed = false;
    let pending = Buffer.alloc(0);
    let size = 0;
    const finish = (error: Error) => {
      if (closed) return;
      closed = true;
      clearTimeout(timeout);
      signal.removeEventListener('abort', abort);
      pending = Buffer.alloc(0); size = 0;
      socket.destroy();
      if (!opened) reject(error);
      else handlers.close(error);
    };
    const abort = () => finish(signal.reason instanceof Error ? signal.reason : new Error('Unix connection cancelled'));
    const timeout = setTimeout(() => finish(new Error('Unix connection timed out')), 15_000);
    signal.addEventListener('abort', abort, { once: true });
    socket.on('error', finish);
    socket.on('close', () => finish(new Error('Unix socket closed')));
    socket.on('data', (bytes: Buffer) => {
      let start = 0;
      while (start < bytes.length && !closed) {
        const end = bytes.indexOf(10, start);
        const part = bytes.subarray(start, end < 0 ? bytes.length : end);
        const needed = size + part.length;
        if (needed >= nativeFrameBytes) { finish(new RangeError('Unix message exceeds the frame limit')); return; }
        if (needed > pending.length) {
          const expanded = Buffer.allocUnsafe(Math.min(nativeFrameBytes, Math.max(needed, pending.length * 2, 4096)));
          pending.copy(expanded, 0, 0, size); pending = expanded;
        }
        part.copy(pending, size); size = needed;
        if (end < 0) break;
        try {
          const text = new TextDecoder('utf-8', { fatal: true }).decode(pending.subarray(0, size));
          size = 0;
          handlers.message(text);
        } catch { finish(new Error('Invalid UTF-8 socket message or rejected frame')); return; }
        start = end + 1;
      }
    });
    socket.once('connect', () => {
      if (closed) return;
      clearTimeout(timeout);
      opened = true;
      resolve({
        kind: 'unix',
        get bufferedAmount() { return socket.writableLength; },
        send(message) {
          if (closed) throw new Error('Unix socket is disconnected');
          const bytes = typeof message === 'string' ? Buffer.byteLength(message) + 1 : Infinity;
          if (bytes > nativeFrameBytes || bytes > nativeFrameBytes - socket.writableLength || message.includes('\n'))
            throw new RangeError('Unix message exceeds frame or queue limit');
          socket.write(message + '\n');
        },
        close() { finish(new Error('Unix socket closed')); },
      });
    });
  });
}
