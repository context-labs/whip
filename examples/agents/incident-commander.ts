// An incident commander built from every primitive the SDK offers. The
// definition is data the daemon stores; the tool handlers and hooks run in the
// process that calls client.agents.serve. Everything below is exercised end to
// end against a live daemon by incident-commander.acceptance.mjs.
import { defineAgent, tool, type AgentDefinition, type ModelInput } from '@whip/sdk/agents';
import { z } from 'zod';

const Incident = z.object({ id: z.string(), service: z.string(), title: z.string(), status: z.enum(['open', 'mitigated', 'closed']), severity: z.union([z.literal(1), z.literal(2), z.literal(3)]) });
export type Incident = z.infer<typeof Incident>;

/** Everything the handlers touch, exposed so tests can observe the process side. */
export interface CommanderState {
  readonly incidents: Map<string, Incident>;
  readonly timeline: { incident: string; entry: string; invocationId: string }[];
  readonly pages: Map<string, { team: string; message: string; agentId: string }>;
  readonly auditLog: { agentId: string; operation: string; arguments: Record<string, unknown> }[];
  readonly turnStarts: { agentId: string; turnId: string; input: string }[];
  readonly toolCalls: { tool: string; agentId: string; rootId: string; turnId: string; invocationId: string }[];
  onCall: string;
}

export interface CommanderOptions {
  /** Registered id; the acceptance test uses its own so a stale registration cannot shadow it. */
  id?: string;
  model?: ModelInput;
}

