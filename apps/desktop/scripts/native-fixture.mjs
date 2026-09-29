// Public native setup shared only by disposable desktop acceptance fixtures.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';

export async function configureFixtureModel(client, baseURL, provider, name, signal) {
  const inventory = await client.listProviders({ signal });
  const configured = await client.createProvider({ revision: inventory.revision, provider,
    declaration: { kind: 'openai-chat', base_url: baseURL,
      credential: { source: 'file', environment: '', file: '', command: null },
      models: { [name]: { prices: { input: '0', output: '0', reasoning: '0', cached_input: '0', cached_output: '0' },
        context_window_tokens: '65536', max_output_tokens: '256', timeout_millis: '30000', max_attempts: 1 } } },
    keep_credential: false, key: { id: randomUUID(), key: 'fixture-only' },
  }, { signal });
  await client.setProviderDefaults({ revision: configured.revision, defaults: { selection: { provider, name, effort: '' }, settings: null } }, { signal });
}

export async function createFixtureSession(client, cwd, provider, name, signal) {
  const definition = client.builtins.find(value => value.id === 'coding');
  assert(definition, 'The canonical coding definition must be advertised');
  const { root } = await client.trees.create({ definition, engine: 'starlark', working_directory: cwd,
    metadata: { title: null, pinned: false, archived: false }, overrides: { model: { provider, name, effort: '' }, automatic_title: false },
  }, randomUUID(), { signal });
  return client.session(root.id);
}

export async function verifyFixtureHistory(session, outcome, callID, expectedResult, answer, signal) {
  assert.equal(outcome.turn?.state, 'succeeded', outcome.turn?.failure);
  const history = await session.history.page({ direction: 'forward', limit: 100 }, { signal });
  assert.equal(history.next_cursor, null, 'Fixture history must fit its explicit page bound');
  const tool = history.messages.find(message => message.role === 'tool' && message.parts.some(part => part.type === 'tool_result' && part.result.call_id === callID));
  assert(tool, 'Canonical history must include the exact worker result');
  const result = tool.parts.find(part => part.type === 'tool_result' && part.result.call_id === callID).result;
  assert.equal(result.is_error, false); assert.deepEqual(JSON.parse(result.output), expectedResult);
  assert.equal(tool.turn_id, outcome.turn.id);
  const cells = await session.turns.cells(outcome.turn.id, { limit: 10 }, { signal });
  assert.equal(cells.items.length, 1);
  const cell = await session.cells.get(cells.items[0].id, { signal });
  assert.equal(cell.state, 'succeeded'); assert.equal(cell.turn_id, outcome.turn.id);
  assert.equal(cell.call_id, callID); assert.equal(cell.result_message_id, tool.id);
  assert(history.messages.some(message => message.role === 'assistant' && message.turn_id === outcome.turn.id && message.parts.some(part => part.type === 'text' && part.text === answer)));
  return cell;
}
