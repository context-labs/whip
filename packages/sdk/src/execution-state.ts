import { assertValid } from '@whip/protocol';
import type { Cell, CellOutput, HostOperation, Message, ToolCall, ToolResult, Turn } from '@whip/protocol';
import type { Client } from './index.js';
import type { Session } from './session.js';
import type { ObservationError, ObservationStatus, SessionView } from './state.js';
import { boundedInteger, bytes, freeze } from './value.js';
import type { DeepReadonly } from './value.js';
import { RemoteError } from './wire.js';

type CellOutputPreview = NonNullable<CellOutput['preview']>;

export interface ExecutionViewSnapshot {
  status: ObservationStatus;
  runtimeID: string;
  sessionID: string;
  epoch: string | null;
  historyRevision: string | null;
  turns: Turn[];
  cells: Cell[];
  output: CellOutputPreview | null;
  operations: HostOperation[];
  windowBefore: string | null;
  olderCursor: string | null;
  latestMissing: boolean;
  retainedBytes: number;
  truncated: boolean;
  unavailable: boolean;
  error?: ObservationError;
}
export interface ExecutionViewOptions {
  maxTurns?: number;
  maxCells?: number;
  maxOperations?: number;
  maxBytes?: number;
  pollIntervalMs?: number;
}

/** Canonical execution evidence alongside one existing transcript owner. Reads
 * never execute code, replay effects, or copy transcript bodies into this view. */
export class ExecutionView {
  private current: DeepReadonly<ExecutionViewSnapshot>;
  private readonly listeners = new Set<() => void>();
  private readonly maxTurns: number;
  private readonly maxCells: number;
  private readonly maxOperations: number;
  private readonly maxBytes: number;
  private readonly interval: number;
  private controller = new AbortController();
  private generation = 0;
  private active = false;
  private closed = false;
  private pending?: Promise<void>;
  private navigation?: Promise<void>;
  private timer?: ReturnType<typeof setTimeout>;
  private unsubscribe?: () => void;
  private before?: string;
  private focused?: string;

