// Live acceptance: the incident commander against a real daemon through the
// SDK's run layer. The fixture runs the recursive runtime behind a scripted
// model, so every prompt below that carries a ```cell block becomes one real
// rlm_exec call, and every host operation, hook decision, executor round trip,
// and child spawn is the production path. Run with:
// npm run acceptance -w @whip/agents-example
import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createWhipClient } from '@whip/sdk';
import { eventually, startFixture } from '@whip/sdk/testing/node';
import { createIncidentCommander } from './dist/incident-commander.js';

let fixture;
let client;
let runtime;
let session;
let state;
const events = [];
const cell = code => `Run this cell:\n\n\`\`\`cell\n${code}\n\`\`\``;
const decisions = () => events.filter(event => event.type === 'hook');

/** Runs one scripted cell as a root turn through session.run and returns the model's echo of the tool result. */
async function runCell(code) {
  const turn = session.run(cell(code), { signal: AbortSignal.timeout(30_000) });
  for await (const event of turn) events.push(event);
  const result = await turn.result();
  assert.equal(result.status, 'succeeded', JSON.stringify(result));
  assert.match(result.text, /^done: /, result.text);
  return result.text.slice('done: '.length);
}
/** The cell result payload the model saw; a failed cell surfaces its error text. */
function payload(text) {
  if (text.startsWith('Error: ')) assert.fail(`cell failed: ${text.split('\n')[0]}`);
  return JSON.parse(text);
}

before(async () => {
  fixture = await startFixture({ env: { WHIP_SDK_AGENTS_FIXTURE: '1' } });
  client = createWhipClient({ endpoint: fixture.info.endpoint, clientId: 'incident-commander-acceptance', clientKind: 'automation' });
  await client.connect();
});
after(async () => {
  client?.close();
  await fixture?.close();
});

test('serve registers the definition and binds this process as its executor', async () => {
  const commander = createIncidentCommander({ id: 'incident-commander-acceptance', model: { model: 'scripted-model', provider: 'fixture' } });
  state = commander.state;
  runtime = await client.agents.serve(commander.agent);
  assert.equal(runtime.definition, 'incident-commander-acceptance');
  assert.match(runtime.revision, /^[0-9a-f]{64}$/);
  const record = await client.agents.get(runtime.definition);
  assert.equal(record.revision, runtime.revision);
  assert.equal(record.built_in, false);
  assert.deepEqual(JSON.parse(JSON.stringify(record.definition)), JSON.parse(JSON.stringify(commander.agent.document)));
  const listed = await client.agents.list();
  assert.ok(listed.items.some(item => item.id === 'coding' && item.built_in));
  assert.ok(listed.items.some(item => item.id === runtime.definition && item.revision === runtime.revision));
});

test('the runtime creates a session pinned to its revision with the definition model defaults', async () => {
  session = await runtime.sessions.create({ cwd: fixture.directory });
  const snapshot = await session.snapshot();
  assert.equal(snapshot.meta.definition, runtime.definition);
  assert.equal(snapshot.meta.definition_revision, runtime.revision);
  assert.equal(snapshot.meta.model, 'scripted-model');
  assert.equal(snapshot.meta.provider, 'fixture');
  const opened = await runtime.sessions.open(session.rootId);
  assert.equal(opened.rootId, session.rootId);
  const mode = await session.setPermissionMode(false).result({ signal: AbortSignal.timeout(30_000) });
  assert.equal(mode.status, 'succeeded', JSON.stringify(mode.failure));
});

test('a custom tool call reaches this process with the turn identity and returns a value to the cell', async () => {
  const result = payload(await runCell('tools.search_incidents(query="auth", status="open")'));
  assert.deepEqual(result.value, [{ id: 'INC-101', service: 'auth', title: 'Login page times out on mobile', status: 'open', severity: 2 }]);
  const call = state.toolCalls.find(call => call.tool === 'search_incidents');
  assert.equal(call.rootId, session.rootId);
  assert.equal(call.agentId, session.rootId);
  assert.ok(call.turnId && call.invocationId);
  const turn = state.turnStarts.find(start => start.agentId === session.rootId);
  assert.ok(turn, 'turn_start ran for the root turn');
  assert.equal(turn.turnId, call.turnId);
  assert.match(turn.input, /^Run this cell/);
  assert.ok(state.auditLog.some(entry => entry.operation === 'tools.search_incidents' && entry.arguments.query === 'auth'), 'before_tool saw the custom tool call');
  assert.ok(events.some(event => event.type === 'host' && event.operation === 'tools.search_incidents' && event.status === 'completed'), 'the host call appeared as a turn event');
});

