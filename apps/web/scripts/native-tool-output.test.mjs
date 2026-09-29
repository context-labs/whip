import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { test } from 'node:test';
import { defineAgent, tool } from '../../../packages/sdk/dist/agents.js';
import { executorSocket } from '../../../packages/sdk/dist/node.js';
import { createExecutionView, createSessionView } from '../../../packages/sdk/dist/state.js';
import { deadline, startFixture } from './native-fixture.mjs';

test('actual custom output keeps the exact byte ceiling, bounded observation and explicit oversized failure', { timeout: 120_000 }, async () => {
  const fixture = await startFixture({ executeCode: true });
  try {
    const client = await fixture.connect('output-boundary');
    for (const engine of ['starlark', 'quickjs']) for (const size of [(512 << 10) - 2, (512 << 10) - 1]) {
      let invocations = 0;
      const lease = await client.agents.serve(defineAgent({ id: 'output-' + engine + '-' + size, name: 'Output boundary fixture', tools: [tool({
        name: 'large_result', description: 'Return an explicitly sized fixture value', input: { type: 'object', properties: {}, additionalProperties: false },
        execute: () => { invocations++; return 'x'.repeat(size); },
      })] }), { transport: await executorSocket(fixture.info.socket) });
      let source, view;
      try {
        const { root } = await client.createTree({ engine, definition: lease.definition, working_directory: fixture.directory,
          metadata: { title: 'Output boundary', pinned: false, archived: false }, overrides: { automatic_title: false } }, randomUUID(), deadline());
        await client.call('grants.create', { id: randomUUID(), session_id: root.id, capability: 'tools.large_result', resource: lease.definition.id + '@' + lease.definition.revision }, deadline());
        const session = client.session(root.id), requestID = randomUUID();
        await session.submit([{ type: 'text', text: engine === 'starlark' ? '```starlark\nprint(len(tools.large_result()))\n```' : '```javascript\nconsole.log(tools.large_result().length)\n```' }], requestID, deadline());
        const completed = await client.wait(requestID, deadline());
        const operations = (await session.turns.operations(completed.turn.id, { limit: 100 }, deadline())).items;
        const operation = operations.find(item => item.capability === 'tools.large_result');
        assert.ok(operation); assert.equal(invocations, 1);
        if (size === (512 << 10) - 2) {
          assert.equal(operation.state, 'succeeded');
          assert.equal(operation.result.value.length, size);
          assert.equal(Buffer.byteLength(JSON.stringify(operation.result.value)), 512 << 10);
          source = createSessionView(session); await source.start();
          view = createExecutionView(session, source, { maxBytes: 4096 }); await view.start();
          assert.equal(view.getSnapshot().truncated, true);
          assert.ok(view.getSnapshot().retainedBytes <= 4096);
          assert.equal(view.getSnapshot().operations.length, 0, 'Bounded view retained the oversized operation body');
        } else {
          assert.equal(operation.state, 'failed');
          assert.match(operation.result.failure, /Handler result exceeds the JSON byte bound/);
          assert.equal('value' in operation.result, false);
        }
        assert.deepEqual(operation.result.content_references ?? [], [], 'Custom output was silently externalized');
        await session.operations.get(operation.id, deadline()); assert.equal(invocations, 1, 'Inspection replayed the handler');
      } finally { await view?.dispose(); await source?.dispose(); await lease.close(); }
    }
  } finally { await fixture.close(); }
});
