import type {
  BoundedTranscriptPage, LifecycleEvent, RootCollectionPage, RootSnapshot,
  SessionCatalogPage, StreamEvent,
} from '@whip/protocol';
import type { SdkEvent, WhipClient } from './client.js';
import type { Session } from './session.js';
import { asError, WhipError } from './errors.js';
import { boundExecutionEvidence, emptyExecutionEvidence, observeExecution, reconcileExecutions, seedExecutions, settleExecutions, type ExecutionEvidence } from './executions.js';
export { executionRows, type ExecutionCell, type ExecutionHostCall, type ExecutionRestart, type ExecutionRow } from './executions.js';

export type DeepReadonly<T> = T extends (...args: never[]) => unknown ? T
  : T extends object ? { readonly [K in keyof T]: DeepReadonly<T[K]> } : T;
type Message = NonNullable<BoundedTranscriptPage['messages']>[number];
type Presentation = NonNullable<RootSnapshot['presentation']>[number];
type Status = 'idle' | 'loading' | 'live' | 'stale' | 'error' | 'closed';

export interface HistoryView {
  revision: string;
  throughSeq: number;
  nextSeq: number;
  hasMore: boolean;
  loading: boolean;
  messages: Message[];
  truncated: boolean;
  error?: Error;
}

export interface SessionViewSnapshot {
  status: Status;
  root?: RootSnapshot;
  history: Record<string, HistoryView>;
  collections: Record<string, RootCollectionPage>;
  retainedBytes: number;
  /** Bounded supplemental REPL evidence; transcript bodies remain in history. */
  executions?: ExecutionEvidence;
  truncated: boolean;
  unavailable: boolean;
  error?: Error;
}

export interface SessionViewOptions {
  maxBytes?: number;
  maxMessages?: number;
  /** Notification batching only; every event is processed in order. */
  notificationIntervalMs?: number;
}

const encoder = new TextEncoder();
const bytes = (value: unknown): number => encoder.encode(JSON.stringify(value)).byteLength;
const errorKind = (error: unknown): unknown => error && typeof error === 'object' && 'kind' in error ? error.kind : undefined;

function freeze<T>(value: T): DeepReadonly<T> {
  if (value && typeof value === 'object' && !Object.isFrozen(value)) {
    for (const child of Object.values(value)) freeze(child);
    Object.freeze(value);
  }
  return value as DeepReadonly<T>;
}

function positive(value: number, name: string): number {
  if (!Number.isSafeInteger(value) || value < 1) throw new RangeError(`${name} must be a positive integer`);
  return value;
}

function terminalChanges(listener: () => void): (outcome: { command_id: string; status: string }) => void {
  const seen = new Set<string>();
  return outcome => {
    if (!['succeeded', 'failed', 'cancelled', 'interrupted'].includes(outcome.status) || seen.has(outcome.command_id)) return;
    seen.add(outcome.command_id);
    if (seen.size > 128) seen.delete(seen.values().next().value!);
    listener();
  };
}

/** One root subscription and a bounded, reconstructible projection of daemon state. */
export class SessionView {
  private current: SessionViewSnapshot = {
    status: 'idle', history: {}, collections: {}, retainedBytes: 0, truncated: false, unavailable: false,
  };
  private published = freeze(this.current);
  private readonly listeners = new Set<() => void>();
  private readonly opened = new Set<string>();
  private readonly lifetime = new AbortController();
  private readonly historyRequests = new Map<string, { epoch: number; promise: Promise<void> }>();
  private stream?: Awaited<ReturnType<WhipClient['events']['subscribe']>>;
  private disconnectListener?: () => void;
  private commandListener?: () => void;
  private noticeTimer?: ReturnType<typeof setTimeout>;
  private refreshTimer?: ReturnType<typeof setTimeout>;
  private refreshPromise?: Promise<void>;
  private refreshAgain = false;
  private epoch = 0;
  private started = false;
  private runtimeID?: string;
  private lastConnectionID?: string;
  private streamConnectionID?: string;
  private presentationGaps = new Set<string>();
  private incompatibleRuntime = false;
  private unknownSeen = false;
  private pendingBytes = 0;
  private readonly maxBytes: number;
  private readonly maxMessages: number;
  private readonly notificationInterval: number;

