import { QueryClient } from '@tanstack/react-query';
import {
  DeliveryUncertainError,
  RpcError,
  type WhipClient,
  type RecoveryRecord,
  type RecoveryStorage,
  type CommandHandle,
  type CommandOutcome,
} from '@whip/sdk';
import {
  createSessionView,
  type SessionView,
} from '@whip/sdk/state';
import type { CommandOperation } from '@whip/protocol';
import { errorMessage, readPreference, type AppPlatform } from './platform';
import { SessionTabs } from './session-tabs';
import { CompositionStore } from './compositions';
import { ReadingPositions } from './reading-positions';
import { SubmittedInputs } from './input-presentation';
import { HostConnections, type HostConnection } from './hosts';

interface ViewLease {
  client: WhipClient;
  runtimeId: string;
  view: SessionView;
  users: number;
  timer?: ReturnType<typeof setTimeout>;
}
export interface CommandNotice {
  id: string;
  commandId: string;
  runtimeId: string;
  label: string;
  status: string;
  error?: string;
  draftKey?: string;
  delivery?: 'uncertain' | 'absent';
}
interface PendingCommand {
  client: WhipClient;
  check(): Promise<unknown>;
  retry(): Promise<unknown>;
}
const draftStoragePrefix = 'whip.web.draft.v1:';
const encodedBytes = (value: string) =>
  new TextEncoder().encode(value).byteLength;
export const commandShortcuts = ['Mod+K', 'Mod+P', 'Mod+Shift+P'] as const;
export const composerShortcuts = [
  'Mod+Shift+L',
  'Mod+Shift+J',
  'Mod+Shift+F',
] as const;
export interface DevicePreferences {
  commandShortcut: (typeof commandShortcuts)[number];
  composerShortcut: (typeof composerShortcuts)[number];
  attentionAnnouncements: boolean;
  desktopNotifications: boolean;
}
const defaultPreferences: DevicePreferences = {
  commandShortcut: 'Mod+K',
  composerShortcut: 'Mod+Shift+L',
  attentionAnnouncements: true,
  desktopNotifications: false,
};
interface RuntimeSnapshot {
  preferences: DevicePreferences;
  hosts: readonly HostConnection[];
  home?: HostConnection;
  legacyHosts: readonly string[];
  profilesReady: boolean;
  profileError?: string;
  selectedHostId?: string;
  error?: string;
  commands: readonly CommandNotice[];
}

