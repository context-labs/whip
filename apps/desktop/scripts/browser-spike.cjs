// Disposable Phase 0 native proof. Not the production browser manager.
// Run: node_modules/electron/dist/Electron.app/Contents/MacOS/Electron apps/desktop/scripts/browser-spike.cjs
const { app, BrowserWindow, WebContentsView, ipcMain, desktopCapturer, systemPreferences } = require('electron');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const http = require('node:http');
const crypto = require('node:crypto');
const { once } = require('node:events');
const { WebSocketServer } = require('ws');

const root = require('node:fs').mkdtempSync(path.join(os.tmpdir(), 'whip-browser-native-'));
app.setPath('userData', path.join(root, 'profile'));
const evidence = { electron: process.versions.electron, platform: process.platform, arch: process.arch, root, checks: [], observations: {} };
const views = [];
let window, server, sockets, control;
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const check = (name, detail) => { evidence.checks.push({ name, detail }); console.log('PASS', name, JSON.stringify(detail ?? null)); };
const deadline = (promise, label, ms = 5000) => Promise.race([promise, new Promise((_, reject) => setTimeout(() => reject(new Error('Timeout: ' + label)), ms).unref())]);
const allowedURL = value => { const url = new URL(value); if (!['http:', 'https:'].includes(url.protocol) && value !== 'about:blank') throw new Error('unsafe URL'); if (url.username || url.password) throw new Error('credentials'); return url.href; };
let layoutRevision = 0;

