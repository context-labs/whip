import { ipcMain, type BrowserWindow, type IpcMainInvokeEvent } from "electron";
import type { BrowserDesignController } from "./browser-design";

export function installBrowserDesignIPC(
  window: BrowserWindow,
  design: BrowserDesignController,
  trusted: (event: IpcMainInvokeEvent) => void,
): () => void {
  const channels: string[] = [];
  const methods = {
    start: (value: unknown) => design.start(value),
    stop: (value: unknown) => design.stop(value),
    update: (value: unknown) => design.update(value),
    capture: (value: unknown) => design.capture(value),
  };
  for (const [name, action] of Object.entries(methods)) {
    const channel = `whip:browser-design:${name}`;
    channels.push(channel);
    ipcMain.handle(channel, (event, value: unknown, ...extra: unknown[]) => {
      trusted(event);
      if (
        event.sender !== window.webContents ||
        event.senderFrame !== window.webContents.mainFrame ||
        extra.length
      )
        throw new Error("Untrusted Design request");
      return action(value);
    });
  }
  for (const name of ["snapshot", "intent"] as const) {
    const channel = `whip:browser-design-overlay:${name}`;
    channels.push(channel);
    ipcMain.handle(channel, (event, value: unknown, ...extra: unknown[]) => {
      design.assertOverlay(event);
      if (extra.length || (name === "snapshot" && value !== undefined))
        throw new Error("Invalid Design request");
      return name === "snapshot" ? design.model() : design.intent(value);
    });
  }
  return () => {
    for (const channel of channels) ipcMain.removeHandler(channel);
  };
}
