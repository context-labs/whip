// Actual native runtime/provider/engine/executor acceptance. Callback effects,
// authority and canonical records are inspected directly; there is no fake SDK.
import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createIncidentCommander } from './dist/incident-commander.js';
import { start, serve, create, runCell, cell, deadline, eventually, grant } from './acceptance.mjs';
let fixture, client, runtime, investigator, scribe, session, commander;
const progress = [];
before(async () => { ({ fixture, client } = await start()); });
after(async () => { await runtime?.close(); await investigator?.close(); await scribe?.close(); await fixture?.close(); });
const succeeded = run => { assert.equal(run.result.admission.turn.state, 'succeeded'); return run; };
const value = (run, capability) => { const operation = run.operations.find(op => op.capability === capability); assert.equal(operation?.state, 'succeeded', JSON.stringify(operation)); return operation.result.value; };

test('serve registers immutable definitions and binds separate root and narrowed child executors', async () => {
  commander = createIncidentCommander({ id: 'incident-commander-acceptance', model: { name: 'model', provider: 'provider', effort: '' } });
  investigator = await serve(client, fixture, commander.investigator, progress);
  scribe = await serve(client, fixture, commander.scribe, progress);
  const definition = commander.agent({ investigator: investigator.definition, scribe: scribe.definition });
  runtime = await serve(client, fixture, definition, progress);
  const record = await client.agents.get(runtime.definition, deadline());
  assert.deepEqual(record.ref, runtime.definition); assert.deepEqual(record.document, definition.document);
  const listed = await client.agents.list({ limit: 100 }, deadline());
  assert.ok(listed.items.some(item => item.ref.id === runtime.definition.id && item.ref.revision === runtime.definition.revision));
  assert.ok(client.builtins.some(item => item.id === 'coding'));
});

test('root pins its exact definition and model defaults; policy authority is changed separately', async () => {
  session = await create(runtime, fixture);
  const record = await session.get(deadline());
  assert.deepEqual(record.definition, runtime.definition); assert.equal(record.configuration.model.name, 'model');
  assert.equal(record.configuration.model.provider, 'provider');
  assert.equal((await runtime.sessions.open(session.id, deadline())).id, session.id);
  assert.equal((await session.permissions.policy(deadline())).mode, 'automatic');
});

test('typed callback, turn-start context, hook and operation have the same canonical identities', async () => {
  const run = succeeded(await runCell(fixture, session, 'print(tools.search_incidents(query="auth", status="open"))'));
  assert.deepEqual(value(run, 'tools.search_incidents'), [{ id: 'INC-101', service: 'auth', title: 'Login page times out on mobile', status: 'open', severity: 2 }]);
  const call = commander.state.toolCalls.find(item => item.tool === 'search_incidents');
  assert.equal(call.sessionID, session.id); assert.equal(call.turnID, run.result.admission.turn.id); assert(call.invocationID);
  assert.ok(commander.state.turnStarts.some(item => item.turnID === call.turnID && item.sessionID === session.id && item.input.includes('search_incidents')));
  assert.ok(commander.state.auditLog.some(item => item.operation === 'tools.search_incidents' && item.arguments.query === 'auth'));
});

test('before_tool denial runs no destructive shell operation', async () => {
  const run = succeeded(await runCell(fixture, session, 'shell.run(command="rm -rf build")'));
  assert.match(run.toolResult.result.output, /destructive shell commands are not allowed during incidents/);
  assert.ok(run.activity.decisions.some(item => item.hook === 'before_tool' && item.decision === 'deny'));
  assert.equal(run.operations.some(item => item.capability === 'shell.run'), false);
});

test('before_tool rewrites the path before the actual host file is read', async () => {
  await writeFile(join(fixture.directory, '.env'), 'SECRET=synthetic-original\n');
  await writeFile(join(fixture.directory, '.env.example'), 'SECRET=example\n');
  const run = succeeded(await runCell(fixture, session, 'print(files.read(path=".env"))'));
  assert.match(run.toolResult.result.output, /SECRET=example/); assert.doesNotMatch(run.toolResult.result.output, /synthetic-original/);
  assert.ok(run.activity.decisions.some(item => item.decision === 'rewrite' && item.reason === 'secrets are redacted'));
});

test('a large typed result is explicitly retained and read through owner-scoped bounded artifact slices', async () => {
  const run = succeeded(await runCell(fixture, session, 'book=tools.fetch_runbook(service="auth")\nref=artifacts.put(text=book["runbook"],source="commander")\nprint(artifacts.read(id=ref["id"],offset="0",length=64))'));
  assert.ok(value(run, 'tools.fetch_runbook').runbook.length > 8192);
  const stored = value(run, 'artifacts.put'); assert(stored.id);
  const page = value(run, 'artifacts.read'); assert.match(Buffer.from(page.data, 'base64').toString(), /RUNBOOK AUTH/); assert(Buffer.from(page.data, 'base64').length <= 64);
  const stranger = await fixture.createRoot(client);
  await assert.rejects(client.session(stranger.root.id).content.get(stored.id, deadline()), /not found/);
});

