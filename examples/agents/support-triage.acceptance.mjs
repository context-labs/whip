import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { audit, supportTriage, supportResearcher } from './dist/support-triage.js';
import { juniorDeveloper } from './dist/junior-developer.js';
import { start, serve, create, runCell, cell, final, deadline, eventually, grant } from './acceptance.mjs';
let fixture, client, runtime, childRuntime, session;
const progress = [], triage = { ticket: '42', summary: 'Login page times out on mobile', escalatedTo: 'auth' };
before(async () => { ({ fixture, client } = await start()); });
after(async () => { await runtime?.close(); await childRuntime?.close(); await fixture?.close(); });

test('registered examples match public definitions and each served session pins an immutable revision', async () => {
  const builtin = client.builtins.find(ref => ref.id === 'junior-developer'); assert(builtin);
  const current = await client.agents.get(builtin, deadline());
  assert.deepEqual(juniorDeveloper.document.defaults, current.document.defaults);
  childRuntime = await serve(client, fixture, supportResearcher, progress);
  runtime = await serve(client, fixture, supportTriage(childRuntime.definition), progress);
  session = await create(runtime, fixture);
  assert.deepEqual((await session.get(deadline())).definition, runtime.definition);
  assert.equal((await runtime.sessions.open(session.id, deadline())).id, session.id);
  await assert.rejects(runtime.sessions.open('missing-root', deadline()), /not found/);
});

test('real typed tool, progress, hook and structured output share the captured turn', async () => {
  const run = await runCell(fixture, session, 'print(tools.lookup_ticket(id="42"))', triage);
  assert.equal(run.result.admission.turn.state, 'succeeded'); assert.deepEqual(run.result.output, triage);
  assert.ok(run.operations.some(operation => operation.capability === 'tools.lookup_ticket' && operation.state === 'succeeded'));
  assert.ok(progress.some(entry => entry.text === 'looking up 42' && entry.sessionID === session.id));
  assert.ok(audit.some(entry => entry.operation === 'tools.lookup_ticket'));
});

test('a hook denial runs no shell operation and is visible in bounded live decisions', async () => {
  const run = await runCell(fixture, session, 'shell.run(command="rm -rf build")', triage);
  assert.equal(run.result.admission.turn.state, 'succeeded');
  assert.match(run.toolResult.result.output, /destructive shell commands are not allowed/);
  assert.ok(run.activity.decisions.some(decision => decision.hook === 'before_tool' && decision.decision === 'deny'));
  assert.equal(run.operations.some(operation => operation.capability === 'shell.run'), false);
});

test('a real root question accepts the exact human answer without an extra permission dialog', async () => {
  const id = crypto.randomUUID();
  const run = session.run([{ type: 'text', text: cell('print(user.ask(question="Escalate to auth?", options=[{"label":"Yes"},{"label":"No"}]))') + '\n' + final(triage) }], id);
  await run.send(deadline());
  const question = await eventually(async () => (await session.questions.list({ pending_only: true }, deadline())).items[0]);
  assert.equal((await session.permissions.list({ pending_only: true }, deadline())).items.length, 0);
  await session.questions.answer(question.operation_id, [{ answer: ['Yes'], dismissed: false }], deadline());
  assert.equal((await run.result(deadline())).admission.turn.state, 'succeeded');
  assert.equal((await session.questions.get(question.operation_id, deadline())).state, 'answered');
});

test('a narrowed registered child clears inherited structured output and uses only delegated tool authority', async () => {
  const toolGrant = await grant(client, session.id, 'tools.lookup_ticket', `${childRuntime.definition.id}@${childRuntime.definition.revision}`);
  const spawned = await session.spawn({ definition: childRuntime.definition, overrides: {}, grant_ids: [toolGrant], budgets: [{ kind: 'model_calls', limit: '8' }],
    parts: [{ type: 'text', text: cell('print(tools.lookup_ticket(id="42"))') }] }, crypto.randomUUID(), deadline());
  assert(spawned.session); assert.equal(spawned.session.configuration.output_schema, null);
  assert.deepEqual(Object.keys(spawned.session.configuration.tools), ['lookup_ticket']);
  const child = await childRuntime.sessions.open(spawned.session.id, deadline());
  const result = await client.wait(spawned.admission.receipt.identity.request_id, deadline());
  assert.equal(result.turn.state, 'succeeded', JSON.stringify(result.turn));
  const operations = (await child.turns.operations(result.turn.id, {}, deadline())).items;
  assert.ok(operations.some(operation => operation.capability === 'tools.lookup_ticket' && operation.grant_id));
});

test('invalid structured output fails after the actual correction budget', async () => {
  const run = session.run([{ type: 'text', text: final({ ticket: 42 }) }], crypto.randomUUID());
  const result = await run.result(deadline());
  assert.equal(result.admission.turn.state, 'failed'); assert.equal(result.output, null);
  assert.match(result.admission.turn.failure, /output|schema|contract/i);
});

test('closing the executor joins it and does not replay later callbacks', async () => {
  session = await create(runtime, fixture);
  const before = audit.length; await runtime.close();
  const started = Date.now();
  const run = session.run([{ type: 'text', text: cell('tools.lookup_ticket(id="42")') + '\n' + final(triage) }], crypto.randomUUID());
  const result = await run.result(deadline());
  assert.equal(result.admission.turn.state, 'succeeded');
  const history = await session.history.page({ direction: 'backward' }, deadline());
  assert.ok(history.messages.filter(item => item.turn_id === result.admission.turn.id).some(item => item.parts.some(part => part.type === 'tool_result' && /executor.*unavailable/.test(part.result.output))));
  assert(Date.now() - started < 15000); assert.equal(audit.length, before);
  assert.equal((await session.turns.operations(result.admission.turn.id, {}, deadline())).items.length, 0);
});
