import type { HistorySnapshot, Message, Operations } from '@whip/protocol';
import type { Client } from './index.js';
import type { Session } from './session.js';
import { RemoteError } from './wire.js';
import { boundedInteger, bytes, freeze } from './value.js';
import type { DeepReadonly } from './value.js';
export type { DeepReadonly } from './value.js';
type MessagePreview = NonNullable<Operations['sessions.observe']['result']['preview']>;
type TreeSummary = Operations['trees.list']['result']['items'][number];

export type ObservationStatus = 'idle' | 'loading' | 'live' | 'stale' | 'suspended' | 'closed';
export interface ObservationError { message: string; kind?: string }
const describe = (error: unknown): ObservationError => ({ message: (error instanceof Error ? error.message : String(error)).slice(0, 256), ...(error instanceof RemoteError ? { kind: error.kind.slice(0, 64) } : {}) });
function measure(value: { retainedBytes: number }) {
  value.retainedBytes = 0;
  let measured = bytes(value);
  while (value.retainedBytes !== measured) { value.retainedBytes = measured; measured = bytes(value); }
}
const isConflict = (error: unknown) => error instanceof RemoteError && error.kind === 'CONFLICT';

/** A gap is explicit omitted evidence, never inferred from sequence arithmetic:
 * retired records may leave perfectly valid gaps between adjacent messages. */
export interface HistoryGap { messageID: string; sequence: string; reason: 'message_too_large'; bytes: number }
type Row = { message: Message; gap?: never } | { gap: HistoryGap; message?: never };
const sequence = (row: Row) => row.message ? row.message.sequence : row.gap!.sequence;
export interface HistoryView {
  snapshot: HistorySnapshot | null;
  messages: Message[];
  gaps: HistoryGap[];
  olderCursor: string | null;
  latestMissing: boolean;
}
export interface SessionViewSnapshot {
  status: ObservationStatus;
  runtimeID: string;
  sessionID: string;
  epoch: string | null;
  history: HistoryView;
  preview: MessagePreview | null;
  previewUnavailable: boolean;
  retainedBytes: number;
  truncated: boolean;
  unavailable: boolean;
  error?: ObservationError;
}
export interface SessionViewOptions { maxMessages?: number; maxBytes?: number; pageSize?: number; pollIntervalMs?: number }

/** One reconstructible transcript window. The view owns observation only;
 * draft text, selection and reading anchors stay with the application. */
export class SessionView {
  private current: DeepReadonly<SessionViewSnapshot>;
  private readonly listeners = new Set<() => void>();
  private rows: Row[] = [];
  private session: Session;
  private controller = new AbortController();
  private generation = 0;
  private active = false;
  private closed = false;
  private timer?: ReturnType<typeof setTimeout>;
  private pending?: Promise<void>;
  private navigation?: { kind: 'older' | 'latest'; promise: Promise<void> };
  private after = '0';
  private followLatest = true;
  private readonly maxMessages: number;
  private readonly maxBytes: number;
  private readonly pageSize: number;
  private readonly interval: number;

