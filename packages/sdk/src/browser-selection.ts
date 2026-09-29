import { assertValid } from '@whip/protocol';
import type { BrowserCommand, BrowserCommandCancel, BrowserCommandResultParams, BrowserInventoryRequest, BrowserInventoryResultParams, BrowserProviderBindParams, BrowserProviderBindResult, BrowserProviderEventParams, BrowserScopesRetired } from '@whip/protocol';
import { BrowserProviderClient } from './browser-provider.js';
import { callSignal, RemoteError } from './wire.js';
import type { CallOptions } from './wire.js';

/** Platform-owned methods must be bounded and settle after cancel/release.
 * Selection/release change agent controls only, never ownership of human tabs. */
export interface BrowserProviderBridge {
  select(input: { offer: BrowserProviderBindParams; provider: BrowserProviderBindResult; connectionId?: string; projectId?: string }): Promise<void>;
  inventory?(request: BrowserInventoryRequest): Promise<BrowserInventoryResultParams>;
  dispatch(command: BrowserCommand): Promise<BrowserCommandResultParams & { screenshotBytes?: Uint8Array }>;
  cancel(input: BrowserCommandCancel): void;
  retire(input: BrowserScopesRetired): Promise<void>;
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
  /** Closes this peer, cancels native work, and joins its bounded handlers. */
  release(): Promise<void>;
}

/** Takes ownership of one freshly connected provider peer for one explicit root.
 * No client registry, reconnect, command retry, or checkpoint restoration. */
