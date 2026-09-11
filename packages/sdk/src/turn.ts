import type { CommandResult, StreamEvent, SubmitPayload } from '@whip/protocol';
import type { SdkEvent } from './client.js';
import type { CommandHandle } from './command.js';
import { DeliveryUncertainError, WhipError, abortError, asError } from './errors.js';
import { executionCode } from './executions.js';
import type { Session } from './session.js';
import type { Subscription } from './subscription.js';
import { object } from './util.js';

export interface RunOptions {
  /** Cancels the turn and stops observing when aborted. */
  signal?: AbortSignal;
  /** Also yield the events of agents spawned during this turn, tagged with their agentId. Child lifecycle events are always yielded. */
  includeChildren?: boolean;
  /** Bounds the unread event buffer; exceeding it fails the iterable, never the result. */
  maxMessages?: number;
  maxBytes?: number;
  /** How long result() waits for the end event after the command settled. */
  endTimeoutMs?: number;
}

export type TurnStatus = 'succeeded' | 'failed' | 'cancelled' | 'interrupted';
export type TurnFailure = NonNullable<CommandResult['failure']>;
export type TurnUsage = NonNullable<StreamEvent['usage']>;
export interface QuestionOption { readonly label: string; readonly description?: string; readonly recommended?: boolean }
export interface QuestionSet { readonly question: string; readonly options: readonly QuestionOption[]; readonly multiple: boolean }

/** Every turn event carries its ordinal and the agent and turn it belongs to. */
export interface TurnEventBase { readonly seq: string; readonly agentId: string; readonly turnId: string }
export type TurnEvent = TurnEventBase & (
  | { readonly type: 'text'; readonly delta: string }
  | { readonly type: 'reasoning'; readonly delta: string }
  | { readonly type: 'cell'; readonly id: string; readonly status: 'called' | 'running' | 'completed'; readonly code?: string; readonly output?: string; readonly result?: string }
  | { readonly type: 'host'; readonly id: string; readonly invocationId: string; readonly operation: string; readonly summary: string; readonly status: 'running' | 'completed' | 'failed' | 'cancelled'; readonly error?: string; readonly duration?: string }
  | { readonly type: 'progress'; readonly id: string; readonly operation: string; readonly text: string }
  | { readonly type: 'hook'; readonly hook: string; readonly operation: string; readonly decision: string; readonly reason: string }
  | { readonly type: 'question'; readonly id: string; readonly question: string; readonly options: readonly QuestionOption[]; readonly multiple: boolean; readonly questions?: readonly QuestionSet[] }
  | { readonly type: 'permission'; readonly id: string; readonly operation: string; readonly command: string; readonly rule: string; readonly path: string }
  | { readonly type: 'child'; readonly childId: string; readonly kind: string; readonly status: string }
  | { readonly type: 'notice'; readonly text: string }
  | { readonly type: 'usage'; readonly usage: TurnUsage }
  | { readonly type: 'end'; readonly status: TurnStatus; readonly error?: string }
  | { readonly type: 'raw'; readonly event: SdkEvent }
);

export type TurnResult<Output = unknown> =
  | { readonly status: 'succeeded'; readonly turnId: string; readonly text: string; readonly output: Output; readonly usage?: TurnUsage }
  | { readonly status: 'failed' | 'cancelled' | 'interrupted'; readonly turnId?: string; readonly text?: string; readonly failure: TurnFailure; readonly usage?: TurnUsage };

const text = (value: unknown): string => typeof value === 'string' ? value : '';
const childKinds = ['agent.admitted', 'agent.turn.started', 'agent.turn.succeeded', 'agent.turn.failed', 'agent.turn.cancelled', 'agent.turn.interrupted', 'agent.subtree.stopped', 'agent.subtree.deleted', 'agent.prompt.queued'];
const endKinds: Record<string, TurnStatus> = { 'turn.succeeded': 'succeeded', 'turn.failed': 'failed', 'turn.cancelled': 'cancelled', 'turn.interrupted': 'interrupted' };

