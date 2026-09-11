import type {
  DefinitionList, DefinitionParams, DefinitionRecord, DefinitionRegisterParams, DefinitionRegisterResult, HookInvokeParams, HookResultParams, ToolInvokeParams,
} from '@whip/protocol';
import type { CallOptions, WhipClient } from './client.js';
import { WhipError, abortError, asError } from './errors.js';

/** The wire document the daemon validates and stores. Generated from the Go definition. */
export type Definition = DefinitionRegisterParams['definition'];
export type ToolSpec = NonNullable<Definition['tools']>[number];
export type ChildDefinition = Definition['children'][string];

/** Context handed to a tool handler for one invocation. */
export interface ToolContext {
  /** The daemon's ledger operation id; reuse it to make side effects idempotent. */
  readonly invocationId: string;
  readonly rootId: string;
  readonly agentId: string;
  readonly turnId: string;
  /** Unix milliseconds after which the daemon settles the call as timed out. */
  readonly deadline: number;
  /** Aborted on cancellation, deadline, executor close, or disconnect. */
  readonly signal: AbortSignal;
  /** Report intermediate output; the daemon streams it to the session. */
  progress(text: string): void;
}
export type ToolHandler<I = Record<string, unknown>> = (input: I, context: ToolContext) => Promise<unknown> | unknown;

export interface ToolDefinition<I = Record<string, unknown>> {
  readonly spec: ToolSpec;
  readonly handler: ToolHandler<I>;
}

/** Wire shapes a before_spawn hook sees: the request as the model wrote it and what it resolved to. */
export type SpawnRequest = NonNullable<HookInvokeParams['spawn']>['request'];
export type ResolvedChild = NonNullable<HookInvokeParams['spawn']>['resolved'];
export type HookName = 'before_tool' | 'before_spawn' | 'turn_start';

/** Identity every hook event carries. */
export interface HookContext {
  readonly invocationId: string;
  readonly rootId: string;
  readonly agentId: string;
  readonly turnId: string;
  /** The session's permission mode; hooks only narrow it. */
  readonly permissionMode: string;
  /** Unix milliseconds after which the daemon applies the hook's required or optional rule. */
  readonly deadline: number;
  /** Aborted on cancellation, deadline, executor close, or disconnect. */
  readonly signal: AbortSignal;
}
export interface BeforeToolEvent extends HookContext {
  /** module.operation, such as shell.run or tools.lookup_ticket. */
  readonly operation: string;
  readonly arguments: Record<string, unknown>;
}
/** Every field is optional; returning nothing allows the operation unchanged. */
export interface BeforeToolResult { decision?: 'allow' | 'deny'; reason?: string; arguments?: Record<string, unknown> }
export interface BeforeSpawnEvent extends HookContext { readonly spawn: SpawnRequest; readonly resolved: ResolvedChild }
export interface BeforeSpawnResult { decision?: 'allow' | 'deny'; reason?: string; spawn?: SpawnRequest }
export interface TurnStartEvent extends HookContext { readonly input: string }
export interface TurnStartResult { context?: string }
export type HookHandler<E, R> = (event: E) => Promise<R | void> | R | void;
export interface HookOptions {
  /** Proceed with a notice when unanswered or failing, instead of denying. */
  optional?: boolean;
  /** Default 30 seconds, ceiling 60. */
  timeoutMs?: number;
}
export interface HooksInput {
  /** Runs before every host operation the agent's cells call; `operations` narrows it to module.operation names. */
  beforeTool?: HookHandler<BeforeToolEvent, BeforeToolResult> | (HookOptions & { handler: HookHandler<BeforeToolEvent, BeforeToolResult>; operations?: string[] });
  /** Runs after a spawn request is parsed and resolved, before the child is admitted. */
  beforeSpawn?: HookHandler<BeforeSpawnEvent, BeforeSpawnResult> | (HookOptions & { handler: HookHandler<BeforeSpawnEvent, BeforeSpawnResult> });
  /** Contributes ephemeral context to each turn; it never gates. */
  turnStart?: HookHandler<TurnStartEvent, TurnStartResult> | (HookOptions & { handler: HookHandler<TurnStartEvent, TurnStartResult> });
}
export interface HookHandlers {
  readonly before_tool?: HookHandler<BeforeToolEvent, BeforeToolResult>;
  readonly before_spawn?: HookHandler<BeforeSpawnEvent, BeforeSpawnResult>;
  readonly turn_start?: HookHandler<TurnStartEvent, TurnStartResult>;
}

