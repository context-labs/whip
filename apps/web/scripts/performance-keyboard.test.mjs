import assert from 'node:assert/strict';
import test from 'node:test';
import { keyboardResult } from './performance-keyboard.mjs';

function sample() {
  const keys = Array.from({ length: 40 }, (_, index) => ({ startTime: index * 100, trusted: true }));
  return { keys, entries: [], frames: keys.map(() => 1), phases: [], overflow: false, dropped: 0,
    browserEventCount: 40, visible: true, focused: true };
}
test('retains missing events as unknown rather than assuming threshold censorship', () => {
  const result = keyboardResult(sample());
  assert.equal(result.p95Rounded, null); assert.equal(result.missing, 40);
  assert.equal(result.lower.p95, 0); assert.equal(result.upper.p95, null);
});
test('uses all event ranks instead of taking p95 only over slow reported events', () => {
  const raw = sample();
  raw.entries = [37, 38, 39].map(index => ({ startTime: index * 100, duration: (index - 35) * 8,
    processingStart: index * 100, processingEnd: index * 100 + 1, interactionId: index }));
  const result = keyboardResult(raw);
  assert.equal(result.p95Rounded, null); assert.equal(result.missing, 37);
  assert.equal(result.lower.p95, 12); assert.equal(result.upper.p95, null);
});
test('preserves the native quantization bounds when all events are reported', () => {
  const raw = sample();
  raw.entries = raw.keys.map(({ startTime }, index) => ({ startTime, duration: 72,
    processingStart: startTime + 1, processingEnd: startTime + 2, interactionId: index + 1 }));
  const result = keyboardResult(raw);
  assert.equal(result.missing, 0); assert.equal(result.p95Rounded, 72);
  assert.equal(result.lower.p95, 68); assert.equal(result.upper.p95, 76);
});
test('rejects dropped, incomplete, ambiguous and unrelated slow event evidence', () => {
  for (const mutate of [raw => { raw.dropped = 1; }, raw => { raw.keys.pop(); }, raw => { raw.browserEventCount--; },
    raw => { raw.focused = false; }, raw => { raw.keys[0].trusted = false; },
    raw => { raw.entries = [{ startTime: 50 }]; },
    raw => { raw.entries = [{ startTime: 0 }, { startTime: 0 }]; }]) {
    const raw = sample(); mutate(raw); assert.throws(() => keyboardResult(raw));
  }
});
