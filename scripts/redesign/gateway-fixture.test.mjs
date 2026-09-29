import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { promisify } from 'node:util';
import { test } from 'node:test';
import { Client, DeliveryError } from '../../packages/sdk/dist/index.js';
import { browserSocket, browserContent } from '../../packages/sdk/dist/browser.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';

const exec = promisify(execFile);
const options = () => ({ signal: AbortSignal.timeout(10_000) });

test('production v4 gateway and native browser SDK preserve scoped delivery and shutdown', { timeout: 120_000 }, async t => {
  const directory = await mkdtemp('/tmp/whip-browser-');
  const binary = join(directory, 'runtime');
  let child;
  let output = '';
  const record = data => { output = (output + data.toString()).slice(-(1 << 20)); };
  const stop = async (signal = 'SIGTERM') => {
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    const ended = once(child, 'exit'); child.kill(signal);
    const timer = setTimeout(() => child.kill('SIGKILL'), 5_000);
    try { await ended; } finally { clearTimeout(timer); }
  };
  t.after(async () => { await stop(); await rm(directory, { recursive: true, force: true }); });
  await exec('go', ['build', '-race=false', '-o', binary, './cmd/whip-runtime'], { cwd: resolve('.'), timeout: 60_000 });
  const start = async (web = true) => {
    child = spawn(binary, ['-directory', join(directory, 'state'), '-scripted', '-scripted-delay', '300ms', ...(web ? ['-web', '-web-listen', '127.0.0.1:0'] : [])], { stdio: ['ignore', 'pipe', 'pipe'] });
    child.stderr.on('data', record); child.stdout.on('data', record);
    return await new Promise((resolve, reject) => {
      let raw = '';
      const cleanup = () => { clearTimeout(timer); child.stdout.off('data', data); child.off('exit', exit); child.off('error', failed); };
      const failed = error => { cleanup(); reject(error); };
      const exit = (code, signal) => failed(new Error(`Runtime exited ${code}/${signal}\n${output}`));
      const data = chunk => { raw += chunk; const end = raw.indexOf('\n'); if (end >= 0) { try { const value = JSON.parse(raw.slice(0, end)); cleanup(); resolve(value); } catch (error) { failed(error); } } };
      const timer = setTimeout(() => failed(new Error('Runtime readiness timed out\n' + output)), 15_000);
      child.stdout.on('data', data); child.on('exit', exit); child.on('error', failed);
    });
  };
  let ready = await start(false);
  assert.equal(ready.web, undefined); assert.ok(ready.process_epoch);
  await stop(); ready = await start();
  const pin = { expectedRuntimeID: ready.runtime_id, expectedProcessEpoch: ready.process_epoch };
  const web = await Client.connect(browserSocket(ready.web, pin), { clientID: 'browser', expectedRuntimeID: ready.runtime_id, ...options() });
  const local = await Client.connect(unixSocket(ready.socket), { clientID: 'local', expectedRuntimeID: ready.runtime_id, ...options() });
  const discovery = await (await fetch(ready.web + '/api/v4/web')).json();
  assert.equal(discovery.available, false); assert.equal(discovery.process_epoch, ready.process_epoch); assert.equal(discovery.max_content_bytes, 4 << 20);
  await assert.rejects(Client.connect(browserSocket(ready.web, { ...pin, expectedProcessEpoch: 'wrong' }), { clientID: 'wrong', ...options() }), error => error.kind === 'IDENTITY');
  assert.ok((await web.providerPresets()).items.length > 0);
  const createParams = engine => ({ engine, metadata: { title: null, pinned: false, archived: false }, definition: web.builtins[0], working_directory: directory, overrides: { model: { provider: 'scripted', name: 'scripted', effort: '' }, report_mode: 'message', automatic_title: false } });
  const first = await web.createTree(createParams('starlark'), 'browser-root', options());
  const second = await web.createTree(createParams('quickjs'), 'browser-root-second', options());
  const content = browserContent(ready.web, pin);
  const bytes = new Uint8Array(4 << 20); bytes.fill(65);
  await content.upload(first.root.id, 'same-reference', 'text/html', bytes, options());
  const other = new TextEncoder().encode('different owner');
  await content.upload(second.root.id, 'same-reference', 'text/plain', other, options());
  assert.deepEqual(await content.download(first.root.id, 'same-reference', options()), bytes);
  assert.deepEqual(await content.download(second.root.id, 'same-reference', options()), other);

  // Lose a real native response after host admission. The wrapper never repeats
  // a request; explicit receipt reads use an independent Unix connection.
  const NativeSocket = globalThis.WebSocket;
  let dropped = 0;
  class DropAcknowledgement extends NativeSocket {
    set onmessage(handler) {
      super.onmessage = handler === null ? null : event => {
        const value = JSON.parse(event.data);
        if (value.result?.receipt?.identity?.request_id?.startsWith('browser-lost-')) { dropped++; this.close(); return; }
        handler.call(this, event);
      };
    }
    get onmessage() { return super.onmessage; }
  }
  try {
    globalThis.WebSocket = DropAcknowledgement;
    for (const [index, tree] of [first, second].entries()) {
      const requestID = 'browser-lost-' + index;
      await assert.rejects(web.submit(tree.root.id, [{ type: 'text', text: 'survive a lost browser acknowledgement' }], requestID, options()), DeliveryError);
      const recovered = await local.call('receipts.get', { client_id: 'browser', request_id: requestID }, options());
      assert.equal(recovered.input.session_id, tree.root.id);
    }
  } finally { globalThis.WebSocket = NativeSocket; }
  assert.equal(dropped, 2);
  for (const index of [0, 1]) {
    for (;;) {
      const result = await local.call('receipts.get', { client_id: 'browser', request_id: 'browser-lost-' + index }, options());
      if (result.turn?.finished_at) { assert.equal(result.turn.state, 'succeeded'); break; }
      await new Promise(resolve => setTimeout(resolve, 20));
    }
  }
  const old = ready;
  await stop('SIGKILL');
  await assert.rejects(web.treeCatalog(options()), DeliveryError);
  ready = await start();
  assert.equal(ready.runtime_id, old.runtime_id); assert.notEqual(ready.process_epoch, old.process_epoch);
  await assert.rejects(Client.connect(browserSocket(ready.web, pin), { clientID: 'stale', ...options() }), error => error.kind === 'IDENTITY');
  const fresh = await Client.connect(browserSocket(ready.web, { expectedRuntimeID: ready.runtime_id, expectedProcessEpoch: ready.process_epoch }), { clientID: 'fresh', ...options() });
  assert.equal((await fresh.getTreeCreation('browser-root', options())).root.id, first.root.id);
  const root = await fetch(ready.web + '/'); assert.equal(root.status, 503); await root.body.cancel();
  const terminalSocket = new NativeSocket(ready.web.replace('http:', 'ws:') + '/api/v4/ws');
  await once(terminalSocket, 'open');
  terminalSocket.send(JSON.stringify({ jsonrpc: '2.0', id: 'init', method: 'initialize', params: { major: 4 } }));
  const [initialized] = await once(terminalSocket, 'message'); assert.equal(JSON.parse(initialized.data).result.network_client, true);
  terminalSocket.send(JSON.stringify({ jsonrpc: '2.0', id: 'terminal', method: 'terminals.open', params: {} }));
  const [denied] = await once(terminalSocket, 'message'); assert.equal(JSON.parse(denied.data).error.kind, 'NETWORK_RESTRICTED');
  const closed = once(terminalSocket, 'close'); await stop(); await closed;
  assert.equal(child.exitCode, 0, output);
  await assert.rejects(fetch(ready.web + '/api/v4/web'));
});
