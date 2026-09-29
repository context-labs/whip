import assert from 'node:assert/strict';
import { test } from 'node:test';
import { sampleBrowserRetention } from './performance-retention.mjs';

function fixture({ fail, stall } = {}) {
  const calls = [], pending = new Set();
  let closed = false;
  return { calls, pending, session: {
    async send(method) {
      assert.equal(closed, false); calls.push(method);
      if (method === fail) throw new Error('sample failed');
      if (method === stall) return new Promise((_, reject) => pending.add(reject));
      if (method === 'Runtime.getHeapUsage') return { usedSize: 42 };
      if (method === 'Memory.getDOMCounters') return { nodes: 17 };
      return {};
    },
    async detach() {
      assert.equal(closed, false); closed = true; calls.push('detach');
      for (const reject of pending) reject(new Error('Detached'));
      pending.clear();
    },
  } };
}

for (const collectGarbage of [false, true]) test(`retention ${collectGarbage ? 'explicit diagnostic' : 'default natural'} owns one bounded session`, async () => {
  const value = fixture();
  const result = await sampleBrowserRetention({ newCDPSession: async () => value.session }, {}, collectGarbage ? { collectGarbage } : undefined);
  assert.deepEqual(value.calls, [...(collectGarbage ? ['HeapProfiler.collectGarbage'] : []), 'Runtime.getHeapUsage', 'Memory.getDOMCounters', 'detach']);
  assert.equal(result.usedSize, 42); assert.equal(result.nodes, 17);
  assert.equal(result.collection, collectGarbage ? 'forced-diagnostic' : 'natural');
  if (collectGarbage) assert.match(result.boundary, /does not establish natural retention or memory acceptance/);
});

test('failed natural sample cannot fall back to forced collection', async () => {
  const value = fixture({ fail: 'Runtime.getHeapUsage' });
  await assert.rejects(sampleBrowserRetention({ newCDPSession: async () => value.session }, {}), /sample failed/);
  assert.deepEqual(value.calls, ['Runtime.getHeapUsage', 'detach']);
});

test('stalled protocol reads or optional collection are detached and joined', { concurrency: true }, async t => {
  await Promise.all(['Runtime.getHeapUsage', 'Memory.getDOMCounters', 'HeapProfiler.collectGarbage'].map(stall => t.test(stall, async () => {
    const value = fixture({ stall });
    await assert.rejects(sampleBrowserRetention({ newCDPSession: async () => value.session }, {}, { collectGarbage: stall.startsWith('HeapProfiler') }), /10 seconds|Detached/);
    assert.equal(value.pending.size, 0); assert.equal(value.calls.filter(item => item === 'detach').length, 1);
  })));
});
