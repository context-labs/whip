import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdir, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox } from '@playwright/test';
import { startFixture, deadline, eventually } from './native-fixture.mjs';
import { checkAgentDock } from './agent-dock.mjs';

// npm run pack:web && node apps/web/scripts/native-agent-dock.mjs
const directory = process.env.WHIP_AGENT_DOCK_RESULTS ?? '/tmp/whip-native-agent-dock';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
await mkdir(directory, { recursive: true });
const reports = [];
for (const name of names) {
  let fixture, browser, page;
  const report = { name }, errors = [], csp = [], frames = [];
  reports.push(report);
  try {
    fixture = await startFixture({ executeCode: true, lifetimeMs: 600000 });
    const client = await fixture.connect(`agent-dock-${name}`), { root } = await fixture.createRoot(client);
    const session = client.session(root.id), policy = await session.permissions.policy(deadline());
    await session.permissions.setMode({ mode: 'automatic', expected_revision: policy.revision }, randomUUID(), deadline());
    const run = async text => {
      const handle = session.submission([{ type: 'text', text }], randomUUID());
      await handle.send(deadline()); assert.equal((await handle.wait(deadline())).turn.state, 'succeeded');
    };
    for (let index = 0; index < 12; index++) await run(`Reading fixture ${index + 1}. ` + 'Retain this conversation while inspecting delegated work. '.repeat(8));
    await run('Create eight completed child agents.\n```starlark\nfor index in range(8):\n  agents.spawn(prompt="Completed child %d" % index)\n```');
    const children = (await client.call('sessions.list', { tree_id: root.tree_id, limit: 16 }, deadline())).items.filter(value => value.parent_id === root.id);
    assert.equal(children.length, 8);
    for (const child of children) await eventually(async () => {
      const turn = (await client.session(child.id).turns.page({ limit: 1 }, deadline())).items[0];
      assert(!turn || !['failed', 'interrupted', 'cancelled'].includes(turn.state), `Child ${child.id} ended ${turn?.state}`);
      return turn?.state === 'succeeded';
    }, { description: 'actual child completed' });
    browser = await ({ chromium, firefox }[name]).launch();
    page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    page.setDefaultTimeout(15000);
    page.on('pageerror', error => { if (errors.length < 64) errors.push(String(error.stack ?? error).slice(0, 4096)); });
    page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
      try {
        const request = JSON.parse(String(payload));
        assert(frames.length < 25000, 'Agent dock traffic evidence bound exceeded');
        frames.push({ method: request.method, params: { session_id: request.params?.session_id } });
      } catch (error) { if (errors.length < 64) errors.push(error.message); }
    }));
    await page.exposeFunction('__recordDockCSP', directive => { if (csp.length < 64) csp.push(String(directive).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__recordDockCSP(event.violatedDirective).catch(() => {}); }));
    await page.goto(`${fixture.info.web}/h/${client.runtimeID}/s/${root.id}`);
    report.version = browser.version(); report.root = root.id; report.children = children.map(child => child.id);
    report.result = await checkAgentDock({ page, client, root: root.id, child: children[0].id, frames, directory, name });
    assert.deepEqual(errors, []); assert.deepEqual(csp, []); report.passed = true;
    console.log(`${name}: native agent dock, metadata-only roster, actual child split/tab routing, isolated drafts/attachment/anchors and narrow/large type passed`);
  } catch (error) {
    report.error = String(error.stack ?? error); report.errors = errors; report.csp = csp; report.traffic = frames.slice(-80);
    if (page) {
      await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {});
      report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16000);
    }
    throw error;
  } finally {
    try { await browser?.close(); }
    finally {
      try { await fixture?.close(); }
      finally { await writeFile(join(directory, 'results.json'), JSON.stringify(reports, null, 2)); }
    }
  }
}
