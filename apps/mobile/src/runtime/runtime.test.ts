/// <reference types="node" />
/** @jest-environment node */
import { DatabaseSync } from 'node:sqlite';
import { createWhipClient, type RecoveryRecord } from '@whip/sdk';
import * as SessionState from '@whip/sdk/state';
import { manifest, type RootSnapshot } from '@whip/protocol';
import { conversationRows } from '@whip/app/presentation';
import { MobileRuntime } from './runtime';
import { SqliteMobileStorage, type Draft, type StorageDatabase } from './storage';

jest.mock('expo-crypto', () => ({ randomUUID: () => require('node:crypto').randomUUID(), CryptoDigestAlgorithm: { SHA256: 'SHA256' } }));
jest.mock('../theme/theme', () => ({ defaultAppearance: { mode: 'system', light: 'github-light', dark: 'whip' } }));

const delay = (ms = 0) => new Promise(resolve => setTimeout(resolve, ms));
function deferred<T = void>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r; }); return { promise, resolve }; }
async function until(check: () => boolean) { for (let i = 0; i < 300; i++) { if (check()) return; await delay(5); } throw new Error('Condition did not settle'); }
type Request = { id: string; method: string; params: Record<string, any> };
type Peer = { requests: Request[]; closed: boolean; reply(request: Request, value: unknown): void; error(request: Request, kind: string): void };
const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => { for (const cleanup of cleanups.splice(0)) await cleanup(); jest.restoreAllMocks(); });
function rootSnapshot(): RootSnapshot {
  return {
    root_id: 'root', cursor: '10', history_revision: '1', active_turns: {},
    meta: { id: 'root', kind: 'agent', title: 'Test', model: '', provider: '', cwd: '/', goal: '', forked_from: '', fork_seq: 0,
      tags: [], pinned: false, effort: '', usage_in: 0, usage_cached: 0, usage_out: 0, updated_at: '' },
    messages: [], message_seqs: [], presentation: [], agent_presentations: {}, agents: [], inbox: [], blackboard: [],
    budgets: [], capabilities: [], schedules: [], permissions: [], questions: [],
  };
}
function inboxInput(agentId: string, seq: string, text: string): NonNullable<RootSnapshot['inbox']>[number] {
  return { root_id: 'root', agent_id: agentId, seq, kind: 'submit', status: 'queued',
    payload: { text, reference_id: '', digest: '', size: '0', media_type: '', source: '' } };
}
async function fixture(queryTimeoutMs = 30) {
  const db = new DatabaseSync(':memory:');
  const adapter: StorageDatabase = {
    execAsync: async sql => { db.exec(sql); },
    runAsync: async (sql, ...args) => db.prepare(sql).run(...args),
    getAllAsync: async <T>(sql: string, ...args: Array<string | number>) => db.prepare(sql).all(...args) as T[],
    getFirstAsync: async <T>(sql: string, ...args: Array<string | number>) => (db.prepare(sql).get(...args) ?? null) as T | null,
    closeAsync: async () => { db.close(); },
  };
  const storage = new SqliteMobileStorage(adapter);
  await storage.initialize(true);
  const peers: Peer[] = [];
  const outcomes = new Map<string, Record<string, unknown>>();
  const root = rootSnapshot();
  let override: ((request: Request, peer: Peer) => boolean) | undefined;
  let opening: Promise<void> | undefined;
  const runtime = new MobileRuntime(storage, options => createWhipClient({ ...options, queryTimeoutMs, commandPollMs: 5, endpoint: async handlers => {
    if (opening) { const held = opening; opening = undefined; await held; }
    const peer: Peer = { requests: [], closed: false,
      reply: (request, result) => handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, result })),
      error: (request, kind) => handlers.message(JSON.stringify({ jsonrpc: '2.0', id: request.id, error: { code: -32000, message: kind, data: { kind } } })),
    };
    peers.push(peer);
    return { kind: 'unix', bufferedAmount: 0, close() { peer.closed = true; }, send(text) {
      const request = JSON.parse(text) as Request; peer.requests.push(request);
      if (override?.(request, peer)) return;
      if (request.method === 'initialize') peer.reply(request, {
        protocol_major: 4, protocol_minor: 1, runtime_id: 'runtime', connection_id: `connection-${peers.length}`,
        generation: '1', build_id: 'fixture', host_platform: 'darwin', host_architecture: 'arm64', capabilities: [], negotiated_capabilities: [],
        operations: manifest.operations.map(operation => ({ ...operation })),
        limits: { frame_bytes: 1 << 20, connections: 64, in_flight_requests: 32, outbound_messages: 1024, outbound_bytes: String(8 << 20), root_subscriptions: 16, content_chunk_bytes: 256 << 10, upload_bytes: String(64 << 20) },
      });
      else if (request.method === 'sessions.list') peer.reply(request, { items: [], revision: '1', has_more: false });
      else if (request.method === 'sessions.revision') peer.reply(request, { revision: '1' });
      else if (request.method === 'root.snapshot') peer.reply(request, root);
      else if (request.method === 'events.subscribe') peer.reply(request, { subscription_id: request.params.subscription_id, cursor: request.params.cursor });
      else if (request.method === 'events.unsubscribe') peer.reply(request, {});
      else if (request.method === 'command.submit') {
        const outcome = { command_id: request.params.command_id, operation: request.params.operation, ingress_seq: '1', status: 'queued' };
        outcomes.set(outcome.command_id, { ...outcome, status: 'succeeded' }); peer.reply(request, outcome);
      } else if (request.method === 'command.status') {
        const outcome = outcomes.get(request.params.command_id);
        if (outcome) peer.reply(request, outcome); else peer.error(request, 'command_not_found');
      } else if (request.method === 'daemon.ping') peer.reply(request, { build_id: 'fixture', generation: '1' });
      else throw new Error(`Unexpected ${request.method}`);
    } };
  } }));
  cleanups.push(() => runtime.dispose());
  const host = { id: 'host', name: 'Fixture', url: 'https://host.example.ts.net', clientId: 'phone', runtimeId: 'runtime' };
  return { runtime, storage, db, peers, outcomes, host, root, requests: () => peers.flatMap(peer => peer.requests),
    override(fn: typeof override) { override = fn; }, holdOpening(promise: Promise<void>) { opening = promise; } };
}
const record = (commandId: string): RecoveryRecord => ({ version: 1, runtimeId: 'runtime', clientId: 'phone', commandId, rootId: 'root', operation: 'submit' });