test('before_tool denies a destructive shell command before it runs', async () => {
  const text = await runCell('shell.run(command="rm -rf build")');
  assert.match(text, /hook before_tool denied shell\.run: destructive shell commands are not allowed during incidents/);
  const denial = decisions().find(decision => decision.decision === 'deny' && decision.operation === 'shell.run');
  assert.equal(denial?.hook, 'before_tool');
  assert.match(denial?.reason ?? '', /destructive/);
});

test('before_tool rewrites a secret read and the rewritten path is what the host reads', async () => {
  await writeFile(join(fixture.directory, '.env'), 'SECRET=real\n');
  await writeFile(join(fixture.directory, '.env.example'), 'SECRET=example\n');
  const text = await runCell('files.read(path=".env")');
  assert.match(text, /SECRET=example/);
  assert.doesNotMatch(text, /SECRET=real/);
  const rewrite = decisions().find(decision => decision.decision === 'rewrite' && decision.operation === 'files.read');
  assert.equal(rewrite?.reason, 'secrets are redacted');
});

test('a large tool result becomes a content handle the cell reads in bounded slices', async () => {
  const result = payload(await runCell('tools.fetch_runbook(service="auth")'));
  assert.ok(result.value.handle, 'runbook came back as a handle');
  assert.match(result.value.preview, /RUNBOOK AUTH/);
  assert.ok(result.value.size > 8192);
  const slice = payload(await runCell(`context.read(handle=${JSON.stringify(result.value.handle)}, offset=0, length=64)`));
  assert.match(JSON.stringify(slice.value), /RUNBOOK AUTH/);
});

test('tool progress streams into the turn and a rewritten argument reaches the handler', async () => {
  const long = 'x'.repeat(300);
  const result = payload(await runCell(`tools.page_oncall(team="auth", message="${long}")`));
  assert.equal(result.value.acknowledgedBy, 'Sam');
  assert.equal(state.pages.size, 1);
  const page = [...state.pages.values()][0];
  assert.equal(page.message.length, 200, 'before_tool shortened the page');
  assert.match(page.message, /\.\.\.$/);
  const progress = events.filter(event => event.type === 'progress' && event.operation === 'tools.page_oncall');
  assert.deepEqual(progress.map(event => event.text), ['paging auth on-call', 'page acknowledged by Sam']);
});

test('state, artifacts, and messages modules run beside the custom tools', async () => {
  const result = payload(await runCell([
    'state.private_set(key="incident", value={"id": "INC-101", "owner": "auth"})',
    'stored = state.private_get(key="incident")["value"]',
    'note = artifacts.put(text="Timeline opened for INC-101", source="commander")',
    'inbox = messages.list()',
    '{"incident": stored["id"], "owner": stored["owner"], "note": note, "inbox": inbox}',
  ].join('\n')));
  assert.equal(result.value.incident, 'INC-101');
  assert.equal(result.value.owner, 'auth');
  assert.ok(result.value.note, 'artifact was stored');
});

test('before_spawn routes an unnamed child to the investigator, which runs its own narrowed tools', async () => {
  const task = `Investigate INC-101.\n\n\`\`\`cell\ntools.search_incidents(query="INC-101")\n\`\`\``;
  const result = payload(await runCell(`agents.spawn(prompt=${JSON.stringify(task)}, name="scout", report="message")`));
  assert.equal(result.value.name, 'scout');
  assert.equal(result.value.report, 'message');
  const rewrite = decisions().find(decision => decision.hook === 'before_spawn' && decision.decision === 'rewrite');
  assert.equal(rewrite?.reason, 'unnamed children investigate');
  assert.ok(events.some(event => event.type === 'child' && event.childId === result.value.id && event.kind === 'agent.admitted'), 'the admission appeared as a child event');
  const childCall = await eventually(() => state.toolCalls.find(call => call.tool === 'search_incidents' && call.agentId !== session.rootId), { description: 'child tool call' });
  assert.equal(childCall.rootId, session.rootId);
  assert.equal(childCall.agentId, result.value.id);
  const childTurn = await eventually(() => state.turnStarts.find(start => start.agentId === result.value.id), { description: 'child turn_start' });
  assert.match(childTurn.input, /Investigate INC-101/);
  const listed = await session.agents.list();
  assert.match(JSON.stringify(listed.result), /"scout"/);
});

test('before_spawn denies a child that would hold shell', async () => {
  const text = await runCell('agents.spawn(prompt="ops work", name="ops", capabilities=["shell"])');
  assert.match(text, /hook before_spawn denied agents\.spawn: children may not hold shell/);
  assert.doesNotMatch(JSON.stringify((await session.agents.list()).result), /"ops"/);
});

test('a closed runtime fails later calls fast instead of waiting out the timeout', async () => {
  runtime.close();
  await runtime.done;
  const started = Date.now();
  const text = await runCell('tools.search_incidents(query="auth")');
  assert.match(text, /executor closed/);
  assert.ok(Date.now() - started < 10_000, 'the call did not wait for a timeout');
});
