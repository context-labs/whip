// Diagnostic driver only. Loads the staged production main/preload unchanged;
// the daemon is the existing isolated Go fixture with a synthetic runner.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { _electron } from 'playwright';
import { eventually, repository } from '../../../packages/sdk/scripts/fixture.mjs';

const exec = promisify(execFile);

export async function isolateDesktopPerformance() {
  // Resolve caches before isolating HOME; compiling a fixture should not fetch
  // dependencies afresh or accidentally inherit provider/account credentials.
  const { stdout } = await exec('go', ['env', 'GOCACHE', 'GOMODCACHE']);
  const [cache, modules] = stdout.trim().split('\n');
  const directory = await mkdtemp('/tmp/whip-desktop-performance-');
  const env = { PATH: process.env.PATH, GOCACHE: cache, GOMODCACHE: modules,
    HOME: join(directory, 'home'), TMPDIR: join(directory, 'tmp'),
    WHIP_HOME: join(directory, 'unused-home'), WHIP_DESKTOP_USER_DATA: join(directory, 'user-data'),
    WHIP_WEB_PERF_FIXTURE: '1', WHIP_SDK_KEEP_FIXTURE: '1', WHIP_DESKTOP_FIXTURE: '1' };
  for (const key of ['USER', 'LOGNAME', 'LANG', 'LC_ALL', 'DEVELOPER_DIR', 'SDKROOT'])
    if (process.env[key]) env[key] = process.env[key];
  await Promise.all(['HOME', 'TMPDIR', 'WHIP_HOME', 'WHIP_DESKTOP_USER_DATA'].map(key => mkdir(env[key], { mode: 0o700 })));
  for (const key of Object.keys(process.env)) delete process.env[key];
  Object.assign(process.env, env);
  return { directory, env };
}

