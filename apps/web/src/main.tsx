import type { DesktopBridge } from '@whip/app/desktop-bridge';
import { mountApplication } from './bootstrap';
import { createBrowserPlatform } from './platform/browser';
import { createDesktopPlatform } from './platform/desktop';

declare global { interface Window { whipDesktop?: DesktopBridge } }

let storageUnavailable = false;
let reportStorage: (() => void) | undefined;
const unavailable = () => { storageUnavailable = true; reportStorage?.(); };
const desktop = window.whipDesktop;
try {
  if ((location.protocol === 'whip-app:' || desktop) && desktop?.version !== 2)
    throw new Error('The desktop host is unavailable or incompatible. Restart Whip or reinstall the application.');
  const platform = desktop ? createDesktopPlatform(desktop, unavailable) : createBrowserPlatform(unavailable);
  const application = mountApplication(platform, desktop);
  reportStorage = () => application.runtime.report('Device storage is unavailable. Preferences, drafts, and command recovery identities will be kept only until this page is closed or reloaded.');
  if (storageUnavailable) reportStorage();
  if (import.meta.hot) import.meta.hot.dispose(application.dispose);
} catch (error) {
  const root = document.getElementById('root');
  if (root) { root.setAttribute('role', 'alert'); root.textContent = error instanceof Error ? error.message : String(error); }
}
