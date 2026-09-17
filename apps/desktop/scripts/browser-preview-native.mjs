// Opt-in real-host native integration. Remote fixture lifecycle belongs to its creator.
import { build } from 'esbuild';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import electron from 'electron';
if (!process.env.BROWSER_PREVIEW_NATIVE_MANIFEST) throw new Error('Explicit owned SSH fixture manifest is required; this test never chooses a host');
const manifest = JSON.parse(await readFile(process.env.BROWSER_PREVIEW_NATIVE_MANIFEST, 'utf8'));
if (!manifest.localHelperPath || manifest.connectionProfile?.target?.kind !== 'ssh' || !manifest.runtimeId || !manifest.projectId || !manifest.remoteHTTPMarker || [manifest.remoteHTTPPort, manifest.remoteUnapprovedPort, manifest.remoteIPv6Port].some(port => !Number.isInteger(port) || port < 1 || port > 65535)) throw new Error('Incomplete owned SSH fixture manifest');
const directory = await mkdtemp(path.join(tmpdir(), 'whip-browser-preview-native-'));
let child;
try {
  await build({ entryPoints: ['apps/desktop/scripts/browser-preview-native-main.ts', 'apps/desktop/src/preload.ts'], outdir: directory,
    bundle: true, platform: 'node', format: 'cjs', external: ['electron'], outExtension: { '.js': '.cjs' }, entryNames: '[name]',
    define: { __APP_VERSION__: '"native-preview-test"', __APP_NAME__: '"Whip"' } });
  if (process.env.BROWSER_PREVIEW_NATIVE_SDK === '1') await build({ entryPoints: ['apps/desktop/scripts/browser-sdk-native-renderer.ts'], outfile: path.join(directory, 'sdk.js'), bundle: true, platform: 'browser', format: 'iife', target: 'es2022' });
  child = spawn(electron, [path.join(directory, 'browser-preview-native-main.cjs')], { stdio: 'inherit', env: { ...process.env, BROWSER_PREVIEW_NATIVE_DIRECTORY: directory } });
  const timer = setTimeout(() => child.kill('SIGTERM'), 300_000); timer.unref();
  const killTimer = setTimeout(() => child.kill('SIGKILL'), 315_000); killTimer.unref();
  const [code, signal] = await once(child, 'exit'); clearTimeout(timer); clearTimeout(killTimer);
  if (code !== 0) throw new Error(`Native SSH preview test exited ${code ?? signal}`);
} finally {
  if (child?.exitCode === null && child?.signalCode === null) child.kill('SIGKILL');
  await rm(directory, { recursive: true, force: true });
}
