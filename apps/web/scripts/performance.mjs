import assert from 'node:assert/strict';
import { mkdir, mkdtemp, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { chromium } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { createSessionView } from '../../../packages/sdk/dist/state.js';
import { checkComposerReading } from './composer-reading.mjs';
import { deadline, eventually } from './native-fixture.mjs';
import { startHistoryFixture } from './native-history-fixture.mjs';

// Owned canonical store seed followed by the real production runtime/provider.
const desktop = process.env.WHIP_WEB_PERFORMANCE_HOST === 'desktop';
const { isolateDesktopPerformance, launchDesktopPerformance, exerciseDesktopTabs, exerciseDesktopTransfer, finishDesktopPerformance } = desktop ? await import('./performance-desktop.mjs') : {};
const requestedDirectory = resolve(process.env.WHIP_WEB_BROWSER_RESULTS ??
  (desktop ? '/tmp/whip-desktop-performance-results' : '/tmp/whip-web-browser-results'));
await mkdir(requestedDirectory, { recursive: true });
const directory = desktop ? await mkdtemp(join(requestedDirectory, 'run-')) : requestedDirectory;
console.log(`Performance results: ${directory}`);
const summarize = (samples) => {
  const values = samples.slice().sort((a, b) => a - b);
  return {
    count: values.length,
    median: values[Math.floor(values.length / 2)],
    p95: values[Math.ceil(values.length * 0.95) - 1],
    max: values.at(-1),
  };
};
let isolation, host, fixture, client, browser, context, page, view;
let succeeded = false;
const errors = [];
const metrics = { recordedAt: new Date().toISOString(), platform: process.platform, checks: [] };
try {
if (desktop) isolation = await isolateDesktopPerformance();
fixture = await startHistoryFixture({ retainOnFailure: desktop, managedDirectory: desktop, workers: 16, performanceStreams: true });
console.log(`Fixture ready: ${fixture.directory}`);
client = await fixture.connect(`web-performance-${randomUUID()}`);
if (desktop) {
  host = await launchDesktopPerformance(fixture, isolation);
  ({ page, context } = host);
  await page.setViewportSize({ width: 1360, height: 960 });
} else {
  browser = await chromium.launch({ headless: true });
  context = await browser.newContext({ viewport: { width: 1360, height: 960 } });
  page = await context.newPage();
}
context.setDefaultTimeout(15_000);
view = createSessionView(client.session(fixture.history.root_id));
Object.assign(metrics, {
  browser: desktop ? host.version : await browser.version(),
  host: desktop ? { kind: 'staged-electron-ipc', rendererDigest: host.rendererDigest,
    limitation: 'Stock Playwright Electron runs staged main/preload and production renderer with the isolated native Go runtime, actual workers and a local fake provider. Not signed-package, SSH/WAN or startup/idle acceptance. The controller SDK measurements use a separate native gateway connection.' } : { kind: 'browser-websocket' },
  fixture: {
    rootMessages: 10_000,
    retainedChildren: 100,
    messagesPerChild: 100,
    explicitContentBodyBytes: 1_400_000,
    contentProvenance: 'Explicit owner-scoped content part; not auto-externalized tool output. Actual executor byte limits are covered separately.',
    retainedTreeOperations: 128, markdownStreams: true,
    streamBoundary: 'Each of 16 actual provider requests emits 2,000 markdown deltas at 30ms intervals, then remains explicitly held until cancellation. Later probes can measure held active requests after their finite delta workload completes.',
    modelContext: 'Synthetic canonical compaction through message 9,996 plus raw tail; raw 10,000-message transcript remains intact. Summary is seeded store evidence, not measured past inference.',
  },
});
const origin = desktop ? host.origin : fixture.info.web;
// The same browser monotonic clock bounds native admission between the request
// call and verified receipt return. It does not pretend to timestamp SQL COMMIT.
await page.exposeFunction('__performanceSubmit', async ({ requestID, text }) => {
  const admission = await client.session(fixture.history.root_id).submit([{ type: 'text', text }], requestID, deadline());
  assert.equal(admission.input.state, 'queued');
  assert.deepEqual(admission.receipt.identity, { client_id: client.clientID, request_id: requestID });
  return { inputID: admission.input.id };
});
await page.addInitScript(({ desktop, rootId }) => {
  window.__performanceEventLatency = [];
  window.__performanceAcceptedDOM = [];
  window.__performanceExpected = [];
  window.__performanceProbeOverflow = false;
  window.__performanceIPCFrames = 0;
  window.__performanceContentHandles = [];
  const pending = [], seen = new Map();
  const receive = data => {
    try {
      const message = JSON.parse(data);
      const handle = message.result?.content ?? (message.result?.digest ? message.result : undefined);
      if (handle?.digest && !window.__performanceContentHandles.some(item => item.id === handle.id)) {
        if (window.__performanceContentHandles.length >= 16) window.__performanceProbeOverflow = true;
        else window.__performanceContentHandles.push({ id: handle.id, digest: handle.digest, size: handle.size });
      }
      const observation = message.result, preview = observation?.preview;
      if (observation?.snapshot?.session_id !== rootId || !preview?.text?.includes('delta-')) return;
      const marker = [...preview.text.matchAll(/delta-\d{4}/g)].at(-1)?.[0];
      if (!marker || seen.get(preview.message_id) === marker) return;
      if (pending.length >= 512 || seen.size >= 16) { window.__performanceProbeOverflow = true; return; }
      seen.set(preview.message_id, marker);
      pending.push({ messageID: `message:${preview.message_id}:part:0`, marker, start: performance.now() });
    } catch {}
  };
  if (desktop) {
    if (!window.whipDesktop) throw new Error('Desktop performance probe requires the real preload bridge');
    const stop = window.whipDesktop.onEvent(event => {
      if (event.kind === 'frame') { window.__performanceIPCFrames++; receive(event.frame); }
    });
    window.addEventListener('pagehide', stop, { once: true });
  } else {
    const NativeWebSocket = window.WebSocket;
    window.WebSocket = class extends NativeWebSocket {
      constructor(...args) { super(...args); this.addEventListener('message', message => receive(message.data)); }
    };
  }
  new MutationObserver(() => {
    const rows = [...document.querySelectorAll('[data-message-id]')];
    for (let index = pending.length - 1; index >= 0; index--) {
      const item = pending[index];
      if (!rows.some(row => row.dataset.messageId === item.messageID && row.textContent.includes(item.marker))) continue;
      if (window.__performanceEventLatency.length >= 512) window.__performanceProbeOverflow = true;
      else window.__performanceEventLatency.push(performance.now() - item.start);
      pending.splice(index, 1);
    }
    const queue = [...document.querySelectorAll('[data-queue-row]')];
    for (const item of window.__performanceExpected) {
      if (item.seen) continue;
      const row = queue.find(row => row.dataset.queueRow === item.rowID);
      if (!row || !row.textContent.includes(item.marker)) continue;
      item.seen = true;
      if (window.__performanceAcceptedDOM.length >= 40) window.__performanceProbeOverflow = true;
      else window.__performanceAcceptedDOM.push({ requestID: item.requestID, browser_ms: performance.now() });
    }
  }).observe(document, { subtree: true, childList: true, characterData: true });
}, { desktop, rootId: fixture.history.root_id });
const rootRoute = `/h/${fixture.info.runtime_id}/s/${fixture.history.root_id}`;
const requestCounts = Object.create(null);
let requestTotal = 0, requestOverflow = false;
page.on('pageerror', (error) => errors.push({ message: error.message, stack: error.stack }));
page.on('websocket', (socket) =>
  socket.on('framesent', ({ payload }) => {
    try {
      const { method } = JSON.parse(String(payload));
      if (typeof method !== 'string' || method.length > 128 || requestTotal >= 1_000_000 ||
          !Object.hasOwn(requestCounts, method) && Object.keys(requestCounts).length >= 256) throw new Error('Performance request counter limit exceeded');
      requestCounts[method] = (requestCounts[method] ?? 0) + 1;
      requestTotal++;
    } catch { requestOverflow = true; }
  }),
);
const viewport = page.getByRole('region', {
  name: 'Conversation',
  exact: true,
});
const ready = async () => {
  await eventually(() => page.locator('[data-whip-composer]').isEnabled());
  await page.locator('[data-whip-composer]').waitFor();
};
const frame = () =>
  page.evaluate(
    () =>
      new Promise((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(resolve)),
      ),
  );
  console.log('Checking retained history and child views');
  await view.start();
  const root = view.getSnapshot();
  assert.equal(root.status, 'live');
  assert.equal(root.history.snapshot.through_sequence, '10000');
  assert.ok(root.history.messages.length <= 100);
  for (let index = 0; index < 6; index++) await view.loadOlder();
  assert.equal(view.getSnapshot().history.messages.length, 512);
  const opened = [];
  let maxRetained = view.getSnapshot().retainedBytes;
  let maxMessages = 0;
  // Each native SessionView has one owner. Release every inspected child rather
  // than retaining a second transcript cache or a tree-wide legacy reducer.
  for (const child of fixture.history.children) {
    const childView = createSessionView(client.session(child.id));
    try {
      const start = performance.now(); await childView.start(); opened.push(performance.now() - start);
      const state = childView.getSnapshot();
      assert.equal(state.history.messages.length, 100);
      maxRetained = Math.max(maxRetained, view.getSnapshot().retainedBytes + state.retainedBytes);
      maxMessages = Math.max(maxMessages, view.getSnapshot().history.messages.length + state.history.messages.length);
      assert.ok(state.retainedBytes + view.getSnapshot().retainedBytes <= 8 << 20);
    } finally { await childView.dispose(); }
    assert.equal(childView.getSnapshot().history.messages.length, 0);
  }
  metrics.sdk = {
    childOpenMilliseconds: summarize(opened),
    maximumRetainedPayloadBytes: maxRetained,
    maximumRetainedMessages: maxMessages,
    finalRetainedMessages:
      view.getSnapshot().history.messages.length,
  };
  metrics.checks.push(
    '10,000 stored root messages page into at most 512 retained messages; 100 child transcripts inspected and released',
  );

  await page.goto(origin + rootRoute);
  await ready();
  if (desktop) {
    assert.equal(await page.evaluate(() => location.origin), 'whip-app://bundle');
    assert.equal(await page.evaluate(() => JSON.parse(localStorage.getItem('whip.hosts.v2') || '[]').find(host => host.id === 'local')?.runtimeId), fixture.info.runtime_id);
    assert((await host.traffic()).requests.some(request => request.method === 'initialize'), 'Renderer did not attach through desktop IPC');
    assert(await page.evaluate(() => window.__performanceIPCFrames > 0), 'Desktop receipt probe saw no real frames');
  }
  await frame();
  metrics.initialRenderedRows = await page.locator('[data-reading-id]').count();
  assert.ok(metrics.initialRenderedRows < 80);
  // The native bounded child summary arrives independently of history. Wait
  // for its real 37px dock before testing typing-only layout stability.
  await page.getByRole('region', { name: 'Session agents', exact: true }).waitFor();
  await page.locator('[data-activity-group]').filter({ hasText: 'Read 128 files' }).waitFor();
  await page.getByText('Some execution details are outside this bounded window. Open REPL to inspect older work.', { exact: true }).waitFor();
  await page.evaluate(() => document.fonts.ready);
  metrics.composerReading = await checkComposerReading(page);
  // Repeated real paging must retain the visible row at exactly the same offset.
  const anchors = [];
  for (let index = 0; index < 4; index++) {
    await viewport.evaluate((element) => {
      element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 }));
      element.scrollTop = (element.scrollHeight - element.clientHeight) / 2;
    });
    // Stay away from automatic near-top loading and let measured rows settle.
    let previousOffset;
    let stableSince = performance.now();
    await eventually(async () => {
      const offset = await viewport.evaluate(element => element.scrollTop);
      if (offset !== previousOffset) stableSince = performance.now();
      previousOffset = offset;
      return performance.now() - stableSince >= 300;
    });
    const before = await viewport.evaluate((element) => {
      const row = [...element.querySelectorAll('[data-reading-id]')].find(
        (item) =>
          item.getBoundingClientRect().bottom >
          element.getBoundingClientRect().top,
      );
      return {
        id: row.dataset.readingId,
        offset:
          row.getBoundingClientRect().top - element.getBoundingClientRect().top,
      };
    });
    const start = performance.now();
    await page
      .getByRole('button', { name: 'Load earlier messages', exact: true })
      // Playwright's auto-scroll would request one page before this click.
      .evaluate(button => button.click());
    await eventually(
      async () =>
        !(await page
          .getByRole('button', { name: 'Load earlier messages', exact: true })
          .isDisabled()),
    );
    await frame();
    const after = await viewport.evaluate((element, id) => {
      const row = [...element.querySelectorAll('[data-reading-id]')].find(
        (item) => item.dataset.readingId === id,
      );
      return row
        ? row.getBoundingClientRect().top - element.getBoundingClientRect().top
        : null;
    }, before.id);
    (metrics.anchorOffsets ??= []).push({ before: before.offset, after });
    assert.notEqual(
      after,
      null,
      'Prepended history must keep the anchor row mounted',
    );
    assert.ok(
      Math.abs(after - before.offset) <= 2,
      `Scroll anchor moved ${after - before.offset}px`,
    );
    anchors.push(performance.now() - start);
  }
  metrics.prependMilliseconds = summarize(anchors);
  metrics.maximumVisibleRowsAfterPaging = await page
    .locator('[data-reading-id]')
    .count();
  assert.ok(metrics.maximumVisibleRowsAfterPaging < 80);
  // A virtualized row containing native text selection must remain mounted.
  const selection = await viewport.evaluate((element) => {
    const row = [...element.querySelectorAll('[data-reading-id]')].find(
      (item) =>
        item.getBoundingClientRect().bottom >
        element.getBoundingClientRect().top,
    );
    const paragraph = row.querySelector('p');
    const text = paragraph.firstChild;
    const range = document.createRange();
    range.setStart(text, 0);
    range.setEnd(text, Math.min(12, text.textContent.length));
    getSelection().removeAllRanges();
    getSelection().addRange(range);
    window.__performanceSelectedRow = row;
    return { id: row.dataset.readingId, text: getSelection().toString() };
  });
  await viewport.evaluate((element) => {
    element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: 1 }));
    element.scrollTop += 8000;
  });
  await frame();
  assert.equal(
    await page.evaluate(() => getSelection().toString()),
    selection.text,
  );
  assert.equal(
    await page.evaluate(() => window.__performanceSelectedRow.isConnected),
    true,
  );
  await page.evaluate(() => getSelection().removeAllRanges());
  metrics.checks.push(
    'Four real history prepends preserve scroll anchors; virtualized selection survives an 8,000px scroll',
  );

  // Each recipient restores its own retained reading anchor after unmounting.
  const captureAnchor = () =>
    viewport.evaluate((element) => {
      const row = [...element.querySelectorAll('[data-reading-id]')].find(
        (item) =>
          item.getBoundingClientRect().bottom >
          element.getBoundingClientRect().top,
      );
      return {
        id: row.dataset.readingId,
        offset:
          row.getBoundingClientRect().top - element.getBoundingClientRect().top,
      };
    });
  const assertAnchor = async (anchor) => {
    await eventually(() =>
      viewport.evaluate((element, saved) => {
        const row = [...element.querySelectorAll('[data-reading-id]')].find(
          (item) => item.dataset.readingId === saved.id,
        );
        return (
          !!row &&
          Math.abs(
            row.getBoundingClientRect().top -
              element.getBoundingClientRect().top -
              saved.offset,
          ) <= 2
        );
      }, anchor),
    );
  };
  await frame();
  const stableAnchor = async () => {
    let previous, stableSince = performance.now();
    return eventually(async () => {
      const current = await captureAnchor();
      if (!previous || previous.id !== current.id || Math.abs(previous.offset - current.offset) >= 1) stableSince = performance.now();
      previous = current;
      return performance.now() - stableSince >= 300 ? current : false;
    }, { description: 'settled pre-switch reading anchor' });
  };
  const rootAnchor = await stableAnchor();
  await page.locator(`[data-workspace-tab="${fixture.history.root_id}"]`).getByRole('button', { name: /^Tab actions for / }).click();
  await page.getByRole('menuitem', { name: 'Session details', exact: true }).click();
  const details = page.getByRole('dialog', { name: 'Session details', exact: true });
  const childLink = details.getByRole('link', { name: 'perf-child-000', exact: true });
  await details.locator('article code').first().waitFor();
  for (let index = 0; index < 7 && !await childLink.count(); index++) {
    const prior = await details.locator('article code').first().textContent();
    assert.ok(await details.locator('article').count() <= 16);
    await details.getByRole('button', { name: 'Next page', exact: true }).click();
    await eventually(async () => await details.locator('article code').first().textContent() !== prior);
  }
  await childLink.click();
  await ready();
  await frame();
  await viewport.evaluate((element) => {
    element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 }));
    element.scrollTop = 300;
  });
  await frame();
  const childAnchor = await stableAnchor();
  metrics.cachedAnchors = { root: rootAnchor, child: childAnchor, observations: [] };
  console.log('Checking cached root/child switches');
  const switches = [];
  for (let index = 0; index < 20; index++) {
    if (index % 5 === 0) console.log(`Cached switch ${index + 1}/20`);
    const start = performance.now();
    await page.locator('[data-session-info-bar]').getByRole('button', { name: 'Session actions', exact: true }).click();
    const menuOpened = performance.now();
    await page.getByRole('menuitem', { name: 'Root conversation', exact: true }).click();
    const navigated = performance.now();
    await page.waitForFunction(() =>
      document
        .querySelector('[aria-label="Conversation"]')
        ?.textContent.includes('Root message'),
    );
    const contentVisible = performance.now();
    try { await assertAnchor(rootAnchor); }
    finally { metrics.cachedAnchors.observations.push({ index, recipient: 'root', observed: await captureAnchor() }); }
    const anchorRestored = performance.now();
    (metrics.cachedSwitchStages ??= []).push({ menu: menuOpened - start, navigation: navigated - menuOpened, content: contentVisible - navigated, anchor: anchorRestored - contentVisible });
    switches.push(anchorRestored - start);
    assert.equal(await viewport.getByText(/perf-child-000 message/).count(), 0);
    if (index < 19) {
      await page.goBack();
      await page
        .getByRole('button', { name: 'Agent: perf-child-000', exact: true })
        .waitFor();
      await page.waitForFunction(() =>
        document
          .querySelector('[aria-label="Conversation"]')
          ?.textContent.includes('perf-child-000 message'),
      );
      try { await assertAnchor(childAnchor); }
      finally { metrics.cachedAnchors.observations.push({ index, recipient: 'child', observed: await captureAnchor() }); }
      assert.equal(await viewport.getByText(/Root message/).count(), 0);
    }
  }
  metrics.cachedRootSwitchMilliseconds = summarize(switches);
  metrics.checks.push(
    '20 cached child-to-root switches restore independent reading anchors and never mix transcript content',
  );

  console.log(desktop ? 'Checking 32 desktop tabs and retained memory' : 'Checking 32 drafts and 16 streams');
  const desktopRoots = desktop ? await exerciseDesktopTabs({ host, fixture, client, ready, frame, summarize, metrics }) : undefined;

  // Near the aggregate draft ceiling, with a near-per-draft-ceiling active value.
  const draft = await page.evaluate(
    ({ runtimeId, rootId, children }) => {
      const prefix = 'whip.web.draft.v1:';
      const active = `${runtimeId}:${rootId}:${rootId}`;
      const drafts = Array.from({ length: 31 }, (_, index) => [
        `${runtimeId}:${rootId}:${children[index].id}`,
        'd'.repeat(25_400),
      ]);
      drafts.push([active, '']);
      const spare =
        (1 << 20) -
        new TextEncoder().encode(JSON.stringify(drafts)).byteLength -
        2048;
      drafts[31][1] = 'a'.repeat(spare);
      for (const [key, text] of drafts)
        localStorage.setItem(prefix + key, text);
      return {
        count: drafts.length,
        bytes: new TextEncoder().encode(JSON.stringify(drafts)).byteLength,
        activeBytes: spare,
      };
    },
    { runtimeId: fixture.info.runtime_id, rootId: fixture.history.root_id, children: fixture.history.children },
  );
  assert.equal(draft.count, 32);
  assert.ok(draft.activeBytes <= 256 << 10);
  await page.reload();
  await ready();
  const textarea = page.getByLabel('Message WHIP', { exact: true });
  assert.equal((await textarea.inputValue()).length, draft.activeBytes);
  if (await page.getByRole('button', { name: 'Latest', exact: true }).count()) await page.getByRole('button', { name: 'Latest', exact: true }).click();
  await frame();
  console.log('Measuring typing and accepted inputs under 16 actual provider streams');
  const concurrent = [];
  for (let index = 0; index < 15; index++) {
    const id = desktopRoots ? desktopRoots[index + 1] : (await fixture.createRoot(client)).root.id;
    const command = client.session(id).submission([{ type: 'text', text: `hold:performance-stream-${index}` }], randomUUID());
    await command.send(deadline()); concurrent.push({ root: id, command });
  }
  const live = client.session(fixture.history.root_id).submission([{ type: 'text', text: 'hold:performance-stream-visible' }], randomUUID());
  await live.send(deadline());
  await eventually(async () => (await fixture.effects()).filter(text => text.startsWith('hold:performance-stream-')).length === 16, { description: 'sixteen active actual providers' });
  await page.waitForFunction(() => window.__performanceEventLatency.length > 0);
  await textarea.focus();
  await textarea.press('End');
  await frame();
  await page.evaluate(() => {
    window.__performanceInputs = [];
    window.__performanceStreams = 0;
    if (document.visibilityState !== 'visible')
      throw new Error('Keyboard paint timing requires a visible browser page');
    if (
      !PerformanceObserver.supportedEntryTypes.includes('event') ||
      !performance.eventCounts
    )
      throw new Error('Chromium native EventTiming and eventCounts are required');
    const keyboard = {
      keydowns: [],
      entries: [],
      overflow: false,
      droppedEntries: null,
      initialEventCount: performance.eventCounts.get('keydown') ?? 0,
    };
    const recordKeydown = (event) => {
      if (!event.target.hasAttribute('data-whip-composer') || event.key !== 'x')
        return;
      if (keyboard.keydowns.length >= 128) keyboard.overflow = true;
      else
        keyboard.keydowns.push({
          startTime: event.timeStamp,
          trusted: event.isTrusted,
        });
    };
    document.addEventListener('keydown', recordKeydown, true);
    const collectEntries = (entries) => {
      for (const entry of entries) {
        if (entry.name !== 'keydown') continue;
        if (keyboard.entries.length >= 128) keyboard.overflow = true;
        else
          keyboard.entries.push({
            startTime: entry.startTime,
            duration: entry.duration,
            interactionId: entry.interactionId,
          });
      }
    };
    const eventObserver = new PerformanceObserver((list, _observer, options) => {
      if (options?.droppedEntriesCount !== undefined)
        keyboard.droppedEntries =
          (keyboard.droppedEntries ?? 0) + options.droppedEntriesCount;
      collectEntries(list.getEntries());
    });
    // Install before typing: buffered historical event entries use a higher
    // threshold and would lose the faster keyboard samples in this population.
    eventObserver.observe({ type: 'event', durationThreshold: 16 });
    window.__finishPerformanceKeyboard = () => {
      if (document.visibilityState !== 'visible')
        throw new Error('Browser page became hidden during keyboard timing');
      collectEntries(eventObserver.takeRecords());
      eventObserver.disconnect();
      document.removeEventListener('keydown', recordKeydown, true);
      return {
        ...keyboard,
        browserEventCount:
          (performance.eventCounts.get('keydown') ?? 0) -
          keyboard.initialEventCount,
      };
    };
    document.addEventListener(
      'input',
      (event) => {
        if (!event.target.hasAttribute('data-whip-composer')) return;
        const start = performance.now();
        requestAnimationFrame(() =>
          window.__performanceInputs.push(performance.now() - start),
        );
      },
      true,
    );
    new MutationObserver((records) => {
      window.__performanceStreams += records.filter(
        (record) => record.type === 'characterData',
      ).length;
    }).observe(document.querySelector('[aria-label="Conversation"]'), {
      subtree: true,
      characterData: true,
    });
  });
  const acceptedProbes = [];
  for (let index = 0; index < 40; index++) {
    if (index % 5 === 0) console.log(`Accepted input probe ${index + 1}/40`);
    const requestID = randomUUID(), marker = `accepted-probe-${index}`;
    const probe = await page.evaluate(async ({ requestID, marker, rowID }) => {
      if (window.__performanceExpected.length >= 40) throw new Error('Admission probe limit exceeded');
      window.__performanceExpected.push({ requestID, marker, rowID, seen: false });
      const started = performance.now();
      const receipt = await window.__performanceSubmit({ requestID, text: marker });
      return { requestID, started, acknowledged: performance.now(), inputID: receipt.inputID };
    }, { requestID, marker, rowID: `input:${JSON.stringify([fixture.history.root_id, client.clientID, requestID])}` });
    acceptedProbes.push(probe);
    await page.waitForFunction(id => window.__performanceAcceptedDOM.some(item => item.requestID === id), requestID);
    // Trusted key events retain native EventTiming; insertText cannot do this.
    await page.keyboard.type('x'); await frame();
    assert.equal((await client.session(fixture.history.root_id).inputs.cancel(probe.inputID, deadline())).state, 'cancelled');
  }
  // Let completed keyboard interactions render and the performance timeline
  // observer drain before treating omitted entries as threshold-censored.
  await frame();
  await page.waitForTimeout(250);
  const input = await page.evaluate(() => ({
    samples: window.__performanceInputs,
    streamingMutations: window.__performanceStreams,
    eventLatency: window.__performanceEventLatency,
    acceptedDOM: window.__performanceAcceptedDOM,
    probeOverflow: window.__performanceProbeOverflow,
    keyboard: window.__finishPerformanceKeyboard(),
  }));
  assert.equal(input.samples.length, 40);
  assert.ok(input.streamingMutations > 0);
  metrics.drafts = {
    ...draft,
    concurrentAgents: 16,
    inputToAnimationFrameMilliseconds: {
      ...summarize(input.samples),
      boundary:
        'Input handler to next requestAnimationFrame callback; responsiveness proxy, excludes rendering and paint.',
    },
    streamingMutations: input.streamingMutations,
  };
  const keyboard = input.keyboard;
  assert.equal(keyboard.overflow, false);
  assert.ok(
    keyboard.droppedEntries === null || keyboard.droppedEntries === 0,
    'Native EventTiming reports dropped entries',
  );
  assert.equal(keyboard.keydowns.length, 40);
  assert.equal(keyboard.browserEventCount, 40);
  assert.ok(keyboard.keydowns.every((event) => event.trusted));
  const matchedEntries = new Set();
  const keyDurations = keyboard.keydowns.map((event) => {
    const matches = keyboard.entries.filter(
      (entry) => Math.abs(entry.startTime - event.startTime) <= 0.2,
    );
    assert.ok(matches.length <= 1, 'Ambiguous native keyboard timing identity');
    if (!matches.length) return null;
    const entry = matches[0];
    assert.ok(!matchedEntries.has(entry), 'Native entry matched multiple keys');
    matchedEntries.add(entry);
    assert.ok(entry.duration >= 16);
    assert.equal(entry.duration % 8, 0, 'Unexpected native duration quantization');
    assert.ok(entry.interactionId > 0);
    return entry.duration;
  });
  // Unknown slow entries within our measured input interval must fail, rather
  // than being silently dropped and making the resulting percentile optimistic.
  assert.ok(
    keyboard.entries.every(
      (entry) =>
        matchedEntries.has(entry) ||
        entry.startTime < keyboard.keydowns[0].startTime - 0.2 ||
        entry.startTime > keyboard.keydowns.at(-1).startTime + 0.2,
    ),
    'Unmatched native keyboard timing inside the measured input interval',
  );
  const observedDurations = keyDurations.filter((duration) => duration !== null);
  const censoredCount = keyDurations.length - observedDurations.length;
  const rounded = observedDurations.slice().sort((a, b) => a - b);
  const p95Rank = Math.ceil(keyDurations.length * 0.95);
  const lower = summarize(
    keyDurations.map((value) => value === null ? 0 : Math.max(0, value - 4)),
  );
  const upper = summarize(
    keyDurations.map((value) => value === null ? 20 : value + 4),
  );
  metrics.drafts.keyboardToNextPaintMilliseconds = {
    count: keyDurations.length,
    trustedKeydowns: keyboard.keydowns.length,
    browserEventCount: keyboard.browserEventCount,
    reportedCount: observedDurations.length,
    censoredCount,
    p95Rounded:
      p95Rank > censoredCount ? rounded[p95Rank - censoredCount - 1] : null,
    medianBounds: { lower: lower.median, upper: upper.median },
    p95Bounds: { lower: lower.p95, upper: upper.p95 },
    maxBounds: { lower: lower.max, upper: upper.max },
    observed: summarize(observedDurations),
    reportingThresholdMilliseconds: 16,
    durationQuantizationMilliseconds: 8,
    censoredUpperBoundMilliseconds: 20,
    droppedEntries: keyboard.droppedEntries,
    boundary:
      'Native PerformanceEventTiming duration from trusted keyboard keydown timestamp through the next browser rendering completion, rounded to 8ms; a browser next-paint estimate, not physical display timing.',
    censoring:
      'All 40 keydowns are retained. Entries below the 16ms reporting threshold are absent; each absent value is conservatively bounded to 0–20ms including the 4ms rounding allowance. Percentile bounds include both reported and censored events. A rounded p95 is supplied only when its rank is represented by reported entries.',
    deliveryCheck:
      'Observer installed before typing at the minimum threshold; trusted capture count and browser eventCounts both equal 40. After each keyup two animation frames elapse; final frames, 250ms observer drainage, takeRecords and dropped-entry/identity checks guard against silently losing slow entries.',
    reference: 'https://www.w3.org/TR/event-timing/',
  };
  metrics.eventReceivedToDOMMilliseconds = summarize(input.eventLatency);
  assert.ok(input.eventLatency.length > 10);
  assert.equal(input.probeOverflow, false);
  const observations = new Map(input.acceptedDOM.map(sample => [sample.requestID, sample]));
  assert.equal(observations.size, 40);
  const bounds = acceptedProbes.map(probe => {
    const observed = observations.get(probe.requestID); assert.ok(observed);
    assert.ok(observed.browser_ms >= probe.started, 'DOM observation preceded the request');
    return { requestID: probe.requestID, requestToDOM: observed.browser_ms - probe.started,
      commitLower: Math.max(0, observed.browser_ms - probe.acknowledged), commitUpper: observed.browser_ms - probe.started };
  });
  metrics.acceptedInputToDOMMilliseconds = {
    requestToDOM: summarize(bounds.map(item => item.requestToDOM)),
    commitBracketLower: summarize(bounds.map(item => item.commitLower)),
    commitBracketUpper: summarize(bounds.map(item => item.commitUpper)), samples: bounds,
    boundary: 'Browser monotonic request call through real gateway/SDK admission to MutationObserver of the exact client/request/session-owned queue row. Each accepted input is explicitly cancelled before the next probe; none contacts the provider.',
    commitBounds: 'SQL acceptance occurs after request start and before the verified admission receipt returns. DOM observation minus those endpoints bounds commit-to-DOM; no exact SQL timestamp, clock calibration, or physical paint claim.',
  };
  metrics.checks.push('40 real native admissions reached exact identity-correlated queue DOM under 16 concurrent actual provider streams; accepted queued inputs cancelled explicitly without inference');
  if (desktop) { console.log('Measuring bounded desktop uploads and native download'); await exerciseDesktopTransfer({ host, fixture, client, metrics, directory, summarize }); }
  for (const id of [...concurrent.map(item => item.root), fixture.history.root_id]) {
    const session = client.session(id), activity = await session.activity(deadline());
    assert.ok(activity.active_turn, 'Performance stream ended before the measurement completed');
    await session.cancelTurn(activity.active_turn.id, deadline());
  }
  for (const { command } of [...concurrent, { command: live }]) assert.equal((await command.wait(deadline())).turn.state, 'cancelled');
  assert.ok(!(await fixture.effects()).some(text => text.startsWith('accepted-probe-')), 'Cancelled admission probe contacted the provider');
  const cdp = await context.newCDPSession(page);
  await cdp.send('HeapProfiler.collectGarbage');
  metrics.browserRetained = {
    ...(await cdp.send('Runtime.getHeapUsage')),
    ...(await cdp.send('Memory.getDOMCounters')),
    currentTranscriptRows: await page.locator('[data-reading-id]').count(),
  };
  if (desktop) {
    const traffic = await host.traffic();
    assert.equal(traffic.overflow, false); assert(traffic.maximumObservations <= 16);
    metrics.requestCounts = traffic.requestCounts;
    metrics.desktopTrafficProbe = { retainedRecords: traffic.requests.length, prunedRecords: traffic.prunedRequests, totalFrames: traffic.requestTotal, boundary: 'Full bounded per-method counters; at most 8192 recent metadata records. Timed probes require complete retained intervals.' };
    metrics.desktopAfterWork = await host.processMemory('after-streams-and-transfer');
  }
  assert.equal(requestOverflow, false, 'Performance request counters exceeded bounds');
  metrics.requestCounts ??= requestCounts;
  if (!desktop) metrics.browserTrafficProbe = { totalFrames: requestTotal,
    boundary: 'Exact per-method counts for at most 256 method names and one million frames; no request bodies retained.' };
  metrics.checks.push(
    '32 near-limit drafts remain editable with 16 concurrent native roots using a local fake provider and visible stream updates',
  );
  assert.deepEqual(errors, []);
  console.log(JSON.stringify(metrics, null, 2));
  await writeFile(
    join(directory, 'performance.json'),
    JSON.stringify(metrics, null, 2),
  );
  succeeded = true;
} catch (error) {
  await page?.screenshot({
      path: join(directory, 'performance-failure.png'),
      fullPage: true,
    })
    .catch(() => {});
  await writeFile(
    join(directory, 'performance-failure.json'),
    JSON.stringify(
      {
        error: String(error), stack: error.stack, fixtureDirectory: fixture?.directory, isolatedDirectory: isolation?.directory,
        metrics,
        errors,
        nativeTraffic: host ? await host.traffic().catch(() => null) : undefined,
        acceptedDOM: await page?.evaluate(() => window.__performanceAcceptedDOM).catch(() => null),
        html: await page?.locator('body')
          .innerText()
          .catch(() => ''),
      },
      null,
      2,
    ),
  );
  throw error;
} finally {
  const cleanupErrors = [];
  await view?.dispose().catch(error => cleanupErrors.push(error));
  if (desktop && isolation) await finishDesktopPerformance(isolation, host, fixture, succeeded, cleanupErrors);
  else {
    await browser?.close().catch(error => cleanupErrors.push(error));
    await fixture?.close().catch(error => cleanupErrors.push(error));
    if (cleanupErrors.length) throw new AggregateError(cleanupErrors, 'Performance fixture cleanup failed');
  }
}
