import {test} from 'node:test';
import assert from 'node:assert/strict';
import {defaultDisplayPreferences, parseDisplayPreferences, validateDisplayPreferences} from '../src/appearance-data.ts';

test('display preferences round-trip exact bounds and closed font choices', () => {
  for (const uiSize of [12, 13, 20]) for (const codeSize of [10, 12, 24]) {
    const display = {...defaultDisplayPreferences, uiSize, codeSize, uiFont: 'system', codeFont: 'system', wrapCode: true};
    assert.deepEqual(parseDisplayPreferences(JSON.stringify({version: 1, display})), display);
  }
  assert.deepEqual(parseDisplayPreferences(null), defaultDisplayPreferences);
  const value = validateDisplayPreferences({...defaultDisplayPreferences, arbitraryCSS: 'url(invalid)'});
  assert.deepEqual(value, defaultDisplayPreferences);
});
test('corrupt, oversized, fractional and non-enum display preferences are rejected', () => {
  for (const patch of [{uiSize: 11}, {uiSize: 21}, {codeSize: 9}, {codeSize: 25}, {uiSize: 13.5}, {uiSize: Infinity}, {codeSize: NaN},
    {uiFont: 'url(https://example.com)'}, {codeFont: {}}, {motion: 'none'}, {contrast: 'forced'}, {wrapCode: 1}]) {
    assert.throws(() => validateDisplayPreferences({...defaultDisplayPreferences, ...patch}), /Invalid display/);
  }
  for (const text of ['{', '{}', 'null', JSON.stringify({version: 2, display: defaultDisplayPreferences}), ' '.repeat(4097), JSON.stringify({version: 1, display: defaultDisplayPreferences, padding: '界'.repeat(1600)})]) assert.throws(() => parseDisplayPreferences(text));
});
