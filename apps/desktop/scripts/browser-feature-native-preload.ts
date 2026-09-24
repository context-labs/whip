// Fixture-only fixed probes; no generic IPC escape is exposed by production preload.
import '../src/preload';
import { contextBridge, ipcRenderer } from 'electron';
contextBridge.exposeInMainWorld('browserFeatureTest', {
  snapshot: () => ipcRenderer.invoke('whip:browser:snapshot'),
  identity: () => ipcRenderer.invoke('whip:browser-agent:identity'),
});
