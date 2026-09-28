import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { once } from 'node:events';
import { cp, mkdir, mkdtemp, readFile, rename, rm, symlink, writeFile } from 'node:fs/promises';
import http from 'node:http';
import net from 'node:net';
import { basename, join, resolve } from 'node:path';
import { promisify } from 'node:util';
import { test } from 'node:test';
import { Client, DeliveryError, RemoteError } from '../../packages/sdk/dist/index.js';
import { unixSocket as socketTransport } from '../../packages/sdk/dist/node.js';

const exec = promisify(execFile);
const deadline = () => ({ signal: AbortSignal.timeout(15_000) });
const fixtureAbort = new AbortController();

// A stage timeout must also stop observers that deliberately have no RPC deadline.
function unixSocket(path) {
  const transport = socketTransport(path);
  return (request, runtimeID, options = {}) => transport(request, runtimeID, {
    ...options,
    signal: AbortSignal.any([fixtureAbort.signal, ...(options.signal ? [options.signal] : [])]),
  });
}

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
  const start = async (delay = '60ms', environment = {}) => {
    fixtureAbort.signal.throwIfAborted();
    const args = ['-directory', join(directory, 'state'), '-workers', '1'];
    if (delay !== null) args.push('-scripted', '-scripted-delay', delay);
    child = spawn(binary, args, { stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env, ...environment } });
    child.stderr.on('data', record);
    child.stdout.on('data', record);
    let buffer = '';
    info = await new Promise((resolve, reject) => {
      const cleanup = () => { clearTimeout(timer); child.removeListener('error', failed); child.removeListener('exit', earlyExit); child.stdout.removeListener('data', data); fixtureAbort.signal.removeEventListener('abort', aborted); };
      const failed = error => { cleanup(); reject(error); };
      const aborted = () => failed(fixtureAbort.signal.reason);
      const earlyExit = (code, signal) => failed(new Error(`Runtime exited before ready: ${code}/${signal}\n${output}`));
      const data = chunk => {
        buffer += chunk;
        const end = buffer.indexOf('\n');
        if (end < 0) return;
        try { const ready = JSON.parse(buffer.slice(0, end)); cleanup(); resolve(ready); } catch (error) { failed(error); }
      };
      const timer = setTimeout(() => failed(new Error('Runtime readiness timed out\n' + output)), 25_000);
      child.once('error', failed); child.once('exit', earlyExit); child.stdout.on('data', data);
      fixtureAbort.signal.addEventListener('abort', aborted, { once: true });
    });
    return info;
  };
  const build = async () => {
    try {
      // This is production-binary acceptance. The required Go suites separately
      // run with -race; its worker RSS overhead is outside production limits.
      await exec('go', ['build', '-race=false', '-o', binary, './cmd/whip-runtime'], {
        cwd: resolve('.'), timeout: 120_000, signal: fixtureAbort.signal,
      });
    } catch (error) {
      record(error.stderr ?? '');
      throw error;
    }
  };
  return { directory, build, start, stop, get info() { return info; }, get pid() { return child?.pid; }, get output() { return output; } };
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

test('v4 SDK executes, recovers lost acknowledgements, and preserves queued input across SIGKILL', async t => {
  const runtime = await fixture();
  const evidence = [];
  const artifacts = join('test-results/redesign', basename(runtime.directory));
  await mkdir(artifacts, { recursive: true });
  const progress = { directory: runtime.directory, runtime_build: 'production', stages: [] };
  const saveProgress = () => writeFile(join(artifacts, 'progress.json'), JSON.stringify({ ...progress, pid: runtime.pid }, null, 2) + '\n');
  const stage = async (name, run, timeout = 60_000) => {
    const entry = { name, started_at: new Date().toISOString(), timeout_ms: timeout, status: 'running' };
    progress.stages.push(entry);
    await saveProgress();
    console.log(`[v4 fixture] ${name}: started (${timeout}ms budget)`);
    const started = performance.now();
    let timer;
    try {
      const result = await Promise.race([run(), new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error(`Stage ${name} timed out after ${timeout}ms`)), timeout);
      })]);
      entry.status = 'passed';
      return result;
    } catch (error) {
      entry.status = 'failed';
      entry.error = error.stack ?? String(error);
      fixtureAbort.abort(error);
      throw error;
    } finally {
      clearTimeout(timer);
      entry.elapsed_ms = Math.round(performance.now() - started);
      await saveProgress();
      console.log(`[v4 fixture] ${name}: ${entry.status} (${entry.elapsed_ms}ms)`);
    }
  };
  let failure;
  let proxyClose;
  try {
    await stage('compile runtime', runtime.build, 125_000);
    await stage('start runtime', runtime.start, 30_000);
    const { client, createParams } = await stage('admission and crash recovery', async () => {
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

      const configured = await client.call('sessions.configure', { session_id: root.id, expected_revision: root.config_revision, patch: { instructions: { text: 'inherit me', project_files: [], discover_skills: false, skill_roots: [], standing_instructions: false, project_root: null } } }, deadline());
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
      return { client, createParams };
    });
    await stage('host account projections', () => accountAcceptance(runtime, client, evidence));
    await stage('content owners', () => contentOwnerAcceptance(runtime, client, createParams, evidence));
    await stage('resources', () => resourceAcceptance(runtime, client, createParams, evidence));
    await stage('schedules', () => scheduleAcceptance(runtime, client, createParams, evidence));
    await stage('goals', () => goalAcceptance(runtime, client, createParams, evidence));
    await stage('goal formulation', () => goalFormulationAcceptance(runtime, client, createParams, evidence));
    await stage('budgets', () => budgetAcceptance(client, createParams, evidence));
    await stage('logical write allowances', () => writeAllowanceAcceptance(runtime, client, createParams, evidence));
    await stage('mail', () => mailAcceptance(runtime, client, createParams, evidence));
    await stage('mail evidence', () => mailEvidenceAcceptance(runtime, client, createParams, evidence));
    await stage('state', () => stateAcceptance(runtime, client, createParams, evidence));
    await stage('state subscriptions', () => stateSubscriptionAcceptance(runtime, client, createParams, evidence));
    await stage('completion reports', () => completionAcceptance(runtime, client, createParams, evidence));
    await stage('HTTP provider', () => providerAcceptance(runtime, client, createParams, evidence));
    await stage('stateless model helpers', () => modelHelpersAcceptance(runtime, client, createParams, evidence));
    await stage('Responses provider', () => responsesAcceptance(runtime, client, createParams, evidence));
    await stage('subscription admission', () => subscriptionAdmissionAcceptance(runtime, client, createParams, evidence));
    await stage('output contracts', () => outputAcceptance(runtime, client, createParams, evidence));
    await stage('context compaction', () => contextAcceptance(runtime, client, createParams, evidence));
    await stage('context recovery', () => contextRecoveryAcceptance(runtime, client, createParams, evidence), 150_000);
    await stage('context policy', () => contextPolicyAcceptance(runtime, client, createParams, evidence));
    await stage('instructions', () => instructionAcceptance(runtime, client, createParams, evidence));
    await stage('host skills', () => hostSkillAcceptance(runtime, client, createParams, evidence));
    await stage('standing instructions', () => standingInstructionAcceptance(runtime, client, createParams, evidence));
    await stage('project ancestor instructions', () => projectInstructionAcceptance(runtime, client, createParams, evidence));
    await stage('engines', () => engineAcceptance(runtime, client, createParams, evidence));
    await stage('operations', () => operationAcceptance(runtime, client, createParams, evidence));
    await stage('streaming', () => streamAcceptance(runtime, client, createParams, evidence));
  } catch (error) {
    failure = error;
  } finally {
    fixtureAbort.abort(failure ?? new Error('Fixture complete'));
    try {
      await stage('shutdown', async () => { await proxyClose?.(); await runtime.stop(); }, 15_000);
    } catch (error) {
      failure ??= error;
    }
    if (failure) {
      // Write small diagnostics first so a failed snapshot copy cannot hide the stage.
      await writeFile(join(artifacts, 'runtime.log'), runtime.output);
      await writeFile(join(artifacts, 'observations.json'), JSON.stringify(evidence, null, 2) + '\n');
      t.diagnostic(`Failure artifacts: ${artifacts}; original: ${runtime.directory}`);
      await cp(runtime.directory, artifacts, { recursive: true, filter: path => !path.endsWith('/runtime') && !path.endsWith('.sock') });
    } else {
      await rm(runtime.directory, { recursive: true, force: true });
      await rm(artifacts, { recursive: true, force: true });
    }
  }
  if (failure) throw failure;
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
      ...createParams, overrides: { model: { provider: 'fixture', name: 'fixture-model', effort: '', temperature: 0, top_p: 0.75 } },
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
    assert.equal(requests[0].body.temperature, 0);
    assert.equal(requests[0].body.top_p, 0.75);
    assert.equal(requests[0].body.messages.at(-1).content[1].image_url.url, 'data:image/png;base64,' + image);
    evidence.push({ httpProvider: { result, ledger, history, requests } });
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}

