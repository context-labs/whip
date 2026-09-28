import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { cp, mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import net from 'node:net';
import { basename, join, resolve } from 'node:path';
import { promisify } from 'node:util';
import { test } from 'node:test';
import { Client, DeliveryError, RemoteError } from '../../packages/sdk/dist/index.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';

const exec = promisify(execFile);
const deadline = () => ({ signal: AbortSignal.timeout(15_000) });

async function fixture() {
  const directory = await mkdtemp('/tmp/whip-v4-');
  const binary = join(directory, 'runtime');
  let child;
  let output = '';
  let info;
  const record = chunk => { output = (output + chunk.toString()).slice(-(1 << 20)); };
  const stop = async (signal = 'SIGTERM') => {
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    const exit = once(child, 'exit');
    child.kill(signal);
    const timeout = setTimeout(() => child.kill('SIGKILL'), 10_000);
    try { await exit; } finally { clearTimeout(timeout); }
  };
  const start = async (delay = '60ms') => {
    child = spawn(binary, ['-directory', join(directory, 'state'), '-scripted', '-workers', '1', '-scripted-delay', delay], { stdio: ['ignore', 'pipe', 'pipe'] });
    child.stderr.on('data', record);
    child.stdout.on('data', record);
    let buffer = '';
    info = await new Promise((resolve, reject) => {
      const cleanup = () => { clearTimeout(timer); child.removeListener('error', failed); child.removeListener('exit', earlyExit); child.stdout.removeListener('data', data); };
      const failed = error => { cleanup(); reject(error); };
      const earlyExit = (code, signal) => failed(new Error(`Runtime exited before ready: ${code}/${signal}\n${output}`));
      const data = chunk => {
        buffer += chunk;
        const end = buffer.indexOf('\n');
        if (end < 0) return;
        try { const ready = JSON.parse(buffer.slice(0, end)); cleanup(); resolve(ready); } catch (error) { failed(error); }
      };
      const timer = setTimeout(() => failed(new Error('Runtime readiness timed out\n' + output)), 25_000);
      child.once('error', failed); child.once('exit', earlyExit); child.stdout.on('data', data);
    });
    return info;
  };
  try {
    await exec('go', ['build', ...(process.env.WHIP_SDK_RACE === '1' ? ['-race'] : []), '-o', binary, './cmd/whip-runtime'], { cwd: resolve('.'), timeout: 120_000 });
    await start();
  } catch (error) {
    await stop();
    await writeFile(join(directory, 'runtime.log'), output);
    throw new Error(`Fixture startup failed; retained ${directory}`, { cause: error });
  }
  return { directory, start, stop, get info() { return info; }, get pid() { return child.pid; }, get output() { return output; } };
}

async function dropAcknowledgement(path, upstream, requestID, onDrop) {
  const connections = new Set();
  const server = net.createServer(client => {
    const backend = net.connect(upstream);
    connections.add(client); connections.add(backend);
    const close = () => { client.destroy(); backend.destroy(); connections.delete(client); connections.delete(backend); };
    client.on('error', close); backend.on('error', close); client.on('close', close); backend.on('close', close);
    client.pipe(backend);
    let pending = '';
    backend.on('data', chunk => {
      pending += chunk;
      for (;;) {
        const end = pending.indexOf('\n'); if (end < 0) return;
        const line = pending.slice(0, end); pending = pending.slice(end + 1);
        const response = JSON.parse(line);
        if (response.result?.receipt?.identity.request_id === requestID) { onDrop(response.result); close(); return; }
        client.write(line + '\n');
      }
    });
  });
  server.listen(path); await once(server, 'listening');
  return async () => { for (const connection of connections) connection.destroy(); await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve())); };
}

