import assert from 'node:assert/strict';
import test from 'node:test';
import { sessionLinkPath } from '../src/links';

test('session links select only the configured release channel and preserve shared routing', () => {
  assert.equal(sessionLinkPath('whip://session/runtime/root?agent=child'), '/h/runtime/s/root?agent=child');
  assert.equal(sessionLinkPath('whip-beta://session/runtime/root', 'whip-beta'), '/h/runtime/s/root');
  assert.throws(() => sessionLinkPath('whip://session/runtime/root', 'whip-beta'));
  for (const link of ['whip-beta://session/runtime/root', 'https://session/runtime/root',
    'whip://user:secret@session/runtime/root', 'whip://session/runtime/%2Froot',
    'whip://session/runtime/root#fragment', 'whip://session/runtime/root/command', 'whip://foreign/runtime/root'])
    assert.throws(() => sessionLinkPath(link));
});