async function modelHelpersAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const counts = new Map();
  const blocked = new Set();
  const large = '界🙂"\\\n'.repeat(4000);
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    requests.push(body);
    const prompt = body.messages.findLast(item => item.role === 'user').content;
    let message;
    let cost = 0;
    if (prompt.startsWith('helper:')) {
      const kind = prompt.split(':')[1];
      const count = (counts.get(prompt) ?? 0) + 1;
      counts.set(prompt, count);
      if (kind === 'block') { blocked.add(prompt); return; }
      if (kind === 'failure' || (kind === 'retry' && count === 1)) {
        response.writeHead(kind === 'failure' ? 400 : 429, { 'content-type': 'application/json' });
        response.end(JSON.stringify({ error: 'confirmed helper rejection' }));
        return;
      }
      message = { role: 'assistant', content: kind === 'large' ? large : kind };
      cost = 0.000000001;
    } else if (body.messages.at(-1).role === 'tool') {
      message = { role: 'assistant', content: 'helper cell settled' };
    } else {
      const prompts = prompt.endsWith(':crash')
        ? [`helper:small:${prompt}`, `helper:block:${prompt}`]
        : [`helper:retry:${prompt}`, `helper:failure:${prompt}`, `helper:large:${prompt}`];
      let code;
      if (body.model === 'starlark') {
        code = prompt.endsWith(':crash')
          ? `models.batch(prompts=${JSON.stringify(prompts)}, max_tokens=19)`
          : `single=models.call(prompt=${JSON.stringify('helper:small:' + prompt)}, max_tokens=17)\nitems=models.batch(prompts=${JSON.stringify(prompts)}, max_tokens=19)\nprint(single["text"])\nprint(items[0]["text"])`;
      } else {
        code = prompt.endsWith(':crash')
          ? `await models.batch({prompts:${JSON.stringify(prompts)},max_tokens:19});`
          : `var single=await models.call({prompt:${JSON.stringify('helper:small:' + prompt)},max_tokens:17});var items=await models.batch({prompts:${JSON.stringify(prompts)},max_tokens:19});print(single.text);print(items[0].text);`;
      }
      message = { role: 'assistant', content: null, tool_calls: [{ id: 'helpers', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
    }
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 2, completion_tokens: 1, cost } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.helpers = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: Object.fromEntries(['starlark', 'quickjs'].map(engine => [engine, { max_output_tokens: 500, timeout_millis: 30000, max_attempts: 2 }])),
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    for (const engine of ['starlark', 'quickjs']) {
      const { root } = await client.call('trees.create', {
        ...createParams, engine, overrides: { report_mode: 'message', model: { provider: 'helpers', name: engine, effort: 'low', temperature: 0, top_p: 0.5 } },
      }, deadline());
      for (const capability of ['models.call', 'models.batch']) {
        await client.call('grants.create', { id: `${engine}-${capability}`, session_id: root.id, capability, resource: root.tree_id }, deadline());
      }
      let rootReference;
      for (const depth of ['root', 'child']) {
        const requestID = `${engine}:helpers-${depth}`;
        let owner = root;
        if (depth === 'child') {
          const child = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: requestID }], grant_ids: null }, requestID, deadline());
          owner = child.session;
        } else {
          await client.submit(owner.id, [{ type: 'text', text: requestID }], requestID, deadline());
        }
        const done = await client.wait(requestID, deadline());
        assert.equal(done.turn.state, 'succeeded', JSON.stringify(done.turn));
        const operations = await client.call('turns.operations', { turn_id: done.turn.id, limit: 100 }, deadline());
        assert.equal(operations.items.length, 2);
        const single = operations.items.find(item => item.capability === 'models.call');
        const batch = operations.items.find(item => item.capability === 'models.batch');
        assert.equal(single.state, 'succeeded');
        assert.equal(single.result.value.text, 'small');
        assert.equal(single.result.value.content_ref, null);
        assert.equal(batch.state, 'succeeded');
        const values = batch.result.value;
        assert.equal(values.length, 3);
        assert.equal(values[0].text, 'retry');
        assert.equal(values[0].failure, null);
        assert.ok(values[1].failure);
        assert.equal(values[2].failure, null);
        assert.equal(values[2].truncated, true);
        assert.equal(values[2].bytes, String(Buffer.byteLength(large)));
        const ledger = await client.call('turns.attempts', { turn_id: done.turn.id, limit: 100 }, deadline());
        const helpers = ledger.items.filter(item => item.request.purpose === 'model_helper');
        assert.equal(helpers.length, 5);
        assert.ok(helpers.every(item => item.message_id === null));
        assert.equal(helpers.filter(item => item.operation_id === single.id && item.batch_index === 0).length, 1);
        assert.deepEqual(helpers.filter(item => item.operation_id === batch.id && item.batch_index === 0).map(item => item.state).sort(), ['failed', 'succeeded']);
        for (const [index, value] of values.entries()) {
          if (value.failure) continue;
          const attempt = helpers.find(item => item.id === value.attempt_id);
          assert.equal(attempt.operation_id, batch.id);
          assert.equal(attempt.batch_index, index);
          assert.equal(attempt.cost_nano_usd, '1');
        }
        const reference = values[2].content_ref;
        const read = await client.call('content.read', { session_id: owner.id, reference_id: reference }, deadline());
        assert.equal(Buffer.from(read.data_base64, 'base64').toString('utf8'), large);
        const history = await client.call('sessions.history', { session_id: owner.id, after: '0', limit: 100 }, deadline());
        assert.deepEqual(history.items.map(item => item.role), ['user', 'assistant', 'tool', 'assistant']);
        assert.ok(!JSON.stringify(history).includes(JSON.stringify(large).slice(1, -1)));
        if (depth === 'root') rootReference = reference;
        else {
          await assert.rejects(client.call('content.read', { session_id: root.id, reference_id: reference }, deadline()), error => error.kind === 'NOT_FOUND');
          await client.call('sessions.delete', { session_id: owner.id }, deadline());
          await assert.rejects(client.call('content.read', { session_id: owner.id, reference_id: reference }, deadline()), error => error.kind === 'NOT_FOUND');
        }
        evidence.push({ modelHelpers: { engine, depth, done, operations, ledger } });
      }
      const crashID = `${engine}:crash`;
      await client.submit(root.id, [{ type: 'text', text: crashID }], crashID, deadline());
      const active = await until(() => client.recover(crashID, deadline()), value => value.turn?.state === 'running');
      await until(async () => blocked.has(`helper:block:${crashID}`), Boolean);
      await until(() => client.call('turns.attempts', { turn_id: active.turn.id, limit: 100 }, deadline()), page => page.items.some(item => item.request.purpose === 'model_helper' && item.batch_index === 0 && item.state === 'succeeded'));
      const before = requests.length;
      await runtime.stop('SIGKILL');
      await runtime.start(null);
      assert.equal((await client.wait(crashID, deadline())).turn.state, 'interrupted');
      const recovered = await client.call('turns.attempts', { turn_id: active.turn.id, limit: 100 }, deadline());
      const partial = recovered.items.filter(item => item.request.purpose === 'model_helper').sort((a, b) => a.batch_index - b.batch_index);
      assert.deepEqual(partial.map(item => item.state), ['succeeded', 'uncertain']);
      assert.deepEqual(partial.map(item => item.cost_nano_usd), ['1', null]);
      const operations = await client.call('turns.operations', { turn_id: active.turn.id, limit: 100 }, deadline());
      assert.equal(operations.items[0].state, 'uncertain');
      assert.equal(requests.length, before, 'restart replayed an unfinished batch');
      const retained = await client.call('content.read', { session_id: root.id, reference_id: rootReference }, deadline());
      assert.equal(Buffer.from(retained.data_base64, 'base64').toString('utf8'), large);
      evidence.push({ modelHelpersRecovery: { engine, recovered, operations } });
    }
    const helpers = requests.filter(body => body.messages[0]?.content?.startsWith('helper:'));
    assert.ok(helpers.length > 0);
    for (const body of helpers) {
      assert.equal(body.messages.length, 1);
      assert.equal(body.messages[0].role, 'user');
      assert.ok(!body.tools?.length);
      assert.equal(body.temperature, 0);
      assert.equal(body.top_p, 0.5);
      assert.equal(body.reasoning_effort, 'low');
      assert.ok([17, 19].includes(body.max_completion_tokens));
    }
  } finally {
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}

async function responsesAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const marker = 'opaque-response-continuation';
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    requests.push({ path: request.url, body, raw });
    const prompt = body.input.findLast(item => item.role === 'user')?.content?.find(item => item.type === 'input_text')?.text;
    const engine = prompt.split(':')[0];
    const result = body.input.at(-1).type === 'function_call_output';
    const code = engine === 'starlark'
      ? 'view=context.inspect(limit=100)\nfor item in view["items"]:\n if item["role"]=="assistant":\n  print(context.read(id=item["id"],offset="0",length=65536)["data_base64"])\n  break'
      : 'var view=await context.inspect({limit:100}); var item=view.items.find(item=>item.role==="assistant"); print((await context.read({id:item.id,offset:"0",length:65536})).data_base64);';
    const output = [
      { type: 'reasoning', encrypted_content: marker, future: { preserved: '9007199254740993' } },
      result ? { type: 'message', role: 'assistant', content: [{ type: 'output_text', text: 'Responses completed' }] }
        : { type: 'function_call', id: 'output-call', call_id: 'execute-responses', name: 'execute', arguments: JSON.stringify({ code }) },
    ];
    response.setHeader('content-type', 'text/event-stream');
    // Exercise completed item replay when the terminal envelope omits output.
    output.forEach((item, output_index) => response.write(`data: ${JSON.stringify({ type: 'response.output_item.done', output_index, item })}\n\n`));
    response.end(`data: ${JSON.stringify({ type: 'response.completed', response: { status: 'completed', usage: { input_tokens: 30, output_tokens: 7, input_tokens_details: { cached_tokens: 4 }, output_tokens_details: { reasoning_tokens: 2 }, cost: 0.000000001 } } })}\n\n`);
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    await runtime.stop();
    const path = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(path, 'utf8'));
    host.providers.responses = { kind: 'openai-responses', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '', models: { responses: { max_output_tokens: 500, timeout_millis: 10000, max_attempts: 1 } } };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    for (const engine of ['starlark', 'quickjs']) {
      const { root } = await client.call('trees.create', { ...createParams, engine, overrides: { ...createParams.overrides, model: { provider: 'responses', name: 'responses', effort: 'off' } } }, deadline());
      for (const capability of ['context.inspect', 'context.read']) {
        await client.call('grants.create', { id: `${root.id}-${capability}`, session_id: root.id, capability, resource: root.tree_id, issuer_id: null }, deadline());
      }
      for (const stage of ['initial', 'restart', 'route-change']) {
        if (stage === 'restart') { await runtime.stop(); await runtime.start(null); }
        if (stage === 'route-change') {
          const changed = JSON.parse(await readFile(path, 'utf8'));
          changed.providers.responses.base_url += '/other';
          await writeFile(path, JSON.stringify(changed), { mode: 0o600 });
        }
        const start = requests.length;
        const id = `${engine}:responses:${stage}`;
        await client.submit(root.id, [{ type: 'text', text: id }], id, deadline());
        const completed = await client.wait(id, deadline());
        assert.equal(completed.turn.state, 'succeeded', completed.turn.failure);
        assert.equal(requests.length, start + 2, 'each Responses round dispatches exactly one recorded request');
        const first = requests[start], second = requests[start + 1];
        assert.equal(first.raw.includes(marker), stage === 'restart', 'opaque replay requires the original route and survives restart');
        assert.ok(second.raw.includes(marker), 'the next model round must replay its committed reasoning');
        assert.ok(second.body.input.some(item => item.type === 'function_call_output'));
        assert.equal(first.body.max_output_tokens, 500);
        assert.equal(first.body.reasoning, undefined);
        assert.equal(first.body.store, false);
        assert.equal(first.body.prompt_cache_key, root.id);
        const attempts = await client.call('turns.attempts', { turn_id: completed.turn.id, limit: 100 }, deadline());
        const history = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
        const operations = await client.call('turns.operations', { turn_id: completed.turn.id, limit: 100 }, deadline());
        const snapshot = await client.call('context.snapshot', { session_id: root.id }, deadline());
        const metadata = await client.call('context.list', { session_id: root.id, after: '0', through_sequence: snapshot.through_sequence, limit: 100 }, deadline());
        const search = await client.call('context.search', { session_id: root.id, after: '0', through_sequence: snapshot.through_sequence, query: marker, limit: 100 }, deadline());
        assert.equal(search.matches.length, 0);
        assert.equal(attempts.items.length, 2);
        for (const [index, attempt] of attempts.items.entries()) {
          assert.equal(attempt.request.adapter, 'openai-responses');
          assert.equal(attempt.request.model.effort, 'off');
          assert.equal(attempt.request.request_digest, createHash('sha256').update(requests[start + index].raw).digest('hex'));
          assert.equal(attempt.cost_nano_usd, '1');
          assert.equal(attempt.result.usage.cached_input, '4');
          assert.equal(attempt.result.usage.reasoning, '2');
        }
        for (const value of [attempts, history, operations, metadata, search]) {
          assert.equal(JSON.stringify(value).includes(marker), false, 'private provider state leaked through a public projection');
          assert.equal(JSON.stringify(value).includes('encrypted_content'), false);
        }
        for (const operation of operations.items.filter(item => item.capability === 'context.read')) {
          assert.equal(operation.state, 'succeeded');
          assert.equal(Buffer.from(operation.result.value.data_base64, 'base64').toString('utf8').includes(marker), false);
        }
        evidence.push({ responses: { engine, stage, completed, attempts, operations } });
      }
    }
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
    response.write('data: ' + JSON.stringify({ choices: [{ index: 0, delta: { role: 'assistant', reasoning_content: 'Checking ' }, finish_reason: null }] }) + '\n\n');
    response.write('data: ' + JSON.stringify({ choices: [{ index: 0, delta: { content: 'partial' }, finish_reason: null }] }) + '\n\n');
    response.write('data: ' + JSON.stringify({ choices: [{ index: 0, delta: { reasoning_content: prompt === 'stream:bounded' ? '界'.repeat(50000) : 'the request' }, finish_reason: null }] }) + '\n\n');
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
    const provisional = await until(observe, view => view.preview?.text === 'partial' && view.preview?.reasoning === 'Checking the request');
    assert.deepEqual(provisional.messages.map(message => message.role), ['user']);
    assert.equal(provisional.preview.truncated, false);
    const iterator = client.observe(root.id);
    const previewPage = (await iterator.next()).value;
    assert.equal(previewPage.preview.attempt_id, provisional.preview.attempt_id);
    assert.equal(previewPage.preview.reasoning, 'Checking the request');
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
    assert.equal(JSON.stringify({ committed, ledger }).includes('Checking the request'), false, 'reasoning preview entered durable projections');

    await client.submit(root.id, [{ type: 'text', text: 'stream:bounded' }], 'stream:bounded', deadline());
    const bounded = await until(observe, view => view.preview?.truncated === true);
    const previewBytes = Buffer.byteLength(bounded.preview.text) + Buffer.byteLength(bounded.preview.reasoning) + bounded.preview.calls.reduce((size, call) => size + Buffer.byteLength(call.id) + Buffer.byteLength(call.name) + Buffer.byteLength(call.arguments), 0);
    assert.ok(previewBytes <= 128 * 1024 && previewBytes >= 128 * 1024 - 3, `shared preview bytes: ${previewBytes}`);
    assert.equal(bounded.preview.text, 'partial');
    assert.ok(bounded.preview.reasoning.startsWith('Checking 界'));
    assert.equal(bounded.preview.reasoning.includes('\uFFFD'), false, 'preview split a Unicode codepoint');
    completions.get('stream:bounded')();
    assert.equal((await client.wait('stream:bounded', deadline())).turn.state, 'succeeded');
    const afterBounded = await observe();
    assert.equal(afterBounded.preview, null);
    assert.deepEqual(afterBounded.messages.at(-1).parts, [{ type: 'text', text: 'partial completed' }]);

    await client.submit(root.id, [{ type: 'text', text: 'stream:crash' }], 'stream:crash', deadline());
    const beforeKill = await until(observe, view => view.preview?.text === 'partial' && view.preview?.reasoning === 'Checking the request');
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
    await until(observe, view => view.preview?.text === 'partial' && view.preview?.reasoning === 'Checking the request');
    const controller = new AbortController();
    const observer = client.observe(root.id, { signal: controller.signal });
    await observer.next();
    controller.abort();
    await assert.rejects(observer.next(), error => error.name === 'AbortError');
    assert.equal((await client.recover('stream:observer-abort', deadline())).turn.state, 'running');
    completions.get('stream:observer-abort')();
    assert.equal((await client.wait('stream:observer-abort', deadline())).turn.state, 'succeeded');

    await client.submit(root.id, [{ type: 'text', text: 'stream:cancel' }], 'stream:cancel', deadline());
    const cancelling = await until(observe, view => view.preview?.text === 'partial' && view.preview?.reasoning === 'Checking the request');
    await client.call('turns.cancel', { turn_id: cancelling.preview.turn_id }, { signal: AbortSignal.timeout(3000) });
    assert.equal((await client.wait('stream:cancel', deadline())).turn.state, 'cancelled');
    const afterCancel = await observe();
    assert.equal(afterCancel.preview, null);
    assert.equal(afterCancel.messages.some(message => message.id === cancelling.preview.message_id), false);
    assert.ok(requests.every(request => request.stream === true && request.stream_options?.include_usage === true));
    assert.equal(JSON.stringify(requests).includes('Checking the request'), false, 'reasoning preview was replayed in provider context');
    evidence.push({ provisional, committed, ledger, bounded, afterBounded, beforeKill, afterKill, interrupted, interruptedAttempts, afterCancel });
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

