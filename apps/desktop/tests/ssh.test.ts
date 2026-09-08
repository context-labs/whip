import assert from 'node:assert/strict';
import { execFile, spawn, type ChildProcess } from 'node:child_process';
import { once } from 'node:events';
import { access, chmod, lstat, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { connect, createServer, type Server, type Socket } from 'node:net';
import { userInfo } from 'node:os';
import path from 'node:path';
import { promisify } from 'node:util';
import { setTimeout as delay } from 'node:timers/promises';
import test, { type TestContext } from 'node:test';
import { validateProfile } from '@whip/app/platform';
import { shellQuote, sshArguments, SSHConnection } from '../src/ssh';

const exec = promisify(execFile);
const helper = process.env.WHIP_DESKTOP_SSH_TEST_EXECUTABLE;
const integration = { skip: !helper && 'Set WHIP_DESKTOP_SSH_TEST_EXECUTABLE to a built Whip helper', timeout: 30_000 };
const fixtureQuote = (value: string) => `'${value.replaceAll("'", "'\"'\"'")}'`;

test('quotes remote paths as one literal shell argument without executing metacharacters', async t => {
  const directory = await mkdtemp('/tmp/whip-ssh-quote-');
  t.after(() => rm(directory, { recursive: true, force: true }));
  const marker = path.join(directory, 'must-not-exist');
  for (const value of [
    '/Applications/A Whip.app/Contents/Helpers/whip', "/srv/it's a project/whip", '--literal-option',
    `/tmp/$(touch ${marker})`, `/tmp/\`touch ${marker}\``, `/tmp/a'; touch ${marker}; #`,
    '/tmp/$HOME/*?[abc]\\name', '/tmp/unicode-é-文', '100% complete',
  ]) {
    const result = await exec('/bin/sh', ['-c', `printf '%s' ${shellQuote(value)}`], { env: { PATH: '/usr/bin:/bin', HOME: directory } });
    assert.equal(result.stdout, value);
  }
  await assert.rejects(access(marker), { code: 'ENOENT' });
  for (const value of ['', '/tmp/line\nbreak', '/tmp/\0hidden', '/tmp/\rhidden', '/tmp/\u007fhidden'])
    assert.throws(() => shellQuote(value), /argument/);
});

test('passes SSH identity paths literally and enforces noninteractive forwarding controls', async t => {
  const directory = await mkdtemp('/tmp/whip-ssh-argv-');
  t.after(() => rm(directory, { recursive: true, force: true }));
  const marker = path.join(directory, 'must-not-exist');
  const identityFile = `${directory}/a key'; $(touch must-not-exist); #`;
  await writeFile(identityFile, 'fixture identity', { mode: 0o600 });
  const profile = validateProfile({ id: 'ssh:fixture', label: 'Fixture', target: {
    kind: 'ssh', host: 'fixture-alias', user: 'fixture-user', port: 2299, identityFile,
  } });
  assert.equal(profile.target.kind, 'ssh');
  if (profile.target.kind !== 'ssh') throw new Error('Expected SSH fixture');
  const args = sshArguments(profile.target);
  assert.equal(args[args.indexOf('-i') + 1], identityFile);
  const { stdout } = await exec('/usr/bin/ssh', [...args, '-G', '-F', '/dev/null', profile.target.host], { cwd: directory });
  for (const line of ['user fixture-user', 'port 2299', 'requesttty false', 'forwardagent no',
    'exitonforwardfailure yes', 'connecttimeout 15', 'serveraliveinterval 15', 'serveralivecountmax 3'])
    assert.ok(stdout.split('\n').includes(line), line);
  assert.ok(stdout.includes(`identityfile ${identityFile}\n`));
  await assert.rejects(access(marker), { code: 'ENOENT' });
  for (const host of ['-oProxyCommand=bad', 'user@host', 'host;command', 'host\n'])
    assert.throws(() => validateProfile({ id: 'ssh:fixture', label: 'Fixture', target: { kind: 'ssh', host } }));
});

test('rejects cancelled and malformed remote setup before launching SSH', async () => {
  const controller = new AbortController(); controller.abort(new Error('Cancelled fixture'));
  const cancelled = new SSHConnection({ target: { kind: 'ssh', host: 'fixture' }, executable: '/not-executed',
    env: {}, signal: controller.signal, progress() {}, prompt: async () => { throw new Error('Unexpected prompt'); } });
  await assert.rejects(cancelled.getSocket(), /Cancelled fixture/);
  await cancelled.dispose();
  for (const target of [
    { kind: 'ssh' as const, host: 'fixture', remoteHome: 'relative/home' },
    { kind: 'ssh' as const, host: 'fixture', remoteExecutable: 'relative/path/whip' },
  ]) {
    const connection = new SSHConnection({ target, executable: '/not-executed', env: {},
      signal: new AbortController().signal, progress() {}, prompt: async () => { throw new Error('Unexpected prompt'); } });
    await assert.rejects(connection.getSocket(), /absolute path/);
    await connection.dispose();
  }
});

test('preserves Unicode in an authentication request split inside a UTF-8 character', { timeout: 5000 }, async t => {
  const directory = await mkdtemp('/tmp/whip-ssh-prompt-');
  const executable = path.join(directory, 'supervisor');
  const script = path.join(directory, 'prompt.cjs');
  const replyFile = path.join(directory, 'reply.json');
  const message = 'Enter the passphrase for café/文/🔑:';
  await writeFile(script, `
const { connect } = require('node:net');
const { writeFileSync } = require('node:fs');
process.stdin.resume(); process.stdin.once('end', () => process.exit(0));
const socket = connect(process.env.WHIP_DESKTOP_PROMPT_SOCKET);
const request = Buffer.from(JSON.stringify({ token: process.env.WHIP_DESKTOP_PROMPT_TOKEN,
  prompt: ${JSON.stringify(message)}, confirm: false }) + '\\n');
const split = request.indexOf(Buffer.from('é')) + 1;
socket.once('connect', () => {
  socket.write(request.subarray(0, split));
  setTimeout(() => socket.write(request.subarray(split)), 150);
});
let reply = '';
socket.on('data', bytes => { reply += bytes.toString(); });
socket.once('end', () => { writeFileSync(${JSON.stringify(replyFile)}, reply); process.exit(1); });
`, { mode: 0o600 });
  await writeFile(executable, `#!/bin/sh\nexec ${fixtureQuote(process.execPath)} ${fixtureQuote(script)}\n`, { mode: 0o700 });
  const seen: string[] = [];
  const connection = new SSHConnection({ executable, env: {}, target: { kind: 'ssh', host: 'fixture' },
    signal: t.signal, progress() {}, prompt: async value => { seen.push(value.message); return ['réponse 文']; } });
  t.after(async () => { await connection.dispose(); await rm(directory, { recursive: true, force: true }); });
  // This fixture exercises only the real prompt socket; it intentionally never
  // establishes a control master after recording the authentication reply.
  await assert.rejects(connection.getSocket(), /SSH connection failed/);
  assert.deepEqual(seen, [message]);
  assert.deepEqual(JSON.parse(await readFile(replyFile, 'utf8')), { answer: 'réponse 文' });
});

// All identities, authorized keys, known_hosts, shell startup directories and sockets
// are private fixture files. The real system SSH client never reads ~/.ssh/config.
async function fixture(t: TestContext, options: {
  encrypted?: boolean; unknownHost?: boolean; forwarding?: boolean; stopped?: boolean; inheritedForward?: boolean;
  fragmentedStatus?: boolean;
} = {}) {
  assert.ok(helper, 'An explicit built Whip helper is required');
  const directory = await mkdtemp('/tmp/whip-sshd-test-');
  await chmod(directory, 0o700);
  const connections = new Set<SSHConnection>();
  const peers = new Set<Socket>();
  let sshd: ChildProcess | undefined;
  let echo: Server | undefined;
  t.after(async () => {
    await Promise.allSettled([...connections].map(connection => connection.dispose()));
    for (const peer of peers) peer.destroy();
    if (echo?.listening) await new Promise<void>((resolve, reject) => echo!.close(error => error ? reject(error) : resolve()));
    if (sshd && sshd.exitCode === null && sshd.signalCode === null) {
      const closed = once(sshd, 'close'); sshd.kill('SIGTERM'); await closed;
    }
    await rm(directory, { recursive: true, force: true });
  });
  const hostKey = path.join(directory, 'host');
  const identity = path.join(directory, 'identity');
  const knownHosts = path.join(directory, 'known_hosts');
  const log = path.join(directory, 'remote.log');
  const whipcodeHomeLog = path.join(directory, 'whipcode-home.log');
  const state = path.join(directory, 'running');
  const remoteSocket = path.join(directory, options.fragmentedStatus ? 'café-文.sock' : 'remote.sock');
  const inheritedSocket = path.join(directory, 'unrelated.sock');
  const remoteExecutable = path.join(directory, "whip 'fixture'");
  const marker = path.join(directory, 'must-not-exist');
  const remoteHome = `${directory}/home '; touch ${marker}; #`;
  const password = 'ephemeral fixture passphrase';
  await exec('/usr/bin/ssh-keygen', ['-q', '-t', 'ed25519', '-N', '', '-f', hostKey]);
  await exec('/usr/bin/ssh-keygen', ['-q', '-t', 'ed25519', '-N', options.encrypted ? password : '', '-f', identity]);
  await writeFile(path.join(directory, 'authorized_keys'), await readFile(identity + '.pub'), { mode: 0o600 });
  const reserve = createServer(); reserve.listen(0, '127.0.0.1'); await once(reserve, 'listening');
  const port = (reserve.address() as { port: number }).port;
  await new Promise<void>(resolve => reserve.close(() => resolve()));
  const hostPublicKey = (await readFile(hostKey + '.pub', 'utf8')).trim().split(' ').slice(0, 2).join(' ');
  await writeFile(knownHosts, options.unknownHost ? '' : `[127.0.0.1]:${port} ${hostPublicKey}\n`, { mode: 0o600 });
  await writeFile(path.join(directory, 'dispatch'), '#!/bin/sh\nexec /bin/sh -c "$SSH_ORIGINAL_COMMAND"\n', { mode: 0o700 });
  if (!options.stopped) await writeFile(state, 'running');
  const runningStatus = JSON.stringify({ state: 'running', socket: remoteSocket, pid: 123 });
  const statusScript = path.join(directory, 'status.cjs');
  if (options.fragmentedStatus) await writeFile(statusScript, `
const output = Buffer.from(${JSON.stringify(runningStatus + '\n')});
const split = output.indexOf(Buffer.from('é')) + 1;
process.stdout.write(output.subarray(0, split));
setTimeout(() => process.stdout.write(output.subarray(split)), 150);
`, { mode: 0o600 });
  await writeFile(remoteExecutable, `#!/bin/sh
printf '%s|%s\\n' "$1 $2" "$WHIP_HOME" >> ${fixtureQuote(log)}
printf '%s\\n' "$WHIPCODE_HOME" >> ${fixtureQuote(whipcodeHomeLog)}
case "$1 $2" in
  'daemon status')
    if [ -f ${fixtureQuote(state)} ]; then
      ${options.fragmentedStatus ? `${fixtureQuote(process.execPath)} ${fixtureQuote(statusScript)}` : `printf '%s\\n' '${runningStatus}'`}
    else
      printf '%s\\n' '${JSON.stringify({ state: 'stopped', socket: remoteSocket })}'
    fi ;;
  'daemon start') : > ${fixtureQuote(state)} ;;
  *) exit 2 ;;
esac
`, { mode: 0o700 });
  echo = createServer(peer => {
    peers.add(peer); peer.on('error', () => {}); peer.once('close', () => peers.delete(peer));
    peer.on('data', bytes => peer.write(bytes));
  });
  echo.listen(remoteSocket); await once(echo, 'listening');
  const username = userInfo().username;
  const serverConfig = path.join(directory, 'sshd_config');
  await writeFile(serverConfig, `Port ${port}
ListenAddress 127.0.0.1
HostKey ${hostKey}
PidFile ${directory}/sshd.pid
AuthorizedKeysFile ${directory}/authorized_keys
# The immediate fixture directory and key are owner-only; /tmp's shared parent
# would otherwise make OpenSSH reject this isolated authorized_keys location.
StrictModes no
UsePAM no
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers ${username}
# OpenSSH also gates Unix destination permissions on this setting in
# do_authenticated; the client creates only the tested Unix forwarding path.
AllowTcpForwarding local
AllowStreamLocalForwarding ${options.forwarding === false ? 'no' : 'yes'}
AllowAgentForwarding no
X11Forwarding no
PermitTTY no
ForceCommand ${directory}/dispatch
SetEnv HOME=${directory} ZDOTDIR=${directory} BASH_ENV=/dev/null ENV=/dev/null
LogLevel VERBOSE
`);
  await exec('/usr/sbin/sshd', ['-t', '-f', serverConfig]);
  sshd = spawn('/usr/sbin/sshd', ['-D', '-e', '-f', serverConfig], { env: { PATH: '/usr/bin:/bin', HOME: directory }, stdio: ['ignore', 'ignore', 'pipe'] });
  const serverProcess = sshd;
  let serverOutput = '';
  await new Promise<void>((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error(`Fixture sshd did not listen: ${serverOutput}`)), 5000);
    serverProcess.stderr!.on('data', bytes => {
      serverOutput = (serverOutput + String(bytes)).slice(-16_384);
      if (serverOutput.includes('Server listening')) { clearTimeout(timeout); resolve(); }
    });
    serverProcess.once('error', error => { clearTimeout(timeout); reject(error); });
    serverProcess.once('exit', () => { clearTimeout(timeout); reject(new Error(`Fixture sshd exited: ${serverOutput}`)); });
  });
  const clientConfig = path.join(directory, 'ssh_config');
  await writeFile(clientConfig, `Host *
  UserKnownHostsFile ${knownHosts}
  GlobalKnownHostsFile /dev/null
  IdentityAgent none
  IdentitiesOnly yes
  PasswordAuthentication no
  KbdInteractiveAuthentication no
  LogLevel ERROR
${options.inheritedForward ? `  LocalForward ${inheritedSocket} ${remoteSocket}\n` : ''}
`);
  const wrapper = path.join(directory, 'supervisor');
  await writeFile(wrapper, `#!/bin/sh
[ "$1" = '_desktop-ssh' ] || exit 91
shift
exec ${fixtureQuote(helper)} _desktop-ssh -F ${fixtureQuote(clientConfig)} "$@"
`, { mode: 0o700 });
  return { directory, remoteSocket, remoteHome, inheritedSocket, log, whipcodeHomeLog, marker, password, serverOutput: () => serverOutput,
    connection(prompt: ConstructorParameters<typeof SSHConnection>[0]['prompt'], controller = new AbortController()) {
      const connection = new SSHConnection({ executable: wrapper, env: { HOME: directory, PATH: '/usr/bin:/bin', USER: username, LOGNAME: username },
        target: { kind: 'ssh', host: '127.0.0.1', user: username, port, identityFile: identity, remoteExecutable, remoteHome },
        signal: AbortSignal.any([controller.signal, t.signal, AbortSignal.timeout(12_000)]), progress() {}, prompt });
      connections.add(connection);
      return connection;
    },
  };
}

