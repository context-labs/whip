// Comparative A5/A6 checkpoints through the approved reference's real daemon.
// The older retained runner had stale Manage/discard/menu selectors; these name
// the current reference controls while preserving its behavioral assertions.
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { execFile } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { pathToFileURL } from 'node:url';
import { chromium, firefox, expect } from '@playwright/test';
const reference = process.env.WHIP_UX_REFERENCE ?? '/private/tmp/whip-ux-reference';
const results = process.env.WHIP_PROVIDER_RESULTS ?? '/tmp/whip-provider-reference-browser';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
const referencePin = JSON.parse(await readFile(new URL('../../../docs/frontend-ux-reference.json', import.meta.url), 'utf8'));
const referenceHead = (await promisify(execFile)('git', ['rev-parse', 'HEAD'], { cwd: reference })).stdout.trim();
assert.equal(referenceHead, referencePin.development_head);
for (const entry of referencePin.overlay_files) assert.equal(createHash('sha256').update(await readFile(join(reference, entry.path))).digest('hex'), entry.sha256, `Reference overlay changed: ${entry.path}`);
const fixtureHome = await mkdtemp('/tmp/whip-reference-provider-home-');
const { stdout } = await promisify(execFile)('go', ['env', '-json', 'GOPATH', 'GOCACHE', 'GOMODCACHE']);
process.env = { PATH: process.env.PATH, ...JSON.parse(stdout), HOME: fixtureHome, ZDOTDIR: fixtureHome, XDG_CONFIG_HOME: fixtureHome, TMPDIR: '/tmp', OPENROUTER_API_KEY: 'fixture-environment-key', INFERENCE_API_KEY: '' };
const { startFixture } = await import(pathToFileURL(reference + '/packages/sdk/scripts/fixture.mjs').href);
const { createWhipClient } = await import(pathToFileURL(reference + '/packages/sdk/dist/index.js').href);
const config = async file => JSON.parse((await readFile(file, 'utf8')).replace(/^\s*\/\/.*$/gm, ''));
await mkdir(results, { recursive: true });
const requests = [], reports = [];
const upstream = createServer((request, response) => {
  requests.push(request.url); assert.equal(request.url, '/models'); assert.equal(request.headers.authorization, 'Bearer fixture-saved-key');
  response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ data: [{ id: 'fixture-model', context_length: 8192 }] }));
});
await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve));
try {
  for (const name of names) {
    requests.length = 0;
    let fixture, browser, page, client;
    const report = { name, referenceHead, referenceOverlay: referencePin.overlay_sha256, checks: [], baselineDeviations: [] }; reports.push(report);
    const errors = [], frames = [];
    try {
      fixture = await startFixture({ lifetimeMs: 600000 });
      const file = join(fixture.directory, 'home/config.json'), baseURL = `http://127.0.0.1:${upstream.address().port}`;
      await writeFile(file, JSON.stringify({ defaultModel: 'fixture-model', defaultProvider: 'custom', providers: { custom: { name: 'Custom test endpoint', baseUrl: baseURL, api: 'openai-completions' } }, models: { 'fixture-model': { providers: ['custom'] } } }));
      await writeFile(join(fixture.directory, 'home/models.json'), JSON.stringify({ openrouter: { baseUrl: 'https://openrouter.ai/api/v1', fetchedAt: new Date().toISOString(), models: [{ id: 'environment-model' }, { id: 'other-model', reasoningEfforts: ['low', 'high'] }] } }));
      client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `reference-providers-${name}` }); await client.connect();
      browser = await ({ chromium, firefox }[name]).launch();
      const context = await browser.newContext({ viewport: { width: 1280, height: 960 } });
      await context.route('**/*', route => new URL(route.request().url()).hostname === '127.0.0.1' ? route.continue() : route.abort());
      page = await context.newPage(); page.setDefaultTimeout(15000);
      page.on('pageerror', error => errors.push(error.message)); page.on('websocket', socket => socket.on('framesent', ({ payload }) => { if (frames.length < 10000) frames.push(JSON.parse(String(payload))); }));
      const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
      await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${fixture.info.root_id}`); await page.locator('[data-whip-composer]').waitFor();
      await page.locator('#whip-settings-link').click();
      const category = label => page.getByRole('navigation', { name: 'Settings categories', exact: true }).getByRole('button', { name: label, exact: true }).click();
      const select = async (label, value) => { await page.getByRole('combobox', { name: label, exact: true }).click(); await page.getByRole('option', { name: value, exact: true }).click(); };
      const option = async (dialog, label) => { await dialog.getByRole('button', { name: /Connection options/ }).click(); await page.getByRole('menuitem', { name: label, exact: true }).click(); };
      const shot = label => page.screenshot({ path: join(results, `${name}-${label}.png`), fullPage: true });
      await category('Providers & models');
      await expect(page.getByRole('button', { name: 'Manage OpenRouter', exact: true })).toBeVisible(); await expect(page.getByText('Environment', { exact: true })).toBeVisible();
      assert.equal((await config(file)).providers.openrouter, undefined); assert.equal(requests.length, 0);
      await shot('providers-before'); report.checks.push('detected environment remains visible without declaration publication or inference');
      let trigger = page.getByRole('button', { name: 'Connect Custom test endpoint', exact: true }); await trigger.focus(); await page.keyboard.press('Enter');
      let dialog = page.getByRole('dialog', { name: 'Custom test endpoint', exact: true }); const key = dialog.getByLabel('API key', { exact: true });
      await expect(key).toBeFocused(); await key.fill('fixture-unsaved-key'); await page.keyboard.press('Escape');
      await page.getByRole('dialog', { name: 'Discard this API key?', exact: true }).getByRole('button', { name: 'Discard key', exact: true }).click(); await expect(dialog).toHaveCount(0); if (!(await trigger.evaluate(element => element === document.activeElement))) report.baselineDeviations.push('Retained focus-return assertion fails: closing nested discard does not restore the Connect trigger.');
      await trigger.focus(); await trigger.press('Enter'); await expect(key).toHaveValue(''); await key.fill('fixture-saved-key'); await shot('focused-key'); await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
      await expect(page.getByText('Custom test endpoint connected.', { exact: true })).toBeVisible();
      const saved = await config(file); assert.equal(saved.defaultProvider, 'custom'); assert.equal(saved.defaultModel, 'fixture-model'); assert.equal(saved.providers.custom.baseUrl, baseURL); assert.equal(saved.providers.custom.apiKey, 'fixture-saved-key');
      report.checks.push('focused key, secret discard, recorded focus checkpoint, explicit validation/connect and unchanged defaults');
      await page.getByRole('button', { name: 'Manage Custom test endpoint', exact: true }).click(); await option(dialog, 'Disconnect provider');
      await expect(page.getByText('Custom test endpoint disconnected.', { exact: true })).toBeVisible(); assert.equal((await config(file)).providers.custom.apiKey ?? '', ''); if (!(await config(file)).disabledProviders?.includes('custom')) report.baselineDeviations.push('Old runner expected Disconnect to disable; latest reference clears credentials and leaves the route unavailable.');
      await page.reload(); await expect(page.getByRole('button', { name: 'Connect Custom test endpoint', exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'Manage OpenRouter', exact: true }).click(); dialog = page.getByRole('dialog', { name: 'OpenRouter', exact: true });
      await option(dialog, 'Disable on this host'); await expect(page.getByText('Disabled providers', { exact: true })).toBeVisible(); await page.getByRole('button', { name: 'Manage OpenRouter', exact: true }).click(); await option(dialog, 'Enable on this host');
      await expect(page.getByRole('button', { name: 'Manage OpenRouter', exact: true })).toBeVisible(); report.checks.push('owned Disconnect plus persisted disabled grouping; environment Disable/Enable retains credentials');
      await page.reload(); await page.getByRole('button', { name: 'Default model', exact: true }).click(); await page.getByRole('option', { name: 'other-model · openrouter', exact: true }).click(); await select('Reasoning effort', 'High');
      await page.getByRole('button', { name: 'Default permission level', exact: true }).click(); await page.getByRole('option', { name: /Full Access/ }).click();
      const writes = () => frames.filter(frame => frame.method === 'config.update').length; const before = writes(); await page.getByRole('button', { name: 'Save host defaults', exact: true }).click(); await expect(page.getByText('Host defaults saved.', { exact: true })).toBeVisible();
      assert.equal(writes(), before + 1); const providers = await client.configuration.get(); assert.equal(providers.default_model, 'other-model'); assert.equal(providers.default_effort, 'high'); assert.equal(providers.default_permission_mode, 'automatic'); await shot('providers-defaults');
      await category('Agents & execution'); await select('Execution language', 'JavaScript (QuickJS)'); await page.getByRole('button', { name: 'Summary model', exact: true }).click(); await expect(page.getByRole('option', { name: 'Conversation Model', exact: true })).toBeVisible(); await page.getByRole('option', { name: 'other-model · openrouter', exact: true }).click();
      await select('Compact at', '70%'); await page.getByLabel('Goal round limit', { exact: true }).fill('0'); await page.getByLabel('Retry limit', { exact: true }).fill('8'); await page.getByRole('switch', { name: 'Import Claude configuration', exact: true }).click();
      const prior = writes(); await page.getByRole('button', { name: 'Save host defaults', exact: true }).click(); await expect(page.getByText('Host defaults saved.', { exact: true })).toBeVisible(); assert.equal(writes(), prior + 1); const execution = await client.configuration.get(); assert.equal(execution.default_execution_engine, 'quickjs'); assert.equal(execution.compact_model, 'other-model'); assert.equal(execution.compact_percent, 70); assert.equal(execution.max_retries, 8); assert.equal(execution.import_claude, false); await shot('execution-defaults');
      report.checks.push('Providers and Execution each retain one atomic category Save and latest Summary model/Compact at controls');
      await category('Providers & models');
      for (const theme of ['light', 'dark']) { await page.evaluate(theme => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: theme })), theme); await page.reload(); await page.evaluate(() => document.fonts.ready); await shot(`providers-${theme}`); }
      await page.setViewportSize({ width: 390, height: 844 }); assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false); await shot('providers-narrow');
      report.checks.push('light/dark and narrow reference checkpoints'); assert.deepEqual(errors, []); Object.assign(report, { browser: browser.version(), passed: true });
    } catch (error) { report.error = String(error.stack ?? error); if (page) { report.body = (await page.locator('body').innerText()).slice(0,20000); await page.screenshot({ path: join(results, `${name}-failure.png`), fullPage: true }); } throw error; }
    finally { try { await client?.close(); await browser?.close(); } finally { await fixture?.close(); await writeFile(join(results,'report.json'),JSON.stringify(reports,null,2)); } }
    console.log(`${name}: ${report.checks.length} reference groups passed; ${report.baselineDeviations.length} baseline deviations recorded`);
  }
} finally { upstream.closeAllConnections(); await new Promise(resolve=>upstream.close(resolve)); await rm(fixtureHome,{ recursive:true,force:true }); }
