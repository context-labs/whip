// Staged desktop host: a terminal tab on This Mac over the Unix socket. Like
// smoke.mjs this uses stock Electron against staged production assets; signed
// installed-app acceptance is separate.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';
import { _electron } from 'playwright';
import { Client } from '../../../packages/sdk/dist/index.js';
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
  assert(!status.process.web_endpoint, 'Local attachment must not enable TCP');

  // Open a terminal from the command palette on an empty workspace.
  await page.keyboard.press('Meta+k');
  const commands = page.getByRole('dialog', { name: 'Commands', exact: true });
  await commands.getByRole('combobox').fill('New terminal');
  await page.getByRole('option', { name: 'New terminal', exact: true }).click();
  await eventually(() => /\/h\/[^/]+\/t\//.test(page.url()), 'terminal route');
  const view = page.locator('[data-terminal-view]');
  const live = () => eventually(async () => (await view.getAttribute('data-terminal-status')) === 'live', 'terminal live');
  await live();
  const terminalId = await view.getAttribute('data-terminal-view');
  assert(terminalId);
  assert.equal(decodeURIComponent(new URL(page.url()).pathname.split('/t/')[1]), terminalId);
  checks.push('palette opens a shell on This Mac over the Unix socket');

  await view.click();
  await page.keyboard.type('echo whip-$((40+2))');
  await page.keyboard.press('Enter');
  // A second native client reads bounded replay pages without taking over the UI.
  client = await Client.connect(unixSocket(status.socket), { clientID: `desktop-terminal-${crypto.randomUUID()}`,
    expectedRuntimeID: status.process.runtime_id, signal: AbortSignal.timeout(15000) });
  const ref = { id: terminalId, process_epoch: client.processEpoch };
  const replay = async marker => eventually(async () => {
    let cursor = '0', text = '';
    for (let pages = 0; pages < 32; pages++) {
      const output = await client.readTerminal(ref, cursor, 32768, { signal: AbortSignal.timeout(5000) });
      assert.equal(output.terminal.id, terminalId);
      assert.equal(output.terminal.process_epoch, client.processEpoch);
      text += Buffer.from(output.data_base64, 'base64').toString();
      if (text.includes(marker)) return output;
      if (BigInt(output.next) >= BigInt(output.end)) break;
      assert(BigInt(output.next) > BigInt(cursor), 'Replay cursor must advance');
      cursor = output.next;
    }
    return false;
  }, `replay containing ${marker}`);
  await replay('whip-42');
  checks.push('typed keystrokes reach the shell and the daemon retains its output');
  await live(); // Independent replay observation must not detach the renderer.
  await page.screenshot({ path: path.join(artifacts, 'desktop-terminal.png') });

  // Copy: select by dragging across the first rows. ghostty-web copies a mouse
  // selection on mouseup (the async clipboard API is denied here, so it lands through
  // execCommand); wait for that before proving the Copy command itself, the one the
  // Edit menu and Cmd+C run. A native role's menuItem.click() is a no-op on macOS.
  const copyCommand = () => electron.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].webContents.copy());
  const clipboard = () => electron.evaluate(({ clipboard }) => clipboard.readText());
  const canvas = await page.locator('[data-terminal-view] canvas').boundingBox();
  await page.mouse.move(canvas.x + 2, canvas.y + 2);
  await page.mouse.down();
  await page.mouse.move(canvas.x + canvas.width - 4, canvas.y + 60, { steps: 8 });
  await page.mouse.up();
  await eventually(async () => (await view.getAttribute('data-terminal-selection')) === 'true', 'terminal selection made by dragging');
  await eventually(async () => (await clipboard()).includes('whip-42'), 'copy on select landed');
  await electron.evaluate(({ clipboard }) => clipboard.writeText(''));
  await copyCommand();
  await eventually(async () => (await clipboard()).includes('whip-42'), 'the Copy command put the terminal selection on the clipboard');
  await electron.evaluate(({ clipboard }) => clipboard.writeText(''));
  await view.click({ button: 'right', position: { x: 40, y: 20 } });
  await page.getByRole('menuitem', { name: 'Copy', exact: true }).click();
  await eventually(async () => (await clipboard()).includes('whip-42'), 'context menu Copy put the terminal selection on the clipboard');
  checks.push('the Copy command (Edit menu, Cmd+C) and right-click Copy both copy the terminal selection');
  await view.click();

  // A program that owns the mouse (whip's TUI, vim): a plain drag is reported to it,
  // Shift+drag selects locally and copies through the same Copy command.
  await page.keyboard.type("clear; echo tracked-$((60+6)); printf '\\e[?1002h\\e[?1006h'; cat -v");
  await page.keyboard.press('Enter');
  await replay('tracked-66');
  await live(); // Independent replay observation must not detach the renderer.
  // The independent backend read can lead the renderer's next bounded poll.
  // Retry only this local selection/copy observation until the painted output arrives.
  await eventually(async () => {
    await page.keyboard.down('Shift');
    await page.mouse.move(canvas.x + 2, canvas.y + 2);
    await page.mouse.down();
    await page.mouse.move(canvas.x + canvas.width - 4, canvas.y + 40, { steps: 8 });
    await page.mouse.up();
    await page.keyboard.up('Shift');
    if (await view.getAttribute('data-terminal-selection') !== 'true') return false;
    await electron.evaluate(({ clipboard }) => clipboard.writeText(''));
    await copyCommand();
    return (await clipboard()).includes('tracked-66');
  }, 'the Copy command copied the Shift+drag selection');
  await view.click();
  await page.keyboard.press('Control+c');
  await page.keyboard.type("printf '\\e[?1002l\\e[?1006l'");
  await page.keyboard.press('Enter');
  checks.push('Shift+drag selects and copies while a program has mouse tracking');

  // Paste goes through the renderer's native paste command; the window denies
  // clipboard-read permission, so this is the only path and only a real host proves it.
  await electron.evaluate(({ clipboard }) => clipboard.writeText('echo paste-$((50+5))'));
  // The terminal still has keyboard focus. A click before the disable-mouse
  // output is painted would correctly be reported to the just-finished program.
  await page.keyboard.press('Meta+v');
  await page.keyboard.press('Enter');
  await replay('paste-55');
  checks.push('Cmd+V pastes through the native paste event');
  await live(); // Independent replay observation must not detach the renderer.

  // Reload keeps the shell and the tab; the replay still holds earlier output.
  await page.reload();
  await live();
  assert.equal(await view.getAttribute('data-terminal-view'), terminalId, 'reload restored the same shell');
  await replay('paste-55');
  await live(); // Independent replay observation must not detach the renderer.
  checks.push('reload restores the tab and reattaches the same shell');

  // Exercise the production New session menu/IPC path without replacing or
  // stopping the terminal. OS accelerator routing still needs real-key acceptance.
  await view.click();
  await electron.evaluate(({ Menu }) => {
    const item = Menu.getApplicationMenu().items.find(item => item.label === 'File')
      .submenu.items.find(item => item.label === 'New session');
    if (item.accelerator !== 'CmdOrCtrl+T') throw new Error('Missing New session accelerator');
    item.click();
  });
  await eventually(() => /\/new\//.test(page.url()), 'New Chat from terminal focus');
  const closedDraftURL = page.url();
  await electron.evaluate(({ Menu }) => {
    Menu.getApplicationMenu().items.find(item => item.label === 'File')
      .submenu.items.find(item => item.label === 'Close tab').click();
  });
  await live();
  assert.equal(await view.getAttribute('data-terminal-view'), terminalId, 'New session preserved the terminal');
  await replay('paste-55');
  await live(); // Independent replay observation must not detach the renderer.
  checks.push('New session native menu opens a draft and preserves the running terminal');

  // Restore that exact draft via the production menu, including from a hidden window.
  await electron.evaluate(({ BrowserWindow, Menu }) => {
    const window = BrowserWindow.getAllWindows()[0];
    window.close();
    if (window.isVisible()) throw new Error('Closed window should be hidden');
    const item = Menu.getApplicationMenu().items.find(item => item.label === 'File')
      .submenu.items.find(item => item.label === 'Reopen closed tab');
    if (item.accelerator !== 'CmdOrCtrl+Shift+T') throw new Error('Missing Reopen closed tab accelerator');
    item.click();
    if (!window.isVisible() || !window.webContents.isFocused()) throw new Error('Reopen must reveal and focus the window');
  });
  await eventually(() => page.url() === closedDraftURL, 'Reopen restored the original draft identity');
  await electron.evaluate(({ Menu }) => {
    Menu.getApplicationMenu().items.find(item => item.label === 'File')
      .submenu.items.find(item => item.label === 'Close tab').click();
  });
  await live();
  assert.equal(await view.getAttribute('data-terminal-view'), terminalId, 'Reopen preserved the terminal');
  await replay('paste-55');
  await live(); // Independent replay observation must not detach the renderer.
  checks.push('Reopen closed tab native menu restores the same draft from a hidden window and preserves the running terminal');

  // Closing the native window hides it without destroying the workspace.
  // New session must reveal that same window instead of creating an invisible draft.
  await electron.evaluate(({ BrowserWindow, Menu }) => {
    const window = BrowserWindow.getAllWindows()[0];
    window.close();
    if (window.isVisible()) throw new Error('Closed window should be hidden');
    Menu.getApplicationMenu().items.find(item => item.label === 'File')
      .submenu.items.find(item => item.label === 'New session').click();
    if (!window.isVisible() || !window.webContents.isFocused()) throw new Error('New session must reveal and focus the window');
  });
  await eventually(() => /\/new\//.test(page.url()), 'New Chat from hidden window');
  await electron.evaluate(({ Menu }) => {
    Menu.getApplicationMenu().items.find(item => item.label === 'File')
      .submenu.items.find(item => item.label === 'Close tab').click();
  });
  await live();
  assert.equal(await view.getAttribute('data-terminal-view'), terminalId);
  checks.push('New session reveals a previously closed native window');

  // Cmd+W is a native menu accelerator; synthesized renderer keys never reach it,
  // so click the real File > Close tab item in the main process.
  await view.click();
  await electron.evaluate(({ Menu }) => {
    const file = Menu.getApplicationMenu().items.find(item => item.label === 'File');
    file.submenu.items.find(item => item.label === 'Close tab').click();
  });
  await eventually(async () => (await view.count()) === 0, 'terminal view closed');
  await eventually(async () => {
    try { await client.readTerminal(ref, '0', 1, { signal: AbortSignal.timeout(5000) }); return false; }
    catch (error) { return error?.kind === 'NOT_FOUND'; }
  }, 'daemon forgot the closed terminal');
  checks.push('Cmd+W closes the tab and ends the shell');
  await page.screenshot({ path: path.join(artifacts, 'desktop-after-close.png') });
  assert.deepEqual(errors, []);
  const result = { purpose: 'Staged desktop terminal tab on This Mac; not signed installed-app acceptance',
    recordedAt: new Date().toISOString(), rendererDigest: manifest.rendererDigest, runtimeDigest: manifest.files.whipcode.sha256, processEpoch: client.processEpoch, terminalId, shell: env.SHELL, replay: 'Bounded cursor reads do not transfer a terminal attachment or stop the shell.', checks, rendererErrors: errors };
  await writeFile(path.join(artifacts, 'desktop-terminal.json'), JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result, null, 2));
} catch (error) {
  for (const details of await page?.getByRole('button', { name: 'Error details', exact: true }).all() ?? []) await details.click().catch(() => {});
  await page?.screenshot({ path: path.join(artifacts, 'desktop-terminal-failure.png') }).catch(() => {});
  await writeFile(path.join(artifacts, 'desktop-terminal-failure.txt'), `${error.stack}\n\n${await page?.locator('body').innerText().catch(() => '')}\n\n${JSON.stringify(errors)}`).catch(() => {});
  throw error;
} finally {
  if (electron) await electron.evaluate(({ app }) => app.exit(0)).catch(() => {});
  try {
    if (runtimeInstalled) {
      await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
      assert.equal(JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout).state, 'stopped');
    }
    await rm(fixture, { recursive: true, force: true });
  } catch (error) {
    console.error(`Preserved smoke fixture after cleanup failure: ${fixture}`);
    throw error;
  }
}
