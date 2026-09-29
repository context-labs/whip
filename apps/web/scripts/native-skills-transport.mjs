import assert from 'node:assert/strict';

/** Delays real metadata replies only; observes submission ACKs without replay. */
export async function skillsTransport(page, { delayMs = 1500 } = {}) {
  assert(Number.isSafeInteger(delayMs) && delayMs >= 1 && delayMs <= 1500);
  const frames = [], responses = [], errors = [], connections = new Set();
  const skillMethods = new Set(['host.skills.complete', 'skills.list']);
  let closed = false, nextConnection = 0, bytes = 0, overflow = false, timers = 0;
  const retain = (target, record) => {
    const size = Buffer.byteLength(JSON.stringify(record));
    if (frames.length + responses.length >= 20000 || bytes + size > 8 << 20) { overflow = true; throw new Error('Skill probe evidence bound exceeded'); }
    bytes += size; target.push(record);
  };
  const fail = error => { if (errors.length < 32) errors.push(String(error.stack ?? error).slice(0, 2048)); else overflow = true; };
  await page.routeWebSocket('**/api/v4/ws', socket => {
    if (closed) { socket.close(); return; }
    if (connections.size >= 128) { fail(new Error('Skill proxy connection bound reached')); socket.close(); return; }
    const upstream = socket.connectToServer(), connection = ++nextConnection, pending = new Map(), delayed = new Set();
    let live = true;
    const retire = () => {
      if (!live) return; live = false; connections.delete(retire); pending.clear();
      for (const timer of delayed) { clearTimeout(timer); timers--; } delayed.clear();
      socket.close(); upstream.close();
    };
    connections.add(retire); socket.onClose(retire); upstream.onClose(retire);
    socket.onMessage(message => {
      if (!live || closed) return;
      try {
        assert(Buffer.byteLength(message) <= 4 << 20);
        const request = JSON.parse(String(message));
        const record = { connection, id: request.id, method: request.method, params: request.params, sentAt: Date.now() };
        retain(frames, record);
        if (skillMethods.has(request.method) || request.method === 'sessions.submit') { assert(pending.size < 64); assert(!pending.has(request.id)); pending.set(request.id, record); }
        upstream.send(message);
      } catch (error) { fail(error); retire(); }
    });
    upstream.onMessage(message => {
      if (!live || closed) return;
      try {
        assert(Buffer.byteLength(message) <= 8 << 20);
        const reply = JSON.parse(String(message)), request = pending.get(reply.id);
        if (!request) { socket.send(message); return; }
        pending.delete(reply.id);
        if (request.method === 'sessions.submit') {
          retain(responses, { ...request, deliveredAt: Date.now(), frame: reply }); socket.send(message); return;
        }
        assert(timers < 64); timers++;
        const timer = setTimeout(() => {
          delayed.delete(timer); timers--;
          if (!live || closed) return;
          try { retain(responses, { ...request, deliveredAt: Date.now(), frame: reply }); socket.send(message); }
          catch (error) { fail(error); retire(); }
        }, delayMs);
        delayed.add(timer);
      } catch (error) { fail(error); retire(); }
    });
  });
  return { frames, responses, errors,
    close() { closed = true; for (const retire of [...connections]) retire(); },
    assertBounds() { assert.equal(overflow, false); assert.equal(timers, 0); assert.equal(connections.size, 0); assert.deepEqual(errors, []); },
    get bytes() { return bytes; },
  };
}
