import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { chmod, lstat, mkdtemp, rm } from 'node:fs/promises';
import { createServer, type Socket } from 'node:net';
import path from 'node:path';
import { StringDecoder } from 'node:string_decoder';
import { setTimeout as delay } from 'node:timers/promises';
import type { ConnectionTarget } from '@whip/app/platform';
import type { HostPrompt } from '@whip/app/desktop-bridge';
import { parseDaemonStatus } from './runtime';

type Target = Extract<ConnectionTarget, { kind: 'ssh' }>;
type Prompt = (prompt: Omit<HostPrompt, 'id' | 'attemptId'>, signal: AbortSignal) => Promise<string[] | null>;
interface Options {
  target: Target; executable: string; env: NodeJS.ProcessEnv; signal: AbortSignal;
  progress(message: string): void; prompt: Prompt;
}
interface Job { done: Promise<{ stdout: string; stderr: string; code: number }>; cancel(): void; exited: boolean }

export function shellQuote(value: string): string {
  if (!value || /[\u0000-\u001f\u007f]/.test(value)) throw new Error('Invalid remote shell argument');
  return `'${value.replace(/'/g, `'\\''`)}'`;
}
export function sshArguments(target: Target): string[] {
  const args = ['-T', '-o', 'ConnectTimeout=15', '-o', 'ServerAliveInterval=15', '-o', 'ServerAliveCountMax=3',
    '-o', 'ExitOnForwardFailure=yes', '-o', 'ForwardAgent=no', '-o', 'RemoteCommand=none'];
  if (target.user) args.push('-l', target.user);
  if (target.port) args.push('-p', String(target.port));
  if (target.identityFile) args.push('-i', target.identityFile);
  return args;
}

/** One selected profile owns establishment; SDK reconnects reuse its active lease. */
export class SSHConnection {
  private controller = new AbortController();
  private signal: AbortSignal;
  private jobs = new Set<Job>();
  private starting?: Promise<string>;
  private master?: Job;
  private socket?: string;
  private authenticated = false;
  private authenticationRequired = false;
  private cleanupAttempt?: () => Promise<void>;
  constructor(private options: Options) {
    this.signal = AbortSignal.any([options.signal, this.controller.signal]);
    this.signal.addEventListener('abort', () => { void this.dispose().catch(() => {}); }, { once: true });
  }
  async getSocket(): Promise<string> {
    this.signal.throwIfAborted();
    if (this.authenticationRequired) throw new Error('SSH authentication is needed. Reconnect this host to continue.');
    if (this.master && !this.master.exited && this.socket) return this.socket;
    if (this.starting) return this.starting;
    this.starting = this.establish();
    try { return await this.starting; } finally { this.starting = undefined; }
  }

  private job(args: string[], env: NodeJS.ProcessEnv, signal: AbortSignal): Job {
    signal.throwIfAborted();
    const child = spawn(this.options.executable, ['_desktop-ssh', ...args], { env, stdio: ['pipe', 'pipe', 'pipe'] });
    const job: Job = { exited: false, done: undefined!, cancel: () => { child.stdin.end(); } };
    const abort = () => job.cancel();
    signal.addEventListener('abort', abort, { once: true });
    this.jobs.add(job);
    job.done = new Promise((resolve, reject) => {
      let stdout = ''; let stderr = ''; let bytes = 0; let failure: Error | undefined;
      const out = new StringDecoder('utf8'); const err = new StringDecoder('utf8');
      const receive = (data: Buffer, error: boolean) => {
        bytes += data.length;
        if (bytes > 128 << 10) { failure = new Error('SSH output exceeded its limit'); job.cancel(); return; }
        if (error) stderr += err.write(data); else stdout += out.write(data);
      };
      child.stdout.on('data', data => receive(data, false)); child.stderr.on('data', data => receive(data, true));
      child.stdin.on('error', () => {});
      child.once('error', reject);
      child.once('close', code => {
        stdout += out.end(); stderr += err.end();
        job.exited = true; this.jobs.delete(job); signal.removeEventListener('abort', abort);
        if (failure) reject(failure); else if (signal.aborted) reject(signal.reason);
        else resolve({ stdout, stderr, code: code ?? 1 });
      });
    });
    // Long-lived masters are observed by establishment and getSocket, never a
    // detached rejecting promise when cancellation arrives between SDK attempts.
    void job.done.catch(() => {});
    return job;
  }

  private async establish(): Promise<string> {
    await this.cleanupAttempt?.(); this.cleanupAttempt = undefined;
    this.signal.throwIfAborted();
    const { target, progress } = this.options;
    if (target.remoteHome && !path.posix.isAbsolute(target.remoteHome)) throw new Error('Remote Whip home must be an absolute path');
    if (target.remoteExecutable && !path.posix.isAbsolute(target.remoteExecutable) && !/^[a-zA-Z0-9_.-]+$/.test(target.remoteExecutable))
      throw new Error('Remote Whip executable must be an absolute path or command name');
    // Short path is intentional: macOS Unix socket addresses are limited to 104 bytes.
    const directory = await mkdtemp('/tmp/whip-ssh-'); await chmod(directory, 0o700);
    const control = path.join(directory, 'control'); const socket = path.join(directory, 'daemon');
    const promptSocket = path.join(directory, 'prompt'); const token = randomBytes(32).toString('hex');
    const attempt = new AbortController(); const signal = AbortSignal.any([this.signal, attempt.signal]);
    const clients = new Set<Socket>(); let pendingPrompt = false;
    let machineDeadline = Date.now() + 30_000;
    const server = createServer(client => {
      if (clients.size >= 2) { client.destroy(); return; }
      clients.add(client); client.once('close', () => clients.delete(client)); client.on('error', () => {});
      const clientLifetime = new AbortController();
      client.once('close', () => clientLifetime.abort());
      let input = ''; let bytes = 0; let received = false;
      const decoder = new StringDecoder('utf8');
      client.setTimeout(5000, () => client.destroy());
      client.on('data', chunk => {
        if (received) { client.destroy(); return; }
        bytes += chunk.length;
        if (bytes > 16 << 10) { client.destroy(); return; }
        input += decoder.write(chunk);
        if (!input.includes('\n')) return;
        received = true;
        let request: { token: string; prompt: string; confirm: boolean };
        try {
          request = JSON.parse(input);
          if (request.token !== token || typeof request.prompt !== 'string' || request.prompt.length > 8192 ||
              typeof request.confirm !== 'boolean' || pendingPrompt || this.authenticated) throw new Error('Invalid prompt');
        } catch { client.end(JSON.stringify({ cancel: true }) + '\n'); return; }
        pendingPrompt = true; client.setTimeout(0);
        const started = Date.now();
        progress(request.confirm ? 'Verify this SSH host…' : 'SSH authentication is required…');
        const lifetime = AbortSignal.any([signal, clientLifetime.signal, AbortSignal.timeout(10 * 60_000)]);
        void this.options.prompt({ title: request.confirm ? 'Verify SSH host' : `Authenticate ${target.host}`,
          message: request.prompt, fields: request.confirm ? [] : [{ label: 'Response', secret: true }],
          confirmLabel: request.confirm ? 'Trust this host' : 'Continue' }, lifetime).then(values => {
          if (values === null) {
            if (!client.destroyed) client.end(JSON.stringify({ cancel: true }) + '\n');
            attempt.abort(new Error('SSH authentication cancelled')); return;
          }
          if (client.destroyed) return;
          const answer = values === null ? undefined : request.confirm ? 'yes' : values[0];
          if (answer === undefined || /[\u0000-\u001f\u007f]/.test(answer) || Buffer.byteLength(answer) > 4096)
            client.end(JSON.stringify({ cancel: true }) + '\n');
          else client.end(JSON.stringify({ answer }) + '\n');
        }, () => {
          if (!client.destroyed) client.end(JSON.stringify({ cancel: true }) + '\n');
          attempt.abort(new Error('SSH authentication cancelled'));
        }).finally(() => {
          pendingPrompt = false; machineDeadline += Date.now() - started;
        });
      });
    });
    server.on('error', () => {});
    const env = { ...this.options.env, SSH_ASKPASS: this.options.executable, SSH_ASKPASS_REQUIRE: 'force',
      DISPLAY: this.options.env.DISPLAY || 'whip:0', WHIP_DESKTOP_PROMPT_SOCKET: promptSocket, WHIP_DESKTOP_PROMPT_TOKEN: token };
    const args = [...sshArguments(target), '-S', control];
    let cleaning: Promise<void> | undefined;
    this.cleanupAttempt = () => cleaning ??= (async () => {
      attempt.abort();
      for (const client of clients) client.destroy(); server.close();
      await Promise.allSettled([...this.jobs].map(job => job.done));
      await rm(directory, { recursive: true, force: true });
    })();
    const command = async (additional: string[], timeout = 15_000) => {
      // All follow-up commands address this already authenticated control socket.
      // Do not inherit unrelated user forwards or fall back to a second connection.
      const job = this.job(['-F', '/dev/null', '-S', control, '-o', 'ProxyCommand=false', '-T', ...additional], env,
        AbortSignal.any([signal, AbortSignal.timeout(timeout)]));
      const result = await job.done;
      if (result.code !== 0) throw new Error(`SSH failed: ${result.stderr.slice(-2048) || `exit ${result.code}`}`);
      return result.stdout;
    };
    try {
      signal.throwIfAborted();
      await new Promise<void>((resolve, reject) => {
        const abort = () => reject(signal.reason);
        signal.addEventListener('abort', abort, { once: true });
        server.once('error', reject);
        server.listen({ path: promptSocket, signal }, () => {
          server.removeListener('error', reject); signal.removeEventListener('abort', abort); resolve();
        });
      });
      await chmod(promptSocket, 0o600);
      progress(this.authenticated ? `Reconnecting to ${target.host}…` : `Connecting to ${target.host}…`);
      this.master = this.job([...args, '-o', 'ControlMaster=yes', '-o', 'ControlPersist=no', '-o', 'StrictHostKeyChecking=ask',
        '-o', 'ClearAllForwardings=yes', '-o', `BatchMode=${this.authenticated ? 'yes' : 'no'}`, '-N', target.host], env, signal);
      while (true) {
        signal.throwIfAborted();
        if (this.master.exited) {
          const result = await this.master.done;
          if (this.authenticated && /Permission denied|Host key verification failed/i.test(result.stderr)) this.authenticationRequired = true;
          throw new Error(`SSH connection failed: ${result.stderr.slice(-2048) || 'connection closed'}`);
        }
        if (!pendingPrompt && Date.now() > machineDeadline) throw new Error('SSH connection timed out');
        if (await lstat(control).then(stat => stat.isSocket(), () => false)) break;
        await delay(100, undefined, { signal });
      }
      await command(['-O', 'check', target.host]);
      const marker = randomBytes(16).toString('hex');
      const remote = async (body: string, timeout?: number) => {
        const script = `printf '\\nWHIP_BEGIN_${marker}\\n'; ${body}; whip_desktop_status=$?; printf '\\nWHIP_END_${marker}:%s\\n' "$whip_desktop_status"`;
        const output = await command(['-o', 'BatchMode=yes', '-o', 'ControlMaster=no', target.host, script], timeout);
        const begin = output.indexOf(`WHIP_BEGIN_${marker}\n`); const end = output.lastIndexOf(`\nWHIP_END_${marker}:`);
        if (begin < 0 || end < begin || output.slice(end).trim() !== `WHIP_END_${marker}:0`)
          throw new Error('The remote Whip command failed. Check its executable, home and version.');
        return output.slice(begin + `WHIP_BEGIN_${marker}\n`.length, end).trim();
      };
      progress('Finding Whip on the SSH host…');
      const executable = target.remoteExecutable ?? await remote('PATH="$PATH:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin"; command -v whip');
      if (!path.posix.isAbsolute(executable) && !/^[a-zA-Z0-9_.-]+$/.test(executable)) throw new Error('Whip is not installed on this SSH host; provide its absolute executable path');
      // The explicit remote home applies to either installed distribution.
      const program = `${target.remoteHome ? `WHIP_HOME=${shellQuote(target.remoteHome)} WHIPCODE_HOME=${shellQuote(target.remoteHome)} ` : ''}${shellQuote(executable)}`;
      let status = parseDaemonStatus(await remote(`${program} daemon status --json`));
      if (status.state === 'unhealthy' && !status.stale_socket) throw new Error(`The remote runtime needs attention: ${status.error ?? 'unhealthy daemon'}`);
      if (status.state === 'stopped' || status.stale_socket) {
        progress('Starting the installed remote Whip daemon…');
        let failure: unknown;
        try { await remote(`${program} daemon start`, 30_000); } catch (error) { failure = error; }
        status = parseDaemonStatus(await remote(`${program} daemon status --json`));
        if (status.state !== 'running') throw failure ?? new Error('The remote daemon did not become ready');
      }
      if (status.socket.includes(':')) throw new Error('The remote socket path cannot contain a colon');
      progress('Opening the SSH connection to Whip…');
      await command(['-O', 'forward', '-L', `${socket}:${status.socket}`, target.host]);
      if (!(await lstat(socket)).isSocket()) throw new Error('SSH did not create the Whip socket');
      this.socket = socket; this.authenticated = true;
      return socket;
    } catch (error) {
      await this.cleanupAttempt(); this.socket = undefined; this.master = undefined;
      throw error;
    }
  }
  async dispose() {
    if (!this.controller.signal.aborted) this.controller.abort();
    for (const job of this.jobs) job.cancel();
    await this.cleanupAttempt?.();
  }
}
