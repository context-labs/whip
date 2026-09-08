import type { BrowserWindow } from 'electron';
import { lstat, open, realpath, type FileHandle } from 'node:fs/promises';
import path from 'node:path';
import { isDesktopURL } from './assets';

// No arguments or renderer-facing API: this observes a fixed, disposable fixture.
// A textarea alone is insufficient: it remains editable while disconnected.
const snapshotScript = String.raw`(async () => {
  const visible = element => !!element && element.getClientRects().length > 0;
  const enabled = element => visible(element) && !element.matches(':disabled') && element.getAttribute('aria-disabled') !== 'true';
  const inspect = () => {
    const navigation = document.querySelector('[aria-label="Session navigation"]');
    const host = visible(navigation?.querySelector('[aria-label="Manage execution hosts"]'));
    const noNotice = ![...document.querySelectorAll('[role="alert"], p[role="status"]')].some(visible);
    const composer = document.querySelector('[data-whip-composer][aria-label="Message WHIP"]');
    const session = enabled(composer) && enabled(composer?.closest('form')?.querySelector('[aria-label="Add context"]'));
    const home = !!document.querySelector('#workspace-path') && [...document.querySelectorAll('button')]
      .some(button => button.textContent?.trim() === 'Browse host' && enabled(button));
    const conversation = document.querySelector('[aria-label="Conversation"]');
    const transcript = visible(conversation) && [...conversation.querySelectorAll('[data-message-id]')].slice(0, 128)
      .some(message => (message.textContent || '').slice(0, 4096).includes('Verified Whip desktop startup fixture: 42'));
    return { host, noNotice, home, session, transcript, visible: document.visibilityState === 'visible', fonts: document.fonts.status === 'loaded' };
  };
  let state = inspect();
  let painted = false;
  if (state.host && state.noNotice && state.visible && state.fonts && (state.home || state.session)) {
    let timer;
    painted = await Promise.race([
      document.fonts.ready.then(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve(true))))),
      new Promise(resolve => { timer = setTimeout(() => resolve(false), 250); }),
    ]);
    clearTimeout(timer);
    state = inspect();
  }
  const shell = performance.getEntriesByName('whip-shell-ready', 'mark')[0]?.startTime;
  return { ...state, painted, now: performance.now(), shell: shell ?? null, pathname: location.pathname.slice(0, 1024) };
})()`;

interface Snapshot {
  now: number; shell: number | null; pathname: string;
  host: boolean; noNotice: boolean; home: boolean; session: boolean;
  transcript: boolean; visible: boolean; fonts: boolean; painted: boolean;
}
// For connected/usable these bracket the observation call, not the unknown
// instant the UI first became ready. Only upperMs is a readiness upper bound.
interface Timing { lowerMs: number; upperMs: number }
export interface StartupProbeOptions {
  userData: string;
  rendererDigest: string;
  /** Uses the normal main-process quit path after the fixture result is flushed. */
  quit(): void;
}

async function privateDirectory(directory: string) {
  const stat = await lstat(directory);
  if (!path.isAbsolute(directory) || !stat.isDirectory() || stat.isSymbolicLink() ||
      stat.uid !== process.getuid?.() || (stat.mode & 0o077) !== 0 || await realpath(directory) !== directory)
    throw new Error('Startup measurement requires a private fixture directory');
}

