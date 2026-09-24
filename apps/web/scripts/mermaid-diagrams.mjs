import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';
import { _electron, chromium, expect, firefox } from '@playwright/test';
import { createWhipClient } from '../../../packages/sdk/dist/index.js';
import { eventually, startFixture } from '../../../packages/sdk/scripts/fixture.mjs';

// npm run pack:web && node apps/web/scripts/mermaid-diagrams.mjs
// Real packaged renderer + isolated daemon, never the person's running daemon.
// Only the clipped-history case injects wire metadata (explicitly reported below).
const directory = process.env.WHIP_WEB_BROWSER_RESULTS ?? '/tmp/whip-mermaid-diagrams';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert.ok(names.every(name => ['chromium', 'firefox', 'electron'].includes(name)));
await mkdir(directory, { recursive: true });
const manifest = JSON.parse(await readFile(new URL('../renderer-manifest.json', import.meta.url), 'utf8'));
const fixture = await startFixture({ allowedOrigins: names.includes('electron') ? ['whip-app://bundle'] : [], env: { WHIP_WEB_INLINE_IMAGES_FIXTURE: '1' }, lifetimeMs: 15 * 60_000 });
const origin = fixture.info.endpoint.replace(/^ws/, 'http').replace('/api/v3/ws', '');
const reports = [];
const source = 'flowchart LR\n  Root[Root agent] --> Worker[Diagram worker]\n  Worker --> Reader[Conversation]';
const fence = code => '```mermaid\n' + code + '\n```';
const decoded = image => expect.poll(() => image.evaluate(img => img.complete && img.naturalWidth > 0)).toBe(true);