test('a superseded connection cannot clear a newer host and identity is saved before initialization', async () => {
  const f = await fixture(); const held = deferred(); const set = f.storage.set.bind(f.storage);
  jest.spyOn(f.storage, 'set').mockImplementation(async (bucket, key, value) => { if (bucket === 'hosts' && key === 'old') await held.promise; return set(bucket, key, value); });
  const old = f.runtime.connect({ ...f.host, id: 'old', clientId: 'old-phone' }); void old.catch(() => {});
  await f.runtime.connect(f.host);
  expect(await f.storage.get('hosts', 'host')).toMatchObject({ clientId: 'phone' });
  held.resolve(); await expect(old).rejects.toMatchObject({ kind: 'closed' });
  expect(f.runtime.getSnapshot()).toMatchObject({ ready: true, host: { id: 'host' } });
  expect(f.peers).toHaveLength(1); expect(f.peers[0].closed).toBe(false);
});

test('backgrounding during connection initialization can resume the same client', async () => {
  const f = await fixture(); const held = deferred(); f.holdOpening(held.promise);
  const connected = f.runtime.connect(f.host); void connected.catch(() => {});
  await until(() => f.runtime.getSnapshot().connecting && !f.runtime.getSnapshot().client);
  await delay(5); f.runtime.setActive(false); held.resolve(); await delay(5);
  expect(f.runtime.getSnapshot().ready).toBe(false);
  f.runtime.setActive(true); await connected;
  expect(f.runtime.getSnapshot().ready).toBe(true);
  expect(f.peers.filter(peer => !peer.closed)).toHaveLength(1);
});

test('failed connections retain their saved client identity for the next attempt', async () => {
  const f = await fixture();
  f.override((request, peer) => { if (request.method !== 'initialize') return false; peer.error(request, 'execution_failed'); return true; });
  await expect(f.runtime.connect(f.host)).rejects.toBeDefined();
  expect(f.runtime.newHost(f.host.url)).toMatchObject({ id: f.host.id, clientId: f.host.clientId });
  expect(await f.storage.list('hosts')).toHaveLength(1);
});

