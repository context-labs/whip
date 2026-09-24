import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { copyFile, lstat, mkdir, mkdtemp, readFile, rm, symlink, truncate, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';
import { rendererFiles, rendererDigest, readRendererManifest, verifyRenderer } from './renderer-artifact.mjs';
const exec = promisify(execFile);

async function fixture(t) {
  const root = await mkdtemp(path.join(tmpdir(), 'whip-web-pack-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const source = path.join(root, 'apps/web/dist');
  const target = path.join(root, 'internal/webassets/dist');
  await mkdir(path.join(root, 'scripts'), { recursive: true });
  await mkdir(path.join(source, 'assets'), { recursive: true });
  await mkdir(target, { recursive: true });
  await copyFile(new URL('./pack-web.mjs', import.meta.url), path.join(root, 'scripts/pack-web.mjs'));
  await writeFile(path.join(source, 'index.html'), '<html><script type="module" src="/assets/app.js"></script></html>');
  await writeFile(path.join(source, 'assets/app.js'), 'export const app=true;');
  await writeFile(path.join(target, '.gitkeep'), '');
  await writeFile(path.join(target, 'stale.js'), 'old build');
  await copyFile(new URL('./renderer-artifact.mjs', import.meta.url), path.join(root, 'scripts/renderer-artifact.mjs'));
  const csp = (await readFile(new URL('../internal/webassets/csp.txt', import.meta.url), 'utf8')).trim();
  await writeFile(path.join(root, 'internal/webassets/csp.txt'), csp);
  const files = await rendererFiles(source);
  await writeFile(path.join(root, 'apps/web/renderer-manifest.json'), JSON.stringify({ schema: 1, csp, files, digest: rendererDigest(files, csp) }));
  const run = () => exec(process.execPath, [path.join(root, 'scripts/pack-web.mjs')]);
  return { root, source, target, run };
}

test('packages the real build tree and removes stale generated assets', async t => {
  const f = await fixture(t);
  await f.run();
  assert.match(await readFile(path.join(f.target, 'index.html'), 'utf8'), /<html>/);
  assert.equal(await readFile(path.join(f.target, 'assets/app.js'), 'utf8'), 'export const app=true;');
  assert.equal(await readFile(path.join(f.target, '.gitkeep'), 'utf8'), '');
  await assert.rejects(readFile(path.join(f.target, 'stale.js')), { code: 'ENOENT' });
});

test('rejects missing app entry before replacing the previous bundle', async t => {
  const f = await fixture(t);
  await writeFile(path.join(f.source, 'index.html'), '<html>broken</html>');
  await assert.rejects(f.run(), /missing its application entry/);
  assert.equal(await readFile(path.join(f.target, 'stale.js'), 'utf8'), 'old build');
});

test('rejects symlinks outside the generated bundle', async t => {
  const f = await fixture(t);
  const outside = path.join(f.root, 'outside.txt');
  await writeFile(outside, 'host file');
  await symlink(outside, path.join(f.source, 'assets/host.txt'));
  await assert.rejects(f.run(), /Unexpected web artifact/);
  assert.equal(await readFile(path.join(f.target, 'stale.js'), 'utf8'), 'old build');
});

test('rejects changed, missing and extra renderer files before touching the previous bundle', async t => {
  for (const change of ['changed', 'missing', 'extra']) await t.test(change, async t => {
    const f = await fixture(t);
    if (change === 'changed') await writeFile(path.join(f.source, 'assets/app.js'), 'export const app=null;');
    if (change === 'missing') await rm(path.join(f.source, 'assets/app.js'));
    if (change === 'extra') await writeFile(path.join(f.source, 'assets/extra.js'), 'export const extra=true;');
    await assert.rejects(f.run(), /Renderer files differ from the built artifact/);
    assert.equal(await readFile(path.join(f.target, 'stale.js'), 'utf8'), 'old build');
  });
});

test('rejects a missing entry point and release artifacts with hidden files, maps or oversized files', async t => {
  for (const change of ['entry', 'hidden', 'map', 'large']) await t.test(change, async t => {
    const f = await fixture(t);
    if (change === 'entry') await rm(path.join(f.source, 'index.html'));
    if (change === 'hidden') await writeFile(path.join(f.source, 'assets/.env'), 'private');
    if (change === 'map') await writeFile(path.join(f.source, 'assets/app.js.map'), '{}');
    if (change === 'large') {
      const filename = path.join(f.source, 'assets/large.bin');
      await writeFile(filename, '');
      await truncate(filename, (32 << 20) + 1);
    }
    await assert.rejects(f.run(), /Build the web app first|Unexpected web artifact|Invalid release web artifact/);
    assert.equal(await readFile(path.join(f.target, 'stale.js'), 'utf8'), 'old build');
  });
});

test('rejects symlinked source directories and target directories without writing through them', async t => {
  const f = await fixture(t);
  const outside = path.join(f.root, 'outside');
  await mkdir(outside);
  await writeFile(path.join(outside, 'keep.txt'), 'host data');
  await symlink(outside, path.join(f.source, 'linked-directory'));
  await assert.rejects(f.run(), /Unexpected web artifact/);
  await rm(path.join(f.source, 'linked-directory'));
  await rm(f.target, { recursive: true });
  await symlink(outside, f.target);
  await assert.rejects(f.run(), /target must not be a symlink/);
  assert.equal(await readFile(path.join(outside, 'keep.txt'), 'utf8'), 'host data');
  await assert.rejects(readFile(path.join(outside, 'index.html')), { code: 'ENOENT' });
});

test('does not let an ignored source placeholder symlink escape artifact verification', async t => {
  const f = await fixture(t);
  const outside = path.join(f.root, 'outside.txt');
  await writeFile(outside, 'host data must survive');
  await symlink(outside, path.join(f.source, '.gitkeep'));
  await assert.rejects(f.run(), /artifact|gitkeep|placeholder|symlink/i);
  assert.equal(await readFile(outside, 'utf8'), 'host data must survive');
  assert.equal(await readFile(path.join(f.target, 'stale.js'), 'utf8'), 'old build');
});

test('never follows an existing target placeholder symlink while refreshing a bundle', async t => {
  const f = await fixture(t);
  const outside = path.join(f.root, 'outside.txt');
  await writeFile(outside, 'host data must survive');
  await rm(path.join(f.target, '.gitkeep'));
  await symlink(outside, path.join(f.target, '.gitkeep'));
  const [result] = await Promise.allSettled([f.run()]);
  assert.equal(await readFile(outside, 'utf8'), 'host data must survive');
  if (result.status === 'fulfilled') {
    assert.equal((await lstat(path.join(f.target, '.gitkeep'))).isSymbolicLink(), false);
    assert.equal(await readFile(path.join(f.target, '.gitkeep'), 'utf8'), '');
  } else assert.match(String(result.reason), /artifact|gitkeep|placeholder|symlink/i);
});

test('rejects manifest tampering and a self-consistent manifest with a different server CSP', async t => {
  const f = await fixture(t);
  const filename = path.join(f.root, 'apps/web/renderer-manifest.json');
  const original = JSON.parse(await readFile(filename, 'utf8'));
  await writeFile(filename, JSON.stringify({ ...original, digest: '0'.repeat(64) }));
  await assert.rejects(f.run(), /Renderer manifest digest does not match/);
  const csp = "default-src 'none'; script-src 'unsafe-inline'";
  await writeFile(filename, JSON.stringify({ ...original, csp, digest: rendererDigest(original.files, csp) }));
  await assert.rejects(f.run(), /Renderer CSP differs from the checked-out server policy/);
  assert.equal(await readFile(path.join(f.target, 'stale.js'), 'utf8'), 'old build');
});

test('validates manifest paths and bounds independently of its self-consistent digest', async t => {
  const f = await fixture(t);
  const filename = path.join(f.root, 'apps/web/renderer-manifest.json');
  const original = await readRendererManifest(filename);
  const entry = original.files['assets/app.js'];
  for (const name of ['../outside.js', '/absolute.js', 'assets/../outside.js', 'assets\\outside.js', 'assets//app.js', '.env']) {
    const files = { ...original.files, [name]: entry };
    await writeFile(filename, JSON.stringify({ ...original, files, digest: rendererDigest(files, original.csp) }));
    await assert.rejects(readRendererManifest(filename), /Invalid renderer manifest entry/, name);
  }
  for (const bytes of [-1, 0.5, (32 << 20) + 1]) {
    const files = { ...original.files, 'assets/app.js': { ...entry, bytes } };
    await writeFile(filename, JSON.stringify({ ...original, files, digest: rendererDigest(files, original.csp) }));
    await assert.rejects(readRendererManifest(filename), /Invalid renderer manifest entry/);
  }
  const files = Object.fromEntries(Array.from({ length: 4097 }, (_, index) => [`asset-${index}.js`, entry]));
  await writeFile(filename, JSON.stringify({ ...original, files, digest: rendererDigest(files, original.csp) }));
  await assert.rejects(readRendererManifest(filename), /Invalid renderer file count/);
  await writeFile(filename, ' '.repeat((2 << 20) + 1));
  await assert.rejects(readRendererManifest(filename), /Renderer manifest is too large/);
});

test('verifies the copied artifact byte-for-byte and detects subsequent target tampering', async t => {
  const f = await fixture(t);
  const manifest = await readRendererManifest(path.join(f.root, 'apps/web/renderer-manifest.json'));
  await f.run();
  await verifyRenderer(f.target, manifest);
  await writeFile(path.join(f.target, 'assets/app.js'), 'export const app=null;');
  await assert.rejects(verifyRenderer(f.target, manifest), /Renderer files differ from the built artifact/);
});
