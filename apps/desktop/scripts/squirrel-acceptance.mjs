// One-time release acceptance. This must never run against a developer's home.
import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { promisify } from 'node:util';
import { createHash, randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { createServer } from 'node:http';
import { homedir } from 'node:os';
import { mkdir, readFile, writeFile, lstat, unlink } from 'node:fs/promises';
import path from 'node:path';
import asar from '@electron/asar';
import { build } from 'esbuild';
import { chromium } from 'playwright';
import { createWhipClient, webSocket } from '../../../packages/sdk/dist/index.js';
import { unixSocket } from '../../../packages/sdk/dist/node.js';
const exec = promisify(execFile);
const run = async (command, args, options = {}) => (await exec(command, args, { timeout: 30_000, maxBuffer: 4 << 20, ...options })).stdout;
const until = async (check, label, timeout = 60_000) => {
  const end = Date.now() + timeout; let last;
  while (Date.now() < end) {
    try { const result = await check(); if (result) return result; } catch (error) { last = error; }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  throw new Error(`Timed out: ${label}`, { cause: last });
};
assert.equal(process.env.GITHUB_ACTIONS, 'true');
assert.equal(process.env.RUNNER_ENVIRONMENT, 'github-hosted');
assert.equal(process.platform, 'darwin'); assert.equal(process.arch, 'arm64');
const home = homedir(); assert(home.startsWith('/Users/runner'));
assert(!process.env.WHIPCODE_HOME && !process.env.WHIP_DESKTOP_FIXTURE);
const runtimeHome = path.join(home, '.whipcode');
const userData = path.join(home, 'Library/Application Support/Whip Beta');
for (const dir of [runtimeHome, userData]) assert.equal(await lstat(dir).then(() => true, () => false), false, `Refusing nonempty runner: ${dir}`);
const targetVersion = process.env.RELEASE_VERSION;
const bundle = path.join(home, 'Applications/Whip Beta.app');
const executable = path.join(home, '.local/bin/whipcode');
await mkdir(path.dirname(bundle), { recursive: true });
await mkdir(path.dirname(executable), { recursive: true });
await mkdir(runtimeHome, { mode: 0o700 });
await mkdir(userData, { recursive: true, mode: 0o700 });
const oldURL = 'https://whipcode-releases.inference.net/qa-20260908/desktop/beta/darwin/arm64/Whip%20Beta-darwin-arm64-0.2.0-beta.0.zip';
const old = await fetch(oldURL, { redirect: 'error', signal: AbortSignal.timeout(120_000) });
assert.equal(old.status, 200);
const oldBytes = Buffer.from(await old.arrayBuffer()); assert(oldBytes.length < 256 << 20);
assert.equal(createHash('sha256').update(oldBytes).digest('hex'), '951a09eb1012c56fd21e861eb0dfe7bcd5b686b3bdf11a33adfb6f26609bd8cc');
await writeFile('acceptance/previous.zip', oldBytes);
await mkdir('previous', { recursive: true });
await run('/usr/bin/ditto', ['-x', '-k', 'acceptance/previous.zip', 'previous']);
await unlink('acceptance/previous.zip');
await run('/usr/bin/ditto', ['previous/Whip Beta.app', bundle]);
await run('/usr/bin/codesign', ['--verify', '--deep', '--strict', '-R', '=anchor apple generic and certificate leaf[subject.OU] = "JAPWPV5JY2"', bundle]);
const manifest = JSON.parse(asar.extractFile(path.join(bundle, 'Contents/Resources/app.asar'), 'runtime-manifest.json'));
const targetManifest = JSON.parse(asar.extractFile('downloaded/Whip Beta.app/Contents/Resources/app.asar', 'runtime-manifest.json'));
assert.equal(manifest.version, '0.2.0-beta.0'); assert.equal(targetManifest.version, targetVersion);
await build({ entryPoints: ['apps/desktop/src/runtime.ts'], bundle: true, platform: 'node', format: 'esm', outfile: 'acceptance/runtime.mjs' });
const { LocalRuntime, fileDigest } = await import(path.resolve('acceptance/runtime.mjs'));
const settings = path.join(userData, 'native-local-runtime.json');
const manager = new LocalRuntime({ source: path.join(bundle, 'Contents/Helpers'), manifest, settingsFile: settings });
const signal = AbortSignal.timeout(15 * 60_000);
await manager.install(executable, signal);
let requests = 0; let held = false;
const provider = createServer(async (request, response) => {
  if (request.method === 'GET' && request.url === '/models') {
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ data: [{ id: 'acceptance-model', context_length: 65536, max_completion_tokens: 256,
      pricing: { prompt: '0', completion: '0' } }] })); return;
  }
  if (request.method !== 'POST' || request.url !== '/chat/completions') {
    response.writeHead(404); response.end('Unknown fixture endpoint'); return;
  }
  const chunks = []; for await (const chunk of request) chunks.push(chunk);
  const body = JSON.parse(Buffer.concat(chunks)); requests++;
  response.writeHead(200, { 'Content-Type': 'text/event-stream' });
  if (JSON.stringify(body.messages).includes('Keep this turn running during the download')) { held = true; response.write(': waiting\n\n'); return; }
  response.end(`data: ${JSON.stringify({ choices: [{ delta: { content: 'Retained desktop update acceptance answer' }, finish_reason: 'stop' }] })}\n\ndata: [DONE]\n\n`);
});
provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
const baseUrl = `http://127.0.0.1:${provider.address().port}`;
const config = JSON.stringify({ defaultModel: 'acceptance-model', defaultProvider: 'acceptance-provider',
  compactModel: 'acceptance-model', compactProvider: 'acceptance-provider',
  providers: { 'acceptance-provider': { baseUrl, api: 'openai-completions', apiKey: 'fixture-only' } },
  models: { 'acceptance-model': { providers: ['acceptance-provider'], context: 65536, maxOut: 256 } }, rlm: { maxWorkers: 2 } });
