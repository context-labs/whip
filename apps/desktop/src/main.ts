import { app, BrowserWindow, dialog, ipcMain, Menu, powerMonitor, protocol, screen, session } from 'electron';
import { randomUUID } from 'node:crypto';
import { readFile, writeFile, rename } from 'node:fs/promises';
import path from 'node:path';
import { validateProfile } from '@whip/app/platform';
import type { DesktopEvent, HostPrompt } from '@whip/app/desktop-bridge';
import { createAssetHandler, desktopScheme, desktopURL, isDesktopURL, type RendererManifest } from './assets';
import { installRuntime, prepareLocal, readRuntimeManifest, runtimeEnvironment } from './runtime';
import { DesktopTransports, validHandle } from './transport';
import { NativeEffects } from './native';
import { SSHConnection } from './ssh';
import { DesktopUpdates, readDesktopConfig } from './updates';
import { sessionLinkPath } from './links';
import { attachStartupProbe } from './startup-probe';

protocol.registerSchemesAsPrivileged([{ scheme: desktopScheme,
  privileges: { standard: true, secure: true, supportFetchAPI: true, corsEnabled: true } }]);
// Fixtures must opt in explicitly; never silently attach dev scripts to ~/.whip.
if (!app.isPackaged || process.env.WHIP_DESKTOP_FIXTURE) {
  if (!process.env.WHIP_DESKTOP_FIXTURE || !process.env.WHIP_HOME || !process.env.WHIP_DESKTOP_USER_DATA)
    throw new Error('Development requires an isolated WHIP_HOME and WHIP_DESKTOP_USER_DATA');
  if (!path.isAbsolute(process.env.WHIP_HOME) || !path.isAbsolute(process.env.WHIP_DESKTOP_USER_DATA))
    throw new Error('Fixture data paths must be absolute');
  app.setPath('userData', process.env.WHIP_DESKTOP_USER_DATA);
}
app.setName(__APP_NAME__);
let pendingSessionPath: string | undefined;
let openSession: ((path: string) => void) | undefined;
app.on('open-url', (event, url) => {
  event.preventDefault();
  try {
    const path = sessionLinkPath(url, __APP_NAME__ === 'Whip Beta' ? 'whip-beta' : 'whip');
    if (openSession) openSession(path); else pendingSessionPath = path;
  } catch { /* Foreign or malformed links never gain connection authority. */ }
});
const ownsWindow = app.requestSingleInstanceLock();
if (!ownsWindow) app.quit();
else void start().catch(async error => {
  await app.whenReady();
  dialog.showErrorBox('Whip could not start', error instanceof Error ? error.message : 'Invalid desktop installation');
  app.exit(1);
});

