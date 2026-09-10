import assert from 'node:assert/strict';
import { validateRuntimeSigning } from './runtime-signing.mjs';

export function validateRuntimeEvidence(value, evidence) {
  assert(value?.schema === 1 && value.completed === true, 'Signed runtime acceptance did not complete');
  assert(evidence.signed && evidence.notarized, 'Runtime acceptance requires a signed, notarized package');
  validateRuntimeSigning(evidence.runtimeSigning, evidence.teamId);
  for (const key of ['version', 'buildId', 'source', 'nativeFiles', 'teamId', 'runtimeSigning'])
    assert.deepEqual(value.package?.[key], evidence[key], `Runtime acceptance tested a different package: ${key}`);
  const expected = evidence.nativeFiles?.whipcode?.sha256;
  assert.match(expected ?? '', /^[a-f0-9]{64}$/);
  for (const name of ['packaged', 'installed']) {
    const report = value[name];
    assert.equal(report?.sha256, expected, `${name} runtime digest differs`);
    assert.equal(report.architecture, 'arm64', `${name} runtime was not tested on arm64`);
    assert.deepEqual(Object.keys(report.engines ?? {}).sort(), ['quickjs', 'starlark'], `${name} runtime did not test both engines`);
    for (const engine of ['quickjs', 'starlark']) {
      const result = report.engines[engine];
      assert.equal(result.descriptor?.id, engine, `${name} engine identity differs`);
      for (const key of ['build', 'abi', 'profile']) assert(typeof result.descriptor[key] === 'string' && result.descriptor[key].length > 0, `Missing ${engine} ${key}`);
      const checks = ['execution', 'output', 'host-call', 'persistent-state', 'checkpoint-restore',
        ...(engine === 'quickjs' ? ['cancellation', 'recovery'] : [])];
      assert.deepEqual(result.checks, checks, `${name} ${engine} acceptance is incomplete`);
      assert.deepEqual(result.descriptor, value.packaged.engines[engine].descriptor, 'Installed engine differs from the package');
    }
  }
  return value;
}
