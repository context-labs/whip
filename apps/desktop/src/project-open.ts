import { execFile } from 'node:child_process';
import { stat } from 'node:fs/promises';
import { homedir } from 'node:os';
import path from 'node:path';
import { promisify } from 'node:util';
import { pathToFileURL } from 'node:url';
import { randomUUID } from 'node:crypto';
import { createWhipClient } from '@whip/sdk/node';
import type { TransportFactory } from '@whip/sdk';
import type { ConnectionTarget, OpenProjectRequest, ProjectEditor, ProjectEditorID } from '@whip/app/platform';

const execute = promisify(execFile);
const applications = {
  cursor: { label: 'Cursor', bundle: 'Cursor.app', bundleID: 'com.todesktop.230313mzl4w4u92', cli: 'Contents/Resources/app/bin/cursor' },
  vscode: { label: 'VS Code', bundle: 'Visual Studio Code.app', bundleID: 'com.microsoft.VSCode', cli: 'Contents/Resources/app/bin/code' },
  zed: { label: 'Zed', bundle: 'Zed.app', bundleID: 'dev.zed.Zed', cli: 'Contents/MacOS/cli' },
  finder: { label: 'Finder', bundle: 'Finder.app', bundleID: 'com.apple.finder', cli: '' },
} as const;

function boundedText(value: unknown, name: string, limit: number): asserts value is string {
  if (typeof value !== 'string' || !value || Buffer.byteLength(value) > limit || /[\u0000-\u001f\u007f]/.test(value))
    throw new Error(`Invalid ${name}.`);
}

export function validateOpenProject(value: unknown): OpenProjectRequest {
  if (!value || typeof value !== 'object') throw new Error('Invalid project request.');
  const request = value as OpenProjectRequest;
  if (!Object.hasOwn(applications, request.app)) throw new Error('Choose a supported editor.');
  boundedText(request.directory, 'working directory', 16 << 10);
  // Remote Whip hosts and the initial desktop distribution use POSIX paths.
  if (!path.posix.isAbsolute(request.directory)) throw new Error('The working directory must be an absolute path.');
  boundedText(request.connectionId, 'connection identity', 128);
  boundedText(request.runtimeId, 'runtime identity', 256);
  if (request.sshAlias !== undefined) {
    boundedText(request.sshAlias, 'SSH alias', 255);
    if (!/^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$/.test(request.sshAlias))
      throw new Error('Use an OpenSSH alias or hostname, without a username, port, spaces, or command options.');
  }
  return { app: request.app, directory: request.directory, connectionId: request.connectionId,
    runtimeId: request.runtimeId, ...(request.sshAlias === undefined ? {} : { sshAlias: request.sshAlias }) };
}

/** URL hosts never acquire local filesystem authority, even for loopback URLs. */
export function projectSSHAlias(target: ConnectionTarget, explicit?: string): string | undefined {
  if (target.kind === 'local') {
    if (explicit) throw new Error('A local folder cannot use an SSH alias.');
    return undefined;
  }
  if (explicit) return explicit;
  if (target.kind === 'ssh' && !target.user && !target.port && !target.identityFile && /^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$/.test(target.host))
    return target.host;
  throw new Error(target.kind === 'ssh'
    ? 'Configure an OpenSSH alias for editors that includes this host’s username, port, and identity settings.'
    : 'Configure SSH for editors on this host. Its Whip server URL does not identify an SSH connection.');
}

export function projectArguments(app: Exclude<ProjectEditorID, 'finder'>, directory: string, alias?: string): string[] {
  // Encode each segment without normalizing the daemon's path or treating #,
  // %, quotes, spaces, or shell metacharacters as URL syntax.
  const pathname = directory.split('/').map(segment => encodeURIComponent(segment)).join('/');
  if (app === 'zed') return [alias ? `ssh://${alias}${pathname}` : directory];
  return ['--folder-uri', alias ? `vscode-remote://ssh-remote+${alias}${pathname}` : pathToFileURL(directory).href];
}

