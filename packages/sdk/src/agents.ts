import { assertValid } from '@whip/protocol';
import type { Admission, DefinitionDocument, DefinitionRef, ExecutorEvent, Operations } from '@whip/protocol';
import type { Client } from './index.js';
import { Session } from './session.js';
import type { RecoveryJournal } from './command.js';
import type { CallOptions } from './wire.js';
import type { DuplexTransport } from './executors.js';
import { serve } from './serve.js';
import { freeze } from './value.js';
import { describesObject, formatIssues, toJsonSchema, validateWith } from './schema.js';
import type { InferInput, InferOutput, Schema } from './schema.js';
export type { InferInput, InferOutput, JsonSchema, Schema, StandardSchemaWithJSON } from './schema.js';
export type Invocation = NonNullable<ExecutorEvent['invocation']>;
export type AgentOutput<O> = O extends Schema ? InferOutput<O> : unknown;
export type ToolReturn<O> = O extends Schema ? InferInput<O> : unknown;
export interface HandlerContext {
  readonly invocationID: string;
  readonly sessionID: string;
  readonly turnID: string;
  readonly operationID: string | null;
  readonly origin: Invocation['origin'];
  readonly deadline: number;
  readonly signal: AbortSignal;
}
export interface ToolContext extends HandlerContext {
  /** Await each bounded progress acknowledgement before issuing another. */
  progress(text: string): Promise<void>;
}
type ToolSpec = NonNullable<DefinitionDocument['defaults']['tools']>[string];
export interface ToolOptions<I extends Schema, O extends Schema | undefined = undefined> {
  name: string; description: string; input: I;
  /** execute returns this schema's input; validation transforms it once for the wire. */
  output?: O; timeoutMs?: number;
  execute(input: InferOutput<I>, context: ToolContext): ToolReturn<O> | Promise<ToolReturn<O>>;
}
export interface ToolDefinition<I extends Schema = Schema, O extends Schema | undefined = Schema | undefined> {
  readonly name: string; readonly spec: ToolSpec; readonly input: I; readonly output?: O;
  execute(input: InferOutput<I>, context: ToolContext): ToolReturn<O> | Promise<ToolReturn<O>>;
}
export function tool<I extends Schema, O extends Schema | undefined = undefined>(options: ToolOptions<I, O>): ToolDefinition<I, O> {
  if (!/^[a-z][a-z0-9_]{0,63}$/.test(options.name) || ['constructor', 'prototype', '__proto__'].includes(options.name)) throw new TypeError('Invalid custom tool name');
  if (typeof options.execute !== 'function') throw new TypeError('Tool execute handler is required');
  const input = toJsonSchema(options.input, 'input', `Tool ${options.name}`);
  if (!describesObject(input)) throw new TypeError('Tool input must describe an object');
  const timeout = options.timeoutMs ?? 0;
  if (!Number.isSafeInteger(timeout) || timeout < 0 || timeout > 900_000) throw new RangeError('Tool timeout must be within 0..900000 ms');
  return Object.freeze({ name: options.name, input: options.input, output: options.output, execute: options.execute,
    spec: freeze({ description: options.description, timeout_millis: timeout, input_schema: input, output_schema: options.output === undefined ? null : toJsonSchema(options.output, 'output', `Tool ${options.name}`) }) as ToolSpec });
}
export type HookName = 'before_tool' | 'before_spawn' | 'turn_start';
export interface HookContext extends HandlerContext { readonly permissionMode: string }
export interface BeforeToolEvent extends HookContext { readonly operation: string; readonly arguments: Record<string, unknown> }
export interface BeforeSpawnEvent extends HookContext { readonly spawn: { request: Record<string, unknown>; resolved: Record<string, unknown> } }
export interface TurnStartEvent extends HookContext { readonly input: string }
export interface HookResult { decision?: 'allow' | 'deny'; reason?: string; arguments?: Record<string, unknown>; spawn?: Record<string, unknown>; context?: string }
export type HookHandler<E> = (event: E) => HookResult | void | Promise<HookResult | void>;
interface HookOptions<E> { handler: HookHandler<E>; optional?: boolean; timeoutMs?: number }
export interface HooksInput {
  beforeTool?: HookHandler<BeforeToolEvent> | (HookOptions<BeforeToolEvent> & { operations?: string[] });
  beforeSpawn?: HookHandler<BeforeSpawnEvent> | HookOptions<BeforeSpawnEvent>;
  turnStart?: HookHandler<TurnStartEvent> | HookOptions<TurnStartEvent>;
}
export interface AgentInput<O extends Schema | undefined = undefined> {
  id: string; name: string;
  /** Canonical v4 fields. Omitted fields inherit; explicit empty collections clear. */
  defaults?: Omit<DefinitionDocument['defaults'], 'tools' | 'hooks' | 'output'>;
  tools?: readonly ToolDefinition[]; hooks?: HooksInput;
  /** Provider bytes follow the input schema; typed result applies its transform once. */
  output?: O;
}
export interface AgentDefinition<Output = unknown> {
  readonly document: DefinitionDocument;
  readonly handlers: Readonly<Record<string, ToolDefinition>>;
  readonly hooks: Readonly<Partial<Record<HookName, HookHandler<never>>>>;
  readonly output?: Schema;
  readonly outputType?: Output;
}
export function defineAgent<O extends Schema | undefined = undefined>(input: AgentInput<O>): AgentDefinition<AgentOutput<O>> {
  const declarations: NonNullable<DefinitionDocument['defaults']['tools']> = Object.create(null);
  const handlers: Record<string, ToolDefinition> = Object.create(null);
  for (const entry of input.tools ?? []) {
    if (Object.hasOwn(handlers, entry.name)) throw new TypeError('Duplicate tool declaration');
    declarations[entry.name] = entry.spec; handlers[entry.name] = entry;
  }
  const hooks: Partial<Record<HookName, HookHandler<never>>> = {};
  const hookSpecs: NonNullable<DefinitionDocument['defaults']['hooks']> = {};
  const declare = <E>(name: HookName, value?: HookHandler<E> | (HookOptions<E> & { operations?: string[] })) => {
    if (!value) return;
    const options = typeof value === 'function' ? { handler: value } : value;
    if (typeof options.handler !== 'function') throw new TypeError('Hook handler is required');
    const timeout = options.timeoutMs ?? 0;
    if (!Number.isSafeInteger(timeout) || timeout < 0 || timeout > 60_000) throw new RangeError('Hook timeout must be within 0..60000 ms');
    hooks[name] = options.handler;
    hookSpecs[name] = { optional: options.optional ?? false, timeout_millis: timeout, operations: options.operations ?? null };
  };
  declare('before_tool', input.hooks?.beforeTool); declare('before_spawn', input.hooks?.beforeSpawn); declare('turn_start', input.hooks?.turnStart);
  const output = input.output === undefined ? undefined : toJsonSchema(input.output, 'input', 'Agent output');
  if (output && !describesObject(output)) throw new TypeError('Agent output must describe an object');
  const document = JSON.parse(JSON.stringify({ id: input.id, name: input.name, defaults: { ...input.defaults,
    ...(input.tools === undefined ? {} : { tools: declarations }), ...(input.hooks === undefined ? {} : { hooks: hookSpecs }), ...(output === undefined ? {} : { output: { schema: output } }) } })) as DefinitionDocument;
  assertValid('DefinitionDocument', document);
  return Object.freeze({ document: freeze(document) as DefinitionDocument, handlers: Object.freeze(handlers), hooks: Object.freeze(hooks), output: input.output });
}
/** Typed JSON deliberately rejects unsafe integer coercion. Low-level executor
 * and output methods retain the original base64 bytes for exact-number callers. */
