// Signed shipping-fuse Browser acceptance fixture. UI interaction is deliberately
// external (native Accessibility/manual); no CDP, injected bridge, or app patch.
// Run: node apps/desktop/scripts/browser-packaged.mjs '/path/to/Whip Beta.app' [--enabled] [--user-home]
// --user-home requires consent to normal macOS keychain use. Only the app gets
// the real HOME; daemon home, app data, browser profile and temp files stay isolated.
// Write {"action":"launch","enabled":true}, {"action":"stop"}, or
// {"action":"quit"} to the printed fixture's command.json. Default launch is OFF.
import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { createServer } from 'node:http';
import { mkdir, mkdtemp, readFile, realpath, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { setTimeout as delay } from 'node:timers/promises';
import asar from '@electron/asar';
import { LocalRuntime, readRuntimeManifest } from '../src/runtime.ts';
import { verifyDesktop } from './verify.mjs';

const exec = promisify(execFile);
assert.equal(process.platform, 'darwin');
const flags = process.argv.slice(3);
assert.ok(process.argv[2] && flags.every(flag => ['--enabled', '--user-home'].includes(flag)) && new Set(flags).size === flags.length, 'Pass a signed Beta app bundle and optional --enabled / --user-home');
const userHome = flags.includes('--user-home');
assert.ok(!userHome || path.isAbsolute(process.env.HOME ?? ''), '--user-home requires an absolute HOME');
const bundle = await realpath(process.argv[2]);
const verified = await verifyDesktop(bundle, { signed: true });
const archive = path.join(bundle, 'Contents/Resources/app.asar');
const metadata = JSON.parse(asar.extractFile(archive, 'package.json').toString());
assert.equal(metadata.productName, 'Whip Beta', 'Never launch the user’s stable app for this fixture');
const fixture = await realpath(await mkdtemp('/tmp/whip-browser-package-'));
const executable = path.join(fixture, 'bin/whipcode');
const marker = randomUUID();
const ownership = `--whip-browser-fixture=${marker}`;
const minimal = { PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh' };
for (const key of ['HOME', 'USER', 'LOGNAME', 'TMPDIR']) if (process.env[key]) minimal[key] = process.env[key];
const env = { ...minimal, HOME: path.join(fixture, 'user'), TMPDIR: path.join(fixture, 'tmp'),
  WHIPCODE_HOME: path.join(fixture, 'home'), WHIPCODE_NETWORK: '0', WHIP_DESKTOP_FIXTURE: '1',
  WHIP_DESKTOP_EXECUTABLE: executable, WHIP_DESKTOP_USER_DATA: path.join(fixture, 'profile') };
for (const directory of [env.HOME, env.TMPDIR, env.WHIPCODE_HOME, env.WHIP_DESKTOP_USER_DATA, path.join(fixture, 'work')]) await mkdir(directory, { mode: 0o700 });
let launcher, appPID, enabled = false, interrupted = false, installed = false;
const records = [], requests = { pages: 0, clicks: 0 };
const evidencePath = `${fixture}-evidence.json`;
const record = (action, detail = {}) => records.push({ at: new Date().toISOString(), action, ...detail });
const page = `<!doctype html><meta charset="utf-8"><title>Packaged Browser Fixture</title>
<style>body{font:18px system-ui;margin:32px;line-height:1.5}input,button,a{font:inherit;margin:8px}h1{font-size:24px}#count{font-size:48px;color:#087f8c}</style>
<h1>Native Browser acceptance</h1><p id="marker">${marker}</p>
<label>Fixture input <input aria-label="Fixture input" id="input"></label>
<button id="increment">Increment fixture</button><div id="count">0</div>
<a href="/second">Second fixture page</a><a href="/popup" target="_blank">Open fixture popup</a>
<p id="storage"></p><script>
let count=0; increment.onclick=async()=>{await fetch('/click',{method:'POST'});document.querySelector('#count').textContent=String(++count)};
const visits=Number(localStorage.getItem('fixture-visits')||0)+1;localStorage.setItem('fixture-visits',String(visits));
document.querySelector('#storage').textContent='Profile visits: '+visits;
</script>`;
const server = createServer((request, response) => {
  if (request.url === '/status') { response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ marker, ...requests })); return; }
  if (request.url === '/click' && request.method === 'POST') { requests.clicks++; request.resume(); response.end('ok'); return; }
  if (['/', '/second', '/popup'].includes(request.url)) {
    requests.pages++; response.setHeader('Content-Type', 'text/html; charset=utf-8'); response.end(page); return;
  }
  response.writeHead(404); response.end('Fixture endpoint not found');
});
server.listen(0, '127.0.0.1'); await once(server, 'listening');
const url = `http://127.0.0.1:${server.address().port}/`;
const snapshot = async () => writeFile(path.join(fixture, 'state.json'), JSON.stringify({ fixture, bundle, executable, env, userHome, marker, url, appPID, enabled, requests, evidencePath }, null, 2), { mode: 0o600 });
async function findOwnedPID() {
  const rows = (await exec('/bin/ps', ['-axo', 'pid=,command='])).stdout.split('\n');
  const matches = rows.filter(row => row.includes(`${bundle}/Contents/MacOS/`) && row.includes(ownership));
  assert(matches.length <= 1, 'Ambiguous fixture app ownership');
  return matches.length ? Number(matches[0].trim().split(/\s+/)[0]) : undefined;
}
async function owned() {
  if (!appPID) return false;
  try { return (await exec('/bin/ps', ['-p', String(appPID), '-o', 'args='])).stdout.includes(ownership); }
  catch { return false; }
}
async function stop() {
  appPID ??= await findOwnedPID();
  if (await owned()) process.kill(appPID, 'SIGTERM');
  for (let i = 0; i < 300 && await owned(); i++) await delay(100);
  if (await owned()) {
    // A forced fixture cleanup is recorded, never counted as graceful restart evidence.
    record('forced-stop', { appPID, reason: 'SIGTERM grace expired' });
    process.kill(appPID, 'SIGKILL');
    for (let i = 0; i < 50 && await owned(); i++) await delay(100);
  }
  assert(!await owned(), 'Owned fixture app did not stop; refusing another launch');
  if (launcher?.exitCode === null && launcher?.signalCode === null) launcher.kill('SIGTERM');
  appPID = undefined; launcher = undefined; await snapshot();
}
async function launch(flag) {
  await stop(); enabled = flag;
  // Exercise the packaged default with the opt-in absent, not explicitly false.
  const launchEnv = { ...env, ...(userHome ? { HOME: minimal.HOME } : {}), ...(flag ? { WHIP_DESKTOP_BROWSER_TABS: '1' } : {}) };
  launcher = spawn('/usr/bin/open', ['-n', '-W', '-a', bundle, ...Object.entries(launchEnv).flatMap(([key, value]) => ['--env', `${key}=${value}`]), '--args', ownership], { env: minimal, stdio: ['ignore', 'ignore', 'pipe'] });
  launcher.stderr.on('data', data => process.stderr.write(data));
  let launchError;
  launcher.once('error', error => { launchError = error; });
  for (let i = 0; i < 150; i++) {
    if (launchError) throw launchError;
    appPID = await findOwnedPID();
    if (appPID) break;
    if (launcher.exitCode !== null) throw new Error(`LaunchServices exited ${launcher.exitCode}`);
    await delay(100);
  }
  assert(appPID, 'No owned app PID after LaunchServices launch');
  record('launch', { enabled, userHome, appPID }); await snapshot(); console.log(JSON.stringify({ fixture, appPID, enabled, url }));
}
process.once('SIGINT', () => { interrupted = true; });
process.once('SIGTERM', () => { interrupted = true; });
try {
  const manifestFile = path.join(fixture, 'runtime-manifest.json');
  await writeFile(manifestFile, asar.extractFile(archive, 'runtime-manifest.json'), { mode: 0o600 });
  await new LocalRuntime({ source: path.join(bundle, 'Contents/Helpers'), manifest: await readRuntimeManifest(manifestFile),
    settingsFile: path.join(fixture, 'native-local-runtime.json'), defaultExecutable: executable, env })
    .install(executable, AbortSignal.timeout(15_000));
  installed = true;
  const baseUrl = url.slice(0, -1);
  await writeFile(path.join(env.WHIPCODE_HOME, 'config.json'), JSON.stringify({ defaultModel: 'browser-fixture', defaultProvider: 'browser-fixture',
    compactModel: 'browser-fixture', compactProvider: 'browser-fixture',
    providers: { 'browser-fixture': { baseUrl, api: 'openai-completions', apiKey: 'fixture-only' } },
    models: { 'browser-fixture': { providers: ['browser-fixture'], context: 65536, maxOut: 256 } }, rlm: { maxWorkers: 2 } }), { mode: 0o600 });
  await writeFile(path.join(env.WHIPCODE_HOME, 'models.json'), JSON.stringify({ 'browser-fixture': { fetchedAt: new Date().toISOString(), baseUrl,
    models: [{ id: 'browser-fixture', contextLength: 65536, maxCompletionTokens: 256, pricing: { prompt: '0', completion: '0' } }] } }), { mode: 0o600 });
  await exec(executable, ['daemon', 'start'], { env, cwd: path.join(fixture, 'work'), timeout: 15_000 });
  await launch(flags.includes('--enabled'));
  const deadline = Date.now() + 45 * 60_000;
  while (!interrupted && Date.now() < deadline) {
    let command;
    try { command = JSON.parse(await readFile(path.join(fixture, 'command.json'), 'utf8')); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    if (command) {
      await rm(path.join(fixture, 'command.json'));
      if (command.action === 'quit') break;
      if (command.action === 'stop') { await stop(); record('stop'); }
      else if (command.action === 'launch' && typeof command.enabled === 'boolean') await launch(command.enabled);
      else throw new Error('Unsupported fixture command');
      await snapshot();
    }
    await delay(250);
  }
} finally {
  const errors = [];
  for (const cleanup of [stop,
    async () => { if (installed) await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 }); },
    async () => { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }]) {
    try { await cleanup(); } catch (error) { errors.push(error); }
  }
  record('cleanup', { errors: errors.map(error => String(error)) });
  await writeFile(evidencePath, JSON.stringify({ purpose: 'Signed shipping-fuse ordinary UI fixture, not automated UI assertions', verified, bundle, userHome, marker, requests, records }, null, 2), { mode: 0o600 });
  if (errors.length) throw new AggregateError(errors, `Cleanup incomplete; inspect owned fixture ${fixture}`);
  await rm(fixture, { recursive: true });
  console.log(`Fixture cleanup complete; evidence ${evidencePath}`);
}
