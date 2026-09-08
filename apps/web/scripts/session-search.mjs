import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdir, readFile } from 'node:fs/promises';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { startFixture, eventually } from '../../../packages/sdk/scripts/fixture.mjs';
const output = '/tmp/whip-session-search-results';
await mkdir(output, { recursive: true });
for (const [name, launcher] of Object.entries({ chromium, firefox })) {
  const fixture = await startFixture();
  const browser = await launcher.launch();
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: crypto.randomUUID(), clientKind: 'human' });
  try {
    await client.connect();
    const roots = [];
    for (let i = 0; i < 70; i++) {
      const result = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
      roots.push(result.result.root_id);
      await client.session(roots.at(-1)).rename(`Search dialog session ${i}`).result();
    }
    const page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
    const errors = [], frames = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('websocket', socket => socket.on('framesent', ({ payload }) => { try { frames.push(JSON.parse(String(payload))); } catch {} }));
    const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
    await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${roots[0]}`);
    await page.getByLabel('Message WHIP', { exact: true }).waitFor();
    const trigger = page.getByRole('button', { name: 'Search sessions', exact: true });
    const dialog = page.getByRole('dialog', { name: 'Search sessions', exact: true });
    const input = dialog.getByRole('textbox', { name: 'Search sessions on this host' });
    await trigger.click(); await input.waitFor();
    assert.equal(await input.evaluate(node => node === document.activeElement), true);
    await eventually(async () => await dialog.locator('[data-search-index]').count() === 64);
    assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, 1);
    const axeSource = await readFile(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
    await page.route(origin + '/__search-axe.js', route => route.fulfill({ contentType: 'text/javascript', body: axeSource }));
    await page.addScriptTag({ url: origin + '/__search-axe.js' });
    const violations = await page.evaluate(async () => (await axe.run(document.querySelector('[role="dialog"]'))).violations.filter(item => ['serious', 'critical'].includes(item.impact)).map(item => ({ id: item.id, nodes: item.nodes.map(node => node.target) })));
    assert.deepEqual(violations, [], 'Search dialog accessibility');
    await page.keyboard.press('ArrowDown');
    const target = await dialog.locator('[data-search-index="1"] a').getAttribute('href');
    await page.keyboard.press('Enter');
    await eventually(() => new URL(page.url()).pathname === new URL(target, origin).pathname);
    assert.equal(await dialog.count(), 0);
    await trigger.click(); await input.fill('definitely-no-matching-session');
    await dialog.getByText('No matching sessions.', { exact: true }).waitFor();
    await input.fill('Search dialog session');
    await dialog.getByRole('button', { name: 'Load more sessions' }).waitFor();
    await dialog.getByRole('button', { name: 'Load more sessions' }).click();
    await eventually(async () => await dialog.locator('[data-search-index]').count() === 6);
    await dialog.getByRole('button', { name: 'First results' }).click();
    await eventually(async () => await dialog.locator('[data-search-index]').count() === 64);
    await input.focus(); await page.keyboard.press('Escape');
    await eventually(async () => await dialog.count() === 0);
    assert.equal(await trigger.evaluate(node => node === document.activeElement), true);
    await trigger.click(); assert.equal(await input.inputValue(), '');
    await page.screenshot({ path: `${output}/${name}-light.png` });
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await trigger.click(); await input.waitFor();
    await page.screenshot({ path: `${output}/${name}-dark.png` });
    await input.press('Escape');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    await trigger.click(); await input.waitFor();
    await eventually(async () => await page.getByRole('dialog', { name: 'WHIP', exact: true }).count() === 0);
    assert.equal(await input.evaluate(node => node === document.activeElement), true);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.screenshot({ path: `${output}/${name}-mobile.png` });
    await input.press('Escape');
    await eventually(async () => await dialog.count() === 0);
    assert.equal(await page.getByRole('button', { name: 'Open navigation', exact: true }).evaluate(node => node === document.activeElement), true);
    assert.deepEqual(errors, []);
    console.log(`${name}: recent results, keyboard selection, filtering/pagination, focus return, light/dark/mobile and no extra root hydration passed`);
  } catch (error) {
    for (const page of browser.contexts().flatMap(context => context.pages())) { await page.screenshot({ path: `${output}/${name}-failure.png` }).catch(() => {}); console.log(await page.locator('body').innerText().catch(() => '')); }
    throw error;
  } finally { client.close(); await browser.close(); await fixture.close(); }
}
