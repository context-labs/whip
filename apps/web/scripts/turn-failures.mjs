import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Production renderer and real durable outcomes; no live credentials or model calls.
process.env.WHIP_WEB_TURN_FAILURE_FIXTURE = '1';
const directory = process.env.WHIP_TURN_FAILURE_RESULTS ?? '/tmp/whip-turn-failure-results';
await mkdir(directory, { recursive: true });
const checks = [];
for (const [name, engine] of Object.entries({ chromium, firefox })) {
  const fixture = await startFixture();
  const browser = await engine.launch();
  try {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
    const route = `${origin}/h/${fixture.info.runtime_id}/s/${fixture.info.root_id}`;
    for (const theme of ['claude-code', 'light', 'dark']) {
      for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 800 });
        await page.goto(route);
        await page.evaluate(theme => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: theme })), theme);
        for (const variant of ['empty', 'history', 'long']) {
          await page.goto(`${route}?agent=turn-failed-${variant}&view=repl`);
          const notice = page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true });
          await expect(notice).toHaveCount(1);
          await expect(notice).toContainText('Architecture researcher');
          await expect(notice).toContainText('Invalid');
          if (variant === 'history') await expect(page.locator('[data-repl-cell]').filter({ visible: true })).toHaveCount(1);
          else await expect(page.getByText('The last turn failed', { exact: true })).toBeVisible();
          if (variant === 'long') {
            const details = notice.getByRole('button', { name: 'Error details', exact: true });
            await details.focus(); await page.keyboard.press('Enter');
            await expect(notice.getByText('Showing the beginning of the recorded error.')).toBeVisible();
          }
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
          const bounds = await notice.boundingBox();
          assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= width + 1 && bounds.height <= 321);
          await page.screenshot({ path: join(directory, `${name}-${theme}-${width}-${variant}.png`) });
        }
        checks.push(`${name}: ${theme}, ${width}px: empty/existing cells, large error, keyboard, bounded geometry`);
      }
    }
    await page.goto(`${route}?agent=turn-failed-empty`);
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(1);
    await page.reload();
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(1);
    await fixture.crashAndRestart();
    await page.reload();
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(1);
    const response = await fetch(`${fixture.info.frontend}/control/turn-outcome/succeed`, { method: 'POST' });
    assert.ok(response.ok, await response.text());
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(0);
    await page.reload();
    await expect(page.getByText('Follow-up completed.', { exact: true })).toBeVisible();
    assert.deepEqual(errors, []);
    checks.push(`${name}: chat, reload, daemon restart and subsequent success`);
  } finally { await browser.close(); await fixture.close(); }
}
await writeFile(join(directory, 'report.json'), JSON.stringify(checks, null, 2));
console.log(`Passed ${checks.length} turn-outcome workflows. Artifacts: ${directory}`);
