import { BrowserWindow, WebContentsView, Menu, clipboard, dialog, session, type Session, type WebContents } from 'electron';
import { randomUUID } from 'node:crypto';
import type { BrowserEvent, BrowserInventory, BrowserPresentation, BrowserRestoreTab, BrowserShortcut, BrowserTabState, BrowserTarget } from '@whip/app/desktop-bridge';
import { browserAction, browserID, browserLimits, browserTitle, browserURL, guestResourceAllowed, nativeBounds, object, presentation, restoreTabs, target } from './browser-policy';

/** A tab-lifetime network lease. Proxy/auth secrets never leave Electron main. */
export interface BrowserEnvironmentLease {
  session: Session;
  bind?(contents: WebContents): void;
  close(): Promise<void>;
}
interface Entry {
  state: BrowserTabState;
  admitted: boolean;
  admissionTimer?: ReturnType<typeof setTimeout>;
  view?: WebContentsView;
  contents?: WebContents;
  realizing?: Promise<WebContents>;
  lease?: BrowserEnvironmentLease;
  releasing?: Promise<void>;
  closing?: Promise<{ status: 'closed' | 'cancelled' }>;
  closeRequested?: boolean;
  navigationAttempt?: number;
  cancelClose?: () => void;
}
export interface BrowserManagerOptions {
  environment?(id: string, tabId: string): Promise<BrowserEnvironmentLease>;
  confirm?(message: string, detail: string): boolean;
  invalidateControl?(tabId: string, reason: string): void;
  admissionTimeoutMs?: number;
}

/** One owner per application window; renderer mounts only present, never own guests. */
export class BrowserManager {
  readonly version = 1 as const;
  private epoch = randomUUID();
  private revision = 0;
  private layoutRevision = 0;
  private layout?: BrowserPresentation;
  private disposed = false;
  private entries = new Map<string, Entry>();
  private admissions = new Map<string, Set<() => void>>();
  private sessions = new Map<Session, () => void>();
  private listeners: Array<() => void> = [];

  private nativeBlocks = 0;
  constructor(private window: BrowserWindow, private emit: (event: BrowserEvent) => void, private options: BrowserManagerOptions = {}) {
    const contents = window.webContents;
    const rendererLost = () => this.resetRenderer();
    const navigating = (_event: unknown, _url: string, inPlace: boolean, main: boolean) => { if (main && !inPlace) this.resetRenderer(); };
    const resized = () => this.hide(); // A fresh measured snapshot must acknowledge new geometry.
    contents.on('render-process-gone', rendererLost);
    contents.on('did-start-navigation', navigating);
    window.on('resize', resized);
    window.on('hide', resized);
    window.once('closed', () => this.dispose());
    this.listeners.push(() => contents.removeListener('render-process-gone', rendererLost), () => contents.removeListener('did-start-navigation', navigating), () => window.removeListener('resize', resized), () => window.removeListener('hide', resized));
  }

