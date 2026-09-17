import { assertValid, type BrowserCommand, type BrowserCommandCancel, type BrowserCommandResultParams, type BrowserProviderBindParams, type BrowserProviderBindResult, type BrowserProviderEventParams } from '@whip/protocol';
import type { CallOptions, WhipClient } from './client.js';
import { RpcError, WhipError, asError } from './errors.js';
import { upload } from './content.js';
import { byteLength, withSignal } from './util.js';

/** Structural native adapter. SDK never imports Electron or application packages. */
export interface BrowserProviderBridge {
  select(input: { offer: BrowserProviderBindParams; provider: BrowserProviderBindResult; connectionId?: string; projectId?: string }): Promise<void>;
  dispatch(command: BrowserCommand): Promise<BrowserCommandResultParams & { screenshotBytes?: Uint8Array }>;
  cancel(input: BrowserCommandCancel): void;
  release(input: { rootId: string; providerEpoch: string }): Promise<void>;
  onEvent(listener: (event: { kind: string; event?: BrowserProviderEventParams }) => void): () => void;
}
export interface BrowserSelectionOptions extends CallOptions {
  connectionId?: string;
  projectId?: string;
  onError?(error: Error): void;
}
export interface BrowserSelection {
  readonly provider: Readonly<BrowserProviderBindResult>;
  readonly active: boolean;
  /** Releases this exact epoch only. It never closes human Browser tabs. */
  release(): Promise<void>;
}

/** Explicit root association, not a default/newest-wins browser destination. */
export class BrowserProviders {
  private readonly roots = new Map<string, SelectedBrowser>();
  constructor(private readonly client: WhipClient) {
    client.onNotification('browser.command', command => this.roots.get(command.root_id)?.receive(command));
    client.onNotification('browser.command.cancel', command => this.roots.get(command.root_id)?.cancel(command));
    client.onNotification('browser.provider.revoked', event => this.roots.get(event.root_id)?.revoke(event.provider_epoch, event.reason));
    client.subscribe(() => {
      if (client.getSnapshot().state !== 'connected') {
        for (const selection of this.roots.values()) selection.disconnected();
      }
    });
  }
  async select(input: BrowserProviderBindParams, bridge: BrowserProviderBridge, options: BrowserSelectionOptions = {}): Promise<BrowserSelection> {
    this.client.requireConnected();
    const offer = structuredClone(input);
    assertValid('BrowserProviderBindParams', offer);
    if (this.roots.has(offer.root_id)) throw new WhipError('invalid_arguments', 'Release the existing Browser provider before selecting another');
    const selection = new SelectedBrowser(this.client, offer, bridge, { ...options }, () => {
      if (this.roots.get(offer.root_id) === selection) this.roots.delete(offer.root_id);
    });
    this.roots.set(offer.root_id, selection);
    return selection.start();
  }
}

