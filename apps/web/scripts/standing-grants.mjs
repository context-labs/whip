import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

const directory = process.env.WHIP_GRANT_RESULTS ?? '/tmp/whip-standing-grant-results';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
await mkdir(directory, { recursive: true });
const reports = [];
for (const name of names) {
  let fixture, browser, page;
  const report = { name, checks: [] }, errors = [], csp = []; reports.push(report);
  try {
    fixture = await startFixture({ executeCode: true, lifetimeMs: 300000 });
    const client = await fixture.connect(), { root } = await fixture.createRoot(client, { title: 'Exact standing authority' });
    const session = client.session(root.id), foreign = await fixture.createRoot(client, { title: 'Foreign authority' });
    const submit = (owner, path) => {
      const id = randomUUID();
      return owner.submit([{ type: 'text', text: '```starlark\nfiles.write(path=' + JSON.stringify(path) + ',content="Exact scoped write.")\n```' }], id, deadline()).then(() => id);
    };
    const pending = owner => eventually(async () => (await owner.permissions.list({ pending_only: true }, deadline())).items?.[0]);
    const initial = await submit(session, 'first-denied.txt'), permission = await pending(session);
    const operation = await session.operations.get(permission.operation_id, deadline());
    browser = await ({ chromium, firefox }[name]).launch(); page = await browser.newPage({ viewport: { width: 1280, height: 900 } }); page.setDefaultTimeout(15000);
    page.on('pageerror', error => { if (errors.length < 32) errors.push(String(error.stack ?? error).slice(0, 4096)); });
    await page.exposeFunction('__grantCSP', value => { if (csp.length < 32) csp.push(String(value).slice(0, 256)); });
    await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => { void window.__grantCSP(event.violatedDirective).catch(() => {}); }));
    const route = `${fixture.info.web}/h/${client.runtimeID}/s/${root.id}`;
    const details = page.getByRole('dialog', { name: 'Session details', exact: true });
    const open = async child => {
      await page.goto(route + (child ? `?agent=${child}` : ''));
      await page.locator('[data-session-info-bar]').getByRole('button', { name: 'Session actions', exact: true }).click();
      await page.getByRole('menuitem', { name: 'Session details', exact: true }).click();
      await details.getByRole('combobox', { name: 'Inspector section' }).click();
      await page.getByRole('option', { name: 'Permissions', exact: true }).click();
    };
    await open();
    await details.getByLabel('Exact capability').fill(operation.capability);
    await details.getByLabel('Exact resource').fill(operation.resource);
    await details.getByRole('button', { name: 'Create standing grant', exact: true }).click();
    await expect(details.getByText('Standing grant created.', { exact: true })).toBeVisible();
    const grant = (await client.call('grants.list', { session_id: root.id, limit: 32 }, deadline())).items.find(item => item.capability === operation.capability && item.operation_id === null);
    assert.ok(grant); assert.equal(grant.resource, operation.resource); assert.equal(grant.issuer_id, null);
    assert.equal((await pending(session)).operation_id, operation.id, 'Creating standing authority must not silently approve an existing request');
    await page.keyboard.press('Escape');
    await page.getByRole('region', { name: 'Your approval is needed' }).getByRole('button', { name: 'Deny', exact: true }).click();
    await client.wait(initial, deadline());
    const allowed = await submit(session, 'standing-root.txt'); await client.wait(allowed, deadline());
    assert.equal(await readFile(join(fixture.directory, 'standing-root.txt'), 'utf8'), 'Exact scoped write.');
    assert.equal((await session.permissions.list({ pending_only: true }, deadline())).items.length, 0);
    report.checks.push('explicit exact root grant is effective for later work without deciding an existing approval');
    const foreignRequest = await submit(client.session(foreign.root.id), 'foreign-needs-approval.txt');
    const foreignPermission = await pending(client.session(foreign.root.id));
    await assert.rejects(session.permissions.resolve(foreignPermission.operation_id, true, deadline()), /another session/);
    await client.session(foreign.root.id).permissions.resolve(foreignPermission.operation_id, false, deadline()); await client.wait(foreignRequest, deadline());
    report.checks.push('standing root grant never authorizes a foreign root; wrong-owner decision rejected');
    const spawnID = randomUUID();
    const spawned = await session.spawn({ overrides: { report_mode: 'notice' }, grant_ids: [], budgets: [{ kind: 'model_calls', limit: '10' }], parts: [{ type: 'text', text: 'Child remains separately scoped.' }] }, spawnID, deadline());
    await client.wait(spawnID, deadline()); assert.ok(spawned.session);
    const child = client.session(spawned.session.id);
    await assert.rejects(client.call('grants.create', { id: randomUUID(), session_id: child.id, capability: grant.capability, resource: grant.resource }, deadline()), error => error.kind === 'CONFLICT');
    const other = await client.call('grants.create', { id: randomUUID(), session_id: foreign.root.id, capability: grant.capability, resource: grant.resource }, deadline());
    await assert.rejects(client.call('grants.create', { id: randomUUID(), session_id: child.id, capability: grant.capability, resource: grant.resource, issuer_id: other.id }, deadline()), error => error.kind === 'CONFLICT');
    await open(child.id);
    await expect(details.getByLabel('Exact capability')).toHaveCount(0);
    await details.getByRole('combobox', { name: 'Parent standing grant' }).click();
    await page.getByRole('option', { name: new RegExp(grant.id) }).click();
    await details.getByRole('button', { name: 'Create standing grant', exact: true }).click();
    await expect(details.getByText('Standing grant created.', { exact: true })).toBeVisible();
    const delegated = (await client.call('grants.list', { session_id: child.id, limit: 32 }, deadline())).items.find(item => item.issuer_id === grant.id);
    assert.ok(delegated); assert.equal(delegated.capability, grant.capability); assert.equal(delegated.resource, grant.resource);
    await page.keyboard.press('Escape');
    await client.wait(await submit(child, 'standing-child.txt'), deadline());
    assert.equal(await readFile(join(fixture.directory, 'standing-child.txt'), 'utf8'), 'Exact scoped write.');
    report.checks.push('child can select only an exact direct-parent standing issuer; absent and foreign issuers rejected by host');
    await open();
    await details.locator('article').filter({ hasText: 'files.write' }).getByRole('button', { name: 'Revoke grant' }).click();
    await eventually(async () => (await client.call('grants.list', { session_id: root.id, limit: 32 }, deadline())).items.find(item => item.id === grant.id)?.revoked_at);
    const deniedID = await submit(child, 'revoked-child.txt'); await client.wait(deniedID, deadline());
    await assert.rejects(readFile(join(fixture.directory, 'revoked-child.txt')), { code: 'ENOENT' });
    report.checks.push('revoking the root issuer removes the delegated child authority');
    await page.screenshot({ path: join(directory, `${name}-grants.png`), fullPage: true });
    assert.deepEqual(errors, []); assert.deepEqual(csp, []); Object.assign(report, { browser: browser.version(), root: root.id, grant: grant.id, child: child.id, errors, csp, passed: true });
    console.log(`${name}: ${report.checks.length} real standing-grant workflows passed`);
  } catch (error) {
    report.error = String(error.stack ?? error); report.errors = errors; report.csp = csp;
    if (page) { await page.screenshot({ path: join(directory, `${name}-failure.png`) }).catch(() => {}); report.body = (await page.locator('body').innerText().catch(() => '')).slice(0, 16000); }
    throw error;
  } finally {
    try { await browser?.close(); } finally { try { await fixture?.close(); } finally { await writeFile(join(directory, 'report.json'), JSON.stringify(reports, null, 2)); } }
  }
}
