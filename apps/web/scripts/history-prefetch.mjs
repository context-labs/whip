import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { chromium, firefox } from '@playwright/test';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// npm run build && npm run pack:web && node apps/web/scripts/history-prefetch.mjs
// Only an isolated daemon is used. The existing performance fixture seeds 10k
// real committed records, including grouped tools and an offloaded tool body.
const directory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-history-prefetch';
const browsers = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
const delays = (process.env.WHIP_WEB_HISTORY_DELAYS ?? '100,300,800').split(',').map(Number);
assert.ok(browsers.every(name => ['chromium', 'firefox'].includes(name)));
assert.ok(delays.every(delay => Number.isFinite(delay) && delay >= 0));
await mkdir(directory, { recursive: true });
const fixture = await startFixture({ env: { WHIP_WEB_PERF_FIXTURE: '1' }, lifetimeMs: 15 * 60_000 });
const results = [];
try {
  for (const name of browsers) {
    const browser = await { chromium, firefox }[name].launch();
    try {
      for (const delay of delays) {
        const page = await browser.newPage({ viewport: { width: 1100, height: 900 } });
        const report = { browser: name, version: browser.version(), delay, requests: [], checks: [] };
        results.push(report);
        const errors = [], timers = new Set(), pending = new Map();
        let phase = 'warmup', holdWarmup = true, releaseWarmup, smallPages = false;
        page.on('pageerror', error => errors.push(error.message));
        const conversation = page.getByRole('region', { name: 'Conversation', exact: true });
        const geometry = () => conversation.evaluate(element => ({
          top: element.scrollTop, height: element.clientHeight, total: element.scrollHeight,
          threshold: Math.min(2400, Math.max(800, element.clientHeight * 2)),
          mountedRows: element.querySelectorAll('[data-reading-id]').length,
        }));
        const anchor = () => conversation.evaluate(element => {
          const viewport = element.getBoundingClientRect();
          const row = [...element.querySelectorAll('[data-reading-id]')].find(row => {
            const rect = row.getBoundingClientRect();
            return rect.bottom > viewport.top && rect.top < viewport.bottom;
          });
          return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - viewport.top } : null;
        });
        const settle = async () => {
          let previous, since = performance.now();
          return eventually(async () => {
            const row = await anchor();
            if (!row || row.id !== previous?.id || Math.abs(row.offset - previous.offset) >= 0.5) since = performance.now();
            previous = row;
            return row && performance.now() - since >= 350 ? row : false;
          }, { description: `${name}/${delay}: stable reading anchor` });
        };
        const quiet = async () => {
          await eventually(() => pending.size === 0, { description: 'history replies delivered' });
          // Allow enough time for a forbidden continuation, not merely one frame.
          await page.waitForTimeout(delay + 650);
          assert.equal(pending.size, 0, 'History must become idle');
        };
        const assertAnchor = async (before, label) => {
          await settle();
          const after = await conversation.evaluate((element, id) => {
            const row = [...element.querySelectorAll('[data-reading-id]')].find(row => row.dataset.readingId === id);
            return row ? { id, offset: row.getBoundingClientRect().top - element.getBoundingClientRect().top } : null;
          }, before.id);
          assert.ok(after, `${label}: surviving anchor must remain mounted`);
          const drift = Math.abs(after.offset - before.offset);
          report.checks.push({ label, before, after, drift, geometry: await geometry() });
          assert.ok(drift <= 2, `${label}: anchor drift ${drift}px exceeds 2px`);
        };
        await page.routeWebSocket('**/api/v3/ws', route => {
          const server = route.connectToServer();
          route.onMessage(async data => {
            const request = JSON.parse(String(data));
            if (request.method !== 'history.page') { server.send(data); return; }
            const record = {
              id: request.id, phase, started: performance.now(), params: request.params,
              geometry: await geometry().catch(() => null), effectiveLimit: smallPages ? 1 : request.params.limit,
            };
            report.requests.push(record);
            pending.set(request.id, record);
            report.maxInFlight = Math.max(report.maxInFlight ?? 0, pending.size);
            // Real revision-pinned pages, not synthetic rows. Small pages model
            // raw pages that add little visible height and deterministically
            // exercise continuation without needing 384 enormous DOM rows.
            server.send(smallPages ? JSON.stringify({ ...request, params: { ...request.params, limit: 1 } }) : data);
          });
          server.onMessage(data => {
            const reply = JSON.parse(String(data));
            const record = pending.get(reply.id);
            if (!record) { route.send(data); return; }
            record.serverLatency = performance.now() - record.started;
            record.bytes = Buffer.byteLength(data);
            record.records = reply.result?.messages?.length;
            record.nextSeq = reply.result?.next_seq;
            record.error = reply.error;
            const deliver = () => {
              const timer = setTimeout(() => {
                timers.delete(timer);
                record.latency = performance.now() - record.started;
                record.delivered = performance.now();
                pending.delete(reply.id);
                route.send(data);
              }, delay);
              timers.add(timer);
            };
            if (holdWarmup && phase === 'warmup') releaseWarmup = deliver;
            else deliver();
          });
        });
        try {
          const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
          const start = performance.now();
          await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${fixture.info.root_id}`);
          await eventually(() => page.getByLabel('Message WHIP', { exact: true }).isEnabled());
          await eventually(() => conversation.locator('[data-reading-id]').count());
          report.initialInteractiveMs = performance.now() - start;
          await eventually(() => releaseWarmup, { description: 'background warm-up response held' });
          assert.equal(report.requests.length, 1, 'Fresh attachment warms exactly one page');
          assert.equal(report.requests[0].delivered, undefined, 'Initial snapshot is interactive before warm-up returns');
          holdWarmup = false;
          releaseWarmup();
          await quiet();
          await page.evaluate(() => document.fonts.ready);
          await settle();
          assert.equal(report.requests.length, 1, 'Idle initial warm-up must not chain');
          report.checks.push({ label: 'one-page interactive warmup', geometry: await geometry() });

          phase = 'no-intent';
          await conversation.evaluate(element => { element.scrollTop = 0; });
          await settle();
          for (const width of [650, 1100]) {
            await page.setViewportSize({ width, height: 900 });
            await settle();
          }
          // Layout changes, selection, and downward input cannot authorize reads.
          await conversation.evaluate(element => {
            element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: 1 }));
            const text = [...element.querySelectorAll('[data-reading-id]')].find(row => row.textContent)?.firstChild;
            if (text) { const range = document.createRange(); range.selectNodeContents(text); getSelection().removeAllRanges(); getSelection().addRange(range); }
          });
          await quiet();
          assert.equal(report.requests.length, 1, 'Programmatic scroll, resize, selection and downward input must not fetch');
          await page.evaluate(() => getSelection().removeAllRanges());
          await conversation.evaluate(element => { element.scrollTop = 0; });
          await settle();
          assert.equal((await geometry()).top, 0, 'Ignored input guards must be tested at the hard top');
          await conversation.evaluate(element => {
            element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1, ctrlKey: true }));
            const prevented = new WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: -1 });
            prevented.preventDefault();
            element.dispatchEvent(prevented);
            // A test-only descendant scroller exercises real computed overflow
            // and event bubbling without depending on which tool rows mount.
            const nested = document.createElement('div');
            nested.style.cssText = 'position:absolute;width:100px;height:30px;overflow:auto';
            nested.innerHTML = '<div style="height:200px">nested tool output</div>';
            element.append(nested);
            nested.scrollTop = 40;
            nested.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 }));
            nested.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'PageUp' }));
            nested.scrollTop = 0;
            nested.style.overscrollBehaviorY = 'contain';
            nested.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 }));
            nested.remove();
          });
          await quiet();
          assert.equal(report.requests.length, 1, 'Zoom, prevented input, and nested scrolling must not authorize history');
          report.checks.push({ label: 'ignored nested/zoom/prevented top-edge input', geometry: await geometry() });

          phase = 'early';
          const target = (await geometry()).threshold - 150;
          assert.ok(target > 256, 'Early fetch must be outside the old 256px boundary');
          await conversation.evaluate((element, top) => { element.scrollTop = top; }, target);
          await settle();
          assert.equal(report.requests.length, 1, 'Programmatic positioning inside zone must not fetch');
          const beforeEarly = await anchor();
          await conversation.hover();
          await page.mouse.wheel(0, -1);
          await eventually(() => report.requests.length === 2, { description: 'early upward history request' });
          assert.ok(report.requests[1].geometry.top > 256, 'Prefetch must start before the old boundary');
          await quiet();
          await assertAnchor(beforeEarly, 'early prepend');
          assert.equal(report.requests.length, 2, 'Full-height page must stop once outside the zone');

          phase = 'top-refill';
          smallPages = true;
          await conversation.evaluate(element => { element.scrollTop = 0; });
          await settle();
          await quiet();
          assert.equal(report.requests.length, 2, 'Restoration must not start another episode');
          const beforeTop = await anchor();
          const boundaryStarted = performance.now();
          await conversation.hover();
          await page.mouse.wheel(0, -1);
          await eventually(() => report.requests.length >= 5, { description: 'three small pages without another gesture' });
          await quiet();
          assert.equal(report.requests.length, 5, 'Episode is capped at three pages');
          assert.equal(report.requests[2].geometry.top, 0, 'Native upward input at the hard top must fetch without scrolling');
          report.boundaryReplyMs = report.requests[2].delivered - boundaryStarted;
          await assertAnchor(beforeTop, 'bounded top refill');
          const refill = report.requests.slice(2);
          assert.equal(new Set(refill.map(request => request.params.before_seq)).size, 3, 'Every continuation must advance its raw cursor');
          assert.ok((await geometry()).top < (await geometry()).threshold, 'Cap is tested while still inside the zone');

          phase = 'keyboard-cancel';
          await conversation.evaluate(element => {
            element.scrollTop = 0;
            element.querySelector('[data-reading-id]')?.focus({ preventScroll: true });
          });
          await settle();
          await page.keyboard.press('PageUp');
          await eventually(() => report.requests.length === 6, { description: 'keyboard starts a fresh bounded episode' });
          await conversation.evaluate(element => element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: 1 })));
          await quiet();
          assert.equal(report.requests.length, 6, 'Downward input cancels post-page continuation');

          phase = 'latest';
          await page.getByRole('button', { name: 'Latest', exact: true }).click();
          await settle();
          await quiet();
          assert.equal(report.requests.length, 6, 'Latest must not authorize older history');
          assert.equal(report.maxInFlight, 1, 'Only one history request may be in flight per agent');
          for (const request of report.requests) {
            assert.equal(request.params.limit, 128, 'Production request record budget stays unchanged');
            assert.equal(request.params.max_bytes, 256 * 1024, 'Production byte budget stays unchanged');
            assert.equal(request.error, undefined);
          }
          assert.deepEqual(errors, []);
          report.totalBytes = report.requests.reduce((sum, request) => sum + request.bytes, 0);
          report.passed = true;
          await page.screenshot({ path: `${directory}/${name}-${delay}.png` });
          console.log(`${name}/${delay}ms: warm-up=1, early=1, refill=3; anchors <=2px; no spurious fetches`);
        } catch (error) {
          report.error = String(error.stack ?? error);
          await page.screenshot({ path: `${directory}/${name}-${delay}-failure.png` }).catch(() => {});
          throw error;
        } finally {
          for (const timer of timers) clearTimeout(timer);
          await writeFile(`${directory}/results.json`, JSON.stringify(results, null, 2));
          await page.close();
        }
      }
    } finally { await browser.close(); }
  }
} finally { await fixture.close(); }
