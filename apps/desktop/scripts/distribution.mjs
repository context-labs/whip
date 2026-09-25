import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { createHash } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import { chmod, copyFile, lstat, mkdir, mkdtemp, readFile, readdir, readlink, realpath, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { pipeline } from 'node:stream/promises';
import { promisify } from 'node:util';
import yauzl from 'yauzl';
import semver from 'semver';
import { sha256 } from '../../../scripts/renderer-artifact.mjs';
import { verifyDesktop } from './verify.mjs';

export const desktopArchiveNames = Object.freeze(['whipcode-desktop-darwin-arm64.dmg', 'whipcode-desktop-darwin-arm64.zip']);

export function desktopArtifactURL(updateURL, version, name) {
  assert(desktopArchiveNames.includes(name), 'Invalid Desktop artifact name');
  assert(typeof version === 'string' && semver.valid(version) === version && /^[1-9]\d*\./.test(version), 'Invalid Desktop artifact version');
  const feed = new URL(updateURL);
  assert(feed.href === updateURL && feed.protocol === 'https:' && !feed.username && !feed.password && !feed.search && !feed.hash &&
    /^\/(?:[A-Za-z0-9_-]+\/)+RELEASES\.json$/.test(feed.pathname), 'Invalid canonical feed root');
  return new URL(`v${version}/${name}`, new URL('./', feed));
}

const exec = promisify(execFile);
const maxEntries = 50_000;
const maxExpandedBytes = 2 ** 32;
const maxFileBytes = 2 ** 30;
function contained(root, filename) {
  const relative = path.relative(root, filename);
  return relative === '' || (!path.isAbsolute(relative) && relative !== '..' && !relative.startsWith(`..${path.sep}`));
}

/** Bind all app code, signatures, resource bytes and framework links before make. */
export async function bundleDigest(bundle) {
  const root = path.resolve(bundle);
  const rootStat = await lstat(root);
  assert(rootStat.isDirectory() && !rootStat.isSymbolicLink(), 'Application must be a real directory');
  const canonicalRoot = await realpath(root);
  const entries = []; let total = 0;
  async function visit(relative) {
    assert(entries.length < maxEntries, 'Application has too many files');
    const filename = path.join(root, relative); const stat = await lstat(filename);
    if (stat.isSymbolicLink()) {
      const target = await readlink(filename);
      assert(!path.isAbsolute(target) && contained(canonicalRoot, await realpath(filename)), `Escaping application link: ${relative}`);
      entries.push([relative, 'link', target]);
    } else if (stat.isDirectory()) {
      entries.push([relative, 'directory']);
      for (const name of (await readdir(filename)).sort()) await visit(path.join(relative, name));
    } else {
      assert(stat.isFile() && stat.size <= maxFileBytes, `Invalid application file: ${relative}`);
      total += stat.size; assert(total <= maxExpandedBytes, 'Application exceeds its size limit');
      const hash = createHash('sha256'); let bytes = 0;
      for await (const chunk of createReadStream(filename)) {
        bytes += chunk.length; assert(bytes <= maxFileBytes, `Application file grew beyond its limit: ${relative}`); hash.update(chunk);
      }
      assert.equal(bytes, stat.size, `Application changed while hashing: ${relative}`);
      entries.push([relative, 'file', stat.mode & 0o111, bytes, hash.digest('hex')]);
    }
  }
  await visit('');
  return sha256(JSON.stringify(entries));
}

/** Extract only bounded app entries; no write ever follows an archive symlink. */
export async function extractApplicationZip(archive, directory, bundleName) {
  assert(bundleName === path.basename(bundleName) && bundleName.endsWith('.app'), 'Invalid application name');
  await mkdir(directory, { mode: 0o700 });
  const zip = await promisify(yauzl.open)(archive, { lazyEntries: true, autoClose: false, strictFileNames: true, validateEntrySizes: true });
  try {
    const entries = []; const names = new Set(); let expanded = 0;
    await new Promise((resolve, reject) => {
      zip.on('error', reject); zip.once('end', resolve);
      zip.on('entry', entry => {
        try {
          const name = entry.fileName.replace(/\/$/, '');
          assert(name && Buffer.byteLength(name) <= 4096 && !/[\\\u0000-\u001f\u007f]/.test(name) &&
            name.split('/').every(part => part && part !== '.' && part !== '..') &&
            (name === bundleName || name.startsWith(`${bundleName}/`)), `Invalid ZIP path: ${name}`);
          assert(!names.has(name), `Duplicate ZIP path: ${name}`); names.add(name);
          assert(!(entry.generalPurposeBitFlag & 1), 'Encrypted ZIP entries are unsupported');
          assert(Number.isSafeInteger(entry.uncompressedSize) && entry.uncompressedSize <= maxFileBytes, 'ZIP entry exceeds its size limit');
          expanded += entry.uncompressedSize;
          assert(entries.length < maxEntries && expanded <= maxExpandedBytes, 'ZIP exceeds its extraction limits');
          const mode = entry.externalFileAttributes >>> 16; const type = mode & 0o170000;
          const directoryEntry = entry.fileName.endsWith('/');
          assert(type === 0 || type === 0o100000 || type === 0o040000 || type === 0o120000, 'Unsupported ZIP entry type');
          assert(type !== 0o040000 || directoryEntry, 'Invalid ZIP directory');
          assert(!directoryEntry || type === 0 || type === 0o040000, 'Invalid ZIP directory type');
          if (type === 0o120000) assert(entry.uncompressedSize > 0 && entry.uncompressedSize <= 4096, 'Invalid ZIP link size');
          entries.push({ entry, name, mode, link: type === 0o120000, directory: directoryEntry });
          zip.readEntry();
        } catch (error) { reject(error); }
      });
      zip.readEntry();
    });
    assert(entries.length > 0, 'Empty application ZIP');
    // All parents exist before any symlink is created. A symlink used as an
    // entry's parent then fails with EEXIST instead of redirecting extraction.
    for (const item of entries) await mkdir(path.dirname(path.join(directory, item.name)), { recursive: true, mode: 0o700 });
    for (const item of entries.filter(item => !item.link)) {
      const target = path.join(directory, item.name);
      if (item.directory) { await mkdir(target, { recursive: true, mode: 0o700 }); continue; }
      const stream = await promisify(zip.openReadStream.bind(zip))(item.entry);
      await pipeline(stream, createWriteStream(target, { flags: 'wx', mode: (item.mode & 0o777) || 0o644 }));
      await chmod(target, (item.mode & 0o777) || 0o644);
    }
    for (const item of entries.filter(item => item.link)) {
      const stream = await promisify(zip.openReadStream.bind(zip))(item.entry); const chunks = [];
      await new Promise((resolve, reject) => {
        stream.on('data', chunk => chunks.push(chunk)); stream.once('end', resolve); stream.once('error', reject);
      });
      const bytes = Buffer.concat(chunks); const target = bytes.toString('utf8');
      assert(Buffer.from(target).equals(bytes) && !/[\\\u0000-\u001f\u007f]/.test(target) && !path.isAbsolute(target) &&
        contained(path.join(directory, bundleName), path.resolve(directory, path.dirname(item.name), target)), `Escaping ZIP link: ${item.name}`);
      await symlink(target, path.join(directory, item.name));
    }
    const root = await realpath(path.join(directory, bundleName));
    for (const item of entries.filter(item => item.link))
      assert(contained(root, await realpath(path.join(directory, item.name))), `Escaping ZIP link chain: ${item.name}`);
    return path.join(directory, bundleName);
  } finally { zip.close(); }
}

async function verifyContainedApplication(bundle, packageEvidence) {
  assert.equal(await bundleDigest(bundle), packageEvidence.bundleDigest, 'Distribution application bytes differ from the verified package');
  const verified = await verifyDesktop(bundle, { signed: packageEvidence.signed, notarized: packageEvidence.notarized });
  for (const field of ['version', 'distribution', 'buildId', 'updateURL', 'rendererDigest', 'nativeFiles', 'source', 'compatibility', 'teamId', 'runtimeSigning', 'fuses', 'signed', 'notarized'])
    assert.deepEqual(verified[field], packageEvidence[field], `Distribution ${field} differs from package evidence`);
  return { bundleDigest: packageEvidence.bundleDigest, rendererDigest: verified.rendererDigest, signed: verified.signed, notarized: verified.notarized };
}

async function verifyDistributionArchive(filename, packageEvidence) {
  const directory = await mkdtemp(path.join(tmpdir(), 'whip-distribution-'));
  const bundleName = path.basename(packageEvidence.bundle);
  if (filename.endsWith('.zip')) {
    try {
      const bundle = await extractApplicationZip(filename, path.join(directory, 'expanded'), bundleName);
      return await verifyContainedApplication(bundle, packageEvidence);
    } finally { await rm(directory, { recursive: true, force: true }); }
  }
  const mount = path.join(directory, 'volume'); await mkdir(mount, { mode: 0o700 });
  const device = (await lstat(mount)).dev;
  try {
    await exec('/usr/bin/hdiutil', ['attach', filename, '-readonly', '-nobrowse', '-noautoopen', '-mountpoint', mount], { timeout: 60_000 });
    assert.notEqual((await lstat(mount)).dev, device, 'DMG did not mount at the isolated mountpoint');
    const applications = (await readdir(mount)).filter(name => name.endsWith('.app'));
    assert.deepEqual(applications, [bundleName], 'DMG must contain exactly the verified application');
    return await verifyContainedApplication(path.join(mount, bundleName), packageEvidence);
  } finally {
    // A failed attach may still have mounted the volume. Only detach our private
    // mountpoint; never recursively remove it while another device is mounted.
    if ((await lstat(mount)).dev !== device) {
      try { await exec('/usr/bin/hdiutil', ['detach', mount], { timeout: 30_000 }); }
      catch { await exec('/usr/bin/hdiutil', ['detach', mount, '-force'], { timeout: 30_000 }); }
    }
    assert.equal((await lstat(mount)).dev, device, `DMG remains mounted at ${mount}`);
    await rm(directory, { recursive: true, force: true });
  }
}
function notaryArguments() {
  if (process.env.WHIP_DESKTOP_NOTARY_PROFILE) return ['--keychain-profile', process.env.WHIP_DESKTOP_NOTARY_PROFILE];
  const { WHIP_DESKTOP_NOTARY_KEY: key, WHIP_DESKTOP_NOTARY_KEY_ID: id, WHIP_DESKTOP_NOTARY_ISSUER: issuer } = process.env;
  if (!key || !id || !issuer) throw new Error('Configure the notarization keychain profile or API key');
  return ['--key', key, '--key-id', id, '--issuer', issuer];
}

/** Distribution hashes are taken only after signing/notarizing the final DMG. */
export async function prepareDistribution(artifacts, directory, packageEvidence) {
  assert.match(packageEvidence.bundleDigest ?? '', /^[a-f0-9]{64}$/, 'Package evidence must bind the complete application before make');
  assert.equal(await bundleDigest(packageEvidence.bundle), packageEvidence.bundleDigest, 'Packaged application changed during make');
  assert.equal(artifacts.filter(file => file.endsWith('.dmg')).length, 1);
  assert.equal(artifacts.filter(file => file.endsWith('.zip')).length, 1);
  const outputs = new Map();
  for (const source of artifacts) {
    const input = path.basename(source);
    assert(/^[A-Za-z0-9][A-Za-z0-9 ._-]{0,199}$/.test(input) && !input.includes('..'), 'Invalid maker artifact name');
    const name = input === 'RELEASES.json' ? input : desktopArchiveNames.find(name => path.extname(name) === path.extname(input));
    assert(name && !outputs.has(name), `Unexpected or duplicate distribution artifact ${input}`);
    const stat = await lstat(source);
    assert(stat.isFile() && !stat.isSymbolicLink() && stat.size > 0 && stat.size <= maxFileBytes, `Invalid distribution artifact ${name}`);
    outputs.set(name, source);
  }
  let rewrittenFeed;
  if (outputs.has('RELEASES.json')) {
    assert((await lstat(outputs.get('RELEASES.json'))).size <= 2 << 20, 'Maker feed exceeds its limit');
    const feed = JSON.parse(await readFile(outputs.get('RELEASES.json'), 'utf8'));
    assert(feed.currentRelease === packageEvidence.version && Array.isArray(feed.releases), 'Invalid maker feed');
    const current = feed.releases.filter(release => release.version === packageEvidence.version);
    assert.equal(current.length, 1, 'Feed must identify exactly one current release');
    assert.equal(current[0].updateTo?.version, packageEvidence.version, 'Feed update version differs');
    const canonical = desktopArtifactURL(packageEvidence.updateURL, packageEvidence.version, desktopArchiveNames[1]);
    const archive = path.basename(outputs.get(desktopArchiveNames[1]));
    const original = new URL(encodeURIComponent(archive), new URL('./', packageEvidence.updateURL));
    assert.equal(current[0].updateTo.url, original.href, 'Feed names a different ZIP or feed root');
    current[0].updateTo.url = canonical.href;
    rewrittenFeed = JSON.stringify(feed) + '\n';
  }
  await rm(directory, { recursive: true, force: true }); await mkdir(directory, { recursive: true });
  const files = {}; const applications = {}; let dmgNotary;
  for (const [name, source] of outputs) {
    const target = path.join(directory, name); await copyFile(source, target);
    if (name === 'RELEASES.json') await writeFile(target, rewrittenFeed);
    if (name.endsWith('.dmg') && process.env.WHIP_DESKTOP_NOTARIZE === '1') {
      assert(packageEvidence.notarized, 'The ZIP must already contain the stapled application');
      await exec('/usr/bin/codesign', ['--force', '--sign', process.env.WHIP_DESKTOP_SIGN_IDENTITY, '--timestamp', target]);
      const result = await exec('/usr/bin/xcrun', ['notarytool', 'submit', target, ...notaryArguments(), '--wait', '--timeout', '20m', '--output-format', 'json'],
        { timeout: 21 * 60_000, maxBuffer: 1 << 20 });
      const submission = JSON.parse(result.stdout);
      assert.equal(submission.status, 'Accepted', `DMG notarization failed: ${submission.status}`);
      dmgNotary = submission.id;
      await exec('/usr/bin/xcrun', ['stapler', 'staple', target]);
      await exec('/usr/bin/xcrun', ['stapler', 'validate', target]);
      await exec('/usr/sbin/spctl', ['--assess', '--type', 'open', '--context', 'context:primary-signature', '--verbose=2', target]);
    }
    if (/\.(?:dmg|zip)$/.test(name)) applications[name] = await verifyDistributionArchive(target, packageEvidence);
    const bytes = await readFile(target); files[name] = { bytes: bytes.length, sha256: sha256(bytes) };
  }
  if (process.env.WHIP_DESKTOP_RELEASE === '1') assert(files['RELEASES.json'] && dmgNotary, 'A release requires its feed and notarized DMG');
  const evidence = { recordedAt: new Date().toISOString(), ...packageEvidence, ...(dmgNotary ? { dmgNotary } : {}), applications, files };
  await writeFile(path.join(directory, 'evidence.json'), JSON.stringify(evidence, null, 2) + '\n');
  // Final union checksums are assembled once by release-candidate.mjs; this evidence covers only signed Desktop payloads.
  return evidence;
}