  constructor(readonly session: Session, options: SessionViewOptions = {}) {
    this.maxBytes = positive(options.maxBytes ?? 8 * 1024 * 1024, 'maxBytes');
    this.maxMessages = positive(options.maxMessages ?? 512, 'maxMessages');
    this.notificationInterval = positive(options.notificationIntervalMs ?? 16, 'notificationIntervalMs');
    this.opened.add(session.rootId);
  }

  getSnapshot = (): DeepReadonly<SessionViewSnapshot> => this.published;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  async start(): Promise<void> {
    if (this.lifetime.signal.aborted) throw new Error('Session view is closed');
    if (this.started) return this.refreshPromise;
    this.started = true;
    this.disconnectListener = this.session.client.subscribe(() => this.connectionChanged());
    this.commandListener = this.session.client.onCommand(terminalChanges(() => this.scheduleRefresh()));
    this.connectionChanged(false);
    await this.refresh();
  }

  async dispose(): Promise<void> {
    if (this.lifetime.signal.aborted) return;
    this.lifetime.abort();
    this.epoch++;
    clearTimeout(this.noticeTimer);
    clearTimeout(this.refreshTimer);
    this.disconnectListener?.();
    this.commandListener?.();
    const stream = this.stream;
    this.stream = undefined;
    this.set({ ...this.current, status: 'closed', executions: undefined }, true);
    this.listeners.clear();
    await stream?.dispose();
  }

  /** Refresh reconciles metadata without erasing observed activity in the same turn. */
  async refresh(): Promise<void> {
    if (!this.started || this.lifetime.signal.aborted || this.incompatibleRuntime) return;
    if (this.session.client.getSnapshot().state !== 'connected') return;
    clearTimeout(this.refreshTimer);
    this.refreshTimer = undefined;
    if (this.refreshPromise) {
      this.refreshAgain = true;
      return this.refreshPromise;
    }
    this.refreshPromise = this.synchronize();
    try { await this.refreshPromise; }
    finally {
      this.refreshPromise = undefined;
      if (this.refreshAgain) {
        this.refreshAgain = false;
        this.scheduleRefresh();
      }
    }
  }

  async openAgent(agentId: string): Promise<void> {
    if (!agentId) throw new Error('Agent ID is required');
    if (this.lifetime.signal.aborted) throw new Error('Session view is closed');
    this.opened.add(agentId);
    try { await this.readHistory(agentId, false); }
    catch (error) {
      if (agentId !== this.session.rootId) this.opened.delete(agentId);
      throw error;
    }
  }

  closeAgent(agentId: string): void {
    if (agentId === this.session.rootId) return;
    this.opened.delete(agentId);
    const history = { ...this.current.history };
    delete history[agentId];
    this.set({ ...this.current, history }, true);
  }

  async loadOlder(agentId = this.session.rootId): Promise<void> {
    if (!this.opened.has(agentId)) return this.openAgent(agentId);
    const history = this.current.history[agentId];
    if (history && !history.hasMore) return;
    await this.readHistory(agentId, !!history?.messages.length);
  }

  async loadCollection(name: string, options: { more?: boolean } = {}): Promise<void> {
    if (this.lifetime.signal.aborted) throw new Error('Session view is closed');
    const epoch = this.epoch;
    const previous = options.more ? this.current.collections[name] : undefined;
    if (previous && !previous.has_more) return;
    if (previous && !previous.next_cursor) throw new WhipError('invalid_response', 'Collection is missing its continuation cursor');
    try {
      const page = await this.session.client.call('root.collection', {
        root_id: this.session.rootId, collection: name,
        ...(previous?.next_cursor ? { cursor: previous.next_cursor } : {}), limit: 128, max_bytes: 256 * 1024,
      }, { signal: this.lifetime.signal });
      if (epoch !== this.epoch || this.lifetime.signal.aborted) return;
      if (previous && previous.revision !== page.revision) throw new WhipError('resynchronization_required', 'Collection revision changed');
      const merged = { ...page, items: [...(previous?.items ?? []), ...(page.items ?? [])] };
      this.set({ ...this.current, collections: { ...this.current.collections, [name]: merged } }, true);
    } catch (error) {
      if (errorKind(error) === 'resynchronization_required') {
        const collections = { ...this.current.collections };
        delete collections[name];
        this.set({ ...this.current, collections }, true);
      }
      throw error;
    }
  }

