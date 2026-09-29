// Shared test composition only: production runtime, local synthetic provider,
// real executor peers, canonical reads. No scripted client or event reducer.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { executorSocket } from '@whip/sdk/node';
import { deadline, eventually, startFixture } from '../../apps/web/scripts/native-fixture.mjs';
export { deadline, eventually };
export const cell = code => ['```starlark', code, '```'].join('\n');
export const final = value => ['```final', JSON.stringify(value), '```'].join('\n');
export async function start() {
  const fixture = await startFixture({ executeCode: true, agentResponses: true, lifetimeMs: 900000 });
  return { fixture, client: await fixture.connect('agents-' + randomUUID()) };
}
export async function serve(client, fixture, definition, progress = []) {
  // Observe the public read immediately after each real progress ACK, while the
  // callback is still active. Progress is ephemeral, never a replayable event log.
  const handlers = Object.fromEntries(Object.entries(definition.handlers).map(([name, handler]) => [name, { ...handler,
    execute: (input, context) => handler.execute(input, { ...context, progress: async text => {
      await context.progress(text);
      const activity = await client.executorActivity(context.sessionID, { signal: context.signal });
      assert.equal(activity.activity?.progress?.text, text);
      assert.equal(activity.activity.progress.operation_id, context.operationID);
      progress.push({ text, sessionID: context.sessionID, operationID: context.operationID });
      assert(progress.length <= 128, 'Fixture progress evidence stays bounded');
    } }),
  }]));
  return client.agents.serve({ ...definition, handlers }, { transport: await executorSocket(fixture.info.socket) });
}
export async function create(runtime, fixture) {
  const session = await runtime.sessions.create({ creation_id: randomUUID(), engine: 'starlark', working_directory: fixture.directory, metadata: { title: null, pinned: false, archived: false },
    overrides: { automatic_title: false } }, deadline());
  const policy = await session.permissions.policy(deadline());
  await session.permissions.setMode({ mode: 'automatic', expected_revision: policy.revision }, randomUUID(), deadline());
  return session;
}
export async function grant(client, sessionID, capability, resource) {
  const id = randomUUID(); await client.call('grants.create', { id, session_id: sessionID, capability, resource }, deadline()); return id;
}
export async function runCell(fixture, session, code, output) {
  const id = randomUUID(), hold = 'agent-' + id;
  const text = `hold:${hold}\n` + cell(code) + (output === undefined ? '' : '\n' + final(output));
  const run = session.run([{ type: 'text', text }], id);
  await run.send(deadline());
  const admitted = await eventually(async () => { const value = await session.client.recover(id, deadline()); return value.turn ? value : false; });
  await eventually(async () => (await session.turns.cells(admitted.turn.id, {}, deadline())).items.some(cell => cell.result_message_id), { description: 'actual cell result before final response' });
  const activity = (await session.client.executorActivity(session.id, deadline())).activity;
  fixture.release(hold);
  const result = await run.result(deadline());
  const history = await session.history.page({ direction: 'backward', limit: 100 }, deadline());
  const messages = history.messages.filter(message => message.turn_id === admitted.turn.id);
  const toolResult = messages.flatMap(message => message.parts).find(part => part.type === 'tool_result');
  assert(toolResult, 'canonical execution result exists');
  const operations = (await session.turns.operations(admitted.turn.id, {}, deadline())).items;
  return { result, activity, operations, toolResult, messages };
}
