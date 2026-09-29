import { expect, it } from 'vitest';
import { assertValid, type ContractTypes } from '@whip/protocol';
import fixtures from '../../protocol/schema/fixtures.json';
import { contextUsageLines, turnUsageLines } from '../src/usage-presentation';
function sample<T extends keyof ContractTypes>(type: T): ContractTypes[T] {
  const value: unknown = structuredClone(fixtures.find(item => item.type === type && item.valid)!.value);
  assertValid(type, value); return value;
}
it('marks a captured tail stale as the observed transcript advances and hides mismatched revisions', () => {
  const value = sample('ContextUsage');
  value.prefill!.stale = false; value.through_sequence = value.prefill!.through_sequence;
  const history = { session_id: value.session_id, revision: value.history_revision, through_sequence: '9007199254740994', message_count: '1' };
  expect(contextUsageLines(value, value.session_id, value.config_revision, history)).toContain('Stale prefill: the transcript has advanced since this request.');
  expect(contextUsageLines(value, 'foreign', value.config_revision, history)).toEqual(['Context reading belongs to another session.']);
  expect(contextUsageLines(value, value.session_id, '99', history)).toEqual(['Context usage is unknown after configuration changed.']);
  expect(contextUsageLines(value, value.session_id, value.config_revision, { ...history, revision: '99' })).toEqual(['Context usage is unknown after history changed.']);
  value.prefill!.input_tokens = '0'; value.prefill!.input_source = 'estimated'; value.prefill!.context_window_tokens = null;
  const lines = contextUsageLines(value, value.session_id, value.config_revision, history);
  expect(lines).toContain('Estimated input: 0 tokens.'); expect(lines).toContain('Model context capacity is unknown.');
});
it('formats only canonical turn accounting, preserving zero, missing, unknown, and overflow fields', () => {
  const value = sample('TurnUsage'); value.usage.input_tokens.overflow = true;
  const text = turnUsageLines(value).join('\n');
  expect(text).toContain('9,007,199,254,740,993');
  expect(text).toContain('1 committed compactions');
  expect(text).toContain('Input tokens: at least');
  expect(text).toContain('Output tokens: not reported');
  expect(text).toContain('unknown cost');
});
