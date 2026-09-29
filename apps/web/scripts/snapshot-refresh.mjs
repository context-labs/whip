import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, expect, firefox } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

// Uses the packaged production app and an isolated native runtime and local HTTP provider.
// Run npm run pack:web first; this never attaches to the user's daemon.
const resultsDirectory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-web-snapshot-refresh';
await mkdir(resultsDirectory, { recursive: true });
const fixture = await startFixture();
const origin = fixture.info.web;
const results = {};
try {
  for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
    const browser = await { chromium, firefox }[name].launch({ headless: true });
    const page = await browser.newPage({ viewport: { width: 1360, height: 960 } });
    page.setDefaultTimeout(15_000);
    const client = await fixture.connect(`snapshot-refresh-${randomUUID()}`);
    const snapshots = [];
    const errors = [];
    let observing = false;
    page.on('pageerror', error => errors.push(error.message));
    page.on('websocket', socket => socket.on('framereceived', ({ payload }) => {
      const message = JSON.parse(String(payload));
      if (message.result?.snapshot) { observing = true; snapshots.push(message.result); }
    }));
    try {
      const { root } = await fixture.createRoot(client);
      const rootId = root.id;
      const session = client.session(rootId);
      await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${rootId}`);
      await eventually(() => observing, { description: 'browser observes native history before streaming starts' });
      const prompt = `hold:tool-stream-refresh-${name}`;
      const turn = session.submission([{ type: 'text', text: prompt }], randomUUID());
      await turn.send(deadline());
      const activity = page.locator('[data-activity-group]');
      await expect(activity).toHaveCount(1);
      await expect(activity.locator('[data-activity-content]')).toContainText('Called 1 tool · 1 execution');
      await expect(activity.locator('[data-activity-content]')).toHaveAttribute('aria-expanded', 'true');
      const steps = page.locator('[data-activity-step]');
      await expect(steps).toHaveCount(2);
      for (const step of await steps.all()) await step.locator('[data-activity-content]').click();
      const cells = page.locator('[data-activity-detail]');
      await expect(cells).toHaveCount(2);
      const groupId = await activity.getAttribute('data-activity-group');
      const cellIds = await cells.evaluateAll(elements => elements.map(element => element.getAttribute('data-activity-detail')));
      assert.equal(new Set(cellIds).size, 2);
      const completedTool = cells.first();
      await expect(steps.first().getByText('Done', { exact: true })).toBeVisible();
      await expect(completedTool.getByRole('region', { name: 'Output', exact: true })).toHaveText('completed before snapshot');
      await expect(cells.nth(1).getByRole('region', { name: 'Live output · provisional', exact: true })).toHaveText('first\nsecond');
      // Native model prose commits with its tool calls before the first cell. It
      // must stay one canonical row across bounded empty observation pages.
      const earlyText = page.locator('[data-message-role="assistant"][data-message-id]').filter({ hasText: prompt }).first();
      const earlyId = await earlyText.getAttribute('data-message-id');
      const earlyBody = await earlyText.innerText();
      assert.ok(earlyBody.includes(prompt));
      const suffix = await session.history.page({ direction: 'forward' }, deadline());
      const completed = suffix.messages.find(message => message.parts.some(part => part.type === 'tool_result'));
      assert.ok(completed, 'Completed cell evidence must already be canonical');
      const snapshotsBefore = snapshots.length;
      // Queued input triggers a lifecycle refresh without completing this turn.
      const queued = session.submission([{ type: 'text', text: 'Continue after snapshot refresh' }], randomUUID());
      await queued.send(deadline());
      await eventually(() => snapshots.slice(snapshotsBefore).some(current =>
        current.snapshot.session_id === rootId && current.snapshot.revision === suffix.snapshot.revision
          && BigInt(current.snapshot.through_sequence) >= BigInt(suffix.snapshot.through_sequence)
          && current.messages.length === 0),
      { description: 'browser receives a bounded refresh for the same active turn' });
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      assert.equal(await earlyText.getAttribute('data-message-id'), earlyId);
      assert.equal(await earlyText.innerText(), earlyBody);
      assert.equal(await activity.getAttribute('data-activity-group'), groupId);
      assert.deepEqual(await cells.evaluateAll(elements => elements.map(element => element.getAttribute('data-activity-detail'))), cellIds);
      await expect(steps.first().getByText('Done', { exact: true })).toBeVisible();
      await expect(completedTool.getByRole('region', { name: 'Output', exact: true })).toHaveText('completed before snapshot');
      await expect(cells.nth(1).getByRole('region', { name: 'Live output · provisional', exact: true })).toHaveText('first\nsecond');
      await page.screenshot({ path: join(resultsDirectory, `${name}.png`), fullPage: true });
      await fixture.release(`tool-stream-refresh-${name}`);
      assert.equal((await turn.wait(deadline())).turn.state, 'succeeded');
      assert.equal((await queued.wait(deadline())).turn.state, 'succeeded');
      assert.equal((await session.cells.output(deadline())).preview, null);
      assert.equal(await earlyText.getAttribute('data-message-id'), earlyId);
      // Native execute calls have their own committed model message, followed by
      // one completion answer and the queued answer. Never duplicate the earlier prose.
      // Ordered presentation slots keep attempt-scoped IDs through settlement.
      const assistantHistory = page.locator('[data-message-id][data-message-role="assistant"]');
      await expect(assistantHistory).toHaveCount(3);
      await expect(assistantHistory.filter({ hasText: prompt })).toHaveCount(1);
      await expect(assistantHistory.filter({ hasText: 'Continue after snapshot refresh' })).toHaveCount(1);
      assert.deepEqual(errors, []);
      results[name] = { passed: true, history_revision: suffix.snapshot.revision, empty_observation_pages: snapshots.filter(item => item.messages?.length === 0).length, preserved: ['earlier text', 'row identity', 'completed tool status', 'tool output'] };
    } catch (error) {
      results[name] = { passed: false, error: String(error), stack: error.stack, browserErrors: errors };
      await page.screenshot({ path: join(resultsDirectory, `${name}-failure.png`), fullPage: true }).catch(() => {});
    } finally {
      await browser.close();
    }
    console.log(name, results[name]);
  }
} finally {
  await writeFile(join(resultsDirectory, 'report.json'), JSON.stringify(results, null, 2) + '\n');
  await fixture.close();
}
if (Object.values(results).some(result => !result.passed)) process.exitCode = 1;