  private connectionChanged(refresh = true): void {
    const connection = this.session.client.getSnapshot();
    if (connection.state === 'connected') {
      if (this.runtimeID && connection.info?.runtime_id !== this.runtimeID) {
        this.incompatibleRuntime = true;
        this.epoch++;
        void this.stream?.dispose();
        this.stream = undefined;
        this.set({ ...this.current, executions: undefined, status: 'error', error: new WhipError('runtime_changed', 'Execution runtime changed; open a new session view') }, true);
      } else if (connection.info?.connection_id !== this.lastConnectionID) {
        this.lastConnectionID = connection.info?.connection_id;
        if (refresh) void this.refresh();
      }
    } else {
      this.epoch++;
      this.stream = undefined;
      this.set({ ...this.current, executions: this.current.executions && settleExecutions(this.current.executions), status: connection.state === 'incompatible' ? 'error' : this.current.root ? 'stale' : 'loading', error: connection.error }, true);
    }
  }

  private scheduleRefresh(): void {
    if (!this.started || this.lifetime.signal.aborted || this.refreshTimer) return;
    this.refreshTimer = setTimeout(() => {
      this.refreshTimer = undefined;
      void this.refresh();
    }, 100);
  }

  private async synchronize(): Promise<void> {
    const epoch = ++this.epoch;
    const oldStream = this.stream;
    const continuous = !!oldStream && this.current.status === 'live'
      && this.streamConnectionID === this.session.client.getSnapshot().info?.connection_id;
    this.stream = undefined;
    this.set({ ...this.current, status: continuous ? 'live' : this.current.root ? 'stale' : 'loading' }, true);
    // Capture after publishing so reconciliation cannot restore evicted rows.
    const previous = continuous ? this.current.root : undefined;
    try {
      await oldStream?.dispose();
      const snapshot = await this.session.client.call('root.snapshot', { root_id: this.session.rootId }, { signal: this.lifetime.signal });
      if (epoch !== this.epoch || this.lifetime.signal.aborted) return;
      if (snapshot.root_id !== this.session.rootId) throw new Error('Snapshot belongs to a different root');
      if (previous && BigInt(snapshot.cursor) < BigInt(previous.cursor)) {
        throw new WhipError('resynchronization_required', 'Snapshot cursor moved backwards');
      }
      const gaps = new Set<string>();
      const omitted = { ...snapshot.omitted };
      const partial = !!(snapshot.omitted?.presentation || snapshot.omitted?.presentation_prefix);
      const reconcile = (agentId: string, events?: Presentation[] | null): Presentation[] => {
        const sameTurn = previous?.history_revision === snapshot.history_revision
          && !!snapshot.active_turns[agentId] && previous.active_turns[agentId] === snapshot.active_turns[agentId];
        if (sameTurn) {
          for (const key of ['presentation', 'presentation_prefix']) {
            if (previous.omitted?.[key]) omitted[key] = true;
          }
        }
        let rows = sameTurn ? (agentId === snapshot.root_id ? previous.presentation : previous.agent_presentations?.[agentId]) ?? [] : [];
        let cursor = sameTurn ? BigInt(previous.cursor) : 0n;
        let gap = sameTurn && this.presentationGaps.has(agentId);
        for (const event of events ?? []) {
          const seq = BigInt(event.seq);
          // Filter raw events before grouping: a grouped row keeps its FIRST seq.
          if (seq <= cursor) continue;
          gap ||= partial && seq !== cursor + 1n;
          const next = appendPresentation(rows, event, !gap);
          if (next.at(-1) !== rows.at(-1)) gap = false;
          rows = next;
          cursor = seq;
        }
        // Missing suffixes also separate the next live delta from retained text.
        if (rows.length && (gap || (partial && cursor < BigInt(snapshot.cursor)))) gaps.add(agentId);
        return rows;
      };
      const root = {
        ...snapshot,
        omitted,
        presentation: reconcile(snapshot.root_id, snapshot.presentation),
        agent_presentations: Object.fromEntries([...new Set([
          ...Object.keys(previous?.agent_presentations ?? {}), ...Object.keys(snapshot.agent_presentations ?? {}),
        ])].map(id => [id, reconcile(id, snapshot.agent_presentations?.[id])])),
      };
      const stream = await this.session.client.events.subscribe(root.root_id, root.cursor, { signal: this.lifetime.signal });
      if (epoch !== this.epoch || this.lifetime.signal.aborted) { await stream.dispose(); return; }
      this.runtimeID ??= this.session.client.getSnapshot().info?.runtime_id;
      this.stream = stream;
      this.streamConnectionID = this.session.client.getSnapshot().info?.connection_id;
      this.presentationGaps = gaps;
      const revisionChanged = this.current.root?.history_revision !== root.history_revision;
      const history = revisionChanged ? {} : { ...this.current.history };
      const recent = (root.messages ?? []).map((message, index) => ({
        seq: root.message_seqs?.[index] ?? (root.first_message_seq ?? 1) + index, message,
      }));
      const first = recent[0]?.seq ?? 1;
      const previousHistory = history[root.root_id];
      const all = mergeMessages(previousHistory?.messages ?? [], recent);
      const merged = all.slice(-this.maxMessages);
      const nextSeq = merged[0]?.seq ?? first;
      // Snapshot omission describes its suffix, not the merged history. Preserve
      // a known beginning only while that same boundary remains in the cache.
      const hasMore = merged.length
        ? nextSeq > 1 && (previousHistory?.nextSeq !== nextSeq || previousHistory.hasMore)
        : !!root.omitted?.messages;
      history[root.root_id] = {
        revision: root.history_revision, throughSeq: recent.at(-1)?.seq ?? previousHistory?.throughSeq ?? 0,
        nextSeq, hasMore,
        loading: false, messages: merged, truncated: all.length > merged.length || !!root.omitted?.messages,
      };
      this.set({
        status: 'live', root, history, collections: {}, retainedBytes: 0,
        executions: seedExecutions(this.current.executions, snapshot, history),
        truncated: Object.values(root.omitted ?? {}).some(Boolean), unavailable: this.unknownSeen,
      }, true);
      void this.consume(stream, epoch);
      for (const agentId of this.opened) {
        // A deleted/unavailable child does not invalidate the root snapshot.
        if (agentId !== this.session.rootId && epoch === this.epoch) await this.readHistory(agentId, false).catch(() => {});
      }
    } catch (error) {
      if (epoch !== this.epoch || this.lifetime.signal.aborted) return;
      this.set({ ...this.current, executions: this.current.executions && settleExecutions(this.current.executions), status: this.current.root ? 'stale' : 'error', error: asError(error) }, true);
      if (errorKind(error) === 'resynchronization_required') this.scheduleRefresh();
    }
  }

