import assert from 'node:assert/strict';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { chromium, firefox, _electron, expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';
import { checkComposerReading } from './composer-reading.mjs';

// Production renderer and real queue commands/storage. Only the model is a
// deterministic fixture, with gates before its production steer hook and commit.
const directory = process.env.WHIP_QUEUE_RESULTS ?? '/tmp/whip-composer-queue-results';
const repository = fileURLToPath(new URL('../../../', import.meta.url));
const exec = promisify(execFile);
await mkdir(directory, { recursive: true });
const reports = [];
async function surface(name) {
  if (name !== 'electron') {
    const browser = await ({ chromium, firefox }[name]).launch();
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, recordVideo: { dir: directory } });
    return { page: await context.newPage(), close: async () => { await context.close(); await browser.close(); } };
  }
  const temporary = await mkdtemp('/tmp/whip-queue-desktop-');
  for (const name of ['user', 'home', 'data', 'bin']) await mkdir(join(temporary, name), { mode: 0o700 });
  const binary = join(temporary, 'bin/whipcode');
  await copyFile(join(repository, 'apps/desktop/.stage/native/whipcode'), binary);
  const env = { ...process.env, HOME: join(temporary, 'user'), WHIP_DESKTOP_FIXTURE: '1', WHIPCODE_HOME: join(temporary, 'home'),
    WHIPCODE_NETWORK: '0', WHIP_DESKTOP_USER_DATA: join(temporary, 'data'), WHIP_DESKTOP_EXECUTABLE: binary };
  const app = await _electron.launch({ args: [join(repository, 'apps/desktop/.stage/app')], env, timeout: 30_000, recordVideo: { dir: directory } });
  return { page: await app.firstWindow(), close: async () => {
    await app.close();
    await exec(binary, ['daemon', 'stop'], { env, timeout: 15_000 });
    await rm(temporary, { recursive: true, force: true });
  } };
}

