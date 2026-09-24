import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { juniorDeveloper } from './junior-developer.js';

test('the example matches the runtime fixture byte for byte', () => {
  const fixture = JSON.parse(readFileSync(new URL('../../../internal/agentdef/testdata/junior-developer.json', import.meta.url), 'utf8')) as unknown;
  assert.deepEqual(JSON.parse(JSON.stringify(juniorDeveloper.document)), fixture);
});
