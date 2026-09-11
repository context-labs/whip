import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { chromium, firefox, expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Production renderer + isolated daemon histories. No product state is injected.
const directory = process.env.WHIP_SETTINGS_CONVERSATION_RESULTS ?? '/tmp/whip-settings-conversation-results';
await mkdir(directory, { recursive: true });
const leaves = node => node.type === 'pane' ? [node] : [...leaves(node.first), ...leaves(node.second)];
const engines = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
const results = {};
async function scenario(engine, mode, run) {
  if (process.env.WHIP_SETTINGS_CONVERSATION_MODE && process.env.WHIP_SETTINGS_CONVERSATION_MODE !== mode) return;
  process.env.WHIP_WEB_REPL_FIXTURE = mode === 'repl' ? '1' : '';
  process.env.WHIP_WEB_PERF_FIXTURE = mode === 'body' ? '1' : '';
  const manifest = JSON.parse(await readFile(new URL('../renderer-manifest.json', import.meta.url), 'utf8'));
  const fixture = await startFixture({ lifetimeMs: 600_000 });
  const browser = await ({ chromium, firefox }[engine]).launch();
  const page = await browser.newPage({ viewport: { width: 1800, height: 1120 } });
  page.setDefaultTimeout(15_000);
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `settings-${crypto.randomUUID()}`, clientKind: 'human' });
  const frames = [], httpReads = [], replies = new Set(), errors = [], csp = [], checks = [];
  const subscriptions = new Map();
  page.on('pageerror', error => errors.push(error.message));
  page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/v3/content/')) httpReads.push(request.url()); });
  await page.addInitScript(() => { window.__settingsCSP = []; document.addEventListener('securitypolicyviolation', event => window.__settingsCSP.push(event.violatedDirective)); });
  page.on('websocket', socket => {
    const active = new Set(); subscriptions.set(socket, active);
    socket.on('framesent', ({ payload }) => {
      const value = JSON.parse(String(payload)); frames.push(value);
      if (value.method === 'events.subscribe') active.add(value.params.subscription_id);
      if (value.method === 'events.unsubscribe') active.delete(value.params.subscription_id);
    });
    socket.on('framereceived', ({ payload }) => { const value = JSON.parse(String(payload)); if (value.id && !value.method) replies.add(value.id); });
    socket.on('close', () => subscriptions.delete(socket));
  });
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const root = fixture.info.root_id;
  const rootURL = `${origin}/h/${fixture.info.runtime_id}/s/${root}`;
  const workspace = () => page.evaluate(() => JSON.parse(sessionStorage.getItem('whip.web.workspace.v3')).workspace);
  const panel = id => page.locator(`[data-workspace-view="${id}"]`);
  const chat = id => panel(id).getByRole('region', { name: 'Conversation', exact: true });
  const notebook = id => panel(id).getByRole('region', { name: 'REPL executions', exact: true });
  const tab = id => page.locator(`[id="whip-workspace-tab-${encodeURIComponent(id)}"]`);
  const contentReads = () => httpReads.length + frames.filter(frame => frame.method === 'content.read').length;
  const action = async (id, label) => {
    await page.locator(`[data-workspace-tab="${id}"]`).getByRole('button', { name: /^Tab actions for / }).click();
    await page.getByRole('menuitem', { name: label, exact: true }).click();
    await page.getByRole('menu').waitFor({ state: 'hidden' });
  };
  let checkedRelease = false;
  const settings = async () => {
    await page.locator('#whip-settings-link').click();
    await expect(page.getByRole('heading', { name: 'Appearance', exact: true })).toBeVisible();
    await expect(page.locator('[data-workspace-view]')).toHaveCount(0);
    if (!checkedRelease) {
      await eventually(() => [...subscriptions.values()].every(active => active.size === 0), { timeout: 40_000, description: 'Settings releases root subscriptions after the existing 30-second idle grace' });
      checkedRelease = true; checks.push('Settings removes transcript consumers and releases idle root subscriptions after the bounded grace');
    }
  };
  const back = async () => { await page.getByRole('button', { name: 'Back to workspace', exact: true }).click(); await page.locator('[data-workspace-frame]').first().waitFor(); };
  const density = async value => {
    const slider = page.getByRole('slider', { name: 'Tool call density', exact: true });
    await slider.focus(); await page.keyboard.press('Home');
    for (let step = 0; step < value; step++) await page.keyboard.press('ArrowRight');
    await expect(slider).toHaveAttribute('aria-valuetext', ['Compact', 'Comfortable', 'Detailed'][value]);
  };
  const select = async (scope, label, value) => { await scope.getByRole('combobox', { name: label, exact: true }).click(); await page.getByRole('option', { name: value, exact: true }).click(); };
  const anchor = region => region.evaluate(element => {
    const top = element.getBoundingClientRect().top;
    const row = [...element.querySelectorAll('[data-reading-id]')].find(item => item.getBoundingClientRect().bottom > top);
    return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - top } : null;
  });
  const stableAnchor = async region => {
    let previous, since = performance.now();
    return eventually(async () => {
      const value = await anchor(region);
      if (!value || value.id !== previous?.id || Math.abs(value.offset - previous.offset) >= 1) since = performance.now();
      previous = value;
      return value && performance.now() - since >= 350 ? value : false;
    }, { description: 'settled reading anchor' });
  };
  const exactReturn = async (label, ready) => {
    const url = page.url(), saved = await workspace();
    await settings();
    assert.deepEqual((await workspace()).layout, saved.layout, `${label}: Settings changed the split layout`);
    await back(); await ready();
    await expect(page).toHaveURL(url);
    const returned = await workspace();
    assert.deepEqual(returned.layout, saved.layout, `${label}: Back changed the split layout`);
    assert.equal(returned.focusedPaneId, saved.focusedPaneId, `${label}: Back changed focused pane`);
    checks.push(`${label}: exact URL, agent/mode/view identity, split layout and focus restored`);
  };
  try {
    await Promise.all(Object.entries(manifest.files).map(async ([path, file]) => {
      const response = await fetch(`${origin}/${path}`);
      assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256, `Fixture renderer differs: ${path}`);
    }));
    await client.connect();
    await page.goto(rootURL);
    await panel(root).locator('[data-whip-composer]').waitFor();
    await page.evaluate(() => document.fonts.ready);
    await run({ fixture, page, client, root, rootURL, frames, replies, checks, workspace, panel, chat, notebook, tab, action, settings, back, density, select, anchor, stableAnchor, exactReturn, contentReads });
    assert.deepEqual(errors, []);
    csp.push(...await page.evaluate(() => window.__settingsCSP)); assert.deepEqual(csp, []);
    await page.screenshot({ path: join(directory, `${engine}-${mode}.png`) });
    results[`${engine}-${mode}`] = { rendererDigest: manifest.digest, checks, pageErrors: errors, cspErrors: csp, frameCount: frames.length, contentReads: contentReads() };
    await writeFile(join(directory, 'results.json'), JSON.stringify(results, null, 2));
    console.log(`${engine}/${mode}: ${checks.length} production conversation checks passed`);
  } catch (error) {
    await page.screenshot({ path: join(directory, `${engine}-${mode}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${engine}-${mode}-failure.txt`), `${error.stack}\nCompleted checks: ${JSON.stringify(checks)}\nPage errors: ${JSON.stringify(errors)}\n${await page.locator('body').innerText().catch(() => '')}`);
    throw error;
  } finally { client.close(); await browser.close(); await fixture.close(); }
}

