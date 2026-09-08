import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import fs from 'node:fs/promises';
import { chmod, lstat, mkdir, mkdtemp, readFile, readdir, rename, rm, symlink, truncate, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test, { type TestContext } from 'node:test';
import { fileDigest, installRuntime, parseDaemonStatus, prepareLocal, readRuntimeManifest, run, verifyRuntime, type RuntimeManifest } from '../src/runtime';

const signal = () => new AbortController().signal;
const hash = (bytes: string | Uint8Array) => createHash('sha256').update(bytes).digest('hex');
async function fixture(t: TestContext) {
  const directory = await mkdtemp(path.join(tmpdir(), 'whip-runtime-test-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const source = path.join(directory, 'source');
  const retained = path.join(directory, 'retained');
  await mkdir(source);
  for (const name of ['whip', 'whip-computer'])
    await writeFile(path.join(source, name), `#!/bin/sh\nprintf '${name} fixture\\n'\n`, { mode: 0o700 });
  const files = {} as RuntimeManifest['files'];
  for (const name of ['whip', 'whip-computer'] as const) {
    const bytes = await readFile(path.join(source, name));
    files[name] = { bytes: bytes.length, sha256: hash(bytes) };
  }
  const manifest: RuntimeManifest = { schema: 1, version: '1.2.3', architecture: 'arm64', rendererDigest: 'a'.repeat(64),
    source: { commit: 'a'.repeat(40), dirty: false, lockfile: 'b'.repeat(64) },
    compatibility: { protocolMajor: 3, protocolMinor: 0, schemaVersion: 10 }, files };
  const manifestPath = path.join(directory, 'runtime-manifest.json');
  await writeFile(manifestPath, JSON.stringify(manifest));
  return { directory, source, retained, manifest, manifestPath };
}

async function localFixture(t: TestContext, initialStatus = {}) {
  const f = await fixture(t);
  const quote = (value: string) => `'${value.replaceAll("'", "'\\''")}'`;
  const shell = path.join(f.directory, 'fixture-shell');
  const state = path.join(f.directory, 'running');
  const log = path.join(f.directory, 'commands.log');
  const socket = '/tmp/whip-fixture.sock';
  await writeFile(shell, '#!/bin/sh\nprintf "\\000WHIP_PATH=/usr/bin:/bin\\000"\n', { mode: 0o700 });
  const previous = { SHELL: process.env.SHELL, WHIP_HOME: process.env.WHIP_HOME };
  process.env.SHELL = shell;
  process.env.WHIP_HOME = f.directory;
  t.after(() => {
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) delete process.env[key]; else process.env[key] = value;
    }
  });
  const script = `#!/bin/sh
case "$1 $2" in
  'daemon status')
    printf 'status:%s\\n' "$0" >> ${quote(log)}
    if [ -f ${quote(state)} ]; then
      printf '%s\\n' '${JSON.stringify({ state: 'running', socket, pid: 123 })}'
    else
      printf '%s\\n' '${JSON.stringify({ state: 'stopped', socket, ...initialStatus })}'
    fi
    ;;
  'daemon start')
    printf 'start:%s:%s\\n' "$0" "$WHIP_COMPUTER_BIN" >> ${quote(log)}
    : > ${quote(state)}
    ;;
  *) exit 2 ;;
esac
`;
  await writeFile(path.join(f.source, 'whip'), script);
  f.manifest.files.whip = { bytes: Buffer.byteLength(script), sha256: hash(script) };
  return { ...f, state, log, socket };
}

test('installs immutable executable copies and reuses an already verified installation', async t => {
  const f = await fixture(t);
  const installed = await installRuntime(f.source, f.retained, f.manifest, signal());
  assert.notEqual(path.dirname(installed.executable), f.source);
  assert.equal(path.dirname(installed.executable), path.dirname(installed.helper));
  assert.deepEqual(await readFile(installed.executable), await readFile(path.join(f.source, 'whip')));
  assert.deepEqual(await readFile(installed.helper), await readFile(path.join(f.source, 'whip-computer')));
  assert.equal((await lstat(installed.executable)).mode & 0o777, 0o700);
  assert.equal((await lstat(f.retained)).mode & 0o777, 0o700);
  const before = await lstat(installed.executable);
  assert.deepEqual(await installRuntime(f.source, f.retained, f.manifest, signal()), installed);
  assert.equal((await lstat(installed.executable)).ino, before.ino);
  const original = await readFile(installed.executable);
  await writeFile(path.join(f.source, 'whip'), '#!/bin/sh\nprintf "new build\\n"\n');
  const bytes = await readFile(path.join(f.source, 'whip'));
  const next = { ...f.manifest, files: { ...f.manifest.files, whip: { bytes: bytes.length, sha256: hash(bytes) } } };
  const newer = await installRuntime(f.source, f.retained, next, signal());
  assert.notEqual(newer.executable, installed.executable, 'a reissued version must get a different immutable path');
  assert.deepEqual(await readFile(installed.executable), original);
});

test('concurrent installers converge on one complete destination and remove temporary copies', async t => {
  const f = await fixture(t);
  const [first, second] = await Promise.all([
    installRuntime(f.source, f.retained, f.manifest, signal()),
    installRuntime(f.source, f.retained, f.manifest, signal()),
  ]);
  assert.deepEqual(first, second);
  assert.equal((await readdir(f.retained)).length, 1);
  await verifyRuntime(path.dirname(first.executable), f.manifest, signal());
});

test('does not replace or repair a corrupted or partial retained installation in place', async t => {
  const f = await fixture(t);
  const installed = await installRuntime(f.source, f.retained, f.manifest, signal());
  await writeFile(installed.executable, 'corrupted old executable');
  await assert.rejects(installRuntime(f.source, f.retained, f.manifest, signal()), /integrity/);
  assert.equal(await readFile(installed.executable, 'utf8'), 'corrupted old executable');
  await writeFile(installed.executable, await readFile(path.join(f.source, 'whip')));
  await rm(installed.helper);
  await assert.rejects(installRuntime(f.source, f.retained, f.manifest, signal()));
  await assert.rejects(lstat(installed.helper), { code: 'ENOENT' });
  assert.equal((await readdir(f.retained)).some(name => name.startsWith('.install-')), false);
});

test('rejects symlinks and non-executable or tampered runtime files', async t => {
  const f = await fixture(t);
  const executable = path.join(f.source, 'whip');
  await chmod(executable, 0o600);
  await assert.rejects(verifyRuntime(f.source, f.manifest, signal()), /integrity/);
  await chmod(executable, 0o700);
  const original = await readFile(executable);
  const tampered = Buffer.from(original); tampered[tampered.length - 2] ^= 1;
  await writeFile(executable, tampered);
  await assert.rejects(verifyRuntime(f.source, f.manifest, signal()), /integrity/);
  const outside = path.join(f.directory, 'outside');
  await writeFile(outside, original, { mode: 0o700 });
  await rm(executable); await symlink(outside, executable);
  await assert.rejects(verifyRuntime(f.source, f.manifest, signal()), /integrity/);
  await rm(executable); await writeFile(executable, original, { mode: 0o700 });
  const real = path.join(f.directory, 'real-source');
  await rename(f.source, real); await symlink(real, f.source);
  await assert.rejects(verifyRuntime(f.source, f.manifest, signal()), /runtime directory/);
});

test('rejects a retained-root symlink even when the installation already exists behind it', async t => {
  const f = await fixture(t);
  await installRuntime(f.source, f.retained, f.manifest, signal());
  const real = path.join(f.directory, 'outside-retained');
  await rename(f.retained, real); await symlink(real, f.retained);
  await assert.rejects(installRuntime(f.source, f.retained, f.manifest, signal()), /retained-runtime directory/);
});

test('honors cancellation before installation and cleans a cancelled partial copy', async t => {
  const f = await fixture(t);
  const controller = new AbortController(); controller.abort(new Error('Cancelled before installation'));
  await assert.rejects(installRuntime(f.source, f.retained, f.manifest, controller.signal), /Cancelled/);
  await assert.rejects(lstat(f.retained), { code: 'ENOENT' });
  const copying = new AbortController();
  const copy = fs.copyFile;
  t.mock.method(fs, 'copyFile', async (...args: Parameters<typeof fs.copyFile>) => {
    await copy(...args);
    copying.abort(new Error('Cancelled while copying'));
  });
  await assert.rejects(installRuntime(f.source, f.retained, f.manifest, copying.signal), /Cancelled/);
  assert.deepEqual(await readdir(f.retained), []);
  assert.equal(await fileDigest(path.join(f.source, 'whip')), f.manifest.files.whip.sha256);
});

test('removes a partial installation when the second companion copy fails', async t => {
  const f = await fixture(t);
  const copy = fs.copyFile;
  t.mock.method(fs, 'copyFile', async (...args: Parameters<typeof fs.copyFile>) => {
    if (String(args[0]).endsWith('whip-computer')) throw Object.assign(new Error('Disk full'), { code: 'ENOSPC' });
    return copy(...args);
  });
  await assert.rejects(installRuntime(f.source, f.retained, f.manifest, signal()), /Disk full/);
  assert.deepEqual(await readdir(f.retained), []);
});

test('cancels file verification and refuses a cancelled cached-install lookup', async t => {
  const f = await fixture(t);
  const installed = await installRuntime(f.source, f.retained, f.manifest, signal());
  const controller = new AbortController();
  const hashing = fileDigest(installed.executable, controller.signal);
  controller.abort();
  await assert.rejects(hashing, { name: 'AbortError' });
  await assert.rejects(installRuntime(f.source, f.retained, f.manifest, controller.signal), { name: 'AbortError' });
  await verifyRuntime(path.dirname(installed.executable), f.manifest, signal());
});

test('bounds subprocess output and cancels or times out owned fixture processes', async t => {
  const f = await fixture(t);
  const env = { PATH: '/usr/bin:/bin', HOME: f.directory };
  assert.equal(await run(process.execPath, ['-e', 'process.stdout.write("fixture")'], env, signal()), 'fixture');
  await assert.rejects(run(process.execPath, ['-e', 'process.stdout.write("x".repeat(65537))'], env, signal()), /maxBuffer/);
  const controller = new AbortController();
  const pending = run(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], env, controller.signal);
  controller.abort();
  await assert.rejects(pending, { name: 'AbortError' });
  await assert.rejects(run(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], env, signal(), 100), { killed: true });
});