/** Owns UI observation lifetimes; accepted execution continues after disposal. */
export class AppRuntime {
  readonly connections: HostConnections;
  readonly tabs: SessionTabs;
  readonly compositions = new CompositionStore();
  readonly readingPositions = new ReadingPositions();
  readonly submittedInputs = new SubmittedInputs();
  readonly queries = new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 10_000,
        gcTime: 0,
        retry: false,
        networkMode: 'always',
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
      },
      mutations: { retry: false, networkMode: 'always', gcTime: 0 },
    },
  });
  private state: RuntimeSnapshot;
  private readonly listeners = new Set<() => void>();
  private readonly views = new Map<string, ViewLease>();
  private readonly drafts = new Map<string, string>();
  private readonly draftListeners = new Map<string, Set<() => void>>();
  private readonly agentReaders = new WeakMap<SessionView, Map<string, { users: number }>>();
  private readonly draftIdentities = new Set<string>();
  private readonly durableDrafts = new Set<string>();
  private readonly pending = new Map<string, PendingCommand>();
  private draftTimer?: ReturnType<typeof setTimeout>;
  private readonly dirtyDrafts = new Set<string>();
  private closed = false;

  constructor(readonly platform: AppPlatform) {
    const preferences = readPreference<Partial<DevicePreferences> | null>(
      platform.storage,
      'whip.web.preferences.v1',
      null,
    );
    const hosts = readPreference<unknown>(
      platform.storage,
      'whip.web.hosts.v1',
      [],
    );
    this.state = {
      hosts: [],
      profilesReady: false,
      commands: [],
      legacyHosts: Array.isArray(hosts)
        ? hosts
            .filter(
              (host): host is string =>
                typeof host === 'string' && host.length <= 2048,
            )
        : [],
      preferences: {
        commandShortcut: commandShortcuts.includes(
          preferences?.commandShortcut as never,
        )
          ? preferences!.commandShortcut!
          : defaultPreferences.commandShortcut,
        composerShortcut: composerShortcuts.includes(
          preferences?.composerShortcut as never,
        )
          ? preferences!.composerShortcut!
          : defaultPreferences.composerShortcut,
        attentionAnnouncements: preferences?.attentionAnnouncements !== false,
        desktopNotifications: preferences?.desktopNotifications === true,
      },
    };
    this.tabs = new SessionTabs(platform.windowStorage, message => this.report(message), this.lastSession()?.runtimeId);
    let previousTabs = this.tabs.getSnapshot();
    this.tabs.subscribe(() => {
      const next = this.tabs.getSnapshot();
      const before = previousTabs.workspace, after = next.workspace;
      const views = new Set([...after.tabs, ...after.closed.map(item => item.tab)].map(tab => tab.id));
      for (const tab of [...before.tabs, ...before.closed.map(item => item.tab)]) {
        if (!views.has(tab.id)) this.readingPositions.forgetView(tab.runtimeId, tab.id);
      }
      previousTabs = next;
    });
    try { for (const key of this.savedDrafts().keys()) this.draftIdentities.add(key); } catch (error) { this.report(error); }
    this.connections = new HostConnections(platform, this.recoveryStorage(), {
      connected: runtimeId => { void this.queries.invalidateQueries({ predicate: query => query.queryKey[1] === runtimeId }); },
      detached: (client, runtimeId) => {
        if (runtimeId) this.compositions.invalidateRuntime(runtimeId);
        for (const [id, pending] of this.pending) if (pending.client === client) this.pending.delete(id);
        for (const [id, lease] of this.views) if (lease.client === client) this.dropView(id, lease);
        if (runtimeId) this.queries.removeQueries({ predicate: query => query.queryKey[1] === runtimeId });
      },
    });
    const updateHosts = () => {
      const { hosts, profilesReady, profileError, selectedId } = this.connections.getSnapshot();
      this.update({ hosts, home: hosts.find(host => host.local), profilesReady, profileError, selectedHostId: selectedId });
    };
    this.connections.subscribe(updateHosts);
    updateHosts();
  }
  getSnapshot = () => this.state;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  private update(patch: Partial<RuntimeSnapshot>) {
    this.state = Object.freeze({ ...this.state, ...patch });
    for (const listener of this.listeners) listener();
  }
  setPreferences(patch: Partial<DevicePreferences>) {
    const preferences = { ...this.state.preferences, ...patch };
    this.platform.storage.setItem(
      'whip.web.preferences.v1',
      JSON.stringify(preferences),
    );
    this.update({ preferences });
  }
  forgetLegacyHost(endpoint: string) {
    const hosts = this.state.legacyHosts.filter((host) => host !== endpoint);
    this.platform.storage.setItem('whip.web.hosts.v1', JSON.stringify(hosts));
    this.update({ legacyHosts: hosts });
  }
  rememberSession(runtimeId: string, rootId: string) {
    this.platform.storage.setItem(
      'whip.web.last-session.v1',
      JSON.stringify({ runtimeId, rootId }),
    );
  }
  lastSession(): { runtimeId: string; rootId: string } | undefined {
    const saved = readPreference<unknown>(
      this.platform.storage,
      'whip.web.last-session.v1',
      null,
    );
    if (
      saved &&
      typeof saved === 'object' &&
      'runtimeId' in saved &&
      'rootId' in saved &&
      typeof saved.runtimeId === 'string' &&
      typeof saved.rootId === 'string'
    )
      return { runtimeId: saved.runtimeId, rootId: saved.rootId };
  }
  clearError() {
    this.update({ error: undefined });
  }
  report(error: unknown) {
    // Disposing a view or detaching a host cancels its local observers.
    if (error && typeof error === 'object' && 'name' in error && error.name === 'AbortError') return;
    this.update({ error: errorMessage(error) });
  }
  draft(key: string) {
    if (!this.dirtyDrafts.has(key)) {
      try {
        const text = this.platform.storage.getItem(draftStoragePrefix + key);
        if (this.platform.storage.persistent !== false) {
          if (text) this.durableDrafts.add(key); else this.durableDrafts.delete(key);
        }
        if (text) {
          this.validateDrafts(new Map([[key, text]]));
          return text;
        }
        return '';
      } catch (error) { this.report(error); }
    }
    return this.drafts.get(key) ?? '';
  }
  subscribeDraft(key: string, listener: () => void) {
    let listeners = this.draftListeners.get(key);
    if (!listeners) { listeners = new Set(); this.draftListeners.set(key, listeners); }
    listeners.add(listener);
    return () => { listeners.delete(listener); if (!listeners.size) this.draftListeners.delete(key); };
  }
  hasSessionDraft(runtimeId: string, rootId: string) {
    const prefix = `${runtimeId}:${rootId}:`;
    return this.compositions.hasAttachments(runtimeId, rootId) || [...this.draftIdentities].some(key => key.startsWith(prefix));
  }
  private validateDrafts(drafts: Map<string, string>) {
    if (drafts.size > 32)
      throw new Error('There are 32 unsent drafts. Send or clear a draft before creating another.');
    if (encodedBytes(JSON.stringify([...drafts])) > 1024 * 1024)
      throw new Error('Unsent drafts have reached the 1 MiB limit. Send or clear a draft before adding more.');
    for (const [key, text] of drafts) {
      if (!key || key.length > 512) throw new Error('Invalid draft identity');
      if (encodedBytes(text) > 256 * 1024)
        throw new Error('A draft can contain at most 256 KiB. Shorten it before continuing.');
    }
  }
  private savedDrafts() {
    const saved = new Map<string, string>();
    for (const key of this.platform.storage.keys()) {
      if (!key.startsWith(draftStoragePrefix)) continue;
      const text = this.platform.storage.getItem(key);
      if (text) saved.set(key.slice(draftStoragePrefix.length), text);
      if (text && this.platform.storage.persistent !== false)
        this.durableDrafts.add(key.slice(draftStoragePrefix.length));
      // Stop reading oversized storage before retaining unbounded values.
      this.validateDrafts(saved);
    }
    if (this.platform.storage.persistent !== false) {
      this.durableDrafts.clear();
      for (const key of saved.keys()) this.durableDrafts.add(key);
    }
    return saved;
  }
  private mergedDrafts() {
    const merged = this.savedDrafts();
    for (const key of this.dirtyDrafts) {
      const text = this.drafts.get(key);
      if (text) merged.set(key, text); else merged.delete(key);
    }
    return merged;
  }
  setDraft(key: string, text: string) {
    if (!key || key.length > 512) throw new Error('Invalid draft identity');
    if (text) {
      const next = this.mergedDrafts();
      next.set(key, text);
      this.validateDrafts(next);
    }
    const wasUnsaved = this.hasUnsavedDrafts();
    if (text) this.drafts.set(key, text); else this.drafts.delete(key);
    const hadDraft = this.draftIdentities.has(key);
    if (text) this.draftIdentities.add(key); else this.draftIdentities.delete(key);
    this.dirtyDrafts.add(key);
    for (const listener of this.draftListeners.get(key) ?? []) listener();
    clearTimeout(this.draftTimer);
    this.draftTimer = setTimeout(() => this.flushDrafts(), 150);
    if (hadDraft !== !!text || wasUnsaved !== this.hasUnsavedDrafts()) this.update({});
  }
  hasUnsavedDrafts() {
    return this.dirtyDrafts.size > 0 || (this.platform.storage.persistent === false && this.draftIdentities.size > 0);
  }
  /** Flush text synchronously and tell the host whether closing could lose changes. */
  flushDrafts(): { saved: boolean; error?: string } {
    clearTimeout(this.draftTimer);
    this.draftTimer = undefined;
    const wasUnsaved = this.hasUnsavedDrafts();
    try {
      // Explicit deletion must remain possible even when externally written
      // drafts already exceed the aggregate admission bound.
      for (const key of this.dirtyDrafts) {
        if (this.drafts.has(key)) continue;
        if (this.platform.storage.persistent !== false && this.platform.storage.getItem(draftStoragePrefix + key))
          this.durableDrafts.add(key);
        this.platform.storage.removeItem(draftStoragePrefix + key);
        // A fallback-only deletion cannot remove the old disk entry. Keep its
        // tombstone dirty so a close never claims that change was saved.
        if (this.platform.storage.persistent === false && this.durableDrafts.has(key)) continue;
        this.durableDrafts.delete(key);
        this.dirtyDrafts.delete(key);
      }
      if (this.dirtyDrafts.size) this.validateDrafts(this.mergedDrafts());
      // Each recipient has its own atomic Storage entry. Synchronous pagehide
      // writes never replace another tab's unrelated drafts or need a lock.
      for (const key of this.dirtyDrafts) {
        const text = this.drafts.get(key);
        if (!text) continue;
        this.platform.storage.setItem(draftStoragePrefix + key, text);
        if (this.platform.storage.persistent !== false) this.durableDrafts.add(key);
        this.drafts.delete(key);
        this.dirtyDrafts.delete(key);
      }
    } catch (error) {
      const message = 'Drafts could not be saved; keep this page open to retain them.';
      this.report(new Error(message, { cause: error }));
      return { saved: false, error: message };
    }
    const unsaved = this.hasUnsavedDrafts();
    if (wasUnsaved !== unsaved) this.update({});
    return unsaved
      ? { saved: false, error: 'Device storage is unavailable. Closing or reloading may lose draft changes; keep this page open to retain them.' }
      : { saved: true };
  }
  /** Explicitly forget unsent draft text without touching command identities. */
  discardDrafts() {
    clearTimeout(this.draftTimer);
    this.draftTimer = undefined;
    const keys = new Set([
      ...this.platform.storage.keys().filter(key => key.startsWith(draftStoragePrefix)),
      ...[...this.drafts.keys(), ...this.dirtyDrafts].map(key => draftStoragePrefix + key),
    ]);
    for (const key of keys) {
      const recipient = key.slice(draftStoragePrefix.length);
      this.drafts.delete(recipient);
      this.draftIdentities.delete(recipient);
      this.dirtyDrafts.add(recipient);
      for (const listener of this.draftListeners.get(recipient) ?? []) listener();
    }
    const flushed = this.flushDrafts();
    this.compositions.clearAll();
    this.update({});
    if (!flushed.saved) throw new Error(flushed.error);
  }
  private recoveryStorage(): RecoveryStorage {
    const key = 'whip.web.recovery.v1';
    const read = (): RecoveryRecord[] => {
      const records = readPreference<unknown>(this.platform.storage, key, []);
      if (!Array.isArray(records)) return [];
      return records.filter(
        (record): record is RecoveryRecord =>
          !!record &&
          record.version === 1 &&
          typeof record.runtimeId === 'string' &&
          typeof record.clientId === 'string' &&
          typeof record.commandId === 'string' &&
          typeof record.operation === 'string',
      );
    };
    const same = (a: RecoveryRecord, b: RecoveryRecord) =>
      a.runtimeId === b.runtimeId &&
      a.clientId === b.clientId &&
      a.commandId === b.commandId;
    const update = (write: () => void) =>
      this.platform.storage.transaction
        ? this.platform.storage.transaction(key, write)
        : Promise.resolve().then(write);
    return {
      list: async () => read(),
      put: (record) =>
        update(() => {
          const records = read().filter((item) => !same(item, record));
          if (records.length >= 1024)
            throw new Error(
              'Command recovery storage is full. Inspect and forget old commands before submitting more.',
            );
          this.platform.storage.setItem(
            key,
            JSON.stringify([...records, record]),
          );
        }),
      delete: (record) =>
        update(() =>
          this.platform.storage.setItem(
            key,
            JSON.stringify(read().filter((item) => !same(item, record))),
          ),
        ),
    };
  }
  async connect(): Promise<void> {
    try { await this.connections.connect(); }
    catch (error) {
      if (!this.closed && this.connections.home().error !== errorMessage(error)) this.report(error);
      throw error;
    }
  }
  acquireView(runtimeId: string, rootId: string): { view: SessionView; release(): void } {
    const client = this.connections.host(runtimeId)?.client;
    if (!client) throw new Error('Connect to a host first');
    const key = JSON.stringify([runtimeId, rootId]);
    let lease = this.views.get(key);
    if (!lease) {
      for (const [id, candidate] of this.views) {
        if (this.views.size < 4) break;
        if (!candidate.users) this.dropView(id, candidate);
      }
      if (this.views.size >= 4)
        throw new Error(
          'Four session views are already open. Close a view before opening another.',
        );
      lease = { client, runtimeId, view: createSessionView(client.session(rootId)), users: 0 };
      this.views.set(key, lease);
      // Snapshot and history failures belong to the view's scoped error state.
      void lease.view.start().catch(() => {});
    }
    // Reusing a root moves it behind older inactive views in eviction order.
    this.views.delete(key);
    this.views.set(key, lease);
    clearTimeout(lease.timer);
    lease.users++;
    const retained = lease;
    let released = false;
    return {
      view: lease.view,
      release: () => {
        if (released) return;
        released = true;
        retained.users--;
        if (!retained.users && this.views.get(key) === retained)
          retained.timer = setTimeout(
            () => this.dropView(key, retained),
            30_000,
          );
      },
    };
  }
  /** A child's history remains open until its last visible consumer leaves. */
  acquireAgent(view: SessionView, agentId: string): () => void {
    if (agentId === view.session.rootId) return () => {};
    let readers = this.agentReaders.get(view);
    if (!readers) { readers = new Map(); this.agentReaders.set(view, readers); }
    let reader = readers.get(agentId);
    if (!reader) {
      reader = { users: 0 };
      readers.set(agentId, reader);
      void view.openAgent(agentId).catch(() => {});
    }
    reader.users++;
    const retained = reader;
    let released = false;
    return () => {
      if (released) return;
      released = true;
      retained.users--;
      // StrictMode and pane transfers can release/reacquire in the same commit.
      queueMicrotask(() => {
        if (!retained.users && readers.get(agentId) === retained) {
          readers.delete(agentId);
          view.closeAgent(agentId);
        }
      });
    };
  }
  private dropView(id: string, lease: ViewLease) {
    clearTimeout(lease.timer);
    if (this.views.get(id) === lease) this.views.delete(id);
    void lease.view.dispose();
  }
  private commandNotice(notice: CommandNotice) {
    const notices = [
      ...this.state.commands.filter((item) => item.id !== notice.id),
      Object.freeze(notice),
    ];
    const recent = new Set(
      notices
        .filter((item) => !item.delivery)
        .slice(-32)
        .map((item) => item.id),
    );
    this.update({
      commands: notices.filter((item) => item.delivery || recent.has(item.id)),
    });
  }
  checkCommand(commandId: string): Promise<unknown> {
    const pending = this.pending.get(commandId);
    if (!pending)
      return Promise.reject(
        new Error(
          'This command has no unresolved local delivery. Inspect its stored recovery record.',
        ),
      );
    return pending.check();
  }
  retryCommand(commandId: string): Promise<unknown> {
    const pending = this.pending.get(commandId);
    if (
      !pending ||
      this.state.commands.find((item) => item.id === commandId)?.delivery !==
        'absent'
    )
      return Promise.reject(
        new Error(
          'Retry is available only after the daemon confirms the original command is absent.',
        ),
      );
    return pending.retry();
  }
  run<O extends CommandOperation>(
    handle: CommandHandle<O>,
    label: string,
    onAccepted?: () => void,
    draftKey?: string,
  ): Promise<CommandOutcome<O>> {
    const { runtimeId, clientId } = handle.record;
    const signal = this.connections.signal(handle.client);
    const id = JSON.stringify([runtimeId, clientId, handle.commandId]);
    const attached = () => this.connections.isAttached(handle.client);
    let accepted = false;
    let uncertain = false;
    let work: Promise<CommandOutcome<O>> | undefined;
    const notice = (
      status: string,
      extra: Pick<CommandNotice, 'error' | 'delivery'> = {},
    ) =>
      this.commandNotice({
        id,
        commandId: handle.commandId,
        runtimeId,
        label,
        draftKey,
        status,
        ...extra,
      });
    const lookup = async () => {
      for (;;) {
        await handle.client.whenConnected(signal);
        try {
          return await handle.status({ signal });
        } catch (error) {
          if (
            signal.aborted ||
            handle.client.getSnapshot().state !== 'reconnecting'
          )
            throw error;
        }
      }
    };
    const observe = async (
      mode: 'initial' | 'check' | 'retry',
    ): Promise<CommandOutcome<O>> => {
      let terminal = false;
      try {
        let receipt: CommandOutcome<O>;
        try {
          receipt = await (mode === 'initial'
            ? handle.accepted({ signal })
            : mode === 'retry'
              ? handle.retry({ signal })
              : lookup());
        } catch (error) {
          if (!(error instanceof DeliveryUncertainError)) throw error;
          signal.throwIfAborted();
          if (!attached())
            throw new Error('Host changed while submitting');
          uncertain = true;
          this.pending.set(id, pending);
          if (!signal.aborted && attached())
            notice('Checking acceptance', { delivery: 'uncertain' });
          receipt = await lookup();
        }
        signal.throwIfAborted();
        if (!attached())
          throw new Error('Host changed while submitting');
        this.pending.delete(id);
        this.submittedInputs.acknowledge(receipt, runtimeId);
        notice(receipt.status);
        if (!accepted) {
          accepted = true;
          onAccepted?.();
        }
        const outcome = await handle.result({ signal });
        signal.throwIfAborted();
        if (!attached()) throw new Error('Host changed while awaiting the command');
        this.submittedInputs.acknowledge(outcome, runtimeId);
        terminal = true;
        notice(
          outcome.status,
          outcome.failure ? { error: outcome.failure.message } : {},
        );
        if (outcome.status !== 'succeeded')
          throw new Error(
            outcome.failure?.message ?? `${label}: ${outcome.status}`,
          );
        await this.queries.invalidateQueries({ predicate: query => query.queryKey[1] === runtimeId });
        signal.throwIfAborted();
        if (!attached()) throw new Error('Host changed while refreshing the command result');
        return outcome;
      } catch (error) {
        if (!signal.aborted && attached()) {
          if (!uncertain || accepted) this.submittedInputs.remove(handle.commandId, runtimeId);
          if (uncertain && !accepted) {
            this.pending.set(id, pending);
            const absent =
              error instanceof RpcError && error.kind === 'command_not_found';
            notice(
              absent
                ? 'Not accepted · explicit retry available'
                : 'Acceptance unresolved',
              {
                delivery: absent ? 'absent' : 'uncertain',
                error: errorMessage(error),
              },
            );
          } else if (!terminal)
            notice('Needs attention', { error: errorMessage(error) });
          this.report(error);
        }
        throw error;
      }
    };
    const start = (mode: 'initial' | 'check' | 'retry') => {
      if (!work)
        work = observe(mode).finally(() => {
          work = undefined;
        });
      return work;
    };
    const pending: PendingCommand = {
      client: handle.client,
      check: () => start('check'),
      retry: () => start('retry'),
    };
    notice('Submitting');
    return start('initial');
  }
  dispose() {
    if (this.closed) return;
    this.flushDrafts();
    this.closed = true;
    this.connections.dispose();
    this.submittedInputs.clear();
    this.pending.clear();
    this.queries.clear();
    this.tabs.dispose();
    this.compositions.dispose();
    this.readingPositions.clear();
    this.listeners.clear();
    this.draftListeners.clear();
  }
}
