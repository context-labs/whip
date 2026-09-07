import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DeliveryUncertainError, RpcError, type CommandHandle, type ConnectionSnapshot, type RecoveryStorage } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { createFallbackStorage, type AppStorage } from '../src/platform';

const mocks = vi.hoisted(() => {
  const client = { connect: vi.fn(async () => {}), whenConnected: vi.fn(async () => {}), close: vi.fn(), subscribe: vi.fn((_listener: () => void) => vi.fn()), getSnapshot: vi.fn((): Pick<ConnectionSnapshot, 'state' | 'error'> & { info?: { runtime_id: string; connection_id: string } } => ({ state: 'connected', info: { runtime_id: 'runtime', connection_id: 'connection' } })), session: vi.fn((rootId: string) => ({ rootId })) };
  return { client, options: [] as { recoveryStorage: RecoveryStorage }[], createView: vi.fn(() => ({ start: vi.fn(async () => {}), dispose: vi.fn(async () => {}) })), list: { start: vi.fn(async () => {}), dispose: vi.fn(async () => {}) } };
});
vi.mock('@whip/sdk', async importOriginal => ({ ...await importOriginal<typeof import('@whip/sdk')>(), createWhipClient: (options: { recoveryStorage: RecoveryStorage }) => { mocks.options.push(options); return mocks.client; } }));
vi.mock('@whip/sdk/state', () => ({ createSessionView: mocks.createView, createSessionListView: () => mocks.list }));

