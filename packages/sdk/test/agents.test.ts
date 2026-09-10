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

async function until(check: () => boolean, what: string): Promise<void> {
  const deadline = Date.now() + 5000;
  while (!check()) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}`);
    await new Promise(resolve => setTimeout(resolve, 5));
  }
}

test('serve binds the executor, runs handlers, honors cancellation, re-binds after reconnect, and fails fast once closed', async t => {
  const revision = 'b'.repeat(64);
  const lookup = tool('lookup_ticket', 'Fetch a ticket by id', { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] },
    async ({ id }: { id: string }, context) => {
      context.progress(`looking up ${id}`);
      if (id === 'boom') throw new Error('upstream unavailable');
      if (id === 'hang') await new Promise((_, reject) => context.signal.addEventListener('abort', () => reject(context.signal.reason), { once: true }));
      return { id, invocation: context.invocationId, turn: context.turnId, agent: context.agentId };
    });
  const agent = defineAgent({ id: 'support-bot', modules: ['context'], tools: [lookup] });
  let generation = 0;
  const pending: unknown[] = [];
  const fixture = transportFixture({ request(request, connection) {
    switch (request.method) {
      case 'definitions.register': connection.reply(request, { id: 'support-bot', revision, created: true }); break;
      case 'executor.bind': connection.reply(request, { generation: String(++generation), tools: ['lookup_ticket'] }); break;
      case 'executor.pending': connection.reply(request, { invocations: pending.splice(0) }); break;
      case 'tool.result': case 'tool.progress': connection.reply(request, { accepted: true }); break;
    }
  } });
  const invoke = (id: string, input: unknown, overrides: Record<string, unknown> = {}) => ({
    invocation_id: id, definition: 'support-bot', revision, generation: String(generation), root_id: 'root', agent_id: 'agent-1', turn_id: 'turn-1',
    tool: 'lookup_ticket', input, deadline_millis: String(Date.now() + 60_000), ...overrides,
  });
  const requests = (method: string) => fixture.current.requests.filter(request => request.method === method);
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'executor' });
  t.after(() => client.close());
  await client.connect();
  await assert.rejects(client.agents.serve(defineAgent({ id: 'toolless', modules: ['context'] })), /declares no tools/);
  const executor = await client.agents.serve(agent);
  assert.deepEqual(fixture.current.requests.map(request => request.method), ['initialize', 'definitions.register', 'executor.bind']);
  assert.deepEqual(requests('executor.bind')[0]?.params, { definition: 'support-bot', revision, tools: ['lookup_ticket'] });
  assert.equal(executor.generation, '1');
  assert.equal(executor.revision, revision);

  fixture.current.notify('tool.invoke', invoke('inv-1', { id: '42' }));
  await until(() => requests('tool.result').length === 1, 'first result');
  assert.deepEqual(requests('tool.progress')[0]?.params, { invocation_id: 'inv-1', generation: '1', text: 'looking up 42' });
  assert.deepEqual(requests('tool.result')[0]?.params, { invocation_id: 'inv-1', generation: '1', output: { id: '42', invocation: 'inv-1', turn: 'turn-1', agent: 'agent-1' } });

  fixture.current.notify('tool.invoke', invoke('inv-2', { id: 'boom' }));
  await until(() => requests('tool.result').length === 2, 'error result');
  assert.deepEqual(requests('tool.result')[1]?.params, { invocation_id: 'inv-2', generation: '1', error: 'upstream unavailable' });

  fixture.current.notify('tool.invoke', invoke('inv-3', { id: 'hang' }));
  await until(() => executor.active === 1, 'running handler');
  fixture.current.notify('tool.cancel', { invocation_id: 'inv-3', generation: '1', reason: 'timeout' });
  await until(() => executor.active === 0, 'cancelled handler');
  fixture.current.notify('tool.invoke', invoke('inv-4', { id: '1' }, { generation: '9' }));
  fixture.current.notify('tool.invoke', invoke('inv-5', { id: '1' }, { tool: 'nope' }));
  await until(() => requests('tool.result').length === 3, 'unknown tool result');
  assert.equal(requests('tool.result').some(request => request.params.invocation_id === 'inv-3' || request.params.invocation_id === 'inv-4'), false);
  assert.match(String(requests('tool.result')[2]?.params.error), /no handler for tool nope/);

  // Reconnect: bind again on the new connection, then drain pending invocations.
  pending.push(invoke('inv-6', { id: '7' }, { generation: '2' }));
  fixture.current.fail();
  await until(() => fixture.connections.length === 2 && client.getSnapshot().state === 'connected', 'reconnect');
  await until(() => requests('tool.result').length === 1, 'drained result');
  assert.deepEqual(fixture.current.requests.map(request => request.method), ['initialize', 'executor.bind', 'executor.pending', 'tool.progress', 'tool.result']);
  assert.deepEqual(requests('executor.pending')[0]?.params, { definition: 'support-bot', revision, generation: '2' });
  assert.equal(requests('tool.result')[0]?.params.invocation_id, 'inv-6');
  assert.equal(executor.generation, '2');

  executor.close();
  await executor.done;
  fixture.current.notify('tool.invoke', invoke('inv-7', { id: '42' }));
  await until(() => requests('tool.result').length === 2, 'closed result');
  assert.deepEqual(requests('tool.result')[1]?.params, { invocation_id: 'inv-7', generation: '2', error: 'executor closed' });
});