/**
 * One root turn: the submit command plus every event the daemon journals for
 * it, mapped to a small typed union. The subscription starts before the
 * command so nothing is missed; the turn is identified by the turn.started
 * whose inbox sequence is the command's ingress sequence. The command outcome
 * is authoritative; the event stream is best effort.
 */
export class Turn<Output = unknown> implements AsyncIterable<TurnEvent> {
  private readonly queue: TurnEvent[] = [];
  private queuedBytes = 0;
  private waiter?: { resolve(value: IteratorResult<TurnEvent>): void; reject(error: Error): void };
  private iterableError?: Error;
  private iterableClosed = false;
  private subscription?: Subscription;
  private command?: CommandHandle<'submit'>;
  private readonly started: Promise<CommandHandle<'submit'>>;
  private readonly pump: Promise<void>;
  private readonly ended: Promise<void>;
  private resolveEnded!: () => void;
  private turnIdentity?: string;
  private resolveTurn!: (turnId: string) => void;
  private rejectTurn!: (error: Error) => void;
  private readonly children = new Set<string>();
  private accumulated = '';
  private lastUsage?: TurnUsage;
  private endStatus?: TurnStatus;
  /** Resolves once the daemon has started the turn; rejects when the command settles without one. */
  readonly turnId: Promise<string>;

  constructor(private readonly session: Session, private readonly payload: SubmitPayload, private readonly options: RunOptions = {}) {
    this.turnId = new Promise((resolve, reject) => { this.resolveTurn = resolve; this.rejectTurn = reject; });
    void this.turnId.catch(() => {});
    this.ended = new Promise(resolve => { this.resolveEnded = resolve; });
    let resolveStarted!: (command: CommandHandle<'submit'>) => void;
    let rejectStarted!: (error: Error) => void;
    this.started = new Promise((resolve, reject) => { resolveStarted = resolve; rejectStarted = reject; });
    void this.started.catch(() => {});
    this.pump = this.begin(resolveStarted, rejectStarted).catch(error => { this.fail(asError(error)); });
    if (options.signal) {
      const abort = () => { void this.cancel().catch(() => {}); this.fail(abortError(options.signal)); };
      if (options.signal.aborted) abort();
      else options.signal.addEventListener('abort', abort, { once: true });
    }
  }

  private async begin(resolveStarted: (command: CommandHandle<'submit'>) => void, rejectStarted: (error: Error) => void): Promise<void> {
    const { session } = this;
    let ingressSeq: string | undefined;
    try {
      const snapshot = await session.snapshot();
      this.subscription = await session.client.events.subscribe(session.rootId, snapshot.cursor, { maxMessages: this.options.maxMessages, maxBytes: this.options.maxBytes });
      this.command = session.submit(this.payload);
      resolveStarted(this.command);
      try { ingressSeq = (await this.command.accepted()).ingress_seq; }
      catch (error) { if (!(error instanceof DeliveryUncertainError)) throw error; }
    } catch (error) {
      rejectStarted(asError(error));
      throw error;
    }
    for await (const event of this.subscription) {
      const payload = object(event.payload) ? event.payload : {};
      const agentId = text(payload.agent_id);
      const eventTurn = text(payload.turn_id);
      if (this.turnIdentity === undefined) {
        // The turn whose inbox sequence is this command's ingress sequence; with acceptance unknown, the next root turn.
        if (event.kind === 'turn.started' && agentId === session.rootId && (ingressSeq === undefined || text(payload.inbox_seq) === ingressSeq)) {
          this.turnIdentity = eventTurn;
          this.resolveTurn(eventTurn);
          this.push({ type: 'raw', event, seq: event.seq, agentId, turnId: eventTurn });
        }
        continue;
      }
      const isRoot = agentId === session.rootId;
      const child = childKinds.includes(event.kind) && agentId !== '' && !isRoot;
      if (event.kind === 'agent.admitted' && child) this.children.add(agentId);
      const belongs = eventTurn === this.turnIdentity || (eventTurn === '' && isRoot) || (this.children.has(agentId) && (child || this.options.includeChildren === true));
      if (!belongs) continue;
      const mapped = this.map(event, payload, agentId, eventTurn || this.turnIdentity, child);
      if (mapped) this.push(mapped);
      if (mapped?.type === 'end') break;
    }
    this.finish();
  }

