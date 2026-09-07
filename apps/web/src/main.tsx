import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { createWhipApplication } from '@whip/app';
import { initializeTheme } from '@whip/ui/themes';
import { createFallbackStorage, type AppPlatform, type AppStorage } from '@whip/app/platform';
import '@whip/ui/fonts.css';
import '@whip/ui/reset.css';

let storageUnavailable = false;
let reportStorage: (() => void) | undefined;
const transaction: NonNullable<AppStorage['transaction']> = async (key, update) => {
  if (!navigator.locks) throw new Error('This browser needs Web Locks to safely save command recovery across tabs. Use a current browser on HTTPS or localhost.');
  let entered = false;
  try { return await navigator.locks.request(key, () => { entered = true; return update(); }); }
  catch (error) {
    if (entered) throw error;
    throw new Error('The browser denied the lock needed to save command recovery across tabs. Use a current browser on HTTPS or localhost.', { cause: error });
  }
};
const storage = createFallbackStorage(() => {
  const browserStorage = window.localStorage;
  return {
    keys: () => Array.from({ length: browserStorage.length }, (_, index) => browserStorage.key(index)).filter((key): key is string => key !== null),
    getItem: (key: string) => browserStorage.getItem(key),
    setItem: (key: string, value: string) => browserStorage.setItem(key, value),
    removeItem: (key: string) => browserStorage.removeItem(key),
  };
}, () => {
  storageUnavailable = true;
  reportStorage?.();
}, transaction);
initializeTheme({ storage });
const platform: AppPlatform = {
  storage,
  windowStorage: createFallbackStorage(() => {
    const browserStorage = window.sessionStorage;
    return {
      keys: () => Array.from({ length: browserStorage.length }, (_, index) => browserStorage.key(index)).filter((key): key is string => key !== null),
      getItem: (key: string) => browserStorage.getItem(key),
      setItem: (key: string, value: string) => browserStorage.setItem(key, value),
      removeItem: (key: string) => browserStorage.removeItem(key),
    };
  }, () => { storageUnavailable = true; reportStorage?.(); }),
  defaultEndpoint: location.origin,
  openExternal(url) {
    const target = new URL(url);
    if (!['https:', 'http:'].includes(target.protocol)) throw new Error('Unsupported link protocol');
    window.open(target.href, '_blank', 'noopener,noreferrer');
  },
  async copy(text) {
    if (!navigator.clipboard) throw new Error('Clipboard access requires HTTPS or localhost. Select the text to copy it manually.');
    await navigator.clipboard.writeText(text);
  },
  download(bytes, filename, mediaType) {
    const url = URL.createObjectURL(new Blob([bytes], { type: mediaType }));
    const link = document.createElement('a'); link.href = url; link.download = filename;
    document.body.append(link); link.click(); link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  },
};
const application = createWhipApplication(platform);
reportStorage = () => application.runtime.report('Browser storage is unavailable. Preferences, drafts, and command recovery identities will be kept only until this page is closed or reloaded.');
createRoot(document.getElementById('root')!).render(<StrictMode><application.Application /></StrictMode>);
// The runtime reports setup errors; the SDK exposes current connection errors.
void application.runtime.connect().catch(() => {});
if (storageUnavailable) reportStorage();
// Files are window-memory drafts; install the browser's leave warning only
// while there is something a reload cannot restore.
const warnBeforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
const updateLeaveWarning = () => {
  window.removeEventListener('beforeunload', warnBeforeUnload);
  if (application.runtime.compositions.hasAttachments()) window.addEventListener('beforeunload', warnBeforeUnload);
};
const unsubscribeCompositions = application.runtime.compositions.subscribe(updateLeaveWarning);
window.addEventListener('pagehide', event => { application.runtime.flushDrafts(); if (!event.persisted) application.dispose(); });
if (import.meta.hot) import.meta.hot.dispose(() => { unsubscribeCompositions(); window.removeEventListener('beforeunload', warnBeforeUnload); application.dispose(); });
