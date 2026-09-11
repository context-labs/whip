import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { WhipClient } from '../src/client.js';
import { defineAgent, tool, type HooksInput } from '../src/agents.js';
import type { StandardResult, StandardSchemaWithJSON } from '../src/schema.js';
import { transportFixture } from './transport-fixture.js';

/** A Standard JSON Schema built by hand, so the SDK's typing and validation are tested without a schema library. */
function standard<I, O = I>(json: Record<string, unknown>, validate: (value: unknown) => StandardResult<O>): StandardSchemaWithJSON<I, O> {
  return { '~standard': { version: 1, vendor: 'hand-rolled', validate, jsonSchema: { input: () => json, output: () => json } } };
}
const ticketInput = standard<{ id: string }>({ type: 'object', properties: { id: { type: 'string' } }, required: ['id'] }, value => {
  const id = (value as { id?: unknown }).id;
  return typeof id === 'string' && id !== 'bad' ? { value: { id } } : { issues: [{ message: id === 'bad' ? 'id "bad" is reserved' : 'expected a string', path: ['id'] }] };
});
const ticketOutput = standard<{ id: string; title: string }>({ type: 'object', properties: { id: { type: 'string' }, title: { type: 'string' } }, required: ['id', 'title'] }, value => {
  const record = value as { id?: unknown; title?: unknown };
  return typeof record.title === 'string' ? { value: { id: String(record.id), title: record.title } } : { issues: [{ message: 'title is required' }] };
});

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
  const lookup = tool({ name: 'lookup_ticket', description: 'Fetch a ticket by id', input: { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] }, execute: async input => ({ id: (input as { id: string }).id }), timeoutMs: 1000 });
  const agent = defineAgent({ id: 'support-bot', modules: ['context'], tools: [lookup], children: { helper: { modules: ['context'], tools: ['lookup_ticket'], report: 'message' } } });
  assert.deepEqual(agent.document.tools, [{ name: 'lookup_ticket', description: 'Fetch a ticket by id', input_schema: { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] }, timeout_millis: 1000 }]);
  assert.equal(agent.handlers.get('lookup_ticket'), lookup);
  assert.deepEqual(agent.document.children.helper, { instructions: null, modules: ['context'], capabilities: null, tools: ['lookup_ticket'], model: { model: '', provider: '', effort: '' }, budgets: {}, report: 'message' });
  assert.throws(() => defineAgent({ id: 'dup', modules: ['context'], tools: [lookup, lookup] }), /declared twice/);
  assert.throws(() => defineAgent({ id: 'none', modules: [] }), /at least one host module/);
  assert.throws(() => tool({ name: 'bad', description: 'd', input: [] as unknown as Record<string, unknown>, execute: () => 1 }), /Standard JSON Schema or a JSON Schema object/);
  assert.throws(() => tool({ name: 'list', description: 'd', input: { type: 'array' }, execute: () => 1 }), /must describe an object/);
  assert.throws(() => tool({ name: 'slow', description: 'd', input: { type: 'object' }, execute: () => 1, timeoutMs: -1 }), /timeoutMs/);
});

test('a Standard JSON Schema types the handler and derives the document schema', () => {
  const lookup = tool({ name: 'lookup_ticket', description: 'Fetch a ticket by id', input: ticketInput, output: ticketOutput, execute: async ({ id }, context) => ({ id, title: `ticket ${id} for ${context.agentId}` }) });
  // The input type is the schema's; a return that does not match the output schema does not compile.
  tool({ name: 'typed', description: 'd', input: ticketInput, execute: ({ id }) => { const text: string = id; return text; } });
  // @ts-expect-error the output schema requires a title
  tool({ name: 'untyped', description: 'd', input: ticketInput, output: ticketOutput, execute: async ({ id }) => ({ id }) });
  // @ts-expect-error raw JSON Schema types the input unknown
  tool({ name: 'raw', description: 'd', input: { type: 'object' }, execute: input => input.id });
  assert.deepEqual(lookup.spec, { name: 'lookup_ticket', description: 'Fetch a ticket by id', input_schema: { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] }, timeout_millis: 0 });
  assert.equal(lookup.validate, true);
  const throwing: StandardSchemaWithJSON<{ id: string }> = { '~standard': { version: 1, vendor: 'hand-rolled', validate: () => ({ value: { id: '' } }),
    jsonSchema: { input: () => { throw new Error('unsupported target'); }, output: () => ({}) } } };
  assert.throws(() => tool({ name: 'nojson', description: 'd', input: throwing, execute: () => 1 }), /hand-rolled schema cannot produce draft-2020-12 JSON Schema/);
});

