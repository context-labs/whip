import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { expect } from '@playwright/test';
import { createSessionView, executionRows } from '../../../packages/sdk/dist/state.js';
import { eventually } from '../../../packages/sdk/scripts/fixture.mjs';

// Real committed records, with a delayed/failed transport read only in this
// isolated browser. Both limits exercise the production snapshot and SDK paths.
export async function checkHistoryRecovery({ page, client, fixture, root, directory, name }) {
  const report = [];
  const requests = [];
  let mode = 'hold', releasePage;
  await page.routeWebSocket('**/api/v3/ws', route => {
    const server = route.connectToServer();
    route.onMessage(data => {
      const request = JSON.parse(String(data));
      if (request.method === 'history.page' && request.params.after_seq !== undefined) {
        requests.push(request);
        if (mode === 'hold') { releasePage = () => { mode = 'pass'; server.send(data); }; return; }
        if (mode === 'fail') {
          mode = 'pass';
          route.send(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: { code: -32603, message: 'Synthetic history read unavailable' } }));
          return;
        }
      }
      server.send(data);
    });
  });
  await page.reload();
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  await expect(reading).toBeVisible();
  const session = client.session(root);
  const view = createSessionView(session);
  await view.start();
  const findGap = async () => {
    await reading.hover(); await page.mouse.wheel(0, -1);
    await page.waitForTimeout(200);
    await reading.evaluate(element => { element.dispatchEvent(new Event('scrollend')); element.scrollTop = 0; });
    for (let attempt = 0; attempt < 100; attempt++) {
      const gap = reading.locator('[data-history-gap]').first();
      const visible = await reading.evaluate(element => {
        const gap = element.querySelector('[data-history-gap]');
        if (!gap) { element.scrollTop += element.clientHeight * 0.6; return false; }
        const top = gap.getBoundingClientRect().top - element.getBoundingClientRect().top;
        if (top >= 0 && top < element.clientHeight - 100) return true;
        element.scrollTop += top - 100; return false;
      });
      if (visible) return gap;
      await page.waitForTimeout(30);
    }
    throw new Error('Gap control was not reachable');
  };
  try {
    for (const kind of ['count', 'bytes']) {
      const input = `history-gap:${kind}`;
      mode = kind === 'count' ? 'hold' : 'fail';
      const before = view.getSnapshot().history[root].throughSeq;
      const requestCount = requests.length;
      const work = session.submit({ text: input });
      await work.accepted();
      const live = await eventually(() => {
        const cells = executionRows(view.getSnapshot(), root).filter(row => row.callId?.startsWith(input));
        return cells.length === 5 && cells;
      });
      const ids = live.map(row => row.id);
      await fixture.release(input);
      await work.result();
      const snapshot = await session.snapshot();
      assert.ok(snapshot.message_seqs[0] > before + 1, `${kind} fixture must leave an interior interval`);
      assert.ok(snapshot.messages.length <= 64);
      if (kind === 'bytes') assert.ok(snapshot.messages.length < 64, 'Byte budget must constrain the snapshot separately');
      await eventually(() => requests.length > requestCount);
      const control = await findGap();
      await page.screenshot({ path: join(directory, `${name}-history-${kind}-gap.png`) });
      let anchorDrift;
      if (kind === 'count') {
        await expect(control).toContainText('Loading missing messages');
        // Pin a selected, already loaded row after the gap. Filling above it
        // must preserve the DOM, its selection, and its pixel position.
        const anchor = await control.evaluate(element => {
          const region = element.closest('[role="region"]');
          const row = element.closest('[data-reading-id]').nextElementSibling;
          region.scrollTop += row.getBoundingClientRect().top - region.getBoundingClientRect().top - 40;
          return row.dataset.readingId;
        });
        const position = () => reading.evaluate((element, id) => {
          const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.dataset.readingId === id);
          return row?.getBoundingClientRect().top - element.getBoundingClientRect().top;
        }, anchor);
        await expect.poll(position).toBeCloseTo(40, 0);
        const selected = await reading.evaluate((element, id) => {
          const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.dataset.readingId === id);
          const text = document.createTreeWalker(row, NodeFilter.SHOW_TEXT).nextNode();
          const range = document.createRange(); range.setStart(text, 0); range.setEnd(text, Math.min(16, text.length));
          const selection = document.getSelection(); selection.removeAllRanges(); selection.addRange(range); return selection.toString();
        }, anchor);
        releasePage();
        await expect(control).toHaveCount(0);
        await expect.poll(position).toBeCloseTo(40, 0);
        anchorDrift = Math.abs((await position()) - 40);
        assert.equal(await page.evaluate(() => document.getSelection()?.toString()), selected);
        await page.evaluate(() => document.getSelection()?.removeAllRanges());
      } else {
        await expect(control).toContainText("Couldn't load messages");
        const readsBeforeSwitch = requests.length;
        await page.getByRole('button', { name: 'REPL', exact: true }).click();
        const notebook = page.getByRole('region', { name: 'REPL executions', exact: true });
        await expect(notebook.locator('[data-history-gap]')).toContainText("Couldn't load messages");
        await page.goBack(); await findGap();
        assert.equal(requests.length, readsBeforeSwitch, 'Chat and REPL share the failed range without refetching');
        const retry = control.getByRole('button', { name: 'Retry', exact: true });
        await retry.focus(); await page.keyboard.press('Enter');
        await expect(control).toHaveCount(0);
        await expect.poll(() => page.evaluate(() => document.activeElement?.hasAttribute('data-reading-id'))).toBe(true);
      }
      await eventually(() => !view.getSnapshot().history[root].gaps.length);
      const recovered = executionRows(view.getSnapshot(), root).filter(row => row.callId?.startsWith(input));
      assert.deepEqual(recovered.map(row => row.id), ids);
      assert.ok(recovered.every(row => Number.isInteger(row.seq) && !row.historyUnmatched));
      assert.equal(recovered.flatMap(row => row.hosts).length, 5);
      const state = view.getSnapshot();
      assert.ok(state.retainedBytes <= 8 * 1024 * 1024);
      assert.ok(state.history[root].messages.length <= 512);
      assert.ok(requests.length - requestCount <= (kind === 'bytes' ? 5 : 4));
      await session.submit({ text: `Later reply after ${kind} recovery` }).result();
      await reading.evaluate(element => { element.scrollTop = element.scrollHeight; });
      const latest = page.getByRole('button', { name: 'Latest', exact: true });
      if (await latest.isVisible()) await latest.click();
      await expect.poll(() => reading.evaluate(element => [...element.querySelectorAll('[data-reading-id]')].at(-1)?.textContent)).toContain(`Later reply after ${kind} recovery`);
      const mounted = await reading.locator('[data-reading-id]').count();
      assert.ok(mounted < 80, `${mounted} rendered rows exceeds the normal bound`);
      await page.screenshot({ path: join(directory, `${name}-history-${kind}-recovered.png`) });
      report.push({ kind, snapshotMessages: snapshot.messages.length, missingFrom: before + 1, missingThrough: snapshot.message_seqs[0] - 1,
        gapRequests: requests.length - requestCount, recoveredOperations: recovered.length, anchorDrift,
        retainedBytes: state.retainedBytes, retainedRecords: state.history[root].messages.length, mounted });
    }
    const beforeLarge = view.getSnapshot().history[root].throughSeq;
    const largeRequestStart = requests.length;
    const large = session.submit({ text: 'history-gap:large' });
    await large.accepted();
    await eventually(() => executionRows(view.getSnapshot(), root).some(row => row.callId?.startsWith('history-gap:large')));
    await fixture.release('history-gap:large'); await large.result();
    await eventually(() => view.getSnapshot().history[root].throughSeq > beforeLarge + 700);
    await eventually(() => !view.getSnapshot().history[root].gaps?.some(gap => gap.status === 'loading' || gap.status === 'pending'));
    const largeHistory = view.getSnapshot().history[root];
    assert.ok(largeHistory.messages.length <= 512);
    assert.ok(largeHistory.hasMore || largeHistory.gaps.length, 'Omitted history remains explicitly reachable');
    const continuation = await findGap();
    await expect(continuation.getByRole('button', { name: 'Load missing messages', exact: true })).toBeEnabled();
    assert.equal(requests.length - largeRequestStart, 4, 'Large recovery stops after four automatic pages');
    await page.evaluate(() => {
      const region = document.querySelector('[aria-label="Conversation"]');
      window.historyRecoveryPeakRows = 0;
      const measure = () => { window.historyRecoveryPeakRows = Math.max(window.historyRecoveryPeakRows, region.querySelectorAll('[data-reading-id]').length); };
      window.historyRecoveryObserver = new MutationObserver(measure); window.historyRecoveryObserver.observe(region, { childList: true, subtree: true }); measure();
    });
    const previousEnd = await continuation.getAttribute('data-history-gap');
    await continuation.getByRole('button', { name: 'Load missing messages', exact: true }).click();
    await expect(continuation.getByRole('button', { name: 'Load missing messages', exact: true })).toBeEnabled();
    assert.equal(await continuation.getAttribute('data-history-gap'), previousEnd, 'One-page continuation preserves the control identity');
    assert.equal(requests.length - largeRequestStart, 5);
    const peakRows = await page.evaluate(() => { window.historyRecoveryObserver.disconnect(); return window.historyRecoveryPeakRows; });
    assert.ok(peakRows < 80, `Recovery mounted ${peakRows} rows`);
    await page.screenshot({ path: join(directory, `${name}-history-continuation.png`) });
    report.push({ kind: 'large', retainedRecords: largeHistory.messages.length, gaps: largeHistory.gaps, hasMore: largeHistory.hasMore, automaticPages: 4, explicitPages: 1, peakRows });
    await writeFile(join(directory, `${name}-history-recovery.json`), JSON.stringify(report, null, 2));
    return report;
  } finally { await view.dispose(); }
}
