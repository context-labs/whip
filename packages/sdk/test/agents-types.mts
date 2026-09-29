import { defineAgent, tool } from '../src/agents.js';
import type { AgentSession, StandardSchemaWithJSON } from '../src/agents.js';
import type { Client } from '../src/index.js';
import type { DuplexTransport } from '../src/executors.js';
declare const input: StandardSchemaWithJSON<{ raw: string }, { value: number }>;
declare const output: StandardSchemaWithJSON<{ answer: string }, { answer: number }>;
const lookup = tool({ name: 'lookup', description: 'Typed lookup', input, output, execute(value, context) {
  const number: number = value.value;
  const signal: AbortSignal = context.signal;
  // The callback receives validated/transformed output, not the schema's raw input.
  // @ts-expect-error raw input does not survive the declared transform.
  value.raw;
  void signal;
  return { answer: String(number) };
} });
const agent = defineAgent({ id: 'typed', name: 'Typed', tools: [lookup], output });
export async function typedSession(client: Client, transport: DuplexTransport) {
  const served = await client.agents.serve(agent, { transport });
  const session: AgentSession<{ answer: number }> = await served.sessions.open('root');
  const run = session.run([{ type: 'text', text: 'work' }], 'request');
  const result = await run.result();
  const answer: number | undefined = result.output?.answer;
  // @ts-expect-error output is the declared output shape.
  result.output?.missing;
  return answer;
}
