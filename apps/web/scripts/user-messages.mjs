import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { readRendererManifest, verifyRenderer } from '../../../scripts/renderer-artifact.mjs';
import { chromium, firefox, expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { startFixture, eventually } from '../../../packages/sdk/scripts/fixture.mjs';

// The fixture gateway embeds internal/webassets/dist. Fail clearly instead of
// silently validating an old renderer after a frontend-only build.
const renderer = await readRendererManifest(fileURLToPath(new URL('../renderer-manifest.json', import.meta.url)));
await verifyRenderer(fileURLToPath(new URL('../../../internal/webassets/dist', import.meta.url)), renderer);
const output = '/tmp/whip-user-message-results';
await mkdir(output, { recursive: true });
for (const [name, launcher] of Object.entries({ chromium, firefox })) {
  const fixture = await startFixture({ lifetimeMs: 8 * 60_000 });
  const browser = await launcher.launch();
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: crypto.randomUUID(), clientKind: 'human' });
  let page;
  try {
    await client.connect();
    const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
    const rootId = created.result.root_id;
    const session = client.session(rootId);
    const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
    page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.addInitScript(() => {
      window.__messageCsp = [];
      document.addEventListener('securitypolicyviolation', event => window.__messageCsp.push(event.violatedDirective));
    });
    let releaseSend;
    let intercept = true;
    await page.routeWebSocket('**/api/v3/ws', socket => {
      const server = socket.connectToServer();
      socket.onMessage(message => {
        if (intercept && JSON.parse(String(message)).method === 'command.submit') {
          intercept = false;
          releaseSend = () => server.send(message);
        } else server.send(message);
      });
    });
    await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${rootId}`);
    const composer = page.getByLabel('Message WHIP', { exact: true });
    const send = page.getByRole('button', { name: 'Send message', exact: true });
    await eventually(async () => await send.isEnabled() || await composer.isEnabled());
    const prompt = 'hold:thinking-response';
    await composer.fill(prompt); await send.click();
    const user = page.locator('[data-message-role="user"]').filter({ hasText: prompt });
    await user.getByRole('status').filter({ hasText: 'Sending…' }).waitFor();
    assert.equal(await user.count(), 1, 'One preview before request transmission');
    assert.equal((await session.snapshot()).messages?.length ?? 0, 0, 'No committed input yet');
    assert.equal((await fixture.effects()).length, 0, 'Host has not received the held request');
    const working = page.locator('[data-transcript-working]');
    await expect(working).toHaveText('Sending…');
    await expect(working.locator('[data-turn-elapsed]')).toHaveCount(0);
    await expect(working.locator('[data-activity-animation] > span')).toHaveCount(9);
    await page.screenshot({ path: `${output}/${name}-sending.png` });
    assert.equal(await user.locator('time').count(), 1);
    const bubble = user.locator('[data-user-bubble]');
    const box = await bubble.boundingBox(), rowBox = await user.boundingBox();
    assert.ok(Math.abs(box.x + box.width - rowBox.x - rowBox.width) < 2, 'Bubble is aligned right');
    assert.ok(box.width < rowBox.width * .81, 'Short messages shrink to their text');
    await composer.focus(); await page.mouse.move(1, 1);
    const actions = user.locator('[data-message-actions]');
    assert.equal(await actions.evaluate(node => getComputedStyle(node).opacity), '0');
    await bubble.hover();
    assert.equal(await actions.evaluate(node => getComputedStyle(node).opacity), '1');
    await page.mouse.move(1, 1); await user.getByRole('button', { name: 'Copy message' }).focus();
    assert.equal(await actions.evaluate(node => getComputedStyle(node).opacity), '1', 'Keyboard focus reveals actions');
    await composer.focus();
    await eventually(() => releaseSend);
    releaseSend();
    await eventually(async () => (await session.snapshot()).inbox?.some(item => item.status === 'running'));
    await eventually(async () => (await user.count()) === 1 && !(await user.getByRole('status').count()));
    await expect(working).toContainText('Thinking…');
    await expect(page.locator('[data-message-role="assistant"]')).toHaveCount(0);
    await expect(working.locator('[data-activity-animation]')).toHaveAttribute('data-activity-animation', 'running');
    await expect(working).toContainText('Pondering…', { timeout: 10_000 });
    await expect(working.locator('[data-turn-elapsed]')).toContainText('s');
    await page.screenshot({ path: `${output}/${name}-thinking.png` });
    await fixture.release('thinking-first-token');
    await expect(working).toContainText('Writing a response…');
    assert.equal(await page.locator('[data-message-role="assistant"]').count(), 1, 'Response is streaming but unfinished');
    await expect(page.locator('[data-response-actions]')).toHaveCount(0);
    await bubble.hover(); await page.screenshot({ path: `${output}/${name}-light.png` });
    await composer.fill('Queued while the response runs'); await composer.press('Enter');
    await expect(page.getByText('1 queued message', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Steer queued message: Queued while the response runs', exact: true })).toBeVisible();
    const roles = await page.locator('[data-message-role]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-message-role')));
    assert.deepEqual(roles, ['user', 'assistant'], 'Queued input stays in the queue lane until delivery');
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await composer.waitFor(); await user.waitFor();
    assert.equal(await user.count(), 1, 'Reload reconstructs running user input from the inbox');
    await bubble.hover(); await page.screenshot({ path: `${output}/${name}-dark.png` });
    await fixture.release('thinking-response');
    await eventually(async () => Object.keys((await session.snapshot()).active_turns).length === 0);
    await expect(working).toHaveCount(0);
    await eventually(async () => (await user.count()) === 1 && (await user.getAttribute('data-message-id')).startsWith('h:'));
    // Identical authored prompts must remain separate after fast completion.
    for (let index = 0; index < 2; index++) {
      await composer.fill('Same message'); await send.click();
      await eventually(async () => await composer.inputValue() === '');
    }
    await eventually(async () => (await page.locator('[data-message-role="user"]').filter({ hasText: 'Same message' }).count()) === 2);
    await eventually(async () => (await page.locator('[data-message-role="user"][data-message-id^="h:"]').filter({ hasText: 'Same message' }).count()) === 2);
    await page.setViewportSize({ width: 390, height: 844 });
    await eventually(async () => {
      await page.locator('[data-message-role="user"]').last().scrollIntoViewIfNeeded();
      return true;
    });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.screenshot({ path: `${output}/${name}-narrow.png` });
    await checkResponseActions({ page, browser, client, fixture, origin, name });
    assert.deepEqual(errors, []);
    assert.deepEqual(await page.evaluate(() => window.__messageCsp), []);
    console.log(`${name}: immediate Sending, thinking before the first token, 7s caption rotation, trailer completion, pre-send bubble, running/reloaded input, exact-once completion, identical messages, hover/focus, right alignment, light/dark/narrow and CSP passed`);
  } catch (error) {
    if (page) {
      await page.screenshot({ path: `${output}/${name}-failure.png` }).catch(() => {});
      console.log(await page.locator('body').innerText().catch(() => ''));
    }
    throw error;
  } finally { client.close(); await browser.close(); await fixture.close(); }
}

// Reuse the real daemon fixture: verify commands against persisted history, not
// just that a menu closes. Clipboard I/O is the only substituted platform edge.
async function checkResponseActions({ page, browser, client, fixture, origin, name }) {
  await page.setViewportSize({ width: 1440, height: 960 });
  const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
  const rootId = created.result.root_id, session = client.session(rootId);
  const first = 'Response footer fixture — keep this answer';
  await session.submit({ text: first }).result();
  await eventually(async () => (await session.history.page()).messages.filter(entry => entry.message?.role === 'assistant').length === 1);
  await session.submit({ text: 'Later response — remove only this turn' }).result();
  await eventually(async () => (await session.history.page()).messages.filter(entry => entry.message?.role === 'assistant').length === 2);
  const original = (await session.history.page()).messages;
  const answer = original.find(entry => entry.message?.role === 'assistant');
  assert.ok(answer?.message.sent_at, 'Assistant timestamps are recorded on the host');
  const prefix = original.filter(entry => entry.seq <= answer.seq);
  const url = origin + '/h/' + fixture.info.runtime_id + '/s/' + rootId;
  await page.goto(url);
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  const footers = page.locator('[data-response-actions]');
  const footer = footers.first();
  const menuButton = footer.getByRole('button', { name: 'Response history actions' });
  const composer = page.getByLabel('Message WHIP', { exact: true });
  await expect(footers).toHaveCount(2);
  assert.equal(Date.parse(await footer.locator('time').getAttribute('datetime')), Date.parse(answer.message.sent_at));
  assert.ok((await footer.locator('time').getAttribute('aria-label'))?.length > 10, 'Full date/time is accessible');
  await composer.focus(); await page.mouse.move(1, 1);
  assert.equal(await footer.evaluate(node => getComputedStyle(node).opacity), '0');
  const before = await reading.evaluate(node => node.scrollTop);
  await page.locator('[data-message-role="assistant"]').first().hover();
  assert.equal(await footer.evaluate(node => getComputedStyle(node).opacity), '1', 'Hovering the response reveals its footer');
  assert.equal(await reading.evaluate(node => node.scrollTop), before, 'Revealing controls preserves reading position');
  const geometry = await footer.boundingBox();
  const prose = await page.locator('[data-message-role="assistant"]').first().boundingBox();
  assert.ok(Math.abs(geometry.x - prose.x) < 2, 'Agent footer aligns to the prose left edge');
  await page.evaluate(() => {
    window.__responseCopies = [];
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: {
      writeText: async text => { window.__responseCopies.push(text); },
    } });
  });
  await footer.getByRole('button', { name: 'Copy response', exact: true }).click();
  await expect(footer.getByRole('button', { name: 'Copied', exact: true })).toBeVisible();
  assert.deepEqual(await page.evaluate(() => window.__responseCopies), [answer.message.content]);
  await page.mouse.move(1, 1); await menuButton.focus();
  assert.equal(await footer.evaluate(node => getComputedStyle(node).opacity), '1', 'Keyboard focus reveals controls');
  await page.keyboard.press('Enter');
  await expect(page.getByRole('menuitem', { name: 'Fork from here', exact: true })).toBeVisible();
  await page.mouse.move(1, 1);
  assert.equal(await footer.evaluate(node => getComputedStyle(node).opacity), '1', 'Open menu preserves controls');
  await page.keyboard.press('Escape');
  await expect(menuButton).toBeFocused();

  // A denied write belongs to this response; successful retry clears the error.
  await page.evaluate(() => { navigator.clipboard.writeText = async () => { throw new Error('Clipboard blocked for this test'); }; });
  await footer.getByRole('button', { name: 'Copy response', exact: true }).click();
  await expect(page.getByText('Could not copy message', { exact: true })).toBeVisible();
  await footer.getByText('Error details', { exact: true }).click();
  await expect(page.getByText('Clipboard blocked for this test', { exact: true })).toBeVisible();
  await page.evaluate(() => { navigator.clipboard.writeText = async text => { window.__responseCopies.push(text); }; });
  await footer.getByRole('button', { name: 'Copy response', exact: true }).click();
  await expect(page.getByText('Clipboard blocked for this test', { exact: true })).toHaveCount(0);

  for (const theme of ['light', 'dark', 'claude-code']) {
    await page.evaluate(id => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id })), theme);
    await page.reload(); await expect(footers).toHaveCount(2);
    assert.equal(Date.parse(await footer.locator('time').getAttribute('datetime')), Date.parse(answer.message.sent_at));
    await menuButton.focus(); await page.keyboard.press('Enter');
    const menu = page.getByRole('menu');
    await expect(menu).toBeVisible();
    const box = await menu.boundingBox();
    assert.ok(box.x >= 0 && box.y >= 0 && box.x + box.width <= 1440 && box.y + box.height <= 960, 'Menu stays in the viewport');
    await page.screenshot({ path: output + '/' + name + '-response-' + theme + '.png' });
    await page.keyboard.press('Escape');
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await menuButton.focus(); await page.keyboard.press('Enter');
  const narrowMenu = await page.getByRole('menu').boundingBox();
  assert.ok(narrowMenu.x >= 0 && narrowMenu.x + narrowMenu.width <= 390, 'Narrow response menu does not overflow');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  await page.screenshot({ path: output + '/' + name + '-response-narrow.png' });
  await page.keyboard.press('Escape');
  await page.setViewportSize({ width: 1440, height: 960 });

  await menuButton.focus(); await page.keyboard.press('Enter');
  await page.getByRole('menuitem', { name: 'Fork from here', exact: true }).click();
  const forkDialog = page.getByRole('dialog', { name: /Fork/ });
  await forkDialog.getByRole('button', { name: 'Confirm fork', exact: true }).click();
  await eventually(() => !page.url().endsWith('/s/' + rootId));
  const forkId = new URL(page.url()).pathname.split('/s/')[1]?.split('/')[0];
  assert.ok(forkId && forkId !== rootId, 'Fork opens the new session');
  assert.deepEqual((await client.session(forkId).history.page()).messages.map(entry => entry.message), prefix.map(entry => entry.message), 'Fork includes selected response and no later records');
  assert.equal((await session.history.page()).messages.length, original.length, 'Fork leaves source history alone');

  await page.goto(url); await expect(footers).toHaveCount(2);
  await menuButton.focus(); await page.keyboard.press('Enter');
  await page.getByRole('menuitem', { name: 'Rewind to here…', exact: true }).click();
  const rewindDialog = page.getByRole('dialog', { name: /Rewind/ });
  assert.equal((await session.history.page()).messages.length, original.length, 'Rewind waits for confirmation');
  await rewindDialog.getByRole('button', { name: 'Confirm rewind', exact: true }).click();
  await expect(rewindDialog).toHaveCount(0);
  assert.deepEqual((await session.history.page()).messages.map(entry => entry.message), prefix.map(entry => entry.message), 'Rewind retains selected response, removes only later history');
  await expect(footers).toHaveCount(1);
  await menuButton.focus(); await page.keyboard.press('Enter');
  await expect(page.getByRole('menuitem', { name: 'Rewind to here…', exact: true })).toBeDisabled();
  await page.keyboard.press('Escape');

  const touchContext = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true });
  try {
    const touch = await touchContext.newPage();
    await touch.goto(url);
    const actions = touch.locator('[data-response-actions]').first();
    await expect(actions).toBeVisible();
    assert.equal(await actions.evaluate(node => getComputedStyle(node).opacity), '1', 'Touch controls are always available');
    const sizes = await actions.getByRole('button').evaluateAll(nodes => nodes.map(node => { const rect = node.getBoundingClientRect(); return [rect.width, rect.height]; }));
    assert.ok(sizes.length >= 2 && sizes.every(([width, height]) => width >= 44 && height >= 44), 'Touch targets are at least 44px');
    await actions.getByRole('button', { name: 'Response history actions' }).tap();
    await expect(touch.getByRole('menuitem', { name: 'Fork from here', exact: true })).toBeVisible();
    await touch.screenshot({ path: output + '/' + name + '-response-touch.png' });
  } finally { await touchContext.close(); }
  console.log(name + ': response date/copy/menu, hover/focus/touch, clipboard failure/retry, theme/reload/narrow, and real inclusive fork/rewind passed');
}
