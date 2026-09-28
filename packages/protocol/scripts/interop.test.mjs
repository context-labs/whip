import { readFile } from 'node:fs/promises';
import { spawnSync } from 'node:child_process';
import assert from 'node:assert/strict';
import test from 'node:test';
import { validate, manifest } from '../generated/index.js';

const fixtures = JSON.parse(await readFile(new URL('../schema/fixtures.json', import.meta.url), 'utf8'));
test('actual Go JSON agrees with standalone TypeScript validation', () => {
  assert.equal(manifest.major, 4);
  for (const fixture of fixtures) {
    assert.equal(validate(fixture.type, fixture.value), fixture.valid, fixture.type + ': ' + JSON.stringify(fixture.value));
  }
  assert.equal(fixtures.find(f => f.type === 'Session').value.config_revision, '9007199254740993');
  const patch = fixtures.find(f => f.type === 'UpdateConfigurationParams').value.patch;
  assert.deepEqual(patch.tools, {});
  assert.deepEqual(patch.output, { schema: null });
  assert.equal(patch.report_mode, 'inline');
  assert.equal(fixtures.find(f => f.type === 'Session').value.configuration.report_mode, 'notice');
});
test('TypeScript JSON re-encoding retains counters and agrees with Go', () => {
  const fixture = structuredClone(fixtures.find(f => f.type === 'HistoryResult'));
  fixture.value.items[0].sequence = (BigInt(fixture.value.items[0].sequence) + 1n).toString();
  const input = [...fixtures, fixture];
  const result = spawnSync('go', ['run', '../../cmd/whip-contract', '-validate'], {
    input: JSON.stringify(input), encoding: 'utf8', timeout: 60000,
  });
  assert.equal(result.status, 0, result.error?.message ?? result.stderr);
});
test('unknown types and ambiguous parts fail closed', () => {
  assert.throws(() => validate('LegacyCommand', {}), /Unknown/);
  const submit = structuredClone(fixtures.find(f => f.type === 'SubmitParams' && f.valid).value);
  submit.parts[0].reference_id = 'content';
  assert.equal(validate('SubmitParams', submit), false);
  submit.parts = [];
  assert.equal(validate('SubmitParams', submit), false);
});

test('completion reads pin an exact snapshot and bound byte pages', () => {
  const read = structuredClone(fixtures.find(f => f.type === 'ReadCompletionParams').value);
  assert.equal(validate('ReadCompletionParams', read), true);
  assert.equal(validate('ReadCompletionParams', { ...read, length: 65537 }), false);
  assert.equal(validate('ReadCompletionParams', { ...read, offset: 0 }), false);
  delete read.turn_id;
  assert.equal(validate('ReadCompletionParams', read), false);
  const update = structuredClone(fixtures.find(f => f.type === 'UpdateConfigurationParams').value);
  update.patch.report_mode = 'automatic';
  assert.equal(validate('UpdateConfigurationParams', update), false);
});
