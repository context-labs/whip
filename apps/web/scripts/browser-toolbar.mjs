import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, preview } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, firefox, expect } from '@playwright/test';

const web = fileURLToPath(new URL('../', import.meta.url));
const results = process.env.WHIP_BROWSER_TOOLBAR_RESULTS ?? '/tmp/whip-browser-toolbar-results';
await mkdir(results, { recursive: true });
process.chdir(web);
const config = {
  configFile: false, root: resolve(web, 'scripts/fixtures/browser-toolbar'), logLevel: 'warn',
  plugins: [stylex.vite({ useCSSLayers: { before: ['whip-reset'] }, runtimeInjection: false,
    unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(web, '../..') } }), react()],
  build: { outDir: resolve(results, 'dist'), emptyOutDir: true },
  preview: { host: '127.0.0.1', port: 0 },
};
await build(config);
const server = await preview(config);
const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
try {
  for (const [engine, browserType] of Object.entries({ chromium, firefox })) {
    const browser = await browserType.launch();
    try {
      const page = await browser.newPage();
      const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      for (const width of [1280, 480, 320]) {
        for (const theme of ['light', 'dark']) {
          // Narrow desktop split panes retain the window's pointer/viewport metrics.
          await page.setViewportSize({ width: 1280, height: 720 });
          await page.goto(`${origin}/?theme=${theme}`);
          await page.locator('main').evaluate((node, width) => { node.style.width = `${width}px`; }, width);
          const navigation = page.getByRole('group', { name: 'Browser toolbar' });
          const address = navigation.getByRole('textbox', { name: 'Browser address' });
          await expect(address).toHaveValue('http://localhost:3000/');
          await page.evaluate(() => document.fonts.ready);
          const bounds = await navigation.boundingBox(), input = await address.boundingBox();
          assert.equal(bounds.x, 0);
          assert.equal(bounds.width, width, 'Divider must span the entire browser pane');
          assert.ok(input.width >= 50, 'Address must remain usable in narrow panes');
          const topBar = page.getByRole('banner', { name: 'Session information' });
          const switcher = topBar.getByRole('group', { name: 'Session view' });
          for (const name of ['Chat', 'REPL', 'Trace', 'Chat']) {
            await switcher.getByRole('button', { name, exact: true }).click();
            await expect(switcher.getByRole('button', { pressed: true })).toHaveCount(1);
            await expect(switcher.getByRole('button', { name, exact: true })).toHaveAttribute('aria-pressed', 'true');
          }
          if (width > 600) {
            await topBar.getByRole('button', { name: 'Session details', exact: true }).click();
            await expect(topBar.getByRole('button', { name: 'Hide session details' })).toHaveAttribute('aria-expanded', 'true');
            await topBar.getByRole('button', { name: 'Hide session details' }).click();
          }
          const session = await topBar.boundingBox();
          const buttons = await switcher.getByRole('button').all();
          for (const button of buttons) {
            const box = await button.boundingBox();
            assert.ok(box.x >= session.x && box.x + box.width <= session.x + session.width, 'View controls must fit narrow panes');
            assert.ok(box.y >= session.y && box.y + box.height <= session.y + session.height);
          }
          const menu = await topBar.getByRole('button', { name: 'Session actions' }).boundingBox();
          assert.ok(menu.x + menu.width <= session.x + session.width, 'Actions must not overflow');
          const sessionAction = await page.getByRole('button', { name: 'Session actions', exact: true }).boundingBox();
          assert.equal(bounds.height, session.height, 'Browser and session headers must have the same height');
          assert.equal(bounds.height, 37, 'Desktop toolbar must remain a compact single row, including its divider');
          assert.equal(input.height, sessionAction.height, 'Address and header actions must share the same control height');
          for (const button of await navigation.getByRole('button').all()) {
            const box = await button.boundingBox();
            assert.equal(box.height, sessionAction.height);
            assert.equal(box.width, sessionAction.width);
            assert.equal(box.y + box.height / 2, input.y + input.height / 2, 'Controls must be vertically centered together');
          }
          for (const icon of await navigation.locator('svg').all()) {
            const box = await icon.boundingBox();
            assert.equal(box.width, 16, 'Toolbar icons must match the session header');
            assert.equal(box.height, 16);
          }
          const border = await navigation.evaluate(node => {
            const css = getComputedStyle(node);
            return { width: css.borderBottomWidth, style: css.borderBottomStyle, color: css.borderBottomColor };
          });
          assert.equal(border.width, '1px');
          assert.equal(border.style, 'solid');
          assert.notEqual(border.color, 'rgba(0, 0, 0, 0)');
          const inputBorders = await address.evaluate(node => {
            const css = getComputedStyle(node);
            return ['Top', 'Right', 'Bottom', 'Left'].map(side => ({
              width: css[`border${side}Width`], style: css[`border${side}Style`], color: css[`border${side}Color`],
            }));
          });
          for (const border of inputBorders) {
            assert.equal(border.width, '1px', 'Address input must retain its border');
            assert.equal(border.style, 'solid');
            assert.notEqual(border.color, 'rgba(0, 0, 0, 0)');
          }
          const provenance = await navigation.getByRole('img', { name: 'This Mac', exact: true }).boundingBox();
          assert.ok(provenance.x >= input.x + input.width, 'Environment indicator must follow the address input');
          assert.ok(provenance.y >= bounds.y && provenance.y + provenance.height <= bounds.y + bounds.height);
          let right = provenance.x + provenance.width;
          for (const name of ['Open SSH preview…', 'Conversation access…', 'Browser actions']) {
            const button = navigation.getByRole('button', { name, exact: true });
            await expect(button.locator('svg')).toHaveCount(1);
            assert.equal(await button.textContent(), '', 'Toolbar actions must be icon-only');
            assert.equal(await button.evaluate(node => node.closest('form')), null, 'Dialog forms must not bubble submits into address navigation');
            const box = await button.boundingBox();
            assert.ok(box.x >= right && box.y >= bounds.y && box.y + box.height <= bounds.y + bounds.height);
            right = box.x + box.width;
          }
          assert.ok(right <= width);
          assert.equal(await page.evaluate(() => document.documentElement.scrollWidth), 1280);
          await page.screenshot({ path: resolve(results, `${engine}-${theme}-${width}.png`) });
        }
      }
      await page.setViewportSize({ width: 800, height: 600 });
      await page.goto(origin);
      const address = page.getByRole('textbox', { name: 'Browser address' });
      await address.fill('example.com');
      await address.press('Enter');
      await expect.poll(() => page.evaluate(() => window.browserActions)).toContainEqual({ kind: 'navigate', url: 'https://example.com/' });
      for (const [button, dialog] of [['Open SSH preview…', 'Open SSH preview'], ['Conversation access…', 'Conversation Browser access']]) {
        await page.getByRole('button', { name: button, exact: true }).click();
        await expect(page.getByRole('dialog', { name: dialog, exact: true })).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(page.getByRole('dialog')).toHaveCount(0);
      }
      await page.getByRole('button', { name: 'Browser actions', exact: true }).click();
      await page.getByRole('menuitem', { name: 'Find in page', exact: true }).click();
      await expect(page.getByRole('textbox', { name: 'Find text' })).toBeFocused();
      await page.getByRole('button', { name: 'Close find', exact: true }).click();
      await page.goto(`${origin}/?loading&ssh`);
      await expect(page.getByRole('img', { name: 'SSH preview', exact: true })).toHaveCount(1);
      await page.getByRole('button', { name: 'Stop loading', exact: true }).click();
      await expect.poll(() => page.evaluate(() => window.browserActions)).toContainEqual({ kind: 'stop' });
      await page.goto(`${origin}/?unavailable`);
      await expect(page.getByRole('button', { name: 'Open SSH preview…', exact: true })).toBeDisabled();
      await expect(page.getByRole('button', { name: 'Conversation access…', exact: true })).toBeDisabled();
      const touch = await browser.newContext({ hasTouch: true, viewport: { width: 1280, height: 720 } });
      try {
        const page = await touch.newPage();
        await page.goto(origin);
        const toolbar = page.getByRole('group', { name: 'Browser toolbar' });
        const bounds = await toolbar.boundingBox();
        const session = await page.getByRole('banner', { name: 'Session information' }).boundingBox();
        assert.equal(bounds.height, session.height, 'Touch headers must stay aligned too');
        const sessionViews = page.getByRole('group', { name: 'Session view' });
        for (const button of [...await toolbar.getByRole('button').all(), ...await sessionViews.getByRole('button').all()]) {
          const box = await button.boundingBox();
          assert.ok(box.width >= 44 && box.height >= 44, 'Touch actions must retain 44px targets');
        }
      } finally { await touch.close(); }
      assert.deepEqual(errors, []);
      console.log(`${engine}: light/dark, 1280/480/320px, matching session-header height/icons, touch targets, edge-to-edge divider, navigation, dialogs, find and disabled/loading states passed`);
    } finally { await browser.close(); }
  }
} finally { await new Promise(resolve => server.httpServer.close(resolve)); }