test('reuses a running fixture daemon without installing or starting another process', async t => {
  const f = await localFixture(t);
  await writeFile(f.state, 'running');
  assert.equal(await prepareLocal({ source: f.source, retainedRoot: f.retained, manifest: f.manifest, signal: signal(), progress() {} }), f.socket);
  assert.equal(await readFile(f.log, 'utf8'), `status:${path.join(f.source, 'whip')}\n`);
  await assert.rejects(lstat(f.retained), { code: 'ENOENT' });
});

test('starts a stopped fixture from its retained executable with the matching retained helper', async t => {
  const f = await localFixture(t);
  assert.equal(await prepareLocal({ source: f.source, retainedRoot: f.retained, manifest: f.manifest,
    signal: signal(), progress() {} }), f.socket);
  const retained = path.join(f.retained, (await readdir(f.retained))[0]!);
  assert.deepEqual((await readFile(f.log, 'utf8')).trim().split('\n'), [
    `status:${path.join(f.source, 'whip')}`,
    `start:${path.join(retained, 'whip')}:${path.join(retained, 'whip-computer')}`,
    `status:${path.join(retained, 'whip')}`,
  ]);
});

test('recovers an unowned stale socket through normal daemon start', async t => {
  const f = await localFixture(t, { state: 'unhealthy', stale_socket: true });
  assert.equal(await prepareLocal({ source: f.source, retainedRoot: f.retained, manifest: f.manifest,
    signal: signal(), progress() {} }), f.socket);
  assert.match(await readFile(f.log, 'utf8'), /\nstart:/);
});

