import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
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

async function dropAcknowledgement(path, upstream, requestID, onDrop, matches = response => response.result?.receipt?.identity.request_id === requestID) {
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
        if (matches(response)) { onDrop(response.result); close(); return; }
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
      engine: 'starlark', resources: [{ kind: 'depth', limit: '8' }, { kind: 'descendants', limit: '99' }, { kind: 'queued_inputs', limit: '100' }],
      definition: client.builtins[0], overrides: { report_mode: 'message', model: { provider: 'scripted', name: 'scripted', effort: '' } },
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
    const spawnParams = { parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'child' }], grant_ids: null };
    const spawned = await client.spawn(spawnParams, 'child', deadline());
    const child = spawned.session;
    assert.equal(spawned.admission.input.session_id, child.id);
    assert.equal((await client.spawn(spawnParams, 'child', deadline())).admission.input.id, spawned.admission.input.id);
    await assert.rejects(client.spawn({ ...spawnParams, grant_ids: [] }, 'child', deadline()), error => error.kind === 'CONFLICT');
    assert.equal(child.configuration.instructions.text, configured.configuration.instructions.text);
    const sessions = await client.call('sessions.list', { tree_id: tree.id, limit: 100 }, deadline());
    assert.equal(sessions.items.length, 2);
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
    await resourceAcceptance(runtime, client, createParams, evidence);
    await budgetAcceptance(client, createParams, evidence);
    await writeAllowanceAcceptance(runtime, client, createParams, evidence);
    await mailAcceptance(runtime, client, createParams, evidence);
    await stateAcceptance(runtime, client, createParams, evidence);
    await stateSubscriptionAcceptance(runtime, client, createParams, evidence);
    await completionAcceptance(runtime, client, createParams, evidence);
    await providerAcceptance(runtime, client, createParams, evidence);
    await outputAcceptance(runtime, client, createParams, evidence);
    await contextAcceptance(runtime, client, createParams, evidence);
    await contextRecoveryAcceptance(runtime, client, createParams, evidence);
    await contextPolicyAcceptance(runtime, client, createParams, evidence);
    await instructionAcceptance(runtime, client, createParams, evidence);
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


async function budgetAcceptance(client, createParams, evidence) {
  const { root } = await client.call('trees.create', createParams, deadline());
  const initial = await client.call('budgets.list', { session_id: root.id }, deadline());
  assert.equal(initial.items.length, 6);
  assert.ok(initial.items.filter(budget => budget.kind.startsWith('model_')).every(budget => budget.limit === null && budget.revision === '0' && budget.used === '0'));
  assert.equal(initial.items.find(budget => budget.kind === 'logical_writes').limit, '100000');
  assert.equal(initial.items.find(budget => budget.kind === 'logical_write_bytes').limit, '1073741824');
  const limited = await client.call('budgets.set', { session_id: root.id, expected_revision: '0', budget: { kind: 'model_calls', limit: '2' } }, deadline());
  assert.equal(limited.revision, '1');
  await assert.rejects(client.call('budgets.set', { session_id: root.id, expected_revision: '0', budget: { kind: 'model_calls', limit: '3' } }, deadline()), error => error.kind === 'CONFLICT');
  await client.submit(root.id, [{ type: 'text', text: 'one parent request' }], 'budget:parent', deadline());
  assert.equal((await client.wait('budget:parent', deadline())).turn.state, 'succeeded');
  const child = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'one child request' }], grant_ids: [], budgets: [{ kind: 'model_calls', limit: '1' }] }, 'budget:child', deadline());
  assert.equal((await client.wait('budget:child', deadline())).turn.state, 'succeeded');
  const inspect = () => client.call('budgets.list', { session_id: root.id }, deadline());
  const charged = await inspect();
  const calls = charged.items.find(budget => budget.kind === 'model_calls');
  assert.equal(calls.used, '2'); assert.equal(calls.reserved, '0'); assert.equal(calls.uncertain, '0');
  await client.call('sessions.delete', { session_id: child.session.id }, deadline());
  assert.equal((await inspect()).items.find(budget => budget.kind === 'model_calls').used, '2', 'deleting a child replenished ancestor allowance');
  await client.submit(root.id, [{ type: 'text', text: 'must not dispatch' }], 'budget:exhausted', deadline());
  const blocked = await client.wait('budget:exhausted', deadline());
  assert.equal(blocked.turn.state, 'failed');
  assert.deepEqual((await client.call('turns.attempts', { turn_id: blocked.turn.id, limit: 100 }, deadline())).items, []);
  evidence.push({ budgetInitial: initial, budgetCharged: charged, budgetBlocked: blocked });
}

async function writeAllowanceAcceptance(runtime, client, createParams, evidence) {
  const { root } = await client.call('trees.create', createParams, deadline());
  const inspect = sessionID => client.call('budgets.list', { session_id: sessionID }, deadline());
  const initial = await inspect(root.id);
  for (const [kind, limit] of [['logical_writes', '3'], ['logical_write_bytes', '11']]) {
    const current = initial.items.find(item => item.kind === kind);
    await client.call('budgets.set', { session_id: root.id, expected_revision: current.revision, budget: { kind, limit } }, deadline());
  }
  const spawn = { parent_id: root.id, parts: [{ type: 'text', text: 'child' }], grant_ids: [], overrides: {} };
  const child = await client.spawn(spawn, 'write:child', deadline());
  await client.wait('write:child', deadline());
  const encode = raw => Buffer.from(raw).toString('base64');
  const params = { session_id: child.session.id, scope: 'tree', key: 'write-allowance', expected_revision: '0', data_base64: encode('[1]') };
  const first = await client.writeState(params, 'write:first', deadline());
  const append = { ...params, expected_revision: first.revision, data_base64: encode('[2]') };
  const second = await client.appendState(append, 'write:second', deadline());
  const charged = await inspect(root.id);
  for (const [kind, used] of [['logical_writes', '3'], ['logical_write_bytes', '11']]) {
    const budget = charged.items.find(item => item.kind === kind);
    assert.equal(budget.used, used, 'spawn and submitted suffix must each charge once');
    assert.equal(budget.reserved, '0'); assert.equal(budget.uncertain, '0');
  }
  assert.deepEqual(await client.appendState(append, 'write:second', deadline()), second);
  await assert.rejects(client.writeState({ ...params, key: 'denied' }, 'write:denied', deadline()), error => error.kind === 'LIMIT');
  await assert.rejects(client.call('state.get', { session_id: root.id, scope: 'tree', key: 'denied' }, deadline()), error => error.kind === 'NOT_FOUND');
  await client.submit(child.session.id, [{ type: 'text', text: 'human input and model settlement remain available' }], 'write:human', deadline());
  assert.equal((await client.wait('write:human', deadline())).turn.state, 'succeeded');
  await runtime.stop(); await runtime.start('0');
  const restarted = await inspect(root.id);
  assert.deepEqual(restarted.items.filter(item => item.kind.startsWith('logical_')), charged.items.filter(item => item.kind.startsWith('logical_')));
  await client.call('sessions.delete', { session_id: child.session.id }, deadline());
  const retained = await inspect(root.id);
  assert.deepEqual(retained.items.filter(item => item.kind.startsWith('logical_')), charged.items.filter(item => item.kind.startsWith('logical_')));
  const replay = await client.spawn(spawn, 'write:child', deadline());
  assert.equal(replay.session, null);
  assert.ok(replay.admission.receipt.deleted_at);
  const state = await client.call('state.read', { session_id: root.id, version_id: second.id, offset: '0', length: 65536 }, deadline());
  assert.equal(Buffer.from(state.data_base64, 'base64').toString(), '[1,2]');
  evidence.push({ writeAllowance: { root: root.id, charged, restarted, retained, replay, state } });
}

