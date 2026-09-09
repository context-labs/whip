// Signed LaunchServices measurement only. No CDP, inspector, injected bridge,
// altered fuses, or network timing collector. The fake provider is seed-only.
import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { randomBytes, randomUUID } from 'node:crypto';
import { once, EventEmitter } from 'node:events';
import { createServer } from 'node:http';
import { lstat, mkdir, mkdtemp, readFile, realpath, rm, symlink, writeFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { cpus, totalmem, release } from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { parseArgs, promisify } from 'node:util';
import asar from '@electron/asar';
import { createWhipClient } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
import { LocalRuntime, readRuntimeManifest } from '../src/runtime.ts';
import { verifyDesktop } from './verify.mjs';
import { repositoryRoot, sha256 } from '../../../scripts/renderer-artifact.mjs';

const exec = promisify(execFile);
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const marker = 'Whip desktop startup fixture';
const answer = `Verified ${marker}: 42`;
const { values, positionals } = parseArgs({ allowPositionals: true, options: {
  samples: { type: 'string', default: '30' }, 'first-samples': { type: 'string', default: '3' },
  output: { type: 'string' }, 'self-test': { type: 'boolean' }, notarized: { type: 'boolean' },
  idle: { type: 'boolean' },
  'idle-settle': { type: 'string', default: '30' },
} });
assert(positionals.length <= 1, 'Usage: startup.mjs [Whip.app] [--samples 30] [--first-samples 3] [--output file]');
const minimalEnvironment = () => {
  const env = { PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh' };
  for (const key of ['HOME', 'USER', 'LOGNAME', 'TMPDIR']) if (process.env[key]) env[key] = process.env[key];
  return env;
};
const run = async (binary, args, env, timeout = 15_000) => (await exec(binary, args, {
  env, timeout, encoding: 'utf8', maxBuffer: 256 << 10,
})).stdout;
async function eventually(check, label, timeout = 35_000) {
  const deadline = performance.now() + timeout;
  do { const value = await check(); if (value) return value; await delay(25); } while (performance.now() < deadline);
  throw new Error(`Startup fixture timed out: ${label}`);
}
async function fixtureStatus(executable, env) {
  // internal/daemon/socket_unix.go uses this path below 100 bytes, otherwise a
  // hashed temporary runtime. Keep our fixtures short and prove the exact path.
  const socket = path.join(env.WHIPCODE_HOME, 'runtime-v2/daemon.sock');
  assert(Buffer.byteLength(socket) < 100, 'Use a shorter fixture home');
  const status = JSON.parse(await run(executable, ['daemon', 'status', '--json'], env));
  assert.equal(status.socket, socket); assert(!status.network_endpoint);
  if (status.state === 'running') {
    const directory = await lstat(path.dirname(socket)), endpoint = await lstat(socket);
    assert(directory.isDirectory() && !directory.isSymbolicLink() && directory.uid === process.getuid() && (directory.mode & 0o077) === 0);
    assert(endpoint.isSocket() && endpoint.uid === process.getuid() && (endpoint.mode & 0o077) === 0);
  }
  return status;
}
function summarize(samples) {
  const sorted = samples.toSorted((a, b) => a - b);
  if (!sorted.length) return { count: 0 };
  const middle = Math.floor(sorted.length / 2);
  return { count: sorted.length, medianMs: sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2,
    ...(sorted.length >= 30 ? { p95Ms: sorted[Math.ceil(sorted.length * 0.95) - 1] } : {}), minMs: sorted[0], maxMs: sorted.at(-1) };
}

function parseProcesses(output) {
  return output.trim().split('\n').filter(Boolean).map(line => {
    const match = /^\s*(\d+)\s+(\d+)\s+(.{24})\s+(\S+)\s+(\d+)\s+(.+)$/.exec(line);
    assert(match, 'Unexpected macOS ps format');
    const clock = /^(?:(\d+)-)?(?:(\d+):)?(\d+):(\d+(?:\.\d+)?)$/.exec(match[4]);
    assert(clock, 'Unexpected cumulative CPU time');
    return { pid: Number(match[1]), ppid: Number(match[2]), started: match[3],
      cpuSeconds: Number(clock[1] ?? 0) * 86400 + Number(clock[2] ?? 0) * 3600 + Number(clock[3]) * 60 + Number(clock[4]),
      rssKiB: Number(match[5]), executable: match[6] };
  });
}
async function processSnapshot() {
  const before = performance.now();
  const rows = parseProcesses(await run('/bin/ps', ['-axo', 'pid=,ppid=,lstart=,time=,rss=,comm='], minimalEnvironment()));
  const after = performance.now();
  return { atMs: (before + after) / 2, scanMs: after - before, rows };
}
function processTree(rows, root) {
  const included = new Set([root]);
  for (let previous = -1; previous !== included.size;) {
    previous = included.size;
    for (const row of rows) if (included.has(row.ppid)) included.add(row.pid);
  }
  return rows.filter(row => included.has(row.pid));
}
async function sampleIdle(pid, daemonPID, fixture, interrupted, result) {
  const settleSeconds = Number(values['idle-settle']);
  assert(Number.isInteger(settleSeconds) && settleSeconds >= 30 && settleSeconds <= 300);
  Object.assign(result, { settleSeconds, requestedSampleSeconds: 60, snapshots: [],
    systemBeforeSettle: await run('/usr/bin/vm_stat', [], minimalEnvironment()) });
  console.log(`Idle fixture GUI ${pid}, daemon ${daemonPID}: settling ${settleSeconds} seconds before a 60-second OS sample`);
  await delay(settleSeconds * 1000); assert(!interrupted(), 'Idle measurement interrupted');
  result.systemAfterSettle = await run('/usr/bin/vm_stat', [], minimalEnvironment());
  for (let index = 0; index <= 12; index++) {
    if (index) await delay(5000);
    assert(!interrupted(), 'Idle measurement interrupted');
    const snapshot = await processSnapshot();
    result.snapshots.push({ atMs: snapshot.atMs, scanMs: snapshot.scanMs,
      application: processTree(snapshot.rows, pid), daemon: processTree(snapshot.rows, daemonPID) });
    assert(result.snapshots.at(-1).application.some(row => row.pid === pid), 'Idle application exited during sampling');
    assert(result.snapshots.at(-1).daemon.some(row => row.pid === daemonPID), 'Fixture daemon exited during sampling');
    if (index === 0 || index === 6 || index === 12) console.log(`Idle OS sample ${index}/12 captured`);
  }
  result.systemAfterSampling = await run('/usr/bin/vm_stat', [], minimalEnvironment());
  const first = result.snapshots[0], last = result.snapshots.at(-1);
  result.elapsedSeconds = (last.atMs - first.atMs) / 1000;
  assert(result.elapsedSeconds >= 60);
  result.statistics = {};
  for (const group of ['application', 'daemon']) {
    const identities = rows => rows.map(row => `${row.pid}:${row.started}`).sort();
    const stable = result.snapshots.every(snapshot => JSON.stringify(identities(snapshot[group])) === JSON.stringify(identities(first[group])));
    const persistent = first[group].filter(row => result.snapshots.every(snapshot => snapshot[group].some(candidate => candidate.pid === row.pid && candidate.started === row.started)));
    const persistentCPUSeconds = persistent.reduce((sum, row) => sum + last[group].find(final => final.pid === row.pid).cpuSeconds - row.cpuSeconds, 0);
    const cpuSeconds = stable ? last[group].reduce((sum, row) => sum + row.cpuSeconds - first[group].find(initial => initial.pid === row.pid).cpuSeconds, 0) : null;
    const rss = result.snapshots.map(snapshot => snapshot[group].reduce((sum, row) => sum + row.rssKiB, 0) / 1024);
    result.statistics[group] = { stableProcessSet: stable, processCount: first[group].length,
      cpuSeconds, averageCPUPercentOfOneCore: stable ? cpuSeconds / result.elapsedSeconds * 100 : null,
      persistentProcessCount: persistent.length, persistentCPUSeconds,
      persistentCPUPercentLowerBound: persistentCPUSeconds / result.elapsedSeconds * 100,
      rssMiB: { mean: rss.reduce((sum, value) => sum + value, 0) / rss.length, min: Math.min(...rss), max: Math.max(...rss) } };
  }
  result.roles = [];
  for (const row of [...last.application, ...last.daemon]) {
    const args = await run('/bin/ps', ['-p', String(row.pid), '-o', 'args='], minimalEnvironment());
    result.roles.push({ pid: row.pid, type: /(?:^|\s)--type=([a-z-]+)/.exec(args)?.[1],
      utilitySubtype: /(?:^|\s)--utility-sub-type=([\w.]+)/.exec(args)?.[1],
      daemon: row.pid === daemonPID, worker: row.ppid === daemonPID });
  }
  if ((result.statistics.application.averageCPUPercentOfOneCore === null || result.statistics.application.averageCPUPercentOfOneCore > 1) && settleSeconds >= 90) {
    result.nativeSamples = [];
    for (const row of last.application.filter(row => row.pid === pid || row.executable.includes('(Renderer)'))) {
      const filename = path.join(fixture, `native-sample-${row.pid}.txt`);
      try {
        await run('/usr/bin/sample', [String(row.pid), '5', '10', '-file', filename], minimalEnvironment(), 20_000);
        result.nativeSamples.push({ pid: row.pid, text: await readFile(filename, 'utf8') });
      } catch (error) { result.nativeSamples.push({ pid: row.pid, unavailable: true, diagnostic: String(error.stderr ?? error.message).slice(0, 4096) }); }
    }
  }
  // Detailed memory traversal is intentionally after the CPU sample. The macOS
  // footprint tool de-duplicates shared mappings across explicitly selected PIDs.
  for (const group of ['application', 'daemon']) {
    const filename = path.join(fixture, `footprint-${group}.json`);
    try {
      const stdout = await run('/usr/bin/footprint', [...last[group].flatMap(row => ['-p', String(row.pid)]), '-f', 'bytes', '-j', filename], minimalEnvironment(), 30_000);
      result[`${group}Footprint`] = { stdout, data: JSON.parse(await readFile(filename, 'utf8')) };
    } catch (error) { result[`${group}Footprint`] = { unavailable: true, diagnostic: String(error.stderr ?? error.message).slice(0, 4096) }; }
  }
  console.log(`Idle results: ${JSON.stringify(result.statistics)}`);
  return result;
}

// One executable check covers opt-in, origin rejection, private output, connected
// controls, and file-descriptor cleanup without building or launching the app.
async function selfTest() {
  assert.equal(parseProcesses('82474 50096 Tue Sep  8 00:44:01 2026       0:00.01   2512 /bin/zsh')[0].cpuSeconds, 0.01);
  assert.equal(parseProcesses('1 0 Tue Sep  8 00:44:01 2026 1:02:03.45 100 /a')[0].cpuSeconds, 3723.45);
  assert.deepEqual(processTree([{ pid: 1, ppid: 0 }, { pid: 2, ppid: 1 }, { pid: 3, ppid: 2 }, { pid: 4, ppid: 0 }], 1).map(row => row.pid), [1, 2, 3]);
  const fixture = await realpath(await mkdtemp('/tmp/whip-startup-test-'));
  const saved = { ...process.env };
  const source = await readFile(new URL('../src/startup-probe.ts', import.meta.url), 'utf8');
  const compiled = stripTypeScriptTypes(source.replace("'./assets'", JSON.stringify(pathToFileURL(path.join(repositoryRoot, 'apps/desktop/src/assets.ts')).href)));
  await writeFile(path.join(fixture, 'probe.mjs'), compiled);
  const { attachStartupProbe } = await import(pathToFileURL(path.join(fixture, 'probe.mjs')));
  let capturedScript;
  const fakeWindow = () => {
    const window = new EventEmitter(); const webContents = new EventEmitter();
    let count = 0;
    Object.assign(webContents, { isDestroyed: () => false, getURL: () => 'whip-app://bundle/',
      executeJavaScript: async script => {
        capturedScript = script;
        assert(!script.includes('localStorage')); assert(!script.includes('fetch('));
        count++;
        return { now: 10, shell: 1, pathname: '/', host: true, noNotice: count > 1, home: true, session: false,
          transcript: false, visible: true, fonts: true, painted: true, forbiddenExtra: 'must not be serialized' };
      } });
    Object.assign(window, { webContents, isDestroyed: () => false });
    return { window, count: () => count };
  };
  try {
    delete process.env.WHIP_DESKTOP_FIXTURE;
    await attachStartupProbe(fakeWindow().window, { userData: fixture, rendererDigest: 'a'.repeat(64), quit() { assert.fail(); } });
    await assert.rejects(lstat(path.join(fixture, 'startup.json')), { code: 'ENOENT' });
    process.env.WHIP_DESKTOP_FIXTURE = '1'; process.env.WHIP_DESKTOP_STARTUP_PROBE = '1';
    process.env.WHIP_DESKTOP_STARTUP_RUN = 'a'.repeat(32); process.env.WHIP_DESKTOP_STARTUP_NS = String(process.hrtime.bigint());
    process.env.WHIP_DESKTOP_USER_DATA = fixture; process.env.WHIPCODE_HOME = fixture;
    const f = fakeWindow(); let quit = 0;
    await attachStartupProbe(f.window, { userData: fixture, rendererDigest: 'a'.repeat(64), quit() { quit++; } });
    assert.equal(JSON.parse(await readFile(path.join(fixture, 'startup.json'), 'utf8')).state, 'collecting');
    f.window.webContents.emit('dom-ready');
    await eventually(() => quit === 1, 'complete test probe', 2000);
    const record = JSON.parse(await readFile(path.join(fixture, 'startup.json'), 'utf8'));
    assert.equal(record.state, 'complete'); assert(f.count() >= 2);
    assert(!JSON.stringify(record).includes('forbiddenExtra')); assert.equal(record.checks.noNotice, true);
    assert.equal(f.window.listenerCount('closed'), 0); assert.equal(f.window.webContents.listenerCount('dom-ready'), 0);
    await assert.rejects(attachStartupProbe(fakeWindow().window, { userData: fixture, rendererDigest: 'a'.repeat(64), quit() {} }), { code: 'EEXIST' });
    await rm(path.join(fixture, 'startup.json'));
    const outside = path.join(fixture, 'outside'); await writeFile(outside, 'preserved'); await symlink(outside, path.join(fixture, 'startup.json'));
    await assert.rejects(attachStartupProbe(fakeWindow().window, { userData: fixture, rendererDigest: 'a'.repeat(64), quit() {} }), { code: 'EEXIST' });
    assert.equal(await readFile(outside, 'utf8'), 'preserved'); await rm(path.join(fixture, 'startup.json'));
    const wrong = fakeWindow(); wrong.window.webContents.getURL = () => 'https://example.invalid'; quit = 0;
    await attachStartupProbe(wrong.window, { userData: fixture, rendererDigest: 'a'.repeat(64), quit() { quit++; } });
    wrong.window.webContents.emit('dom-ready'); await eventually(() => quit === 1, 'origin rejection', 2000);
    assert.equal(wrong.count(), 0); assert.equal(JSON.parse(await readFile(path.join(fixture, 'startup.json'), 'utf8')).state, 'unexpected-origin');
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<aside aria-label="Session navigation"><button aria-label="Manage servers">This Mac</button></aside><form><textarea data-whip-composer aria-label="Message WHIP"></textarea><button aria-label="Add context" disabled></button></form><section aria-label="Conversation"><div data-message-id="fixture">Verified Whip desktop startup fixture: 42</div></section>',
      { url: 'whip-app://bundle/h/runtime/s/root', runScripts: 'outside-only', pretendToBeVisual: true });
    try {
      Object.defineProperty(dom.window.document, 'fonts', { value: { status: 'loaded', ready: Promise.resolve() } });
      dom.window.HTMLElement.prototype.getClientRects = () => [{}];
      dom.window.performance.getEntriesByName = () => [{ startTime: 0 }];
      let observed = await dom.window.eval(capturedScript);
      assert.equal(observed.session, false, 'Editable textarea must not prove connection');
      dom.window.document.querySelector('button[disabled]').disabled = false;
      observed = await dom.window.eval(capturedScript);
      assert(observed.session && observed.transcript && observed.noNotice && observed.painted);
      dom.window.document.body.insertAdjacentHTML('beforeend', '<div role="alert">Connection failed</div>');
      assert.equal((await dom.window.eval(capturedScript)).noNotice, false);
    } finally { dom.window.close(); }
    console.log('Startup probe self-test passed');
  } finally {
    for (const key of Object.keys(process.env)) if (!(key in saved)) delete process.env[key];
    Object.assign(process.env, saved); await rm(fixture, { recursive: true, force: true });
  }
}

async function seedSession(fixture, executable, env, scheme) {
  let requests = 0; let providerFailure;
  const provider = createServer((request, response) => {
    void (async () => {
      assert(++requests <= 2); assert.equal(request.method, 'POST'); assert.equal(request.url, '/chat/completions');
      assert.equal(request.headers.authorization, 'Bearer fixture-only');
      let size = 0; const chunks = [];
      for await (const chunk of request) { size += chunk.length; assert(size <= 1 << 20); chunks.push(chunk); }
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
      assert.equal(body.model, 'startup-model'); assert.equal(body.stream, true);
      assert.deepEqual(body.tools.map(tool => tool.function.name), ['rlm_exec']);
      assert(body.messages.some(message => message.role === 'user' && JSON.stringify(message.content).includes(marker)));
      let delta;
      if (requests === 1) delta = { tool_calls: [{ index: 0, id: 'startup-tool', type: 'function', function: {
        name: 'rlm_exec', arguments: JSON.stringify({ code: `print(${JSON.stringify(marker)})\n{"answer": 6 * 7}` }),
      } }] };
      else {
        const tool = body.messages.find(message => message.role === 'tool' && message.tool_call_id === 'startup-tool');
        assert(tool); const result = JSON.parse(tool.content);
        assert.equal(result.output, marker + '\n'); assert.deepEqual(result.value, { answer: 42 }); assert(result.steps > 0);
        delta = { content: answer };
      }
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      response.end(`data: ${JSON.stringify({ choices: [{ delta, finish_reason: delta.tool_calls ? 'tool_calls' : 'stop' }] })}\n\ndata: [DONE]\n\n`);
    })().catch(error => { providerFailure ??= error; response.writeHead(500); response.end('Fixture failed'); });
  });
  provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
  const baseURL = `http://127.0.0.1:${provider.address().port}`;
  let client;
  try {
    await writeFile(path.join(env.WHIPCODE_HOME, 'config.json'), JSON.stringify({ defaultModel: 'startup-model', defaultProvider: 'startup-provider',
      compactModel: 'startup-model', compactProvider: 'startup-provider',
      providers: { 'startup-provider': { baseUrl: baseURL, api: 'openai-completions', apiKey: 'fixture-only' } },
      models: { 'startup-model': { providers: ['startup-provider'], context: 65536, maxOut: 256 } }, rlm: { maxWorkers: 2 } }), { mode: 0o600 });
    await writeFile(path.join(env.WHIPCODE_HOME, 'models.json'), JSON.stringify({ 'startup-provider': { fetchedAt: new Date().toISOString(), baseUrl: baseURL,
      models: [{ id: 'startup-model', contextLength: 65536, maxCompletionTokens: 256, pricing: { prompt: '0', completion: '0' } }] } }), { mode: 0o600 });
    // prepare has already started this isolated daemon. Restart after config
    // seeding so provider registration never depends on live config reloading.
    await run(executable, ['daemon', 'stop'], env);
    assert.equal((await fixtureStatus(executable, env)).state, 'stopped');
    await run(executable, ['daemon', 'start'], env);
    const status = await fixtureStatus(executable, env);
    assert.equal(status.state, 'running');
    client = createWhipClient({ endpoint: unixSocket(status.socket), clientId: randomUUID(), clientKind: 'automation' });
    const signal = AbortSignal.timeout(30_000); await client.connect({ signal });
    const created = await client.sessions.create({ cwd: path.join(fixture, 'work'), model: 'startup-model', provider: 'startup-provider' }).result({ signal });
    assert.equal(created.status, 'succeeded');
    const rootId = created.result.root_id;
    const outcome = await client.session(rootId).submit({ text: marker }).result({ signal });
    assert.equal(outcome.status, 'succeeded'); if (providerFailure) throw providerFailure;
    const snapshot = await client.session(rootId).snapshot({ signal });
    assert(snapshot.messages.some(message => message.role === 'assistant' && message.content === answer));
    assert.equal(requests, 2);
    const runtimeId = client.getSnapshot().info.runtime_id;
    return { route: `/h/${runtimeId}/s/${rootId}`, link: `${scheme}://session/${runtimeId}/${rootId}`, providerRequests: requests };
  } finally {
    client?.close(); provider.closeAllConnections(); await new Promise(resolve => provider.close(resolve));
  }
}

async function main() {
  assert.equal(process.platform, 'darwin'); assert.equal(process.arch, 'arm64');
  const samples = Number(values.samples), firstSamples = Number(values['first-samples']);
  assert(Number.isInteger(samples) && samples >= 30 && samples <= 100, 'Use 30–100 warm samples');
  assert(Number.isInteger(firstSamples) && firstSamples >= 1 && firstSamples <= 30, 'Use 1–30 first-launch samples');
  const bundle = await realpath(positionals[0] ?? path.join(repositoryRoot, 'apps/desktop/out/Whip-darwin-arm64/Whip.app'));
  const evidence = await verifyDesktop(bundle, { signed: true, notarized: !!values.notarized });
  const archive = path.join(bundle, 'Contents/Resources/app.asar');
  const archiveSHA256 = sha256(await readFile(archive));
  const packageInfo = JSON.parse(asar.extractFile(archive, 'package.json').toString());
  const appBinary = path.join(bundle, 'Contents/MacOS', packageInfo.productName);
  assert((await lstat(appBinary)).isFile());
  assert(asar.extractFile(archive, 'main.cjs').includes(Buffer.from('WHIP_DESKTOP_STARTUP_PROBE')), 'Rebuild/sign the app with the reviewed startup probe first');
  const fixture = await realpath(await mkdtemp('/tmp/whip-startup-'));
  const executable = path.join(fixture, 'bin/whipcode');
  const output = path.resolve(values.output ?? path.join(repositoryRoot, `.ai-docs/plans/desktop-app/evidence/packaged-${values.idle ? 'idle' : 'startup'}.json`));
  const results = []; const environments = []; const ownedPids = new Set(); const ownedDaemonPids = new Set();
  let completed = false; let interrupted = false; let seeded;
  const interrupt = () => { interrupted = true; };
  process.once('SIGINT', interrupt); process.once('SIGTERM', interrupt);
  const createFixture = async name => {
    const directory = path.join(fixture, name), home = path.join(directory, 'home'), userData = path.join(directory, 'user-data');
    const userHome = path.join(directory, 'user'), temporary = path.join(directory, 'tmp');
    await mkdir(directory, { mode: 0o700 }); await mkdir(home, { mode: 0o700 }); await mkdir(userData, { mode: 0o700 });
    await mkdir(userHome, { mode: 0o700 }); await mkdir(temporary, { mode: 0o700 });
    // /usr/bin/open itself keeps the login user's HOME for LaunchServices. Its
    // --env arguments isolate app/daemon credential lookup and login shell files.
    const env = { ...minimalEnvironment(), HOME: userHome, TMPDIR: temporary,
      WHIPCODE_HOME: home, WHIPCODE_NETWORK: '0', WHIP_DESKTOP_USER_DATA: userData, WHIP_DESKTOP_FIXTURE: '1',
      WHIP_DESKTOP_EXECUTABLE: executable };
    environments.push(env); return { directory, env, userData };
  };
  async function launch(f, scenario, { route = '/', link, warmup = false, idle = false } = {}) {
    if (interrupted) throw new Error('Startup measurement interrupted');
    await rm(path.join(f.userData, 'startup.json'), { force: true });
    const runId = randomBytes(16).toString('hex');
    const ownershipArgument = `--whip-startup-run=${runId}`;
    const start = process.hrtime.bigint();
    const systemBeforeLaunch = idle ? await run('/usr/bin/vm_stat', [], minimalEnvironment()) : undefined;
    const env = { ...f.env, WHIP_DESKTOP_STARTUP_PROBE: idle ? '0' : '1', WHIP_DESKTOP_STARTUP_RUN: runId,
      WHIP_DESKTOP_STARTUP_NS: String(start), WHIP_DESKTOP_STARTUP_ROUTE: route };
    const args = ['-n', '-W', '-a', bundle, ...Object.entries(env).flatMap(([key, value]) => ['--env', `${key}=${value}`]),
      ...(link ? [link] : []), '--args', ownershipArgument];
    const child = spawn('/usr/bin/open', args, { env: minimalEnvironment(), stdio: ['ignore', 'pipe', 'pipe'] });
    let launchError = ''; child.stderr.on('data', chunk => { launchError = (launchError + chunk).slice(-4096); });
    child.stdout.resume();
    let launchExit;
    const exit = new Promise(resolve => {
      child.once('error', error => { launchExit = { code: null, signal: null, error: error.code }; resolve(launchExit); });
      child.once('exit', (code, signal) => { launchExit = { code, signal }; resolve(launchExit); });
    });
    let record, pid, captured;
    try {
      if (idle) {
        pid = await eventually(async () => {
          for (const candidate of (await processSnapshot()).rows.filter(row => row.executable === appBinary)) {
            const args = await run('/bin/ps', ['-p', String(candidate.pid), '-o', 'args='], minimalEnvironment()).catch(() => '');
            if (new RegExp(`(?:^|\\s)${ownershipArgument}(?:\\s|$)`).test(args)) return candidate.pid;
          }
          assert(!launchExit, 'Idle fixture exited before process identification'); return false;
        }, 'owned idle application');
        ownedPids.add(pid);
        const status = await fixtureStatus(executable, f.env);
        assert.equal(status.state, 'running'); ownedDaemonPids.add(status.pid);
        captured = { scenario, warmup, runId, pid, state: 'sampling', systemBeforeLaunch };
        results.push(captured);
        captured.idle = {};
        await sampleIdle(pid, status.pid, f.directory, () => interrupted, captured.idle);
        for (const sample of captured.idle.snapshots) {
          for (const row of sample.application) ownedPids.add(row.pid);
          for (const row of sample.daemon) ownedDaemonPids.add(row.pid);
        }
        captured.state = 'complete';
        return;
      }
      record = await eventually(async () => {
        try {
          const filename = path.join(f.userData, 'startup.json'); const stat = await lstat(filename);
          assert(stat.isFile() && !stat.isSymbolicLink() && stat.uid === process.getuid() && (stat.mode & 0o077) === 0 && stat.size <= 8192);
          const value = JSON.parse(await readFile(filename, 'utf8'));
          captured = value;
          assert.equal(value.runId, runId); assert.equal(value.rendererDigest, evidence.rendererDigest);
          assert(Number.isSafeInteger(value.pid) && value.pid > 0); pid = value.pid; ownedPids.add(pid);
          assert(!interrupted, 'Startup measurement interrupted');
          return value.state !== 'collecting' ? value : false;
        } catch (error) {
          if (error.code === 'ENOENT' || error instanceof SyntaxError) {
            assert(!launchExit, 'Signed app exited before producing its startup report'); return false;
          }
          throw error;
        }
      }, 'signed app startup report');
      const receivedMs = Number(process.hrtime.bigint() - start) / 1e6;
      assert(record.windowCreatedMs >= 0 && record.finishedMs <= receivedMs, 'Parent/main monotonic clock mismatch');
      captured = { scenario, warmup, ...record, receivedMs, launchServicesExit: undefined };
      results.push(captured);
      const closed = await Promise.race([exit, delay(10_000).then(() => undefined)]);
      captured.launchServicesExit = closed;
      assert(closed, 'Fixture app did not quit through the normal close path'); assert.equal(closed.code, 0, launchError);
      assert.equal(record.state, 'complete'); assert(record.checks.host && record.checks.noNotice && record.checks.fonts && record.checks.painted);
      assert(record.usable && record.connected && record.shell);
      const status = await fixtureStatus(executable, f.env);
      assert.equal(status.state, 'running'); assert(Number.isSafeInteger(status.pid) && status.pid > 0); ownedDaemonPids.add(status.pid);
      console.log(`${scenario}${warmup ? ' warmup' : ''}: shell ≤${record.shell.upperMs.toFixed(1)} ms, usable ≤${record.usable.upperMs.toFixed(1)} ms`);
    } catch (error) {
      if (!idle) results.push({ scenario, warmup, runId, pid, state: 'runner-failed',
        unidentifiedApp: !pid && !launchExit, ...(captured ? { lastReport: captured } : {}), launchError, launchExit });
      throw error;
    } finally {
      if (pid) {
        const stillOwned = async () => {
          const command = await run('/bin/ps', ['-p', String(pid), '-o', 'comm='], minimalEnvironment()).catch(() => '');
          if (command.trim() !== appBinary) return false;
          const args = await run('/bin/ps', ['-p', String(pid), '-o', 'args='], minimalEnvironment()).catch(() => '');
          return new RegExp(`(?:^|\\s)${ownershipArgument}(?:\\s|$)`).test(args);
        };
        if (await stillOwned()) { try { process.kill(pid, 'SIGTERM'); } catch {} }
        await eventually(async () => !await stillOwned(), 'owned GUI exit', 5000).catch(async () => {
          if (await stillOwned()) { try { process.kill(pid, 'SIGKILL'); } catch {} }
          await eventually(async () => !await stillOwned(), 'owned GUI killed', 5000);
        });
      }
      if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM');
      await exit.catch(() => {});
    }
  }
  try {
    await mkdir(path.join(fixture, 'work'), { mode: 0o700 });
    await mkdir(path.join(fixture, 'installer-user'), { mode: 0o700 });
    const manifestFile = path.join(fixture, 'runtime-manifest.json');
    await writeFile(manifestFile, asar.extractFile(archive, 'runtime-manifest.json'), { mode: 0o600 });
    await new LocalRuntime({ source: path.join(bundle, 'Contents/Helpers'), manifest: await readRuntimeManifest(manifestFile),
      settingsFile: path.join(fixture, 'native-local-runtime.json'), defaultExecutable: executable,
      env: { ...minimalEnvironment(), HOME: path.join(fixture, 'installer-user'), WHIPCODE_HOME: path.join(fixture, 'installer-home') } })
      .install(executable, AbortSignal.timeout(15_000));
    for (let index = 0; index < (values.idle ? 0 : firstSamples); index++) {
      const f = await createFixture(`first-${index}`); await launch(f, 'first-launch');
      await run(executable, ['daemon', 'stop'], f.env); await rm(f.directory, { recursive: true, force: true });
    }
    const retained = await createFixture('retained'); await launch(retained, 'prepare', { warmup: true });
    seeded = await seedSession(fixture, executable, retained.env, packageInfo.productName === 'Whip Beta' ? 'whip-beta' : 'whip');
    await launch(retained, 'prepare-session', { route: seeded.route, link: seeded.link, warmup: true });
    if (values.idle) {
      await launch(retained, 'idle', { idle: true });
      completed = true;
      return;
    }
    // Warm launches use ordinary restoration; the deep link was only a setup step.
    for (let index = 0; index < samples; index++) await launch(retained, 'warm-attach', { route: seeded.route });
    for (let index = 0; index < samples; index++) {
      await run(executable, ['daemon', 'stop'], retained.env);
      await launch(retained, 'retained-start', { route: seeded.route });
    }
    completed = true;
  } finally {
    const cleanup = [];
    for (const env of environments) {
      if (!await lstat(env.WHIPCODE_HOME).catch(() => false)) continue;
      try {
        const status = await fixtureStatus(executable, env);
        if (status.pid) ownedDaemonPids.add(status.pid);
        await run(executable, ['daemon', 'stop'], env).catch(() => {});
        if ((await fixtureStatus(executable, env)).state !== 'stopped') await run(executable, ['daemon', 'stop', '--force'], env);
        cleanup.push({ scenario: path.basename(path.dirname(env.WHIPCODE_HOME)), state: (await fixtureStatus(executable, env)).state });
      } catch { cleanup.push({ scenario: path.basename(path.dirname(env.WHIPCODE_HOME)), state: 'cleanup-failed' }); }
    }
    const statistics = {};
    for (const scenario of ['first-launch', 'warm-attach', 'retained-start']) {
      const attempts = results.filter(record => record.scenario === scenario && !record.warmup);
      const measured = attempts.filter(record => record.state === 'complete' && record.launchServicesExit?.code === 0);
      statistics[scenario] = { attempted: attempts.length, failed: attempts.length - measured.length,
        shellUpperMs: summarize(measured.map(record => record.shell.upperMs)),
        usableUpperMs: summarize(measured.map(record => record.usable.upperMs)),
        probeWallMs: summarize(measured.map(record => record.instrumentation.wallMs)),
        maxProbeBracketMs: summarize(measured.map(record => record.instrumentation.maxWallMs)) };
    }
    await mkdir(path.dirname(output), { recursive: true });
    await writeFile(output, JSON.stringify({ recordedAt: new Date().toISOString(), purpose: values.idle ? 'Signed app settled idle OS sampling; startup probe disabled during idle window' : 'Signed LaunchServices startup; OS caches warm; first launch with an explicitly installed canonical runtime separate from daemon start and attachment',
      clock: 'Parent/main Darwin mach_continuous_time via hrtime; renderer mark correlated through native executeJavaScript call/return',
      timingSemantics: 'shell lower/upper bound the existing renderer mark. connected/usable lower/upper bracket an observation, not the unknown readiness transition. Report only conservative upperMs. Polling is 25 ms; each usable observation waits for fonts and two animation frames (250 ms bound). Probe wall time includes that wait and IPC, not CPU time; initial report write/datasync is additional startup overhead.',
      completed, interrupted, archiveSHA256, environment: { cpu: cpus()[0]?.model, totalMemoryBytes: totalmem(), darwin: release(), node: process.version,
        PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh', applicationHOME: 'dedicated empty fixture user directory', applicationTMPDIR: 'dedicated fixture directory', inheritedCredentials: false },
      evidence, fixture: { providerRequests: seeded?.providerRequests, retainedAnswer: answer, daemonTransport: 'private Unix socket', providerClosedDuringMeasurements: true },
      requested: values.idle ? { settleSeconds: Number(values['idle-settle']), sampleSeconds: 60 } : { samples, firstSamples }, statistics, results, cleanup,
      ownedGuiPids: [...ownedPids], ownedDaemonPids: [...ownedDaemonPids] }, null, 2) + '\n');
    const clean = cleanup.every(item => item.state === 'stopped') && !results.some(item => item.unidentifiedApp);
    if (clean) await rm(fixture, { recursive: true, force: true });
    process.removeListener('SIGINT', interrupt); process.removeListener('SIGTERM', interrupt);
    console.log(`Startup evidence: ${output}`);
    assert(clean, `Fixture process cleanup could not be verified; preserved fixture at ${fixture}`);
  }
}

if (values['self-test']) await selfTest(); else await main();
