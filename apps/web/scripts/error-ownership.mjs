import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, preview } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, expect } from '@playwright/test';
import { startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// --desktop keeps the isolated Electron app open for Computer acceptance.
// Default: real app workflows in Chromium, screenshots and assertion report.
const manual = process.argv.includes('--desktop');
const web = fileURLToPath(new URL('../', import.meta.url));
const results = process.env.WHIP_ERROR_RESULTS ?? '/tmp/whip-error-ownership-results';
const port = Number(process.env.WHIP_ERROR_PORT ?? 4177);
const origin = `http://127.0.0.1:${port}`;
process.env.WHIP_WEB_TURN_FAILURE_FIXTURE = '1';
process.env.WHIP_WEB_REPL_FIXTURE = '1';
await mkdir(results, { recursive: true });
const fixture = await startFixture({ allowedOrigins: [origin], lifetimeMs: manual ? 30 * 60_000 : 4 * 60_000 });
process.chdir(web);
const proxy = {
  '/api': { target: fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', ''), ws: true, changeOrigin: true },
  '/fixture': { target: fixture.info.frontend, changeOrigin: true, rewrite: path => path.replace(/^\/fixture/, '') },
};
const config = {
  configFile: false, root: resolve(web, 'scripts/fixtures/error-ownership'), logLevel: 'warn',
  define: { __FIXTURE__: JSON.stringify({ root_id: fixture.info.root_id, runtime_id: fixture.info.runtime_id,
    ...(manual ? { turn_agent: 'turn-failed-long' } : {}) }) },
  plugins: [stylex.vite({ useCSSLayers: { before: ['whip-reset'] }, runtimeInjection: false,
    unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(web, '../..') } }), react()],
  build: { outDir: resolve(results, 'dist'), emptyOutDir: true, assetsInlineLimit: 0 },
  preview: { host: '127.0.0.1', port, strictPort: true, proxy },
};
let server;
let browser;
try {
  await build(config);
  server = await preview(config);
  await writeFile(resolve(results, 'fixture.json'), JSON.stringify({ origin, ...fixture.info }, null, 2));
  if (manual) {
    const electron = spawn(resolve(web, '../../node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'),
      [resolve(web, 'scripts/fixtures/error-ownership/electron.cjs')], {
        env: { ...process.env, WHIP_ERROR_ORIGIN: origin, WHIP_ERROR_ELECTRON_DATA: resolve(results, 'electron-profile') },
        stdio: 'inherit',
      });
    console.log(`ERROR OWNERSHIP DESKTOP READY: ${origin}\nArtifacts: ${results}\nUse Error scenario in the separate fixture toolbar. Close Electron to stop.`);
    await new Promise((resolve, reject) => { electron.once('exit', resolve); electron.once('error', reject); });
  } else {
    browser = await chromium.launch();
    const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const report = [];
    const notice = type => page.locator(`[data-error-type="${type}"]`).filter({ visible: true });
    const composer = () => page.locator('[data-whip-composer]').filter({ visible: true });
    const capture = async name => {
      await page.evaluate(() => document.fonts.ready);
      await page.screenshot({ path: resolve(results, `${name}.png`), fullPage: true });
    };
    const scenario = async name => {
      await page.getByRole('combobox', { name: 'Error scenario' }).selectOption(name);
      await expect(page.getByRole('combobox', { name: 'Error scenario' })).toHaveValue(name);
      if (!['session', 'execution'].includes(name)) await expect(composer()).toBeVisible();
    };
    await page.goto(origin);
    await expect(composer()).toBeVisible();

    await scenario('application');
    await composer().fill('Keep this draft through a storage failure.');
    await expect(notice('application')).toHaveCount(1);
    await capture('application');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await expect(notice('application')).toHaveCount(0);
    await expect(composer()).toHaveValue('Keep this draft through a storage failure.');
    await capture('application-restored');
    report.push('Application: failed actual draft storage write; retained draft after restoring storage.');

    await scenario('host');
    await composer().fill('Keep this draft while offline.');
    await page.getByRole('button', { name: 'Disconnect', exact: true }).click();
    await expect(notice('host')).toHaveCount(1);
    await expect(notice('session')).toHaveCount(0);
    await expect(notice('application')).toHaveCount(0);
    await capture('host');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await expect(notice('host')).toHaveCount(0, { timeout: 15000 });
    await expect(composer()).toHaveValue('Keep this draft while offline.');
    await capture('host-restored');
    report.push('Host: one host error, no session/global duplicate, clears on reconnect, draft retained.');

    await scenario('session');
    await expect(notice('session')).toHaveCount(1);
    await expect(notice('host')).toHaveCount(0);
    await expect(page.getByText(/Reconnecting|Connecting/)).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'What would you like to work on?', exact: true })).toHaveCount(0);
    await capture('session');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await notice('session').getByRole('button', { name: 'Refresh' }).click();
    await expect(notice('session')).toHaveCount(0);
    await capture('session-restored');
    report.push('Session: snapshot failure scoped to session without connection or new-session messaging; Refresh recovers with host connected.');

    await scenario('combined');
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(1);
    await page.getByRole('button', { name: 'Disconnect', exact: true }).click();
    await expect(notice('host')).toHaveCount(1);
    await expect(notice('session')).toHaveCount(0);
    await expect(notice('application')).toHaveCount(0);
    await capture('combined');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await expect(notice('host')).toHaveCount(0, { timeout: 15000 });
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(1);
    await capture('combined-restored');
    report.push('Combined: host and recorded turn remain distinct; reconnect clears only host failure.');

    await scenario('execution');
    await expect(notice('execution')).toHaveCount(1);
    await notice('execution').locator('summary').click();
    await expect(page.getByText('synthetic Starlark division by zero', { exact: false })).toBeVisible();
    await capture('execution');
    report.push('Execution: persisted failing cell is visible in real REPL.');

    await scenario('submission');
    await composer().fill('Please retain this rejected message.');
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(notice('submission')).toHaveCount(1);
    await expect(notice('application')).toHaveCount(0);
    await expect(composer()).toHaveValue('Please retain this rejected message.');
    await capture('submission');
    report.push('Submission: rejected input appears beside composer with retained draft and no global duplicate.');

    await scenario('uncertain');
    await composer().fill('Verify this original identity before retrying.');
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(notice('submission')).toHaveCount(1, { timeout: 15000 });
    await capture('submission-uncertain');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await notice('submission').getByRole('button', { name: 'Check status' }).click();
    await expect(notice('submission')).toContainText(/not accepted|not found|not submitted/i);
    await capture('submission-uncertain-restored');
    report.push('Submission uncertainty: lost acknowledgement reconciles original identity without automatic replay.');

    await scenario('resource');
    await page.getByRole('button', { name: 'Model', exact: true }).click();
    await expect(notice('resource')).toHaveCount(1);
    await expect(notice('application')).toHaveCount(0);
    await capture('resource');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await page.getByRole('button', { name: 'Model', exact: true }).click();
    await notice('resource').getByRole('button', { name: 'Retry', exact: true }).click();
    await expect(notice('resource')).toHaveCount(0);
    await capture('resource-restored');
    report.push('Resource: model catalog failure stays inside its picker; Retry recovers after fault removal.');
    await page.keyboard.press('Escape');

    await scenario('action');
    await page.locator('[data-sidebar-session]').getByRole('button', { name: /^Actions for / }).click();
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click();
    const rename = page.getByRole('dialog', { name: 'Rename session' });
    await rename.getByRole('textbox').fill('Name retained on failure');
    await rename.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(notice('action')).toHaveCount(1);
    await expect(notice('application')).toHaveCount(0);
    await expect(rename.getByRole('textbox')).toHaveValue('Name retained on failure');
    await capture('action');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await page.locator('[data-sidebar-session]').getByRole('button', { name: /^Actions for / }).click();
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click();
    await rename.getByRole('textbox').fill('Name retained on failure');
    await rename.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(rename).toBeHidden({ timeout: 15000 });
    await capture('action-restored');
    report.push('Action: rename rejection stays in dialog; retry succeeds and preserves name.');

    await scenario('validation');
    await page.getByRole('link', { name: 'Settings', exact: true }).click();
    await page.getByRole('navigation', { name: 'Settings categories' }).getByRole('button', { name: 'Servers', exact: true }).click();
    await page.getByRole('button', { name: 'Add server', exact: true }).click();
    const serverDialog = page.getByRole('dialog', { name: 'Add server', exact: true });
    await serverDialog.getByRole('textbox', { name: 'Server address', exact: true }).fill('file:///not-a-server');
    await serverDialog.getByRole('button', { name: 'Add server', exact: true }).click();
    await expect(notice('validation')).toHaveCount(1);
    await expect(notice('application')).toHaveCount(0);
    await capture('validation');
    report.push('Validation: invalid server address stays inside the form.');
    await page.keyboard.press('Escape');

    await scenario('turn');
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(1);
    await capture('turn');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(0);
    await capture('turn-restored');
    report.push('Turn: genuine recorded outcome and successful follow-up recovery.');
    assert.deepEqual(errors, []);
    await writeFile(resolve(results, 'report.json'), JSON.stringify(report, null, 2));
    console.log(`Passed ${report.length} error ownership workflows. Artifacts: ${results}`);
  }
} finally {
  await browser?.close();
  if (server) await new Promise(resolve => server.httpServer.close(resolve));
  await fixture.close();
}
