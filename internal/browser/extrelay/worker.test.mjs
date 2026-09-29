import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import test from 'node:test';
import vm from 'node:vm';

const source = await fs.readFile(new URL('./extension/background.js', import.meta.url), 'utf8');
const tick = () => new Promise((resolve) => setImmediate(resolve));
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };

async function worker(options = {}) {
  const handlers = {};
  const sockets = [];
  const commands = [];
  const detaches = [];
  const attached = [];
  const listen = (name) => ({ addListener(handler) { handlers[name] = handler; } });
  class Socket {
    static OPEN = 1;
    readyState = 1;
    bufferedAmount = 0;
    sent = [];
    constructor(url) { this.url = url; sockets.push(this); }
    send(data) { this.sent.push(JSON.parse(data)); }
    close() { this.readyState = 3; }
  }
  const chrome = {
    runtime: { getURL: (path) => `chrome-extension://test/${path}`, onInstalled: listen('install'), onStartup: listen('startup') },
    tabs: { get: (id) => options.tab?.(id) ?? Promise.resolve({ title: 'Pinned', url: 'https://example.com' }), query: async () => [] },
    action: { onClicked: listen('click'), setBadgeText: async () => {}, setBadgeBackgroundColor: async () => {} },
    debugger: {
      attach: (tab) => { attached.push(tab.tabId); return options.attach?.(tab) ?? Promise.resolve(); },
      detach: async (tab) => { detaches.push(tab.tabId); },
      sendCommand: (tab, method, params) => { const result = deferred(); commands.push({ tab: tab.tabId, method, params, result }); return result.promise; },
      onEvent: listen('event'), onDetach: listen('detach'),
    },
  };
  vm.runInNewContext(source, {
    chrome, WebSocket: Socket, AbortController, TextEncoder, setTimeout, clearTimeout,
    setInterval: (fn) => { handlers.heartbeat = fn; },
    fetch: async () => ({ ok: true, text: async () => JSON.stringify(options.relay ?? { addr: '127.0.0.1:1234', token: 'a'.repeat(48) }) }),
  });
  await tick();
  async function select(id) { await handlers.click({ id }); const socket = sockets.at(-1); if (socket?.onopen) await socket.onopen(); return socket; }
  function request(socket, id) { socket.onmessage?.({ data: JSON.stringify({ id, method: 'Runtime.evaluate', params: { expression: 'effect()' }, sessionId: 'whip-ext' }) }); }
  return { handlers, sockets, commands, detaches, attached, select, request };
}

test('late result and old close stay with the retired pin', async () => {
  const w = await worker();
  assert.equal(w.attached.length, 0, 'ordinary startup cannot pin a browser');
  const old = await w.select(1);
  const oldClose = old.onclose;
  w.request(old, 7);
  await tick();
  assert.equal(w.commands.length, 1);
  const replacement = await w.select(2);
  assert.equal(old.readyState, 3);
  w.commands[0].result.resolve({ value: 'old private result' });
  oldClose();
  await tick();
  assert.equal(replacement.readyState, 1);
  assert.deepEqual(replacement.sent.map((value) => value.method), ['whip.attached']);
  assert.deepEqual(w.detaches, [1]);
  w.request(replacement, 7);
  await tick();
  w.commands[1].result.resolve({ value: 'new result' });
  await tick();
  assert.equal(replacement.sent.at(-1).result.value, 'new result');
});

test('late tab metadata cannot publish a newer selection', async () => {
  const metadata = deferred();
  const w = await worker({ tab: (id) => id === 1 ? metadata.promise : Promise.resolve({ title: 'Two', url: 'https://two.example' }) });
  await w.handlers.click({ id: 1 });
  const old = w.sockets[0];
  const opening = old.onopen();
  const replacement = await w.select(2);
  metadata.resolve({ title: 'Old', url: 'https://old.example' });
  await opening;
  assert.equal(old.sent.length, 0);
  assert.equal(replacement.sent[0].params.tabId, 2);
});

test('revocation during debugger attach cleans the late attachment', async () => {
  const attachment = deferred();
  const w = await worker({ attach: () => attachment.promise });
  const selecting = w.handlers.click({ id: 1 });
  await tick();
  w.handlers.detach({ tabId: 1 });
  await tick();
  attachment.resolve();
  await selecting;
  assert.equal(w.sockets.length, 0);
  assert.deepEqual(w.detaches, [1]);
});

test('pending command bound survives pin replacement and never replays', async () => {
  const w = await worker();
  const old = await w.select(1);
  for (let id = 1; id <= 32; id++) w.request(old, id);
  await tick();
  assert.equal(w.commands.length, 32);
  const next = await w.select(2);
  w.request(next, 1);
  await tick();
  assert.equal(w.commands.length, 32);
  assert.equal(next.readyState, 3);
  for (const command of w.commands) command.result.resolve({ value: 'retired' });
  await tick();
  assert.equal(next.sent.length, 1);
});

test('duplicate or foreign-session command retires selection before dispatch', async () => {
  for (const reason of ['duplicate', 'foreign']) {
    const w = await worker(); const socket = await w.select(1);
    if (reason === 'duplicate') { w.request(socket, 1); w.request(socket, 1); }
    else socket.onmessage({ data: JSON.stringify({ id: 1, method: 'Runtime.evaluate', sessionId: 'foreign' }) });
    await tick();
    assert.equal(socket.readyState, 3);
    assert.equal(w.commands.length, 0);
  }
});

test('relay configuration cannot redirect authenticated pin off loopback', async () => {
  for (const addr of ['attacker.example:1234', '127.0.0.1:65536', '127.0.0.1:1/path']) {
    const w = await worker({ relay: { addr, token: 'a'.repeat(48) } });
    await w.handlers.click({ id: 1 });
    assert.equal(w.attached.length, 0);
    assert.equal(w.sockets.length, 0);
  }
});
