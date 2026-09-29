import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';
import { contentTransport } from './native-content-transport.mjs';
import { checkComposerAttachments } from './composer-attachments.mjs';
import { checkStoredMessages } from './stored-messages.mjs';

const directory = process.env.WHIP_CONTENT_RESULTS ?? '/tmp/whip-native-content-results';
const browsers = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
const results = {};
await mkdir(directory, { recursive: true });
const manifest = JSON.parse(await readFile(new URL('../renderer-manifest.json', import.meta.url), 'utf8'));
for (const name of browsers) for (const mode of (process.env.WHIP_CONTENT_MODE ?? 'composer,stored').split(',')) {
  assert.ok(['composer', 'stored'].includes(mode), 'Unsupported content mode');
  assert.ok(['chromium', 'firefox'].includes(name), 'Unsupported browser');
  let fixture, browser, page, transfers;
  const errors = [], cspErrors = [];
  try {
    fixture = await startFixture({ lifetimeMs: 600_000, executeCode: true });
    const client = await fixture.connect(`content-probe-${crypto.randomUUID()}`);
    const { root } = await fixture.createRoot(client, { title: 'Native content acceptance' });
    const session = client.session(root.id);
    // Real admitted turns provide enough history for drop/textarea anchor tests.
    for (let index = 0; index < 12; index++) {
      const request = crypto.randomUUID();
      await session.submit([{ type: 'text', text: `Reading message ${index + 1}.\n\n` + 'Native content preserves my reading position. '.repeat(20) }], request, deadline());
      assert.equal((await client.wait(request, deadline())).turn.state, 'succeeded');
    }
    for (const [path, file] of Object.entries(manifest.files)) {
      const response = await fetch(`${fixture.info.web}/${path}`);
      assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256, `Production renderer differs: ${path}`);
    }
    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.setDefaultTimeout(15_000);
    page.on('pageerror', error => { if (errors.length < 64) errors.push(error.message.slice(0, 2048)); });
    await page.exposeFunction('recordContentCSP', directive => { if (cspErrors.length < 64) cspErrors.push(String(directive)); });
    await page.addInitScript(() => {
      window.cspErrors = [];
      document.addEventListener('securitypolicyviolation', event => { if (window.cspErrors.length < 64) { window.cspErrors.push(event.violatedDirective); void window.recordContentCSP(event.violatedDirective); } });
    });
    transfers = await contentTransport(page, root.id);
    const response = await page.goto(`${fixture.info.web}/h/${fixture.info.runtime_id}/s/${root.id}`);
    assert.ok(response.headers()['content-security-policy'].includes("script-src 'self' 'wasm-unsafe-eval'"));
    await expect(page.getByLabel('Message WHIP', { exact: true })).toBeVisible();
    await page.evaluate(() => document.fonts.ready);
    const checks = await (mode === 'composer' ? checkComposerAttachments : checkStoredMessages)({ page, directory, name: `${name}-${mode}`, transfers, session, client, root: root.id });
    // Closing the probe must discard a held mutation, not release it upstream.
    transfers.hold('content.put');
    const beforeClose = transfers.count('content.put');
    await page.getByLabel('Message WHIP', { exact: true }).locator('xpath=ancestor::form').locator('input[type=file]').setInputFiles({ name: 'cleanup-only.txt', mimeType: 'text/plain', buffer: Buffer.from('Never dispatch during cleanup') });
    const held = await eventually(() => transfers.count('content.put') > beforeClose && transfers.records.findLast(record => record.method === 'content.put'));
    transfers.close();
    await assert.rejects(session.content.get(held.reference, deadline()), error => error.kind === 'NOT_FOUND');
    transfers.assertHealthy();
    assert.deepEqual(errors, []);
    assert.deepEqual(cspErrors, []);
    assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
    results[`${name}-${mode}`] = { rendererDigest: manifest.digest, checks, contentTransfers: transfers.records, pageErrors: errors, cspErrors };
    await writeFile(join(directory, 'results.json'), JSON.stringify(results, null, 2));
    console.log(`${name}/${mode}: native scoped content, preview lifetime, transfer failure/retry and reading-position checks passed`);
  } catch (error) {
    if (page) {
      await page.screenshot({ path: join(directory, `${name}-${mode}-failure.png`) }).catch(() => {});
      await writeFile(join(directory, `${name}-${mode}-failure.txt`), `${error.stack}\n${JSON.stringify(errors)}\n${await page.locator('body').innerText().catch(() => '')}`);
    }
    throw error;
  } finally {
    transfers?.close();
    try { await browser?.close(); } finally { await fixture?.close(); }
  }
}
