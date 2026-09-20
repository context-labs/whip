// Actual Electron test of production BrowserManager, IPC registration and preload.
// No production app instance, daemon or Chromium debug listener is involved.
import { build } from 'esbuild';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import electron from 'electron';
const directory = await mkdtemp(path.join(tmpdir(), 'whip-browser-production-'));
let child, fixture;
try {
  await build({ entryPoints: ['apps/desktop/scripts/browser-native-main.ts', 'apps/desktop/scripts/browser-feature-native-preload.ts', 'apps/desktop/src/preload.ts'], outdir: directory,
    bundle: true, platform: 'node', format: 'cjs', external: ['electron'], outExtension: { '.js': '.cjs' },
    entryNames: '[name]', define: { __APP_VERSION__: '"native-test"', __APP_NAME__: '"Whip"' } });
  await build({ entryPoints: ['apps/desktop/scripts/browser-workspace-native-renderer.ts'], outfile: path.join(directory, 'workspace.js'), bundle: true, platform: 'browser', format: 'iife', target: 'es2022' });
  if (process.env.BROWSER_NATIVE_DAEMON === '1') {
    const { startFixture } = await import('../../../packages/sdk/scripts/fixture.mjs');
    fixture = await startFixture({ env: { WHIP_SDK_AGENTS_FIXTURE: '1' } });
    await build({ entryPoints: ['apps/desktop/scripts/browser-discovery-native-renderer.ts'], outfile: path.join(directory, 'discovery.js'), bundle: true, platform: 'browser', format: 'iife', target: 'es2022' });
  }
  child = spawn(electron, [path.join(directory, 'browser-native-main.cjs')], { stdio: 'inherit', env: { ...process.env, BROWSER_NATIVE_DIRECTORY: directory, ...(fixture ? { BROWSER_NATIVE_FIXTURE: JSON.stringify(fixture.info) } : {}) } });
  const timeout = setTimeout(() => child.kill('SIGKILL'), fixture ? 90000 : 30000); timeout.unref();
  const [code, signal] = await once(child, 'exit'); clearTimeout(timeout);
  if (code !== 0) throw new Error(`Native browser production test exited ${code ?? signal}`);
} finally {
  if (child?.exitCode === null && child?.signalCode === null) child.kill('SIGKILL');
  await fixture?.close();
  await rm(directory, { recursive: true, force: true });
}