  private async consume(stream: NonNullable<SessionView['stream']>, epoch: number): Promise<void> {
    try {
      for await (const event of stream) {
        if (epoch !== this.epoch || this.stream !== stream || this.lifetime.signal.aborted) return;
        this.apply(event);
      }
    } catch (error) {
      if (epoch !== this.epoch || this.lifetime.signal.aborted) return;
      this.set({ ...this.current, executions: this.current.executions && settleExecutions(this.current.executions), status: 'stale', error: asError(error) }, true);
      // Recovery never keeps applying a stream after a gap, expiry, or overflow.
      this.scheduleRefresh();
    }
  }

  private apply(event: SdkEvent): void {
    const previous = this.current.root;
    if (!previous || event.root_id !== previous.root_id || BigInt(event.seq) <= BigInt(previous.cursor)) return;
    if (BigInt(event.seq) !== BigInt(previous.cursor) + 1n) throw new WhipError('resynchronization_required', 'Event sequence gap; resynchronization required');
    const executions = observeExecution(this.current.executions ?? emptyExecutionEvidence(previous.root_id, previous.history_revision),
      event, previous.active_turns, this.current.history, Date.now());
    let root = { ...previous, cursor: event.seq };
    let unavailable = this.current.unavailable;
    const payload = event.payload as Record<string, unknown>;
    if (event.unknown || (payload?.truncated && payload.content)) {
      unavailable = true;
      if (!this.unknownSeen) { this.unknownSeen = true; this.scheduleRefresh(); }
    }
    if (event.kind === 'stream.accounting') {
      const accounting = (event.payload as StreamEvent).accounting;
      if (accounting && accounting.root_id === root.root_id && accounting.agent_id === root.root_id
        && accounting.scope === 'subtree' && (!root.accounting || BigInt(accounting.revision) >= BigInt(root.accounting.revision))) {
        root.accounting = accounting;
      }
    } else if (event.kind.startsWith('stream.')) {
      const item: Presentation = { seq: event.seq, kind: event.kind, payload: event.payload };
      const agentId = typeof payload?.agent_id === 'string' && payload.agent_id ? payload.agent_id : root.root_id;
      const rows = (agentId === root.root_id ? root.presentation : root.agent_presentations?.[agentId]) ?? [];
      const next = appendPresentation(rows, item, !this.presentationGaps.has(agentId));
      if (next.at(-1) !== rows.at(-1)) this.presentationGaps.delete(agentId);
      if (agentId !== root.root_id) root.agent_presentations = { ...root.agent_presentations, [agentId]: next };
      else root.presentation = next;
    } else if (event.kind.startsWith('session.') && event.kind.endsWith('.updated')) {
      root.meta = {
        ...root.meta,
        ...(typeof payload.title === 'string' ? { title: payload.title } : {}),
        ...(typeof payload.model === 'string' ? { model: payload.model } : {}),
        ...(typeof payload.provider === 'string' ? { provider: payload.provider } : {}),
        ...(payload.effort_changed && typeof payload.effort === 'string' ? { effort: payload.effort } : {}),
        ...(typeof payload.working_directory === 'string' ? { cwd: payload.working_directory } : {}),
      };
      // permission_mode is a top-level RootSnapshot field, not session meta.
      if (typeof payload.permission_mode === 'string') root = { ...root, permission_mode: payload.permission_mode };
    } else {
      const lifecycle = payload as LifecycleEvent;
      if (/^(agent\.)?turn\./.test(event.kind)) {
        const agentId = lifecycle.agent_id || root.root_id;
        root.active_turns = { ...root.active_turns };
        if (event.kind.endsWith('.started') && lifecycle.turn_id) {
          this.presentationGaps.delete(agentId);
          root.active_turns[agentId] = lifecycle.turn_id;
          if (agentId === root.root_id) root.presentation = [];
          else root.agent_presentations = { ...root.agent_presentations, [agentId]: [] };
        } else if (root.active_turns[agentId] === lifecycle.turn_id) delete root.active_turns[agentId];
      }
      if (event.kind === 'question.pending') {
        root.questions = [...(root.questions ?? []).filter(item => item.question_id !== lifecycle.question_id), lifecycle];
      } else if (event.kind === 'question.answered' || event.kind === 'question.closed') {
        root.questions = (root.questions ?? []).filter(item => item.question_id !== lifecycle.question_id);
      }
      // Lifecycle events intentionally carry partial records. A coalesced snapshot
      // refreshes collections and committed history instead of inventing missing fields.
      if (!event.unknown) this.scheduleRefresh();
    }
    // Hidden tabs can throttle notification timers indefinitely. Bound incoming
    // growth independently, without reserializing the cached history per delta.
    this.pendingBytes += bytes(event);
    this.set({ ...this.current, root, executions, unavailable }, this.published.retainedBytes + this.pendingBytes > this.maxBytes);
  }

