import assert from 'node:assert/strict';
import { mkdir, mkdtemp, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { chromium } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { createSessionView } from '../../../packages/sdk/dist/state.js';
import {
  eventually,
  startFixture,
} from '../../../packages/sdk/scripts/fixture.mjs';
import { isolateDesktopPerformance, launchDesktopPerformance, exerciseDesktopTabs,
  exerciseDesktopTransfer, finishDesktopPerformance } from './performance-desktop.mjs';

// Opt-in, isolated production-store seed; never connects to a user's daemon.
process.env.WHIP_WEB_PERF_FIXTURE = '1';
const desktop = process.env.WHIP_WEB_PERFORMANCE_HOST === 'desktop';
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
fixture = await startFixture({ retainOnFailure: desktop });
console.log(`Fixture ready: ${fixture.directory}`);
client = createWhipClient({
  endpoint: fixture.info.endpoint,
  clientId: `web-performance-${crypto.randomUUID()}`,
  clientKind: 'human',
});
if (desktop) {
  host = await launchDesktopPerformance(fixture, isolation);
  ({ page, context } = host);
  await page.setViewportSize({ width: 1360, height: 960 });
} else {
  browser = await chromium.launch({ headless: true });
  context = await browser.newContext({ viewport: { width: 1360, height: 960 } });
  page = await context.newPage();
}
view = createSessionView(client.session(fixture.info.root_id));
Object.assign(metrics, {
  browser: desktop ? host.version : await browser.version(),
  host: desktop ? { kind: 'staged-electron-ipc', rendererDigest: host.rendererDigest,
    limitation: 'Stock Playwright Electron runs staged main/preload and production renderer with the isolated synthetic Go runner. Not signed-package, actual RLM worker, SSH/WAN or startup/idle acceptance. The controller SDK measurements use a separate fixture WebSocket connection.' } : { kind: 'browser-websocket' },
  fixture: {
    rootMessages: 10_000,
    retainedChildren: 100,
    messagesPerChild: 100,
    largeToolBodyBytes: 1_400_000,
  },
});
const origin = desktop ? host.origin : fixture.info.endpoint
  .replace(/^ws/, 'http')
  .replace('/api/v3/ws', '');
// A test-only frontend clock probe crosses Playwright's binding, not a product
// endpoint. The Go timestamp must lie between the browser's call/return times;
// intersecting these brackets does not assume symmetric request latency.
await page.exposeFunction('__performanceFixtureClock', async () => {
  const response = await fetch(
    fixture.info.frontend + '/control/performance/clock',
    {
      signal: AbortSignal.timeout(5000),
    },
  );
  assert.equal(response.status, 200);
  return response.json();
});
const calibrateClock = () =>
  page.evaluate(async () => {
    const samples = [];
    for (let index = 0; index < 20; index++) {
      const start = performance.now();
      const { monotonic_ms: daemon } = await window.__performanceFixtureClock();
      samples.push({ start, end: performance.now(), daemon });
    }
    return samples;
  });
await page.addInitScript(({ desktop, rootId }) => {
  window.__performanceEventLatency = [];
  window.__performanceCommitDOM = [];
  window.__performanceProbeOverflow = false;
  window.__performanceIPCFrames = 0;
  window.__performanceContentHandles = [];
  const pending = [];
  const receive = data => {
        try {
          const message = JSON.parse(data);
          const handle = message.result?.content ?? (message.result?.reference_id ? message.result : undefined);
          if (handle?.digest && !window.__performanceContentHandles.some(item => item.reference_id === handle.reference_id)) {
            if (window.__performanceContentHandles.length >= 16) window.__performanceProbeOverflow = true;
            else window.__performanceContentHandles.push({ reference_id: handle.reference_id, digest: handle.digest, size: handle.size });
          }
          const event = message.params?.event;
          if (
            event?.kind !== 'stream.text' || event.root_id !== rootId ||
            !(
              event.payload?.text?.includes('delta-') ||
              event.payload?.text?.includes('commit-probe-')
            )
          )
            return;
          if (pending.length >= 512) {
            window.__performanceProbeOverflow = true;
            return;
          }
          pending.push({
            sequence: event.seq,
            text: event.payload.text,
            start: performance.now(),
          });
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
    const live = [...document.querySelectorAll('[data-message-id^=\"live:\"]')]
      .map((element) => element.textContent)
      .join('\n');
    for (let index = pending.length - 1; index >= 0; index--) {
      if (!live.includes(pending[index].text)) continue;
      const stamp = performance.now();
      if (pending[index].text.includes('commit-probe-')) {
        if (window.__performanceCommitDOM.length >= 128)
          window.__performanceProbeOverflow = true;
        else
          window.__performanceCommitDOM.push({
            sequence: pending[index].sequence,
            marker: pending[index].text,
            browser_ms: stamp,
            received_ms: pending[index].start,
          });
      } else if (window.__performanceEventLatency.length < 512) {
        window.__performanceEventLatency.push(stamp - pending[index].start);
      } else window.__performanceProbeOverflow = true;
      pending.splice(index, 1);
    }
  }).observe(document, { subtree: true, childList: true, characterData: true });
}, { desktop, rootId: fixture.info.root_id });
const rootRoute = `/h/${fixture.info.runtime_id}/s/${fixture.info.root_id}`;
const requests = [];
page.on('pageerror', (error) => errors.push(error.message));
page.on('websocket', (socket) =>
  socket.on('framesent', ({ payload }) => {
    try {
      requests.push(JSON.parse(String(payload)));
    } catch {}
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
  await client.connect();
  console.log('Checking retained history and child views');
  await view.start();
  const root = view.getSnapshot();
  assert.equal(root.status, 'live');
  assert.equal(root.history[fixture.info.root_id].throughSeq, 9_999);
  assert.ok(root.history[fixture.info.root_id].messages.length <= 128);
  await view.loadCollection('agents');
  assert.equal(view.getSnapshot().collections.agents.items.length, 101);
  for (let index = 0; index < 6; index++) await view.loadOlder();
  assert.equal(
    view.getSnapshot().history[fixture.info.root_id].messages.length,
    512,
  );
  const opened = [];
  let maxRetained = view.getSnapshot().retainedBytes;
  let maxMessages = 0;
  for (let index = 0; index < 100; index++) {
    const id = `perf-child-${String(index).padStart(3, '0')}`;
    const start = performance.now();
    await view.openAgent(id);
    opened.push(performance.now() - start);
    const state = view.getSnapshot();
    assert.equal(state.history[id].messages.length, 100);
    maxRetained = Math.max(maxRetained, state.retainedBytes);
    maxMessages = Math.max(
      maxMessages,
      Object.values(state.history).reduce(
        (count, history) => count + history.messages.length,
        0,
      ),
    );
    assert.ok(state.retainedBytes <= 8 << 20);
    view.closeAgent(id);
    assert.equal(Object.keys(view.getSnapshot().history).length, 1);
  }
  metrics.sdk = {
    childOpenMilliseconds: summarize(opened),
    maximumRetainedPayloadBytes: maxRetained,
    maximumRetainedMessages: maxMessages,
    finalRetainedMessages:
      view.getSnapshot().history[fixture.info.root_id].messages.length,
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
  metrics.initialRenderedRows = await page.locator('[data-message-id]').count();
  assert.ok(metrics.initialRenderedRows < 80);
  // Repeated real paging must retain the visible row at exactly the same offset.
  const anchors = [];
  for (let index = 0; index < 4; index++) {
    await viewport.evaluate((element) => {
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
      const row = [...element.querySelectorAll('[data-message-id]')].find(
        (item) =>
          item.getBoundingClientRect().bottom >
          element.getBoundingClientRect().top,
      );
      return {
        id: row.dataset.messageId,
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
      const row = [...element.querySelectorAll('[data-message-id]')].find(
        (item) => item.dataset.messageId === id,
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
    .locator('[data-message-id]')
    .count();
  assert.ok(metrics.maximumVisibleRowsAfterPaging < 80);
  // A virtualized row containing native text selection must remain mounted.
  const selection = await viewport.evaluate((element) => {
    const row = [...element.querySelectorAll('[data-message-id]')].find(
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
    return { id: row.dataset.messageId, text: getSelection().toString() };
  });
  await viewport.evaluate((element) => {
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
      const row = [...element.querySelectorAll('[data-message-id]')].find(
        (item) =>
          item.getBoundingClientRect().bottom >
          element.getBoundingClientRect().top,
      );
      return {
        id: row.dataset.messageId,
        offset:
          row.getBoundingClientRect().top - element.getBoundingClientRect().top,
      };
    });
  const assertAnchor = async (anchor) => {
    await eventually(() =>
      viewport.evaluate((element, saved) => {
        const row = [...element.querySelectorAll('[data-message-id]')].find(
          (item) => item.dataset.messageId === saved.id,
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
  await page.locator(`[data-workspace-tab="${fixture.info.root_id}"]`).getByRole('button', { name: /^Tab actions for / }).click();
  await page.getByRole('menuitem', { name: 'Session details', exact: true }).click();
  await page.getByRole('link', { name: 'perf-child-000', exact: true }).click();
  await ready();
  await frame();
  await viewport.evaluate((element) => {
    element.scrollTop = 300;
  });
  await frame();
  const childAnchor = await stableAnchor();
  metrics.cachedAnchors = { root: rootAnchor, child: childAnchor, observations: [] };
  console.log('Checking cached root/child switches');
  const switches = [];
  for (let index = 0; index < 20; index++) {
    const start = performance.now();
    await page
      .getByRole('link', { name: 'Root conversation', exact: true })
      .click();
    await page.waitForFunction(() =>
      document
        .querySelector('[aria-label="Conversation"]')
        ?.textContent.includes('Root message'),
    );
    try { await assertAnchor(rootAnchor); }
    finally { metrics.cachedAnchors.observations.push({ index, recipient: 'root', observed: await captureAnchor() }); }
    switches.push(performance.now() - start);
    assert.equal(await viewport.getByText(/perf-child-000 message/).count(), 0);
    if (index < 19) {
      await page.goBack();
      await page
        .getByRole('link', { name: 'Root conversation', exact: true })
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
    ({ runtimeId, rootId }) => {
      const prefix = 'whip.web.draft.v1:';
      const active = `${runtimeId}:${rootId}:${rootId}`;
      const drafts = Array.from({ length: 31 }, (_, index) => [
        `${runtimeId}:${rootId}:perf-child-${String(index).padStart(3, '0')}`,
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
    { runtimeId: fixture.info.runtime_id, rootId: fixture.info.root_id },
  );
  assert.equal(draft.count, 32);
  assert.ok(draft.activeBytes <= 256 << 10);
  await page.reload();
  await ready();
  const textarea = page.getByLabel('Message WHIP', { exact: true });
  assert.equal((await textarea.inputValue()).length, draft.activeBytes);
  const clockBefore = await calibrateClock();
  console.log('Measuring typing and committed events under 16 streams');
  const concurrent = [];
  for (let index = 0; index < 15; index++) {
    const created = desktopRoots ? { status: 'succeeded', result: { root_id: desktopRoots[index + 1] } } : await client.sessions
      .create({ cwd: fixture.directory, model: 'model', provider: 'provider' })
      .result();
    assert.equal(created.status, 'succeeded');
    const command = client
      .session(created.result.root_id)
      .submit({ text: 'hold:performance-stream' });
    await command.accepted();
    concurrent.push({ root: created.result.root_id, command });
  }
  const live = client
    .session(fixture.info.root_id)
    .submit({ text: 'hold:performance-stream' });
  await live.accepted();
  await page.locator('[data-message-id^="live:"]').waitFor();
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
  const commitProbes = [];
  for (let index = 0; index < 40; index++) {
    const response = await fetch(
      fixture.info.frontend + '/control/performance/commit',
      {
        method: 'POST',
        signal: AbortSignal.timeout(5000),
      },
    );
    assert.equal(response.status, 200);
    commitProbes.push(await response.json());
    // Real keydown/keyup events are needed for native keyboard EventTiming;
    // insertText dispatches input directly and cannot measure this boundary.
    await page.keyboard.type('x');
    await frame();
  }
  await page.waitForFunction(() => window.__performanceCommitDOM.length === 40);
  const clockAfter = await calibrateClock();
  // Let completed keyboard interactions render and the performance timeline
  // observer drain before treating omitted entries as threshold-censored.
  await frame();
  await page.waitForTimeout(250);
  const input = await page.evaluate(() => ({
    samples: window.__performanceInputs,
    streamingMutations: window.__performanceStreams,
    eventLatency: window.__performanceEventLatency,
    commitDOM: window.__performanceCommitDOM,
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
  const clockSamples = [...clockBefore, ...clockAfter];
  // Allow 0.2ms for browser timer quantization. Agreement before and after the
  // loaded interval checks the negligible-relative-drift assumption on this Mac.
  const lowerOffset = Math.max(
    ...clockSamples.map((sample) => sample.start - sample.daemon - 0.2),
  );
  const upperOffset = Math.min(
    ...clockSamples.map((sample) => sample.end - sample.daemon + 0.2),
  );
  assert.ok(
    lowerOffset <= upperOffset,
    'Go/browser monotonic clock brackets disagree across the measurement interval',
  );
  const errorBound = (upperOffset - lowerOffset) / 2;
  assert.ok(
    errorBound <= 5,
    `Clock correlation is too imprecise (${errorBound}ms)`,
  );
  const offset = (lowerOffset + upperOffset) / 2;
  const observations = new Map(
    input.commitDOM.map((sample) => [sample.sequence, sample]),
  );
  assert.equal(observations.size, 40);
  const commitToDOM = commitProbes.map((probe) => {
    const observed = observations.get(probe.sequence);
    assert.ok(
      observed,
      `No DOM mutation for committed event ${probe.sequence}`,
    );
    assert.equal(observed.marker, probe.marker);
    const elapsed = observed.browser_ms - (probe.committed_ms + offset);
    assert.ok(
      elapsed >= -errorBound,
      'DOM observation precedes the measured commit boundary',
    );
    return elapsed;
  });
  metrics.commitReturnToDOMMilliseconds = {
    ...summarize(commitToDOM),
    clockErrorBoundMilliseconds: errorBound,
    boundary:
      'Timestamp immediately after successful Store.AppendRootEvent COMMIT return to MutationObserver observation of its marker in a mounted live transcript row; excludes physical paint.',
  };
  metrics.commitClockCalibration = {
    method:
      'Go time.Since and browser performance.now; intersect 20 request-response brackets before and 20 after the loaded interval, without assuming symmetric latency.',
    sampleCount: clockSamples.length,
    measuredIntervalMilliseconds: clockAfter.at(-1).end - clockBefore[0].start,
    roundTripMilliseconds: summarize(
      clockSamples.map((sample) => sample.end - sample.start),
    ),
    browserMinusDaemonOffsetMilliseconds: {
      lower: lowerOffset,
      upper: upperOffset,
    },
    timerQuantizationAllowanceMilliseconds: 0.2,
    assumption:
      'Both monotonic clocks have negligible relative drift during this short same-machine run; disjoint before/after bounds fail the measurement.',
  };
  metrics.checks.push(
    '40 post-COMMIT-return stream probes reached the real SDK/app DOM under 16 concurrent agents, with bounded monotonic-clock calibration error',
  );
  if (desktop) { console.log('Measuring chunked desktop upload and native download'); await exerciseDesktopTransfer({ host, fixture, metrics, directory, summarize }); }
  for (const { root: id } of concurrent) {
    const snapshot = await client.session(id).snapshot();
    const turn = snapshot.active_turns[id];
    if (turn) await client.session(id).cancelTurn(turn).result();
  }
  const snapshot = await client.session(fixture.info.root_id).snapshot();
  if (snapshot.active_turns[fixture.info.root_id])
    await client
      .session(fixture.info.root_id)
      .cancelTurn(snapshot.active_turns[fixture.info.root_id])
      .result();
  const cdp = await context.newCDPSession(page);
  await cdp.send('HeapProfiler.collectGarbage');
  metrics.browserRetained = {
    ...(await cdp.send('Runtime.getHeapUsage')),
    ...(await cdp.send('Memory.getDOMCounters')),
    currentTranscriptRows: await page.locator('[data-message-id]').count(),
  };
  if (desktop) {
    const traffic = await host.traffic();
    assert.equal(traffic.overflow, false); assert(traffic.maximumSubscriptions <= 4);
    requests.push(...traffic.requests);
    metrics.desktopAfterWork = await host.processMemory('after-streams-and-transfer');
  }
  metrics.requestCounts = Object.fromEntries(
    [...new Set(requests.map((request) => request.method))].map((method) => [
      method,
      requests.filter((request) => request.method === method).length,
    ]),
  );
  metrics.checks.push(
    '32 near-limit drafts remain editable with 16 concurrent fake root agents and visible stream updates',
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
  client?.close();
  if (desktop && isolation) await finishDesktopPerformance(isolation, host, fixture, succeeded, cleanupErrors);
  else {
    await browser?.close().catch(error => cleanupErrors.push(error));
    await fixture?.close().catch(error => cleanupErrors.push(error));
    if (cleanupErrors.length) throw new AggregateError(cleanupErrors, 'Performance fixture cleanup failed');
  }
}