async function exchange(socket: string, value: string) {
  const client = connect(socket);
  client.setTimeout(3000, () => client.destroy(new Error('Fixture forwarding timed out')));
  try {
    return await new Promise<string>((resolve, reject) => {
      client.once('error', reject);
      client.once('close', () => reject(new Error('Fixture forwarding closed before a reply')));
      client.once('data', bytes => resolve(String(bytes)));
      client.once('connect', () => client.write(value));
    });
  } finally { client.destroy(); }
}

test('real SSH forwards an isolated Unix socket, quotes remote paths and preserves remote work on disposal', integration, async t => {
  const f = await fixture(t, { stopped: true, inheritedForward: true });
  const connection = f.connection(async () => { throw new Error('Unexpected authentication prompt'); });
  let sockets: string[];
  try { sockets = await Promise.all([connection.getSocket(), connection.getSocket()]); }
  catch (error) { throw new Error(`${String(error)}\nFixture server: ${f.serverOutput()}`); }
  assert.equal(sockets[0], sockets[1]);
  assert.equal((await lstat(path.dirname(sockets[0]!))).mode & 0o777, 0o700);
  try { assert.equal(await exchange(sockets[0]!, 'through SSH'), 'through SSH'); }
  catch (error) { throw new Error(`${String(error)}\nFixture server: ${f.serverOutput()}`); }
  assert.deepEqual((await readFile(f.log, 'utf8')).trim().split('\n'), [
    `daemon status|${f.remoteHome}`, `daemon start|${f.remoteHome}`, `daemon status|${f.remoteHome}`,
  ]);
  assert.deepEqual((await readFile(f.whipcodeHomeLog, 'utf8')).trim().split('\n'), Array(3).fill(f.remoteHome));
  await assert.rejects(access(f.marker), { code: 'ENOENT' });
  await assert.rejects(lstat(f.inheritedSocket), { code: 'ENOENT' });
  await connection.dispose();
  await assert.rejects(lstat(sockets[0]!), { code: 'ENOENT' });
  assert.equal(await exchange(f.remoteSocket, 'remote remains alive'), 'remote remains alive');
});

