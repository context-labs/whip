import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { fileURLToPath } from 'node:url';
import { compare } from './compare.mjs';
import { loadSDK, smoke, lifecycle, rollback } from './scenarios.mjs';

const { values } = parseArgs({ options: { candidate: { type: 'string' }, base: { type: 'string' }, output: { type: 'string' } } });
assert.ok(values.candidate && values.base, 'usage: node scripts/backend-compat/run.mjs --candidate /absolute/checkout --base <parent SHA> [--output /artifacts]');
assert.match(values.base, /^[0-9a-f]{40}$/, 'base must be an explicit full commit SHA');
assert.equal(process.versions.node.split('.')[0], '24', 'use the repository Node 24 toolchain');
const candidate = resolve(values.candidate);
const output = values.output ? resolve(values.output) : await mkdtemp(join(tmpdir(), 'whip-compat-'));
const baseline = join(output, 'baseline');
const parent = join(output, 'pr-base');
await mkdir(output, { recursive: true });
assert.deepEqual(await readdir(output), [], 'output must be empty; use a fresh directory to prevent stale baseline artifacts');
const manifest = JSON.parse(await readFile(new URL('./baseline.json', import.meta.url)));
const evidence = { status: 'running', manifest, candidate, output, node: process.version, commands: [], results: {} };
const digest = bytes => createHash('sha256').update(bytes).digest('hex');
async function command(program, args, cwd = candidate) {
  console.log(`compatibility: ${program} ${args.join(' ')}`);
  const child = spawn(program, args, { cwd, stdio: ['ignore', 'pipe', 'pipe'] });
  const chunks = [], errors = [];
  child.stdout.on('data', data => chunks.push(data)); child.stderr.on('data', data => errors.push(data));
  const code = await new Promise((resolve, reject) => { child.on('error', reject); child.on('exit', resolve); });
  const stdout = Buffer.concat(chunks), stderr = Buffer.concat(errors);
  const log = `command-${evidence.commands.length}.log`;
  evidence.commands.push({ program, args, cwd, code, log });
  const logged = program === 'git' && args[0] === 'archive' ? Buffer.from(`archive sha256=${digest(stdout)}\n`) : stdout;
  await writeFile(join(output, log), Buffer.concat([logged, stderr]));
  assert.equal(code, 0, `${program} failed; see ${join(output, log)}\n${stderr.toString().slice(-4000)}`);
  return stdout;
}
async function tree(path) {
  const files = {};
  async function visit(relative = '') {
    for (const entry of (await readdir(join(path, relative), { withFileTypes: true })).sort((a, b) => a.name.localeCompare(b.name))) {
      const name = join(relative, entry.name);
      if (entry.isDirectory()) await visit(name);
      else { assert.ok(entry.isFile(), `unexpected artifact symlink ${name}`); files[name] = digest(await readFile(join(path, name))); }
    }
  }
  await visit(); return files;
}
const baseBinary = join(output, 'baseline-daemon.test'), candidateBinary = join(output, 'candidate-daemon.test');
const parentBinary = values.base === manifest.revision ? baseBinary : join(output, 'pr-base-daemon.test');
try {
  evidence.go = (await command('go', ['version'])).toString().trim();
  evidence.parentRevision = (await command('git', ['rev-parse', '--verify', values.base + '^{commit}'])).toString().trim();
  evidence.candidateRevision = (await command('git', ['rev-parse', 'HEAD'])).toString().trim();
  evidence.candidateDiff = (await command('git', ['diff', '--binary', 'HEAD'])).toString();
  // Comparing the working tree also rejects uncommitted contract changes.
  const changed = (await command('git', ['diff', '--name-only', manifest.revision, '--', ...manifest.frozenPaths])).toString().trim();
  assert.equal(changed, '', `frozen contract source changed:\n${changed}`);
  const added = (await command('git', ['ls-files', '--others', '--exclude-standard', '--', ...manifest.frozenPaths])).toString().trim();
  assert.equal(added, '', `untracked contract source added:\n${added}`);
  {
    await mkdir(baseline, { recursive: true });
    const archive = await command('git', ['archive', manifest.revision]);
    const archivePath = join(output, 'baseline.tar'); await writeFile(archivePath, archive);
    await command('tar', ['-xf', archivePath, '-C', baseline]); await rm(archivePath);
    for (const checkout of [baseline, candidate]) {
      await command('npm', ['ci', '--ignore-scripts', '--no-audit', '--no-fund'], checkout);
      for (const path of manifest.generatedPaths) await rm(join(checkout, path), { recursive: true, force: true });
      await command('npm', ['run', 'generate'], checkout);
      await command('npm', ['run', 'build'], checkout);
    }
    await command('go', ['test', '-c', '-tags=integration', '-o', baseBinary, './internal/daemon'], baseline);
    await command('go', ['test', '-c', '-tags=integration', '-o', candidateBinary, './internal/daemon'], candidate);
    if (parentBinary !== baseBinary) {
      await mkdir(parent);
      const archivePath = join(output, 'pr-base.tar');
      await writeFile(archivePath, await command('git', ['archive', values.base]));
      await command('tar', ['-xf', archivePath, '-C', parent]); await rm(archivePath);
      await command('go', ['test', '-c', '-tags=integration', '-o', parentBinary, './internal/daemon'], parent);
    }
  }
  for (const path of manifest.generatedPaths) compare(await tree(join(baseline, path)), await tree(join(candidate, path)), `fresh generated ${path}`);
  compare(await tree(join(baseline, 'packages/sdk/dist')), await tree(join(candidate, 'packages/sdk/dist')), 'compiled SDK artifact freeze');
  evidence.artifacts = { baselineBinary: digest(await readFile(baseBinary)), parentBinary: digest(await readFile(parentBinary)), candidateBinary: digest(await readFile(candidateBinary)), sdk: await tree(join(baseline, 'packages/sdk/dist')), protocol: await tree(join(baseline, 'packages/protocol/generated')), lockfile: digest(await readFile(join(baseline, 'package-lock.json'))) };
  await command(process.execPath, ['--test', fileURLToPath(new URL('./compare.test.mjs', import.meta.url))]);
  const sdk = await loadSDK(baseline);
  // Every invocation gets new state, including repeated calibration runs.
  const runs = await mkdtemp(join(output, 'runs-'));
  for (const transport of ['unix', 'websocket']) {
    const reference = await smoke(sdk, baseBinary, join(runs, `base-${transport}`), transport);
    const calibration = await smoke(sdk, baseBinary, join(runs, `calibration-${transport}`), transport);
    await writeFile(join(runs, `reference-${transport}.json`), JSON.stringify(reference, null, 2));
    await writeFile(join(runs, `calibration-${transport}.json`), JSON.stringify(calibration, null, 2));
    compare(reference, calibration, `${transport} base-versus-base calibration`);
    const responseMutation = structuredClone(reference); responseMutation.updated.max_retries++;
    assert.throws(() => compare(reference, responseMutation), assert.AssertionError);
    const orderMutation = structuredClone(reference); orderMutation.live.reverse();
    assert.throws(() => compare(reference, orderMutation), assert.AssertionError);
    evidence.results[`${transport}MutationDetection`] = 'passed: actual response and event order mutations rejected';
    const actual = await smoke(sdk, candidateBinary, join(runs, `candidate-${transport}`), transport);
    await writeFile(join(runs, `candidate-${transport}.json`), JSON.stringify(actual, null, 2));
    compare(reference, actual, `${transport} fixed-SDK differential`);
    const parentTranscript = parentBinary === baseBinary ? reference : await smoke(sdk, parentBinary, join(runs, `parent-${transport}`), transport);
    await writeFile(join(runs, `parent-${transport}.json`), JSON.stringify(parentTranscript, null, 2));
    compare(parentTranscript, actual, `${transport} immediate-PR-base differential`);
    evidence.results[transport] = 'passed: calibrated response/error/event transcript';
  }
  for (const [name, binary] of [['baseline', baseBinary], ['candidate', candidateBinary]]) {
    await lifecycle(sdk, binary, join(runs, `${name}-lifecycle`)); evidence.results[`${name}Lifecycle`] = 'passed';
  }
  await rollback(sdk, baseBinary, baseBinary, join(runs, 'rollback-calibration'));
  await rollback(sdk, baseBinary, candidateBinary, join(runs, 'rollback-candidate'));
  if (parentBinary !== baseBinary) await rollback(sdk, parentBinary, candidateBinary, join(runs, 'rollback-parent'));
  evidence.results.rollback = 'passed: fixed and immediate bases; both engines, retained root/child state, content, schema, trace/export';
  evidence.status = 'passed';
  console.log(`Compatibility checks passed. Evidence: ${join(output, 'evidence.json')}`);
} catch (error) { evidence.status = 'failed'; evidence.failure = { name: error.name, message: error.message }; throw error; }
finally { await writeFile(join(output, 'evidence.json'), JSON.stringify(evidence, null, 2)); }