  constructor(session: Session, options: SessionViewOptions = {}) {
    this.session = session;
    this.maxMessages = boundedInteger(options.maxMessages ?? 512, 'maxMessages', 4096);
    this.maxBytes = boundedInteger(options.maxBytes ?? 8 << 20, 'maxBytes', 64 << 20);
    if (this.maxBytes < 4096) throw new RangeError('maxBytes must be at least 4096');
    this.pageSize = boundedInteger(options.pageSize ?? 100, 'pageSize', 100);
    this.interval = boundedInteger(options.pollIntervalMs ?? 250, 'pollIntervalMs', 60_000);
    this.current = freeze({ status: 'idle', runtimeID: session.client.runtimeID, sessionID: session.id, epoch: null, history: { snapshot: null, messages: [], gaps: [], olderCursor: null, latestMissing: false }, preview: null, previewUnavailable: false, retainedBytes: 0, truncated: false, unavailable: false });
  }
  getSnapshot = (): DeepReadonly<SessionViewSnapshot> => this.current;
  subscribe = (listener: () => void): (() => void) => {
    if (this.closed) throw new Error('Session view is closed');
    if (this.listeners.size >= 64 && !this.listeners.has(listener)) throw new RangeError('Session view listener limit exceeded');
    this.listeners.add(listener); return () => { this.listeners.delete(listener); };
  };
  private publish(value: SessionViewSnapshot) {
    measure(value);
    this.current = freeze(value);
    for (const listener of this.listeners) listener();
  }
  private patch(value: Partial<SessionViewSnapshot>) { this.publish({ ...this.current, ...value } as SessionViewSnapshot); }
  private schedule() {
    if (!this.active || this.closed || this.timer) return;
    this.timer = setTimeout(() => { this.timer = undefined; void this.refresh(); }, this.interval);
  }
  async start(): Promise<void> {
    if (this.closed) throw new Error('Session view is closed');
    this.active = true;
    if (this.controller.signal.aborted) this.controller = new AbortController();
    await this.refresh();
  }
  async suspend(): Promise<void> {
    this.active = false; this.generation++; clearTimeout(this.timer); this.timer = undefined; this.controller.abort();
    if (!this.closed) this.patch({ status: 'suspended', preview: null, previewUnavailable: false });
    await this.pending?.catch(() => {});
    await this.navigation?.promise.catch(() => {});
  }
  resume(): Promise<void> { return this.start(); }
  async reconnect(client: Client): Promise<void> {
    if (client.runtimeID !== this.current.runtimeID) throw new TypeError('Session view belongs to another runtime');
    await this.suspend(); this.session = client.session(this.current.sessionID); await this.start();
  }
  async dispose(): Promise<void> {
    if (this.closed) return;
    await this.suspend(); this.closed = true; this.rows = [];
    this.patch({ status: 'closed', preview: null, history: { snapshot: null, messages: [], gaps: [], olderCursor: null, latestMissing: false } });
    this.listeners.clear();
  }
  refresh(): Promise<void> {
    if (this.closed) return Promise.reject(new Error('Session view is closed'));
    if (!this.active) return Promise.resolve();
    if (this.pending) return this.pending;
    const generation = this.generation;
    this.pending = Promise.resolve().then(() => this.read(generation)).catch(error => {
      if (generation !== this.generation) return;
      this.patch({ status: 'stale', preview: null, previewUnavailable: false, error: describe(error), unavailable: error instanceof TypeError || error instanceof RemoteError && error.kind === 'NOT_FOUND' });
    }).finally(() => { this.pending = undefined; this.schedule(); });
    return this.pending;
  }
  private valid(generation: number) { return generation === this.generation && this.active && !this.closed; }
  private validateMessages(snapshot: HistorySnapshot, messages: readonly Message[], after?: string, before?: string) {
    if (snapshot.session_id !== this.session.id) throw new TypeError('History snapshot belongs to another session');
    let previous = after ?? '0'; const ids = new Set<string>();
    for (const message of messages) {
      if (message.session_id !== this.session.id || message.retired_by !== null || message.retired_revision !== null || ids.has(message.id) || BigInt(message.sequence) <= BigInt(previous) || BigInt(message.sequence) > BigInt(snapshot.through_sequence) || before !== undefined && BigInt(message.sequence) >= BigInt(before)) throw new TypeError('Invalid canonical history page identity or ordering');
      previous = message.sequence; ids.add(message.id);
    }
  }
  private async page(cursor?: string, expectedRevision?: string) {
    const result = await this.session.client.call('sessions.history_page', { session_id: this.session.id, direction: 'backward', limit: this.pageSize, ...(cursor === undefined ? {} : { cursor }), ...(expectedRevision === undefined ? {} : { expected_revision: expectedRevision }) }, { signal: this.controller.signal });
    const messages = result.messages ?? [];
    this.validateMessages(result.snapshot, messages, undefined, cursor);
    if (expectedRevision !== undefined && result.snapshot.revision !== expectedRevision || result.next_cursor !== null && (messages.length === 0 || result.next_cursor !== messages[0]!.sequence)) throw new TypeError('History page revision or continuation mismatch');
    return { ...result, messages };
  }
  private convert(messages: readonly Message[]): Row[] {
    return messages.map(message => {
      const size = bytes(message);
      return size > this.maxBytes - 2048 ? { gap: { messageID: message.id, sequence: message.sequence, reason: 'message_too_large' as const, bytes: size } } : { message };
    });
  }
  private window(snapshot: HistorySnapshot, rows: Row[], olderCursor: string | null, preview: MessagePreview | null, epoch: string | null, keep: 'older' | 'latest') {
    let latestMissing = keep === 'older' ? this.current.history.latestMissing : BigInt(this.after) < BigInt(snapshot.through_sequence);
    let previewUnavailable = false;
    if (preview && bytes(preview) > Math.floor(this.maxBytes / 4)) { preview = null; previewUnavailable = true; }
    const make = (): SessionViewSnapshot => ({ status: 'live', runtimeID: this.current.runtimeID, sessionID: this.session.id, epoch, history: { snapshot, messages: rows.flatMap(row => row.message ? [row.message] : []), gaps: rows.flatMap(row => row.gap ? [row.gap] : []), olderCursor, latestMissing }, preview, previewUnavailable, retainedBytes: 0, truncated: olderCursor !== null || latestMissing || rows.some(row => !!row.gap) || previewUnavailable, unavailable: false });
    while (rows.length > this.maxMessages || bytes(make()) > this.maxBytes - 2048) {
      if (rows.length === 0) throw new RangeError('Session metadata exceeds view byte limit');
      if (rows.length === 1 && rows[0]!.message) {
        const message = rows[0]!.message!;
        rows = [{ gap: { messageID: message.id, sequence: message.sequence, reason: 'message_too_large', bytes: bytes(message) } }];
        continue;
      }
      if (keep === 'latest') { rows.shift(); olderCursor = rows.length ? sequence(rows[0]!) : snapshot.through_sequence; }
      else { rows.pop(); latestMissing = true; }
    }
    this.rows = rows;
    const committed = new Set(rows.flatMap(row => row.message ? [row.message.id] : [row.gap.messageID]));
    if (preview && committed.has(preview.message_id)) preview = null;
    this.publish(make());
  }
  private async seed(generation: number): Promise<void> {
    const page = await this.page();
    if (!this.valid(generation)) return;
    this.followLatest = true; this.after = page.snapshot.through_sequence;
    this.window(page.snapshot, this.convert(page.messages), page.next_cursor, null, this.current.epoch, 'latest');
  }
  private async read(generation: number): Promise<void> {
    if (!this.current.history.snapshot) { this.patch({ status: 'loading', error: undefined }); await this.seed(generation); }
    if (!this.valid(generation)) return;
    const revision = this.current.history.snapshot!.revision;
    try {
      const observation = await this.session.client.call('sessions.observe', { session_id: this.session.id, after: this.after, expected_revision: revision, limit: this.pageSize }, { signal: this.controller.signal });
      if (!this.valid(generation)) return;
      if (observation.snapshot.revision !== revision || BigInt(observation.snapshot.through_sequence) < BigInt(this.after)) throw new TypeError('Observation history revision or boundary mismatch');
      this.validateMessages(observation.snapshot, observation.messages ?? [], this.after);
      const messages = observation.messages ?? [];
      const last = messages.at(-1);
      if (last) this.after = last.sequence;
      if (this.followLatest) {
        this.window(observation.snapshot, [...this.rows, ...this.convert(messages)], this.current.history.olderCursor, observation.preview, observation.epoch, 'latest');
      } else {
        if (messages.length) this.patch({ history: { ...this.current.history, latestMissing: true } as HistoryView });
        this.after = observation.snapshot.through_sequence; // Deliberately do not retain an off-screen suffix.
        this.window(observation.snapshot, this.rows, this.current.history.olderCursor, observation.preview, observation.epoch, 'older');
      }
    } catch (error) {
      if (!isConflict(error) || !this.valid(generation)) throw error;
      // Rewind replaces the window using a fresh tail page. Never replay old
      // pages from zero or join two revisions into one presentation.
      this.rows = []; this.after = '0';
      this.patch({ status: 'loading', preview: null, history: { snapshot: null, messages: [], gaps: [], olderCursor: null, latestMissing: false } });
      await this.seed(generation);
    }
  }
  private navigate(kind: 'older' | 'latest'): Promise<void> {
    if (this.closed || !this.active) return Promise.reject(new Error('Session view is not observing'));
    if (this.navigation) return this.navigation.kind === kind ? this.navigation.promise : Promise.reject(new Error('Session navigation is busy'));
    const generation = this.generation;
    const promise = (async () => {
      await this.pending;
      if (!this.valid(generation)) return;
      clearTimeout(this.timer); this.timer = undefined;
      this.pending = (async () => {
        if (kind === 'latest') { await this.seed(generation); return; }
        const current = this.current.history;
        if (current.olderCursor === null || current.snapshot === null) return;
        const page = await this.page(current.olderCursor, current.snapshot.revision);
        if (!this.valid(generation)) return;
        this.followLatest = false;
        this.window(page.snapshot, [...this.convert(page.messages), ...this.rows], page.next_cursor, this.current.preview as MessagePreview | null, this.current.epoch, 'older');
      })().catch(error => {
        if (!this.valid(generation)) return;
        if (isConflict(error)) { this.rows = []; this.patch({ history: { snapshot: null, messages: [], gaps: [], olderCursor: null, latestMissing: false }, preview: null }); }
        this.patch({ status: 'stale', error: describe(error) });
        throw error;
      });
      try { await this.pending; } finally { this.pending = undefined; this.schedule(); }
    })().finally(() => { this.navigation = undefined; });
    this.navigation = { kind, promise };
    return promise;
  }
  loadOlder(): Promise<void> { return this.navigate('older'); }
  latest(): Promise<void> { return this.navigate('latest'); }
}