test('reconnect reruns failed reconciliation without needing a new connection event', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  const failed = jest.spyOn(f.storage, 'listRecovery').mockRejectedValue(new Error('disk unavailable'));
  await expect(f.runtime.reconcile()).rejects.toThrow('disk unavailable');
  expect(f.runtime.getSnapshot().ready).toBe(false);
  failed.mockRestore(); await f.runtime.reconnect();
  expect(f.runtime.getSnapshot().ready).toBe(true); expect(f.peers).toHaveLength(1);
});

test('a failed queued draft write prevents submission before any command byte is sent', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  jest.spyOn(f.storage, 'setDraft').mockRejectedValue(new Error('disk full'));
  const draft = f.runtime.setDraft('draft', 'keep me');
  await expect(f.runtime.run('submit', { text: draft.text }, { rootId: 'root', intent: { agentId: 'root', draftKey: 'draft', draftRevision: draft.revision } })).rejects.toThrow('disk full');
  expect(f.requests().filter(request => request.method === 'command.submit')).toHaveLength(0);
  expect(f.runtime.draft('draft').text).toBe('keep me');
});

test('manually cleared drafts release their durable count budget', async () => {
  const f = await fixture();
  for (let i = 0; i < 16; i++) f.runtime.setDraft(String(i), 'draft');
  await f.storage.flush();
  for (let i = 0; i < 16; i++) f.runtime.setDraft(String(i), '');
  await f.storage.flush(); await delay();
  expect(await f.storage.list('drafts')).toHaveLength(0);
  f.runtime.setDraft('new', 'new draft'); await f.storage.flush();
  expect(await f.storage.list('drafts')).toHaveLength(1);
});

test('acceptance never clears the durable fallback after a newer draft write fails', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  const original = f.runtime.setDraft('draft', 'old'); await f.storage.flush();
  const held = deferred(); const mark = f.storage.markAccepted.bind(f.storage);
  jest.spyOn(f.storage, 'markAccepted').mockImplementation(async record => { await held.promise; return mark(record); });
  const sending = f.runtime.run('submit', { text: 'old' }, { rootId: 'root', intent: { agentId: 'root', draftKey: 'draft', draftRevision: original.revision } }); void sending.catch(() => {});
  await until(() => f.runtime.getSnapshot().commands.some(command => command.accepted));
  jest.spyOn(f.storage, 'setDraft').mockRejectedValue(new Error('disk full'));
  const edited = f.runtime.setDraft('draft', 'new'); held.resolve(); await sending;
  expect(f.runtime.draft('draft')).toEqual(edited);
  expect(await f.storage.get<Draft>('drafts', 'draft')).toEqual(original);
});

test('a second input is admitted before the first turn completes and Stop bypasses send locks', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  f.override((request, peer) => {
    if (request.method === 'command.status') { peer.reply(request, { ...f.outcomes.get(request.params.command_id), status: 'running' }); return true; }
    return false;
  });
  const first = f.runtime.run('submit', { text: 'first' }, { rootId: 'root', intent: { agentId: 'root' } }); void first.catch(() => {});
  await until(() => f.requests().some(request => request.method === 'command.status'));
  const second = f.runtime.run('steer', { text: 'second' }, { rootId: 'root', intent: { agentId: 'root' } }); void second.catch(() => {});
  await until(() => f.requests().filter(request => request.method === 'command.submit').length === 2);
  f.override(undefined); await Promise.all([first, second]);
  f.override((request) => request.method === 'command.submit' && request.params.operation === 'submit');
  const uncertain = f.runtime.run('submit', { text: 'uncertain' }, { rootId: 'root', intent: { agentId: 'root' } }); void uncertain.catch(() => {});
  await until(() => f.runtime.getSnapshot().commands.some(command => command.status === 'sending'));
  const stopped = await f.runtime.run('cancel', { turn_id: 'exact-turn' }, { rootId: 'root', intent: { agentId: 'root' } });
  expect(stopped.status).toBe('succeeded');
  await expect(uncertain).rejects.toBeDefined();
});

