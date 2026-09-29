import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryObserver } from '@tanstack/react-query';
import { DeliveryError, DurableCommand, RecoveryPersistenceError, type Client, type Admission } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { createFallbackStorage, type AppStorage } from '../src/platform';

const mocks = vi.hoisted(() => ({
  client: { runtimeID: 'runtime', clientID: 'client', listProviders: vi.fn(async () => ({ revision: '1', defaults: null, routes: [] })), session: vi.fn((id: string) => ({ id })) },
  createView: vi.fn(() => ({ start: vi.fn(async () => {}), suspend: vi.fn(async () => {}), reconnect: vi.fn(async () => {}), dispose: vi.fn(async () => {}) })),
  listeners: new Set<() => void>(), catalogRevision: '1',
}));
vi.mock('@whip/sdk/state', () => ({ createSessionView: mocks.createView }));
vi.mock('../src/hosts', () => ({ HostConnections: class {
  private attached = false;
  private controller = new AbortController();
  private listeners = new Set<() => void>();
  constructor(_platform: unknown, _recovery: unknown, private effects: { connected(runtime: string, client: unknown): void; detached(client: unknown, runtime: string, options: { recovering: boolean }): void }) {}
  home() { return { id: 'local', local: true, runtimeId: 'runtime', state: this.attached ? 'connected' : 'closed', client: this.attached ? mocks.client : undefined,
    list: { getSnapshot: () => ({ revision: mocks.catalogRevision }), subscribe: (callback: () => void) => { mocks.listeners.add(callback); return () => mocks.listeners.delete(callback); } } }; }
  host(id: string) { return id === 'runtime' ? this.home() : undefined; }
  getSnapshot = () => ({ hosts: [this.home()], profilesReady: true, selectedId: 'local' });
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => this.listeners.delete(listener); };
  isAttached(client: unknown) { return this.attached && client === mocks.client; }
  signal(client: unknown) { if (!this.isAttached(client)) throw new Error('Detached'); return this.controller.signal; }
  async connect() { this.attached = true; this.controller = new AbortController(); this.effects.connected('runtime', mocks.client); for (const listener of this.listeners) listener(); }
  disconnect(_id: string, recovering = false) { if (!this.attached) return; this.attached = false; this.controller.abort(); this.effects.detached(mocks.client, 'runtime', { recovering }); for (const listener of this.listeners) listener(); }
  dispose() { this.disconnect('local'); this.listeners.clear(); }
} }));
function runtime(storage?: AppStorage) {
  const values = new Map<string, string>();
  return new AppRuntime({ defaultEndpoint: 'http://localhost:8080', storage: storage ?? { persistent: false, keys: () => [...values.keys()], getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } }, copy: async () => {}, download: () => {}, openExternal: () => {} });
}
beforeEach(() => { vi.clearAllMocks(); mocks.listeners.clear(); mocks.catalogRevision = '1'; });
afterEach(() => vi.useRealTimers());

function admission(id = 'original-id', session = 'child', state = 'succeeded'): Admission {
  return { receipt: { identity: { client_id: 'client', request_id: id }, digest: '0'.repeat(64), input_id: 'input-' + id, deleted_at: null, created_at: '2026-01-01T00:00:00Z' },
    input: { id: 'input-' + id, session_id: session, state: 'claimed' }, turn: { id: 'turn-' + id, session_id: session, state, finished_at: state === 'running' ? null : '2026-01-01T00:00:01Z', failure: null } } as Admission;
}
function command(overrides: Record<string, unknown> = {}) {
  const id = String(overrides.id ?? 'original-id');
  return { id, method: 'sessions.submit', params: { identity: { client_id: 'client', request_id: id } },
    record: { namespace: 'whip.v4.commands', version: 1, runtimeID: 'runtime', clientID: 'client', accepted: false, request: '{}' },
    send: vi.fn(async () => { throw new DeliveryError('lost acknowledgement'); }), check: vi.fn(async () => ({ state: 'found', evidence: admission(id) })),
    wait: vi.fn(async () => admission(id)), retry: vi.fn(async () => admission(id)), forget: vi.fn(async () => {}), ...overrides } as unknown as DurableCommand<'sessions.submit'>;
}
const noticeId = (id: string) => JSON.stringify(['runtime', 'client', id]);