async function mailEvidenceAcceptance(runtime, client, createParams, evidence) {
  const { root } = await client.call('trees.create', createParams, deadline());
  const sender = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'evidence sender' }], grant_ids: [] }, 'evidence-sender', deadline());
  const recipient = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'evidence recipient' }], grant_ids: [] }, 'evidence-recipient', deadline());
  await client.wait('evidence-sender', deadline());
  await client.wait('evidence-recipient', deadline());
  const bytes = Buffer.from('MAIL_EVIDENCE_BYTES_STAY_LAZY 🌏\n'.repeat(2400));
  const source = await client.call('content.put', { session_id: sender.session.id, reference_id: 'mail-evidence-source', media_type: 'text/plain', data_base64: bytes.toString('base64') }, deadline());
  const params = { sender_id: sender.session.id, recipient_id: recipient.session.id, delivery: 'next_turn', subject: 'Full evidence', body: '', evidence_ref: source.id };
  let dropped;
  const proxyPath = join(runtime.directory, 'mail-evidence-drop.sock');
  const close = await dropAcknowledgement(proxyPath, runtime.info.socket, 'mail-evidence', value => { dropped = value; }, response => response.result?.mail_id === 'mail-evidence');
  try {
    const unreliable = await Client.connect(unixSocket(proxyPath), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    await assert.rejects(unreliable.sendMail(params, 'mail-evidence', deadline()), DeliveryError);
  } finally { await close(); }
  const admitted = await client.sendMail(params, 'mail-evidence', deadline());
  assert.deepEqual(admitted, dropped);
  const shared = admitted.mail.evidence_ref;
  assert.ok(shared);
  assert.notEqual(shared, source.id, 'recipient must receive its own authority identity');
  const read = () => client.call('mail.read', { session_id: recipient.session.id, mail_id: admitted.mail_id }, deadline());
  const content = () => client.call('content.read', { session_id: recipient.session.id, reference_id: shared }, deadline());
  const initial = await read();
  assert.equal(initial.body, '');
  assert.equal(initial.mail.body_bytes, '0');
  assert.equal(initial.mail.state, 'pending');
  const inbox = await client.call('mail.list', { session_id: recipient.session.id, limit: 100 }, deadline());
  assert.equal(inbox.items.find(mail => mail.id === admitted.mail_id).evidence_ref, shared);
  const full = await content();
  assert.equal(full.reference.session_id, recipient.session.id);
  assert.equal(full.reference.digest, source.digest);
  assert.deepEqual(Buffer.from(full.data_base64, 'base64'), bytes);
  for (const [owner, reference] of [[root.id, shared], [sender.session.id, shared], [recipient.session.id, source.id]]) {
    await assert.rejects(client.call('content.read', { session_id: owner, reference_id: reference }, deadline()), error => error.kind === 'NOT_FOUND');
  }
  await assert.rejects(client.sendMail({ ...params, evidence_ref: shared }, 'mail-evidence-foreign', deadline()), error => error.kind === 'NOT_FOUND');
  await assert.rejects(client.sendMail({ ...params, evidence_ref: source.digest }, 'mail-evidence-digest', deadline()), error => error.kind === 'NOT_FOUND');
  await assert.rejects(client.sendMail({ ...params, evidence_ref: null }, 'mail-evidence-empty', deadline()), error => error.kind === 'INVALID');
  await assert.rejects(client.sendMail({ ...params, evidence_ref: shared }, admitted.mail_id, deadline()), error => error.kind === 'CONFLICT');
  assert.equal((await read()).mail.state, 'pending', 'inspection and content reads must not acknowledge mail');
  await client.call('sessions.delete', { session_id: sender.session.id }, deadline());
  await runtime.stop(); await runtime.start('0');
  assert.equal((await client.sendMail(params, 'mail-evidence', deadline())).mail.evidence_ref, shared);
  assert.deepEqual(await content(), full);
  await client.submit(recipient.session.id, [{ type: 'text', text: 'present scoped evidence metadata' }], 'mail-evidence-present', deadline());
  const completed = await client.wait('mail-evidence-present', deadline());
  assert.equal(completed.turn.state, 'succeeded', completed.turn.failure);
  assert.equal((await read()).mail.state, 'delivered');
  const history = await client.call('sessions.history', { session_id: recipient.session.id, after: '0', limit: 100 }, deadline());
  const presented = history.items.filter(message => message.mail?.id === admitted.mail_id);
  assert.equal(presented.length, 1);
  assert.equal(presented[0].mail.revision, '1');
  assert.ok(JSON.stringify(presented[0].parts).includes(shared));
  assert.equal(JSON.stringify(history).includes('MAIL_EVIDENCE_BYTES_STAY_LAZY'), false, 'mail must not hydrate the evidence into context');
  assert.equal((await client.sendMail(params, 'mail-evidence', deadline())).mail.evidence_ref, shared);
  assert.deepEqual(await content(), full);
  evidence.push({ mailEvidence: { source, admitted, initial, shared, completed, presented } });
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
  assert.equal(rootResources.items.length, 7);
  assert.equal(scope(rootResources, root.id, 'descendants').limit, '1');
  const spawnParams = {
    parent_id: root.id, parts: [{ type: 'text', text: 'resource child' }], overrides: {}, grant_ids: [],
    resources: [{ kind: 'descendants', limit: '0' }],
  };
  const child = await client.spawn(spawnParams, 'resource-child', deadline());
  const childResources = await list(child.session.id);
  assert.deepEqual(childResources.items.map(item => item.session_id), [
    ...Array(7).fill(child.session.id), ...Array(7).fill(root.id),
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
  const evidenceRef = report.mail.evidence_ref;
  assert.ok(evidenceRef);
  assert.equal(evidenceRef, delivered.evidence_ref);
  assert.equal(Object.hasOwn(notice, 'evidence_ref'), false, 'attachments belong to mail metadata');
  assert.equal(notice.turn_id, finished.turn.id);
  assert.equal(notice.input_id, finished.input.id);
  assert.equal(notice.mode, 'inline');
  assert.equal(notice.text_truncated, true);
  assert.ok(Buffer.byteLength(notice.preview) <= 4096 && Buffer.byteLength(notice.preview) > 2048);
  const full = await client.call('content.read', { session_id: root.id, reference_id: evidenceRef }, deadline());
  const snapshot = JSON.parse(Buffer.from(full.data_base64, 'base64'));
  assert.equal(snapshot.text, 'ack: ' + prompt);
  assert.equal(snapshot.text_bytes, String(Buffer.byteLength(snapshot.text)));
  await assert.rejects(client.call('content.read', { session_id: child.session.id, reference_id: evidenceRef }, deadline()), error => error.kind === 'NOT_FOUND');
  await client.call('sessions.delete', { session_id: child.session.id }, deadline());
  assert.deepEqual(await client.call('content.read', { session_id: root.id, reference_id: evidenceRef }, deadline()), full);
  const history = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
  const presented = history.items.filter(item => item.mail?.id === delivered.id);
  assert.equal(presented.length, 1);
  assert.ok(JSON.stringify(presented[0].parts).includes(evidenceRef), 'truncated digest must retain its attachment');
  assert.equal(history.items.filter(item => item.input_id !== null).length, 0, 'report wakes a normal mail turn without synthetic input');
  assert.equal((await client.call('completions.list', { parent_id: root.id, limit: 100 }, deadline())).items.length, 0);
  const quiet = await client.spawn({ parent_id: root.id, overrides: { report_mode: 'message' }, parts: [{ type: 'text', text: 'I report explicitly' }], grant_ids: [] }, 'report-message', deadline());
  await client.wait('report-message', deadline());
  assert.equal((await client.call('mail.list', { session_id: root.id, limit: 100 }, deadline())).items.length, 1);
  await runtime.stop(); await runtime.start('0');
  const reopened = await client.call('mail.list', { session_id: root.id, limit: 100 }, deadline());
  assert.equal(reopened.items.length, 1, 'restart must not regenerate already published reports');
  assert.equal(reopened.items[0].evidence_ref, evidenceRef);
  assert.deepEqual(await client.call('content.read', { session_id: root.id, reference_id: evidenceRef }, deadline()), full);
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
    const hiddenMetadata = '---\nname: hidden\ndescription: >-\n  Hidden skill metadata.\ndisable-model-invocation: true\n---\n';
    const hiddenBody = hiddenMetadata + 'HIDDEN_BODY_BEFORE' + 'x'.repeat(70 << 10);
    await writeFile(hidden, hiddenBody, { mode: 0o600 });
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
    const policy = { text: 'CONFIGURED_INSTRUCTIONS_BEFORE', project_files: ['AGENTS.md'], discover_skills: true, skill_roots: [], standing_instructions: false, project_root: null };
    const { root } = await client.call('trees.create', {
      ...createParams, engine: 'quickjs', working_directory: workspace,
      overrides: { ...createParams.overrides, model: { provider: 'instructions', name: 'instructions', effort: '' }, instructions: policy },
    }, deadline());
    assert.deepEqual(await client.call('skills.list', { session_id: root.id, limit: 100 }, deadline()), { items: [], next_after: null });
    const grant = await client.call('grants.create', { id: 'instruction-read-before', session_id: root.id, capability: 'files.read', resource: workspace }, deadline());
    const catalog = await client.call('skills.list', { session_id: root.id, limit: 1 }, deadline());
    assert.equal(catalog.items[0].name, 'hidden');
    assert.equal(catalog.items[0].disabled, true);
    assert.equal(catalog.items[0].source.kind, 'skill_metadata');
    assert.equal(catalog.next_after, 'hidden');
    const nextCatalog = await client.call('skills.list', { session_id: root.id, after: catalog.next_after, limit: 1 }, deadline());
    assert.deepEqual(nextCatalog.items.map(skill => skill.name), ['visible']);
    assert.equal(nextCatalog.next_after, null);
    assert.deepEqual((await client.call('skills.list', { session_id: root.id, prefix: 'Hidden', limit: 100 }, deadline())).items, []);
    assert.deepEqual((await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline())).items, []);
    const literalInput = 'original instruction input $hidden $hidden';
    await client.submit(root.id, [{ type: 'text', text: literalInput }], 'instructions-before', deadline());
    await until(async () => requests.length, count => count === 1);
    const active = await client.recover('instructions-before', deadline());
    const captured = await client.call('turns.instructions', { turn_id: active.turn.id }, deadline());
    const instructions = requests[0].messages[0].content;
    assert.ok(instructions.includes('PROJECT_INSTRUCTIONS_BEFORE'));
    assert.ok(instructions.includes('Visible skill metadata.'));
    assert.ok(!instructions.includes('<name>hidden</name>'), 'disabled skills stay out of automatic catalog');
    assert.ok(!instructions.includes('SKILL_BODY_MUST_STAY_DEFERRED'));
    assert.equal(instructions.split('HIDDEN_BODY_BEFORE').length - 1, 1, 'disabled explicit references load one full body');
    assert.equal(captured.manifest.sha256, createHash('sha256').update(instructions).digest('hex'));
    assert.equal(captured.manifest.bytes, String(Buffer.byteLength(instructions)));
    assert.equal(captured.manifest.sources.length, 4, 'audit includes metadata plus the selected body');
    const invocation = captured.manifest.sources.find(source => source.kind === 'invoked_skill');
    assert.equal(invocation.bytes, String(Buffer.byteLength(hiddenBody)));
    assert.equal(invocation.sha256, createHash('sha256').update(hiddenBody).digest('hex'));
    const skill = captured.manifest.sources.find(source => source.path === '.agents/skills/visible/SKILL.md');
    assert.equal(skill.bytes, String(Buffer.byteLength(metadata)));
    assert.equal(skill.sha256, createHash('sha256').update(metadata).digest('hex'));
    await writeFile(join(workspace, 'AGENTS.md'), 'PROJECT_INSTRUCTIONS_AFTER', { mode: 0o600 });
    await writeFile(hidden, hiddenMetadata + 'HIDDEN_BODY_AFTER');
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
    assert.deepEqual((await client.call('skills.list', { session_id: root.id, limit: 100 }, deadline())).items, [], 'revoked catalog inspection never probes malformed files');
    await writeFile(visible, metadata + 'SKILL_BODY_MUST_STAY_DEFERRED', { mode: 0o600 });
    await client.call('grants.create', { id: 'instruction-read-after', session_id: root.id, capability: 'files.read', resource: workspace }, deadline());
    const spawned = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'child instructions' }], grant_ids: null }, 'instructions-child', deadline());
    const childDone = await client.wait('instructions-child', deadline());
    assert.equal(childDone.turn.state, 'succeeded', childDone.turn.failure);
    assert.ok(requests.at(-1).messages[0].content.includes('PROJECT_INSTRUCTIONS_AFTER'));
    assert.ok(!requests.at(-1).messages[0].content.includes('HIDDEN_BODY_AFTER'), 'child does not inherit an earlier turn invocation');
    const currentChild = await client.call('sessions.get', { session_id: spawned.session.id }, deadline());
    const childPolicy = { ...policy, discover_skills: false };
    await client.call('sessions.configure', { session_id: spawned.session.id, expected_revision: currentChild.config_revision, patch: { instructions: childPolicy } }, deadline());
    await runtime.stop();
    await writeFile(join(workspace, 'AGENTS.md'), 'PROJECT_INSTRUCTIONS_RESTARTED', { mode: 0o600 });
    await runtime.start(null);
    assert.deepEqual(await client.call('turns.instructions', { turn_id: completed.turn.id }, deadline()), captured);
    await client.submit(spawned.session.id, [{ type: 'text', text: 'refresh after restart $hidden' }], 'instructions-restart', deadline());
    const restarted = await client.wait('instructions-restart', deadline());
    assert.equal(restarted.turn.state, 'succeeded', restarted.turn.failure);
    assert.ok(requests.at(-1).messages[0].content.includes('PROJECT_INSTRUCTIONS_RESTARTED'));
    assert.ok(requests.at(-1).messages[0].content.includes('HIDDEN_BODY_AFTER'));
    assert.ok(!requests.at(-1).messages[0].content.includes('<available_skills>'));
    assert.equal((await client.call('skills.list', { session_id: spawned.session.id, prefix: 'hid', limit: 100 }, deadline())).items[0].disabled, true, 'inspection remains available with automatic discovery disabled');
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
    assert.equal(history.items[0].parts[0].text, literalInput, 'skill invocation never rewrites canonical input');
    evidence.push({ instructions: { completed, captured, denied, childDone, restarted, failed, maintenance } });
  } finally {
    release.resolve();
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}

