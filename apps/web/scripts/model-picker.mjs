// Real ModelPicker geometry, with an isolated catalog and no daemon/model calls.
import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, preview } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, firefox, expect } from '@playwright/test';

const web = fileURLToPath(new URL('../', import.meta.url));
const results = process.env.WHIP_MODEL_PICKER_RESULTS ?? '/tmp/whip-model-picker-results';
await mkdir(results, { recursive: true });
process.chdir(web); // StyleX source-package discovery follows the consumer cwd.
const config = {
  configFile: false, root: resolve(web, 'scripts/fixtures/model-picker'), logLevel: 'warn',
  plugins: [stylex.vite({ useCSSLayers: { before: ['whip-reset'] }, runtimeInjection: false,
    unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(web, '../..') } }), react()],
  build: { outDir: resolve(results, 'dist'), emptyOutDir: true, assetsInlineLimit: 0 },
  preview: { host: '127.0.0.1', port: 0, headers: {
    'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'",
  } },
};
await build(config);
const server = await preview(config);
const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
const report = [];
try {
  for (const [engine, browserType] of Object.entries({ chromium, firefox })) {
    if (process.env.WHIP_WEB_BROWSERS && !process.env.WHIP_WEB_BROWSERS.split(',').includes(engine)) continue;
    const browser = await browserType.launch();
    try {
      const page = await browser.newPage();
      await page.bringToFront();
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.addInitScript(() => {
        window.cspErrors = [];
        document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective));
      });
      const card = page.locator('[data-side][data-open] > [data-side][data-open]').filter({ hasText: 'Context' });
      const inside = async () => {
        await expect(card).toBeVisible();
        await expect.poll(async () => card.evaluate(element => {
          const rect = element.getBoundingClientRect();
          return rect.left >= 7 && rect.top >= 7 && rect.right <= innerWidth - 7 && rect.bottom <= innerHeight - 7;
        })).toBe(true);
      };
      for (const scenario of [
        { name: 'right', width: 1200, height: 800, x: 25, side: 'right' },
        { name: 'left', width: 740, height: 600, x: 85, side: 'left' },
        { name: 'neither-side', width: 390, height: 700, x: 50 },
        { name: 'short', width: 640, height: 280, x: 85 },
        { name: 'small', width: 320, height: 240, x: 50 },
      ]) {
        for (const theme of ['light', 'dark']) {
          await page.setViewportSize({ width: scenario.width, height: scenario.height });
          await page.goto(`${origin}/?x=${scenario.x}&theme=${theme}`);
          console.log(`${engine}: ${scenario.name}, ${theme}`);
          await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
          await page.getByRole('button', { name: 'Model', exact: true }).click();
          const options = page.getByRole('listbox', { name: 'Models', exact: true }).getByRole('option');
          await options.first().hover();
          await inside();
          if (scenario.side) await expect(card).toHaveAttribute('data-side', scenario.side);
          await page.screenshot({ path: resolve(results, `${engine}-${scenario.name}-${theme}.png`) });
          // End-of-list scrolling must move the anchor and keep the detail card visible.
          await options.last().hover();
          await inside();
          await expect(card).toContainText('model-39');
          await page.mouse.move(1, 1);
          await page.getByRole('textbox', { name: 'Search models' }).focus();
          await page.keyboard.press('Tab');
          // Firefox includes the scrollable list itself in the tab order.
          if (await page.getByRole('listbox', { name: 'Models', exact: true }).evaluate(element => element === document.activeElement))
            await page.keyboard.press('Tab');
          await expect(options.first()).toBeFocused();
          await inside();
          await page.setViewportSize({ width: 340, height: 260 });
          await inside();
          await page.keyboard.press('Escape');
          await expect(card).toBeHidden();
          if (await page.getByRole('textbox', { name: 'Search models' }).isVisible()) await page.keyboard.press('Escape');
          await expect(page.getByRole('button', { name: 'Model', exact: true })).toBeFocused();
          assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
          report.push(`${engine}: ${scenario.name}, ${theme}: bounds, scroll, keyboard, live resize, Escape`);
        }
      }
      assert.deepEqual(errors, []);
    } finally { await browser.close(); }
  }
  await writeFile(resolve(results, 'report.json'), JSON.stringify(report, null, 2) + '\n');
  console.log(`Passed ${report.length} model-picker layout scenarios. Artifacts: ${results}`);
} finally { await new Promise(resolve => server.httpServer.close(resolve)); }
