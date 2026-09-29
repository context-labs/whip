// An incident commander built from every primitive the SDK offers. The
// definition is data the runtime stores; the tool handlers and hooks run in the
// process that calls client.agents.serve. Everything below is exercised end to
// end against a live runtime by incident-commander.acceptance.mjs.
import { defineAgent, tool, type BeforeToolEvent, type BeforeSpawnEvent, type TurnStartEvent, type HookResult, type ToolContext } from '@whip/sdk/agents';
import type { DefinitionDocument, DefinitionRef } from '@whip/sdk';
import { z } from 'zod';

const Incident = z.object({ id: z.string(), service: z.string(), title: z.string(), status: z.enum(['open', 'mitigated', 'closed']), severity: z.union([z.literal(1), z.literal(2), z.literal(3)]) });
export type Incident = z.infer<typeof Incident>;

/** Everything the handlers touch, exposed so tests can observe the process side. */
export interface CommanderState {
  readonly incidents: Map<string, Incident>;
  readonly timeline: { incident: string; entry: string; invocationID: string }[];
  readonly pages: Map<string, { team: string; message: string; sessionID: string }>;
  readonly auditLog: { sessionID: string; operation: string; arguments: Record<string, unknown> }[];
  readonly turnStarts: { sessionID: string; turnID: string; input: string }[];
  readonly toolCalls: { tool: string; sessionID: string; turnID: string; invocationID: string }[];
  onCall: string;
}

export interface CommanderOptions {
  /** Registered id; the acceptance test uses its own so a stale registration cannot shadow it. */
  id?: string;
  model?: NonNullable<DefinitionDocument['defaults']['model']>;
}

