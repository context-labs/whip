import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Real packaged app, isolated synthetic history, and normal durable event delivery.
process.env.WHIP_WEB_REPL_FIXTURE = '1';
const directory = process.env.WHIP_WEB_REPL_RESULTS ?? '/tmp/whip-repl-viewer-results';
await mkdir(directory, { recursive: true });
const results = {};
const leaves = node => node.type === 'pane' ? [node] : [...leaves(node.first), ...leaves(node.second)];
const allTabs = workspace => leaves(workspace.layout).flatMap(pane => pane.tabs);
const axeSource = await readFile(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  console.log(`${name}: starting REPL viewer fixture`);
  const fixture = await startFixture();
  const browser = await ({ chromium, firefox }[name]).launch();
  const context = await browser.newContext({ viewport: { width: 1600, height: 1040 } });
  context.setDefaultTimeout(15_000);
  const page = await context.newPage();
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `repl-${crypto.randomUUID()}`, clientKind: 'human' });
  const runtimeId = fixture.info.runtime_id, root = fixture.info.root_id;
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const url = `${origin}/h/${runtimeId}/s/${root}`;
  const frames = [], errors = [], checks = [];
  const replies = new Set();
  const subscriptions = new Map();
  let maximumSubscriptions = 0;
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', event => { if (event.type() === 'error' && /content.security.policy|violates.*directive/i.test(event.text())) errors.push(event.text()); });
  page.on('websocket', socket => {
    const active = new Set(); subscriptions.set(socket, active);
    socket.on('framesent', ({ payload }) => {
      try {
        const frame = JSON.parse(String(payload)); frames.push(frame);
        if (frame.method === 'events.subscribe') active.add(frame.params.subscription_id);
        if (frame.method === 'events.unsubscribe') active.delete(frame.params.subscription_id);
        maximumSubscriptions = Math.max(maximumSubscriptions, active.size);
      } catch {}
    });
    socket.on('close', () => subscriptions.delete(socket));
    socket.on('framereceived', ({ payload }) => {
      const frame = JSON.parse(String(payload));
      if (frame.id) replies.add(frame.id);
    });
  });
  await context.addInitScript(() => {
    if (!localStorage.getItem('whip.appearance.theme.v1')) localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' }));
  });
  const workspace = () => page.evaluate(() => JSON.parse(sessionStorage.getItem('whip.web.workspace.v3')).workspace);
  const panel = id => page.locator(`[data-workspace-view="${id}"]`);
  const tab = id => page.locator(`[id="whip-workspace-tab-${encodeURIComponent(id)}"]`);
  const chat = id => panel(id).getByRole('region', { name: 'Conversation', exact: true });
  const notebook = id => panel(id).getByRole('region', { name: 'REPL executions', exact: true });
  const ready = async (id, mode) => {
    await (mode === 'repl' ? notebook(id) : panel(id).getByLabel('Message WHIP', { exact: true })).waitFor();
    await expect(page.locator('html')).toHaveAttribute('data-theme', /claude-code|light|dark/);
  };
  const action = async (id, label) => {
    await page.locator(`[data-workspace-tab="${id}"]`).getByRole('button', { name: /^Tab actions for / }).click();
    await page.getByRole('menuitem', { name: label, exact: true }).click();
    await page.getByRole('menu').waitFor({ state: 'hidden' });
  };
  const chooseAgent = async (id, label) => {
    await panel(id).getByRole('button', { name: /^Agent:/ }).click();
    await page.getByRole('dialog', { name: 'Session details', exact: true }).getByRole('link', { name: label === 'Root agent' ? 'root' : label, exact: true }).click();
    await expect(panel(id).getByRole('button', { name: /^Agent:/ })).toContainText(label === 'Root agent' ? 'Root' : label);
  };
  const anchor = region => region.evaluate(element => {
    const top = element.getBoundingClientRect().top;
    const row = [...element.querySelectorAll('[data-reading-id]')].find(item => item.getBoundingClientRect().bottom > top);
    return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - top } : null;
  });
  const sameAnchor = (region, expected) => eventually(async () => {
    const value = await anchor(region);
    return value?.id === expected?.id && Math.abs(value.offset - expected.offset) < 4;
  }, { description: 'saved reading anchor' });
  const backwardReads = agentId => frames.filter(frame => frame.method === 'history.page' && frame.params.agent_id === agentId && frame.params.before_seq > 0);
  const scrollPage = async (region, agentId) => {
    const count = backwardReads(agentId).length;
    await region.dispatchEvent('wheel');
    await eventually(async () => {
      // Newly prepended rows are measured lazily when this position is visited.
      await region.evaluate(element => { element.scrollTop = 350; });
      await region.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      return Math.abs((await region.evaluate(element => element.scrollTop)) - 350) < 1 && await anchor(region);
    }, { description: 'settled position above pagination threshold' });
    // Scroll the real viewport through the threshold. Capture the visible row in
    // the same task, before the scroll handler can start its asynchronous read.
    const before = await region.evaluate(element => {
      element.scrollTop = 200;
      const top = element.getBoundingClientRect().top;
      const row = [...element.querySelectorAll('[data-reading-id]')].find(item => item.getBoundingClientRect().bottom > top);
      return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - top } : null;
    });
    assert.ok(before, 'No visible row before pagination');
    await eventually(() => {
      const reads = backwardReads(agentId);
      return reads.length > count && replies.has(reads.at(-1).id);
    }, { description: 'scroll fetches an older history page' });
    await sameAnchor(region, before);
    assert.equal(backwardReads(agentId).length, count + 1, 'A prepend must not start another page');
    return backwardReads(agentId).at(-1);
  };
  const scrollUp = async region => {
    await region.hover({ position: { x: 8, y: 8 } });
    await page.mouse.wheel(0, -700);
    let previous, stableSince = performance.now();
    return eventually(async () => {
      const current = await anchor(region);
      const away = await region.evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight > 64);
      if (!away || !current || current.id !== previous?.id || Math.abs(current.offset - previous.offset) >= 1) stableSince = performance.now();
      previous = current;
      return away && current && performance.now() - stableSince >= 300 ? current : false;
    }, { description: 'settled reading position after user scroll' });
  };
  const latest = async id => {
    const button = panel(id).getByRole('button', { name: 'Latest', exact: true });
    if (await button.count()) await button.click();
    else await notebook(id).evaluate(element => { element.scrollTop = element.scrollHeight; });
  };
  const step = async value => {
    const response = await fetch(`${fixture.info.frontend}/control/repl/${value}`, { method: 'POST' });
    assert.equal(response.status, 204, `REPL fixture ${value}: ${await response.text()}`);
  };
  const screenshot = async label => { await page.mouse.move(0, 0); await page.screenshot({ path: join(directory, `${name}-${label}.png`) }); };
  try {
    await client.connect();
    await page.goto(url); await ready(root, 'chat');
    await panel(root).getByLabel('Message WHIP', { exact: true }).fill('Keep this draft while I inspect execution evidence.');
    const chatAnchor = await scrollUp(chat(root));
    assert.ok(chatAnchor, 'Chat has no readable anchor');
    const initialSnapshots = frames.filter(frame => frame.method === 'root.snapshot').length;
    const initialHistory = frames.filter(frame => frame.method === 'history.page').length;
    await page.locator(`[data-workspace-tab="${root}"]`).getByRole('button', { name: /^Tab actions for / }).focus();
    await page.keyboard.press('Enter');
    await page.getByRole('menuitem', { name: 'Open REPL', exact: true }).press('Enter');
    const repl = leaves((await workspace()).layout)[0].selected;
    assert.notEqual(repl, root);
    await ready(repl, 'repl');
    await expect(panel(repl)).toBeFocused();
    assert.equal(new URL(page.url()).searchParams.get('view'), 'repl');
    assert.deepEqual(allTabs(await workspace()).map(tab => tab.id), [root, repl], 'REPL must open immediately right of the preserved chat');
    assert.equal(allTabs(await workspace())[0].id, root, 'Opening REPL replaced the stable view ID');
    assert.equal(await panel(repl).locator('[data-whip-composer]').count(), 0);
    assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, initialSnapshots, 'Mode switch acquired another root');
    assert.equal(frames.filter(frame => frame.method === 'history.page').length, initialHistory, 'Mode switch fetched history automatically');
    const savedLink = page.locator('#whip-session-navigation').getByRole('link', { name: 'Inspect Root cell 000.', exact: true });
    assert.equal(new URL(await savedLink.getAttribute('href'), origin).searchParams.get('view'), 'repl');
    await page.getByRole('button', { name: 'Search sessions', exact: true }).click();
    const searchDialog = page.getByRole('dialog', { name: 'Search sessions', exact: true });
    const resultLink = searchDialog.getByRole('link', { name: /^Inspect Root cell 000\./ });
    assert.equal(new URL(await resultLink.getAttribute('href'), origin).searchParams.get('view'), 'repl');
    await page.keyboard.press('Escape'); await searchDialog.waitFor({ state: 'hidden' });
    const lastCell = notebook(repl).locator('[data-repl-cell]').filter({ hasText: 'Root cell 179' });
    await expect(lastCell).toBeVisible();
    await expect(lastCell).toContainText('Completed');
    await expect(lastCell).toContainText('206 steps');
    await expect(notebook(repl).locator('[data-repl-cell]').filter({ hasText: 'Root cell 176' })).toContainText('Failed');
    await lastCell.getByRole('button', { name: 'Show 5 more lines', exact: true }).click();
    await expect(lastCell.getByRole('button', { name: 'Collapse output', exact: true })).toHaveAttribute('aria-expanded', 'true');
    await expect(lastCell.getByRole('region', { name: 'Return value', exact: true })).toContainText('9');
    const replAnchor = await scrollUp(notebook(repl));
    await action(repl, 'Open chat'); await ready(root, 'chat');
    assert.equal(await panel(root).getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft while I inspect execution evidence.');
    await sameAnchor(chat(root), chatAnchor);
    await panel(root).getByRole('button', { name: 'Open REPL', exact: true }).click();
    const toolbarRepl = leaves((await workspace()).layout)[0].selected;
    assert.notEqual(toolbarRepl, repl, 'Toolbar opening must create a fresh view even with an existing REPL');
    assert.deepEqual(allTabs(await workspace()).map(tab => tab.id), [root, toolbarRepl, repl]);
    await ready(toolbarRepl, 'repl');
    await tab(toolbarRepl).press('Delete');
    await tab(repl).click(); await ready(repl, 'repl'); await sameAnchor(notebook(repl), replAnchor);
    checks.push('three-dot menu opens an adjacent REPL; nearest chat, draft and independent reading anchors restore');

    await page.goBack(); await ready(root, 'chat');
    await page.goForward(); await ready(repl, 'repl');
    await page.reload(); await ready(repl, 'repl');
    assert.deepEqual(allTabs(await workspace()).map(tab => tab.kind), ['chat', 'repl']);
    const copied = await context.newPage();
    await copied.goto(page.url()); await copied.getByRole('region', { name: 'REPL executions', exact: true }).waitFor();
    assert.equal(await copied.locator('[data-session-view="repl"]').count(), 1);
    await copied.close();
    await tab(root).click(); await ready(root, 'chat');
    await page.goBack(); await ready(repl, 'repl');
    checks.push('Back/Forward, reload and copied ?view=repl links preserve mode; tab links return to chat');

    const splitAnchor = await scrollUp(notebook(repl));
    await action(repl, 'Split right');
    await eventually(async () => leaves((await workspace()).layout).length === 2, { description: 'REPL split' });
    let state = await workspace();
    const duplicate = leaves(state.layout)[1].selected;
    await ready(duplicate, 'repl');
    await sameAnchor(notebook(duplicate), splitAnchor);
    const subscriptionsBefore = frames.filter(frame => frame.method === 'events.subscribe').length;
    const childReadsBefore = frames.filter(frame => frame.method === 'history.page' && frame.params.agent_id === 'repl-child').length;
    await action(repl, 'Open chat'); await ready(root, 'chat');
    await chooseAgent(duplicate, 'repl-child');
    await expect(notebook(duplicate)).toContainText('Child cell 003');
    assert.equal(await panel(root).getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft while I inspect execution evidence.');
    assert.equal(await panel(root).getByText('Child cell 003', { exact: true }).count(), 0);
    assert.equal(frames.filter(frame => frame.method === 'events.subscribe').length, subscriptionsBefore, 'Child REPL opened another root subscription');
    assert.equal(frames.filter(frame => frame.method === 'history.page' && frame.params.agent_id === 'repl-child').length - childReadsBefore, 1);
    await tab(repl).click(); await ready(repl, 'repl');
    await chooseAgent(repl, 'repl-child');
    await expect(notebook(repl)).toContainText('Child cell 003');
    assert.equal(frames.filter(frame => frame.method === 'history.page' && frame.params.agent_id === 'repl-child').length - childReadsBefore, 1, 'Duplicate child view loaded a second history');
    await chooseAgent(repl, 'Root agent');
    await chooseAgent(duplicate, 'repl-empty');
    await expect(notebook(duplicate)).toContainText('No executions in the loaded history');
    await chooseAgent(duplicate, 'repl-child');
    await step('child');
    await expect(notebook(duplicate)).toContainText('synthetic child failure');
    await expect(notebook(duplicate)).toContainText('Synthetic file unavailable');
    assert.equal(await notebook(repl).getByText('Synthetic file unavailable', { exact: true }).count(), 0);
    await screenshot('split-root-child');
    await page.setViewportSize({ width: 962, height: 1040 });
    await eventually(async () => (await page.locator('[data-workspace-frame]').count()) === 2, { description: 'two minimum-width panes' });
    await eventually(async () => {
      const bounds = await panel(duplicate).boundingBox();
      return bounds.width >= 320 && bounds.width <= 322;
    }, { description: '320px pane geometry after viewport resize' });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    for (const id of [repl, duplicate]) {
      const bar = panel(id).locator('[data-session-info-bar]');
      const status = await bar.getByRole('button', { name: /^Activity:/ }).boundingBox();
      const menu = await bar.getByRole('button', { name: 'Session actions', exact: true }).boundingBox();
      assert(status.x + status.width <= menu.x, 'Narrow status overlaps session controls');
      assert(await bar.evaluate(element => element.scrollWidth <= element.clientWidth), 'Information bar overflows its pane');
    }
    await screenshot('320-pane');
    await page.setViewportSize({ width: 1600, height: 1040 });
    checks.push('chat/REPL duplicates select independent agents; duplicate child history is leased once; empty and failed child evidence is scoped');

    await action(duplicate, 'Move to pane 1');
    await eventually(async () => leaves((await workspace()).layout).length === 1, { description: 'REPL move prunes empty pane' });
    await chooseAgent(duplicate, 'Root agent');
    await action(duplicate, 'Split right');
    state = await workspace();
    const moved = leaves(state.layout)[1].selected;
    await ready(moved, 'repl');
    const source = await tab(moved).boundingBox();
    const target = await panel(duplicate).boundingBox();
    await page.mouse.move(source.x + Math.min(40, source.width / 2), source.y + source.height / 2);
    await page.mouse.down(); await page.mouse.move(source.x + 48, source.y + source.height / 2);
    await page.mouse.move(target.x + target.width / 2, target.y + target.height / 2, { steps: 16 });
    await page.locator('[data-workspace-drop]').waitFor(); await page.mouse.up();
    await eventually(async () => leaves((await workspace()).layout).length === 1, { description: 'dragged REPL joins target pane' });
    await ready(moved, 'repl');
    assert.equal(allTabs(await workspace()).find(value => value.id === moved).kind, 'repl');
    checks.push('split, move and cross-frame drag preserve REPL mode and view identity');

    for (const theme of ['claude-code', 'light', 'dark']) {
      await page.evaluate(id => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id })), theme);
      await page.reload(); await ready(moved, 'repl');
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      await latest(moved);
      await page.route(`${origin}/__repl-axe.js`, route => route.fulfill({ contentType: 'text/javascript', body: axeSource }));
      await page.addScriptTag({ url: `${origin}/__repl-axe.js` });
      const violations = await page.evaluate(async () => (await axe.run(document.body, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(value => ({ id: value.id, nodes: value.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) })));
      assert.deepEqual(violations, [], `${theme} accessibility violations`);
      await screenshot(theme);
    }
    checks.push('Claude Code, light and dark screenshots with WCAG 2/2.1 A/AA Axe checks');

    await chooseAgent(moved, 'repl-paged');
    await expect(panel(moved).getByText(/32 loaded cells$/)).toBeVisible();
    const firstPage = await scrollPage(notebook(moved), 'repl-paged');
    await expect(panel(moved).getByText(/64 loaded cells$/)).toBeVisible();
    const finalPage = await scrollPage(notebook(moved), 'repl-paged');
    assert.ok(finalPage.params.before_seq < firstPage.params.before_seq);
    await expect(panel(moved).getByText(/80 loaded cells$/)).toBeVisible();
    const olderChild = panel(moved).getByRole('button', { name: /Load older executions$/ });
    await expect(olderChild).toHaveCount(0);
    await notebook(moved).evaluate(element => { element.scrollTop = 0; });
    await expect(notebook(moved)).toContainText('Paged cell 000');
    const exhaustedReads = backwardReads('repl-paged').length;
    for (let attempt = 0; attempt < 2; attempt++) {
      const before = frames.length;
      await step('refresh');
      await eventually(() => frames.slice(before).some(frame => frame.method === 'history.page' && frame.params.agent_id === 'repl-paged' && replies.has(frame.id)), { description: 'child metadata refresh completes' });
      await expect(panel(moved).getByText(/80 loaded cells$/)).toBeVisible();
      await expect(olderChild).toHaveCount(0);
      await notebook(moved).evaluate(element => { element.scrollTop = 100; });
      await notebook(moved).evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      assert.equal(backwardReads('repl-paged').length, exhaustedReads, 'Refresh resurrected exhausted pagination');
    }
    checks.push('child REPL scrolls through history with stable anchors and remains exhausted after metadata refresh');

    await chooseAgent(moved, 'repl-sparse');
    await expect(notebook(moved)).toContainText('No executions in the loaded history');
    assert.equal(backwardReads('repl-sparse').length, 0, 'A sparse page must not automatically scan older history');
    await panel(moved).getByRole('button', { name: 'Load older executions', exact: true }).click();
    await expect(panel(moved).getByText(/4 loaded cells$/)).toBeVisible();
    assert.equal(backwardReads('repl-sparse').length, 1);
    await expect(panel(moved).getByRole('button', { name: /Load older executions$/ })).toHaveCount(0);
    checks.push('a page without REPL cells has an explicit bounded fallback to earlier executions');

    await chooseAgent(moved, 'Root agent');
    const rootPage = await scrollPage(notebook(moved), root);
    await action(moved, 'Open chat'); await ready(root, 'chat');
    const chatPage = await scrollPage(chat(root), root);
    assert.ok(chatPage.params.before_seq < rootPage.params.before_seq);
    await tab(moved).click(); await ready(moved, 'repl');
    checks.push('root REPL and main chat both fetch older pages by scrolling through the shared reader');

    const historyBeforePaging = frames.filter(frame => frame.method === 'history.page').length;
    for (let index = 0; index < 4; index++) {
      const older = panel(moved).getByRole('button', { name: 'Load older executions', exact: true });
      if (!await older.count()) break;
      // Activate only one loading path: scrolling to the control first would
      // also request a page through the near-top scroll handler.
      await older.evaluate(button => button.click());
      await eventually(() => frames.filter(frame => frame.method === 'history.page').length > historyBeforePaging + index, { description: 'explicit bounded history page' });
      await eventually(async () => !await older.count() || await older.isEnabled(), { description: 'older page completes or exhausts history' });
      const count = Number((await panel(moved).getByText(/\d+ loaded cells?$/).innerText()).match(/(\d+) loaded cells?/)[1]);
      assert.ok(count <= 128, `Loaded ${count} cells from more than 512 retained messages`);
    }
    const historyRequests = frames.filter(frame => frame.method === 'history.page');
    assert.ok(historyRequests.every(frame => frame.params.limit <= 128 && frame.params.max_bytes <= 256 * 1024));
    assert.ok(await notebook(moved).locator('[data-repl-cell]').count() < 40, 'Notebook retained all cells in the DOM');
    checks.push('history loads only on request in bounded pages and cell rendering remains virtualized');

    const reading = await scrollUp(notebook(moved));
    await step('start');
    await panel(moved).getByRole('button', { name: 'Latest', exact: true }).waitFor();
    await sameAnchor(notebook(moved), reading);
    await latest(moved);
    const live = notebook(moved).locator('[data-repl-cell]').filter({ hasText: 'live' }).last();
    await expect(live).toContainText('Writing');
    await expect(live).toContainText('print("live');
    await step('progress');
    await expect(live).toContainText('Running');
    await expect(live).toContainText('live line 8');
    assert.equal(await live.getByText('files.list', { exact: false }).count(), 3, 'Expected code plus two distinct host calls');
    await expect(live).toContainText('12ms');
    await expect(live).toContainText('8ms');
    await expect(live.getByRole('region', { name: 'Output · last 6 lines (2 earlier)', exact: true })).not.toContainText('live line 1');
    await step('complete');
    await expect(live).toContainText('Completed');
    await expect(live).toContainText('42 steps');
    await expect(notebook(moved).locator('[data-repl-restart]')).toContainText('Restarted · restored 2');
    await live.getByRole('button', { name: 'Show 2 more lines', exact: true }).click();
    await expect(live).toContainText('live line 8');
    await screenshot('live-evidence');
    checks.push('partial code, cumulative output tail, repeated host calls, completion and restart arrive live without stealing the reading anchor');

    await page.setViewportSize({ width: 390, height: 844 });
    await ready(moved, 'repl');
    assert.equal(await page.locator('[data-workspace-frame]').count(), 1);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await screenshot('mobile');
    const mobileAction = async (id, label) => {
      await page.getByRole('button', { name: /^Open sessions:/ }).click();
      const picker = page.getByRole('dialog', { name: 'Open sessions', exact: true });
      const index = allTabs(await workspace()).findIndex(value => value.id === id);
      await picker.getByRole('button', { name: /^Tab actions for / }).nth(index).click();
      await page.getByRole('menuitem', { name: label, exact: true }).click();
      await picker.waitFor({ state: 'hidden' });
    };
    await mobileAction(moved, 'Open chat'); await ready(root, 'chat');
    assert.equal(await panel(root).getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft while I inspect execution evidence.');
    await mobileAction(root, 'Open REPL');
    const mobileRepl = leaves((await workspace()).layout)[0].selected;
    assert.notEqual(mobileRepl, moved);
    await ready(mobileRepl, 'repl');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    assert.ok(maximumSubscriptions <= 1, `Observed ${maximumSubscriptions} root subscriptions for one root`);
    assert.equal(frames.filter(frame => frame.method === 'command.submit').length, 0, 'Viewer interaction submitted a daemon command');
    assert.deepEqual(await fixture.effects(), [], 'Opening views executed fixture runner work');
    assert.deepEqual(errors, []);
    checks.push('mobile picker opens fresh REPL views; viewer actions open one shared root subscription and never submit commands');
    results[name] = { browser: await browser.version(), maximumSubscriptions, historyRequests: historyRequests.length, checks };
    console.log(`${name}: ${checks.length} REPL viewer workflows passed`);
  } catch (error) {
    await screenshot('failure').catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`);
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify(frames, null, 2));
    throw error;
  } finally { client.close(); await browser.close(); await fixture.close(); }
}
await writeFile(join(directory, 'repl-viewer.json'), JSON.stringify(results, null, 2));
