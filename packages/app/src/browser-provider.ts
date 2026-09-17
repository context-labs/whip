import type { BrowserSelection, WhipClient } from '@whip/sdk';
import type { BrowserAgentBridge, BrowserAgentEvent } from './browser-agent-types';
import type { BrowserWorkspace } from './browser-workspace';
import type { HostConnections } from './hosts';
import { sessionViewPane, type SessionTabs } from './session-tabs';
import { errorMessage } from './platform';

export interface BrowserAssociation {
  readonly key: string;
  readonly hostId: string;
  readonly runtimeId: string;
  readonly hostName: string;
  readonly rootId: string;
  readonly title: string;
  readonly tabId: string;
  readonly paneId: string;
  readonly status: 'selecting' | 'selected' | 'unavailable' | 'error';
  readonly error?: string;
}
interface Association {
  value: BrowserAssociation;
  client: WhipClient;
  controller: AbortController;
  pending?: Promise<BrowserSelection>;
  selection?: BrowserSelection;
  admitted: Set<string>;
}
export function browserProjectId(cwd: string): string | undefined {
  return cwd.startsWith('/') && cwd.length <= 508 && !/[\u0000-\u001f\u007f]/.test(cwd) ? `cwd:${cwd}` : undefined;
}
export const browserVersionHelp = 'Browser access requires a conversation created with Browser v1 support. Start a fresh conversation if this one predates Browser support; existing conversations are never silently upgraded.';

