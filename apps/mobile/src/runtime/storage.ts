import type { RecoveryRecord, RecoveryStorage } from '@whip/sdk';

export type StorageBucket = 'hosts' | 'settings' | 'drafts' | 'bookmarks' | 'themes';
export interface Draft { text: string; revision: string }
export interface CommandIntent {
  agentId?: string;
  requestId?: string;
  draftKey?: string;
  draftRevision?: string;
  workflowId?: string;
  step?: string;
}
export type PermissionRecoveryRecord = Omit<RecoveryRecord, 'operation'> & { operation: 'permission.decide' };
type DurableRecord = RecoveryRecord | PermissionRecoveryRecord;
export interface StoredRecovery<R extends DurableRecord = RecoveryRecord> {
  record: R;
  intent?: CommandIntent;
  knownAccepted: boolean;
}
export interface MobileStorage {
  get<T>(bucket: StorageBucket, key: string): Promise<T | undefined>;
  set(bucket: StorageBucket, key: string, value: unknown): Promise<void>;
  delete(bucket: StorageBucket, key: string): Promise<void>;
  list<T>(bucket: StorageBucket): Promise<Array<{ key: string; value: T }>>;
  setDraft(key: string, draft: Draft): Promise<void>;
  clearDraft(key: string, expectedRevision: string): Promise<boolean>;
  prepareIntent(commandId: string, intent: CommandIntent): void;
  discardIntent(commandId: string): void;
  recoveryStorage: RecoveryStorage;
  listRecovery(): Promise<StoredRecovery[]>;
  markAccepted(record: DurableRecord): Promise<void>;
  putDecision(record: PermissionRecoveryRecord, intent: CommandIntent): Promise<void>;
  listDecisions(): Promise<Array<StoredRecovery<PermissionRecoveryRecord>>>;
  markDecisionAccepted(record: PermissionRecoveryRecord): Promise<void>;
  deleteDecision(record: PermissionRecoveryRecord): Promise<void>;
  flush(): Promise<void>;
  close(): Promise<void>;
}
export class StorageError extends Error {
  constructor(readonly code: 'unavailable' | 'quota' | 'corrupt' | 'key_missing' | 'database_missing' | 'schema' | 'closed' | 'reset_pending', message: string) {
    super(message);
    this.name = 'StorageError';
  }
}

