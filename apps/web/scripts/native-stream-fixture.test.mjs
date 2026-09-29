import assert from 'node:assert/strict';
import { test } from 'node:test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

test('held native executor publishes complete cumulative stdout before completion', { timeout: 120000 }, async () => {
  const fixture = await startFixture();
  try {
    const client = await fixture.connect('native-stream');
    const { root } = await fixture.createRoot(client), session = client.session(root.id);
    const held = session.submission([{ type: 'text', text: 'hold:tool-stream-refresh-native' }], 'two-cells');
    await held.send(deadline());
    const output = await eventually(async () => {
      const value = await session.cells.output(deadline()); return value.preview?.text === 'first\nsecond\n' && value;
    }, { description: 'real held cell stdout' });
    const cells = await session.turns.cells(output.preview.turn_id, {}, deadline());
    assert.equal(cells.items.length, 2);
    assert.equal(cells.items.find(cell => cell.id === output.preview.cell_id)?.state, 'running');
    assert.equal(cells.items.find(cell => cell.id !== output.preview.cell_id)?.state, 'succeeded');
    const history = await session.history.page({ direction: 'forward' }, deadline());
    assert.ok(history.messages.some(message => message.parts.some(part => part.type === 'tool_result' && JSON.parse(part.result.output).result?.output === 'completed before snapshot\n')));
    fixture.release('tool-stream-refresh-native'); assert.equal((await held.wait(deadline())).turn.state, 'succeeded');
    assert.equal((await session.cells.output(deadline())).preview, null);
  } finally { await fixture.close(); }
});