test('real SSH preserves the remote Unix path when status output splits a UTF-8 character', integration, async t => {
  const f = await fixture(t, { fragmentedStatus: true });
  const connection = f.connection(async () => { throw new Error('Unexpected prompt'); });
  const socket = await connection.getSocket();
  assert.equal(await exchange(socket, 'Unicode path'), 'Unicode path');
  assert.deepEqual((await readFile(f.log, 'utf8')).trim().split('\n'), [`daemon status|${f.remoteHome}`]);
});

test('real SSH asks before trusting a generated unknown host and decrypting its fixture key', integration, async t => {
  const f = await fixture(t, { unknownHost: true, encrypted: true });
  const prompts: { title: string; fields: number }[] = [];
  const connection = f.connection(async value => {
    prompts.push({ title: value.title, fields: value.fields.length });
    assert.ok(prompts.length <= 2, 'Unexpected repeated fixture authentication prompt');
    return value.fields.length ? [f.password] : [];
  });
  let socket: string;
  try { socket = await connection.getSocket(); }
  catch (error) { throw new Error(`${String(error)}\nFixture server: ${f.serverOutput()}`); }
  assert.equal(await exchange(socket, 'authenticated'), 'authenticated');
  assert.deepEqual(prompts.map(prompt => prompt.fields), [0, 1]);
  assert.match(prompts[0]!.title, /Verify SSH host/);
  assert.match(prompts[1]!.title, /Authenticate/);
});

