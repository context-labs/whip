// Invoked only by the serialized release workflow after all build gates pass.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { lstat, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import semver from 'semver';

const exec = promisify(execFile);
const limit = 1 << 30;
function feedURL(value) {
  const url = new URL(value);
  assert(url.protocol === 'https:' && !url.username && !url.password && !url.search && !url.hash && url.pathname.endsWith('/RELEASES.json'), 'Invalid release feed URL');
  return url;
}
function validateRelease(release, base) {
  assert(semver.valid(release?.version), 'Invalid release version');
  const update = release.updateTo; assert(update?.version === release.version, 'Inconsistent update version');
  const url = new URL(update.url);
  assert(url.origin === base.origin && url.pathname.startsWith(base.pathname) && !url.username && !url.password && !url.search && !url.hash && url.pathname.endsWith('.zip'), 'Foreign update artifact URL');
}
export function mergeReleaseFeed(previous, candidate, version, updateURL) {
  const base = new URL('./', feedURL(updateURL));
  assert(semver.valid(version), 'Invalid current release version');
  assert(previous && Array.isArray(previous.releases) && previous.releases.length <= 200, 'Invalid published release feed');
  for (const release of previous.releases) validateRelease(release, base);
  if (previous.currentRelease) {
    assert(previous.releases.some(release => release.version === previous.currentRelease), 'Published current release is missing');
    assert(semver.gte(version, previous.currentRelease), 'Feed promotion cannot downgrade the current release');
  }
  assert(candidate.currentRelease === version && Array.isArray(candidate.releases), 'Invalid candidate release feed');
  const selected = candidate.releases.filter(release => release.version === version);
  assert.equal(selected.length, 1, 'Candidate must contain exactly one current version');
  validateRelease(selected[0], base);
  const existing = previous.releases.find(release => release.version === version);
  if (existing) assert.equal(existing.updateTo.url, selected[0].updateTo.url, 'Published versions have immutable URLs');
  // Keep bounded recent history; immutable old ZIPs remain available for recovery.
  const releases = [...previous.releases.filter(release => release.version !== version), existing ?? selected[0]]
    .sort((a, b) => semver.compare(a.version, b.version)).slice(-100);
  return { currentRelease: version, releases };
}
async function response(url, bytesLimit) {
  const result = await fetch(url, { redirect: 'error', signal: AbortSignal.timeout(60_000), headers: { 'Cache-Control': 'no-cache' } });
  if (result.status === 404) return undefined;
  assert(result.ok, `Release download failed: HTTP ${result.status}`);
  let size = 0; const chunks = [];
  for await (const chunk of result.body) {
    size += chunk.length; assert(size <= bytesLimit, 'Release download exceeded its limit'); chunks.push(chunk);
  }
  return Buffer.concat(chunks);
}
async function digest(filename) {
  const hash = createHash('sha256');
  for await (const bytes of createReadStream(filename)) hash.update(bytes);
  return hash.digest('hex');
}
async function publishedFeed(bucket, key, directory, env) {
  let head;
  try {
    head = JSON.parse((await exec('aws', ['s3api', 'head-object', '--bucket', bucket, '--key', key], { env, timeout: 60_000 })).stdout);
  } catch (error) {
    if (/\(404\)|\(NoSuchKey\)|\(NotFound\)/.test(String(error.stderr))) return { feed: { currentRelease: '', releases: [] } };
    throw error;
  }
  assert(Number.isSafeInteger(head.ContentLength) && head.ContentLength <= 2 << 20 && typeof head.ETag === 'string' &&
    head.ETag.length <= 256 && !/[\r\n]/.test(head.ETag), 'Invalid published feed metadata');
  const previous = path.join(directory, 'RELEASES.previous.json');
  await exec('aws', ['s3api', 'get-object', '--bucket', bucket, '--key', key, '--if-match', head.ETag, previous], { env, timeout: 60_000 });
  const bytes = await readFile(previous); assert(bytes.length <= 2 << 20, 'Published feed exceeds its limit');
  return { feed: JSON.parse(bytes), etag: head.ETag };
}
export async function publish(directory, env = process.env) {
  const mode = env.WHIP_DESKTOP_PUBLISH_MODE || 'publish';
  assert(['stage', 'promote', 'publish'].includes(mode), 'Invalid publication mode');
  if (env.WHIP_DESKTOP_R2_ENDPOINT) {
    const endpoint = new URL(env.WHIP_DESKTOP_R2_ENDPOINT);
    assert(endpoint.protocol === 'https:' && /^[a-f0-9]{32}\.r2\.cloudflarestorage\.com$/.test(endpoint.hostname) &&
      endpoint.pathname === '/' && !endpoint.port && !endpoint.username && !endpoint.password && !endpoint.search && !endpoint.hash, 'Invalid R2 account endpoint');
    assert(env.WHIP_DESKTOP_R2_ACCESS_KEY_ID && env.WHIP_DESKTOP_R2_SECRET_ACCESS_KEY, 'Configure scoped R2 credentials');
    env = { ...env, AWS_ENDPOINT_URL: endpoint.origin, AWS_REGION: 'auto', AWS_DEFAULT_REGION: 'auto',
      AWS_ACCESS_KEY_ID: env.WHIP_DESKTOP_R2_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY: env.WHIP_DESKTOP_R2_SECRET_ACCESS_KEY };
    delete env.AWS_SESSION_TOKEN;
  }
  const url = feedURL(env.WHIP_DESKTOP_UPDATE_URL);
  const bucket = env.WHIP_DESKTOP_BUCKET;
  assert(/^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/.test(bucket ?? ''), 'Configure WHIP_DESKTOP_BUCKET');
  const prefix = decodeURIComponent(url.pathname.slice(1, -'RELEASES.json'.length));
  assert(prefix && !/[\\\u0000-\u0020]/.test(prefix) && !prefix.split('/').some(part => part.startsWith('.')), 'Invalid feed bucket prefix');
  const evidence = JSON.parse(await readFile(path.join(directory, 'evidence.json'), 'utf8'));
  assert(evidence.signed && evidence.notarized && evidence.dmgNotary && evidence.source?.dirty === false, 'Only verified, notarized clean releases may be published');
  const candidate = JSON.parse(await readFile(path.join(directory, 'RELEASES.json'), 'utf8'));
  // Read the authoritative object with its ETag; a CDN response is not a lock.
  const current = await publishedFeed(bucket, prefix + 'RELEASES.json', directory, env);
  const feed = mergeReleaseFeed(current.feed, candidate, evidence.version, url.href);
  const base = new URL('./', url);
  const names = Object.keys(evidence.files);
  assert(names.some(name => name.endsWith('.zip')) && names.some(name => name.endsWith('.dmg')), 'Missing distribution artifacts');
  assert(/^[a-f0-9]{64}$/.test(evidence.bundleDigest), 'Missing verified application tree');
  for (const name of names) {
    assert(name === path.basename(name) && !name.startsWith('.') && (/\.(zip|dmg)$/.test(name) || name === 'RELEASES.json'), 'Invalid artifact filename');
    if (name !== 'RELEASES.json') {
      const application = evidence.applications?.[name];
      assert(application?.bundleDigest === evidence.bundleDigest && application.rendererDigest === evidence.rendererDigest &&
        application.signed === true && application.notarized === true, `Missing verified archived application: ${name}`);
    }
    const file = path.join(directory, name); const stat = await lstat(file); const expected = evidence.files[name];
    assert(stat.isFile() && !stat.isSymbolicLink() && stat.size <= limit && stat.size === expected.bytes && await digest(file) === expected.sha256, `Artifact changed: ${name}`);
  }
  const selected = feed.releases.find(release => release.version === evidence.version);
  assert(names.filter(name => name.endsWith('.zip')).some(name => new URL(encodeURIComponent(name), base).href === selected.updateTo.url), 'Feed does not identify the verified ZIP');
  for (const name of names.filter(name => name !== 'RELEASES.json')) {
    const expected = evidence.files[name];
    if (mode !== 'promote') try {
      await exec('aws', ['s3api', 'put-object', '--bucket', bucket, '--key', prefix + name, '--body', path.join(directory, name),
        '--if-none-match', '*', '--metadata', `sha256=${expected.sha256}`, '--content-type', name.endsWith('.zip') ? 'application/zip' : 'application/x-apple-diskimage',
        '--cache-control', 'public,max-age=31536000,immutable'], { env, timeout: 10 * 60_000 });
    } catch (error) {
      // A retried release may reuse identical bytes; never overwrite a version.
      if (!/PreconditionFailed|ConditionalRequestConflict/.test(String(error.stderr))) throw error;
    }
    const remote = await response(new URL(encodeURIComponent(name), base), expected.bytes);
    assert(remote && remote.length === expected.bytes && createHash('sha256').update(remote).digest('hex') === expected.sha256, `Published bytes differ: ${name}`);
  }
  if (mode === 'stage') { console.log(`Staged verified Whip ${evidence.version}; live update feed unchanged.`); return; }
  // Conditional promotion also protects against a writer outside our serialized
  // workflow. On conflict, retry from its new history; never overwrite blindly.
  const feedFile = path.join(directory, 'RELEASES.promote.json');
  await writeFile(feedFile, JSON.stringify(feed) + '\n');
  await exec('aws', ['s3api', 'put-object', '--bucket', bucket, '--key', prefix + 'RELEASES.json', '--body', feedFile,
    ...(current.etag ? ['--if-match', current.etag] : ['--if-none-match', '*']),
    '--content-type', 'application/json', '--cache-control', 'no-cache'], { env, timeout: 60_000 });
  const promoted = await response(url, 2 << 20);
  assert(promoted && promoted.equals(await readFile(feedFile)), 'Promoted feed is not visible');
  console.log(`Published verified Whip ${evidence.version}; update feed promoted last.`);
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert(process.argv[2], 'Usage: publish.mjs /path/to/release');
  await publish(path.resolve(process.argv[2]));
}
