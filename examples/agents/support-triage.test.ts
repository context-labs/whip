import assert from 'node:assert/strict';
import test from 'node:test';
import { createWhipClient } from '@whip/sdk';
import { tool, type ToolContext } from '@whip/sdk/agents';
import { scriptedDaemon } from '@whip/sdk/testing';
import { z } from 'zod';
import { audit, escalate, escalations, lookupTicket, supportTriage } from './support-triage.js';

const context: ToolContext = { invocationId: 'inv-1', rootId: 'root', agentId: 'root', turnId: 'turn', deadline: Date.now() + 1000, signal: new AbortController().signal, progress: () => {} };

test('the document pins its derived schemas, so a zod upgrade that changes them shows here first', () => {
  const document = supportTriage.document;
  assert.deepEqual(document.tools?.map(t => t.name), ['lookup_ticket', 'escalate']);
  assert.deepEqual(document.tools?.[0]?.input_schema, { $schema: 'https://json-schema.org/draft/2020-12/schema', type: 'object', properties: { id: { type: 'string' } }, required: ['id'] });
  assert.deepEqual(document.tools?.[0]?.output_schema, {
    $schema: 'https://json-schema.org/draft/2020-12/schema', type: 'object',
    properties: { id: { type: 'string' }, title: { type: 'string' }, status: { type: 'string', enum: ['open', 'closed'] }, customer: { type: 'string' } },
    required: ['id', 'title', 'status', 'customer'], additionalProperties: false,
  });
  assert.deepEqual((document.tools?.[1]?.input_schema as { properties: { note: unknown } }).properties.note, { type: 'string', maxLength: 200 });
  assert.deepEqual(document.output, {
    $schema: 'https://json-schema.org/draft/2020-12/schema', type: 'object',
    properties: { ticket: { type: 'string' }, summary: { type: 'string' }, escalatedTo: { anyOf: [{ type: 'string', enum: ['auth', 'billing', 'mobile'] }, { type: 'null' }] } },
    required: ['ticket', 'summary', 'escalatedTo'], additionalProperties: false,
  });
  assert.deepEqual(document.children.researcher?.tools, ['lookup_ticket']);
  assert.deepEqual(document.hooks, {
    before_tool: { operations: null, optional: false, timeout_millis: 10_000 },
    before_spawn: { operations: null, optional: false, timeout_millis: 0 },
    turn_start: { operations: null, optional: true, timeout_millis: 0 },
  });
});

test('the handlers are typed by their schemas and behave in-process', async () => {
  const ticket = await lookupTicket.execute({ id: '42' }, context);
  const title: string = ticket.title;                          // inferred from the output schema
  assert.equal(title, 'Login page times out on mobile');
  await assert.rejects(Promise.resolve(lookupTicket.execute({ id: '99' }, context)), /ticket 99 not found/);
  await escalate.execute({ id: '42', team: 'auth', note: 'sev2' }, context);
  await escalate.execute({ id: '42', team: 'auth', note: 'sev2' }, context);
  assert.equal(escalations.length, 1, 'a retried escalation is recorded once');
  // A return that does not match the output schema does not compile.
  // @ts-expect-error title, status, and customer are required by Ticket
  tool({ name: 'untyped', description: 'd', input: z.object({ id: z.string() }), output: lookupTicket.output, execute: async ({ id }) => ({ id }) });
  const hookContext = { invocationId: 'hook', rootId: 'root', agentId: 'root', turnId: 'turn', permissionMode: 'automatic', deadline: Date.now() + 1000, signal: new AbortController().signal };
  const beforeTool = supportTriage.hooks.before_tool!;
  assert.deepEqual(await beforeTool({ ...hookContext, operation: 'shell.run', arguments: { command: 'rm -rf build' } }), { decision: 'deny', reason: 'destructive shell commands are not allowed' });
  assert.deepEqual(await beforeTool({ ...hookContext, operation: 'tools.escalate', arguments: { id: '43', team: 'auth', note: 'n' } }), { decision: 'deny', reason: 'closed tickets are not escalated' });
  assert.equal(await beforeTool({ ...hookContext, operation: 'tools.escalate', arguments: { id: '42', team: 'auth', note: 'n' } }), undefined);
  assert.equal(audit.length, 3);
});

test('a scripted turn runs the README loop through session.run with a typed result', async t => {
  const daemon = scriptedDaemon().serveSessions();
  const client = createWhipClient({ endpoint: daemon.factory, clientId: 'support-triage-test', commandPollMs: 5 });
  t.after(() => client.close());
  await client.connect();
  const runtime = await client.agents.serve(supportTriage);
  const session = await runtime.sessions.create({ cwd: '/srv/support' });
  const played = daemon.turn(session.rootId, {
    steps: [
      { text: 'Looking up ticket 42. ' },
      { host: { id: '1:1', operation: 'tools.lookup_ticket', summary: 'id=42' } },
      { hook: { hook: 'before_tool', operation: 'shell.run', decision: 'deny', reason: 'destructive shell commands are not allowed' } },
      { question: { id: 'q-1', question: 'Escalate to auth?', options: [{ label: 'Yes' }, { label: 'No' }] } },
      { text: 'Escalated to auth.' },
    ],
    text: 'Looking up ticket 42. Escalated to auth.',
    output: { ticket: '42', summary: 'Login page times out on mobile', escalatedTo: 'auth' },
  });
  const turn = session.run('Triage ticket 42 and escalate it if it is still open.');
  const seen: string[] = [];
  let text = '';
  for await (const event of turn) {
    seen.push(event.type);
    if (event.type === 'text') text += event.delta;
    if (event.type === 'question') await event.answer([event.options[0]!.label]);
  }
  await played;
  assert.deepEqual(seen, ['raw', 'text', 'host', 'host', 'hook', 'question', 'text', 'end']);
  assert.equal(text, 'Looking up ticket 42. Escalated to auth.');
  const result = await turn.result();
  assert.equal(result.status, 'succeeded');
  if (result.status === 'succeeded') {
    const escalatedTo: 'auth' | 'billing' | 'mobile' | null = result.output.escalatedTo;   // typed by the contract
    assert.deepEqual([result.output.ticket, result.output.summary, escalatedTo], ['42', 'Login page times out on mobile', 'auth']);
  }
  const answers = daemon.current.requests.filter(request => request.method === 'command.submit' && request.params.operation === 'question.answer');
  assert.deepEqual(answers.map(request => request.params.payload), [{ id: 'q-1', answer: ['Yes'], dismissed: false }]);
});
