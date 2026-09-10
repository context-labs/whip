import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Run after npm run pack:web. Real packaged renderer, isolated daemon, synthetic text only.
const directory = process.env.WHIP_WEB_NEW_CHAT_RESULTS ?? '/tmp/whip-new-chat-tabs-results';
await mkdir(directory, { recursive: true });
const results = {};
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const launcher = { chromium, firefox }[name];
  assert.ok(launcher, `Unsupported browser ${name}`);
  console.log(`${name}: starting New Chat fixture`);
  const fixture = await startFixture();
  let browser, client, page, providerServer;
  let holdCreate = false, releaseCreate;
  const frames = [], errors = [], checks = [];
  try {
    browser = await launcher.launch();
    const context = await browser.newContext({ viewport: { width: 1440, height: 960 }, colorScheme: 'light' });
    context.setDefaultTimeout(15_000);
    // This loopback endpoint serves catalog discovery only. TestV2SDKBridge executes
    // all model turns with its existing synthetic runner; no credentials or paid calls.
    providerServer = createServer((_request, response) => {
      response.writeHead(200, { 'content-type': 'application/json' });
      response.end(JSON.stringify({ object: 'list', data: [{ id: 'fixture-model', object: 'model' }] }));
    });
    await new Promise(resolve => providerServer.listen(0, '127.0.0.1', resolve));
    client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `new-tabs-${crypto.randomUUID()}`, clientKind: 'human' });
    await client.connect();
    const configuration = await client.configuration.get();
    const configured = await client.providers.create({ revision: configuration.revision, provider: 'fixture-local', definition: { name: 'Fixture local', base_url: `http://127.0.0.1:${providerServer.address().port}/v1`, api: 'openai-completions' }, credential: { mode: 'none' }, manual_model: { alias: 'fixture-model', id: 'fixture-model' } });
    await client.configuration.update({ revision: configured.revision, default_provider: 'fixture-local', default_model: 'fixture-model' });
    assert.equal((await client.providers.list()).selection.ready, true);
    page = await context.newPage();
    // Proxy protocol traffic unchanged; one create can be held until tab focus moves.
    // Acceptance, submit and execution responses always come from the real daemon.
    await page.routeWebSocket('**/api/v3/ws', socket => {
      const upstream = socket.connectToServer();
      socket.onMessage(message => {
        const frame = JSON.parse(String(message));
        frames.push(frame);
        if (holdCreate && frame.method === 'command.submit' && frame.params.operation === 'session.create') {
          holdCreate = false;
          releaseCreate = () => upstream.send(message);
        } else upstream.send(message);
      });
    });
    page.on('pageerror', error => errors.push(error.message));

    const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
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
    await page.getByRole('button', { name: 'New session tab', exact: true }).click();
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
    assert.equal(restored.find(tab => tab.id === second).permissionMode, 'prompt');
    checks.push('explicit New creates distinct tabs; independent text/permission/setup survive switching and reload');
    const sessionEffects = frames.filter(frame => frame.method === 'root.snapshot' || (frame.method === 'command.submit' && ['session.create', 'submit', 'agent.submit'].includes(frame.params.operation)));
    assert.deepEqual(sessionEffects, [], 'Unsent drafts must not create/send/hydrate session roots');
    assert.equal(frames.some(frame => frame.method === 'sessions.summaries' && frame.params.root_ids.some(id => [first, second].includes(id))), false);
    checks.push('draft-only workspace makes no create/send/root snapshot RPCs and no summaries for draft IDs');
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
    await tabRow(second).getByRole('button', { name: 'Tab actions for New Chat', exact: true }).click();
    assert.equal(await page.getByRole('menuitem', { name: 'Copy session link', exact: true }).count(), 0);
    await page.getByRole('menuitem', { name: 'Move to split right', exact: true }).click();
    await eventually(async () => (await draft().count()) === 2);
    await page.screenshot({ path: join(directory, `${name}-split.png`) });
    checks.push('draft menus omit session-only actions; draft moves into a real second pane');
    // Add a synthetic existing root without creating it from a draft.
    const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
    assert.equal(created.status, 'succeeded');
    const root = created.result.root_id;
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
    await page.getByRole('heading', { name: 'This New Chat is not available' }).waitFor();
    assert.equal(allTabs((await workspace()).workspace.layout).length, 3);
    checks.push('unknown draft URL shows explicit missing state without allocation');
    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(`${origin}/new/${first}`);
    await tab(first).waitFor();
    for (const descriptor of allTabs((await workspace()).workspace.layout)) {
      await tabRow(descriptor.id).getByRole('button', { name: /^Close / }).click({ force: true });
    }
    await page.getByRole('heading', { name: 'Your workspace is empty' }).waitFor();
    await page.reload();
    await page.getByRole('heading', { name: 'Your workspace is empty' }).waitFor();
    assert.equal(allTabs((await workspace()).workspace.layout).length, 0);
    checks.push('closing the final tab leaves an empty workspace across reload without phantom drafts');
    const commands = operation => frames.filter(frame => frame.method === 'command.submit' && frame.params.operation === operation);
    const createCount = commands('session.create').length, submitCount = commands('submit').length;
    await page.goto(`${origin}/?new=1&runtimeId=${fixture.info.runtime_id}&cwd=${encodeURIComponent(fixture.directory)}`);
    await ready(); const foreground = id();
    await draft().fill('Foreground first message fixture.');
    await page.getByRole('button', { name: 'Send first message', exact: true }).click();
    await page.getByLabel('Message WHIP', { exact: true }).waitFor();
    const promoted = allTabs((await workspace()).workspace.layout).find(tab => tab.id === foreground);
    assert.equal(promoted.kind, 'chat');
    assert.equal(id(), promoted.rootId);
    assert.equal(commands('session.create').length, createCount + 1);
    assert.equal(commands('submit').length, submitCount + 1);
    checks.push('real first send creates/submits exactly once and promotes the same tab ID');
    await page.goto(`${origin}/?new=1&runtimeId=${fixture.info.runtime_id}&cwd=${encodeURIComponent(fixture.directory)}`);
    await ready(); const background = id();
    await draft().fill('Background first message fixture.');
    holdCreate = true; releaseCreate = undefined;
    await page.getByRole('button', { name: 'Send first message', exact: true }).click();
    await eventually(() => releaseCreate, { description: 'outgoing create held before real daemon admission' });
    await tab(foreground).click();
    const focusedComposer = page.getByLabel('Message WHIP', { exact: true });
    await focusedComposer.fill('Keep the focus and unsent text here.');
    releaseCreate();
    await eventually(async () => allTabs((await workspace()).workspace.layout).find(tab => tab.id === background)?.kind === 'chat', { description: 'background draft promotes after real acceptance' });
    assert.equal(id(), promoted.rootId);
    assert.equal(await focusedComposer.inputValue(), 'Keep the focus and unsent text here.');
    assert.equal(await focusedComposer.evaluate(element => document.activeElement === element), true);
    assert.equal(commands('session.create').length, createCount + 2);
    assert.equal(commands('submit').length, submitCount + 2);
    checks.push('real delayed background acceptance promotes in place without route/focus theft or duplicate commands');
    await page.screenshot({ path: join(directory, `${name}-promoted.png`) });
    assert.deepEqual(errors, []);
    results[name] = { browser: await browser.version(), checks, screenshots: ['light', 'dark', 'split', 'mixed', 'compact', 'promoted'], draftSessionEffects: sessionEffects.length, firstSendCommands: { create: commands('session.create').length - createCount, submit: commands('submit').length - submitCount }, provider: 'no-auth loopback catalog; built-in SDK fixture synthetic runner' };
    console.log(`${name}: ${checks.length} New Chat workflows passed`);
  } catch (error) {
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page?.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`);
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify(frames, null, 2));
    throw error;
  } finally { client?.close(); await browser?.close(); await fixture.close(); if (providerServer) await new Promise(resolve => providerServer.close(resolve)); }
}
await writeFile(join(directory, 'new-chat-tabs.json'), JSON.stringify(results, null, 2));
console.log(JSON.stringify(results, null, 2));