test('serve validates typed tool inputs and outputs locally around execute', async t => {
  const revision = 'c'.repeat(64);
  const calls: string[] = [];
  const lookup = tool({ name: 'lookup_ticket', description: 'Fetch a ticket by id', input: ticketInput, output: ticketOutput, execute: async ({ id }) => {
    calls.push(id);
    return id === 'untitled' ? { id } as unknown as { id: string; title: string } : { id, title: `ticket ${id}` };
  } });
  const unchecked = tool({ name: 'unchecked', description: 'd', input: ticketInput, output: ticketOutput, validate: false, execute: async ({ id }) => ({ id } as unknown as { id: string; title: string }) });
  const agent = defineAgent({ id: 'support-bot', modules: ['context'], tools: [lookup, unchecked] });
  const fixture = transportFixture({ request(request, connection) {
    switch (request.method) {
      case 'definitions.register': connection.reply(request, { id: 'support-bot', revision, created: true }); break;
      case 'executor.bind': connection.reply(request, { generation: '1', tools: ['lookup_ticket', 'unchecked'] }); break;
      case 'tool.result': connection.reply(request, { accepted: true }); break;
    }
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'executor', reconnect: false });
  t.after(() => client.close());
  await client.connect();
  await client.agents.serve(agent);
  const invoke = (id: string, tool: string, input: unknown) => fixture.current.notify('tool.invoke', {
    invocation_id: id, definition: 'support-bot', revision, generation: '1', root_id: 'root', agent_id: 'root', turn_id: 'turn-1',
    tool, input, deadline_millis: String(Date.now() + 60_000),
  });
  const results = () => fixture.current.requests.filter(request => request.method === 'tool.result').map(request => request.params);
  invoke('inv-1', 'lookup_ticket', { id: '42' });
  invoke('inv-2', 'lookup_ticket', { id: 'bad' });
  invoke('inv-3', 'lookup_ticket', { id: 'untitled' });
  invoke('inv-4', 'unchecked', { id: 'untitled' });
  await until(() => results().length === 4, 'four results');
  const byId = Object.fromEntries(results().map(result => [result.invocation_id, result]));
  assert.deepEqual(byId['inv-1'], { invocation_id: 'inv-1', generation: '1', output: { id: '42', title: 'ticket 42' } });
  assert.deepEqual(byId['inv-2'], { invocation_id: 'inv-2', generation: '1', error: 'tool lookup_ticket rejected its input: id: id "bad" is reserved' });
  assert.deepEqual(byId['inv-3'], { invocation_id: 'inv-3', generation: '1', error: 'tool lookup_ticket returned a value that does not match its output schema (the handler already ran): title is required' });
  assert.deepEqual(byId['inv-4'], { invocation_id: 'inv-4', generation: '1', output: { id: 'untitled' } });
  assert.deepEqual(calls, ['42', 'untitled'], 'a rejected input never reaches execute');
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
  const lookup = tool({ name: 'lookup_ticket', description: 'Fetch a ticket by id', input: { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] },
    execute: async (input, context) => {
      const { id } = input as { id: string };
      context.progress(`looking up ${id}`);
      if (id === 'boom') throw new Error('upstream unavailable');
      if (id === 'hang') await new Promise((_, reject) => context.signal.addEventListener('abort', () => reject(context.signal.reason), { once: true }));
      return { id, invocation: context.invocationId, turn: context.turnId, agent: context.agentId };
    } });
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

test('hooks are declared in the document and served beside tools', async t => {
  const revision = 'c'.repeat(64);
  const seen: string[] = [];
  const agent = defineAgent({
    id: 'guarded',
    modules: ['context', 'files', 'shell', 'agents'],
    hooks: {
      beforeTool: { operations: ['shell.run', 'files.read'], timeoutMs: 5000, handler: async event => {
        seen.push(`${event.operation}:${event.agentId}:${event.permissionMode}`);
        if (event.operation === 'shell.run' && String(event.arguments.command).includes('rm -rf')) return { decision: 'deny', reason: 'destructive' };
        if (event.operation === 'files.read' && String(event.arguments.path).endsWith('.env')) return { arguments: { ...event.arguments, path: 'README.md' }, reason: 'redacted' };
        if (event.operation === 'files.read' && event.arguments.path === 'boom') throw new Error('hook crashed');
        if (event.operation === 'files.read' && event.arguments.path === 'hang') await new Promise((_, reject) => event.signal.addEventListener('abort', () => reject(event.signal.reason), { once: true }));
      } },
      beforeSpawn: { optional: true, handler: async ({ spawn, resolved }) => {
        seen.push(`spawn:${spawn.definition}:${resolved.definition}`);
        if (resolved.capabilities?.includes('shell')) return { decision: 'deny', reason: 'children may not hold shell' };
        return { spawn: { ...spawn, definition: 'researcher' }, reason: 'all children research' };
      } },
      turnStart: async ({ input }) => ({ context: `Turn context for: ${input}` }),
    },
  });
  assert.deepEqual(agent.document.hooks, {
    before_tool: { operations: ['shell.run', 'files.read'], optional: false, timeout_millis: 5000 },
    before_spawn: { operations: null, optional: true, timeout_millis: 0 },
    turn_start: { operations: null, optional: false, timeout_millis: 0 },
  });
  assert.equal(agent.handlers.size, 0);
  const filtered = { operations: ['shell.run'], handler: () => {} } as unknown as HooksInput['turnStart'];
  assert.throws(() => defineAgent({ id: 'x', modules: ['context'], hooks: { turnStart: filtered } }), /only before_tool accepts operations/);
  assert.throws(() => defineAgent({ id: 'x', modules: ['context'], hooks: { beforeTool: { timeoutMs: -1, handler: () => {} } } }), /non-negative/);
  assert.equal(defineAgent({ id: 'plain', modules: ['context'] }).document.hooks, null);

  const fixture = transportFixture({ request(request, connection) {
    switch (request.method) {
      case 'definitions.register': connection.reply(request, { id: 'guarded', revision, created: true }); break;
      case 'executor.bind': connection.reply(request, { generation: '1', tools: [], hooks: ['before_tool', 'before_spawn', 'turn_start'] }); break;
      case 'hook.result': connection.reply(request, { accepted: true }); break;
    }
  } });
  const client = new WhipClient({ endpoint: fixture.factory, clientId: 'hooks' });
  t.after(() => client.close());
  await client.connect();
  const executor = await client.agents.serve(agent);
  assert.deepEqual(fixture.current.requests.find(request => request.method === 'executor.bind')?.params, { definition: 'guarded', revision, tools: [], hooks: ['before_tool', 'before_spawn', 'turn_start'] });
  const results = () => fixture.current.requests.filter(request => request.method === 'hook.result').map(request => request.params);
  const invoke = (id: string, hook: string, extra: Record<string, unknown>) => ({
    invocation_id: id, definition: 'guarded', revision, generation: '1', root_id: 'root', agent_id: 'agent-1', turn_id: 'turn-1', hook,
    permission_mode: 'automatic', deadline_millis: String(Date.now() + 60_000), ...extra,
  });

  fixture.current.notify('hook.invoke', invoke('h1', 'before_tool', { operation: 'files.list', arguments: { path: '.' } }));
  fixture.current.notify('hook.invoke', invoke('h2', 'before_tool', { operation: 'shell.run', arguments: { command: 'rm -rf /' } }));
  fixture.current.notify('hook.invoke', invoke('h3', 'before_tool', { operation: 'files.read', arguments: { path: 'secret.env' } }));
  fixture.current.notify('hook.invoke', invoke('h4', 'before_tool', { operation: 'files.read', arguments: { path: 'boom' } }));
  await until(() => results().length === 4, 'four hook results');
  assert.deepEqual(results(), [
    { invocation_id: 'h1', generation: '1' },
    { invocation_id: 'h2', generation: '1', decision: 'deny', reason: 'destructive' },
    { invocation_id: 'h3', generation: '1', reason: 'redacted', arguments: { path: 'README.md' } },
    { invocation_id: 'h4', generation: '1', error: 'hook crashed' },
  ]);
  assert.deepEqual(seen, ['files.list:agent-1:automatic', 'shell.run:agent-1:automatic', 'files.read:agent-1:automatic', 'files.read:agent-1:automatic']);

  fixture.current.notify('hook.invoke', invoke('h5', 'before_tool', { operation: 'files.read', arguments: { path: 'hang' } }));
  await until(() => executor.active === 1, 'running hook');
  fixture.current.notify('hook.cancel', { invocation_id: 'h5', generation: '1', reason: 'timeout' });
  await until(() => executor.active === 0, 'cancelled hook');
  assert.equal(results().some(result => result.invocation_id === 'h5'), false);

  const spawn = { prompt: 'go', name: 'kid', definition: '', capabilities: null, tools: null, budgets: {}, report: '', model: '', provider: '', effort: '' };
  fixture.current.notify('hook.invoke', invoke('h6', 'before_spawn', { operation: 'agents.spawn', spawn: { request: spawn, resolved: { definition: 'guarded', modules: ['context'], capabilities: ['read'], tools: null, budgets: {}, report: 'notice' } } }));
  fixture.current.notify('hook.invoke', invoke('h7', 'before_spawn', { operation: 'agents.spawn', spawn: { request: spawn, resolved: { definition: 'guarded', modules: ['context'], capabilities: ['shell'], tools: null, budgets: {}, report: 'notice' } } }));
  fixture.current.notify('hook.invoke', invoke('h8', 'turn_start', { input: 'hello' }));
  fixture.current.notify('hook.invoke', invoke('h9', 'nope', {}));
  await until(() => results().length === 8, 'spawn and turn results');
  // The missing-handler reply posts without awaiting a handler, so order by id.
  assert.deepEqual(results().slice(4).sort((a, b) => String(a.invocation_id).localeCompare(String(b.invocation_id))), [
    { invocation_id: 'h6', generation: '1', reason: 'all children research', spawn: { ...spawn, definition: 'researcher' } },
    { invocation_id: 'h7', generation: '1', decision: 'deny', reason: 'children may not hold shell' },
    { invocation_id: 'h8', generation: '1', context: 'Turn context for: hello' },
    { invocation_id: 'h9', generation: '1', error: 'no handler for hook nope' },
  ]);
  assert.deepEqual(seen.slice(5), ['spawn::guarded', 'spawn::guarded']); // h5 recorded before it hung
  executor.close();
  await executor.done;
});
