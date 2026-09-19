import { contextBridge, ipcRenderer } from "electron";
// Fixture-only bridge: no arbitrary channel or privileged app API.
contextBridge.exposeInMainWorld("designFixture", {
  pick: (point: { x: number; y: number }) =>
    ipcRenderer.invoke("design-fixture:pick", point),
  scroll: (point: { x: number; y: number; deltaY: number }) =>
    ipcRenderer.invoke("design-fixture:scroll", point),
});
