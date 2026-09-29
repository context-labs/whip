import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';
import { Client, DeliveryError, RemoteError } from '../dist/index.js';
import { Agents, defineAgent, tool, decodeJSON } from '../dist/agents.js';
const fixtures = JSON.parse(await readFile(new URL('../../protocol/schema/fixtures.json', import.meta.url), 'utf8'));
const fixture = name => structuredClone(fixtures.find(value => value.type === name && value.valid).value);
const initial = { major: 4, minor: 0, runtime_id: 'runtime', process_epoch: 'boot_test', network_client: false, builtins: [] };
const base64 = value => Buffer.from(typeof value === 'string' ? value : JSON.stringify(value)).toString('base64');
const ref = { id: 'typed-agent', revision: 'a'.repeat(64) };
const tick = () => new Promise(resolve => setImmediate(resolve));
async function until(read) { for (let count = 0; count < 100; count++) { if (read()) return; await tick(); } throw new Error('condition not reached'); }
const basic = (execute, extra = {}) => defineAgent({ id: ref.id, name: 'Typed agent', tools: [tool({ name: 'lookup', description: 'lookup', input: { type: 'object' }, execute, ...extra })] });
async function setup(agent, options = {}) {
  const state = { events: [], calls: [], unary: [], closed: false, waiting: undefined, intercept: undefined, document: undefined, foreign: false };
  const lease = { epoch: 'boot_test', generation: '9007199254740993', definition: ref, tools: Object.keys(agent.handlers), hooks: Object.keys(agent.hooks) };
  const client = await Client.connect(async request => {
    let result;
    if (request.method === 'initialize') result = initial;
    else {
      state.unary.push(request);
      if (request.method === 'definitions.register') { state.document = request.params; result = { ref, document: request.params, created_at: '0' }; }
      else if (request.method === 'sessions.get') { result = fixture('Session'); result.id = request.params.session_id; result.definition = state.foreign ? { ...ref, revision: 'b'.repeat(64) } : ref; result.configuration.tools_definition = ref; result.configuration.hooks_definition = ref; }
      else throw new Error(request.method);
    }
    return { jsonrpc: '2.0', id: request.id, result };
  }, { clientID: 'client' });
  const transport = {
    async request(request) {
      state.calls.push(request);
      await state.intercept?.(request);
      return { jsonrpc: '2.0', id: request.id, result: request.method === 'initialize' ? initial : request.method === 'executor.bind' ? lease : { accepted: true } };
    },
    events: { async *[Symbol.asyncIterator]() { while (!state.closed) { if (!state.events.length) await new Promise(resolve => { state.waiting = resolve; }); if (state.events.length) yield state.events.shift(); } } },
    async close() { state.closed = true; state.waiting?.(); },
  };
  const runtime = await new Agents(client).serve(agent, { transport, ...options });
  const invoke = (id, params = {}) => {
    const invocation = { origin: 'cell', invocation_id: id, lease, kind: 'tool', name: 'lookup', session_id: 'child', turn_id: 'turn', cell_id: 'cell', operation_id: 'operation', operation: 'tools.lookup', arguments_base64: base64({ x: 'input' }), spawn_base64: null, input_preview: '', permission_mode: 'prompt', deadline_millis: String(Date.now() + 60000), ...params };
    const event = { jsonrpc: '2.0', method: 'executor.invoke', epoch: lease.epoch, generation: lease.generation, invocation_id: id, invocation };
    state.events.push(event); state.waiting?.(); state.waiting = undefined; return event;
  };
  const cancel = id => { state.events.push({ jsonrpc: '2.0', method: 'executor.cancel', epoch: lease.epoch, generation: lease.generation, invocation_id: id, invocation: null }); state.waiting?.(); state.waiting = undefined; };
  const results = () => state.calls.filter(call => call.method === 'tool.result' || call.method === 'hook.result');
  return { state, runtime, invoke, cancel, results };
}
function schema(check) { return { '~standard': { version: 1, vendor: 'fixture', validate: check, jsonSchema: { input: () => ({ type: 'object' }), output: () => ({ type: 'object' }) } } }; }

test('authoring preserves canonical empty/false defaults and freezes captured contracts', () => {
  const agent = defineAgent({ id: ref.id, name: 'Agent', defaults: { modules: [], automatic_title: false, mcp_servers: { all: false, servers: [] } }, tools: [], output: { type: 'object' } });
  assert.deepEqual(agent.document.defaults.modules, []); assert.equal(agent.document.defaults.automatic_title, false);
  assert.deepEqual(agent.document.defaults.mcp_servers, { all: false, servers: [] });
  assert.throws(() => { agent.document.name = 'changed'; }, TypeError);
  const entry = tool({ name: 'lookup', description: 'Lookup', input: {}, execute: () => null });
  assert.throws(() => defineAgent({ id: ref.id, name: 'Agent', tools: [entry, entry] }), /Duplicate/);
  assert.throws(() => tool({ name: 'constructor', input: {}, execute() {} }), /tool name/);
  assert.throws(() => tool({ name: 'lookup', input: { type: 'string' }, execute() {} }), /object/);
  assert.throws(() => tool({ name: 'lookup', input: {}, timeoutMs: 900001, execute() {} }), /timeout/);
});

