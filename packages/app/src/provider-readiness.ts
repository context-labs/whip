import type { AppStorage } from './platform';

const prefix = 'whip.web.provider-ready.v1:';

/** Whether a host's default provider was ready the last time its inventory answered; undefined until it has. */
export function recallProviderReady(storage: AppStorage, runtimeId: string | undefined): boolean | undefined {
  if (!runtimeId) return undefined;
  try { const value = storage.getItem(prefix + runtimeId); return value === 'true' ? true : value === 'false' ? false : undefined; }
  catch { return undefined; }
}

export function rememberProviderReady(storage: AppStorage, runtimeId: string | undefined, ready: boolean) {
  if (!runtimeId) return;
  try { storage.setItem(prefix + runtimeId, String(ready)); }
  catch { /* Memory-only storage still has the in-session query cache. */ }
}
