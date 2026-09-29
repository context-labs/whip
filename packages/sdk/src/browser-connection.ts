import { DeliveryError } from './wire.js';

export const maxFrameBytes = 8 << 20;
export const encoder = new TextEncoder();

export function endpointURL(endpoint: string): URL {
  const url = new URL(endpoint);
  if (url.protocol === 'http:') url.protocol = 'ws:';
  if (url.protocol === 'https:') url.protocol = 'wss:';
  if (!['ws:', 'wss:'].includes(url.protocol) || url.username || url.password || url.hash || url.search) throw new TypeError('Expected a browser gateway URL without credentials, query or fragment');
  if (url.pathname === '/') url.pathname = '/api/v4/ws';
  if (url.pathname !== '/api/v4/ws') throw new TypeError('Expected the v4 gateway WebSocket path');
  return url;
}

export interface BrowserConnection {
  send(message: string): void;
  close(): void;
}

/** One native connection, bounded frames and writes, with no reconnect or replay. */
export function openBrowserConnection(endpoint: string, handlers: { message(value: unknown, bytes: number): void; close(error: unknown): void }, signal: AbortSignal): Promise<BrowserConnection> {
  const url = endpointURL(endpoint);
  signal.throwIfAborted();
  return new Promise((resolve, reject) => {
    const socket = new WebSocket(url.href);
    let opened = false;
    let ended = false;
    const finish = (error: unknown) => {
      if (ended) return;
      ended = true;
      signal.removeEventListener('abort', abort);
      socket.onopen = socket.onmessage = socket.onerror = socket.onclose = null;
      if (socket.readyState < WebSocket.CLOSING) socket.close();
      if (opened) handlers.close(error); else reject(error);
    };
    const abort = () => finish(signal.reason);
    signal.addEventListener('abort', abort, { once: true });
    socket.onopen = () => {
      opened = true;
      resolve({
        send(message) {
          if (ended || socket.readyState !== WebSocket.OPEN) throw new DeliveryError('Browser connection is closed; delivery may be unknown');
          const size = encoder.encode(message).byteLength;
          if (size + 1 > maxFrameBytes || size > maxFrameBytes - (socket.bufferedAmount ?? 0)) throw new TypeError('Browser write exceeds frame or queue limit');
          socket.send(message);
        },
        close() { finish(new DeliveryError('Browser connection closed; delivery may be unknown')); },
      });
    };
    socket.onmessage = event => {
      if (ended) return;
      try {
        if (typeof event.data !== 'string') throw new TypeError('Expected a WebSocket text message');
        const bytes = encoder.encode(event.data).byteLength;
        if (bytes + 1 > maxFrameBytes) throw new TypeError('Browser response exceeds frame limit');
        handlers.message(JSON.parse(event.data), bytes);
      } catch (error) { finish(error); }
    };
    socket.onerror = () => queueMicrotask(() => finish(new DeliveryError('WebSocket connection failed; delivery may be unknown')));
    socket.onclose = event => {
      const code = Number.isInteger(event.code) && event.code >= 1000 && event.code <= 4999 ? ` (${event.code})` : '';
      const reason = typeof event.reason === 'string' ? event.reason.slice(0, 256).replace(/[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/gu, ' ').replace(/\s+/g, ' ').trim().replace(/[\uD800-\uDBFF]$/u, '') : '';
      finish(new DeliveryError(`WebSocket closed${code}${reason ? `: ${reason}` : ''}; delivery may be unknown`));
    };
    if (signal.aborted) abort();
  });
}
