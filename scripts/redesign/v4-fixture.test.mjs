import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import http from 'node:http';
import net from 'node:net';
import { basename, join, resolve } from 'node:path';
import { promisify } from 'node:util';
import { test } from 'node:test';
import { Client, DeliveryError, RemoteError } from '../../packages/sdk/dist/index.js';
import { unixSocket } from '../../packages/sdk/dist/node.js';

const exec = promisify(execFile);
const deadline = () => ({ signal: AbortSignal.timeout(15_000) });

async function fixture() {
  const directory = await mkdtemp('/tmp/whip-v4-');
  const binary = join(directory, 'runtime');
  let child;
  let output = '';
  let info;
  const record = chunk => { output = (output + chunk.toString()).slice(-(1 << 20)); };
  const stop = async (signal = 'SIGTERM') => {
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    const exit = once(child, 'exit');
    child.kill(signal);
    const timeout = setTimeout(() => child.kill('SIGKILL'), 10_000);
    try { await exit; } finally { clearTimeout(timeout); }
  };
  const start = async (delay = '60ms') => {
    const args = ['-directory', join(directory, 'state'), '-workers', '1'];
    if (delay !== null) args.push('-scripted', '-scripted-delay', delay);
    child = spawn(binary, args, { stdio: ['ignore', 'pipe', 'pipe'] });
    child.stderr.on('data', record);
    child.stdout.on('data', record);
    let buffer = '';
    info = await new Promise((resolve, reject) => {
      const cleanup = () => { clearTimeout(timer); child.removeListener('error', failed); child.removeListener('exit', earlyExit); child.stdout.removeListener('data', data); };
      const failed = error => { cleanup(); reject(error); };
      const earlyExit = (code, signal) => failed(new Error(`Runtime exited before ready: ${code}/${signal}\n${output}`));
      const data = chunk => {
        buffer += chunk;
        const end = buffer.indexOf('\n');
        if (end < 0) return;
        try { const ready = JSON.parse(buffer.slice(0, end)); cleanup(); resolve(ready); } catch (error) { failed(error); }
      };
      const timer = setTimeout(() => failed(new Error('Runtime readiness timed out\n' + output)), 25_000);
      child.once('error', failed); child.once('exit', earlyExit); child.stdout.on('data', data);
    });
    return info;
  };
  try {
    await exec('go', ['build', ...(process.env.WHIP_SDK_RACE === '1' ? ['-race'] : []), '-o', binary, './cmd/whip-runtime'], { cwd: resolve('.'), timeout: 120_000 });
    await start();
  } catch (error) {
    await stop();
    await writeFile(join(directory, 'runtime.log'), output);
    throw new Error(`Fixture startup failed; retained ${directory}`, { cause: error });
  }
  return { directory, start, stop, get info() { return info; }, get pid() { return child.pid; }, get output() { return output; } };
}

async function dropAcknowledgement(path, upstream, requestID, onDrop) {
  const connections = new Set();
  const server = net.createServer(client => {
    const backend = net.connect(upstream);
    connections.add(client); connections.add(backend);
    const close = () => { client.destroy(); backend.destroy(); connections.delete(client); connections.delete(backend); };
    client.on('error', close); backend.on('error', close); client.on('close', close); backend.on('close', close);
    client.pipe(backend);
    let pending = '';
    backend.on('data', chunk => {
      pending += chunk;
      for (;;) {
        const end = pending.indexOf('\n'); if (end < 0) return;
        const line = pending.slice(0, end); pending = pending.slice(end + 1);
        const response = JSON.parse(line);
        if (response.result?.receipt?.identity.request_id === requestID) { onDrop(response.result); close(); return; }
        client.write(line + '\n');
      }
    });
  });
  server.listen(path); await once(server, 'listening');
  return async () => { for (const connection of connections) connection.destroy(); await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve())); };
}

