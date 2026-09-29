import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { deadline, eventually } from './native-fixture.mjs';

/** Disposable MCP server used to obtain actual operation-owned image refs.
 * Authored uploads cannot fabricate tool results or internal history rows. */
export async function imageServer(images) {
  assert.ok(images.length >= 1 && images.length <= 5);
  assert.ok(images.every(bytes => bytes.length > 0 && bytes.length <= 4 << 20));
  let calls = 0;
  const server = createServer(async (request, response) => {
    if (request.method !== 'POST' || request.url !== '/mcp') { response.writeHead(405).end(); return; }
    try {
      let text = '';
      for await (const chunk of request) { text += chunk; if (Buffer.byteLength(text) > 65536) throw new Error('MCP request exceeds bound'); }
      const message = JSON.parse(text);
      if (!Object.hasOwn(message, 'id')) { response.writeHead(202).end(); return; }
      let result;
      if (message.method === 'initialize') result = { protocolVersion: '2025-03-26', capabilities: { tools: {} }, serverInfo: { name: 'native-image-probe', version: '1' } };
      else if (message.method === 'tools/list') result = { tools: [{ name: 'screenshots', description: 'Return synthetic test images', inputSchema: { type: 'object', properties: { count: { type: 'integer', minimum: 1, maximum: images.length } }, required: ['count'], additionalProperties: false } }] };
      else if (message.method === 'tools/call') {
        assert.equal(message.params.name, 'screenshots');
        const count = message.params.arguments.count;
        assert.ok(Number.isInteger(count) && count >= 1 && count <= images.length && calls < 16);
        calls++;
        result = { content: images.slice(0, count).map(bytes => ({ type: 'image', mimeType: 'image/png', data: bytes.toString('base64') })) };
      } else throw new Error('Unexpected MCP method');
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ jsonrpc: '2.0', id: message.id, result }));
    } catch { response.writeHead(400).end(); }
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  return {
    get calls() { return calls; },
    async attach(client, root) {
      const current = await client.mcpConfiguration(deadline());
      await client.configureMCP({ revision: current.revision, name: 'images', server: { command: [], env: {}, cwd: '', url: `http://127.0.0.1:${server.address().port}/mcp`, headers: {}, enabled: true, note: '', startup_timeout_seconds: 15, tool_timeout_seconds: 15 }, remove: false, imports: null, brand_icons: null }, deadline());
      const session = client.session(root), policy = await session.permissions.policy(deadline());
      await session.permissions.setMode({ expected_revision: policy.revision, mode: 'automatic' }, crypto.randomUUID(), deadline());
      await client.refreshMCP(root, deadline());
      await eventually(async () => (await client.mcpStatus(root, deadline())).items.some(item => item.name === 'images' && item.state === 'ready'), { description: 'authorized loopback MCP image server' });
    },
    close: () => new Promise((resolve, reject) => { server.close(error => error ? reject(error) : resolve()); server.closeAllConnections(); }),
  };
}
