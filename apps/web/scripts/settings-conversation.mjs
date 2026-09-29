import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { chromium, firefox, expect } from '@playwright/test';
import { defineAgent } from '../../../packages/sdk/dist/agents.js';
import { deadline, eventually, startFixture } from './native-fixture.mjs';
import { startHistoryFixture } from './native-history-fixture.mjs';

// Production renderer/runtime and canonical synthetic store history. Live cells
// below execute normally; historical seed records do not claim prior effects.
const directory = process.env.WHIP_SETTINGS_CONVERSATION_RESULTS ?? '/tmp/whip-settings-conversation-results';
await mkdir(directory, { recursive: true });
const leaves = node => node.type === 'pane' ? [node] : [...leaves(node.first), ...leaves(node.second)];
const engines = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
const results = {};
async function scenario(engine, mode, run) {
  if (process.env.WHIP_SETTINGS_CONVERSATION_MODE && process.env.WHIP_SETTINGS_CONVERSATION_MODE !== mode) return;
  const manifest = JSON.parse(await readFile(new URL('../renderer-manifest.json', import.meta.url), 'utf8'));
  const started = performance.now();
  const lifecycle = (stage, extra = {}) => console.error(JSON.stringify({ event: 'settings-conversation-lifecycle', engine, mode, stage, elapsed_ms: Math.round(performance.now() - started), ...extra }));
  lifecycle('starting-fixture');
  const fixture = await (mode === 'body' ? startHistoryFixture : startFixture)({ lifetimeMs: 600_000, executeCode: true });
  lifecycle('fixture-ready', { process_epoch: fixture.info.process_epoch });
  let browser;
  try {
    browser = await ({ chromium, firefox }[engine]).launch();
    const page = await browser.newPage({ viewport: { width: 1800, height: 1120 } });
    page.setDefaultTimeout(15_000);
    const client = await fixture.connect(`settings-${crypto.randomUUID()}`);
    const frames = [], replies = new Set(), errors = [], csp = [], checks = [];
    const problem = text => { if (errors.length < 64) errors.push(text.slice(0, 2048)); };
    let httpReads = 0;
    let lastObserve = 0;
    page.on('pageerror', error => problem(error.message));
    page.on('request', request => { if (new URL(request.url()).pathname.startsWith('/api/v4/content/')) httpReads++; });
    await page.addInitScript(() => { window.__settingsCSP = []; document.addEventListener('securitypolicyviolation', event => { if (window.__settingsCSP.length < 64) window.__settingsCSP.push(event.violatedDirective); }); });
    page.on('websocket', socket => {
      socket.on('framesent', ({ payload }) => {
        const value = JSON.parse(String(payload));
        if (frames.length === 25_000) { problem('Probe exceeded its frame observation bound'); return; }
        frames.push({ id: value.id, method: value.method });
        if (value.method === 'sessions.observe') lastObserve = Date.now();
      });
      socket.on('framereceived', ({ payload }) => { const value = JSON.parse(String(payload)); if (value.id && !value.method) { if (replies.size < 25_000) replies.add(value.id); else problem('Probe exceeded its reply observation bound'); } });
    });
    const origin = fixture.info.web;
    const root = mode === 'body' ? fixture.history.root_id : (await fixture.createRoot(client, { title: 'Native settings conversation' })).root.id;
    const rootURL = `${origin}/h/${fixture.info.runtime_id}/s/${root}`;
    const workspace = () => page.evaluate(() => JSON.parse(sessionStorage.getItem('whip.web.workspace.v3')).workspace);
    const panel = id => page.locator(`[data-workspace-view="${id}"]`);
    const chat = id => panel(id).getByRole('region', { name: 'Conversation', exact: true });
    const notebook = id => panel(id).getByRole('region', { name: 'REPL executions', exact: true });
    const tab = id => page.locator(`[id="whip-workspace-tab-${encodeURIComponent(id)}"]`);
    const contentReads = () => httpReads + frames.filter(frame => frame.method === 'content.read').length;
    const action = async (id, label) => {
      await page.locator(`[data-workspace-tab="${id}"]`).getByRole('button', { name: /^Tab actions for / }).click();
      await page.getByRole('menuitem', { name: label, exact: true }).click();
      await page.getByRole('menu').waitFor({ state: 'hidden' });
    };
    let checkedRelease = false;
    const settings = async () => {
      await page.locator('#whip-settings-link').click();
      await page.getByRole('navigation', { name: 'Settings categories', exact: true }).getByRole('button', { name: 'Appearance', exact: true }).click();
      await expect(page.getByRole('heading', { name: 'Appearance', exact: true })).toBeVisible();
      await expect(page.locator('[data-workspace-view]')).toHaveCount(0);
      if (!checkedRelease) {
        await eventually(() => Date.now() - lastObserve > 2500, { timeout: 40_000, description: 'Settings stops native observation after the 30-second idle grace' });
        checkedRelease = true; checks.push('Settings removes transcript consumers and releases idle native observers after the bounded grace');
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
      if (mode === 'repl') {
        const definition = await client.agents.register(defineAgent({ id: 'repl-child', name: 'repl-child' }), deadline());
        const childRequest = crypto.randomUUID();
        await client.session(root).spawn({ definition: definition.ref, overrides: {}, grant_ids: [], budgets: [{ kind: 'model_calls', limit: '8' }], parts: [{ type: 'text', text: 'Child-only settings transcript.' }] }, childRequest, deadline());
        assert.equal((await client.wait(childRequest, deadline())).turn.state, 'succeeded');
        for (let index = 0; index < 20; index++) {
          const historyRequest = crypto.randomUUID();
          await client.session(root).submit([{ type: 'text', text: `Reading message ${index + 1}. **Retained history**\n\n` + 'Bounded native history preserves the reading position. '.repeat(16) }], historyRequest, deadline());
          assert.equal((await client.wait(historyRequest, deadline())).turn.state, 'succeeded');
        }
        const request = crypto.randomUUID();
        await client.session(root).submit([{ type: 'text', text: '```starlark\nprint("Recorded density preview")\n```' }], request, deadline());
        assert.equal((await client.wait(request, deadline())).turn.state, 'succeeded');
      }
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
    }
  } finally {
    try {
      await browser?.close();
      lifecycle('browser-closed', { connected: browser?.isConnected() ?? false });
    } finally {
      const exited = fixture.exited;
      await fixture.close();
      const [exit_code, signal] = await exited;
      lifecycle('runtime-closed', { exit_code, signal });
    }
  }
}

for (const engine of engines) {
  await scenario(engine, 'repl', async ({ fixture, page, client, root, frames, replies, checks, workspace, panel, chat, notebook, tab, action, settings, back, density, select, stableAnchor, exactReturn, contentReads }) => {
    await panel(root).locator('[data-whip-composer]').fill('Root draft survives Settings and split changes.');
    const tools = () => chat(root).locator('[data-activity-group] [data-activity-content]');
    await expect(tools().last()).toHaveAttribute('aria-expanded', 'false');
    const reads = contentReads();
    await settings(); await density(1); await back();
    await expect(chat(root).locator('[data-tool-preview]').last()).toBeVisible();
    const preview = await chat(root).locator('[data-tool-preview]').last().textContent();
    assert.ok(preview.length <= 512 && preview.split('\n').length <= 3, 'Comfortable real-tool preview exceeded bounds');
    await settings(); await density(2); await back();
    await expect(tools().last()).toHaveAttribute('aria-expanded', 'true');
    assert.equal(contentReads(), reads, 'Tool density automatically read a stored body');
    checks.push('canonical recorded tools obey Compact/Comfortable/Detailed; previews are bounded and density performs no content reads');

    const key = `settings-live-${crypto.randomUUID()}`, requestID = crypto.randomUUID();
    const guestCode = `print("live line 1\\nlive line 2\\nlive line 3\\nlive line 4\\nlive line 5\\nlive line 6\\nlive line 7\\nlive line 8")\ntools.fixture_wait(key="${key}-first")\nprint("live line 9")\ntools.fixture_wait(key="${key}-second")\nprint("live line 10")`;
    await client.session(root).submit([{ type: 'text', text: '```starlark\n' + guestCode + '\n```' }], requestID, deadline());
    const live = chat(root).locator('[data-activity-group]').filter({ hasText: /^Called/ }).last();
    await expect(live.locator('[data-activity-content]')).toHaveAttribute('aria-expanded', 'true');
    await live.locator('[data-activity-content]').click();
    await expect(live.locator('[data-activity-content]')).toHaveAttribute('aria-expanded', 'false');
    fixture.release(`${key}-first`);
    const captured = await eventually(async () => {
      const receipt = await client.recover(requestID, deadline());
      if (!receipt.turn) return false;
      const operations = (await client.session(root).turns.operations(receipt.turn.id, { limit: 100 }, deadline())).items;
      return operations.some(operation => operation.arguments.key === `${key}-second` && operation.state === 'dispatched') && { turn: receipt.turn, operations };
    }, { description: 'second real executor hold and stdout update' });
    assert.equal(captured.operations.length, 2, 'The held cell must own exactly the two admitted fixture calls');
    const first = captured.operations.find(operation => operation.arguments.key === `${key}-first`);
    const second = captured.operations.find(operation => operation.arguments.key === `${key}-second`);
    assert(first && second && first.id !== second.id);
    assert.equal(first.state, 'succeeded'); assert.equal(second.state, 'dispatched');
    assert(first.cell_id && first.cell_id === second.cell_id);
    for (const operation of [first, second]) {
      assert.equal(operation.origin, 'cell'); assert.equal(operation.session_id, root);
      assert.equal(operation.turn_id, captured.turn.id);
    }
    const cells = (await client.session(root).turns.cells(captured.turn.id, { limit: 100 }, deadline())).items;
    assert.equal(cells.length, 1); assert.equal(cells[0].id, first.cell_id); assert.equal(cells[0].state, 'running');
    await expect(live.locator('[data-activity-content]')).toHaveAttribute('aria-expanded', 'false');
    await live.locator('[data-activity-content]').click();
    const groupID = await live.getAttribute('data-activity-group'); assert(groupID);
    const detail = chat(root).locator(`[data-activity-detail=${JSON.stringify(second.id)}]`);
    await expect(detail).toHaveAttribute('data-activity-owner', groupID);
    const hint = 'Provisional stdout; the committed result will replace it.';
    await expect(detail.getByText(hint, { exact: true })).toBeVisible();
    await expect(detail).toContainText('live line 9');
    // Each operation detail intentionally includes its shared cell evidence.
    // Check every mounted hint's exact owner, without requiring off-screen rows.
    const ownership = await chat(root).evaluate((region, { ids, hint }) => ({
      details: [...region.querySelectorAll('[data-activity-detail]')].filter(node => ids.includes(node.dataset.activityDetail)).map(node => ({
        id: node.dataset.activityDetail, group: node.dataset.activityOwner,
        hints: [...node.querySelectorAll('p')].filter(item => item.textContent === hint).length,
      })),
      hints: [...region.querySelectorAll('p')].filter(node => node.textContent === hint).map(node => node.closest('[data-activity-detail]')?.getAttribute('data-activity-detail') ?? null),
    }), { ids: [first.id, second.id], hint });
    assert(ownership.details.some(item => item.id === second.id));
    assert(ownership.details.every(item => item.group === groupID && item.hints === 1));
    assert.equal(new Set(ownership.details.map(item => item.id)).size, ownership.details.length);
    assert.deepEqual([...ownership.hints].sort(), ownership.details.map(item => item.id).sort(), 'No duplicate or foreign provisional hint');
    await writeFile(join(directory, `${engine}-repl-live-ownership.json`), JSON.stringify({
      runtime_id: fixture.info.runtime_id, process_epoch: fixture.info.process_epoch,
      session_id: root, turn_id: captured.turn.id, cell_id: first.cell_id,
      operation_ids: [first.id, second.id], group_id: groupID, mounted: ownership,
    }, null, 2));
    await live.locator('[data-activity-content]').click();
    fixture.release(`${key}-second`);
    const completed = await client.wait(requestID, deadline()); assert.equal(completed.turn.state, 'succeeded');
    await expect(live.locator('[data-activity-content]')).toHaveAttribute('aria-expanded', 'false');
    await live.locator('[data-activity-content]').click();
    await expect(live.locator('[data-activity-content]')).toHaveAttribute('aria-expanded', 'true');
    await expect(chat(root).getByText('Provisional stdout; the committed result will replace it.', { exact: true })).toHaveCount(0);
    await action(root, 'Open REPL');
    const liveRepl = leaves((await workspace()).layout)[0].selected;
    const liveCell = notebook(liveRepl).locator('[data-repl-cell]').filter({ hasText: 'live line 10' }).last();
    await expect(liveCell).toBeVisible();
    await liveCell.getByRole('button', { name: /Show 4 more lines/ }).click();
    await expect(liveCell).toContainText('live line 10');
    await tab(root).click();
    assert.equal(contentReads(), reads, 'Explicit bounded disclosure read a body');
    checks.push('manual disclosure overrides Detailed through actual held operation/output/completion; committed cell stdout replaces provisional evidence and is inspectable in REPL');

    await action(root, 'Session details');
    const details = page.getByRole('dialog', { name: 'Session details', exact: true });
    const childLink = details.getByRole('link', { name: 'repl-child', exact: true });
    await childLink.click();
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
    await chat(root).evaluate(element => { element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 })); element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
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
    const followup = crypto.randomUUID();
    await client.session(root).submit([{ type: 'text', text: 'New work keeps following after Appearance changes.' }], followup, deadline());
    await client.wait(followup, deadline());
    await eventually(() => frames.slice(start).some(frame => frame.method === 'sessions.observe' && replies.has(frame.id)), { description: 'new native observation' });
    await eventually(() => chat(root).evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'following new output' });
    checks.push('follow-latest survives Settings and follows subsequent actual daemon output');
  });

  await scenario(engine, 'body', async ({ page, client, root, chat, settings, density, back, stableAnchor, contentReads, checks }) => {
    await settings(); await density(2); await back();
    // The native message names a scoped 1.4MiB text reference. Metadata may load;
    // Detailed presentation must never initiate a content byte read.
    const pageEvidence = await client.session(root).history.page({ direction: 'forward', cursor: '9980', limit: 4 }, deadline());
    assert.ok(pageEvidence.messages.some(message => message.id === 'root-answer-04990' && message.parts.some(part => part.type === 'content' && part.reference_id === 'fixture-large-content')));
    const stored = chat(root).getByRole('button', { name: 'Preview Attachment 1', exact: true });
    await stored.waitFor({ state: 'attached', timeout: 500 }).catch(() => {});
    for (let attempt = 0; attempt < 200 && !await stored.count(); attempt++) {
      await chat(root).evaluate(element => { element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 })); element.scrollTop -= 400; });
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      if (await chat(root).locator('[data-message-id="message:root-answer-04990:part:0"]').count()) {
        // Stop on the canonical owner row so virtualization cannot cancel its
        // metadata read while we continue scrolling through expanded details.
        await stored.waitFor({ state: 'attached', timeout: 15000 });
        break;
      }
      await stored.waitFor({ state: 'attached', timeout: 250 }).catch(() => {});
    }
    await expect(stored).toBeVisible();
    await stored.scrollIntoViewIfNeeded();
    await stableAnchor(chat(root));
    assert.equal(contentReads(), 0, 'Detailed presentation automatically fetched a scoped body');
    await stored.click();
    const dialog = page.getByRole('dialog', { name: 'Attachment 1', exact: true });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('alert')).toContainText('Content exceeds read limit');
    assert.equal(contentReads(), 0, 'Oversized preview bypassed the 1MiB bound');
    const download = page.waitForEvent('download');
    await dialog.getByRole('button', { name: 'Download', exact: true }).click();
    const downloaded = await download;
    assert.equal(await downloaded.failure(), null);
    const stream = await downloaded.createReadStream(); let bytes = 0;
    for await (const chunk of stream) bytes += chunk.length;
    assert.equal(bytes, 1_400_000);
    await eventually(() => contentReads() > 0, { description: 'explicit body download proves scoped content reads' });
    checks.push('native oversized scoped content remains behind explicit disclosure; 1 MiB preview refuses before bytes, while explicit 4 MiB download returns the verified full body');
  });
}