  constructor(private session: Session, private readonly source: SessionView, options: ExecutionViewOptions = {}) {
    const snapshot = source.getSnapshot();
    if (snapshot.runtimeID !== session.client.runtimeID || snapshot.sessionID !== session.id) throw new TypeError('Execution and transcript views must have the same owner');
    this.maxTurns = boundedInteger(options.maxTurns ?? 16, 'maxTurns', 100);
    this.maxCells = boundedInteger(options.maxCells ?? 128, 'maxCells', 1024);
    this.maxOperations = boundedInteger(options.maxOperations ?? 512, 'maxOperations', 4096);
    this.maxBytes = boundedInteger(options.maxBytes ?? 4 << 20, 'maxBytes', 16 << 20);
    if (this.maxBytes < 4096) throw new RangeError('maxBytes must be at least 4096');
    this.interval = boundedInteger(options.pollIntervalMs ?? 1000, 'pollIntervalMs', 60_000);
    this.current = freeze({ status: 'idle', runtimeID: snapshot.runtimeID, sessionID: snapshot.sessionID, epoch: snapshot.epoch, historyRevision: snapshot.history.snapshot?.revision ?? null, turns: [], cells: [], output: null, operations: [], windowBefore: null, olderCursor: null, latestMissing: false, retainedBytes: 0, truncated: false, unavailable: false });
  }
  getSnapshot = (): DeepReadonly<ExecutionViewSnapshot> => this.current;
  subscribe = (listener: () => void): (() => void) => {
    if (this.closed) throw new Error('Execution view is closed');
    if (this.listeners.size >= 64 && !this.listeners.has(listener)) throw new RangeError('Execution view listener limit exceeded');
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };
  private publish(value: ExecutionViewSnapshot) {
    value.retainedBytes = 0;
    let size = bytes(value);
    while (value.retainedBytes !== size) { value.retainedBytes = size; size = bytes(value); }
    this.current = freeze(value);
    for (const listener of this.listeners) listener();
  }
  private patch(value: Partial<ExecutionViewSnapshot>) { this.publish({ ...this.current, ...value } as ExecutionViewSnapshot); }
  private sourceChanged = () => {
    const source = this.source.getSnapshot();
    const revision = source.history.snapshot?.revision ?? null;
    const changed = source.epoch !== this.current.epoch || revision !== this.current.historyRevision;
    if (changed || source.status !== 'live') {
      this.generation++; this.controller.abort(); this.controller = new AbortController();
      if (changed) {
        this.before = undefined; this.focused = undefined;
        this.patch({ epoch: source.epoch, historyRevision: revision, turns: [], cells: [], output: null, operations: [], windowBefore: null, olderCursor: null, latestMissing: false, truncated: false, status: 'loading', error: undefined });
      }
      if (source.status !== 'live') this.patch({ output: null, status: source.status === 'suspended' || source.status === 'closed' ? 'suspended' : 'stale', unavailable: source.unavailable });
    }
    if (this.current.output && source.activity?.active_turn?.id !== this.current.output.turn_id) this.patch({ output: null });
    // Source notifications replace evidence, not a queue of refresh requests.
    this.schedule(changed ? 0 : this.interval);
  };
  private schedule(interval = this.interval) {
    if (!this.active || this.closed || this.timer) return;
    this.timer = setTimeout(() => { this.timer = undefined; void this.refresh(); }, interval);
  }
  async start(): Promise<void> {
    if (this.closed) throw new Error('Execution view is closed');
    if (!this.active) {
      this.active = true;
      if (this.controller.signal.aborted) this.controller = new AbortController();
      this.unsubscribe = this.source.subscribe(this.sourceChanged);
      this.sourceChanged();
    }
    await this.refresh();
  }
  async suspend(): Promise<void> {
    this.active = false; this.generation++; this.controller.abort();
    clearTimeout(this.timer); this.timer = undefined;
    this.unsubscribe?.(); this.unsubscribe = undefined;
    if (!this.closed) this.patch({ status: 'suspended', output: null });
    await this.pending?.catch(() => {});
    await this.navigation?.catch(() => {});
  }
  resume(): Promise<void> { return this.start(); }
  async reconnect(client: Client): Promise<void> {
    if (client.runtimeID !== this.current.runtimeID) throw new TypeError('Execution view belongs to another runtime');
    await this.suspend(); this.session = client.session(this.current.sessionID); await this.start();
  }
  async dispose(): Promise<void> {
    if (this.closed) return;
    await this.suspend(); this.closed = true;
    this.patch({ status: 'closed', turns: [], cells: [], output: null, operations: [], olderCursor: null, error: undefined });
    this.listeners.clear();
  }
  refresh(): Promise<void> {
    if (this.closed) return Promise.reject(new Error('Execution view is closed'));
    if (!this.active) return Promise.resolve();
    if (this.pending) return this.pending;
    const generation = this.generation;
    this.pending = Promise.resolve().then(() => this.read(generation)).catch(error => {
      if (!this.valid(generation)) return;
      this.patch({ status: 'stale', output: null, unavailable: error instanceof TypeError || error instanceof RemoteError && error.kind === 'NOT_FOUND', error: { message: (error instanceof Error ? error.message : String(error)).slice(0, 256), ...(error instanceof RemoteError ? { kind: error.kind.slice(0, 64) } : {}) } });
    }).finally(() => { this.pending = undefined; this.schedule(); });
    return this.pending;
  }
  private valid(generation: number) { return this.active && !this.closed && generation === this.generation; }
  private async read(generation: number) {
    if (!this.valid(generation)) return;
    const source = this.source.getSnapshot();
    if (source.status !== 'live' || source.epoch === null || !source.history.snapshot) return;
    if (source.epoch !== this.session.client.processEpoch) throw new TypeError('Execution connection belongs to another process epoch');
    const options = { signal: this.controller.signal };
    const page = await this.session.turns.page({ before: this.before, limit: this.maxTurns }, options);
    if (!this.valid(generation)) return;
    const ids = new Set<string>();
    if (this.focused) ids.add(this.focused);
    if (this.before === undefined) {
      if (source.activity?.active_turn) ids.add(source.activity.active_turn.id);
      for (const message of [...source.history.messages].reverse()) if (message.turn_id) ids.add(message.turn_id);
    }
    for (const turn of page.items) ids.add(turn.id);
    const value: ExecutionViewSnapshot = { status: 'live', runtimeID: this.current.runtimeID, sessionID: this.current.sessionID, epoch: source.epoch, historyRevision: source.history.snapshot.revision, turns: [], cells: [], output: null, operations: [], windowBefore: this.before ?? null, olderCursor: page.next_cursor, latestMissing: this.before !== undefined, retainedBytes: 0, truncated: ids.size > this.maxTurns || page.next_cursor !== null, unavailable: false };
    let retained = bytes(value), requests = 1;
    const take = <T>(items: T[], record: T) => {
      const size = bytes(record) + 1;
      if (retained + size > this.maxBytes - 2048) { value.truncated = true; return false; }
      items.push(record); retained += size; return true;
    };
    for (const id of [...ids].slice(0, this.maxTurns)) {
      const turn = page.items.find(turn => turn.id === id) ?? await this.session.turns.get(id, options);
      if (!this.valid(generation)) return;
      if (!take(value.turns, turn)) break;
    }
    for (const turn of value.turns) {
      let after: string | undefined;
      // A byte-limited response may be shorter than the requested count. Only
      // an empty page proves the end; advance using the last actual opaque ID.
      while (value.cells.length < this.maxCells && requests++ < 128) {
        const page = await this.session.turns.cells(turn.id, { after, limit: Math.min(100, this.maxCells - value.cells.length) }, options);
        if (!this.valid(generation)) return;
        if (!page.items.length) break;
        for (const cell of page.items) if (!take(value.cells, cell)) break;
        if (value.cells.at(-1)?.id !== page.items.at(-1)!.id) break;
        after = page.items.at(-1)!.id;
      }
      after = undefined;
      while (value.operations.length < this.maxOperations && requests++ < 128) {
        const page = await this.session.turns.operations(turn.id, { after, limit: Math.min(100, this.maxOperations - value.operations.length) }, options);
        if (!this.valid(generation)) return;
        if (!page.items.length) break;
        for (const operation of page.items) if (!take(value.operations, operation)) break;
        if (value.operations.at(-1)?.id !== page.items.at(-1)!.id) break;
        after = page.items.at(-1)!.id;
      }
    }
    const activeTurn = source.activity?.active_turn?.id;
    if (requests < 128 && this.before === undefined && activeTurn && value.cells.some(cell => cell.turn_id === activeTurn && cell.state === 'running')) {
      const observed = await this.session.cells.output(options);
      if (!this.valid(generation)) return;
      const output = observed.preview;
      const cell = output && value.cells.find(cell => cell.id === output.cell_id && cell.turn_id === output.turn_id && cell.state === 'running' && cell.call_id === output.call_id && cell.call_message_id === output.call_message_id);
      if (output && cell && output.history_revision === value.historyRevision && output.turn_id === this.source.getSnapshot().activity?.active_turn?.id) {
        if (retained + bytes(output) <= this.maxBytes - 2048) value.output = output;
        else value.truncated = true;
      }
    }
    value.truncated ||= value.cells.length === this.maxCells || value.operations.length === this.maxOperations || requests >= 128;
    if (this.valid(generation)) this.publish(value);
  }
  private navigate(change: () => void): Promise<void> {
    if (this.closed || !this.active) return Promise.reject(new Error('Execution view is not observing'));
    if (this.navigation) return Promise.reject(new Error('Execution navigation is busy'));
    this.navigation = (async () => {
      await this.pending;
      if (!this.active || this.closed) return;
      change(); await this.refresh();
    })().finally(() => { this.navigation = undefined; });
    return this.navigation;
  }
  loadOlder(): Promise<void> { return this.navigate(() => { if (this.current.olderCursor) { this.before = this.current.olderCursor; this.focused = undefined; } }); }
  latest(): Promise<void> { return this.navigate(() => { this.before = undefined; this.focused = undefined; }); }
  /** Explicitly inspect one retained turn, including evidence outside the transcript window. */
  focus(turnID: string): Promise<void> { assertValid('TurnParams', { turn_id: turnID }); return this.navigate(() => { this.focused = turnID; }); }
}

