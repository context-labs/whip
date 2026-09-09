// Feasibility evidence only: production renderer at its real scheme, with no daemon.
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir, release, cpus } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { performance } from 'node:perf_hooks';
import { build } from 'esbuild';
import { _electron } from 'playwright';
import { readRendererManifest, verifyRenderer } from '../../../scripts/renderer-artifact.mjs';

const root = fileURLToPath(new URL('../../../', import.meta.url));
const temporary = await mkdtemp(path.join(tmpdir(), 'whip-electron-probe-'));
const renderer = path.join(root, 'apps/web/dist');
const manifestPath = path.join(root, 'apps/web/renderer-manifest.json');
const manifest = await readRendererManifest(manifestPath);
await verifyRenderer(renderer, manifest);
const observations = [];
const server = createServer((request, response) => {
  const origin = request.headers.origin;
  observations.push({ method: request.method, path: request.url, origin });
  if (origin) response.setHeader('Access-Control-Allow-Origin', origin);
  response.setHeader('Access-Control-Allow-Methods', 'GET, POST, OPTIONS');
  response.setHeader('Access-Control-Allow-Headers', 'X-Whip-Probe');
  if (request.method === 'OPTIONS') { response.writeHead(204); response.end(); }
  else { response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ origin })); }
});
server.on('upgrade', (request, socket) => {
  observations.push({ method: 'WS', path: request.url, origin: request.headers.origin });
  socket.end('HTTP/1.1 403 Probe complete\r\nConnection: close\r\nContent-Length: 0\r\n\r\n');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const endpoint = `http://127.0.0.1:${server.address().port}`;
let electron;
try {
  const preload = path.join(temporary, 'preload.cjs');
  await writeFile(preload, `const { contextBridge, ipcRenderer } = require('electron');
localStorage.setItem('whip.hosts.v2', '[]');
localStorage.setItem('whip.selectedHost.v2', JSON.stringify('disconnected-probe'));
contextBridge.exposeInMainWorld('whipDesktop', { version: 2, getSystemContrast: async () => false, appVersion: 'probe',
 connectionKinds: ['local', 'url', 'ssh'], onEvent: () => () => {}, ready: () => ipcRenderer.send('probe:ready') });`);
  const main = path.join(temporary, 'main.cjs');
  await build({ stdin: { contents: `import { app, BrowserWindow, protocol } from 'electron';
import { createAssetHandler } from ${JSON.stringify(path.join(root, 'apps/desktop/src/assets.ts'))};
import { readFileSync } from 'node:fs';
app.setPath('userData', ${JSON.stringify(path.join(temporary, 'user-data'))});
protocol.registerSchemesAsPrivileged([{ scheme: 'whip-app', privileges: { standard: true, secure: true, supportFetchAPI: true, corsEnabled: true } }]);
app.whenReady().then(async () => {
 protocol.handle('whip-app', createAssetHandler(${JSON.stringify(renderer)}, JSON.parse(readFileSync(${JSON.stringify(manifestPath)}, 'utf8'))));
 const window = new BrowserWindow({ width: 1200, height: 800, show: true, webPreferences: { preload: ${JSON.stringify(preload)}, sandbox: true, contextIsolation: true, nodeIntegration: false } });
 await window.loadURL('whip-app://bundle');
});
app.on('window-all-closed', () => app.quit());`, resolveDir: root }, outfile: main, bundle: true, platform: 'node', format: 'cjs', external: ['electron'], logLevel: 'silent' });
  const started = performance.now();
  electron = await _electron.launch({ args: [main], timeout: 30_000 });
  const page = await electron.firstWindow();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.getByRole('heading', { name: 'What would you like to work on?' }).waitFor();
  const elapsed = performance.now() - started;
  const browser = await page.evaluate(async endpoint => {
    const id = crypto.randomUUID();
    localStorage.setItem('probe', id);
    const lock = await navigator.locks.request('probe', () => localStorage.getItem('probe'));
    const direct = await fetch(`${endpoint}/content`, { method: 'POST', headers: { 'X-Whip-Probe': '1' } }).then(response => response.json());
    await new Promise(resolve => { const ws = new WebSocket(endpoint.replace('http:', 'ws:') + '/socket'); ws.onerror = () => resolve(); });
    await document.fonts.ready;
    return { origin: location.origin, secure: isSecureContext, lockWorks: lock === id,
      nodeAvailable: typeof window.require !== 'undefined', directOrigin: direct.origin,
      fonts: document.fonts.status, stylesheets: document.styleSheets.length,
      background: getComputedStyle(document.body).backgroundColor,
      readyMark: performance.getEntriesByName('whip-shell-ready')[0]?.startTime };
  }, endpoint);
  assert.equal(browser.origin, 'whip-app://bundle');
  assert.equal(browser.directOrigin, 'whip-app://bundle');
  assert.equal(browser.secure, true);
  assert.equal(browser.lockWorks, true);
  assert.equal(browser.nodeAvailable, false);
  assert.ok(browser.stylesheets > 0);
  assert.ok(observations.some(item => item.method === 'OPTIONS' && item.origin === 'whip-app://bundle'));
  assert.ok(observations.some(item => item.method === 'WS' && item.origin === 'whip-app://bundle'));
  assert.deepEqual(errors, []);
  await page.goto('whip-app://bundle/settings');
  await page.getByRole('heading', { name: 'Appearance', exact: true }).waitFor();
  await page.reload();
  await page.getByRole('heading', { name: 'Appearance', exact: true }).waitFor();
  const evidence = { recordedAt: new Date().toISOString(), purpose: 'Scheme/CSP/storage/renderer feasibility, not a release startup benchmark',
    electron: (await electron.evaluate(({ app }) => process.versions.electron)), os: release(), cpu: cpus()[0]?.model,
    rendererDigest: manifest.digest, observedLaunchToHeadingMs: elapsed, browser, observations, routeReload: true, errors };
  const directory = path.join(root, '.ai-docs/plans/desktop-app/evidence');
  await mkdir(directory, { recursive: true });
  await writeFile(path.join(directory, 'electron-scheme.json'), JSON.stringify(evidence, null, 2) + '\n');
  console.log(JSON.stringify(evidence, null, 2));
} finally {
  await electron?.close();
  await new Promise(resolve => server.close(resolve));
  await rm(temporary, { recursive: true, force: true });
}
