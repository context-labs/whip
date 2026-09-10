import { RpcError, type CommandHandle, type RecoveryRecord, type WhipClient } from '@whip/sdk';
import type { CreateSessionParams } from '@whip/protocol';
import type { AppRuntime } from './runtime';

const legacyPrefix = 'whip.web.welcome.v1:';
const prefix = 'whip.web.welcome.v2:';
const bytes = (value: string) => new TextEncoder().encode(value).byteLength;
export const welcomeDraftKey = (draftId: string) => `new:${draftId}:prompt`;
const submittedKey = (draftId: string) => `new:${draftId}:submission`;
type Create = Pick<CreateSessionParams, 'cwd'> & Partial<Omit<CreateSessionParams, 'cwd'>>;
export interface WelcomeSubmission {
  readonly draftId: string;
  readonly create: RecoveryRecord<'session.create'>;
  readonly input: RecoveryRecord<'submit'>;
  readonly params: CreateSessionParams;
  readonly draftRevision: string;
  readonly rootId?: string;
  readonly state: 'creating' | 'sending' | 'accepted' | 'absent' | 'failed';
  readonly error?: string;
}

export type WelcomeCompletion = (draftId: string, runtimeId: string, rootId: string, commandId: string) => void | Promise<void>;

/** One bounded creation journal per draft; host and command identities never change. */
export class WelcomeSubmissions {
  private revision = 0;
  private readonly listeners = new Set<() => void>();
  readonly getSnapshot = () => this.revision;
  readonly subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };
  private emit() { this.revision++; for (const listener of this.listeners) listener(); }
  private readonly running = new Map<string, Promise<string>>();
  constructor(private readonly runtime: AppRuntime, private readonly complete?: WelcomeCompletion) {}
  get(draftId: string): WelcomeSubmission | undefined {
    const value = this.runtime.platform.storage.getItem(prefix + draftId);
    if (!value) return;
    return this.decode(draftId, value);
  }
  private decode(draftId: string, value: string): WelcomeSubmission {
    if (bytes(value) > 8192) throw new Error('The saved first-message recovery record is too large.');
    const item = JSON.parse(value) as WelcomeSubmission;
    const identity = (value: unknown): value is string => typeof value === 'string' && value.length > 0 && value.length <= 256;
    if (!item || !identity(item.draftId) || item.draftId !== draftId || !identity(item.create?.runtimeId) || item.input?.runtimeId !== item.create.runtimeId ||
      item.create.version !== 1 || item.input.version !== 1 || item.create.operation !== 'session.create' || item.input.operation !== 'submit' ||
      !identity(item.create.commandId) || !identity(item.input.commandId) || !identity(item.create.clientId) || item.create.clientId !== item.input.clientId ||
      typeof item.params?.cwd !== 'string' || !item.params.cwd.trim() || typeof item.params.model !== 'string' || typeof item.params.provider !== 'string' ||
      (item.params.execution_engine !== undefined && item.params.execution_engine !== 'starlark' && item.params.execution_engine !== 'quickjs') ||
      typeof item.draftRevision !== 'string' || item.draftRevision.length > 128 ||
      (item.rootId !== undefined && !identity(item.rootId)) || (item.state === 'accepted' && !item.rootId) ||
      item.input.rootId !== item.rootId || !['creating', 'sending', 'accepted', 'absent', 'failed'].includes(item.state))
      throw new Error('The saved first-message recovery record is invalid.');
    // Journals written before language selection could only create Starlark.
    // Status recovery sends no payload; an explicitly retried absent creation
    // must retain that language even if the host now defaults to QuickJS.
    return item.params.execution_engine === undefined ? { ...item, params: { ...item.params, execution_engine: 'starlark' } } : item;
  }
  /** Includes closed/evicted drafts: recovery is not bounded by workspace history. */
  list(): readonly WelcomeSubmission[] {
    const keys = this.runtime.platform.storage.keys().filter(key => key.startsWith(prefix));
    if (keys.length > 32) throw new Error('First-message recovery storage exceeds its record limit.');
    return keys.map(key => this.get(key.slice(prefix.length))!).filter(Boolean);
  }
  /** Frozen payloads cannot be discarded by another window's ordinary draft cleanup. */
  protectsDraft(key: string): boolean {
    if (key.startsWith('new:') && key.endsWith(':submission')) {
      return !!this.runtime.platform.storage.getItem(prefix + key.slice(4, -11));
    }
    if (key.endsWith(':welcome:submission')) {
      return !!this.runtime.platform.storage.getItem(legacyPrefix + key.slice(0, -19));
    }
    return false;
  }
  /** Import, never send. A durable deterministic owner makes interrupted import repeatable. */
  importLegacy(runtimeId: string): Promise<string | undefined> {
    const storage = this.runtime.platform.storage;
    const marker = 'whip.web.welcome-import.v1:' + runtimeId;
    const id = 'legacy-welcome-' + encodeURIComponent(runtimeId);
    const retained = () => this.get(id) || this.runtime.draft(welcomeDraftKey(id)) || this.runtime.draft(submittedKey(id));
    const migrate = () => {
      const imported = storage.getItem(marker) === id;
      const legacyPrompt = `${runtimeId}:welcome:prompt`;
      const legacySubmitted = `${runtimeId}:welcome:submission`;
      const text = this.runtime.draft(legacyPrompt);
      const journal = storage.getItem(legacyPrefix + runtimeId);
      if (!text && !journal) return imported && retained() ? id : undefined;
      if (id.length > 256) throw new Error('The legacy draft identity is too long to import.');
      if (storage.persistent === false) throw new Error('Restore device storage before importing the saved first message.');
      let item: WelcomeSubmission | undefined;
      if (journal) {
        if (bytes(journal) > 8192) throw new Error('The saved first-message recovery record is too large.');
        item = this.decode(id, JSON.stringify({ ...JSON.parse(journal), draftId: id }));
        if (item.create.runtimeId !== runtimeId) throw new Error('The legacy first message belongs to a different execution host.');
      }
      // Copy before retiring legacy keys. Capacity refusal leaves the original intact.
      if (!imported && text && !this.runtime.draft(welcomeDraftKey(id))) this.runtime.setDraft(welcomeDraftKey(id), text);
      if (!imported && item) {
        const frozen = this.runtime.draft(legacySubmitted);
        if (!frozen && !['accepted', 'failed'].includes(item.state)) throw new Error('The saved first-message payload is missing.');
        if (frozen && !this.runtime.draft(submittedKey(id))) this.runtime.setDraft(submittedKey(id), frozen);
      }
      const flushed = this.runtime.flushDrafts();
      if (!flushed.saved) throw new Error(flushed.error);
      if (!imported && item && !this.get(id)) this.save(id, { ...item, draftRevision: item.draftRevision === this.runtime.draftRevision(legacyPrompt) ? this.runtime.draftRevision(welcomeDraftKey(id)) : item.draftRevision });
      storage.setItem(marker, id);
      if (this.runtime.platform.storage.persistent === false) throw new Error('The legacy first-message import could not be saved.');
      storage.removeItem(legacyPrefix + runtimeId);
      this.runtime.setDraft(legacyPrompt, '');
      this.runtime.setDraft(legacySubmitted, '');
      const cleaned = this.runtime.flushDrafts();
      if (!cleaned.saved) throw new Error(cleaned.error);
      return retained() ? id : undefined;
    };
    return Promise.resolve().then(() => {
      // Most hosts have nothing to import; do not contend with command admission.
      if (!storage.getItem(legacyPrefix + runtimeId) && !this.runtime.draft(`${runtimeId}:welcome:prompt`))
        return storage.getItem(marker) === id && retained() ? id : undefined;
      return storage.transaction ? storage.transaction(prefix, migrate) : migrate();
    });
  }
  private save(draftId: string, item: WelcomeSubmission) {
    const storage = this.runtime.platform.storage;
    const key = prefix + draftId;
    const encoded = JSON.stringify(item);
    const replacing = storage.getItem(legacyPrefix + item.create.runtimeId);
    const legacyKey = replacing && bytes(replacing) <= 8192 && JSON.parse(replacing)?.create?.commandId === item.create.commandId ? legacyPrefix + item.create.runtimeId : undefined;
    const keys = storage.keys().filter(key => (key.startsWith(prefix) || key.startsWith(legacyPrefix)) && key !== legacyKey);
    if (bytes(encoded) > 8192 || (!keys.includes(key) && keys.length >= 32) ||
      keys.filter(item => item !== key).reduce((size, key) => size + bytes(storage.getItem(key) ?? ''), bytes(encoded)) > 256 * 1024)
      throw new Error('First-message recovery storage is full. Resolve existing submissions before creating another session.');
    if (storage.persistent === false) throw new Error('Device storage is unavailable. Restore storage before sending your first message.');
    storage.setItem(key, encoded);
    if (this.runtime.platform.storage.persistent === false) throw new Error('The first-message recovery record could not be saved.');
    this.emit();
  }
  private transition(draftId: string, item: WelcomeSubmission): Promise<WelcomeSubmission> {
    const write = () => {
      const current = this.get(draftId);
      if (current?.create.commandId !== item.create.commandId)
        throw new Error('This first-message recovery record was already retired. A newer request is unchanged.');
      if (current.state === 'accepted') return current;
      this.save(draftId, item);
      return item;
    };
    const storage = this.runtime.platform.storage;
    return storage.transaction ? storage.transaction(prefix, write) : Promise.resolve().then(write);
  }
  private identity(client: WhipClient) {
    const snapshot = client.getSnapshot();
    if (snapshot.state !== 'connected' || !snapshot.info || !this.runtime.connections.isAttached(client)) throw new Error('Reconnect to the original execution host first.');
    return snapshot.info.runtime_id;
  }
  start(id: string, client: WhipClient, params: Create): Promise<string> {
    const runtimeId = this.identity(client);
    if (!id || id.length > 256) throw new Error('Invalid New Chat draft identity.');
    if (this.running.has(id)) return this.running.get(id)!;
    const text = this.runtime.draft(welcomeDraftKey(id));
    // Freeze click-time bytes and revision, not later edits while admission waits.
    if (text && !this.runtime.draftRevision(welcomeDraftKey(id))) this.runtime.setDraft(welcomeDraftKey(id), text);
    const draftRevision = this.runtime.draftRevision(welcomeDraftKey(id));
    const executionEngine = params.execution_engine ?? client.getSnapshot().info?.default_execution_engine ?? 'starlark';
    if (executionEngine !== 'starlark' && executionEngine !== 'quickjs') throw new Error('This host’s default execution language is unsupported. Choose a supported language.');
    const frozenParams: CreateSessionParams = JSON.parse(JSON.stringify({ kind: 'agent', model: '', provider: '', ...params, execution_engine: executionEngine }));
    return this.track(id, async () => {
      const prepare = () => {
        if (this.identity(client) !== runtimeId) throw new Error('The execution host changed before submission.');
        if (this.get(id)) throw new Error('Resolve the existing first message before creating another session.');
        if (!text.trim() || !frozenParams.cwd.trim()) throw new Error('Choose a folder and describe your task first.');
        this.runtime.setDraft(submittedKey(id), text);
        const persisted = this.runtime.flushDrafts();
        if (!persisted.saved) throw new Error(persisted.error);
        const create: RecoveryRecord<'session.create'> = { version: 1, runtimeId, clientId: client.clientId, commandId: crypto.randomUUID(), operation: 'session.create' };
        const input: RecoveryRecord<'submit'> = { ...create, commandId: crypto.randomUUID(), operation: 'submit' };
        const item: WelcomeSubmission = { draftId: id, create, input, draftRevision, params: frozenParams, state: 'creating' };
        this.save(id, item);
        return item;
      };
      const storage = this.runtime.platform.storage;
      // Only admission holds the cross-window lock; network work never does.
      const item = await (storage.transaction ? storage.transaction(prefix, prepare) : Promise.resolve().then(prepare));
      return this.proceed(client, item, 'start');
    });
  }
  resume(id: string, client: WhipClient, retry = false): Promise<string> {
    const runtimeId = this.identity(client);
    if (this.running.has(id)) return this.running.get(id)!;
    const item = this.get(id);
    if (!item) return Promise.reject(new Error('There is no first message to recover.'));
    if (item.create.runtimeId !== runtimeId) return Promise.reject(new Error('Reconnect to the original execution host first.'));
    if (item.create.clientId !== client.clientId) return Promise.reject(new Error('Reconnect with the original client identity to recover this request.'));
    if (item.state === 'accepted' && item.rootId) return this.track(id, () => this.accepted(item));
    if (item.state === 'failed') return Promise.reject(new Error(item.error || 'The original request failed. Open its session or discard it before trying again.'));
    if (retry && item.state !== 'absent') return Promise.reject(new Error('Check the original request before retrying it.'));
    return this.track(id, () => this.proceed(client, item, retry ? 'retry' : 'check'));
  }
  /** Acceptance is already authoritative; only local promotion/cleanup remains. */
  completeAccepted(id: string): Promise<string> {
    const item = this.get(id);
    if (item?.state !== 'accepted' || !item.rootId)
      return Promise.reject(new Error('Resolve first-message acceptance before completing its recovery.'));
    if (this.running.has(id)) return this.running.get(id)!;
    return this.track(id, () => this.accepted(item));
  }
  private track(id: string, action: () => Promise<string>) {
    const promise = action().finally(() => this.running.delete(id));
    this.running.set(id, promise);
    return promise;
  }
  private async proceed(client: WhipClient, saved: WelcomeSubmission, mode: 'start' | 'check' | 'retry'): Promise<string> {
    const id = saved.draftId;
    let item = saved;
    const text = this.runtime.draft(submittedKey(id));
    const signal = this.runtime.connections.signal(client);
    let handle: CommandHandle<'session.create'> | CommandHandle<'submit'> | undefined;
    try {
      if (!item.rootId) {
        const create = mode === 'start' ? client.sessions.create(item.params, { commandId: item.create.commandId }) : client.recover(item.create, item.params);
        handle = create;
        if (mode === 'retry') await create.retry({ signal });
        const result = await this.runtime.run(create, 'Create session', undefined, welcomeDraftKey(id));
        if (!result.result?.root_id) throw new Error('Session creation returned no session.');
        item = { ...item, rootId: result.result.root_id, input: { ...item.input, rootId: result.result.root_id }, state: 'sending', error: undefined };
        item = await this.transition(id, item);
        // A recovered creation continues only because the user explicitly checked it.
        mode = 'start';
      }
      if (!text && mode !== 'check') throw new Error('The saved first-message text is unavailable. Open the created session to continue.');
      const rootId = item.rootId!;
      // Persist the input identity with its root before any request can leave.
      item = await this.transition(id, item);
      if (item.state === 'accepted') return this.accepted(item);
      const input = mode === 'start' ? client.session(rootId).submit({ text }, { commandId: item.input.commandId }) : client.recover(item.input, text ? { text } : undefined);
      handle = input;
      if (mode === 'retry') await input.retry({ signal });
      return await new Promise<string>((resolve, reject) => {
        let accepted = false;
        void this.runtime.run(input, 'Send first message', () => {
          accepted = true;
          void this.transition(id, { ...item, state: 'accepted', error: undefined }).then(async acceptedItem => {
            await this.accepted(acceptedItem);
            resolve(rootId);
          }).catch(error => { this.runtime.report(error); reject(error); });
        }, welcomeDraftKey(id)).catch(error => { if (!accepted) reject(error); });
      });
    } catch (error) {
      // A missing status is not a new submission. Only an explicit retry can reuse it.
      if (error instanceof RpcError && error.kind === 'command_not_found') {
        await this.transition(id, { ...item, state: 'absent', error: 'The host has not accepted this request. Retry the original request when ready.' });
      } else {
        let terminal = false;
        if (handle && !signal.aborted) {
          try { terminal = ['failed', 'cancelled', 'interrupted'].includes((await handle.status({ signal })).status); } catch { /* Unknown remains recoverable. */ }
        }
        await this.transition(id, { ...item, ...(terminal ? { state: 'failed' as const } : {}), error: (error instanceof Error ? error.message : String(error)).slice(0, 2048) });
      }
      throw error;
    }
  }
  private async accepted(item: WelcomeSubmission): Promise<string> {
    if (!item.rootId) throw new Error('The accepted first message has no session identity.');
    const clear = () => {
      const current = this.get(item.draftId);
      if (current?.create.commandId !== item.create.commandId) throw new Error('This first-message recovery record was already retired. A newer request is unchanged.');
      const key = welcomeDraftKey(item.draftId);
      if (this.runtime.draftRevision(key) === item.draftRevision && this.runtime.draft(key) === this.runtime.draft(submittedKey(item.draftId))) this.runtime.setDraft(key, '');
      const result = this.runtime.flushDrafts();
      if (!result.saved) throw new Error(result.error);
    };
    const storage = this.runtime.platform.storage;
    const complete = this.complete;
    const rootId = item.rootId;
    if (complete) {
      const promote = () => {
        if (this.get(item.draftId)?.create.commandId !== item.create.commandId)
          throw new Error('This first-message recovery record was already retired. A newer request is unchanged.');
        return complete(item.draftId, item.create.runtimeId, rootId, item.create.commandId);
      };
      // Represent accepted work durably before clearing either copy of its text.
      await (storage.transaction ? storage.transaction(prefix, promote) : Promise.resolve().then(promote));
    }
    await (storage.transaction ? storage.transaction(prefix, clear) : Promise.resolve().then(clear));
    if (this.complete) await this.finish(item.draftId, item.create.commandId);
    return item.rootId;
  }
  /** Retire after durable promotion, or explicit discard of a known terminal failure. */
  finish(draftId: string, commandId: string): Promise<void> {
    const retire = () => {
      const item = this.get(draftId);
      if (!item) return;
      if (item.create.commandId !== commandId) throw new Error('A newer first message must keep its recovery record.');
      if (!['accepted', 'failed'].includes(item.state)) throw new Error('Resolve first-message acceptance before discarding its recovery record.');
      this.runtime.platform.storage.removeItem(prefix + draftId);
      this.runtime.setDraft(submittedKey(draftId), '');
      const result = this.runtime.flushDrafts();
      if (!result.saved) { this.save(draftId, item); throw new Error(result.error); }
      this.emit();
    };
    const storage = this.runtime.platform.storage;
    return storage.transaction ? storage.transaction(prefix, retire) : Promise.resolve().then(retire);
  }
}
