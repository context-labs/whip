import assert from 'node:assert/strict';
import { join } from 'node:path';
import { expect } from '@playwright/test';
import { checkComposerReading } from './composer-reading.mjs';
import { checkChatFileDrop, checkDropOverlay } from './chat-file-drop.mjs';

// Real local files -> shared composer -> scoped upload -> admitted transcript.
export async function checkComposerAttachments({ page, directory, name }) {
  const input = page.getByLabel('Message WHIP', { exact: true });
  const form = input.locator('xpath=ancestor::form');
  const strip = form.getByRole('group', { name: 'Message attachments', exact: true });
  const upload = form.locator('input[type=file]');
  const send = form.getByRole('button', { name: 'Send message', exact: true });
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  const screenshot = label => page.screenshot({ path: join(directory, `${name}-composer-${label}.png`) });
  const urls = await page.evaluate(() => [0, 1, 2].map(i => {
    const canvas = document.createElement('canvas'); canvas.width = 480; canvas.height = 320 + i * 160;
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = ['#324c62', '#456347', '#63506c'][i]; ctx.fillRect(0, 0, canvas.width, canvas.height);
    ctx.fillStyle = '#fff'; ctx.font = '28px sans-serif';
    ctx.fillText(`Attached image ${i + 1}`, 32, 70);
    for (let row = 0; row < 5; row++) { ctx.fillStyle = `rgba(255,255,255,${0.1 + row * 0.06})`; ctx.fillRect(32, 110 + row * 36, 300 - row * 20, 18); }
    return canvas.toDataURL('image/png');
  }));
  const files = urls.map((url, i) => ({ name: `image-${i + 1}.png`, mimeType: 'image/png', buffer: Buffer.from(url.split(',')[1], 'base64') }));
  const dropChecks = await checkChatFileDrop({ page, files, directory, name });
  await page.evaluate(() => {
    window.composerPreviewURLs = new Set();
    const create = URL.createObjectURL, revoke = URL.revokeObjectURL;
    URL.createObjectURL = file => { const url = create(file); window.composerPreviewURLs.add(url); return url; };
    URL.revokeObjectURL = url => { window.composerPreviewURLs.delete(url); revoke(url); };
    window.holdComposerImages = true;
    const complete = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'complete');
    Object.defineProperty(HTMLImageElement.prototype, 'complete', { ...complete, get() {
      return window.holdComposerImages && this.src.startsWith('blob:') ? false : complete.get.call(this);
    } });
    const listen = HTMLImageElement.prototype.addEventListener;
    HTMLImageElement.prototype.addEventListener = function(type, listener, options) {
      return listen.call(this, type, type === 'load' ? function(event) {
        if (!window.holdComposerImages || !this.src.startsWith('blob:')) listener.call(this, event);
      } : listener, options);
    };
  });
  let release;
  const delayed = new Promise(resolve => { release = resolve; });
  const pattern = '**/api/v3/content/upload?*';
  await page.route(pattern, async route => { await delayed; await route.continue(); });
  await upload.setInputFiles(files);
  await expect(strip.getByRole('button', { name: /^Preview image-/ })).toHaveCount(3);
  await expect(strip.getByRole('img', { name: /^Loading preview/ })).toHaveCount(3);
  await expect(send).toBeDisabled();
  await screenshot('loading');
  const height = await form.evaluate(element => element.getBoundingClientRect().height);
  await expect.poll(() => strip.locator('img').evaluateAll(images => images.every(img => img.naturalWidth > 0))).toBe(true);
  await page.evaluate(() => {
    window.holdComposerImages = false;
    document.querySelectorAll('img[src^="blob:"]').forEach(img => img.dispatchEvent(new Event('load')));
  });
  await expect(strip.getByRole('img', { name: /^Loading preview/ })).toHaveCount(0);
  await expect(strip.getByRole('img', { name: /^Uploading/ })).toHaveCount(3);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  assert.equal(await strip.getByRole('img', { name: 'Uploading image-1.png' }).evaluate(el => getComputedStyle(el).animationName), 'none');
  await screenshot('uploading');
  release();
  await expect(send).toBeEnabled();
  await expect(strip.getByRole('img', { name: /^Uploading/ })).toHaveCount(0);
  assert.equal(await form.evaluate(element => element.getBoundingClientRect().height), height, 'Decoding and upload settlement must not resize the composer');
  await page.unroute(pattern);
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  await input.fill('Please compare these three images.');
  await screenshot('ready');
  assert.deepEqual(await page.getByRole('alert').allTextContents(), [], 'The attachment fixture must have a healthy desktop overlay bridge');
  const preview = strip.getByRole('button', { name: 'Preview image-2.png', exact: true });
  await preview.focus(); await page.keyboard.press('Enter');
  await expect(page.getByRole('dialog', { name: 'image-2.png', exact: true })).toBeVisible();
  await screenshot('original');
  await page.keyboard.press('Escape'); await expect(preview).toBeFocused();
  assert.equal(await page.evaluate(() => window.composerPreviewURLs.size), 3);
  const sources = await strip.locator('img').evaluateAll(images => images.map(img => img.src));
  await page.getByRole('button', { name: 'REPL', exact: true }).click();
  await expect(page.getByRole('region', { name: 'REPL executions', exact: true })).toBeVisible();
  await page.goBack();
  await expect(strip.getByRole('button', { name: /^Preview image-/ })).toHaveCount(3);
  assert.deepEqual(await strip.locator('img').evaluateAll(images => images.map(img => img.src)), sources, 'Remounts must keep the same draft-owned previews');
  assert.equal(await page.evaluate(() => window.composerPreviewURLs.size), 3);
  const readingChecks = await checkComposerReading(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await input.fill('Compare the attached images.');
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  const tiles = await strip.getByRole('button', { name: /^Preview image-/ }).evaluateAll(elements => elements.map(el => el.getBoundingClientRect().toJSON()));
  assert.ok(tiles.every(tile => tile.width === 120 && tile.height === 120));
  assert.ok(tiles[2].top > tiles[0].top, 'Multiple images should wrap in a narrow composer');
  await expect(send).toBeInViewport();
  await screenshot('narrow');
  await page.setViewportSize({ width: 1280, height: 900 });
  await strip.getByRole('button', { name: 'Remove image-2.png', exact: true }).click();
  assert.equal(await page.evaluate(() => window.composerPreviewURLs.size), 2);
  await input.fill('Composer attachment acceptance check.');
  await send.click();
  await expect(strip).toHaveCount(0);
  await expect(input).toHaveValue('');
  assert.equal(await page.evaluate(() => window.composerPreviewURLs.size), 0);
  const sent = reading.locator('[data-message-role="user"]').filter({ hasText: 'Composer attachment acceptance check.' });
  const latest = page.getByRole('button', { name: 'Latest', exact: true });
  if (await latest.isVisible()) await latest.click();
  await expect(sent.getByRole('button', { name: /^Open image/ })).toHaveCount(2);
  await expect(send).toBeVisible();

  // Failed transport and undecodable local files remain removable and distinct.
  await page.route(pattern, route => route.fulfill({ status: 503, body: 'Fixture upload failure' }));
  await upload.setInputFiles(files[0]);
  await expect(strip.getByText('Upload failed', { exact: true })).toBeVisible();
  await expect(strip.locator('img')).toBeVisible();
  await expect(send).toBeDisabled();
  await screenshot('upload-failure');
  await strip.getByRole('button', { name: 'Remove image-1.png', exact: true }).click();
  await page.unroute(pattern);
  await upload.setInputFiles({ name: 'broken.png', mimeType: 'image/png', buffer: Buffer.from('not an image') });
  await expect(strip.getByText('Preview unavailable', { exact: true })).toBeVisible();
  await expect(send).toBeEnabled();
  await expect(strip.getByText('Upload failed', { exact: true })).toHaveCount(0);
  await strip.getByRole('button', { name: 'Remove broken.png', exact: true }).click();
  assert.equal(await page.evaluate(() => window.composerPreviewURLs.size), 0);

  // Light/high-contrast/large-text rendering uses the normal saved preferences.
  await page.evaluate(() => {
    localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'light' }));
    localStorage.setItem('whip.appearance.display.v1', JSON.stringify({ version: 1, display: {
      uiFont: 'system', codeFont: 'system', uiSize: 20, codeSize: 24, wrapCode: true, contrast: 'more', motion: 'reduce',
    } }));
  });
  await page.reload(); await expect(input).toBeVisible();
  await upload.setInputFiles(files);
  await expect(send).toBeEnabled();
  await input.fill('Previewing multiple images.');
  await screenshot('light-large-text');
  await page.setViewportSize({ width: 390, height: 844 });
  await checkDropOverlay({ page, target: input, files, directory, name: `${name}-light-large-narrow` });
  await page.setViewportSize({ width: 1280, height: 900 });
  // Many attachments are bounded independently of textarea growth.
  await upload.setInputFiles(Array.from({ length: 13 }, (_, i) => ({ ...files[i % 3], name: `extra-${i}.png` })));
  await expect(send).toBeEnabled();
  await expect(strip.getByRole('button', { name: /^Preview/ })).toHaveCount(16);
  assert.ok(await strip.evaluate(el => el.clientHeight <= 264 && el.scrollHeight > el.clientHeight));
  await expect(send).toBeInViewport();
  await screenshot('many');
  await strip.getByRole('button', { name: 'Remove extra-12.png', exact: true }).click();
  for (const url of urls) {
    // Exercise pasted and dropped image Files through the same attachment owner.
    await strip.getByRole('button', { name: /^Remove/ }).first().click();
    await input.evaluate((el, { url, drop }) => {
      const bytes = Uint8Array.from(atob(url.split(',')[1]), char => char.charCodeAt(0));
      const data = new DataTransfer(); data.items.add(new File([bytes], drop ? 'dropped.png' : 'pasted.png', { type: 'image/png' }));
      const event = drop ? new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: data })
        : new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: data });
      // Firefox discards files in synthetic ClipboardEventInit. Supply the
      // clipboard payload explicitly; this exercises the handler, not OS paste.
      if (!drop) Object.defineProperty(event, 'clipboardData', { value: data });
      el.dispatchEvent(event);
    }, { url, drop: url === urls[1] });
    await expect(send).toBeEnabled();
  }
  await expect(strip.getByRole('button', { name: 'Preview dropped.png', exact: true })).toHaveCount(1);
  await expect(strip.getByRole('button', { name: 'Preview pasted.png', exact: true })).toHaveCount(2);
  while (await strip.getByRole('button', { name: /^Remove/ }).count()) await strip.getByRole('button', { name: /^Remove/ }).first().click();
  await input.fill('');
  assert.deepEqual(await page.evaluate(() => window.cspErrors), []);
  await page.evaluate(() => {
    localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' }));
    localStorage.removeItem('whip.appearance.display.v1');
  });
  await page.reload(); await expect(input).toBeVisible();
  return { images: 3, maximumAttachments: 16, readingChecks, dropChecks };
}