  snapshot(): BrowserInventory {
    this.active();
    return { epoch: this.epoch, revision: this.revision, tabs: [...this.entries.values()].map(entry => this.copy(entry)) };
  }
  restore(value: unknown): BrowserInventory {
    const input = restoreTabs(value); this.requireEpoch(input.epoch);
    const incoming = input.tabs.filter(tab => !this.entries.has(tab.id));
    if (this.entries.size + incoming.length > browserLimits.restored) throw new Error('Browser restore limit exceeded');
    if (Buffer.byteLength(JSON.stringify([...this.entries.values()].map(entry => this.copy(entry)).concat(incoming as never[]))) > browserLimits.metadataBytes) throw new Error('Browser metadata limit exceeded');
    for (const tab of incoming) this.entries.set(tab.id, this.entry(tab, true, this.entries.size >= browserLimits.tabs));
    if (incoming.length) this.changed();
    return this.snapshot();
  }
  create(value: unknown): BrowserTabState { return this.createEntry(value); }
  /** Only the selected native provider may materialize pre-approved reserved IDs. */
  createControlled(value: unknown): BrowserTabState {
    const input = object(value, ['epoch', 'id', 'generation', 'url', 'environmentId']);
    return this.createEntry({ epoch: input.epoch, url: input.url, environmentId: input.environmentId }, { id: browserID(input.id), generation: browserID(input.generation) });
  }
  private createEntry(value: unknown, reserved?: { id: string; generation: string }): BrowserTabState {
    const input = object(value, ['epoch', 'url', 'environmentId']); this.requireEpoch(browserID(input.epoch));
    if (this.entries.size >= browserLimits.tabs) throw new Error('At most eight Browser tabs can be open; close a Browser tab first');
    const id = reserved?.id ?? randomUUID(), url = browserURL(input.url);
    if (this.entries.has(id)) throw new Error('Browser identity already exists');
    const environmentId = input.environmentId === undefined ? undefined : browserID(input.environmentId);
    const entry = this.entry({ id, url, environmentId }, false);
    if (reserved) entry.state.generation = reserved.generation;
    this.entries.set(id, entry);
    entry.admissionTimer = setTimeout(() => { if (!entry.admitted) this.remove(entry); }, this.options.admissionTimeoutMs ?? 15000);
    entry.admissionTimer.unref();
    this.changed(); // State-before-result; the renderer must still acknowledge admission.
    return this.copy(entry);
  }
  admitted(value: unknown): void {
    const entry = this.requireTarget(target(value)); entry.admitted = true;
    clearTimeout(entry.admissionTimer); entry.admissionTimer = undefined;
    for (const changed of this.admissions.get(entry.state.id) ?? []) changed();
  }
  async waitForAdmission(value: BrowserTarget, signal: AbortSignal): Promise<void> {
    const entry = this.requireTarget(value); signal.throwIfAborted();
    if (entry.admitted) return;
    await new Promise<void>((resolve, reject) => {
      const watchers = this.admissions.get(entry.state.id) ?? new Set<() => void>();
      this.admissions.set(entry.state.id, watchers);
      const done = () => {
        if (!signal.aborted && this.entries.get(entry.state.id) === entry && !entry.admitted) return;
        watchers.delete(done); if (!watchers.size) this.admissions.delete(entry.state.id);
        signal.removeEventListener('abort', done);
        if (signal.aborted || this.entries.get(entry.state.id) !== entry) reject(new Error('Browser admission cancelled')); else resolve();
      };
      watchers.add(done); signal.addEventListener('abort', done, { once: true }); done();
    });
    this.requireTarget(value);
  }
  discardUnadmitted(value: BrowserTarget): void {
    const entry = this.entries.get(value.tabId);
    if (entry && entry.state.generation === value.generation && !entry.admitted) this.remove(entry);
  }
  controlledState(value: BrowserTarget): BrowserTabState { return this.copy(this.requireTarget(value)); }
  async present(value: unknown): Promise<void> {
    const input = presentation(value); this.requireEpoch(input.epoch);
    if (input.revision <= this.layoutRevision) throw new Error('Stale browser presentation');
    for (const slot of input.slots) {
      const entry = this.entries.get(slot.tabId);
      if (!entry || !entry.admitted) throw new Error('Unknown or unadmitted browser tab');
    }
    this.layoutRevision = input.revision; this.layout = input;
    this.hide();
    if (input.blocked || this.nativeBlocks) { if (!this.window.isDestroyed()) this.window.webContents.focus(); return; }
    if (this.window.isDestroyed() || !this.window.isVisible()) return;
    await Promise.all(input.slots.map(async slot => {
      const entry = this.entries.get(slot.tabId)!;
      if (entry.state.status === 'crashed') return; // Recovery is an explicit human/agent action, never a presentation side effect.
      try { await this.realize(entry); } catch { return; } // Error snapshot is the visible unavailable surface.
      if (this.disposed || this.nativeBlocks || this.layout !== input || this.epoch !== input.epoch || !entry.view) return;
      const [width, height] = this.window.getContentSize();
      const bounds = nativeBounds(slot.bounds, this.window.webContents.getZoomFactor(), width, height);
      entry.view.setBounds(bounds);
      entry.view.setVisible(bounds.width > 0 && bounds.height > 0);
    }));
    if (this.layout !== input || this.epoch !== input.epoch) throw new Error('Browser presentation was superseded');
  }
  async act(value: unknown): Promise<void> {
    const input = object(value, ['epoch', 'tabId', 'generation', 'action']);
    const entry = this.requireTarget(target({ epoch: input.epoch, tabId: input.tabId, generation: input.generation }));
    const action = browserAction(input.action);
    if (!entry.admitted) throw new Error('Browser tab has not been admitted');
    const contents = await this.realize(entry, action.kind !== 'navigate');
    this.requireTarget({ epoch: input.epoch as string, tabId: entry.state.id, generation: input.generation as string });
    if (['navigate', 'back', 'forward', 'reload'].includes(action.kind)) this.options.invalidateControl?.(entry.state.id, 'human-navigation');
    switch (action.kind) {
      case 'navigate': await this.navigate(entry, action.url); break;
      case 'back': if (contents.navigationHistory.canGoBack()) contents.navigationHistory.goBack(); break;
      case 'forward': if (contents.navigationHistory.canGoForward()) contents.navigationHistory.goForward(); break;
      case 'reload': if (action.ignoreCache) contents.reloadIgnoringCache(); else contents.reload(); break;
      case 'stop': contents.stop(); break;
      case 'focus': if (entry.view?.getVisible() && !this.layout?.blocked) contents.focus(); break;
      case 'find': if (action.text) contents.findInPage(action.text, { forward: action.forward ?? true, findNext: action.findNext ?? false }); else contents.stopFindInPage('clearSelection'); break;
      case 'stop-find': contents.stopFindInPage({ clear: 'clearSelection', keep: 'keepSelection', activate: 'activateSelection' }[action.action] as 'clearSelection' | 'keepSelection' | 'activateSelection'); break;
      case 'zoom': contents.setZoomFactor(action.factor); entry.state.zoomFactor = action.factor; this.changed(); break;
      case 'devtools':
        this.options.invalidateControl?.(entry.state.id, 'devtools');
        if (action.open) { if (contents.debugger.isAttached()) contents.debugger.detach(); contents.openDevTools({ mode: 'detach' }); }
        else contents.closeDevTools();
        break;
      case 'clear-profile':
        if (!this.confirm('Clear website data?', 'Cookies, saved logins and website storage for every tab in this browser profile will be removed.')) return;
        for (const item of this.entries.values()) if (item.contents?.session === contents.session) this.options.invalidateControl?.(item.state.id, 'profile-cleared');
        await contents.session.clearStorageData(); await contents.session.clearCache();
        break;
    }
  }
  async close(value: unknown): Promise<{ status: 'closed' | 'cancelled' }> {
    const entry = this.requireTarget(target(value));
    if (entry.closing) return entry.closing;
    if (!entry.contents || entry.contents.isDestroyed()) { this.remove(entry); return { status: 'closed' }; }
    const contents = entry.contents;
    entry.closeRequested = true;
    const pending = new Promise<{ status: 'closed' | 'cancelled' }>(resolve => {
      let finished = false;
      const finish = (status: 'closed' | 'cancelled') => {
        if (finished) return; finished = true; clearTimeout(timer);
        contents.removeListener('destroyed', closed); entry.cancelClose = undefined; entry.closing = undefined; entry.closeRequested = false;
        if (status === 'closed') this.remove(entry);
        resolve({ status });
      };
      const closed = () => finish('closed');
      const timer = setTimeout(() => finish('cancelled'), 10000); timer.unref();
      entry.cancelClose = () => finish('cancelled');
      contents.once('destroyed', closed);
      contents.close({ waitForBeforeUnload: true });
    });
    if (entry.closeRequested) entry.closing = pending;
    return pending;
  }

