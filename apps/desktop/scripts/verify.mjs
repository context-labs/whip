import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { readFile, lstat, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import asar from '@electron/asar';
import { FuseV1Options, getCurrentFuseWire } from '@electron/fuses';
import { sha256, readRendererManifest, verifyRenderer, repositoryRoot } from '../../../scripts/renderer-artifact.mjs';
import { readRuntimeManifest } from '../src/runtime.ts';

const exec = promisify(execFile);
export async function verifyDesktop(bundle, { signed = false, notarized = false } = {}) {
  const contents = path.join(bundle, 'Contents');
  const archive = path.join(contents, 'Resources/app.asar');
  const directory = await mkdtemp(path.join(tmpdir(), 'whip-package-verification-'));
  try {
    const roots = new Set(['package.json', 'main.cjs', 'preload.cjs', 'desktop-config.json', 'renderer-manifest.json', 'runtime-manifest.json', 'renderer', 'licenses']);
    for (const name of asar.listPackage(archive)) {
      const relative = name.replace(/^\//, '');
      assert(roots.has(relative.split('/')[0]), `Unexpected archive path ${relative}`);
      const stat = asar.statFile(archive, relative, false);
      assert(!stat.link && !stat.unpacked, `Unsealed archive file ${relative}`);
    }
    asar.extractAll(archive, directory);
    const renderer = await readRendererManifest(path.join(directory, 'renderer-manifest.json'));
    await verifyRenderer(path.join(directory, 'renderer'), renderer);
    await verifyRenderer(path.join(repositoryRoot, 'internal/webassets/dist'), renderer);
    const runtime = await readRuntimeManifest(path.join(directory, 'runtime-manifest.json'));
    assert.equal(runtime.rendererDigest, renderer.digest);
    assert.deepEqual(runtime.source, { ...renderer.source, lockfile: renderer.lockfile });
    for (const [name, source] of Object.entries({ 'Whip.txt': path.join(repositoryRoot, 'LICENSE'),
      LICENSE: path.join(repositoryRoot, 'node_modules/electron/dist/LICENSE'),
      'LICENSES.chromium.html': path.join(repositoryRoot, 'node_modules/electron/dist/LICENSES.chromium.html') }))
      assert((await readFile(path.join(directory, 'licenses', name))).equals(await readFile(source)), `Missing or changed distribution license: ${name}`);
    const helperBytes = await readFile(path.join(contents, 'Helpers/whip-computer'));
    const runtimeBytes = await readFile(path.join(contents, 'Helpers/whipcode'));
    assert(runtimeBytes.indexOf(helperBytes) >= 0, 'The packaged signed helper differs from the Go embed');
    for (const [name, expected] of Object.entries(runtime.files)) {
      const filename = path.join(contents, 'Helpers', name);
      const bytes = await readFile(filename); const stat = await lstat(filename);
      assert(stat.isFile() && !stat.isSymbolicLink() && stat.mode & 0o111, `Invalid native file ${name}`);
      assert.equal(bytes.length, expected.bytes); assert.equal(sha256(bytes), expected.sha256);
      const { stdout } = await exec('/usr/bin/lipo', ['-archs', filename]); assert.equal(stdout.trim(), 'arm64');
      await exec('/usr/bin/codesign', ['--verify', '--strict', filename]);
      if (signed) {
        assert.match(runtime.teamId ?? '', /^[A-Z0-9]{10}$/);
        await exec('/usr/bin/codesign', ['--verify', '--strict', '-R',
          `=anchor apple generic and certificate leaf[subject.OU] = "${runtime.teamId}"`, filename]);
      }
    }
    const metadata = JSON.parse((await exec(path.join(contents, 'Helpers/whipcode'), ['_desktop-runtime-info'],
      { timeout: 5000, maxBuffer: 16 << 10, encoding: 'utf8' })).stdout);
    assert.equal(metadata.distribution, 'whipcode', 'Packaged backend is not the whipcode distribution');
    assert.equal(metadata.distribution, runtime.distribution);
    assert.equal(metadata.buildId, runtime.buildId);
    for (const key of ['protocolMajor', 'protocolMinor', 'schemaVersion'])
      assert.equal(metadata[key], runtime.compatibility[key], `Runtime ${key} differs from its manifest`);
    // Go embeds the same untransformed renderer. Match every file's complete bytes.
    for (const name of Object.keys(renderer.files)) {
      const bytes = await readFile(path.join(directory, 'renderer', name));
      assert(runtimeBytes.indexOf(bytes) >= 0, `Go is missing renderer bytes: ${name}`);
    }
    const fuses = await getCurrentFuseWire(bundle);
    for (const fuse of [FuseV1Options.RunAsNode, FuseV1Options.EnableNodeOptionsEnvironmentVariable, FuseV1Options.EnableNodeCliInspectArguments, FuseV1Options.GrantFileProtocolExtraPrivileges])
      assert.equal(fuses[fuse], 48, `Fuse ${fuse} should be disabled`);
    for (const fuse of [FuseV1Options.EnableEmbeddedAsarIntegrityValidation, FuseV1Options.OnlyLoadAppFromAsar, FuseV1Options.EnableCookieEncryption])
      assert.equal(fuses[fuse], 49, `Fuse ${fuse} should be enabled`);
    if (signed) await exec('/usr/bin/codesign', ['--verify', '--deep', '--strict', '-R',
      `=anchor apple generic and certificate leaf[subject.OU] = "${runtime.teamId}"`, bundle]);
    if (notarized) {
      assert(signed, 'Notarization requires a signed app');
      await exec('/usr/bin/xcrun', ['stapler', 'validate', bundle]);
      await exec('/usr/sbin/spctl', ['--assess', '--type', 'execute', '--verbose=2', bundle]);
    }
    return { bundle, version: runtime.version, distribution: runtime.distribution, buildId: runtime.buildId, rendererDigest: renderer.digest, nativeFiles: runtime.files,
      source: runtime.source, compatibility: runtime.compatibility, teamId: runtime.teamId, fuses, signed, notarized };
  } finally { await rm(directory, { recursive: true, force: true }); }
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const bundle = process.argv.slice(2).find(arg => !arg.startsWith('--'));
  if (!bundle) throw new Error('Usage: verify.mjs /path/Whip.app [--signed]');
  const evidence = await verifyDesktop(path.resolve(bundle), { signed: process.argv.includes('--signed'), notarized: process.argv.includes('--notarized') });
  const output = path.join(repositoryRoot, '.ai-docs/plans/desktop-app/evidence/package.json');
  await writeFile(output, JSON.stringify({ recordedAt: new Date().toISOString(), ...evidence }, null, 2) + '\n');
  console.log(JSON.stringify(evidence, null, 2));
}
