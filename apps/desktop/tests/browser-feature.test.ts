import assert from 'node:assert/strict';
import test from 'node:test';
import { browserTabsEnabled } from '../src/browser-feature';

test('Browser tabs are enabled by default in all desktop builds', () => {
  assert.equal(browserTabsEnabled(undefined), true);
  assert.equal(browserTabsEnabled('1'), true);
});

test('only the explicit main-process launch override disables Browser tabs', () => {
  assert.equal(browserTabsEnabled('0'), false);
  for (const value of ['', 'true', 'false', 'yes', '1 ', '0 ']) assert.equal(browserTabsEnabled(value), true);
});
