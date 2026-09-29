// Diagnostic driver only. Loads the staged production main/preload unchanged;
// the backend is the owned native runtime with actual engines and a local fake provider.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { createHash } from 'node:crypto';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { _electron } from 'playwright';
import { deadline, eventually, repository } from './native-fixture.mjs';

const exec = promisify(execFile);

export async function isolateDesktopPerformance() {
  const directory = await mkdtemp('/tmp/whip-desktop-performance-');
  await mkdir(join(directory, 'user-data'), { mode: 0o700 });
  return { directory };
}

export async function launchDesktopPerformance(fixture, isolation, { executablePath } = {}) {
  const stage = join(repository, 'apps/desktop/.stage');
  const manifest = JSON.parse(await readFile(join(stage, 'app/renderer-manifest.json'), 'utf8'));
  const executable = join(isolation.directory, 'whipcode');
  await copyFile(join(stage, 'native/whipcode'), executable);
  const env = { PATH: join(fixture.directory, 'bin') + ':/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/sh',
    HOME: join(fixture.directory, 'home'), ZDOTDIR: join(fixture.directory, 'home'), TMPDIR: join(fixture.directory, 'tmp'),
    WHIPCODE_HOME: join(fixture.directory, 'home/.whipcode'), WHIPCODE_NETWORK: '0', WHIP_DESKTOP_FIXTURE: '1',
    WHIP_DESKTOP_EXECUTABLE: executable, WHIP_DESKTOP_USER_DATA: join(isolation.directory, 'user-data') };
  const status = JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);
  assert.equal(status.state, 'running');
  assert.equal(status.process.runtime_id, fixture.info.runtime_id); assert.equal(status.process.process_epoch, fixture.info.process_epoch);
  assert.equal(status.socket, fixture.info.socket);
  const fixturePID = status.process.pid;
  assert.ok(Number.isSafeInteger(fixturePID) && fixturePID > 0);
  isolation.env = env;
  const electron = await _electron.launch({ executablePath, args: [join(stage, 'app')], env, timeout: 30_000 });
  isolation.electron = electron;
  const electronProcess = electron.process();
  let stderr = '';
  electronProcess.stderr?.on('data', bytes => { stderr = (stderr + bytes.toString()).slice(-64 * 1024); });
  const pid = electronProcess.pid;
  await writeFile(join(isolation.directory, 'processes.json'), JSON.stringify({ electronPID: pid, daemonPID: fixturePID,
    daemonExecutable: join(fixture.directory, 'runtime'), stage, rendererDigest: manifest.digest, env }, null, 2));
  const page = await electron.firstWindow();
  const context = page.context();
  context.setDefaultTimeout(15_000);
  context.setDefaultNavigationTimeout(30_000);
  // A new test navigation must not interrupt the production initial load.
  await page.locator('#whip-session-navigation').waitFor({ state: 'visible', timeout: 15000 });
  // Passive IPC observations; the production listeners still send/ack every
  // frame. Store bounded metadata, never base64 content or full frame bodies.
  await electron.evaluate(({ ipcMain }) => {
    const state = { requests: [], requestCounts: Object.create(null), requestTotal: 0, prunedRequests: 0, maximumObservations: 0, overflow: false };
    const active = new Set();
    const sent = (_event, id, _sequence, frame) => {
      try {
        const message = JSON.parse(frame);
        if (typeof message.method !== 'string' || message.method.length > 128 || state.requestTotal >= 1_000_000 ||
            !Object.hasOwn(state.requestCounts, message.method) && Object.keys(state.requestCounts).length >= 256) throw new Error('Traffic probe bounds exceeded');
        if (message.method === 'trees.summaries' && (!Array.isArray(message.params?.root_ids) ||
            message.params.root_ids.length > 64 || message.params.root_ids.some(id => typeof id !== 'string' || id.length > 128))) throw new Error('Summary scope probe bounds exceeded');
        state.requestCounts[message.method] = (state.requestCounts[message.method] ?? 0) + 1;
        if (state.requests.length === 8192) { state.requests.splice(0, 4096); state.prunedRequests += 4096; }
        state.requests.push({ sequence: ++state.requestTotal, id: message.id, method: message.method, session: message.params?.session_id,
          roots: message.params?.root_ids?.length,
          rootIDs: message.method === 'trees.summaries' ? message.params.root_ids : undefined, frameBytes: Buffer.byteLength(frame), at: performance.now() });
        if (message.method === 'sessions.observe') active.add(id);
        state.maximumObservations = Math.max(state.maximumObservations, active.size);
      } catch { state.overflow = true; }
    };
    ipcMain.on('whip:sendTransport', sent); ipcMain.on('whip:closeTransport', (_event, id) => active.delete(id));
    globalThis.__whipPerformanceTraffic = () => ({ ...state, activeObservations: active.size });
  });
  return {
    electron, page, context, origin: 'whip-app://bundle', pid,
    version: await electron.evaluate(() => ({ electron: process.versions.electron, chromium: process.versions.chrome })),
    rendererDigest: manifest.digest,
    traffic: () => electron.evaluate(() => globalThis.__whipPerformanceTraffic()),
    async processMemory(phase) {
      const { stdout } = await exec('/bin/ps', ['-axo', 'pid=,ppid=,rss=,comm='], { maxBuffer: 4 << 20 });
      const rows = stdout.split('\n').flatMap(line => {
        const match = /^\s*(\d+)\s+(\d+)\s+(\d+)\s+(.+)$/.exec(line);
        return match ? [{ pid: Number(match[1]), ppid: Number(match[2]), rssKiB: Number(match[3]), executable: match[4] }] : [];
      });
      const tree = root => {
        const retained = new Set([root]); let changed = true;
        while (changed) { changed = false; for (const row of rows) if (retained.has(row.ppid) && !retained.has(row.pid)) { retained.add(row.pid); changed = true; } }
        return rows.filter(row => retained.has(row.pid));
      };
      const application = tree(pid), daemon = tree(fixturePID);
      assert(application.some(row => row.pid === pid) && daemon.some(row => row.pid === fixturePID), 'Measured process disappeared');
      return { phase, at: performance.now(), application, daemon,
        applicationRSSKiB: application.reduce((sum, row) => sum + row.rssKiB, 0),
        daemonRSSKiB: daemon.reduce((sum, row) => sum + row.rssKiB, 0) };
    },
    async saveDiagnostics() { await writeFile(join(isolation.directory, 'electron-stderr.log'), stderr); },
    async close() {
      await closeDesktopProcess(electron);
    },
  };
}

