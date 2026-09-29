import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { promisify } from 'node:util';
import { test } from 'node:test';
import { build } from 'esbuild';
import { Client } from '../../packages/sdk/dist/index.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';

const exec = promisify(execFile);
const options = () => ({ signal: AbortSignal.timeout(10_000) });
async function until(read, predicate) {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    const value = read(); if (predicate(value)) return value;
    await new Promise(resolve => setTimeout(resolve, 10));
  }
  throw new Error('Host observation did not settle');
}

test('shared app host owner reconnects real v4 observers without replay or stopping admitted work', { timeout: 120_000 }, async t => {
  const directory = await mkdtemp('/tmp/whip-app-hosts-');
  const binary = join(directory, 'runtime');
  const bundle = join(directory, 'hosts.mjs');
  const children = new Set();
  let hosts;
  const stop = async (child, signal = 'SIGTERM') => {
    if (child.exitCode !== null || child.signalCode !== null) return;
    const ended = once(child, 'exit'); child.kill(signal);
    const timer = setTimeout(() => child.kill('SIGKILL'), 5_000);
    try { await ended; } finally { clearTimeout(timer); }
  };
  t.after(async () => {
    hosts?.dispose();
    await Promise.all([...children].map(child => stop(child)));
    await rm(directory, { recursive: true, force: true });
  });
  await exec('go', ['build', '-race=false', '-o', binary, './cmd/whip-runtime'], { timeout: 60_000 });
  await build({ entryPoints: ['packages/app/src/hosts.ts'], outfile: bundle, bundle: true, format: 'esm', platform: 'node' });
  const { HostConnections } = await import(pathToFileURL(bundle).href);
  async function start(name, listen = '127.0.0.1:0') {
    const child = spawn(binary, ['-directory', join(directory, name), '-scripted', '-scripted-delay', '300ms', '-web', '-web-listen', listen], { stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env, HOME: directory } });
    children.add(child);
    let output = '';
    child.stderr.on('data', data => { output = (output + data.toString()).slice(-8192); });
    const ready = await new Promise((resolve, reject) => {
      let raw = '';
      const cleanup = () => { clearTimeout(timer); child.stdout.off('data', data); child.off('exit', exit); child.off('error', failed); };
      const failed = error => { cleanup(); reject(error); };
      const exit = () => failed(new Error('Runtime exited before ready: ' + output));
      const data = chunk => {
        raw += chunk.toString(); const end = raw.indexOf('\n');
        if (end >= 0) { try { const value = JSON.parse(raw.slice(0, end)); cleanup(); resolve(value); } catch (error) { failed(error); } }
      };
      const timer = setTimeout(() => failed(new Error('Runtime readiness timed out: ' + output)), 15_000);
      child.stdout.on('data', data); child.once('exit', exit); child.once('error', failed);
    });
    return { child, ready };
  }
  const local = await start('local');
  let remote = await start('remote');
  const storage = new Map();
  const effects = [];
  hosts = new HostConnections({ defaultEndpoint: local.ready.web, storage: {
    keys: () => [...storage.keys()], getItem: key => storage.get(key) ?? null, setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key),
  } }, { list: async () => [], put: async () => {}, delete: async () => {} }, {
    connected(runtimeID, client) { assert.equal(hosts.host(runtimeID).client, client); assert.ok(hosts.host(runtimeID).list); effects.push(['connected', runtimeID, client.processEpoch]); },
    detached(_client, runtimeID, detail) { effects.push(['detached', runtimeID, detail.recovering]); },
  });
  await hosts.connect(); await hosts.refreshProfiles();
  await hosts.save({ id: 'remote', name: 'Remote', url: remote.ready.web, connect_on_launch: true });
  const attached = await until(() => hosts.host(remote.ready.runtime_id), host => host?.state === 'connected');
  const original = attached.client, catalog = attached.list;
  await catalog.start();
  const tree = await original.createTree({ engine: 'quickjs', metadata: { title: 'Kept through reconnect', archived: false, pinned: false }, definition: original.builtins[0], working_directory: directory, overrides: { model: { provider: 'scripted', name: 'scripted', effort: '' }, automatic_title: false } }, 'app-host-root', options());
  await catalog.refresh(); assert.equal(catalog.getSnapshot().items[0].root_id, tree.root.id);
  await hosts.rename('remote', 'Build server');
  assert.equal(hosts.host(remote.ready.runtime_id).client, original);
  const oldReady = remote.ready;
  await stop(remote.child, 'SIGKILL');
  await assert.rejects(original.treeCatalog(options()));
  assert.equal(hosts.host(oldReady.runtime_id).state, 'stale');
  assert.equal(hosts.host(oldReady.runtime_id).client, undefined);
  assert.equal(hosts.host(oldReady.runtime_id).list, catalog);
  assert.equal(catalog.getSnapshot().items[0].root_id, tree.root.id);
  remote = await start('remote', new URL(oldReady.web).host);
  assert.equal(remote.ready.runtime_id, oldReady.runtime_id);
  assert.notEqual(remote.ready.process_epoch, oldReady.process_epoch);
  const recovered = await until(() => hosts.host(oldReady.runtime_id), host => host?.state === 'connected');
  assert.notEqual(recovered.client, original); assert.equal(recovered.list, catalog);
  assert.equal(recovered.client.processEpoch, remote.ready.process_epoch);
  await catalog.refresh(); assert.equal(catalog.getSnapshot().items.length, 1);
  await stop(local.child, 'SIGKILL');
  await assert.rejects(hosts.home().client.treeCatalog(options()));
  assert.equal(hosts.host(remote.ready.runtime_id).client, recovered.client);
  await assert.rejects(hosts.rename('remote', 'Cannot save while Local is offline'), /Reconnect Local/);
  const admission = await recovered.client.submit(tree.root.id, [{ type: 'text', text: 'Continue after this window detaches' }], 'window-detach', options());
  hosts.disconnect('remote');
  const observer = await Client.connect(unixSocket(remote.ready.socket), { clientID: recovered.client.clientID, expectedRuntimeID: remote.ready.runtime_id, ...options() });
  assert.equal((await observer.wait(admission.receipt.identity.request_id, options())).turn.state, 'succeeded');
  assert.ok(effects.some(event => event[0] === 'detached' && event[1] === oldReady.runtime_id && event[2] === true));
});
