import assert from 'node:assert/strict';
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { sha256 } from './renderer-artifact.mjs';
import { mergeReleaseFeed, publish } from '../apps/desktop/scripts/publish.mjs';

const updateURL = 'https://updates.example.test/stable/darwin/arm64/RELEASES.json';
const prefix = 'stable/darwin/arm64/';
const release = (version, extra = {}) => ({ version, updateTo: { version, url: new URL(`v${version}/whipcode-desktop-darwin-arm64.zip`, updateURL).href, ...extra } });
const feed = (...versions) => ({ currentRelease: versions.at(-1) ?? '', releases: versions.map(version => release(version)) });

test('merges authoritative history even when the maker candidate has stale or foreign prior entries', () => {
  const current = feed('1.0.0', '1.1.0'); const candidate = feed('0.9.0', '1.2.0');
  candidate.releases[0].updateTo.url = 'https://elsewhere.invalid/old.zip';
  const merged = mergeReleaseFeed(current, candidate, '1.2.0', updateURL);
  assert.deepEqual(merged.releases.map(item => item.version), ['1.0.0', '1.1.0', '1.2.0']);
  assert.equal(merged.currentRelease, '1.2.0');
});

test('rerunning a release preserves original metadata and refuses URL changes or downgrades', () => {
  const current = feed('1.0.0', '1.2.0'); current.releases[1].updateTo.notes = 'original notes';
  assert.equal(mergeReleaseFeed(current, feed('1.2.0'), '1.2.0', updateURL).releases[1].updateTo.notes, 'original notes');
  const renamed = feed('1.2.0'); renamed.releases[0].updateTo.url = new URL('different.zip', updateURL).href;
  assert.throws(() => mergeReleaseFeed(current, renamed, '1.2.0', updateURL), /canonical versioned ZIP/);
  assert.throws(() => mergeReleaseFeed(current, feed('1.1.0'), '1.1.0', updateURL), /downgrade/);
});

test('rejects malformed feed versions, foreign paths, duplicate current candidates and missing current history', () => {
  for (const url of ['https://elsewhere.invalid/a.zip', 'https://updates.example.test/stable/darwin/arm64-other/a.zip',
    'https://user@updates.example.test/stable/darwin/arm64/a.zip', 'https://updates.example.test/stable/darwin/arm64/a.zip?mutable=1']) {
    const candidate = feed('1.2.0'); candidate.releases[0].updateTo.url = url;
    assert.throws(() => mergeReleaseFeed(feed(), candidate, '1.2.0', updateURL), /Foreign update/);
  }
  const duplicate = feed('1.2.0', '1.2.0');
  assert.throws(() => mergeReleaseFeed(feed(), duplicate, '1.2.0', updateURL), /exactly one/);
  assert.throws(() => mergeReleaseFeed({ currentRelease: '1.1.0', releases: [] }, feed('1.2.0'), '1.2.0', updateURL), /current release is missing/);
  assert.throws(() => mergeReleaseFeed(feed(), feed('broken'), 'broken', updateURL), /Invalid current release version/);
});

test('history retention keeps the latest 100 versions in semantic version order', () => {
  const versions = Array.from({ length: 105 }, (_, index) => `1.${index}.0`);
  const merged = mergeReleaseFeed(feed(...versions), feed('1.105.0'), '1.105.0', updateURL);
  assert.equal(merged.releases.length, 100);
  assert.equal(merged.releases[0].version, '1.6.0');
  assert.equal(merged.releases.at(-1).version, '1.105.0');
});

