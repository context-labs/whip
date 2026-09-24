import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

const results = process.env.WHIP_SESSION_ACTION_RESULTS ?? '/tmp/whip-session-action-results';
await mkdir(results, { recursive: true });
for (const engine of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const fixture = await startFixture();
  const browser = await ({ chromium, firefox }[engine]).launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `actions-${crypto.randomUUID()}`, clientKind: 'human' });
  const frames = [], errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('websocket', socket => socket.on('framesent', ({ payload }) => { try { frames.push(JSON.parse(String(payload))); } catch {} }));
  await page.addInitScript(() => {
    window.cspErrors = [];
    document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective));
  });
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const runtimeId = fixture.info.runtime_id, rootId = fixture.info.root_id;
  const row = id => page.locator(`[data-sidebar-session="${id}"]`);
  const action = async (id, label, rightClick = false) => {
    if (rightClick) await row(id).click({ button: 'right' });
    else { await row(id).hover(); await row(id).getByRole('button', { name: /^Actions for / }).click(); }
    await page.getByRole('menuitem', { name: label, exact: true }).click();
  };
  try {
    await client.connect();
    const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
    assert.equal(created.status, 'succeeded');
    const other = created.result.root_id;
    const longTitle = ('Menu target ' + 'long full title '.repeat(16)).trim();
    await client.session(other).rename(longTitle).result();
    await page.goto(`${origin}/h/${runtimeId}/s/${rootId}`);
    await page.locator('[data-whip-composer]').waitFor();
    await row(other).waitFor();
    // The shared dialog also stacks above Session details and returns focus.
    await page.getByRole('button', { name: /^Tab actions for / }).click();
    await page.getByRole('menuitem', { name: 'Session details', exact: true }).click();
    const details = page.getByRole('dialog', { name: 'Session details', exact: true });
    await details.getByRole('button', { name: 'Session actions', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click();
    await page.getByRole('dialog', { name: 'Rename session' }).getByRole('textbox').waitFor();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog', { name: 'Rename session' })).toBeHidden();
    await expect(details).toBeVisible();
    await details.getByRole('button', { name: 'Close', exact: true }).click();
    const snapshots = frames.filter(frame => frame.method === 'root.snapshot').length;
    await action(other, 'Rename');
    const rename = page.getByRole('dialog', { name: 'Rename session' });
    await expect(rename.getByRole('textbox')).toHaveValue(longTitle);
    assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, snapshots, 'Row action hydrated a transcript');
    assert.ok(page.url().endsWith(`/s/${rootId}`), 'Background row action changed selection');
    await rename.getByRole('textbox').fill('Renamed inactive session');
    await rename.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(rename).toBeHidden();
    await eventually(async () => (await client.sessions.get(other)).title === 'Renamed inactive session');

    await row(other).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Open in', exact: true }).focus();
    await page.keyboard.press('ArrowRight');
    await expect(page.getByRole('menuitem', { name: 'Copy directory' })).toBeVisible();
    await page.screenshot({ path: join(results, `${engine}-submenu-light.png`) });
    await page.keyboard.press('Escape'); await page.keyboard.press('Escape');

    await action(other, 'Archive', true);
    await expect(row(other)).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Undo archive' })).toBeVisible();
    assert.equal((await client.sessions.get(other)).archived, true);
    await page.getByRole('button', { name: 'Archived sessions', exact: true }).click();
    const search = page.getByRole('dialog', { name: 'Archived sessions' });
    await expect(search.getByRole('link', { name: /Renamed inactive session/ })).toBeVisible();
    await search.getByRole('button', { name: /^Actions for Renamed/ }).click();
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click();
    await expect(page.getByRole('dialog', { name: 'Rename session' }).getByRole('textbox')).toHaveValue('Renamed inactive session');
    await page.getByRole('dialog', { name: 'Rename session' }).getByRole('button', { name: 'Close', exact: true }).last().click();
    await expect(search.getByRole('link', { name: /Renamed inactive/ })).toBeVisible();
    await search.getByRole('button', { name: /^Actions for Renamed/ }).click();
    await page.getByRole('menuitem', { name: 'Restore', exact: true }).click();
    await expect(search.getByRole('link', { name: /Renamed inactive/ })).toHaveCount(0);
    await client.session(other).archive(true).result();
    await expect(search.getByRole('link', { name: /Renamed inactive/ })).toBeVisible({ timeout: 10_000 });
    await client.session(other).archive(false).result();
    await expect(search.getByRole('link', { name: /Renamed inactive/ })).toHaveCount(0, { timeout: 10_000 });
    await search.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(row(other)).toBeVisible();

    await action(other, 'Fork');
    await expect(page.getByRole('dialog', { name: 'Fork session' })).toBeHidden();
    await eventually(() => !page.url().endsWith(`/s/${rootId}`), { description: 'fork navigation' });
    const forked = page.url().split('/s/')[1].split('?')[0];
    const forkMeta = await client.sessions.get(forked);
    assert.equal(forkMeta.cwd, fixture.directory);
    assert.ok(forkMeta.title.includes('Renamed inactive session'));
    assert.equal(forkMeta.archived, false);
    await page.locator('[data-whip-composer]').fill('Draft retained while archived');
    await action(forked, 'Archive');
    await expect(row(forked)).toHaveCount(0);
    await expect(page.locator('[data-whip-composer]')).toHaveValue('Draft retained while archived');
    assert.ok(page.url().includes(forked), 'Archiving closed the open view');
    await client.session(forked).archive(false).result();
    await expect(row(forked)).toBeVisible();
    await action(forked, 'Delete…');
    await page.getByRole('dialog', { name: 'Delete this session?' }).getByRole('button', { name: 'Delete session', exact: true }).click();
    await expect(row(forked)).toHaveCount(0);
    await eventually(async () => {
      const workspace = await page.evaluate(() => JSON.parse(sessionStorage.getItem('whip.web.workspace.v3')).workspace);
      const tabs = node => node.type === 'pane' ? node.tabs : [...tabs(node.first), ...tabs(node.second)];
      return ![...tabs(workspace.layout), ...workspace.closed.map(item => item.tab)].some(tab => tab.rootId === forked);
    }, { description: 'delete purges open and closed views' });
    assert.deepEqual(await page.evaluate(id => Object.keys(localStorage).filter(key => key.includes(id)), forked), []);

    for (const theme of ['dark', 'light']) {
      await page.evaluate(id => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id })), theme);
      await page.reload(); await row(other).waitFor();
      await row(other).click({ button: 'right' });
      await page.getByRole('menuitem', { name: 'Open in', exact: true }).hover();
      await expect(page.getByRole('menuitem', { name: 'Copy directory' })).toBeVisible();
      await page.screenshot({ path: join(results, `${engine}-submenu-${theme}.png`) });
      await page.keyboard.press('Escape'); await page.keyboard.press('Escape');
    }
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    await row(other).getByRole('button', { name: /^Actions for / }).click();
    await page.getByRole('menuitem', { name: 'Open in', exact: true }).click();
    await expect(page.getByRole('menuitem', { name: 'Copy directory' })).toBeInViewport();
    await page.screenshot({ path: join(results, `${engine}-submenu-phone.png`) });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    assert.deepEqual(errors, []);
    assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
    await writeFile(join(results, `${engine}.json`), JSON.stringify({ checks: ['full metadata without transcript', 'inactive rename', 'keyboard submenu', 'archived search nested dialog', 'restore', 'same-directory fork', 'archive retains open draft', 'other-client restore', 'delete purges local state', 'light/dark/touch', 'strict CSP'], frames: frames.length }, null, 2));
    console.log(`${engine}: conversation row actions passed`);
  } catch (error) { await page.screenshot({ path: join(results, `${engine}-failure.png`) }).catch(() => {}); throw error; } finally { client.close(); await browser.close(); await fixture.close(); }
}
