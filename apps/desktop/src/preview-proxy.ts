// Main-owned preview routing. Authorization is supplied by the environment owner.
import { randomBytes, timingSafeEqual } from 'node:crypto';
import { lookup } from 'node:dns/promises';
import { Agent, createServer, request, type IncomingMessage, type ServerResponse } from 'node:http';
import { BlockList, connect, isIP, type Socket } from 'node:net';
import type { Duplex } from 'node:stream';
import { PreviewBudget } from './preview-budget.ts';

export type PreviewLoopback = '127.0.0.1' | '::1';
export interface PreviewRoute {
  port: number;
  remoteHost: PreviewLoopback;
  // The owner supplies only an authenticated-master Unix-socket route, never a host name.
  open(signal: AbortSignal): Promise<Socket>;
}
export interface PreviewProxyOptions {
  routes: PreviewRoute[];
  signal?: AbortSignal;
  budget?: PreviewBudget;
  // Injection is for deterministic DNS-rebinding tests, never renderer IPC.
  lookup?: (hostname: string) => Promise<{ address: string; family: number }[]>;
}
const privateIPs = new BlockList();
for (const [address, prefix] of [
  ['0.0.0.0', 8], ['10.0.0.0', 8], ['100.64.0.0', 10], ['127.0.0.0', 8],
  ['169.254.0.0', 16], ['172.16.0.0', 12], ['192.0.0.0', 24], ['192.0.2.0', 24],
  ['192.88.99.0', 24], ['192.168.0.0', 16], ['198.18.0.0', 15], ['198.51.100.0', 24], ['203.0.113.0', 24],
  ['224.0.0.0', 4], ['240.0.0.0', 4],
] as const) privateIPs.addSubnet(address, prefix, 'ipv4');
const globalIPv6 = new BlockList(); globalIPv6.addSubnet('2000::', 3, 'ipv6');
privateIPs.addSubnet('2001::', 23, 'ipv6'); privateIPs.addSubnet('2001:db8::', 32, 'ipv6');
privateIPs.addSubnet('2002::', 16, 'ipv6');
privateIPs.addSubnet('3ffe::', 16, 'ipv6'); privateIPs.addSubnet('3fff::', 20, 'ipv6');
export function isPublicPreviewAddress(address: string): boolean {
  const family = isIP(address);
  if (family === 4) return !privateIPs.check(address, 'ipv4');
  return family === 6 && globalIPv6.check(address, 'ipv6') && !privateIPs.check(address, 'ipv6');
}
export function previewDestination(value: string, tunnel = false) {
  if (value.length > 8192 || /[\\\s]/.test(value)) throw new Error('Invalid destination');
  const url = new URL(tunnel ? `https://${value}/` : value);
  if (url.username || url.password || url.hash || !['http:', 'https:', 'ws:'].includes(url.protocol)) throw new Error('Invalid destination');
  if (tunnel && (url.pathname !== '/' || url.search || !/^(?:\[[0-9a-fA-F:]+\]|[^:/?#]+):[0-9]+$/.test(value))) throw new Error('Invalid tunnel');
  const hostname = url.hostname.replace(/^\[|\]$/g, '').toLowerCase();
  const port = Number(url.port || (url.protocol === 'https:' ? 443 : 80));
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('Invalid port');
  return { url, hostname, port, loopback: ['localhost', '127.0.0.1', '::1'].includes(hostname) };
}
// localhost follows the approved remote family; explicit literals must not be remapped.
export function matchesPreviewLoopback(hostname: string, remoteHost: PreviewLoopback): boolean {
  return hostname === 'localhost' || hostname === remoteHost;
}
export async function openPinnedPublic(
  hostname: string, port: number, signal: AbortSignal,
  resolve = (name: string) => lookup(name, { all: true, verbatim: true }),
): Promise<Socket> {
  const addresses = isIP(hostname) ? [{ address: hostname, family: isIP(hostname) }] : await resolve(hostname);
  signal.throwIfAborted();
  // Reject mixed answers too. Never connect by name after validation (DNS rebinding).
  if (!addresses.length || addresses.some(item => !isPublicPreviewAddress(item.address))) throw new Error('Private destination denied');
  const address = addresses[0]!;
  return await new Promise((resolveSocket, reject) => {
    const socket = connect({ host: address.address, port, family: address.family, signal });
    socket.once('connect', () => { socket.removeListener('error', reject); resolveSocket(socket); });
    socket.once('error', reject);
  });
}
function upstreamHeaders(req: IncomingMessage, host: string, upgrade = false) {
  const headers: IncomingMessage['headers'] = { ...req.headers, host };
  for (const key of String(req.headers.connection ?? '').toLowerCase().split(',')) {
    const name = key.trim(); if (name && name !== 'upgrade') delete headers[name];
  }
  headers.host = host;
  delete headers['proxy-authorization']; delete headers['proxy-connection'];
  headers.connection = upgrade ? 'Upgrade' : 'close';
  if (!upgrade) delete headers.upgrade;
  return headers;
}
export async function createPreviewProxy(options: PreviewProxyOptions) {
  const controller = new AbortController();
  const budget = options.budget ?? new PreviewBudget();
  const routes = new Map<number, PreviewRoute>();
  for (const route of options.routes) {
    if (!Number.isInteger(route.port) || route.port < 1 || route.port > 65535 || routes.has(route.port) || !['127.0.0.1', '::1'].includes(route.remoteHost)) throw new Error('Invalid route');
    routes.set(route.port, route);
  }
  if (routes.size > 4) throw new Error('Preview port limit reached');
  const username = 'preview'; const password = randomBytes(32).toString('hex');
  const realm = `whip-preview-${randomBytes(12).toString('hex')}`;
  const credential = Buffer.from(`Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`);
  const sockets = new Set<Duplex>();
  const streams = new Set<{ client: Duplex; upstream?: Socket; port?: number; controller: AbortController }>();
  const track = (socket: Duplex) => {
    sockets.add(socket); socket.on('error', () => {}); socket.once('close', () => sockets.delete(socket));
  };
  const authenticated = (req: IncomingMessage) => {
    const supplied = Buffer.from(req.headers['proxy-authorization'] ?? '');
    return supplied.length === credential.length && timingSafeEqual(supplied, credential);
  };
  const fail = (res: ServerResponse | Duplex, code: number) => {
    if ('writeHead' in res) {
      const message = code === 502
        ? 'Whip preview could not reach the requested service.\n\nFor an SSH preview, make sure the app is running on the selected SSH host and port, then reload.\n'
        : code === 407 ? 'Whip preview authentication is required. Reopen the SSH preview.\n'
        : 'Whip preview could not open this destination. Check that the host is connected and the address and port are approved, then reopen the SSH preview.\n';
      res.writeHead(code, { 'Connection': 'close', 'Content-Type': 'text/plain; charset=utf-8',
        'Cache-Control': 'no-store', 'X-Content-Type-Options': 'nosniff',
        ...(code === 407 ? { 'Proxy-Authenticate': `Basic realm="${realm}"` } : {}) });
      res.end(message);
    }
    else res.end(`HTTP/1.1 ${code} ${code === 407 ? 'Proxy Authentication Required' : 'Denied'}\r\nConnection: close\r\n${code === 407 ? `Proxy-Authenticate: Basic realm="${realm}"\r\n` : ''}Content-Length: 0\r\n\r\n`);
  };
  const start = async (req: IncomingMessage, client: Duplex, tunnel: boolean) => {
    if (controller.signal.aborted || streams.size >= 32) throw new Error('Unavailable');
    const destination = previewDestination(req.url ?? '', tunnel);
    const release = budget.acquire();
    const stream = { client, upstream: undefined as Socket | undefined, port: destination.loopback ? destination.port : undefined, controller: new AbortController() };
    streams.add(stream);
    let opened = false;
    const finish = () => {
      // Keep the window slot until an uncancellable DNS lookup actually settles.
      if (opened) { release(); streams.delete(stream); }
      stream.controller.abort(); stream.upstream?.destroy();
    };
    client.once('close', finish);
    const opening = new AbortController();
    const timer = setTimeout(() => opening.abort(), 15_000); timer.unref();
    const signal = AbortSignal.any([controller.signal, stream.controller.signal, opening.signal]);
    try {
      if (destination.loopback) {
        const route = routes.get(destination.port);
        if (!route || !matchesPreviewLoopback(destination.hostname, route.remoteHost)) throw new Error('Unapproved loopback destination');
        stream.upstream = await route.open(signal);
        if (routes.get(destination.port) !== route) throw new Error('Route revoked');
      } else stream.upstream = await openPinnedPublic(destination.hostname, destination.port, signal, options.lookup);
      signal.throwIfAborted();
      track(stream.upstream);
      stream.upstream.once('close', () => { release(); streams.delete(stream); client.removeListener('close', finish); });
      return { destination, upstream: stream.upstream, signal };
    } catch (error) { finish(); client.removeListener('close', finish); throw error; }
    finally { clearTimeout(timer); opened = true; if (stream.controller.signal.aborted) { release(); streams.delete(stream); } }
  };
  const server = createServer({ maxHeaderSize: 16 * 1024 }, async (req, res) => {
    if (!authenticated(req)) { fail(res, 407); return; }
    try {
      if (!req.url?.startsWith('http://')) throw new Error('HTTP requires an absolute URL');
      const { destination, upstream, signal } = await start(req, req.socket, false);
      const agent = new Agent({ keepAlive: false }); agent.createConnection = () => upstream;
      const outgoing = request({ method: req.method, path: destination.url.pathname + destination.url.search,
        headers: upstreamHeaders(req, destination.url.host), agent });
      outgoing.once('close', () => agent.destroy());
      outgoing.on('error', () => { if (!res.headersSent) fail(res, 502); else res.destroy(); });
      outgoing.on('response', response => {
        res.writeHead(response.statusCode ?? 502, response.headers);
        void budget.pipe(response, res, signal).catch(() => { response.destroy(); res.destroy(); });
        response.on('error', () => res.destroy());
      });
      req.on('aborted', () => outgoing.destroy());
      void budget.pipe(req, outgoing, signal).catch(() => outgoing.destroy());
    } catch { fail(res, 403); }
  });
  server.maxConnections = 64; server.headersTimeout = 10_000;
  server.on('connection', track);
  server.on('clientError', (_error, socket) => { if (!socket.destroyed) socket.destroy(); });
  const tunnel = async (req: IncomingMessage, client: Duplex, head: Buffer, connectMethod: boolean) => {
    if (!authenticated(req)) { fail(client, 407); return; }
    try {
      const { destination, upstream, signal } = await start(req, client, connectMethod);
      if (connectMethod) client.write('HTTP/1.1 200 Connection Established\r\n\r\n');
      else {
        const headers = upstreamHeaders(req, destination.url.host, true);
        upstream.write(`${req.method} ${destination.url.pathname + destination.url.search} HTTP/1.1\r\n` +
          Object.entries(headers).filter(([, value]) => value !== undefined).map(([key, value]) => `${key}: ${Array.isArray(value) ? value.join(', ') : value}\r\n`).join('') + '\r\n');
      }
      if (head.length > 64 * 1024) throw new Error('Preview chunk limit exceeded');
      if (head.length) upstream.write(head);
      void budget.pipe(client, upstream, signal).catch(() => { client.destroy(); upstream.destroy(); });
      void budget.pipe(upstream, client, signal).catch(() => { client.destroy(); upstream.destroy(); });
      upstream.once('close', () => client.destroy());
    } catch { fail(client, 403); }
  };
  server.on('connect', (req, client, head) => { void tunnel(req, client, head, true); });
  server.on('upgrade', (req, client, head) => { void tunnel(req, client, head, false); });
  await new Promise<void>((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', () => { server.removeListener('error', reject); resolve(); }); });
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('Missing proxy listener');
  let closing: Promise<void> | undefined;
  const close = () => closing ??= (async () => {
    controller.abort(); routes.clear(); for (const socket of sockets) socket.destroy();
    await new Promise<void>(resolve => server.close(() => resolve()));
  })();
  options.signal?.addEventListener('abort', () => { void close(); }, { once: true });
  if (options.signal?.aborted) await close();
  return {
    host: '127.0.0.1', port: address.port, username, password, realm,
    // Never use a bypass for local addresses and never append a DIRECT fallback.
    proxyRules: `http://127.0.0.1:${address.port}`, proxyBypassRules: '<-loopback>',
    addRoute(route: PreviewRoute) {
      controller.signal.throwIfAborted();
      if (!Number.isInteger(route.port) || route.port < 1 || route.port > 65535 || !['127.0.0.1', '::1'].includes(route.remoteHost)) throw new Error('Invalid route');
      if (routes.has(route.port)) throw new Error('Preview port is already approved');
      if (routes.size >= 4) throw new Error('Preview port limit reached');
      routes.set(route.port, route);
    },
    revoke(port: number) {
      routes.delete(port);
      for (const stream of streams) if (stream.port === port) { stream.controller.abort(); stream.client.destroy(); stream.upstream?.destroy(); }
    },
    close,
  };
}