// The private connection is never handed to UI code. All reads and transactions
// share one queue. Expo's exclusive transaction helper opens an unkeyed second
// connection, which cannot read a SQLCipher database.
export interface StorageDatabase {
  execAsync(sql: string): Promise<void>;
  runAsync(sql: string, ...params: Array<string | number>): Promise<unknown>;
  getAllAsync<T>(sql: string, ...params: Array<string | number>): Promise<T[]>;
  getFirstAsync<T>(sql: string, ...params: Array<string | number>): Promise<T | null>;
  closeAsync(): Promise<void>;
}
type Bucket = StorageBucket | 'recovery';
type Row = { key: string; value: string };
type BookmarkEnvelope = { __whipBookmark: 1; savedAt: number; value: unknown };
function bookmark(value: unknown): { savedAt: number; value: unknown } {
  if (value && typeof value === 'object' && (value as BookmarkEnvelope).__whipBookmark === 1) {
    const envelope = value as BookmarkEnvelope;
    if (!Number.isSafeInteger(envelope.savedAt) || envelope.savedAt < 0 || !('value' in envelope)) throw new StorageError('corrupt', 'Unreadable saved reading position');
    return envelope;
  }
  return { savedAt: 0, value }; // Existing plain bookmarks predate retention metadata.
}
const limits: Record<Bucket, { count: number; bytes: number; entry: number }> = {
  themes: { count: 1, bytes: 256 * 1024, entry: 256 * 1024 },
  hosts: { count: 4, bytes: 16 * 1024, entry: 4 * 1024 },
  settings: { count: 32, bytes: 16 * 1024, entry: 4 * 1024 },
  drafts: { count: 16, bytes: 512 * 1024, entry: 64 * 1024 },
  bookmarks: { count: 64, bytes: 64 * 1024, entry: 4 * 1024 },
  recovery: { count: 64, bytes: 64 * 1024, entry: 64 * 1024 },
};
const schema = `CREATE TABLE records (
  bucket TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL,
  PRIMARY KEY (bucket, key)
) WITHOUT ROWID;
PRAGMA user_version = 2;`;
const bytes = (value: string) => new TextEncoder().encode(value).byteLength;
const recoveryKey = (record: DurableRecord) => JSON.stringify([record.runtimeId, record.clientId, record.commandId]);
function identifier(value: unknown, label: string): asserts value is string {
  if (typeof value !== 'string' || !value || bytes(value) > 1024) throw new StorageError('corrupt', `Invalid ${label}`);
}
function encode(value: unknown): string {
  let encoded: string | undefined;
  try { encoded = JSON.stringify(value); } catch { /* Report without including private text. */ }
  if (encoded === undefined) throw new StorageError('corrupt', 'Storage value must be JSON');
  return encoded;
}
function decode<T>(value: string): T {
  try { return JSON.parse(value) as T; }
  catch { throw new StorageError('corrupt', 'Stored data could not be read; it has been preserved'); }
}
function cleanIntent(intent: CommandIntent): CommandIntent {
  if (!intent || typeof intent !== 'object' || Array.isArray(intent)) throw new StorageError('corrupt', 'Unreadable command recipient metadata');
  const clean: CommandIntent = {};
  for (const field of ['agentId', 'requestId', 'draftKey', 'draftRevision', 'workflowId', 'step'] as const) {
    if (intent[field] !== undefined) { identifier(intent[field], field); clean[field] = intent[field]; }
  }
  return clean;
}
function cleanRecord<R extends DurableRecord>(record: R): R {
  if (record.version !== 1) throw new StorageError('corrupt', 'Unsupported recovery record version');
  for (const field of ['runtimeId', 'clientId', 'commandId', 'operation'] as const) identifier(record[field], field);
  if (record.rootId !== undefined) identifier(record.rootId, 'rootId');
  return {
    version: 1, runtimeId: record.runtimeId, clientId: record.clientId,
    commandId: record.commandId, operation: record.operation,
    ...(record.rootId === undefined ? {} : { rootId: record.rootId }),
  } as R;
}

export class SqliteMobileStorage implements MobileStorage {
  private tail: Promise<unknown> = Promise.resolve();
  private closing?: Promise<void>;
  private failure?: StorageError;
  private intents = new Map<string, CommandIntent>();
  readonly recoveryStorage: RecoveryStorage = {
    list: async () => (await this.listRecovery()).map(({ record }) => record),
    put: (record) => this.putRecovery(record),
    delete: (record) => this.deleteRecovery(record),
  };
  constructor(private readonly db: StorageDatabase) {}