// Electron page emulation does not resize its native BrowserWindow. Keep the
// requested CSS workload inside real content bounds before measuring rendering.
export async function setDesktopViewport(host, size) {
  assert(Number.isSafeInteger(size.width) && Number.isSafeInteger(size.height) && size.width > 0 && size.height > 0);
  const window = await host.electron.browserWindow(host.page);
  try { await window.evaluate((window, size) => { window.setContentSize(size.width, size.height); window.center(); }, size); }
  finally { await window.dispose(); }
  await host.page.setViewportSize(size);
  const geometry = await assertDesktopViewport(host);
  assert.equal(geometry.native.content.width, size.width, 'Native content width differs from requested viewport');
  assert.equal(geometry.native.content.height, size.height, 'Native content height differs from requested viewport');
  return geometry;
}

export async function assertDesktopViewport(host, { composer = false } = {}) {
  const window = await host.electron.browserWindow(host.page);
  let native;
  try {
    native = await window.evaluate(window => ({ content: window.getContentBounds(), bounds: window.getBounds(),
      visible: window.isVisible(), minimized: window.isMinimized(), focused: window.isFocused() }));
  } finally { await window.dispose(); }
  const workArea = await host.electron.evaluate(({ screen }, bounds) => screen.getDisplayMatching(bounds).workArea, native.bounds);
  const document = await host.page.evaluate(composer => {
    const element = composer ? document.querySelector('[data-whip-composer]') : null;
    return { width: innerWidth, height: innerHeight, visibility: document.visibilityState,
      bounds: document.documentElement.getBoundingClientRect().toJSON(),
      composer: element?.getBoundingClientRect().toJSON() ?? null,
      composerFocused: element !== null && document.activeElement === element };
  }, composer);
  const inside = (inner, outer) => inner.x >= outer.x - 0.01 && inner.y >= outer.y - 0.01 &&
    inner.x + inner.width <= outer.x + outer.width + 0.01 && inner.y + inner.height <= outer.y + outer.height + 0.01;
  assert(native.visible && !native.minimized, 'Native window is not visible');
  assert(inside(native.content, workArea), 'Native content lies outside its display work area');
  assert.equal(document.visibility, 'visible');
  const content = { x: 0, y: 0, width: native.content.width, height: native.content.height };
  assert(document.width > 0 && document.height > 0 && inside({ ...content, width: document.width, height: document.height }, content),
    'Emulated document viewport exceeds native content bounds');
  assert(inside(document.bounds, content), 'Document lies outside native content bounds');
  if (composer) {
    assert(native.focused && document.composerFocused, 'Composer does not have native keyboard focus');
    assert(document.composer?.width > 0 && document.composer?.height > 0 && inside(document.composer, content),
      'Composer lies outside native content bounds');
  }
  return { native, workArea, document, boundary: 'Native window/content and CSS document/composer bounds; does not prove lack of OS occlusion or physical display latency.' };
}

