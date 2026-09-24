import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { candidate } from '../apps/desktop/scripts/release-candidate.mjs';
import { runtimeEntitlements, validateRuntimeSigning } from '../apps/desktop/scripts/runtime-signing.mjs';

async function fixture(t) {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'whip-release-candidate-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const env = { RELEASE_TAG: 'desktop-v1.2.3-beta.1', SOURCE_SHA: 'a'.repeat(40) };
  const version = '1.2.3-beta.1';
  const source = { commit: env.SOURCE_SHA, dirty: false };
  const compatibility = { protocolMajor: 5, protocolMinor: 0, schemaVersion: 11 };
  const files = {};
  for (const [name, value] of Object.entries({ 'Whip-Beta-1.2.3-beta.1.dmg': 'dmg fixture', 'Whip-Beta-1.2.3-beta.1.zip': 'zip fixture',
    'RELEASES.json': JSON.stringify({ currentRelease: version }) })) {
    const bytes = Buffer.from(value); await writeFile(path.join(directory, name), bytes);
    files[name] = { bytes: bytes.length, sha256: createHash('sha256').update(bytes).digest('hex') };
  }
  const json = async (name, value) => writeFile(path.join(directory, name), JSON.stringify(value));
  const teamId = 'JAPWPV5JY2';
  const runtimeSigning = { teamId, identifier: 'com.contextlabs.whip.beta.runtime', hardenedRuntime: true, entitlements: runtimeEntitlements };
  const nativeFiles = { whipcode: { bytes: 123, sha256: 'e'.repeat(64) } };
  const evidence = { version, buildId: version, source, compatibility, rendererDigest: 'b'.repeat(64), signed: true, notarized: true, dmgNotary: 'fixture', files, teamId, nativeFiles, runtimeSigning };
  await json('evidence.json', evidence);
  const report = { sha256: nativeFiles.whipcode.sha256, architecture: 'arm64', engines: Object.fromEntries(['quickjs', 'starlark'].map(engine => [engine, {
    descriptor: { id: engine, build: 'fixture', abi: 'fixture', profile: 'fixture' },
    checks: ['execution', 'output', 'host-call', 'persistent-state', 'checkpoint-restore', ...(engine === 'quickjs' ? ['cancellation', 'recovery'] : [])],
  }])) };
  const runtime = { schema: 1, completed: true, package: evidence, packaged: report, installed: report };
  await json('signed-runtime.json', runtime);
  const startup = { evidence, completed: true, interrupted: false, requested: { samples: 30, firstSamples: 1 },
    results: [{ state: 'complete', launchServicesExit: { code: 0 } }], cleanup: [{ state: 'stopped' }],
    statistics: Object.fromEntries(['first-launch', 'warm-attach', 'retained-start'].map(name => [name, { failed: 0, attempted: name === 'first-launch' ? 1 : 30 }])) };
  await json('signed-startup.json', startup);
  await json('sbom.cdx.json', { bomFormat: 'CycloneDX', components: [{ name: 'fixture' }] });
  await writeFile(path.join(directory, 'THIRD_PARTY_NOTICES.txt'), 'fixture notices');
  await json('linux-runtime.json', { ...compatibility, distribution: 'whipcode', updateOwner: 'standalone', buildId: version, source,
    rendererDigest: evidence.rendererDigest, smoke: { embeddedRenderer: true, daemonReady: true } });
  await writeFile(path.join(directory, 'whipcode-linux-x64'), 'linux fixture');
  return { directory, env, json, evidence, startup, runtime };
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
  const zip = path.join(f.directory, 'Whip-Beta-1.2.3-beta.1.zip');
  await writeFile(zip, 'different');
  await assert.rejects(candidate('assemble', f.directory, f.env), /Signed artifact changed/);
  await rm(zip);
  await assert.rejects(candidate('assemble', f.directory, f.env));
});


test('candidate refuses missing acceptance artifacts and failed or mismatched startup evidence', async t => {
  for (const name of ['signed-startup.json', 'signed-runtime.json', 'sbom.cdx.json', 'THIRD_PARTY_NOTICES.txt']) {
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

test('backend signing requires hardened runtime and exactly the executable-memory entitlement', () => {
  const valid = { teamId: 'JAPWPV5JY2', identifier: 'com.contextlabs.whip.runtime', hardenedRuntime: true, entitlements: runtimeEntitlements };
  validateRuntimeSigning(valid, valid.teamId);
  for (const changed of [{ hardenedRuntime: false }, { entitlements: {} },
    { entitlements: { 'com.apple.security.cs.allow-jit': true } },
    { entitlements: { ...runtimeEntitlements, 'com.apple.security.cs.disable-executable-page-protection': true } },
    { entitlements: { 'com.apple.security.cs.allow-unsigned-executable-memory': 'true' } }, { teamId: 'other' }, { identifier: 'com.other.app' }])
    assert.throws(() => validateRuntimeSigning({ ...valid, ...changed }, valid.teamId));
});

test('candidate rejects incomplete or mismatched runtime execution evidence', async t => {
  for (const change of [
    value => { value.completed = false; },
    value => { value.package.buildId = 'old'; },
    value => { value.installed.sha256 = 'f'.repeat(64); },
    value => { value.packaged.architecture = 'amd64'; },
    value => { delete value.installed.engines.quickjs; },
    value => { value.packaged.engines.quickjs.checks = ['execution']; },
    value => { value.installed.engines.quickjs.descriptor.abi = 'different'; },
  ]) {
    const f = await fixture(t);
    const value = JSON.parse(JSON.stringify(f.runtime));
    change(value);
    await f.json('signed-runtime.json', value);
    await assert.rejects(candidate('assemble', f.directory, f.env));
  }
});

test('candidate refuses signed evidence that omits an archive digest', async t => {
  const f = await fixture(t);
  const files = { ...f.evidence.files }; delete files['Whip-Beta-1.2.3-beta.1.zip'];
  await f.json('evidence.json', { ...f.evidence, files });
  await assert.rejects(candidate('assemble', f.directory, f.env), /bind every installer/);
});
