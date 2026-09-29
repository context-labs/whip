import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { test } from 'node:test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';
import { liveReplCode } from './native-repl-response.mjs';

test('REPL fixture separates provisional call, real stdout/host operations, settlement and checkpoint recovery', { timeout: 120000 }, async () => {
  await assert.rejects(startFixture({ replStreams: 'true' }), /must be a boolean/);
  const fixture = await startFixture({ replStreams: true, executeCode: true, agentResponses: true });
  try {
    await writeFile(join(fixture.directory, 'repl-evidence.txt'), 'Actual bounded file evidence.\n');
    let client = await fixture.connect('repl-process-check');
    const { root } = await fixture.createRoot(client);
    let session = client.session(root.id);
    const policy = await session.permissions.policy(deadline());
    await session.permissions.setMode({ mode: 'automatic', expected_revision: policy.revision }, randomUUID(), deadline());
    const command = session.submission([{ type: 'text', text: 'repl:live' }], randomUUID());
    await command.send(deadline());
    const preview = await eventually(async () => (await client.call('sessions.observe', { session_id: root.id, after: '0', limit: 100 }, deadline())).preview?.calls?.[0]);
    assert.equal(preview.name, 'execute');
    assert(JSON.stringify({ code: liveReplCode }).startsWith(preview.arguments));
    assert.throws(() => JSON.parse(preview.arguments));
    assert.equal((await session.cells.output(deadline())).preview, null);
    const active = (await session.activity(deadline())).active_turn;
    assert.deepEqual((await session.turns.cells(active.id, { limit: 100 }, deadline())).items, []);
    fixture.release('repl-code');
    const expected = Array.from({ length: 8 }, (_, index) => `live line ${index + 1}\n`).join('');
    const output = await eventually(async () => {
      const value = (await session.cells.output(deadline())).preview;
      return value?.text === expected ? value : false;
    });
    assert.equal(output.session_id, root.id); assert.equal(output.turn_id, active.id);
    const operations = await eventually(async () => {
      const items = (await session.turns.operations(active.id, { limit: 100 }, deadline())).items;
      return items.filter(item => item.capability === 'files.read' && item.state === 'succeeded').length === 2 ? items : false;
    });
    assert(operations.every(item => item.session_id === root.id && item.cell_id === output.cell_id));
    fixture.release('repl-execution');
    assert.equal((await command.wait(deadline())).turn.state, 'succeeded');
    assert.equal((await session.cells.output(deadline())).preview, null);
    const history = await session.history.page({ direction: 'forward' }, deadline());
    const envelope = JSON.parse(history.messages.flatMap(item => item.parts).find(part => part.type === 'tool_result').result.output);
    assert.equal(envelope.result.output, expected); assert.equal(envelope.result.value, 42); assert(envelope.result.steps > 0);
    const effects = await fixture.effects();
    await fixture.crashAndRestart();
    await assert.rejects(session.get(deadline()), error => error.kind === 'IDENTITY');
    assert.deepEqual(await fixture.effects(), effects, 'Restart does not replay a cell or provider call');
    client = await fixture.connect(client.clientID); session = client.session(root.id);
    const next = session.submission([{ type: 'text', text: '```starlark\nprint(repl_saved)\n```' }], randomUUID());
    await next.send(deadline()); assert.equal((await next.wait(deadline())).turn.state, 'succeeded');
    const restored = await session.history.page({ direction: 'backward' }, deadline());
    const result = JSON.parse(restored.messages.flatMap(item => item.parts).findLast(part => part.type === 'tool_result').result.output).result;
    assert.equal(result.output, '42\n'); assert(result.restored.restored.includes('repl_saved'));
  } finally { await fixture.close(); }
});

test('code fixture respects tools-free native compaction across the 100-message context boundary', { timeout: 120000 }, async () => {
  const fixture = await startFixture({ executeCode: true, agentResponses: true });
  try {
    const client = await fixture.connect('repl-context-check'), { root } = await fixture.createRoot(client);
    const session = client.session(root.id);
    for (let index = 0; index < 30; index++) {
      const command = session.submission([{ type: 'text', text: `Recorded cell ${index}.\n\`\`\`starlark\nprint(${index})\n\`\`\`\n\`\`\`final\nRecorded evidence ${index}.\n\`\`\`` }], randomUUID());
      await command.send(deadline()); assert.equal((await command.wait(deadline())).turn.state, 'succeeded');
    }
    assert.notEqual((await client.call('context.head', { session_id: root.id }, deadline())).compaction_id, null);
    assert.equal((await session.history.snapshot(deadline())).message_count, '120');
  } finally { await fixture.close(); }
});
