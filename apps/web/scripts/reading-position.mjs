import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Run after npm run pack:web. All messages belong to an isolated test daemon.
const directory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-reading-position';
await mkdir(directory, { recursive: true });
const fixture = await startFixture();
const results = {};
try {
  for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
    const browser = await { chromium, firefox }[name].launch();
    const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: crypto.randomUUID(), clientKind: 'human' });
    const page = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true });
    const requests = [], replies = new Set(), errors = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('websocket', socket => {
      socket.on('framesent', ({ payload }) => requests.push(JSON.parse(String(payload))));
      socket.on('framereceived', ({ payload }) => {
        const message = JSON.parse(String(payload));
        if (message.id && !message.method) replies.add(message.id);
      });
    });
    const conversation = page.getByRole('region', { name: 'Conversation', exact: true });
    const frame = () => page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    const anchor = () => conversation.evaluate(element => {
      const viewport = element.getBoundingClientRect();
      const row = [...element.querySelectorAll('[data-reading-id]')].find(row => {
        const rect = row.getBoundingClientRect();
        return rect.bottom > viewport.top && rect.top < viewport.bottom;
      });
      return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - viewport.top } : null;
    });
    const stableAnchor = async () => {
      let previous, since = performance.now();
      return eventually(async () => {
        const row = await anchor();
        if (!row || row.id !== previous?.id || Math.abs(row.offset - previous.offset) >= 1) since = performance.now();
        previous = row;
        // Include the delayed touch-scroll measurement correction, which can
        // arrive after several otherwise stationary frames.
        return row && performance.now() - since >= 300 ? row : false;
      }, { description: 'stable visible reading anchor' });
    };
    const submit = text => session.submit({ text }).result({ signal: AbortSignal.timeout(15_000) });
    let session;
    try {
      await client.connect();
      const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
      const rootId = created.result.root_id;
      session = client.session(rootId);
      for (let index = 0; index < 21; index++) await submit(`Earlier short message ${index}`);
      for (let index = 0; index < 28; index++) await submit(`Message ${index}\n\n${'A readable paragraph for scrolling. '.repeat(18)}`);
      const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
      await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${rootId}`);
      await eventually(() => page.getByLabel('Message WHIP', { exact: true }).isEnabled());
      await page.evaluate(() => document.fonts.ready);
      for (const width of [320, 390]) {
        await page.setViewportSize({ width, height: 844 });
        await frame();
      }
      await conversation.evaluate(element => { element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
      await page.getByRole('button', { name: 'Latest', exact: true }).waitFor();
      const before = await stableAnchor();
      const requestsBefore = requests.length;
      await submit('New output must not steal the reading position.');
      await eventually(() => requests.slice(requestsBefore).some(item => item.method === 'root.snapshot' && replies.has(item.id)), { description: 'completed turn snapshot' });
      const after = await stableAnchor();
      results[name] = { browser: await browser.version(), before, after };
      assert.equal(after.id, before.id, 'Incoming output replaced the reading anchor');
      assert.ok(Math.abs(after.offset - before.offset) < 5, `Incoming output moved the anchor: ${JSON.stringify({ before, after })}`);
      assert.equal(requests.slice(requestsBefore).filter(item => item.method === 'history.page').length, 0, 'Incoming-output check also paginated');
      await page.getByRole('button', { name: 'Latest', exact: true }).click();
      await eventually(async () => conversation.evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'jump to latest' });
      const followingRequests = requests.length;
      await submit('Keep following new output at the bottom.');
      await eventually(() => requests.slice(followingRequests).some(item => item.method === 'root.snapshot' && replies.has(item.id)), { description: 'followed turn snapshot' });
      await stableAnchor();
      await eventually(async () => conversation.evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'follow new output at the bottom' });
      await conversation.evaluate(element => { element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
      const beforePage = await stableAnchor();
      const pageRequests = requests.length;
      // Activate the actual control without Playwright scrolling it to the top
      // first, which would also trigger automatic pagination.
      await page.getByRole('button', { name: 'Load earlier messages', exact: true }).evaluate(button => button.click());
      await eventually(() => requests.slice(pageRequests).some(item => item.method === 'history.page' && replies.has(item.id)), { description: 'older history page' });
      const afterPage = await stableAnchor();
      results[name].paging = { before: beforePage, after: afterPage };
      assert.equal(await page.getByRole('button', { name: 'Load earlier messages', exact: true }).count(), 0, 'The fixture must exhaust older history');
      assert.equal(afterPage.id, beforePage.id, 'Older history replaced the reading anchor');
      assert.ok(Math.abs(afterPage.offset - beforePage.offset) < 5, `Older history moved the anchor: ${JSON.stringify({ beforePage, afterPage })}`);
      assert.deepEqual(errors, []);
      await page.screenshot({ path: `${directory}/${name}-reading.png` });
      console.log(`${name}: incoming output and older history preserve the reading anchor; Latest and following pass`);
    } catch (error) {
      await page.screenshot({ path: `${directory}/${name}-failure.png` }).catch(() => {});
      throw error;
    } finally {
      await writeFile(`${directory}/results.json`, JSON.stringify(results, null, 2));
      client.close();
      await browser.close();
    }
  }
} finally {
  await fixture.close();
}