export const createSessionView = (session: Session, options?: SessionViewOptions) => new SessionView(session, options);

export interface TreeCatalogSnapshot {
  status: ObservationStatus;
  runtimeID: string;
  revision: string | null;
  items: TreeSummary[];
  nextCursor: string | null;
  windowAfter: string | null;
  retainedBytes: number;
  truncated: boolean;
  error?: ObservationError;
}
export interface TreeCatalogOptions {
  archived?: boolean; pinned?: boolean; maxItems?: number; maxBytes?: number; pageSize?: number; pollIntervalMs?: number;
}

/** A bounded metadata window plus a global invalidation head. Polling does not
 * open conversations, hydrate transcript bodies, or create session views. */
export class TreeCatalogView {
  private current: DeepReadonly<TreeCatalogSnapshot>;
  private readonly listeners = new Set<() => void>();
  private controller = new AbortController();
  private generation = 0;
  private active = false;
  private closed = false;
  private timer?: ReturnType<typeof setTimeout>;
  private pending?: Promise<void>;
  private navigation?: { kind: 'more' | 'next'; promise: Promise<void> };
  private readonly maxItems: number;
  private readonly maxBytes: number;
  private readonly pageSize: number;
  private readonly interval: number;
  private readonly filter: { archived?: boolean; pinned?: boolean };
  constructor(private client: Client, options: TreeCatalogOptions = {}) {
    this.maxItems = boundedInteger(options.maxItems ?? 500, 'maxItems', 4096);
    this.maxBytes = boundedInteger(options.maxBytes ?? 2 << 20, 'maxBytes', 64 << 20);
    if (this.maxBytes < 4096) throw new RangeError('maxBytes must be at least 4096');
    this.pageSize = boundedInteger(options.pageSize ?? 100, 'pageSize', 100);
    this.interval = boundedInteger(options.pollIntervalMs ?? 2000, 'pollIntervalMs', 60_000);
    this.filter = { ...(options.archived === undefined ? {} : { archived: options.archived }), ...(options.pinned === undefined ? {} : { pinned: options.pinned }) };
    this.current = freeze({ status: 'idle', runtimeID: client.runtimeID, revision: null, items: [], nextCursor: null, windowAfter: null, retainedBytes: 0, truncated: false });
  }
  getSnapshot = (): DeepReadonly<TreeCatalogSnapshot> => this.current;
  subscribe = (listener: () => void): (() => void) => {
    if (this.closed) throw new Error('Tree catalog view is closed');
    if (this.listeners.size >= 64 && !this.listeners.has(listener)) throw new RangeError('Tree catalog listener limit exceeded');
    this.listeners.add(listener); return () => { this.listeners.delete(listener); };
  };
  private publish(value: TreeCatalogSnapshot) {
    measure(value); this.current = freeze(value);
    for (const listener of this.listeners) listener();
  }
  private patch(value: Partial<TreeCatalogSnapshot>) { this.publish({ ...this.current, ...value } as TreeCatalogSnapshot); }
  private schedule() {
    if (!this.active || this.closed || this.timer) return;
    this.timer = setTimeout(() => { this.timer = undefined; void this.refresh(); }, this.interval);
  }
  async start(): Promise<void> {
    if (this.closed) throw new Error('Tree catalog view is closed');
    this.active = true; if (this.controller.signal.aborted) this.controller = new AbortController();
    await this.refresh();
  }
  async suspend(): Promise<void> {
    this.active = false; this.generation++; clearTimeout(this.timer); this.timer = undefined; this.controller.abort();
    if (!this.closed) this.patch({ status: 'suspended' });
    await this.pending?.catch(() => {}); await this.navigation?.promise.catch(() => {});
  }
  resume(): Promise<void> { return this.start(); }
  async reconnect(client: Client): Promise<void> {
    if (client.runtimeID !== this.current.runtimeID) throw new TypeError('Tree catalog belongs to another runtime');
    await this.suspend(); this.client = client; await this.start();
  }
  async dispose(): Promise<void> {
    if (this.closed) return;
    await this.suspend(); this.closed = true;
    this.patch({ status: 'closed', items: [], nextCursor: null }); this.listeners.clear();
  }
  private valid(generation: number) { return generation === this.generation && this.active && !this.closed; }
  refresh(): Promise<void> {
    if (this.closed) return Promise.reject(new Error('Tree catalog view is closed'));
    if (!this.active) return Promise.resolve();
    if (this.pending) return this.pending;
    const generation = this.generation;
    this.pending = (async () => {
      if (this.current.revision !== null) {
        const head = await this.client.treeCatalog({ signal: this.controller.signal });
        if (!this.valid(generation)) return;
        if (head.revision === this.current.revision) { this.patch({ status: 'live', error: undefined }); return; }
      }
      await this.fetch(generation, false, this.current.windowAfter);
    })().catch(error => { if (this.valid(generation)) this.patch({ status: 'stale', error: describe(error) }); })
      .finally(() => { this.pending = undefined; this.schedule(); });
    return this.pending;
  }
  private async fetch(generation: number, append: boolean, after: string | null) {
    const old = this.current;
    const limit = Math.min(this.pageSize, append ? this.maxItems - old.items.length : this.maxItems);
    if (limit < 1) { this.patch({ truncated: true }); return; }
    const page = await this.client.listTrees({ ...this.filter, limit, ...(after === null ? {} : { after }), ...(append && old.revision !== null ? { expected_revision: old.revision } : {}) }, { signal: this.controller.signal });
    if (!this.valid(generation)) return;
    if (append && page.revision !== old.revision) throw new TypeError('Catalog page revision mismatch');
    let previous = after ?? '';
    for (const item of page.items) {
      if (item.tree.id <= previous) throw new TypeError('Catalog cursor did not advance');
      previous = item.tree.id;
    }
    if (page.next_cursor !== null && (!page.items.length || page.next_cursor !== previous)) throw new TypeError('Catalog continuation mismatch');
    const items = append ? [...old.items] as TreeSummary[] : [];
    let overflow = false;
    const candidate = (next: TreeSummary[]): TreeCatalogSnapshot => ({ status: 'live', runtimeID: old.runtimeID, revision: page.revision, items: next, nextCursor: page.next_cursor, windowAfter: append ? old.windowAfter : after, retainedBytes: 0, truncated: false });
    for (const item of page.items) {
      if (bytes(candidate([...items, item])) > this.maxBytes - 2048) { overflow = true; break; }
      items.push(item);
    }
    if (!items.length && page.items.length) throw new RangeError('Catalog item exceeds view byte limit');
    const next = overflow ? items.at(-1)!.tree.id : page.next_cursor;
    this.publish({ ...candidate(items), nextCursor: next, truncated: overflow || next !== null && items.length >= this.maxItems });
  }
  private navigate(kind: 'more' | 'next'): Promise<void> {
    if (this.closed || !this.active) return Promise.reject(new Error('Tree catalog is not observing'));
    if (this.navigation) return this.navigation.kind === kind ? this.navigation.promise : Promise.reject(new Error('Catalog navigation is busy'));
    const generation = this.generation;
    const promise = (async () => {
      await this.pending;
      if (!this.valid(generation) || this.current.nextCursor === null) return;
      clearTimeout(this.timer); this.timer = undefined;
      this.pending = this.fetch(generation, kind === 'more', this.current.nextCursor).catch(async error => {
        if (!this.valid(generation)) return;
        if (isConflict(error)) { await this.fetch(generation, false, this.current.windowAfter); return; }
        this.patch({ status: 'stale', error: describe(error) }); throw error;
      });
      try { await this.pending; } finally { this.pending = undefined; this.schedule(); }
    })().finally(() => { this.navigation = undefined; });
    this.navigation = { kind, promise }; return promise;
  }
  loadMore(): Promise<void> { return this.navigate('more'); }
  /** Replace the bounded window, so large catalogs remain traversable at capacity. */
  next(): Promise<void> { return this.navigate('next'); }
}
export const createTreeCatalogView = (client: Client, options?: TreeCatalogOptions) => new TreeCatalogView(client, options);
