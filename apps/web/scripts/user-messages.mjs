import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium, firefox, expect } from '@playwright/test';
import { startFixture, eventually, deadline } from './native-fixture.mjs';

const output = '/tmp/whip-user-message-results';
await mkdir(output, { recursive: true });
for (const [name, launcher] of Object.entries({ chromium, firefox })) {
  const fixture = await startFixture();
  const browser = await launcher.launch();
  const client = await fixture.connect();
  let page;
  try {
    const created = await fixture.createRoot(client);
    const rootId = created.root.id;
    const session = client.session(rootId);
    const origin = fixture.info.web;
    page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
    const errors = [];
    page.on('pageerror', error => errors.push({ message: error.message, stack: error.stack }));
    await page.addInitScript(() => {
      window.__messageCsp = [];
      document.addEventListener('securitypolicyviolation', event => window.__messageCsp.push(event.violatedDirective));
    });
    let releaseSend, heldRequest;
    let intercept = true;
    const submissions = [], checks = [];
    await page.routeWebSocket('**/api/v4/ws', socket => {
      const server = socket.connectToServer();
      socket.onMessage(message => {
        const request = JSON.parse(String(message));
        if (request.method === 'sessions.submit') submissions.push(request);
        if (request.method === 'receipts.match') checks.push(request);
        if (intercept && JSON.parse(String(message)).method === 'sessions.submit') {
          intercept = false; heldRequest = request;
          server.onMessage(data => {
            const response = JSON.parse(String(data));
            if (response.id === heldRequest.id) { socket.close({ code: 1011, reason: 'fixture dropped native acceptance' }); server.close(); }
            else socket.send(data);
          });
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
    assert.equal((await session.history.page({ direction: 'forward' }, deadline())).messages.length, 0, 'No committed input yet');
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
    const stableID = await user.getAttribute('data-message-id');
    await eventually(async () => (await session.activity(deadline())).active_turn);
    await page.getByRole('button', { name: 'Check status', exact: true }).click();
    await eventually(() => checks.length > 0, { description: 'lost acceptance is explicitly inspected through native receipts' });
    assert.equal(submissions.length, 1, 'Lost ACK triggered a mutation replay');
    assert.equal(await user.getAttribute('data-message-id'), stableID);
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
    await bubble.hover(); await page.screenshot({ path: `${output}/${name}-light.png` });
    await composer.fill('Queued while the response runs'); await composer.press('Enter');
    const queued = page.getByRole('region', { name: 'Queued messages', exact: true });
    await queued.getByRole('button', { name: 'Preview queued message: Queued while the response runs', exact: true }).waitFor();
    await eventually(async () => (await session.inputs.page({ state: 'queued' }, deadline())).items.length === 1, { description: 'exact queued native input admitted' });
    const roles = await page.locator('[data-message-role]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-message-role')));
    assert.deepEqual(roles, ['user', 'assistant'], 'Queued inputs stay in the scoped composer queue until claimed');
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await composer.waitFor(); await user.waitFor();
    assert.equal(await user.count(), 1, 'Reload reconstructs the running input without duplicating its row');
    await queued.getByRole('button', { name: 'Preview queued message: Queued while the response runs', exact: true }).waitFor();
    await bubble.hover(); await page.screenshot({ path: `${output}/${name}-dark.png` });
    await fixture.release('thinking-response');
    await eventually(async () => !(await session.activity(deadline())).active_turn);
    await expect(working).toHaveCount(0);
    await eventually(async () => (await user.count()) === 1 && (await user.getAttribute('data-message-id')) === stableID && await user.evaluate(element => !!element.closest('[data-reading-seq]')?.getAttribute('data-reading-seq')));
    // Identical authored prompts must remain separate after fast completion.
    for (let index = 0; index < 2; index++) {
      await composer.fill('Same message'); await send.click();
      await eventually(async () => await composer.inputValue() === '');
    }
    await eventually(async () => (await page.locator('[data-message-role="user"]').filter({ hasText: 'Same message' }).count()) === 2);
    await eventually(async () => (await page.locator('[data-reading-seq] [data-message-role="user"]').count()) === 4);
    await page.setViewportSize({ width: 390, height: 844 });
    await eventually(async () => {
      await page.locator('[data-message-role="user"]').last().scrollIntoViewIfNeeded();
      return true;
    });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.screenshot({ path: `${output}/${name}-narrow.png` });
    assert.deepEqual(errors, []);
    assert.deepEqual(await page.evaluate(() => window.__messageCsp), []);
    console.log(`${name}: immediate Sending, thinking before the first token, 7s caption rotation, trailer completion, pre-send bubble, native lost-ACK receipt inspection without replay, running/reloaded input, stable exact-identity completion, identical messages, hover/focus, right alignment, light/dark/narrow and CSP passed`);
  } catch (error) {
    if (page) {
      await page.screenshot({ path: `${output}/${name}-failure.png` }).catch(() => {});
      console.log(await page.locator('body').innerText().catch(() => ''));
    }
    throw error;
  } finally { await browser.close(); await fixture.close(); }
}
