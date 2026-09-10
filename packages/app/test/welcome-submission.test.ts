import { expect, it, vi } from 'vitest';
import { CommandHandle, RpcError, type RecoveryRecord, type WhipClient } from '@whip/sdk';
import { AppRuntime } from '../src/runtime';
import { WelcomeSubmissions, welcomeDraftKey } from '../src/welcome-submission';
import { selectedSessionTab, TAB_STORAGE_KEY } from '../src/session-tabs';
import { createFallbackStorage, type AppStorage } from '../src/platform';

// Journal tests deliberately keep promotion separate from protocol/recovery behavior.
function journalRuntime(platform: ConstructorParameters<typeof AppRuntime>[0]) {
  const app = new AppRuntime(platform);
  Object.defineProperty(app, 'welcome', { value: new WelcomeSubmissions(app) });
  return app;
}

function fixture(standalone = true, windowOverride?: AppStorage | null) {
  const values = new Map<string, string>();
  const storage = { keys: () => [...values.keys()], getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value); }, removeItem: (key: string) => { values.delete(key); } };
  const windowStorage = windowOverride === null ? undefined : windowOverride ?? storage;
  const makeRuntime = standalone ? journalRuntime : (platform: ConstructorParameters<typeof AppRuntime>[0]) => new AppRuntime(platform);
  const app = makeRuntime({ windowStorage: standalone ? undefined : windowStorage, defaultEndpoint: 'http://localhost:8080', storage, copy: async () => {}, download: async () => {}, openExternal: async () => {} });
  const outcomes = new Map<string, any>();
  const sends: any[] = [];
  let failStatus = false;
  let lose: 'session.create' | 'submit' | undefined;
  let admit = true;
  const raw = {
    clientId: 'client', requireConnected: () => ({ runtime_id: 'host' }),
    getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'host' } }),
    whenConnected: async () => {}, commandOutcome: (_record: unknown, outcome: unknown) => outcome,
    callEncoded: async (_method: string, encoded: string) => {
      const request = JSON.parse(encoded); sends.push(request);
      const outcome = { command_id: request.command_id, operation: request.operation, status: 'succeeded', result: request.operation === 'session.create' ? { root_id: 'created' } : {} };
      if (admit) outcomes.set(request.command_id, outcome);
      if (request.operation === lose) throw new Error('Connection dropped');
      return outcome;
    },
    commandStatus: async (record: RecoveryRecord) => {
      if (failStatus) throw new Error('Status unavailable');
      if (!outcomes.has(record.commandId)) throw new RpcError({ code: -32000, message: 'Original request absent', data: { kind: 'command_not_found' } });
      return outcomes.get(record.commandId);
    },
    waitForCommandTick: async () => {},
    sessions: { create: vi.fn() }, session: vi.fn(), recover: vi.fn(),
  };
  const client = raw as unknown as WhipClient;
  raw.sessions.create.mockImplementation((params, options) => CommandHandle.submit(client, 'host', 'session.create', params, options));
  raw.session.mockImplementation((rootId: string) => ({ submit: (payload: { text: string }, options: { commandId: string }) => CommandHandle.submit(client, 'host', 'submit', payload, { ...options, rootId }) }));
  raw.recover.mockImplementation((record, payload) => CommandHandle.recover(client, record, payload));
  vi.spyOn(app.connections, 'isAttached').mockReturnValue(true);
  vi.spyOn(app.connections, 'signal').mockReturnValue(new AbortController().signal);
  app.setDraft(welcomeDraftKey('host'), 'Explain auth');
  return { app, client, raw, sends, values, storage, windowStorage, outcomes,
    lose(operation: typeof lose, accepted = true) { lose = operation; admit = accepted; failStatus = true; },
    restore() { lose = undefined; admit = true; failStatus = false; },
  };
}

it('writes recovery before sending, creates once, sends the exact draft once, and clears after acceptance', async () => {
  const f = fixture();
  const write = vi.spyOn(f.storage, 'setItem');
  const rootId = await f.app.welcome.start('host', f.client, { cwd: '/project', model: 'kimi', provider: 'inference-net', permission_mode: 'prompt' });
  expect(rootId).toBe('created');
  expect(f.sends.map(request => request.operation)).toEqual(['session.create', 'submit']);
  expect(f.sends[0].payload.permission_mode).toBe('prompt');
  expect(f.sends[1].payload).toEqual({ text: 'Explain auth' });
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('');
  expect(f.app.welcome.get('host')?.state).toBe('accepted');
  expect(write.mock.calls.filter(([key]) => key.startsWith('whip.web.welcome')).every(([, value]) => !value.includes('Explain auth'))).toBe(true);
  await f.app.welcome.finish('host', f.app.welcome.get('host')!.create.commandId); expect(f.app.welcome.get('host')).toBeUndefined();
  f.app.dispose();
});

