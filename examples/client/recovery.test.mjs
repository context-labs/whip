import assert from 'node:assert/strict';
import { test } from 'node:test';
import { RecoveryJournal, recoveryNamespace } from '@whip/sdk';
import { browserRecoveryStorage } from './recovery.ts';
class Storage {
  data = new Map(); get length() { return this.data.size; }
  key(index) { return [...this.data.keys()][index] ?? null; }
  getItem(key) { return this.data.get(key) ?? null; }
  setItem(key, value) { this.data.set(key, value); }
  removeItem(key) { this.data.delete(key); }
}
class Locks {
  pending = Promise.resolve();
  request(_name, callback) { const next = this.pending.then(callback); this.pending = next.catch(() => {}); return next; }
}
const record = (id, accepted = false) => ({ namespace: recoveryNamespace, version: 1, runtimeID: 'runtime', clientID: 'client', accepted,
  request: JSON.stringify({ method: 'sessions.submit', params: { identity: { client_id: 'client', request_id: id }, session_id: 'session', source: 'user', parts: [{ type: 'text', text: 'authored input' }] } }) });

test('independent tab journals cannot exceed shared count or byte bounds', async () => {
  const storage = new Storage(), locks = new Locks();
  const first = new RecoveryJournal(browserRecoveryStorage(storage, locks), { maxRecords: 32, maxBytes: 4 << 20 });
  const second = new RecoveryJournal(browserRecoveryStorage(storage, locks), { maxRecords: 32, maxBytes: 4 << 20 });
  for (let index = 0; index < 31; index++) await first.put(record('seed-' + index));
  const results = await Promise.allSettled([first.put(record('first')), second.put(record('second'))]);
  assert.deepEqual(results.map(item => item.status).sort(), ['fulfilled', 'rejected']);
  assert.equal((await first.list()).length, 32); assert.equal((await second.list()).length, 32);
  const raw = browserRecoveryStorage(new Storage(), locks);
  const large = { ...record('large'), request: 'x'.repeat((2 << 20) + 1) };
  // The SDK validates shape first; storage independently bounds aggregate bytes.
  await raw.put(recoveryNamespace, 'large-one', large);
  await assert.rejects(raw.put(recoveryNamespace, 'large-two', large), /full/);
});

test('a stale tab cannot replace exact payload or downgrade an accepted receipt', async () => {
  const storage = new Storage(), locks = new Locks(), adapter = browserRecoveryStorage(storage, locks);
  await Promise.all([adapter.put(recoveryNamespace, 'same', record('same', true)), adapter.put(recoveryNamespace, 'same', record('same'))]);
  assert.equal((await adapter.list(recoveryNamespace, 32))[0].accepted, true);
  await assert.rejects(adapter.put(recoveryNamespace, 'same', record('other')), /identity or payload/);
  await adapter.delete(recoveryNamespace, 'same'); assert.equal((await adapter.list(recoveryNamespace, 32)).length, 0);
});

test('missing browser locking fails before persistence or submission', async () => {
  const storage = new Storage(), journal = new RecoveryJournal(browserRecoveryStorage(storage, undefined));
  await assert.rejects(journal.put(record('unavailable')), /Web Locks/); assert.equal(storage.length, 0);
});
