import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// Run after npm run pack:web. Every host has its own daemon, database, config,
// fake runner and endpoint. Only the explicit Local fixture origin is trusted.
const directory = process.env.WHIP_WEB_MULTIPLE_HOST_RESULTS ?? '/tmp/whip-multiple-host-results';
await mkdir(directory, { recursive: true });
const results = {};
const origin = fixture => fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
const panes = node => node.type === 'pane' ? [node] : [...panes(node.first), ...panes(node.second)];
const commandResult = handle => handle.result({ signal: AbortSignal.timeout(15_000) });

for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const launcher = { chromium, firefox }[name];
  assert.ok(launcher, `Unsupported browser: ${name}`);
  console.log(`${name}: starting three isolated daemons`);
  const fixtures = [], clients = [], errors = [], frames = [], checks = [];
  const sockets = new Map();
  let browser, page, failure, maximumSubscriptions = 0;
  const activeSubscriptions = () => [...sockets.values()].reduce((total, subscriptions) => total + subscriptions.size, 0);
  try {
    const local = await startFixture(); fixtures.push(local);
    const remoteA = await startFixture({ allowedOrigins: [origin(local)] }); fixtures.push(remoteA);
    const remoteB = await startFixture({ allowedOrigins: [origin(local)] }); fixtures.push(remoteB);
    const hosts = [local, remoteA, remoteB].map((fixture, index) => ({
      fixture, name: ['Local', 'Remote A', 'Remote B'][index], root: fixture.info.root_id,
      model: ['model-local', 'model-a', 'model-b'][index],
      runtimeId: fixture.info.runtime_id,
      client: createWhipClient({ endpoint: fixture.info.endpoint, clientId: `multi-host-${name}`, clientKind: 'human' }),
    }));
    clients.push(...hosts.map(host => host.client));
    for (const host of hosts) {
      await writeFile(join(host.fixture.directory, 'home', 'config.json'), JSON.stringify({
        defaultModel: host.model, defaultProvider: 'provider',
        providers: { provider: { name: 'Isolated fixture', baseUrl: 'http://127.0.0.1:1/v1', api: 'openai-completions', apiKey: 'isolated-test' } },
        models: { [host.model]: { providers: ['provider'], context: 128000 } },
      }), { mode: 0o600 });
      await host.client.connect();
      assert.equal((await commandResult(host.client.session(host.root).rename(`Shared title ${host.name}`))).status, 'succeeded');
      const configuration = await host.client.configuration.get();
      await host.client.configuration.update({ revision: configuration.revision, default_model: host.model, default_provider: 'provider', import_claude: false, import_codex: false });
    }
    browser = await launcher.launch();
    const context = await browser.newContext({ viewport: { width: 1800, height: 1120 } });
    context.setDefaultTimeout(15_000);
    page = await context.newPage();
    page.on('pageerror', error => errors.push(error.message));
    page.on('console', event => {
      if (event.type() === 'error' && /content.security.policy|violates.*directive|unsafe-eval/i.test(event.text())) errors.push(event.text());
    });
    page.on('websocket', socket => {
      const active = new Set(); sockets.set(socket, active);
      socket.on('framesent', ({ payload }) => {
        try {
          const frame = JSON.parse(String(payload)); frames.push({ endpoint: socket.url(), ...frame });
          if (frame.method === 'events.subscribe') active.add(frame.params.subscription_id);
          if (frame.method === 'events.unsubscribe') active.delete(frame.params.subscription_id);
          maximumSubscriptions = Math.max(maximumSubscriptions, activeSubscriptions());
        } catch { /* The SDK owns malformed-frame diagnostics. */ }
      });
      socket.on('close', () => sockets.delete(socket));
    });
    const workspace = () => page.evaluate(() => JSON.parse(sessionStorage.getItem('whip.web.workspace.v3')).workspace);
    const tab = id => page.locator(`[id="whip-workspace-tab-${encodeURIComponent(id)}"]`);
    const panel = id => page.locator(`[data-workspace-view="${id}"]`);
    const ready = async id => {
      await panel(id).getByLabel('Message WHIP', { exact: true }).waitFor();
      await eventually(() => panel(id).getByLabel('Message WHIP', { exact: true }).isEnabled(), { description: `enabled composer ${id}` });
    };
    const action = async (id, label) => {
      await tab(id).click({ button: 'right' });
      await page.getByRole('menuitem', { name: label, exact: true }).click();
      await page.getByRole('menu').waitFor({ state: 'hidden' });
    };
    const send = async (id, text) => {
      await ready(id);
      await panel(id).getByLabel('Message WHIP', { exact: true }).fill(text);
      await panel(id).getByRole('button', { name: 'Send message', exact: true }).click();
      await eventually(async () => await panel(id).getByLabel('Message WHIP', { exact: true }).inputValue() === '', { description: `accepted ${text}` });
    };
    const select = async (label, option) => {
      await page.getByRole('combobox', { name: label, exact: true }).click();
      await page.getByRole('option', { name: option, exact: true }).click();
    };
    const addDialog = page.getByRole('dialog', { name: 'Add server', exact: true });
    const manage = () => page.getByRole('button', { name: 'Manage servers', exact: true }).click();
    const back = () => page.getByRole('button', { name: 'Back to workspace', exact: true }).click();
    const hostAction = async (name, action) => {
      await page.getByRole('button', { name: `Actions for ${name}`, exact: true }).click();
      await page.getByRole('menuitem', { name: action, exact: true }).click();
      if (action === 'Remove server') await page.getByRole('alertdialog').getByRole('button', { name: 'Remove server', exact: true }).click();
    };
    const addHost = async host => {
      await page.getByRole('button', { name: 'Add server', exact: true }).click();
      await addDialog.getByLabel(/Server name/).fill(host.name);
      await addDialog.getByLabel('Server address', { exact: true }).fill(origin(host.fixture));
      await addDialog.getByRole('button', { name: 'Add server', exact: true }).click();
      await addDialog.waitFor({ state: 'hidden' });
      await back();
    };
    await page.goto(`${origin(local)}/h/${hosts[0].runtimeId}/s/${hosts[0].root}`);
    await ready(hosts[0].root);
    for (const host of hosts.slice(1)) {
      await manage();
      await addHost(host);
      await page.getByRole('region', { name: `${host.name} sessions`, exact: true }).getByRole('link', { name: `Shared title ${host.name}`, exact: true }).waitFor();
    }
    const profiles = (await hosts[0].client.configuration.get()).remote_hosts;
    assert.deepEqual(profiles.map(profile => profile.name), ['Remote A', 'Remote B']);
    for (const host of hosts.slice(1)) assert.deepEqual((await host.client.configuration.get()).remote_hosts, []);
    const peer = await browser.newContext();
    const peerPage = await peer.newPage();
    await peerPage.goto(origin(local));
    for (const host of hosts) await peerPage.getByRole('region', { name: `${host.name} sessions`, exact: true }).waitFor();
    await peer.close();
    checks.push('UI saves two verified profiles only in Local config; a fresh browser discovers all three hosts');

    // The remote directory form lives in a portal: Go must not submit creation.
    for (const host of hosts) {
      await page.getByRole('link', { name: 'New session', exact: true }).click();
      await page.getByRole('group', { name: 'Session location', exact: true }).getByRole('button', { name: host.name === 'Local' ? 'Local' : 'Remote', exact: true }).click();
      if (host.name !== 'Local') await select('Execution host', host.name);
      await page.getByLabel(`Working directory on ${host.name}`, { exact: true }).waitFor();
      if (host.name !== 'Local') assert.equal(await page.getByRole('button', { name: 'Choose folder…', exact: true }).count(), 0);
      const before = frames.filter(frame => frame.method === 'command.submit').length;
      await page.getByRole('button', { name: 'Browse host', exact: true }).click();
      const picker = page.getByRole('dialog', { name: 'Choose a working directory', exact: true });
      await picker.getByLabel('Host path', { exact: true }).fill(host.fixture.directory);
      await picker.getByRole('button', { name: 'Go', exact: true }).click();
      await picker.getByText(host.fixture.directory, { exact: true }).waitFor();
      await picker.getByRole('button', { name: 'Use this folder', exact: true }).click();
      assert.equal(await page.getByLabel(`Working directory on ${host.name}`, { exact: true }).inputValue(), host.fixture.directory);
      assert.equal(frames.filter(frame => frame.method === 'command.submit').length, before, 'Directory browsing submitted the parent form');
      await page.getByRole('button', { name: 'Start a session', exact: true }).click();
      await eventually(() => new URL(page.url()).pathname.startsWith(`/h/${host.runtimeId}/s/`));
      const created = new URL(page.url()).pathname.split('/').at(-1);
      await ready(created);
      const snapshot = await host.client.session(created).snapshot();
      assert.equal(snapshot.meta.cwd, host.fixture.directory);
      assert.equal(snapshot.meta.model, host.model, 'Creation used another host’s default model');
      await action(created, 'Close tab');
    }
    checks.push('creation uses each host’s own folder and distinct model default; Go never submits creation');

    // Build the mixed workspace through ordinary sidebar and tab-menu actions.
    for (const host of hosts) {
      await page.getByRole('region', { name: `${host.name} sessions`, exact: true }).getByRole('link', { name: `Shared title ${host.name}`, exact: true }).click();
      await ready(host.root);
    }
    await action(hosts[0].root, 'Split right');
    await action(hosts[1].root, 'Move to pane 2');
    await action(hosts[2].root, 'Split down');
    await tab(hosts[0].root).click();
    const selected = panes((await workspace()).layout).map(pane => pane.tabs.find(tab => tab.id === pane.selected));
    for (const host of hosts) {
      host.view = selected.find(tab => tab.runtimeId === host.runtimeId).id;
      await ready(host.view);
      await panel(host.view).getByLabel('Message WHIP', { exact: true }).fill(`Draft on ${host.name}`);
    }
    assert.equal(await page.locator('[data-workspace-frame]').count(), 3);
    for (const host of hosts) assert.equal(await panel(host.view).getByLabel('Message WHIP', { exact: true }).inputValue(), `Draft on ${host.name}`);
    await send(hosts[1].view, 'hold:multi-a');
    await send(hosts[2].view, 'hold:multi-b');
    for (const host of hosts.slice(1)) await panel(host.view).getByRole('button', { name: 'Pause this turn', exact: true }).waitFor();
    await send(hosts[0].view, 'Local while both remotes run');
    await eventually(async () => (await local.effects()).includes('Local while both remotes run'));
    assert.ok(!(await remoteA.effects()).includes('Local while both remotes run'));
    for (const [fixture, key] of [[remoteA, 'multi-a'], [remoteB, 'multi-b']]) await fixture.release(key);
    for (const host of hosts.slice(1)) await panel(host.view).getByRole('button', { name: 'Pause this turn', exact: true }).waitFor({ state: 'hidden' });
    checks.push('three mixed-host panes retain independent drafts and route concurrent turns to their own daemons');

    await hosts[1].client.permissions.setMode(hosts[1].root, true).result();
    await send(hosts[1].view, 'permission:multi-host');
    await panel(hosts[1].view).getByRole('button', { name: 'Allow once', exact: true }).waitFor();
    const snapshotsBeforeSearch = frames.filter(frame => frame.method === 'root.snapshot').length;
    await page.getByRole('button', { name: /^Attention ·/ }).click();
    const attention = page.getByRole('dialog', { name: 'Activity and attention', exact: true });
    await attention.getByRole('region', { name: 'Remote A attention', exact: true }).getByRole('link', { name: 'Shared title Remote A · Remote A', exact: true }).waitFor();
    await select('Attention host', 'Remote A');
    assert.equal(await attention.getByRole('region', { name: 'Local attention', exact: true }).count(), 0);
    await attention.getByRole('button', { name: 'Close', exact: true }).click();
    await page.getByRole('button', { name: 'Search sessions', exact: true }).click();
    const search = page.getByRole('dialog', { name: 'Search sessions', exact: true });
    await search.getByRole('textbox', { name: 'Search sessions across hosts', exact: true }).fill('Shared title');
    for (const host of hosts) await search.getByRole('region', { name: `${host.name} search results`, exact: true }).getByRole('link', { name: new RegExp(`^Shared title ${host.name} · ${host.name}`) }).waitFor();
    await select('Search host', 'Remote B');
    assert.equal(await search.getByRole('region', { name: 'Remote A search results', exact: true }).count(), 0);
    await search.getByRole('button', { name: 'Close', exact: true }).click();
    assert.equal(frames.filter(frame => frame.method === 'root.snapshot').length, snapshotsBeforeSearch, 'Search/attention hydrated extra roots');
    await panel(hosts[1].view).getByRole('button', { name: 'Allow once', exact: true }).click();
    await panel(hosts[1].view).getByRole('button', { name: 'Allow once', exact: true }).waitFor({ state: 'hidden' });
    const decisions = frames.filter(frame => frame.method === 'permission.decide');
    assert.equal(decisions.length, 1);
    assert.equal(new URL(decisions[0].endpoint).host, new URL(remoteA.info.endpoint).host);
    checks.push('search and permission attention label/filter hosts without root hydration; approval reaches Remote A only');

    await panel(hosts[2].view).locator('input[type=file]').setInputFiles({
      name: 'remote-note.txt', mimeType: 'text/plain', buffer: Buffer.from('Attachment stored on Remote B.'),
    });
    await panel(hosts[2].view).getByText('remote-note.txt · Ready', { exact: true }).waitFor();
    await send(hosts[2].view, 'Read the remote attachment.');
    await eventually(async () => await panel(hosts[2].view).locator('[data-message-id]').filter({ hasText: 'Attachment stored on Remote B.' }).count() >= 1);
    for (const host of hosts.slice(0, 2)) assert.equal(await panel(host.view).getByText('Attachment stored on Remote B.', { exact: true }).count(), 0);
    checks.push('a remote file upload is materialized only in its Remote B conversation');

    for (const target of [hosts[1], hosts[0]]) {
      const survivor = hosts[2];
      await panel(target.view).getByLabel('Message WHIP', { exact: true }).fill(`Preserve ${target.name}`);
      await target.fixture.crashAndRestart({ beforeRestart: async () => {
        await eventually(() => [...sockets.keys()].every(socket => new URL(socket.url()).host !== new URL(target.fixture.info.endpoint).host), { description: `${target.name} socket disconnect` });
        await ready(survivor.view);
        await send(survivor.view, `Remote B while ${target.name} is offline`);
        await eventually(async () => (await remoteB.effects()).includes(`Remote B while ${target.name} is offline`));
        await page.getByRole('button', { name: 'Search sessions', exact: true }).click();
        await search.getByRole('region', { name: `${target.name} search results`, exact: true }).getByRole('alert').waitFor();
        await search.getByRole('region', { name: 'Remote B search results', exact: true }).getByRole('link').first().waitFor();
        await search.getByRole('button', { name: 'Close', exact: true }).click();
      } });
      await ready(target.view);
      assert.equal(await panel(target.view).getByLabel('Message WHIP', { exact: true }).inputValue(), `Preserve ${target.name}`);
      await send(target.view, `${target.name} after reconnect`);
      await eventually(async () => (await target.fixture.effects()).includes(`${target.name} after reconnect`));
    }
    checks.push('Remote A and Local process outages preserve healthy remote work, partial search, drafts and reconnect');

    // Explicit disconnect detaches observation; an accepted remote turn continues.
    await send(hosts[1].view, 'hold:detached');
    await panel(hosts[1].view).getByRole('button', { name: 'Pause this turn', exact: true }).waitFor();
    await manage(); await hostAction('Remote A', 'Disconnect');
    await remoteA.release('detached');
    await eventually(async () => Object.keys((await hosts[1].client.session(hosts[1].root).snapshot()).active_turns).length === 0);
    await hostAction('Remote A', 'Connect');
    await back();
    await ready(hosts[1].view);
    checks.push('explicit host disconnect leaves accepted work running and reconnect recovers its result');

    const removedHost = hosts[1];
    const unsent = 'Keep this Remote A draft when its saved host is removed.';
    const draftKey = `whip.web.draft.v1:${removedHost.runtimeId}:${removedHost.root}:${removedHost.root}`;
    await panel(removedHost.view).getByLabel('Message WHIP', { exact: true }).fill(unsent);
    await eventually(async () => await page.evaluate(key => localStorage.getItem(key), draftKey) === unsent);
    await manage();
    await hostAction(removedHost.name, 'Remove server');
    await eventually(async () => (await hosts[0].client.configuration.get()).remote_hosts.length === 1);
    await back();
    await panel(removedHost.view).getByRole('heading', { name: 'This execution host is unavailable', exact: true }).waitFor();
    const retained = panes((await workspace()).layout).flatMap(pane => pane.tabs).find(tab => tab.id === removedHost.view);
    assert.equal(retained.runtimeId, removedHost.runtimeId);
    assert.equal(retained.rootId, removedHost.root);
    assert.equal(await page.evaluate(key => localStorage.getItem(key), draftKey), unsent);
    for (const survivor of [hosts[0], hosts[2]]) {
      const text = `${survivor.name} after Remote A profile removal`;
      await send(survivor.view, text);
      await eventually(async () => (await survivor.fixture.effects()).includes(text));
    }
    await panel(removedHost.view).getByRole('button', { name: 'Manage servers', exact: true }).click();
    await addHost(removedHost);
    await ready(removedHost.view);
    assert.equal(await panel(removedHost.view).getByLabel('Message WHIP', { exact: true }).inputValue(), unsent);
    const restoredProfile = (await hosts[0].client.configuration.get()).remote_hosts.find(profile => profile.runtime_id === removedHost.runtimeId);
    assert.ok(restoredProfile);
    assert.notEqual(restoredProfile.id, profiles.find(profile => profile.runtime_id === removedHost.runtimeId).id);
    assert.ok(!(await remoteA.effects()).includes(unsent), 'Re-adding a host submitted its draft');
    checks.push('removing Remote A preserves its unavailable tab and draft; Local/B keep working and re-adding its runtime restores the draft');

    const layout = (await workspace()).layout;
    assert.ok(maximumSubscriptions <= 4, `Before reload: ${maximumSubscriptions} simultaneous root subscriptions across all hosts`);
    // A full document replacement disposes that window's runtime. Playwright
    // may deliver old socket close events after the replacement has connected.
    sockets.clear();
    await page.reload();
    for (const host of hosts) await ready(host.view);
    assert.equal(await panel(removedHost.view).getByLabel('Message WHIP', { exact: true }).inputValue(), unsent);
    assert.deepEqual((await workspace()).layout, layout);
    assert.equal(await page.locator('[data-workspace-frame]').count(), 3);
    assert.ok(maximumSubscriptions <= 4, `Observed ${maximumSubscriptions} simultaneous root subscriptions across all hosts`);
    assert.ok(activeSubscriptions() <= 4);
    assert.deepEqual(errors, []);
    assert.equal(await page.getByRole('button', { name: 'Dismiss error', exact: true }).count(), 0, 'Host lifecycle operations left a global error banner');
    await page.screenshot({ path: join(directory, `${name}-three-hosts.png`) });
    checks.push('reload restores the mixed layout; all host connections together retain at most four root subscriptions');
    results[name] = { browser: await browser.version(), maximumSubscriptions, checks };
    await writeFile(join(directory, 'multiple-hosts.json'), JSON.stringify(results, null, 2));
    console.log(`${name}: ${checks.length} multiple-host workflows passed`);
  } catch (error) {
    failure = error;
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page?.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}\n\n${fixtures.map(fixture => fixture.output).join('\n')}`);
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify(frames, null, 2));
    throw error;
  } finally {
    for (const client of clients) client.close();
    await browser?.close();
    const cleanup = await Promise.allSettled(fixtures.map(fixture => fixture.close()));
    const failures = cleanup.filter(result => result.status === 'rejected').map(result => result.reason);
    if (failure) for (const error of failures) console.error(error);
    else if (failures.length) throw new AggregateError(failures, 'Multiple-host fixture cleanup failed');
  }
}