test('real SSH host-trust cancellation never authenticates or saves the generated host key', integration, async t => {
  const f = await fixture(t, { unknownHost: true });
  const prompts: { title: string; fields: number }[] = [];
  const connection = f.connection(async value => {
    prompts.push({ title: value.title, fields: value.fields.length }); return null;
  });
  await assert.rejects(connection.getSocket());
  assert.deepEqual(prompts, [{ title: 'Verify SSH host', fields: 0 }]);
  assert.equal(await readFile(path.join(f.directory, 'known_hosts'), 'utf8'), '');
  await assert.rejects(access(f.log), { code: 'ENOENT' });
  assert.equal(await exchange(f.remoteSocket, 'unaffected'), 'unaffected');
});

test('real SSH prompt cancellation ends establishment rather than asking for the passphrase again', integration, async t => {
  const f = await fixture(t, { encrypted: true });
  let prompts = 0;
  const connection = f.connection(async () => { prompts++; return null; });
  await assert.rejects(connection.getSocket());
  assert.equal(prompts, 1);
  assert.equal(await exchange(f.remoteSocket, 'unaffected'), 'unaffected');
});

test('real SSH cancellation dismisses pending authentication and removes its local resources', integration, async t => {
  const f = await fixture(t, { encrypted: true });
  const controller = new AbortController();
  let promptSignal: AbortSignal | undefined;
  const connection = f.connection(async (_value, lifetime) => {
    promptSignal = lifetime; controller.abort(new Error('Cancelled while authenticating'));
    return null;
  }, controller);
  await assert.rejects(connection.getSocket());
  assert.equal(promptSignal?.aborted, true);
  await connection.dispose();
  assert.equal(await exchange(f.remoteSocket, 'unaffected'), 'unaffected');
});

