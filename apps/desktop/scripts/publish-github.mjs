// Stage immutable assets in a draft; promote only the already-verified draft.
import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { lstat, readdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import semver from 'semver';
import { candidate } from './release-candidate.mjs';

const exec = promisify(execFile);
const limit = 2 ** 30;
const httpStatus = error => Number(/\bHTTP (\d{3})\b/.exec(String(error.stderr))?.[1]);
const conflict = error => httpStatus(error) ? [409, 422].includes(httpStatus(error)) : /already exists|already_exists/i.test(String(error.stderr));

async function localAsset(filename) {
  const file = path.resolve(filename); const name = path.basename(file); const stat = await lstat(file);
  // gh treats '#' as an asset label separator, including inside a parent path.
  assert(/^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$/.test(name) && !/[#\u0000-\u001f\u007f]/.test(file), 'Invalid GitHub asset filename');
  assert(stat.isFile() && !stat.isSymbolicLink() && stat.size > 0 && stat.size <= limit, `Invalid GitHub asset: ${name}`);
  const hash = createHash('sha256'); let size = 0;
  for await (const chunk of createReadStream(file)) {
    size += chunk.length; assert(size <= limit, `GitHub asset exceeds its size limit: ${name}`); hash.update(chunk);
  }
  assert.equal(size, stat.size, `GitHub asset changed while hashing: ${name}`);
  return { file, name, size, sha256: hash.digest('hex') };
}

export async function publishGitHubAssets(filenames, env = process.env) {
  const tag = env.RELEASE_TAG; const repository = env.GITHUB_REPOSITORY;
  const mode = env.WHIP_DESKTOP_PUBLISH_MODE || 'publish';
  assert(['stage', 'promote', 'publish'].includes(mode), 'Invalid publication mode');
  assert(/^v[1-9]\d*\.\d+\.\d+(?:-[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*)?$/.test(tag ?? '') &&
    semver.valid(tag) === tag.slice(1) && tag.length <= 128, 'Invalid release tag');
  assert(/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository ?? ''), 'Configure GITHUB_REPOSITORY');
  assert(env.GH_TOKEN?.trim(), 'Configure GH_TOKEN');
  assert(Array.isArray(filenames) && filenames.length > 0 && filenames.length <= 32, 'Invalid GitHub asset count');
  const assets = []; const names = new Set();
  for (const filename of filenames) {
    const asset = await localAsset(filename);
    assert(!names.has(asset.name), `Duplicate GitHub asset: ${asset.name}`); names.add(asset.name); assets.push(asset);
  }
  const childEnv = { ...env, GH_PROMPT_DISABLED: '1', GH_PAGER: 'cat' };
  const run = (args, timeout = 60_000) => exec('gh', args, { env: childEnv, timeout, killSignal: 'SIGKILL', maxBuffer: 2 << 20 });
  const api = async endpoint => JSON.parse((await run(['api', endpoint, '--hostname', 'github.com'])).stdout);
  const base = `repos/${repository}`;
  const prerelease = semver.prerelease(tag) !== null;
  // Scan bounded published history, not just /latest (which may itself be stale).
  const newestStable = async () => {
    let latest;
    for (let page = 1; page <= 100; page++) {
      const releases = await api(`${base}/releases?per_page=100&page=${page}`);
      assert(Array.isArray(releases), 'Invalid GitHub release listing');
      for (const release of releases) if (release.draft === false && release.prerelease === false &&
        /^v[1-9]\d*\.\d+\.\d+$/.test(release.tag_name ?? '') && semver.valid(release.tag_name)) {
        if (!latest || semver.gt(release.tag_name, latest)) latest = release.tag_name;
      }
      if (releases.length < 100) return latest;
    }
    throw new Error('Stable release listing exceeds the lookup limit');
  };
  // A repository/auth failure must not be mistaken for an absent release.
  await api(base);
  const verifySource = async () => {
    assert(/^[a-f0-9]{40}$/.test(env.SOURCE_SHA ?? ''), 'Configure the candidate SOURCE_SHA');
    assert.equal((await api(`${base}/commits/${encodeURIComponent(tag)}`)).sha, env.SOURCE_SHA, 'Release tag no longer identifies the candidate source');
  };
  await verifySource();
  const getRelease = async () => {
    try {
      const release = await api(`${base}/releases/tags/${encodeURIComponent(tag)}`);
      assert(Number.isSafeInteger(release.id) && release.id > 0 && release.tag_name === tag, 'Invalid GitHub release identity');
      return release;
    } catch (error) { if (httpStatus(error) !== 404) throw error; }
    // The by-tag REST endpoint only discovers published releases. Authenticated
    // listing includes drafts; do not create another draft after a partial run.
    for (let page = 1; page <= 20; page++) {
      const releases = await api(`${base}/releases?per_page=100&page=${page}`);
      assert(Array.isArray(releases) && releases.length <= 100, 'Invalid GitHub release listing');
      const matches = releases.filter(release => release.tag_name === tag);
      assert(matches.length <= 1, 'Multiple drafts identify the release tag');
      if (matches.length) {
        assert(Number.isSafeInteger(matches[0].id) && matches[0].id > 0, 'Invalid draft release identity');
        return matches[0];
      }
      if (releases.length < 100) return undefined;
    }
    throw new Error('Release listing exceeds the lookup limit; locate the existing draft before retrying');
  };
  let release = await getRelease();
  if (!release) {
    assert(mode !== 'promote', 'Stage the complete release before promotion');
    const download = `https://github.com/${repository}/releases/download/${tag}`;
    const rows = assets.filter(asset => !/\.(json|txt)$/.test(asset.name) && asset.name !== 'SHA256SUMS')
      .map(asset => `| ${asset.name} | [Download](${download}/${asset.name}) |`).join('\n');
    const notes = `## Downloads\n\n| Artifact | Link |\n| --- | --- |\n${rows}\n\nVerify downloads with [SHA256SUMS](${download}/SHA256SUMS).\n\nPinned CLI install:\n\n\`\`\`sh\ncurl -fsSL ${download}/install.sh | sh\n\`\`\`\n\nLatest stable CLI install (fails until a complete v1+ stable release exists):\n\n\`\`\`sh\ncurl -fsSL ${download}/latest.sh | sh\n\`\`\`\n`;
    try { await run(['release', 'create', tag, '--repo', `github.com/${repository}`, '--verify-tag', '--title', tag, '--generate-notes', '--notes', notes, '--draft', '--latest=false',
      ...(prerelease ? ['--prerelease'] : [])]); }
    catch (error) { if (!conflict(error)) throw error; }
    // GitHub may acknowledge creation before the draft appears in its listing.
    // Retry only absence; authentication and server failures still stop immediately.
    for (const delay of [0, 1000, 2000, 4000, 8000, 16000, 30000]) {
      if (delay) await new Promise(resolve => setTimeout(resolve, delay));
      release = await getRelease();
      if (release) break;
    }
    assert(release, 'The created GitHub release is not readable');
  }
  assert.equal(release.prerelease, prerelease, 'Release channel differs from candidate');
  const releaseId = release.id;
  const listAssets = async () => {
    const pages = JSON.parse((await run(['api', `${base}/releases/${releaseId}/assets?per_page=100`, '--hostname', 'github.com', '--paginate', '--slurp'])).stdout);
    assert(Array.isArray(pages) && pages.every(Array.isArray), 'Invalid GitHub asset listing');
    const listed = pages.flat(); assert(listed.length <= 500, 'GitHub release has too many assets');
    const result = new Map();
    for (const asset of listed) {
      assert(Number.isSafeInteger(asset.id) && asset.id > 0 && typeof asset.name === 'string' && !result.has(asset.name), 'Invalid or duplicate GitHub asset identity');
      assert(names.has(asset.name), `Unexpected GitHub release asset: ${asset.name}`);
      result.set(asset.name, asset);
    }
    return result;
  };
  const verify = async (remote, expected) => {
    assert(remote?.state === 'uploaded' && remote.size === expected.size, `GitHub asset differs or is incomplete: ${expected.name}`);
    // Download by immutable asset ID, streaming directly into a bounded digest.
    // No archive-sized buffers or temporary downloads survive failures.
    await new Promise((resolve, reject) => {
      const child = spawn('gh', ['api', `${base}/releases/assets/${remote.id}`, '--hostname', 'github.com', '--header', 'Accept: application/octet-stream'],
        { env: childEnv, stdio: ['ignore', 'pipe', 'pipe'], timeout: 10 * 60_000, killSignal: 'SIGKILL' });
      const hash = createHash('sha256'); let size = 0; let stderr = ''; let failure;
      child.stdout.on('data', chunk => {
        if (failure) return;
        size += chunk.length;
        if (size > expected.size) { failure = new Error(`GitHub asset exceeds expected size: ${expected.name}`); child.kill('SIGKILL'); }
        else hash.update(chunk);
      });
      child.stderr.on('data', chunk => { stderr = (stderr + chunk.toString()).slice(0, 16 << 10); });
      child.once('error', reject);
      child.once('close', (code, signal) => {
        if (failure) { reject(failure); return; }
        if (code !== 0) { reject(new Error(`GitHub asset download failed (${signal ?? code}): ${stderr}`)); return; }
        if (size !== expected.size || hash.digest('hex') !== expected.sha256) { reject(new Error(`GitHub asset bytes differ: ${expected.name}`)); return; }
        resolve();
      });
    });
  };
  const existing = await listAssets(); const verified = new Map();
  // Find changed existing content before uploading any missing assets.
  for (const asset of assets) if (existing.has(asset.name)) {
    const remote = existing.get(asset.name); await verify(remote, asset); verified.set(asset.name, remote.id);
  }
  for (const asset of assets) if (!verified.has(asset.name)) {
    assert(release.draft && mode !== 'promote', `Stage all assets before publication: ${asset.name}`);
    try { await run(['release', 'upload', tag, '--repo', `github.com/${repository}`, asset.file], 10 * 60_000); }
    catch (error) { if (!conflict(error)) throw error; }
    // A competing upload is acceptable only when its exact bytes match.
    const remote = (await listAssets()).get(asset.name);
    await verify(remote, asset); verified.set(asset.name, remote.id);
  }
  const reread = await getRelease();
  assert.equal(reread?.id, releaseId, 'GitHub release changed during publication');
  assert.equal(reread.prerelease, prerelease, 'Release channel changed during publication');
  release = reread;
  const final = await listAssets();
  for (const asset of assets) assert.equal(final.get(asset.name)?.id, verified.get(asset.name), `GitHub asset changed during publication: ${asset.name}`);
  await verifySource();
  if (mode !== 'stage' && release.draft) {
    const newest = prerelease ? undefined : await newestStable();
    const latest = !prerelease && (!newest || semver.gt(tag, newest));
    await run(['release', 'edit', tag, '--repo', `github.com/${repository}`, '--draft=false', `--latest=${latest}`, `--prerelease=${prerelease}`]);
    const published = await getRelease();
    assert(published?.id === releaseId && published.draft === false, 'GitHub release promotion was not confirmed');
  }
  console.log(`GitHub release ${tag}: verified ${assets.length} immutable assets.`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const files = process.argv.slice(2);
  assert(files.length && files.every(file => path.dirname(path.resolve(file)) === path.dirname(path.resolve(files[0]))), 'Use one complete candidate directory');
  const directory = path.dirname(path.resolve(files[0]));
  await candidate('verify', directory);
  assert.deepEqual(files.map(file => path.basename(file)).sort(), (await readdir(directory)).sort(), 'Publish the entire candidate');
  await publishGitHubAssets(files);
}
