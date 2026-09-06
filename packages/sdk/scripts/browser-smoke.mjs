import { build } from 'esbuild';
import { chromium, firefox, webkit } from '@playwright/test';
import { copyFile, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { eventually, run, startFixture } from './fixture.mjs';

const requested = (process.env.WHIP_SDK_BROWSERS ?? (process.platform === 'darwin' ? 'chromium,firefox,safari' : 'chromium,firefox')).split(',');
const fixture = await startFixture();
const results = {};
try {
  const publicDirectory = join(fixture.directory, 'public');
  await build({
    entryPoints: [fileURLToPath(new URL('../test/browser-smoke.mjs', import.meta.url))],
    outfile: join(publicDirectory, 'sdk-smoke.js'), bundle: true, format: 'esm', platform: 'browser', target: 'es2022',
  });
  await copyFile(new URL('../../protocol/schema/signing-fixture.json', import.meta.url), join(publicDirectory, 'signing-fixture.json'));
  await writeFile(join(publicDirectory, 'index.html'), '<!doctype html><html lang="en"><meta charset="utf-8"><title>WHIP SDK acceptance</title><body><pre id="status">Running isolated WHIP SDK checks…</pre><script type="module" src="/sdk-smoke.js"></script></body></html>');
  for (const name of requested) {
    let browser;
    let safariSession;
    const url = fixture.info.frontend + '/?browser=' + name;
    try {
      if (name === 'safari') {
        if (process.env.WHIP_SAFARIDRIVER_URL) {
          const base = process.env.WHIP_SAFARIDRIVER_URL.replace(/\/$/, '');
          const webdriver = async (path, body) => {
            const response = await fetch(base + path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
            const result = await response.json();
            if (!response.ok || result.value?.error) throw new Error(JSON.stringify(result.value));
            return result.value;
          };
          safariSession = (await webdriver('/session', { capabilities: { alwaysMatch: { browserName: 'safari' } } })).sessionId;
          await webdriver(`/session/${safariSession}/url`, { url });
        } else if (process.platform === 'darwin') {
          await run('open', ['-a', 'Safari', url]);
        } else throw new Error('Actual Safari requires macOS or WHIP_SAFARIDRIVER_URL');
      } else {
        const launcher = { chromium, firefox, webkit }[name];
        if (!launcher) throw new Error(`Unknown browser ${name}`);
        browser = await launcher.launch({ headless: true });
        const page = await browser.newPage();
        page.on('pageerror', error => console.error(`${name} page error: ${error}`));
        const response = await page.goto(url);
        const csp = response.headers()['content-security-policy'];
        if (!csp || csp.includes('unsafe-eval') || csp.includes('unsafe-inline')) throw new Error(`Missing strict CSP: ${csp}`);
      }
      results[name] = await eventually(async () => JSON.parse(await readFile(join(fixture.directory, name + '-result.json'), 'utf8')), { timeout: 45_000, description: `${name} SDK smoke result` });
    } catch (error) {
      results[name] = { passed: false, error: String(error), cause: error.cause ? String(error.cause) : undefined };
    } finally {
      await browser?.close();
      if (safariSession) await fetch(process.env.WHIP_SAFARIDRIVER_URL.replace(/\/$/, '') + '/session/' + safariSession, { method: 'DELETE' });
    }
  }
} finally {
  console.log(JSON.stringify(results, null, 2));
  if (process.env.WHIP_SDK_BROWSER_RESULTS) await writeFile(process.env.WHIP_SDK_BROWSER_RESULTS, JSON.stringify(results, null, 2) + '\n');
  await fixture.close();
}
if (Object.values(results).some(result => !result.passed)) process.exitCode = 1;
