import path from 'node:path';
import type { HostPrompt, BrowserTabState, BrowserTarget } from '@whip/app/desktop-bridge';
import { browserID, browserLimits, browserURL, object, target } from './browser-policy';
import type { BrowserManager } from './browser-manager';
import type { BrowserControl } from './browser-control';
import type { BrowserPreviewAuthority } from './browser-preview-authority';

type Offer = Awaited<ReturnType<BrowserPreviewAuthority['prepareHuman']>>;
type Confirm = (attemptId: string, value: Omit<HostPrompt, 'id' | 'attemptId'>, signal: AbortSignal) => Promise<string[] | null>;
export function humanPreviewRequest(value: unknown) {
  const input = object(value, ['epoch', 'connectionId', 'runtimeId', 'projectId', 'url']);
  const epoch = browserID(input.epoch), connectionId = browserID(input.connectionId);
  if (typeof input.projectId !== 'string' || input.projectId.length > 4096 || /[\u0000-\u001f\u007f]/.test(input.projectId) || !input.projectId.startsWith('cwd:') || !path.posix.isAbsolute(input.projectId.slice(4)))
    throw new Error('Choose an absolute project directory from the selected host catalog');
  if (typeof input.runtimeId !== 'string' || !input.runtimeId || input.runtimeId.length > 4096 || /[\u0000-\u001f\u007f]/.test(input.runtimeId)) throw new Error('Invalid runtime identity');
  if (typeof input.url !== 'string') throw new Error('Enter a literal loopback preview URL');
  const provided = /^[a-z]+:\/\//i.test(input.url) ? input.url : `http://${input.url}`;
  if (!/^https?:\/\/(127\.0\.0\.1|\[::1\])(?::[0-9]+)?(?:[/?#]|$)/i.test(provided)) throw new Error('Use literal 127.0.0.1 or [::1] for the selected SSH host');
  const url = browserURL(provided), parsed = new URL(url);
  const loopback = parsed.hostname === '[::1]' ? '::1' as const : '127.0.0.1' as const;
  const port = Number(parsed.port || (parsed.protocol === 'https:' ? 443 : 80));
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('Invalid preview port');
  return { epoch, connectionId, runtimeId: input.runtimeId, projectId: input.projectId, url, loopback, port };
}

/** Explicit human consent is ephemeral and distinct from every agent grant. */
export class BrowserHumanPreview {
  private lifetime = new AbortController();
  private pending = new Map<string, { target: BrowserTarget; url: string; offer: Offer; timer: ReturnType<typeof setTimeout>; admitting?: boolean }>();
  constructor(private manager: BrowserManager, private previews: BrowserPreviewAuthority, private control: BrowserControl, private confirm: Confirm) {}
  private current(epoch: string) {
    this.lifetime.signal.throwIfAborted();
    const snapshot = this.manager.snapshot();
    if (snapshot.epoch !== epoch) throw new Error('The application window changed; reopen the preview picker');
    if (snapshot.tabs.length >= browserLimits.tabs) throw new Error('Close a Browser tab before opening an SSH preview');
  }
  async create(value: unknown): Promise<BrowserTabState | undefined> {
    const request = humanPreviewRequest(value); this.current(request.epoch);
    const offer = await this.previews.prepareHuman({ connectionId: request.connectionId, runtimeId: request.runtimeId, projectId: request.projectId, loopback: request.loopback, port: request.port });
    this.current(request.epoch); offer.assertCurrent();
    const signal = AbortSignal.any([this.lifetime.signal, offer.signal, AbortSignal.timeout(120_000)]);
    const unblock = this.manager.blockNative();
    let answer: string[] | null;
    try { answer = await this.confirm(request.connectionId, { title: 'Open SSH preview?', fields: [], confirmLabel: 'Open preview',
      message: `Host: ${offer.host}\nSaved host: ${offer.preview.host_id}\nRuntime: ${request.runtimeId}\nProject: ${request.projectId.slice(4)}\nURL: ${request.url}\nRemote loopback: ${request.loopback}\nApproved ports: ${offer.preview.ports!.join(', ')}\n\nAll tabs in this project environment gain network access to these ports. This does not grant any agent control. Existing agent controllers are revoked if the port scope expands.`,
    }, signal); } finally { unblock(); }
    if (answer === null) return undefined;
    signal.throwIfAborted(); this.current(request.epoch); await offer.verifyCurrent(); signal.throwIfAborted(); this.current(request.epoch); offer.assertCurrent();
    const page = this.manager.create({ epoch: request.epoch, url: request.url, environmentId: offer.preview.environment_id });
    const admitted = { epoch: request.epoch, tabId: page.id, generation: page.generation };
    const timer = setTimeout(() => { this.pending.delete(page.id); this.manager.discardUnadmitted(admitted); }, 15_000); timer.unref();
    this.pending.set(page.id, { target: admitted, url: request.url, offer, timer });
    return page;
  }
  assertIdle(value: unknown): void {
    const input = object(value, ['epoch', 'tabId', 'generation', 'action']);
    const state = this.manager.controlledState(target({ epoch: input.epoch, tabId: input.tabId, generation: input.generation }));
    if (state.environmentId && this.control.humanEnvironmentBusy(state.environmentId)) throw new Error('Human preview admission is in progress');
  }
  async admitted(value: unknown): Promise<void> {
    const input = target(value), pending = this.pending.get(input.tabId);
    if (!pending) { this.assertIdle(input); this.manager.admitted(input); return; }
    if (pending.admitting) throw new Error('Human preview admission is already in progress');
    if (JSON.stringify(input) !== JSON.stringify(pending.target)) throw new Error('Human preview admission identity changed');
    pending.admitting = true; clearTimeout(pending.timer);
    const unblock = this.manager.blockNative();
    const guard = () => {
      this.lifetime.signal.throwIfAborted(); pending.offer.signal.throwIfAborted();
      if (this.manager.snapshot().tabs.length > browserLimits.tabs) throw new Error('Browser admission capacity changed');
      const state = this.manager.controlledState(input);
      if (state.url !== pending.url || state.environmentId !== pending.offer.preview.environment_id) throw new Error('Human preview admission changed');
    };
    try {
      await this.control.withHumanEnvironment(pending.offer.preview.environment_id, async () => {
        await pending.offer.verifyCurrent(); guard();
        await pending.offer.admit(input.tabId, guard, async () => {
          guard(); this.manager.admitted(input); await this.manager.controlledContents(input); guard();
        }, () => this.manager.discardCreated(input));
      });
    } catch (error) { await this.manager.discardCreated(input); throw error; }
    finally { this.pending.delete(input.tabId); unblock(); }
  }
  dispose(): void {
    this.lifetime.abort();
    for (const pending of this.pending.values()) { clearTimeout(pending.timer); this.manager.discardUnadmitted(pending.target); }
    this.pending.clear();
  }
}