async function run() {
  await app.whenReady();
  server = http.createServer((request, response) => {
    if (request.url === '/download') { response.writeHead(200, { 'Content-Disposition': 'attachment; filename=escape.txt' }); response.end('not saved'); return; }
    if (request.url === '/redirect-file') { response.writeHead(302, { Location: 'file:///etc/passwd' }); response.end(); return; }
    response.setHeader('Content-Type', 'text/html; charset=utf-8');
    if (request.url === '/shell') { response.end('<!doctype html><style>body{margin:0;background:#202030;color:white}#overlay{position:fixed;inset:0;background:#604080;display:none}</style><h1>Native fixture shell</h1><div id=overlay>APPLICATION OVERLAY</div><iframe src="/frame"></iframe>'); return; }
    if (request.url === '/frame') { response.end('<!doctype html><input aria-label="Frame input" id="frame-input"><p>frame content</p>'); return; }
    response.end(`<!doctype html><title>Guest ${request.url}</title><style>body{background:#176541;color:white;font:20px sans-serif}input{font:inherit}</style><h1>Visible guest</h1><input id="name" aria-label="Name"><button id="act" onclick="document.querySelector('#result').textContent='clicked'">Act</button><p id="result">ready</p><a id="next" href="/next">Next</a><iframe src="/frame"></iframe><input id="upload" type="file">`);
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const origin = 'http://127.0.0.1:' + server.address().port;
  const prefs = { sandbox: true, contextIsolation: true, nodeIntegration: false, webSecurity: true, webviewTag: false };
  window = new BrowserWindow({ width: 1000, height: 760, show: true, webPreferences: { ...prefs, preload: path.join(__dirname, 'browser-spike-preload.cjs'), nodeIntegrationInSubFrames: true } });
  const trusted = event => {
    if (event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame || event.senderFrame.url !== origin + '/shell') throw new Error('Untrusted browser request');
  };
  for (const [method, action] of Object.entries({
    ping: () => 'pong',
    present: value => {
      assert.ok(Number.isSafeInteger(value.revision) && value.revision > layoutRevision, 'stale layout');
      assert.ok(Array.isArray(value.slots) && value.slots.length <= 4);
      for (const slot of value.slots) {
        assert.ok(Number.isInteger(slot.id) && views[slot.id]);
        for (const number of Object.values(slot.rect)) assert.ok(Number.isFinite(number) && number >= 0 && number <= 10000);
      }
      layoutRevision = value.revision;
      for (let id = 0; id < views.length; id++) {
        const slot = value.slots.find(slot => slot.id === id);
        if (!slot || value.overlayBlocked) { views[id].setVisible(false); continue; }
        const z = window.webContents.getZoomFactor(), r = slot.rect;
        const x = Math.round(r.x * z), y = Math.round(r.y * z);
        views[id].setBounds({ x, y, width: Math.round((r.x + r.width) * z) - x, height: Math.round((r.y + r.height) * z) - y });
        views[id].setVisible(true);
      }
      return views.map(view => ({ bounds: view.getBounds(), visible: view.getVisible() }));
    },
    navigate: value => views[value.id].webContents.loadURL(allowedURL(value.url)),
    focus: id => { views[id].webContents.focus(); return views[id].webContents.isFocused(); },
  })) ipcMain.handle('browser-spike:' + method, (event, value) => { trusted(event); return action(value); });
  await window.loadURL(origin + '/shell');
  assert.equal(await window.webContents.executeJavaScript('browserSpike.ping()'), 'pong');
  check('real sandboxed preload -> ipcMain registration');
  // Test actual subframe IPC with the same preload loaded intentionally in the fixture.
  const frame = window.webContents.mainFrame.frames[0];
  const frameRejection = await frame.executeJavaScript('browserSpike.ping().then(()=>false,e=>String(e))');
  assert.match(frameRejection, /Untrusted browser request/);
  check('real renderer subframe IPC rejected', frameRejection);
  const attacker = new BrowserWindow({ show: false, webPreferences: { ...prefs, preload: path.join(__dirname, 'browser-spike-preload.cjs') } });
  await attacker.loadURL(origin + '/shell');
  assert.match(await attacker.webContents.executeJavaScript('browserSpike.ping().then(()=>false,e=>String(e))'), /Untrusted browser request/);
  attacker.destroy(); check('different renderer sender IPC rejected');

  const denied = { permission: [], popup: 0, download: 0, scheme: 0 };
  for (let id = 0; id < 4; id++) {
    const view = new WebContentsView({ webPreferences: { ...prefs, partition: 'native-spike-' + crypto.randomUUID() } });
    views.push(view); window.contentView.addChildView(view);
    const wc = view.webContents;
    wc.session.setPermissionCheckHandler(() => false);
    wc.session.setPermissionRequestHandler((_wc, permission, callback) => { denied.permission.push(permission); callback(false); });
    wc.session.on('will-download', event => { denied.download++; event.preventDefault(); });
    wc.session.webRequest.onBeforeRequest((details, callback) => {
      const protocol = new URL(details.url).protocol;
      const safe = ['http:', 'https:', 'about:', 'data:', 'blob:'].includes(protocol) || (protocol === 'devtools:' && details.webContentsId !== wc.id);
      if (!safe) denied.scheme++;
      callback({ cancel: !safe });
    });
    wc.setWindowOpenHandler(() => { denied.popup++; return { action: 'deny' }; });
    wc.on('will-navigate', (event, url) => { try { allowedURL(url); } catch { denied.scheme++; event.preventDefault(); } });
    wc.on('will-redirect', (event, url) => { try { allowedURL(url); } catch { denied.scheme++; event.preventDefault(); } });
    await wc.loadURL(origin + '/guest-' + id);
  }
  const call = (method, value) => window.webContents.executeJavaScript('browserSpike.' + method + '(' + JSON.stringify(value) + ')');
  const slots = views.map((_, id) => ({ id, rect: { x: (id % 2) * 400 + 20, y: Math.floor(id / 2) * 280 + 90, width: 390, height: 260 } }));
  let presented = await call('present', { revision: 1, slots, overlayBlocked: false });
  assert.ok(presented.every(item => item.visible));
  assert.equal(views.length, 4); check('four native WebContentsViews via real bridge', presented);
  window.webContents.setZoomFactor(1.25);
  presented = await call('present', { revision: 2, slots: [{ id: 0, rect: { x: 10.25, y: 20.25, width: 301.5, height: 220.5 } }], overlayBlocked: false });
  assert.deepEqual(presented[0].bounds, { x: 13, y: 25, width: 377, height: 276 });
  assert.ok(presented.slice(1).every(item => !item.visible));
  check('CSS edges converted using app zoom, no DPR', presented[0]);
  await assert.rejects(call('present', { revision: 1, slots, overlayBlocked: false }), /stale layout/);
  check('stale geometry snapshot rejected');
  window.webContents.setZoomFactor(1);
  await call('present', { revision: 3, slots, overlayBlocked: false });
  window.show(); window.focus(); app.focus({ steal: true });
  await sleep(150);
  assert.equal(await call('focus', 0), true);
  await views[0].webContents.executeJavaScript('document.querySelector("#name").focus()');
  views[0].webContents.sendInputEvent({ type: 'char', keyCode: 'x' });
  await sleep(50);
  assert.equal(await views[0].webContents.executeJavaScript('document.querySelector("#name").value'), 'x');
  check('native focus and keyboard input reach visible guest');
  const guest = views[0].webContents;
  await guest.executeJavaScript('localStorage.setItem("isolation","selected")');
  assert.equal(await views[1].webContents.executeJavaScript('localStorage.getItem("isolation")'), null);
  check('identical-origin native session partitions do not share localStorage');
  assert.deepEqual(await guest.executeJavaScript('({node:typeof require,process:typeof process,bridge:typeof browserSpike})'), { node: 'undefined', process: 'undefined', bridge: 'undefined' });
  check('guest has no Node or application preload bridge');
  const screenshot = await guest.capturePage();
  assert.ok(screenshot.toPNG().length > 1000); assert.ok(screenshot.getSize().width > 0);
  await fs.writeFile(path.join(root, 'guest.png'), screenshot.toPNG());
  const visibleCapture = await window.capturePage();
  await fs.writeFile(path.join(root, 'window-visible.png'), visibleCapture.toPNG());
  const pixel = (image, x, y) => { const size = image.getSize(), scale = size.width / window.getContentBounds().width; const bitmap = image.toBitmap(); const offset = (Math.round(y * scale) * size.width + Math.round(x * scale)) * 4; return [...bitmap.subarray(offset, offset + 4)]; };
  evidence.observations.compositorVisiblePixel = pixel(visibleCapture, 30, 340);
  evidence.observations.screenPermission = systemPreferences.getMediaAccessStatus('screen');
  async function captureCompositor(name) {
    if (evidence.observations.screenPermission !== 'granted') return null;
    const sources = await desktopCapturer.getSources({ types: ['window'], thumbnailSize: { width: 1000, height: 760 } });
    const source = sources.find(source => source.id === window.getMediaSourceId());
    assert.ok(source && !source.thumbnail.isEmpty());
    await fs.writeFile(path.join(root, name + '.png'), source.thumbnail.toPNG());
    const bitmap = source.thumbnail.toBitmap(); let greenPixels = 0, purplePixels = 0;
    for (let offset = 0; offset < bitmap.length; offset += 4) {
      const [b, g, r] = bitmap.subarray(offset, offset + 3);
      // ScreenCaptureKit applies display color management; classify the unique green field, not exact RGB bytes.
      if (g > r + 25 && g > b + 20 && g >= 80 && g <= 130) greenPixels++;
      if (Math.abs(b - 128) <= 3 && Math.abs(g - 64) <= 3 && Math.abs(r - 96) <= 3) purplePixels++;
    }
    return { greenPixels, purplePixels, size: source.thumbnail.getSize() };
  }
  evidence.observations.compositorVisible = await captureCompositor('compositor-visible');
  await window.webContents.executeJavaScript('document.querySelector("#overlay").style.display="block"');
  await call('present', { revision: 4, slots, overlayBlocked: true });
  assert.ok(views.every(view => !view.getVisible()));
  window.webContents.focus(); assert.ok(window.webContents.isFocused());
  await sleep(80);
  const overlayCapture = await window.capturePage();
  await fs.writeFile(path.join(root, 'window-overlay.png'), overlayCapture.toPNG());
  evidence.observations.compositorOverlayPixel = pixel(overlayCapture, 30, 340);
  // BrowserWindow.capturePage captures its renderer, NOT WebContentsView composition.
  evidence.observations.windowCaptureIncludesGuest = evidence.observations.compositorVisiblePixel.join() === '65,101,23,255';
  evidence.observations.compositorOverlay = await captureCompositor('compositor-overlay');
  if (evidence.observations.compositorVisible && evidence.observations.compositorOverlay) {
    assert.ok(evidence.observations.compositorVisible.greenPixels > 20000);
    assert.ok(evidence.observations.compositorOverlay.greenPixels < 100);
    assert.ok(evidence.observations.compositorOverlay.purplePixels > 100000);
    check('real window compositor capture proves native guests disappear beneath overlay', { visible: evidence.observations.compositorVisible, blocked: evidence.observations.compositorOverlay });
  }
  check('capture API boundary measured (window.capturePage is shell only)', evidence.observations);
  await window.webContents.executeJavaScript('document.querySelector("#overlay").style.display="none"');
  await call('present', { revision: 5, slots, overlayBlocked: false });
  check('overlay suppresses all native guests and restores them; shell regains focus');
  check('native screenshot captured', { bytes: screenshot.toPNG().length, size: screenshot.getSize() });
  assert.equal(await guest.executeJavaScript('Notification.requestPermission()'), 'denied');
  await guest.executeJavaScript('window.open("' + origin + '/popup")');
  guest.downloadURL(origin + '/download');
  await sleep(100);
  assert.ok(denied.popup > 0 && denied.download > 0);
  await assert.rejects(call('navigate', { id: 0, url: 'file:///etc/passwd' }), /unsafe URL/);
  await assert.rejects(call('navigate', { id: 0, url: 'javascript:alert(1)' }), /unsafe URL/);
  check('permission, popup, download, privileged navigation denial', denied);
  await call('navigate', { id: 0, url: origin + '/next' });
  assert.ok(guest.navigationHistory.canGoBack());
  guest.navigationHistory.goBack(); await deadline(once(guest, 'did-finish-load'), 'back');
  assert.equal(guest.getURL(), origin + '/guest-0');
  guest.navigationHistory.goForward(); await deadline(once(guest, 'did-finish-load'), 'forward');
  assert.equal(guest.getURL(), origin + '/next');
  check('navigation history preserves guest identity');

  guest.debugger.attach('1.3');
  const targetInfo = (await guest.debugger.sendCommand('Target.getTargetInfo')).targetInfo;
  const sessionID = 'selected-session', targetID = 'selected-tab';
  const allowed = new Set([
    'Page.enable', 'Page.disable', 'Page.navigate', 'Page.reload', 'Page.stopLoading', 'Page.getFrameTree', 'Page.getNavigationHistory', 'Page.navigateToHistoryEntry', 'Page.getLayoutMetrics', 'Page.captureScreenshot', 'Page.addScriptToEvaluateOnNewDocument', 'Page.removeScriptToEvaluateOnNewDocument', 'Page.createIsolatedWorld', 'Page.handleJavaScriptDialog',
    'Runtime.enable', 'Runtime.disable', 'Runtime.evaluate', 'Runtime.callFunctionOn', 'Runtime.getProperties', 'Runtime.releaseObject', 'Runtime.releaseObjectGroup',
    'DOM.enable', 'DOM.disable', 'DOM.getDocument', 'DOM.describeNode', 'DOM.resolveNode', 'DOM.getBoxModel', 'DOM.getContentQuads', 'DOM.scrollIntoViewIfNeeded', 'DOM.querySelector', 'DOM.querySelectorAll', 'DOM.getAttributes',
    'Accessibility.enable', 'Accessibility.getFullAXTree', 'Accessibility.getPartialAXTree',
    'Input.dispatchMouseEvent', 'Input.dispatchKeyEvent', 'Input.insertText', 'Input.dispatchTouchEvent',
  ]);
  const info = () => ({ targetId: targetID, type: 'page', url: guest.getURL(), title: guest.getTitle(), attached: true, canAccessOpener: false });
  async function dispatch(command) {
    assert.ok(command && typeof command.method === 'string');
    if (command.sessionId !== undefined && command.sessionId !== sessionID) throw new Error('Foreign session forbidden');
    const params = command.params || {};
    switch (command.method) {
      case 'Browser.getVersion': return { protocolVersion: '1.3', product: 'Chrome/' + process.versions.chrome, revision: '', userAgent: guest.getUserAgent(), jsVersion: process.versions.v8 };
      case 'Target.getTargets': return { targetInfos: [info()] };
      case 'Target.getTargetInfo': if (params.targetId !== undefined && params.targetId !== targetID) throw new Error('Foreign target forbidden'); return { targetInfo: info() };
      case 'Target.attachToTarget': if (params.targetId !== targetID || params.flatten !== true) throw new Error('Foreign target forbidden'); return { sessionId: sessionID };
      case 'Target.setDiscoverTargets': return {};
      case 'Target.detachFromTarget': if (params.sessionId !== sessionID) throw new Error('Foreign session forbidden'); return {};
    }
    if (!allowed.has(command.method)) throw new Error('CDP method forbidden: ' + command.method);
    if (command.sessionId !== sessionID) throw new Error('Selected session required');
    if (command.method === 'Page.navigate') allowedURL(params.url);
    return guest.debugger.sendCommand(command.method, params);
  }
  const cdp = (method, params = {}, sessionId = sessionID) => dispatch({ method, params, sessionId });
  assert.equal((await cdp('Target.getTargets')).targetInfos.length, 1);
  for (const [method, params, sessionId] of [
    ['Browser.close', {}], ['Target.createTarget', { url: origin }], ['Target.attachToTarget', { targetId: String(window.webContents.id), flatten: true }],
    ['Target.getTargetInfo', { targetId: targetInfo.targetId }], ['Target.sendMessageToTarget', {}],
    ['DOM.setFileInputFiles', { files: ['/etc/passwd'] }], ['Browser.setDownloadBehavior', { behavior: 'allow', downloadPath: root }],
    ['Page.navigate', { url: 'file:///etc/passwd' }], ['Runtime.evaluate', { expression: '1' }, 'foreign-session'],
  ]) await assert.rejects(cdp(method, params, sessionId), /forbidden|unsafe URL/);
  check('scoped CDP rejects browser/target/session/file/download escapes');
  assert.ok((await cdp('Accessibility.getFullAXTree')).nodes.length > 0);
  assert.ok((await cdp('DOM.getDocument')).root.nodeId);
  assert.ok((await cdp('Page.captureScreenshot', { format: 'png' })).data.length > 1000);
  const contexts = [];
  const contextListener = (_event, method, params) => { if (method === 'Runtime.executionContextCreated') contexts.push(params.context); };
  guest.debugger.on('message', contextListener);
  await cdp('Runtime.enable');
  assert.ok(contexts.some(context => context.auxData?.frameId !== targetInfo.targetId));
  guest.debugger.removeListener('message', contextListener);
  check('actual debugger AX/DOM/screenshot and frame execution contexts', { contexts: contexts.length });
  await cdp('Page.enable');
  const dialogOpening = new Promise(resolve => {
    const listener = (_event, method, params) => { if (method === 'Page.javascriptDialogOpening') { guest.debugger.removeListener('message', listener); resolve(params); } };
    guest.debugger.on('message', listener);
  });
  const dialogResult = cdp('Runtime.evaluate', { expression: 'confirm("Native fixture dialog")', returnByValue: true });
  const openedDialog = await deadline(dialogOpening, 'JavaScript dialog event');
  await cdp('Page.handleJavaScriptDialog', { accept: false });
  assert.equal((await deadline(dialogResult, 'JavaScript dialog result')).result.value, false);
  check('native JavaScript confirm dialog surfaced and dismissed via scoped CDP', { type: openedDialog.type });
  // Electron DevTools contention is observed, never assumed from type definitions.
  let detachReason;
  guest.debugger.once('detach', (_event, reason) => { detachReason = reason; });
  guest.openDevTools({ mode: 'detach', activate: false });
  await deadline(once(guest, 'devtools-opened'), 'DevTools open'); await sleep(100);
  evidence.observations.devtools = { debuggerAttached: guest.debugger.isAttached(), detachReason: detachReason || null };
  guest.closeDevTools(); await sleep(100);
  if (!guest.debugger.isAttached()) guest.debugger.attach('1.3');
  assert.equal((await cdp('Runtime.evaluate', { expression: '2+3', returnByValue: true })).result.value, 5);
  check('DevTools/debugger contention measured and controlled recovery works', evidence.observations.devtools);
  // Present the granted page before the independent Rod proof. Background-input
  // focus policy remains a production acceptance item, not hidden emulation.
  window.focus(); guest.focus();

  const token = crypto.randomBytes(24).toString('hex');
  sockets = new WebSocketServer({ noServer: true, maxPayload: 65536 });
  server.on('upgrade', (request, socket, head) => {
    if (request.url !== '/cdp/' + token) { socket.destroy(); return; }
    sockets.handleUpgrade(request, socket, head, ws => sockets.emit('connection', ws));
  });
  sockets.on('connection', ws => {
    const listener = (_event, method, params, nativeSession) => { if (!nativeSession && ws.readyState === 1) ws.send(JSON.stringify({ method, params, sessionId: sessionID })); };
    guest.debugger.on('message', listener);
    ws.on('close', () => guest.debugger.removeListener('message', listener));
    ws.on('message', async bytes => {
      let message;
      try {
        message = JSON.parse(bytes.toString());
        evidence.observations.rodMethods ||= {};
        evidence.observations.rodMethods[message.method] = (evidence.observations.rodMethods[message.method] || 0) + 1;
        const result = await dispatch(message);
        ws.send(JSON.stringify({ id: message.id, result }));
        if (message.method === 'Target.setDiscoverTargets' && message.params?.discover) ws.send(JSON.stringify({ method: 'Target.targetCreated', params: { targetInfo: info() } }));
      } catch (error) { ws.send(JSON.stringify({ id: message?.id, error: { code: -32000, message: String(error.message) } })); }
    });
  });
  control = { wsURL: origin.replace('http:', 'ws:') + '/cdp/' + token, targetId: targetID, sessionId: sessionID, origin, root, pid: process.pid };
  if (process.env.BROWSER_SPIKE_CONTROL) await fs.writeFile(process.env.BROWSER_SPIKE_CONTROL, JSON.stringify(control), { mode: 0o600 });
  console.log('READY', JSON.stringify({ ...control, wsURL: '[private control file]' }));
  await sleep(Number(process.env.BROWSER_SPIKE_HOLD_MS || 0));

  // Close/beforeunload and crash affect only fixture-owned guests.
  const closeGuest = views[3].webContents;
  await closeGuest.executeJavaScript('window.onbeforeunload=()=>"unsaved"; document.querySelector("#name").focus()', true);
  closeGuest.sendInputEvent({ type: 'char', keyCode: 'z' });
  let prevented = false;
  closeGuest.on('will-prevent-unload', event => { prevented = true; event.preventDefault(); });
  const destroyed = once(closeGuest, 'destroyed');
  closeGuest.close({ waitForBeforeUnload: true });
  await deadline(destroyed, 'beforeunload close');
  check('beforeunload close lifecycle', { willPreventUnload: prevented, destroyed: closeGuest.isDestroyed() });
  const crashGuest = views[2].webContents;
  const crash = once(crashGuest, 'render-process-gone'); crashGuest.forcefullyCrashRenderer();
  const [, details] = await deadline(crash, 'guest crash');
  assert.ok(!window.webContents.isDestroyed() && !guest.isDestroyed());
  check('isolated guest renderer crash leaves application/selected guest alive', details.reason);
}

async function cleanup() {
  for (const view of views) { const wc = view.webContents; if (wc && !wc.isDestroyed()) { if (wc.debugger.isAttached()) wc.debugger.detach(); wc.close(); } }
  for (const client of sockets?.clients || []) client.terminate();
  sockets?.close(); server?.closeAllConnections(); server?.close();
  if (window && !window.isDestroyed()) window.destroy();
  for (const name of ['ping', 'present', 'navigate', 'focus']) ipcMain.removeHandler('browser-spike:' + name);
  if (process.env.BROWSER_SPIKE_CONTROL) await fs.rm(process.env.BROWSER_SPIKE_CONTROL, { force: true });
  evidence.finishedAt = new Date().toISOString();
  if (process.env.BROWSER_SPIKE_EVIDENCE) await fs.writeFile(process.env.BROWSER_SPIKE_EVIDENCE, JSON.stringify(evidence, null, 2));
  await fs.rm(path.join(root, 'profile'), { recursive: true, force: true });
  if (process.env.BROWSER_SPIKE_KEEP !== '1') await fs.rm(root, { recursive: true, force: true });
}
app.on('window-all-closed', () => {});
run().then(async () => { evidence.ok = true; await cleanup(); console.log('NATIVE_SPIKE_OK', JSON.stringify(evidence)); app.exit(0); }).catch(async error => { evidence.ok = false; evidence.error = error.stack; console.error(error); await cleanup(); app.exit(1); });
