// Run against an already-built binary: node scripts/web-gateway-smoke.mjs /path/to/whipcode
// Add --browser for real Chromium bootstrap (installed Playwright browser required).
// Every daemon uses a disposable home; no installed runtime is inspected or changed.
import assert from 'node:assert/strict';
import { Client, RemoteError } from '@whip/sdk';
import { browserSocket, discoverGateway } from '@whip/sdk/browser';
import { execFile, spawn } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { get } from 'node:http';
import path from 'node:path';
import { promisify } from 'node:util';
import { setTimeout as delay } from 'node:timers/promises';

const binary = path.resolve(process.argv[2] || './whipcode');
const directory = await mkdtemp('/tmp/whip-gateway-');
const execute = promisify(execFile);
const homes = [], children = [];
let browser;
function environment(name, extra = {}) {
  const home = path.join(directory, name);
  const env = { PATH: process.env.PATH, HOME: directory, XDG_CONFIG_HOME: path.join(directory, 'config'), ZDOTDIR: directory,
    SHELL: '/bin/sh', TMPDIR: directory, NODE_ENV: 'test',
    WHIPCODE_HOME: home, WHIPCODE_LISTEN: '127.0.0.1:0', ...extra };
  homes.push(env);
  return env;
}
const run = async (env, ...args) => (await execute(binary, args, { env, timeout: 30000 })).stdout;
const status = async (env) => JSON.parse(await run(env, 'daemon', 'status', '--json'));
async function eventually(check, description) {
  const until = Date.now() + 10000;
  while (Date.now() < until) {
    const value = await check();
    if (value) return value;
    await delay(25);
  }
  throw new Error(`Timed out: ${description}`);
}
async function waitForExit(child) {
  const timeout = setTimeout(() => child.kill('SIGKILL'), 10000);
  try {
    const result = await child.finished;
    assert.notEqual(result.signal, 'SIGKILL', 'gateway did not exit within its shutdown bound');
    return result;
  } finally { clearTimeout(timeout); }
}
async function foreground(env) {
  const child = spawn(binary, ['web', '--no-open'], { env, stdio: ['ignore', 'pipe', 'pipe'] });
  children.push(child);
  child.finished = new Promise((resolve) => child.once('exit', (code, signal) => resolve({ code, signal })));
  let output = '', errors = '';
  child.stdout.on('data', (data) => { output += data; });
  child.stderr.on('data', (data) => { errors += data; child.gatewayErrors = errors; });
  const endpoint = await eventually(() => {
    assert.equal(child.exitCode, null, `gateway exited: ${errors}`);
    return output.split(/\s+/).find((word) => word.startsWith('http://'));
  }, 'foreground gateway readiness');
  return { child, endpoint: new URL(endpoint).origin };
}
async function stopped(endpoint) {
  await eventually(async () => {
    try { await fetch(`${endpoint}/api/v4/web`, { signal: AbortSignal.timeout(500) }); return false; }
    catch { return true; }
  }, `listener closed: ${endpoint}`);
}
async function rpcSmoke(endpoint) {
  const discovery = await discoverGateway(endpoint, { signal: AbortSignal.timeout(5000) });
  const client = await Client.connect(browserSocket(endpoint, {
    expectedRuntimeID: discovery.runtime_id, expectedProcessEpoch: discovery.process_epoch,
  }), { clientID: crypto.randomUUID(), expectedRuntimeID: discovery.runtime_id });
  assert.equal(client.processEpoch, discovery.process_epoch);
  await assert.rejects(client.closeTerminal({ id: 'missing', process_epoch: client.processEpoch }),
    error => error instanceof RemoteError && error.kind === 'NETWORK_RESTRICTED');
}