  /** Main-only automation seam. A caller must independently validate its live grant. */
  async controlledContents(value: BrowserTarget): Promise<WebContents> {
    const entry = this.requireTarget(target(value));
    if (!entry.admitted) throw new Error('Browser admission is pending');
    const contents = await this.realize(entry); this.requireTarget(value);
    if (contents.isDevToolsOpened()) throw new Error('Close guest DevTools before controlling this page');
    return contents;
  }
  invalidateEnvironment(id: string, reason: string): void {
    for (const entry of this.entries.values()) if (entry.state.environmentId === id) {
      this.releaseNative(entry); entry.state.generation = randomUUID(); entry.state.status = 'unavailable';
      entry.state.error = { code: 'environment_unavailable', message: reason.slice(0, 256) };
    }
    this.changed();
  }
  /** Main-owned prompts cannot be covered by a late renderer presentation. */
  blockNative(): () => void {
    this.nativeBlocks++; this.hide(); if (!this.window.isDestroyed()) this.window.webContents.focus();
    let released = false;
    return () => { if (!released) { released = true; this.nativeBlocks--; this.hide(); } };
  }
  /** Roll back only a newly owned admission, including a partially realized guest. */
  async discardCreated(value: BrowserTarget): Promise<void> {
    const entry = this.entries.get(value.tabId);
    if (!entry || entry.state.generation !== value.generation || value.epoch !== this.epoch) return;
    this.remove(entry); await entry.realizing?.catch(() => {}); await entry.releasing;
  }
  hide(): void { for (const entry of this.entries.values()) entry.view?.setVisible(false); }
  resetRenderer(): void {
    if (this.disposed) return;
    this.hide(); this.epoch = randomUUID(); this.layout = undefined; this.layoutRevision = 0;
    for (const entry of this.entries.values()) {
      this.options.invalidateControl?.(entry.state.id, 'renderer-lost');
      if (!entry.admitted) this.remove(entry);
    }
    this.changed();
  }
  dispose(): void {
    if (this.disposed) return; this.disposed = true; this.hide();
    for (const cleanup of this.listeners.splice(0)) cleanup();
    for (const entry of this.entries.values()) { clearTimeout(entry.admissionTimer); this.releaseNative(entry); }
    this.entries.clear();
    for (const cleanup of this.sessions.values()) cleanup(); this.sessions.clear();
  }