test('typed handlers apply Standard Schema input/output transforms and serialize acknowledged progress', async () => {
  let called = 0;
  const agent = basic(async (input, context) => {
    called++; assert.equal(input.transformed, true); assert.equal(context.sessionID, 'child'); assert.equal(context.origin, 'cell');
    await context.progress('working'); return { value: 1 };
  }, { input: schema(value => ({ value: { ...value, transformed: true } })), output: schema(value => ({ value: { ...value, validated: true } })) });
  const { runtime, invoke, state, results } = await setup(agent);
  invoke('one'); await until(() => results().length === 1);
  assert.deepEqual(state.calls.slice(-2).map(call => call.method), ['tool.progress', 'tool.result']);
  assert.deepEqual(decodeJSON(results()[0].params.output_base64), { value: 1, validated: true }); assert.equal(called, 1);
  await runtime.close(); assert.equal(runtime.active, 0);
});

test('hooks expose captured context and return canonical narrowing replies', async () => {
  const agent = defineAgent({ id: ref.id, name: 'Hooks', hooks: {
    turnStart: event => { assert.equal(event.input, 'preview'); return { context: 'context' }; },
    beforeTool: { operations: ['tools.lookup'], handler: event => { assert.equal(event.arguments.x, 'input'); return { arguments: { x: 'rewritten' } }; } },
    beforeSpawn: event => { assert.equal(event.spawn.resolved.id, 'child'); return { decision: 'deny', reason: 'narrow scope' }; },
  } });
  const { runtime, invoke, results } = await setup(agent);
  invoke('start', { kind: 'hook', name: 'turn_start', origin: 'turn', operation_id: null, cell_id: null, input_preview: 'preview' });
  invoke('before', { kind: 'hook', name: 'before_tool' });
  invoke('spawn', { kind: 'hook', name: 'before_spawn', spawn_base64: base64({ request: {}, resolved: { id: 'child' } }) });
  await until(() => results().length === 3);
  assert.equal(results().find(call => call.params.invocation_id === 'start').params.context, 'context');
  assert.deepEqual(decodeJSON(results().find(call => call.params.invocation_id === 'before').params.arguments_base64), { x: 'rewritten' });
  assert.equal(results().find(call => call.params.invocation_id === 'spawn').params.decision, 'deny');
  await runtime.close();
});

test('unsafe integers and failed validation never invoke a handler or return rounded output', async () => {
  let called = 0; const { runtime, invoke, results } = await setup(basic(() => { called++; return {}; }));
  invoke('unsafe', { arguments_base64: base64('{"number":9007199254740993}') });
  await until(() => results().length === 1); assert.equal(called, 0); assert.match(results()[0].params.failure, /unsafe number/); assert.equal(results()[0].params.output_base64, null);
  await runtime.close();
  const checked = await setup(basic(() => { called++; return {}; }, { input: schema(() => ({ issues: [{ message: 'rejected' }] })) }));
  checked.invoke('invalid'); await until(() => checked.results().length === 1); assert.equal(called, 0); await checked.runtime.close();
});

test('lost result acknowledgement closes and joins without a second result or callback', async () => {
  let called = 0; const { runtime, invoke, state, results } = await setup(basic(() => { called++; return {}; }));
  state.intercept = request => { if (request.method === 'tool.result') throw new DeliveryError('lost acknowledgement'); };
  invoke('lost'); await assert.rejects(runtime.done, DeliveryError);
  assert.equal(called, 1); assert.equal(results().length, 1); assert.equal(state.closed, true); assert.equal(runtime.active, 0);
});

test('a repeated completed invocation and lifetime overflow close rather than evicting callback identities', async () => {
  let called = 0; const first = await setup(basic(() => { called++; return {}; }));
  first.invoke('repeat'); await until(() => first.results().length === 1); first.invoke('repeat');
  await assert.rejects(first.runtime.done, /Repeated invocation/); assert.equal(called, 1);
  const bounded = await setup(basic(() => { called++; return {}; }), { maxInvocations: 2 });
  for (const id of ['a', 'b']) { bounded.invoke(id); await until(() => bounded.results().length === (id === 'a' ? 1 : 2)); }
  bounded.invoke('c'); await assert.rejects(bounded.runtime.done, /lifetime invocation limit/); assert.equal(called, 3);
});