describe('v4 application observation ownership', () => {
  it('shares root and child views across StrictMode and disposes only after the final lease', async () => {
    vi.useFakeTimers(); const app = runtime(); await app.connect();
    const first = app.acquireView('runtime', 'root', 'child'); first.release();
    const second = app.acquireView('runtime', 'root', 'child');
    const root = app.acquireView('runtime', 'root');
    expect(first.view).toBe(second.view); expect(root.view).not.toBe(first.view);
    expect(mocks.createView).toHaveBeenCalledWith({ id: 'child' }, { maxBytes: 4 << 20, maxMessages: 256 });
    vi.advanceTimersByTime(30_001); expect(first.view.dispose).not.toHaveBeenCalled();
    second.release(); second.release(); vi.advanceTimersByTime(30_001);
    expect(first.view.dispose).toHaveBeenCalledOnce();
    app.dispose(); root.release(); expect(vi.getTimerCount()).toBe(0);
  });
  it('never evicts active views and reuses the least recently used inactive slot', async () => {
    const app = runtime(); await app.connect();
    const leases = Array.from({ length: 16 }, (_, index) => app.acquireView('runtime', 'root', `s${index}`));
    expect(() => app.acquireView('runtime', 'root', 'overflow')).toThrow('Sixteen');
    leases.forEach(lease => lease.release()); app.acquireView('runtime', 'root', 's0').release();
    app.acquireView('runtime', 'root', 'overflow').release();
    expect(leases[0]!.view.dispose).not.toHaveBeenCalled(); expect(leases[1]!.view.dispose).toHaveBeenCalledOnce(); app.dispose();
  });
  it('deletion clears root and child views/drafts/presentation only for the named root and runtime', async () => {
    const app = runtime(); await app.connect();
    const root = app.acquireView('runtime', 'root'), child = app.acquireView('runtime', 'root', 'child'), keep = app.acquireView('runtime', 'keep');
    app.setDraft('runtime:root:child', 'remove'); app.setDraft('other:root:child', 'keep'); app.flushDrafts();
    app.submittedInputs.add({ runtimeId: 'runtime', rootId: 'root', agentId: 'child' }, 'remove');
    app.forgetSession('runtime', 'root');
    expect(root.view.dispose).toHaveBeenCalledOnce(); expect(child.view.dispose).toHaveBeenCalledOnce(); expect(keep.view.dispose).not.toHaveBeenCalled();
    expect(app.draft('runtime:root:child')).toBe(''); expect(app.draft('other:root:child')).toBe('keep'); expect(app.submittedInputs.getSnapshot()).toHaveLength(0); app.dispose();
  });
  it('catalog head changes invalidate off-page metadata without another title poll/cache', async () => {
    const app = runtime(); await app.connect();
    const local = vi.fn(async () => ({ title: 'Local' })), other = vi.fn(async () => ({ title: 'Other' }));
    const a = new QueryObserver(app.queries, { queryKey: ['session-tab-summaries', 'runtime'], queryFn: local });
    const b = new QueryObserver(app.queries, { queryKey: ['session-tab-summaries', 'other'], queryFn: other });
    const offA = a.subscribe(() => {}), offB = b.subscribe(() => {}); await Promise.all([a.refetch(), b.refetch()]);
    const countA = local.mock.calls.length, countB = other.mock.calls.length;
    mocks.catalogRevision = '2'; for (const listener of mocks.listeners) listener();
    await vi.waitFor(() => expect(local.mock.calls.length).toBe(countA + 1)); expect(other.mock.calls.length).toBe(countB);
    offA(); offB(); app.dispose();
  });
  it('retains only host query metadata after its consumer leaves', async () => {
    vi.useFakeTimers(); const app = runtime(); const keys = ['provider-list', 'runtime-configuration', 'provider-catalogs', 'definitions'];
    for (const key of [...keys, 'detail']) { const observer = new QueryObserver(app.queries, { queryKey: [key, 'runtime'], queryFn: async () => ({ value: true }) }); const off = observer.subscribe(() => {}); await observer.refetch(); off(); }
    await vi.advanceTimersByTimeAsync(1); expect(app.queries.getQueryData(['detail', 'runtime'])).toBeUndefined();
    for (const key of keys) expect(app.queries.getQueryData([key, 'runtime'])).toBeDefined();
    await vi.advanceTimersByTimeAsync(5 * 60_000); for (const key of keys) expect(app.queries.getQueryData([key, 'runtime'])).toBeUndefined(); app.dispose();
  });
});

