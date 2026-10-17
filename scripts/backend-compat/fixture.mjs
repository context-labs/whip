import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join, isAbsolute } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

export async function eventually(check, label, timeout = 30_000) {
  const end = Date.now() + timeout;
  let last;
  while (Date.now() < end) {
    try { const value = await check(); if (value) return value; } catch (error) { last = error; }
    await delay(25);
  }
  throw new Error(`Timed out: ${label}`, { cause: last });
}

// Deliberately independent of SDK testing-node: that helper builds its own Go
// checkout. This launcher executes only the explicitly supplied artifact.
export async function startFixture(binary, directory, { agents = false } = {}) {
  assert.ok(isAbsolute(binary), 'fixture binary must be absolute');
  await mkdir(join(directory, 'public'), { recursive: true });
  let generation = 1;
  try { generation += JSON.parse(await readFile(join(directory, 'bridge.json'))).generation; }
  catch (error) { if (error.code !== 'ENOENT') throw error; }
  // Do not inherit live provider credentials, configuration paths or telemetry.
  const env = Object.fromEntries(['PATH', 'TMPDIR', 'TEMP', 'TMP', 'SYSTEMROOT'].flatMap(key => process.env[key] ? [[key, process.env[key]]] : []));
  Object.assign(env, { WHIP_SDK_FIXTURE_DIR: directory, WHIP_SDK_FIXTURE_LIFETIME: '15m', WHIP_SDK_AGENTS_FIXTURE: agents ? '1' : '0' });
  const child = spawn(binary, ['-test.run=^TestV2SDKBridge$', '-test.timeout=16m', '-test.v'], { env, stdio: ['ignore', 'pipe', 'pipe'] });
  let output = '';
  child.stdout.on('data', data => { output += data; });
  child.stderr.on('data', data => { output += data; });
  const exited = new Promise((resolve, reject) => {
    child.once('error', reject);
    child.once('exit', (code, signal) => resolve({ code, signal }));
  });
  let info;
  try {
    info = await eventually(async () => {
      if (child.exitCode !== null || child.signalCode !== null) throw new Error(`fixture exited: ${output}`);
      const value = JSON.parse(await readFile(join(directory, 'bridge.json')));
      return value.generation === generation && value;
    }, `fixture generation ${generation}`);
  } catch (error) {
    child.kill('SIGKILL'); await exited;
    await writeFile(join(directory, `process-${generation}.log`), output);
    throw new Error(`Cannot start ${binary}: ${output}`, { cause: error });
  }
  let closed = false;
  return {
    info, directory,
    async effects() {
      const response = await fetch(info.frontend + '/control/effects', { signal: AbortSignal.timeout(3000) });
      assert.ok(response.ok);
      return (await response.text()).trim().split('\n').filter(Boolean).map(JSON.parse);
    },
    async release(key) {
      const response = await fetch(info.frontend + '/control/release?key=' + encodeURIComponent(key), { method: 'POST', signal: AbortSignal.timeout(3000) });
      assert.ok(response.ok);
    },
    async close(crash = false) {
      if (closed) return;
      closed = true;
      if (crash) child.kill('SIGKILL');
      else await fetch(info.frontend + '/done', { method: 'POST', signal: AbortSignal.timeout(3000) }).catch(() => {});
      const timer = setTimeout(() => child.kill('SIGKILL'), 10_000);
      const result = await exited;
      clearTimeout(timer);
      await writeFile(join(directory, `process-${generation}.log`), output);
      assert.ok(!output.includes('WARNING: DATA RACE'), output);
      if (crash) assert.equal(result.signal, 'SIGKILL', output);
      else assert.deepEqual(result, { code: 0, signal: null }, output);
    },
  };
}
