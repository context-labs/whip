import { execFile } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { chmod, copyFile, lstat, mkdir, mkdtemp, readFile, rename, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';

const exec = promisify(execFile);
export interface RuntimeManifest {
  schema: 1;
  version: string;
  architecture: 'arm64';
  teamId?: string;
  rendererDigest: string;
  source: { commit: string; dirty: boolean; lockfile: string };
  compatibility: { protocolMajor: number; protocolMinor: number; schemaVersion: number };
  files: Record<'whip' | 'whip-computer', { bytes: number; sha256: string }>;
}
export interface RuntimeInstallation { executable: string; helper: string }
export interface DaemonStatus { state: 'running' | 'stopped' | 'unhealthy'; socket: string; error?: string; pid?: number; stale_socket?: boolean }

export async function run(executable: string, args: string[], env: NodeJS.ProcessEnv, signal: AbortSignal, timeout = 15_000) {
  signal.throwIfAborted();
  const result = await exec(executable, args, { env, signal, timeout, maxBuffer: 64 << 10, encoding: 'utf8' });
  return result.stdout;
}

export async function fileDigest(filename: string, signal?: AbortSignal) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(filename, { signal })) hash.update(chunk);
  signal?.throwIfAborted();
  return hash.digest('hex');
}

export async function readRuntimeManifest(filename: string): Promise<RuntimeManifest> {
  const stat = await lstat(filename);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.size > 16 << 10) throw new Error('Invalid packaged runtime manifest');
  const value = JSON.parse(await readFile(filename, 'utf8')) as RuntimeManifest;
  if (value.schema !== 1 || !/^[a-zA-Z0-9.+-]{1,128}$/.test(value.version) || value.architecture !== 'arm64' ||
      !/^[a-f0-9]{64}$/.test(value.rendererDigest) || (value.teamId !== undefined && !/^[A-Z0-9]{10}$/.test(value.teamId)) ||
      !/^[a-f0-9]{40}$/.test(value.source?.commit) || typeof value.source?.dirty !== 'boolean' ||
      !/^[a-f0-9]{64}$/.test(value.source?.lockfile) || !Number.isSafeInteger(value.compatibility?.protocolMajor) ||
      !Number.isSafeInteger(value.compatibility?.protocolMinor) || value.compatibility.protocolMinor < 0 ||
      value.compatibility.protocolMajor < 1 || !Number.isSafeInteger(value.compatibility?.schemaVersion) || value.compatibility.schemaVersion < 1 ||
      Object.keys(value.files ?? {}).sort().join(',') !== 'whip,whip-computer') throw new Error('Invalid packaged runtime manifest');
  for (const file of Object.values(value.files))
    if (!Number.isSafeInteger(file.bytes) || file.bytes < 1 || file.bytes > 512 << 20 || !/^[a-f0-9]{64}$/.test(file.sha256))
      throw new Error('Invalid packaged runtime file');
  return value;
}

export async function verifyRuntime(directory: string, manifest: RuntimeManifest, signal: AbortSignal) {
  const parent = await lstat(directory);
  if (!parent.isDirectory() || parent.isSymbolicLink()) throw new Error('Invalid runtime directory');
  for (const [name, expected] of Object.entries(manifest.files)) {
    signal.throwIfAborted();
    const filename = path.join(directory, name);
    const stat = await lstat(filename);
    if (!stat.isFile() || stat.isSymbolicLink() || stat.size !== expected.bytes || !(stat.mode & 0o111) ||
        await fileDigest(filename, signal) !== expected.sha256) throw new Error(`Packaged ${name} failed integrity verification`);
    if (manifest.teamId) await run('/usr/bin/codesign', ['--verify', '--strict', '-R',
      `=anchor apple generic and certificate leaf[subject.OU] = "${manifest.teamId}"`, filename], process.env, signal);
  }
  signal.throwIfAborted();
}