class SelectedBrowser {
  private binding?: BrowserProviderBindResult;
  private unbinding?: Promise<unknown>;
  private live = true;
  private serverRevoked = false;
  private readonly pending = new Map<string, { cancelled: boolean; command: BrowserCommand }>();
  private readonly revoked = new Map<string, string>();
  private readonly seen = new Set<string>();
  private events = Promise.resolve();
  private eventCount = 0;
  private eventBytes = 0;
  private readonly retiredEvents = new Set<string>();
  private readonly lifetime = new AbortController();
  private readonly ready: Promise<void>;
  private acknowledge!: () => void;
  private readonly off: () => void;
  constructor(private readonly client: WhipClient, private readonly offer: BrowserProviderBindParams, private readonly bridge: BrowserProviderBridge,
    private readonly options: BrowserSelectionOptions, private readonly removed: () => void) {
    this.ready = new Promise(resolve => { this.acknowledge = resolve; });
    this.events = this.ready;
    this.off = bridge.onEvent(message => {
      const event = message.event;
      if (message.kind !== 'provider' || !event || !this.matches(event.root_id, event.provider_epoch)) return;
      this.observe(event);
    });
  }
  private observe(event: BrowserProviderEventParams): void {
    const key = JSON.stringify([event.tab_id, event.tab_generation, event.attachment_id, event.attachment_generation]);
    if (this.retiredEvents.has(key)) return;
    try {
      // Snapshot once and bound retained payloads, not just the RPC frame size.
      const encoded = JSON.stringify(event);
      const bytes = byteLength(encoded);
      if (this.eventCount >= 64 || this.eventBytes + bytes > 2 * 1024 * 1024) {
        throw new WhipError('resource_limit', 'Browser observation queue exceeded its limit; select the provider again explicitly');
      }
      const snapshot: BrowserProviderEventParams = JSON.parse(encoded);
      this.eventCount++; this.eventBytes += bytes;
      // Preserve observation order. Dropping a live sequence would hide a gap.
      this.events = this.events.then(async () => {
        if (this.matches(snapshot.root_id, snapshot.provider_epoch) && !this.retiredEvents.has(key)) {
          await this.client.call('browser.provider.event', snapshot, { signal: this.lifetime.signal, timeoutMs: 10_000 });
        }
      }).catch(error => {
        if (error instanceof RpcError && error.kind === 'browser_event_stale') {
          // The broker authenticated the holder but this exact attachment retired.
          // Other attachments keep their independent authority and event sequence.
          if (this.retiredEvents.size >= 8192) return this.fail(new WhipError('resource_limit', 'Browser retired observation capacity reached'));
          this.retiredEvents.add(key);
          return;
        }
        if (this.live) return this.fail(error);
      }).finally(() => { this.eventCount--; this.eventBytes -= bytes; });
    } catch (error) { void this.fail(error); }
  }
  start(): Promise<BrowserSelection> {
    return this.bind();
  }
  private async bind(): Promise<BrowserSelection> {
    try {
      const signal = AbortSignal.any([this.lifetime.signal, AbortSignal.timeout(this.options.timeoutMs ?? 30_000), ...(this.options.signal ? [this.options.signal] : [])]);
      const binding = await this.client.call('browser.provider.bind', this.offer, { ...this.options, signal });
      this.binding = binding;
      const revoked = this.revoked.get(binding.provider_epoch);
      if (revoked !== undefined) { this.revoke(binding.provider_epoch, revoked); throw new WhipError('unavailable_capability', 'Browser provider was revoked before selection completed'); }
      this.revoked.clear();
      if (!this.live) { await this.release(); throw new WhipError('disconnected', 'Browser selection was interrupted'); }
      const native = this.bridge.select({ offer: this.offer, provider: binding,
        ...(this.options.connectionId ? { connectionId: this.options.connectionId } : {}),
        ...(this.options.projectId ? { projectId: this.options.projectId } : {}) });
      // A timed-out native invocation cannot be cancelled or replayed. A late ACK
      // must release its exact epoch without touching any replacement selection.
      void native.then(() => {
        if (signal.aborted || !this.live) return this.bridge.release({ rootId: this.offer.root_id, providerEpoch: binding.provider_epoch });
      }).catch(error => { if (!this.live) this.report(error); });
      await withSignal(native, signal);
      signal.throwIfAborted();
      this.acknowledge();
      const selected = this;
      return { provider: Object.freeze({ ...binding }), get active() { return selected.live; }, release: () => selected.release() };
    } catch (error) { void this.release().catch(releaseError => this.report(releaseError)); throw error; }
  }
  private matches(root: string, epoch: string): boolean { return this.live && root === this.offer.root_id && epoch === this.binding?.provider_epoch; }
  receive(command: BrowserCommand): void {
    // Commands may race the bind reply; never dispatch until native selection ACKs.
    if (!this.live || this.seen.has(command.command_id)) return;
    if (this.seen.size >= 8192 || this.pending.size >= 32) { void this.fail(new WhipError('resource_limit', 'Browser provider command capacity reached')); return; }
    this.seen.add(command.command_id);
    const state = { cancelled: false, command };
    this.pending.set(command.command_id, state);
    void this.ready.then(async () => {
      if (state.cancelled || !this.matches(command.root_id, command.provider_epoch)) return;
      let result: BrowserCommandResultParams;
      try {
        if (BigInt(command.deadline_millis) <= BigInt(Date.now())) {
          result = this.failure(command, 'deadline_exceeded', 'Browser command expired before native delivery');
        } else {
          const native = await this.bridge.dispatch(command);
          const { screenshotBytes, ...wire } = native;
          assertValid('BrowserCommandResultParams', wire, 'response');
          if (wire.command_id !== command.command_id || wire.root_id !== command.root_id || wire.provider_epoch !== command.provider_epoch || wire.attachment_generation !== (command.scope.attachment_generation ?? '')) {
            throw new WhipError('invalid_response', 'Native Browser result identity changed');
          }
          result = wire;
          if (screenshotBytes) {
            if (wire.screenshot || screenshotBytes.byteLength > 8 * 1024 * 1024 || screenshotBytes.byteLength < 3 || screenshotBytes[0] !== 0xff || screenshotBytes[1] !== 0xd8 || screenshotBytes[2] !== 0xff) throw new WhipError('invalid_response', 'Invalid native Browser screenshot');
            if (state.cancelled || !this.matches(command.root_id, command.provider_epoch)) return;
            const content = await upload(this.client, new Uint8Array(screenshotBytes), { rootId: command.root_id, agentId: command.agent_id, mediaType: 'image/jpeg', source: 'desktop-browser', signal: this.lifetime.signal, timeoutMs: Math.max(1, Math.min(30_000, Number(BigInt(command.deadline_millis) - BigInt(Date.now())))) }, 'connection');
            result = { ...wire, screenshot: content.handle };
          }
        }
      } catch (error) {
        // Native delivery may already have changed the page. Never replay it.
        result = this.failure(command, 'outcome_unknown', asError(error).message);
      }
      if (!state.cancelled && this.matches(command.root_id, command.provider_epoch)) await this.client.call('browser.command.result', result);
    }).catch(error => {
      // An exact cancellation may arrive after result submission but before its
      // rejection. Retired work must not tear down other attachments or epochs.
      if (!state.cancelled && this.matches(command.root_id, command.provider_epoch)) return this.fail(error);
    }).finally(() => this.pending.delete(command.command_id));
  }
  cancel(command: BrowserCommandCancel): void {
    if (command.root_id !== this.offer.root_id || (this.binding && command.provider_epoch !== this.binding.provider_epoch)) return;
    const pending = this.pending.get(command.command_id);
    if (!pending || pending.command.provider_epoch !== command.provider_epoch || (pending.command.scope.attachment_generation ?? '') !== command.attachment_generation) return;
    pending.cancelled = true;
    if (this.matches(command.root_id, command.provider_epoch)) this.bridge.cancel(command);
  }
  private failure(command: BrowserCommand, kind: string, message: string): BrowserCommandResultParams {
    return { command_id: command.command_id, root_id: command.root_id, provider_epoch: command.provider_epoch,
      attachment_generation: command.scope.attachment_generation ?? '', document_revision: command.expected_document ?? '', error: { kind, message: message.slice(0, 2048) } };
  }
  revoke(epoch: string, reason: string): void {
    if (!this.live) return;
    if (!this.binding) {
      if (this.revoked.size >= 8) { void this.fail(new WhipError('resource_limit', 'Browser selection revocation capacity reached')); return; }
      this.revoked.set(epoch, reason);
      return;
    }
    if (this.binding.provider_epoch !== epoch) return;
    this.serverRevoked = true;
    this.disconnected();
    this.report(new WhipError('unavailable_capability', `Browser provider was revoked (${reason.slice(0, 128)}); select a provider again explicitly`));
  }
  disconnected(): void { void this.release(false).catch(error => this.report(error)); }
  private report(error: unknown): void {
    // An observer cannot break teardown or create an unhandled command rejection.
    try { this.options.onError?.(asError(error)); } catch { /* observer failure */ }
  }
  private async fail(error: unknown): Promise<void> {
    const cleanup = this.release();
    this.report(error);
    try { await cleanup; } catch (releaseError) { this.report(releaseError); }
  }
  async release(notify = true): Promise<void> {
    this.live = false;
    this.lifetime.abort(new WhipError('disconnected', 'Browser selection was released'));
    this.acknowledge();
    this.off();
    this.removed();
    for (const pending of this.pending.values()) pending.cancelled = true;
    const binding = this.binding;
    if (!binding) return;
    const native = withSignal(Promise.resolve().then(() => this.bridge.release({ rootId: this.offer.root_id, providerEpoch: binding.provider_epoch })), AbortSignal.timeout(5_000));
    if (notify && !this.serverRevoked && this.client.getSnapshot().state === 'connected' && !this.unbinding) {
      this.unbinding = this.client.call('browser.provider.unbind', { root_id: this.offer.root_id, provider_epoch: binding.provider_epoch }, { timeoutMs: 5_000 });
    }
    await Promise.all([native, this.unbinding]);
  }
}
