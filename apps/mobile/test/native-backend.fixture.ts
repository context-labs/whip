/// <reference types="node" />
/** @jest-environment node */
import { execFile, spawn, type ChildProcessWithoutNullStreams } from 'node:child_process';
import { once } from 'node:events';
import { existsSync } from 'node:fs';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { createServer, type Server } from 'node:http';
import { Client, type Operations } from '@whip/sdk';
import { browserSocket } from '@whip/sdk/browser';
import { DatabaseSync } from 'node:sqlite';
import { promisify } from 'node:util';
import { createTraceView } from '@whip/sdk/state';
import { MobileRuntime, type SavedHost } from '../src/runtime/runtime';
import { connectMobile } from '../src/runtime/connection';
import { SqliteMobileStorage, type StorageDatabase } from '../src/runtime/storage';
import { draftKey } from '../src/runtime/address';
import { readMobileContent } from '../src/features/content';
import { historyCut } from '../src/features/history-actions';
jest.mock('expo-crypto', () => ({ randomUUID: () => require('node:crypto').randomUUID(), CryptoDigestAlgorithm: { SHA256: 'sha256' }, digestStringAsync: async (_: string, value: string) => require('node:crypto').createHash('sha256').update(value).digest('hex'), digest: async (_: string, value: Uint8Array) => require('node:crypto').createHash('sha256').update(value).digest() }));
jest.setTimeout(120_000);
const deadline = () => ({ signal: AbortSignal.timeout(15_000) });
async function until<T>(read: () => T | Promise<T>, predicate: (value: T) => boolean, label: string) {
  const end = Date.now() + 15_000;
  while (Date.now() < end) { const value = await read(); if (predicate(value)) return value; await new Promise(resolve => setTimeout(resolve, 20)); }
  throw new Error('Mobile fixture observation did not settle: ' + label);
}
type Ready = { runtime_id: string; process_epoch: string; web: string };
let provider: Server;
let directory: string, binary: string, process: ChildProcessWithoutNullStreams | undefined, ready: Ready, diagnostic = '';
const runtimes: MobileRuntime[] = [];
async function stop() {
  if (!process || process.exitCode !== null || process.signalCode !== null) return;
  const ended = once(process, 'exit'), held = process; held.kill('SIGTERM');
  const timer = setTimeout(() => held.kill('SIGKILL'), 5000); try { await ended; } finally { clearTimeout(timer); }
}
async function start(scripted = false, browserDriver = ''): Promise<Ready> {
  process = spawn(binary, ['-directory', join(directory, 'host'), ...(scripted ? ['-scripted'] : []), '-web', '-web-listen', '127.0.0.1:0'], { cwd: directory, env: { NODE_ENV: 'test', PATH: '/usr/bin:/bin:/usr/sbin:/sbin', HOME: join(directory, 'home'), XDG_CONFIG_HOME: join(directory, 'home', '.config'), ZDOTDIR: join(directory, 'home'), TMPDIR: join(directory, 'tmp'), SHELL: '/bin/sh', WHIPCODE_HOME: join(directory, 'home', '.whipcode'), WHIP_BROWSER_DRIVER: browserDriver } });
  const held = process; held.stderr.on('data', data => { diagnostic = (diagnostic + data).slice(-(1 << 20)); });
  return new Promise((yes, no) => {
    let line = '';
    const cleanup = () => { clearTimeout(timer); held.stdout.off('data', data); held.off('error', failed); held.off('exit', exited); };
    const failed = (error: Error) => { cleanup(); no(error); }, exited = () => failed(new Error('Fixture runtime exited: ' + diagnostic));
    const data = (chunk: Buffer) => { line += chunk; if (line.length > 65536) { failed(new Error('Oversized startup line')); return; } const end = line.indexOf('\n'); if (end < 0) return; try { const value = JSON.parse(line.slice(0, end)); cleanup(); yes(value); } catch (error) { failed(error as Error); } };
    const timer = setTimeout(() => failed(new Error('Runtime startup timed out: ' + diagnostic)), 15_000);
    held.stdout.on('data', data); held.once('error', failed); held.once('exit', exited);
  });
}
beforeAll(async () => {
  Object.assign(globalThis, { __DEV__: true });
  provider = createServer(async (request, response) => {
    let body = ''; for await (const chunk of request) { body += chunk; if (body.length > (1 << 20)) { response.writeHead(413).end(); return; } }
    const messages = JSON.parse(body).messages, last = messages.at(-1);
    const text = typeof last?.content === 'string' ? last.content : (last?.content ?? []).filter((part: { type: string }) => part.type === 'text').map((part: { text: string }) => part.text).join('\n');
    const quickjs = text.endsWith('quickjs-question');
    const code = quickjs ? 'console.log("before question 界"); console.log(await user.ask({question:"Choose a route",options:[{label:"A"},{label:"B"}]}))' : 'print("before question 界")\nprint(user.ask(question="Choose a route",options=[{"label":"A"},{"label":"B"}]))';
    const message = !text.endsWith('-question') || last?.role === 'tool' ? { role: 'assistant', content: last?.role === 'tool' ? 'Question answered' : 'ack: ' + text } : { role: 'assistant', content: null, tool_calls: [{ id: 'ask-once', type: 'function', function: { name: 'execute', arguments: JSON.stringify({ code }) } }] };
    response.setHeader('Content-Type', 'application/json'); response.end(JSON.stringify({ choices: [{ message, finish_reason: message.tool_calls ? 'tool_calls' : 'stop' }], usage: { prompt_tokens: 5, completion_tokens: 5, cost: 0 } }));
  });
  provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
  directory = await mkdtemp('/tmp/whip-mobile-v4-'); await mkdir(join(directory, 'home')); await mkdir(join(directory, 'tmp')); binary = join(directory, 'runtime');
  await promisify(execFile)('go', ['build', '-race=false', '-o', binary, './cmd/whip-runtime'], { cwd: resolve('../..'), timeout: 60_000, env: { ...global.process.env, GOTOOLCHAIN: 'go1.27.0' } });
  ready = await start(true); await stop();
  const configPath = join(directory, 'host', 'host.json'), config = JSON.parse(await readFile(configPath, 'utf8')), address = provider.address();
  if (!address || typeof address === 'string') throw new Error('Fixture provider address missing');
  config.providers.questions = { kind: 'openai-chat', base_url: `http://127.0.0.1:${address.port}/v1`, credential_source: 'none', models: { questions: { max_output_tokens: 500, timeout_millis: 30000, max_attempts: 1 } } };
  await writeFile(configPath, JSON.stringify(config), { mode: 0o600 }); ready = await start();
});
afterAll(async () => { for (const runtime of runtimes) await runtime.dispose(); await stop(); if (provider) await new Promise<void>(yes => provider.close(() => yes())); if (directory) await rm(directory, { recursive: true, force: true }); });
async function mobile(name: string) {
  const path = join(directory, name + '.db'), fresh = !existsSync(path), db = new DatabaseSync(path);
  const adapter: StorageDatabase = { execAsync: async sql => { db.exec(sql); }, runAsync: async (sql, ...args) => db.prepare(sql).run(...args), getAllAsync: async <T,>(sql: string, ...args: Array<string | number>) => db.prepare(sql).all(...args) as T[], getFirstAsync: async <T,>(sql: string, ...args: Array<string | number>) => (db.prepare(sql).get(...args) ?? null) as T | null, closeAsync: async () => db.close() };
  const storage = new SqliteMobileStorage(adapter); await storage.initialize(fresh);
  const requests: string[] = []; let dropMethod: string | undefined;
  const runtime = new MobileRuntime(storage, async (host, signal) => {
    const client = await connectMobile(host, signal), call = client.call.bind(client);
    // The real gateway has durably replied before the phone loses its ACK.
    jest.spyOn(client, 'call').mockImplementation(async (...args: Parameters<typeof call>) => {
      requests.push(args[0]); let result;
      try { result = await call(...args); } catch (error) { if (error instanceof Error) error.message = args[0] + ': ' + error.message; throw error; }
      if (dropMethod === args[0]) { dropMethod = undefined; throw new DOMException('lost mobile ACK', 'AbortError'); }
      return result;
    });
    return client;
  });
  runtimes.push(runtime); await runtime.start(false);
  const host: SavedHost = { id: 'test-host', name: 'Disposable backend', url: ready.web, runtimeId: ready.runtime_id, clientId: 'mobile-' + name };
  await runtime.connect(host);
  return { runtime, storage, requests, host, loseSubmit() { dropMethod = 'sessions.submit'; }, lose(method: string) { dropMethod = method; } };
}
async function root(runtime: MobileRuntime, name: string, engine: 'starlark' | 'quickjs' = 'starlark') {
  const client = runtime.requireReady(), definition = client.builtins.find(item => item.id === 'assistant');
  if (!definition) throw new Error('Pinned assistant definition missing');
  const created = await runtime.run('trees.create', { creation_id: name, engine, definition, working_directory: directory, overrides: { automatic_title: false, model: { provider: 'questions', name: 'questions', effort: '' } }, metadata: { title: name, pinned: false, archived: false } });
  if (!created.root) throw new Error('Fresh creation is missing its root'); return created.root;
}
test.each(['starlark', 'quickjs'] as const)('mobile observes %s root/child history, permissions, questions, traces and scoped content over the real gateway', async engine => {
  const f = await mobile(engine), client = f.runtime.requireReady(), parent = await root(f.runtime, engine, engine);
  const spawned = await f.runtime.run('sessions.spawn', { identity: { client_id: client.clientID, request_id: engine + '-spawn' }, parent_id: parent.id, overrides: {}, parts: [{ type: 'text', text: 'child only' }], grant_ids: null }, { rootId: parent.id });
  const child = spawned.session!; expect(child.parent_id).toBe(parent.id); expect(child.tree_id).toBe(parent.tree_id);
  expect((await client.wait(engine + '-spawn', deadline())).turn?.state).toBe('succeeded');
  let lease = f.runtime.acquireView(child.id, client.runtimeID);
  await until(() => lease.view.getSnapshot(), value => value.status === 'live' && value.history.messages.length === 2, 'child transcript');
  expect(lease.view.getSnapshot().history.messages.every(message => message.session_id === child.id)).toBe(true);
  expect(lease.view.getSnapshot().history.messages[1].parts).toEqual([{ type: 'text', text: 'ack: child only' }]);
  await until(() => lease.execution.getSnapshot(), value => value.turns.some(turn => turn.session_id === child.id && turn.state === 'succeeded'), 'child execution');
  const parentHistory = await client.call('sessions.history_page', { session_id: parent.id, direction: 'forward', limit: 20 }, deadline());
  expect((parentHistory.messages ?? []).every(message => message.session_id === parent.id && message.input_id !== spawned.admission.input?.id)).toBe(true); // Completion mail is legitimate parent history.
  expect((await client.trees.list({ search: engine }, deadline())).items.some(item => item.root_id === parent.id)).toBe(true);
  expect((await client.trees.recent(64, deadline())).items.some(item => item.root_id === parent.id)).toBe(true);
  const reference = await client.session(child.id).content.put({ reference_id: engine + '-text', media_type: 'text/plain', data_base64: Buffer.from('Scoped mobile text 🐎').toString('base64') }, deadline());
  expect(await readMobileContent(client, ready.web, child.id, reference.id, deadline().signal)).toBe('Scoped mobile text 🐎');
  await expect(readMobileContent(client, ready.web, parent.id, reference, deadline().signal)).rejects.toThrow('another recipient');
  await expect(client.session(parent.id).content.get(reference.id, deadline())).rejects.toMatchObject({ kind: 'NOT_FOUND' });
  const writeTool = { module: 'files' as const, name: 'write', arguments_base64: Buffer.from(JSON.stringify({ path: engine + '.txt', content: 'approved once' })).toString('base64') };
  await client.callTool(child.id, writeTool, engine + '-undelegated', deadline());
  expect((await client.wait(engine + '-undelegated', deadline())).turn).toMatchObject({ state: 'failed', failure: 'no delegated authority' });
  await client.callTool(parent.id, writeTool, engine + '-write', deadline());
  const permissions = await until(async () => {
    const value = await client.session(parent.id).permissions.list({ pending_only: true, limit: 16 }, deadline());
    if (!value.items?.length) { const state = await client.recover(engine + '-write', deadline()); if (state.turn?.finished_at) throw new Error('Write finished before consent: ' + JSON.stringify(state.turn)); }
    return value;
  }, value => value.items?.length === 1, 'file permission');
  expect(existsSync(join(directory, engine + '.txt'))).toBe(false);
  expect((await client.hostAttention({ limit: 64, max_bytes: 128 << 10, after: null }, deadline())).items.some(item => item.root_id === parent.id && item.session_id === parent.id)).toBe(true);
  await f.runtime.decisions.decide(parent.id, permissions.items![0].operation_id, parent.id, true);
  expect((await client.wait(engine + '-write', deadline())).turn?.state).toBe('succeeded'); expect(await readFile(join(directory, engine + '.txt'), 'utf8')).toBe('approved once');
  const beforeAsk = await client.session(parent.id).get(deadline()); await client.session(parent.id).configure(beforeAsk.config_revision, { model: { provider: 'questions', name: 'questions', effort: '' } }, deadline());
  await f.runtime.run('sessions.submit', { session_id: parent.id, identity: { client_id: client.clientID, request_id: engine + '-ask' }, source: 'user', parts: [{ type: 'text', text: engine + '-question' }] }, { rootId: parent.id });
  lease.release();
  const rootLease = f.runtime.acquireView(parent.id, client.runtimeID);
  const questions = await until(async () => {
    const value = await client.session(parent.id).questions.list({ pending_only: true, limit: 16 }, deadline());
    if (!value.items.length) { const state = await client.recover(engine + '-ask', deadline()); if (state.turn?.finished_at) throw new Error('Question finished before response: ' + JSON.stringify(state.turn)); }
    return value;
  }, value => value.items.length === 1, 'user question'), question = questions.items[0];
  const liveOutput = await until(async () => { await rootLease.view.refresh(); await rootLease.execution.refresh(); return rootLease.execution.getSnapshot(); }, value => value.output?.cell_id === question.cell_id, 'native stdout before human answer');
  expect(liveOutput.output?.text).toBe('before question 界\n');
  expect(liveOutput.output?.session_id).toBe(parent.id);
  const prefill = await client.session(parent.id).context.usage(deadline());
  expect(prefill.prefill).toMatchObject({ input_tokens: '5', input_source: 'reported', stale: true });
  expect(prefill.prefill?.context_window_tokens).toBeNull();
  await f.runtime.decisions.answer(parent.id, parent.id, question.operation_id, [{ answer: ['B'], dismissed: false }], {}, question.request);
  const answered = await client.wait(engine + '-ask', deadline());
  expect(answered.turn?.state).toBe('succeeded');
  await rootLease.view.refresh(); await rootLease.execution.refresh(); expect(rootLease.execution.getSnapshot().output).toBeNull();
  const turnUsage = await client.session(parent.id).turns.usage(question.turn_id, deadline());
  expect(turnUsage.usage.attempts.settled).toBe('2'); expect(turnUsage.compactions).toBe('0'); expect(turnUsage.usage.input_tokens.value).toBe('10');
  rootLease.release(); lease = f.runtime.acquireView(child.id, client.runtimeID);
  await until(() => lease.view.getSnapshot(), value => value.status === 'live', 'child reselected');
  expect((await client.session(parent.id).questions.get(question.operation_id, deadline())).answers).toEqual([{ answer: ['B'], dismissed: false }]);
  const trace = createTraceView(client, parent.id, { maxBytes: 256 << 10, maxRows: 64, maxRoots: 16 });
  try { await trace.start(); expect(trace.getSnapshot().roots.length).toBeGreaterThan(0); expect(trace.getSnapshot().retainedBytes).toBeLessThanOrEqual(256 << 10); } finally { await trace.dispose(); }
  const policy = await client.session(parent.id).permissions.policy(deadline()); await f.runtime.run('permissions.set_mode', { session_id: parent.id, edit_id: engine + '-mode', expected_revision: policy.revision, mode: 'automatic' }, { rootId: parent.id });
  expect((await client.session(child.id).permissions.policy(deadline())).mode).toBe('automatic');
  await lease.view.refresh(); const cut = historyCut(await client.session(child.id).get(deadline()), lease.view.getSnapshot(), '2');
  const fork = await f.runtime.run('sessions.fork', { ...cut, fork_id: engine + '-fork', title: null }, { rootId: parent.id }); expect(fork.root?.parent_id).toBe(null);
  await client.session(child.id).lifecycle('stopped', deadline());
  expect((await client.session(parent.id).get(deadline())).lifecycle).toBe('active');
  await f.runtime.run('sessions.rewind', { session_id: child.id, edit_id: engine + '-rewind', expected_revision: cut.expected_history_revision, observed_through: cut.observed_through, keep_through: '0' }, { rootId: parent.id });
  await lease.view.refresh(); expect(lease.view.getSnapshot().history.messages).toHaveLength(0); await client.session(child.id).lifecycle('active', deadline()); lease.release(); await f.runtime.dispose();
});
test('lost ACK, mobile restart and host restart inspect durable evidence without replay or cross-epoch requests', async () => {
  let f = await mobile('recovery'); const owner = await root(f.runtime, 'recovery-root'), client = f.runtime.requireReady();
  const key = draftKey(client.runtimeID, owner.id, owner.id), draft = f.runtime.setDraft(key, 'send once'); f.loseSubmit();
  const submit: Operations['sessions.submit']['params'] = { session_id: owner.id, identity: { client_id: client.clientID, request_id: 'lost' }, source: 'user' as const, parts: [{ type: 'text' as const, text: 'send once' }] };
  await expect(f.runtime.run('sessions.submit', submit, { rootId: owner.id, intent: { draftKey: key, draftRevision: draft.revision } })).rejects.toThrow('lost mobile ACK');
  await client.wait('lost', deadline()); await f.runtime.reconnect(); expect(f.requests.filter(method => method === 'sessions.submit')).toHaveLength(1);
  const unknown = f.runtime.getSnapshot().commands.find(item => item.record.commandId === 'lost')!; expect(unknown.status).toBe('unknown'); await f.runtime.checkCommand(unknown); expect(f.runtime.draft(key).text).toBe('');
  const second = f.runtime.setDraft(key, 'preserve after restart'); f.loseSubmit(); await expect(f.runtime.run('sessions.submit', { ...submit, identity: { client_id: client.clientID, request_id: 'restart' }, parts: [{ type: 'text', text: second.text }] }, { rootId: owner.id, intent: { draftKey: key, draftRevision: second.revision } })).rejects.toThrow('lost mobile ACK');
  await f.runtime.requireReady().wait('restart', deadline());
  f.storage.prepareIntent('old-request', { agentId: 'old-root' });
  await f.storage.recoveryStorage.put({ version: 1, runtimeId: 'retired-runtime', clientId: 'retired-client', commandId: 'old-request', operation: 'submit', rootId: 'old-root' });
  await f.runtime.dispose(); const previous = ready; await stop(); ready = await start(); expect(ready.runtime_id).toBe(previous.runtime_id); expect(ready.process_epoch).not.toBe(previous.process_epoch);
  // Reach the restarted process through its new address using the old epoch pin.
  await expect(Client.connect(browserSocket(ready.web, { expectedRuntimeID: previous.runtime_id, expectedProcessEpoch: previous.process_epoch }), { clientID: 'stale', ...deadline() })).rejects.toMatchObject({ kind: 'IDENTITY' });
  f = await mobile('recovery'); expect(f.requests).toEqual([]); const restored = f.runtime.getSnapshot().commands.find(item => item.record.commandId === 'restart')!;
  expect(restored.retryable).toBe(false); await f.runtime.checkCommand(restored); const inspected = f.runtime.getSnapshot().commands.find(item => item.record.commandId === 'restart')!; expect(inspected.status).toBe('identity_only'); expect(inspected.retryable).toBe(false);
  expect(f.runtime.draft(key).text).toBe('preserve after restart'); expect(f.requests).toEqual(['receipts.get']); expect((await f.storage.listRecovery())[0].record.commandId).toBe('old-request');
  const messages = (await f.runtime.requireReady().call('sessions.history_page', { session_id: owner.id, direction: 'forward', limit: 20 }, deadline())).messages; expect(messages!.filter(message => message.role === 'user')).toHaveLength(2);
});