for (const status of [{ state: 'unhealthy', pid: 123 }, { state: 'unhealthy', stale_socket: false }])
  test(`leaves an unhealthy runtime untouched (${JSON.stringify(status)})`, async t => {
    const f = await localFixture(t, status);
    await assert.rejects(prepareLocal({ source: f.source, retainedRoot: f.retained, manifest: f.manifest,
      signal: signal(), progress() {} }), /needs attention/);
    assert.equal(await readFile(f.log, 'utf8'), `status:${path.join(f.source, 'whip')}\n`);
    await assert.rejects(lstat(f.retained), { code: 'ENOENT' });
  });

test('rejects missing, malformed, oversized and symlinked runtime manifests', async t => {
  const f = await fixture(t);
  assert.deepEqual(await readRuntimeManifest(f.manifestPath), f.manifest);
  for (const change of [
    { schema: 2 }, { version: '../escape' }, { architecture: 'x64' }, { rendererDigest: 'invalid' },
    { teamId: 'not-a-team' }, { files: {} }, { files: { ...f.manifest.files, other: f.manifest.files.whip } },
    { files: { ...f.manifest.files, whip: { ...f.manifest.files.whip, bytes: 0 } } },
    { files: { ...f.manifest.files, whip: { ...f.manifest.files.whip, bytes: (512 << 20) + 1 } } },
    { files: { ...f.manifest.files, whip: { ...f.manifest.files.whip, sha256: 'invalid' } } },
  ]) {
    await writeFile(f.manifestPath, JSON.stringify({ ...f.manifest, ...change }));
    await assert.rejects(readRuntimeManifest(f.manifestPath));
  }
  await writeFile(f.manifestPath, ''); await truncate(f.manifestPath, (16 << 10) + 1);
  await assert.rejects(readRuntimeManifest(f.manifestPath), /manifest/);
  await rm(f.manifestPath);
  await assert.rejects(readRuntimeManifest(f.manifestPath), { code: 'ENOENT' });
  const outside = path.join(f.directory, 'outside.json');
  await writeFile(outside, JSON.stringify(f.manifest)); await symlink(outside, f.manifestPath);
  await assert.rejects(readRuntimeManifest(f.manifestPath), /manifest/);
});