test('status-recovered admission releases the send lock before completion', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  f.override((request, peer) => {
    if (request.method === 'command.submit' && request.params.operation === 'submit') {
      f.outcomes.set(request.params.command_id, { command_id: request.params.command_id, operation: 'submit', ingress_seq: '1', status: 'running' });
      return true;
    }
    return false;
  });
  const sending = f.runtime.run('submit', { text: 'first' }, { rootId: 'root', intent: { agentId: 'root' } }); void sending.catch(() => {});
  await until(() => f.runtime.getSnapshot().commands.some(command => command.status === 'running'));
  expect((await f.runtime.run('steer', { text: 'next' }, { rootId: 'root', intent: { agentId: 'root' } })).status).toBe('succeeded');
  const first = f.runtime.getSnapshot().commands.find(command => command.record.operation === 'submit')!;
  f.outcomes.set(first.record.commandId, { ...f.outcomes.get(first.record.commandId), status: 'succeeded' });
  await sending;
});

test('an already-terminal receipt remains successful without a redundant status lookup', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  f.override((request, peer) => {
    if (request.method !== 'command.submit') return false;
    peer.reply(request, { command_id: request.params.command_id, operation: request.params.operation, ingress_seq: '1', status: 'succeeded' });
    return true;
  });
  expect((await f.runtime.run('submit', { text: 'once' }, { rootId: 'root' })).status).toBe('succeeded');
  expect(f.requests().filter(request => request.method === 'command.status')).toHaveLength(0);
});

test('a view refresh failure does not turn a successful command into a rejected result', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  const view = { start: jest.fn(async () => {}), dispose: jest.fn(async () => {}), subscribe: jest.fn(() => () => {}), getSnapshot: () => ({ status: 'live' }), refresh: jest.fn(async () => { throw new Error('history unavailable'); }) };
  jest.spyOn(SessionState, 'createSessionView').mockReturnValue(view as unknown as SessionState.SessionView);
  f.runtime.acquireView('root', 'runtime');
  expect((await f.runtime.run('submit', { text: 'once' }, { rootId: 'root' })).status).toBe('succeeded');
  expect(f.runtime.getSnapshot().error).toBe('history unavailable');
});

test('completed messages sent before opening the view hand off to history without deduplicating identical text', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  for (let i = 0; i < 2; i++) {
    await f.runtime.run('submit', { text: 'same prompt' }, { rootId: 'root', preview: { agentId: 'root', text: 'same prompt', queued: false } });
  }
  const inputs = f.runtime.submitted.getSnapshot();
  expect(inputs).toHaveLength(2); expect(inputs[0].id).not.toBe(inputs[1].id);
  expect(inputs.every(input => !input.confirmed)).toBe(true);
  f.root.messages = [
    { role: 'user', content: 'same prompt', authored: true }, { role: 'assistant', content: 'first answer' },
    { role: 'user', content: 'same prompt', authored: true }, { role: 'assistant', content: 'second answer' },
  ];
  f.root.message_seqs = [1, 2, 3, 4];
  const lease = f.runtime.acquireView('root', 'runtime');
  await until(() => f.runtime.submitted.getSnapshot().every(input => input.confirmed));
  const snapshot = lease.view.getSnapshot();
  const rows = conversationRows(snapshot.history.root, snapshot.root?.presentation, snapshot.root?.inbox ?? [], f.runtime.submitted.getSnapshot());
  expect(rows.filter(row => row.role === 'user')).toHaveLength(2);
  expect(rows.filter(row => row.delivery)).toHaveLength(0);
  expect(f.requests().filter(request => request.method === 'root.snapshot').every(request => request.params.root_id === 'root')).toBe(true);
  expect(f.requests().some(request => request.method === 'history.page')).toBe(false);
});

