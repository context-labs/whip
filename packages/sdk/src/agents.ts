import type {
  CreateSessionParams, DefinitionList, SubmitPayload, DefinitionParams, DefinitionRecord, DefinitionRegisterParams, DefinitionRegisterResult, HookInvokeParams, HookResultParams, ToolInvokeParams,
} from '@whip/protocol';
import type { CallOptions, WhipClient } from './client.js';
import type { CommandOptions } from './command.js';
import { Session } from './session.js';
import { Turn, type RunOptions } from './turn.js';
import { WhipError, abortError, asError } from './errors.js';
import { describesObject, formatIssues, isStandardSchema, toJsonSchema, validateWith, type InferInput, type InferOutput, type Schema } from './schema.js';

export type { InferInput, InferOutput, JsonSchema, Schema, StandardSchemaWithJSON } from './schema.js';

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
/** What execute must return: the output schema's type when one is declared, otherwise anything JSON. */
export type ToolReturn<O> = O extends Schema ? InferOutput<O> : unknown;
export type ToolHandler<I extends Schema = Schema, O extends Schema | undefined = Schema | undefined> =
  (input: InferInput<I>, context: ToolContext) => ToolReturn<O> | Promise<ToolReturn<O>>;

export interface ToolOptions<I extends Schema, O extends Schema | undefined = undefined> {
  /** Lowercase identifier; the cell calls tools.<name>. */
  name: string;
  description: string;
  /** Keyword arguments the cell passes. A Standard JSON Schema (zod, ArkType, Valibot) infers the handler's input type; raw JSON Schema types it unknown. */
  input: I;
  /** What execute returns. Typed at compile time and validated locally before the result is posted. */
  output?: O;
  execute(input: InferInput<I>, context: ToolContext): ToolReturn<O> | Promise<ToolReturn<O>>;
  /** Default 5 minutes, ceiling 15. */
  timeoutMs?: number;
  /** Run the schemas' own validation around execute (default true); raw JSON Schema has none. */
  validate?: boolean;
}

export interface ToolDefinition<I extends Schema = Schema, O extends Schema | undefined = Schema | undefined> {
  readonly spec: ToolSpec;
  readonly input: I;
  readonly output: O | undefined;
  readonly validate: boolean;
  execute(input: InferInput<I>, context: ToolContext): ToolReturn<O> | Promise<ToolReturn<O>>;
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
  /** Replaces the parent's output contract for this child. */
  output?: Schema;
}
/** The turn result type an agent's output contract implies: the schema's type, or unknown without one. */
export type AgentOutput<O> = O extends Schema ? InferOutput<O> : unknown;
export interface AgentInput<O extends Schema | undefined = undefined> {
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
  tools?: readonly ToolDefinition[];
  children?: Record<string, ChildInput>;
  surface?: { autoTitle?: boolean; goalLoop?: boolean };
  hooks?: HooksInput;
  /** What a turn returns: an object schema the daemon enforces on the final message. It types the turn result. */
  output?: O;
}

/** An authored agent: the document the daemon stores plus the tools and hooks an executor serves. */
export interface AgentDefinition<Output = unknown> {
  readonly document: Definition;
  /** Tool definitions by name; serve runs their execute and validates around it. */
  readonly handlers: ReadonlyMap<string, ToolDefinition>;
  readonly hooks: HookHandlers;
  /** Carries the output contract's type; never set at runtime. */
  readonly outputType?: Output;
}

/**
 * Declare one custom tool. The input schema's JSON Schema goes to the daemon,
 * which validates every call against it before the handler runs; the output
 * schema types execute's return and is checked before the result is posted.
 */
