import assert from 'node:assert/strict';
import { expect } from '@playwright/test';

// Real layout regression: jsdom cannot reproduce scrollTop clamping when the
// composer temporarily shrinks during its scrollHeight measurement.
export async function checkComposerReading(page) {
  const input = page.getByLabel('Message WHIP', { exact: true });
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  const latest = page.getByRole('button', { name: 'Latest', exact: true });
  const viewport = page.viewportSize() ?? await page.evaluate(() => ({ width: innerWidth, height: innerHeight }));
  const draft = 'I opened a new session and attached it, but I got this error.\nWhat is going on here?\nHow should the model be able to drive the browser?';
  const samples = [];
  const frame = () => page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const settle = async () => { await frame(); await page.waitForTimeout(250); };
  const bottom = async () => {
    if (await latest.count()) await latest.click();
    await expect.poll(() => reading.evaluate(element => element.scrollHeight - element.clientHeight - element.scrollTop)).toBeLessThanOrEqual(2);
    await settle();
  };
  const checkTyping = async label => {
    await input.focus();
    await input.press('End');
    await settle();
    await page.evaluate(() => {
      const reading = document.querySelector('[aria-label="Conversation"]');
      const input = document.querySelector('[data-whip-composer]');
      const measure = () => ({ top: reading.scrollTop, viewport: reading.clientHeight, input: input.clientHeight });
      const probe = window.__composerReadingProbe = { before: measure(), samples: [], frame: 0 };
      const tick = () => {
        probe.samples.push(measure());
        if (probe.samples.length < 512) probe.frame = requestAnimationFrame(tick);
      };
      tick();
    });
    // Include draft persistence between keys as well as faster continuous input.
    await input.pressSequentially('abc', { delay: 200 });
    await input.pressSequentially('def', { delay: 20 });
    await settle();
    const probe = await page.evaluate(() => {
      const probe = window.__composerReadingProbe;
      cancelAnimationFrame(probe.frame);
      delete window.__composerReadingProbe;
      return probe;
    });
    assert(probe.samples.length > 3 && probe.samples.length < 512);
    const drift = Math.max(...probe.samples.map(sample => Math.abs(sample.top - probe.before.top)));
    assert(probe.samples.every(sample => sample.viewport === probe.before.viewport && sample.input === probe.before.input), `${label}: typing without wrapping resized the layout`);
    assert(drift <= 1, `${label}: typing moved the transcript ${drift}px`);
    samples.push({ label, ...probe.before, frames: probe.samples.length, maximumScrollDrift: drift });
  };
  await page.locator('input[type=file]').setInputFiles({ name: 'composer-scroll.txt', mimeType: 'text/plain', buffer: Buffer.from('Isolated attachment for the composer scroll regression.') });
  await expect(page.getByText('composer-scroll.txt · Ready', { exact: true })).toBeVisible();
  await input.fill(draft);
  await bottom();
  await checkTyping('multiline draft with attachment, pinned');

  await reading.hover();
  await page.mouse.wheel(0, -400);
  await expect(latest).toBeVisible();
  await settle();
  await checkTyping('multiline draft with attachment, reading history');
  const detached = await reading.evaluate(element => element.scrollTop);
  await input.fill('A line in a longer draft.\n'.repeat(20));
  await expect.poll(() => input.evaluate(element => element.clientHeight)).toBe(220);
  await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(detached, 0);
  await input.fill(draft);
  await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(detached, 0);

  await input.fill('A line in a longer draft.\n'.repeat(20));
  await bottom();
  await checkTyping('maximum-height draft, pinned');
  await page.setViewportSize({ width: 390, height: 844 });
  await input.fill(draft);
  await bottom();
  await checkTyping('narrow pane with wrapped draft, pinned');

  await input.fill('');
  await page.getByRole('button', { name: 'Remove composer-scroll.txt', exact: true }).click();
  await page.setViewportSize(viewport);
  await bottom();
  await expect.poll(() => input.evaluate(element => element.clientHeight)).toBe(40);
  return samples;
}
