import type { IpcRenderer } from 'electron';
import type { BrowserPlatform, BrowserEvent, BrowserAgentBridge, BrowserAgentEvent, BrowserDesignEvent } from '@whip/app/desktop-bridge';

export function browserAgentPreload(ipc: IpcRenderer): BrowserAgentBridge {
  return {
    identity: () => ipc.invoke('whip:browser-agent:identity'),
    preview: input => ipc.invoke('whip:browser-agent:preview', input),
    select: input => ipc.invoke('whip:browser-agent:select', input),
    inventory: input => ipc.invoke('whip:browser-agent:inventory', input),
    dispatch: input => ipc.invoke('whip:browser-agent:dispatch', input),
    cancel: input => { void ipc.invoke('whip:browser-agent:cancel', input).catch(() => {}); },
    release: input => ipc.invoke('whip:browser-agent:release', input),
    onEvent(listener) {
      const receive = (_event: Electron.IpcRendererEvent, event: BrowserAgentEvent) => listener(event);
      ipc.on('whip:browser-agent:event', receive);
      return () => { ipc.removeListener('whip:browser-agent:event', receive); };
    },
  };
}

/** Keep the native method list closed; never export invoke or Electron event objects. */
export function browserPreload(ipc: IpcRenderer): BrowserPlatform {
  return {
    version: 1,
    design: {
      start: target => ipc.invoke('whip:browser-design:start', target),
      stop: lease => ipc.invoke('whip:browser-design:stop', lease),
      update: input => ipc.invoke('whip:browser-design:update', input),
      capture: input => ipc.invoke('whip:browser-design:capture', input),
      onEvent(listener) {
        const receive = (_event: Electron.IpcRendererEvent, event: BrowserDesignEvent) => listener(event);
        ipc.on('whip:browser-design:event', receive);
        return () => { ipc.removeListener('whip:browser-design:event', receive); };
      },
    },
    snapshot: () => ipc.invoke('whip:browser:snapshot'),
    restore: input => ipc.invoke('whip:browser:restore', input),
    create: input => ipc.invoke('whip:browser:create', input),
    createPreview: input => ipc.invoke('whip:browser:createPreview', input),
    admitted: input => ipc.invoke('whip:browser:admitted', input),
    present: input => ipc.invoke('whip:browser:present', input),
    act: input => ipc.invoke('whip:browser:act', input),
    close: input => ipc.invoke('whip:browser:close', input),
    onEvent(listener) {
      const receive = (_event: Electron.IpcRendererEvent, event: BrowserEvent) => listener(event);
      ipc.on('whip:browser:event', receive);
      return () => { ipc.removeListener('whip:browser:event', receive); };
    },
  };
}
