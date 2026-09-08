import { urlProfile, resolveURLConnection, type AppPlatform } from '@whip/app/platform';
import { browserStorage } from './storage';

export function createBrowserPlatform(unavailable: () => void): AppPlatform {
  return {
    storage: browserStorage(() => window.localStorage, unavailable),
    windowStorage: browserStorage(() => window.sessionStorage, unavailable),
    defaultConnection: urlProfile(location.origin),
    connectionKinds: ['url'],
    resolveConnection: resolveURLConnection,
    async openExternal(url) {
      const target = new URL(url);
      if (!['https:', 'http:'].includes(target.protocol)) throw new Error('Unsupported link protocol');
      window.open(target.href, '_blank', 'noopener,noreferrer');
    },
    async copy(text) {
      if (!navigator.clipboard) throw new Error('Clipboard access requires HTTPS or localhost. Select the text to copy it manually.');
      await navigator.clipboard.writeText(text);
    },
    async download(bytes, filename, mediaType) {
      const url = URL.createObjectURL(new Blob([bytes], { type: mediaType }));
      const link = document.createElement('a');
      link.href = url; link.download = filename;
      document.body.append(link); link.click(); link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
      return 'saved';
    },
    sessionLink: path => new URL(path, location.origin).href,
  };
}
