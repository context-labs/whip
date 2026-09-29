import assert from 'node:assert/strict';
import { test } from 'node:test';
import { join } from 'node:path';
import { realpath } from 'node:fs/promises';
import { DurableCommand } from '../../../packages/sdk/dist/index.js';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

test('production web fixture executes both engines, scopes consent and preserves crash evidence', { timeout: 120000 }, async () => {
  const rejectionMessage = 'Private synthetic provider detail '.repeat(1500);
  const fixture = await startFixture({ executeCode: true, rejectInput: 'fixture-provider-rejection', rejectionMessage });
  try {
    let client = await fixture.connect('native-fixture-check');
    await assert.rejects(client.listTerminals(deadline()), error => error.kind === 'NETWORK_RESTRICTED');
    const { root } = await fixture.createRoot(client), session = client.session(root.id);
    assert.equal((await client.hostDirectories({ path: '~', after: '', prefix: '', show_hidden: false, limit: 16 }, deadline())).path, join(fixture.directory, 'home'));
    await assert.rejects(client.call('host.directory.pick', { start: fixture.directory }, deadline()), /unavailable/);
    for (const scope of [null, root.id]) {
      const discovery = await client.mcpImportCandidates(scope, deadline());
      assert.deepEqual(discovery.candidates, [], 'Disposable runtime discovered external MCP configuration');
      assert.deepEqual(discovery.source_errors, {});
    }
    for (const [engine, code] of [['starlark', 'print("native starlark")'], ['quickjs', 'console.log("native quickjs")']]) {
      const { root: codeRoot } = await fixture.createRoot(client, { engine });
      const codeSession = client.session(codeRoot.id), requestID = 'code-' + engine;
      await codeSession.submit([{ type: 'text', text: '```' + (engine === 'quickjs' ? 'javascript' : 'starlark') + '\n' + code + '\n```' }], requestID, deadline());
      assert.equal((await client.wait(requestID, deadline())).turn.state, 'succeeded');
      const history = await codeSession.history.page({ direction: 'forward' }, deadline());
      const result = history.messages.flatMap(message => message.parts).find(part => part.type === 'tool_result');
      assert.equal(JSON.parse(result.result.output).result.output, 'native ' + engine + '\n');
    }
    await session.submit([{ type: 'text', text: 'fixture-provider-rejection' }], 'rejected', deadline());
    const rejected = (await client.wait('rejected', deadline())).turn;
    assert.equal(rejected.state, 'failed');
    assert.match(rejected.failure, /provider returned HTTP 400/);
    assert.ok(!rejected.failure.includes('Private synthetic provider detail'));
    assert.ok(Buffer.byteLength(rejected.failure) < 256);
    await session.submit([{ type: 'text', text: 'Follow up after rejected provider response' }], 'after-rejection', deadline());
    assert.equal((await client.wait('after-rejection', deadline())).turn.state, 'succeeded');
    assert.equal((await fixture.effects()).filter(text => text === 'fixture-provider-rejection').length, 1);
    const thinking = session.submission([{ type: 'text', text: 'hold:thinking-response' }], 'thinking');
    await thinking.send(deadline());
    await eventually(async () => (await fixture.effects()).includes('hold:thinking-response'));
    assert.equal((await client.call('sessions.observe', { session_id: session.id, after: '0', limit: 100 }, deadline())).preview?.text ?? '', '');
    fixture.release('thinking-first-token');
    await eventually(async () => (await client.call('sessions.observe', { session_id: session.id, after: '0', limit: 100 }, deadline())).preview?.text === 'hold:thinking-response');
    fixture.release('thinking-response'); assert.equal((await thinking.wait(deadline())).turn.state, 'succeeded');
    const permission = session.submission([{ type: 'text', text: 'permission:fixture' }], 'permission'); await permission.send(deadline());
    const pending = await eventually(async () => (await session.permissions.list({ pending_only: true }, deadline())).items?.[0], { description: 'actual pending file permission' });
    const { root: other } = await fixture.createRoot(client);
    await assert.rejects(client.session(other.id).operations.get(pending.operation_id, deadline()), /another session/);
    await session.permissions.resolve(pending.operation_id, true, deadline()); assert.equal((await permission.wait(deadline())).turn.state, 'succeeded');
    const crashed = session.submission([{ type: 'text', text: 'hold:crash-native' }], 'crash'); await crashed.send(deadline());
    await eventually(async () => (await fixture.effects()).filter(text => text === 'hold:crash-native').length === 1, { description: 'one external request before crash' });
    const oldEpoch = client.processEpoch; await fixture.crashAndRestart();
    await assert.rejects(client.session(root.id).get(deadline()), error => error.kind === 'IDENTITY');
    client = await fixture.connect(client.clientID); assert.notEqual(client.processEpoch, oldEpoch);
    const recovered = DurableCommand.recover(client, crashed.record);
    assert.equal((await recovered.check(deadline())).state, 'found'); assert.equal((await recovered.wait(deadline())).turn.state, 'interrupted');
    assert.equal((await fixture.effects()).filter(text => text === 'hold:crash-native').length, 1);
    const response = await fetch(fixture.info.web + '/h/' + client.runtimeID + '/s/' + root.id);
    assert.equal(response.status, 200); assert.match(await response.text(), /<html/);
    const policy = response.headers.get('content-security-policy'); assert.ok(policy && !policy.includes("'unsafe-eval'") && !policy.includes("'unsafe-inline'"));
  } finally { await fixture.close(); }
});

