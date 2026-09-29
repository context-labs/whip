import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { chromium, firefox } from '@playwright/test';
import { eventually } from './native-fixture.mjs';
import { startHistoryFixture } from './native-history-fixture.mjs';

// npm run pack:web && node apps/web/scripts/history-prefetch.mjs
// Synthetic canonical history in an owned directory, followed by the unchanged
// production native runtime/SDK/renderer. Only read-only replies are delayed.
const directory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-history-prefetch';
const browsers = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
const delays = (process.env.WHIP_WEB_HISTORY_DELAYS ?? '100,300,800').split(',').map(Number);
assert.ok(browsers.every(name => ['chromium', 'firefox'].includes(name)));
assert.ok(delays.length > 0 && delays.length <= 8 && delays.every(delay => Number.isFinite(delay) && delay >= 0 && delay <= 2000));
await mkdir(directory, { recursive: true });
const fixture = await startHistoryFixture();
const results = [];
try {
  for (const name of browsers) {
    const browser = await { chromium, firefox }[name].launch();
    try {
      for (const delay of delays) {
        const page = await browser.newPage({ viewport: { width: 1100, height: 900 } });
        const report = { browser: name, version: browser.version(), delay, requests: [], checks: [] };
        results.push(report);
        const errors = [], timers = new Set(), pending = new Map(), connections = new Set(), closing = new Set();
        let closed = false, nextConnection = 0;
        const fail = error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); };
        const closeEndpoint = endpoint => {
          const pendingClose = endpoint.close().catch(fail).finally(() => closing.delete(pendingClose));
          closing.add(pendingClose);
        };
        page.setDefaultTimeout(15_000);
        let phase = 'warmup', holdWarmup = true, releaseWarmup, smallPages = false;
        page.on('pageerror', fail);
        page.on('console', event => { if (event.type() === 'error' && /content.security.policy|violates.*directive/i.test(event.text())) fail(event.text()); });
        const conversation = page.getByRole('region', { name: 'Conversation', exact: true });
        const geometry = () => page.evaluate(() => {
          // The native composer can be ready before its first history window.
          // Do not wait for that window before forwarding the request that owns it.
          const element = document.querySelector('[role="region"][aria-label="Conversation"]');
          return element ? {
            top: element.scrollTop, height: element.clientHeight, total: element.scrollHeight,
            threshold: Math.min(2400, Math.max(800, element.clientHeight * 2)),
            mountedRows: element.querySelectorAll('[data-reading-id]').length,
          } : null;
        });
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
        await page.routeWebSocket('**/api/v4/ws', route => {
          if (closed) { closeEndpoint(route); return; }
          if (connections.size >= 256) { fail(new Error('History proxy connection bound exceeded')); closeEndpoint(route); return; }
          const id = ++nextConnection, server = route.connectToServer();
          let ended = false;
          const ownedTimers = new Set(), ownedRequests = new Set();
          const retire = () => {
            if (ended) return;
            ended = true; connections.delete(retire);
            for (const timer of ownedTimers) { clearTimeout(timer); timers.delete(timer); }
            for (const key of ownedRequests) pending.delete(key);
            closeEndpoint(route); closeEndpoint(server);
          };
          connections.add(retire);
          route.onClose(retire); server.onClose(retire);
          const guarded = work => { void work().catch(error => { if (!ended && !closed) fail(error); retire(); }); };
          route.onMessage(data => guarded(async () => {
            if (ended || closed) return;
            const request = JSON.parse(String(data));
            if (request.method !== 'sessions.history_page') { server.send(data); return; }
            assert.equal(request.params.session_id, fixture.history.root_id);
            assert(report.requests.length < 64, 'History evidence bound exceeded');
            const key = `${id}:${request.id}`;
            const record = {
              id: request.id, phase, started: performance.now(), params: request.params,
              geometry: await geometry().catch(() => null), effectiveLimit: smallPages ? 1 : request.params.limit,
            };
            if (ended || closed) return;
            report.requests.push(record); pending.set(key, record); ownedRequests.add(key);
            report.maxInFlight = Math.max(report.maxInFlight ?? 0, pending.size);
            // Actual revision-pinned pages. Count-one requests exercise bounded
            // continuation when a raw page contributes little visible height.
            server.send(smallPages ? JSON.stringify({ ...request, params: { ...request.params, limit: 1 } }) : data);
          }));
          server.onMessage(data => guarded(async () => {
            if (ended || closed) return;
            const reply = JSON.parse(String(data)), key = `${id}:${reply.id}`, record = pending.get(key);
            if (!record) { route.send(data); return; }
            record.serverLatency = performance.now() - record.started;
            record.bytes = Buffer.byteLength(data);
            record.records = reply.result?.messages?.length;
            record.nextCursor = reply.result?.next_cursor;
            record.revision = reply.result?.snapshot?.revision;
            record.error = reply.error;
            const deliver = () => {
              if (ended || closed) return;
              const timer = setTimeout(() => {
                timers.delete(timer); ownedTimers.delete(timer);
                if (ended || closed) return;
                record.latency = performance.now() - record.started;
                record.delivered = performance.now();
                pending.delete(key); ownedRequests.delete(key);
                try { route.send(data); } catch (error) { fail(error); retire(); }
              }, delay);
              timers.add(timer); ownedTimers.add(timer);
            };
            if (holdWarmup && phase === 'warmup') releaseWarmup = deliver;
            else deliver();
          }));
        });
        try {
          const origin = fixture.info.web;
          const start = performance.now();
          await page.goto(`${origin}/h/${fixture.info.runtime_id}/s/${fixture.history.root_id}`);
          await eventually(() => page.getByLabel('Message WHIP', { exact: true }).isEnabled());
          report.initialInteractiveMs = performance.now() - start;
          await eventually(() => releaseWarmup, { description: 'background warm-up response held' });
          assert.equal(report.requests.length, 1, 'Fresh attachment warms exactly one page');
          assert.equal(report.requests[0].delivered, undefined, 'Composer is interactive before the first canonical history page returns');
          holdWarmup = false;
          releaseWarmup();
          await quiet();
          await eventually(() => conversation.locator('[data-reading-id]').count());
          await page.evaluate(() => document.fonts.ready);
          await settle();
          assert.equal(report.requests.length, 1, 'Idle initial warm-up must not chain');
          report.checks.push({ label: 'interactive composer while the one-page canonical history seed is held', geometry: await geometry() });

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
            const nestedBody = document.createElement('div');
            nestedBody.style.height = '200px'; nestedBody.textContent = 'nested tool output';
            nested.append(nestedBody);
            element.append(nested);
            nested.scrollTop = 40;
            if (nested.clientHeight !== 30 || nested.scrollHeight !== 200 || nested.scrollTop !== 40) throw new Error('Fixture nested scrolling geometry was not applied');
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
          assert.equal(new Set(refill.map(request => request.params.cursor)).size, 3, 'Every continuation must advance its raw cursor');
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
            assert.equal(request.params.limit, 100, 'Native page record budget stays unchanged');
            assert(request.bytes < 8 << 20, 'Native reply stays within the wire byte bound');
            assert(request.records <= request.effectiveLimit);
            assert.equal(typeof request.revision, 'string');
            assert.equal(request.error, undefined);
          }
          assert.deepEqual(errors, []);
          report.totalBytes = report.requests.reduce((sum, request) => sum + request.bytes, 0);
          report.passed = true;
          await page.screenshot({ path: `${directory}/${name}-${delay}.png` });
          console.log(`${name}/${delay}ms: warm-up=1, early=1, refill=3; anchors <=2px; no spurious fetches`);
        } catch (error) {
          report.error = String(error.stack ?? error); report.errors = errors;
          await page.screenshot({ path: `${directory}/${name}-${delay}-failure.png` }).catch(() => {});
          throw error;
        } finally {
          closed = true;
          for (const retire of [...connections]) retire();
          for (const timer of timers) clearTimeout(timer);
          pending.clear();
          try {
            await Promise.all(closing);
            await writeFile(`${directory}/results.json`, JSON.stringify(results, null, 2));
          } finally { await page.close(); }
        }
      }
    } finally { await browser.close(); }
  }
} finally { await fixture.close(); }
