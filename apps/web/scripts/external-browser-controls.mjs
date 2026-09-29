import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdir, stat, writeFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

const directory = process.env.WHIP_EXTERNAL_BROWSER_RESULTS ?? '/tmp/whip-external-browser-results';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
await mkdir(directory, { recursive: true });
const reports = [];
for (const name of names) {
  let fixture, browser, page;
  const report = { name, checks: [] }, errors = [], csp = []; reports.push(report);
  try {
    fixture = await startFixture({ lifetimeMs: 180000 });
    const client = await fixture.connect(), { root } = await fixture.createRoot(client);
    browser = await ({ chromium, firefox }[name]).launch(); page = await browser.newPage({ viewport: { width: 1280, height: 900 } }); page.setDefaultTimeout(15000);
    page.on('pageerror', error => { if (errors.length < 32) errors.push(String(error.stack ?? error).slice(0, 4096)); });
    await page.exposeFunction('__browserCSP', value => { if (csp.length < 32) csp.push(String(value).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__browserCSP(event.violatedDirective).catch(() => {}); }));
    await page.goto(`${fixture.info.web}/settings?section=execution`);
    await page.getByRole('combobox', { name: 'External browser mode', exact: true }).click();
    await page.getByRole('option', { name: 'Headless Chrome', exact: true }).click();
    await page.getByLabel('Chrome executable', { exact: true }).fill('/fixture/not-launched');
    await page.getByRole('button', { name: 'Save external Chrome settings', exact: true }).click();
    await expect(page.getByText('External Chrome settings saved. The next browser request still needs permission.')).toBeVisible();
    let saved = await client.hosts.externalBrowser(deadline()); assert.equal(saved.configuration.mode, 'headless'); assert.equal(saved.configuration.executable, '/fixture/not-launched');
    await assert.rejects(stat(join(dirname(fixture.info.socket), 'browser')), { code: 'ENOENT' });
    report.checks.push('actual host mode CAS persists complete configuration without launching or opening a browser');
    await page.getByLabel('Chrome executable', { exact: true }).fill('/fixture/stale-draft');
    await client.hosts.setExternalBrowser(saved.revision, { ...saved.configuration, allow_private_urls: true }, deadline());
    await page.getByRole('button', { name: 'Save external Chrome settings', exact: true }).click();
    await expect(page.getByText(/Your previous request may have arrived/)).toBeVisible();
    assert.equal((await client.hosts.externalBrowser(deadline())).configuration.executable, '/fixture/not-launched');
    await expect(page.getByLabel('Chrome executable', { exact: true })).toHaveValue('/fixture/stale-draft');
    await page.getByRole('button', { name: 'Discard edits and read external Chrome settings', exact: true }).click();
    await expect(page.getByLabel('Chrome executable', { exact: true })).toHaveValue('/fixture/not-launched');
    report.checks.push('other-client conflict preserves the draft and never rebases or replays the edit');
    const requestID = randomUUID();
    await client.callTool(root.id, { module: 'browser', name: 'run', arguments_base64: Buffer.from(JSON.stringify({ session: 'default', code: 'info()' })).toString('base64') }, requestID, deadline());
    const permission = await eventually(async () => (await client.session(root.id).permissions.list({ pending_only: true }, deadline())).items[0]);
    const operation = await client.session(root.id).operations.get(permission.operation_id, deadline()); assert.equal(operation.capability, 'browser.external');
    const original = (await client.hosts.externalBrowserSessions(root.id, deadline())).items[0]; assert.equal(original.state, 'prepared');
    const open = async child => {
      await page.goto(`${fixture.info.web}/h/${client.runtimeID}/s/${root.id}${child ? `?agent=${child}` : ''}`);
      await page.locator('[data-session-info-bar]').getByRole('button', { name: 'Session actions', exact: true }).click();
      await page.getByRole('menuitem', { name: 'Session details', exact: true }).click();
      const details = page.getByRole('dialog', { name: 'Session details', exact: true });
      await details.getByRole('combobox', { name: 'Inspector section', exact: true }).click(); await page.getByRole('option', { name: 'Host integrations', exact: true }).click();
      await details.getByRole('combobox', { name: 'Integration', exact: true }).click(); await page.getByRole('option', { name: 'Browser automation', exact: true }).click();
      return details;
    };
    let details = await open();
    await details.getByText('Exact browser authority', { exact: true }).click(); await expect(details.getByText(original.generation, { exact: true })).toBeVisible();
    await details.getByRole('button', { name: 'Reconnect default', exact: true }).click();
    const fresh = await eventually(async () => { const value = (await client.hosts.externalBrowserSessions(root.id, deadline())).items[0]; return value.generation !== original.generation && value; });
    assert.equal(fresh.state, 'prepared'); assert.notEqual(fresh.resource, original.resource);
    await client.wait(requestID, deadline()); await assert.rejects(stat(join(dirname(fixture.info.socket), 'browser')), { code: 'ENOENT' });
    await details.getByRole('button', { name: 'Disconnect default', exact: true }).click();
    await eventually(async () => (await client.hosts.externalBrowserSessions(root.id, deadline())).items[0].state === 'ended');
    report.checks.push('root UI shows exact resource/generation; reconnect cancels old pending control without dispatch or replay, disconnect retires new generation');
    const spawnID = randomUUID(), child = await client.session(root.id).spawn({ overrides: { report_mode: 'notice' }, parts: [{ type: 'text', text: 'Child inspection' }], budgets: [{ kind: 'model_calls', limit: '4' }], grant_ids: [] }, spawnID, deadline());
    await client.wait(spawnID, deadline()); assert(child.session);
    details = await open(child.session.id); await expect(details.getByText(/Open the root agent to reconnect/)).toBeVisible();
    await expect(details.getByRole('button', { name: 'Reconnect default', exact: true })).toHaveCount(0);
    await expect(details.getByRole('button', { name: 'Disconnect default', exact: true })).toHaveCount(0);
    report.checks.push('child inspector exposes root metadata without connection actions or implicit grants');
    await page.screenshot({ path: join(directory, `${name}.png`), fullPage: true }); assert.deepEqual(errors, []); assert.deepEqual(csp, []);
    Object.assign(report, { passed: true, browser: browser.version(), errors, csp }); console.log(`${name}: ${report.checks.length} native external-browser control workflows passed`);
  } catch (error) { Object.assign(report, { error: String(error.stack ?? error), errors, csp }); if (page) report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16000); throw error; }
  finally { try { await browser?.close(); } finally { try { await fixture?.close(); } finally { await writeFile(join(directory, 'report.json'), JSON.stringify(reports, null, 2)); } } }
}
