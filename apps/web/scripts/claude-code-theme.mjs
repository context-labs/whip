import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdir, readFile } from 'node:fs/promises';
import { chromium, firefox, expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

const output = '/tmp/whip-claude-code-theme-results';
await mkdir(output, { recursive: true });
for (const [name, launcher] of Object.entries({ chromium, firefox })) {
  const fixture = await startFixture();
  const browser = await launcher.launch();
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: crypto.randomUUID(), clientKind: 'human' });
  let page;
  try {
    await client.connect();
    const roots = [];
    for (const title of ['Claude Code SDK process communication', 'Prime Agent and WHIP comparison', 'Review the runtime architecture']) {
      const result = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
      roots.push(result.result.root_id);
      await client.session(roots.at(-1)).rename(title).result();
    }
    const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
    page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(() => {
      window.__themeCsp = [];
      document.addEventListener('securitypolicyviolation', event => window.__themeCsp.push(event.violatedDirective));
    });
    await page.goto(`${origin}/settings`);
    const chooseTheme = async (current, label) => {
      await page.getByRole('button', { name: current, exact: true }).click();
      const dialog = page.getByRole('dialog', { name: 'Appearance', exact: true });
      const search = dialog.getByRole('combobox', { name: 'Search themes' });
      await search.fill(label);
      await search.press('Enter');
      await dialog.getByRole('button', { name: 'Done', exact: true }).click();
    };
    await chooseTheme('System appearance', 'Claude Code');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'claude-code');
    await page.reload();
    await expect(page.getByRole('button', { name: 'Claude Code', exact: true })).toBeVisible();
    const appearance = async () => page.locator('#whip-session-navigation').evaluate(element => ({
      background: getComputedStyle(document.documentElement).backgroundColor,
      foreground: getComputedStyle(document.documentElement).color,
      navigation: getComputedStyle(element).backgroundColor,
      border: getComputedStyle(element).borderRightColor,
    }));
    assert.deepEqual(await appearance(), {background: 'rgb(20, 20, 20)', foreground: 'rgb(194, 192, 184)', navigation: 'rgb(17, 17, 16)', border: 'rgb(28, 28, 27)'});
    await page.screenshot({ path: `${output}/${name}-settings.png` });
    await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${roots[0]}`);
    await page.getByLabel('Message WHIP', { exact: true }).fill('Explain how the SDK communicates with the Claude Code process.');
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(page.locator('[data-user-bubble]').first()).toBeVisible();
    const selected = page.locator(`[data-sidebar-session="${roots[0]}"] > div`);
    const other = page.locator(`[data-sidebar-session="${roots[1]}"] > div`);
    await expect(selected).toHaveCSS('background-color', 'rgb(52, 52, 52)');
    await other.hover();
    await expect(other).toHaveCSS('background-color', 'rgb(34, 34, 33)');
    await page.mouse.move(800, 30);
    await page.screenshot({ path: `${output}/${name}-conversation.png` });
    const axeSource = await readFile(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
    await page.route(`${origin}/__theme-axe.js`, route => route.fulfill({ contentType: 'text/javascript', body: axeSource }));
    await page.addScriptTag({ url: `${origin}/__theme-axe.js` });
    const checkContrast = async () => {
      const violations = await page.evaluate(async () => (await axe.run(document.body, {runOnly: {type: 'rule', values: ['color-contrast']}})).violations.map(item => ({id: item.id, nodes: item.nodes.map(node => ({target: node.target, summary: node.failureSummary}))})));
      assert.deepEqual(violations, []);
    };
    await checkContrast();
    await page.getByRole('button', { name: 'Search sessions', exact: true }).click();
    await expect(page.getByRole('dialog', { name: 'Search sessions', exact: true })).toBeVisible();
    await checkContrast();
    await page.screenshot({ path: `${output}/${name}-search.png` });
    await page.keyboard.press('Escape');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    await expect(page.locator('#whip-session-navigation')).toHaveCSS('background-color', 'rgb(17, 17, 16)');
    await page.screenshot({ path: `${output}/${name}-mobile.png` });
    await page.keyboard.press('Escape');
    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`${origin}/settings`);
    await chooseTheme('Claude Code', 'dark');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    assert.notEqual((await appearance()).navigation, 'rgb(17, 17, 16)', 'Pinned navigation must reset when switching themes');
    assert.notEqual((await appearance()).border, 'rgb(28, 28, 27)', 'Pinned border must reset when switching themes');
    assert.deepEqual(await page.evaluate(() => window.__themeCsp), []);
    assert.deepEqual(errors, []);
    console.log(`${name}: Claude Code picker, reload, exact Paper surfaces, hover/selection, contrast, search, mobile, theme reset and CSP passed`);
  } catch (error) {
    if (page) { await page.screenshot({ path: `${output}/${name}-failure.png` }); console.error(await page.locator('body').innerText()); }
    throw error;
  } finally { client.close(); await browser.close(); await fixture.close(); }
}
