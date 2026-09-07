import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { copyFile, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';
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