export interface InstructionsInput {
  persona?: string;
  rules?: string;
  /** Project instruction files read along the authorized directory chain. Omit to disable discovery. */
  projectFiles?: string[];
  skillDiscovery?: boolean;
  standingInstructions?: boolean;
}
export interface ModelInput { model?: string; provider?: string; effort?: string }
export interface CompactionInput { model?: string; provider?: string; threshold?: number }
export interface ChildInput {
  instructions?: InstructionsInput;
  modules?: string[];
  capabilities?: string[];
  tools?: string[];
  model?: ModelInput;
  budgets?: Record<string, number>;
  report?: 'notice' | 'inline' | 'message';
}
export interface AgentInput {
  /** Lowercase id, letters, digits and hyphens; must not shadow a built-in. */
  id: string;
  instructions?: InstructionsInput;
  /** Built-in host modules the model may call. */
  modules: string[];
  capabilities?: string[];
  model?: ModelInput;
  compaction?: CompactionInput;
  /** Named MCP servers from host configuration; omit for every configured server. Requires the mcp capability. */
  mcp?: { servers?: string[] };
  tools?: ToolDefinition<never>[] | ToolDefinition[];
  children?: Record<string, ChildInput>;
  surface?: { autoTitle?: boolean; goalLoop?: boolean };
  hooks?: HooksInput;
}

/** An authored agent: the document the daemon stores plus the handlers an executor serves. */
export interface AgentDefinition {
  readonly document: Definition;
  readonly handlers: ReadonlyMap<string, ToolHandler>;
  readonly hooks: HookHandlers;
}

/** Declare one custom tool. The schema is JSON Schema for the keyword arguments. */
export function tool<I = Record<string, unknown>>(
  name: string, description: string, inputSchema: Record<string, unknown>, handler: ToolHandler<I>, options: { timeoutMs?: number } = {},
): ToolDefinition<I> {
  if (!name.trim()) throw new TypeError('tool name is required');
  if (typeof inputSchema !== 'object' || inputSchema === null || Array.isArray(inputSchema)) throw new TypeError(`tool ${name}: input schema must be a JSON Schema object`);
  if (options.timeoutMs !== undefined && (!Number.isSafeInteger(options.timeoutMs) || options.timeoutMs < 0)) throw new TypeError(`tool ${name}: timeoutMs must be a non-negative integer`);
  return { spec: { name, description, input_schema: inputSchema, timeout_millis: options.timeoutMs ?? 0 }, handler };
}

/**
 * Build the canonical definition document from authoring input. The daemon is
 * the authority on validation; this only rejects shapes that cannot be sent.
 */
