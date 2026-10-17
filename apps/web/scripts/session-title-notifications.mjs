import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Real renderer + daemon/WebSocket, with two-second polling lengthened only in
// this fixture so passing cannot be attributed to the fallback sync path.
const output = process.env.WHIP_TITLE_NOTIFICATION_RESULTS ?? '/tmp/whip-title-notifications-browser';
await mkdir(output, { recursive: true });
for (const engine of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const fixture = await startFixture();
  const browser = await ({ chromium, firefox }[engine]).launch();
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: crypto.randomUUID(), clientKind: 'human' });
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const sent = [], received = [], errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('websocket', socket => {
    socket.on('framesent', ({ payload }) => { try { sent.push(JSON.parse(String(payload))); } catch {} });
    socket.on('framereceived', ({ payload }) => { try { received.push(JSON.parse(String(payload))); } catch {} });
  });
  await page.addInitScript(() => {
    const timeout = window.setTimeout.bind(window), interval = window.setInterval.bind(window);
    window.setTimeout = (handler, delay, ...args) => timeout(handler, delay === 2000 ? 60000 : delay, ...args);
    window.setInterval = (handler, delay, ...args) => interval(handler, delay === 2000 ? 60000 : delay, ...args);
  });
  const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const route = id => origin + '/h/' + fixture.info.runtime_id + '/s/' + id;
  const row = id => page.locator('[data-sidebar-session="' + id + '"]');
  try {
    await client.connect();
    assert(client.getSnapshot().info.negotiated_capabilities.includes('session_title_notifications'));
    const active = fixture.info.root_id;
    const roots = [];
    for (const title of ['Inactive old title', 'Unopened old title']) {
      const result = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
      assert.equal(result.status, 'succeeded');
      roots.push(result.result.root_id);
      assert.equal((await client.session(roots.at(-1)).rename(title).result()).status, 'succeeded');
    }
    const [inactive, unopened] = roots;
    await page.goto(route(inactive));
    await expect(row(inactive)).toBeVisible({ timeout: 15000 });
    await row(active).getByRole('link').click();
    await expect(page).toHaveURL(route(active));
    await expect(page.getByRole('tab', { name: /Inactive old title/ }).first()).toBeVisible();
    await expect(row(unopened)).toContainText('Unopened old title');
    received.length = 0;
    const started = performance.now();
    assert.equal((await client.session(inactive).rename('Inactive new title').result()).status, 'succeeded');
    assert.equal((await client.session(unopened).rename('Unopened new title').result()).status, 'succeeded');
    await expect(page.getByRole('tab', { name: /Inactive new title/ }).first()).toBeVisible({ timeout: 3000 });
    await expect(row(inactive)).toContainText('Inactive new title', { timeout: 3000 });
    await expect(row(unopened)).toContainText('Unopened new title', { timeout: 3000 });
    assert(received.some(frame => frame.method === 'sessions.title.changed' && frame.params.root_id === inactive));
    assert(received.some(frame => frame.method === 'sessions.title.changed' && frame.params.root_id === unopened));
    // The formerly active view may remain leased for 30 seconds; existing root
    // events can still refresh it. The never-opened session is the strict proof
    // that a host notification needs no root hydration.
    assert(!sent.some(frame => ['events.subscribe', 'root.snapshot'].includes(frame.method) && frame.params.root_id === unopened), 'Unopened root was hydrated');
    assert.deepEqual(errors, []);
    await page.screenshot({ path: join(output, engine + '.png') });
    await writeFile(join(output, engine + '.json'), JSON.stringify({
      engine, changedWithinMs: performance.now() - started, pollingIntervalInFixtureMs: 60000,
      notificationRoots: received.filter(frame => frame.method === 'sessions.title.changed').map(frame => frame.params.root_id),
      unopenedRootNeverHydrated: true, inactiveTabUpdated: true, errors,
    }, null, 2));
    console.log(engine + ': title notification updates inactive tab and unopened sidebar session');
  } catch (error) {
    await writeFile(join(output, engine + "-failure.json"), JSON.stringify({ sent, received, errors }, null, 2));
    throw error;
  } finally {
    client.close();
    await browser.close();
    await fixture.close();
  }
}
