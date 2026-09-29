// A4: real pending operations, exact decisions and transport replacement.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { access, mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { chromium, firefox, expect } from '@playwright/test';
import { deadline, eventually, repository, startFixture } from './native-fixture.mjs';
const directory = process.env.WHIP_PERMISSION_RECOVERY_RESULTS ?? '/tmp/whip-permission-recovery-restoration';
const names = (process.env.WHIP_WEB_BROWSERS ?? 'chromium,firefox').split(',');
assert(names.length > 0 && names.length <= 2 && new Set(names).size === names.length && names.every(name => ['chromium', 'firefox'].includes(name)));
const manifest = JSON.parse(await readFile(join(repository, 'apps/web/renderer-manifest.json'), 'utf8'));
await mkdir(directory, { recursive: true });
const reports = [];
for (const name of names) {
  let fixture, browser, page, drop, lostID, liveSocket;
  const report = { name, rendererDigest: manifest.digest, checks: [] }, frames = [], errors = []; reports.push(report);
  try {
    fixture = await startFixture({ executeCode: true, lifetimeMs: 600000 });
    const client = await fixture.connect(`permission-recovery-${name}`);
    const { root } = await fixture.createRoot(client, { engine: 'quickjs', title: 'Permission recovery' });
    const session = client.session(root.id), request = randomUUID();
    await session.submit([{ type: 'text', text: '```js\nawait Promise.allSettled(["first.txt","second.txt","third.txt"].map(path => files.write({path,content:"Explicit fixture content."})));\n```' }], request, deadline());
    const permissions = await eventually(async () => { const p = await session.permissions.list({ pending_only: true, limit: 3 }, deadline()); return p.items.length === 3 && p.items; });
    const first = await session.operations.get(permissions[0].operation_id, deadline()), second = await session.operations.get(permissions[1].operation_id, deadline()), third = await session.operations.get(permissions[2].operation_id, deadline());
    browser = await ({ chromium, firefox }[name]).launch(); page = await browser.newPage({ viewport: { width: 1280, height: 900 } }); page.setDefaultTimeout(15000);
    page.on('pageerror', error => { if (errors.length < 32) errors.push(error.message); });
    await page.routeWebSocket('**/*', socket => {
      const server = socket.connectToServer();
      liveSocket = () => { void socket.close({ code: 1011, reason: 'Fixture interrupted idle observation' }); void server.close(); };
      socket.onMessage(message => {
        const frame = JSON.parse(String(message)); assert(frames.length < 10000); frames.push(frame);
        if (['permissions.resolve', 'questions.answer'].includes(frame.method) && drop) { const mode = drop; drop = undefined; if (mode === 'before') { void socket.close({ code: 1011, reason: 'Fixture interrupted before publication' }); void server.close(); return; } lostID = frame.id; }
        server.send(message);
      });
      server.onMessage(message => { const frame = JSON.parse(String(message)); if (frame.id === lostID && frame.result) { lostID = undefined; void socket.close({ code: 1011, reason: 'Fixture lost decision acknowledgement' }); void server.close(); return; } socket.send(message); });
    });
    const count = method => frames.filter(frame => frame.method === method).length;
    const card = page.getByRole('region', { name: 'Your approval is needed', exact: true });
    await page.goto(`${fixture.info.web}/h/${client.runtimeID}/s/${root.id}`);
    await expect(card).toContainText('Root agent'); await expect(card.getByRole('status')).toHaveText('2 more waiting');
    await expect(card.getByLabel('Requested operation')).toHaveText(first.arguments.path);
    const before = count('initialize'); drop = 'before'; await card.getByRole('button', { name: 'Deny', exact: true }).click();
    await eventually(async () => count('permissions.resolve') === 1 && count('initialize') > before, { description: 'unpublished denial and replacement peer' });
    await expect(card.getByRole('button', { name: 'Retry same denial', exact: true })).toBeVisible();
    await expect(card.getByRole('button', { name: 'Allow once', exact: true })).toHaveCount(0);
    await card.getByRole('button', { name: 'Check approval state', exact: true }).click();
    assert.equal(count('permissions.resolve'), 1); assert.equal((await session.permissions.list({ pending_only: true }, deadline())).items.length, 3);
    await card.getByRole('button', { name: 'Retry same denial', exact: true }).click();
    await expect(card.getByLabel('Requested operation')).toHaveText(second.arguments.path);
    assert.deepEqual(frames.filter(frame => frame.method === 'permissions.resolve').map(frame => frame.params), [{ operation_id: first.id, approved: false }, { operation_id: first.id, approved: false }]);
    await assert.rejects(access(join(fixture.directory, first.arguments.path)), { code: 'ENOENT' });
    report.checks.push('unpublished denial survives replacement; Check only reads; explicit retry retains exact operation and decision');
    // The pending queue changes after publication, before the browser gets its acknowledgement.
    const beforeLost = count('initialize'); drop = 'after'; await card.getByRole('button', { name: 'Allow once', exact: true }).click();
    await eventually(async () => count('initialize') > beforeLost && (await session.operations.get(second.id, deadline())).state === 'succeeded', { description: 'published approval with lost reply and replacement peer' });
    await expect(card.getByRole('button', { name: 'Check approval state', exact: true })).toBeVisible();
    await expect(card.getByLabel('Requested operation')).toHaveText(second.arguments.path);
    assert.equal(count('permissions.resolve'), 3);
    await card.getByRole('button', { name: 'Check approval state', exact: true }).click();
    await expect(card.getByLabel('Requested operation')).toHaveText(third.arguments.path);
    await card.getByRole('button', { name: 'Deny', exact: true }).click(); await expect(card).toHaveCount(0);
    await client.wait(request, deadline()); assert.equal(count('permissions.resolve'), 4);
    await assert.rejects(access(join(fixture.directory, third.arguments.path)), { code: 'ENOENT' });
    assert.equal(await readFile(join(fixture.directory, second.arguments.path), 'utf8'), 'Explicit fixture content.');
    const grants = await session.grants.list({ limit: 32 }, deadline()); assert.equal(grants.items.filter(grant => grant.operation_id === null && grant.capability === 'files.write').length, 0);
    report.checks.push('lost accepted approval remains pinned until exact read-check; no replay or standing authority');
    // Human-question drafts and exact attempted answers share the same recovery boundary.
    const created = await fixture.createRoot(client, { engine: 'starlark', title: 'Question recovery' });
    const questionsSession = client.session(created.root.id), batchRequest = randomUUID();
    await questionsSession.submit([{ type: 'text', text: 'question:batch' }], batchRequest, deadline());
    const batch = await eventually(async () => (await questionsSession.questions.list({ pending_only: true }, deadline())).items[0]);
    await page.goto(`${fixture.info.web}/h/${client.runtimeID}/s/${created.root.id}`);
    await page.getByRole('radio', { name: /Proceed/ }).click();
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await page.getByRole('checkbox', { name: /Web/ }).click();
    await page.getByLabel('Write your own response').fill('Keep my additional answer');
    const beforeDraft = count('initialize'); liveSocket();
    await eventually(async () => count('initialize') > beforeDraft, { description: 'question draft replacement peer' });
    await expect(page.getByLabel('Write your own response')).toHaveValue('Keep my additional answer');
    await expect(page.getByRole('checkbox', { name: /Web/ })).toHaveAttribute('aria-checked', 'true');
    await expect(page.getByRole('button', { name: 'Back', exact: true })).toBeEnabled();
    await page.getByRole('button', { name: 'Back', exact: true }).click();
    await expect(page.getByRole('radio', { name: /Proceed/ })).toHaveAttribute('aria-checked', 'true');
    assert.equal(count('questions.answer'), 0);
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    await page.getByRole('button', { name: 'Next', exact: true }).click();
    drop = 'before'; await page.getByRole('button', { name: 'Skip', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Retry same response', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Check answer state', exact: true }).click();
    assert.equal(count('questions.answer'), 1);
    assert.equal((await questionsSession.questions.get(batch.operation_id, deadline())).state, 'pending');
    await page.getByRole('button', { name: 'Retry same response', exact: true }).click();
    await expect(page.getByLabel('Write your own response')).toHaveCount(0);
    await client.wait(batchRequest, deadline());
    const answerFrames = frames.filter(frame => frame.method === 'questions.answer');
    assert.equal(answerFrames.length, 2); assert.deepEqual(answerFrames[0].params, answerFrames[1].params);
    assert.deepEqual(answerFrames[0].params, { session_id: created.root.id, operation_id: batch.operation_id, answers: [{ answer: ['Proceed'], dismissed: false }, { answer: ['Web', 'Keep my additional answer'], dismissed: false }, { answer: [], dismissed: true }] });
    report.checks.push('authored batch choices, custom text and page survive idle socket loss; unpublished response retains exact answers for read-check and explicit retry');
    const singleRequest = randomUUID();
    await questionsSession.submit([{ type: 'text', text: 'question:single' }], singleRequest, deadline());
    const single = await eventually(async () => (await questionsSession.questions.list({ pending_only: true }, deadline())).items[0]);
    await page.getByLabel('Write your own response').fill('Exact accepted answer');
    drop = 'after'; await page.getByRole('button', { name: 'Send', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Check answer state', exact: true })).toBeVisible();
    await expect(page.getByLabel('Write your own response')).toHaveValue('Exact accepted answer');
    assert.equal(count('questions.answer'), 3);
    assert.equal((await questionsSession.questions.get(single.operation_id, deadline())).state, 'answered');
    await page.getByRole('button', { name: 'Check answer state', exact: true }).click();
    await expect(page.getByLabel('Write your own response')).toHaveCount(0);
    await client.wait(singleRequest, deadline()); assert.equal(count('questions.answer'), 3);
    report.checks.push('published response remains visible after its reply is lost and the pending list empties; exact read-check retires it without replay');
    assert.deepEqual(errors, []); Object.assign(report, { browser: browser.version(), passed: true });
    await page.screenshot({ path: join(directory, `${name}-completed.png`), fullPage: true });
  } catch (error) { report.error = String(error.stack ?? error); report.errors = errors; if (page) { report.body = (await page.locator('body').innerText()).slice(0,16000); await page.screenshot({ path: join(directory, `${name}-failure.png`), fullPage: true }); } throw error; }
  finally { try { await browser?.close(); } finally { try { await fixture?.close(); } finally { await writeFile(join(directory, 'report.json'), JSON.stringify(reports, null, 2)); } } }
  console.log(`${name}: ${report.checks.length} permission recovery groups passed`);
}
