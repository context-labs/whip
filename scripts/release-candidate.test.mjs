import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { candidate } from '../apps/desktop/scripts/release-candidate.mjs';

async function fixture(t) {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'whip-release-candidate-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const env = { RELEASE_TAG: 'desktop-v1.2.3-beta.1', SOURCE_SHA: 'a'.repeat(40) };
  const version = '1.2.3-beta.1';
  const source = { commit: env.SOURCE_SHA, dirty: false };
  const compatibility = { protocolMajor: 4, protocolMinor: 1, schemaVersion: 10 };
  const files = {};
  for (const [name, value] of Object.entries({ 'Whip Beta-1.2.3-beta.1.dmg': 'dmg fixture', 'Whip Beta-1.2.3-beta.1.zip': 'zip fixture',
    'RELEASES.json': JSON.stringify({ currentRelease: version }) })) {
    const bytes = Buffer.from(value); await writeFile(path.join(directory, name), bytes);
    files[name] = { bytes: bytes.length, sha256: createHash('sha256').update(bytes).digest('hex') };
  }
  const json = async (name, value) => writeFile(path.join(directory, name), JSON.stringify(value));
  const evidence = { version, buildId: version, source, compatibility, rendererDigest: 'b'.repeat(64), signed: true, notarized: true, dmgNotary: 'fixture', files };
  await json('evidence.json', evidence);
  const startup = { evidence, completed: true, interrupted: false, requested: { samples: 30, firstSamples: 1 },
    results: [{ state: 'complete', launchServicesExit: { code: 0 } }], cleanup: [{ state: 'stopped' }],
    statistics: Object.fromEntries(['first-launch', 'warm-attach', 'retained-start'].map(name => [name, { failed: 0, attempted: name === 'first-launch' ? 1 : 30 }])) };
  await json('signed-startup.json', startup);
  await json('sbom.cdx.json', { bomFormat: 'CycloneDX', components: [{ name: 'fixture' }] });
  await writeFile(path.join(directory, 'THIRD_PARTY_NOTICES.txt'), 'fixture notices');
  await json('linux-runtime.json', { ...compatibility, distribution: 'whipcode', updateOwner: 'standalone', buildId: version, source,
    rendererDigest: evidence.rendererDigest, smoke: { embeddedRenderer: true, daemonReady: true } });
  await writeFile(path.join(directory, 'whipcode-linux-x64'), 'linux fixture');
  return { directory, env, json, evidence, startup };
}

test('release candidate binds desktop and Linux bytes to the same source and version', async t => {
  const f = await fixture(t);
  await candidate('assemble', f.directory, f.env);
  await candidate('verify', f.directory, f.env);
  const before = await readFile(path.join(f.directory, 'SHA256SUMS'), 'utf8');
  await candidate('assemble', f.directory, f.env);
  assert.equal(await readFile(path.join(f.directory, 'SHA256SUMS'), 'utf8'), before);
  await writeFile(path.join(f.directory, 'whipcode-linux-x64'), 'tampered Linux');
  await assert.rejects(candidate('verify', f.directory, f.env), /identity or bytes differ/);
});

test('candidate cannot promote a different source, version, dirty or unsigned desktop', async t => {
  for (const changed of [{ source: { commit: 'c'.repeat(40), dirty: false } }, { source: { commit: 'a'.repeat(40), dirty: true } },
    { version: '1.2.3' }, { signed: false }, { buildId: 'different' }]) {
    const f = await fixture(t);
    await f.json('evidence.json', { ...f.evidence, ...changed });
    await assert.rejects(candidate('assemble', f.directory, f.env));
  }
});

test('candidate refuses missing or changed signed assets before manifest assembly', async t => {
  const f = await fixture(t);
  const zip = path.join(f.directory, 'Whip Beta-1.2.3-beta.1.zip');
  await writeFile(zip, 'different');
  await assert.rejects(candidate('assemble', f.directory, f.env), /Signed artifact changed/);
  await rm(zip);
  await assert.rejects(candidate('assemble', f.directory, f.env));
});


test('candidate refuses missing acceptance artifacts and failed or mismatched startup evidence', async t => {
  for (const name of ['signed-startup.json', 'sbom.cdx.json', 'THIRD_PARTY_NOTICES.txt']) {
    const f = await fixture(t);
    await rm(path.join(f.directory, name));
    await assert.rejects(candidate('assemble', f.directory, f.env), /Missing required/);
  }
  for (const changed of [{ completed: false }, { interrupted: true }, { results: [{ state: 'timeout' }] },
    { evidence: { buildId: 'old' } }, { statistics: {} }, { cleanup: [{ state: 'cleanup-failed' }] }, { requested: { samples: 1 } }]) {
    const f = await fixture(t);
    await f.json('signed-startup.json', { ...f.startup, ...changed });
    await assert.rejects(candidate('assemble', f.directory, f.env));
  }
});

test('candidate refuses signed evidence that omits an archive digest', async t => {
  const f = await fixture(t);
  const files = { ...f.evidence.files }; delete files['Whip Beta-1.2.3-beta.1.zip'];
  await f.json('evidence.json', { ...f.evidence, files });
  await assert.rejects(candidate('assemble', f.directory, f.env), /bind every installer/);
});
