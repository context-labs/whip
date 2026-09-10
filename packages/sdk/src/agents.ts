import type {
  DefinitionList, DefinitionParams, DefinitionRecord, DefinitionRegisterParams, DefinitionRegisterResult,
} from '@whip/protocol';
import type { CallOptions, WhipClient } from './client.js';

/** The wire document the daemon validates and stores. Generated from the Go definition. */
export type Definition = DefinitionRegisterParams['definition'];
export type ToolSpec = NonNullable<Definition['tools']>[number];
export type ChildDefinition = Definition['children'][string];

/** Context handed to a tool handler for one invocation. */
export interface ToolContext {
  readonly invocationId: string;
  readonly rootId: string;
  readonly agentId: string;
  readonly turnId: string;
  readonly deadline: number;
  readonly signal: AbortSignal;
  /** Report intermediate output; the daemon streams it as tool output. */
  progress(text: string): void;
}
export type ToolHandler<I = Record<string, unknown>> = (input: I, context: ToolContext) => Promise<unknown> | unknown;

export interface ToolDefinition<I = Record<string, unknown>> {
  readonly spec: ToolSpec;
  readonly handler: ToolHandler<I>;
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
}

/** An authored agent: the document the daemon stores plus the handlers an executor serves. */
export interface AgentDefinition {
  readonly document: Definition;
  readonly handlers: ReadonlyMap<string, ToolHandler>;
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
  };
  return { document: Object.freeze(document), handlers };
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

/** Registry operations. Registration is idempotent on content: the same document yields the same revision. */
export class Agents {
  constructor(private readonly client: WhipClient) {}
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