test('a successful child admission stays queued through its exact inbox identity, including colliding recipient sequences', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  f.override((request, peer) => {
    if (request.method !== 'command.submit') return false;
    peer.reply(request, { command_id: request.params.command_id, operation: 'agent.submit', ingress_seq: '42', status: 'succeeded', result: { agent_id: 'child', inbox_seq: '7', status: 'queued' } });
    return true;
  });
  await f.runtime.run('agent.submit', { id: 'child', text: 'child prompt' }, { rootId: 'root', preview: { agentId: 'child', text: 'child prompt', queued: true } });
  const rootPreview = f.runtime.submitted.add({ runtimeId: 'runtime', rootId: 'root', agentId: 'root' }, 'root prompt', true, 'other-command');
  f.runtime.submitted.accept(rootPreview, '7');
  f.root.inbox = [inboxInput('child', '7', 'host payload')];
  f.root.omitted = { inbox: true }; // Other recipients may be outside this bounded page.
  const lease = f.runtime.acquireView('root', 'runtime');
  await until(() => f.runtime.submitted.getSnapshot()[0].confirmed);
  expect(f.runtime.submitted.getSnapshot()[1].confirmed).toBe(false);
  const snapshot = lease.view.getSnapshot();
  const rows = conversationRows(undefined, [], snapshot.root?.inbox?.filter(item => item.agent_id === 'child') ?? [], f.runtime.submitted.getSnapshot().filter(input => input.agentId === 'child'));
  expect(rows).toMatchObject([{ text: 'child prompt', delivery: 'Queued' }]);
  expect(rows[0].id).toBe(`input:${f.runtime.submitted.getSnapshot()[0].id}`);
});

test('definitive child admission failure removes the sending preview and preserves its saved draft and outcome', async () => {
  const f = await fixture(1000); await f.runtime.connect(f.host);
  const draft = f.runtime.setDraft('child-draft', 'try this');
  let held: { request: Request; peer: Peer } | undefined;
  f.override((request, peer) => { if (request.method !== 'command.status') return false; held = { request, peer }; return true; });
  const sending = f.runtime.run('agent.submit', { id: 'child', text: draft.text }, {
    rootId: 'root', intent: { agentId: 'child', draftKey: 'child-draft', draftRevision: draft.revision },
    preview: { agentId: 'child', text: draft.text, queued: true },
  });
  await until(() => !!held);
  expect(conversationRows(undefined, [], [], f.runtime.submitted.getSnapshot())).toMatchObject([{ text: draft.text, delivery: 'Sending…' }]);
  held!.peer.reply(held!.request, { ...f.outcomes.get(held!.request.params.command_id), status: 'failed', failure: { code: -32000, message: 'recipient is unavailable' } });
  const outcome = await sending;
  expect(outcome).toMatchObject({ status: 'failed', failure: { message: 'recipient is unavailable' } });
  expect(f.runtime.getSnapshot().commands[0]).toMatchObject({ status: 'failed', accepted: true, outcome });
  expect(f.runtime.submitted.getSnapshot()).toHaveLength(0);
  expect(f.runtime.draft('child-draft')).toEqual(draft);
  expect(await f.storage.get<Draft>('drafts', 'child-draft')).toEqual(draft);
});

test('a pre-admission snapshot cannot confirm a message or a later identical unaccepted preview', async () => {
  const f = await fixture(1000); await f.runtime.connect(f.host);
  const snapshots: Array<{ request: Request; peer: Peer }> = [];
  let second: { request: Request; peer: Peer } | undefined;
  let submissions = 0;
  f.override((request, peer) => {
    if (request.method === 'root.snapshot') { snapshots.push({ request, peer }); return true; }
    if (request.method !== 'command.submit') return false;
    if (++submissions === 2) { second = { request, peer }; return true; }
    const outcome = { command_id: request.params.command_id, operation: 'submit', ingress_seq: '1', status: 'running' };
    f.outcomes.set(outcome.command_id, outcome); peer.reply(request, outcome); return true;
  });
  const lease = f.runtime.acquireView('root', 'runtime');
  await until(() => snapshots.length === 1);
  const stale = structuredClone(f.root);
  const options = { rootId: 'root', preview: { agentId: 'root', text: 'same prompt', queued: false } };
  void f.runtime.run('submit', { text: 'same prompt' }, options).catch(() => {});
  await until(() => f.runtime.submitted.getSnapshot()[0]?.accepted);
  void f.runtime.run('submit', { text: 'same prompt' }, options).catch(() => {});
  await until(() => !!second);
  snapshots[0].peer.reply(snapshots[0].request, stale);
  await until(() => snapshots.length >= 2);
  expect(f.runtime.submitted.getSnapshot().every(input => !input.confirmed)).toBe(true);
  f.root.messages = [{ role: 'user', content: 'same prompt', authored: true }]; f.root.message_seqs = [1];
  f.override((request, peer) => {
    if (request.method === 'command.submit') { second = { request, peer }; return true; }
    return false;
  });
  snapshots[1].peer.reply(snapshots[1].request, f.root);
  await until(() => f.runtime.submitted.getSnapshot()[0].confirmed);
  expect(f.runtime.submitted.getSnapshot()[1]).toMatchObject({ accepted: false, confirmed: false });
  const snapshot = lease.view.getSnapshot();
  const rows = conversationRows(snapshot.history.root, [], snapshot.root?.inbox ?? [], f.runtime.submitted.getSnapshot());
  expect(rows.filter(row => row.role === 'user')).toHaveLength(2);
  expect(rows.filter(row => row.delivery)).toMatchObject([{ text: 'same prompt', delivery: 'Sending…' }]);
});

