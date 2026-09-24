// Current production renderer + actual sandboxed preload/native IPC, in an
// isolated unsigned Electron host. This is not signed installed-app acceptance.
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { execFile } from 'node:child_process';
import { once } from 'node:events';
import { cp, mkdir, mkdtemp, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import path from 'node:path';
import { promisify } from 'node:util';
import { _electron } from 'playwright';
import { fileDigest, LocalRuntime } from '../src/runtime.ts';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';
import { startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

const execute = promisify(execFile);
const fixture = await mkdtemp('/tmp/whip-editor-ipc-');
const application = path.join(fixture, 'app'); const native = path.join(fixture, 'native');
const executable = path.join(fixture, 'bin/whipcode');
const folderName = `${path.basename(fixture)} # % ü`;
const env = { PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh', HOME: path.join(fixture, 'user'), TMPDIR: path.join(fixture, 'tmp'),
  WHIP_DESKTOP_FIXTURE: '1', WHIPCODE_HOME: path.join(fixture, 'home'), WHIPCODE_NETWORK: '0', WHIP_DESKTOP_EXECUTABLE: executable,
  WHIP_DESKTOP_USER_DATA: path.join(fixture, 'user-data') };
let electron; let remote; let installed = false; let finderOpened = false;
const peers = new Set();
const pendingServer = createServer(peer => { peers.add(peer); peer.on('close', () => peers.delete(peer)); });
try {
  for (const directory of [env.HOME, env.TMPDIR, env.WHIPCODE_HOME, env.WHIP_DESKTOP_USER_DATA, native, path.dirname(executable)])
    await mkdir(directory, { recursive: true });
  const stage = path.join(repositoryRoot, 'apps/desktop/.stage');
  await cp(path.join(stage, 'app'), application, { recursive: true });
  await cp(path.join(stage, 'native/whip-computer'), path.join(native, 'whip-computer'));
  const metadata = JSON.parse(await readFile(path.join(application, 'package.json'), 'utf8'));
  const renderer = JSON.parse(await readFile(path.join(repositoryRoot, 'apps/web/renderer-manifest.json'), 'utf8'));
  await rm(path.join(application, 'renderer'), { recursive: true, force: true });
  await cp(path.join(repositoryRoot, 'apps/web/dist'), path.join(application, 'renderer'), { recursive: true });
  await writeFile(path.join(application, 'renderer-manifest.json'), JSON.stringify(renderer));
  const manifest = JSON.parse(await readFile(path.join(application, 'runtime-manifest.json'), 'utf8'));
  const overlay = path.join(fixture, 'overlay.json');
  await writeFile(overlay, JSON.stringify({ Replace: { [path.join(repositoryRoot, 'internal/computer/bin/whip-computer')]: path.join(native, 'whip-computer') } }));
  await execute('go', ['build', '-overlay', overlay, '-ldflags', `-X main.version=${manifest.buildId} -X github.com/context-labs/whip/internal/buildinfo.Name=whipcode -X github.com/context-labs/whip/internal/buildinfo.UpdateOwner=desktop`,
    '-o', path.join(native, 'whipcode'), './cmd/whip'], { cwd: repositoryRoot, timeout: 120_000, maxBuffer: 1 << 20 });
  const info = JSON.parse((await execute(path.join(native, 'whipcode'), ['_desktop-runtime-info'])).stdout);
  manifest.rendererDigest = renderer.digest;
  manifest.source = { ...renderer.source, lockfile: renderer.lockfile };
  delete manifest.teamId; // This diagnostic does not assert release signing.
  manifest.compatibility = { protocolMajor: info.protocolMajor, protocolMinor: info.protocolMinor, schemaVersion: info.schemaVersion };
  manifest.files.whipcode = { bytes: (await stat(path.join(native, 'whipcode'))).size, sha256: await fileDigest(path.join(native, 'whipcode')) };
  await writeFile(path.join(application, 'runtime-manifest.json'), JSON.stringify(manifest));
  await build({ absWorkingDir: path.join(repositoryRoot, 'apps/desktop'), entryPoints: { main: 'src/main.ts', preload: 'src/preload.ts' },
    outdir: application, outExtension: { '.js': '.cjs' }, bundle: true, platform: 'node', format: 'cjs', target: 'node24', external: ['electron'],
    define: { __APP_VERSION__: JSON.stringify(metadata.version), __APP_NAME__: JSON.stringify(metadata.productName) } });
  await new LocalRuntime({ source: native, manifest, env, settingsFile: path.join(env.WHIP_DESKTOP_USER_DATA, 'native-local-runtime.json'), defaultExecutable: executable })
    .install(executable, AbortSignal.timeout(15_000));
  installed = true;
  remote = await startFixture({ allowedOrigins: ['whip-app://bundle'] });
  electron = await _electron.launch({ args: [application], env, timeout: 30_000 });
  const page = await electron.firstWindow();
  await page.waitForFunction(() => JSON.parse(localStorage.getItem('whip.hosts.v2') || '[]').some(host => host.id === 'local' && host.runtimeId), undefined, { timeout: 30_000 });
  const profile = await page.evaluate(() => JSON.parse(localStorage.getItem('whip.hosts.v2')).find(host => host.id === 'local'));
  const editors = await page.evaluate(() => window.whipDesktop.listProjectEditors());
  assert.deepEqual(editors.map(editor => editor.id), ['cursor', 'vscode', 'zed', 'finder']);
  const directory = path.join(fixture, folderName); await mkdir(directory);
  const request = { app: 'finder', directory, connectionId: 'editor-ipc', runtimeId: profile.runtimeId };
  const invoke = input => page.evaluate(async input => {
    try { await window.whipDesktop.openProject(input.request, input.source); return 'opened'; }
    catch (error) { return error.message; }
  }, input);
  assert.match(await invoke({ request }), /source host is disconnected/);
  await page.evaluate(profile => window.whipDesktop.prepareConnection('editor-ipc', profile), profile);
  assert.match(await invoke({ request: { ...request, runtimeId: 'replacement-runtime' } }), /different runtime/);
  assert.match(await invoke({ request: { ...request, directory: path.join(directory, 'missing') } }), /directory no longer exists/);
  if (process.env.WHIP_EDITOR_IPC_OPEN_FINDER === '1') {
    assert.equal(await invoke({ request }), 'opened'); finderOpened = true;
  }
  const remoteRequest = { ...request, connectionId: 'remote-url', runtimeId: remote.info.runtime_id, sshAlias: 'whip-editor-fixture' };
  const source = { id: 'remote-url', label: 'Remote fixture', runtimeId: remote.info.runtime_id, target: { kind: 'url', endpoint: remote.info.endpoint } };
  assert.match(await invoke({ request: remoteRequest, source }), /Finder.*This Mac/);
  assert.match(await invoke({ request: { ...remoteRequest, app: 'cursor', sshAlias: undefined }, source }), /Configure SSH for editors/);
  pendingServer.listen(0, '127.0.0.1'); await once(pendingServer, 'listening');
  const accepted = once(pendingServer, 'connection');
  const pending = invoke({ request: { ...remoteRequest, connectionId: 'pending-url' },
    source: { ...source, target: { kind: 'url', endpoint: `http://127.0.0.1:${pendingServer.address().port}` } } });
  await accepted;
  await page.evaluate(() => window.whipDesktop.releaseConnection('pending-url'));
  let timer;
  const cancelled = await Promise.race([pending, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('URL source disposal did not cancel native verification')), 3000); })]);
  clearTimeout(timer); assert.match(cancelled, /host could not be verified/);
  // A cancelled URL effect must release the global in-flight editor gate.
  assert.match(await invoke({ request: remoteRequest, source }), /Finder.*This Mac/);
  await page.evaluate(() => window.whipDesktop.releaseConnection('editor-ipc'));
  assert.match(await invoke({ request }), /source host is disconnected/);
  const output = process.env.WHIP_EDITOR_IPC_SMOKE_OUTPUT ?? path.join(repositoryRoot, '.ai-docs/plans/conversation-row-actions/native-ipc-smoke.json');
  const result = { recordedAt: new Date().toISOString(), scope: 'Current production renderer with real sandboxed preload/native IPC in isolated stock Electron; not signed installed-app acceptance',
    rendererDigest: renderer.digest, protocolMajor: info.protocolMajor, schemaVersion: info.schemaVersion, editorDiscovery: true, localFinderLaunch: finderOpened, staleRuntimeRejected: true,
    missingDirectoryActionable: true, urlSourceNeverLocal: true, urlSourceNeedsAlias: true, urlSourceReleaseCancelsVerification: true, detachedNativeSourceRejected: true };
  await writeFile(output, JSON.stringify(result, null, 2) + '\n'); console.log(JSON.stringify(result, null, 2));
} finally {
  if (electron) await electron.evaluate(({ app }) => app.exit(0)).catch(() => {});
  for (const peer of peers) peer.destroy(); if (pendingServer.listening) await new Promise(resolve => pendingServer.close(resolve));
  await remote?.close();
  if (finderOpened) await execute('/usr/bin/osascript', ['-e', `tell application "System Events" to tell process "Finder" to click (first button of window ${JSON.stringify(folderName)} whose description is "close button")`], { timeout: 3000 }).catch(() => {});
  if (installed) {
    await execute(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
    assert.equal(JSON.parse((await execute(executable, ['daemon', 'status', '--json'], { env })).stdout).state, 'stopped');
  }
  await rm(fixture, { recursive: true, force: true });
}
