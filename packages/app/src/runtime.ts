import { QueryClient } from '@tanstack/react-query';
import {
  createWhipClient,
  DeliveryUncertainError,
  RpcError,
  type WhipClient,
  type RecoveryRecord,
  type RecoveryStorage,
  type CommandHandle,
  type CommandOutcome,
} from '@whip/sdk';
import {
  createSessionListView,
  createSessionView,
  type SessionListView,
  type SessionView,
} from '@whip/sdk/state';
import type { CommandOperation } from '@whip/protocol';
import { errorMessage, readPreference, type AppPlatform } from './platform';
import { SessionTabs } from './session-tabs';
import { CompositionStore } from './compositions';
import { ReadingPositions } from './reading-positions';

interface ViewLease {
  view: SessionView;
  users: number;
  timer?: ReturnType<typeof setTimeout>;
}
export interface CommandNotice {
  id: string;
  label: string;
  status: string;
  error?: string;
  draftKey?: string;
  delivery?: 'uncertain' | 'absent';
}
interface PendingCommand {
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
}
const defaultPreferences: DevicePreferences = {
  commandShortcut: 'Mod+K',
  composerShortcut: 'Mod+Shift+L',
  attentionAnnouncements: true,
};
interface RuntimeSnapshot {
  preferences: DevicePreferences;
  hosts: readonly string[];
  client?: WhipClient;
  list?: SessionListView;
  endpoint: string;
  error?: string;
  commands: readonly CommandNotice[];
}

/** Owns UI observation lifetimes; accepted execution continues after disposal. */
export class AppRuntime {
  readonly tabs: SessionTabs;
  readonly compositions = new CompositionStore();
  readonly readingPositions = new ReadingPositions();
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
  private readonly draftIdentities = new Set<string>();
  private readonly pending = new Map<string, PendingCommand>();
  private draftTimer?: ReturnType<typeof setTimeout>;
  private readonly dirtyDrafts = new Set<string>();
  private unsubscribe?: () => void;
  private waits = new AbortController();
  private epoch = 0;
  private closed = false;

