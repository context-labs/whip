import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, preview } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, firefox, expect } from '@playwright/test';

const web = fileURLToPath(new URL('../', import.meta.url));
const results = '/tmp/whip-ssh-profile-results';
await mkdir(results, { recursive: true });
process.chdir(web);
const config = {
  configFile: false, root: resolve(web, 'scripts/fixtures/ssh-profiles'), logLevel: 'warn',
  plugins: [stylex.vite({ useCSSLayers: { before: ['whip-reset'] }, runtimeInjection: false,
    unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(web, '../..') } }), react()],
  build: { outDir: resolve(results, 'dist'), emptyOutDir: true, assetsInlineLimit: 0 },
  preview: { host: '127.0.0.1', port: 0, headers: { 'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'" } },
};
await build(config);
const server = await preview(config);
const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
const report = [];
try {
  for (const [engine, browserType] of Object.entries({ chromium, firefox })) {
    const browser = await browserType.launch();
    try {
      const page = await browser.newPage();
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.addInitScript(() => { window.cspErrors = []; document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective)); });
      for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }, { width: 640, height: 400 }]) {
        for (const theme of ['dark', 'light']) {
          await page.setViewportSize(viewport);
          await page.goto(`${origin}/?theme=${theme}`);
          const dialog = page.getByRole('dialog');
          await page.getByRole('tab', { name: 'SSH', exact: true }).click();
          const radio = page.getByRole('radio', { name: 'kuzco-4090' });
          await expect(radio).toBeVisible();
          await expect(dialog.getByRole('button', { name: 'Connect', exact: true })).toBeDisabled();
          await radio.click();
          await expect(radio).toBeChecked();
          await expect(radio.locator('svg')).toBeVisible();
          await page.evaluate(() => document.fonts.ready);
          await page.screenshot({ animations: 'disabled', path: resolve(results, `${engine}-${viewport.width}-${theme}-profiles.png`) });
          const bounds = await dialog.evaluate(element => ({ width: element.scrollWidth, clientWidth: element.clientWidth, rect: element.getBoundingClientRect().toJSON(), viewport: innerWidth }));
          assert.ok(bounds.width <= bounds.clientWidth + 1, JSON.stringify(bounds));
          assert.ok(bounds.rect.left >= 15 && bounds.rect.right <= bounds.viewport - 15, JSON.stringify(bounds));
          await dialog.getByRole('button', { name: /Advanced SSH options/ }).click();
          await page.getByLabel('SSH host or alias').fill('manual-host');
          await page.getByLabel('Username', { exact: true }).fill('deploy');
          await page.screenshot({ animations: 'disabled', path: resolve(results, `${engine}-${viewport.width}-${theme}-manual.png`) });
          await page.getByRole('button', { name: 'Choose a profile' }).click();
          await expect(radio).toBeChecked();
          await dialog.getByRole('button', { name: /Advanced SSH options/ }).click();
          await expect(page.getByLabel('SSH host or alias')).toHaveValue('manual-host');
          await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
          await expect(dialog).toHaveCount(0);
          assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
          report.push(`${engine}: ${viewport.width}x${viewport.height} ${theme} — profile selection, manual draft, submit, geometry, CSP`);
        }
      }
      await page.setViewportSize({ width: 1440, height: 900 });
      for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }, { width: 640, height: 400 }]) {
        await page.setViewportSize(viewport);
        await page.goto(`${origin}/?connecting=1`);
        await page.getByRole('tab', { name: 'SSH', exact: true }).click();
        await page.getByRole('radio', { name: 'kuzco-4090' }).click();
        await page.getByRole('dialog').getByRole('button', { name: 'Connect', exact: true }).click();
        const cancel = page.getByRole('button', { name: 'Cancel connection' });
        await expect(page.getByText('Connecting to kuzco-4090…', { exact: true })).toHaveCount(1);
        await expect(page.getByRole('button', { name: 'Connecting…' })).toHaveCount(0);
        await expect(cancel).toBeInViewport();
        await page.screenshot({ animations: 'disabled', path: resolve(results, `${engine}-${viewport.width}-connecting.png`) });
        await cancel.click();
        await expect(page.getByRole('radio', { name: 'kuzco-4090' })).toBeChecked();
        report.push(`${engine}: ${viewport.width} — single connection status, visible cancel, preserved selection`);
      }
      await page.setViewportSize({ width: 1440, height: 900 });
      for (const scenario of ['empty', 'error', 'failure', 'slow']) {
        await page.goto(`${origin}/?${scenario}=1`);
        await page.getByRole('tab', { name: 'SSH', exact: true }).click();
        if (scenario === 'empty') await expect(page.getByText('No SSH profiles found', { exact: true })).toBeVisible();
        if (scenario === 'error') {
          await expect(page.getByText('Couldn’t read SSH profiles')).toBeVisible();
          await page.getByRole('button', { name: 'Retry', exact: true }).click();
        }
        if (scenario === 'slow') {
          const list = page.locator('[aria-busy]').last();
          await expect(page.getByText('Loading SSH profiles…')).toBeVisible();
          const initial = await list.boundingBox();
          await page.getByRole('radio', { name: 'kuzco-4090' }).waitFor();
          assert.equal((await list.boundingBox()).height, initial.height);
        }
        if (scenario === 'failure') {
          await page.getByRole('radio', { name: 'kuzco-4090' }).click();
          await page.getByRole('dialog').getByRole('button', { name: 'Connect', exact: true }).click();
          await expect(page.getByRole('button', { name: 'Cancel connection' })).toBeVisible();
          await expect(page.getByRole('button', { name: 'Try again' })).toBeVisible();
          await page.getByRole('button', { name: 'Edit connection' }).click();
          await expect(page.getByRole('radio', { name: 'kuzco-4090' })).toBeChecked();
        }
        await page.screenshot({ animations: 'disabled', path: resolve(results, `${engine}-${scenario}.png`) });
        report.push(`${engine}: ${scenario}`);
      }
      for (const reconnect of [false, true]) {
        for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }, { width: 640, height: 400 }]) {
          await page.setViewportSize(viewport);
          await page.goto(`${origin}/?auth=1${reconnect ? '&reconnect=1' : ''}`);
          if (reconnect) {
            await page.getByRole('button', { name: 'Actions for Staging' }).click();
            await page.getByRole('menuitem', { name: 'Connect', exact: true }).click();
          } else {
            await page.getByRole('tab', { name: 'SSH', exact: true }).click();
            await page.getByRole('radio', { name: 'kuzco-4090' }).click();
            await page.getByRole('dialog').getByRole('button', { name: 'Connect', exact: true }).click();
          }
          const dialog = page.getByRole('dialog');
          await expect(page.getByRole('button', { name: 'Cancel connection' })).toBeVisible();
          const progressBounds = await dialog.boundingBox();
          const password = page.getByLabel('Password', { exact: true });
          await expect(password).toBeVisible();
          await expect(dialog).toHaveCount(1);
          await expect(page.getByRole('status')).toHaveCount(0);
          const authBounds = await dialog.boundingBox();
          assert.ok(Math.abs(authBounds.height - progressBounds.height) <= 2, JSON.stringify({ progressBounds, authBounds }));
          await expect(page.getByRole('button', { name: 'Continue', exact: true })).toBeInViewport();
          await page.screenshot({ path: resolve(results, `${engine}-${viewport.width}-${reconnect ? 'reconnect' : 'initial'}-auth.png`) });
          await password.fill('synthetic-test-password');
          await page.getByRole('button', { name: 'Continue', exact: true }).click();
          await expect(dialog).toHaveCount(0);
          assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
          report.push(`${engine}: ${viewport.width} ${reconnect ? 'reconnect' : 'initial'} — one dialog, inline authentication, stable geometry, verified completion`);
        }
      }
      assert.deepEqual(errors, []);
    } finally { await browser.close(); }
  }
  await writeFile(resolve(results, 'report.json'), JSON.stringify(report, null, 2));
  console.log(report.join('\n'));
} finally { await new Promise(resolve => server.httpServer.close(resolve)); }