test('progress is observable during the real callback and rewritten arguments reach it', async () => {
  const run = succeeded(await runCell(fixture, session, `print(tools.page_oncall(team="auth",message="${'x'.repeat(300)}"))`));
  assert.equal(value(run, 'tools.page_oncall').acknowledgedBy, 'Sam');
  const page = [...commander.state.pages.values()][0]; assert.equal(page.message.length, 200); assert.match(page.message, /\.\.\.$/);
  assert.deepEqual(progress.map(item => item.text), ['paging auth on-call', 'page acknowledged by Sam']);
});

test('state, artifacts and mailbox reads execute beside the custom tools', async () => {
  const run = succeeded(await runCell(fixture, session, 'state.write(scope="session",key="incident",expected_revision="0",value={"id":"INC-101","owner":"auth"})\nstored=state.get(scope="session",key="incident")\nnote=artifacts.put(text="Timeline opened for INC-101",source="commander")\ninbox=mail.list()\nprint(stored,note,inbox)'));
  assert.match(run.toolResult.result.output, /INC-101/); assert(value(run, 'artifacts.put').id);
  assert.ok(run.operations.some(item => item.capability === 'mail.list' && item.state === 'succeeded'));
});

test('before_spawn redirects an unnamed child to an immutable investigator with only delegated tool authority', async () => {
  const toolGrant = await grant(client, session.id, 'tools.search_incidents', `${investigator.definition.id}@${investigator.definition.revision}`);
  const task = 'Investigate INC-101.\n' + cell('print(tools.search_incidents(query="INC-101"))');
  const run = succeeded(await runCell(fixture, session, `print(agents.spawn(prompt=${JSON.stringify(task)},grant_ids=[${JSON.stringify(toolGrant)}],budgets=[{"kind":"model_calls","limit":"8"}]))`));
  assert.doesNotMatch(run.toolResult.result.output, /\"error\"/, run.toolResult.result.output);
  assert.ok(run.activity.decisions.some(item => item.hook === 'before_spawn' && item.decision === 'rewrite' && item.reason === 'unnamed children investigate'));
  const child = await eventually(async () => (await client.call('sessions.list', { tree_id: (await session.get(deadline())).tree_id, limit: 100 }, deadline())).items.find(item => item.parent_id === session.id));
  assert.deepEqual(child.definition, investigator.definition); assert.equal(child.parent_id, session.id);
  assert.equal(child.configuration.report_mode, 'message'); assert(!child.configuration.modules.includes('shell'));
  const childSession = await investigator.sessions.open(child.id, deadline());
  const childTurn = await eventually(async () => (await childSession.turns.page({}, deadline())).items[0]);
  await eventually(async () => !!(await childSession.turns.get(childTurn.id, deadline())).finished_at);
  assert.equal((await childSession.turns.get(childTurn.id, deadline())).state, 'succeeded', JSON.stringify(await childSession.turns.get(childTurn.id, deadline())));
  const call = await eventually(() => commander.state.toolCalls.find(item => item.sessionID === child.id));
  assert.equal(call.tool, 'search_incidents');
  await eventually(async () => (await childSession.turns.get(call.turnID, deadline())).state === 'succeeded');
  assert.ok((await childSession.turns.operations(call.turnID, {}, deadline())).items.some(item => item.grant_id && item.capability === 'tools.search_incidents'));
  assert.ok(commander.state.turnStarts.some(item => item.sessionID === child.id && item.input.includes('Investigate INC-101')));

});

test('before_spawn denies a shell child without admitting it', async () => {
  const before = await client.call('sessions.list', { tree_id: (await session.get(deadline())).tree_id, limit: 100 }, deadline());
  const run = succeeded(await runCell(fixture, session, 'agents.spawn(prompt="ops work",overrides={"modules":["shell"]})'));
  assert.match(run.toolResult.result.output, /children may not hold shell/);
  const after = await client.call('sessions.list', { tree_id: (await session.get(deadline())).tree_id, limit: 100 }, deadline());
  assert.deepEqual(after.items.map(item => item.id), before.items.map(item => item.id));
});

test('a closed executor produces a bounded cell error without dispatching or replaying callbacks', async () => {
  await runtime.close(); const count = commander.state.toolCalls.length, started = Date.now();
  const result = await session.run([{ type: 'text', text: cell('tools.search_incidents(query="auth")') }], crypto.randomUUID()).result(deadline());
  assert.equal(result.admission.turn.state, 'succeeded');
  const history = await session.history.page({ direction: 'backward' }, deadline());
  assert.ok(history.messages.filter(item => item.turn_id === result.admission.turn.id).some(item => item.parts.some(part => part.type === 'tool_result' && /executor.*unavailable/.test(part.result.output))));
  // Two absent hooks each exhaust the native five-second bind grace, never the
  // longer tool timeout; the fixture provider may still finish after a cell error.
  assert(Date.now() - started < 15000); assert.equal(commander.state.toolCalls.length, count);
  assert.equal((await session.turns.operations(result.admission.turn.id, {}, deadline())).items.length, 0);
});
