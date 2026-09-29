import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, repository, startFixture } from './native-fixture.mjs';
import { isolateDesktopPerformance, launchDesktopPerformance, finishDesktopPerformance } from './performance-desktop.mjs';
import { checkComposerReading } from './composer-reading.mjs';
import { checkComposerPanels } from './composer-panels.mjs';

// Real native inputs, immutable steering receipts and scoped content. Only the
// HTTP provider is synthetic; gates never inject queue or transcript records.
const directory = process.env.WHIP_QUEUE_RESULTS ?? '/tmp/whip-composer-queue-results';
await mkdir(directory, { recursive: true });
const reports = [];
async function surface(name, fixture) {
  if (name !== 'electron') {
    const browser = await ({ chromium, firefox }[name]).launch();
    try {
      const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
      return { page: await context.newPage(), close: () => browser.close() };
    } catch (error) { await browser.close(); throw error; }
  }
  const isolation = await isolateDesktopPerformance();
  let host;
  try {
    host = await launchDesktopPerformance(fixture, isolation);
    const web = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
    assert.equal(host.rendererDigest, web.digest, 'Staged desktop must use the tested renderer');
    return { page: host.page, close: () => finishDesktopPerformance(isolation, host, undefined, true) };
  } catch (error) { await finishDesktopPerformance(isolation, host, undefined, false, [error]); throw error; }
}

