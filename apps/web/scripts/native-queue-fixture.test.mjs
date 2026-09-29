import assert from 'node:assert/strict';
import test from 'node:test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

for (const engine of ['starlark', 'quickjs']) test(`${engine}: native steering preserves exact parts, one target, FIFO and foreign-owner rejection`, { timeout: 120_000 }, async () => {
  const fixture = await startFixture({ queueStreams: true });
  try {
    const client = await fixture.connect();
    const { root } = await fixture.createRoot(client, { engine });
    const session = client.session(root.id);
    const submit = async (text, parts = []) => {
      const work = session.submission([{ type: 'text', text }, ...parts], crypto.randomUUID());
      await work.send(deadline()); return work;
    };
    const opening = await submit('queue:boundary');
    const turn = await eventually(async () => (await opening.check(deadline())).evidence?.turn);
    const beforeEffects = (await fixture.effects()).length;
    const a = await submit('Queue A');
    const image = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=', 'base64');
    const ref = await session.content.upload('queue-image', 'image/png', image, deadline());
    const b = await submit('Queue B with images', [{ type: 'content', reference_id: ref.id }]);
    const c = await submit('Queue C');
    const input = (await b.check(deadline())).evidence.input;
    const { root: other } = await fixture.createRoot(client, { engine });
    await assert.rejects(client.session(other.id).inputs.get(input.id, deadline()), error => error.kind === 'NOT_FOUND');
    await assert.rejects(client.session(other.id).inputs.steer(input.id, turn.id, crypto.randomUUID(), deadline()), error => error.kind === 'NOT_FOUND');
    const edit = crypto.randomUUID();
    const promoted = await session.inputs.steer(input.id, turn.id, edit, deadline());
    assert.equal(promoted.input.steering.consumed, false);
    assert.deepEqual((await session.inputs.steer(input.id, turn.id, edit, deadline())).input.parts, input.parts);
    fixture.release('queue-boundary');
    await eventually(async () => (await session.inputs.get(input.id, deadline())).steering.consumed);
    const consumed = await session.inputs.get(input.id, deadline());
    assert.equal(consumed.turn_id, turn.id);
    assert.deepEqual(consumed.parts, input.parts);
    assert.deepEqual(Buffer.from(await session.content.readBytes(ref.id, { maxBytes: 4 << 20, ...deadline() })), image);
    fixture.release('queue-finish');
    for (const work of [opening, b, a, c]) assert.equal((await work.wait(deadline())).turn.state, 'succeeded');
    assert.deepEqual((await fixture.effects()).slice(beforeEffects).filter(text => text.startsWith('Queue ')), ['Queue B with images', 'Queue A', 'Queue C']);
    const history = (await session.history.page({ direction: 'forward', limit: 100 }, deadline())).messages;
    assert.equal(history.filter(message => message.input_id === input.id).length, 1);
    assert.equal(history.find(message => message.input_id === input.id).opening_input, false);
    const ending = await submit('hold:queue-target-end');
    const endingTurn = await eventually(async () => (await ending.check(deadline())).evidence?.turn);
    const fallback = await submit('Keep after turn ends');
    const waiting = (await fallback.check(deadline())).evidence.input;
    await session.inputs.steer(waiting.id, endingTurn.id, crypto.randomUUID(), deadline());
    await session.cancelTurn(endingTurn.id, deadline());
    assert.equal((await ending.wait(deadline())).turn.state, 'cancelled');
    const continued = await fallback.wait(deadline());
    assert.equal(continued.turn.state, 'succeeded');
    assert.notEqual(continued.turn.id, endingTurn.id);
    assert.equal(continued.input.steering.consumed, false);
    assert.equal((await fixture.effects()).filter(text => text === 'Keep after turn ends').length, 1);
  } finally { await fixture.close(); }
});