export function projectEnvironment(source: NodeJS.ProcessEnv): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { PATH: '/usr/bin:/bin:/usr/sbin:/sbin' };
  // Preserve OS identity and the user's agent, without forwarding provider keys,
  // Node injection flags, or a parent editor's remote CLI routing hooks.
  for (const key of ['HOME', 'USER', 'LOGNAME', 'SHELL', 'TMPDIR', 'LANG', 'LC_ALL', 'LC_CTYPE', 'SSH_AUTH_SOCK'])
    if (source[key]) env[key] = source[key];
  return env;
}

/** A bounded identity read; never a second owner of session views or commands. */
export async function verifyProjectRuntime(endpoint: string | TransportFactory, runtimeId: string, signal: AbortSignal): Promise<void> {
  const verifier = createWhipClient({ endpoint, clientId: `desktop-editor-${randomUUID()}`, expectedRuntimeId: runtimeId,
    reconnect: false, connectTimeoutMs: 5000 });
  try { await verifier.connect({ signal }); }
  catch { throw new Error('The source host could not be verified. Reconnect it and confirm that it still serves this conversation’s runtime.'); }
  finally { verifier.close(); }
}

export class ProjectEditors {
  constructor(private openPath: (directory: string) => Promise<string>,
    private run: (file: string, args: string[], signal?: AbortSignal) => Promise<string> = async (file, args, signal) => {
      const { stdout } = await execute(file, args, { timeout: 10_000, maxBuffer: 16 << 10, signal, env: projectEnvironment(process.env) });
      return stdout;
    },
    private directories = ['/Applications', path.join(homedir(), 'Applications')]) {}

  private async launcher(id: ProjectEditorID, signal?: AbortSignal): Promise<string | undefined> {
    if (process.platform !== 'darwin') return undefined;
    if (id === 'finder') return '/System/Library/CoreServices/Finder.app';
    const application = applications[id];
    const existing = async (bundle: string) => {
      const file = path.join(bundle, application.cli);
      try { return (await stat(file)).isFile() ? file : undefined; } catch { return undefined; }
    };
    for (const directory of this.directories) {
      signal?.throwIfAborted();
      const file = await existing(path.join(directory, application.bundle));
      if (file) return file;
    }
    // Spotlight also finds apps installed outside the standard Applications folders.
    try {
      const matches = await this.run('/usr/bin/mdfind', [`kMDItemCFBundleIdentifier == '${application.bundleID}'`], signal);
      for (const bundle of matches.trim().split('\n').slice(0, 16)) {
        if (!path.isAbsolute(bundle) || path.basename(bundle) !== application.bundle) continue;
        const file = await existing(bundle); if (file) return file;
      }
    } catch { signal?.throwIfAborted(); }
    return undefined;
  }

  async list(signal?: AbortSignal): Promise<readonly ProjectEditor[]> {
    return Promise.all((Object.keys(applications) as ProjectEditorID[]).map(async id => ({ id, label: applications[id].label,
      installed: !!await this.launcher(id, signal) })));
  }

  async open(input: OpenProjectRequest, target: ConnectionTarget, signal: AbortSignal): Promise<void> {
    const request = validateOpenProject(input);
    const alias = projectSSHAlias(target, request.sshAlias);
    if (request.app === 'finder' && alias) throw new Error('Finder can open folders on This Mac only. Choose an SSH editor for this host.');
    if (!alias) {
      try { if (!(await stat(request.directory)).isDirectory()) throw new Error(); }
      catch { throw new Error('The local working directory no longer exists or cannot be opened.'); }
    }
    const launcher = await this.launcher(request.app, signal);
    if (!launcher) throw new Error(`${applications[request.app].label} is not installed. Install it, then reopen the menu.`);
    signal.throwIfAborted();
    if (request.app === 'finder') {
      const error = await this.openPath(request.directory);
      if (error) throw new Error(`Finder could not open this directory: ${error.slice(0, 1024)}`);
      return;
    }
    try { await this.run(launcher, projectArguments(request.app, request.directory, alias), signal); }
    catch {
      signal.throwIfAborted();
      throw new Error(`${applications[request.app].label} did not accept the folder request. Open the editor and check its command-line launcher${alias ? ' and Remote SSH support' : ''}, then retry.`);
    }
  }
}
