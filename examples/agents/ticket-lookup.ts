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
  modules: ['context', 'state', 'user'],
  tools: [lookupTicket],
  surface: { autoTitle: true, goalLoop: false },
});

// Serve the tool and start a session that can call it:
//
//   import { createWhipClient } from '@whip/sdk';
//   const client = createWhipClient({ endpoint: 'http://127.0.0.1:8080', clientId: 'support-triage' });
//   await client.connect();
//   const executor = await client.agents.serve(supportTriage); // registers, binds, serves until close()
//   const created = await client.sessions.create({ cwd: '/path/to/repo', definition: 'support-triage' }).result();
//   ...
//   executor.close();