for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  assert(['chromium', 'firefox', 'electron'].includes(name));
  console.log(`${name}: native queue fixture`);
  let fixture, browser, page, queueTail;
  const errors = [], frames = [], checks = [], cspErrors = [];
  try {
    fixture = await startFixture({ queueStreams: true, lifetimeMs: 900_000, managedDirectory: name === 'electron',
      ...(name === 'electron' ? { allowedOrigins: ['whip-app://bundle'] } : {}) });
    const client = await fixture.connect(`queue-${crypto.randomUUID()}`);
    const createdRoot = await fixture.createRoot(client, { title: 'Queue acceptance' });
    const root = createdRoot.root.id, session = client.session(root);
    const submit = async (owner, text) => { const command = owner.submission([{ type: 'text', text }], crypto.randomUUID()); await command.send(deadline()); return command; };
    for (let index = 0; index < 12; index++) {
      const work = await submit(session, `Reading message ${index + 1}. ` + 'Retain the reading anchor while composing. '.repeat(20));
      assert.equal((await work.wait(deadline())).turn.state, 'succeeded');
    }
    const children = [];
    for (let index = 0; index < 3; index++) {
      const child = await session.spawn({ parts: [{ type: 'text', text: `hold:queue-child-${index}` }], overrides: { report_mode: 'message' }, grant_ids: [] }, crypto.randomUUID(), deadline());
      assert(child.session); children.push(client.session(child.session.id));
    }
    const manifest = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
    for (const [path, file] of Object.entries(manifest.files)) {
      const response = await fetch(`${fixture.info.web}/${path}`); assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256);
    }
    browser = await surface(name, fixture); page = browser.page; page.setDefaultTimeout(15_000);
    const origin = name === 'electron' ? 'whip-app://bundle' : fixture.info.web;
    const url = `${origin}/h/${fixture.info.runtime_id}/s/${root}`;
    const input = page.getByRole('textbox', { name: 'Message WHIP', exact: true });
    const reading = page.getByRole('region', { name: 'Conversation', exact: true });
    const queue = page.getByRole('region', { name: 'Queued messages', exact: true });
    const row = label => queue.locator('[data-queue-row]').filter({ has: page.getByRole('button', { name: `Preview queued message: ${label}`, exact: true }) });
    const form = input.locator('xpath=ancestor::form');
    const dock = form.locator('[data-agent-dock]');
    const disclosure = dock.getByRole('button', { name: /^Agents/ });
    const stacked = async () => {
      await expect.poll(() => form.evaluate(element => {
        const bounds = element.querySelector('[data-whip-composer]').parentElement.getBoundingClientRect();
        return bounds.bottom <= innerHeight && bounds.right <= innerWidth;
      })).toBe(true);
      const geometry = await checkComposerPanels(form);
      assert.deepEqual(geometry.panels.map(panel => panel.kind), ['agents', 'queue']);
      assert.ok(geometry.composer.bottom <= geometry.viewport, `Stack keeps the composer in view: ${JSON.stringify(geometry)}`);
      assert.ok(geometry.composer.top - geometry.panels[0].top < geometry.viewport * 0.5, 'Combined panels stay below half the viewport');
      return geometry;
    };
    const screenshot = label => page.screenshot({ path: join(directory, `${name}-${label}.png`) });
    const send = async text => {
      await input.fill(text);
      await page.getByRole('button', { name: 'Queue message', exact: true }).click();
      await expect(input).toHaveValue('');
      await expect(row(text)).toBeVisible();
    };
    page.on('pageerror', error => { if (errors.length < 64) errors.push(error.message.slice(0, 2048)); });
    page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
      try { const frame = JSON.parse(String(payload)); if (['inputs.steer', 'inputs.cancel', 'sessions.submit'].includes(frame.method)) {
        assert(frames.length < 4096); frames.push({ method: frame.method, params: { session_id: frame.params.session_id,
          input_id: frame.params.input_id, turn_id: frame.params.turn_id, edit_id: frame.params.edit_id, identity: frame.params.identity } });
      } } catch (error) { if (errors.length < 64) errors.push(error.message); }
    }));
    await page.exposeFunction('recordQueueCSP', directive => { if (cspErrors.length < 64) cspErrors.push(String(directive)); });
    await page.addInitScript(() => {
      document.addEventListener('securitypolicyviolation', event => void window.recordQueueCSP(event.violatedDirective));
      if (!localStorage.getItem('whip.appearance.theme.v1')) localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' }));
    });
    await page.goto(url); await input.waitFor();
    const typing = await checkComposerReading(page);
    checks.push('long-conversation typing remains stable with attachments and in narrow panes');
    const work = await submit(session, 'queue:boundary');
    const activeTurn = await eventually(async () => (await work.check(deadline())).evidence?.turn);
    await eventually(async () => (await fixture.effects()).includes('queue:boundary'));
    await expect(page.getByRole('button', { name: /Pause this turn/ })).toBeVisible();
    await send('Queue A');
    const urls = await page.evaluate(() => [0, 1].map(i => {
      const canvas = document.createElement('canvas'); canvas.width = 240; canvas.height = 180;
      const ctx = canvas.getContext('2d'); ctx.fillStyle = i ? '#456347' : '#324c62'; ctx.fillRect(0, 0, 240, 180);
      ctx.fillStyle = '#fff'; ctx.font = '22px sans-serif'; ctx.fillText(`Queue image ${i + 1}`, 20, 70);
      return canvas.toDataURL('image/png');
    }));
    await page.locator('input[type=file]').setInputFiles(urls.map((url, i) => ({ name: `queue-image-${i + 1}.png`, mimeType: 'image/png', buffer: Buffer.from(url.split(',')[1], 'base64') })));
    await expect(page.locator('[data-composer-attachments]').getByRole('img', { name: /^Uploading/ })).toHaveCount(0);
    await send('Queue B with images');
    await input.fill('Queue C'); await input.press('Enter');
    await expect(row('Queue C')).toBeVisible();
    await expect(queue.locator('[data-queue-row]')).toHaveCount(3);
    await expect(reading.getByText('Queue A', { exact: true })).toHaveCount(0);
    // Canonical queue metadata carries an attachment count. Full bytes are
    // loaded only by the explicit full-message preview below.
    await expect(row('Queue B with images')).toContainText('2');
    await row('Queue B with images').getByRole('button', { name: /^Preview queued/ }).click();
    const preview = page.getByRole('dialog', { name: 'Queued message', exact: true });
    await expect(preview.locator('img')).toHaveCount(2);
    await expect.poll(() => preview.locator('img').evaluateAll(images => images.every(img => img.naturalWidth > 0))).toBe(true);
    await screenshot('multiple-attachments'); await page.keyboard.press('Escape');
    checks.push('Enter and Send queue once; pending input is absent from transcript; full two-image preview resolves');
    await input.fill('Keep this unsent draft');
    await stacked();
    await screenshot('dark-collapsed-agents');
    await disclosure.click();
    await stacked();
    await screenshot('dark');
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'light' })));
    await page.reload(); await expect(queue.locator('[data-queue-row]')).toHaveCount(3);
    await expect(input).toHaveValue('Keep this unsent draft');
    await disclosure.click();
    await stacked();
    await screenshot('light');
    await page.emulateMedia({ contrast: 'more', reducedMotion: 'reduce' });
    await page.setViewportSize({ width: 320, height: 700 });
    await page.evaluate(() => { document.documentElement.style.fontSize = '20px'; });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await stacked();
    await screenshot('narrow-large-contrast-reduced-motion');
    await writeFile(join(directory, `${name}-queue-accessibility.yml`), await queue.ariaSnapshot());
    await page.setViewportSize({ width: 1280, height: 900 }); await page.emulateMedia({ contrast: 'no-preference', reducedMotion: 'no-preference' });
    await page.evaluate(() => { document.documentElement.style.fontSize = ''; });
    checks.push('queue and draft survive reload; light/dark, narrow, enlarged text, contrast and reduced motion inspected');
    const removed = await submit(session, 'Remove this queued message');
    const remove = row('Remove this queued message').getByRole('button', { name: /^Remove queued/ });
    await remove.focus(); await page.keyboard.press('Enter');
    await expect(row('Remove this queued message')).toHaveCount(0);
    assert.equal((await removed.check(deadline())).evidence.input.state, 'cancelled');
    await expect(input).toHaveValue('Keep this unsent draft');
    await expect.poll(() => page.evaluate(() => document.activeElement?.hasAttribute('data-queue-action') || document.activeElement?.hasAttribute('data-whip-composer'))).toBe(true);
    checks.push('keyboard removal only cancels waiting input and retains focus and draft');
    const pendingB = (await session.inputs.page({ state: 'queued' }, deadline())).items.find(item => item.text_preview === 'Queue B with images');
    assert(pendingB);
    const capturedB = await session.inputs.get(pendingB.id, deadline());
    const childPending = await submit(children[0], 'Child input stays with its owner');
    const childInput = (await childPending.check(deadline())).evidence.input;
    await assert.rejects(session.inputs.get(childInput.id, deadline()), error => error.kind === 'NOT_FOUND');
    await assert.rejects(session.inputs.steer(childInput.id, activeTurn.id, crypto.randomUUID(), deadline()), error => error.kind === 'NOT_FOUND');
    await assert.rejects(children[0].inputs.steer(pendingB.id, activeTurn.id, crypto.randomUUID(), deadline()), error => error.kind === 'NOT_FOUND');
    assert.equal((await children[0].inputs.get(childInput.id, deadline())).state, 'queued');
    await children[0].inputs.cancel(childInput.id, deadline());
    await row('Queue B with images').getByRole('button', { name: /^Steer queued/ }).click();
    await expect(row('Queue B with images')).toContainText('Steering');
    await fixture.release('queue-boundary');
    await expect(queue.locator('[data-queue-row]')).toHaveCount(2);
    await expect(reading.getByText('After the queue boundary.', { exact: true })).toBeVisible();
    await expect(reading.locator('img')).toHaveCount(2);
    const text = await reading.innerText();
    assert(text.indexOf('Before the queue boundary.') < text.indexOf('Queue B with images'));
    assert(text.indexOf('Queue B with images') < text.indexOf('After the queue boundary.'));
    await screenshot('steer-boundary');
    await page.reload();
    await expect(reading.getByText('After the queue boundary.', { exact: true })).toBeVisible();
    const reopened = await reading.innerText();
    assert(reopened.indexOf('Before the queue boundary.') < reopened.indexOf('Queue B with images'));
    assert(reopened.indexOf('Queue B with images') < reopened.indexOf('After the queue boundary.'));
    await expect(reading.locator('img')).toHaveCount(2);
    const consumedB = await session.inputs.get(pendingB.id, deadline());
    assert.equal(consumedB.turn_id, activeTurn.id); assert.equal(consumedB.steering.consumed, true);
    assert.deepEqual(consumedB.parts, capturedB.parts);
    const refs = consumedB.parts.filter(part => part.type === 'content').map(part => part.reference_id);
    for (let index = 0; index < refs.length; index++) assert.deepEqual(Buffer.from(await session.content.readBytes(refs[index], { maxBytes: 4 << 20, ...deadline() })), Buffer.from(urls[index].split(',')[1], 'base64'));
    await fixture.release('queue-finish'); await work.wait(deadline());
    await expect(queue).toHaveCount(0);
    await eventually(async () => (await fixture.effects()).includes('Queue C'));
    const effects = (await fixture.effects()).filter(value => ['Queue B with images', 'Queue A', 'Queue C'].includes(value));
    assert.deepEqual(effects, ['Queue B with images', 'Queue A', 'Queue C']);
    await page.reload(); await input.waitFor();
    await expect(reading.getByText('Queue B with images', { exact: true })).toHaveCount(1);
    await expect(reading.locator('img')).toHaveCount(2);
    checks.push('B steers at the gated production boundary; A/C remain FIFO; history restores B and both images exactly once');
    const held = await submit(session, 'hold:queue-reload');
    const heldTurn = await eventually(async () => (await held.check(deadline())).evidence?.turn);
    await eventually(async () => (await fixture.effects()).includes('hold:queue-reload'));
    await reading.hover(); await page.mouse.wheel(0, -350);
    await expect(page.getByRole('button', { name: 'Latest', exact: true })).toBeVisible();
    let previousTop, stableSince = Date.now();
    await expect.poll(async () => { const top = await reading.evaluate(element => element.scrollTop); if (top !== previousTop) stableSince = Date.now(); previousTop = top; return Date.now() - stableSince; }).toBeGreaterThan(250);
    const anchor = await reading.evaluate(element => element.scrollTop);
    const large = [];
    for (let i = 0; i < 24; i++) { const command = await submit(session, `Queue window ${i + 1}`); large.push((await command.check(deadline())).evidence.input); }
    await expect.poll(() => queue.locator('[data-queue-row]').count()).toBeGreaterThan(0);
    await expect.poll(() => queue.locator('[data-queue-row]').count()).toBeLessThan(16);
    await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(anchor, 0);
    await input.focus(); await input.pressSequentially(' more draft', { delay: 20 });
    await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(anchor, 0);
    // Admission ACKs precede the observed input page. Scroll only after all 24
    // canonical rows are known, otherwise later arrivals extend the old bottom
    // while their overscan rows already satisfy Playwright's toBeVisible().
    const observedBeforeWait = await queue.locator('[data-queue-row]').first().getAttribute('aria-setsize');
    await expect(queue.locator('[data-queue-row]').first()).toHaveAttribute('aria-setsize', String(large.length));
    const beforeTailScroll = await queue.locator('ol').evaluate(element => ({
      observed: element.querySelector('[data-queue-row]')?.getAttribute('aria-setsize'),
      scrollTop: element.scrollTop, scrollHeight: element.scrollHeight, clientHeight: element.clientHeight,
    }));
    await queue.locator('ol').evaluate(element => { element.scrollTop = element.scrollHeight; });
    await expect(row('Queue window 24')).toBeVisible();
    if (await disclosure.getAttribute('aria-expanded') === 'false') await disclosure.click();
    await stacked();
    queueTail = { observedBeforeWait, before: beforeTailScroll, after: await row('Queue window 24').evaluate(element => {
      const list = element.closest('ol'), bounds = element.getBoundingClientRect(), viewport = list.getBoundingClientRect();
      return { observed: element.getAttribute('aria-setsize'), row: bounds.toJSON(), viewport: viewport.toJSON(),
        scrollTop: list.scrollTop, scrollHeight: list.scrollHeight, clientHeight: list.clientHeight,
        hit: document.elementFromPoint(bounds.left + bounds.width / 2, bounds.bottom - 2)?.closest('[data-queue-row]') === element };
    }) };
    assert.ok(queueTail.after.row.bottom <= queueTail.after.viewport.bottom + 1, `Last queued row is fully visible: ${JSON.stringify(queueTail)}`);
    assert.ok(queueTail.after.hit, `Last queued row remains hit-testable above composer: ${JSON.stringify(queueTail)}`);
    await screenshot('virtual-queue-reading-history');
    for (const item of large) await session.inputs.cancel(item.id, deadline());
    await expect(queue).toHaveCount(0);
    await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(anchor, 0);
    checks.push('24-entry queue mounts fewer than 16 rows, scrolls to the last item, and grows/shrinks without moving the history anchor or typing viewport');
    await send('Keep after turn ends');
    await row('Keep after turn ends').getByRole('button', { name: /^Steer queued/ }).click();
    await expect(row('Keep after turn ends')).toContainText('Steering');
    await page.reload(); await expect(row('Keep after turn ends')).toContainText('Steering');
    // A native final provider response is itself a steering boundary. Cancel
    // this held turn to prove fallback when its exact target ends unconsumed.
    await session.cancelTurn(heldTurn.id, deadline());
    assert.equal((await held.wait(deadline())).turn.state, 'cancelled');
    await eventually(async () => (await fixture.effects()).includes('Keep after turn ends'));
    await expect(queue).toHaveCount(0);
    assert.equal((await fixture.effects()).filter(value => value === 'Keep after turn ends').length, 1);
    checks.push('unconsumed steer survives reload and returns to ordinary queue when its exact target is cancelled; foreign child inputs cannot be read or steered through the root');
    assert.deepEqual(errors, []);
    const controls = frames.filter(frame => frame.method === 'inputs.steer');
    if (name !== 'electron') assert.equal(controls.length, 2);
    assert.equal(new Set(controls.map(frame => frame.params.edit_id)).size, controls.length);
    assert(controls.every(frame => frame.params.session_id === root));
    assert.deepEqual(cspErrors, []);
    await screenshot('settled');
    const { root: solo } = await fixture.createRoot(client, { title: 'Queue without children' });
    const soloRoot = solo.id, soloSession = client.session(soloRoot);
    const soloWork = await submit(soloSession, 'hold:queue-solo');
    await eventually(async () => (await soloWork.check(deadline())).evidence?.turn);
    const soloMessage = await submit(soloSession, 'This queue has no agents');
    await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${soloRoot}`);
    await expect(queue).toBeVisible();
    await expect(dock).toHaveCount(0);
    assert.deepEqual((await checkComposerPanels(form)).panels.map(panel => panel.kind), ['queue']);
    await screenshot('queue-only');
    await soloSession.inputs.cancel((await soloMessage.check(deadline())).evidence.input.id, deadline());
    await fixture.release('queue-solo'); await soloWork.wait(deadline());
    await expect(queue).toHaveCount(0);
    assert.equal((await checkComposerPanels(form)).panels.length, 0);
    await screenshot('composer-only');
    checks.push('agents and queue stack in collapsed/expanded dark/light and 320px large-type states; queue-only and composer-only leave no empty surface');
    reports.push({ name, rendererDigest: manifest.digest, checks, typing, effects, controls, queueTail, errors, cspErrors });
  } catch (error) {
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n${JSON.stringify({ queueTail })}\n${await page?.locator('body').innerText().catch(() => '')}
${fixture?.output ?? ''}`);
    throw error;
  } finally {
    try { await browser?.close(); } finally { await fixture?.close(); }
    await writeFile(join(directory, 'report.json'), JSON.stringify(reports, null, 2));
  }
}
console.log(JSON.stringify(reports, null, 2));
