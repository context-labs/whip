import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { chmod, cp, mkdir, mkdtemp, readFile, readlink, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';
import { crc32 } from 'node:zlib';
import { bundleDigest, extractApplicationZip, prepareDistribution } from '../apps/desktop/scripts/distribution.mjs';

// Stored ZIP entries keep hostile central-directory paths and Unix modes intact.
function zip(entries) {
  const local = []; const central = []; let offset = 0;
  for (const { name, body = '', mode = 0o100644, size } of entries) {
    const filename = Buffer.from(name); const bytes = Buffer.from(body);
    const header = Buffer.alloc(30); header.writeUInt32LE(0x04034b50);
    header.writeUInt16LE(20, 4); header.writeUInt32LE(crc32(bytes), 14);
    header.writeUInt32LE(bytes.length, 18); header.writeUInt32LE(size ?? bytes.length, 22); header.writeUInt16LE(filename.length, 26);
    const directory = Buffer.alloc(46); directory.writeUInt32LE(0x02014b50);
    directory.writeUInt16LE(0x0314, 4); directory.writeUInt16LE(20, 6);
    directory.writeUInt32LE(crc32(bytes), 16); directory.writeUInt32LE(bytes.length, 20);
    directory.writeUInt32LE(size ?? bytes.length, 24); directory.writeUInt16LE(filename.length, 28);
    directory.writeUInt32LE((mode << 16) >>> 0, 38); directory.writeUInt32LE(offset, 42);
    local.push(header, filename, bytes); central.push(directory, filename);
    offset += header.length + filename.length + bytes.length;
  }
  const end = Buffer.alloc(22); end.writeUInt32LE(0x06054b50);
  end.writeUInt16LE(entries.length, 8); end.writeUInt16LE(entries.length, 10);
  end.writeUInt32LE(Buffer.concat(central).length, 12); end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, ...central, end]);
}
async function fixture(t) {
  const root = await mkdtemp(path.join(tmpdir(), 'whip-distribution-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const archive = path.join(root, 'Whip.zip');
  const extract = async entries => {
    await writeFile(archive, zip(entries));
    return extractApplicationZip(archive, path.join(root, 'expanded'), 'Whip.app');
  };
  return { root, archive, extract };
}

test('extracts the exact application and relative framework links, preserving executable bits', async t => {
  const f = await fixture(t);
  const bundle = await f.extract([
    { name: 'Whip.app/Contents/main.cjs', body: 'shared renderer bootstrap' },
    { name: 'Whip.app/Contents/Frameworks/Test/Versions/A/Test', body: 'executable', mode: 0o100755 },
    { name: 'Whip.app/Contents/Frameworks/Test/Versions/Current', body: 'A', mode: 0o120777 },
    { name: 'Whip.app/Contents/Frameworks/Test/Test', body: 'Versions/Current/Test', mode: 0o120777 },
  ]);
  assert.equal(await readFile(path.join(bundle, 'Contents/Frameworks/Test/Test'), 'utf8'), 'executable');
  assert.equal(await readlink(path.join(bundle, 'Contents/Frameworks/Test/Versions/Current')), 'A');
  const copy = path.join(f.root, 'copy.app'); await cp(bundle, copy, { recursive: true, verbatimSymlinks: true });
  assert.equal(await bundleDigest(bundle), await bundleDigest(copy));
  await chmod(path.join(copy, 'Contents/Frameworks/Test/Versions/A/Test'), 0o644);
  assert.notEqual(await bundleDigest(bundle), await bundleDigest(copy));
});

test('full bundle digest detects changed, missing, and additional main/preload/Electron bytes', async t => {
  for (const mutation of ['changed', 'missing', 'extra']) await t.test(mutation, async t => {
    const f = await fixture(t);
    const bundle = await f.extract([{ name: 'Whip.app/Contents/main.cjs', body: 'original main process' }]);
    const expected = await bundleDigest(bundle);
    if (mutation === 'changed') await writeFile(path.join(bundle, 'Contents/main.cjs'), 'substituted main process');
    if (mutation === 'missing') await rm(path.join(bundle, 'Contents/main.cjs'));
    if (mutation === 'extra') await writeFile(path.join(bundle, 'Contents/preload.cjs'), 'unexpected preload');
    assert.notEqual(await bundleDigest(bundle), expected);
  });
});

test('rejects traversal, absolute paths, foreign apps, duplicate names and special device entries', async t => {
  for (const entries of [
    [{ name: '../outside', body: 'bad' }],
    [{ name: '/Whip.app/main', body: 'bad' }],
    [{ name: 'Whip.app/../outside', body: 'bad' }],
    [{ name: 'Whip.app/back\\slash', body: 'bad' }],
    [{ name: 'Other.app/main', body: 'bad' }],
    [{ name: 'Whip.app/main', body: 'one' }, { name: 'Whip.app/main', body: 'two' }],
    [{ name: 'Whip.app/device', mode: 0o020666 }],
  ]) await t.test(entries[0].name, async t => {
    const f = await fixture(t);
    await assert.rejects(f.extract(entries), /ZIP|relative path|absolute path|invalid characters|invalid relative path/i);
    await assert.rejects(readFile(path.join(f.root, 'outside')), { code: 'ENOENT' });
  });
});

test('never writes through escaping symlinks or a symlink used as an archive parent', async t => {
  for (const entries of [
    [{ name: 'Whip.app/link', body: '../../outside', mode: 0o120777 }],
    [{ name: 'Whip.app/link', body: '/tmp', mode: 0o120777 }],
    [{ name: 'Whip.app/link', body: '../../outside', mode: 0o120777 }, { name: 'Whip.app/link/keep.txt', body: 'changed' }],
    [{ name: 'Whip.app/link', body: '../outside', mode: 0o120777 }, { name: 'Whip.app/link', body: 'changed' }],
    [{ name: 'Whip.app/A/link', body: '..', mode: 0o120777 }, { name: 'Whip.app/chain', body: 'A/link/../../outside', mode: 0o120777 }],
  ]) await t.test(entries.map(entry => entry.name).join(','), async t => {
    const f = await fixture(t); const outside = path.join(f.root, 'outside');
    await mkdir(outside); await writeFile(path.join(outside, 'keep.txt'), 'host data');
    await assert.rejects(f.extract(entries), /ZIP|EEXIST/);
    assert.equal(await readFile(path.join(outside, 'keep.txt'), 'utf8'), 'host data');
  });
});

test('rejects truncated entries and declared sizes beyond extraction bounds', async t => {
  for (const size of [7, 2 ** 30 + 1]) await t.test(String(size), async t => {
    const f = await fixture(t);
    await assert.rejects(f.extract([{ name: 'Whip.app/main', body: 'tiny', size }]), /size|ZIP|bytes/);
  });
});

test('bundle digest rejects directory aliases, escaping links and dangling framework links', async t => {
  const f = await fixture(t); const actual = path.join(f.root, 'Whip.app'); await mkdir(actual);
  const alias = path.join(f.root, 'alias.app'); await symlink(actual, alias);
  await assert.rejects(bundleDigest(alias), /real directory/);
  await symlink('../outside', path.join(actual, 'link')); await writeFile(path.join(f.root, 'outside'), 'host data');
  await assert.rejects(bundleDigest(actual), /Escaping application link/);
  await rm(path.join(f.root, 'outside'));
  await assert.rejects(bundleDigest(actual), { code: 'ENOENT' });
});

test('distribution refuses missing or stale before-make evidence before removing prior output', async t => {
  const f = await fixture(t); const bundle = await f.extract([{ name: 'Whip.app/main', body: 'original' }]);
  const directory = path.join(f.root, 'release'); await mkdir(directory); await writeFile(path.join(directory, 'keep'), 'previous release');
  await assert.rejects(prepareDistribution([], directory, { bundle }), /before make/);
  const expected = await bundleDigest(bundle); await writeFile(path.join(bundle, 'main'), 'changed by maker');
  await assert.rejects(prepareDistribution([], directory, { bundle, bundleDigest: expected }), /changed during make/);
  assert.equal(await readFile(path.join(directory, 'keep'), 'utf8'), 'previous release');
});

test('a substituted DMG application is rejected and its private volume is detached on failure', { skip: process.platform !== 'darwin', timeout: 60_000 }, async t => {
  for (const key of ['WHIP_DESKTOP_RELEASE', 'WHIP_DESKTOP_NOTARIZE']) {
    const original = process.env[key]; delete process.env[key];
    t.after(() => { if (original === undefined) delete process.env[key]; else process.env[key] = original; });
  }
  const f = await fixture(t); const exec = promisify(execFile);
  const bundle = await f.extract([{ name: 'Whip.app/Contents/main.cjs', body: 'verified native main' }]);
  const evidence = { bundle, bundleDigest: await bundleDigest(bundle) };
  const imageRoot = path.join(f.root, 'image source'); await mkdir(imageRoot);
  await cp(bundle, path.join(imageRoot, 'Whip.app'), { recursive: true, verbatimSymlinks: true });
  await writeFile(path.join(imageRoot, 'Whip.app/Contents/main.cjs'), 'substituted native main');
  const dmg = path.join(f.root, 'Whip bad.dmg');
  await exec('/usr/bin/hdiutil', ['create', '-srcfolder', imageRoot, '-format', 'UDZO', '-volname', 'Whip Distribution Fixture', dmg], { timeout: 30_000 });
  await assert.rejects(prepareDistribution([dmg, f.archive], path.join(f.root, 'release'), evidence), /application bytes differ/);
  const mounted = await exec('/usr/bin/hdiutil', ['info'], { timeout: 10_000 });
  assert(!mounted.stdout.includes(path.join(f.root, 'release', path.basename(dmg))), 'A rejected DMG must not remain mounted');
  await assert.rejects(readFile(path.join(f.root, 'release/evidence.json')), { code: 'ENOENT' });
});