await writeFile(path.join(runtimeHome, 'config.json'), config, { mode: 0o600 });
await writeFile(path.join(runtimeHome, 'models.json'), JSON.stringify({ 'acceptance-provider': { fetchedAt: new Date().toISOString(), baseUrl,
  models: [{ id: 'acceptance-model', contextLength: 65536, maxCompletionTokens: 256, pricing: { prompt: '0', completion: '0' } }] } }), { mode: 0o600 });
await manager.prepare(signal, () => {});
const status = async () => JSON.parse(await run(executable, ['daemon', 'status', '--json']));
const before = await status(); assert.equal(before.daemon_build, manifest.buildId);
const client = createWhipClient({ endpoint: unixSocket(before.socket), clientId: randomUUID(), clientKind: 'automation' });
let browser; let oldPID; let newPID; let socketClient;
const report = { from: manifest.version, to: targetVersion, completed: false, events: [] };
const pids = async () => (await run('/bin/ps', ['-axo', 'pid=,comm='])).split('\n').flatMap(line => {
  const match = /^\s*(\d+)\s+(.+)$/.exec(line); return match?.[2] === path.join(bundle, 'Contents/MacOS/Whip Beta') ? [Number(match[1])] : [];
});
try {
  await client.connect({ signal }); const runtimeId = client.getSnapshot().info.runtime_id;
  const root = (await client.sessions.create({ cwd: home, model: 'acceptance-model', provider: 'acceptance-provider' }).result({ signal })).result.root_id;
  assert.equal((await client.session(root).submit({ text: 'Keep this completed answer through the update' }).result({ signal })).status, 'succeeded');
  const retained = await client.session(root).snapshot({ signal });
  const command = client.session(root).submit({ text: 'Keep this turn running during the download' });
  await command.accepted({ signal }); await until(() => held, 'provider holding active work');
  spawn('/usr/bin/open', ['-n', '-a', bundle, '--args', '--remote-debugging-port=9229'], { stdio: 'ignore' }).unref();
  oldPID = await until(async () => (await pids())[0], 'old app process');
  browser = await until(async () => chromium.connectOverCDP('http://127.0.0.1:9229', { timeout: 1000 }), 'Chromium debugging connection');
  const page = await until(() => browser.contexts()[0]?.pages().find(page => page.url().startsWith('whip-app://bundle')), 'app renderer');
  assert.equal(await page.evaluate(() => window.whipDesktop.appVersion), manifest.version);
  await page.evaluate(() => { window.__updateEvents = []; window.whipDesktop.onEvent(event => { if (event.kind === 'update') window.__updateEvents.push(event); }); });
  await page.evaluate(() => window.whipDesktop.checkForUpdates());
  await until(async () => {
    report.events = await page.evaluate(() => window.__updateEvents);
    const error = report.events.find(event => event.state === 'error'); assert(!error, JSON.stringify(error));
    return report.events.some(event => event.state === 'downloaded' && event.version === targetVersion);
  }, 'signed update download', 180_000);
  assert.equal((await status()).pid, before.pid);
  assert.equal((await client.call('command.status', { command_id: command.commandId })).status, 'running');
  report.downloadPreservedActiveWork = true;
  await page.evaluate(() => { window.__installReturned = false; window.whipDesktop.installUpdate().then(() => { window.__installReturned = true; }); });
  await run(path.join(process.env.RUNNER_TEMP, 'whip-click-button'), [String(oldPID), 'Cancel']);
  await until(() => page.evaluate(() => window.__installReturned), 'cancelled install returned');
  assert.equal((await status()).pid, before.pid);
  assert.equal(JSON.parse(await readFile(settings)).managed.approvedVersion, undefined);
  report.cancelPreservedBackend = true;
  await page.evaluate(() => { void window.whipDesktop.installUpdate(); });
  await run(path.join(process.env.RUNNER_TEMP, 'whip-click-button'), [String(oldPID), 'Restart to update']);
  newPID = await until(async () => (await pids()).find(pid => pid !== oldPID), 'Squirrel relaunch', 180_000);
  const after = await until(async () => { const value = await status(); return value.state === 'running' && value.daemon_build === targetManifest.buildId && value.build_match ? value : false; }, 'automatic managed backend update', 60_000);
  assert.notEqual(after.pid, before.pid);
  assert.equal(await fileDigest(executable), targetManifest.files.whipcode.sha256);
  assert.equal(await fileDigest(path.join(bundle, 'Contents/Resources/app.asar')), await fileDigest('downloaded/Whip Beta.app/Contents/Resources/app.asar'));
  await run('/usr/bin/codesign', ['--verify', '--deep', '--strict', bundle]);
  await until(() => client.getSnapshot().state === 'connected', 'CLI client reconnect');
  assert.equal(client.getSnapshot().info.runtime_id, runtimeId);
  const restored = await client.session(root).snapshot({ signal });
  assert.deepEqual(restored.messages.slice(0, retained.messages.length), retained.messages);
  assert.equal(await readFile(path.join(runtimeHome, 'config.json'), 'utf8'), config);
  assert.equal((await client.call('command.status', { command_id: command.commandId })).status, 'interrupted');
  assert.equal(JSON.parse(await readFile(settings)).managed.approvedVersion, undefined);
  const web = await fetch(after.network_endpoint); assert.equal(web.status, 200); assert((await web.text()).includes('<html'));
  socketClient = createWhipClient({ endpoint: webSocket(after.network_endpoint.replace(/^http/, 'ws') + '/api/v3/ws'), clientId: randomUUID(), clientKind: 'automation' });
  await socketClient.connect({ signal }); assert.equal((await socketClient.session(root).snapshot({ signal })).root_id, root);
  Object.assign(report, { completed: true, oldPID, newPID, before, after, oneApprovalUpdatedBoth: true, configPreserved: true, historyPreserved: true, interruptedWorkRecorded: true, unixReconnected: true, websocketReconnected: true, webRendererServed: true, providerRequests: requests });
} finally {
  socketClient?.close(); client.close();
  for (const pid of await pids()) { try { process.kill(pid, 'SIGTERM'); } catch {} }
  await run(executable, ['daemon', 'stop']).catch(() => {});
  provider.closeAllConnections(); await new Promise(resolve => provider.close(resolve));
  report.cleanup = await status();
  await browser?.close().catch(() => {});
  await writeFile('acceptance/squirrel-update.json', JSON.stringify(report, null, 2)+'\n');
}
assert(report.completed && report.cleanup.state === 'stopped');
console.log('Actual signed Squirrel app and canonical backend update passed.');
