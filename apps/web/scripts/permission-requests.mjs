import assert from 'node:assert/strict';
import { createHash, randomUUID } from 'node:crypto';
import { access, mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, repository, startFixture } from './native-fixture.mjs';

// Actual concurrent root operations, SQL permission decisions and production UI.
// Child delegation and standing grants are distinct from one-operation consent.
const directory = process.env.WHIP_PERMISSION_RESULTS ?? '/tmp/whip-permission-request-results';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
const manifest = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
await mkdir(directory, { recursive: true });
const reports = [];
for (const name of names) {
  let fixture, browser, page;
  const report = { name, rendererDigest: manifest.digest, checks: [] }, errors = [], csp = [];
  reports.push(report);
  try {
    fixture = await startFixture({ executeCode: true, lifetimeMs: 600000 });
    const client = await fixture.connect(`native-permissions-${name}`);
    const foreign = await fixture.createRoot(client, { title: 'Unrelated permission owner' });
    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage(); page.setDefaultTimeout(15000);
    page.on('pageerror', error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); });
    await page.exposeFunction('__permissionCSP', value => { if (csp.length < 64) csp.push(String(value).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__permissionCSP(event.violatedDirective).catch(() => {}); }));
    for (const [path, file] of Object.entries(manifest.files)) {
      const response = await fetch(`${fixture.info.web}/${path}`); assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256);
    }
    await page.goto(fixture.info.web);
    for (const scenario of [
      { name: 'desktop', width: 1440, height: 900 },
      { name: 'phone', width: 390, height: 844 },
      { name: 'narrow-long', width: 320, height: 640, long: true },
      { name: 'short', width: 640, height: 320, long: true },
    ]) for (const theme of ['light', 'dark', 'claude-code']) {
      const { root } = await fixture.createRoot(client, { engine: 'quickjs', title: 'Native permission layout' });
      const session = client.session(root.id), request = randomUUID();
      const files = [`permission-${randomUUID()}.txt`, `permission-${randomUUID()}.txt`];
      const contents = scenario.long ? 'Long exact operation arguments remain bounded and selectable.\n'.repeat(100) : 'Isolated approved write.';
      const code = `await Promise.allSettled(${JSON.stringify(files)}.map(path => files.write({path,content:${JSON.stringify(contents)}})));`;
      await session.submit([{ type: 'text', text: '```js\n' + code + '\n```' }], request, deadline());
      const permissions = await eventually(async () => {
        const result = await session.permissions.list({ pending_only: true, limit: 2 }, deadline());
        return result.items.length === 2 && (await session.activity(deadline())).pending_permission_count === '2' ? result.items : false;
      }, { description: 'two actual concurrent permission waits' });
      const first = await session.operations.get(permissions[0].operation_id, deadline());
      const second = await session.operations.get(permissions[1].operation_id, deadline());
      assert.equal(first.session_id, root.id); assert.equal(second.session_id, root.id);
      await assert.rejects(client.session(foreign.root.id).permissions.resolve(first.id, true, deadline()), /another session/);
      assert.equal((await session.permissions.list({ pending_only: true }, deadline())).items.length, 2, 'Foreign handle must not decide either request');
      for (const file of files) await assert.rejects(access(join(fixture.directory, file)), { code: 'ENOENT' });
      await page.evaluate(theme => {
        localStorage.clear(); sessionStorage.clear();
        localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: theme }));
      }, theme);
      await page.setViewportSize({ width: scenario.width, height: scenario.height });
      await page.goto(`${fixture.info.web}/h/${client.runtimeID}/s/${root.id}`);
      const card = page.getByRole('region', { name: 'Your approval is needed' });
      await expect(card).toHaveCount(1); await expect(card.getByRole('status')).toHaveText('1 more waiting');
      await expect(card).toContainText(root.id);
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      await page.evaluate(() => document.fonts.ready);
      assert.ok((await card.getByText('files.write', { exact: true }).boundingBox()).height < 24, 'Exact owner must not squeeze the capability into a vertical column');
      await page.getByRole('textbox', { name: 'Message WHIP' }).waitFor();
      const geometry = await card.evaluate(element => {
        const card = element.getBoundingClientRect(), dock = element.closest('[aria-label="Needs your attention"]').getBoundingClientRect();
        const composer = document.querySelector('[data-whip-composer]').parentElement.getBoundingClientRect();
        return { left: card.left, right: card.right, width: card.width, bottom: Math.min(card.bottom, dock.bottom),
          composerLeft: composer.left, composerRight: composer.right, composerTop: composer.top,
          documentWidth: document.documentElement.scrollWidth, viewportWidth: innerWidth };
      });
      assert.ok(geometry.left >= 12 && geometry.right <= scenario.width - 12, JSON.stringify(geometry));
      assert.ok(geometry.width <= 816 && Math.abs(geometry.left - geometry.composerLeft) < 1 && Math.abs(geometry.right - geometry.composerRight) < 1, JSON.stringify(geometry));
      // Subpixel DOMRect conversion may put a shared edge 0.000015px apart.
      assert.ok(geometry.bottom <= geometry.composerTop + .01 && geometry.composerTop - geometry.bottom <= 9, JSON.stringify(geometry));
      assert.ok(geometry.documentWidth <= geometry.viewportWidth);
      const allow = card.getByRole('button', { name: 'Allow once', exact: true });
      if (scenario.width <= 767) assert.ok((await allow.boundingBox()).height >= 44 - .01);
      await allow.scrollIntoViewIfNeeded(); await expect(allow).toBeInViewport();
      await card.getByText('Exact operation arguments', { exact: true }).click();
      await expect(card.getByLabel('Requested operation', { exact: true })).toContainText(first.arguments.path);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await card.getByText('Exact operation arguments', { exact: true }).click();
      await page.screenshot({ path: join(directory, `${name}-${scenario.name}-${theme}.png`), fullPage: true });
      await card.getByRole('button', { name: 'Deny', exact: true }).focus(); await page.keyboard.press('Enter');
      await eventually(async () => (await session.permissions.list({ limit: 8 }, deadline())).items.find(item => item.operation_id === first.id)?.state === 'denied');
      await expect(card).toHaveCount(1);
      await card.getByText('Exact operation arguments', { exact: true }).click();
      await expect(card.getByLabel('Requested operation', { exact: true })).toContainText(second.arguments.path);
      await card.getByRole('button', { name: 'Allow once', exact: true }).click(); await expect(card).toHaveCount(0);
      await client.wait(request, deadline());
      await assert.rejects(access(join(fixture.directory, first.arguments.path)), { code: 'ENOENT' });
      assert.equal(await readFile(join(fixture.directory, second.arguments.path), 'utf8'), contents);
      const grants = await client.call('grants.list', { session_id: root.id, limit: 32 }, deadline());
      assert.equal(grants.items.filter(grant => grant.capability === 'files.write' && grant.operation_id === null).length, 0, 'Allow once must not create standing authority');
      report.checks.push({ scenario: scenario.name, theme, owner: root.id, denied: first.id, approved: second.id, geometry });
      console.log(`${name}: ${scenario.name}/${theme} passed`);
    }
    assert.deepEqual(errors, []); assert.deepEqual(csp, []);
    Object.assign(report, { browser: browser.version(), errors, csp, passed: true });
  } catch (error) {
    report.error = String(error.stack ?? error); report.errors = errors; report.csp = csp;
    if (page) {
      await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
      report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16000);
    }
    throw error;
  } finally {
    try { await browser?.close(); }
    finally { try { await fixture?.close(); } finally { await writeFile(join(directory, 'report.json'), JSON.stringify(reports, null, 2)); } }
  }
}
console.log(`Passed ${reports.reduce((count, report) => count + report.checks.length, 0)} native permission layouts. Artifacts: ${directory}`);
