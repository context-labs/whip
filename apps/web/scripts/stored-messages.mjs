import assert from 'node:assert/strict';
import { join } from 'node:path';
import { expect } from '@playwright/test';
import { deadline } from './native-fixture.mjs';
import { imageServer } from './native-image-fixture.mjs';

// Real upload -> canonical durable parts -> owner-scoped verified content.
// Native history contains canonical text; image bytes remain explicit refs.
export async function checkStoredMessages({ page, client, root, directory, name, transfers }) {
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
  const session = client.session(root);
  const imageBytes = [dataURL, ...extraImages].map(url => Buffer.from(url.split(',')[1], 'base64'));
  const images = await Promise.all(imageBytes.map((bytes, index) => session.content.upload(`stored-image-${index}`, 'image/png', bytes, deadline())));
  const requests = () => transfers.records.filter(record => record.method === 'content.read');
  for (let i = 0; i < 3; i++) {
    const request = crypto.randomUUID();
    await session.submit([{ type: 'text', text: `Screenshot message ${i}. Please inspect these images.` },
      ...images.slice(0, i * 2 + 1).map(image => ({ type: 'content', reference_id: image.id }))], request, deadline());
    assert.equal((await client.wait(request, deadline())).turn.state, 'succeeded');
  }
  // Hold completion notifications to capture the real thumbnail loading UI,
  // independently of how quickly these local data URLs decode on each engine.
  await page.addInitScript(() => {
    window.holdAttachmentLoads = !sessionStorage.getItem('attachment-loading-checked');
    const complete = Object.getOwnPropertyDescriptor(HTMLImageElement.prototype, 'complete');
    Object.defineProperty(HTMLImageElement.prototype, 'complete', { ...complete, get() {
      return window.holdAttachmentLoads && this.alt.startsWith('Attachment ') ? false : complete.get.call(this);
    } });
    const listen = HTMLImageElement.prototype.addEventListener;
    HTMLImageElement.prototype.addEventListener = function(type, listener, options) {
      // React can receive loads on detached image nodes before they have a
      // document event path, so intercept the image listener itself.
      return listen.call(this, type, type === 'load' ? function(event) {
        if (!window.holdAttachmentLoads || !this.alt.startsWith('Attachment ')) listener.call(this, event);
      } : listener, options);
    };
  });
  await page.reload();
  const latest = reading.locator('[data-message-role="user"]').filter({ hasText: 'Screenshot message 2.' });
  const tail = async () => {
    await expect(reading.locator('[data-message-role]').first()).toBeVisible();
    const latestButton = page.getByRole('button', { name: 'Latest', exact: true });
    if (await latestButton.isVisible()) await latestButton.click();
    await reading.evaluate(element => { element.scrollTop = element.scrollHeight; });
    await expect(reading.locator('[data-message-role="user"]').filter({ hasText: 'Later message 37' })).toBeVisible();
    await reading.hover(); await page.mouse.wheel(0, -200);
  };
  const loadRecentInput = async () => {
    await expect(reading).toBeVisible();
    const earlier = reading.getByRole('button', { name: 'Load earlier messages', exact: true });
    if (!(await reading.locator('[data-message-role="user"]').count()) && await earlier.isVisible()) await earlier.click();
  };
  await loadRecentInput();
  await expect(latest).toBeVisible();
  await expect(latest.getByRole('button', { name: /^Preview Attachment / })).toHaveCount(5);
  const spinner = latest.getByRole('img', { name: 'Loading Attachment 1' });
  await expect(spinner).toBeVisible();
  const thumbnailHeight = await latest.evaluate(element => element.getBoundingClientRect().height);
  await page.emulateMedia({ reducedMotion: 'reduce' });
  assert.equal(await spinner.evaluate(element => getComputedStyle(element).animationName), 'none');
  await page.screenshot({ path: join(directory, `${name}-attachment-loading.png`) });
  await page.evaluate(() => {
    window.holdAttachmentLoads = false; sessionStorage.setItem('attachment-loading-checked', '1');
    document.querySelectorAll('img[alt^="Attachment "]').forEach(img => {
      if (img.complete) img.dispatchEvent(new Event('load'));
    });
  });
  await expect.poll(() => latest.locator('img').first().evaluate(img => img.complete && img.naturalWidth === 640 && img.naturalHeight === 360)).toBe(true);
  await expect(latest.getByRole('img', { name: /^Loading Attachment / })).toHaveCount(0);
  assert.ok(Math.abs(await latest.evaluate(element => element.getBoundingClientRect().height) - thumbnailHeight) < 1 / 64, 'Decoding thumbnails must not resize the row (within layout subpixel precision)');
  await page.emulateMedia({ reducedMotion: 'no-preference' });
  const previewTrigger = latest.getByRole('button', { name: 'Preview Attachment 2', exact: true });
  await previewTrigger.focus(); await page.keyboard.press('Enter');
  const preview = page.getByRole('dialog', { name: 'Attachment 2', exact: true });
  await expect(preview).toBeVisible();
  await expect.poll(() => preview.locator('img').evaluate(img => img.complete && img.naturalWidth === 320 && img.naturalHeight === 640)).toBe(true);
  await page.screenshot({ path: join(directory, `${name}-attachment-preview.png`) });
  await page.keyboard.press('Escape'); await expect(preview).toBeHidden(); await expect(previewTrigger).toBeFocused();
  await expect(reading.getByText(/Read stored message|View image attachment/)).toHaveCount(0);
  await page.screenshot({ path: join(directory, `${name}-stored-message-inline.png`) });
  assert.ok(requests().length > 0, 'Historical images must exercise owner-scoped content reads');
  for (const request of requests()) { assert.equal(request.owner, root); assert.ok(images.some(image => image.id === request.reference)); }
  // Explicitly exercise an interrupted transfer and in-place retry.
  const failed = transfers.failNext('content.read', { disconnect: false, reference: images.at(-1).id });
  await page.reload(); await loadRecentInput();
  const retry = reading.getByRole('button', { name: 'Retry', exact: true });
  await expect(retry).toBeVisible();
  assert.ok(failed.hit);
  const readsBeforeRetry = requests().filter(request => request.reference === failed.hit.reference).length;
  await retry.click();
  await expect(latest).toBeVisible();
  await expect(retry).toHaveCount(0);
  await expect.poll(() => requests().filter(request => request.reference === failed.hit.reference).length).toBe(readsBeforeRetry + 1);
  await expect.poll(() => latest.getByRole('img', { name: 'Attachment 5', exact: true }).evaluate(img => img.complete && img.naturalWidth === 480)).toBe(true);
  assert.equal(requests().filter(request => request.reference === failed.hit.reference).length, readsBeforeRetry + 1, 'Explicit retry reads the same owner/ref exactly once');
  // Push the attachments out of the mounted window and then revisit history.
  for (let i = 0; i < 38; i++) {
    const request = crypto.randomUUID();
    await session.submit([{ type: 'text', text: `Later message ${i}` }], request, deadline());
    assert.equal((await client.wait(request, deadline())).turn.state, 'succeeded');
  }
  await page.reload(); await expect(reading).toBeVisible();
  await tail();
  for (let i = 0; i < 80 && !(await latest.isVisible()); i++) {
    await reading.evaluate(element => { element.scrollTop = Math.max(0, element.scrollTop - element.clientHeight * 0.8); });
    await page.waitForTimeout(150);
    const earlier = reading.getByRole('button', { name: 'Load earlier messages', exact: true });
    if (await reading.evaluate(element => element.scrollTop < 100) && await earlier.isVisible() && await earlier.isEnabled()) await earlier.click();
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
  const release = transfers.hold('content.read');
  // A fresh view guarantees a cold content read independently of background
  // timer throttling while the previous Query cache is being disposed.
  await page.reload(); await expect(reading).toBeVisible();
  await expect(reading.locator('[data-message-role="user"]').filter({ hasText: 'Later message 37' })).toBeVisible();
  await reading.hover(); await page.mouse.wheel(0, -200);
  for (let i = 0; i < 80 && !(await reading.getByRole('img', { name: /^Loading Attachment / }).count()); i++) {
    await reading.evaluate(element => { element.scrollTop = Math.max(0, element.scrollTop - element.clientHeight * 0.6); });
    await page.waitForTimeout(40);
    const earlier = reading.getByRole('button', { name: 'Load earlier messages', exact: true });
    if (await reading.evaluate(element => element.scrollTop < 100) && await earlier.isVisible() && await earlier.isEnabled()) await earlier.click();
  }
  await expect(reading.getByRole('img', { name: /^Loading Attachment / }).first()).toBeVisible();
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
  await tail();
  for (let i = 0; i < 80 && !(await latest.isVisible()); i++) {
    await reading.evaluate(element => { element.scrollTop = Math.max(0, element.scrollTop - element.clientHeight * 0.8); });
    await page.waitForTimeout(80);
    const earlier = reading.getByRole('button', { name: 'Load earlier messages', exact: true });
    if (await reading.evaluate(element => element.scrollTop < 100) && await earlier.isVisible() && await earlier.isEnabled()) await earlier.click();
  }
  await expect(latest).toBeVisible(); await latest.scrollIntoViewIfNeeded();
  await expect(latest.getByRole('button', { name: /^Preview Attachment / })).toHaveCount(5);
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Large text must not cause horizontal page overflow');
  await page.screenshot({ path: join(directory, `${name}-attachments-light-large-text.png`) });

  // Tool screenshots now belong to actual committed MCP operations. The
  // retired JSON message-body handle is not reconstructed or forged here.
  await page.evaluate(() => {
    localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' }));
    localStorage.removeItem('whip.appearance.display.v1');
  });
  await page.setViewportSize({ width: 1280, height: 900 });
  const largeHostImage = Buffer.from(await page.evaluate(() => {
    const canvas = document.createElement('canvas'); canvas.width = 1280; canvas.height = 720;
    const context = canvas.getContext('2d'), pixels = context.createImageData(canvas.width, canvas.height);
    let seed = 12345;
    for (let index = 0; index < pixels.data.length; index += 4) {
      seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
      pixels.data[index] = seed & 255; pixels.data[index + 1] = seed >>> 8 & 255; pixels.data[index + 2] = seed >>> 16 & 255; pixels.data[index + 3] = 255;
    }
    context.putImageData(pixels, 0, 0); return canvas.toDataURL('image/png').split(',')[1];
  }), 'base64');
  assert.ok(largeHostImage.length > (1 << 20) && largeHostImage.length <= (4 << 20));
  const mcp = await imageServer([imageBytes[0], largeHostImage]);
  try {
    await mcp.attach(client, root);
    const request = crypto.randomUUID();
    await session.submit([{ type: 'text', text: '```starlark\nprint(mcp.call(server="images", tool="screenshots", arguments={"count": 2}))\n```' }], request, deadline());
    const done = await client.wait(request, deadline()); assert.equal(done.turn.state, 'succeeded');
    const operations = (await session.turns.operations(done.turn.id, { limit: 100 }, deadline())).items;
    const operation = operations.find(item => item.capability === 'mcp.call.trusted');
    assert.equal(operation?.state, 'succeeded'); const imageRefs = operation.result.content_references;
    assert.equal(imageRefs.length, 2); assert.equal(mcp.calls, 1);
    await page.reload(); await expect(reading).toBeVisible();
    const group = reading.locator('[data-activity-group]').last();
    const host = reading.locator(`[data-activity-detail="${operation.id}"]`);
    const hostReads = () => requests().filter(request => imageRefs.includes(request.reference)).length;
    await expect(group).toBeVisible();
    assert.equal(hostReads(), 0, 'Collapsed operations must not read screenshot bytes');
    await expect(host.locator('img, [data-user-bubble]')).toHaveCount(0);
    await group.locator('[data-activity-content]').click();
    await reading.getByRole('button').filter({ hasText: 'mcp.call.trusted' }).click();
    await host.getByText('Operation details', { exact: true }).click();
    await expect(host.getByRole('button', { name: 'Read host result', exact: true })).toHaveCount(2);
    assert.equal(hostReads(), 0, 'Inspecting metadata must not read screenshot bytes');
    await host.getByRole('button', { name: 'Read host result', exact: true }).first().click();
    await expect.poll(() => host.getByRole('img', { name: 'Host result', exact: true }).evaluate(img => img.complete && img.naturalWidth === 640)).toBe(true);
    assert.equal(hostReads(), 1, 'Explicit expansion reads exactly one scoped image');
    await host.getByRole('button', { name: 'Read host result', exact: true }).last().click();
    await expect(host.getByRole('img', { name: 'Host result', exact: true })).toHaveCount(2);
    await expect.poll(() => host.getByRole('img', { name: 'Host result', exact: true }).last().evaluate(img => img.complete && img.naturalWidth === 1280)).toBe(true);
    assert.equal(hostReads(), 2);
    await page.screenshot({ path: join(directory, `${name}-host-images-expanded.png`) });
    await group.locator('[data-activity-content]').click();
    await expect(host.locator('img')).toHaveCount(0);
    assert.equal(mcp.calls, 1, 'Read/expand/collapse replayed the MCP effect');
  } finally { await mcp.close(); }
  return { encodedImageBytes: dataURL.length, contentReads: requests().length, mounted: count, retry: !!failed.hit, anchorDrift,
    attachments: 5, loadingPreservesHeight: true, reducedMotion: true, keyboardPreview: true, narrowImagesWrap: true, lightLargeText: true,
    hostImagesCollapsed: true, hostImagesExplicitRead: true, largeHostImageBytes: largeHostImage.length };
}
