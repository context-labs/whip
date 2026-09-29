import assert from 'node:assert/strict';
import test from 'node:test';
import { tool, type ToolContext, type HookContext } from '@whip/sdk/agents';
import { z } from 'zod';
import { audit, beforeTool, escalate, escalations, lookupTicket, supportTriage, supportResearcher } from './support-triage.js';
const ref = { id: 'support-researcher', revision: 'a'.repeat(64) };
const context: ToolContext = { invocationID: 'inv-1', sessionID: 'root', turnID: 'turn', operationID: 'operation', origin: 'cell', deadline: Date.now() + 1000, signal: new AbortController().signal, progress: async () => {} };
const hookContext: HookContext = { ...context, permissionMode: 'automatic' };

test('native declarations pin tool schemas and an immutable narrowed child, without granting authority', () => {
  const document = supportTriage(ref).document;
  assert.deepEqual(Object.keys(document.defaults.tools!), ['lookup_ticket', 'escalate']);
  assert.deepEqual(document.defaults.tools!.lookup_ticket!.input_schema, { $schema: 'https://json-schema.org/draft/2020-12/schema', type: 'object', properties: { id: { type: 'string' } }, required: ['id'] });
  assert.deepEqual(document.defaults.tools!.lookup_ticket!.output_schema, {
    $schema: 'https://json-schema.org/draft/2020-12/schema', type: 'object',
    properties: { id: { type: 'string' }, title: { type: 'string' }, status: { type: 'string', enum: ['open', 'closed'] }, customer: { type: 'string' } },
    required: ['id', 'title', 'status', 'customer'], additionalProperties: false,
  });
  assert.match(JSON.stringify(document.defaults.tools!.escalate!.input_schema), /"maxLength":200/);
  assert.match(JSON.stringify(document.defaults.output), /"escalatedTo"/);
  assert.deepEqual(document.defaults.children, { researcher: ref });
  assert.deepEqual(Object.keys(supportResearcher.handlers), ['lookup_ticket']);
  assert.deepEqual(supportResearcher.document.defaults.output, { schema: null });
  assert.deepEqual(document.defaults.hooks, supportResearcher.document.defaults.hooks, 'required hook contracts stay identical across narrowing');
  assert.equal('capabilities' in document, false);
});

test('typed handlers reject missing tickets, preserve business idempotency, and enforce hook policy', async () => {
  const ticket = await lookupTicket.execute({ id: '42' }, context);
  const title: string = ticket.title;
  assert.equal(title, 'Login page times out on mobile');
  await assert.rejects(Promise.resolve(lookupTicket.execute({ id: '99' }, context)), /ticket 99 not found/);
  await escalate.execute({ id: '42', team: 'auth', note: 'sev2' }, context);
  await escalate.execute({ id: '42', team: 'auth', note: 'sev2' }, context);
  assert.equal(escalations.length, 1);
  // @ts-expect-error title, status, and customer are required by the output schema
  tool({ name: 'untyped', description: 'd', input: z.object({ id: z.string() }), output: lookupTicket.output, execute: async ({ id }) => ({ id }) });
  assert.deepEqual(await beforeTool({ ...hookContext, operation: 'shell.run', arguments: { command: 'rm -rf build' } }), { decision: 'deny', reason: 'destructive shell commands are not allowed' });
  assert.deepEqual(await beforeTool({ ...hookContext, operation: 'tools.escalate', arguments: { id: '43', team: 'auth', note: 'n' } }), { decision: 'deny', reason: 'closed tickets are not escalated' });
  assert.equal(await beforeTool({ ...hookContext, operation: 'tools.escalate', arguments: { id: '42', team: 'auth', note: 'n' } }), undefined);
  assert.equal(audit.length, 3);
});
// Real structured turns, questions, progress, child narrowing and executor closure
// are covered by support-triage.acceptance.mjs through the production runtime.
