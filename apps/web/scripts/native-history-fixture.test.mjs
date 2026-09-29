import assert from 'node:assert/strict';
import { test } from 'node:test';
import { deadline } from './native-fixture.mjs';
import { startHistoryFixture } from './native-history-fixture.mjs';

test('canonical large history retains exact owner, counts, child isolation and operation pages', { timeout: 180_000 }, async () => {
  const fixture = await startHistoryFixture();
  try {
    const client = await fixture.connect('history-proof');
    const session = client.session(fixture.history.root_id);
    const page = await session.history.page({ direction: 'backward', limit: 100 }, deadline());
    assert.equal(page.messages.at(-1).sequence, '10000');
    assert.ok(page.messages.length <= 100);
    const reference = await session.content.get(fixture.history.large_content, deadline());
    assert.equal(reference.size, '1400000');
    const text = new TextDecoder('utf-8', { fatal: true }).decode(await session.content.readBytes(reference, { maxBytes: 1_400_000, ...deadline() }));
    assert.equal(Buffer.byteLength(text), 1_400_000);
    for (const child of fixture.history.children) {
      const childSession = client.session(child.id);
      assert.equal((await childSession.get(deadline())).lifecycle, 'stopped');
      assert.equal((await childSession.history.snapshot(deadline())).message_count, '100');
    }
    await assert.rejects(client.session(fixture.history.children[0].id).content.get(reference.id, deadline()), error => error.kind === 'NOT_FOUND');
    const cell = await session.cells.get(fixture.history.cell_id, deadline());
    let after, count = 0;
    do {
      const page = await session.turns.operations(cell.turn_id, { after, limit: 64 }, deadline());
      assert.ok(page.items.every(item => item.state === 'succeeded' && item.cell_id === cell.id));
      count += page.items.length; after = page.items.length === 64 ? page.items.at(-1).id : undefined;
    } while (after);
    assert.equal(count, 128);
    assert.deepEqual(await fixture.effects(), [], 'Synthetic history must not contact the model');
  } finally { await fixture.close(); }
});