for (const engine of engines) {
  await scenario(engine, 'repl', async ({ fixture, page, client, root, frames, replies, checks, workspace, panel, chat, notebook, tab, action, settings, back, density, select, stableAnchor, exactReturn, contentReads }) => {
    await panel(root).locator('[data-whip-composer]').fill('Root draft survives Settings and split changes.');
    const tools = () => chat(root).locator('[data-message-role="tool"] details');
    await expect(tools().last()).not.toHaveAttribute('open');
    const reads = contentReads();
    await settings(); await density(1); await back();
    await expect(chat(root).locator('[data-tool-preview]').last()).toBeVisible();
    const preview = await chat(root).locator('[data-tool-preview]').last().textContent();
    assert.ok(preview.length <= 512 && preview.split('\n').length <= 3, 'Comfortable real-tool preview exceeded bounds');
    await settings(); await density(2); await back();
    await expect(tools().last()).toHaveAttribute('open', '');
    assert.equal(contentReads(), reads, 'Tool density automatically read a stored body');
    checks.push('real recorded tools obey Compact/Comfortable/Detailed; previews are bounded and density performs no content reads');

    const step = async name => { const response = await fetch(`${fixture.info.frontend}/control/repl/${name}`, { method: 'POST' }); assert.equal(response.status, 204); };
    await step('start');
    const live = chat(root).locator('[data-message-id="live-tool:browser-live"] details');
    await expect(live).toHaveAttribute('open', '');
    await live.locator('summary').click(); await expect(live).not.toHaveAttribute('open');
    await step('progress'); await expect(live).toContainText('in progress'); await expect(live).not.toHaveAttribute('open');
    await step('complete'); await expect(live).not.toContainText('in progress'); await expect(live).not.toHaveAttribute('open');
    await live.locator('summary').click(); await expect(live).toHaveAttribute('open', ''); await expect(live).toContainText('live line 8');
    assert.equal(contentReads(), reads, 'Explicit bounded disclosure read a body');
    checks.push('manual disclosure overrides Detailed default during real in-progress/output/completion events');

    await action(root, 'Session details');
    await page.getByRole('link', { name: 'repl-child', exact: true }).click();
    await panel(root).getByLabel('Message this agent', { exact: true }).fill('Child draft is separate from the root.');
    await exactReturn('child chat', () => panel(root).getByLabel('Message this agent', { exact: true }).waitFor());
    await expect(panel(root).getByLabel('Message this agent', { exact: true })).toHaveValue('Child draft is separate from the root.');
    await action(root, 'Open REPL');
    const repl = leaves((await workspace()).layout)[0].selected;
    await notebook(repl).waitFor();
    await exactReturn('child REPL', () => notebook(repl).waitFor());
    await expect(panel(repl).getByRole('button', { name: 'Agent: repl-child', exact: true })).toBeVisible();
    await action(repl, 'Split right');
    await eventually(async () => leaves((await workspace()).layout).length === 2, { description: 'two settings-return panes' });
    const duplicate = leaves((await workspace()).layout)[1].selected;
    await notebook(duplicate).waitFor();
    await tab(root).click();
    await panel(root).locator('[data-session-info-bar]').getByRole('button', { name: 'Session actions', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Root conversation', exact: true }).click();
    await panel(root).getByLabel('Message WHIP', { exact: true }).waitFor();
    await tab(duplicate).click();
    await exactReturn('split root chat and child REPL', async () => { await notebook(duplicate).waitFor(); await chat(root).waitFor(); });
    await expect(panel(root).getByLabel('Message WHIP', { exact: true })).toHaveValue('Root draft survives Settings and split changes.');
    checks.push('root and child drafts stay isolated across all Settings round trips');

    await tab(root).click();
    await chat(root).evaluate(element => { element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
    await stableAnchor(chat(root));
    await chat(root).evaluate(element => {
      const top = element.getBoundingClientRect().top;
      const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.querySelector('[data-message-role="assistant"]') && row.getBoundingClientRect().top > top);
      if (!row) throw new Error('No assistant reading anchor');
      element.scrollTop += row.getBoundingClientRect().top - top + 20;
    });
    const before = await stableAnchor(chat(root));
    await settings();
    const ui = page.getByRole('textbox', { name: 'UI font size', exact: true }); await ui.fill('20'); await ui.press('Tab');
    const code = page.getByRole('textbox', { name: 'Code font size', exact: true }); await code.fill('24'); await code.press('Tab');
    if (!await page.getByRole('switch', { name: 'Code block word wrap', exact: true }).isChecked()) await page.getByRole('switch', { name: 'Code block word wrap', exact: true }).click();
    await density(1); await back(); await chat(root).waitFor();
    const after = await stableAnchor(chat(root));
    assert.equal(after.id, before.id, 'Appearance replaced an older-history reading anchor');
    assert.ok(Math.abs(after.offset - before.offset) < 6, `Appearance shifted the reading anchor: ${JSON.stringify({ before, after })}`);
    await expect(panel(root).getByRole('button', { name: 'Latest', exact: true })).toBeVisible();
    checks.push(`older history retains message and offset after font/code-size/wrap/density changes (${before.id}, drift ${Math.abs(after.offset - before.offset).toFixed(2)}px)`);
    await panel(root).getByRole('button', { name: 'Latest', exact: true }).click();
    await eventually(() => chat(root).evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64));
    await settings(); await density(0); await back();
    await eventually(() => chat(root).evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'follow-latest retained after Settings' });
    const start = frames.length;
    await client.session(root).submit({ text: 'New work keeps following after Appearance changes.' }).result({ signal: AbortSignal.timeout(15_000) });
    await eventually(() => frames.slice(start).some(frame => frame.method === 'root.snapshot' && replies.has(frame.id)), { description: 'new message snapshot' });
    await eventually(() => chat(root).evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'following new output' });
    checks.push('follow-latest survives Settings and follows subsequent actual daemon output');
  });

  await scenario(engine, 'body', async ({ page, root, frames, replies, chat, settings, density, back, contentReads, checks }) => {
    await settings(); await density(2); await back();
    // The initial snapshot stops after the oversized record. Load its history
    // envelope explicitly, then inspect it near the end of the now-loaded page.
    const beforePage = frames.length;
    await chat(root).getByRole('button', { name: 'Load earlier messages', exact: true }).click();
    await eventually(() => frames.slice(beforePage).some(frame => frame.method === 'history.page' && replies.has(frame.id)), { description: 'oversized history envelope loaded' });
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    await chat(root).evaluate(element => { element.scrollTop = element.scrollHeight; });
    await eventually(() => chat(root).evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'settled latest body fixture position' });
    const stored = chat(root).getByRole('button', { name: /^Read stored message · / });
    for (let attempt = 0; attempt < 30 && !await stored.count(); attempt++) {
      await chat(root).evaluate(element => { element.scrollTop -= 400; });
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    }
    await expect(stored.first()).toBeVisible();
    assert.equal(contentReads(), 0, 'Detailed tool presentation automatically fetched an oversized body');
    const row = stored.first().locator('xpath=ancestor::article');
    assert.ok(!await row.innerText().then(text => text.includes('Large tool output stays on the host.')), 'Oversized body leaked into automatic details');
    await stored.first().click();
    const dialog = page.getByRole('dialog', { name: 'Stored message', exact: true });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('alert')).toContainText('Content exceeds the requested byte limit');
    assert.equal(contentReads(), 0, 'Oversized preview bypassed the 1 MiB bound');
    const download = page.waitForEvent('download');
    await dialog.getByRole('button', { name: 'Download stored message', exact: true }).click();
    const downloaded = await download;
    assert.equal(downloaded.suggestedFilename(), 'whip-message.json');
    assert.equal(await downloaded.failure(), null);
    await eventually(() => contentReads() > 0, { description: 'explicit body download proves content-read observer' });
    checks.push('actual oversized tool body stays behind a handle in Detailed mode; preview respects its byte limit, and only explicit download reads content');
  });
}