/** Attach once after window creation, before loadURL. Inert outside explicit fixtures. */
export async function attachStartupProbe(window: BrowserWindow, options: StartupProbeOptions): Promise<void> {
  if (process.env.WHIP_DESKTOP_FIXTURE !== '1' || process.env.WHIP_DESKTOP_STARTUP_PROBE !== '1') return;
  const runId = process.env.WHIP_DESKTOP_STARTUP_RUN ?? '';
  const started = process.env.WHIP_DESKTOP_STARTUP_NS ?? '';
  const expectedPath = process.env.WHIP_DESKTOP_STARTUP_ROUTE ?? '/';
  if (process.platform !== 'darwin' || !/^[a-f0-9]{32}$/.test(runId) || !/^\d{1,20}$/.test(started) ||
      !/^[a-f0-9]{64}$/.test(options.rendererDigest) || options.userData !== process.env.WHIP_DESKTOP_USER_DATA ||
      !process.env.WHIPCODE_HOME || !/^(?:\/|\/h\/[a-zA-Z0-9_-]{1,128}\/s\/[a-zA-Z0-9_-]{1,128})$/.test(expectedPath))
    throw new Error('Invalid startup fixture options');
  const parentStart = BigInt(started);
  const attached = process.hrtime.bigint();
  if (parentStart <= 0 || attached < parentStart || attached - parentStart > 60_000_000_000n)
    throw new Error('Invalid startup fixture clock');
  await privateDirectory(options.userData);
  await privateDirectory(process.env.WHIPCODE_HOME);
  // Exclusive creation refuses stale files and symlinks. Keep the same descriptor
  // for the initial PID record and final bounded report; never follow a later path.
  const file = await open(path.join(options.userData, 'startup.json'), 'wx', 0o600);
  const elapsed = (stamp = process.hrtime.bigint()) => Number(stamp - parentStart) / 1e6;
  const result = {
    schema: 1, runId, pid: process.pid, rendererDigest: options.rendererDigest,
    versions: { node: process.versions.node, electron: process.versions.electron, uv: process.versions.uv },
    state: 'collecting', target: expectedPath === '/' ? 'home' : 'retained-session',
    windowCreatedMs: elapsed(attached), domReadyMs: undefined as number | undefined,
    shell: undefined as Timing | undefined, connected: undefined as Timing | undefined,
    usable: undefined as Timing | undefined, finishedMs: undefined as number | undefined,
    checks: undefined as Omit<Snapshot, 'now' | 'shell' | 'pathname'> | undefined,
    instrumentation: { pollIntervalMs: 25, paintWaitBoundMs: 250, probes: 0, wallMs: 0, maxWallMs: 0 },
  };
  const write = async (handle: FileHandle) => {
    const bytes = Buffer.from(JSON.stringify(result) + '\n');
    if (bytes.length > 8192) throw new Error('Startup report exceeds its limit');
    for (let offset = 0; offset < bytes.length;) {
      const { bytesWritten } = await handle.write(bytes, offset, bytes.length - offset, offset);
      if (!bytesWritten) throw new Error('Could not write startup report');
      offset += bytesWritten;
    }
    await handle.truncate(bytes.length);
    await handle.datasync();
  };
  try { await write(file); } catch (error) { await file.close(); throw error; }
  let finished = false;
  let polling: ReturnType<typeof setTimeout> | undefined;
  const stop = () => {
    clearTimeout(deadline); clearTimeout(polling);
    window.webContents.removeListener('dom-ready', ready);
    window.webContents.removeListener('render-process-gone', failed);
    window.removeListener('closed', closed);
  };
  const finish = async (state: string) => {
    if (finished) return;
    finished = true; stop(); result.state = state; result.finishedMs = elapsed();
    try { await write(file); } finally { await file.close(); options.quit(); }
  };
  const finishQuietly = (state: string) => { void finish(state).catch(() => options.quit()); };
  const failed = () => finishQuietly('renderer-failed');
  const closed = () => finishQuietly('closed');
  const poll = async () => {
    if (finished) return;
    if (window.isDestroyed() || window.webContents.isDestroyed()) { closed(); return; }
    if (!isDesktopURL(window.webContents.getURL())) { finishQuietly('unexpected-origin'); return; }
    const before = process.hrtime.bigint();
    const snapshot: unknown = await window.webContents.executeJavaScript(snapshotScript);
    const after = process.hrtime.bigint();
    if (finished) return;
    if (!snapshot || typeof snapshot !== 'object') { failed(); return; }
    const value = snapshot as Snapshot;
    if (!Number.isFinite(value.now) || value.now < 0 || value.now > 60_000 ||
        (value.shell !== null && (!Number.isFinite(value.shell) || value.shell < 0 || value.shell > value.now)) ||
        typeof value.pathname !== 'string' || value.pathname.length > 1024 ||
        ['host', 'noNotice', 'home', 'session', 'transcript', 'visible', 'fonts', 'painted'].some(key => typeof value[key as keyof Snapshot] !== 'boolean')) {
      failed(); return;
    }
    const duration = Number(after - before) / 1e6;
    result.instrumentation.probes++; result.instrumentation.wallMs += duration;
    result.instrumentation.maxWallMs = Math.max(result.instrumentation.maxWallMs, duration);
    // Node's Darwin hrtime uses the host monotonic clock. Renderer time has a
    // different origin: bound its mark using the native call/return bracket.
    if (!result.shell && value.shell !== null) {
      const age = value.now - value.shell;
      result.shell = { lowerMs: Math.max(0, elapsed(before) - age), upperMs: Math.max(0, elapsed(after) - age) };
    }
    result.checks = { host: value.host, noNotice: value.noNotice, home: value.home, session: value.session,
      transcript: value.transcript, visible: value.visible, fonts: value.fonts, painted: value.painted };
    const connected = value.host && value.noNotice && value.visible &&
      (expectedPath === '/' ? value.home : value.session) && value.pathname === expectedPath;
    if (connected && !result.connected) result.connected = { lowerMs: elapsed(before), upperMs: elapsed(after) };
    if (connected && result.shell && value.fonts && value.painted && (expectedPath === '/' || value.transcript)) {
      result.usable = { lowerMs: elapsed(before), upperMs: elapsed(after) };
      await finish('complete'); return;
    }
    if (result.instrumentation.probes >= 1000) { finishQuietly('timeout'); return; }
    polling = setTimeout(() => { void poll().catch(failed); }, 25);
  };
  const ready = () => {
    if (result.domReadyMs !== undefined || finished) return;
    result.domReadyMs = elapsed(); void poll().catch(failed);
  };
  const deadline = setTimeout(() => finishQuietly('timeout'), 30_000);
  window.webContents.once('dom-ready', ready);
  window.webContents.once('render-process-gone', failed);
  window.once('closed', closed);
}