  private map(event: SdkEvent, payload: Record<string, unknown>, agentId: string, turnId: string, child: boolean): TurnEvent | undefined {
    const base = { seq: event.seq, agentId, turnId };
    if (child) return { ...base, type: 'child', childId: agentId, kind: event.kind, status: text(payload.status) };
    const isRoot = agentId === this.session.rootId;
    switch (event.kind) {
      case 'stream.text':
        if (isRoot) this.accumulated += text(payload.text);
        return { ...base, type: 'text', delta: text(payload.text) };
      case 'stream.reasoning': return { ...base, type: 'reasoning', delta: text(payload.text) };
      case 'stream.tool.call': case 'stream.tool.started': case 'stream.tool.output': case 'stream.tool.completed': {
        if (event.kind !== 'stream.tool.output' && payload.name !== 'rlm_exec') return { ...base, type: 'raw', event };
        const id = text(payload.id);
        if (event.kind === 'stream.tool.call') return { ...base, type: 'cell', id, status: 'called', code: executionCode(text(payload.args)) };
        if (event.kind === 'stream.tool.started') return { ...base, type: 'cell', id, status: 'running', code: executionCode(text(payload.args)) };
        if (event.kind === 'stream.tool.output') return { ...base, type: 'cell', id, status: 'running', output: text(payload.text) };
        return { ...base, type: 'cell', id, status: 'completed', result: text(payload.result) };
      }
      case 'stream.cell.host.started':
        return { ...base, type: 'host', id: text(payload.id), invocationId: text(payload.invocation_id), operation: text(payload.name), summary: text(payload.args), status: 'running' };
      case 'stream.cell.host': {
        const hostStatus = text(payload.host_status);
        const status = hostStatus === 'cancelled' ? 'cancelled' : hostStatus === 'failed' || text(payload.result) !== '' ? 'failed' : 'completed';
        return { ...base, type: 'host', id: text(payload.id), invocationId: text(payload.invocation_id), operation: text(payload.name), summary: text(payload.args), status, ...(text(payload.result) ? { error: text(payload.result) } : {}), duration: text(payload.text) };
      }
      case 'stream.tool.progress': return { ...base, type: 'progress', id: text(payload.id), operation: text(payload.name), text: text(payload.text) };
      case 'stream.hook.decision': return { ...base, type: 'hook', hook: text(payload.name), operation: text(payload.args), decision: text(payload.text), reason: text(payload.result) };
      case 'question.pending':
        return { ...base, type: 'question', id: text(payload.question_id), question: text(payload.question), options: Array.isArray(payload.options) ? payload.options as QuestionOption[] : [], multiple: payload.multiple === true,
          ...(Array.isArray(payload.questions) && payload.questions.length > 0 ? { questions: payload.questions as QuestionSet[] } : {}) };
      case 'permission.pending':
        return { ...base, type: 'permission', id: text(payload.permission_id), operation: text(payload.operation), command: text(payload.command), rule: text(payload.rule), path: text(payload.canonical_path) };
      case 'stream.notice': return { ...base, type: 'notice', text: text(payload.text) };
      case 'stream.usage': {
        if (!object(payload.usage)) return { ...base, type: 'raw', event };
        const usage = payload.usage as unknown as TurnUsage;
        if (isRoot) this.lastUsage = usage;
        return { ...base, type: 'usage', usage };
      }
      default: {
        const status = endKinds[event.kind];
        if (status && isRoot) {
          this.endStatus = status;
          return { ...base, type: 'end', status, ...(text(payload.error) ? { error: text(payload.error) } : {}) };
        }
        return { ...base, type: 'raw', event };
      }
    }
  }

