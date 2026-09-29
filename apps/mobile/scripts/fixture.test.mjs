import assert from 'node:assert/strict';
import { request } from 'node:http';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { access } from 'node:fs/promises';
import { test } from 'node:test';
import { deadline, eventually, fixtureExternalOrigin, startFixture } from '../../web/scripts/native-fixture.mjs';

function probe(endpoint, headers) {
  return new Promise((resolve, reject) => {
    const req = request(new URL('/api/v4/web', endpoint), { headers, signal: AbortSignal.timeout(5000) }, response => {
      response.resume(); response.once('end', () => resolve(response.statusCode));
    });
    req.once('error', reject); req.end();
  });
}

test('manual mobile fixture allows exactly its HTTPS proxy and retains real question workflows', { timeout: 120000 }, async () => {
  for (const value of ['http://phone.test', 'https://phone.test/', 'https://a:b@phone.test', 'https://*.test', 'https://phone.test?q=1', 'https://phone.test#x']) {
    assert.throws(() => fixtureExternalOrigin(value), /exact HTTPS origin/);
  }
  assert.equal(fixtureExternalOrigin(undefined), undefined);
  const fixture = await startFixture({ externalOrigin: 'https://phone.test:8443', agentResponses: true });
  try {
    assert.equal(await probe(fixture.info.web, { host: 'phone.test:8443', origin: 'https://phone.test:8443' }), 200);
    assert.equal(await probe(fixture.info.web, { host: 'foreign.test:8443' }), 403);
    assert.equal(await probe(fixture.info.web, { origin: 'https://foreign.test:8443' }), 403);
    const client = await fixture.connect('manual-phone'), other = await fixture.connect('manual-web');
    const { root } = await fixture.createRoot(client), session = client.session(root.id);
    for (const kind of ['single', 'batch']) {
      const command = session.submission([{ type: 'text', text: 'question:' + kind }], 'question-' + kind);
      await command.send(deadline());
      const pending = await eventually(async () => (await session.questions.list({ pending_only: true }, deadline())).items?.[0]);
      assert.equal(pending.request.questions.length, kind === 'batch' ? 3 : 1);
      const answers = [{ answer: ['Proceed'], dismissed: false }];
      if (kind === 'batch') answers.push({ answer: ['Web', 'Mobile', 'custom text'], dismissed: false }, { answer: [], dismissed: true });
      await other.session(root.id).questions.answer(pending.operation_id, answers, deadline());
      assert.equal((await command.wait(deadline())).turn.state, 'succeeded');
      assert.deepEqual((await session.questions.list({ pending_only: true }, deadline())).items, []);
      const history = await session.history.page({ direction: 'forward' }, deadline());
      assert.ok(history.messages.some(message => message.parts.some(part => part.type === 'text' && part.text.includes('Proceed'))));
    }
    const exited = fixture.exited;
    await fixture.close(); assert.ok(Array.isArray(await exited));
  } finally { await fixture.close(); }
});

test('manual mobile CLI prints native identity and removes its own fixture on SIGTERM', { timeout: 120000 }, async () => {
  const child = spawn(process.execPath, ['apps/mobile/scripts/fixture.mjs', '--minutes=1'], { stdio: ['ignore', 'pipe', 'pipe'] });
  let output = '', error = '';
  child.stdout.on('data', data => { output = (output + data).slice(-(1 << 20)); });
  child.stderr.on('data', data => { error = (error + data).slice(-(1 << 20)); });
  const ended = once(child, 'exit');
  try {
    await eventually(() => output.includes('Fixture expires within') || child.exitCode !== null || child.signalCode !== null, { timeout: 90000, description: 'manual fixture ready' });
    assert.ok(output.includes('Fixture expires within'), error);
    const directory = output.match(/Fixture cwd: (.+)/)?.[1];
    assert.ok(directory?.startsWith('/tmp/whip-web-native-'));
    assert.match(output, /Web conversation: http:\/\/127\.0\.0\.1:\d+\/h\/[^/\n]+\/s\/[^\n]+/);
    child.kill('SIGTERM');
    assert.deepEqual(await ended, [0, null], error);
    await assert.rejects(access(directory), { code: 'ENOENT' });
  } finally {
    if (child.exitCode === null && child.signalCode === null) { child.kill('SIGKILL'); await ended; }
  }
});
