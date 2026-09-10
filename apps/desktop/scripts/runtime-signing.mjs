import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';

const exec = promisify(execFile);
export const runtimeEntitlements = { 'com.apple.security.cs.allow-unsigned-executable-memory': true };

export function validateRuntimeSigning(signing, teamId) {
  assert(signing && typeof signing === 'object', 'Missing backend signing policy');
  assert.match(teamId ?? '', /^[A-Z0-9]{10}$/, 'Missing backend signing team');
  assert.equal(signing.hardenedRuntime, true, 'Backend hardened runtime is missing');
  assert.deepEqual(signing.entitlements, runtimeEntitlements, 'Backend executable-memory entitlements differ from policy');
  assert.equal(signing.teamId, teamId, 'Backend signing team differs from the package');
  assert.match(signing.identifier, /^com\.contextlabs\.whip(?:\.beta)?\.runtime$/, 'Unexpected backend signing identity');
}

export async function verifyRuntimeSigning(executable, teamId) {
  const options = { timeout: 10_000, maxBuffer: 64 << 10, encoding: 'utf8' };
  const { stdout: plist } = await exec('/usr/bin/codesign', ['-d', '--entitlements', '-', '--xml', executable], options);
  const entitlements = await new Promise((resolve, reject) => {
    const child = execFile('/usr/bin/plutil', ['-convert', 'json', '-o', '-', '--', '-'], options,
      (error, stdout) => {
        if (error) { reject(new Error('Cannot read backend entitlements', { cause: error })); return; }
        try { resolve(JSON.parse(stdout)); } catch (error) { reject(error); }
      });
    child.stdin.on('error', reject);
    child.stdin.end(plist);
  });
  const { stderr } = await exec('/usr/bin/codesign', ['-dvv', executable], options);
  const flags = /^CodeDirectory .*flags=(0x[0-9a-f]+)/mi.exec(stderr)?.[1];
  const signing = { entitlements, hardenedRuntime: !!(Number(flags) & 0x10000),
    identifier: /^Identifier=(.+)$/m.exec(stderr)?.[1], teamId: /^TeamIdentifier=(.+)$/m.exec(stderr)?.[1] };
  validateRuntimeSigning(signing, teamId);
  return signing;
}