  private push(event: TurnEvent): void {
    if (this.iterableClosed || this.iterableError) return;
    if (this.waiter) { const waiter = this.waiter; this.waiter = undefined; waiter.resolve({ done: false, value: event }); return; }
    const bytes = JSON.stringify(event).length;
    if (this.queue.length >= (this.options.maxMessages ?? 1024) || this.queuedBytes + bytes > (this.options.maxBytes ?? 8 << 20)) {
      this.iterableError = new WhipError('resynchronization_required', 'Turn event consumer fell behind; the result still settles from the command');
      this.queue.length = 0; this.queuedBytes = 0;
      return;
    }
    this.queue.push(event); this.queuedBytes += bytes;
  }
  /** Ends iteration; the result still resolves from the command. */
  private fail(error: Error): void {
    if (!this.iterableError && !this.iterableClosed) { this.iterableError = error; this.waiter?.reject(error); this.waiter = undefined; }
    if (this.turnIdentity === undefined) this.rejectTurn(error);
    void this.subscription?.dispose().catch(() => {});
    this.resolveEnded();
  }
  private finish(): void {
    this.iterableClosed = true;
    this.waiter?.resolve({ done: true, value: undefined }); this.waiter = undefined;
    if (this.turnIdentity === undefined) this.rejectTurn(new WhipError('execution_failed', 'The turn ended before it started'));
    void this.subscription?.dispose().catch(() => {});
    this.resolveEnded();
  }

  [Symbol.asyncIterator](): AsyncIterator<TurnEvent> {
    return {
      next: (): Promise<IteratorResult<TurnEvent>> => {
        const next = this.queue.shift();
        if (next) { this.queuedBytes -= JSON.stringify(next).length; return Promise.resolve({ done: false, value: next }); }
        if (this.iterableError) return Promise.reject(this.iterableError);
        if (this.iterableClosed) return Promise.resolve({ done: true, value: undefined });
        if (this.waiter) return Promise.reject(new WhipError('invalid_arguments', 'A turn supports one iterator consumer'));
        return new Promise((resolve, reject) => { this.waiter = { resolve, reject }; });
      },
      return: async (): Promise<IteratorResult<TurnEvent>> => { this.iterableClosed = true; this.waiter = undefined; return { done: true, value: undefined }; },
    };
  }

  /** The command's terminal outcome, joined with the end event when the turn ran. */
  async result(): Promise<TurnResult<Output>> {
    const command = await this.started;
    const outcome = await command.result({ signal: this.options.signal });
    if (this.turnIdentity !== undefined && !this.endStatus) {
      await Promise.race([this.ended, this.pump, new Promise(resolve => setTimeout(resolve, this.options.endTimeoutMs ?? 5_000))]);
    }
    // The command is the truth: once it has settled, nothing further can start
    // this turn, and an end event that has not arrived by now is not waited for.
    if (!this.iterableClosed && !this.iterableError) this.finish();
    const result: Record<string, unknown> = object(outcome.result) ? outcome.result : {};
    const usage = this.lastUsage ? { usage: this.lastUsage } : {};
    if (outcome.status === 'succeeded') {
      return { status: 'succeeded', turnId: this.turnIdentity ?? '', text: text(result.text) || this.accumulated, output: result.output as Output, ...usage };
    }
    const status: TurnStatus = outcome.status === 'cancelled' || outcome.status === 'interrupted' ? outcome.status : 'failed';
    const failure: TurnFailure = outcome.failure ?? { code: -32000, message: `turn ${outcome.status}`, data: { kind: 'execution_failed' } };
    return { status, ...(this.turnIdentity !== undefined ? { turnId: this.turnIdentity } : {}), ...(this.accumulated ? { text: this.accumulated } : {}), failure, ...usage };
  }
  /** The final text, or a rejection carrying the failure. */
  async text(): Promise<string> {
    const result = await this.result();
    if (result.status === 'succeeded') return result.text;
    throw new WhipError(result.failure.data?.kind ?? 'execution_failed', result.failure.message, { cause: result.failure });
  }
  /** Command-targeted cancellation; resolves once the daemon accepted it. */
  async cancel(): Promise<void> {
    const command = await this.started;
    await command.cancel().accepted();
  }
}
