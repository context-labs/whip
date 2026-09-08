import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { mkdir, mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

export const repository = fileURLToPath(new URL('../../../', import.meta.url));

export function fixtureExternalOrigin(value) {
  if (value === undefined) return undefined;
  try {
    if (typeof value !== 'string' || value.length > 2048) throw new TypeError();
    const parsed = new URL(value);
    if (parsed.protocol !== 'https:' || value !== parsed.origin || parsed.hostname.includes('*')) throw new TypeError();
    return value;
  } catch {
    throw new TypeError('externalOrigin must be an exact HTTPS origin without credentials, path, query, fragment or wildcards');
  }
}

export async function run(command, args, options = {}) {
  const child = spawn(command, args, { cwd: repository, stdio: 'inherit', ...options });
  const [code, signal] = await once(child, 'exit');
  if (code !== 0) throw new Error(`${command} exited with ${code ?? signal}`);
}

export async function eventually(check, { timeout = 15_000, interval = 25, description = 'condition' } = {}) {
  const deadline = Date.now() + timeout;
  let last;
  while (Date.now() < deadline) {
    try {
      const value = await check();
      if (value) return value;
    } catch (error) { last = error; }
    await new Promise(resolve => setTimeout(resolve, interval));
  }
  throw new Error(`Timed out waiting for ${description}`, { cause: last });
}

// Every daemon launched here owns an isolated home and is attach-only from the
// SDK's perspective. Keeping the binary lets restart tests bypass the Go runner
// and kill the actual daemon process, including all outstanding execution.
// Manual native acceptance may extend the four-minute default, up to 30 minutes.
export async function startFixture({ lifetimeMs = 4 * 60_000, externalOrigin } = {}) {
  if (!Number.isInteger(lifetimeMs) || lifetimeMs <= 0 || lifetimeMs > 30 * 60_000) {
    throw new RangeError('Fixture lifetimeMs must be a positive integer no greater than 1800000 (30 minutes)');
  }
  externalOrigin = fixtureExternalOrigin(externalOrigin);
  const directory = await mkdtemp(join(tmpdir(), 'whip-sdk-'));
  const binary = join(directory, 'daemon.test');
  await mkdir(join(directory, 'public'));
  let child;
  let exit;
  let info;
  let output = '';
  let finished = false;
  const start = async () => {
    const generation = (info?.generation ?? 0) + 1;
    child = spawn(binary, ['-test.run=^TestV2SDKBridge$', `-test.timeout=${lifetimeMs + 60_000}ms`, '-test.v'], {
      cwd: repository,
      env: {
        ...process.env,
        WHIP_HOME: join(directory, 'home'),
        WHIP_SDK_FIXTURE_DIR: directory,
        WHIP_SDK_FIXTURE_LIFETIME: `${lifetimeMs}ms`,
        WHIP_SDK_FIXTURE_ORIGIN: externalOrigin ?? '',
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    child.stdout.on('data', data => { output += data; });
    child.stderr.on('data', data => { output += data; });
    exit = once(child, 'exit');
    info = await eventually(async () => {
      if (child.exitCode !== null) throw new Error(`Fixture exited: ${output}`);
      const candidate = JSON.parse(await readFile(join(directory, 'bridge.json'), 'utf8'));
      if (candidate.generation !== generation) return false;
      return candidate;
    }, { timeout: 30_000, description: `daemon generation ${generation}` });
  };
  const close = async () => {
    if (finished) return;
    finished = true;
    try {
      if (child && child.exitCode === null && child.signalCode === null) {
        await fetch(info.frontend + '/done', { method: 'POST', signal: AbortSignal.timeout(3_000) }).catch(() => {});
        const timer = setTimeout(() => child.kill('SIGKILL'), 5_000);
        await exit;
        clearTimeout(timer);
      }
      const [code, signal] = await exit;
      if (code !== 0 || output.includes('WARNING: DATA RACE')) throw new Error(`SDK fixture failed (${code ?? signal}): ${output}`);
    } finally {
      if (process.env.WHIP_SDK_KEEP_FIXTURE) console.log(`SDK fixture retained: ${directory}`);
      else await rm(directory, { recursive: true, force: true });
    }
  };
  try {
    await run('go', ['test', '-c', ...(process.env.WHIP_SDK_RACE ? ['-race'] : []), '-tags=integration', '-o', binary, './internal/daemon']);
    await start();
  } catch (error) {
    if (child) child.kill('SIGKILL');
    await rm(directory, { recursive: true, force: true });
    throw new Error(`Cannot start SDK fixture: ${output}`, { cause: error });
  }
  return {
    get info() { return info; },
    get exited() { return exit; },
    directory,
    get output() { return output; },
    async crashAndRestart() {
      child.kill('SIGKILL');
      const [, signal] = await exit;
      if (signal !== 'SIGKILL') throw new Error(`Fixture did not crash: ${output}`);
      await start();
      return info;
    },
    async release(key) {
      const response = await fetch(info.frontend + '/control/release?key=' + encodeURIComponent(key), { method: 'POST' });
      if (!response.ok) throw new Error(`Release failed: ${response.status}`);
    },
    async effects() {
      const response = await fetch(info.frontend + '/control/effects');
      const text = await response.text();
      return text.trim() ? text.trim().split('\n').map(line => JSON.parse(line)) : [];
    },
    close,
  };
}