async function start() {
  await app.whenReady();
  let developmentOrigin: string | undefined;
  if (!app.isPackaged && process.env.WHIP_DESKTOP_DEV_URL) {
    const url = new URL(process.env.WHIP_DESKTOP_DEV_URL);
    if (url.protocol !== 'http:' || url.hostname !== '127.0.0.1' || !url.port || url.username || url.password || url.pathname !== '/' || url.search || url.hash)
      throw new Error('The development renderer must use an exact loopback HTTP origin');
    developmentOrigin = url.origin;
  }
  const rendererURL = (value: string) => {
    if (isDesktopURL(value)) return true;
    try { const url = new URL(value); return !!developmentOrigin && url.origin === developmentOrigin && !url.username && !url.password; }
    catch { return false; }
  };
  const root = app.getAppPath();
  const renderer = JSON.parse(await readFile(path.join(root, 'renderer-manifest.json'), 'utf8')) as RendererManifest;
  const manifest = await readRuntimeManifest(path.join(root, 'runtime-manifest.json'));
  const config = readDesktopConfig(JSON.parse(await readFile(path.join(root, 'desktop-config.json'), 'utf8')));
  if (config.channel === 'beta' && !process.env.WHIP_HOME) process.env.WHIP_HOME = path.join(app.getPath('userData'), 'runtime-home');
  if (manifest.rendererDigest !== renderer.digest) throw new Error('The renderer and runtime belong to different builds');
  const source = app.isPackaged ? path.join(process.resourcesPath, '..', 'Helpers') : path.join(root, '..', 'native');
  const retainedRoot = path.join(app.getPath('userData'), 'runtimes');
  protocol.handle(desktopScheme, createAssetHandler(path.join(root, 'renderer'), renderer));
  session.defaultSession.setPermissionRequestHandler((_contents, _permission, respond) => respond(false));
  session.defaultSession.setPermissionCheckHandler(() => false);
  session.defaultSession.setDevicePermissionHandler(() => false);
  const boundsFile = path.join(app.getPath('userData'), 'window.json');
  let bounds: Electron.Rectangle | undefined;
  try {
    const saved = JSON.parse(await readFile(boundsFile, 'utf8')) as Electron.Rectangle;
    if (['x', 'y', 'width', 'height'].every(key => Number.isSafeInteger(saved[key as keyof Electron.Rectangle])) &&
        saved.width >= 800 && saved.width <= 7680 && saved.height >= 600 && saved.height <= 4320 &&
        screen.getAllDisplays().some(({ workArea: area }) => saved.x < area.x + area.width && saved.y < area.y + area.height &&
          saved.x + saved.width > area.x + 80 && saved.y + saved.height > area.y + 80)) bounds = saved;
  } catch { /* A missing or invalid viewing preference uses normal window placement. */ }
  const window = new BrowserWindow({ width: 1200, height: 800, minWidth: 800, minHeight: 600, ...bounds,
    show: false, backgroundColor: '#111111', title: 'Whip',
    webPreferences: { preload: path.join(root, 'preload.cjs'), sandbox: true, contextIsolation: true,
      nodeIntegration: false, webSecurity: true, webviewTag: false, spellcheck: true } });
  await attachStartupProbe(window, { userData: app.getPath('userData'), rendererDigest: renderer.digest, quit: () => app.quit() });
  const emit = (event: DesktopEvent) => { if (!window.isDestroyed() && !window.webContents.isDestroyed()) window.webContents.send('whip:event', event); };
  const transports = new DesktopTransports(emit);
  const native = new NativeEffects(window, emit);
  const connections = new Map<string, { controller: AbortController; socket?: string; ssh?: SSHConnection }>();
  const prompts = new Map<string, { fields: number; finish(value: string[] | null): void }>();
  const prompt = (attemptId: string, value: Omit<HostPrompt, 'id' | 'attemptId'>, signal: AbortSignal) =>
    new Promise<string[] | null>((resolve, reject) => {
      signal.throwIfAborted();
      if (prompts.size >= 4) { reject(new Error('Too many authentication requests')); return; }
      const id = randomUUID();
      const finish = (answer: string[] | null) => {
        if (!prompts.delete(id)) return;
        signal.removeEventListener('abort', abort); emit({ kind: 'prompt-dismissed', id }); resolve(answer);
      };
      const abort = () => finish(null);
      prompts.set(id, { fields: value.fields.length, finish });
      signal.addEventListener('abort', abort, { once: true });
      window.show(); window.focus(); emit({ kind: 'prompt', prompt: { ...value, id, attemptId } });
    });
  const release = (id: string) => {
    validHandle(id); const connection = connections.get(id); connections.delete(id);
    transports.release(id); connection?.controller.abort();
    return connection?.ssh?.dispose();
  };
  const disposeConnections = () => Promise.allSettled([...connections.keys()].map(release));
  const trusted = (event: Electron.IpcMainEvent | Electron.IpcMainInvokeEvent) => {
    if (window.isDestroyed() || event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame ||
        !rendererURL(event.senderFrame.url)) throw new Error('Untrusted desktop request');
  };
  const handle = (name: string, action: (...args: any[]) => unknown) => {
    ipcMain.handle(`whip:${name}`, (event, ...args: unknown[]) => { trusted(event); return action(...args); });
  };
  const listen = (name: string, action: (...args: any[]) => unknown) => {
    ipcMain.on(`whip:${name}`, (event, ...args: unknown[]) => {
      try { trusted(event); void Promise.resolve(action(...args)).catch(() => disposeConnections()); }
      catch { void disposeConnections(); }
    });
  };
  handle('prepareConnection', async (id: string, input: unknown) => {
    validHandle(id);
    const profile = validateProfile(input);
    if (profile.target.kind === 'url') throw new Error('URL connections belong to the shared renderer');
    if (connections.has(id) || connections.size >= 2) throw new Error('Desktop connection limit reached');
    const connection = { controller: new AbortController() } as { controller: AbortController; socket?: string; ssh?: SSHConnection };
    connections.set(id, connection);
    try {
      const progress = (message: string) => { if (connections.get(id) === connection) emit({ kind: 'progress', attemptId: id, message }); };
      const signal = connection.controller.signal;
      if (profile.target.kind === 'ssh') {
        progress('Preparing SSH…');
        const installed = await installRuntime(source, retainedRoot, manifest, signal);
        const env = await runtimeEnvironment(signal);
        connection.ssh = new SSHConnection({ target: profile.target, executable: installed.executable, env, signal, progress,
          prompt: (value, lifetime) => prompt(id, value, lifetime) });
        connection.socket = await connection.ssh.getSocket();
      } else connection.socket = await prepareLocal({ source, retainedRoot, manifest, signal, progress });
      connection.controller.signal.throwIfAborted();
    } catch (error) { if (connections.get(id) === connection) await release(id); throw error; }
  });
  listen('releaseConnection', release);
  listen('hideWindow', () => { void saveBounds().catch(() => {}); window.hide(); });
  listen('setNotificationsEnabled', (enabled: boolean) => {
    if (typeof enabled !== 'boolean') throw new Error('Invalid notification preference');
    window.webContents.setBackgroundThrottling(!enabled);
  });
  handle('openTransport', async (id: string, connectionId: string) => {
    validHandle(connectionId);
    const connection = connections.get(connectionId);
    if (!connection?.socket) throw new Error('The selected connection is not ready');
    const socket = connection.ssh ? await connection.ssh.getSocket() : connection.socket;
    await transports.open(id, connectionId, socket, connection.controller.signal);
  });
  listen('sendTransport', transports.send.bind(transports));
  listen('acknowledgeTransport', transports.acknowledge.bind(transports));
  listen('closeTransport', (id: string) => { validHandle(id); transports.close(id); });
  for (const method of ['copy', 'openExternal', 'beginSave', 'writeSave', 'finishSave', 'cancelSave', 'notify'] as const)
    handle(method, native[method].bind(native));
  handle('pickDirectory', () => {
    if (![...connections.values()].some(connection => connection.socket && !connection.ssh)) throw new Error('Select This Mac to choose a local directory');
    return native.pickDirectory();
  });
  handle('answerPrompt', (id: string, values: string[] | null) => {
    validHandle(id); const pending = prompts.get(id);
    if (!pending) return;
    if (values !== null && (!Array.isArray(values) || values.length !== pending.fields ||
        values.some(value => typeof value !== 'string' || Buffer.byteLength(value) > 4096 || /[\u0000-\u001f\u007f]/.test(value))))
      throw new Error('Invalid authentication response');
    pending.finish(values);
  });
  handle('checkForUpdates', () => updates.check());
  handle('installUpdate', () => updates.install());
  let rendererReady = false;
  openSession = path => {
    window.show(); window.focus();
    if (rendererReady) emit({ kind: 'navigate', path }); else pendingSessionPath = path;
  };
  listen('ready', () => {
    rendererReady = true; updates.ready(); if (!window.isVisible()) window.show();
    if (pendingSessionPath) { const path = pendingSessionPath; pendingSessionPath = undefined; openSession?.(path); }
  });
  let quitApproved = false;
  let closeInProgress = false;
  let pendingClose: { id: string; resolve: (result: { attachments: boolean; error?: string }) => void } | undefined;
  listen('replyClose', (id: string, result: { attachments: boolean; error?: string }) => {
    if (id !== pendingClose?.id || !result || typeof result.attachments !== 'boolean' ||
        (result.error !== undefined && (typeof result.error !== 'string' || result.error.length > 8192))) return;
    pendingClose.resolve(result); pendingClose = undefined;
  });
  const saveBounds = async () => {
    if (window.isDestroyed() || window.isFullScreen()) return;
    const temporary = `${boundsFile}.tmp`;
    await writeFile(temporary, JSON.stringify(window.getNormalBounds()), { mode: 0o600 }); await rename(temporary, boundsFile);
  };
  const requestClose = async (reason: 'quit' | 'reload' | 'update') => {
    if (closeInProgress) return;
    closeInProgress = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      const result = await new Promise<{ attachments: boolean; error?: string }>(resolve => {
        const id = randomUUID(); pendingClose = { id, resolve };
        timer = setTimeout(() => { pendingClose = undefined; resolve({ attachments: true, error: 'Whip could not confirm that drafts were saved.' }); }, 5000);
        if (rendererReady && !window.webContents.isCrashed()) emit({ kind: 'close-request', id, reason });
      });
      clearTimeout(timer);
      if (result.attachments || result.error) {
        const choice = await dialog.showMessageBox(window, { type: 'warning', buttons: ['Cancel', reason === 'quit' ? 'Quit Whip' : reason === 'update' ? 'Restart to update' : 'Reload'],
          defaultId: 0, cancelId: 0, message: 'Some work is only stored in this window',
          detail: [result.error, result.attachments ? 'Unsent file attachments will be lost.' : undefined].filter(Boolean).join('\n') });
        if (choice.response !== 1) return;
      }
      await saveBounds().catch(() => {});
      if (reason === 'update') {
        // Leave observations alive if Squirrel fails before it can quit.
        quitApproved = true;
        try { updates.quitAndInstall(); } catch (error) { quitApproved = false; throw error; }
        return;
      }
      await disposeConnections(); await native.dispose();
      if (reason === 'quit') { quitApproved = true; app.quit(); }
      else { rendererReady = false; window.webContents.setBackgroundThrottling(true); window.webContents.reload(); }
    } catch (error) {
      quitApproved = false;
      dialog.showErrorBox('Whip could not complete this action', error instanceof Error ? error.message : 'Please try again.');
    } finally { clearTimeout(timer); pendingClose = undefined; closeInProgress = false; }
  };
  const updates = new DesktopUpdates(config, emit, () => requestClose('update'), quitting => { quitApproved = quitting; });
  app.on('before-quit', event => { if (!quitApproved) { event.preventDefault(); void requestClose('quit'); } });
  app.on('will-quit', () => { updates.dispose(); void disposeConnections(); void native.dispose(); transports.dispose(); });
  app.on('second-instance', () => { window.show(); window.focus(); });
  app.on('activate', () => { window.show(); window.focus(); });
  window.on('close', event => {
    if (!quitApproved) { event.preventDefault(); void saveBounds().catch(() => {}); window.hide(); }
  });
  window.webContents.setWindowOpenHandler(({ url }) => {
    void native.openExternal(url).catch(() => {}); return { action: 'deny' };
  });
  window.webContents.on('will-navigate', (event, url) => {
    if (!rendererURL(url)) { event.preventDefault(); void native.openExternal(url).catch(() => {}); }
  });
  window.webContents.on('will-redirect', (event, url) => { if (!rendererURL(url)) event.preventDefault(); });
  window.webContents.on('will-attach-webview', event => event.preventDefault());
  window.webContents.on('render-process-gone', () => {
    window.webContents.setBackgroundThrottling(true);
    rendererReady = false; void disposeConnections(); void native.dispose();
    void dialog.showMessageBox(window, { type: 'error', message: 'The Whip window stopped responding',
      detail: 'Accepted work is still running in the daemon. Reload to reconnect.', buttons: ['Reload', 'Keep closed'] }).then(result => {
      if (result.response === 0 && !window.isDestroyed()) window.webContents.reload();
    });
  });
  powerMonitor.on('resume', () => emit({ kind: 'attention-wakeup' }));
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    { role: 'appMenu' }, { role: 'editMenu' },
    { label: 'File', submenu: [{ label: 'Close tab', accelerator: 'CmdOrCtrl+W', click: () => emit({ kind: 'close-tab' }) }] },
    { label: 'View', submenu: [{ label: 'Reload', accelerator: 'CmdOrCtrl+R', click: () => { void requestClose('reload'); } }, { role: 'togglefullscreen' }] },
    { role: 'windowMenu' },
  ]));
  window.once('ready-to-show', () => window.show());
  await window.loadURL(developmentOrigin ?? desktopURL);
}

declare const __APP_NAME__: string;