async function mailAcceptance(runtime, client, createParams, evidence) {
  const { root } = await client.call('trees.create', createParams, deadline());
  const child = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'mail sender' }], grant_ids: [] }, 'mail-child', deadline());
  await client.wait('mail-child', deadline());
  const params = { sender_id: child.session.id, recipient_id: root.id, delivery: 'next_turn', subject: 'Read-only inspection', body: 'This body remains canonical mail.' };
  const admission = await client.sendMail(params, 'mail-next-turn', deadline());
  const read = () => client.call('mail.read', { session_id: root.id, mail_id: admission.mail_id }, deadline());
  const inbox = await client.call('mail.list', { session_id: root.id, state: 'pending', limit: 100 }, deadline());
  assert.equal(inbox.items.length, 1);
  assert.equal('body' in inbox.items[0], false);
  assert.equal((await read()).body, params.body);
  assert.equal((await read()).mail.state, 'pending', 'human reads must not acknowledge mail');
  assert.deepEqual((await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline())).items, []);
  await assert.rejects(client.call('mail.read', { session_id: child.session.id, mail_id: admission.mail_id }, deadline()), error => error.kind === 'NOT_FOUND');
  await assert.rejects(client.sendMail({ ...params, body: 'changed' }, admission.mail_id, deadline()), error => error.kind === 'CONFLICT');
  await client.submit(root.id, [{ type: 'text', text: 'present pending mail' }], 'mail-explicit-turn', deadline());
  const completed = await client.wait('mail-explicit-turn', deadline());
  assert.equal(completed.turn.state, 'succeeded');
  assert.equal((await read()).mail.state, 'delivered');
  const history = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
  const presented = history.items.find(message => message.mail?.id === admission.mail_id);
  assert.ok(presented, 'history must retain its immutable mail source');
  assert.equal(presented.input_id, null);
  assert.equal(presented.mail.revision, '1');

  let dropped;
  const lostParams = { ...params, delivery: 'queued', subject: 'Lost mail acknowledgment', body: 'Mail can start a turn without an input.' };
  const proxyPath = join(runtime.directory, 'mail-drop.sock');
  const close = await dropAcknowledgement(proxyPath, runtime.info.socket, 'mail-lost-ack', value => { dropped = value; }, response => response.result?.mail_id === 'mail-lost-ack');
  try {
    const unreliable = await Client.connect(unixSocket(proxyPath), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    await assert.rejects(unreliable.sendMail(lostParams, 'mail-lost-ack', deadline()), DeliveryError);
  } finally { await close(); }
  assert.equal(dropped.mail_id, 'mail-lost-ack');
  const retried = await client.sendMail(lostParams, 'mail-lost-ack', deadline());
  assert.equal(retried.mail_id, dropped.mail_id);
  const delivered = await until(() => client.call('mail.read', { session_id: root.id, mail_id: retried.mail_id }, deadline()), result => result.mail.state === 'delivered');
  const after = await client.call('sessions.history', { session_id: root.id, after: presented.sequence, limit: 100 }, deadline());
  const mailOnlyMessage = after.items.find(message => message.mail?.id === retried.mail_id);
  assert.ok(mailOnlyMessage);
  assert.equal(after.items.filter(message => message.mail?.id === retried.mail_id).length, 1);
  assert.equal(after.items.filter(message => message.turn_id === mailOnlyMessage.turn_id && message.input_id !== null).length, 0);
  assert.equal((await client.call('turns.get', { turn_id: mailOnlyMessage.turn_id }, deadline())).state, 'succeeded');
  await client.call('sessions.delete', { session_id: root.id }, deadline());
  const deleted = await client.sendMail(lostParams, 'mail-lost-ack', deadline());
  assert.equal(deleted.mail, null);
  assert.ok(deleted.deleted_at);
  evidence.push({ mail: { admission, inbox, completed, history, dropped, retried, delivered, after, deleted } });
}

async function stateAcceptance(runtime, client, createParams, evidence) {
  const { root } = await client.call('trees.create', createParams, deadline());
  const encode = raw => Buffer.from(raw).toString('base64');
  const params = { session_id: root.id, scope: 'session', key: 'exact', expected_revision: '0', data_base64: encode('[9007199254740993]') };
  let dropped;
  const proxyPath = join(runtime.directory, 'state-drop.sock');
  const close = await dropAcknowledgement(proxyPath, runtime.info.socket, 'state-first', value => { dropped = value; }, response => response.result?.id === 'state-first');
  try {
    const unreliable = await Client.connect(unixSocket(proxyPath), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    await assert.rejects(unreliable.writeState(params, 'state-first', deadline()), DeliveryError);
  } finally { await close(); }
  const first = await client.writeState(params, 'state-first', deadline());
  assert.deepEqual(first, dropped);
  const append = { ...params, expected_revision: first.revision, data_base64: encode('[2]') };
  const second = await client.appendState(append, 'state-second', deadline());
  assert.equal(second.revision, '2');
  assert.deepEqual(await client.appendState(append, 'state-second', deadline()), second);
  assert.deepEqual(await client.writeState(params, 'state-first', deadline()), first);
  await assert.rejects(client.writeState(params, 'state-stale', deadline()), error => error.kind === 'CONFLICT');
  await assert.rejects(client.writeState({ ...params, data_base64: encode('null') }, 'state-first', deadline()), error => error.kind === 'CONFLICT');
  const read = (id, offset = '0', length = 65536) => client.call('state.read', { session_id: root.id, version_id: id, offset, length }, deadline());
  assert.equal(Buffer.from((await read(first.id)).data_base64, 'base64').toString(), '[9007199254740993]');
  assert.equal(Buffer.from((await read(second.id)).data_base64, 'base64').toString(), '[9007199254740993,2]');
  const raw = JSON.stringify('🌏'.repeat(20000));
  const large = await client.writeState({ ...params, key: 'large', data_base64: encode(raw) }, 'state-large', deadline());
  const prefix = await read(large.id, '0', 65536);
  const suffix = await read(large.id, '65536', 65536);
  assert.equal(Buffer.concat([Buffer.from(prefix.data_base64, 'base64'), Buffer.from(suffix.data_base64, 'base64')]).toString(), raw);
  const history = await client.call('state.history', { session_id: root.id, scope: 'session', key: 'exact', after: '1', limit: 1 }, deadline());
  assert.deepEqual(history.items, [second]);
  assert.deepEqual((await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline())).items, []);
  await runtime.stop(); await runtime.start('0');
  assert.equal((await read(large.id)).version.digest, large.digest);
  await client.call('sessions.delete', { session_id: root.id }, deadline());
  await assert.rejects(client.writeState(params, 'state-first', deadline()), error => error.kind === 'NOT_FOUND');
  evidence.push({ state: { first, second, large, history } });
}

