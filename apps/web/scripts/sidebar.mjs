import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Production assets, isolated host, synthetic sessions. Never touches a user daemon.
const directory = process.env.WHIP_WEB_SIDEBAR_RESULTS ?? '/tmp/whip-sidebar-results';
await mkdir(directory, { recursive: true });
const results = {};
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const fixture = await startFixture();
  const browser = await ({ chromium, firefox }[name]).launch();
  const context = await browser.newContext({ viewport: { width: 1440, height: 960 } });
  context.setDefaultTimeout(10_000);
  const page = await context.newPage();
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `sidebar-${crypto.randomUUID()}`, clientKind: 'human' });
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const route = id => `/h/${fixture.info.runtime_id}/s/${id}`;
  const frames = [], errors = [], checks = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', event => { if (event.type() === 'error' && /content.security.policy|violates.*directive|refused to (execute|apply|load)/i.test(event.text())) errors.push(event.text()); });
  page.on('websocket', socket => socket.on('framesent', ({ payload }) => { try { frames.push(JSON.parse(String(payload))); } catch {} }));
  const sidebar = () => page.locator('aside[aria-label="Session navigation"]');
  const separator = () => page.getByRole('separator', { name: 'Resize session navigation' });
  const saved = () => page.getByLabel('Saved sessions', { exact: true });
  const group = cwd => sidebar().getByRole('button', { name: cwd, exact: true });
  const ready = () => page.getByLabel('Message WHIP', { exact: true }).waitFor();
  try {
    await client.connect();
    const paths = [join(fixture.directory, 'repo/main'), join(fixture.directory, 'worktrees/main'), join(fixture.directory, 'sdk')];
    for (const path of paths) await mkdir(path, { recursive: true });
    const roots = [];
    for (let i = 0; i < 140; i++) {
      // Catalog recency has one-second precision. Keep directory groups in
      // separate seconds so random session IDs cannot reorder the fixture.
      if (i === 130 || i === 135) await new Promise(resolve => setTimeout(resolve, 1100));
      const result = await client.sessions.create({ cwd: paths[i < 130 ? 0 : i < 135 ? 1 : 2], model: 'model', provider: 'provider' }).result();
      assert.equal(result.status, 'succeeded'); roots.push(result.result.root_id);
      await client.session(roots[i]).rename(`Session ${String(i).padStart(3, '0')}${i === 139 ? ' — a long session title to verify clipping without expanding the navigation' : ''}`).result();
    }
    await page.goto(origin + route(roots[139])); await ready();
    await group(paths[2]).waitFor();
    assert.equal((await sidebar().boundingBox()).width, 320);
    assert.equal(await page.locator(`[data-sidebar-session="${roots[139]}"]`).evaluate(node => node.getBoundingClientRect().height), 28);
    assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, 1);
    await eventually(() => frames.some(frame => frame.method === 'sessions.summaries' && frame.params.root_ids.length > 1), { description: 'visible sidebar activity is requested' });
    const summaryRequests = frames.filter(frame => frame.method === 'sessions.summaries');
    assert.ok(summaryRequests.every(frame => frame.params.root_ids.length <= 32), 'Summary requests exceed the daemon limit');
    assert.ok(new Set(summaryRequests.flatMap(frame => frame.params.root_ids)).size < 128, 'Sidebar polled the entire catalog instead of rendered rows');
    assert.equal(await sidebar().getByRole('link', { name: 'Settings', exact: true }).count(), 1);
    assert.ok((await group(paths[0]).innerText()).includes('main · repo'));
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('Paper density, 320px default, exact directories/worktrees, labels and no extra root hydration');

    const action = id => page.locator(`[data-sidebar-session="${id}"]`).getByRole('button');
    const caret = path => group(path).locator('[data-directory-caret]');
    const opacity = locator => locator.evaluate(node => getComputedStyle(node).opacity);
    const rowFill = id => page.locator(`[data-sidebar-session="${id}"] a`).evaluate(node => getComputedStyle(node.parentElement).backgroundColor);
    const tokenFill = token => page.evaluate(token => {
      const probe = document.createElement('span'); probe.style.backgroundColor = `var(--whip-${token})`; document.body.append(probe);
      const color = getComputedStyle(probe).backgroundColor; probe.remove(); return color;
    }, token);
    await page.mouse.move(900, 500);
    assert.equal(await opacity(action(roots[139])), '0');
    assert.equal(await opacity(caret(paths[2])), '0');
    assert.equal(await rowFill(roots[139]), await tokenFill('hover'));
    await sidebar().getByRole('link', { name: 'Session 138', exact: true }).hover();
    assert.equal(await opacity(action(roots[138])), '1');
    assert.equal(await opacity(action(roots[139])), '0');
    assert.equal(await opacity(caret(paths[2])), '1', 'Child hover must reveal its directory caret');
    assert.equal(await opacity(caret(paths[1])), '0');
    assert.equal(await rowFill(roots[138]), await tokenFill('element'));
    await sidebar().getByRole('link', { name: /^Session 139/ }).hover();
    assert.equal(await rowFill(roots[139]), await tokenFill('hover'), 'Selected fill must survive hover');
    await group(paths[2]).hover();
    assert.equal(await group(paths[2]).evaluate(node => getComputedStyle(node).backgroundColor), 'rgba(0, 0, 0, 0)');
    assert.equal(await opacity(caret(paths[2])), '1');
    assert.ok((await caret(paths[2]).boundingBox()).x > (await group(paths[2]).locator('span').first().boundingBox()).x);
    await page.mouse.move(900, 500);
    assert.equal(await opacity(caret(paths[2])), '0');
    await action(roots[138]).focus();
    assert.equal(await opacity(action(roots[138])), '1', 'Keyboard focus must reveal the action');
    await action(roots[138]).click();
    await page.getByRole('menuitem', { name: 'Open in background tab', exact: true }).hover();
    assert.equal(await opacity(action(roots[138])), '1', 'Open menu must retain its visible trigger');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Hide navigation', exact: true }).focus();
    checks.push('hover-only desktop actions and trailing group caret, keyboard/open-menu access, and theme surface roles');

    await group(paths[1]).click();
    await page.waitForTimeout(2200);
    assert.equal(await group(paths[1]).getAttribute('aria-expanded'), 'false');
    await page.reload(); await ready();
    assert.equal(await group(paths[1]).getAttribute('aria-expanded'), 'false');
    await page.goto(origin + route(roots[133])); await ready();
    await eventually(async () => await group(paths[1]).getAttribute('aria-expanded') === 'true');
    assert.equal(await sidebar().getByRole('link', { name: 'Session 133', exact: true }).getAttribute('aria-current'), 'page');
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('collapse persists through polling/reload; explicit navigation expands and reveals');

    await separator().focus(); await page.keyboard.press('End');
    assert.equal((await sidebar().boundingBox()).width, 420);
    await page.screenshot({ path: join(directory, `${name}-420-light.png`) });
    await page.keyboard.press('Home'); assert.equal((await sidebar().boundingBox()).width, 256);
    await page.keyboard.press('ArrowRight'); assert.equal((await sidebar().boundingBox()).width, 264);
    const bounds = await separator().boundingBox();
    await page.mouse.move(bounds.x + 3, bounds.y + 100); await page.mouse.down(); await page.mouse.move(bounds.x + 159, bounds.y + 100); await page.mouse.up();
    assert.equal((await sidebar().boundingBox()).width, 420);
    await separator().focus(); await page.keyboard.press('Enter');
    assert.equal(await sidebar().count(), 0);
    await eventually(() => page.getByRole('button', { name: 'Open navigation', exact: true }).evaluate(node => node === document.activeElement), { description: 'focus transfers after hiding navigation' });
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    assert.equal((await sidebar().boundingBox()).width, 420);
    await page.setViewportSize({ width: 768, height: 960 });
    await eventually(async () => (await sidebar().boundingBox()).width === 288, { description: 'viewport cap' });
    await page.setViewportSize({ width: 1440, height: 960 });
    await eventually(async () => (await sidebar().boundingBox()).width === 420, { description: 'remembered width' });
    await separator().dblclick(); assert.equal((await sidebar().boundingBox()).width, 320);
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('pointer/keyboard resize, viewport cap, hide/restore and focus');

    await sidebar().getByRole('link', { name: `New session in ${paths[1]}`, exact: true }).click();
    const cwd = page.getByLabel('Working directory on Local', { exact: true });
    await eventually(async () => await cwd.inputValue() === paths[1], { description: 'directory prefill after handshake' });
    await cwd.fill('/typed/unsent'); await page.waitForTimeout(2200); assert.equal(await cwd.inputValue(), '/typed/unsent');
    await page.reload(); await eventually(async () => await cwd.inputValue() === paths[1], { description: 'reload directory prefill' });
    await sidebar().getByRole('link', { name: 'New session', exact: true }).click(); assert.equal(await cwd.inputValue(), '');
    assert.equal(frames.filter(frame => frame.method === 'command.submit').length, 0);
    await page.goto(origin + route(roots[133]) + '?panel=execution');
    await page.getByRole('dialog', { name: 'Session details', exact: true }).waitFor();
    await page.goto(`${origin}/?cwd=${encodeURIComponent(paths[0])}&runtimeId=another-host`);
    await page.getByText('Select or add a remote host to begin.', { exact: true }).waitFor();
    assert.equal(await cwd.count(), 0);
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('directory prefill survives reload, preserves edits, clears globally, keeps an unknown host explicit and submits nothing');

    await page.getByRole('button', { name: 'Hide navigation', exact: true }).focus();
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+k' : 'Control+k');
    await page.getByRole('dialog', { name: 'Commands', exact: true }).getByRole('combobox').fill('Browse sessions');
    await page.getByRole('option', { name: 'Browse sessions', exact: true }).click();
    const input = page.getByLabel('Search sessions on this host', { exact: true });
    assert.equal(await input.evaluate(node => node === document.activeElement), true);
    await input.fill('Session 133');
    await page.getByLabel('Session search results', { exact: true }).getByRole('link', { name: new RegExp('Session 133') }).waitFor();
    const resultLink = page.getByLabel('Session search results', { exact: true }).getByRole('link', { name: /Session 133/ });
    assert.ok((await resultLink.getAttribute('href')).includes('panel=execution'), 'Sidebar lost saved inspector location');
    const popupPromise = context.waitForEvent('page');
    await resultLink.click({ button: 'middle' });
    const popup = await popupPromise;
    await popup.getByRole('dialog', { name: 'Session details', exact: true }).waitFor();
    assert.ok(popup.url().includes('panel=execution'));
    await popup.getByRole('dialog', { name: 'Session details', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
    const originalWidth = (await sidebar().boundingBox()).width;
    await popup.getByRole('separator', { name: 'Resize session navigation' }).focus(); await popup.keyboard.press('Home');
    assert.equal((await sidebar().boundingBox()).width, originalWidth, 'Window layouts were shared');
    await popup.close();
    const count = frames.filter(frame => frame.method === 'root.snapshot').length;
    await page.getByLabel('Session search results', { exact: true }).getByRole('link', { name: /Session 133/ }).hover();
    await page.waitForTimeout(300); assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, count);
    await page.getByRole('button', { name: 'Actions for Session 133 on Local', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Open in background tab', exact: true }).click();
    assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, count);
    assert.equal(new URL(page.url()).pathname, '/');
    await input.focus(); await page.keyboard.press('Escape');
    assert.equal(await input.count(), 0);
    await eventually(async () => await page.evaluate(() => document.activeElement?.getAttribute('aria-label') === 'Hide navigation'), { description: 'restore focus to command opener or persistent navigation fallback' });
    await page.getByRole('button', { name: 'Hide navigation', exact: true }).click();
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    assert.equal(await input.count(), 0, 'Consumed search command replayed on remount');
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('debounced flat search, Escape focus restoration, consumed commands, background menu and hover without hydration');

    await saved().evaluate(node => { node.scrollTop = 1400; });
    await page.waitForTimeout(100);
    const anchor = await saved().evaluate(node => {
      const top = node.getBoundingClientRect().top;
      const row = [...node.querySelectorAll('[data-sidebar-session]')].find(row => row.getBoundingClientRect().bottom > top);
      return { id: row.dataset.sidebarSession, y: row.getBoundingClientRect().top - top };
    });
    // A rename moves an old row to the front of its directory in server order.
    await client.session(roots[0]).rename('Moved by catalog refresh').result();
    await page.waitForTimeout(2500);
    const after = await page.locator(`[data-sidebar-session="${anchor.id}"]`).evaluate(node => node.getBoundingClientRect().top - node.closest('[aria-label="Saved sessions"]').getBoundingClientRect().top);
    assert.ok(Math.abs(after - anchor.y) < 2, `Reading anchor moved: ${anchor.y} -> ${after}`);
    await saved().evaluate(node => { node.scrollTop = node.scrollHeight; });
    await saved().getByRole('button', { name: 'Load more sessions' }).click();
    await eventually(async () => await saved().getByRole('button', { name: 'Load more sessions' }).count() === 0);
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('catalog reorder preserves visible anchor; explicit load-more reaches later pages');

    await page.goto(origin + route(roots[139])); await ready();
    await page.screenshot({ path: join(directory, `${name}-320-light.png`) });
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await ready();
    await page.screenshot({ path: join(directory, `${name}-320-dark.png`) });
    await separator().focus(); await page.keyboard.press('End');
    await page.screenshot({ path: join(directory, `${name}-420-dark.png`) });
    if (name === 'chromium') {
      const source = await readFile('packages/ui/src/generated/theme-catalog.ts', 'utf8');
      const ids = [...source.matchAll(/"id":\s*"([^"]+)"/g)].map(match => match[1]);
      for (const id of ids) {
        await page.evaluate(id => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id })), id);
        await page.reload(); await ready();
        assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), id);
        assert.equal((await sidebar().boundingBox()).width, 420);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
        await sidebar().getByRole('button', { name: 'Search sessions', exact: true }).click();
        assert.equal(await input.evaluate(node => node === document.activeElement), true);
        await page.keyboard.press('Escape');
      }
      assert.equal(ids.length, 66);
      console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('all 66 themes render sidebar selection, controls, focus and geometry');
    }
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    const sheet = page.getByRole('dialog', { name: 'WHIP', exact: true });
    assert.ok((await sheet.getByRole('button', { name: 'Search sessions', exact: true }).boundingBox()).height >= 44);
    const geometry = await sheet.evaluate(node => { const list = node.querySelector('[aria-label="Saved sessions"]'); const host = node.querySelector('[aria-label="Manage servers"]'); return { height: list.clientHeight, hostBottom: host.getBoundingClientRect().bottom, viewport: innerHeight }; });
    assert.ok(geometry.height > 100 && geometry.hostBottom <= geometry.viewport, JSON.stringify(geometry));
    await sheet.getByRole('button', { name: 'Search sessions', exact: true }).click();
    const searchDialog = page.getByRole('dialog', { name: 'Search sessions', exact: true });
    await input.fill('Session 139');
    await searchDialog.getByRole('link', { name: /Session 139/ }).waitFor();
    assert.ok((await searchDialog.getByRole('link', { name: /Session 139/ }).boundingBox()).height >= 44);
    await page.screenshot({ path: join(directory, `${name}-mobile.png`) });
    await searchDialog.getByRole('link', { name: /Session 139/ }).click(); await ready();
    await eventually(async () => await searchDialog.count() === 0 && await sheet.count() === 0);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('mobile Sheet, 44px controls, search/selection, close and no document overflow');
    assert.deepEqual(errors, []);
    results[name] = { checks, screenshots: directory };
    console.log(`${name}: ${checks.length} sidebar workflows passed`);
  } catch (error) {
    await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`);
    throw error;
  } finally { client.close(); await browser.close(); await fixture.close(); }
}
await writeFile(join(directory, 'sidebar.json'), JSON.stringify(results, null, 2));
