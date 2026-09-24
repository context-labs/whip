import { createFallbackStorage, type AppStorage } from '@whip/app/platform';

const transaction: NonNullable<AppStorage['transaction']> = async (key, update) => {
  if (!navigator.locks) throw new Error('This browser needs Web Locks to safely save command recovery across tabs. Use a current browser on HTTPS or localhost.');
  let entered = false;
  try { return await navigator.locks.request(key, () => { entered = true; return update(); }); }
  catch (error) {
    if (entered) throw error;
    throw new Error('The browser denied the lock needed to save command recovery across tabs. Use a current browser on HTTPS or localhost.', { cause: error });
  }
};

export function browserStorage(get: () => Storage, unavailable: () => void, prefix = ''): AppStorage {
  return createFallbackStorage(() => {
    const storage = get();
    return {
      keys: () => Array.from({ length: storage.length }, (_, index) => storage.key(index))
        .filter((key): key is string => key !== null && key.startsWith(prefix)).map(key => key.slice(prefix.length)),
      getItem: key => storage.getItem(prefix + key),
      setItem: (key, value) => storage.setItem(prefix + key, value),
      removeItem: key => storage.removeItem(prefix + key),
    };
  }, unavailable, (key, update) => transaction(prefix + key, update));
}
