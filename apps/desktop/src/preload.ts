import { contextBridge, ipcRenderer } from 'electron';
import type { DesktopBridge, DesktopEvent } from '@whip/app/desktop-bridge';

// No ipcRenderer, Electron event, filesystem path authority, or generic invoke
// is exported. The host validates every method again at the privileged boundary.
const invoke = (method: string, ...args: unknown[]) => ipcRenderer.invoke(`whip:${method}`, ...args);
const send = (method: string, ...args: unknown[]) => ipcRenderer.send(`whip:${method}`, ...args);
const bridge: DesktopBridge = {
  version: 2, appVersion: __APP_VERSION__, connectionKinds: ['local', 'url', 'ssh'],
  getSystemContrast: () => invoke('getSystemContrast'),
  // Keep in sync with the BrowserWindow hiddenInset setup in main.ts.
  ...(process.platform === 'darwin' ? { chrome: 'inset' as const } : {}),
  sessionScheme: __APP_NAME__ === 'Whip Beta' ? 'whip-beta' : 'whip',
  onEvent(listener) {
    const receive = (_event: Electron.IpcRendererEvent, value: DesktopEvent) => listener(value);
    ipcRenderer.on('whip:event', receive);
    return () => { ipcRenderer.removeListener('whip:event', receive); };
  },
  prepareConnection: (id, profile) => invoke('prepareConnection', id, profile),
  releaseConnection: id => send('releaseConnection', id),
  openTransport: (id, connectionId) => invoke('openTransport', id, connectionId),
  sendTransport: (id, sequence, frame) => {
    if (typeof frame !== 'string' || frame.length > 1 << 20) throw new Error('Desktop frame limit exceeded');
    send('sendTransport', id, sequence, frame);
  },
  acknowledgeTransport: (id, sequence) => send('acknowledgeTransport', id, sequence),
  closeTransport: id => send('closeTransport', id),
  copy: text => invoke('copy', text),
  openExternal: url => invoke('openExternal', url),
  pickDirectory: () => invoke('pickDirectory'),
  listProjectEditors: () => invoke('listProjectEditors'),
  openProject: (request, urlSource) => invoke('openProject', request, urlSource),
  testLocalRuntime: () => invoke('testLocalRuntime'),
  chooseLocalRuntime: () => invoke('chooseLocalRuntime'),
  installLocalRuntime: () => invoke('installLocalRuntime'),
  installDefaultLocalRuntime: () => invoke('installDefaultLocalRuntime'),
  restartLocalRuntime: () => invoke('restartLocalRuntime'),
  beginSave: (filename, mediaType, bytes) => invoke('beginSave', filename, mediaType, bytes),
  writeSave: (id, offset, bytes) => {
    if (!(bytes instanceof Uint8Array) || bytes.length > 256 << 10) return Promise.reject(new Error('Save chunk limit exceeded'));
    return invoke('writeSave', id, offset, bytes);
  },
  finishSave: id => invoke('finishSave', id),
  cancelSave: id => invoke('cancelSave', id),
  answerPrompt: (id, values) => invoke('answerPrompt', id, values),
  replyClose: (id, result) => send('replyClose', id, result),
  notify: options => invoke('notify', options),
  setNotificationsEnabled: enabled => send('setNotificationsEnabled', enabled),
  hideWindow: () => send('hideWindow'),
  checkForUpdates: () => invoke('checkForUpdates'),
  installUpdate: () => invoke('installUpdate'),
  ready: () => send('ready'),
};
contextBridge.exposeInMainWorld('whipDesktop', bridge);

declare const __APP_VERSION__: string;
declare const __APP_NAME__: string;