async function until(read, predicate) {
  const signal = AbortSignal.timeout(10_000);
  for (;;) {
    signal.throwIfAborted();
    const value = await read(); if (predicate(value)) return value;
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}

test('v4 SDK executes, recovers lost acknowledgements, and preserves queued input across SIGKILL', { timeout: 180_000 }, async t => {
  const runtime = await fixture();
  const evidence = [];
  let passed = false;
  let proxyClose;
  try {
    const client = await Client.connect(unixSocket(runtime.info.socket), { clientID: 'v4-fixture', ...deadline() });
    assert.equal(client.runtimeID, runtime.info.runtime_id);
    const createParams = {
      metadata: { title: 'v4 acceptance', archived: false, pinned: false },
      engine: 'starlark', policy: { max_depth: 8, max_sessions: 100, max_queued_inputs_per_session: 100 },
      definition: client.builtins[0], overrides: { model: { provider: 'scripted', name: 'scripted', effort: '' } },
      working_directory: runtime.directory,
    };
    const { root, tree } = await client.call('trees.create', createParams, deadline());
    const page = () => client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
    assert.deepEqual((await page()).items, []);

    let dropped;
    const proxy = join(runtime.directory, 'drop.sock');
    proxyClose = await dropAcknowledgement(proxy, runtime.info.socket, 'lost-ack', value => { dropped = value; });
    const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    const parts = [{ type: 'text', text: 'durable hello' }];
    await assert.rejects(unreliable.submit(root.id, parts, 'lost-ack', deadline()), DeliveryError);
    assert.ok(dropped?.input.id, 'proxy did not observe committed admission');
    await proxyClose(); proxyClose = undefined;
    const retry = await client.submit(root.id, parts, 'lost-ack', deadline());
    assert.equal(retry.input.id, dropped.input.id);
    await assert.rejects(client.submit(root.id, [{ type: 'text', text: 'different' }], 'lost-ack', deadline()), error => error instanceof RemoteError && error.kind === 'CONFLICT');
    const complete = await client.wait('lost-ack', deadline());
    assert.equal(complete.turn.state, 'succeeded');
    const firstHistory = await page();
    assert.deepEqual(firstHistory.items.map(message => message.role), ['user', 'assistant']);
    assert.equal(firstHistory.items[1].parts[0].text, 'ack: durable hello');
    evidence.push({ dropped, complete, firstHistory });

    await Promise.all(Array.from({ length: 6 }, (_, index) => client.submit(root.id, [{ type: 'text', text: 'parallel-' + index }], 'parallel-' + index, deadline())));
    await Promise.all(Array.from({ length: 6 }, (_, index) => client.wait('parallel-' + index, deadline())));
    const concurrentHistory = await page();
    assert.equal(concurrentHistory.items.length, 14);
    assert.deepEqual(concurrentHistory.items.map(message => message.role), Array.from({ length: 7 }, () => ['user', 'assistant']).flat());

    const abort = new AbortController();
    await client.submit(root.id, [{ type: 'text', text: 'observer abort' }], 'abort-wait', deadline());
    const waiting = client.wait('abort-wait', { signal: abort.signal }); abort.abort();
    await assert.rejects(waiting, error => error.name === 'AbortError');
    const detached = await Client.connect(unixSocket(runtime.info.socket), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    assert.equal((await detached.wait('abort-wait', deadline())).turn.state, 'succeeded');
    const beforeCrash = await page();

    const configured = await client.call('sessions.configure', { session_id: root.id, expected_revision: root.config_revision, patch: { instructions: { text: 'inherit me', project_files: [], discover_skills: false } } }, deadline());
    const child = await client.call('sessions.spawn', { parent_id: root.id, overrides: {} }, deadline());
    assert.equal(child.configuration.instructions.text, configured.configuration.instructions.text);
    const sessions = await client.call('sessions.list', { tree_id: tree.id, limit: 100 }, deadline());
    assert.equal(sessions.items.length, 2);
    await client.submit(child.id, [{ type: 'text', text: 'child' }], 'child', deadline());
    assert.equal((await client.wait('child', deadline())).turn.state, 'succeeded');

    await runtime.stop(); await runtime.start('1h');
    await client.submit(root.id, [{ type: 'text', text: 'interrupted' }], 'claimed-before-kill', deadline());
    const claimed = await until(() => client.recover('claimed-before-kill', deadline()), value => value.turn?.state === 'running');
    const queued = await client.submit(root.id, [{ type: 'text', text: 'survives queue' }], 'queued-before-kill', deadline());
    assert.equal(queued.input.state, 'queued'); assert.equal(queued.turn, null);
    const cancelled = await client.submit(root.id, [{ type: 'text', text: 'cancel me' }], 'cancel-queue', deadline());
    await client.call('inputs.cancel', { input_id: cancelled.input.id }, deadline());
    assert.equal((await client.wait('cancel-queue', deadline())).input.state, 'cancelled');
    const pid = runtime.pid;
    await runtime.stop('SIGKILL'); await runtime.start('0');
    assert.notEqual(runtime.pid, pid); assert.equal(runtime.info.runtime_id, client.runtimeID);
    const interrupted = await client.wait('claimed-before-kill', deadline());
    assert.equal(interrupted.turn.state, 'interrupted'); assert.equal(interrupted.turn.id, claimed.turn.id);
    const resumed = await client.wait('queued-before-kill', deadline());
    assert.equal(resumed.input.id, queued.input.id); assert.equal(resumed.turn.state, 'succeeded');
    const afterCrash = await page();
    assert.deepEqual(afterCrash.items.slice(0, beforeCrash.items.length), beforeCrash.items);
    assert.deepEqual(afterCrash.items.slice(beforeCrash.items.length).map(message => message.role), ['user', 'user', 'assistant']);
    assert.equal(afterCrash.items.at(-1).parts[0].text, 'ack: survives queue');
    assert.equal((await client.submit(root.id, parts, 'lost-ack', deadline())).input.id, dropped.input.id);
    evidence.push({ claimed, queued, interrupted, resumed, afterCrash });

    const example = await exec(process.execPath, ['packages/sdk/examples/session.mjs', runtime.info.socket], { timeout: 20_000 });
    assert.equal(JSON.parse(example.stdout).completed.turn.state, 'succeeded');
    await assert.rejects(Client.connect(unixSocket(runtime.info.socket), { clientID: 'wrong', expectedRuntimeID: 'different', ...deadline() }), error => error instanceof RemoteError && error.kind === 'IDENTITY');
    passed = true;
  } finally {
    await proxyClose?.();
    await runtime.stop();
    if (passed) await rm(runtime.directory, { recursive: true, force: true });
    else {
      const artifacts = join('test-results/redesign', basename(runtime.directory));
      await mkdir(artifacts, { recursive: true });
      await cp(runtime.directory, artifacts, { recursive: true, filter: path => !path.endsWith('/runtime') && !path.endsWith('.sock') });
      await writeFile(join(artifacts, 'runtime.log'), runtime.output);
      await writeFile(join(artifacts, 'observations.json'), JSON.stringify(evidence, null, 2) + '\n');
      t.diagnostic(`Failure artifacts: ${artifacts}; original: ${runtime.directory}`);
    }
  }
});
