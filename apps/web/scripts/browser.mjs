import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { deadline as callDeadline, eventually, startFixture } from './native-fixture.mjs';

// The fixture embeds the packaged production app. Build and pack before running;
// no user daemon, home, credentials, or provider account is used by these tests.
const requested = (process.env.WHIP_WEB_BROWSERS ?? process.env.WHIP_SDK_BROWSERS ?? 'chromium,firefox').split(',');
const resultsDirectory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-web-browser-results';
await mkdir(resultsDirectory, { recursive: true });
const log = (scope, phase) => console.log(`[${new Date().toISOString()}] ${scope}: ${phase}`);
log('fixture', 'compiling and starting isolated native runtime');
const fixture = await startFixture();
log('fixture', `ready at ${fixture.directory}`);
const results = {};
const axeSource = await readFile(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
const milliseconds = samples => {
  const sorted = samples.slice().sort((a, b) => a - b);
  return { count: sorted.length, median: sorted[Math.floor(sorted.length / 2)] ?? 0, p95: sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * .95) - 1)] ?? 0 };
};
const origin = () => fixture.info.web;
const route = rootId => `/h/${fixture.info.runtime_id}/s/${rootId}`;
const textOf = message => message.parts.filter(part => part.type === 'text').map(part => part.text).join('\n');
const messagesOf = async session => (await session.history.page({ direction: 'forward', limit: 100 }, callDeadline())).messages;
async function complete(session, text) { const requestID = randomUUID(); await session.submit([{ type: 'text', text }], requestID, callDeadline()); return session.client.wait(requestID, callDeadline()); }

function observe(page) {
  const requests = [];
  const replies = new Set();
  const errors = [];
  const inflight = new Map();
  const acceptance = [];
  const outcomes = [];
  const rpcErrors = [];
  page.on('pageerror', error => errors.push({ message: error.message, stack: error.stack }));
  page.on('console', event => {
    if (event.type() === 'error' && /content.security.policy|violates.*directive|refused to (execute|apply|load)|unsafe-eval/i.test(event.text())) errors.push(event.text());
  });
  page.on('websocket', socket => {
    socket.on('framesent', ({ payload }) => {
      try {
        const message = JSON.parse(String(payload));
        requests.push(message);
        if (message.method === 'sessions.submit') inflight.set(message.id, performance.now());
      } catch { /* Non-JSON frames are diagnosed by the SDK. */ }
    });
    socket.on('framereceived', ({ payload }) => {
      try {
        const message = JSON.parse(String(payload));
        if (message.id && !message.method) replies.add(message.id);
        if (message.error) rpcErrors.push({ id: message.id, error: message.error });
        if (message.result?.receipt) outcomes.push(message.result);
        if (message.result?.items?.some(item => item.finished_at)) outcomes.push(...message.result.items);
        if (inflight.has(message.id)) { acceptance.push(performance.now() - inflight.get(message.id)); inflight.delete(message.id); }
      } catch { /* See SDK protocol validation. */ }
    });
  });
  return { requests, replies, errors, acceptance, outcomes, rpcErrors };
}
async function measurePresentation(page) {
  await page.addInitScript(() => {
    const samples = []; const pending = []; const seen = new Set();
    window.__whipEventToView = samples;
    const check = () => {
      for (let index = pending.length - 1; index >= 0; index--) {
        const item = pending[index];
        const rows = document.querySelectorAll('[data-activity-detail]');
        if ([...rows].some(row => row.textContent?.includes(item.text))) { samples.push(performance.now() - item.start); pending.splice(index, 1); }
      }
    };
    new MutationObserver(check).observe(document, { subtree: true, childList: true, characterData: true });
    const NativeWebSocket = window.WebSocket;
    window.WebSocket = class extends NativeWebSocket {
      constructor(...args) {
        super(...args);
        this.addEventListener('message', message => {
          try {
            const preview = JSON.parse(message.data).result?.preview;
            if (!preview?.cell_id) return;
            const key = preview.cell_id + ':' + preview.revision;
            if (seen.has(key)) return;
            if (seen.size >= 256) throw new Error('Paint probe exceeded its retained sample bound');
            seen.add(key);
            const text = preview.text?.trimEnd();
            if (typeof text !== 'string' || !text) return;
            pending.push({ text, start: performance.now() });
            if (pending.length > 128) pending.shift();
          } catch { /* Protocol errors are owned by the real SDK. */ }
        });
      }
    };
  });
}
async function ready(page) {
  await page.getByLabel('Message WHIP', { exact: true }).waitFor();
  await eventually(() => page.getByLabel('Message WHIP', { exact: true }).isEnabled());
  assert.ok((await page.getByRole('region', { name: 'Conversation', exact: true }).count()) <= 1, 'Navigation retained multiple conversation trees');
}
async function send(page, text) {
  await page.getByLabel('Message WHIP', { exact: true }).fill(text);
  await page.getByRole('button', { name: /^(Send|Queue) message$/ }).click();
  await eventually(async () => (await page.getByLabel('Message WHIP', { exact: true }).inputValue()) === '', { description: 'authoritative acceptance clears draft' });
}
async function selectTheme(page, id) {
  await page.getByRole('link', { name: 'Settings', exact: true }).first().click();
  await page.getByRole('navigation', { name: 'Settings categories', exact: true }).getByRole('button', { name: 'Appearance', exact: true }).click();
  await page.getByRole('combobox', { name: /^Color theme:/ }).click();
  await page.getByRole('combobox', { name: 'Search themes', exact: true }).fill(id);
  await page.getByRole('option', { name: new RegExp(`^${id}\\s*Dark$`, 'i') }).click();
  await page.waitForFunction(value => document.documentElement.dataset.theme === value, id);
}
async function accessible(page, state) {
  // Inspect settled presentation, not a random frame of a finite text fade.
  // Infinite activity indicators remain running and are still checked by Axe.
  await page.waitForFunction(() => !document.getAnimations().some(animation =>
    animation.playState === 'running' && Number.isFinite(animation.effect?.getComputedTiming().endTime)));
  // Serve the test probe as an external same-origin script: the production CSP
  // stays intact, and the application bundle gains no test-only dependency.
  if (!(await page.evaluate(() => !!window.axe))) {
    const path = origin() + '/__whip-test-a11y.js';
    await page.route(path, route => route.fulfill({ contentType: 'text/javascript', body: axeSource }));
    await page.addScriptTag({ url: path });
    await page.unroute(path);
  }
  const violations = await page.evaluate(async () => (await axe.run(document)).violations
    .filter(item => item.impact === 'critical' || item.impact === 'serious')
    .map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ target: node.target, summary: node.failureSummary, rendered: (() => { const element = document.querySelector(node.target[0]); if (!element) return undefined; const style = getComputedStyle(element); return { text: element.textContent, opacity: style.opacity, color: style.color, disabled: element.matches(':disabled'), animations: element.getAnimations().map(animation => ({ playState: animation.playState, currentTime: animation.currentTime })) }; })() })) })));
  assert.deepEqual(violations, [], `Accessibility violations in ${state}`);
}