export function selectBrowserProvider(peer: BrowserProviderClient, offer: BrowserProviderBindParams, bridge: BrowserProviderBridge, options: BrowserSelectionOptions = {}): Promise<BrowserSelection> {
  return new Selection(peer, structuredClone(offer), bridge, { ...options }).start();
}
const encoder = new TextEncoder();
const scopeKey = (value: { tab_id: string; tab_generation: string; attachment_id: string; attachment_generation: string }) => JSON.stringify([value.tab_id, value.tab_generation, value.attachment_id, value.attachment_generation]);
function withSignal<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  return new Promise((resolve, reject) => {
    const abort = () => reject(signal.reason);
    signal.addEventListener('abort', abort, { once: true });
    promise.then(resolve, reject).finally(() => signal.removeEventListener('abort', abort));
    if (signal.aborted) abort();
  });
}
class Selection {
  private live = true;
  private binding?: BrowserProviderBindResult;
  private stopEvents?: () => void;
  private releaseTask?: Promise<void>;
  private pump?: Promise<void>;
  private nativeSelect?: Promise<void>;
  private readonly lifetime = new AbortController();
  private readonly pending = new Map<string, { command: BrowserCommand; cancelled: boolean }>();
  private readonly seen = new Set<string>();
  private readonly retired = new Set<string>();
  private readonly tasks = new Set<Promise<void>>();
  private inventoryCount = 0;
  private eventCount = 0;
  private eventBytes = 0;
  private events = Promise.resolve();
  private readonly abort = () => { void this.release().catch(error => this.report(error)); };
  constructor(private readonly peer: BrowserProviderClient, private readonly offer: BrowserProviderBindParams, private readonly bridge: BrowserProviderBridge, private readonly options: BrowserSelectionOptions) {}
  async start(): Promise<BrowserSelection> {
    try {
      assertValid('BrowserProviderBindParams', this.offer);
      if (this.offer.version === 2 && !this.bridge.inventory) throw new TypeError('Browser discovery requires a native inventory handler');
      const signal = callSignal(this.options, 30_000);
      this.options.signal?.addEventListener('abort', this.abort, { once: true });
      signal.throwIfAborted();
      this.binding = await this.peer.bind(this.offer, { signal });
      if (this.binding.version !== this.offer.version) throw new TypeError('Browser provider version changed');
      signal.throwIfAborted();
      this.nativeSelect = this.bridge.select({ offer: structuredClone(this.offer), provider: structuredClone(this.binding), ...(this.options.connectionId ? { connectionId: this.options.connectionId } : {}), ...(this.options.projectId ? { projectId: this.options.projectId } : {}) });
      await withSignal(this.nativeSelect, signal);
      signal.throwIfAborted();
      if (!this.live) throw new Error('Browser selection was interrupted');
      this.stopEvents = this.bridge.onEvent(message => {
        if (message.kind === 'provider' && message.event && this.matches(message.event.root_id, message.event.provider_epoch)) this.observe(message.event);
      });
      this.pump = this.read();
      const owner = this;
      return { provider: Object.freeze({ ...this.binding }), get active() { return owner.live; }, release: () => owner.release() };
    } catch (error) { void this.release().catch(error => this.report(error)); throw error; }
  }
  private matches(root: string, epoch: string) { return this.live && root === this.offer.root_id && epoch === this.binding?.provider_epoch; }
  private track(task: Promise<void>) {
    this.tasks.add(task);
    void task.catch(error => { if (this.live) this.fail(error); }).finally(() => this.tasks.delete(task));
  }
  private report(error: unknown) { try { this.options.onError?.(error instanceof Error ? error : new Error('Browser provider failed')); } catch { /* An observer does not own teardown. */ } }
  private fail(error: unknown) { this.report(error); void this.release().catch(error => this.report(error)); }
  private async read() {
    try {
      for await (const event of this.peer.events()) {
        if (!this.live) break;
        if (event.command) this.command(event.command);
        else if (event.inventory) this.inventory(event.inventory);
        else if (event.cancel) this.cancel(event.cancel);
        else if (event.retired) {
          if (!this.matches(event.retired.root_id, event.retired.provider_epoch) || event.retired.provider_id !== this.binding?.provider_id) throw new TypeError('Browser retirement identity changed');
          for (const scope of event.retired.scopes) {
            if (scope.provider_id !== this.binding.provider_id || scope.provider_epoch !== this.binding.provider_epoch) throw new TypeError('Browser retirement scope changed');
            this.retire(scopeKey(scope));
            for (const pending of this.pending.values()) if (scopeKey(pending.command.scope) === scopeKey(scope)) this.cancel({ command_id: pending.command.command_id, root_id: pending.command.root_id, provider_epoch: pending.command.provider_epoch, attachment_generation: pending.command.scope.attachment_generation });
          }
          await this.bridge.retire(event.retired);
        } else if (event.revoked) {
          if (!this.matches(event.revoked.root_id, event.revoked.provider_epoch) || event.revoked.provider_id !== this.binding?.provider_id) throw new TypeError('Browser revocation identity changed');
          throw new Error('Browser provider was revoked; select it again explicitly');
        }
      }
      if (this.live) this.fail(new Error('Browser provider connection ended; select it again explicitly'));
    } catch (error) { if (this.live) this.fail(error); }
  }
  private retire(key: string) {
    if (!this.retired.has(key) && this.retired.size >= 256) throw new RangeError('Browser retired attachment capacity reached');
    this.retired.add(key);
  }
  private observe(event: BrowserProviderEventParams) {
    try {
      assertValid('BrowserProviderEventParams', event);
      const key = scopeKey(event);
      if (this.retired.has(key)) return;
      const encoded = JSON.stringify(event), bytes = encoder.encode(encoded).length;
      if (this.eventCount >= 64 || bytes > (2 << 20) - this.eventBytes) throw new RangeError('Browser observation queue exceeds limit');
      const snapshot: BrowserProviderEventParams = JSON.parse(encoded);
      this.eventCount++; this.eventBytes += bytes;
      this.events = this.events.then(async () => {
        if (this.matches(snapshot.root_id, snapshot.provider_epoch) && !this.retired.has(key)) await this.peer.event(snapshot, { signal: this.lifetime.signal });
      }).catch(error => {
        if (error instanceof RemoteError && error.kind === 'BROWSER_EVENT_STALE') this.retire(key);
        else if (this.live) this.fail(error);
      }).finally(() => { this.eventCount--; this.eventBytes -= bytes; });
      // Observe retirement-capacity errors too; no unhandled asynchronous queue.
      void this.events.catch(error => { if (this.live) this.fail(error); });
    } catch (error) { this.fail(error); }
  }
  private inventory(request: BrowserInventoryRequest) {
    if (!this.matches(request.root_id, request.provider_epoch) || request.provider_id !== this.binding?.provider_id) throw new TypeError('Browser inventory identity changed');
    if (this.inventoryCount >= 16) throw new RangeError('Browser inventory capacity reached');
    this.inventoryCount++;
    this.track((async () => {
      try {
        let result: BrowserInventoryResultParams;
        try {
          result = await this.bridge.inventory!(structuredClone(request));
          assertValid('BrowserInventoryResultParams', result);
          if (result.request_id !== request.request_id || result.root_id !== request.root_id || result.provider_epoch !== request.provider_epoch || encoder.encode(JSON.stringify(result)).length > (64 << 10)) throw new TypeError('Browser inventory identity or bounds changed');
        } catch { result = { request_id: request.request_id, root_id: request.root_id, provider_epoch: request.provider_epoch, tabs: [], error: { kind: 'desktop_unavailable', message: 'Native inventory could not complete safely' } }; }
        if (this.matches(request.root_id, request.provider_epoch)) {
          try { await this.peer.inventory(result, { signal: this.lifetime.signal }); }
          catch (error) { if (!(error instanceof RemoteError && error.kind === 'CONFLICT')) throw error; }
        }
      } finally { this.inventoryCount--; }
    })());
  }
  private command(command: BrowserCommand) {
    if (!this.matches(command.root_id, command.provider_epoch) || command.scope.provider_id !== this.binding?.provider_id || command.scope.provider_epoch !== command.provider_epoch) throw new TypeError('Browser command identity changed');
    if (this.seen.has(command.command_id)) throw new TypeError('Browser command replay rejected');
    if (this.seen.size >= 8192 || this.pending.size >= 32) throw new RangeError('Browser command capacity reached');
    this.seen.add(command.command_id);
    const pending = { command, cancelled: false }; this.pending.set(command.command_id, pending);
    this.track((async () => {
      try {
        let result: BrowserCommandResultParams, screenshot: Uint8Array | undefined;
        try {
          const deadline = BigInt(command.deadline_millis), now = BigInt(Date.now());
          if (deadline <= now || deadline > now + 125000n || this.retired.has(scopeKey(command.scope))) throw new TypeError('Browser command expired or retired');
          const native = await this.bridge.dispatch(structuredClone(command));
          const { screenshotBytes, ...wire } = native;
          assertValid('BrowserCommandResultParams', wire);
          if (wire.command_id !== command.command_id || wire.root_id !== command.root_id || wire.provider_epoch !== command.provider_epoch || wire.attachment_generation !== command.scope.attachment_generation || wire.screenshot !== undefined || encoder.encode(JSON.stringify(wire)).length > (512 << 10)) throw new TypeError('Native browser result identity or bounds changed');
          result = wire;
          if (screenshotBytes !== undefined) {
            if (!(screenshotBytes instanceof Uint8Array) || screenshotBytes.length < 3 || screenshotBytes.length > (4 << 20) || screenshotBytes[0] !== 255 || screenshotBytes[1] !== 216 || screenshotBytes[2] !== 255 || wire.error !== undefined) throw new TypeError('Invalid native browser screenshot');
            screenshot = Uint8Array.from(screenshotBytes);
          }
        } catch { result = { command_id: command.command_id, root_id: command.root_id, provider_epoch: command.provider_epoch, attachment_generation: command.scope.attachment_generation, document_revision: command.expected_document, url: '', title: '', error: { kind: 'outcome_unknown', message: 'Native browser command did not complete safely; delivery may be unknown' } }; }
        if (!pending.cancelled && this.matches(command.root_id, command.provider_epoch)) {
          try {
            if (screenshot) await this.peer.screenshotResult(result, screenshot, { signal: this.lifetime.signal });
            else await this.peer.result(result, { signal: this.lifetime.signal });
          } catch (error) { if (!pending.cancelled) throw error; }
        }
      } finally { this.pending.delete(command.command_id); }
    })());
  }
  private cancel(command: BrowserCommandCancel) {
    const pending = this.pending.get(command.command_id);
    if (!pending || !this.matches(command.root_id, command.provider_epoch) || pending.command.scope.attachment_generation !== command.attachment_generation) return;
    pending.cancelled = true;
    this.bridge.cancel(command);
  }
  release(): Promise<void> {
    if (this.releaseTask) return this.releaseTask;
    this.live = false; this.options.signal?.removeEventListener('abort', this.abort);
    this.stopEvents?.(); this.stopEvents = undefined;
    this.lifetime.abort(new Error('Browser selection released'));
    for (const pending of this.pending.values()) {
      pending.cancelled = true;
      try { this.bridge.cancel({ command_id: pending.command.command_id, root_id: pending.command.root_id, provider_epoch: pending.command.provider_epoch, attachment_generation: pending.command.scope.attachment_generation }); } catch (error) { this.report(error); }
    }
    this.releaseTask = (async () => {
      const failures: unknown[] = [];
      await this.peer.close().catch(error => { failures.push(error); });
      // Native selection can acknowledge after local cancellation. Join it and
      // release that exact epoch before this owner is considered disposed.
      await this.nativeSelect?.catch(() => {});
      if (this.binding) await Promise.resolve().then(() => this.bridge.release({ rootId: this.offer.root_id, providerEpoch: this.binding!.provider_epoch })).catch(error => { failures.push(error); });
      await Promise.allSettled([...this.tasks, this.events]);
      await this.pump;
      if (failures.length) throw new AggregateError(failures, 'Browser provider cleanup failed');
    })();
    return this.releaseTask;
  }
}
