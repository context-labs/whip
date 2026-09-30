import { test } from 'node:test';
import assert from 'node:assert/strict';
import { compare, normalize } from './compare.mjs';

const transcript = () => ({ response: { value: null, cursor: '3' }, error: { code: -32000, message: 'conflict', data: { kind: 'conflict' } }, events: [
  { seq: '1', kind: 'started', payload: { root_id: 'root-a', turn_id: 'turn-a', created_at: '2026-01-01T00:00:00Z' } },
  { seq: '2', kind: 'text', payload: { root_id: 'root-a', turn_id: 'turn-a', text: 'first' } },
  { seq: '3', kind: 'ended', payload: { root_id: 'root-a', turn_id: 'turn-a' } },
] });

test('only named nondeterminism changes; identity relationships remain intact', () => {
  const left = transcript(), right = transcript();
  for (const event of right.events) { event.payload.root_id = 'root-b'; event.payload.turn_id = 'turn-b'; }
  right.events[0].payload.created_at = '2026-03-01T00:00:00Z';
  compare(normalize(left), normalize(right));
});

for (const [name, mutate] of Object.entries({
  response: value => { value.response.value = ''; },
  cursor: value => { value.response.cursor = '4'; },
  omission: value => { delete value.response.value; },
  error: value => { value.error.message = 'different conflict'; },
  order: value => { value.events.reverse(); },
  missing: value => { value.events.pop(); },
  duplicate: value => { value.events.push(value.events[2]); },
  identity: value => { value.events[2].payload.turn_id = 'different-turn'; },
  payload: value => { value.events[1].payload.text = 'changed'; },
})) test(`rejects deliberate ${name} regression`, () => {
  const changed = transcript(); mutate(changed);
  assert.throws(() => compare(normalize(transcript()), normalize(changed)), assert.AssertionError);
});

test('invalid clock values fail instead of disappearing during normalization', () => {
  assert.throws(() => normalize({ created_at: 'invalid' }));
  assert.throws(() => normalize({ end_ns: '-1' }));
});

test('reversed turn and span times fail before normalization', () => {
  assert.throws(() => normalize({ started_at: '2026-01-02T00:00:00Z', finished_at: '2026-01-01T00:00:00Z' }));
  assert.throws(() => normalize({ start_ns: '2', end_ns: '1' }));
});
