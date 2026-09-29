import type { RecoveryRecord, RecoveryStorage } from '@whip/sdk';

const prefix = 'whip.example.v4.recovery:';
const maxRecords = 32, maxBytes = 4 << 20;
type StorageLike = Pick<Storage, 'length' | 'key' | 'getItem' | 'setItem' | 'removeItem'>;
type Locks = { request<T>(name: string, callback: () => Promise<T>): Promise<T> };
const bytes = (value: unknown) => new TextEncoder().encode(JSON.stringify(value)).byteLength;

/** Separate tabs have separate journal queues. Serialize this origin's storage
 * operations and recheck put bounds under the lock before any request is sent. */
export function browserRecoveryStorage(storage: StorageLike, locks: Locks | undefined): RecoveryStorage {
  const locked = <T>(read: () => Promise<T>) => {
    if (!locks) return Promise.reject(new Error('Recovery needs Web Locks; use a secure browser origin before submitting work.'));
    return locks.request(prefix, read);
  };
  const keys = (namespace: string) => Array.from({ length: storage.length }, (_, i) => storage.key(i)).filter((key): key is string => key !== null && key.startsWith(prefix + namespace + ':'));
  const read = (key: string): unknown => JSON.parse(storage.getItem(key) ?? 'null');
  return {
    list: (namespace, limit) => locked(async () => keys(namespace).slice(0, limit).map(read)),
    put: (namespace, identity, record) => locked(async () => {
      const key = prefix + namespace + ':' + identity, current = keys(namespace);
      const previous = storage.getItem(key);
      let next: RecoveryRecord = record;
      if (previous !== null) {
        const old: unknown = JSON.parse(previous);
        if (!old || typeof old !== 'object' || !('request' in old) || old.request !== record.request || !('accepted' in old) || typeof old.accepted !== 'boolean') throw new Error('Recovery storage identity or payload changed');
        if (old.accepted) next = { ...record, accepted: true };
      }
      if (previous === null && current.length >= maxRecords || current.filter(item => item !== key).reduce((total, item) => total + bytes(read(item)), bytes(next)) > maxBytes) throw new Error('Recovery storage is full; resolve or explicitly forget a record before sending');
      storage.setItem(key, JSON.stringify(next));
    }),
    delete: (namespace, identity) => locked(async () => { storage.removeItem(prefix + namespace + ':' + identity); }),
  };
}
