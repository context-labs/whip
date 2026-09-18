import { ipcMain, type BrowserWindow, type IpcMainInvokeEvent } from 'electron';
import type { BrowserManager } from './browser-manager';
import type { BrowserControl } from './browser-control';
import type { BrowserPreviewAuthority } from './browser-preview-authority';
import type { BrowserHumanPreview } from './browser-human-preview';

/** The trusted application main frame is the only caller; guests have no preload. */
export function installBrowserIPC(window: BrowserWindow, manager: BrowserManager, trusted: (event: IpcMainInvokeEvent) => void, control?: BrowserControl, previews?: BrowserPreviewAuthority, human?: BrowserHumanPreview): () => void {
  const methods = {
    snapshot: () => manager.snapshot(),
    restore: (value: unknown) => manager.restore(value),
    create: (value: unknown) => manager.create(value),
    createPreview: (value: unknown) => { if (!human) throw new Error('Human preview is unavailable'); return human.create(value); },
    admitted: (value: unknown) => human ? human.admitted(value) : manager.admitted(value),
    present: (value: unknown) => manager.present(value),
    act: (value: unknown) => { human?.assertIdle(value); return manager.act(value); },
    close: (value: unknown) => manager.close(value),
  };
  for (const [name, action] of Object.entries(methods)) ipcMain.handle(`whip:browser:${name}`, (event, value: unknown, ...extra: unknown[]) => {
    trusted(event);
    if (event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame || extra.length || (name === 'snapshot' && value !== undefined)) throw new Error('Untrusted browser request');
    return action(value);
  });
  const agentMethods = control ? {
    identity: () => control.identity(),
    preview: (value: unknown) => { if (!previews) throw new Error('Preview unavailable'); return previews.describe(value); },
    select: (value: unknown) => control.select(value),
    inventory: (value: unknown) => control.inventory(value),
    dispatch: (value: unknown) => control.dispatch(value),
    cancel: (value: unknown) => control.cancel(value),
    release: (value: unknown) => control.release(value),
  } : {};
  for (const [name, action] of Object.entries(agentMethods)) ipcMain.handle(`whip:browser-agent:${name}`, (event, value: unknown, ...extra: unknown[]) => {
    trusted(event);
    if (event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame || extra.length || (name === 'identity' && value !== undefined)) throw new Error('Untrusted browser provider request');
    return action(value);
  });
  return () => {
    for (const name of Object.keys(methods)) ipcMain.removeHandler(`whip:browser:${name}`);
    for (const name of Object.keys(agentMethods)) ipcMain.removeHandler(`whip:browser-agent:${name}`);
  };
}
