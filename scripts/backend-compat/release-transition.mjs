import assert from 'node:assert/strict';
import { compare } from './compare.mjs';

// The already reviewed features between main and the frozen pre-reorganization
// development baseline. This expectation applies to this exact pair only.
// Provenance and scope: docs/release-readiness.md.
export const releaseTransition = {
  base: '88764670f1c6e2a57e22463e96cfb0d6021aea33',
  baseline: '271c0f8d2a35648d1b45056d57432590b783483c',
};

function replace(object, key, before, after) {
  assert.ok(Object.hasOwn(object, key), `missing release source field ${key}`);
  assert.deepEqual(object[key], before, `unexpected release source field ${key}`);
  object[key] = after;
}

function add(object, key, value) {
  assert.ok(!Object.hasOwn(object, key), `release source already has ${key}`);
  object[key] = value;
}

export function expectedReleaseTranscript(parent) {
  const expected = structuredClone(parent);
  const { initialization, configuration, updated, provider, initial, snapshot, history, live, replay } = expected;

  // f62b5eec6: protocol 6.9 and concrete effort resolved at session creation.
  replace(initialization, 'protocol_minor', 8, 9);
  for (const state of [initial, snapshot]) {
    replace(state.meta, 'effort', '', 'low');
    replace(state.agents[0], 'effort', '', 'low');
  }

  // b8cc56a48: compaction defaults to the conversation model/provider. Revision
  // hashes are exact fixture values, not wildcarded configuration differences.
  for (const state of [configuration, updated]) replace(state, 'compact_model', 'deepseek-v4-flash-0731', '');
  replace(configuration, 'revision', 'f82bc34858a0838f80a01e6e9b9c319905987aa2721f374d499a3b918f555f85', '4f2ca9fffdfa2e329309d36d357715e0abdaca3765524f3d651a0dbaef8de834');
  for (const state of [updated, provider]) replace(state, 'revision', 'a5719c0971e0b596bcc0ae551e8d8f85b84c0f28a3337b8252b416a3b271e44f', 'aa5d3e546d8cbfd6980e2452c78200a2419b5d88ea4d1b35fd61ad745d2a3b68');

  // 12f0ea076: create a folder from the native directory picker.
  const operations = initialization.operations;
  assert.ok(!operations.some(operation => operation.name === 'host.directory.create'));
  const picker = operations.findIndex(operation => operation.name === 'host.directory.pick');
  assert.ok(picker >= 0, 'release source lost directory picker');
  operations.splice(picker, 0, { name: 'host.directory.create', surface: 'rpc', execution: 'ephemeral', permission: 'host-runtime' });

  // 54fca9c1a: title ownership moved to admission, replacing the explicit
  // autotitle command with a notification and an ordered durable title event.
  const autotitle = operations.findIndex(operation => operation.name === 'session.autotitle');
  assert.ok(autotitle >= 0, 'release source lost autotitle');
  assert.deepEqual(operations[autotitle], { name: 'session.autotitle', surface: 'runtime', execution: 'command', permission: 'root-association' });
  operations.splice(autotitle, 1);
  for (const capabilities of [initialization.capabilities, initialization.negotiated_capabilities]) {
    assert.ok(!capabilities.includes('session_title_notifications'));
  }
  initialization.capabilities.push('session_title_notifications');
  const engine = initialization.negotiated_capabilities.indexOf('execution_engines');
  assert.ok(engine >= 0, 'release source lost execution engine capability');
  initialization.negotiated_capabilities.splice(engine, 0, 'session_title_notifications');
  const title = 'A'.repeat(127) + '…';
  replace(snapshot.meta, 'title', 'A'.repeat(63) + '…', title);
  for (const events of [live, replay.events]) {
    assert.deepEqual(events.map(event => event.kind), ['command.queued', 'turn.started', 'span.started', 'stream.text', 'stream.text', 'turn.succeeded', 'span.ended']);
    for (let i = 0; i < events.length; i++) {
      assert.equal(events[i].seq, String(i + 1));
      if (i === 0) continue;
      if (events[i].kind.startsWith('span.')) replace(events[i].payload, 'updated_seq', String(i + 1), String(i + 2));
      events[i].seq = String(i + 2);
    }
    const event = { root_id: events[0].root_id, seq: '2', kind: 'session.title.updated', payload: { title } };
    if (Object.hasOwn(events[0], 'subscription_id')) event.subscription_id = events[0].subscription_id;
    events.splice(1, 0, event);
  }
  replace(replay, 'latest', '7', '8');
  replace(snapshot, 'cursor', '7', '8');
  replace(snapshot.accounting, 'revision', '7', '8');
  replace(snapshot.agents[0].last_turn, 'event_seq', '6', '7');
  for (const messages of [snapshot.messages, history.messages.map(entry => entry.message)]) {
    assert.equal(messages[0].role, 'user');
    add(messages[0], 'presentation', { version: 1, turn_id: snapshot.agents[0].last_turn.turn_id });
    // b8cc56a48: preserve assistant recording time in the durable transcript.
    assert.equal(messages[1].role, 'assistant');
    add(messages[1], 'sent_at', '<timestamp>');
  }
  return expected;
}

export function comparePRBase(parent, candidate, { baseRevision, baselineRevision, baseline, label }) {
  if (baseRevision !== releaseTransition.base) {
    compare(parent, candidate, label);
    return 'strict';
  }
  assert.equal(baselineRevision, releaseTransition.baseline, 'release expectation belongs to a different frozen baseline');
  const expected = expectedReleaseTranscript(parent);
  // Prove the exception describes the pre-reorganization reference itself.
  // A new candidate response can never become its own compatibility oracle.
  compare(expected, baseline, `${label}: reviewed pre-reorganization transition`);
  compare(expected, candidate, label);
  return 'reviewed 6.8 to 6.9 transition';
}
