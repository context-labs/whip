import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { WhipClient } from '../src/client.js';
import { defineAgent, tool } from '../src/agents.js';
import { transportFixture } from './transport-fixture.js';

// The Go test loads the same fixture and requires it to equal the built-in
// JuniorDeveloper. This side requires that defineAgent produces it.
const fixture = JSON.parse(readFileSync(new URL('../../../../internal/agentdef/testdata/junior-developer.json', import.meta.url), 'utf8')) as unknown;

// examples/agents/junior-developer.ts carries the same input for readers.
const juniorDeveloper = defineAgent({
  id: 'junior-developer-ts',
  instructions: {
    persona: 'You are a junior developer working under review.',
    rules: [
      'Operating rules:',
      '- Keep each change small, and explain what you changed and why in plain language.',
      "- Run the project's tests or build after every change; if you cannot run them, say so.",
      '- Never rewrite history, force-push, delete branches, or remove files you did not create.',
      '- Do not add dependencies or change build, CI, or deployment configuration; ask first.',
      '- When a task is ambiguous or risky, ask the user with user.ask instead of guessing.',
    ].join('\n'),
    projectFiles: ['CLAUDE.md', 'AGENTS.md'],
    skillDiscovery: false,
    standingInstructions: true,
  },
  modules: ['context', 'files', 'shell', 'state', 'artifacts', 'permissions', 'user'],
  capabilities: ['read', 'write', 'shell'],
  surface: { autoTitle: true, goalLoop: false },
});

test('defineAgent produces the canonical JuniorDeveloper document', () => {
  assert.deepEqual(JSON.parse(JSON.stringify(juniorDeveloper.document)), fixture);
  assert.equal(juniorDeveloper.handlers.size, 0);
});

test('tools land in the document and handlers stay local', () => {
  const lookup = tool('lookup_ticket', 'Fetch a ticket by id', { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] }, async ({ id }: { id: string }) => ({ id }), { timeoutMs: 1000 });
  const agent = defineAgent({ id: 'support-bot', modules: ['context'], tools: [lookup], children: { helper: { modules: ['context'], tools: ['lookup_ticket'], report: 'message' } } });
  assert.deepEqual(agent.document.tools, [{ name: 'lookup_ticket', description: 'Fetch a ticket by id', input_schema: { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] }, timeout_millis: 1000 }]);
  assert.equal(agent.handlers.get('lookup_ticket'), lookup.handler);
  assert.deepEqual(agent.document.children.helper, { instructions: null, modules: ['context'], capabilities: null, tools: ['lookup_ticket'], model: { model: '', provider: '', effort: '' }, budgets: {}, report: 'message' });
  assert.throws(() => defineAgent({ id: 'dup', modules: ['context'], tools: [lookup, lookup] }), /declared twice/);
  assert.throws(() => defineAgent({ id: 'none', modules: [] }), /at least one host module/);
  assert.throws(() => tool('bad', 'd', [] as unknown as Record<string, unknown>, () => 1), /JSON Schema object/);
});

test('the registry round-trips through the protocol', async t => {
  const fixtureConnection = transportFixture({ request(request, connection) {
    if (request.method === 'definitions.register') connection.reply(request, { id: 'junior-developer-ts', revision: 'a'.repeat(64), created: true });
    if (request.method === 'definitions.list') connection.reply(request, { items: [{ id: 'coding', revision: '', built_in: true, registered_by: '', created_at: '' }, { id: 'junior-developer-ts', revision: 'a'.repeat(64), built_in: false, registered_by: 'client', created_at: '2026-09-10T00:00:00Z' }] });
    if (request.method === 'definitions.get') connection.reply(request, { definition: juniorDeveloper.document, revision: 'a'.repeat(64), built_in: false, registered_by: 'client', created_at: '2026-09-10T00:00:00Z' });
  } });
  const client = new WhipClient({ endpoint: fixtureConnection.factory, clientId: 'client', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  const registered = await client.agents.register(juniorDeveloper);
  assert.equal(registered.created, true);
  assert.deepEqual(fixtureConnection.current.requests.at(-1)?.params, { definition: fixture });
  const listed = await client.agents.list();
  assert.equal(listed.items?.length, 2);
  const record = await client.agents.get('junior-developer-ts', 'a'.repeat(64));
  assert.deepEqual(JSON.parse(JSON.stringify(record.definition)), fixture);
  assert.deepEqual(fixtureConnection.current.requests.at(-1)?.params, { id: 'junior-developer-ts', revision: 'a'.repeat(64) });
});