  private async readHistory(agentId: string, older: boolean): Promise<void> {
    const existing = this.historyRequests.get(agentId);
    if (existing) {
      if (existing.epoch === this.epoch) return existing.promise;
      await existing.promise.catch(() => {});
      if (this.historyRequests.get(agentId) === existing) this.historyRequests.delete(agentId);
      if (this.lifetime.signal.aborted || !this.opened.has(agentId)) return;
      return this.readHistory(agentId, older);
    }
    const request = { epoch: this.epoch, promise: this.fetchHistory(agentId, older) };
    this.historyRequests.set(agentId, request);
    try { await request.promise; }
    finally { if (this.historyRequests.get(agentId) === request) this.historyRequests.delete(agentId); }
  }

  private async fetchHistory(agentId: string, older: boolean): Promise<void> {
    const epoch = this.epoch;
    const prior = this.current.history[agentId];
    const placeholder: HistoryView = prior ?? {
      revision: this.current.root?.history_revision ?? '', throughSeq: 0, nextSeq: 0,
      hasMore: true, messages: [], truncated: false, loading: true,
    };
    this.set({ ...this.current, history: { ...this.current.history, [agentId]: { ...placeholder, loading: true, error: undefined } } }, true);
    try {
      const page = await this.session.client.call('history.page', {
        root_id: this.session.rootId, agent_id: agentId, recent: true,
        through_seq: older && prior ? prior.throughSeq : -1,
        ...(older && prior ? { before_seq: prior.nextSeq, revision: prior.revision } : {}),
        limit: 128, max_bytes: 256 * 1024,
      }, { signal: this.lifetime.signal });
      if (epoch !== this.epoch || !this.opened.has(agentId) || this.lifetime.signal.aborted) return;
      if (older && prior && page.has_more && page.next_seq >= prior.nextSeq && prior.nextSeq > 0) {
        throw new WhipError('invalid_response', 'History cursor did not advance');
      }
      if (this.current.root && page.history_revision !== this.current.root.history_revision) {
        this.scheduleRefresh();
        return;
      }
      const sameRevision = prior?.revision === page.history_revision;
      const all = mergeMessages(sameRevision ? prior?.messages ?? [] : [], page.messages ?? []);
      // Explicit backward paging preserves the page just requested. Advancing the
      // cursor while retaining only newer entries would make old history unreachable.
      const messages = older ? all.slice(0, this.maxMessages) : all.slice(-this.maxMessages);
      const nextSeq = messages[0]?.seq ?? page.next_seq;
      // A recent suffix cannot reset an older retained boundary; trimming the
      // incoming page's beginning, however, makes those messages pageable again.
      const hasMore = sameRevision && prior.nextSeq === nextSeq && nextSeq < page.next_seq
        ? prior.hasMore : nextSeq > page.next_seq || page.has_more;
      const history: HistoryView = {
        revision: page.history_revision, throughSeq: page.through_seq,
        nextSeq, hasMore,
        loading: false, messages, truncated: all.length > messages.length || messages.some(item => !!item.body),
      };
      const histories = { ...this.current.history, [agentId]: history };
      this.set({ ...this.current, history: histories, executions: this.current.executions && reconcileExecutions(this.current.executions, histories) }, true);
    } catch (error) {
      if (epoch !== this.epoch || this.lifetime.signal.aborted || !this.opened.has(agentId)) return;
      const history = { ...this.current.history };
      if (errorKind(error) === 'resynchronization_required') {
        delete history[agentId];
        this.scheduleRefresh();
      } else history[agentId] = { ...placeholder, loading: false, error: asError(error) };
      this.set({ ...this.current, history, error: asError(error) }, true);
      throw error;
    }
  }

