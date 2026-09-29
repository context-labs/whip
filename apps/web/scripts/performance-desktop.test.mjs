import assert from 'node:assert/strict';
import { test } from 'node:test';
import { summaryPollEvidence } from './performance-desktop.mjs';
const requests = (...sets) => sets.map(rootIDs => ({ rootIDs, roots: rootIDs.length }));
test('summary poll evidence distinguishes exact scopes with equal counts', () => {
 assert.deepEqual(summaryPollEvidence(requests(['a','b'],['b','a'],['b','c'],['c','b']), ['a','b'], ['b','c']),
  { sameRootSet: false, combinedPolls: 4, tabPolls: 2, sidebarPolls: 2 });
});
test('identical tab and sidebar scopes retain honest combined evidence', () => {
 assert.deepEqual(summaryPollEvidence(requests(...Array(6).fill(['a','b'])), ['a','b'], ['b','a']),
  { sameRootSet: true, combinedPolls: 6, tabPolls: null, sidebarPolls: null });
 assert.throws(() => summaryPollEvidence(requests(...Array(9).fill(['a'])), ['a'], ['a']), /bounded aggregate/);
});
test('missing owners, unexpected roots and per-tab polling fail', () => {
 assert.throws(() => summaryPollEvidence(requests(['a'],['a']), ['a'], ['b']), /visible-sidebar/);
 assert.throws(() => summaryPollEvidence(requests(['a'],['b'],['c']), ['a'], ['b']), /Unexpected summary/);
 assert.throws(() => summaryPollEvidence(requests(...Array(9).fill(['a']), ['b'], ['b']), ['a'], ['b']), /shared tab/);
});