/** App-owned explicit root/tab/pane association only. The SDK owns transport and grants. */
export class BrowserAssociations {
  private readonly records = new Map<string, Association>();
  private snapshot: readonly BrowserAssociation[] = Object.freeze([]);
  private readonly listeners = new Set<() => void>();
  private readonly stopped: (() => void)[] = [];
  private disposed = false;
  constructor(private readonly bridge: BrowserAgentBridge | undefined, private readonly hosts: HostConnections,
    private readonly tabs: SessionTabs, private readonly browser: BrowserWorkspace, private readonly report: (error: unknown) => void) {
    if (bridge) this.stopped.push(bridge.onEvent(event => { if (event.kind === 'admission') void this.admit(event); }));
    this.stopped.push(hosts.subscribe(() => {
      for (const entry of this.records.values()) {
        const host = hosts.getSnapshot().hosts.find(host => host.id === entry.value.hostId);
        if ((entry.value.status === 'selected' || entry.value.status === 'selecting') &&
          (host?.state !== 'connected' || host.client !== entry.client || host.runtimeId !== entry.value.runtimeId)) {
          entry.controller.abort();
          void entry.selection?.release().catch(this.report);
          this.update(entry, { status: 'unavailable', error: 'Execution host disconnected. Select this conversation again after reconnecting.' });
        }
      }
    }));
  }
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private publish() { this.snapshot = Object.freeze([...this.records.values()].map(entry => entry.value)); for (const listener of this.listeners) listener(); }
  private update(entry: Association, patch: Partial<BrowserAssociation>) {
    if (this.records.get(entry.value.key) !== entry || this.disposed) return;
    entry.value = Object.freeze({ ...entry.value, ...patch }); this.publish();
  }
  async select(input: { hostId: string; rootId: string; title: string; tabId: string; preview?: boolean; projectId?: string }): Promise<void> {
    const bridge = this.bridge;
    if (!bridge || this.disposed) throw new Error('Conversation Browser access requires the desktop app.');
    const host = this.hosts.getSnapshot().hosts.find(host => host.id === input.hostId);
    if (!host?.client || host.state !== 'connected' || !host.runtimeId) throw new Error('Choose a connected execution host.');
    if (!input.rootId || input.rootId.length > 256) throw new Error('Choose a conversation.');
    const client = host.client, tab = this.tabs.workspace().tabs.find(tab => tab.id === input.tabId);
    const pane = sessionViewPane(this.tabs.workspace(), input.tabId);
    if (tab?.kind !== 'browser' || !pane) throw new Error('This Browser tab is no longer open.');
    const key = JSON.stringify([host.runtimeId, input.rootId]), previous = this.records.get(key);
    if (previous && (previous.value.status === 'selected' || previous.value.status === 'selecting')) throw new Error('Release this conversation’s current Browser access before selecting again.');
    if (!previous && this.records.size >= 32) throw new Error('There are 32 Browser associations. Release one before adding another.');
    if ([...this.records.values()].some(entry => entry.value.rootId === input.rootId && entry.value.key !== key)) throw new Error('Release the association with this conversation identity on the other host first.');
    if (previous) await this.release(key);
    if (this.records.has(key)) throw new Error('Another Browser selection is already in progress.');
    const entry: Association = { client, controller: new AbortController(), admitted: new Set(), value: Object.freeze({ key, hostId: host.id,
      runtimeId: host.runtimeId, hostName: host.name, rootId: input.rootId, title: [...input.title].slice(0, 128).join(''),
      tabId: tab.id, paneId: pane.id, status: 'selecting' }) };
    this.records.set(key, entry); this.publish();
    let signal = entry.controller.signal;
    try {
      signal = AbortSignal.any([signal, this.hosts.signal(client)]);
      const identity = await bridge.identity(); signal.throwIfAborted();
      const offered = identity.tabs.find(item => item.tab_id === tab.id);
      if (!offered) throw new Error('This Browser page is not available for conversation access.');
      let preview;
      if (input.preview) {
        if (host.profile.target.kind !== 'ssh') throw new Error('SSH previews require a verified SSH connection, not a URL host.');
        if (!input.projectId) throw new Error('This conversation has no verified project identity for an SSH preview.');
        preview = await bridge.preview({ connectionId: host.profile.id, runtimeId: host.runtimeId, projectId: input.projectId, loopback: '127.0.0.1' });
        signal.throwIfAborted();
      }
      if (offered.preview && (!preview || offered.preview.environment_id !== preview.environment_id || offered.preview.host_id !== preview.host_id || offered.preview.host_identity !== preview.host_identity || offered.preview.connection_generation !== preview.connection_generation)) {
        throw new Error('Select the matching SSH preview environment before offering this page. A different host is never substituted.');
      }
      entry.pending = client.browser.select({ root_id: input.rootId, version: 1, desktop_id: identity.desktopId,
        window_id: identity.windowId, offer_revision: crypto.randomUUID(), create_profile_id: identity.createProfileId,
        offered_tabs: [offered], offered_preview_hosts: preview ? [preview] : [] }, bridge, {
        signal, ...(preview ? { connectionId: host.profile.id, projectId: input.projectId } : {}),
        onError: error => this.update(entry, { status: 'error', error: errorMessage(error) }),
      });
      entry.selection = await entry.pending;
      if (signal.aborted || this.disposed || this.records.get(key) !== entry || !entry.selection.active) {
        await entry.selection.release(); throw new Error('Browser selection was interrupted. Select the conversation again.');
      }
      this.update(entry, { status: 'selected', error: undefined });
    } catch (error) {
      this.update(entry, { status: signal.aborted ? 'unavailable' : 'error', error: errorMessage(error) });
      throw error;
    }
  }
  private async admit(event: Extract<BrowserAgentEvent, { kind: 'admission' }>) {
    // A late/repeated notification must never close a page already owned by the human workspace.
    if (this.tabs.workspace().tabs.some(tab => tab.kind === 'browser' && tab.id === event.tab.id)) return;
    const target = { epoch: event.epoch, tabId: event.tab.id, generation: event.tab.generation };
    const entry = [...this.records.values()].find(entry => entry.value.rootId === event.rootId && (entry.value.status === 'selecting' || entry.value.status === 'selected'));
    try {
      const selection = entry && await entry.pending;
      if (!entry || !selection?.active || selection.provider.provider_epoch !== event.providerEpoch || entry.controller.signal.aborted || this.disposed || this.records.get(entry.value.key) !== entry) {
        await this.browser.platform?.close(target); return;
      }
      if (entry.admitted.has(event.commandId)) return;
      if (entry.admitted.size >= 256) throw new Error('Browser admission capacity reached. Release and select the conversation again.');
      entry.admitted.add(event.commandId);
      await this.browser.admit(event.tab, target, entry.value.paneId);
    } catch (error) {
      await this.browser.platform?.close(target).catch(this.report);
      this.report(error);
    }
  }
  async release(key: string): Promise<void> {
    const entry = this.records.get(key); if (!entry) return;
    entry.controller.abort(); this.records.delete(key); this.publish();
    await entry.selection?.release();
  }
  dispose() {
    if (this.disposed) return;
    this.disposed = true; this.stopped.forEach(stop => stop());
    for (const key of this.records.keys()) void this.release(key).catch(this.report);
    this.listeners.clear();
  }
}