test('reconnect reconciles missed acknowledgements after a failed snapshot without refreshing while paused', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  await f.runtime.run('submit', { text: 'first prompt' }, { rootId: 'root', preview: { agentId: 'root', text: 'first prompt', queued: false } });
  f.override((request, peer) => { if (request.method !== 'root.snapshot') return false; peer.error(request, 'execution_failed'); return true; });
  const lease = f.runtime.acquireView('root', 'runtime');
  await until(() => lease.view.getSnapshot().status === 'error');
  expect(f.runtime.submitted.getSnapshot()[0].confirmed).toBe(false);
  f.runtime.setActive(false);
  const calls = f.requests().length; await delay(20); expect(f.requests()).toHaveLength(calls);
  f.root.messages = [{ role: 'user', content: 'first prompt', authored: true }, { role: 'assistant', content: 'done' }];
  f.root.message_seqs = [1, 2]; f.override(undefined);
  f.runtime.setActive(true);
  await until(() => f.runtime.getSnapshot().ready && f.runtime.submitted.getSnapshot()[0].confirmed);
  expect(f.peers).toHaveLength(2);
  expect(lease.view.getSnapshot().history.root.messages).toHaveLength(2);
});

test('a released view cannot hand off previews when its old snapshot arrives late', async () => {
  const f = await fixture(1000); await f.runtime.connect(f.host);
  await f.runtime.run('submit', { text: 'first prompt' }, { rootId: 'root', preview: { agentId: 'root', text: 'first prompt', queued: false } });
  let held: { request: Request; peer: Peer } | undefined;
  f.override((request, peer) => { if (request.method !== 'root.snapshot') return false; held = { request, peer }; return true; });
  const lease = f.runtime.acquireView('root', 'runtime');
  await until(() => !!held);
  lease.release();
  const calls = f.requests().length;
  held!.peer.reply(held!.request, f.root); await delay(10);
  expect(f.runtime.submitted.getSnapshot()[0].confirmed).toBe(false);
  expect(f.requests()).toHaveLength(calls);
  f.override(undefined);
  f.runtime.acquireView('root', 'runtime');
  await until(() => f.runtime.submitted.getSnapshot()[0].confirmed);
});

test('restored status is checked before ready and previously accepted missing commands never become retryable', async () => {
  const f = await fixture(); f.storage.prepareIntent('accepted', { agentId: 'root' });
  await f.storage.recoveryStorage.put(record('accepted')); await f.storage.markAccepted(record('accepted'));
  let pending: { request: Request; peer: Peer } | undefined;
  f.override((request, peer) => { if (request.method !== 'command.status') return false; pending = { request, peer }; return true; });
  const connected = f.runtime.connect(f.host); void connected.catch(() => {}); await until(() => !!pending);
  expect(f.runtime.getSnapshot().ready).toBe(false);
  pending!.peer.error(pending!.request, 'command_not_found'); await connected;
  const command = f.runtime.getSnapshot().commands[0];
  expect(command).toMatchObject({ status: 'checking', accepted: true, retryable: false });
  expect(f.runtime.isBlocked('root')).toBe(true);
  await expect(f.runtime.retryCommand({ ...command, accepted: false, retryable: true })).rejects.toThrow('already accepted');
  expect(f.requests().filter(request => request.method === 'command.submit')).toHaveLength(0);
});

