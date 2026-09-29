import assert from 'node:assert/strict';
import test from 'node:test';
import { juniorDeveloper } from './junior-developer.js';

test('the junior template is immutable data with explicit discovery and no executor', () => {
  const defaults = juniorDeveloper.document.defaults;
  assert.equal(defaults.instructions?.discover_skills, false);
  assert.equal(defaults.instructions?.standing_instructions, true);
  assert.deepEqual(defaults.instructions?.project_files, ['CLAUDE.md', 'AGENTS.md']);
  assert.match(defaults.instructions?.text ?? '', /Never rewrite history/);
  assert.match(defaults.instructions?.text ?? '', /ask the user with user.ask/);
  assert.deepEqual(Object.keys(juniorDeveloper.handlers), []);
  assert.throws(() => defaults.modules!.push('browser'), TypeError);
});
// The acceptance suite compares this document with the running host's public
// built-in definition, without importing a private Go/legacy fixture.
