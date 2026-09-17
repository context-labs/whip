// Phase 0 fixture only: real sandboxed preload, deliberately tiny bridge.
const { contextBridge, ipcRenderer } = require('electron');
contextBridge.exposeInMainWorld('browserSpike', {
  ping: () => ipcRenderer.invoke('browser-spike:ping'),
  present: value => ipcRenderer.invoke('browser-spike:present', value),
  navigate: value => ipcRenderer.invoke('browser-spike:navigate', value),
  focus: id => ipcRenderer.invoke('browser-spike:focus', id),
});