it('recovers an uncertain created session without allocating another and preserves later edits', async () => {
  const f = fixture(); f.lose('session.create');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Status unavailable');
  const saved = f.app.welcome.get('host')!;
  expect(f.sends).toHaveLength(1);
  f.app.setDraft(welcomeDraftKey('host'), 'A later task');
  f.restore();
  expect(await f.app.welcome.resume('host', f.client)).toBe('created');
  expect(f.sends.map(request => request.operation)).toEqual(['session.create', 'submit']);
  expect(f.sends[1].payload.text).toBe('Explain auth');
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('A later task');
  expect(f.app.welcome.get('host')?.create.commandId).toBe(saved.create.commandId);
  f.app.dispose();
});

it('freezes the execution language before admission and preserves it across default changes and retry', async () => {
  const f = fixture(); f.lose('session.create', false);
  const info = { runtime_id: 'host', default_execution_engine: 'quickjs' };
  f.raw.getSnapshot = () => ({ state: 'connected', info });
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Status unavailable');
  expect(f.app.welcome.get('host')!.params.execution_engine).toBe('quickjs');
  info.default_execution_engine = 'starlark';
  f.restore();
  await expect(f.app.welcome.resume('host', f.client)).rejects.toThrow('Original request absent');
  expect(await f.app.welcome.resume('host', f.client, true)).toBe('created');
  expect(f.sends[1]).toEqual(f.sends[0]);
  expect(f.sends[1].payload.execution_engine).toBe('quickjs');
  f.app.dispose();
});

it('retries a legacy absent creation as Starlark after the host changes its default', async () => {
  const f = fixture(); f.lose('session.create', false);
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Status unavailable');
  const journalKey = 'whip.web.welcome.v2:host';
  const legacy = JSON.parse(f.storage.getItem(journalKey)!);
  delete legacy.params.execution_engine;
  f.storage.setItem(journalKey, JSON.stringify(legacy));
  f.raw.getSnapshot = () => ({ state: 'connected', info: { runtime_id: 'host', default_execution_engine: 'quickjs' } });
  f.restore();
  await expect(f.app.welcome.resume('host', f.client)).rejects.toThrow('Original request absent');
  await f.app.welcome.resume('host', f.client, true);
  expect(f.sends[1].payload.execution_engine).toBe('starlark');
  f.app.dispose();
});

it('recovers an accepted first input after reload without sending it again', async () => {
  const f = fixture(); f.lose('submit');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Status unavailable');
  const original = f.app.welcome.get('host')!;
  expect(original.rootId).toBe('created'); expect(f.sends).toHaveLength(2);
  // A new journal owner restores identities and frozen text from persistent storage.
  const reloaded = journalRuntime({ ...f.app.platform, storage: f.storage });
  vi.spyOn(reloaded.connections, 'isAttached').mockReturnValue(true);
  vi.spyOn(reloaded.connections, 'signal').mockReturnValue(new AbortController().signal);
  f.restore();
  expect(await reloaded.welcome.resume('host', f.client)).toBe('created');
  expect(f.sends).toHaveLength(2);
  expect(reloaded.welcome.get('host')?.input.commandId).toBe(original.input.commandId);
  expect(reloaded.draft(welcomeDraftKey('host'))).toBe('');
  reloaded.dispose(); f.app.dispose();
});

it('requires authoritative absence and explicitly retries the original frozen input identity', async () => {
  const f = fixture(); f.lose('submit', false);
  // Creation must still be admitted before inducing absence for the first input.
  const original = f.raw.callEncoded;
  f.raw.callEncoded = async (method, encoded) => {
    if (JSON.parse(encoded).operation === 'session.create') { f.restore(); const result = await original(method, encoded); f.lose('submit', false); return result; }
    return original(method, encoded);
  };
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Status unavailable');
  await expect(f.app.welcome.resume('host', f.client, true)).rejects.toThrow('Check the original request');
  f.restore();
  await expect(f.app.welcome.resume('host', f.client)).rejects.toThrow('Original request absent');
  expect(f.app.welcome.get('host')?.state).toBe('absent');
  const originalId = f.sends[1].command_id;
  f.app.setDraft(welcomeDraftKey('host'), 'Newer draft');
  expect(await f.app.welcome.resume('host', f.client, true)).toBe('created');
  expect(f.sends).toHaveLength(3);
  expect(f.sends[2]).toEqual(f.sends[1]);
  expect(f.sends[2].command_id).toBe(originalId);
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('Newer draft');
  f.app.dispose();
});

it('does not send when durable journal storage fails and never retargets another host', async () => {
  const f = fixture();
  vi.spyOn(f.storage, 'setItem').mockImplementation((key, value) => { if (key.startsWith('whip.web.welcome')) throw new Error('Storage denied'); f.values.set(key, value); });
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Storage denied');
  expect(f.sends).toHaveLength(0);
  vi.spyOn(f.app.connections, 'isAttached').mockReturnValue(false);
  expect(() => f.app.welcome.start('host', f.client, { cwd: '/project' })).toThrow('original execution host');
  f.app.dispose();
});