async function hostSkillAcceptance(runtime, client, createParams, evidence) {
  const requests = new Map();
  const releases = new Map(['starlark', 'quickjs'].map(engine => [engine, Promise.withResolvers()]));
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    const engine = body.model;
    const received = requests.get(engine) ?? [];
    received.push(body); requests.set(engine, received);
    const first = received.length === 1;
    if (first) await releases.get(engine).promise;
    const code = engine === 'starlark'
      ? 'p=skills.read(scope="host",root_id="team",name="global",offset="0",length=65536)\nq=skills.read(scope="host",root_id="team",name="global",offset=p["next_offset"],length=65536,sha256=p["sha256"])\nprint(p["total_bytes"], p["sha256"], q["next_offset"])'
      : 'const p=await skills.read({scope:"host",root_id:"team",name:"global",offset:"0",length:65536}); const q=await skills.read({scope:"host",root_id:"team",name:"global",offset:p.next_offset,length:65536,sha256:p.sha256}); console.log(p.total_bytes,p.sha256,q.next_offset);';
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message: first
      ? { role: 'assistant', content: null, tool_calls: [{ id: `host-skill-${engine}`, type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] }
      : { role: 'assistant', content: 'Named skill completed.' }, finish_reason: first ? 'tool_calls' : 'stop' }] }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    const hostRoot = join(runtime.directory, 'host-skills');
    await mkdir(join(hostRoot, 'global'), { recursive: true });
    await mkdir(join(hostRoot, 'same'), { recursive: true });
    const metadata = '---\nname: global\ndescription: Global metadata\ndisable-model-invocation: true\n---\n';
    const globalPath = join(hostRoot, 'global', 'SKILL.md');
    await writeFile(join(hostRoot, 'same', 'SKILL.md'), '---\nname: same\ndescription: HOST_DUPLICATE\n---\nHOST_SAME_BODY');
    await runtime.stop();
    const hostPath = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(hostPath, 'utf8'));
    host.skill_roots = { team: hostRoot, unused: join(runtime.directory, 'never-probe-missing-root') };
    host.providers.hostskills = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: Object.fromEntries(['starlark', 'quickjs'].map(engine => [engine, { max_output_tokens: 128, timeout_millis: 5000, max_attempts: 1 }])),
    };
    await writeFile(hostPath, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    for (const engine of ['starlark', 'quickjs']) {
      const workspace = join(runtime.directory, `host-skill-${engine}`);
      await mkdir(join(workspace, '.agents', 'skills', 'same'), { recursive: true });
      await writeFile(join(workspace, '.agents', 'skills', 'same', 'SKILL.md'), '---\nname: same\ndescription: WORKSPACE_DUPLICATE\n---\nWORKSPACE_SAME_BODY');
      await writeFile(globalPath, metadata + 'GLOBAL_BEFORE');
      const policy = { text: '', project_files: [], discover_skills: true, skill_roots: ['unused', 'team'], standing_instructions: false, project_root: null };
      const { root } = await client.call('trees.create', { ...createParams, engine, working_directory: workspace,
        overrides: { ...createParams.overrides, model: { provider: 'hostskills', name: engine, effort: '' }, instructions: policy },
      }, deadline());
      assert.deepEqual((await client.call('skills.list', { session_id: root.id, limit: 100 }, deadline())).items, []);
      await client.call('grants.create', { id: `host-skill-${engine}`, session_id: root.id, capability: 'skills.read', resource: 'team' }, deadline());
      await client.call('grants.create', { id: `host-workspace-${engine}`, session_id: root.id, capability: 'files.read', resource: workspace }, deadline());
      const catalog = await client.call('skills.list', { session_id: root.id, limit: 100 }, deadline());
      assert.deepEqual(catalog.items.map(skill => skill.name), ['global', 'same']);
      assert.equal(catalog.items[0].source.root_id, 'team');
      assert.equal(catalog.items[0].source.scope, 'host');
      assert.equal(catalog.items[1].description, 'WORKSPACE_DUPLICATE');
      assert.equal(catalog.items[1].source.root_id, null);
      assert.ok(!JSON.stringify(catalog).includes(hostRoot), 'catalog cannot expose host paths');
      const key = `host-skills-${engine}`;
      await client.submit(root.id, [{ type: 'text', text: 'Use $global' }], key, deadline());
      await until(async () => requests.get(engine)?.length ?? 0, count => count === 1);
      const captured = requests.get(engine)[0].messages[0].content;
      assert.ok(captured.includes('GLOBAL_BEFORE'));
      assert.ok(captured.includes('WORKSPACE_DUPLICATE'));
      assert.ok(!captured.includes('HOST_DUPLICATE'));
      assert.ok(!captured.includes(hostRoot));
      const changed = metadata + 'GLOBAL_AFTER\n' + '🌍'.repeat(18_000);
      const expectedHash = createHash('sha256').update(changed).digest('hex');
      await writeFile(globalPath, changed);
      const configured = await client.call('sessions.configure', { session_id: root.id, expected_revision: root.config_revision, patch: { instructions: { ...policy, skill_roots: [], standing_instructions: false, project_root: null } } }, deadline());
      releases.get(engine).resolve();
      const done = await client.wait(key, deadline());
      assert.equal(done.turn.state, 'succeeded', done.turn.failure);
      assert.equal(requests.get(engine)[1].messages[0].content, captured, 'active instructions retain old body and root policy');
      const operations = await client.call('turns.operations', { turn_id: done.turn.id, limit: 100 }, deadline());
      assert.equal(operations.items.length, 2);
      const pages = operations.items.map(operation => {
        assert.equal(operation.capability, 'skills.read');
        assert.equal(operation.resource, 'team');
        assert.equal(operation.state, 'succeeded', operation.result?.failure);
        assert.equal(operation.result.value.sha256, expectedHash);
        assert.equal(operation.result.value.total_bytes, String(Buffer.byteLength(changed)));
        return operation.result.value;
      }).sort((a, b) => Number(BigInt(a.offset) - BigInt(b.offset)));
      assert.equal(Buffer.concat(pages.map(page => Buffer.from(page.data_base64, 'base64'))).toString('utf8'), changed);
      assert.equal(pages.at(-1).next_offset, null);
      const audit = await client.call('turns.instructions', { turn_id: done.turn.id }, deadline());
      assert.equal(audit.manifest.sources.find(source => source.kind === 'invoked_skill').root_id, 'team');
      assert.equal(audit.manifest.sources.find(source => source.kind === 'invoked_skill').sha256, createHash('sha256').update(metadata + 'GLOBAL_BEFORE').digest('hex'));
      assert.deepEqual((await client.call('skills.list', { session_id: root.id, limit: 100 }, deadline())).items.map(skill => skill.name), ['same'], 'current inspection uses edited policy');
      await client.submit(root.id, [{ type: 'text', text: 'next without reference' }], `${key}-next`, deadline());
      assert.equal((await client.wait(`${key}-next`, deadline())).turn.state, 'succeeded');
      assert.ok(!requests.get(engine).at(-1).messages[0].content.includes('GLOBAL_AFTER'));
      await client.call('sessions.configure', { session_id: root.id, expected_revision: configured.config_revision, patch: { instructions: { ...policy, skill_roots: ['team'] } } }, deadline());
      const child = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'Use $global' }], grant_ids: null }, `${key}-child`, deadline());
      const childDone = await client.wait(`${key}-child`, deadline());
      assert.equal(childDone.turn.state, 'succeeded', childDone.turn.failure);
      assert.ok(requests.get(engine).at(-1).messages[0].content.includes('GLOBAL_AFTER'));
      await runtime.stop();
      await writeFile(globalPath, metadata + 'GLOBAL_RESTARTED');
      await runtime.start(null);
      assert.deepEqual(await client.call('turns.instructions', { turn_id: done.turn.id }, deadline()), audit);
      await client.submit(child.session.id, [{ type: 'text', text: 'Use $global' }], `${key}-restart`, deadline());
      const restarted = await client.wait(`${key}-restart`, deadline());
      assert.equal(restarted.turn.state, 'succeeded', restarted.turn.failure);
      assert.ok(requests.get(engine).at(-1).messages[0].content.includes('GLOBAL_RESTARTED'));
      await client.call('grants.revoke', { grant_id: `host-skill-${engine}` }, deadline());
      assert.deepEqual((await client.call('skills.list', { session_id: child.session.id, limit: 100 }, deadline())).items.map(skill => skill.name), ['same'], 'issuer revocation removes child host access');
      evidence.push({ hostSkills: { engine, done, audit, pages, childDone, restarted } });
    }
  } finally {
    for (const release of releases.values()) release.resolve();
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}

