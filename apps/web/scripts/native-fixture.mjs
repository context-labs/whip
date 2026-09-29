import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { appendFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { Client } from '../../../packages/sdk/dist/index.js';
import { defineAgent, tool } from '../../../packages/sdk/dist/agents.js';
import { browserSocket, discoverGateway } from '../../../packages/sdk/dist/browser.js';
import { executorSocket, unixSocket } from '../../../packages/sdk/dist/node.js';

export const repository = fileURLToPath(new URL('../../../', import.meta.url));
export const deadline = () => ({ signal: AbortSignal.timeout(15_000) });
export async function eventually(check, { timeout = 15_000, interval = 25, description = 'condition' } = {}) {
  const end = Date.now() + timeout; let last;
  while (Date.now() < end) {
    try { const value = await check(); if (value) return value; } catch (error) { last = error; }
    await new Promise(resolve => setTimeout(resolve, interval));
  }
  throw new Error(`Timed out waiting for ${description}`, { cause: last });
}

export function fixtureExternalOrigin(value) {
  if (value === undefined) return undefined;
  try {
    if (typeof value !== 'string' || value.length > 2048) throw new TypeError();
    const parsed = new URL(value);
    if (parsed.protocol !== 'https:' || value !== parsed.origin || parsed.hostname.includes('*')) throw new TypeError();
    return value;
  } catch { throw new TypeError('externalOrigin must be an exact HTTPS origin without credentials, path, query, fragment or wildcards'); }
}

/** Owns the real production runtime, engines and gateway, a local fake HTTP
 * provider and one explicit fixture executor lease. No legacy runtime or DTOs. */
export async function startFixture({ allowedOrigins = [], retainOnFailure = false, lifetimeMs = 240_000, externalOrigin, executeCode = false, agentResponses = false, rejectInput } = {}) {
  if (rejectInput !== undefined && (typeof rejectInput !== 'string' || rejectInput.length < 1 || rejectInput.length > 256)) throw new RangeError('Rejected fixture input must contain 1..256 characters');
  if (!Number.isInteger(lifetimeMs) || lifetimeMs < 1 || lifetimeMs > 1_800_000) throw new RangeError('Fixture lifetime must be within 1..1800000ms');
  const origin = fixtureExternalOrigin(externalOrigin);
  allowedOrigins = [...new Set([...allowedOrigins, ...(origin ? [origin] : [])])];
  const directory = await mkdtemp('/tmp/whip-web-native-'), state = join(directory, 'state');
  const binary = join(directory, 'runtime'), fixtureHome = join(directory, 'home'), helperDirectory = join(directory, 'bin');
  // The runtime must not discover user credentials, MCP sources or shell startup
  // files. Build tools keep their normal environment; the owned child does not.
  const runtimeEnvironment = { PATH: helperDirectory + ':/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/sh',
    HOME: fixtureHome, ZDOTDIR: fixtureHome, XDG_CONFIG_HOME: join(fixtureHome, '.config'),
    TMPDIR: join(directory, 'tmp'), WHIPCODE_HOME: join(fixtureHome, '.whipcode') };
  const lifetime = new AbortController(), holds = new Map();
  let runtime, exited, executor, local, info, output = '', closed = false, gatewayAddress = '127.0.0.1:0';
  let effectBytes = 0;
  const record = chunk => { output = (output + chunk.toString()).slice(-(1 << 20)); };
  const hold = key => {
    if (typeof key !== 'string' || key.length > 256) throw new Error('Invalid fixture hold identity');
    let entry = holds.get(key);
    if (!entry) {
      if (holds.size >= 128) throw new Error('Fixture hold capacity exceeded');
      let release; const promise = new Promise(resolve => { release = resolve; }); entry = { promise, release }; holds.set(key, entry);
    }
    return entry;
  };
  const wait = async (key, signal) => {
    signal.throwIfAborted(); const entry = hold(key);
    let aborted; const stopped = new Promise((_, reject) => { aborted = () => reject(signal.reason); signal.addEventListener('abort', aborted, { once: true }); });
    try { await Promise.race([entry.promise, stopped]); signal.throwIfAborted(); } finally { signal.removeEventListener('abort', aborted); }
  };
  const provider = createServer(async (request, response) => {
    const aborted = new AbortController(); response.once('close', () => aborted.abort(new Error('Provider connection closed')));
    const signal = AbortSignal.any([lifetime.signal, aborted.signal]);
    try {
      if (request.url === '/v1/models') {
        response.setHeader('Content-Type', 'application/json');
        response.end(JSON.stringify({ data: [{ id: 'model', supports_tools: true }, { id: 'replacement', reasoning_efforts: ['low', 'medium', 'high'], supports_tools: true }] })); return;
      }
      if (request.url !== '/v1/chat/completions' || request.method !== 'POST') { response.writeHead(404).end(); return; }
      let bytes = 0; const chunks = [];
      for await (const chunk of request) { bytes += chunk.length; if (bytes > (8 << 20)) throw new Error('Fixture provider input exceeds 8MiB'); chunks.push(chunk); }
      const body = JSON.parse(Buffer.concat(chunks).toString()), messages = body.messages;
      const last = messages.at(-1), authored = messages.findLast(item => item.role === 'user');
      const text = typeof authored?.content === 'string' ? authored.content : (authored?.content ?? []).filter(part => part.type === 'text').map(part => part.text).join('\n');
      if (last.role !== 'tool') {
        const effect = JSON.stringify(text) + '\n'; effectBytes += Buffer.byteLength(effect);
        if (effectBytes > (16 << 20)) throw new Error('Fixture effect evidence exceeds 16MiB');
        await appendFile(join(directory, 'effects.jsonl'), effect, { mode: 0o600 });
      }
      if (rejectInput !== undefined && text === rejectInput) { response.writeHead(400, { 'Content-Type': 'application/json' }).end(JSON.stringify({ error: { message: 'Explicit fixture provider rejection' } })); return; }
      const stream = !!body.stream;
      response.setHeader('Content-Type', stream ? 'text/event-stream' : 'application/json');
      const delta = value => response.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta: value, finish_reason: null }] })}\n\n`);
      // Opt-in agent acceptance: explicit final text follows actual execution.
      const final = agentResponses ? text.match(/```final\n([\s\S]*?)\n```/)?.[1] : undefined;
      if (final !== undefined && Buffer.byteLength(final) > 65536) throw new RangeError('Fixture final response exceeds 64KiB');
      if (agentResponses && last.role === 'tool' && text.startsWith('hold:agent-')) await wait(text.slice(5).split('\n')[0], signal);
      let message;
      if (agentResponses && last.role === 'tool') message = { role: 'assistant', content: final ?? `done: ${typeof last.content === 'string' ? last.content : JSON.stringify(last.content)}` };
      else if (last.role === 'tool') message = { role: 'assistant', content: text.startsWith('hold:tool-stream') ? 'Completed held tools.' : text };
      else if (executeCode && /```(?:starlark|python|javascript|js)\n([\s\S]*?)\n```/.test(text)) {
        const code = text.match(/```(?:starlark|python|javascript|js)\n([\s\S]*?)\n```/)[1];
        message = { role: 'assistant', content: null, tool_calls: [{ id: randomUUID(), type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
      } else if (final !== undefined) message = { role: 'assistant', content: final };
      else if (text === 'question:single' || text === 'question:batch') {
        const questions = '[{"question":"Continue with the fixture?","options":[{"label":"Proceed","description":"Continue this isolated test.","recommended":True},{"label":"Wait","description":"Choose a different fixture answer."}]}' + (text === 'question:batch' ? ',{"question":"Which surfaces should be checked? Add custom text if needed.","multiple":True,"options":[{"label":"Web"},{"label":"Mobile"}]},{"question":"Optional note: skip this page to test dismissal.","options":[{"label":"No note"},{"label":"Add a note"}]}' : '') + ']';
        const code = `print(user.ask(questions=${questions}))`;
        message = { role: 'assistant', content: null, tool_calls: [{ id: randomUUID(), type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
      } else if (text.startsWith('permission:')) {
        const code = `files.write(path=${JSON.stringify('permission-' + randomUUID() + '.txt')},content=${JSON.stringify(text)})`;
        message = { role: 'assistant', content: null, tool_calls: [{ id: randomUUID(), type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
      } else if (text.startsWith('hold:tool-stream')) {
        const refresh = text.startsWith('hold:tool-stream-refresh-');
        const codes = [refresh ? 'print("completed before snapshot")' : 'print("first"); print("second")', `print("first"); print("second"); tools.fixture_wait(key=${JSON.stringify(text.slice(5))})`];
        message = { role: 'assistant', content: text, tool_calls: codes.map(code => ({ id: randomUUID(), type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } })) };
      } else {
        if (text === 'hold:thinking-response') await wait('thinking-first-token', signal);
        if (stream) { delta({ role: 'assistant', content: text.slice(0, Math.ceil(text.length / 2)) }); delta({ content: text.slice(Math.ceil(text.length / 2)) }); }
        if (text.startsWith('hold:')) await wait(text.slice(5), signal);
        message = { role: 'assistant', content: text };
        if (stream) message = { role: 'assistant', content: null };
      }
      if (stream) {
        if (message.content) { delta({ role: 'assistant', content: message.content.slice(0, Math.ceil(message.content.length / 2)) }); delta({ content: message.content.slice(Math.ceil(message.content.length / 2)) }); }
        if (message.tool_calls) {
          // Native provider fragments interleave two real execute calls. Their
          // cell identities and cumulative stdout come from the actual engine.
          for (let part = 0; part < 2; part++) for (const [index, call] of message.tool_calls.entries()) {
            const middle = Math.ceil(call.function.arguments.length / 2);
            delta({ tool_calls: [{ index, ...(part === 0 ? { id: call.id, type: 'function' } : {}), function: { ...(part === 0 ? { name: call.function.name } : {}), arguments: part === 0 ? call.function.arguments.slice(0, middle) : call.function.arguments.slice(middle) } }] });
          }
        }
        response.write(`data: ${JSON.stringify({ choices: [{ index: 0, delta: {}, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 5, completion_tokens: 5, cost: 0 } })}\n\n`);
        response.end('data: [DONE]\n\n');
      } else response.end(JSON.stringify({ choices: [{ message, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 5, completion_tokens: 5, cost: 0 } }));
    } catch (error) { record(error.stack ?? String(error)); if (!response.headersSent) response.writeHead(500); response.end(); }
  });
  const agent = defineAgent({ id: 'web-fixture', name: 'Native browser fixture', defaults: { automatic_title: false }, tools: [tool({ name: 'fixture_wait', description: 'Wait for explicit test release', timeoutMs: 180_000,
    input: { type: 'object', properties: { key: { type: 'string', maxLength: 256 } }, required: ['key'], additionalProperties: false },
    execute: async ({ key }, context) => { await wait(key, context.signal); return null; },
  })] });
  async function stop(child, signal = 'SIGTERM') {
    if (!child || child.exitCode !== null || child.signalCode !== null) return;
    const ended = once(child, 'exit'); child.kill(signal); const timer = setTimeout(() => child.kill('SIGKILL'), 5000);
    try { await ended; } finally { clearTimeout(timer); }
  }
  async function startProcess(executable, args) {
    lifetime.signal.throwIfAborted(); const child = spawn(executable, args, { cwd: directory, stdio: ['ignore', 'pipe', 'pipe'], env: runtimeEnvironment });
    child.stdout.on('data', record); child.stderr.on('data', record);
    try {
      const ready = await new Promise((resolve, reject) => {
        let text = ''; const clean = () => { clearTimeout(timer); child.stdout.off('data', data); child.off('error', fail); child.off('exit', exit); };
        const fail = error => { clean(); reject(error); }, exit = (code, signal) => fail(new Error(`Fixture exited ${code}/${signal}: ${output}`));
        const data = chunk => { text += chunk; if (text.length > 65536) { fail(new Error('Oversized fixture readiness')); return; } const end = text.indexOf('\n'); if (end < 0) return; try { const value = JSON.parse(text.slice(0, end)); clean(); resolve(value); } catch (error) { fail(error); } };
        const timer = setTimeout(() => fail(new Error('Fixture readiness timed out: ' + output)), 15000);
        child.stdout.on('data', data); child.once('error', fail); child.once('exit', exit);
      });
      return { child, ready };
    } catch (error) { await stop(child); throw error; }
  }
  async function start() {
    const started = await startProcess(binary, ['-directory', state, '-workers', '4', '-web', '-web-listen', gatewayAddress, '-web-origins', allowedOrigins.join(','), ...(origin ? ['-web-hosts', gatewayAddress + ',' + new URL(origin).host] : [])]); runtime = started.child;
    exited = new Promise(resolve => runtime.once('exit', (code, signal) => resolve([code, signal])));
    info = { ...started.ready, generation: (info?.generation ?? 0) + 1 };
    local = await Client.connect(unixSocket(info.socket), { clientID: 'native-web-setup', expectedRuntimeID: info.runtime_id, ...deadline() });
    if (info.generation === 1) {
      const inventory = await local.listProviders(deadline());
      const declaration = { kind: 'openai-chat', base_url: `http://127.0.0.1:${provider.address().port}/v1`, credential: { source: 'none', environment: '', file: '', command: null }, models: {} };
      for (const name of ['model', 'replacement']) declaration.models[name] = { prices: { input: '0', output: '0', reasoning: null, cached_input: null, cached_output: null }, context_window_tokens: null, max_output_tokens: '4096', timeout_millis: '180000', max_attempts: 1 };
      await local.createProvider({ revision: inventory.revision, provider: 'provider', keep_credential: false, key: null, declaration }, deadline());
      const updated = await local.listProviders(deadline());
      await local.setProviderDefaults({ revision: updated.revision, defaults: { selection: { provider: 'provider', name: 'model', effort: '', temperature: null, top_p: null }, settings: null } }, deadline());
    }
    await local.refreshProviderCatalog('provider', deadline());
    executor = await local.agents.serve(agent, { transport: await executorSocket(info.socket), maxConcurrent: 16, maxInvocations: 4096 });
    void executor.done.catch(error => { if (!closed && runtime?.exitCode === null) record(error.stack ?? String(error)); });
    gatewayAddress = new URL(info.web).host;
    const discovery = await discoverGateway(info.web, { expectedRuntimeID: info.runtime_id, ...deadline() });
    assert.equal(discovery.available, true); assert.equal(discovery.process_epoch, info.process_epoch);
  }
  const timer = setTimeout(() => { lifetime.abort(new Error('Fixture lifetime ended')); runtime?.kill('SIGKILL'); provider.closeAllConnections(); provider.close(); }, lifetimeMs); timer.unref();
  async function close() {
    if (closed) return; closed = true; clearTimeout(timer); lifetime.abort(new Error('Fixture closed'));
    await executor?.close().catch(record); await stop(runtime);
    provider.closeAllConnections(); await new Promise(resolve => provider.close(resolve));
    if (retainOnFailure || process.env.WHIP_WEB_KEEP_FIXTURE) { await writeFile(join(directory, 'fixture.log'), output); console.log(`Native fixture retained: ${directory}`); }
    else await rm(directory, { recursive: true, force: true });
  }
  try {
    await mkdir(fixtureHome); await mkdir(runtimeEnvironment.TMPDIR); await mkdir(helperDirectory);
    // Never open a real OS chooser during browser checks. The production app
    // falls back to its real bounded host-directory browser on unavailable.
    for (const name of ['osascript', 'zenity', 'kdialog', 'powershell']) await writeFile(join(helperDirectory, name), '#!/bin/sh\nexit 2\n', { mode: 0o700 });
    await writeFile(join(directory, 'effects.jsonl'), '', { mode: 0o600 });
    provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
    await promisify(execFile)('go', ['build', '-race=false', '-o', binary, './cmd/whip-runtime'], { cwd: repository, timeout: 120000, signal: lifetime.signal, env: { ...process.env, GOTOOLCHAIN: 'go1.27.0' } });
    if (origin) {
      // Select a loopback port before configuring exact Host authorities. A lost
      // reservation fails startup; never broaden the production gateway policy.
      const reservation = createServer(); reservation.listen(0, '127.0.0.1'); await once(reservation, 'listening');
      gatewayAddress = `127.0.0.1:${reservation.address().port}`;
      await new Promise(resolve => reservation.close(resolve));
    }
    await start();
  } catch (error) { await close(); throw new Error(`Native fixture could not start: ${output}`, { cause: error }); }
  return {
    directory, get info() { return info; }, get exited() { return exited; }, get output() { return output; }, close,
    connect: (clientID = randomUUID()) => Client.connect(browserSocket(info.web, { expectedRuntimeID: info.runtime_id, expectedProcessEpoch: info.process_epoch }), { clientID, expectedRuntimeID: info.runtime_id, ...deadline() }),
    async createRoot(client, { title = null, engine = 'starlark', overrides = {} } = {}) {
      const result = await client.createTree({ engine, definition: executor.definition, working_directory: directory, metadata: { title, pinned: false, archived: false }, overrides: { automatic_title: false, model: { provider: 'provider', name: 'model', effort: '' }, ...overrides } }, randomUUID(), deadline());
      await client.call('grants.create', { id: randomUUID(), session_id: result.root.id, capability: 'tools.fixture_wait', resource: executor.definition.id + '@' + executor.definition.revision }, deadline());
      return result;
    },
    release: key => hold(key).release(),
    effects: async () => (await readFile(join(directory, 'effects.jsonl'), 'utf8')).trim().split('\n').filter(Boolean).map(line => JSON.parse(line)),
    async crashAndRestart({ beforeRestart } = {}) {
      await stop(runtime, 'SIGKILL'); await executor?.close().catch(record);
      await beforeRestart?.(); await start(); return info;
    },
  };
}