it('preserves an edited draft even when the user returns it to the originally submitted text', async () => {
  const f = fixture(); f.lose('session.create');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Status unavailable');
  f.app.setDraft(welcomeDraftKey('host'), 'A different thought');
  f.app.setDraft(welcomeDraftKey('host'), 'Explain auth');
  f.app.flushDrafts();
  f.restore(); await f.app.welcome.resume('host', f.client);
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('Explain auth');
  f.app.dispose();
});

it('can resolve accepted input after the user discarded local text without reconstructing or resending it', async () => {
  const f = fixture(); f.lose('submit');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Status unavailable');
  f.app.discardDrafts(); f.restore();
  expect(await f.app.welcome.resume('host', f.client)).toBe('created');
  expect(f.sends).toHaveLength(2);
  f.app.dispose();
});

it('serializes first-message admission across runtime owners sharing device storage', async () => {
  const f = fixture();
  let tail = Promise.resolve();
  const transaction = vi.fn((_key: string, action: () => unknown) => {
    const result = tail.then(action);
    tail = result.then(() => {}, () => {});
    return result;
  });
  const shared = Object.assign(f.storage, { transaction });
  const second = journalRuntime({ ...f.app.platform, storage: shared });
  vi.spyOn(second.connections, 'isAttached').mockReturnValue(true);
  vi.spyOn(second.connections, 'signal').mockReturnValue(new AbortController().signal);
  const outcomes = await Promise.allSettled([
    f.app.welcome.start('host', f.client, { cwd: '/first' }),
    second.welcome.start('host', f.client, { cwd: '/second' }),
  ]);
  expect(transaction.mock.calls.length).toBeGreaterThanOrEqual(2);
  expect(outcomes.filter(outcome => outcome.status === 'fulfilled')).toHaveLength(1);
  expect(outcomes.filter(outcome => outcome.status === 'rejected')).toHaveLength(1);
  expect(f.sends.filter(request => request.operation === 'session.create')).toHaveLength(1);
  expect(f.sends.filter(request => request.operation === 'submit')).toHaveLength(1);
  expect(f.app.welcome.get('host')?.params.cwd).toBe('/first');
  second.dispose(); f.app.dispose();
});

it('does not let a late observer overwrite or retire a newer first-message journal', async () => {
  const f = fixture();
  let tail = Promise.resolve();
  const shared = Object.assign(f.storage, { transaction: (_key: string, action: () => unknown) => {
    const result = tail.then(action); tail = result.then(() => {}, () => {}); return result;
  } });
  f.lose('submit');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/original' })).rejects.toThrow('Status unavailable');
  const original = f.app.welcome.get('host')!;
  f.restore();
  const second = journalRuntime({ ...f.app.platform, storage: shared });
  vi.spyOn(second.connections, 'isAttached').mockReturnValue(true);
  vi.spyOn(second.connections, 'signal').mockReturnValue(new AbortController().signal);
  let release!: () => void; let started!: () => void;
  const blocked = new Promise<void>(resolve => { release = resolve; });
  const observing = new Promise<void>(resolve => { started = resolve; });
  const status = f.raw.commandStatus;
  let hold = true;
  f.raw.commandStatus = async record => {
    if (hold && record.commandId === original.input.commandId) {
      hold = false; started(); await blocked;
      return f.outcomes.get(record.commandId);
    }
    return status(record);
  };
  const oldObserver = second.welcome.resume('host', f.client);
  // Attach rejection observation before releasing the stale request.
  const retired = expect(oldObserver).rejects.toThrow('already retired');
  await observing;
  await f.app.welcome.resume('host', f.client);
  await f.app.welcome.finish('host', original.create.commandId);
  f.app.setDraft(welcomeDraftKey('host'), 'New logical request');
  f.lose('session.create');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/new' })).rejects.toThrow('Status unavailable');
  const next = f.app.welcome.get('host')!;
  expect(next.create.commandId).not.toBe(original.create.commandId);
  release(); await retired;
  expect(f.app.welcome.get('host')!.create.commandId).toBe(next.create.commandId);
  expect(f.app.draft('new:host:submission')).toBe('New logical request');
  await expect(second.welcome.finish('host', original.create.commandId)).rejects.toThrow('newer first message');
  expect(f.app.welcome.get('host')!.create.commandId).toBe(next.create.commandId);
  second.dispose(); f.app.dispose();
});

