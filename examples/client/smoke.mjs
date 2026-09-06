// Exercises the actual example UI against an isolated fake-provider daemon.
import assert from 'node:assert/strict';
import { copyFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, expect } from '@playwright/test';
import { repository, run, startFixture } from '../../packages/sdk/scripts/fixture.mjs';

await run(process.execPath, ['examples/client/serve.mjs', '--build']);
const fixture = await startFixture();
const browser = await chromium.launch({ headless: true });
const errors = [];
let expectedDisconnect = false;
try {
  // The isolated host's default routing is resolved normally, but its runner is
  // fake: session creation and execution need no real provider credentials.
  for (const name of ['index.html', 'app.js', 'style.css']) {
    await copyFile(join(repository, 'examples/client/dist', name), join(fixture.directory, 'public', name));
  }
  const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } });
  page.on('pageerror', error => errors.push(String(error)));
  page.on('console', entry => {
    if (entry.type() === 'error' && !(expectedDisconnect && /WebSocket|ERR_CONNECTION|ERR_INTERNET|Failed to fetch/.test(entry.text()))) errors.push(entry.text());
  });
  await page.goto(fixture.info.frontend);
  await page.getByLabel('Daemon', { exact: true }).fill(fixture.info.endpoint);
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.locator('aside [role=status]')).toHaveText('connected');
  await page.getByLabel('Working directory on host').fill(fixture.directory);
  await page.getByRole('button', { name: 'New session', exact: true }).click();
  const composer = page.getByLabel('Message the root agent');
  await expect(composer).toBeVisible();
  await composer.fill('Review the SDK example for correctness.');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  await expect(page.locator('.composer [role=status]')).toHaveText('succeeded');
  await expect(page.locator('.transcript article .role')).toHaveText(['user', 'assistant']);
  await expect(page.locator('.transcript article pre')).toHaveText(['Review the SDK example for correctness.', 'Review the SDK example for correctness.']);
  await expect(composer).toHaveValue('');

  // Keep the turn active while inspecting repeated cumulative tool updates.
  await composer.fill('hold:tool-stream');
  await page.getByRole('button', { name: 'Send', exact: true }).click();
  const calls = page.locator('.transcript article').filter({ has: page.locator('.role', { hasText: /^stream\.tool\.call$/ }) });
  const outputs = page.locator('.transcript article').filter({ has: page.locator('.role', { hasText: /^stream\.tool\.output$/ }) });
  await expect(calls.locator('pre')).toHaveText(['{"code":"print(1)"}', '{"code":"print(1)"}']);
  await expect(outputs.locator('pre')).toHaveText(['first\nsecond', 'first\nsecond']);
  const selected = await page.locator('aside li button').evaluateAll(buttons => buttons.findIndex(button => button.getAttribute('aria-current') === 'true'));
  assert.ok(selected >= 0);
  // A full page reload discards the client projection. The next view must fold
  // the daemon's raw presentation snapshot, rather than resurrect duplicate rows.
  await page.reload();
  await page.getByRole('button', { name: 'Connect', exact: true }).click();
  await expect(page.locator('aside [role=status]')).toHaveText('connected', { timeout: 15_000 });
  await page.locator('aside li button').nth(selected).click();
  await expect(page.locator('.workspace > .row [role=status]')).toHaveText('live');
  await expect(calls.locator('pre')).toHaveText(['{"code":"print(1)"}', '{"code":"print(1)"}']);
  await expect(outputs.locator('pre')).toHaveText(['first\nsecond', 'first\nsecond']);
  await fixture.release('tool-stream');
  await expect(calls).toHaveCount(0);
  await expect(outputs).toHaveCount(0);

  const draft = 'Keep this unsent draft while reconnecting.';
  await composer.fill(draft);
  expectedDisconnect = true;
  // Keep the browser offline while the process restarts so the disconnected UI
  // can be asserted independently of how quickly the daemon returns.
  await page.context().setOffline(true);
  await fixture.crashAndRestart();
  await expect(page.locator('aside [role=status]')).toHaveText('reconnecting');
  await expect(page.getByText('Reconnecting. Your draft stays here; accepted work stays on the host.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeDisabled();
  await expect(composer).toHaveValue(draft);
  await page.context().setOffline(false);
  await expect(page.locator('aside [role=status]')).toHaveText('connected', { timeout: 15_000 });
  await expect(page.locator('.workspace > .row [role=status]')).toHaveText('live');
  expectedDisconnect = false;
  await expect(composer).toHaveValue(draft);
  await expect(page.locator('.transcript article .role')).toHaveText(['user', 'assistant', 'user', 'assistant']);
  await expect(page.getByRole('alert')).toHaveCount(0);
  assert.deepEqual(errors, [], 'Unexpected browser console or JavaScript errors');
  const screenshot = process.env.WHIP_SDK_EXAMPLE_SCREENSHOT ?? '/tmp/whip-sdk-example.png';
  await page.screenshot({ path: screenshot, fullPage: true });
  console.log(JSON.stringify({ passed: true, created_session: true, transcript: true, reconnect_draft: true, console_errors: errors.length, screenshot }, null, 2));
} finally {
  await browser.close();
  await fixture.close();
}