async function stateSubscriptionAcceptance(runtime, client, createParams, evidence) {
  const { root } = await client.call('trees.create', createParams, deadline());
  const { session: author } = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'state author' }], grant_ids: [] }, 'state-notifications-child', deadline());
  await client.wait('state-notifications-child', deadline());
  const params = { session_id: root.id, key: 'topic', after: '0', delivery: 'next_turn' };
  const subscription = await client.subscribeState(params, 'state-watch', deadline());
  const write = (revision, sessionID = author.id) => client.writeState({ session_id: sessionID, scope: 'tree', key: 'topic', expected_revision: String(revision - 1), data_base64: Buffer.from(String(revision)).toString('base64') }, 'notified-state-' + revision, deadline());
  await write(1);
  await runtime.stop(); await runtime.start('0');
  const second = await write(2);
  const inbox = await client.call('mail.list', { session_id: root.id, state: 'pending', limit: 100 }, deadline());
  assert.equal(inbox.items.length, 1);
  assert.deepEqual(inbox.items[0].source, { kind: 'state', id: subscription.id });
  assert.equal(inbox.items[0].revision, '2');
  const read = await client.call('mail.read', { session_id: root.id, mail_id: inbox.items[0].id }, deadline());
  assert.equal(JSON.parse(read.body).version_id, second.id);
  assert.equal((await client.subscribeState(params, subscription.id, deadline())).cursor, '2');
  await client.submit(root.id, [{ type: 'text', text: 'handle state changes' }], 'state-handle', deadline());
  await client.wait('state-handle', deadline());
  assert.equal((await client.call('mail.read', { session_id: root.id, mail_id: read.mail.id }, deadline())).mail.state, 'delivered');
  await write(3);
  await write(4, root.id);
  const next = await client.call('mail.list', { session_id: root.id, state: 'pending', limit: 100 }, deadline());
  assert.equal(next.items.length, 1);
  assert.notEqual(next.items[0].id, read.mail.id);
  assert.equal(next.items[0].revision, '1', 'own write must advance the cursor without notifying itself');
  assert.equal((await client.call('state.subscriptions', { session_id: root.id, limit: 100 }, deadline())).items[0].cursor, '4');
  const cancelled = await client.call('state.unsubscribe', { session_id: root.id, subscription_id: subscription.id }, deadline());
  assert.ok(cancelled.cancelled_at);
  await write(5);
  assert.equal((await client.call('mail.read', { session_id: root.id, mail_id: next.items[0].id }, deadline())).mail.revision, '1');
  assert.ok((await client.subscribeState(params, subscription.id, deadline())).cancelled_at, 'retry must not resurrect a cancelled subscription');
  await client.call('sessions.delete', { session_id: root.id }, deadline());
  evidence.push({ stateSubscriptions: { subscription, second, inbox, next, cancelled } });
}


async function resourceAcceptance(runtime, client, createParams, evidence) {
  const { root } = await client.call('trees.create', {
    ...createParams,
    resources: [{ kind: 'descendants', limit: '1' }, { kind: 'runnable_descendants', limit: '0' }],
  }, deadline());
  const list = sessionID => client.call('resources.list', { session_id: sessionID }, deadline());
  const scope = (page, owner, kind) => page.items.find(item => item.session_id === owner && item.kind === kind);
  const rootResources = await list(root.id);
  assert.equal(rootResources.items.length, 6);
  assert.equal(scope(rootResources, root.id, 'descendants').limit, '1');
  const spawnParams = {
    parent_id: root.id, parts: [{ type: 'text', text: 'resource child' }], overrides: {}, grant_ids: [],
    resources: [{ kind: 'descendants', limit: '0' }],
  };
  const child = await client.spawn(spawnParams, 'resource-child', deadline());
  const childResources = await list(child.session.id);
  assert.deepEqual(childResources.items.map(item => item.session_id), [
    ...Array(6).fill(child.session.id), ...Array(6).fill(root.id),
  ]);
  assert.equal(scope(childResources, root.id, 'descendants').used, '1');
  await assert.rejects(client.spawn(spawnParams, 'resource-rejected', deadline()), error => error instanceof RemoteError && error.kind === 'LIMIT');
  const childLimit = scope(childResources, child.session.id, 'descendants');
  const cleared = await client.call('resources.set', {
    session_id: child.session.id, expected_revision: childLimit.revision,
    resource: { kind: 'descendants', limit: null },
  }, deadline());
  assert.equal(cleared.limit, null);
  await assert.rejects(client.call('resources.set', {
    session_id: child.session.id, expected_revision: childLimit.revision,
    resource: { kind: 'descendants', limit: '0' },
  }, deadline()), error => error.kind === 'CONFLICT');
  const active = scope(rootResources, root.id, 'active_operations');
  const exact = await client.call('resources.set', {
    session_id: root.id, expected_revision: active.revision,
    resource: { kind: 'active_operations', limit: '9007199254740993' },
  }, deadline());
  assert.equal(exact.limit, '9007199254740993');
  const queued = await client.recover('resource-child', deadline());
  assert.equal(queued.input.state, 'queued');
  assert.equal(queued.turn, null);
  const runnable = scope(rootResources, root.id, 'runnable_descendants');
  const resumed = await client.call('resources.set', {
    session_id: root.id, expected_revision: runnable.revision,
    resource: { kind: 'runnable_descendants', limit: '1' },
  }, deadline());
  await client.wait('resource-child', deadline());
  await runtime.stop(); await runtime.start('0');
  const reopened = await list(child.session.id);
  assert.equal(scope(reopened, root.id, 'active_operations').limit, exact.limit);
  assert.equal(scope(reopened, root.id, 'active_operations').revision, exact.revision);
  assert.equal(scope(reopened, child.session.id, 'descendants').limit, null);
  assert.equal(scope(reopened, child.session.id, 'descendants').revision, cleared.revision);
  assert.equal(scope(reopened, root.id, 'runnable_descendants').limit, resumed.limit);
  assert.equal(scope(reopened, root.id, 'runnable_descendants').used, '0');
  await client.call('sessions.delete', { session_id: child.session.id }, deadline());
  const released = await list(root.id);
  assert.equal(scope(released, root.id, 'descendants').used, '0');
  const replacement = await client.spawn(spawnParams, 'resource-rejected', deadline());
  assert.equal((await client.wait('resource-rejected', deadline())).turn.state, 'succeeded');
  evidence.push({ resourceRoot: root.id, rootResources, childResources, cleared, exact, queued, resumed, reopened, released, replacement });
}


