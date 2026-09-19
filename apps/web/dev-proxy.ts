import type { ClientRequest } from 'node:http';
import { TLSSocket } from 'node:tls';
import type { ProxyOptions } from 'vite';

/** The dev server is a same-origin API facade for a local browser, not an open relay. */
export function daemonProxy(target = process.env.WHIP_WEB_DAEMON || 'http://127.0.0.1:8080'): ProxyOptions {
  const upstream = new URL(target);
  if (!['http:', 'https:'].includes(upstream.protocol) || upstream.username || upstream.password
    || upstream.pathname !== '/' || upstream.search || upstream.hash) {
    throw new Error('WHIP_WEB_DAEMON must be an HTTP(S) daemon origin, without credentials or a path.');
  }
  return {
    target: upstream.origin, ws: true, changeOrigin: true,
    bypass(request) {
      const { remoteAddress, localPort } = request.socket;
      const local = ['127.0.0.1', '::1', '::ffff:127.0.0.1'].includes(remoteAddress ?? '');
      const hosts = ['127.0.0.1', 'localhost', '[::1]'].map(host => `${host}:${localPort}`);
      const origin = request.headers.origin;
      const scheme = request.socket instanceof TLSSocket ? 'https' : 'http';
      const sameOrigin = origin === `${scheme}://${request.headers.host}`;
      // HTTP reads may omit Origin. Browser upgrades and writes must identify
      // this exact dev origin; Vite does not validate proxied WebSocket origins.
      const read = !request.headers.upgrade && ['GET', 'HEAD'].includes(request.method ?? '');
      if (!local || !hosts.includes(request.headers.host ?? '')
        || (origin ? !sameOrigin : !read)
        || ['cross-site', 'same-site'].includes(request.headers['sec-fetch-site'] as string)) return false;
    },
    configure(proxy) {
      // Rewrite only after bypass has admitted the request, for both scoped
      // content transfers and WebSockets. The daemon's allowlist stays intact.
      const origin = (request: ClientRequest) => request.setHeader('Origin', upstream.origin);
      proxy.on('proxyReq', origin);
      proxy.on('proxyReqWs', origin);
    },
  };
}
