import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { repositoryRoot } from '../../../scripts/renderer-artifact.mjs';
import { LocalRuntime, fileDigest } from '../src/runtime.ts';
import { extractApplicationZip } from './distribution.mjs';
import { verifyDesktop } from './verify.mjs';
import { verifyRuntimeSigning } from './runtime-signing.mjs';
import { validateRuntimeEvidence } from './runtime-evidence.mjs';

const exec = promisify(execFile);
export async function acceptRuntime(directory) {
  assert(process.platform === 'darwin' && process.arch === 'arm64', 'Runtime release acceptance requires macOS arm64');
  const output = path.join(directory, 'signed-runtime.json');
  await rm(output, { force: true }); // Failed retries must not leave passing evidence.
  const evidence = JSON.parse(await readFile(path.join(directory, 'evidence.json'), 'utf8'));
  const archives = Object.keys(evidence.files ?? {}).filter(name => name.endsWith('.zip'));
  assert.equal(archives.length, 1, 'Expected one final distribution ZIP');
  const archive = archives[0];
  assert.equal(archive, path.basename(archive), 'Invalid distribution ZIP name');
  assert.equal(await fileDigest(path.join(directory, archive)), evidence.files[archive].sha256, 'Distribution ZIP changed');
  const fixture = await mkdtemp('/tmp/whip-runtime-acceptance-');
  try {
    const name = evidence.version.includes('-') ? 'Whip Beta.app' : 'Whip.app';
    const bundle = await extractApplicationZip(path.join(directory, archive), path.join(fixture, 'expanded'), name);
    const verified = await verifyDesktop(bundle, { signed: true, notarized: true });
    const identity = Object.fromEntries(['version', 'buildId', 'source', 'nativeFiles', 'teamId', 'runtimeSigning'].map(key => [key, verified[key]]));
    for (const [key, value] of Object.entries(identity)) assert.deepEqual(value, evidence[key], `Extracted package differs: ${key}`);
    const native = path.join(bundle, 'Contents/Helpers');
    const run = async (executable, name) => {
      const filename = path.join(fixture, `${name}.json`);
      await exec('go', ['test', '-tags=integration', './internal/rlm', '-run', '^TestPackagedRuntime$', '-count=1', '-timeout=2m'],
        { cwd: repositoryRoot, env: { ...process.env, WHIP_RLM_TEST_EXECUTABLE: executable, WHIP_RLM_TEST_REPORT: filename }, timeout: 180_000, maxBuffer: 2 << 20 });
      assert.equal(await fileDigest(executable), evidence.nativeFiles.whipcode.sha256, 'Tested executable changed');
      return JSON.parse(await readFile(filename, 'utf8'));
    };
    const packaged = await run(path.join(native, 'whipcode'), 'packaged');
    const user = path.join(fixture, 'user'); await mkdir(user);
    const env = { HOME: user, PATH: '/usr/bin:/bin:/usr/sbin:/sbin', SHELL: '/bin/zsh', WHIPCODE_HOME: path.join(user, '.whipcode') };
    const executable = path.join(user, 'bin/whipcode');
    const manifest = { ...verified, schema: 1, architecture: 'arm64', files: verified.nativeFiles };
    await new LocalRuntime({ source: native, manifest, env, settingsFile: path.join(user, 'runtime.json'), defaultExecutable: executable })
      .install(executable, AbortSignal.timeout(30_000));
    assert.deepEqual(await verifyRuntimeSigning(executable, evidence.teamId), verified.runtimeSigning, 'Installation changed the backend signing policy');
    const installed = await run(executable, 'installed');
    const result = validateRuntimeEvidence({ schema: 1, completed: true, package: identity, packaged, installed }, evidence);
    await writeFile(output, JSON.stringify(result, null, 2) + '\n');
    return result;
  } finally { await rm(fixture, { recursive: true, force: true }); }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert(process.argv[2], 'Usage: runtime-acceptance.mjs release-directory');
  await acceptRuntime(path.resolve(process.argv[2]));
  console.log('Both runtimes passed execution from the signed distribution and installed backend');
}