test('mobile denial and captured reload keep independent policy and exact outcomes across lost ACK and phone restart', async () => {
  let f = await mobile('controls'); const owner = await root(f.runtime, 'controls-root'); let client = f.runtime.requireReady();
  await f.runtime.run('sessions.submit', { session_id: owner.id, identity: { client_id: client.clientID, request_id: 'hold-reload' }, source: 'user', parts: [{ type: 'text', text: 'starlark-question' }] }, { rootId: owner.id });
  await until(() => client.session(owner.id).questions.list({ pending_only: true, limit: 16 }, deadline()), value => value.items.length === 1, 'question holds idle reload boundary');
  const original = await client.session(owner.id).get(deadline());
  f.lose('sessions.reload');
  await expect(f.runtime.run('sessions.reload', { session_id: owner.id, edit_id: 'captured-mobile', expected_revision: original.config_revision }, { rootId: owner.id })).rejects.toThrow('lost mobile ACK');
  expect((await client.session(owner.id).reloads.get('captured-mobile', deadline())).state).toBe('pending');
  expect((await client.session(owner.id).get(deadline())).config_revision).toBe(original.config_revision);
  await f.runtime.dispose(); f = await mobile('controls'); client = f.runtime.requireReady();
  expect(f.requests).toEqual([]);
  const restored = f.runtime.getSnapshot().commands.find(item => item.record.commandId === 'captured-mobile')!;
  expect((await f.runtime.checkCommand(restored)).status).toBe('identity_only');
  expect(f.requests).toEqual(['sessions.reload_edit']);
  expect((await client.session(owner.id).reloads.cancel('captured-mobile', deadline())).state).toBe('interrupted');
  await f.runtime.forgetCommand(restored);
  const policy = await client.session(owner.id).permissions.policy(deadline()); expect(policy.deny_interactive).toBe(false);
  f.lose('permissions.set_denial');
  await expect(f.runtime.run('permissions.set_denial', { session_id: owner.id, edit_id: 'deny-mobile', expected_revision: policy.revision, deny_interactive: true }, { rootId: owner.id })).rejects.toThrow('lost mobile ACK');
  const denial = f.runtime.getSnapshot().commands.find(item => item.record.commandId === 'deny-mobile')!;
  expect((await f.runtime.checkCommand(denial)).knownAccepted).toBe(true);
  const denied = await client.session(owner.id).permissions.policy(deadline()); expect(denied.deny_interactive).toBe(true); expect(denied.mode).toBe(policy.mode);
  const intrinsic = await client.session(owner.id).questions.list({ pending_only: true, limit: 16 }, deadline()); expect(intrinsic.items).toHaveLength(1);
  await f.runtime.decisions.answer(owner.id, owner.id, intrinsic.items[0].operation_id, [{ answer: ['B'], dismissed: false }], {}, intrinsic.items[0].request);
  await client.wait('hold-reload', deadline());
  await client.callTool(owner.id, { module: 'files', name: 'write', arguments_base64: Buffer.from(JSON.stringify({ path: 'denied-mobile.txt', content: 'must not write' })).toString('base64') }, 'denied-write', deadline());
  expect((await client.wait('denied-write', deadline())).turn?.state).toBe('failed');
  expect(existsSync(join(directory, 'denied-mobile.txt'))).toBe(false);
  expect((await client.session(owner.id).permissions.list({ pending_only: true, limit: 16 }, deadline())).items).toHaveLength(0);
  await f.runtime.run('permissions.set_mode', { session_id: owner.id, edit_id: 'full-mobile', expected_revision: denied.revision, mode: 'automatic' }, { rootId: owner.id });
  const automatic = await client.session(owner.id).permissions.policy(deadline()); expect(automatic.deny_interactive).toBe(true);
  await f.runtime.run('permissions.set_denial', { session_id: owner.id, edit_id: 'clear-mobile', expected_revision: automatic.revision, deny_interactive: false }, { rootId: owner.id });
  expect(await client.session(owner.id).permissions.policy(deadline())).toMatchObject({ mode: 'automatic', deny_interactive: false });
  const before = await client.session(owner.id).get(deadline());
  const history = await client.call('sessions.history_page', { session_id: owner.id, direction: 'forward', limit: 20 }, deadline());
  await f.runtime.run('sessions.reload', { session_id: owner.id, edit_id: 'apply-mobile', expected_revision: before.config_revision }, { rootId: owner.id });
  const applied = await until(() => client.session(owner.id).reloads.get('apply-mobile', deadline()), value => value.state !== 'pending', 'captured settings applied');
  expect(applied.state).toBe('applied'); expect(applied.revision).toBe(String(BigInt(before.config_revision) + 1n));
  const after = await client.session(owner.id).get(deadline()); expect(after.configuration.model).toEqual(before.configuration.model); expect(after.configuration.automatic_title).toBe(false);
  expect((await client.call('sessions.history_page', { session_id: owner.id, direction: 'forward', limit: 20 }, deadline())).messages).toEqual(history.messages);
  expect(f.requests.filter(method => method === 'permissions.set_denial')).toHaveLength(2);
  expect(f.requests.filter(method => method === 'sessions.reload')).toHaveLength(1);
  await f.runtime.dispose();
});

