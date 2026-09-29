// Browser acceptance against the production runtime, local synthetic provider,
// real native engine and executor. No legacy testing package or installed host.
import assert from 'node:assert/strict';
import { chromium, expect } from '@playwright/test';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { fileURLToPath } from 'node:url';
import { startExample } from './serve.mjs';
import { deadline, eventually, startFixture } from '../../apps/web/scripts/native-fixture.mjs';
const server = await startExample(0);
const fixture = await startFixture({ allowedOrigins: [server.url], executeCode: true, lifetimeMs: 900000 });
const browser = await chromium.launch({ headless: true });
const errors = []; let expectedDisconnect = false;
try {
  const client = await fixture.connect('client-example-acceptance');
  const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } });
  page.on('pageerror', error => errors.push(String(error)));
  page.on('console', entry => { if (entry.type() === 'error' && !(expectedDisconnect && /WebSocket|ERR_CONNECTION|ERR_INTERNET|Failed to fetch/.test(entry.text()))) errors.push(entry.text()); });
  let dropNext = false, droppedRequestID;
  await page.routeWebSocket('**', route => {
    const server = route.connectToServer(); let droppedID;
    route.onMessage(data => { const request = JSON.parse(String(data)); if (dropNext && request.method === 'sessions.submit') { dropNext = false; droppedID = request.id; droppedRequestID = request.params.identity.request_id; } server.send(data); });
    server.onMessage(data => { const response = JSON.parse(String(data)); if (droppedID !== undefined && response.id === droppedID) { route.close(); server.close(); } else route.send(data); });
  });
  const connect = async () => { await page.getByLabel('Host', { exact: true }).fill(fixture.info.web); await page.getByRole('button', { name: 'Connect', exact: true }).click(); await expect(page.locator('aside [role=status]')).toHaveText('connected'); };
  const composer = page.getByLabel('Message the root agent');
  const send = async text => { await composer.fill(text); await page.getByRole('button', { name: 'Send', exact: true }).click(); };
  const selectedRoot = async () => {
    const records = await client.trees.list({ limit: 100 }, deadline());
    const rootID = await page.locator('.workspace .row .path').last().textContent();
    assert(records.items.some(item => item.root_id === rootID)); return client.session(rootID);
  };
  await page.goto(server.url); await connect();
  await page.getByLabel('Working directory on host').fill(fixture.directory);
  await page.getByRole('button', { name: 'New session', exact: true }).click();
  await expect(composer).toBeVisible();
  await send('Review the native SDK example for correctness.');
  await expect(page.locator('.composer [role=status]')).toHaveText('succeeded');
  await expect(page.locator('.transcript article pre')).toHaveText(['Review the native SDK example for correctness.', 'Review the native SDK example for correctness.']);
  await expect(composer).toHaveValue('');
  const authoredRoot = await selectedRoot();

  // Scoped bytes, not a textual guess at a content handle.
  await page.locator('input[type=file]').setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('scoped attachment evidence') });
  await page.getByRole('button', { name: /Read attachment/ }).click();
  await expect(page.getByRole('alert')).toContainText('scoped attachment evidence');
  await send('Read the attached notes.');
  await expect(page.locator('.composer [role=status]')).toHaveText('succeeded');
  await page.getByRole('button', { name: 'Read scoped attachment', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('scoped attachment evidence');

  // Root and child use the same selected-session service and view ownership.
  const parent = await authoredRoot.get(deadline());
  const child = await authoredRoot.spawn({ definition: parent.definition, overrides: { automatic_title: false, report_mode: 'inline' }, grant_ids: [], budgets: [{ kind: 'model_calls', limit: '8' }], parts: [{ type: 'text', text: 'A child has its own transcript.' }] }, crypto.randomUUID(), deadline());
  assert(child.session); await client.wait(child.admission.receipt.identity.request_id, deadline());
  await page.getByRole('button', { name: 'Refresh children' }).click();
  await page.getByLabel('Inspect session').selectOption(child.session.id);
  await expect(page.locator('.transcript')).toContainText('A child has its own transcript.');
  await expect(page.getByLabel('Message the selected child')).toBeVisible();
  await page.getByLabel('Inspect session').selectOption(authoredRoot.id);

  // The fixture owns this explicit custom-tool definition and peer. Choosing a
  // definition in the browser never silently binds an executor or grants tools.
  const { root } = await fixture.createRoot(client, { title: 'Live cells' });
  const native = client.session(root.id), policy = await native.permissions.policy(deadline());
  await native.permissions.setMode({ expected_revision: policy.revision, mode: 'automatic' }, crypto.randomUUID(), deadline());
  await page.getByRole('button', { name: 'Live cells', exact: false }).click();
  await send('hold:tool-stream-refresh-example');
  await expect(page.locator('.cell')).toHaveCount(2);
  await expect(page.locator('.cell').filter({ hasText: 'running' }).locator('.output')).toHaveText('first\nsecond\n');
  const runningID = await page.locator('.workspace .row .path').last().textContent(); assert.equal(runningID, root.id);
  await page.reload(); await connect(); await page.getByRole('button', { name: 'Live cells', exact: false }).click();
  await expect(page.locator('.cell')).toHaveCount(2);
  await expect(page.locator('.cell').filter({ hasText: 'running' }).locator('.output')).toHaveText('first\nsecond\n');
  fixture.release('tool-stream-refresh-example');
  await expect(page.locator('.cell').filter({ hasText: 'running' })).toHaveCount(0);
  await expect(page.locator('.cell')).toHaveCount(2); // Committed records replace only provisional stdout.

  // Connectivity only rebuilds observation, preserving the exact runtime pin.
  const draft = 'Keep this unsent draft while reconnecting.';
  await composer.fill(draft); expectedDisconnect = true;
  const effects = await fixture.effects();
  await page.context().setOffline(true); await fixture.crashAndRestart();
  await expect(page.locator('aside [role=status]')).toHaveText('reconnecting');
  await expect(page.getByText('Reconnecting. Your draft stays here; accepted work stays on the host.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeDisabled(); await expect(composer).toHaveValue(draft);
  await page.context().setOffline(false);
  await expect(page.locator('aside [role=status]')).toHaveText('connected', { timeout: 15000 });
  await expect(page.locator('.workspace > .row [role=status]')).toHaveText('live');
  expectedDisconnect = false; await expect(composer).toHaveValue(draft);
  assert.deepEqual(await fixture.effects(), effects, 'reconnection did not regenerate any admitted input');

  // Ask-mode operations show the canonical capability/resource/arguments.
  const reconnected = await fixture.connect('client-example-acceptance'), live = reconnected.session(root.id);
  const freshPolicy = await live.permissions.policy(deadline());
  await live.permissions.setMode({ expected_revision: freshPolicy.revision, mode: 'prompt' }, crypto.randomUUID(), deadline());
  for (const allow of [true, false]) {
    const text = 'permission:' + crypto.randomUUID(); await send(text);
    const permission = page.locator('.question').filter({ has: page.getByRole('heading', { name: 'Permission requested', exact: true }) });
    await expect(permission).toHaveCount(1); await expect(permission).toContainText('files.write');
    await permission.getByRole('button', { name: allow ? 'Allow once' : 'Deny', exact: true }).click();
    await expect(permission).toHaveCount(0); await expect(page.locator('.composer [role=status]')).toHaveText('succeeded');
    const submitted = await eventually(async () => (await live.inputs.page({ state: 'all' }, deadline())).items.find(item => item.text_preview === text));
    const input = await live.inputs.get(submitted.id, deadline()); assert(input.turn_id);
    const operation = (await live.turns.operations(input.turn_id, {}, deadline())).items.find(item => item.capability === 'files.write');
    assert.equal(operation.state, allow ? 'succeeded' : 'denied'); // Provider may finish after a denied cell.
  }

  // Single and batched human questions retain real answers and dismissal.
  const questionCode = 'print(user.ask(questions=[{"question":"Choose a release","options":[{"label":"Stable","recommended":True},{"label":"Preview"}]},{"question":"Name the owner","options":[{"label":"Sam"},{"label":"Team"}]}]))';
  await send(['```starlark', questionCode, '```'].join('\n'));
  const questions = page.locator('form.question');
  await expect(questions).toHaveCount(1);
  await questions.getByLabel(/Stable/).check(); await questions.getByLabel('Your answer').nth(1).fill('Custom owner');
  await questions.getByRole('button', { name: 'Answer', exact: true }).click();
  await expect(questions).toHaveCount(0); await expect(page.locator('.composer [role=status]')).toHaveText('succeeded');
  const answered = (await live.questions.list({}, deadline())).items.find(item => item.request.questions[0].question === 'Choose a release');
  assert.deepEqual(answered.answers, [{ answer: ['Stable'], dismissed: false }, { answer: ['Custom owner'], dismissed: false }]);
  await send(['```starlark', 'print(user.ask(question="Dismiss this choice",options=[{"label":"One"},{"label":"Two"}]))', '```'].join('\n'));
  await expect(questions).toHaveCount(1); await questions.getByRole('button', { name: 'Dismiss', exact: true }).click();
  await expect(questions).toHaveCount(0); await expect(page.locator('.composer [role=status]')).toHaveText('succeeded');

  // Lose only the acknowledgement of one actual mutation, then reload. The
  // journal's retained identity and payload are inspected and retried explicitly.
  dropNext = true;
  await send('Lost acknowledgement retains exact recovery.');
  await expect(page.locator('.composer [role=status]')).toContainText('unknown');
  const effectsAfter = await eventually(async () => { const items = await fixture.effects(); return items.some(item => item === 'Lost acknowledgement retains exact recovery.') ? items : false; });
  await page.reload(); await connect();
  await page.getByText(/Recover submitted commands/).click();
  assert(droppedRequestID);
  const recovery = page.locator('.recovery').filter({ hasText: droppedRequestID }); await expect(recovery).toHaveCount(1);
  await recovery.getByRole('button', { name: 'Check only' }).click(); await expect(recovery).toContainText('Exact request accepted');
  await recovery.getByRole('button', { name: 'Retry exact request' }).click(); await expect(recovery).toContainText('Exact request acknowledged');
  assert.deepEqual(await fixture.effects(), effectsAfter, 'exact retry returned the saved receipt, without another provider call');
  page.once('dialog', dialog => dialog.accept()); await recovery.getByRole('button', { name: 'Forget tracking' }).click(); await expect(recovery).toHaveCount(0);

  for (const endpoint of [fixture.info.socket, fixture.info.web]) {
    const run = spawn(process.execPath, [fileURLToPath(new URL('node.mjs', import.meta.url)), endpoint, fixture.directory, 'Native Node client works.'], { stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env, WHIP_RUNTIME_ID: fixture.info.runtime_id } });
    let output = ''; const capture = chunk => { output += chunk; assert(output.length < 65536); };
    run.stdout.on('data', capture); run.stderr.on('data', capture);
    const timer = setTimeout(() => run.kill('SIGKILL'), 150000);
    try { const [code] = await once(run, 'exit'); assert.equal(code, 0, output); assert.match(output, /Creation recovery:/); assert.match(output, /Submission recovery:/); assert.match(output, /"state": "succeeded"/); }
    finally { clearTimeout(timer); }
  }
  assert.deepEqual(errors, [], 'Unexpected browser errors');
  const screenshot = process.env.WHIP_SDK_EXAMPLE_SCREENSHOT ?? '/tmp/whip-sdk-example-native.png'; await page.screenshot({ path: screenshot, fullPage: true });
  console.log(JSON.stringify({ passed: true, native_node: true, transcript: true, scoped_content: true, child_selection: true, live_cells_reload: true, reconnect_draft: true, permissions: true, human_questions: true, lost_ack_exact_recovery: true, screenshot }, null, 2));
} finally { await browser.close(); await fixture.close(); await server.close(); }
