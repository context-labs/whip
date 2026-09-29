import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { chromium, firefox } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

// Run after npm run pack:web. All messages belong to an isolated test daemon.
const directory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-reading-position';
await mkdir(directory, { recursive: true });
const fixture = await startFixture({ lifetimeMs: 900000 });
const results = {};
try {
  for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
    assert(['chromium', 'firefox'].includes(name));
    let browser, page;
    const requests = [], errors = [], csp = [];
    try {
      browser = await { chromium, firefox }[name].launch();
      const client = await fixture.connect(`reading-${name}`);
      page = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true });
      page.setDefaultTimeout(15000);
      page.on('pageerror', error => { if (errors.length < 64) errors.push(error.message); });
      await page.exposeFunction('__recordReadingCSP', directive => { if (csp.length < 64) csp.push(String(directive).slice(0, 256)); });
      await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__recordReadingCSP(event.violatedDirective).catch(() => {}); }));
      page.on('websocket', socket => {
        const pending = new Map();
        socket.on('framesent', ({ payload }) => {
          try {
            const request = JSON.parse(String(payload));
            assert(requests.length < 25000, 'Reading traffic evidence bound exceeded');
            const item = { id: request.id, method: request.method, session: request.params?.session_id, cursor: request.params?.cursor };
            requests.push(item); pending.set(request.id, item);
          } catch (error) { if (errors.length < 64) errors.push(error.message); }
        });
        socket.on('framereceived', ({ payload }) => {
          try {
            const message = JSON.parse(String(payload)), request = pending.get(message.id);
            if (request) {
              request.replied = true; request.error = message.error?.kind;
              request.lastSequence = message.result?.messages?.at(-1)?.sequence;
              request.nextCursor = message.result?.next_cursor; pending.delete(message.id);
            }
          } catch (error) { if (errors.length < 64) errors.push(error.message); }
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
      const submit = async text => {
        const command = session.submission([{ type: 'text', text }], randomUUID());
        await command.send(deadline()); assert.equal((await command.wait(deadline())).turn.state, 'succeeded');
        return (await session.history.snapshot(deadline())).through_sequence;
      };
      let session;
      const created = await fixture.createRoot(client);
      const rootId = created.root.id;
      session = client.session(rootId);
      // Preserve all 128 actual turns. Native history seeds one 100-record
      // window; two explicit older pages exhaust this canonical transcript.
      for (let index = 0; index < 100; index++) await submit(`Earlier short message ${index}`);
      for (let index = 0; index < 28; index++) await submit(`Message ${index}\n\n${'A readable paragraph for scrolling. '.repeat(18)}`);
      const origin = fixture.info.web;
      await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${rootId}`);
      await eventually(() => page.getByLabel('Message WHIP', { exact: true }).isEnabled());
      await page.evaluate(() => document.fonts.ready);
      for (const width of [320, 390]) {
        await page.setViewportSize({ width, height: 844 });
        await frame();
      }
      await conversation.evaluate(element => { element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 })); element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
      await page.getByRole('button', { name: 'Latest', exact: true }).waitFor();
      const before = await stableAnchor();
      const requestsBefore = requests.length;
      const received = await submit('New output must not steal the reading position.');
      await eventually(() => requests.slice(requestsBefore).some(item => item.method === 'sessions.observe' && item.session === rootId && item.lastSequence === received), { description: 'exact completed turn observed in native history' });
      const after = await stableAnchor();
      results[name] = { browser: await browser.version(), before, after };
      assert.equal(after.id, before.id, 'Incoming output replaced the reading anchor');
      assert.ok(Math.abs(after.offset - before.offset) < 5, `Incoming output moved the anchor: ${JSON.stringify({ before, after })}`);
      assert.equal(requests.slice(requestsBefore).filter(item => item.method === 'sessions.history_page').length, 0, 'Incoming-output check also paginated');
      await page.getByRole('button', { name: 'Latest', exact: true }).click();
      await eventually(async () => conversation.evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'jump to latest' });
      const followingRequests = requests.length;
      const followed = await submit('Keep following new output at the bottom.');
      await eventually(() => requests.slice(followingRequests).some(item => item.method === 'sessions.observe' && item.session === rootId && item.lastSequence === followed), { description: 'exact followed native turn observed' });
      await stableAnchor();
      await eventually(async () => conversation.evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight < 64), { description: 'follow new output at the bottom' });
      await conversation.evaluate(element => { element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 })); element.scrollTop = (element.scrollHeight - element.clientHeight) / 2; });
      results[name].paging = [];
      let lastCursor;
      for (let index = 0; index < 2; index++) {
        const beforePage = await stableAnchor(), pageRequests = requests.length;
        // Activate the actual control without Playwright scrolling to its top,
        // which would change the reading gesture and confound the anchor proof.
        await page.getByRole('button', { name: 'Load earlier messages', exact: true }).evaluate(button => button.click());
        const reply = await eventually(() => requests.slice(pageRequests).find(item => item.method === 'sessions.history_page' && item.replied), { description: 'actual older native history page' });
        assert.equal(reply.error, undefined); assert.equal(reply.session, rootId);
        if (lastCursor !== undefined) assert.equal(reply.cursor, lastCursor);
        lastCursor = reply.nextCursor;
        const afterPage = await stableAnchor();
        results[name].paging.push({ before: beforePage, after: afterPage, cursor: reply.cursor, nextCursor: reply.nextCursor });
        assert.equal(afterPage.id, beforePage.id, 'Older history replaced the reading anchor');
        assert.ok(Math.abs(afterPage.offset - beforePage.offset) < 5, `Older history moved the anchor: ${JSON.stringify({ beforePage, afterPage })}`);
      }
      assert.equal(lastCursor, null, 'The actual canonical history has been exhausted');
      assert.equal(await page.getByRole('button', { name: 'Load earlier messages', exact: true }).count(), 0);
      assert.deepEqual(csp, []);
      assert.deepEqual(errors, []);
      await page.screenshot({ path: `${directory}/${name}-reading.png` });
      console.log(`${name}: incoming output and older history preserve the reading anchor; Latest and following pass`);
    } catch (error) {
      results[name] = { ...results[name], error: String(error.stack ?? error), errors, csp, traffic: requests.slice(-80) };
      await page?.screenshot({ path: `${directory}/${name}-failure.png` }).catch(() => {});
      throw error;
    } finally {
      try { await browser?.close(); }
      finally { await writeFile(`${directory}/results.json`, JSON.stringify(results, null, 2)); }
    }
  }
} finally {
  await fixture.close();
}
