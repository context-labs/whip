import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';
import { createRendererManifest, verifyRendererProvenance } from './renderer-artifact.mjs';

const exec = promisify(execFile);
async function fixture(t) {
  const root = await mkdtemp(path.join(tmpdir(), 'whip-provenance-test-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(path.join(root, 'apps/web/dist'), { recursive: true });
  await mkdir(path.join(root, 'internal/webassets'), { recursive: true });
  await writeFile(path.join(root, '.gitignore'), 'apps/web/dist/\napps/web/renderer-manifest.json\n');
  await writeFile(path.join(root, 'package-lock.json'), '{"lockfileVersion":3}\n');
  await writeFile(path.join(root, 'main.go'), 'package main\n');
  await writeFile(path.join(root, 'internal/webassets/csp.txt'), "default-src 'self'");
  await writeFile(path.join(root, 'apps/web/dist/index.html'), '<html><script src="/app.js"></script></html>');
  await writeFile(path.join(root, 'apps/web/dist/app.js'), 'export const app = true;');
  const git = args => exec('git', args, { cwd: root });
  await git(['init', '--quiet']); await git(['add', '.']);
  await git(['-c', 'user.name=Whip Test', '-c', 'user.email=whip-test@example.invalid', '-c', 'commit.gpgsign=false', 'commit', '--quiet', '-m', 'fixture']);
  return { root, manifest: await createRendererManifest(root) };
}

test('a clean renderer artifact is accepted only for its exact commit and dependency lockfile', async t => {
  const { root, manifest } = await fixture(t);
  assert.equal(manifest.source.dirty, false);
  await verifyRendererProvenance(manifest, root, true);
  await assert.rejects(verifyRendererProvenance({ ...manifest, source: { ...manifest.source, commit: '0'.repeat(40) } }, root, true), /provenance/);
  await writeFile(path.join(root, 'package-lock.json'), '{"lockfileVersion":3,"changed":true}\n');
  await assert.rejects(verifyRendererProvenance(manifest, root, true), /provenance/);
});

test('a clean producer cannot authorize a dirty CLI or desktop consumer checkout', async t => {
  for (const mutation of ['tracked native source', 'untracked source']) await t.test(mutation, async t => {
    const { root, manifest } = await fixture(t);
    await writeFile(path.join(root, mutation.startsWith('tracked') ? 'main.go' : 'untracked.go'), 'package changed\n');
    await verifyRendererProvenance(manifest, root, false);
    await assert.rejects(verifyRendererProvenance(manifest, root, true), /consumer must use a clean checkout/);
  });
});

test('development artifacts remain usable while release rejects dirty producer provenance', async t => {
  const { root } = await fixture(t);
  await writeFile(path.join(root, 'main.go'), 'package dirty\n');
  const manifest = await createRendererManifest(root);
  assert.equal(manifest.source.dirty, true);
  await verifyRendererProvenance(manifest, root, false);
  await assert.rejects(verifyRendererProvenance(manifest, root, true), /renderer must come from a clean checkout/);
});

test('local source metadata builds without Git but cannot authorize a release', async t => {
  const { root, manifest } = await fixture(t);
  const local = await createRendererManifest(root, manifest.source);
  assert.equal(local.source.local, true);
  await verifyRendererProvenance(local, root, false);
  await assert.rejects(verifyRendererProvenance(local, root, true), /cannot use local source metadata/);
  await rm(path.join(root, '.git'), { recursive: true });
  assert.deepEqual((await createRendererManifest(root, manifest.source)).source, local.source);
  await assert.rejects(createRendererManifest(root), /git/);
  await assert.rejects(verifyRendererProvenance(local, root, true), /cannot use local source metadata/);
});

test('invalid or partial local metadata cannot silently replace Git provenance', async t => {
  const { root, manifest } = await fixture(t);
  for (const source of [null, {}, { commit: 'main', dirty: true },
    { commit: manifest.source.commit }, { ...manifest.source, dirty: 'false' },
    { commit: [manifest.source.commit], dirty: false }]) {
    await assert.rejects(createRendererManifest(root, source), /Invalid local renderer source metadata/);
  }
});
