// Native response boundary and offline acceptance, using a private loopback runtime.
// Run after npm run pack:web; set WHIP_WEB_BROWSER=firefox for the second browser.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdir, writeFile, readFile } from 'node:fs/promises';
import { chromium, firefox, expect } from '@playwright/test';
import { startFixture, deadline, eventually, repository } from './native-fixture.mjs';
const { defineAgent } = await import(repository + 'packages/sdk/dist/agents.js');
const name = process.env.WHIP_WEB_BROWSER ?? 'chromium';
assert(['chromium', 'firefox'].includes(name));
const directory = process.env.WHIP_UX_FOOTER_RESULTS ?? '/tmp/whip-native-response-history-' + name;
await mkdir(directory, { recursive: true });
const report = {
  browser: name,
  manifest: JSON.parse(await readFile(repository + 'apps/web/renderer-manifest.json', 'utf8')),
  gates: [],
  frames: [],
  errors: [],
};
let fixture,
  browser,
  page,
  phase = 'seed';
const check = (name) => {
  report.gates.push(name);
  console.log(name);
};
try {
  fixture = await startFixture({ executeCode: true, agentResponses: true, lifetimeMs: 600000 });
  const client = await fixture.connect('footer-' + randomUUID()),
    { root } = await fixture.createRoot(client, { title: 'Response history controls' }),
    session = client.session(root.id);
  const run = async (text, wait = true) => {
    const command = session.submission([{ type: 'text', text }], randomUUID());
    await command.send(deadline());
    if (wait) assert.equal((await command.wait(deadline())).turn.state, 'succeeded');
    return command;
  };
  const definition = await client.agents.register(
    defineAgent({ id: 'footer-child', name: 'footer-child', defaults: { automatic_title: false } }),
    deadline(),
  );
  const spawnID = randomUUID();
  await session.spawn(
    {
      definition: definition.ref,
      overrides: { report_mode: 'message' },
      grant_ids: [],
      parts: [{ type: 'text', text: 'Child response for draft navigation.' }],
    },
    spawnID,
    deadline(),
  );
  await client.wait(spawnID, deadline());
  const texts = [
    'First complete response.\n\nSecond paragraph belongs to the same response.',
    'Second response with exact execution context.',
    'Third later response.',
  ];
  for (let i = 0; i < 3; i++)
    await run(
      'Inspect response ' +
        i +
        '.\n```starlark\nprint("output ' +
        i +
        '")\n42\n```\n```final\n' +
        texts[i] +
        '\n```',
    );
  const history = await session.history.page({ direction: 'backward', limit: 100 }, deadline()),
    firstGroup = history.messages[0].group_id,
    firstMessages = history.messages.filter((m) => m.group_id === firstGroup),
    keep = firstMessages.at(-1).sequence;
  const expectedTimestamp = firstMessages.filter((m) => m.role === 'assistant').at(-1).created_at;
  // Firefox's own test preference enables its real asynchronous clipboard API.
  // Neither browser replaces navigator.clipboard or the app's copy implementation.
  browser = await { chromium, firefox }[name].launch(
    name === 'firefox' ? { firefoxUserPrefs: { 'dom.events.testing.asyncClipboard': true } } : {},
  );
  const context = await browser.newContext({
    viewport: { width: 1440, height: 1000 },
    ...(name === 'chromium' ? { permissions: ['clipboard-read', 'clipboard-write'] } : {}),
  });
  context.setDefaultTimeout(15000);
  await context.addInitScript(() =>
    localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: 'claude-code' })),
  );
  page = await context.newPage();
  page.on('pageerror', (e) => report.errors.push(String(e)));
  page.on('websocket', (socket) => {
    const pending = new Map();
    socket.on('framesent', ({ payload }) => {
      const frame = JSON.parse(String(payload));
      if (frame.method) {
        assert(report.frames.length < 10000);
        const row = { method: frame.method, params: frame.params, id: frame.id };
        report.frames.push(row);
        pending.set(frame.id, row);
      }
    });
    socket.on('framereceived', ({ payload }) => {
      const reply = JSON.parse(String(payload)),
        row = pending.get(reply.id);
      if (row && ['sessions.fork', 'sessions.rewind'].includes(row.method)) {
        row.result = reply.result;
        row.error = reply.error;
      }
    });
  });
  const url = (id) => `${fixture.info.web}/h/${client.runtimeID}/s/${id}`;
  const footer = () => page.locator('[data-response-actions]').first();
  const reader = () => page.getByRole('region', { name: 'Conversation', exact: true }).last();
  const clipboard = () => page.evaluate(() => navigator.clipboard.readText());
  const first = async () => {
    await reader().hover({ position: { x: 8, y: 8 } });
    await page.mouse.wheel(0, -700);
    await reader().evaluate(
      () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))),
    );
    await page.locator('[data-response-end]').first().scrollIntoViewIfNeeded();
    await page.locator('[data-response-end]').first().hover();
  };
  const activate = async (button) => {
    await button.focus();
    await button.press('Enter');
  };
  const action = async (name) => {
    await activate(footer().getByRole('button', { name: 'Response history actions', exact: true }));
    await page.getByRole('menuitem', { name, exact: true }).click();
  };
  phase = 'timestamp and copy';
  await page.goto(url(root.id));
  await reader().waitFor();
  await expect(page.locator('[data-response-actions]')).toHaveCount(3);
  await first();
  await expect(footer().locator('time')).toHaveAttribute('datetime', expectedTimestamp);
  await activate(footer().getByRole('button', { name: 'Copy response', exact: true }));
  await eventually(async () => (await clipboard()) === texts[0], { description: 'full response clipboard' });
  check('A10 actual canonical timestamp and full multi-paragraph response copy');
  phase = 'draft preservation';
  const composer = () => page.getByRole('textbox', { name: 'Message WHIP', exact: true }).last(),
    draft = 'Keep this unsent draft and selected caret range.';
  await composer().fill(draft);
  await composer().press('ControlOrMeta+A');
  await composer().press('ArrowLeft');
  for (let i = 0; i < 4; i++) await composer().press('ArrowRight');
  for (let i = 0; i < 6; i++) await composer().press('Shift+ArrowRight');
  const sameDraft = async () => {
    await expect(composer()).toHaveValue(draft);
    assert.deepEqual(await composer().evaluate((e) => [e.selectionStart, e.selectionEnd]), [4, 10]);
  };
  await sameDraft();
  await page.locator('[data-session-info-bar]').getByRole('button', { name: 'REPL', exact: true }).click();
  await page.getByRole('region', { name: 'REPL executions', exact: true }).waitFor();
  await page.locator('[data-session-info-bar]').getByRole('button', { name: 'Chat', exact: true }).click();
  await reader().waitFor();
  await sameDraft();
  await page.getByRole('button', { name: 'Agent: Root', exact: true }).click();
  await page
    .getByRole('dialog', { name: 'Session details', exact: true })
    .getByRole('link', { name: 'footer-child', exact: true })
    .click();
  await page.getByRole('button', { name: 'Agent: footer-child', exact: true }).waitFor();
  await page
    .locator('[data-session-info-bar]')
    .getByRole('button', { name: 'Session actions', exact: true })
    .click();
  await page.getByRole('menuitem', { name: 'Root conversation', exact: true }).click();
  await page.getByRole('button', { name: 'Agent: Root', exact: true }).waitFor();
  await sameDraft();
  check('A10 actual keyboard selection and unsent draft survive Chat/REPL/child navigation');
  phase = 'offline copy';
  await first();
  await context.setOffline(true);
  await eventually(
    async () =>
      (await footer().getByRole('button', { name: 'Response history actions', exact: true }).count()) === 0,
    { description: 'offline actions removed' },
  );
  await expect(reader()).toContainText(texts[0].split('\n')[0]);
  await page.evaluate(() => navigator.clipboard.writeText('fixture clipboard sentinel'));
  await activate(footer().getByRole('button', { name: 'Copy response', exact: true }));
  await eventually(async () => (await clipboard()) === texts[0], { description: 'offline clipboard copy' });
  await expect(footer().locator('time')).toHaveAttribute('datetime', expectedTimestamp);
  check('A10 offline copy and timestamp retained without history mutations');
  await context.setOffline(false);
  await footer().getByRole('button', { name: 'Response history actions', exact: true }).waitFor();
  phase = 'fork';
  await action('Fork from here');
  const forkDialog = page.getByRole('dialog', { name: 'Fork from here?', exact: true });
  await forkDialog.getByRole('button', { name: 'Confirm fork', exact: true }).click();
  const fork = await eventually(
    () => report.frames.find((f) => f.method === 'sessions.fork' && f.result)?.result,
    { description: 'native fork receipt' },
  );
  assert.equal(fork.fork.keep_through, keep);
  assert.equal(fork.fork.expected_history_revision, history.snapshot.revision);
  assert.equal(fork.fork.observed_through, history.snapshot.through_sequence);
  const imported = await client
    .session(fork.fork.root_id)
    .history.page({ direction: 'backward', limit: 100 }, deadline());
  assert.equal(imported.messages.length, firstMessages.length);
  assert(imported.messages.every((m) => m.source && m.turn_id === null));
  assert.deepEqual(
    imported.messages.map((m) => m.parts),
    firstMessages.map((m) => m.parts),
  );
  await expect(reader()).toContainText(texts[0].split('\n')[0]);
  await expect(reader()).not.toContainText(texts[1]);
  await activate(footer().getByRole('button', { name: 'Copy response', exact: true }));
  assert.equal(await clipboard(), texts[0]);
  check('A10 fork keeps whole native group through exact canonical end; imported footer and copy remain');
  phase = 'stale confirmation';
  await page.goto(url(root.id));
  await reader().waitFor();
  await first();
  await action('Rewind to here…');
  const rewindDialog = page.getByRole('dialog', { name: 'Rewind to here?', exact: true });
  await rewindDialog.waitFor();
  await run('Fourth append while history confirmation is open.');
  const afterAppend = await session.history.page({ direction: 'backward', limit: 100 }, deadline());
  await rewindDialog.getByRole('button', { name: 'Confirm rewind', exact: true }).click();
  const stale = await eventually(() => report.frames.find((f) => f.method === 'sessions.rewind' && f.error), {
    description: 'native stale history rejection',
  });
  assert.equal(stale.params.observed_through, history.snapshot.through_sequence);
  assert.equal(stale.error.kind, 'CONFLICT');
  await expect(rewindDialog).toContainText('original request is retained');
  assert.deepEqual(
    (await session.history.page({ direction: 'backward', limit: 100 }, deadline())).messages.map((m) => m.id),
    afterAppend.messages.map((m) => m.id),
  );
  await page.keyboard.press('Escape');
  check('A10 stale confirmation refuses unseen tail and retains later canonical messages');
  phase = 'active rewind guard';
  const held = await run('hold:footer-active', false);
  await eventually(
    async () => (await session.turns.page({ limit: 1 }, deadline())).items[0].state === 'running',
    { description: 'native active turn' },
  );
  await activate(footer().getByRole('button', { name: 'Response history actions', exact: true }));
  await expect(page.getByRole('menuitem', { name: 'Rewind to here…', exact: true })).toHaveAttribute(
    'aria-disabled',
    'true',
  );
  await expect(page.getByRole('menuitem', { name: 'Fork from here', exact: true })).not.toHaveAttribute(
    'aria-disabled',
    'true',
  );
  await page.keyboard.press('Escape');
  fixture.release('footer-active');
  assert.equal((await held.wait(deadline())).turn.state, 'succeeded');
  check('A10 active root work disables rewind while earlier response fork remains available');
  phase = 'rewind';
  await eventually(
    async () => {
      await activate(footer().getByRole('button', { name: 'Response history actions', exact: true }));
      const item = page.getByRole('menuitem', { name: 'Rewind to here…', exact: true });
      const ready = (await item.getAttribute('aria-disabled')) !== 'true';
      await page.keyboard.press('Escape');
      return ready;
    },
    { description: 'root settles before fresh rewind' },
  );
  const beforeRewind = await session.history.page({ direction: 'backward', limit: 100 }, deadline());
  await action('Rewind to here…');
  await rewindDialog.getByRole('button', { name: 'Confirm rewind', exact: true }).click();
  const rewound = await eventually(
    () => report.frames.find((f) => f.method === 'sessions.rewind' && f.result),
    { description: 'native rewind receipt' },
  );
  assert.equal(rewound.params.keep_through, keep);
  assert.equal(rewound.params.observed_through, beforeRewind.snapshot.through_sequence);
  assert.deepEqual(
    (await session.history.page({ direction: 'backward', limit: 100 }, deadline())).messages.map((m) => m.id),
    firstMessages.map((m) => m.id),
  );
  await expect(reader()).not.toContainText(texts[1]);
  await expect(reader()).toContainText(texts[0].split('\n')[0]);
  check('A10 fresh response rewind keeps entire selected group and retires later history');
  assert.deepEqual(report.errors, []);
  await page.screenshot({ path: directory + '/completed.png' });
  report.passed = true;
} catch (error) {
  report.error = String(error.stack ?? error);
  report.failedPhase = phase;
  console.error(report.error);
  if (page) {
    report.body = (await page.locator('body').innerText()).slice(0, 20000);
    report.scroll = await page
      .getByRole('region', { name: 'Conversation', exact: true })
      .last()
      .evaluate((e) => ({ top: e.scrollTop, height: e.scrollHeight, client: e.clientHeight }))
      .catch(() => null);
    await page.screenshot({ path: directory + '/failure.png' }).catch(() => {});
  }
} finally {
  await browser?.close();
  await fixture?.close();
  await writeFile(directory + '/results.json', JSON.stringify(report, null, 2));
}
if (!report.passed) process.exitCode = 1;
