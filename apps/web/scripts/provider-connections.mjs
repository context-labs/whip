import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { startFixture } from '../../../packages/sdk/scripts/fixture.mjs';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';

// Synthetic credentials only. The provider API below runs entirely on loopback.
process.env.INFERENCE_API_KEY = '';
process.env.OPENROUTER_API_KEY = 'fixture-environment-key';
const results = process.env.WHIP_PROVIDER_RESULTS ?? '/tmp/whip-provider-browser-results';
const readConfig = async filename => JSON.parse((await readFile(filename, 'utf8')).replace(/^\s*\/\/.*$/gm, ''));
await mkdir(results, { recursive: true });
const upstream = createServer((request, response) => {
  assert.equal(request.url, '/models');
  assert.equal(request.headers.authorization, 'Bearer fixture-saved-key');
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify({ data: [{ id: 'fixture-model', context_length: 8192 }] }));
});
await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve));
try {
  for (const engine of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
    const fixture = await startFixture({ lifetimeMs: 600_000 });
    const browser = await ({ chromium, firefox }[engine]).launch();
    const page = await browser.newPage({ viewport: { width: 1280, height: 960 } });
    const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `provider-browser-${engine}` });
    const errors = [], checks = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(() => {
      window.cspErrors = [];
      document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective));
    });
    const configFile = join(fixture.directory, 'home', 'config.json');
    try {
      const baseURL = `http://127.0.0.1:${upstream.address().port}`;
      const config = { defaultModel: 'fixture-model', defaultProvider: 'custom', providers: { custom: { name: 'Custom test endpoint', baseUrl: baseURL, api: 'openai-completions' } }, models: { 'fixture-model': { providers: ['custom'] } } };
      await writeFile(configFile, JSON.stringify(config));
      await writeFile(join(fixture.directory, 'home', 'models.json'), JSON.stringify({ openrouter: { baseUrl: 'https://openrouter.ai/api/v1', fetchedAt: new Date().toISOString(), models: [{ id: 'environment-model' }] } }));
      await client.connect();
      const inventory = await client.providers.list();
      assert.equal(inventory.providers.find(entry => entry.id === 'openrouter').status.key_source, 'environment');
      assert.equal((await readConfig(configFile)).providers.openrouter, undefined);
      const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
      await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${fixture.info.root_id}`);
      await page.locator('[data-whip-composer]').waitFor();
      await page.locator('#whip-settings-link').click();
      await page.getByRole('navigation', { name: 'Settings categories', exact: true }).getByRole('button', { name: 'Providers & models', exact: true }).click();
      await expect(page.getByRole('button', { name: 'Manage OpenRouter' })).toBeVisible();
      await expect(page.getByText('Environment', { exact: true })).toBeVisible();
      await expect(page.getByText(/Your default model is unchanged/)).toBeVisible();
      checks.push('environment discovery is visible without writing provider entries; missing default offers explicit repair');

      const custom = page.getByRole('button', { name: 'Manage Custom test endpoint', exact: true });
      await custom.focus(); await page.keyboard.press('Enter');
      const dialog = page.getByRole('dialog', { name: 'Custom test endpoint', exact: true });
      await expect(dialog).toBeVisible();
      await dialog.getByLabel('API key', { exact: true }).fill('draft-key');
      await page.keyboard.press('Escape');
      await expect(dialog.getByText('Discard the unsaved API key?')).toBeVisible();
      await dialog.getByRole('button', { name: 'Discard key', exact: true }).click();
      await expect(dialog).toHaveCount(0);
      await expect(custom).toBeFocused();
      await custom.press('Enter');
      await expect(dialog.getByLabel('API key', { exact: true })).toHaveValue('');
      await dialog.getByLabel('API key', { exact: true }).fill('fixture-saved-key');
      await dialog.getByRole('button', { name: 'Connect', exact: true }).click();
      await expect(page.getByText('Custom test endpoint connected.', { exact: true })).toBeVisible();
      const saved = await readConfig(configFile);
      assert.equal(saved.defaultModel, config.defaultModel); assert.equal(saved.defaultProvider, config.defaultProvider);
      assert.equal(saved.providers.custom.baseUrl, baseURL); assert.equal(saved.providers.custom.apiKey, 'fixture-saved-key');
      assert.equal(saved.providers.openrouter, undefined);
      checks.push('keyboard connect, dirty-secret discard, focus return and validation/save work without changing defaults or endpoint');

      await custom.click();
      await dialog.getByRole('button', { name: 'Disconnect provider', exact: true }).click();
      await expect(page.getByText('Custom test endpoint disconnected.', { exact: true })).toBeVisible();
      const disconnected = await readConfig(configFile);
      assert.equal(disconnected.providers.custom.apiKey ?? '', '');
      assert.ok(disconnected.disabledProviders.includes('custom'));
      const catalog = (await client.providers.catalogs()).result;
      assert.equal(catalog.providers.custom.available, false);
      assert.equal(catalog.catalogs.custom, undefined);
      assert.ok(catalog.catalogs.openrouter.models.some(model => model.id === 'environment-model'));
      await page.reload();
      await expect(page.getByText('Disabled', { exact: true })).toBeVisible();
      checks.push('disconnect survives reload, removes its cached models, preserves other environment models and shows default repair');

      await page.getByRole('button', { name: 'Manage OpenRouter' }).click();
      const environmentDialog = page.getByRole('dialog', { name: 'OpenRouter', exact: true });
      await expect(environmentDialog.getByText('OPENROUTER_API_KEY')).toBeVisible();
      await expect(environmentDialog.getByRole('button', { name: 'Disconnect provider', exact: true })).toHaveCount(0);
      await environmentDialog.getByRole('button', { name: 'Disable on this host', exact: true }).click();
      await expect(page.getByText('OpenRouter disabled on this host.', { exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'Connect OpenRouter', exact: true }).click();
      await environmentDialog.getByRole('button', { name: 'Enable on this host', exact: true }).click();
      await expect(page.getByRole('button', { name: 'Manage OpenRouter' })).toBeVisible();
      checks.push('environment connections disable and re-enable without credential deletion');

      const categories = page.getByRole('navigation', { name: 'Settings categories', exact: true });
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
      const logo = page.locator('svg').filter({ has: page.locator('use[href$="#openrouter"]') });
      assert.ok(await logo.evaluate(node => node.getBBox().width > 0));
      assert.deepEqual(errors, []); assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
      checks.push('bundled SVGs, light/dark themes and narrow layout render under production CSP without browser errors');
      await writeFile(join(results, `${engine}.json`), JSON.stringify({ checks, errors }, null, 2));
      console.log(`${engine}: ${checks.length} provider workflow checks passed`);
    } catch (error) {
      await page.screenshot({ path: join(results, `${engine}-failure.png`) });
      await writeFile(join(results, `${engine}-failure.txt`), await page.locator('body').innerText());
      throw error;
    } finally { await client.close(); await browser.close(); await fixture.close(); }
  }
} finally { await new Promise(resolve => upstream.close(resolve)); }
