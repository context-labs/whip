import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, expect, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Uses the packaged production app and an isolated fake-provider daemon.
// Run npm run pack:web first; this never attaches to the user's daemon.
const resultsDirectory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-web-snapshot-refresh';
await mkdir(resultsDirectory, { recursive: true });
const fixture = await startFixture();
const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
const results = {};
try {
  for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
    const browser = await { chromium, firefox }[name].launch({ headless: true });
    const page = await browser.newPage({ viewport: { width: 1360, height: 960 } });
    page.setDefaultTimeout(15_000);
    const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `snapshot-refresh-${crypto.randomUUID()}` });
    const snapshots = [];
    const errors = [];
    let subscribed = false;
    page.on('pageerror', error => errors.push(error.message));
    page.on('websocket', socket => socket.on('framereceived', ({ payload }) => {
      const message = JSON.parse(String(payload));
      if (message.result?.subscription_id) subscribed = true;
      if (message.result?.root_id && message.result?.cursor) snapshots.push(message.result);
    }));
    try {
      await client.connect();
      const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
      assert.equal(created.status, 'succeeded');
      const rootId = created.result.root_id;
      const session = client.session(rootId);
      await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${rootId}`);
      await eventually(() => subscribed, { description: 'browser subscribes before streaming starts' });
      const prompt = `hold:tool-stream-refresh-${name}`;
      const turn = session.submit({ text: prompt });
      await turn.accepted();
      const activity = page.locator('[data-activity-group]');
      await expect(activity).toHaveCount(1);
      await expect(activity.locator(':scope > details > summary')).toContainText('2 executions');
      await activity.locator(':scope > details > summary').click();
      const cells = activity.locator('[data-activity-cell]');
      await expect(cells).toHaveCount(2);
      const groupId = await activity.getAttribute('data-activity-group');
      const cellIds = await cells.evaluateAll(elements => elements.map(element => element.getAttribute('data-activity-cell')));
      assert.equal(new Set(cellIds).size, 2);
      const completedTool = cells.first();
      await expect(completedTool.getByText('Completed', { exact: true })).toBeVisible();
      await expect(completedTool.locator(':scope > pre')).toHaveText('completed before snapshot');
      await expect(cells.nth(1).locator(':scope > pre')).toHaveText('first\nsecond');
      const earlyText = page.locator('[data-message-id^="live:"]');
      const earlyId = await earlyText.getAttribute('data-message-id');
      const earlyBody = await earlyText.innerText();
      assert.ok(earlyBody.includes(prompt));
      const suffix = await eventually(async () => {
        const current = await session.snapshot();
        return current.omitted?.presentation_prefix && current.presentation?.length === 129
          && current.presentation.every(item => ['stream.tool.call', 'stream.tool.output'].includes(item.kind)
            && item.payload.id === 'tool-b') && current;
      }, { description: 'snapshot window excludes earlier text and the completed tool' });
      const snapshotsBefore = snapshots.length;
      // Queued input triggers a lifecycle refresh without completing this turn.
      const queued = session.submit({ text: 'Continue after snapshot refresh' });
      await queued.accepted();
      await eventually(() => snapshots.slice(snapshotsBefore).some(current =>
        current.active_turns[rootId] === suffix.active_turns[rootId] && BigInt(current.cursor) >= BigInt(suffix.cursor)),
      { description: 'browser receives a bounded refresh for the same active turn' });
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      assert.equal(await earlyText.getAttribute('data-message-id'), earlyId);
      assert.equal(await earlyText.innerText(), earlyBody);
      assert.equal(await activity.getAttribute('data-activity-group'), groupId);
      assert.deepEqual(await cells.evaluateAll(elements => elements.map(element => element.getAttribute('data-activity-cell'))), cellIds);
      await expect(completedTool.getByText('Completed', { exact: true })).toBeVisible();
      await expect(completedTool.locator(':scope > pre')).toHaveText('completed before snapshot');
      await expect(cells.nth(1).locator(':scope > pre')).toHaveText('first\nsecond');
      await page.screenshot({ path: join(resultsDirectory, `${name}.png`), fullPage: true });
      await fixture.release(`tool-stream-refresh-${name}`);
      assert.equal((await turn.result({ signal: AbortSignal.timeout(15_000) })).status, 'succeeded');
      assert.equal((await queued.result({ signal: AbortSignal.timeout(15_000) })).status, 'succeeded');
      await expect(earlyText).toHaveCount(0);
      // Execution evidence remains available after the turn, while streamed prose
      // is replaced by committed history without a duplicate assistant message.
      const assistantHistory = page.locator('[data-message-id^="h:"][data-message-role="assistant"]');
      await expect(assistantHistory).toHaveCount(2);
      await expect(assistantHistory.filter({ hasText: prompt })).toHaveCount(1);
      await expect(assistantHistory.filter({ hasText: 'Continue after snapshot refresh' })).toHaveCount(1);
      assert.deepEqual(errors, []);
      results[name] = { passed: true, snapshot_events: suffix.presentation.length, preserved: ['earlier text', 'row identity', 'completed tool status', 'tool output'] };
    } catch (error) {
      results[name] = { passed: false, error: String(error), stack: error.stack, browserErrors: errors };
      await page.screenshot({ path: join(resultsDirectory, `${name}-failure.png`), fullPage: true }).catch(() => {});
    } finally {
      client.close();
      await browser.close();
    }
    console.log(name, results[name]);
  }
} finally {
  await writeFile(join(resultsDirectory, 'report.json'), JSON.stringify(results, null, 2) + '\n');
  await fixture.close();
}
if (Object.values(results).some(result => !result.passed)) process.exitCode = 1;
