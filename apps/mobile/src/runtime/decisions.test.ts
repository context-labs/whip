/** @jest-environment node */
import { RpcError } from '@whip/sdk';
import { DecisionStore } from './decisions';
import type { MobileRuntime } from './runtime';
import type { PermissionRecoveryRecord, StoredRecovery } from './storage';

const deferred = <T,>() => { let resolve!: (value: T) => void; let reject!: (error: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const missing = () => new RpcError({ code: -32000, message: 'Missing', data: { kind: 'command_not_found' } });
const saved = (id = 'decision', knownAccepted = false): StoredRecovery<PermissionRecoveryRecord> => ({ record: { version: 1, commandId: id, runtimeId: 'runtime', clientId: 'client', operation: 'permission.decide', rootId: 'root' }, intent: { requestId: 'request', agentId: 'agent' }, knownAccepted });
const outcome = (id = 'decision') => ({ operation: 'permission.decide', command_id: id, ingress_seq: '1', status: 'succeeded', result: { operation_id: 'operation', lease_id: 'lease' } });
function fixture(initial: Array<StoredRecovery<PermissionRecoveryRecord>> = []) {
  let sequence = 0;
  const records = new Map(initial.map(item => [item.record.commandId, structuredClone(item)]));
  const client = { clientId: 'client', lifetimeSignal: new AbortController().signal,
    createId: () => `new-${++sequence}`, supports: () => true,
    getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'runtime', connection_id: 'connection' } }),
    requireConnected: () => ({ runtime_id: 'runtime' }),
    permissions: { decide: jest.fn(async () => ({})), status: jest.fn(async (id: string, _options?: { signal: AbortSignal; timeoutMs?: number }) => outcome(id)) },
  };
  let state = { client, host: { runtimeId: 'runtime' }, active: true, ready: true };
  const storage = {
    listDecisions: jest.fn(async () => [...records.values()].map(item => structuredClone(item))),
    putDecision: jest.fn(async (record: PermissionRecoveryRecord, intent: unknown) => { records.set(record.commandId, { record, intent, knownAccepted: false } as StoredRecovery<PermissionRecoveryRecord>); }),
    markDecisionAccepted: jest.fn(async (record: PermissionRecoveryRecord) => { const item = records.get(record.commandId); if (!item) throw new Error('missing durable record'); item.knownAccepted = true; }),
    deleteDecision: jest.fn(async (record: PermissionRecoveryRecord) => { records.delete(record.commandId); }),
  };
  const runtime = { getSnapshot: () => state, requireReady: () => { if (!state.ready || !state.active) throw new Error('Not ready'); return state.client; }, storage, query: { invalidateQueries: jest.fn(async () => {}) } };
  const store = new DecisionStore(runtime as unknown as MobileRuntime);
  return { store, client, storage, records, replaceHost: () => { store.reset(); state = { ...state, client: { ...client, clientId: 'new-client' }, host: { runtimeId: 'new-runtime' } }; }, setActive: (active: boolean) => { state = { ...state, active }; } };
}

test('decision storage failure sends nothing and is visible without inventing an accepted result', async () => {
  const f = fixture(); await f.store.reconcile();
  f.storage.putDecision.mockRejectedValueOnce(new Error('disk full'));
  await expect(f.store.decide('root', 'request', 'agent', true)).rejects.toThrow('disk full');
  expect(f.client.permissions.decide).not.toHaveBeenCalled();
  expect(f.client.permissions.status).not.toHaveBeenCalled();
  expect(f.store.forRequest('request')).toMatchObject({ status: 'failed', knownAccepted: false, message: expect.stringContaining('not sent') });
  expect(f.store.isBlocked('request')).toBe(true);
  await f.store.clear('new-1');
  expect(f.store.isBlocked('request')).toBe(false);
});