export function createIncidentCommander(options: CommanderOptions = {}): { agent: AgentDefinition; state: CommanderState } {
  const state: CommanderState = {
    incidents: new Map<string, Incident>([
      ['INC-101', { id: 'INC-101', service: 'auth', title: 'Login page times out on mobile', status: 'open', severity: 2 }],
      ['INC-102', { id: 'INC-102', service: 'billing', title: 'Invoices double-charged after refund', status: 'mitigated', severity: 1 }],
      ['INC-103', { id: 'INC-103', service: 'auth', title: 'Password reset emails delayed', status: 'closed', severity: 3 }],
    ]),
    timeline: [], pages: new Map(), auditLog: [], turnStarts: [], toolCalls: [], onCall: 'Sam',
  };
  const record = (tool: string, context: { agentId: string; rootId: string; turnId: string; invocationId: string }) =>
    state.toolCalls.push({ tool, agentId: context.agentId, rootId: context.rootId, turnId: context.turnId, invocationId: context.invocationId });

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

  // Runbooks are large: the daemon returns them to the cell as a content handle
  // with a preview, and the cell reads bounded slices with context.read.
  const fetchRunbook = tool({
    name: 'fetch_runbook',
    description: 'Fetch the operational runbook for a service. Large; returned as a handle.',
    input: z.object({ service: z.string() }),
    output: z.object({ service: z.string(), runbook: z.string() }),
    execute: async ({ service }, context) => {
      record('fetch_runbook', context);
      const steps = Array.from({ length: 400 }, (_, index) => `Step ${index + 1}: check ${service} component ${index % 7} and record the outcome in the incident timeline.`);
      return { service, runbook: `RUNBOOK ${service.toUpperCase()}\n${steps.join('\n')}` };
    },
    timeoutMs: 20_000,
  });

  // A side effect keyed by invocation id: a retried invocation pages once.
  const pageOncall = tool({
    name: 'page_oncall',
    description: 'Page the on-call engineer for a team with a message.',
    input: z.object({ team: z.string(), message: z.string().max(500) }),
    output: z.object({ paged: z.literal(true), team: z.string(), acknowledgedBy: z.string(), invocationId: z.string() }),
    execute: async ({ team, message }, context) => {
      record('page_oncall', context);
      context.progress(`paging ${team} on-call`);
      if (!state.pages.has(context.invocationId)) state.pages.set(context.invocationId, { team, message, agentId: context.agentId });
      context.progress(`page acknowledged by ${state.onCall}`);
      return { paged: true as const, team, acknowledgedBy: state.onCall, invocationId: context.invocationId };
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
      state.timeline.push({ incident, entry, invocationId: context.invocationId });
      return { incident, entries: state.timeline.filter(item => item.incident === incident).length };
    },
  });

  const agent = defineAgent({
    id: options.id ?? 'incident-commander',
    instructions: {
      persona: 'You are the incident commander for a small platform team. You coordinate; you do not fix code yourself.',
      rules: [
        'Operating rules:',
        '- Look incidents up with tools.search_incidents before describing them; never guess ids or status.',
        '- Read the runbook with tools.fetch_runbook and context.read before recommending steps.',
        '- Delegate investigation to an investigator child and note-taking to a scribe child; keep your own turns short.',
        '- Record every decision with tools.record_timeline. Page on-call only for severity 1 or 2.',
        '- Ask the user with user.ask before any action that changes production.',
      ].join('\n'),
      projectFiles: ['RUNBOOK.md', 'AGENTS.md'],
      skillDiscovery: false,
      standingInstructions: false,
    },
    modules: ['context', 'files', 'shell', 'state', 'artifacts', 'messages', 'agents', 'models', 'mcp', 'permissions', 'user'],
    capabilities: ['read', 'write', 'shell', 'mcp'],
    model: options.model,
    compaction: { threshold: 0.75 },
    mcp: { servers: ['statuspage'] },
    tools: [searchIncidents, fetchRunbook, pageOncall, recordTimeline],
    children: {
      investigator: {
        instructions: { persona: 'You investigate one incident and report findings to your parent; you change nothing.', skillDiscovery: false },
        modules: ['context', 'files', 'shell', 'state', 'messages'],
        capabilities: ['read'],
        tools: ['search_incidents', 'fetch_runbook'],
        budgets: { tokens: 40_000 },
        report: 'message',
      },
      scribe: {
        instructions: { persona: 'You keep the incident timeline accurate and terse.' },
        modules: ['context', 'state', 'artifacts', 'messages'],
        capabilities: ['read'],
        tools: ['record_timeline'],
        report: 'inline',
      },
    },
    hooks: {
      // Every host operation on the root and its children passes here first.
      beforeTool: { timeoutMs: 10_000, handler: async ({ operation, arguments: args, agentId }) => {
        state.auditLog.push({ agentId, operation, arguments: args });
        if (operation === 'shell.run' && /\brm\s+-rf\b|--force/.test(String(args.command))) return { decision: 'deny', reason: 'destructive shell commands are not allowed during incidents' };
        if (operation === 'files.read' && /(^|\/)\.env$/.test(String(args.path))) return { arguments: { ...args, path: `${String(args.path)}.example` }, reason: 'secrets are redacted' };
        if (operation === 'tools.page_oncall' && String(args.message).length > 200) return { arguments: { ...args, message: `${String(args.message).slice(0, 197)}...` }, reason: 'pages are kept short' };
      } },
      // Children run as the investigator unless the parent chose one; none may hold shell.
      // An unnamed child resolves to the parent's own capabilities, so the shell
      // check looks at what was asked for, and the redirect handles the rest.
      beforeSpawn: async ({ spawn, resolved }) => {
        const askedForShell = spawn.capabilities?.includes('shell') || (spawn.definition !== '' && resolved.capabilities?.includes('shell'));
        if (askedForShell) return { decision: 'deny', reason: 'children may not hold shell' };
        if (!spawn.definition) return { spawn: { ...spawn, definition: 'investigator' }, reason: 'unnamed children investigate' };
      },
      // Live facts for each turn; ephemeral, never in history.
      turnStart: async ({ agentId, turnId, input }) => {
        state.turnStarts.push({ agentId, turnId, input });
        const open = [...state.incidents.values()].filter(incident => incident.status === 'open').length;
        return { context: `On call: ${state.onCall}. Open incidents: ${open}. Timeline entries: ${state.timeline.length}.` };
      },
    },
    surface: { autoTitle: true, goalLoop: false },
  });
  return { agent, state };
}

export const incidentCommander = createIncidentCommander().agent;

// Serve it and start a session that can call it:
//
//   import { createWhipClient } from '@whip/sdk';
//   const client = createWhipClient({ endpoint: 'http://127.0.0.1:8080', clientId: 'incident-commander' });
//   await client.connect();
//   const executor = await client.agents.serve(incidentCommander); // registers, binds tools and hooks, serves until close()
//   const created = await client.sessions.create({ cwd: '/path/to/repo', definition: 'incident-commander' }).result();
