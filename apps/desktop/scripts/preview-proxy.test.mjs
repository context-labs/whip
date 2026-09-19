import assert from 'node:assert/strict';
import { test } from 'node:test';
import { request } from 'node:http';
import { connect, createServer } from 'node:net';
import { once } from 'node:events';
import { createPreviewProxy, isPublicPreviewAddress, previewDestination, openPinnedPublic } from '../src/preview-proxy.ts';
import { previewFixture } from './preview-fixture.mjs';

function get(proxy, target, authenticated = true) {
  return new Promise((resolve, reject) => {
    const headers = authenticated ? { 'Proxy-Authorization': `Basic ${Buffer.from(`${proxy.username}:${proxy.password}`).toString('base64')}` } : {};
    const req = request({ host: proxy.host, port: proxy.port, path: target, headers, agent: false }, res => {
      let body = ''; res.on('data', chunk => body += chunk); res.on('end', () => resolve({ code: res.statusCode, body }));
    }); req.on('error', reject); req.end();
  });
}
test('an upstream disconnect renders an actionable error instead of a blank page', async () => {
  // Simulate an SSH forward accepting locally, then closing when the remote port refuses.
  const upstream = createServer(socket => socket.once('data', () => socket.destroy()));
  await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve));
  const proxy = await createPreviewProxy({ routes: [{
    port: 3000, remoteHost: '127.0.0.1',
    open: async () => {
      const socket = connect(upstream.address().port, '127.0.0.1');
      await once(socket, 'connect'); return socket;
    },
  }] });
  try {
    const response = await get(proxy, 'http://127.0.0.1:3000/');
    assert.equal(response.code, 502);
    assert.match(response.body, /Whip preview could not reach the requested service/);
    assert.match(response.body, /SSH host/);
    assert.match(response.body, /reload/i);
  } finally {
    await proxy.close();
    await new Promise(resolve => upstream.close(resolve));
  }
});

test('literal loopback aliases and public address boundary', async () => {
  for (const host of ['localhost', '127.0.0.1', '[::1]', '2130706433', '0x7f000001']) assert.equal(previewDestination(`http://${host}:3000`).loopback, true);
  for (const address of ['0.0.0.0', '10.0.0.1', '100.64.0.1', '127.1.2.3', '169.254.169.254', '172.16.0.1', '192.168.1.1', '224.0.0.1', '::1', '::ffff:127.0.0.1', 'fc00::1', 'fe80::1', '64:ff9b::7f00:1', '2002:7f00:1::']) assert.equal(isPublicPreviewAddress(address), false, address);
  for (const address of ['1.1.1.1', '8.8.8.8', '2606:4700:4700::1111']) assert.equal(isPublicPreviewAddress(address), true, address);
  for (const value of ['http://user:secret@localhost:3000', 'file:///etc/passwd', 'http://localhost:0', 'http://localhost:65536', 'http://localhost:3000/#fragment']) assert.throws(() => previewDestination(value));
  await assert.rejects(openPinnedPublic('rebind.invalid', 80, new AbortController().signal, async () => [{ address: '1.1.1.1', family: 4 }, { address: '127.0.0.1', family: 4 }]), /Private destination denied/);
});
test('authenticated origin-preserving HTTP, denial, CONNECT and revocation', async () => {
  const fixture = await previewFixture();
  const proxy = await createPreviewProxy({ routes: [fixture.route], lookup: async () => [{ address: '127.0.0.1', family: 4 }] });
  try {
    assert.equal((await get(proxy, `http://localhost:${fixture.port}/`, false)).code, 407);
    assert.equal(fixture.hits.length, 0);
    for (const host of ['localhost', '127.0.0.1']) {
      const response = await get(proxy, `http://${host}:${fixture.port}/`);
      assert.equal(response.code, 200); const body = JSON.parse(response.body);
      assert.equal(body.source, 'private-route'); assert.equal(body.host, `${host}:${fixture.port}`); assert.equal(body.proxyAuthLeaked, false);
    }
    for (const host of [`localhost:${fixture.deniedPort}`, '10.0.0.1:80', '169.254.169.254:80', 'rebind.invalid:80', '[::ffff:127.0.0.1]:80']) {
      assert.equal((await get(proxy, `http://${host}/`)).code, 403);
    }
    const socket = connect(proxy.port, proxy.host); await once(socket, 'connect');
    socket.write(`CONNECT 127.0.0.1:${fixture.port} HTTP/1.1\r\nHost: 127.0.0.1:${fixture.port}\r\nProxy-Authorization: Basic ${Buffer.from(`${proxy.username}:${proxy.password}`).toString('base64')}\r\n\r\n`);
    const [header] = await once(socket, 'data'); assert.match(header.toString(), /200 Connection Established/);
    socket.write(`GET /sse HTTP/1.1\r\nHost: localhost:${fixture.port}\r\n\r\n`);
    const [data] = await once(socket, 'data'); assert.match(data.toString(), /remote-sse/);
    const closed = once(socket, 'close'); proxy.revoke(fixture.port); await closed;
    assert.equal((await get(proxy, `http://localhost:${fixture.port}/`)).code, 403);
    assert.equal(fixture.localHits, 0);
  } finally { await proxy.close(); await fixture.close(); }
});

test('same-port cross-family literals are denied before HTTP, CONNECT or WebSocket route opens', async () => {
  const fixture = await previewFixture();
  try {
    for (const remoteHost of ['127.0.0.1', '::1']) {
      let opens = 0;
      const proxy = await createPreviewProxy({ routes: [{ ...fixture.route, remoteHost, open: signal => { opens++; return fixture.route.open(signal); } }] });
      try {
        const approved = remoteHost === '::1' ? '[::1]' : remoteHost;
        const denied = remoteHost === '::1' ? '127.0.0.1' : '[::1]';
        for (const host of [approved, 'localhost']) assert.equal((await get(proxy, `http://${host}:${fixture.port}/`)).code, 200);
        const before = opens;
        assert.equal((await get(proxy, `http://${denied}:${fixture.port}/`)).code, 403);
        for (const method of ['CONNECT', 'websocket']) {
          const socket = connect(proxy.port, proxy.host);
          socket.setTimeout(3000, () => socket.destroy(new Error('Timed out waiting for explicit denial')));
          try {
            await once(socket, 'connect');
            const auth = Buffer.from(`${proxy.username}:${proxy.password}`).toString('base64');
            const line = method === 'CONNECT' ? `CONNECT ${denied}:${fixture.port}` : `GET http://${denied}:${fixture.port}/ws`;
            const upgrade = method === 'websocket' ? 'Connection: Upgrade\r\nUpgrade: websocket\r\n' : '';
            socket.write(`${line} HTTP/1.1\r\nHost: ${denied}:${fixture.port}\r\nProxy-Authorization: Basic ${auth}\r\n${upgrade}\r\n`);
            const [response] = await once(socket, 'data');
            assert.match(response.toString(), /^HTTP\/1\.1 403 /);
          } finally { socket.destroy(); }
        }
        assert.equal(opens, before, 'Denied cross-family requests must not open even the authorized remote route');
      } finally { await proxy.close(); }
    }
    assert.equal(fixture.localHits, 0);
  } finally { await fixture.close(); }
});
