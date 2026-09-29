import { QueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { Client, RemoteError, type Operations, type Question } from '@whip/sdk';
import { createSessionView, createExecutionView, type SessionView, type ExecutionView } from '@whip/sdk/state';
import { SubmittedInputs } from '@whip/app/presentation';
import { serverOrigin } from './address';
import type { Draft, MobileStorage } from './storage';
import { defaultAppearance, appearanceRecord, type Appearance, type AppearanceRecord } from '../theme/preferences';
import { validateTheme, type ThemeDefinition } from '@whip/ui/theme-data';
import { creationResultRecorded, creationSettingsKey, type CreationWorkflow } from '../features/creation';
import { connectMobile } from './connection';
import { DecisionStore } from './decisions';
import { MobileCommands, type DeliveryState } from './commands';
import type { MobileDurableMethod, RecoveryIntent } from './recovery-metadata';

export type SavedHost = { id: string; name: string; url: string; clientId: string; runtimeId?: string };
export type CommandState = DeliveryState;
export interface SessionLease { view: SessionView; execution: ExecutionView; release(): void }
export type RuntimeSnapshot = {
  hosts: readonly SavedHost[]; host?: SavedHost; client?: Client;
  active: boolean; ready: boolean; connecting: boolean; error?: string; appearance: Appearance; customThemes?: readonly ThemeDefinition[];
  commands: readonly CommandState[]; revision: number; lastSync?: string; lastErrorCode?: string;
};
const message = (error: unknown) => error instanceof Error ? error.message : String(error);

/** Owns mobile lifetimes and local drafts. Native SDK views are the only owners
 * of transcript and execution state; Query is reserved for bounded read metadata. */
export class MobileRuntime {
  readonly query = new QueryClient({ defaultOptions: { queries: {
    staleTime: 10_000, gcTime: 0, retry: false, networkMode: 'always',
    refetchOnWindowFocus: false, refetchOnReconnect: false,
  }, mutations: { retry: false, networkMode: 'always' } } });
  readonly submitted = new SubmittedInputs();
  readonly commands: MobileCommands;
  readonly decisions = new DecisionStore(this);
  private state: RuntimeSnapshot = { hosts: [], active: true, ready: false, connecting: false, appearance: defaultAppearance, commands: [], revision: 0 };
  private listeners = new Set<() => void>();
  private epoch = 0;
  private lifetime = new AbortController();
  private disposed = false;
  private viewTransition: Promise<unknown> = Promise.resolve();
  private view?: { sessionId: string; view: SessionView; execution: ExecutionView; users: number };
  private drafts = new Map<string, Draft>();
  private draftWrites = new Map<string, { revision: string; promise: Promise<void>; status: 'saving' | 'saved' | 'failed' }>();
  constructor(readonly storage: MobileStorage, private readonly createClient = connectMobile) {
    this.commands = new MobileCommands(storage.nativeRecovery, request => Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, request));
    this.commands.subscribe(() => this.update({ commands: this.commands.getSnapshot() }));
  }
  getSnapshot = () => this.state;
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  private update(patch: Partial<RuntimeSnapshot>) {
    this.state = Object.freeze({ ...this.state, ...patch, revision: this.state.revision + 1 });
    for (const fn of this.listeners) fn();
  }
  report = (error: unknown) => this.update({ error: message(error), lastErrorCode: error instanceof RemoteError ? error.kind : 'local_error' });
  clearError = () => this.update({ error: undefined });
  async start(connectSaved = true) {
    const [hosts, appearance, drafts, selected] = await Promise.all([
      this.storage.list<SavedHost>('hosts'), this.storage.get<Appearance>('settings', 'appearance'),
      this.storage.list<Draft>('drafts'), this.storage.get<string>('settings', 'selectedHost'),
    ]);
    this.drafts = new Map(drafts.map(item => [item.key, item.value]));
    let record: AppearanceRecord;
    try { const saved = await this.storage.get<AppearanceRecord>('themes', 'appearance'); if (saved && saved.version !== 2) throw new Error('Unknown appearance version'); record = appearanceRecord(saved?.appearance ?? appearance ?? defaultAppearance, saved?.themes); }
    catch { record = appearanceRecord(defaultAppearance); this.report(new Error('Saved appearance is unavailable. Using the default themes; saved data has been preserved.')); }
    this.update({ hosts: hosts.map(item => item.value), appearance: record.appearance, customThemes: record.themes });
    const host = this.state.hosts.find(h => h.id === selected);
    if (host && connectSaved && this.state.active) await this.connect(host).catch(this.report);
  }
  setHostProfiles(hosts: readonly SavedHost[]) {
    const host = hosts.find(h => h.id === this.state.host?.id && h.url === this.state.host.url && h.runtimeId === this.state.host.runtimeId && h.clientId === this.state.host.clientId);
    this.update({ hosts, ...(host ? { host } : {}) });
  }
  newHost(url: string, name = ''): SavedHost {
    const origin = serverOrigin(url, __DEV__);
    const existing = this.state.hosts.find(h => h.url === origin);
    return existing ? { ...existing, name: name.trim() || existing.name } : {
      id: Crypto.randomUUID(), name: name.trim() || new URL(origin).hostname, url: origin, clientId: Crypto.randomUUID(),
    };
  }
  async connect(host: SavedHost, options: { select?: boolean; list?: boolean; recovering?: boolean } = {}) {
    if (this.disposed) throw new Error('Mobile runtime is closed');
    if (!this.state.active) throw new Error('Open Whip before connecting');
    host = { ...host, url: serverOrigin(host.url, __DEV__) };
    const retained = options.recovering && this.state.host?.runtimeId === host.runtimeId && this.state.host?.clientId === host.clientId;
    const cleanup = this.detach({ recovering: retained });
    const epoch = this.epoch; await cleanup; this.assertEpoch(epoch);
    this.update({ host, connecting: true, error: undefined });
    try {
      // Persist the caller namespace before initialization can use it.
      await this.storage.set('hosts', host.id, host); this.assertEpoch(epoch);
      this.update({ hosts: [...this.state.hosts.filter(h => h.id !== host.id), host] });
      const client = await this.createClient(host, this.lifetime.signal); this.assertEpoch(epoch);
      if (host.runtimeId && client.runtimeID !== host.runtimeId) throw new Error('Host identity changed');
      host = { ...host, runtimeId: client.runtimeID };
      await this.storage.set('hosts', host.id, host); this.assertEpoch(epoch);
      if (options.select !== false) await this.storage.set('settings', 'selectedHost', host.id);
      this.assertEpoch(epoch);
      await this.commands.bind(client); this.assertEpoch(epoch);
      this.update({ client, host, hosts: [...this.state.hosts.filter(h => h.id !== host.id), host], connecting: true, ready: false, lastSync: new Date().toISOString() });
      await this.decisions.reconcile(); this.assertEpoch(epoch);
      const held = this.view;
      if (held) {
        const transition = this.viewTransition.catch(() => {}).then(async () => {
          this.assertEpoch(epoch); if (this.view !== held) return;
          await held.view.reconnect(client); this.assertEpoch(epoch);
          await held.execution.reconnect(client); this.assertEpoch(epoch);
        });
        this.viewTransition = transition; await transition;
      }
      this.assertEpoch(epoch); this.update({ ready: true, connecting: false });
    } catch (error) {
      if (epoch === this.epoch) { this.update({ connecting: false, ready: false }); this.report(error); }
      throw error;
    }
  }
  private assertEpoch(epoch: number) { if (epoch !== this.epoch || this.disposed) throw new Error('Host changed; observation was detached'); }
  async detach(options: { recovering?: boolean } = {}) {
    ++this.epoch; this.lifetime.abort(); this.lifetime = new AbortController(); this.commands.detach(); this.decisions.reset();
    const view = this.view;
    if (!options.recovering) { this.view = undefined; this.submitted.clear(); }
    const queries = this.query.cancelQueries();
    if (options.recovering) void this.query.invalidateQueries({ refetchType: 'none' }); else this.query.clear();
    this.update({ client: undefined, ...(options.recovering ? {} : { host: undefined }), ready: false, connecting: false });
    if (view) {
      const transition = this.viewTransition.catch(() => {}).then(async () => {
        await (options.recovering ? view.execution.suspend() : view.execution.dispose());
        await (options.recovering ? view.view.suspend() : view.view.dispose());
      });
      this.viewTransition = transition; await transition;
    }
    await queries;
  }
  async removeHost(id: string) {
    let epoch = this.epoch;
    if (this.state.host?.id === id) { const cleanup = this.detach(); epoch = this.epoch; await cleanup; }
    this.assertEpoch(epoch); await this.storage.delete('hosts', id); this.assertEpoch(epoch);
    if (await this.storage.get('settings', 'selectedHost') === id) await this.storage.delete('settings', 'selectedHost');
    this.assertEpoch(epoch); this.update({ hosts: this.state.hosts.filter(host => host.id !== id) });
  }
  setActive(active: boolean) {
    if (active === this.state.active) return;
    this.update({ active, ready: false });
    if (!active) {
      ++this.epoch;
      this.lifetime.abort(new Error('Mobile observation is suspended'));
      void this.view?.execution.suspend(); void this.view?.view.suspend();
      void this.query.cancelQueries(); void this.storage.flush().catch(this.report);
    } else if (this.state.host) void this.reconnect().catch(this.report);
  }
  async reconnect() {
    if (!this.state.active) throw new Error('Open Whip before reconnecting');
    if (!this.state.host) throw new Error('Choose a Whip host first');
    return this.connect(this.state.host, { select: false, recovering: true });
  }
  requireReady(): Client {
    if (!this.state.ready || !this.state.active || !this.state.client) throw new Error('Reconnect to Whip before sending. Your draft is saved.');
    return this.state.client;
  }
  acquireView(sessionId: string, runtimeId: string): SessionLease {
    const client = this.state.client;
    if (!client || client.runtimeID !== runtimeId) throw new Error('This session belongs to another host runtime');
    if (this.view && this.view.sessionId !== sessionId) {
      const old = this.view; this.view = undefined; void old.execution.dispose().then(() => old.view.dispose()).catch(this.report);
    }
    if (!this.view) {
      const session = client.session(sessionId);
      const view = createSessionView(session, { maxBytes: 8 << 20, maxMessages: 512, pollIntervalMs: 1000 });
      const execution = createExecutionView(session, view, { maxBytes: 2 << 20, maxTurns: 8, maxCells: 64, maxOperations: 128 });
      this.view = { sessionId, view, execution, users: 0 };
      if (this.state.active) void view.start().then(() => { if (this.view?.view === view && this.state.active) return execution.start(); }).catch(error => { if (this.view?.view === view) this.report(error); });
    }
    const held = this.view; held.users++; let released = false;
    return { view: held.view, execution: held.execution, release: () => {
      if (released) return; released = true;
      if (--held.users === 0) {
        if (this.view === held) this.view = undefined;
        void held.execution.dispose().then(() => held.view.dispose()).catch(this.report);
      }
    } };
  }
  private appearanceWrites: Promise<void> = Promise.resolve();
  private saveAppearance(change: () => AppearanceRecord) {
    const next = this.appearanceWrites.catch(() => {}).then(async () => { const record = change(); await this.storage.set('themes', 'appearance', record); this.update({ appearance: record.appearance, customThemes: record.themes }); });
    this.appearanceWrites = next; return next;
  }
  setAppearance(appearance: Appearance) { return this.saveAppearance(() => appearanceRecord(appearance, this.state.customThemes)); }
  addTheme(input: unknown) { const theme = validateTheme(input); return this.saveAppearance(() => appearanceRecord(this.state.appearance, [...(this.state.customThemes ?? []).filter(t => t.id !== theme.id), theme])); }
  removeTheme(id: string) { return this.saveAppearance(() => appearanceRecord({ ...this.state.appearance, ...(this.state.appearance.light === id ? { light: defaultAppearance.light } : {}), ...(this.state.appearance.dark === id ? { dark: defaultAppearance.dark } : {}) }, (this.state.customThemes ?? []).filter(t => t.id !== id))); }

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
  private async clearSubmittedDraft(intent: RecoveryIntent | undefined, epoch: number) {
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
  isBlocked(_rootId: string, sessionId = _rootId) { return this.commands.isBlocked(sessionId); }
  async run<M extends MobileDurableMethod>(method: M, params: Operations[M]['params'], options: { rootId?: string; intent?: RecoveryIntent } = {}): Promise<Operations[M]['result']> {
    const client = this.requireReady(); const epoch = this.epoch;
    if (options.intent?.draftKey) await this.draftWrites.get(options.intent.draftKey)?.promise;
    this.assertEpoch(epoch); if (this.requireReady() !== client) throw new Error('Host changed before submission');
    const input = method === 'sessions.submit' ? params as Operations['sessions.submit']['params'] : undefined;
    if (input && options.rootId) this.submitted.add({ runtimeId: client.runtimeID, rootId: options.rootId, agentId: input.session_id, clientId: client.clientID }, input.parts.filter(part => part.type === 'text').map(part => part.text).join('\n'), !!this.view?.view.getSnapshot().activity?.active_turn, input.identity.request_id);
    let result: Operations[M]['result'];
    try { result = await this.commands.run(client.command(method, params), options); }
    catch (error) {
      const delivery = input && this.commands.getSnapshot().find(value => value.record.commandId === input.identity.request_id && value.record.runtimeId === client.runtimeID);
      if (input && (!delivery || delivery.status === 'failed' || delivery.status === 'missing')) this.submitted.remove(input.identity.request_id, client.runtimeID);
      throw error;
    }
    this.assertEpoch(epoch); if (method === 'sessions.submit') { this.submitted.acknowledge(result as Operations['sessions.submit']['result'], client.runtimeID); await this.clearSubmittedDraft(options.intent, epoch); }
    await this.view?.view.refresh().catch(this.report); void this.query.invalidateQueries();
    return result;
  }
  async checkCommand(command: CommandState) {
    this.requireReady(); const epoch = this.epoch;
    const value = await this.commands.check(command.record, this.lifetime.signal);
    this.assertEpoch(epoch);
    if (value.knownAccepted && value.record.operation === 'sessions.submit') { if (value.inputId) this.submitted.accept(value.record.commandId, value.inputId, value.record.runtimeId); await this.clearSubmittedDraft(value.intent, epoch); }
    return value;
  }
  async retryCommand(command: CommandState) {
    this.requireReady(); const epoch = this.epoch;
    const result = await this.commands.retry(command.record);
    this.assertEpoch(epoch); await this.clearSubmittedDraft(command.intent, epoch);
    await this.view?.view.refresh().catch(this.report); void this.query.invalidateQueries(); return result;
  }
  async answerQuestion(rootId: string, params: Operations['questions.answer']['params'], intent: RecoveryIntent, expected: Question['request']) {
    const client = this.requireReady(); const epoch = this.epoch;
    if (intent.draftKey) await this.draftWrites.get(intent.draftKey)?.promise;
    this.assertEpoch(epoch); if (this.requireReady() !== client) throw new Error('Host changed before answering');
    await this.decisions.answer(rootId, params.session_id, params.operation_id, params.answers, intent, expected);
    this.assertEpoch(epoch); await this.clearSubmittedDraft(intent, epoch);
  }
  async forgetCommand(command: CommandState) {
    if (command.intent.workflowId && command.knownAccepted) {
      const host = this.state.host;
      const workflow = host && await this.storage.get<CreationWorkflow>('settings', creationSettingsKey(host.id));
      if (!workflow || !creationResultRecorded(workflow, command)) throw new Error('Save the recovered creation result before clearing its delivery record.');
    }
    await this.commands.forget(command.record);
    if (!command.knownAccepted) this.submitted.remove(command.record.commandId, command.record.runtimeId);
  }
  async dispose(closeStorage = true) {
    if (this.disposed) return; this.disposed = true;
    await this.detach(); this.commands.dispose(); this.decisions.reset(); this.listeners.clear();
    if (closeStorage) await this.storage.close();
  }
}
