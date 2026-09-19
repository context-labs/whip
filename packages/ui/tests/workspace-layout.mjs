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
      // A sidebar source is not a sortable tab: it opens a new view without moving
      // the source or disturbing the selected view in the other pane.
      const externalVisit = async (options = '') => {
        await page.goto(`${origin}/?external&${options}`);
        await expect(page.getByRole('link', { name: 'Saved alpha', exact: true })).toBeVisible();
        await expect(page.locator('[data-workspace-tab-strip]')).toHaveCount(options.includes('standalone') || options.includes('compact') ? 1 : 2);
      };
      const strip = id => page.locator(`[data-workspace-tab-strip="${id}"]`);
      const order = id => strip(id).locator('[data-workspace-tab]').evaluateAll(nodes => nodes.map(node => node.dataset.workspaceTab));
      const externalDrops = () => page.getByLabel('External drops');
      const preview = page.locator('[data-workspace-drag-preview]');
      const pointIn = async (locator, fraction = .5) => {
        const box = await locator.boundingBox();
        assert(box, 'Drop target has no geometry');
        return { x: box.x + box.width * fraction, y: box.y + box.height / 2 };
      };
      const externalDrag = async point => {
        const before = await externalDrops().textContent();
        const source = page.getByRole('link', { name: 'Saved alpha', exact: true });
        const from = await pointIn(source);
        await page.mouse.move(from.x, from.y); await page.mouse.down();
        await page.mouse.move(from.x + 9, from.y);
        await expect(preview).toHaveCount(1);
        await page.mouse.move(point.x, point.y, { steps: 20 });
        await expect(preview).toBeVisible();
        assert.equal(await preview.locator('button,a,[role=tab],[id]').count(), 0, 'External preview duplicated live controls');
        await expect(source).toBeVisible();
        await expect(externalDrops()).toHaveText(before);
        await expect(page.getByLabel('Source clicks')).toHaveText('');
      };
      const cleanExternal = async () => {
        await expect(preview).toHaveCount(0);
        await expect(page.locator('[data-workspace-drop]')).toHaveCount(0);
        assert.deepEqual(await page.evaluate(() => window.__cspErrors), [], `${name}: external drag CSP violations`);
        assert.equal(await page.locator('style').count(), 0, `${name}: external runtime style injection`);
      };
      await externalVisit();
      await page.getByRole('textbox', { name: 'Draft alpha' }).fill('Original view survives');
      await page.evaluate(() => {
        window.__externalSource = document.querySelector('a[href="#saved-alpha"]');
        window.__externalDraft = document.querySelector('textarea[aria-label="Draft alpha"]');
        window.__externalScroll = document.querySelector('[data-view-scroll="alpha"]');
        window.__externalScroll.scrollTop = 160;
      });
      await externalDrag(await pointIn(tab('delta'), .15));
      await page.mouse.up();
      await expect.poll(() => order('two')).toEqual(['gamma', 'external-view-1', 'delta']);
      await expect(tab('external-view-1')).toHaveAttribute('aria-selected', 'true');
      await expect(tab('alpha')).toHaveAttribute('aria-selected', 'true');
      await expect.poll(() => order('one')).toEqual(['alpha', 'beta']);
      await expect(page.getByRole('textbox', { name: 'Draft alpha' })).toHaveValue('Original view survives');
      assert(await page.evaluate(() => window.__externalSource === document.querySelector('a[href="#saved-alpha"]') && window.__externalDraft === document.querySelector('textarea[aria-label="Draft alpha"]') && window.__externalScroll === document.querySelector('[data-view-scroll="alpha"]') && window.__externalScroll.scrollTop === 160), 'External drop replaced source or other pane view');
      assert.equal(new URL(page.url()).hash, '', 'Drag navigated the sidebar link');
      await cleanExternal();
      // Every subsequent drop also creates a fresh ID, even for the same payload.
      await externalDrag(await pointIn(tab('gamma'), .1));
      await page.mouse.up();
      await expect.poll(() => order('two')).toEqual(['external-view-2', 'gamma', 'external-view-1', 'delta']);
      await cleanExternal();

      // Empty usable strip space accepts append; utilities and content centers don't.
      for (const target of ['empty', 'append', 'utility', 'content', 'outside']) {
        await externalVisit(target === 'empty' ? 'empty' : '');
        const locator = target === 'utility' ? page.getByRole('button', { name: 'Utility two' })
          : target === 'content' ? page.locator('[data-workspace-slot="two"]')
          : target === 'outside' ? page.getByRole('button', { name: 'Session action' }) : strip('two');
        await externalDrag(await pointIn(locator, target === 'append' ? .98 : .5));
        await page.mouse.up();
        if (target === 'empty' || target === 'append') {
          await expect.poll(() => order('two')).toEqual(target === 'empty' ? ['external-view-1'] : ['gamma', 'delta', 'external-view-1']);
        } else await expect(externalDrops()).toHaveText('[]');
        await cleanExternal();
      }
      const edgePoint = async (paneId, edge) => {
        const box = await page.locator(`[data-workspace-slot="${paneId}"]`).boundingBox();
        assert(box);
        return { x: box.x + box.width * (edge === 'left' ? .01 : edge === 'right' ? .99 : .5),
          y: box.y + box.height * (edge === 'top' ? .01 : edge === 'bottom' ? .99 : .5) };
      };
      await page.setViewportSize({ width: 1800, height: 1000 });
      for (const edge of ['left', 'right', 'top', 'bottom']) {
        await externalVisit();
        await page.getByRole('textbox', { name: 'Draft gamma' }).fill('Keep the target draft and reading position');
        await page.evaluate(() => {
          window.__edgeDraft = document.querySelector('textarea[aria-label="Draft gamma"]');
          window.__edgeScroll = document.querySelector('[data-view-scroll="gamma"]');
          window.__edgeScroll.scrollTop = 160;
        });
        await externalDrag(await edgePoint('two', edge));
        await expect(page.locator(`[data-workspace-drop="${edge}"]`)).toBeVisible();
        await expect(page.locator('[data-workspace-tab-strip]')).toHaveCount(2);
        await page.mouse.up();
        await expect(page.locator('[data-workspace-tab-strip]')).toHaveCount(3);
        await expect.poll(() => order('two')).toEqual(['gamma', 'delta']);
        await expect.poll(() => order('one')).toEqual(['alpha', 'beta']);
        await expect.poll(() => order('external-pane-1')).toEqual(['external-view-1']);
        await expect(tab('external-view-1')).toHaveAttribute('aria-selected', 'true');
        const oldBox = await page.locator('[data-workspace-frame="two"]').boundingBox();
        const newBox = await page.locator('[data-workspace-frame="external-pane-1"]').boundingBox();
        assert(edge === 'left' ? newBox.x + newBox.width <= oldBox.x + 1
          : edge === 'right' ? oldBox.x + oldBox.width <= newBox.x + 1
          : edge === 'top' ? newBox.y + newBox.height <= oldBox.y + 1
          : oldBox.y + oldBox.height <= newBox.y + 1, `${edge}: wrong split placement`);
        assert.equal(await page.evaluate(() => window.__edgeDraft === document.querySelector('textarea[aria-label="Draft gamma"]')
          && window.__edgeScroll === document.querySelector('[data-view-scroll="gamma"]')
          && window.__edgeScroll.scrollTop === 160), true, `${edge}: original view remounted or lost reading position`);
        await expect(page.getByRole('textbox', { name: 'Draft gamma' })).toHaveValue('Keep the target draft and reading position');
        await cleanExternal();
      }
      // Two accepted edges reach the pane cap; a fifth pane must not be offered.
      await externalVisit();
      for (const paneId of ['one', 'two']) {
        await externalDrag(await edgePoint(paneId, 'top'));
        await page.mouse.up();
        await cleanExternal();
      }
      await expect(page.locator('[data-workspace-tab-strip]')).toHaveCount(4);
      const cappedDrops = await externalDrops().textContent();
      await externalDrag(await edgePoint('two', 'left'));
      await expect(page.locator('[data-workspace-drop]')).toHaveCount(0);
      await page.mouse.up();
      await expect(externalDrops()).toHaveText(cappedDrops);
      await cleanExternal();
      // Compact presentation and insufficient dimensions reject edges without mutation.
      for (const test of [{ options: 'compact', width: 1800, height: 1000, pane: 'one', edge: 'left' },
        { options: '', width: 1400, height: 1000, pane: 'two', edge: 'left' },
        { options: '', width: 1800, height: 500, pane: 'two', edge: 'top' }]) {
        await page.setViewportSize({ width: test.width, height: test.height });
        await externalVisit(test.options);
        await externalDrag(await edgePoint(test.pane, test.edge));
        await expect(page.locator('[data-workspace-drop]')).toHaveCount(0);
        await page.mouse.up();
        await expect(externalDrops()).toHaveText('[]');
        await cleanExternal();
      }
      await page.setViewportSize({ width: 1800, height: 1000 });
      for (const target of ['tab', 'edge']) for (const cancellation of ['escape', 'blur', 'pointercancel', 'remove', 'reject']) {
        await externalVisit();
        await externalDrag(target === 'edge' ? await edgePoint('two', 'top') : await pointIn(tab('gamma'), .1));
        if (target === 'edge') await expect(page.locator('[data-workspace-drop="top"]')).toBeVisible();
        if (cancellation === 'escape') await page.keyboard.press('Escape');
        if (cancellation === 'blur') await page.evaluate(() => window.dispatchEvent(new Event('blur')));
        if (cancellation === 'pointercancel') await page.evaluate(() => document.dispatchEvent(new PointerEvent('pointercancel', { bubbles: true })));
        if (cancellation === 'remove') await page.getByRole('button', { name: 'Remove source' }).evaluate(button => button.click());
        if (cancellation === 'reject') {
          await page.getByRole('button', { name: 'Reject external drops' }).evaluate(button => button.click());
          await expect(page.getByRole('button', { name: 'Reject external drops' })).toBeDisabled();
        } else await expect(preview, `${name}: ${cancellation} removes the drag before release`).toHaveCount(0);
        await page.mouse.up();
        await expect(externalDrops(), `${name}: ${cancellation} should cancel without creation`).toHaveText('[]');
        await expect.poll(() => order('two')).toEqual(['gamma', 'delta']);
        await expect(page.getByLabel('Source clicks')).toHaveText('');
        await cleanExternal();
      }
      // Release in the same task as DOM removal, before the RAF cleanup sees it.
      await externalVisit();
      await page.evaluate(() => document.addEventListener('pointerdown', event => { window.__sourcePointer = event.pointerId; }, { once: true }));
      const removedTarget = await pointIn(tab('gamma'), .1);
      await externalDrag(removedTarget);
      await page.evaluate(point => {
        document.querySelector('a[href="#saved-alpha"]').remove();
        document.dispatchEvent(new PointerEvent('pointerup', { bubbles: true, pointerId: window.__sourcePointer, pointerType: 'mouse', button: 0, buttons: 0, clientX: point.x, clientY: point.y }));
      }, removedTarget);
      await page.mouse.up();
      await expect(externalDrops()).toHaveText('[]');
      await expect.poll(() => order('two')).toEqual(['gamma', 'delta']);
      await cleanExternal();
      // Normal/modified links and sibling actions keep their native semantics.
      await externalVisit();
      const source = page.getByRole('link', { name: 'Saved alpha', exact: true });
      await source.click();
      await expect(page.getByLabel('Source clicks')).toHaveText('plain');
      assert.equal(new URL(page.url()).hash, '#saved-alpha');
      for (const modifier of ['Control', 'Meta', 'Shift', 'Alt']) {
        await externalVisit();
        const from = await pointIn(source), to = await pointIn(tab('gamma'));
        await page.keyboard.down(modifier);
        await page.mouse.move(from.x, from.y); await page.mouse.down(); await page.mouse.move(to.x, to.y, { steps: 20 });
        await expect(preview).toHaveCount(0);
        await page.mouse.up(); await page.keyboard.up(modifier);
        await expect(externalDrops()).toHaveText('[]');
      }
      await source.focus(); await page.keyboard.press('Enter');
      await expect(page.getByLabel('Source clicks')).toHaveText('plain');
      await page.getByRole('button', { name: 'Session action' }).click();
      await expect(page.getByLabel('Source clicks')).toHaveText('action');
      const disabled = await pointIn(page.getByRole('link', { name: 'Disabled source' }));
      await page.mouse.move(disabled.x, disabled.y); await page.mouse.down();
      const disabledTarget = await pointIn(tab('gamma'));
      await page.mouse.move(disabledTarget.x, disabledTarget.y, { steps: 20 });
      await expect(preview).toHaveCount(0); await page.mouse.up();
      await expect(externalDrops()).toHaveText('[]');

      for (const options of ['zoom', 'rtl', 'zoom&rtl', 'reduced']) {
        await page.emulateMedia({ reducedMotion: options === 'reduced' ? 'reduce' : 'no-preference' });
        await externalVisit(options);
        await externalDrag(await pointIn(tab('gamma'), options.includes('rtl') ? .9 : .1));
        await page.mouse.up();
        await expect.poll(() => order('two')).toEqual(['external-view-1', 'gamma', 'delta']);
        await cleanExternal();
      }
      await page.emulateMedia({ reducedMotion: 'no-preference' });
      await externalVisit('standalone');
      await expect(page.locator('[data-workspace-layout]')).toHaveCount(0);
      await externalDrag(await pointIn(tab('gamma'), .1));
      await page.mouse.up();
      await expect.poll(() => order('two')).toEqual(['external-view-1', 'gamma', 'delta']);
      await cleanExternal();
      await drag('delta', await pointIn(tab('external-view-1'), .1));
      await page.mouse.up();
      await expect.poll(() => order('two')).toEqual(['delta', 'external-view-1', 'gamma']);
      await cleanExternal();
      await externalVisit('overflow');
      await externalDrag(await pointIn(strip('two'), .98));
      await expect.poll(() => strip('two').evaluate(element => element.scrollLeft)).toBeGreaterThan(100);
      await page.mouse.up();
      await expect(externalDrops()).not.toHaveText('[]');
      await cleanExternal();
      await page.addScriptTag({ url: `${origin}/axe.js` });
      const externalViolations = await page.evaluate(async () => (await axe.run(document.getElementById('root'), { runOnly: { type: 'rule', values: ['button-name', 'aria-required-children', 'aria-required-parent', 'aria-valid-attr-value', 'nested-interactive'] } })).violations);
      assert.deepEqual(externalViolations, [], `${name}: external fixture accessibility`);
      assert.deepEqual(errors, [], `${name}: browser errors after external drags`);
      console.log(`${name}: existing layout regressions plus external insertion and all four split edges, stable source/views, invalid targets, pane cap/minimum/compact guards, edge cancellation, links, zoom/RTL, overflow, reduced motion, Axe and strict CSP passed`);
    } finally { await browser.close(); }
  }
} finally { await new Promise(resolve => server.close(resolve)); }