async function completionAcceptance(runtime, client, createParams, evidence) {
  const { root } = await client.call('trees.create', { ...createParams, overrides: { ...createParams.overrides, report_mode: 'notice' } }, deadline());
  const prompt = '🙂'.repeat(1800);
  const child = await client.spawn({ parent_id: root.id, overrides: { report_mode: 'inline' }, parts: [{ type: 'text', text: prompt }], grant_ids: [] }, 'report-inline', deadline());
  const finished = await client.wait('report-inline', deadline());
  const inbox = await until(() => client.call('mail.list', { session_id: root.id, limit: 100 }, deadline()), value => value.items.some(item => item.source.kind === 'completion' && item.state === 'delivered'));
  const delivered = inbox.items.find(item => item.source.id === child.session.id);
  const report = await client.call('mail.read', { session_id: root.id, mail_id: delivered.id }, deadline());
  const notice = JSON.parse(report.body);
  assert.equal(notice.turn_id, finished.turn.id);
  assert.equal(notice.input_id, finished.input.id);
  assert.equal(notice.mode, 'inline');
  assert.equal(notice.text_truncated, true);
  assert.ok(Buffer.byteLength(notice.preview) <= 4096 && Buffer.byteLength(notice.preview) > 2048);
  const full = await client.call('content.read', { session_id: root.id, reference_id: notice.evidence_ref }, deadline());
  const snapshot = JSON.parse(Buffer.from(full.data_base64, 'base64'));
  assert.equal(snapshot.text, 'ack: ' + prompt);
  assert.equal(snapshot.text_bytes, String(Buffer.byteLength(snapshot.text)));
  await assert.rejects(client.call('content.read', { session_id: child.session.id, reference_id: notice.evidence_ref }, deadline()), error => error.kind === 'NOT_FOUND');
  await client.call('sessions.delete', { session_id: child.session.id }, deadline());
  assert.deepEqual(await client.call('content.read', { session_id: root.id, reference_id: notice.evidence_ref }, deadline()), full);
  const history = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
  assert.equal(history.items.filter(item => item.mail?.id === delivered.id).length, 1);
  assert.equal(history.items.filter(item => item.input_id !== null).length, 0, 'report wakes a normal mail turn without synthetic input');
  assert.equal((await client.call('completions.list', { parent_id: root.id, limit: 100 }, deadline())).items.length, 0);
  const quiet = await client.spawn({ parent_id: root.id, overrides: { report_mode: 'message' }, parts: [{ type: 'text', text: 'I report explicitly' }], grant_ids: [] }, 'report-message', deadline());
  await client.wait('report-message', deadline());
  assert.equal((await client.call('mail.list', { session_id: root.id, limit: 100 }, deadline())).items.length, 1);
  await runtime.stop(); await runtime.start('0');
  assert.equal((await client.call('mail.list', { session_id: root.id, limit: 100 }, deadline())).items.length, 1, 'restart must not regenerate already published reports');
  assert.equal((await client.call('sessions.get', { session_id: quiet.session.id }, deadline())).configuration.report_mode, 'message');
  await client.call('sessions.delete', { session_id: root.id }, deadline());

  // Fill the parent's retained-content allowance. The child's completion still
  // commits, remains independently inspectable, and survives source deletion.
  const pressured = await client.call('trees.create', createParams, deadline());
  const data = Buffer.alloc(4 << 20, 'p').toString('base64');
  for (let i = 0; i < 16; i++) {
    await client.call('content.put', { session_id: pressured.root.id, reference_id: 'report-fill-' + i, media_type: 'application/octet-stream', data_base64: data }, deadline());
  }
  const pendingPrompt = 'pending evidence '.repeat(5000);
  const pendingChild = await client.spawn({ parent_id: pressured.root.id, overrides: { report_mode: 'notice' }, parts: [{ type: 'text', text: pendingPrompt }], grant_ids: [] }, 'report-pressure', deadline());
  const pendingTurn = await client.wait('report-pressure', deadline());
  const pending = await client.call('completions.list', { parent_id: pressured.root.id, limit: 100 }, deadline());
  assert.equal(pending.items.length, 1);
  assert.equal(pending.items[0].turn_id, pendingTurn.turn.id);
  assert.equal(pending.items[0].state, 'succeeded');
  await client.call('sessions.delete', { session_id: pendingChild.session.id }, deadline());
  await runtime.stop(); await runtime.start('0');
  assert.deepEqual(await client.call('completions.list', { parent_id: pressured.root.id, limit: 100 }, deadline()), pending);
  const chunks = [];
  let offset = '0';
  do {
    const part = await client.call('completions.read', { parent_id: pressured.root.id, child_id: pendingChild.session.id, turn_id: pendingTurn.turn.id, offset, length: 65536 }, deadline());
    assert.equal(part.offset, offset);
    const bytes = Buffer.from(part.data_base64, 'base64');
    assert.ok(bytes.length <= 65536);
    chunks.push(bytes);
    offset = part.next_offset;
  } while (offset !== null);
  assert.ok(chunks.length > 1);
  assert.equal(JSON.parse(Buffer.concat(chunks)).text, 'ack: ' + pendingPrompt);
  await assert.rejects(client.call('completions.read', { parent_id: pressured.root.id, child_id: pendingChild.session.id, turn_id: 'superseded-turn', offset: '0', length: 1 }, deadline()), error => error.kind === 'CONFLICT');
  assert.equal((await client.call('mail.list', { session_id: pressured.root.id, limit: 100 }, deadline())).items.length, 0);
  await client.call('sessions.delete', { session_id: pressured.root.id }, deadline());
  evidence.push({ completionReports: { notice, pending } });
}

