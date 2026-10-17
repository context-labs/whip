import assert from 'node:assert/strict';
import { test } from 'node:test';
import { comparePRBase, releaseTransition } from './release-transition.mjs';

function transcript(upgraded) {
  const revision = upgraded ? 'aa5d3e546d8cbfd6980e2452c78200a2419b5d88ea4d1b35fd61ad745d2a3b68' : 'a5719c0971e0b596bcc0ae551e8d8f85b84c0f28a3337b8252b416a3b271e44f';
  const title = 'A'.repeat(upgraded ? 127 : 63) + '…';
  const kinds = ['command.queued', ...(upgraded ? ['session.title.updated'] : []), 'turn.started', 'span.started', 'stream.text', 'stream.text', 'turn.succeeded', 'span.ended'];
  const events = kinds.map((kind, i) => ({ root_id: 'root', seq: String(i + 1), kind,
    payload: kind === 'session.title.updated' ? { title } : kind.startsWith('span.') ? { updated_seq: String(i + 1), trace_id: 'trace' } : { text: 'preserved payload' },
  }));
  const messages = () => [
    { role: 'user', content: 'input', ...(upgraded ? { presentation: { version: 1, turn_id: 'turn' } } : {}) },
    { role: 'assistant', content: 'output', ...(upgraded ? { sent_at: '<timestamp>' } : {}) },
  ];
  const effort = upgraded ? 'low' : '';
  return {
    initialization: {
      protocol_minor: upgraded ? 9 : 8,
      capabilities: ['commands', 'execution_engines', ...(upgraded ? ['session_title_notifications'] : [])],
      negotiated_capabilities: ['commands', ...(upgraded ? ['session_title_notifications'] : []), 'execution_engines'],
      operations: [
        ...(upgraded ? [{ name: 'host.directory.create', surface: 'rpc', execution: 'ephemeral', permission: 'host-runtime' }] : []),
        { name: 'host.directory.pick', surface: 'rpc', execution: 'query', permission: 'host-runtime' },
        ...(!upgraded ? [{ name: 'session.autotitle', surface: 'runtime', execution: 'command', permission: 'root-association' }] : []),
      ],
    },
    configuration: { compact_model: upgraded ? '' : 'deepseek-v4-flash-0731', revision: upgraded ? '4f2ca9fffdfa2e329309d36d357715e0abdaca3765524f3d651a0dbaef8de834' : 'f82bc34858a0838f80a01e6e9b9c319905987aa2721f374d499a3b918f555f85' },
    updated: { compact_model: upgraded ? '' : 'deepseek-v4-flash-0731', revision },
    provider: { revision, name: 'unchanged provider' },
    initial: { meta: { effort }, agents: [{ effort }] },
    snapshot: { meta: { effort, title }, agents: [{ effort, last_turn: { turn_id: 'turn', event_seq: upgraded ? '7' : '6' } }],
      cursor: upgraded ? '8' : '7', accounting: { revision: upgraded ? '8' : '7' }, messages: messages() },
    history: { messages: messages().map(message => ({ message })) },
    live: events.map(event => ({ ...structuredClone(event), subscription_id: 'subscription' })),
    replay: { latest: upgraded ? '8' : '7', events },
    error: { code: -32000, message: 'unchanged error', data: null },
  };
}

const options = () => ({ baseRevision: releaseTransition.base, baselineRevision: releaseTransition.baseline, baseline: transcript(true), label: 'test release' });

test('accepts the exact reviewed historical transition without modifying inputs', () => {
  const parent = transcript(false), candidate = transcript(true);
  assert.equal(comparePRBase(parent, candidate, options()), 'reviewed 6.8 to 6.9 transition');
  assert.deepEqual(parent, transcript(false));
  assert.deepEqual(candidate, transcript(true));
});

for (const [name, mutate] of Object.entries({
  effort: value => { value.snapshot.meta.effort = 'high'; },
  configuration: value => { value.configuration.revision = 'unexpected'; },
  provider: value => { value.provider.name = 'changed provider'; },
  error: value => { value.error.data = {}; },
  title: value => { value.live[1].payload.title = 'different title'; },
  eventOrder: value => { value.live.reverse(); },
  eventCount: value => { value.replay.events.splice(1, 1); },
  cursor: value => { value.snapshot.cursor = '9'; },
  identity: value => { value.history.messages[0].message.presentation.turn_id = 'other-turn'; },
  addedField: value => { value.snapshot.meta.new_field = true; },
})) test(`rejects an additional candidate ${name} change`, () => {
  const candidate = transcript(true); mutate(candidate);
  assert.throws(() => comparePRBase(transcript(false), candidate, options()), assert.AssertionError);
});

test('an unexpected historical value cannot be overwritten by a release expectation', () => {
  const parent = transcript(false); parent.initial.meta.effort = 'off';
  assert.throws(() => comparePRBase(parent, transcript(true), options()), /unexpected release source field effort/);
});

test('release expectations must describe the independently built frozen baseline', () => {
  const config = options(); config.baseline.updated.compact_model = 'another-model';
  assert.throws(() => comparePRBase(transcript(false), transcript(true), config), /reviewed pre-reorganization transition/);
  assert.throws(() => comparePRBase(transcript(false), transcript(true), { ...options(), baselineRevision: '0'.repeat(40) }), /different frozen baseline/);
});

test('every other PR base retains exact transcript equality', () => {
  const config = { ...options(), baseRevision: '1'.repeat(40) };
  assert.throws(() => comparePRBase(transcript(false), transcript(true), config), assert.AssertionError);
  assert.equal(comparePRBase(transcript(true), transcript(true), config), 'strict');
});
