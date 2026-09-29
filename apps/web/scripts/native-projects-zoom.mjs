import assert from 'node:assert/strict';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { _electron } from 'playwright';
import { expect } from '@playwright/test';

// Real Chromium zoom in an isolated stock-Electron window loading the current
// web artifact. This is reflow acceptance, not signed desktop/OS picker evidence.
const repository = resolve(process.argv[2] ?? fileURLToPath(new URL('../../../', import.meta.url)));
const { startFixture } = await import(pathToFileURL(join(repository, 'apps/web/scripts/native-fixture.mjs')));
const output = process.env.WHIP_PROJECTS_ZOOM_RESULTS ?? '/tmp/whip-projects-zoom';
await mkdir(output, { recursive: true });
const directory = await mkdtemp('/tmp/whip-projects-zoom-');
let fixture, electron, page;
const checks = [], errors = [];
const manifest = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
const report = { rendererDigest: manifest.digest, source: manifest.source, checks, errors,
  scope: 'Current renderer, disposable daemon, isolated stock Electron, actual webContents zoom; no installed app or OS-native picker chrome.' };
try {
  fixture = await startFixture();
  const client = await fixture.connect('projects-zoom'), { root } = await fixture.createRoot(client, { title: 'Zoom project' });
  for (const name of ['user', 'tmp', 'profile']) await mkdir(join(directory, name));
  const main = join(directory, 'main.cjs');
  await writeFile(main, `const {app,BrowserWindow}=require('electron');
app.setPath('userData',process.env.WHIP_ZOOM_PROFILE);
app.whenReady().then(()=>{const w=new BrowserWindow({width:1280,height:1200,webPreferences:{sandbox:true,contextIsolation:true,nodeIntegration:false}});w.loadURL(process.env.WHIP_ZOOM_URL)});
app.on('window-all-closed',()=>app.quit());`);
  electron = await _electron.launch({ args: [main], env: { PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh',
    HOME: join(directory, 'user'), TMPDIR: join(directory, 'tmp'), WHIP_ZOOM_PROFILE: join(directory, 'profile'), WHIP_ZOOM_URL: fixture.info.web } });
  page = await electron.firstWindow(); page.setDefaultTimeout(15000);
  page.on('pageerror', error => { if (errors.length < 32) errors.push(error.message); });
  // Playwright's page capture clips the Retina surface at Electron page zoom.
  // Native capturePage preserves the full window at the actual device scale.
  const capture = async label => {
    const encoded = await electron.evaluate(async ({ BrowserWindow }) =>
      (await BrowserWindow.getAllWindows()[0].webContents.capturePage()).toPNG().toString('base64'));
    await writeFile(join(output, `${label}.png`), Buffer.from(encoded, 'base64'));
  };
  const check = async (dialog, label) => {
    const geometry = await dialog.evaluate(node => { const b = node.getBoundingClientRect(); return {
      width: innerWidth, page: document.documentElement.scrollWidth, left: b.left, right: b.right,
      client: node.clientWidth, scroll: node.scrollWidth,
    }; });
    assert(geometry.left >= -1 && geometry.right <= geometry.width + 1 && geometry.page <= geometry.width + 1
      && geometry.scroll <= geometry.client + 1, `${label}: ${JSON.stringify(geometry)}`);
    await capture(label);
    return geometry;
  };
  for (const width of [530, 390, 320]) {
    await electron.evaluate(({ BrowserWindow }, width) => { const window = BrowserWindow.getAllWindows()[0]; window.setContentSize(width * 2, 1400); window.webContents.setZoomFactor(2); }, width);
    await expect.poll(() => page.evaluate(() => innerWidth)).toBe(width);
    await page.goto(`${fixture.info.web}/settings?section=execution`);
    await page.getByRole('button', { name: 'New agent', exact: true }).click();
    const editor = page.getByRole('dialog', { name: 'New agent', exact: true });
    await check(editor, `${width}-agent-200pct`);
    const register = editor.getByRole('button', { name: 'Register agent', exact: true });
    await register.scrollIntoViewIfNeeded();
    await expect(register).toBeInViewport();
    await capture(`${width}-agent-footer-200pct`);
    await editor.getByRole('button', { name: 'Close', exact: true }).click();
    await page.goto(`${fixture.info.web}/?new=1&runtimeId=${fixture.info.runtime_id}&cwd=${encodeURIComponent(fixture.directory)}`);
    await page.getByRole('button', { name: 'Project folder', exact: true }).click();
    const picker = page.getByRole('dialog', { name: 'Choose a folder', exact: true });
    await picker.getByRole('button', { name: 'New folder', exact: true }).click();
    await picker.getByRole('textbox', { name: 'Folder name', exact: true }).fill('Actual zoom form');
    const geometry = await check(picker, `${width}-folder-200pct`);
    await picker.getByRole('button', { name: 'Create', exact: true }).scrollIntoViewIfNeeded();
    await expect(picker.getByRole('button', { name: 'Create', exact: true })).toBeInViewport();
    await picker.getByRole('button', { name: 'Close', exact: true }).click();
    await page.goto(`${fixture.info.web}/h/${fixture.info.runtime_id}/s/${root.id}`);
    await expect(page.getByRole('textbox', { name: 'Message WHIP', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    const navigation = page.getByRole('dialog', { name: 'WHIP', exact: true });
    const projects = await check(navigation, `${width}-projects-200pct`);
    await expect(navigation.getByRole('region', { name: 'Projects', exact: true })).toBeVisible();
    const wordmark = await navigation.getByRole('img', { name: 'Whipcode', exact: true }).boundingBox();
    assert(wordmark && wordmark.x >= projects.left && wordmark.x + wordmark.width <= projects.right,
      `Projects wordmark fits inside the sheet at ${width}px: ${JSON.stringify(wordmark)}`);
    await navigation.getByRole('button', { name: 'Close', exact: true }).click();
    checks.push({ width, zoomFactor: 2, geometry, projects, wordmark, agentFooterReachable: true });
  }
  assert.deepEqual(errors, []);
} catch (error) {
  report.failure = String(error.stack ?? error);
  if (electron) await electron.evaluate(async ({ BrowserWindow }) =>
    (await BrowserWindow.getAllWindows()[0].webContents.capturePage()).toPNG().toString('base64'))
    .then(encoded => writeFile(join(output, 'failure.png'), Buffer.from(encoded, 'base64'))).catch(() => {});
  await writeFile(join(output, 'failure.txt'), (await page?.locator('body').innerText().catch(() => '') ?? '').slice(0, 16384));
} finally {
  for (const close of [() => electron?.close(), () => fixture?.close(), () => rm(directory, { recursive: true, force: true })]) {
    try { await close(); } catch (error) { report.failure = `Cleanup failed: ${String(error)}`; }
  }
  await writeFile(join(output, 'report.json'), JSON.stringify(report, null, 2));
}
console.log(JSON.stringify(report, null, 2));
if (report.failure) process.exitCode = 1;
