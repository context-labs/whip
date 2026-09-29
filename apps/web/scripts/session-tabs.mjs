import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

// Real packaged application, native runtime, engine and local fixture provider.
const directory = process.env.WHIP_WEB_TAB_RESULTS ?? '/tmp/whip-session-tabs-results';
await mkdir(directory, { recursive: true });
const summarize = values => { const sorted = values.slice().sort((a, b) => a - b); return { count: sorted.length, median: sorted[Math.floor(sorted.length / 2)], p95: sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * .95) - 1)] }; };
const results = {};
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  console.log(`${name}: starting session-tabs fixture`);
  const fixture = await startFixture();
  const browser = await ({ chromium, firefox }[name]).launch();
  let client = await fixture.connect(`tabs-${randomUUID()}`);
  const context = await browser.newContext({ viewport: { width: 1440, height: 960 } });
  context.setDefaultTimeout(15_000);
  const page = await context.newPage();
  const origin = fixture.info.web;
  const route = id => `/h/${fixture.info.runtime_id}/s/${id}`;
  const frames = [], errors = [];
  // The native SDK owns bounded polled views, not event subscriptions. Measure
  // actual session observation traffic over one second and retain the heap probes.
  const observedRoots = new Map();
  const activeObservedRoots = () => [...observedRoots.values()].filter(at => performance.now() - at < 1000).length;
  let maximumObservedRoots = 0;
  page.on('pageerror', error => errors.push({ message: error.message, stack: error.stack, completedChecks: checks.length }));
  page.on('console', event => { if (event.type() === 'error' && /content.security.policy|violates.*directive|refused to (execute|apply|load)/i.test(event.text())) errors.push(event.text()); });
  page.on('websocket', socket => { const startedAt = performance.now(); socket.on('framesent', ({ payload }) => {
    try {
      const message = JSON.parse(String(payload)); frames.push({ ...message, startedAt, sentAt: performance.now() });
      if (message.method === 'sessions.observe') {
        observedRoots.set(message.params.session_id, performance.now());
        maximumObservedRoots = Math.max(maximumObservedRoots, activeObservedRoots());
      }
    } catch { /* Native SDK owns malformed frame diagnostics. */ }
  }); });
  let holdUpload = false, releaseUpload, uploadReceived;
  await page.routeWebSocket('**/api/v4/ws', socket => {
    const server = socket.connectToServer();
    socket.onMessage(async data => {
      const request = JSON.parse(String(data));
      if (holdUpload && request.method === 'content.put') {
        uploadReceived = request;
        await new Promise(resolve => { releaseUpload = resolve; });
      }
      server.send(data);
    });
  });
  const ready = async () => { await page.getByLabel('Message WHIP', { exact: true }).waitFor(); await eventually(() => page.getByRole('button', { name: 'Add context', exact: true }).isEnabled(), { description: 'selected native session is ready for scoped actions' }); };
  const tab = id => page.locator(`#whip-workspace-tab-${id}`);
  const checks = [];
  const retainedState = [];
  try {
    const roots = [];
    for (let i = 0; i < 32; i++) {
      const result = await fixture.createRoot(client, { title: ['Session recovery', 'Permission flow', 'Theme refinements', 'SDK quickstart'][i] ?? null });
      roots.push(result.root.id);
    }
    const seed = { version: 1, workspaces: [{ runtimeId: fixture.info.runtime_id, tabs: roots.slice(0, 4).map((rootId, index) => ({ rootId, titleHint: ['Session recovery', 'Permission flow', 'Theme refinements', 'SDK quickstart'][index], location: {} })), closed: [], lastActiveRootId: roots[2] }] };
    await context.addInitScript(seed => { if (!sessionStorage.getItem('whip.web.tabs.v1')) sessionStorage.setItem('whip.web.tabs.v1', JSON.stringify(seed)); }, seed);
    await page.goto(origin + route(roots[0])); await ready();
    await eventually(async () => (await page.getByRole('tab').count()) === 4, { description: 'four restored tabs' });
    assert.equal(await tab(roots[0]).getAttribute('aria-selected'), 'true');
    await eventually(() => frames.some(frame => frame.method === 'trees.summaries'), { description: 'background summary query' });
    assert.equal(frames.filter(frame => frame.method === 'sessions.history_page').length, 1, 'Restoring labels opened extra roots');
    await page.getByLabel('Message WHIP', { exact: true }).fill('Keep this draft across session switches.');
    await tab(roots[1]).click(); await ready(); await page.getByLabel('Message WHIP', { exact: true }).fill('Independent draft.');
    await tab(roots[0]).click(); await ready(); assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft across session switches.');
    assert.equal(await page.getByRole('region', { name: 'Conversation', exact: true }).count(), 0, 'Empty sessions should not invent a transcript');
    checks.push('deep-link precedence, four metadata tabs without hydration, independent drafts');

    await page.locator(`[data-workspace-tab="${roots[0]}"]`).getByRole('button', { name: /^Close / }).click();
    await eventually(() => page.url().endsWith(route(roots[1])), { description: 'close selects right neighbor' });
    assert.equal(await tab(roots[0]).count(), 0);
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+k' : 'Control+k');
    await page.getByRole('dialog', { name: 'Commands', exact: true }).getByRole('combobox').fill('Reopen closed tab'); await page.getByRole('option', { name: 'Reopen closed tab', exact: true }).click();
    await ready(); assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft across session switches.');
    const commandCount = frames.filter(frame => ['trees.create', 'sessions.submit', 'sessions.spawn', 'sessions.configure', 'sessions.delete'].includes(frame.method)).length;
    assert.equal(commandCount, 0, 'Tab operations submitted daemon commands');
    await page.reload(); await ready(); assert.equal(await tab(roots[0]).getAttribute('aria-selected'), 'true');
    checks.push('close/reopen and reload preserve draft; navigation sends no durable commands');

    const peer = await context.newPage(); await peer.goto(origin + route(roots[2]));
    await peer.getByLabel('Message WHIP', { exact: true }).waitFor();
    const peerCount = await peer.getByRole('tab').count();
    await page.locator(`[data-workspace-tab="${roots[3]}"]`).getByRole('button', { name: /^Close / }).click({ force: true });
    assert.equal(await peer.getByRole('tab').count(), peerCount, 'Browser windows share tab layout mutations');
    await peer.close(); checks.push('independent browser-window layouts');

    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+k' : 'Control+k'); await page.getByRole('dialog', { name: 'Commands', exact: true }).getByRole('combobox').fill('Reopen closed tab'); await page.getByRole('option', { name: 'Reopen closed tab', exact: true }).click(); await ready();
    const samples = [];
    for (let i = 0; i < 16; i++) {
      const id = roots[i % 4], start = performance.now(); await tab(id).click(); await ready();
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(resolve))); samples.push(performance.now() - start);
    }
    // Hold an actual native content.put before dispatch across unmount and
    // close/reopen. No tab action
    // may cancel the request or attach its data to another recipient.
    await tab(roots[0]).click(); await ready();
    holdUpload = true;
    await page.locator('input[type=file]').setInputFiles({ name: 'retained-tab.txt', mimeType: 'text/plain', buffer: Buffer.from('This attachment stays with its original session.') });
    await eventually(() => uploadReceived, { description: 'native upload held before dispatch' });
    assert.equal(uploadReceived.params.session_id, roots[0]);
    await tab(roots[1]).click(); await ready();
    assert.equal(await page.getByRole('button', { name: 'Remove retained-tab.txt', exact: true }).count(), 0);
    await page.locator(`[data-workspace-tab="${roots[0]}"]`).getByRole('button', { name: /^Close / }).click({ force: true });
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+k' : 'Control+k'); await page.getByRole('dialog', { name: 'Commands', exact: true }).getByRole('combobox').fill('Reopen closed tab'); await page.getByRole('option', { name: 'Reopen closed tab', exact: true }).click(); await ready();
    holdUpload = false; releaseUpload();
    await page.getByRole('button', { name: 'Remove retained-tab.txt', exact: true }).waitFor();
    await page.getByText('retained-tab.txt · Ready', { exact: true }).waitFor();
    await page.getByRole('button', { name: 'Remove retained-tab.txt', exact: true }).click();
    const uploaded = await client.session(roots[0]).content.get(uploadReceived.params.reference_id, deadline());
    assert.equal(new TextDecoder().decode(await client.session(roots[0]).content.readBytes(uploaded, deadline())), 'This attachment stays with its original session.');
    await assert.rejects(client.session(roots[1]).content.get(uploaded.id, deadline()), error => error.kind === 'NOT_FOUND');
    checks.push('in-flight attachment survives tab switch and close/reopen without crossing recipients');

    // A background root still advertises human attention. A second client
    // answers once; the bounded summary converges without selecting that root.
    const permissionSession = client.session(roots[1]), policy = await permissionSession.permissions.policy(deadline());
    await permissionSession.permissions.setMode({ mode: 'prompt', expected_revision: policy.revision }, randomUUID(), deadline());
    const awaitingPermission = permissionSession.submission([{ type: 'text', text: `permission:tabs-${name}` }], randomUUID());
    await awaitingPermission.send(deadline());
    await eventually(async () => (await tab(roots[1]).getAttribute('aria-label')).includes('needs your input'), { description: 'background permission indicator' });
    const permission = (await permissionSession.permissions.list({ pending_only: true }, deadline())).items[0];
    assert.ok(permission);
    await permissionSession.permissions.resolve(permission.operation_id, true, deadline());
    assert.equal((await awaitingPermission.wait(deadline())).turn.state, 'succeeded');
    await eventually(async () => !(await tab(roots[1]).getAttribute('aria-label')).includes('needs your input'), { description: 'other-client decision updates background tab' });
    assert.equal(await tab(roots[0]).getAttribute('aria-selected'), 'true');
    checks.push('background permission and competing-client resolution converge without selecting the root');

    const streaming = client.session(roots[0]).submission([{ type: 'text', text: 'hold:tool-stream' }], randomUUID()); await streaming.send(deadline());
    const executions = page.locator('[data-activity-group]').filter({ hasText: 'Called 1 tool · 1 execution' });
    await expect(executions).toHaveCount(1);
    for (let index = 0; index < 3; index++) { await tab(roots[2]).click(); await ready(); await tab(roots[0]).click(); await ready(); }
    await expect(executions).toHaveCount(1);
    const toggle = executions.locator('[data-activity-content]');
    if (await toggle.getAttribute('aria-expanded') !== 'true') await toggle.click();
    await expect(page.locator('[data-activity-step]')).toHaveCount(2);
    await expect(page.locator('[data-message-role="assistant"]').filter({ hasText: 'hold:tool-stream' })).toHaveCount(1);
    await fixture.release('tool-stream'); assert.equal((await streaming.wait(deadline())).turn.state, 'succeeded');
    checks.push('switching during a live turn retains grouped text/tool streams without duplicates');
    await page.mouse.move(0, 0);
    await page.screenshot({ path: join(directory, `${name}-desktop.png`) });
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await ready();
    await page.screenshot({ path: join(directory, `${name}-desktop-dark.png`) });
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'light' })));
    const storage = { version: 1, workspaces: [{ ...seed.workspaces[0], tabs: roots.map((rootId, index) => ({ rootId, titleHint: `Session ${index + 1}`, location: {} })), lastActiveRootId: roots[0] }] };
    await page.evaluate(value => { sessionStorage.removeItem('whip.web.workspace.v3'); sessionStorage.setItem('whip.web.tabs.v1', JSON.stringify(value)); }, storage);
    await page.goto(origin + route(roots[0])); await ready(); assert.equal(await page.getByRole('tab').count(), 32);
    const cold = [];
    for (let i = 0; i < 8; i++) { const start = performance.now(); await tab(roots[i]).click(); await ready(); cold.push(performance.now() - start); }
    assert.ok(maximumObservedRoots <= 16, `Observed ${maximumObservedRoots} session views; native AppRuntime bounds them at sixteen`);
    assert.ok((await page.getByRole('region', { name: 'Conversation', exact: true }).count()) <= 1);
    // Let the final navigation reveal its sidebar row before measuring idle polls.
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    const sidebarIds = await page.locator('[data-sidebar-session]').evaluateAll(rows => rows.map(row => row.dataset.sidebarSession).sort());
    const expectedPolls = [JSON.stringify([...roots].sort())];
    for (let offset = 0; offset < sidebarIds.length; offset += 32) expectedPolls.push(JSON.stringify(sidebarIds.slice(offset, offset + 32)));
    const from = performance.now(); await page.waitForTimeout(6100);
    const polls = new Map();
    for (const frame of frames.filter(frame => frame.method === 'trees.summaries' && frame.sentAt > from)) {
      assert.ok(frame.params.root_ids.length <= 32, 'Summary request exceeded the root limit');
      const key = JSON.stringify([...frame.params.root_ids].sort());
      polls.set(key, (polls.get(key) ?? 0) + 1);
    }
    // Open tabs have one poll; sidebar rows may need multiple 32-root batches.
    assert.deepEqual([...polls.keys()].sort(), [...new Set(expectedPolls)].sort(), 'Expected only open-tab and rendered-sidebar summary batches');
    for (const [key, count] of polls) {
      // The tab and sidebar queries are separate owners even when their root sets match.
      const scopes = expectedPolls.filter(value => value === key).length;
      assert.ok(count >= 2 * scopes && count <= 4 * scopes, `Expected a 2s poll for ${scopes} scopes, observed ${count}`);
    }
    const pollCount = [...polls.values()].reduce((total, count) => total + count, 0);
    checks.push('32 metadata tabs retain <=16 native session views and bounded tab/sidebar summary polls');

    await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); document.dispatchEvent(new Event('visibilitychange')); });
    // Allow the visibility effect to retire polling. Reads whose socket opened
    // earlier may still finish their required initialize handshake.
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    const hiddenFrom = performance.now(); await page.waitForTimeout(4200);
    assert.equal(frames.filter(frame => frame.method === 'trees.summaries' && frame.startedAt > hiddenFrom).length, 0);
    await page.evaluate(() => { delete document.visibilityState; document.dispatchEvent(new Event('visibilitychange')); });
    checks.push('summary polling stops when document visibility reports hidden');
    await page.getByLabel('Message WHIP', { exact: true }).fill('Restart keeps this draft and the open layout.');
    await fixture.crashAndRestart(); client = await fixture.connect(client.clientID);
    await eventually(async () => await page.getByLabel('Message WHIP', { exact: true }).isEnabled() && (await tab(roots[7]).getAttribute('aria-label')) !== null && !(await tab(roots[7]).getAttribute('aria-label')).includes('Activity unavailable'), { description: 'same-runtime restart recovers session and summaries' });
    assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Restart keeps this draft and the open layout.');
    assert.equal(await page.getByRole('tab').count(), 32);
    checks.push('same-runtime daemon crash/restart retains all tabs and unsent draft');
    const extra = await fixture.createRoot(client);
    const previousSnapshots = frames.filter(frame => frame.method === 'sessions.history_page').length;
    await page.goto(origin + route(extra.root.id));
    await page.getByRole('heading', { name: 'Your session tabs are full', exact: true }).waitFor();
    assert.equal(frames.filter(frame => frame.method === 'sessions.history_page').length, previousSnapshots, 'Overflow route hydrated a 33rd root');
    await tab(roots[0]).click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Close tab', exact: true }).click(); await ready();
    assert.equal(await page.getByRole('tab').count(), 32);
    assert.equal(await tab(extra.root.id).getAttribute('aria-selected'), 'true');
    checks.push('a 33rd deep link does not hydrate until a tab is explicitly closed');

    await client.session(roots[5]).delete(deadline());
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
        await page.evaluate(value => { sessionStorage.removeItem('whip.web.workspace.v3'); sessionStorage.setItem('whip.web.tabs.v1', JSON.stringify(value)); }, { version: 1, workspaces: [{ runtimeId: fixture.info.runtime_id, tabs: ids.map(rootId => ({ rootId, titleHint: '', location: {} })), closed: [] }] });
        await page.goto(origin + route(roots[2])); await ready();
        for (const id of ids.slice(1, 4)) { await tab(id).click(); await ready(); }
        await tab(roots[2]).click(); await ready();
        await inspector.send('HeapProfiler.collectGarbage');
        const heap = await inspector.send('Runtime.getHeapUsage');
        retainedState.push({ openTabs: count, usedHeapBytesAfterGC: heap.usedSize, observedSessionViews: activeObservedRoots(), metadataBytes: await page.evaluate(() => new TextEncoder().encode(sessionStorage.getItem('whip.web.workspace.v3')).length), mountedConversations: await page.getByRole('region', { name: 'Conversation', exact: true }).count() });
      }
      await inspector.detach();
      checks.push('whole-app retained heap, metadata bytes and native view reads measured at 1/8/32 tabs');
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
    results[name] = { browser: await browser.version(), checks, retainedState, switchInteractionMs: summarize(samples), coldSwitchInteractionMs: summarize(cold), measurement: 'Playwright click-to-ready-plus-rAF wall time (includes automation overhead; not physical paint)', maximumObservedRoots, steadySummaryPollsIn6100ms: pollCount };
    console.log(`${name}: ${checks.length} session-tab workflows passed`);
  } catch (error) {
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify(frames.filter(frame => /sessions\.|trees.summaries/.test(frame.method)), null, 2));
    await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`);
    throw error;
  } finally { releaseUpload?.(); await browser.close(); await fixture.close(); }
}
await writeFile(join(directory, 'session-tabs.json'), JSON.stringify(results, null, 2));
console.log(JSON.stringify(results, null, 2));