it('runs two drafts on one host independently while sharing same-draft double submit', async () => {
  const f = fixture();
  f.app.setDraft(welcomeDraftKey('second'), 'Second task');
  const first = f.app.welcome.start('host', f.client, { cwd: '/one' });
  expect(f.app.welcome.start('host', f.client, { cwd: '/ignored' })).toBe(first);
  await Promise.all([first, f.app.welcome.start('second', f.client, { cwd: '/two' })]);
  expect(f.sends.filter(request => request.operation === 'session.create').map(request => request.payload.cwd)).toEqual(['/one', '/two']);
  expect(f.sends.filter(request => request.operation === 'submit').map(request => request.payload.text)).toEqual(['Explain auth', 'Second task']);
  expect(f.app.welcome.list().map(item => item.draftId)).toEqual(['host', 'second']);
  expect(f.app.welcome.get('host')!.create.commandId).not.toBe(f.app.welcome.get('second')!.create.commandId);
  f.app.dispose();
});

it('freezes params at invocation and refuses recovery on a different host or client', async () => {
  const f = fixture(); f.lose('session.create');
  const params = { cwd: '/original' };
  const promise = f.app.welcome.start('host', f.client, params);
  params.cwd = '/edited';
  await expect(promise).rejects.toThrow('Status unavailable');
  expect(f.app.welcome.get('host')!.params.cwd).toBe('/original');
  const otherHost = { ...f.raw, getSnapshot: () => ({ state: 'connected', info: { runtime_id: 'other' } }) } as unknown as WhipClient;
  await expect(f.app.welcome.resume('host', otherHost)).rejects.toThrow('original execution host');
  await expect(f.app.welcome.resume('host', { ...f.raw, clientId: 'other' } as unknown as WhipClient)).rejects.toThrow('original client identity');
  expect(f.sends).toHaveLength(1);
  f.app.dispose();
});

