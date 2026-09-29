// Exercise first-run setup in the staged Electron host with isolated state.
// This uses stock Electron, not the signed/fused installed application.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { once } from 'node:events';
import { lstat, mkdir, mkdtemp, readFile, realpath, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import path from 'node:path';
import { promisify } from 'node:util';
import { _electron } from 'playwright';
import { expect } from '@playwright/test';
import { Client } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
import { fileDigest, readRuntimeManifest } from '../src/runtime.ts';
import { verifyFixtureHistory } from './native-fixture.mjs';
import { onboardingStage } from './onboarding-fixture.mjs';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

const exec = promisify(execFile);
const fixture = await mkdtemp('/tmp/whip-onboarding-');
let stage;
let provenance;
const executable = path.join(fixture, 'bin/whipcode');
const output = path.resolve(process.env.WHIP_ONBOARDING_SMOKE_OUTPUT ?? path.join(repositoryRoot, '.ai-docs/plans/provider-onboarding/evidence/desktop.json'));
const artifacts = path.dirname(output);
const env = {
  PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh',
  HOME: path.join(fixture, 'user'), ZDOTDIR: path.join(fixture, 'user'), TMPDIR: path.join(fixture, 'tmp'),
  WHIP_DESKTOP_FIXTURE: '1', WHIPCODE_HOME: path.join(fixture, 'home'), WHIPCODE_NETWORK: '0',
  WHIP_DESKTOP_EXECUTABLE: executable, WHIP_DESKTOP_USER_DATA: path.join(fixture, 'desktop'),
};
for (const directory of [env.HOME, env.TMPDIR, env.WHIP_DESKTOP_USER_DATA, artifacts]) await mkdir(directory, { recursive: true });
let electron;
let client;
const errors = [];
const screenshots = [];
const capture = async (page, name) => {
  const filename = path.join(artifacts, name + '.png');
  await page.screenshot({ path: filename }); screenshots.push(filename);
};
const launch = () => _electron.launch({ args: [path.join(stage, 'app')], env, timeout: 30_000 });
const connect = async status => {
  const value = await Client.connect(unixSocket(status.socket), { clientID: crypto.randomUUID(), expectedRuntimeID: status.process.runtime_id, signal: AbortSignal.timeout(10_000) });
  assert.equal(value.processEpoch, status.process.process_epoch); return value;
};
const status = async () => JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);

