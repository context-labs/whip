/// <reference types="node" />
/** @jest-environment node */
import { DatabaseSync } from 'node:sqlite';
import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

jest.mock('expo', () => ({ requireNativeModule: jest.fn() }));
jest.mock('expo-crypto', () => ({ getRandomBytesAsync: jest.fn(async () => new Uint8Array(32).fill(17)) }));
jest.mock('expo-secure-store', () => ({
  getItemAsync: jest.fn(), setItemAsync: jest.fn(), deleteItemAsync: jest.fn(),
  AFTER_FIRST_UNLOCK_THIS_DEVICE_ONLY: 'this-device-only',
}));
jest.mock('expo-sqlite', () => ({ openDatabaseAsync: jest.fn() }));

const key = (initialized: boolean) => JSON.stringify({ version: 1, key: '11'.repeat(32), initialized });
async function nativeFixture(saved: string | null, exists: boolean, encrypted = true) {
  jest.resetModules();
  const Expo = require('expo') as typeof import('expo');
  const SecureStore = require('expo-secure-store') as typeof import('expo-secure-store');
  const SQLite = require('expo-sqlite') as typeof import('expo-sqlite');
  let database = new DatabaseSync(':memory:');
  let currentKey = saved;
  const location = { directory: '/private/Whip', databaseExists: exists, databaseFilesExist: exists, backupExcluded: true, resetPending: false };
  let closed = false;
  const calls: string[] = [];
  const adapter = {
    execAsync: async (sql: string) => { calls.push(sql); database.exec(sql); },
    runAsync: async (sql: string, ...params: Array<string | number>) => database.prepare(sql).run(...params),
    getAllAsync: async (sql: string, ...params: Array<string | number>) => database.prepare(sql).all(...params),
    getFirstAsync: async (sql: string, ...params: Array<string | number>) => {
      calls.push(sql);
      if (sql === 'PRAGMA cipher_version') return encrypted ? { cipher_version: '4.11.0' } : null;
      return database.prepare(sql).get(...params) ?? null;
    },
    closeAsync: jest.fn(async () => { calls.push('close'); closed = true; database.close(); }),
  };
  const native = {
    prepareDirectory: jest.fn(async () => ({ ...location })),
    beginReset: jest.fn(async () => { calls.push('beginReset'); location.resetPending = true; }),
    removeDatabaseFiles: jest.fn(async () => { calls.push('removeDatabaseFiles'); location.databaseExists = false; location.databaseFilesExist = false; }),
    finishReset: jest.fn(async () => { calls.push('finishReset'); location.resetPending = false; }),
  };
  jest.mocked(Expo.requireNativeModule).mockReturnValue(native);
  jest.mocked(SecureStore.getItemAsync).mockImplementation(async () => currentKey);
  jest.mocked(SecureStore.setItemAsync).mockImplementation(async (_name, value) => { currentKey = value; });
  jest.mocked(SecureStore.deleteItemAsync).mockImplementation(async () => { calls.push('deleteKey'); currentKey = null; });
  jest.mocked(SQLite.openDatabaseAsync).mockImplementation(async () => {
    if (closed) { database = new DatabaseSync(':memory:'); closed = false; }
    location.databaseExists = true; location.databaseFilesExist = true;
    return adapter as never;
  });
  const { openMobileStorage, resetMobileStorage } = require('./storage') as typeof import('./storage');
  return { openMobileStorage, resetMobileStorage, SQLite, SecureStore, get database() { return database; }, calls, native, location, adapter, isClosed: () => closed };
}

test('fresh setup persists a device-only pending key before SQLite and verifies SQLCipher before schema', async () => {
  const fixture = await nativeFixture(null, false);
  const storage = await fixture.openMobileStorage();
  const { SecureStore, SQLite } = fixture;
  expect(SecureStore.setItemAsync).toHaveBeenNthCalledWith(1, 'whip.database.v1', key(false), { keychainAccessible: 'this-device-only', requireAuthentication: false });
  expect(SecureStore.setItemAsync).toHaveBeenNthCalledWith(2, 'whip.database.v1', key(true), { keychainAccessible: 'this-device-only', requireAuthentication: false });
  expect(jest.mocked(SecureStore.setItemAsync).mock.invocationCallOrder[0]).toBeLessThan(jest.mocked(SQLite.openDatabaseAsync).mock.invocationCallOrder[0]);
  expect(SQLite.openDatabaseAsync).toHaveBeenCalledWith('whip.db', { useNewConnection: true }, 'file:///private/Whip');
  expect(fixture.calls[0]).toMatch(/^PRAGMA key =/);
  expect(fixture.calls[1]).toBe('PRAGMA cipher_version');
  await storage.setDraft('recipient', { text: 'retained', revision: 'r1' });
  await storage.close();
});

