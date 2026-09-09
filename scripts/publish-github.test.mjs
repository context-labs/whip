import assert from 'node:assert/strict';
import { mkdir, mkdtemp, readFile, rm, symlink, truncate, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { publishGitHubAssets } from '../apps/desktop/scripts/publish-github.mjs';

// All gh invocations use this local executable; no GitHub credentials or network.
const ghFixture = `#!/usr/bin/env node
const fs = require('node:fs'); const path = require('node:path');
const file = path.join(process.env.WHIP_GITHUB_TEST_ROOT, 'state.json'); const state = JSON.parse(fs.readFileSync(file));
const args = process.argv.slice(2); state.calls.push(args); const mode = state.mode;
const save = () => fs.writeFileSync(file, JSON.stringify(state));
const fail = message => { save(); process.stderr.write(message); process.exit(1); };
const json = value => { save(); console.log(JSON.stringify(value)); };
const add = (name, bytes) => { const asset = { id: state.nextId++, name, state: 'uploaded', size: bytes.length, body: bytes.toString('base64') }; state.assets.push(asset); return asset; };
if (args[0] === 'api') {
  const endpoint = args[1];
  if (endpoint === 'repos/context-labs/whip') {
    if (mode === 'repo-auth') fail('gh: Bad credentials (HTTP 401)');
    json({ full_name: 'context-labs/whip' });
  } else if (endpoint.includes('/commits/')) {
    json({ sha: process.env.SOURCE_SHA });
  } else if (endpoint.includes('/releases/tags/')) {
    if (mode === 'release-auth') fail('gh: Forbidden (HTTP 403)');
    if (mode === 'release-network') fail('gh: Service unavailable (HTTP 503)');
    if (!state.release || state.release.draft) fail('gh: Not Found (HTTP 404)');
    json(state.release);
  } else if (endpoint.includes('/releases?per_page=')) {
    if (mode === 'create-delayed' && state.release && state.hiddenListings-- > 0) json([]);
    else json(state.release ? [state.release] : []);
  } else if (/\\/releases\\/\\d+\\/assets\\?/.test(endpoint)) {
    if (mode === 'list-auth') fail('gh: Forbidden (HTTP 403)');
    const assets = state.assets.map(({ body, ...metadata }) => metadata);
    json([assets.slice(0, 1), assets.slice(1)]);
  } else if (/\\/releases\\/assets\\/\\d+$/.test(endpoint)) {
    if (mode === 'download-auth') fail('gh: Forbidden (HTTP 403)');
    const id = Number(endpoint.split('/').at(-1)); const asset = state.assets.find(item => item.id === id);
    if (!asset) fail('gh: Not Found (HTTP 404)');
    let bytes = Buffer.from(asset.body, 'base64');
    if (mode === 'overlong') bytes = Buffer.concat([bytes, Buffer.from('extra')]);
    if (mode === 'truncated') bytes = bytes.subarray(0, bytes.length - 1);
    if (mode === 'replace-after-read') asset.id = state.nextId++;
    save(); process.stdout.write(bytes);
  } else fail('Unexpected API endpoint');
} else if (args[0] === 'release' && args[1] === 'create') {
  if (mode === 'create-auth') fail('gh: Forbidden (HTTP 403)');
  state.release = { id: 10, tag_name: args[2], name: mode === 'create-race' ? 'Human title' : args[2], body: 'Generated or existing notes', draft: args.includes('--draft'), prerelease: args.includes('--prerelease') };
  if (mode === 'create-race') fail('gh: Validation failed (HTTP 422): already_exists');
  state.hiddenListings = 2; save(); console.log('created');
} else if (args[0] === 'release' && args[1] === 'upload') {
  if (args.includes('--clobber')) fail('Never clobber');
  if (mode === 'upload-auth') fail('gh: Forbidden (HTTP 403)');
  if (mode === 'upload-network') fail('gh: Service unavailable (HTTP 503)');
  const filename = args.at(-1); const name = path.basename(filename); const bytes = fs.readFileSync(filename);
  if (state.assets.some(asset => asset.name === name)) fail('asset already exists');
  if (mode === 'upload-race-changed') add(name, Buffer.alloc(bytes.length, 88)); else add(name, bytes);
  if (mode.startsWith('upload-race')) fail(mode === 'upload-race-cli' ? 'asset already exists' : 'gh: Validation failed (HTTP 422): already_exists');
  save(); console.log('uploaded');
} else if (args[0] === 'release' && args[1] === 'edit') {
  if (mode === 'promote-error') fail('gh: Service unavailable (HTTP 503)');
  state.release.draft = false; state.release.prerelease = args.includes('--prerelease=true'); save();
} else fail('Unexpected mutation or gh command');
`;

async function fixture(t, { existing = {}, release = true, mode = '' } = {}) {
  const root = await mkdtemp(path.join(tmpdir(), 'whip-github-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const bin = path.join(root, 'bin'); await mkdir(bin);
  await writeFile(path.join(bin, 'gh'), ghFixture, { mode: 0o700 });
  const metadata = { id: 10, tag_name: 'v1.2.3', name: 'Hand-written release title', body: 'Keep curated notes', draft: true, prerelease: true };
  let nextId = 100;
  const assets = Object.entries(existing).map(([name, body]) => ({ id: nextId++, name, size: Buffer.byteLength(body), state: 'uploaded', body: Buffer.from(body).toString('base64') }));
  const stateFile = path.join(root, 'state.json');
  await writeFile(stateFile, JSON.stringify({ release: release ? metadata : null, assets, nextId, mode, calls: [] }));
  const env = { PATH: `${bin}${path.delimiter}${path.dirname(process.execPath)}`, HOME: root, WHIP_GITHUB_TEST_ROOT: root,
    RELEASE_TAG: 'v1.2.3', GITHUB_REPOSITORY: 'context-labs/whip', GH_TOKEN: 'local-fixture-token', WHIP_DESKTOP_PUBLISH_MODE: 'stage' };
  const local = async (name, bytes = name) => { const file = path.join(root, name); await mkdir(path.dirname(file), { recursive: true }); await writeFile(file, bytes); return file; };
  return { root, env, metadata, local, state: async () => JSON.parse(await readFile(stateFile, 'utf8')) };
}
const writes = state => state.calls.filter(args => args[0] === 'release');

test('identical GitHub reruns verify every asset and preserve all existing release metadata', async t => {
  const f = await fixture(t, { existing: { 'whip-linux-x64': 'CLI', 'Whip Beta.zip': 'desktop', 'unrelated.txt': 'keep' } });
  const files = [await f.local('whip-linux-x64', 'CLI'), await f.local('Whip Beta.zip', 'desktop')];
  await publishGitHubAssets(files, f.env); await publishGitHubAssets(files, f.env);
  const state = await f.state(); assert.deepEqual(state.release, f.metadata); assert.deepEqual(writes(state), []);
  assert.equal(state.calls.filter(args => args[0] === 'api' && /\/releases\/assets\/\d+$/.test(args[1])).length, 4);
  assert(state.assets.some(asset => asset.name === 'unrelated.txt'));
});

test('uploads only missing artifacts and a retry verifies the previously uploaded bytes', async t => {
  const f = await fixture(t, { existing: { 'whip-linux-x64': 'CLI' } });
  const contents = { 'whip-linux-x64': 'CLI', 'whip-linux-arm64': 'arm', 'whip-darwin-x64': 'mac x64', 'whip-darwin-arm64': 'mac arm',
    SHA256SUMS: 'checksums', 'install.sh': 'installer', 'Whip.dmg': 'DMG', 'Whip.zip': 'ZIP', 'RELEASES.json': 'feed', 'evidence.json': 'evidence' };
  const files = []; for (const [name, bytes] of Object.entries(contents)) files.push(await f.local(name, bytes));
  await publishGitHubAssets(files, f.env); await publishGitHubAssets(files, f.env);
  const state = await f.state(); assert.equal(writes(state).length, files.length - 1);
  assert(writes(state).every(args => args[1] === 'upload' && !args.includes('--clobber')));
  assert.deepEqual(state.release, f.metadata);
  assert.deepEqual(state.assets.map(asset => asset.name).sort(), Object.keys(contents).sort());
});

test('different existing bytes fail before any missing asset is uploaded', async t => {
  const f = await fixture(t, { existing: { 'Whip.zip': 'old' } });
  await assert.rejects(publishGitHubAssets([await f.local('missing.dmg', 'new'), await f.local('Whip.zip', 'new')], f.env), /bytes differ/);
  assert.deepEqual(writes(await f.state()), []);
});

test('creates an absent release with the existing notes behavior and safely accepts a create race', async t => {
  for (const mode of ['', 'create-race', 'create-delayed']) await t.test(mode || 'created', async t => {
    const f = await fixture(t, { release: false, mode });
    await publishGitHubAssets([await f.local('Whip.zip')], f.env);
    const state = await f.state(); const create = writes(state).find(args => args[1] === 'create');
    assert(create.includes('--generate-notes') && create.includes('--verify-tag'));
    assert.equal(state.release.name, mode === 'create-race' ? 'Human title' : 'v1.2.3');
    assert.equal(writes(state).filter(args => args[1] === 'create').length, 1);
    assert(!state.calls.some(args => args[1] === 'edit'));
  });
});

test('upload races accept identical bytes, including CLI conflicts, and reject a different winner', async t => {
  for (const mode of ['upload-race-identical', 'upload-race-cli', 'upload-race-changed']) await t.test(mode, async t => {
    const f = await fixture(t, { mode }); const files = [await f.local('Whip.zip', 'expected')];
    if (mode.endsWith('changed')) await assert.rejects(publishGitHubAssets(files, f.env), /bytes differ/);
    else await publishGitHubAssets(files, f.env);
    assert.equal(writes(await f.state()).length, 1);
  });
});

test('authentication and network errors never become release absence or successful upload races', async t => {
  for (const mode of ['repo-auth', 'release-auth', 'release-network', 'list-auth', 'download-auth', 'create-auth', 'upload-auth', 'upload-network'])
    await t.test(mode, async t => {
      const f = await fixture(t, { mode, release: mode !== 'create-auth', existing: mode === 'download-auth' ? { 'Whip.zip': 'expected' } : {} });
      await assert.rejects(publishGitHubAssets([await f.local('Whip.zip', 'expected')], f.env), /HTTP 401|HTTP 403|HTTP 503/);
      const mutations = writes(await f.state());
      if (!mode.startsWith('create-') && !mode.startsWith('upload-')) assert.deepEqual(mutations, []);
      else assert.equal(mutations.length, 1);
    });
});

test('bounds streamed downloads and rejects truncation or asset replacement after verification', async t => {
  for (const mode of ['overlong', 'truncated', 'replace-after-read']) await t.test(mode, async t => {
    const f = await fixture(t, { mode, existing: { 'Whip.zip': 'expected' } });
    await assert.rejects(publishGitHubAssets([await f.local('Whip.zip', 'expected')], f.env), /exceeds expected size|bytes differ|changed during publication/);
    assert.deepEqual(writes(await f.state()), []);
  });
});

test('invalid names, duplicate names, symlinks and oversized local assets fail before GitHub access', async t => {
  for (const kind of ['label', 'duplicate', 'symlink', 'oversized']) await t.test(kind, async t => {
    const f = await fixture(t); let files;
    if (kind === 'label') files = [await f.local('Whip.zip#label')];
    if (kind === 'duplicate') files = [await f.local('one/Whip.zip'), await f.local('two/Whip.zip')];
    if (kind === 'symlink') { const target = await f.local('original'); const link = path.join(f.root, 'Whip.zip'); await symlink(target, link); files = [link]; }
    if (kind === 'oversized') { const file = await f.local('Whip.zip'); await truncate(file, 2 ** 30 + 1); files = [file]; }
    await assert.rejects(publishGitHubAssets(files, f.env), /Invalid GitHub asset|Duplicate GitHub asset/);
    assert.deepEqual((await f.state()).calls, []);
  });
});

test('desktop release stages privately, then promotes the verified draft without rebuilding or uploading', async t => {
  const f = await fixture(t, { release: false });
  const env = { ...f.env, RELEASE_TAG: 'desktop-v1.2.3-beta.1', SOURCE_SHA: 'a'.repeat(40) };
  const files = [await f.local('Whip.zip'), await f.local('Whip.dmg')];
  await publishGitHubAssets(files, env);
  const staged = await f.state();
  assert.equal(staged.release.draft, true);
  assert.equal(staged.release.prerelease, true);
  assert(writes(staged).find(call => call[1] === 'create').includes('--draft'));
  await publishGitHubAssets(files, { ...env, WHIP_DESKTOP_PUBLISH_MODE: 'promote' });
  const promoted = await f.state();
  assert.equal(promoted.release.draft, false);
  assert.equal(promoted.release.prerelease, true);
  assert.deepEqual(promoted.calls.slice(staged.calls.length).filter(call => call[0] === 'release'),
    [['release', 'edit', env.RELEASE_TAG, '--repo', 'github.com/context-labs/whip', '--draft=false', '--latest=false', '--prerelease=true']]);
  await publishGitHubAssets(files, { ...env, WHIP_DESKTOP_PUBLISH_MODE: 'promote' });
  assert.equal(writes(await f.state()).filter(call => call[1] === 'edit').length, 1);
});

test('promotion refuses missing assets and failed promotion leaves a complete retryable draft', async t => {
  const f = await fixture(t, { mode: 'promote-error' });
  const file = await f.local('Whip.zip');
  await assert.rejects(publishGitHubAssets([file], { ...f.env, WHIP_DESKTOP_PUBLISH_MODE: 'promote' }), /Stage all assets/);
  assert.deepEqual(writes(await f.state()), []);
  await publishGitHubAssets([file], f.env);
  await assert.rejects(publishGitHubAssets([file], { ...f.env, WHIP_DESKTOP_PUBLISH_MODE: 'promote' }), /HTTP 503/);
  assert.equal((await f.state()).release.draft, true);
  assert.equal((await f.state()).assets.length, 1);
});


test('standalone stable CLI promotion remains latest while desktop never replaces it', async t => {
  const f = await fixture(t, { release: false });
  await publishGitHubAssets([await f.local('whip-linux-x64')], { ...f.env, WHIP_DESKTOP_PUBLISH_MODE: 'publish' });
  const edited = (await f.state()).calls.find(args => args[0] === 'release' && args[1] === 'edit');
  assert(edited.includes('--latest=true'));
});