export function defineAgent(input: AgentInput): AgentDefinition {
  if (!input.id.trim()) throw new TypeError('agent id is required');
  if (!Array.isArray(input.modules) || input.modules.length === 0) throw new TypeError(`agent ${input.id}: select at least one host module`);
  const handlers = new Map<string, ToolHandler>();
  const tools: ToolSpec[] = [];
  for (const definition of (input.tools ?? []) as ToolDefinition[]) {
    if (handlers.has(definition.spec.name)) throw new TypeError(`agent ${input.id}: tool ${definition.spec.name} is declared twice`);
    handlers.set(definition.spec.name, definition.handler);
    tools.push(definition.spec);
  }
  const children: Definition['children'] = {};
  for (const [name, child] of Object.entries(input.children ?? {})) {
    children[name] = {
      instructions: child.instructions ? instructions(child.instructions) : null,
      modules: list(child.modules), capabilities: list(child.capabilities), tools: list(child.tools),
      model: model(child.model), budgets: { ...(child.budgets ?? {}) }, report: child.report ?? '',
    };
  }
  const hooks: HookHandlers = {};
  const hookSpecs: Record<HookName, NonNullable<Definition['hooks']>['before_tool']> = { before_tool: null, before_spawn: null, turn_start: null };
  const declare = <E, R>(name: HookName, value: HookHandler<E, R> | (HookOptions & { handler: HookHandler<E, R>; operations?: string[] }) | undefined) => {
    if (!value) return;
    const options: HookOptions & { operations?: string[] } = typeof value === 'function' ? {} : value;
    const handler = typeof value === 'function' ? value : value.handler;
    if (typeof handler !== 'function') throw new TypeError(`agent ${input.id}: hook ${name} needs a handler`);
    if (options.timeoutMs !== undefined && (!Number.isSafeInteger(options.timeoutMs) || options.timeoutMs < 0)) throw new TypeError(`agent ${input.id}: hook ${name} timeoutMs must be a non-negative integer`);
    if (options.operations !== undefined && name !== 'before_tool') throw new TypeError(`agent ${input.id}: only before_tool accepts operations`);
    (hooks as Record<string, unknown>)[name] = handler;
    hookSpecs[name] = { operations: list(options.operations), optional: options.optional ?? false, timeout_millis: options.timeoutMs ?? 0 };
  };
  declare('before_tool', input.hooks?.beforeTool);
  declare('before_spawn', input.hooks?.beforeSpawn);
  declare('turn_start', input.hooks?.turnStart);
  const document: Definition = {
    id: input.id,
    instructions: instructions(input.instructions ?? {}),
    modules: [...input.modules],
    capabilities: list(input.capabilities),
    model: model(input.model),
    compaction: { model: input.compaction?.model ?? '', provider: input.compaction?.provider ?? '', threshold: input.compaction?.threshold ?? 0 },
    mcp: { servers: list(input.mcp?.servers) },
    tools: tools.length > 0 ? tools : null,
    children,
    surface: { auto_title: input.surface?.autoTitle ?? true, goal_loop: input.surface?.goalLoop ?? false },
    hooks: Object.keys(hooks).length > 0 ? hookSpecs : null,
  };
  return { document: Object.freeze(document), handlers, hooks: Object.freeze(hooks) };
}

/** Turn a handler's return value into the optional-field hook reply. */
function hookReply(result: unknown): Partial<HookResultParams> {
  if (!result || typeof result !== 'object') return {};
  const value = result as BeforeToolResult & BeforeSpawnResult & TurnStartResult;
  const reply: Partial<HookResultParams> = {};
  if (value.decision) reply.decision = value.decision;
  if (value.reason) reply.reason = value.reason;
  if (value.arguments) reply.arguments = value.arguments;
  if (value.spawn) reply.spawn = value.spawn;
  if (value.context) reply.context = value.context;
  return reply;
}

function instructions(input: InstructionsInput): Definition['instructions'] {
  return {
    persona: input.persona ?? '', rules: input.rules ?? '', project_files: list(input.projectFiles),
    skill_discovery: input.skillDiscovery ?? false, standing_instructions: input.standingInstructions ?? false,
  };
}
function model(input: ModelInput | undefined): Definition['model'] {
  return { model: input?.model ?? '', provider: input?.provider ?? '', effort: input?.effort ?? '' };
}
/** Absent or empty lists are null in the canonical document. */
function list(values: string[] | undefined): null | string[] {
  return values && values.length > 0 ? [...values] : null;
}

export interface ServeOptions { signal?: AbortSignal }

/** A running executor: this process serves the agent's tools and hooks for one definition revision. */
export interface Executor {
  readonly definition: string;
  readonly revision: string;
  /** Current lease generation; a reconnect re-binds and changes it. */
  readonly generation: string;
  /** Tool and hook handlers currently running. */
  readonly active: number;
  /** Stop serving. Running handlers are aborted; later invocations fail fast. */
  close(): void;
  /** Settles when serving stops. */
  readonly done: Promise<void>;
}

