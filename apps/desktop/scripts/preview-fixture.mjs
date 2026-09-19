import { createServer } from 'node:http';
import { connect } from 'node:net';
import { createHash } from 'node:crypto';
import { mkdtemp, rm, chmod } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';

// A private Unix listener stands in for an SSH -O forward endpoint. It is NOT SSH evidence.
export async function previewFixture() {
  const directory = await mkdtemp(path.join(tmpdir(), 'wp-preview-'));
  await chmod(directory, 0o700);
  const socketPath = path.join(directory, 'route');
  const sockets = new Set(); const hits = []; let localHits = 0;
  const track = socket => { sockets.add(socket); socket.on('error', () => {}); socket.once('close', () => sockets.delete(socket)); };
  const local = createServer((_req, res) => { localHits++; res.end('WRONG: viewing Mac'); });
  local.on('connection', track);
  await new Promise(resolve => local.listen(0, '127.0.0.1', resolve));
  const port = local.address().port;
  const denied = createServer((_req, res) => { localHits++; res.end('WRONG: unapproved Mac port'); });
  denied.on('connection', track);
  await new Promise(resolve => denied.listen(0, '127.0.0.1', resolve));
  const deniedPort = denied.address().port;
  const remote = createServer((req, res) => {
    hits.push({ url: req.url, host: req.headers.host, origin: req.headers.origin, auth: req.headers['proxy-authorization'] });
    if (req.url === '/sse') {
      res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' });
      res.write('data: remote-sse\n\n'); return;
    }
    if (req.url === '/redirect') { res.writeHead(302, { Location: `http://localhost:${deniedPort}/denied` }); res.end(); return; }
    if (req.url === '/worker.js') {
      res.setHeader('Content-Type', 'application/javascript');
      res.end(`self.addEventListener('install', () => self.skipWaiting()); self.addEventListener('activate', e => e.waitUntil(self.clients.claim())); self.addEventListener('message', e => e.waitUntil(fetch('http://localhost:${deniedPort}/worker-denied').then(r => e.source.postMessage({status:r.status}), () => e.source.postMessage({blocked:true}))));`); return;
    }
    if (req.url === '/page') {
      res.setHeader('Content-Type', 'text/html');
      res.end('<!doctype html><meta charset="utf-8"><title>SSH proxy origin fixture</title><h1>Private route fixture</h1>'); return;
    }
    res.setHeader('Content-Type', 'application/json');
    res.end(JSON.stringify({ source: 'private-route', host: req.headers.host, origin: req.headers.origin, cookie: req.headers.cookie, proxyAuthLeaked: !!req.headers['proxy-authorization'] }));
  });
  remote.on('connection', track);
  remote.on('upgrade', (req, socket) => {
    hits.push({ url: req.url, host: req.headers.host, origin: req.headers.origin, auth: req.headers['proxy-authorization'] });
    const accept = createHash('sha1').update(req.headers['sec-websocket-key'] + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
    socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`);
    const text = Buffer.from('remote-websocket'); socket.write(Buffer.concat([Buffer.from([0x81, text.length]), text]));
    socket.on('data', data => { if ((data[0] & 15) === 8) socket.end(Buffer.from([0x88, 0])); });
  });
  await new Promise(resolve => remote.listen(socketPath, resolve));
  const route = { port, remoteHost: '127.0.0.1', open: signal => new Promise((resolve, reject) => {
    const socket = connect({ path: socketPath, signal }); socket.once('error', reject);
    socket.once('connect', () => { socket.removeListener('error', reject); resolve(socket); });
  }) };
  return { port, deniedPort, route, hits, get localHits() { return localHits; }, async close() {
    for (const socket of sockets) socket.destroy();
    await Promise.all([local, denied, remote].map(server => new Promise(resolve => server.close(resolve))));
    await rm(directory, { recursive: true, force: true });
  } };
}