async function firstMessageScenario() {
  const model = 'z-ai/glm-5.3';
  const marker = 'Whip onboarding tool fixture';
  const prompt = 'Calculate 6 * 7 with execute and explain the answer.';
  const prefix = 'Verified desktop onboarding: ';
  const answer = prefix + '42';
  const title = 'Verify desktop onboarding';
  const draft = 'Retain this unsent follow-up after reopening.';
  const work = path.join(fixture, 'project');
  const requests = [];
  let providerFailure;
  let workerResult;
  let releaseAnswer;
  const provider = createServer((request, response) => {
    void (async () => {
      assert(requests.length < 16, 'Unexpected provider request loop');
      if (request.headers.authorization === 'Bearer rejected-fixture-key') {
        assert.equal(request.method, 'GET'); assert.equal(request.url, '/key');
        requests.push({ kind: 'rejected-key' }); response.writeHead(401); response.end('{"error":{"message":"invalid fixture key"}}'); return;
      }
      assert.equal(request.headers.authorization, 'Bearer fixture-only-key');
      if (request.method === 'GET' && request.url === '/key') {
        requests.push({ kind: 'validated-key' }); response.writeHead(200, { 'Content-Type': 'application/json' }); response.end('{"data":{"label":"fixture"}}'); return;
      }
      if (request.method === 'GET' && request.url === '/models') {
        requests.push({ kind: 'models' });
        response.writeHead(200, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify({ data: [{ id: model, name: 'Fixture coding model', context_length: 65536,
          top_provider: { max_completion_tokens: 1024 }, supported_parameters: ['tools'], pricing: { prompt: '0', completion: '0' } }] }));
        return;
      }
      assert.equal(request.method, 'POST'); assert.equal(request.url, '/chat/completions');
      let size = 0; const chunks = [];
      for await (const chunk of request) { size += chunk.length; assert(size <= 1 << 20); chunks.push(chunk); }
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
      assert.equal(body.model, model, 'Title, compaction, and turns must stay on the selected OpenRouter model');
      const instruction = body.messages.find(message => message.role === 'system')?.content ?? '';
      const naming = instruction.startsWith('Write a concise title for');
      const compacting = instruction.startsWith('Summarize the conversation data');
      if (naming || compacting) {
        assert(!body.tools?.length);
        requests.push({ kind: naming ? 'title' : 'compaction', model: body.model });
        response.writeHead(200, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify({ choices: [{ message: { role: 'assistant', content: naming ? title : 'The user asked to calculate 6 * 7. The Starlark tool returned 42.' }, finish_reason: 'stop' }],
          usage: { prompt_tokens: 100, completion_tokens: 12 } }));
        return;
      }
      const newestUser = body.messages.findLast(message => message.role === 'user');
      if (JSON.stringify(newestUser?.content).includes('Onboarding compaction context')) {
        requests.push({ kind: 'compaction-source', model: body.model });
        response.writeHead(200, { 'Content-Type': 'text/event-stream' });
        response.end(`data: ${JSON.stringify({ choices: [{ delta: { content: 'Additional fixture context acknowledged.' }, finish_reason: 'stop' }] })}\n\ndata: [DONE]\n\n`);
        return;
      }
      assert.deepEqual(body.tools.map(tool => tool.function.name), ['execute']);
      assert(body.messages.some(message => message.role === 'user' && JSON.stringify(message.content).includes(prompt)));
      const tool = body.messages.find(message => message.role === 'tool' && message.tool_call_id === 'onboarding-tool');
      requests.push({ kind: tool ? 'tool-result' : 'first-turn', model: body.model });
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      if (!tool) {
        assert.equal(requests.filter(entry => entry.kind === 'first-turn').length, 1, 'First prompt must be submitted exactly once');
        response.end(`data: ${JSON.stringify({ choices: [{ delta: { tool_calls: [{ index: 0, id: 'onboarding-tool', type: 'function', function: {
          name: 'execute', arguments: JSON.stringify({ code: `print(${JSON.stringify(marker)})\n{"answer": 6 * 7}` }),
        } }] }, finish_reason: 'tool_calls' }] })}\n\ndata: [DONE]\n\n`);
        return;
      }
      workerResult = JSON.parse(tool.content); const result = workerResult.result;
      assert.equal(result.output, marker + '\n'); assert.deepEqual(result.value, { answer: 42 }); assert.equal(result.format_version, 2); assert.equal(result.execution_engine, 'starlark'); assert(result.metrics.starlark_steps > 0);
      response.write(`data: ${JSON.stringify({ choices: [{ delta: { content: prefix }, finish_reason: null }] })}\n\n`);
      // The UI must observe a partial answer before the final chunk is released.
      await new Promise(resolve => {
        const timeout = setTimeout(resolve, 30_000);
        releaseAnswer = () => { clearTimeout(timeout); resolve(); };
      });
      response.end(`data: ${JSON.stringify({ choices: [{ delta: { content: '42' }, finish_reason: 'stop' }], usage: { prompt_tokens: 1000, completion_tokens: 20 } })}\n\ndata: [DONE]\n\n`);
    })().catch(error => {
      providerFailure ??= error;
      console.error('Fixture provider assertion:', error.message);
      if (!response.headersSent) response.writeHead(500);
      response.end('Fixture provider assertion failed');
    });
  });
  provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
  try {
    client = undefined;
    await electron.evaluate(({ app }) => app.exit(0)).catch(error => { if (!/closed|destroyed/.test(error.message)) throw error; });
    electron = undefined;
    await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
    assert.equal((await status()).state, 'stopped');
    env.WHIP_ONBOARDING_PROVIDER = `http://127.0.0.1:${provider.address().port}`;
    await mkdir(work);
    electron = await launch();
    const page = await electron.firstWindow();
    page.on('pageerror', error => errors.push(error.message));
    const input = page.getByRole('textbox', { name: 'Your first message', exact: true });
    await expect(page.getByRole('region', { name: 'Provider setup' })).toBeVisible({ timeout: 30_000 });
    await expect(input).toHaveCount(0);
    const current = await status();
    client = await connect(current);
    assert.equal((await client.listProviders()).routes.length, 0);
    assert.equal((await client.trees.list()).items.length, 0);
    const started = performance.now();
    await page.getByRole('button', { name: 'Connect OpenRouter', exact: true }).click();
    const connection = page.getByRole('dialog', { name: 'OpenRouter', exact: true });
    await expect(connection.getByLabel('API key', { exact: true })).toHaveAttribute('type', 'password');
    await connection.getByLabel('API key', { exact: true }).fill('rejected-fixture-key');
    await connection.getByRole('button', { name: 'Connect', exact: true }).click();
    await expect(connection.getByRole('alert')).toBeVisible();
    await expect(connection.getByLabel('API key', { exact: true })).toHaveValue('rejected-fixture-key');
    assert.equal(requests.filter(entry => entry.kind === 'rejected-key').length, 1);
    assert.equal((await client.listProviders()).routes.length, 0, 'Rejected discovery must not publish a route');
    await connection.getByLabel('API key', { exact: true }).fill('fixture-only-key');
    await connection.getByRole('button', { name: 'Connect', exact: true }).click();
    await expect(connection).toHaveCount(0);
    await page.getByRole('button', { name: `Use ${model}`, exact: true }).click();
    await expect(input).toBeVisible();
    await expect(input).toBeFocused();
    // First-panel mount starts hidden for measurement; native autoFocus alone cannot focus it.
    for (const source of ['sidebar', 'frontdoor', 'tab-menu']) {
      await page.getByRole('button', { name: /^Close New Chat/ }).click();
      await expect(page.locator('[data-empty-workspace="frontdoor"]')).toBeVisible();
      if (source === 'sidebar') await page.getByRole('link', { name: 'New session', exact: true }).click();
      else if (source === 'frontdoor') await page.locator('[data-empty-workspace="frontdoor"]').getByRole('button', { name: 'New session', exact: true }).click();
      else {
        await page.getByRole('button', { name: 'Pane 1 actions', exact: true }).click();
        await page.getByRole('menuitem', { name: 'New session', exact: true }).click();
      }
      await expect(input).toBeFocused();
    }
    await input.fill(prompt);
    const selected = await client.listProviders();
    assert.equal(selected.defaults.name, model); assert.equal(selected.defaults.provider, 'openrouter');
    const credential = selected.routes.find(route => route.id === 'openrouter').credential;
    assert.equal(credential.state, 'available'); assert.equal(credential.source, 'file');
    assert((await realpath(credential.file)).startsWith(await realpath(fixture) + '/'));
    assert.equal((await lstat(credential.file)).mode & 0o777, 0o600);
    assert.equal((await readFile(credential.file, 'utf8')).trim(), 'fixture-only-key');
    assert.equal(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }).includes('fixture-only-key')), false);
    assert.equal(requests.filter(entry => !['models', 'rejected-key', 'validated-key'].includes(entry.kind)).length, 0, 'Setup must not send a hidden inference request');
    await electron.evaluate(({ dialog }, directory) => { dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [directory] }); }, work);
    await page.getByRole('button', { name: 'Project folder', exact: true }).click();
    await page.getByRole('button', { name: 'Permission approval mode', exact: true }).click();
    await page.getByRole('option', { name: /Full Access/ }).click();
    await capture(page, 'desktop-ready-first-message');
    const submitted = performance.now();
    await page.getByRole('button', { name: 'Send first message', exact: true }).click();
    await expect(page.getByText(prefix.trim(), { exact: true }).last()).toBeVisible({ timeout: 30_000 });
    const firstStreamMs = performance.now() - submitted;
    const rootId = new URL(page.url()).pathname.split('/s/')[1];
    assert(rootId);
    const root = client.session(rootId);
    const owner = await root.get();
    const decision = await client.getAutomaticTitleDecision(owner.tree_id);
    assert.equal(decision.reason, 'eligible'); assert.equal(decision.source, prompt);
    assert.equal((await client.trees.get(owner.tree_id)).metadata.title, prompt);
    // The title decision and fallback commit with admission; its ordinary
    // maintenance helper runs after this same-session turn releases execution.
    await capture(page, 'desktop-first-message-streaming');
    releaseAnswer();
    await expect(page.getByText(answer, { exact: true }).last()).toBeVisible({ timeout: 15_000 });
    const setupToResponseMs = performance.now() - started;
    await expect.poll(async () => (await root.activity()).active_turn).toBeNull();
    const snapshot = await root.get();
    assert.equal(snapshot.configuration.model.name, model); assert.equal(snapshot.configuration.model.provider, 'openrouter');
    assert.equal(snapshot.working_directory, work); assert.equal((await root.permissions.policy()).mode, 'automatic');
    assert.equal((await client.trees.list()).items.length, 1);
    await expect.poll(() => requests.filter(entry => entry.kind === 'title').length, { timeout: 15_000 }).toBe(1);
    await expect.poll(async () => (await client.trees.get(owner.tree_id)).metadata.title).toBe(title);
    await expect.poll(async () => (await root.activity()).active_turn).toBeNull();
    const history = await root.history.page({ direction: 'forward', limit: 100 });
    const resultMessage = history.messages.find(message => message.role === 'tool' && message.parts.some(part => part.type === 'tool_result' && part.result.call_id === 'onboarding-tool'));
    assert(resultMessage?.turn_id);
    await verifyFixtureHistory(root, { turn: await root.turns.get(resultMessage.turn_id) }, 'onboarding-tool', workerResult, answer, AbortSignal.timeout(10_000));
    // Native compaction preserves four newest conversation turns. An isolated
    // first turn is a successful no-op; add real bounded source turns so the
    // retained helper-routing assertion still exercises an actual provider call.
    const noOpID = crypto.randomUUID();
    await root.history.compact(noOpID);
    assert.equal((await client.wait(noOpID, { signal: AbortSignal.timeout(15_000) })).turn?.state, 'succeeded');
    assert.equal(requests.filter(entry => entry.kind === 'compaction').length, 0);
    for (let index = 0; index < 4; index++) {
      const requestID = crypto.randomUUID();
      await root.submit([{ type: 'text', text: `Onboarding compaction context ${index + 1}` }], requestID);
      const outcome = await client.wait(requestID, { signal: AbortSignal.timeout(15_000) });
      assert.equal(outcome.turn?.state, 'succeeded', outcome.turn?.failure);
    }
    const compactID = crypto.randomUUID();
    await root.history.compact(compactID);
    const compact = await client.wait(compactID, { signal: AbortSignal.timeout(15_000) });
    assert.equal(compact.turn?.state, 'succeeded', compact.turn?.failure);
    assert.equal(requests.filter(entry => entry.kind === 'compaction').length, 1);
    const contextHead = await client.call('context.head', { session_id: rootId });
    assert(contextHead.compaction_id);
    const completedRequestCount = requests.length;
    const composer = page.getByRole('textbox', { name: 'Message WHIP', exact: true });
    await composer.fill(draft);
    await page.reload();
    await expect(page.getByRole('textbox', { name: 'Message WHIP', exact: true })).toHaveValue(draft);
    await expect(page.getByRole('button', { name: 'Permission approval mode', exact: true })).toContainText('Full Access');
    assert.equal(new URL(page.url()).pathname.split('/s/')[1], rootId);
    await capture(page, 'desktop-first-message-reloaded');
    client = undefined;
    await electron.evaluate(({ app }) => app.exit(0)).catch(error => { if (!/closed|destroyed/.test(error.message)) throw error; });
    electron = undefined;
    await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
    electron = await launch();
    const resumed = await electron.firstWindow();
    resumed.on('pageerror', error => errors.push(error.message));
    await expect(resumed.getByRole('textbox', { name: 'Message WHIP', exact: true })).toHaveValue(draft, { timeout: 30_000 });
    await expect(resumed.getByRole('button', { name: 'Permission approval mode', exact: true })).toContainText('Full Access');
    const restarted = await status();
    client = await connect(restarted);
    assert.equal(client.runtimeID, current.process.runtime_id); assert.notEqual(client.processEpoch, current.process.process_epoch);
    const persisted = await client.listProviders();
    assert.equal(persisted.defaults.name, model); assert.equal(persisted.defaults.provider, 'openrouter');
    const restored = await client.session(rootId).get();
    assert.equal((await client.session(rootId).permissions.policy()).mode, 'automatic');
    assert.equal(restored.configuration.model.name, model); assert.equal(restored.configuration.model.provider, 'openrouter'); assert.equal(restored.working_directory, work);
    assert.equal(new URL(resumed.url()).pathname.split('/s/')[1], rootId);
    const readiness = await client.providerReadiness(persisted.defaults);
    assert.equal(readiness.configured, true); assert.equal(readiness.credential_state, 'available');
    assert.equal((await client.trees.list()).items.length, 1);
    assert.equal(requests.filter(entry => entry.kind === 'first-turn').length, 1);
    assert.equal(requests.length, completedRequestCount, 'Reopen must not replay inference, title, compaction, or discovery');
    assert.deepEqual(await client.call('context.head', { session_id: rootId }), contextHead);
    assert.equal((await client.trees.get(restored.tree_id)).metadata.title, title);
    if (providerFailure) throw providerFailure;
    return { provider: 'OpenRouter fixture on loopback; no public provider calls', model,
      explicitRejectedKeyThenValidatedConnection: true, optionalPermissionActions: 2, firstStreamMs, setupToResponseMs,
      composerHiddenUntilProviderChoice: true, rejectedKeyRetainedWithoutRoute: true, maskedKeySavedOnHost: true,
      defaultPairConfirmedInUI: true, nativeFolderSelected: true, firstMessageCreatedOneSession: true,
      streamedBeforeCompletion: true, nativeExecuteRoundTrip: true, titleAndCompactionUseSelectedModel: true,
      auxiliaryOperations: 'Automatic naming intent/fallback commits with first input; queued title helper runs after the active turn. Manual compaction keeps four recent turns; the fixture verifies a no-op before adding four explicit context turns, then performs one real compaction through native receipts.',
      reloadPreservesDraft: true, daemonRestartPreservesSessionDefaultsAndPermission: true, requests };
  } finally {
    releaseAnswer?.();
    provider.closeAllConnections();
    await new Promise(resolve => provider.close(resolve));
  }
}

