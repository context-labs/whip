/// <reference types="node" />
/** @jest-environment node */
import { DatabaseSync } from 'node:sqlite';
import type { RecoveryRecord } from '@whip/sdk';
import { SqliteMobileStorage, type StorageDatabase, type Draft } from './storage';

function fixture() {
  const database = new DatabaseSync(':memory:');
  let rejectInsert = false;
  const adapter: StorageDatabase = {
    execAsync: async (sql) => { database.exec(sql); },
    runAsync: async (sql, ...params) => {
      if (rejectInsert && sql.startsWith('INSERT')) throw new Error('disk full');
      return database.prepare(sql).run(...params);
    },
    getAllAsync: async <T>(sql: string, ...params: Array<string | number>) => database.prepare(sql).all(...params) as T[],
    getFirstAsync: async <T>(sql: string, ...params: Array<string | number>) => (database.prepare(sql).get(...params) ?? null) as T | null,
    closeAsync: async () => { database.close(); },
  };
  const storage = new SqliteMobileStorage(adapter);
  return { storage, database, adapter, failWrites: () => { rejectInsert = true; } };
}
const record = (commandId: string): RecoveryRecord => ({
  version: 1, commandId, runtimeId: 'runtime', clientId: 'phone', operation: 'agent.submit', rootId: 'root',
});

test('commits recovery and only whitelisted recipient metadata in one row before returning', async () => {
  const { storage, database } = fixture();
  await storage.initialize(true);
  storage.prepareIntent('command', { agentId: 'child', draftKey: 'child-draft', draftRevision: 'r1', prompt: 'must not persist' } as never);
  await storage.recoveryStorage.put({ ...record('command'), payload: 'must not persist' } as never);
  expect(await storage.listRecovery()).toEqual([{
    record: record('command'), intent: { agentId: 'child', draftKey: 'child-draft', draftRevision: 'r1' }, knownAccepted: false,
  }]);
  expect(database.prepare('SELECT value FROM records').get()?.value).not.toContain('must not persist');
  await storage.markAccepted(record('command'));
  await storage.recoveryStorage.put(record('command'));
  expect((await storage.listRecovery())[0].knownAccepted).toBe(true);
  await storage.recoveryStorage.delete(record('command'));
  expect(await storage.listRecovery()).toEqual([]);
  await storage.close();
});

test('failed persistence leaves neither recovery nor detached intent row, and blocks unprepared recovery', async () => {
  const { storage, failWrites } = fixture();
  await storage.initialize(true);
  await expect(storage.recoveryStorage.put(record('no-intent'))).rejects.toMatchObject({ code: 'corrupt' });
  storage.prepareIntent('failed', { agentId: 'child' });
  failWrites();
  await expect(storage.recoveryStorage.put(record('failed'))).rejects.toThrow('disk full');
  expect(await storage.listRecovery()).toEqual([]);
  await storage.close();
});

test('serialized acceptance does not clear an edited draft even when its text is identical', async () => {
  const { storage } = fixture();
  await storage.initialize(true);
  await storage.setDraft('root', { text: 'root text', revision: 'root-r1' });
  await storage.setDraft('child', { text: 'same text', revision: 'r1' });
  const updated = storage.setDraft('child', { text: 'same text', revision: 'r2' });
  const clearOld = storage.clearDraft('child', 'r1');
  await updated;
  expect(await clearOld).toBe(false);
  expect(await storage.get<Draft>('drafts', 'child')).toEqual({ text: 'same text', revision: 'r2' });
  expect(await storage.get<Draft>('drafts', 'root')).toEqual({ text: 'root text', revision: 'root-r1' });
  expect(await storage.clearDraft('child', 'r2')).toBe(true);
  await storage.close();
});

test('drafts use byte/count budgets and refuse overflow without eviction or destructive replacement', async () => {
  const { storage } = fixture();
  await storage.initialize(true);
  for (let i = 0; i < 16; i++) await storage.setDraft(String(i), { text: 'draft', revision: 'r1' });
  await expect(storage.setDraft('17', { text: 'more', revision: 'r1' })).rejects.toMatchObject({ code: 'quota' });
  await expect(storage.setDraft('0', { text: '👋'.repeat(17_000), revision: 'r2' })).rejects.toMatchObject({ code: 'quota' });
  expect((await storage.list<Draft>('drafts')).length).toBe(16);
  expect((await storage.get<Draft>('drafts', '0'))?.text).toBe('draft');
  for (let i = 0; i < 16; i++) await storage.setDraft(String(i), { text: 'x'.repeat(32_000), revision: 'r2' });
  await expect(storage.setDraft('0', { text: 'x'.repeat(64_000), revision: 'r3' })).rejects.toMatchObject({ code: 'quota' });
  expect((await storage.get<Draft>('drafts', '0'))?.revision).toBe('r2');
  await storage.close();
});