for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  console.log(`${name}: queue fixture`);
  const fixture = await startFixture({ env: { WHIP_WEB_REPL_FIXTURE: '1', WHIP_WEB_INLINE_IMAGES_FIXTURE: '1' },
    ...(name === 'electron' ? { allowedOrigins: ['whip-app://bundle'] } : {}) });
  const browser = await surface(name);
  const { page } = browser;
  page.setDefaultTimeout(15_000);
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientKind: 'human', clientId: `queue-${crypto.randomUUID()}` });
  const root = fixture.info.root_id, errors = [], frames = [], checks = [];
  const origin = name === 'electron' ? 'whip-app://bundle' : fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
  const url = `${origin}/h/${fixture.info.runtime_id}/s/${root}`;
  const input = page.getByRole('textbox', { name: 'Message WHIP', exact: true });
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  const queue = page.getByRole('region', { name: 'Queued messages', exact: true });
  const row = label => queue.locator('[data-queue-row]').filter({ has: page.getByRole('button', { name: `Preview queued message: ${label}`, exact: true }) });
  const screenshot = label => page.screenshot({ path: join(directory, `${name}-${label}.png`) });
  const send = async text => {
    await input.fill(text);
    await page.getByRole('button', { name: 'Queue message', exact: true }).click();
    await expect(input).toHaveValue('');
    await expect(row(text)).toBeVisible();
  };
  page.on('pageerror', error => errors.push(error.message));
  page.on('websocket', socket => socket.on('framesent', ({ payload }) => { try { frames.push(JSON.parse(String(payload))); } catch {} }));
  await page.addInitScript(() => {
    if (!localStorage.getItem('whip.appearance.theme.v1')) localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' }));
  });
  try {
    await client.connect();
    const session = client.session(root);
    if (name === 'electron') {
      const staged = JSON.parse(await readFile(join(repository, 'apps/desktop/.stage/app/renderer-manifest.json'), 'utf8'));
      const web = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
      assert.equal(staged.digest, web.digest, 'Staged desktop must use the tested renderer');
      await page.getByRole('button', { name: 'Manage servers', exact: true }).click();
      await page.getByRole('button', { name: 'Add server', exact: true }).click();
      const hosts = page.getByRole('dialog', { name: 'Add server', exact: true });
      await hosts.getByRole('textbox', { name: /Server name/ }).fill('Queue fixture');
      await hosts.getByRole('textbox', { name: 'Server address', exact: true }).fill(fixture.info.endpoint);
      await hosts.getByRole('button', { name: 'Connect', exact: true }).click();
      await hosts.waitFor({ state: 'hidden' });
    }
    await page.goto(url); await input.waitFor();
    const typing = await checkComposerReading(page);
    checks.push('long-conversation typing remains stable with attachments and in narrow panes');
    const work = session.submit({ text: 'queue:boundary' }); await work.accepted();
    await eventually(async () => (await session.snapshot()).active_turns[root]);
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
    await expect(row('Queue B with images').locator('img')).toHaveJSProperty('complete', true);
    await row('Queue B with images').getByRole('button', { name: /^Preview queued/ }).click();
    const preview = page.getByRole('dialog', { name: 'Queued message', exact: true });
    await expect(preview.locator('img')).toHaveCount(2);
    await expect.poll(() => preview.locator('img').evaluateAll(images => images.every(img => img.naturalWidth > 0))).toBe(true);
    await screenshot('multiple-attachments'); await page.keyboard.press('Escape');
    checks.push('Enter and Send queue once; pending input is absent from transcript; full two-image preview resolves');
    await input.fill('Keep this unsent draft');
    await screenshot('dark');
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'light' })));
    await page.reload(); await expect(queue.locator('[data-queue-row]')).toHaveCount(3);
    await expect(input).toHaveValue('Keep this unsent draft');
    await screenshot('light');
    await page.emulateMedia({ contrast: 'more', reducedMotion: 'reduce' });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.evaluate(() => { document.documentElement.style.fontSize = '20px'; });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await screenshot('narrow-large-contrast-reduced-motion');
    await writeFile(join(directory, `${name}-queue-accessibility.yml`), await queue.ariaSnapshot());
    await page.setViewportSize({ width: 1280, height: 900 }); await page.emulateMedia({ contrast: 'no-preference', reducedMotion: 'no-preference' });
    await page.evaluate(() => { document.documentElement.style.fontSize = ''; });
    checks.push('queue and draft survive reload; light/dark, narrow, enlarged text, contrast and reduced motion inspected');
    const removed = session.submit({ text: 'Remove this queued message' }); await removed.accepted();
    const remove = row('Remove this queued message').getByRole('button', { name: /^Remove queued/ });
    await remove.focus(); await page.keyboard.press('Enter');
    await expect(row('Remove this queued message')).toHaveCount(0);
    assert.equal((await removed.result()).status, 'cancelled');
    await expect(input).toHaveValue('Keep this unsent draft');
    await expect.poll(() => page.evaluate(() => document.activeElement?.hasAttribute('data-queue-action') || document.activeElement?.hasAttribute('data-whip-composer'))).toBe(true);
    checks.push('keyboard removal only cancels waiting input and retains focus and draft');
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
    await fixture.release('queue-finish'); await work.result();
    await expect(queue).toHaveCount(0);
    await eventually(async () => (await fixture.effects()).includes('Queue C'));
    const effects = (await fixture.effects()).filter(value => !value.startsWith('hold:'));
    assert.deepEqual(effects, ['Queue B with images', 'Queue A', 'Queue C']);
    await page.reload(); await input.waitFor();
    await expect(reading.getByText('Queue B with images', { exact: true })).toHaveCount(1);
    await expect(reading.locator('img')).toHaveCount(2);
    checks.push('B steers at the gated production boundary; A/C remain FIFO; history restores B and both images exactly once');
    const held = session.submit({ text: 'hold:queue-reload' }); await held.accepted();
    await eventually(async () => (await session.snapshot()).active_turns[root]);
    await reading.hover(); await page.mouse.wheel(0, -350);
    await expect(page.getByRole('button', { name: 'Latest', exact: true })).toBeVisible();
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
    const anchor = await reading.evaluate(element => element.scrollTop);
    const large = [];
    for (let i = 0; i < 24; i++) large.push(await session.submit({ text: `Queue window ${i + 1}` }).accepted());
    await expect.poll(() => queue.locator('[data-queue-row]').count()).toBeGreaterThan(0);
    await expect.poll(() => queue.locator('[data-queue-row]').count()).toBeLessThan(16);
    await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(anchor, 0);
    await input.focus(); await input.pressSequentially(' more draft', { delay: 20 });
    await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(anchor, 0);
    await queue.locator('ol').evaluate(element => { element.scrollTop = element.scrollHeight; });
    await expect(row('Queue window 24')).toBeVisible();
    await screenshot('virtual-queue-reading-history');
    for (const receipt of large) await session.inbox.remove(root, receipt.ingress_seq).result();
    await expect(queue).toHaveCount(0);
    await expect.poll(() => reading.evaluate(element => element.scrollTop)).toBeCloseTo(anchor, 0);
    checks.push('24-entry queue mounts fewer than 16 rows, scrolls to the last item, and grows/shrinks without moving the history anchor or typing viewport');
    await send('Keep after turn ends');
    await row('Keep after turn ends').getByRole('button', { name: /^Steer queued/ }).click();
    await expect(row('Keep after turn ends')).toContainText('Steering');
    await page.reload(); await expect(row('Keep after turn ends')).toContainText('Steering');
    await fixture.release('queue-reload'); await held.result();
    await eventually(async () => (await fixture.effects()).includes('Keep after turn ends'));
    await expect(queue).toHaveCount(0);
    assert.equal((await fixture.effects()).filter(value => value === 'Keep after turn ends').length, 1);
    checks.push('unconsumed steer survives reload and returns to ordinary queue when its target ends');
    assert.deepEqual(errors, []);
    const controls = frames.filter(frame => frame.method === 'command.submit' && /^inbox\./.test(frame.params.operation));
    assert.equal(new Set(controls.map(frame => frame.params.command_id)).size, controls.length);
    await screenshot('settled');
    reports.push({ name, checks, typing, effects, controls: controls.map(frame => ({ operation: frame.params.operation, params: frame.params.payload })), errors });
  } catch (error) {
    await screenshot('failure').catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n${await page.locator('body').innerText().catch(() => '')}`);
    throw error;
  } finally {
    client.close(); await browser.close(); await fixture.close();
    await writeFile(join(directory, 'report.json'), JSON.stringify(reports, null, 2));
  }
}
console.log(JSON.stringify(reports, null, 2));
