import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build, preview } from 'vite';
import react from '@vitejs/plugin-react';
import stylex from '@stylexjs/unplugin';
import { chromium, firefox, expect } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { startFixture, deadline, eventually } from './native-fixture.mjs';

// --desktop keeps the isolated Electron app open for Computer acceptance.
// Default: real app workflows in Chromium and Firefox, screenshots and assertion report.
const manual = process.argv.includes('--desktop');
const web = fileURLToPath(new URL('../', import.meta.url));
const results = process.env.WHIP_ERROR_RESULTS ?? '/tmp/whip-error-ownership-results';
const port = Number(process.env.WHIP_ERROR_PORT ?? 4177);
assert(Number.isInteger(port) && port >= 1 && port <= 65535, 'Fixture port must be within 1..65535');
const origin = `http://127.0.0.1:${port}`;
const names = manual ? ['desktop'] : (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(manual || names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
const reports = [];
for (const name of names) {
let fixture, server, browser, page, electron;
const report = { name, checks: [] }, errors = [], csp = [];
reports.push(report);
try {
  await mkdir(resolve(results, name), { recursive: true });
  fixture = await startFixture({ allowedOrigins: [origin], executeCode: true, rejectInput: 'error-ownership-rejected-turn', lifetimeMs: manual ? 30 * 60_000 : 600000 });
  const client = await fixture.connect(`error-ownership-${name}`);
  const { root } = await fixture.createRoot(client, { title: 'Error ownership session' });
  const session = client.session(root.id), completionTurns = [];
  const submit = async (owner, text) => {
    const request = randomUUID(); await owner.submit([{ type: 'text', text }], request, deadline());
    const outcome = await client.wait(request, deadline()); if (owner.id !== root.id) completionTurns.push(outcome.turn.id); return outcome;
  };
  await submit(session, 'Healthy root history.');
  const spawnChild = async text => {
    const request = randomUUID();
    const result = await session.spawn({ overrides: { report_mode: 'notice' }, grant_ids: [], budgets: [{ kind: 'model_calls', limit: '10' }], parts: [{ type: 'text', text }] }, request, deadline());
    const outcome = await client.wait(request, deadline()); completionTurns.push(outcome.turn.id); assert.ok(result.session); return client.session(result.session.id);
  };
  const turn = await spawnChild('error-ownership-rejected-turn');
  const execution = await spawnChild('```starlark\nprint("Committed before failing cell")\n```');
  await submit(execution, '```starlark\nprint(1 // 0)\n```');
  const failed = await turn.activity(deadline()); assert.equal(failed.active_turn, null);
  await eventually(async () => {
    const effects = await fixture.effects(), activity = await session.activity(deadline());
    return completionTurns.every(id => effects.some(text => text.startsWith(`[Mail mail_completion_${id} revision `))) && activity.active_turn === null && activity.queued_input_count === '0';
  }, { description: 'actual child-completion reports consumed before error inspection' });
  const proxy = { '/api': { target: fixture.info.web, ws: true, changeOrigin: true } };
  const policy = (await fetch(fixture.info.web)).headers.get('content-security-policy'); assert.ok(policy);
  const config = {
    configFile: false, root: resolve(web, 'scripts/fixtures/error-ownership'), logLevel: 'warn',
    define: { __FIXTURE__: JSON.stringify({ root_id: root.id, runtime_id: client.runtimeID, turn_agent: turn.id, execution_agent: execution.id }) },
    plugins: [stylex.vite({ useCSSLayers: { before: ['whip-reset'] }, runtimeInjection: false,
      unstable_moduleResolution: { type: 'commonJS', rootDir: resolve(web, '../..') } }), react()],
    build: { outDir: resolve(results, name, 'dist'), emptyOutDir: true, assetsInlineLimit: 0 },
    preview: { host: '127.0.0.1', port, strictPort: true, proxy, headers: { 'Content-Security-Policy': policy } },
  };
  await build(config); server = await preview(config);
  await writeFile(resolve(results, name, 'fixture.json'), JSON.stringify({ origin, runtime_id: client.runtimeID, process_epoch: client.processEpoch, root: root.id, turn: turn.id, execution: execution.id }, null, 2));
  if (manual) {
    electron = spawn(resolve(web, '../../node_modules/electron/dist/Electron.app/Contents/MacOS/Electron'),
      [resolve(web, 'scripts/fixtures/error-ownership/electron.cjs')], {
        env: { ...process.env, WHIP_ERROR_ORIGIN: origin, WHIP_ERROR_ELECTRON_DATA: resolve(results, 'electron-profile') },
        stdio: 'inherit',
      });
    console.log(`ERROR OWNERSHIP DESKTOP READY: ${origin}\nArtifacts: ${results}\nUse Error scenario in the separate fixture toolbar. Close Electron to stop.`);
    void fixture.exited.then(() => { if (electron?.exitCode === null && electron.signalCode === null) electron.kill('SIGTERM'); });
    await new Promise((resolve, reject) => { electron.once('exit', resolve); electron.once('error', reject); });
  } else {
    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
    page.setDefaultTimeout(15000);
    page.on('pageerror', error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); });
    await page.exposeFunction('__errorCSP', value => { if (csp.length < 64) csp.push(String(value).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__errorCSP(event.violatedDirective).catch(() => {}); }));
    const notice = type => page.locator(`[data-error-type="${type}"]`).filter({ visible: true });
    const composer = () => page.locator('[data-whip-composer]').filter({ visible: true });
    const capture = async name => {
      await page.evaluate(() => document.fonts.ready);
      await page.screenshot({ path: resolve(results, report.name, `${name}.png`), fullPage: true });
    };
    const scenario = async name => {
      await page.getByRole('combobox', { name: 'Error scenario' }).selectOption(name);
      await expect(page.getByRole('combobox', { name: 'Error scenario' })).toHaveValue(name);
      if (!['session', 'execution'].includes(name)) await expect(composer()).toBeVisible();
    };
    await page.goto(origin);
    await expect(composer()).toBeVisible();

    await scenario('application');
    await composer().fill('Keep this draft through a storage failure.');
    await expect(notice('application')).toHaveCount(1);
    await capture('application');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await expect(notice('application')).toHaveCount(0);
    await expect(composer()).toHaveValue('Keep this draft through a storage failure.');
    await capture('application-restored');
    report.checks.push('Application: failed actual draft storage write; retained draft after restoring storage.');

    await scenario('host');
    await composer().fill('Keep this draft while offline.');
    await page.getByRole('button', { name: 'Disconnect', exact: true }).click();
    await expect(notice('host')).toHaveCount(1);
    await expect(notice('session')).toHaveCount(0);
    await expect(notice('application')).toHaveCount(0);
    await capture('host');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await expect(notice('host')).toHaveCount(0, { timeout: 15000 });
    await expect(composer()).toHaveValue('Keep this draft while offline.');
    await capture('host-restored');
    report.checks.push('Host: one host error, no session/global duplicate, clears on reconnect, draft retained.');

    await scenario('session');
    await expect(notice('session')).toHaveCount(1);
    await expect(notice('host')).toHaveCount(0);
    await expect(page.getByText(/Reconnecting|Connecting/)).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'What would you like to work on?', exact: true })).toHaveCount(0);
    await capture('session');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await notice('session').getByRole('button', { name: 'Refresh' }).click();
    await expect(notice('session')).toHaveCount(0);
    await capture('session-restored');
    report.checks.push('Session: snapshot failure scoped to session without connection or new-session messaging; Refresh recovers with host connected.');

    await scenario('combined');
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(1);
    await page.getByRole('button', { name: 'Disconnect', exact: true }).click();
    await expect(notice('host')).toHaveCount(1);
    await expect(notice('session')).toHaveCount(0);
    await expect(notice('application')).toHaveCount(0);
    await capture('combined');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await expect(notice('host')).toHaveCount(0, { timeout: 15000 });
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(1);
    await capture('combined-restored');
    report.checks.push('Combined: host and recorded turn remain distinct; reconnect clears only host failure.');

    await scenario('execution');
    await expect(notice('execution')).toHaveCount(1);
    await notice('execution').locator('summary').click();
    await expect(notice('execution').locator('pre')).toBeVisible();
    await expect(notice('execution')).toContainText(/division by zero/);
    await expect(page.locator('[data-repl-cell]').filter({ hasText: 'Committed before failing cell' })).toHaveCount(1);
    await capture('execution');
    report.checks.push('Execution: persisted failing cell is visible in real REPL.');

    await scenario('submission');
    await composer().fill('Please retain this rejected message.');
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(notice('submission')).toHaveCount(1);
    await expect(notice('application')).toHaveCount(0);
    await expect(composer()).toHaveValue('Please retain this rejected message.');
    await capture('submission');
    report.checks.push('Submission: rejected input appears beside composer with retained draft and no global duplicate.');

    await eventually(async () => (await session.activity(deadline())).active_turn === null);
    const effectsBeforeUncertain = await fixture.effects();
    await scenario('uncertain');
    await composer().fill('Verify this original identity before retrying.');
    await page.getByRole('button', { name: 'Send message', exact: true }).click();
    await expect(notice('submission')).toHaveCount(1, { timeout: 15000 });
    await capture('submission-uncertain');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await notice('submission').getByRole('button', { name: 'Check status' }).click();
    await expect(notice('submission')).toContainText(/not accepted|not found|not submitted|not received/i);
    await capture('submission-uncertain-restored');
    assert.deepEqual(await fixture.effects(), effectsBeforeUncertain, 'Uncertain delivery inspection replayed a dropped input');
    await expect(composer()).toHaveValue('Verify this original identity before retrying.');
    report.checks.push('Submission uncertainty: lost acknowledgement reconciles original identity without automatic replay.');

    await scenario('resource');
    await page.getByRole('button', { name: 'Model', exact: true }).click();
    await expect(notice('resource')).toHaveCount(1);
    await expect(notice('application')).toHaveCount(0);
    await capture('resource');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await page.getByRole('button', { name: 'Model', exact: true }).click();
    await notice('resource').getByRole('button', { name: 'Retry', exact: true }).click();
    await expect(notice('resource')).toHaveCount(0);
    await capture('resource-restored');
    report.checks.push('Resource: model catalog failure stays inside its picker; Retry recovers after fault removal.');
    await page.keyboard.press('Escape');

    await scenario('action');
    await page.locator(`[data-sidebar-session="${root.id}"]`).getByRole('button', { name: /^Actions for / }).click();
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click();
    const rename = page.getByRole('dialog', { name: 'Rename session' });
    await rename.getByRole('textbox').fill('Name retained on failure');
    await rename.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(notice('action')).toHaveCount(1);
    await expect(notice('application')).toHaveCount(0);
    await expect(rename.getByRole('textbox')).toHaveValue('Name retained on failure');
    await capture('action');
    await page.keyboard.press('Escape');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await page.locator(`[data-sidebar-session="${root.id}"]`).getByRole('button', { name: /^Actions for / }).click();
    await page.getByRole('menuitem', { name: 'Rename', exact: true }).click();
    await rename.getByRole('textbox').fill('Name retained on failure');
    await rename.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(rename).toBeHidden({ timeout: 15000 });
    await capture('action-restored');
    report.checks.push('Action: rename rejection stays in dialog; retry succeeds and preserves name.');

    await scenario('validation');
    await page.getByRole('link', { name: 'Settings', exact: true }).click();
    await page.getByRole('navigation', { name: 'Settings categories' }).getByRole('button', { name: 'Servers', exact: true }).click();
    await page.getByRole('button', { name: 'Add server', exact: true }).click();
    const serverDialog = page.getByRole('dialog', { name: 'Add server', exact: true });
    await serverDialog.getByRole('textbox', { name: 'Server address', exact: true }).fill('file:///not-a-server');
    await serverDialog.getByRole('button', { name: 'Connect', exact: true }).click();
    await expect(notice('validation')).toHaveCount(1);
    await expect(notice('application')).toHaveCount(0);
    await capture('validation');
    report.checks.push('Validation: invalid server address stays inside the form.');
    await page.keyboard.press('Escape');

    await scenario('turn');
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(1);
    await capture('turn');
    await page.getByRole('button', { name: 'Restore', exact: true }).click();
    await expect(page.locator('[data-agent-turn-outcome="failed"]').filter({ visible: true })).toHaveCount(0);
    await capture('turn-restored');
    report.checks.push('Turn: genuine recorded outcome and successful follow-up recovery.');
    assert.deepEqual(errors, []); assert.deepEqual(csp, []); Object.assign(report, { errors, csp, browser: browser.version(), passed: true });
    console.log(`${name}: Passed ${report.checks.length} error ownership workflows. Artifacts: ${results}`);
  }
} catch (error) {
  report.error = String(error.stack ?? error); report.errors = errors; report.csp = csp;
  if (page) { await page.screenshot({ path: resolve(results, name, 'failure.png') }).catch(() => {}); report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16000); }
  throw error;
} finally {
  try { await browser?.close(); }
  finally {
    try {
      if (electron && electron.exitCode === null && electron.signalCode === null) {
        const ended = new Promise(resolve => electron.once('exit', resolve)); electron.kill('SIGTERM');
        const timer = setTimeout(() => electron.kill('SIGKILL'), 5000); try { await ended; } finally { clearTimeout(timer); }
      }
      if (server) await new Promise(resolve => server.httpServer.close(resolve));
    } finally { try { await fixture?.close(); } finally { await writeFile(resolve(results, 'report.json'), JSON.stringify(reports, null, 2)); } }
  }
}
}
