import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DeliveryUncertainError, RpcError, type CommandHandle, type ConnectionSnapshot, type RecoveryStorage } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { createFallbackStorage, type AppStorage } from '../src/platform';

const mocks = vi.hoisted(() => {
  const client = { clientId: 'client', configuration: { get: vi.fn(async () => ({ revision: '1', remote_hosts: [] as {id: string; name: string; url: string; runtime_id: string; connect_on_launch: boolean}[] })) }, connect: vi.fn(async () => {}), whenConnected: vi.fn(async () => {}), close: vi.fn(), subscribe: vi.fn((_listener: () => void) => vi.fn()), getSnapshot: vi.fn((): Pick<ConnectionSnapshot, 'state' | 'error'> & { info?: { runtime_id: string; connection_id: string } } => ({ state: 'connected', info: { runtime_id: 'runtime', connection_id: 'connection' } })), session: vi.fn((rootId: string) => ({ rootId })) };
  return { client, remotes: new Map<string, typeof client>(), options: [] as { recoveryStorage: RecoveryStorage }[], createView: vi.fn(() => ({ start: vi.fn(async () => {}), dispose: vi.fn(async () => {}) })), list: { start: vi.fn(async () => {}), dispose: vi.fn(async () => {}) } };
});
vi.mock('@whip/sdk', async importOriginal => ({ ...await importOriginal<typeof import('@whip/sdk')>(), createWhipClient: (options: { endpoint: string; recoveryStorage: RecoveryStorage }) => { mocks.options.push(options); return mocks.remotes.get(options.endpoint) ?? mocks.client; } }));
vi.mock('@whip/sdk/state', () => ({ createSessionView: mocks.createView, createSessionListView: () => mocks.list }));

