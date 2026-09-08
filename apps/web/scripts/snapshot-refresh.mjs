import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
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
      const completedTool = page.locator('[data-message-id="live-tool:tool-a"]');
      await eventually(async () => (await completedTool.count()) === 1
        && !(await completedTool.locator('summary').innerText()).includes('in progress'), { description: 'tool completion observed during the held turn' });
      const earlyText = page.locator('[data-message-id^="live:"]');
      const earlyId = await earlyText.getAttribute('data-message-id');
      const earlyBody = await earlyText.innerText();
      assert.ok(earlyBody.includes(prompt));
      const suffix = await eventually(async () => {
        const current = await session.snapshot();
        return current.omitted?.presentation_prefix && current.presentation?.length === 129
          && current.presentation.every(item => item.kind === 'stream.usage') && current;
      }, { description: 'snapshot window excludes all earlier text and tool events' });
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
      assert.equal(await page.locator('[data-message-id^="live-tool:"]').count(), 2);
      assert.doesNotMatch(await completedTool.locator('summary').innerText(), /in progress/);
      await completedTool.locator('summary').click();
      assert.match(await completedTool.innerText(), /completed before snapshot/);
      await page.screenshot({ path: join(resultsDirectory, `${name}.png`), fullPage: true });
      await fixture.release(`tool-stream-refresh-${name}`);
      assert.equal((await turn.result({ signal: AbortSignal.timeout(15_000) })).status, 'succeeded');
      assert.equal((await queued.result({ signal: AbortSignal.timeout(15_000) })).status, 'succeeded');
      await eventually(async () => (await page.locator('[data-message-id^="live-tool:"]').count()) === 0, { description: 'completion hands presentation over to history' });
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
