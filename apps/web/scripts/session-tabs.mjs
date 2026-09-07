import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Real packaged application and isolated daemon, with synthetic sessions only.
const directory = process.env.WHIP_WEB_TAB_RESULTS ?? '/tmp/whip-session-tabs-results';
await mkdir(directory, { recursive: true });
const summarize = values => { const sorted = values.slice().sort((a, b) => a - b); return { count: sorted.length, median: sorted[Math.floor(sorted.length / 2)], p95: sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * .95) - 1)] }; };
const results = {};
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  console.log(`${name}: starting session-tabs fixture`);
  const fixture = await startFixture();
  const browser = await ({ chromium, firefox }[name]).launch();
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `tabs-${crypto.randomUUID()}`, clientKind: 'human' });
  const context = await browser.newContext({ viewport: { width: 1440, height: 960 } });
  context.setDefaultTimeout(15_000);
  const page = await context.newPage();
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const route = id => `/h/${fixture.info.runtime_id}/s/${id}`;
  const frames = [], errors = [];
  const subscriptionSets = new Map();
  let currentSocket;
  let maximumSubscriptions = 0;
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', event => { if (event.type() === 'error' && /content.security.policy|violates.*directive|refused to (execute|apply|load)/i.test(event.text())) errors.push(event.text()); });
  page.on('websocket', socket => {
    currentSocket = socket;
    const activeSubscriptions = new Set();
    subscriptionSets.set(socket, activeSubscriptions);
    socket.on('framesent', ({ payload }) => {
      try {
        const message = JSON.parse(String(payload)); frames.push({ ...message, sentAt: performance.now() });
        if (message.method === 'events.subscribe') activeSubscriptions.add(message.params.subscription_id);
        if (message.method === 'events.unsubscribe') activeSubscriptions.delete(message.params.subscription_id);
        maximumSubscriptions = Math.max(maximumSubscriptions, ...[...subscriptionSets.values()].map(set => set.size));
      } catch {}
    });
    socket.on('close', () => subscriptionSets.delete(socket));
  });
  const ready = async () => { await page.getByLabel('Message WHIP', { exact: true }).waitFor(); await page.getByText('live', { exact: true }).waitFor(); };
  const tab = id => page.locator(`#whip-workspace-tab-${id}`);
  const checks = [];
  const retainedState = [];
  try {
    await client.connect();
    const roots = [];
    for (let i = 0; i < 32; i++) {
      const result = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
      assert.equal(result.status, 'succeeded'); roots.push(result.result.root_id);
      if (i < 4) await client.session(roots[i]).rename(['Session recovery', 'Permission flow', 'Theme refinements', 'SDK quickstart'][i]).result();
    }
    const seed = { version: 1, workspaces: [{ runtimeId: fixture.info.runtime_id, tabs: roots.slice(0, 4).map((rootId, index) => ({ rootId, titleHint: ['Session recovery', 'Permission flow', 'Theme refinements', 'SDK quickstart'][index], location: {} })), closed: [], lastActiveRootId: roots[2] }] };
    await context.addInitScript(seed => { if (!sessionStorage.getItem('whip.web.tabs.v1')) sessionStorage.setItem('whip.web.tabs.v1', JSON.stringify(seed)); }, seed);
    await page.goto(origin + route(roots[0])); await ready();
    await eventually(async () => (await page.getByRole('tab').count()) === 4, { description: 'four restored tabs' });
    assert.equal(await tab(roots[0]).getAttribute('aria-selected'), 'true');
    await eventually(() => frames.some(frame => frame.method === 'sessions.summaries'), { description: 'background summary query' });
    assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, 1, 'Restoring labels opened extra roots');
    await page.getByLabel('Message WHIP', { exact: true }).fill('Keep this draft across session switches.');
    await tab(roots[1]).click(); await ready(); await page.getByLabel('Message WHIP', { exact: true }).fill('Independent draft.');
    await tab(roots[0]).click(); await ready(); assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft across session switches.');
    assert.equal(await page.getByRole('region', { name: 'Conversation', exact: true }).count(), 0, 'Empty sessions should not invent a transcript');
    checks.push('deep-link precedence, four metadata tabs without hydration, independent drafts');

    await page.locator(`[data-workspace-tab="${roots[0]}"]`).getByRole('button', { name: /^Close / }).click();
    await eventually(() => page.url().endsWith(route(roots[1])), { description: 'close selects right neighbor' });
    assert.equal(await tab(roots[0]).count(), 0);
    await page.getByRole('button', { name: 'Application menu', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Reopen closed tab', exact: true }).click();
    await ready(); assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft across session switches.');
    const commandCount = frames.filter(frame => frame.method === 'command.submit').length;
    assert.equal(commandCount, 0, 'Tab operations submitted daemon commands');
    await page.reload(); await ready(); assert.equal(await tab(roots[0]).getAttribute('aria-selected'), 'true');
    checks.push('close/reopen and reload preserve draft; navigation sends no durable commands');

    const peer = await context.newPage(); await peer.goto(origin + route(roots[2]));
    await peer.getByLabel('Message WHIP', { exact: true }).waitFor();
    const peerCount = await peer.getByRole('tab').count();
    await page.locator(`[data-workspace-tab="${roots[3]}"]`).getByRole('button', { name: /^Close / }).click({ force: true });
    assert.equal(await peer.getByRole('tab').count(), peerCount, 'Browser windows share tab layout mutations');
    await peer.close(); checks.push('independent browser-window layouts');

    await page.getByRole('button', { name: 'Application menu', exact: true }).click(); await page.getByRole('menuitem', { name: 'Reopen closed tab', exact: true }).click(); await ready();
    const samples = [];
    for (let i = 0; i < 16; i++) {
      const id = roots[i % 4], start = performance.now(); await tab(id).click(); await ready();
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(resolve))); samples.push(performance.now() - start);
    }
    // Hold a real HTTP upload across unmount and close/reopen. No tab action
    // may cancel the request or attach its data to another recipient.
    await tab(roots[0]).click(); await ready();
    await page.route('**/api/v3/content*', async route => { await new Promise(resolve => setTimeout(resolve, 300)); await route.continue(); });
    await page.locator('input[type=file]').setInputFiles({ name: 'retained-tab.txt', mimeType: 'text/plain', buffer: Buffer.from('This attachment stays with its original session.') });
    await tab(roots[1]).click(); await ready();
    assert.equal(await page.getByRole('button', { name: 'Remove retained-tab.txt', exact: true }).count(), 0);
    await page.locator(`[data-workspace-tab="${roots[0]}"]`).getByRole('button', { name: /^Close / }).click({ force: true });
    await page.getByRole('button', { name: 'Application menu', exact: true }).click(); await page.getByRole('menuitem', { name: 'Reopen closed tab', exact: true }).click(); await ready();
    await page.getByRole('button', { name: 'Remove retained-tab.txt', exact: true }).waitFor();
    await page.getByText('retained-tab.txt · Ready', { exact: true }).waitFor();
    await page.getByRole('button', { name: 'Remove retained-tab.txt', exact: true }).click();
    await page.unroute('**/api/v3/content*');
    checks.push('in-flight attachment survives tab switch and close/reopen without crossing recipients');

    // A background root still advertises human attention. A second client
    // answers once; the bounded summary converges without selecting that root.
    await client.permissions.setMode(roots[1], true).result();
    const awaitingPermission = client.session(roots[1]).submit({ text: `permission:tabs-${name}` });
    await awaitingPermission.accepted();
    await eventually(async () => (await tab(roots[1]).getAttribute('aria-label')).includes('needs your input'), { description: 'background permission indicator' });
    const permission = (await client.session(roots[1]).snapshot()).permissions.find(item => item.status === 'pending');
    assert.ok(permission);
    await client.permissions.decide({ root_id: roots[1], permission_id: permission.id, allow: true });
    assert.equal((await awaitingPermission.result()).status, 'succeeded');
    await eventually(async () => !(await tab(roots[1]).getAttribute('aria-label')).includes('needs your input'), { description: 'other-client decision updates background tab' });
    assert.equal(await tab(roots[0]).getAttribute('aria-selected'), 'true');
    checks.push('background permission and competing-client resolution converge without selecting the root');

    const streaming = client.session(roots[0]).submit({ text: 'hold:tool-stream' }); await streaming.accepted();
    await eventually(async () => await page.locator('[data-message-id^="live-tool:"]').count() === 2, { description: 'streamed tools before switching' });
    for (let index = 0; index < 3; index++) { await tab(roots[2]).click(); await ready(); await tab(roots[0]).click(); await ready(); }
    assert.equal(await page.locator('[data-message-id^="live-tool:"]').count(), 2);
    assert.equal(await page.locator('[data-message-id^="live:"]').count(), 1);
    await fixture.release('tool-stream'); assert.equal((await streaming.result()).status, 'succeeded');
    checks.push('switching during a live turn retains grouped text/tool streams without duplicates');
    await page.mouse.move(0, 0);
    await page.screenshot({ path: join(directory, `${name}-desktop.png`) });
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await ready();
    await page.screenshot({ path: join(directory, `${name}-desktop-dark.png`) });
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'light' })));
    const storage = { version: 1, workspaces: [{ ...seed.workspaces[0], tabs: roots.map((rootId, index) => ({ rootId, titleHint: `Session ${index + 1}`, location: {} })), lastActiveRootId: roots[0] }] };
    await page.evaluate(value => sessionStorage.setItem('whip.web.tabs.v1', JSON.stringify(value)), storage);
    await page.goto(origin + route(roots[0])); await ready(); assert.equal(await page.getByRole('tab').count(), 32);
    const cold = [];
    for (let i = 0; i < 8; i++) { const start = performance.now(); await tab(roots[i]).click(); await ready(); cold.push(performance.now() - start); }
    assert.ok(maximumSubscriptions <= 4, `Retained ${maximumSubscriptions} root subscriptions`);
    assert.ok((await page.getByRole('region', { name: 'Conversation', exact: true }).count()) <= 1);
    const from = performance.now(); await page.waitForTimeout(6100);
    const pollCount = frames.filter(frame => frame.method === 'sessions.summaries' && frame.sentAt > from).length;
    assert.ok(pollCount >= 2 && pollCount <= 4, `Expected one shared 2s poll, observed ${pollCount}`);
    checks.push('32 tabs retain <=4 root subscriptions and one shared background poll');

    await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange')); });
    const hiddenFrom = performance.now(); await page.waitForTimeout(4200);
    assert.equal(frames.filter(frame => frame.method === 'sessions.summaries' && frame.sentAt > hiddenFrom).length, 0);
    await page.evaluate(() => { delete document.visibilityState; document.dispatchEvent(new Event('visibilitychange')); });
    checks.push('summary polling stops when document visibility reports hidden');
    await page.getByLabel('Message WHIP', { exact: true }).fill('Restart keeps this draft and the open layout.');
    await fixture.crashAndRestart(); await client.whenConnected();
    await eventually(async () => (await page.getByText('live', { exact: true }).count()) === 1 && (await tab(roots[7]).getAttribute('aria-label')) !== null && !(await tab(roots[7]).getAttribute('aria-label')).includes('Activity unavailable'), { description: 'same-runtime restart recovers session and summaries' });
    assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Restart keeps this draft and the open layout.');
    assert.equal(await page.getByRole('tab').count(), 32);
    checks.push('same-runtime daemon crash/restart retains all tabs and unsent draft');
    const extra = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
    assert.equal(extra.status, 'succeeded');
    const previousSnapshots = frames.filter(frame => frame.method === 'root.snapshot').length;
    await page.goto(origin + route(extra.result.root_id));
    await page.getByRole('heading', { name: 'Your session tabs are full', exact: true }).waitFor();
    assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, previousSnapshots, 'Overflow route hydrated a 33rd root');
    await tab(roots[0]).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Close tab', exact: true }).click(); await ready();
    assert.equal(await page.getByRole('tab').count(), 32);
    assert.equal(await tab(extra.result.root_id).getAttribute('aria-selected'), 'true');
    checks.push('a 33rd deep link does not hydrate until a tab is explicitly closed');

    await client.session(roots[5]).delete().result();
    await eventually(async () => (await tab(roots[5]).getAttribute('aria-label')).includes('Session unavailable'), { description: 'authoritative missing-root indicator' });
    await tab(roots[5]).click();
    await page.getByText('Refresh', { exact: true }).waitFor();
    await tab(roots[6]).click(); await ready();
    assert.equal(await page.getByRole('alert').filter({ hasText: /not found|no rows/i }).count(), 0, 'Missing-root error leaked into another session');
    checks.push('deleted root remains explicitly unavailable and its error does not leak to another tab');
    // Observe full app heaps after GC with 1/8/32 metadata tabs. This is a
    // whole-bundle heap measurement, not an attribution of bytes to tabs alone.
    if (name === 'chromium') {
      const inspector = await context.newCDPSession(page);
      for (const count of [1, 8, 32]) {
        const ids = [roots[2], ...roots.filter(id => id !== roots[2])].slice(0, count);
        await page.evaluate(value => sessionStorage.setItem('whip.web.tabs.v1', JSON.stringify(value)), { version: 1, workspaces: [{ runtimeId: fixture.info.runtime_id, tabs: ids.map(rootId => ({ rootId, titleHint: '', location: {} })), closed: [] }] });
        await page.goto(origin + route(roots[2])); await ready();
        for (const id of ids.slice(1, 4)) { await tab(id).click(); await ready(); }
        await tab(roots[2]).click(); await ready();
        await inspector.send('HeapProfiler.collectGarbage');
        const heap = await inspector.send('Runtime.getHeapUsage');
        retainedState.push({ openTabs: count, usedHeapBytesAfterGC: heap.usedSize, activeSubscriptions: subscriptionSets.get(currentSocket)?.size ?? 0, metadataBytes: await page.evaluate(() => new TextEncoder().encode(sessionStorage.getItem('whip.web.tabs.v1')).length), mountedConversations: await page.getByRole('region', { name: 'Conversation', exact: true }).count() });
      }
      await inspector.detach();
      checks.push('whole-app retained heap, metadata bytes and subscriptions measured at 1/8/32 tabs');
    }
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: /^Open sessions:/ }).click();
    const sheet = page.getByRole('dialog', { name: 'Open sessions', exact: true });
    await sheet.getByLabel('Find an open session', { exact: true }).fill('Theme refinements');
    await sheet.getByRole('button', { name: /Theme refinements.*observed activity/ }).click(); await ready();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.screenshot({ path: join(directory, `${name}-mobile.png`) });
    checks.push('mobile session picker, search, selection and no horizontal document overflow');
    assert.deepEqual(errors, []);
    results[name] = { browser: await browser.version(), checks, retainedState, switchInteractionMs: summarize(samples), coldSwitchInteractionMs: summarize(cold), measurement: 'Playwright click-to-ready-plus-rAF wall time (includes automation overhead; not physical paint)', maximumSubscriptions, steadySummaryPollsIn6100ms: pollCount };
    console.log(`${name}: ${checks.length} session-tab workflows passed`);
  } catch (error) {
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify(frames.filter(frame => /events\.|root.snapshot/.test(frame.method)), null, 2));
    await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`);
    throw error;
  } finally { client.close(); await browser.close(); await fixture.close(); }
}
await writeFile(join(directory, 'session-tabs.json'), JSON.stringify(results, null, 2));
console.log(JSON.stringify(results, null, 2));