export function tool<I extends Schema, O extends Schema | undefined = undefined>(options: ToolOptions<I, O>): ToolDefinition<I, O> {
  const { name, description, input, output, execute, timeoutMs, validate = true } = options;
  if (typeof name !== 'string' || !name.trim()) throw new TypeError('tool name is required');
  if (typeof description !== 'string') throw new TypeError(`tool ${name}: description is required`);
  if (typeof execute !== 'function') throw new TypeError(`tool ${name}: execute is required`);
  if (timeoutMs !== undefined && (!Number.isSafeInteger(timeoutMs) || timeoutMs < 0)) throw new TypeError(`tool ${name}: timeoutMs must be a non-negative integer`);
  const inputSchema = toJsonSchema(input, 'input', `tool ${name} input`);
  if (!describesObject(inputSchema)) throw new TypeError(`tool ${name}: input schema must describe an object; the cell passes keyword arguments`);
  const outputSchema = output === undefined ? null : toJsonSchema(output, 'output', `tool ${name} output`);
  return { spec: { name, description, input_schema: inputSchema, output_schema: outputSchema, timeout_millis: timeoutMs ?? 0 }, input, output, validate, execute };
}

/**
 * Build the canonical definition document from authoring input. The daemon is
 * the authority on validation; this only rejects shapes that cannot be sent.
 */
