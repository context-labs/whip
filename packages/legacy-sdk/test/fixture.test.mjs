import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';
import { fixtureExternalOrigin, repository, startFixture } from '../scripts/fixture.mjs';

test('fixture external origin accepts only exact HTTPS origins', async () => {
  for (const value of [undefined, 'https://whip.example.ts.net', 'https://whip.example.ts.net:8443', 'https://[::1]:8443']) {
    assert.equal(fixtureExternalOrigin(value), value);
  }
  for (const value of ['', null, 'http://whip.example.ts.net', 'wss://whip.example.ts.net', 'https://',
    'https://user:password@whip.example.ts.net', 'https://whip.example.ts.net/', 'https://whip.example.ts.net/api',
    'https://whip.example.ts.net?', 'https://whip.example.ts.net?q=1', 'https://whip.example.ts.net#fragment',
    'https://*.example.ts.net', ' https://whip.example.ts.net', 'https://whip.example.ts.net:65536']) {
    assert.throws(() => fixtureExternalOrigin(value), TypeError);
    await assert.rejects(startFixture({ externalOrigin: value }), TypeError);
  }
});

test('manual fixture rejects an invalid origin before compilation', () => {
  const result = spawnSync(process.execPath, ['apps/mobile/scripts/fixture.mjs', '--origin=https://*.example.ts.net'], {
    cwd: repository, encoding: 'utf8', timeout: 5_000,
  });
  assert.equal(result.error, undefined);
  assert.notEqual(result.status, 0);
  assert.ok(!result.stdout.includes('Compiling'));
  assert.match(result.stderr, /exact HTTPS origin/);
});
