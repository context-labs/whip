import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Focused, isolated coverage of the shared desktop/web directory-first sidebar.
const directory = process.env.WHIP_WEB_DIRECTORY_RESULTS ?? '/tmp/whip-directory-sidebar-results';
await mkdir(directory, { recursive: true });
const origin = fixture => fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
const assertSessionGap = async (scope, height) => {
  const links = scope.locator('[data-sidebar-session] a');
  const first = await links.nth(0).boundingBox(), second = await links.nth(1).boundingBox();
  assert.equal(first.height, height, 'Session targets retain their full height');
  assert.equal(second.y - (first.y + first.height), 1, 'Adjacent session backgrounds have a 1px gap');
};
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const fixtures = [], clients = [], errors = [], frames = [], checks = [];
  let browser;
  try {
    const local = await startFixture(); fixtures.push(local);
    const remote = await startFixture({ allowedOrigins: [origin(local)] }); fixtures.push(remote);
    for (const fixture of fixtures) {
      const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: 'directory-sidebar', clientKind: 'human' });
      clients.push(client); await client.connect();
    }
    const shared = join(local.directory, 'whip'), other = join(local.directory, 'rlm-new');
    await mkdir(shared); await mkdir(other);
    const create = async (client, cwd, title) => {
      const receipt = await client.sessions.create({ cwd, model: 'model', provider: 'provider' }).result();
      assert.equal(receipt.status, 'succeeded');
      const root = receipt.result.root_id;
      await client.session(root).rename(title).result(); return root;
    };
    await create(clients[0], other, 'Explore repo with subagents');
    await new Promise(resolve => setTimeout(resolve, 1100));
    const remoteRoot = await create(clients[1], shared, 'This is a remote session');
    await new Promise(resolve => setTimeout(resolve, 1100));
    const localRoot = await create(clients[0], shared, 'Map Whip backend architecture');
    const config = await clients[0].configuration.get();
    await clients[0].configuration.update({ revision: config.revision, remote_hosts: [{ id: 'mm', name: 'mm', url: origin(remote), runtime_id: remote.info.runtime_id, connect_on_launch: true }] });
    browser = await ({ chromium, firefox }[name]).launch();
    const context = await browser.newContext({ viewport: { width: 1440, height: 960 }, reducedMotion: 'reduce' });
    const page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    page.on('websocket', socket => socket.on('framesent', ({ payload }) => { try { frames.push({ endpoint: socket.url(), ...JSON.parse(String(payload)) }); } catch {} }));
    await page.goto(origin(local) + '/h/' + local.info.runtime_id + '/s/' + localRoot);
    const sidebar = page.locator('aside[aria-label="Session navigation"]');
    const row = (runtime, cwd) => sidebar.locator('[data-sidebar-directory][data-sidebar-runtime="' + runtime + '"][data-sidebar-directory="' + cwd + '"]');
    const session = (runtime, root) => sidebar.locator('[data-sidebar-session="' + root + '"][data-sidebar-runtime="' + runtime + '"]');
    const remoteRow = row(remote.info.runtime_id, shared), localRow = row(local.info.runtime_id, shared);
    await remoteRow.waitFor(); await localRow.waitFor();
    await page.getByLabel('Message WHIP', { exact: true }).waitFor();
    const order = await sidebar.locator('[data-sidebar-directory]').evaluateAll(nodes => nodes.slice(0, 3).map(node => [node.dataset.sidebarRuntime, node.dataset.sidebarDirectory]));
    assert.deepEqual(order, [[local.info.runtime_id, shared], [remote.info.runtime_id, shared], [local.info.runtime_id, other]]);
    assert.equal(await sidebar.getByRole('region', { name: 'Local sessions', exact: true }).count(), 0);
    assert.equal(await remoteRow.getByRole('button', { name: 'mm · Connected · ' + shared, exact: true }).count(), 1);
    assert.equal(await sidebar.locator('[data-sidebar-directory] .lucide-folder, [data-sidebar-directory] .lucide-folder-open, [data-sidebar-directory] .lucide-globe').count(), 0);
    await remoteRow.getByRole('button', { name: shared, exact: true }).click();
    assert.equal(await session(remote.info.runtime_id, remoteRoot).count(), 0);
    assert.equal(await session(local.info.runtime_id, localRoot).count(), 1);
    await clients[1].session(remoteRoot).rename('Remote update while collapsed').result();
    await page.waitForTimeout(2300);
    assert.equal(await remoteRow.getByRole('button', { name: shared, exact: true }).getAttribute('aria-expanded'), 'false');
    await remoteRow.getByRole('button', { name: shared, exact: true }).click();
    await session(remote.info.runtime_id, remoteRoot).getByRole('link').click();
    await eventually(() => new URL(page.url()).pathname.includes(remote.info.runtime_id));
    assert.equal(await session(remote.info.runtime_id, remoteRoot).getByRole('link').getAttribute('aria-current'), 'page');
    checks.push('mixed recency, exact host/path identity, scoped collapse and remote selection');

    const resize = page.getByRole('separator', { name: 'Resize session navigation' });
    for (const theme of ['light', 'dark', 'claude-code', 'cobalt2']) {
      await page.evaluate(id => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id })), theme);
      await page.reload(); await remoteRow.waitFor();
      for (const [width, key] of [[256, 'Home'], [420, 'End']]) {
        await resize.focus(); await page.keyboard.press(key);
        await eventually(async () => Math.round((await sidebar.boundingBox()).width) === width);
        assert.equal(await sidebar.getByLabel('Saved sessions', { exact: true }).evaluate(node => node.scrollWidth > node.clientWidth), false);
        const centers = await remoteRow.locator(':scope > button, :scope > a').evaluateAll(nodes => nodes.map(node => { const box = node.getBoundingClientRect(); return box.y + box.height / 2; }));
        assert.ok(Math.max(...centers) - Math.min(...centers) < 1, 'Directory controls must share a centerline');
        const hostTrigger = remoteRow.getByRole('button', { name: 'mm · Connected · ' + shared, exact: true });
        await hostTrigger.focus(); await page.keyboard.press('Enter');
        const details = page.getByRole('dialog', { name: /^mm Connected/ });
        await details.waitFor();
        assert.equal(await details.getByText(shared, { exact: true }).count(), 1);
        const geometry = await details.evaluate(node => {
          const action = node.querySelector('button'), rect = node.getBoundingClientRect(), css = getComputedStyle(node);
          return { width: rect.width, left: rect.left, right: rect.right, overflow: node.scrollWidth > node.clientWidth,
            border: css.borderTopWidth, align: getComputedStyle(action).justifyContent,
            titleMargin: getComputedStyle(node.querySelector('h2')).marginBlockStart, side: node.dataset.side };
        });
        assert.ok(geometry.width <= 300 && geometry.left >= 0 && geometry.right <= 1440);
        assert.equal(geometry.overflow, false); assert.equal(geometry.border, '1px'); assert.equal(geometry.align, 'flex-start');
        assert.equal(geometry.titleMargin, '0px'); assert.equal(geometry.side, 'right');
        await page.screenshot({ path: join(directory, name + '-' + theme + '-' + width + '.png') });
        await page.keyboard.press('Escape'); await details.waitFor({ state: 'hidden' });
        assert.ok(await hostTrigger.evaluate(node => node === document.activeElement));
      }
    }
    checks.push('256/420 px, light/dark/Claude Code/colorful themes, no overflow and aligned targets');
    // Restart only this isolated remote fixture, never the developer's daemon.
    await remote.crashAndRestart({ beforeRestart: async () => {
      await remoteRow.getByRole('button', { name: /mm · (Reconnecting|Offline)/ }).waitFor();
      assert.equal(await session(remote.info.runtime_id, remoteRoot).count(), 1);
      assert.equal(await session(local.info.runtime_id, localRoot).count(), 1);
      await page.screenshot({ path: join(directory, name + '-offline.png') });
    } });
    await remoteRow.getByRole('button', { name: 'mm · Connected · ' + shared, exact: true }).waitFor();
    checks.push('transport loss retains rows with truthful status and reconnect restores identity');

    // A directory can cross hosts in the order without displacing the reading anchor.
    let newest;
    for (let index = 0; index < 42; index++) newest = await create(clients[0], shared, 'Session ' + index);
    await session(local.info.runtime_id, newest).waitFor();
    const more = sidebar.locator('button[data-sidebar-runtime="' + local.info.runtime_id + '"]').filter({ hasText: /^More$/ });
    await more.waitFor();
    const sessionLinks = sidebar.locator('[data-sidebar-session] a');
    await session(local.info.runtime_id, newest).getByRole('link').click();
    await eventually(async () => await sessionLinks.nth(0).getAttribute('aria-current') === 'page');
    await sessionLinks.nth(1).hover();
    await assertSessionGap(sidebar, 28);
    await page.screenshot({ path: join(directory, name + '-session-gap.png') });
    checks.push('active and hovered sessions stay separated by 1px without shrinking targets');
    for (let count = 0; count < 4; count++) await more.click();
    const saved = sidebar.getByLabel('Saved sessions', { exact: true });
    await saved.evaluate(node => { node.scrollTop = 240; });
    await page.waitForTimeout(150);
    assert.equal(await saved.evaluate(node => node.scrollTop), 240, 'Anchor fixture needs space below the reading position');
    const anchor = await saved.evaluate(node => {
      const top = node.getBoundingClientRect().top;
      const row = [...node.querySelectorAll('[data-sidebar-session]')].find(row => row.getBoundingClientRect().bottom > top);
      return { id: row.dataset.sidebarSession, runtime: row.dataset.sidebarRuntime, y: row.getBoundingClientRect().top - top };
    });
    await new Promise(resolve => setTimeout(resolve, 1100));
    await create(clients[1], shared, 'New remote session above reading position');
    await page.waitForTimeout(2400);
    const after = await session(anchor.runtime, anchor.id).evaluate(node => node.getBoundingClientRect().top - node.closest('[aria-label="Saved sessions"]').getBoundingClientRect().top);
    assert.ok(Math.abs(after - anchor.y) < 2, 'Cross-host directory reorder must preserve the reading anchor: ' + JSON.stringify({ anchor, after, scroll: await saved.evaluate(node => node.scrollTop) }));
    await saved.evaluate(node => { node.scrollTop = 0; });
    await page.waitForTimeout(150);
    assert.equal(await sidebar.locator('[data-sidebar-directory]').first().getAttribute('data-sidebar-runtime'), remote.info.runtime_id);
    checks.push('cross-host directory reorder preserves the visible session anchor');

    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    const sheet = page.getByRole('dialog', { name: 'WHIP', exact: true });
    await sheet.getByLabel('Saved sessions', { exact: true }).evaluate(node => { node.scrollTop = 0; });
    await page.waitForTimeout(150);
    await assertSessionGap(sheet, 44);
    const directoryButtons = sheet.locator('[data-sidebar-directory] > button');
    assert.ok((await directoryButtons.first().boundingBox()).height >= 44);
    assert.equal(await sheet.getByLabel('Saved sessions', { exact: true }).evaluate(node => node.scrollWidth > node.clientWidth), false);
    const mobileHost = sheet.getByRole('button', { name: 'mm · Connected · ' + shared, exact: true });
    assert.ok((await mobileHost.boundingBox()).height >= 44);
    await mobileHost.click();
    const mobileDetails = page.getByRole('dialog', { name: /^mm Connected/ });
    await mobileDetails.waitFor();
    assert.equal(await mobileDetails.evaluate(node => node.scrollWidth > node.clientWidth), false);
    const mobileBox = await mobileDetails.boundingBox();
    assert.ok(mobileBox.x >= 0 && mobileBox.x + mobileBox.width <= 390);
    assert.ok((await mobileDetails.getByRole('button', { name: 'Manage servers' }).boundingBox()).height >= 44);
    await page.screenshot({ path: join(directory, name + '-mobile.png') });
    await page.keyboard.press('Escape');
    checks.push('mobile-web sheet preserves touch targets and avoids horizontal overflow');
    assert.ok(frames.filter(frame => frame.method === 'sessions.summaries').every(frame => frame.params.root_ids.length <= 32));
    assert.equal(errors.length, 0, errors.join('\n'));
    await writeFile(join(directory, name + '-results.json'), JSON.stringify({ checks, errors }, null, 2));
    console.log(name + ': ' + checks.join('; '));
  } finally {
    await browser?.close(); for (const client of clients) client.close();
    for (const fixture of fixtures.reverse()) await fixture.close();
  }
}
