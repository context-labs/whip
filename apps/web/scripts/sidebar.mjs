import assert from 'node:assert/strict';
import { mkdir, readFile, realpath, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

// Production assets and native catalog in an isolated runtime. No user runtime.
const directory = process.env.WHIP_WEB_SIDEBAR_RESULTS ?? '/tmp/whip-sidebar-results';
await mkdir(directory, { recursive: true });
const results = {};
for (const name of (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',')) {
  const fixture = await startFixture();
  let browser, page;
  const frames = [], errors = [], checks = [], catalogReads = [];
  const proxyConnections = new Set(), proxyClosing = new Set();
  let proxyClosed = false, armFiltered = false, releaseSearch, filteredReply;
  const recordError = error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); };
  const closeEndpoint = endpoint => { const pending = endpoint.close().catch(recordError).finally(() => proxyClosing.delete(pending)); proxyClosing.add(pending); };
  try {
    browser = await ({ chromium, firefox }[name]).launch();
    const context = await browser.newContext({ viewport: { width: 1440, height: 960 } });
    context.setDefaultTimeout(10_000);
    page = await context.newPage();
    const client = await fixture.connect(`sidebar-${crypto.randomUUID()}`);
    const origin = fixture.info.web;
    const route = id => `/h/${fixture.info.runtime_id}/s/${id}`;
    page.on('pageerror', recordError);
    page.on('console', event => { if (event.type() === 'error' && /content.security.policy|violates.*directive|refused to (execute|apply|load)/i.test(event.text())) recordError(event.text()); });
    page.on('websocket', socket => {
      const pending = new Map();
      socket.on('framesent', ({ payload }) => {
        try {
          const request = JSON.parse(String(payload));
          assert(frames.length < 25000, 'Sidebar traffic evidence bound exceeded');
          const rootIDs = request.method === 'trees.summaries' ? request.params?.root_ids : undefined;
          if (rootIDs !== undefined) assert(Array.isArray(rootIDs) && rootIDs.length <= 64 && rootIDs.every(id => typeof id === 'string' && id.length <= 128));
          frames.push({ method: request.method, params: { root_ids: rootIDs } });
          if (request.method === 'trees.list' && request.params?.search) {
            assert(catalogReads.length < 128, 'Sidebar search evidence bound exceeded');
            const record = { id: request.id, search: request.params.search, started: performance.now() }; catalogReads.push(record); pending.set(request.id, record);
          }
        } catch (error) { recordError(error); }
      });
      socket.on('framereceived', ({ payload }) => {
        try {
          const reply = JSON.parse(String(payload)), record = pending.get(reply.id);
          if (record) { record.replied = true; record.received = performance.now(); record.error = reply.error?.kind; record.roots = reply.result?.items?.map(item => item.root_id); pending.delete(reply.id); }
        } catch (error) { recordError(error); }
      });
    });
    const sidebar = () => page.locator('aside[aria-label="Session navigation"]');
    const separator = () => page.getByRole('separator', { name: 'Resize session navigation' });
    const saved = () => page.getByLabel('Saved sessions', { exact: true });
    const group = cwd => sidebar().getByRole('button', { name: cwd, exact: true });
    const ready = () => page.getByLabel('Message WHIP', { exact: true }).waitFor();
    const paths = [join(fixture.directory, 'repo/main'), join(fixture.directory, 'worktrees/main'), join(fixture.directory, 'sdk')];
    for (const path of paths) await mkdir(path, { recursive: true });
    for (const [index, path] of paths.entries()) paths[index] = await realpath(path);
    const created = [];
    for (let i = 0; i < 140; i++) created.push(await fixture.createRoot(client));
    // Native catalog pages have immutable ID order. Assign test roles after
    // allocation so the first page contains both small groups and part of the
    // large group; do not fabricate recency or depend on random ID placement.
    created.sort((a, b) => a.tree.id < b.tree.id ? 1 : -1);
    const roots = created.map(item => item.root.id);
    for (const [i, item] of created.entries()) {
      await client.setWorkingDirectory({ id: crypto.randomUUID(), session_id: item.root.id, expected_revision: item.root.config_revision, path: paths[i < 130 ? 0 : i < 135 ? 1 : 2] }, deadline());
      await client.trees.update(item.tree.id, item.tree.revision, { ...item.tree.metadata,
        title: `Session ${String(i).padStart(3, '0')}${i === 139 ? ' — a long session title to verify clipping without expanding the navigation' : ''}` }, deadline());
    }
    await page.routeWebSocket('**/api/v4/ws', route => {
      if (proxyClosed || proxyConnections.size >= 256) { if (!proxyClosed) recordError(new Error('Sidebar proxy connection bound exceeded')); closeEndpoint(route); return; }
      const server = route.connectToServer(); let closed = false, target;
      const retire = () => { if (closed) return; closed = true; proxyConnections.delete(retire); closeEndpoint(route); closeEndpoint(server); };
      const guarded = work => { try { if (!closed && !proxyClosed) work(); } catch (error) { recordError(error); retire(); } };
      proxyConnections.add(retire); route.onClose(retire); server.onClose(retire);
      route.onMessage(data => guarded(() => {
        const request = JSON.parse(String(data));
        if (armFiltered && request.method === 'trees.list' && request.params?.search === 'Session 139') target = request.id;
        server.send(data);
      }));
      server.onMessage(data => guarded(() => {
        const reply = JSON.parse(String(data));
        if (target !== undefined && reply.id === target && !filteredReply) {
          assert.equal(reply.error, undefined); assert(reply.result.items.some(item => item.root_id === roots[139]));
          filteredReply = { id: reply.id, roots: reply.result.items.map(item => item.root_id) };
          releaseSearch = () => { releaseSearch = undefined; guarded(() => route.send(data)); }; return;
        }
        route.send(data);
      }));
    });
    await page.goto(origin + route(roots[139])); await ready();
    await group(paths[2]).waitFor();
    assert.equal((await sidebar().boundingBox()).width, 320);
    assert.equal(await page.locator(`[data-sidebar-session="${roots[139]}"]`).evaluate(node => node.getBoundingClientRect().height), 28);
    assert.equal(frames.filter(frame => frame.method === 'sessions.history_page').length, 1);
    await eventually(() => frames.some(frame => frame.method === 'trees.summaries' && frame.params.root_ids.length > 1), { description: 'visible sidebar activity is requested' });
    const summaryRequests = frames.filter(frame => frame.method === 'trees.summaries');
    assert.ok(summaryRequests.every(frame => frame.params.root_ids.length <= 32), 'Summary requests exceed the daemon limit');
    assert.ok(new Set(summaryRequests.flatMap(frame => frame.params.root_ids)).size < 128, 'Sidebar polled the entire catalog instead of rendered rows');
    assert.equal(await sidebar().getByRole('link', { name: 'Settings', exact: true }).count(), 1);
    assert.ok((await group(paths[0]).innerText()).includes('main · repo'));
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('Paper density, 320px default, exact directories/worktrees, labels and no extra root hydration');

    const more = () => sidebar().getByRole('button', { name: `More sessions in ${paths[0]}`, exact: true });
    await more().waitFor();
    assert.equal(await sidebar().locator(`[data-sidebar-session][data-sidebar-cwd="${paths[0]}"]`).count(), 7);
    assert.equal(await more().getAttribute('aria-expanded'), 'false');
    await more().focus(); await page.keyboard.press('Enter');
    await eventually(async () => await sidebar().locator(`[data-sidebar-session][data-sidebar-cwd="${paths[0]}"]`).count() === 14);
    const header = sidebar().locator('[data-sidebar-header]');
    const edge = sidebar().locator('[data-sidebar-scroll-edge]');
    const headerBefore = await header.boundingBox();
    const searchBefore = await sidebar().getByRole('button', { name: 'Search sessions', exact: true }).boundingBox();
    assert.equal(await edge.evaluate(node => getComputedStyle(node).opacity), '0');
    await saved().evaluate(node => { node.scrollTop = 80; });
    await eventually(() => edge.evaluate(node => getComputedStyle(node).opacity === '1'));
    assert.deepEqual(await header.boundingBox(), headerBefore, 'New session must remain pinned');
    assert.ok((await sidebar().getByRole('button', { name: 'Search sessions', exact: true }).boundingBox()).y < searchBefore.y, 'Secondary navigation must scroll');
    await saved().evaluate(node => { node.scrollTop = 0; });
    await eventually(() => edge.evaluate(node => getComputedStyle(node).opacity === '0'));
    checks.push('pinned New session, scrolling secondary navigation, and scroll-only header edge');
    const less = sidebar().getByRole('button', { name: `Less sessions in ${paths[0]}`, exact: true });
    assert.equal(await less.count(), 0, 'Less must not appear before all sessions are shown');
    for (let batch = 0; batch < 20; batch++) {
      await saved().evaluate(node => { node.scrollTop = node.scrollHeight; });
      await more().or(less).waitFor();
      if (await less.count()) break;
      await more().click();
    }
    await less.waitFor();
    assert.equal(await less.getAttribute('aria-expanded'), 'true');
    await less.click();
    await saved().evaluate(node => { node.scrollTop = 0; });
    await more().waitFor();
    // The native ID page puts the small groups first; the virtual list may
    // omit the last two collapsed rows at this height. Inspect both positions
    // to prove the logical seven-row limit without demanding offscreen DOM.
    const collapsedRows = await sidebar().locator('[data-sidebar-session]').evaluateAll(rows => rows.map(row => ({ id: row.dataset.sidebarSession, cwd: row.dataset.sidebarCwd })));
    await saved().evaluate(node => { node.scrollTop = node.scrollHeight; });
    await eventually(async () => await sidebar().locator(`[data-sidebar-session][data-sidebar-cwd="${paths[0]}"]`).count() === 7);
    for (const row of await sidebar().locator('[data-sidebar-session]').evaluateAll(rows => rows.map(row => ({ id: row.dataset.sidebarSession, cwd: row.dataset.sidebarCwd })))) if (!collapsedRows.some(item => item.id === row.id)) collapsedRows.push(row);
    assert.equal(collapsedRows.filter(row => row.cwd === paths[0]).length, 7);
    assert.equal(collapsedRows.length, 17);
    await saved().evaluate(node => { node.scrollTop = 0; });
    checks.push('seven sessions per directory, keyboard More, and Less');

    const action = id => page.locator(`[data-sidebar-session="${id}"]`).getByRole('button', { name: /^Actions for / });
    const caret = path => group(path).locator('[data-directory-caret]');
    const opacity = locator => locator.evaluate(node => getComputedStyle(node).opacity);
    const rowFill = id => page.locator(`[data-sidebar-session="${id}"] a`).evaluate(node => getComputedStyle(node.parentElement).backgroundColor);
    const tokenFill = token => page.evaluate(token => {
      const probe = document.createElement('span'); probe.style.backgroundColor = `var(--whip-${token})`; document.body.append(probe);
      const color = getComputedStyle(probe).backgroundColor; probe.remove(); return color;
    }, token);
    await page.mouse.move(900, 500);
    assert.equal(await opacity(action(roots[139])), '0');
    assert.equal(await opacity(caret(paths[2])), '0');
    assert.equal(await rowFill(roots[139]), await tokenFill('hover'));
    await sidebar().getByRole('link', { name: 'Session 138', exact: true }).hover();
    assert.equal(await opacity(action(roots[138])), '1');
    assert.equal(await opacity(action(roots[139])), '0');
    assert.equal(await opacity(caret(paths[2])), '1', 'Child hover must reveal its directory caret');
    assert.equal(await opacity(caret(paths[1])), '0');
    assert.equal(await rowFill(roots[138]), await tokenFill('element'));
    await sidebar().getByRole('link', { name: /^Session 139/ }).hover();
    assert.equal(await rowFill(roots[139]), await tokenFill('hover'), 'Selected fill must survive hover');
    await group(paths[2]).hover();
    assert.equal(await group(paths[2]).evaluate(node => getComputedStyle(node).backgroundColor), 'rgba(0, 0, 0, 0)');
    assert.equal(await opacity(caret(paths[2])), '1');
    assert.ok((await caret(paths[2]).boundingBox()).x > (await group(paths[2]).locator('span').first().boundingBox()).x);
    await page.mouse.move(900, 500);
    assert.equal(await opacity(caret(paths[2])), '0');
    await action(roots[138]).focus();
    assert.equal(await opacity(action(roots[138])), '1', 'Keyboard focus must reveal the action');
    await action(roots[138]).click();
    await page.getByRole('menuitem', { name: 'Open in background tab', exact: true }).hover();
    assert.equal(await opacity(action(roots[138])), '1', 'Open menu must retain its visible trigger');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Hide navigation', exact: true }).focus();
    checks.push('hover-only desktop actions and trailing group caret, keyboard/open-menu access, and theme surface roles');

    await group(paths[1]).click();
    await page.waitForTimeout(2200);
    assert.equal(await group(paths[1]).getAttribute('aria-expanded'), 'false');
    await page.reload(); await ready();
    assert.equal(await group(paths[1]).getAttribute('aria-expanded'), 'false');
    await page.goto(origin + route(roots[133])); await ready();
    await eventually(async () => await group(paths[1]).getAttribute('aria-expanded') === 'true');
    assert.equal(await sidebar().getByRole('link', { name: 'Session 133', exact: true }).getAttribute('aria-current'), 'page');
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('collapse persists through polling/reload; explicit navigation expands and reveals');

    await separator().focus(); await page.keyboard.press('End');
    assert.equal((await sidebar().boundingBox()).width, 420);
    await page.screenshot({ path: join(directory, `${name}-420-light.png`) });
    await page.keyboard.press('Home'); assert.equal((await sidebar().boundingBox()).width, 256);
    await page.keyboard.press('ArrowRight'); assert.equal((await sidebar().boundingBox()).width, 264);
    const bounds = await separator().boundingBox();
    await page.mouse.move(bounds.x + 3, bounds.y + 100); await page.mouse.down(); await page.mouse.move(bounds.x + 159, bounds.y + 100); await page.mouse.up();
    assert.equal((await sidebar().boundingBox()).width, 420);
    await separator().focus(); await page.keyboard.press('Enter');
    assert.equal(await sidebar().count(), 0);
    await eventually(() => page.getByRole('button', { name: 'Open navigation', exact: true }).evaluate(node => node === document.activeElement), { description: 'focus transfers after hiding navigation' });
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    assert.equal((await sidebar().boundingBox()).width, 420);
    await page.setViewportSize({ width: 768, height: 960 });
    await eventually(async () => (await sidebar().boundingBox()).width === 288, { description: 'viewport cap' });
    await page.setViewportSize({ width: 1440, height: 960 });
    await eventually(async () => (await sidebar().boundingBox()).width === 420, { description: 'remembered width' });
    await separator().dblclick(); assert.equal((await sidebar().boundingBox()).width, 320);
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('pointer/keyboard resize, viewport cap, hide/restore and focus');

    await sidebar().getByRole('link', { name: `New session in ${paths[1]}`, exact: true }).click();
    const cwd = page.getByRole('button', { name: 'Project folder', exact: true });
    await eventually(async () => await cwd.getAttribute('title') === paths[1], { description: 'directory prefill after handshake' });
    await cwd.click();
    const chooser = page.getByRole('dialog', { name: 'Choose a folder', exact: true });
    await chooser.getByRole('button', { name: 'Edit path', exact: true }).click();
    const typed = chooser.getByRole('textbox', { name: 'Remote path', exact: true });
    await typed.fill(paths[0]); await page.waitForTimeout(2200); assert.equal(await typed.inputValue(), paths[0]);
    await typed.press('Enter');
    await chooser.getByRole('button', { name: 'Choose folder', exact: true }).click();
    await eventually(async () => await cwd.getAttribute('title') === paths[0], { description: 'explicit selected directory' });
    await page.reload(); await eventually(async () => await cwd.getAttribute('title') === paths[0], { description: 'edited draft directory survives reload' });
    await sidebar().getByRole('link', { name: 'New session', exact: true }).click();
    await eventually(async () => await cwd.getAttribute('title') === 'Choose a project folder', { description: 'fresh global draft starts without directory' });
    assert.equal(frames.filter(frame => ['trees.create', 'sessions.submit', 'sessions.spawn', 'sessions.configure', 'sessions.delete'].includes(frame.method)).length, 0);
    await page.goto(origin + route(roots[133]) + '?panel=execution');
    await page.getByRole('dialog', { name: 'Session details', exact: true }).waitFor();
    await page.goto(`${origin}/?cwd=${encodeURIComponent(paths[0])}&runtimeId=another-host`);
    await page.getByText('Select or add an execution host to begin.', { exact: true }).waitFor();
    assert.equal(await cwd.count(), 0);
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('directory prefill, native chooser fallback, edited draft persistence, fresh global folder, explicit unknown host and no admitted work');

    await page.getByRole('button', { name: 'Hide navigation', exact: true }).focus();
    await page.keyboard.press(process.platform === 'darwin' ? 'Meta+k' : 'Control+k');
    await page.getByRole('dialog', { name: 'Commands', exact: true }).getByRole('combobox').fill('Search sessions');
    await page.getByRole('option', { name: 'Search sessions', exact: true }).click();
    const input = page.getByLabel('Search sessions on this host', { exact: true });
    await eventually(() => input.evaluate(node => node === document.activeElement), { description: 'search dialog receives keyboard focus' });
    await input.fill('Session 133');
    await page.getByRole('dialog', { name: 'Search sessions', exact: true }).getByRole('link', { name: new RegExp('Session 133') }).waitFor();
    const resultLink = page.getByRole('dialog', { name: 'Search sessions', exact: true }).getByRole('link', { name: /Session 133/ });
    assert.ok((await resultLink.getAttribute('href')).includes('panel=execution'), 'Sidebar lost saved inspector location');
    const popupPromise = context.waitForEvent('page');
    await resultLink.click({ button: 'middle' });
    const popup = await popupPromise;
    await popup.getByRole('dialog', { name: 'Session details', exact: true }).waitFor();
    assert.ok(popup.url().includes('panel=execution'));
    await popup.getByRole('dialog', { name: 'Session details', exact: true }).getByRole('button', { name: 'Close', exact: true }).click();
    const originalWidth = (await sidebar().boundingBox()).width;
    await popup.getByRole('separator', { name: 'Resize session navigation' }).focus(); await popup.keyboard.press('Home');
    assert.equal((await sidebar().boundingBox()).width, originalWidth, 'Window layouts were shared');
    await popup.close();
    const count = frames.filter(frame => frame.method === 'sessions.history_page').length, backgroundOrigin = page.url();
    await page.getByRole('dialog', { name: 'Search sessions', exact: true }).getByRole('link', { name: /Session 133/ }).hover();
    await page.waitForTimeout(300); assert.equal(frames.filter(frame => frame.method === 'sessions.history_page').length, count);
    await page.getByRole('button', { name: 'Actions for Session 133 on Local', exact: true }).click();
    await page.getByRole('menuitem', { name: 'Open in background tab', exact: true }).click();
    assert.equal(frames.filter(frame => frame.method === 'sessions.history_page').length, count);
    assert.equal(page.url(), backgroundOrigin, 'Opening a background session changed the current draft route');
    await input.focus(); await page.keyboard.press('Escape');
    assert.equal(await input.count(), 0);
    await eventually(async () => await page.evaluate(() => document.activeElement?.getAttribute('aria-label') === 'Hide navigation'), { description: 'restore focus to command opener or persistent navigation fallback' });
    await page.getByRole('button', { name: 'Hide navigation', exact: true }).click();
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    assert.equal(await input.count(), 0, 'Consumed search command replayed on remount');
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('debounced flat search, Escape focus restoration, consumed commands, background menu and hover without hydration');

    await saved().evaluate(node => { node.scrollTop = 1400; });
    await page.waitForTimeout(100);
    const anchor = await saved().evaluate(node => {
      const top = node.getBoundingClientRect().top;
      const row = [...node.querySelectorAll('[data-sidebar-session]')].find(row => row.getBoundingClientRect().bottom > top);
      return { id: row.dataset.sidebarSession, y: row.getBoundingClientRect().top - top };
    });
    // Native metadata edits preserve ID ordering. Moving an existing root
    // between directories changes the grouped rows before the reading anchor.
    const moved = await client.session(roots[138]).get(deadline());
    await client.setWorkingDirectory({ id: crypto.randomUUID(), session_id: moved.id, expected_revision: moved.config_revision, path: paths[0] }, deadline());
    await page.waitForTimeout(2500);
    const after = await page.locator(`[data-sidebar-session="${anchor.id}"]`).evaluate(node => node.getBoundingClientRect().top - node.closest('[aria-label="Saved sessions"]').getBoundingClientRect().top);
    assert.ok(Math.abs(after - anchor.y) < 2, `Reading anchor moved: ${anchor.y} -> ${after}`);
    await saved().evaluate(node => { node.scrollTop = node.scrollHeight; });
    await saved().getByRole('button', { name: 'Load more sessions' }).click();
    await eventually(async () => await saved().getByRole('button', { name: 'Load more sessions' }).count() === 0);
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('catalog reorder preserves visible anchor; explicit load-more reaches later pages');

    await page.goto(origin + route(roots[139])); await ready();
    await page.screenshot({ path: join(directory, `${name}-320-light.png`) });
    await page.evaluate(() => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'dark' })));
    await page.reload(); await ready();
    await page.screenshot({ path: join(directory, `${name}-320-dark.png`) });
    await separator().focus(); await page.keyboard.press('End');
    await page.screenshot({ path: join(directory, `${name}-420-dark.png`) });
    if (name === 'chromium') {
      const source = await readFile('packages/ui/src/generated/theme-catalog.ts', 'utf8');
      const ids = [...source.matchAll(/"id":\s*"([^"]+)"/g)].map(match => match[1]);
      for (const id of ids) {
        await page.evaluate(id => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id })), id);
        await page.reload(); await ready();
        assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), id);
        assert.equal((await sidebar().boundingBox()).width, 420);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
        await sidebar().getByRole('button', { name: 'Search sessions', exact: true }).click();
        await eventually(() => input.evaluate(node => node === document.activeElement), { description: 'search dialog receives keyboard focus' });
        await page.keyboard.press('Escape');
      }
      assert.equal(ids.length, 66);
      console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('all 66 themes render sidebar selection, controls, focus and geometry');
    }
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
    const sheet = page.getByRole('dialog', { name: 'WHIP', exact: true });
    assert.ok((await sheet.getByRole('button', { name: 'Search sessions', exact: true }).boundingBox()).height >= 44);
    await sheet.getByRole('button', { name: 'Manage servers', exact: true }).scrollIntoViewIfNeeded();
    const geometry = await sheet.evaluate(node => { const list = node.querySelector('[aria-label="Saved sessions"]'); const host = node.querySelector('[aria-label="Manage servers"]'); return { height: list.clientHeight, hostBottom: host.getBoundingClientRect().bottom, viewport: innerHeight }; });
    assert.ok(geometry.height > 100 && geometry.hostBottom <= geometry.viewport, JSON.stringify(geometry));
    await sheet.getByLabel('Saved sessions', { exact: true }).evaluate(node => { node.scrollTop = 0; });
    await sheet.getByRole('button', { name: 'Search sessions', exact: true }).click();
    const searchDialog = page.getByRole('dialog', { name: 'Search sessions', exact: true });
    // The same root appears in the recent results before the 200ms debounce.
    // A locator matching its label alone can click across the query replacement.
    await searchDialog.getByRole('link', { name: /Session 139/ }).waitFor();
    armFiltered = true;
    const searchStart = catalogReads.length;
    await input.fill('Session 139');
    await eventually(() => releaseSearch, { description: 'actual filtered catalog reply is held' });
    assert.equal(await searchDialog.getByRole('link', { name: /Session 139/ }).count(), 0, 'A recent match is not the pending filtered result');
    assert.equal(await searchDialog.getByLabel('Session search results').getAttribute('aria-busy'), 'true');
    releaseSearch();
    await eventually(() => catalogReads.slice(searchStart).some(read => read.search === 'Session 139' && read.replied && !read.error && read.roots?.includes(roots[139])), { description: 'exact filtered native search reply received' });
    await eventually(async () => await searchDialog.getByLabel('Session search results').getAttribute('aria-busy') === 'false', { description: 'filtered search rows committed' });
    await searchDialog.getByRole('link', { name: /Session 139/ }).waitFor();
    await page.evaluate(() => {
      window.sidebarSelectionClicks = [];
      document.addEventListener('click', event => {
        if (window.sidebarSelectionClicks.length < 16) window.sidebarSelectionClicks.push({ button: event.button, modifiers: [event.metaKey, event.ctrlKey, event.shiftKey, event.altKey], prevented: event.defaultPrevented, link: event.target.closest('a')?.getAttribute('href') });
      }, true);
    });
    assert.ok((await searchDialog.getByRole('link', { name: /Session 139/ }).boundingBox()).height >= 44);
    await page.screenshot({ path: join(directory, `${name}-mobile.png`) });
    await searchDialog.getByRole('link', { name: /Session 139/ }).click(); await ready();
    await eventually(async () => await searchDialog.count() === 0 && await sheet.count() === 0, { description: 'selecting the filtered root closes search and navigation' });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    console.log(`${name}: completed workflow ${checks.length + 1}`);
    checks.push('mobile Sheet, 44px controls, search/selection, close and no document overflow');
    assert.deepEqual(errors, []);
    results[name] = { checks, screenshots: directory };
    console.log(`${name}: ${checks.length} sidebar workflows passed`);
  } catch (error) {
    await page?.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
    await writeFile(join(directory, `${name}-failure.txt`), `${error.stack}\n\n${page ? await page.locator('body').innerText().catch(() => '') : ''}\n\n${JSON.stringify({ errors, catalogReads, filteredReply, clicks: await page?.evaluate(() => window.sidebarSelectionClicks).catch(() => undefined) })}`);
    throw error;
  } finally {
    proxyClosed = true; releaseSearch = undefined; for (const retire of [...proxyConnections]) retire();
    try { await Promise.all(proxyClosing); await browser?.close(); } finally { await fixture.close(); }
  }
}
await writeFile(join(directory, 'sidebar.json'), JSON.stringify(results, null, 2));
