import {createServer} from 'node:http';
import {readFile, mkdir, writeFile} from 'node:fs/promises';
import {resolve, extname, sep} from 'node:path';
import {fileURLToPath} from 'node:url';
import {execFile} from 'node:child_process';
import {promisify} from 'node:util';
import {build} from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import {chromium, firefox} from '@playwright/test';

const packageRoot = fileURLToPath(new URL('../', import.meta.url));
const root = resolve(packageRoot, 'ui-test-results/csp');
await mkdir(root, {recursive: true});
await build({configFile: false, root: resolve(packageRoot, 'tests/fixtures/csp'), plugins: [stylex.vite({useCSSLayers: true, runtimeInjection: false, unstable_moduleResolution: {type: 'commonJS', rootDir: resolve(packageRoot, '../..')}}), react()], logLevel: 'warn', build: {outDir: root, emptyOutDir: true, assetsInlineLimit: 0}});
const csp = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'";
const mime = {'.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.woff2': 'font/woff2'};
let safariResult;
const server = createServer(async (req, res) => {
  if (req.method === 'POST' && req.url === '/result') {
    let body = '';
    for await (const chunk of req) {body += chunk; if (body.length > 16_384) {res.writeHead(413).end(); return;}}
    try {safariResult = JSON.parse(body); res.writeHead(204).end();} catch {res.writeHead(400).end();}
    return;
  }
  const path = resolve(root, `.${new URL(req.url, 'http://localhost').pathname === '/' ? '/index.html' : new URL(req.url, 'http://localhost').pathname}`);
  if (!path.startsWith(root + sep)) {res.writeHead(403).end(); return;}
  try {res.setHeader('Content-Security-Policy', csp); res.setHeader('Content-Type', mime[extname(path)] ?? 'application/octet-stream'); res.end(await readFile(path));} catch {res.writeHead(404).end();}
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const report = [];
try {
  for (const [name, engine] of Object.entries({chromium, firefox})) {
    const browser = await engine.launch();
    try {
      const page = await browser.newPage(); const errors = [];
      page.on('pageerror', error => errors.push(error.message));
      await page.addInitScript(() => {window.__cspErrors = []; document.addEventListener('securitypolicyviolation', event => window.__cspErrors.push(`${event.violatedDirective}: ${event.blockedURI}`));});
      await page.goto(origin);
      const code = page.getByLabel('Starlark example', {exact: true});
      await page.locator('figure').filter({has: code}).and(page.locator('[data-highlighted="true"]')).waitFor();
      const text = await code.textContent();
      if (!text.includes('return agents.get')) throw new Error('Missing highlighted text');
      await code.evaluate(el => {const node = el.querySelector('[data-token="keyword"]').firstChild; window.__codeNode = node; const range = document.createRange(); range.selectNodeContents(node); const selection = getSelection(); selection.removeAllRanges(); selection.addRange(range); window.__codeSelection = selection.toString();});
      await page.getByRole('button', {name: 'Switch theme', exact: true}).click();
      await page.waitForFunction(() => document.documentElement.dataset.theme === 'light');
      const stable = await code.evaluate(el => ({node: el.querySelector('[data-token="keyword"]').firstChild === window.__codeNode, selection: getSelection().toString() === window.__codeSelection}));
      if (!stable.node || !stable.selection) throw new Error(`Theme switch replaced code identity/selection: ${JSON.stringify(stable)}`);
      await page.getByRole('button', {name: 'Custom code theme', exact: true}).click();
      await page.waitForFunction(() => document.documentElement.dataset.theme === 'custom:csp');
      const chroma = await code.evaluate(el => {
        const token = getComputedStyle(el.querySelector('[data-token="keyword"]'));
        return {background: getComputedStyle(el).backgroundColor, tokenBackground: token.backgroundColor, weight: token.fontWeight, fontStyle: token.fontStyle, decoration: token.textDecorationLine};
      });
      if (chroma.background !== 'rgb(16, 32, 48)' || chroma.tokenBackground !== 'rgb(32, 48, 64)' || chroma.weight !== '700' || chroma.fontStyle !== 'italic' || !chroma.decoration.includes('underline')) throw new Error(`Custom Chroma attributes were discarded: ${JSON.stringify(chroma)}`);
      if (await page.getByLabel('Escaped JavaScript').locator('img, script').count()) throw new Error('Source text became executable HTML');
      await page.getByText(/Showing a bounded excerpt/).waitFor();
      await page.getByRole('button', {name: 'Open dialog', exact: true}).click();
      await page.getByRole('dialog').getByText('return True', {exact: false}).waitFor();
      await page.keyboard.press('Escape');
      await page.getByRole('dialog').waitFor({state: 'hidden'});
      await page.getByRole('button', {name: 'Open menu', exact: true}).click();
      await page.getByRole('menuitem', {name: 'Inspect safely'}).click();
      const violations = await page.evaluate(() => window.__cspErrors);
      if (errors.length || violations.length) throw new Error(`${name} errors: ${JSON.stringify({errors, violations})}`);
      await page.screenshot({path: resolve(packageRoot, `ui-test-results/csp-${name}.png`), fullPage: true});
      report.push({browser: name, csp, codeSelectionRetained: true, customChromaAttributes: true, scriptNodes: 0, cspViolations: 0});
    } finally {await browser.close();}
  }
  if (process.platform === 'darwin' && process.env.WHIP_UI_SKIP_SAFARI !== '1') {
    await promisify(execFile)('open', ['-a', 'Safari', `${origin}/?smoke=1`]);
    const deadline = Date.now() + 45_000;
    while (!safariResult && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 100));
    if (!safariResult?.passed) throw new Error(`Actual Safari smoke failed: ${JSON.stringify(safariResult ?? 'no browser report received')}`);
    report.push({...safariResult, csp});
  }
  await writeFile(resolve(packageRoot, 'ui-test-results/csp-report.json'), JSON.stringify(report, null, 2));
  console.log(`Strict CSP, lazy highlighting, code selection/theme identity and portaled controls passed in ${report.map(item => item.browser).join(', ')}.`);
} finally {await new Promise(resolve => server.close(resolve));}
