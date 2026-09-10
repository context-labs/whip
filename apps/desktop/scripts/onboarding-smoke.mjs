// Exercise first-run setup in the staged Electron host with isolated state.
// This uses stock Electron, not the signed/fused installed application.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { once } from 'node:events';
import { lstat, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import path from 'node:path';
import { promisify } from 'node:util';
import { _electron } from 'playwright';
import { expect } from '@playwright/test';
import { parseConfigFileTextToJson } from 'typescript';
import { createWhipClient } from '@whip/sdk';
import { unixSocket } from '@whip/sdk/node';
import { fileDigest, readRuntimeManifest } from '../src/runtime.ts';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';

const exec = promisify(execFile);
const fixture = await mkdtemp('/tmp/whip-onboarding-');
const stage = path.join(repositoryRoot, 'apps/desktop/.stage');
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
const status = async () => JSON.parse((await exec(executable, ['daemon', 'status', '--json'], { env, timeout: 5000 })).stdout);

async function firstMessageScenario() {
  const model = 'fixture-openrouter-model';
  const alias = 'fixture-coding';
  const marker = 'Whip onboarding tool fixture';
  const prompt = 'Calculate 6 * 7 with rlm_exec and explain the answer.';
  const prefix = 'Verified desktop onboarding: ';
  const answer = prefix + '42';
  const title = 'Verify desktop onboarding';
  const draft = 'Retain this unsent follow-up after reopening.';
  const work = path.join(fixture, 'project');
  const requests = [];
  let providerFailure;
  let releaseAnswer;
  const provider = createServer((request, response) => {
    void (async () => {
      assert(requests.length < 16, 'Unexpected provider request loop');
      assert.equal(request.headers.authorization, 'Bearer fixture-only-key');
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
      if (!body.stream) {
        const naming = body.messages[0].content.startsWith('Name this coding session.');
        requests.push({ kind: naming ? 'title' : 'compaction', model: body.model });
        response.writeHead(200, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify({ choices: [{ message: { role: 'assistant', content: naming ? title : 'The user asked to calculate 6 * 7. The Starlark tool returned 42.' }, finish_reason: 'stop' }],
          usage: { prompt_tokens: 100, completion_tokens: 12 } }));
        return;
      }
      assert.deepEqual(body.tools.map(tool => tool.function.name), ['rlm_exec']);
      assert(body.messages.some(message => message.role === 'user' && message.content === prompt));
      const tool = body.messages.find(message => message.role === 'tool' && message.tool_call_id === 'onboarding-tool');
      requests.push({ kind: tool ? 'tool-result' : 'first-turn', model: body.model });
      response.writeHead(200, { 'Content-Type': 'text/event-stream' });
      if (!tool) {
        assert.equal(requests.filter(entry => entry.kind === 'first-turn').length, 1, 'First prompt must be submitted exactly once');
        response.end(`data: ${JSON.stringify({ choices: [{ delta: { tool_calls: [{ index: 0, id: 'onboarding-tool', type: 'function', function: {
          name: 'rlm_exec', arguments: JSON.stringify({ code: `print(${JSON.stringify(marker)})\n{"answer": 6 * 7}` }),
        } }] }, finish_reason: 'tool_calls' }] })}\n\ndata: [DONE]\n\n`);
        return;
      }
      const result = JSON.parse(tool.content);
      assert.equal(result.output, marker + '\n'); assert.deepEqual(result.value, { answer: 42 }); assert(result.steps > 0);
      response.write(`data: ${JSON.stringify({ choices: [{ delta: { content: prefix }, finish_reason: null }] })}\n\n`);
      // The UI must observe a partial answer before the final chunk is released.
      await new Promise(resolve => {
        const timeout = setTimeout(resolve, 10_000);
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
    client.close(); client = undefined;
    await electron.evaluate(({ app }) => app.exit(0)).catch(error => { if (!/closed|destroyed/.test(error.message)) throw error; });
    electron = undefined;
    await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
    assert.equal((await status()).state, 'stopped');
    const configFile = path.join(env.WHIPCODE_HOME, 'config.json');
    const parsed = parseConfigFileTextToJson(configFile, await readFile(configFile, 'utf8'));
    assert(!parsed.error, 'Could not read the fixture JSONC configuration');
    const config = parsed.config;
    config.providers.openrouter = { name: 'OpenRouter', api: 'openai-completions', baseUrl: `http://127.0.0.1:${provider.address().port}` };
    config.models[alias] = { id: model, providers: ['openrouter'], context: 65536, maxOut: 1024 };
    // Retain shipped default/compaction aliases to exercise cross-provider repair.
    await writeFile(configFile, JSON.stringify(config), { mode: 0o600 });
    await mkdir(work);
    electron = await launch();
    const page = await electron.firstWindow();
    page.on('pageerror', error => errors.push(error.message));
    const input = page.getByRole('textbox', { name: 'Your first message', exact: true });
    await expect(input).toBeVisible({ timeout: 30_000 });
    const current = await status();
    client = createWhipClient({ endpoint: unixSocket(current.socket), clientId: `first-message-${crypto.randomUUID()}`, clientKind: 'human' });
    await client.connect();
    assert.equal((await client.providers.list()).providers.find(entry => entry.id === 'openrouter').status.available, false);
    assert.equal((await client.sessions.list()).items.length, 0);
    const started = performance.now();
    await input.fill(prompt);
    await page.getByRole('button', { name: 'Connect OpenRouter', exact: true }).click();
    const connection = page.getByRole('dialog', { name: 'OpenRouter', exact: true });
    await expect(connection.getByLabel('API key', { exact: true })).toHaveAttribute('type', 'password');
    await connection.getByLabel('API key', { exact: true }).fill('fixture-only-key');
    await connection.getByRole('button', { name: 'Connect', exact: true }).click();
    await expect(connection).toHaveCount(0);
    await page.getByRole('button', { name: `Use ${alias}`, exact: true }).click();
    await expect(input).toHaveValue(prompt);
    await expect(input).toBeFocused();
    const selected = await client.configuration.get();
    assert.equal(selected.default_model, alias); assert.equal(selected.default_provider, 'openrouter');
    assert.equal(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }).includes('fixture-only-key')), false);
    assert.equal(requests.filter(entry => entry.kind !== 'models').length, 0, 'Setup must not send a hidden inference request');
    await electron.evaluate(({ dialog }, directory) => { dialog.showOpenDialog = async () => ({ canceled: false, filePaths: [directory] }); }, work);
    await page.getByRole('button', { name: 'Choose folder…', exact: true }).click();
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
    // Naming is opt-in in the web app. Exercise its existing command while
    // the test holds the stream, before the completed exchange triggers it.
    assert.equal((await root.command('session.autotitle', {}).result({ signal: AbortSignal.timeout(5000) })).status, 'succeeded');
    await capture(page, 'desktop-first-message-streaming');
    releaseAnswer();
    await expect(page.getByText(answer, { exact: true }).last()).toBeVisible({ timeout: 15_000 });
    const setupToResponseMs = performance.now() - started;
    await expect.poll(async () => Object.keys((await root.snapshot()).active_turns).length).toBe(0);
    const snapshot = await root.snapshot();
    assert.equal(snapshot.meta.model, alias); assert.equal(snapshot.meta.provider, 'openrouter');
    assert.equal(snapshot.meta.cwd, work); assert.equal(snapshot.permission_mode, 'automatic');
    assert.equal((await client.sessions.list()).items.length, 1);
    await expect.poll(() => requests.filter(entry => entry.kind === 'title').length, { timeout: 10_000 }).toBe(1);
    await expect.poll(async () => (await root.snapshot()).meta.title).toBe(title);
    const compact = await root.history.compact().result({ signal: AbortSignal.timeout(15_000) });
    assert.equal(compact.status, 'succeeded');
    assert.equal(requests.filter(entry => entry.kind === 'compaction').length, 1);
    const composer = page.getByRole('textbox', { name: 'Message WHIP', exact: true });
    await composer.fill(draft);
    await page.reload();
    await expect(page.getByRole('textbox', { name: 'Message WHIP', exact: true })).toHaveValue(draft);
    await expect(page.getByRole('button', { name: 'Permission approval mode', exact: true })).toContainText('Full Access');
    assert.equal(new URL(page.url()).pathname.split('/s/')[1], rootId);
    await capture(page, 'desktop-first-message-reloaded');
    client.close(); client = undefined;
    await electron.evaluate(({ app }) => app.exit(0)).catch(error => { if (!/closed|destroyed/.test(error.message)) throw error; });
    electron = undefined;
    await exec(executable, ['daemon', 'stop'], { env, timeout: 15_000 });
    electron = await launch();
    const resumed = await electron.firstWindow();
    resumed.on('pageerror', error => errors.push(error.message));
    await expect(resumed.getByRole('textbox', { name: 'Message WHIP', exact: true })).toHaveValue(draft, { timeout: 30_000 });
    await expect(resumed.getByRole('button', { name: 'Permission approval mode', exact: true })).toContainText('Full Access');
    const restarted = await status();
    client = createWhipClient({ endpoint: unixSocket(restarted.socket), clientId: `resumed-${crypto.randomUUID()}`, clientKind: 'human' });
    await client.connect();
    const persisted = await client.configuration.get();
    assert.equal(persisted.default_model, alias); assert.equal(persisted.default_provider, 'openrouter');
    const restored = await client.session(rootId).snapshot();
    assert.equal(restored.permission_mode, 'automatic');
    assert.equal(restored.meta.model, alias); assert.equal(restored.meta.provider, 'openrouter'); assert.equal(restored.meta.cwd, work);
    assert.equal(new URL(resumed.url()).pathname.split('/s/')[1], rootId);
    assert.equal((await client.providers.list()).selection.ready, true);
    assert.equal((await client.sessions.list()).items.length, 1);
    assert.equal(requests.filter(entry => entry.kind === 'first-turn').length, 1);
    if (providerFailure) throw providerFailure;
    return { provider: 'OpenRouter fixture on loopback; no public provider calls', model, alias,
      connectionAndSendActions: 5, optionalPermissionActions: 2, firstStreamMs, setupToResponseMs,
      draftedBeforeAuthentication: true, draftPreservedThroughAuthentication: true, maskedKeySavedOnHost: true,
      defaultPairConfirmedInUI: true, nativeFolderSelected: true, firstMessageCreatedOneSession: true,
      streamedBeforeCompletion: true, rlmExecRoundTrip: true, titleAndCompactionUseSelectedModel: true,
      auxiliaryOperations: 'Title opt-in and manual compaction use existing SDK commands; the first prompt is submitted from the renderer UI.',
      reloadPreservesDraft: true, daemonRestartPreservesSessionDefaultsAndPermission: true, requests };
  } finally {
    releaseAnswer?.();
    provider.closeAllConnections();
    await new Promise(resolve => provider.close(resolve));
  }
}

try {
  const manifest = await readRuntimeManifest(path.join(stage, 'app/runtime-manifest.json'));
  const started = performance.now();
  electron = await launch();
  const page = await electron.firstWindow();
  page.on('pageerror', error => errors.push(error.message));
  const setup = page.getByRole('button', { name: 'Set up this Mac', exact: true });
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
  const labels = await providers.getByRole('button', { name: /^Connect / }).allTextContents();
  assert.match(labels[0] ?? '', /Inference.net/);
  await expect(providers.getByText('Recommended', { exact: true })).toHaveCount(1);
  const installed = await status();
  assert.equal(installed.state, 'running'); assert(!installed.network_endpoint);
  assert.equal(await fileDigest(executable), manifest.files.whipcode.sha256);
  assert.equal(JSON.parse(await readFile(path.join(env.WHIP_DESKTOP_USER_DATA, 'native-local-runtime.json'), 'utf8')).executable, executable);
  client = createWhipClient({ endpoint: unixSocket(installed.socket), clientId: `onboarding-${crypto.randomUUID()}`, clientKind: 'human' });
  await client.connect();
  const inventory = await client.providers.list();
  assert(inventory.providers.every(entry => !entry.status.available), 'The isolated fixture unexpectedly inherited credentials');
  assert.equal(inventory.selection.ready, false);
  await capture(page, 'desktop-provider-chooser');
  // Close only the fixture GUI; accepted daemon work must survive.
  await electron.evaluate(({ app }) => app.exit(0)).catch(error => { if (!/closed|destroyed/.test(error.message)) throw error; });
  electron = undefined;
  assert.equal((await status()).pid, installed.pid);
  const relaunchStarted = performance.now();
  electron = await launch();
  const reopened = await electron.firstWindow();
  reopened.on('pageerror', error => errors.push(error.message));
  await expect(reopened.getByRole('region', { name: 'Provider setup', exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(reopened.getByRole('button', { name: 'Connect Inference.net', exact: true })).toBeEnabled();
  const relaunchToProviderChooserMs = performance.now() - relaunchStarted;
  assert.equal((await status()).pid, installed.pid);
  await expect(reopened.getByRole('button', { name: 'Set up this Mac', exact: true })).toHaveCount(0);
  await capture(reopened, 'desktop-relaunched');
  const firstMessage = await firstMessageScenario();
  assert.deepEqual(errors, []);
  const result = {
    recordedAt: new Date().toISOString(), purpose: 'Isolated staged Electron first-run onboarding; not signed/Finder acceptance or public-provider compatibility validation',
    runtimeBuild: manifest.buildId, runtimeDigest: manifest.files.whipcode.sha256, rendererDigest: manifest.rendererDigest,
    setupActions: 1, initialSetupVisibleMs, setupToProviderChooserMs, relaunchToProviderChooserMs,
    timingSemantics: 'One warm scripted sample, not a human usability measurement. UI waits include rendering and test observation. Authentication and inference use only the local fixture provider; no paid request was performed.',
    missingBackendStartsOnlyAfterSetup: true, verifiedInstalledBytes: true, noInheritedCredentials: true,
    inferenceNetFirstWithOneRecommendation: true, noDaemonTCP: true, relaunchAttachedSameDaemon: true,
    firstMessage, rendererErrors: errors, screenshots,
  };
  await writeFile(output, JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result, null, 2));
} finally {
  client?.close();
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