/** Registry operations. Registration is idempotent on content: the same document yields the same revision. */
export class Agents {
  constructor(private readonly client: WhipClient) {}
  /**
   * Register the agent, bind this connection as its executor, and run its tool
   * handlers until close(). Bind before creating sessions: a tool call with no
   * executor fails after a short wait. The executor re-binds after a reconnect
   * and drains invocations that were pending for it.
   */
  async serve(agent: AgentDefinition, options: ServeOptions = {}): Promise<Executor> {
    const hookNames = (['before_tool', 'before_spawn', 'turn_start'] as const).filter(name => agent.hooks?.[name]);
    if (agent.handlers.size === 0 && hookNames.length === 0) throw new TypeError(`agent ${agent.document.id} declares no tools or hooks to serve`);
    options.signal?.throwIfAborted();
    const client = this.client;
    const registered = await this.register(agent, options);
    const definition = agent.document.id;
    const revision = registered.revision;
    const tools = [...agent.handlers.keys()];
    const running = new Map<string, AbortController>();
    let generation = '';
    let closed = false;
    let resolveDone!: () => void;
    const done = new Promise<void>(resolve => { resolveDone = resolve; });
    const bind = async () => {
      const lease = await client.call('executor.bind', { definition, revision, tools, ...(hookNames.length > 0 ? { hooks: hookNames } : {}) }, options);
      generation = lease.generation;
    };
    const settleHook = async (invocation: HookInvokeParams, body: Partial<HookResultParams>) => {
      try { await client.call('hook.result', { invocation_id: invocation.invocation_id, generation: invocation.generation, ...body }); }
      catch { /* the lease moved or the hook already settled; the daemon's rule applies */ }
    };
    const runHook = async (invocation: HookInvokeParams) => {
      if (closed) { await settleHook(invocation, { error: 'executor closed' }); return; }
      const handler = agent.hooks?.[invocation.hook as HookName] as HookHandler<unknown, unknown> | undefined;
      if (!handler) { await settleHook(invocation, { error: `no handler for hook ${invocation.hook}` }); return; }
      const controller = new AbortController();
      running.set(invocation.invocation_id, controller);
      const deadline = Number(invocation.deadline_millis);
      const timer = setTimeout(() => controller.abort(new WhipError('timeout', 'Hook deadline passed')), Math.max(0, deadline - Date.now()));
      const context: HookContext = {
        invocationId: invocation.invocation_id, rootId: invocation.root_id, agentId: invocation.agent_id, turnId: invocation.turn_id,
        permissionMode: invocation.permission_mode, deadline, signal: controller.signal,
      };
      try {
        let event: unknown;
        if (invocation.hook === 'before_tool') event = { ...context, operation: invocation.operation ?? '', arguments: (invocation.arguments ?? {}) as Record<string, unknown> } satisfies BeforeToolEvent;
        else if (invocation.hook === 'before_spawn') event = { ...context, spawn: invocation.spawn!.request, resolved: invocation.spawn!.resolved } satisfies BeforeSpawnEvent;
        else event = { ...context, input: invocation.input ?? '' } satisfies TurnStartEvent;
        const result = await handler(event);
        if (!controller.signal.aborted) await settleHook(invocation, hookReply(result));
      } catch (error) {
        if (!controller.signal.aborted) await settleHook(invocation, { error: asError(error).message || 'hook failed' });
      } finally {
        clearTimeout(timer);
        running.delete(invocation.invocation_id);
      }
    };
    const settle = async (invocation: ToolInvokeParams, body: { output?: unknown; error?: string }) => {
      try { await client.call('tool.result', { invocation_id: invocation.invocation_id, generation: invocation.generation, ...body }); }
      catch { /* the lease moved or the call already settled; the daemon's record wins */ }
    };
    const run = async (invocation: ToolInvokeParams) => {
      if (closed) { await settle(invocation, { error: 'executor closed' }); return; }
      const handler = agent.handlers.get(invocation.tool);
      if (!handler) { await settle(invocation, { error: `no handler for tool ${invocation.tool}` }); return; }
      const controller = new AbortController();
      running.set(invocation.invocation_id, controller);
      const deadline = Number(invocation.deadline_millis);
      const timer = setTimeout(() => controller.abort(new WhipError('timeout', 'Tool deadline passed')), Math.max(0, deadline - Date.now()));
      const context: ToolContext = {
        invocationId: invocation.invocation_id, rootId: invocation.root_id, agentId: invocation.agent_id, turnId: invocation.turn_id,
        deadline, signal: controller.signal,
        progress: text => { if (!controller.signal.aborted) void client.call('tool.progress', { invocation_id: invocation.invocation_id, generation: invocation.generation, text }).catch(() => {}); },
      };
      try {
        const output = await handler((invocation.input ?? {}) as Record<string, unknown>, context);
        if (!controller.signal.aborted) await settle(invocation, { output: output === undefined ? null : output });
      } catch (error) {
        if (!controller.signal.aborted) await settle(invocation, { error: asError(error).message || 'tool failed' });
      } finally {
        clearTimeout(timer);
        running.delete(invocation.invocation_id);
      }
    };
    const abortAll = (reason: Error) => { for (const controller of [...running.values()]) controller.abort(reason); running.clear(); };
    // The invoke listener outlives close(): the daemon keeps routing to this
    // connection's lease until it drops, and a fast "executor closed" beats a
    // five-minute timeout. Only the lease this executor last held is answered.
    client.onNotification('tool.invoke', invocation => {
      if (invocation.definition !== definition || invocation.revision !== revision || invocation.generation !== generation) return;
      void run(invocation);
    });
    client.onNotification('hook.invoke', invocation => {
      if (invocation.definition !== definition || invocation.revision !== revision || invocation.generation !== generation) return;
      void runHook(invocation);
    });
    const cancelled = (cancel: { invocation_id: string; reason: string }) => { running.get(cancel.invocation_id)?.abort(new WhipError('cancelled', `Invocation cancelled: ${cancel.reason}`)); };
    const unsubscribe = [client.onNotification('tool.cancel', cancelled), client.onNotification('hook.cancel', cancelled)];
    // After a reconnect the daemon has dropped this lease and failed its calls;
    // bind again and drain anything still addressed to the new lease.
    let connected = true;
    unsubscribe.push(client.subscribe(() => {
      const state = client.getSnapshot().state;
      if (state === 'closed' || state === 'incompatible') { close(); return; }
      if (state !== 'connected') { if (connected) { connected = false; abortAll(new WhipError('disconnected', 'Executor connection lost')); } return; }
      if (connected || closed) return;
      connected = true;
      void bind().then(() => client.call('executor.pending', { definition, revision, generation: generation }))
        .then(pending => {
          for (const invocation of pending.invocations ?? []) void run(invocation);
          for (const invocation of pending.hooks ?? []) void runHook(invocation);
        })
        .catch(() => { /* the next reconnect retries; the daemon fails calls with no executor */ });
    }));
    const close = () => {
      if (closed) return;
      closed = true;
      for (const stop of unsubscribe) stop();
      abortAll(new WhipError('closed', 'Executor closed'));
      resolveDone();
    };
    options.signal?.addEventListener('abort', close, { once: true });
    try { await bind(); } catch (error) { close(); throw error; }
    if (options.signal?.aborted) { close(); throw abortError(options.signal); }
    return { definition, revision, get generation() { return generation; }, get active() { return running.size; }, close, done };
  }
  register(agent: AgentDefinition | Definition, options: CallOptions = {}): Promise<DefinitionRegisterResult> {
    const definition = 'document' in agent ? agent.document : agent;
    return this.client.call('definitions.register', { definition } satisfies DefinitionRegisterParams, options);
  }
  get(id: string, revision?: string, options: CallOptions = {}): Promise<DefinitionRecord> {
    const params: DefinitionParams = revision ? { id, revision } : { id };
    return this.client.call('definitions.get', params, options);
  }
  list(options: CallOptions = {}): Promise<DefinitionList> {
    return this.client.call('definitions.list', {}, options);
  }
}
