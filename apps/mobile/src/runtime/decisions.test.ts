/** @jest-environment node */
import { RemoteError } from '@whip/sdk';
import { DecisionStore } from './decisions';
import type { MobileRuntime } from './runtime';
import type { StoredMetadata } from './recovery-metadata';
jest.mock('expo-crypto', () => ({ CryptoDigestAlgorithm: { SHA256: 'sha256' }, digestStringAsync: async (_: string, value: string) => require('node:crypto').createHash('sha256').update(value).digest('hex') }));
const deferred = <T,>() => { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; };
const saved = (knownAccepted = false): StoredMetadata => ({ record: { version: 4, commandId: 'operation', runtimeId: 'runtime', clientId: 'phone', operation: 'permissions.resolve', requestHash: 'a'.repeat(64), rootId: 'root', sessionId: 'child' }, intent: {}, knownAccepted });
function fixture(initial: StoredMetadata[] = []) {
  const records = new Map(initial.map(value => [value.record.commandId, structuredClone(value)]));
  const get = jest.fn(async (_id: string, _options?: unknown) => ({ id: 'operation', session_id: 'child', state: 'waiting' }));
  const client = { runtimeID: 'runtime', clientID: 'phone', call: jest.fn(async () => ({})), session: jest.fn(() => ({ operations: { get }, questions: { get } })) };
  const storage = { list: jest.fn(async () => structuredClone([...records.values()])), put: jest.fn(async (value: StoredMetadata) => { records.set(value.record.commandId, structuredClone(value)); }), accept: jest.fn(async () => { records.get('operation')!.knownAccepted = true; }), delete: jest.fn(async () => { records.delete('operation'); }) };
  let state = { client, active: true, ready: true };
  const runtime = { getSnapshot: () => state, requireReady() { if (!state.active || !state.ready) throw new Error('Not ready'); return state.client; }, storage: { nativeRecovery: storage }, query: { invalidateQueries: async () => {} } };
  const store = new DecisionStore(runtime as unknown as MobileRuntime);
  return { store, client, get, storage, records, replace() { store.reset(); state = { ...state, client: { ...client, runtimeID: 'another' } }; } };
}
test('decision persistence failure sends no authority and makes no success claim', async () => {
  const f = fixture(); await f.store.reconcile(); f.storage.put.mockRejectedValueOnce(new Error('disk full'));
  await expect(f.store.decide('root', 'operation', 'child', true)).rejects.toThrow('disk full'); expect(f.client.call).not.toHaveBeenCalled(); expect(f.store.forRequest('operation')).toBeUndefined();
});
test('captured permission is scoped and revalidated after local persistence', async () => {
  const f = fixture(); await f.store.reconcile(); const validate = jest.fn().mockImplementationOnce(() => {}).mockImplementationOnce(() => { throw new Error('Changed host'); });
  await expect(f.store.decide('root', 'operation', 'child', true, validate)).rejects.toThrow('Changed host'); expect(f.client.session).toHaveBeenCalledWith('child'); expect(f.client.call).not.toHaveBeenCalled();
  await f.store.check('operation'); expect(f.store.forRequest('operation')?.status).toBe('pending'); expect(f.store.isBlocked('operation')).toBe(true);
});
test('local acceptance survives failed durability and missing read without a replay', async () => {
  const f = fixture(); await f.store.reconcile(); f.storage.accept.mockRejectedValueOnce(new Error('disk full'));
  await expect(f.store.decide('root', 'operation', 'child', false)).rejects.toThrow('disk full'); expect(f.store.forRequest('operation')).toMatchObject({ knownAccepted: true, status: 'unknown' });
  f.get.mockRejectedValueOnce(new RemoteError({ code: -32000, kind: 'NOT_FOUND', message: 'gone' })); await f.store.check('operation'); await f.store.reconcile(); expect(f.store.forRequest('operation')?.knownAccepted).toBe(true); expect(f.client.call).toHaveBeenCalledTimes(1);
});
test('resolved elsewhere is distinct from delivery and reconnection never sends a decision', async () => {
  const f = fixture([saved()]); await f.store.reconcile(); expect(f.client.call).not.toHaveBeenCalled(); f.get.mockResolvedValueOnce({ id: 'operation', session_id: 'child', state: 'succeeded' });
  await f.store.check('operation'); expect(f.store.forRequest('operation')).toMatchObject({ status: 'resolved', knownAccepted: false, message: expect.stringContaining('Another client') });
});
test('parallel checks admit one read; reset cancels and ignores late read/clear', async () => {
  const f = fixture([saved()]); await f.store.reconcile(); const later = deferred<{ id: string; session_id: string; state: string }>(); f.get.mockReturnValueOnce(later.promise);
  const one = f.store.check('operation'); await f.store.check('operation'); expect(f.get).toHaveBeenCalledTimes(1); const signal = (f.get.mock.calls[0][1] as { signal: AbortSignal }).signal;
  f.replace(); later.resolve({ id: 'operation', session_id: 'child', state: 'succeeded' }); await one; expect(signal.aborted).toBe(true); expect(f.store.getSnapshot().items).toEqual([]);
  const g = fixture([saved()]); await g.store.reconcile(); const clear = deferred<void>(); g.storage.delete.mockReturnValueOnce(clear.promise); const pending = g.store.clear('operation'); g.replace(); clear.resolve(); await pending; expect(g.store.getSnapshot()).toMatchObject({ items: [], ready: false });
});
test('busy decision prevents a concurrent opposite decision before hashing or storage', async () => {
  const f = fixture(); await f.store.reconcile(); const later = deferred<{}>(); f.client.call.mockReturnValueOnce(later.promise);
  const first = f.store.decide('root', 'operation', 'child', true);
  await expect(f.store.decide('root', 'operation', 'child', false)).rejects.toThrow('previous decision');
  for (let i = 0; i < 20 && !f.client.call.mock.calls.length; i++) await Promise.resolve(); later.resolve({}); await first;
  expect(f.client.call).toHaveBeenCalledTimes(1); expect(f.store.forRequest('operation')).toMatchObject({ status: 'delivered', knownAccepted: true });
});
