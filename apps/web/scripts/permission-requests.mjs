import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, preview } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, firefox, expect } from '@playwright/test';

const web = fileURLToPath(new URL('../', import.meta.url));
const results = process.env.WHIP_PERMISSION_RESULTS ?? '/tmp/whip-permission-request-results';
await mkdir(results, { recursive: true });
process.chdir(web);
const config = {
  configFile: false, root: resolve(web, 'scripts/fixtures/permission-requests'), logLevel: 'warn',
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
    const browser = await browserType.launch();
    try {
      const page = await browser.newPage();
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.addInitScript(() => {
        window.cspErrors = [];
        document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective));
      });
      for (const scenario of [
        { name: 'desktop', width: 1440, height: 900 },
        { name: 'phone', width: 390, height: 844 },
        { name: 'narrow-long', width: 320, height: 640, long: true },
        { name: 'short', width: 640, height: 320, long: true },
      ]) {
        for (const theme of ['light', 'dark', 'claude-code']) {
          await page.setViewportSize({ width: scenario.width, height: scenario.height });
          await page.goto(`${origin}/?theme=${theme}${scenario.long ? '&long=1' : ''}`);
          const card = page.getByRole('region', { name: 'Your approval is needed' });
          await expect(card).toHaveCount(1);
          await page.evaluate(() => document.fonts.ready);
          await expect(card.getByRole('status')).toHaveText('1 more waiting');
          assert.ok((await card.getByText('bash', { exact: true }).boundingBox()).height < 24, 'Long agent names must not squeeze the tool name into a vertical column');
          await page.getByRole('textbox', { name: 'Message WHIP' }).waitFor();
          const geometry = await card.evaluate(element => {
            const card = element.getBoundingClientRect();
            const dock = element.closest('[aria-label="Needs your attention"]').getBoundingClientRect();
            const composer = document.querySelector('[data-whip-composer]').parentElement.getBoundingClientRect();
            return { left: card.left, right: card.right, width: card.width, bottom: Math.min(card.bottom, dock.bottom),
              composerLeft: composer.left, composerRight: composer.right, composerTop: composer.top,
              documentWidth: document.documentElement.scrollWidth, viewportWidth: innerWidth };
          });
          assert.ok(geometry.left >= 12 && geometry.right <= scenario.width - 12);
          assert.ok(geometry.width <= 816 && Math.abs(geometry.left - geometry.composerLeft) < 1 && Math.abs(geometry.right - geometry.composerRight) < 1);
          assert.ok(geometry.bottom <= geometry.composerTop && geometry.composerTop - geometry.bottom <= 9, `${engine}/${scenario.name}/${theme}: ${JSON.stringify(geometry)}`);
          assert.ok(geometry.documentWidth <= geometry.viewportWidth);
          const allow = card.getByRole('button', { name: 'Allow once', exact: true });
          if (scenario.width <= 767) {
            const box = await allow.boundingBox();
            // Firefox reports fractional viewport coordinates with float32 rounding.
            assert.ok(box.height >= 44 - 0.01, `${engine}/${scenario.name}/${theme}: touch target ${JSON.stringify(box)}`);
          }
          await allow.scrollIntoViewIfNeeded();
          await expect(allow).toBeInViewport();
          await page.screenshot({ path: resolve(results, `${engine}-${scenario.name}-${theme}.png`), fullPage: true });
          if (engine === 'chromium' && scenario.name === 'desktop' && theme === 'dark') {
            const box = await card.boundingBox();
            const composer = await page.locator('form').boundingBox();
            await page.screenshot({ path: resolve(results, 'preview.png'), clip: {
              x: box.x - 12, y: box.y - 12, width: box.width + 24, height: composer.y + composer.height - box.y + 12,
            } });
          }
          // Scope selection must stay reachable at narrow widths and reset for the next agent.
          await card.getByRole('combobox', { name: 'Permission scope' }).click();
          await page.getByRole('option', { name: scenario.long ? 'Remember for this session and children' : 'Remember on this host' }).click();
          await expect(card.getByRole('button', { name: 'Allow and remember' })).toBeVisible();
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
          await card.getByRole('button', { name: 'Deny', exact: true }).focus();
          await page.keyboard.press('Enter');
          await expect(card).toHaveCount(1);
          await expect(card).toContainText('Root agent');
          await expect(card).toContainText('/project/README.md');
          await expect(card.getByRole('combobox', { name: 'Permission scope' })).toContainText('This request only');
          await card.getByRole('button', { name: 'Allow once', exact: true }).click();
          await expect(card).toHaveCount(0);
          assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
          report.push(`${engine}: ${scenario.name}, ${theme}: composer alignment, bounded command, scope, keyboard, queue, CSP`);
        }
      }
      assert.deepEqual(errors, []);
    } finally { await browser.close(); }
  }
  await writeFile(resolve(results, 'report.json'), JSON.stringify(report, null, 2) + '\n');
  console.log(`Passed ${report.length} permission layouts. Artifacts: ${results}`);
} finally { await new Promise(resolve => server.httpServer.close(resolve)); }