// Bound cleanup even when Electron's main-thread inspector is unresponsive.
// Signals target only this launcher-owned child, never a discovered/user app.
export async function closeDesktopProcess(electron) {
  const child = electron.process();
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = new Promise(resolve => child.once('exit', resolve));
  let commandError;
  const command = electron.evaluate(({ app }) => app.exit(0)).catch(error => {
    if (!/closed|destroyed/.test(error.message)) commandError = error;
  });
  const signal = name => { if (child.exitCode === null && child.signalCode === null) child.kill(name); };
  const soft = setTimeout(() => signal('SIGTERM'), 1500);
  const hard = setTimeout(() => signal('SIGKILL'), 5000);
  let timer;
  try {
    await Promise.race([Promise.all([exited, command]), new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error('Owned Electron process/inspector did not join within 10 seconds')), 10000);
    })]);
    if (commandError) throw commandError;
  } finally { clearTimeout(soft); clearTimeout(hard); clearTimeout(timer); }
}

// Keep only 8192 recent frame metadata records; all method counts remain exact.
// Every timed probe must still have its complete interval, or fail explicitly.
function requestsSince(traffic, after) {
  assert.equal(traffic.overflow, false);
  assert((traffic.requests[0]?.sequence ?? after + 1) <= after + 1, 'Traffic probe interval was evicted');
  return traffic.requests.filter(request => request.sequence > after);
}

