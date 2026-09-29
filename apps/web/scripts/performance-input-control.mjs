// Causal diagnostic only: one idle root, no history/streams/32-tab workload.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdir, mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { promisify } from 'node:util';
import { startFixture, eventually } from './native-fixture.mjs';
import { isolateDesktopPerformance, launchDesktopPerformance, setDesktopViewport, assertDesktopViewport, finishDesktopPerformance } from './performance-desktop.mjs';
import { startKeyboardProbe, keyboardResult } from './performance-keyboard.mjs';
import { sampleBrowserRetention } from './performance-retention.mjs';

const base = resolve(process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-performance-input-controls');
await mkdir(base, { recursive: true });
const directory = await mkdtemp(join(base, 'run-'));
const report = { boundary: 'Isolated idle production composer vs native textarea with matching geometry/typography. Not the 32-tab/16-stream/full-history acceptance workload, not physical display latency.',
  recordedAt: new Date().toISOString(), cases: [], errors: [] };
let isolation, fixture, host, client, watchdog, closing, timedOut, succeeded = false;
try {
  report.source = (await promisify(execFile)('git', ['rev-parse', 'HEAD'], { timeout: 5000 })).stdout.trim();
  isolation = await isolateDesktopPerformance();
  fixture = await startFixture({ managedDirectory: true });
  client = await fixture.connect();
  const { root } = await fixture.createRoot(client, { title: 'Isolated keyboard diagnostic' });
  host = await launchDesktopPerformance(fixture, isolation);
  watchdog = setTimeout(() => {
    timedOut = new Error('Keyboard control exceeded 90 seconds');
    closing = host.close().catch(error => report.errors.push(String(error)));
  }, 90000);
  const { page, context } = host;
  page.on('pageerror', error => { if (report.errors.length < 64) report.errors.push(String(error)); });
  report.version = host.version; report.rendererDigest = host.rendererDigest;
  report.viewport = await setDesktopViewport(host, { width: 1360, height: 960 });
  await page.goto(`${host.origin}/h/${fixture.info.runtime_id}/s/${root.id}`);
  const composer = page.locator('[data-whip-composer]');
  await eventually(() => composer.isEnabled());
  await page.evaluate(() => document.fonts.ready);
  if (process.env.WHIP_WEB_PERFORMANCE_INSPECT === '1') {
    await writeFile(join(directory, 'inspection-ready.json'), JSON.stringify({ pid: host.pid, isolation: isolation.directory,
      fixture: fixture.directory, app: resolve('node_modules/electron/dist/Electron.app') }, null, 2), { mode: 0o600 });
    console.log(`Awaiting owned-window inspection: ${directory}`);
    await eventually(async () => (await readFile(join(directory, 'inspection-release'), 'utf8')) === 'continue\n',
      { timeout: 45000, interval: 100, description: 'explicit diagnostic inspection release' });
    report.inspection = 'Opt-in pause released before keyboard cases; external owned-window observation/focus evidence must be recorded separately.';
  }
  const frame = () => page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  for (const length of [0, 32768, 255543]) {
    const value = 'a'.repeat(length);
    await composer.fill(value); await frame();
    assert.equal(await composer.inputValue(), value);
    for (const kind of ['composer', 'plain-textarea']) {
      const selector = kind === 'composer' ? '[data-whip-composer]' : '[data-keyboard-control]';
      if (kind === 'plain-textarea') await page.evaluate(value => {
        const source = document.querySelector('[data-whip-composer]'), rect = source.getBoundingClientRect(), style = getComputedStyle(source);
        const control = document.createElement('textarea'); control.dataset.keyboardControl = '';
        control.setAttribute('aria-label', 'Diagnostic native textarea'); control.value = value;
        control.spellcheck = source.spellcheck; control.wrap = source.wrap;
        for (const property of ['font', 'font-family', 'font-size', 'font-weight', 'line-height', 'letter-spacing', 'padding', 'border', 'box-sizing', 'white-space', 'overflow-wrap', 'word-break', 'color', 'background-color'])
          control.style.setProperty(property, style.getPropertyValue(property));
        Object.assign(control.style, { position: 'fixed', left: `${rect.x}px`, top: `${rect.y}px`, width: `${rect.width}px`, height: `${rect.height}px`, zIndex: '2147483647', margin: '0', resize: 'none' });
        document.body.append(control);
      }, value);
      const target = page.locator(selector);
      await target.focus();
      await target.evaluate(element => { element.setSelectionRange(element.value.length, element.value.length); element.scrollTop = element.scrollHeight; });
      await frame();
      const before = await assertDesktopViewport(host, { composer: kind === 'composer' });
      assert(before.native.focused);
      const geometry = await target.evaluate(element => {
        const box = element.getBoundingClientRect();
        if (document.activeElement !== element || box.width <= 0 || box.height <= 0 || box.x < 0 || box.y < 0 || box.right > innerWidth || box.bottom > innerHeight)
          throw new Error('Control is not focused and fully visible');
        const computed = getComputedStyle(element);
        return { bounds: box.toJSON(), characters: element.value.length, font: computed.font, lineHeight: computed.lineHeight,
          selection: [element.selectionStart, element.selectionEnd] };
      });
      assert.equal(geometry.characters, length); assert.deepEqual(geometry.selection, [length, length]);
      await page.evaluate(startKeyboardProbe, selector);
      for (let index = 0; index < 40; index++) { await page.keyboard.type('x'); await frame(); }
      await frame(); await page.waitForTimeout(250);
      const timing = keyboardResult(await page.evaluate(() => window.__finishKeyboardProbe()));
      assert.equal(await target.inputValue(), value + 'x'.repeat(40));
      assert.deepEqual(await target.evaluate(element => [element.selectionStart, element.selectionEnd]), [length + 40, length + 40]);
      const after = await assertDesktopViewport(host, { composer: kind === 'composer' });
      assert(after.native.focused);
      report.cases.push({ kind, length, geometry, before, after, timing,
        memory: await host.processMemory(`${kind}-${length}-natural`), retention: await sampleBrowserRetention(context, page) });
      console.log(JSON.stringify({ kind, length, p95: timing.p95Rounded, lower: timing.lower.p95, upper: timing.upper.p95 }));
      if (kind === 'plain-textarea') await target.evaluate(element => element.remove());
    }
  }
  report.traffic = await host.traffic(); assert.equal(report.traffic.overflow, false);
  assert.equal(report.traffic.requestCounts['sessions.submit'] ?? 0, 0);
  assert.deepEqual(report.errors, []);
  if (timedOut) throw timedOut;
  succeeded = true;
} catch (error) {
  report.failure = String(error.stack ?? error);
  if (host) {
    report.failureViewport = await assertDesktopViewport(host).catch(error => ({ error: String(error) }));
    report.failureFocus = await host.page.evaluate(() => ({ documentFocused: document.hasFocus(),
      activeTag: document.activeElement?.tagName, activeLabel: document.activeElement?.getAttribute('aria-label'),
      composerConnected: document.querySelector('[data-whip-composer]')?.isConnected ?? false })).catch(error => ({ error: String(error) }));
  }
  throw error;
} finally {
  clearTimeout(watchdog); await closing;
  try {
    if (isolation) await finishDesktopPerformance(isolation, host, fixture, succeeded);
    else await fixture?.close();
  } catch (error) { report.cleanupFailure = String(error); throw error; }
  finally { await writeFile(join(directory, 'controls.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 }); console.log(`Diagnostic results: ${directory}`); }
}
