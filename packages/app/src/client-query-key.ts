import type { Client } from '@whip/sdk';

const keys = new WeakMap<Client, string>();
/** A replacement transport must not reuse another Client's reads or cursors. */
export function clientQueryKey(client: Client): string {
  let key = keys.get(client);
  if (!key) { key = crypto.randomUUID(); keys.set(client, key); }
  return key;
}
