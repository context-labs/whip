import { randomUUID } from 'node:crypto';
import { isDeepStrictEqual } from 'node:util';
import { assertValid, type BrowserCommand, type BrowserCommandCancel, type BrowserProviderEventParams } from '@whip/protocol';
import type { BrowserAgentIdentity, BrowserAgentSelection, BrowserAgentEvent, BrowserAgentResult, BrowserAgentScope, BrowserTarget, BrowserEvent } from '@whip/app/desktop-bridge';
import { BrowserManager } from './browser-manager';
import { ScopedBrowserDebugger } from './browser-cdp';
import { browserID, browserURL, object } from './browser-policy';

type Selection = { input: BrowserAgentSelection; epoch: string; lifetime: AbortController; seen: Set<string> };
type Operation = { id: string; lifetime: AbortController; navigating?: boolean };
type Attachment = { selection: Selection; scope: BrowserAgentScope; agentId: string; target: BrowserTarget; sequence: bigint; debugger?: ScopedBrowserDebugger; operation?: Operation; live: boolean; lastState?: string };
class ControlError extends Error { constructor(readonly kind: string, message: string) { super(message); } }
function fail(kind: string, message: string): never { throw new ControlError(kind, message); }
const same = (a: unknown, b: unknown) => isDeepStrictEqual(a, b);
const clone = <T>(value: T): T => structuredClone(value);