describe('draft persistence and bounded storage', () => {
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
  it('evicts the earliest unowned drafts at the count and total bounds; a single oversized draft is still refused', () => {
    const app = runtime();
    for (let i = 0; i < 32; i++) app.setDraft(`runtime:root:${i}`, `draft ${i}`);
    app.setDraft('runtime:root:overflow', 'new');
    expect(app.draft('runtime:root:overflow')).toBe('new');
    expect(app.draft('runtime:root:0')).toBe('');
    expect(app.draft('runtime:root:1')).toBe('draft 1');
    expect(() => app.setDraft('runtime:root:1', 'é'.repeat(128 * 1024 + 1))).toThrow('256 KiB');
    expect(app.draft('runtime:root:1')).toBe('draft 1');
    app.dispose();
    const total = runtime();
    for (let i = 0; i < 3; i++) total.setDraft(`runtime:root:${i}`, 'a'.repeat(256 * 1024));
    total.setDraft('runtime:root:3', 'a'.repeat(256 * 1024));
    expect(total.draft('runtime:root:3')).toHaveLength(256 * 1024);
    expect(total.draft('runtime:root:0')).toBe('');
    total.dispose();
  });
  it('evicts drafts of tabs that are no longer open before drafts of open or recently closed tabs', () => {
    const app = runtime();
    app.tabs.open('runtime', 'kept'); app.setDraft('runtime:kept:root', 'Keep me');
    const draft = app.tabs.openNew(); app.setDraft(`new:${draft.id}:prompt`, 'Keep the New Chat too');
    const closed = app.tabs.open('runtime', 'closed'); app.tabs.closeViews([closed]); app.setDraft('runtime:closed:root', 'Reopenable');
    for (let i = 0; i < 31; i++) app.setDraft(`runtime:gone:${i}`, `orphan ${i}`);
    expect(app.draft('runtime:kept:root')).toBe('Keep me');
    expect(app.draft(`new:${draft.id}:prompt`)).toBe('Keep the New Chat too');
    expect(app.draft('runtime:closed:root')).toBe('Reopenable');
    expect(app.draft('runtime:gone:0')).toBe(''); expect(app.draft('runtime:gone:1')).toBe('');
    expect(app.draft('runtime:gone:30')).toBe('orphan 30');
    app.dispose();
  });
  it('retires first-message journals and their frozen payload copies from earlier builds', () => {
    const values = new Map<string, string>([
      ['whip.web.welcome.v2:abc', '{}'], ['whip.web.welcome.v1:host', '{}'], ['whip.web.welcome-import.v1:host', 'legacy-welcome-host'],
      ['whip.web.draft.v1:new:abc:submission', 'frozen'], ['whip.web.draft-revision.v1:new:abc:submission', 'r1'],
      ['whip.web.draft.v1:host:welcome:prompt', 'old prompt'], ['whip.web.draft.v1:runtime:root:root', 'kept'],
    ]);
    const app = runtime({ keys: () => [...values.keys()], getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value); }, removeItem: key => { values.delete(key); } });
    expect([...values.keys()]).toEqual(['whip.web.draft.v1:runtime:root:root']);
    expect(app.draft('runtime:root:root')).toBe('kept');
    app.dispose();
  });
  it('keeps another writer\'s overflow within the bound and still allows explicit clearing', () => {
    const app = runtime();
    for (let index = 0; index < 33; index++) app.platform.storage.setItem(`whip.web.draft.v1:runtime:root:${index}`, 'External draft');
    app.setDraft('runtime:root:new', 'Additional'); app.flushDrafts();
    expect(app.platform.storage.keys().filter(key => key.startsWith('whip.web.draft.v1:'))).toHaveLength(32);
    expect(app.platform.storage.getItem('whip.web.draft.v1:runtime:root:new')).toBe('Additional');
    app.setDraft('runtime:root:5', ''); app.flushDrafts();
    expect(app.platform.storage.getItem('whip.web.draft.v1:runtime:root:5')).toBeNull();
    app.setDraft('runtime:root:6', 'Updated'); app.flushDrafts();
    expect(app.platform.storage.getItem('whip.web.draft.v1:runtime:root:6')).toBe('Updated');
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


describe('v4 command acceptance and local lifetimes', () => {
  it('reconciles a lost acknowledgement once without sending again and retains canonical child input identity', async () => {
    const app = runtime(); await app.connect(); const id = app.submittedInputs.add({ runtimeId: 'runtime', rootId: 'root', agentId: 'child', clientId: 'client' }, 'Text');
    const handle = command({ id }); const accepted = vi.fn();
    await app.run(handle, 'Send', accepted, 'draft'); expect(handle.send).toHaveBeenCalledOnce(); expect(handle.retry).not.toHaveBeenCalled();
    expect(handle.check).toHaveBeenCalledOnce(); expect(accepted).toHaveBeenCalledOnce(); expect(handle.forget).toHaveBeenCalledOnce();
    expect(app.submittedInputs.getSnapshot()[0]).toMatchObject({ id, inputId: 'input-' + id, accepted: true }); app.dispose();
  });
  it('retains confirmed absence for explicit same-handle retry', async () => {
    const app = runtime(); await app.connect(); const handle = command({ check: vi.fn(async () => ({ state: 'missing' })) }); const accepted = vi.fn();
    await expect(app.run(handle, 'Send', accepted)).rejects.toThrow('no receipt'); expect(app.getSnapshot().commands[0]?.delivery).toBe('absent');
    expect(handle.retry).not.toHaveBeenCalled(); await app.retryCommand(noticeId(handle.id)); expect(handle.retry).toHaveBeenCalledOnce(); expect(accepted).toHaveBeenCalledOnce(); app.dispose();
  });
  it('never equates identity-only evidence or generic lookup errors with absence', async () => {
    const app = runtime(); await app.connect(); const check = vi.fn().mockRejectedValueOnce(new Error('Database unavailable')).mockResolvedValueOnce({ state: 'identity_only', evidence: admission() }).mockResolvedValue({ state: 'found', evidence: admission() });
    const handle = command({ check }); await expect(app.run(handle, 'Send')).rejects.toThrow('Database unavailable');
    await expect(app.retryCommand(noticeId(handle.id))).rejects.toThrow('only after'); await expect(app.checkCommand(noticeId(handle.id))).rejects.toThrow('exact original');
    expect(app.getSnapshot().commands[0]?.delivery).toBe('uncertain'); await app.checkCommand(noticeId(handle.id)); expect(handle.retry).not.toHaveBeenCalled(); app.dispose();
  });
  it('keeps authoritative interruption distinct from uncertain delivery', async () => {
    const app = runtime(); await app.connect(); const result = admission(); result.turn!.state = 'interrupted'; result.turn!.failure = 'Process restarted';
    await expect(app.run(command({ send: async () => result, wait: async () => result }), 'Send')).rejects.toThrow('Process restarted');
    expect(app.getSnapshot().commands[0]?.status).toBe('interrupted'); expect(app.getSnapshot().commands[0]?.delivery).toBeUndefined(); app.dispose();
  });
  it('known host acceptance survives failed local persistence and clears the authored draft once', async () => {
    const app = runtime(); await app.connect(); const accepted = vi.fn(); const handle = command();
    const error = new RecoveryPersistenceError({ ...handle.record, accepted: true }, admission(), new Error('Disk full'));
    Object.assign(handle, { send: async () => { throw error; } }); await expect(app.run(handle, 'Send', accepted)).rejects.toBe(error);
    expect(accepted).toHaveBeenCalledOnce(); expect(handle.retry).not.toHaveBeenCalled(); expect(app.getSnapshot().commands[0]?.status).toContain('Accepted'); app.dispose();
  });
  it('bounds completed notices while keeping unresolved requests visible', async () => {
    const app = runtime(); await app.connect(); await expect(app.run(command({ check: async () => ({ state: 'missing' }) }), 'Uncertain')).rejects.toThrow();
    for (let i = 0; i < 40; i++) await app.run(command({ id: `done-${i}`, send: async () => admission(`done-${i}`) }), 'Send');
    expect(app.getSnapshot().commands).toHaveLength(33); expect(app.getSnapshot().commands[0]?.delivery).toBe('absent'); app.dispose();
  });
  it('shares concurrent explicit checks and ignores acceptance from a detached host', async () => {
    const app = runtime(); await app.connect(); let finish!: (value: unknown) => void;
    const handle = command({ check: vi.fn().mockRejectedValueOnce(new Error('Offline')).mockImplementation(() => new Promise(resolve => { finish = resolve; })) });
    await expect(app.run(handle, 'Send')).rejects.toThrow('Offline'); const first = app.checkCommand(noticeId(handle.id)); const second = app.checkCommand(noticeId(handle.id)); expect(first).toBe(second);
    finish({ state: 'found', evidence: admission() }); await first;
    const accepted = vi.fn(); const late = command({ send: () => new Promise(resolve => { finish = resolve; }) }); const waiting = app.run(late, 'Late', accepted);
    app.connections.disconnect('local'); finish(admission()); await expect(waiting).rejects.toThrow(); expect(accepted).not.toHaveBeenCalled(); app.dispose();
  });
  it('disposal cancels observation only and suppresses abort errors from the global banner', async () => {
    const app = runtime(); await app.connect(); const accepted = vi.fn(), cancel = vi.fn();
    const handle = command({ send: async () => admission(), wait: ({ signal }: { signal: AbortSignal }) => new Promise((_, reject) => signal.addEventListener('abort', () => reject(signal.reason), { once: true })), cancel });
    const pending = app.run(handle, 'Send', accepted).catch(error => app.report(error)); await vi.waitFor(() => expect(accepted).toHaveBeenCalledOnce());
    app.dispose(); await pending; expect(cancel).not.toHaveBeenCalled(); expect(app.getSnapshot().error).toBeUndefined();
  });
  it('prevents navigation from an old command after disposal during query refresh', async () => {
    const app = runtime(); await app.connect(); let release!: () => void;
    vi.spyOn(app.queries, 'invalidateQueries').mockImplementation(() => new Promise(resolve => { release = resolve; })); const navigate = vi.fn();
    const waiting = app.run(command({ send: async () => admission() }), 'Send').then(navigate); await vi.waitFor(() => expect(release).toBeTypeOf('function'));
    app.dispose(); release(); await expect(waiting).rejects.toThrow(); expect(navigate).not.toHaveBeenCalled();
  });
});


it.each(['claimed', 'uncertain', 'succeeded'] as const)('keeps workspace %s distinct from input acceptance and retains unresolved tracking', async state => {
  const app = runtime(); await app.connect();
  const result = { action: { id: 'action', session_id: 'child', snapshot_id: 'snapshot', kind: 'restore', state, failure: null }, snapshot: { id: 'snapshot' } };
  const handle = command({ method: 'workspace.restore', params: { session_id: 'child', snapshot_id: 'snapshot', action_id: 'action' }, send: vi.fn(async () => result) });
  const accepted = vi.fn();
  if (state === 'succeeded') {
    await expect(app.run(handle, 'Restore', accepted)).resolves.toBe(result);
    expect(handle.forget).toHaveBeenCalledOnce();
  } else {
    await expect(app.run(handle, 'Restore', accepted)).rejects.toThrow(state);
    expect(handle.forget).not.toHaveBeenCalled();
    expect(app.getSnapshot().commands[0]?.status).toBe(`Accepted · workspace ${state}`);
  }
  expect(accepted).toHaveBeenCalledOnce(); expect(handle.wait).not.toHaveBeenCalled(); expect(handle.retry).not.toHaveBeenCalled(); app.dispose();
});


it('keeps aborted possibly-sent input unresolved and explicitly checks the exact record after reconnection', async () => {
  const app = runtime(); await app.connect();
  const id = app.submittedInputs.add({ runtimeId: 'runtime', rootId: 'root', agentId: 'child', clientId: 'client' }, 'Retained authored work');
  const accepted = vi.fn();
  const handle = command({ id, send: vi.fn(async ({ signal }: { signal: AbortSignal }) => {
    app.connections.disconnect('local', true);
    signal.throwIfAborted();
    throw new Error('unreachable');
  }) });
  await expect(app.run(handle, 'Send', accepted, 'draft')).rejects.toThrow(/may have reached/);
  expect(app.getSnapshot().commands[0]).toMatchObject({ delivery: 'uncertain', draftKey: 'draft' });
  expect(app.submittedInputs.getSnapshot()[0]?.id).toBe(id);
  expect(accepted).not.toHaveBeenCalled(); expect(handle.check).not.toHaveBeenCalled(); expect(handle.retry).not.toHaveBeenCalled();
  await expect(app.checkCommand(noticeId(id))).rejects.toThrow('Reconnect');
  const previous = mocks.client;
  mocks.client = { ...previous };
  const recovered = command({ id });
  const recover = vi.spyOn(DurableCommand, 'recover').mockReturnValue(recovered as never);
  try {
    await app.connect();
    expect(recover).not.toHaveBeenCalled(); expect(recovered.send).not.toHaveBeenCalled();
    await app.checkCommand(noticeId(id));
    expect(recover).toHaveBeenCalledWith(mocks.client, handle.record, { journal: app.recovery });
    expect(recovered.check).toHaveBeenCalledOnce(); expect(recovered.send).not.toHaveBeenCalled(); expect(recovered.retry).not.toHaveBeenCalled();
    expect(accepted).toHaveBeenCalledOnce(); expect(app.getSnapshot().commands[0]?.status).toBe('succeeded');
  } finally { app.dispose(); recover.mockRestore(); mocks.client = previous; }
});
