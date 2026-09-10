import { app, BrowserWindow, dialog, ipcMain, Menu, nativeTheme, powerMonitor, protocol, screen, session, shell } from 'electron';
import { randomUUID } from 'node:crypto';
import { homedir } from 'node:os';
import { readFile, writeFile, rename } from 'node:fs/promises';
import path from 'node:path';
import { validateProfile, type ConnectionProfile } from '@whip/app/platform';
import { unixSocket } from '@whip/sdk/node';
import type { DesktopEvent, HostPrompt } from '@whip/app/desktop-bridge';
import { createAssetHandler, desktopScheme, desktopURL, isDesktopURL, type RendererManifest } from './assets';
import { LocalRuntime, readRuntimeManifest, runtimeEnvironment } from './runtime';
import { DesktopTransports, validHandle } from './transport';
import { NativeEffects } from './native';
import { ProjectEditors, validateOpenProject, verifyProjectRuntime } from './project-open';
import { SSHConnection } from './ssh';
import { DesktopUpdates, readDesktopConfig } from './updates';
import { sessionLinkPath } from './links';
import { attachStartupProbe } from './startup-probe';

protocol.registerSchemesAsPrivileged([{ scheme: desktopScheme,
  privileges: { standard: true, secure: true, supportFetchAPI: true, corsEnabled: true } }]);
