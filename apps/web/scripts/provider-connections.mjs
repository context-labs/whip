import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, startFixture } from './native-fixture.mjs';

// Real native routes and private key publication. All discovery is loopback;
// the environment route observes the fixture's disposable WHIPCODE_HOME only.
const results = process.env.WHIP_PROVIDER_RESULTS ?? '/tmp/whip-provider-browser-results';
await mkdir(results, { recursive: true });
const requests = [];
const upstream = createServer((request, response) => {
  requests.push({ path: request.url, authorization: request.headers.authorization });
  if (request.url !== '/v1/models' || request.headers.authorization !== 'Bearer fixture-saved-key') {
    response.writeHead(401).end(); return;
  }
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify({ data: [{ id: 'fixture-model', context_length: 8192 }] }));
});
await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve));
try {
  for (const engine of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
    const fixture = await startFixture({ lifetimeMs: 600_000 });
    let browser;
    try {
      browser = await ({ chromium, firefox }[engine]).launch();
      const page = await browser.newPage({ viewport: { width: 1280, height: 960 } });
      const client = await fixture.connect(`provider-browser-${engine}`);
      const errors = [], checks = [], frames = [];
      page.on('pageerror', error => errors.push(error.message));
      page.on('websocket', socket => socket.on('framesent', ({ payload }) => { frames.push(JSON.parse(String(payload))); }));
      await page.addInitScript(() => {
        window.cspErrors = [];
        document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective));
      });
      const select = async (scope, label, value) => {
        await scope.getByRole('combobox', { name: label, exact: true }).click();
        await page.getByRole('option', { name: value, exact: true }).click();
      };
      const advanced = async dialog => {
        await dialog.getByRole('button', { name: 'Connection options', exact: true }).click();
        await page.getByRole('menuitem', { name: 'Advanced configuration…', exact: true }).click();
      };
      try {
        const baseURL = `http://127.0.0.1:${upstream.address().port}/v1`;
        let inventory = await client.listProviders(deadline());
        assert.equal(inventory.routes.some(route => route.id === 'openrouter'), false, 'Presets must not auto-discover an environment route');
        const defaultSelection = inventory.defaults;
        const model = inventory.routes.find(route => route.id === 'provider').models.model;
        for (const [id, credential] of [['custom', { source: 'none', environment: '', file: '', command: null }], ['openrouter', { source: 'env', environment: 'WHIPCODE_HOME', file: '', command: null }]]) {
          inventory = await client.createProvider({ revision: inventory.revision, provider: id, declaration: { kind: 'openai-chat', base_url: baseURL, credential, models: { 'fixture-model': model } }, keep_credential: false, key: null }, deadline());
        }
        inventory = await client.setProviderDefaults({ revision: inventory.revision, defaults: { selection: { provider: 'custom', name: 'fixture-model', effort: '' }, settings: null } }, deadline());
        const selectedDefault = inventory.defaults;
        const { root } = await fixture.createRoot(client);
        const requestStart = requests.length;
        await page.goto(`${fixture.info.web}/h/${fixture.info.runtime_id}/s/${root.id}`);
        await page.locator('[data-whip-composer]').waitFor();
        await page.locator('#whip-settings-link').click();
        const categories = page.getByRole('navigation', { name: 'Settings categories', exact: true });
        await categories.getByRole('button', { name: 'Providers & models', exact: true }).click();
        await expect(page.getByRole('button', { name: 'Manage OpenRouter', exact: true })).toBeVisible();
        await expect(page.getByText('Environment', { exact: true })).toBeVisible();
        assert.equal(requests.length, requestStart, 'Opening Settings initiated provider discovery');
        checks.push('explicit environment route is visible; presets and opening Settings neither discover credentials nor perform provider network work');

        const custom = page.getByRole('button', { name: 'Manage custom', exact: true });
        await custom.focus(); await page.keyboard.press('Enter');
        const dialog = page.getByRole('dialog', { name: 'custom', exact: true });
        await advanced(dialog);
        await select(dialog, 'Credential source', 'Paste API key');
        await dialog.getByLabel('API key', { exact: true }).fill('draft-key');
        await page.keyboard.press('Escape');
        const discard = page.getByRole('dialog', { name: 'Discard this credential?', exact: true });
        await expect(discard).toBeVisible();
        await discard.getByRole('button', { name: 'Discard credential', exact: true }).click();
        await expect(dialog).toHaveCount(0); await expect(custom).toBeFocused();
        await custom.press('Enter'); await advanced(dialog); await select(dialog, 'Credential source', 'Paste API key');
        await expect(dialog.getByLabel('API key', { exact: true })).toHaveValue('');
        await dialog.getByLabel('API key', { exact: true }).fill('fixture-saved-key');
        await dialog.getByRole('button', { name: 'Save provider', exact: true }).click();
        await expect(dialog).toHaveCount(0);
        await expect(page.getByText('custom connected.', { exact: true })).toBeVisible();
        const saved = await client.listProviders(deadline());
        assert.deepEqual(saved.defaults, selectedDefault);
        const route = saved.routes.find(entry => entry.id === 'custom');
        assert.equal(route.base_url, baseURL); assert.equal(route.credential.source, 'file');
        assert.equal((await readFile(route.credential.file, 'utf8')).trim(), 'fixture-saved-key');
        assert.ok(!JSON.stringify(saved).includes('fixture-saved-key'), 'Inventory exposed the key');
        assert.equal(requests.length, requestStart, 'Custom Save pretended to validate inference');
        await custom.click(); await advanced(dialog);
        await dialog.getByRole('button', { name: 'Refresh model catalog', exact: true }).click();
        await expect(dialog.getByText('Catalog: catalog_response. Inference has not been tested.', { exact: true })).toBeVisible();
        assert.equal(requests.length, requestStart + 1); assert.equal(requests.at(-1).authorization, 'Bearer fixture-saved-key');
        assert.equal((await client.providerCatalog('custom', deadline())).models[0].id, 'fixture-model');
        checks.push('keyboard secret discard restores focus; explicit Save publishes a private key without changing defaults; explicit loopback discovery validates credentials separately');

        await dialog.getByRole('button', { name: 'Remove configured route', exact: true }).click();
        const remove = page.getByRole('dialog', { name: 'Remove this provider route?', exact: true });
        await remove.getByRole('button', { name: 'Remove route', exact: true }).click();
        await expect(remove.getByText('Could not remove provider route', { exact: true })).toBeVisible();
        assert.ok((await client.listProviders(deadline())).routes.some(route => route.id === 'custom'), 'Removal allowed a dangling default');
        await remove.getByRole('button', { name: 'Cancel', exact: true }).click();
        await dialog.getByRole('button', { name: 'Done', exact: true }).click();
        await page.getByRole('button', { name: 'Default model', exact: true }).click();
        await page.getByRole('option', { name: `${defaultSelection.name} · ${defaultSelection.provider}`, exact: true }).click();
        await page.getByRole('button', { name: 'Save host defaults', exact: true }).click();
        await expect(page.getByText('Host defaults saved.', { exact: true })).toBeVisible();
        assert.deepEqual((await client.listProviders(deadline())).defaults, defaultSelection);
        await custom.click(); await advanced(dialog); await dialog.getByRole('button', { name: 'Remove configured route', exact: true }).click();
        await remove.getByRole('button', { name: 'Remove route', exact: true }).click();
        await expect(page.getByText('Provider route removed. Credential files are unchanged.', { exact: true })).toBeVisible();
        assert.equal((await readFile(route.credential.file, 'utf8')).trim(), 'fixture-saved-key');
        await page.reload(); await expect(custom).toHaveCount(0);
        const current = await client.listProviders(deadline());
        assert.equal(current.routes.some(route => route.id === 'custom'), false);
        assert.equal(current.routes.find(route => route.id === 'openrouter').credential.environment, 'WHIPCODE_HOME');
        assert.ok((await client.providerCatalog('provider', deadline())).models.some(model => model.id === 'model'));
        checks.push('route removal rejects an in-use default; after explicit repair it survives reload, preserves key files and unrelated route/catalog evidence');

        await page.getByRole('button', { name: 'Manage OpenRouter', exact: true }).click();
        const environmentDialog = page.getByRole('dialog', { name: 'OpenRouter', exact: true });
        await expect(environmentDialog.getByText('WHIPCODE_HOME', { exact: true })).toBeVisible();
        await environmentDialog.getByRole('button', { name: 'Connection options', exact: true }).click();
        await page.getByRole('menuitem', { name: 'Disable on this host', exact: true }).click();
        await expect(environmentDialog).toHaveCount(0);
        assert.equal((await client.listProviders(deadline())).routes.find(route => route.id === 'openrouter').disabled, true);
        await page.getByRole('button', { name: 'Manage OpenRouter', exact: true }).click();
        await environmentDialog.getByRole('button', { name: 'Connection options', exact: true }).click();
        await page.getByRole('menuitem', { name: 'Enable on this host', exact: true }).click();
        await expect(environmentDialog).toHaveCount(0);
        const enabledRoute = (await client.listProviders(deadline())).routes.find(route => route.id === 'openrouter');
        assert.equal(enabledRoute.disabled, false); assert.equal(enabledRoute.credential.environment, 'WHIPCODE_HOME');
        await page.getByRole('button', { name: 'Manage OpenRouter', exact: true }).click();
        await advanced(environmentDialog);
        await environmentDialog.getByRole('button', { name: 'Remove configured route', exact: true }).click();
        await remove.getByRole('button', { name: 'Remove route', exact: true }).click();
        await page.getByRole('button', { name: 'Connect OpenRouter', exact: true }).click();
        await expect(environmentDialog.getByLabel('API key', { exact: true })).toBeFocused();
        await environmentDialog.getByRole('button', { name: 'Back', exact: true }).click();
        await advanced(environmentDialog);
        await environmentDialog.getByLabel('Endpoint', { exact: true }).fill(baseURL);
        await select(environmentDialog, 'Credential source', 'Environment variable');
        await environmentDialog.getByLabel('Environment variable', { exact: true }).fill('WHIPCODE_HOME');
        await environmentDialog.getByRole('button', { name: 'Save provider', exact: true }).click();
        await expect(environmentDialog).toHaveCount(0);
        await expect(page.getByText('OpenRouter connected.', { exact: true })).toBeVisible();
        assert.equal(requests.length, requestStart + 1, 'Environment editing initiated discovery');
        checks.push('environment route disable/enable persists reversible state without changing its source; advanced removal/recreation is explicit with no implicit discovery');

        for (const [label, theme, search] of [['light', 'light Light', 'light'], ['dark', 'Claude Code Dark', 'Claude']]) {
          await categories.getByRole('button', { name: 'Appearance', exact: true }).click();
          await page.getByRole('combobox', { name: /^Color theme:/ }).click();
          await page.getByRole('combobox', { name: 'Search themes', exact: true }).fill(search);
          await page.getByRole('option', { name: theme, exact: true }).click();
          await categories.getByRole('button', { name: 'Providers & models', exact: true }).click();
          await page.screenshot({ path: join(results, `${engine}-${label}.png`) });
        }
        await page.setViewportSize({ width: 390, height: 844 });
        await expect(page.getByRole('button', { name: 'Manage OpenRouter' })).toBeVisible();
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth), false);
        await page.screenshot({ path: join(results, `${engine}-narrow.png`) });
        const logo = page.locator('svg').filter({ has: page.locator('use[href$="#openrouter-dark"]') });
        assert.ok(await logo.evaluate(node => node.getBBox().width > 0));
        await page.setViewportSize({ width: 1280, height: 960 });
        await categories.getByRole('button', { name: 'Appearance', exact: true }).click();
        const fontSize = page.getByRole('textbox', { name: 'UI font size', exact: true });
        await fontSize.fill('20'); await fontSize.press('Tab');
        await categories.getByRole('button', { name: 'Providers & models', exact: true }).click();
        await page.getByRole('button', { name: 'Manage OpenRouter' }).click();
        await advanced(environmentDialog);
        await expect(environmentDialog).toHaveCSS('font-size', '20px');
        await page.setViewportSize({ width: 320, height: 640 });
        assert.equal(await environmentDialog.evaluate(element => element.scrollWidth > element.clientWidth), false, 'Provider dialog overflows at the largest text size');
        await page.screenshot({ path: join(results, `${engine}-dialog-narrow-large-text.png`) });
        checks.push('provider dialog typography, bundled SVGs, light/dark themes and narrow layout render under production CSP');
        assert.deepEqual(errors, []); assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
        assert.equal(frames.some(frame => frame.method?.startsWith('accounts.') && !/\.(list|status|cleanup)$/.test(frame.method)), false, 'Inspection initiated an account effect');
        await writeFile(join(results, `${engine}.json`), JSON.stringify({ checks, errors, frameCount: frames.length }, null, 2));
        console.log(`${engine}: ${checks.length} native provider workflow checks passed`);
      } catch (error) {
        await page.screenshot({ path: join(results, `${engine}-failure.png`) });
        await writeFile(join(results, `${engine}-failure.txt`), `${error.stack}\n${await page.locator('body').innerText()}`);
        throw error;
      }
    } finally { try { await browser?.close(); } finally { await fixture.close(); } }
  }
} finally { upstream.closeAllConnections(); await new Promise(resolve => upstream.close(resolve)); }