try {
  const discovery = await (await fetch(origin() + '/api/v4/web', { signal: AbortSignal.timeout(10_000) })).json();
  assert.equal(discovery.available, true, 'Build and pack the application before compiling the fixture');
  for (const name of requested) {
    const launcher = { chromium, firefox }[name];
    if (!launcher) throw new Error(`Unsupported automated browser ${name}; actual Safari has a separate release smoke.`);
    let phase = 'launching browser';
    const progress = next => { phase = next; log(name, next); };
    progress(phase);
    const browser = await launcher.launch({ headless: true, timeout: 30_000 });
    const context = await browser.newContext({ viewport: { width: 1360, height: 960 }, colorScheme: 'light' });
    context.setDefaultTimeout(15_000);
    context.setDefaultNavigationTimeout(30_000);
    const page = await context.newPage();
    let activePage = page;
    const observed = observe(page);
    const observations = [observed];
    await measurePresentation(page);
    let client = await fixture.connect(`web-test-${name}-${randomUUID()}`);
    const checks = [];
    const recovery = [];
    let rootId;
    let stoppingCommand;
    const deadline = setTimeout(() => {
      log(name, `120s browser deadline exceeded during ${phase}`);
      void browser.close();
    }, 120_000);
    deadline.unref();
    try {
      progress('attach/deep link and empty-state accessibility');
      const created = await fixture.createRoot(client, { title: `Browser ${name}` });
      rootId = created.root.id;
      let session = client.session(rootId);
      const response = await page.goto(origin() + route(rootId));
      const csp = response.headers()['content-security-policy'];
      assert.ok(csp && !csp.includes("'unsafe-eval'") && !csp.includes("'unsafe-inline'"));
      await ready(page);
      await accessible(page, 'empty conversation');
      checks.push('production same-origin deep link and strict CSP');

      progress('keyboard command search and composer focus');
      await page.keyboard.press(process.platform === 'darwin' ? 'Meta+k' : 'Control+k');
      const commandSearch = page.getByRole('combobox', { name: 'Search commands', exact: true });
      await eventually(() => commandSearch.evaluate(element => document.activeElement === element), { description: 'command search receives keyboard focus' });
      await page.keyboard.type('Focus message composer');
      await page.keyboard.press('Enter');
      await commandSearch.waitFor({ state: 'hidden' });
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      assert.equal(await page.getByLabel('Message WHIP', { exact: true }).evaluate(element => document.activeElement === element), true, 'Command selection must focus the requested composer');
      checks.push('command palette accepts immediate keyboard search and focuses the requested composer');

      progress('compact composer and session model picker');
      const composer = page.getByLabel('Message WHIP', { exact: true });
      const emptyHeight = (await composer.boundingBox()).height;
      await composer.fill(Array.from({ length: 12 }, (_, index) => `Draft line ${index + 1}`).join('\n'));
      await eventually(async () => (await composer.boundingBox()).height > emptyHeight + 100);
      await composer.fill('Keep this draft while changing the model');
      await eventually(async () => (await composer.boundingBox()).height === emptyHeight);
      const modelTrigger = page.getByRole('button', { name: 'Model', exact: true });
      await modelTrigger.click();
      const modelSearch = page.getByRole('textbox', { name: 'Search models', exact: true });
      await modelSearch.fill('replacement');
      await page.getByRole('option', { name: 'replacement · provider', exact: true }).click();
      await eventually(async () => (await session.get(callDeadline())).configuration.model.name === 'replacement', { description: 'composer model selection reaches the host' });
      await modelSearch.waitFor({ state: 'hidden' });
      // Host acceptance precedes browser replay, which changes toolbar geometry.
      // Wait for the displayed model and popup focus return before another click.
      await eventually(async () => (await modelTrigger.innerText()).includes('replacement'), { description: 'browser renders the selected model' });
      await eventually(() => modelTrigger.evaluate(element => document.activeElement === element), { description: 'model picker returns focus to its trigger' });
      const effortTrigger = page.getByRole('button', { name: 'Reasoning effort', exact: true });
      await effortTrigger.click();
      await page.getByRole('option', { name: 'Medium', exact: true }).click();
      await eventually(async () => (await session.get(callDeadline())).configuration.model.effort === 'medium');
      await page.getByRole('listbox', { name: 'Reasoning effort', exact: true }).waitFor({ state: 'hidden' });
      await page.screenshot({ path: join(resultsDirectory, `${name}-model-picker.png`) });
      assert.equal(await composer.inputValue(), 'Keep this draft while changing the model');
      assert.equal((await messagesOf(session)).length, 0, 'Changing models submitted the message draft');
      await page.reload(); await ready(page);
      assert.ok((await modelTrigger.innerText()).includes('replacement'));
      assert.equal(await effortTrigger.innerText(), 'Medium');
      assert.equal(await composer.inputValue(), 'Keep this draft while changing the model');
      checks.push('composer grows and shrinks, model/effort selections apply, and draft/model survive reload');

      progress('stream grouping, theme switch and reload');
      // Compact activity intentionally omits output. Measure painted updates in
      // the detailed presentation, selected through the same control users use.
      await page.getByRole('link', { name: 'Settings', exact: true }).first().click();
      await page.getByRole('navigation', { name: 'Settings categories', exact: true }).getByRole('button', { name: 'Appearance', exact: true }).click();
      const density = page.getByRole('slider', { name: 'Tool call density', exact: true });
      await density.focus(); await density.press('End');
      await eventually(async () => (await density.getAttribute('aria-valuetext')) === 'Detailed');
      await page.getByRole('button', { name: 'Back to workspace', exact: true }).click();
      await ready(page);
      const toolStreamKey = `tool-stream-browser-${name}`;
      await send(page, `hold:${toolStreamKey}`);
      const activity = page.locator('[data-activity-group]');
      await eventually(async () => (await activity.count()) === 1 && (await activity.innerText()).includes('Called 1 tool · 1 execution'), { description: 'two cumulative executions in one activity group' });
      assert.equal(await page.locator('[data-message-role="assistant"][data-message-id]').filter({ hasText: `hold:${toolStreamKey}` }).count(), 1, 'Streamed prose must hand over to one canonical model-call row');
      const presentationLatency = await eventually(async () => {
        const samples = await page.evaluate(() => window.__whipEventToView);
        return samples.length >= 1 && samples;
      }, { description: 'grouped output updates are painted' });
      const tools = page.locator('[data-activity-detail]');
      await eventually(async () => (await tools.count()) === 2);
      const cellIds = await tools.evaluateAll(cells => cells.map(cell => cell.getAttribute('data-activity-detail')));
      assert.equal(await modelTrigger.isEnabled(), false);
      assert.equal(await effortTrigger.isEnabled(), false);
      for (let index = 0; index < 2; index++) {
        assert.match(await tools.nth(index).getByRole('region', { name: 'Executed code', exact: true }).innerText(), /print\("first"\)/);
        const text = await tools.nth(index).getByRole('region', { name: /^(Output|Live output · provisional)$/ }).innerText();
        assert.equal((text.match(/first/g) ?? []).length, 1);
        assert.equal((text.match(/second/g) ?? []).length, 1);
      }
      const preservedExecutions = async () => {
        await eventually(async () => (await activity.count()) === 1 && (await activity.innerText()).includes('Called 1 tool · 1 execution'));
        const toggle = activity.locator('[data-activity-content]');
        if (await toggle.getAttribute('aria-expanded') !== 'true') await toggle.click();
        assert.deepEqual(await tools.evaluateAll(cells => cells.map(cell => cell.getAttribute('data-activity-detail'))), cellIds);
      };
      await page.getByLabel('Message WHIP', { exact: true }).fill(`Unsent ${name} draft`);
      await selectTheme(page, 'nord');
      await page.getByRole('button', { name: 'Back to workspace', exact: true }).click();
      await ready(page);
      assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), `Unsent ${name} draft`);
      assert.equal(await page.getByRole('region', { name: 'Conversation', exact: true }).count(), 1);
      await preservedExecutions();
      await page.reload(); await ready(page);
      assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), `Unsent ${name} draft`);
      assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'nord');
      assert.equal(await page.getByRole('region', { name: 'Conversation', exact: true }).count(), 1);
      await preservedExecutions();
      await fixture.release(toolStreamKey);
      await eventually(async () => !(await session.activity(callDeadline())).active_turn, { description: 'held tool turn completes' });
      checks.push('delta/cumulative streams stay grouped across theme changes and reload; draft/theme persist');

      progress('host context completion');
      await writeFile(join(fixture.directory, 'web-browser-context.txt'), 'Host-side completion fixture.');
      await page.getByRole('button', { name: 'Add context', exact: true }).click();
      await page.getByRole('combobox', { name: 'Search on the host', exact: true }).fill('web-browser-context');
      await page.getByRole('option', { name: /web-browser-context\.txt/ }).click();
      assert.match(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), /@web-browser-context\.txt/);
      assert.ok(observed.requests.some(item => item.method === 'workspace.complete' && item.params.session_id === rootId));
      checks.push('context completion queries the execution host and inserts an unsent reference');

      progress('text and PNG upload/preview');
      await page.locator('input[type=file]').setInputFiles({ name: 'browser-note.txt', mimeType: 'text/plain', buffer: Buffer.from('Browser attachment body is visible in history.') });
      await page.getByText('browser-note.txt · Ready', { exact: true }).waitFor();
      await send(page, 'Read the attached browser note.');
      const attachedText = page.locator('[data-message-role="user"]').filter({ hasText: 'Read the attached browser note.' });
      await eventually(() => attachedText.evaluate(element => !!element.closest('[data-reading-seq]')?.getAttribute('data-reading-seq')), { description: 'attachment has canonical history sequence' });
      await attachedText.getByRole('button', { name: 'Preview Attachment 1', exact: true }).click();
      const attachmentDialog = page.getByRole('dialog', { name: 'Attachment 1', exact: true });
      await attachmentDialog.getByText('Browser attachment body is visible in history.', { exact: true }).waitFor();
      await attachmentDialog.getByRole('button', { name: 'Close', exact: true }).click();
      const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=', 'base64');
      await page.locator('input[type=file]').setInputFiles({ name: 'tiny.png', mimeType: 'image/png', buffer: png });
      const imagePreview = page.getByRole('button', { name: 'Preview tiny.png', exact: true });
      await imagePreview.waitFor();
      await eventually(async () => (await imagePreview.getAttribute('title')) === 'tiny.png', { description: 'PNG upload completes' });
      await eventually(() => imagePreview.getByAltText('tiny.png', { exact: true }).evaluate(image => image.complete && image.naturalWidth === 1 && image.naturalHeight === 1), { description: 'draft PNG thumbnail decodes' });
      await send(page, 'Inspect the tiny image.');
      await eventually(async () => page.locator('[data-message-role="user"]').filter({ hasText: 'Inspect the tiny image.' }).getByAltText('Attachment 1', { exact: true }).evaluate(image => image.complete && image.naturalWidth === 1 && image.naturalHeight === 1), { description: 'inline PNG attachment preview decodes' });
      checks.push('real text and image uploads reach scoped history previews with automatic inline PNG decoding');


      progress('two-client permission resolution');
      const peer = await context.newPage(); const peerObserved = observe(peer); observations.push(peerObserved);
      await peer.goto(origin() + route(rootId)); await ready(peer);
      const policy = await session.permissions.policy(callDeadline());
      await session.permissions.setMode({ mode: 'prompt', expected_revision: policy.revision }, randomUUID(), callDeadline());
      await send(page, `permission:${name}`);
      await page.getByRole('button', { name: 'Allow once', exact: true }).waitFor();
      await peer.getByRole('button', { name: 'Allow once', exact: true }).waitFor();
      await accessible(page, 'pending permission');
      const permission = (await session.permissions.list({ pending_only: true }, callDeadline())).items[0];
      assert.ok(permission);
      await page.getByRole('button', { name: 'Allow once', exact: true }).click();
      await page.getByRole('button', { name: 'Allow once', exact: true }).waitFor({ state: 'hidden' });
      await peer.getByRole('button', { name: 'Allow once', exact: true }).waitFor({ state: 'hidden' });
      const pending = await session.permissions.list({ pending_only: true }, callDeadline());
      assert.equal(pending.items.filter(item => item.operation_id === permission.operation_id).length, 0);
      assert.equal(observed.requests.filter(item => item.method === 'permissions.resolve').length, 1);
      assert.equal(peerObserved.requests.filter(item => item.method === 'permissions.resolve').length, 0);
      checks.push('two real browser clients share one permission resolution');
      progress('concurrent command identities and independent drafts');
      // Drafts are shared by runtime/root/recipient. Exercise concurrent recovery
      // writes with distinct recipients instead of racing edits to one draft.
      const other = await fixture.createRoot(client);
      await peer.goto(origin() + route(other.root.id)); await ready(peer);
      const beforeParallel = [observed.requests.length, peerObserved.requests.length];
      // Both browser engines share this fixture; released holds are one-shot.
      // Include the browser identity so the second run remains unfinished too.
      for (let index = 0; index < 8; index++) await Promise.all([send(page, `hold:Parallel ${name} A ${index}`), send(peer, `hold:Parallel ${name} B ${index}`)]);
      const submitted = [...observed.requests.slice(beforeParallel[0]), ...peerObserved.requests.slice(beforeParallel[1])].filter(item => item.method === 'sessions.submit');
      assert.equal(submitted.length, 16);
      const saved = await page.evaluate(() => JSON.parse(localStorage.getItem('whip.web.recovery.v4')).map(entry => entry.record));
      // Completed native commands are deliberately forgotten. Keep these turns
      // held until every exact original request is proved present in the shared journal.
      for (const request of submitted) assert.ok(saved.some(record => record.runtimeID === fixture.info.runtime_id
        && record.clientID === request.params.identity.client_id
        && JSON.stringify(JSON.parse(record.request).params) === JSON.stringify(request.params)), 'Concurrent tabs lost an unfinished exact request');
      for (let index = 0; index < 8; index++) for (const recipient of ['A', 'B']) fixture.release(`Parallel ${name} ${recipient} ${index}`);
      for (const [target, prefix] of [[session, `hold:Parallel ${name} A`], [client.session(other.root.id), `hold:Parallel ${name} B`]]) {
        const texts = await eventually(async () => {
          const messages = await messagesOf(target);
          const sent = messages.filter(message => message.role === 'user' && textOf(message).startsWith(prefix)).map(textOf);
          return sent.length >= 8 ? sent : false;
        }, { description: `${prefix} messages persist exactly once` });
        assert.deepEqual(texts, Array.from({ length: 8 }, (_, index) => `${prefix} ${index}`));
      }
      checks.push('concurrent tabs retain all unfinished exact requests and commit each once');
      await page.getByLabel('Message WHIP', { exact: true }).fill('Root A draft survives another tab');
      await peer.getByLabel('Message WHIP', { exact: true }).fill('Root B draft survives another tab');
      await eventually(async () => (await page.evaluate(() => Object.keys(localStorage).filter(key => key.startsWith('whip.web.draft.v1:')).map(key => localStorage.getItem(key)))).filter(text => text?.includes('draft survives another tab')).length === 2, { description: 'independent recipient drafts both persist' });
      await page.reload(); await ready(page); await peer.reload(); await ready(peer);
      assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Root A draft survives another tab');
      assert.equal(await peer.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Root B draft survives another tab');
      checks.push('independent tabs preserve recipient drafts across reloads');

      await peer.close();

      progress('hover history-read bounds and route-owned inspector');
      const watchedBeforeHover = observed.requests.filter(item => item.method === 'sessions.history_page').length;
      await page.locator(`a[href="${route(other.root.id)}"]`).first().hover();
      // Longer than the router's ordinary intent-preload delay. Hovering a link
      // must never acquire a root view or hydrate history.
      await page.waitForTimeout(300);
      assert.equal(observed.requests.filter(item => item.method === 'sessions.history_page').length, watchedBeforeHover);
      await page.goto(origin() + route(rootId) + '?panel=goals'); await ready(page);
      const details = page.getByRole('dialog', { name: 'Session details', exact: true });
      await details.getByRole('combobox', { name: 'Inspector section', exact: true }).filter({ hasText: 'Goals & schedules' }).waitFor();
      await accessible(page, 'goals and schedules inspector');
      await page.reload(); await ready(page);
      await details.getByRole('combobox', { name: 'Inspector section', exact: true }).filter({ hasText: 'Goals & schedules' }).waitFor();
      await details.getByRole('button', { name: 'Close', exact: true }).click();
      await details.waitFor({ state: 'hidden' });
      assert.equal(new URL(page.url()).searchParams.has('panel'), false);
      checks.push('route-owned details survive reload; hovering session links performs no history hydration');

      if (name === 'chromium') {
        progress('renderer freeze/thaw');
        const lifecycle = await context.newCDPSession(page);
        const frozenText = 'Authoritative work completed while the renderer was frozen.';
        try {
          await lifecycle.send('Page.setWebLifecycleState', { state: 'frozen' });
          assert.equal((await complete(session, frozenText)).turn.state, 'succeeded');
        } finally {
          await lifecycle.send('Page.setWebLifecycleState', { state: 'active' });
          await lifecycle.detach();
        }
        await eventually(async () => (await page.locator('[data-message-id]').filter({ hasText: frozenText }).count()) === 2, { description: 'renderer thaw converges to authored input and response' });
        assert.equal((await fixture.effects()).filter(text => text === frozenText).length, 1);
        checks.push('simulated renderer freeze/thaw converges without repeating work (not physical sleep)');
      }

      progress('daemon crash/restart recovery');
      const held = `hold:restart-${name}`;
      await send(page, held);
      const heldRequest = observed.requests.find(item => item.method === 'sessions.submit' && item.params.parts?.some(part => part.type === 'text' && part.text === held));
      assert.ok(heldRequest);
      await eventually(async () => (await fixture.effects()).filter(item => item === held).length === 1, { description: 'accepted work began before crash' });
      const initializesBefore = observed.requests.filter(item => item.method === 'initialize').length;
      const started = performance.now();
      await fixture.crashAndRestart();
      client = await fixture.connect(client.clientID); session = client.session(rootId);
      await ready(page);
      await eventually(async () => observed.requests.filter(item => item.method === 'initialize').length > initializesBefore, { description: 'browser reinitializes after daemon restart' });
      recovery.push(performance.now() - started);
      assert.equal((await fixture.effects()).filter(item => item === held).length, 1, 'Restart must not repeat uncertain external effects');
      await eventually(async () => (await client.call('receipts.match', { method: 'sessions.submit', params_base64: Buffer.from(JSON.stringify(heldRequest.params)).toString('base64') }, callDeadline())).turn?.state === 'interrupted', { description: 'exact accepted browser command has a durable interrupted outcome' });
      await eventually(async () => !(await page.getByRole('button', { name: 'Pause this turn', exact: true }).count()), { description: 'browser retires the interrupted turn control' });
      checks.push('crash/restart reconnect without duplicate admitted work');

      progress('wrong-runtime route guard');
      const wrong = await context.newPage(); const wrongObserved = observe(wrong); observations.push(wrongObserved);
      activePage = wrong;
      await wrong.goto(origin() + `/h/wrong-runtime/s/${rootId}`);
      await wrong.getByText('Unavailable host wrong-ru is unavailable. Connect it to continue this session.', { exact: true }).waitFor();
      assert.equal(wrongObserved.requests.filter(item => ['sessions.get', 'sessions.observe', 'sessions.history_page'].includes(item.method)).length, 0);
      await wrong.close(); activePage = page; checks.push('wrong-runtime routes never open or subscribe roots');

      progress('phone layout, scroll anchoring and latest');
      for (let index = 0; index < 28; index++) await complete(session, `Message ${index}\n\n${'A readable paragraph for scrolling. '.repeat(18)}`);
      const phone = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true }); const phoneObserved = observe(phone); observations.push(phoneObserved);
      activePage = phone;
      phone.setDefaultTimeout(15_000);
      phone.setDefaultNavigationTimeout(30_000);
      await phone.goto(origin() + route(rootId)); await ready(phone);
      for (const width of [320, 390]) {
        await phone.setViewportSize({ width, height: 844 });
        const clipped = await phone.locator('header').first().evaluate(header => {
          const bounds = header.getBoundingClientRect();
          return [...header.querySelectorAll('button,a,[role="button"]')].flatMap(element => {
            const rect = element.getBoundingClientRect();
            if (!rect.width || !rect.height || getComputedStyle(element).visibility === 'hidden') return [];
            return rect.left < 0 || rect.right > innerWidth || rect.top < bounds.top || rect.bottom > bounds.bottom
              ? [{ label: element.getAttribute('aria-label') ?? element.textContent, left: rect.left, right: rect.right, width: innerWidth }] : [];
          });
        });
        assert.deepEqual(clipped, [], `Header controls are clipped at ${width}px`);
        await phone.screenshot({ path: join(resultsDirectory, `${name}-phone-${width}.png`), fullPage: true });
      }
      const measurements = await phone.getByRole('button', { name: 'Send message', exact: true }).evaluate(element => {
        const rect = element.getBoundingClientRect();
        return { overflow: document.documentElement.scrollWidth > innerWidth, top: rect.top, bottom: rect.bottom, height: rect.height, viewport: innerHeight };
      });
      assert.equal(measurements.overflow, false, JSON.stringify(measurements));
      assert.ok(measurements.bottom <= measurements.viewport && measurements.top >= 0, `Composer outside phone viewport: ${JSON.stringify(measurements)}`);
      const conversation = phone.getByLabel('Conversation', { exact: true });
      // Following stops on user input, not programmatic layout/scroll changes.
      await conversation.hover();
      await phone.mouse.wheel(0, -300);
      await phone.getByRole('button', { name: 'Latest', exact: true }).waitFor();
      // Reading in the middle isolates incoming output from near-top pagination,
      // which legitimately changes scrollTop while preserving a message anchor.
      await conversation.evaluate(element => { element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
      const stableAnchor = async () => {
        let previous;
        let stableSince = performance.now();
        return eventually(async () => {
          const current = await conversation.evaluate(element => {
            const viewport = element.getBoundingClientRect();
            const row = [...element.querySelectorAll('[data-reading-id]')].find(row => {
              const rect = row.getBoundingClientRect();
              return rect.bottom > viewport.top && rect.top < viewport.bottom;
            });
            return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - viewport.top } : null;
          });
          // Touch scrolling defers measurement corrections until it settles.
          // Two adjacent polls can otherwise capture a temporary position.
          if (current && previous?.id === current.id && Math.abs(previous.offset - current.offset) < 1) {
            if (performance.now() - stableSince >= 300) return current;
          } else stableSince = performance.now();
          previous = current;
          return false;
        }, { description: 'stable visible reading anchor' });
      };
      const before = await stableAnchor();
      const requestsBefore = phoneObserved.requests.length;
      await complete(session, 'New output must not steal the reading position.');
      await eventually(() => phoneObserved.requests.slice(requestsBefore).some(item => item.method === 'sessions.observe' && phoneObserved.replies.has(item.id)), { description: 'phone receives completed turn snapshot' });
      await phone.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      const after = await stableAnchor();
      assert.equal(after.id, before.id, `New output changed the reading anchor: ${JSON.stringify({ before, after })}`);
      assert.ok(Math.abs(after.offset - before.offset) < 5, `New output moved the reading anchor: ${JSON.stringify({ before, after })}`);
      assert.equal(phoneObserved.requests.slice(requestsBefore).filter(item => item.method === 'sessions.history_page').length, 0, 'Incoming-output check must not also paginate history');
      await phone.getByRole('button', { name: 'Latest', exact: true }).click();
      await eventually(async () => conversation.evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'jump to latest' });
      progress('phone prompt and permission denial');
      await send(phone, `permission:phone-${name}`);
      await phone.getByRole('button', { name: 'Deny', exact: true }).click();
      await phone.getByRole('button', { name: 'Deny', exact: true }).waitFor({ state: 'hidden' });
      await eventually(() => phone.getByLabel('Message WHIP', { exact: true }).evaluate(element => document.activeElement === element), { description: 'permission resolution returns focus to the same composer' });
      await eventually(async () => !(await session.activity(callDeadline())).active_turn, { description: 'phone permission denial resolves the turn' });
      progress('phone stops turn from another client');
      // Daemon completion precedes the browser's replay. Retire the previous
      // turn's control before admitting new work so this clicks the new target.
      await phone.getByRole('button', { name: 'Pause this turn', exact: true }).waitFor({ state: 'hidden' });
      const remoteTurn = session.submission([{ type: 'text', text: `hold:phone-stop-${name}` }], randomUUID());
      stoppingCommand = remoteTurn;
      await remoteTurn.send(callDeadline());
      const expectedTurn = await eventually(async () => (await session.activity(callDeadline())).active_turn?.id, { description: 'new remote turn is active before phone cancellation' });
      await phone.getByRole('button', { name: 'Pause this turn', exact: true }).click();
      await eventually(() => phoneObserved.requests.some(item => item.method === 'turns.cancel' && item.params.turn_id === expectedTurn), { description: 'phone submits cancellation for the new exact turn' });
      assert.equal((await remoteTurn.wait(callDeadline())).turn.state, 'cancelled');
      await phone.getByRole('button', { name: 'Pause this turn', exact: true }).waitFor({ state: 'hidden' });
      checks.push('touch viewport submits, denies permission, and stops a turn begun by another client');
      progress('phone accessibility');
      await accessible(phone, 'phone conversation');
      checks.push('no critical or serious Axe violations in empty, pending, inspector, and phone states');
      await phone.screenshot({ path: join(resultsDirectory, `${name}-phone.png`), fullPage: true });
      await phone.close(); checks.push('320/390px phone header controls, readable scroll anchoring and jump to latest');
      assert.deepEqual([...observed.errors, ...peerObserved.errors, ...wrongObserved.errors, ...phoneObserved.errors], []);
      results[name] = { passed: true, checks, acceptance_ms: milliseconds(observed.acceptance), event_to_view_ms: milliseconds(presentationLatency), restart_recovery_ms: milliseconds(recovery), receipt_match_requests: observed.requests.filter(item => item.method === 'receipts.match').length };
      await page.screenshot({ path: join(resultsDirectory, `${name}-desktop.png`), fullPage: true });
    } catch (error) {
      log(name, `FAILED during ${phase}: ${error.stack ?? error}`);
      const pendingCommand = stoppingCommand ? await stoppingCommand.check({ signal: AbortSignal.timeout(3_000) }).catch(error => ({ lookupError: String(error) })) : undefined;
      const stopControls = await activePage.evaluate(() => [...document.querySelectorAll('button')].filter(button => button.getAttribute('aria-label') === 'Pause this turn').map(button => ({ text: button.textContent, disabled: button.disabled, visible: !!button.getClientRects().length }))).catch(error => ({ readError: String(error) }));
      results[name] = { passed: false, phase, checks, pendingCommand, stopControls, error: String(error), cause: error.cause ? String(error.cause) : undefined, stack: error.stack, browserErrors: observations.flatMap(item => item.errors), rpcErrors: observations.flatMap(item => item.rpcErrors), recentRequests: observations.flatMap(item => item.requests.slice(-20)).map(item => ({ method: item.method, operation: item.params?.operation, commandId: item.params?.command_id, turnId: item.params?.payload?.turn_id, text: item.params?.payload?.text })) };
      await activePage.screenshot({ path: join(resultsDirectory, `${name}-failure.png`), fullPage: true, timeout: 5_000 }).catch(() => {});
      await writeFile(join(resultsDirectory, `${name}-failure.txt`), await activePage.locator('body').innerText({ timeout: 5_000 }).catch(() => 'Page unavailable'));
      await writeFile(join(resultsDirectory, `${name}-failure.html`), await activePage.content().catch(() => 'Page unavailable'));
    } finally {
      clearTimeout(deadline);
      await browser.close();
      await writeFile(join(resultsDirectory, 'report.json'), JSON.stringify(results, null, 2) + '\n');
      log(name, results[name]?.passed ? 'PASSED' : 'FAILED (artifacts saved)');
    }
  }
} finally {
  await writeFile(join(resultsDirectory, 'report.json'), JSON.stringify(results, null, 2) + '\n');
  console.log(JSON.stringify(results, null, 2));
  log('fixture', 'closing isolated daemon');
  await fixture.close();
  log('fixture', 'closed');
}
if (Object.values(results).some(result => !result.passed)) process.exitCode = 1;
