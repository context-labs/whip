// Native host integration against staged production assets. Shipping-fuse and
// signed Finder acceptance are separate; this diagnostic uses stock Electron.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { lstat, mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { _electron } from 'playwright';
import { expect } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { fileDigest, LocalRuntime, readRuntimeManifest } from '../src/runtime.ts';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';
import { startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

const exec = promisify(execFile);
// Keep both the runtime and its isolated TMPDIR below macOS's Unix socket limit.
const fixture = await mkdtemp('/tmp/whip-desktop-smoke-');
const stage = path.join(repositoryRoot, 'apps/desktop/.stage');
const executable = path.join(fixture, 'bin/whipcode');
const env = { PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh',
  HOME: path.join(fixture, 'user'), TMPDIR: path.join(fixture, 'tmp'),
  WHIP_DESKTOP_FIXTURE: '1', WHIPCODE_HOME: path.join(fixture, 'home'), WHIPCODE_NETWORK: '0',
  WHIP_DESKTOP_EXECUTABLE: executable,
  WHIP_DESKTOP_USER_DATA: path.join(fixture, 'user-data') };
for (const directory of [env.HOME, env.TMPDIR, env.WHIPCODE_HOME, env.WHIP_DESKTOP_USER_DATA]) {
  await mkdir(directory, { mode: 0o700 });
}
let electron;
let remote;
let runtimeInstalled = false;
try {
  const manifest = await readRuntimeManifest(path.join(stage, 'app/runtime-manifest.json'));
  await new LocalRuntime({ source: path.join(stage, 'native'), manifest, env,
    settingsFile: path.join(env.WHIP_DESKTOP_USER_DATA, 'native-local-runtime.json'), defaultExecutable: executable })
    .install(executable, AbortSignal.timeout(15_000));
  runtimeInstalled = true;
  remote = await startFixture({ allowedOrigins: ['whip-app://bundle'] });
  const artifacts = process.env.WHIP_DESKTOP_SMOKE_ARTIFACTS;
  if (artifacts) await mkdir(artifacts, { recursive: true });
  const launch = () => _electron.launch({ args: [path.join(stage, 'app')], env, timeout: 30_000,
    ...(artifacts ? { recordVideo: { dir: artifacts, size: { width: 1280, height: 800 } } } : {}) });
  electron = await launch();
  let diagnostics = '';
  electron.process().stderr?.on('data', bytes => { diagnostics = (diagnostics + bytes.toString()).slice(-8192); });
  const page = await electron.firstWindow();
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  await page.waitForFunction(() => JSON.parse(localStorage.getItem('whip.hosts.v2') || '[]').some(host => host.id === 'local' && host.runtimeId), undefined, { timeout: 30_000 })
    .catch(async error => { console.error((await page.locator('body').innerText()).slice(0, 6000), diagnostics); throw error; });
  const { stdout } = await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 });
  const initial = JSON.parse(stdout);
  assert.equal(initial.state, 'running'); assert(!initial.network_endpoint, 'Local attachment must not enable TCP');
  assert.equal(await fileDigest(executable), manifest.files.whipcode.sha256);
  await assert.rejects(lstat(path.join(env.WHIP_DESKTOP_USER_DATA, 'runtimes')), { code: 'ENOENT' });
  await assert.rejects(lstat(path.join(env.HOME, '.whip')), { code: 'ENOENT' });
  await page.locator('#whip-session-navigation').getByRole('button', { name: 'Manage servers', exact: true }).click();
  await page.getByRole('button', { name: 'Add server', exact: true }).click();
  const hosts = page.getByRole('dialog', { name: 'Add server', exact: true });
  await hosts.getByRole('textbox', { name: /Server name/ }).fill('Smoke URL');
  await hosts.getByRole('textbox', { name: 'Server address', exact: true }).fill(remote.info.endpoint);
  await hosts.getByRole('button', { name: 'Connect', exact: true }).click();
  await hosts.waitFor({ state: 'hidden' });
  await page.getByRole('button', { name: 'Back to workspace', exact: true }).click();
  const remoteLink = page.locator(`a[href="/h/${remote.info.runtime_id}/s/${remote.info.root_id}"]`).first();
  await remoteLink.waitFor(); await remoteLink.click();
  await page.getByLabel('Message WHIP', { exact: true }).waitFor();
  const client = createWhipClient({ endpoint: remote.info.endpoint, clientId: `desktop-tabs-${crypto.randomUUID()}`, clientKind: 'human' });
  try {
    await client.connect();
    for (const title of ['Review changes', 'Check the implementation']) {
      const created = await client.sessions.create({ cwd: remote.directory, model: 'model', provider: 'provider' }).result();
      assert.equal(created.status, 'succeeded');
      const id = created.result.root_id;
      await client.session(id).rename(title).result();
      const link = page.locator(`a[href="/h/${remote.info.runtime_id}/s/${id}"]`).first();
      await link.waitFor(); await link.click();
      await page.getByLabel('Message WHIP', { exact: true }).waitFor();
    }
  } finally { client.close(); }
  await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' })));
  await page.reload();
  await expect(page.getByRole('tab')).toHaveCount(3);
  await page.getByLabel('Message WHIP', { exact: true }).fill('Preserve this desktop draft while moving the tab.');
  const tabs = page.locator('[data-workspace-tab]');
  const order = await tabs.evaluateAll(items => items.map(item => item.dataset.workspaceTab));
  // Retain only geometry for this drag; never session text or application state.
  const dragDiagnostics = await tabs.last().evaluateHandle(source => {
    const strip = source.parentElement;
    const snapshot = () => ({ source: source.getBoundingClientRect().toJSON(), sourceTransform: getComputedStyle(source).transform, sourceOffsetWidth: source.offsetWidth,
      strip: { rect: strip.getBoundingClientRect().toJSON(), scrollLeft: strip.scrollLeft, scrollWidth: strip.scrollWidth, clientWidth: strip.clientWidth, offsetWidth: strip.offsetWidth },
      viewport: { width: innerWidth, height: innerHeight, devicePixelRatio }, fonts: document.fonts.status });
    const data = { capture: snapshot() };
    const events = new AbortController();
    const pointer = event => {
      data.pointer = { x: event.clientX, y: event.clientY, buttons: event.buttons };
      if (event.type === 'pointerdown') data.pointerDown = { ...snapshot(), pointer: data.pointer };
    };
    for (const type of ['pointerdown', 'pointermove']) document.addEventListener(type, pointer, { capture: true, passive: true, signal: events.signal });
    const observer = new MutationObserver(() => {
      if (source.hasAttribute('data-dragging') && !data.activation) data.activation = { ...snapshot(), pointer: data.pointer };
    });
    observer.observe(source, { attributes: true, attributeFilter: ['data-dragging'] });
    return { data, snapshot, stop: () => { events.abort(); observer.disconnect(); } };
  });
  const source = await tabs.last().boundingBox(), target = await tabs.first().boundingBox();
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'desktop-tabs.png') });
  await page.mouse.move(source.x + 40, source.y + source.height / 2); await page.mouse.down();
  await page.mouse.move(source.x + 48, source.y + source.height / 2);
  const x = target.x + target.width / 2 - 8;
  await page.mouse.move(x, target.y + target.height / 2, { steps: 20 });
  const preview = page.locator('[data-workspace-drag-preview]');
  await expect(preview).toHaveCount(1);
  await expect.poll(async () => Math.abs((await preview.boundingBox()).x + 40 - x)).toBeLessThan(2).catch(async error => {
    try {
      const geometry = await dragDiagnostics.evaluate(({ data, snapshot }) => {
        const preview = document.querySelector('[data-workspace-drag-preview]');
        return { ...data, failure: snapshot(), preview: preview && { rect: preview.getBoundingClientRect().toJSON(), transform: getComputedStyle(preview).transform, offsetWidth: preview.offsetWidth } };
      });
      const evidence = JSON.stringify({ recordedAt: new Date().toISOString(), source, target, commandedPointer: { x, y: target.y + target.height / 2 }, expectedPickupOffset: 40, ...geometry }, null, 2);
      console.error('Desktop drag geometry:', evidence);
      if (artifacts) await writeFile(path.join(artifacts, 'desktop-drag-failure.json'), evidence + '\n');
    } catch (diagnosticError) { console.error('Could not capture desktop drag geometry:', diagnosticError); }
    if (artifacts) await page.screenshot({ path: path.join(artifacts, 'desktop-drag-failure.png') })
      .catch(diagnosticError => console.error('Could not capture desktop drag screenshot:', diagnosticError));
    throw error;
  }).finally(async () => {
    await dragDiagnostics.evaluate(({ stop }) => stop()).catch(() => {});
    await dragDiagnostics.dispose().catch(() => {});
  });
  assert.deepEqual(await tabs.evaluateAll(items => items.map(item => item.dataset.workspaceTab)), order);
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'desktop-tabs-dragging.png') });
  await page.mouse.up();
  await expect(preview).toHaveCount(0);
  await expect.poll(() => tabs.evaluateAll(items => items.map(item => item.dataset.workspaceTab))).toEqual([order[2], order[0], order[1]]);
  await expect(page.getByLabel('Message WHIP', { exact: true })).toHaveValue('Preserve this desktop draft while moving the tab.');
  // A sidebar drag copies the session into a fresh view, not a new session or a moved tab.
  const sidebarSession = page.locator('#whip-session-navigation').getByRole('link', { name: 'Check the implementation', exact: true });
  const sidebarBox = await sidebarSession.boundingBox(), firstTab = await tabs.first().boundingBox();
  const stripBox = await page.locator('[data-workspace-tab-strip]').boundingBox();
  assert(sidebarBox && firstTab && stripBox);
  const windowBounds = await electron.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].getBounds());
  const beforeCopy = await tabs.evaluateAll(items => items.map(item => item.dataset.workspaceTab));
  await page.mouse.move(sidebarBox.x + 50, sidebarBox.y + sidebarBox.height / 2);
  await page.mouse.down();
  await page.mouse.move(sidebarBox.x + 58, sidebarBox.y + sidebarBox.height / 2);
  // The exposed strip above the contoured tabs is normally a native drag region.
  await page.mouse.move(firstTab.x + 8, stripBox.y + 2, { steps: 20 });
  await expect(preview).toHaveCount(1);
  await expect(tabs).toHaveCount(3);
  if (artifacts) await page.screenshot({ path: path.join(artifacts, 'desktop-sidebar-dragging.png') });
  await page.mouse.up();
  await expect(preview).toHaveCount(0);
  await expect(tabs).toHaveCount(4);
  const afterCopy = await tabs.evaluateAll(items => items.map(item => item.dataset.workspaceTab));
  assert(!beforeCopy.includes(afterCopy[0]), 'Sidebar drop must allocate a fresh view ID');
  assert.deepEqual(afterCopy.slice(1), beforeCopy, 'Existing views must stay in place');
  await expect(tabs.first().getByRole('tab')).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByLabel('Message WHIP', { exact: true })).toHaveValue('Preserve this desktop draft while moving the tab.');
  assert.deepEqual(await electron.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].getBounds()), windowBounds,
    'Sidebar drag must not move the native window');
  // Native titlebar regions must not steal content-edge drags either.
  const splitViews = new Set();
  const originalPanel = page.locator(`[data-workspace-view="${afterCopy[0]}"]`);
  await originalPanel.getByLabel('Message WHIP', { exact: true }).evaluate(element => { window.__desktopOriginalDraft = element; });
  for (const edge of ['left', 'right', 'top', 'bottom']) {
    const from = await sidebarSession.boundingBox(), slot = await page.locator('[data-workspace-slot]').boundingBox();
    assert(from && slot);
    await page.mouse.move(from.x + 50, from.y + from.height / 2); await page.mouse.down();
    await page.mouse.move(from.x + 59, from.y + from.height / 2);
    await page.mouse.move(slot.x + slot.width * (edge === 'left' ? .01 : edge === 'right' ? .99 : .5),
      slot.y + slot.height * (edge === 'top' ? .01 : edge === 'bottom' ? .99 : .5), { steps: 20 });
    await expect(page.locator(`[data-workspace-drop="${edge}"]`)).toBeVisible();
    await expect(tabs).toHaveCount(4);
    if (artifacts) await page.screenshot({ path: path.join(artifacts, `desktop-sidebar-${edge}-preview.png`) });
    await page.mouse.up();
    await expect(preview).toHaveCount(0);
    await expect(page.locator('[data-workspace-frame]')).toHaveCount(2);
    await expect(tabs).toHaveCount(5);
    const ids = await tabs.evaluateAll(items => items.map(item => item.dataset.workspaceTab));
    assert.deepEqual(ids.filter(id => afterCopy.includes(id)), afterCopy, `${edge}: moved existing desktop views`);
    const added = ids.find(id => !afterCopy.includes(id));
    assert(added && !splitViews.has(added), `${edge}: reused an existing split view`);
    splitViews.add(added);
    const addedTab = page.locator(`[data-workspace-tab="${added}"]`);
    await expect(addedTab.getByRole('tab')).toHaveAttribute('aria-selected', 'true');
    await expect(page.locator(`[data-workspace-view="${added}"]`).getByLabel('Message WHIP', { exact: true })).toHaveValue('Preserve this desktop draft while moving the tab.');
    assert.equal(await originalPanel.getByLabel('Message WHIP', { exact: true }).evaluate(element => element === window.__desktopOriginalDraft), true);
    assert.deepEqual(await electron.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].getBounds()), windowBounds,
      `${edge}: sidebar split moved the native window`);
    await addedTab.getByRole('tab').focus(); await page.keyboard.press('Delete');
    await expect(page.locator('[data-workspace-frame]')).toHaveCount(1);
    await expect(tabs).toHaveCount(4);
  }
  if (process.env.WHIP_WEB_TURN_FAILURE_FIXTURE === '1') {
    const route = new URL(page.url());
    route.pathname = `/h/${remote.info.runtime_id}/s/${remote.info.root_id}`;
    route.search = '?agent=turn-failed-empty&view=repl';
    await page.goto(route.href);
    const notice = page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true });
    await expect(notice).toHaveCount(1);
    await expect(notice).toContainText('Architecture researcher');
    await expect(page.getByText('The last turn failed', { exact: true })).toBeVisible();
    if (artifacts) await page.screenshot({ path: path.join(artifacts, 'desktop-turn-failure.png') });
    await page.reload();
    await expect(notice).toHaveCount(1);
    const response = await fetch(`${remote.info.frontend}/control/turn-outcome/succeed`, { method: 'POST' });
    assert.ok(response.ok, await response.text());
    await expect(notice).toHaveCount(0);
    route.search = '?agent=turn-failed-empty';
    await page.goto(route.href);
    await expect(page.getByText('Follow-up completed.', { exact: true })).toBeVisible();
  }
  await page.locator('#whip-session-navigation').getByRole('button', { name: 'Manage servers', exact: true }).click();
  const openActions = name => page.getByRole('button', { name: `Actions for ${name}`, exact: true }).click();
  await openActions('Smoke URL');
  await page.getByRole('menuitem', { name: 'Disconnect', exact: true }).click();
  const disconnect = page.getByRole('alertdialog', { name: 'Disconnect Smoke URL?', exact: true });
  await disconnect.getByRole('button', { name: 'Disconnect', exact: true }).click();
  await disconnect.waitFor({ state: 'hidden' });
  await openActions('Smoke URL');
  await expect(page.getByRole('menuitem', { name: 'Connect', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await openActions('This Mac');
  await expect(page.getByRole('menuitem', { name: 'Disconnect', exact: true }), 'Disconnecting URL detached This Mac').toBeVisible();
  await page.keyboard.press('Escape');
  await openActions('Smoke URL');
  await page.getByRole('menuitem', { name: 'Connect', exact: true }).click();
  await openActions('Smoke URL');
  await page.getByRole('menuitem', { name: 'Disconnect', exact: true }).waitFor();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Back to workspace', exact: true }).click();
  await page.getByRole('link', { name: 'Settings', exact: true }).first().click();
  await page.getByRole('heading', { name: 'General', exact: true }).waitFor();
  await page.reload();
  await page.getByRole('heading', { name: 'General', exact: true }).waitFor();
  assert.deepEqual(errors, []);
  // Force only the fixture GUI process to disappear: daemon lifetime is separate.
  await electron.evaluate(({ app }) => app.exit(0)).catch(error => {
    if (!/closed|destroyed/.test(error.message)) throw error;
  });
  electron = undefined;
  const survived = JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);
  assert.equal(survived.state, 'running'); assert.equal(survived.pid, initial.pid);
  electron = await launch();
  const reopened = await electron.firstWindow();
  // The saved identity exists before reconnect. Require a host-dependent enabled
  // control to prove the new renderer actually attached to the daemon.
  await reopened.locator('[data-settings-layout], [aria-label="Session navigation"]').first().waitFor();
  if (await reopened.locator('[data-settings-layout]').isVisible()) {
    await reopened.getByRole('button', { name: 'Servers', exact: true }).click();
  } else {
    await reopened.locator('#whip-session-navigation').getByRole('button', { name: 'Manage servers', exact: true }).click();
  }
  for (const name of ['This Mac', 'Smoke URL']) {
    await reopened.getByRole('button', { name: `Actions for ${name}`, exact: true }).click();
    await reopened.getByRole('menuitem', { name: 'Disconnect', exact: true }).waitFor();
    await reopened.keyboard.press('Escape');
  }
  const attached = JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);
  assert.equal(attached.pid, initial.pid);
  const result = { purpose: 'Staged host integration; not signed/fused installed-app acceptance or a startup benchmark',
    recordedAt: new Date().toISOString(),
    ...(process.env.WHIP_WEB_TURN_FAILURE_FIXTURE === '1' ? { savedTurnFailure: true, failureClearsOnSuccess: true } : {}),
    noNetwork: !initial.network_endpoint, canonicalRuntimeInstalled: true, noRetainedRuntime: true, noLegacyHome: true, daemonSurvivedGUIExit: true,
    relaunchAttachedSameDaemon: true, settingsReload: true, desktopOriginURL: true, independentHostDisconnect: true, multipleHostsRestored: true, movingTabPreview: true, tabReorderPreservesDraft: true, sidebarDragCreatesFreshView: true, sidebarEdgeSplits: [...splitViews].length === 4, sidebarDragPreservesWindowBounds: true, rendererErrors: errors };
  await writeFile(process.env.WHIP_DESKTOP_SMOKE_OUTPUT ?? path.join(repositoryRoot, '.ai-docs/plans/desktop-app/evidence/local-smoke.json'), JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result, null, 2));
} finally {
  if (electron) await electron.evaluate(({ app }) => app.exit(0)).catch(() => {});
  // Canonical fixture executables must stay present if cleanup cannot stop their owner.
  // In particular, do not turn a shutdown failure into a deleted live runtime.
  try {
    if (runtimeInstalled) {
      await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
      const stopped = JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);
      assert.equal(stopped.state, 'stopped');
    }
    await rm(fixture, { recursive: true, force: true });
  } catch (error) {
    console.error(`Preserved smoke fixture after cleanup failure: ${fixture}`);
    throw error;
  } finally { await remote?.close(); }
}
