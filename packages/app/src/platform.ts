/** Platform effects belong to the web/Electron shell, never the runtime SDK. */
export interface AppStorage {
  keys(): string[];
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
  transaction?<T>(key: string, update: () => T): Promise<T>;
}
export interface AppPlatform {
  storage: AppStorage;
  /** Independent per-window layout storage; omitted shells retain tabs in memory. */
  windowStorage?: AppStorage;
  defaultEndpoint: string;
  openExternal(url: string): void;
  copy(text: string): Promise<void>;
  download(bytes: Uint8Array<ArrayBuffer>, filename: string, mediaType: string): void;
}

/** Keep the app usable when browser policy or quota denies persistent storage. */
export function createFallbackStorage(getStorage: () => AppStorage, onUnavailable: () => void, transaction?: AppStorage['transaction']): AppStorage {
  const memory = new Map<string, string>();
  let storage: AppStorage | undefined;
  let notified = false;
  const unavailable = () => {
    storage = undefined;
    if (!notified) { notified = true; onUnavailable(); }
  };
  try { storage = getStorage(); }
  catch { unavailable(); }
  return {
    transaction(key, update) {
      // A private in-memory fallback cannot race another browser tab. Persistent
      // read-modify-write must acquire the origin's browser lock first.
      if (!storage) return Promise.resolve().then(update);
      if (!transaction) return Promise.reject(new Error('This browser needs Web Locks to safely save command recovery across tabs. Use a current browser on HTTPS or localhost.'));
      return transaction(key, update);
    },
    keys() {
      if (storage) { try { return storage.keys(); } catch { unavailable(); } }
      return [...memory.keys()];
    },
    getItem(key) {
      if (storage) {
        try {
          const value = storage.getItem(key);
          if (value === null) memory.delete(key); else memory.set(key, value);
          return value;
        } catch { unavailable(); }
      }
      return memory.get(key) ?? null;
    },
    setItem(key, value) {
      if (storage) { try { storage.setItem(key, value); } catch { unavailable(); } }
      memory.set(key, value);
    },
    removeItem(key) {
      if (storage) { try { storage.removeItem(key); } catch { unavailable(); } }
      memory.delete(key);
    },
  };
}

export function readPreference<T>(storage: AppStorage, key: string, fallback: T): T {
  try { const value = storage.getItem(key); return value === null ? fallback : JSON.parse(value) as T; }
  catch { return fallback; }
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