export function defineAgent<O extends Schema | undefined = undefined>(input: AgentInput<O>): AgentDefinition<AgentOutput<O>> {
  if (!input.id.trim()) throw new TypeError('agent id is required');
  const contract = (schema: Schema | undefined, subject: string) => {
    if (schema === undefined) return null;
    const derived = toJsonSchema(schema, 'output', subject);
    if (!describesObject(derived)) throw new TypeError(`${subject}: output contract must describe an object`);
    return derived;
  };
  if (!Array.isArray(input.modules) || input.modules.length === 0) throw new TypeError(`agent ${input.id}: select at least one host module`);
  const handlers = new Map<string, ToolDefinition>();
  const tools: ToolSpec[] = [];
  for (const definition of input.tools ?? []) {
    if (handlers.has(definition.spec.name)) throw new TypeError(`agent ${input.id}: tool ${definition.spec.name} is declared twice`);
    handlers.set(definition.spec.name, definition);
    tools.push(definition.spec);
  }
  const children: Definition['children'] = {};
  for (const [name, child] of Object.entries(input.children ?? {})) {
    children[name] = {
      instructions: child.instructions ? instructions(child.instructions) : null,
      modules: list(child.modules), capabilities: list(child.capabilities), tools: list(child.tools),
      model: model(child.model), budgets: { ...(child.budgets ?? {}) }, report: child.report ?? '',
      output: contract(child.output, `agent ${input.id} child ${name} output`),
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
    output: contract(input.output, `agent ${input.id} output`),
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

/** Session creation on a served definition; the definition and its revision come from the runtime. */
export type RuntimeSessionParams = Pick<CreateSessionParams, 'cwd'> & Partial<Omit<CreateSessionParams, 'cwd' | 'definition' | 'kind'>>;

/** A session on a served definition: run() returns a turn typed by the agent's output contract. */
export class AgentSession<Output = unknown> extends Session {
  override run(input: string | SubmitPayload, options: RunOptions = {}): Turn<Output> {
    return new Turn<Output>(this, typeof input === 'string' ? { text: input } : input, options);
  }
}

/**
 * A served agent: this process answers its tools and hooks for one definition
 * revision, and the sessions created here pin that revision, so a runtime
 * never drives a session whose tools it does not serve.
 */
export interface AgentRuntime<Output = unknown> {
  readonly definition: string;
  readonly revision: string;
  /** Current lease generation; a reconnect re-binds and changes it. */
  readonly generation: string;
  /** Tool and hook handlers currently running. */
  readonly active: number;
  readonly sessions: {
    /** Create a session on this definition and return it once the daemon has admitted it and it pins this revision. */
    create(params: RuntimeSessionParams, options?: CommandOptions): Promise<AgentSession<Output>>;
    /** Open an existing session after checking that it pins this definition and revision. */
    open(rootId: string, options?: CallOptions): Promise<AgentSession<Output>>;
  };
  /** Stop serving. Running handlers are aborted; later invocations fail fast. Sessions outlive the runtime. */
  close(): void;
  /** Settles when serving stops. */
  readonly done: Promise<void>;
}
/** The pre-runtime name; kept as an alias for one release. */
export type Executor = AgentRuntime;

/** Registry operations. Registration is idempotent on content: the same document yields the same revision. */
export class Agents {
  constructor(private readonly client: WhipClient) {}
  /**
   * Register the agent, bind this connection as its executor, and run its tool
   * handlers until close(). Bind before creating sessions: a tool call with no
   * executor fails after a short wait. The executor re-binds after a reconnect
   * and drains invocations that were pending for it.
   */
  async serve<Output>(agent: AgentDefinition<Output>, options: ServeOptions = {}): Promise<AgentRuntime<Output>> {
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
      const definition = agent.handlers.get(invocation.tool);
      if (!definition) { await settle(invocation, { error: `no handler for tool ${invocation.tool}` }); return; }
      const controller = new AbortController();
      running.set(invocation.invocation_id, controller);
      const deadline = Number(invocation.deadline_millis);
      const timer = setTimeout(() => controller.abort(new WhipError('timeout', 'Tool deadline passed')), Math.max(0, deadline - Date.now()));
      // The daemon handles requests concurrently, so progress reports are
      // sent one at a time, each acknowledged before the next, and the result
      // is posted only after the last report landed. Order is preserved and no
      // report arrives after the result and gets rejected as late.
      let reports: Promise<unknown> = Promise.resolve();
      const context: ToolContext = {
        invocationId: invocation.invocation_id, rootId: invocation.root_id, agentId: invocation.agent_id, turnId: invocation.turn_id,
        deadline, signal: controller.signal,
        progress: text => {
          if (controller.signal.aborted) return;
          reports = reports.then(() => client.call('tool.progress', { invocation_id: invocation.invocation_id, generation: invocation.generation, text })).catch(() => {});
        },
      };
      try {
        // The daemon validated the input against the same JSON Schema; the
        // library's own check adds refinements the schema cannot express and
        // applies its defaults and transforms.
        let input: unknown = invocation.input ?? {};
        if (definition.validate && isStandardSchema(definition.input)) {
          const checked = await validateWith(definition.input, input);
          if (checked.issues) { await settle(invocation, { error: `tool ${invocation.tool} rejected its input: ${formatIssues(checked.issues)}` }); return; }
          input = checked.value;
        }
        let output: unknown = await definition.execute(input, context);
        if (definition.validate && isStandardSchema(definition.output)) {
          const checked = await validateWith(definition.output, output);
          if (checked.issues) {
            await reports;
            if (!controller.signal.aborted) await settle(invocation, { error: `tool ${invocation.tool} returned a value that does not match its output schema (the handler already ran): ${formatIssues(checked.issues)}` });
            return;
          }
          output = checked.value;
        }
        await reports;
        if (!controller.signal.aborted) await settle(invocation, { output: output === undefined ? null : output });
      } catch (error) {
        await reports;
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
    // Creation resolves the definition's latest revision; a registration that
    // raced ahead would leave the session pinned to tools this process does
    // not serve, so both paths read the pin back before handing out a session.
    const pinned = async (session: AgentSession<Output>, callOptions: CallOptions = {}): Promise<AgentSession<Output>> => {
      const snapshot = await session.snapshot(callOptions);
      if (snapshot.meta.definition !== definition || snapshot.meta.definition_revision !== revision) {
        throw new WhipError('conflict', `session ${session.rootId} runs ${snapshot.meta.definition || 'coding'}@${snapshot.meta.definition_revision.slice(0, 12) || 'built-in'}, not ${definition}@${revision.slice(0, 12)} served here`);
      }
      return session;
    };
    const sessions: AgentRuntime<Output>['sessions'] = {
      create: async (params, commandOptions = {}) => {
        const outcome = await client.sessions.create({ ...params, definition }, commandOptions).result();
        if (outcome.status !== 'succeeded' || !outcome.result) throw new WhipError('execution_failed', `session creation ${outcome.status}: ${outcome.failure?.message ?? 'no root was returned'}`, { cause: outcome.failure ?? undefined });
        return pinned(new AgentSession<Output>(client, outcome.result.root_id));
      },
      open: (rootId, callOptions = {}) => pinned(new AgentSession<Output>(client, rootId), callOptions),
    };
    return { definition, revision, get generation() { return generation; }, get active() { return running.size; }, sessions, close, done };
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
