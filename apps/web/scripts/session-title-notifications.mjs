import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, startFixture } from './native-fixture.mjs';

// Native catalog revisions replace the retired title-notification event. Keep
// the actual inactive-tab and unopened-sidebar guarantees without hydrating an
// unopened transcript or inventing a legacy subscription/capability.
const output = process.env.WHIP_TITLE_NOTIFICATION_RESULTS ?? '/tmp/whip-title-notifications-browser';
await mkdir(output, { recursive: true });
for (const engine of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  assert(['chromium', 'firefox'].includes(engine));
  let fixture, browser, page;
  const sent = [], errors = [];
  const recordError = error => { if (errors.length < 32) errors.push(String(error.stack ?? error).slice(0, 4096)); };
  try {
    fixture = await startFixture();
    const client = await fixture.connect(`titles-${crypto.randomUUID()}`);
    const active = await fixture.createRoot(client, { title: 'Active title' });
    const inactive = await fixture.createRoot(client, { title: 'Inactive old title' });
    const unopened = await fixture.createRoot(client, { title: 'Unopened old title' });
    browser = await ({ chromium, firefox }[engine]).launch();
    page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    page.setDefaultTimeout(15_000);
    page.on('pageerror', recordError);
    await page.exposeFunction('titleCSP', recordError);
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.titleCSP(event.violatedDirective); }));
    page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
      try {
        assert(Buffer.byteLength(payload) <= (8 << 20));
        const frame = JSON.parse(String(payload));
        assert(sent.length < 2048, 'Title probe frame bound exceeded');
        sent.push({ method: frame.method, session_id: frame.params?.session_id, root_ids: frame.params?.root_ids });
      } catch (error) { recordError(error); }
    }));
    const route = id => `${fixture.info.web}/h/${fixture.info.runtime_id}/s/${id}`;
    const row = id => page.locator(`[data-sidebar-session="${id}"]`);
    await page.goto(route(inactive.root.id));
    await expect(row(inactive.root.id)).toBeVisible();
    await row(active.root.id).getByRole('link').click();
    await expect(page).toHaveURL(route(active.root.id));
    await expect(page.getByRole('tab', { name: /Inactive old title/ }).first()).toBeVisible();
    await expect(row(unopened.root.id)).toContainText('Unopened old title');
    const prior = await client.trees.catalog(deadline());
    const started = performance.now();
    for (const [item, title] of [[inactive, 'Inactive new title'], [unopened, 'Unopened new title']]) {
      await client.trees.update(item.tree.id, item.tree.revision, { ...item.tree.metadata, title }, deadline());
    }
    await expect(page.getByRole('tab', { name: /Inactive new title/ }).first()).toBeVisible({ timeout: 3000 });
    await expect(row(inactive.root.id)).toContainText('Inactive new title', { timeout: 3000 });
    await expect(row(unopened.root.id)).toContainText('Unopened new title', { timeout: 3000 });
    const current = await client.trees.catalog(deadline());
    assert.notEqual(current.revision, prior.revision);
    assert(sent.some(frame => frame.method === 'trees.catalog'));
    assert(sent.some(frame => frame.method === 'trees.summaries'));
    assert(!sent.some(frame => ['sessions.history_page', 'sessions.history', 'sessions.observe', 'sessions.turns'].includes(frame.method) && frame.session_id === unopened.root.id), 'Unopened root was hydrated');
    assert(!sent.some(frame => ['events.subscribe', 'root.snapshot'].includes(frame.method)));
    assert.deepEqual(errors, []);
    await page.screenshot({ path: join(output, engine + '.png') });
    await writeFile(join(output, engine + '.json'), JSON.stringify({
      engine, browser: browser.version(), changedWithinMs: performance.now() - started,
      catalogRevisions: [prior.revision, current.revision], defaultNativePolling: true,
      unopenedRootNeverHydrated: true, inactiveTabUpdated: true, errors,
    }, null, 2));
    console.log(`${engine}: native catalog title updates inactive tab and unopened sidebar session`);
  } catch (error) {
    await page?.screenshot({ path: join(output, engine + '-failure.png') }).catch(() => {});
    await writeFile(join(output, engine + '-failure.json'), JSON.stringify({ error: String(error.stack ?? error), sent, errors }, null, 2));
    throw error;
  } finally {
    try { await browser?.close(); } finally { await fixture?.close(); }
  }
}