/** Never modify a retained installation: old daemons spawn workers using os.Executable(). */
export async function installRuntime(source: string, root: string, manifest: RuntimeManifest, signal: AbortSignal): Promise<RuntimeInstallation> {
  const identity = createHash('sha256').update(JSON.stringify(manifest)).digest('hex');
  const destination = path.join(root, `${manifest.version}-${identity}`);
  const paths = { executable: path.join(destination, 'whip'), helper: path.join(destination, 'whip-computer') };
  const verifyInstalled = async () => {
    const recorded = await readRuntimeManifest(path.join(destination, 'runtime-manifest.json'));
    if (JSON.stringify(recorded) !== JSON.stringify(manifest)) throw new Error('The retained runtime manifest changed');
    await verifyRuntime(destination, manifest, signal);
  };
  try {
    const rootStat = await lstat(root);
    if (!rootStat.isDirectory() || rootStat.isSymbolicLink()) throw new Error('Invalid retained-runtime directory');
  } catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error; }
  const exists = await lstat(destination).then(() => true, error => {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
    return false;
  });
  if (exists) { await verifyInstalled(); return paths; }
  await verifyRuntime(source, manifest, signal);
  await mkdir(root, { recursive: true, mode: 0o700 });
  const rootStat = await lstat(root);
  if (!rootStat.isDirectory() || rootStat.isSymbolicLink()) throw new Error('Invalid retained-runtime directory');
  const temporary = await mkdtemp(path.join(root, '.install-'));
  try {
    for (const name of Object.keys(manifest.files)) {
      signal.throwIfAborted();
      const target = path.join(temporary, name);
      await copyFile(path.join(source, name), target);
      await chmod(target, 0o700);
    }
    await writeFile(path.join(temporary, 'runtime-manifest.json'), JSON.stringify(manifest), { mode: 0o600, flag: 'wx' });
    await verifyRuntime(temporary, manifest, signal);
    signal.throwIfAborted();
    try { await rename(temporary, destination); }
    catch (error) {
      if (!['EEXIST', 'ENOTEMPTY'].includes((error as NodeJS.ErrnoException).code ?? '')) throw error;
      await verifyInstalled();
    }
    return paths;
  } finally { await rm(temporary, { recursive: true, force: true }); }
}

export function parseDaemonStatus(stdout: string): DaemonStatus {
  const value = JSON.parse(stdout) as DaemonStatus;
  if (!['running', 'stopped', 'unhealthy'].includes(value.state) || typeof value.socket !== 'string' ||
      !path.isAbsolute(value.socket) || Buffer.byteLength(value.socket) > 103 || /[\u0000-\u001f\u007f]/.test(value.socket) ||
      (value.pid !== undefined && (!Number.isSafeInteger(value.pid) || value.pid <= 0)) ||
      (value.stale_socket !== undefined && typeof value.stale_socket !== 'boolean') ||
      (value.error !== undefined && (typeof value.error !== 'string' || value.error.length > 8192)))
    throw new Error('Whip returned an invalid runtime status');
  return value;
}

/** Finder supplies a small PATH. Only extract PATH, never persist the shell environment. */
export async function runtimeEnvironment(signal: AbortSignal): Promise<NodeJS.ProcessEnv> {
  const env = { ...process.env };
  const shell = env.SHELL;
  if (shell && path.isAbsolute(shell)) {
    try {
      const output = await run(shell, ['-l', '-c', 'printf "\\0WHIP_PATH=%s\\0" "$PATH"'], env, signal, 3000);
      const match = /\x00WHIP_PATH=([^\x00]{1,16384})\x00/.exec(output);
      if (match && !/[\r\n]/.test(match[1]!)) env.PATH = match[1];
    } catch { signal.throwIfAborted(); }
  }
  env.PATH = [...new Set((env.PATH ?? '').split(':').filter(part => path.isAbsolute(part)))
    .values(), '/opt/homebrew/bin', '/usr/local/bin', '/usr/bin', '/bin', '/usr/sbin', '/sbin'].join(':');
  return env;
}

export async function prepareLocal(options: {
  source: string; retainedRoot: string; manifest: RuntimeManifest;
  signal: AbortSignal; progress(message: string): void;
}): Promise<string> {
  const { signal, progress, manifest } = options;
  progress('Looking for the local Whip runtime…');
  // Verify before executing, including when an existing daemon avoids installation.
  await verifyRuntime(options.source, manifest, signal);
  const bundled = path.join(options.source, 'whip');
  const env = await runtimeEnvironment(signal);
  const status = async (executable: string) => parseDaemonStatus(await run(executable, ['daemon', 'status', '--json'], env, signal));
  const existing = await status(bundled);
  if (existing.state === 'running') return existing.socket;
  if (existing.state === 'unhealthy' && !existing.stale_socket) throw new Error(`The local runtime needs attention: ${existing.error ?? 'unhealthy daemon'}. Inspect it with whip daemon status.`);
  progress('Installing the bundled Whip runtime…');
  const installed = await installRuntime(options.source, options.retainedRoot, manifest, signal);
  env.WHIP_COMPUTER_BIN = installed.helper;
  progress('Starting the local Whip runtime…');
  let startFailure: unknown;
  try { await run(installed.executable, ['daemon', 'start'], env, signal); }
  catch (error) { signal.throwIfAborted(); startFailure = error; }
  // A simultaneous CLI start can win with a different compatible build.
  const ready = await status(installed.executable);
  if (ready.state === 'running') return ready.socket;
  throw new Error(`Whip could not start: ${ready.error ?? (startFailure instanceof Error ? startFailure.message : 'readiness check failed')}`);
}