test('recovery count and combined metadata bytes reject admission without dropping unresolved records', async () => {
  const first = fixture();
  await first.storage.initialize(true);
  for (let i = 0; i < 64; i++) {
    first.storage.prepareIntent(String(i), { agentId: 'child' });
    await first.storage.recoveryStorage.put(record(String(i)));
  }
  first.storage.prepareIntent('overflow', { agentId: 'child' });
  await expect(first.storage.recoveryStorage.put(record('overflow'))).rejects.toMatchObject({ code: 'quota' });
  expect(await first.storage.recoveryStorage.list()).toHaveLength(64);
  await first.storage.close();
  const second = fixture();
  await second.storage.initialize(true);
  let admitted = 0;
  for (let i = 0; i < 64; i++) {
    second.storage.prepareIntent(String(i), { agentId: 'x'.repeat(1000), draftKey: 'y'.repeat(1000), draftRevision: 'z'.repeat(1000) });
    try { await second.storage.recoveryStorage.put(record(String(i))); admitted++; }
    catch (error) { expect(error).toMatchObject({ code: 'quota' }); break; }
  }
  expect(admitted).toBeGreaterThan(0);
  expect(admitted).toBeLessThan(64);
  expect(await second.storage.recoveryStorage.list()).toHaveLength(admitted);
  await second.storage.close();
});

test('queued writes freeze their arguments and host edits retain stable client IDs', async () => {
  const { storage } = fixture();
  await storage.initialize(true);
  const host = { url: 'https://host.example.ts.net', clientId: 'phone-id' };
  const write = storage.set('hosts', 'host', host);
  host.clientId = 'mutated';
  await write;
  expect(await storage.get('hosts', 'host')).toEqual({ url: host.url, clientId: 'phone-id' });
  await expect(storage.set('hosts', 'host', host)).rejects.toMatchObject({ code: 'corrupt' });
  for (let i = 0; i < 3; i++) await storage.set('hosts', String(i), { clientId: `phone-${i}` });
  await expect(storage.set('hosts', 'extra', {})).rejects.toMatchObject({ code: 'quota' });
  await storage.close();
});

test('never initializes over unknown or newer schemas and retains unreadable recovery', async () => {
  const { storage, database } = fixture();
  database.exec('CREATE TABLE valuable (data TEXT); INSERT INTO valuable VALUES (\'keep\')');
  await expect(storage.initialize(true)).rejects.toMatchObject({ code: 'schema' });
  expect(database.prepare('SELECT data FROM valuable').get()?.data).toBe('keep');
  database.exec('PRAGMA user_version = 3');
  await expect(storage.initialize(true)).rejects.toMatchObject({ code: 'schema' });
  await storage.close();
  const other = fixture();
  await other.storage.initialize(true);
  other.database.prepare('INSERT INTO records VALUES (?, ?, ?)').run('recovery', 'bad', '{broken');
  await expect(other.storage.listRecovery()).rejects.toMatchObject({ code: 'corrupt' });
  expect(other.database.prepare('SELECT COUNT(*) AS count FROM records').get()?.count).toBe(1);
  await other.storage.close();
});

test('flush and close wait for queued writes; closed storage admits nothing', async () => {
  const { storage } = fixture();
  await storage.initialize(true);
  const writes = Array.from({ length: 10 }, (_, i) => storage.set('settings', String(i), i));
  await storage.flush();
  expect(await storage.list('settings')).toHaveLength(10);
  await Promise.all(writes);
  await storage.close();
  await expect(storage.get('settings', '1')).rejects.toMatchObject({ code: 'closed' });
});