export async function exerciseDesktopTabs({ host, fixture, client, ready, frame, summarize, metrics }) {
  const { page, context } = host;
  const roots = [fixture.history.root_id];
  for (let index = 1; index < 32; index++) {
    roots.push((await fixture.createRoot(client)).root.id);
  }
  const snapshots = [];
  const inspector = await context.newCDPSession(page);
  const tab = id => page.locator(`#whip-workspace-tab-${id}`);
  const select = async id => {
    await tab(id).click();
    await eventually(async () => (await tab(id).getAttribute('aria-selected')) === 'true' &&
      new URL(page.url()).pathname === `/h/${fixture.info.runtime_id}/s/${id}`, { description: 'selected desktop tab route' });
    await ready();
  };
  for (const count of [1, 8, 32]) {
    await page.evaluate(({ runtimeId, roots }) => {
      const prefix = 'whip.desktop.window.main.';
      localStorage.setItem(prefix + 'whip.web.workspace.v3', JSON.stringify({ version: 3, migrated: [], workspace: {
        layout: { type: 'pane', id: 'main', selected: roots[0], tabs: roots.map(rootId => ({ id: rootId,
          kind: 'chat', runtimeId, rootId, titleHint: rootId, location: {} })) },
        focusedPaneId: 'main', closed: [], restoreSelection: true } }));
    }, { runtimeId: fixture.info.runtime_id, roots: roots.slice(0, count) });
    await page.goto(`${host.origin}/h/${fixture.info.runtime_id}/s/${roots[0]}`); await ready();
    assert.equal(await page.getByRole('tab').count(), count);
    for (const id of roots.slice(1, Math.min(count, 4))) await select(id);
    await select(roots[0]); await frame();
    await inspector.send('HeapProfiler.collectGarbage');
    const traffic = await host.traffic();
    assert.equal(traffic.overflow, false); assert(traffic.maximumObservations <= 16);
    snapshots.push({ tabs: count, heap: await inspector.send('Runtime.getHeapUsage'),
      activeObservations: traffic.activeObservations, memory: await host.processMemory(`tabs-${count}`),
      metadataBytes: await page.evaluate(() => new TextEncoder().encode(localStorage.getItem('whip.desktop.window.main.whip.web.workspace.v3')).length) });
  }
  const switches = [];
  for (let index = 0; index < 20; index++) {
    const started = performance.now();
    await select(roots[index % 4]); await frame(); switches.push(performance.now() - started);
  }
  await select(roots[0]);
  const sidebarIDs = await page.locator('[data-sidebar-session]').evaluateAll(rows => rows.map(row => row.dataset.sidebarSession).sort());
  const before = (await host.traffic()).requestTotal;
  await page.waitForTimeout(6100);
  const traffic = await host.traffic();
  const summaries = requestsSince(traffic, before).filter(request => request.method === 'trees.summaries');
  const summaryPolling = summaryPollEvidence(summaries, roots, sidebarIDs);
  assert(traffic.maximumObservations <= 16); assert.equal(traffic.overflow, false);
  assert((await page.getByRole('region', { name: 'Conversation', exact: true }).count()) <= 1);
  metrics.desktopTabs = { snapshots, cachedSwitchMilliseconds: summarize(switches), maximumObservations: traffic.maximumObservations,
    summaryPollingIn6100ms: summaryPolling, boundary: 'Playwright click through ready and two animation frames; includes automation overhead.' };
  metrics.checks.push('Staged IPC: 1/8/32 restored metadata tabs, <=16 native session observation waits, bounded aggregate polling for tabs and visible sidebar (identical root sets remain unattributed), 20 cached switches');
  await inspector.detach();
  return roots;
}

// The two independent metadata owners can request exactly the same roots.
// Wire evidence cannot distinguish them then; preserve that ambiguity instead
// of assigning both to the tab owner merely because their counts match.
export function summaryPollEvidence(requests, tabIDs, sidebarIDs) {
  const key = ids => JSON.stringify([...ids].sort());
  assert(sidebarIDs.length > 0 && sidebarIDs.length <= 32);
  const tabs = key(tabIDs), sidebar = key(sidebarIDs);
  assert(requests.every(request => key(request.rootIDs) === tabs || key(request.rootIDs) === sidebar), 'Unexpected summary root scope');
  if (tabs === sidebar) {
    assert(requests.length >= 4 && requests.length <= 8, `Expected two bounded aggregate poll owners, received ${requests.length}`);
    return { sameRootSet: true, combinedPolls: requests.length, tabPolls: null, sidebarPolls: null };
  }
  const tabPolls = requests.filter(request => key(request.rootIDs) === tabs).length;
  const sidebarPolls = requests.filter(request => key(request.rootIDs) === sidebar).length;
  assert(tabPolls >= 2 && tabPolls <= 4, `Expected one shared tab summary poll, received ${tabPolls}`);
  assert(sidebarPolls >= 2 && sidebarPolls <= 4, `Expected one visible-sidebar aggregate poll, received ${sidebarPolls}`);
  return { sameRootSet: false, combinedPolls: requests.length, tabPolls, sidebarPolls };
}

