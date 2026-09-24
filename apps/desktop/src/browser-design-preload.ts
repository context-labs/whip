import { contextBridge, ipcRenderer } from "electron";
import type {
  BrowserDesignBridge,
  BrowserDesignModel,
} from "@whip/app/desktop-bridge";

/** Dedicated trusted overlay bridge; no daemon, filesystem or application APIs. */
const bridge: BrowserDesignBridge = {
  snapshot: () => ipcRenderer.invoke("whip:browser-design-overlay:snapshot"),
  intent: (input) =>
    ipcRenderer.invoke("whip:browser-design-overlay:intent", input),
  onModel(listener) {
    const receive = (
      _event: Electron.IpcRendererEvent,
      model: BrowserDesignModel,
    ) => listener(model);
    ipcRenderer.on("whip:browser-design-overlay:model", receive);
    return () => {
      ipcRenderer.removeListener("whip:browser-design-overlay:model", receive);
    };
  },
};
contextBridge.exposeInMainWorld("whipBrowserDesign", bridge);
