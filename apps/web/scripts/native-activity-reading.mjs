import assert from 'node:assert/strict';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { expect } from '@playwright/test';
import { finishActivityScrollTrace, installActivityScrollTrace } from './native-activity-scroll-trace.mjs';
import { deadline } from './native-fixture.mjs';

// Real provider deltas and canonical settlement exercise reading behavior. The
// fixture only releases its own bounded HTTP holds; it never injects UI events.
export async function checkActivityReading({ page, fixture, run, directory, name }) {
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  const latest = page.getByRole('button', { name: 'Latest', exact: true });
  const tail = async () => { if (await latest.isVisible()) await latest.click(); };
  const screenshot = label => page.screenshot({ path: join(directory, `${name}-${label}.png`) });
  const assertLayout = () => expect.poll(() => reading.evaluate(element => {
    const rows = [...element.querySelectorAll('[data-reading-id]')];
    const ids = rows.map(row => row.dataset.readingId);
    const boxes = rows.map(row => row.getBoundingClientRect()).sort((a, b) => a.top - b.top);
    return new Set(ids).size === ids.length && boxes.every((box, index) => index === 0 || box.top >= boxes[index - 1].bottom - 1);
  })).toBe(true);
  const prose = await run('activity:prose');
  await tail();
  await expect(reading.getByRole('heading', { name: 'Streaming report' })).toBeVisible();
  await expect(reading.locator('strong').filter({ hasText: 'Hello' })).toBeVisible();
  fixture.release('activity-prose-more');
  await expect(reading.locator('[data-markdown-block]').filter({ hasText: 'Hello world' })).toBeVisible();
  await expect(reading.locator('code').filter({ hasText: 'const answer = 42;' })).toBeVisible();
  await reading.locator('p').filter({ hasText: 'Hello world' }).evaluate(node => {
    const range = document.createRange(); range.selectNodeContents(node);
    getSelection().removeAllRanges(); getSelection().addRange(range);
  });
  fixture.release('activity-prose-end'); assert.equal((await prose.wait(deadline())).turn.state, 'succeeded');
  await expect.poll(() => page.evaluate(() => getSelection().toString())).toBe('Hello world 🌍');
  await screenshot('markdown');
  const accessible = await reading.ariaSnapshot();
  assert(accessible.includes('heading "Streaming report"'));
  await writeFile(join(directory, `${name}-accessibility.yml`), accessible);
  await page.evaluate(() => getSelection().removeAllRanges());

  const listWork = await run('activity:list'); await tail();
  const list = reading.locator('ol').filter({ hasText: 'dotwhip-diver' });
  for (let index = 0; index < 3; index++) {
    if (index) fixture.release(index === 1 ? 'activity-list-more' : 'activity-list-end');
    await expect(list.locator('li')).toHaveCount(index + 1); await assertLayout();
  }
  await expect(list.locator('strong')).toHaveText(['dotwhip-diver', 'fs-sweeper', 'env-profiler']);
  await expect(list.locator('li').nth(0)).toContainText('its own child');
  await expect(list.locator('li').nth(1)).toContainText('code or data, delegating');
  await screenshot('numbered-list');
  await page.setViewportSize({ width: 390, height: 844 }); await tail();
  await expect(list).toHaveCount(1); await assertLayout(); await screenshot('numbered-list-narrow');
  await page.setViewportSize({ width: 1280, height: 900 }); await assertLayout();
  fixture.release('activity-list-complete'); assert.equal((await listWork.wait(deadline())).turn.state, 'succeeded');

  const scroll = await run('activity:scroll'); await tail();
  await expect(reading.locator('[data-markdown-block]').filter({ hasText: 'Streaming paragraph.' })).toBeVisible();
  const owner = await reading.locator('article').filter({ hasText: 'Streaming paragraph.' }).getAttribute('data-message-id');
  assert(owner && owner.startsWith('message:'));
  const row = reading.locator(`[data-message-id="${owner}"]`);
  const gap = () => reading.evaluate(element => element.scrollHeight - element.clientHeight - element.scrollTop);
  const settledOffset = async () => {
    let previous = -1, unchangedSince = Date.now();
    await expect.poll(async () => {
      const offset = await reading.evaluate(element => element.scrollTop);
      if (offset !== previous) { previous = offset; unchangedSince = Date.now(); }
      return Date.now() - unchangedSince;
    }, { intervals: [50] }).toBeGreaterThanOrEqual(300);
    return previous;
  };
  let next = 0;
  const stream = async () => {
    assert(next < 16);
    const before = (await row.allTextContents()).join('').length;
    fixture.release(`activity-scroll-${next++}`);
    await expect.poll(async () => (await row.allTextContents()).join('').length).toBeGreaterThan(before);
  };
  await page.evaluate(installActivityScrollTrace);
  try {
    for (let index = 0; index < 3; index++) { await stream(); await expect.poll(gap).toBeLessThanOrEqual(2); }
    const box = await reading.boundingBox();
    await page.mouse.click(box.x + 60, box.y + box.height - 40);
    await stream(); await expect.poll(gap).toBeLessThanOrEqual(2);
    await reading.hover(); await page.mouse.wheel(0, -12);
    await expect.poll(gap).toBeGreaterThan(3); await expect(latest).toBeVisible();
  } finally {
    await writeFile(join(directory, `${name}-small-scroll-trace.json`), JSON.stringify(await page.evaluate(finishActivityScrollTrace), null, 2));
  }
  const detached = await settledOffset();
  // Chunk 6 begins a new block while reading remains detached.
  for (let index = 0; index < 3; index++) await stream();
  await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(detached, 0);
  await expect.poll(async () => { if (await gap() > 2) await page.mouse.wheel(0, 800); return gap(); }).toBeLessThanOrEqual(2);
  await expect(latest).toHaveCount(0);
  await stream(); await expect.poll(gap).toBeLessThanOrEqual(2);
  await stream(); await expect.poll(gap).toBeLessThanOrEqual(2);
  // Chunk 9 begins another block concurrently with a new upward gesture.
  await Promise.all([page.mouse.wheel(0, -12), stream()]);
  await expect(latest).toBeVisible(); const overlap = await settledOffset();
  await stream();
  await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(overlap, 0);
  await expect.poll(gap).toBeGreaterThan(3);
  await latest.click(); await expect.poll(gap).toBeLessThanOrEqual(2);
  await page.mouse.wheel(0, -900); await expect(latest).toBeVisible();
  await latest.click(); await reading.hover(); await page.mouse.wheel(0, -120);
  await expect(latest).toBeVisible(); const interrupted = await settledOffset();
  await stream();
  await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(interrupted, 0);
  await screenshot('scroll-detached');
  await latest.click(); await expect.poll(gap).toBeLessThanOrEqual(2); await screenshot('scroll-pinned');
  while (next < 16) await stream();
  fixture.release('activity-scroll-complete'); assert.equal((await scroll.wait(deadline())).turn.state, 'succeeded');
  await assertLayout();
  const result = { pinnedGap: await gap(), detachedOffset: detached, overlapOffset: overlap, interruptedOffset: interrupted,
    checks: ['canonical Markdown/Unicode/code and selection survive settlement', 'ordered-list streaming retains unique virtual keys and nonoverlapping rows at narrow width', 'tall streamed paragraph stays pinned', 'ordinary clicks retain following', '12px upward wheel stays detached through growth/new block', 'downward return resumes following', 'upward gesture overlapping a new block remains detached', 'Latest is interruptible'] };
  await writeFile(join(directory, `${name}-scroll-follow.json`), JSON.stringify(result, null, 2));
  return result;
}
