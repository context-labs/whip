import type { WebContents } from 'electron';
import { randomUUID } from 'node:crypto';
import { browserURL, object } from './browser-policy';

// The selected guest's debugger is the authority boundary. No Browser/Network/
// Storage/Target forwarding and no debug listener, host paths or other sessions.
const methods = new Set([
  'Page.enable', 'Page.disable', 'Page.navigate', 'Page.reload', 'Page.stopLoading', 'Page.getFrameTree',
  'Page.getNavigationHistory', 'Page.navigateToHistoryEntry', 'Page.getLayoutMetrics', 'Page.captureScreenshot',
  'Page.addScriptToEvaluateOnNewDocument', 'Page.removeScriptToEvaluateOnNewDocument', 'Page.createIsolatedWorld', 'Page.handleJavaScriptDialog',
  'Runtime.enable', 'Runtime.disable', 'Runtime.evaluate', 'Runtime.callFunctionOn', 'Runtime.getProperties', 'Runtime.releaseObject', 'Runtime.releaseObjectGroup',
  'DOM.enable', 'DOM.disable', 'DOM.getDocument', 'DOM.describeNode', 'DOM.resolveNode', 'DOM.getBoxModel', 'DOM.getContentQuads',
  'DOM.scrollIntoViewIfNeeded', 'DOM.querySelector', 'DOM.querySelectorAll', 'DOM.getAttributes',
  'Accessibility.enable', 'Accessibility.disable', 'Accessibility.getFullAXTree', 'Accessibility.getPartialAXTree',
  'Input.dispatchMouseEvent', 'Input.dispatchKeyEvent', 'Input.insertText', 'Input.dispatchTouchEvent',
]);
export interface BrowserCDPCommand { method: string; params?: Record<string, unknown>; sessionId?: string }
export interface BrowserCDPEvent { method: string; params: Record<string, unknown>; sessionId?: string }
const commandLimit = 256 * 1024, eventLimit = 256 * 1024, resultLimit = 12 * 1024 * 1024;
function bytes(value: unknown): number { return Buffer.byteLength(JSON.stringify(value)); }
export function validateCDP(value: unknown, sessionId: string): BrowserCDPCommand {
  const command = object(value, ['method', 'params', 'sessionId']);
  if (typeof command.method !== 'string' || command.method.length > 128 || bytes(command) > commandLimit) throw new Error('Invalid CDP command');
  if (command.sessionId !== undefined && command.sessionId !== sessionId) throw new Error('Foreign CDP session');
  const params = command.params ?? {};
  if (!params || typeof params !== 'object' || Array.isArray(params)) throw new Error('Invalid CDP parameters');
  return { method: command.method, params: params as Record<string, unknown>, sessionId: command.sessionId as string | undefined };
}

