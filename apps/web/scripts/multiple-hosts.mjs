import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, repository, startFixture } from './native-fixture.mjs';

// Run after npm run pack:web. Every host has its own daemon, database, config,
// actual engines, synthetic HTTP provider and endpoint. Only the explicit Local fixture origin is trusted.
const directory = process.env.WHIP_WEB_MULTIPLE_HOST_RESULTS ?? '/tmp/whip-multiple-host-results';
await mkdir(directory, { recursive: true });
const results = {};
const origin = fixture => fixture.info.web;
const panes = node => node.type === 'pane' ? [node] : [...panes(node.first), ...panes(node.second)];

for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const launcher = { chromium, firefox }[name];
  assert.ok(launcher, `Unsupported browser: ${name}`);
  console.log(`${name}: starting three isolated daemons`);
  const fixtures = [], errors = [], frames = [], checks = [], cspErrors = [], closedOwners = [];
  const sockets = new Map();
  let browser, page, failure, maximumObservations = 0;
  const activeObservations = () => [...sockets.values()].reduce((total, observations) => total + observations.size, 0);
  try {
    const local = await startFixture({ lifetimeMs: 900_000 }); fixtures.push(local);
    const remoteA = await startFixture({ allowedOrigins: [origin(local)], lifetimeMs: 900_000 }); fixtures.push(remoteA);
    const remoteB = await startFixture({ allowedOrigins: [origin(local)], lifetimeMs: 900_000 }); fixtures.push(remoteB);
    const hosts = [];
    for (const [index, fixture] of [local, remoteA, remoteB].entries()) {
      const name = ['Local', 'Remote A', 'Remote B'][index], model = ['model-local', 'model-a', 'model-b'][index];
      const client = await fixture.connect(`multi-host-${index}`);
      const inventory = await client.listProviders(deadline()), source = inventory.routes.find(route => route.id === 'provider');
      const configured = await client.createProvider({ revision: inventory.revision, provider: 'host-fixture', keep_credential: false, key: null,
        declaration: { kind: 'openai-chat', base_url: source.base_url, credential: { source: 'none', environment: '', file: '', command: null },
          models: { [model]: source.models.model } } }, deadline());
      await client.setProviderDefaults({ revision: configured.revision, defaults: { selection: { provider: 'host-fixture', name: model, effort: '' }, settings: null } }, deadline());
      const { root } = await fixture.createRoot(client, { title: `Shared title ${name}`, overrides: { model: { provider: 'host-fixture', name: model, effort: '' } } });
      hosts.push({ fixture, name, model, root: root.id, runtimeId: client.runtimeID, client });
    }
    const manifest = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
    for (const [path, file] of Object.entries(manifest.files)) {
      const response = await fetch(`${origin(local)}/${path}`); assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256);
    }
    browser = await launcher.launch();
    const context = await browser.newContext({ viewport: { width: 1800, height: 1120 } });
    context.setDefaultTimeout(15_000);
    page = await context.newPage();
    page.on('pageerror', error => { if (errors.length < 64) errors.push(error.message.slice(0, 2048)); });
    page.on('console', event => {
      if (event.type() === 'error' && /content.security.policy|violates.*directive|unsafe-eval/i.test(event.text())) { if (errors.length < 64) errors.push(event.text().slice(0, 2048)); }
    });
    await page.exposeFunction('recordMultiHostCSP', directive => { if (cspErrors.length < 64) cspErrors.push(String(directive)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => void window.recordMultiHostCSP(event.violatedDirective)));
    page.on('websocket', socket => {
      const active = new Set(); sockets.set(socket, active);
      socket.on('framesent', ({ payload }) => {
        try {
          const request = JSON.parse(String(payload)); assert(frames.length < 40_000, 'Host traffic evidence limit exceeded');
          const params = request.params ?? {};
          frames.push({ at: Date.now(), endpoint: socket.url(), method: request.method, params: { session_id: params.session_id, root_id: params.root_id,
            operation_id: params.operation_id, creation_id: params.creation_id, reference_id: params.reference_id, identity: params.identity } });
          if (request.method === 'sessions.observe') active.add(request.id);
          maximumObservations = Math.max(maximumObservations, activeObservations());
        } catch (error) { if (errors.length < 64) errors.push(error.message); }
      });
      socket.on('framereceived', ({ payload }) => { try { active.delete(JSON.parse(String(payload)).id); } catch {} });
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
    const manage = () => page.locator('#whip-session-navigation').getByRole('button', { name: 'Manage servers', exact: true }).click();
    const back = () => page.getByRole('button', { name: 'Back to workspace', exact: true }).click();
    const hostAction = async (name, action) => {
      await page.getByRole('button', { name: `Actions for ${name}`, exact: true }).click();
      await page.getByRole('menuitem', { name: action, exact: true }).click();
      if (action === 'Remove server' || action === 'Disconnect') {
        const confirmation = page.getByRole('alertdialog');
        await confirmation.getByRole('button', { name: action, exact: true }).click();
        await confirmation.waitFor({ state: 'hidden' });
      }
    };
    const addHost = async host => {
      await page.getByRole('button', { name: 'Add server', exact: true }).click();
      await addDialog.getByLabel(/Server name/).fill(host.name);
      await addDialog.getByLabel('Server address', { exact: true }).fill(origin(host.fixture));
      await addDialog.getByRole('button', { name: 'Connect', exact: true }).click();
      await addDialog.waitFor({ state: 'hidden' });
      await back();
    };
    await page.goto(`${origin(local)}/h/${hosts[0].runtimeId}/s/${hosts[0].root}`);
    await ready(hosts[0].root);
    for (const host of hosts.slice(1)) {
      await manage();
      await addHost(host);
      await page.getByRole('region', { name: 'Projects', exact: true }).locator(`[data-sidebar-runtime="${host.runtimeId}"][data-sidebar-session="${host.root}"]`).getByRole('link', { name: `Shared title ${host.name}`, exact: true }).waitFor();
    }
    const profiles = (await hosts[0].client.hosts.profiles(deadline())).profiles;
    assert.deepEqual(profiles.map(profile => profile.name), ['Remote A', 'Remote B']);
    for (const host of hosts.slice(1)) assert.deepEqual((await host.client.hosts.profiles(deadline())).profiles, []);
    const peer = await browser.newContext();
    const peerPage = await peer.newPage();
    await peerPage.goto(origin(local));
    for (const host of hosts) await peerPage.getByRole('region', { name: 'Projects', exact: true }).locator(`[data-sidebar-runtime="${host.runtimeId}"][data-sidebar-session="${host.root}"]`).waitFor();
    await peer.close();
    checks.push('UI saves two verified profiles only in Local config; a fresh browser discovers all three hosts');

    // Go in the nested native directory form must not create a session or
    // submit the draft. Explicit first-message submission owns both admissions.
    for (const host of hosts) {
      await page.getByRole('link', { name: 'New session', exact: true }).click();
      await page.getByRole('button', { name: 'Execution host', exact: true }).click();
      await page.getByRole('menuitem').filter({ hasText: host.name }).first().click();
      const before = frames.filter(frame => ['trees.create', 'sessions.submit'].includes(frame.method)).length;
      await page.getByRole('button', { name: 'Project folder', exact: true }).click();
      const picker = page.getByRole('dialog', { name: 'Choose a folder', exact: true });
      await picker.getByRole('button', { name: 'Edit path', exact: true }).click();
      await picker.getByLabel('Remote path', { exact: true }).fill(host.fixture.directory);
      await picker.getByRole('button', { name: 'Go', exact: true }).click();
      await expect(picker.getByText(host.fixture.directory, { exact: true })).toBeVisible();
      await picker.getByRole('button', { name: 'Choose folder', exact: true }).click();
      await expect(page.getByRole('button', { name: 'Project folder', exact: true })).toHaveAttribute('title', host.fixture.directory);
      assert.equal(frames.filter(frame => ['trees.create', 'sessions.submit'].includes(frame.method)).length, before, 'Directory browsing admitted work');
      await page.getByRole('textbox', { name: 'Your first message', exact: true }).fill(`First on ${host.name}`);
      await page.getByRole('button', { name: 'Send first message', exact: true }).click();
      await eventually(() => new URL(page.url()).pathname.startsWith(`/h/${host.runtimeId}/s/`));
      const created = new URL(page.url()).pathname.split('/').at(-1);
      const createdView = panes((await workspace()).layout).flatMap(pane => pane.tabs).find(tab => tab.rootId === created && tab.runtimeId === host.runtimeId);
      assert(createdView);
      await ready(createdView.id);
      const owner = await host.client.session(created).get(deadline());
      assert.equal(owner.working_directory, host.fixture.directory);
      assert.equal(owner.configuration.model.name, host.model, 'Creation used another host’s default model');
      assert.equal(owner.configuration.model.provider, 'host-fixture');
      for (const other of hosts.filter(value => value !== host)) await assert.rejects(other.client.session(created).get(deadline()), error => error.kind === 'NOT_FOUND');
      await action(createdView.id, 'Close tab');
      closedOwners.push({ id: created, closedAt: Date.now() });
    }
    checks.push('creation uses each host’s own folder and distinct model default; Go never submits creation');

    // Native inactive view leases remain warm for 30 seconds, then dispose.
    // Do not mistake already-open warm polls for search-triggered hydration.
    const warmMaximumObservations = maximumObservations;
    assert(warmMaximumObservations <= 16, 'Warm session view retention exceeded its documented bound');
    await eventually(() => {
      const now = Date.now();
      return closedOwners.every(owner => now - owner.closedAt > 30_750)
        && !frames.some(frame => frame.method === 'sessions.observe' && closedOwners.some(owner => owner.id === frame.params.session_id) && now - frame.at < 750);
    }, { timeout: 35_000, description: 'documented unused session-view expiry' });
    maximumObservations = activeObservations();

    // Build the mixed workspace through ordinary sidebar and tab-menu actions.
    for (const host of hosts) {
      await page.getByRole('region', { name: 'Projects', exact: true }).locator(`[data-sidebar-runtime="${host.runtimeId}"][data-sidebar-session="${host.root}"]`).getByRole('link', { name: `Shared title ${host.name}`, exact: true }).click();
      const opened = panes((await workspace()).layout).flatMap(pane => pane.tabs).find(tab => tab.rootId === host.root && tab.runtimeId === host.runtimeId);
      assert(opened); host.initialView = opened.id;
      await ready(opened.id);
    }
    await action(hosts[0].initialView, 'Split right');
    await action(hosts[1].initialView, 'Move to pane 2');
    await action(hosts[2].initialView, 'Split down');
    await tab(hosts[0].initialView).click();
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

    const remoteSession = hosts[1].client.session(hosts[1].root);
    const policy = await remoteSession.permissions.policy(deadline());
    await remoteSession.permissions.setMode({ expected_revision: policy.revision, mode: 'prompt' }, crypto.randomUUID(), deadline());
    await send(hosts[1].view, 'permission:multi-host');
    await panel(hosts[1].view).getByRole('button', { name: 'Allow once', exact: true }).waitFor();
    const pendingPermission = (await remoteSession.permissions.list({ pending_only: true }, deadline())).items[0];
    assert(pendingPermission);
    for (const foreign of [hosts[0], hosts[2]]) await assert.rejects(foreign.client.call('permissions.resolve', { operation_id: pendingPermission.operation_id, approved: true }, deadline()), error => error.kind === 'NOT_FOUND');
    const hydrationStart = frames.length;
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
    const visibleOwners = new Set(hosts.map(host => host.root));
    assert(frames.slice(hydrationStart).filter(frame => ['sessions.observe', 'sessions.history_page', 'sessions.turns'].includes(frame.method)).every(frame => visibleOwners.has(frame.params.session_id)), 'Search/attention opened hidden session observations');
    await panel(hosts[1].view).getByRole('button', { name: 'Allow once', exact: true }).click();
    await panel(hosts[1].view).getByRole('button', { name: 'Allow once', exact: true }).waitFor({ state: 'hidden' });
    await eventually(async () => (await remoteSession.permissions.list({}, deadline())).items.find(item => item.operation_id === pendingPermission.operation_id)?.state === 'approved', { description: 'Remote A approval committed' });
    await eventually(() => frames.some(frame => frame.method === 'permissions.resolve'), { description: 'captured UI approval request' });
    const decisions = frames.filter(frame => frame.method === 'permissions.resolve');
    assert.equal(decisions.length, 1);
    assert.equal(new URL(decisions[0].endpoint).host, new URL(remoteA.info.web).host);
    checks.push('search and permission attention label/filter hosts without root hydration; approval reaches Remote A only');

    await panel(hosts[2].view).locator('input[type=file]').setInputFiles({
      name: 'remote-note.txt', mimeType: 'text/plain', buffer: Buffer.from('Attachment stored on Remote B.'),
    });
    await panel(hosts[2].view).getByText('remote-note.txt · Ready', { exact: true }).waitFor();
    await send(hosts[2].view, 'Read the remote attachment.');
    const attached = await eventually(async () => (await hosts[2].client.session(hosts[2].root).history.page({ direction: 'backward', limit: 20 }, deadline())).messages.find(message => message.role === 'user' && message.parts.some(part => part.type === 'text' && part.text === 'Read the remote attachment.')));
    const ref = attached.parts.find(part => part.type === 'content').reference_id;
    assert.equal(Buffer.from(await hosts[2].client.session(hosts[2].root).content.readBytes(ref, deadline())).toString(), 'Attachment stored on Remote B.');
    for (const host of hosts.slice(0, 2)) await assert.rejects(host.client.session(host.root).content.get(ref, deadline()), error => error.kind === 'NOT_FOUND');
    const attachedRow = panel(hosts[2].view).locator(`[data-reading-seq="${attached.sequence}"]`);
    await attachedRow.getByRole('button', { name: 'Preview Attachment 1', exact: true }).click();
    const attachment = page.getByRole('dialog', { name: 'Attachment 1', exact: true });
    await expect(attachment).toContainText('Attachment stored on Remote B.');
    await attachment.getByRole('button', { name: 'Close', exact: true }).click();
    for (const host of hosts.slice(0, 2)) assert.equal(await panel(host.view).getByText('Attachment stored on Remote B.', { exact: true }).count(), 0);
    checks.push('a remote file upload is materialized only in its Remote B conversation');

    for (const target of [hosts[1], hosts[0]]) {
      const survivor = hosts[2];
      await panel(target.view).getByLabel('Message WHIP', { exact: true }).fill(`Preserve ${target.name}`);
      const effectsBeforeRestart = await target.fixture.effects();
      const epochBeforeRestart = target.client.processEpoch;
      await target.fixture.crashAndRestart({ beforeRestart: async () => {
        await eventually(() => [...sockets.keys()].every(socket => new URL(socket.url()).host !== new URL(target.fixture.info.web).host), { description: `${target.name} socket disconnect` });
        await ready(survivor.view);
        await send(survivor.view, `Remote B while ${target.name} is offline`);
        await eventually(async () => (await remoteB.effects()).includes(`Remote B while ${target.name} is offline`));
        await page.getByRole('button', { name: 'Search sessions', exact: true }).click();
        await expect(search.getByRole('region', { name: `${target.name} search results`, exact: true }).getByRole('status')).toContainText('offline');
        await search.getByRole('region', { name: 'Remote B search results', exact: true }).getByRole('link').first().waitFor();
        await search.getByRole('button', { name: 'Close', exact: true }).click();
      } });
      target.client = await target.fixture.connect(`multi-host-${target.runtimeId}`);
      assert.equal(target.client.runtimeID, target.runtimeId);
      assert.notEqual(target.client.processEpoch, epochBeforeRestart);
      await ready(target.view);
      assert.deepEqual(await target.fixture.effects(), effectsBeforeRestart, 'Observation reconnect replayed provider work');
      assert.equal(await panel(target.view).getByLabel('Message WHIP', { exact: true }).inputValue(), `Preserve ${target.name}`);
      await send(target.view, `${target.name} after reconnect`);
      await eventually(async () => (await target.fixture.effects()).includes(`${target.name} after reconnect`));
    }
    checks.push('Remote A and Local process outages preserve healthy remote work, partial search, drafts and reconnect');

    // Explicit disconnect detaches observation; an accepted remote turn continues.
    await send(hosts[1].view, 'hold:detached');
    await panel(hosts[1].view).getByRole('button', { name: 'Pause this turn', exact: true }).waitFor();
    await manage(); await hostAction('Remote A', 'Disconnect');
    await eventually(() => [...sockets.keys()].every(socket => new URL(socket.url()).host !== new URL(remoteA.info.web).host), { description: 'explicit Remote A detach' });
    await remoteA.release('detached');
    await eventually(async () => (await hosts[1].client.session(hosts[1].root).activity(deadline())).active_turn === null);
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
    await eventually(async () => (await hosts[0].client.hosts.profiles(deadline())).profiles.length === 1);
    await back();
    await expect(panel(removedHost.view).getByRole('status').filter({ hasText: /^Session unavailable$/ })).toBeVisible();
    await expect(panel(removedHost.view).getByText(/is unavailable\. Connect it to continue this session\./)).toBeVisible();
    await expect(panel(removedHost.view).getByLabel('Message WHIP', { exact: true })).toHaveCount(0);
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
    const restoredProfile = (await hosts[0].client.hosts.profiles(deadline())).profiles.find(profile => profile.runtime_id === removedHost.runtimeId);
    assert.ok(restoredProfile);
    assert.notEqual(restoredProfile.id, profiles.find(profile => profile.runtime_id === removedHost.runtimeId).id);
    assert.ok(!(await remoteA.effects()).includes(unsent), 'Re-adding a host submitted its draft');
    checks.push('removing Remote A preserves its unavailable tab and draft; Local/B keep working and re-adding its runtime restores the draft');

    const layout = (await workspace()).layout;
    assert.ok(maximumObservations <= 4, `Before reload: ${maximumObservations} simultaneous session observations across all hosts`);
    // A full document replacement disposes that window's runtime. Playwright
    // may deliver old socket close events after the replacement has connected.
    sockets.clear();
    await page.reload();
    for (const host of hosts) await ready(host.view);
    assert.equal(await panel(removedHost.view).getByLabel('Message WHIP', { exact: true }).inputValue(), unsent);
    assert.deepEqual((await workspace()).layout, layout);
    assert.equal(await page.locator('[data-workspace-frame]').count(), 3);
    assert.ok(maximumObservations <= 4, `Observed ${maximumObservations} simultaneous session observations across all hosts`);
    assert.ok(activeObservations() <= 4);
    assert.deepEqual(errors, []); assert.deepEqual(cspErrors, []);
    assert.equal(await page.getByRole('button', { name: 'Dismiss error', exact: true }).count(), 0, 'Host lifecycle operations left a global error banner');
    await page.screenshot({ path: join(directory, `${name}-three-hosts.png`) });
    checks.push('reload restores the mixed layout; all host connections together retain at most four session observations');
    results[name] = { browser: await browser.version(), rendererDigest: manifest.digest, warmMaximumObservations, maximumObservations, checks, errors, cspErrors };
    await writeFile(join(directory, 'multiple-hosts.json'), JSON.stringify(results, null, 2));
    console.log(`${name}: ${checks.length} multiple-host workflows passed`);
  } catch (error) {
    failure = error;
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${await page?.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}\n\n${fixtures.map(fixture => fixture.output).join('\n')}`);
    await writeFile(join(directory, `${name}-frames.json`), JSON.stringify(frames, null, 2));
    throw error;
  } finally {
    const cleanup = await Promise.allSettled([browser?.close(), ...fixtures.map(fixture => fixture.close())]);
    const failures = cleanup.filter(result => result.status === 'rejected').map(result => result.reason);
    if (failure) for (const error of failures) console.error(error);
    else if (failures.length) throw new AggregateError(failures, 'Multiple-host fixture cleanup failed');
  }
}