export function createIncidentCommander(options: CommanderOptions = {}) {
  const state: CommanderState = {
    incidents: new Map<string, Incident>([
      ['INC-101', { id: 'INC-101', service: 'auth', title: 'Login page times out on mobile', status: 'open', severity: 2 }],
      ['INC-102', { id: 'INC-102', service: 'billing', title: 'Invoices double-charged after refund', status: 'mitigated', severity: 1 }],
      ['INC-103', { id: 'INC-103', service: 'auth', title: 'Password reset emails delayed', status: 'closed', severity: 3 }],
    ]),
    timeline: [], pages: new Map(), auditLog: [], turnStarts: [], toolCalls: [], onCall: 'Sam',
  };
  const record = (tool: string, context: ToolContext) => {
    state.toolCalls.push({ tool, sessionID: context.sessionID, turnID: context.turnID, invocationID: context.invocationID });
    if (state.toolCalls.length > 128) state.toolCalls.shift();
  };

  const searchIncidents = tool({
    name: 'search_incidents',
    description: 'Search incidents by free text over id, service, and title; optionally filter by status.',
    input: z.object({ query: z.string(), status: z.enum(['open', 'mitigated', 'closed']).optional() }),
    output: z.array(Incident),
    execute: async ({ query, status }, context) => {
      record('search_incidents', context);
      const needle = query.toLowerCase();
      return [...state.incidents.values()]
        .filter(incident => !status || incident.status === status)
        .filter(incident => `${incident.id} ${incident.service} ${incident.title}`.toLowerCase().includes(needle));
    },
  });

  // Runbooks are bounded JSON. Store large results explicitly with artifacts.put,
  // then inspect owner-scoped byte slices with artifacts.read.
  const fetchRunbook = tool({
    name: 'fetch_runbook',
    description: 'Fetch a bounded operational runbook; store it with artifacts.put for retained excerpts.',
    input: z.object({ service: z.string() }),
    output: z.object({ service: z.string(), runbook: z.string() }),
    execute: async ({ service }, context) => {
      record('fetch_runbook', context);
      const steps = Array.from({ length: 400 }, (_, index) => `Step ${index + 1}: check ${service} component ${index % 7} and record the outcome in the incident timeline.`);
      return { service, runbook: `RUNBOOK ${service.toUpperCase()}\n${steps.join('\n')}` };
    },
    timeoutMs: 20_000,
  });

  // The business ledger is keyed by invocation ID; disconnected executors never replay it.
  const pageOncall = tool({
    name: 'page_oncall',
    description: 'Page the on-call engineer for a team with a message.',
    input: z.object({ team: z.string(), message: z.string().max(500) }),
    output: z.object({ paged: z.literal(true), team: z.string(), acknowledgedBy: z.string(), invocationID: z.string() }),
    execute: async ({ team, message }, context) => {
      record('page_oncall', context);
      await context.progress(`paging ${team} on-call`);
      if (!state.pages.has(context.invocationID) && state.pages.size >= 128) throw new Error('Example page ledger is full');
      if (!state.pages.has(context.invocationID)) state.pages.set(context.invocationID, { team, message, sessionID: context.sessionID });
      await context.progress(`page acknowledged by ${state.onCall}`);
      return { paged: true as const, team, acknowledgedBy: state.onCall, invocationID: context.invocationID };
    },
  });

  const recordTimeline = tool({
    name: 'record_timeline',
    description: 'Append an entry to an incident timeline.',
    input: z.object({ incident: z.string(), entry: z.string() }),
    output: z.object({ incident: z.string(), entries: z.number().int() }),
    execute: async ({ incident, entry }, context) => {
      record('record_timeline', context);
      if (!state.incidents.has(incident)) throw new Error(`unknown incident ${incident}`);
      if (state.timeline.length >= 128) throw new Error('Example timeline is full');
      state.timeline.push({ incident, entry, invocationID: context.invocationID });
      return { incident, entries: state.timeline.filter(item => item.incident === incident).length };
    },
  });

  const beforeTool = async ({ operation, arguments: args, sessionID }: BeforeToolEvent): Promise<HookResult | void> => {
    state.auditLog.push({ sessionID, operation, arguments: args });
    if (state.auditLog.length > 128) state.auditLog.shift();
    if (operation === 'shell.run' && /\brm\s+-rf\b|--force/.test(String(args.command))) return { decision: 'deny', reason: 'destructive shell commands are not allowed during incidents' };
    if (operation === 'files.read' && /(^|\/)\.env$/.test(String(args.path))) return { arguments: { ...args, path: `${String(args.path)}.example` }, reason: 'secrets are redacted' };
    if (operation === 'tools.page_oncall' && String(args.message).length > 200) return { arguments: { ...args, message: `${String(args.message).slice(0, 197)}...` }, reason: 'pages are kept short' };
  };
  const turnStart = async ({ sessionID, turnID, input }: TurnStartEvent) => {
    state.turnStarts.push({ sessionID, turnID, input }); if (state.turnStarts.length > 128) state.turnStarts.shift();
    const open = [...state.incidents.values()].filter(incident => incident.status === 'open').length;
    return { context: `On call: ${state.onCall}. Open incidents: ${open}. Timeline entries: ${state.timeline.length}.` };
  };
  const instructions = (text: string, project_files: string[] = []) => ({ project_root: null, text, project_files, discover_skills: false, standing_instructions: false, skill_roots: [] });
  const hooks = { beforeTool: { timeoutMs: 10_000, handler: beforeTool }, beforeSpawn: async (): Promise<HookResult> => ({ decision: 'deny', reason: 'investigators and scribes do not delegate further' }), turnStart };
  const common = { model: options.model, automatic_title: false, goals_enabled: false, children: {}, mcp_servers: { all: false, servers: [] } };
  const investigator = defineAgent({ id: `${options.id ?? 'incident-commander'}-investigator`, name: 'Investigator',
    defaults: { ...common, instructions: instructions('Investigate one incident and report findings to your parent; change nothing.'), modules: ['context', 'files', 'state', 'mail'], report_mode: 'message' },
    tools: [searchIncidents, fetchRunbook], hooks, output: null,
  });
  const scribe = defineAgent({ id: `${options.id ?? 'incident-commander'}-scribe`, name: 'Scribe',
    defaults: { ...common, instructions: instructions('Keep the incident timeline accurate and terse.'), modules: ['context', 'state', 'artifacts', 'mail'], report_mode: 'inline' },
    tools: [recordTimeline], hooks, output: null,
  });
  const agent = (children: { investigator: DefinitionRef; scribe: DefinitionRef }) => {
    const beforeSpawn = async ({ spawn }: BeforeSpawnEvent): Promise<HookResult | void> => {
      const overrides = spawn.request.overrides;
      if (overrides && typeof overrides === 'object' && 'modules' in overrides && Array.isArray(overrides.modules) && overrides.modules.includes('shell'))
        return { decision: 'deny', reason: 'children may not hold shell' };
      if (!spawn.request.definition) return { spawn: { ...spawn.request, definition: children.investigator }, reason: 'unnamed children investigate' };
      const configuration = spawn.resolved.configuration;
      if (configuration && typeof configuration === 'object' && 'modules' in configuration && Array.isArray(configuration.modules) && configuration.modules.includes('shell'))
        return { decision: 'deny', reason: 'children may not hold shell' };
    };
    return defineAgent({
      id: options.id ?? 'incident-commander', name: 'Incident commander',
      defaults: {
        instructions: instructions([
          'You are the incident commander for a small platform team. Coordinate; do not fix code yourself.',
          'Look incidents up before describing them; never guess ids or status.',
          'Read runbooks before recommending steps. Use artifacts.put/read for bounded retained excerpts.',
          'Delegate investigation to an investigator and note-taking to a scribe. Pass only explicitly delegated grant IDs.',
          'Record every decision. Page on-call only for severity 1 or 2.',
          'Ask the user with user.ask before any action that changes production.',
        ].join('\n'), ['RUNBOOK.md', 'AGENTS.md']),
        modules: ['context', 'files', 'shell', 'state', 'artifacts', 'mail', 'agents', 'models', 'mcp', 'permissions', 'user'],
        model: options.model, compaction: { model: null, threshold_percent: 75 },
        mcp_servers: { all: false, servers: ['statuspage'] }, children, automatic_title: true, goals_enabled: false,
      },
      tools: [searchIncidents, fetchRunbook, pageOncall, recordTimeline],
      hooks: { ...hooks, beforeSpawn },
    });
  };
  return { agent, investigator, scribe, state, tools: { searchIncidents, fetchRunbook, pageOncall, recordTimeline }, hooks: { beforeTool, turnStart } };
}

// const example = createIncidentCommander({ model: { provider: 'your-provider', name: 'your-model', effort: '' } });
// Serve investigator and scribe on separate executorSocket(socket) peers first.
// Serve example.agent({ investigator: investigator.definition, scribe: scribe.definition }).
// Root declarations do not grant operations. Grant exact custom-tool resources and
// workspace scopes explicitly; pass only the intended parent grant IDs when spawning.
// Child budgets are admission-time bounds: [{ kind: 'model_calls', limit: '8' }].
// Token budgets additionally require a known conservative input-token bound.
// Closing each returned runtime joins cooperative callbacks and never rebinds.