/** Native selected-provider owner. Wire observations can never mint authority. */
export class BrowserControl {
  private readonly desktopId = randomUUID();
  private readonly windowId = randomUUID();
  private readonly profileId = randomUUID();
  private readonly selections = new Map<string, Selection>();
  private readonly attachments = new Map<string, Attachment>();
  private readonly reservations = new Set<string>();
  private readonly expanding = new Map<string, Attachment>();
  private readonly humanChanges = new Set<string>();
  private readonly pending = new Map<string, { command: BrowserCommand; lifetime: AbortController }>();
  constructor(private manager: BrowserManager, private emit: (event: BrowserAgentEvent) => void, private options: {
    prepare?(selection: BrowserAgentSelection): Promise<void>;
    previewState?(id: string): BrowserAgentScope['preview'];
    preview?(selection: BrowserAgentSelection, scope: BrowserAgentScope): Promise<string>;
    expand?(selection: BrowserAgentSelection, scope: BrowserAgentScope, port: number): Promise<void>;
    release?(selection: BrowserAgentSelection): void;
  } = {}) {}
  identity(): BrowserAgentIdentity {
    const inventory = this.manager.snapshot();
    return { desktopId: this.desktopId, windowId: this.windowId, createProfileId: this.profileId,
      tabs: inventory.tabs.flatMap(tab => {
        const preview = tab.environmentId ? this.options.previewState?.(tab.environmentId) : undefined;
        if (tab.environmentId && !preview) return [];
        return [{ tab_id: tab.id, tab_generation: tab.generation, profile_id: tab.environmentId ?? this.profileId,
          document_revision: String(tab.documentGeneration), url: tab.url, title: tab.title, ...(preview ? { preview } : {}) }];
      }) };
  }
  async select(value: unknown): Promise<void> {
    const input = object(value, ['offer', 'provider', 'connectionId', 'projectId']) as unknown as BrowserAgentSelection;
    assertValid('BrowserProviderBindParams', input.offer); assertValid('BrowserProviderBindResult', input.provider);
    const { offer, provider } = input;
    browserID(offer.root_id); browserID(provider.provider_id); browserID(provider.provider_epoch);
    if (offer.version !== 1 || provider.version !== 1 || offer.desktop_id !== this.desktopId || offer.window_id !== this.windowId || offer.create_profile_id !== this.profileId) fail('desktop_unavailable', 'Native provider identity changed');
    if ((offer.offered_tabs?.length ?? 0) > 32 || (offer.offered_preview_hosts?.length ?? 0) > 16 || this.selections.size >= 32 && !this.selections.has(offer.root_id)) fail('browser_busy', 'Native provider capacity reached');
    const current = this.identity(), seen = new Set<string>();
    for (const tab of offer.offered_tabs ?? []) {
      const live = current.tabs.find(value => value.tab_id === tab.tab_id);
      if (!live || live.tab_generation !== tab.tab_generation || live.profile_id !== tab.profile_id || !same(live.preview ?? null, tab.preview ?? null) || seen.has(tab.tab_id)) fail('attachment_revoked', 'The offered tab changed');
      seen.add(tab.tab_id);
    }
    if ((offer.offered_preview_hosts?.length || offer.offered_tabs?.some(tab => tab.preview)) && !this.options.prepare) fail('host_not_connected', 'Native preview authority is unavailable');
    await this.options.prepare?.(input);
    const epoch = this.manager.snapshot().epoch;
    const old = this.selections.get(offer.root_id);
    if (old) this.release({ rootId: offer.root_id, providerEpoch: old.input.provider.provider_epoch });
    this.selections.set(offer.root_id, { input: clone(input), epoch, lifetime: new AbortController(), seen: new Set() });
  }
  release(value: unknown): void {
    const input = object(value, ['rootId', 'providerEpoch']);
    const selection = this.selections.get(browserID(input.rootId));
    if (!selection || selection.input.provider.provider_epoch !== input.providerEpoch) return;
    selection.lifetime.abort(); this.selections.delete(input.rootId as string);
    for (const attachment of [...this.attachments.values()]) if (attachment.selection === selection) this.revoke(attachment, 'Provider association ended');
    this.options.release?.(selection.input);
  }
  cancel(value: unknown): void {
    assertValid('BrowserCommandCancel', value); const input = value as BrowserCommandCancel;
    const pending = this.pending.get(input.command_id);
    if (pending && pending.command.root_id === input.root_id && pending.command.provider_epoch === input.provider_epoch && pending.command.scope.attachment_generation === input.attachment_generation) pending.lifetime.abort();
  }
  invalidate(tabId: string, reason: string): void {
    const attachment = this.attachments.get(tabId); if (!attachment) return;
    if (reason === 'document-changed') {
      const operation = attachment.operation;
      // Only a dispatched explicit navigation can claim this document transition.
      // User/page-initiated navigation is not attributed merely because a batch is active.
      const ownNavigation = operation?.navigating && !operation.lifetime.signal.aborted;
      if (!ownNavigation) operation?.lifetime.abort();
      this.event(attachment, 'document', undefined, undefined, ownNavigation ? operation.id : undefined); return;
    }
    if (reason === 'human-navigation') { attachment.operation?.lifetime.abort(); this.event(attachment, 'document'); return; }
    this.revoke(attachment, reason);
  }
  observe(event: BrowserEvent): void {
    if (event.kind !== 'snapshot') return;
    for (const attachment of [...this.attachments.values()]) {
      const state = event.snapshot.tabs.find(tab => tab.id === attachment.target.tabId);
      if (!state || state.generation !== attachment.target.generation || event.snapshot.epoch !== attachment.target.epoch) { this.revoke(attachment, 'Native page identity changed'); continue; }
      const key = JSON.stringify([state.url, state.title, state.documentGeneration, state.status]);
      if (attachment.lastState !== key) { attachment.lastState = key; this.event(attachment, 'state', undefined, undefined, attachment.operation?.id); }
    }
  }
  invalidateEnvironment(id: string, reason: string): void {
    for (const attachment of [...this.attachments.values()]) if (attachment.scope.preview?.environment_id === id && !(this.expanding.get(id) === attachment && reason === 'Preview destination scope changed')) this.revoke(attachment, reason);
  }
  humanEnvironmentBusy(id: string): boolean { return this.humanChanges.has(id); }
  /** Human scope changes exclude in-flight model work and all new commands atomically. */
  async withHumanEnvironment<T>(id: string, work: () => Promise<T>): Promise<T> {
    if (this.humanChanges.has(id) || this.expanding.has(id) ||
        [...this.pending.values()].some(item => item.command.scope.preview?.environment_id === id) ||
        [...this.attachments.values()].some(item => item.scope.preview?.environment_id === id && item.operation))
      fail('browser_busy', 'End the active browser operation before changing human preview access');
    this.humanChanges.add(id);
    try { return await work(); } finally { this.humanChanges.delete(id); }
  }
  private current(command: BrowserCommand): Selection {
    if (command.scope.preview && this.humanChanges.has(command.scope.preview.environment_id)) fail('browser_busy', 'Human preview admission is changing this environment');
    const selection = this.selections.get(command.root_id);
    if (!selection || selection.lifetime.signal.aborted || selection.epoch !== this.manager.snapshot().epoch ||
        selection.input.provider.provider_epoch !== command.provider_epoch || command.scope.provider_epoch !== command.provider_epoch ||
        selection.input.provider.provider_id !== command.scope.provider_id) fail('attachment_revoked', 'Native provider authority is no longer current');
    return selection;
  }
  private previewCurrent(scope: BrowserAgentScope, expanding?: Attachment): void {
    if (!scope.preview) return;
    const owner = this.expanding.get(scope.preview.environment_id);
    if (owner && owner !== expanding) fail('browser_busy', 'Preview scope is changing');
    if (owner === expanding && expanding) return;
    const current = this.options.previewState?.(scope.preview.environment_id);
    if (!current || !same(current, scope.preview)) fail('attachment_revoked', 'Preview scope is no longer current');
  }
  private attached(command: BrowserCommand, selection: Selection): Attachment {
    const attachment = this.attachments.get(command.scope.tab_id);
    if (!attachment || !attachment.live || attachment.selection !== selection || attachment.agentId !== command.agent_id || !same(attachment.scope, command.scope)) fail('attachment_revoked', 'Browser attachment authority changed');
    this.manager.controlledState(attachment.target); this.previewCurrent(attachment.scope); return attachment;
  }
  private assertAttachment(attachment: Attachment): void {
    if (!attachment.live || this.attachments.get(attachment.scope.tab_id) !== attachment || attachment.selection.lifetime.signal.aborted ||
        this.selections.get(attachment.selection.input.offer.root_id) !== attachment.selection) fail('attachment_revoked', 'Browser attachment revoked');
    this.manager.controlledState(attachment.target); this.previewCurrent(attachment.scope, attachment);
  }
  private event(attachment: Attachment, kind: string, method?: string, params?: unknown, operationId?: string): void {
    let document = '', url = '', title = ''; try { const state = this.manager.controlledState(attachment.target); document = String(state.documentGeneration); url = state.url; title = state.title; } catch { /* Revocation may follow native removal. */ }
    const scope = attachment.scope;
    const event: BrowserProviderEventParams = { root_id: attachment.selection.input.offer.root_id, provider_epoch: scope.provider_epoch,
      tab_id: scope.tab_id, tab_generation: scope.tab_generation, attachment_id: scope.attachment_id!, attachment_generation: scope.attachment_generation!,
      sequence: String(++attachment.sequence), document_revision: document, url, title, kind, ...(method ? { method, params } : {}), ...(operationId ? { operation_id: operationId } : {}) };
    this.emit({ kind: 'provider', event });
  }
  private revoke(attachment: Attachment, reason: string, notify = true): void {
    if (!attachment.live) return; attachment.live = false; attachment.operation?.lifetime.abort();
    attachment.debugger?.close(reason); this.attachments.delete(attachment.scope.tab_id);
    if (notify) this.event(attachment, 'revoked');
  }
  private metadata(attachment: Attachment) {
    const state = this.manager.controlledState(attachment.target), preview = attachment.scope.preview;
    return { attachment_id: attachment.scope.attachment_id, tab_id: state.id, document_revision: String(state.documentGeneration), url: state.url, title: state.title,
      network: preview ? { kind: 'ssh', host_id: preview.host_id, ports: preview.ports ?? [] } : { kind: 'mac', ports: [] },
      supported_operations: preview ? ['run', 'detach', 'allow_preview_port'] : ['run', 'detach'], media: [] };
  }
  async dispatch(value: unknown): Promise<BrowserAgentResult> {
    assertValid('BrowserCommand', value); const command = clone(value as BrowserCommand);
    const result: BrowserAgentResult = { command_id: command.command_id, root_id: command.root_id, provider_epoch: command.provider_epoch,
      attachment_generation: command.scope.attachment_generation ?? '', document_revision: '' };
    let timer: ReturnType<typeof setTimeout> | undefined;
    let started = false, reserved = false;
    let signal: AbortSignal | undefined;
    let acquired: Attachment | undefined;
    try {
      if (Buffer.byteLength(JSON.stringify(command)) > 256 * 1024) fail('unsupported_operation', 'Browser command exceeds its limit');
      for (const id of [command.command_id, command.operation_id, command.root_id, command.agent_id, command.scope.tab_id, command.scope.tab_generation, command.scope.attachment_id, command.scope.attachment_generation]) browserID(id);
      if (!command.scope.rights?.includes('control') || command.scope.rights.some(right => !['create', 'control', 'route'].includes(right))) fail('permission_denied', 'Unsupported browser rights');
      const selection = this.current(command);
      if (selection.seen.has(command.command_id)) fail('outcome_unknown', 'Duplicate browser command is not replayed');
      if (selection.seen.size >= 16384 || this.pending.size >= 128 || this.pending.has(command.command_id)) fail('browser_busy', 'Native browser command capacity reached');
      const deadline = Number(command.deadline_millis);
      if (!/^\d+$/.test(command.deadline_millis) || !Number.isSafeInteger(deadline) || deadline <= Date.now() || deadline > Date.now() + 125000) fail('attachment_revoked', 'Browser command deadline expired or invalid');
      selection.seen.add(command.command_id);
      const lifetime = new AbortController(); this.pending.set(command.command_id, { command, lifetime });
      signal = AbortSignal.any([selection.lifetime.signal, lifetime.signal]);
      timer = setTimeout(() => lifetime.abort(), Math.max(1, deadline - Date.now())); timer.unref();
      started = true;
      if (command.kind === 'open' || command.kind === 'attach') {
        if (this.attachments.has(command.scope.tab_id) || this.reservations.has(command.scope.tab_id)) fail('browser_busy', 'This page already has a controller');
        this.reservations.add(command.scope.tab_id); reserved = true;
        const args = object(command.arguments, ['url', 'preview_host_id', 'tab_id', 'attachment_id', 'code', 'expected_document', 'timeout', 'port']);
        let target: BrowserTarget;
        if (command.kind === 'open') {
          if (!command.scope.rights.includes('create') || command.scope.profile_id !== this.profileId) fail('permission_denied', 'Browser creation is not offered');
          if (command.scope.preview) {
            const requested = command.scope.preview, url = new URL(browserURL(args.url));
            if (this.expanding.has(requested.environment_id)) fail('browser_busy', 'Preview scope is changing');
            const loopback = url.hostname.replace(/^\[|\]$/g, '');
            const port = Number(url.port || (url.protocol === 'https:' ? 443 : 80));
            const offered = selection.input.offer.offered_preview_hosts?.find(preview => same({ ...preview, ports: [] }, { ...requested, ports: [] }));
            const ports = [...new Set([...(offered?.ports ?? []), port])].sort((a, b) => a - b);
            if (!offered || !['http:', 'https:'].includes(url.protocol) || !!url.username || !!url.password || loopback !== requested.loopback || !same(ports, requested.ports)) fail('host_not_connected', 'Preview is not in the selected offer');
          }
          const environmentId = command.scope.preview ? await this.options.preview?.(selection.input, command.scope) : undefined;
          if (command.scope.preview && !environmentId) fail('host_not_connected', 'Preview environment is unavailable');
          signal.throwIfAborted(); this.current(command); this.previewCurrent(command.scope);
          const tab = this.manager.createControlled({ epoch: selection.epoch, id: command.scope.tab_id, generation: command.scope.tab_generation, url: browserURL(args.url), environmentId });
          target = { epoch: selection.epoch, tabId: tab.id, generation: tab.generation };
          try {
            this.emit({ kind: 'admission', commandId: command.command_id, rootId: command.root_id, providerEpoch: command.provider_epoch, epoch: selection.epoch, tab });
            await this.manager.waitForAdmission(target, signal);
          } catch (error) { this.manager.discardUnadmitted(target); throw error; }
        } else {
          const offered = selection.input.offer.offered_tabs?.find(tab => tab.tab_id === command.scope.tab_id);
          if (!offered || offered.tab_generation !== command.scope.tab_generation || offered.profile_id !== command.scope.profile_id || !same(offered.preview ?? null, command.scope.preview ?? null)) fail('permission_denied', 'This browser page is not offered');
          target = { epoch: selection.epoch, tabId: command.scope.tab_id, generation: command.scope.tab_generation };
        }
        signal.throwIfAborted(); this.current(command); this.previewCurrent(command.scope);
        const contents = await this.manager.controlledContents(target);
        signal.throwIfAborted(); this.current(command); this.previewCurrent(command.scope);
        if (contents.isDevToolsOpened() || contents.debugger.isAttached()) fail('browser_busy', 'Page DevTools or another debugger is attached');
        const attachment: Attachment = { selection, scope: clone(command.scope), agentId: command.agent_id, target, sequence: 0n, live: true };
        this.attachments.set(command.scope.tab_id, attachment); acquired = attachment; result.result = this.metadata(attachment);
      } else if (command.kind === 'transfer') {
        const args = object(command.arguments, ['child_agent_id', 'attachments']); browserID(args.child_agent_id);
        if (!Array.isArray(args.attachments) || !args.attachments.length || args.attachments.length > 8) fail('permission_denied', 'Invalid transfer');
        const moves = args.attachments.map(value => {
          const item = object(value, ['parent_scope', 'child_scope']);
          const parent = item.parent_scope as BrowserAgentScope, child = item.child_scope as BrowserAgentScope;
          assertValid('BrowserCommand', { ...command, scope: parent }); assertValid('BrowserCommand', { ...command, scope: child });
          const attachment = this.attached({ ...command, scope: parent }, selection);
          if (attachment.operation || !child || child.tab_id !== parent.tab_id || child.tab_generation !== parent.tab_generation || child.profile_id !== parent.profile_id || child.provider_id !== parent.provider_id || child.provider_epoch !== parent.provider_epoch || !same(child.preview, parent.preview) || !same(child.rights, parent.rights)) fail('browser_busy', 'Transfer is stale or busy');
          browserID(child.attachment_id); browserID(child.attachment_generation);
          return { attachment, child: clone(child) };
        });
        if (new Set(moves.map(move => move.attachment.scope.tab_id)).size !== moves.length) fail('permission_denied', 'Duplicate transfer target');
        for (const move of moves) { move.attachment.scope = move.child; move.attachment.agentId = args.child_agent_id as string; move.attachment.sequence = 0n; }
        result.result = moves.map(move => this.metadata(move.attachment));
      } else {
        const attachment = this.attached(command, selection);
        if (command.expected_document && command.expected_document !== String(this.manager.controlledState(attachment.target).documentGeneration)) fail('stale_document', 'The page document changed');
        switch (command.kind) {
          case 'detach': result.result = this.metadata(attachment); this.revoke(attachment, 'Detached', false); break;
          case 'begin': {
            if (attachment.operation) fail('browser_busy', 'Browser run is already active');
            const expected = command.expected_document || (command.arguments as { expected_document?: string })?.expected_document;
            if (expected && expected !== String(this.manager.controlledState(attachment.target).documentGeneration)) fail('stale_document', 'The browser document changed');
            const operation = { id: command.operation_id, lifetime: new AbortController() }; attachment.operation = operation;
            try {
              const contents = await this.manager.controlledContents(attachment.target); this.assertAttachment(attachment); signal.throwIfAborted();
              if (!attachment.debugger) attachment.debugger = new ScopedBrowserDebugger(contents, attachment.scope.tab_id, {
                assertLive: () => this.assertAttachment(attachment),
                event: event => this.event(attachment, 'cdp', event.method, event.params, attachment.operation?.id),
                revoked: reason => this.revoke(attachment, reason),
              });
              result.result = this.metadata(attachment);
            } catch (error) { attachment.operation = undefined; operation.lifetime.abort(); throw error; }
            break;
          }
          case 'end':
            if (attachment.operation?.id !== command.operation_id) fail('attachment_revoked', 'Browser operation is no longer current');
            attachment.operation.lifetime.abort(); attachment.operation = undefined; result.result = this.metadata(attachment); break;
          case 'cdp': {
            if (!attachment.operation || attachment.operation.id !== command.operation_id || attachment.operation.lifetime.signal.aborted || !attachment.debugger) fail('attachment_revoked', 'No active native browser operation');
            const args = object(command.arguments, ['method', 'params']);
            if (typeof args.method !== 'string' || /^(Target|Browser)\./.test(args.method)) fail('unsupported_operation', 'Only selected-page CDP is accepted');
            const operation = attachment.operation;
            const navigating = ['Page.navigate', 'Page.reload', 'Page.navigateToHistoryEntry'].includes(args.method);
            if (navigating && operation.navigating) fail('browser_busy', 'A page navigation is already in flight');
            if (navigating) operation.navigating = true;
            let answer: unknown;
            try { answer = await attachment.debugger.dispatch({ method: args.method, params: args.params ?? {}, sessionId: attachment.debugger.sessionId }, AbortSignal.any([signal, operation.lifetime.signal])); }
            finally { if (navigating) operation.navigating = false; }
            if (args.method === 'Page.captureScreenshot') {
              const data = (answer as { data?: unknown }).data;
              if (typeof data !== 'string') fail('unsupported_operation', 'Invalid screenshot');
              const image = Buffer.from(data, 'base64');
              if (image.length > 8 * 1024 * 1024) fail('unsupported_operation', 'Screenshot exceeds its limit');
              result.screenshotBytes = new Uint8Array(image); result.result = {};
            } else result.result = answer;
            break;
          }
          case 'allow_preview_port': {
            const args = object(command.arguments, ['url', 'preview_host_id', 'tab_id', 'attachment_id', 'code', 'expected_document', 'timeout', 'port']);
            if (!command.scope.preview || !command.scope.rights.includes('route') || !Number.isInteger(args.port) || (args.port as number) < 1 || (args.port as number) > 65535 || !this.options.expand) fail('permission_denied', 'Preview expansion is unavailable');
            if (attachment.operation) fail('browser_busy', 'End the active browser run before changing its preview scope');
            const environmentId = command.scope.preview.environment_id;
            if (this.expanding.has(environmentId)) fail('browser_busy', 'Preview scope is already changing');
            this.expanding.set(environmentId, attachment);
            try { await this.options.expand(selection.input, attachment.scope, args.port as number); this.assertAttachment(attachment); } finally { this.expanding.delete(environmentId); }
            attachment.scope.preview!.ports = [...new Set([...(attachment.scope.preview!.ports ?? []), args.port as number])].sort((a, b) => a - b);
            result.result = this.metadata(attachment); break;
          }
          default: fail('unsupported_operation', 'Unknown native browser operation');
        }
      }
      signal.throwIfAborted(); this.current(command);
      const attachment = this.attachments.get(command.scope.tab_id);
      if (attachment) result.document_revision = String(this.manager.controlledState(attachment.target).documentGeneration);
      else if (result.result && typeof result.result === 'object' && 'document_revision' in result.result) result.document_revision = String(result.result.document_revision);
    } catch (error) {
      if (acquired) this.revoke(acquired, 'Admission result could not be delivered');
      result.error = { kind: error instanceof ControlError ? error.kind : (signal?.aborted || error instanceof Error && error.message === 'outcome_unknown') ? 'outcome_unknown' : started ? 'attachment_revoked' : 'unsupported_operation',
        message: error instanceof ControlError ? error.message : 'The native browser operation could not complete safely' };
      delete result.result; delete result.screenshotBytes;
    } finally { clearTimeout(timer); if (started) this.pending.delete(command.command_id); if (reserved) this.reservations.delete(command.scope.tab_id); }
    return result;
  }
  dispose(): void { for (const [rootId, selection] of [...this.selections]) this.release({ rootId, providerEpoch: selection.input.provider.provider_epoch }); }
}