  private set(next: SessionViewSnapshot, immediate = false): void {
    this.current = next;
    if (immediate) { clearTimeout(this.noticeTimer); this.noticeTimer = undefined; this.publish(); }
    else if (!this.noticeTimer) this.noticeTimer = setTimeout(() => { this.noticeTimer = undefined; this.publish(); }, this.notificationInterval);
  }

  private publish(): void {
    const before = this.current;
    this.current = bound(before, this.maxBytes, this.maxMessages);
    for (const id of Object.keys(before.history)) {
      if (id !== this.session.rootId && !this.current.history[id]) this.opened.delete(id);
    }
    this.published = freeze(this.current);
    this.pendingBytes = 0;
    for (const listener of this.listeners) listener();
  }
}

function mergeMessages(left: Message[], right: Message[]): Message[] {
  return [...new Map([...left, ...right].map(message => [message.seq, message])).values()].sort((a, b) => a.seq - b.seq);
}

function appendPresentation(previous: Presentation[], item: Presentation, continuous = true): Presentation[] {
  if (item.kind === 'stream.accounting') return previous;
  const payload = (item.payload ?? {}) as StreamEvent;
  const cumulative = ['stream.tool.call', 'stream.tool.output'].includes(item.kind);
  const index = cumulative && payload.id ? previous.findIndex(row => {
    const value = (row.payload ?? {}) as StreamEvent;
    return row.kind === item.kind && value.id === payload.id && value.agent_id === payload.agent_id;
  }) : previous.length - 1;
  const last = previous[index];
  const lastPayload = (last?.payload ?? {}) as StreamEvent;
  if (last?.kind === item.kind && payload.id === lastPayload.id && payload.agent_id === lastPayload.agent_id) {
    let next: Presentation | undefined;
    // Tool arguments/output are full values so far, even when calls interleave.
    if (cumulative && payload.id) next = { ...item, seq: last.seq };
    else if (continuous && ['stream.text', 'stream.reasoning', 'stream.terminal.output'].includes(item.kind)
      && typeof payload.text === 'string' && typeof lastPayload.text === 'string') {
      next = { ...last, payload: { ...payload, text: lastPayload.text + payload.text } };
    }
    // Keep the first event's sequence as the row key; root.cursor tracks progress.
    if (next) return [...previous.slice(0, index), next, ...previous.slice(index + 1)];
  }
  return [...previous, item];
}

