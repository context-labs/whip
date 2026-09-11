// A support triage agent with one custom tool. The tool runs in this process:
// the daemon validates the model's arguments against the schema, records the
// call in its ledger, and hands it to whichever process called
// client.agents.serve for this definition revision.
import { defineAgent, tool } from '@whip/sdk/agents';

interface Ticket { id: string; title: string; status: 'open' | 'closed' }

const tickets = new Map<string, Ticket>([
  ['42', { id: '42', title: 'Login page times out on mobile', status: 'open' }],
  ['43', { id: '43', title: 'Export button disabled after refresh', status: 'closed' }],
]);

export const lookupTicket = tool(
  'lookup_ticket',
  'Fetch a support ticket by id. Returns its title and status.',
  { type: 'object', properties: { id: { type: 'string', description: 'The ticket id' } }, required: ['id'], additionalProperties: false },
  async ({ id }: { id: string }, context) => {
    context.progress(`looking up ticket ${id}`);
    const ticket = tickets.get(id);
    if (!ticket) throw new Error(`ticket ${id} not found`);
    return ticket;
  },
  { timeoutMs: 30_000 },
);

export const supportTriage = defineAgent({
  id: 'support-triage',
  instructions: {
    persona: 'You triage support tickets for a small engineering team.',
    rules: [
      'Operating rules:',
      '- Look tickets up with tools.lookup_ticket before describing them; never guess a title or status.',
      '- Summarize each ticket in one line and recommend an owner.',
    ].join('\n'),
    skillDiscovery: false,
    standingInstructions: false,
  },
  modules: ['context', 'files', 'shell', 'state', 'user', 'agents'],
  capabilities: ['read', 'shell'],
  tools: [lookupTicket],
  children: { researcher: { modules: ['context', 'files'], capabilities: ['read'], tools: ['lookup_ticket'], report: 'message' } },
  hooks: {
    // Observe every operation; deny destructive shell commands; redirect reads of secrets.
    beforeTool: async ({ operation, arguments: args, agentId }) => {
      auditLog.push(`${agentId} ${operation}`);
      if (operation === 'shell.run' && /\brm\s+-rf\b/.test(String(args.command))) return { decision: 'deny', reason: 'destructive shell commands are not allowed' };
      if (operation === 'files.read' && String(args.path).endsWith('.env')) return { arguments: { ...args, path: `${String(args.path)}.example` }, reason: 'secrets are redacted' };
    },
    // Children always run as the researcher child and never gain shell.
    beforeSpawn: async ({ spawn, resolved }) => {
      if (resolved.capabilities?.includes('shell')) return { decision: 'deny', reason: 'children may not hold shell' };
      if (spawn.definition !== 'researcher') return { spawn: { ...spawn, definition: 'researcher' }, reason: 'all children run as researcher' };
    },
    // Live facts for each turn; ephemeral, never in history.
    turnStart: async () => ({ context: `On call: ${onCall}. Open tickets: ${[...tickets.values()].filter(ticket => ticket.status === 'open').length}.` }),
  },
  surface: { autoTitle: true, goalLoop: false },
});

export const auditLog: string[] = [];
const onCall = 'Sam';

// Serve the tool and start a session that can call it:
//
//   import { createWhipClient } from '@whip/sdk';
//   const client = createWhipClient({ endpoint: 'http://127.0.0.1:8080', clientId: 'support-triage' });
//   await client.connect();
//   const executor = await client.agents.serve(supportTriage); // registers, binds tools and hooks, serves until close()
//   const created = await client.sessions.create({ cwd: '/path/to/repo', definition: 'support-triage' }).result();
//   ...
//   executor.close();
