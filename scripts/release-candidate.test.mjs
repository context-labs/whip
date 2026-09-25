import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mkdtemp, readFile, readdir, rm, symlink, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { buildInstallers } from './build-installers.mjs';
import { candidate } from '../apps/desktop/scripts/release-candidate.mjs';
import { runtimeIdentityFields, validateRuntimeEvidence } from '../apps/desktop/scripts/runtime-evidence.mjs';
import { runtimeEntitlements, validateRuntimeSigning } from '../apps/desktop/scripts/runtime-signing.mjs';

async function fixture(t, version = '1.2.3-alpha.1') {
  const directory = await mkdtemp(path.join(os.tmpdir(), 'whip-release-candidate-'));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const env = { RELEASE_TAG: `v${version}`, SOURCE_SHA: 'a'.repeat(40) };
  const channel = version.includes('-') ? 'beta' : 'stable';
  const updateURL = `https://updates.example.test/${channel}/darwin/arm64/RELEASES.json`;
  const source = { commit: env.SOURCE_SHA, dirty: false };
  const compatibility = { protocolMajor: 5, protocolMinor: 0, schemaVersion: 11 };
  const files = {};
  for (const [name, value] of Object.entries({ 'whipcode-desktop-darwin-arm64.dmg': 'dmg fixture', 'whipcode-desktop-darwin-arm64.zip': 'zip fixture',
    'RELEASES.json': JSON.stringify({ currentRelease: version, releases: [{ version, updateTo: { version,
      url: new URL(`v${version}/whipcode-desktop-darwin-arm64.zip`, updateURL).href } }] }) })) {
    const bytes = Buffer.from(value); await writeFile(path.join(directory, name), bytes);
    files[name] = { bytes: bytes.length, sha256: createHash('sha256').update(bytes).digest('hex') };
  }
  const json = async (name, value) => writeFile(path.join(directory, name), JSON.stringify(value));
  const teamId = 'JAPWPV5JY2';
  const runtimeSigning = { teamId, identifier: 'com.contextlabs.whip.beta.runtime', hardenedRuntime: true, entitlements: runtimeEntitlements };
  const nativeFiles = { whipcode: { bytes: 123, sha256: 'e'.repeat(64) } };
  const evidence = { version, buildId: version, channel, updateURL, updateOwner: 'desktop', source, compatibility, rendererDigest: 'b'.repeat(64), signed: true, notarized: true, dmgNotary: 'fixture', files, teamId, nativeFiles, runtimeSigning };
  await json('evidence.json', evidence);
  const report = { sha256: nativeFiles.whipcode.sha256, architecture: 'arm64', engines: Object.fromEntries(['quickjs', 'starlark'].map(engine => [engine, {
    descriptor: { id: engine, build: 'fixture', abi: 'fixture', profile: 'fixture' },
    checks: ['execution', 'output', 'host-call', 'persistent-state', 'checkpoint-restore', ...(engine === 'quickjs' ? ['cancellation', 'recovery'] : [])],
  }])) };
  const runtime = { schema: 1, completed: true, package: Object.fromEntries(runtimeIdentityFields.map(key => [key, evidence[key]])), packaged: report, installed: report };
  await json('signed-runtime.json', runtime);
  const startup = { evidence, completed: true, interrupted: false, requested: { samples: 30, firstSamples: 1 },
    results: [{ state: 'complete', launchServicesExit: { code: 0 } }], cleanup: [{ state: 'stopped' }],
    statistics: Object.fromEntries(['first-launch', 'warm-attach', 'retained-start'].map(name => [name, { failed: 0, attempted: name === 'first-launch' ? 1 : 30 }])) };
  await json('signed-startup.json', startup);
  await json('sbom.cdx.json', { bomFormat: 'CycloneDX', components: [{ name: 'fixture' }] });
  await writeFile(path.join(directory, 'THIRD_PARTY_NOTICES.txt'), 'fixture notices');
  await json('linux-runtime.json', { ...compatibility, distribution: 'whipcode', updateOwner: 'standalone', buildId: `v${version}`, source,
    sha256: createHash('sha256').update('linux fixture').digest('hex'), rendererDigest: evidence.rendererDigest, smoke: { embeddedRenderer: true, daemonReady: true } });
  await writeFile(path.join(directory, 'whipcode-linux-x64'), 'linux fixture');
  for (const name of ['whipcode-linux-arm64', 'whipcode-darwin-x64', 'whipcode-darwin-arm64'])
    await writeFile(path.join(directory, name), name + ' fixture');
  const installers = buildInstallers(await readFile(new URL('../install.sh', import.meta.url), 'utf8'), env.RELEASE_TAG);
  for (const [name, contents] of Object.entries(installers)) await writeFile(path.join(directory, name), contents);
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
  await assert.rejects(candidate('verify', f.directory, f.env), /identity or bytes differ|different executable bytes/);
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
  const zip = path.join(f.directory, 'whipcode-desktop-darwin-arm64.zip');
  await writeFile(zip, 'different');
  await assert.rejects(candidate('assemble', f.directory, f.env), /Signed artifact changed/);
  await rm(zip);
  await assert.rejects(candidate('assemble', f.directory, f.env));
});


