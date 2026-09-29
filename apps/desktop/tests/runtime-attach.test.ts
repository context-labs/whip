import assert from 'node:assert/strict';
import { once } from 'node:events';
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { createServer, type Socket } from 'node:net';
import path from 'node:path';
import test, { type TestContext } from 'node:test';
import { manifest } from '@whip/protocol';
import { LocalRuntime, fileDigest, type DaemonStatus } from '../src/runtime';
import { nativeRuntimeStatus } from './native-runtime-fixture';

const signal = () => new AbortController().signal;
async function fixture(t: TestContext, options: { state?: DaemonStatus['state']; minor?: number; major?: number; hang?: boolean; restart?: boolean; replaced?: boolean } = {}) {
  // Short enough for macOS Unix socket paths, including on machines with long TMPDIRs.
  const directory = await mkdtemp('/tmp/whip-attach-');
  const home = path.join(directory, 'home');
  const executable = path.join(directory, 'whipcode');
  const socket = path.join(directory, 'runtime.sock');
  const settingsFile = path.join(directory, 'user-data', 'native-local-runtime.json');
  const log = path.join(directory, 'calls.jsonl');
  const requests: string[] = [];
  const sockets = new Set<Socket>();
  const server = createServer(connection => {
    sockets.add(connection);
    connection.on('close', () => sockets.delete(connection));
    let pending = '';
    connection.on('data', bytes => {
      pending += bytes.toString();
      while (pending.includes('\n')) {
        const end = pending.indexOf('\n');
        const request = JSON.parse(pending.slice(0, end)); pending = pending.slice(end + 1);
        requests.push(request.method);
        assert.ok(['initialize', 'host.status'].includes(request.method));
        if (options.hang) continue;
        const live = nativeRuntimeStatus(socket).process!;
        live.pid = process.pid; live.build = 'older-running-build';
        if (options.restart) live.process_epoch = 'replacement-epoch';
        if (options.replaced && request.method === 'host.status') live.runtime_id = 'other-runtime';
        connection.write(JSON.stringify({ jsonrpc: '2.0', id: request.id, result: request.method === 'initialize' ? {
          major: options.major ?? manifest.major, minor: options.minor ?? manifest.minor,
          runtime_id: live.runtime_id, process_epoch: live.process_epoch, network_client: false, builtins: [],
        } : live }) + '\n');
      }
    });
  });
  t.after(async () => { for (const socket of sockets) socket.destroy(); await new Promise<void>(resolve => server.close(() => resolve())); await rm(directory, { recursive: true, force: true }); });
  server.listen(socket); await once(server, 'listening');
  const info = { distribution: 'whipcode', buildId: 'different-installed-build', protocolMajor: manifest.major, protocolMinor: manifest.minor, schemaVersion: 999 };
  const status = nativeRuntimeStatus(socket, options.state);
  if (status.process) { status.process.pid = process.pid; status.process.build = 'older-running-build'; }
  await writeFile(executable, `#!${process.execPath}
const fs = require('node:fs');
const args = process.argv.slice(2);
fs.appendFileSync(${JSON.stringify(log)}, JSON.stringify(args) + '\\n');
if (args[0] === '_desktop-runtime-info') console.log(${JSON.stringify(JSON.stringify(info))});
else if (JSON.stringify(args) === '["daemon","status","--json"]') console.log(${JSON.stringify(JSON.stringify(status))});
else process.exit(90);
`, { mode: 0o700 });
  const runtime = new LocalRuntime({ mode: 'attach', defaultExecutable: executable, settingsFile,
    env: { ...process.env, WHIPCODE_HOME: home } });
  return { runtime, directory, home, executable, settingsFile, log, socket, requests, sockets };
}

