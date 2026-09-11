// Live acceptance: the README program against a real daemon. The fixture runs
// the recursive runtime behind a scripted model: a prompt carrying a ```cell
// block becomes one real rlm_exec call, and a ```final block is the model's
// final message once the cell has run, so the output contract is exercised on
// the production path. Run with: npm run acceptance -w @whip/agents-example
import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { createWhipClient } from '@whip/sdk';
import { startFixture } from '@whip/sdk/testing/node';
import { audit, supportTriage } from './dist/support-triage.js';

let fixture;
let client;
let runtime;
let session;
const cell = code => `Run this cell:\n\n\`\`\`cell\n${code}\n\`\`\``;
const final = value => `\n\n\`\`\`final\n${JSON.stringify(value)}\n\`\`\``;
const triage = { ticket: '42', summary: 'Login page times out on mobile', escalatedTo: 'auth' };

/** Runs one turn and returns its events and typed result. */
async function run(input) {
  const turn = session.run(input, { signal: AbortSignal.timeout(30_000) });
  const events = [];
  for await (const event of turn) events.push(event);
  return { events, result: await turn.result() };
}

before(async () => {
  fixture = await startFixture({ env: { WHIP_SDK_AGENTS_FIXTURE: '1' } });
  client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: 'support-triage-acceptance', clientKind: 'automation' });
  await client.connect();
});
after(async () => {
  runtime?.close();
  client?.close();
  await fixture?.close();
});

test('serve returns a runtime whose sessions pin the served revision', async () => {
  runtime = await client.agents.serve(supportTriage);
  assert.equal(runtime.definition, 'support-triage');
  assert.match(runtime.revision, /^[0-9a-f]{64}$/);
  session = await runtime.sessions.create({ cwd: fixture.directory });
  const snapshot = await session.snapshot();
  assert.equal(snapshot.meta.definition, 'support-triage');
  assert.equal(snapshot.meta.definition_revision, runtime.revision);
  const mode = await session.setPermissionMode(false).result();
  assert.equal(mode.status, 'succeeded', JSON.stringify(mode.failure));
  await assert.rejects(runtime.sessions.open('missing-root'), /session|conflict/);
});

test('a turn calls the typed tool, streams its host call, and returns the output contract as a typed value', async () => {
  const { events, result } = await run(cell('tools.lookup_ticket(id="42")') + final(triage));
  assert.equal(result.status, 'succeeded', JSON.stringify(result));
  assert.deepEqual(result.output, triage);
  assert.equal(result.text, JSON.stringify(triage));
  const host = events.filter(event => event.type === 'host' && event.operation === 'tools.lookup_ticket');
  assert.deepEqual(host.map(event => event.status), ['running', 'completed']);
  assert.ok(events.some(event => event.type === 'progress' && event.text === 'looking up 42'), 'progress streamed into the turn');
  assert.ok(events.some(event => event.type === 'end' && event.status === 'succeeded'));
  assert.ok(audit.some(entry => entry.operation === 'tools.lookup_ticket'), 'before_tool observed the call');
});

test('a hook denial surfaces as a hook event and the turn still settles', async () => {
  const { events, result } = await run(cell('shell.run(command="rm -rf build")') + final(triage));
  assert.equal(result.status, 'succeeded', JSON.stringify(result));
  const denial = events.find(event => event.type === 'hook' && event.decision === 'deny');
  assert.ok(denial, 'the denial was observed');
  assert.equal(denial.hook, 'before_tool');
  assert.equal(denial.operation, 'shell.run');
  assert.equal(denial.reason, 'destructive shell commands are not allowed');
  const host = events.find(event => event.type === 'host' && event.operation === 'shell.run' && event.status === 'failed');
  assert.match(host?.error ?? '', /hook before_tool denied shell\.run/);
});

test('a final message that violates the output contract is corrected once, then fails the turn', async () => {
  const { result } = await run('Reply with the final block.' + final({ ticket: 42 }));
  assert.equal(result.status, 'failed');
  assert.match(result.failure.message, /output_invalid/);
});

test('a closed runtime fails later tool calls fast', async () => {
  runtime.close();
  await runtime.done;
  const started = Date.now();
  const { result } = await run(cell('tools.lookup_ticket(id="42")') + final(triage));
  assert.equal(result.status, 'succeeded', JSON.stringify(result));
  assert.ok(Date.now() - started < 15_000, 'the call did not wait out the timeout');
});
