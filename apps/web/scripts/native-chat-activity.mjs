import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, repository, startFixture } from './native-fixture.mjs';
import { checkComposerReading } from './composer-reading.mjs';
import { checkActivityReading } from './native-activity-reading.mjs';

const directory = process.env.WHIP_CHAT_ACTIVITY_RESULTS ?? '/tmp/whip-native-chat-activity-results';
await mkdir(directory, { recursive: true });
const report = [];
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  let fixture, browser, page;
  const errors = [], traffic = [], csp = [];
  try {
    assert(['chromium', 'firefox'].includes(name));
    fixture = await startFixture({ activityStreams: true, lifetimeMs: 900_000 });
    await mkdir(join(fixture.directory, 'source'));
    await writeFile(join(fixture.directory, 'persisted.md'), 'Persisted fixture body.');
    for (const file of ['one', 'two', 'three', ...Array.from({ length: 128 }, (_, index) => `file-${index}`)])
      await writeFile(join(fixture.directory, 'source', file + '.md'), `Read ${file}.`);
    const client = await fixture.connect(`activity-${name}`), { root } = await fixture.createRoot(client);
    const session = client.session(root.id), policy = await session.permissions.policy(deadline());
    await session.permissions.setMode({ mode: 'automatic', expected_revision: policy.revision }, randomUUID(), deadline());
    for (let index = 0; index < 12; index++) {
      const handle = session.submission([{ type: 'text', text: `Reading fixture ${index + 1}. ` + 'A retained paragraph for the reading and draft checks. '.repeat(8) }], randomUUID());
      await handle.send(deadline()); assert.equal((await handle.wait(deadline())).turn.state, 'succeeded');
    }
    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.setDefaultTimeout(15_000);
    page.on('pageerror', error => { if (errors.length < 64) errors.push(error.stack ?? error.message); });
    page.on('console', message => {
      if (message.type() === 'error' && /content.security.policy|violates.*directive/i.test(message.text()) && errors.length < 64) errors.push(message.text());
    });
    page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
      try {
        const request = JSON.parse(String(payload));
        assert(traffic.length < 25_000, 'Activity traffic evidence overflow');
        traffic.push({ method: request.method, session: request.params?.session_id });
      } catch (error) { if (errors.length < 64) errors.push(error.message); }
    }));
    await page.exposeFunction('__recordActivityCSP', directive => { if (csp.length < 64) csp.push(String(directive).slice(0, 256)); });
    await page.addInitScript(() => {
      if (!localStorage.getItem('whip.appearance.theme.v1')) localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' }));
      document.addEventListener('securitypolicyviolation', event => { void window.__recordActivityCSP(event.violatedDirective).catch(() => {}); });
    });
    const manifest = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
    for (const [path, file] of Object.entries(manifest.files)) {
      const response = await fetch(fixture.info.web + '/' + path);
      assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256);
    }
    const url = `${fixture.info.web}/h/${client.runtimeID}/s/${root.id}`;
    await page.goto(url); await page.getByRole('textbox', { name: 'Message WHIP', exact: true }).waitFor();
    const reading = page.getByRole('region', { name: 'Conversation', exact: true });
    const dock = page.locator('[data-current-activity]');
    const working = reading.locator('[data-transcript-working]');
    const latest = page.getByRole('button', { name: 'Latest', exact: true });
    const tail = async () => { if (await latest.isVisible()) await latest.click(); await reading.evaluate(element => { element.scrollTop = element.scrollHeight; }); };
    const screenshot = label => page.screenshot({ path: join(directory, `${name}-${label}.png`) });
    const run = async text => { const handle = session.submission([{ type: 'text', text }], randomUUID()); await handle.send(deadline()); return handle; };
    const checks = [];
    console.log(`${name}: live reasoning and canonical restored file activity`);
    const saved = await run('activity:saved');
    const thought = page.locator('[data-activity-group]').filter({ hasText: 'Thought' });
    await expect(thought).toBeVisible();
    const thoughtStep = page.locator('[data-activity-step]').filter({ hasText: 'Thought' }).getByRole('button');
    await expect(thoughtStep).toHaveAttribute('aria-expanded', 'true');
    await thoughtStep.click(); await expect(thoughtStep).toHaveAttribute('aria-expanded', 'false');
    await thoughtStep.focus(); await page.keyboard.press('Enter');
    await expect(page.locator('[data-activity-detail]')).toContainText('Saved reasoning');
    await page.reload(); await expect(thought).toBeVisible();
    fixture.release('activity-saved-reasoning'); await saved.wait(deadline());
    await expect(thought).toHaveCount(0);
    await page.reload();
    const persisted = page.locator('[data-activity-group]').filter({ hasText: /read 1 file/i });
    await expect(persisted).toBeVisible(); await persisted.getByRole('button').focus(); await page.keyboard.press('Enter');
    await expect(page.locator('[data-activity-step]').filter({ hasText: 'persisted.md' })).toBeVisible();
    await page.keyboard.press('Enter'); await expect(persisted.getByRole('button')).toHaveAttribute('aria-expanded', 'false');
    checks.push('live reasoning reload/disclosure; settled reasoning cleared; canonical file execution restores with keyboard disclosure');
    await writeFile(join(directory, `${name}-composer-reading.json`), JSON.stringify(await checkComposerReading(page), null, 2));

    console.log(`${name}: actual operations, REPL and child wait`);
    const work = await run('activity:work');
    await tail();
    const group = page.locator('[data-activity-group]').filter({ hasText: /read 3 files/i });
    await expect(group).toHaveCount(1); await expect(group.getByRole('button')).toHaveAttribute('aria-expanded', 'true');
    await expect(page.locator('[data-activity-step]').filter({ hasText: /source\/(one|two|three)\.md/ })).toHaveCount(3);
    await expect(dock.getByRole('status')).toHaveText('tools.fixture_wait');
    await expect(working).toContainText('tools.fixture_wait…');
    await expect(working.locator('[data-activity-animation] > span')).toHaveCount(9);
    await expect(page.locator('[data-chat-activity]')).toHaveCount(0);
    await expect(dock.locator('[data-activity-animation]')).toHaveAttribute('data-activity-animation', 'running');
    const groupID = await group.getAttribute('data-activity-group');
    const operation = page.locator('[data-activity-step]').filter({ hasText: 'tools.fixture_wait' });
    await operation.getByRole('button').focus(); await page.keyboard.press('Enter');
    await expect(operation.getByRole('button')).toHaveAttribute('aria-expanded', 'true');
    await page.locator('[data-activity-detail]').getByRole('button', { name: 'Open in REPL' }).click();
    const notebook = page.locator('[data-repl-cell]').filter({ hasText: 'activity-reads' });
    await expect(notebook).toContainText('Running');
    await page.goBack(); await expect(group).toHaveCount(1);
    // Returning from REPL remounts the indicator's deliberate 1s delay.
    await expect(dock.locator('[data-activity-animation]')).toHaveAttribute('data-activity-animation', 'running');
    if (name === 'chromium') {
      const cdp = await page.context().newCDPSession(page);
      try {
        await cdp.send('Tracing.start', { categories: 'devtools.timeline', transferMode: 'ReturnAsStream' });
        const animation = await dock.locator('[data-activity-animation] > span').first().evaluate(async dot => {
          const first = Number(getComputedStyle(dot).opacity); await new Promise(resolve => setTimeout(resolve, 400));
          return { first, second: Number(getComputedStyle(dot).opacity) };
        });
        assert.notEqual(animation.first, animation.second);
        let timeout;
        const finished = new Promise((resolve, reject) => {
          timeout = setTimeout(() => reject(new Error('Animation trace did not finish')), 15_000);
          cdp.once('Tracing.tracingComplete', resolve);
        });
        let stream;
        try { const [, event] = await Promise.all([cdp.send('Tracing.end'), finished]); ({ stream } = event); } finally { clearTimeout(timeout); }
        let trace = '';
        try {
          for (;;) { const part = await cdp.send('IO.read', { handle: stream }); trace += part.data; assert(Buffer.byteLength(trace) <= 16 << 20); if (part.eof) break; }
        } finally { await cdp.send('IO.close', { handle: stream }); }
        const layouts = JSON.parse(trace).traceEvents.filter(event => event.name === 'Layout').length;
        assert(layouts <= 2, `Opacity animation caused ${layouts} layouts in 400ms`);
        await writeFile(join(directory, 'animation-trace.json'), trace);
        await writeFile(join(directory, 'animation-observation.json'), JSON.stringify({ ...animation, layouts, intervalMs: 400 }));
      } finally { await cdp.detach(); }
    }
    await dock.getByRole('button', { name: 'Pause activity animation' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-motion', 'reduce');
    await expect(dock.locator('[data-activity-animation]')).toHaveAttribute('data-activity-animation', 'static');
    await expect(working.locator('[data-activity-animation]')).toHaveAttribute('data-activity-animation', 'static');
    for (const width of [390, 320]) {
      await page.setViewportSize({ width, height: 844 }); await expect(dock).toBeInViewport();
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)); await screenshot(`narrow-${width}`);
    }
    for (const appearance of [{ theme: 'light', uiSize: 20, codeSize: 24, label: 'light-large-type' }, { theme: 'claude-code', uiSize: 12, codeSize: 10, label: 'dark-small-type' }]) {
      await page.evaluate(value => {
        localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: value.theme }));
        localStorage.setItem('whip.appearance.display.v1', JSON.stringify({ version: 1, display: {
          uiFont: 'system', codeFont: 'system', uiSize: value.uiSize, codeSize: value.codeSize, wrapCode: true, contrast: 'more', motion: 'reduce',
        } }));
      }, appearance);
      await page.reload(); await expect(dock.getByRole('status')).toHaveText('tools.fixture_wait'); await tail();
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), appearance.label);
      await screenshot(appearance.label);
    }
    await page.setViewportSize({ width: 1280, height: 900 });
    const historyReads = () => traffic.filter(item => item.method === 'sessions.history_page' && item.session === root.id).length;
    const priorHistoryReads = historyReads();
    fixture.release('activity-reads');
    await expect(dock.getByRole('status')).toHaveText('Waiting for work to continue');
    assert.equal(historyReads(), priorHistoryReads, 'A host phase transition must not rehydrate canonical history');
    const workTurn = (await work.check(deadline())).evidence.turn.id;
    const child = await eventually(async () => (await session.turns.operations(workTurn, {}, deadline())).items.find(item => item.capability === 'agents.spawn')?.result?.value?.session_id);
    await expect(page.locator(`[data-inline-agent="${child}"]`)).toHaveCount(1);
    assert.equal(traffic.filter(item => item.method === 'sessions.history_page' && item.session === child).length, 0, 'An inline child summary must not hydrate its private transcript');
    assert.equal(await group.getAttribute('data-activity-group'), groupID);
    fixture.release('activity-child');
    await expect(dock.getByRole('status')).toHaveText('Writing a response');
    const selected = await group.getByRole('button').evaluate(element => { const range = document.createRange(); range.selectNodeContents(element); getSelection().removeAllRanges(); getSelection().addRange(range); return getSelection().toString(); });
    fixture.release('activity-complete'); assert.equal((await work.wait(deadline())).turn.state, 'succeeded');
    assert.equal(await page.evaluate(() => getSelection().toString()), selected);
    await page.evaluate(() => { getSelection().removeAllRanges(); document.activeElement?.blur(); });
    await expect(group.getByRole('button')).toHaveAttribute('aria-expanded', 'false');
    checks.push('three actual file reads, exact live executor state, shared REPL navigation, child spawn and released-worker wait, scoped inline child, reduced motion/narrow widths, selection through completion and automatic fold');

    console.log(`${name}: actual 128-operation tree`);
    const tree = await run('activity:tree');
    await reading.evaluate(element => {
      window.activityMaximumRows = 0;
      const sample = () => { window.activityMaximumRows = Math.max(window.activityMaximumRows, element.querySelectorAll('[data-reading-id]').length); };
      const observer = new MutationObserver(sample); observer.observe(element, { subtree: true, childList: true });
      window.finishActivityRows = () => { sample(); observer.disconnect(); return window.activityMaximumRows; };
    });
    await tail();
    await expect.poll(async () => { await tail(); return reading.locator('[data-activity-step]').filter({ hasText: 'source/file-' }).count(); }).toBeGreaterThan(0);
    assert(await reading.locator('[data-reading-id]').count() < 80); await screenshot('long-tree');
    fixture.release('activity-tree'); assert.equal((await tree.wait(deadline())).turn.state, 'succeeded');
    await expect(reading.locator('[data-activity-step]').filter({ hasText: 'source/file-' })).toHaveCount(0, { timeout: 12_000 });
    const maximumTreeRows = await page.evaluate(() => window.finishActivityRows()); assert(maximumTreeRows < 80);
    checks.push('actual 128-file operation tree remains below 80 mounted rows through expansion/fold');
    console.log(`${name}: real Markdown stream and reading intent`);
    const readingChecks = await checkActivityReading({ page, fixture, run, directory, name });
    checks.push(...readingChecks.checks);
    for (const text of ['First follow-up without tools', 'Second follow-up without tools']) {
      const follow = await run(text); assert.equal((await follow.wait(deadline())).turn.state, 'succeeded');
      await tail();
      await expect(reading.locator('[data-markdown-block]').filter({ hasText: text })).toBeVisible();
      await expect.poll(() => reading.evaluate(element => !![...element.querySelectorAll('[data-reading-id]')].at(-1)?.querySelector('[data-markdown-block]'))).toBe(true);
      await expect(reading.locator('[data-transcript-working]')).toHaveCount(0);
    }
    await page.getByRole('button', { name: 'REPL', exact: true }).click();
    await expect(notebook).toContainText('Review complete.');
    await page.goBack(); await expect(reading).toBeVisible();
    await reading.evaluate(element => { element.scrollTop = 300; });
    await expect(page.getByRole('textbox', { name: 'Message WHIP', exact: true })).toBeInViewport();
    checks.push('later replies finish in prose; prior canonical execution remains in the REPL; composer remains reachable during history reading');
    assert.deepEqual(errors, []); assert.deepEqual(csp, []);
    report.push({ browser: name, rendererDigest: manifest.digest, checks, maximumTreeRows,
      dispositions: ['Native reasoning is live preview only, never restored from completed history.', 'Actual files.read operations settle before the explicit fixture executor hold; no injected Reading-files phase.', 'agents.wait_after_cell settles the cell and releases execution permission; UI reports Waiting for work to continue instead of an invented active agents.wait operation.'] });
    await writeFile(join(directory, 'results.json'), JSON.stringify(report, null, 2));
  } catch (error) {
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    const surface = await page?.locator('[aria-label=Conversation]').evaluate(element => ({
      top: element.scrollTop, height: element.clientHeight, total: element.scrollHeight,
      mounted: [...element.querySelectorAll('[data-reading-id]')].slice(0, 80).map(row => ({ id: row.dataset.readingId, text: row.textContent.slice(0, 256) })),
    })).catch(() => undefined);
    await writeFile(join(directory, `${name}-failure.json`), JSON.stringify({ error: error.stack, errors, csp, surface, traffic: traffic.slice(-200), fixture: fixture?.output }, null, 2));
    throw error;
  } finally { try { await browser?.close(); } finally { await fixture?.close(); } }
}
