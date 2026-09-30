import assert from 'node:assert/strict';
import { createHash, randomUUID } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, expect, firefox } from '@playwright/test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';
import { openDesktopRemote } from './native-desktop-remote.mjs';
import { assertDesktopViewport } from './performance-desktop.mjs';

// npm run pack:web && node apps/web/scripts/mermaid-diagrams.mjs
// Real packaged renderer + isolated daemon, never the person's running daemon.
// Every transcript is canonical native history; no retired presentation/event shapes.
const directory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-mermaid-diagrams';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 3 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox', 'electron'].includes(name)));
await mkdir(directory, { recursive: true });
const manifest = JSON.parse(await readFile(new URL('../renderer-manifest.json', import.meta.url), 'utf8'));
const fixture = await startFixture({ allowedOrigins: names.includes('electron') ? ['whip-app://bundle'] : [], lifetimeMs: 15 * 60_000 });
const origin = fixture.info.web;
const reports = [];
const source = 'flowchart LR\n  Root[Root agent] --> Worker[Diagram worker]\n  Worker --> Reader[Conversation]';
const fence = code => '```mermaid\n' + code + '\n```';
const decoded = image => expect.poll(() => image.evaluate(img => img.complete && img.naturalWidth > 0)).toBe(true);

async function checkElectron() {
  // Actual staged renderer/main/preload and a distinct disposable native URL
  // host. Reuse the joined launcher; never discover or start an installed host.
  const report = { browser: 'electron', rendererDigest: manifest.digest, checks: [], errors: [] };
  reports.push(report);
  let host, page;
  try {
    const staged = JSON.parse(await readFile(new URL('../../desktop/.stage/app/renderer-manifest.json', import.meta.url), 'utf8'));
    assert.equal(staged.digest, manifest.digest, 'Desktop stage must match current packaged renderer');
    host = await openDesktopRemote(fixture); page = host.page;
    report.version = host.version;
    report.viewport = await assertDesktopViewport(host);
    page.on('pageerror', error => { if (report.errors.length < 64) report.errors.push(error.message.slice(0, 4096)); });
    await page.exposeFunction('mermaidElectronCSP', value => { if (report.errors.length < 64) report.errors.push(String(value).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.mermaidElectronCSP(event.violatedDirective).catch(() => {}); }));
    const client = await fixture.connect(`mermaid-electron-${randomUUID()}`);
    const { root } = await fixture.createRoot(client);
    const session = client.session(root.id);
    await page.goto(`whip-app://bundle/h/${fixture.info.runtime_id}/s/${root.id}`);
    await expect(page.getByRole('textbox', { name: 'Message WHIP', exact: true })).toBeEnabled();
    const prompt = `hold:electron-mermaid\n\n${fence(source)}`;
    const turn = session.submission([{ type: 'text', text: prompt }], randomUUID()); await turn.send(deadline());
    const figure = page.locator('[data-message-role="assistant"] [data-mermaid-view]');
    await expect(figure.getByRole('status')).toHaveText('Diagram will render when this response finishes.');
    await expect(figure.locator('img')).toHaveCount(0);
    await fixture.release(prompt.slice(5));
    assert.equal((await turn.wait(deadline())).turn.state, 'succeeded');
    await decoded(figure.locator('img'));
    await figure.getByRole('button', { name: 'Source', exact: true }).click();
    await expect(figure.locator('pre')).toHaveText(source);
    await figure.getByRole('button', { name: 'Diagram', exact: true }).click();
    await figure.getByRole('button', { name: 'Expand', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Mermaid · Flowchart', exact: true });
    await decoded(dialog.locator('img')); await page.keyboard.press('Escape');
    await page.reload(); await decoded(figure.locator('img'));
    assert.deepEqual(report.errors, []);
    await page.screenshot({ path: join(directory, 'electron-history.png') });
    report.checks.push('same staged renderer; local custom-protocol worker/font/image decode; live settlement; source/expand; persisted history; production CSP');
    report.passed = true;
  } catch (error) {
    report.passed = false; report.failure = error.stack ?? String(error);
    await page?.screenshot({ path: join(directory, 'electron-failure.png') }).catch(() => {});
    console.error(report.failure);
  } finally {
    await host?.close(report.passed === true);
    await writeFile(join(directory, 'results.json'), JSON.stringify(reports, null, 2) + '\n');
  }
  console.log(`electron: ${report.passed ? 'passed' : 'failed'}`);
}

try {
  // This also catches accidentally running against stale Go embed assets.
  for (const [path, file] of Object.entries(manifest.files)) {
    const response = await fetch(`${origin}/${path}`);
    assert.equal(response.status, 200, path);
    assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256, path);
  }
  for (const name of names) {
    if (name === 'electron') { await checkElectron(); continue; }
    let browser, context, client;
    const report = { browser: name, rendererDigest: manifest.digest, checks: [], errors: [], csp: [] };
    reports.push(report);
    let page, releasePending = () => {};
    const setup = async () => {
      const next = await context.newPage();
      next.setDefaultTimeout(15_000);
      next.on('pageerror', error => { if (report.errors.length < 64) report.errors.push(error.message.slice(0, 4096)); });
      await next.exposeFunction('mermaidCSP', value => { if (report.csp.length < 64) report.csp.push(String(value).slice(0, 256)); });
      await next.addInitScript(() => {
        window.__mermaidCopies = [];
        // Observe the existing browser clipboard boundary, including exact bytes,
        // without changing the person's OS clipboard or needing Firefox permissions.
        Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async text => { if (text.length > 65536 || window.__mermaidCopies.length >= 16) throw new Error('Clipboard probe bound exceeded'); window.__mermaidCopies.push(text); } } });
        document.addEventListener('securitypolicyviolation', event => { void window.mermaidCSP(event.violatedDirective).catch(() => {}); });
      });
      return next;
    };
    const create = async () => {
      const { root } = await fixture.createRoot(client);
      return { session: client.session(root.id), url: `${origin}/h/${fixture.info.runtime_id}/s/${root.id}` };
    };
    const submit = async (session, text) => {
      const command = session.submission([{ type: 'text', text }], randomUUID());
      await command.send(deadline()); assert.equal((await command.wait(deadline())).turn.state, 'succeeded');
    };
    const ready = async () => { await expect(page.getByRole('textbox', { name: 'Message WHIP', exact: true })).toBeEnabled(); await page.evaluate(() => document.fonts.ready); };
    const reading = () => page.getByRole('region', { name: 'Conversation', exact: true });
    const anchor = () => reading().evaluate(element => {
      const viewport = element.getBoundingClientRect();
      const row = [...element.querySelectorAll('[data-reading-id]')].find(row => { const rect = row.getBoundingClientRect(); return rect.bottom > viewport.top && rect.top < viewport.bottom; });
      return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - viewport.top } : null;
    });
    const stable = async () => {
      let previous, since = performance.now();
      return eventually(async () => {
        const value = await anchor();
        if (!value || value.id !== previous?.id || Math.abs(value.offset - previous.offset) >= 1) since = performance.now();
        previous = value;
        return value && performance.now() - since >= 300 ? value : false;
      }, { description: 'stable reading anchor' });
    };
    const assertAnchor = (before, after, label) => {
      assert.equal(after.id, before.id, `${label}: changed anchor`);
      assert.ok(Math.abs(after.offset - before.offset) < 5, `${label}: ${JSON.stringify({ before, after })}`);
      report.checks.push({ label, before, after });
    };
    try {
      client = await fixture.connect(`mermaid-${randomUUID()}`);
      browser = await { chromium, firefox }[name].launch();
      context = await browser.newContext({ viewport: { width: 1200, height: 900 }, reducedMotion: 'reduce' });
      report.version = browser.version();
      page = await setup();
      const ordinary = await create();
      await submit(ordinary.session, 'Ordinary code stays code.\n\n```javascript\nconst untouched = 42;\n```');
      const assets = [];
      page.on('request', request => { if (assets.length < 256) assets.push(request.url().slice(0, 2048)); else if (report.errors.length < 64) report.errors.push('Asset evidence overflow'); });
      await page.goto(ordinary.url); await ready();
      await expect(page.locator('[data-message-role="assistant"] pre')).toContainText('const untouched = 42;');
      await expect(page.locator('[data-mermaid-view]')).toHaveCount(0);
      assert.equal(assets.filter(url => /mermaid-worker/.test(url)).length, 0, 'Ordinary chat loaded the diagram worker');
      report.checks.push('ordinary fences unchanged; no worker load');

      const live = await create();
      await page.goto(live.url); await ready();
      const prompt = `hold:${name[0]}

${fence(source)}

Between duplicate diagrams.

${fence(source)}`;
      const turn = live.session.submission([{ type: 'text', text: prompt }], randomUUID()); await turn.send(deadline());
      const user = page.locator('[data-message-role="user"]');
      const assistant = page.locator('[data-message-role="assistant"]');
      const userFigures = user.locator('[data-mermaid-view]');
      const liveFigures = assistant.locator('[data-mermaid-view]');
      await expect(liveFigures).toHaveCount(2);
      await expect(liveFigures.first().getByRole('status')).toHaveText('Diagram will render when this response finishes.');
      await expect(liveFigures.locator('img')).toHaveCount(0);
      await decoded(userFigures.first().locator('img'));
      await expect(userFigures).toHaveCount(2);
      await page.screenshot({ path: join(directory, `${name}-live-source.png`) });
      await fixture.release(prompt.slice(5));
      assert.equal((await turn.wait(deadline())).turn.state, 'succeeded');
      await expect(liveFigures.locator('img')).toHaveCount(2);
      await decoded(liveFigures.first().locator('img'));
      await expect(liveFigures.getByRole('region', { name: 'Mermaid diagram', exact: true }).locator('svg, object, embed, iframe')).toHaveCount(0);
      report.checks.push('static user Markdown renders; both live assistant fences wait for turn settlement');
      const first = liveFigures.first();
      await first.getByRole('button', { name: 'Source', exact: true }).click();
      await expect(first.locator('pre')).toHaveText(source);
      await expect(liveFigures.last()).toHaveAttribute('data-mermaid-view', 'diagram');
      await first.getByRole('button', { name: 'Copy source', exact: true }).click();
      assert.equal(await page.evaluate(() => window.__mermaidCopies.at(-1)), source);
      const responseCopy = page.getByRole('button', { name: 'Copy response', exact: true });
      await page.locator('[data-response-end]').filter({ has: responseCopy }).hover();
      await responseCopy.click();
      assert.equal(await page.evaluate(() => window.__mermaidCopies.at(-1)), prompt);
      await first.getByRole('button', { name: 'Diagram', exact: true }).click();
      const expand = first.getByRole('button', { name: 'Expand', exact: true });
      await expand.focus(); await page.keyboard.press('Enter');
      const dialog = page.getByRole('dialog', { name: 'Mermaid · Flowchart', exact: true });
      await expect(dialog).toBeVisible(); await decoded(dialog.locator('img'));
      await dialog.getByRole('button', { name: '100%', exact: true }).click();
      await expect(dialog.getByRole('button', { name: '100%', exact: true })).toHaveAttribute('aria-pressed', 'true');
      await dialog.getByRole('button', { name: 'Fit', exact: true }).click();
      await page.keyboard.press('Escape'); await expect(dialog).toBeHidden(); await expect(expand).toBeFocused();
      report.checks.push('independent duplicate views, exact source/response copy boundary, keyboard expand/Fit/100%/focus return');
      await page.reload(); await ready();
      await expect(page.locator('[data-message-role="assistant"] [data-mermaid-view] img')).toHaveCount(2);
      await page.screenshot({ path: join(directory, `${name}-history-diagrams.png`) });
      report.checks.push('settled history reload renders duplicate diagrams');
      assert.deepEqual(report.csp, []);
      await page.close();

      // Hold only the cold worker asset. The next paragraph is the reading anchor
      // while the preceding mounted history diagram changes source -> image.
      page = await setup();
      let releaseWorker, retiredWorker = false;
      const workerGate = new Promise(resolve => { releaseWorker = resolve; releasePending = () => { retiredWorker = true; resolve(); }; });
      await page.route('**/mermaid-worker-*.js', async route => {
        await workerGate;
        try { if (retiredWorker) await route.abort(); else await route.continue(); }
        catch (error) { if (!retiredWorker && report.errors.length < 64) report.errors.push(String(error).slice(0, 4096)); }
      });
      const scroll = await create();
      const paragraphs = Array.from({ length: 18 }, (_, i) => `Reading paragraph ${i}. ${'Keep this place while a diagram is laid out. '.repeat(12)}`);
      await submit(scroll.session, `${fence(source)}

${fence(source)}

${paragraphs.join('\n\n')}`);
      await page.goto(scroll.url); await ready();
      const prose = page.locator('[data-message-role="assistant"] [data-markdown-block]');
      await reading().hover({ position: { x: 8, y: 8 } });
      await page.mouse.wheel(0, -1);
      // Virtualized rows mount as we move toward the start of the assistant turn.
      await reading().evaluate(element => { element.scrollTop = element.scrollHeight; });
      await eventually(async () => {
        const target = prose.filter({ hasText: 'Reading paragraph 0.' });
        if (await target.count()) {
          const id = await target.evaluate(element => {
            const scroller = element.closest('[role="region"][aria-label="Conversation"]');
            scroller.scrollTop += element.getBoundingClientRect().top - scroller.getBoundingClientRect().top + 8;
            return element.closest('[data-reading-id]').dataset.readingId;
          });
          // Wait for virtual measurements, then reposition until this paragraph
          // really is anchored rather than a still-resizing static user row.
          return (await stable()).id === id;
        }
        await reading().evaluate(element => { element.scrollTop -= 500; }); return false;
      }, { description: 'paragraph after mounted history diagrams' });
      await expect(page.locator('[data-message-role="assistant"] [data-mermaid-view]')).toHaveCount(2);
      const beforeRender = await stable();
      assert.ok(beforeRender.id.endsWith(':block:2'), 'Anchor must be the paragraph after the two diagrams');
      releaseWorker();
      await expect(page.locator('[data-message-role="assistant"] [data-mermaid-view] img')).toHaveCount(2);
      assertAnchor(beforeRender, await stable(), 'delayed image render above reading anchor');
      const diagramRows = page.locator('[data-message-role="assistant"] [data-mermaid-view]');
      const oldImage = await diagramRows.last().locator('img').getAttribute('src');
      const beforeTheme = await stable();
      await page.emulateMedia({ colorScheme: 'dark' });
      await expect(diagramRows.last().locator('img')).not.toHaveAttribute('src', oldImage);
      await decoded(diagramRows.last().locator('img'));
      assertAnchor(beforeTheme, await stable(), 'system theme image replacement preserves reading anchor');
      await page.emulateMedia({ colorScheme: 'light' });
      await decoded(diagramRows.last().locator('img')); await stable();
      await diagramRows.first().getByRole('button', { name: 'Source', exact: true }).evaluate(button => button.click());
      await stable();
      const sourceRow = await diagramRows.first().evaluate(element => element.closest('[data-reading-id]').dataset.readingId);
      await reading().evaluate(element => { element.scrollTop = element.scrollHeight; }); await stable();
      await expect(page.locator(`[data-reading-id=${JSON.stringify(sourceRow)}]`)).toHaveCount(0);
      await reading().evaluate(element => { element.scrollTop = element.scrollHeight; });
      await eventually(async () => { if (await page.locator(`[data-reading-id=${JSON.stringify(sourceRow)}]`).count()) return true; await reading().evaluate(element => { element.scrollTop -= 400; }); return false; });
      await expect(page.locator(`[data-reading-id=${JSON.stringify(sourceRow)}] [data-mermaid-view]`)).toHaveAttribute('data-mermaid-view', 'source');
      await expect(diagramRows.last()).toHaveAttribute('data-mermaid-view', 'diagram');
      report.checks.push('virtual unmount/remount retains only the chosen duplicate source view');
      await page.screenshot({ path: join(directory, `${name}-remount.png`) });
      assert.deepEqual(report.csp, []);
      await page.close();

      // The first diagram falls outside the initial snapshot + one-page warmup.
      // Loading it must preserve the reader's position before moving to its row.
      page = await setup();
      const history = await create();
      await submit(history.session, `${fence(source)}\n\n${fence(source)}`);
      for (let index = 0; index < 128; index++) await submit(history.session, `Later history message ${index}.`);
      const requests = []; report.historyRequests = requests;
      let socketNumber = 0;
      page.on('websocket', socket => {
        const connection = ++socketNumber, pending = new Map();
        socket.on('close', () => pending.clear());
        socket.on('framesent', ({ payload }) => {
          try {
            assert(Buffer.byteLength(payload) <= (8 << 20));
            const frame = JSON.parse(String(payload));
            if (frame.method !== 'sessions.history_page') return;
            assert(requests.length < 128 && pending.size < 16, 'History probe bound exceeded');
            const params = { session_id: frame.params.session_id, direction: frame.params.direction, cursor: frame.params.cursor, limit: frame.params.limit };
            assert(Buffer.byteLength(JSON.stringify(params)) <= 1024, 'History metadata bound exceeded');
            const request = { connection, id: frame.id, method: frame.method, params, settled: false };
            requests.push(request); pending.set(frame.id, request);
          } catch (error) { if (report.errors.length < 64) report.errors.push(String(error).slice(0, 4096)); }
        });
        socket.on('framereceived', ({ payload }) => {
          try {
            assert(Buffer.byteLength(payload) <= (8 << 20));
            const reply = JSON.parse(String(payload)), request = pending.get(reply.id);
            if (request) { request.settled = !reply.error; request.nextCursor = reply.result?.next_cursor; pending.delete(reply.id); }
          } catch (error) { if (report.errors.length < 64) report.errors.push(String(error).slice(0, 4096)); }
        });
      });
      await page.goto(history.url); await ready(); await stable();
      const earlier = page.getByRole('button', { name: 'Load earlier messages', exact: true });
      await expect(earlier).toHaveCount(1);
      await reading().evaluate(element => { element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 })); element.scrollTop -= 400; });
      let nextCursor;
      for (let index = 0; index < 3; index++) {
        const beforePage = await stable(), firstRequest = requests.length;
        await expect(earlier).toBeEnabled();
        await earlier.evaluate(button => button.click());
        const completed = await eventually(() => requests.slice(firstRequest).find(request => request.params.session_id === history.session.id && request.params.direction === 'backward' && request.settled), { description: 'completed exact older history page' });
        if (nextCursor !== undefined) assert.equal(completed.params.cursor, nextCursor, 'Explicit older read uses the returned opaque cursor');
        assertAnchor(beforePage, await stable(), `older diagram history page ${index + 1} preserves reading anchor`);
        nextCursor = completed.nextCursor;
        if (nextCursor === null) break;
      }
      assert.equal(nextCursor, null, 'Bounded explicit history reads reached the original diagrams');
      await reading().evaluate(element => { element.scrollTop = 0; });
      const staticFigures = page.locator('[data-message-role="user"] [data-mermaid-view]');
      await expect(staticFigures).toHaveCount(2); await decoded(staticFigures.first().locator('img'));
      assert(requests.every(request => request.params.session_id === history.session.id && request.params.limit <= 100));
      report.checks.push('real older sessions.history_page renders static duplicate diagram fences');
      await staticFigures.first().getByRole('button', { name: 'Source', exact: true }).click();
      const staticRow = await staticFigures.first().evaluate(element => element.closest('[data-reading-id]').dataset.readingId);
      // ReadingList intentionally keeps the focused row mounted. Move focus away
      // before proving an ordinary unmount rather than defeating that guarantee.
      await page.getByRole('textbox', { name: 'Message WHIP', exact: true }).focus();
      await reading().evaluate(element => { element.scrollTop = element.scrollHeight; }); await stable();
      await expect(page.locator(`[data-reading-id=${JSON.stringify(staticRow)}]`)).toHaveCount(0);
      await reading().evaluate(element => { element.scrollTop = 0; });
      await expect(staticFigures.first()).toHaveAttribute('data-mermaid-view', 'source');
      await expect(staticFigures.last()).toHaveAttribute('data-mermaid-view', 'diagram');
      report.checks.push('static same-source duplicate fence choices stay independent after virtual remount');
      await page.screenshot({ path: join(directory, `${name}-older-history.png`) });
      assert.deepEqual(report.csp, []);
      await page.close();

      // Native history has complete canonical text or explicit scoped content,
      // never retired clipped presentation metadata. Exercise the real source
      // byte boundary instead: no misleading image may render from an excerpt.
      page = await setup();
      const oversized = await create();
      const largeSource = source + '\n%%' + ' bounded comment'.repeat(2300);
      assert(Buffer.byteLength(largeSource) > 32768 && Buffer.byteLength(largeSource) < 65536);
      await submit(oversized.session, fence(largeSource));
      const stored = await oversized.session.history.page({ direction: 'backward', limit: 16 }, deadline());
      assert.equal(stored.messages.find(message => message.role === 'assistant').parts[0].text, fence(largeSource));
      await page.goto(oversized.url); await ready();
      const limited = page.locator('[data-message-role="assistant"] [data-mermaid-view]');
      await expect(limited.getByRole('status').filter({ hasText: 'Diagram source exceeds 32 KiB.' })).toBeVisible();
      await expect(limited.locator('img')).toHaveCount(0);
      await expect(limited.getByText(/Showing a bounded excerpt/)).toBeVisible();
      assert.equal(Buffer.byteLength(await limited.locator('pre').innerText()), 16384);
      await limited.getByRole('button', { name: 'Copy source', exact: true }).click();
      assert.equal(await page.evaluate(() => window.__mermaidCopies.at(-1)), largeSource);
      report.checks.push('actual complete oversized native source stays source-only with an explicit bounded excerpt; exact full copy remains available');
      await page.screenshot({ path: join(directory, `${name}-bounded-source.png`) });
      assert.deepEqual(report.csp, []);
      assert.deepEqual(report.errors, []);
      report.passed = true;
    } catch (error) {
      report.passed = false; report.failure = error.stack ?? String(error);
      await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
      if (page && !page.isClosed()) await writeFile(join(directory, `${name}-failure.txt`), (await page.locator('body').innerText()).slice(0, 16384)).catch(() => {});
      console.error(`${name}: ${report.failure}`);
    } finally {
      releasePending(); await browser?.close();
      await writeFile(join(directory, 'results.json'), JSON.stringify(reports, null, 2) + '\n');
    }
    console.log(`${name}: ${report.passed ? 'passed' : 'failed'} (${report.checks.length} checks)`);
  }
} finally { await fixture.close(); }
if (reports.some(report => !report.passed)) process.exitCode = 1;
