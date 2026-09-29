import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import http from 'node:http';
import net from 'node:net';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { test } from 'node:test';
import { Client, ExecutorClient } from '../../packages/sdk/dist/index.js';
import { unixSocket, executorSocket } from '../../packages/sdk/dist/node.js';

const deadline = () => ({ signal: AbortSignal.timeout(15_000) });
const base64 = value => Buffer.from(value).toString('base64');
const identity = event => ({ epoch: event.epoch, generation: event.generation, invocation_id: event.invocation_id });
async function until(read, predicate) {
  const end = Date.now() + 15_000;
  while (Date.now() < end) { const value = await read(); if (predicate(value)) return value; await new Promise(resolve => setTimeout(resolve, 5)); }
  throw new Error('Observation timed out');
}
async function rawOperation(socket, operationID) {
  const connection = net.connect(socket); await once(connection, 'connect');
  try {
    connection.setTimeout(15_000, () => connection.destroy(new Error('Raw observation timed out')));
    let pending = '';
    const lines = [];
    const consume = async () => {
      while (!lines.length) {
        const [chunk] = await once(connection, 'data'); pending += chunk.toString();
        let end; while ((end = pending.indexOf('\n')) >= 0) { lines.push(pending.slice(0, end)); pending = pending.slice(end + 1); }
      }
      return lines.shift();
    };
    connection.write(JSON.stringify({ jsonrpc: '2.0', id: 'initialize', method: 'initialize', params: { major: 4 } }) + '\n');
    await consume();
    connection.write(JSON.stringify({ jsonrpc: '2.0', id: 'operation', method: 'operations.get', params: { operation_id: operationID } }) + '\n');
    return await consume();
  } finally { connection.destroy(); }
}

