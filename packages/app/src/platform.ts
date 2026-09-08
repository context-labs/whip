import type { ConnectionOptions, ConnectionProfile, ConnectionTarget, ResolvedConnection } from './connections';
export { localProfile, urlProfile, validateProfile, resolveURLConnection } from './connections';
export type { ConnectionOptions, ConnectionProfile, ConnectionTarget, ResolvedConnection } from './connections';

/** Platform effects belong to the web/Electron shell, never the runtime SDK. */
export interface AppStorage {
  /** False when successful writes are retained only for this renderer's lifetime. */
  readonly persistent?: boolean;
  keys(): string[];
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
  transaction?<T>(key: string, update: () => T): Promise<T>;
}
export type AppUpdateSnapshot = Readonly<{
  state: 'idle' | 'checking' | 'available' | 'downloaded' | 'current' | 'error';
  version?: string;
  error?: string;
}>;
export interface AppUpdates {
  readonly currentVersion: string;
  getSnapshot(): AppUpdateSnapshot;
  subscribe(listener: () => void): () => void;
  check(): Promise<void>;
  install(): Promise<void>;
}
export interface AppNotification {
  id: string;
  title: string;
  body: string;
  path: string;
}
/** A bounded diagnostic result from the native host; never raw logs or environment. */
export interface LocalRuntimeStatus {
  state: 'missing' | 'stopped' | 'running' | 'unhealthy' | 'incompatible';
  executable?: string;
  home: string;
  clientBuild?: string;
  daemonBuild?: string;
  message: string;
  canInstall: boolean;
}
export interface AppLocalRuntime {
  /** Inspect the installation and daemon without creating state or starting work. */
  test(): Promise<LocalRuntimeStatus>;
  /** Use a native chooser; selecting an executable does not restart the daemon. */
  choose(): Promise<LocalRuntimeStatus>;
  install(): Promise<LocalRuntimeStatus>;
  /** Explicitly interrupts the local daemon's work. */
  restart(): Promise<LocalRuntimeStatus>;
}
export interface AppPlatform {
  storage: AppStorage;
  /** Independent per-window layout storage; omitted shells retain tabs in memory. */
  windowStorage?: AppStorage;
  defaultEndpoint?: string;
  defaultConnection?: ConnectionProfile;
  connectionKinds?: readonly ConnectionTarget['kind'][];
  resolveConnection?(profile: ConnectionProfile, options: ConnectionOptions): Promise<ResolvedConnection>;
  openExternal(url: string): Promise<void>;
  copy(text: string): Promise<void>;
  download(bytes: Uint8Array<ArrayBuffer>, filename: string, mediaType: string): Promise<'saved' | 'cancelled' | void>;
  sessionLink?(path: string, profile?: ConnectionProfile): string;
  pickDirectory?(): Promise<string | undefined>;
  updates?: AppUpdates;
  localRuntime?: AppLocalRuntime;
  notify?(notification: AppNotification): Promise<void>;
  setNotificationsEnabled?(enabled: boolean): void;
  onCloseTab?(listener: () => void): () => void;
  hideWindow?(): void;
  /** Release shell-owned observations after the shared application unmounts. */
  dispose?(): void;
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
    get persistent() { return storage !== undefined && storage.persistent !== false; },
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