async function standingInstructionAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  let blockNext = false;
  const release = Promise.withResolvers();
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    requests.push(JSON.parse(raw));
    const blocked = blockNext;
    blockNext = false;
    if (blocked) await release.promise;
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message: blocked
      ? { role: 'assistant', content: null, tool_calls: [{ id: 'standing-cell', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code: 'console.log(7)' }) } }] }
      : { role: 'assistant', content: 'Standing instructions completed.' }, finish_reason: blocked ? 'tool_calls' : 'stop' }] }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    const file = join(runtime.directory, 'me.md');
    const original = '# COMMENT_NOT_IN_PROMPT\n\n  STANDING_BEFORE  \n   # HIDDEN_COMMENT\n STANDING_SECOND\n';
    await writeFile(file, original, { mode: 0o600 });
    await runtime.stop();
    const hostPath = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(hostPath, 'utf8'));
    host.standing_instructions_file = file;
    host.providers.standing = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: { standing: { max_output_tokens: 128, timeout_millis: 5000, max_attempts: 1 } },
    };
    await writeFile(hostPath, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    const workspace = join(runtime.directory, 'standing-workspace');
    await mkdir(workspace);
    const policy = { text: 'ordinary configured text', project_files: [], discover_skills: false, skill_roots: [], standing_instructions: true, project_root: null };
    const { root } = await client.call('trees.create', { ...createParams, engine: 'quickjs', working_directory: workspace,
      overrides: { ...createParams.overrides, model: { provider: 'standing', name: 'standing', effort: '' }, instructions: policy },
    }, deadline());
    await client.submit(root.id, [{ type: 'text', text: 'no standing authority' }], 'standing-denied', deadline());
    const denied = await client.wait('standing-denied', deadline());
    assert.equal(denied.turn.state, 'succeeded', denied.turn.failure);
    assert.ok(!requests.at(-1).messages[0].content.includes('STANDING_BEFORE'));
    assert.deepEqual((await client.call('turns.instructions', { turn_id: denied.turn.id }, deadline())).manifest.sources, []);
    const grant = await client.call('grants.create', { id: 'standing-before', session_id: root.id, capability: 'instructions.read', resource: 'standing' }, deadline());
    blockNext = true;
    await client.submit(root.id, [{ type: 'text', text: 'standing capture' }], 'standing-active', deadline());
    await until(async () => requests.length, count => count === 2);
    const active = await client.recover('standing-active', deadline());
    const captured = await client.call('turns.instructions', { turn_id: active.turn.id }, deadline());
    const instructions = requests[1].messages[0].content;
    assert.ok(instructions.includes('STANDING_BEFORE\nSTANDING_SECOND'));
    assert.ok(!instructions.includes('COMMENT_NOT_IN_PROMPT'));
    assert.ok(!instructions.includes('HIDDEN_COMMENT'));
    assert.deepEqual(captured.manifest.sources, [{ kind: 'standing_instructions', scope: 'host', root_id: 'standing', path: 'me.md', bytes: String(Buffer.byteLength(original)), sha256: createHash('sha256').update(original).digest('hex') }]);
    assert.ok(!JSON.stringify(captured).includes(file));
    await writeFile(file, 'STANDING_AFTER');
    const disabled = await client.call('sessions.configure', { session_id: root.id, expected_revision: root.config_revision, patch: { instructions: { ...policy, standing_instructions: false, project_root: null } } }, deadline());
    await client.call('grants.revoke', { grant_id: grant.id }, deadline());
    release.resolve();
    const done = await client.wait('standing-active', deadline());
    assert.equal(done.turn.state, 'succeeded', done.turn.failure);
    assert.equal(requests[2].messages[0].content, instructions, 'standing bytes stay frozen through later cells');
    await writeFile(file, Buffer.from([0xff]));
    await client.submit(root.id, [{ type: 'text', text: 'disabled source' }], 'standing-disabled', deadline());
    assert.equal((await client.wait('standing-disabled', deadline())).turn.state, 'succeeded');
    const enabled = await client.call('sessions.configure', { session_id: root.id, expected_revision: disabled.config_revision, patch: { instructions: policy } }, deadline());
    await client.submit(root.id, [{ type: 'text', text: 'revoked source' }], 'standing-revoked', deadline());
    const revoked = await client.wait('standing-revoked', deadline());
    assert.equal(revoked.turn.state, 'succeeded', revoked.turn.failure);
    assert.deepEqual((await client.call('turns.instructions', { turn_id: revoked.turn.id }, deadline())).manifest.sources, []);
    await writeFile(file, '# ignored\nSTANDING_AFTER');
    await client.call('grants.create', { id: 'standing-after', session_id: root.id, capability: 'instructions.read', resource: 'standing' }, deadline());
    const child = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'child standing instructions' }], grant_ids: null }, 'standing-child', deadline());
    const childDone = await client.wait('standing-child', deadline());
    assert.equal(childDone.turn.state, 'succeeded', childDone.turn.failure);
    assert.ok(requests.at(-1).messages[0].content.includes('STANDING_AFTER'));
    assert.equal(child.session.configuration.instructions.standing_instructions, enabled.configuration.instructions.standing_instructions);
    await runtime.stop();
    await writeFile(file, 'STANDING_RESTARTED');
    await runtime.start(null);
    assert.deepEqual(await client.call('turns.instructions', { turn_id: done.turn.id }, deadline()), captured);
    await client.submit(child.session.id, [{ type: 'text', text: 'new standing instructions' }], 'standing-restart', deadline());
    const restarted = await client.wait('standing-restart', deadline());
    assert.equal(restarted.turn.state, 'succeeded', restarted.turn.failure);
    assert.ok(requests.at(-1).messages[0].content.includes('STANDING_RESTARTED'));
    await writeFile(file, Buffer.from([0xff]));
    const count = requests.length;
    await client.submit(child.session.id, [{ type: 'text', text: 'bad standing source' }], 'standing-broken', deadline());
    const failed = await client.wait('standing-broken', deadline());
    assert.equal(failed.turn.state, 'failed');
    assert.equal(requests.length, count);
    assert.deepEqual((await client.call('turns.attempts', { turn_id: failed.turn.id, limit: 100 }, deadline())).items, []);
    assert.equal((await client.call('turns.instructions', { turn_id: failed.turn.id }, deadline())).manifest, null);
    assert.deepEqual((await client.call('skills.list', { session_id: child.session.id, limit: 100 }, deadline())).items, [], 'catalog inspection does not read standing source');
    await client.compact(child.session.id, 'standing-maintenance', deadline());
    const maintenance = await client.wait('standing-maintenance', deadline());
    assert.equal(maintenance.turn.state, 'succeeded', maintenance.turn.failure);
    assert.equal((await client.call('turns.instructions', { turn_id: maintenance.turn.id }, deadline())).manifest, null);
    evidence.push({ standing: { denied, done, captured, revoked, childDone, restarted, failed, maintenance } });
  } finally {
    release.resolve();
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}


async function projectInstructionAcceptance(runtime, client, createParams, evidence) {
  const requests = new Map();
  const releases = new Map(['starlark', 'quickjs'].map(engine => [engine, Promise.withResolvers()]));
  const server = http.createServer(async (request, response) => {
    let raw = '';
    for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    const engine = body.model;
    const received = requests.get(engine) ?? [];
    received.push(body); requests.set(engine, received);
    const first = received.length === 1;
    if (first) await releases.get(engine).promise;
    const code = engine === 'starlark'
      ? 'p=skills.read(scope="project",root_id="repo",name="ancestor",offset="0",length=65536)\nprint(p["source"]["scope"],p["source"]["path"])'
      : 'const p=await skills.read({scope:"project",root_id:"repo",name:"ancestor",offset:"0",length:65536}); console.log(p.source.scope,p.source.path);';
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message: first
      ? { role: 'assistant', content: null, tool_calls: [{ id: `project-${engine}`, type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] }
      : { role: 'assistant', content: 'Project instructions completed.' }, finish_reason: first ? 'tool_calls' : 'stop' }] }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  try {
    const boundary = join(runtime.directory, 'project-boundary');
    const skillPath = join(boundary, '.agents', 'skills', 'ancestor', 'SKILL.md');
    await mkdir(join(boundary, '.agents', 'skills', 'ancestor'), { recursive: true });
    const metadata = '---\nname: ancestor\ndescription: Inherited ancestor skill\ndisable-model-invocation: true\n---\n';
    await runtime.stop();
    const hostPath = join(runtime.directory, 'state', 'host.json');
    const host = JSON.parse(await readFile(hostPath, 'utf8'));
    host.project_roots = { repo: boundary };
    host.providers.projects = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: Object.fromEntries(['starlark', 'quickjs'].map(engine => [engine, { max_output_tokens: 128, timeout_millis: 5000, max_attempts: 1 }])),
    };
    await writeFile(hostPath, JSON.stringify(host), { mode: 0o600 });
    await runtime.start(null);
    for (const engine of ['starlark', 'quickjs']) {
      const workspace = join(boundary, engine);
      const alias = join(runtime.directory, `project-alias-${engine}`);
      await mkdir(workspace);
      await symlink(workspace, alias);
      await writeFile(join(boundary, 'AGENTS.md'), 'PROJECT_ANCESTOR_BEFORE');
      await writeFile(join(workspace, 'AGENTS.md'), 'PROJECT_LOCAL_RULE');
      await writeFile(skillPath, metadata + 'PROJECT_BODY_BEFORE');
      const policy = { text: '', project_files: ['CLAUDE.md', 'AGENTS.md'], discover_skills: true, skill_roots: [], standing_instructions: false, project_root: 'repo' };
      const { root } = await client.call('trees.create', { ...createParams, engine, working_directory: alias,
        overrides: { ...createParams.overrides, model: { provider: 'projects', name: engine, effort: '' }, instructions: policy },
      }, deadline());
      assert.deepEqual((await client.call('skills.list', { session_id: root.id, limit: 100 }, deadline())).items, []);
      const grant = await client.call('grants.create', { id: `project-${engine}`, session_id: root.id, capability: 'instructions.read', resource: 'project:repo' }, deadline());
      const catalog = await client.call('skills.list', { session_id: root.id, limit: 100 }, deadline());
      assert.equal(catalog.items.length, 1);
      assert.equal(catalog.items[0].disabled, true);
      assert.deepEqual([catalog.items[0].source.scope, catalog.items[0].source.root_id, catalog.items[0].source.path], ['project', 'repo', '.agents/skills/ancestor/SKILL.md']);
      const key = `project-${engine}`;
      await client.submit(root.id, [{ type: 'text', text: 'Use $ancestor and read its source' }], key, deadline());
      await until(async () => requests.get(engine)?.length ?? 0, count => count === 1);
      const frozen = requests.get(engine)[0].messages[0].content;
      for (const marker of ['PROJECT_ANCESTOR_BEFORE', 'PROJECT_LOCAL_RULE', 'PROJECT_BODY_BEFORE']) assert.ok(frozen.includes(marker), marker);
      assert.ok(frozen.indexOf('PROJECT_ANCESTOR_BEFORE') < frozen.indexOf('PROJECT_LOCAL_RULE'));
      assert.ok(!JSON.stringify(catalog).includes(boundary));
      const cleared = await client.call('sessions.configure', { session_id: root.id, expected_revision: root.config_revision, patch: { instructions: { ...policy, project_root: null } } }, deadline());
      releases.get(engine).resolve();
      const done = await client.wait(key, deadline());
      assert.equal(done.turn.state, 'succeeded', done.turn.failure);
      assert.equal(requests.get(engine)[1].messages[0].content, frozen);
      const operations = await client.call('turns.operations', { turn_id: done.turn.id, limit: 100 }, deadline());
      assert.equal(operations.items.length, 1);
      const operation = operations.items[0];
      assert.deepEqual([operation.capability, operation.resource, operation.state], ['instructions.read', 'project:repo', 'succeeded']);
      assert.equal(operation.result.value.source.scope, 'project');
      assert.equal(Buffer.from(operation.result.value.data_base64, 'base64').toString('utf8'), metadata + 'PROJECT_BODY_BEFORE');
      const audit = await client.call('turns.instructions', { turn_id: done.turn.id }, deadline());
      assert.equal(audit.manifest.sources.length, 4);
      assert.ok(audit.manifest.sources.every(source => source.scope === 'project' && source.root_id === 'repo'));
      assert.deepEqual((await client.call('skills.list', { session_id: root.id, limit: 100 }, deadline())).items, []);
      await client.call('sessions.configure', { session_id: root.id, expected_revision: cleared.config_revision, patch: { instructions: policy } }, deadline());
      await writeFile(join(boundary, 'AGENTS.md'), 'PROJECT_ANCESTOR_AFTER');
      const restricted = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'restricted child' }], grant_ids: [] }, `${key}-restricted`, deadline());
      assert.equal((await client.wait(`${key}-restricted`, deadline())).turn.state, 'succeeded');
      assert.ok(!requests.get(engine).at(-1).messages[0].content.includes('PROJECT_ANCESTOR_AFTER'));
      const delegated = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: 'delegated child' }], grant_ids: null }, `${key}-delegated`, deadline());
      assert.equal((await client.wait(`${key}-delegated`, deadline())).turn.state, 'succeeded');
      assert.ok(requests.get(engine).at(-1).messages[0].content.includes('PROJECT_ANCESTOR_AFTER'));
      assert.equal(delegated.session.configuration.instructions.project_root, 'repo');
      await runtime.stop();
      await writeFile(skillPath, metadata + 'PROJECT_BODY_RESTARTED');
      await runtime.start(null);
      assert.deepEqual(await client.call('turns.instructions', { turn_id: done.turn.id }, deadline()), audit);
      await client.submit(delegated.session.id, [{ type: 'text', text: 'Use $ancestor again' }], `${key}-restart`, deadline());
      const restarted = await client.wait(`${key}-restart`, deadline());
      assert.equal(restarted.turn.state, 'succeeded', restarted.turn.failure);
      assert.ok(requests.get(engine).at(-1).messages[0].content.includes('PROJECT_BODY_RESTARTED'));
      await client.call('grants.revoke', { grant_id: grant.id }, deadline());
      await writeFile(join(boundary, 'AGENTS.md'), Buffer.from([0xff]));
      assert.deepEqual((await client.call('skills.list', { session_id: delegated.session.id, limit: 100 }, deadline())).items, []);
      await client.submit(delegated.session.id, [{ type: 'text', text: 'revoked sources' }], `${key}-revoked`, deadline());
      assert.equal((await client.wait(`${key}-revoked`, deadline())).turn.state, 'succeeded');
      evidence.push({ projectInstructions: { engine, done, audit, operation, restricted, delegated, restarted } });
    }
  } finally {
    for (const release of releases.values()) release.resolve();
    server.closeAllConnections();
    await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
}