function runtime(storage?: AppStorage) {
  const values = new Map<string, string>();
  return new AppRuntime({ defaultEndpoint: 'http://localhost:8080', storage: storage ?? { keys: () => [...values.keys()], getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } }, copy: async () => {}, download: () => {}, openExternal: () => {} });
}
beforeEach(() => { vi.clearAllMocks(); mocks.options.length = 0; });
afterEach(() => { vi.useRealTimers(); });
describe('application observation ownership', () => {
  it('shares a view across StrictMode lease release/reacquisition and disposes once', async () => {
    vi.useFakeTimers();
    const app = runtime(); await app.connect();
    const first = app.acquireView('root'); first.release();
    const second = app.acquireView('root');
    expect(second.view).toBe(first.view);
    vi.advanceTimersByTime(30_001);
    expect(first.view.dispose).not.toHaveBeenCalled();
    second.release(); second.release();
    vi.advanceTimersByTime(30_001);
    expect(first.view.dispose).toHaveBeenCalledTimes(1);
    app.dispose(); app.dispose();
    expect(mocks.client.close).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
  it('never evicts actively observed roots to bypass subscription limits', async () => {
    const app = runtime(); await app.connect();
    const leases = ['a', 'b', 'c', 'd'].map(id => app.acquireView(id));
    expect(() => app.acquireView('e')).toThrow('Four session views');
    for (const lease of leases) expect(lease.view.dispose).not.toHaveBeenCalled();
    app.dispose();
  });
  it('evicts the least recently used inactive view instead of the first-created view', async () => {
    const app = runtime(); await app.connect();
    const leases = ['a', 'b', 'c', 'd'].map(id => app.acquireView(id));
    leases.forEach(lease => lease.release());
    app.acquireView('a').release(); app.acquireView('e').release();
    expect(leases[0]!.view.dispose).not.toHaveBeenCalled();
    expect(leases[1]!.view.dispose).toHaveBeenCalledTimes(1); app.dispose();
  });
  it('separates drafts by runtime, root, and recipient without persisting prompt bodies', () => {
    const app = runtime();
    app.setDraft('host:a:child', 'Private draft');
    expect(app.draft('host:a:child')).toBe('Private draft');
    expect(app.draft('other:a:child')).toBe('');
    expect(app.platform.storage.getItem('whip.web.recovery.v1')).toBeNull();
    app.dispose();
  });
  it('keeps authoritative failed outcomes distinct from delivery uncertainty', async () => {
    const app = runtime(); await app.connect();
    const handle = { commandId: 'id', accepted: async () => ({ status: 'running' }), result: async () => ({ status: 'interrupted', failure: { message: 'Daemon restarted' } }) } as unknown as CommandHandle<'submit'>;
    await expect(app.run(handle, 'Send')).rejects.toThrow('Daemon restarted');
    expect(app.getSnapshot().commands[0]?.status).toBe('interrupted');
    app.dispose();
  });
  it('validates a host address before detaching an existing connection', async () => {
    const app = runtime(); await app.connect();
    await expect(app.connect('javascript:alert(1)')).rejects.toThrow();
    expect(mocks.client.close).not.toHaveBeenCalled();
    app.dispose();
  });
  it('does not schedule a retired lease timer after disposing the application', async () => {
    vi.useFakeTimers();
    const app = runtime(); await app.connect();
    const lease = app.acquireView('root');
    app.dispose(); lease.release();
    expect(vi.getTimerCount()).toBe(0);
    expect(lease.view.dispose).toHaveBeenCalledTimes(1);
  });
  it('reports setup storage errors without detaching the healthy connection', async () => {
    const app = runtime(); await app.connect();
    vi.spyOn(app.platform.storage, 'setItem').mockImplementation(() => { throw new Error('Storage denied'); });
    await expect(app.connect('http://localhost:9000')).rejects.toThrow('Storage denied');
    expect(app.getSnapshot().error).toBe('Storage denied');
    expect(mocks.client.close).not.toHaveBeenCalled();
    app.dispose();
  });
  it('keeps recoverable connection failures out of persistent application errors', async () => {
    const error = new Error('WebSocket connection failed');
    const snapshot = vi.spyOn(mocks.client, 'getSnapshot').mockReturnValue({ state: 'reconnecting', error });
    mocks.client.connect.mockRejectedValueOnce(error);
    const app = runtime();
    await expect(app.connect()).rejects.toBe(error);
    expect(app.getSnapshot().error).toBeUndefined();
    app.report('Drafts could not be saved');
    snapshot.mockReturnValue({ state: 'connected', info: { runtime_id: 'runtime', connection_id: 'recovered' } });
    for (const [listener] of mocks.client.subscribe.mock.calls) listener();
    expect(app.getSnapshot().error).toBe('Drafts could not be saved');
    app.dispose();
  });
});

describe('draft persistence and bounded storage', () => {
  it('writes and forgets recovery identities only inside the shared storage transaction', async () => {
    const source = runtime().platform.storage;
    const callbacks: (() => unknown)[] = [];
    const transaction = vi.fn((_key: string, update: () => unknown) => new Promise(resolve => { callbacks.push(() => resolve(update())); })) as NonNullable<AppStorage['transaction']>;
    const storage = createFallbackStorage(() => source, vi.fn(), transaction);
    const app = runtime(storage); await app.connect();
    const recovery = mocks.options.at(-1)!.recoveryStorage;
    const first = { version: 1 as const, runtimeId: 'runtime', clientId: 'client', commandId: 'first', operation: 'submit' as const };
    const second = { ...first, commandId: 'second' };
    const writingA = recovery.put(first); const writingB = recovery.put(second);
    expect(source.getItem('whip.web.recovery.v1')).toBeNull();
    expect(transaction).toHaveBeenCalledTimes(2);
    callbacks.shift()!(); await writingA;
    callbacks.shift()!(); await writingB;
    expect((await recovery.list()).map(record => record.commandId)).toEqual(['first', 'second']);
    const deleting = recovery.delete(first);
    expect((await recovery.list()).map(record => record.commandId)).toEqual(['first', 'second']);
    callbacks.shift()!(); await deleting;
    expect((await recovery.list()).map(record => record.commandId)).toEqual(['second']);
    app.dispose();
  });
  it('refuses persistent recovery writes when browser locks are missing or denied', async () => {
    const source = runtime().platform.storage;
    const record = { version: 1 as const, runtimeId: 'runtime', clientId: 'client', commandId: 'first', operation: 'submit' as const };
    const unavailable = runtime(createFallbackStorage(() => source, vi.fn())); await unavailable.connect();
    await expect(mocks.options.at(-1)!.recoveryStorage.put(record)).rejects.toThrow('Web Locks');
    expect(source.getItem('whip.web.recovery.v1')).toBeNull();
    unavailable.dispose();
    const denied = runtime(createFallbackStorage(() => source, vi.fn(), async () => { throw new Error('Browser denied the lock'); })); await denied.connect();
    await expect(mocks.options.at(-1)!.recoveryStorage.put(record)).rejects.toThrow('denied the lock');
    expect(source.getItem('whip.web.recovery.v1')).toBeNull();
    denied.dispose();
    const ephemeral = runtime(createFallbackStorage(() => { throw new Error('No storage'); }, vi.fn())); await ephemeral.connect();
    await mocks.options.at(-1)!.recoveryStorage.put(record);
    expect((await mocks.options.at(-1)!.recoveryStorage.list()).length).toBe(1);
    ephemeral.dispose();
  });
  it('debounces drafts separately from metadata and restores them after reload', () => {
    vi.useFakeTimers();
    const app = runtime();
    const save = vi.spyOn(app.platform.storage, 'setItem');
    app.setDraft('runtime:root:agent', 'first');
    app.setDraft('runtime:root:agent', 'private draft');
    expect(save).not.toHaveBeenCalled();
    vi.advanceTimersByTime(150);
    expect(save).toHaveBeenCalledTimes(1);
    expect(app.platform.storage.getItem('whip.web.recovery.v1')).toBeNull();
    const reloaded = runtime(app.platform.storage);
    expect(reloaded.draft('runtime:root:agent')).toBe('private draft');
    app.setDraft('runtime:root:agent', 'updated before page closes');
    app.dispose();
    const closed = runtime(app.platform.storage);
    expect(closed.draft('runtime:root:agent')).toBe('updated before page closes');
    reloaded.dispose(); closed.dispose();
  });
  it('preserves different recipients across independent tabs and synchronous disposal', () => {
    const first = runtime();
    const second = runtime(first.platform.storage);
    first.setDraft('runtime:root:a', 'First tab'); first.flushDrafts();
    second.setDraft('runtime:root:b', 'Second tab'); second.dispose();
    first.setDraft('runtime:root:a', 'First tab updated'); first.dispose();
    const reloaded = runtime(first.platform.storage);
    expect(reloaded.draft('runtime:root:a')).toBe('First tab updated');
    expect(reloaded.draft('runtime:root:b')).toBe('Second tab');
    reloaded.setDraft('runtime:root:a', ''); reloaded.dispose();
    expect(runtime(first.platform.storage).draft('runtime:root:b')).toBe('Second tab');
  });
  it('uses last writer only for the same recipient without resurrecting stale drafts', () => {
    const first = runtime(); first.setDraft('runtime:root:a', 'Original'); first.flushDrafts();
    const second = runtime(first.platform.storage);
    first.setDraft('runtime:root:a', ''); first.flushDrafts();
    expect(second.draft('runtime:root:a')).toBe('');
    second.setDraft('runtime:root:b', 'Independent'); second.flushDrafts();
    expect(runtime(first.platform.storage).draft('runtime:root:a')).toBe('');
    second.setDraft('runtime:root:a', 'Explicit competing edit'); second.flushDrafts();
    expect(runtime(first.platform.storage).draft('runtime:root:a')).toBe('Explicit competing edit');
    first.dispose(); second.dispose();
  });
  it('refuses overflow without truncating or evicting another draft', () => {
    const app = runtime();
    for (let i = 0; i < 32; i++) app.setDraft(`runtime:root:${i}`, `draft ${i}`);
    expect(() => app.setDraft('runtime:root:overflow', 'new')).toThrow('32 unsent drafts');
    expect(() => app.setDraft('runtime:root:0', 'é'.repeat(128 * 1024 + 1))).toThrow('256 KiB');
    expect(app.draft('runtime:root:0')).toBe('draft 0');
    app.dispose();
    const total = runtime();
    for (let i = 0; i < 3; i++) total.setDraft(`runtime:root:${i}`, 'a'.repeat(256 * 1024));
    expect(() => total.setDraft('runtime:root:3', 'a'.repeat(256 * 1024))).toThrow('1 MiB');
    expect(total.draft('runtime:root:2')).toHaveLength(256 * 1024);
    total.dispose();
  });
  it('allows explicit clearing when another writer has exceeded aggregate bounds', () => {
    const app = runtime();
    for (let index = 0; index < 33; index++) app.platform.storage.setItem(`whip.web.draft.v1:runtime:root:${index}`, 'External draft');
    expect(() => app.setDraft('runtime:root:new', 'Additional')).toThrow('32 unsent drafts');
    app.setDraft('runtime:root:0', ''); app.flushDrafts();
    expect(app.platform.storage.getItem('whip.web.draft.v1:runtime:root:0')).toBeNull();
    app.setDraft('runtime:root:1', 'Updated'); app.flushDrafts();
    expect(app.platform.storage.getItem('whip.web.draft.v1:runtime:root:1')).toBe('Updated');
    app.dispose();
  });
  it('explicitly discards saved and pending drafts without changing recovery or preferences', () => {
    vi.useFakeTimers();
    const app = runtime();
    app.setDraft('runtime:root:saved', 'Saved'); app.flushDrafts();
    app.setDraft('runtime:root:pending', 'Pending');
    app.platform.storage.setItem('whip.web.recovery.v1', 'identity metadata');
    app.platform.storage.setItem('whip.appearance.theme.v1', 'theme preference');
    app.discardDrafts();
    expect(app.draft('runtime:root:saved')).toBe('');
    expect(app.draft('runtime:root:pending')).toBe('');
    expect(vi.getTimerCount()).toBe(0);
    expect(app.platform.storage.keys().filter(key => key.startsWith('whip.web.draft.v1:'))).toEqual([]);
    expect(app.platform.storage.getItem('whip.web.recovery.v1')).toBe('identity metadata');
    expect(app.platform.storage.getItem('whip.appearance.theme.v1')).toBe('theme preference');
    app.dispose();
  });
  it('retains in-memory drafts and reports failed writes', () => {
    const app = runtime();
    app.setDraft('runtime:root:agent', 'keep this');
    vi.spyOn(app.platform.storage, 'setItem').mockImplementation(() => { throw new Error('Quota'); });
    app.flushDrafts();
    expect(app.draft('runtime:root:agent')).toBe('keep this');
    expect(app.getSnapshot().error).toContain('Drafts could not be saved');
    app.dispose();
  });
  it('falls back on denied storage access and later quota failures, with one notice', () => {
    const notice = vi.fn();
    const denied = createFallbackStorage(() => { throw new Error('Denied'); }, notice);
    denied.setItem('key', 'value'); expect(denied.getItem('key')).toBe('value');
    denied.removeItem('key'); expect(denied.getItem('key')).toBeNull();
    expect(notice).toHaveBeenCalledTimes(1);
    notice.mockClear();
    const source = { keys: () => ['previous'], getItem: () => 'existing', setItem: () => { throw new Error('Quota'); }, removeItem: () => {} };
    const quota = createFallbackStorage(() => source, notice);
    expect(quota.getItem('previous')).toBe('existing');
    quota.setItem('next', 'new');
    expect(quota.getItem('previous')).toBe('existing');
    expect(quota.getItem('next')).toBe('new');
    expect(notice).toHaveBeenCalledTimes(1);
  });
});

function command(overrides: Record<string, unknown> = {}) {
  return { client: mocks.client, commandId: 'original-id', accepted: vi.fn(async () => { throw new DeliveryUncertainError('original-id'); }), status: vi.fn(async () => ({ status: 'running' })), result: vi.fn(async () => ({ status: 'succeeded' })), retry: vi.fn(async () => ({ status: 'running' })), ...overrides } as unknown as CommandHandle<'submit'>;
}
const missing = () => new RpcError({ code: -32011, message: 'Command not found', data: { kind: 'command_not_found' } });

describe('uncertain command acceptance', () => {
  it('reconciles lost acknowledgements and calls acceptance once before completion', async () => {
    const app = runtime(); await app.connect();
    const handle = command();
    const accepted = vi.fn();
    await app.run(handle, 'Send', accepted, 'runtime:root:agent');
    expect(handle.status).toHaveBeenCalledTimes(1);
    expect(handle.result).toHaveBeenCalledTimes(1);
    expect(handle.retry).not.toHaveBeenCalled();
    expect(accepted).toHaveBeenCalledTimes(1);
    expect(app.getSnapshot().commands[0]).toMatchObject({ status: 'succeeded', draftKey: 'runtime:root:agent' });
    expect(app.getSnapshot().commands[0]?.delivery).toBeUndefined();
    app.dispose();
  });
  it('retains absence for explicit same-handle retry and never sends it automatically', async () => {
    const app = runtime(); await app.connect();
    const handle = command({ status: vi.fn(async () => { throw missing(); }) });
    const accepted = vi.fn();
    await expect(app.run(handle, 'Send', accepted, 'draft')).rejects.toThrow('Command not found');
    expect(app.getSnapshot().commands[0]?.delivery).toBe('absent');
    expect(handle.retry).not.toHaveBeenCalled();
    expect(accepted).not.toHaveBeenCalled();
    await app.retryCommand('original-id');
    expect(handle.retry).toHaveBeenCalledTimes(1);
    expect(accepted).toHaveBeenCalledTimes(1);
    expect(app.getSnapshot().commands[0]?.id).toBe('original-id');
    expect(app.getSnapshot().commands[0]?.delivery).toBeUndefined();
    app.dispose();
  });
  it('does not treat generic lookup failure as absence, and supports explicit recheck', async () => {
    const app = runtime(); await app.connect();
    const status = vi.fn().mockRejectedValueOnce(new Error('Database unavailable')).mockResolvedValue({ status: 'running' });
    const handle = command({ status });
    const accepted = vi.fn();
    await expect(app.run(handle, 'Send', accepted, 'draft')).rejects.toThrow('Database unavailable');
    expect(app.getSnapshot().commands[0]?.delivery).toBe('uncertain');
    await expect(app.retryCommand('original-id')).rejects.toThrow('only after');
    await app.checkCommand('original-id');
    expect(handle.retry).not.toHaveBeenCalled();
    expect(accepted).toHaveBeenCalledTimes(1);
    app.dispose();
  });
  it('keeps unresolved delivery visible while bounding completed notices', async () => {
    const app = runtime(); await app.connect();
    await expect(app.run(command({ status: async () => { throw missing(); } }), 'Send', undefined, 'draft')).rejects.toThrow();
    for (let i = 0; i < 40; i++) await app.run(command({ commandId: `done-${i}`, accepted: async () => ({ status: 'running' }) }), 'Rename');
    expect(app.getSnapshot().commands).toHaveLength(33);
    expect(app.getSnapshot().commands.find(item => item.id === 'original-id')?.delivery).toBe('absent');
    app.dispose();
  });
  it('shares concurrent local status checks and ignores old-host acceptance', async () => {
    const app = runtime(); await app.connect();
    let finishStatus!: (value: { status: string }) => void;
    const status = vi.fn().mockRejectedValueOnce(new Error('Lookup failed')).mockImplementation(() => new Promise(resolve => { finishStatus = resolve; }));
    const handle = command({ status });
    await expect(app.run(handle, 'Send')).rejects.toThrow('Lookup failed');
    const first = app.checkCommand('original-id');
    const second = app.checkCommand('original-id');
    expect(first).toBe(second);
    await Promise.resolve();
    expect(status).toHaveBeenCalledTimes(2);
    finishStatus({ status: 'running' });
    await first;
    let finishAcceptance!: (value: { status: string }) => void;
    const late = command({ accepted: () => new Promise(resolve => { finishAcceptance = resolve; }) });
    const onAccepted = vi.fn();
    const waiting = app.run(late, 'Late send', onAccepted);
    await app.connect('http://localhost:9000');
    finishAcceptance({ status: 'running' });
    await expect(waiting).rejects.toThrow();
    expect(onAccepted).not.toHaveBeenCalled();
    expect(app.getSnapshot().commands).toEqual([]);
    app.dispose();
  });
  it('treats disposal as local detachment and never calls cancellation', async () => {
    const app = runtime(); await app.connect();
    const handle = command({ accepted: ({ signal }: { signal: AbortSignal }) => new Promise((_, reject) => signal.addEventListener('abort', () => reject(signal.reason), { once: true })), cancel: vi.fn() });
    const accepted = vi.fn();
    const waiting = app.run(handle, 'Send', accepted, 'draft');
    app.dispose();
    await expect(waiting).rejects.toThrow();
    expect(handle.cancel).not.toHaveBeenCalled();
    expect(accepted).not.toHaveBeenCalled();
    expect(app.getSnapshot().commands[0]?.status).toBe('Submitting');
    expect(app.getSnapshot().error).toBeUndefined();
  });
});

it('an old command cannot navigate after disposal during query refresh', async () => {
  const app = runtime(); await app.connect();
  let release!: () => void;
  vi.spyOn(app.queries, 'invalidateQueries').mockImplementation(() => new Promise<void>(resolve => { release = resolve; }));
  const handle = { commandId: 'old-host', accepted: async () => ({ status: 'running' }), result: async () => ({ status: 'succeeded', result: { root_id: 'old-root' } }) } as unknown as CommandHandle<'session.create'>;
  const navigate = vi.fn();
  const waiting = app.run(handle, 'Create').then(navigate);
  for (let i = 0; i < 10 && !release; i++) await Promise.resolve();
  expect(release).toBeTypeOf('function');
  app.dispose(); release();
  await expect(waiting).rejects.toThrow();
  expect(navigate).not.toHaveBeenCalled();
});