test('mobile host browser settings inspect lost CAS acknowledgment, persist across restart and respect process pins', async () => {
  let f = await mobile('browser-driver'), client = f.runtime.requireReady();
  const before = await client.hosts.browserDriver(deadline()), next = before.driver === 'rod' ? 'chromedp' : 'rod';
  expect(before).toMatchObject({ driver: before.configured_driver, pinned: false });
  f.lose('host.set_browser_driver');
  await expect(client.hosts.setBrowserDriver(before.revision, next, deadline())).rejects.toThrow('lost mobile ACK');
  expect(f.requests.filter(method => method === 'host.set_browser_driver')).toHaveLength(1);
  const saved = await client.hosts.browserDriver(deadline());
  expect(saved).toMatchObject({ configured_driver: next, driver: next, pinned: false });
  await expect(client.hosts.setBrowserDriver(before.revision, before.driver, deadline())).rejects.toMatchObject({ kind: 'CONFLICT' });
  expect(await client.hosts.browserDriver(deadline())).toEqual(saved);
  const oldEpoch = client.processEpoch; await f.runtime.dispose(); await stop(); ready = await start(false, before.driver);
  f = await mobile('browser-driver'); client = f.runtime.requireReady(); expect(client.processEpoch).not.toBe(oldEpoch);
  const pinned = await client.hosts.browserDriver(deadline());
  expect(pinned).toEqual({ ...saved, driver: before.driver, pinned: true });
  expect(f.requests).toEqual(['host.browser_driver']);
  await expect(client.hosts.setBrowserDriver(pinned.revision, next, deadline())).rejects.toMatchObject({ kind: 'INVALID' });
  const aligned = await client.hosts.setBrowserDriver(pinned.revision, before.driver, deadline());
  expect(aligned).toMatchObject({ configured_driver: before.driver, driver: before.driver, pinned: true });
  expect(f.requests.every(method => method === 'host.browser_driver' || method === 'host.set_browser_driver')).toBe(true);
});

