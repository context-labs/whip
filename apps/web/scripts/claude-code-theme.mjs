import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { chromium, firefox, expect } from '@playwright/test';
import { startFixture } from './native-fixture.mjs';

const output = process.env.WHIP_THEME_RESULTS ?? '/tmp/whip-claude-code-theme-results';
await mkdir(output, { recursive: true });
const browsers = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(browsers.length > 0 && browsers.length <= 2 && new Set(browsers).size === browsers.length && browsers.every(name => ['chromium', 'firefox'].includes(name)));
for (const name of browsers) {
  let fixture, browser, page;
  const errors = [], csp = [];
  try {
    fixture = await startFixture();
    const client = await fixture.connect(`theme-${crypto.randomUUID()}`);
    const roots = [];
    for (const title of ['Claude Code SDK process communication', 'Prime Agent and WHIP comparison', 'Review the runtime architecture']) {
      roots.push((await fixture.createRoot(client, { title })).root.id);
    }
    const origin = fixture.info.web;
    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
    page.setDefaultTimeout(15_000);
    page.on('pageerror', error => { if (errors.length < 64) errors.push(error.message.slice(0, 4096)); });
    await page.exposeFunction('themeCSP', value => { if (csp.length < 64) csp.push(String(value).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.themeCSP(event.violatedDirective).catch(() => {}); }));
    await page.goto(`${origin}/settings`);
    const chooseTheme = async (current, label) => {
      const picker = page.getByRole('combobox', { name: /^Color theme:/ });
      await expect(picker).toContainText(current);
      await picker.click();
      const search = page.getByRole('combobox', { name: 'Search themes', exact: true });
      await search.fill(label);
      await page.getByRole('option', { name: label + ' Dark', exact: true }).click();
      await expect(search).toBeHidden();
    };
    await chooseTheme('System appearance', 'Claude Code');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'claude-code');
    await page.reload();
    await expect(page.getByRole('combobox', { name: 'Color theme: Claude Code', exact: true })).toBeVisible();
    const appearance = async () => page.locator('#whip-session-navigation').evaluate(element => ({
      background: getComputedStyle(document.documentElement).backgroundColor,
      foreground: getComputedStyle(document.documentElement).color,
      navigation: getComputedStyle(element).backgroundColor,
      border: getComputedStyle(element).borderRightColor,
    }));
    assert.deepEqual(await page.evaluate(() => ({ background: getComputedStyle(document.documentElement).backgroundColor, foreground: getComputedStyle(document.documentElement).color })), { background: 'rgb(20, 20, 20)', foreground: 'rgb(194, 192, 184)' });
    await page.screenshot({ path: `${output}/${name}-settings.png` });
    await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${roots[0]}`);
    await page.getByLabel('Message WHIP', { exact: true }).waitFor();
    assert.deepEqual(await appearance(), { background: 'rgb(20, 20, 20)', foreground: 'rgb(194, 192, 184)', navigation: 'rgb(17, 17, 16)', border: 'rgb(28, 28, 27)' });
    await page.getByLabel('Message WHIP', { exact: true }).fill('Explain how the SDK communicates with the Claude Code process.');
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(page.locator('[data-user-bubble]').first()).toBeVisible();
    await expect(page.getByText('Idle', { exact: true })).toBeVisible();
    assert.deepEqual(await fixture.effects(), ['Explain how the SDK communicates with the Claude Code process.']);
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
    await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${roots[0]}`);
    await page.getByLabel('Message WHIP', { exact: true }).waitFor();
    assert.notEqual((await appearance()).navigation, 'rgb(17, 17, 16)', 'Pinned navigation must reset when switching themes');
    assert.notEqual((await appearance()).border, 'rgb(28, 28, 27)', 'Pinned border must reset when switching themes');
    assert.deepEqual(csp, []);
    assert.deepEqual(errors, []);
    await writeFile(`${output}/${name}.json`, JSON.stringify({ browser: browser.version(), checks: ['theme picker and reload', 'exact Paper surfaces', 'selection and hover', 'real native provider response', 'conversation and search contrast', 'mobile navigation', 'theme reset'], errors, csp }, null, 2));
    console.log(`${name}: Claude Code picker, reload, exact Paper surfaces, hover/selection, contrast, search, mobile, theme reset and CSP passed`);
  } catch (error) {
    if (page) { await page.screenshot({ path: `${output}/${name}-failure.png` }); console.error((await page.locator('body').innerText()).slice(0, 16384)); }
    throw error;
  } finally { try { await browser?.close(); } finally { await fixture?.close(); } }
}
