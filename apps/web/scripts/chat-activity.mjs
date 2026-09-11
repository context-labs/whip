import assert from 'node:assert/strict';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { createHash } from 'node:crypto';
import { _electron, chromium, firefox, expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Production renderer, real SDK subscription and durable fixed event fixtures.
// No live provider, user daemon, credentials, editor or displayed command is used.
process.env.WHIP_WEB_REPL_FIXTURE = '1';
const directory = process.env.WHIP_CHAT_ACTIVITY_RESULTS ?? '/tmp/whip-chat-activity-results';
const repository = fileURLToPath(new URL('../../../', import.meta.url));
const exec = promisify(execFile);
await mkdir(directory, { recursive: true });
const report = [];

async function surface(name) {
  if (name !== 'electron') {
    const browser = await ({ chromium, firefox }[name]).launch();
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 }, recordVideo: { dir: directory, size: { width: 1280, height: 900 } } });
    return { page, close: () => browser.close() };
  }
  const temporary = await mkdtemp('/tmp/whip-chat-desktop-');
  for (const name of ['user', 'home', 'data', 'bin']) await mkdir(join(temporary, name), { mode: 0o700 });
  const binary = join(temporary, 'bin/whipcode');
  await copyFile(join(repository, 'apps/desktop/.stage/native/whipcode'), binary);
  const env = { ...process.env, HOME: join(temporary, 'user'), WHIP_DESKTOP_FIXTURE: '1',
    WHIPCODE_HOME: join(temporary, 'home'), WHIPCODE_NETWORK: '0',
    WHIP_DESKTOP_USER_DATA: join(temporary, 'data'), WHIP_DESKTOP_EXECUTABLE: binary };
  const app = await _electron.launch({ args: [join(repository, 'apps/desktop/.stage/app')], env, timeout: 30_000 });
  return { page: await app.firstWindow(), app, close: async () => {
    await app.close();
    // Keep the canonical fixture executable if its daemon cannot be stopped.
    await exec(binary, ['daemon', 'stop'], { env, timeout: 15_000 });
    await rm(temporary, { recursive: true, force: true });
  } };
}