try {
  const local = environment('local');
  await run(local, 'daemon', 'start');
  const before = await status(local);
  assert.equal(before.state, 'running');
  assert.equal(before.process.web_endpoint, '');
  assert(!before.process.web_endpoint);
  await run({ ...local, WHIPCODE_NETWORK: '1' }, 'daemon', 'start');
  assert.equal((await status(local)).process.web_endpoint, '', 'start must not reconfigure an existing daemon');
  const one = await foreground(local);
  const two = await foreground(local);
  assert.notEqual(one.endpoint, two.endpoint);
  const discovery = await (await fetch(`${one.endpoint}/api/v4/web`)).json();
  assert.equal(discovery.available, true, 'pack web assets before building the smoke binary');
  assert.equal(discovery.major, 4);
  assert.equal((await fetch(`${one.endpoint}/`, { headers: { Origin: 'https://untrusted.invalid' } })).status, 403);
  const forbiddenHost = await new Promise((resolve, reject) => {
    get(one.endpoint, { headers: { Host: 'untrusted.invalid' } }, (response) => {
      response.resume(); resolve(response.statusCode);
    }).on('error', reject);
  });
  assert.equal(forbiddenHost, 403);
  const asset = await fetch(`${one.endpoint}/`);
  assert.equal(asset.status, 200);
  assert(asset.headers.get('content-security-policy'));
  await rpcSmoke(one.endpoint);
  assert.equal((await run(environment('url-only'), 'web', '--url', one.endpoint, '--no-open')).trim(), `${one.endpoint}/`);
  if (process.argv.includes('--browser')) {
    const { chromium } = await import('@playwright/test');
    browser = await chromium.launch({ headless: true });
    const page = await browser.newPage();
    const errors = [], initialized = [];
    page.on('pageerror', (error) => errors.push(error.message));
    page.on('websocket', (socket) => socket.on('framereceived', ({ payload }) => {
      const frame = JSON.parse(String(payload));
      if (frame.result?.major === 4 && frame.result?.runtime_id) initialized.push(frame.result);
    }));
    await page.goto(one.endpoint);
    await page.getByRole('button', { name: 'New session', exact: true }).first().waitFor({ state: 'visible' });
    await eventually(() => initialized.length, 'browser SDK handshake');
    assert(initialized.every((value) => value.network_client === true && value.runtime_id === before.process.runtime_id && value.process_epoch === before.process.process_epoch));
    assert.deepEqual(errors, []);
    await browser.close();
    browser = undefined;
    console.log('PASS real Chromium UI bootstrap and restricted SDK handshake');
  }
  one.child.kill('SIGINT');
  assert.equal((await waitForExit(one.child)).code, 0);
  await stopped(one.endpoint);
  assert.equal((await status(local)).process.pid, before.process.pid);
  assert.equal((await fetch(`${two.endpoint}/api/v4/web`)).status, 200);
  const busy = new URL(two.endpoint).host;
  const failed = environment('failed', { WHIPCODE_NETWORK: '1', WHIPCODE_LISTEN: busy });
  await assert.rejects(run(failed, 'daemon', 'start'), /browser gateway failed.*address already in use/);
  const failure = await status(failed);
  assert.equal(failure.state, 'running');
  assert.equal(failure.process.web_state, 'failed');
  assert.match(failure.process.web_error, /address already in use/);
  assert(!failure.process.web_endpoint);
  assert.equal((await status(local)).process.process_epoch, before.process.process_epoch);
  console.log('PASS socket-only startup, unchanged existing runtime, concurrent foreground, security, Ctrl+C, explicit conflict isolation');
  const managed = environment('managed', { WHIPCODE_NETWORK: '1' });
  await run(managed, 'daemon', 'start');
  const ready = await status(managed);
  assert(ready.process.web_endpoint, 'managed native gateway must be ready');
  await rpcSmoke(ready.process.web_endpoint);
  await run(managed, 'daemon', 'stop');
  await stopped(ready.process.web_endpoint);
  await run(local, 'daemon', 'stop');
  assert.equal((await waitForExit(two.child)).code, 1, 'backend loss should be actionable');
  await stopped(two.endpoint);
  console.log('PASS managed readiness, restricted forwarding, parent shutdown and foreground backend-loss cleanup');
} finally {
  await browser?.close();
  for (const child of children) { if (child.exitCode === null) child.kill('SIGTERM'); }
  for (const env of homes) {
    await run(env, 'daemon', 'stop', '--timeout', '5s').catch((error) => console.error(error.stderr || error.message));
  }
  await Promise.all(children.map(waitForExit));
  for (const child of children) if (child.gatewayErrors) console.error(child.gatewayErrors);
  await rm(directory, { recursive: true, force: true });
}
