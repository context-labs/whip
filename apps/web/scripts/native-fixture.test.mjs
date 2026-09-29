import assert from 'node:assert/strict';
import { test } from 'node:test';
import { DurableCommand } from '../../../packages/sdk/dist/index.js';
import { deadline, eventually, startFixture } from './native-fixture.mjs';

test('production web fixture executes both engines, scopes consent and preserves crash evidence', { timeout: 120000 }, async () => {
  const fixture = await startFixture({ executeCode: true, rejectInput: 'fixture-provider-rejection' });
  try {
    let client = await fixture.connect('native-fixture-check');
    const { root } = await fixture.createRoot(client), session = client.session(root.id);
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
    assert.equal((await client.wait('rejected', deadline())).turn.state, 'failed');
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
