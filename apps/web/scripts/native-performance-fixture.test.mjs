import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { test } from 'node:test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

test('sixteen native providers stream concurrently and exact queued probes cancel without inference', { timeout: 120000 }, async () => {
  const fixture = await startFixture({ workers: 16, performanceStreams: true });
  try {
    const client = await fixture.connect('performance-fixture-proof'), runs = [];
    for (let index = 0; index < 16; index++) {
      const { root } = await fixture.createRoot(client), session = client.session(root.id);
      const command = session.submission([{ type: 'text', text: `hold:performance-stream-${index}` }], randomUUID());
      await command.send(deadline()); runs.push({ session, command });
    }
    await eventually(async () => {
      const states = await Promise.all(runs.map(({ session }) => client.call('sessions.observe', { session_id: session.id, after: '0', limit: 100 }, deadline())));
      return states.every(state => /delta-000[1-9]/.test(state.preview?.text ?? ''));
    }, { description: 'sixteen actual streaming native attempts' });
    const session = runs[0].session, requestID = randomUUID();
    const admission = await session.submit([{ type: 'text', text: 'accepted-probe' }], requestID, deadline());
    assert.equal(admission.input.state, 'queued');
    const queue = await session.inputs.page({ state: 'queued' }, deadline());
    assert.deepEqual(queue.items[0].identity, { client_id: client.clientID, request_id: requestID });
    assert.equal(queue.items[0].id, admission.input.id);
    assert.equal((await session.inputs.cancel(admission.input.id, deadline())).state, 'cancelled');
    for (const { session, command } of runs) {
      const activity = await session.activity(deadline());
      assert.ok(activity.active_turn); await session.cancelTurn(activity.active_turn.id, deadline());
      assert.equal((await command.wait(deadline())).turn.state, 'cancelled');
    }
    assert.equal((await fixture.effects()).length, 16);
  } finally { await fixture.close(); }
});