function bound(state: SessionViewSnapshot, maxBytes: number, maxMessages: number): SessionViewSnapshot {
  let next = { ...state, history: { ...state.history }, collections: { ...state.collections } };
  for (const [id, history] of Object.entries(next.history)) {
    if (history.messages.length > maxMessages) {
      const messages = history.messages.slice(-maxMessages);
      next.history[id] = { ...history, messages, nextSeq: messages[0]!.seq, hasMore: true, truncated: true };
      next.truncated = true;
    }
  }
  const size = (): number => bytes({ root: next.root, history: next.history, collections: next.collections, executions: next.executions });
  if (next.executions?.truncated) next.truncated = true;
  let total = size();
  if (total > maxBytes) {
    next.truncated = true;
    next.collections = {};
    if (next.executions) {
      const otherBytes = bytes({ root: next.root, history: next.history, collections: next.collections });
      next.executions = boundExecutionEvidence(next.executions, Math.max(0, maxBytes - otherBytes - 32));
    }
    total = size();
    const children = Object.keys(next.history).filter(id => id !== next.root?.root_id)
      .sort((a, b) => Number(!!next.root?.active_turns[a]) - Number(!!next.root?.active_turns[b]));
    for (const id of children) {
      if (total <= maxBytes) break;
      delete next.history[id];
      total = size();
    }
    const rootHistory = next.root && next.history[next.root.root_id];
    if (total > maxBytes && next.root && rootHistory) {
      next.history[next.root.root_id] = { ...rootHistory, messages: [], truncated: true, hasMore: true, nextSeq: 0 };
      total = size();
    }
  }
  if (total > maxBytes && next.root) {
    next.root = { ...next.root, messages: [], message_seqs: [], presentation: [], agent_presentations: {}, omitted: { ...next.root.omitted, messages: true, presentation: true } };
    next.unavailable = true;
    total = size();
  }
  if (total > maxBytes) {
    // A configured budget may be smaller than a single metadata record. Expose
    // that limitation explicitly rather than violating the bound indefinitely.
    next.root = undefined;
    next.executions = undefined;
    next.history = {};
    next.collections = {};
    next.unavailable = true;
    next.error = new Error('Session metadata exceeds the view byte budget');
    total = size();
  }
  return { ...next, retainedBytes: total };
}

export function createSessionView(session: Session, options?: SessionViewOptions): SessionView {
  return new SessionView(session, options);
}

export interface SessionListSnapshot {
  status: Status;
  page?: SessionCatalogPage;
  error?: Error;
  truncated: boolean;
}

/** Catalog polling is active only while observed; it never opens root subscriptions. */
export class SessionListView {
  private current: SessionListSnapshot = Object.freeze({ status: 'idle', truncated: false });
  private readonly listeners = new Set<() => void>();
  private readonly lifetime = new AbortController();
  private timer?: ReturnType<typeof setTimeout>;
  private stopConnection?: () => void;
  private stopCommands?: () => void;
  private pending?: Promise<void>;
  private started = false;
  private epoch = 0;
  private refreshAgain = false;
  private runtimeID?: string;
  private readonly interval: number;
  private readonly maxBytes: number;

  constructor(readonly client: WhipClient, options: { pollIntervalMs?: number; maxBytes?: number } = {}) {
    this.interval = positive(options.pollIntervalMs ?? 2000, 'pollIntervalMs');
    this.maxBytes = positive(options.maxBytes ?? 2 * 1024 * 1024, 'maxBytes');
  }