test('backgrounding during restored status lookup keeps the client resumable and stops network activity', async () => {
  const f = await fixture(); f.storage.prepareIntent('recover', { agentId: 'root' });
  await f.storage.recoveryStorage.put(record('recover'));
  f.override(request => request.method === 'command.status');
  const connected = f.runtime.connect(f.host); void connected.catch(() => {});
  await until(() => f.requests().some(request => request.method === 'command.status'));
  f.runtime.setActive(false); await connected;
  expect(f.runtime.getSnapshot().client?.getSnapshot().state).toBe('paused');
  const calls = f.requests().length; await delay(20); expect(f.requests()).toHaveLength(calls);
  f.outcomes.set('recover', { command_id: 'recover', operation: 'submit', ingress_seq: '1', status: 'succeeded' });
  f.override(undefined); f.runtime.setActive(true);
  await until(() => f.runtime.getSnapshot().ready);
  expect(f.runtime.getSnapshot().commands[0].status).toBe('succeeded');
});

test('failed acceptance persistence retains the draft and blocks a second send until status cleanup succeeds', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  const draft = f.runtime.setDraft('draft', 'once');
  const failed = jest.spyOn(f.storage, 'markAccepted').mockRejectedValue(new Error('disk full'));
  const options = { rootId: 'root', intent: { agentId: 'root', draftKey: 'draft', draftRevision: draft.revision } };
  await expect(f.runtime.run('submit', { text: 'once' }, options)).rejects.toThrow('disk full');
  expect(f.runtime.getSnapshot().commands[0]).toMatchObject({ accepted: true, status: 'checking' });
  expect(f.runtime.draft('draft')).toEqual(draft);
  await expect(f.runtime.run('submit', { text: 'once' }, options)).rejects.toThrow('previous delivery');
  failed.mockRestore();
  await f.runtime.checkCommand(f.runtime.getSnapshot().commands[0]);
  expect(f.runtime.draft('draft').text).toBe('');
  expect(f.requests().filter(request => request.method === 'command.submit')).toHaveLength(1);
});

test('checking an uncertain command uses fresh status and preserves generic lookup failures as uncertainty', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  f.override((request, peer) => {
    if (request.method === 'command.submit') return true;
    if (request.method === 'command.status') { peer.error(request, 'execution_failed'); return true; }
    return false;
  });
  await expect(f.runtime.run('submit', { text: 'once' }, { rootId: 'root', intent: { agentId: 'root' } })).rejects.toMatchObject({ kind: 'execution_failed' });
  const command = f.runtime.getSnapshot().commands[0]; expect(command.status).toBe('checking');
  f.outcomes.set(command.record.commandId, { command_id: command.record.commandId, operation: 'submit', ingress_seq: '1', status: 'succeeded' });
  f.override(undefined);
  expect((await f.runtime.checkCommand(command)).status).toBe('succeeded');
  expect(f.requests().filter(request => request.method === 'command.submit')).toHaveLength(1);
});

test('a failed creation submit preserves its workflow draft and hands off any committed history', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  const draft = f.runtime.setDraft('creation', 'first prompt');
  f.override((request, peer) => {
    if (request.method !== 'command.status') return false;
    peer.reply(request, { ...f.outcomes.get(request.params.command_id), status: 'failed', failure: { code: -32000, message: 'cannot run' } }); return true;
  });
  const outcome = await f.runtime.run('submit', { text: draft.text }, { rootId: 'root', intent: { workflowId: 'workflow', step: 'submit', draftKey: 'creation', draftRevision: draft.revision }, preview: { agentId: 'root', text: draft.text, queued: false } });
  expect(outcome.status).toBe('failed'); expect(f.runtime.draft('creation')).toEqual(draft);
  expect(f.runtime.submitted.getSnapshot()[0]).toMatchObject({ accepted: true, confirmed: false });
  f.root.messages = [{ role: 'user', content: draft.text, authored: true }, { role: 'assistant', content: 'partial answer' }];
  f.root.message_seqs = [1, 2];
  const lease = f.runtime.acquireView('root', 'runtime');
  await until(() => f.runtime.submitted.getSnapshot()[0].confirmed);
  const snapshot = lease.view.getSnapshot();
  expect(conversationRows(snapshot.history.root, [], snapshot.root?.inbox ?? [], f.runtime.submitted.getSnapshot())).toMatchObject([
    { role: 'user', text: draft.text }, { role: 'assistant', text: 'partial answer' },
  ]);
});