test('candidate refuses missing acceptance artifacts and failed or mismatched startup evidence', async t => {
  for (const name of ['signed-startup.json', 'signed-runtime.json', 'sbom.cdx.json', 'THIRD_PARTY_NOTICES.txt']) {
    const f = await fixture(t);
    await rm(path.join(f.directory, name));
    await assert.rejects(candidate('assemble', f.directory, f.env), /Candidate inventory differs/);
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
  const files = { ...f.evidence.files }; delete files['whipcode-desktop-darwin-arm64.zip'];
  await f.json('evidence.json', { ...f.evidence, files });
  await assert.rejects(candidate('assemble', f.directory, f.env), /bind every installer/);
});

test('candidate requires the exact v-prefixed standalone Linux version and update owner', async t => {
  for (const changed of [{ buildId: '1.2.3-alpha.1' }, { buildId: 'v1.2.3-beta.2' },
    { buildId: 'v1.2.3' }, { buildId: 'vv1.2.3-alpha.1' }, { updateOwner: 'desktop' }]) {
    const f = await fixture(t);
    const linux = JSON.parse(await readFile(path.join(f.directory, 'linux-runtime.json'), 'utf8'));
    await f.json('linux-runtime.json', { ...linux, ...changed });
    await assert.rejects(candidate('assemble', f.directory, f.env));
  }
});

test('unified manifest covers the exact payload and sums bind it without recursive hashing', async t => {
  const f = await fixture(t);
  await candidate('assemble', f.directory, f.env);
  const manifest = JSON.parse(await readFile(path.join(f.directory, 'artifact-manifest.json')));
  const sums = (await readFile(path.join(f.directory, 'SHA256SUMS'), 'utf8')).trim().split('\n');
  assert.equal((await readdir(f.directory)).length, 17);
  assert.equal(Object.keys(manifest.files).length, 15);
  assert(!Object.hasOwn(manifest.files, 'SHA256SUMS'));
  assert(!Object.hasOwn(manifest.files, 'artifact-manifest.json'));
  assert.equal(sums.length, 16);
  assert.equal(new Set(sums.map(line => line.split('  ')[1])).size, 16);
  for (const line of sums) {
    const [digest, name] = line.split('  ');
    assert.notEqual(name, 'SHA256SUMS');
    assert.equal(digest, createHash('sha256').update(await readFile(path.join(f.directory, name))).digest('hex'));
  }
  assert.deepEqual(Object.keys(f.evidence.files).sort(), ['RELEASES.json', 'whipcode-desktop-darwin-arm64.dmg', 'whipcode-desktop-darwin-arm64.zip']);
  await writeFile(path.join(f.directory, 'SHA256SUMS'), sums.join('\n') + '\n' + sums[0] + '\n');
  await assert.rejects(candidate('verify', f.directory, f.env), /checksums differ/);
});

test('candidate fails closed on missing CLI, foreign files, retired tags, and mismatched channel/ownership', async t => {
  for (const name of ['whipcode-linux-x64', 'whipcode-linux-arm64', 'whipcode-darwin-x64', 'whipcode-darwin-arm64', 'install.sh', 'latest.sh']) {
    const f = await fixture(t); await rm(path.join(f.directory, name));
    await assert.rejects(candidate('assemble', f.directory, f.env), /inventory differs/);
  }
  for (const name of ['unrelated.txt', 'Whip-extra.zip']) {
    const f = await fixture(t); await writeFile(path.join(f.directory, name), 'extra');
    await assert.rejects(candidate('assemble', f.directory, f.env));
  }
  for (const tag of ['desktop-v1.2.3-alpha.1', 'v0.9.0', 'v1.2.3-alpha.01', 'v1.2.3']) {
    const f = await fixture(t); await assert.rejects(candidate('assemble', f.directory, { ...f.env, RELEASE_TAG: tag }));
  }
  for (const changed of [{ channel: 'stable' }, { updateOwner: 'standalone' }]) {
    const f = await fixture(t);
    await f.json('evidence.json', { ...f.evidence, ...changed });
    await assert.rejects(candidate('assemble', f.directory, f.env));
  }
  const f = await fixture(t); await rm(path.join(f.directory, 'install.sh'));
  await symlink('whipcode-linux-x64', path.join(f.directory, 'install.sh'));
  await assert.rejects(candidate('assemble', f.directory, f.env), /Invalid candidate file/);
});

test('stable union assembles independently and a conflicting reassembly cannot bless changed bytes', async t => {
  const f = await fixture(t, '1.2.3');
  await candidate('assemble', f.directory, f.env);
  await candidate('verify', f.directory, f.env);
  const manifest = await readFile(path.join(f.directory, 'artifact-manifest.json'), 'utf8');
  await writeFile(path.join(f.directory, 'whipcode-darwin-arm64'), 'different standalone build');
  await assert.rejects(candidate('assemble', f.directory, f.env), /identity or bytes differ/);
  assert.equal(await readFile(path.join(f.directory, 'artifact-manifest.json'), 'utf8'), manifest);
});

test('runtime acceptance producer projection shares and binds channel, owner, and source identity', async t => {
  assert.deepEqual(runtimeIdentityFields, ['version', 'buildId', 'channel', 'updateOwner', 'source', 'nativeFiles', 'teamId', 'runtimeSigning']);
  const producer = await readFile(new URL('../apps/desktop/scripts/runtime-acceptance.mjs', import.meta.url), 'utf8');
  assert(producer.includes('Object.fromEntries(runtimeIdentityFields.map(key => [key, verified[key]]))'));
  const f = await fixture(t);
  assert.deepEqual(Object.keys(f.runtime.package), runtimeIdentityFields);
  assert(!Object.hasOwn(f.runtime.package, 'files'), 'fixture must model the producer projection, not full evidence');
  validateRuntimeEvidence(f.runtime, f.evidence);
  for (const field of ['channel', 'updateOwner', 'source']) {
    const missing = structuredClone(f.runtime); delete missing.package[field];
    assert.throws(() => validateRuntimeEvidence(missing, f.evidence), new RegExp(`different package: ${field}`));
  }
});

test('candidate rejects noncanonical Desktop names and signed-but-wrong current feed URLs', async t => {
  const f = await fixture(t);
  const canonical = new URL('v1.2.3-alpha.1/whipcode-desktop-darwin-arm64.zip', f.evidence.updateURL).href;
  for (const url of [canonical.replace('/v1.2.3-alpha.1/', '/v1.2.3-alpha.2/'), canonical + '?x=1',
    canonical.replace('/v1.2.3-alpha.1/', '/x/../v1.2.3-alpha.1/'),
    canonical.replace('/v1.2.3-alpha.1/', '/%761.2.3-alpha.1/'), canonical.replace('updates.example.test', 'foreign.example.test')]) {
    const bytes = JSON.stringify({ currentRelease: f.evidence.version, releases: [{ version: f.evidence.version,
      updateTo: { version: f.evidence.version, url } }] });
    await writeFile(path.join(f.directory, 'RELEASES.json'), bytes);
    f.evidence.files['RELEASES.json'] = { bytes: Buffer.byteLength(bytes), sha256: createHash('sha256').update(bytes).digest('hex') };
    await f.json('evidence.json', f.evidence);
    await assert.rejects(candidate('assemble', f.directory, f.env), /canonical versioned ZIP/);
  }
  await rm(path.join(f.directory, 'whipcode-desktop-darwin-arm64.zip'));
  await writeFile(path.join(f.directory, 'Whip-Beta-1.2.3-alpha.1.zip'), 'alias');
  await assert.rejects(candidate('assemble', f.directory, f.env), /inventory differs/);
});


test('candidate verifies both deterministic installers before hashes can legitimize tampering', async t => {
  for (const version of ['1.2.3-alpha.1', '1.2.3']) {
    for (const name of ['install.sh', 'latest.sh']) {
      const f = await fixture(t, version);
      await candidate('assemble', f.directory, f.env);
      await candidate('verify', f.directory, f.env);
      await writeFile(path.join(f.directory, name), (await readFile(path.join(f.directory, name), 'utf8')) + '\n# changed\n');
      await assert.rejects(candidate('assemble', f.directory, f.env), /Generated installer differs/);
      await assert.rejects(candidate('verify', f.directory, f.env), /Generated installer differs/);
    }
  }
});
