import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { once } from 'node:events';
import { createServer as createHTTPServer, request } from 'node:http';
import { test } from 'node:test';
import { createServer } from 'vite';
import { daemonProxy } from '../dev-proxy.ts';

function call(url, { websocket = false, headers = {}, method = 'GET' } = {}) {
  return new Promise((resolve, reject) => {
    const req = request(url, { method, headers: { ...(websocket ? {
      Connection: 'Upgrade', Upgrade: 'websocket', 'Sec-WebSocket-Version': '13',
      'Sec-WebSocket-Key': 'dGhlIHNhbXBsZSBub25jZQ==',
    } : {}), ...headers } });
    req.on('error', reject);
    req.on('response', res => {
      res.resume(); res.on('end', () => resolve(res.statusCode));
    });
    req.on('upgrade', (res, socket) => { socket.destroy(); resolve(res.statusCode); });
    req.setTimeout(5000, () => req.destroy(new Error('Proxy request timed out')));
    req.end();
  });
}

test('dev API proxy admits same-origin HTTP/WS and rejects cross-origin relay requests', async () => {
  const received = [];
  const upstream = createHTTPServer((req, res) => {
    received.push({ origin: req.headers.origin, host: req.headers.host, path: req.url });
    res.end('ok');
  });
  upstream.on('upgrade', (req, socket) => {
    received.push({ origin: req.headers.origin, host: req.headers.host, path: req.url });
    const accept = createHash('sha1').update(req.headers['sec-websocket-key'] + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
    socket.end(`HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`);
  });
  upstream.listen(0, '127.0.0.1'); await once(upstream, 'listening');
  const target = `http://127.0.0.1:${upstream.address().port}`;
  const vite = await createServer({ configFile: false, logLevel: 'silent',
    server: { host: '127.0.0.1', port: 0, proxy: { '/api/': daemonProxy(target) } } });
  try {
    await vite.listen();
    const port = vite.httpServer.address().port, origin = `http://127.0.0.1:${port}`;
    const endpoint = `${origin}/api/v3/ws`;
    assert.equal(await call(`${origin}/api/v3/web`), 200);
    assert.equal(await call(`${origin}/api/v3/content/file`, { method: 'POST', headers: { Origin: origin } }), 200);
    assert.equal(await call(endpoint, { websocket: true, headers: { Origin: origin } }), 101);
    assert.equal(await call(endpoint, { websocket: true, headers: { Host: `localhost:${port}`, Origin: `http://localhost:${port}` } }), 101);
    assert.equal(received.length, 4);
    assert.ok(received.every(value => value.origin === target && value.host === new URL(target).host));
    assert.equal(received[1].path, '/api/v3/content/file');
    for (const websocket of [false, true]) {
      for (const hostile of ['https://example.invalid', 'null', `${origin}.evil.test`, `${origin}/`, 'http://127.0.0.1:1']) {
        assert.equal(await call(endpoint, { websocket, headers: { Origin: hostile } }), 404);
      }
      // Vite's HTTP host guard rejects first; upgrades reach the proxy guard.
      assert.equal(await call(endpoint, { websocket, headers: { Host: 'evil.test', Origin: 'http://evil.test' } }), websocket ? 404 : 403);
      assert.equal(await call(endpoint, { websocket, headers: { Origin: [origin, origin] } }), 404);
      assert.equal(await call(endpoint, { websocket, headers: { Origin: origin, 'Sec-Fetch-Site': 'cross-site' } }), 404);
    }
    assert.equal(await call(endpoint, { websocket: true }), 404);
    assert.equal(await call(endpoint, { method: 'POST' }), 404);
    assert.equal(await call(endpoint, { headers: { 'Sec-Fetch-Site': 'same-site' } }), 404);
    assert.equal(received.length, 4, 'Rejected requests must never reach the gateway');
  } finally {
    await vite.close(); upstream.closeAllConnections();
    await new Promise(resolve => upstream.close(resolve));
  }
});

test('dev proxy defaults to the foreground gateway and preserves explicit overrides', t => {
  const previous = process.env.WHIP_WEB_DAEMON;
  t.after(() => {
    if (previous === undefined) delete process.env.WHIP_WEB_DAEMON;
    else process.env.WHIP_WEB_DAEMON = previous;
  });
  delete process.env.WHIP_WEB_DAEMON;
  assert.equal(daemonProxy().target, 'http://127.0.0.1:4444');
  process.env.WHIP_WEB_DAEMON = 'http://127.0.0.1:9123';
  assert.equal(daemonProxy().target, 'http://127.0.0.1:9123');
});

test('gateway target is an explicit HTTP(S) origin', () => {
  assert.equal(daemonProxy('http://127.0.0.1:9000').target, 'http://127.0.0.1:9000');
  assert.equal(daemonProxy('https://daemon.example').target, 'https://daemon.example');
  for (const target of ['file:///tmp/daemon', 'ws://localhost:8080', 'http://user:pass@localhost:8080',
    'http://localhost:8080/api/v3/ws', 'http://localhost:8080/?target=other', 'http://localhost:8080/#other']) {
    assert.throws(() => daemonProxy(target));
  }
});