export const createExecutionView = (session: Session, source: SessionView, options?: ExecutionViewOptions) => new ExecutionView(session, source, options);

export interface CellExecutionRow {
  cell: DeepReadonly<Cell>;
  output: DeepReadonly<CellOutputPreview> | null;
  turn: DeepReadonly<Turn> | null;
  call: { message: DeepReadonly<Message>; value: DeepReadonly<ToolCall> } | null;
  result: { message: DeepReadonly<Message>; value: DeepReadonly<ToolResult> } | null;
  operations: readonly DeepReadonly<HostOperation>[];
}

/** Exact joins only. A copied message with no local turn link, a similarly named
 * call, or an unrelated result never becomes local execution evidence. Returned
 * objects reference the two bounded snapshots; no transcript cache is retained. */
export function cellExecutionRows(execution: DeepReadonly<ExecutionViewSnapshot>, messages: readonly DeepReadonly<Message>[]): CellExecutionRow[] {
  const owned = new Map(messages.filter(message => message.session_id === execution.sessionID && message.turn_id !== null && message.retired_by === null && message.retired_revision === null).map(message => [message.id, message]));
  const turns = new Map(execution.turns.map(turn => [turn.id, turn]));
  return execution.cells.map(cell => {
    const match = (id: string | null) => { const message = id === null ? undefined : owned.get(id); return message?.turn_id === cell.turn_id ? message : undefined; };
    const callMessage = match(cell.call_message_id), resultMessage = match(cell.result_message_id);
    const call = callMessage?.parts?.find(part => part.type === 'tool_call' && part.call.id === cell.call_id);
    const result = resultMessage?.parts?.find(part => part.type === 'tool_result' && part.result.call_id === cell.call_id);
    const output = execution.status === 'live' && cell.state === 'running' && execution.output?.cell_id === cell.id && execution.output.turn_id === cell.turn_id && execution.output.session_id === cell.session_id && execution.output.call_message_id === cell.call_message_id && execution.output.call_id === cell.call_id && execution.output.history_revision === execution.historyRevision ? execution.output : null;
    return { cell, output, turn: turns.get(cell.turn_id) ?? null, call: call?.type === 'tool_call' ? { message: callMessage!, value: call.call } : null, result: result?.type === 'tool_result' ? { message: resultMessage!, value: result.result } : null, operations: execution.operations.filter(operation => operation.origin === 'cell' && operation.cell_id === cell.id && operation.turn_id === cell.turn_id && operation.session_id === cell.session_id) };
  });
}
