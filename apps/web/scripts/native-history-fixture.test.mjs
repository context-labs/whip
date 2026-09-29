import assert from 'node:assert/strict';
import { test } from 'node:test';
import { lstat } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { deadline, eventually } from './native-fixture.mjs';
import { startHistoryFixture } from './native-history-fixture.mjs';

for (const managedDirectory of [false, true]) test(`canonical large history retains exact owner, counts, child isolation and operation pages (managed=${managedDirectory})`, { timeout: 180_000 }, async () => {
  const fixture = await startHistoryFixture({ performanceStreams: true, managedDirectory });
  try {
    const state = dirname(fixture.info.socket);
    assert.equal(state, join(fixture.directory, managedDirectory ? 'home/.whipcode/runtime-v4' : 'state'));
    assert.equal((await lstat(state)).isSymbolicLink(), false);
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
    const head = await client.call('context.head', { session_id: session.id }, deadline());
    assert.equal(head.compaction_id, fixture.history.compaction_id);
    const summary = await client.call('context.compaction', { session_id: session.id, compaction_id: head.compaction_id }, deadline());
    assert.equal(summary.metadata.through_sequence, '9996');
    assert.match(summary.text, /Synthetic fixture summary/);
    // The same owner can now execute new work within the actual model context
    // limits while all 10k raw messages and explicit content remain inspectable.
    const command = session.submission([{ type: 'text', text: 'hold:performance-stream-history-proof' }], 'after-history');
    await command.send(deadline());
    await eventually(async () => (await client.call('sessions.observe', { session_id: session.id, after: '10000', limit: 100 }, deadline())).preview?.text.includes('delta-0001'), { description: 'real provider stream after selected synthetic compaction' });
    const activity = await session.activity(deadline()); await session.cancelTurn(activity.active_turn.id, deadline());
    assert.equal((await command.wait(deadline())).turn.state, 'cancelled');
    assert.deepEqual(await fixture.effects(), ['hold:performance-stream-history-proof']);
    assert.equal((await session.history.snapshot(deadline())).message_count, '10001');
  } finally { await fixture.close(); }
});