  constructor(readonly platform: AppPlatform) {
    const saved = readPreference<unknown>(
      platform.storage,
      'whip.web.endpoint',
      platform.defaultEndpoint,
    );
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
      endpoint: typeof saved === 'string' ? saved : platform.defaultEndpoint,
      commands: [],
      hosts: Array.isArray(hosts)
        ? hosts
            .filter(
              (host): host is string =>
                typeof host === 'string' && host.length <= 2048,
            )
            .slice(0, 16)
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
      },
    };
    this.tabs = new SessionTabs(platform.windowStorage, message => this.report(message));
    let previousTabs = this.tabs.getSnapshot();
    this.tabs.subscribe(() => {
      const next = this.tabs.getSnapshot();
      for (const workspace of previousTabs.workspaces) {
        const retained = next.workspaces.find(item => item.runtimeId === workspace.runtimeId);
        const roots = new Set([...(retained?.tabs ?? []).map(item => item.rootId), ...(retained?.closed ?? []).map(item => item.tab.rootId)]);
        for (const rootId of [...workspace.tabs.map(item => item.rootId), ...workspace.closed.map(item => item.tab.rootId)]) {
          if (!roots.has(rootId)) this.readingPositions.forgetRoot(workspace.runtimeId, rootId);
        }
      }
      previousTabs = next;
    });
    try { for (const key of this.savedDrafts().keys()) this.draftIdentities.add(key); } catch (error) { this.report(error); }
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
  forgetHost(endpoint: string) {
    const hosts = this.state.hosts.filter((host) => host !== endpoint);
    this.platform.storage.setItem('whip.web.hosts.v1', JSON.stringify(hosts));
    this.update({ hosts });
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
    this.update({ error: errorMessage(error) });
  }
  draft(key: string) {
    if (!this.dirtyDrafts.has(key)) {
      try {
        const text = this.platform.storage.getItem(draftStoragePrefix + key);
        if (text) {
          this.validateDrafts(new Map([[key, text]]));
          return text;
        }
        return '';
      } catch (error) { this.report(error); }
    }
    return this.drafts.get(key) ?? '';
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
      // Stop reading oversized storage before retaining unbounded values.
      this.validateDrafts(saved);
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
    if (text) this.drafts.set(key, text); else this.drafts.delete(key);
    const hadDraft = this.draftIdentities.has(key);
    if (text) this.draftIdentities.add(key); else this.draftIdentities.delete(key);
    this.dirtyDrafts.add(key);
    clearTimeout(this.draftTimer);
    this.draftTimer = setTimeout(() => this.flushDrafts(), 150);
    if (hadDraft !== !!text) this.update({});
  }
  flushDrafts() {
    clearTimeout(this.draftTimer);
    this.draftTimer = undefined;
    if (!this.dirtyDrafts.size) return;
    try {
      // Explicit deletion must remain possible even when externally written
      // drafts already exceed the aggregate admission bound.
      for (const key of this.dirtyDrafts) {
        if (this.drafts.has(key)) continue;
        this.platform.storage.removeItem(draftStoragePrefix + key);
        this.dirtyDrafts.delete(key);
      }
      if (!this.dirtyDrafts.size) return;
      this.validateDrafts(this.mergedDrafts());
      // Each recipient has its own atomic Storage entry. Synchronous pagehide
      // writes never replace another tab's unrelated drafts or need a lock.
      for (const key of this.dirtyDrafts) {
        const text = this.drafts.get(key);
        this.platform.storage.setItem(draftStoragePrefix + key, text!);
        this.drafts.delete(key);
        this.dirtyDrafts.delete(key);
      }
    } catch (error) {
      this.report(new Error('Drafts could not be saved; keep this page open to retain them.', { cause: error }));
    }
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
      this.platform.storage.removeItem(key);
      const recipient = key.slice(draftStoragePrefix.length);
      this.drafts.delete(recipient);
      this.draftIdentities.delete(recipient);
      this.dirtyDrafts.delete(recipient);
    }
    this.compositions.clearAll();
    this.update({});
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
  async connect(endpoint = this.state.endpoint): Promise<void> {
    let epoch = this.epoch;
    try {
      if (this.closed) throw new Error('Application has been disposed');
      const url = new URL(endpoint);
      if (
        !['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol) ||
        url.username ||
        url.password
      )
        throw new Error(
          'Enter an HTTP or WebSocket daemon endpoint without credentials',
        );
      let clientId = this.platform.storage.getItem('whip.web.client.v1');
      if (!clientId) {
        if (!globalThis.crypto?.randomUUID)
          throw new Error(
            'This browser needs HTTPS (or localhost) to connect to WHIP. Open the host’s HTTPS address.',
          );
        clientId = crypto.randomUUID();
        this.platform.storage.setItem('whip.web.client.v1', clientId);
      }
      this.platform.storage.setItem(
        'whip.web.endpoint',
        JSON.stringify(endpoint),
      );
      const client = createWhipClient({
        endpoint,
        clientId,
        clientKind: 'human',
        recoveryStorage: this.recoveryStorage(),
      });
      const list = createSessionListView(client);
      epoch = ++this.epoch;
      this.detach();
      let previousConnection: string | undefined;
      this.unsubscribe = client.subscribe(() => {
        const info = client.getSnapshot().info;
        if (info?.connection_id && info.connection_id !== previousConnection) {
          previousConnection = info.connection_id;
          void this.queries.invalidateQueries();
        }
      });
      this.update({ client, list, endpoint, error: undefined, commands: [] });
      await list.start();
      if (epoch !== this.epoch)
        throw new Error('Host changed while connecting');
      await client.connect();
      if (epoch !== this.epoch) throw new Error('Host changed while connecting');
      const hosts = [
        endpoint,
        ...this.state.hosts.filter((host) => host !== endpoint),
      ].slice(0, 16);
      this.platform.storage.setItem('whip.web.hosts.v1', JSON.stringify(hosts));
      this.update({ hosts });
    } catch (error) {
      // Connection failures already belong to the SDK's recoverable state.
      if (!this.closed && epoch === this.epoch && this.state.client?.getSnapshot().error !== error) this.report(error);
      throw error;
    }
  }
  acquireView(rootId: string): { view: SessionView; release(): void } {
    const client = this.state.client;
    if (!client) throw new Error('Connect to a host first');
    let lease = this.views.get(rootId);
    if (!lease) {
      for (const [id, candidate] of this.views) {
        if (this.views.size < 4) break;
        if (!candidate.users) this.dropView(id, candidate);
      }
      if (this.views.size >= 4)
        throw new Error(
          'Four session views are already open. Close a view before opening another.',
        );
      lease = { view: createSessionView(client.session(rootId)), users: 0 };
      this.views.set(rootId, lease);
      // Snapshot and history failures belong to the view's scoped error state.
      void lease.view.start().catch(() => {});
    }
    // Reusing a root moves it behind older inactive views in eviction order.
    this.views.delete(rootId);
    this.views.set(rootId, lease);
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
        if (!retained.users && this.views.get(rootId) === retained)
          retained.timer = setTimeout(
            () => this.dropView(rootId, retained),
            30_000,
          );
      },
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
    const epoch = this.epoch;
    const signal = this.waits.signal;
    let accepted = false;
    let uncertain = false;
    let work: Promise<CommandOutcome<O>> | undefined;
    const notice = (
      status: string,
      extra: Pick<CommandNotice, 'error' | 'delivery'> = {},
    ) =>
      this.commandNotice({
        id: handle.commandId,
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
          if (epoch !== this.epoch)
            throw new Error('Host changed while submitting');
          uncertain = true;
          this.pending.set(handle.commandId, pending);
          if (!signal.aborted && epoch === this.epoch)
            notice('Checking acceptance', { delivery: 'uncertain' });
          receipt = await lookup();
        }
        signal.throwIfAborted();
        if (epoch !== this.epoch)
          throw new Error('Host changed while submitting');
        this.pending.delete(handle.commandId);
        notice(receipt.status);
        if (!accepted) {
          accepted = true;
          onAccepted?.();
        }
        const outcome = await handle.result({ signal });
        signal.throwIfAborted();
        if (epoch !== this.epoch) throw new Error('Host changed while awaiting the command');
        terminal = true;
        notice(
          outcome.status,
          outcome.failure ? { error: outcome.failure.message } : {},
        );
        if (outcome.status !== 'succeeded')
          throw new Error(
            outcome.failure?.message ?? `${label}: ${outcome.status}`,
          );
        await this.queries.invalidateQueries();
        signal.throwIfAborted();
        if (epoch !== this.epoch) throw new Error('Host changed while refreshing the command result');
        return outcome;
      } catch (error) {
        if (!signal.aborted && epoch === this.epoch) {
          if (uncertain && !accepted) {
            this.pending.set(handle.commandId, pending);
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
      check: () => start('check'),
      retry: () => start('retry'),
    };
    notice('Submitting');
    return start('initial');
  }
  private detach() {
    this.compositions.invalidateRuntime(this.state.client?.getSnapshot().info?.runtime_id);
    this.waits.abort();
    this.waits = new AbortController();
    this.pending.clear();
    this.unsubscribe?.();
    this.unsubscribe = undefined;
    for (const [id, lease] of this.views) this.dropView(id, lease);
    void this.state.list?.dispose();
    this.state.client?.close();
    this.queries.clear();
  }
  dispose() {
    if (this.closed) return;
    this.flushDrafts();
    this.closed = true;
    ++this.epoch;
    this.detach();
    this.tabs.dispose();
    this.compositions.dispose();
    this.readingPositions.clear();
    this.listeners.clear();
  }
}
