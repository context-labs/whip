import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import fs from 'node:fs/promises';
import { chmod, lstat, mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test, { type TestContext } from 'node:test';
import { LocalRuntime, fileDigest, parseDaemonStatus, readRuntimeManifest, run, verifyRuntime, type RuntimeManifest } from '../src/runtime';

const signal = () => new AbortController().signal;
const hash = (bytes: string | Uint8Array) => createHash('sha256').update(bytes).digest('hex');
const quote = (value: string) => `'${value.replaceAll("'", "'\\''")}'`;
async function fixture(t: TestContext, options: { distribution?: string; initial?: object; startError?: string } = {}) {
  const directory = await mkdtemp(path.join(tmpdir(), 'whip-runtime-test-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const source = path.join(directory, 'payload');
  const executable = path.join(directory, 'bin', 'whipcode');
  const home = path.join(directory, 'home');
  const settingsFile = path.join(directory, 'desktop', 'native-local-runtime.json');
  const state = path.join(home, 'running');
  const log = path.join(directory, 'commands.log');
  const socket = '/tmp/whip-fixture.sock';
  const env = { HOME: directory, WHIPCODE_HOME: home, WHIPCODE_NETWORK: '0', PATH: '/usr/bin:/bin' };
  const info = { distribution: options.distribution || 'whipcode', buildId: 'local-test', protocolMajor: 4, protocolMinor: 1, schemaVersion: 10 };
  const running = { state: 'running', socket, pid: 123, client_build: 'local-test', daemon_build: 'local-test' };
  await mkdir(source);
  const script = `#!/bin/sh
case "$1 $2" in
  '_desktop-runtime-info ') printf '%s\\n' '${JSON.stringify(info)}' ;;
  'daemon status')
    printf 'status:%s\\n' "$0" >> ${quote(log)}
    if [ -f ${quote(state)} ]; then printf '%s\\n' '${JSON.stringify(running)}'
    else printf '%s\\n' '${JSON.stringify({ state: 'stopped', socket, ...options.initial })}'; fi ;;
  'daemon start'|'daemon restart')
    printf 'start:%s:%s:%s\\n' "$0" "$WHIPCODE_HOME" "$WHIPCODE_LISTEN" >> ${quote(log)}
    ${options.startError ? `printf '%s\\n' ${quote(options.startError)} >&2; exit 1` : `mkdir -p ${quote(home)}; : > ${quote(state)}`} ;;
  *) exit 2 ;;
esac
`;
  await writeFile(path.join(source, 'whipcode'), script, { mode: 0o700 });
  await writeFile(path.join(source, 'whip-computer'), '#!/bin/sh\nexit 0\n', { mode: 0o700 });
  const files = {} as RuntimeManifest['files'];
  for (const name of ['whipcode', 'whip-computer'] as const) {
    const bytes = await readFile(path.join(source, name)); files[name] = { bytes: bytes.length, sha256: hash(bytes) };
  }
  const manifest: RuntimeManifest = { schema: 1, version: '1.2.3', buildId: info.buildId, distribution: 'whipcode', architecture: 'arm64', rendererDigest: 'a'.repeat(64),
    source: { commit: 'a'.repeat(40), dirty: false, lockfile: 'b'.repeat(64) },
    compatibility: { protocolMajor: 4, protocolMinor: 1, schemaVersion: 10 }, files };
  const opts = { source, manifest, settingsFile, env, defaultExecutable: executable };
  const runtime = new LocalRuntime(opts);
  return { directory, source, executable, home, settingsFile, state, log, socket, manifest, runtime, opts };
}
const absent = async (filename: string) => assert.rejects(lstat(filename), { code: 'ENOENT' });

test('test is read-only for missing installations and does not silently use packaged bytes', async t => {
  const f = await fixture(t);
  const before = await readdir(f.directory);
  assert.equal((await f.runtime.test(signal())).state, 'missing');
  assert.deepEqual(await readdir(f.directory), before);
  await assert.rejects(f.runtime.prepare(signal(), () => {}), /not installed/);
  await absent(f.home); await absent(f.settingsFile); await absent(path.dirname(f.executable));
});

test('explicit install publishes verified canonical bytes; test never starts a daemon', async t => {
  const f = await fixture(t);
  const result = await f.runtime.install(f.executable, signal());
  assert.equal(result.state, 'stopped');
  assert.equal(await fileDigest(f.executable), f.manifest.files.whipcode.sha256);
  assert.equal((await lstat(f.executable)).mode & 0o777, 0o755);
  assert.equal(JSON.parse(await readFile(f.settingsFile, 'utf8')).executable, f.executable);
  const inode = (await lstat(f.executable)).ino;
  await f.runtime.install(f.executable, signal());
  assert.equal((await lstat(f.executable)).ino, inode);
  await f.runtime.test(signal());
  await absent(f.home);
  assert.doesNotMatch(await readFile(f.log, 'utf8'), /start:/);
  assert.deepEqual(await readdir(path.dirname(f.executable)), ['whipcode']);
});

test('desktop connects through the installed binary and reuses its running daemon', async t => {
  const f = await fixture(t);
  await f.runtime.install(f.executable, signal());
  await rm(f.source, { recursive: true });
  assert.equal(await f.runtime.prepare(signal(), () => {}), f.socket);
  const other = new LocalRuntime(f.opts);
  assert.equal(await other.prepare(signal(), () => {}), f.socket);
  const commands = await readFile(f.log, 'utf8');
  assert.equal(commands.split('\n').filter(line => line.startsWith('start:')).length, 1);
  assert.match(commands, new RegExp(`start:${f.executable}:${f.home}:127.0.0.1:8080`));
  await absent(path.join(f.directory, '.whip'));
  await absent(path.join(path.dirname(f.settingsFile), 'runtimes'));
});

test('saved selection wins over PATH changes and missing saved binaries do not fall back', async t => {
  const f = await fixture(t);
  await f.runtime.install(f.executable, signal());
  const changed = new LocalRuntime({ ...f.opts, defaultExecutable: path.join(f.source, 'whipcode'), env: { ...f.opts.env, PATH: f.source } });
  assert.equal(await changed.executable(signal()), f.executable);
  await rm(f.executable);
  assert.equal((await changed.test(signal())).state, 'missing');
  await assert.rejects(changed.prepare(signal(), () => {}), /not installed/);
});

test('explicit choice validates distribution and persists without starting work', async t => {
  const f = await fixture(t);
  const chosen = path.join(f.source, 'whipcode');
  assert.equal((await f.runtime.choose(chosen, signal())).state, 'stopped');
  assert.equal(await f.runtime.executable(signal()), chosen);
  await absent(f.home);
  const bad = await fixture(t, { distribution: 'whip' });
  await assert.rejects(f.runtime.choose(path.join(bad.source, 'whipcode'), signal()), /not a whipcode/);
  assert.equal(await f.runtime.executable(signal()), chosen);
});

test('cancelling selection before settings publication preserves the previous executable', async t => {
  const f = await fixture(t);
  await f.runtime.install(f.executable, signal());
  const before = await readFile(f.settingsFile, 'utf8');
  const controller = new AbortController();
  const original = fs.writeFile;
  t.mock.method(fs, 'writeFile', async (...args: Parameters<typeof fs.writeFile>) => {
    await original(...args);
    if (typeof args[0] === 'string' && path.basename(args[0]) === 'settings.json') controller.abort();
  });
  await assert.rejects(f.runtime.choose(path.join(f.source, 'whipcode'), controller.signal), { name: 'AbortError' });
  assert.equal(await readFile(f.settingsFile, 'utf8'), before);
  assert.equal(await f.runtime.executable(signal()), f.executable);
  assert.deepEqual(await readdir(path.dirname(f.settingsFile)), ['native-local-runtime.json']);
  await absent(f.home);
  assert.doesNotMatch(await readFile(f.log, 'utf8'), /start:/);
});

test('refuses corrupt payloads and existing different installations without overwriting', async t => {
  const f = await fixture(t);
  await f.runtime.install(f.executable, signal());
  await writeFile(f.executable, 'existing user binary');
  await assert.rejects(f.runtime.install(f.executable, signal()), /already exists/);
  assert.equal(await readFile(f.executable, 'utf8'), 'existing user binary');
  await writeFile(path.join(f.source, 'whipcode'), 'tampered');
  await assert.rejects(f.runtime.install(path.join(f.directory, 'another'), signal()), /integrity/);
  await absent(path.join(f.directory, 'another'));
});

test('refuses a verified payload whose executable build identity differs from its manifest', async t => {
  const f = await fixture(t);
  f.manifest.buildId = 'different-build';
  await assert.rejects(f.runtime.install(f.executable, signal()), /build identity does not match its manifest/);
  await absent(f.executable); await absent(f.settingsFile); await absent(f.home);
  await absent(path.dirname(f.executable));
});

test('installation permission failures offer bounded recovery instructions without raw filesystem details', async t => {
  const f = await fixture(t);
  const original = fs.mkdir;
  t.mock.method(fs, 'mkdir', async (...args: Parameters<typeof fs.mkdir>) => {
    if (args[0] === path.dirname(f.executable))
      throw Object.assign(new Error('EACCES: sensitive fixture details '.repeat(1000)), { code: 'EACCES' });
    return original(...args);
  });
  await assert.rejects(f.runtime.install(f.executable, signal()), error => {
    const message = (error as Error).message;
    assert.match(message, /Choose a writable executable location/);
    assert.match(message, /install it through your terminal/);
    assert.ok(message.length < 300);
    assert.doesNotMatch(message, /sensitive fixture|EACCES/);
    return true;
  });
  await absent(f.executable); await absent(f.settingsFile); await absent(f.home);
});

test('exclusive publication leaves a concurrently installed executable intact', async t => {
  const f = await fixture(t);
  const original = fs.copyFile;
  t.mock.method(fs, 'copyFile', async (...args: Parameters<typeof fs.copyFile>) => {
    await original(...args); await writeFile(f.executable, 'concurrent installation', { mode: 0o700 });
  });
  await assert.rejects(f.runtime.install(f.executable, signal()), { code: 'EEXIST' });
  assert.equal(await readFile(f.executable, 'utf8'), 'concurrent installation');
  assert.deepEqual(await readdir(path.dirname(f.executable)), ['whipcode']);
  await absent(f.settingsFile);
});

test('cancelled copies remove temporary files without creating an installation', async t => {
  const f = await fixture(t);
  const controller = new AbortController();
  const original = fs.copyFile;
  t.mock.method(fs, 'copyFile', async (...args: Parameters<typeof fs.copyFile>) => {
    await original(...args); controller.abort();
  });
  await assert.rejects(f.runtime.install(f.executable, controller.signal), { name: 'AbortError' });
  await absent(f.executable); await absent(f.settingsFile);
  assert.deepEqual(await readdir(path.dirname(f.executable)), []);
});

test('existing unhealthy owners require explicit restart; stale unowned sockets recover', async t => {
  const f = await fixture(t, { initial: { state: 'unhealthy', pid: 123 } });
  await f.runtime.install(f.executable, signal());
  await assert.rejects(f.runtime.prepare(signal(), () => {}), /unhealthy/);
  assert.doesNotMatch(await readFile(f.log, 'utf8'), /start:/);
  assert.equal((await f.runtime.restart(signal())).state, 'running');
  const stale = await fixture(t, { initial: { state: 'unhealthy', stale_socket: true } });
  await stale.runtime.install(stale.executable, signal());
  assert.equal(await stale.runtime.prepare(signal(), () => {}), stale.socket);
});

test('startup port conflicts are actionable and do not expose raw subprocess output', async t => {
  const f = await fixture(t, { startError: 'listen tcp: address already in use; sensitive fixture text' });
  await f.runtime.install(f.executable, signal());
  await assert.rejects(f.runtime.prepare(signal(), () => {}), error => {
    assert.match((error as Error).message, /port is already in use/);
    assert.doesNotMatch((error as Error).message, /sensitive fixture/); return true;
  });
});

test('rejects non-executable and symlinked package payloads', async t => {
  const f = await fixture(t);
  const executable = path.join(f.source, 'whipcode');
  await chmod(executable, 0o600);
  await assert.rejects(verifyRuntime(f.source, f.manifest, signal()), /integrity/);
  await chmod(executable, 0o700);
  const bytes = await readFile(executable); await rm(executable);
  const outside = path.join(f.directory, 'outside'); await writeFile(outside, bytes, { mode: 0o700 });
  await symlink(outside, executable);
  await assert.rejects(verifyRuntime(f.source, f.manifest, signal()), /integrity/);
});

test('manifest validation checks distribution, build identity and file inventory', async t => {
  const f = await fixture(t);
  const filename = path.join(f.directory, 'manifest.json');
  await writeFile(filename, JSON.stringify(f.manifest));
  assert.deepEqual(await readRuntimeManifest(filename), f.manifest);
  for (const change of [{ distribution: 'whip' }, { buildId: '../escape' }, { version: '../escape' }, { schema: 2 }, { files: {} }, { architecture: 'x64' }]) {
    await writeFile(filename, JSON.stringify({ ...f.manifest, ...change }));
    await assert.rejects(readRuntimeManifest(filename), /Invalid/);
  }
});

test('bounds subprocess output and stops timed out or cancelled children', async t => {
  const f = await fixture(t);
  assert.equal(await run(process.execPath, ['-e', 'process.stdout.write("fixture")'], f.opts.env, signal()), 'fixture');
  await assert.rejects(run(process.execPath, ['-e', 'process.stdout.write("x".repeat(65537))'], f.opts.env, signal()), /maxBuffer/);
  const controller = new AbortController();
  const child = run(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], f.opts.env, controller.signal);
  controller.abort(); await assert.rejects(child, { name: 'AbortError' });
  await assert.rejects(run(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], f.opts.env, signal(), 50), { killed: true });
});

test('validates daemon socket and process metadata before transport attachment', () => {
  assert.equal(parseDaemonStatus(JSON.stringify({ state: 'stopped', socket: '/tmp/test.sock' })).state, 'stopped');
  for (const value of [{ state: 'oops', socket: '/tmp/x' }, { state: 'running', socket: 'relative' }, { state: 'running', socket: '/tmp/x', pid: -1 }])
    assert.throws(() => parseDaemonStatus(JSON.stringify(value)), /invalid runtime status/);
});
