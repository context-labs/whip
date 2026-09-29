import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { startHistoryFixture } from './native-history-fixture.mjs';
import { checkHistoryRecovery } from './history-gap.mjs';

// npm run pack:web && node apps/web/scripts/native-history-recovery.mjs
const directory = process.env.WHIP_HISTORY_RECOVERY_RESULTS ?? '/tmp/whip-native-history-recovery';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
await mkdir(directory, { recursive: true });
const reports = [];
const fixture = await startHistoryFixture();
try {
  const client = await fixture.connect('native-history-recovery');
  for (const name of names) {
    let browser, page;
    const errors = [], csp = [], report = { name };
    reports.push(report);
    try {
      browser = await ({ chromium, firefox }[name]).launch();
      page = await browser.newPage({ viewport: { width: 1100, height: 900 } });
      page.setDefaultTimeout(15000);
      page.on('pageerror', error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); });
      await page.exposeFunction('__recordHistoryCSP', directive => { if (csp.length < 64) csp.push(String(directive).slice(0, 256)); });
      await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__recordHistoryCSP(event.violatedDirective).catch(() => {}); }));
      report.version = browser.version();
      report.result = await checkHistoryRecovery({ page, client, fixture, root: fixture.history.root_id, directory, name });
      assert.deepEqual(errors, []); assert.deepEqual(csp, []);
      report.passed = true;
      console.log(`${name}: held/failed exact history pages, shared evidence, explicit keyboard retry, DOM/selection/anchor/draft and canonical tail passed`);
    } catch (error) {
      report.error = String(error.stack ?? error); report.errors = errors; report.csp = csp; report.history = error.historyEvidence;
      if (page) {
        await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
        report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16000);
      }
      throw error;
    } finally {
      try { await browser?.close(); }
      finally { await writeFile(join(directory, 'results.json'), JSON.stringify(reports, null, 2)); }
    }
  }
} finally { await fixture.close(); }
