import assert from 'node:assert/strict';
import {mkdir, writeFile} from 'node:fs/promises';
import {chromium, firefox, expect} from '@playwright/test';
import {eventually, startFixture} from '../../../packages/sdk/scripts/fixture.mjs';
import {createWhipClient} from '../../../packages/sdk/dist/index.js';

process.env.WHIP_WEB_REPL_FIXTURE = '1';
process.env.WHIP_WEB_CHAT_POLISH_FIXTURE = '1';
const output = process.env.WHIP_CHAT_POLISH_RESULTS ?? '/tmp/whip-chat-polish-results';
await mkdir(output, {recursive: true});
for (const [name, launcher] of Object.entries({chromium, firefox})) {
  const fixture = await startFixture();
  const browser = await launcher.launch();
  const page = await browser.newPage({viewport: {width: 1440, height: 1000}});
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => {
    localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({version: 1, id: 'claude-code'}));
    window.cspErrors = [];
    document.addEventListener('securitypolicyviolation', event => window.cspErrors.push(event.violatedDirective));
  });
  try {
    const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
    await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${fixture.info.root_id}`);
    const reading = page.getByRole('region', {name: 'Conversation', exact: true});
    const groups = reading.locator('[data-activity-group]');
    await expect(groups).toHaveCount(2);
    await expect(reading.getByText('Both surveys are complete; the reports remain available for inspection.')).toBeVisible();
    await expect(reading.getByText('Mailbox update', {exact: true})).toHaveCount(0);
    const work = groups.filter({hasText: 'Completed work'});
    await expect(work.locator(':scope > details > summary')).toContainText('1 execution · agent updates');
    await expect(groups.last().locator(':scope > details > summary')).toHaveText('Agent updates');
    const reply = reading.locator('[data-message-role="assistant"]').first();
    await expect(reply.locator('[data-message-prose] > p').last()).toHaveCSS('margin-bottom', '0px');
    await page.screenshot({path: `${output}/${name}-collapsed.png`});
    const summary = work.locator(':scope > details > summary');
    await summary.focus();
    await page.keyboard.press('Enter');
    await expect(work.locator('[data-agent-updates]')).not.toHaveAttribute('open');
    await work.locator('[data-agent-updates] > summary').click();
    await expect(work.getByText(/The shared renderer serves both/)).toBeVisible();
    await page.screenshot({path: `${output}/${name}-expanded.png`});
    const link = work.getByRole('button', {name: 'Open in REPL'});
    assert.ok((await link.boundingBox()).width < 200, 'REPL action should fit its text');
    for (const width of [530, 390, 320]) {
      await page.setViewportSize({width, height: 1000});
      await reading.evaluate(node => {node.scrollTop = 0;});
      const user = reading.locator('[data-message-role="user"]').first();
      await expect(user).toBeVisible();
      const geometry = await user.evaluate(node => {
        const bubble = node.querySelector('[data-user-bubble]').getBoundingClientRect();
        return {reserved: node.getBoundingClientRect().bottom - bubble.bottom,
          actionHeight: node.querySelector('[data-message-actions] button').getBoundingClientRect().height};
      });
      assert.ok(geometry.reserved <= 29 && geometry.actionHeight <= 29, `Narrow desktop action spacing: ${JSON.stringify(geometry)}`);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await user.getByRole('button', {name: 'Copy message'}).focus();
      await expect(user.locator('[data-message-actions]')).toHaveCSS('opacity', '1');
      await page.screenshot({path: `${output}/${name}-${width}.png`});
    }
    const touch = await browser.newPage({viewport: {width: 390, height: 844}, hasTouch: true});
    try {
      await touch.goto(page.url());
      const touchActions = touch.locator('[data-message-role="user"] [data-message-actions]').first();
      await expect(touchActions).toHaveCSS('opacity', '1');
      assert.ok((await touchActions.getByRole('button', {name: 'Copy message'}).boundingBox()).height >= 44);
    } finally {await touch.close();}
    const client = createWhipClient({endpoint: fixture.info.endpoint, clientId: `chat-polish-${name}`, clientKind: 'human'});
    try {
      await client.connect();
      await page.setViewportSize({width: 1280, height: 900});
      const session = client.session(fixture.info.root_id);
      const work = session.submit({text: 'hold:large-render'});
      await work.accepted();
      await eventually(async () => (await session.snapshot()).active_turns[fixture.info.root_id]);
      const step = async name => {
        const result = await fetch(`${fixture.info.frontend}/control/repl/activity-${name}`, {method: 'POST'});
        assert.equal(result.status, 204);
      };
      await step('large');
      const live = reading.getByText('I am inspecting the repository.', {exact: true});
      await expect(live).toBeVisible();
      const latest = page.getByRole('button', {name: 'Latest', exact: true});
      if (await latest.count()) await latest.click();
      await expect(live).toBeInViewport();
      await expect(reading.getByText('Tool activity', {exact: true})).toHaveCount(0);
      await expect(groups.last().locator(':scope > details > summary')).toContainText('1 execution');
      const activeGroupId = await groups.last().getAttribute('data-activity-group');
      await expect(reading.locator('[data-message-role="assistant"] [data-message-actions]')).toHaveCount(0);
      const liveArticle = live.locator('xpath=ancestor::article');
      await expect(liveArticle).toHaveCSS('padding-bottom', '8px');
      const hasActiveCopy = () => live.evaluate(node => {
        const row = node.closest('[data-reading-id]');
        let next = row;
        while (next) {
          if (next.querySelector('[data-response-actions]')) return true;
          next = next.nextElementSibling;
        }
        return false;
      });
      assert.equal(await hasActiveCopy(), false, 'active response must not reserve a copy footer');
      await page.screenshot({path: `${output}/${name}-large-stream.png`});
      await step('large-complete');
      await expect(reading.getByText('The inspection has finished.', {exact: true})).toBeVisible();
      assert.equal(await groups.last().getAttribute('data-activity-group'), activeGroupId);
      assert.equal(await hasActiveCopy(), false, 'tool completion alone does not complete the turn');
      assert.equal((await fetch(`${fixture.info.frontend}/control/release?key=large-render`, {method: 'POST'})).status, 204);
      await work.result();
      await expect(page.locator('[data-current-activity]').getByRole('status')).toHaveText('Idle');
      const completed = reading.locator('[data-message-role="assistant"]').last().locator('xpath=ancestor::div[@data-reading-id]');
      await expect(completed.getByRole('button', {name: 'Copy response', exact: true})).toBeVisible();
      if (await latest.count()) await latest.click();
      await expect(completed.getByRole('button', {name: 'Copy response', exact: true})).toBeInViewport();
      await page.screenshot({path: `${output}/${name}-response-footer.png`});
    } finally {client.close();}
    assert.deepEqual(errors, []);
    assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
    await writeFile(`${output}/${name}.json`, JSON.stringify({checks: ['recorded digests grouped with work', 'standalone updates have no invented outcome', 'authored replies preserved', 'raw messages explicitly inspectable', 'keyboard disclosure and copy', 'compact action spacing at 530/390/320px', 'touch actions stay visible with 44px targets', 'no document overflow', 'large cumulative root updates stay in one activity group', 'child and legacy content-only events do not create root tool cards', 'active prose has no copy controls or reserved action space', 'copy footer appears only when the response turn finishes'], errors}, null, 2));
    console.log(`${name}: chat spacing and agent update checks passed`);
  } catch (error) {
    await page.screenshot({path: `${output}/${name}-failure.png`});
    throw error;
  } finally {await browser.close(); await fixture.close();}
}
