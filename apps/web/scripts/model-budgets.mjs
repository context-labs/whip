import assert from 'node:assert/strict';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { startFixture, deadline } from './native-fixture.mjs';

// Canonical ledger reads and production inspector UI on an empty native root.
// No provider invocation, fake snapshot or model-budget mutation is required.
const output = process.env.WHIP_BUDGET_RESULTS ?? '/tmp/whip-model-budget-browser';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
await mkdir(output, { recursive: true });
const reports = [];
let fixture;
try {
  fixture = await startFixture({ lifetimeMs: 600000 });
  const client = await fixture.connect('native-budget-browser');
  const { root } = await fixture.createRoot(client, { title: 'Read-only native usage' });
  const session = client.session(root.id);
  const budgets = await session.budgets.list(deadline());
  for (const kind of ['model_calls', 'model_tokens', 'model_cost_nano_usd', 'model_elapsed_millis']) {
    const state = budgets.items.find(item => item.kind === kind);
    assert.equal(state.session_id, root.id);
    assert.equal(state.limit, null);
    assert.equal(state.used, '0'); assert.equal(state.reserved, '0'); assert.equal(state.uncertain, '0');
    assert.equal(state.incomplete, false);
  }
  assert.equal(budgets.items.find(item => item.kind === 'logical_writes').limit, '100000');
  assert.equal(budgets.items.find(item => item.kind === 'logical_write_bytes').limit, '1073741824');
  const resources = await session.resources.list(deadline());
  assert.equal(resources.items.find(item => item.kind === 'active_operations').limit, '64');
  const usage = await session.usage(deadline());
  assert.equal(usage.attempts.settled, '0'); assert.equal(usage.attempts.in_flight, '0');
  assert.equal(usage.reported_cost.value, '0'); assert.equal(usage.reported_cost.attempts, '0');
  assert.equal(usage.input_tokens.known_attempts, '0');
  const route = `${fixture.info.web}/h/${client.runtimeID}/s/${root.id}?panel=limits`;
  for (const name of names) {
    let browser;
    try {
      browser = await ({ chromium, firefox }[name]).launch();
      for (const colorScheme of ['dark', 'light']) {
        for (const width of [1280, 390]) {
          let context, page;
          const report = { name, colorScheme, width, checks: [] }, errors = [], csp = [];
          const methods = new Map(); let frames = 0, overflow = false;
          reports.push(report);
          try {
            context = await browser.newContext({ viewport: { width, height: 960 }, colorScheme });
            page = await context.newPage(); page.setDefaultTimeout(15000);
            page.on('pageerror', error => { if (errors.length < 64) errors.push(error.message.slice(0, 4096)); });
            page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
              try { const frame = JSON.parse(String(payload)); frames++; if (methods.size < 128 || methods.has(frame.method)) methods.set(frame.method, (methods.get(frame.method) ?? 0) + 1); else overflow = true; } catch {}
            }));
            await page.exposeFunction('__budgetCSP', value => { if (csp.length < 64) csp.push(String(value).slice(0, 256)); });
            await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__budgetCSP(event.violatedDirective).catch(() => {}); }));
            assert.equal((await page.goto(route)).status(), 200);
            const details = page.getByRole('dialog', { name: 'Session details', exact: true });
            const modelBudget = details.locator('section').filter({ has: page.getByRole('heading', { name: 'Selected agent budgets', exact: true }) });
            const wholeTree = details.locator('section').filter({ has: page.getByRole('heading', { name: 'Whole-tree usage', exact: true }) });
            const check = async () => {
              await expect(modelBudget.getByText('No local cap', { exact: true })).toHaveCount(4);
              await expect(modelBudget.getByText('$0.000000000 used · $0.000000000 in flight', { exact: true })).toBeVisible();
              await expect(wholeTree.getByRole('group', { name: 'Provider-reported cost', exact: true })).toContainText('$0.0000 · 0 attempts');
              await expect(wholeTree.getByRole('group', { name: 'Input tokens', exact: true })).toContainText('Not reported');
              await expect(details.getByText('active operations', { exact: true }).locator('..')).toContainText('0 used · Limit 64');
              await expect(details.getByRole('button', { name: 'Set cap', exact: true })).toHaveCount(0);
              await expect(details.getByLabel('Budget agent', { exact: true })).toHaveCount(0);
            };
            await check();
            await modelBudget.scrollIntoViewIfNeeded();
            await page.screenshot({ path: join(output, `${name}-${colorScheme}-${width}.png`) });
            await page.reload(); await check();
            for (const mutation of ['budgets.set', 'resources.set', 'sessions.submit', 'sessions.compact', 'goals.formulate', 'goals.resume', 'tool.call', 'shell.run']) assert.equal(methods.get(mutation) ?? 0, 0, `${mutation} was sent by read-only inspection`);
            assert.deepEqual(errors, []); assert.deepEqual(csp, []); assert(!overflow && frames <= 2048, 'Budget request evidence capacity exceeded');
            report.checks.push('four uncapped model budgets with exact zero used/reserved/uncertain native evidence', 'active operations remains capped at 64', 'whole-tree zero charges identify zero attempts and absent token reports', 'no cap editor or effect admission', 'reload preserves canonical evidence', 'strict CSP');
            Object.assign(report, { passed: true, browser: browser.version(), methods: Object.fromEntries(methods), frames, errors, csp });
            console.log(`${name} ${colorScheme} ${width}px: read-only native usage and reload passed`);
          } catch (error) {
            report.error = String(error.stack ?? error); report.errors = errors; report.csp = csp;
            if (page) { await page.screenshot({ path: join(output, `${name}-${colorScheme}-${width}-failure.png`) }).catch(() => {}); report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16000); }
            throw error;
          } finally { await context?.close(); }
        }
      }
    } finally { await browser?.close(); }
  }
  assert.deepEqual(await session.budgets.list(deadline()), budgets, 'Inspector reads changed budget state');
  assert.deepEqual(await session.usage(deadline()), usage, 'Inspector reads changed attempt accounting');
  assert.deepEqual(await fixture.effects(), [], 'Read-only inspector invoked a model');
} finally {
  try { await fixture?.close(); } finally { await writeFile(join(output, 'report.json'), JSON.stringify(reports, null, 2)); }
}