async function checkElectron() {
  // npm run build:desktop -- --renderer-ready first. The app and its automatically
  // started local runtime use disposable homes; the test chat uses our SDK fixture.
  const repository = fileURLToPath(new URL('../../../', import.meta.url));
  const temporary = await mkdtemp('/tmp/whip-mermaid-desktop-');
  for (const name of ['user', 'home', 'data', 'bin']) await mkdir(join(temporary, name), { mode: 0o700 });
  const binary = join(temporary, 'bin/whipcode');
  await copyFile(join(repository, 'apps/desktop/.stage/native/whipcode'), binary);
  const env = { ...process.env, HOME: join(temporary, 'user'), WHIP_DESKTOP_FIXTURE: '1', WHIPCODE_HOME: join(temporary, 'home'),
    WHIPCODE_NETWORK: '0', WHIP_DESKTOP_USER_DATA: join(temporary, 'data'), WHIP_DESKTOP_EXECUTABLE: binary };
  const report = { browser: 'electron', rendererDigest: manifest.digest, checks: [], errors: [] };
  reports.push(report);
  let app, page;
  const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `mermaid-electron-${crypto.randomUUID()}`, clientKind: 'human' });
  try {
    const staged = JSON.parse(await readFile(join(repository, 'apps/desktop/.stage/app/renderer-manifest.json'), 'utf8'));
    assert.equal(staged.digest, manifest.digest, 'Desktop stage must match current packaged renderer');
    app = await _electron.launch({ args: [join(repository, 'apps/desktop/.stage/app')], env, timeout: 30_000 });
    page = await app.firstWindow(); page.setDefaultTimeout(15_000);
    report.version = await app.evaluate(() => process.versions.electron);
    page.on('pageerror', error => report.errors.push(error.message));
    await page.addInitScript(() => {
      window.__mermaidCsp = [];
      document.addEventListener('securitypolicyviolation', event => window.__mermaidCsp.push(event.violatedDirective));
    });
    await page.getByRole('button', { name: 'Manage servers', exact: true }).click();
    await page.getByRole('button', { name: 'Add server', exact: true }).click();
    const hosts = page.getByRole('dialog', { name: 'Add server', exact: true });
    await hosts.getByRole('textbox', { name: /Server name/ }).fill('Mermaid isolated fixture');
    await hosts.getByRole('textbox', { name: 'Server address', exact: true }).fill(fixture.info.endpoint);
    await hosts.getByRole('button', { name: 'Connect', exact: true }).click(); await hosts.waitFor({ state: 'hidden' });
    await client.connect();
    const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
    const session = client.session(created.result.root_id);
    await page.goto(`whip-app://bundle/h/${fixture.info.runtime_id}/s/${created.result.root_id}`);
    await expect(page.getByRole('textbox', { name: 'Message WHIP', exact: true })).toBeEnabled();
    const prompt = `hold:electron-mermaid\n\n${fence(source)}`;
    const turn = session.submit({ text: prompt });
    const figure = page.locator('[data-message-role="assistant"] [data-mermaid-view]');
    await expect(figure.getByRole('status')).toHaveText('Diagram will render when this response finishes.');
    await expect(figure.locator('img')).toHaveCount(0);
    await fixture.release(prompt.slice(5));
    assert.equal((await turn.result({ signal: AbortSignal.timeout(15_000) })).status, 'succeeded');
    await decoded(figure.locator('img'));
    await figure.getByRole('button', { name: 'Source', exact: true }).click();
    await expect(figure.locator('pre')).toHaveText(source);
    await figure.getByRole('button', { name: 'Diagram', exact: true }).click();
    await figure.getByRole('button', { name: 'Expand', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Mermaid · Flowchart', exact: true });
    await decoded(dialog.locator('img')); await page.keyboard.press('Escape');
    await page.reload(); await decoded(figure.locator('img'));
    assert.deepEqual(await page.evaluate(() => window.__mermaidCsp), []);
    assert.deepEqual(report.errors, []);
    await page.screenshot({ path: join(directory, 'electron-history.png') });
    report.checks.push('same staged renderer; local custom-protocol worker/font/image decode; live settlement; source/expand; persisted history; production CSP');
    report.passed = true;
  } catch (error) {
    report.passed = false; report.failure = error.stack ?? String(error);
    await page?.screenshot({ path: join(directory, 'electron-failure.png') }).catch(() => {});
    console.error(report.failure);
  } finally {
    client.close();
    if (app) await app.close();
    // Never issue stop without the disposable environment and copied executable.
    await promisify(execFile)(binary, ['daemon', 'stop'], { env, timeout: 15_000 });
    await rm(temporary, { recursive: true, force: true });
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
    const browser = await { chromium, firefox }[name].launch();
    const context = await browser.newContext({ viewport: { width: 1200, height: 900 }, reducedMotion: 'reduce' });
    const client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: `mermaid-${crypto.randomUUID()}`, clientKind: 'human' });
    const report = { browser: name, version: browser.version(), rendererDigest: manifest.digest, checks: [], errors: [] };
    reports.push(report);
    let page, releasePending = () => {};
    const setup = async () => {
      const next = await context.newPage();
      next.setDefaultTimeout(15_000);
      next.on('pageerror', error => report.errors.push(error.message));
      await next.addInitScript(() => {
        window.__mermaidCopies = [];
        window.__mermaidCsp = [];
        // Observe the existing browser clipboard boundary, including exact bytes,
        // without changing the person's OS clipboard or needing Firefox permissions.
        Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async text => { window.__mermaidCopies.push(text); } } });
        document.addEventListener('securitypolicyviolation', event => window.__mermaidCsp.push({ directive: event.violatedDirective, blocked: event.blockedURI }));
      });
      return next;
    };
    const create = async () => {
      const created = await client.sessions.create({ cwd: fixture.directory, model: 'model', provider: 'provider' }).result();
      assert.equal(created.status, 'succeeded');
      return { session: client.session(created.result.root_id), url: `${origin}/h/${fixture.info.runtime_id}/s/${created.result.root_id}` };
    };
    const submit = async (session, text) => assert.equal((await session.submit({ text }).result({ signal: AbortSignal.timeout(15_000) })).status, 'succeeded');
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
      await client.connect();
      page = await setup();
      const ordinary = await create();
      await submit(ordinary.session, 'Ordinary code stays code.\n\n```javascript\nconst untouched = 42;\n```');
      const assets = [];
      page.on('request', request => assets.push(request.url()));
      await page.goto(ordinary.url); await ready();
      await expect(page.locator('[data-message-role="assistant"] pre')).toContainText('const untouched = 42;');
      await expect(page.locator('[data-mermaid-view]')).toHaveCount(0);
      assert.equal(assets.filter(url => /mermaid-worker/.test(url)).length, 0, 'Ordinary chat loaded the diagram worker');
      report.checks.push('ordinary fences unchanged; no worker load');

      const live = await create();
      await page.goto(live.url); await ready();
      const prompt = `hold:mermaid-${name}

${fence(source)}

Between duplicate diagrams.

${fence(source)}`;
      const turn = live.session.submit({ text: prompt });
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
      assert.equal((await turn.result({ signal: AbortSignal.timeout(15_000) })).status, 'succeeded');
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
      await page.getByRole('button', { name: 'Copy response', exact: true }).click();
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
      assert.deepEqual(await page.evaluate(() => window.__mermaidCsp), []);
      await page.close();

      // Hold only the cold worker asset. The next paragraph is the reading anchor
      // while the preceding mounted history diagram changes source -> image.
      page = await setup();
      let releaseWorker;
      const workerGate = new Promise(resolve => { releaseWorker = resolve; releasePending = resolve; });
      await page.route('**/mermaid-worker-*.js', async route => { await workerGate; await route.continue(); });
      const scroll = await create();
      const paragraphs = Array.from({ length: 18 }, (_, i) => `Reading paragraph ${i}. ${'Keep this place while a diagram is laid out. '.repeat(12)}`);
      await submit(scroll.session, `${fence(source)}

${fence(source)}

${paragraphs.join('\n\n')}`);
      await page.goto(scroll.url); await ready();
      const prose = page.locator('[data-message-role="assistant"] [data-markdown-block]');
      await reading().evaluate(element => element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 })));
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
      await expect(page.locator(`[data-reading-id="${sourceRow}"]`)).toHaveCount(0);
      await reading().evaluate(element => { element.scrollTop = element.scrollHeight; });
      await eventually(async () => { if (await page.locator(`[data-reading-id="${sourceRow}"]`).count()) return true; await reading().evaluate(element => { element.scrollTop -= 400; }); return false; });
      await expect(page.locator(`[data-reading-id="${sourceRow}"] [data-mermaid-view]`)).toHaveAttribute('data-mermaid-view', 'source');
      await expect(diagramRows.last()).toHaveAttribute('data-mermaid-view', 'diagram');
      report.checks.push('virtual unmount/remount retains only the chosen duplicate source view');
      await page.screenshot({ path: join(directory, `${name}-remount.png`) });
      assert.deepEqual(await page.evaluate(() => window.__mermaidCsp), []);
      await page.close();

      // The first diagram falls outside the initial snapshot + one-page warmup.
      // Loading it must preserve the reader's position before moving to its row.
      page = await setup();
      const history = await create();
      await submit(history.session, `${fence(source)}\n\n${fence(source)}`);
      for (let index = 0; index < 128; index++) await submit(history.session, `Later history message ${index}.`);
      const requests = [], replies = new Set();
      page.on('websocket', socket => {
        socket.on('framesent', ({ payload }) => requests.push(JSON.parse(String(payload))));
        socket.on('framereceived', ({ payload }) => { const reply = JSON.parse(String(payload)); if (reply.id && !reply.method) replies.add(reply.id); });
      });
      await page.goto(history.url); await ready(); await stable();
      const earlier = page.getByRole('button', { name: 'Load earlier messages', exact: true });
      await expect(earlier).toHaveCount(1);
      await reading().evaluate(element => { element.dispatchEvent(new WheelEvent('wheel', { bubbles: true, deltaY: -1 })); element.scrollTop -= 400; });
      const beforePage = await stable(), firstRequest = requests.length;
      await earlier.evaluate(button => button.click());
      await eventually(() => requests.slice(firstRequest).some(request => request.method === 'history.page' && replies.has(request.id)), { description: 'completed older history page' });
      assertAnchor(beforePage, await stable(), 'older diagram history page preserves reading anchor');
      await reading().evaluate(element => { element.scrollTop = 0; });
      const staticFigures = page.locator('[data-message-role="user"] [data-mermaid-view]');
      await expect(staticFigures).toHaveCount(2); await decoded(staticFigures.first().locator('img'));
      report.checks.push('real older history.page renders static duplicate diagram fences');
      await staticFigures.first().getByRole('button', { name: 'Source', exact: true }).click();
      const staticRow = await staticFigures.first().evaluate(element => element.closest('[data-reading-id]').dataset.readingId);
      // ReadingList intentionally keeps the focused row mounted. Move focus away
      // before proving an ordinary unmount rather than defeating that guarantee.
      await page.getByRole('textbox', { name: 'Message WHIP', exact: true }).focus();
      await reading().evaluate(element => { element.scrollTop = element.scrollHeight; }); await stable();
      await expect(page.locator(`[data-reading-id="${staticRow}"]`)).toHaveCount(0);
      await reading().evaluate(element => { element.scrollTop = 0; });
      await expect(staticFigures.first()).toHaveAttribute('data-mermaid-view', 'source');
      await expect(staticFigures.last()).toHaveAttribute('data-mermaid-view', 'diagram');
      report.checks.push('static same-source duplicate fence choices stay independent after virtual remount');
      await page.screenshot({ path: join(directory, `${name}-older-history.png`) });
      assert.deepEqual(await page.evaluate(() => window.__mermaidCsp), []);
      await page.close();

      // Keep real durable history and revision/cursor semantics; only this single
      // wire message gets simulated clipped presentation, not a fake React tree.
      page = await setup();
      const clipped = await create();
      await submit(clipped.session, `CLIPPED-MERMAID

${fence(source)}`);
      let injected = 0;
      await page.routeWebSocket('**/api/v3/ws', route => {
        const server = route.connectToServer();
        server.onMessage(data => {
          const message = JSON.parse(String(data));
          for (const entry of [...(message.result?.messages ?? []), ...(message.result?.entries ?? [])]) {
            const record = entry.message ?? entry;
            if (record.role === 'assistant' && typeof record.content === 'string' && record.content.startsWith('CLIPPED-MERMAID')) {
              record.content = '';
              record.presentation = { version: 1, turn_id: 'clipped-mermaid', omitted: 1, parts: [{ id: 'clipped-mermaid-part', kind: 'text', text: fence(source), omitted: 1 }] };
              injected++; report.clippedInjections = injected;
            }
          }
          route.send(JSON.stringify(message));
        });
      });
      await page.goto(clipped.url); await ready();
      const truncated = page.locator('[data-message-role="assistant"] [data-mermaid-view]');
      await expect(truncated.getByRole('status').filter({ hasText: 'This diagram source is incomplete. Showing source.' })).toBeVisible();
      await expect(truncated.locator('img')).toHaveCount(0); await expect(truncated.locator('pre')).toHaveText(source);
      assert.ok(injected > 0);
      report.checks.push({ label: 'simulated clipped wire-history metadata stays source, no misleading partial image', injected });
      await page.screenshot({ path: join(directory, `${name}-truncated-source.png`) });
      assert.deepEqual(await page.evaluate(() => window.__mermaidCsp), []);
      assert.deepEqual(report.errors, []);
      report.passed = true;
    } catch (error) {
      report.passed = false; report.failure = error.stack ?? String(error);
      await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
      if (page && !page.isClosed()) await writeFile(join(directory, `${name}-failure.txt`), await page.locator('body').innerText()).catch(() => {});
      console.error(`${name}: ${report.failure}`);
    } finally {
      releasePending(); client.close(); await browser.close();
      await writeFile(join(directory, 'results.json'), JSON.stringify(reports, null, 2) + '\n');
    }
    console.log(`${name}: ${report.passed ? 'passed' : 'failed'} (${report.checks.length} checks)`);
  }
} finally { await fixture.close(); }
if (reports.some(report => !report.passed)) process.exitCode = 1;