async function until(read, predicate, timeout = 10_000) {
  const signal = AbortSignal.timeout(timeout);
  for (;;) {
    signal.throwIfAborted();
    const value = await read(); if (predicate(value)) return value;
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}

test('v4 SDK executes, recovers lost acknowledgements, and preserves queued input across SIGKILL', { timeout: 180_000 }, async t => {
  const runtime = await fixture();
  const evidence = [];
  let passed = false;
  let proxyClose;
  try {
    const client = await Client.connect(unixSocket(runtime.info.socket), { clientID: 'v4-fixture', ...deadline() });
    assert.equal(client.runtimeID, runtime.info.runtime_id);
    const createParams = {
      metadata: { title: 'v4 acceptance', archived: false, pinned: false },
      engine: 'starlark', policy: { max_depth: 8, max_sessions: 100, max_queued_inputs_per_session: 100 },
      definition: client.builtins[0], overrides: { model: { provider: 'scripted', name: 'scripted', effort: '' } },
      working_directory: runtime.directory,
    };
    const { root, tree } = await client.call('trees.create', createParams, deadline());
    const page = () => client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
    assert.deepEqual((await page()).items, []);

    let dropped;
    const proxy = join(runtime.directory, 'drop.sock');
    proxyClose = await dropAcknowledgement(proxy, runtime.info.socket, 'lost-ack', value => { dropped = value; });
    const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    const parts = [{ type: 'text', text: 'durable hello' }];
    await assert.rejects(unreliable.submit(root.id, parts, 'lost-ack', deadline()), DeliveryError);
    assert.ok(dropped?.input.id, 'proxy did not observe committed admission');
    await proxyClose(); proxyClose = undefined;
    const retry = await client.submit(root.id, parts, 'lost-ack', deadline());
    assert.equal(retry.input.id, dropped.input.id);
    await assert.rejects(client.submit(root.id, [{ type: 'text', text: 'different' }], 'lost-ack', deadline()), error => error instanceof RemoteError && error.kind === 'CONFLICT');
    const complete = await client.wait('lost-ack', deadline());
    assert.equal(complete.turn.state, 'succeeded');
    const ledger = await client.call('turns.attempts', { turn_id: complete.turn.id, limit: 100 }, deadline());
    assert.equal(ledger.items.length, 1);
    assert.equal(ledger.items[0].state, 'succeeded');
    assert.equal(ledger.items[0].cost_nano_usd, '0');
    assert.equal(ledger.items[0].result.usage.input, null, 'scripted provider does not invent token usage');
    const firstHistory = await page();
    assert.deepEqual(firstHistory.items.map(message => message.role), ['user', 'assistant']);
    assert.equal(firstHistory.items[1].parts[0].text, 'ack: durable hello');
    assert.equal(ledger.items[0].message_id, firstHistory.items[1].id);
    evidence.push({ dropped, complete, firstHistory });

    await Promise.all(Array.from({ length: 6 }, (_, index) => client.submit(root.id, [{ type: 'text', text: 'parallel-' + index }], 'parallel-' + index, deadline())));
    await Promise.all(Array.from({ length: 6 }, (_, index) => client.wait('parallel-' + index, deadline())));
    const concurrentHistory = await page();
    assert.equal(concurrentHistory.items.length, 14);
    assert.deepEqual(concurrentHistory.items.map(message => message.role), Array.from({ length: 7 }, () => ['user', 'assistant']).flat());

    const abort = new AbortController();
    await client.submit(root.id, [{ type: 'text', text: 'observer abort' }], 'abort-wait', deadline());
    const waiting = client.wait('abort-wait', { signal: abort.signal }); abort.abort();
    await assert.rejects(waiting, error => error.name === 'AbortError');
    const detached = await Client.connect(unixSocket(runtime.info.socket), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    assert.equal((await detached.wait('abort-wait', deadline())).turn.state, 'succeeded');
    const beforeCrash = await page();

    const configured = await client.call('sessions.configure', { session_id: root.id, expected_revision: root.config_revision, patch: { instructions: { text: 'inherit me', project_files: [], discover_skills: false } } }, deadline());
    const child = await client.call('sessions.spawn', { parent_id: root.id, overrides: {} }, deadline());
    assert.equal(child.configuration.instructions.text, configured.configuration.instructions.text);
    const sessions = await client.call('sessions.list', { tree_id: tree.id, limit: 100 }, deadline());
    assert.equal(sessions.items.length, 2);
    await client.submit(child.id, [{ type: 'text', text: 'child' }], 'child', deadline());
    assert.equal((await client.wait('child', deadline())).turn.state, 'succeeded');

    await runtime.stop(); await runtime.start('1h');
    await client.submit(root.id, [{ type: 'text', text: 'interrupted' }], 'claimed-before-kill', deadline());
    const claimed = await until(() => client.recover('claimed-before-kill', deadline()), value => value.turn?.state === 'running');
    await until(() => client.call('turns.attempts', { turn_id: claimed.turn.id, limit: 100 }, deadline()), value => value.items.some(attempt => attempt.state === 'dispatched'));
    const queued = await client.submit(root.id, [{ type: 'text', text: 'survives queue' }], 'queued-before-kill', deadline());
    assert.equal(queued.input.state, 'queued'); assert.equal(queued.turn, null);
    const cancelled = await client.submit(root.id, [{ type: 'text', text: 'cancel me' }], 'cancel-queue', deadline());
    await client.call('inputs.cancel', { input_id: cancelled.input.id }, deadline());
    assert.equal((await client.wait('cancel-queue', deadline())).input.state, 'cancelled');
    const pid = runtime.pid;
    await runtime.stop('SIGKILL'); await runtime.start('0');
    assert.notEqual(runtime.pid, pid); assert.equal(runtime.info.runtime_id, client.runtimeID);
    const interrupted = await client.wait('claimed-before-kill', deadline());
    assert.equal(interrupted.turn.state, 'interrupted'); assert.equal(interrupted.turn.id, claimed.turn.id);
    const interruptedLedger = await client.call('turns.attempts', { turn_id: interrupted.turn.id, limit: 100 }, deadline());
    assert.equal(interruptedLedger.items.length, 1);
    assert.equal(interruptedLedger.items[0].state, 'uncertain');
    const resumed = await client.wait('queued-before-kill', deadline());
    assert.equal(resumed.input.id, queued.input.id); assert.equal(resumed.turn.state, 'succeeded');
    const afterCrash = await page();
    assert.deepEqual(afterCrash.items.slice(0, beforeCrash.items.length), beforeCrash.items);
    assert.deepEqual(afterCrash.items.slice(beforeCrash.items.length).map(message => message.role), ['user', 'user', 'assistant']);
    assert.equal(afterCrash.items.at(-1).parts[0].text, 'ack: survives queue');
    assert.equal((await client.submit(root.id, parts, 'lost-ack', deadline())).input.id, dropped.input.id);
    evidence.push({ claimed, queued, interrupted, resumed, afterCrash });

    const example = await exec(process.execPath, ['packages/sdk/examples/session.mjs', runtime.info.socket], { timeout: 20_000 });
    assert.equal(JSON.parse(example.stdout).completed.turn.state, 'succeeded');
    await assert.rejects(Client.connect(unixSocket(runtime.info.socket), { clientID: 'wrong', expectedRuntimeID: 'different', ...deadline() }), error => error instanceof RemoteError && error.kind === 'IDENTITY');
    await providerAcceptance(runtime, client, createParams, evidence);
    await engineAcceptance(runtime, client, createParams, evidence);
    await operationAcceptance(runtime, client, createParams, evidence);
    await streamAcceptance(runtime, client, createParams, evidence);
    passed = true;
  } finally {
    await proxyClose?.();
    await runtime.stop();
    if (passed) await rm(runtime.directory, { recursive: true, force: true });
    else {
      const artifacts = join('test-results/redesign', basename(runtime.directory));
      await mkdir(artifacts, { recursive: true });
      await cp(runtime.directory, artifacts, { recursive: true, filter: path => !path.endsWith('/runtime') && !path.endsWith('.sock') });
      await writeFile(join(artifacts, 'runtime.log'), runtime.output);
      await writeFile(join(artifacts, 'observations.json'), JSON.stringify(evidence, null, 2) + '\n');
      t.diagnostic(`Failure artifacts: ${artifacts}; original: ${runtime.directory}`);
    }
  }
});

// Exercise the real HTTP adapter through the command, socket and SDK. This is
// deterministic local coverage; the separate live-provider smoke remains required.
async function providerAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const server = http.createServer(async (request, response) => {
    let body = '';
    for await (const chunk of request) body += chunk;
    requests.push({ path: request.url, body: JSON.parse(body) });
    response.setHeader('content-type', 'application/json');
    if (requests.length === 1) {
      response.writeHead(429);
      response.end(JSON.stringify({ error: 'retry this rejected attempt' }));
      return;
    }
    response.end(JSON.stringify({
      choices: [{ message: { role: 'assistant', content: 'HTTP adapter completed' }, finish_reason: 'stop' }],
      usage: { prompt_tokens: 10, completion_tokens: 3, cost: 0.000000001 },
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.fixture = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: { 'fixture-model': { max_output_tokens: 123, timeout_millis: 2000, max_attempts: 2 } },
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    const { root } = await client.call('trees.create', {
      ...createParams, overrides: { model: { provider: 'fixture', name: 'fixture-model', effort: '' } },
    }, deadline());
    const image = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a7S8AAAAASUVORK5CYII=';
    const upload = { session_id: root.id, reference_id: 'sdk-image', media_type: 'image/png', data_base64: image };
    const reference = await client.call('content.put', upload, deadline());
    assert.deepEqual(await client.call('content.put', upload, deadline()), reference);
    const read = await client.call('content.read', { session_id: root.id, reference_id: reference.id }, deadline());
    assert.equal(read.data_base64, image);
    const { root: foreign } = await client.call('trees.create', createParams, deadline());
    await assert.rejects(client.call('content.read', { session_id: foreign.id, reference_id: reference.id }, deadline()), error => error instanceof RemoteError && error.kind === 'NOT_FOUND');
    await assert.rejects(client.submit(foreign.id, [{ type: 'content', reference_id: reference.id }], 'foreign-content', deadline()), error => error instanceof RemoteError && error.kind === 'NOT_FOUND');
    await client.submit(root.id, [{ type: 'text', text: 'HTTP provider round trip' }, { type: 'content', reference_id: reference.id }], 'http-provider', deadline());
    const result = await client.wait('http-provider', deadline());
    assert.equal(result.turn.state, 'succeeded');
    const ledger = await client.call('turns.attempts', { turn_id: result.turn.id, limit: 100 }, deadline());
    const history = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
    assert.deepEqual(ledger.items.map(item => item.state), ['failed', 'succeeded']);
    assert.deepEqual(ledger.items.map(item => item.cost_nano_usd), [null, '1']);
    assert.equal(ledger.items[0].logical_id, ledger.items[1].logical_id);
    assert.equal(ledger.items[0].request.request_digest, ledger.items[1].request.request_digest);
    assert.equal(ledger.items[1].result.usage.input, '10');
    assert.equal(ledger.items[1].result.usage.cached_input, null);
    assert.equal(history.items.length, 2);
    assert.deepEqual(history.items[0].parts[1], { type: 'content', reference_id: reference.id });
    assert.equal(history.items[1].parts[0].text, 'HTTP adapter completed');
    assert.equal(ledger.items[1].message_id, history.items[1].id);
    assert.equal(requests.length, 2);
    assert.deepEqual(requests[0], requests[1]);
    assert.equal(requests[0].path, '/v1/chat/completions');
    assert.equal(requests[0].body.max_completion_tokens, 123);
    assert.equal(requests[0].body.messages.at(-1).content[1].image_url.url, 'data:image/png;base64,' + image);
    evidence.push({ httpProvider: { result, ledger, history, requests } });
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}

async function engineAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const awaitingFinal = new Set();
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    requests.push(body);
    const last = body.messages.at(-1);
    const prompt = body.messages.findLast(item => item.role === 'user').content;
    const engine = prompt.split(':')[0];
    let message;
    let finish_reason;
    if (last.role === 'tool') {
      if (prompt.endsWith(':kill-after-cell')) {
        awaitingFinal.add(engine);
        return; // The fixture kills the runtime after this cell's durable commit.
      }
      message = { role: 'assistant', content: last.content };
      finish_reason = 'stop';
    } else {
      let code;
      if (prompt.endsWith(':first')) code = engine === 'starlark' ? 'x = 40\nprint(x)' : 'var x = 40; console.log(x)';
      else if (prompt.endsWith(':kill-after-cell')) code = engine === 'starlark' ? 'x += 2\nprint(x)' : 'x += 2; console.log(x)';
      else code = engine === 'starlark' ? 'print(x)' : 'console.log(x)';
      message = { role: 'assistant', content: null, tool_calls: [{ id: 'engine-call', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
      finish_reason = 'tool_calls';
    }
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message, finish_reason }], usage: { prompt_tokens: 12, completion_tokens: 4, cost: 0 } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.engine = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: { engine: { max_output_tokens: 500, timeout_millis: 30000, max_attempts: 1 } },
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    for (const engine of ['starlark', 'quickjs']) {
      const { root } = await client.call('trees.create', {
        ...createParams, engine, overrides: { model: { provider: 'engine', name: 'engine', effort: '' } },
      }, deadline());
      const history = () => client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
      const firstID = `${engine}:first`;
      await client.submit(root.id, [{ type: 'text', text: firstID }], firstID, deadline());
      const first = await client.wait(firstID, deadline());
      assert.equal(first.turn.state, 'succeeded');
      const firstHistory = await history();
      assert.deepEqual(firstHistory.items.map(item => item.role), ['user', 'assistant', 'tool', 'assistant']);
      assert.equal(firstHistory.items[1].parts[0].call.name, 'execute');
      assert.equal(firstHistory.items[2].parts[0].result.call_id, 'engine-call');
      assert.equal(JSON.parse(firstHistory.items.at(-1).parts[0].text).result.output, '40\n');
      const attempts = await client.call('turns.attempts', { turn_id: first.turn.id, limit: 100 }, deadline());
      assert.equal(attempts.items.length, 2);
      assert.notEqual(attempts.items[0].logical_id, attempts.items[1].logical_id);
      await client.call('sessions.configure', {
        session_id: root.id, expected_revision: root.config_revision,
        patch: { model: { provider: 'engine', name: 'changed-model', effort: '' } },
      }, deadline());
      const killedID = `${engine}:kill-after-cell`;
      await client.submit(root.id, [{ type: 'text', text: killedID }], killedID, deadline());
      await until(async () => awaitingFinal.has(engine), value => value);
      const beforeKill = await history();
      assert.equal(beforeKill.items.at(-1).role, 'tool');
      assert.equal(JSON.parse(beforeKill.items.at(-1).parts[0].result.output).result.output, '42\n');
      await runtime.stop('SIGKILL');
      await runtime.start(null);
      const interrupted = await client.wait(killedID, deadline());
      assert.equal(interrupted.turn.state, 'interrupted');
      const interruptedAttempts = await client.call('turns.attempts', { turn_id: interrupted.turn.id, limit: 100 }, deadline());
      assert.deepEqual(interruptedAttempts.items.map(item => item.state), ['succeeded', 'uncertain']);
      assert.deepEqual(await history(), beforeKill);
      const readID = `${engine}:read-after-kill`;
      await client.submit(root.id, [{ type: 'text', text: readID }], readID, deadline());
      const read = await client.wait(readID, deadline());
      assert.equal(read.turn.state, 'succeeded');
      const after = await history();
      assert.equal(JSON.parse(after.items.at(-1).parts[0].text).result.output, '42\n');
      assert.equal(after.items.filter(item => item.role === 'tool').length, 3);
      evidence.push({ engine, first, attempts, beforeKill, interrupted, interruptedAttempts, after });
    }
    assert.ok(requests.every(request => request.tools?.[0]?.function.name === 'execute'));
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}


// A completed external write and an unapproved second write share one unfinished
// cell. Killing the actual runtime must retain the first outcome, cancel the
// second waiter, and mark only the lost interpreter boundary uncertain.
async function operationAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    const prompt = body.messages.findLast(item => item.role === 'user').content;
    requests.push(prompt);
    const engine = prompt.split(':')[0];
    const code = engine === 'starlark'
      ? 'files.write(path="completed.txt", content="effect completed")\nfiles.write(path="never.txt", content="must not happen")'
      : 'await files.write({path:"completed.txt",content:"effect completed"}); await files.write({path:"never.txt",content:"must not happen"})';
    const message = prompt.endsWith(':inspect-after-restart')
      ? { role: 'assistant', content: 'history remains available' }
      : { role: 'assistant', content: null, tool_calls: [{ id: 'files-call', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 12, completion_tokens: 4, cost: 0 } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.operations = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: { operations: { max_output_tokens: 500, timeout_millis: 30000, max_attempts: 1 } },
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    for (const engine of ['starlark', 'quickjs']) {
      const workspace = join(runtime.directory, engine + '-effects');
      await mkdir(workspace);
      const { root } = await client.call('trees.create', {
        ...createParams, engine, working_directory: workspace,
        overrides: { model: { provider: 'operations', name: 'operations', effort: '' } },
      }, deadline());
      const requestID = engine + ':kill-during-effects';
      await client.submit(root.id, [{ type: 'text', text: requestID }], requestID, deadline());
      const permissions = () => client.call('permissions.list', { session_id: root.id, limit: 100 }, deadline());
      const pending = await until(permissions, page => page.items.some(item => item.state === 'pending'), 30_000);
      const firstPermission = pending.items.find(item => item.state === 'pending');
      const firstOperation = await client.call('operations.get', { operation_id: firstPermission.operation_id }, deadline());
      assert.equal(firstOperation.session_id, root.id);
      assert.equal(firstOperation.capability, 'files.write');
      assert.equal(firstOperation.resource, workspace);
      assert.equal(firstOperation.arguments.path, 'completed.txt');
      assert.equal(firstOperation.dispatched_at, null);
      await assert.rejects(readFile(join(workspace, 'completed.txt')), { code: 'ENOENT' });
      await client.call('permissions.resolve', { operation_id: firstOperation.id, approved: true }, deadline());
      const secondPage = await until(permissions, page => page.items.some(item => item.state === 'pending' && item.operation_id !== firstOperation.id));
      const secondPermission = secondPage.items.find(item => item.state === 'pending');
      const operations = () => client.call('turns.operations', { turn_id: firstOperation.turn_id, limit: 100 }, deadline());
      const cells = () => client.call('turns.cells', { turn_id: firstOperation.turn_id, limit: 100 }, deadline());
      const beforeKill = await operations();
      assert.equal(beforeKill.items.find(item => item.id === firstOperation.id).state, 'succeeded');
      assert.equal(beforeKill.items.find(item => item.id === secondPermission.operation_id).state, 'waiting');
      assert.equal(await readFile(join(workspace, 'completed.txt'), 'utf8'), 'effect completed');
      const grants = await client.call('grants.list', { session_id: root.id, limit: 100 }, deadline());
      assert.equal(grants.items.length, 1);
      assert.equal(grants.items[0].operation_id, firstOperation.id);
      const beforeCells = await cells();
      assert.equal(beforeCells.items.length, 1);
      assert.equal(beforeCells.items[0].state, 'running');
      assert.equal(beforeCells.items[0].checkpoint, null);
      await runtime.stop('SIGKILL');
      await writeFile(join(workspace, 'completed.txt'), 'external change after crash');
      await runtime.start(null);
      const interrupted = await client.wait(requestID, deadline());
      assert.equal(interrupted.turn.state, 'interrupted');
      const afterKill = await operations();
      assert.deepEqual(afterKill.items.find(item => item.id === firstOperation.id), beforeKill.items.find(item => item.id === firstOperation.id));
      const cancelled = afterKill.items.find(item => item.id === secondPermission.operation_id);
      assert.equal(cancelled.state, 'cancelled');
      assert.equal(cancelled.dispatched_at, null);
      assert.equal((await permissions()).items.find(item => item.operation_id === cancelled.id).state, 'cancelled');
      await assert.rejects(client.call('permissions.resolve', { operation_id: cancelled.id, approved: true }, deadline()), error => error instanceof RemoteError && error.kind === 'CONFLICT');
      const afterCells = await cells();
      assert.equal(afterCells.items[0].state, 'uncertain');
      assert.equal(afterCells.items[0].checkpoint, null);
      assert.ok(afterCells.items[0].result_message_id);
      assert.deepEqual(await client.call('cells.get', { cell_id: afterCells.items[0].id }, deadline()), afterCells.items[0]);
      const inspectID = engine + ':inspect-after-restart';
      await client.submit(root.id, [{ type: 'text', text: inspectID }], inspectID, deadline());
      assert.equal((await client.wait(inspectID, deadline())).turn.state, 'succeeded');
      assert.equal(await readFile(join(workspace, 'completed.txt'), 'utf8'), 'external change after crash');
      await assert.rejects(readFile(join(workspace, 'never.txt')), { code: 'ENOENT' });
      assert.equal(requests.filter(prompt => prompt === requestID).length, 1);
      evidence.push({ engine, beforeKill, beforeCells, afterKill, afterCells, interrupted });
    }
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}


async function streamAcceptance(runtime, client, createParams, evidence) {
  const completions = new Map();
  const requests = [];
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    requests.push(body);
    const prompt = body.messages.findLast(item => item.role === 'user').content;
    response.setHeader('content-type', 'text/event-stream');
    response.write('data: ' + JSON.stringify({ choices: [{ index: 0, delta: { role: 'assistant', content: 'partial' }, finish_reason: null }] }) + '\n\n');
    completions.set(prompt, () => {
      response.write('data: ' + JSON.stringify({ choices: [{ index: 0, delta: { content: ' completed' }, finish_reason: 'stop' }] }) + '\n\n');
      response.write('data: ' + JSON.stringify({ choices: [], usage: { prompt_tokens: 14, completion_tokens: 4, cost: 0 } }) + '\n\n');
      response.end('data: [DONE]\n\n');
      completions.delete(prompt);
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.stream = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: { stream: { max_output_tokens: 100, timeout_millis: 30000, max_attempts: 1 } },
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    const { root } = await client.call('trees.create', {
      ...createParams, overrides: { model: { provider: 'stream', name: 'stream', effort: '' } },
    }, deadline());
    const observe = () => client.call('sessions.observe', { session_id: root.id, after: '0', limit: 100 }, deadline());
    await client.submit(root.id, [{ type: 'text', text: 'stream:complete' }], 'stream:complete', deadline());
    const provisional = await until(observe, view => view.preview?.text === 'partial');
    assert.deepEqual(provisional.messages.map(message => message.role), ['user']);
    assert.equal(provisional.preview.truncated, false);
    const iterator = client.observe(root.id);
    const previewPage = (await iterator.next()).value;
    assert.equal(previewPage.preview.attempt_id, provisional.preview.attempt_id);
    completions.get('stream:complete')();
    const completed = await client.wait('stream:complete', deadline());
    assert.equal(completed.turn.state, 'succeeded');
    let committed;
    for (;;) {
      const page = (await iterator.next()).value;
      if (page.messages.some(message => message.id === provisional.preview.message_id)) { committed = page; break; }
    }
    await iterator.return();
    assert.equal(committed.preview, null);
    assert.equal(committed.messages.filter(message => message.id === provisional.preview.message_id).length, 1);
    assert.equal(committed.messages.at(-1).parts[0].text, 'partial completed');
    const ledger = await client.call('turns.attempts', { turn_id: completed.turn.id, limit: 100 }, deadline());
    assert.equal(ledger.items[0].id, provisional.preview.attempt_id);
    assert.equal(ledger.items[0].message_id, provisional.preview.message_id);
    assert.equal(ledger.items[0].result.usage.input, '14');
    assert.equal(ledger.items[0].cost_nano_usd, '0');

    await client.submit(root.id, [{ type: 'text', text: 'stream:crash' }], 'stream:crash', deadline());
    const beforeKill = await until(observe, view => view.preview?.text === 'partial');
    await runtime.stop('SIGKILL');
    await runtime.start(null);
    const interrupted = await client.wait('stream:crash', deadline());
    assert.equal(interrupted.turn.state, 'interrupted');
    const afterKill = await observe();
    assert.notEqual(afterKill.epoch, beforeKill.epoch);
    assert.equal(afterKill.preview, null);
    assert.deepEqual(afterKill.messages, beforeKill.messages);
    assert.equal(afterKill.messages.some(message => message.id === beforeKill.preview.message_id), false);
    const interruptedAttempts = await client.call('turns.attempts', { turn_id: interrupted.turn.id, limit: 100 }, deadline());
    assert.equal(interruptedAttempts.items[0].state, 'uncertain');
    assert.equal(interruptedAttempts.items[0].message_id, null);
    assert.equal(requests.filter(request => request.messages.at(-1).content === 'stream:crash').length, 1);

    await client.submit(root.id, [{ type: 'text', text: 'stream:observer-abort' }], 'stream:observer-abort', deadline());
    await until(observe, view => view.preview?.text === 'partial');
    const controller = new AbortController();
    const observer = client.observe(root.id, { signal: controller.signal });
    await observer.next();
    controller.abort();
    await assert.rejects(observer.next(), error => error.name === 'AbortError');
    assert.equal((await client.recover('stream:observer-abort', deadline())).turn.state, 'running');
    completions.get('stream:observer-abort')();
    assert.equal((await client.wait('stream:observer-abort', deadline())).turn.state, 'succeeded');

    await client.submit(root.id, [{ type: 'text', text: 'stream:cancel' }], 'stream:cancel', deadline());
    const cancelling = await until(observe, view => view.preview?.text === 'partial');
    await client.call('turns.cancel', { turn_id: cancelling.preview.turn_id }, { signal: AbortSignal.timeout(3000) });
    assert.equal((await client.wait('stream:cancel', deadline())).turn.state, 'cancelled');
    const afterCancel = await observe();
    assert.equal(afterCancel.preview, null);
    assert.equal(afterCancel.messages.some(message => message.id === cancelling.preview.message_id), false);
    assert.ok(requests.every(request => request.stream === true && request.stream_options?.include_usage === true));
    evidence.push({ provisional, committed, ledger, beforeKill, afterKill, interrupted, interruptedAttempts, afterCancel });
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}
