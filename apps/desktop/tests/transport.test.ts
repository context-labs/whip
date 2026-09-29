import assert from 'node:assert/strict';
import { EventEmitter, once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer, type Socket } from 'node:net';
import path from 'node:path';
import { setImmediate } from 'node:timers/promises';
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
    async open(id = 'one', connectionId = 'host', signal = new AbortController().signal, purpose?: 'browser-provider') {
      const accepted = once(server, 'connection');
      const opening = transports.open(id, connectionId, socketPath, signal, purpose);
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

test('forwards ordered UTF-8 frames over native Unix framing and acknowledges sent bytes', async t => {
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

test('forwards native content-sized frames above the retired one MiB cap', async t => {
  const f = await fixture(t);
  const peer = await f.open();
  const frame = JSON.stringify({ data_base64: Buffer.alloc(4 << 20, 42).toString('base64') });
  peer.write(frame + '\n');
  const received = await f.event(event => event.kind === 'frame');
  assert.equal((received as Extract<DesktopEvent, { kind: 'frame' }>).frame, frame);
  f.transports.acknowledge('one', 1);
  const chunks: Buffer[] = [];
  let size = 0;
  const echoed = new Promise<void>(resolve => peer.on('data', bytes => {
    chunks.push(bytes); size += bytes.length;
    if (size === Buffer.byteLength(frame) + 1) resolve();
  }));
  f.transports.send('one', 1, frame);
  await echoed;
  assert.equal(Buffer.concat(chunks).toString(), frame + '\n');
  assert.equal(f.events.some(event => event.kind === 'closed'), false);
});

test('rejects invalid UTF-8 rather than replacing bytes in a native response', async t => {
  const f = await fixture(t);
  const peer = await f.open();
  peer.write(Buffer.from([0xc3, 0x28, 0x0a]));
  const closed = await f.event(event => event.kind === 'closed');
  assert.match((closed as Extract<DesktopEvent, { kind: 'closed' }>).error, /UTF-8/);
  assert.equal(f.events.some(event => event.kind === 'frame'), false);
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
  peer.write('x'.repeat((8 << 20) + 1));
  const closed = await f.event(event => event.kind === 'closed');
  assert.match((closed as Extract<DesktopEvent, { kind: 'closed' }>).error, /frame limit/);
  assert.equal(f.events.some(event => event.kind === 'frame'), false);
});

test('rejects newline injection, wrong sequences and oversized UTF-8 outbound frames', async t => {
  const f = await fixture(t);
  for (const [index, frame] of ['one\ntwo', 'x'.repeat(8 << 20), 'é'.repeat(1 << 22)].entries()) {
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

test('releases only transports owned by the selected connection', async t => {
  const f = await fixture(t);
  for (let index = 0; index < 4; index++) await f.open(`handle-${index}`, index < 2 ? 'first' : 'second');
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

test('supports independent native hosts and retains a bounded reconnect allowance', async t => {
  const f = await fixture(t);
  for (let index = 0; index < 32; index++) await f.open(`transport-${index}`, `host-${Math.floor(index / 2)}`);
  const waiting = f.transports.open('overflow', 'other', f.socketPath, new AbortController().signal);
  f.transports.release('host-3');
  await waiting;
  f.transports.send('overflow', 1, 'admitted after release');
  const closed = f.events.filter(event => event.kind === 'closed').map(event => event.id);
  assert.deepEqual(closed.sort(), ['transport-6', 'transport-7']);
  await f.open('replacement', 'host-3');
});


test('bounds queued native opens and admits an app-shaped metadata burst once in FIFO order', async t => {
  const f = await fixture(t);
  let accepted = 0;
  f.server.on('connection', () => { accepted++; });
  for (let index = 0; index < 32; index++) await f.open(`held-${index}`);
  const admitted: number[] = [];
  const calls = Array.from({ length: 64 }, (_, index) => f.transports.open(`metadata-${index}`, 'other', f.socketPath, new AbortController().signal)
    .then(() => { admitted.push(index); f.transports.send(`metadata-${index}`, 1, 'metadata request'); f.transports.close(`metadata-${index}`); }));
  await assert.rejects(f.transports.open('overflow', 'other', f.socketPath, new AbortController().signal), /wait queue is full/);
  await assert.rejects(f.transports.open('metadata-0', 'other', f.socketPath, new AbortController().signal), /already in use/);
  assert.equal(accepted, 32);
  assert.deepEqual(admitted, []);
  f.transports.close('held-0');
  await Promise.all(calls);
  assert.deepEqual(admitted, Array.from({ length: 64 }, (_, index) => index));
  assert.equal(accepted, 96);
  assert.equal(f.events.filter(event => event.kind === 'sent').length, 64);
});

test('cancels a queued caller before it has a native handle and never opens or sends it later', async t => {
  const f = await fixture(t);
  let accepted = 0; f.server.on('connection', () => { accepted++; });
  for (let index = 0; index < 32; index++) await f.open(`held-${index}`);
  const controller = new AbortController();
  const call = f.transports.open('cancelled-wait', 'host', f.socketPath, controller.signal);
  const rejected = assert.rejects(call, /cancelled/);
  controller.abort(new Error('caller deadline'));
  await rejected;
  assert.equal(accepted, 32);
  const next = f.transports.open('next', 'host', f.socketPath, new AbortController().signal);
  f.transports.close('held-0'); await next;
  assert.equal(accepted, 33);
  assert.equal(f.events.filter(event => event.kind === 'closed' && event.id === 'cancelled-wait').length, 1);
  assert.equal(f.events.some(event => event.kind === 'sent' && event.id === 'cancelled-wait'), false);
});

test('host release removes its pending opens before freeing active slots; disposal rejects every remaining waiter', async t => {
  const f = await fixture(t);
  let accepted = 0; f.server.on('connection', () => { accepted++; });
  for (let index = 0; index < 32; index++) await f.open(`held-${index}`, index ? 'second' : 'first');
  const first = assert.rejects(f.transports.open('first-wait', 'first', f.socketPath, new AbortController().signal), /closed/);
  const second = f.transports.open('second-wait', 'second', f.socketPath, new AbortController().signal);
  f.transports.release('first'); await Promise.all([first, second]);
  assert.equal(accepted, 33);
  const pending = Array.from({ length: 4 }, (_, index) => assert.rejects(
    f.transports.open(`dispose-${index}`, 'second', f.socketPath, new AbortController().signal), /closed/));
  f.transports.dispose(); await Promise.all(pending);
  assert.equal(accepted, 33);
  await assert.rejects(f.transports.open('late', 'second', f.socketPath, new AbortController().signal), /closed/);
});

test('renderer close cancels a queued handle; its old deadline cannot retire a replacement', async t => {
  const f = await fixture(t);
  for (let index = 0; index < 32; index++) await f.open(`held-${index}`);
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const old = assert.rejects(f.transports.open('same', 'old-host', f.socketPath, new AbortController().signal), /closed/);
  f.transports.close('same'); await old;
  const replacement = f.transports.open('same', 'new-host', f.socketPath, new AbortController().signal);
  f.transports.close('held-0'); await replacement;
  t.mock.timers.tick(15_000);
  f.transports.send('same', 1, 'replacement remains owned');
  assert.equal(f.events.filter(event => event.kind === 'closed' && event.id === 'same').length, 1);
  assert.ok(f.events.some(event => event.kind === 'sent' && event.id === 'same'));
});

test('a queued open has its own finite deadline without allocating a socket', async t => {
  const f = await fixture(t);
  let accepted = 0; f.server.on('connection', () => { accepted++; });
  for (let index = 0; index < 32; index++) await f.open(`held-${index}`);
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const expired = assert.rejects(f.transports.open('expired', 'host', f.socketPath, new AbortController().signal), /wait timed out/);
  t.mock.timers.tick(15_000); await expired;
  f.transports.close('held-0');
  assert.equal(accepted, 32);
  assert.equal(f.events.filter(event => event.kind === 'closed' && event.id === 'expired').length, 1);
});


test('32 persistent browser peers leave 32 ordinary sockets available, with independent FIFO admission', async t => {
  const f = await fixture(t);
  const signal = new AbortController().signal;
  let accepted = 0; f.server.on('connection', () => { accepted++; });
  for (let index = 0; index < 32; index++) await f.open(`peer-${index}`, 'host', signal, 'browser-provider');
  for (let index = 0; index < 32; index++) await f.open(`read-${index}`);
  assert.equal(accepted, 64);
  const admitted: string[] = [];
  const read = f.transports.open('next-read', 'host', f.socketPath, signal).then(() => { admitted.push('read'); });
  const peer = f.transports.open('next-peer', 'host', f.socketPath, signal, 'browser-provider').then(() => { admitted.push('peer'); });
  f.transports.close('peer-0'); await peer;
  assert.deepEqual(admitted, ['peer']); // A blocked earlier ordinary read does not block the peer pool.
  f.transports.close('read-0'); await read;
  assert.deepEqual(admitted, ['peer', 'read']);
  assert.equal(accepted, 66);
  f.transports.send('next-read', 1, 'ordinary request');
  f.transports.send('next-peer', 1, 'browser request');
  f.transports.release('host');
  assert.equal(f.events.filter(event => event.kind === 'closed').length, 66);
});

test('frame contents never reclassify an ordinary socket or borrow spare browser capacity', async t => {
  const f = await fixture(t);
  for (let index = 0; index < 32; index++) await f.open(`read-${index}`);
  f.transports.send('read-0', 1, '{"method":"browser.provider.bind"}');
  let admitted = false;
  const queued = f.transports.open('read-overflow', 'host', f.socketPath, new AbortController().signal).then(() => { admitted = true; });
  await f.open('peer', 'host', new AbortController().signal, 'browser-provider');
  f.transports.close('peer');
  await setImmediate();
  assert.equal(admitted, false);
  f.transports.close('read-0'); await queued;
  assert.equal(admitted, true);
});

test('rejects forged purposes before any allocation and shares one 64-entry wait bound across pools', async t => {
  const f = await fixture(t);
  const signal = new AbortController().signal;
  let accepted = 0; f.server.on('connection', () => { accepted++; });
  for (const purpose of [null, '', 'ordinary', 'executor', 'shell', true, {}, ['browser-provider']])
    await assert.rejects(f.transports.open('forged', 'host', f.socketPath, signal, purpose), /purpose/);
  assert.equal(accepted, 0);
  for (let index = 0; index < 32; index++) {
    await f.open(`peer-${index}`, 'host', signal, 'browser-provider');
    await f.open(`read-${index}`);
  }
  const pending = Array.from({ length: 64 }, (_, index) => assert.rejects(
    f.transports.open(`wait-${index}`, 'host', f.socketPath, signal, index % 2 ? 'browser-provider' : undefined), /closed/));
  for (const purpose of [undefined, 'browser-provider'])
    await assert.rejects(f.transports.open('overflow', 'host', f.socketPath, signal, purpose), /wait queue is full/);
  f.transports.dispose(); await Promise.all(pending);
  assert.equal(accepted, 64);
});

test('a queued browser peer cancellation or deadline cannot allocate later or close another pool', async t => {
  const f = await fixture(t);
  const signal = new AbortController().signal;
  let accepted = 0; f.server.on('connection', () => { accepted++; });
  for (let index = 0; index < 32; index++) await f.open(`peer-${index}`, 'host', signal, 'browser-provider');
  await f.open('ordinary');
  const controller = new AbortController();
  const cancelled = assert.rejects(f.transports.open('cancelled-peer', 'host', f.socketPath, controller.signal, 'browser-provider'), /cancelled/);
  controller.abort(); await cancelled;
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const expired = assert.rejects(f.transports.open('expired-peer', 'host', f.socketPath, signal, 'browser-provider'), /wait timed out/);
  t.mock.timers.tick(15_000); await expired;
  f.transports.close('peer-0'); await setImmediate();
  assert.equal(accepted, 33);
  f.transports.send('ordinary', 1, 'unaffected ordinary request');
  assert.equal(f.events.some(event => event.kind === 'closed' && event.id === 'ordinary'), false);
});
