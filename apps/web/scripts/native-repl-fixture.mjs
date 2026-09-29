import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { defineAgent } from '../../../packages/sdk/dist/agents.js';
import { deadline, startFixture } from './native-fixture.mjs';

const fence = code => '```starlark\n' + code + '\n```';
export const recordedReplPrompt = (label, index) => {
  const title = `${label} cell ${String(index).padStart(3, '0')}`;
  const code = `# ${title}\nvalues = [n * n for n in range(4)]\nprint(${JSON.stringify(title)})\nfor line in range(10):\n  print("A bounded line of printed evidence.")\n${index % 11 === 0 ? '1 / 0' : 'values'}`;
  return `Inspect ${title}.\n${fence(code)}\n\`\`\`final\n${label} analysis ${index}. ` + 'The conversation and notebook preserve separate reading positions. '.repeat(5) + '\n```';
};

// Canonical inputs, actual engine executions and immutable data-only child
// definitions. No SQL seed, fake cell result, presentation record or event stream.
export async function startReplFixture() {
  const fixture = await startFixture({ executeCode: true, agentResponses: true, replStreams: true, lifetimeMs: 900000 });
  try {
    await writeFile(join(fixture.directory, 'repl-evidence.txt'), 'Read the same actual file twice.\n');
    await writeFile(join(fixture.directory, 'repl-unreadable.bin'), Buffer.from([255]));
    const client = await fixture.connect(`repl-${randomUUID()}`);
    const { root } = await fixture.createRoot(client, { title: 'Inspect Root cell 000.' });
    const session = client.session(root.id), policy = await session.permissions.policy(deadline());
    await session.permissions.setMode({ mode: 'automatic', expected_revision: policy.revision }, randomUUID(), deadline());
    const run = async (owner, text) => {
      const command = owner.submission([{ type: 'text', text }], randomUUID());
      await command.send(deadline());
      const admitted = await command.wait(deadline());
      assert.equal(admitted.turn.state, 'succeeded', JSON.stringify({ input: text.slice(0, 80), failure: admitted.turn.failure }));
      return admitted.turn;
    };
    const readGrant = await client.call('grants.create', { id: randomUUID(), session_id: root.id, capability: 'files.read', resource: root.working_directory }, deadline());
    const children = {}, turns = {};
    for (const [name, label, count] of [['repl-child', 'Child', 4], ['repl-empty', 'Empty', 0], ['repl-paged', 'Paged', 80], ['repl-sparse', 'Sparse', 4]]) {
      const definition = await client.agents.register(defineAgent({ id: name, name, defaults: { automatic_title: false } }), deadline());
      const requestID = randomUUID();
      const spawned = await session.spawn({ definition: definition.ref, overrides: { report_mode: 'notice' }, grant_ids: [], parts: [{ type: 'text', text: 'No execution has run in this agent yet.' }] }, requestID, deadline());
      assert.equal((await client.wait(requestID, deadline())).turn.state, 'succeeded');
      assert.equal(spawned.session.parent_id, root.id);
      const child = client.session(spawned.session.id); children[name] = child;
      if (name === 'repl-child') await client.call('grants.create', { id: randomUUID(), session_id: child.id, capability: readGrant.capability, resource: readGrant.resource, issuer_id: readGrant.id }, deadline());
      turns[name] = [];
      for (let index = 0; index < count; index++) turns[name].push((await run(child, recordedReplPrompt(label, index))).id);
      if (name === 'repl-sparse') for (let index = 0; index < 60; index++) await run(child, `No execution in this later message ${index}.`);
    }
    // Seed root executions after child completion mail has settled, so the
    // newest execution window describes the intended root cells. This also
    // exceeds the 512-record transcript window using real ordinary turns.
    for (let index = 0; index < 180; index++) await run(session, recordedReplPrompt('Root', index));
    // Capture exact committed evidence for labels and counters; never assert
    // invented legacy step counts or durations against native execution.
    const latest = async owner => {
      const turn = (await owner.turns.page({ limit: 1 }, deadline())).items[0];
      const cell = (await owner.turns.cells(turn.id, { limit: 100 }, deadline())).items[0];
      assert(cell, 'Expected an actual cell in the captured latest turn');
      const history = await owner.history.page({ direction: 'backward', limit: 100 }, deadline());
      const part = history.messages.find(message => message.id === cell.result_message_id)?.parts.find(part => part.type === 'tool_result');
      assert(part); return { turn, cell, result: JSON.parse(part.result.output).result };
    };
    return { fixture, client, root, session, children, turns, run, latest, rootEvidence: await latest(session) };
  } catch (error) { await fixture.close(); throw error; }
}
