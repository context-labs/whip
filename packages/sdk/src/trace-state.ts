import { assertValid } from '@whip/protocol';
import type { TracePageParams, TracePageResult } from '@whip/protocol';
import type { Client } from './index.js';
import type { ObservationError, ObservationStatus } from './state.js';
import { boundedInteger, bytes, freeze } from './value.js';
import type { DeepReadonly } from './value.js';
import { RemoteError } from './wire.js';

export type TraceRow = TracePageResult['items'][number];

export interface TraceViewSnapshot {
  status: ObservationStatus;
  runtimeID: string;
  rootID: string;
  epoch: string;
  revision: string | null;
  observedAtNS: string | null;
  receivedAtMs: number | null;
  traceID: string;
  /** Current canonical rows, newest change first; null spans are tombstones. */
  rows: readonly DeepReadonly<TraceRow>[];
  roots: readonly DeepReadonly<TraceRow>[];
  windowBefore: string | null;
  olderCursor: string | null;
  rootsBefore: string | null;
  olderRootsCursor: string | null;
  latestMissing: boolean;
  retainedBytes: number;
  truncated: boolean;
  unavailable: boolean;
  error?: ObservationError;
}
export interface TraceViewOptions {
  maxRows?: number;
  maxRoots?: number;
  maxBytes?: number;
  pollIntervalMs?: number;
}

/** One bounded root-scoped trace window and a separate bounded trace picker.
 * Every refresh replaces both pages at the same revision. Older windows retain
 * their exclusive cursor; polling never crawls history or jumps to the head. */
export class TraceView {
  private current: DeepReadonly<TraceViewSnapshot>;
  private readonly listeners = new Set<() => void>();
  private readonly maxRows: number;
  private readonly maxRoots: number;
  private readonly rowBytes: number;
  private readonly rootBytes: number;
  private readonly interval: number;
  private controller = new AbortController();
  private generation = 0;
  private active = false;
  private closed = false;
  private closing?: Promise<void>;
  private pending?: Promise<void>;
  private navigation?: Promise<void>;
  private timer?: ReturnType<typeof setTimeout>;
  private before: string | null = null;
  private rootsBefore: string | null = null;
  private traceID = '';

