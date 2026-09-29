import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { test } from 'node:test';
import { traceInput } from './performance-trace.mjs';

class Session extends EventEmitter {
  active = new Set(); detached = 0;
  constructor(stall) { super(); this.stall = stall; }
  async send(method) {
    if (this.detached) throw new Error('Detached');
    if (method === 'Tracing.start') this.emit('Tracing.bufferUsage', { percentFull: 0.25 });
    if (method === this.stall) return new Promise((_, reject) => this.active.add(reject));
    if (method === 'Tracing.end' && this.stall !== 'completion') queueMicrotask(() => this.emit('Tracing.tracingComplete', { stream: 'trace', traceFormat: 'json', dataLossOccurred: this.stall === 'loss' }));
    return method === 'IO.read' ? { data: '{"traceEvents":[]}', eof: true } : {};
  }
  async detach() { this.detached++; for (const reject of this.active) reject(new Error('Detached')); this.active.clear(); }
}

test('trace finish writes one complete artifact and retires its borrowed session once', async () => {
  const directory = await mkdtemp('/tmp/whip-trace-test-'), session = new Session();
  try {
    const trace = await traceInput({ newCDPSession: async () => session }, {}, directory + '/trace.json');
    const first = trace.finish(); assert.equal(trace.finish(), first);
    assert.equal((await first).bytes, 18);
    assert.deepEqual((await first).bufferUsage, { samples: 1, maximumPercentFull: 0.25 });
    assert.deepEqual(JSON.parse(await readFile(directory + '/trace.json', 'utf8')), { traceEvents: [] });
    assert.equal(session.detached, 1); assert.equal(session.listenerCount('Tracing.tracingComplete'), 0);
    assert.equal(session.listenerCount('Tracing.bufferUsage'), 0);
  } finally { await rm(directory, { recursive: true, force: true }); }
});

test('trace deadlines retire and join stalled start, end, completion, read and close work', { concurrency: true }, async t => {
  await Promise.all(['Tracing.start', 'Tracing.end', 'completion', 'IO.read', 'IO.close'].map(stall => t.test(stall, async () => {
    const directory = await mkdtemp('/tmp/whip-trace-stall-'), session = new Session(stall);
    try {
      const started = traceInput({ newCDPSession: async () => session }, {}, directory + '/trace.json');
      if (stall === 'Tracing.start') await assert.rejects(started, /startup exceeded 10 seconds/);
      else await assert.rejects((await started).finish(), /10 seconds/);
      assert.equal(session.detached, 1); assert.equal(session.active.size, 0);
      assert.equal(session.listenerCount('Tracing.tracingComplete'), 0);
      assert.equal(session.listenerCount('Tracing.bufferUsage'), 0);
      await assert.rejects(readFile(directory + '/trace.json'), { code: 'ENOENT' });
    } finally { await rm(directory, { recursive: true, force: true }); }
  })));
});

test('lost-data trace remains explicitly incomplete and cannot publish a complete artifact', async () => {
  const directory = await mkdtemp('/tmp/whip-trace-loss-'), session = new Session('loss');
  try {
    const trace = await traceInput({ newCDPSession: async () => session }, {}, directory + '/trace.json');
    await assert.rejects(trace.finish(), /lost data.*fraction 0.25.*incomplete diagnostic/);
    assert.deepEqual(JSON.parse(await readFile(directory + '/trace.json.partial', 'utf8')), { traceEvents: [] });
    await assert.rejects(readFile(directory + '/trace.json'), { code: 'ENOENT' });
    assert.equal(session.detached, 1); assert.equal(session.active.size, 0);
    assert.equal(session.listenerCount('Tracing.bufferUsage'), 0);
  } finally { await rm(directory, { recursive: true, force: true }); }
});