test('permission decisions and runtime commands share limits but expose separate status namespaces', async () => {
  const { storage } = fixture();
  await storage.initialize(true);
  const decision = { ...record('decision'), operation: 'permission.decide' as const };
  await storage.putDecision(decision, { requestId: 'permission-1', agentId: 'child' });
  expect(await storage.listRecovery()).toEqual([]);
  expect(await storage.recoveryStorage.list()).toEqual([]);
  expect(await storage.listDecisions()).toEqual([{ record: decision, intent: { requestId: 'permission-1', agentId: 'child' }, knownAccepted: false }]);
  await storage.markDecisionAccepted(decision);
  expect((await storage.listDecisions())[0].knownAccepted).toBe(true);
  await expect(storage.recoveryStorage.delete(record('decision'))).rejects.toMatchObject({ code: 'corrupt' });
  await expect(storage.markAccepted(record('decision'))).rejects.toMatchObject({ code: 'corrupt' });
  for (let i = 0; i < 63; i++) {
    storage.prepareIntent(String(i), { agentId: 'child' });
    await storage.recoveryStorage.put(record(String(i)));
  }
  await expect(storage.putDecision({ ...decision, commandId: 'overflow' }, { requestId: 'permission-2' })).rejects.toMatchObject({ code: 'quota' });
  expect(await storage.listRecovery()).toHaveLength(63);
  expect(await storage.listDecisions()).toHaveLength(1);
  await storage.deleteDecision(decision);
  expect(await storage.listDecisions()).toEqual([]);
  expect(await storage.listRecovery()).toHaveLength(63);
  await storage.close();
});


test('bookmarks evict least recently saved hints atomically without changing the public value shape', async () => {
  const { storage } = fixture(); await storage.initialize(true);
  const value = (messageId: string) => ({ messageId, revision: 'history', offset: 20, follow: false });
  for (let index = 0; index < 64; index++) await storage.set('bookmarks', String(index), value(String(index)));
  await storage.set('bookmarks', '0', value('0-updated'));
  await storage.set('bookmarks', '64', value('64'));
  expect(await storage.get('bookmarks', '1')).toBeUndefined();
  expect(await storage.get('bookmarks', '0')).toEqual(value('0-updated'));
  expect(await storage.list('bookmarks')).toHaveLength(64);
  expect((await storage.list('bookmarks'))[0].value).not.toHaveProperty('__whipBookmark');
  await storage.close();
});

test('bookmark byte retention and rollback preserve other storage and existing hints on failed replacement', async () => {
  const { storage, failWrites, database } = fixture(); await storage.initialize(true);
  await storage.setDraft('draft', { text: 'keep this work', revision: 'r1' });
  for (let index = 0; index < 40; index++) await storage.set('bookmarks', String(index), { messageId: 'x'.repeat(2000), revision: 'r', offset: 0, follow: false });
  const count = (await storage.list('bookmarks')).length;
  expect(count).toBeLessThan(40);
  expect(database.prepare("SELECT SUM(length(CAST(key AS BLOB)) + length(CAST(value AS BLOB))) AS bytes FROM records WHERE bucket = 'bookmarks'").get()?.bytes).toBeLessThanOrEqual(64 * 1024);
  const before = await storage.list('bookmarks');
  failWrites();
  await expect(storage.set('bookmarks', 'next', { messageId: 'y'.repeat(3000), revision: 'r', offset: 0, follow: false })).rejects.toThrow('disk full');
  expect(await storage.list('bookmarks')).toEqual(before);
  expect(await storage.get('drafts', 'draft')).toEqual({ text: 'keep this work', revision: 'r1' });
  await storage.close();
});

test('legacy plain bookmark values remain readable and are evicted before recently saved hints', async () => {
  const { storage, database } = fixture(); await storage.initialize(true);
  const legacy = { messageId: 'old', revision: 'r', offset: 10, follow: false };
  database.prepare('INSERT INTO records VALUES (?, ?, ?)').run('bookmarks', 'legacy', JSON.stringify(legacy));
  expect(await storage.get('bookmarks', 'legacy')).toEqual(legacy);
  for (let index = 0; index < 64; index++) await storage.set('bookmarks', String(index), { ...legacy, messageId: String(index) });
  expect(await storage.get('bookmarks', 'legacy')).toBeUndefined();
  await storage.close();
});

test('v1 migrates without dropping drafts and appearance replacement stays atomic', async () => {
  const f = fixture(); await f.storage.initialize(true);
  await f.storage.setDraft('draft', { revision: '1', text: 'Keep my work' });
  f.database.exec('PRAGMA user_version = 1');
  await f.storage.initialize(false);
  expect(f.database.prepare('PRAGMA user_version').get()?.user_version).toBe(2);
  expect(await f.storage.get('drafts', 'draft')).toEqual({ revision: '1', text: 'Keep my work' });
  await f.storage.set('themes', 'appearance', { themes: ['original'], appearance: 'original' });
  f.failWrites();
  await expect(f.storage.set('themes', 'appearance', { themes: [], appearance: 'new' })).rejects.toThrow();
  expect(await f.storage.get('themes', 'appearance')).toEqual({ themes: ['original'], appearance: 'original' });
  await f.storage.close();
});
