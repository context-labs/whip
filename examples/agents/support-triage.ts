// The support triage agent from the SDK README: two typed tools with output
// schemas, one named child, all three hooks, and an output contract. The tool
// handlers and hooks run in the process that calls client.agents.serve; the
// definition is data the daemon stores. support-triage.test.ts drives it
// through the scripted daemon and support-triage.acceptance.mjs through a
// live one.
import { defineAgent, tool } from '@whip/sdk/agents';
import { z } from 'zod';

const Ticket = z.object({
  id: z.string(),
  title: z.string(),
  status: z.enum(['open', 'closed']),
  customer: z.string(),
});
export type Ticket = z.infer<typeof Ticket>;

export const tickets = new Map<string, Ticket>([
  ['42', { id: '42', title: 'Login page times out on mobile', status: 'open', customer: 'acme' }],
  ['43', { id: '43', title: 'Export button disabled after refresh', status: 'closed', customer: 'globex' }],
]);
export const escalations: { id: string; team: string; note: string }[] = [];
export const audit: { agentId: string; operation: string }[] = [];
export const roster = { current: async () => 'Sam' };

// ---- Tools. Input types are inferred from the schema; the return type must match output.

export const lookupTicket = tool({
  name: 'lookup_ticket',
  description: 'Fetch a support ticket by id. Returns its title, status, and customer.',
  input: z.object({ id: z.string() }),
  output: Ticket,
  execute: async ({ id }, ctx) => {              // id: string
    ctx.progress(`looking up ${id}`);            // streamed to the session while it runs
    const ticket = tickets.get(id);
    if (!ticket) throw new Error(`ticket ${id} not found`);   // the error the model reads
    return ticket;                               // checked against Ticket at compile time and before posting
  },
  timeoutMs: 30_000,
});

export const escalate = tool({
  name: 'escalate',
  description: 'Escalate a ticket to an engineering team with a one-line note.',
  input: z.object({
    id: z.string(),
    team: z.enum(['auth', 'billing', 'mobile']),
    note: z.string().max(200),                  // a constraint JSON Schema expresses; the daemon enforces it
  }),
  output: z.object({ escalated: z.literal(true), ticket: z.string(), team: z.string(), invocation: z.string() }),
  execute: async ({ id, team, note }, ctx) => {
    // ctx.invocationId is the ledger operation id: reuse it to make a retry idempotent.
    if (!escalations.some(e => e.id === id && e.team === team)) escalations.push({ id, team, note });
    return { escalated: true as const, ticket: id, team, invocation: ctx.invocationId };
  },
});

// ---- The definition: data the daemon stores under a content revision.

export const supportTriage = defineAgent({
  id: 'support-triage',
  instructions: {
    persona: 'You triage support tickets for a small engineering team.',
    rules: [
      'Operating rules:',
      '- Look a ticket up with tools.lookup_ticket before describing it; never guess a title or status.',
      '- Escalate only open tickets, and only once you can name the responsible team.',
      '- Ask the user with user.ask when the customer impact is unclear.',
    ].join('\n'),
    skillDiscovery: false,
    standingInstructions: false,
  },
  modules: ['context', 'files', 'shell', 'state', 'user', 'agents'],
  capabilities: ['read', 'shell'],
  tools: [lookupTicket, escalate],

  // What a turn returns. The daemon enforces it and the turn result is typed.
  output: z.object({
    ticket: z.string(),
    summary: z.string(),
    escalatedTo: z.enum(['auth', 'billing', 'mobile']).nullable(),
  }),

  // A named child the model can spawn with agents.spawn(definition="researcher").
  children: {
    researcher: {
      instructions: { persona: 'You research one question about a ticket and report back in three lines.' },
      modules: ['context', 'files', 'state'],
      capabilities: ['read'],
      tools: ['lookup_ticket'],           // may look tickets up, never escalate
      budgets: { tokens: 20_000 },
      report: 'message',
    },
  },

  // Hooks run in this process too. Returning nothing means allow, unchanged.
  hooks: {
    beforeTool: {
      timeoutMs: 10_000,
      handler: async ({ operation, arguments: args, agentId }) => {
        audit.push({ agentId, operation });
        if (operation === 'shell.run' && /\brm\s+-rf\b/.test(String(args.command)))
          return { decision: 'deny', reason: 'destructive shell commands are not allowed' };
        if (operation === 'files.read' && /(^|\/)\.env$/.test(String(args.path)))
          return { arguments: { ...args, path: `${String(args.path)}.example` }, reason: 'secrets are redacted' };
        if (operation === 'tools.escalate' && tickets.get(String(args.id))?.status === 'closed')
          return { decision: 'deny', reason: 'closed tickets are not escalated' };
      },
    },
    beforeSpawn: async ({ spawn, resolved }) => {
      if (resolved.tools?.includes('escalate')) return { decision: 'deny', reason: 'children may not escalate' };
      if (!spawn.definition) return { spawn: { ...spawn, definition: 'researcher' }, reason: 'unnamed children run as researcher' };
    },
    turnStart: { optional: true, handler: async () => ({
      context: `On call: ${await roster.current()}. Open tickets: ${[...tickets.values()].filter(t => t.status === 'open').length}. Escalations so far: ${escalations.length}.`,
    }) },
  },

  surface: { autoTitle: true, goalLoop: false },
});

// Serve it and run a turn:
//
//   import { createWhipClient } from '@whip/sdk';
//   const client = createWhipClient({ endpoint: 'http://127.0.0.1:8080', clientId: 'support-triage' });
//   await client.connect();
//   const runtime = await client.agents.serve(supportTriage);       // register, bind tools and hooks, serve until close()
//   const session = await runtime.sessions.create({ cwd: '/srv/support' });
//   const turn = session.run('Triage ticket 42 and escalate it if it is still open.');
//   for await (const event of turn) if (event.type === 'text') process.stdout.write(event.delta);
//   const result = await turn.result();                               // TurnResult<{ ticket, summary, escalatedTo }>
//   if (result.status === 'succeeded') console.log(result.output.summary);
//   runtime.close();
