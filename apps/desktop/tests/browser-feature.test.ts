import assert from 'node:assert/strict';
import test from 'node:test';
import { browserTabsEnabled } from '../src/browser-feature';
test('packaged Browser feature is off unless main launch environment explicitly opts in', () => {
  for (const value of [undefined, '', '0', 'true', 'yes', '1 ']) assert.equal(browserTabsEnabled(true, value), false);
  assert.equal(browserTabsEnabled(true, '1'), true);
  assert.equal(browserTabsEnabled(false, undefined), true);
  assert.equal(browserTabsEnabled(false, '0'), true);
});