// Fixtures must opt in explicitly; never silently attach dev scripts to ~/.whipcode.
if (!app.isPackaged || process.env.WHIP_DESKTOP_FIXTURE) {
  if (!process.env.WHIP_DESKTOP_FIXTURE || !process.env.WHIPCODE_HOME || !process.env.WHIP_DESKTOP_USER_DATA || !process.env.WHIP_DESKTOP_EXECUTABLE)
    throw new Error('Development requires isolated WHIPCODE_HOME, WHIP_DESKTOP_USER_DATA and WHIP_DESKTOP_EXECUTABLE paths');
  if (!path.isAbsolute(process.env.WHIPCODE_HOME) || !path.isAbsolute(process.env.WHIP_DESKTOP_USER_DATA) || !path.isAbsolute(process.env.WHIP_DESKTOP_EXECUTABLE))
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
  if (manifest.rendererDigest !== renderer.digest) throw new Error('The renderer and runtime belong to different builds');
  const source = app.isPackaged ? path.join(process.resourcesPath, '..', 'Helpers') : path.join(root, '..', 'native');
  const localRuntime = new LocalRuntime({ source, manifest,
    settingsFile: path.join(app.getPath('userData'), 'native-local-runtime.json'),
    confirmUpdate: async detail => (await dialog.showMessageBox(window, { type: 'warning',
      message: 'Restart and update the local Whip backend?', detail,
      buttons: ['Later', 'Restart and update'], defaultId: 0, cancelId: 0 })).response === 1,
    defaultExecutable: process.env.WHIP_DESKTOP_FIXTURE ? process.env.WHIP_DESKTOP_EXECUTABLE : undefined });
  let runtimeLifetime = new AbortController();
  const cancelRuntimeActions = () => { runtimeLifetime.abort(); runtimeLifetime = new AbortController(); };
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
  // On macOS the native title bar hides (hiddenInset) and the renderer's top
  // chrome owns window dragging; the dots are vertically centered in the 48px
  // sidebar brand row / tab strip and clear the sidebar's 8px padding.
  const window = new BrowserWindow({ width: 1200, height: 800, minWidth: 800, minHeight: 600, ...bounds,
    show: false, backgroundColor: '#111111', title: 'Whip',
    ...(process.platform === 'darwin' ? { titleBarStyle: 'hiddenInset' as const, trafficLightPosition: { x: 12, y: 18 } } : {}),
    webPreferences: { preload: path.join(root, 'preload.cjs'), sandbox: true, contextIsolation: true,
      nodeIntegration: false, webSecurity: true, webviewTag: false, spellcheck: true } });
  await attachStartupProbe(window, { userData: app.getPath('userData'), rendererDigest: renderer.digest, quit: () => app.quit() });
  const emit = (event: DesktopEvent) => { if (!window.isDestroyed() && !window.webContents.isDestroyed()) window.webContents.send('whip:event', event); };
  const transports = new DesktopTransports(emit);
  const native = new NativeEffects(window, emit);
  const projectEditors = new ProjectEditors(directory => shell.openPath(directory));
  const connections = new Map<string, { profile: ConnectionProfile; controller: AbortController; socket?: string; ssh?: SSHConnection }>();
  let openingProject: { id: string; controller: AbortController } | undefined;
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
    if (openingProject?.id === id) openingProject.controller.abort();
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
  let runtimeActionPending = false;
  handle('getSystemContrast', (...args: unknown[]) => {
    if (args.length) throw new Error('System appearance does not accept arguments.');
    return nativeTheme.shouldUseHighContrastColors;
  });
  const appearanceChanged = () => emit({ kind: 'system-contrast', highContrast: nativeTheme.shouldUseHighContrastColors });
  nativeTheme.on('updated', appearanceChanged);
  window.once('closed', () => nativeTheme.removeListener('updated', appearanceChanged));
  const runtimeAction = (name: string, action: (signal: AbortSignal) => Promise<unknown>) => handle(name, async (...args: unknown[]) => {
    if (args.length) throw new Error('Local runtime actions do not accept arguments.');
    if (runtimeActionPending) throw new Error('A local runtime action is already in progress.');
    runtimeActionPending = true;
    try { return await action(AbortSignal.any([runtimeLifetime.signal, AbortSignal.timeout(45_000)])); }
    finally { runtimeActionPending = false; }
  });
  runtimeAction('testLocalRuntime', signal => localRuntime.test(signal));
  runtimeAction('installDefaultLocalRuntime', signal => localRuntime.installDefault(signal));
  runtimeAction('chooseLocalRuntime', async signal => {
    const choice = await dialog.showOpenDialog(window, { title: 'Choose whipcode executable', properties: ['openFile'], buttonLabel: 'Use whipcode' });
    signal.throwIfAborted();
    return choice.canceled || !choice.filePaths[0] ? localRuntime.test(signal) : localRuntime.choose(choice.filePaths[0], signal);
  });
  runtimeAction('installLocalRuntime', async signal => {
    const current = await localRuntime.test(signal);
    if (!current.canInstall) throw new Error('whipcode is already installed. Desktop will not overwrite an existing backend.');
    const choice = await dialog.showSaveDialog(window, { title: 'Install whipcode', buttonLabel: 'Install whipcode',
      defaultPath: current.executable || path.join(homedir(), '.local/bin/whipcode'), message: 'Desktop and terminal will share this executable. Desktop will keep the binary updated with the app.' });
    signal.throwIfAborted();
    return choice.canceled || !choice.filePath ? current : localRuntime.install(choice.filePath, signal);
  });
  runtimeAction('restartLocalRuntime', signal => localRuntime.restart(signal));
  handle('prepareConnection', async (id: string, input: unknown) => {
    validHandle(id);
    const profile = validateProfile(input);
    if (profile.target.kind === 'url') throw new Error('URL connections belong to the shared renderer');
    if (connections.has(id) || connections.size >= 16) throw new Error('Desktop connection limit reached');
    const connection = { profile, controller: new AbortController() } as NonNullable<ReturnType<typeof connections.get>>;
    connections.set(id, connection);
    try {
      const progress = (message: string) => { if (connections.get(id) === connection) emit({ kind: 'progress', attemptId: id, message }); };
      const signal = connection.controller.signal;
      if (profile.target.kind === 'ssh') {
        progress('Preparing SSH…');
        const executable = await localRuntime.executable(signal);
        const env = await runtimeEnvironment(signal);
        connection.ssh = new SSHConnection({ target: profile.target, executable, env, signal, progress,
          prompt: (value, lifetime) => prompt(id, value, lifetime) });
        connection.socket = await connection.ssh.getSocket();
      } else connection.socket = await localRuntime.prepare(signal, progress);
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
  let editorList: ReturnType<ProjectEditors['list']> | undefined;
  handle('listProjectEditors', (...args: unknown[]) => {
    if (args.length) throw new Error('Editor discovery does not accept arguments.');
    return editorList ??= projectEditors.list(runtimeLifetime.signal).finally(() => { editorList = undefined; });
  });
  handle('openProject', async (input: unknown, urlSource?: unknown) => {
    const request = validateOpenProject(input);
    validHandle(request.connectionId);
    const connection = connections.get(request.connectionId);
    const source = urlSource === undefined ? connection?.profile : validateProfile(urlSource);
    if (!source) throw new Error('The source host is disconnected. Reconnect it before opening this folder.');
    if (urlSource !== undefined && source.target.kind !== 'url') throw new Error('An external source must use a Whip server URL.');
    if (openingProject) throw new Error('Wait for the current editor request to finish.');
    if (source.runtimeId && source.runtimeId !== request.runtimeId)
      throw new Error('This conversation belongs to a different runtime. Reconnect the source host.');
    openingProject = { id: request.connectionId, controller: new AbortController() };
    const signal = AbortSignal.any([openingProject.controller.signal, ...(connection ? [connection.controller.signal] : []), runtimeLifetime.signal, AbortSignal.timeout(15_000)]);
    try {
      const socket = connection?.ssh ? await connection.ssh.getSocket() : connection?.socket;
      const endpoint = source.target.kind === 'url' ? source.target.endpoint : socket ? unixSocket(socket) : undefined;
      if (!endpoint) throw new Error('The source host is not ready. Reconnect it before opening this folder.');
      await verifyProjectRuntime(endpoint, request.runtimeId, signal);
      signal.throwIfAborted();
      if (connections.get(request.connectionId) !== connection) throw new Error('The source host changed. Reopen the conversation menu.');
      await projectEditors.open(request, source.target, signal);
    } finally { openingProject = undefined; }
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
  let checkedLocalUpdate = false;
  openSession = path => {
    window.show(); window.focus();
    if (rendererReady) emit({ kind: 'navigate', path }); else pendingSessionPath = path;
  };
  listen('ready', () => {
    rendererReady = true; updates.ready(); if (!window.isVisible()) window.show();
    if (!checkedLocalUpdate) {
      checkedLocalUpdate = true;
      const signal = runtimeLifetime.signal;
      void localRuntime.synchronize(signal).catch(error => {
        if (!signal.aborted && !(error instanceof Error && error.name === 'AbortError')) dialog.showErrorBox('Local backend update needs attention', error instanceof Error ? error.message : 'Connect This Mac to retry the update.');
      });
    }
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
  const requestClose = async (reason: 'quit' | 'reload' | 'update', updateVersion?: string) => {
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
      if (result.attachments || result.error || reason === 'update') {
        const choice = await dialog.showMessageBox(window, { type: 'warning', buttons: ['Cancel', reason === 'quit' ? 'Quit Whip' : reason === 'update' ? 'Restart to update' : 'Reload'],
          defaultId: 0, cancelId: 0, message: reason === 'update' ? 'Restart and update Whip?' : 'Some work is only stored in this window',
          detail: [reason === 'update' ? 'The app and its managed local backend will update together. Restarting the backend interrupts work from all connected clients; sessions and configuration remain on disk.' : undefined,
            result.error, result.attachments ? 'Unsent file attachments will be lost.' : undefined].filter(Boolean).join('\n') });
        if (choice.response !== 1) return;
      }
      await saveBounds().catch(() => {});
      if (reason === 'update') {
        await localRuntime.approveUpdate(updateVersion, runtimeLifetime.signal);
        // Leave observations alive if Squirrel fails before it can quit.
        quitApproved = true;
        try { updates.quitAndInstall(); } catch (error) {
          quitApproved = false; await localRuntime.approveUpdate(undefined, runtimeLifetime.signal); throw error;
        }
        return;
      }
      await disposeConnections(); await native.dispose();
      cancelRuntimeActions();
      if (reason === 'quit') { quitApproved = true; app.quit(); }
      else { rendererReady = false; window.webContents.setBackgroundThrottling(true); window.webContents.reload(); }
    } catch (error) {
      quitApproved = false;
      dialog.showErrorBox('Whip could not complete this action', error instanceof Error ? error.message : 'Please try again.');
    } finally { clearTimeout(timer); pendingClose = undefined; closeInProgress = false; }
  };
  const updates = new DesktopUpdates(config, emit, version => requestClose('update', version), quitting => {
    quitApproved = quitting;
    if (!quitting) void localRuntime.approveUpdate(undefined, runtimeLifetime.signal).catch(() => {});
  });
  app.on('before-quit', event => { if (!quitApproved) { event.preventDefault(); void requestClose('quit'); } });
  app.on('will-quit', () => { runtimeLifetime.abort(); updates.dispose(); void disposeConnections(); void native.dispose(); transports.dispose(); });
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
    cancelRuntimeActions();
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
