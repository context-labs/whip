import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, mkdir, writeFile } from 'node:fs/promises';
import { resolve, extname, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import { build } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, firefox, expect } from '@playwright/test';

const require = createRequire(import.meta.url);
const packageRoot = fileURLToPath(new URL('../', import.meta.url));
const output = resolve(packageRoot, 'ui-test-results/workspace-layout');
await mkdir(output, { recursive: true });
await build({ configFile: false, root: resolve(packageRoot, 'tests/fixtures/workspace-layout'), plugins: [stylex.vite({ useCSSLayers: {before: ['whip-reset']}, runtimeInjection: false, unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(packageRoot, '../..') } }), react()], logLevel: 'warn', build: { outDir: output, emptyOutDir: true, assetsInlineLimit: 0 } });
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'";
const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2' };
const server = createServer(async (req, res) => {
  const pathname = new URL(req.url, 'http://localhost').pathname;
  if (pathname === '/axe.js') { res.setHeader('Content-Type', 'text/javascript'); res.end(await readFile(require.resolve('axe-core/axe.min.js'))); return; }
  const path = resolve(output, `.${pathname === '/' ? '/index.html' : pathname}`);
  if (!path.startsWith(output + sep)) { res.writeHead(403).end(); return; }
  try { res.setHeader('Content-Security-Policy', csp); res.setHeader('Content-Type', mime[extname(path)] ?? 'application/octet-stream'); res.end(await readFile(path)); } catch { res.writeHead(404).end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
try {
  for (const [name, engine] of Object.entries({ chromium, firefox })) {
    const browser = await engine.launch();
    const errors = [];
    try {
      const page = await browser.newPage({ viewport: { width: 1400, height: 1000 } });
      page.setDefaultTimeout(10000);
      page.on('pageerror', error => errors.push(error.message));
      await page.addInitScript(() => {
        window.__cspErrors = [];
        document.addEventListener('securitypolicyviolation', event => window.__cspErrors.push(`${event.violatedDirective}: ${event.blockedURI}`));
      });
      const visit = async (theme = 'dark') => { await page.goto(`${origin}/?theme=${theme}`); await expect(page.getByRole('tabpanel')).toHaveCount(3); await expect(page.getByLabel('Compact layout')).toHaveText('false'); };
      const tab = value => page.locator(`[role="tab"][id="whip-workspace-tab-${value}"]`);
      const drag = async (value, point) => {
        const from = await tab(value).boundingBox();
        await page.mouse.move(from.x + 40, from.y + from.height / 2); await page.mouse.down();
        await page.mouse.move(from.x + 48, from.y + from.height / 2);
        await expect(page.locator(`[data-workspace-tab="${value}"][data-dragging]`)).toBeVisible();
        await page.mouse.move(point.x, point.y, { steps: 20 });
        const preview = page.locator('[data-workspace-drag-preview]');
        await expect(preview).toHaveCount(1);
        await expect.poll(async () => Math.abs((await preview.boundingBox()).x + 40 - point.x)).toBeLessThan(2);
        assert.equal(await preview.locator('button,a,[role=tab],[id]').count(), 0, 'Drag preview duplicated live controls or IDs');
      };
      await visit();
      await page.getByRole('textbox', { name: 'Workspace notes' }).fill('An independent renderer with its own state.');
      const draft = page.getByRole('textbox', { name: 'Draft alpha' });
      await draft.fill('Keep this exact draft');
      await page.evaluate(() => {
        window.__draft = document.querySelector('[aria-label="Draft alpha"]');
        window.__scroll = document.querySelector('[data-view-scroll="alpha"]');
        window.__scroll.scrollTop = 160;
      });
      await draft.focus();
      await page.getByRole('button', { name: 'Move alpha right' }).evaluate(button => button.click());
      await expect(page.locator('[data-workspace-view="alpha"]')).toHaveAttribute('data-workspace-pane', 'three');
      await expect(draft).toHaveValue('Keep this exact draft');
      assert(await page.evaluate(() => window.__draft === document.querySelector('[aria-label="Draft alpha"]')), 'Moving a selected view replaced its DOM');
      assert(await page.evaluate(() => window.__scroll === document.querySelector('[data-view-scroll="alpha"]') && window.__scroll.scrollTop === 160), 'Moving a selected view lost its scroll element/position');
      await expect(draft).toBeFocused();
      // A selected view remains draggable after a programmatic transfer without reloading.
      const firstTarget = await tab('beta').boundingBox();
      await drag('alpha', { x: firstTarget.x + 30, y: firstTarget.y + 12 });
      await page.keyboard.press('Escape'); await page.mouse.up();
      await expect(page.locator('[data-dragging]')).toHaveCount(0);
      await expect(page.locator('[data-workspace-view="alpha"]')).toHaveAttribute('data-workspace-pane', 'three');
      await tab('delta').click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Move to right pane' }).click();
      await expect(page.getByRole('menu')).toHaveCount(0);
      await expect(page.locator('[data-workspace-frame]')).toHaveCount(2);
      // This transfer prunes the source pane and remounts the surviving left header.
      for (let attempt = 0; attempt < 3; attempt++) {
        const target = await tab('beta').boundingBox();
        await drag('delta', { x: target.x + 30, y: target.y + 12 });
        await expect(page.locator('[data-workspace-drop]')).toHaveCount(0);
        await page.keyboard.press('Escape'); await page.mouse.up();
      await expect(page.locator('[data-dragging]')).toHaveCount(0);
      }
      // Pointer transfer uses one shared drag context and retains the selected view.
      await visit();
      await page.evaluate(() => { window.__draft = document.querySelector('[aria-label="Draft alpha"]'); });
      const target = await tab('gamma').boundingBox();
      await drag('alpha', { x: target.x + target.width - 10, y: target.y + target.height / 2 });
      await expect(page.locator('[data-workspace-drop]')).toHaveCount(0);
      await page.mouse.up();
      await expect(page.locator('[data-workspace-view="alpha"]')).toHaveAttribute('data-workspace-pane', 'three');
      assert(await page.evaluate(() => window.__draft === document.querySelector('[aria-label="Draft alpha"]')), 'Dragging a selected view remounted it');
      await expect(page.locator('[data-workspace-tab-group="three"]')).toHaveCount(2);
      // Escape discards a proposed transfer; nothing is committed while dragging.
      const lower = await tab('delta').boundingBox();
      const savedDrop = await page.getByLabel('Last drop').textContent();
      await drag('alpha', { x: lower.x + 60, y: lower.y + 12 });
      await expect(page.locator('[data-workspace-drop]')).toHaveCount(0);
      await page.keyboard.press('Escape'); await page.mouse.up();
      await expect(page.locator('[data-dragging]')).toHaveCount(0);
      await expect(page.locator('[data-workspace-drop]')).toHaveCount(0);
      await expect(page.locator('[data-workspace-drag-preview]')).toHaveCount(0);
      await expect(page.locator('[data-tab-settling]')).toHaveCount(0);
      await expect(page.getByLabel('Last drop')).toHaveText(savedDrop);
      await expect(page.locator('[data-workspace-view="alpha"]')).toHaveAttribute('data-workspace-pane', 'three');
      // Nested separators expose values, keyboard resizing, and explicit commit callbacks.
      const separator = page.getByRole('separator', { name: 'Resize panes horizontally' });
      const oldWidth = (await page.locator('[data-workspace-view="alpha"]').boundingBox()).width;
      await separator.focus(); await separator.press('ArrowLeft');
      await expect(page.getByLabel('Last resize')).toContainText('outer:');
      await expect(separator).toHaveAttribute('aria-valuenow', /\d/);
      assert((await page.locator('[data-workspace-view="alpha"]').boundingBox()).width > oldWidth, 'Keyboard separator did not resize the panes');
      const separatorBounds = await separator.boundingBox();
      const beforeHorizontalDrag = (await page.locator('[data-workspace-view="alpha"]').boundingBox()).width;
      await page.mouse.move(separatorBounds.x + 2, separatorBounds.y + 100); await page.mouse.down();
      await page.mouse.move(separatorBounds.x - 70, separatorBounds.y + 100, { steps: 10 }); await page.mouse.up();
      await expect.poll(async () => (await page.locator('[data-workspace-view="alpha"]').boundingBox()).width).toBeGreaterThan(beforeHorizontalDrag + 40);
      const vertical = page.getByRole('separator', { name: 'Resize panes vertically' });
      const lowerPane = page.locator('[data-workspace-frame="two"]');
      const oldHeight = (await lowerPane.boundingBox()).height;
      await vertical.focus(); await vertical.press('ArrowUp');
      await expect(page.getByLabel('Last resize')).toContainText('inner:');
      await expect.poll(async () => (await lowerPane.boundingBox()).height).toBeGreaterThan(oldHeight);
      const verticalBounds = await vertical.boundingBox();
      const beforeDrag = (await lowerPane.boundingBox()).height;
      // The resize hit target must extend into the adjacent rendered view.
      await page.mouse.move(verticalBounds.x + 100, verticalBounds.y - 2); await page.mouse.down();
      await page.mouse.move(verticalBounds.x + 100, verticalBounds.y - 70, { steps: 10 }); await page.mouse.up();
      await expect.poll(async () => (await lowerPane.boundingBox()).height).toBeGreaterThan(beforeDrag + 40);
      // Vertical edge split is possible in the full-height right pane.
      await visit();
      const right = await page.locator('[data-workspace-view="gamma"]').boundingBox();
      await drag('beta', { x: right.x + right.width / 2, y: right.y + right.height - 30 });
      await expect(page.locator('[data-workspace-drop="bottom"]')).toBeVisible();
      await page.mouse.up();
      await expect(page.getByRole('tabpanel')).toHaveCount(4);
      await expect(page.getByLabel('Last drop')).toContainText('"edge":"bottom"');
      await page.setViewportSize({ width: 700, height: 900 });
      await expect(page.getByLabel('Compact layout')).toHaveText('true');
      await expect(page.getByRole('tabpanel')).toHaveCount(1);
      await expect(page.getByRole('separator')).toHaveCount(0);
      await page.setViewportSize({ width: 1400, height: 1000 });
      await expect(page.getByLabel('Compact layout')).toHaveText('false');
      await expect(page.getByRole('tabpanel')).toHaveCount(4);
      await expect(page.getByRole('separator')).toHaveCount(3);
      await page.screenshot({ path: resolve(output, `${name}-nested.png`) });
      await page.addScriptTag({ url: `${origin}/axe.js` });
      const violations = await page.evaluate(async () => (await axe.run(document.getElementById('root'), { runOnly: { type: 'rule', values: ['button-name', 'aria-required-children', 'aria-required-parent', 'aria-valid-attr-value', 'nested-interactive'] } })).violations);
      assert.deepEqual(violations, [], `${name}: accessibility`);
      assert.deepEqual(await page.evaluate(() => window.__cspErrors), [], `${name}: CSP violations`);
      assert.equal(await page.locator('style').count(), 0, `${name}: runtime style injection`);
      // disableCursor leaves one empty library sheet; it must never inject any CSS rules.
      assert.equal(await page.evaluate(() => document.adoptedStyleSheets.reduce((count, sheet) => count + sheet.cssRules.length, 0)), 0, `${name}: runtime adopted CSS rules`);
      assert.deepEqual(errors, [], `${name}: browser errors`);
      await visit('light');
      const notes = page.getByRole('textbox', { name: 'Workspace notes' });
      await notes.fill('A second renderer keeps its editable state across docking.');
      await notes.evaluate(element => { window.__notes = element; });
      const notesTarget = await tab('alpha').boundingBox();
      await drag('gamma', { x: notesTarget.x + 30, y: notesTarget.y + 12 });
      await page.mouse.up();
      await expect(page.locator('[data-workspace-view="gamma"]')).toHaveAttribute('data-workspace-pane', 'one');
      await expect(notes).toHaveValue('A second renderer keeps its editable state across docking.');
      assert(await notes.evaluate(element => element === window.__notes), 'The notes renderer remounted during docking');
      await page.screenshot({ path: resolve(output, `${name}-light.png`) });
      console.log(`${name}: nested layout, stable view movement, shared drag/cancel, keyboard/pointer resize, compact restoration, Axe and strict CSP passed`);
    } finally { await browser.close(); }
  }
} finally { await new Promise(resolve => server.close(resolve)); }
