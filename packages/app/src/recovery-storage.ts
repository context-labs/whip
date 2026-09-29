import { RecoveryError, recoveryNamespace, type RecoveryRecord, type RecoveryStorage } from '@whip/sdk';
import type { AppStorage } from './platform';

const storageKey = 'whip.web.recovery.v4';
const maxRecords = 64;
const maxBytes = 8 << 20;
const size = (value: string) => new TextEncoder().encode(value).byteLength;
type Entry = { key: string; record: RecoveryRecord };

/** Persistent windows share one locked journal namespace. Recheck the complete
 * bound inside the storage transaction: separate SDK journals can race a read. */
export function recoveryStorage(storage: AppStorage): RecoveryStorage {
  function read(): Entry[] {
    const raw = storage.getItem(storageKey);
    if (raw === null) return [];
    if (raw.length > maxBytes || size(raw) > maxBytes) throw new RecoveryError('Saved command recovery exceeds its byte limit');
    const entries: unknown = JSON.parse(raw);
    if (!Array.isArray(entries) || entries.length > maxRecords) throw new RecoveryError('Saved command recovery exceeds its record limit');
    const keys = new Set<string>();
    for (const entry of entries) {
      if (!entry || typeof entry !== 'object' || Object.keys(entry).sort().join(',') !== 'key,record'
        || typeof entry.key !== 'string' || entry.key.length > 1024 || keys.has(entry.key)
        || !entry.record || entry.record.namespace !== recoveryNamespace) throw new RecoveryError('Saved command recovery is invalid; existing records have been preserved');
      keys.add(entry.key);
    }
    return entries;
  }
  const isPersistent = () => storage.persistent !== false;
  const write = (entries: Entry[]) => {
    const raw = JSON.stringify(entries);
    if (entries.length > maxRecords || size(raw) > maxBytes) throw new RecoveryError('Command recovery is full; resolve or explicitly forget a saved request');
    const persistent = isPersistent();
    storage.setItem(storageKey, raw);
    if (persistent && !isPersistent()) throw new RecoveryError('Command recovery could not be saved to device storage');
  };
  const update = (run: () => void) => {
    if (storage.transaction) return storage.transaction(storageKey, run);
    if (storage.persistent !== false) return Promise.reject(new RecoveryError('Persistent command recovery requires shared storage locking'));
    return Promise.resolve().then(run);
  };
  const scope = (namespace: typeof recoveryNamespace) => {
    if (namespace !== recoveryNamespace) throw new RecoveryError('Invalid command recovery namespace');
  };
  return {
    list: async (namespace, limit) => { scope(namespace); return read().slice(0, limit).map(entry => entry.record); },
    put: async (namespace, key, record) => {
      scope(namespace);
      await update(() => {
        const entries = read();
        const previous = entries.find(entry => entry.key === key);
        if (previous && previous.record.request !== record.request) throw new RecoveryError('Recovery identity already has another request');
        const next = { key, record: previous?.record.accepted ? { ...record, accepted: true } : record };
        write(previous ? entries.map(entry => entry.key === key ? next : entry) : [...entries, next]);
      });
    },
    delete: async (namespace, key) => { scope(namespace); await update(() => write(read().filter(entry => entry.key !== key))); },
  };
}