async function scheduleAcceptance(runtime, client, createParams, evidence) {
  const { root } = await client.call('trees.create', { ...createParams, resources: [{ kind: 'schedules', limit: '2' }] }, deadline());
  for (const clientID of ['schedule', 'operation']) {
    await assert.rejects(client.call('sessions.submit', { identity: { client_id: clientID, request_id: 'collision' }, session_id: root.id, source: 'user', parts: [{ type: 'text', text: 'collision' }] }, deadline()), error => error.kind === 'INVALID');
    await assert.rejects(client.call('sessions.spawn', { identity: { client_id: clientID, request_id: 'collision' }, parent_id: root.id, parts: [{ type: 'text', text: 'collision' }], overrides: {}, grant_ids: [] }, deadline()), error => error.kind === 'INVALID');
  }
  const params = { session_id: root.id, expression: '@at 2500-01-02T03:04:05.123456789-07:00', parts: [{ type: 'text', text: 'future exact slot' }] };
  const proxy = join(runtime.directory, 'schedule-drop.sock');
  let dropped;
  const close = await dropAcknowledgement(proxy, runtime.info.socket, '', value => { dropped = value; }, response => response.result?.id === 'schedule-lost-ack');
  try {
    const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    await assert.rejects(unreliable.createSchedule(params, 'schedule-lost-ack', deadline()), DeliveryError);
  } finally { await close(); }
  const retried = await client.createSchedule(params, 'schedule-lost-ack', deadline());
  assert.deepEqual(retried, dropped);
  assert.equal(retried.schedule.next_due, '2500-01-02T10:04:05.123456789Z');
  await assert.rejects(client.createSchedule({ ...params, expression: '@every 1m' }, 'schedule-lost-ack', deadline()), error => error.kind === 'CONFLICT');
  const child = await client.spawn({ parent_id: root.id, parts: [{ type: 'text', text: 'schedule child' }], overrides: {}, grant_ids: [] }, 'schedule-child', deadline());
  await client.wait('schedule-child', deadline());
  const capacities = await client.call('resources.list', { session_id: child.session.id }, deadline());
  const queue = capacities.items.find(item => item.session_id === child.session.id && item.kind === 'queued_inputs');
  const blocked = await client.call('resources.set', { session_id: child.session.id, expected_revision: queue.revision, resource: { kind: 'queued_inputs', limit: '0' } }, deadline());
  const childParams = { session_id: child.session.id, expression: '@at 2000-01-02T03:04:05.987654321-07:00', parts: [{ type: 'text', text: 'schedule child wake' }] };
  const pending = await client.createSchedule(childParams, 'schedule-restart', deadline());
  await assert.rejects(client.createSchedule(params, 'schedule-capacity-denied', deadline()), error => error.kind === 'LIMIT');
  await client.call('sessions.lifecycle', { session_id: child.session.id, lifecycle: 'stopped' }, deadline());
  await client.call('resources.set', { session_id: child.session.id, expected_revision: blocked.revision, resource: { kind: 'queued_inputs', limit: '2' } }, deadline());
  await runtime.stop('SIGKILL'); await runtime.start();
  const before = await client.call('schedules.get', { session_id: child.session.id, schedule_id: pending.id }, deadline());
  assert.equal(before.schedule.latest, null);
  assert.equal(before.schedule.next_due, '2000-01-02T10:04:05.987654321Z');
  assert.deepEqual(before.parts, childParams.parts);
  await client.call('sessions.lifecycle', { session_id: child.session.id, lifecycle: 'active' }, deadline());
  let current;
  const end = Date.now() + 10_000;
  do {
    current = await client.call('schedules.get', { session_id: child.session.id, schedule_id: pending.id }, deadline());
    if (current.schedule.latest) break;
    assert.ok(Date.now() < end, 'due unloaded child was never admitted');
    await new Promise(resolve => setTimeout(resolve, 5));
  } while (true);
  assert.equal(current.schedule.next_due, null);
  const latest = current.schedule.latest;
  let completed;
  do {
    completed = await client.call('receipts.get', latest.identity, deadline());
    if (completed.turn?.finished_at) break;
    assert.ok(Date.now() < end, 'schedule input never completed');
    await new Promise(resolve => setTimeout(resolve, 5));
  } while (true);
  assert.equal(completed.input.source, 'schedule');
  assert.equal(completed.input.session_id, child.session.id);
  assert.deepEqual(completed.input.schedule, { schedule_id: pending.id, scheduled_for: '2000-01-02T10:04:05.987654321Z' });
  assert.equal(completed.turn.state, 'succeeded');
  const upcoming = await client.call('schedules.list', { session_id: child.session.id, upcoming: true, limit: 10 }, deadline());
  assert.deepEqual(upcoming.items, []);
  const cancelled = await client.call('schedules.cancel', { session_id: root.id, schedule_id: retried.id }, deadline());
  assert.ok(cancelled.schedule.cancelled_at);
  assert.ok((await client.createSchedule(params, retried.id, deadline())).schedule.cancelled_at);
  await client.call('sessions.delete', { session_id: root.id }, deadline());
  const deleted = await client.createSchedule(params, retried.id, deadline());
  assert.equal(deleted.schedule, null); assert.ok(deleted.deleted_at);
  const receipt = await client.call('receipts.get', latest.identity, deadline());
  assert.equal(receipt.input, null); assert.ok(receipt.receipt.deleted_at);
  evidence.push({ schedules: { retried, before, completed, upcoming, cancelled, deleted, receipt } });
}


// Successful subscription execution uses an injected transport in the Go runtime
// acceptance. This shipping-process case proves admission with the fixed route;
// a rejecting proxy guarantees a regression cannot contact the real provider.
async function subscriptionAdmissionAcceptance(runtime, client, createParams, evidence) {
  const connections = [];
  const proxy = http.createServer((request, response) => {
    connections.push(request.url);
    response.writeHead(502); response.end();
  });
  proxy.on('connect', (request, socket) => {
    connections.push(request.url);
    socket.end('HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n');
  });
  proxy.listen(0, '127.0.0.1'); await once(proxy, 'listening');
  const hostPath = join(runtime.directory, 'state', 'host.json');
  const credentialPath = join(runtime.directory, 'state', 'openai-codex.json');
  await runtime.stop();
  const previous = await readFile(hostPath, 'utf8');
  try {
    const host = JSON.parse(previous);
    host.providers.subscription = { kind: 'openai-codex', base_url: '', credential_env: '', models: { 'gpt-6-astra': { context_window_tokens: 400000, timeout_millis: 1000, max_attempts: 1 } } };
    await writeFile(hostPath, JSON.stringify(host), { mode: 0o600 });
    await writeFile(credentialPath, JSON.stringify({ accessToken: 'fixture-private-access', refreshToken: 'fixture-private-refresh', accountId: 'fixture-account', expiresAt: new Date(Date.now() + 3600000).toISOString() }), { mode: 0o600 });
    const endpoint = `http://127.0.0.1:${proxy.address().port}`;
    await runtime.start(null, { HTTPS_PROXY: endpoint, https_proxy: endpoint, NO_PROXY: '', no_proxy: '' });
    for (const engine of ['starlark', 'quickjs']) {
      for (const [kind, limit] of [['model_cost_nano_usd', '1'], ['model_tokens', '527999']]) {
        const { root } = await client.call('trees.create', { ...createParams, engine, overrides: { ...createParams.overrides, model: { provider: 'subscription', name: 'gpt-6-astra', effort: '' } } }, deadline());
        await client.call('budgets.set', { session_id: root.id, expected_revision: '0', budget: { kind, limit } }, deadline());
        const id = `subscription:${engine}:${kind}`;
        await client.submit(root.id, [{ type: 'text', text: 'must not contact provider' }], id, deadline());
        const completed = await client.wait(id, deadline());
        assert.equal(completed.turn.state, 'failed');
        assert.match(completed.turn.failure, /admission limit/);
        const attempts = await client.call('turns.attempts', { turn_id: completed.turn.id, limit: 100 }, deadline());
        assert.deepEqual(attempts.items, []);
        const history = await client.call('sessions.history', { session_id: root.id, after: '0', limit: 100 }, deadline());
        assert.equal(JSON.stringify({ completed, attempts, history }).includes('fixture-private'), false);
        evidence.push({ subscriptionAdmission: { engine, kind, completed, attempts, history } });
      }
    }
    assert.deepEqual(connections, [], 'subscription refusal attempted external HTTP');
  } finally {
    await runtime.stop();
    await writeFile(hostPath, previous, { mode: 0o600 });
    await rm(credentialPath, { force: true });
    proxy.closeAllConnections();
    await new Promise((resolve, reject) => proxy.close(error => error ? reject(error) : resolve()));
  }
  await runtime.start();
}

