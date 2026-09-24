// Canonical signed runtime acceptance; this models client detach and source-bundle
// removal, not a Finder launch or a notarized Squirrel N -> N+1 installation.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { once } from 'node:events';
import { copyFile, lstat, mkdir, mkdtemp, readFile, realpath, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import path from 'node:path';
import { parseArgs, promisify } from 'node:util';
import asar from '@electron/asar';
import { createWhipClient } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
import { fileDigest, LocalRuntime, readRuntimeManifest } from '../src/runtime.ts';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

const { values, positionals } = parseArgs({ allowPositionals: true, options: {
  manifest: { type: 'string' }, output: { type: 'string' },
  'keep-fixture': { type: 'boolean' },
} });
assert(positionals.length <= 1, 'Usage: node continuity.mjs [Whip.app | native-directory] [--manifest file] [--output file]');
assert.equal(process.platform, 'darwin'); assert.equal(process.arch, 'arm64');
const source = await realpath(positionals[0] ?? path.join(repositoryRoot, 'apps/desktop/out/Whip-darwin-arm64/Whip.app'));
const output = path.resolve(values.output ?? path.join(repositoryRoot, '.ai-docs/plans/desktop-app/evidence/runtime-continuity.json'));
const fixture = await realpath(await mkdtemp('/tmp/whip-continuity-'));
const copiedSource = path.join(fixture, 'application-source');
const home = path.join(fixture, 'home');
const deadline = AbortSignal.timeout(90_000);
const clients = new Set();
const exec = promisify(execFile);
const run = async (binary, args, env, timeout = 10_000) => (await exec(binary, args, {
  env, timeout, maxBuffer: 256 << 10, encoding: 'utf8',
})).stdout;
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function eventually(check, description, timeout = 15_000) {
  const end = Date.now() + timeout;
  do {
    deadline.throwIfAborted();
    const result = await check(); if (result) return result;
    await delay(30);
  } while (Date.now() < end);
  throw new Error(`Timed out: ${description}`);
}

const token = crypto.randomUUID();
const phases = new Map(['before', 'held', 'after'].map(name => [name, { marker: `continuity-${name}-${token}`, requests: 0 }]));
let releaseHeld;
const held = new Promise(resolve => { releaseHeld = resolve; });
let providerFailure;
let providerRequests = 0;
const provider = createServer((request, response) => {
  void (async () => {
    assert(++providerRequests <= 12, 'Unexpected provider request loop');
    assert.equal(request.method, 'POST'); assert.equal(request.url, '/chat/completions');
    assert.equal(request.headers.authorization, 'Bearer fixture-only');
    let bytes = 0; const chunks = [];
    for await (const chunk of request) { bytes += chunk.length; assert(bytes <= 1 << 20); chunks.push(chunk); }
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    assert.equal(body.model, 'continuity-model'); assert.equal(body.stream, true);
    assert.deepEqual(body.tools.map(tool => tool.function.name), ['rlm_exec']);
    const phase = [...phases.values()].find(value => body.messages.some(message => message.role === 'user' && JSON.stringify(message.content).includes(value.marker)));
    assert(phase, 'Provider request did not originate from this fixture');
    assert(++phase.requests <= 2, 'A turn should require exactly one tool call and one final answer');
    let delta;
    if (phase.requests === 1) {
      phase.code = `print(${JSON.stringify(phase.marker)})\n{"marker": ${JSON.stringify(phase.marker)}, "answer": 6 * 7}`;
      delta = { tool_calls: [{ index: 0, id: phase.marker, type: 'function', function: {
        name: 'rlm_exec', arguments: JSON.stringify({ code: phase.code }),
      } }] };
    } else {
      const tool = body.messages.find(message => message.role === 'tool' && message.tool_call_id === phase.marker);
      assert(tool, 'The provider must receive an actual worker tool result');
      phase.toolResult = JSON.parse(tool.content);
      assert.equal(phase.toolResult.output, phase.marker + '\n');
      assert.deepEqual(phase.toolResult.value, { marker: phase.marker, answer: 42 });
      assert(phase.toolResult.steps > 0, 'Worker must report executed Starlark steps');
      if (phase === phases.get('held')) await held;
      phase.answer = `Verified ${phase.marker}: 42`;
      delta = { content: phase.answer };
    }
    response.writeHead(200, { 'Content-Type': 'text/event-stream' });
    response.end(`data: ${JSON.stringify({ choices: [{ delta, finish_reason: delta.tool_calls ? 'tool_calls' : 'stop' }] })}\n\ndata: [DONE]\n\n`);
  })().catch(error => { providerFailure ??= error; response.writeHead(500); response.end('Fixture provider failed'); });
});

let installed, env, daemonPID, result, cleanupError;
let startAttempted = false;
try {
  await Promise.all([copiedSource, home, path.join(fixture, 'user'), path.join(fixture, 'tmp'), path.join(fixture, 'work')]
    .map(directory => mkdir(directory, { mode: 0o700 })));
  const bundle = source.endsWith('.app');
  const sourceNative = bundle ? path.join(source, 'Contents/Helpers') : source;
  const manifestBytes = values.manifest ? await readFile(values.manifest)
    : bundle ? asar.extractFile(path.join(source, 'Contents/Resources/app.asar'), 'runtime-manifest.json')
      : await readFile(path.join(source, '../app/runtime-manifest.json'));
  assert(manifestBytes.length <= 16 << 10, 'Runtime manifest exceeds its size bound');
  await writeFile(path.join(copiedSource, 'runtime-manifest.json'), manifestBytes, { mode: 0o600 });
  const manifest = await readRuntimeManifest(path.join(copiedSource, 'runtime-manifest.json'));
  assert.equal(manifest.schema, 1); assert.equal(manifest.architecture, 'arm64');
  assert.match(manifest.teamId ?? '', /^[A-Z0-9]{10}$/, 'Acceptance requires a Developer ID signed runtime');
  assert.deepEqual(Object.keys(manifest.files).sort(), ['whip-computer', 'whipcode']);
  for (const name of ['whip-computer', 'whipcode']) {
    assert((await lstat(path.join(sourceNative, name))).isFile());
    await copyFile(path.join(sourceNative, name), path.join(copiedSource, name));
  }
  installed = { executable: path.join(fixture, 'bin/whipcode') };
  env = { PATH: '/usr/bin:/bin:/usr/sbin:/sbin', HOME: path.join(fixture, 'user'), TMPDIR: path.join(fixture, 'tmp'),
    WHIPCODE_HOME: home, WHIPCODE_NETWORK: '0' };
  await new LocalRuntime({ source: copiedSource, manifest, env, settingsFile: path.join(fixture, 'native-local-runtime.json'),
    defaultExecutable: installed.executable }).install(installed.executable, deadline);
  const helperBytes = await readFile(path.join(copiedSource, 'whip-computer'));
  assert.equal(manifest.distribution, 'whipcode');
  const metadata = JSON.parse(await run(installed.executable, ['_desktop-runtime-info'], env));
  assert.equal(metadata.buildId, manifest.buildId); assert.equal(metadata.protocolMajor, manifest.compatibility.protocolMajor);
  assert.equal(metadata.schemaVersion, manifest.compatibility.schemaVersion);
  assert.equal(metadata.distribution, 'whipcode');
  assert.equal(metadata.protocolMinor, manifest.compatibility.protocolMinor);
  provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
  const baseURL = `http://127.0.0.1:${provider.address().port}`;
  const model = { providers: ['continuity-provider'], context: 65536, maxOut: 256 };
  await writeFile(path.join(home, 'config.json'), JSON.stringify({
    defaultModel: 'continuity-model', defaultProvider: 'continuity-provider', compactModel: 'continuity-model', compactProvider: 'continuity-provider',
    providers: { 'continuity-provider': { baseUrl: baseURL, api: 'openai-completions', apiKey: 'fixture-only' } },
    models: { 'continuity-model': model }, rlm: { maxWorkers: 4 },
  }), { mode: 0o600 });
  await writeFile(path.join(home, 'models.json'), JSON.stringify({ 'continuity-provider': {
    fetchedAt: new Date().toISOString(), baseUrl: baseURL,
    models: [{ id: 'continuity-model', contextLength: 65536, maxCompletionTokens: 256,
      pricing: { prompt: '0', completion: '0', inputCacheRead: '0' } }],
  } }), { mode: 0o600 });
  assert.equal(JSON.parse(await run(installed.executable, ['daemon', 'status', '--json'], env)).state, 'stopped');
  startAttempted = true;
  await run(installed.executable, ['daemon', 'start'], env, 20_000);
  const status = async () => JSON.parse(await run(installed.executable, ['daemon', 'status', '--json'], env));
  const initial = await status();
  assert.equal(initial.state, 'running'); assert(!initial.network_endpoint); daemonPID = initial.pid;
  assert(initial.socket.startsWith(home + path.sep), 'Daemon socket must belong to this isolated home');
  const clientId = crypto.randomUUID();
  async function connect() {
    const client = createWhipClient({ endpoint: unixSocket(initial.socket), clientId, clientKind: 'automation' });
    clients.add(client); await client.connect({ signal: deadline }); return client;
  }
  async function processEvidence(pid) {
    const command = (await run('/bin/ps', ['-p', String(pid), '-o', 'command='], env)).trim();
    const files = await run('/usr/sbin/lsof', ['-a', '-p', String(pid), '-d', 'txt', '-Fn'], env);
    const executableMapped = files.split('\n').includes('n' + installed.executable);
    assert(command.startsWith(installed.executable + ' '), `Process ${pid} did not launch from canonical runtime`);
    assert(executableMapped, `Process ${pid} does not map the canonical executable`);
    return { pid, command, executableMapped };
  }
  async function workers() {
    let pids;
    try { pids = (await run('/usr/bin/pgrep', ['-P', String(daemonPID)], env)).trim().split(/\s+/).map(Number); }
    catch (error) { if (error.code === 1) return []; throw error; }
    const found = [];
    for (const pid of pids) {
      const command = (await run('/bin/ps', ['-p', String(pid), '-o', 'command='], env)).trim();
      if (command.startsWith(installed.executable + ' _kernel ')) found.push(await processEvidence(pid));
    }
    return found;
  }
  async function start(client, name) {
    const created = await client.sessions.create({ cwd: path.join(fixture, 'work'), model: 'continuity-model', provider: 'continuity-provider' }).result({ signal: deadline });
    assert.equal(created.status, 'succeeded', JSON.stringify(created));
    const rootId = created.result.root_id;
    const command = client.session(rootId).submit({ text: phases.get(name).marker });
    const accepted = await command.accepted({ signal: deadline });
    assert(!['failed', 'cancelled', 'interrupted'].includes(accepted.status));
    return { rootId, command };
  }
  async function finish(client, name, turn) {
    const outcome = await turn.command.result({ signal: deadline });
    if (providerFailure) throw providerFailure;
    assert.equal(outcome.status, 'succeeded', JSON.stringify(outcome));
    const phase = phases.get(name);
    assert.equal(outcome.result.text, phase.answer);
    const snapshot = await client.session(turn.rootId).snapshot({ signal: deadline });
    const tool = snapshot.messages.find(message => message.role === 'tool' && message.tool_call_id === phase.marker);
    assert(tool, 'Durable snapshot must include the worker result');
    assert.deepEqual(JSON.parse(tool.content), phase.toolResult);
    assert(snapshot.messages.some(message => message.role === 'assistant' && message.content === phase.answer));
    return { rootId: turn.rootId, commandId: turn.command.commandId, status: outcome.status,
      toolResult: phase.toolResult, answer: phase.answer, snapshotVerified: true };
  }
  let client = await connect();
  const info = client.requireConnected();
  assert.equal(info.pid, daemonPID); assert.equal(info.build_id, manifest.buildId);
  const daemon = await processEvidence(daemonPID);
  const before = await finish(client, 'before', await start(client, 'before'));
  const beforeWorkers = await eventually(async () => { const found = await workers(); return found.length ? found : undefined; }, 'first canonical worker');
  const pending = await start(client, 'held');
  await eventually(() => { if (providerFailure) throw providerFailure; return phases.get('held').toolResult; }, 'held worker result');
  assert.equal((await pending.command.status({ signal: deadline })).status, 'running');
  const heldWorkers = await workers(); assert(heldWorkers.length > beforeWorkers.length, 'A new session must start a distinct worker');
  client.close(); clients.delete(client);
  // Only our disposable source copy is removed. The installed app, staging
  // directory and canonical installation are never modified.
  assert.equal(path.dirname(copiedSource), fixture);
  await rm(copiedSource, { recursive: true });
  assert.equal(await lstat(copiedSource).then(() => true, error => { if (error.code === 'ENOENT') return false; throw error; }), false);
  releaseHeld();
  client = await connect();
  assert.equal(client.requireConnected().runtime_id, info.runtime_id);
  const recovered = await finish(client, 'held', { rootId: pending.rootId, command: client.recover(pending.command.record) });
  const after = await finish(client, 'after', await start(client, 'after'));
  const afterWorkers = await workers();
  const newWorkers = afterWorkers.filter(worker => !heldWorkers.some(previous => previous.pid === worker.pid));
  assert(newWorkers.length > 0, 'Source removal must be followed by spawning a new worker from the canonical executable');
  const final = await status(); assert.equal(final.state, 'running'); assert.equal(final.pid, daemonPID);
  assert.equal(await fileDigest(installed.executable), manifest.files.whipcode.sha256);
  assert((await readFile(installed.executable)).includes(helperBytes), 'Canonical runtime must retain the exact embedded helper');
  await run('/usr/bin/codesign', ['--verify', '--strict', '-R',
    `=anchor apple generic and certificate leaf[subject.OU] = "${manifest.teamId}"`, installed.executable], env);
  assert.equal(providerRequests, 6); assert(!providerFailure);
  result = { purpose: 'Signed canonical runtime and RLM worker continuity across client detach and copied application-source removal; not an actual Squirrel upgrade',
    recordedAt: new Date().toISOString(), source, fixture, manifest, executableMetadata: metadata,
    negotiated: { protocolMajor: info.protocol_major, protocolMinor: info.protocol_minor, runtimeId: info.runtime_id, buildId: info.build_id },
    environment: { WHIPCODE_HOME: home, embeddedHelper: true, inheritedCredentials: false, provider: baseURL, daemonTCP: false },
    daemon, finalDaemonPID: final.pid, beforeWorkers, heldWorkers, afterWorkers, newWorkerPIDsAfterSourceRemoval: newWorkers.map(worker => worker.pid),
    before, disconnectedAcceptedTurn: recovered, after, providerRequests, sourceCopyRemoved: true, canonicalSignaturesAndHashesVerified: true,
    limits: ['No Electron process launched: SDK client disconnect models GUI detachment.', 'No N-to-N+1 Squirrel installation or notarization tested.',
],
  };
} finally {
  releaseHeld(); for (const client of clients) client.close();
  provider.closeAllConnections(); if (provider.listening) await new Promise(resolve => provider.close(resolve));
  if (startAttempted) {
    try {
      await run(installed.executable, ['daemon', 'stop', '--force'], env, 20_000);
      const stopped = JSON.parse(await run(installed.executable, ['daemon', 'status', '--json'], env));
      assert.equal(stopped.state, 'stopped', 'Fixture daemon was not cleaned up');
      if (result) result.cleanup = { daemonStopped: true, fixtureRemoved: !values['keep-fixture'] };
    } catch (error) { cleanupError = error; }
  }
  if (!values['keep-fixture'] && !cleanupError) await rm(fixture, { recursive: true, force: true });
  else console.error(`Continuity fixture retained: ${fixture}`);
}
if (cleanupError) throw cleanupError;
await mkdir(path.dirname(output), { recursive: true });
await writeFile(output, JSON.stringify(result, null, 2) + '\n');
console.log(JSON.stringify({ evidence: output, daemonPID, newWorkers: result.newWorkerPIDsAfterSourceRemoval,
  workerTurnsVerified: 3, recoveredAcceptedTurn: true, cleanup: result.cleanup }, null, 2));
