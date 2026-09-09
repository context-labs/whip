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
import { themeCatalog } from '../src/generated/theme-catalog.ts';

const require = createRequire(import.meta.url);
const packageRoot = fileURLToPath(new URL('../', import.meta.url));
const output = resolve(packageRoot, 'ui-test-results/workspace-tabs');
await mkdir(output, { recursive: true });
await build({ configFile: false, root: resolve(packageRoot, 'tests/fixtures/workspace-tabs'), plugins: [stylex.vite({ useCSSLayers: {before: ['whip-reset']}, runtimeInjection: false, unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(packageRoot, '../..') } }), react()], logLevel: 'warn', build: { outDir: output, emptyOutDir: true, assetsInlineLimit: 0 } });
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
const report = { browsers: [], themes: [] };
try {
  for (const [name, engine] of Object.entries({ chromium, firefox })) {
    console.log(`${name}: workspace tab keyboard, close and reorder scenarios`);
    const browser = await engine.launch();
    const errors = [];
    try {
      const page = await browser.newPage({ viewport: { width: 1200, height: 800 } });
      page.setDefaultTimeout(10_000); page.setDefaultNavigationTimeout(20_000);
      page.on('pageerror', error => errors.push(error.message));
      await page.addInitScript(() => {
        window.__cspErrors = [];
        document.addEventListener('securitypolicyviolation', event => window.__cspErrors.push(`${event.violatedDirective}: ${event.blockedURI}`));
      });
      const visit = async (params = '') => { await page.goto(`${origin}/${params}`); await expect(page.getByRole('tab')).toHaveCount(params.includes('many') ? 32 : 4); };
      const tab = value => page.locator(`[role="tab"][id="whip-workspace-tab-${value}"]`);
      const selected = value => expect(page.getByLabel('Active session', { exact: true })).toHaveText(value);
      for (const variant of ['', '?buttons']) {
        await visit(variant);
        await tab('alpha').focus();
        await tab('alpha').press('ArrowRight'); await expect(tab('beta')).toBeFocused(); await selected('alpha');
        await tab('beta').press('End'); await expect(tab('delta')).toBeFocused(); await selected('alpha');
        await tab('delta').press('Home'); await expect(tab('alpha')).toBeFocused();
        await tab('alpha').press('ArrowRight'); await tab('beta').press(' '); await selected('beta');
        await expect(page.getByLabel('Navigation count', { exact: true })).toHaveText('1');
        await tab('beta').press('ArrowRight'); await tab('gamma').press('Enter'); await selected('gamma');
        await expect(page.getByLabel('Navigation count', { exact: true })).toHaveText('2');
        await expect(page.getByRole('tabpanel')).toHaveAttribute('aria-labelledby', 'whip-workspace-tab-gamma');
        assert.equal(await tab('gamma').locator('button,a').count(), 0, 'Tab contains an interactive child');
        await tab('gamma').press('Delete'); await expect(tab('gamma')).toHaveCount(0); await expect(tab('delta')).toBeFocused(); await selected('delta');
        await tab('delta').press('Delete'); await expect(tab('beta')).toBeFocused(); await selected('beta');
        await tab('alpha').click({ button: 'middle' }); await expect(tab('alpha')).toHaveCount(0); await selected('beta');
        await tab('beta').press('Delete'); await expect(page.getByRole('button', { name: 'New session', exact: true })).toBeFocused(); await selected('none');
      }
      await visit();
      const tree = await page.getByRole('tablist').ariaSnapshot();
      assert.equal((tree.match(/\n\s+- tab /g) ?? []).length, 4, 'Accessibility tablist does not own all four tabs');
      assert(!tree.includes('- button '), 'Sibling actions were incorrectly owned by the tablist');
      if (name === 'chromium') {
        const cdp = await page.context().newCDPSession(page);
        const {nodes} = await cdp.send('Accessibility.getFullAXTree');
        const owner = nodes.find(node => node.role?.value === 'tablist');
        assert.deepEqual(owner.childIds.map(id => nodes.find(node => node.nodeId === id)?.role?.value), ['tab','tab','tab','tab'], 'Native Chromium AX ownership differs from ARIA ownership');
        await cdp.detach();
      }
      await tab('alpha').focus();
      await page.getByRole('button', { name: /^Close Investigate storage/ }).click();
      await selected('alpha'); await expect(tab('alpha')).toBeFocused();
      await page.getByRole('button', { name: 'Home', exact: true }).click();
      await expect(page.locator('[role="tab"][aria-selected="true"]')).toHaveCount(0);
      await expect(page.getByRole('tabpanel')).toHaveCount(0);
      await tab('alpha').click(); await selected('alpha');
      // Ctrl/Meta link activation retains native new-tab behavior without selecting another app tab.
      const popup = page.context().waitForEvent('page', { timeout: 10_000 });
      await tab('gamma').click({ modifiers: [process.platform === 'darwin' ? 'Meta' : 'Control'] });
      await (await popup).close(); await selected('alpha');
      await expect(page.getByLabel('Navigation count', { exact: true })).toHaveText('1');
      await tab('gamma').click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Move left', exact: true }).click();
      await expect(page.getByLabel('Reordered sessions', { exact: true })).toHaveText('gamma,alpha,delta');
      await selected('alpha');

      for (const theme of ['light', 'dark', 'claude-code']) {
        await visit(`?theme=${theme}`);
        const shape = await tab('alpha').evaluate(element => {
          const item = element.closest('[data-workspace-tab]');
          const strip = item.closest('[data-workspace-tab-strip]');
          const surface = item.querySelector('[data-workspace-tab-shape]');
          const center = getComputedStyle(surface.firstElementChild);
          return { bottom: item.getBoundingClientRect().bottom, stripBottom: strip.getBoundingClientRect().bottom,
            borderTop: center.borderTopWidth, borderBottom: center.borderBottomWidth,
            shoulder: surface.querySelector('svg').getBoundingClientRect().left, stripLeft: strip.getBoundingClientRect().left,
            body: getComputedStyle(document.getElementById('workspace-panel').parentElement.parentElement).backgroundColor,
            fill: center.backgroundColor };
        });
        assert(Math.abs(shape.bottom - shape.stripBottom) < 1, 'Active tab does not reach the content edge');
        assert.equal(shape.borderTop, '1px', 'Active tab is missing its upper border');
        assert.equal(shape.borderBottom, '0px', 'Active tab must stay open to the content below');
        assert(shape.shoulder >= shape.stripLeft, 'First tab shoulder is clipped by the scroller');
        assert.equal(shape.fill, shape.body);
        await page.screenshot({ path: resolve(output, `${name}-${theme}.png`) });
        await tab('beta').click();
        await page.mouse.move(1100, 600);
        await page.screenshot({ path: resolve(output, `${name}-${theme}-middle.png`) });
      }
      await visit();
      const originalDraft = await page.getByRole('textbox', { name: 'Conversation draft' }).inputValue();
      const from = await tab('alpha').boundingBox(); const to = await page.locator('[data-workspace-tab="gamma"]').boundingBox();
      await page.mouse.move(from.x + 40, from.y + from.height / 2); await page.mouse.down();
      await page.mouse.move(from.x + 43, from.y + from.height / 2);
      await expect(page.getByLabel('Reordered sessions', { exact: true })).toHaveText('');
      await page.mouse.up();
      await page.mouse.move(from.x + 40, from.y + from.height / 2); await page.mouse.down();
      await page.mouse.move(from.x + 48, from.y + from.height / 2);
      const preview = page.locator('[data-workspace-drag-preview]');
      await expect(preview).toHaveCount(1);
      assert.equal(await preview.evaluate(element => getComputedStyle(element).boxShadow), 'none', 'Dragged tab should glide without a shadow');
      const grabbed = await preview.boundingBox();
      assert(Math.abs(grabbed.x - (from.x + 8)) < 2, `Preview lost the original pointer offset: ${JSON.stringify({from, grabbed})}`);
      await page.mouse.move(from.x + 68, from.y + from.height / 2);
      await expect.poll(async () => (await preview.boundingBox()).x).toBeGreaterThan(grabbed.x + 15);
      await page.mouse.move(to.x + to.width / 2 + 4, to.y + to.height / 2, { steps: 15 });
      await expect.poll(async () => (await tab('beta').boundingBox()).x).toBeLessThan(from.x + 2);
      assert.deepEqual(await page.locator('[data-workspace-tab]').evaluateAll(elements => elements.map(el => el.dataset.workspaceTab)), ['alpha','beta','gamma','delta'], 'Dragging mutated the committed DOM order');
      await expect(page.getByLabel('Reordered sessions', { exact: true })).toHaveText('');
      await page.screenshot({ path: resolve(output, `${name}-dragging.png`) });
      await page.mouse.up();
      await expect(page.getByLabel('Reordered sessions', { exact: true })).toHaveText('beta,gamma,alpha,delta');
      await selected('alpha');
      await expect(page.getByRole('textbox', { name: 'Conversation draft' })).toHaveValue(originalDraft);
      await expect(page.getByLabel('Navigation count', { exact: true })).toHaveText('1');
      // Dragging a background tab must not activate its link; Escape restores its previous order.
      const background = await tab('beta').boundingBox(); const last = await page.locator('[data-workspace-tab="delta"]').boundingBox();
      await page.mouse.move(background.x + 40, background.y + background.height / 2); await page.mouse.down();
      await page.mouse.move(last.x + last.width / 2 + 4, last.y + last.height / 2, { steps: 20 });
      await expect(preview).toHaveCount(1);
      await expect.poll(async () => (await tab('gamma').boundingBox()).x).toBeLessThan(background.x + 2);
      await page.keyboard.press('Escape'); await page.mouse.up();
      await expect(page.locator('[data-dragging]')).toHaveCount(0);
      await expect.poll(() => page.locator('[data-workspace-tab]').evaluateAll(elements => elements.map(el => el.dataset.workspaceTab).join(','))).toBe('beta,gamma,alpha,delta');
      await expect(page.getByLabel('Reordered sessions', { exact: true })).toHaveText('beta,gamma,alpha,delta');
      await expect(page.getByLabel('Navigation count', { exact: true })).toHaveText('1'); await selected('alpha');
      assert.deepEqual(await page.evaluate(() => window.__cspErrors), [], `${name}: drag caused a CSP violation`);

      // Drop to the left, RTL, and browser zoom share the same pointer/slot geometry.
      for (const { rtl, zoom, reduced = false } of [{ rtl: false, zoom: 1 }, { rtl: true, zoom: 1 }, { rtl: false, zoom: 1.25 }, { rtl: false, zoom: 1.5 }, { rtl: false, zoom: 2 }, { rtl: false, zoom: 1, reduced: true }]) {
        await visit(rtl ? '?rtl' : '');
        await page.emulateMedia({ reducedMotion: reduced ? 'reduce' : 'no-preference' });
        await page.evaluate(zoom => { document.documentElement.style.zoom = String(zoom); }, zoom);
        const source = await page.locator('[data-workspace-tab="beta"]').boundingBox();
        const destination = await page.locator('[data-workspace-tab="alpha"]').boundingBox();
        const handle = await tab('beta').boundingBox();
        const grabX = handle.x + handle.width / 2;
        const offset = grabX - source.x;
        await page.mouse.move(grabX, source.y + source.height / 2); await page.mouse.down();
        await page.mouse.move(grabX + 8, source.y + source.height / 2);
        await expect(preview).toHaveCount(1);
        await expect.poll(async () => Math.abs((await preview.boundingBox()).width - source.width)).toBeLessThan(2);
        const x = destination.x + destination.width / 2 + (rtl ? 8 : -8);
        await page.mouse.move(x, destination.y + destination.height / 2, { steps: 10 });
        await expect.poll(async () => Math.abs((await preview.boundingBox()).x + offset - x)).toBeLessThan(2);
        if (reduced) assert(await page.locator('[data-workspace-tab]').evaluateAll(items => items.every(item => item.getAnimations().every(animation => animation.effect.getTiming().duration === 0))), 'Reduced motion animated neighboring tabs');
        await page.mouse.up();
        await expect(page.getByLabel('Reordered sessions', { exact: true })).toHaveText('beta,alpha,gamma,delta');
        await expect(preview).toHaveCount(0);
        await expect(page.locator('[data-tab-settling]')).toHaveCount(0);
      }
      await page.emulateMedia({ reducedMotion: 'no-preference' });
      for (const cancel of ['outside', 'blur', 'capture', 'removed']) {
        await visit();
        const box = await tab('alpha').boundingBox();
        await page.mouse.move(box.x + 40, box.y + box.height / 2); await page.mouse.down();
        await page.mouse.move(box.x + 70, box.y + box.height / 2);
        await expect(preview).toHaveCount(1);
        if (cancel === 'outside') await page.mouse.move(500, 600);
        if (cancel === 'blur') await page.evaluate(() => window.dispatchEvent(new Event('blur')));
        if (cancel === 'capture') await page.evaluate(() => window.dispatchEvent(new PointerEvent('lostpointercapture', { buttons: 1 })));
        if (cancel === 'removed') await page.getByRole('button', { name: /^Close Inspect event/ }).evaluate(button => button.click());
        await page.mouse.up();
        await expect(preview).toHaveCount(0);
        await expect(page.locator('[data-tab-settling]')).toHaveCount(0);
        await expect(page.getByLabel('Reordered sessions', { exact: true })).toHaveText('');
        assert(await page.locator('[data-workspace-tab]').evaluateAll(items => items.every(item => !item.style.transform)), 'Cancellation left tab transforms behind');
      }
      await visit('?many');
      const scrollStrip = page.locator('[data-workspace-tab-strip]');
      const start = await tab('session-0').boundingBox();
      const edge = await scrollStrip.boundingBox();
      await page.mouse.move(start.x + 40, start.y + start.height / 2); await page.mouse.down();
      await page.mouse.move(start.x + 48, start.y + start.height / 2);
      await page.mouse.move(edge.x + edge.width - 8, start.y + start.height / 2, { steps: 10 });
      await expect.poll(() => scrollStrip.evaluate(element => element.scrollLeft)).toBeGreaterThan(200);
      await expect(preview).toHaveCount(1);
      await page.mouse.up();
      await expect(preview).toHaveCount(0);
      assert.notEqual(await page.getByLabel('Reordered sessions', { exact: true }).textContent(), '', 'Auto-scroll did not commit its final destination');

      await visit('?many');
      const list = page.locator('[data-workspace-tab-strip]');
      await page.getByRole('button', { name: 'Select last', exact: true }).click();
      await expect(tab('session-31')).toBeInViewport();
      const lastShoulder = await page.locator('[data-workspace-tab="session-31"] [data-workspace-tab-shape] svg').last().boundingBox();
      const visibleStrip = await list.boundingBox();
      assert(lastShoulder.x + lastShoulder.width <= visibleStrip.x + visibleStrip.width + 1, 'Revealing the last tab clips its shoulder');
      const before = await list.evaluate(el => el.scrollLeft);
      await page.getByRole('button', { name: 'Change status', exact: true }).click();
      assert.equal(await list.evaluate(el => el.scrollLeft), before, 'Status update changed the strip scroll position');
      const widths = await page.locator('[data-workspace-tab]').evaluateAll(elements => elements.map(el => el.getBoundingClientRect().width));
      assert(widths.every(width => width >= 144 && width <= 224), `Unreadable tab widths: ${widths}`);
      assert.equal(await list.evaluate(el => el.parentElement.getBoundingClientRect().height), 48);
      await page.evaluate(() => { document.documentElement.style.zoom = '2'; });
      await page.getByRole('button', { name: 'Home', exact: true }).click();
      await page.getByRole('button', { name: 'Select last', exact: true }).click();
      await expect(tab('session-31')).toBeInViewport();
      const utilityBounds = await page.getByRole('button', { name: 'New session', exact: true }).boundingBox();
      assert(utilityBounds.x >= 0 && utilityBounds.x + utilityBounds.width <= 1200, 'Utilities were clipped at 200% zoom');

      await visit('?rtl');
      await tab('alpha').focus(); await tab('alpha').press('ArrowLeft'); await expect(tab('beta')).toBeFocused(); await selected('alpha');
      await page.emulateMedia({ reducedMotion: 'reduce' });
      await tab('beta').press(' '); await selected('beta');
      await page.screenshot({ path: resolve(output, `${name}-rtl.png`) });
      const touch = await browser.newContext({ viewport: { width: 1024, height: 768 }, hasTouch: true });
      const touchPage = await touch.newPage(); touchPage.setDefaultTimeout(10_000);
      await touchPage.goto(origin); await expect(touchPage.getByRole('tab')).toHaveCount(4);
      const touchTab = touchPage.locator('#whip-workspace-tab-alpha');
      await touchTab.dispatchEvent('pointerdown', { pointerId: 1, pointerType: 'touch', isPrimary: true, button: 0, buttons: 1, clientX: 20, clientY: 60 });
      await touchTab.dispatchEvent('pointermove', { pointerId: 1, pointerType: 'touch', isPrimary: true, buttons: 1, clientX: 600, clientY: 60 });
      await expect(touchPage.locator('[data-dragging]')).toHaveCount(0);
      await touchTab.dispatchEvent('pointerup', { pointerId: 1, pointerType: 'touch', isPrimary: true, button: 0, clientX: 600, clientY: 60 });
      await expect(touchPage.getByLabel('Reordered sessions', { exact: true })).toHaveText('');
      const closeSize = await touchPage.getByRole('button', { name: /^Close Inspect event/ }).boundingBox();
      assert(closeSize.width >= 44 && closeSize.height >= 44, 'Touch close target is too small');
      await touch.close();
      if (name === 'chromium') {
        console.log(`chromium: workspace tab states across all ${themeCatalog.length} themes`);
        for (const theme of themeCatalog) {
          await visit(`?theme=${encodeURIComponent(theme.id)}`);
          const appearance = await tab('alpha').evaluate(element => {
            const item = element.closest('[data-workspace-tab]');
            const row = item.closest('[data-workspace-tab-strip]').parentElement;
            const center = getComputedStyle(item.querySelector('[data-workspace-tab-shape]').firstElementChild);
            return {
              canvas: center.backgroundColor,
              navigation: getComputedStyle(row).backgroundColor,
              borderWidth: center.borderTopWidth,
              borderStyle: center.borderTopStyle,
              borderBottomWidth: center.borderBottomWidth,
              closeBorderWidth: getComputedStyle(item.querySelector('button[aria-label^="Close "]')).borderTopWidth,
            };
          });
          assert.notEqual(appearance.navigation, 'rgba(0, 0, 0, 0)', `${theme.id}: navigation surface is missing`);
          assert.notEqual(appearance.navigation, appearance.canvas, `${theme.id}: selected tab has no surface separation`);
          assert.equal(appearance.borderWidth, '1px', `${theme.id}: active tab is missing its upper outline`);
          assert.equal(appearance.borderStyle, 'solid');
          assert.equal(appearance.borderBottomWidth, '0px', `${theme.id}: active tab must stay open along the bottom`);
          assert.equal(appearance.closeBorderWidth, '0px', `${theme.id}: close button has a browser-default border`);
          await tab('alpha').focus();
          await page.addScriptTag({ url: `${origin}/axe.js` });
          const violations = await page.evaluate(async () => (await axe.run(document.getElementById('root'), { runOnly: { type: 'rule', values: ['color-contrast', 'button-name', 'aria-required-children', 'aria-required-parent', 'aria-valid-attr-value', 'nested-interactive'] } })).violations.map(v => ({ id: v.id, nodes: v.nodes.map(n => ({ html: n.html, failure: n.failureSummary })) })));
          report.themes.push({ id: theme.id, violations });
          assert.deepEqual(violations, [], `${theme.id}: workspace tab accessibility failed`);
        }
      }
      assert.deepEqual(errors, [], `${name}: browser errors`);
      report.browsers.push({ name, passed: true, csp, manualActivation: true, siblingControls: true, focusAfterClose: true, nativeModifiedLinks: true, pointerReorder: true, noTouchDrag: true, overflow: true, rtl: true });
    } finally { await browser.close(); }
  }
  console.log(`Workspace tab strict-CSP keyboard/close/reorder/overflow/RTL/touch scenarios passed in Chromium and Firefox; all ${themeCatalog.length} themes passed Axe.`);
} finally {
  await writeFile(resolve(packageRoot, 'ui-test-results/workspace-tabs-report.json'), JSON.stringify(report, null, 2));
  await new Promise(resolve => server.close(resolve));
}
