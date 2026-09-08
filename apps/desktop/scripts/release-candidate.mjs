import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { lstat, readFile, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

async function hash(file) {
  const stat = await lstat(file);
  assert(stat.isFile() && !stat.isSymbolicLink() && stat.size > 0 && stat.size <= 2 ** 30, 'Invalid candidate file');
  let bytes = 0; const hash = createHash('sha256');
  for await (const chunk of createReadStream(file)) { bytes += chunk.length; assert(bytes <= stat.size, 'Candidate file grew'); hash.update(chunk); }
  assert.equal(bytes, stat.size, 'Candidate file changed');
  return { bytes, sha256: hash.digest('hex') };
}

export async function candidate(mode, directory, env = process.env) {
  assert(['assemble', 'verify'].includes(mode), 'Use assemble or verify');
  const match = /^desktop-v(\d+\.\d+\.\d+(?:-[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?)$/.exec(env.RELEASE_TAG ?? '');
  assert(match && /^[a-f0-9]{40}$/.test(env.SOURCE_SHA ?? ''), 'Invalid release identity');
  const names = (await readdir(directory)).sort();
  assert(names.length <= 20 && names.every(name => /^[A-Za-z0-9][A-Za-z0-9._ -]{0,199}$/.test(name)), 'Invalid candidate filenames');
  const json = async name => {
    assert((await lstat(path.join(directory, name))).size <= 2 << 20, 'Candidate metadata exceeds its limit');
    return JSON.parse(await readFile(path.join(directory, name), 'utf8'));
  };
  for (const name of ['signed-startup.json', 'sbom.cdx.json', 'THIRD_PARTY_NOTICES.txt'])
    assert(names.includes(name), `Missing required acceptance artifact: ${name}`);
  const evidence = await json('evidence.json'); const linux = await json('linux-runtime.json');
  const startup = await json('signed-startup.json');
  assert(startup.completed === true && startup.interrupted === false, 'Signed startup acceptance did not complete');
  for (const key of ['version', 'buildId', 'source', 'rendererDigest', 'compatibility', 'nativeFiles', 'teamId'])
    assert.deepEqual(startup.evidence?.[key], evidence[key], `Startup tested a different package: ${key}`);
  assert(startup.evidence?.signed && startup.evidence?.notarized, 'Startup did not test a signed, notarized app');
  assert(startup.requested?.samples >= 30 && startup.requested?.firstSamples >= 1, 'Startup sample count is insufficient');
  assert(startup.results?.length > 0 && startup.results.every(result => result.state === 'complete' && result.launchServicesExit?.code === 0), 'Startup contains a failed launch');
  for (const scenario of ['first-launch', 'warm-attach', 'retained-start']) {
    const stats = startup.statistics?.[scenario];
    assert(stats?.failed === 0 && stats.attempted >= (scenario === 'first-launch' ? 1 : 30), `Missing startup scenario: ${scenario}`);
  }
  assert(startup.cleanup?.length > 0 && startup.cleanup.every(item => item.state === 'stopped'), 'Startup fixture cleanup failed');
  const sbom = await json('sbom.cdx.json');
  assert(sbom.bomFormat === 'CycloneDX' && sbom.components?.length > 0, 'Missing dependency inventory');
  assert.equal(evidence.version, match[1]); assert.equal(evidence.buildId, match[1]);
  assert.equal(evidence.source?.commit, env.SOURCE_SHA); assert.equal(evidence.source?.dirty, false);
  assert(evidence.signed && evidence.notarized && evidence.dmgNotary, 'Candidate must be signed and notarized');
  assert.equal(linux.buildId, evidence.buildId); assert.equal(linux.distribution, 'whipcode'); assert.equal(linux.updateOwner, 'standalone');
  assert.equal(linux.source?.commit, env.SOURCE_SHA); assert.equal(linux.source?.dirty, false);
  assert(linux.smoke?.embeddedRenderer && linux.smoke?.daemonReady, 'Linux acceptance is missing');
  assert.equal(linux.rendererDigest, evidence.rendererDigest);
  for (const field of ['protocolMajor', 'protocolMinor', 'schemaVersion']) assert.equal(linux[field], evidence.compatibility[field]);
  for (const suffix of ['.dmg', '.zip']) assert.equal(names.filter(name => name.endsWith(suffix)).length, 1);
  assert.deepEqual(Object.keys(evidence.files).sort(), names.filter(name => name.endsWith('.dmg') || name.endsWith('.zip') || name === 'RELEASES.json').sort(), 'Signed evidence must bind every installer and feed');
  for (const [name, expected] of Object.entries(evidence.files)) {
    assert(names.includes(name), `Missing signed artifact: ${name}`);
    assert.deepEqual(await hash(path.join(directory, name)), expected, `Signed artifact changed: ${name}`);
  }
  const feed = await json('RELEASES.json');
  assert.equal(feed.currentRelease, evidence.version);
  const files = {};
  for (const name of names.filter(name => !['artifact-manifest.json', 'SHA256SUMS'].includes(name))) files[name] = await hash(path.join(directory, name));
  assert(files['whipcode-linux-x64'], 'Matching Linux backend is missing');
  const manifest = { schema: 1, tag: env.RELEASE_TAG, source: env.SOURCE_SHA, version: match[1], files };
  const manifestFile = path.join(directory, 'artifact-manifest.json');
  if (mode === 'assemble') await writeFile(manifestFile, JSON.stringify(manifest, null, 2) + '\n');
  else assert.deepEqual(await json('artifact-manifest.json'), manifest, 'Candidate identity or bytes differ');
  files['artifact-manifest.json'] = await hash(manifestFile);
  const sums = Object.entries(files).sort(([a], [b]) => a.localeCompare(b, 'en')).map(([name, file]) => `${file.sha256}  ${name}`).join('\n') + '\n';
  if (mode === 'assemble') await writeFile(path.join(directory, 'SHA256SUMS'), sums);
  else assert.equal(await readFile(path.join(directory, 'SHA256SUMS'), 'utf8'), sums, 'Candidate checksums differ');
  console.log(`Verified ${env.RELEASE_TAG} from ${env.SOURCE_SHA}: ${Object.keys(files).length} final artifacts`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url))
  await candidate(process.argv[2], path.resolve(process.argv[3]));
