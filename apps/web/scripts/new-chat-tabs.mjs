import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';
import { fileTransfer, checkDropOverlay } from './chat-file-drop.mjs';

// Run after npm run pack:web. Real packaged renderer, isolated native runtime, engines and local provider; synthetic text/images only.
const directory = process.env.WHIP_WEB_NEW_CHAT_RESULTS ?? '/tmp/whip-new-chat-tabs-results';
await mkdir(directory, { recursive: true });
const results = {};
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const launcher = { chromium, firefox }[name];
  assert.ok(launcher, `Unsupported browser ${name}`);
  console.log(`${name}: starting New Chat fixture`);
  const fixture = await startFixture();
  let browser, client, page;
  let closed = false, holdUpload = false, failUpload = false;
  const connections = new Set(), closing = new Set(), uploadRequests = [], heldUploads = [];
  const closeEndpoint = endpoint => { const pending = endpoint.close().catch(error => { if (errors.length < 32) errors.push(String(error).slice(0, 4096)); }).finally(() => closing.delete(pending)); closing.add(pending); };
  let holdCreate = false, releaseCreate;
  const frames = [], errors = [], checks = [];
  try {
    browser = await launcher.launch();
    const context = await browser.newContext({ viewport: { width: 1440, height: 960 }, colorScheme: 'light' });
    context.setDefaultTimeout(15_000);
    client = await fixture.connect(`new-tabs-${crypto.randomUUID()}`);
    page = await context.newPage();
    // Hold only exact real native effects. Cleanup closes both endpoints and
    // discards held work; it never releases a create/upload upstream.
    await page.routeWebSocket('**/api/v4/ws', socket => {
      if (closed || connections.size >= 128) { if (!closed && errors.length < 32) errors.push('New Chat proxy connection bound'); closeEndpoint(socket); return; }
      const upstream = socket.connectToServer(); let live = true;
      const retire = () => { if (!live) return; live = false; connections.delete(retire); closeEndpoint(socket); closeEndpoint(upstream); };
      connections.add(retire); socket.onClose(retire); upstream.onClose(retire);
      socket.onMessage(message => {
        try {
          if (!live || closed) return;
          assert(Buffer.byteLength(message) <= (8 << 20));
          const frame = JSON.parse(String(message));
          assert(frames.length < 4096, 'New Chat probe frame bound exceeded');
          // Never retain attachment bytes or repeated transcript bodies.
          frames.push({ method: frame.method, session_id: frame.params?.session_id, root_ids: frame.params?.root_ids });
          const forward = () => { if (live && !closed) upstream.send(message); };
          if (holdCreate && frame.method === 'trees.create') {
            holdCreate = false; releaseCreate = forward;
          } else if (frame.method === 'content.put') {
            assert(uploadRequests.length < 16);
            uploadRequests.push({ owner: frame.params.session_id, reference: frame.params.reference_id });
            if (failUpload) {
              socket.send(JSON.stringify({ jsonrpc: '2.0', id: frame.id, error: { code: -32603, kind: 'INTERNAL', message: 'Synthetic unavailable content' } }));
            } else if (holdUpload) { assert(heldUploads.length < 2); heldUploads.push(forward); }
            else forward();
          } else forward();
        } catch (error) { if (errors.length < 32) errors.push(String(error).slice(0, 4096)); retire(); }
      });
    });
    page.on('pageerror', error => { if (errors.length < 32) errors.push(error.message.slice(0, 4096)); });
    await page.exposeFunction('draftCSP', error => { if (errors.length < 32) errors.push(String(error)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.draftCSP(event.violatedDirective); }));

    const origin = fixture.info.web;
    const draft = () => page.getByLabel('Your first message', { exact: true });
    const ready = async () => { await draft().waitFor(); await eventually(() => draft().isEnabled()); };
    const id = () => decodeURIComponent(new URL(page.url()).pathname.split('/').at(-1));
    const tab = id => page.locator(`[role="tab"][id="whip-workspace-tab-${id}"]`);
    const tabRow = id => page.locator(`[data-workspace-tab="${id}"]`);
    const workspace = () => page.evaluate(() => JSON.parse(sessionStorage.getItem('whip.web.workspace.v3')));
    const allTabs = tree => tree.type === 'pane' ? tree.tabs : [...allTabs(tree.first), ...allTabs(tree.second)];
    const command = async label => {
      await page.keyboard.press(process.platform === 'darwin' ? 'Meta+k' : 'Control+k');
      await page.getByRole('dialog', { name: 'Commands', exact: true }).getByRole('combobox').fill(label);
      await page.getByRole('option', { name: label, exact: true }).click();
    };
    await page.goto(`${origin}/?new=1&runtimeId=${fixture.info.runtime_id}&cwd=${encodeURIComponent(fixture.directory)}`);
    await ready(); const first = id();
    assert.match(new URL(page.url()).pathname, /^\/new\//);
    await draft().fill('First independent unsent task.');
    await page.getByRole('button', { name: 'Permission approval mode', exact: true }).click();
    await page.getByRole('option', { name: /^Full Access/ }).click();
    assert.equal(await page.getByRole('button', { name: 'New session tab', exact: true }).count(), 0);
    await page.getByRole('button', { name: 'Pane 1 actions', exact: true }).click();
    await expect(page.getByRole('menuitem').first()).toHaveText('New session');
    await page.getByRole('menuitem', { name: 'New session', exact: true }).click();
    await ready(); const second = id(); assert.notEqual(first, second);
    await draft().fill('Second independent unsent task.');
    await tab(first).click(); await ready();
    assert.equal(await draft().inputValue(), 'First independent unsent task.');
    assert.match(await page.getByRole('button', { name: 'Permission approval mode', exact: true }).innerText(), /Full Access/);
    await tab(second).click(); await ready();
    assert.equal(await draft().inputValue(), 'Second independent unsent task.');
    assert.match(await page.getByRole('button', { name: 'Permission approval mode', exact: true }).innerText(), /Ask for approval/);
    await page.reload(); await ready();
    assert.equal(id(), second); assert.equal(await draft().inputValue(), 'Second independent unsent task.');
    const restored = allTabs((await workspace()).workspace.layout);
    assert.equal(restored.length, 2);
    assert.equal(restored.find(tab => tab.id === first).cwd, fixture.directory);
    assert.equal(restored.find(tab => tab.id === first).permissionMode, 'automatic');
    assert.equal(restored.find(tab => tab.id === second).cwd, '');
    // An untouched native draft inherits the host default; the visible Ask
    // choice above must not manufacture a saved override.
    assert.equal(restored.find(tab => tab.id === second).permissionMode, undefined);
    checks.push('explicit New creates distinct tabs; independent text/permission/setup survive switching and reload');
    const sessionEffects = frames.filter(frame => ['trees.create', 'sessions.submit', 'sessions.spawn', 'sessions.history_page', 'sessions.history', 'sessions.observe', 'sessions.turns'].includes(frame.method));
    assert.deepEqual(sessionEffects, [], 'Unsent drafts must not create/send/hydrate session roots');
    assert.equal(frames.some(frame => frame.method === 'trees.summaries' && frame.root_ids.some(id => [first, second].includes(id))), false);
    checks.push('draft-only workspace makes no create/send/history/activity RPCs and no summaries for draft IDs');
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'light' })));
    await page.reload(); await ready();
    const lightBackground = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--whip-background'));
    await page.screenshot({ path: join(directory, `${name}-light.png`) });
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await ready();
    assert.notEqual(await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--whip-background')), lightBackground);
    await page.screenshot({ path: join(directory, `${name}-dark.png`) });
    await tabRow(second).getByRole('button', { name: /^Close / }).click();
    await ready(); assert.equal(id(), first);
    await command('Reopen closed tab'); await ready();
    assert.equal(id(), second); assert.equal(await draft().inputValue(), 'Second independent unsent task.');
    checks.push('closing and reopening a draft preserves identity and text');
    await tab(second).click({ button: 'right' });
    assert.equal(await page.getByRole('menuitem', { name: 'Copy session link', exact: true }).count(), 0);
    await page.getByRole('menuitem', { name: 'Move to split right', exact: true }).click();
    await eventually(async () => (await draft().count()) === 2);
    await page.screenshot({ path: join(directory, `${name}-split.png`) });
    checks.push('draft menus omit session-only actions; draft moves into a real second pane');
    const splitInputs = await draft().all();
    const splitSurface = splitInputs[0].locator('xpath=ancestor::*[@data-chat-drop-surface]');
    const otherSurface = splitInputs[1].locator('xpath=ancestor::*[@data-chat-drop-surface]');
    const splitImage = { name: 'split.png', mimeType: 'image/png', buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64') };
    await splitInputs[1].focus();
    await checkDropOverlay({ page, target: splitInputs[0], files: [splitImage], directory, name: `${name}-split` });
    const splitTransfer = await fileTransfer(page, [splitImage, { ...splitImage, name: 'split-second.png' }]);
    const beforeSplitCommands = frames.filter(frame => ['trees.create', 'sessions.submit', 'sessions.spawn'].includes(frame.method)).length;
    await splitSurface.dispatchEvent('drop', { dataTransfer: splitTransfer });
    await splitTransfer.dispose();
    await expect(splitSurface.getByRole('button', { name: /^Preview split/ })).toHaveCount(2);
    await expect(otherSurface.getByRole('group', { name: 'Message attachments', exact: true })).toHaveCount(0);
    await expect(splitInputs[1]).toBeFocused();
    assert.equal(frames.filter(frame => ['trees.create', 'sessions.submit', 'sessions.spawn'].includes(frame.method)).length, beforeSplitCommands);
    await page.screenshot({ path: join(directory, `${name}-split-drop-attached.png`) });
    for (const file of ['split.png', 'split-second.png']) await splitSurface.getByRole('button', { name: `Remove ${file}`, exact: true }).click();
    checks.push('whole-pane New Chat drop stages two images only in the pane under the cursor, without creating a session or stealing focus');
    // Add a synthetic existing root without creating it from a draft.
    const created = await fixture.createRoot(client);
    const root = created.root.id;
    await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${root}`);
    await page.getByLabel('Message WHIP', { exact: true }).waitFor();
    assert.equal(await draft().count(), 1);
    assert.equal(allTabs((await workspace()).workspace.layout).length, 3);
    checks.push('session and unsent draft render together in mixed panes');
    await page.screenshot({ path: join(directory, `${name}-mixed.png`) });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: /^Open sessions:/ }).click();
    const picker = page.getByRole('dialog', { name: 'Open sessions', exact: true });
    await picker.getByRole('button', { name: /New Chat.*Not sent yet/ }).first().click();
    await ready();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await page.screenshot({ path: join(directory, `${name}-compact.png`) });
    checks.push('compact picker selects a draft with no horizontal document overflow');
    await page.goto(`${origin}/new/unknown-draft`);
    await page.getByRole('heading', { name: 'This New Chat isn’t open here.' }).waitFor();
    assert.equal(allTabs((await workspace()).workspace.layout).length, 3);
    checks.push('unknown draft URL shows explicit missing state without allocation');
    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`${origin}/new/${first}`);
    await tab(first).waitFor();
    const closingTabs = allTabs((await workspace()).workspace.layout);
    for (const descriptor of closingTabs) {
      await tabRow(descriptor.id).getByRole('button', { name: /^Close / }).click();
      await expect(tabRow(descriptor.id)).toHaveCount(0);
    }
    // Since 203695a03b, closing the final session opens one empty draft on
    // that host/folder. Closing that draft then leaves the workspace empty.
    if (closingTabs.at(-1).kind !== 'new') {
      await ready();
      const [replacement, ...extra] = allTabs((await workspace()).workspace.layout);
      assert.equal(extra.length, 0);
      assert.equal(replacement.kind, 'new');
      assert.equal(replacement.runtimeId, fixture.info.runtime_id);
      assert.equal(replacement.cwd, fixture.directory);
      assert.equal(await draft().inputValue(), '');
      await tabRow(replacement.id).getByRole('button', { name: /^Close / }).click();
      await expect(tabRow(replacement.id)).toHaveCount(0);
    }
    await page.getByRole('heading', { name: 'What do you want to work on?' }).waitFor();
    assert.equal(allTabs((await workspace()).workspace.layout).length, 0);
    await page.reload();
    await page.getByRole('heading', { name: 'What do you want to work on?' }).waitFor();
    assert.equal(allTabs((await workspace()).workspace.layout).length, 0);
    assert.equal(await page.getByRole('button', { name: 'New session tab', exact: true }).count(), 0);
    await page.getByRole('button', { name: 'Pane 1 actions', exact: true }).click();
    await expect(page.getByRole('menuitem').first()).toHaveText('New session');
    await page.getByRole('menuitem', { name: 'New session', exact: true }).click();
    await ready();
    assert.equal(await draft().inputValue(), '');
    assert.equal(allTabs((await workspace()).workspace.layout).length, 1);
    checks.push('closing the final tab leaves an empty workspace across reload; explicit New opens one blank draft');
    const commands = method => frames.filter(frame => frame.method === method);
    const createCount = commands('trees.create').length, submitCount = commands('sessions.submit').length;
    await page.goto(`${origin}/?new=1&runtimeId=${fixture.info.runtime_id}&cwd=${encodeURIComponent(fixture.directory)}`);
    await ready(); const foreground = id();
    const imageURL = await page.evaluate(() => {
      const canvas = document.createElement('canvas'); canvas.width = 320; canvas.height = 240;
      const context = canvas.getContext('2d'); context.fillStyle = '#456347'; context.fillRect(0, 0, 320, 240);
      context.fillStyle = '#fff'; context.font = '20px sans-serif'; context.fillText('First message image', 20, 80);
      return canvas.toDataURL('image/png');
    });
    const image = { name: 'first.png', mimeType: 'image/png', buffer: Buffer.from(imageURL.split(',')[1], 'base64') };
    const strip = () => page.getByRole('textbox', { name: /^(Your first message|Message WHIP)$/ })
      .locator('xpath=ancestor::form').getByRole('group', { name: 'Message attachments', exact: true });
    holdUpload = true;
    const firstTransfer = await fileTransfer(page, [image, { ...image, name: 'second.png' }]);
    await draft().locator('xpath=ancestor::*[@data-chat-drop-surface]').dispatchEvent('drop', { dataTransfer: firstTransfer });
    await firstTransfer.dispose();
    await expect(strip().getByRole('button', { name: /^Preview/ })).toHaveCount(2);
    await expect.poll(() => strip().locator('img').evaluateAll(images => images.every(image => image.naturalWidth > 0))).toBe(true);
    assert.deepEqual(uploadRequests, []);
    assert.equal(commands('trees.create').length, createCount);
    await page.screenshot({ path: join(directory, `${name}-first-images-ready.png`) });
    await page.getByRole('button', { name: 'Send first message', exact: true }).click();
    await page.getByLabel('Message WHIP', { exact: true }).waitFor();
    const promoted = allTabs((await workspace()).workspace.layout).find(tab => tab.id === foreground);
    assert.equal(promoted.kind, 'chat');
    assert.equal(id(), promoted.rootId);
    assert.equal(commands('trees.create').length, createCount + 1);
    assert.equal(commands('sessions.submit').length, submitCount);
    await expect(strip().getByRole('img', { name: /^Uploading/ })).toHaveCount(2);
    await page.screenshot({ path: join(directory, `${name}-first-images-uploading.png`) });
    await eventually(() => heldUploads.length > 0, { description: 'exact first native upload held' });
    holdUpload = false; for (const forward of heldUploads.splice(0)) forward();
    await eventually(() => commands('sessions.submit').length === submitCount + 1);
    await expect(strip()).toHaveCount(0);
    assert.equal(uploadRequests.length, 2);
    assert.ok(uploadRequests.every(request => request.owner === promoted.rootId));
    const sentImages = page.getByRole('region', { name: 'Conversation', exact: true }).getByRole('button', { name: /^Preview Attachment / });
    await expect(sentImages).toHaveCount(2);
    // The actual local provider emits deterministic fixture responses. Inspect the authored image row,
    // not its fixture-only response, and verify both thumbnails really decode.
    await expect(page.getByText('Idle', { exact: true })).toBeVisible();
    await page.getByRole('region', { name: 'Conversation', exact: true }).hover();
    await page.mouse.wheel(0, -10000);
    await sentImages.first().scrollIntoViewIfNeeded();
    await expect.poll(() => sentImages.locator('img').evaluateAll(images => images.length === 2 && images.every(image => image.naturalWidth > 0))).toBe(true);
    await page.screenshot({ path: join(directory, `${name}-first-images-sent.png`) });
    assert.equal(commands('sessions.submit').length, submitCount + 1);
    const authored = (await client.session(promoted.rootId).history.page({ direction: 'backward', limit: 16 }, deadline())).messages.find(message => message.role === 'user');
    assert.equal(authored?.parts.filter(part => part.type === 'content').length, 2);
    checks.push('image-only first message stages multiple previews locally, creates once, uploads in root scope and renders both images after acceptance');
    await page.goto(`${origin}/?new=1&runtimeId=${fixture.info.runtime_id}&cwd=${encodeURIComponent(fixture.directory)}`);
    await ready(); const background = id();
    await draft().fill('Background first message fixture.');
    await page.locator('input[type=file]').setInputFiles(image);
    holdCreate = true; releaseCreate = undefined;
    await page.getByRole('button', { name: 'Send first message', exact: true }).click();
    await eventually(() => releaseCreate, { description: 'outgoing create held before real daemon admission' });
    await tab(foreground).click();
    const focusedComposer = page.getByLabel('Message WHIP', { exact: true });
    await focusedComposer.fill('Keep the focus and unsent text here.');
    releaseCreate();
    await eventually(async () => allTabs((await workspace()).workspace.layout).find(tab => tab.id === background)?.kind === 'chat', { description: 'background draft promotes after real acceptance' });
    await eventually(() => commands('sessions.submit').length === submitCount + 2);
    assert.equal(id(), promoted.rootId);
    assert.equal(await focusedComposer.inputValue(), 'Keep the focus and unsent text here.');
    assert.equal(await focusedComposer.evaluate(element => document.activeElement === element), true);
    assert.equal(commands('trees.create').length, createCount + 2);
    assert.equal(commands('sessions.submit').length, submitCount + 2);
    checks.push('delayed background creation preserves and submits the image without route/focus theft or duplicate commands');
    await page.screenshot({ path: join(directory, `${name}-promoted.png`) });
    await page.goto(`${origin}/?new=1&runtimeId=${fixture.info.runtime_id}&cwd=${encodeURIComponent(fixture.directory)}`);
    await ready();
    await draft().fill('Keep my image after a failed upload.');
    await page.locator('input[type=file]').setInputFiles(image);
    failUpload = true;
    await page.getByRole('button', { name: 'Send first message', exact: true }).click();
    const composer = page.getByLabel('Message WHIP', { exact: true });
    await expect(composer).toHaveValue('Keep my image after a failed upload.');
    await expect(strip().getByText('Upload failed', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Send message', exact: true })).toBeDisabled();
    const failedRoot = id();
    assert.equal(commands('trees.create').length, createCount + 3);
    assert.equal(commands('sessions.submit').length, submitCount + 2);
    failUpload = false;
    await strip().getByRole('button', { name: 'Remove first.png', exact: true }).click();
    await page.locator('input[type=file]').setInputFiles(image);
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(strip()).toHaveCount(0);
    assert.equal(id(), failedRoot);
    assert.equal(commands('trees.create').length, createCount + 3);
    assert.equal(commands('sessions.submit').length, submitCount + 3);
    checks.push('failed initial upload retains text/image in the created session; replacing the file sends without another root');
    assert.deepEqual(errors, []);
    results[name] = { browser: await browser.version(), checks, screenshots: ['light', 'dark', 'split', 'mixed', 'compact', 'promoted'], draftSessionEffects: sessionEffects.length, firstSendCommands: { create: commands('trees.create').length - createCount, submit: commands('sessions.submit').length - submitCount }, provider: 'no-auth loopback HTTP provider; production native runtime and engine' };
    console.log(`${name}: ${checks.length} New Chat workflows passed`);
  } catch (error) {
    await page?.locator('details').evaluateAll(items => { for (const item of items) item.open = true; }).catch(() => {});
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page?.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`);
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify(frames, null, 2));
    throw error;
  } finally {
    closed = true; releaseCreate = undefined; heldUploads.length = 0;
    for (const retire of connections) retire();
    try { await Promise.allSettled([...closing]); await browser?.close(); } finally { await fixture.close(); }
  }
}
await writeFile(join(directory, 'new-chat-tabs.json'), JSON.stringify(results, null, 2));
console.log(JSON.stringify(results, null, 2));