async function outputAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const valid = '{"count":9007199254740993}';
  const replies = ['not JSON', '```json\n' + valid + '\n```', '{"count":"wrong"}', 'still invalid', 'null', 'plain text after clearing'];
  const server = http.createServer(async (request, response) => {
    let body = '';
    for await (const chunk of request) body += chunk;
    requests.push(JSON.parse(body));
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({
      choices: [{ message: { role: 'assistant', content: replies[requests.length - 1] ?? 'unexpected model call' }, finish_reason: 'stop' }],
      usage: { prompt_tokens: 10, completion_tokens: 3, cost: 0.000000001 },
    }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.fixture = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: { 'fixture-model': { max_output_tokens: 123, timeout_millis: 2000, max_attempts: 1 } },
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    const schema = { type: 'object', properties: { count: { type: 'integer' } }, required: ['count'], additionalProperties: false };
    const { root } = await client.call('trees.create', {
      ...createParams, overrides: { ...createParams.overrides, model: { provider: 'fixture', name: 'fixture-model', effort: '' }, output: { schema } },
    }, deadline());
    const output = turnID => client.call('turns.output', { turn_id: turnID }, deadline());
    await client.submit(root.id, [{ type: 'text', text: 'return structured count' }], 'output-valid', deadline());
    const first = await client.wait('output-valid', deadline());
    assert.equal(first.turn.state, 'succeeded');
    const projected = await output(first.turn.id);
    assert.equal(projected.output.turn_id, first.turn.id);
    assert.equal(Buffer.from(projected.output.data_base64, 'base64').toString(), valid, 'wire must preserve JSON integers beyond JavaScript safe integer range');
    const attempts = await client.call('turns.attempts', { turn_id: first.turn.id, limit: 100 }, deadline());
    assert.equal(attempts.items.length, 2);
    assert.ok(attempts.items.every(item => item.state === 'succeeded'));
    assert.notEqual(attempts.items[0].logical_id, attempts.items[1].logical_id, 'correction is its own recorded model round');
    const firstHistory = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
    assert.deepEqual(firstHistory.items.map(item => item.role), ['user', 'assistant', 'assistant']);
    assert.equal(firstHistory.items[1].parts[0].text, 'not JSON');
    assert.equal(firstHistory.items[2].id, projected.output.message_id);
    assert.equal(firstHistory.items[2].parts[0].text, replies[1]);
    assert.ok(JSON.stringify(requests[1]).includes('not JSON'), 'corrective round must retain invalid assistant context');
    await client.submit(root.id, [{ type: 'text', text: 'exercise invalid output' }], 'output-invalid', deadline());
    const failed = await client.wait('output-invalid', deadline());
    assert.equal(failed.turn.state, 'failed');
    assert.match(failed.turn.failure, /output_invalid/);
    assert.equal((await output(failed.turn.id)).output, null);
    assert.equal((await client.call('turns.attempts', { turn_id: failed.turn.id, limit: 100 }, deadline())).items.length, 2);
    const nullable = await client.call('sessions.configure', { session_id: root.id, expected_revision: root.config_revision, patch: { output: { schema: { type: 'null' } } } }, deadline());
    await client.submit(root.id, [{ type: 'text', text: 'return null' }], 'output-null', deadline());
    const nullTurn = await client.wait('output-null', deadline());
    assert.equal(nullTurn.turn.state, 'succeeded');
    assert.equal(Buffer.from((await output(nullTurn.turn.id)).output.data_base64, 'base64').toString(), 'null');
    await client.call('sessions.configure', { session_id: root.id, expected_revision: nullable.config_revision, patch: { output: { schema: null } } }, deadline());
    await client.submit(root.id, [{ type: 'text', text: 'ordinary reply' }], 'output-cleared', deadline());
    const cleared = await client.wait('output-cleared', deadline());
    assert.equal(cleared.turn.state, 'succeeded');
    assert.equal((await output(cleared.turn.id)).output, null);
    assert.deepEqual(await output(first.turn.id), projected, 'old output must use its captured schema');
    assert.equal(requests.length, 6);
    await runtime.stop(); await runtime.start(null);
    assert.deepEqual(await output(first.turn.id), projected);
    assert.equal(requests.length, 6, 'reading restored output must not execute the model');
    evidence.push({ outputContracts: { first, projected, attempts, failed, nullTurn, cleared } });
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}


async function contextAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const summary = 'Condensed older turns.';
  const server = http.createServer(async (request, response) => {
    let body = '';
    for await (const chunk of request) body += chunk;
    const value = JSON.parse(body);
    requests.push(value);
    const helper = value.messages[0]?.content.startsWith('Summarize the conversation data');
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({
      choices: [{ message: { role: 'assistant', content: helper ? summary : 'Raw answer ' + requests.length }, finish_reason: 'stop' }],
      usage: { prompt_tokens: 30, completion_tokens: 5 },
    }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.fixture = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: { 'fixture-model': { max_output_tokens: 128, timeout_millis: 2000, max_attempts: 1 } },
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    const { root } = await client.call('trees.create', {
      ...createParams, overrides: { ...createParams.overrides, model: { provider: 'fixture', name: 'fixture-model', effort: '' } },
    }, deadline());
    const history = () => client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
    const head = () => client.call('context.head', { session_id: root.id }, deadline());
    for (let index = 0; index < 7; index++) {
      await client.submit(root.id, [{ type: 'text', text: `context marker ${index} 🧭 9007199254740993 ` + '.'.repeat(200) }], `context-${index}`, deadline());
      const done = await client.wait(`context-${index}`, deadline());
      assert.equal(done.input.kind, 'prompt');
      assert.equal(done.turn.kind, 'prompt');
      assert.equal(done.turn.state, 'succeeded');
    }
    const raw = await history();
    assert.equal(raw.items.length, 14);
    const snapshot = await client.call('context.snapshot', { session_id: root.id }, deadline());
    assert.equal(snapshot.through_sequence, '14');
    assert.equal(snapshot.message_count, '14');
    const configured = await client.call('sessions.configure', { session_id: root.id, expected_revision: root.config_revision, patch: { output: { schema: { type: 'null' } } } }, deadline());
    const admitted = await client.compact(root.id, 'context-compact', deadline());
    assert.equal(admitted.input.kind, 'compact');
    assert.deepEqual(admitted.input.parts, []);
    const compacted = await client.wait('context-compact', deadline());
    assert.equal(compacted.turn.kind, 'compact');
    assert.equal(compacted.turn.state, 'succeeded', compacted.turn.failure);
    assert.deepEqual(await history(), raw, 'compaction must preserve raw conversation history');
    assert.equal((await client.call('turns.output', { turn_id: compacted.turn.id }, deadline())).output, null, 'maintenance does not enforce or produce structured answers');
    const attempts = await client.call('turns.attempts', { turn_id: compacted.turn.id, limit: 100 }, deadline());
    assert.equal(attempts.items.length, 1);
    assert.equal(attempts.items[0].request.purpose, 'compaction');
    assert.equal(attempts.items[0].message_id, null);
    assert.deepEqual((await client.call('turns.cells', { turn_id: compacted.turn.id, limit: 100 }, deadline())).items, []);
    const selected = await head();
    assert.equal(selected.revision, '1');
    assert.ok(selected.compaction_id);
    const summaries = await client.call('context.compactions', { session_id: root.id, limit: 100 }, deadline());
    assert.equal(summaries.items.length, 1);
    const summaryRecord = await client.call('context.compaction', { session_id: root.id, compaction_id: selected.compaction_id }, deadline());
    assert.equal(summaryRecord.text, summary);
    assert.equal(summaryRecord.metadata.through_sequence, '6', 'manual compaction keeps the latest four complete turns');
    assert.equal(summaryRecord.metadata.attempt_id, attempts.items[0].id);
    assert.deepEqual(summaryRecord.metadata.pinned_message_ids, []);
    assert.equal((await client.compact(root.id, 'context-compact', deadline())).input.id, admitted.input.id);
    await assert.rejects(client.submit(root.id, [{ type: 'text', text: 'different kind' }], 'context-compact', deadline()), error => error.kind === 'CONFLICT');
    await client.compact(root.id, 'context-noop', deadline());
    const noop = await client.wait('context-noop', deadline());
    assert.equal(noop.turn.state, 'succeeded');
    assert.equal((await client.call('turns.attempts', { turn_id: noop.turn.id, limit: 100 }, deadline())).items.length, 0);
    assert.equal(requests.length, 8, 'no remaining old history means no helper request');
    await runtime.stop(); await runtime.start(null);
    assert.deepEqual(await head(), selected);
    assert.deepEqual(await history(), raw);
    await client.call('sessions.configure', { session_id: root.id, expected_revision: configured.config_revision, patch: { output: { schema: null } } }, deadline());
    await client.submit(root.id, [{ type: 'text', text: 'context marker after snapshot' }], 'context-followup', deadline());
    assert.equal((await client.wait('context-followup', deadline())).turn.state, 'succeeded');
    const followup = requests.at(-1);
    assert.ok(JSON.stringify(followup).includes('untrusted_context_summary'));
    assert.ok(JSON.stringify(followup).includes(summary));
    assert.ok(!JSON.stringify(followup).includes('context marker 0'), 'covered prefix is replaced only in model context');
    assert.ok(JSON.stringify(followup).includes('context marker 3'), 'recent raw tail is retained');
    const listed = await client.call('context.list', { session_id: root.id, after: '0', through_sequence: snapshot.through_sequence, limit: 100 }, deadline());
    assert.equal(listed.items.length, 14, 'a fixed snapshot excludes later appends');
    assert.equal(listed.next_after, null);
    const chunks = [];
    let offset = '0';
    do {
      const part = await client.call('context.read', { session_id: root.id, message_id: raw.items[0].id, offset, length: 7 }, deadline());
      chunks.push(Buffer.from(part.data_base64, 'base64'));
      offset = part.next_offset;
    } while (offset !== null);
    assert.deepEqual(JSON.parse(Buffer.concat(chunks).toString('utf8')), raw.items[0].parts, 'byte pages may split UTF-8 but reconstruct exact stored parts');
    const matches = await client.call('context.search', { session_id: root.id, after: '0', through_sequence: snapshot.through_sequence, query: 'context marker', limit: 100 }, deadline());
    assert.equal(matches.matches.length, 7);
    assert.ok(matches.matches.every(match => BigInt(match.message.sequence) <= BigInt(snapshot.through_sequence)));
    const foreign = await client.call('trees.create', createParams, deadline());
    await assert.rejects(client.call('context.read', { session_id: foreign.root.id, message_id: raw.items[0].id, offset: '0', length: 10 }, deadline()), error => error.kind === 'NOT_FOUND');
    await assert.rejects(client.call('context.compaction', { session_id: foreign.root.id, compaction_id: selected.compaction_id }, deadline()), error => error.kind === 'NOT_FOUND');
    const undone = await client.call('context.select', { session_id: root.id, expected_revision: selected.revision, compaction_id: null }, deadline());
    assert.equal(undone.revision, '2');
    assert.equal(undone.compaction_id, null);
    await assert.rejects(client.call('context.select', { session_id: root.id, expected_revision: selected.revision, compaction_id: null }, deadline()), error => error.kind === 'CONFLICT');
    assert.deepEqual((await client.call('context.compactions', { session_id: root.id, limit: 100 }, deadline())).items, summaries.items, 'undo changes selection without deleting summary evidence');
    await client.submit(root.id, [{ type: 'text', text: 'after undo' }], 'context-after-undo', deadline());
    assert.equal((await client.wait('context-after-undo', deadline())).turn.state, 'succeeded');
    assert.ok(JSON.stringify(requests.at(-1)).includes('context marker 0'), 'undo restores raw prefix to model context');
    assert.ok(!JSON.stringify(requests.at(-1)).includes('untrusted_context_summary'));
    assert.deepEqual((await history()).items.slice(0, 14), raw.items);
    let automatic;
    for (let index = 9; index < 51; index++) {
      const requestID = 'context-auto-' + index;
      await client.submit(root.id, [{ type: 'text', text: 'Automatic context turn ' + index }], requestID, deadline());
      automatic = await client.wait(requestID, deadline());
      assert.equal(automatic.turn.state, 'succeeded', automatic.turn.failure);
    }
    const autoAttempts = await client.call('turns.attempts', { turn_id: automatic.turn.id, limit: 100 }, deadline());
    assert.deepEqual(autoAttempts.items.map(item => item.request.purpose).sort(), ['compaction', 'turn']);
    const autoHead = await head();
    assert.equal(autoHead.revision, '3');
    const autoSummary = await client.call('context.compaction', { session_id: root.id, compaction_id: autoHead.compaction_id }, deadline());
    assert.equal(autoSummary.metadata.through_sequence, '94');
    const afterAuto = await client.call('context.snapshot', { session_id: root.id }, deadline());
    assert.equal(afterAuto.message_count, '102', 'automatic folding must retain every original raw message');
    assert.ok(requests.at(-1).messages.length <= 101, 'ordinary request stays under its history cap plus system instructions');
    evidence.push({ context: { compacted, attempts, selected, summaryRecord, snapshot, undone, autoAttempts, autoHead, afterAuto } });
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}


async function contextRecoveryAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const states = new Map(['starlark', 'quickjs'].map(engine => [engine, { round: 0, retry: 0, twice: 0 }]));
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    const engine = body.model.split('-').at(-1);
    const state = states.get(engine);
    const helper = body.messages[0]?.content.startsWith('Summarize the conversation data');
    const prompt = body.messages.findLast(item => item.role === 'user')?.content ?? '';
    requests.push({ engine, helper, body });
    response.setHeader('content-type', 'application/json');
    let message;
    let finish_reason = 'stop';
    if (helper) message = { role: 'assistant', content: 'Preserve the opening task and completed count.' };
    else if (prompt.endsWith(':uncertain')) { response.destroy(); return; }
    else if (prompt.endsWith(':twice') || (prompt.endsWith(':retry') && state.retry++ === 0)) {
      if (prompt.endsWith(':twice')) state.twice++;
      response.statusCode = 400;
      response.end(JSON.stringify({ error: { code: 'context_length_exceeded', message: 'private provider diagnostic' }, usage: { prompt_tokens: 6, completion_tokens: 0 } }));
      return;
    } else if (prompt.endsWith(':long') && state.round < 10) {
      const round = state.round++;
      message = { role: 'assistant', content: 'Retained work evidence: ' + 'x'.repeat(600_000), tool_calls: Array.from({ length: 3 }, (_, index) => {
        const start = round === 0 && index === 0;
        const code = engine === 'starlark'
          ? (start ? 'context_count = 0\n' : '') + 'context_count += 1\nprint(context_count)'
          : (start ? 'var context_count = 0; ' : '') + 'context_count += 1; console.log(context_count)';
        return { id: `count-${round}-${index}`, type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } };
      }) };
      finish_reason = 'tool_calls';
    } else if (prompt.endsWith(':retry') && body.messages.at(-1).role !== 'tool') {
      const code = engine === 'starlark' ? 'print(context_count)' : 'console.log(context_count)';
      message = { role: 'assistant', content: null, tool_calls: [{ id: 'restored-count', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
      finish_reason = 'tool_calls';
    } else message = { role: 'assistant', content: body.messages.at(-1).content };
    response.end(JSON.stringify({ choices: [{ message, finish_reason }], usage: { prompt_tokens: 30, completion_tokens: 5 } }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.context = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: Object.fromEntries([...states.keys()].map(engine => [`context-${engine}`, { max_output_tokens: 2048, timeout_millis: 5000, max_attempts: 1 }])),
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    for (const engine of states.keys()) {
      const { root } = await client.call('trees.create', {
        ...createParams, engine, overrides: { model: { provider: 'context', name: `context-${engine}`, effort: '' } },
      }, deadline());
      const requestID = `context-recovery-${engine}`;
      await client.submit(root.id, [{ type: 'text', text: `${engine}:long` }], requestID, deadline());
      const completed = await client.wait(requestID, { signal: AbortSignal.timeout(60_000) });
      assert.equal(completed.turn.state, 'succeeded', completed.turn.failure);
      const raw = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
      const selected = await client.call('context.head', { session_id: root.id }, deadline());
      assert.ok(selected.compaction_id, 'one long turn must compact before its next model request');
      const summary = await client.call('context.compaction', { session_id: root.id, compaction_id: selected.compaction_id }, deadline());
      assert.deepEqual(summary.metadata.pinned_message_ids, [raw.items[0].id]);
      assert.equal(raw.items[0].input_id, completed.input.id, 'pin identifies the exact accepted opening input');
      assert.equal((await client.call('context.snapshot', { session_id: root.id }, deadline())).message_count, '42');
      const cells = await client.call('turns.cells', { turn_id: completed.turn.id, limit: 100 }, deadline());
      assert.equal(cells.items.length, 30);
      assert.ok(cells.items.every(cell => cell.state === 'succeeded'));
      const ordinary = requests.filter(item => item.engine === engine && !item.helper);
      assert.ok(ordinary.every(item => item.body.messages.filter(message => message.role === 'user' && message.content === `${engine}:long`).length === 1), 'opening input remains present exactly once across splits');
      await runtime.stop(); await runtime.start(null);
      const retryID = requestID + '-retry';
      await client.submit(root.id, [{ type: 'text', text: `${engine}:retry` }], retryID, deadline());
      const retried = await client.wait(retryID, deadline());
      assert.equal(retried.turn.state, 'succeeded', retried.turn.failure);
      const attempts = await client.call('turns.attempts', { turn_id: retried.turn.id, limit: 100 }, deadline());
      assert.equal(attempts.items.length, 4, 'failed request, helper, resumed code request and final reply all have accounted attempts');
      const rejected = attempts.items.find(item => item.state === 'failed');
      assert.equal(rejected.result.usage.input, '6');
      assert.ok(!JSON.stringify(rejected).includes('private provider diagnostic'));
      assert.equal(attempts.items.filter(item => item.request.purpose === 'compaction').length, 1);
      const retryHistory = await client.call('sessions.history', { session_id: root.id, after: '42', limit: 100 }, deadline());
      assert.equal(JSON.parse(retryHistory.items.at(-1).parts[0].text).result.output, '30\n', 'restart and context recovery cannot replay prior cells');
      assert.equal((await client.call('turns.cells', { turn_id: retried.turn.id, limit: 100 }, deadline())).items.length, 1);
      const twiceID = requestID + '-twice';
      await client.submit(root.id, [{ type: 'text', text: `${engine}:twice` }], twiceID, deadline());
      const twice = await client.wait(twiceID, deadline());
      assert.equal(twice.turn.state, 'failed');
      assert.equal(states.get(engine).twice, 2);
      const twiceAttempts = await client.call('turns.attempts', { turn_id: twice.turn.id, limit: 100 }, deadline());
      assert.equal(twiceAttempts.items.length, 3);
      assert.equal(twiceAttempts.items.filter(item => item.request.purpose === 'compaction').length, 1);
      const uncertainID = requestID + '-uncertain';
      await client.submit(root.id, [{ type: 'text', text: `${engine}:uncertain` }], uncertainID, deadline());
      const uncertain = await client.wait(uncertainID, deadline());
      assert.equal(uncertain.turn.state, 'failed');
      const unknown = await client.call('turns.attempts', { turn_id: uncertain.turn.id, limit: 100 }, deadline());
      assert.equal(unknown.items.length, 1);
      assert.equal(unknown.items[0].state, 'uncertain');
      evidence.push({ contextRecovery: { engine, completed, selected, summary, retried, attempts, twice, twiceAttempts, uncertain, unknown } });
    }
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}

async function contextPolicyAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  let ordinaryCount = 0;
  const blocked = Promise.withResolvers();
  const release = Promise.withResolvers();
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    const helper = body.messages[0]?.content.startsWith('Summarize the conversation data');
    requests.push({ helper, body });
    const ordinary = helper ? 0 : ++ordinaryCount;
    if (ordinary === 6) { blocked.resolve(); await release.promise; }
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({
      choices: [{ message: { role: 'assistant', content: helper ? 'Earlier tasks completed.' : `Policy answer ${ordinary}.` }, finish_reason: 'stop' }],
      usage: {
        prompt_tokens: helper ? 90_000 : ordinary === 6 ? 60_000 : 10, completion_tokens: 2,
        prompt_tokens_details: { cached_tokens: 0 }, completion_tokens_details: { reasoning_tokens: 0, cached_tokens: 0 },
      },
    }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.policy = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: Object.fromEntries(['conversation', 'helper-a', 'helper-b'].map(name => [name, {
        context_window_tokens: 100_000, max_output_tokens: 128, timeout_millis: 5000, max_attempts: 1,
        prices: { input: 3_000_000, output: 5_000_000 },
      }])),
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    const model = name => ({ provider: 'policy', name, effort: '' });
    const { root } = await client.call('trees.create', {
      ...createParams, overrides: { model: model('conversation'), compaction: { model: model('helper-a'), threshold_percent: 50 } },
    }, deadline());
    const submit = async index => {
      const id = 'context-policy-' + index;
      await client.submit(root.id, [{ type: 'text', text: `Policy task ${index}. ` + 'Useful history. '.repeat(20) }], id, deadline());
      const done = await client.wait(id, deadline());
      assert.equal(done.turn.state, 'succeeded', done.turn.failure);
      return done;
    };
    for (let index = 1; index <= 5; index++) await submit(index);
    const waiting = submit(6);
    await Promise.race([blocked.promise, new Promise((_, reject) => {
      const timer = setTimeout(() => reject(new Error('Policy request did not block')), 10_000);
      timer.unref();
      blocked.promise.then(() => clearTimeout(timer));
    })]);
    const configured = await client.call('sessions.configure', {
      session_id: root.id, expected_revision: root.config_revision,
      patch: { compaction: { model: model('helper-b'), threshold_percent: 75 } },
    }, deadline());
    release.resolve();
    const completed = await waiting;
    const attempts = await client.call('turns.attempts', { turn_id: completed.turn.id, limit: 100 }, deadline());
    assert.deepEqual(attempts.items.map(item => item.request.purpose).sort(), ['compaction', 'turn']);
    const helper = attempts.items.find(item => item.request.purpose === 'compaction');
    assert.equal(helper.request.model.name, 'helper-a', 'active turn retains its old helper and 50% threshold');
    assert.equal(helper.result.usage.input, '90000');
    assert.equal(helper.cost_nano_usd, '270010', 'helper uses its own accounted pricing');
    assert.equal(helper.message_id, null);
    assert.equal(requests.filter(item => item.helper).length, 1, 'helper usage never triggers another fold');
    const selected = await client.call('context.head', { session_id: root.id }, deadline());
    assert.equal(selected.revision, '1');
    assert.equal((await client.call('context.snapshot', { session_id: root.id }, deadline())).message_count, '12');
    await submit(7);
    assert.equal(requests.filter(item => item.helper).length, 1, 'new turns start without a stale occupancy cache');
    await client.compact(root.id, 'context-policy-helper-b', deadline());
    const next = await client.wait('context-policy-helper-b', deadline());
    assert.equal(next.turn.state, 'succeeded', next.turn.failure);
    assert.equal(requests.at(-1).body.model, 'helper-b', 'next turn captures the updated helper');
    const reset = await client.call('sessions.configure', {
      session_id: root.id, expected_revision: configured.config_revision,
      patch: { compaction: { model: null, threshold_percent: 0 } },
    }, deadline());
    assert.deepEqual(reset.configuration.compaction, { model: null, threshold_percent: 50 });
    await runtime.stop(); await runtime.start(null);
    await submit(8);
    await client.compact(root.id, 'context-policy-default', deadline());
    const fallback = await client.wait('context-policy-default', deadline());
    assert.equal(fallback.turn.state, 'succeeded', fallback.turn.failure);
    assert.equal(requests.at(-1).helper, true);
    assert.equal(requests.at(-1).body.model, 'conversation', 'explicit reset survives restart and uses the conversation route');
    await submit(9);
    await client.call('sessions.configure', {
      session_id: root.id, expected_revision: reset.config_revision,
      patch: { compaction: { model: { provider: 'missing', name: 'missing', effort: '' }, threshold_percent: 50 } },
    }, deadline());
    const count = requests.length;
    await client.compact(root.id, 'context-policy-bad-route', deadline());
    const failed = await client.wait('context-policy-bad-route', deadline());
    assert.equal(failed.turn.state, 'failed');
    assert.equal(requests.length, count, 'an explicit invalid helper cannot silently fall back');
    evidence.push({ contextPolicy: { completed, attempts, selected, configured, next, reset, fallback, failed } });
  } finally {
    release.resolve();
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}

async function instructionAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const release = Promise.withResolvers();
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    requests.push(body);
    const first = requests.length === 1;
    if (first) await release.promise;
    response.setHeader('content-type', 'application/json');
    const message = first
      ? { role: 'assistant', content: null, tool_calls: [{ id: 'instruction-cell', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code: 'console.log(42)' }) } }] }
      : { role: 'assistant', content: 'Instruction capture completed.' };
    response.end(JSON.stringify({ choices: [{ message, finish_reason: first ? 'tool_calls' : 'stop' }] }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    const workspace = join(runtime.directory, 'instructions');
    const visible = join(workspace, '.agents', 'skills', 'visible', 'SKILL.md');
    const hidden = join(workspace, '.agents', 'skills', 'hidden', 'SKILL.md');
    await mkdir(join(workspace, '.agents', 'skills', 'visible'), { recursive: true });
    await mkdir(join(workspace, '.agents', 'skills', 'hidden'), { recursive: true });
    const metadata = '---\nname: visible\ndescription: Visible skill metadata.\n---\n';
    await writeFile(visible, metadata + 'SKILL_BODY_MUST_STAY_DEFERRED', { mode: 0o600 });
    await writeFile(hidden, '---\nname: hidden\ndescription: >-\n  Hidden skill metadata.\ndisable-model-invocation: true\n---\nHIDDEN_BODY', { mode: 0o600 });
    await writeFile(join(workspace, 'AGENTS.md'), 'PROJECT_INSTRUCTIONS_BEFORE', { mode: 0o600 });
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.instructions = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: { instructions: { max_output_tokens: 128, timeout_millis: 5000, max_attempts: 1 } },
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    const policy = { text: 'CONFIGURED_INSTRUCTIONS_BEFORE', project_files: ['AGENTS.md'], discover_skills: true };
    const { root } = await client.call('trees.create', {
      ...createParams, engine: 'quickjs', working_directory: workspace,
      overrides: { ...createParams.overrides, model: { provider: 'instructions', name: 'instructions', effort: '' }, instructions: policy },
    }, deadline());
    const grant = await client.call('grants.create', { id: 'instruction-read-before', session_id: root.id, capability: 'files.read', resource: workspace }, deadline());
    await client.submit(root.id, [{ type: 'text', text: 'original instruction input' }], 'instructions-before', deadline());
    await until(async () => requests.length, count => count === 1);
    const active = await client.recover('instructions-before', deadline());
    const captured = await client.call('turns.instructions', { turn_id: active.turn.id }, deadline());
    const instructions = requests[0].messages[0].content;
    assert.ok(instructions.includes('PROJECT_INSTRUCTIONS_BEFORE'));
    assert.ok(instructions.includes('Visible skill metadata.'));
    assert.ok(!instructions.includes('Hidden skill metadata.'));
    assert.ok(!instructions.includes('SKILL_BODY_MUST_STAY_DEFERRED'));
    assert.ok(!instructions.includes('HIDDEN_BODY'));
    assert.equal(captured.manifest.sha256, createHash('sha256').update(instructions).digest('hex'));
    assert.equal(captured.manifest.bytes, String(Buffer.byteLength(instructions)));
    assert.equal(captured.manifest.sources.length, 3, 'audit includes hidden metadata that affected discovery');
    const skill = captured.manifest.sources.find(source => source.path === '.agents/skills/visible/SKILL.md');
    assert.equal(skill.bytes, String(Buffer.byteLength(metadata)));
    assert.equal(skill.sha256, createHash('sha256').update(metadata).digest('hex'));
    await writeFile(join(workspace, 'AGENTS.md'), 'PROJECT_INSTRUCTIONS_AFTER', { mode: 0o600 });
    policy.text = 'CONFIGURED_INSTRUCTIONS_AFTER';
    await client.call('sessions.configure', { session_id: root.id, expected_revision: root.config_revision, patch: { instructions: policy } }, deadline());
    await client.call('grants.revoke', { grant_id: grant.id }, deadline());
    release.resolve();
    const completed = await client.wait('instructions-before', deadline());
    assert.equal(completed.turn.state, 'succeeded', completed.turn.failure);
    assert.equal(requests[1].messages[0].content, instructions, 'file/configuration changes and revocation cannot change a captured turn');
    assert.equal((await client.call('turns.cells', { turn_id: completed.turn.id, limit: 100 }, deadline())).items.length, 1);
    await writeFile(visible, Buffer.from([0xff]));
    await client.submit(root.id, [{ type: 'text', text: 'denied optional sources' }], 'instructions-denied', deadline());
    const denied = await client.wait('instructions-denied', deadline());
    assert.equal(denied.turn.state, 'succeeded', denied.turn.failure);
    assert.ok(requests.at(-1).messages[0].content.includes(policy.text));
    assert.ok(!requests.at(-1).messages[0].content.includes('PROJECT_INSTRUCTIONS_AFTER'));
    assert.deepEqual((await client.call('turns.instructions', { turn_id: denied.turn.id }, deadline())).manifest.sources, []);
    await writeFile(visible, metadata + 'SKILL_BODY_MUST_STAY_DEFERRED', { mode: 0o600 });
    await client.call('grants.create', { id: 'instruction-read-after', session_id: root.id, capability: 'files.read', resource: workspace }, deadline());
    const spawned = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'child instructions' }], grant_ids: null }, 'instructions-child', deadline());
    const childDone = await client.wait('instructions-child', deadline());
    assert.equal(childDone.turn.state, 'succeeded', childDone.turn.failure);
    assert.ok(requests.at(-1).messages[0].content.includes('PROJECT_INSTRUCTIONS_AFTER'));
    await runtime.stop();
    await writeFile(join(workspace, 'AGENTS.md'), 'PROJECT_INSTRUCTIONS_RESTARTED', { mode: 0o600 });
    await runtime.start(null);
    assert.deepEqual(await client.call('turns.instructions', { turn_id: completed.turn.id }, deadline()), captured);
    await client.submit(spawned.session.id, [{ type: 'text', text: 'refresh after restart' }], 'instructions-restart', deadline());
    const restarted = await client.wait('instructions-restart', deadline());
    assert.equal(restarted.turn.state, 'succeeded', restarted.turn.failure);
    assert.ok(requests.at(-1).messages[0].content.includes('PROJECT_INSTRUCTIONS_RESTARTED'));
    await writeFile(join(workspace, 'AGENTS.md'), Buffer.from([0xff]));
    const count = requests.length;
    await client.submit(spawned.session.id, [{ type: 'text', text: 'broken applicable source' }], 'instructions-broken', deadline());
    const failed = await client.wait('instructions-broken', deadline());
    assert.equal(failed.turn.state, 'failed');
    assert.equal(requests.length, count, 'applicable source failure must precede provider dispatch');
    assert.deepEqual((await client.call('turns.attempts', { turn_id: failed.turn.id, limit: 100 }, deadline())).items, []);
    assert.equal((await client.call('turns.instructions', { turn_id: failed.turn.id }, deadline())).manifest, null);
    await client.compact(spawned.session.id, 'instructions-maintenance', deadline());
    const maintenance = await client.wait('instructions-maintenance', deadline());
    assert.equal(maintenance.turn.state, 'succeeded', maintenance.turn.failure);
    assert.equal((await client.call('turns.instructions', { turn_id: maintenance.turn.id }, deadline())).manifest, null);
    const history = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
    assert.equal(history.items[0].parts[0].text, 'original instruction input', 'dynamic files never rewrite canonical input');
    evidence.push({ instructions: { completed, captured, denied, childDone, restarted, failed, maintenance } });
  } finally {
    release.resolve();
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}
