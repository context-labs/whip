/// <reference types="node" />
/** @jest-environment node */
import { DatabaseSync } from 'node:sqlite';
import { readFileSync } from 'node:fs';
import { Client, type Request, type Response, type Operations } from '@whip/sdk';
import { MobileRuntime, type SavedHost } from './runtime';
import { SqliteMobileStorage, type StorageDatabase } from './storage';
import { draftKey } from './address';
jest.mock('expo-crypto', () => ({ randomUUID: () => require('node:crypto').randomUUID(), CryptoDigestAlgorithm: { SHA256: 'sha256' }, digestStringAsync: async (_: string, value: string) => require('node:crypto').createHash('sha256').update(value).digest('hex') }));
const fixtureValues = JSON.parse(readFileSync('../../packages/protocol/schema/fixtures.json', 'utf8'));
const contract = (name: string): any => name === 'Admission' ? { receipt: { identity: { client_id: 'phone', request_id: 'request' }, digest: 'a'.repeat(64), input_id: 'input', deleted_at: null, created_at: '2026-09-28T00:00:00Z' }, input: { id: 'input', session_id: 'child', source: 'user', kind: 'prompt', parts: [{ type: 'text', text: 'private prompt' }], state: 'queued', turn_id: null, goal: null, schedule: null, host_operation: null, created_at: '2026-09-28T00:00:00Z' }, turn: null } : structuredClone(fixtureValues.find((v: { type: string; valid: boolean }) => v.type === name && v.valid).value);
const deferred = <T = void,>() => { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; };
const delay = (ms = 0) => new Promise(resolve => setTimeout(resolve, ms));
async function until(check: () => boolean) { for (let i = 0; i < 100; i++) { if (check()) return; await delay(5); } throw new Error('Condition did not settle'); }
const cleanups: Array<() => Promise<void>> = []; afterEach(async () => { for (const close of cleanups.splice(0)) await close(); jest.restoreAllMocks(); });
const host = (id = 'host'): SavedHost => ({ id, name: id, url: 'https://' + id + '.example', clientId: 'phone', runtimeId: 'runtime' });
const success = (request: Request, result: unknown): Response => ({ jsonrpc: '2.0', id: request.id, result });
async function fixture() {
  const db = new DatabaseSync(':memory:'); const adapter: StorageDatabase = { execAsync: async sql => { db.exec(sql); }, runAsync: async (sql, ...args) => db.prepare(sql).run(...args), getAllAsync: async <T,>(sql: string, ...args: Array<string | number>) => db.prepare(sql).all(...args) as T[], getFirstAsync: async <T,>(sql: string, ...args: Array<string | number>) => (db.prepare(sql).get(...args) ?? null) as T | null, closeAsync: async () => db.close() };
  const storage = new SqliteMobileStorage(adapter); await storage.initialize(true);
  const requests: Request[] = []; let intercept: ((request: Request) => Promise<Response | undefined | void>) | undefined; let opening: Promise<void> | undefined; let boot = 0;
  const admissions = new Map<string, Operations['sessions.submit']['result']>();
  const runtime = new MobileRuntime(storage, async (selected, signal) => {
    const held = opening; opening = undefined; if (held) await held;
    return Client.connect(async (request, _expected, options) => {
      signal.throwIfAborted(); options.signal?.throwIfAborted();
      if (request.method === 'initialize') return success(request, { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot' + ++boot, network_client: true, builtins: [] });
      requests.push(structuredClone(request)); const override = await intercept?.(request); if (override) return override;
      const params = request.params as Record<string, any>; const sessionId = params.session_id ?? 'root';
      if (request.method === 'sessions.submit') {
        const admission = contract('Admission') as Operations['sessions.submit']['result']; admission.input!.id = 'input' + requests.length; admission.input!.session_id = sessionId; admission.receipt.identity = params.identity; admission.receipt.input_id = admission.input!.id; admission.input!.parts = params.parts; admission.turn = null;
        admissions.set(params.identity.request_id, admission); return success(request, admission);
      }
      if (request.method === 'receipts.match' || request.method === 'receipts.get') { const admission = admissions.get(request.method === 'receipts.match' ? JSON.parse(Buffer.from(params.params_base64, 'base64').toString()).identity.request_id : params.identity.request_id); return admission ? success(request, admission) : { jsonrpc: '2.0', id: request.id, error: { code: -32000, kind: 'NOT_FOUND', message: 'missing' } }; }
      const snapshot = { session_id: sessionId, revision: '1', through_sequence: '0', message_count: '0' };
      if (request.method === 'sessions.activity') return success(request, { ...contract('SessionActivity'), session_id: sessionId, active_turn: null, active_input_id: null });
      if (request.method === 'sessions.history_page') return success(request, { snapshot, messages: [], next_cursor: null });
      if (request.method === 'sessions.observe') return success(request, { snapshot, epoch: 'boot', preview: null, messages: [] });
      if (request.method === 'turns.page') return success(request, { items: [], next_cursor: null });
      throw new Error('Unexpected ' + request.method);
    }, { clientID: selected.clientId, signal });
  });
  await runtime.start(false); cleanups.push(() => runtime.dispose());
  const submit = (id = 'request', sessionId = 'child'): Operations['sessions.submit']['params'] => ({ session_id: sessionId, source: 'user' as const, parts: [{ type: 'text' as const, text: 'private message' }], identity: { client_id: 'phone', request_id: id } });
  return { runtime, storage, requests, admissions, submit, intercept(fn?: typeof intercept) { intercept = fn; }, hold(promise: Promise<void>) { opening = promise; } };
}
test('superseded initialization never replaces newer host; caller identity is persisted before connection', async () => {
  const f = await fixture(), held = deferred(); f.hold(held.promise); const first = f.runtime.connect(host('old')); await until(() => f.runtime.getSnapshot().connecting);
  await until(() => f.runtime.getSnapshot().hosts.some(h => h.id === 'old')); expect((await f.storage.get<SavedHost>('hosts', 'old'))?.clientId).toBe('phone');
  await f.runtime.connect(host('new')); held.resolve(); await expect(first).rejects.toThrow(); expect(f.runtime.getSnapshot().host?.id).toBe('new');
});
test('host identity mismatch preserves local drafts and never attaches views', async () => {
  const f = await fixture(); f.runtime.setDraft('draft', 'keep'); await expect(f.runtime.connect({ ...host(), runtimeId: 'different' })).rejects.toThrow('identity'); expect(f.runtime.getSnapshot().ready).toBe(false); expect(f.runtime.draft('draft').text).toBe('keep');
});
test('background connection is cancelled; foreground reconnect uses same caller and no mutation', async () => {
  const f = await fixture(), held = deferred(); f.hold(held.promise); const opening = f.runtime.connect(host()); await until(() => !!f.runtime.getSnapshot().host); f.runtime.setActive(false); held.resolve(); await expect(opening).rejects.toThrow();
  expect(f.runtime.getSnapshot().ready).toBe(false); f.runtime.setActive(true); await until(() => f.runtime.getSnapshot().ready); expect(f.runtime.getSnapshot().client?.clientID).toBe('phone'); expect(f.requests).toEqual([]);
});
test('failed reconciliation can reconnect without creating a new local owner or sending work', async () => {
  const f = await fixture(); jest.spyOn(f.storage.nativeRecovery, 'list').mockRejectedValueOnce(new Error('read failed')); await expect(f.runtime.connect(host())).rejects.toThrow('read failed');
  await f.runtime.reconnect(); expect(f.runtime.getSnapshot().ready).toBe(true); expect(f.requests).toEqual([]);
});
test('failed queued draft persistence prevents submission; acceptance does not clear a newer unsaved edit', async () => {
  const f = await fixture(); await f.runtime.connect(host()); const key = draftKey('runtime', 'root', 'child');
  jest.spyOn(f.storage, 'setDraft').mockRejectedValueOnce(new Error('disk full')); const failed = f.runtime.setDraft(key, 'old');
  await expect(f.runtime.run('sessions.submit', f.submit(), { rootId: 'root', intent: { draftKey: key, draftRevision: failed.revision } })).rejects.toThrow('disk full'); expect(f.requests).toEqual([]);
  const draft = f.runtime.setDraft(key, 'original'); const later = deferred<Response>(); f.intercept(async request => request.method === 'sessions.submit' ? later.promise : undefined);
  const sent = f.runtime.run('sessions.submit', f.submit(), { rootId: 'root', intent: { draftKey: key, draftRevision: draft.revision } }); await until(() => f.requests.length > 0);
  jest.spyOn(f.storage, 'setDraft').mockRejectedValueOnce(new Error('disk full')); f.runtime.setDraft(key, 'newer');
  const admission = contract('Admission'); admission.receipt.identity = { client_id: 'phone', request_id: 'request' }; admission.input.session_id = 'child'; later.resolve(success(f.requests[0], admission)); await sent;
  expect(f.runtime.draft(key).text).toBe('newer'); expect(f.runtime.draftStatus(key)).toBe('failed'); expect((await f.storage.get<{ text: string }>('drafts', key))?.text).toBe('original');
});
test('acceptance clears only its exact draft and unlocks another input without waiting for execution', async () => {
  const f = await fixture(); await f.runtime.connect(host()); const key = draftKey('runtime', 'root', 'child'), draft = f.runtime.setDraft(key, 'text');
  await f.runtime.run('sessions.submit', f.submit(), { rootId: 'root', intent: { draftKey: key, draftRevision: draft.revision } }); expect(f.runtime.draft(key).text).toBe(''); expect(f.runtime.isBlocked('root', 'child')).toBe(false);
  await f.runtime.run('sessions.submit', f.submit('second'), { rootId: 'root' }); expect(f.requests.map(r => r.method)).toEqual(['sessions.submit', 'sessions.submit']);
});
test('lost ACK remains unknown across detach; only explicit exact check resolves it', async () => {
  const f = await fixture(); await f.runtime.connect(host()); let first = true;
  f.intercept(async request => { if (request.method !== 'sessions.submit' || !first) return; first = false; const admission = contract('Admission'); admission.receipt.identity = (request.params as any).identity; admission.input.session_id = 'child'; f.admissions.set('request', admission); throw new DOMException('lost', 'AbortError'); });
  await expect(f.runtime.run('sessions.submit', f.submit(), { rootId: 'root' })).rejects.toThrow('lost'); expect(f.runtime.isBlocked('root', 'child')).toBe(true); await f.runtime.reconnect(); expect(f.requests).toHaveLength(1);
  await f.runtime.checkCommand(f.runtime.getSnapshot().commands[0]); expect(f.runtime.isBlocked('root', 'child')).toBe(false); expect(f.requests.map(r => r.method)).toEqual(['sessions.submit', 'receipts.match']);
});
test('failed acceptance durability stays blocked across rebind until explicit check persists known acceptance', async () => {
  const f = await fixture(); await f.runtime.connect(host()); jest.spyOn(f.storage.nativeRecovery, 'accept').mockRejectedValueOnce(new Error('disk full'));
  await expect(f.runtime.run('sessions.submit', f.submit())).rejects.toThrow('disk full'); await f.runtime.reconnect(); expect(f.runtime.isBlocked('child')).toBe(true);
  await f.runtime.checkCommand(f.runtime.getSnapshot().commands[0]); expect(f.runtime.isBlocked('child')).toBe(false);
});
test('SDK views share one exact recipient owner; stale root release cannot close child replacement', async () => {
  const f = await fixture(); await f.runtime.connect(host()); const root = f.runtime.acquireView('root', 'runtime'), again = f.runtime.acquireView('root', 'runtime'); expect(root.view).toBe(again.view);
  await until(() => root.view.getSnapshot().status === 'live'); const child = f.runtime.acquireView('child', 'runtime'); root.release(); again.release(); await until(() => child.view.getSnapshot().status === 'live'); expect(child.view.getSnapshot().sessionID).toBe('child');
  f.runtime.setActive(false); await until(() => child.view.getSnapshot().status === 'suspended'); const before = f.requests.length; await delay(20); expect(f.requests).toHaveLength(before); child.release();
});
test('manual draft clearing releases count budget and cannot erase a later revision', async () => {
  const f = await fixture(); for (let i = 0; i < 16; i++) f.runtime.setDraft('d' + i, 'text'); await f.storage.flush(); expect(() => f.runtime.setDraft('d16', 'text')).toThrow('draft');
  const old = f.runtime.draft('d0'); f.runtime.setDraft('d0', 'new'); await expect(f.runtime.discardDraft('d0', old.revision)).rejects.toThrow('changed');
  const current = f.runtime.draft('d0'); await f.runtime.discardDraft('d0', current.revision); expect(() => f.runtime.setDraft('d16', 'text')).not.toThrow();
});
test('same-host recovery retains bounded metadata reads while explicit host switch clears them', async () => {
  const f = await fixture(); await f.runtime.connect(host()); f.runtime.query.setQueryData(['runtime', 'session-metadata'], { selected: 'child' });
  await f.runtime.reconnect(); expect(f.runtime.query.getQueryData(['runtime', 'session-metadata'])).toEqual({ selected: 'child' });
  await f.runtime.connect(host('another')); expect(f.runtime.query.getQueryData(['runtime', 'session-metadata'])).toBeUndefined();
});
test('submission previews transfer by exact admission identity and confirmed failure removes only its own preview', async () => {
  const f = await fixture(); await f.runtime.connect(host());
  const accepted = await f.runtime.run('sessions.submit', f.submit('first'), { rootId: 'root' });
  expect(f.runtime.submitted.getSnapshot()[0]).toMatchObject({ id: 'first', inputId: accepted.input!.id, agentId: 'child' });
  f.runtime.submitted.confirm(['first'], 'runtime'); expect(f.runtime.submitted.getSnapshot()[0].confirmed).toBe(true);
  jest.spyOn(f.storage.nativeRecovery, 'put').mockRejectedValueOnce(new Error('disk full')); await expect(f.runtime.run('sessions.submit', f.submit('second'), { rootId: 'root' })).rejects.toThrow(); expect(f.runtime.submitted.getSnapshot().map(item => item.id)).toEqual(['first']);
});
