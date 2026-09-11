// Staged desktop host: a terminal tab on This Mac over the Unix socket. Like
// smoke.mjs this uses stock Electron against staged production assets; signed
// installed-app acceptance is separate.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { _electron } from 'playwright';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { unixSocket } from '../../../packages/sdk/dist/node.js';
import { LocalRuntime, readRuntimeManifest } from '../src/runtime.ts';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

const exec = promisify(execFile);
// Keep the runtime below macOS's Unix socket path limit, as smoke.mjs does.
const fixture = await mkdtemp('/tmp/whip-desktop-terminal-');
const stage = path.join(repositoryRoot, 'apps/desktop/.stage');
const executable = path.join(fixture, 'bin/whipcode');
const env = { PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh',
  HOME: path.join(fixture, 'user'), TMPDIR: path.join(fixture, 'tmp'),
  WHIP_DESKTOP_FIXTURE: '1', WHIPCODE_HOME: path.join(fixture, 'home'), WHIPCODE_NETWORK: '0',
  WHIP_DESKTOP_EXECUTABLE: executable, WHIP_DESKTOP_USER_DATA: path.join(fixture, 'user-data') };
for (const directory of [env.HOME, env.TMPDIR, env.WHIPCODE_HOME, env.WHIP_DESKTOP_USER_DATA]) await mkdir(directory, { mode: 0o700 });
const artifacts = process.env.WHIP_DESKTOP_TERMINAL_ARTIFACTS ?? '/tmp/whip-desktop-terminal-results';
await mkdir(artifacts, { recursive: true });
const eventually = async (check, description, timeout = 20_000) => {
  const deadline = Date.now() + timeout; let last;
  while (Date.now() < deadline) {
    try { const value = await check(); if (value) return value; } catch (error) { last = error; }
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error(`Timed out waiting for ${description}`, { cause: last });
};
let electron; let client; let page; let runtimeInstalled = false;
const checks = []; const errors = [];
try {
  const manifest = await readRuntimeManifest(path.join(stage, 'app/runtime-manifest.json'));
  await new LocalRuntime({ source: path.join(stage, 'native'), manifest, env,
    settingsFile: path.join(env.WHIP_DESKTOP_USER_DATA, 'native-local-runtime.json'), defaultExecutable: executable })
    .install(executable, AbortSignal.timeout(15_000));
  runtimeInstalled = true;
  electron = await _electron.launch({ args: [path.join(stage, 'app')], env, timeout: 30_000 });
  let diagnostics = '';
  electron.process().stderr?.on('data', bytes => { diagnostics = (diagnostics + bytes.toString()).slice(-8192); });
  page = await electron.firstWindow();
  page.on('pageerror', error => errors.push(error.message));
  page.setDefaultTimeout(20_000);
  await page.waitForFunction(() => JSON.parse(localStorage.getItem('whip.hosts.v2') || '[]').some(host => host.id === 'local' && host.runtimeId), undefined, { timeout: 30_000 })
    .catch(async error => { console.error((await page.locator('body').innerText()).slice(0, 4000), diagnostics); throw error; });
  const status = JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);
  assert.equal(status.state, 'running');
  assert(!status.network_endpoint, 'Local attachment must not enable TCP');

  // Open a terminal from the command palette on an empty workspace.
  await page.keyboard.press('Meta+k');
  const commands = page.getByRole('dialog', { name: 'Commands', exact: true });
  await commands.getByRole('combobox').fill('New terminal');
  await page.getByRole('option', { name: 'New terminal', exact: true }).click();
  await eventually(() => /\/h\/[^/]+\/t\//.test(page.url()), 'terminal route');
  const terminalId = decodeURIComponent(new URL(page.url()).pathname.split('/t/')[1]);
  const view = page.locator('[data-terminal-view]');
  const live = () => eventually(async () => (await view.getAttribute('data-terminal-status')) === 'live', 'terminal live');
  const reattach = async () => {
    await eventually(async () => (await view.getAttribute('data-terminal-status')) === 'detached', 'page notices the takeover');
    await page.getByRole('button', { name: 'Reattach here', exact: true }).click();
    await live();
  };
  await live();
  checks.push('palette opens a shell on This Mac over the Unix socket');

  await view.click();
  await page.keyboard.type('echo whip-$((40+2))');
  await page.keyboard.press('Enter');
  // Verify through the daemon: a second Unix client attaches and reads the ring.
  client = createWhipClient({ endpoint: unixSocket(status.socket), clientId: `desktop-terminal-${crypto.randomUUID()}`, clientKind: 'human' });
  await client.connect();
  const replay = async marker => {
    let text = '';
    const off = client.terminals.onOutput(output => { if (output.id === terminalId) text += new TextDecoder().decode(output.bytes); });
    try {
      const attachment = await client.terminals.attach(terminalId, 0);
      await eventually(() => text.includes(marker), `replay containing ${marker}`);
      return attachment;
    } finally { off(); }
  };
  await replay('whip-42');
  checks.push('typed keystrokes reach the shell and the daemon retains its output');
  await reattach();
  await page.screenshot({ path: path.join(artifacts, 'desktop-terminal.png') });

  // Copy: select by dragging across the first rows, then copy through the native Edit
  // menu (what Cmd+C triggers on macOS) and through the terminal's context menu.
  const canvas = await page.locator('[data-terminal-view] canvas').boundingBox();
  await page.mouse.move(canvas.x + 2, canvas.y + 2);
  await page.mouse.down();
  await page.mouse.move(canvas.x + canvas.width - 4, canvas.y + 60, { steps: 8 });
  await page.mouse.up();
  await eventually(async () => (await view.getAttribute('data-terminal-selection')) === 'true', 'terminal selection made by dragging');
  await electron.evaluate(({ clipboard }) => clipboard.writeText(''));
  await electron.evaluate(({ Menu }) => {
    const edit = Menu.getApplicationMenu().items.find(item => item.role === 'editmenu' || item.label === 'Edit');
    edit.submenu.items.find(item => item.role === 'copy').click();
  });
  await eventually(async () => (await electron.evaluate(({ clipboard }) => clipboard.readText())).includes('whip-42'), 'Edit > Copy put the terminal selection on the clipboard');
  await electron.evaluate(({ clipboard }) => clipboard.writeText(''));
  await view.click({ button: 'right', position: { x: 40, y: 20 } });
  await page.getByRole('menuitem', { name: 'Copy', exact: true }).click();
  await eventually(async () => (await electron.evaluate(({ clipboard }) => clipboard.readText())).includes('whip-42'), 'context menu Copy put the terminal selection on the clipboard');
  checks.push('Cmd+C through the Edit menu and right-click Copy both copy the terminal selection');
  await view.click();

  // Paste goes through the renderer's native paste command; the window denies
  // clipboard-read permission, so this is the only path and only a real host proves it.
  await electron.evaluate(({ clipboard }) => clipboard.writeText('echo paste-$((50+5))'));
  await view.click();
  await page.keyboard.press('Meta+v');
  await page.keyboard.press('Enter');
  await replay('paste-55');
  checks.push('Cmd+V pastes through the native paste event');
  await reattach();

  // Reload keeps the shell and the tab; the replay still holds earlier output.
  await page.reload();
  await live();
  assert.equal(await view.getAttribute('data-terminal-view'), terminalId, 'reload restored the same shell');
  await replay('paste-55');
  await reattach();
  checks.push('reload restores the tab and reattaches the same shell');

  // Cmd+W is a native menu accelerator; synthesized renderer keys never reach it,
  // so click the real File > Close tab item in the main process.
  await view.click();
  await electron.evaluate(({ Menu }) => {
    const file = Menu.getApplicationMenu().items.find(item => item.label === 'File');
    file.submenu.items.find(item => item.label === 'Close tab').click();
  });
  await eventually(async () => (await view.count()) === 0, 'terminal view closed');
  await eventually(async () => {
    try { await client.terminals.attach(terminalId, 0); return false; }
    catch (error) { return error?.code === -32003; }
  }, 'daemon forgot the closed terminal');
  checks.push('Cmd+W closes the tab and ends the shell');
  await page.screenshot({ path: path.join(artifacts, 'desktop-after-close.png') });
  assert.deepEqual(errors, []);
  const result = { purpose: 'Staged desktop terminal tab on This Mac; not signed installed-app acceptance',
    recordedAt: new Date().toISOString(), terminalId, shell: env.SHELL, checks, rendererErrors: errors };
  await writeFile(path.join(artifacts, 'desktop-terminal.json'), JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result, null, 2));
} catch (error) {
  await page?.screenshot({ path: path.join(artifacts, 'desktop-terminal-failure.png') }).catch(() => {});
  await writeFile(path.join(artifacts, 'desktop-terminal-failure.txt'), `${error.stack}\n\n${await page?.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`).catch(() => {});
  throw error;
} finally {
  client?.close();
  if (electron) await electron.evaluate(({ app }) => app.exit(0)).catch(() => {});
  try {
    if (runtimeInstalled) await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
    await rm(fixture, { recursive: true, force: true });
  } catch (error) {
    console.error(`Preserved smoke fixture after cleanup failure: ${fixture}`);
    throw error;
  }
}
