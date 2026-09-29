import assert from 'node:assert/strict';

// Intercepts the actual unary v4 connection. Only content calls can be delayed
// or failed; initialization, observation and unrelated effects pass unchanged.
// Retained evidence contains identities and byte counts, never file bodies.
export async function contentTransport(page, owner) {
  const records = [], failures = [], held = new Set(), connections = new Set();
  let gate, fault, closed = false;
  const record = request => {
    const params = request.params;
    assert.equal(params.session_id, owner, 'Content transfer crossed session ownership');
    assert.equal(typeof params.reference_id, 'string');
    assert.ok(records.length < 4096, 'Content observation exceeded 4096 records');
    const bytes = params.data_base64 === undefined ? undefined : Buffer.from(params.data_base64, 'base64').length;
    if (bytes !== undefined) assert.ok(bytes <= (4 << 20), 'Upload exceeded the shared content bound');
    records.push({ method: request.method, owner: params.session_id, reference: params.reference_id, bytes });
  };
  await page.routeWebSocket('**/api/v4/ws', socket => {
    if (closed) { socket.close(); return; }
    const server = socket.connectToServer();
    let live = true;
    const retire = () => {
      if (!live) return;
      live = false; connections.delete(retire); socket.close(); server.close();
    };
    connections.add(retire);
    socket.onClose(retire); server.onClose(retire);
    socket.onMessage(async message => {
      try {
        const request = JSON.parse(String(message));
        if (['content.put', 'content.get', 'content.read'].includes(request.method)) {
          record(request);
          if (fault?.method === request.method && (!fault.reference || fault.reference === request.params.reference_id)) {
            const selected = fault; fault = undefined;
            selected.hit = { owner: request.params.session_id, reference: request.params.reference_id };
            // Fail before transmission. A failed attachment remains removable;
            // the production SDK must not resend this effect automatically.
            if (selected.disconnect) retire();
            else socket.send(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: { code: -32603, kind: 'INTERNAL', message: 'Synthetic unavailable content' } }));
            return;
          }
          if (gate?.method === request.method) await gate.promise;
        }
        if (closed || !live) return;
        server.send(message);
      } catch (error) {
        if (closed || !live) return;
        if (failures.length < 16) failures.push(String(error).slice(0, 2048));
        retire();
      }
    });
  });
  return {
    records,
    count: method => records.filter(record => record.method === method).length,
    hold(method) {
      assert.equal(closed, false, 'Content probe is closed');
      assert.equal(gate, undefined, 'Content holds must be sequential');
      let resolve; const promise = new Promise(done => { resolve = done; });
      const value = { method, promise }; gate = value;
      const release = () => { resolve(); held.delete(release); if (gate === value) gate = undefined; };
      held.add(release); return release;
    },
    failNext(method, { disconnect = true, reference } = {}) {
      assert.equal(fault, undefined, 'Unconsumed content fault');
      fault = { method, disconnect, reference }; return fault;
    },
    assertHealthy() { assert.deepEqual(failures, []); },
    close() {
      closed = true;
      for (const retire of connections) retire();
      for (const release of held) release();
    },
  };
}
