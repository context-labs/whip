import type { NativeSurfaceHold } from '@whip/ui';
import type { BrowserAction, BrowserEvent, BrowserInventory, BrowserPlatform, BrowserTarget, BrowserTabState } from './browser-types';
import { browserAddress, browserPreviewAddress } from './browser-address';
import { isBrowserTab, sessionPanes, SessionTabs, type BrowserTab } from './session-tabs';

/** Observes main-owned state and presents existing workspace slots; does not own another tab inventory. */
export class BrowserWorkspace {
  private state: BrowserInventory = { epoch: '', revision: -1, tabs: [] };
  private readonly listeners = new Set<() => void>();
  private readonly events = new Set<(event: BrowserEvent) => void>();
  private readonly slots = new Map<string, { token: symbol; element: HTMLElement }>();
  private readonly holds = new Set<symbol>();
  private readonly admissions = new Set<string>();
  private revision = 0;
  private disposed = false;
  private stopped: (() => void)[] = [];
  private startPromise?: Promise<void>;
  private syncing?: Promise<void>;
  private registered = '';
  private hideACK: Promise<void> = Promise.resolve();
  private lastPresentation = '';
  private frame?: number;
  private readonly retiredEpochs = new Set<string>();
  private focused?: BrowserTarget;
  private returnFocus?: BrowserTarget;
  constructor(readonly platform: BrowserPlatform | undefined, private readonly tabs: SessionTabs, private readonly report: (error: unknown) => void) {}
  getSnapshot = () => this.state;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  onEvent = (listener: (event: BrowserEvent) => void) => { this.events.add(listener); return () => { this.events.delete(listener); }; };
  private accept(snapshot: BrowserInventory) {
    if (this.disposed || this.retiredEpochs.has(snapshot.epoch) || (snapshot.epoch === this.state.epoch && snapshot.revision <= this.state.revision)) return;
    if (snapshot.epoch !== this.state.epoch) {
      if (this.state.epoch) this.retiredEpochs.add(this.state.epoch);
      this.focused = this.returnFocus = undefined; this.revision = 0; this.lastPresentation = ''; this.registered = ''; }
    this.state = snapshot;
    for (const tab of snapshot.tabs) if (this.tabs.workspace().tabs.some(item => item.id === tab.id && isBrowserTab(item))) {
      try { this.tabs.updateBrowser(tab.id, { url: tab.url, titleHint: tab.title }); } catch (error) { this.report(error); }
    }
    for (const listener of this.listeners) listener();
  }
  start(): Promise<void> {
    if (!this.platform || this.disposed) return Promise.resolve();
    if (this.startPromise) return this.startPromise;
    this.stopped.push(this.platform.onEvent(event => {
      if (event.kind === 'snapshot') { this.accept(event.snapshot); if (this.state !== event.snapshot) return; }
      else {
        const target = this.target(event.tabId);
        if (target?.epoch !== event.epoch || target.generation !== event.generation) return;
        if (event.kind === 'focused' || (event.kind === 'shortcut' && event.shortcut === 'commands')) this.focused = target;
      }
      for (const listener of this.events) listener(event);
    }));
    this.startPromise = this.platform.snapshot().then(async snapshot => { this.accept(snapshot); await this.sync(); });
    this.stopped.push(this.tabs.subscribe(() => { void this.startPromise?.then(() => this.sync()).catch(this.report); }));
    if (typeof window !== 'undefined') {
      const measure = () => this.schedule();
      const visibility = () => { void this.present().catch(this.report); };
      const shellFocus = () => { this.focused = undefined; };
      document.addEventListener('focusin', shellFocus); document.addEventListener('pointerdown', shellFocus, true);
      window.addEventListener('resize', measure); window.addEventListener('scroll', measure, true);
      window.visualViewport?.addEventListener('resize', measure); window.visualViewport?.addEventListener('scroll', measure);
      document.addEventListener('visibilitychange', visibility);
      this.stopped.push(() => { window.removeEventListener('resize', measure); window.removeEventListener('scroll', measure, true);
        window.visualViewport?.removeEventListener('resize', measure); window.visualViewport?.removeEventListener('scroll', measure);
        document.removeEventListener('visibilitychange', visibility);
        document.removeEventListener('focusin', shellFocus); document.removeEventListener('pointerdown', shellFocus, true); });
    }
    return this.startPromise;
  }
  private async sync(): Promise<void> {
    if (!this.platform || !this.state.epoch || this.disposed) return;
    if (this.syncing) { await this.syncing; return this.sync(); }
    const tabs = this.tabs.workspace().tabs.filter(isBrowserTab).map(({ id, url, titleHint, environmentId }) => ({
      id, url, titleHint, ...(environmentId === undefined ? {} : { environmentId }),
    }));
    const signature = JSON.stringify(tabs.map(tab => tab.id));
    if (signature === this.registered) return;
    this.syncing = this.platform.restore({ epoch: this.state.epoch, tabs }).then(snapshot => {
      this.registered = signature; this.accept(snapshot);
    });
    try { await this.syncing; } finally { this.syncing = undefined; }
    await this.present();
  }
  target(id: string): BrowserTarget | undefined {
    const state = this.state.tabs.find(tab => tab.id === id);
    return state && this.state.epoch ? { epoch: this.state.epoch, tabId: id, generation: state.generation } : undefined;
  }
  async create(url = 'about:blank', environmentId?: string, paneId?: string): Promise<BrowserTab> {
    if (!this.platform) throw new Error('Browser tabs require the desktop app.');
    if (!this.tabs.canOpenBrowser()) throw new Error('Close a Browser tab before opening another (8 Browser tabs, 32 total).');
    await this.start();
    const epoch = this.state.epoch;
    const page = await this.platform.create({ epoch, url: browserAddress(url), ...(environmentId ? { environmentId } : {}) });
    const target = { epoch, tabId: page.id, generation: page.generation };
    return this.admit(page, target, paneId, { background: false });
  }
  /** Human preview confirmation belongs to native, before routes or tab realization. */
  async createPreview(input: { connectionId: string; runtimeId: string; projectId: string; url: string }, paneId?: string): Promise<BrowserTab | undefined> {
    if (!this.platform?.createPreview) throw new Error('SSH previews require a supported desktop app.');
    if (!this.tabs.canOpenBrowser()) throw new Error('Close a Browser tab before opening another (8 Browser tabs, 32 total).');
    const url = browserPreviewAddress(input.url);
    await this.start();
    const epoch = this.state.epoch;
    const page = await this.platform.createPreview({ ...input, epoch, url });
    if (!page) return;
    return this.admit(page, { epoch, tabId: page.id, generation: page.generation }, paneId, { background: false });
  }
  /** Provider-created pages are admitted only by their originating workspace, without stealing focus. */
  async admit(page: BrowserTabState, target: BrowserTarget, paneId?: string, options: { background?: boolean } = { background: true }): Promise<BrowserTab> {
    if (!this.platform) throw new Error('Browser tabs require the desktop app.');
    let inserted = false;
    try {
      await this.start();
      if (!this.target(page.id)) this.accept(await this.platform.snapshot());
      const current = this.target(page.id);
      if (this.disposed || target.tabId !== page.id || current?.epoch !== target.epoch || current.generation !== target.generation) throw new Error('The browser connection changed before tab admission.');
      if (paneId && !sessionPanes(this.tabs.workspace().layout).some(pane => pane.id === paneId)) throw new Error('The originating workspace pane is no longer available.');
      this.admissions.add(page.id);
      const descriptor = this.tabs.openBrowser({ id: page.id, url: page.url, titleHint: page.title, ...(page.environmentId ? { environmentId: page.environmentId } : {}) }, paneId, { background: options.background !== false });
      inserted = true;
      await this.platform.admitted(target);
      return descriptor;
    } catch (error) {
      if (inserted) this.tabs.discardBrowser(page.id);
      await this.platform.close(target).catch(this.report);
      throw error;
    } finally {
      this.admissions.delete(page.id); this.schedule();
    }
  }
  async act(id: string, action: BrowserAction) {
    await this.start(); await this.sync();
    const target = this.target(id);
    if (!this.platform || !target) throw new Error('This Browser tab is unavailable.');
    if (action.kind === 'focus' && this.holds.size) { this.returnFocus = target; return; }
    await this.platform.act({ ...target, action });
  }
  /** Native beforeunload cancellation must precede removal from the workspace. */
  async mayClose(id: string): Promise<boolean> {
    if (!this.platform) return true;
    await this.start(); await this.sync();
    const target = this.target(id);
    if (!target) throw new Error('Browser state is unavailable; retry after reconnecting.');
    return (await this.platform.close(target)).status === 'closed';
  }
  acquireOverlay = (): NativeSurfaceHold => {
    if (!this.platform) return { ready: Promise.resolve(), release() {} };
    if (this.holds.size >= 128) throw new Error('Too many open surfaces.');
    const token = Symbol(); this.holds.add(token);
    if (this.holds.size === 1) {
      this.returnFocus = this.focused;
      this.hideACK = this.start().then(() => this.present(this.holds.size > 0));
    }
    // The consumer also observes rejection so it can keep controls closed.
    void this.hideACK.catch(this.report);
    return { ready: this.hideACK, release: () => {
      if (!this.holds.delete(token) || this.holds.size) return;
      const target = this.returnFocus; this.returnFocus = undefined;
      void this.present().then(() => {
        if (!target || this.holds.size || this.disposed || !this.slots.has(target.tabId)) return;
        const current = this.target(target.tabId);
        if (current?.epoch === target.epoch && current.generation === target.generation) return this.platform?.act({ ...target, action: { kind: 'focus' } });
      }).catch(this.report);
    } };
  };
  register(id: string, element: HTMLElement): () => void {
    const token = Symbol(); this.slots.set(id, { token, element });
    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(() => this.schedule());
    observer?.observe(element);
    void this.start().then(() => this.sync()).then(() => this.present()).catch(this.report);
    return () => {
      observer?.disconnect();
      if (this.slots.get(id)?.token !== token) return;
      this.slots.delete(id); void this.present().catch(this.report);
    };
  }
  private schedule() {
    if (this.frame !== undefined || this.disposed) return;
    this.frame = requestAnimationFrame(() => { this.frame = undefined; void this.present().catch(this.report); });
  }
  private async present(forceBlocked = false): Promise<void> {
    if (!this.platform || !this.state.epoch) return;
    const blocked = forceBlocked || this.holds.size > 0;
    const slots = this.disposed || (typeof document !== 'undefined' && document.visibilityState === 'hidden') ? [] : [...this.slots].flatMap(([tabId, { element }]) => {
      if (this.admissions.has(tabId) || !element.isConnected || !this.target(tabId) || !element.getClientRects().length) return [];
      const rect = element.getBoundingClientRect();
      if (![rect.x, rect.y, rect.width, rect.height].every(Number.isFinite) || rect.width <= 0 || rect.height <= 0) return [];
      return [{ tabId, slotId: tabId, bounds: { x: rect.x, y: rect.y, width: rect.width, height: rect.height } }];
    }).slice(0, 4);
    const signature = JSON.stringify({ blocked, slots });
    if (signature === this.lastPresentation && !forceBlocked) return;
    const revision = ++this.revision;
    await this.platform.present({ epoch: this.state.epoch, revision, slots, blocked });
    if (revision === this.revision) this.lastPresentation = signature;
  }
  dispose() {
    this.disposed = true; if (this.frame !== undefined) cancelAnimationFrame(this.frame);
    for (const stop of this.stopped) stop(); this.stopped = []; this.slots.clear(); this.holds.clear();
    void this.present().catch(this.report); this.listeners.clear(); this.events.clear();
  }
}
