import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { startFixture, eventually } from '../../../packages/sdk/scripts/fixture.mjs';

const output = '/tmp/whip-user-message-results';
await mkdir(output, { recursive: true });
for (const [name, launcher] of Object.entries({ chromium, firefox })) {
  const fixture = await startFixture();
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
    const prompt = 'hold:message-visibility';
    await composer.fill(prompt); await send.click();
    const user = page.locator('[data-message-role="user"]').filter({ hasText: prompt });
    await user.getByRole('status').filter({ hasText: 'Sending…' }).waitFor();
    assert.equal(await user.count(), 1, 'One preview before request transmission');
    assert.equal((await session.snapshot()).messages?.length ?? 0, 0, 'No committed input yet');
    assert.equal((await fixture.effects()).length, 0, 'Host has not received the held request');
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
    assert.equal(await page.locator('[data-message-role="assistant"]').count(), 1, 'Response is streaming but unfinished');
    await bubble.hover(); await page.screenshot({ path: `${output}/${name}-light.png` });
    await composer.fill('Queued while the response runs'); await composer.press('Enter');
    const queued = page.locator('[data-message-role="user"]').filter({ hasText: 'Queued while the response runs' });
    await queued.getByRole('status').filter({ hasText: 'Queued' }).waitFor();
    const roles = await page.locator('[data-message-role]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-message-role')));
    assert.deepEqual(roles, ['user', 'assistant', 'user'], 'Queued message follows the current response');
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await composer.waitFor(); await user.waitFor();
    assert.equal(await user.count(), 1, 'Reload reconstructs running user input from the inbox');
    await bubble.hover(); await page.screenshot({ path: `${output}/${name}-dark.png` });
    await fixture.release('message-visibility');
    await eventually(async () => Object.keys((await session.snapshot()).active_turns).length === 0);
    await eventually(async () => (await user.count()) === 1 && (await user.getAttribute('data-message-id')).startsWith('h:'));
    // Identical authored prompts must remain separate after fast completion.
    for (let index = 0; index < 2; index++) {
      await composer.fill('Same message'); await send.click();
      await eventually(async () => await composer.inputValue() === '');
    }
    await eventually(async () => (await page.locator('[data-message-role="user"]').filter({ hasText: 'Same message' }).count()) === 2);
    await eventually(async () => (await page.locator('[data-message-role="user"][data-message-id^="h:"]').count()) === 4);
    await page.setViewportSize({ width: 390, height: 844 });
    await eventually(async () => {
      await page.locator('[data-message-role="user"]').last().scrollIntoViewIfNeeded();
      return true;
    });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.screenshot({ path: `${output}/${name}-narrow.png` });
    assert.deepEqual(errors, []);
    assert.deepEqual(await page.evaluate(() => window.__messageCsp), []);
    console.log(`${name}: immediate pre-send bubble, running/reloaded input, exact-once completion, identical messages, hover/focus, right alignment, light/dark/narrow and CSP passed`);
  } catch (error) {
    if (page) {
      await page.screenshot({ path: `${output}/${name}-failure.png` }).catch(() => {});
      console.log(await page.locator('body').innerText().catch(() => ''));
    }
    throw error;
  } finally { client.close(); await browser.close(); await fixture.close(); }
}
