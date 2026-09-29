import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { eventually, startFixture, deadline } from './native-fixture.mjs';

const results = process.env.WHIP_SESSION_ACTION_RESULTS ?? '/tmp/whip-session-action-results';
const engines = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(engines.length > 0 && engines.length <= 2 && new Set(engines).size === engines.length && engines.every(name => ['chromium', 'firefox'].includes(name)));
const reports = [];
await mkdir(results, { recursive: true });
for (const engine of engines) {
  let fixture, browser, page;
  const frames = [], errors = [], csp = [], report = { engine, checks: [] };
  let frameCount = 0;
  reports.push(report);
  try {
    fixture = await startFixture();
    const client = await fixture.connect(`actions-${randomUUID()}`);
    const { root } = await fixture.createRoot(client, { title: 'Selected root stays unchanged' });
    const rootId = root.id, runtimeId = client.runtimeID, origin = fixture.info.web;
    const longTitle = ('Menu target ' + 'long full title '.repeat(16)).trim();
    const { root: otherRoot } = await fixture.createRoot(client, { title: longTitle });
    const other = otherRoot.id;
    const input = randomUUID();
    await client.session(other).submit([{ type: 'text', text: 'Committed history for exact same-directory fork.' }], input, deadline());
    assert.equal((await client.wait(input, deadline())).turn.state, 'succeeded');
    const sourceHistory = await client.session(other).history.page({ direction: 'forward' }, deadline());
    assert.equal(sourceHistory.next_cursor, null);
    assert(sourceHistory.messages.length >= 2);
    const tree = async id => client.trees.get((await client.session(id).get(deadline())).tree_id, deadline());
    const archive = async (id, value) => {
      const current = await tree(id);
      return client.trees.update(current.id, current.revision, { ...current.metadata, archived: value }, deadline());
    };
    browser = await ({ chromium, firefox }[engine]).launch();
    page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    page.setDefaultTimeout(15000);
    page.on('pageerror', error => { if (errors.length < 64) errors.push(error.message.slice(0, 4096)); });
    page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
      try {
        const frame = JSON.parse(String(payload));
        frameCount++;
        if (frames.length < 2048) frames.push({ method: frame.method, owner: frame.params?.session_id, tree: frame.params?.tree_id });
      } catch {}
    }));
    await page.exposeFunction('__actionCSP', value => { if (csp.length < 64) csp.push(String(value).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__actionCSP(event.violatedDirective).catch(() => {}); }));
    const row = id => page.locator(`[data-sidebar-session="${id}"]`);
    const action = async (id, label, rightClick = false) => {
      if (rightClick) await row(id).click({ button: 'right' });
      else { await row(id).hover(); await row(id).getByRole('button', { name: /^Actions for / }).click(); }
      await page.getByRole('menuitem', { name: label, exact: true }).click();
    };
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
    const beforeRename = frames.length;
    await action(other, 'Rename');
    const rename = page.getByRole('dialog', { name: 'Rename session' });
    await expect(rename.getByRole('textbox')).toHaveValue(longTitle);
    assert.equal(frames.slice(beforeRename).some(frame => frame.owner === other && ['sessions.observe', 'sessions.history_page', 'sessions.history', 'sessions.turns'].includes(frame.method)), false, 'Background rename hydrated its transcript/executions');
    assert.ok(page.url().endsWith(`/s/${rootId}`), 'Background row action changed selection');
    await rename.getByRole('textbox').fill('Renamed inactive session');
    await rename.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(rename).toBeHidden();
    await eventually(async () => (await tree(other)).metadata.title === 'Renamed inactive session');

    await row(other).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Open in', exact: true }).focus();
    await page.keyboard.press('ArrowRight');
    await expect(page.getByRole('menuitem', { name: 'Copy directory' })).toBeVisible();
    await page.screenshot({ path: join(results, `${engine}-submenu-light.png`) });
    await page.keyboard.press('Escape'); await page.keyboard.press('Escape');

    await action(other, 'Archive', true);
    await expect(row(other)).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Undo archive' })).toBeVisible();
    assert.equal((await tree(other)).metadata.archived, true);
    await page.getByRole('button', { name: 'Search sessions', exact: true }).click();
    const search = page.getByRole('dialog', { name: 'Search sessions', exact: true });
    await search.getByRole('combobox', { name: 'Search state' }).click();
    await page.getByRole('option', { name: 'Archived sessions', exact: true }).click();
    await expect(search.getByRole('link', { name: /Renamed inactive session/ })).toBeVisible();
    await expect(search.getByRole('link', { name: /Renamed inactive session/ })).toContainText('Archived');
    await expect(search.getByRole('link', { name: /Selected root stays unchanged/ })).toHaveCount(0);
    await search.getByRole('button', { name: /^Actions for Renamed/ }).click();
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click();
    await expect(page.getByRole('dialog', { name: 'Rename session' }).getByRole('textbox')).toHaveValue('Renamed inactive session');
    await page.getByRole('dialog', { name: 'Rename session' }).getByRole('button', { name: 'Close', exact: true }).last().click();
    await expect(search.getByRole('link', { name: /Renamed inactive/ })).toBeVisible();
    await search.getByRole('button', { name: /^Actions for Renamed/ }).click();
    await page.getByRole('menuitem', { name: 'Restore', exact: true }).click();
    await expect(search.getByRole('link', { name: /Renamed inactive/ })).toHaveCount(0);
    await archive(other, true);
    await expect(search.getByRole('link', { name: /Renamed inactive/ })).toBeVisible({ timeout: 10_000 });
    await archive(other, false);
    await expect(search.getByRole('link', { name: /Renamed inactive/ })).toHaveCount(0, { timeout: 10_000 });
    await search.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(row(other)).toBeVisible();

    await action(other, 'Fork');
    await expect(page.getByRole('dialog', { name: 'Fork session' })).toBeHidden();
    await eventually(() => !page.url().endsWith(`/s/${rootId}`), { description: 'fork navigation' });
    const forked = page.url().split('/s/')[1].split('?')[0];
    const forkOwner = await client.session(forked).get(deadline()), forkTree = await tree(forked);
    assert.equal(forkOwner.parent_id, null);
    assert.notEqual(forkOwner.id, other);
    assert.notEqual(forkOwner.tree_id, otherRoot.tree_id);
    assert.equal(forkOwner.working_directory, fixture.directory);
    assert.ok(forkTree.metadata.title.includes('Renamed inactive session'));
    assert.equal(forkTree.metadata.archived, false);
    const forkHistory = await client.session(forked).history.page({ direction: 'forward' }, deadline());
    assert.equal(forkHistory.next_cursor, null);
    assert.deepEqual(forkHistory.messages.map(message => ({ role: message.role, parts: message.parts })), sourceHistory.messages.map(message => ({ role: message.role, parts: message.parts })));
    assert.equal((await client.session(forked).usage(deadline())).attempts.settled, '0', 'Fork copied model charges');
    assert.equal((await tree(rootId)).metadata.title, 'Selected root stays unchanged');
    await page.locator('[data-whip-composer]').fill('Draft retained while archived');
    await action(forked, 'Archive');
    await expect(row(forked)).toHaveCount(0);
    await expect(page.locator('[data-whip-composer]')).toHaveValue('Draft retained while archived');
    assert.ok(page.url().includes(forked), 'Archiving closed the open view');
    await archive(forked, false);
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
    await assert.rejects(client.session(forked).get(deadline()), error => error.kind === 'NOT_FOUND');
    assert.deepEqual((await client.session(other).history.page({ direction: 'forward' }, deadline())).messages, sourceHistory.messages);

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
    assert.deepEqual(csp, []);
    assert(frameCount <= 2048, 'Action request evidence capacity exceeded');
    assert.equal((await tree(rootId)).metadata.title, 'Selected root stays unchanged');
    assert.equal((await tree(other)).metadata.title, 'Renamed inactive session');
    report.checks.push('full metadata without background transcript/turn hydration', 'inactive rename preserves selected root', 'keyboard submenu', 'archived search nested dialog', 'restore', 'same-directory fork with exact committed content and no copied charges', 'archive retains open draft', 'other-client restore invalidates catalog', 'delete purges local state', 'light/dark/touch', 'strict CSP');
    Object.assign(report, { passed: true, browser: browser.version(), frames: frameCount, errors, csp });
    console.log(`${engine}: ${report.checks.length} native conversation row action workflows passed`);
  } catch (error) {
    report.error = String(error.stack ?? error);
    report.errors = errors; report.csp = csp; report.frames = frames;
    if (page) { await page.screenshot({ path: join(results, `${engine}-failure.png`) }).catch(() => {}); report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16000); }
    throw error;
  } finally {
    try { await browser?.close(); }
    finally { try { await fixture?.close(); } finally { await writeFile(join(results, 'report.json'), JSON.stringify(reports, null, 2)); } }
  }
}