// Real subprocess argument handling and conditional writes, with no AWS access.
const awsFixture = `#!/usr/bin/env node
const fs = require('node:fs'); const path = require('node:path');
const root = process.env.WHIP_PUBLISH_TEST_ROOT; const filename = path.join(root, 'objects.json');
const state = JSON.parse(fs.readFileSync(filename)); const args = process.argv.slice(2);
const option = name => args[args.indexOf(name) + 1]; const key = option('--key');
state.calls.push(args); const save = () => fs.writeFileSync(filename, JSON.stringify(state));
const fail = message => { save(); process.stderr.write(message); process.exit(1); };
const isFeed = key.endsWith('/RELEASES.json');
if ((process.env.WHIP_PUBLISH_TEST_RACE === 'get' && args[1] === 'get-object') ||
    (process.env.WHIP_PUBLISH_TEST_RACE === 'promote' && args[1] === 'put-object' && isFeed)) {
  state.objects[key] = { etag: '"concurrent"', body: Buffer.from(JSON.stringify(state.concurrent)).toString('base64') };
}
const object = state.objects[key];
if (args[1] === 'head-object') {
  if (!object) fail('(404) Not Found');
  save(); console.log(JSON.stringify({ ETag: object.etag, ContentLength: Buffer.from(object.body, 'base64').length }));
} else if (args[1] === 'get-object') {
  if (!object || option('--if-match') !== object.etag) fail('(PreconditionFailed)');
  fs.writeFileSync(args.at(-1), Buffer.from(object.body, 'base64')); save(); console.log('{}');
} else if (args[1] === 'put-object') {
  if ((args.includes('--if-none-match') && object) || (args.includes('--if-match') && option('--if-match') !== object?.etag)) fail('(PreconditionFailed)');
  state.objects[key] = { etag: '"new"', body: fs.readFileSync(option('--body')).toString('base64') }; save(); console.log('{}');
} else fail('Unexpected AWS command');
`;
async function fixture(t, { previous = feed('1.0.0'), race = '' } = {}) {
  const root = await mkdtemp(path.join(tmpdir(), 'whip-publish-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const bin = path.join(root, 'bin'); const directory = path.join(root, 'release');
  await mkdir(bin); await mkdir(directory);
  await writeFile(path.join(bin, 'aws'), awsFixture, { mode: 0o700 });
  const objects = previous ? { [prefix + 'RELEASES.json']: { etag: '"previous"', body: Buffer.from(JSON.stringify(previous)).toString('base64') } } : {};
  const stateFile = path.join(root, 'objects.json');
  await writeFile(stateFile, JSON.stringify({ calls: [], objects, concurrent: feed('1.0.0', '1.1.0') }));
  const files = {}; const applications = {};
  for (const [name, bytes] of Object.entries({ 'whipcode-desktop-darwin-arm64.zip': 'verified ZIP', 'whipcode-desktop-darwin-arm64.dmg': 'verified DMG', 'RELEASES.json': JSON.stringify(feed('1.2.0')) })) {
    await writeFile(path.join(directory, name), bytes);
    files[name] = { bytes: Buffer.byteLength(bytes), sha256: sha256(bytes) };
    if (name !== 'RELEASES.json') applications[name] = { bundleDigest: 'a'.repeat(64), rendererDigest: 'b'.repeat(64), signed: true, notarized: true };
  }
  const evidence = { version: '1.2.0', updateURL, signed: true, notarized: true, dmgNotary: 'notary-test-id', source: { dirty: false },
    bundleDigest: 'a'.repeat(64), rendererDigest: 'b'.repeat(64), applications, files };
  await writeFile(path.join(directory, 'evidence.json'), JSON.stringify(evidence));
  const env = { PATH: `${bin}${path.delimiter}${path.dirname(process.execPath)}`, HOME: root, WHIP_PUBLISH_TEST_ROOT: root,
    WHIP_PUBLISH_TEST_RACE: race, WHIP_DESKTOP_UPDATE_URL: updateURL, WHIP_DESKTOP_BUCKET: 'whip-test-bucket' };
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async value => {
    const url = new URL(value); assert.equal(url.origin, 'https://updates.example.test');
    const state = JSON.parse(await readFile(stateFile, 'utf8')); const object = state.objects[decodeURIComponent(url.pathname.slice(1))];
    return object ? new Response(Buffer.from(object.body, 'base64')) : new Response('', { status: 404 });
  };
  t.after(() => { globalThis.fetch = originalFetch; });
  return { root, directory, evidence, env, state: async () => JSON.parse(await readFile(stateFile, 'utf8')) };
}

test('publishes immutable verified assets first, then conditionally promotes the authoritative ETag', async t => {
  const f = await fixture(t); await publish(f.directory, f.env);
  const state = await f.state(); const writes = state.calls.filter(args => args[1] === 'put-object');
  assert.deepEqual(writes.map(args => args[args.indexOf('--key') + 1]), [prefix + 'v1.2.0/whipcode-desktop-darwin-arm64.zip', prefix + 'v1.2.0/whipcode-desktop-darwin-arm64.dmg', prefix + 'RELEASES.json']);
  assert(writes.slice(0, 2).every(args => args.includes('--if-none-match')));
  assert.equal(writes[2][writes[2].indexOf('--if-match') + 1], '"previous"');
  const pinnedGet = state.calls.find(args => args[1] === 'get-object');
  assert.equal(pinnedGet[pinnedGet.indexOf('--if-match') + 1], '"previous"');
  const published = JSON.parse(Buffer.from(state.objects[prefix + 'RELEASES.json'].body, 'base64'));
  assert.deepEqual(published.releases.map(item => item.version), ['1.0.0', '1.2.0']);
});

test('a first feed uses if-none-match and never overwrites an intervening first publisher', async t => {
  for (const race of ['', 'promote']) await t.test(race || 'no race', async t => {
    const f = await fixture(t, { previous: null, race });
    if (race) await assert.rejects(publish(f.directory, f.env), /PreconditionFailed/);
    else await publish(f.directory, f.env);
    const state = await f.state(); const promotion = state.calls.find(args => args[1] === 'put-object' && args[args.indexOf('--key') + 1].endsWith('RELEASES.json'));
    assert.equal(promotion[promotion.indexOf('--if-none-match') + 1], '*');
    if (race) assert.equal(JSON.parse(Buffer.from(state.objects[prefix + 'RELEASES.json'].body, 'base64')).currentRelease, '1.1.0');
  });
});

test('a concurrent feed change during GET or promotion cannot drop the other release', async t => {
  for (const race of ['get', 'promote']) await t.test(race, async t => {
    const f = await fixture(t, { race });
    await assert.rejects(publish(f.directory, f.env), /PreconditionFailed/);
    const state = await f.state();
    const published = JSON.parse(Buffer.from(state.objects[prefix + 'RELEASES.json'].body, 'base64'));
    assert.deepEqual(published.releases.map(item => item.version), ['1.0.0', '1.1.0']);
    if (race === 'get') assert.equal(state.calls.filter(args => args[1] === 'put-object').length, 0);
  });
});

test('changed local artifacts stop publication before any writes and identical release reruns remain safe', async t => {
  const f = await fixture(t); await writeFile(path.join(f.directory, 'whipcode-desktop-darwin-arm64.zip'), 'tampered ZIP');
  await assert.rejects(publish(f.directory, f.env), /Artifact changed/);
  assert.equal((await f.state()).calls.filter(args => args[1] === 'put-object').length, 0);
  await writeFile(path.join(f.directory, 'whipcode-desktop-darwin-arm64.zip'), 'verified ZIP');
  await publish(f.directory, f.env); await publish(f.directory, f.env);
  const published = JSON.parse(Buffer.from((await f.state()).objects[prefix + 'RELEASES.json'].body, 'base64'));
  assert.equal(published.releases.filter(item => item.version === '1.2.0').length, 1);
});

test('old or mismatched archive evidence cannot authorize publication', async t => {
  for (const mutation of ['missing archive proof', 'different app tree', 'different renderer', 'unsigned archive']) await t.test(mutation, async t => {
    const f = await fixture(t); const proof = f.evidence.applications['whipcode-desktop-darwin-arm64.zip'];
    if (mutation === 'missing archive proof') delete f.evidence.applications;
    if (mutation === 'different app tree') proof.bundleDigest = 'c'.repeat(64);
    if (mutation === 'different renderer') proof.rendererDigest = 'c'.repeat(64);
    if (mutation === 'unsigned archive') proof.signed = false;
    await writeFile(path.join(f.directory, 'evidence.json'), JSON.stringify(f.evidence));
    await assert.rejects(publish(f.directory, f.env), /verified archived application/);
    assert.equal((await f.state()).calls.filter(args => args[1] === 'put-object').length, 0);
  });
});

test('staging leaves the live feed unchanged and promotion only writes the feed', async t => {
  const f = await fixture(t);
  const before = (await f.state()).objects[prefix + 'RELEASES.json'];
  await publish(f.directory, { ...f.env, WHIP_DESKTOP_PUBLISH_MODE: 'stage' });
  const staged = await f.state();
  assert.deepEqual(staged.objects[prefix + 'RELEASES.json'], before);
  await publish(f.directory, { ...f.env, WHIP_DESKTOP_PUBLISH_MODE: 'promote' });
  const writes = (await f.state()).calls.slice(staged.calls.length).filter(args => args[1] === 'put-object');
  assert.equal(writes.length, 1);
  assert.equal(writes[0][writes[0].indexOf('--key') + 1], prefix + 'RELEASES.json');
});

test('promotion cannot advertise objects that have not been staged', async t => {
  const f = await fixture(t);
  await assert.rejects(publish(f.directory, { ...f.env, WHIP_DESKTOP_PUBLISH_MODE: 'promote' }), /Published bytes differ/);
  assert.equal((await f.state()).calls.filter(args => args[1] === 'put-object').length, 0);
});

test('R2 rejects foreign endpoints and requires explicit scoped credentials before storage access', async t => {
  const f = await fixture(t);
  await assert.rejects(publish(f.directory, { ...f.env, WHIP_DESKTOP_R2_ENDPOINT: 'https://example.com' }), /Invalid R2 account endpoint/);
  await assert.rejects(publish(f.directory, { ...f.env, WHIP_DESKTOP_R2_ENDPOINT: `https://${'a'.repeat(32)}.r2.cloudflarestorage.com` }), /Configure scoped R2 credentials/);
  assert.equal((await f.state()).calls.length, 0);
});

test('R2 stage and feed retry leave the unified candidate unchanged after feed failure', async t => {
  const f = await fixture(t, { race: 'promote' });
  const original = (await readdir(f.directory)).sort();
  // Unified CLI/evidence files are not Desktop CDN payloads.
  await writeFile(path.join(f.directory, 'whipcode-linux-x64'), 'standalone');
  await writeFile(path.join(f.directory, 'artifact-manifest.json'), '{}');
  const expected = [...original, 'whipcode-linux-x64', 'artifact-manifest.json'].sort();
  await publish(f.directory, { ...f.env, WHIP_DESKTOP_PUBLISH_MODE: 'stage' });
  await assert.rejects(publish(f.directory, { ...f.env, WHIP_DESKTOP_PUBLISH_MODE: 'promote' }), /PreconditionFailed/);
  assert.deepEqual((await readdir(f.directory)).sort(), expected);
  await publish(f.directory, { ...f.env, WHIP_DESKTOP_PUBLISH_MODE: 'promote' });
  assert.deepEqual((await readdir(f.directory)).sort(), expected);
  const objects = Object.keys((await f.state()).objects);
  assert(!objects.some(name => name.includes('whipcode-linux') || name.includes('artifact-manifest')));
});

test('public alpha versions use the beta feed without changing semantic identity', () => {
  const url = updateURL.replace('/stable/', '/beta/');
  const make = version => ({ currentRelease: version, releases: [{ version, updateTo: { version,
    url: new URL(`v${version}/whipcode-desktop-darwin-arm64.zip`, url).href } }] });
  const result = mergeReleaseFeed(make('1.0.0-alpha.9'), make('1.0.0-alpha.10'), '1.0.0-alpha.10', url);
  assert.equal(result.currentRelease, '1.0.0-alpha.10');
  assert.throws(() => mergeReleaseFeed(make('1.0.0'), make('1.0.0-alpha.10'), '1.0.0-alpha.10', url), /channel differs/);
});

test('two releases use the same basename at distinct immutable version keys and preserve old URL history', async t => {
  const previous = feed('1.0.0');
  previous.releases[0].updateTo.url = new URL('Whip-1.0.0.zip', updateURL).href;
  const f = await fixture(t, { previous });
  await publish(f.directory, f.env);
  const first = (await f.state()).objects[prefix + 'v1.2.0/whipcode-desktop-darwin-arm64.zip'];
  const bytes = 'second version ZIP';
  await writeFile(path.join(f.directory, 'whipcode-desktop-darwin-arm64.zip'), bytes);
  const nextFeed = JSON.stringify(feed('1.3.0'));
  await writeFile(path.join(f.directory, 'RELEASES.json'), nextFeed);
  f.evidence.version = '1.3.0';
  f.evidence.files['whipcode-desktop-darwin-arm64.zip'] = { bytes: Buffer.byteLength(bytes), sha256: sha256(bytes) };
  f.evidence.files['RELEASES.json'] = { bytes: Buffer.byteLength(nextFeed), sha256: sha256(nextFeed) };
  await writeFile(path.join(f.directory, 'evidence.json'), JSON.stringify(f.evidence));
  await publish(f.directory, f.env); await publish(f.directory, f.env);
  const objects = (await f.state()).objects;
  assert.deepEqual(objects[prefix + 'v1.2.0/whipcode-desktop-darwin-arm64.zip'], first);
  assert.equal(Buffer.from(objects[prefix + 'v1.3.0/whipcode-desktop-darwin-arm64.zip'].body, 'base64').toString(), bytes);
  const current = JSON.parse(Buffer.from(objects[prefix + 'RELEASES.json'].body, 'base64'));
  assert.deepEqual(current.releases[0], previous.releases[0]);
  assert.equal(current.releases.at(-1).updateTo.url, new URL('v1.3.0/whipcode-desktop-darwin-arm64.zip', updateURL).href);
  await writeFile(path.join(f.directory, 'whipcode-desktop-darwin-arm64.zip'), 'different third bytes');
  f.evidence.files['whipcode-desktop-darwin-arm64.zip'] = { bytes: 21, sha256: sha256('different third bytes') };
  await writeFile(path.join(f.directory, 'evidence.json'), JSON.stringify(f.evidence));
  await assert.rejects(publish(f.directory, f.env), /Published bytes differ/);
});

test('a new candidate rejects equivalent-normalized, wrong version, query, and foreign payload URLs', () => {
  const canonical = new URL('v1.2.0/whipcode-desktop-darwin-arm64.zip', updateURL).href;
  for (const url of [canonical.replace('/v1.2.0/', '/v1.1.0/'), canonical.replace('/v1.2.0/', '/x/../v1.2.0/'),
    canonical.replace('/v1.2.0/', '/%761.2.0/'), canonical + '?download=1', canonical + '#hash',
    canonical.replace('updates.example.test', 'foreign.example.test'), new URL('whipcode-desktop-darwin-arm64.zip', updateURL).href]) {
    const next = feed('1.2.0'); next.releases[0].updateTo.url = url;
    assert.throws(() => mergeReleaseFeed(feed('1.0.0'), next, '1.2.0', updateURL));
  }
});
