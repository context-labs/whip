import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// The fixture embeds the packaged production app. Build and pack before running;
// no user daemon, home, credentials, or provider account is used by these tests.
const requested = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
const resultsDirectory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-web-browser-results';
await mkdir(resultsDirectory, { recursive: true });
const log = (scope, phase) => console.log(`[${new Date().toISOString()}] ${scope}: ${phase}`);
log('fixture', 'compiling and starting isolated daemon');
const fixture = await startFixture();
log('fixture', `ready at ${fixture.directory}`);
await writeFile(join(fixture.directory, 'home', 'models.json'), JSON.stringify({
  provider: { fetchedAt: new Date().toISOString(), models: [
    { id: 'model' }, { id: 'replacement', reasoningEfforts: ['low', 'medium', 'high'] },
  ] },
}));
const results = {};
const axeSource = await readFile(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
const milliseconds = samples => {
  const sorted = samples.slice().sort((a, b) => a - b);
  return { count: sorted.length, median: sorted[Math.floor(sorted.length / 2)] ?? 0, p95: sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * .95) - 1)] ?? 0 };
};
const origin = () => fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
const route = rootId => `/h/${fixture.info.runtime_id}/s/${rootId}`;

function observe(page) {
  const requests = [];
  const replies = new Set();
  const errors = [];
  const inflight = new Map();
  const acceptance = [];
  const outcomes = [];
  const rpcErrors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', event => {
    if (event.type() === 'error' && /content.security.policy|violates.*directive|refused to (execute|apply|load)|unsafe-eval/i.test(event.text())) errors.push(event.text());
  });
  page.on('websocket', socket => {
    socket.on('framesent', ({ payload }) => {
      try {
        const message = JSON.parse(String(payload));
        requests.push(message);
        if (message.method === 'command.submit') inflight.set(message.id, performance.now());
      } catch { /* Non-JSON frames are diagnosed by the SDK. */ }
    });
    socket.on('framereceived', ({ payload }) => {
      try {
        const message = JSON.parse(String(payload));
        if (message.id && !message.method) replies.add(message.id);
        if (message.error) rpcErrors.push({ id: message.id, error: message.error });
        if (message.result?.command_id && message.result?.status) outcomes.push(message.result);
        if (inflight.has(message.id)) { acceptance.push(performance.now() - inflight.get(message.id)); inflight.delete(message.id); }
      } catch { /* See SDK protocol validation. */ }
    });
  });
  return { requests, replies, errors, acceptance, outcomes, rpcErrors };
}
async function measurePresentation(page) {
  await page.addInitScript(() => {
    const samples = []; const pending = [];
    window.__whipEventToView = samples;
    const check = () => {
      for (let index = pending.length - 1; index >= 0; index--) {
        const item = pending[index];
        const row = document.querySelector(`[data-message-id="live-tool:${CSS.escape(item.id)}"]`);
        if (row?.textContent?.includes(item.text)) { samples.push(performance.now() - item.start); pending.splice(index, 1); }
      }
    };
    new MutationObserver(check).observe(document, { subtree: true, childList: true, characterData: true });
    const NativeWebSocket = window.WebSocket;
    window.WebSocket = class extends NativeWebSocket {
      constructor(...args) {
        super(...args);
        this.addEventListener('message', message => {
          try {
            const event = JSON.parse(message.data).params?.event;
            if (!event?.kind?.startsWith('stream.tool.') || !event.payload?.id) return;
            let text = event.payload.args ?? event.payload.text ?? event.payload.result;
            if (event.payload.args) { try { text = JSON.parse(text).code ?? text; } catch {} }
            if (typeof text !== 'string' || !text) return;
            pending.push({ id: event.payload.id, text, start: performance.now() });
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
  await page.getByRole('button', { name: 'Send message', exact: true }).click();
  await eventually(async () => (await page.getByLabel('Message WHIP', { exact: true }).inputValue()) === '', { description: 'authoritative acceptance clears draft' });
}
async function selectTheme(page, id) {
  await page.getByRole('link', { name: 'Settings', exact: true }).first().click();
  await page.getByRole('button', { name: 'System appearance', exact: true }).last().click();
  const dialog = page.getByRole('dialog', { name: 'Appearance', exact: true });
  await dialog.getByRole('combobox', { name: 'Search themes' }).fill(id);
  await page.getByRole('option', { name: new RegExp(`^${id}\\s*Dark$`) }).click();
  await dialog.getByRole('button', { name: 'Done', exact: true }).click();
  await page.waitForFunction(value => document.documentElement.dataset.theme === value, id);
}
async function accessible(page, state) {
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
  const discovery = await (await fetch(origin() + '/api/v3/web', { signal: AbortSignal.timeout(10_000) })).json();
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
    const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `web-test-${name}-${crypto.randomUUID()}`, clientKind: 'human' });
    const checks = [];
    const recovery = [];
    let rootId;
    let stoppingCommand;
    const deadline = setTimeout(() => {
      log(name, `120s browser deadline exceeded during ${phase}`);
      client.close();
      void browser.close();
    }, 120_000);
    deadline.unref();
    try {
      progress('attach/deep link and empty-state accessibility');
      await client.connect();
      const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result({ signal: AbortSignal.timeout(15_000) });
      assert.equal(created.status, 'succeeded'); rootId = created.result.root_id;
      const session = client.session(rootId);
      await session.rename(`Browser ${name}`).result({ signal: AbortSignal.timeout(15_000) });
      const response = await page.goto(origin() + route(rootId));
      const csp = response.headers()['content-security-policy'];
      assert.ok(csp && !csp.includes('unsafe-eval') && !csp.includes('unsafe-inline'));
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
      await page.getByRole('option', { name: 'replacement', exact: true }).click();
      await eventually(async () => (await session.snapshot()).meta.model === 'replacement', { description: 'composer model selection reaches the host' });
      await modelSearch.waitFor({ state: 'hidden' });
      // Host acceptance precedes browser replay, which changes toolbar geometry.
      // Wait for the displayed model and popup focus return before another click.
      await eventually(async () => (await modelTrigger.innerText()).includes('replacement'), { description: 'browser renders the selected model' });
      await eventually(() => modelTrigger.evaluate(element => document.activeElement === element), { description: 'model picker returns focus to its trigger' });
      const effortTrigger = page.getByRole('button', { name: 'Reasoning effort', exact: true });
      await effortTrigger.click();
      await page.getByRole('option', { name: 'Medium', exact: true }).click();
      await eventually(async () => (await session.snapshot()).meta.effort === 'medium');
      await page.screenshot({ path: join(resultsDirectory, `${name}-model-picker.png`) });
      assert.equal(await composer.inputValue(), 'Keep this draft while changing the model');
      assert.equal((await session.snapshot()).messages?.length ?? 0, 0, 'Changing models submitted the message draft');
      await page.reload(); await ready(page);
      assert.ok((await modelTrigger.innerText()).includes('replacement'));
      assert.equal(await composer.inputValue(), 'Keep this draft while changing the model');
      checks.push('composer grows and shrinks, model/effort selections apply, and draft/model survive reload');

      progress('stream grouping, theme switch and reload');
      await send(page, 'hold:tool-stream');
      await eventually(async () => (await page.locator('[data-message-id^="live-tool:"]').count()) === 2, { description: 'two cumulative tool rows' });
      assert.equal(await page.locator('[data-message-id^="live:"]').count(), 1, 'Text deltas must append to one live row');
      const tools = page.locator('[data-message-id^="live-tool:"]');
      assert.equal(await modelTrigger.isEnabled(), false);
      assert.equal(await effortTrigger.isEnabled(), false);
      for (let index = 0; index < 2; index++) {
        await tools.nth(index).locator('summary').click();
        const text = await tools.nth(index).innerText();
        assert.match(text, /print\(1\)/);
        assert.equal((text.match(/first/g) ?? []).length, 1);
        assert.equal((text.match(/second/g) ?? []).length, 1);
      }
      const presentationLatency = await page.evaluate(() => window.__whipEventToView);
      assert.ok(presentationLatency.length >= 2, 'Presentation latency probe observed no tool updates');
      await page.getByLabel('Message WHIP', { exact: true }).fill(`Unsent ${name} draft`);
      await selectTheme(page, 'nord');
      await page.locator(`a[href="${route(rootId)}"]`).first().click();
      await ready(page);
      assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), `Unsent ${name} draft`);
      assert.equal(await page.getByRole('region', { name: 'Conversation', exact: true }).count(), 1);
      assert.equal(await page.locator('[data-message-id^="live-tool:"]').count(), 2);
      await page.reload(); await ready(page);
      assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), `Unsent ${name} draft`);
      assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'nord');
      assert.equal(await page.getByRole('region', { name: 'Conversation', exact: true }).count(), 1);
      assert.equal(await page.locator('[data-message-id^="live-tool:"]').count(), 2);
      await fixture.release('tool-stream');
      await eventually(async () => Object.keys((await session.snapshot()).active_turns).length === 0, { description: 'held tool turn completes' });
      checks.push('delta/cumulative streams stay grouped across theme changes and reload; draft/theme persist');

      progress('host context completion');
      await writeFile(join(fixture.directory, 'web-browser-context.txt'), 'Host-side completion fixture.');
      await page.getByRole('button', { name: 'Add context', exact: true }).click();
      await page.getByRole('combobox', { name: 'Search on the host', exact: true }).fill('web-browser-context');
      await page.getByRole('option', { name: /web-browser-context\.txt/ }).click();
      assert.match(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), /@web-browser-context\.txt/);
      assert.ok(observed.requests.some(item => item.method === 'workspace.complete' && item.params.agent_id === rootId));
      checks.push('context completion queries the execution host and inserts an unsent reference');

      progress('text and PNG upload/preview');
      await page.locator('input[type=file]').setInputFiles({ name: 'browser-note.txt', mimeType: 'text/plain', buffer: Buffer.from('Browser attachment body is visible in history.') });
      await page.getByText('browser-note.txt · Ready', { exact: true }).waitFor();
      await send(page, 'Read the attached browser note.');
      await eventually(async () => (await page.locator('[data-message-id]').filter({ hasText: 'Browser attachment body is visible in history.' }).count()) >= 1, { description: 'text attachment is materialized in transcript' });
      const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=', 'base64');
      await page.locator('input[type=file]').setInputFiles({ name: 'tiny.png', mimeType: 'image/png', buffer: png });
      await page.getByText('tiny.png · Ready', { exact: true }).waitFor();
      await send(page, 'Inspect the tiny image.');
      await page.getByRole('button', { name: 'View image attachment', exact: true }).first().click();
      await eventually(async () => page.getByAltText('Attached image', { exact: true }).first().evaluate(image => image.complete && image.naturalWidth === 1 && image.naturalHeight === 1), { description: 'explicit PNG attachment preview decodes' });
      checks.push('real text and image uploads reach history with explicit bounded PNG preview');


      progress('two-client permission resolution');
      const peer = await context.newPage(); const peerObserved = observe(peer); observations.push(peerObserved);
      await peer.goto(origin() + route(rootId)); await ready(peer);
      await client.permissions.setMode(rootId, true).result({ signal: AbortSignal.timeout(15_000) });
      await send(page, `permission:${name}`);
      await page.getByRole('button', { name: 'Allow once', exact: true }).waitFor();
      await peer.getByRole('button', { name: 'Allow once', exact: true }).waitFor();
      await accessible(page, 'pending permission');
      const permission = (await session.snapshot()).permissions.find(item => item.status === 'pending');
      assert.ok(permission);
      await page.getByRole('button', { name: 'Allow once', exact: true }).click();
      await page.getByRole('button', { name: 'Allow once', exact: true }).waitFor({ state: 'hidden' });
      await peer.getByRole('button', { name: 'Allow once', exact: true }).waitFor({ state: 'hidden' });
      const snapshot = await session.snapshot();
      assert.equal((snapshot.permissions ?? []).filter(item => item.id === permission.id && item.status === 'pending').length, 0);
      assert.equal(observed.requests.filter(item => item.method === 'permission.decide').length, 1);
      assert.equal(peerObserved.requests.filter(item => item.method === 'permission.decide').length, 0);
      checks.push('two real browser clients share one permission resolution');
      progress('concurrent command identities and independent drafts');
      // Drafts are shared by runtime/root/recipient. Exercise concurrent recovery
      // writes with distinct recipients instead of racing edits to one draft.
      const other = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result({ signal: AbortSignal.timeout(15_000) });
      assert.equal(other.status, 'succeeded');
      await peer.goto(origin() + route(other.result.root_id)); await ready(peer);
      for (let index = 0; index < 8; index++) await Promise.all([send(page, `Parallel A ${index}`), send(peer, `Parallel B ${index}`)]);
      for (const [target, prefix] of [[session, 'Parallel A'], [client.session(other.result.root_id), 'Parallel B']]) {
        const texts = await eventually(async () => {
          const messages = (await target.snapshot()).messages ?? [];
          const sent = messages.filter(message => message.role === 'user' && String(message.content).startsWith(prefix)).map(message => message.content);
          return sent.length >= 8 ? sent : false;
        }, { description: `${prefix} messages persist exactly once` });
        assert.deepEqual(texts, Array.from({ length: 8 }, (_, index) => `${prefix} ${index}`));
      }
      const identities = await page.evaluate(() => JSON.parse(localStorage.getItem('whip.web.recovery.v1')).map(record => record.commandId));
      const submitted = [...observed.requests, ...peerObserved.requests].filter(item => item.method === 'command.submit').map(item => item.params.command_id);
      assert.ok(submitted.every(id => identities.includes(id)), 'Concurrent tabs lost command recovery identities');
      checks.push('concurrent tabs retain all submitted recovery identities');
      await page.getByLabel('Message WHIP', { exact: true }).fill('Root A draft survives another tab');
      await peer.getByLabel('Message WHIP', { exact: true }).fill('Root B draft survives another tab');
      await eventually(async () => (await page.evaluate(() => Object.keys(localStorage).filter(key => key.startsWith('whip.web.draft.v1:')).map(key => localStorage.getItem(key)))).filter(text => text?.includes('draft survives another tab')).length === 2, { description: 'independent recipient drafts both persist' });
      await page.reload(); await ready(page); await peer.reload(); await ready(peer);
      assert.equal(await page.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Root A draft survives another tab');
      assert.equal(await peer.getByLabel('Message WHIP', { exact: true }).inputValue(), 'Root B draft survives another tab');
      checks.push('independent tabs preserve recipient drafts across reloads');

      await peer.close();

      progress('hover subscription bounds and route-owned inspector');
      const watchedBeforeHover = observed.requests.filter(item => item.method === 'events.subscribe').length;
      await page.locator(`a[href="${route(other.result.root_id)}"]`).first().hover();
      // Longer than the router's ordinary intent-preload delay. Hovering a link
      // must never acquire a root view or one of the daemon's subscriptions.
      await page.waitForTimeout(300);
      assert.equal(observed.requests.filter(item => item.method === 'events.subscribe').length, watchedBeforeHover);
      await page.goto(origin() + route(rootId) + '?panel=goals'); await ready(page);
      const details = page.getByRole('dialog', { name: 'Session details', exact: true });
      await details.getByRole('combobox', { name: 'Inspector section', exact: true }).filter({ hasText: 'Goals & schedules' }).waitFor();
      await accessible(page, 'goals and schedules inspector');
      await page.reload(); await ready(page);
      await details.getByRole('combobox', { name: 'Inspector section', exact: true }).filter({ hasText: 'Goals & schedules' }).waitFor();
      await details.getByRole('button', { name: 'Close', exact: true }).click();
      await details.waitFor({ state: 'hidden' });
      assert.equal(new URL(page.url()).searchParams.has('panel'), false);
      checks.push('route-owned details survive reload; hovering session links consumes no subscriptions');

      if (name === 'chromium') {
        progress('renderer freeze/thaw');
        const lifecycle = await context.newCDPSession(page);
        const frozenText = 'Authoritative work completed while the renderer was frozen.';
        try {
          await lifecycle.send('Page.setWebLifecycleState', { state: 'frozen' });
          assert.equal((await session.submit({ text: frozenText }).result({ signal: AbortSignal.timeout(15_000) })).status, 'succeeded');
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
      const heldId = observed.requests.find(item => item.method === 'command.submit' && item.params.payload?.text === held)?.params.command_id;
      assert.ok(heldId);
      await eventually(async () => (await fixture.effects()).filter(item => item === held).length === 1, { description: 'accepted work began before crash' });
      const initializesBefore = observed.requests.filter(item => item.method === 'initialize').length;
      const started = performance.now();
      await fixture.crashAndRestart();
      await client.whenConnected(AbortSignal.timeout(15_000));
      await ready(page);
      await eventually(async () => observed.requests.filter(item => item.method === 'initialize').length > initializesBefore, { description: 'browser reinitializes after daemon restart' });
      recovery.push(performance.now() - started);
      assert.equal((await fixture.effects()).filter(item => item === held).length, 1, 'Restart must not repeat uncertain external effects');
      await eventually(() => observed.outcomes.some(item => item.command_id === heldId && item.status === 'interrupted'), { description: 'original browser command reconciles to interrupted' });
      checks.push('crash/restart reconnect without duplicate admitted work');

      progress('wrong-runtime route guard');
      const wrong = await context.newPage(); const wrongObserved = observe(wrong); observations.push(wrongObserved);
      await wrong.goto(origin() + `/h/wrong-runtime/s/${rootId}`);
      await wrong.getByRole('heading', { name: 'This session belongs to another host' }).waitFor();
      assert.equal(wrongObserved.requests.filter(item => ['root.snapshot', 'events.subscribe', 'history.page'].includes(item.method)).length, 0);
      await wrong.close(); checks.push('wrong-runtime routes never open or subscribe roots');

      progress('phone layout, scroll anchoring and latest');
      for (let index = 0; index < 28; index++) await session.submit({ text: `Message ${index}\n\n${'A readable paragraph for scrolling. '.repeat(18)}` }).result({ signal: AbortSignal.timeout(15_000) });
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
      // Reading in the middle isolates incoming output from near-top pagination,
      // which legitimately changes scrollTop while preserving a message anchor.
      await conversation.evaluate(element => { element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
      await phone.getByRole('button', { name: 'Latest', exact: true }).waitFor();
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
      await session.submit({ text: 'New output must not steal the reading position.' }).result({ signal: AbortSignal.timeout(15_000) });
      await eventually(() => phoneObserved.requests.slice(requestsBefore).some(item => item.method === 'root.snapshot' && phoneObserved.replies.has(item.id)), { description: 'phone receives completed turn snapshot' });
      await phone.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      const after = await stableAnchor();
      assert.equal(after.id, before.id, `New output changed the reading anchor: ${JSON.stringify({ before, after })}`);
      assert.ok(Math.abs(after.offset - before.offset) < 5, `New output moved the reading anchor: ${JSON.stringify({ before, after })}`);
      assert.equal(phoneObserved.requests.slice(requestsBefore).filter(item => item.method === 'history.page').length, 0, 'Incoming-output check must not also paginate history');
      await phone.getByRole('button', { name: 'Latest', exact: true }).click();
      await eventually(async () => conversation.evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'jump to latest' });
      progress('phone prompt and permission denial');
      await send(phone, `permission:phone-${name}`);
      await phone.getByRole('button', { name: 'Deny', exact: true }).click();
      await phone.getByRole('button', { name: 'Deny', exact: true }).waitFor({ state: 'hidden' });
      await eventually(() => phone.getByLabel('Message WHIP', { exact: true }).evaluate(element => document.activeElement === element), { description: 'permission resolution returns focus to the same composer' });
      await eventually(async () => Object.keys((await session.snapshot()).active_turns).length === 0, { description: 'phone permission denial resolves the turn' });
      progress('phone stops turn from another client');
      // Daemon completion precedes the browser's replay. Retire the previous
      // turn's control before admitting new work so this clicks the new target.
      await phone.getByRole('button', { name: 'Pause this turn', exact: true }).waitFor({ state: 'hidden' });
      const remoteTurn = session.submit({ text: `hold:phone-stop-${name}` });
      stoppingCommand = remoteTurn;
      await remoteTurn.accepted({ signal: AbortSignal.timeout(15_000) });
      const expectedTurn = await eventually(async () => (await session.snapshot()).active_turns[rootId], { description: 'new remote turn is active before phone cancellation' });
      await phone.getByRole('button', { name: 'Pause this turn', exact: true }).click();
      await eventually(() => phoneObserved.requests.some(item => item.method === 'command.submit' && item.params.operation === 'cancel' && item.params.payload.turn_id === expectedTurn), { description: 'phone submits cancellation for the new exact turn' });
      assert.equal((await remoteTurn.result({ signal: AbortSignal.timeout(15_000) })).status, 'cancelled');
      await phone.getByRole('button', { name: 'Pause this turn', exact: true }).waitFor({ state: 'hidden' });
      checks.push('touch viewport submits, denies permission, and stops a turn begun by another client');
      progress('phone accessibility');
      await accessible(phone, 'phone conversation');
      checks.push('no critical or serious Axe violations in empty, pending, inspector, and phone states');
      await phone.screenshot({ path: join(resultsDirectory, `${name}-phone.png`), fullPage: true });
      await phone.close(); checks.push('320/390px phone header controls, readable scroll anchoring and jump to latest');
      assert.deepEqual([...observed.errors, ...peerObserved.errors, ...wrongObserved.errors, ...phoneObserved.errors], []);
      results[name] = { passed: true, checks, acceptance_ms: milliseconds(observed.acceptance), event_to_view_ms: milliseconds(presentationLatency), restart_recovery_ms: milliseconds(recovery), command_status_requests: observed.requests.filter(item => item.method === 'command.status').length };
      await page.screenshot({ path: join(resultsDirectory, `${name}-desktop.png`), fullPage: true });
    } catch (error) {
      log(name, `FAILED during ${phase}: ${error.stack ?? error}`);
      const pendingCommand = stoppingCommand ? await stoppingCommand.status({ signal: AbortSignal.timeout(3_000) }).catch(error => ({ lookupError: String(error) })) : undefined;
      const stopControls = await activePage.evaluate(() => [...document.querySelectorAll('button')].filter(button => button.getAttribute('aria-label') === 'Pause this turn').map(button => ({ text: button.textContent, disabled: button.disabled, visible: !!button.getClientRects().length }))).catch(error => ({ readError: String(error) }));
      results[name] = { passed: false, phase, checks, pendingCommand, stopControls, error: String(error), cause: error.cause ? String(error.cause) : undefined, stack: error.stack, browserErrors: observations.flatMap(item => item.errors), rpcErrors: observations.flatMap(item => item.rpcErrors), recentRequests: observations.flatMap(item => item.requests.slice(-20)).map(item => ({ method: item.method, operation: item.params?.operation, commandId: item.params?.command_id, turnId: item.params?.payload?.turn_id, text: item.params?.payload?.text })) };
      await activePage.screenshot({ path: join(resultsDirectory, `${name}-failure.png`), fullPage: true, timeout: 5_000 }).catch(() => {});
      await writeFile(join(resultsDirectory, `${name}-failure.txt`), await activePage.locator('body').innerText({ timeout: 5_000 }).catch(() => 'Page unavailable'));
      await writeFile(join(resultsDirectory, `${name}-failure.html`), await activePage.content().catch(() => 'Page unavailable'));
    } finally {
      clearTimeout(deadline);
      client.close();
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