test.each([
  '/var/mobile/Containers/Data/Application/example/Library/Application Support/Whip',
  '/data/user/0/dev.contextlabs.whip.mobile/no_backup/Whip',
  '/private/Whip 100%/#drafts?/caf\u00e9',
])('SQLite receives a file URI preserving the native directory: %s', async directory => {
  const fixture = await nativeFixture(null, false);
  fixture.location.directory = directory;
  const storage = await fixture.openMobileStorage();
  const uri = jest.mocked(fixture.SQLite.openDatabaseAsync).mock.calls[0][2]!;
  expect(uri).toMatch(/^file:\/\/\//);
  expect(fileURLToPath(`${uri}/whip.db`)).toBe(`${directory}/whip.db`);
  expect(new URL(uri).search).toBe('');
  expect(new URL(uri).hash).toBe('');
  await storage.close();
});

test.each(['relative/Whip', 'file:///private/Whip', '/private/Whip\0other'])('invalid native directory fails before creating keys or files: %s', async directory => {
  const fixture = await nativeFixture(null, false);
  fixture.location.directory = directory;
  await expect(fixture.openMobileStorage()).rejects.toMatchObject({ code: 'unavailable' });
  expect(fixture.SecureStore.setItemAsync).not.toHaveBeenCalled();
  expect(fixture.SQLite.openDatabaseAsync).not.toHaveBeenCalled();
  fixture.database.close();
});

// Exercise Foundation and the installed Expo native URL conversion, rather than
// replacing its behavior with a JS mock. CI on other OSes still runs URI tests.
(process.platform === 'darwin' ? test : test.skip)('Expo iOS resolves the URI to the native backup-excluded directory and reopens the same file', async () => {
  const temporary = mkdtempSync(join(tmpdir(), 'whip-sqlite-path-'));
  const directory = join(temporary, 'Library', 'Application Support', 'Whip 100% # ? caf\u00e9');
  const fixture = await nativeFixture(null, false);
  fixture.location.directory = directory;
  const storage = await fixture.openMobileStorage();
  const uri = jest.mocked(fixture.SQLite.openDatabaseAsync).mock.calls[0][2]!;
  await storage.close();
  const expoRoot = dirname(require.resolve('expo-sqlite/package.json'));
  const conversion = readFileSync(join(expoRoot, 'ios/URL+FilePath.swift'), 'utf8');
  try {
    const result = spawnSync('xcrun', ['swift', '-'], {
      encoding: 'utf8', timeout: 25_000,
      env: { ...process.env, WHIP_TEST_DIRECTORY: directory, WHIP_TEST_URI: `${uri}/whip.db` },
      input: `import Foundation
import SQLite3
${conversion}
let environment = ProcessInfo.processInfo.environment
let directory = environment["WHIP_TEST_DIRECTORY"]!
var nativeDirectory = URL(fileURLWithPath: directory, isDirectory: true)
let expected = nativeDirectory.appendingPathComponent("whip.db").path
let manager = FileManager.default
try manager.createDirectory(at: nativeDirectory, withIntermediateDirectories: true)
var values = URLResourceValues()
values.isExcludedFromBackup = true
try nativeDirectory.setResourceValues(values)
let excluded = try nativeDirectory.resourceValues(forKeys: [.isExcludedFromBackupKey]).isExcludedFromBackup
precondition(excluded == true)
// This is the URL(string:) entry point used by SQLiteModule.swift.
let path = URL(string: environment["WHIP_TEST_URI"]!)!
precondition(path.isFileURL && path.toFilePath() == expected)
precondition(path.deletingLastPathComponent().toFilePath() == nativeDirectory.path)
precondition(URL(string: expected)!.toFilePath() != expected, "Bare paths must reproduce the original bug")
precondition(URL(string: expected)!.toFilePath().contains("Application%20Support"))
var database: OpaquePointer?
precondition(sqlite3_open(path.toFilePath(), &database) == SQLITE_OK)
precondition(sqlite3_exec(database, "CREATE TABLE fixture(value TEXT); INSERT INTO fixture VALUES ('survives reopen');", nil, nil, nil) == SQLITE_OK)
precondition(sqlite3_close(database) == SQLITE_OK)
precondition(manager.fileExists(atPath: expected))
// Native existence checks and a second open must find exactly that same file.
precondition(sqlite3_open(path.toFilePath(), &database) == SQLITE_OK)
var statement: OpaquePointer?
precondition(sqlite3_prepare_v2(database, "SELECT value FROM fixture", -1, &statement, nil) == SQLITE_OK)
precondition(sqlite3_step(statement) == SQLITE_ROW)
precondition(String(cString: sqlite3_column_text(statement, 0)) == "survives reopen")
precondition(sqlite3_finalize(statement) == SQLITE_OK)
precondition(sqlite3_close(database) == SQLITE_OK)
print("Native path and reopen verified")
`,
    });
    expect({ status: result.status, error: result.error?.message, stderr: result.stderr }).toEqual({ status: 0, error: undefined, stderr: '' });
    expect(result.stdout).toContain('Native path and reopen verified');
    expect(existsSync(join(directory, 'whip.db'))).toBe(true);
    expect(existsSync(join(temporary, 'Library', 'Application%20Support'))).toBe(false);
  } finally { rmSync(temporary, { recursive: true, force: true }); }
}, 30_000);

test.each([
  { saved: null, exists: true, code: 'key_missing' },
  { saved: key(true), exists: false, code: 'database_missing' },
  { saved: '{"version":1,"key":"invalid","initialized":true}', exists: true, code: 'key_missing' },
])('key/database mismatch fails without opening, replacing, or deleting data: $code', async ({ saved, exists, code }) => {
  const fixture = await nativeFixture(saved, exists);
  await expect(fixture.openMobileStorage()).rejects.toMatchObject({ code });
  expect(fixture.SQLite.openDatabaseAsync).not.toHaveBeenCalled();
  expect(fixture.SecureStore.setItemAsync).not.toHaveBeenCalled();
  fixture.database.close();
});

test('a build without SQLCipher never creates schema or marks the key initialized', async () => {
  const fixture = await nativeFixture(null, false, false);
  await expect(fixture.openMobileStorage()).rejects.toMatchObject({ code: 'unavailable' });
  expect(fixture.calls).toHaveLength(3);
  expect(fixture.calls.at(-1)).toBe('close');
  expect(fixture.SecureStore.setItemAsync).toHaveBeenCalledTimes(1);
  expect(fixture.isClosed()).toBe(true);
});

test('interrupted first setup resumes with the original pending key, while initialized empty databases are preserved', async () => {
  const pending = await nativeFixture(key(false), true);
  const storage = await pending.openMobileStorage();
  expect(pending.SecureStore.setItemAsync).toHaveBeenCalledTimes(1);
  expect(pending.SecureStore.setItemAsync).toHaveBeenCalledWith('whip.database.v1', key(true), expect.any(Object));
  await storage.close();
  const initialized = await nativeFixture(key(true), true);
  await expect(initialized.openMobileStorage()).rejects.toMatchObject({ code: 'schema' });
  expect(initialized.SecureStore.setItemAsync).not.toHaveBeenCalled();
  expect(initialized.isClosed()).toBe(true);
});

test('reset requires explicit confirmation, drains/closes first and erases only the named local key after files', async () => {
  const fixture = await nativeFixture(null, false);
  const storage = await fixture.openMobileStorage();
  const queued = storage.setDraft('draft', { text: 'local unsent work', revision: 'r1' });
  await expect(fixture.resetMobileStorage('unconfirmed' as never)).rejects.toMatchObject({ code: 'unavailable' });
  expect(fixture.native.beginReset).not.toHaveBeenCalled();
  await fixture.resetMobileStorage('erase-local-whip-data');
  await queued;
  expect(fixture.calls.slice(-5)).toEqual(['close', 'beginReset', 'removeDatabaseFiles', 'deleteKey', 'finishReset']);
  expect(fixture.SecureStore.deleteItemAsync).toHaveBeenCalledWith('whip.database.v1', { keychainAccessible: 'this-device-only', requireAuthentication: false });
  const fresh = await fixture.openMobileStorage();
  expect(await fresh.list('drafts')).toEqual([]);
  expect(await fresh.list('hosts')).toEqual([]);
  await fresh.close();
});

test('interrupted reset fails closed without regenerating a key and can be explicitly finished', async () => {
  const fixture = await nativeFixture(null, false);
  await fixture.openMobileStorage();
  fixture.native.finishReset.mockRejectedValueOnce(new Error('interrupted after key deletion'));
  await expect(fixture.resetMobileStorage('erase-local-whip-data')).rejects.toThrow('interrupted');
  const writes = jest.mocked(fixture.SecureStore.setItemAsync).mock.calls.length;
  await expect(fixture.openMobileStorage()).rejects.toMatchObject({ code: 'reset_pending' });
  expect(fixture.SecureStore.setItemAsync).toHaveBeenCalledTimes(writes);
  expect(fixture.location.resetPending).toBe(true);
  await fixture.resetMobileStorage('erase-local-whip-data');
  const fresh = await fixture.openMobileStorage();
  expect(fixture.location.resetPending).toBe(false);
  await fresh.close();
});

test('file deletion failure preserves its encryption key and close failure prevents reset entirely', async () => {
  const fixture = await nativeFixture(null, false);
  await fixture.openMobileStorage();
  fixture.native.removeDatabaseFiles.mockRejectedValueOnce(new Error('filesystem unavailable'));
  await expect(fixture.resetMobileStorage('erase-local-whip-data')).rejects.toThrow('filesystem unavailable');
  expect(fixture.SecureStore.deleteItemAsync).not.toHaveBeenCalled();
  expect(fixture.native.finishReset).not.toHaveBeenCalled();
  await expect(fixture.openMobileStorage()).rejects.toMatchObject({ code: 'reset_pending' });
  const other = await nativeFixture(null, false);
  await other.openMobileStorage();
  other.adapter.closeAsync.mockRejectedValue(new Error('busy connection'));
  await expect(other.resetMobileStorage('erase-local-whip-data')).rejects.toThrow('busy connection');
  expect(other.native.beginReset).not.toHaveBeenCalled();
  expect(other.SecureStore.deleteItemAsync).not.toHaveBeenCalled();
  other.database.close();
});

test('a failed key deletion retains the reset marker until an explicit retry finishes', async () => {
  const fixture = await nativeFixture(null, false);
  await fixture.openMobileStorage();
  jest.mocked(fixture.SecureStore.deleteItemAsync).mockRejectedValueOnce(new Error('keychain unavailable'));
  await expect(fixture.resetMobileStorage('erase-local-whip-data')).rejects.toThrow('keychain unavailable');
  expect(fixture.native.finishReset).not.toHaveBeenCalled();
  expect(fixture.location.databaseExists).toBe(false);
  expect(fixture.location.resetPending).toBe(true);
  await expect(fixture.openMobileStorage()).rejects.toMatchObject({ code: 'reset_pending' });
  await fixture.resetMobileStorage('erase-local-whip-data');
  const fresh = await fixture.openMobileStorage();
  expect(await fresh.list('drafts')).toEqual([]);
  await fresh.close();
});

test('concurrent reset requests coalesce and normal opening is blocked while reset owns storage', async () => {
  const fixture = await nativeFixture(key(true), false);
  let release!: () => void;
  fixture.native.beginReset.mockImplementationOnce(() => new Promise<void>(resolve => { release = resolve; }));
  const first = fixture.resetMobileStorage('erase-local-whip-data');
  const second = fixture.resetMobileStorage('erase-local-whip-data');
  expect(first).toBe(second);
  await Promise.resolve(); await Promise.resolve();
  await expect(fixture.openMobileStorage()).rejects.toMatchObject({ code: 'reset_pending' });
  release(); await first;
  expect(fixture.SecureStore.deleteItemAsync).toHaveBeenCalledTimes(1);
  fixture.database.close();
});

test('orphaned sidecars and an existing reset marker never trigger fresh database creation', async () => {
  const fixture = await nativeFixture(null, false);
  fixture.location.databaseFilesExist = true;
  await expect(fixture.openMobileStorage()).rejects.toMatchObject({ code: 'database_missing' });
  expect(fixture.SQLite.openDatabaseAsync).not.toHaveBeenCalled();
  expect(fixture.SecureStore.setItemAsync).not.toHaveBeenCalled();
  fixture.location.resetPending = true;
  await expect(fixture.openMobileStorage()).rejects.toMatchObject({ code: 'reset_pending' });
  fixture.database.close();
});
