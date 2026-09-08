import assert from 'node:assert/strict';
import { EventEmitter, once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer, type Socket } from 'node:net';
import path from 'node:path';
import test, { type TestContext } from 'node:test';
import type { DesktopEvent } from '@whip/app/desktop-bridge';
import { DesktopTransports, validHandle } from '../src/transport';

async function fixture(t: TestContext) {
  // macOS Unix socket paths have a 104-byte limit; its normal TMPDIR can exceed it.
  const directory = await mkdtemp('/tmp/whip-transport-');
  const socketPath = path.join(directory, 's');
  const peers = new Set<Socket>();
  const server = createServer(peer => {
    peers.add(peer); peer.on('error', () => {}); peer.once('close', () => peers.delete(peer));
  });
  server.listen(socketPath);
  await once(server, 'listening');
  const events: DesktopEvent[] = [];
  const changed = new EventEmitter();
  const transports = new DesktopTransports(event => { events.push(event); changed.emit('change'); });
  t.after(async () => {
    transports.dispose();
    for (const peer of peers) peer.destroy();
    await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    await rm(directory, { recursive: true, force: true });
  });
  return { transports, events, socketPath, server,
    async open(id = 'one', connectionId = 'host', signal = new AbortController().signal) {
      const accepted = once(server, 'connection');
      const opening = transports.open(id, connectionId, socketPath, signal);
      const [peer] = await accepted;
      await opening;
      return peer as Socket;
    },
    async event(predicate: (event: DesktopEvent) => boolean) {
      const existing = events.find(predicate);
      if (existing) return existing;
      return new Promise<DesktopEvent>((resolve, reject) => {
        const timeout = setTimeout(() => { cleanup(); reject(new Error('Expected transport event did not arrive')); }, 2000);
        const cleanup = () => { clearTimeout(timeout); changed.off('change', check); };
        const check = () => { const event = events.find(predicate); if (event) { cleanup(); resolve(event); } };
        changed.on('change', check);
      });
    },
  };
}

test('forwards ordered UTF-8 frames over the real SDK Unix transport and acknowledges sent bytes', async t => {
  const f = await fixture(t);
  const peer = await f.open();
  // Deliver an incomplete UTF-8 code point after the first complete frame.
  peer.write(Buffer.concat([Buffer.from('first\n'), Buffer.from([0xc3])]));
  await f.event(event => event.kind === 'frame' && event.sequence === 1);
  assert.equal(f.events.filter(event => event.kind === 'frame').length, 1);
  peer.write(Buffer.concat([Buffer.from([0xa9]), Buffer.from(' second\nthird\n')]));
  await f.event(event => event.kind === 'frame' && event.sequence === 3);
  assert.deepEqual(f.events.filter(event => event.kind === 'frame'), [
    { kind: 'frame', id: 'one', sequence: 1, frame: 'first' },
    { kind: 'frame', id: 'one', sequence: 2, frame: 'é second' },
    { kind: 'frame', id: 'one', sequence: 3, frame: 'third' },
  ]);
  for (const sequence of [1, 2, 3]) f.transports.acknowledge('one', sequence);
  const received = once(peer, 'data');
  f.transports.send('one', 1, '{"message":"hello"}');
  assert.equal(String((await received)[0]), '{"message":"hello"}\n');
  assert.ok(f.events.some(event => event.kind === 'sent' && event.sequence === 1 && event.buffered >= 0));
  assert.equal(f.events.some(event => event.kind === 'closed'), false);
});

test('closes on out-of-order acknowledgements without acknowledging another connection', async t => {
  const f = await fixture(t);
  const first = await f.open('one', 'first-host');
  await f.open('two', 'second-host');
  first.write('first\nsecond\n');
  await f.event(event => event.kind === 'frame' && event.id === 'one' && event.sequence === 2);
  f.transports.acknowledge('one', 2);
  assert.ok(f.events.some(event => event.kind === 'closed' && event.id === 'one' && /acknowledgement/.test(event.error)));
  f.transports.send('two', 1, 'still connected');
  assert.equal(f.events.some(event => event.kind === 'closed' && event.id === 'two'), false);
});

test('bounds unacknowledged frame count, including zero-byte frames', async t => {
  const f = await fixture(t);
  const peer = await f.open();
  peer.write('\n'.repeat(1025));
  const closed = await f.event(event => event.kind === 'closed');
  assert.match((closed as Extract<DesktopEvent, { kind: 'closed' }>).error, /queue exceeded/);
  assert.equal(f.events.filter(event => event.kind === 'frame').length, 1024);
});