export async function launchDesktopPerformance(fixture, isolation) {
  const stage = join(repository, 'apps/desktop/.stage');
  const manifest = JSON.parse(await readFile(join(stage, 'app/renderer-manifest.json'), 'utf8'));
  const env = { ...isolation.env, WHIP_HOME: join(fixture.directory, 'home') };
  const status = JSON.parse((await exec(join(stage, 'native/whip'), ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);
  assert.equal(status.state, 'running'); assert.equal(status.pid, fixture.pid);
  await writeFile(join(isolation.directory, 'processes.json'), JSON.stringify({ daemonPID: fixture.pid,
    daemonExecutable: join(fixture.directory, 'daemon.test'), stage, rendererDigest: manifest.digest, env }, null, 2));
  const electron = await _electron.launch({ args: [join(stage, 'app')], env, timeout: 30_000 });
  isolation.electron = electron;
  const electronProcess = electron.process();
  let stderr = '';
  electronProcess.stderr?.on('data', bytes => { stderr = (stderr + bytes.toString()).slice(-64 * 1024); });
  const pid = electronProcess.pid;
  await writeFile(join(isolation.directory, 'processes.json'), JSON.stringify({ electronPID: pid, daemonPID: fixture.pid,
    daemonExecutable: join(fixture.directory, 'daemon.test'), stage, rendererDigest: manifest.digest, env }, null, 2));
  const page = await electron.firstWindow();
  const context = page.context();
  context.setDefaultTimeout(15_000);
  context.setDefaultNavigationTimeout(30_000);
  // Passive IPC observations; the production listeners still send/ack every
  // frame. Store bounded metadata, never base64 content or full frame bodies.
  await electron.evaluate(({ ipcMain }) => {
    const state = { requests: [], maximumSubscriptions: 0, overflow: false };
    const active = new Map();
    const sent = (_event, id, _sequence, frame) => {
      try {
        const message = JSON.parse(frame);
        if (state.requests.length >= 8192) { state.overflow = true; return; }
        state.requests.push({ id: message.id, method: message.method, operation: message.params?.operation,
          subscription: message.params?.subscription_id, root: message.params?.root_id, agent: message.params?.agent_id,
          frameBytes: Buffer.byteLength(frame), at: performance.now() });
        let subscriptions = active.get(id);
        if (!subscriptions) { subscriptions = new Set(); active.set(id, subscriptions); }
        if (message.method === 'events.subscribe') subscriptions.add(message.params.subscription_id);
        if (message.method === 'events.unsubscribe') subscriptions.delete(message.params.subscription_id);
        const count = [...active.values()].reduce((sum, set) => sum + set.size, 0);
        state.maximumSubscriptions = Math.max(state.maximumSubscriptions, count);
      } catch { state.overflow = true; }
    };
    const closed = (_event, id) => active.delete(id);
    ipcMain.on('whip:sendTransport', sent); ipcMain.on('whip:closeTransport', closed);
    globalThis.__whipPerformanceTraffic = () => ({ ...state,
      activeSubscriptions: [...active.values()].reduce((sum, set) => sum + set.size, 0) });
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
      const application = tree(pid), daemon = tree(fixture.pid);
      assert(application.some(row => row.pid === pid) && daemon.some(row => row.pid === fixture.pid), 'Measured process disappeared');
      return { phase, at: performance.now(), application, daemon,
        applicationRSSKiB: application.reduce((sum, row) => sum + row.rssKiB, 0),
        daemonRSSKiB: daemon.reduce((sum, row) => sum + row.rssKiB, 0) };
    },
    async saveDiagnostics() { await writeFile(join(isolation.directory, 'electron-stderr.log'), stderr); },
    async close() {
      await electron.evaluate(({ app }) => app.exit(0)).catch(error => {
        if (!/closed|destroyed/.test(error.message)) throw error;
      });
      await eventually(() => electronProcess.exitCode !== null || electronProcess.signalCode !== null,
        { timeout: 10_000, description: 'fixture Electron process exit' });
    },
  };
}

export async function exerciseDesktopTabs({ host, fixture, client, ready, frame, summarize, metrics }) {
  const { page, context } = host;
  const roots = [fixture.info.root_id];
  for (let index = 1; index < 32; index++) {
    const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
    assert.equal(created.status, 'succeeded'); roots.push(created.result.root_id);
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
      localStorage.removeItem(prefix + 'whip.web.workspace.v2');
      localStorage.setItem(prefix + 'whip.web.tabs.v1', JSON.stringify({ version: 1, workspaces: [{ runtimeId,
        tabs: roots.map(rootId => ({ rootId, titleHint: rootId, location: {} })), closed: [], lastActiveRootId: roots[0] }] }));
    }, { runtimeId: fixture.info.runtime_id, roots: roots.slice(0, count) });
    await page.goto(`${host.origin}/h/${fixture.info.runtime_id}/s/${roots[0]}`); await ready();
    assert.equal(await page.getByRole('tab').count(), count);
    for (const id of roots.slice(1, Math.min(count, 4))) await select(id);
    await select(roots[0]); await frame();
    await inspector.send('HeapProfiler.collectGarbage');
    const traffic = await host.traffic();
    assert.equal(traffic.overflow, false); assert(traffic.maximumSubscriptions <= 4);
    snapshots.push({ tabs: count, heap: await inspector.send('Runtime.getHeapUsage'),
      activeSubscriptions: traffic.activeSubscriptions, memory: await host.processMemory(`tabs-${count}`),
      metadataBytes: await page.evaluate(() => new TextEncoder().encode(localStorage.getItem('whip.desktop.window.main.whip.web.workspace.v2')).length) });
  }
  const switches = [];
  for (let index = 0; index < 20; index++) {
    const started = performance.now();
    await select(roots[index % 4]); await frame(); switches.push(performance.now() - started);
  }
  await select(roots[0]);
  const before = (await host.traffic()).requests.length;
  await page.waitForTimeout(6100);
  const traffic = await host.traffic();
  const polls = traffic.requests.slice(before).filter(request => request.method === 'sessions.summaries').length;
  assert(polls >= 2 && polls <= 4, `Expected one shared summary poll, received ${polls}`);
  assert(traffic.maximumSubscriptions <= 4); assert.equal(traffic.overflow, false);
  assert((await page.getByRole('region', { name: 'Conversation', exact: true }).count()) <= 1);
  metrics.desktopTabs = { snapshots, cachedSwitchMilliseconds: summarize(switches), maximumSubscriptions: traffic.maximumSubscriptions,
    summaryPollsIn6100ms: polls, boundary: 'Playwright click through ready and two animation frames; includes automation overhead.' };
  metrics.checks.push('Staged IPC: 1/8/32 restored metadata tabs, <=4 root subscriptions, one shared summary poll, 20 cached switches');
  await inspector.detach();
  return roots;
}