function runtime(storage?: AppStorage) {
  const values = new Map<string, string>();
  return new AppRuntime({ defaultEndpoint: 'http://localhost:8080', storage: storage ?? { keys: () => [...values.keys()], getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } }, copy: async () => {}, download: () => {}, openExternal: () => {} });
}
beforeEach(() => { vi.clearAllMocks(); mocks.options.length = 0; mocks.remotes.clear(); mocks.client.configuration.get.mockResolvedValue({ revision: '1', remote_hosts: [] }); });
afterEach(() => { vi.useRealTimers(); });
describe('application observation ownership', () => {
  it('forgets one deleted root across views, child drafts and local presentation without touching another runtime', async () => {
    vi.useFakeTimers();
    const app = runtime(); await app.connect();
    const deleted = app.acquireView('runtime', 'root');
    const other = app.acquireView('runtime', 'keep');
    app.tabs.visit('runtime', 'root', {});
    const duplicate = app.tabs.split('root', 'right'); app.tabs.closeViews([duplicate]);
    app.tabs.open('remote', 'root');
    app.rememberSession('runtime', 'root');
    app.setDraft('runtime:root:root', 'Saved root draft'); app.flushDrafts();
    app.setDraft('runtime:root:child', 'Pending child draft');
    app.setDraft('remote:root:child', 'Other host');
    app.setDraft('runtime:keep:child', 'Other root');
    app.platform.storage.setItem('whip.web.draft.v1:runtime:root:external', 'Another window’s draft');
    app.platform.storage.setItem('whip.web.recovery.v1', 'command identities');
    app.queries.setQueryData(['detail', 'runtime', 'root'], 'deleted root');
    app.queries.setQueryData(['detail', 'remote', 'root'], 'other host');
    app.submittedInputs.add({ runtimeId: 'runtime', rootId: 'root', agentId: 'child' }, 'deleted preview');
    app.submittedInputs.add({ runtimeId: 'remote', rootId: 'root', agentId: 'child' }, 'other preview');
    const changed = vi.fn(); app.subscribeDraft('runtime:root:child', changed);
    app.forgetSession('runtime', 'root');
    expect(deleted.view.dispose).toHaveBeenCalledTimes(1);
    expect(other.view.dispose).not.toHaveBeenCalled();
    expect(app.tabs.workspace().tabs.map(tab => tab.runtimeId)).toEqual(['remote']);
    expect(app.tabs.workspace().closed).toEqual([]);
    expect(app.lastSession()).toBeUndefined();
    expect(app.draft('runtime:root:root')).toBe('');
    expect(app.draft('runtime:root:child')).toBe('');
    expect(app.draft('runtime:root:external')).toBe('');
    expect(app.draft('remote:root:child')).toBe('Other host');
    expect(app.draft('runtime:keep:child')).toBe('Other root');
    expect(changed).toHaveBeenCalledTimes(1);
    expect(app.hasSessionDraft('runtime', 'root')).toBe(false);
    expect(app.platform.storage.getItem('whip.web.recovery.v1')).toBe('command identities');
    expect(app.queries.getQueryData(['detail', 'runtime', 'root'])).toBeUndefined();
    expect(app.queries.getQueryData(['detail', 'remote', 'root'])).toBe('other host');
    expect(app.submittedInputs.getSnapshot().map(input => input.runtimeId)).toEqual(['remote']);
    deleted.release(); vi.advanceTimersByTime(30_001);
    expect(deleted.view.dispose).toHaveBeenCalledTimes(1);
    app.dispose();
  });
  it('keeps a deleted draft tombstone visible as unsaved when device storage refuses removal', () => {
    const app = runtime();
    app.setDraft('runtime:root:child', 'Saved draft'); app.flushDrafts();
    app.setDraft('remote:root:child', 'Keep'); app.flushDrafts();
    const remove = vi.spyOn(app.platform.storage, 'removeItem').mockImplementation(() => { throw new Error('Storage denied'); });
    app.forgetSession('runtime', 'root');
    expect(app.draft('runtime:root:child')).toBe('');
    expect(app.draft('remote:root:child')).toBe('Keep');
    expect(app.hasUnsavedDrafts()).toBe(true);
    expect(app.getSnapshot().error).toContain('could not be saved');
    remove.mockRestore();
    expect(app.flushDrafts()).toEqual({ saved: true });
    expect(app.platform.storage.getItem('whip.web.draft.v1:runtime:root:child')).toBeNull();
    app.dispose();
  });
  it('shares a view across StrictMode lease release/reacquisition and disposes once', async () => {
    vi.useFakeTimers();
    const app = runtime(); await app.connect();
    const first = app.acquireView('runtime', 'root'); first.release();
    const second = app.acquireView('runtime', 'root');
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
    const leases = ['a', 'b', 'c', 'd'].map(id => app.acquireView('runtime', id));
    expect(() => app.acquireView('runtime', 'e')).toThrow('Four session views');
    for (const lease of leases) expect(lease.view.dispose).not.toHaveBeenCalled();
    app.dispose();
  });
  it('isolates equal root and command IDs across hosts and detaches only their owning connection', async () => {
    const remote = { ...mocks.client, close: vi.fn(), getSnapshot: vi.fn(() => ({ state: 'connected' as const, info: { runtime_id: 'remote', connection_id: 'remote-connection' } })) };
    mocks.remotes.set('http://remote.test', remote);
    mocks.client.configuration.get.mockResolvedValue({ revision: '1', remote_hosts: [{ id: 'remote-profile', name: 'Remote', url: 'http://remote.test', runtime_id: 'remote', connect_on_launch: true }] });
    const app = runtime(); await app.connect(); await app.connections.refreshProfiles();
    const localView = app.acquireView('runtime', 'same');
    const remoteView = app.acquireView('remote', 'same');
    expect(localView.view).not.toBe(remoteView.view);
    app.queries.setQueryData(['detail', 'runtime', 'same'], 'local data');
    app.queries.setQueryData(['detail', 'remote', 'same'], 'remote data');
    const handle = (client: typeof mocks.client, runtimeId: string) => command({
      client, commandId: 'same', record: { runtimeId, clientId: 'client' },
      accepted: async () => ({ status: 'running' }),
    });
    await app.run(handle(mocks.client, 'runtime'), 'Local command');
    expect(app.queries.getQueryState(['detail', 'runtime', 'same'])?.isInvalidated).toBe(true);
    expect(app.queries.getQueryState(['detail', 'remote', 'same'])?.isInvalidated).toBe(false);
    await app.run(handle(remote, 'remote'), 'Remote command');
    expect(app.getSnapshot().commands.map(notice => notice.runtimeId)).toEqual(['runtime', 'remote']);
    app.setDraft('runtime:same:root', 'local draft'); app.setDraft('remote:same:root', 'remote draft');
    app.connections.disconnect('local');
    expect(localView.view.dispose).toHaveBeenCalledTimes(1);
    expect(remoteView.view.dispose).not.toHaveBeenCalled();
    expect(remote.close).not.toHaveBeenCalled();
    expect(app.queries.getQueryData(['detail', 'remote', 'same'])).toBe('remote data');
    expect(app.draft('runtime:same:root')).toBe('local draft');
    expect(app.draft('remote:same:root')).toBe('remote draft');
    app.dispose();
  });
  it('evicts the least recently used inactive view instead of the first-created view', async () => {
    const app = runtime(); await app.connect();
    const leases = ['a', 'b', 'c', 'd'].map(id => app.acquireView('runtime', id));
    leases.forEach(lease => lease.release());
    app.acquireView('runtime', 'a').release(); app.acquireView('runtime', 'e').release();
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
    const handle = { client: mocks.client, record: { runtimeId: 'runtime', clientId: 'client' }, commandId: 'id', accepted: async () => ({ status: 'running' }), result: async () => ({ status: 'interrupted', failure: { message: 'Daemon restarted' } }) } as unknown as CommandHandle<'submit'>;
    await expect(app.run(handle, 'Send')).rejects.toThrow('Daemon restarted');
    expect(app.getSnapshot().commands[0]?.status).toBe('interrupted');
    app.dispose();
  });
  it('validates a host address before detaching an existing connection', async () => {
    const app = runtime(); await app.connect();
    await expect(app.connections.save({ id: 'bad', name: 'Invalid', url: 'javascript:alert(1)', connect_on_launch: true })).rejects.toThrow();
    expect(mocks.client.close).not.toHaveBeenCalled();
    app.dispose();
  });
  it('does not schedule a retired lease timer after disposing the application', async () => {
    vi.useFakeTimers();
    const app = runtime(); await app.connect();
    const lease = app.acquireView('runtime', 'root');
    app.dispose(); lease.release();
    expect(vi.getTimerCount()).toBe(0);
    expect(lease.view.dispose).toHaveBeenCalledTimes(1);
  });
  it('reports failure to persist a browser client identity before connecting', async () => {
    const app = runtime();
    vi.spyOn(app.platform.storage, 'setItem').mockImplementation(() => { throw new Error('Storage denied'); });
    await expect(app.connect()).rejects.toThrow('Storage denied');
    expect(app.getSnapshot().home?.error).toBe('Storage denied');
    expect(mocks.client.connect).not.toHaveBeenCalled();
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
    expect(save).toHaveBeenCalledTimes(2); // Text plus bounded revision metadata share the debounce.
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
  return { client: mocks.client, record: { runtimeId: 'runtime', clientId: 'client' }, commandId: 'original-id', accepted: vi.fn(async () => { throw new DeliveryUncertainError('original-id'); }), status: vi.fn(async () => ({ status: 'running' })), result: vi.fn(async () => ({ status: 'succeeded' })), retry: vi.fn(async () => ({ status: 'running' })), ...overrides } as unknown as CommandHandle<'submit'>;
}
const noticeId = (id: string) => JSON.stringify(['runtime', 'client', id]);
const missing = () => new RpcError({ code: -32011, message: 'Command not found', data: { kind: 'command_not_found' } });

describe('uncertain command acceptance', () => {
  it('reconciles a child preview after an explicit retry finishes outside the original composer wait', async () => {
    const app = runtime(); await app.connect();
    const id = app.submittedInputs.add({ runtimeId: 'runtime', rootId: 'root', agentId: 'child' }, 'Child message');
    const handle = command({
      commandId: id,
      status: vi.fn(async () => { throw missing(); }),
      retry: vi.fn(async () => ({ command_id: id, ingress_seq: '10', status: 'running' })),
      result: vi.fn(async () => ({ command_id: id, ingress_seq: '10', status: 'succeeded', result: { inbox_seq: '20' } })),
    });
    await expect(app.run(handle, 'Message child')).rejects.toThrow('Command not found');
    expect(app.submittedInputs.getSnapshot()[0]).toMatchObject({ id, accepted: false });
    await app.retryCommand(noticeId(id));
    expect(app.submittedInputs.getSnapshot()[0]).toMatchObject({ id, accepted: true, inboxSeq: '20' });
    app.dispose();
    expect(app.submittedInputs.getSnapshot()).toHaveLength(0);
  });
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
    await app.retryCommand(noticeId('original-id'));
    expect(handle.retry).toHaveBeenCalledTimes(1);
    expect(accepted).toHaveBeenCalledTimes(1);
    expect(app.getSnapshot().commands[0]?.commandId).toBe('original-id');
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
    await expect(app.retryCommand(noticeId('original-id'))).rejects.toThrow('only after');
    await app.checkCommand(noticeId('original-id'));
    expect(handle.retry).not.toHaveBeenCalled();
    expect(accepted).toHaveBeenCalledTimes(1);
    app.dispose();
  });
  it('keeps unresolved delivery visible while bounding completed notices', async () => {
    const app = runtime(); await app.connect();
    await expect(app.run(command({ status: async () => { throw missing(); } }), 'Send', undefined, 'draft')).rejects.toThrow();
    for (let i = 0; i < 40; i++) await app.run(command({ commandId: `done-${i}`, accepted: async () => ({ status: 'running' }) }), 'Rename');
    expect(app.getSnapshot().commands).toHaveLength(33);
    expect(app.getSnapshot().commands.find(item => item.commandId === 'original-id')?.delivery).toBe('absent');
    app.dispose();
  });
  it('shares concurrent local status checks and ignores old-host acceptance', async () => {
    const app = runtime(); await app.connect();
    let finishStatus!: (value: { status: string }) => void;
    const status = vi.fn().mockRejectedValueOnce(new Error('Lookup failed')).mockImplementation(() => new Promise(resolve => { finishStatus = resolve; }));
    const handle = command({ status });
    await expect(app.run(handle, 'Send')).rejects.toThrow('Lookup failed');
    const first = app.checkCommand(noticeId('original-id'));
    const second = app.checkCommand(noticeId('original-id'));
    expect(first).toBe(second);
    await Promise.resolve();
    expect(status).toHaveBeenCalledTimes(2);
    finishStatus({ status: 'running' });
    await first;
    let finishAcceptance!: (value: { status: string }) => void;
    const late = command({ accepted: () => new Promise(resolve => { finishAcceptance = resolve; }) });
    const onAccepted = vi.fn();
    const waiting = app.run(late, 'Late send', onAccepted);
    app.connections.disconnect('local');
    finishAcceptance({ status: 'running' });
    await expect(waiting).rejects.toThrow();
    expect(onAccepted).not.toHaveBeenCalled();
    expect(app.getSnapshot().commands.at(-1)?.status).toBe('Submitting');
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
  it('does not turn a detached command observer into a global error when its caller reports the rejection', async () => {
    const app = runtime(); await app.connect();
    const handle = command({
      accepted: async () => ({ status: 'running' }),
      result: ({ signal }: { signal: AbortSignal }) => new Promise((_, reject) => signal.addEventListener('abort', () => reject(signal.reason), { once: true })),
      cancel: vi.fn(),
    });
    const accepted = vi.fn();
    const waiting = app.run(handle, 'Send', accepted).catch(error => app.report(error));
    await vi.waitFor(() => expect(accepted).toHaveBeenCalledOnce());
    app.connections.disconnect('local');
    await waiting;
    expect(app.getSnapshot().error).toBeUndefined();
    expect(handle.cancel).not.toHaveBeenCalled();
    app.report(new DOMException('Provider timed out', 'TimeoutError'));
    expect(app.getSnapshot().error).toContain('Provider timed out');
    app.report(new Error('Command failed'));
    expect(app.getSnapshot().error).toBe('Command failed');
    app.dispose();
  });
});

it('an old command cannot navigate after disposal during query refresh', async () => {
  const app = runtime(); await app.connect();
  let release!: () => void;
  vi.spyOn(app.queries, 'invalidateQueries').mockImplementation(() => new Promise<void>(resolve => { release = resolve; }));
  const handle = { client: mocks.client, record: { runtimeId: 'runtime', clientId: 'client' }, commandId: 'old-host', accepted: async () => ({ status: 'running' }), result: async () => ({ status: 'succeeded', result: { root_id: 'old-root' } }) } as unknown as CommandHandle<'session.create'>;
  const navigate = vi.fn();
  const waiting = app.run(handle, 'Create').then(navigate);
  for (let i = 0; i < 10 && !release; i++) await Promise.resolve();
  expect(release).toBeTypeOf('function');
  app.dispose(); release();
  await expect(waiting).rejects.toThrow();
  expect(navigate).not.toHaveBeenCalled();
});

describe('split pane observation and draft consumers', () => {
  it('reference-counts duplicate child history readers and survives StrictMode reacquisition', async () => {
    const app = runtime();
    const view = { session: { rootId: 'root' }, openAgent: vi.fn(async () => {}), closeAgent: vi.fn() };
    const typed = view as unknown as Parameters<AppRuntime['acquireAgent']>[0];
    const first = app.acquireAgent(typed, 'child');
    const second = app.acquireAgent(typed, 'child');
    const other = app.acquireAgent(typed, 'other');
    expect(view.openAgent.mock.calls).toEqual([['child'], ['other']]);
    first(); first(); await Promise.resolve();
    expect(view.closeAgent).not.toHaveBeenCalled();
    second();
    const reacquired = app.acquireAgent(typed, 'child');
    await Promise.resolve();
    expect(view.openAgent).toHaveBeenCalledTimes(2);
    expect(view.closeAgent).not.toHaveBeenCalled();
    reacquired(); await Promise.resolve();
    expect(view.closeAgent).toHaveBeenCalledExactlyOnceWith('child');
    other(); await Promise.resolve();
    expect(view.closeAgent).toHaveBeenLastCalledWith('other');
    app.acquireAgent(typed, 'root')();
    expect(view.openAgent).toHaveBeenCalledTimes(2);
    app.dispose();
  });
  it('notifies all views of one recipient while keeping other drafts independent', () => {
    const app = runtime();
    const first = vi.fn(), duplicate = vi.fn(), other = vi.fn();
    const off = app.subscribeDraft('host:root:child', first);
    const offDuplicate = app.subscribeDraft('host:root:child', duplicate);
    const offOther = app.subscribeDraft('host:root:other', other);
    app.setDraft('host:root:child', 'shared');
    expect(first).toHaveBeenCalledTimes(1); expect(duplicate).toHaveBeenCalledTimes(1); expect(other).not.toHaveBeenCalled();
    off(); off(); app.setDraft('host:root:child', 'edited');
    expect(first).toHaveBeenCalledTimes(1); expect(duplicate).toHaveBeenCalledTimes(2);
    expect(app.draft('host:root:child')).toBe('edited');
    app.setDraft('host:root:other', 'independent');
    expect(other).toHaveBeenCalledTimes(1); expect(duplicate).toHaveBeenCalledTimes(2);
    offDuplicate(); offOther(); app.dispose();
  });
});

it('notifies recipient subscribers after discarding saved and pending drafts', () => {
  const app = runtime();
  app.setDraft('host:root:saved', 'saved'); app.flushDrafts();
  app.setDraft('host:root:pending', 'pending');
  const saved = vi.fn(), pending = vi.fn(), empty = vi.fn();
  app.subscribeDraft('host:root:saved', saved);
  app.subscribeDraft('host:root:pending', pending);
  app.subscribeDraft('host:root:empty', empty);
  app.discardDrafts();
  expect(saved).toHaveBeenCalledTimes(1); expect(pending).toHaveBeenCalledTimes(1);
  expect(empty).not.toHaveBeenCalled();
  expect(app.draft('host:root:saved')).toBe(''); expect(app.draft('host:root:pending')).toBe('');
  app.dispose();
});