test('a local forwarding socket does not prove the server allows the remote Unix destination', integration, async t => {
  const f = await fixture(t, { forwarding: false });
  const connection = f.connection(async () => { throw new Error('Unexpected prompt'); });
  const socket = await connection.getSocket();
  await assert.rejects(exchange(socket, 'blocked'));
  assert.equal(await exchange(f.remoteSocket, 'remote remains alive'), 'remote remains alive');
});

test('real SSH recovers a lost owned master without restarting healthy remote work', integration, async t => {
  const f = await fixture(t);
  const connection = f.connection(async () => { throw new Error('Unexpected prompt during recovery'); });
  const initial = await connection.getSocket();
  assert.equal(await exchange(initial, 'before disconnect'), 'before disconnect');
  await exec('/usr/bin/ssh', ['-F', '/dev/null', '-S', path.join(path.dirname(initial), 'control'),
    '-o', 'ProxyCommand=false', '-O', 'exit', '127.0.0.1']);
  let recovered = initial;
  // The control command acknowledges before the owned master's close event.
  for (let attempt = 0; attempt < 100 && recovered === initial; attempt++) {
    await delay(20);
    recovered = await connection.getSocket();
  }
  assert.notEqual(recovered, initial, 'Expected a new owned forwarding lease');
  assert.equal(await exchange(recovered, 'after reconnect'), 'after reconnect');
  assert.deepEqual((await readFile(f.log, 'utf8')).trim().split('\n'), [
    `daemon status|${f.remoteHome}`, `daemon status|${f.remoteHome}`,
  ]);
  await assert.rejects(lstat(initial), { code: 'ENOENT' });
});

