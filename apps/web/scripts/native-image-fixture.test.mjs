import assert from 'node:assert/strict';
import { test } from 'node:test';
import { deadline, startFixture } from './native-fixture.mjs';
import { imageServer } from './native-image-fixture.mjs';

test('native MCP images are actual operation-owned bounded content, never authored internal rows', { timeout: 120_000 }, async () => {
  const fixture = await startFixture({ executeCode: true });
  let server;
  try {
    const client = await fixture.connect();
    const { root } = await fixture.createRoot(client);
    const image = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=', 'base64');
    server = await imageServer([image]); await server.attach(client, root.id);
    const id = crypto.randomUUID(), session = client.session(root.id);
    await session.submit([{ type: 'text', text: '```starlark\nprint(mcp.call(server="images", tool="screenshots", arguments={"count": 1}))\n```' }], id, deadline());
    const done = await client.wait(id, deadline());
    assert.equal(done.turn.state, 'succeeded', JSON.stringify(done) + fixture.output);
    const operations = (await session.turns.operations(done.turn.id, { limit: 100 }, deadline())).items;
    const operation = operations.find(item => item.capability === 'mcp.call.trusted');
    assert.ok(operation, JSON.stringify(operations)); assert.equal(operation.state, 'succeeded', JSON.stringify(operation));
    assert.equal(server.calls, 1); assert.equal(operation.result.content_references.length, 1);
    const ref = operation.result.content_references[0];
    const metadata = await session.content.get(ref, deadline());
    assert.equal(metadata.session_id, root.id); assert.equal(metadata.media_type, 'image/png');
    assert.deepEqual(Buffer.from(await session.content.readBytes(metadata, deadline())), image);
  } finally { try { await fixture.close(); } finally { await server?.close(); } }
});