async function goalAcceptance(runtime, client, createParams, evidence) {
  const receipt = identity => client.call('receipts.get', identity, deadline());
  const finish = identity => until(() => receipt(identity), value => value.turn?.finished_at || value.input?.state === 'cancelled');
  const paused = (owner, id) => until(() => client.getGoal(owner, id, deadline()), value => value.state === 'paused');
  const { root: idle } = await client.call('trees.create', createParams, deadline());
  assert.equal((await client.currentGoal(idle.id, deadline())).goal, null);
  const defaults = await client.createGoal({ session_id: idle.id, expected_current: null, spec: { text: 'default allowance' }, start: false }, 'goal-default', deadline());
  assert.equal(defaults.goal.spec.max_continuations, '100');
  const replacement = await client.createGoal({ session_id: idle.id, expected_current: { id: defaults.id, revision: defaults.goal.revision }, spec: { text: 'exact allowance', max_continuations: '9007199254740993' }, start: false }, 'goal-exact', deadline());
  assert.equal(replacement.goal.spec.max_continuations, '9007199254740993');
  const old = await client.createGoal({ session_id: idle.id, expected_current: null, spec: { text: 'default allowance' }, start: false }, defaults.id, deadline());
  assert.equal(old.current, false); assert.equal(old.goal.state, 'superseded'); assert.equal(old.initial, null);
  await client.cancelGoal(idle.id, replacement.id, deadline());
  const { root: disabled } = await client.call('trees.create', { ...createParams, overrides: { ...createParams.overrides, goals_enabled: false } }, deadline());
  assert.equal(disabled.configuration.goals_enabled, false);
  await assert.rejects(client.createGoal({ session_id: disabled.id, expected_current: null, spec: { text: 'disabled' }, start: false }, 'goal-disabled', deadline()), error => error.kind === 'INVALID');

  // Saturate the worker so the lost acknowledgement is definitely for queued initial work.
  await runtime.stop(); await runtime.start('1h');
  const { root: blocker } = await client.call('trees.create', createParams, deadline());
  await client.submit(blocker.id, [{ type: 'text', text: 'hold goal queue' }], 'goal-blocker', deadline());
  await until(() => client.recover('goal-blocker', deadline()), value => value.turn?.state === 'running');
  const { root: queued } = await client.call('trees.create', createParams, deadline());
  const params = { session_id: queued.id, expected_current: null, spec: { text: 'accepted exactly once', max_continuations: '0' }, start: true };
  let dropped;
  const proxy = join(runtime.directory, 'goal-drop.sock');
  const close = await dropAcknowledgement(proxy, runtime.info.socket, '', value => { dropped = value; }, response => response.result?.id === 'goal-lost-ack');
  try {
    const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
    await assert.rejects(unreliable.createGoal(params, 'goal-lost-ack', deadline()), DeliveryError);
  } finally { await close(); }
  assert.equal(dropped.initial.input.state, 'queued');
  // The disabled queued goal is retired by Claim without a synthetic failed turn.
  const { root: changing } = await client.call('trees.create', createParams, deadline());
  const disabledQueued = await client.createGoal({ session_id: changing.id, expected_current: null, spec: { text: 'disable before claim' }, start: true }, 'goal-queued-disabled', deadline());
  await client.call('sessions.configure', { session_id: changing.id, expected_revision: changing.config_revision, patch: { goals_enabled: false } }, deadline());
  await runtime.stop('SIGKILL'); await runtime.start('0s');
  const recovered = await client.createGoal(params, 'goal-lost-ack', deadline());
  assert.equal(recovered.initial.input.id, dropped.initial.input.id);
  const completed = await finish(recovered.initial.receipt.identity);
  assert.equal(completed.turn.state, 'succeeded'); assert.equal(completed.input.source, 'goal');
  assert.equal(completed.turn.goal.id, recovered.id);
  const roundLimit = await paused(queued.id, recovered.id);
  assert.equal(roundLimit.stop_reason, 'round_limit'); assert.equal(roundLimit.continuations_used, '0');
  assert.equal((await client.call('turns.attempts', { turn_id: completed.turn.id, limit: 100 }, deadline())).items.length, 1);
  const retired = await finish(disabledQueued.initial.receipt.identity);
  assert.equal(retired.input.state, 'cancelled'); assert.equal(retired.turn, null);
  assert.equal((await paused(changing.id, disabledQueued.id)).stop_reason, 'disabled');

  // A dispatched initial attempt becomes uncertain on restart and never auto-replays.
  await runtime.stop(); await runtime.start('1h');
  const { root: interruptedOwner } = await client.call('trees.create', createParams, deadline());
  const interruptedParams = { session_id: interruptedOwner.id, expected_current: null, spec: { text: 'requires explicit resume', max_continuations: '1' }, start: true };
  const active = await client.createGoal(interruptedParams, 'goal-interrupted', deadline());
  const running = await until(() => receipt(active.initial.receipt.identity), value => value.turn?.state === 'running');
  await until(() => client.call('turns.attempts', { turn_id: running.turn.id, limit: 100 }, deadline()), value => value.items.some(item => item.state === 'dispatched'));
  await runtime.stop('SIGKILL'); await runtime.start('0s');
  const interrupted = await finish(active.initial.receipt.identity);
  assert.equal(interrupted.turn.state, 'interrupted');
  const stopped = await paused(interruptedOwner.id, active.id);
  assert.equal(stopped.stop_reason, 'turn_interrupted'); assert.equal(stopped.continuations_used, '0');
  const replay = await client.createGoal(interruptedParams, active.id, deadline());
  assert.equal(replay.initial.input.id, active.initial.input.id); assert.equal(replay.goal.state, 'paused');
  assert.equal((await client.call('sessions.history', { session_id: interruptedOwner.id, after: '0', limit: 100 }, deadline())).items.length, 1);
  const resumeRef = { id: active.id, revision: stopped.revision };
  const resumed = await client.resumeGoal(interruptedOwner.id, resumeRef, 'goal-resume', deadline());
  assert.equal((await client.wait('goal-resume', deadline())).turn.state, 'succeeded');
  const exhausted = await paused(interruptedOwner.id, active.id);
  assert.equal(exhausted.continuations_used, '1'); assert.equal(exhausted.stop_reason, 'round_limit');
  assert.equal((await client.resumeGoal(interruptedOwner.id, resumeRef, 'goal-resume', deadline())).input.id, resumed.input.id);
  await assert.rejects(client.resumeGoal(interruptedOwner.id, { id: active.id, revision: exhausted.revision }, 'goal-exhausted', deadline()), error => error.kind === 'LIMIT');

  const requests = [];
  const finishedGoals = [];
  const server = http.createServer(async (request, response) => {
    let raw = ''; for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw); requests.push(body);
    const system = body.messages.find(item => item.role === 'system')?.content ?? '';
    const encoded = system.split('Captured goal for this turn (JSON data):\n')[1]?.split('\n')[0];
    const goal = encoded ? JSON.parse(encoded) : null;
    let message = { role: 'assistant', content: 'progress' };
    if (goal && goal.revision !== '1' && body.messages.at(-1).role !== 'tool') {
      const args = { goal_id: goal.id, expected_revision: goal.revision, evidence: 'fixture verified result' };
      const code = goal.spec.text.startsWith('starlark')
        ? `print(goals.complete(goal_id=${JSON.stringify(args.goal_id)},expected_revision=${JSON.stringify(args.expected_revision)},evidence=${JSON.stringify(args.evidence)}))`
        : `print(await goals.complete(${JSON.stringify(args)}));`;
      message = { role: 'assistant', content: null, tool_calls: [{ id: 'goal-call', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
    }
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 20, completion_tokens: 5, cost: 0 } }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const path = join(runtime.directory, 'state', 'host.json');
  await runtime.stop(); const previous = await readFile(path, 'utf8');
  try {
    const host = JSON.parse(previous);
    host.providers.goals = { kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '', models: { goals: { max_output_tokens: 500, timeout_millis: 10000, max_attempts: 1 } } };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 }); await runtime.start(null);
    for (const engine of ['starlark', 'quickjs']) {
      const { root } = await client.call('trees.create', { ...createParams, engine, overrides: { ...createParams.overrides, model: { provider: 'goals', name: 'goals', effort: '' } } }, deadline());
      const grant = await client.call('grants.create', { id: `goal-grant-${engine}`, session_id: root.id, capability: 'goals.complete', resource: root.tree_id }, deadline());
      const child = await client.spawn({ parent_id: root.id, parts: [{ type: 'text', text: 'initialize child' }], overrides: {}, grant_ids: [grant.id] }, `goal-child-${engine}`, deadline());
      await client.wait(`goal-child-${engine}`, deadline());
      for (const owner of [root, child.session]) {
        const goalID = `goal-${owner.id}`;
        await client.createGoal({ session_id: owner.id, expected_current: null, spec: { text: `${engine} complete with evidence`, max_continuations: '2' }, start: true }, goalID, deadline());
        const final = await until(() => client.getGoal(owner.id, goalID, deadline()), value => value.state === 'completed');
        assert.equal(final.continuations_used, '1'); assert.ok(final.completion_turn_id); assert.ok(final.completion_operation_id);
        const operations = await client.call('turns.operations', { turn_id: final.completion_turn_id, limit: 100 }, deadline());
        assert.equal(operations.items.length, 1); assert.equal(operations.items[0].state, 'succeeded');
        assert.equal(operations.items[0].result.value.accepted, true);
        const history = await client.call('sessions.history', { session_id: owner.id, after: '0', limit: 100 }, deadline());
        assert.equal(history.items.filter(item => item.role === 'user' && item.parts[0]?.text === 'Work on the goal.').length, 2);
        finishedGoals.push({ owner: owner.id, final, messages: history.items.length });
        evidence.push({ goalsEngine: { engine, owner: owner.id, final, operations } });
      }
    }
  } finally {
    await runtime.stop(); await writeFile(path, previous, { mode: 0o600 });
    server.closeAllConnections(); await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    if (!fixtureAbort.signal.aborted) await runtime.start();
  }
  for (const saved of finishedGoals) {
    assert.deepEqual(await client.getGoal(saved.owner, saved.final.id, deadline()), saved.final);
    assert.equal((await client.call('sessions.history', { session_id: saved.owner, after: '0', limit: 100 }, deadline())).items.length, saved.messages);
  }
  evidence.push({ goals: { defaults, old, recovered, completed, roundLimit, retired, interrupted, stopped, resumed, exhausted, requests: requests.length } });
}


