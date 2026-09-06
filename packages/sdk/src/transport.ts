import { WhipError, abortError } from './errors.js';

export interface Transport {
  readonly kind: 'websocket' | 'unix';
  readonly bufferedAmount: number;
  readonly httpEndpoint?: string;
  send(message: string): void;
  close(): void;
}
export interface TransportHandlers {
  message(message: string): void;
  close(error: Error): void;
}
export type TransportFactory = (handlers: TransportHandlers, signal: AbortSignal) => Promise<Transport>;

/** One native WebSocket text message per JSON-RPC envelope. */
export function webSocket(endpoint: string): TransportFactory {
  const url = new URL(endpoint);
  if (url.protocol === 'http:') url.protocol = 'ws:';
  if (url.protocol === 'https:') url.protocol = 'wss:';
  if (url.protocol !== 'ws:' && url.protocol !== 'wss:') throw new TypeError('Expected a WebSocket or HTTP endpoint');
  if (url.username || url.password || url.hash) throw new TypeError('Endpoint must not contain credentials or a fragment');
  if (url.pathname === '/') url.pathname = '/api/v2/ws';
  const base = new URL(url);
  base.protocol = url.protocol === 'wss:' ? 'https:' : 'http:';
  base.pathname = '/'; base.search = '';
  return (handlers, signal) => new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const socket = new WebSocket(url);
    let opened = false;
    let closed = false;
    const finish = (error: Error) => {
      if (closed) return;
      closed = true;
      signal.removeEventListener('abort', abort);
      socket.onopen = socket.onmessage = socket.onerror = socket.onclose = null;
      if (socket.readyState < WebSocket.CLOSING) socket.close();
      if (!opened) reject(error);
      else handlers.close(error);
    };
    const abort = () => finish(abortError(signal));
    signal.addEventListener('abort', abort, { once: true });
    socket.onopen = () => {
      opened = true;
      resolve({
        kind: 'websocket', httpEndpoint: base.origin,
        get bufferedAmount() { return socket.bufferedAmount; },
        send(message) {
          if (closed || socket.readyState !== WebSocket.OPEN) throw new WhipError('disconnected', 'WebSocket is disconnected');
          socket.send(message);
        },
        close() { finish(new WhipError('disconnected', 'WebSocket closed')); },
      });
    };
    socket.onmessage = event => {
      if (typeof event.data !== 'string') { finish(new WhipError('invalid_response', 'Expected a WebSocket text message')); return; }
      handlers.message(event.data);
    };
    socket.onerror = () => finish(new WhipError('disconnected', 'WebSocket connection failed'));
    socket.onclose = () => finish(new WhipError('disconnected', 'WebSocket closed'));
  });
}
