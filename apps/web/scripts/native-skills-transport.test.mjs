import assert from 'node:assert/strict';
import test from 'node:test';
import { skillsTransport } from './native-skills-transport.mjs';

function endpoint() {
  return { sent: [], closed: false, onMessage(callback) { this.message = callback; }, onClose(callback) { this.closeCallback = callback; },
    send(message) { assert(!this.closed); this.sent.push(message); }, close() { if (this.closed) return; this.closed = true; this.closeCallback?.(); } };
}
async function fixture() {
  let connect;
  const proxy = await skillsTransport({ async routeWebSocket(_pattern, callback) { connect = callback; } }, { delayMs: 10 });
  const socket = endpoint(), upstream = endpoint(); socket.connectToServer = () => upstream; connect(socket);
  return { proxy, socket, upstream };
}
test('forwards one request and genuine delayed reply with exact connection identity', async () => {
  const { proxy, socket, upstream } = await fixture();
  const request = JSON.stringify({ id: 'same-id', method: 'skills.list', params: { session_id: 'root', prefix: '', limit: 100 } });
  const reply = JSON.stringify({ id: 'same-id', result: { items: [], next_after: null } });
  socket.message(request); upstream.message(reply); assert.deepEqual(upstream.sent, [request]); assert.deepEqual(socket.sent, []);
  await new Promise(resolve => setTimeout(resolve, 25)); assert.deepEqual(socket.sent, [reply]);
  assert.equal(proxy.responses[0].connection, proxy.frames[0].connection); proxy.close(); proxy.assertBounds();
});
test('retirement cancels held replies and cannot dispatch or report delayed cleanup failures', async () => {
  const { proxy, socket, upstream } = await fixture();
  socket.message(JSON.stringify({ id: 'id', method: 'host.skills.complete', params: { scope: 'global' } }));
  upstream.message(JSON.stringify({ id: 'id', result: { candidates: [], truncated: false } }));
  proxy.close(); proxy.close(); await new Promise(resolve => setTimeout(resolve, 25));
  assert(socket.closed && upstream.closed); assert.equal(upstream.sent.length, 1); assert.deepEqual(socket.sent, []); assert.deepEqual(proxy.responses, []); proxy.assertBounds();
});