test('a request is revalidated after durable storage and never automatically resent', async () => {
  const f = fixture(); await f.store.reconcile();
  const validate = jest.fn().mockImplementationOnce(() => {}).mockImplementationOnce(() => { throw new Error('Answered elsewhere'); });
  await expect(f.store.decide('root', 'request', 'agent', true, validate)).rejects.toThrow('Answered elsewhere');
  expect(f.records.size).toBe(1);
  expect(f.client.permissions.decide).not.toHaveBeenCalled();
  f.client.permissions.status.mockRejectedValue(missing());
  await f.store.check('new-1');
  await f.store.reconcile();
  expect(f.store.forRequest('request')?.status).toBe('not_found');
  expect(f.client.permissions.decide).not.toHaveBeenCalled();
  expect(f.store.isBlocked('request')).toBe(true);
});

test('accepted knowledge survives a failed disk update and a subsequent missing lookup', async () => {
  const f = fixture(); await f.store.reconcile();
  f.storage.markDecisionAccepted.mockRejectedValue(new Error('disk full'));
  await expect(f.store.decide('root', 'request', 'agent', true)).rejects.toThrow('disk full');
  expect(f.store.forRequest('request')).toMatchObject({ knownAccepted: true, status: 'checking' });
  f.client.permissions.status.mockRejectedValue(missing());
  await f.store.check('new-1'); await f.store.reconcile();
  expect(f.store.forRequest('request')).toMatchObject({ knownAccepted: true, status: 'checking' });
  await expect(f.store.clear('new-1')).rejects.toThrow('Check the decision');
  expect(f.client.permissions.decide).toHaveBeenCalledTimes(1);
});

test('parallel status checks coalesce, and reset cancels/ignores late lookup and clear callbacks', async () => {
  const f = fixture([saved()]); await f.store.reconcile();
  const later = deferred<ReturnType<typeof outcome>>();
  f.client.permissions.status.mockReturnValueOnce(later.promise);
  const first = f.store.check('decision'); const second = f.store.check('decision');
  expect(first).toBe(second);
  const signal = f.client.permissions.status.mock.calls.at(-1)![1] as { signal: AbortSignal };
  f.replaceHost(); later.resolve(outcome()); await first;
  expect(signal.signal.aborted).toBe(true);
  expect(f.store.getSnapshot().items).toEqual([]);
  const g = fixture([saved()]); await g.store.reconcile();
  const cleared = deferred<void>(); g.storage.deleteDecision.mockReturnValueOnce(cleared.promise);
  const clear = g.store.clear('decision'); g.replaceHost(); cleared.resolve(); await clear;
  expect(g.store.getSnapshot()).toMatchObject({ identity: '', items: [], ready: false });
});

test('reconciliation coalesces, blocks admission while loading, and preserves an in-flight decision', async () => {
  const f = fixture(); await f.store.reconcile();
  const sent = deferred<object>(); f.client.permissions.decide.mockReturnValueOnce(sent.promise);
  const decision = f.store.decide('root', 'request', 'agent', true);
  await Promise.resolve(); await Promise.resolve();
  const list = deferred<Array<StoredRecovery<PermissionRecoveryRecord>>>(); f.storage.listDecisions.mockReturnValueOnce(list.promise);
  const first = f.store.reconcile(); const second = f.store.reconcile();
  expect(first).toBe(second);
  expect(f.store.isBlocked('another-request')).toBe(true);
  list.resolve([]); await first;
  expect(f.store.forRequest('request')?.status).toBe('sending');
  expect(f.client.permissions.status).not.toHaveBeenCalled();
  sent.resolve({}); await decision;
  expect(f.store.forRequest('request')).toMatchObject({ status: 'succeeded', knownAccepted: true });
});

test('missing correlation blocks its root and inactive observation performs no lookup', async () => {
  const record = saved(); delete record.intent;
  const f = fixture([record]); await f.store.reconcile();
  expect(f.store.isBlocked('unrelated', 'root')).toBe(true);
  f.setActive(false); const before = f.client.permissions.status.mock.calls.length;
  await f.store.check('decision'); await f.store.reconcile();
  expect(f.client.permissions.status).toHaveBeenCalledTimes(before);
  expect(f.store.getSnapshot().ready).toBe(false);
});
