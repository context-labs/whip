import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { createRequire } from 'node:module';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually } from './native-fixture.mjs';
import { startReplFixture } from './native-repl-fixture.mjs';
import { installObservationProbe } from './native-observation-probe.mjs';

const directory = process.env.WHIP_WEB_REPL_RESULTS ?? '/tmp/whip-native-repl-viewer-results';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
await mkdir(directory, { recursive: true });
const reports = [];
const leaves = node => node.type === 'pane' ? [node] : [...leaves(node.first), ...leaves(node.second)];
const allTabs = workspace => leaves(workspace.layout).flatMap(pane => pane.tabs);
const axeSource = await readFile(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
for (const name of names) {
  let seed, fixture, browser, context, page, client, root, rootID, children, baselineEffects;
  const checks = [], errors = [], csp = [], frames = [], currentObservations = new Map(), maxObservations = new Map(), rawOverlaps = [];
  const report = { name, checks, errors, csp, observationDocuments: [], rawOverlaps }; reports.push(report);
  const check = text => { checks.push(text); console.log(`${name}: ${text}`); };
  let frameBytes = 0, connectionID = 0;
  const recordError = error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); };
  const retainObservationEvidence = evidence => {
    const index = report.observationDocuments.findIndex(item => item.documentID === evidence.documentID);
    if ((index < 0 && report.observationDocuments.length >= 32) || Buffer.byteLength(JSON.stringify(evidence)) > (128 << 10))
      recordError(new Error('Observation lifetime evidence overflow'));
    else if (index < 0) report.observationDocuments.push(evidence);
    else report.observationDocuments[index] = evidence;
  };
  const workspace = () => page.evaluate(() => JSON.parse(sessionStorage.getItem('whip.web.workspace.v3')).workspace);
  const panel = id => page.locator(`[data-workspace-view=${JSON.stringify(id)}]`);
  const tab = id => page.locator(`[id=${JSON.stringify('whip-workspace-tab-' + encodeURIComponent(id))}]`);
  const chat = id => panel(id).getByRole('region', { name: 'Conversation', exact: true });
  const notebook = id => panel(id).getByRole('region', { name: 'REPL executions', exact: true });
  const ready = async (id, mode) => {
    await (mode === 'repl' ? notebook(id) : mode === 'trace' ? panel(id).locator('[data-session-view="trace"]') : panel(id).getByRole('textbox', { name: 'Message WHIP', exact: true })).waitFor();
    await expect(page.locator('html')).toHaveAttribute('data-theme', /claude-code|light|dark/);
  };
  const action = async (id, label) => {
    await page.locator(`[data-workspace-tab=${JSON.stringify(id)}]`).getByRole('button', { name: /^Tab actions for / }).click();
    await page.getByRole('menuitem', { name: label, exact: true }).click();
    await page.getByRole('menu').waitFor({ state: 'hidden' });
  };
  const chooseAgent = async (id, label) => {
    await panel(id).getByRole('button', { name: /^Agent:/ }).click();
    const details = page.getByRole('dialog', { name: 'Session details', exact: true });
    await details.getByRole('link', { name: label, exact: true }).click();
    await expect(panel(id).getByRole('button', { name: /^Agent:/ })).toContainText(label === 'Root agent' ? 'Root' : label);
    const owner = label === 'Root agent' ? rootID : children[label].id;
    await eventually(() => ['sessions.history_page', 'sessions.turns', 'sessions.activity'].every(method => frames.some(frame => frame.method === method && frame.params.session_id === owner && frame.settled)), { description: 'selected owner native history, execution and activity initialized' });
    await expect(panel(id).getByRole('button', { name: /^Activity:/ })).toContainText('Idle');
  };
  const anchor = region => region.evaluate(element => {
    const top = element.getBoundingClientRect().top;
    const row = [...element.querySelectorAll('[data-reading-id]')].find(item => item.getBoundingClientRect().bottom > top);
    return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - top } : null;
  });
  const sameAnchor = (region, expected) => eventually(async () => {
    const value = await anchor(region);
    return value?.id === expected?.id && Math.abs(value.offset - expected.offset) < 4;
  }, { description: 'saved exact reading anchor' }).catch(async error => { report.anchorFailure = { expected, actual: await anchor(region) }; throw error; });
  const scrollUp = async region => {
    await eventually(() => region.evaluate(element => element.scrollHeight - element.clientHeight > 700), { description: 'recorded rows fill the native reading window before a scroll gesture' });
    await region.hover({ position: { x: 8, y: 8 } }); await page.mouse.wheel(0, -700);
    let previous, stableSince = performance.now();
    const geometry = await region.evaluate(element => ({ top: element.scrollTop, height: element.scrollHeight, viewport: element.clientHeight }));
    return eventually(async () => {
      const value = await anchor(region);
      const away = await region.evaluate(element => element.scrollHeight - element.scrollTop - element.clientHeight > 64);
      if (!away || !value || value.id !== previous?.id || Math.abs(value.offset - previous.offset) >= 1) stableSince = performance.now();
      previous = value;
      return away && value && performance.now() - stableSince >= 300 ? value : false;
    }, { description: 'settled reading position after user scroll' }).catch(async error => {
      report.scrollFailure = { before: geometry, after: await region.evaluate(element => ({ top: element.scrollTop, height: element.scrollHeight, viewport: element.clientHeight })), anchor: previous }; throw error;
    });
  };
  const latest = async id => {
    const button = panel(id).getByRole('button', { name: /Latest$/ });
    if (await button.count()) await button.click();
    else await notebook(id).evaluate(element => { element.scrollTop = element.scrollHeight; });
    let stableSince = performance.now(), previous;
    await eventually(async () => {
      const geometry = await notebook(id).evaluate(element => ({ top: element.scrollTop, height: element.scrollHeight, viewport: element.clientHeight }));
      const loading = await button.evaluateAll(buttons => buttons.some(button => button.disabled));
      const settled = !loading && Math.abs(geometry.height - geometry.top - geometry.viewport) <= 2;
      const value = JSON.stringify(geometry);
      if (!settled || value !== previous) stableSince = performance.now();
      previous = value; return settled && performance.now() - stableSince >= 300;
    }, { description: 'Latest completes canonical reads and settles at the tail before next user action' });
  };
  const backwardReads = owner => frames.filter(frame => frame.method === 'sessions.history_page' && frame.params.session_id === owner && frame.params.direction === 'backward' && frame.params.cursor !== undefined);
  const executionReads = owner => frames.filter(frame => frame.method === 'sessions.turns' && frame.params.session_id === owner && frame.params.before !== undefined);
  const older = async (id, owner) => {
    const start = frames.length;
    const history = frames.findLast(frame => frame.method === 'sessions.history_page' && frame.params.session_id === owner && frame.settled);
    const turns = frames.findLast(frame => frame.method === 'sessions.turns' && frame.params.session_id === owner && frame.settled);
    const method = history.nextCursor !== null ? 'sessions.history_page' : 'sessions.turns';
    const cursor = method === 'sessions.history_page' ? history.nextCursor : turns.nextCursor;
    assert(cursor, 'An older read requires an exact returned cursor');
    const button = panel(id).getByRole('button', { name: /Load older executions$/ });
    await expect(button).toBeEnabled(); await button.evaluate(button => button.click());
    const request = await eventually(() => frames.slice(start).find(frame => frame.params.session_id === owner && frame.settled &&
      frame.method === method && (method === 'sessions.history_page' ? frame.params.cursor : frame.params.before) === cursor), { description: 'exact owner older read completed' });
    await eventually(() => button.evaluateAll(buttons => buttons.length === 0 || !buttons[0].disabled), { description: 'older read control settled or exhausted' });
    return request;
  };
  const screenshot = async label => { await page.mouse.move(0, 0); await page.screenshot({ path: join(directory, `${name}-${label}.png`) }); };
  try {
    console.log(`${name}: seeding actual REPL executions`);
    seed = await startReplFixture(); ({ fixture, client, children } = seed); rootID = seed.root.id;
    baselineEffects = await fixture.effects();
    browser = await ({ chromium, firefox }[name]).launch();
    context = await browser.newContext({ viewport: { width: 1600, height: 1040 } }); context.setDefaultTimeout(15000);
    page = await context.newPage(); report.version = browser.version();
    page.on('pageerror', recordError);
    await page.exposeFunction('replObservationEvidence', retainObservationEvidence);
    await page.addInitScript(installObservationProbe);
    await context.exposeFunction('replCSP', directive => { if (csp.length < 64) csp.push(String(directive).slice(0, 256)); });
    await context.addInitScript(() => {
      if (!localStorage.getItem('whip.appearance.theme.v1')) localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' }));
      document.addEventListener('securitypolicyviolation', event => { void window.replCSP?.(event.violatedDirective).catch(() => {}); });
    });
    page.on('websocket', socket => {
      const pending = new Map(), connection = ++connectionID;
      let initial = null;
      const settle = (request, reason) => {
        if (request.settled) return; request.settled = true;
        request.settledAt = performance.now(); request.settlement = reason;
        if (request.method === 'sessions.observe') currentObservations.get(request.params.session_id)?.delete(request);
      };
      socket.on('close', () => { for (const request of pending.values()) settle(request, 'close_event'); pending.clear(); });
      socket.on('framesent', ({ payload }) => {
        try {
          assert(Buffer.byteLength(payload) <= (8 << 20)); const value = JSON.parse(String(payload));
          if (!value.method) return;
          const params = value.params ?? {};
          if (value.method === 'initialize') initial = { runtime: params.expected_runtime_id, epoch: params.expected_process_epoch };
          const request = { connection, requestID: value.id, initial, sentAt: performance.now(), method: value.method, params: { session_id: params.session_id, turn_id: params.turn_id, direction: params.direction, cursor: params.cursor, before: params.before, after: params.after, limit: params.limit }, settled: false };
          frameBytes += Buffer.byteLength(JSON.stringify(request)); assert(frames.length < 20000 && frameBytes <= (4 << 20) && pending.size < 8, 'Bounded REPL read evidence overflow');
          frames.push(request); pending.set(value.id, request);
          if (request.method === 'sessions.observe') {
            const owner = request.params.session_id, active = currentObservations.get(owner) ?? new Set();
            active.add(request); currentObservations.set(owner, active);
            maxObservations.set(owner, Math.max(maxObservations.get(owner) ?? 0, active.size));
            if (active.size > 1) { assert(rawOverlaps.length < 32 && active.size <= 16, 'Raw observation overlap evidence overflow'); rawOverlaps.push([...active]); }
          }
        } catch (error) { recordError(error); }
      });
      socket.on('framereceived', ({ payload }) => {
        try { assert(Buffer.byteLength(payload) <= (8 << 20)); const reply = JSON.parse(String(payload)); const request = pending.get(reply.id); if (request) { request.error = reply.error?.kind; request.nextCursor = reply.result?.next_cursor; if (request.method === 'sessions.history_page') request.messageCount = reply.result?.messages?.length; if (request.method === 'sessions.turns') { request.turnIDs = reply.result?.items?.map(item => item.id); frameBytes += Buffer.byteLength(JSON.stringify(request.turnIDs ?? [])); } if (request.method === 'turns.cells') request.cellCount = reply.result?.items?.length; assert(frameBytes <= (4 << 20)); settle(request, 'reply'); pending.delete(reply.id); } }
        catch (error) { recordError(error); }
      });
    });
    const origin = fixture.info.web, runtimeId = client.runtimeID, url = `${origin}/h/${runtimeId}/s/${rootID}`;
    await page.goto(url);
    await page.getByRole('textbox', { name: 'Message WHIP', exact: true }).waitFor();
    root = leaves((await workspace()).layout)[0].selected;
    await ready(root, 'chat');
    await panel(root).getByLabel('Message WHIP', { exact: true }).fill('Keep this draft while I inspect execution evidence.');
    const chatAnchor = await scrollUp(chat(root));
    assert.ok(chatAnchor, 'Chat has no readable anchor');
    const initialHistory = frames.filter(frame => frame.method === 'sessions.history_page').length;
    await page.locator(`[data-workspace-tab="${root}"]`).getByRole('button', { name: /^Tab actions for / }).focus();
    await page.keyboard.press('Enter');
    await page.getByRole('menuitem', { name: 'Open REPL', exact: true }).press('Enter');
    const repl = leaves((await workspace()).layout)[0].selected;
    assert.notEqual(repl, root);
    await ready(repl, 'repl');
    await expect(panel(repl)).toBeFocused();
    assert.equal(new URL(page.url()).searchParams.get('view'), 'repl');
    assert.deepEqual(allTabs(await workspace()).map(tab => tab.id), [root, repl], 'REPL must open immediately right of the preserved chat');
    assert.equal(allTabs(await workspace())[0].id, root, 'Opening REPL replaced the stable view ID');
    assert.equal(await panel(repl).locator('[data-whip-composer]').count(), 0);
    const topFade = await notebook(repl).evaluate(element => getComputedStyle(element).maskImage);
    assert.match(topFade, /^linear-gradient\(/, 'REPL should share the chat top fade');
    assert.match(topFade, /20px/);
    assert.doesNotMatch(topFade, /calc\(/, 'REPL should not fade the bottom edge');
    await page.emulateMedia({ forcedColors: 'active' });
    await expect(notebook(repl)).toHaveCSS('mask-image', 'none');
    await page.emulateMedia({ forcedColors: 'none', media: 'print' });
    await expect(notebook(repl)).toHaveCSS('mask-image', 'none');
    await page.emulateMedia({ media: 'screen' });
    assert.equal(frames.filter(frame => frame.method === 'sessions.history_page').length, initialHistory, 'Mode switch fetched history automatically');
    const savedLink = page.locator('#whip-session-navigation').getByRole('link', { name: 'Inspect Root cell 000.', exact: true });
    assert.equal(new URL(await savedLink.getAttribute('href'), origin).searchParams.get('view'), 'repl');
    await page.getByRole('button', { name: 'Search sessions', exact: true }).click();
    const searchDialog = page.getByRole('dialog', { name: 'Search sessions', exact: true });
    const resultLink = searchDialog.getByRole('link', { name: /^Inspect Root cell 000\./ });
    assert.equal(new URL(await resultLink.getAttribute('href'), origin).searchParams.get('view'), 'repl');
    await page.keyboard.press('Escape'); await searchDialog.waitFor({ state: 'hidden' });
    const lastCell = notebook(repl).locator('[data-repl-cell]').filter({ hasText: 'Root cell 179' });
    await expect(lastCell).toBeVisible();
    await expect(lastCell).toContainText('Completed');
    await expect(lastCell).toContainText(`${seed.rootEvidence.result.steps.toLocaleString('en-US')} steps`);
    await expect(notebook(repl).locator('[data-repl-cell]').filter({ hasText: 'Root cell 176' })).toContainText('Failed');
    await lastCell.getByRole('button', { name: 'Show 5 more lines', exact: true }).click();
    await expect(lastCell.getByRole('button', { name: 'Collapse output', exact: true })).toHaveAttribute('aria-expanded', 'true');
    await expect(lastCell.getByRole('region', { name: 'Return value', exact: true })).toContainText('9');
    assert.deepEqual(seed.rootEvidence.result.value, [0, 1, 4, 9]);
    const replAnchor = await scrollUp(notebook(repl));
    await action(repl, 'Open chat'); await ready(root, 'chat');
    assert.equal(await panel(root).getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft while I inspect execution evidence.');
    await sameAnchor(chat(root), chatAnchor);
    for (const [label, kind] of [['REPL', 'repl'], ['Trace', 'trace'], ['Chat', 'chat']]) {
      await panel(root).getByRole('button', { name: label, exact: true }).click();
      await ready(root, kind);
      assert.equal(leaves((await workspace()).layout)[0].selected, root, 'Top-bar switching must keep the current tab');
      assert.deepEqual(allTabs(await workspace()).map(tab => tab.id), [root, repl]);
      assert.equal(allTabs(await workspace()).find(tab => tab.id === repl).kind, 'repl', 'Other tabs must not change');
    }
    assert.equal(await panel(root).getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft while I inspect execution evidence.');
    await sameAnchor(chat(root), chatAnchor);
    await tab(repl).click(); await ready(repl, 'repl'); await sameAnchor(notebook(repl), replAnchor);
    check('three-dot menu opens an adjacent REPL; nearest chat, draft and independent reading anchors restore');

    await page.goBack(); await ready(root, 'chat');
    await page.goForward(); await ready(repl, 'repl');
    await page.reload(); await ready(repl, 'repl');
    assert.deepEqual(allTabs(await workspace()).map(tab => tab.kind), ['chat', 'repl']);
    const copied = await context.newPage();
    await copied.goto(page.url()); await copied.getByRole('region', { name: 'REPL executions', exact: true }).waitFor();
    assert.equal(await copied.locator('[data-session-view="repl"]').count(), 1);
    await copied.close(); await page.bringToFront();
    await tab(root).click(); await ready(root, 'chat');
    await page.goBack(); await ready(repl, 'repl');
    check('Back/Forward, reload and copied ?view=repl links preserve mode; tab links return to chat');

    const splitAnchor = await scrollUp(notebook(repl));
    await action(repl, 'Split right');
    await eventually(async () => leaves((await workspace()).layout).length === 2, { description: 'REPL split' });
    let state = await workspace();
    const duplicate = leaves(state.layout)[1].selected;
    await ready(duplicate, 'repl');
    await sameAnchor(notebook(duplicate), splitAnchor);
    const rootReadsBefore = frames.filter(frame => frame.method === 'sessions.history_page' && frame.params.session_id === rootID).length;
    const childReadsBefore = frames.filter(frame => frame.method === 'sessions.history_page' && frame.params.session_id === children['repl-child'].id).length;
    await action(repl, 'Open chat'); await ready(root, 'chat');
    await chooseAgent(duplicate, 'repl-child');
    await expect(notebook(duplicate)).toContainText('Child cell 003');
    assert.equal(await panel(root).getByLabel('Message WHIP', { exact: true }).inputValue(), 'Keep this draft while I inspect execution evidence.');
    assert.equal(await panel(root).getByText('Child cell 003', { exact: true }).count(), 0);
    assert.equal(frames.filter(frame => frame.method === 'sessions.history_page' && frame.params.session_id === rootID).length, rootReadsBefore, 'Child REPL rehydrated the unchanged root');
    assert.equal(frames.filter(frame => frame.method === 'sessions.history_page' && frame.params.session_id === children['repl-child'].id).length - childReadsBefore, 1);
    await tab(repl).click(); await ready(repl, 'repl');
    await chooseAgent(repl, 'repl-child');
    await expect(notebook(repl)).toContainText('Child cell 003');
    assert.equal(frames.filter(frame => frame.method === 'sessions.history_page' && frame.params.session_id === children['repl-child'].id).length - childReadsBefore, 1, 'Duplicate child view loaded a second history');
    await chooseAgent(repl, 'Root agent');
    await chooseAgent(duplicate, 'repl-empty');
    await expect(notebook(duplicate)).toContainText('No executions in the loaded history');
    await chooseAgent(duplicate, 'repl-child');
    const failedTurn = await seed.run(children['repl-child'], '```starlark\nprint("child live evidence")\nfiles.read(path="repl-unreadable.bin")\n```');
    const failedOperation = (await children['repl-child'].turns.operations(failedTurn.id, { limit: 100 }, deadline())).items[0];
    assert.equal(failedOperation.state, 'failed'); assert(failedOperation.result.failure);
    const failedCard = notebook(duplicate).locator(`[data-repl-cell=${JSON.stringify(failedOperation.cell_id)}]`);
    await expect(failedCard).toContainText('Failed');
    const operationError = failedCard.locator(`[data-error-owner=${JSON.stringify(failedOperation.id)}]`);
    await operationError.getByText('Error details', { exact: true }).click();
    await expect(operationError).toContainText(failedOperation.result.failure);
    const cellError = failedCard.locator(`[data-error-owner=${JSON.stringify(failedOperation.cell_id)}]`);
    await cellError.getByText('Error details', { exact: true }).click();
    await expect(cellError).toContainText('UTF-8');
    baselineEffects = await fixture.effects();
    assert.equal(await notebook(repl).locator(`[data-error-owner=${JSON.stringify(failedOperation.id)}]`).count(), 0);
    await screenshot('split-root-child');
    await page.setViewportSize({ width: 962, height: 1040 });
    await eventually(async () => (await page.locator('[data-workspace-frame]').count()) === 2, { description: 'two minimum-width panes' });
    await eventually(async () => {
      const bounds = await panel(duplicate).boundingBox();
      return bounds.width >= 320 && bounds.width <= 322;
    }, { description: '320px pane geometry after viewport resize' });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    for (const id of [repl, duplicate]) {
      const bar = panel(id).locator('[data-session-info-bar]');
      const status = await bar.getByRole('button', { name: /^Activity:/ }).boundingBox();
      const menu = await bar.getByRole('button', { name: 'Session actions', exact: true }).boundingBox();
      assert(status.x + status.width <= menu.x, 'Narrow status overlaps session controls');
      assert(await bar.evaluate(element => element.scrollWidth <= element.clientWidth), 'Information bar overflows its pane');
    }
    await screenshot('320-pane');
    await page.setViewportSize({ width: 1600, height: 1040 });
    check('chat/REPL duplicates select independent agents; duplicate child history is leased once; empty and failed child evidence is scoped');

    await action(duplicate, 'Move to pane 1');
    await eventually(async () => leaves((await workspace()).layout).length === 1, { description: 'REPL move prunes empty pane' });
    await chooseAgent(duplicate, 'Root agent');
    await action(duplicate, 'Split right');
    state = await workspace();
    const moved = leaves(state.layout)[1].selected;
    await ready(moved, 'repl');
    const source = await tab(moved).boundingBox();
    const target = await panel(duplicate).boundingBox();
    await page.mouse.move(source.x + Math.min(40, source.width / 2), source.y + source.height / 2);
    await page.mouse.down(); await page.mouse.move(source.x + 48, source.y + source.height / 2);
    await page.mouse.move(target.x + target.width / 2, target.y + target.height / 2, { steps: 16 });
    await page.locator('[data-workspace-drop]').waitFor(); await page.mouse.up();
    await eventually(async () => leaves((await workspace()).layout).length === 1, { description: 'dragged REPL joins target pane' });
    await ready(moved, 'repl');
    assert.equal(allTabs(await workspace()).find(value => value.id === moved).kind, 'repl');
    check('split, move and cross-frame drag preserve REPL mode and view identity');

    for (const theme of ['claude-code', 'light', 'dark']) {
      await page.evaluate(id => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id })), theme);
      await page.reload(); await ready(moved, 'repl');
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      await latest(moved);
      await page.route(`${origin}/__repl-axe.js`, route => route.fulfill({ contentType: 'text/javascript', body: axeSource }));
      await page.addScriptTag({ url: `${origin}/__repl-axe.js` });
      const violations = await page.evaluate(async () => (await axe.run(document.body, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } })).violations.map(value => ({ id: value.id, nodes: value.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) })));
      assert.deepEqual(violations, [], `${theme} accessibility violations`);
      await screenshot(theme);
    }
    check('Claude Code, light and dark screenshots with WCAG 2/2.1 A/AA Axe checks');

    await chooseAgent(moved, 'repl-paged');
    await expect(notebook(moved)).toContainText('Paged cell 079');
    const cursors = []; report.paged = cursors;
    let sawOldestCell = false;
    for (let index = 0; index < 10; index++) {
      if (!await panel(moved).getByRole('button', { name: /Load older executions$/ }).count()) break;
      const request = await older(moved, children['repl-paged'].id);
      assert(!request.error); cursors.push({ method: request.method, params: request.params, nextCursor: request.nextCursor });
      assert((await notebook(moved).locator('[data-repl-cell]').count()) < 40, 'Execution DOM must remain virtualized');
      if (request.method === 'sessions.turns' && request.turnIDs?.includes(seed.turns['repl-paged'][0])) {
        await notebook(moved).evaluate(element => { element.scrollTop = 350; });
        await expect(notebook(moved)).toContainText('Paged cell 000'); sawOldestCell = true;
      }
    }
    await expect(panel(moved).getByRole('button', { name: /Load older executions$/ })).toHaveCount(0);
    assert(sawOldestCell, 'The exact older window exposed the first actual cell');
    assert(seed.turns['repl-paged'].every(turnID => frames.some(frame => frame.method === 'turns.cells' && frame.params.turn_id === turnID && frame.cellCount === 1)), 'All80 actual cells were read through explicit bounded windows');
    assert(cursors.some(read => read.method === 'sessions.history_page'));
    assert(cursors.some(read => read.method === 'sessions.turns'));
    const exhausted = frames.length, finalCursor = executionReads(children['repl-paged'].id).at(-1).params.before;
    await eventually(() => frames.slice(exhausted).filter(frame => frame.method === 'sessions.turns' && frame.params.session_id === children['repl-paged'].id && frame.settled).length >= 2);
    assert(frames.slice(exhausted).filter(frame => frame.method === 'sessions.turns' && frame.params.session_id === children['repl-paged'].id).every(frame => frame.params.before === finalCursor));
    await expect(panel(moved).getByRole('button', { name: /Load older executions$/ })).toHaveCount(0);
    report.paged = cursors;
    check('80 actual child cells page through bounded canonical history and execution windows; exhausted cursor survives repeated reads');

    await chooseAgent(moved, 'repl-sparse');
    await expect(notebook(moved)).toContainText('No executions in the loaded history');
    assert.equal(backwardReads(children['repl-sparse'].id).length, 0, 'A sparse initial window must not scan older history');
    assert.equal(executionReads(children['repl-sparse'].id).length, 0, 'A sparse initial window must not scan older turns');
    let sawSparseCell = false;
    for (let index = 0; index < 10; index++) {
      if (!await panel(moved).getByRole('button', { name: /Load older executions$/ }).count()) break;
      const request = await older(moved, children['repl-sparse'].id);
      if (request.method === 'sessions.turns' && request.turnIDs?.includes(seed.turns['repl-sparse'][0])) {
        await notebook(moved).evaluate(element => { element.scrollTop = 350; });
        await expect(notebook(moved)).toContainText('Sparse cell 000'); sawSparseCell = true;
      }
    }
    assert(sawSparseCell, 'Explicit older windows reach sparse actual cells');
    await expect(panel(moved).getByRole('button', { name: /Load older executions$/ })).toHaveCount(0);
    check('text-only tail stays empty until explicit bounded history/turn reads reach actual earlier cells');

    await chooseAgent(moved, 'Root agent');
    await latest(moved);
    const beforeOlder = await scrollUp(notebook(moved));
    const rootPage = await older(moved, rootID);
    await sameAnchor(notebook(moved), beforeOlder);
    await action(moved, 'Open chat'); await ready(root, 'chat');
    const previousPages = backwardReads(rootID).length;
    await panel(root).getByRole('button', { name: 'Load earlier messages', exact: true }).evaluate(button => button.click());
    const chatPage = await eventually(() => backwardReads(rootID).slice(previousPages).find(frame => frame.settled));
    assert.equal(chatPage.params.cursor, rootPage.nextCursor, 'Chat and REPL share the exact returned history cursor');
    await tab(moved).click(); await ready(moved, 'repl');
    for (let index = 0; index < 4; index++) await older(moved, rootID);
    assert(backwardReads(rootID).reduce((count, frame) => count + (frame.messageCount ?? 0), 0) > 512, 'Explicit root history traversed beyond the retained transcript bound');
    assert(frames.filter(frame => ['sessions.history_page', 'sessions.turns', 'turns.cells', 'turns.operations'].includes(frame.method)).every(frame => frame.params.limit <= 100));
    assert((await notebook(moved).locator('[data-repl-cell]').count()) < 40);
    check('root REPL and Chat share canonical transcript pages/anchors; execution metadata and DOM remain bounded independently');
    assert.deepEqual(await fixture.effects(), baselineEffects, 'All viewer interactions remained read-only');

    await latest(moved);
    const reading = await scrollUp(notebook(moved));
    const liveCommand = seed.session.submission([{ type: 'text', text: 'repl:live' }], randomUUID());
    await liveCommand.send(deadline());
    await panel(moved).getByRole('button', { name: 'Latest', exact: true }).waitFor();
    await sameAnchor(notebook(moved), reading);
    await latest(moved);
    await expect(notebook(moved).getByRole('article', { name: 'Provisional execution', exact: true })).toContainText('Writing · provisional');
    fixture.release('repl-code');
    const output = await eventually(async () => (await seed.session.cells.output(deadline())).preview);
    const live = notebook(moved).locator(`[data-repl-cell=${JSON.stringify(output.cell_id)}]`);
    await expect(live).toContainText('Running'); await expect(live).toContainText('live line 8');
    await expect(live.getByLabel('Host calls').getByText('files.read', { exact: true })).toHaveCount(2);
    await expect(live.getByRole('region', { name: 'Output · last 6 lines (2 earlier)', exact: true })).not.toContainText('live line 1');
    fixture.release('repl-execution'); assert.equal((await liveCommand.wait(deadline())).turn.state, 'succeeded');
    const committed = await seed.latest(seed.session);
    assert.equal(committed.cell.id, output.cell_id);
    await expect(live).toContainText('Completed');
    await expect(live).toContainText(`${committed.result.steps.toLocaleString('en-US')} steps`);
    await live.getByRole('button', { name: 'Show 2 more lines', exact: true }).click();
    await expect(live).toContainText('live line 1'); await expect(live).toContainText('live line 8');
    await expect(live.getByRole('region', { name: 'Return value', exact: true })).toContainText('42');
    await screenshot('live-evidence');
    baselineEffects = await fixture.effects();
    await fixture.crashAndRestart();
    await assert.rejects(seed.session.get(deadline()), error => error.kind === 'IDENTITY');
    assert.deepEqual(await fixture.effects(), baselineEffects, 'Reconnect/restart never replays viewer work');
    client = await fixture.connect(client.clientID);
    const restoredSession = client.session(rootID);
    await seed.run(restoredSession, '```starlark\nprint(repl_saved)\n```');
    const restored = await seed.latest(restoredSession);
    assert.equal(restored.result.output, '42\n'); assert(restored.result.restored.restored.includes('repl_saved'));
    await latest(moved);
    const afterRestart = notebook(moved).locator(`[data-repl-cell=${JSON.stringify(restored.cell.id)}]`);
    await expect(afterRestart).toContainText(`Worker restarted; restored ${restored.result.restored.restored.length} saved names`);
    baselineEffects = await fixture.effects();
    check('actual partial provider call, cumulative stdout tail, distinct host calls, committed result and crash-restored checkpoint preserve exact ownership and reader intent');

    await page.setViewportSize({ width: 390, height: 844 }); await ready(moved, 'repl');
    assert.equal(await page.locator('[data-workspace-frame]').count(), 1);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    await screenshot('mobile');
    const mobileAction = async (id, label) => {
      await page.getByRole('button', { name: /^Open sessions:/ }).click();
      const picker = page.getByRole('dialog', { name: 'Open sessions', exact: true });
      const index = allTabs(await workspace()).findIndex(value => value.id === id);
      await picker.getByRole('button', { name: /^Tab actions for / }).nth(index).click();
      await page.getByRole('menuitem', { name: label, exact: true }).click(); await picker.waitFor({ state: 'hidden' });
    };
    await mobileAction(moved, 'Open chat'); await ready(root, 'chat');
    await expect(panel(root).getByRole('textbox', { name: 'Message WHIP', exact: true })).toHaveValue('Keep this draft while I inspect execution evidence.');
    await mobileAction(root, 'Open REPL'); const mobileRepl = leaves((await workspace()).layout)[0].selected;
    assert.notEqual(mobileRepl, moved); await ready(mobileRepl, 'repl');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
    report.maximumOwnerObservations = Object.fromEntries(maxObservations);
    retainObservationEvidence(await page.evaluate(() => window.__readObservationProbe()));
    assert([...maxObservations.values()].every(count => count <= 1), 'Duplicate views must share the owner observation');
    const reads = new Set(['providers.list', 'providers.presets', 'host.permission_default', 'host.execution_defaults', 'mcp.configuration', 'schedules.list', 'skills.list', 'initialize', 'host.status', 'host.profiles', 'providers.get', 'providers.catalog', 'providers.bundled', 'providers.readiness', 'trees.catalog', 'trees.list', 'trees.get', 'trees.summaries', 'sessions.get', 'sessions.list', 'sessions.activity', 'sessions.history_page', 'sessions.observe', 'sessions.turns', 'inputs.page', 'turns.get', 'turns.cells', 'turns.operations', 'cells.output', 'trace.page', 'questions.list', 'permissions.list', 'permissions.policy', 'tool.schemas', 'host.attention', 'definitions.get']);
    assert.deepEqual([...new Set(frames.map(frame => frame.method))].filter(method => !reads.has(method)), [], 'Viewer issued a non-observation method');
    assert.deepEqual(await fixture.effects(), baselineEffects);
    assert.deepEqual(errors, []); assert.deepEqual(csp, []);
    check('mobile picker opens a new read-only REPL, same-owner observations remain shared, drafts persist, and viewer requests never execute work');
    report.maximumOwnerObservations = Object.fromEntries(maxObservations); report.frames = frames.length; report.evidenceBytes = frameBytes;
    report.passed = true; console.log(`${name}: ${checks.length} native REPL viewer groups passed`);
  } catch (error) {
    report.error = String(error.stack ?? error).slice(0, 16384); report.reads = frames.slice(-80);
    report.maximumOwnerObservations = Object.fromEntries(maxObservations);
    if (page) {
      const evidence = await page.evaluate(() => window.__readObservationProbe?.()).catch(() => null);
      if (evidence) retainObservationEvidence(evidence);
      await screenshot('failure').catch(() => {});
      report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16384);
    }
    throw error;
  } finally {
    try { await browser?.close(); }
    finally { try { await fixture?.close(); } finally { await writeFile(join(directory, 'results.json'), JSON.stringify(reports, null, 2)); } }
  }
}
