import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium } from '@playwright/test';
import { startFixture } from './native-fixture.mjs';
import { safariWorkflows } from './native-safari-workflows.mjs';

// Explicit contract rehearsal only. This does not satisfy actual Safari acceptance.
const directory = process.env.WHIP_SAFARI_REHEARSAL_RESULTS ?? '/tmp/whip-native-safari-rehearsal';
await mkdir(directory, { recursive: true });
let fixture, browser, page;
const report = { browser: 'Chromium rehearsal; NOT Safari', checks: [], errors: [], csp: [], status: 'pending' };
try {
  fixture = await startFixture({ lifetimeMs: 180000 }); browser = await chromium.launch();
  page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.on('pageerror', error => { assert(report.errors.length < 64); report.errors.push(String(error).slice(0, 2048)); });
  await page.exposeBinding('__safariRehearsalCSP', (_source, value) => { assert(report.csp.length < 64); report.csp.push(value); });
  await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => window.__safariRehearsalCSP(event.violatedDirective + ': ' + event.blockedURI)));
  const driver = {
    async click(selector, text) {
      const handle = await page.evaluateHandle(({ selector, text }) => [...document.querySelectorAll(selector)]
        .find(element => !element.disabled && (text === null || (element.getAttribute('aria-label') ?? element.textContent.trim()) === text)), { selector, text });
      try { const element = handle.asElement(); assert(element, 'Rehearsal element missing'); await element.click(); }
      finally { await handle.dispose(); }
    },
    evaluate: (callback, ...args) => page.evaluate(`(${callback.toString()})(...${JSON.stringify(args)})`),
    async call(method, path, body) {
      if (method === 'POST' && path === '/url') return await page.goto(body.url);
      if (method === 'POST' && path === '/refresh') return await page.reload();
      if (method === 'GET' && path === '/screenshot') return (await page.screenshot()).toString('base64');
      throw new Error('Unexpected rehearsal WebDriver operation');
    },
  };
  await safariWorkflows(driver, fixture, directory, report);
  assert.deepEqual(report.errors, []); assert.deepEqual(report.csp, []); report.status = 'passed';
} catch (error) { report.status = 'failed'; report.error = String(error.stack ?? error); report.page = await page?.locator('body').innerText().then(text => text.slice(0, 32768)).catch(() => null); process.exitCode = 1; console.error(error); }
finally {
  for (const close of [() => browser?.close(), () => fixture?.close()]) {
    try { await close(); } catch (error) { report.status = 'failed'; (report.cleanupErrors ??= []).push(String(error.stack ?? error).slice(0, 4096)); process.exitCode = 1; }
  }
  await writeFile(join(directory, 'rehearsal-report.json'), JSON.stringify(report, null, 2));
}
console.log(JSON.stringify(report, null, 2));
