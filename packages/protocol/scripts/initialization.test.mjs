import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtemp, readFile, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const repository = fileURLToPath(new URL('../../../', import.meta.url));
const generator = fileURLToPath(new URL('./generate.mjs', import.meta.url));

test('protocol initialization is repeatable and freshness checks never repair output', async t => {
  const directory = await mkdtemp(join(tmpdir(), 'whip-protocol-initialization-'));
  t.after(() => rm(directory, { recursive: true, force: true }));

  function run(command, args, cwd) {
    const result = spawnSync(command, args, { cwd, encoding: 'utf8' });
    assert.ifError(result.error);
    assert.equal(result.signal, null, result.stderr);
    return result;
  }
  const go = check => run('go', ['run', './cmd/whip-contract', '-out', join(directory, 'schema'),
    ...(check ? ['-check'] : [])], repository);
  const node = check => run(process.execPath, [generator, ...(check ? ['--check'] : [])], directory);

  async function snapshot() {
    const files = {};
    for (const name of (await readdir(directory, { recursive: true })).sort()) {
      if ((await stat(join(directory, name))).isDirectory()) continue;
      files[name] = createHash('sha256').update(await readFile(join(directory, name))).digest('hex');
    }
    return files;
  }
  function generate() {
    for (const stage of [go, node]) {
      const result = stage(false);
      assert.equal(result.status, 0, result.stderr);
    }
  }
  async function check(stage, fresh) {
    const before = await snapshot();
    const result = stage(true);
    if (fresh) assert.equal(result.status, 0, result.stderr);
    else {
      assert.notEqual(result.status, 0, 'stale or missing artifacts must fail');
      assert.match(result.stderr, stage === go ? /generated contract/ : /contract drift|ENOENT|Missing generated/i);
    }
    assert.deepEqual(await snapshot(), before, 'checks must not write artifacts');
  }

  await check(go, false);
  await check(node, false);
  assert.deepEqual(await readdir(directory), [], 'checks must not initialize missing output directories');
  const schemas = go(false);
  assert.equal(schemas.status, 0, schemas.stderr);
  await check(node, false);
  generate();
  assert.deepEqual((await readdir(join(directory, 'generated'))).sort(),
    ['index.d.ts', 'index.js', 'request-validators.js', 'response-validators.js']);
  const { validate } = await import(pathToFileURL(join(directory, 'generated/index.js')).href);
  const fixtures = JSON.parse(await readFile(join(directory, 'schema/fixtures.json'), 'utf8'));
  const subscription = fixtures.find(fixture => fixture.type === 'SubscribeParams');
  assert.equal(validate(subscription.type, subscription.value), true);
  assert.equal(validate(subscription.type, { ...subscription.value, cursor: 1 }), false);

  const initialized = await snapshot();
  generate();
  assert.deepEqual(await snapshot(), initialized, 'generation must be byte-for-byte deterministic');
  await check(go, true);
  await check(node, true);

  const schemaPath = join(directory, 'schema/SubscribeParams.json');
  const schema = JSON.parse(await readFile(schemaPath, 'utf8'));
  await writeFile(schemaPath, JSON.stringify({ ...schema, description: 'Stale contract' }));
  await check(go, false);
  await check(node, false);
  generate();

  const runtimePath = join(directory, 'generated/index.js');
  await writeFile(runtimePath, (await readFile(runtimePath, 'utf8')) + '\n// stale output\n');
  await check(node, false);
  await rm(runtimePath);
  await check(node, false);
  await rm(schemaPath);
  await check(go, false);
  generate();
  await writeFile(join(directory, 'schema/RemovedContract.json'), '{}\n');
  await check(go, false);
  generate();
  assert.deepEqual(await snapshot(), initialized, 'refresh must restore missing output and remove obsolete schemas');
});
