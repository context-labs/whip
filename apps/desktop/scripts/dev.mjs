import { context } from 'esbuild';
import { createServer } from 'vite';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { lstat, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { buildDesktop, command } from './build.mjs';
import { fileDigest, LocalRuntime, readRuntimeManifest, run } from '../src/runtime.ts';
import { developmentOptions, developmentHelp } from '../src/development.ts';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

const desktop = fileURLToPath(new URL('../', import.meta.url));
const require = createRequire(import.meta.url);
const options = developmentOptions(process.argv.slice(2), desktop);
if (options.help) { console.log(developmentHelp); process.exit(0); }
const { env, attach } = options;
// Electron 44 intentionally has no npm postinstall. Fetch the pinned runtime.
await command(process.execPath, [require.resolve('electron/install.js')]);
let appDirectory;
let metadata;
if (attach) {
  // The source renderer and Electron shell need the SDK, but no Go/Swift payload.
  await command('npm', ['run', 'build']);
  const runtime = new LocalRuntime({ mode: 'attach', env,
    settingsFile: path.join(env.WHIP_DESKTOP_USER_DATA, 'native-local-runtime.json'),
    defaultExecutable: env.WHIP_DESKTOP_EXECUTABLE });
  const status = await runtime.test(AbortSignal.timeout(15_000));
  console.log(`Attach target: ${status.executable}\nDaemon home: ${status.home}\n${status.message}`);
  if (status.state !== 'running') throw new Error(status.message);
  appDirectory = options.appDirectory;
  metadata = { name: 'whip-desktop-dev', productName: 'Whip Dev',
    version: JSON.parse(await readFile(path.join(desktop, 'package.json'), 'utf8')).version, main: 'main.cjs' };
  await mkdir(appDirectory, { recursive: true });
  await writeFile(path.join(appDirectory, 'package.json'), JSON.stringify(metadata) + '\n');
} else {
  appDirectory = await buildDesktop();
  metadata = JSON.parse(await readFile(path.join(appDirectory, 'package.json'), 'utf8'));
  await mkdir(env.WHIPCODE_HOME, { recursive: true });
  const manifest = await readRuntimeManifest(path.join(appDirectory, 'runtime-manifest.json'));
  const signal = AbortSignal.timeout(15_000);
  const selected = env.WHIP_DESKTOP_EXECUTABLE;
  const previous = await lstat(selected).catch(error => { if (error.code !== 'ENOENT') throw error; });
  if (previous) {
    if (!previous.isFile() || previous.isSymbolicLink()) throw new Error('The development executable must be a real file.');
    if (await fileDigest(selected) !== manifest.files.whipcode.sha256) {
      const status = JSON.parse(await run(selected, ['daemon', 'status', '--json'], env, signal));
      if (status.state !== 'stopped') throw new Error('The development daemon is running with another build. Use --attach with --home and --executable to reuse this fixture, use --attach alone for the main daemon, or stop the fixture separately before rebuilding its backend.');
      await rm(selected);
    }
  }
  await new LocalRuntime({ source: path.join(desktop, '.stage/native'), manifest, env,
    settingsFile: path.join(env.WHIP_DESKTOP_USER_DATA, 'native-local-runtime.json'), defaultExecutable: selected }).install(selected, signal);
}
const server = await createServer({ root: path.join(repositoryRoot, 'apps/web'),
  configFile: path.join(repositoryRoot, 'apps/web/vite.config.ts'),
  server: { host: '127.0.0.1', port: options.port, strictPort: true } });
await server.listen();
let child; let closed = false; let restart = Promise.resolve();
async function stopWindow() {
  const previous = child; child = undefined;
  if (!previous || previous.exitCode !== null) return;
  const exited = once(previous, 'exit');
  previous.kill('SIGTERM');
  const timer = setTimeout(() => previous.kill('SIGKILL'), 5000);
  try { await exited; } finally { clearTimeout(timer); }
}
const watcher = await context({ absWorkingDir: desktop, entryPoints: { main: 'src/main.ts', preload: 'src/preload.ts', 'browser-design-preload': 'src/browser-design-preload.ts' },
  outdir: appDirectory, outExtension: { '.js': '.cjs' }, bundle: true, platform: 'node', format: 'cjs', target: 'node24',
  external: ['electron'], define: { __APP_VERSION__: JSON.stringify(metadata.version), __APP_NAME__: JSON.stringify(metadata.productName) },
  plugins: [{ name: 'restart-desktop-shell', setup(build) {
    build.onEnd(result => {
      if (result.errors.length) return;
      restart = restart.then(async () => {
        await stopWindow();
        if (!closed) {
          child = spawn(require('electron'), [appDirectory], { env, stdio: 'inherit' });
          child.on('error', error => console.error(error.message));
        }
      }).catch(error => console.error(error.message));
    });
  } }],
});
async function dispose() {
  if (closed) return; closed = true;
  await watcher.dispose(); await restart; await stopWindow(); await server.close();
}
process.once('SIGINT', () => void dispose()); process.once('SIGTERM', () => void dispose());
await watcher.watch();
console.log(`Desktop development ${attach ? 'attaches to' : 'uses'} ${env.WHIPCODE_HOME}. The daemon survives closing the GUI.\nRenderer HMR: ${env.WHIP_DESKTOP_DEV_URL} (one shared Vite server).`);
