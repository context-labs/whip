import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { createHash } from 'node:crypto';
import { copyFile, mkdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { preview } from 'vite';

if (process.platform !== 'darwin') throw new Error('WHIP screenshot baselines are a macOS-only local gate. Run test:browser/test:csp for portable checks; do not compare Linux rendering to macOS baselines.');
const flags = process.argv.slice(2);
assert.ok(flags.every(flag => ['--update', '--prove-diff'].includes(flag)), 'Supported flags: --update or --prove-diff');
assert.ok(flags.length <= 1, 'Baseline updates and negative proofs must run separately.');
const root = fileURLToPath(new URL('../', import.meta.url));
const results = fileURLToPath(new URL('../ui-test-results/', import.meta.url));
const baseline = fileURLToPath(new URL('./visual-baselines/macos/runtime-light.png', import.meta.url));
const proof = flags.includes('--prove-diff');
const before = proof ? createHash('sha256').update(await readFile(baseline)).digest('hex') : undefined;
await mkdir(results, { recursive: true });
const server = await preview({ configFile: false, root, build: { outDir: 'storybook-static' }, preview: { host: '127.0.0.1', port: 0 } });
try {
  const address = server.httpServer.address();
  const child = spawn(process.execPath, [fileURLToPath(import.meta.resolve('@playwright/test/cli')), 'test', '--config', fileURLToPath(new URL('./visual.config.mjs', import.meta.url)), ...(flags.includes('--update') ? ['--update-snapshots'] : []), ...(proof ? ['--grep', 'runtime light$'] : [])], {
    cwd: root, stdio: 'inherit',
    env: { ...process.env, WHIP_UI_VISUAL_ORIGIN: `http://127.0.0.1:${address.port}`, WHIP_UI_VISUAL_MUTATE: proof ? '1' : '' },
  });
  const [code, signal] = await once(child, 'exit');
  if (!proof) { assert.equal(code, 0, `Visual comparison failed (${code ?? signal}); inspect ui-test-results/visual. Update baselines only after reviewing intended changes.`); }
  else {
    const report = JSON.parse(await readFile(new URL('../ui-test-results/visual-report.json', import.meta.url), 'utf8'));
    const tests = [];
    const collect = suite => { for (const spec of suite.specs ?? []) tests.push(...spec.tests); for (const nested of suite.suites ?? []) collect(nested); };
    for (const suite of report.suites) collect(suite);
    assert.equal(code, 1, 'The intentional geometry change must fail image comparison');
    assert.equal(tests.length, 1);
    const result = tests[0].results[0];
    const diff = result.attachments?.find(attachment => attachment.name.endsWith('-diff.png'));
    assert.equal(result.status, 'failed');
    assert.match(result.error?.message ?? '', /toHaveScreenshot/);
    assert.ok(diff && (await readFile(diff.path)).length > 0, 'Require an actual expected/actual image difference, not an unrelated test failure');
    assert.equal(createHash('sha256').update(await readFile(baseline)).digest('hex'), before, 'Negative proof must not rewrite the reviewed baseline');
    const proofDirectory = new URL('../ui-test-results/visual-diff-proof/', import.meta.url);
    await mkdir(proofDirectory, { recursive: true });
    for (const attachment of result.attachments.filter(item => item.contentType === 'image/png')) await copyFile(attachment.path, new URL(attachment.name, proofDirectory));
    await writeFile(new URL('../ui-test-results/visual-diff-proof.json', import.meta.url), JSON.stringify({ passed: true, expectedFailure: 'runtime light geometry mutation', baselineSHA256: before, diff: fileURLToPath(new URL(diff.name, proofDirectory)) }, null, 2));
    console.log('Negative proof passed: changed component geometry failed screenshot comparison and the baseline was unchanged.');
  }
} finally { await new Promise((resolve, reject) => server.httpServer.close(error => error ? reject(error) : resolve())); }