export function decodeJSON(encoded: string): unknown {
  const binary = atob(encoded);
  const text = new TextDecoder('utf-8', { fatal: true }).decode(Uint8Array.from(binary, character => character.charCodeAt(0)));
  return JSON.parse(text, (_key, value: unknown) => {
    if (typeof value === 'number' && (!Number.isFinite(value) || Number.isInteger(value) && !Number.isSafeInteger(value))) throw new TypeError('Typed JSON contains an unsafe number; use the raw byte API');
    return value;
  });
}
export class AgentSession<Output = unknown> extends Session {
  constructor(client: Client, id: string, private readonly outputSchema?: Schema) { super(client, id); }
  /** Inert until send/result; the command and its exact recovery record stay public. */
  run(parts: Operations['sessions.submit']['params']['parts'], requestID: string, options: { journal?: RecoveryJournal } = {}) {
    const command = this.submission(parts, requestID, options);
    return { command, send: (callOptions: CallOptions = {}) => command.send(callOptions), result: async (callOptions: CallOptions = {}): Promise<{ admission: Admission; output: Output | null }> => {
      await command.send(callOptions);
      const admission = await command.wait(callOptions);
      if (admission.turn?.state !== 'succeeded') return { admission, output: null };
      const result = await this.turns.output(admission.turn.id, callOptions);
      if (!result.output) return { admission, output: null };
      if (result.output.turn_id !== admission.turn.id) throw new TypeError('Structured output belongs to another turn');
      const checked = await validateWith(this.outputSchema, decodeJSON(result.output.data_base64));
      if (checked.issues) throw new TypeError('Agent output validation failed: ' + formatIssues(checked.issues));
      return { admission, output: checked.value as Output };
    } };
  }
}
export interface ServeOptions {
  /** Ownership transfers to serve, including cleanup when setup fails. */
  transport: DuplexTransport; signal?: AbortSignal;
  /** Default 16, maximum 128; there is no waiting callback queue. */
  maxConcurrent?: number;
  /** Default 4096, maximum 65536. Exhaustion closes this lease; IDs are never evicted or replayed. */
  maxInvocations?: number;
}
export interface AgentRuntime<Output = unknown> {
  readonly definition: DefinitionRef; readonly generation: string; readonly active: number;
  /** Rejects when observation, delivery, validation or a resource bound closes the lease. */
  readonly done: Promise<void>;
  readonly sessions: {
    create(params: Omit<Operations['trees.create']['params'], 'definition'>, options?: CallOptions): Promise<AgentSession<Output>>;
    open(sessionID: string, options?: CallOptions): Promise<AgentSession<Output>>;
  };
  /** Abort callbacks and join them. JavaScript handlers must cooperate with signal. */
  close(): Promise<void>;
}
export class Agents {
  constructor(readonly client: Client) {}
  register(agent: AgentDefinition | DefinitionDocument, options: CallOptions = {}) { return this.client.call('definitions.register', 'document' in agent ? agent.document : agent, options); }
  get(ref: DefinitionRef, options: CallOptions = {}) { return this.client.call('definitions.get', ref, options); }
  list(params: Operations['definitions.list']['params'] = { limit: 100 }, options: CallOptions = {}) { return this.client.call('definitions.list', params, options); }
  serve<Output>(agent: AgentDefinition<Output>, options: ServeOptions): Promise<AgentRuntime<Output>> { return serve(this, agent, options); }
}
