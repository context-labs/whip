// Explicit macOS acceptance check for the installed editors. SSH identities,
// configuration and editor user data are private; the real OS HOME is preserved.
// No real remote is contacted.
// This verifies handoff and SSH authentication, not remote editor server setup.
import assert from 'node:assert/strict';
import { build } from 'esbuild';
import { execFile, spawn } from 'node:child_process';
import { once } from 'node:events';
import { cp, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import { homedir, userInfo } from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';
import { setTimeout as delay } from 'node:timers/promises';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import semver from 'semver';

if (process.env.WHIP_EDITOR_SMOKE_ALLOW_APPS !== '1')
  throw new Error('This manual diagnostic launches installed editors. Set WHIP_EDITOR_SMOKE_ALLOW_APPS=1 explicitly; macOS keychain suppression is not yet verified for Cursor.');
if (process.platform !== 'darwin') throw new Error('Installed editor smoke currently requires macOS.');
const execute = promisify(execFile);
const root = fileURLToPath(new URL('../../../', import.meta.url));
const fixture = await mkdtemp('/tmp/whip-editor-smoke-');
const children = [];
const results = [];
let server;
let serverLog = '';
const apps = [
  { id: 'cursor', cli: '/Applications/Cursor.app/Contents/Resources/app/bin/cursor', extensions: '.cursor/extensions', prefix: 'anysphere.remote-ssh-' },
  { id: 'vscode', cli: '/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code', extensions: '.vscode/extensions', prefix: 'ms-vscode-remote.remote-ssh-' },
  { id: 'zed', cli: '/Applications/Zed.app/Contents/MacOS/cli' },
];
const selected = process.env.WHIP_EDITOR_SMOKE_APPS?.split(',') ?? apps.map(app => app.id);
assert.ok(selected.length && selected.every(id => apps.some(app => app.id === id)), 'Choose cursor,vscode,zed for WHIP_EDITOR_SMOKE_APPS');
try {
  await build({ entryPoints: [path.join(root, 'apps/desktop/src/project-open.ts')], outfile: path.join(fixture, 'project-open.cjs'),
    bundle: true, platform: 'node', format: 'cjs' });
  const { ProjectEditors, projectEnvironment } = createRequire(import.meta.url)(path.join(fixture, 'project-open.cjs'));
  const project = path.join(fixture, 'Project # % ü with spaces'); await mkdir(project);
  await writeFile(path.join(project, 'README.md'), '# Whip editor smoke fixture\n');
  for (const name of ['host', 'identity']) await execute('/usr/bin/ssh-keygen', ['-q', '-t', 'ed25519', '-N', '', '-f', path.join(fixture, name)]);
  await cp(path.join(fixture, 'identity.pub'), path.join(fixture, 'authorized_keys'));
  const reserve = createServer(); reserve.listen(0, '127.0.0.1'); await once(reserve, 'listening');
  const port = reserve.address().port; await new Promise(resolve => reserve.close(resolve));
  const key = (await readFile(path.join(fixture, 'host.pub'), 'utf8')).trim().split(' ').slice(0, 2).join(' ');
  await writeFile(path.join(fixture, 'known_hosts'), `[127.0.0.1]:${port} ${key}\n`);
  const alias = 'whip-editor-fixture';
  const config = path.join(fixture, 'ssh_config');
  await writeFile(config, `Host ${alias}
  HostName 127.0.0.1
  Port ${port}
  User ${userInfo().username}
  IdentityFile ${fixture}/identity
  UserKnownHostsFile ${fixture}/known_hosts
  GlobalKnownHostsFile /dev/null
  IdentitiesOnly yes
  IdentityAgent none
  StrictHostKeyChecking yes
  PasswordAuthentication no
  KbdInteractiveAuthentication no
`);
  // Deliberately refuse remote server setup after authentication. The test cannot
  // install an editor server or run a project task on this computer by accident.
  const dispatch = path.join(fixture, 'dispatch');
  await writeFile(dispatch, '#!/bin/sh\nprintf "Whip editor fixture: server setup intentionally disabled\\n" >&2\nexit 1\n', { mode: 0o700 });
  const serverConfig = path.join(fixture, 'sshd_config');
  await writeFile(serverConfig, `Port ${port}
ListenAddress 127.0.0.1
HostKey ${fixture}/host
PidFile ${fixture}/sshd.pid
AuthorizedKeysFile ${fixture}/authorized_keys
StrictModes no
UsePAM no
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers ${userInfo().username}
AllowTcpForwarding yes
AllowStreamLocalForwarding yes
AllowAgentForwarding no
X11Forwarding no
PermitTTY no
ForceCommand ${dispatch}
SetEnv HOME=${fixture} ZDOTDIR=${fixture} BASH_ENV=/dev/null ENV=/dev/null
LogLevel VERBOSE
`);
  await execute('/usr/sbin/sshd', ['-t', '-f', serverConfig]);
  server = spawn('/usr/sbin/sshd', ['-D', '-e', '-f', serverConfig], { env: { PATH: '/usr/bin:/bin', HOME: fixture }, stdio: ['ignore', 'ignore', 'pipe'] });
  server.stderr.on('data', bytes => { serverLog = (serverLog + String(bytes)).slice(-64 * 1024); });
  for (let i = 0; i < 50 && !serverLog.includes('Server listening'); i++) await delay(100);
  assert.match(serverLog, /Server listening/, 'Fixture SSH listener did not start');
  for (const application of apps.filter(app => selected.includes(app.id))) {
    if (application.id === 'zed') {
      const processes = (await execute('/bin/ps', ['-axo', 'command='])).stdout.split('\n');
      if (processes.some(command => command.startsWith('/Applications/Zed.app/Contents/MacOS/zed') && !command.includes('--crash-handler'))) {
        results.push({ app: 'zed', skipped: 'Zed already has open windows. Its macOS single-instance check prevents an isolated remote configuration; close Zed before rerunning the zed-only smoke.' });
        console.log('zed: isolated remote smoke skipped to preserve existing editor windows');
        continue;
      }
    }
    const data = path.join(fixture, application.id);
    await mkdir(data, { recursive: true });
    // Preserve macOS's real home/keychain context. A synthetic HOME without a
    // login keychain caused Cursor to prompt; --user-data-dir isolates app data.
    const env = { ...projectEnvironment(process.env), TMPDIR: fixture };
    const args = ['--user-data-dir', data];
    if (application.id === 'zed') {
      await mkdir(path.join(data, 'config'), { recursive: true });
      await writeFile(path.join(data, 'config/settings.json'), JSON.stringify({
        ssh_connections: [{ host: alias, args: ['-F', config] }], telemetry: { diagnostics: false, metrics: false }, auto_update: false,
      }));
      // --foreground bypasses macOS Launch Services reusing a user's existing Zed.
      args.push('--foreground', '--new');
    } else {
      const extensions = path.join(data, 'extensions'); await mkdir(extensions, { recursive: true });
      const installed = path.join(homedir(), application.extensions);
      const resource = application.cli.split('/Contents/')[0] + '/Contents/Resources/app';
      const product = JSON.parse(await readFile(path.join(resource, 'product.json'), 'utf8'));
      const metadata = JSON.parse(await readFile(path.join(resource, 'package.json'), 'utf8'));
      const versions = (await readdir(installed)).filter(name => name.startsWith(application.prefix) && /^\d/.test(name.slice(application.prefix.length)))
        .sort((a, b) => b.localeCompare(a, undefined, { numeric: true }));
      let extension;
      for (const version of versions) {
        const candidate = JSON.parse(await readFile(path.join(installed, version, 'package.json'), 'utf8'));
        if (semver.satisfies(product.vscodeVersion ?? metadata.version, candidate.engines.vscode)) { extension = version; break; }
      }
      assert.ok(extension, `Install ${application.id}'s Remote SSH extension before running this smoke.`);
      await cp(path.join(installed, extension), path.join(extensions, extension), { recursive: true });
      await mkdir(path.join(data, 'User'), { recursive: true });
      await writeFile(path.join(data, 'User/settings.json'), JSON.stringify({
        'remote.SSH.configFile': config, 'remote.SSH.useExecServer': false, 'remote.SSH.showLoginTerminal': false,
        'security.workspace.trust.enabled': false, 'update.mode': 'none', 'extensions.autoUpdate': false, 'telemetry.telemetryLevel': 'off',
      }));
      // These fixture-only switches never enter the production native launcher.
      // Their effect on Cursor's own credential module has not been verified.
      args.push('--extensions-dir', extensions, '--new-window', '--password-store=basic', '--use-mock-keychain');
    }
    const before = (serverLog.match(/Accepted publickey/g) ?? []).length;
    let launchArguments;
    let launchLog = '';
    const editors = new ProjectEditors(async () => '', async (file, generated, signal) => {
      assert.equal(file, application.cli);
      launchArguments = generated;
      const child = spawn(file, [...args, ...generated], { env, signal, stdio: ['ignore', 'pipe', 'pipe'] });
      children.push(child); await once(child, 'spawn');
      child.on('error', () => {});
      for (const stream of [child.stdout, child.stderr]) stream.on('data', bytes => { launchLog = (launchLog + String(bytes)).slice(-64 * 1024); });
      if (application.id !== 'zed') assert.equal((await once(child, 'exit'))[0], 0, 'Editor rejected its launch arguments');
      return '';
    });
    await editors.open({ app: application.id, directory: project, connectionId: 'fixture', runtimeId: 'fixture', sshAlias: alias },
      { kind: 'url', endpoint: 'http://127.0.0.1:1' }, AbortSignal.timeout(30_000));
    for (let i = 0; i < 150 && (serverLog.match(/Accepted publickey/g) ?? []).length === before; i++) await delay(200);
    await writeFile(path.join(data, 'launcher.log'), launchLog);
    assert.ok((serverLog.match(/Accepted publickey/g) ?? []).length > before, `${application.id} did not authenticate to the isolated SSH alias`);
    let savedFolder;
    if (application.id !== 'zed') {
      const storage = JSON.parse(await readFile(path.join(data, 'User/globalStorage/storage.json'), 'utf8'));
      const window = storage.windowsState?.lastActiveWindow ?? storage.backupWorkspaces?.folders?.find(folder => folder.remoteAuthority === `ssh-remote+${alias}`);
      assert.ok(window, 'Editor did not retain the requested remote workspace');
      assert.equal(window.remoteAuthority, `ssh-remote+${alias}`);
      savedFolder = window.folder ?? window.folderUri;
      assert.equal(decodeURIComponent(new URL(savedFolder).pathname), project);
    }
    results.push({ app: application.id, accepted: true, sshAuthenticated: true, launchArguments, ...(savedFolder ? { savedFolder } : {}),
      remoteServerSetup: 'Intentionally disabled after authentication by fixture ForceCommand' });
    console.log(`${application.id}: actual launcher and isolated SSH authentication passed`);
  }
  const output = process.env.WHIP_EDITOR_SMOKE_OUTPUT ?? path.join(root, '.ai-docs/plans/conversation-row-actions/native-editors-smoke.json');
  await writeFile(output, JSON.stringify({ recordedAt: new Date().toISOString(), platform: process.platform, scope: 'Installed editor handoff and SSH authentication; not remote server installation or browsing', results }, null, 2) + '\n');
  console.log(output);
} finally {
  // Only processes carrying our unique fixture path are ours to close. Existing
  // editor instances and windows must survive this test.
  for (const child of children) if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM');
  if (server && server.exitCode === null && server.signalCode === null) server.kill('SIGTERM');
  const ownedEditors = async () => (await execute('/bin/ps', ['-axo', 'pid=,command='])).stdout.split('\n').flatMap(line => {
    const match = /^\s*(\d+) (.*)$/.exec(line);
    return match && match[2].startsWith('/Applications/') && /--user-data-dir(?:=| )/.test(match[2]) &&
      (match[2].includes(fixture) || match[2].includes(fixture.replace('/tmp/', '/private/tmp/'))) ? [Number(match[1])] : [];
  });
  const stop = (pids, signal) => { for (const pid of pids) { try { process.kill(pid, signal); } catch {} } };
  stop(await ownedEditors(), 'SIGTERM');
  for (let i = 0; i < 20 && (await ownedEditors()).length; i++) await delay(250);
  // A detached GUI can outlive its CLI and can ignore SIGTERM during a modal.
  // Only PIDs still advertising our exact fixture data directory may be killed.
  stop(await ownedEditors(), 'SIGKILL');
  for (const child of children) if (child.exitCode === null && child.signalCode === null) child.kill('SIGKILL');
  for (let i = 0; i < 20 && (await ownedEditors()).length; i++) await delay(100);
  await delay(200);
  const remaining = await ownedEditors();
  if (remaining.length || process.env.WHIP_EDITOR_SMOKE_KEEP === '1') console.log(`Preserved fixture: ${fixture}${remaining.length ? `; editor PIDs still alive: ${remaining.join(', ')}` : ''}`);
  else await rm(fixture, { recursive: true, force: true });
}