  /** Called after SQLCipher verification; a version mismatch never wipes data. */
  async initialize(allowCreate: boolean): Promise<void> {
    await this.enqueue(async () => {
      const version = await this.db.getFirstAsync<{ user_version: number }>('PRAGMA user_version');
      if (version?.user_version === 0 && allowCreate) {
        await this.transaction(async () => {
          const tables = await this.db.getAllAsync<{ name: string }>("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'");
          if (tables.length) throw new StorageError('schema', 'Unrecognized database schema; existing data has been preserved');
          await this.db.execAsync(schema);
        });
      } else if (version?.user_version === 1) {
        await this.transaction(() => this.db.execAsync('PRAGMA user_version = 2'));
      } else if (version?.user_version !== 2) {
        throw new StorageError('schema', 'This app cannot read the saved database version; existing data has been preserved');
      }
      const unknown = await this.db.getFirstAsync<{ bucket: string }>("SELECT bucket FROM records WHERE bucket NOT IN ('hosts', 'settings', 'drafts', 'bookmarks', 'recovery', 'themes') LIMIT 1");
      if (unknown) throw new StorageError('schema', 'Unrecognized saved data; existing records have been preserved');
      for (const bucket of Object.keys(limits) as Bucket[]) await this.rows(bucket);
    });
  }
  private enqueue<T>(task: () => Promise<T>): Promise<T> {
    if (this.closing) return Promise.reject(new StorageError('closed', 'Storage is closed'));
    const result = this.tail.then(() => {
      if (this.failure) throw this.failure;
      return task();
    });
    this.tail = result.catch(() => {});
    return result;
  }
  private async transaction<T>(task: () => Promise<T>): Promise<T> {
    await this.db.execAsync('BEGIN IMMEDIATE');
    try {
      const result = await task();
      await this.db.execAsync('COMMIT');
      return result;
    } catch (error) {
      try { await this.db.execAsync('ROLLBACK'); }
      catch { this.failure = new StorageError('unavailable', 'Storage transaction failed; reopen the app before sending'); }
      throw error;
    }
  }
  private async rows(bucket: Bucket): Promise<Row[]> {
    const size = await this.db.getFirstAsync<{ count: number; bytes: number; entry: number }>(
      `SELECT COUNT(*) AS count,
       COALESCE(SUM(length(CAST(key AS BLOB)) + length(CAST(value AS BLOB))), 0) AS bytes,
       COALESCE(MAX(length(CAST(key AS BLOB)) + length(CAST(value AS BLOB))), 0) AS entry
       FROM records WHERE bucket = ?`, bucket,
    );
    const budget = limits[bucket];
    if (!size || size.count > budget.count || size.bytes > budget.bytes || size.entry > budget.entry) {
      throw new StorageError('quota', 'Saved data exceeds this app’s storage limits; it has been preserved');
    }
    return this.db.getAllAsync<Row>('SELECT key, value FROM records WHERE bucket = ? ORDER BY key LIMIT ?', bucket, budget.count);
  }
  private validateBucket(bucket: StorageBucket): void {
    if (!['hosts', 'settings', 'drafts', 'bookmarks', 'themes'].includes(bucket)) throw new StorageError('corrupt', 'Unknown storage bucket');
  }
  private async write(bucket: Bucket, key: string, value: unknown): Promise<void> {
    identifier(key, 'storage key');
    const encoded = encode(value);
    const rows = await this.rows(bucket);
    const remaining = rows.filter((row) => row.key !== key);
    const budget = limits[bucket];
    // Count the key and encoded value, including recovery correlation metadata.
    const size = bytes(key) + bytes(encoded);
    if (size > budget.entry || remaining.length + 1 > budget.count || remaining.reduce((sum, row) => sum + bytes(row.key) + bytes(row.value), size) > budget.bytes) {
      throw new StorageError('quota', `${bucket} storage is full. Resolve or remove saved items before adding more; existing work has been preserved.`);
    }
    await this.db.runAsync('INSERT INTO records (bucket, key, value) VALUES (?, ?, ?) ON CONFLICT(bucket, key) DO UPDATE SET value = excluded.value', bucket, key, encoded);
  }
  get<T>(bucket: StorageBucket, key: string): Promise<T | undefined> {
    this.validateBucket(bucket);
    return this.enqueue(async () => {
      const row = await this.db.getFirstAsync<Row>('SELECT key, value FROM records WHERE bucket = ? AND key = ?', bucket, key);
      if (!row) return undefined;
      const value = decode<T>(row.value);
      return (bucket === 'bookmarks' ? bookmark(value).value : value) as T;
    });
  }
  set(bucket: StorageBucket, key: string, value: unknown): Promise<void> {
    this.validateBucket(bucket);
    // Freeze the value before awaiting earlier writes or a transaction.
    const frozen = decode<unknown>(encode(value));
    return this.enqueue(() => this.transaction(async () => {
      if (bucket === 'drafts') this.validateDraft(frozen);
      if (bucket === 'hosts') {
        const previous = await this.db.getFirstAsync<Row>('SELECT key, value FROM records WHERE bucket = ? AND key = ?', bucket, key);
        const clientId = previous && decode<{ clientId?: string }>(previous.value).clientId;
        if (clientId && (!frozen || typeof frozen !== 'object' || (frozen as { clientId?: string }).clientId !== clientId)) {
          throw new StorageError('corrupt', 'A saved host client identity cannot change during an edit');
        }
      }
      if (bucket === 'bookmarks') await this.writeBookmark(key, frozen);
      else await this.write(bucket, key, frozen);
    }));
  }
  private async writeBookmark(key: string, value: unknown): Promise<void> {
    identifier(key, 'bookmark key');
    const rows = (await this.rows('bookmarks')).map(row => ({ ...row, savedAt: bookmark(decode(row.value)).savedAt }));
    const savedAt = Math.max(Date.now(), ...rows.map(row => row.savedAt + 1));
    if (!Number.isSafeInteger(savedAt)) throw new StorageError('corrupt', 'Unreadable saved reading position order');
    const envelope: BookmarkEnvelope = { __whipBookmark: 1, savedAt, value };
    const size = bytes(key) + bytes(encode(envelope));
    const budget = limits.bookmarks;
    if (size > budget.entry) throw new StorageError('quota', 'This reading position exceeds the bookmark storage limit');
    const retained = rows.filter(row => row.key !== key).sort((left, right) => left.savedAt - right.savedAt || left.key.localeCompare(right.key));
    let total = retained.reduce((sum, row) => sum + bytes(row.key) + bytes(row.value), size);
    while (retained.length + 1 > budget.count || total > budget.bytes) {
      const oldest = retained.shift()!;
      await this.db.runAsync('DELETE FROM records WHERE bucket = ? AND key = ?', 'bookmarks', oldest.key);
      total -= bytes(oldest.key) + bytes(oldest.value);
    }
    // Reading hints are reconstructible. Eviction and replacement share this
    // transaction, so a failed write cannot discard the prior reading places.
    await this.write('bookmarks', key, envelope);
  }
  delete(bucket: StorageBucket, key: string): Promise<void> {
    this.validateBucket(bucket);
    return this.enqueue(() => this.db.runAsync('DELETE FROM records WHERE bucket = ? AND key = ?', bucket, key).then(() => {}));
  }
  list<T>(bucket: StorageBucket): Promise<Array<{ key: string; value: T }>> {
    this.validateBucket(bucket);
    return this.enqueue(async () => (await this.rows(bucket)).map(({ key, value }) => ({ key, value: (bucket === 'bookmarks' ? bookmark(decode(value)).value : decode(value)) as T })));
  }
  private validateDraft(value: unknown): asserts value is Draft {
    if (!value || typeof value !== 'object' || typeof (value as Draft).text !== 'string') throw new StorageError('corrupt', 'Invalid saved draft');
    identifier((value as Draft).revision, 'draft revision');
  }
  setDraft(key: string, draft: Draft): Promise<void> { return this.set('drafts', key, draft); }
  clearDraft(key: string, expectedRevision: string): Promise<boolean> {
    return this.enqueue(() => this.transaction(async () => {
      const row = await this.db.getFirstAsync<Row>('SELECT key, value FROM records WHERE bucket = ? AND key = ?', 'drafts', key);
      if (!row) return false;
      const draft = decode<Draft>(row.value);
      this.validateDraft(draft);
      if (draft.revision !== expectedRevision) return false;
      await this.db.runAsync('DELETE FROM records WHERE bucket = ? AND key = ?', 'drafts', key);
      return true;
    }));
  }
  prepareIntent(commandId: string, intent: CommandIntent): void {
    if (this.closing) throw new StorageError('closed', 'Storage is closed');
    identifier(commandId, 'command ID');
    if (this.intents.has(commandId)) throw new StorageError('corrupt', 'Command intent already prepared');
    const clean = cleanIntent(intent);
    const total = [...this.intents].reduce((sum, [id, value]) => sum + bytes(id) + bytes(encode(value)), bytes(commandId) + bytes(encode(clean)));
    if (this.intents.size >= 64 || total > 64 * 1024) throw new StorageError('quota', 'Too many prepared commands; resolve existing submissions first');
    this.intents.set(commandId, clean);
  }
  discardIntent(commandId: string): void { this.intents.delete(commandId); }
  private putRecovery(source: DurableRecord, decisionIntent?: CommandIntent): Promise<void> {
    const record = cleanRecord(source);
    const intent = decisionIntent ? cleanIntent(decisionIntent) : this.intents.get(record.commandId);
    return this.enqueue(async () => {
      await this.transaction(async () => {
        const key = recoveryKey(record);
        const existing = await this.db.getFirstAsync<Row>('SELECT key, value FROM records WHERE bucket = ? AND key = ?', 'recovery', key);
        if (existing) {
          const saved = this.readRecovery(existing);
          if (encode(saved.record) !== encode(record) || (intent && encode(saved.intent) !== encode(intent))) {
            throw new StorageError('corrupt', 'Recovery identity conflicts with an existing command');
          }
          return;
        }
        if (!intent) throw new StorageError('corrupt', 'Prepare the command recipient before persisting recovery');
        await this.write('recovery', key, { record, intent, knownAccepted: false } satisfies StoredRecovery<DurableRecord>);
      });
      this.intents.delete(record.commandId);
    });
  }
  private readRecovery(row: Row): StoredRecovery<DurableRecord> {
    const value = decode<StoredRecovery<DurableRecord>>(row.value);
    if (!value || typeof value.knownAccepted !== 'boolean' || !value.record) throw new StorageError('corrupt', 'Unreadable command recovery metadata; existing data has been preserved');
    const record = cleanRecord(value.record);
    if (row.key !== recoveryKey(record)) throw new StorageError('corrupt', 'Command recovery identity mismatch');
    // Missing correlation remains explicitly missing; callers block the root
    // conservatively instead of guessing a recipient from the selected screen.
    return { record, knownAccepted: value.knownAccepted, ...(value.intent ? { intent: cleanIntent(value.intent) } : {}) };
  }
  listRecovery(): Promise<StoredRecovery[]> {
    return this.enqueue(async () => (await this.rows('recovery')).map((row) => this.readRecovery(row))
      .filter((value): value is StoredRecovery => value.record.operation !== 'permission.decide'));
  }
  putDecision(record: PermissionRecoveryRecord, intent: CommandIntent): Promise<void> {
    if (record.operation !== 'permission.decide') return Promise.reject(new StorageError('corrupt', 'Invalid permission recovery operation'));
    identifier(intent.requestId, 'permission request ID');
    return this.putRecovery(record, intent);
  }
  listDecisions(): Promise<Array<StoredRecovery<PermissionRecoveryRecord>>> {
    return this.enqueue(async () => (await this.rows('recovery')).map((row) => this.readRecovery(row))
      .filter((value): value is StoredRecovery<PermissionRecoveryRecord> => value.record.operation === 'permission.decide'));
  }
  markDecisionAccepted(record: PermissionRecoveryRecord): Promise<void> { return this.markAccepted(record); }
  deleteDecision(record: PermissionRecoveryRecord): Promise<void> { return this.deleteRecovery(record); }
  private deleteRecovery(source: DurableRecord): Promise<void> {
    const record = cleanRecord(source);
    return this.enqueue(() => this.transaction(async () => {
      const key = recoveryKey(record);
      const row = await this.db.getFirstAsync<Row>('SELECT key, value FROM records WHERE bucket = ? AND key = ?', 'recovery', key);
      if (!row) return;
      if (encode(this.readRecovery(row).record) !== encode(record)) throw new StorageError('corrupt', 'Recovery identity does not match the saved command');
      await this.db.runAsync('DELETE FROM records WHERE bucket = ? AND key = ?', 'recovery', key);
    }));
  }
  markAccepted(source: DurableRecord): Promise<void> {
    const record = cleanRecord(source);
    const key = recoveryKey(record);
    return this.enqueue(() => this.transaction(async () => {
      const row = await this.db.getFirstAsync<Row>('SELECT key, value FROM records WHERE bucket = ? AND key = ?', 'recovery', key);
      if (!row) throw new StorageError('corrupt', 'Accepted command has no durable recovery record');
      const saved = this.readRecovery(row);
      if (encode(saved.record) !== encode(record)) throw new StorageError('corrupt', 'Accepted identity does not match the saved command');
      await this.write('recovery', key, { ...saved, knownAccepted: true });
    }));
  }
  async flush(): Promise<void> { await this.tail; if (this.failure) throw this.failure; }
  close(): Promise<void> {
    if (!this.closing) this.closing = this.tail.then(async () => {
      this.intents.clear();
      await this.db.closeAsync();
    });
    return this.closing;
  }
}

