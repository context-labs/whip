import { QueryClient } from '@tanstack/react-query';
import { rememberProviderReady } from './provider-readiness';
import { readModelCatalog } from './model-options';
import {
  DeliveryError, DurableCommand, RecoveryError, RecoveryJournal, RecoveryPersistenceError,
  type Client, type DurableMethod, type Operations,
  type Admission,
} from '@whip/sdk';
import { createExecutionView, createSessionView, createTraceView, type ExecutionView, type SessionView, type TraceView } from '@whip/sdk/state';
import { recoveryStorage } from './recovery-storage';
import { errorMessage, readPreference, type AppPlatform } from './platform';
import { parseSettingsReturn, settingsReturnKey, type SettingsReturn } from './settings/navigation';
import { isSessionTab, SessionTabs, welcomeDraftKey } from './session-tabs';
import { BrowserAssociations } from './browser-provider';
import { BrowserWorkspace } from './browser-workspace';
import { CompositionStore } from './compositions';
import { ReadingPositions } from './reading-positions';
import { SubmittedInputs } from './input-presentation';
import { HostConnections, type HostConnection } from './hosts';

interface ViewLease {
  client: Client;
  runtimeId: string;
  rootId: string;
  view: SessionView;
  execution: ExecutionView;
  users: number;
  timer?: ReturnType<typeof setTimeout>;
}
interface TraceLease {
  client: Client;
  runtimeId: string;
  rootId: string;
  view: TraceView;
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
  turnId?: string;
  delivery?: 'uncertain' | 'absent';
}
interface PendingCommand {
  client: Client;
  check(): Promise<unknown>;
  retry(): Promise<unknown>;
}
const draftStoragePrefix = 'whip.web.draft.v1:';
const draftRevisionPrefix = 'whip.web.draft-revision.v1:';
const encodedBytes = (value: string) =>
  new TextEncoder().encode(value).byteLength;
export const commandShortcuts = ['Mod+K', 'Mod+P', 'Mod+Shift+P'] as const;
export const composerShortcuts = [
  'Mod+Shift+L',
  'Mod+Shift+J',
  'Mod+Shift+F',
] as const;
export const terminalShortcuts = ['Control+`', 'Mod+`', 'Mod+Shift+T'] as const;
export interface DevicePreferences {
  toolDensity: 'compact' | 'comfortable' | 'detailed';
  commandShortcut: (typeof commandShortcuts)[number];
  composerShortcut: (typeof composerShortcuts)[number];
  terminalShortcut: (typeof terminalShortcuts)[number];
  attentionAnnouncements: boolean;
  desktopNotifications: boolean;
}
const defaultPreferences: DevicePreferences = {
  toolDensity: 'compact',
  commandShortcut: 'Mod+K',
  composerShortcut: 'Mod+Shift+L',
  terminalShortcut: 'Control+`',
  attentionAnnouncements: true,
  desktopNotifications: false,
};
interface RuntimeSnapshot {
  errorTitle?: string;
  preferences: DevicePreferences;
  hosts: readonly HostConnection[];
  home?: HostConnection;
  legacyHosts: readonly string[];
  profilesReady: boolean;
  profileError?: string;
  selectedHostId?: string;
  error?: string;
  workspaceError?: string;
  commands: readonly CommandNotice[];
}

/** Owns UI observation lifetimes; accepted execution continues after disposal. */
export class AppRuntime {
  readonly browser: BrowserWorkspace;
  readonly browserAssociations: BrowserAssociations;
  readonly connections: HostConnections;
  readonly tabs: SessionTabs;
  readonly compositions = new CompositionStore(client => this.connections.isAttached(client));
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
  private readonly traces = new Map<string, TraceLease>();
  private readonly titleListeners = new Map<Client, () => void>();
  private readonly drafts = new Map<string, string>();
  private readonly draftRevisions = new Map<string, string>();
  private readonly draftListeners = new Map<string, Set<() => void>>();
  readonly recovery: RecoveryJournal;
  private readonly draftIdentities = new Set<string>();
  private readonly durableDrafts = new Set<string>();
  private readonly pending = new Map<string, PendingCommand>();
  private draftTimer?: ReturnType<typeof setTimeout>;
  private readonly dirtyDrafts = new Set<string>();
  private closed = false;
  settingsReturn?: SettingsReturn;