test('production executor socket preserves captured hooks, exact payloads, progress and disconnect uncertainty', { timeout: 180_000 }, async t => {
  const directory = await mkdtemp('/tmp/whip-executor-');
  let child, executor;
  const stop = async () => {
    await executor?.close(); executor = undefined;
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    const exited = once(child, 'exit'); child.kill('SIGTERM');
    const timer = setTimeout(() => child.kill('SIGKILL'), 10_000);
    try { await exited; } finally { clearTimeout(timer); }
  };
  const server = http.createServer(async (request, response) => {
    let raw = ''; for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw), last = body.messages.at(-1);
    const prompt = body.messages.findLast(item => item.role === 'user').content;
    const code = prompt.startsWith('quickjs') ? "console.log((await tools.lookup({id:'item'})).value)" : 'print(tools.lookup(id="item")["value"])';
    const message = last.role === 'tool' ? { role: 'assistant', content: 'completed' } : { role: 'assistant', content: null, tool_calls: [{ id: 'custom-call', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 2, completion_tokens: 1, cost: 0 } }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(async () => { await stop(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); await rm(directory, { recursive: true, force: true }); });
  const binary = join(directory, 'runtime');
  await promisify(execFile)('go', ['build', '-race=false', '-o', binary, './cmd/whip-runtime'], { timeout: 120_000 });
  let diagnostics = '';
  const start = async () => {
    child = spawn(binary, ['-directory', join(directory, 'state'), '-workers', '1'], { stdio: ['ignore', 'pipe', 'pipe'] });
    child.stderr.on('data', chunk => { diagnostics = (diagnostics + chunk).slice(-(1 << 20)); });
    return await new Promise((resolve, reject) => {
      let text = '';
      const timer = setTimeout(() => reject(new Error('Runtime readiness timed out: ' + diagnostics)), 20_000);
      child.once('error', reject); child.once('exit', code => { clearTimeout(timer); reject(new Error('Runtime exited: ' + code + diagnostics)); });
      child.stdout.on('data', chunk => { text += chunk; const end = text.indexOf('\n'); if (end < 0) return; clearTimeout(timer); resolve(JSON.parse(text.slice(0, end))); });
    });
  };
  let info = await start();
  let client = await Client.connect(unixSocket(info.socket), { clientID: 'executor-fixture', ...deadline() });
  const inventory = await client.listProviders(deadline());
  await client.createProvider({ revision: inventory.revision, provider: 'fixture', keep_credential: false, key: null,
    declaration: { kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential: { source: 'none', environment: '', file: '', command: null }, models: {} } }, deadline());
  const definition = await client.call('definitions.register', { id: 'executor-fixture', name: 'Executor fixture', defaults: { modules: [],
    tools: { lookup: { timeout_millis: 900000, description: 'Lookup', input_schema: { type: 'object', required: ['id'], properties: { id: { type: 'string' }, count: { type: 'integer' } }, additionalProperties: false }, output_schema: { type: 'object', required: ['value'], properties: { value: { type: 'integer' } }, additionalProperties: false } } },
    hooks: { turn_start: { operations: null, optional: false, timeout_millis: 10000 }, before_tool: { operations: ['tools.lookup'], optional: false, timeout_millis: 10000 } },
  } }, deadline());
  executor = await ExecutorClient.connect(await executorSocket(info.socket), { expectedRuntimeID: client.runtimeID, ...deadline() });
  const lease = await executor.bind({ definition: definition.ref, tools: ['lookup'], hooks: ['before_tool', 'turn_start'] }, deadline());
  const events = executor.events();
  const hook = async (name, extra = {}) => {
    const { value: event } = await events.next(); assert.equal(event.method, 'executor.invoke'); assert.equal(event.invocation.name, name);
    await executor.hookResult({ ...identity(event), decision: '', reason: '', arguments_base64: null, spawn_base64: null, context: '', failure: '', ...extra }, deadline()); return event;
  };
  const roots = [];
  for (const engine of ['starlark', 'quickjs']) {
    const { root } = await client.call('trees.create', { creation_id: `executor-${engine}`, metadata: { title: engine, archived: false, pinned: false }, engine, definition: definition.ref, working_directory: directory, overrides: { model: { provider: 'fixture', name: 'test', effort: '' } } }, deadline());
    roots.push(root);
    await client.call('grants.create', { id: 'grant-' + engine, session_id: root.id, capability: 'tools.lookup', resource: definition.ref.id + '@' + definition.ref.revision }, deadline());
    await client.submit(root.id, [{ type: 'text', text: engine }], engine, deadline());
    await hook('turn_start', { context: 'captured contribution' });
    await hook('before_tool', { arguments_base64: base64('{"id":"item","count":9007199254740993}') });
    const { value: event } = await events.next();
    assert.equal(event.invocation.kind, 'tool'); assert.equal(event.invocation.session_id, root.id);
    assert.equal(Buffer.from(event.invocation.arguments_base64, 'base64').toString(), '{"count":9007199254740993,"id":"item"}');
    assert.equal((await client.call('operations.get', { operation_id: event.invocation.operation_id }, deadline())).state, 'dispatched');
    const pending = await executor.pending({ epoch: lease.epoch, definition: lease.definition, generation: lease.generation, after: null }, deadline());
    assert.equal(pending.items[0].invocation_id, event.invocation_id);
    await executor.progress({ ...identity(event), text: '😀'.repeat(512) }, deadline());
    const activity = await until(() => client.executorActivity(root.id, deadline()), value => value.activity?.progress !== null);
    assert.equal(activity.activity.progress.invocation_id, event.invocation_id); assert.equal(Buffer.byteLength(activity.activity.progress.text), 2048);
    await executor.result({ ...identity(event), output_base64: base64('{"value":9007199254740993}'), failure: '' }, deadline());
    assert.equal((await client.wait(engine, deadline())).turn.state, 'succeeded');
    const raw = await rawOperation(info.socket, event.invocation.operation_id);
    assert.match(raw, /"value":9007199254740993/); assert.match(raw, /"count":9007199254740993/);
    assert.equal((await client.executorActivity(root.id, deadline())).activity, null);
    await assert.rejects(executor.result({ ...identity(event), output_base64: base64('null'), failure: '' }, deadline()), error => error.kind === 'CONFLICT');
  }
  await client.submit(roots[0].id, [{ type: 'text', text: 'starlark-disconnect' }], 'disconnect', deadline());
  await hook('turn_start'); await hook('before_tool');
  const { value: interrupted } = await events.next(); await executor.close(); executor = undefined;
  await client.wait('disconnect', deadline());
  assert.equal((await client.call('operations.get', { operation_id: interrupted.invocation.operation_id }, deadline())).state, 'uncertain');
  await stop(); info = await start();
  client = await Client.connect(unixSocket(info.socket), { clientID: 'executor-fixture', expectedRuntimeID: client.runtimeID, ...deadline() });
  executor = await ExecutorClient.connect(await executorSocket(info.socket), { expectedRuntimeID: client.runtimeID, ...deadline() });
  const restarted = await executor.bind({ definition: definition.ref, tools: ['lookup'], hooks: ['before_tool', 'turn_start'] }, deadline());
  assert.notEqual(restarted.epoch, lease.epoch);
  assert.deepEqual((await executor.pending({ epoch: restarted.epoch, definition: restarted.definition, generation: restarted.generation, after: null }, deadline())).items, []);
  await assert.rejects(executor.result({ ...identity(interrupted), output_base64: base64('null'), failure: '' }, deadline()), error => error.kind === 'CONFLICT');
  assert.equal((await client.executorActivity(roots[0].id, deadline())).activity, null);
});