  getSnapshot = (): DeepReadonly<SessionListSnapshot> => this.current;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    if (this.listeners.size === 1 && this.started) { void this.refresh(); this.pollLater(); }
    return () => {
      this.listeners.delete(listener);
      if (!this.listeners.size) { clearTimeout(this.timer); this.timer = undefined; }
    };
  };

  async start(): Promise<void> {
    if (this.lifetime.signal.aborted) throw new Error('Session list view is closed');
    if (this.started) return this.pending;
    this.started = true;
    this.stopConnection = this.client.subscribe(() => {
      this.epoch++;
      if (this.client.getSnapshot().state === 'connected') {
        if (this.runtimeID && this.runtimeID !== this.client.getSnapshot().info?.runtime_id) {
          this.set({ status: 'loading', truncated: false });
        }
        void this.refresh();
        this.pollLater();
      } else {
        clearTimeout(this.timer);
        this.timer = undefined;
        this.set({ ...this.current, status: 'stale' });
      }
    });
    this.stopCommands = this.client.onCommand(terminalChanges(() => {
      if (this.listeners.size) void this.refresh();
    }));
    await this.refresh();
    this.pollLater();
  }

  async refresh(): Promise<void> {
    if (!this.started || this.lifetime.signal.aborted || this.client.getSnapshot().state !== 'connected') return;
    if (this.pending) { this.refreshAgain = true; return this.pending; }
    this.pending = this.fetch(false);
    try { await this.pending; } finally {
      this.pending = undefined;
      if (this.refreshAgain) { this.refreshAgain = false; void this.refresh(); }
    }
  }

  async loadMore(): Promise<void> {
    if (this.pending) return this.pending;
    if (!this.current.page?.has_more) return;
    if (!this.current.page.next_cursor) throw new WhipError('invalid_response', 'Session list is missing its continuation cursor');
    this.pending = this.fetch(true);
    try { await this.pending; } finally { this.pending = undefined; }
  }

  async dispose(): Promise<void> {
    if (this.lifetime.signal.aborted) return;
    this.lifetime.abort();
    this.epoch++;
    clearTimeout(this.timer);
    this.stopConnection?.();
    this.stopCommands?.();
    this.set({ ...this.current, status: 'closed' });
    this.listeners.clear();
  }

  private async fetch(more: boolean): Promise<void> {
    const epoch = this.epoch;
    const previous = this.current.page;
    this.set({ ...this.current, status: previous ? 'stale' : 'loading' });
    try {
      if (previous && !more) {
        const revision = await this.client.call('sessions.revision', {}, { signal: this.lifetime.signal });
        if (epoch !== this.epoch || this.lifetime.signal.aborted) return;
        if (revision.revision === previous.revision) { this.set({ ...this.current, status: 'live', error: undefined }); return; }
      }
      const page = await this.client.call('sessions.list', {
        ...(more && previous?.next_cursor ? { cursor: previous.next_cursor } : {}), limit: 128, max_bytes: 256 * 1024,
      }, { signal: this.lifetime.signal });
      if (epoch !== this.epoch || this.lifetime.signal.aborted) return;
      if (more && previous && previous.revision !== page.revision) throw new WhipError('resynchronization_required', 'Session list revision changed');
      this.runtimeID = this.client.getSnapshot().info?.runtime_id;
      const merged = { ...page, items: more ? [...(previous?.items ?? []), ...(page.items ?? [])] : page.items };
      if (bytes(merged) > this.maxBytes) {
        this.set({ ...this.current, status: 'live', truncated: true, error: new Error('Session list cache is full; refresh to start a new page window') });
      } else this.set({ status: 'live', page: merged, truncated: false });
    } catch (error) {
      if (epoch !== this.epoch || this.lifetime.signal.aborted) return;
      this.set({ ...this.current, status: previous ? 'stale' : 'error', error: asError(error) });
      if (more && errorKind(error) === 'resynchronization_required') {
        this.set({ status: 'stale', truncated: false });
        await this.fetch(false);
      }
    }
  }

  private pollLater(): void {
    if (this.lifetime.signal.aborted || !this.started || !this.listeners.size || this.timer || this.client.getSnapshot().state !== 'connected') return;
    this.timer = setTimeout(async () => {
      this.timer = undefined;
      await this.refresh();
      this.pollLater();
    }, this.interval);
  }

  private set(value: SessionListSnapshot): void {
    this.current = freeze(value) as SessionListSnapshot;
    for (const listener of this.listeners) listener();
  }
}

export function createSessionListView(client: WhipClient, options?: { pollIntervalMs?: number; maxBytes?: number }): SessionListView {
  return new SessionListView(client, options);
}