  private entry(tab: BrowserRestoreTab, admitted: boolean, overflow = false): Entry {
    return { admitted, state: { id: tab.id, generation: randomUUID(), documentGeneration: 0, status: overflow ? 'unavailable' : 'restored', url: tab.url, title: browserTitle(tab.titleHint ?? ''), loading: false, canGoBack: false, canGoForward: false, zoomFactor: 1, ...(tab.environmentId ? { environmentId: tab.environmentId } : {}), ...(overflow ? { error: { code: 'capacity', message: 'Close a Browser tab to make room for this restored page' } } : {}) } };
  }
  private copy(entry: Entry): BrowserTabState { return { ...entry.state, ...(entry.state.error ? { error: { ...entry.state.error } } : {}) }; }
  private active(): void { if (this.disposed || this.window.isDestroyed()) throw new Error('Browser window is closed'); }
  private requireEpoch(epoch: string): void { this.active(); if (epoch !== this.epoch) throw new Error('Stale browser window'); }
  private requireTarget(value: BrowserTarget): Entry {
    this.requireEpoch(value.epoch); const entry = this.entries.get(value.tabId);
    if (!entry || entry.state.generation !== value.generation) throw new Error('Unknown or stale browser tab');
    return entry;
  }
  private changed(): void { if (!this.disposed) { this.revision++; this.emit({ kind: 'snapshot', snapshot: this.snapshot() }); } }
  private confirm(message: string, detail: string): boolean {
    return this.options.confirm?.(message, detail) ?? dialog.showMessageBoxSync(this.window, { type: 'warning', message, detail, buttons: ['Cancel', 'Continue'], defaultId: 0, cancelId: 0, noLink: true }) === 1;
  }
  private remove(entry: Entry): void {
    if (this.entries.get(entry.state.id) !== entry) return;
    this.entries.delete(entry.state.id); clearTimeout(entry.admissionTimer); this.releaseNative(entry);
    for (const changed of this.admissions.get(entry.state.id) ?? []) changed(); this.changed();
  }
  private releaseNative(entry: Entry): void {
    this.options.invalidateControl?.(entry.state.id, 'closed');
    const view = entry.view, contents = entry.contents; entry.view = undefined; entry.contents = undefined;
    if (view && !this.window.isDestroyed()) this.window.contentView.removeChildView(view);
    if (contents && !contents.isDestroyed()) { if (contents.debugger.isAttached()) contents.debugger.detach(); contents.close(); }
    if (entry.lease) { entry.releasing = Promise.all([entry.releasing, entry.lease.close().catch(() => {})]).then(() => {}); entry.lease = undefined; }
  }
  private async realize(entry: Entry, initialNavigation = true): Promise<WebContents> {
    this.active();
    if (entry.contents && !entry.contents.isDestroyed()) return entry.contents;
    if (entry.realizing) return entry.realizing;
    const occupied = [...this.entries.values()].filter(item => item.contents || item.realizing).length;
    if (occupied >= browserLimits.tabs) {
      entry.state.status = 'unavailable'; entry.state.error = { code: 'capacity', message: 'Close a Browser tab to make room for this page' }; this.changed();
      throw new Error('Browser page limit exceeded');
    }
    const generation = entry.state.generation;
    const promise = (async () => {
      let lease: BrowserEnvironmentLease | undefined;
      let created: WebContents | undefined;
      try {
        if (entry.state.environmentId) {
          if (!this.options.environment) throw new Error('Preview environment is unavailable');
          lease = await this.options.environment(entry.state.environmentId, entry.state.id);
        }
        if (this.disposed || this.entries.get(entry.state.id) !== entry || generation !== entry.state.generation) { throw new Error('Browser page was closed'); }
        const profile = lease?.session ?? session.fromPartition('persist:whip-browser-normal-v1');
        this.secureSession(profile);
        const view = new WebContentsView({ webPreferences: { session: profile, sandbox: true, contextIsolation: true, nodeIntegration: false, webSecurity: true, webviewTag: false, spellcheck: true, navigateOnDragDrop: false } });
        const contents = view.webContents; created = contents;
        entry.lease = lease; entry.view = view; entry.contents = contents;
        // A newly admitted background page still needs a real viewport for DOM and screenshots.
        view.setBounds({ x: 0, y: 0, width: 800, height: 600 });
        view.setVisible(false); this.window.contentView.addChildView(view);
        if (lease) contents.setWebRTCIPHandlingPolicy('disable_non_proxied_udp');
        lease?.bind?.(contents); this.watch(entry, contents);
        entry.state.status = 'ready'; delete entry.state.error; this.changed();
        if (initialNavigation) void this.navigate(entry, entry.state.url).catch(() => {});
        return contents;
      } catch (error) {
        if (created && entry.contents === created) { this.releaseNative(entry); lease = undefined; }
        await lease?.close().catch(() => {});
        if (!this.disposed && this.entries.get(entry.state.id) === entry) {
          entry.state.status = 'unavailable'; entry.state.error = { code: entry.state.environmentId ? 'environment_unavailable' : 'page_unavailable', message: entry.state.environmentId ? 'This preview environment is unavailable. Reconnect and approve it again.' : 'This browser page could not be created.' }; this.changed();
        }
        throw error;
      }
    })();
    entry.realizing = promise;
    try { return await promise; } finally { if (entry.realizing === promise) entry.realizing = undefined; }
  }
  private async navigate(entry: Entry, url: string): Promise<void> {
    const destination = browserURL(url), contents = entry.contents!;
    const attempt = (entry.navigationAttempt ?? 0) + 1; entry.navigationAttempt = attempt;
    entry.state.pendingURL = destination; delete entry.state.error; this.changed();
    try { await contents.loadURL(destination); }
    catch {
      if (!contents.isDestroyed() && this.entries.get(entry.state.id) === entry && entry.navigationAttempt === attempt) {
        entry.state.loading = false; entry.state.error = { code: 'navigation_failed', message: 'The page could not be loaded. Check the address or try again.' }; this.changed();
      }
      throw new Error('Browser navigation failed');
    }
  }
  private secureSession(profile: Session): void {
    if (this.sessions.has(profile)) return;
    profile.setPermissionCheckHandler(() => false);
    profile.setPermissionRequestHandler((_contents, _permission, callback) => callback(false));
    profile.webRequest.onBeforeRequest((details, callback) => {
      // An explicit, current DevTools identity may load its own internal frontend.
      const devtools = [...this.entries.values()].some(entry => entry.contents?.devToolsWebContents?.id === details.webContentsId);
      callback({ cancel: !guestResourceAllowed(details.url) && !(devtools && details.url.startsWith('devtools:')) });
    });
    const download = (event: Electron.Event, _item: Electron.DownloadItem, contents: WebContents) => {
      event.preventDefault(); const entry = [...this.entries.values()].find(entry => entry.contents === contents);
      if (entry) { entry.state.error = { code: 'download_blocked', message: 'This page requested a download. Automatic website downloads are blocked.' }; this.changed(); }
    };
    profile.on('will-download', download);
    this.sessions.set(profile, () => { profile.removeListener('will-download', download); /* Keep the fail-closed session guards for background workers. */ });
  }
  private watch(entry: Entry, contents: WebContents): void {
    const identity = (): BrowserTarget => ({ epoch: this.epoch, tabId: entry.state.id, generation: entry.state.generation });
    const current = () => !this.disposed && this.entries.get(entry.state.id) === entry && entry.contents === contents;
    const updated = () => { if (current()) { entry.state.canGoBack = contents.navigationHistory.canGoBack(); entry.state.canGoForward = contents.navigationHistory.canGoForward(); this.changed(); } };
    contents.setWindowOpenHandler(() => { if (current()) { entry.state.error = { code: 'popup_blocked', message: 'A website popup was blocked. Open its address in a new Browser tab.' }; this.changed(); } return { action: 'deny' }; });
    contents.on('context-menu', (_event, details) => {
      if (!current()) return;
      const items: Electron.MenuItemConstructorOptions[] = [
        { label: 'Back', enabled: contents.navigationHistory.canGoBack(), click: () => contents.navigationHistory.goBack() },
        { label: 'Forward', enabled: contents.navigationHistory.canGoForward(), click: () => contents.navigationHistory.goForward() },
        { label: 'Reload', click: () => contents.reload() },
      ];
      if (details.selectionText || details.isEditable) items.push({ type: 'separator' },
        { label: 'Copy', enabled: details.editFlags.canCopy, click: () => contents.copy() });
      if (details.isEditable) items.push(
        { label: 'Cut', enabled: details.editFlags.canCut, click: () => contents.cut() },
        { label: 'Paste', enabled: details.editFlags.canPaste, click: () => contents.paste() },
        { label: 'Select All', click: () => contents.selectAll() });
      if (details.linkURL) {
        try { const url = browserURL(details.linkURL); items.push({ type: 'separator' }, { label: 'Copy Link Address', click: () => clipboard.writeText(url) }); } catch { /* No privileged address reaches the native clipboard menu. */ }
      }
      items.push({ type: 'separator' }, { label: 'Inspect Element', click: () => {
        if (!current()) return; this.options.invalidateControl?.(entry.state.id, 'devtools');
        if (contents.debugger.isAttached()) contents.debugger.detach(); contents.inspectElement(details.x, details.y);
      } });
      Menu.buildFromTemplate(items).popup({ window: this.window });
    });
    contents.on('will-attach-webview', event => event.preventDefault());
    contents.on('will-frame-navigate', event => { if (event.isMainFrame) { try { browserURL(event.url); } catch { event.preventDefault(); } } else if (!guestResourceAllowed(event.url)) event.preventDefault(); });
    contents.on('will-redirect', (event, url) => { try { browserURL(url); } catch { event.preventDefault(); } });
    contents.on('did-start-navigation', (_event, url, inPlace, main) => {
      if (!main || !current()) return;
      if (!inPlace) { entry.state.documentGeneration++; this.options.invalidateControl?.(entry.state.id, 'document-changed'); }
      entry.state.pendingURL = url.length <= browserLimits.urlBytes ? url : undefined; entry.state.loading = true; delete entry.state.error; this.changed();
    });
    const committed = (url: string) => { if (!current()) return; try { entry.state.url = browserURL(url); } catch { return; } entry.state.status = 'ready'; delete entry.state.pendingURL; updated(); };
    contents.on('did-navigate', (_event, url) => committed(url));
    contents.on('did-navigate-in-page', (_event, url, main) => { if (main) committed(url); });
    contents.on('did-start-loading', () => { if (current()) { entry.state.loading = true; this.changed(); } });
    contents.on('did-stop-loading', () => { if (current()) { entry.state.loading = false; updated(); } });
    contents.on('page-title-updated', (_event, title) => { if (current()) { entry.state.title = browserTitle(title); this.changed(); } });
    contents.on('did-fail-load', (_event, code, _description, _url, main) => {
      if (current() && main && code !== -3) { entry.state.loading = false; entry.state.error = { code: 'navigation_failed', message: 'The page could not be loaded. Check the address or try again.' }; this.changed(); }
    });
    contents.on('focus', () => { if (current()) this.emit({ kind: 'focused', ...identity() }); });
    contents.on('found-in-page', (_event, result) => { if (current()) this.emit({ kind: 'find', ...identity(), requestId: result.requestId, matches: result.matches, activeMatchOrdinal: result.activeMatchOrdinal, final: result.finalUpdate }); });
    contents.on('before-input-event', (event, input) => {
      if (input.type !== 'keyDown' || input.isAutoRepeat || !current()) return;
      const modifier = process.platform === 'darwin' ? input.meta : input.control;
      let shortcut: BrowserShortcut | undefined;
      if (modifier && !input.alt) shortcut = ({ l: 'address', f: 'find', r: 'reload', w: 'close', k: 'commands', t: 'new-browser', '+': 'zoom-in', '=': 'zoom-in', '-': 'zoom-out', '0': 'zoom-reset' } as Record<string, BrowserShortcut>)[input.key.toLowerCase()];
      if (input.control && !input.meta && !input.alt && input.key === 'Tab') shortcut = input.shift ? 'tab-previous' : 'tab-next';
      if (input.alt && !modifier) shortcut = input.key === 'ArrowLeft' ? 'back' : input.key === 'ArrowRight' ? 'forward' : undefined;
      if (shortcut) {
        event.preventDefault();
        if (['address', 'find', 'commands', 'new-browser', 'tab-next', 'tab-previous', 'close'].includes(shortcut)) this.window.webContents.focus();
        this.emit({ kind: 'shortcut', ...identity(), shortcut });
      }
    });
    contents.on('will-prevent-unload', event => {
      if (this.confirm('Leave this page?', 'Unsaved changes on this website may be lost.')) event.preventDefault();
      else entry.cancelClose?.();
    });
    contents.on('devtools-opened', () => this.options.invalidateControl?.(entry.state.id, 'devtools'));
    contents.on('render-process-gone', () => {
      if (!current()) return;
      this.options.invalidateControl?.(entry.state.id, 'crashed'); this.releaseNative(entry); entry.state.generation = randomUUID();
      entry.state.status = 'crashed'; entry.state.loading = false; entry.state.documentGeneration++;
      entry.state.error = { code: 'crashed', message: 'This browser page stopped responding. Reload to recover it.' }; this.changed();
    });
    contents.on('destroyed', () => {
      if (!current() || entry.closeRequested) return;
      this.releaseNative(entry); entry.state.generation = randomUUID(); entry.state.status = 'unavailable'; entry.state.loading = false;
      this.options.invalidateControl?.(entry.state.id, 'destroyed'); this.changed();
    });
  }
}