test('requires the requested Developer ID signature even when the file hashes match', { skip: process.platform !== 'darwin' }, async t => {
  const f = await fixture(t);
  await assert.rejects(verifyRuntime(f.source, { ...f.manifest, teamId: 'AAAAAAAAAA' }, signal()), /codesign|not signed|code object/i);
});

test('parses bounded daemon status and rejects invalid socket, process and error fields', () => {
  assert.deepEqual(parseDaemonStatus('{"state":"running","socket":"/tmp/whip.sock","pid":123}'), { state: 'running', socket: '/tmp/whip.sock', pid: 123 });
  for (const state of ['stopped', 'unhealthy'])
    assert.equal(parseDaemonStatus(JSON.stringify({ state, socket: '/tmp/whip.sock' })).state, state);
  for (const value of [
    null, { state: 'unknown' }, { socket: 'relative.sock' }, { socket: '/tmp/\nwhip' },
    { socket: '/tmp/' + 'é'.repeat(50) }, { error: 'x'.repeat(8193) }, { error: 3 },
    { pid: '123' }, { pid: 0 }, { pid: -1 }, { pid: 1.5 }, { stale_socket: 'true' },
  ]) expectInvalid(value);
  function expectInvalid(value: unknown) {
    const status = value === null ? null : { state: 'running', socket: '/tmp/whip.sock', ...value as object };
    assert.throws(() => parseDaemonStatus(JSON.stringify(status)), JSON.stringify(value));
  }
  assert.throws(() => parseDaemonStatus('{}'));
  assert.throws(() => parseDaemonStatus('banner\n{"state":"running","socket":"/tmp/whip.sock"}'));
});
