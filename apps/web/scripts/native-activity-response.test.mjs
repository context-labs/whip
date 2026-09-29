import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { deadline, eventually, startFixture } from './native-fixture.mjs';
import { activityResponse } from './native-activity-response.mjs';

test('activity provider streams stay bounded and cancellation interrupts holds', async () => {
  for (const text of ['activity:prose', 'activity:list', 'activity:scroll']) {
    let bytes = 0, holds = 0;
    const response = await activityResponse({ text, last: { role: 'user' }, signal: new AbortController().signal,
      delta: value => { bytes += Buffer.byteLength(JSON.stringify(value)); }, wait: async () => { holds++; } });
    assert.equal(response.content, null); assert(bytes > 100 && bytes < 64 << 10); assert(holds > 0 && holds <= 17);
  }
  const controller = new AbortController();
  const pending = activityResponse({ text: 'activity:prose', last: { role: 'user' }, delta: () => {}, signal: controller.signal,
    wait: (_key, signal) => new Promise((_, reject) => signal.addEventListener('abort', () => reject(signal.reason), { once: true })) });
  controller.abort(new Error('fixture stopped'));
  await assert.rejects(pending, /fixture stopped/);
});

test('activity fixture records real files, child wait and bounded tool tree', { timeout: 120000 }, async () => {
  const fixture = await startFixture({ activityStreams: true });
  try {
    await mkdir(join(fixture.directory, 'source'));
    await writeFile(join(fixture.directory, 'persisted.md'), 'Persisted fixture body.');
    for (const name of ['one', 'two', 'three', ...Array.from({ length: 128 }, (_, index) => `file-${index}`)])
      await writeFile(join(fixture.directory, 'source', name + '.md'), `Read ${name}.`);
    const client = await fixture.connect('activity-fixture-check');
    const { root } = await fixture.createRoot(client), session = client.session(root.id);
    const policy = await session.permissions.policy(deadline());
    await session.permissions.setMode({ mode: 'automatic', expected_revision: policy.revision }, 'activity-policy', deadline());
    const run = text => session.submission([{ type: 'text', text }], text);
    const saved = run('activity:saved'); await saved.send(deadline());
    await eventually(async () => (await client.call('sessions.observe', { session_id: root.id, after: '0', limit: 100 }, deadline())).preview?.reasoning === 'Saved reasoning');
    fixture.release('activity-saved-reasoning');
    assert.equal((await saved.wait(deadline())).turn.state, 'succeeded');
    const history = await session.history.page({ direction: 'forward' }, deadline());
    assert(!history.messages.some(message => message.parts.some(part => part.type === 'reasoning' || part.text === 'Saved reasoning')));
    assert(history.messages.some(message => message.parts.some(part => part.type === 'tool_result' && part.result.output.includes('Persisted fixture body.'))));
    const work = run('activity:work'), admission = await work.send(deadline());
    const turnID = await eventually(async () => (await work.check(deadline())).evidence?.turn?.id);
    const reads = await eventually(async () => {
      const { items } = await session.turns.operations(turnID, {}, deadline());
      return items.some(item => item.capability === 'tools.fixture_wait' && item.state === 'dispatched') && items;
    });
    assert.equal(reads.filter(item => item.capability === 'files.read' && item.state === 'succeeded').length, 3);
    fixture.release('activity-reads');
    const child = await eventually(async () => {
      const { items } = await session.turns.operations(turnID, {}, deadline());
      return items.find(item => item.capability === 'agents.spawn' && item.state === 'succeeded')?.result?.value?.session_id;
    });
    assert.equal((await client.session(child).get(deadline())).parent_id, root.id);
    await eventually(async () => !(await session.activity(deadline())).execution_permit);
    fixture.release('activity-child');
    await eventually(async () => (await client.call('sessions.observe', { session_id: root.id, after: '0', limit: 100 }, deadline())).preview?.text === 'Review complete.');
    fixture.release('activity-complete');
    assert.equal((await work.wait(deadline())).turn.state, 'succeeded');
    assert.equal(admission.input.session_id, root.id);
    const tree = run('activity:tree'); await tree.send(deadline());
    const treeTurn = await eventually(async () => (await tree.check(deadline())).evidence?.turn?.id);
    const observed = await eventually(async () => {
      const state = await session.turns.get(treeTurn, deadline());
      if (state.finished_at) return { state, history: await session.history.page({ direction: 'backward' }, deadline()) };
      return (await session.turns.operations(treeTurn, { limit: 100 }, deadline())).items.length === 100 && { state };
    });
    assert.equal(observed.state.state, 'running', JSON.stringify(observed.history));
    fixture.release('activity-tree');
    assert.equal((await tree.wait(deadline())).turn.state, 'succeeded');
    const first = await session.turns.operations(treeTurn, { limit: 100 }, deadline());
    const last = await session.turns.operations(treeTurn, { limit: 100, after: first.items.at(-1).id }, deadline());
    const operations = [...first.items, ...last.items];
    assert.equal(operations.length, 129); assert.equal(operations.filter(item => item.capability === 'files.read' && item.state === 'succeeded').length, 128);
    for (const [kind, keys, expected] of [
      ['prose', ['activity-prose-more', 'activity-prose-end'], 'const answer = 42;'],
      ['list', ['activity-list-more', 'activity-list-end', 'activity-list-complete'], '3. **env-profiler**'],
      ['scroll', [...Array.from({ length: 16 }, (_, index) => `activity-scroll-${index}`), 'activity-scroll-complete'], 'Growth 15.'],
    ]) {
      const stream = run(`activity:${kind}`); await stream.send(deadline());
      await eventually(async () => (await client.call('sessions.observe', { session_id: root.id, after: '0', limit: 100 }, deadline())).preview?.text);
      for (const key of keys) fixture.release(key);
      assert.equal((await stream.wait(deadline())).turn.state, 'succeeded');
      const final = (await session.history.page({ direction: 'backward' }, deadline())).messages.at(-1);
      assert(final.parts.some(part => part.type === 'text' && part.text.includes(expected)));
      assert(Buffer.byteLength(JSON.stringify(final)) < 64 << 10);
    }
  } finally { await fixture.close(); }
});
