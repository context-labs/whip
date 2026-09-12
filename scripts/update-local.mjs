import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { constants } from 'node:fs';
import { access, copyFile, lstat, mkdir, mkdtemp, readFile, rename, rm, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import path from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import { parseArgs, promisify } from 'node:util';
import { repositoryRoot, sha256 } from './renderer-artifact.mjs';

const exec = promisify(execFile);
const run = async (file, args, options = {}) => (await exec(file, args,
  { cwd: repositoryRoot, encoding: 'utf8', timeout: 60_000, maxBuffer: 1 << 20, ...options })).stdout.trim();
const digest = async filename => sha256(await readFile(filename));
const help = `Usage: task update:local [-- --app /Applications/Whip.app --executable /path/to/whipcode]

Build, sign, verify and install the current working tree, then restart the shared
daemon and reopen Desktop. Running agent work is interrupted; saved state remains.
Requires macOS Apple Silicon, Node 24, Go, Xcode and a Developer ID signing identity.
Defaults: /Applications/Whip.app and its saved backend (or /usr/local/bin/whipcode).
WHIP_DESKTOP_VERSION and WHIPCODE_VERSION optionally override version/build ID.
WHIP_DESKTOP_SIGN_IDENTITY / WHIP_DESKTOP_TEAM_ID select a signing identity.
WHIP_DESKTOP_NOTARIZE=1 uses the existing packaging notarization settings.
See README.md for details and recovery. No Git pull or release publication occurs.`;

async function build(env) {
  for (const [file, args] of [['npm', ['ci']], [process.execPath, ['node_modules/electron/install.js']],
    [process.execPath, ['apps/desktop/scripts/package.mjs']]]) {
    await new Promise((resolve, reject) => {
      const child = spawn(file, args, { cwd: repositoryRoot, stdio: 'inherit', env });
      child.once('error', reject);
      child.once('exit', (code, signal) => code === 0 ? resolve() : reject(new Error(`${file} failed (${signal || code})`)));
    });
  }
}

export async function quitApp(app, { execute = run, wait = delay } = {}) {
  try {
    await execute('/usr/bin/osascript', ['-l', 'JavaScript', '-e',
      'function run(argv) { const app = Application(argv[0]); if (app.running()) app.quit(); }', app]);
  } catch (error) {
    // Electron cancels the initial Apple event while it saves drafts, then quits
    // asynchronously. Only observed exit authorizes replacing the installed app.
    if (!/\(-128\)\s*$/.test(error.stderr || '')) throw error;
  }
  for (let attempt = 0; attempt < 30; attempt++) {
    if (await execute('/usr/bin/osascript', ['-l', 'JavaScript', '-e',
      'function run(argv) { return Application(argv[0]).running(); }', app]) === 'false') return;
    await wait(1000);
  }
  throw new Error('Whip did not quit. Resolve any unsaved-draft dialog or quit it normally, then retry.');
}

// Keep actual filesystem replacement in tests; only native packaging/processes
// are substituted. Backend shutdown and atomic replacement belong to the Go helper.
export async function installLocalBuild({ bundle, app, executable, expected, evidence, env },
  { execute = run, verify, quit = quitApp } = {}) {
  verify ??= (await import('../apps/desktop/scripts/verify.mjs')).verifyDesktop;
  const directory = await mkdtemp(path.join(path.dirname(app), '.whip-local-update-'));
  const staged = path.join(directory, path.basename(app));
  const previous = path.join(directory, 'previous.app');
  let preserve = false;
  try {
    await execute('/usr/bin/ditto', [bundle, staged]);
    const verified = await verify(staged, { signed: true, notarized: env.WHIP_DESKTOP_NOTARIZE === '1' });
    assert.equal(verified.buildId, evidence.buildId, 'Staged build changed');
    assert.equal(verified.nativeFiles.whipcode.sha256, evidence.nativeFiles.whipcode.sha256);
    assert.equal(await digest(executable), expected, 'Installed backend changed while building; retry');
    await copyFile(executable, path.join(directory, 'whipcode.previous'), constants.COPYFILE_EXCL);
    assert.equal(await digest(path.join(directory, 'whipcode.previous')), expected);
    await quit(app);
    await rename(app, previous);
    preserve = true;
    try { await rename(staged, app); } catch (error) { await rename(previous, app); throw error; }
    console.log(`Previous app and executable retained in ${directory}`);
    const payload = path.join(app, 'Contents/Helpers/whipcode');
    const result = JSON.parse(await execute(payload, ['_desktop-runtime-sync', '--executable', executable,
      '--expected-sha256', expected, '--sha256', evidence.nativeFiles.whipcode.sha256, '--interrupt'], { env }));
    assert.equal(result.state, 'ready', 'Updated backend did not become ready');
    assert.equal(result.buildId, evidence.buildId);
    assert.equal(await digest(executable), evidence.nativeFiles.whipcode.sha256);
    const status = JSON.parse(await execute(executable, ['daemon', 'status', '--json'], { env }));
    assert.equal(status.state, 'running');
    assert.equal(status.daemon_build, evidence.buildId);
    assert.equal(status.build_match, true);
    await execute('/usr/bin/open', ['-a', app]);
    return { buildId: evidence.buildId, app, executable, backup: directory, status };
  } catch (error) {
    if (preserve) throw new Error(`${error.message}\nUpdate incomplete; app destination: ${app}; recovery files: ${directory}. ` +
      'Fix the reported problem and rerun update:local. Do not restore an older backend over a migrated database.', { cause: error });
    throw error;
  } finally {
    if (!preserve) await rm(directory, { recursive: true, force: true });
  }
}

export async function main(args = process.argv.slice(2)) {
  const { values } = parseArgs({ args, options: { app: { type: 'string' }, executable: { type: 'string' }, help: { type: 'boolean' } } });
  if (values.help) { console.log(help); return; }
  assert(process.platform === 'darwin' && process.arch === 'arm64', 'Local desktop updates require macOS Apple Silicon');
  assert.equal(process.versions.node.split('.')[0], '24', 'Use Node 24');
  const app = path.resolve(values.app || '/Applications/Whip.app');
  assert((await lstat(app)).isDirectory(), 'Choose an existing app directory, not a symlink');
  await access(path.dirname(app), constants.W_OK);
  const plist = key => run('/usr/libexec/PlistBuddy', ['-c', `Print :${key}`, path.join(app, 'Contents/Info.plist')]);
  const bundleId = await plist('CFBundleIdentifier');
  assert(['com.contextlabs.whip', 'com.contextlabs.whip.beta'].includes(bundleId), 'Choose Whip.app or Whip Beta.app');
  const channel = bundleId.endsWith('.beta') ? 'beta' : 'stable';
  const name = channel === 'beta' ? 'Whip Beta' : 'Whip';
  const settingsFile = path.join(homedir(), 'Library/Application Support', name, 'native-local-runtime.json');
  const settings = await readFile(settingsFile, 'utf8').then(JSON.parse).catch(error => {
    if (error.code === 'ENOENT') return {}; throw error;
  });
  const executable = path.resolve(values.executable || settings.executable || '/usr/local/bin/whipcode');
  if (settings.executable) assert.equal(executable, settings.executable, `Desktop uses ${settings.executable}; choose the intended executable in Desktop first`);
  if (settings.managed) assert.equal(settings.managed.channel, channel, 'Desktop backend is managed by a different channel');
  assert((await lstat(executable)).isFile(), 'Backend must be an existing regular file, not a symlink');
  assert((await lstat(path.dirname(executable))).isDirectory(), 'Backend parent must not be a symlink');
  await access(path.dirname(executable), constants.W_OK);
  const expected = await digest(executable);
  const info = JSON.parse(await run(executable, ['_desktop-runtime-info']));
  assert.equal(info.distribution, 'whipcode');
  const installedSignature = await exec('/usr/bin/codesign', ['-dvv', app]);
  const team = process.env.WHIP_DESKTOP_TEAM_ID || installedSignature.stderr.match(/^TeamIdentifier=([A-Z0-9]{10})$/m)?.[1];
  assert.match(team || '', /^[A-Z0-9]{10}$/, 'Set WHIP_DESKTOP_TEAM_ID to your Developer ID team');
  const identities = await run('/usr/bin/security', ['find-identity', '-v', '-p', 'codesigning']);
  const matching = [...identities.matchAll(/([A-F0-9]{40}) "(Developer ID Application: [^"\n]+)"/g)]
    .filter(match => match[2].endsWith(`(${team})`));
  const identity = process.env.WHIP_DESKTOP_SIGN_IDENTITY || (matching.length === 1 ? matching[0][1] : undefined);
  assert(identity && matching.some(match => match[1] === identity || match[2] === identity), 'Set WHIP_DESKTOP_SIGN_IDENTITY to an available Developer ID Application identity for this team');
  const version = process.env.WHIP_DESKTOP_VERSION || await plist('CFBundleShortVersionString');
  assert(/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(version), 'Invalid desktop version');
  assert.equal(version.includes('-'), channel === 'beta', 'Beta versions need a prerelease suffix; stable versions must not have one');
  const commit = await run('git', ['rev-parse', '--short', 'HEAD']);
  const buildId = process.env.WHIPCODE_VERSION || `local-${new Date().toISOString().replace(/[^0-9]/g, '')}-${commit}`;
  const env = { ...process.env, WHIP_DESKTOP_CHANNEL: channel, WHIP_DESKTOP_VERSION: version,
    WHIPCODE_VERSION: buildId, WHIP_DESKTOP_SIGN_IDENTITY: identity, WHIP_DESKTOP_TEAM_ID: team };
  delete env.WHIP_DESKTOP_RELEASE;
  delete env.WHIP_DESKTOP_UPDATE_URL;
  // Match Desktop's normal local endpoint; explicit runtime environment wins.
  env.WHIPCODE_LISTEN ??= '127.0.0.1:8080';
  const lock = path.join(repositoryRoot, 'apps/desktop/.update-local.lock');
  await mkdir(lock).catch(error => {
    if (error.code === 'EEXIST') throw new Error(`Another local update owns ${lock}. If it was interrupted, remove that directory after confirming its process has exited.`);
    throw error;
  });
  try {
    await writeFile(path.join(lock, 'pid'), `${process.pid}\n`);
    console.log(`Building ${name} ${version} (${buildId}) from ${repositoryRoot}\nDestination: ${app}\nBackend: ${executable}\nThe daemon will restart after the signed package is verified.`);
    await build(env);
    const evidence = JSON.parse(await readFile(path.join(repositoryRoot, 'apps/desktop/out/release/evidence.json'), 'utf8'));
    assert.equal(evidence.buildId, buildId);
    assert.equal(evidence.signed, true);
    const result = await installLocalBuild({ bundle: evidence.bundle, app, executable, expected, evidence, env });
    console.log(`Local update complete.\n${JSON.stringify(result, null, 2)}`);
  } finally { await rm(lock, { recursive: true, force: true }); }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url))
  main().catch(error => { console.error(error.message); process.exitCode = 1; });