test('attach accepts the native major and compatible minor versions and different builds without replacing bytes or saved ownership', async t => {
  const f = await fixture(t, { minor: manifest.minor + 1 });
  await mkdir(path.dirname(f.settingsFile));
  const settings = JSON.stringify({ executable: '/wrong/executable', managed: { sha256: 'a'.repeat(64), channel: 'beta', approvedVersion: '1.2.3' } });
  await writeFile(f.settingsFile, settings);
  const digest = await fileDigest(f.executable);
  for (let attempt = 0; attempt < 3; attempt++) {
    await f.runtime.synchronize(signal());
    assert.equal(await f.runtime.prepare(signal(), () => {}), f.socket);
  }
  const result = await f.runtime.test(signal());
  assert.equal(result.state, 'running');
  assert.equal(result.clientBuild, 'different-installed-build');
  assert.equal(result.daemonBuild, 'older-running-build');
  assert.equal(await fileDigest(f.executable), digest);
  assert.equal(await readFile(f.settingsFile, 'utf8'), settings);
  assert.deepEqual(new Set(f.requests), new Set(['initialize', 'host.status']));
  const calls = (await readFile(f.log, 'utf8')).trim().split('\n').map(line => JSON.parse(line));
  assert(calls.every(args => args[0] === '_desktop-runtime-info' || JSON.stringify(args) === '["daemon","status","--json"]'));
  await assert.rejects(stat(f.home), { code: 'ENOENT' });
});

test('attach refuses stopped and unhealthy daemons, including stale sockets, without starting or repairing them', async t => {
  for (const state of ['stopped', 'unhealthy'] as const) await t.test(state, async t => {
    const f = await fixture(t, { state });
    await assert.rejects(f.runtime.prepare(signal(), () => {}), new RegExp(state));
    assert.equal(f.requests.length, 0);
    assert.doesNotMatch(await readFile(f.log, 'utf8'), /start|restart|sync/);
    await assert.rejects(stat(f.home), { code: 'ENOENT' });
    await assert.rejects(stat(f.settingsFile), { code: 'ENOENT' });
  });
});

test('attach checks the running protocol, even when the installed executable is current', async t => {
  for (const version of [{ major: manifest.major - 1 }, { major: manifest.major + 1 }]) await t.test(JSON.stringify(version), async t => {
    const f = await fixture(t, version);
    assert.equal((await f.runtime.test(signal())).state, 'incompatible');
    await assert.rejects(f.runtime.prepare(signal(), () => {}), /protocol/);
    assert.doesNotMatch(await readFile(f.log, 'utf8'), /start|restart|sync/);
  });
});

test('attach blocks all backend management entry points, including previously approved updates', async t => {
  const f = await fixture(t);
  for (const action of [() => f.runtime.install(f.executable, signal()), () => f.runtime.installDefault(signal()),
    () => f.runtime.restart(signal()), () => f.runtime.choose('/other/executable', signal()),
    () => f.runtime.approveUpdate('1.2.3', signal())]) await assert.rejects(action(), /Attach mode/);
  await assert.rejects(stat(f.log), { code: 'ENOENT' });
  await assert.rejects(stat(f.settingsFile), { code: 'ENOENT' });
  await rm(f.executable);
  assert.equal((await f.runtime.test(signal())).canInstall, false);
  await assert.rejects(f.runtime.prepare(signal(), () => {}), /missing/);
});

test('cancelling an attachment closes its probe and leaves no pending connection', async t => {
  const f = await fixture(t, { hang: true });
  const controller = new AbortController();
  const pending = f.runtime.prepare(controller.signal, () => {});
  while (!f.requests.length) await new Promise(resolve => setTimeout(resolve, 10));
  const closed = once([...f.sockets][0]!, 'close');
  controller.abort();
  await assert.rejects(pending, /abort/i);
  await closed;
  assert.equal(f.sockets.size, 0);
});

test('attach rejects epoch or runtime replacement during inspection without mutation', async t => {
  for (const options of [{ restart: true }, { replaced: true }]) await t.test(JSON.stringify(options), async t => {
    const f = await fixture(t, options);
    assert.equal((await f.runtime.test(signal())).state, 'unhealthy');
    assert.doesNotMatch(await readFile(f.log, 'utf8'), /start|restart|sync/);
    await assert.rejects(stat(f.settingsFile), { code: 'ENOENT' });
  });
});