test('opt-in agent responses use real execution evidence and explicit structured final blocks', { timeout: 120000 }, async () => {
  const fixture = await startFixture({ executeCode: true, agentResponses: true });
  try {
    const client = await fixture.connect('agent-response-check');
    const { root } = await fixture.createRoot(client, { overrides: { output: { schema: { type: 'object', properties: { answer: { type: 'string' } }, required: ['answer'], additionalProperties: false } } } });
    const session = client.session(root.id), key = 'agent-structured';
    const run = session.submission([{ type: 'text', text: `hold:${key}
\`\`\`starlark
print("executed")
\`\`\`
\`\`\`final
{"answer":"captured"}
\`\`\`` }], 'structured');
    await run.send(deadline());
    await eventually(async () => (await session.history.page({ direction: 'forward' }, deadline())).messages.some(message => message.parts.some(part => part.type === 'tool_result')), { description: 'real committed execution before held final' });
    fixture.release(key);
    const settled = await run.wait(deadline()); assert.equal(settled.turn.state, 'succeeded');
    const output = await session.turns.output(settled.turn.id, deadline());
    assert.deepEqual(JSON.parse(Buffer.from(output.output.data_base64, 'base64')), { answer: 'captured' });
    const { root: plain } = await fixture.createRoot(client);
    await client.session(plain.id).submit([{ type: 'text', text: `\`\`\`starlark
print("evidence")
\`\`\`` }], 'plain', deadline());
    assert.equal((await client.wait('plain', deadline())).turn.state, 'succeeded');
    const history = await client.session(plain.id).history.page({ direction: 'forward' }, deadline());
    assert.ok(history.messages.some(message => message.parts.some(part => part.type === 'text' && part.text.startsWith('done: ') && part.text.includes('evidence'))));
  } finally { await fixture.close(); }
});

test('fixture rejection body rejects oversized and invalid options before owning a process', async () => {
  await assert.rejects(startFixture({ rejectionMessage: '界'.repeat(22000) }), /64KiB/);
  await assert.rejects(startFixture({ rejectionMessage: null }), /64KiB/);
});


test('terminal fixture opt-in preserves the native network guard and exact ephemeral owner', { timeout: 120000 }, async () => {
  await assert.rejects(startFixture({ networkTerminals: 'true' }), /must be a boolean/);
  const fixture = await startFixture({ networkTerminals: true });
  try {
    const client = await fixture.connect('native-terminal-check');
    const shell = await client.openTerminal({ cwd: fixture.directory, cols: 80, rows: 24 }, deadline());
    const ref = { id: shell.id, process_epoch: shell.process_epoch };
    assert.equal(shell.cwd, await realpath(fixture.directory)); assert.equal(shell.process_epoch, client.processEpoch);
    await client.writeTerminal(ref, new TextEncoder().encode("printf 'terminal-%s\\n' native-check\n"), deadline());
    const read = await eventually(async () => {
      const page = await client.readTerminal(ref, '0', 32768, deadline());
      return Buffer.from(page.data_base64, 'base64').toString().includes('terminal-native-check') && page;
    }, { description: 'real terminal output through explicit network opt-in' });
    assert.equal(BigInt(read.next) - BigInt(read.from), BigInt(Buffer.from(read.data_base64, 'base64').length));
    const other = await fixture.connect('independent-terminal-reader');
    const replay = await other.readTerminal(ref, '0', 32768, deadline());
    assert.ok(Buffer.from(replay.data_base64, 'base64').toString().includes('terminal-native-check'));
    assert.equal((await client.listTerminals(deadline())).items.length, 1);
    await client.closeTerminal(ref, deadline());
    await assert.rejects(other.readTerminal(ref, '0', 32768, deadline()), error => error.kind === 'NOT_FOUND');
  } finally { await fixture.close(); }
});