  constructor(
    private client: Client,
    rootID: string,
    options: TraceViewOptions = {},
  ) {
    this.maxRows = boundedInteger(options.maxRows ?? 1024, 'maxRows', 2048);
    this.maxRoots = boundedInteger(options.maxRoots ?? 64, 'maxRoots', 256);
    const maxBytes = boundedInteger(options.maxBytes ?? 1 << 20, 'maxBytes', 2 << 20);
    if (maxBytes < 16384) throw new RangeError('maxBytes must be at least 16384');
    this.rowBytes = Math.min(524288, Math.floor((maxBytes * 3) / 4) - 4096);
    this.rootBytes = Math.min(524288, Math.floor(maxBytes / 4));
    this.interval = boundedInteger(options.pollIntervalMs ?? 1000, 'pollIntervalMs', 60000);
    assertValid('TracePageParams', {
      root_id: rootID,
      before: null,
      expected_revision: null,
      trace_id: '',
      roots_only: false,
      limit: this.maxRows,
      max_bytes: this.rowBytes,
    });
    this.current = freeze({
      status: 'idle',
      runtimeID: client.runtimeID,
      rootID,
      epoch: client.processEpoch,
      revision: null,
      observedAtNS: null,
      receivedAtMs: null,
      traceID: '',
      rows: [],
      roots: [],
      windowBefore: null,
      olderCursor: null,
      rootsBefore: null,
      olderRootsCursor: null,
      latestMissing: false,
      retainedBytes: 0,
      truncated: false,
      unavailable: false,
    });
  }
  getSnapshot = (): DeepReadonly<TraceViewSnapshot> => this.current;
  subscribe = (listener: () => void): (() => void) => {
    if (this.closed) throw new Error('Trace view is closed');
    if (this.listeners.size >= 64 && !this.listeners.has(listener))
      throw new RangeError('Trace view listener limit exceeded');
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  private publish(value: TraceViewSnapshot) {
    value.retainedBytes = 0;
    let size = bytes(value);
    while (value.retainedBytes !== size) {
      value.retainedBytes = size;
      size = bytes(value);
    }
    this.current = freeze(value);
    for (const listener of this.listeners) listener();
  }
  private patch(value: Partial<TraceViewSnapshot>) {
    this.publish({ ...this.current, ...value });
  }
  private schedule() {
    if (!this.active || this.closed || this.timer) return;
    this.timer = setTimeout(() => {
      this.timer = undefined;
      void this.refresh();
    }, this.interval);
  }
  async start(): Promise<void> {
    if (this.closed) throw new Error('Trace view is closed');
    this.active = true;
    if (this.controller.signal.aborted) this.controller = new AbortController();
    if (this.current.revision === null) this.patch({ status: 'loading' });
    await this.refresh();
  }
  async suspend(): Promise<void> {
    this.active = false;
    this.generation++;
    this.controller.abort();
    clearTimeout(this.timer);
    this.timer = undefined;
    if (!this.closed) this.patch({ status: 'suspended' });
    await this.pending?.catch(() => {});
    await this.navigation?.catch(() => {});
  }
  resume(): Promise<void> {
    return this.start();
  }
  async reconnect(client: Client): Promise<void> {
    if (this.closed) throw new Error('Trace view is closed');
    if (client.runtimeID !== this.current.runtimeID)
      throw new TypeError('Trace view belongs to another runtime');
    await this.suspend();
    if (this.closed) return;
    this.client = client;
    if (client.processEpoch !== this.current.epoch) {
      this.before = null;
      this.rootsBefore = null;
      this.patch({
        epoch: client.processEpoch,
        revision: null,
        observedAtNS: null,
        receivedAtMs: null,
        rows: [],
        roots: [],
        windowBefore: null,
        olderCursor: null,
        rootsBefore: null,
        olderRootsCursor: null,
        latestMissing: false,
        truncated: false,
        error: undefined,
        unavailable: false,
      });
    }
    await this.start();
  }
  dispose(): Promise<void> {
    if (this.closing) return this.closing;
    this.closed = true;
    this.closing = this.suspend().then(() => {
      this.patch({
        status: 'closed',
        rows: [],
        roots: [],
        olderCursor: null,
        olderRootsCursor: null,
        error: undefined,
      });
      this.listeners.clear();
    });
    return this.closing;
  }
  refresh(): Promise<void> {
    if (this.closed) return Promise.reject(new Error('Trace view is closed'));
    if (!this.active) return Promise.resolve();
    if (this.pending) return this.pending;
    const generation = this.generation;
    this.pending = Promise.resolve()
      .then(() => this.read(generation))
      .catch((error) => {
        if (!this.valid(generation)) return;
        this.patch({
          status: 'stale',
          unavailable:
            error instanceof TypeError ||
            (error instanceof RemoteError && error.kind === 'NOT_FOUND'),
          error: {
            message: (error instanceof Error ? error.message : String(error)).slice(0, 256),
            ...(error instanceof RemoteError ? { kind: error.kind.slice(0, 64) } : {}),
          },
        });
      })
      .finally(() => {
        this.pending = undefined;
        this.schedule();
      });
    return this.pending;
  }
  private valid(generation: number) {
    return this.active && !this.closed && generation === this.generation;
  }
  private async page(before: string | null, roots: boolean, expected: string | null) {
    const params: TracePageParams = {
      root_id: this.current.rootID,
      before,
      expected_revision: expected,
      trace_id: roots ? '' : this.traceID,
      roots_only: roots,
      limit: roots ? this.maxRoots : this.maxRows,
      max_bytes: roots ? this.rootBytes : this.rowBytes,
    };
    const page = await this.client.tracePage(params, { signal: this.controller.signal });
    const receivedAtMs = performance.now();
    checkPage(page, params);
    return { page, receivedAtMs };
  }
  private async read(generation: number) {
    if (!this.valid(generation)) return;
    const { page, receivedAtMs } = await this.page(this.before, false, null);
    if (!this.valid(generation)) return;
    const { page: roots } = await this.page(this.rootsBefore, true, page.revision);
    if (!this.valid(generation)) return;
    this.publish({
      status: 'live',
      runtimeID: this.current.runtimeID,
      rootID: this.current.rootID,
      epoch: this.client.processEpoch,
      revision: page.revision,
      observedAtNS: page.observed_at_ns,
      receivedAtMs,
      traceID: this.traceID,
      rows: page.items,
      roots: roots.items,
      windowBefore: this.before,
      olderCursor: page.has_more ? page.next : null,
      rootsBefore: this.rootsBefore,
      olderRootsCursor: roots.has_more ? roots.next : null,
      latestMissing: this.before !== null,
      retainedBytes: 0,
      truncated:
        page.has_more || roots.has_more || this.before !== null || this.rootsBefore !== null,
      unavailable: false,
    });
  }
  private navigate(change: () => void): Promise<void> {
    if (this.closed || !this.active)
      return Promise.reject(new Error('Trace view is not observing'));
    if (this.navigation) return Promise.reject(new Error('Trace navigation is busy'));
    this.navigation = (async () => {
      await this.pending;
      if (!this.active || this.closed) return;
      change();
      await this.refresh();
    })().finally(() => {
      this.navigation = undefined;
    });
    return this.navigation;
  }
  loadOlder(): Promise<void> {
    return this.navigate(() => {
      if (this.current.olderCursor !== null) this.before = this.current.olderCursor;
    });
  }
  latest(): Promise<void> {
    return this.navigate(() => {
      this.before = null;
    });
  }
  loadOlderRoots(): Promise<void> {
    return this.navigate(() => {
      if (this.current.olderRootsCursor !== null) this.rootsBefore = this.current.olderRootsCursor;
    });
  }
  latestRoots(): Promise<void> {
    return this.navigate(() => {
      this.rootsBefore = null;
    });
  }
  /** Empty selects all traces in this root's bounded window. */
  selectTrace(traceID: string): Promise<void> {
    assertValid('TraceExportParams', {
      root_id: this.current.rootID,
      trace_id: traceID,
      expected_revision: null,
    });
    return this.navigate(() => {
      this.traceID = traceID;
      this.before = null;
    });
  }
}

function checkPage(page: TracePageResult, params: TracePageParams & { before?: string | null }) {
  if (
    bytes(page) > params.max_bytes ||
    page.items.length > params.limit ||
    (params.expected_revision !== null && page.revision !== params.expected_revision)
  )
    throw new TypeError('Trace page exceeds the requested bounds or revision');
  const before = params.before ?? null;
  const revision = BigInt(page.revision),
    next = BigInt(page.next);
  const upper = before === null ? revision + 1n : BigInt(before);
  let previous = upper;
  const ids = new Set<string>();
  for (const row of page.items) {
    const sequence = BigInt(row.sequence);
    if (
      row.root_id !== params.root_id ||
      sequence <= 0n ||
      sequence > revision ||
      sequence >= previous ||
      ids.has(row.span_id) ||
      (row.span !== null &&
        ((params.trace_id !== '' && row.span.trace_id !== params.trace_id) ||
          (params.roots_only && row.span.parent_span_id !== null)))
    )
      throw new TypeError('Trace page does not match its owner, order or filter');
    ids.add(row.span_id);
    previous = sequence;
  }
  if (page.has_more ? next <= 0n || next >= upper || next > previous : next !== 0n)
    throw new TypeError('Trace page cursor does not advance within its window');
}

export const createTraceView = (client: Client, rootID: string, options?: TraceViewOptions) =>
  new TraceView(client, rootID, options);

/** Host read time advanced only by local monotonic elapsed time. This is display
 * timing, never a committed end timestamp or measured transport latency. */
export function traceNowNS(
  snapshot: DeepReadonly<TraceViewSnapshot>,
  now = performance.now(),
): bigint | null {
  if (snapshot.observedAtNS === null || snapshot.receivedAtMs === null) return null;
  const elapsed = snapshot.status === 'live' ? Math.max(0, now - snapshot.receivedAtMs) : 0;
  return BigInt(snapshot.observedAtNS) + BigInt(Math.floor(elapsed * 1e6));
}
