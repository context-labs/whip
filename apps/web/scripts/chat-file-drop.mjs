import assert from 'node:assert/strict';
import { join } from 'node:path';
import { expect } from '@playwright/test';

export async function fileTransfer(page, files) {
  return page.evaluateHandle(files => {
    const transfer = new DataTransfer();
    for (const file of files) transfer.items.add(new File([
      Uint8Array.from(atob(file.base64), char => char.charCodeAt(0)),
    ], file.name, { type: file.mimeType }));
    return transfer;
  }, files.map(file => ({ name: file.name, mimeType: file.mimeType, base64: file.buffer.toString('base64') })));
}

export async function checkDropOverlay({ page, target, files, directory, name }) {
  const dataTransfer = await fileTransfer(page, files);
  try {
    await page.locator('body').dispatchEvent('dragenter', { dataTransfer });
    const surface = target.locator('xpath=ancestor-or-self::*[@data-chat-drop-surface]');
    const before = await surface.boundingBox();
    await target.dispatchEvent('dragenter', { dataTransfer });
    const overlay = surface.locator('[data-chat-file-drop]');
    await expect(overlay).toHaveText('Drop to attach');
    assert.deepEqual(await surface.boundingBox(), before, 'Hover must not resize the pane');
    const rect = await overlay.boundingBox();
    for (const axis of ['x', 'y', 'width', 'height']) assert.ok(Math.abs(rect[axis] - before[axis]) < 1, `Overlay must fill its pane: ${axis}`);
    assert.equal(await overlay.evaluate(el => getComputedStyle(el).pointerEvents), 'none');
    await page.screenshot({ path: join(directory, `${name}-drop-overlay.png`) });
    await target.dispatchEvent('dragleave', { dataTransfer });
    await expect(overlay).toHaveCount(0);
  } finally { await dataTransfer.dispose(); }
}

export async function checkChatFileDrop({ page, files, directory, name }) {
  const input = page.getByLabel('Message WHIP', { exact: true });
  const surface = input.locator('xpath=ancestor::*[@data-chat-drop-surface]');
  const reading = page.getByRole('region', { name: 'Conversation', exact: true });
  const strip = surface.getByRole('group', { name: 'Message attachments', exact: true });
  const uploads = [];
  const onRequest = request => { if (request.url().includes('/api/v3/content/upload?')) uploads.push(request.url()); };
  page.on('request', onRequest);
  const batch = [files[0], files[1], { name: 'drop-notes.txt', mimeType: 'text/plain', buffer: Buffer.from('Notes for the attached screenshots.') }];
  const dataTransfer = await fileTransfer(page, batch);
  try {
    await input.fill('Keep this draft and my reading position.');
    await input.focus();
    await reading.hover();
    await page.mouse.wheel(0, -700);
    await expect(page.getByRole('button', { name: 'Latest', exact: true })).toBeVisible();
    const before = await reading.evaluate(el => ({ top: el.scrollTop, height: el.clientHeight }));
    await reading.dispatchEvent('dragenter', { dataTransfer });
    await expect(surface.locator('[data-chat-file-drop]')).toBeVisible();
    await input.dispatchEvent('dragenter', { dataTransfer });
    await reading.dispatchEvent('dragleave', { dataTransfer });
    await expect(surface.locator('[data-chat-file-drop]')).toBeVisible();
    assert.deepEqual(await reading.evaluate(el => ({ top: el.scrollTop, height: el.clientHeight })), before);
    await expect(input).toBeFocused();
    await checkDropOverlay({ page, target: reading, files, directory, name });
    // A window-level exit resets the nested enter count left by the prior probe.
    await page.locator('body').dispatchEvent('dragenter', { dataTransfer });
    await expect(surface.locator('[data-chat-file-drop]')).toHaveCount(0);
    const url = page.url();
    await reading.dispatchEvent('drop', { dataTransfer });
    await expect(strip.getByRole('button', { name: /^Preview image-/ })).toHaveCount(2);
    await expect(strip.getByText('drop-notes.txt · Ready', { exact: true })).toBeVisible();
    await expect(surface.getByRole('button', { name: 'Send message', exact: true })).toBeEnabled();
    assert.equal(uploads.length, 3, 'One upload per file, despite nested composer handlers');
    assert.ok(Math.abs(await reading.evaluate(el => el.scrollTop) - before.top) <= 1, 'Drop must preserve reading position');
    assert.equal(page.url(), url);
    await expect(input).toHaveValue('Keep this draft and my reading position.');
    await expect(input).toBeFocused();
    await page.screenshot({ path: join(directory, `${name}-drop-attached-reading-history.png`) });

    await strip.getByRole('button', { name: 'Preview image-1.png', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'image-1.png', exact: true });
    await dialog.dispatchEvent('dragover', { dataTransfer });
    await dialog.dispatchEvent('drop', { dataTransfer });
    await expect(surface.locator('[data-chat-file-drop]')).toHaveCount(0);
    assert.equal(uploads.length, 3);
    await page.keyboard.press('Escape');
    while (await strip.getByRole('button', { name: /^Remove/ }).count()) await strip.getByRole('button', { name: /^Remove/ }).first().click();

    // Escape and the OS-cancellation fallback must both remove transient state.
    await reading.dispatchEvent('dragenter', { dataTransfer });
    await page.keyboard.press('Escape');
    await expect(surface.locator('[data-chat-file-drop]')).toHaveCount(0);
    await reading.dispatchEvent('dragenter', { dataTransfer });
    await expect(surface.locator('[data-chat-file-drop]')).toHaveCount(0, { timeout: 3000 });
    await page.locator('body').dispatchEvent('drop', { dataTransfer });
    assert.equal(page.url(), url);
    assert.equal(uploads.length, 3);

    // Direct composer drops still append once, and uploads block a second drop.
    let release;
    const held = new Promise(resolve => { release = resolve; });
    await page.route('**/api/v3/content/upload?*', async route => { await held; await route.continue(); });
    try {
      await input.dispatchEvent('drop', { dataTransfer });
      await expect(strip.getByRole('button', { name: /^Remove/ })).toHaveCount(3);
      await reading.dispatchEvent('dragenter', { dataTransfer });
      await expect(surface.locator('[data-chat-file-drop]')).toContainText('Wait for the current upload');
      await reading.dispatchEvent('drop', { dataTransfer });
      await expect(surface.getByRole('alert')).toContainText('Wait for the current upload');
      await expect(strip.getByRole('button', { name: /^Remove/ })).toHaveCount(3);
    } finally { release(); }
    await expect(surface.getByRole('button', { name: 'Send message', exact: true })).toBeEnabled();
    assert.equal(uploads.length, 6);
    await page.unroute('**/api/v3/content/upload?*');
    while (await strip.getByRole('button', { name: /^Remove/ }).count()) await strip.getByRole('button', { name: /^Remove/ }).first().click();
    await input.fill('');
    await page.getByRole('button', { name: 'Latest', exact: true }).click();
    return { uploads: uploads.length, duplicateUploads: 0, hoverScrollDrift: 0, mixedFiles: batch.length };
  } finally { page.off('request', onRequest); await dataTransfer.dispose(); }
}
