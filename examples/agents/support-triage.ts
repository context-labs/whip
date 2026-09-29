// Typed tools and hooks execute only while their exact definition's executor is
// connected. Child definitions are registered first and referenced immutably.
import type { DefinitionRef } from '@whip/sdk';
import { defineAgent, tool, type BeforeToolEvent, type BeforeSpawnEvent, type HookResult } from '@whip/sdk/agents';
import { z } from 'zod';

const Ticket = z.object({ id: z.string(), title: z.string(), status: z.enum(['open', 'closed']), customer: z.string() });
export type Ticket = z.infer<typeof Ticket>;
export const tickets = new Map<string, Ticket>([
  ['42', { id: '42', title: 'Login page times out on mobile', status: 'open', customer: 'acme' }],
  ['43', { id: '43', title: 'Export button disabled after refresh', status: 'closed', customer: 'globex' }],
]);
export const escalations: { id: string; team: string; note: string }[] = [];
export const audit: { sessionID: string; operation: string }[] = [];
export const roster = { current: async () => 'Sam' };

export const lookupTicket = tool({
  name: 'lookup_ticket', description: 'Fetch a support ticket by id. Returns its title, status, and customer.',
  input: z.object({ id: z.string() }), output: Ticket, timeoutMs: 30_000,
  execute: async ({ id }, context) => {
    await context.progress(`looking up ${id}`); // Await the bounded acknowledgement.
    const ticket = tickets.get(id);
    if (!ticket) throw new Error(`ticket ${id} not found`);
    return ticket;
  },
});
export const escalate = tool({
  name: 'escalate', description: 'Escalate a ticket to an engineering team with a one-line note.',
  input: z.object({ id: z.string(), team: z.enum(['auth', 'billing', 'mobile']), note: z.string().max(200) }),
  output: z.object({ escalated: z.literal(true), ticket: z.string(), team: z.string(), invocation: z.string() }),
  execute: async ({ id, team, note }, context) => {
    if (!tickets.has(id)) throw new Error(`ticket ${id} not found`);
    // Business idempotency is explicit; executor disconnect never replays callbacks.
    if (!escalations.some(entry => entry.id === id && entry.team === team)) escalations.push({ id, team, note });
    return { escalated: true as const, ticket: id, team, invocation: context.invocationID };
  },
});
export const beforeTool = async ({ operation, arguments: args, sessionID }: BeforeToolEvent): Promise<HookResult | void> => {
  audit.push({ sessionID, operation }); if (audit.length > 128) audit.shift();
  if (operation === 'shell.run' && /\brm\s+-rf\b/.test(String(args.command)))
    return { decision: 'deny', reason: 'destructive shell commands are not allowed' };
  if (operation === 'files.read' && /(^|\/)\.env$/.test(String(args.path)))
    return { arguments: { ...args, path: `${String(args.path)}.example` }, reason: 'secrets are redacted' };
  if (operation === 'tools.escalate' && tickets.get(String(args.id))?.status === 'closed')
    return { decision: 'deny', reason: 'closed tickets are not escalated' };
};
export const turnStart = async () => ({ context: `On call: ${await roster.current()}. Open tickets: ${[...tickets.values()].filter(ticket => ticket.status === 'open').length}. Escalations so far: ${escalations.length}.` });
const instructions = (text: string) => ({ project_root: null, text, project_files: [], discover_skills: false, standing_instructions: false, skill_roots: [] });
export const supportResearcher = defineAgent({
  id: 'support-researcher', name: 'Support researcher',
  defaults: { instructions: instructions('Research one question about a ticket and report back in three lines.'), modules: ['context', 'files', 'state'], report_mode: 'message', automatic_title: false, goals_enabled: false, children: {} },
  output: null, tools: [lookupTicket], hooks: { beforeTool: { timeoutMs: 10_000, handler: beforeTool }, beforeSpawn: async () => ({ decision: 'deny', reason: 'researchers do not delegate further' }), turnStart: { optional: true, handler: turnStart } },
});
export function supportTriage(researcher: DefinitionRef) {
  const beforeSpawn = async ({ spawn }: BeforeSpawnEvent): Promise<HookResult | void> => {
    // Select a pre-registered narrowed definition. Host validation still rechecks
    // bindings and every delegated grant; a hook cannot create authority.
    if (!spawn.request.definition) return { spawn: { ...spawn.request, definition: researcher }, reason: 'unnamed children run as researcher' };
    const ref = spawn.request.definition;
    if (!ref || typeof ref !== 'object' || !('id' in ref) || !('revision' in ref) || ref.id !== researcher.id || ref.revision !== researcher.revision)
      return { decision: 'deny', reason: 'children may not escalate' };
  };
  return defineAgent({
    id: 'support-triage', name: 'Support triage',
    defaults: {
      instructions: instructions(['You triage support tickets for a small engineering team.',
        'Look a ticket up with tools.lookup_ticket before describing it; never guess a title or status.',
        'Escalate only open tickets, and only once you can name the responsible team.',
        'Ask the user with user.ask when customer impact is unclear.'].join('\n')),
      modules: ['context', 'files', 'shell', 'state', 'user', 'agents'],
      children: { researcher }, automatic_title: true, goals_enabled: false,
    },
    tools: [lookupTicket, escalate],
    output: z.object({ ticket: z.string(), summary: z.string(), escalatedTo: z.enum(['auth', 'billing', 'mobile']).nullable() }),
    hooks: { beforeTool: { timeoutMs: 10_000, handler: beforeTool }, beforeSpawn, turnStart: { optional: true, handler: turnStart } },
  });
}

// const child = await client.agents.serve(supportResearcher, { transport: await executorSocket(socket) });
// const runtime = await client.agents.serve(supportTriage(child.definition), { transport: await executorSocket(socket) });
// const session = await runtime.sessions.create({ creation_id: crypto.randomUUID(),
//   working_directory: '/srv/support', metadata: { title: null, pinned: false, archived: false }, overrides: {} });
// const turn = session.run([{ type: 'text', text: 'Triage ticket 42.' }], crypto.randomUUID());
// await turn.send(); // Save turn.command.record before sending when recovery is required.
// const result = await turn.result(); // { admission, output }, typed by the output schema.
// await runtime.close(); await child.close(); // Joins callbacks; never rebinds or replays.