test('cancellation and capacity close abort callbacks and retain slots until joined completion', async () => {
  let finished = 0;
  const agent = basic(async (_input, context) => { await new Promise(resolve => context.signal.addEventListener('abort', resolve, { once: true })); await tick(); finished++; return {}; });
  const one = await setup(agent); one.invoke('cancelled'); await until(() => one.runtime.active === 1); await tick(); one.cancel('cancelled');
  await until(() => finished === 1); assert.equal(one.results().length, 0); await one.runtime.close(); assert.equal(one.runtime.active, 0);
  const two = await setup(agent, { maxConcurrent: 1 }); two.invoke('first'); await until(() => two.runtime.active === 1); await tick(); two.invoke('second');
  await assert.rejects(two.runtime.done, /capacity/); assert.equal(finished, 2); assert.equal(two.runtime.active, 0); assert.equal(two.results().length, 0);
});

test('foreign definition events and session handles fail closed', async () => {
  let called = 0; const value = await setup(basic(() => { called++; return {}; }));
  value.state.foreign = true; await assert.rejects(value.runtime.sessions.open('root'), /not pinned/);
  const event = value.invoke('foreign'); event.invocation.lease = { ...event.invocation.lease, definition: { ...ref, revision: 'b'.repeat(64) } };
  await assert.rejects(value.runtime.done, /Foreign definition/); assert.equal(called, 0);
});

test('slow progress admits one outstanding acknowledgement and no queued reports', async () => {
  let release;
  const held = new Promise(resolve => { release = resolve; });
  const value = await setup(basic(async (_input, context) => {
    const first = context.progress('first');
    await assert.rejects(context.progress('second'), /previous progress/);
    await first;
    return {};
  }));
  value.state.intercept = request => request.method === 'tool.progress' ? held : undefined;
  value.invoke('progress'); await until(() => value.state.calls.some(call => call.method === 'tool.progress'));
  assert.equal(value.results().length, 0); assert.equal(value.state.calls.filter(call => call.method === 'tool.progress').length, 1);
  release(); await until(() => value.results().length === 1); await value.runtime.close();
});

test('expired invocations do not call handlers and output bounds fail once after execution', async () => {
  let called = 0; const value = await setup(basic(() => { called++; return { text: 'x'.repeat(600000) }; }));
  value.invoke('expired', { deadline_millis: String(Date.now() - 1) }); await until(() => value.results().length === 1);
  assert.equal(called, 0); assert.match(value.results()[0].params.failure, /deadline/);
  value.invoke('oversized'); await until(() => value.results().length === 2); assert.equal(called, 1);
  assert.match(value.results()[1].params.failure, /byte bound/); assert.equal(value.results()[1].params.output_base64, null);
  await value.runtime.close();
});


test('tool output transforms handler schema input once into the advertised wire output', async () => {
  let validations = 0;
  const output = { '~standard': { version: 1, vendor: 'transform',
    validate: value => { validations++; assert.equal(typeof value, 'string'); return { value: Number(value) }; },
    jsonSchema: { input: () => ({ type: 'string' }), output: () => ({ type: 'number' }) },
  } };
  const agent = basic(() => '42', { output });
  assert.deepEqual(agent.document.defaults.tools.lookup.output_schema, { type: 'number' });
  const value = await setup(agent); value.invoke('transform'); await until(() => value.results().length === 1);
  assert.equal(decodeJSON(value.results()[0].params.output_base64), 42); assert.equal(validations, 1);
  await value.runtime.close();
});

test('a cancellation/result race settles only that invocation and leaves the lease available', async () => {
  const value = await setup(basic(() => ({})));
  value.state.intercept = request => { if (request.method === 'tool.result' && request.params.invocation_id === 'late') throw new RemoteError({ code: -32009, kind: 'CONFLICT', message: 'already settled' }); };
  value.invoke('late'); await until(() => value.results().length === 1); await tick();
  assert.equal(value.state.closed, false);
  value.invoke('next'); await until(() => value.results().length === 2); assert.equal(value.state.closed, false);
  await value.runtime.close();
});

test('a pending progress/cancellation race does not close unrelated lease work', async () => {
  let reject;
  const pending = new Promise((_resolve, fail) => { reject = fail; });
  const value = await setup(basic((_input, context) => { void context.progress('notice'); return {}; }));
  value.state.intercept = request => request.method === 'tool.progress' && request.params.invocation_id === 'late-progress' ? pending : undefined;
  value.invoke('late-progress'); await until(() => value.state.calls.some(call => call.method === 'tool.progress')); await tick();
  reject(new RemoteError({ code: -32009, kind: 'CONFLICT', message: 'already cancelled' }));
  await until(() => value.runtime.active === 0); assert.equal(value.state.closed, false); assert.equal(value.results().length, 0);
  value.invoke('next'); await until(() => value.results().length === 1); assert.equal(value.results()[0].params.invocation_id, 'next');
  await value.runtime.close();
});

test('an explicit null output clears inheritance without inventing a validator', () => {
  const clear = defineAgent({ id: 'child', name: 'Child', output: null });
  const inherit = defineAgent({ id: 'child', name: 'Child' });
  assert.deepEqual(clear.document.defaults.output, { schema: null });
  assert.equal(clear.output, undefined);
  assert.equal(Object.hasOwn(inherit.document.defaults, 'output'), false);
  assert.notDeepEqual(clear.document, inherit.document);
});
