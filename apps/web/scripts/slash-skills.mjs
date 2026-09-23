import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { existsSync } from 'node:fs';
import { mkdir, mkdtemp, realpath, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, dirname, join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Run after npm run pack:web. Real packaged renderer/daemon; no user's daemon,
// settings, skills, credentials or model. Skill fixtures live in isolated roots.
const directory = process.env.WHIP_SLASH_SKILLS_RESULTS ?? '/tmp/whip-slash-skills-results';
await mkdir(directory, { recursive: true });
const reports = [];
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const launcher = { chromium, firefox }[name];
  assert.ok(launcher, `Unsupported browser ${name}`);
  if (!existsSync(launcher.executablePath())) {
    reports.push({ name, skipped: 'Browser not installed' });
    continue;
  }
  const home = await mkdtemp(join(tmpdir(), 'whip-slash-home-'));
  const frames = [], responses = [], errors = [], providerRequests = [], checks = [];
  const delays = new Set();
  const skillMethods = new Set(['host.skills.complete', 'workspace.complete']);
  const report = { name, checks, errors, providerRequests, metrics: [], coverage: 'Packaged web app + real daemon discovery; genuine RPC replies delayed in transport, no candidate/result mocks; synthetic turn runner; not native Desktop acceptance' };
  reports.push(report);
  let fixture, browser, client, page, providerServer;
  try {
    // HOME isolates the extra user-level skill roots as well as provider discovery.
    const env = { HOME: home, TMPDIR: home, WHIPCODE_HOME: join(home, 'whipcode') };
    for (const key of Object.keys(process.env)) {
      if (/(API_KEY|ACCESS_TOKEN|AUTH_TOKEN)$/.test(key)) env[key] = '';
    }
    fixture = await startFixture({ env });
    client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `slash-${crypto.randomUUID()}`, clientKind: 'sdk' });
    await client.connect();
    // Daemon TestMain replaces HOME with its own temporary directory. TMPDIR
    // keeps that directory inside our owned fixture; discover its actual path.
    const daemonHome = await realpath((await client.host.directories({ path: '~', limit: 1 })).path);
    assert.equal(dirname(daemonHome), await realpath(home));
    assert.match(basename(daemonHome), /^whip-daemon-test-home-/);
    const cwd = join(fixture.directory, 'project');
    const skills = ['accept-alpha', 'accept-alpine', 'accept-beta', ...Array.from({ length: 96 }, (_, i) => `accept-long-${String(i).padStart(2, '0')}`), 'accept-zebra'];
    const globalSkills = skills.map(skill => skill.replace('accept-', 'global-'));
    const projectCatalog = [...skills, ...globalSkills].sort();
    for (const skill of [...skills, ...globalSkills]) {
      const root = skill === 'global-beta' ? join(daemonHome, '.agents/skills')
        : skill.startsWith('global-') ? join(fixture.directory, 'home/skills') : join(cwd, '.agents/skills');
      const folder = join(root, skill);
      await mkdir(folder, { recursive: true });
      await writeFile(join(folder, 'SKILL.md'), `---
name: ${skill}
description: Synthetic ${skill} acceptance skill
---
Synthetic fixture instructions; do not access any external service.
`);
    }
    const effectsBefore = await fixture.effects();
    assert.equal((await client.providers.list()).selection.ready, false);
    assert.ok(client.getSnapshot().info.capabilities.includes('skill_catalog_completion'), 'Daemon advertises full catalog completion');
    assert.ok(client.getSnapshot().info.capabilities.includes('host_global_skill_completion'), 'Daemon advertises global discovery');
    const globalPreview = await client.call('host.skills.complete', { scope: 'global', definition: 'coding', permission_mode: 'prompt', prefix: '', limit: 1024 });
    // Exact membership excludes both the synthetic project and the repository
    // process cwd. Malformed cwd scan tripwires live in the backend unit tests.
    assert.deepEqual(globalPreview.candidates.map(candidate => candidate.text), globalSkills.map(skill => `$${skill}`));
    assert.ok(!globalPreview.truncated);
    assert.deepEqual(globalPreview.warnings ?? [], []);
    const preview = await client.call('host.skills.complete', { cwd, definition: 'coding', permission_mode: 'prompt', prefix: '', limit: 1024 });
    assert.deepEqual(preview.candidates.map(candidate => candidate.text), projectCatalog.map(skill => `$${skill}`));
    assert.ok(!preview.truncated, 'Synthetic catalog is complete');
    report.catalogSize = preview.candidates.length;
    report.globalCatalogSize = globalPreview.candidates.length;
    assert.deepEqual(await fixture.effects(), effectsBefore);
    checks.push('advertised global/catalog capabilities return exactly 100 globals from both isolated roots, then 200 project-plus-global names; no cwd leakage, provider readiness, truncation or turn execution');

    browser = await launcher.launch();
    report.browser = await browser.version();
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: 'light' });
    context.setDefaultTimeout(15_000);
    page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    await page.routeWebSocket('**/api/v3/ws', socket => {
      const upstream = socket.connectToServer();
      const pending = new Map();
      socket.onMessage(message => {
        const frame = JSON.parse(String(message));
        frames.push(frame);
        if (skillMethods.has(frame.method) || ['initialize', 'config.get'].includes(frame.method)) pending.set(String(frame.id), { id: frame.id, method: frame.method, sentAt: Date.now() });
        upstream.send(message);
      });
      upstream.onMessage(message => {
        const frame = JSON.parse(String(message));
        const request = pending.get(String(frame.id));
        if (!request) { socket.send(message); return; }
        pending.delete(String(frame.id));
        if (!skillMethods.has(request.method)) {
          responses.push({ ...request, deliveredAt: Date.now(), frame });
          socket.send(message);
          return;
        }
        // Delay the actual daemon result, not a fabricated catalog. An accidental
        // warm-typing RPC would incur this same delay and fail the assertions.
        const timer = setTimeout(() => {
          delays.delete(timer);
          responses.push({ ...request, deliveredAt: Date.now(), frame });
          socket.send(message);
        }, 1500);
        delays.add(timer);
      });
    });
    const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
    const first = page.getByLabel('Your first message', { exact: true });
    const input = page.getByLabel('Message WHIP', { exact: true });
    const list = page.getByRole('listbox', { name: 'Skills', exact: true });
    const options = list.getByRole('option');
    const commands = operation => frames.filter(frame => frame.method === 'command.submit' && frame.params.operation === operation);
    const sends = () => frames.filter(frame => frame.method === 'command.submit' && ['session.create', 'submit', 'agent.submit'].includes(frame.params.operation));
    const screenshot = label => page.screenshot({ path: join(directory, `${name}-${label}.png`) });
    const open = async (field, text = '/accept-al') => {
      await field.fill(text);
      // Reload restores the same draft. Native caret motion observes it even
      // when fill has no changed value and therefore emits no input event.
      await field.press('ArrowLeft');
      await field.press('ArrowRight');
      await expect(options).toHaveCount(text === '/accept-al' ? 2 : 32);
    };
    const catalogAcceptance = async (field, method, composer, global = false) => {
      const stem = global ? 'global' : 'accept';
      const catalog = global ? globalSkills : projectCatalog;
      const requests = () => frames.filter(frame => frame.method === method && (frame.params.scope === 'global') === global);
      const submitted = sends().length;
      await field.evaluate(element => { window.__slashAcceptanceFocus = { element, id: element.id }; });
      await field.focus();
      await expect(field).toBeFocused();
      await eventually(() => requests().length > 0);
      assert.equal(await field.inputValue(), '', `${composer}: focus preloads before slash typing`);
      assert.equal(requests().length, 1, `${composer}: one deduplicated preload`);
      assert.equal(requests()[0].params.prefix, '');
      assert.equal(requests()[0].params.limit, 1024);
      await field.fill(`/${stem}-z`);
      await expect(field).toHaveValue(`/${stem}-z`);
      await expect(page.getByText('Loading skills…', { exact: true })).toBeVisible();
      await field.press('Enter');
      assert.equal(sends().length, submitted, `${composer}: pending Enter never submits`);
      await expect(field).toHaveValue(`/${stem}-z`);
      await screenshot(`${composer}-cold`);
      await expect(options).toHaveCount(1);
      await expect(options.first()).toContainText(`/${stem}-zebra`);
      const response = responses.find(response => response.id === requests()[0].id);
      assert.ok(response && response.deliveredAt - response.sentAt >= 1500);
      assert.deepEqual(response.frame.result.candidates.map(candidate => candidate.text), catalog.map(skill => `$${skill}`));
      assert.deepEqual(response.frame.result.warnings ?? [], []);
      assert.ok(!response.frame.result.truncated);
      await screenshot(`${composer}-beyond-64`);
      const before = requests().length;
      // Dispatch native input events into the real controlled textarea. Observe
      // the DOM on the next animation frame, excluding Playwright round trips.
      // Keep the menu open >10s to cover freshness expiry during continued typing.
      const warm = await field.evaluate(async (element, stem) => {
        const cases = [
          ['/', 32, `/${stem}-alpha`], [`/${stem}-a`, 2, `/${stem}-alpha`],
          [`/${stem}-alp`, 2, `/${stem}-alpha`], [`/${stem}-alpha`, 1, `/${stem}-alpha`],
          [`/${stem}-al`, 2, `/${stem}-alpha`], [`/${stem}-z`, 1, `/${stem}-zebra`],
          [`/${stem}-NONMATCH`, 0, null], [`/${stem}-long-95`, 1, `/${stem}-long-95`],
        ];
        let loadingFlashes = 0;
        const loading = () => { if (document.body.textContent.includes('Loading skills')) loadingFlashes++; };
        const observer = new MutationObserver(loading);
        observer.observe(document.body, { subtree: true, childList: true, characterData: true });
        const samples = [];
        const start = performance.now();
        try {
          for (let i = 0; performance.now() - start < 11_000; i++) {
            const [text, count, first] = cases[i % cases.length];
            const changedAt = performance.now();
            Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(element, text);
            element.setSelectionRange(text.length, text.length);
            element.dispatchEvent(new Event('input', { bubbles: true }));
            await new Promise(resolve => requestAnimationFrame(resolve));
            const rows = [...document.querySelectorAll('[role="listbox"][aria-label="Skills"] [role="option"]')];
            loading();
            samples.push({ text, milliseconds: performance.now() - changedAt, count: rows.length, expectedCount: count, first: rows[0]?.textContent ?? null, expectedFirst: first });
            await new Promise(resolve => setTimeout(resolve, 150));
          }
        } finally { observer.disconnect(); }
        return { samples, loadingFlashes, durationMs: performance.now() - start };
      }, stem);
      report.metrics.push({ composer, coldMs: response.deliveredAt - response.sentAt, warmRequests: requests().length - before, ...warm });
      assert.equal(warm.loadingFlashes, 0, `${composer}: no warm loading flashes`);
      assert.equal(requests().length, before, `${composer}: no warm RPCs, even beyond freshness expiry`);
      for (const sample of warm.samples) {
        assert.equal(sample.count, sample.expectedCount, `${composer}: next-frame rows for ${sample.text}`);
        if (sample.expectedFirst) assert.ok(sample.first?.includes(sample.expectedFirst), `${composer}: next-frame first candidate for ${sample.text}`);
      }
      assert.equal(sends().length, submitted, `${composer}: filtering never submits`);
      checks.push(`${composer}: focus preload; real 1500ms delayed cold response; pending editing/Enter; >64 discovery; next-frame local filtering before 32-row cap; no RPC/loading flashes through 11s typing`);
    };
    await page.goto(`${origin}/?new=1&runtimeId=${fixture.info.runtime_id}`);
    await expect(first).toBeEnabled();
    assert.match(new URL(page.url()).pathname, /^\/new\//);
    // Startup can replace the initial composer while provider/config state resolves.
    // Focus once after that boundary, never refocus in a loop to force a preload.
    await eventually(() => responses.some(response => response.method === 'config.get'));
    await expect(page.getByText('Ask for approval', { exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Connect a provider to get started', exact: true })).toBeVisible();
    await expect(page.locator('[data-startup-phase="visible"]')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveText('Choose folder');
    await catalogAcceptance(first, 'host.skills.complete', 'global-new-chat', true);
    const globalRequest = frames.find(frame => frame.method === 'host.skills.complete');
    assert.deepEqual(globalRequest.params, { scope: 'global', definition: 'coding', permission_mode: 'prompt', prefix: '', limit: 1024 });
    await first.fill('/accept-');
    await expect(options).toHaveCount(0);
    await expect(page.getByText('No matching skills.', { exact: true })).toBeVisible();
    await first.fill('/global-beta');
    await expect(options).toHaveCount(1);
    await expect(options.first()).toContainText('/global-beta');
    await first.press('Enter');
    await expect(first).toHaveValue('$global-beta ');
    await expect(first).toBeFocused();
    assert.equal(await first.evaluate(element => element.selectionStart), '$global-beta '.length);
    await expect(page.getByRole('button', { name: 'Send first message', exact: true })).toBeDisabled();
    assert.deepEqual(sends(), []);
    assert.equal(frames.some(frame => frame.method === 'root.snapshot'), false);
    assert.deepEqual(await fixture.effects(), effectsBefore);
    await screenshot('global-no-folder-no-provider');
    checks.push('folderless/no-provider New Chat shows only globals from both user roots; OS-global selection preserves caret/focus with zero sends, creates or root hydration');

    // Use the actual host folder picker, not a query-string or app-state rewrite.
    // Selecting a folder updates the existing draft; no clear-folder UI is added.
    const projectDraft = decodeURIComponent(new URL(page.url()).pathname.split('/').at(-1));
    await page.getByRole('button', { name: 'Project folder', exact: true }).click();
    const folderDialog = page.getByRole('dialog', { name: 'Choose a folder', exact: true });
    await folderDialog.getByRole('button', { name: 'Edit path', exact: true }).click();
    await folderDialog.getByRole('textbox', { name: 'Remote path', exact: true }).fill(cwd);
    await folderDialog.getByRole('button', { name: 'Go', exact: true }).click();
    await folderDialog.getByRole('button', { name: 'Choose folder', exact: true }).click();
    await expect(folderDialog).toHaveCount(0);
    await expect(first).toHaveValue('$global-beta ');
    await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveAttribute('title', cwd);
    await first.fill('');
    await catalogAcceptance(first, 'host.skills.complete', 'new-chat');
    await first.fill('/global-beta');
    await expect(options).toHaveCount(1);
    await expect(options.first()).toContainText('/global-beta');
    checks.push('real folder selection preserves inserted draft and transitions globals to project-plus-global catalog using legacy cwd request shape');
    await open(first, '/');
    await expect(list).toBeVisible();
    assert.equal(await first.getAttribute('aria-haspopup'), 'listbox');
    await open(first);
    await expect(options.first()).toContainText('/accept-alpha');
    const hostRequest = frames.filter(frame => frame.method === 'host.skills.complete').at(-1);
    assert.deepEqual(hostRequest.params, { cwd, definition: 'coding', permission_mode: 'prompt', prefix: '', limit: 1024 });
    assert.equal(frames.some(frame => frame.method === 'workspace.complete'), false);
    await first.press('Enter');
    await expect(first).toHaveValue('$accept-alpha ');
    await expect(list).toHaveCount(0);
    await expect(first).toBeFocused();
    assert.deepEqual(sends(), []);
    assert.equal(frames.some(frame => frame.method === 'root.snapshot'), false);
    assert.deepEqual(await fixture.effects(), effectsBefore);
    checks.push('new-session / lists and prefix filters on selected host/CWD/definition/permission; Enter inserts canonical reference without create/send/root hydration');

    await expect(page.getByRole('button', { name: 'Send first message', exact: true })).toBeDisabled();
    assert.equal((await client.providers.list()).selection.ready, false);
    await screenshot('new-session-no-provider');
    checks.push('browser completion remains usable with no ready model/provider; send remains disabled');

    // A fresh folderless tab is the supported way back to globals in the product.
    const globalCallsBefore = frames.filter(frame => frame.method === 'host.skills.complete' && frame.params.scope === 'global').length;
    await page.getByRole('button', { name: 'Pane 1 actions', exact: true }).click();
    await page.getByRole('menuitem', { name: 'New session', exact: true }).click();
    await expect(first).toHaveValue('');
    await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveText('Choose folder');
    await first.focus();
    await eventually(() => frames.filter(frame => frame.method === 'host.skills.complete' && frame.params.scope === 'global').length > globalCallsBefore);
    assert.equal(frames.filter(frame => frame.method === 'host.skills.complete' && frame.params.scope === 'global').length, globalCallsBefore + 1);
    const freshGlobal = frames.filter(frame => frame.method === 'host.skills.complete').at(-1);
    assert.deepEqual(freshGlobal.params, globalRequest.params);
    await first.fill('/accept-');
    await eventually(() => responses.some(response => response.id === freshGlobal.id));
    await expect(page.getByText('No matching skills.', { exact: true })).toBeVisible();
    await expect(options).toHaveCount(0);
    await first.fill('/global-alpha');
    await expect(options).toHaveCount(1);
    await expect(options.first()).toContainText('/global-alpha');
    await screenshot('fresh-folderless-draft');
    await page.locator(`[role="tab"][id="whip-workspace-tab-${projectDraft}"]`).click();
    await expect(first).toHaveValue('$accept-alpha ');
    await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveAttribute('title', cwd);
    assert.deepEqual(sends(), []);
    assert.deepEqual(await fixture.effects(), effectsBefore);
    checks.push('fresh folderless draft after project selection preloads globals without stale project rows; original project draft/reference remains intact');

    // Only now configure a no-auth loopback catalog for the deliberate send.
    // TestV2SDKBridge still runs the turn with its synthetic runner, not a model.
    providerServer = createServer((request, response) => {
      providerRequests.push(`${request.method} ${request.url}`);
      response.writeHead(request.method === 'GET' ? 200 : 500, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ object: 'list', data: [{ id: 'fixture-model', object: 'model' }] }));
    });
    await new Promise(resolve => providerServer.listen(0, '127.0.0.1', resolve));
    const configuration = await client.configuration.get();
    const configured = await client.providers.create({ revision: configuration.revision, provider: 'fixture-local', definition: { name: 'Fixture local', base_url: `http://127.0.0.1:${providerServer.address().port}/v1`, api: 'openai-completions' }, credential: { mode: 'none' }, manual_model: { alias: 'fixture-model', id: 'fixture-model' } });
    await client.configuration.update({ revision: configured.revision, default_provider: 'fixture-local', default_model: 'fixture-model' });
    await page.reload();
    await expect(first).toHaveValue('$accept-alpha ');
    // The next, deliberate send retains the normal create-then-submit flow.
    await first.fill('$accept-alpha Synthetic first message');
    await page.getByRole('button', { name: 'Send first message', exact: true }).click();
    await expect(input).toBeEnabled();
    await eventually(() => commands('submit').length === 1);
    assert.equal(commands('session.create').length, 1);
    const root = decodeURIComponent(new URL(page.url()).pathname.split('/').at(-1));
    assert.match(new URL(page.url()).pathname, /\/s\//);
    assert.equal(commands('submit')[0].params.payload.text, '$accept-alpha Synthetic first message');
    await eventually(async () => !(await client.session(root).snapshot()).active_turns[root]);
    checks.push('only explicit first send creates one root and submits once through existing create flow');
    const sendCount = sends().length;

    await catalogAcceptance(input, 'workspace.complete', 'existing-session');
    await open(input);
    await expect(options.first()).toHaveAttribute('aria-selected', 'true');
    await input.press('ArrowDown');
    await expect(options.nth(1)).toHaveAttribute('aria-selected', 'true');
    await input.press('ArrowUp');
    await expect(options.first()).toHaveAttribute('aria-selected', 'true');
    await input.press('Enter');
    await expect(input).toHaveValue('$accept-alpha ');
    await expect(list).toHaveCount(0);
    await input.dispatchEvent('keydown', { key: 'Enter', code: 'Enter', repeat: true, bubbles: true });
    await expect(input).toHaveValue('$accept-alpha ');
    assert.equal(sends().length, sendCount, 'Held Enter after selection must not submit');
    const workspaceRequest = frames.filter(frame => frame.method === 'workspace.complete').at(-1);
    assert.deepEqual(workspaceRequest.params, { root_id: root, agent_id: root, kind: 'skill', prefix: '', limit: 1024 });
    await open(input);
    await options.nth(1).click();
    await expect(input).toHaveValue('$accept-alpine ');
    await expect(input).toBeFocused();
    await open(input);
    await input.press('Escape');
    await expect(list).toHaveCount(0);
    await expect(input).toHaveValue('/accept-al');
    await expect(input).toBeFocused();
    checks.push('existing-session scope uses root/agent; Up/Down/Enter/click select and Escape preserves draft/focus');

    await input.fill('Before /accept-al after');
    await input.evaluate(element => element.setSelectionRange(18, 18));
    await input.press('ArrowLeft'); // Native selection event: caret at the slash token end.
    await expect(options).toHaveCount(2);
    await input.press('Enter');
    await expect(input).toHaveValue('Before $accept-alpha after');
    await expect.poll(() => input.evaluate(element => element.selectionStart)).toBe('Before $accept-alpha '.length);
    checks.push('caret-middle replacement preserves surrounding draft and restores insertion caret');

    for (const theme of ['light', 'dark']) {
      await page.evaluate(theme => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: theme })), theme);
      await page.reload();
      await expect(input).toBeEnabled();
      await open(input, '/accept-long-');
      await screenshot(theme);
    }
    const reading = page.getByRole('region', { name: 'Conversation', exact: true });
    const before = await reading.evaluate(element => ({ top: element.scrollTop, height: element.clientHeight }));
    for (let i = 0; i < 24; i++) await input.press('ArrowDown');
    await expect.poll(() => list.evaluate(element => element.scrollTop)).toBeGreaterThan(0);
    const after = await reading.evaluate(element => ({ top: element.scrollTop, height: element.clientHeight }));
    assert.deepEqual(after, before, 'Scrolling active suggestion must not scroll/resize conversation');
    checks.push('bounded 32-item long list scrolls independently of conversation');
    await page.setViewportSize({ width: 390, height: 844 });
    await input.press('Escape');
    await open(input);
    await expect(list).toBeInViewport();
    const bounds = await list.boundingBox();
    assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= 391, 'Narrow popup stays within viewport');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await screenshot('narrow');
    await input.press('Escape');
    assert.equal(sends().length, sendCount, 'Picker interactions never submit');
    assert.ok(providerRequests.every(request => request.startsWith('GET ')), 'No model/provider inference calls');
    assert.deepEqual(errors, []);
    checks.push('light/dark/narrow screenshots; no document overflow, page errors or model/provider inference requests');
    report.status = 'passed';
    console.log(`${name}: ${checks.length} slash skill checks passed`);
  } catch (error) {
    report.status = 'failed'; report.failure = error.stack;
    report.focusAtFailure = await page?.evaluate(() => ({ active: { tag: document.activeElement?.tagName, id: document.activeElement?.id, label: document.activeElement?.getAttribute('aria-label') }, original: { id: window.__slashAcceptanceFocus?.id, connected: window.__slashAcceptanceFocus?.element.isConnected, active: window.__slashAcceptanceFocus?.element === document.activeElement }, textareas: [...document.querySelectorAll('textarea')].map(element => element.outerHTML) })).catch(() => null);
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}
${await page?.locator('body').innerText().catch(() => '')}`);
    process.exitCode = 1;
    console.error(error);
  } finally {
    for (const timer of delays) clearTimeout(timer);
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify({ requests: frames, responses }, null, 2));
    client?.close();
    await browser?.close();
    await fixture?.close();
    if (providerServer) await new Promise(resolve => providerServer.close(resolve));
    await rm(home, { recursive: true, force: true });
    await writeFile(join(directory, 'report.json'), JSON.stringify(reports, null, 2));
  }
  // Inspect any concrete failure before retrying, including shared fixture setup.
  if (report.status === 'failed') break;
}
assert.ok(reports.some(report => report.status), 'No installed browsers; no acceptance coverage ran');
console.log(JSON.stringify(reports, null, 2));
