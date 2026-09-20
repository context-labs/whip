import assert from 'node:assert/strict';
import { join } from 'node:path';
import { expect } from '@playwright/test';

// Real upload -> durable transcript -> bounded history handle -> scoped content
// transfer. Run against the isolated chat fixture, never the developer's daemon.
export async function checkStoredMessages({ page, client, root, directory, name }) {
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  const dataURL = await page.evaluate(() => {
    const canvas = document.createElement('canvas'); canvas.width = 640; canvas.height = 360;
    const context = canvas.getContext('2d'); const pixels = context.createImageData(640, 360);
    let seed = 12345;
    for (let i = 0; i < pixels.data.length; i += 4) {
      seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
      pixels.data[i] = seed & 255; pixels.data[i + 1] = seed >>> 8 & 255; pixels.data[i + 2] = seed >>> 16 & 255; pixels.data[i + 3] = 255;
    }
    context.putImageData(pixels, 0, 0); return canvas.toDataURL('image/png');
  });
  assert.ok(dataURL.length > 256 * 1024);
  const extraImages = await page.evaluate(() => [[320, 640], [800, 240], [240, 240], [480, 320]].map(([width, height], i) => {
    const canvas = document.createElement('canvas'); canvas.width = width; canvas.height = height;
    const context = canvas.getContext('2d');
    context.fillStyle = ['#2b5475', '#734b69', '#48684b', '#846439'][i]; context.fillRect(0, 0, width, height);
    context.fillStyle = '#fff'; context.font = '32px sans-serif'; context.textAlign = 'center';
    context.fillText(`Image ${i + 2}`, width / 2, height / 2); return canvas.toDataURL('image/png');
  }));
  const images = await Promise.all([dataURL, ...extraImages].map(url => client.upload(Buffer.from(url.split(',')[1], 'base64'), { rootId: root, agentId: root, mediaType: 'image/png' })));
  const requests = [];
  page.on('request', request => { if (request.method() === 'GET' && request.url().includes('/api/v3/content/')) requests.push(request.url()); });
  const session = client.session(root);
  for (let i = 0; i < 3; i++) await session.submit({ text: `Screenshot message ${i}. Please inspect these images.`,
    attachments: images.slice(0, i * 2 + 1).map((image, index) => image.asAttachment('image', `screenshot-${index + 1}.png`)) }).result();
  // Hold completion notifications to capture the real thumbnail loading UI,
  // independently of how quickly these local data URLs decode on each engine.
  await page.addInitScript(() => {
    window.holdAttachmentLoads = !sessionStorage.getItem('attachment-loading-checked');
    const complete = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'complete');
    Object.defineProperty(HTMLImageElement.prototype, 'complete', { ...complete, get() {
      return window.holdAttachmentLoads && this.alt === 'Attached image' ? false : complete.get.call(this);
    } });
    const listen = HTMLImageElement.prototype.addEventListener;
    HTMLImageElement.prototype.addEventListener = function(type, listener, options) {
      // React can receive loads on detached image nodes before they have a
      // document event path, so intercept the image listener itself.
      return listen.call(this, type, type === 'load' ? function(event) {
        if (!window.holdAttachmentLoads || this.alt !== 'Attached image') listener.call(this, event);
      } : listener, options);
    };
  });
  await page.reload();
  const latest = reading.locator('[data-message-role="user"]').filter({ hasText: 'Screenshot message 2.' });
  const loadRecentInput = async () => {
    await expect(reading).toBeVisible();
    const earlier = reading.getByRole('button', { name: 'Load earlier messages', exact: true });
    if (!(await reading.locator('[data-message-role="user"]').count()) && await earlier.isVisible()) await earlier.click();
  };
  await loadRecentInput();
  await expect(latest).toBeVisible();
  await expect(latest.getByRole('button', { name: /^Open image/ })).toHaveCount(5);
  const spinner = latest.getByRole('img', { name: 'Loading image 1 of 5' });
  await expect(spinner).toBeVisible();
  const thumbnailHeight = await latest.evaluate(element => element.getBoundingClientRect().height);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  assert.equal(await spinner.evaluate(element => getComputedStyle(element).animationName), 'none');
  await page.screenshot({ path: join(directory, `${name}-attachment-loading.png`) });
  await page.evaluate(() => {
    window.holdAttachmentLoads = false; sessionStorage.setItem('attachment-loading-checked', '1');
    document.querySelectorAll('img[alt="Attached image"]').forEach(img => {
      if (img.complete) img.dispatchEvent(new Event('load'));
    });
  });
  await expect.poll(() => latest.locator('img').first().evaluate(img => img.complete && img.naturalWidth === 640 && img.naturalHeight === 360)).toBe(true);
  await expect(latest.getByRole('img', { name: /^Loading image/ })).toHaveCount(0);
  assert.equal(await latest.evaluate(element => element.getBoundingClientRect().height), thumbnailHeight, 'Decoding thumbnails must not resize the row');
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const previewTrigger = latest.getByRole('button', { name: 'Open image 2 of 5', exact: true });
  await previewTrigger.focus(); await page.keyboard.press('Enter');
  const preview = page.getByRole('dialog', { name: 'Image 2 of 5', exact: true });
  await expect(preview).toBeVisible();
  await expect.poll(() => preview.locator('img').evaluate(img => img.complete && img.naturalWidth === 320 && img.naturalHeight === 640)).toBe(true);
  await page.screenshot({ path: join(directory, `${name}-attachment-preview.png`) });
  await page.keyboard.press('Escape'); await expect(preview).toBeHidden(); await expect(previewTrigger).toBeFocused();
  await expect(reading.getByText(/Read stored message|View image attachment/)).toHaveCount(0);
  await page.screenshot({ path: join(directory, `${name}-stored-message-inline.png`) });
  assert.ok(requests.length > 0, 'Large historical messages must exercise scoped body reads');
  for (const request of requests) {
    const url = new URL(request); assert.equal(url.searchParams.get('root_id'), root); assert.equal(url.searchParams.get('agent_id'), root);
  }
  // Explicitly exercise an interrupted transfer and in-place retry.
  let failed = false;
  await page.route('**/api/v3/content/*?*', async route => {
    if (!failed && route.request().method() === 'GET') { failed = true; await route.fulfill({ status: 503, body: 'Synthetic unavailable content' }); }
    else await route.continue();
  });
  await page.reload(); await loadRecentInput();
  const retry = reading.getByRole('button', { name: 'Retry', exact: true });
  await expect(retry).toBeVisible(); await retry.click();
  await expect(reading.getByText('Could not load message', { exact: true })).toHaveCount(0);
  await expect(latest).toBeVisible();
  await page.unroute('**/api/v3/content/*?*');
  // Push the attachments out of the mounted window and then revisit history.
  for (let i = 0; i < 38; i++) await session.submit({ text: `Later message ${i}` }).result();
  await page.reload(); await expect(reading).toBeVisible();
  await reading.hover(); await page.mouse.wheel(0, -1);
  for (let i = 0; i < 80 && !(await latest.isVisible()); i++) {
    await reading.evaluate(element => { element.scrollTop = Math.max(0, element.scrollTop - element.clientHeight * 0.8); });
    await page.waitForTimeout(80);
    const earlier = reading.getByRole('button', { name: 'Load earlier messages', exact: true });
    if (await earlier.isVisible() && await earlier.isEnabled()) await earlier.click();
  }
  await expect(latest).toBeVisible();
  await expect.poll(() => latest.locator('img').first().evaluate(img => img.complete && img.naturalWidth === 640)).toBe(true);
  const count = await reading.locator('[data-reading-id]').count();
  assert.ok(count < 80, `${count} mounted rows exceeds the normal bound`);
  await page.screenshot({ path: join(directory, `${name}-stored-message-history.png`) });
  // Reopen history over a slow connection. Growing an image above
  // the reader must preserve the following response's position and selection.
  const anchorId = await latest.evaluate(element => element.closest('[data-reading-id]').nextElementSibling.dataset.readingId);
  const anchorPosition = () => reading.evaluate((element, id) => {
    const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.dataset.readingId === id);
    return row?.getBoundingClientRect().top - element.getBoundingClientRect().top;
  }, anchorId);
  await reading.evaluate(element => { element.scrollTop = element.scrollHeight; });
  await expect(latest).toHaveCount(0);
  let release; const held = new Promise(resolve => { release = resolve; });
  await page.route('**/api/v3/content/*?*', async route => { await held; await route.continue(); });
  // A fresh view guarantees a cold content read independently of background
  // timer throttling while the previous Query cache is being disposed.
  await page.reload(); await expect(reading).toBeVisible();
  for (let i = 0; i < 80 && !(await reading.getByText('Loading message…', { exact: true }).count()); i++) {
    await reading.evaluate(element => { element.scrollTop = Math.max(0, element.scrollTop - element.clientHeight * 0.6); });
    await page.waitForTimeout(40);
    const earlier = reading.getByRole('button', { name: 'Load earlier messages', exact: true });
    if (await earlier.isVisible() && await earlier.isEnabled()) await earlier.click();
  }
  await expect(reading.getByText('Loading message…', { exact: true }).first()).toBeVisible();
  // Let rows entering the virtual window finish measurement before selecting.
  await expect.poll(async () => {
    await reading.evaluate((element, id) => {
      const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.dataset.readingId === id);
      element.scrollTop += row.getBoundingClientRect().top - element.getBoundingClientRect().top - 40;
    }, anchorId);
    await page.waitForTimeout(100);
    return anchorPosition();
  }).toBeCloseTo(40, 0);
  const selection = await reading.evaluate((element, id) => {
    const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.dataset.readingId === id);
    const text = document.createTreeWalker(row, NodeFilter.SHOW_TEXT).nextNode();
    const range = document.createRange(); range.setStart(text, 0); range.setEnd(text, Math.min(20, text.length));
    document.getSelection().removeAllRanges(); document.getSelection().addRange(range); return document.getSelection().toString();
  }, anchorId);
  // Allow the native selectionchange event and virtualizer scroll state to settle.
  await page.waitForTimeout(200);
  await expect.poll(anchorPosition).toBeCloseTo(40, 0);
  release();
  await expect.poll(() => latest.locator('img').first().evaluate(img => img.complete && img.naturalWidth === 640)).toBe(true);
  await expect.poll(anchorPosition).toBeCloseTo(40, 0);
  assert.equal(await page.evaluate(() => document.getSelection().toString()), selection);
  await page.unroute('**/api/v3/content/*?*');
  await page.evaluate(() => document.getSelection().removeAllRanges());
  const anchorDrift = Math.abs((await anchorPosition()) - 40);
  await page.setViewportSize({ width: 420, height: 850 });
  await latest.scrollIntoViewIfNeeded();
  await expect.poll(() => latest.evaluate(element => {
    const images = [...element.querySelectorAll('img')].map(img => img.getBoundingClientRect().toJSON());
    const bubble = element.querySelector('[data-user-bubble]').getBoundingClientRect().toJSON(), row = element.getBoundingClientRect().toJSON();
    const fits = images.length === 5 && images.every(img => Math.abs(img.width - 78) < 0.1 && Math.abs(img.height - 78) < 0.1 && img.right <= row.right + 0.1 && img.left >= row.left - 0.1 && img.bottom < bubble.top)
      && images.at(-1).top > images[0].top && Math.abs(images.at(-1).right - bubble.right) <= 1.1;
    return { fits, images, bubble, row };
  })).toMatchObject({ fits: true });
  await page.screenshot({ path: join(directory, `${name}-stored-message-narrow.png`) });
  // Verify the same layout under the supported large-text/contrast settings.
  await page.evaluate(() => {
    localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'light' }));
    localStorage.setItem('whip.appearance.display.v1', JSON.stringify({ version: 1, display: {
      uiFont: 'system', codeFont: 'system', uiSize: 20, codeSize: 24, wrapCode: true, contrast: 'more', motion: 'reduce',
    } }));
  });
  await page.reload(); await expect(reading).toBeVisible();
  for (let i = 0; i < 80 && !(await latest.isVisible()); i++) {
    await reading.evaluate(element => { element.scrollTop = Math.max(0, element.scrollTop - element.clientHeight * 0.8); });
    await page.waitForTimeout(80);
    const earlier = reading.getByRole('button', { name: 'Load earlier messages', exact: true });
    if (await earlier.isVisible() && await earlier.isEnabled()) await earlier.click();
  }
  await expect(latest).toBeVisible(); await latest.scrollIntoViewIfNeeded();
  await expect(latest.getByRole('button', { name: /^Open image/ })).toHaveCount(5);
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Large text must not cause horizontal page overflow');
  await page.screenshot({ path: join(directory, `${name}-attachments-light-large-text.png`) });

  // The same provider role also carries tool screenshots. Exercise both inline
  // and reference-only internal records through the real history projection.
  await session.submit({ text: 'Internal screenshot fixture', attachments: images.map((image, index) => image.asAttachment('image', `screenshot-${index}.png`)) }).result();
  let history = await session.history.page({ limit: 4, max_bytes: 256 << 10 });
  let internalBody = history.messages.find(entry => entry.role === 'user' && !entry.authored && entry.body)?.body;
  // An oversized record gets a body reference only when it leads its page.
  for (let i = 0; i < 4 && !internalBody && history.has_more; i++) {
    history = await session.history.page({ limit: 4, max_bytes: 256 << 10, before_seq: history.next_seq, through_seq: history.through_seq, revision: history.history_revision });
    internalBody = history.messages.find(entry => entry.role === 'user' && !entry.authored && entry.body)?.body;
  }
  assert.ok(internalBody && Number(internalBody.size) > (1 << 20), 'Fixture must cover an internal image body larger than the old explicit text-read limit');
  const internalReads = () => requests.filter(url => new URL(url).pathname.endsWith(`/${internalBody.reference_id}`)).length;
  await page.evaluate(() => {
    localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' }));
    localStorage.removeItem('whip.appearance.display.v1');
  });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.reload(); await expect(reading).toBeVisible();
  const internal = reading.locator('[data-message-role="internal"]');
  const authored = reading.locator('[data-message-role="user"]').filter({ hasText: 'Internal screenshot fixture' });
  for (let i = 0; i < 6 && (!(await authored.count()) || await internal.count() < 2); i++) {
    const earlier = reading.getByRole('button', { name: 'Load earlier messages', exact: true });
    await expect(earlier).toBeVisible(); await earlier.click(); await page.waitForTimeout(100);
  }
  await expect(authored.getByRole('button', { name: /^Open image/ })).toHaveCount(5);
  await expect(internal).toHaveCount(2);
  await expect(internal.locator('img, [data-user-bubble]')).toHaveCount(0);
  await expect(reading.getByText('images attached (browser/computer screenshots or MCP results):', { exact: true })).toHaveCount(0);
  assert.equal(internalReads(), 0, 'Collapsed screenshot deliveries must not fetch their stored bodies');
  await page.screenshot({ path: join(directory, `${name}-internal-images-collapsed.png`) });
  await internal.first().locator('summary').click();
  await expect(internal.first().locator('img')).toHaveCount(1);
  await internal.first().locator('summary').click();
  await internal.last().locator('summary').click();
  await expect(internal.last().locator('img')).toHaveCount(2);
  await expect.poll(() => internal.last().locator('img').first().evaluate(img => img.complete && img.naturalWidth === 640)).toBe(true);
  assert.equal(internalReads(), 1, 'Explicit expansion reads the internal body once');
  await expect(internal.locator('[data-user-bubble]')).toHaveCount(0);
  await page.screenshot({ path: join(directory, `${name}-internal-images-expanded.png`) });
  await internal.last().locator('summary').click();
  await expect(internal.locator('img')).toHaveCount(0);
  return { encodedImageBytes: dataURL.length, contentReads: requests.length, mounted: count, retry: failed, anchorDrift,
    attachments: 5, loadingPreservesHeight: true, reducedMotion: true, keyboardPreview: true, narrowImagesWrap: true, lightLargeText: true,
    internalImagesCollapsed: true, internalImagesExplicitRead: true };
}
