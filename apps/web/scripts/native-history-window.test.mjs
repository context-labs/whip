import assert from 'node:assert/strict';
import { test } from 'node:test';
import { randomUUID } from 'node:crypto';
import { createSessionView, createExecutionView, cellExecutionRows } from '../../../packages/sdk/dist/state.js';
import { readLargeMessage } from '../../../packages/app/src/conversation-history.ts';
import { deadline } from './native-fixture.mjs';
import { startHistoryFixture } from './native-history-fixture.mjs';

test('native history windows retain exact count/byte limits, canonical execution joins and explicit large-message reads', { timeout: 180000 }, async () => {
  const fixture = await startHistoryFixture(), owned = [];
  const keep = value => { owned.push(value); return value; };
  try {
    const client = await fixture.connect('native-history-windows');
    const session = client.session(fixture.history.root_id);
    const view = keep(createSessionView(session, { pollIntervalMs: 60000 }));
    await view.start();
    assert.equal(view.getSnapshot().history.messages.length, 100);
    const execution = keep(createExecutionView(session, view, { pollIntervalMs: 60000 }));
    await execution.start();
    let row = cellExecutionRows(execution.getSnapshot(), view.getSnapshot().history.messages).find(row => row.cell.id === fixture.history.cell_id);
    assert(row.call && row.result); assert.equal(row.operations.length, 128);
    assert(row.operations.every(operation => operation.session_id === session.id && operation.cell_id === row.cell.id));
    const call = row.call.message.id, result = row.result.message.id;
    let cursor = view.getSnapshot().history.olderCursor;
    for (let index = 0; index < 6; index++) {
      await view.loadOlder();
      const state = view.getSnapshot();
      assert(BigInt(state.history.olderCursor) < BigInt(cursor)); cursor = state.history.olderCursor;
      assert(state.history.messages.length <= 512); assert(state.retainedBytes <= 8 << 20);
      assert.equal(state.retainedBytes, Buffer.byteLength(JSON.stringify(state)));
    }
    const older = view.getSnapshot();
    assert.equal(older.history.messages.length, 512);
    assert.equal(older.history.latestMissing, true);
    assert(older.history.olderCursor !== null);
    // ExecutionView retains exact shared immutable bodies for its cells, even
    // before a refresh can hydrate them after the chat window moves away.
    row = cellExecutionRows(execution.getSnapshot(), older.history.messages).find(row => row.cell.id === fixture.history.cell_id);
    assert.equal(row.call.message.id, call); assert.equal(row.result.message.id, result); assert.equal(row.operations.length, 128);
    await view.latest();
    row = cellExecutionRows(execution.getSnapshot(), view.getSnapshot().history.messages).find(row => row.cell.id === fixture.history.cell_id);
    assert.equal(row.call.message.id, call); assert.equal(row.result.message.id, result);
    assert.equal(view.getSnapshot().history.messages.at(-1).sequence, '10000');
    const bytes = keep(createSessionView(session, { maxBytes: 4096, pollIntervalMs: 60000 }));
    await bytes.start();
    assert(bytes.getSnapshot().history.messages.length < 100);
    assert(bytes.getSnapshot().retainedBytes <= 4096); assert.equal(bytes.getSnapshot().truncated, true);
    assert.deepEqual(await fixture.effects(), [], 'Synthetic history window inspection never executes a provider request');

    const { root } = await fixture.createRoot(client);
    const largeSession = client.session(root.id), text = 'Canonical large message. ' + 'κ'.repeat(16384);
    const command = largeSession.submission([{ type: 'text', text }], randomUUID());
    await command.send(deadline()); assert.equal((await command.wait(deadline())).turn.state, 'succeeded');
    const bounded = keep(createSessionView(largeSession, { maxBytes: 4096, pollIntervalMs: 60000 }));
    await bounded.start();
    const gaps = bounded.getSnapshot().history.gaps;
    assert.equal(gaps.length, 2); assert.equal(bounded.getSnapshot().history.messages.length, 0);
    assert(gaps.every(gap => gap.reason === 'message_too_large' && gap.bytes > 32768));
    assert(bounded.getSnapshot().retainedBytes <= 4096);
    for (const gap of gaps) {
      const body = await readLargeMessage(largeSession, gap.messageID, gap.sequence, 1 << 20, deadline().signal);
      assert.equal(JSON.parse(new TextDecoder().decode(body))[0].text, text);
      await assert.rejects(readLargeMessage(session, gap.messageID, gap.sequence, 1 << 20, deadline().signal), error => error.kind === 'NOT_FOUND');
      await assert.rejects(readLargeMessage(largeSession, gap.messageID, gap.sequence, 1024, deadline().signal), /exceeds this/);
    }
    assert.deepEqual(bounded.getSnapshot().history.gaps, gaps, 'Explicit inspection does not enlarge the retained transcript window');
    await assert.rejects(readLargeMessage(largeSession, gaps[0].messageID, gaps[0].sequence, 1 << 20, AbortSignal.abort()), error => error.name === 'AbortError');
  } finally {
    const settled = await Promise.allSettled(owned.reverse().map(value => value.dispose()));
    try { await fixture.close(); }
    finally {
      const failures = settled.filter(result => result.status === 'rejected').map(result => result.reason);
      if (failures.length) throw new AggregateError(failures, 'History view cleanup failed');
    }
  }
});