async function goalFormulationAcceptance(runtime, client, createParams, evidence) {
  const requests = [];
  const formulations = [];
  let blocked = false;
  const server = http.createServer(async (request, response) => {
    let raw = ''; for await (const chunk of request) raw += chunk;
    const body = JSON.parse(raw);
    requests.push(body);
    const system = body.messages.find(item => item.role === 'system')?.content ?? '';
    const helper = system.startsWith('Formulate one clear, actionable goal');
    if (helper) {
      const source = JSON.parse(body.messages.filter(item => item.role === 'user').map(item => item.content).join(''));
      formulations.push({ body, source });
      if (JSON.stringify(source).includes('crash-formulation')) { blocked = true; return; }
    }
    const message = { role: 'assistant', content: helper ? 'Complete the recorded objective with verified evidence.' : 'Recorded source response.' };
    response.setHeader('content-type', 'application/json');
    response.end(JSON.stringify({ choices: [{ message, finish_reason: 'stop' }], usage: { prompt_tokens: 2, completion_tokens: 1, cost: helper ? 0.000000001 : 0 } }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const path = join(runtime.directory, 'state', 'host.json');
  await runtime.stop();
  const previous = await readFile(path, 'utf8');
  const history = owner => client.call('sessions.history', { session_id: owner, after: '0', limit: 100 }, deadline());
  const attempts = turn => client.call('turns.attempts', { turn_id: turn, limit: 100 }, deadline());
  const saved = [];
  try {
    const host = JSON.parse(previous);
    host.providers.formulation = {
      kind: 'openai-chat', base_url: `http://127.0.0.1:${server.address().port}/v1`, credential_env: '',
      models: Object.fromEntries(['starlark', 'quickjs'].map(engine => [engine, { max_output_tokens: 500, timeout_millis: 30000, max_attempts: 2 }])),
    };
    await writeFile(path, JSON.stringify(host), { mode: 0o600 }); await runtime.start(null);
    const create = engine => client.call('trees.create', {
      ...createParams, engine, overrides: { report_mode: 'message', model: { provider: 'formulation', name: engine, effort: 'low', temperature: 0, top_p: 0.5 } },
    }, deadline());
    for (const engine of ['starlark', 'quickjs']) {
      const { root } = await create(engine);
      const child = await client.spawn({ parent_id: root.id, overrides: {}, parts: [{ type: 'text', text: `${engine} child objective` }], grant_ids: [] }, `formulation-child-${engine}`, deadline());
      await client.wait(`formulation-child-${engine}`, deadline());
      await client.submit(root.id, [{ type: 'text', text: `${engine} root objective` }], `formulation-seed-${engine}`, deadline());
      await client.wait(`formulation-seed-${engine}`, deadline());
      for (const owner of [root, child.session]) {
        const before = await history(owner.id);
        const requestID = `formulation-${owner.id}`;
        const params = { session_id: owner.id, request: { goal_id: `goal-${owner.id}`, expected_current: null, max_continuations: '9007199254740993', start: false, tail_messages: 2 } };
        let admitted;
        if (owner.id === root.id) {
          const proxy = join(runtime.directory, 'formulation-drop.sock');
          const close = await dropAcknowledgement(proxy, runtime.info.socket, requestID, value => { admitted = value; });
          try {
            const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
            await assert.rejects(unreliable.formulateGoal(params, requestID, deadline()), DeliveryError);
          } finally { await close(); }
        } else admitted = await client.formulateGoal(params, requestID, deadline());
        assert.equal(admitted.input.kind, 'goal_formulation');
        assert.deepEqual(admitted.input.parts, []);
        const done = await client.wait(requestID, deadline());
        assert.equal(done.turn.state, 'succeeded');
        assert.equal(done.turn.kind, 'goal_formulation');
        assert.equal(done.turn.goal, null);
        const ledger = await attempts(done.turn.id);
        assert.equal(ledger.items.length, 1);
        const attempt = ledger.items[0];
        assert.equal(attempt.request.purpose, 'goal_formulation');
        assert.equal(attempt.message_id, null);
        assert.equal(attempt.cost_nano_usd, '1');
        const candidate = await client.getGoalFormulation(owner.id, attempt.id, deadline());
        assert.equal(candidate.accepted, true);
        assert.equal(candidate.rejection, null);
        assert.equal(candidate.after_sequence, '0');
        assert.equal(candidate.through_sequence, '2');
        assert.equal(candidate.request.start, false);
        assert.equal(candidate.request.max_continuations, '9007199254740993');
        const goal = (await client.currentGoal(owner.id, deadline())).goal;
        assert.equal(goal.origin_formulation_attempt_id, attempt.id);
        assert.equal(goal.spec.text, candidate.text);
        assert.equal(goal.spec.max_continuations, '9007199254740993');
        assert.deepEqual((await history(owner.id)).items, before.items);
        assert.deepEqual((await client.call('turns.operations', { turn_id: done.turn.id, limit: 100 }, deadline())).items, []);
        const retry = await client.formulateGoal(params, requestID, deadline());
        assert.equal(retry.input.id, admitted.input.id);
        await assert.rejects(client.formulateGoal({ ...params, request: { ...params.request, start: true } }, requestID, deadline()), error => error.kind === 'CONFLICT');
        await assert.rejects(client.getGoalFormulation(owner.id === root.id ? child.session.id : root.id, attempt.id, deadline()), error => error.kind === 'NOT_FOUND');
        await client.cancelGoal(owner.id, goal.id, deadline());
        assert.deepEqual(await client.getGoalFormulation(owner.id, attempt.id, deadline()), candidate);
        const rejectedID = `rejected-${owner.id}`;
        await client.formulateGoal({ session_id: owner.id, request: { goal_id: rejectedID, expected_current: null, start: false } }, rejectedID, deadline());
        const rejected = await client.wait(rejectedID, deadline());
        assert.equal(rejected.turn.state, 'failed');
        const rejectedAttempt = (await attempts(rejected.turn.id)).items[0];
        assert.equal(rejectedAttempt.state, 'succeeded');
        assert.equal(rejectedAttempt.cost_nano_usd, '1');
        const rejection = await client.getGoalFormulation(owner.id, rejectedAttempt.id, deadline());
        assert.equal(rejection.accepted, false); assert.ok(rejection.rejection); assert.ok(rejection.text);
        assert.equal((await client.currentGoal(owner.id, deadline())).goal.id, goal.id);
        assert.deepEqual((await history(owner.id)).items, before.items);
        if (owner.id !== root.id) {
          await client.call('sessions.delete', { session_id: owner.id }, deadline());
          assert.ok((await client.formulateGoal(params, requestID, deadline())).receipt.deleted_at);
        }
        saved.push({ owner: owner.id, attempt: attempt.id, candidate });
        evidence.push({ goalFormulation: { engine, done, ledger, candidate, rejection } });
      }
    }
    const { root: starting } = await create('starlark');
    await client.submit(starting.id, [{ type: 'text', text: 'Start a formulated objective.' }], 'formulation-start-seed', deadline());
    await client.wait('formulation-start-seed', deadline());
    await client.formulateGoal({ session_id: starting.id, request: { goal_id: 'formulated-start', expected_current: null, max_continuations: '0', start: true } }, 'formulation-start', deadline());
    const started = await client.wait('formulation-start', deadline());
    assert.equal(started.turn.state, 'succeeded');
    const paused = await until(() => client.getGoal(starting.id, 'formulated-start', deadline()), goal => goal.state === 'paused');
    const startedHistory = await history(starting.id);
    assert.equal(startedHistory.items.filter(item => item.role === 'user' && item.parts[0]?.text === 'Work on the goal.').length, 1);
    assert.equal(paused.continuations_used, '0');
    assert.ok(startedHistory.items.every(item => item.turn_id !== started.turn.id));

    const { root: crashing } = await create('quickjs');
    await client.submit(crashing.id, [{ type: 'text', text: 'crash-formulation objective' }], 'formulation-crash-seed', deadline());
    await client.wait('formulation-crash-seed', deadline());
    const crashParams = { session_id: crashing.id, request: { goal_id: 'formulated-crash', expected_current: null, start: true } };
    await client.formulateGoal(crashParams, 'formulation-crash', deadline());
    const running = await until(() => client.recover('formulation-crash', deadline()), value => value.turn?.state === 'running');
    await until(async () => blocked, Boolean);
    const beforeRestart = requests.length;
    await runtime.stop('SIGKILL'); await runtime.start(null);
    assert.equal((await client.wait('formulation-crash', deadline())).turn.state, 'interrupted');
    const uncertain = (await attempts(running.turn.id)).items[0];
    assert.equal(uncertain.state, 'uncertain'); assert.equal(uncertain.cost_nano_usd, null);
    assert.equal((await client.currentGoal(crashing.id, deadline())).goal, null);
    await assert.rejects(client.getGoalFormulation(crashing.id, uncertain.id, deadline()), error => error.kind === 'NOT_FOUND');
    assert.equal((await client.formulateGoal(crashParams, 'formulation-crash', deadline())).turn.id, running.turn.id);
    for (const value of saved) assert.deepEqual(await client.getGoalFormulation(value.owner, value.attempt, deadline()), value.candidate);
    assert.equal(requests.length, beforeRestart, 'restart or inspection replayed formulation');
    assert.equal(formulations.length, 10);
    for (const { body, source } of formulations) {
      assert.ok(!body.tools?.length);
      assert.equal(body.temperature, 0); assert.equal(body.top_p, 0.5); assert.equal(body.reasoning_effort, 'low');
      assert.deepEqual(source.map(item => item.Role), ['user', 'assistant']);
    }
    evidence.push({ goalFormulationRecovery: { started, paused, uncertain, calls: formulations.length } });
  } finally {
    await runtime.stop(); await writeFile(path, previous, { mode: 0o600 });
    server.closeAllConnections(); await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
    if (!fixtureAbort.signal.aborted) await runtime.start();
  }
}


async function contentOwnerAcceptance(runtime, client, createParams, evidence) {
  const referenceID = 'same-opaque-handle';
  const owners = [];
  for (const text of ['first owner: café', 'second owner: 界🙂']) {
    const { root } = await client.call('trees.create', createParams, deadline());
    const params = { session_id: root.id, reference_id: referenceID, media_type: 'text/plain', data_base64: Buffer.from(text).toString('base64') };
    const reference = await client.call('content.put', params, deadline());
    assert.deepEqual(await client.call('content.put', params, deadline()), reference);
    assert.equal(reference.id, referenceID);
    const read = await client.call('content.read', { session_id: root.id, reference_id: referenceID }, deadline());
    assert.equal(Buffer.from(read.data_base64, 'base64').toString('utf8'), text);
    await assert.rejects(client.call('content.put', { ...params, data_base64: Buffer.from('conflicting bytes').toString('base64') }, deadline()), error => error.kind === 'CONFLICT');
    owners.push({ root, params, reference, text });
  }
  const { root: unrelated } = await client.call('trees.create', createParams, deadline());
  await assert.rejects(client.call('content.read', { session_id: unrelated.id, reference_id: referenceID }, deadline()), error => error.kind === 'NOT_FOUND');
  await runtime.stop(); await runtime.start();
  for (const value of owners) assert.deepEqual(await client.call('content.put', value.params, deadline()), value.reference);
  await client.call('sessions.delete', { session_id: owners[0].root.id }, deadline());
  await assert.rejects(client.call('content.read', { session_id: owners[0].root.id, reference_id: referenceID }, deadline()), error => error.kind === 'NOT_FOUND');
  const remaining = await client.call('content.read', { session_id: owners[1].root.id, reference_id: referenceID }, deadline());
  assert.equal(Buffer.from(remaining.data_base64, 'base64').toString('utf8'), owners[1].text);
  evidence.push({ contentOwners: { referenceID, owners: owners.map(value => value.root.id), remaining } });
}


async function accountAcceptance(runtime, client, evidence) {
  const path = join(runtime.directory, 'state', 'host.json');
  const credentialsPath = join(runtime.directory, 'state', 'openai-codex.json');
  const backupPath = join(runtime.directory, 'state', 'fixture-account-backup.json');
  const previous = await readFile(path, 'utf8');
  const oldID = 'A'.repeat(26) + ':' + 'B'.repeat(26);
  const signedOut = await client.openAIAccountStatus(deadline());
  assert.equal(signedOut.auth_state, 'signed_out');
  assert.equal(signedOut.account_id, null); assert.equal(signedOut.expires_at, null);
  assert.deepEqual((await client.listOpenAILogins(deadline())).items, []);
  assert.deepEqual(await client.getOpenAILogin(oldID, deadline()), {
    id: oldID, state: 'interrupted', verification_url: null, user_code: null, expires_at: null, failure: null,
  });
  await assert.rejects(client.cancelOpenAILogin(oldID, deadline()), error => error.kind === 'NOT_FOUND');
  await assert.rejects(client.setupOpenAIAccount(deadline()), error => error.kind === 'ACCOUNT_CREDENTIALS');
  const credentials = {
    accessToken: 'fixture-private-access', refreshToken: 'fixture-private-refresh',
    accountId: 'fixture-account', email: 'fixture@example.test', plan: 'fixture-plan',
    expiresAt: '2001-02-03T04:05:06.123456789Z',
  };
  try {
    // Synthetic expired credentials exercise local evidence without contacting
    // the provider. Device approval itself is covered by intercepted Go tests.
    await runtime.stop();
    await writeFile(credentialsPath, JSON.stringify(credentials), { mode: 0o600 });
    await runtime.start();
    const stored = await client.openAIAccountStatus(deadline());
    assert.equal(stored.auth_state, 'stored');
    assert.equal(stored.account_id, credentials.accountId);
    assert.equal(stored.email, credentials.email); assert.equal(stored.plan, credentials.plan);
    assert.equal(stored.expires_at, credentials.expiresAt);
    for (const secret of [credentials.accessToken, credentials.refreshToken]) assert.ok(!JSON.stringify(stored).includes(secret));
    const configured = await client.setupOpenAIAccount(deadline());
    assert.equal(configured.auth_state, 'stored'); assert.equal(configured.route_state, 'configured');
    assert.deepEqual(JSON.parse(await readFile(credentialsPath, 'utf8')), credentials);
    const setupHost = await readFile(path, 'utf8');
    assert.deepEqual(JSON.parse(setupHost).defaults, JSON.parse(previous).defaults);
    assert.deepEqual(await client.setupOpenAIAccount(deadline()), configured);
    assert.equal(await readFile(path, 'utf8'), setupHost, 'idempotent setup rewrote the host declaration');
    assert.deepEqual((await client.listOpenAILogins(deadline())).items, []);

    // A conflicting custom route refuses login before any device request and
    // preserves both the user's declaration and saved account.
    const conflict = JSON.parse(setupHost);
    conflict.providers['openai-codex'] = { kind: 'openai-chat', base_url: 'http://127.0.0.1:1/v1', credential_env: '', models: {} };
    await writeFile(path, JSON.stringify(conflict), { mode: 0o600 });
    assert.equal((await client.openAIAccountStatus(deadline())).route_state, 'conflict');
    await assert.rejects(client.beginOpenAILogin(deadline()), error => error.kind === 'ACCOUNT_CONFIGURATION');
    await assert.rejects(client.setupOpenAIAccount(deadline()), error => error.kind === 'ACCOUNT_SETUP');
    assert.deepEqual((await client.listOpenAILogins(deadline())).items, []);
    assert.deepEqual(JSON.parse(await readFile(path, 'utf8')), conflict);
    await writeFile(path, setupHost, { mode: 0o600 });

    // Force a real removal failure after the manager has loaded credentials.
    // Local authorization is revoked immediately, but status must keep the
    // unresolved storage problem visible until explicit logout retry.
    await rename(credentialsPath, backupPath);
    await mkdir(credentialsPath, { mode: 0o700 });
    await writeFile(join(credentialsPath, 'blocker'), 'fixture');
    await assert.rejects(client.logoutOpenAIAccount(deadline()), error => error.kind === 'ACCOUNT_LOGOUT');
    const unavailable = await client.openAIAccountStatus(deadline());
    assert.equal(unavailable.auth_state, 'unavailable'); assert.ok(unavailable.failure);
    assert.equal(unavailable.account_id, null);
    await assert.rejects(client.setupOpenAIAccount(deadline()), error => error.kind === 'ACCOUNT_CREDENTIALS');
    await rm(credentialsPath, { recursive: true });
    await rename(backupPath, credentialsPath);
    assert.equal((await client.openAIAccountStatus(deadline())).auth_state, 'unavailable');

    const proxy = join(runtime.directory, 'account-logout-drop.sock');
    let dropped;
    const close = await dropAcknowledgement(proxy, runtime.info.socket, '', value => { dropped = value; }, response => response.result?.auth_state === 'signed_out');
    try {
      const unreliable = await Client.connect(unixSocket(proxy), { clientID: client.clientID, expectedRuntimeID: client.runtimeID, ...deadline() });
      await assert.rejects(unreliable.logoutOpenAIAccount(deadline()), DeliveryError);
    } finally { await close(); }
    assert.equal(dropped.auth_state, 'signed_out'); assert.equal(dropped.route_state, 'configured');
    assert.deepEqual(await client.openAIAccountStatus(deadline()), dropped);
    assert.deepEqual(await client.logoutOpenAIAccount(deadline()), dropped);
    await assert.rejects(readFile(credentialsPath), error => error.code === 'ENOENT');
    assert.equal(await readFile(path, 'utf8'), setupHost, 'logout altered configured routes or defaults');
    await runtime.stop(); await runtime.start();
    assert.deepEqual(await client.openAIAccountStatus(deadline()), dropped);
    assert.equal((await client.getOpenAILogin(oldID, deadline())).state, 'interrupted');
    evidence.push({ hostAccounts: { signedOut, stored, configured, unavailable, loggedOut: dropped } });
  } finally {
    await runtime.stop();
    await writeFile(path, previous, { mode: 0o600 });
    await rm(credentialsPath, { recursive: true, force: true });
    await rm(backupPath, { force: true });
    if (!fixtureAbort.signal.aborted) await runtime.start();
  }
}