test('successful creation results cannot be forgotten until their created root is journaled', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  f.override((request, peer) => {
    if (request.method !== 'command.status') return false;
    peer.reply(request, { ...f.outcomes.get(request.params.command_id), result: { root_id: 'created-root' } }); return true;
  });
  await f.runtime.run('session.create', { kind: 'agent', cwd: '/', model: '', provider: '' }, { intent: { workflowId: 'workflow', step: 'create' } });
  const command = f.runtime.getSnapshot().commands[0];
  await expect(f.runtime.forgetCommand(command)).rejects.toThrow('New session');
  expect(await f.storage.listRecovery()).toHaveLength(1);
  await f.storage.set('settings', 'creation:host', { version: 1, id: 'workflow', runtimeId: 'runtime', clientId: 'phone', cwd: '/', rootId: 'created-root', effortDone: false, promptSent: false });
  await f.runtime.forgetCommand(command); expect(await f.storage.listRecovery()).toHaveLength(0);
});

test('a stale recovery read cannot resurrect a command cleared during reconciliation', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  await f.runtime.run('submit', { text: 'once' }, { rootId: 'root' });
  const command = f.runtime.getSnapshot().commands[0];
  const saved = await f.storage.listRecovery(); const held = deferred();
  jest.spyOn(f.storage, 'listRecovery').mockImplementationOnce(async () => { await held.promise; return saved; });
  const reconciling = f.runtime.reconcile();
  await f.runtime.forgetCommand(command); held.resolve(); await reconciling;
  expect(f.runtime.getSnapshot()).toMatchObject({ ready: true, commands: [] });
  expect(await f.storage.listRecovery()).toHaveLength(0);
});

test('root and child leases share one owner and stale releases cannot close replacement history', async () => {
  const f = await fixture(); await f.runtime.connect(f.host);
  const makeView = () => ({ start: jest.fn(async () => {}), dispose: jest.fn(async () => {}), subscribe: jest.fn(() => () => {}), getSnapshot: () => ({ status: 'live' }), openAgent: jest.fn(async () => {}), closeAgent: jest.fn(), refresh: jest.fn(async () => {}) });
  const first = makeView(); const second = makeView();
  jest.spyOn(SessionState, 'createSessionView').mockReturnValueOnce(first as unknown as SessionState.SessionView).mockReturnValueOnce(second as unknown as SessionState.SessionView);
  const root = f.runtime.acquireView('root', 'runtime'); const shared = f.runtime.acquireView('root', 'runtime');
  expect(root.view).toBe(shared.view); expect(first.start).toHaveBeenCalledTimes(1);
  const oldChild = f.runtime.acquireAgent(root.view, 'child');
  const otherChild = f.runtime.acquireAgent(root.view, 'other');
  const currentChild = f.runtime.acquireAgent(root.view, 'child');
  first.closeAgent.mockClear(); oldChild.release(); otherChild.release();
  expect(first.closeAgent).not.toHaveBeenCalled();
  currentChild.release(); expect(first.closeAgent).toHaveBeenCalledWith('child');
  const replacement = f.runtime.acquireView('replacement', 'runtime');
  root.release(); shared.release(); expect(second.dispose).not.toHaveBeenCalled();
  replacement.release(); replacement.release(); expect(second.dispose).toHaveBeenCalledTimes(1);
});

test('explicit discard cannot erase a newer edit and save status is truthful', async () => {
  const f = await fixture(); await f.runtime.start();
  const initial = f.runtime.setDraft('local', 'old');
  await f.storage.flush();
  expect(f.runtime.draftStatus('local')).toBe('saved');
  const held = deferred(); const remove = f.storage.delete.bind(f.storage);
  jest.spyOn(f.storage, 'delete').mockImplementation(async (bucket, key) => { await remove(bucket, key); await held.promise; });
  const discarded = f.runtime.discardDraft('local', initial.revision);
  await delay();
  const next = f.runtime.setDraft('local', 'new edit');
  held.resolve(); await discarded; await f.storage.flush();
  expect(f.runtime.draft('local')).toEqual(next);
  expect(await f.storage.get('drafts', 'local')).toEqual(next);
  await expect(f.runtime.discardDraft('local', initial.revision)).rejects.toThrow('changed');
});
