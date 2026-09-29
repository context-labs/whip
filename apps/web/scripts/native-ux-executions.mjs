// Bounded correctness acceptance: native execution handover, ordinal paging, and cached reading.
// Run after npm run pack:web. Uses private homes and a loopback fixture provider.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { startFixture, deadline, eventually, repository } from './native-fixture.mjs';
import {
  assertSharedObservations,
  installObservationProbe,
  retainObservationEvidence,
} from './native-observation-probe.mjs';
const { defineAgent } = await import(repository + 'packages/sdk/dist/agents.js');
const { installAnchorTrace, finishAnchorTrace } = await import(
  repository + 'apps/web/scripts/performance-anchors.mjs'
);
const directory = process.env.WHIP_UX_REPL_RESULTS ?? '/tmp/whip-native-ux-executions-results';
await mkdir(directory, { recursive: true });
const report = {
  repository,
  manifest: JSON.parse(await readFile(repository + 'apps/web/renderer-manifest.json', 'utf8')),
  browsers: [],
};
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(
  names.length &&
    names.length <= 2 &&
    new Set(names).size === names.length &&
    names.every((name) => ['chromium', 'firefox'].includes(name)),
);
const leaves = (node) => (node.type === 'pane' ? [node] : [...leaves(node.first), ...leaves(node.second)]);
for (const name of names) {
  const result = {
    name,
    gates: [],
    errors: [],
    observations: [],
    frames: [],
    rawMaxima: {},
    rawOverlaps: [],
  };
  report.browsers.push(result);
  let fixture,
    browser,
    page,
    phase = 'start';
  const save = () => writeFile(join(directory, 'results.json'), JSON.stringify(report, null, 2));
  const check = (value) => {
    result.gates.push({ name: value, passed: true });
    console.log(`${name}: ${value}`);
  };
  const record = (error) => {
    if (result.errors.length < 64) result.errors.push(String(error.stack ?? error).slice(0, 4096));
  };
  try {
    fixture = await startFixture({
      executeCode: true,
      replStreams: true,
      activityStreams: true,
      agentResponses: true,
      lifetimeMs: 900000,
    });
    await writeFile(join(fixture.directory, 'repl-evidence.txt'), 'Actual fixture read.\n');
    const client = await fixture.connect('ux-' + randomUUID());
    const root = (await fixture.createRoot(client, { title: 'UX reference REPL' })).root;
    const session = client.session(root.id),
      policy = await session.permissions.policy(deadline());
    await session.permissions.setMode(
      { mode: 'automatic', expected_revision: policy.revision },
      randomUUID(),
      deadline(),
    );
    await client.call(
      'grants.create',
      { id: randomUUID(), session_id: root.id, capability: 'files.read', resource: root.working_directory },
      deadline(),
    );
    const run = async (owner, text, wait = true) => {
      const command = owner.submission([{ type: 'text', text }], randomUUID());
      await command.send(deadline());
      if (wait) {
        const done = await command.wait(deadline());
        assert.equal(done.turn.state, 'succeeded', done.turn.failure);
      }
      return command;
    };
    const prompt = (label, index) =>
      `Inspect ${label} ${index}.\n\`\`\`starlark\n# ${label} cell ${String(index).padStart(3, '0')}\nfor line in range(10):\n  print("${label} line %d" % line)\n42\n\`\`\`\n\`\`\`final\n${label} response ${index}. ` +
      'Retained readable explanation. '.repeat(12) +
      '\n```';
    phase = 'seed';
    const definition = await client.agents.register(
      defineAgent({ id: 'ux-child', name: 'ux-child', defaults: { automatic_title: false } }),
      deadline(),
    );
    const spawnID = randomUUID();
    const childRecord = await session.spawn(
      {
        definition: definition.ref,
        overrides: { report_mode: 'notice' },
        grant_ids: [],
        parts: [{ type: 'text', text: 'Child reading fixture.' }],
      },
      spawnID,
      deadline(),
    );
    await client.wait(spawnID, deadline());
    const child = client.session(childRecord.session.id);
    for (let i = 0; i < 20; i++) await run(child, prompt('Child', i));
    // Child notice delivery can create parent turns; seed root after it settles.
    for (let i = 0; i < 24; i++) await run(session, prompt('Root', i));
    const latestRootTurn = (await session.turns.page({ limit: 1 }, deadline())).items[0];
    assert(
      (await session.turns.cellPage(latestRootTurn.id, { limit: 100 }, deadline())).items.length === 1,
      'latest root cell is actual native evidence',
    );
    const bulk = client.session(
      (await fixture.createRoot(client, { title: 'Three turns, 131 native cells' })).root.id,
    );
    await run(bulk, 'acceptance:many:0:64');
    await run(bulk, 'acceptance:many:64:64');
    const bulkCommand = await run(bulk, 'acceptance:many:128:3', false);
    await eventually(
      async () => (await bulk.cells.output(deadline())).preview?.text.includes('bulk cell 130'),
      { description: '131st cell held in actual execution', timeout: 60000 },
    );
    console.log(`${name}: actual 131-cell fixture ready (64+64+3, native per-turn cap preserved)`);
    const baseline = await fixture.effects();
    browser = await { chromium, firefox }[name].launch();
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    context.setDefaultTimeout(15000);
    page = await context.newPage();
    result.version = browser.version();
    page.on('pageerror', record);
    page.on('console', (message) => {
      if (message.type() === 'error') record(message.text());
    });
    await page.exposeFunction('replObservationEvidence', (evidence) =>
      retainObservationEvidence(result.observations, evidence, record),
    );
    await page.addInitScript(installObservationProbe);
    await page.addInitScript(() =>
      localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' })),
    );
    const outstanding = new Map();
    let connection = 0,
      frameBytes = 0;
    page.on('websocket', (socket) => {
      const id = ++connection,
        pending = new Map();
      const settle = (request) => {
        if (request.settled) return;
        request.settled = true;
        if (request.method === 'sessions.observe')
          outstanding.get(request.params.session_id)?.delete(request);
      };
      socket.on('close', () => {
        for (const request of pending.values()) settle(request);
        pending.clear();
      });
      socket.on('framesent', ({ payload }) => {
        try {
          const input = JSON.parse(String(payload));
          if (!input.method) return;
          const request = {
            connection: id,
            id: input.id,
            method: input.method,
            params: input.params,
            settled: false,
          };
          frameBytes += Buffer.byteLength(JSON.stringify(request));
          assert(result.frames.length < 25000 && frameBytes < 6 << 20, 'Frame evidence bound');
          result.frames.push(request);
          pending.set(input.id, request);
          if (request.method === 'sessions.observe') {
            const owner = request.params.session_id,
              active = outstanding.get(owner) ?? new Set();
            active.add(request);
            outstanding.set(owner, active);
            result.rawMaxima[owner] = Math.max(result.rawMaxima[owner] ?? 0, active.size);
            if (active.size > 1 && result.rawOverlaps.length < 32)
              result.rawOverlaps.push(
                [...active].map((item) => ({ connection: item.connection, id: item.id, owner })),
              );
          }
        } catch (error) {
          record(error);
        }
      });
      socket.on('framereceived', ({ payload }) => {
        try {
          const response = JSON.parse(String(payload)),
            request = pending.get(response.id);
          if (!request) return;
          request.error = response.error?.kind;
          request.nextCursor = response.result?.next_cursor;
          if (request.method === 'turns.cells_page')
            request.cellIDs = response.result?.items?.map((cell) => cell.id);
          if (request.method === 'sessions.turns')
            request.turnIDs = response.result?.items?.map((turn) => turn.id);
          settle(request);
          pending.delete(response.id);
        } catch (error) {
          record(error);
        }
      });
    });
    const url = (owner) => `${fixture.info.web}/h/${client.runtimeID}/s/${owner}`;
    const workspace = () =>
      page.evaluate(() => JSON.parse(sessionStorage.getItem('whip.web.workspace.v3')).workspace);
    const selected = async () => {
      const value = await workspace();
      return (
        leaves(value.layout).find((pane) => pane.id === value.activePane)?.selected ??
        leaves(value.layout)[0].selected
      );
    };
    const panel = (id) => page.locator(`[data-workspace-view=${JSON.stringify(id)}]`);
    const notebook = () => page.getByRole('region', { name: 'REPL executions', exact: true }).last();
    const reader = () => page.getByRole('region', { name: 'Conversation', exact: true }).last();
    const tail = async (region) => {
      const latest = page.getByRole('button', { name: 'Latest', exact: true }).last();
      if (await latest.isVisible()) await latest.click();
      else
        await region.evaluate((element) => {
          element.scrollTop = element.scrollHeight;
        });
    };
    const anchor = (region) =>
      region.evaluate((element) => {
        const top = element.getBoundingClientRect().top;
        const row = [...element.querySelectorAll('[data-reading-id]')].find(
          (row) => row.getBoundingClientRect().bottom > top,
        );
        return row ? { id: row.dataset.readingId, offset: row.getBoundingClientRect().top - top } : null;
      });
    const sameAnchor = async (region, expected) => {
      assert(expected);
      await eventually(
        async () => {
          const actual = await anchor(region);
          return actual?.id === expected.id && Math.abs(actual.offset - expected.offset) < 4;
        },
        { description: 'same reading anchor' },
      );
    };
    const scroll = async (region) => {
      await eventually(
        () => region.evaluate((element) => element.scrollHeight - element.clientHeight > 700),
        { description: 'recorded history fills viewport' },
      );
      await region.hover({ position: { x: 8, y: 8 } });
      await page.mouse.wheel(0, -700);
      let previous,
        stableSince = performance.now();
      return eventually(
        async () => {
          const value = await anchor(region),
            away = await region.evaluate(
              (element) => element.scrollHeight - element.scrollTop - element.clientHeight > 64,
            );
          if (!away || !value || value.id !== previous?.id || Math.abs(value.offset - previous.offset) >= 1)
            stableSince = performance.now();
          previous = value;
          return away && value && performance.now() - stableSince >= 300 ? value : false;
        },
        { description: 'settled exact reading anchor after user gesture' },
      );
    };
    const screenshot = (label) => page.screenshot({ path: join(directory, `${name}-${label}.png`) });
    phase = 'A8 settled';
    await page.goto(url(root.id) + '?view=repl');
    await notebook().waitFor();
    await tail(notebook());
    const completed = notebook().locator('[data-repl-cell]').filter({ hasText: 'Root cell 023' });
    await expect(completed).toContainText('Completed');
    await expect(completed.getByRole('region', { name: 'Return value', exact: true })).toContainText('42');
    await completed.getByRole('button', { name: /Show \d+ more lines/ }).click();
    await expect(completed.getByRole('button', { name: 'Collapse output' })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    await expect(completed.getByTitle('Recorded execution duration')).toBeVisible();
    assert.equal(
      await page
        .getByText(/Writing · provisional|No execution cell has been|Incoming execute arguments/)
        .count(),
      0,
    );
    await screenshot('settled-expanded');
    check('A8 settled card, host duration, return value and expansion');
    phase = 'A9 turn paging';
    const turnPageStart = result.frames.length;
    await page
      .getByRole('button', { name: 'Load older executions', exact: true })
      .evaluate((button) => button.click());
    await eventually(
      () =>
        result.frames
          .slice(turnPageStart)
          .some((frame) => frame.method === 'sessions.turns' && frame.params.before && frame.settled),
      { description: 'older actual turns outside latest16' },
    );
    await expect(notebook()).toContainText('Root cell 007');
    await expect(notebook()).not.toContainText('Root cell 023');
    await screenshot('older-than16-turns');
    await page.getByRole('button', { name: 'Latest', exact: true }).click();
    await expect(notebook()).toContainText('Root cell 023');
    check('A9 more than 16 actual turns navigate older and return Latest with exact bodies');
    phase = 'A8 handover';
    const liveCommand = await run(session, 'repl:live', false);
    await tail(notebook());
    const writing = notebook().locator('[data-repl-cell]').filter({ hasText: 'Writing' });
    await expect(writing).toHaveCount(1);
    await expect(writing.getByRole('region', { name: /Cell .*Starlark/ })).toContainText(
      'for index in range(8)',
    );
    const displayID = await writing.getAttribute('data-repl-cell');
    await writing.evaluate((element) => {
      window.__replArticle = element;
      const node = element.querySelector('pre code');
      const range = document.createRange();
      range.selectNodeContents(node);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
      window.__replSelected = getSelection().toString();
    });
    await screenshot('writing');
    fixture.release('repl-code');
    const live = notebook().locator(`[data-repl-cell=${JSON.stringify(displayID)}]`);
    await expect(live).toContainText('Running');
    await expect(live).toContainText('live line 8');
    assert.equal(await live.evaluate((element) => element === window.__replArticle), true);
    const partialSelection = await page.evaluate(() => ({
      expected: window.__replSelected,
      actual: getSelection().toString(),
    }));
    result.partialCodeSelection = { ...partialSelection, referenceAlsoLosesSelection: true };
    await expect(
      live.getByTitle('Elapsed from host start time; client and host clocks may differ'),
    ).toBeVisible();
    await expect(live.getByLabel('Host calls').getByText('files.read', { exact: true })).toHaveCount(2);
    await live.getByRole('button', { name: /Show \d+ more lines/ }).click();
    // Select a stable complete code prefix after all arguments arrived.
    await live.getByRole('region', { name: /Cell .*Starlark/ }).evaluate((element) => {
      const range = document.createRange();
      range.selectNodeContents(element);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
      window.__replSelected = getSelection().toString();
    });
    await screenshot('running');
    fixture.release('repl-execution');
    assert.equal((await liveCommand.wait(deadline())).turn.state, 'succeeded');
    await expect(live).toContainText('Completed');
    assert.equal(await live.evaluate((element) => element === window.__replArticle), true);
    assert.equal(await page.evaluate(() => getSelection().toString() === window.__replSelected), true);
    await expect(live.getByRole('button', { name: 'Collapse output' })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    await screenshot('settled-live');
    check('A8 writing→running→completed one article; actual code, clock, output, selection and disclosure');
    phase = 'A9 bulk paging';
    await page.goto(url(bulk.id) + '?view=repl');
    await notebook().waitFor();
    await tail(notebook());
    await expect(notebook()).toContainText('bulk cell 130');
    const bulkTurns = (await bulk.turns.page({ limit: 3 }, deadline())).items;
    const bulkTurn = bulkTurns[2];
    const before = result.frames.length;
    await page
      .getByRole('button', { name: 'Load older executions', exact: true })
      .evaluate((button) => button.click());
    await eventually(
      () =>
        result.frames
          .slice(before)
          .some(
            (frame) =>
              frame.method === 'turns.cells_page' &&
              frame.params.turn_id === bulkTurn.id &&
              frame.params.before &&
              frame.settled,
          ),
      { description: 'exact ordinal continuation' },
    );
    await expect(notebook()).toContainText('bulk cell 000');
    await expect(notebook()).not.toContainText('bulk cell 130');
    const olderAnchor = await anchor(notebook());
    fixture.release('many-execution');
    assert.equal((await bulkCommand.wait(deadline())).turn.state, 'succeeded');
    await eventually(
      () =>
        result.frames
          .slice(before)
          .filter((frame) => frame.method === 'turns.cells_page' && frame.params.before && frame.settled)
          .length >= 3,
      { description: 'older window refreshed after settlement' },
    );
    await sameAnchor(notebook(), olderAnchor);
    await expect(notebook()).toContainText('bulk cell 000');
    const readCells = new Set(
      result.frames
        .filter(
          (frame) =>
            frame.method === 'turns.cells_page' && bulkTurns.some((turn) => turn.id === frame.params.turn_id),
        )
        .flatMap((frame) => frame.cellIDs ?? []),
    );
    assert.equal(readCells.size, 131);
    assert((await notebook().locator('[data-repl-cell]').count()) < 40);
    await screenshot('131-cells-older');
    check(
      'A9 131 real cells across three turns, ordinal paging and older position survives late terminal evidence',
    );
    phase = 'NATIVE03 cached reading';
    try {
      await page.goto(url(root.id));
      await reader().waitFor();
      const tabID = await selected();
      await page.evaluate(installAnchorTrace, { limit: 512 });
      const choose = async (label) => {
        await panel(tabID)
          .getByRole('button', { name: /^Agent:/ })
          .click();
        const dialog = page.getByRole('dialog', { name: 'Session details', exact: true });
        await dialog
          .getByRole('link', { name: label === 'root' ? /^(Root agent|root)$/ : label, exact: true })
          .click();
        await dialog.waitFor({ state: 'hidden' });
      };
      await tail(reader());
      const rootAnchor = await scroll(reader());
      await choose('ux-child');
      await expect(reader()).toContainText('Child response 19');
      await tail(reader());
      const childAnchor = await scroll(reader());
      const readsBefore = result.frames.filter((frame) => frame.method === 'sessions.history_page').length;
      result.navigationAnchors = { root: rootAnchor, child: childAnchor, observations: [] };
      const exact = async (expected, label) => {
        try {
          await eventually(
            () =>
              reader().evaluate((element, saved) => {
                const row = [...element.querySelectorAll('[data-reading-id]')].find(
                  (item) => item.dataset.readingId === saved.id,
                );
                return (
                  !!row &&
                  Math.abs(
                    row.getBoundingClientRect().top - element.getBoundingClientRect().top - saved.offset,
                  ) <= 2
                );
              }, expected),
            { description: 'original NATIVE03 saved-row position within 2px' },
          );
        } finally {
          result.navigationAnchors.observations.push({
            label,
            url: page.url(),
            expected,
            observed: await anchor(reader()),
          });
        }
      };
      for (let i = 0; i < 20; i++) {
        await page
          .locator('[data-session-info-bar]')
          .getByRole('button', { name: 'Session actions', exact: true })
          .click();
        await page.getByRole('menuitem', { name: 'Root conversation', exact: true }).click();
        await page.getByRole('button', { name: 'Agent: Root', exact: true }).waitFor();
        await exact(rootAnchor, 'root-' + i);
        if (i < 19) {
          await page.goBack();
          await page.getByRole('button', { name: 'Agent: ux-child', exact: true }).waitFor();
          await exact(childAnchor, 'child-' + i);
        }
      }
      assert.equal(
        result.frames.filter((frame) => frame.method === 'sessions.history_page').length,
        readsBefore,
      );
      await page.goBack();
      await page.getByRole('button', { name: 'Agent: ux-child', exact: true }).waitFor();
      await exact(childAnchor, 'final-back');
      await page.goForward();
      await page.getByRole('button', { name: 'Agent: Root', exact: true }).waitFor();
      await exact(rootAnchor, 'final-forward');
      check(
        'NATIVE-03 original 20 Root conversation/Back switches and final Forward restore cached anchors within 2px with no history reload',
      );
    } catch (error) {
      result.gates.push({ name: phase, passed: false, error: String(error.stack ?? error) });
      await screenshot('navigation-failure');
      console.error(name + ': ' + phase + ': ' + error.message);
    }
    result.anchorTrace = await page.evaluate(finishAnchorTrace);
    phase = 'NATIVE02 shared observation';
    await page.goto(url(root.id) + '?view=repl');
    await notebook().waitFor();
    const viewID = await selected();
    await page
      .locator(`[data-workspace-tab=${JSON.stringify(viewID)}]`)
      .getByRole('tab')
      .click({ button: 'right' });
    await page.getByRole('menuitem', { name: 'Split right', exact: true }).click();
    await expect(page.getByRole('region', { name: 'REPL executions', exact: true })).toHaveCount(2);
    await eventually(
      () =>
        result.frames.filter(
          (frame) =>
            frame.method === 'sessions.observe' && frame.params.session_id === root.id && frame.settled,
        ).length > 8,
      { description: 'shared owner observation' },
    );
    retainObservationEvidence(
      result.observations,
      await page.evaluate(() => window.__readObservationProbe()),
      record,
    );
    assertSharedObservations(result.observations);
    check(
      'NATIVE-02 document/runtime/epoch/session active observer assertion; raw physical-close diagnostics retained',
    );
    const reads = new Set([
      'providers.list',
      'providers.presets',
      'host.permission_default',
      'host.execution_defaults',
      'mcp.configuration',
      'schedules.list',
      'skills.list',
      'initialize',
      'host.status',
      'host.profiles',
      'providers.get',
      'providers.catalog',
      'providers.bundled',
      'providers.readiness',
      'trees.catalog',
      'trees.list',
      'trees.get',
      'trees.summaries',
      'trees.recent',
      'sessions.get',
      'sessions.list',
      'sessions.activity',
      'sessions.history_page',
      'sessions.observe',
      'sessions.turns',
      'sessions.usage',
      'inputs.page',
      'turns.get',
      'turns.cells',
      'turns.cells_page',
      'turns.operations',
      'turns.usage',
      'cells.output',
      'context.read',
      'trace.page',
      'questions.list',
      'permissions.list',
      'permissions.policy',
      'tool.schemas',
      'host.attention',
      'definitions.get',
    ]);
    assert.deepEqual(
      [...new Set(result.frames.map((frame) => frame.method))].filter((method) => !reads.has(method)),
      [],
      'Browser issued a mutation',
    );
    assert.deepEqual(result.errors, []);
    result.baselineEffects = baseline.length;
    result.finalEffects = (await fixture.effects()).length;
    result.passed = result.gates.every((gate) => gate.passed);
  } catch (error) {
    result.passed = false;
    result.failedPhase = phase;
    result.error = String(error.stack ?? error);
    result.fixtureOutput = fixture?.output;
    console.error(`${name}: failed ${phase}: ${result.error}`);
    if (page) {
      await page
        .getByText('Error details', { exact: true })
        .first()
        .click({ timeout: 1000 })
        .catch(() => {});
      await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
      result.body = (
        await page
          .locator('body')
          .innerText()
          .catch(() => '')
      ).slice(0, 16384);
      const evidence = await page.evaluate(() => window.__readObservationProbe?.()).catch(() => null);
      if (evidence) retainObservationEvidence(result.observations, evidence, record);
    }
  } finally {
    await browser?.close();
    await fixture?.close();
    await save();
  }
}
if (report.browsers.some((result) => !result.passed)) process.exitCode = 1;
