import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {readFile, mkdir, writeFile} from 'node:fs/promises';
import {resolve, extname, sep} from 'node:path';
import {fileURLToPath} from 'node:url';
import {createRequire} from 'node:module';
import {build} from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import {chromium, firefox, webkit, expect as baseExpect} from '@playwright/test';
const expect = baseExpect.configure({timeout: 15000});
import {themeCatalog} from '../src/generated/theme-catalog.ts';

const require = createRequire(import.meta.url);
const packageRoot = fileURLToPath(new URL('../', import.meta.url));
const output = resolve(packageRoot, 'ui-test-results/mermaid');
await mkdir(output, {recursive: true});
await build({configFile: false, root: resolve(packageRoot, 'tests/fixtures/mermaid'), plugins: [stylex.vite({useCSSLayers: {before: ['whip-reset']}, runtimeInjection: false, unstable_moduleResolution: {type: 'commonJS', rootDir: resolve(packageRoot, '../..')}}), react()], logLevel: 'warn', build: {outDir: output, emptyOutDir: true, assetsInlineLimit: 0}});
// Use the production policy verbatim; do not relax the existing generic UI fixture.
const csp = (await readFile(resolve(packageRoot, '../../internal/webassets/csp.txt'), 'utf8')).trim();
const mime = {'.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2'};
const server = createServer(async (req, res) => {
  const pathname = new URL(req.url, 'http://localhost').pathname;
  if (pathname === '/axe.js') {res.setHeader('Content-Type', 'text/javascript'); res.end(await readFile(require.resolve('axe-core/axe.min.js'))); return;}
  const path = resolve(output, `.${pathname === '/' ? '/index.html' : pathname}`);
  if (!path.startsWith(output + sep)) {res.writeHead(403).end(); return;}
  try {res.setHeader('Content-Security-Policy', csp); res.setHeader('Content-Type', mime[extname(path)] ?? 'application/octet-stream'); res.end(await readFile(path));} catch {res.writeHead(404).end();}
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const code = 'flowchart TD\n  Root[Root agent] --> Worker[Tool worker]\n  Worker --> Mailbox[Mailbox]\n  Mailbox --> Root';
const report = {browsers: [], skipped: [], themes: [], checks: [], notes: ['Playwright WebKit screenshots inject a preparatory body{} stylesheet and log a CSP warning; exact-CSP diagram background and embedded-font canvas pixels are checked independently.']};
try {
  for (const [name, engine] of Object.entries({chromium, firefox, webkit}).filter(([name]) => !process.env.WHIP_MERMAID_BROWSERS || process.env.WHIP_MERMAID_BROWSERS.split(',').includes(name))) {
    let browser;
    try {browser = await engine.launch();} catch (error) {if (name !== 'webkit') throw error; report.skipped.push(`webkit: ${error.message.split('\n')[0]}`); continue;}
    console.log(`${name}: Mermaid source/image, lifecycle, appearance, CSP and dialog checks`);
    try {
      const page = await browser.newPage({viewport: {width: 1000, height: 800}});
      page.setDefaultTimeout(15000);
      const errors = [], external = [];
      page.on('pageerror', error => errors.push(error.message));
      page.on('console', message => {if (message.type() === 'error') console.error(`${name} browser: ${message.text()}`);});
      page.on('request', request => {if (!request.url().startsWith(origin) && /^https?:/.test(request.url())) external.push(request.url());});
      await page.addInitScript(() => {
        window.__mermaidCSP = [];
        window.__mermaidURLs = {created: [], revoked: [], blobs: {}};
        window.__mermaidWorkers = 0; window.__mermaidTerminations = 0;
        const create = URL.createObjectURL.bind(URL), revoke = URL.revokeObjectURL.bind(URL);
        URL.createObjectURL = blob => {const url = create(blob); window.__mermaidURLs.created.push(url); window.__mermaidURLs.blobs[url] = blob; return url;};
        URL.revokeObjectURL = url => {window.__mermaidURLs.revoked.push(url); revoke(url);};
        const WorkerClass = window.Worker;
        window.Worker = class extends WorkerClass {constructor(...args) {super(...args); window.__mermaidWorkers++;} terminate() {window.__mermaidTerminations++; super.terminate();}};
        document.addEventListener('securitypolicyviolation', event => window.__mermaidCSP.push(`${event.violatedDirective}: ${event.blockedURI}`));
      });
      const figure = page.locator('[data-mermaid-view]');
      const diagram = () => figure.getByRole('img');
      const source = () => figure.getByRole('region', {name: 'Mermaid source', exact: true});
      const visit = params => page.goto(`${origin}/${params ?? ''}`);
      const ready = async () => {await expect(diagram()).toBeVisible(); await expect.poll(() => diagram().evaluate(image => image.complete && image.naturalWidth > 0)).toBe(true);};
      const assertPolicy = async () => {assert.deepEqual(await page.evaluate(() => window.__mermaidCSP), []); assert.deepEqual(external, []);};
      await visit('?ordinary');
      await expect(page.getByRole('region', {name: 'Ordinary code'})).toHaveText('Source remains plain text.');
      await expect(page.getByRole('button', {name: 'Copy Ordinary code'})).toBeVisible();
      assert.equal(await page.evaluate(() => window.__mermaidWorkers), 0, 'ordinary code must not start the renderer');
      await page.getByRole('button', {name: 'Mount diagram'}).click(); await ready();
      await expect(diagram()).toHaveAttribute('src', /^blob:/);
      assert.equal(await figure.locator('svg:not(button svg)').count(), 0, 'diagram SVG must not enter the page DOM');
      await figure.getByRole('button', {name: 'Copy source', exact: true}).click();
      await expect.poll(() => page.evaluate(() => window.copiedMermaid)).toBe(code);
      await figure.getByRole('button', {name: 'Source', exact: true}).click(); await expect(source()).toHaveText(code);
      await figure.getByRole('button', {name: 'Diagram', exact: true}).click(); await ready();
      const priorURL = await diagram().getAttribute('src'), priorBox = await figure.boundingBox();
      const border = await figure.evaluate(element => getComputedStyle(element).borderTopWidth); assert.equal(border, '1px', 'compiled border remains visible');
      await page.getByRole('button', {name: 'Switch theme'}).click();
      await expect.poll(() => diagram().getAttribute('src')).not.toBe(priorURL);
      const nextBox = await figure.boundingBox(); assert.ok(Math.abs(priorBox.height - nextBox.height) < 1, 'theme-only replacement preserves footprint');
      await expect.poll(() => page.evaluate(url => window.__mermaidURLs.revoked.includes(url), priorURL)).toBe(true);
      await page.getByRole('button', {name: 'Preview theme', exact: true}).click(); await ready();
      await page.getByRole('button', {name: 'Cancel preview', exact: true}).click(); await ready();
      await figure.getByRole('button', {name: 'Expand', exact: true}).click();
      const dialog = page.getByRole('dialog'); await expect(dialog).toBeVisible();
      await dialog.getByRole('button', {name: '100%', exact: true}).click();
      await expect(dialog.getByRole('button', {name: '100%', exact: true})).toHaveAttribute('aria-pressed', 'true');
      await dialog.getByRole('button', {name: 'Fit', exact: true}).click();
      await page.keyboard.press('Escape'); await expect(dialog).toHaveCount(0);
      await expect(figure.getByRole('button', {name: 'Expand', exact: true})).toBeFocused();
      const fontImage = await diagram().getAttribute('src');
      const pixels = async () => diagram().evaluate(image => {const canvas = document.createElement('canvas'); canvas.width = image.naturalWidth; canvas.height = image.naturalHeight; const ctx = canvas.getContext('2d'); ctx.drawImage(image, 0, 0); return Array.from(ctx.getImageData(0, 0, canvas.width, canvas.height).data);});
      const interPixels = await pixels();
      const background = await figure.getByRole('region', {name: 'Mermaid diagram', exact: true}).evaluate(element => getComputedStyle(element).backgroundColor);
      assert.deepEqual(interPixels.slice(0, 3), background.match(/\d+/g).slice(0, 3).map(Number), 'SVG image applies the resolved theme background under CSP');
      const svg = await diagram().evaluate(image => window.__mermaidURLs.blobs[image.src].text());
      assert.match(svg, /@font-face/); assert.doesNotMatch(svg, /@import|fonts\.googleapis\.com/);
      await page.getByRole('button', {name: 'System diagram font', exact: true}).click();
      await expect.poll(() => diagram().getAttribute('src')).not.toBe(fontImage);
      assert.notDeepEqual(await pixels(), interPixels, 'embedded Inter must visibly differ from system font');
      await page.getByRole('button', {name: 'Larger system font'}).click(); await ready();
      await page.getByRole('button', {name: 'Increase contrast'}).click(); await ready();
      await assertPolicy();
      await page.addScriptTag({url: `${origin}/axe.js`});
      assert.deepEqual(await page.evaluate(async () => (await window.axe.run(document)).violations.map(({id, nodes}) => ({id, nodes: nodes.map(n => n.target)}))), []);
      // WebKit's Playwright screenshot helper injects body{} to sync animation;
      // its CSP console warning is separate from the pixel-verified SVG image.
      console.log(`${name}: screenshot diagram`);
      await page.screenshot({path: resolve(output, `${name}-diagram.png`), fullPage: true, caret: 'initial'});
      console.log(`${name}: screenshot diagram complete`);
      await page.getByRole('button', {name: 'Unmount diagram'}).click();
      await expect.poll(() => page.evaluate(() => window.__mermaidURLs.created.every(url => window.__mermaidURLs.revoked.includes(url)))).toBe(true);
      await expect.poll(() => page.evaluate(() => window.__mermaidWorkers === window.__mermaidTerminations)).toBe(true);
      await visit('?live'); await expect(source()).toHaveText(code);
      assert.equal(await page.evaluate(() => window.__mermaidWorkers), 0, 'live source must not start renderer');
      await source().focus(); await page.evaluate(() => window.settleMermaid());
      await expect(figure.getByRole('button', {name: 'Diagram', exact: true})).toBeEnabled();
      await expect(source()).toBeFocused(); await expect(source()).toHaveText(code);
      await figure.getByRole('button', {name: 'Diagram', exact: true}).click(); await ready();
      await visit('?live');
      await source().evaluate(element => {const range = document.createRange(); range.selectNodeContents(element); window.getSelection().removeAllRanges(); window.getSelection().addRange(range);});
      await page.evaluate(() => window.settleMermaid());
      await expect(figure.getByRole('button', {name: 'Diagram', exact: true})).toBeEnabled();
      assert.equal(await page.evaluate(() => window.getSelection().toString()), code);
      await expect(source()).toHaveText(code);
      await visit('?live'); await page.evaluate(() => window.settleMermaid()); await ready();
      await visit('?decorate&live'); await expect(source().locator('mark')).toHaveText(code);
      for (const variant of ['truncated', 'unsupported', 'unsafe', 'large', 'invalid']) {
        await visit(`?${variant}`); await expect(source()).toBeVisible();
        await expect(figure.getByRole('status').first()).toBeVisible();
        if (variant === 'truncated') {
          await expect(figure.getByRole('status')).toHaveCount(1);
          await expect(figure.getByRole('status')).toHaveText('This diagram source is incomplete. Showing source.');
          await expect(source()).toHaveText(code);
          await expect(figure.getByText(/Showing a bounded excerpt/)).toHaveCount(0);
          // Local CodeBlock clipping still reports the bytes it actually omitted.
          await page.evaluate(() => window.setMermaidSource('flowchart TD\n' + 'x'.repeat(20000)));
          await expect(figure.getByRole('status')).toHaveCount(2);
          await expect(figure.getByText(/Showing a bounded excerpt \(16,384 bytes\)/)).toBeVisible();
          assert.equal((await source().textContent()).length, 16384);
        }
        await expect(diagram()).toHaveCount(0);
        if (variant !== 'invalid') assert.equal(await page.evaluate(() => window.__mermaidWorkers), 0, `${variant} source must not start renderer`);
        await assertPolicy();
      }
      for (const family of ['sequence', 'state', 'class', 'er', 'xychart', 'nonlatin']) {
        await visit(`?${family}`); await ready(); await assertPolicy();
        console.log(`${name}: screenshot ${family}`);
        await page.screenshot({path: resolve(output, `${name}-${family}.png`), fullPage: true, caret: 'initial'});
        console.log(`${name}: screenshot ${family} complete`);
      }
      await page.route('**/*mermaid-worker*.js', route => route.abort());
      await visit(); await expect(figure.getByRole('button', {name: 'Retry', exact: true})).toBeVisible();
      await expect(source()).toHaveText(code);
      await page.unroute('**/*mermaid-worker*.js');
      await figure.getByRole('button', {name: 'Retry', exact: true}).click(); await ready();
      await visit(); await ready();
      await page.evaluate(() => {window.setMermaidSource('flowchart TD\n  X[New source] --> Y[Current]');});
      await figure.getByRole('button', {name: 'Source', exact: true}).click();
      await expect(source()).toContainText('New source');
      await expect(source()).not.toContainText('Root agent');
      await figure.getByRole('button', {name: 'Diagram', exact: true}).click(); await ready();
      await assertPolicy();
      assert.deepEqual(errors, []);
      if (name === 'chromium') {
        for (const theme of themeCatalog) {
          await visit(`?theme=${encodeURIComponent(theme.id)}`); await ready(); await assertPolicy(); report.themes.push(theme.id);
        }
        const touch = await browser.newPage({viewport: {width: 360, height: 740}, hasTouch: true, isMobile: true});
        await touch.goto(`${origin}/?wide`); await expect(touch.locator('[data-mermaid-view] img')).toBeVisible();
        const dimensions = await touch.evaluate(() => ({width: window.innerWidth, scroll: document.documentElement.scrollWidth}));
        assert.ok(dimensions.scroll <= dimensions.width, 'diagram must not cause page-level horizontal overflow');
        for (const button of await touch.locator('[data-mermaid-view] button').all()) assert.ok((await button.boundingBox()).height >= 44, 'touch controls are at least 44px tall');
        await touch.screenshot({path: resolve(output, 'narrow-touch.png'), fullPage: true, caret: 'initial'});
        await touch.getByRole('button', {name: 'Expand', exact: true}).click();
        for (const label of ['Fit', '100%']) assert.ok((await touch.getByRole('dialog').getByRole('button', {name: label, exact: true}).boundingBox()).height >= 44);
        await touch.close();
      }
      report.browsers.push(name);
    } finally {await browser.close();}
  }
  report.checks = ['isolated Blob image under production CSP; no third-party requests', 'lazy no-worker ordinary/live/ineligible source', 'source/copy authority and CodeBlock default header regression', 'theme footprint, URL replacement/unmount cleanup', 'source focus/selection preserved at settlement', 'dialog Fit/100% and Escape focus return', 'a11y scan, narrow touch controls, all-theme smoke'];
  await writeFile(resolve(output, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report, null, 2));
} finally {await new Promise(resolve => server.close(resolve));}
