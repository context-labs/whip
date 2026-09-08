import { context } from 'esbuild';
import { createServer } from 'vite';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdir, readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { buildDesktop, command } from './build.mjs';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

const desktop = fileURLToPath(new URL('../', import.meta.url));
const require = createRequire(import.meta.url);
// Electron 44 intentionally has no npm postinstall. Fetch the pinned runtime.
await command(process.execPath, [require.resolve('electron/install.js')]);
const appDirectory = await buildDesktop();
const metadata = JSON.parse(await readFile(path.join(appDirectory, 'package.json'), 'utf8'));
const fixture = path.join(desktop, '.dev');
await mkdir(path.join(fixture, 'home'), { recursive: true });
const server = await createServer({ root: path.join(repositoryRoot, 'apps/web'),
  configFile: path.join(repositoryRoot, 'apps/web/vite.config.ts'),
  server: { host: '127.0.0.1', port: 3001, strictPort: true } });
await server.listen();
const env = { ...process.env, WHIP_DESKTOP_FIXTURE: '1', WHIP_HOME: path.join(fixture, 'home'),
  WHIP_DESKTOP_USER_DATA: path.join(fixture, 'user-data'), WHIP_DESKTOP_DEV_URL: 'http://127.0.0.1:3001/' };
for (const key of ['WHIP_NETWORK', 'WHIP_LISTEN', 'WHIP_ALLOWED_HOSTS', 'WHIP_ALLOWED_ORIGINS']) delete env[key];
let child; let closed = false; let restart = Promise.resolve();
async function stopWindow() {
  const previous = child; child = undefined;
  if (!previous || previous.exitCode !== null) return;
  const exited = once(previous, 'exit');
  previous.kill('SIGTERM');
  const timer = setTimeout(() => previous.kill('SIGKILL'), 5000);
  try { await exited; } finally { clearTimeout(timer); }
}
const watcher = await context({ absWorkingDir: desktop, entryPoints: { main: 'src/main.ts', preload: 'src/preload.ts' },
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
console.log(`Desktop development uses ${fixture}. The fixture daemon survives closing the GUI.\nRenderer HMR: http://127.0.0.1:3001 (one shared Vite server).`);