it('calls app-owned completion without a panel and retains acceptance until promotion persists', async () => {
  const f = fixture();
  const complete = vi.fn().mockRejectedValueOnce(new Error('Layout storage denied')).mockResolvedValue(undefined);
  const service = new WelcomeSubmissions(f.app, complete);
  await expect(service.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Layout storage denied');
  const saved = service.get('host')!;
  expect(saved.state).toBe('accepted');
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('Explain auth');
  expect(f.app.draft('new:host:submission')).toBe('Explain auth');
  expect(complete).toHaveBeenCalledWith('host', 'host', 'created', saved.create.commandId);
  expect(await service.resume('host', f.client)).toBe('created');
  expect(service.get('host')).toBeUndefined();
  expect(f.app.draft('new:host:submission')).toBe('');
  expect(f.sends).toHaveLength(2);
  f.app.dispose();
});

it('keeps unresolved payloads through cleanup in another window', async () => {
  const f = fixture(); f.lose('session.create');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow();
  const second = journalRuntime({ ...f.app.platform, storage: f.storage });
  second.discardDrafts();
  expect(second.draft('new:host:submission')).toBe('Explain auth');
  expect(() => second.setDraft('new:host:submission', 'overwrite')).toThrow('Resolve the first-message');
  f.restore();
  expect(await f.app.welcome.resume('host', f.client)).toBe('created');
  expect(f.sends[1].payload.text).toBe('Explain auth');
  second.dispose(); f.app.dispose();
});

it('refuses dispatch when editable and frozen payloads exceed the shared text budget', async () => {
  const f = fixture();
  for (let i = 0; i < 31; i++) f.app.setDraft(`other:${i}`, 'work');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('32 unsent drafts');
  expect(f.sends).toHaveLength(0);
  expect(f.app.welcome.get('host')).toBeUndefined();
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('Explain auth');
  f.app.dispose();
});

it('imports legacy prompts once without sending and shares one deterministic owner', async () => {
  const f = fixture();
  f.app.setDraft('host:welcome:prompt', 'Legacy task'); f.app.flushDrafts();
  const id = await f.app.welcome.importLegacy('host');
  expect(id).toBe('legacy-welcome-host');
  expect(f.app.draft(welcomeDraftKey(id!))).toBe('Legacy task');
  expect(f.app.draft('host:welcome:prompt')).toBe('');
  expect(await f.app.welcome.importLegacy('host')).toBe(id);
  expect(f.sends).toHaveLength(0);
  expect(f.app.welcome.get(id!)).toBeUndefined();
  f.app.dispose();
});

it('imports a legacy unresolved journal with original command IDs and frozen payload', async () => {
  const f = fixture(); f.lose('submit');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow();
  const { draftId: _, ...legacy } = f.app.welcome.get('host')!;
  f.values.set('whip.web.welcome.v1:host', JSON.stringify(legacy));
  f.values.delete('whip.web.welcome.v2:host');
  f.values.set('whip.web.draft.v1:host:welcome:prompt', 'Explain auth');
  f.values.set('whip.web.draft-revision.v1:host:welcome:prompt', legacy.draftRevision);
  f.values.set('whip.web.draft.v1:host:welcome:submission', 'Explain auth');
  const id = (await f.app.welcome.importLegacy('host'))!;
  expect(f.app.welcome.get(id)!.input.commandId).toBe(legacy.input.commandId);
  expect(f.app.welcome.get(id)!.create.commandId).toBe(legacy.create.commandId);
  expect(f.values.has('whip.web.welcome.v1:host')).toBe(false);
  expect(f.app.draft(`new:${id}:submission`)).toBe('Explain auth');
  expect(f.sends).toHaveLength(2);
  f.restore(); await f.app.welcome.resume(id, f.client);
  expect(f.app.draft(welcomeDraftKey(id))).toBe('');
  expect(f.sends).toHaveLength(2);
  f.app.dispose();
});

it('does not import newer legacy edits as the submitted revision', async () => {
  const f = fixture(); f.lose('submit');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow();
  const { draftId: _, ...legacy } = f.app.welcome.get('host')!;
  f.values.set('whip.web.welcome.v1:host', JSON.stringify(legacy));
  f.values.delete('whip.web.welcome.v2:host');
  f.app.setDraft('host:welcome:prompt', 'Explain auth');
  // The text matches, but this is a later edit and must not be cleared.
  f.values.set('whip.web.draft.v1:host:welcome:submission', 'Explain auth');
  const id = (await f.app.welcome.importLegacy('host'))!;
  f.restore(); await f.app.welcome.resume(id, f.client);
  expect(f.app.draft(welcomeDraftKey(id))).toBe('Explain auth');
  f.app.dispose();
});

it('publishes journal changes and retires only a matching terminal identity', async () => {
  const f = fixture();
  const changed = vi.fn(); const stop = f.app.welcome.subscribe(changed);
  const revision = f.app.welcome.getSnapshot();
  await f.app.welcome.start('host', f.client, { cwd: '/project' });
  expect(f.app.welcome.getSnapshot()).toBeGreaterThan(revision);
  expect(changed).toHaveBeenCalled();
  await expect(f.app.welcome.finish('host', 'stale')).rejects.toThrow('newer first message');
  stop(); f.app.dispose();
});

it('keeps a terminal create failure until explicit discard and never retries a fresh identity', async () => {
  const f = fixture();
  f.raw.callEncoded = async (_method, encoded) => {
    const request = JSON.parse(encoded); f.sends.push(request);
    const outcome = { command_id: request.command_id, operation: request.operation, status: 'failed', failure: { message: 'Create denied' } };
    f.outcomes.set(request.command_id, outcome); return outcome;
  };
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Create denied');
  const saved = f.app.welcome.get('host')!;
  expect(saved.state).toBe('failed');
  await expect(f.app.welcome.resume('host', f.client, true)).rejects.toThrow('Create denied');
  expect(f.sends).toHaveLength(1);
  await f.app.welcome.finish('host', saved.create.commandId);
  expect(f.app.welcome.get('host')).toBeUndefined();
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('Explain auth');
  f.app.dispose();
});

it('refuses oversized or excessive journal admission before dispatch', async () => {
  const f = fixture();
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project', model: 'x'.repeat(8192) })).rejects.toThrow('recovery storage is full');
  expect(f.sends).toHaveLength(0);
  f.lose('session.create');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow();
  const original = f.app.welcome.get('host')!;
  for (let i = 0; i < 31; i++) f.values.set(`whip.web.welcome.v2:other-${i}`, JSON.stringify({ ...original, draftId: `other-${i}` }));
  f.app.setDraft(welcomeDraftKey('extra'), 'Extra');
  await expect(f.app.welcome.start('extra', f.client, { cwd: '/project' })).rejects.toThrow('recovery storage is full');
  expect(f.sends).toHaveLength(1);
  f.app.dispose();
});

it('leaves legacy originals intact when import cannot persist the journal', async () => {
  const f = fixture(); f.lose('submit');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow();
  const { draftId: _, ...legacy } = f.app.welcome.get('host')!;
  f.values.set('whip.web.welcome.v1:host', JSON.stringify(legacy));
  f.values.delete('whip.web.welcome.v2:host');
  f.values.set('whip.web.draft.v1:host:welcome:prompt', 'Legacy task');
  f.values.set('whip.web.draft.v1:host:welcome:submission', 'Explain auth');
  const set = f.storage.setItem;
  vi.spyOn(f.storage, 'setItem').mockImplementation((key, value) => {
    if (key.startsWith('whip.web.welcome.v2:')) throw new Error('Storage denied');
    set(key, value);
  });
  await expect(f.app.welcome.importLegacy('host')).rejects.toThrow('Storage denied');
  expect(f.values.has('whip.web.welcome.v1:host')).toBe(true);
  expect(f.app.draft('host:welcome:submission')).toBe('Explain auth');
  expect(f.app.draft('host:welcome:prompt')).toBe('Legacy task');
  expect(f.sends).toHaveLength(2);
  f.app.dispose();
});

it('app-owned completion promotes a background draft durably without stealing focus', async () => {
  const f = fixture(false);
  f.app.tabs.ensureNew('host', { runtimeId: 'host', cwd: '/project' });
  const foreground = f.app.tabs.openNew({ cwd: '/other' });
  const before = f.app.tabs.workspace();
  await f.app.welcome.start('host', f.client, { cwd: '/project' });
  expect(f.app.tabs.workspace().tabs.find(tab => tab.id === 'host')).toMatchObject({ kind: 'chat', runtimeId: 'host', rootId: 'created' });
  expect(selectedSessionTab(f.app.tabs.workspace())?.id).toBe(foreground.id);
  expect(f.app.tabs.workspace().focusedPaneId).toBe(before.focusedPaneId);
  expect(f.storage.getItem(TAB_STORAGE_KEY)).toContain('created');
  expect(f.app.welcome.get('host')).toBeUndefined();
  expect(f.app.draft('new:host:submission')).toBe('');
  f.app.dispose();
});

it('app-owned completion promotes a closed draft in place without reopening it', async () => {
  const f = fixture(false);
  f.app.tabs.ensureNew('host', { runtimeId: 'host', cwd: '/project' });
  const foreground = f.app.tabs.openNew({ cwd: '/other' });
  const pending = f.app.welcome.start('host', f.client, { cwd: '/project' });
  f.app.tabs.closeViews(['host']);
  await pending;
  expect(f.app.tabs.workspace().tabs.some(tab => tab.id === 'host')).toBe(false);
  expect(f.app.tabs.workspace().closed.find(item => item.tab.id === 'host')?.tab).toMatchObject({ kind: 'chat', runtimeId: 'host', rootId: 'created' });
  expect(selectedSessionTab(f.app.tabs.workspace())?.id).toBe(foreground.id);
  expect(f.app.welcome.get('host')).toBeUndefined();
  f.app.dispose();
});

it('recovers accepted work after its descriptor is evicted from closed history', async () => {
  const f = fixture(false);
  f.app.tabs.ensureNew('host', { runtimeId: 'host', cwd: '/project' });
  f.app.tabs.closeViews(['host']);
  for (let i = 0; i < 20; i++) {
    const tab = f.app.tabs.openNew({ cwd: `/other/${i}` });
    f.app.tabs.closeViews([tab.id]);
  }
  const foreground = f.app.tabs.openNew({ cwd: '/foreground' });
  expect(f.app.tabs.workspace().closed.some(item => item.tab.id === 'host')).toBe(false);
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Reopen its New Chat tab');
  const accepted = f.app.welcome.get('host')!;
  expect(accepted.state).toBe('accepted');
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('Explain auth');
  expect(f.app.welcome.list().map(item => item.draftId)).toContain('host');
  expect(f.app.draft('new:host:submission')).toBe('Explain auth');
  expect(f.app.recoverWelcome('host')).toMatchObject({ id: 'host', kind: 'new', runtimeId: 'host', cwd: '/project' });
  expect(selectedSessionTab(f.app.tabs.workspace())?.id).toBe(foreground.id);
  await f.app.welcome.resume('host', f.client);
  expect(f.app.tabs.workspace().tabs.find(tab => tab.id === 'host')).toMatchObject({ kind: 'chat', rootId: 'created' });
  expect(selectedSessionTab(f.app.tabs.workspace())?.id).toBe(foreground.id);
  expect(f.app.welcome.get('host')).toBeUndefined();
  expect(f.sends).toHaveLength(2);
  f.app.dispose();
});

it('does not retire app-owned recovery when window layout persistence throws', async () => {
  const f = fixture(false);
  f.app.tabs.ensureNew('host', { runtimeId: 'host', cwd: '/project' });
  const set = f.storage.setItem;
  const write = vi.spyOn(f.storage, 'setItem').mockImplementation((key, value) => {
    if (key === TAB_STORAGE_KEY) throw new Error('Window quota exhausted');
    set(key, value);
  });
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Window quota exhausted');
  expect(f.app.welcome.get('host')?.state).toBe('accepted');
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('Explain auth');
  expect(f.app.draft('new:host:submission')).toBe('Explain auth');
  expect(f.app.tabs.workspace().tabs.find(tab => tab.id === 'host')?.kind).toBe('new');
  write.mockRestore();
  await f.app.welcome.resume('host', f.client);
  expect(f.app.welcome.get('host')).toBeUndefined();
  expect(f.app.tabs.workspace().tabs.find(tab => tab.id === 'host')?.kind).toBe('chat');
  expect(f.sends).toHaveLength(2);
  f.app.dispose();
});

it('reveals an imported legacy prompt idempotently through runtime recovery', async () => {
  const f = fixture(false);
  const foreground = f.app.tabs.openNew({ cwd: '/foreground' });
  f.app.setDraft('host:welcome:prompt', 'Legacy task'); f.app.flushDrafts();
  const id = (await f.app.welcome.importLegacy('host'))!;
  f.app.recoverWelcome(id, 'host', 'profile');
  f.app.recoverWelcome(id, 'host', 'profile');
  expect(f.app.tabs.workspace().tabs.filter(tab => tab.id === id)).toHaveLength(1);
  expect(f.app.tabs.workspace().tabs.find(tab => tab.id === id)).toMatchObject({ kind: 'new', runtimeId: 'host', hostProfileId: 'profile' });
  expect(selectedSessionTab(f.app.tabs.workspace())?.id).toBe(foreground.id);
  expect(f.app.draft(welcomeDraftKey(id))).toBe('Legacy task');
  expect(f.sends).toHaveLength(0);
  f.app.dispose();
});

it('freezes click-time text and revision while admission waits for the storage lock', async () => {
  const f = fixture();
  let release!: () => void;
  let first = true;
  Object.assign(f.storage, { transaction: (_key: string, action: () => unknown) => {
    if (!first) return Promise.resolve().then(action);
    first = false;
    return new Promise(resolve => { release = () => resolve(action()); });
  } });
  const originalRevision = f.app.draftRevision(welcomeDraftKey('host'));
  const pending = f.app.welcome.start('host', f.client, { cwd: '/project' });
  f.app.setDraft(welcomeDraftKey('host'), 'A later independent edit');
  release(); await pending;
  expect(f.sends[1].payload.text).toBe('Explain auth');
  expect(f.app.welcome.get('host')!.draftRevision).toBe(originalRevision);
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('A later independent edit');
  f.app.dispose();
});

it('does not reveal a consumed legacy marker after its data is retired', async () => {
  const f = fixture(false);
  f.app.setDraft('host:welcome:prompt', 'Legacy task'); f.app.flushDrafts();
  const id = (await f.app.welcome.importLegacy('host'))!;
  f.app.recoverWelcome(id, 'host');
  await f.app.welcome.start(id, f.client, { cwd: '/project' });
  expect(f.app.welcome.get(id)).toBeUndefined();
  expect(await f.app.welcome.importLegacy('host')).toBeUndefined();
  expect(f.storage.getItem('whip.web.welcome-import.v1:host')).toBe(id);
  expect(f.sends).toHaveLength(2);
  f.app.dispose();
});

it('retains recovery through actual fallback-adapter quota loss and resumes after reload', async () => {
  const windowValues = new Map<string, string>();
  let quotaFailure = false;
  const backing: AppStorage = {
    keys: () => [...windowValues.keys()], getItem: key => windowValues.get(key) ?? null,
    setItem: (key, value) => { if (quotaFailure) throw new DOMException('Quota exhausted', 'QuotaExceededError'); windowValues.set(key, value); },
    removeItem: key => { windowValues.delete(key); },
  };
  const unavailable = vi.fn();
  const windowStorage = createFallbackStorage(() => backing, unavailable);
  const f = fixture(false, windowStorage);
  f.app.tabs.ensureNew('host', { runtimeId: 'host', cwd: '/project' });
  const durableLayout = backing.getItem(TAB_STORAGE_KEY);
  expect(windowStorage.persistent).toBe(true);
  quotaFailure = true;
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Window storage is unavailable');
  expect(windowStorage.persistent).toBe(false);
  expect(unavailable).toHaveBeenCalledTimes(1);
  expect(backing.getItem(TAB_STORAGE_KEY)).toBe(durableLayout);
  expect(f.storage.getItem('whip.web.welcome.v2:host')).not.toBeNull();
  expect(f.app.welcome.get('host')?.state).toBe('accepted');
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('Explain auth');
  expect(f.app.draft('new:host:submission')).toBe('Explain auth');
  expect(f.app.tabs.workspace().tabs.find(tab => tab.id === 'host')?.kind).toBe('new');
  await expect(f.app.welcome.resume('host', f.client)).rejects.toThrow('Window storage is unavailable');
  expect(f.sends).toHaveLength(2);
  // The fallback is intentionally permanent in one renderer; reload restores backing storage.
  quotaFailure = false;
  const reloaded = new AppRuntime({ ...f.app.platform, windowStorage: createFallbackStorage(() => backing, vi.fn()) });
  vi.spyOn(reloaded.connections, 'isAttached').mockReturnValue(false);
  await reloaded.welcome.completeAccepted('host');
  expect(reloaded.welcome.get('host')).toBeUndefined();
  expect(reloaded.tabs.workspace().tabs.find(tab => tab.id === 'host')?.kind).toBe('chat');
  expect(backing.getItem(TAB_STORAGE_KEY)).toContain('created');
  expect(f.sends).toHaveLength(2);
  reloaded.dispose(); f.app.dispose();
});

it('retains accepted recovery when text cleanup fails after durable app promotion', async () => {
  const f = fixture(false);
  f.app.tabs.ensureNew('host', { runtimeId: 'host', cwd: '/project' });
  const remove = f.storage.removeItem;
  const cleanup = vi.spyOn(f.storage, 'removeItem').mockImplementation(key => {
    if (key === 'whip.web.draft.v1:new:host:prompt') throw new Error('Draft cleanup denied');
    remove(key);
  });
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Drafts could not be saved');
  expect(f.app.tabs.workspace().tabs.find(tab => tab.id === 'host')?.kind).toBe('chat');
  expect(f.storage.getItem(TAB_STORAGE_KEY)).toContain('created');
  expect(f.app.welcome.get('host')?.state).toBe('accepted');
  expect(f.app.draft('new:host:submission')).toBe('Explain auth');
  cleanup.mockRestore();
  vi.spyOn(f.app.connections, 'isAttached').mockReturnValue(false);
  await f.app.welcome.completeAccepted('host');
  expect(f.app.welcome.get('host')).toBeUndefined();
  expect(f.app.draft('new:host:submission')).toBe('');
  expect(f.sends).toHaveLength(2);
  f.app.dispose();
});

it('keeps both text copies and acceptance when actual runtime has no window storage', async () => {
  const f = fixture(false, null);
  f.app.tabs.ensureNew('host', { runtimeId: 'host', cwd: '/project' });
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow('Restore window storage');
  expect(f.app.welcome.get('host')?.state).toBe('accepted');
  expect(f.app.draft(welcomeDraftKey('host'))).toBe('Explain auth');
  expect(f.app.draft('new:host:submission')).toBe('Explain auth');
  expect(f.app.tabs.workspace().tabs.find(tab => tab.id === 'host')?.kind).toBe('new');
  await expect(f.app.welcome.resume('host', f.client)).rejects.toThrow('Restore window storage');
  expect(f.sends).toHaveLength(2);
  f.app.dispose();
});

it('completes cleanup without a host after real local-storage fallback interrupts retirement', async () => {
  const f = fixture(false); f.app.flushDrafts();
  let failCleanup = true;
  const backing: AppStorage = { ...f.storage, removeItem: key => {
    if (failCleanup && key === 'whip.web.draft.v1:new:host:prompt') throw new DOMException('Storage policy changed', 'SecurityError');
    f.storage.removeItem(key);
  } };
  const transaction: AppStorage['transaction'] = (_key, update) => Promise.resolve().then(update);
  const localStorage = createFallbackStorage(() => backing, vi.fn(), transaction);
  const app = new AppRuntime({ ...f.app.platform, storage: localStorage });
  app.tabs.ensureNew('host', { runtimeId: 'host', cwd: '/project' });
  vi.spyOn(app.connections, 'isAttached').mockReturnValue(true);
  vi.spyOn(app.connections, 'signal').mockReturnValue(new AbortController().signal);
  await expect(app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow();
  expect(localStorage.persistent).toBe(false);
  expect(app.tabs.workspace().tabs.find(tab => tab.id === 'host')?.kind).toBe('chat');
  expect(app.welcome.get('host')?.state).toBe('accepted');
  expect(f.storage.getItem('whip.web.welcome.v2:host')).not.toBeNull();
  expect(f.storage.getItem('whip.web.draft.v1:new:host:submission')).toBe('Explain auth');
  failCleanup = false;
  const reloaded = new AppRuntime({ ...app.platform, storage: createFallbackStorage(() => backing, vi.fn(), transaction) });
  expect(reloaded.connections.isAttached(f.client)).toBe(false);
  expect(await reloaded.welcome.completeAccepted('host')).toBe('created');
  expect(reloaded.welcome.get('host')).toBeUndefined();
  expect(f.storage.getItem('whip.web.draft.v1:new:host:submission')).toBeNull();
  expect(reloaded.tabs.workspace().tabs.find(tab => tab.id === 'host')?.kind).toBe('chat');
  expect(f.sends).toHaveLength(2);
  reloaded.dispose(); app.dispose(); f.app.dispose();
});

it('refuses local completion until acceptance is authoritative', async () => {
  const f = fixture(); f.lose('session.create');
  await expect(f.app.welcome.completeAccepted('missing')).rejects.toThrow('Resolve first-message acceptance');
  await expect(f.app.welcome.start('host', f.client, { cwd: '/project' })).rejects.toThrow();
  const original = f.app.welcome.get('host')!;
  await expect(f.app.welcome.completeAccepted('host')).rejects.toThrow('Resolve first-message acceptance');
  expect(f.app.welcome.get('host')!.create.commandId).toBe(original.create.commandId);
  expect(f.sends).toHaveLength(1);
  f.app.dispose();
});
