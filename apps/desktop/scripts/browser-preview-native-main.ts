import assert from 'node:assert/strict';
import { createServer, type Server } from 'node:http';
import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { randomUUID } from 'node:crypto';
import { app, BrowserWindow, ipcMain, type WebContents, type Session } from 'electron';
import { DesktopTransports } from '../src/transport';
import type { ConnectionProfile, ConnectionTarget } from '@whip/app/platform';
import type { BrowserCommand } from '@whip/protocol';
import type { BrowserAgentIdentity, BrowserAgentPreview, BrowserAgentResult, BrowserAgentScope } from '@whip/app/desktop-bridge';
import { SSHConnection } from '../src/ssh';
import { BrowserManager } from '../src/browser-manager';
import { BrowserControl } from '../src/browser-control';
import { BrowserPreviewAuthority } from '../src/browser-preview-authority';
import { browserPreviewLease } from '../src/browser-preview';
import { installBrowserIPC } from '../src/browser-ipc';
import { BrowserHumanPreview } from '../src/browser-human-preview';
import type { BrowserTabState } from '@whip/app/desktop-bridge';

type Manifest = { localHelperPath: string; connectionProfile: ConnectionProfile & { target: Extract<ConnectionTarget, { kind: 'ssh' }> }; runtimeId: string; projectId: string; remoteHTTPPort: number; remoteHTTPMarker: string; remoteUnapprovedPort: number; remoteIPv6Port: number };
const directory = process.env.BROWSER_PREVIEW_NATIVE_DIRECTORY!;
app.setPath('userData', path.join(directory, 'profile'));
app.commandLine.appendSwitch('disable-quic');
app.on('window-all-closed', () => {});
const steps: string[] = [];
const pass = (step: string) => { steps.push(step); console.log('PASS ' + step); };
const delay = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));
async function until(test: () => Promise<boolean> | boolean, label: string, timeout = 15000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) { if (await test()) return; await delay(50); }
  throw new Error('Timed out: ' + label);
}
async function listen(server: Server, port: number, host = '127.0.0.1'): Promise<number> {
  await new Promise<void>((resolve, reject) => { server.once('error', reject); server.listen(port, host, () => { server.removeListener('error', reject); resolve(); }); });
  return (server.address() as { port: number }).port;
}
async function close(server?: Server) { if (!server) return; server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); }
async function marker(contents: WebContents, text: string) {
  await until(async () => {
    if (contents.isDestroyed()) return false;
    try { return await contents.executeJavaScript(`document.body?.textContent.includes(${JSON.stringify(text)}) === true`); } catch { return false; }
  }, 'remote fixture marker');
}
let window: BrowserWindow | undefined, manager: BrowserManager | undefined, control: BrowserControl | undefined, previews: BrowserPreviewAuthority | undefined;
let cleanupIPC: (() => void) | undefined;
let humanPreview: BrowserHumanPreview | undefined;
let humanAnswer: string[] | null = [], failHumanTabId: string | undefined;
const humanPrompts: string[] = [];
let shell: Server | undefined, decoy: Server | undefined, deniedDecoy: Server | undefined, ipv6Decoy: Server | undefined;
let connection: { profile: ConnectionProfile; ssh: SSHConnection; socket: string; controller: AbortController } | undefined;
let transports: DesktopTransports | undefined;
let stopping = false;
async function cleanup() {
  if (stopping) return; stopping = true;
  transports?.dispose(); ipcMain.removeHandler('whip:openTransport');
  for (const method of ['sendTransport', 'acknowledgeTransport', 'closeTransport']) ipcMain.removeAllListeners('whip:' + method);
  humanPreview?.dispose(); control?.dispose(); manager?.dispose(); cleanupIPC?.();
  await previews?.dispose(); connection?.controller.abort(); await connection?.ssh.dispose();
  if (window && !window.isDestroyed()) window.destroy();
  await Promise.all([close(shell), close(decoy), close(deniedDecoy), close(ipv6Decoy)]);
}
process.once('SIGTERM', () => { void cleanup().finally(() => app.exit(143)); });
async function run() {
  const manifest = JSON.parse(await readFile(process.env.BROWSER_PREVIEW_NATIVE_MANIFEST!, 'utf8')) as Manifest;
  assert.equal(manifest.connectionProfile.target.kind, 'ssh'); assert.ok(Number.isInteger(manifest.remoteHTTPPort) && manifest.remoteHTTPPort > 0 && manifest.remoteHTTPPort <= 65535);
  await app.whenReady();
  let localConnections = 0, deniedConnections = 0, ipv6Connections = 0;
  decoy = createServer((_request, response) => response.end('LOCAL-DECOY-MUST-NOT-BE-REACHED'));
  decoy.on('connection', () => localConnections++); await listen(decoy, manifest.remoteHTTPPort);
  deniedDecoy = createServer((_request, response) => response.end('UNAPPROVED-LOCAL-DECOY'));
  deniedDecoy.on('connection', () => deniedConnections++); const deniedPort = await listen(deniedDecoy, manifest.remoteUnapprovedPort);
  const sdkScript = process.env.BROWSER_PREVIEW_NATIVE_SDK === '1' ? await readFile(path.join(directory, 'sdk.js')) : undefined;
  shell = createServer((request, response) => {
    if (request.url === '/sdk.js' && sdkScript) { response.setHeader('content-type', 'application/javascript'); response.end(sdkScript); return; }
    response.setHeader('content-type', 'text/html'); response.end('<!doctype html><title>Owned native SSH preview fixture</title><main>Trusted fixture shell</main>' + (sdkScript ? '<script src="/sdk.js"></script>' : ''));
  });
  const shellPort = await listen(shell, 0), shellURL = `http://127.0.0.1:${shellPort}/shell`;
  const connectionId = randomUUID(), profile = manifest.connectionProfile;
  async function connect() {
    const controller = new AbortController();
    const ssh = new SSHConnection({ target: manifest.connectionProfile.target, executable: manifest.localHelperPath, env: process.env, signal: controller.signal,
      progress: message => console.log('SSH fixture: ' + message), prompt: async () => { throw new Error('Unexpected SSH credential/host prompt; fixture may only use existing trust and authentication'); } });
    try { const socket = await ssh.getSocket(); connection = { profile, ssh, socket, controller }; }
    catch (error) { controller.abort(); await ssh.dispose(); throw error; }
  }
  await connect();
  window = new BrowserWindow({ width: 1000, height: 760, show: process.env.BROWSER_PREVIEW_NATIVE_SDK_ONLY !== '1', webPreferences: { preload: path.join(directory, 'preload.cjs'), additionalArguments: ['--whip-browser-tabs'], sandbox: true, contextIsolation: true, nodeIntegration: false, webviewTag: false } });
  previews = new BrowserPreviewAuthority(app.getPath('userData'), id => id === connectionId ? connection : undefined,
    (id, reason) => control?.invalidateEnvironment(id, reason), id => manager?.invalidateEnvironment(id, 'Preview connection ended'));
  manager = new BrowserManager(window, event => { control?.observe(event); window!.webContents.send('whip:browser:event', event); }, {
    environment: async (id, tabId) => { if (tabId === failHumanTabId) throw new Error('Owned admission failure'); return browserPreviewLease(previews!.environments, id, tabId); }, invalidateControl: (id, reason) => control?.invalidate(id, reason),
  });
  control = new BrowserControl(manager, event => window!.webContents.send('whip:browser-agent:event', event), {
    prepare: selection => previews!.prepare(selection), preview: (selection, scope) => previews!.ensure(selection, scope),
    expand: (selection, scope, port) => previews!.expand(selection, scope, port), previewState: id => previews!.offered(id),
  });
  humanPreview = new BrowserHumanPreview(manager, previews, control, async (_id, prompt) => { humanPrompts.push(prompt.message); return humanAnswer; });
  cleanupIPC = installBrowserIPC(window, manager, event => {
    if (event.sender !== window!.webContents || event.senderFrame !== window!.webContents.mainFrame || event.senderFrame.url !== shellURL) throw new Error('Untrusted fixture renderer');
  }, control, previews, humanPreview);
  if (sdkScript) {
    const trusted = (event: Electron.IpcMainEvent | Electron.IpcMainInvokeEvent) => {
      if (event.sender !== window!.webContents || event.senderFrame !== window!.webContents.mainFrame || event.senderFrame.url !== shellURL) throw new Error('Untrusted SDK fixture renderer');
    };
    transports = new DesktopTransports(event => window!.webContents.send('whip:event', event));
    ipcMain.handle('whip:openTransport', async (event, id, selected) => { trusted(event); assert.equal(selected, connectionId); await transports!.open(id, selected, await connection!.ssh.getSocket(), connection!.controller.signal); });
    ipcMain.on('whip:sendTransport', (event, id, sequence, frame) => { trusted(event); transports!.send(id, sequence, frame); });
    ipcMain.on('whip:acknowledgeTransport', (event, id, sequence) => { trusted(event); transports!.acknowledge(id, sequence); });
    ipcMain.on('whip:closeTransport', (event, id) => { trusted(event); transports!.close(id); });
  }
  await window.loadURL(shellURL);
  const call = <T>(method: string, value?: unknown): Promise<T> => window!.webContents.executeJavaScript(`whipDesktop.browserAgent.${method}(${value === undefined ? '' : JSON.stringify(value)})`);
  await window.webContents.executeJavaScript(`window.stopAdmissions = whipDesktop.browserAgent.onEvent(event => { if (event.kind === 'admission') void whipDesktop.browser.admitted({epoch:event.epoch,tabId:event.tab.id,generation:event.tab.generation}); }); void 0;`);
  async function runSDK() {
    const sdk = await window!.webContents.executeJavaScript(`runNativeSDK(${JSON.stringify({ connectionId, runtimeId: manifest.runtimeId, cwd: manifest.projectId, url: `http://127.0.0.1:${manifest.remoteHTTPPort}/`, marker: manifest.remoteHTTPMarker })}).catch(error => ({ok:false,error:error.message,stack:error.stack}))`);
    if (!sdk.ok && sdk.tabId) {
      const page = manager!.snapshot().tabs.find(tab => tab.id === sdk.tabId);
      sdk.humanTabPreserved = !!page;
      if (page) sdk.delayedEffectCount = await (await manager!.controlledContents({ epoch: manager!.snapshot().epoch, tabId: page.id, generation: page.generation })).executeJavaScript('window.ownedSdkCancelledCount');
    }
    assert.equal(sdk.ok, true, JSON.stringify(sdk)); assert.deepEqual(sdk.errors, []);
    for (const report of sdk.reports) pass(report);
    const sdkPage = manager!.snapshot().tabs.find(tab => tab.id === sdk.tabId)!;
    const sdkTarget = { epoch: manager!.snapshot().epoch, tabId: sdkPage.id, generation: sdkPage.generation };
    const sdkGuest = await manager!.controlledContents(sdkTarget);
    await marker(sdkGuest, manifest.remoteHTTPMarker);
    assert.equal(await sdkGuest.executeJavaScript('window.ownedSdkCancelledCount'), 1, 'Timed-out effect must occur exactly once, never replay');
    await manager!.close(sdkTarget); assert.equal(localConnections, 0);
    pass('SDK root/provider deletion preserves actual remote human guest until explicit native close; Mac decoys untouched');
    return sdk;
  }
  if (process.env.BROWSER_PREVIEW_NATIVE_SDK_ONLY === '1') {
    assert.ok(sdkScript); const sdk = await runSDK();
    if (process.env.BROWSER_PREVIEW_NATIVE_EVIDENCE) await writeFile(process.env.BROWSER_PREVIEW_NATIVE_EVIDENCE, JSON.stringify({ ok: true, electron: process.versions.electron, platform: process.platform, arch: process.arch, host: manifest.connectionProfile.target.host, runtimeId: manifest.runtimeId, sdk, steps, localConnections, deniedConnections }, null, 2));
    return;
  }
  const identity = await call<BrowserAgentIdentity>('identity');
  const request = { connectionId, runtimeId: manifest.runtimeId, projectId: manifest.projectId, loopback: '127.0.0.1' as const };
  const preview = await call<BrowserAgentPreview>('preview', request); assert.deepEqual(preview.ports, []);
  assert.equal(preview.connection_generation, connection!.ssh.previewConnection!.generation);
  await assert.rejects(previews.environments.acquire(preview.environment_id, 'not-approved'), /unavailable|not active|not available/i);
  assert.equal(localConnections, 0); pass('real SSH runtime identity verified; metadata-only preview offer starts no usable environment');
  const rootId = randomUUID(), agentId = randomUUID(), provider = { version: 1, provider_id: randomUUID(), provider_epoch: randomUUID() };
  let scope: BrowserAgentScope;
  const select = async (preview: BrowserAgentPreview, port = manifest.remoteHTTPPort) => {
    await call('select', { connectionId, projectId: manifest.projectId, provider, offer: { version: 1, root_id: rootId,
      desktop_id: identity.desktopId, window_id: identity.windowId, create_profile_id: identity.createProfileId, offer_revision: randomUUID(), offered_tabs: [], offered_preview_hosts: [preview] } });
    scope = { provider_id: provider.provider_id, provider_epoch: provider.provider_epoch, tab_id: randomUUID(), tab_generation: randomUUID(), profile_id: identity.createProfileId,
      attachment_id: randomUUID(), attachment_generation: randomUUID(), rights: ['create', 'control', 'route'], preview: { ...preview, ports: [port] } };
  };
  let operationId = randomUUID();
  const command = (kind: string, args: unknown = {}): BrowserCommand => ({ command_id: randomUUID(), operation_id: operationId, root_id: rootId, agent_id: agentId,
    provider_epoch: provider.provider_epoch, deadline_millis: String(Date.now() + 20000), kind, scope: structuredClone(scope), arguments: args });
  const dispatch = (kind: string, args: unknown = {}) => call<BrowserAgentResult>('dispatch', command(kind, args));
  const remoteURL = `http://127.0.0.1:${manifest.remoteHTTPPort}/`;
  await select(preview);
  assert.equal((await dispatch('open', { url: remoteURL })).error, undefined);
  let target = { epoch: manager.snapshot().epoch, tabId: scope!.tab_id, generation: scope!.tab_generation };
  let guest = await manager.controlledContents(target); await marker(guest, manifest.remoteHTTPMarker);
  const actualProfile = guest.session;
  assert.throws(() => previews!.environments.assertURL(preview.environment_id, `http://[::1]:${manifest.remoteHTTPPort}/`));
  assert.equal(await guest.executeJavaScript(`fetch('http://[::1]:${manifest.remoteHTTPPort}/',{cache:'no-store'}).then(()=>false,()=>true)`), true);
  assert.equal(localConnections, 0); pass('production BrowserControl admission + BrowserManager guest loads real remote HTTP through SSH with zero same-port Mac decoy connections');
  assert.equal(await guest.executeJavaScript('typeof window.whipDesktop'), 'undefined');
  const storage = randomUUID(); await guest.executeJavaScript(`localStorage.setItem('owned-preview-proof',${JSON.stringify(storage)})`);
  const info = await guest.executeJavaScript("fetch('/inspect',{cache:'no-store'}).then(r=>r.json())");
  assert.equal(info.marker, manifest.remoteHTTPMarker); assert.equal(info.host, `127.0.0.1:${manifest.remoteHTTPPort}`);
  assert.equal(info.proxyAuthorization, null); assert.equal(info.authorization, null);
  await guest.executeJavaScript("fetch('/cookie',{cache:'no-store'}).then(r=>r.text())");
  const cookies = await guest.executeJavaScript("fetch('/inspect',{cache:'no-store'}).then(r=>r.json())");
  assert.ok(cookies.cookie.includes('remote_preview=' + manifest.remoteHTTPMarker));
  assert.equal(await guest.executeJavaScript("document.cookie.includes('remote_preview')"), false);
  const siteStatus = await guest.executeJavaScript("fetch('/website-auth',{cache:'no-store'}).then(r=>r.status)");
  assert.equal(siteStatus, 401);
  const afterSiteAuth = await guest.executeJavaScript("fetch('/inspect',{cache:'no-store'}).then(r=>r.json())");
  assert.equal(afterSiteAuth.authorization, null); assert.equal(afterSiteAuth.proxyAuthorization, null);
  assert.equal(localConnections, 0); pass('logical remote Host and HttpOnly cookies preserved; proxy credentials never reach site headers or website-auth challenges');
  const ws = await guest.executeJavaScript(`new Promise((resolve,reject)=>{const socket=new WebSocket(${JSON.stringify(`ws://127.0.0.1:${manifest.remoteHTTPPort}/ws`)});const timer=setTimeout(()=>{socket.close();reject(Error('WS timeout'))},5000);socket.onmessage=e=>{clearTimeout(timer);socket.close();resolve(e.data)};socket.onerror=()=>{clearTimeout(timer);socket.close();reject(Error('WS failure'))}})`);
  assert.equal(ws, manifest.remoteHTTPMarker);
  const sse = await guest.executeJavaScript("new Promise((resolve,reject)=>{const source=new EventSource('/events');const timer=setTimeout(()=>{source.close();reject(Error('SSE timeout'))},5000);source.onmessage=e=>{clearTimeout(timer);source.close();resolve(e.data)};source.onerror=()=>{clearTimeout(timer);source.close();reject(Error('SSE failure'))}})");
  assert.equal(sse, manifest.remoteHTTPMarker); pass('actual remote WebSocket upgrade and SSE stream traverse the native preview route');
  const deniedURL = `http://127.0.0.1:${deniedPort}/unapproved`;
  assert.equal(await guest.executeJavaScript(`fetch('/redirect?url='+encodeURIComponent(${JSON.stringify(deniedURL)}),{cache:'no-store'}).then(()=>false,()=>true)`), true);
  const worker = await guest.executeJavaScript(`(async()=>{await navigator.serviceWorker.register('/worker.js');const registration=await navigator.serviceWorker.ready;return new Promise((resolve,reject)=>{const timer=setTimeout(()=>reject(Error('worker timeout')),5000);const receive=e=>{clearTimeout(timer);navigator.serviceWorker.removeEventListener('message',receive);resolve(e.data)};navigator.serviceWorker.addEventListener('message',receive);registration.active.postMessage(${JSON.stringify(deniedURL)})})})()`);
  assert.equal(worker, 'blocked');
  await guest.executeJavaScript("navigator.serviceWorker.getRegistrations().then(values=>Promise.all(values.map(value=>value.unregister())))");
  const blocked = await guest.executeJavaScript(`fetch('http://127.0.0.1:${deniedPort}/unapproved',{cache:'no-store'}).then(()=>false,()=>true)`);
  assert.equal(blocked, true); assert.equal(deniedConnections, 0); pass('unapproved loopback destination is denied without touching Mac; guest has no native preload');
  assert.equal((await dispatch('begin')).error, undefined);
  const screenshot = await dispatch('cdp', { method: 'Page.captureScreenshot', params: { format: 'jpeg', quality: 50 } });
  assert.equal(screenshot.error, undefined); assert.ok(screenshot.screenshotBytes instanceof Uint8Array && screenshot.screenshotBytes.length > 100);
  assert.equal((await dispatch('allow_preview_port', { port: deniedPort })).error?.kind, 'browser_busy');
  assert.equal((await dispatch('end')).error, undefined);
  const staleScopeCommand = command('begin');
  assert.equal((await dispatch('allow_preview_port', { port: deniedPort })).error, undefined);
  scope!.preview!.ports = [manifest.remoteHTTPPort, deniedPort].sort((a, b) => a - b);
  assert.ok((await call<BrowserAgentResult>('dispatch', staleScopeCommand)).error);
  await guest.loadURL(deniedURL); await marker(guest, manifest.remoteHTTPMarker); assert.equal(deniedConnections, 0);
  pass('explicit approved port expansion reaches formerly denied remote endpoint, rejects stale port scope, and never touches same-port Mac decoy');
  assert.equal((await dispatch('detach')).error, undefined);
  await guest.loadURL(remoteURL + '?human-after-detach=' + randomUUID()); await marker(guest, manifest.remoteHTTPMarker);
  assert.equal(localConnections, 0); pass('actual preview guest scoped CDP screenshot works; model detach preserves human tab and its SSH route lease');
  const oldCommand = command('begin'), oldGeneration = preview.connection_generation;
  connection!.controller.abort(); await connection!.ssh.dispose();
  await until(() => guest.isDestroyed() && manager!.snapshot().tabs.find(tab => tab.id === target.tabId)?.status === 'unavailable', 'disconnect invalidates native guest');
  assert.ok((await call<BrowserAgentResult>('dispatch', oldCommand)).error); assert.equal(localConnections, 0);
  async function probe(profile: Session, suffix: string) {
    const isolated = new BrowserWindow({ show: false, webPreferences: { session: profile, sandbox: true, contextIsolation: true, nodeIntegration: false, webviewTag: false } });
    try { await assert.rejects(isolated.webContents.loadURL(remoteURL + '?' + suffix + '=' + randomUUID())); }
    finally { isolated.destroy(); }
  }
  await probe(actualProfile, 'after-disconnect'); assert.equal(localConnections, 0); pass('owned SSH master loss revokes attachment/destroys guest; retained profile stays fail-closed with zero Mac fallback');
  await connect();
  const rebound = await call<BrowserAgentPreview>('preview', request);
  assert.equal(rebound.environment_id, preview.environment_id); assert.notEqual(rebound.connection_generation, oldGeneration); assert.deepEqual(rebound.ports, []);
  provider.provider_epoch = randomUUID(); operationId = randomUUID(); await select(rebound);
  assert.equal((await dispatch('open', { url: remoteURL })).error, undefined);
  target = { epoch: manager.snapshot().epoch, tabId: scope!.tab_id, generation: scope!.tab_generation };
  guest = await manager.controlledContents(target); await marker(guest, manifest.remoteHTTPMarker);
  assert.equal(guest.session, actualProfile); assert.equal(await guest.executeJavaScript("localStorage.getItem('owned-preview-proof')"), storage);
  assert.ok((await call<BrowserAgentResult>('dispatch', oldCommand)).error); assert.equal(localConnections, 0);
  pass('explicit reconnect changes connection/provider authority but preserves stable isolated site data; stale grant cannot revive');
  assert.equal((await manager.close(target)).status, 'closed');
  assert.ok(!manager.snapshot().tabs.some(tab => tab.id === target.tabId));
  await until(() => guest.isDestroyed(), 'last guest closed');
  await until(() => { try { previews!.environments.assertURL(rebound.environment_id, remoteURL); return false; } catch { return true; } }, 'last tab lease released');
  await probe(actualProfile, 'after-last-tab'); assert.equal(localConnections, 0); assert.equal(deniedConnections, 0);
  pass('last preview tab close tears down its routes/proxy; same-port Mac decoy remains untouched');
  connection!.controller.abort(); await connection!.ssh.dispose(); await connect();
  ipv6Decoy = createServer((_request, response) => response.end('IPV6-LOCAL-DECOY-MUST-NOT-BE-REACHED'));
  ipv6Decoy.on('connection', () => ipv6Connections++); await listen(ipv6Decoy, manifest.remoteIPv6Port, '::1');
  const ipv6 = await call<BrowserAgentPreview>('preview', { ...request, loopback: '::1' });
  assert.equal(ipv6.environment_id, preview.environment_id); assert.deepEqual(ipv6.ports, []);
  provider.provider_epoch = randomUUID(); operationId = randomUUID(); await select(ipv6, manifest.remoteIPv6Port);
  const ipv6URL = `http://[::1]:${manifest.remoteIPv6Port}/`;
  assert.equal((await dispatch('open', { url: ipv6URL })).error, undefined);
  target = { epoch: manager.snapshot().epoch, tabId: scope!.tab_id, generation: scope!.tab_generation };
  guest = await manager.controlledContents(target); await marker(guest, manifest.remoteHTTPMarker);
  const ipv6Info = await guest.executeJavaScript("fetch('/inspect',{cache:'no-store'}).then(r=>r.json())");
  assert.equal(ipv6Info.host, `[::1]:${manifest.remoteIPv6Port}`); assert.equal(ipv6Info.proxyAuthorization, null);
  assert.equal(await guest.executeJavaScript(`fetch(${JSON.stringify(remoteURL)},{cache:'no-store'}).then(()=>false,()=>true)`), true);
  assert.throws(() => previews!.environments.assertURL(ipv6.environment_id, `http://127.0.0.1:${manifest.remoteIPv6Port}/`));
  assert.equal(await guest.executeJavaScript(`fetch('http://127.0.0.1:${manifest.remoteIPv6Port}/',{cache:'no-store'}).then(()=>false,()=>true)`), true);
  assert.equal(ipv6Connections, 0); assert.equal(localConnections, 0);
  assert.equal((await manager.close(target)).status, 'closed');
  pass('explicit IPv6 literal-loopback preview reaches real remote endpoint with preserved Host, no IPv4 widening, and zero same-port Mac IPv6 connections');
  const humanCall = <T>(method: string, value: unknown): Promise<T> => window!.webContents.executeJavaScript(`whipDesktop.browser.${method}(${JSON.stringify(value)})`);
  const humanRequest = { epoch: manager.snapshot().epoch, connectionId, runtimeId: manifest.runtimeId, projectId: 'cwd:' + manifest.projectId, url: remoteURL };
  const humanOffer = await call<BrowserAgentPreview>('preview', { ...request, projectId: humanRequest.projectId });
  const humanTarget = (page: BrowserTabState) => ({ epoch: humanRequest.epoch, tabId: page.id, generation: page.generation });
  const countBefore = manager.snapshot().tabs.length;
  humanAnswer = null; assert.equal(await humanCall('createPreview', humanRequest), undefined);
  assert.equal(manager.snapshot().tabs.length, countBefore);
  await assert.rejects(previews.environments.acquire(humanOffer.environment_id, 'unapproved-human'));
  humanAnswer = [];
  const cancelled = await humanCall<BrowserTabState>('createPreview', humanRequest);
  await assert.rejects(previews.environments.acquire(humanOffer.environment_id, 'before-human-admission'));
  await manager.close(humanTarget(cancelled)); await assert.rejects(humanCall('admitted', humanTarget(cancelled)));
  await assert.rejects(previews.environments.acquire(humanOffer.environment_id, 'after-human-cancel'));
  pass('human native confirmation denial and closed-before-admission create no route, guest or grant; confirmed result is metadata only');
  const humanPage = await humanCall<BrowserTabState>('createPreview', humanRequest);
  await humanCall('admitted', humanTarget(humanPage));
  const humanGuest = await manager.controlledContents(humanTarget(humanPage)); await marker(humanGuest, manifest.remoteHTTPMarker);
  const failed = await humanCall<BrowserTabState>('createPreview', { ...humanRequest, url: deniedURL }); failHumanTabId = failed.id;
  await assert.rejects(humanCall('admitted', humanTarget(failed))); failHumanTabId = undefined;
  assert.ok(!manager.snapshot().tabs.some(tab => tab.id === failed.id));
  assert.deepEqual(previews.offered(humanOffer.environment_id)!.ports, [manifest.remoteHTTPPort]);
  assert.throws(() => previews!.environments.assertURL(humanOffer.environment_id, deniedURL));
  await humanGuest.loadURL(remoteURL + '?after-human-rollback=' + randomUUID()); await marker(humanGuest, manifest.remoteHTTPMarker);
  const expandedHuman = await humanCall<BrowserTabState>('createPreview', { ...humanRequest, url: deniedURL });
  await humanCall('admitted', humanTarget(expandedHuman));
  const expandedGuest = await manager.controlledContents(humanTarget(expandedHuman)); await marker(expandedGuest, manifest.remoteHTTPMarker);
  assert.ok(humanPrompts.every(message => message.includes('All tabs in this project environment') && message.includes('does not grant any agent control')));
  assert.ok(humanPrompts.at(-1)!.includes(String(deniedPort)) && humanPrompts.at(-1)!.includes(String(manifest.remoteHTTPPort)));
  await manager.close(humanTarget(expandedHuman)); await manager.close(humanTarget(humanPage));
  await until(() => { try { previews!.environments.assertURL(humanOffer.environment_id, remoteURL); return false; } catch { return true; } }, 'human last-tab routes released');
  assert.equal(localConnections, 0); assert.equal(deniedConnections, 0); assert.equal(ipv6Connections, 0);
  pass('human transactional admission loads actual remote; failed expansion rolls back only new route/guest and preserves prior human route; approved union expansion and last-tab cleanup pass');
  let sdk: unknown;
  if (sdkScript) sdk = await runSDK();
  if (process.env.BROWSER_PREVIEW_NATIVE_EVIDENCE) await writeFile(process.env.BROWSER_PREVIEW_NATIVE_EVIDENCE, JSON.stringify({ ok: true, electron: process.versions.electron,
    platform: process.platform, arch: process.arch, host: manifest.connectionProfile.target.host, runtimeId: manifest.runtimeId, sdk, steps, localConnections, deniedConnections, ipv6Connections, screenshotBytes: screenshot.screenshotBytes!.length }, null, 2));
}
run().then(async () => { await cleanup(); console.log('NATIVE_SSH_PREVIEW_OK'); app.exit(0); }, async error => { console.error(error); await cleanup(); app.exit(1); });