export async function exerciseDesktopTransfer({ host, fixture, client, metrics, directory, summarize }) {
  const { page } = host;
  const bitmap = (width, height, fill) => {
    const pixels = width * height * 4, data = Buffer.alloc(54 + pixels, fill);
    data.fill(0, 0, 54); data.write('BM'); data.writeUInt32LE(data.length, 2); data.writeUInt32LE(54, 10);
    data.writeUInt32LE(40, 14); data.writeInt32LE(width, 18); data.writeInt32LE(height, 22);
    data.writeUInt16LE(1, 26); data.writeUInt16LE(32, 28); data.writeUInt32LE(pixels, 34);
    return data;
  };
  // Real owned files exercise the native file chooser. Buffer payloads make
  // Playwright manufacture Files via a large renderer-side base64 conversion,
  // which would measure test-driver CPU and memory as application upload cost.
  const inputDirectory = join(directory, 'transfer-inputs');
  await mkdir(inputDirectory, { mode: 0o700 });
  const oversized = bitmap(2048, 1024, 0x7f);
  const oversizedPath = join(inputDirectory, 'oversized.bmp');
  await writeFile(oversizedPath, oversized);
  const beforeRejected = (await host.traffic()).requestCounts['content.put'] ?? 0;
  await page.locator('input[type=file]').setInputFiles(oversizedPath);
  const rejected = page.locator('[data-error-type=resource]').filter({ hasText: 'oversized.bmp could not upload' });
  await rejected.locator('summary').click();
  await rejected.getByText('Attachments are limited to 4 MiB per file.', { exact: true }).waitFor();
  assert.equal((await host.traffic()).requestCounts['content.put'] ?? 0, beforeRejected);
  await page.getByRole('button', { name: 'Remove oversized.bmp', exact: true }).click();
  const files = Array.from({ length: 3 }, (_, index) => ({ name: `desktop-performance-${index}.bmp`,
    mimeType: 'image/bmp', buffer: bitmap(1024, 768, 0x70 + index) }));
  const inputPaths = await Promise.all(files.map(async file => { const path = join(inputDirectory, file.name); await writeFile(path, file.buffer); return path; }));
  const uploads = files.map(file => ({ name: file.name, bytes: file.buffer.length,
    digest: createHash('sha256').update(file.buffer).digest('hex') }));
  const samples = [];
  metrics.desktopTransfer = { inputBoundary: 'Native file input over actual files in the disposable fixture; no Playwright buffer-to-File injection.', rejectedSingleFileBytes: oversized.length,
    uploadedBytes: uploads.reduce((sum, item) => sum + item.bytes, 0), uploads, memorySamples: samples };
  assert(metrics.desktopTransfer.uploadedBytes > 8 << 20);
  const capture = phase => host.processMemory(phase).then(sample => {
    if (samples.length >= 256) throw new Error('Transfer memory probe overflow'); samples.push(sample);
  });
  let sampling = true, samplingError;
  const sampler = (async () => { while (sampling) { await capture('transfer'); await new Promise(resolve => setTimeout(resolve, 100)); } })()
    .catch(error => { samplingError = error; });
  const before = (await host.traffic()).requestTotal;
  const started = performance.now();
  let uploadMs, downloadMs, scheduleID;
  const typing = [], session = client.session(fixture.history.root_id);
  try {
    const upload = page.locator('input[type=file]').setInputFiles(inputPaths)
      .then(() => eventually(async () => {
        const handles = await page.evaluate(() => window.__performanceContentHandles);
        return uploads.every(item => handles.some(handle => handle.digest === item.digest && handle.size === String(item.bytes)));
      }, { description: 'three exact scoped native content upload receipts' }))
      .then(() => { uploadMs = performance.now() - started; return {}; }, error => ({ error }));
    await page.getByLabel('Message WHIP', { exact: true }).focus();
    for (let index = 0; index < 12; index++) {
      const start = performance.now(); await page.keyboard.type('x');
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      typing.push({ milliseconds: performance.now() - start, uploadStillPending: uploadMs === undefined });
    }
    const uploaded = await upload;
    if (uploaded.error) throw uploaded.error;
    const handles = await page.evaluate(() => window.__performanceContentHandles);
    for (const [index, item] of uploads.entries()) {
      const handle = handles.find(handle => handle.digest === item.digest && handle.size === String(item.bytes));
      const reference = await session.content.get(handle.id, deadline());
      assert.equal(reference.session_id, session.id);
      assert.deepEqual(Buffer.from(await session.content.readBytes(reference, deadline())), files[index].buffer);
      await page.getByRole('button', { name: `Remove ${item.name}`, exact: true }).click();
    }
    // This is explicit existing owner-scoped content, not an externalized tool
    // result. The current scheduled-attachment inspector uses production
    // ContentRead and the same bounded native save path. It never fires.
    scheduleID = crypto.randomUUID();
    await session.schedules.create({ expression: '@at 2099-01-01T00:00:00Z', parts: [
      { type: 'text', text: 'Desktop performance download evidence' },
      { type: 'content', reference_id: fixture.history.large_content },
    ] }, scheduleID, deadline());
    await page.goto(`${host.origin}/h/${fixture.info.runtime_id}/s/${session.id}?panel=goals`);
    const details = page.getByRole('dialog', { name: 'Session details', exact: true });
    await details.getByRole('button', { name: 'Read scheduled prompt', exact: true }).click();
    const output = join(directory, 'desktop-scoped-content.txt');
    await host.electron.evaluate(({ dialog }, output) => {
      // Only save-dialog selection is substituted. Production IPC, bounded
      // writes, fsync and atomic rename remain unchanged.
      dialog.showSaveDialog = async () => ({ canceled: false, filePath: output });
    }, output);
    const downloadStarted = performance.now();
    await details.getByRole('button', { name: 'Download', exact: true }).click();
    const bytes = await eventually(async () => readFile(output), { description: 'native saved content file' });
    downloadMs = performance.now() - downloadStarted;
    const reference = await session.content.get(fixture.history.large_content, deadline());
    assert.equal(bytes.length, 1_400_000);
    assert.equal(createHash('sha256').update(bytes).digest('hex'), reference.digest);
    await details.getByRole('button', { name: 'Close', exact: true }).click();
    const operations = requestsSince(await host.traffic(), before);
    const puts = operations.filter(request => request.method === 'content.put');
    assert.equal(puts.length, 3); assert(puts.every(request => request.session === session.id));
    assert(operations.some(request => request.method === 'content.read' && request.session === session.id));
    assert(operations.every(request => request.frameBytes < 8 << 20));
    Object.assign(metrics.desktopTransfer, { downloadedBytes: bytes.length, uploadMs, downloadMs,
      typing: summarize(typing.map(sample => sample.milliseconds)), keySamples: typing,
      concurrentTypingSamples: typing.filter(sample => sample.uploadStillPending).length,
      uploadRequests: puts.length, maximumFrameBytes: Math.max(...operations.map(request => request.frameBytes)),
      savedSHA256: reference.digest,
      boundary: 'Staged native IPC: three bounded content.put requests totaling over 8 MiB, exact owner/digest/byte rereads, and scoped 1.4 MiB content.read followed by production 256 KiB native save chunks/fsync/rename. Save-dialog destination alone is substituted. The original 8 MiB single file is explicitly rejected before transfer; no old upload.chunk or WAN/transfer-ceiling claim.' });
    metrics.checks.push('Staged IPC: over-limit single attachment rejected; >8MiB aggregate valid attachments preserve scoped exact bytes while typing; scheduled attachment downloads through real native save and is cancelled without firing');
  } finally {
    sampling = false; await sampler;
    if (scheduleID) await session.schedules.cancel(scheduleID, deadline());
    if (samplingError) throw samplingError;
  }
  await capture('after-transfer');
  metrics.desktopTransfer.maximumApplicationRSSKiB = Math.max(...samples.map(sample => sample.applicationRSSKiB));
  metrics.desktopTransfer.memoryCaveat = '100ms process RSS samples may miss brief peaks; sums can double-count shared pages. Native backend RSS is separate.';
}

export async function finishDesktopPerformance(isolation, host, fixture, succeeded, priorFailures = []) {
  const failures = [...priorFailures];
  if (host) {
    await host.saveDiagnostics().catch(error => failures.push(error));
    await host.close().catch(error => failures.push(error));
  } else if (isolation.electron) {
    await closeDesktopProcess(isolation.electron).catch(error => failures.push(error));
  }
  if (fixture) await fixture.close().catch(error => failures.push(error));
  if (!succeeded || failures.length) {
    await writeFile(join(isolation.directory, 'cleanup.json'), JSON.stringify({ succeeded, errors: failures.map(error => String(error)) }, null, 2));
    console.error(`Desktop performance fixture retained: ${isolation.directory}`);
  } else await rm(isolation.directory, { recursive: true, force: true });
  if (failures.length) throw new AggregateError(failures, `Cleanup failed; retained ${isolation.directory}`);
}
