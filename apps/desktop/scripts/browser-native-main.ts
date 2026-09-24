import { app, BrowserWindow, dialog, type WebContents } from 'electron';
import assert from 'node:assert/strict';
import http from 'node:http';
import path from 'node:path';
import { EventEmitter, once } from 'node:events';
import { readFileSync } from 'node:fs';
import { testBrowserControlRegressions } from './browser-control-regressions';
import { testBrowserControl } from './browser-control-native';
import { testBrowserDiscovery } from './browser-discovery-native';
import { BrowserManager } from '../src/browser-manager';
import { nativeHumanPreviewConfirmation } from '../src/browser-human-confirmation';
import { ScopedBrowserDebugger } from '../src/browser-cdp';
import { installBrowserIPC } from '../src/browser-ipc';
import type { BrowserInventory, BrowserTabState, BrowserTarget } from '@whip/app/desktop-bridge';

const directory = process.env.BROWSER_NATIVE_DIRECTORY!;
app.setPath('userData', path.join(directory, 'profile'));
let window: BrowserWindow, manager: BrowserManager, cleanupIPC: () => void;
let permitUnload = false;
const hits = new Map<string, number>();
const server = http.createServer((request, response) => {
  const url = request.url!; hits.set(url, (hits.get(url) ?? 0) + 1);
  if (url === '/workspace.js') { response.setHeader('Content-Type', 'application/javascript'); response.end(readFileSync(path.join(directory, 'workspace.js'))); return; }
  response.setHeader('Content-Type', 'text/html');
  if (url === '/shell') response.end('<!doctype html><h1>Application fixture</h1><iframe src="/frame"></iframe><script src="/workspace.js"></script>');
  else response.end('<!doctype html><title>Native page</title><input id="name"><h1>Browser fixture</h1>');
});
const pass = (name: string) => console.log('PASS', name);
const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));
async function run() {
  await app.whenReady(); server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const origin = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  const preferences = { preload: path.join(directory, 'preload.cjs'), additionalArguments: ['--whip-browser-tabs'], sandbox: true, contextIsolation: true, nodeIntegration: false, webviewTag: false };
  window = new BrowserWindow({ width: 1000, height: 760, show: true, webPreferences: { ...preferences, nodeIntegrationInSubFrames: true } });
  const disabled = new BrowserWindow({ show: false, webPreferences: { ...preferences, additionalArguments: [], preload: path.join(directory, 'browser-feature-native-preload.cjs') } });
  try {
    await disabled.loadURL(origin + '/feature-off');
    assert.deepEqual(await disabled.webContents.executeJavaScript('[typeof whipDesktop.browser, typeof whipDesktop.browserAgent]'), ['undefined', 'undefined']);
    for (const method of ['snapshot', 'identity']) assert.equal(await disabled.webContents.executeJavaScript(`browserFeatureTest.${method}().then(()=>false,()=>true)`), true);
    pass('disabled production preload omits Browser bridges and no Browser IPC handler is installed');
  } finally { disabled.destroy(); }
  for (const event of ['focus', 'blur', 'show', 'hide', 'resize'] as const) window.on(event, () => console.log('WINDOW_EVENT', event, { visible: window.isVisible(), focused: window.isFocused() }));
  const events: unknown[] = [];
  manager = new BrowserManager(window, event => { events.push(event); if (!window.isDestroyed()) window.webContents.send('whip:browser:event', event); }, { confirm: () => permitUnload, admissionTimeoutMs: 1500 });
  cleanupIPC = installBrowserIPC(window, manager, event => {
    if (event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame || event.senderFrame.url !== origin + '/shell') throw new Error('Untrusted browser request');
  });
  await window.loadURL(origin + '/shell');
  const call = <T>(method: string, input?: unknown): Promise<T> => window.webContents.executeJavaScript(`whipDesktop.browser.${method}(${input === undefined ? '' : JSON.stringify(input)})`);
  let inventory = await call<BrowserInventory>('snapshot');
  let epoch = inventory.epoch;
  assert.equal(inventory.tabs.length, 0);
  assert.match(await window.webContents.mainFrame.frames[0].executeJavaScript('whipDesktop.browser.snapshot().then(()=>"bad",e=>String(e))'), /Untrusted/);
  const attacker = new BrowserWindow({ show: false, webPreferences: preferences });
  await attacker.loadURL(origin + '/shell');
  assert.match(await attacker.webContents.executeJavaScript('whipDesktop.browser.snapshot().then(()=>"bad",e=>String(e))'), /Untrusted/);
  attacker.destroy();
  pass('production preload + production IPC reject foreign renderer and actual subframe');
  const boundary = await window.webContents.executeJavaScript('runWorkspaceBoundary().catch(error => ({ok:false,error:error.message,stack:error.stack}))');
  assert.equal(boundary.ok, true, JSON.stringify(boundary));
  assert.equal((await call<BrowserInventory>('snapshot')).tabs.length, 0);
  pass('actual BrowserWorkspace renderer + production preload/IPC: strict metadata projection restores addresses and first New Browser, raw workspace extras still rejected');
  events.length = 0;
  const outgoingEpoch = epoch;
  manager.resetRenderer(); inventory = await call<BrowserInventory>('snapshot'); epoch = inventory.epoch;
  assert.notEqual(epoch, outgoingEpoch);
  assert.ok(!events.some(event => (event as { snapshot?: BrowserInventory }).snapshot?.epoch === epoch),
    'The outgoing renderer must not receive the incoming epoch and spend its first presentation revision');
  await assert.rejects(call('present', { epoch: outgoingEpoch, revision: 100, blocked: true, slots: [] }), /Stale/);
  await call('present', { epoch, revision: 1, blocked: true, slots: [] });
  pass('renderer handoff keeps the fresh epoch private until snapshot; incoming overlays can present revision one');
  manager.resetRenderer(); inventory = await call<BrowserInventory>('snapshot'); epoch = inventory.epoch; events.length = 0;

  const confirmation = nativeHumanPreviewConfirmation(window, (parent, options) => dialog.showMessageBox(parent, options));
  const consent = new AbortController(); let sheetBegan = false;
  const sheet = () => { sheetBegan = true; }; window.on('sheet-begin', sheet);
  const abortConsent = setTimeout(() => consent.abort(), 1000);
  try {
    assert.equal(await confirmation('already-connected-fixture', {
      title: 'SSH preview confirmation regression', fields: [], confirmLabel: 'Open preview',
      message: 'Host: isolated fixture\nRuntime: fixture\nProject: /fixture\nURL: http://127.0.0.1:3000/\nApproved ports: 3000\nNo connection or authority will be created by this cancellation test.',
    }, consent.signal), null);
    assert.equal(consent.signal.aborted, true);
    if (process.platform === 'darwin') assert.equal(sheetBegan, true, 'Native sheet must actually open before cancellation');
  } finally { clearTimeout(abortConsent); consent.abort(); window.removeListener('sheet-begin', sheet); }
  pass('production human consent adapter opens a parented native sheet for an already-connected host and abort closes it without approval');

  const tabs: BrowserTabState[] = [];
  for (let i = 0; i < 8; i++) {
    const tab = await call<BrowserTabState>('create', { epoch, url: origin + '/page-' + i });
    tabs.push(tab); await call('admitted', { epoch, tabId: tab.id, generation: tab.generation });
  }
  assert.equal([...hits.keys()].filter(url => url.startsWith('/page-')).length, 0);
  await assert.rejects(call('create', { epoch, url: origin }), /eight Browser/);
  assert.ok(events.length >= 8);
  pass('new-create limit eight; metadata creation/admission is lazy and state precedes result');
  const target = (tab = tabs[0]): BrowserTarget => ({ epoch, tabId: tab.id, generation: tab.generation });
  const slots = tabs.slice(0, 4).map((tab, i) => ({ tabId: tab.id, slotId: `pane-${i}`, bounds: { x: (i % 2) * 400, y: Math.floor(i / 2) * 280 + 60, width: 390, height: 260 } }));
  await call('present', { epoch, revision: 1, blocked: false, slots });
  assert.equal(window.contentView.children.length, 4); // WebContentsViews are native child views.
  for (let i = 0; i < 4; i++) await call('act', { ...target(tabs[i]), action: { kind: 'navigate', url: origin + '/page-' + i } });
  const guest = await manager.controlledContents(target());
  assert.equal(guest.getURL(), origin + '/page-0');
  assert.deepEqual(await guest.executeJavaScript('({require:typeof require,process:typeof process,bridge:typeof whipDesktop})'), { require: 'undefined', process: 'undefined', bridge: 'undefined' });
  assert.notEqual(guest.session, window.webContents.session);
  await assert.rejects(call('act', { ...target(), action: { kind: 'navigate', url: 'file:///etc/passwd' } }));
  await assert.rejects(call('act', { ...target(), action: { kind: 'evaluate', code: '1' } }));
  await assert.rejects(call('act', { ...target(), generation: 'stale', action: { kind: 'stop' } }));
  pass('four production WebContentsViews, isolated website session and closed action validation');

  window.show(); window.focus(); app.focus({ steal: true }); await sleep(100);
  await call('act', { ...target(), action: { kind: 'focus' } });
  console.log('FOCUS_STATE', { window: window.id, visible: window.isVisible(), focused: window.isFocused(), guest: guest.id, guestFocused: guest.isFocused(), shellFocused: window.webContents.isFocused(), children: window.contentView.children.map(view => ({ visible: view.getVisible(), bounds: view.getBounds() })), focusedWindow: BrowserWindow.getFocusedWindow()?.id, openWindows: BrowserWindow.getAllWindows().map(item => ({ id: item.id, focused: item.isFocused(), visible: item.isVisible() })) });
  assert.ok(guest.isFocused());
  await call('present', { epoch, revision: 2, blocked: true, slots });
  assert.ok(window.contentView.children.every(view => !view.getVisible()));
  assert.ok(window.webContents.isFocused());
  await assert.rejects(call('present', { epoch, revision: 1, blocked: false, slots }), /Stale/);
  window.webContents.setZoomFactor(1.25);
  await call('present', { epoch, revision: 3, blocked: false, slots: [{ ...slots[0], bounds: { x: 10.25, y: 20.25, width: 301.5, height: 220.5 } }] });
  assert.deepEqual(window.contentView.children[0].getBounds(), { x: 13, y: 25, width: 377, height: 276 });
  window.webContents.setZoomFactor(1);
  await call('present', { epoch, revision: 4, blocked: false, slots });
  window.show(); window.focus(); app.focus({ steal: true }); await sleep(100);
  await call('act', { ...target(), action: { kind: 'focus' } });
  assert.ok(guest.isFocused());
  await guest.executeJavaScript('document.querySelector("#name").focus()');
  guest.sendInputEvent({ type: 'char', keyCode: 'x' }); await sleep(30);
  assert.equal(await guest.executeJavaScript('document.querySelector("#name").value'), 'x');
  guest.sendInputEvent({ type: 'keyDown', keyCode: 'l', modifiers: [process.platform === 'darwin' ? 'meta' : 'control'] }); await sleep(30);
  assert.ok(window.webContents.isFocused());
  assert.ok(events.some(event => (event as { shortcut?: string }).shortcut === 'address'));
  await call('act', { ...target(), action: { kind: 'focus' } });
  guest.sendInputEvent({ type: 'keyDown', keyCode: 'r', modifiers: [process.platform === 'darwin' ? 'meta' : 'control'] }); await sleep(30);
  assert.ok(guest.isFocused());
  // Native tab shortcuts belong to the application menu even with guest input
  // focused. Preventing these events would suppress the accelerators as well.
  for (const shift of [false, true]) {
    const modifiers = [process.platform === 'darwin' ? 'meta' : 'control', ...(shift ? ['shift'] : [])];
    const shortcutsBefore = events.filter(event => (event as { kind?: string }).kind === 'shortcut').length;
    const nextInput = once(guest, 'before-input-event');
    guest.sendInputEvent({ type: 'keyDown', keyCode: 't', modifiers });
    const [input] = await nextInput;
    assert.equal(input.defaultPrevented, false, 'guest must leave Cmd/Ctrl+[Shift+]T to the application menu');
    assert.equal(events.filter(event => (event as { kind?: string }).kind === 'shortcut').length, shortcutsBefore);
    guest.sendInputEvent({ type: 'keyUp', keyCode: 't', modifiers });
  }
  pass('guest Cmd/Ctrl+T and Cmd/Ctrl+Shift+T do not intercept native tab accelerators');
  pass('production present ACK hides all; stale layout rejected; app zoom and native focus work');

  let controlLive = true;
  const cdpEvents: string[] = [], revoked: string[] = [];
  const cdp = new ScopedBrowserDebugger(guest, tabs[0].id, { assertLive() { if (!controlLive) throw new Error('Control revoked'); }, event(value) { cdpEvents.push(value.method); }, revoked(reason) { revoked.push(reason); } });
  const command = (method: string, params: Record<string, unknown> = {}, sessionId = cdp.sessionId) => cdp.dispatch({ method, params, sessionId });
  assert.equal(((await command('Target.getTargets')) as { targetInfos: unknown[] }).targetInfos.length, 1);
  for (const [method, params] of [
    ['Browser.close', {}], ['Target.createTarget', { url: origin }], ['Target.attachToTarget', { targetId: 'foreign', flatten: true }],
    ['Target.getTargetInfo', { targetId: String(window.webContents.id) }], ['Target.sendMessageToTarget', {}],
    ['Storage.getCookies', {}], ['Network.getCookies', {}], ['DOM.setFileInputFiles', { files: ['/tmp/private'] }],
    ['Page.printToPDF', {}], ['Page.navigate', { url: 'file:///tmp/private' }],
  ] as Array<[string, Record<string, unknown>]>) await assert.rejects(command(method, params));
  await assert.rejects(command('Runtime.evaluate', { expression: '1' }, 'foreign-session'));
  await command('Runtime.enable'); await command('Page.enable');
  assert.ok(((await command('Accessibility.getFullAXTree')) as { nodes: unknown[] }).nodes.length > 0);
  assert.ok(((await command('DOM.getDocument')) as { root: { nodeId: number } }).root.nodeId);
  await command('Runtime.evaluate', { expression: 'document.querySelector("#name").value="";document.querySelector("#name").focus();document.querySelector("#name").oninput=e=>document.body.dataset.trusted=String(e.isTrusted)' });
  await command('Input.insertText', { text: 'trusted-native' });
  assert.equal(await guest.executeJavaScript('document.querySelector("#name").value'), 'trusted-native');
  assert.equal(await guest.executeJavaScript('document.body.dataset.trusted'), 'true');
  assert.ok(((await command('Page.captureScreenshot', { format: 'jpeg', quality: 70 })) as { data: string }).data.length > 1000);
  assert.ok(cdpEvents.includes('Runtime.executionContextCreated'));
  const abort = new AbortController();
  const uncertain = cdp.dispatch({ method: 'Runtime.evaluate', sessionId: cdp.sessionId, params: { expression: 'new Promise(r=>setTimeout(()=>{document.body.dataset.delivered="true";r(true)},100))', awaitPromise: true } }, abort.signal);
  setTimeout(() => abort.abort(), 10);
  await assert.rejects(uncertain, /outcome_unknown/); await sleep(130);
  assert.equal(await guest.executeJavaScript('document.body.dataset.delivered'), 'true');
  controlLive = false; await assert.rejects(command('Runtime.evaluate', { expression: '1' }), /revoked/);
  cdp.close(); assert.ok(!guest.debugger.isAttached()); assert.equal(revoked.length, 1);
  pass('production scoped debugger: Rod discovery, page DOM/AX/trusted input/JPEG, escape rejection, bounded cancellation uncertainty and revoke');

  await guest.executeJavaScript('window.onbeforeunload=()=>"unsaved";void 0', true);
  const cancelled = await call<{ status: string }>('close', target());
  assert.equal(cancelled.status, 'cancelled');
  assert.ok((await call<BrowserInventory>('snapshot')).tabs.some(tab => tab.id === tabs[0].id));
  permitUnload = true;
  assert.equal((await call<{ status: string }>('close', target())).status, 'closed');
  assert.ok(guest.isDestroyed());
  pass('production native beforeunload cancellation preserves page; confirmed close destroys it');

  for (const tab of tabs.slice(1)) await call('close', target(tab));
  const descriptors = Array.from({ length: 32 }, (_, i) => ({ id: `restored-${i}`, url: origin + '/restore-' + i, titleHint: 'Saved' }));
  inventory = await call<BrowserInventory>('restore', { epoch, tabs: descriptors });
  assert.equal(inventory.tabs.length, 32);
  assert.equal(inventory.tabs.filter(tab => tab.status === 'unavailable').length, 24);
  assert.equal(hits.get('/restore-0'), undefined);
  inventory = await call('restore', { epoch, tabs: [{ ...descriptors[0], url: origin + '/stale' }] });
  assert.equal(inventory.tabs[0].url, origin + '/restore-0');
  await assert.rejects(call('create', { epoch, url: origin }), /eight Browser/);
  pass('production restore preserves32 metadata, marks excess unavailable and never overwrites live URL');

  const selected = inventory.tabs[0];
  await call('present', { epoch, revision: 5, blocked: false, slots: [{ tabId: selected.id, slotId: 'pane', bounds: slots[0].bounds }] });
  const selectedTarget = { epoch, tabId: selected.id, generation: selected.generation };
  const crashing = await manager.controlledContents(selectedTarget);
  await call('act', { ...selectedTarget, action: { kind: 'navigate', url: origin + '/crash' } });
  const crashed = once(crashing, 'destroyed'); crashing.forcefullyCrashRenderer(); await crashed;
  assert.equal((await call<BrowserInventory>('snapshot')).tabs[0].status, 'crashed');
  assert.ok(!window.webContents.isDestroyed());
  const crashedState = (await call<BrowserInventory>('snapshot')).tabs[0];
  const recoveredTarget = { ...selectedTarget, generation: crashedState.generation };
  assert.notEqual(recoveredTarget.generation, selectedTarget.generation);
  await call('act', { ...recoveredTarget, action: { kind: 'navigate', url: origin + '/recovered' } });
  assert.equal((await call<BrowserInventory>('snapshot')).tabs[0].status, 'ready');
  const recoveredContents = await manager.controlledContents(recoveredTarget);
  manager.resetRenderer();
  await assert.rejects(call('act', { ...selectedTarget, action: { kind: 'reload' } }), /Stale/);
  assert.ok(window.contentView.children.every(view => !view.getVisible()));
  pass('guest crash isolated and old renderer epoch cannot mutate/present');
  const destroyed = once(recoveredContents, 'destroyed'); manager.dispose(); await destroyed; assert.ok(recoveredContents.isDestroyed());
  cleanupIPC();
  await assert.rejects(call('snapshot'), /No handler/);
  pass('manager disposal destroys owned guests and unregisters the real IPC handlers');
  await testBrowserControl(window, origin);
  await testBrowserControlRegressions(window, origin);
  await testBrowserDiscovery(window, directory);

  // Regression: native BrowserWindow.webContents throws after close. No pre-dispose is allowed.
  const emitter = new EventEmitter(), shellEmitter = new EventEmitter(); let fakeClosed = false;
  Object.defineProperty(emitter, 'webContents', { get() { if (fakeClosed) throw new Error('Object has been destroyed'); return shellEmitter; } });
  Object.assign(emitter, { isDestroyed: () => fakeClosed });
  const fakeManager = new BrowserManager(emitter as unknown as BrowserWindow, () => {});
  assert.equal(shellEmitter.listenerCount('render-process-gone'), 1);
  fakeClosed = true; assert.doesNotThrow(() => emitter.emit('closed'));
  assert.equal(shellEmitter.listenerCount('render-process-gone'), 0);
  assert.equal(shellEmitter.listenerCount('did-start-navigation'), 0);
  assert.doesNotThrow(() => fakeManager.dispose());

  for (const enabled of [false, true]) {
    const closing = new BrowserWindow({ show: enabled, width: 600, height: 420, webPreferences: { ...preferences, additionalArguments: enabled ? ['--whip-browser-tabs'] : [] } });
    const owned = new BrowserManager(closing, () => {});
    let liveGuest: WebContents | undefined;
    try {
      await closing.loadURL(origin + '/shell');
      assert.equal(await closing.webContents.executeJavaScript('typeof whipDesktop.browser'), enabled ? 'object' : 'undefined');
      if (enabled) {
        const epoch = owned.snapshot().epoch;
        const tab = owned.create({ epoch, url: origin + '/window-close' });
        const target = { epoch, tabId: tab.id, generation: tab.generation }; owned.admitted(target);
        await owned.present({ epoch, revision: 1, blocked: false, slots: [{ tabId: tab.id, slotId: 'close-test', bounds: { x: 0, y: 40, width: 560, height: 340 } }] });
        liveGuest = await owned.controlledContents(target);
        assert.equal(liveGuest.isDestroyed(), false); assert.ok(closing.contentView.children[0].getVisible());
      }
      const closed = once(closing, 'closed'); closing.close(); await closed;
      await sleep(50);
      assert.equal(closing.isDestroyed(), true);
      if (liveGuest) assert.equal(liveGuest.isDestroyed(), true);
      assert.throws(() => owned.snapshot(), /window is closed/);
      assert.doesNotThrow(() => owned.dispose());
    } finally { if (!closing.isDestroyed()) closing.destroy(); }
    pass(`actual BrowserWindow.close disposes safely without pre-dispose: feature ${enabled ? 'enabled with live guest' : 'disabled with no guest'}`);
  }
}
app.on('window-all-closed', () => {});
run().then(() => { console.log('NATIVE_MANAGER_OK'); app.exit(0); }).catch(error => { console.error(error); app.exit(1); }).finally(() => {
  manager?.dispose(); cleanupIPC?.(); server.closeAllConnections(); server.close(); if (window && !window.isDestroyed()) window.destroy();
});
