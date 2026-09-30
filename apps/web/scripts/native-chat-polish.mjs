import assert from 'node:assert/strict';
import { createHash, randomUUID } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, repository, startFixture } from './native-fixture.mjs';

const output = process.env.WHIP_CHAT_POLISH_RESULTS ?? '/tmp/whip-chat-polish-results';
await mkdir(output, { recursive: true });
const reports = [], names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
for (const name of names) {
  let fixture, browser, page;
  const errors = [], csp = [], checks = [];
  try {
    fixture = await startFixture({ chatPolishStreams: true, lifetimeMs: 300_000 });
    await writeFile(join(fixture.directory, 'polish.txt'), 'Recorded native file contents.');
    const client = await fixture.connect(`polish-${name}`), { root } = await fixture.createRoot(client);
    const session = client.session(root.id), policy = await session.permissions.policy(deadline());
    await session.permissions.setMode({ mode: 'automatic', expected_revision: policy.revision }, randomUUID(), deadline());
    const run = async (owner, text) => { const handle = owner.submission([{ type: 'text', text }], randomUUID()); await handle.send(deadline()); return handle; };
    const childRequest = randomUUID();
    const child = await session.spawn({ overrides: { report_mode: 'message' }, grant_ids: [], parts: [{ type: 'text', text: 'Prepare the survey.' }] }, childRequest, deadline());
    assert.equal((await client.wait(childRequest, deadline())).turn.state, 'succeeded');
    const sendMail = (delivery, subject, body) => client.sendMail({ sender_id: child.session.id, recipient_id: root.id, delivery, subject, body }, randomUUID(), deadline());
    const firstMail = await sendMail('next_turn', 'Survey findings', 'The shared renderer serves both web and desktop clients.');
    const saved = await run(session, 'polish:saved'), savedResult = await saved.wait(deadline());
    assert.equal(savedResult.turn.state, 'succeeded');
    const secondMail = await sendMail('queued', 'Follow-up findings', 'The separate update is still available without inventing an execution.');
    await eventually(async () => (await session.mail.read(secondMail.mail_id, deadline())).mail.state === 'delivered');
    await eventually(async () => !(await session.activity(deadline())).active_turn);
    const history = await session.history.page({ direction: 'backward' }, deadline());
    const mailMessages = history.messages.filter(message => message.mail);
    assert.equal(mailMessages.length, 2); assert(mailMessages.some(message => message.mail.id === firstMail.mail_id));
    assert.equal((await session.turns.cells(savedResult.turn.id, {}, deadline())).items.length, 1);
    const standalone = mailMessages.find(message => message.mail.id === secondMail.mail_id);
    assert.equal((await session.turns.cells(standalone.turn_id, {}, deadline())).items.length, 0);

    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    page.setDefaultTimeout(15000);
    page.on('pageerror', error => { if (errors.length < 32) errors.push(error.message); });
    await page.exposeFunction('__polishCSP', value => { if (csp.length < 32) csp.push(value); });
    await page.addInitScript(() => {
      localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' }));
      document.addEventListener('securitypolicyviolation', event => { void window.__polishCSP(event.violatedDirective); });
    });
    const manifest = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
    for (const [path, file] of Object.entries(manifest.files)) {
      const response = await fetch(fixture.info.web + '/' + path); assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256);
    }
    await page.goto(`${fixture.info.web}/h/${client.runtimeID}/s/${root.id}`);
    const reading = page.getByRole('region', { name: 'Conversation', exact: true });
    const groups = reading.locator('[data-activity-group]');
    await expect(groups).toHaveCount(2);
    const work = groups.filter({ hasText: '1 execution' });
    await expect(work).toHaveCount(1);
    await expect(work.getByRole('button')).toHaveText('1 execution · agent updates');
    await expect(groups.last().getByRole('button')).toHaveText('Agent updates');
    await expect(reading.getByText('Both surveys are complete; the reports remain available for inspection.', { exact: true })).toBeVisible();
    await expect(reading.getByText('Mailbox update', { exact: true })).toHaveCount(0);
    const reply = reading.locator('[data-message-role="assistant"]').first();
    await expect(reply.locator('[data-message-prose] > p').last()).toHaveCSS('margin-bottom', '0px');
    await page.screenshot({ path: join(output, `${name}-collapsed.png`) });
    const workID = await work.getAttribute('data-activity-group');
    await work.getByRole('button').focus(); await page.keyboard.press('Enter');
    const members = reading.locator(`[data-activity-owner="${workID}"]`);
    const mail = members.locator('[data-activity-content]').filter({ hasText: 'Agent messages' });
    await expect(mail).toHaveAttribute('aria-expanded', 'false');
    await mail.click(); await expect(reading.getByText(/The shared renderer serves both/)).toBeVisible();
    const execution = members.filter('[data-activity-step]').getByRole('button', { name: 'Execution Done', exact: true });
    await execution.click();
    const link = reading.getByRole('button', { name: 'Open in REPL', exact: true });
    assert((await link.boundingBox()).width < 200, 'REPL action must fit its text');
    await page.screenshot({ path: join(output, `${name}-expanded.png`) });
    checks.push('actual child mail and execution share a recorded group; independent mail has no invented outcome; authored replies and explicit details survive');

    for (const width of [530, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      await reading.evaluate(node => { node.scrollTop = 0; });
      const user = reading.locator('[data-message-role="user"]').first(); await expect(user).toBeVisible();
      const geometry = await user.evaluate(node => {
        const bubble = node.querySelector('[data-user-bubble]').getBoundingClientRect();
        return { reserved: node.getBoundingClientRect().bottom - bubble.bottom, actionHeight: node.querySelector('[data-message-actions] button').getBoundingClientRect().height };
      });
      assert(geometry.reserved <= 29 && geometry.actionHeight <= 29, `Narrow desktop spacing: ${JSON.stringify(geometry)}`);
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      await user.getByRole('button', { name: 'Copy message', exact: true }).focus();
      await expect(user.locator('[data-message-actions]')).toHaveCSS('opacity', '1');
      await page.screenshot({ path: join(output, `${name}-${width}.png`) });
    }
    const touch = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true });
    try {
      await touch.goto(page.url());
      const actions = touch.locator('[data-message-role="user"] [data-message-actions]').first();
      await expect(actions).toHaveCSS('opacity', '1');
      assert((await actions.getByRole('button', { name: 'Copy message', exact: true }).boundingBox()).height >= 44);
    } finally { await touch.close(); }
    checks.push('530/390/320px spacing, keyboard copy, no document overflow; touch copy remains visible with44px target');

    await page.setViewportSize({ width: 1280, height: 900 });
    const live = await run(session, 'polish:live');
    const latest = page.getByRole('button', { name: 'Latest', exact: true });
    const tail = async () => { if (await latest.isVisible()) await latest.click(); };
    await tail();
    const thought = groups.filter({ hasText: 'Thought' });
    await expect(thought).toBeVisible(); const thoughtID = await thought.getAttribute('data-activity-group');
    await expect.poll(() => reading.locator('[data-activity-detail]').filter({ hasText: 'Reasoning begins.' }).textContent()).toMatch(/Reasoning begins/);
    const reasoning = reading.locator('[data-activity-detail]').filter({ hasText: 'Reasoning begins.' });
    assert((await reasoning.textContent()).length > 6000);
    fixture.release('polish-reasoning-more'); await expect(reasoning).toContainText('The complete reasoning tail remains readable.');
    assert.equal(await thought.getAttribute('data-activity-group'), thoughtID);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.screenshot({ path: join(output, `${name}-large-reasoning.png`) });
    fixture.release('polish-execute');
    const active = groups.filter({ hasText: /read 1 file.*called 1 tool/i });
    await expect(active).toBeVisible(); const activeID = await active.getAttribute('data-activity-group');
    const childWork = await run(client.session(child.session.id), 'hold:polish-child-only');
    await eventually(async () => (await client.session(child.session.id).activity(deadline())).active_turn);
    await expect(reading.getByText('hold:polish-child-only', { exact: true })).toHaveCount(0);
    assert.equal(await active.getAttribute('data-activity-group'), activeID);
    fixture.release('polish-child-only'); assert.equal((await childWork.wait(deadline())).turn.state, 'succeeded');
    fixture.release('polish-cell');
    const prose = reading.getByText('I am inspecting the repository.', { exact: true });
    await expect(prose).toBeVisible(); await tail(); await expect(prose).toBeInViewport();
    const activeCopy = () => prose.evaluate(node => {
      for (let row = node.closest('[data-reading-id]'); row; row = row.nextElementSibling) if (row.querySelector('[data-response-actions]')) return true;
      return false;
    });
    assert.equal(await activeCopy(), false); await expect(prose.locator('xpath=ancestor::article')).toHaveCSS('padding-bottom', '8px');
    assert.equal(await active.getAttribute('data-activity-group'), activeID);
    fixture.release('polish-prose-more'); await expect(reading.getByText('The inspection has finished.', { exact: true })).toBeVisible();
    assert.equal(await activeCopy(), false, 'Prose and tool completion do not finish the turn');
    await page.screenshot({ path: join(output, `${name}-live-response.png`) });
    fixture.release('polish-complete'); assert.equal((await live.wait(deadline())).turn.state, 'succeeded');
    await expect(page.locator('[data-current-activity]').getByRole('status')).toHaveText('Idle');
    const completed = reading.locator('[data-message-role="assistant"]').last().locator('xpath=ancestor::div[@data-reading-id]');
    await expect(completed.getByRole('button', { name: 'Copy response', exact: true })).toBeVisible();
    await tail(); await expect(completed.getByRole('button', { name: 'Copy response', exact: true })).toBeInViewport();
    await page.screenshot({ path: join(output, `${name}-response-footer.png`) });
    checks.push('6000+ character cumulative reasoning stays in one group without overflow; child-only stream never appears as root work', 'canonical execution identity survives tool settlement; active response has no copy footer; completed response has one visible copy footer');
    assert.deepEqual(errors, []); assert.deepEqual(csp, []);
    reports.push({ browser: name, rendererDigest: manifest.digest, checks, errors, csp });
    await writeFile(join(output, 'results.json'), JSON.stringify(reports, null, 2));
    console.log(`${name}: ${checks.length} native chat polish groups passed`);
  } catch (error) {
    if (page) { await page.screenshot({ path: join(output, `${name}-failure.png`) }); await writeFile(join(output, `${name}-failure.txt`), await page.locator('body').innerText()); }
    throw error;
  } finally { await browser?.close(); await fixture?.close(); }
}