interface KeyRecord { version: 1; key: string; initialized: boolean }
const keyName = 'whip.database.v1';
let opening: Promise<MobileStorage> | undefined;
let nativeDatabase: StorageDatabase | undefined;
let activeStorage: MobileStorage | undefined;
let resetting: Promise<void> | undefined;
type NativeStorage = {
  prepareDirectory(): Promise<{ directory: string; databaseExists: boolean; databaseFilesExist?: boolean; resetPending?: boolean; backupExcluded: boolean }>;
  beginReset(): Promise<void>;
  removeDatabaseFiles(): Promise<void>;
  finishReset(): Promise<void>;
};
function nativeStorage(): NativeStorage {
  const Expo = require('expo') as typeof import('expo');
  return Expo.requireNativeModule<NativeStorage>('WhipStorage');
}

/** No Expo Go/plain SQLite fallback: availability failures block durable actions. */
export function openMobileStorage(): Promise<MobileStorage> {
  if (resetting) return Promise.reject(new StorageError('reset_pending', 'Local data reset is in progress'));
  if (!opening && nativeDatabase) return Promise.reject(new StorageError('unavailable', 'The previous storage connection could not close. Reopen the app before continuing.'));
  if (!opening) opening = openNativeStorage().catch((error: unknown) => { opening = undefined; throw error; });
  return opening;
}
async function openNativeStorage(): Promise<MobileStorage> {
  const SQLite = require('expo-sqlite') as typeof import('expo-sqlite');
  const SecureStore = require('expo-secure-store') as typeof import('expo-secure-store');
  const Crypto = require('expo-crypto') as typeof import('expo-crypto');
  const native = nativeStorage();
  const location = await native.prepareDirectory();
  if (location.resetPending) throw new StorageError('reset_pending', 'A local data reset was interrupted. Finish resetting local data before opening Whip.');
  if (!location.databaseExists && location.databaseFilesExist) throw new StorageError('database_missing', 'Database sidecar files remain without their database. Existing files have been preserved.');
  if (!location.backupExcluded) throw new StorageError('unavailable', 'Private storage could not be excluded from device backups');
  if (!location.directory.startsWith('/') || location.directory.includes('\0')) throw new StorageError('unavailable', 'Private storage did not return an absolute directory path');
  // Expo SQLite 57 parses this as a URL on both platforms. A bare iOS path
  // preserves percent escapes in toFilePath(), diverging from FileManager.
  const databaseDirectory = `file://${location.directory.split('/').map(encodeURIComponent).join('/')}`;
  const options = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK_THIS_DEVICE_ONLY, requireAuthentication: false };
  const saved = await SecureStore.getItemAsync(keyName, options);
  let key: KeyRecord;
  if (saved) {
    key = decode<KeyRecord>(saved);
    if (!key || key.version !== 1 || !/^[0-9a-f]{64}$/.test(key.key) || typeof key.initialized !== 'boolean') throw new StorageError('key_missing', 'The saved encryption key is invalid; the database has been preserved');
    if (key.initialized && !location.databaseExists) throw new StorageError('database_missing', 'The saved database is missing. Existing identity and recovery cannot be safely restored automatically.');
  } else {
    if (location.databaseExists) throw new StorageError('key_missing', 'The encryption key is missing; the existing database has been preserved');
    const random = await Crypto.getRandomBytesAsync(32);
    key = { version: 1, key: [...random].map((byte) => byte.toString(16).padStart(2, '0')).join(''), initialized: false };
    // Store before creating any encrypted bytes. A crash during setup can resume
    // with this pending key, without rotating it or deleting a partial database.
    await SecureStore.setItemAsync(keyName, encode(key), options);
  }
  const db = await SQLite.openDatabaseAsync('whip.db', { useNewConnection: true }, databaseDirectory);
  nativeDatabase = db;
  try {
    // The only interpolated SQL is a locally generated, validated 256-bit hex key.
    await db.execAsync(`PRAGMA key = "x'${key.key}'";`);
    const cipher = await db.getFirstAsync<{ cipher_version: string }>('PRAGMA cipher_version');
    if (!cipher?.cipher_version) throw new StorageError('unavailable', 'This build does not include SQLCipher encryption');
    await db.execAsync('PRAGMA journal_mode = WAL; PRAGMA synchronous = FULL; PRAGMA foreign_keys = ON; PRAGMA temp_store = MEMORY;');
    const storage = new SqliteMobileStorage(db);
    await storage.initialize(!key.initialized);
    await SecureStore.setItemAsync(keyName, encode({ ...key, initialized: true }), options);
    const close = storage.close.bind(storage);
    let closingStorage: Promise<void> | undefined;
    storage.close = () => closingStorage ??= close().then(() => {
      if (activeStorage === storage) { activeStorage = undefined; nativeDatabase = undefined; opening = undefined; }
    });
    activeStorage = storage;
    return storage;
  } catch (error) {
    try { await db.closeAsync(); if (nativeDatabase === db) nativeDatabase = undefined; } catch { /* Keep the handle: reset must close it before deleting files. */ }
    if (error instanceof StorageError) throw error;
    throw new StorageError('unavailable', 'Encrypted storage could not be opened; existing data has been preserved');
  }
}

/**
 * Destructively erase only this app's local data after the caller's explicit
 * confirmation dialog. Detach the runtime first; accepted host work continues.
 * Interrupted resets remain blocked on launch until this action is confirmed again.
 */
export function resetMobileStorage(confirmation: 'erase-local-whip-data'): Promise<void> {
  if (confirmation !== 'erase-local-whip-data') return Promise.reject(new StorageError('unavailable', 'Explicit confirmation is required to erase local Whip data'));
  if (resetting) return resetting;
  const existingOpening = opening;
  const promise = (async () => {
    await existingOpening?.catch(() => {});
    if (activeStorage) await activeStorage.close();
    else if (nativeDatabase) { await nativeDatabase.closeAsync(); nativeDatabase = undefined; }
    const SecureStore = require('expo-secure-store') as typeof import('expo-secure-store');
    const native = nativeStorage();
    await native.beginReset();
    await native.removeDatabaseFiles();
    await SecureStore.deleteItemAsync(keyName, { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK_THIS_DEVICE_ONLY, requireAuthentication: false });
    await native.finishReset();
    opening = undefined;
  })();
  resetting = promise;
  void promise.finally(() => { if (resetting === promise) resetting = undefined; }).catch(() => {});
  return promise;
}