/** Main-only selected-target adapter. Caller checks its exact live resource grant. */
export class ScopedBrowserDebugger {
  readonly sessionId = randomUUID();
  private closed = false;
  private pending = 0;
  private readonly lifetime = new AbortController();
  constructor(private contents: WebContents, readonly targetId: string, private options: {
    assertLive(): void;
    event(event: BrowserCDPEvent): void;
    revoked(reason: string): void;
  }) {
    options.assertLive();
    if (contents.isDestroyed() || contents.isDevToolsOpened() || contents.debugger.isAttached()) throw new Error('Browser debugger is busy');
    contents.debugger.attach('1.3');
    contents.debugger.on('message', this.message);
    contents.debugger.on('detach', this.detached);
  }
  private message = (_event: Electron.Event, method: string, params: Record<string, unknown>, sessionId?: string): void => {
    if (this.closed || sessionId || !/^(Page|Runtime|DOM|Accessibility)\./.test(method)) return;
    try {
      this.options.assertLive();
      const value = { method, params, sessionId: this.sessionId };
      if (bytes(value) > eventLimit) { this.close('Browser event exceeded its limit'); return; }
      this.options.event(value);
    } catch { this.close('Browser authority changed'); }
  };
  private detached = (): void => this.close('Browser debugger detached');
  private info() { return { targetId: this.targetId, type: 'page', url: this.contents.getURL(), title: this.contents.getTitle(), attached: true, canAccessOpener: false }; }
  async dispatch(value: unknown, signal?: AbortSignal): Promise<unknown> {
    this.options.assertLive();
    if (this.closed || this.contents.isDestroyed()) throw new Error('Browser attachment revoked');
    signal?.throwIfAborted();
    const command = validateCDP(value, this.sessionId), params = command.params!;
    // Rod's discovery/selection API is virtualized to one opaque guest identity.
    switch (command.method) {
      case 'Browser.getVersion': return { protocolVersion: '1.3', product: 'Chrome/' + process.versions.chrome, revision: '', userAgent: this.contents.getUserAgent(), jsVersion: process.versions.v8 };
      case 'Target.getTargets': object(params, []); return { targetInfos: [this.info()] };
      case 'Target.getTargetInfo':
        object(params, ['targetId']); if (params.targetId !== undefined && params.targetId !== this.targetId) throw new Error('Foreign CDP target'); return { targetInfo: this.info() };
      case 'Target.attachToTarget':
        object(params, ['targetId', 'flatten']); if (params.targetId !== this.targetId || params.flatten !== true) throw new Error('Foreign CDP target'); return { sessionId: this.sessionId };
      case 'Target.detachFromTarget':
        object(params, ['sessionId']); if (params.sessionId !== this.sessionId) throw new Error('Foreign CDP session'); return {};
      case 'Target.setDiscoverTargets':
        object(params, ['discover']); if (typeof params.discover !== 'boolean') throw new Error('Invalid discovery request');
        if (params.discover) this.options.event({ method: 'Target.targetCreated', params: { targetInfo: this.info() } });
        return {};
    }
    if (!methods.has(command.method)) throw new Error('CDP method is not supported');
    if (command.sessionId !== this.sessionId) throw new Error('Selected CDP session is required');
    if (['browserContextId', 'targetId', 'sessionId'].some(key => key in params)) throw new Error('Foreign CDP authority is forbidden');
    if (command.method === 'Page.navigate') params.url = browserURL(params.url);
    if (command.method === 'Page.captureScreenshot') {
      if (params.format !== undefined && !['png', 'jpeg', 'webp'].includes(params.format as string)) throw new Error('Invalid screenshot format');
      if (params.clip !== undefined) {
        const clip = object(params.clip, ['x', 'y', 'width', 'height', 'scale']);
        if (Object.values(clip).some(value => typeof value !== 'number' || !Number.isFinite(value)) ||
            ![clip.width, clip.height, clip.scale].every(value => (value as number) > 0) ||
            (clip.width as number) * (clip.height as number) * (clip.scale as number) ** 2 > 16_777_216) throw new Error('Screenshot exceeds its limit');
      }
    }
    if (this.pending >= 32) throw new Error('Browser CDP command limit reached');
    this.pending++;
    const lifetime = AbortSignal.any([this.lifetime.signal, ...(signal ? [signal] : [])]);
    let delivered = false;
    let abort: () => void = () => {};
    try {
      const cancelled = new Promise<never>((_resolve, reject) => {
        abort = () => reject(new Error(delivered ? 'outcome_unknown' : 'Browser command cancelled'));
        lifetime.addEventListener('abort', abort, { once: true });
      });
      lifetime.throwIfAborted(); this.options.assertLive(); delivered = true;
      const result = await Promise.race([this.contents.debugger.sendCommand(command.method, params), cancelled]);
      this.options.assertLive();
      if (bytes(result) > resultLimit) throw new Error('Browser result exceeded its limit');
      return result;
    } finally { lifetime.removeEventListener('abort', abort); this.pending--; }
  }
  close(reason = 'Browser attachment revoked'): void {
    if (this.closed) return; this.closed = true; this.lifetime.abort();
    this.contents.debugger.removeListener('message', this.message);
    this.contents.debugger.removeListener('detach', this.detached);
    if (!this.contents.isDestroyed() && this.contents.debugger.isAttached()) this.contents.debugger.detach();
    this.options.revoked(reason);
  }
}
