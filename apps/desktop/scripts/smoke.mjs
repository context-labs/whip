// Native host integration against staged production assets. Shipping-fuse and
// signed Finder acceptance are separate; this diagnostic uses stock Electron.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, mkdir, readdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { _electron } from 'playwright';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

const exec = promisify(execFile);
// Keep both the runtime and its isolated TMPDIR below macOS's Unix socket limit.
const fixture = await mkdtemp('/tmp/whip-desktop-smoke-');
const stage = path.join(repositoryRoot, 'apps/desktop/.stage');
const executable = path.join(stage, 'native/whip');
const env = { PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh',
  HOME: path.join(fixture, 'user'), TMPDIR: path.join(fixture, 'tmp'),
  WHIP_DESKTOP_FIXTURE: '1', WHIP_HOME: path.join(fixture, 'home'),
  WHIP_DESKTOP_USER_DATA: path.join(fixture, 'user-data') };
for (const directory of [env.HOME, env.TMPDIR, env.WHIP_HOME, env.WHIP_DESKTOP_USER_DATA]) {
  await mkdir(directory, { mode: 0o700 });
}
let electron;
try {
  const launch = () => _electron.launch({ args: [path.join(stage, 'app')], env, timeout: 30_000 });
  electron = await launch();
  let diagnostics = '';
  electron.process().stderr?.on('data', bytes => { diagnostics = (diagnostics + bytes.toString()).slice(-8192); });
  const page = await electron.firstWindow();
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  await page.waitForFunction(() => JSON.parse(localStorage.getItem('whip.hosts.v2') || '[]').some(host => host.id === 'local' && host.runtimeId), undefined, { timeout: 30_000 })
    .catch(async error => { console.error((await page.locator('body').innerText()).slice(0, 6000), diagnostics); throw error; });
  const { stdout } = await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 });
  const initial = JSON.parse(stdout);
  assert.equal(initial.state, 'running'); assert(!initial.network_endpoint, 'Local attachment must not enable TCP');
  const runtimeFiles = await readdir(path.join(env.WHIP_DESKTOP_USER_DATA, 'runtimes'));
  assert.equal(runtimeFiles.length, 1);
  await page.getByRole('link', { name: 'Settings', exact: true }).first().click();
  await page.getByRole('heading', { name: 'Settings', exact: true }).waitFor();
  await page.reload();
  await page.getByRole('heading', { name: 'Settings', exact: true }).waitFor();
  assert.deepEqual(errors, []);
  // Force only the fixture GUI process to disappear: daemon lifetime is separate.
  await electron.evaluate(({ app }) => app.exit(0)).catch(error => {
    if (!/closed|destroyed/.test(error.message)) throw error;
  });
  electron = undefined;
  const survived = JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);
  assert.equal(survived.state, 'running'); assert.equal(survived.pid, initial.pid);
  electron = await launch();
  const reopened = await electron.firstWindow();
  // The saved identity exists before reconnect. Require a host-dependent enabled
  // control to prove the new renderer actually attached to the daemon.
  await reopened.waitForFunction(() => [...document.querySelectorAll('button')]
    .some(button => button.textContent?.trim() === 'Browse this Mac' && !button.disabled));
  const attached = JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);
  assert.equal(attached.pid, initial.pid);
  const result = { purpose: 'Staged host integration; not signed/fused installed-app acceptance or a startup benchmark',
    recordedAt: new Date().toISOString(),
    noNetwork: !initial.network_endpoint, runtimeInstalled: true, daemonSurvivedGUIExit: true,
    relaunchAttachedSameDaemon: true, settingsReload: true, rendererErrors: errors };
  await writeFile(path.join(repositoryRoot, '.ai-docs/plans/desktop-app/evidence/local-smoke.json'), JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result, null, 2));
} finally {
  if (electron) await electron.evaluate(({ app }) => app.exit(0)).catch(() => {});
  // Retained executables must stay present if cleanup cannot stop their owner.
  // In particular, do not turn a shutdown failure into a deleted live runtime.
  try {
    await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
    const stopped = JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);
    assert.equal(stopped.state, 'stopped');
    await rm(fixture, { recursive: true, force: true });
  } catch (error) {
    console.error(`Preserved smoke fixture after cleanup failure: ${fixture}`);
    throw error;
  }
}
