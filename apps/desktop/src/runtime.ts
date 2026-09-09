import { execFile } from 'node:child_process';
import { createHash } from 'node:crypto';
import { constants, createReadStream } from 'node:fs';
import { homedir, userInfo } from 'node:os';
import type { LocalRuntimeStatus } from '@whip/app/platform';
import { access, chmod, copyFile, link, lstat, mkdir, mkdtemp, readFile, rename, rm, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';

const exec = promisify(execFile);
export interface RuntimeManifest {
  schema: 1;
  version: string;
  buildId: string;
  distribution: 'whipcode';
  architecture: 'arm64';
  teamId?: string;
  rendererDigest: string;
  source: { commit: string; dirty: boolean; lockfile: string };
  compatibility: { protocolMajor: number; protocolMinor: number; schemaVersion: number };
  files: Record<'whipcode' | 'whip-computer', { bytes: number; sha256: string }>;
}
export interface DaemonStatus { state: 'running' | 'stopped' | 'unhealthy'; socket: string; error?: string; pid?: number; stale_socket?: boolean; client_build?: string; daemon_build?: string; build_match?: boolean }

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
  if (value.schema !== 1 || value.distribution !== 'whipcode' || !/^[a-zA-Z0-9.+-]{1,128}$/.test(value.buildId) || !/^[a-zA-Z0-9.+-]{1,128}$/.test(value.version) || value.architecture !== 'arm64' ||
      !/^[a-f0-9]{64}$/.test(value.rendererDigest) || (value.teamId !== undefined && !/^[A-Z0-9]{10}$/.test(value.teamId)) ||
      !/^[a-f0-9]{40}$/.test(value.source?.commit) || typeof value.source?.dirty !== 'boolean' ||
      !/^[a-f0-9]{64}$/.test(value.source?.lockfile) || !Number.isSafeInteger(value.compatibility?.protocolMajor) ||
      !Number.isSafeInteger(value.compatibility?.protocolMinor) || value.compatibility.protocolMinor < 0 ||
      value.compatibility.protocolMajor < 1 || !Number.isSafeInteger(value.compatibility?.schemaVersion) || value.compatibility.schemaVersion < 1 ||
      Object.keys(value.files ?? {}).sort().join(',') !== 'whip-computer,whipcode') throw new Error('Invalid packaged runtime manifest');
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

// Keep this small allowlist aligned with config.ProviderPresets (checked by tests).
export const providerEnvironmentNames = ['INFERENCE_API_KEY', 'OPENROUTER_API_KEY'] as const;

/** Recover shell PATH and, for local launches only, supported provider keys. Never persist them. */
export async function runtimeEnvironment(signal: AbortSignal, providerKeys = false, inherited = process.env): Promise<NodeJS.ProcessEnv> {
  const env = { ...inherited };
  let shell = env.SHELL;
  if (!shell && providerKeys) {
    try { shell = userInfo().shell || '/bin/sh'; } catch { shell = '/bin/sh'; }
  }
  if (shell && path.isAbsolute(shell)) {
    try {
      const names = ['PATH', ...(providerKeys ? providerEnvironmentNames : [])];
      // Only fixed identifiers enter the command; values stay in quoted shell expansions.
      const command = `printf '\\0WHIP_ENV\\0'; ${names.map(name => `printf '%s\\0%s\\0' '${name}' "\${${name}-}"`).join('; ')}`;
      const output = await run(shell, [providerKeys ? '-il' : '-l', '-c', command], env, signal, 3000);
      const marker = '\x00WHIP_ENV\x00';
      const start = output.lastIndexOf(marker);
      if (start >= 0) {
        const fields = output.slice(start + marker.length).split('\0');
        for (let index = 0; index + 1 < fields.length; index += 2) {
          const name = fields[index]!; const value = fields[index + 1]!;
          if (!names.includes(name) || value.length > 16384 || /[\r\n]/.test(value)) continue;
          if (name === 'PATH' || (env[name] === undefined && value.trim())) env[name] = value;
        }
      }
    } catch { signal.throwIfAborted(); }
  }
  env.PATH = [...new Set((env.PATH ?? '').split(':').filter(part => path.isAbsolute(part)))
    .values(), '/opt/homebrew/bin', '/usr/local/bin', '/usr/bin', '/bin', '/usr/sbin', '/sbin'].join(':');
  return env;
}

type LocalRuntimeOptions = {
  source: string;
  manifest: RuntimeManifest;
  settingsFile: string;
  defaultExecutable?: string;
  env?: NodeJS.ProcessEnv;
  confirmUpdate?: (message: string) => Promise<boolean>;
};

type ManagedRuntime = { sha256: string; channel: 'stable' | 'beta'; approvedVersion?: string };
type RuntimeSettings = { executable: string; managed?: ManagedRuntime };

function absolutePath(value: unknown): string {
  if (typeof value !== 'string' || !path.isAbsolute(value) || Buffer.byteLength(value) > 2048 || /[\u0000-\u001f\u007f]/.test(value))
    throw new Error('Choose an absolute executable or home path without control characters.');
  return path.normalize(value);
}

/** One installed executable owns local work. Packaged bytes are an explicit install payload. */
export class LocalRuntime {
  private busy = false;
  private mutationDone?: Promise<void>;
  private synchronizing?: Promise<void>;
  private readonly options: LocalRuntimeOptions;
  constructor(options: LocalRuntimeOptions) { this.options = options; }

  private async environment(signal: AbortSignal) {
    signal.throwIfAborted();
    const env = this.options.env ? { ...this.options.env } : await runtimeEnvironment(signal, true);
    env.WHIPCODE_HOME = absolutePath(env.WHIPCODE_HOME || path.join(env.HOME || homedir(), '.whipcode'));
    // A stable loopback endpoint also makes CLI/web usable when desktop starts first.
    env.WHIPCODE_LISTEN ??= '127.0.0.1:8080';
    delete env.WHIP_HOME;
    delete env.WHIP_COMPUTER_BIN; // The installed distribution extracts its matching embedded helper.
    return env;
  }

  private async settings(): Promise<RuntimeSettings | undefined> {
    try {
      const info = await lstat(this.options.settingsFile);
      if (!info.isFile() || info.size > 4096) throw new Error('Invalid local runtime settings. Choose the executable again.');
      const value = JSON.parse(await readFile(this.options.settingsFile, 'utf8')) as RuntimeSettings;
      value.executable = absolutePath(value.executable);
      if (value.managed && (!/^[a-f0-9]{64}$/.test(value.managed.sha256) || !['stable', 'beta'].includes(value.managed.channel) ||
          (value.managed.approvedVersion !== undefined && !/^[a-zA-Z0-9.+-]{1,128}$/.test(value.managed.approvedVersion))))
        throw new Error('Invalid managed backend settings. Choose or reinstall whipcode.');
      return value;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
    }
  }

  private async selected(env: NodeJS.ProcessEnv): Promise<string | undefined> {
    const saved = await this.settings();
    if (saved) return saved.executable;
    if (this.options.defaultExecutable) return absolutePath(this.options.defaultExecutable);
    const dirs = [...new Set([...(env.PATH || '').split(':'), '/usr/local/bin', '/opt/homebrew/bin', path.join(env.HOME || homedir(), '.local/bin')])];
    for (const dir of dirs.filter(dir => path.isAbsolute(dir))) {
      const candidate = path.join(dir, 'whipcode');
      if (await stat(candidate).then(info => info.isFile(), () => false)) return candidate;
    }
    return undefined;
  }

  private async save(executable: string, signal: AbortSignal, managed?: ManagedRuntime | null) {
    signal.throwIfAborted();
    if (managed === undefined) {
      const previous = await this.settings();
      if (previous?.executable === executable) managed = previous.managed;
    }
    const directory = path.dirname(this.options.settingsFile);
    await mkdir(directory, { recursive: true, mode: 0o700 });
    const temporary = await mkdtemp(path.join(directory, '.local-runtime-'));
    try {
      const filename = path.join(temporary, 'settings.json');
      await writeFile(filename, JSON.stringify({ executable, ...(managed ? { managed } : {}) }) + '\n', { mode: 0o600, flag: 'wx' });
      signal.throwIfAborted();
      await rename(filename, this.options.settingsFile);
    } finally { await rm(temporary, { recursive: true, force: true }); }
  }

  private async metadata(executable: string, env: NodeJS.ProcessEnv, signal: AbortSignal) {
    if (!(await stat(executable)).isFile()) throw new Error('The selected path is not an executable file.');
    await access(executable, constants.X_OK);
    let info: { distribution?: string; buildId?: string; protocolMajor?: number; protocolMinor?: number; schemaVersion?: number };
    try { info = JSON.parse(await run(executable, ['_desktop-runtime-info'], env, signal, 5000)); }
    catch { signal.throwIfAborted(); throw new Error('The executable did not return valid whipcode build information within 5 seconds. Choose a current whipcode build.'); }
    const compatibility = this.options.manifest.compatibility;
    if (info.distribution !== 'whipcode') throw new Error('This executable is not a whipcode distribution. Choose whipcode, or install the packaged build.');
    if (typeof info.buildId !== 'string' || !/^[a-zA-Z0-9.+-]{1,128}$/.test(info.buildId) ||
        info.protocolMajor !== compatibility.protocolMajor || !Number.isSafeInteger(info.protocolMinor) ||
        info.protocolMinor! < compatibility.protocolMinor || info.schemaVersion !== compatibility.schemaVersion)
      throw new Error('This whipcode build is incompatible with this desktop app. Install a matching backend build before connecting.');
    return info as typeof info & { buildId: string };
  }

  private async probe(env: NodeJS.ProcessEnv, executable: string | undefined, signal: AbortSignal): Promise<LocalRuntimeStatus & { socket?: string; stale?: boolean }> {
    const base = { executable, home: env.WHIPCODE_HOME!, canInstall: false };
    if (!executable || !await stat(executable).then(() => true, error => {
      if (error.code === 'ENOENT') return false;
      throw new Error('Cannot read the selected whipcode executable. Check its permissions or choose another path.');
    })) return { ...base, state: 'missing', canInstall: true, message: 'whipcode is not installed at the selected path. Install it or choose an existing executable.' };
    let clientBuild: string;
    try { clientBuild = (await this.metadata(executable, env, signal)).buildId; }
    catch (error) {
      signal.throwIfAborted();
      const denied = (error as NodeJS.ErrnoException).code === 'EACCES';
      return { ...base, state: 'incompatible', message: denied ? 'The selected file is not executable. Check its permissions or choose another whipcode executable.' : (error as Error).message };
    }
    try {
      const status = parseDaemonStatus(await run(executable, ['daemon', 'status', '--json'], env, signal, 5000));
      const daemonBuild = typeof status.daemon_build === 'string' ? status.daemon_build.slice(0, 128) : undefined;
      const message = status.state === 'running'
        ? `Connected to the local daemon.${daemonBuild !== clientBuild ? ' The running daemon uses a different build; an explicit restart will use the selected executable.' : ''}`
        : status.state === 'stopped' ? 'whipcode is installed. Connect This Mac to start its daemon.'
          : status.stale_socket ? 'The previous daemon left a stale socket. Connect This Mac to recover it.'
            : 'The local daemon is unhealthy or incompatible. Restart it explicitly, or inspect it with whipcode daemon status.';
      return { ...base, state: status.state, clientBuild, daemonBuild, message, socket: status.socket, stale: status.stale_socket };
    } catch {
      signal.throwIfAborted();
      return { ...base, state: 'unhealthy', clientBuild, message: 'The daemon status check failed or timed out after 5 seconds. Inspect whipcode daemon status, then retry.' };
    }
  }

  async test(signal: AbortSignal): Promise<LocalRuntimeStatus> {
    const env = await this.environment(signal);
    const { socket: _socket, stale: _stale, ...status } = await this.probe(env, await this.selected(env), signal);
    return status;
  }

  private async mutation<T>(action: () => Promise<T>): Promise<T> {
    if (this.busy) throw new Error('Another local runtime operation is in progress. Wait for it to finish, then retry.');
    this.busy = true;
    let done!: () => void;
    this.mutationDone = new Promise(resolve => { done = resolve; });
    try { return await action(); } finally { this.busy = false; this.mutationDone = undefined; done(); }
  }

  async choose(executable: string, signal: AbortSignal): Promise<LocalRuntimeStatus> {
    return this.mutation(async () => {
      const env = await this.environment(signal);
      executable = absolutePath(executable);
      await this.metadata(executable, env, signal);
      signal.throwIfAborted();
      await this.save(executable, signal, null);
      return this.test(signal);
    });
  }

  async executable(signal: AbortSignal): Promise<string> {
    const env = await this.environment(signal);
    const executable = await this.selected(env);
    if (!executable) throw new Error('Install or choose whipcode in Execution hosts → This Mac before connecting.');
    await this.metadata(executable, env, signal);
    return executable;
  }

  async install(executable: string, signal: AbortSignal): Promise<LocalRuntimeStatus> {
    return this.mutation(async () => {
      executable = absolutePath(executable);
      const env = await this.environment(signal);
      await verifyRuntime(this.options.source, this.options.manifest, signal);
      const source = path.join(this.options.source, 'whipcode');
      if ((await this.metadata(source, env, signal)).buildId !== this.options.manifest.buildId)
        throw new Error('The packaged whipcode build identity does not match its manifest. Reinstall the desktop app.');
      const expected = this.options.manifest.files.whipcode;
      const existing = await lstat(executable).catch(error => { if (error.code !== 'ENOENT') throw error; });
      if (existing) {
        if (!existing.isFile() || await fileDigest(executable, signal) !== expected.sha256)
          throw new Error('An installation already exists at this path. Stop its daemon and explicitly replace it before upgrading; desktop will not overwrite it.');
        await this.metadata(executable, env, signal);
      } else {
        const directory = path.dirname(executable);
        await mkdir(directory, { recursive: true, mode: 0o755 });
        if (!(await lstat(directory)).isDirectory()) throw new Error('The installation directory must be a real directory, not a symlink.');
        const temporary = await mkdtemp(path.join(directory, '.whipcode-install-'));
        try {
          const target = path.join(temporary, 'whipcode');
          await copyFile(source, target, constants.COPYFILE_EXCL);
          await chmod(target, 0o755);
          if (await fileDigest(target, signal) !== expected.sha256) throw new Error('The copied whipcode payload failed integrity verification.');
          signal.throwIfAborted();
          // Exclusive publication cannot overwrite a running or concurrently installed executable.
          await link(target, executable);
        } finally { await rm(temporary, { recursive: true, force: true }); }
      }
      await this.save(executable, signal, { sha256: expected.sha256, channel: this.channel });
      return this.test(signal);
    }).catch(error => {
      if (['EACCES', 'EPERM'].includes((error as NodeJS.ErrnoException).code || ''))
        throw new Error('Cannot install whipcode here. Choose a writable executable location, such as ~/.local/bin/whipcode, or install it through your terminal.');
      throw error;
    });
  }

  private get channel(): 'stable' | 'beta' { return this.options.manifest.version.includes('-') ? 'beta' : 'stable'; }

  async approveUpdate(version: string | undefined, signal: AbortSignal) {
    if (version !== undefined && !/^[a-zA-Z0-9.+-]{1,128}$/.test(version)) throw new Error('Invalid update version');
    if (this.synchronizing) await this.synchronizing;
    return this.mutation(async () => {
      const saved = await this.settings();
      if (saved?.managed) await this.save(saved.executable, signal, { ...saved.managed, approvedVersion: version });
    });
  }

  async synchronize(signal: AbortSignal, progress: (message: string) => void = () => {}) {
    if (this.synchronizing) return this.synchronizing;
    this.synchronizing = (async () => {
      // Startup and host restoration may request synchronization together, or
      // restoration may already be starting the verified executable. Wait for
      // that mutation instead of turning routine startup into an error dialog.
      while (this.mutationDone) await this.mutationDone;
      signal.throwIfAborted();
      return this.mutation(async () => {
        const saved = await this.settings();
        if (!saved?.managed) return;
        if (saved.managed.channel !== this.channel) throw new Error('This backend belongs to another Whip release channel. Switch its installation explicitly before connecting.');
        const expected = this.options.manifest.files.whipcode.sha256;
        const actual = await fileDigest(saved.executable, signal).catch(error => {
          if (error.code === 'ENOENT') throw new Error('whipcode is not installed at the saved path. Install it again before connecting.');
          throw error;
        });
        if (actual !== saved.managed.sha256 && actual !== expected)
          throw new Error('whipcode was changed outside desktop. Choose that executable to manage it externally, or explicitly reinstall Whip.');
        const env = await this.environment(signal);
        const completed = { sha256: expected, channel: this.channel,
          ...(saved.managed.approvedVersion !== this.options.manifest.version ? { approvedVersion: saved.managed.approvedVersion } : {}) };
        if (actual === expected) {
          const status = await this.probe(env, saved.executable, signal);
          if (status.state === 'stopped' || status.stale || (status.state === 'running' && status.daemonBuild === this.options.manifest.buildId)) {
            await this.save(saved.executable, signal, completed);
            return;
          }
        }
        progress('Verifying the Whip backend update…');
        await verifyRuntime(this.options.source, this.options.manifest, signal);
        const source = path.join(this.options.source, 'whipcode');
        const args = ['_desktop-runtime-sync', '--executable', saved.executable, '--expected-sha256', saved.managed.sha256, '--sha256', expected];
        const apply = async (interrupt: boolean) => {
          const result = JSON.parse(await run(source, [...args, ...(interrupt ? ['--interrupt'] : [])], env, signal, 45_000));
          if (!['ready', 'approval-required'].includes(result.state) || result.buildId !== this.options.manifest.buildId || result.executable !== saved.executable)
            throw new Error('The backend updater returned an invalid result. Retry the update.');
          return result.state as 'ready' | 'approval-required';
        };
        let result = await apply(saved.managed.approvedVersion === this.options.manifest.version);
        if (result === 'approval-required') {
          const approved = await this.options.confirmUpdate?.(`Whip ${this.options.manifest.version} needs to update its local backend. Restarting interrupts work running through desktop, terminal, web, or mobile. Sessions and configuration remain on disk.`);
          signal.throwIfAborted();
          if (!approved) throw new Error('Backend update deferred. Running work continues. Connect This Mac again when you are ready to restart and update.');
          await this.save(saved.executable, signal, { ...saved.managed, approvedVersion: this.options.manifest.version });
          progress('Updating and restarting the local Whip backend…');
          result = await apply(true);
        }
        if (result !== 'ready') throw new Error('The backend update still requires approval. Retry the update.');
        await this.save(saved.executable, signal, completed);
        progress('The Whip app and backend are up to date.');
      });
    })();
    try { await this.synchronizing; } finally { this.synchronizing = undefined; }
  }

  async prepare(signal: AbortSignal, progress: (message: string) => void): Promise<string> {
    await this.synchronize(signal, progress);
    return this.mutation(async () => {
      progress('Locating the canonical whipcode executable…');
      const env = await this.environment(signal);
      const executable = await this.selected(env);
      progress('Checking the installation and contacting the daemon…');
      const status = await this.probe(env, executable, signal);
      if (!executable || status.state === 'missing' || status.state === 'incompatible' || (status.state === 'unhealthy' && !status.stale))
        throw new Error(status.message + ' Open Execution hosts → This Mac for connection diagnostics.');
      await this.save(executable, signal);
      if (status.state === 'running') { progress('Attaching to the local daemon…'); return status.socket!; }
      progress('Starting the canonical whipcode daemon…');
      let failure = '';
      try { await run(executable, ['daemon', 'start'], env, signal); }
      catch (error) { signal.throwIfAborted(); failure = startupFailure(error); }
      const ready = await this.probe(env, executable, signal);
      if (ready.state !== 'running') throw new Error(failure || ready.message);
      progress('Attaching to the local daemon…');
      return ready.socket!;
    });
  }

  async restart(signal: AbortSignal): Promise<LocalRuntimeStatus> {
    return this.mutation(async () => {
      const env = await this.environment(signal);
      const executable = await this.executable(signal);
      try { await run(executable, ['daemon', 'restart'], env, signal, 25_000); }
      catch (error) { signal.throwIfAborted(); throw new Error(startupFailure(error)); }
      return this.test(signal);
    });
  }
}

function startupFailure(error: unknown): string {
  const detail = String((error as { stderr?: string }).stderr || '');
  if (/address already in use/i.test(detail)) return 'whipcode could not start because its network port is already in use. Stop the conflicting listener or choose a different WHIPCODE_LISTEN, then retry.';
  if (/permission denied/i.test(detail)) return 'whipcode could not start because access was denied. Check executable and home-directory permissions, then retry.';
  return 'whipcode could not start or restart within the allowed time. Inspect whipcode daemon status and daemon logs for this home, then retry. No alternate backend was started.';
}
