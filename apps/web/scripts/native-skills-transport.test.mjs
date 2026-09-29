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
  return { proxy, socket, upstream, connect };
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

test('outgoing submission is not acceptance; only its connection-local real ACK supplies readiness', async () => {
  const { proxy, socket, upstream, connect } = await fixture();
  const otherSocket = endpoint(), otherUpstream = endpoint(); otherSocket.connectToServer = () => otherUpstream; connect(otherSocket);
  const request = JSON.stringify({ id: 'same-id', method: 'sessions.submit', params: { session_id: 'root', identity: { client_id: 'client', request_id: 'first' }, parts: [{ type: 'text', text: 'synthetic' }] } });
  socket.message(request);
  const submission = proxy.frames[0];
  const acknowledged = () => proxy.responses.find(response => response.connection === submission.connection && response.id === submission.id && response.method === submission.method);
  assert.deepEqual(upstream.sent, [request]); assert.equal(acknowledged(), undefined);
  otherSocket.message(request);
  const foreign = JSON.stringify({ id: 'same-id', result: { turn: { id: 'foreign' }, input: { id: 'foreign-input' } } });
  otherUpstream.message(foreign);
  assert.deepEqual(otherSocket.sent, [foreign]); assert.equal(acknowledged(), undefined);
  const reply = JSON.stringify({ id: 'same-id', result: { turn: null, input: { id: 'accepted-input', state: 'queued', turn_id: null } } });
  upstream.message(reply);
  assert.deepEqual(socket.sent, [reply]); assert.equal(acknowledged().frame.result.input.id, 'accepted-input');
  assert.equal(acknowledged().frame.result.turn, null, 'Admission can acknowledge queued input before a turn is claimed');
  assert.deepEqual(upstream.sent, [request], 'Waiting for acceptance never resends the mutation');
  proxy.close(); proxy.assertBounds();
});