  constructor(readonly platform: AppPlatform) {
    // New Chat can unmount completely between drafts. Keep only its host metadata warm.
    for (const key of ['provider-list', 'provider-presets', 'provider-readiness', 'provider-catalogs', 'host-permission-default', 'host-execution-defaults', 'mcp-configuration', 'definitions'])
      this.queries.setQueryDefaults([key], { gcTime: 5 * 60_000 });
    try {
      const saved = platform.windowStorage?.getItem(settingsReturnKey);
      if (saved && saved.length <= 4096) this.settingsReturn = parseSettingsReturn(JSON.parse(saved));
    } catch { /* Return to the retained workspace when navigation storage is unavailable. */ }
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
        toolDensity: preferences?.toolDensity === 'comfortable' || preferences?.toolDensity === 'detailed' ? preferences.toolDensity : 'compact',
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
        terminalShortcut: terminalShortcuts.includes(preferences?.terminalShortcut as never)
          ? preferences!.terminalShortcut!
          : defaultPreferences.terminalShortcut,
        attentionAnnouncements: preferences?.attentionAnnouncements !== false,
        desktopNotifications: preferences?.desktopNotifications === true,
      },
    };
    this.recovery = new RecoveryJournal(recoveryStorage(platform.storage));
    this.tabs = new SessionTabs(platform.windowStorage, message => this.report(message), this.lastSession()?.runtimeId);
    this.browser = new BrowserWorkspace(platform.browser, this.tabs, error => this.reportWorkspace(error));
    let previousTabs = this.tabs.getSnapshot();
    this.tabs.subscribe(() => {
      const next = this.tabs.getSnapshot();
      const before = previousTabs.workspace, after = next.workspace;
      const views = new Set([...after.tabs, ...after.closed.map(item => item.tab)].map(tab => tab.id));
      for (const tab of [...before.tabs, ...before.closed.map(item => item.tab)]) {
        if ((isSessionTab(tab) || tab.kind === 'terminal') && !views.has(tab.id)) this.readingPositions.forgetView(tab.runtimeId, tab.id);
        if (tab.kind === 'new' && !views.has(tab.id)) this.compositions.clear(welcomeDraftKey(tab.id));
      }
      previousTabs = next;
    });
    try {
      // Earlier builds journaled the first message separately and froze a copy of
      // its text as a draft. Both are retired; the copies must not count toward the bound.
      for (const key of platform.storage.keys()) {
        if (/^whip\.web\.welcome(?:-import)?\.v[12]:/.test(key)) platform.storage.removeItem(key);
        else if (key.startsWith(draftStoragePrefix) && /(?::submission$|:welcome:)/.test(key)) {
          platform.storage.removeItem(key);
          platform.storage.removeItem(draftRevisionPrefix + key.slice(draftStoragePrefix.length));
        }
      }
      const saved = this.savedDrafts();
      for (const key of saved.keys()) this.draftIdentities.add(key);
      // An interrupted two-key write can leave revision-only metadata. It carries
      // no text and must not accumulate outside the existing draft count bound.
      for (const key of platform.storage.keys())
        if (key.startsWith(draftRevisionPrefix) && !saved.has(key.slice(draftRevisionPrefix.length))) platform.storage.removeItem(key);
    } catch (error) { this.report(error); }
    this.connections = new HostConnections(platform, recoveryStorage(platform.storage), {
      connected: (runtimeId, client) => {
        this.observeSessionTitles(runtimeId, client);
        void this.queries.invalidateQueries({ predicate: query => query.queryKey[1] === runtimeId });
        void this.primeProviders(runtimeId, client);
        for (const lease of this.views.values()) if (lease.runtimeId === runtimeId && lease.client !== client) {
          lease.client = client;
          void Promise.all([lease.view.reconnect(client), lease.execution.reconnect(client)]).catch(error => this.report(error));
        }
        for (const lease of this.traces.values()) if (lease.runtimeId === runtimeId && lease.client !== client) {
          lease.client = client;
          void lease.view.reconnect(client).catch(error => this.report(error));
        }
      },
      detached: (client, runtimeId, options?: { recovering: boolean }) => {
        this.titleListeners.get(client)?.();
        this.titleListeners.delete(client);
        if (runtimeId) this.compositions.invalidateRuntime(runtimeId, { preserveUploaded: options?.recovering });
        // Durable unresolved commands survive transport replacement; only an explicit
        // recovery action may bind their exact record to a matching new client.
        for (const [id, lease] of this.views) if (lease.client === client) {
          if (options?.recovering) void Promise.all([lease.view.suspend(), lease.execution.suspend()]); else this.dropView(id, lease);
        }
        for (const [id, lease] of this.traces) if (lease.client === client) {
          if (options?.recovering) void lease.view.suspend(); else this.dropTrace(id, lease);
        }
        if (runtimeId) this.queries.removeQueries({ predicate: query => query.queryKey[1] === runtimeId });
      },
    });
    this.browserAssociations = new BrowserAssociations(platform.browserAgent, this.connections, this.tabs, this.browser, error => this.reportWorkspace(error));
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
    this.notify();
  }
  private notify() {
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
  rememberSettingsReturn(value: SettingsReturn) {
    this.settingsReturn = parseSettingsReturn(value);
    try { this.platform.windowStorage?.setItem(settingsReturnKey, JSON.stringify(this.settingsReturn)); }
    catch { this.report('The return location is kept for this window, but could not be saved.'); }
  }
  clearSettingsReturn() {
    this.settingsReturn = undefined;
    try { this.platform.windowStorage?.removeItem(settingsReturnKey); }
    catch { this.report('The previous Settings return location could not be cleared.'); }
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
  private observeSessionTitles(runtimeId: string, client: Client) {
    this.titleListeners.get(client)?.();
    const catalog = this.connections.host(runtimeId)?.list;
    if (!catalog) return;
    let revision = catalog.getSnapshot().revision;
    const off = catalog.subscribe(() => {
      const next = catalog.getSnapshot();
      if (next.revision === null || next.revision === revision) return;
      revision = next.revision;
      // The SDK catalog head includes changes outside the visible page.
      const filters = { predicate: (query: { queryKey: readonly unknown[] }) => query.queryKey[1] === runtimeId
        && ['session-tab-summaries', 'session-sidebar-summaries', 'session-search', 'host-attention'].includes(query.queryKey[0] as string) };
      void this.queries.cancelQueries(filters).then(() => {
        if (this.connections.isAttached(client)) return this.queries.invalidateQueries(filters);
      });
    });
    this.titleListeners.set(client, off);
  }
  /** Warm host metadata; readiness remains a separate local-evidence query. */
  primeProviders(runtimeId?: string, client = runtimeId ? this.connections.host(runtimeId)?.client : undefined): Promise<void> {
    if (!runtimeId || !client) return Promise.resolve();
    return this.queries.prefetchQuery({ queryKey: ['provider-list', runtimeId], queryFn: ({ signal }) => client.listProviders({ signal }) })
      .then(async () => {
        if (!this.connections.isAttached(client)) return;
        await Promise.all([
          this.queries.prefetchQuery({ queryKey: ['provider-presets', runtimeId], queryFn: ({ signal }) => client.providerPresets({ signal }) }),
          this.queries.prefetchQuery({ queryKey: ['provider-catalogs', runtimeId, ''], queryFn: ({ signal }) => readModelCatalog(client, signal) }),
          this.queries.prefetchQuery({ queryKey: ['host-permission-default', runtimeId, client.processEpoch], queryFn: ({ signal }) => client.getDefaultPermissionMode({ signal }) }),
          this.queries.prefetchQuery({ queryKey: ['host-execution-defaults', runtimeId, client.processEpoch], queryFn: ({ signal }) => client.hosts.executionDefaults({ signal }) }),
          this.queries.prefetchQuery({ queryKey: ['mcp-configuration', runtimeId], queryFn: ({ signal }) => client.mcpConfiguration({ signal }) }),
        ]);
        const inventory = this.queries.getQueryData<Operations['providers.list']['result']>(['provider-list', runtimeId]);
        if (!inventory) return;
        if (!inventory.defaults) { rememberProviderReady(this.platform.storage, runtimeId, false); return; }
        const queryKey = ['provider-readiness', runtimeId, inventory.defaults];
        await this.queries.prefetchQuery({ queryKey,
          queryFn: ({ signal }) => client.providerReadiness(inventory.defaults!, { signal }) });
        const ready = this.queries.getQueryState<Operations['providers.readiness']['result']>(queryKey);
        if (ready?.status === 'success' && ready.data && this.connections.isAttached(client))
          rememberProviderReady(this.platform.storage, runtimeId, ready.data.configured && ['available', 'not_required'].includes(ready.data.credential_state));
      });
  }
  /** Remove local state only after deletion has succeeded on this runtime. */
  forgetSession(runtimeId: string, rootId: string) {
    const prefix = `${runtimeId}:${rootId}:`;
    const matches = (tab: { runtimeId?: string; rootId?: string }) => tab.runtimeId === runtimeId && tab.rootId === rootId;
    const workspace = this.tabs.workspace();
    const viewIds = [...workspace.tabs, ...workspace.closed.map(item => item.tab)].filter(tab => tab.kind !== 'browser' && matches(tab)).map(tab => tab.id);
    this.compositions.clearSession(runtimeId, rootId, viewIds);
    this.tabs.purge(runtimeId, rootId);
    for (const [key, lease] of this.views) if (lease.runtimeId === runtimeId && lease.rootId === rootId) this.dropView(key, lease);
    for (const [key, lease] of this.traces) if (lease.runtimeId === runtimeId && lease.rootId === rootId) this.dropTrace(key, lease);
    this.queries.removeQueries({ predicate: query => query.queryKey[1] === runtimeId && query.queryKey[2] === rootId });
    for (const input of this.submittedInputs.getSnapshot())
      if (matches(input)) this.submittedInputs.remove(input.id, runtimeId);
    const keys = new Set([...this.draftIdentities, ...this.drafts.keys(), ...this.dirtyDrafts, ...this.durableDrafts]);
    try {
      for (const key of this.platform.storage.keys())
        if (key.startsWith(draftStoragePrefix + prefix)) keys.add(key.slice(draftStoragePrefix.length));
    } catch (error) {
      this.report(new Error('The session was deleted, but some saved drafts could not be found in device storage.', { cause: error }));
    }
    for (const key of keys) {
      if (!key.startsWith(prefix)) continue;
      this.drafts.delete(key);
      this.draftIdentities.delete(key);
      this.dirtyDrafts.add(key);
      for (const listener of this.draftListeners.get(key) ?? []) listener();
    }
    this.flushDrafts();
    const last = this.lastSession();
    if (last && matches(last)) {
      try { this.platform.storage.removeItem('whip.web.last-session.v1'); }
      catch (error) { this.report(new Error('The session was deleted, but its last-session shortcut could not be cleared from device storage.', { cause: error })); }
    }
    this.update({});
  }
  /** Navigation and tab actions remain beside the workspace controls. */
  reportWorkspace(error: unknown) {
    if (error instanceof Error && error.name === 'AbortError') return;
    this.update({ workspaceError: errorMessage(error) });
  }
  clearWorkspaceError() { this.update({ workspaceError: undefined }); }
  clearError() {
    this.update({ error: undefined, errorTitle: undefined });
  }
  report(error: unknown, title?: string) {
    // Disposing a view or detaching a host cancels its local observers.
    if (error && typeof error === 'object' && 'name' in error && error.name === 'AbortError') return;
    this.update({ error: errorMessage(error), errorTitle: title ?? (typeof error === 'string' ? error : undefined) });
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
  /** A revision distinguishes later edits even when they return to identical text. */
  draftRevision(key: string): string {
    if (this.dirtyDrafts.has(key)) return this.draftRevisions.get(key) ?? '';
    const revision = this.platform.storage.getItem(draftRevisionPrefix + key);
    return revision && revision.length <= 128 ? revision : '';
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
    let savedBytes = 2; // JSON array brackets; each entry contributes its encoding and a comma.
    for (const key of this.platform.storage.keys()) {
      if (!key.startsWith(draftStoragePrefix)) continue;
      const text = this.platform.storage.getItem(key);
      // Bounds are enforced by eviction on the next write; reading only refuses
      // to retain an oversized entry, and stops at a hard ceiling.
      if (text && encodedBytes(text) <= 256 * 1024) {
        const identity = key.slice(draftStoragePrefix.length);
        const previous = saved.get(identity);
        if (previous !== undefined) savedBytes -= encodedBytes(JSON.stringify([identity, previous]));
        else if (saved.size) savedBytes++;
        saved.set(identity, text);
        savedBytes += encodedBytes(JSON.stringify([identity, text]));
      }
      if (text && this.platform.storage.persistent !== false)
        this.durableDrafts.add(key.slice(draftStoragePrefix.length));
      if (savedBytes > 2 * 1024 * 1024)
        throw new Error('Unsent drafts exceed the device storage bound.');
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
    const draftCount = this.draftIdentities.size;
    if (!key || key.length > 512) throw new Error('Invalid draft identity');
    if (text) {
      const next = this.mergedDrafts();
      next.set(key, text);
      this.evictDrafts(next, key);
      this.validateDrafts(next);
    }
    const wasUnsaved = this.hasUnsavedDrafts();
    if (text) { this.drafts.set(key, text); this.draftRevisions.set(key, crypto.randomUUID()); }
    else { this.drafts.delete(key); this.draftRevisions.delete(key); }
    const hadDraft = this.draftIdentities.has(key);
    if (text) this.draftIdentities.add(key); else this.draftIdentities.delete(key);
    this.dirtyDrafts.add(key);
    for (const listener of this.draftListeners.get(key) ?? []) listener();
    clearTimeout(this.draftTimer);
    this.draftTimer = setTimeout(() => this.flushDrafts(), 150);
    if (hadDraft !== !!text || draftCount !== this.draftIdentities.size) this.update({});
    // The leave-warning listener needs this transition immediately. React's
    // app snapshot is unchanged; the recipient's draft has its own subscribers.
    else if (wasUnsaved !== this.hasUnsavedDrafts()) this.notify();
  }
  hasUnsavedDrafts() {
    return this.dirtyDrafts.size > 0 || (this.platform.storage.persistent === false && this.draftIdentities.size > 0);
  }
  /** Flush text synchronously and tell the host whether closing could lose changes. */
  flushDrafts(): { saved: boolean; error?: string } {
    clearTimeout(this.draftTimer);
    this.draftTimer = undefined;
    const wasUnsaved = this.hasUnsavedDrafts();
    const draftCount = this.draftIdentities.size;
    try {
      // Bound first, so drafts evicted here are removed by the deletion pass below.
      if (this.dirtyDrafts.size) this.evictDrafts(this.mergedDrafts());
      // Explicit deletion must remain possible even when externally written
      // drafts already exceed the aggregate admission bound.
      for (const key of this.dirtyDrafts) {
        if (this.drafts.has(key)) continue;
        if (this.platform.storage.persistent !== false && this.platform.storage.getItem(draftStoragePrefix + key))
          this.durableDrafts.add(key);
        this.platform.storage.removeItem(draftRevisionPrefix + key);
        this.platform.storage.removeItem(draftStoragePrefix + key);
        this.draftRevisions.delete(key);
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
        this.platform.storage.setItem(draftRevisionPrefix + key, this.draftRevisions.get(key) ?? crypto.randomUUID());
        this.platform.storage.setItem(draftStoragePrefix + key, text);
        if (this.platform.storage.persistent !== false) this.durableDrafts.add(key);
        this.drafts.delete(key);
        this.draftRevisions.delete(key);
        this.dirtyDrafts.delete(key);
      }
    } catch (error) {
      const message = 'Drafts could not be saved; keep this page open to retain them.';
      this.report(new Error(message, { cause: error }), message);
      return { saved: false, error: message };
    }
    const unsaved = this.hasUnsavedDrafts();
    if (!unsaved && this.state.error === 'Drafts could not be saved; keep this page open to retain them.') this.clearError();
    if (draftCount !== this.draftIdentities.size) this.update({});
    else if (wasUnsaved !== unsaved) this.notify();
    return unsaved
      ? { saved: false, error: 'Device storage is unavailable. Closing or reloading may lose draft changes; keep this page open to retain them.' }
      : { saved: true };
  }
  /**
   * Keep drafts inside their count and size bounds without anyone managing
   * them: drop drafts that no open or recently closed tab owns first, then the
   * earliest stored ones, never the draft being written.
   */
  private evictDrafts(drafts: Map<string, string>, keep?: string) {
    const over = () => drafts.size > 32 || encodedBytes(JSON.stringify([...drafts])) > 1024 * 1024;
    if (!over()) return;
    const workspace = this.tabs.workspace();
    const tabs = [...workspace.tabs, ...workspace.closed.map(item => item.tab)];
    const owned = (key: string) => tabs.some(tab => tab.kind === 'new' ? key === welcomeDraftKey(tab.id) : isSessionTab(tab) && key.startsWith(`${tab.runtimeId}:${tab.rootId}:`));
    for (const evictable of [(key: string) => !owned(key), () => true]) {
      for (const key of [...drafts.keys()]) {
        if (!over()) return;
        if (key === keep || !evictable(key)) continue;
        drafts.delete(key);
        this.drafts.delete(key); this.draftRevisions.delete(key); this.draftIdentities.delete(key);
        this.dirtyDrafts.add(key);
        for (const listener of this.draftListeners.get(key) ?? []) listener();
      }
    }
  }
  async connect(): Promise<void> {
    try { await this.connections.connect(); }
    catch (error) {
      if (!this.closed && this.connections.home().error !== errorMessage(error)) this.report(error);
      throw error;
    }
  }
  acquireView(runtimeId: string, rootId: string, sessionId = rootId): { view: SessionView; execution: ExecutionView; release(): void } {
    const client = this.connections.host(runtimeId)?.client;
    if (!client) throw new Error('Connect to a host first');
    const key = JSON.stringify([runtimeId, sessionId]);
    let lease = this.views.get(key);
    if (!lease) {
      for (const [id, candidate] of this.views) {
        if (this.views.size < 16) break;
        if (!candidate.users) this.dropView(id, candidate);
      }
      if (this.views.size >= 16)
        throw new Error(
          'Sixteen session views are already open. Close a view before opening another.',
        );
      const session = client.session(sessionId);
      const view = createSessionView(session, { maxBytes: 4 << 20, maxMessages: 256 });
      lease = { client, runtimeId, rootId, view, execution: createExecutionView(session, view), users: 0 };
      this.views.set(key, lease);
      // Snapshot and history failures belong to the view's scoped error state.
      void Promise.all([lease.view.start(), lease.execution.start()]).catch(() => {});
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
      execution: lease.execution,
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
  private dropView(id: string, lease: ViewLease) {
    clearTimeout(lease.timer);
    if (this.views.get(id) === lease) this.views.delete(id);
    void Promise.all([lease.view.dispose(), lease.execution.dispose()]);
  }
  /** A trace reads the whole root but each pane owns its filter and reading window. */
  acquireTrace(runtimeId: string, rootId: string, viewId: string): { view: TraceView; release(): void } {
    const client = this.connections.host(runtimeId)?.client;
    if (this.closed || !client) throw new Error('Connect to a host first');
    const key = JSON.stringify([runtimeId, rootId, viewId]);
    let lease = this.traces.get(key);
    if (!lease) {
      for (const [id, candidate] of this.traces) {
        if (this.traces.size < 8) break;
        if (!candidate.users) this.dropTrace(id, candidate);
      }
      if (this.traces.size >= 8) throw new Error('Eight trace views are already open. Close a view before opening another.');
      lease = { client, runtimeId, rootId, view: createTraceView(client, rootId), users: 0 };
      this.traces.set(key, lease);
      void lease.view.start().catch(() => {});
    }
    this.traces.delete(key);
    this.traces.set(key, lease);
    clearTimeout(lease.timer);
    lease.users++;
    const retained = lease;
    let released = false;
    return { view: retained.view, release: () => {
      if (released) return;
      released = true;
      retained.users--;
      if (!retained.users && this.traces.get(key) === retained)
        retained.timer = setTimeout(() => this.dropTrace(key, retained), 30_000);
    } };
  }
  private dropTrace(id: string, lease: TraceLease) {
    clearTimeout(lease.timer);
    if (this.traces.get(id) === lease) this.traces.delete(id);
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
  /** App mutations share the durable journal; constructing a handle sends nothing. */
  command<M extends DurableMethod>(client: Client, method: M, params: Operations[M]['params']) {
    return client.command(method, params, { journal: this.recovery });
  }
  run<M extends DurableMethod>(
    handle: DurableCommand<M>, label: string, onAccepted?: (value: Operations[M]['result']) => void, draftKey?: string,
  ): Promise<Operations[M]['result']> {
    const { runtimeID: runtimeId, clientID } = handle.record;
    let client = this.connections.host(runtimeId)?.client;
    if (!client || client.clientID !== clientID) return Promise.reject(new Error('Reconnect the original host before sending this command'));
    let signal = this.connections.signal(client);
    const id = JSON.stringify([runtimeId, clientID, handle.id]);
    const attached = () => !!client && this.connections.isAttached(client);
    let accepted = false, uncertain = false;
    let work: Promise<Operations[M]['result']> | undefined;
    const notice = (status: string, extra: Pick<CommandNotice, 'error' | 'delivery' | 'turnId'> = {}) =>
      this.commandNotice({ id, commandId: handle.id, runtimeId, label, draftKey, status, ...extra });
    const accept = (value: unknown) => {
      if (value && typeof value === 'object' && 'receipt' in value) this.submittedInputs.acknowledge(value as Admission, runtimeId);
      if (!accepted) { accepted = true; onAccepted?.(value as Operations[M]['result']); }
    };
    const check = async (): Promise<Operations[M]['result']> => {
      const result = await handle.check({ signal });
      if (result.state === 'missing') {
        notice('Not accepted · explicit retry available', { delivery: 'absent' });
        throw new RecoveryError('The host has no receipt for this command');
      }
      if (result.state !== 'found') {
        notice('Acceptance unresolved', { delivery: 'uncertain' });
        throw new RecoveryError('The available receipt does not establish the exact original request; inspect the saved recovery record');
      }
      // Input admissions and edit receipts have different native result shapes.
      if (handle.method === 'workspace.capture' || handle.method === 'workspace.restore' || handle.method === 'workspace.release') {
        const action = result.evidence;
        if (!client || !('snapshot_id' in action)) throw new RecoveryError('Workspace receipt is unavailable');
        const snapshot = await client.getWorkspaceSnapshot(action.session_id, action.snapshot_id, { signal });
        if (snapshot.id !== action.snapshot_id || snapshot.session_id !== action.session_id)
          throw new RecoveryError('Workspace snapshot does not belong to the accepted action');
        return { action, snapshot } as Operations[M]['result'];
      }
      return result.evidence as Operations[M]['result'];
    };
    const observe = async (mode: 'initial' | 'check' | 'retry'): Promise<Operations[M]['result']> => {
      let terminal = false;
      try {
        let result: Operations[M]['result'];
        try {
          result = await (mode === 'initial' ? handle.send({ signal }) : mode === 'retry' ? handle.retry({ signal }) : check());
        } catch (error) {
          if (error instanceof RecoveryPersistenceError && error.accepted) {
            // The host acknowledged; a local disk failure must not restore a sent draft or resend.
            accept(error.acknowledgement);
            notice('Accepted · recovery storage needs attention', { error: errorMessage(error) });
            throw error;
          }
          if (!(error instanceof DeliveryError) && !(signal.aborted && !(error instanceof RecoveryPersistenceError))) throw error;
          uncertain = true; this.pending.set(id, pending);
          if (signal.aborted || !attached()) throw new RecoveryError('Delivery may have reached the host. Reconnect and check the original command before sending again.');
          notice('Checking acceptance', { delivery: 'uncertain' });
          result = await check();
        }
        signal.throwIfAborted();
        if (!attached()) throw new Error('Host changed while submitting');
        accept(result); this.pending.delete(id);
        notice('Accepted');
        if ('identity' in handle.params) {
          const outcome = await handle.wait({ signal });
          signal.throwIfAborted();
          if (!attached()) throw new Error('Host changed while observing the command');
          this.submittedInputs.acknowledge(outcome, runtimeId);
          terminal = true;
          const status = outcome.receipt.deleted_at ? 'deleted' : outcome.input?.state === 'cancelled' ? 'cancelled' : outcome.turn?.state ?? 'unavailable';
          notice(status, { ...(outcome.turn ? { turnId: outcome.turn.id } : {}), ...(outcome.turn?.failure ? { error: outcome.turn.failure } : {}) });
          await handle.forget();
          if (status !== 'succeeded') throw new Error(outcome.turn?.failure ?? `${label}: ${status}`);
          result = outcome as Operations[M]['result'];
        } else {
          if (handle.method === 'workspace.capture' || handle.method === 'workspace.restore' || handle.method === 'workspace.release') {
            const action = (result as Operations['workspace.capture']['result']).action;
            if (action.state !== 'succeeded') {
              terminal = true;
              notice(`Accepted · workspace ${action.state}`, action.failure ? { error: action.failure } : {});
              throw new RecoveryError(action.failure ?? `Workspace action is ${action.state}; inspect its saved receipt before continuing`);
            }
          }
          terminal = true;
          if (handle.method === 'sessions.reload') {
            const reload = result as Operations['sessions.reload']['result'];
            notice(`Reload ${reload.state}`);
            if (reload.state === 'applied') await handle.forget();
          } else {
            notice('Succeeded'); await handle.forget();
          }
        }
        await this.queries.invalidateQueries({ predicate: query => query.queryKey[1] === runtimeId });
        signal.throwIfAborted();
        if (!attached()) throw new Error('Host changed while refreshing command results');
        return result;
      } catch (error) {
        if (!this.closed) {
          // A local wait abort says nothing about an already started send.
          if (!accepted && signal.aborted && !(error instanceof RecoveryPersistenceError)) uncertain = true;
          if (uncertain && !accepted) {
            this.pending.set(id, pending);
            if (this.state.commands.find(item => item.id === id)?.delivery !== 'absent')
              notice('Acceptance unresolved', { delivery: 'uncertain', error: errorMessage(error) });
          } else if (!terminal) notice(accepted ? 'Accepted · needs attention' : 'Needs attention', { error: errorMessage(error) });
          if (!uncertain || accepted) this.submittedInputs.remove(handle.id, runtimeId);
        }
        throw error;
      }
    };
    const start = (mode: 'initial' | 'check' | 'retry') => {
      if (work) return work;
      try {
        const current = this.connections.host(runtimeId)?.client;
        if (!current || current.clientID !== clientID || !this.connections.isAttached(current))
          throw new RecoveryError('Reconnect the original host and client before checking this command');
        if (current !== client) {
          handle = DurableCommand.recover(current, handle.record, { journal: this.recovery }) as DurableCommand<M>;
          client = current;
          pending.client = current;
        }
        signal = this.connections.signal(current);
      } catch (error) { return Promise.reject(error); }
      work = observe(mode).finally(() => { work = undefined; });
      return work;
    };
    const pending: PendingCommand = { client, check: () => start('check'), retry: () => start('retry') };
    notice('Submitting');
    return start('initial');
  }
  dispose() {
    if (this.closed) return;
    this.flushDrafts();
    this.closed = true;
    this.browserAssociations.dispose();
    this.connections.dispose();
    // Recovery detach can leave suspended leases after the transport is gone.
    for (const [id, lease] of this.views) this.dropView(id, lease);
    for (const [id, lease] of this.traces) this.dropTrace(id, lease);
    this.submittedInputs.clear();
    this.pending.clear();
    this.queries.clear();
    this.browser.dispose();
    this.tabs.dispose();
    this.compositions.dispose();
    this.readingPositions.clear();
    this.listeners.clear();
    this.draftListeners.clear();
    this.draftRevisions.clear();
  }
}