test('mobile external Chrome controls use exact CAS and root generation without launch or replay across restart', async () => {
  let f = await mobile('external-browser'), client = f.runtime.requireReady(); const owner = await root(f.runtime, 'external-browser-root');
  const original = await client.hosts.externalBrowser(deadline());
  const configuration = { mode: 'headless' as const, executable: join(directory, 'never-launch-this'), live_endpoint: '', live_profile: '', allow_private_urls: false };
  f.lose('host.set_external_browser'); await expect(client.hosts.setExternalBrowser(original.revision, configuration, deadline())).rejects.toThrow('lost mobile ACK');
  const saved = await client.hosts.externalBrowser(deadline()); expect(saved.configuration).toEqual(configuration);
  expect(f.requests.filter(method => method === 'host.set_external_browser')).toHaveLength(1);
  await expect(client.hosts.setExternalBrowser(original.revision, original.configuration, deadline())).rejects.toMatchObject({ kind: 'CONFLICT' });
  expect(await client.hosts.externalBrowserSessions(owner.id, deadline())).toEqual({ items: [] });
  expect(existsSync(join(directory, 'host', 'browser'))).toBe(false);
  await client.callTool(owner.id, { module: 'browser', name: 'run', arguments_base64: Buffer.from(JSON.stringify({ session: 'default', code: 'info()' })).toString('base64') }, 'external-browser-held', deadline());
  const pending = await until(() => client.session(owner.id).permissions.list({ pending_only: true }, deadline()), value => value.items?.length === 1, 'external browser permission');
  const permission = pending.items?.[0]; if (!permission) throw new Error('Browser permission missing');
  expect((await client.session(owner.id).operations.get(permission.operation_id, deadline())).capability).toBe('browser.external');
  const captured = (await client.hosts.externalBrowserSessions(owner.id, deadline())).items[0]; if (!captured) throw new Error('Prepared browser resource missing'); expect(captured).toMatchObject({ root_id: owner.id, state: 'prepared', mode: 'headless' });
  f.lose('browser.reconnect_external'); await expect(client.hosts.reconnectExternalBrowser(owner.id, captured.name, captured.generation, deadline())).rejects.toThrow('lost mobile ACK');
  const fresh = (await client.hosts.externalBrowserSessions(owner.id, deadline())).items[0]; if (!fresh) throw new Error('Fresh browser resource missing'); expect(fresh.generation).not.toBe(captured.generation); expect(fresh.resource).not.toBe(captured.resource); expect(fresh.state).toBe('prepared');
  expect((await client.wait('external-browser-held', deadline())).turn?.state).not.toBe('succeeded'); expect(f.requests.filter(method => method === 'browser.reconnect_external')).toHaveLength(1);
  expect(existsSync(join(directory, 'host', 'browser'))).toBe(false);
  expect((await client.hosts.disconnectExternalBrowser(owner.id, fresh.name, fresh.generation, deadline())).state).toBe('ended');
  const previousEpoch = client.processEpoch; await f.runtime.dispose(); await stop(); ready = await start();
  f = await mobile('external-browser'); client = f.runtime.requireReady(); expect(client.processEpoch).not.toBe(previousEpoch);
  expect((await client.hosts.externalBrowser(deadline())).configuration).toEqual(configuration); expect(await client.hosts.externalBrowserSessions(owner.id, deadline())).toEqual({ items: [] });
  expect(f.requests).toEqual(['host.external_browser', 'browser.external_sessions']); expect(existsSync(join(directory, 'host', 'browser'))).toBe(false);
  await f.runtime.dispose();
});
