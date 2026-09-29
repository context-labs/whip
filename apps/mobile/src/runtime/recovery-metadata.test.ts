/// <reference types="node" />
/** @jest-environment node */
import { createHash } from 'node:crypto';
import { DatabaseSync } from 'node:sqlite';
import { Client } from '@whip/sdk';
import { metadataKey, projectRecovery, validateMetadata, type StoredMetadata } from './recovery-metadata';
import { SqliteMobileStorage, type StorageDatabase } from './storage';

const hash = async (text: string) => createHash('sha256').update(text).digest('hex');
async function command(text = 'private original input', requestID = 'request') {
  const client = await Client.connect(async request => ({ jsonrpc: '2.0', id: request.id, result: { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot', network_client: true, builtins: [] } }), { clientID: 'phone' });
  return client.session('child').submission([{ type: 'text', text }], requestID);
}
function fixture() {
  const database = new DatabaseSync(':memory:');
  let failure = false;
  const adapter: StorageDatabase = {
    execAsync: async sql => { database.exec(sql); },
    runAsync: async (sql, ...params) => { if (failure && sql.startsWith('INSERT')) throw new Error('disk full'); return database.prepare(sql).run(...params); },
    getAllAsync: async <T>(sql: string, ...params: Array<string | number>) => database.prepare(sql).all(...params) as T[],
    getFirstAsync: async <T>(sql: string, ...params: Array<string | number>) => (database.prepare(sql).get(...params) ?? null) as T | null,
    closeAsync: async () => { database.close(); },
  };
  const storage = new SqliteMobileStorage(adapter);
  return { database, storage, adapter, fail: () => { failure = true; } };
}

test('native command projects only exact identities and a local fingerprint; no request body is persisted', async () => {
  const f = fixture(); await f.storage.initialize(true);
  const prepared = await command();
  const value = await projectRecovery(prepared.record, hash, { rootId: 'root', intent: { draftKey: 'draft', draftRevision: 'revision' } });
  await f.storage.nativeRecovery.put(value);
  expect(value).toEqual({ record: { version: 4, runtimeId: 'runtime', clientId: 'phone', commandId: 'request', operation: 'sessions.submit', requestHash: await hash(prepared.record.request), rootId: 'root', sessionId: 'child' }, intent: { draftKey: 'draft', draftRevision: 'revision' }, knownAccepted: false });
  const row = f.database.prepare('SELECT value FROM records').get()!;
  expect(row.value).not.toContain('private original input');
  expect(row.value).not.toContain('parts');
  expect(await f.storage.nativeRecovery.list()).toEqual([value]);
  expect(await f.storage.listRecovery()).toEqual([]);
  await f.storage.close();
});

test('same identity retries are stable and cannot change bytes, recipient, intent or known acceptance', async () => {
  const f = fixture(); await f.storage.initialize(true);
  const value = await projectRecovery((await command()).record, hash, { rootId: 'root' });
  await f.storage.nativeRecovery.put(value);
  await f.storage.nativeRecovery.accept(value.record);
  const reordered = { ...value, record: Object.fromEntries(Object.entries(value.record).reverse()) } as StoredMetadata;
  await f.storage.nativeRecovery.put(reordered);
  expect((await f.storage.nativeRecovery.list())[0].knownAccepted).toBe(true);
  const changed = await projectRecovery((await command('different input')).record, hash, { rootId: 'root' });
  for (const conflict of [changed, { ...value, record: { ...value.record, sessionId: 'another' } }, { ...value, intent: { draftKey: 'other' } }])
    await expect(f.storage.nativeRecovery.put(conflict)).rejects.toMatchObject({ code: 'corrupt' });
  await expect(f.storage.nativeRecovery.delete(changed.record)).rejects.toMatchObject({ code: 'corrupt' });
  expect(await f.storage.nativeRecovery.list()).toEqual([{ ...value, knownAccepted: true }]);
  await f.storage.nativeRecovery.delete(value.record); expect(await f.storage.nativeRecovery.list()).toEqual([]);
  await f.storage.close();
});

test('schema upgrade preserves retired recovery and drafts, with no interpretation as v4', async () => {
  const f = fixture(); await f.storage.initialize(true);
  const old = { version: 1 as const, runtimeId: 'old-runtime', clientId: 'phone', commandId: 'old', operation: 'submit' as const, rootId: 'old-root' };
  f.storage.prepareIntent(old.commandId, {}); await f.storage.recoveryStorage.put(old);
  await f.storage.setDraft('old-draft', { text: 'keep this draft', revision: 'r1' });
  f.database.exec('PRAGMA user_version = 2');
  const rows = f.database.prepare('SELECT * FROM records ORDER BY bucket,key').all();
  await f.storage.initialize(false);
  expect(f.database.prepare('PRAGMA user_version').get()?.user_version).toBe(3);
  expect(f.database.prepare('SELECT * FROM records ORDER BY bucket,key').all()).toEqual(rows);
  expect(await f.storage.nativeRecovery.list()).toEqual([]);
  expect((await f.storage.listRecovery())[0].record).toEqual(old);
  await f.storage.close();
});

test('native and retired metadata share the original total 64-record ceiling and never evict unresolved records', async () => {
  const f = fixture(); await f.storage.initialize(true);
  const value = await projectRecovery((await command()).record, hash);
  for (let i = 0; i < 63; i++) await f.storage.nativeRecovery.put({ ...value, record: { ...value.record, commandId: String(i) } });
  f.storage.prepareIntent('old', {});
  await f.storage.recoveryStorage.put({ version: 1, runtimeId: 'old', clientId: 'phone', commandId: 'old', operation: 'submit' });
  await expect(f.storage.nativeRecovery.put(value)).rejects.toMatchObject({ code: 'quota' });
  expect(await f.storage.nativeRecovery.list()).toHaveLength(63); expect(await f.storage.listRecovery()).toHaveLength(1);
  await f.storage.close();
});

test('failed metadata persistence is atomic, and unknown fields cannot leak bodies into storage', async () => {
  const f = fixture(); await f.storage.initialize(true);
  const value = await projectRecovery((await command()).record, hash);
  expect(() => f.storage.nativeRecovery.put({ ...value, record: { ...value.record, body: 'secret' } } as never)).toThrow();
  expect(() => validateMetadata({ ...value, intent: { prompt: 'secret' } } as never)).toThrow();
  f.fail(); await expect(f.storage.nativeRecovery.put(value)).rejects.toThrow('disk full');
  expect(await f.storage.nativeRecovery.list()).toEqual([]);
  await f.storage.close();
});

test('stored unknown data and key mismatches fail closed without deleting evidence', async () => {
  const f = fixture(); await f.storage.initialize(true);
  const value = await projectRecovery((await command()).record, hash);
  const raw = JSON.stringify({ ...value, request: 'untrusted body' });
  f.database.prepare("INSERT INTO records VALUES ('recovery-v4', ?, ?)").run(metadataKey(value.record), raw);
  await expect(f.storage.nativeRecovery.list()).rejects.toThrow();
  expect(f.database.prepare('SELECT value FROM records').get()?.value).toBe(raw);
  f.database.prepare('UPDATE records SET value=?, key=?').run(JSON.stringify(value), 'wrong-key');
  await expect(f.storage.nativeRecovery.list()).rejects.toMatchObject({ code: 'corrupt' });
  await f.storage.close();
});
