import { QueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { createWhipClient, RpcError, WhipError, isTerminal, type WhipClient, type CommandHandle, type CommandOutcome, type RecoveryRecord } from '@whip/sdk';
import { createSessionListView, createSessionView, type SessionView, type SessionListView } from '@whip/sdk/state';
import { SubmittedInputs } from '@whip/app/presentation';
import type { CommandOperation, RuntimeOperations } from '@whip/protocol';
import { serverOrigin } from './address';
import type { CommandIntent, Draft, MobileStorage } from './storage';
import { defaultAppearance, type Appearance } from '../theme/theme';
import { DecisionStore } from './decisions';
import { creationResultRecorded, type CreationWorkflow } from '../features/creation';

export type SavedHost = { id: string; name: string; url: string; clientId: string; runtimeId?: string };
export type CommandState = { record: RecoveryRecord; intent?: CommandIntent; status: string; message?: string; accepted: boolean; retryable?: boolean; outcome?: CommandOutcome };
type RuntimeSnapshot = {
  hosts: readonly SavedHost[]; host?: SavedHost; client?: WhipClient; list?: SessionListView;
  active: boolean; ready: boolean; connecting: boolean; error?: string; appearance: Appearance;
  commands: readonly CommandState[]; revision: number; lastSync?: string; lastErrorCode?: string;
};
const message = (error: unknown) => error instanceof Error ? error.message : String(error);
const namespace = (r: RecoveryRecord) => JSON.stringify([r.runtimeId, r.clientId, r.commandId]);

/** Owns native lifetimes; SDK views remain the sole session-state authority. */
export class MobileRuntime {
  readonly query = new QueryClient({ defaultOptions: { queries: {
    staleTime: 10_000, gcTime: 0, retry: false, networkMode: 'always',
    refetchOnWindowFocus: false, refetchOnReconnect: false,
  }, mutations: { retry: false, networkMode: 'always' } } });
  readonly submitted = new SubmittedInputs();
  readonly decisions = new DecisionStore(this);
  private state: RuntimeSnapshot = { hosts: [], active: true, ready: false, connecting: false, appearance: defaultAppearance, commands: [], revision: 0 };
  private listeners = new Set<() => void>();
  private epoch = 0;
  private lifetime = new AbortController();
  private stopConnection?: () => void;
  private connectingClient?: WhipClient;
  private reconciliation?: { connectionId: string; promise: Promise<void> };
  private disposed = false;
  private view?: { rootId: string; view: SessionView; users: number; stop: () => void; syncInputs: () => Promise<void> };
  private drafts = new Map<string, Draft>();
  private draftWrites = new Map<string, { revision: string; promise: Promise<void>; status: 'saving' | 'saved' | 'failed' }>();
  private child?: { view: SessionView; agentId: string; users: number };
  private handles = new Map<string, CommandHandle>();
  private locks = new Map<string, string>();
  private originalPayloads = new Set<string>();
  private observers = new Map<string, Promise<CommandOutcome>>();
  constructor(readonly storage: MobileStorage, private readonly createClient = createWhipClient) {}
  getSnapshot = () => this.state;
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  private update(patch: Partial<RuntimeSnapshot>) {
    this.state = Object.freeze({ ...this.state, ...patch, revision: this.state.revision + 1 });
    for (const fn of this.listeners) fn();
  }
  report = (error: unknown) => this.update({ error: message(error), lastErrorCode: error instanceof WhipError && /^[a-z_]{1,64}$/.test(error.kind) ? error.kind : 'local_error' });
  clearError = () => this.update({ error: undefined });
  async start() {
    const [hosts, appearance, drafts, selected] = await Promise.all([
      this.storage.list<SavedHost>('hosts'), this.storage.get<Appearance>('settings', 'appearance'),
      this.storage.list<Draft>('drafts'), this.storage.get<string>('settings', 'selectedHost'),
    ]);
    this.drafts = new Map(drafts.map(item => [item.key, item.value]));
    this.update({ hosts: hosts.map(item => item.value), appearance: appearance ?? defaultAppearance });
    const host = this.state.hosts.find(h => h.id === selected);
    if (host) await this.connect(host).catch(this.report);
  }
  newHost(url: string, name = ''): SavedHost {
    const origin = serverOrigin(url, __DEV__);
    const existing = this.state.hosts.find(h => h.url === origin);
    return existing ? { ...existing, name: name.trim() || existing.name } : {
      id: Crypto.randomUUID(), name: name.trim() || new URL(origin).hostname, url: origin, clientId: Crypto.randomUUID(),
    };
  }
  async connect(host: SavedHost) {
    if (this.disposed) throw new WhipError('closed', 'Mobile runtime is closed');
    const url = serverOrigin(host.url, __DEV__);
    const cleanup = this.detach();
    const epoch = this.epoch;
    await cleanup;
    this.assertEpoch(epoch);
    host = { ...host, url };
    this.update({ host, connecting: true, error: undefined });
    let client: WhipClient | undefined;
    try {
      // Persist the client namespace before even initialization can use it.
      await this.storage.set('hosts', host.id, host);
      this.assertEpoch(epoch);
      this.update({ hosts: [...this.state.hosts.filter(h => h.id !== host.id), host] });
      client = this.createClient({ endpoint: url, clientId: host.clientId, clientKind: 'human', buildId: '@whip/mobile:0.1.0',
        connectTimeoutMs: 15_000, recoveryStorage: this.storage.recoveryStorage, randomUUID: Crypto.randomUUID,
        sha256: async bytes => new Uint8Array(await Crypto.digest(Crypto.CryptoDigestAlgorithm.SHA256, bytes)),
      });
      this.connectingClient = client;
      if (!this.state.active) client.pause();
      else {
        try { await client.connect({ signal: this.lifetime.signal }); }
        catch (error) { if (!(error instanceof WhipError) || error.kind !== 'paused') throw error; }
      }
      const info = await client.whenConnected(this.lifetime.signal);
      this.assertEpoch(epoch);
      if (host.runtimeId && host.runtimeId !== info.runtime_id)
        throw new WhipError('runtime_changed', 'This address now serves a different Whip runtime. Remove and add this server to trust its new identity. Existing drafts and recovery records are retained.');
      host = { ...host, runtimeId: info.runtime_id };
      await this.storage.set('hosts', host.id, host);
      this.assertEpoch(epoch);
      await this.storage.set('settings', 'selectedHost', host.id);
      this.assertEpoch(epoch);
      const list = createSessionListView(client);
      const attached = client;
      this.connectingClient = undefined;
      this.update({ client, host, hosts: [...this.state.hosts.filter(h => h.id !== host.id), host], list, connecting: false });
      this.stopConnection = client.subscribe(() => {
        if (epoch !== this.epoch) return;
        this.update({ ready: false });
        if (attached.getSnapshot().state === 'connected' && this.state.active)
          void this.reconcile(epoch).catch(error => { if (epoch === this.epoch && this.state.active) this.report(error); });
      });
      await list.start();
      await this.reconcile(epoch);
    } catch (error) {
      if (epoch === this.epoch && client && this.state.client === client && client.getSnapshot().state === 'paused') return;
      client?.close();
      if (epoch === this.epoch) {
        this.connectingClient = undefined;
        this.stopConnection?.(); this.stopConnection = undefined;
        await this.state.list?.dispose();
        if (epoch === this.epoch) { this.update({ client: undefined, list: undefined, connecting: false, ready: false }); this.report(error); }
      }
      throw error;
    }
  }
  private assertEpoch(epoch: number) { if (epoch !== this.epoch) throw new WhipError('closed', 'Host changed; observation was detached.'); }
  async detach() {
    ++this.epoch;
    this.lifetime.abort(); this.lifetime = new AbortController();
    this.stopConnection?.(); this.stopConnection = undefined;
    const view = this.view; this.view = undefined;
    const list = this.state.list;
    const client = this.state.client ?? this.connectingClient;
    this.connectingClient = undefined;
    this.child = undefined;
    view?.stop(); client?.close();
    this.reconciliation = undefined;
    this.handles.clear(); this.locks.clear(); this.originalPayloads.clear(); this.observers.clear(); this.submitted.clear();
    this.decisions.reset();
    const queries = this.query.cancelQueries();
    this.query.clear();
    // Clear owned state synchronously: an older detach must never clear a new host.
    this.update({ client: undefined, list: undefined, host: undefined, commands: [], ready: false, connecting: false, lastSync: undefined, lastErrorCode: undefined });
    await Promise.all([view?.view.dispose(), list?.dispose(), queries]);
  }
  async removeHost(id: string) {
    let epoch = this.epoch;
    if (this.state.host?.id === id) { const cleanup = this.detach(); epoch = this.epoch; await cleanup; }
    this.assertEpoch(epoch);
    await this.storage.delete('hosts', id);
    this.assertEpoch(epoch);
    const selected = await this.storage.get('settings', 'selectedHost');
    this.assertEpoch(epoch);
    if (selected === id) await this.storage.delete('settings', 'selectedHost');
    this.assertEpoch(epoch);
    this.update({ hosts: this.state.hosts.filter(h => h.id !== id) });
  }
  async setAppearance(appearance: Appearance) { await this.storage.set('settings', 'appearance', appearance); this.update({ appearance }); }
  setActive(active: boolean) {
    if (active === this.state.active) return;
    this.update({ active, ready: false });
    const epoch = this.epoch;
    const client = this.state.client ?? this.connectingClient;
    if (!active) {
      void this.storage.flush().catch(error => { if (epoch === this.epoch) this.report(error); });
      void this.query.cancelQueries();
      client?.pause();
    } else if (client) {
      void client.resume({ signal: this.lifetime.signal }).then(() => {
        if (epoch === this.epoch) return this.reconcile(epoch);
      }).catch(error => { if (epoch === this.epoch && this.state.active) this.report(error); });
    }
  }
  async reconnect() {
    const epoch = this.epoch;
    const client = this.state.client ?? this.connectingClient;
    if (!this.state.active) throw new Error('Open Whip before reconnecting.');
    if (!client) {
      if (!this.state.host) throw new Error('Choose a Whip server first.');
      return this.connect(this.state.host);
    }
    await client.resume({ signal: this.lifetime.signal });
    this.assertEpoch(epoch);
    await this.reconcile(epoch);
  }
  requireReady(): WhipClient {
    if (!this.state.ready || !this.state.active || !this.state.client) throw new Error('Reconnect to Whip before sending. Your draft is saved.');
    this.state.client.requireConnected();
    return this.state.client;
  }
  acquireView(rootId: string, runtimeId: string) {
    const client = this.state.client;
    if (!client || this.state.host?.runtimeId !== runtimeId || client.getSnapshot().info?.runtime_id !== runtimeId) throw new Error('This session belongs to another host runtime.');
    if (this.view && this.view.rootId !== rootId) {
      this.view.stop(); void this.view.view.dispose(); this.view = undefined; this.child = undefined;
    }
    if (!this.view) {
      const epoch = this.epoch;
      const view = createSessionView(client.session(rootId), { maxBytes: 8 << 20, maxMessages: 512, notificationIntervalMs: 16 });
      let stopped = false;
      let syncing: Promise<void> | undefined;
      const current = () => !stopped && epoch === this.epoch && this.view?.view === view && this.state.active && client.getSnapshot().state === 'connected';
      const pending = () => this.submitted.getSnapshot().filter(input => input.runtimeId === runtimeId && input.rootId === rootId && input.accepted && !input.confirmed);
      const report = (error: unknown) => { if (current()) this.report(error); };
      const syncInputs = (): Promise<void> => {
        if (!current()) return Promise.resolve();
        const inbox = view.getSnapshot().root?.inbox;
        this.submitted.confirm(pending().filter(input => inbox?.some(item => item.agent_id === input.agentId && item.seq === input.inboxSeq)).map(input => input.id));
        if (syncing) return syncing;
        if (!pending().length) return Promise.resolve();
        const promise = (async () => {
          // Install the coalescing promise before a refresh publishes state.
          await Promise.resolve();
          while (current()) {
            const inputs = pending();
            if (!inputs.length) return;
            // The first refresh can join a snapshot started before admission.
            // The second hands these exact commands to authoritative inbox/history.
            await view.refresh();
            if (!current()) return;
            await view.refresh();
            const snapshot = view.getSnapshot();
            if (!current() || snapshot.status !== 'live' || snapshot.root?.omitted?.inbox) return;
            this.submitted.confirm(inputs.map(input => input.id));
          }
        })();
        syncing = promise;
        void promise.finally(() => { if (syncing === promise) syncing = undefined; }).catch(() => {});
        return promise;
      };
      const changed = () => { void syncInputs().catch(report); };
      const stopInputs = this.submitted.subscribe(changed);
      const stopView = view.subscribe(() => { if (view.getSnapshot().status === 'live') changed(); });
      const stop = () => { stopped = true; stopInputs(); stopView(); };
      this.view = { rootId, view, users: 0, stop, syncInputs };
      void view.start().then(syncInputs).catch(report);
    }
    const held = this.view; held.users++;
    let released = false;
    return { view: held.view, release: () => {
      if (released) return; released = true;
      if (--held.users === 0) { held.stop(); void held.view.dispose(); if (this.view === held) { this.view = undefined; this.child = undefined; } }
    } };
  }
  acquireAgent(view: SessionView, agentId: string) {
    if (this.view?.view !== view) throw new Error('The selected session has changed.');
    if (agentId === this.view.rootId) {
      if (this.child) { this.child.view.closeAgent(this.child.agentId); this.child = undefined; }
      return { release() {} };
    }
    if (this.child && (this.child.view !== view || this.child.agentId !== agentId)) {
      this.child.view.closeAgent(this.child.agentId); this.child = undefined;
    }
    if (!this.child) {
      const held = { view, agentId, users: 0 };
      this.child = held;
      void view.openAgent(agentId).catch(error => { if (this.child === held) this.report(error); });
    }
    const held = this.child; held.users++;
    let released = false;
    return { release: () => {
      if (released) return; released = true;
      if (--held.users === 0 && this.child === held) { held.view.closeAgent(held.agentId); this.child = undefined; }
    } };
  }
  draft(key: string): Draft { return this.drafts.get(key) ?? { text: '', revision: '' }; }
  draftStatus(key: string) { return this.draftWrites.get(key)?.status ?? 'saved'; }
  draftEntries() { return [...this.drafts].map(([key, value]) => ({ key, value })); }
  async discardDraft(key: string, revision: string) {
    if (this.drafts.get(key)?.revision !== revision) throw new Error('This draft changed. Review its current text before discarding it.');
    // Drain even a failed previous write; explicit discard may recover a full store.
    await this.draftWrites.get(key)?.promise.catch(() => {});
    if (this.drafts.get(key)?.revision !== revision) throw new Error('This draft changed. Review its current text before discarding it.');
    // setDraft serializes later edits after this delete; never clear their memory.
    await this.storage.delete('drafts', key);
    if (this.drafts.get(key)?.revision === revision) { this.drafts.delete(key); this.draftWrites.delete(key); this.update({}); }
  }
  setDraft(key: string, text: string) {
    const draft = { text, revision: Crypto.randomUUID() };
    if (new TextEncoder().encode(text).byteLength > 64 << 10) throw new Error('A draft can contain at most 64 KiB.');
    if (!this.draftWrites.has(key) && this.draftWrites.size >= 16) throw new Error('Wait for saved draft writes or clear a draft before starting another.');
    if (text && !this.drafts.has(key) && this.drafts.size >= 16) throw new Error('There are 16 saved drafts. Send or clear one before starting another.');
    // The persisted store also checks the aggregate budget before committing.
    const total = [...this.drafts].reduce((bytes, [k, d]) => bytes + (k === key ? 0 : new TextEncoder().encode(d.text).byteLength), new TextEncoder().encode(text).byteLength);
    if (total > 512 << 10) throw new Error('Saved drafts have reached 512 KiB. Send or clear a draft first.');
    if (text) this.drafts.set(key, draft); else this.drafts.delete(key);
    this.update({});
    const promise = text ? this.storage.setDraft(key, draft) : this.storage.delete('drafts', key);
    const write = { revision: draft.revision, promise, status: 'saving' as 'saving' | 'saved' | 'failed' };
    this.draftWrites.set(key, write);
    void promise.then(() => {
      if (this.draftWrites.get(key) !== write) return;
      if (!text) this.draftWrites.delete(key); else write.status = 'saved';
      this.update({});
    }).catch(() => {});
    void promise.catch(error => {
      if (this.draftWrites.get(key) !== write) return;
      write.status = 'failed';
      // Keep failed deletions reviewable so Settings can retry an explicit discard.
      if (!text && !this.drafts.has(key)) this.drafts.set(key, draft);
      this.report(new Error('Draft could not be saved. Keep the app open and copy your text before leaving.', { cause: error }));
    });
    return draft;
  }
  private async clearSubmittedDraft(intent: CommandIntent | undefined, epoch: number) {
    if (!intent?.draftKey || !intent.draftRevision || this.drafts.get(intent.draftKey)?.revision !== intent.draftRevision) return;
    await this.draftWrites.get(intent.draftKey)?.promise;
    this.assertEpoch(epoch);
    if (this.drafts.get(intent.draftKey)?.revision !== intent.draftRevision) return;
    if (await this.storage.clearDraft(intent.draftKey, intent.draftRevision)) {
      this.assertEpoch(epoch);
      if (this.drafts.get(intent.draftKey)?.revision === intent.draftRevision) {
        this.drafts.delete(intent.draftKey); this.draftWrites.delete(intent.draftKey); this.update({});
      }
    }
  }
  private putCommand(command: CommandState) {
    this.update({ commands: [...this.state.commands.filter(item => namespace(item.record) !== namespace(command.record)), Object.freeze(command)] });
  }
  private command(record: RecoveryRecord) { return this.state.commands.find(item => namespace(item.record) === namespace(record)); }
  isBlocked(rootId: string, agentId = rootId, requestId?: string) {
    return this.state.commands.some(c => {
      if (c.record.rootId !== rootId || isTerminal(c.status) || c.status === 'not_found') return false;
      if (c.accepted && ['queued', 'running', 'waiting'].includes(c.status)) return false;
      if (!c.intent) return true;
      if (!requestId && c.intent.requestId) return false;
      return requestId ? c.intent.requestId === requestId : !c.intent.agentId || c.intent.agentId === agentId;
    });
  }
  async run<O extends CommandOperation>(operation: O, payload: RuntimeOperations[O]['params'], options: { rootId?: string; intent?: CommandIntent; preview?: { agentId: string; text: string; queued: boolean } } = {}): Promise<CommandOutcome<O>> {
    const client = this.requireReady();
    const runtimeId = client.requireConnected().runtime_id;
    options = structuredClone(options);
    payload = structuredClone(payload);
    const input = ['submit', 'steer', 'agent.submit'].includes(operation);
    const target = options.intent?.requestId ? ['request', options.intent.requestId]
      : input ? ['input', options.intent?.agentId ?? options.rootId] : [operation, options.intent?.agentId];
    const lock = JSON.stringify([runtimeId, options.rootId, target]);
    if (this.locks.has(lock) || (input || options.intent?.requestId) && options.rootId && this.isBlocked(options.rootId, options.intent?.agentId, options.intent?.requestId))
      throw new Error('Check the previous delivery before sending again.');
    if (this.state.commands.length >= 64) throw new Error('Resolve or clear saved command records before sending more.');
    const epoch = this.epoch;
    const id = client.createId();
    this.locks.set(lock, id);
    try {
      if (options.intent?.draftKey) await this.draftWrites.get(options.intent.draftKey)?.promise;
      this.assertEpoch(epoch);
      if (this.requireReady() !== client) throw new Error('Host changed before submission.');
      this.storage.prepareIntent(id, options.intent ?? {});
      if (options.preview && options.rootId) this.submitted.add({ runtimeId, rootId: options.rootId, agentId: options.preview.agentId }, options.preview.text, options.preview.queued, id);
      const handle = client.submit(operation, payload, { rootId: options.rootId, commandId: id });
      this.handles.set(id, handle); this.originalPayloads.add(id);
      this.putCommand({ record: handle.record, intent: options.intent, status: 'sending', accepted: false });
      return await this.observe(handle, options.intent, epoch, true);
    } finally {
      if (this.locks.get(lock) === id) this.locks.delete(lock);
      this.storage.discardIntent(id);
      if (!this.handles.has(id)) this.submitted.remove(id);
    }
  }
  private async applyOutcome<O extends CommandOperation>(record: RecoveryRecord<O>, intent: CommandIntent | undefined, outcome: CommandOutcome<O>, epoch: number): Promise<CommandOutcome<O>> {
    this.assertEpoch(epoch);
    const previous = this.command(record);
    if (!previous) return outcome;
    if (previous?.outcome && isTerminal(previous.status) && !isTerminal(outcome.status)) return previous.outcome as CommandOutcome<O>;
    this.putCommand({ record, intent, status: outcome.status, accepted: true, outcome });
    try {
      await this.storage.markAccepted(record);
      this.assertEpoch(epoch);
      const clear = intent?.workflowId ? intent.step === 'submit' && outcome.status === 'succeeded'
        : ['submit', 'steer'].includes(record.operation) || outcome.status === 'succeeded';
      if (clear) await this.clearSubmittedDraft(intent, epoch);
    } catch (error) {
      if (epoch === this.epoch) this.putCommand({ record, intent, accepted: true, status: 'checking', outcome, message: message(error) });
      throw error;
    }
    this.assertEpoch(epoch);
    for (const [lock, commandId] of this.locks) if (commandId === record.commandId) this.locks.delete(lock);
    // Child command failure ends admission; root turn failure can retain history.
    if (record.operation === 'agent.submit' && isTerminal(outcome.status) && outcome.status !== 'succeeded') this.submitted.remove(record.commandId);
    else this.submitted.acknowledge(outcome);
    if (isTerminal(outcome.status)) this.originalPayloads.delete(record.commandId);
    return outcome;
  }
  private lookupFailed(record: RecoveryRecord, intent: CommandIntent | undefined, error: unknown, epoch: number) {
    this.assertEpoch(epoch);
    const previous = this.command(record);
    if (!previous) return;
    if (previous && isTerminal(previous.status)) return;
    const accepted = previous?.accepted ?? false;
    const missing = error instanceof RpcError && error.kind === 'command_not_found' && !accepted;
    this.putCommand({ record, intent, accepted, status: missing ? 'not_found' : 'checking', message: message(error),
      retryable: missing && this.originalPayloads.has(record.commandId),
    });
    if (missing) this.submitted.remove(record.commandId);
  }
  private observe<O extends CommandOperation>(handle: CommandHandle<O>, intent: CommandIntent | undefined, epoch: number, initial = false): Promise<CommandOutcome<O>> {
    const existing = this.observers.get(handle.commandId);
    if (existing) return existing as Promise<CommandOutcome<O>>;
    const observing = (async () => {
      const signal = this.lifetime.signal;
      let outcome: CommandOutcome<O> | undefined;
      if (initial) {
        try { outcome = await handle.accepted({ signal }); }
        catch (error) {
          this.assertEpoch(epoch);
          if (!(error instanceof WhipError) || error.kind !== 'delivery_uncertain') {
            this.putCommand({ record: handle.record, intent, accepted: false, status: 'failed', message: message(error) });
            this.submitted.remove(handle.commandId); this.originalPayloads.delete(handle.commandId);
            throw error;
          }
          this.lookupFailed(handle.record, intent, error, epoch);
        }
        if (outcome) await this.applyOutcome(handle.record, intent, outcome, epoch);
      }
      try {
        // A fresh status observer never rereads a rejected admission promise.
        const recovered = handle.client.recover(handle.record);
        if (!outcome) {
          for (;;) {
            await handle.client.whenConnected(signal);
            try { outcome = await recovered.status({ signal }); break; }
            catch (error) {
              if (signal.aborted || error instanceof RpcError || !['reconnecting', 'paused'].includes(handle.client.getSnapshot().state)) throw error;
            }
          }
          // Record recovered admission before waiting for execution to finish.
          outcome = await this.applyOutcome(handle.record, intent, outcome, epoch);
        }
        if (!isTerminal(outcome.status)) {
          outcome = await recovered.result({ signal });
          await this.applyOutcome(handle.record, intent, outcome, epoch);
        }
        if (this.state.active && handle.client.getSnapshot().state === 'connected') {
          try {
            const view = this.view;
            if (view && view.rootId === handle.record.rootId) await view.view.refresh();
            this.assertEpoch(epoch);
            if (this.state.active && handle.client.getSnapshot().state === 'connected') await this.query.invalidateQueries();
          } catch (error) {
            this.assertEpoch(epoch);
            if (this.state.active) this.report(error);
          }
        }
        return outcome;
      } catch (error) {
        if (epoch === this.epoch) this.lookupFailed(handle.record, intent, error, epoch);
        throw error;
      }
    })();
    this.observers.set(handle.commandId, observing);
    void observing.finally(() => { if (this.observers.get(handle.commandId) === observing) this.observers.delete(handle.commandId); }).catch(() => {});
    return observing;
  }
  private async lookup(record: RecoveryRecord, intent: CommandIntent | undefined, epoch: number, signal: AbortSignal) {
    const client = this.state.client!;
    let outcome: CommandOutcome;
    try { outcome = await client.recover(record).status({ signal }); }
    catch (error) { this.lookupFailed(record, intent, error, epoch); return undefined; }
    if (!this.command(record)) return undefined;
    await this.applyOutcome(record, intent, outcome, epoch);
    if (!isTerminal(outcome.status)) {
      const handle = this.handles.get(record.commandId) ?? client.recover(record);
      this.handles.set(record.commandId, handle);
      void this.observe(handle, intent, epoch).catch(error => { if (epoch === this.epoch && this.state.active) this.report(error); });
    }
    return outcome;
  }
  async reconcile(epoch = this.epoch): Promise<void> {
    const client = this.state.client;
    if (!client || !this.state.active || client.getSnapshot().state !== 'connected') return;
    const info = client.requireConnected();
    if (this.reconciliation?.connectionId === info.connection_id) return this.reconciliation.promise;
    const signal = client.lifetimeSignal;
    const promise = (async () => {
      this.update({ ready: false });
      let entries: Awaited<ReturnType<MobileStorage['listRecovery']>>;
      for (;;) {
        const commands = this.state.commands;
        entries = (await this.storage.listRecovery()).filter(entry => entry.record.clientId === client.clientId && entry.record.runtimeId === info.runtime_id);
        this.assertEpoch(epoch);
        if (commands === this.state.commands) break;
      }
      for (const entry of entries) {
        const previous = this.command(entry.record);
        this.putCommand({ ...previous, record: entry.record, intent: entry.intent, status: previous?.status ?? 'checking', accepted: entry.knownAccepted || previous?.accepted || false });
      }
      for (let i = 0; i < entries.length; i += 4) {
        signal.throwIfAborted();
        await Promise.all(entries.slice(i, i + 4).map(entry => this.lookup(entry.record, entry.intent, epoch, signal)));
      }
      signal.throwIfAborted(); this.assertEpoch(epoch);
      await this.decisions.reconcile();
      signal.throwIfAborted(); this.assertEpoch(epoch);
      await this.view?.view.refresh();
      signal.throwIfAborted(); this.assertEpoch(epoch);
      await this.view?.syncInputs();
      signal.throwIfAborted(); this.assertEpoch(epoch);
      const child = this.child;
      if (child) await child.view.openAgent(child.agentId).catch(error => { if (this.child === child && epoch === this.epoch && this.state.active) this.report(error); });
      signal.throwIfAborted(); this.assertEpoch(epoch);
      await this.query.invalidateQueries();
      signal.throwIfAborted(); this.assertEpoch(epoch);
      if (this.state.active && client.getSnapshot().info?.connection_id === info.connection_id && client.getSnapshot().state === 'connected') this.update({ ready: true, lastSync: new Date().toISOString() });
    })();
    this.reconciliation = { connectionId: info.connection_id, promise };
    try { await promise; }
    finally { if (this.reconciliation?.promise === promise) this.reconciliation = undefined; }
  }
  async checkCommand(command: CommandState): Promise<CommandOutcome> {
    const client = this.requireReady();
    if (command.record.runtimeId !== client.requireConnected().runtime_id || command.record.clientId !== client.clientId) throw new Error('This command belongs to another host.');
    const outcome = await this.lookup(command.record, this.command(command.record)?.intent ?? command.intent, this.epoch, client.lifetimeSignal);
    if (!outcome) throw new WhipError('recovery_required', this.command(command.record)?.message ?? 'Command status is unavailable.');
    return outcome;
  }
  async retryCommand(command: CommandState) {
    const client = this.requireReady();
    const epoch = this.epoch;
    const saved = (await this.storage.listRecovery()).find(entry => namespace(entry.record) === namespace(command.record));
    this.assertEpoch(epoch);
    const current = this.command(command.record);
    const handle = this.handles.get(command.record.commandId);
    if (saved?.knownAccepted || current?.accepted || !current?.retryable || !this.originalPayloads.has(command.record.commandId) || !handle || handle.client !== client)
      throw new Error('The original request is unavailable or already accepted. Check its status before sending anything again.');
    await handle.retry({ signal: this.lifetime.signal });
    return this.observe(handle, current.intent, epoch, true);
  }
  async forgetCommand(command: CommandState) {
    const epoch = this.epoch;
    const current = this.command(command.record);
    if (!current || !isTerminal(current.status) && current.status !== 'not_found') throw new Error('Check delivery before clearing this record.');
    if (current.status === 'succeeded' && current.intent?.workflowId) {
      const workflow = this.state.host && await this.storage.get<CreationWorkflow>('settings', `creation:${this.state.host.id}`);
      this.assertEpoch(epoch);
      if (!workflow || !creationResultRecorded(workflow, current)) throw new Error('Open New session to save this workflow result before clearing its command.');
    }
    await this.storage.recoveryStorage.delete(current.record);
    this.assertEpoch(epoch);
    this.handles.delete(command.record.commandId); this.originalPayloads.delete(command.record.commandId); this.submitted.remove(command.record.commandId);
    this.update({ commands: this.state.commands.filter(c => namespace(c.record) !== namespace(command.record)) });
  }
  async dispose() { this.disposed = true; await this.detach(); this.listeners.clear(); await this.storage.close(); }
}
