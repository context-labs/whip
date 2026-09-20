import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import type { BrowserWindow } from 'electron';
import { BrowserManager } from '../src/browser-manager';
import { BrowserControl } from '../src/browser-control';
import { installBrowserIPC } from '../src/browser-ipc';

/** Local fixture only: never attaches to/restarts the user's daemon or app. */
export async function testBrowserDiscovery(window: BrowserWindow, directory: string) {
  if (!process.env.BROWSER_NATIVE_FIXTURE) return;
  const fixture = JSON.parse(process.env.BROWSER_NATIVE_FIXTURE);
  await window.loadURL(fixture.frontend);
  let control: BrowserControl;
  const manager = new BrowserManager(window, event => { control?.observe(event); window.webContents.send('whip:browser:event', event); }, {
    invalidateControl: (id, reason) => control?.invalidate(id, reason),
  });
  control = new BrowserControl(manager, event => window.webContents.send('whip:browser-agent:event', event));
  const cleanup = installBrowserIPC(window, manager, event => {
    if (event.sender !== window.webContents || event.senderFrame !== window.webContents.mainFrame || new URL(event.senderFrame.url).origin !== new URL(fixture.frontend).origin) throw new Error('Untrusted fixture request');
  }, control);
  try {
    await window.webContents.executeJavaScript(readFileSync(path.join(directory, 'discovery.js'), 'utf8'));
    const result = await window.webContents.executeJavaScript(`runBrowserDiscovery(${JSON.stringify({ endpoint: fixture.endpoint, cwd: directory })})`);
    assert.equal(result.ok, true); console.log('NATIVE_DISCOVERY_DAEMON_OK', JSON.stringify(result));
  } finally { control.dispose(); manager.dispose(); cleanup(); }
}
