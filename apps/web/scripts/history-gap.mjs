import assert from 'node:assert/strict';
import { join } from 'node:path';
import { expect } from '@playwright/test';
import { deadline, eventually } from './native-fixture.mjs';

// Native history has a bounded cursor window, not inferred missing event ranges.
// Delaying/failing actual owner-scoped reads must preserve the loaded evidence;
// only explicit navigation retries an older page. Large-message/count/byte gaps
// are separately proved by native-history-window.test.mjs against the real SDK.
export async function checkHistoryRecovery({ page, client, fixture, root, directory, name, origin = fixture.info.web }) {
  const requests = [], errors = [], connections = new Set(), closing = new Set();
  let mode = 'pass', release, closed = false, tailReads = 0;
  const failure = { code: -32603, kind: 'INTERNAL', message: 'Synthetic history read unavailable' };
  const fail = error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); };
  const closeEndpoint = endpoint => {
    const promise = endpoint.close().catch(fail).finally(() => closing.delete(promise));
    closing.add(promise);
  };
  await page.routeWebSocket('**/api/v4/ws', route => {
    if (closed || connections.size >= 256) { if (!closed) fail(new Error('History recovery connection bound exceeded')); closeEndpoint(route); return; }
    const server = route.connectToServer();
    let ended = false, pending;
    const retire = () => { if (ended) return; ended = true; connections.delete(retire); closeEndpoint(route); closeEndpoint(server); };
    const guarded = work => { try { if (!closed && !ended) work(); } catch (error) { fail(error); retire(); } };
    connections.add(retire); route.onClose(retire); server.onClose(retire);
    route.onMessage(data => guarded(() => {
      const request = JSON.parse(String(data)), owned = request.params?.session_id === root;
      if (owned && mode === 'failed' && request.method === 'sessions.observe') {
        // Keep the read outage observable while changing presentation. No failed
        // page is replayed, and unrelated calls still reach the actual runtime.
        route.send(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: failure })); return;
      }
      if (owned && request.method === 'sessions.history_page' && request.params.cursor === undefined) tailReads++;
      if (owned && request.method === 'sessions.history_page' && request.params.direction === 'backward' && request.params.cursor !== undefined) {
        assert(requests.length < 32, 'Recovery request evidence bound exceeded');
        pending = { id: request.id, params: request.params, mode }; requests.push(pending);
        if (mode === 'fail') { mode = 'failed'; pending.error = failure.kind; route.send(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: failure })); return; }
      }
      server.send(data);
    }));
    server.onMessage(data => guarded(() => {
      const reply = JSON.parse(String(data));
      if (pending?.id === reply.id) {
        assert(!reply.error, `Real history read failed: ${JSON.stringify(reply.error)}`);
        pending.count = reply.result.messages.length; pending.cursor = reply.result.next_cursor;
        pending.revision = reply.result.snapshot.revision;
        if (mode === 'hold') {
          assert(!release, 'Only one older history response may be held');
          release = () => { release = undefined; mode = 'pass'; guarded(() => route.send(data)); }; return;
        }
      }
      route.send(data);
    }));
  });
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  const earlier = reading.getByRole('button', { name: /^(Loading )?Load earlier messages$/ });
  const anchor = () => reading.evaluate(element => {
    const top = element.getBoundingClientRect().top;
    const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.getBoundingClientRect().bottom > top + 40);
    if (!row) throw new Error('No canonical anchor row');
    const walker = document.createTreeWalker(row, NodeFilter.SHOW_TEXT);
    let text; while ((text = walker.nextNode()) && !text.textContent.trim()) {}
    if (!text) throw new Error('No anchor text');
    const range = document.createRange(); range.setStart(text, 0); range.setEnd(text, Math.min(16, text.length));
    const selection = getSelection(); selection.removeAllRanges(); selection.addRange(range);
    window.__historyAnchor = row;
    return { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - top, selected: selection.toString() };
  });
  const verifyAnchor = async before => {
    await expect.poll(() => reading.evaluate((element, before) => {
      const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.dataset.readingId === before.id);
      if (!row || row !== window.__historyAnchor || getSelection().toString() !== before.selected) return 9999;
      return Math.abs(row.getBoundingClientRect().top - element.getBoundingClientRect().top - before.offset);
    }, before)).toBeLessThanOrEqual(2);
  };
  const top = async () => {
    await reading.evaluate(element => { element.dispatchEvent(new Event('scrollend')); element.scrollTop = 0; });
    await expect(earlier).toBeVisible(); await expect(earlier).toBeEnabled();
    await page.waitForTimeout(350);
  };
  try {
    await page.goto(`${origin}/h/${client.runtimeID}/s/${root}`);
    await expect(reading).toBeVisible(); await page.evaluate(() => document.fonts.ready);
    const session = client.session(root), initial = await session.history.snapshot(deadline());
    const draft = 'Keep this draft while reading earlier history.';
    const composer = page.getByRole('textbox', { name: 'Message WHIP', exact: true });
    await composer.fill(draft);
    // Retention can only be asserted for evidence that this observer loaded.
    // Execution metadata reads are independent of the first transcript paint.
    const notebook = page.getByRole('region', { name: 'REPL executions', exact: true });
    await page.getByRole('button', { name: 'REPL', exact: true }).click();
    await expect(notebook.locator(`[data-repl-cell="${fixture.history.cell_id}"]`)).toBeVisible();
    await page.goBack(); await expect(reading).toBeVisible();
    await expect(composer).toHaveValue(draft);
    await top(); mode = 'hold';
    await earlier.focus(); await page.keyboard.press('Enter');
    await eventually(() => release, { description: 'actual older history reply held' });
    const before = await anchor();
    await expect(earlier).toBeDisabled(); await expect(composer).toHaveValue(draft);
    await page.waitForTimeout(400); assert.equal(requests.length, 1, 'One older read owns its wait');
    await verifyAnchor(before); release();
    await expect(earlier).toBeEnabled(); await verifyAnchor(before);
    await expect(composer).toHaveValue(draft);
    await page.evaluate(() => getSelection().removeAllRanges());
    await page.screenshot({ path: join(directory, `${name}-history-delayed.png`) });

    await top(); mode = 'fail';
    await earlier.focus(); await page.keyboard.press('Enter');
    const error = page.locator('[data-error-type="session"]').filter({ hasText: 'Synthetic history read unavailable' });
    await expect(error).toBeVisible(); assert.equal(requests.length, 2);
    const failed = requests[1];
    await page.getByRole('button', { name: 'REPL', exact: true }).click();
    await expect(notebook.locator(`[data-repl-cell="${fixture.history.cell_id}"]`)).toBeVisible();
    await expect(error).toBeVisible(); await page.goBack();
    await expect(reading).toBeVisible(); await expect(error).toBeVisible();
    await page.waitForTimeout(500);
    assert.equal(requests.length, 2, 'Chat/REPL share the failed cursor without replaying it');
    await expect(composer).toHaveValue(draft);
    await top(); mode = 'hold';
    await earlier.focus(); await page.keyboard.press('Enter');
    await eventually(() => release, { description: 'explicit retry uses retained canonical cursor' });
    assert.equal(requests.length, 3);
    assert.deepEqual(requests[2].params, failed.params, 'Explicit retry names the exact same owner, revision and cursor');
    const retryAnchor = await anchor(); release();
    await expect(error).toHaveCount(0); await verifyAnchor(retryAnchor);
    await page.evaluate(() => getSelection().removeAllRanges());
    await expect(composer).toHaveValue(draft);
    assert.equal((await session.history.snapshot(deadline())).revision, initial.revision);
    assert.deepEqual(await fixture.effects(), [], 'History inspection does not invoke a provider');
    // Six actual 100-record windows cross the unchanged 512-record SDK bound.
    // Explicit navigation stays bounded; Latest must then read the canonical
    // tail again rather than pretending an evicted tail remains loaded.
    for (let index = 0; index < 3; index++) {
      await top(); const count = requests.length;
      await earlier.focus(); await page.keyboard.press('Enter');
      await eventually(() => requests.length === count + 1 && requests.at(-1).count === 100, { description: 'explicit older page beyond retained window' });
      await expect(earlier).toBeEnabled();
      assert(await reading.locator('[data-reading-id]').count() < 80);
    }
    assert.equal(tailReads, 1, 'Older browsing does not reread the latest page');
    await page.getByRole('button', { name: 'Latest', exact: true }).click();
    await eventually(() => tailReads === 2, { description: 'Latest refills the evicted native tail' });
    await expect.poll(() => reading.evaluate(element => [...element.querySelectorAll('[data-reading-seq]')].at(-1)?.dataset.readingSeq)).toBe('10000');
    assert(await reading.locator('[data-reading-id]').count() < 80);
    assert.deepEqual(errors, []);
    await page.screenshot({ path: join(directory, `${name}-history-recovered.png`) });
    return { requests, tailReads, delayedAnchor: before, retryAnchor, checks: ['held older page retains DOM/selection/anchor/draft', 'failed exact cursor remains shared across Chat and REPL without page replay', 'keyboard retry preserves exact owner/revision/cursor and reading position', 'Latest restores canonical tail; no provider execution; mounted rows under 80'] };
  } catch (error) {
    error.historyEvidence = { requests, tailReads, errors }; throw error;
  } finally {
    closed = true; release = undefined;
    for (const retire of [...connections]) retire();
    await Promise.all(closing);
  }
}