test('real SSH stops automatic reconnect when a lost master needs authentication again', integration, async t => {
  const f = await fixture(t, { encrypted: true });
  let prompts = 0;
  const connection = f.connection(async () => { prompts++; return [f.password]; });
  const initial = await connection.getSocket();
  assert.equal(await exchange(initial, 'authenticated'), 'authenticated');
  await exec('/usr/bin/ssh', ['-F', '/dev/null', '-S', path.join(path.dirname(initial), 'control'),
    '-o', 'ProxyCommand=false', '-O', 'exit', '127.0.0.1']);
  let failure: unknown;
  for (let attempt = 0; attempt < 100 && !failure; attempt++) {
    await delay(20);
    try { assert.equal(await connection.getSocket(), initial, 'A locked fixture key cannot authenticate silently'); }
    catch (error) { failure = error; }
  }
  assert.match(String(failure), /Permission denied/);
  await assert.rejects(connection.getSocket(), /authentication is needed/);
  assert.equal(prompts, 1, 'Automatic reconnect must not open a second authentication prompt');
  assert.deepEqual((await readFile(f.log, 'utf8')).trim().split('\n'), [`daemon status|${f.remoteHome}`]);
  assert.equal(await exchange(f.remoteSocket, 'unaffected'), 'unaffected');
});