try {
  ({ stage, provenance } = await onboardingStage(fixture));
  const manifest = await readRuntimeManifest(path.join(stage, 'app/runtime-manifest.json'));
  const started = performance.now();
  electron = await launch();
  const page = await electron.firstWindow();
  page.on('pageerror', error => errors.push(error.message));
  const setup = page.getByRole('button', { name: 'Get Started', exact: true });
  await expect(setup).toBeEnabled({ timeout: 30_000 });
  const initialSetupVisibleMs = performance.now() - started;
  await assert.rejects(lstat(executable), { code: 'ENOENT' });
  await assert.rejects(lstat(env.WHIPCODE_HOME), { code: 'ENOENT' });
  await capture(page, 'desktop-before-setup');
  const clicked = performance.now();
  await setup.click();
  const providers = page.getByRole('region', { name: 'Provider setup', exact: true });
  await expect(providers).toBeVisible({ timeout: 30_000 });
  await expect(providers.getByRole('button', { name: 'Connect Inference.net', exact: true })).toBeEnabled();
  const setupToProviderChooserMs = performance.now() - clicked;
  await expect(setup).toHaveCount(0);
  const labels = await providers.getByRole('button', { name: /^Connect / }).evaluateAll(buttons => buttons.map(button => button.getAttribute('aria-label') ?? button.textContent));
  assert.match(labels[0] ?? '', /Inference.net/);
  await expect(providers.getByText('Recommended', { exact: true })).toHaveCount(1);
  const installed = await status();
  assert.equal(installed.state, 'running'); assert(!installed.process.web_endpoint);
  assert.equal(await fileDigest(executable), manifest.files.whipcode.sha256);
  assert.equal(JSON.parse(await readFile(path.join(env.WHIP_DESKTOP_USER_DATA, 'native-local-runtime.json'), 'utf8')).executable, executable);
  client = await connect(installed);
  const inventory = await client.listProviders();
  assert.equal(inventory.routes.length, 0, 'The isolated fixture unexpectedly inherited routes or credentials');
  assert.equal(inventory.defaults, null);
  await capture(page, 'desktop-provider-chooser');
  const providerHeading = page.getByRole('heading', { name: 'Connect a provider to get started' });
  const firstProvider = providers.getByRole('button', { name: 'Connect Inference.net', exact: true });
  const headingTop = (await providerHeading.boundingBox()).y;
  const firstProviderTop = (await firstProvider.boundingBox()).y;
  await providers.getByRole('button', { name: 'Show all providers', exact: true }).click();
  await expect(providers.getByRole('button', { name: 'Show fewer providers', exact: true })).toBeAttached();
  assert(Math.abs((await providerHeading.boundingBox()).y - headingTop) < 1, 'Expanding providers must not move the heading');
  assert(Math.abs((await firstProvider.boundingBox()).y - firstProviderTop) < 1, 'Expanding providers must not move existing rows');
  await providers.getByRole('button', { name: 'Show fewer providers', exact: true }).click();
  await expect(providers.getByRole('button', { name: 'Show all providers', exact: true })).toBeVisible();
  assert(Math.abs((await providerHeading.boundingBox()).y - headingTop) < 1, 'Collapsing restores the original centered layout');
  // Close only the fixture GUI; accepted daemon work must survive.
  await electron.evaluate(({ app }) => app.exit(0)).catch(error => { if (!/closed|destroyed/.test(error.message)) throw error; });
  electron = undefined;
  assert.equal((await status()).process.pid, installed.process.pid);
  const relaunchStarted = performance.now();
  electron = await launch();
  const reopened = await electron.firstWindow();
  reopened.on('pageerror', error => errors.push(error.message));
  await expect(reopened.getByRole('region', { name: 'Provider setup', exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(reopened.getByRole('button', { name: 'Connect Inference.net', exact: true })).toBeEnabled();
  const relaunchToProviderChooserMs = performance.now() - relaunchStarted;
  assert.equal((await status()).process.pid, installed.process.pid);
  await expect(reopened.getByRole('button', { name: 'Get Started', exact: true })).toHaveCount(0);
  await capture(reopened, 'desktop-relaunched');
  const firstMessage = await firstMessageScenario();
  assert.deepEqual(errors, []);
  const result = {
    recordedAt: new Date().toISOString(), purpose: 'Isolated staged Electron first-run onboarding; not signed/Finder acceptance or public-provider compatibility validation',
    provenance, runtimeBuild: manifest.buildId, runtimeDigest: manifest.files.whipcode.sha256, rendererDigest: manifest.rendererDigest,
    setupActions: 1, initialSetupVisibleMs, setupToProviderChooserMs, relaunchToProviderChooserMs,
    timingSemantics: 'One warm scripted sample, not a human usability measurement. UI waits include rendering and test observation. Authentication and inference use only the local fixture provider; no paid request was performed.',
    missingBackendStartsOnlyAfterSetup: true, verifiedInstalledBytes: true, noInheritedCredentials: true,
    inferenceNetFirstWithOneRecommendation: true, noDaemonTCP: true, relaunchAttachedSameDaemon: true,
    firstMessage, rendererErrors: errors, screenshots,
  };
  await writeFile(output, JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result, null, 2));
} finally {
  if (electron) await electron.evaluate(({ app }) => app.exit(0)).catch(() => {});
  try {
    if (await lstat(executable).catch(() => undefined)) {
      await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
      assert.equal((await status()).state, 'stopped');
    }
    await rm(fixture, { recursive: true, force: true });
  } catch (error) {
    console.error(`Preserved isolated onboarding fixture after cleanup failure: ${fixture}`);
    throw error;
  }
}