for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  console.log(`${name}: starting chat activity fixture`);
  const fixture = await startFixture(name === 'electron' ? { allowedOrigins: ['whip-app://bundle'] } : {});
  const browser = await surface(name);
  const { page } = browser;
  const manifest = JSON.parse(await readFile(join(repository, name === 'electron' ? 'apps/desktop/.stage/app/renderer-manifest.json' : 'apps/web/renderer-manifest.json'), 'utf8'));
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `activity-${crypto.randomUUID()}`, clientKind: 'human' });
  const frames = [], errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('websocket', socket => socket.on('framesent', ({ payload }) => { try { frames.push(JSON.parse(String(payload))); } catch {} }));
  await page.addInitScript(() => {
    if (!localStorage.getItem('whip.appearance.theme.v1')) localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' }));
    window.cspErrors = [];
    document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective));
  });
  const root = fixture.info.root_id;
  const origin = name === 'electron' ? 'whip-app://bundle' : fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const url = `${origin}/h/${fixture.info.runtime_id}/s/${root}`;
  const step = async value => {
    const response = await fetch(`${fixture.info.frontend}/control/repl/activity-${value}`, { method: 'POST' });
    assert.equal(response.status, 204, await response.text());
  };
  const dock = page.locator('[data-current-activity]');
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  const group = page.locator('[data-activity-group]').filter({ hasText: '3 executions' });
  const screenshot = label => page.screenshot({ path: join(directory, `${name}-${label}.png`) });
  try {
    // Verify the fixture's Go embed and staged Electron use the same renderer.
    const embedded = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
    for (const [path, file] of Object.entries(manifest.files)) {
      const response = await fetch(`${embedded}/${path}`);
      assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256, `Embedded renderer differs: ${path}`);
    }
    await client.connect();
    if (name === 'electron') {
      await page.getByRole('button', { name: 'Manage servers', exact: true }).click();
      await page.getByRole('button', { name: 'Add server', exact: true }).click();
      const hosts = page.getByRole('dialog', { name: 'Add server', exact: true });
      await hosts.getByRole('textbox', { name: /Server name/ }).fill('Chat activity fixture');
      await hosts.getByRole('textbox', { name: 'Server address', exact: true }).fill(fixture.info.endpoint);
      await hosts.getByRole('button', { name: 'Add server', exact: true }).click();
      await hosts.waitFor({ state: 'hidden' });
    }
    await page.goto(url);
    await page.getByRole('textbox', { name: 'Message WHIP', exact: true }).waitFor();
    if (browser.app) {
      // Verify the actual staged renderer's native drag hit regions.
      const chrome = await page.locator('[data-workspace-tab-strip]').evaluate(strip => ({
        height: strip.parentElement.getBoundingClientRect().height,
        drag: getComputedStyle(strip.parentElement).getPropertyValue('-webkit-app-region'),
        tab: getComputedStyle(strip.querySelector('[data-workspace-tab]')).getPropertyValue('-webkit-app-region'),
      }));
      assert.equal(chrome.height, 48); assert.equal(chrome.drag, 'drag'); assert.equal(chrome.tab, 'no-drag');
      await writeFile(join(directory, 'electron-session-chrome.json'), JSON.stringify(chrome, null, 2));
    }
    const work = client.session(root).submit({ text: 'hold:chat-activity' });
    await work.accepted();
    await eventually(async () => (await client.session(root).snapshot()).active_turns[root]);
    await step('start');
    await expect(dock.getByRole('status')).toHaveText('Reading files');
    const latest = page.getByRole('button', { name: 'Latest', exact: true });
    if (await latest.count()) await latest.click();
    await expect(group).toHaveCount(1);
    await expect(group.locator('[data-tool-preview]')).toBeVisible();
    const groupId = await group.getAttribute('data-activity-group');
    await expect(dock.locator('[data-activity-animation]')).toHaveAttribute('data-activity-animation', 'running');
    await screenshot('running');
    if (name === 'chromium') {
      const cdp = await page.context().newCDPSession(page);
      await cdp.send('Tracing.start', { categories: 'devtools.timeline', transferMode: 'ReturnAsStream' });
      const animation = await dock.locator('[data-activity-animation] > span').first().evaluate(async dot => {
        const first = Number(getComputedStyle(dot).opacity);
        await new Promise(resolve => setTimeout(resolve, 400));
        return { first, second: Number(getComputedStyle(dot).opacity) };
      });
      assert.notEqual(animation.first, animation.second, 'The visible indicator must actually animate');
      const finished = new Promise(resolve => cdp.once('Tracing.tracingComplete', resolve));
      await cdp.send('Tracing.end');
      const { stream } = await finished;
      let trace = '';
      for (;;) { const part = await cdp.send('IO.read', { handle: stream }); trace += part.data; if (part.eof) break; }
      await cdp.send('IO.close', { handle: stream });
      await writeFile(join(directory, 'animation-trace.json'), trace);
      const layoutCount = JSON.parse(trace).traceEvents.filter(event => event.name === 'Layout').length;
      assert.ok(layoutCount <= 2, `Opacity animation caused ${layoutCount} layouts in 400ms`);
      await writeFile(join(directory, 'animation-observation.json'), JSON.stringify({ ...animation, layoutCount, intervalMs: 400 }, null, 2));
      await cdp.detach();
    }
    const snapshots = frames.filter(frame => frame.method === 'root.snapshot').length;
    await step('next');
    await expect(dock.getByRole('status')).toHaveText('Waiting for agents');
    assert.equal(await group.getAttribute('data-activity-group'), groupId);
    assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, snapshots, 'Host phases must not add snapshot reads');
    await group.locator('summary').focus();
    await page.keyboard.press('Enter');
    await expect(group.locator('details')).toHaveAttribute('open', '');
    await expect(group.getByText('agents.wait', { exact: true })).toBeVisible();
    await screenshot('expanded');
    await group.getByRole('button', { name: 'Open in REPL' }).click();
    await expect(page.getByRole('region', { name: 'REPL executions', exact: true })).toBeVisible();
    const notebook = page.locator('[data-repl-cell]').filter({ hasText: 'agents.wait' });
    await expect(notebook).toContainText('Running');
    // Opening execution evidence preserves the source chat and pushes history.
    await page.goBack();
    await expect(dock.getByRole('status')).toHaveText('Waiting for agents');
    await expect(group).toHaveCount(1);
    await dock.getByRole('button', { name: 'Pause activity animation' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-motion', 'reduce');
    await expect(dock.locator('[data-activity-animation]')).toHaveAttribute('data-activity-animation', 'static');
    for (const width of [390, 320]) {
      await page.setViewportSize({ width, height: 844 });
      await expect(dock).toBeInViewport();
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await screenshot(`narrow-${width}`);
    }
    for (const appearance of [
      { theme: 'light', uiSize: 20, codeSize: 24, label: 'light-large-type' },
      { theme: 'claude-code', uiSize: 12, codeSize: 10, label: 'dark-small-type' },
    ]) {
      await page.evaluate(value => {
        localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: value.theme }));
        localStorage.setItem('whip.appearance.display.v1', JSON.stringify({ version: 1, display: {
          uiFont: 'system', codeFont: 'system', uiSize: value.uiSize, codeSize: value.codeSize,
          wrapCode: true, contrast: 'more', motion: 'reduce',
        } }));
      }, appearance);
      await page.reload();
      await expect(dock.getByRole('status')).toHaveText('Waiting for agents');
      // Reload restores a reading bookmark. Explicitly scroll to the live group
      // before checking its typography; font reflow need not follow the tail.
      await expect.poll(async () => {
        await reading.evaluate(element => { element.scrollTop = element.scrollHeight; });
        return group.locator('[data-tool-preview]').isVisible();
      }).toBe(true);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), appearance.label);
      await screenshot(appearance.label);
    }
    if (browser.app) {
      await page.setViewportSize({ width: 1280, height: 960 });
      const cdp = await page.context().newCDPSession(page);
      await cdp.send('Emulation.clearDeviceMetricsOverride');
      await browser.app.evaluate(({ BrowserWindow }) => {
        const window = BrowserWindow.getAllWindows()[0];
        window.setSize(1280, 960); window.webContents.setZoomFactor(4);
      });
      await expect.poll(() => page.evaluate(() => innerWidth)).toBeLessThanOrEqual(320);
      const geometry = await page.evaluate(() => ({ width: innerWidth, height: innerHeight, scrollWidth: document.documentElement.scrollWidth }));
      await writeFile(join(directory, 'electron-zoom.json'), JSON.stringify(geometry, null, 2));
      assert.ok(geometry.width >= 300 && geometry.scrollWidth <= geometry.width, `Desktop 400% zoom overflow: ${JSON.stringify(geometry)}`);
      await expect(dock).toBeInViewport();
      await screenshot('zoom-400');
      await browser.app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.setZoomFactor(1));
      await cdp.detach();
    }
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.evaluate(() => {
      const node = document.querySelector('[data-activity-group] [data-tool-preview]');
      if (node) { const range = document.createRange(); range.selectNodeContents(node); getSelection().removeAllRanges(); getSelection().addRange(range); }
    });
    const selected = await page.evaluate(() => getSelection().toString());
    await step('complete');
    await expect(dock.getByRole('status')).toHaveText('Writing a response');
    if (selected) assert.equal(await page.evaluate(() => getSelection().toString()), selected);
    await screenshot('complete');
    const response = await fetch(`${fixture.info.frontend}/control/release?key=chat-activity`, { method: 'POST' });
    assert.equal(response.status, 204);
    await work.result();
    // Normal scrolling must leave the activity dock available without extra subscriptions.
    await reading.evaluate(element => { element.scrollTop = 300; });
    await expect(page.getByRole('textbox', { name: 'Message WHIP', exact: true })).toBeInViewport();
    assert.deepEqual(errors, []);
    assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
    report.push({ browser: name, rendererDigest: manifest.digest, checks: ['identical embedded renderer', 'grouped executions', 'visible collapsed preview', 'accurate host phase', 'no phase polling', 'keyboard disclosure', 'shared REPL state', 'snapshot recovery', 'shared reduced motion', '390/320px bounds', 'light/dark themes with minimum/maximum system type', 'selection through completion', 'bounded production history', ...(browser.app ? ['staged main/preload', '400% native zoom'] : [])] });
    await writeFile(join(directory, 'results.json'), JSON.stringify(report, null, 2));
  } catch (error) {
    await screenshot('failure').catch(() => {});
    await writeFile(join(directory, `${name}-failure.json`), JSON.stringify(await reading.evaluate(element => ({
      scrollTop: element.scrollTop, scrollHeight: element.scrollHeight, clientHeight: element.clientHeight,
      groups: [...element.querySelectorAll('[data-activity-group]')].map(group => ({ id: group.getAttribute('data-activity-group'), text: group.textContent })),
    })).catch(() => ({})), null, 2));
    throw error;
  } finally {
    client.close();
    await browser.close();
    await fixture.close();
  }
}
await writeFile(join(directory, 'results.json'), JSON.stringify(report, null, 2));
console.log(JSON.stringify(report, null, 2));
