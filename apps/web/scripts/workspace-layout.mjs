import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// The production app against an isolated synthetic history/agent fixture.
process.env.WHIP_WEB_PERF_FIXTURE = '1';
const directory = process.env.WHIP_WEB_LAYOUT_RESULTS ?? '/tmp/whip-workspace-layout-results';
await mkdir(directory, { recursive: true });
const results = {};
const leaves = node => node.type === 'pane' ? [node] : [...leaves(node.first), ...leaves(node.second)];
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  console.log(`${name}: starting split workspace fixture`);
  const fixture = await startFixture();
  const browser = await ({ chromium, firefox }[name]).launch();
  const context = await browser.newContext({ viewport: { width: 1800, height: 1120 } });
  context.setDefaultTimeout(15_000);
  const page = await context.newPage();
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `layout-${crypto.randomUUID()}`, clientKind: 'human' });
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const runtimeId = fixture.info.runtime_id, root = fixture.info.root_id;
  const route = id => `/h/${runtimeId}/s/${id}`;
  const frames = [], errors = [], checks = [];
  const sockets = new Map(); let maximumSubscriptions = 0;
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', event => { if (event.type() === 'error' && /content.security.policy|violates.*directive/i.test(event.text())) errors.push(event.text()); });
  page.on('websocket', socket => {
    const active = new Set(); sockets.set(socket, active);
    socket.on('framesent', ({ payload }) => {
      try {
        const frame = JSON.parse(String(payload)); frames.push(frame);
        if (frame.method === 'events.subscribe') active.add(frame.params.subscription_id);
        if (frame.method === 'events.unsubscribe') active.delete(frame.params.subscription_id);
        maximumSubscriptions = Math.max(maximumSubscriptions, active.size);
      } catch {}
    });
    socket.on('close', () => sockets.delete(socket));
  });
  const workspace = () => page.evaluate(id => JSON.parse(sessionStorage.getItem('whip.web.workspace.v2')).workspaces.find(w => w.runtimeId === id), runtimeId);
  const tab = id => page.locator(`[id="whip-workspace-tab-${encodeURIComponent(id)}"]`);
  const panel = id => page.locator(`[data-workspace-view="${id}"]`);
  const ready = id => panel(id).locator('[data-whip-composer]').waitFor();
  const action = async (id, label) => { await tab(id).click({ button: 'right' }); await page.getByRole('menuitem', { name: label, exact: true }).click(); await page.getByRole('menu').waitFor({ state: 'hidden' }); };
  const countPanes = count => eventually(async () => (await page.locator('[data-workspace-frame]').count()) === count, { description: `${count} visible panes` });
  try {
    await client.connect();
    const extras = [];
    for (let i = 0; i < 4; i++) {
      const result = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
      assert.equal(result.status, 'succeeded'); extras.push(result.result.root_id);
      await client.session(result.result.root_id).rename(`Workspace example ${i + 1}`).result();
    }
    await context.addInitScript(seed => {
      if (!sessionStorage.getItem('whip.web.tabs.v1')) sessionStorage.setItem('whip.web.tabs.v1', JSON.stringify(seed));
    }, { version: 1, workspaces: [{ runtimeId, tabs: [root, ...extras].map(rootId => ({ rootId, titleHint: '', location: {} })), lastActiveRootId: root }] });
    await page.goto(origin + route(root)); await ready(root);
    await panel(root).getByRole('region', { name: 'Conversation', exact: true }).waitFor();
    await action(root, 'Split right'); await countPanes(2);
    let state = await workspace();
    const duplicate = leaves(state.layout)[1].selected;
    await ready(duplicate);
    assert.equal(frames.filter(f => f.method === 'root.snapshot').length, 1, 'Duplicating a chat hydrated the root twice');
    await panel(root).getByLabel('Message WHIP', { exact: true }).fill('A shared draft in two independent views.');
    assert.equal(await panel(duplicate).getByLabel('Message WHIP', { exact: true }).inputValue(), 'A shared draft in two independent views.');
    const rootScroll = panel(root).getByRole('region', { name: 'Conversation', exact: true });
    const duplicateScroll = panel(duplicate).getByRole('region', { name: 'Conversation', exact: true });
    await rootScroll.evaluate(el => { el.scrollTop = 220; });
    await duplicateScroll.evaluate(el => { el.scrollTop = 900; });
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    assert.ok(Math.abs((await rootScroll.evaluate(el => el.scrollTop)) - (await duplicateScroll.evaluate(el => el.scrollTop))) > 100);
    checks.push('duplicate root shares one subscription and draft, with independent scroll');

    // Identical URLs still carry independent view identities through browser history.
    await tab(root).click(); await tab(duplicate).click();
    assert.equal((await workspace()).focusedPaneId, leaves((await workspace()).layout)[1].id);
    await page.goBack();
    await eventually(async () => (await workspace()).focusedPaneId === 'main', { description: 'Back focuses original duplicate URL' });
    await page.goForward();
    await tab(root).click();
    await page.locator(`[data-workspace-tab="${duplicate}"]`).getByRole('button', { name: /^Tab actions for / }).click();
    await page.getByRole('menuitem', { name: 'Session details', exact: true }).click();
    await page.getByRole('link', { name: 'perf-child-000', exact: true }).click();
    await panel(duplicate).getByLabel('Message this agent', { exact: true }).waitFor();
    const childModel = panel(duplicate).getByRole('button', { name: 'Model and reasoning', exact: true });
    await childModel.click();
    const childModelDetails = page.getByRole('dialog', { name: 'Agent model', exact: true });
    await childModelDetails.waitFor();
    assert.equal(await childModelDetails.getByRole('button', { name: 'Apply model', exact: true }).count(), 0, 'Child composer offered to change the root model');
    await childModel.click(); await childModelDetails.waitFor({ state: 'hidden' });
    assert.equal(await panel(root).getByLabel('Message WHIP', { exact: true }).count(), 1);
    assert.equal(await panel(root).getByText(/perf-child-000 message/).count(), 0);
    await panel(duplicate).getByRole('link', { name: 'Root conversation', exact: true }).click();
    await ready(duplicate);
    checks.push('same-URL Back/Forward and independent child-agent selection');

    await action(root, 'Split down'); await countPanes(3);
    state = await workspace();
    const lower = leaves(state.layout)[1].selected;
    await ready(lower);
    await panel(lower).locator('[data-whip-composer]').evaluate(el => { window.__movingComposer = el; });
    await action(lower, 'Move to pane 3'); await countPanes(2); await ready(lower);
    assert.equal(await panel(lower).locator('[data-whip-composer]').evaluate(el => el === window.__movingComposer), true, 'Moving selected view remounted its composer');
    assert.equal(await panel(lower).getByLabel('Message WHIP', { exact: true }).inputValue(), 'A shared draft in two independent views.');
    checks.push('nested split and move prune empty pane while retaining selected view DOM');

    // Real cross-pane drag keeps the selected view mounted and supports Escape.
    const before = JSON.stringify((await workspace()).layout);
    const source = await tab(lower).boundingBox();
    const target = await panel(root).boundingBox();
    await page.mouse.move(source.x + Math.min(40, source.width / 2), source.y + source.height / 2);
    await page.mouse.down(); await page.mouse.move(source.x + Math.min(40, source.width / 2) + 8, source.y + source.height / 2);
    await page.mouse.move(target.x + target.width / 2, target.y + target.height / 2, { steps: 16 });
    await page.locator('[data-workspace-drop]').waitFor();
    await page.keyboard.press('Escape'); await page.mouse.up();
    await page.locator('[data-dragging]').waitFor({ state: 'hidden' });
    assert.equal(JSON.stringify((await workspace()).layout), before);
    await page.mouse.move(source.x + Math.min(40, source.width / 2), source.y + source.height / 2);
    await page.mouse.down(); await page.mouse.move(source.x + Math.min(40, source.width / 2) + 8, source.y + source.height / 2);
    await page.mouse.move(target.x + target.width / 2, target.y + target.height / 2, { steps: 16 });
    await page.locator('[data-workspace-drop]').waitFor(); await page.mouse.up();
    await eventually(async () => leaves((await workspace()).layout)[0].selected === lower, { description: 'cross-pane drag selects moved view' });
    assert.equal(await panel(lower).locator('[data-whip-composer]').evaluate(el => el === window.__movingComposer), true);
    checks.push('production tab dragging and cancellation retain view identity');

    await action(lower, 'Split down'); await countPanes(3);
    state = await workspace();
    const bottom = leaves(state.layout)[1].selected;
    await ready(bottom);
    await page.getByRole('button', { name: 'Pane 3 actions', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Focus pane 1', exact: true }).click();
    await page.getByRole('menu').waitFor({ state: 'hidden' });
    await eventually(async () => (await workspace()).focusedPaneId === 'main', { description: 'pane menu focuses destination' });
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+Shift+l' : 'Control+Shift+l');
    assert.equal(await panel(lower).locator('[data-whip-composer]').evaluate(el => el === document.activeElement), true, 'Composer shortcut escaped the focused pane');
    await tab(bottom).click();
    await page.mouse.move(0, 0);
    await page.screenshot({ path: join(directory, `${name}-nested-light.png`) });
    const divider = page.getByRole('separator', { name: 'Resize panes horizontally', exact: true });
    const ratioBefore = (await workspace()).layout.ratio;
    await divider.focus(); await page.keyboard.press('ArrowRight');
    await eventually(async () => (await workspace()).layout.ratio !== ratioBefore, { description: 'keyboard splitter commits size' });
    const verticalDivider = page.getByRole('separator', { name: 'Resize panes vertically', exact: true });
    const verticalBefore = (await workspace()).layout.first.ratio;
    await verticalDivider.focus(); await page.keyboard.press('ArrowUp');
    await eventually(async () => (await workspace()).layout.first.ratio < verticalBefore, { description: 'vertical keyboard splitter commits size' });
    const verticalBounds = await verticalDivider.boundingBox();
    const beforeVerticalDrag = (await workspace()).layout.first.ratio;
    await page.mouse.move(verticalBounds.x + verticalBounds.width / 2, verticalBounds.y - 2); await page.mouse.down();
    await page.mouse.move(verticalBounds.x + verticalBounds.width / 2, verticalBounds.y - 65, { steps: 12 }); await page.mouse.up();
    await eventually(async () => (await workspace()).layout.first.ratio < beforeVerticalDrag - .03, { description: 'vertical pointer splitter commits size from its extended hit target' });
    const saved = (await workspace()).layout;
    await page.setViewportSize({ width: 390, height: 844 }); await countPanes(1);
    assert.equal(await page.locator('[data-whip-composer]').count(), 1);
    assert.equal(await page.getByRole('separator', { name: /^Resize panes/ }).count(), 0);
    assert.deepEqual((await workspace()).layout, saved);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.screenshot({ path: join(directory, `${name}-mobile.png`) });
    await page.setViewportSize({ width: 1800, height: 1120 }); await countPanes(3);
    assert.deepEqual((await workspace()).layout, saved);
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await countPanes(3); await ready(bottom);
    assert.deepEqual((await workspace()).layout, saved);
    await page.screenshot({ path: join(directory, `${name}-nested-dark.png`) });
    await page.getByRole('separator', { name: 'Resize session navigation', exact: true }).focus();
    await page.keyboard.press('End');
    await countPanes(3);
    await page.screenshot({ path: join(directory, `${name}-sidebar-420.png`) });
    await page.getByRole('separator', { name: 'Resize session navigation', exact: true }).focus();
    await page.keyboard.press('Home');
    checks.push('keyboard resize, narrow presentation and full reload preserve the layout');

    // Three more distinct selected roots reach the budget; switching one must
    // release its old lease before opening a fifth distinct root.
    await action(extras[0], 'Move to pane 2'); await ready(extras[0]);
    await action(extras[1], 'Move to pane 3'); await ready(extras[1]);
    await action(lower, 'Split right'); await countPanes(4);
    await action(extras[2], 'Move to pane 2'); await ready(extras[2]);
    await tab(extras[3]).click(); await ready(extras[3]);
    await eventually(() => frames.filter(f => f.method === 'root.snapshot').length >= 5, { description: 'five roots visited under four-root budget' });
    const fresh = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
    assert.equal(fresh.status, 'succeeded');
    // Navigate in the same app without reloading its live root leases.
    await client.session(fresh.result.root_id).rename('Fresh split navigation').result();
    await page.getByRole('link', { name: 'Fresh split navigation', exact: true }).click();
    await ready(fresh.result.root_id);
    assert.ok(maximumSubscriptions <= 4, `Observed ${maximumSubscriptions} subscriptions`);
    assert.equal(await page.getByRole('alert').filter({ hasText: 'Four session views' }).count(), 0);
    assert.equal(frames.filter(f => f.method === 'command.submit').length, 0, 'Layout interaction sent durable commands');
    const working = client.session(fresh.result.root_id).submit({ text: 'hold:tool-stream' });
    await working.accepted();
    await panel(fresh.result.root_id).getByRole('button', { name: 'Stop this turn', exact: true }).waitFor();
    // Minimum-size panes must keep controls reachable even when extra notices
    // and the active-turn delivery selector leave no space for a transcript.
    await page.getByRole('separator', { name: 'Resize panes horizontally', exact: true }).last().focus();
    await page.keyboard.press('Home');
    await page.getByRole('separator', { name: 'Resize panes vertically', exact: true }).focus();
    await page.keyboard.press('Home');
    const smallPanel = panel(fresh.result.root_id);
    await smallPanel.evaluate(el => { el.scrollTop = el.scrollHeight; });
    const sendBounds = await smallPanel.getByRole('button', { name: 'Send message', exact: true }).boundingBox();
    const paneBounds = await smallPanel.boundingBox();
    assert.ok(sendBounds.x >= paneBounds.x && sendBounds.x + sendBounds.width <= paneBounds.x + paneBounds.width + 1, 'Narrow pane clips composer controls');
    assert.ok(sendBounds.y >= paneBounds.y && sendBounds.y + sendBounds.height <= paneBounds.y + paneBounds.height + 1, 'Short pane makes composer unreachable');
    await page.screenshot({ path: join(directory, `${name}-minimum-pane.png`) });
    await fixture.release('tool-stream'); await working.result();
    checks.push('minimum-size active panes retain reachable composer controls');
    assert.deepEqual(errors, []);
    checks.push('four-pane admission and replacing roots stay within four subscriptions without daemon commands');
    results[name] = { browser: await browser.version(), maximumSubscriptions, checks };
    console.log(`${name}: ${checks.length} split workspace workflows passed`);
  } catch (error) {
    await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`);
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify(frames.filter(f => /events\.|root.snapshot|history/.test(f.method)), null, 2));
    throw error;
  } finally { client.close(); await browser.close(); await fixture.close(); }
}
await writeFile(join(directory, 'workspace-layout.json'), JSON.stringify(results, null, 2));