test('bounds unacknowledged receive bytes independently of frame count', async t => {
  const f = await fixture(t);
  const peer = await f.open();
  peer.write(('x'.repeat(256 << 10) + '\n').repeat(33));
  const closed = await f.event(event => event.kind === 'closed');
  assert.match((closed as Extract<DesktopEvent, { kind: 'closed' }>).error, /queue exceeded/);
  assert.equal(f.events.filter(event => event.kind === 'frame').length, 32);
});

test('refuses oversized fragmented input before it forms a complete frame', async t => {
  const f = await fixture(t);
  const peer = await f.open();
  peer.write('x'.repeat((1 << 20) + 1));
  const closed = await f.event(event => event.kind === 'closed');
  assert.match((closed as Extract<DesktopEvent, { kind: 'closed' }>).error, /frame limit/);
  assert.equal(f.events.some(event => event.kind === 'frame'), false);
});

test('rejects newline injection, wrong sequences and oversized UTF-8 outbound frames', async t => {
  const f = await fixture(t);
  for (const [index, frame] of ['one\ntwo', 'x'.repeat(1 << 20), 'é'.repeat(1 << 19)].entries()) {
    const id = `invalid-${index}`;
    await f.open(id);
    f.transports.send(id, 1, frame);
    assert.ok(f.events.some(event => event.kind === 'closed' && event.id === id));
    assert.equal(f.events.some(event => event.kind === 'sent' && event.id === id), false);
  }
  await f.open('sequence');
  f.transports.send('sequence', 2, 'second before first');
  assert.ok(f.events.some(event => event.kind === 'closed' && event.id === 'sequence'));
});

test('enforces connection limits and releases only transports owned by the selected connection', async t => {
  const f = await fixture(t);
  for (let index = 0; index < 4; index++) await f.open(`handle-${index}`, index < 2 ? 'first' : 'second');
  await assert.rejects(f.transports.open('overflow', 'third', f.socketPath, new AbortController().signal), /limit/);
  f.transports.release('first');
  assert.deepEqual(f.events.filter(event => event.kind === 'closed').map(event => event.id), ['handle-0', 'handle-1']);
  await f.open('replacement', 'third');
  f.transports.send('handle-2', 1, 'still connected');
  f.transports.dispose(); f.transports.dispose();
  assert.equal(f.events.filter(event => event.kind === 'closed').length, 5);
});

test('bounds the outbound queue when the Unix peer stops consuming data', async t => {
  const f = await fixture(t);
  const peer = await f.open();
  peer.pause();
  for (let sequence = 1; sequence <= 128 && !f.events.some(event => event.kind === 'closed'); sequence++)
    f.transports.send('one', sequence, 'x'.repeat(256 << 10));
  assert.ok(f.events.some(event => event.kind === 'closed' && /queue limit/.test(event.error)));
  for (const event of f.events) if (event.kind === 'sent') assert.ok(event.buffered <= 8 << 20);
});

test('cancels pending and connected transports exactly once without exhausting handles', async t => {
  const f = await fixture(t);
  const cancelled = new AbortController(); cancelled.abort();
  await assert.rejects(f.transports.open('cancelled', 'host', f.socketPath, cancelled.signal));
  const controller = new AbortController();
  const peer = await f.open('connected', 'host', controller.signal);
  peer.resume();
  const closed = once(peer, 'close');
  controller.abort();
  await closed;
  f.transports.close('connected');
  assert.equal(f.events.filter(event => event.kind === 'closed' && event.id === 'connected').length, 1);
  await f.open('replacement');
});

test('an obsolete pending open cannot close or adopt a reused transport handle', async t => {
  const f = await fixture(t);
  const pending = f.transports.open('reused', 'old-host', f.socketPath, new AbortController().signal);
  const rejected = assert.rejects(pending);
  f.transports.close('reused');
  const replacement = f.transports.open('reused', 'new-host', f.socketPath, new AbortController().signal);
  await Promise.all([rejected, replacement]);
  f.transports.send('reused', 1, 'replacement');
  assert.equal(f.events.filter(event => event.kind === 'closed' && event.id === 'reused').length, 1);
  assert.ok(f.events.some(event => event.kind === 'sent' && event.id === 'reused'));
});

test('validates handles before admitting native transport state', () => {
  for (const value of ['', '../socket', 'with space', '\n', 'a'.repeat(129), null, 7])
    assert.throws(() => validHandle(value), /handle/);
  validHandle('valid-handle-123');
});