export async function exerciseDesktopTransfer({ host, fixture, metrics, directory, summarize }) {
  const { page } = host;
  // A valid uncompressed 32-bit BMP: 8 MiB of deterministic pixel data, below
  // the existing 20 MiB aggregate attachment limit. No provider call is made.
  const pixels = 2048 * 1024 * 4;
  const bmp = Buffer.alloc(54 + pixels, 0x7f);
  bmp.fill(0, 0, 54); bmp.write('BM'); bmp.writeUInt32LE(bmp.length, 2); bmp.writeUInt32LE(54, 10);
  bmp.writeUInt32LE(40, 14); bmp.writeInt32LE(2048, 18); bmp.writeInt32LE(1024, 22);
  bmp.writeUInt16LE(1, 26); bmp.writeUInt16LE(32, 28); bmp.writeUInt32LE(pixels, 34);
  const samples = [];
  const uploadSHA256 = createHash('sha256').update(bmp).digest('hex');
  metrics.desktopTransfer = { uploadedBytes: bmp.length, uploadSHA256, memorySamples: samples };
  const capture = phase => host.processMemory(phase).then(sample => { if (samples.length >= 256) throw new Error('Transfer memory probe overflow'); samples.push(sample); });
  let sampling = true;
  let samplingError;
  const sampler = (async () => { while (sampling) { await capture('transfer'); await new Promise(resolve => setTimeout(resolve, 100)); } })()
    .catch(error => { samplingError = error; });
  const before = (await host.traffic()).requests.length;
  const started = performance.now();
  let uploadMs, downloadMs;
  const typing = [];
  try {
    const upload = page.locator('input[type=file]').setInputFiles({ name: 'desktop-performance.bmp', mimeType: 'image/bmp', buffer: bmp })
      .then(() => page.getByText('desktop-performance.bmp · Ready', { exact: true }).waitFor())
      .then(() => { uploadMs = performance.now() - started; return {}; }, error => ({ error }));
    await page.getByLabel('Message WHIP', { exact: true }).focus();
    for (let index = 0; index < 12; index++) {
      const start = performance.now(); await page.keyboard.type('x');
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      typing.push({ milliseconds: performance.now() - start, uploadStillPending: uploadMs === undefined });
    }
    const uploaded = await upload;
    if (uploaded.error) throw uploaded.error;
    const uploadedHandles = await page.evaluate(() => window.__performanceContentHandles ?? []);
    assert(uploadedHandles.some(handle => handle.digest === uploadSHA256 && handle.size === String(bmp.length)), 'Upload result does not match fixture bytes');
    await page.getByRole('button', { name: 'Remove desktop-performance.bmp', exact: true }).click();
    // The existing seeded tool body is larger than the inline/read-preview
    // limit. Its download still uses scoped content.read through the real SDK.
    const conversation = page.getByRole('region', { name: 'Conversation', exact: true });
    const stored = page.getByRole('button', { name: /^Read stored message · /, includeHidden: true });
    await conversation.evaluate(element => { element.scrollTop = element.scrollHeight; });
    for (let index = 0; index < 24 && !await stored.count(); index++) {
      await conversation.evaluate(element => { element.scrollTop -= 500; });
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(resolve)));
    }
    await stored.first().evaluate(button => {
      const details = button.closest('details');
      if (details && !details.open) details.querySelector('summary').click();
    });
    await stored.first().click();
    const dialog = page.getByRole('dialog', { name: 'Stored message', exact: true });
    const output = join(directory, 'desktop-stored-message.json');
    await host.electron.evaluate(({ dialog }, output) => {
      // Only user selection is substituted in this diagnostic. Production IPC,
      // chunk validation, file writes, fsync and rename remain unmodified.
      dialog.showSaveDialog = async () => ({ canceled: false, filePath: output });
    }, output);
    const downloadStarted = performance.now();
    await dialog.getByRole('button', { name: 'Download stored message', exact: true }).click();
    const bytes = await eventually(async () => readFile(output), { description: 'native saved content file' });
    downloadMs = performance.now() - downloadStarted;
    assert(bytes.length > 1 << 20 && bytes.length < 2 << 20);
    const hashes = await page.evaluate(() => window.__performanceContentHandles ?? []);
    assert(hashes.some(handle => handle.digest === createHash('sha256').update(bytes).digest('hex') && handle.size === String(bytes.length)), 'Saved content does not match the scoped response digest');
    await dialog.getByRole('button', { name: 'Close', exact: true }).click();
    const traffic = await host.traffic();
    const operations = traffic.requests.slice(before);
    assert(operations.some(request => request.method === 'upload.begin' && request.root === fixture.info.root_id && request.agent === fixture.info.root_id), 'Upload used the wrong root/agent scope');
    assert(operations.filter(request => request.method === 'upload.chunk').length > 1);
    assert(operations.filter(request => request.method === 'content.read').length > 1);
    assert(operations.every(request => request.frameBytes <= 1 << 20));
    Object.assign(metrics.desktopTransfer, { downloadedBytes: bytes.length, uploadMs, downloadMs,
      typing: summarize(typing.map(sample => sample.milliseconds)), keySamples: typing,
      concurrentTypingSamples: typing.filter(sample => sample.uploadStillPending).length,
      uploadChunks: operations.filter(request => request.method === 'upload.chunk').length,
      downloadChunks: operations.filter(request => request.method === 'content.read').length,
      savedSHA256: createHash('sha256').update(bytes).digest('hex'),
      boundary: 'Loopback staged IPC upload and scoped download; save-dialog selection is substituted, actual native writes are exercised. Typing samples include automation and two animation frames. No SSH/WAN delay or transfer-ceiling claim.' });
    metrics.checks.push('Staged IPC: multi-MiB file upload and scoped content download use multiple bounded chunks, saved bytes match the content digest');
  } finally { sampling = false; await sampler; if (samplingError) throw samplingError; }
  await capture('after-transfer');
  metrics.desktopTransfer.maximumApplicationRSSKiB = Math.max(...samples.map(sample => sample.applicationRSSKiB));
  metrics.desktopTransfer.memoryCaveat = '100ms process RSS samples may miss brief peaks; sums can double-count shared pages. Synthetic daemon RSS is separate.';
}

export async function finishDesktopPerformance(isolation, host, fixture, succeeded, priorFailures = []) {
  const failures = [...priorFailures];
  if (host) {
    await host.saveDiagnostics().catch(error => failures.push(error));
    await host.close().catch(error => failures.push(error));
  } else if (isolation.electron) {
    await isolation.electron.close().catch(error => failures.push(error));
  }
  if (fixture) await fixture.close().catch(error => failures.push(error));
  if (!succeeded || failures.length) {
    await writeFile(join(isolation.directory, 'cleanup.json'), JSON.stringify({ succeeded, errors: failures.map(error => String(error)) }, null, 2));
    console.error(`Desktop performance fixture retained: ${isolation.directory}`);
  } else await rm(isolation.directory, { recursive: true, force: true });
  if (failures.length) throw new AggregateError(failures, `Cleanup failed; retained ${isolation.directory}`);
}
