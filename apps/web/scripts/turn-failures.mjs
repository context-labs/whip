import assert from 'node:assert/strict';
import { createHash, randomUUID } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { defineAgent } from '../../../packages/sdk/dist/agents.js';
import { deadline, eventually, repository, startFixture } from './native-fixture.mjs';

// Production assets, real native children, actual HTTP rejection and durable
// turns. The private provider body is deliberately never an app error payload.
const directory = process.env.WHIP_TURN_FAILURE_RESULTS ?? '/tmp/whip-turn-failure-results';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
await mkdir(directory, { recursive: true });
const manifest = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
const reports = [];
const rejectedText = 'turn-failure-native-rejection';
const privateMarker = 'PRIVATE_SYNTHETIC_PROVIDER_DETAIL';
const rejectionMessage = (privateMarker + ' Invalid prompt.\n').repeat(1000);
for (const name of names) {
  let fixture, browser, page;
  const report = { name, rendererDigest: manifest.digest, checks: [] }, errors = [], csp = [], methods = new Map();
  reports.push(report);
  try {
    fixture = await startFixture({ executeCode: true, rejectInput: rejectedText, rejectionMessage, lifetimeMs: 600000 });
    let client = await fixture.connect(`native-turn-failures-${name}`);
    const { root } = await fixture.createRoot(client, { title: 'Native turn failure inspection' });
    const variants = {}, completionTurns = [];
    const isCompletion = (text, turnID) => text.startsWith(`[Mail mail_completion_${turnID} revision `);
    const settledReports = async turnIDs => eventually(async () => {
      const effects = await fixture.effects();
      if (!turnIDs.every(id => effects.filter(text => isCompletion(text, id)).length === 1)) return false;
      const activity = await client.session(root.id).activity(deadline());
      return activity.active_turn === null && activity.queued_input_count === '0' ? effects : false;
    }, { description: 'exact child-completion reports consumed by idle parent' });
    const submit = async (session, text, expected = 'succeeded') => {
      const id = randomUUID(); await session.submit([{ type: 'text', text }], id, deadline());
      const accepted = await client.wait(id, deadline()); assert.equal(accepted.turn.state, expected); return accepted.turn;
    };
    await submit(client.session(root.id), 'Parent remains healthy while children are inspected.');
    for (const variant of ['empty', 'history', 'long']) {
      const definition = await client.agents.register(defineAgent({ id: `failure-${variant}`, name: 'Architecture researcher' }), deadline());
      const request = randomUUID();
      const spawned = await client.session(root.id).spawn({ definition: definition.ref, overrides: { report_mode: 'notice' }, grant_ids: [],
        budgets: [{ kind: 'model_calls', limit: '10' }], parts: [{ type: 'text', text: variant === 'history' ? '```starlark\nprint("Committed before failure")\n```' : rejectedText }] }, request, deadline());
      assert.ok(spawned.session); assert.equal(spawned.session.parent_id, root.id);
      const child = client.session(spawned.session.id);
      let turn = (await client.wait(request, deadline())).turn;
      if (variant === 'history') {
        assert.equal(turn.state, 'succeeded'); completionTurns.push(turn.id);
        assert.equal((await child.turns.cells(turn.id, {}, deadline())).items.length, 1);
        turn = await submit(child, rejectedText, 'failed');
      }
      assert.equal(turn.state, 'failed'); assert.match(turn.failure, /provider returned HTTP 400/);
      assert.ok(!turn.failure.includes(privateMarker)); assert.ok(Buffer.byteLength(turn.failure) < 256);
      completionTurns.push(turn.id);
      variants[variant] = { id: child.id, turn: turn.id, failure: turn.failure };
    }
    const effectsBefore = await settledReports(completionTurns);
    assert.equal(effectsBefore.filter(text => text === rejectedText).length, 3, 'Each failure must make exactly one real provider attempt');
    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
    page.setDefaultTimeout(15000);
    page.on('pageerror', error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); });
    page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
      try {
        const message = JSON.parse(String(payload));
        if (typeof message.method !== 'string') return;
        assert(methods.size < 128 || methods.has(message.method), 'Turn-failure method evidence bound exceeded');
        methods.set(message.method, (methods.get(message.method) ?? 0) + 1);
      } catch (error) { if (errors.length < 64) errors.push(String(error.message).slice(0, 4096)); }
    }));
    await page.exposeFunction('__turnFailureCSP', directive => { if (csp.length < 64) csp.push(String(directive).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__turnFailureCSP(event.violatedDirective).catch(() => {}); }));
    const origin = fixture.info.web, route = `${origin}/h/${client.runtimeID}/s/${root.id}`;
    for (const [path, file] of Object.entries(manifest.files)) {
      const response = await fetch(`${origin}/${path}`); assert.equal(response.status, 200);
      assert.equal(createHash('sha256').update(new Uint8Array(await response.arrayBuffer())).digest('hex'), file.sha256, `Renderer differs: ${path}`);
    }
    const notice = () => page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true });
    await page.goto(route);
    for (const theme of ['claude-code', 'light', 'dark']) {
      await page.evaluate(theme => localStorage.setItem('whip.appearance.theme.v1', JSON.stringify({ version: 1, id: theme })), theme);
      for (const width of [1280, 390]) {
        await page.setViewportSize({ width, height: 800 });
        for (const [variant, child] of Object.entries(variants)) {
          await page.goto(`${route}?agent=${child.id}&view=repl`);
          await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
          await expect(notice()).toHaveCount(1);
          await expect(notice()).toHaveAttribute('data-error-owner', `${client.runtimeID}:${child.id}:${child.turn}`);
          await expect(notice()).toContainText('Architecture researcher');
          if (variant === 'history') {
            const cells = page.locator('[data-repl-cell]').filter({ visible: true });
            await expect(cells).toHaveCount(1); await expect(cells).toContainText('Committed before failure');
          } else await expect(page.getByText('The last turn failed', { exact: true })).toBeVisible();
          const details = notice().getByRole('button', { name: 'Error details', exact: true });
          await details.focus(); await page.keyboard.press('Enter');
          await expect(notice().locator('pre')).toHaveText(child.failure);
          await expect(notice().locator('pre')).toBeVisible();
          assert.ok(!(await page.locator('body').innerText()).includes(privateMarker), 'Private raw provider error escaped into the renderer');
          assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
          const bounds = await notice().boundingBox();
          assert.ok(bounds && bounds.x >= 0 && bounds.x + bounds.width <= width + 1 && bounds.height <= 321, JSON.stringify(bounds));
          await page.screenshot({ path: join(directory, `${name}-${theme}-${width}-${variant}.png`) });
        }
        report.checks.push(`${theme}, ${width}px: empty/existing cells, sanitized large provider rejection, exact child/turn, keyboard and bounded geometry`);
      }
    }
    await page.goto(route);
    await expect(page.getByLabel('Message WHIP', { exact: true })).toBeVisible();
    await expect(notice()).toHaveCount(0);
    report.checks.push('failed child does not mark the healthy root as failed');
    const empty = variants.empty;
    const childRoute = `${route}?agent=${empty.id}`;
    await page.goto(childRoute); await expect(notice()).toHaveCount(1);
    await page.reload(); await expect(notice()).toHaveAttribute('data-error-owner', `${client.runtimeID}:${empty.id}:${empty.turn}`);
    await fixture.crashAndRestart();
    await page.reload(); await expect(notice()).toHaveAttribute('data-error-owner', `${client.runtimeID}:${empty.id}:${empty.turn}`);
    assert.deepEqual(await fixture.effects(), effectsBefore, 'Inspection or restart replayed provider work');
    await assert.rejects(client.session(empty.id).get(deadline()), error => error.kind === 'IDENTITY');
    client = await fixture.connect(client.clientID);
    assert.equal((await client.session(empty.id).turns.get(empty.turn, deadline())).state, 'failed');
    const later = await submit(client.session(empty.id), 'Follow-up completed.');
    await expect(notice()).toHaveCount(0);
    await page.reload(); await expect(page.getByText('Follow-up completed.', { exact: true }).last()).toBeVisible();
    assert.equal((await client.session(empty.id).turns.get(later.id, deadline())).state, 'succeeded');
    await page.goto(`${route}?agent=${variants.history.id}&view=repl`);
    await expect(notice()).toHaveAttribute('data-error-owner', `${client.runtimeID}:${variants.history.id}:${variants.history.turn}`);
    const effectsAfter = await settledReports([...completionTurns, later.id]);
    assert.deepEqual(effectsAfter.slice(0, effectsBefore.length), effectsBefore);
    const addedEffects = effectsAfter.slice(effectsBefore.length);
    assert.equal(addedEffects.length, 2, 'Only the explicit follow-up and its parent completion report may run');
    assert.equal(addedEffects[0], 'Follow-up completed.'); assert.ok(isCompletion(addedEffects[1], later.id));
    report.checks.push('chat/reload/host restart retain exact durable failure without replay; successful follow-up clears only that session');
    for (const method of ['trees.create', 'sessions.submit', 'sessions.spawn', 'sessions.lifecycle', 'turns.cancel']) assert.equal(methods.get(method) ?? 0, 0, `Viewer dispatched ${method}`);
    assert.deepEqual(errors, []); assert.deepEqual(csp, []);
    Object.assign(report, { browser: browser.version(), root: root.id, variants, providerAttempts: effectsAfter.length,
      privateProviderBodyBytes: Buffer.byteLength(rejectionMessage), methods: Object.fromEntries(methods), errors, csp, passed: true });
    console.log(`${name}: ${report.checks.length} native turn-outcome workflows passed`);
  } catch (error) {
    report.error = String(error.stack ?? error); report.errors = errors; report.csp = csp;
    if (page) {
      await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
      report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16000);
    }
    throw error;
  } finally {
    try { await browser?.close(); }
    finally {
      try { await fixture?.close(); }
      finally { await writeFile(join(directory, 'report.json'), JSON.stringify(reports, null, 2)); }
    }
  }
}
console.log(`Passed ${reports.reduce((count, report) => count + report.checks.length, 0)} native turn-outcome workflows. Artifacts: ${directory}`);
